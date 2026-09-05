package main

// final_semantic_fleet_generation_source.go reconstructs the ordinary-fleet
// generation proof from the sealed collection graph. It never reads the live
// state directory: every plan, journal entry, signed manifest, and receipt
// source is copied into the lineage artifact before the semantic object is
// sealed.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// finalFleetGenerationSource is the immutable-input join for the ordinary
// fleet proof. The raw map is deliberately separate from archive.files so a
// derived artifact contains only the source bytes that its index consumes.
type finalFleetGenerationSource struct {
	archive    *finalSemanticArchive
	evidence   *FinalSemanticEvidence
	chain      *FinalCollectedChainSnapshot
	events     *finalSemanticEventIndex
	current    *SetupPlan
	plans      map[string]*SetupPlan
	planPaths  map[string]string
	entries    []JournalEntry
	raw        map[string][]byte
	postProofs map[string]FinalArtifactLocator
	versions   map[string]finalFleetGenerationCachedVersion
}

// Stores a parsed signed generation after its source bytes and journal lineage
// have been authenticated. The builder is single-threaded, so the cache needs
// no synchronization and prevents a repeated derived-artifact write from
// creating an alternate source path for the same generation.
type finalFleetGenerationCachedVersion struct {
	evidence FinalFleetGenerationVersionEvidence
	manifest protocol.FleetManifest
}

// constructs the source context after validating the complete journal chain
// and every archived ancestor-plan byte stream before it is used for an
// accepted carried intent.
func newFinalFleetGenerationSource(archive *finalSemanticArchive, evidence *FinalSemanticEvidence, chain *FinalCollectedChainSnapshot, events *finalSemanticEventIndex) (*finalFleetGenerationSource, error) {
	if archive == nil || evidence == nil || chain == nil || events == nil {
		return nil, errors.New("ordinary fleet generation source context is incomplete")
	}
	planBytes, _, err := archive.file("launch-foundation/plan.json")
	if err != nil {
		return nil, err
	}
	current, err := archive.decodeSetupPlan("launch-foundation/plan.json", planBytes)
	if err != nil || current.PlanHash != evidence.PlanHash || current.DeploymentID != evidence.DeploymentID || current.ChainID != evidence.ChainID || current.Netuid != evidence.Netuid {
		return nil, stateMismatchError(err, "ordinary fleet generation current plan differs from semantic identity")
	}
	batcher, _, err := finalPlanFleetBatcher(current)
	if err != nil || !strings.EqualFold(chain.FleetBatcher, batcher.Hex()) {
		return nil, stateMismatchError(err, "ordinary fleet generation snapshot batcher differs from approved plan")
	}
	journalBytes, _, err := archive.file("launch-foundation/journal.jsonl")
	if err != nil {
		return nil, err
	}
	entries, err := decodeFinalSemanticJournalBytes(journalBytes)
	if err != nil {
		return nil, err
	}
	result := &finalFleetGenerationSource{
		archive: archive, evidence: evidence, chain: chain, events: events, current: current,
		plans: map[string]*SetupPlan{current.PlanHash: current}, planPaths: map[string]string{current.PlanHash: "launch-foundation/plan.json"},
		entries: entries, raw: map[string][]byte{"launch-foundation/plan.json": append([]byte(nil), planBytes...), "launch-foundation/journal.jsonl": append([]byte(nil), journalBytes...)},
		postProofs: make(map[string]FinalArtifactLocator), versions: make(map[string]finalFleetGenerationCachedVersion),
	}
	// The closed archive namespace is part of the proof boundary. Do not
	// silently skip a sibling binary, nested file, or unrelated JSON object:
	// accepting only the files we happen to decode would let an archive carry
	// unreviewed plan-history input outside the lineage artifact.
	expectedPlanPaths := make(map[string]string, len(current.PriorPlanHashes))
	for _, planHash := range current.PriorPlanHashes {
		if err := requireFinalHex32("ordinary fleet generation predecessor plan", planHash); err != nil {
			return nil, err
		}
		name := filepath.ToSlash(filepath.Join("plan-history", stringsTrim0x(planHash)+".json"))
		if _, duplicate := expectedPlanPaths[name]; duplicate {
			return nil, fmt.Errorf("ordinary fleet generation predecessor plan path %s is duplicated", name)
		}
		expectedPlanPaths[name] = planHash
	}
	names := make([]string, 0, len(expectedPlanPaths))
	for name := range archive.files {
		if name != "plan-history" && !strings.HasPrefix(name, "plan-history/") {
			continue
		}
		if _, approved := expectedPlanPaths[name]; !approved {
			return nil, fmt.Errorf("ordinary fleet generation plan-history entry %s is not an approved canonical predecessor path", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := archive.files[name]
		plan, decodeErr := archive.decodeSetupPlan(name, data)
		if decodeErr != nil {
			return nil, fmt.Errorf("ordinary fleet generation decode %s: %w", name, decodeErr)
		}
		if plan.PlanHash != expectedPlanPaths[name] {
			return nil, fmt.Errorf("ordinary fleet generation plan-history entry %s does not match its approved predecessor hash", name)
		}
		if prior := result.plans[plan.PlanHash]; prior != nil {
			if !finalJSONEqual(prior, plan) {
				return nil, fmt.Errorf("ordinary fleet generation plan %s has conflicting archived copies", plan.PlanHash)
			}
			continue
		}
		result.plans[plan.PlanHash] = plan
		result.planPaths[plan.PlanHash] = name
	}
	for _, hash := range current.PriorPlanHashes {
		if result.plans[hash] == nil {
			return nil, fmt.Errorf("ordinary fleet generation approved predecessor plan %s is absent", hash)
		}
		// The source context validates every direct predecessor before any
		// carried action is selected. Retain those exact bytes as well: an
		// offline replay must not rely on an ancestor that existed only in the
		// original mutable archive but was not reached by a later loop branch.
		if _, recordErr := result.record(result.planPaths[hash]); recordErr != nil {
			return nil, recordErr
		}
	}
	return result, nil
}

// Returns a detached copy while retaining one immutable copy in the lineage
// index. Repeated indexing still compares the archive's current exact bytes.
func (self *finalFleetGenerationSource) record(path string) ([]byte, error) {
	data, err := self.recordBytes(path)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), data...), nil
}

// Borrows the source index's owned bytes only for internal plan checks or
// immediate detachment by record. Callers must never mutate this byte view.
func (self *finalFleetGenerationSource) recordBytes(path string) ([]byte, error) {
	if self == nil || self.archive == nil || path == "" {
		return nil, errors.New("ordinary fleet generation source file is unavailable")
	}
	data, found := self.archive.files[filepath.ToSlash(path)]
	if !found {
		return nil, fmt.Errorf("closed semantic graph is missing %s", path)
	}
	if prior, found := self.raw[path]; found {
		if !bytes.Equal(prior, data) {
			return nil, fmt.Errorf("ordinary fleet generation source %s changed while being indexed", path)
		}
		return prior, nil
	}
	owned := append([]byte(nil), data...)
	self.raw[path] = owned
	return owned, nil
}

// records the exact approved plan that authorized a selected journal entry.
func (self *finalFleetGenerationSource) recordPlan(planHash string) (*SetupPlan, error) {
	if self == nil || planHash == "" {
		return nil, errors.New("ordinary fleet generation plan identity is unavailable")
	}
	plan := self.plans[planHash]
	path := self.planPaths[planHash]
	if plan == nil || path == "" {
		return nil, fmt.Errorf("ordinary fleet generation source plan %s is absent", planHash)
	}
	if _, err := self.recordBytes(path); err != nil {
		return nil, err
	}
	return plan, nil
}

// finds the sole current action identity that permits a carried predecessor
// intent. A current action cannot inherit an ancestor mutation merely because
// the action ID happens to match.
func (self *finalFleetGenerationSource) currentAction(actionID string) (Action, error) {
	if self == nil || self.current == nil {
		return Action{}, errors.New("ordinary fleet generation current plan is unavailable")
	}
	return exactPlanActionByID(self.current, actionID)
}

// decodes one verified journal postcondition through its canonical plan-hash
// path, then confirms its content hash and action identity before returning
// the immutable bytes used as a derived receipt proof.
func (self *finalFleetGenerationSource) verifiedPostcondition(entry JournalEntry) (*ActionPostcondition, []byte, error) {
	if self == nil || entry.Stage != StageVerified || entry.PlanHash == "" || entry.ActionID == "" || entry.IntentHash == "" {
		return nil, nil, errors.New("ordinary fleet generation verified journal identity is incomplete")
	}
	path, err := postconditionRelativePath(entry.PlanHash, entry.ActionID)
	if err != nil || entry.PostconditionPath != path {
		return nil, nil, stateMismatchError(err, "ordinary fleet generation postcondition path is not canonical")
	}
	data, err := self.record(path)
	if err != nil {
		return nil, nil, err
	}
	record, err := decodeFinalActionPostconditionV4(data)
	if err != nil || record.DeploymentID != entry.DeploymentID || record.PlanHash != entry.PlanHash || record.ActionID != entry.ActionID || record.IntentHash != entry.IntentHash {
		return nil, nil, stateMismatchError(err, "ordinary fleet generation postcondition identity differs from its journal")
	}
	hash, err := canonicalHashHex(record)
	if err != nil || !strings.EqualFold(hash, entry.PostconditionHash) {
		return nil, nil, stateMismatchError(err, "ordinary fleet generation postcondition hash differs from its journal")
	}
	if err := verifyFinalHead("ordinary fleet generation postcondition EVM", record.EVMFinalized); err != nil {
		return nil, nil, err
	}
	if err := verifyFinalHead("ordinary fleet generation postcondition native", record.SubstrateFinalized); err != nil {
		return nil, nil, err
	}
	return record, data, nil
}

// locates one exact successful finalized transaction and its later verified
// postcondition. Expected transaction coordinates come from signed public
// evidence, so a same-named retry cannot be substituted by journal order.
func (self *finalFleetGenerationSource) verifiedMutation(actionID, transactionHash string, block uint64, blockHash string, match func(*ActionPostcondition) bool) (Action, JournalEntry, *ActionPostcondition, []byte, error) {
	if self == nil || actionID == "" || transactionHash == "" || block == 0 || blockHash == "" {
		return Action{}, JournalEntry{}, nil, nil, errors.New("ordinary fleet generation mutation coordinates are incomplete")
	}
	if err := requireFinalHex32("ordinary fleet generation transaction", transactionHash); err != nil {
		return Action{}, JournalEntry{}, nil, nil, err
	}
	if err := requireFinalHex32("ordinary fleet generation transaction block", blockHash); err != nil {
		return Action{}, JournalEntry{}, nil, nil, err
	}
	allowed := self.current.allowedPlanHashes()
	var found *struct {
		action Action
		entry  JournalEntry
		post   *ActionPostcondition
		data   []byte
	}
	for _, verified := range self.entries {
		if verified.Stage != StageVerified || verified.ActionID != actionID || !allowed[verified.PlanHash] {
			continue
		}
		plan, err := self.recordPlan(verified.PlanHash)
		if err != nil {
			return Action{}, JournalEntry{}, nil, nil, err
		}
		action, err := exactPlanActionByID(plan, actionID)
		if err != nil || !actionAcceptsIntent(action, verified.IntentHash) {
			return Action{}, JournalEntry{}, nil, nil, stateMismatchError(err, "ordinary fleet generation source action does not accept its verified intent")
		}
		current, err := self.currentAction(actionID)
		if err != nil || !actionAcceptsIntent(current, verified.IntentHash) {
			return Action{}, JournalEntry{}, nil, nil, stateMismatchError(err, "ordinary fleet generation current action does not accept predecessor intent")
		}
		post, data, err := self.verifiedPostcondition(verified)
		if err != nil || match != nil && !match(post) {
			if err != nil {
				return Action{}, JournalEntry{}, nil, nil, err
			}
			continue
		}
		var finalized *JournalEntry
		for index := range self.entries {
			candidate := &self.entries[index]
			if candidate.Stage != StageFinalized || candidate.PlanHash != verified.PlanHash || candidate.ActionID != verified.ActionID || candidate.IntentHash != verified.IntentHash {
				continue
			}
			if !strings.EqualFold(candidate.TransactionHash, transactionHash) || candidate.BlockNumber != block || !strings.EqualFold(candidate.BlockHash, blockHash) {
				continue
			}
			if finalized != nil {
				return Action{}, JournalEntry{}, nil, nil, fmt.Errorf("ordinary fleet generation action %s has duplicate exact finalization", actionID)
			}
			finalized = candidate
		}
		if finalized == nil {
			continue
		}
		// Preserve the finalized transaction coordinates while carrying the
		// later authenticated postcondition locator.  The journal deliberately
		// records these at different stages, so returning the raw finalized row
		// would make every downstream proof look for a locator that can only
		// exist on the verified row.
		finalizedCopy := *finalized
		finalizedCopy.PostconditionHash = verified.PostconditionHash
		finalizedCopy.PostconditionPath = verified.PostconditionPath
		candidate := &struct {
			action Action
			entry  JournalEntry
			post   *ActionPostcondition
			data   []byte
		}{action: action, entry: finalizedCopy, post: post, data: data}
		if found != nil {
			return Action{}, JournalEntry{}, nil, nil, fmt.Errorf("ordinary fleet generation action %s has multiple matching verified mutations", actionID)
		}
		found = candidate
	}
	if found == nil {
		return Action{}, JournalEntry{}, nil, nil, fmt.Errorf("ordinary fleet generation action %s has no exact approved verified mutation", actionID)
	}
	return found.action, found.entry, found.post, found.data, nil
}

// materializes a content-addressed proof for one journal-authenticated
// postcondition. The map makes shared aliases deterministic and avoids
// recreating a second URI for the same byte stream.
func (self *finalFleetGenerationSource) postconditionProof(entry JournalEntry, data []byte) (FinalArtifactLocator, error) {
	if self == nil || entry.PostconditionPath == "" || len(data) == 0 {
		return FinalArtifactLocator{}, errors.New("ordinary fleet generation postcondition proof is unavailable")
	}
	if proof, found := self.postProofs[entry.PostconditionPath]; found {
		return proof, nil
	}
	name := filepath.ToSlash(filepath.Join("fleet-generation", "postconditions", stringsTrim0x(entry.PlanHash), entry.ActionID+".json"))
	proof, err := self.archive.derivedBytes("fleet-generation-postcondition", name, data)
	if err != nil {
		return FinalArtifactLocator{}, err
	}
	self.postProofs[entry.PostconditionPath] = proof
	return proof, nil
}

// builds the sealed generation proof after terminal topology construction so
// the proof can partition exactly the same 202 fleet IDs as the final census.
func (self *finalSemanticArchive) buildFleetGeneration(source *FinalSemanticEvidence, chain *FinalCollectedChainSnapshot, events *finalSemanticEventIndex) error {
	context, err := newFinalFleetGenerationSource(self, source, chain, events)
	if err != nil {
		return err
	}
	lineage := &FinalFleetGenerationLineageEvidence{Schema: finalFleetGenerationLineageSchema}
	for batch := uint64(1); batch <= finalFleetGenerationBatchCount; batch++ {
		initial, err := context.batch(batch, 1)
		if err != nil {
			return err
		}
		lineage.Batches = append(lineage.Batches, initial)
	}
	for batch := uint64(1); batch <= finalFleetGenerationBatchCount; batch++ {
		refresh, err := context.batch(batch, 2)
		if err != nil {
			return err
		}
		lineage.Batches = append(lineage.Batches, refresh)
	}
	for fleetID := uint64(1); fleetID <= finalFleetGenerationSetupFleetCount; fleetID++ {
		batch := (fleetID-1)/finalFleetGenerationBatchSize + 1
		initial, _, err := context.version(fleetID, 1, batch)
		if err != nil {
			return err
		}
		refresh, _, err := context.version(fleetID, 2, batch)
		if err != nil {
			return err
		}
		if err := context.verifyReplacement(fleetID, initial, refresh); err != nil {
			return err
		}
		lineage.SetupFleets = append(lineage.SetupFleets, FinalFleetGenerationFleetEvidence{FleetID: fleetID, Initial: initial, Refresh: refresh})
	}
	for fleetID := finalFleetGenerationSetupFleetCount + 1; fleetID <= finalFleetGenerationSetupFleetCount+finalFleetGenerationChallengerFleetCount; fleetID++ {
		initial, _, err := context.version(fleetID, 1, 0)
		if err != nil {
			return err
		}
		registration, transition, err := context.challenger(fleetID)
		if err != nil {
			return err
		}
		lineage.ChallengerFleets = append(lineage.ChallengerFleets, FinalFleetGenerationChallengerEvidence{FleetID: fleetID, Initial: initial, Registration: registration, Transition: transition})
	}
	files := make([]finalFleetGenerationLineageFile, 0, len(context.raw))
	names := make([]string, 0, len(context.raw))
	for name := range context.raw {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := context.raw[name]
		files = append(files, finalFleetGenerationLineageFile{Path: name, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data)), Data: append([]byte(nil), data...)})
	}
	artifact := finalFleetGenerationLineageArtifact{Schema: finalFleetGenerationLineageSchema, DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, Files: files}
	locator, err := self.derived("fleet-generation-lineage", "fleet-generation-lineage.json", artifact)
	if err != nil {
		return err
	}
	lineage.Artifact = locator
	canonicalizeFinalFleetGenerationLineage(lineage)
	if err := verifyFinalFleetGenerationLineage(source, lineage); err != nil {
		return err
	}
	if err := verifyFinalFleetGenerationEventTopology(source, lineage); err != nil {
		return err
	}
	source.FleetGeneration = lineage
	return nil
}

// decodes and validates one signed manifest generation together with every
// dual-signed member binding and its native commitment journal lineage.
func (self *finalFleetGenerationSource) version(fleetID, generation, batch uint64) (FinalFleetGenerationVersionEvidence, protocol.FleetManifest, error) {
	if self == nil || fleetID == 0 || generation != 1 && generation != 2 {
		return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, errors.New("ordinary fleet generation coordinates are invalid")
	}
	key := finalFleetGenerationBatchKey(generation, fleetID)
	if cached, found := self.versions[key]; found {
		return cached.evidence, cached.manifest, nil
	}
	suffix := ""
	if generation == 2 {
		suffix = ".refresh"
	}
	manifestPath := fmt.Sprintf("public/fleet-%d%s.json", fleetID, suffix)
	manifestData, err := self.record(manifestPath)
	if err != nil {
		return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, err
	}
	manifest, err := protocol.ParseFleetManifest(manifestData)
	if err != nil || manifest.ChainID != self.evidence.ChainID || manifest.Netuid != self.evidence.Netuid || common.BytesToAddress(manifest.Coordinator[:]) != common.HexToAddress(self.evidence.Deployment.CoordinatorProxy) || manifest.Generation != generation || len(manifest.Members) != int(finalFleetGenerationMembersPerFleet) {
		return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, stateMismatchError(err, "ordinary fleet generation manifest identity differs")
	}
	commitmentPath := fmt.Sprintf("public/fleet-%d%s.commitment.json", fleetID, suffix)
	commitmentData, err := self.record(commitmentPath)
	if err != nil {
		return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, err
	}
	var commitment FleetCommitmentEvidence
	if err := decodeStrictJSONBytes(commitmentData, &commitment); err != nil {
		return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, fmt.Errorf("decode ordinary fleet %d generation %d commitment: %w", fleetID, generation, err)
	}
	hash, err := manifest.CommitmentHash()
	if err != nil || commitment.Schema != fleetCommitmentEvidenceSchemaV2 || !strings.EqualFold(commitment.CommitmentHash, "0x"+hex.EncodeToString(hash[:])) || !strings.EqualFold(commitment.Hotkey, "0x"+hex.EncodeToString(manifest.Hotkey[:])) || commitment.FinalizedBlock == 0 || commitment.FinalizedBlockHash == "" || commitment.ExtrinsicHash == "" || commitment.ManifestURI != strings.TrimPrefix(manifestPath, "public/") {
		return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, stateMismatchError(err, "ordinary fleet generation commitment differs from signed manifest")
	}
	commitmentActionID := fmt.Sprintf("fleet.commitment.%d", fleetID)
	if generation == 2 {
		commitmentActionID = fmt.Sprintf("fleet.refresh.commitment.%d", fleetID)
	}
	_, commitmentEntry, post, postData, err := self.verifiedMutation(commitmentActionID, strings.ToLower(commitment.ExtrinsicHash), commitment.FinalizedBlock, strings.ToLower(commitment.FinalizedBlockHash), nil)
	if err != nil {
		return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, err
	}
	if post.SubstrateFinalized.Number < commitmentEntry.BlockNumber || post.SubstrateFinalized.Number > self.evidence.NativeTerminalHead.Number {
		return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, errors.New("ordinary fleet generation commitment finality is outside semantic boundaries")
	}
	manifestLocator, err := self.archive.derivedBytes("fleet-generation-manifest", filepath.ToSlash(filepath.Join("fleet-generation", "manifests", fmt.Sprintf("fleet-%d-generation-%d.json", fleetID, generation))), manifestData)
	if err != nil {
		return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, err
	}
	commitmentLocator, err := self.archive.derivedBytes("fleet-generation-commitment", filepath.ToSlash(filepath.Join("fleet-generation", "commitments", fmt.Sprintf("fleet-%d-generation-%d.json", fleetID, generation))), commitmentData)
	if err != nil {
		return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, err
	}
	commitmentProof, err := self.postconditionProof(commitmentEntry, postData)
	if err != nil {
		return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, err
	}
	version := FinalFleetGenerationVersionEvidence{
		Generation: generation, Batch: batch, Manifest: manifestLocator, Commitment: commitmentLocator,
		CommitmentAction:        FinalFleetGenerationActionEvidence{ActionID: commitmentActionID, PlanHash: strings.ToLower(commitmentEntry.PlanHash), IntentHash: strings.ToLower(commitmentEntry.IntentHash)},
		CommitmentExtrinsicHash: strings.ToLower(commitment.ExtrinsicHash), CommitmentPostcondition: commitmentProof, CommitmentHash: strings.ToLower(commitment.CommitmentHash), Hotkey: strings.ToLower(commitment.Hotkey),
		NativeHead: ChainHead{Number: commitment.FinalizedBlock, Hash: strings.ToLower(commitment.FinalizedBlockHash)},
	}
	for memberIndex := range manifest.Members {
		memberNumber := uint64(memberIndex + 1)
		if generation == 1 {
			memberEvidence, err := self.initialMember(fleetID, memberNumber, manifest, commitment)
			if err != nil {
				return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, err
			}
			version.Members = append(version.Members, memberEvidence)
		} else {
			memberEvidence, err := self.refreshMember(fleetID, memberNumber, manifest, commitment)
			if err != nil {
				return FinalFleetGenerationVersionEvidence{}, protocol.FleetManifest{}, err
			}
			version.Members = append(version.Members, memberEvidence)
		}
	}
	self.versions[key] = finalFleetGenerationCachedVersion{evidence: version, manifest: *manifest}
	return version, *manifest, nil
}

// Confirms every signed revoke preimage names the exact authenticated
// generation-one member rather than merely any earlier binding with the same
// public client key. This is deliberately source-level: the compact semantic
// projection retains stable identity while the lineage artifact retains the
// signed revoke fields required to prove replacement.
func (self *finalFleetGenerationSource) verifyReplacement(fleetID uint64, initial, refresh FinalFleetGenerationVersionEvidence) error {
	if self == nil || initial.Generation != 1 || refresh.Generation != 2 || initial.CommitmentHash == "" || refresh.CommitmentHash == "" || strings.EqualFold(initial.CommitmentHash, refresh.CommitmentHash) || len(initial.Members) != int(finalFleetGenerationMembersPerFleet) || len(refresh.Members) != int(finalFleetGenerationMembersPerFleet) {
		return fmt.Errorf("ordinary fleet generation %d replacement context is invalid", fleetID)
	}
	for memberIndex := range initial.Members {
		memberNumber := uint64(memberIndex + 1)
		data, err := self.record(fmt.Sprintf("public/fleet-%d-member-%d.refresh.binding.json", fleetID, memberNumber))
		if err != nil {
			return err
		}
		var binding FleetRefreshBindingEvidence
		if err := decodeStrictJSONBytes(data, &binding); err != nil {
			return err
		}
		before, after := initial.Members[memberIndex], refresh.Members[memberIndex]
		if binding.Schema != fleetRefreshBindingEvidenceSchema || binding.Fleet != int(fleetID) || binding.Member != int(memberNumber) || binding.PriorGeneration != 1 || binding.ReplacementGeneration != 2 || !strings.EqualFold(binding.PriorCommitmentHash, before.CommitmentHash) || !strings.EqualFold(binding.CommitmentHash, after.CommitmentHash) || binding.PriorValidFromEpoch != before.ValidFromEpoch || binding.PriorOriginalValidToEpoch != before.ValidToEpoch || !strings.EqualFold(binding.ClientID, before.ClientID) || !strings.EqualFold(binding.ClientKey, before.ClientKey) || !strings.EqualFold(binding.FleetID, before.FleetKey) || !strings.EqualFold(binding.Hotkey, before.Hotkey) || binding.ValidFromEpoch != after.ValidFromEpoch || binding.ValidToEpoch != after.ValidToEpoch {
			return fmt.Errorf("ordinary fleet generation %d member %d revoke does not replace its exact generation one binding", fleetID, memberNumber)
		}
	}
	return nil
}

// validates one generation-one binding's manifest preimage and both detached
// signatures before projecting its immutable member identity.
func (self *finalFleetGenerationSource) initialMember(fleetID, memberNumber uint64, manifest *protocol.FleetManifest, commitment FleetCommitmentEvidence) (FinalFleetGenerationMemberEvidence, error) {
	path := fmt.Sprintf("public/fleet-%d-member-%d.binding.json", fleetID, memberNumber)
	data, err := self.record(path)
	if err != nil {
		return FinalFleetGenerationMemberEvidence{}, err
	}
	var evidence FleetBindingEvidence
	if err := decodeStrictJSONBytes(data, &evidence); err != nil {
		return FinalFleetGenerationMemberEvidence{}, err
	}
	if evidence.Schema != "urnetwork-fleet-binding-evidence-v1" || manifest == nil || memberNumber == 0 || memberNumber > uint64(len(manifest.Members)) || evidence.Generation != 1 || evidence.BlockNumber == 0 || evidence.TransactionHash == "" || evidence.BlockHash == "" {
		return FinalFleetGenerationMemberEvidence{}, errors.New("ordinary fleet generation-one binding identity is incomplete")
	}
	binding, err := manifest.Binding(manifest.Members[memberNumber-1], evidence.ValidFromEpoch, evidence.ValidToEpoch)
	if err != nil {
		return FinalFleetGenerationMemberEvidence{}, err
	}
	fields := []struct {
		got  string
		want []byte
	}{
		{got: evidence.ClientID, want: binding.ClientID[:]}, {got: evidence.ClientKey, want: binding.ClientKey[:]},
		{got: evidence.FleetID, want: binding.FleetID[:]}, {got: evidence.Hotkey, want: binding.Hotkey[:]}, {got: evidence.CommitmentHash, want: binding.CommitmentHash[:]},
	}
	for _, field := range fields {
		got, ok := evidenceFixedHex(field.got, len(field.want))
		if !ok || !bytes.Equal(got, field.want) {
			return FinalFleetGenerationMemberEvidence{}, errors.New("ordinary fleet generation-one binding differs from its manifest")
		}
	}
	digest, err := binding.Digest()
	if err != nil || !strings.EqualFold(evidence.BindingDigest, "0x"+hex.EncodeToString(digest[:])) {
		return FinalFleetGenerationMemberEvidence{}, stateMismatchError(err, "ordinary fleet generation-one binding digest differs")
	}
	clientSignature, clientOK := evidenceFixedHex(evidence.ClientSignature, ed25519.SignatureSize)
	hotkeySignature, hotkeyOK := evidenceFixedHex(evidence.HotkeySignature, ed25519.SignatureSize)
	if !clientOK || !hotkeyOK || !binding.VerifyClient(clientSignature) || !binding.VerifyHotkey(hotkeySignature) {
		return FinalFleetGenerationMemberEvidence{}, errors.New("ordinary fleet generation-one binding signatures are invalid")
	}
	if _, _, _, _, err := self.verifiedMutation(fmt.Sprintf("fleet.bind.%d.%d", fleetID, memberNumber), strings.ToLower(evidence.TransactionHash), evidence.BlockNumber, strings.ToLower(evidence.BlockHash), nil); err != nil {
		return FinalFleetGenerationMemberEvidence{}, err
	}
	if !strings.EqualFold(evidence.CommitmentHash, commitment.CommitmentHash) {
		return FinalFleetGenerationMemberEvidence{}, errors.New("ordinary fleet generation-one member commitment differs")
	}
	return FinalFleetGenerationMemberEvidence{Member: memberNumber, ClientID: strings.ToLower(evidence.ClientID), ClientKey: strings.ToLower(evidence.ClientKey), FleetKey: strings.ToLower(evidence.FleetID), Hotkey: strings.ToLower(evidence.Hotkey), CommitmentHash: strings.ToLower(evidence.CommitmentHash), Generation: 1, ValidFromEpoch: evidence.ValidFromEpoch, ValidToEpoch: evidence.ValidToEpoch, UID: evidence.UID}, nil
}

// validates one successor binding and the independent revoke consent which
// proves that a generation-two row replaced exactly generation one.
func (self *finalFleetGenerationSource) refreshMember(fleetID, memberNumber uint64, manifest *protocol.FleetManifest, commitment FleetCommitmentEvidence) (FinalFleetGenerationMemberEvidence, error) {
	path := fmt.Sprintf("public/fleet-%d-member-%d.refresh.binding.json", fleetID, memberNumber)
	data, err := self.record(path)
	if err != nil {
		return FinalFleetGenerationMemberEvidence{}, err
	}
	var evidence FleetRefreshBindingEvidence
	if err := decodeStrictJSONBytes(data, &evidence); err != nil {
		return FinalFleetGenerationMemberEvidence{}, err
	}
	if manifest == nil || int(memberNumber) != evidence.Member || int(fleetID) != evidence.Fleet || evidence.TransactionHash == "" || evidence.BlockNumber == 0 || evidence.BlockHash == "" {
		return FinalFleetGenerationMemberEvidence{}, errors.New("ordinary fleet generation-two binding identity is incomplete")
	}
	binding, revoke, err := fleetRefreshEvidenceBindings(evidence, common.HexToAddress(self.evidence.Deployment.CoordinatorProxy), self.evidence.ChainID, self.evidence.Netuid)
	if err != nil || !fleetRefreshReplacementMatchesManifest(binding, *manifest, evidence.Member, binding.CommitmentHash, evidence.ValidFromEpoch, evidence.ValidToEpoch) {
		return FinalFleetGenerationMemberEvidence{}, stateMismatchError(err, "ordinary fleet generation-two binding differs from its manifest")
	}
	if !strings.EqualFold(evidence.CommitmentHash, commitment.CommitmentHash) || !strings.EqualFold(evidence.PriorCommitmentHash, "") && strings.EqualFold(evidence.PriorCommitmentHash, evidence.CommitmentHash) {
		return FinalFleetGenerationMemberEvidence{}, errors.New("ordinary fleet generation-two commitment lineage is invalid")
	}
	bindingDigest, err := binding.Digest()
	if err != nil || !strings.EqualFold(evidence.BindingDigest, "0x"+hex.EncodeToString(bindingDigest[:])) {
		return FinalFleetGenerationMemberEvidence{}, stateMismatchError(err, "ordinary fleet generation-two binding digest differs")
	}
	revokeDigest, err := revoke.Digest()
	if err != nil || !strings.EqualFold(evidence.RevokeDigest, "0x"+hex.EncodeToString(revokeDigest[:])) {
		return FinalFleetGenerationMemberEvidence{}, stateMismatchError(err, "ordinary fleet generation-two revoke digest differs")
	}
	clientSignature, clientOK := evidenceFixedHex(evidence.ClientSignature, ed25519.SignatureSize)
	hotkeySignature, hotkeyOK := evidenceFixedHex(evidence.HotkeySignature, ed25519.SignatureSize)
	revokeSignature, revokeOK := evidenceFixedHex(evidence.RevokeSignature, ed25519.SignatureSize)
	if !clientOK || !hotkeyOK || !revokeOK || !binding.VerifyClient(clientSignature) || !binding.VerifyHotkey(hotkeySignature) || !revoke.VerifyClient(ed25519.PublicKey(binding.ClientKey[:]), revokeSignature) {
		return FinalFleetGenerationMemberEvidence{}, errors.New("ordinary fleet generation-two binding signatures are invalid")
	}
	if _, _, _, _, err := self.verifiedMutation(fmt.Sprintf("fleet.refresh.batch.%d", (fleetID-1)/finalFleetGenerationBatchSize+1), strings.ToLower(evidence.TransactionHash), evidence.BlockNumber, strings.ToLower(evidence.BlockHash), nil); err != nil {
		return FinalFleetGenerationMemberEvidence{}, err
	}
	return FinalFleetGenerationMemberEvidence{Member: memberNumber, ClientID: strings.ToLower(evidence.ClientID), ClientKey: strings.ToLower(evidence.ClientKey), FleetKey: strings.ToLower(evidence.FleetID), Hotkey: strings.ToLower(evidence.Hotkey), CommitmentHash: strings.ToLower(evidence.CommitmentHash), Generation: 2, ValidFromEpoch: evidence.ValidFromEpoch, ValidToEpoch: evidence.ValidToEpoch, UID: evidence.UID}, nil
}

// builds one exact batch partition. Generation one records an explicit mix of
// installed batcher receipt coverage and carried mirror/bind history; generation
// two records the single atomic replacement helper transaction.
func (self *finalFleetGenerationSource) batch(batch, generation uint64) (FinalFleetGenerationBatchEvidence, error) {
	if self == nil || batch == 0 || batch > finalFleetGenerationBatchCount || generation != 1 && generation != 2 {
		return FinalFleetGenerationBatchEvidence{}, errors.New("ordinary fleet generation batch coordinates are invalid")
	}
	first := (batch-1)*finalFleetGenerationBatchSize + 1
	last := first + finalFleetGenerationBatchSize - 1
	actionID := finalFleetGenerationActionID(generation, batch)
	current, err := self.currentAction(actionID)
	if err != nil || current.Kind != "evm-transaction" {
		return FinalFleetGenerationBatchEvidence{}, stateMismatchError(err, "ordinary fleet generation batch has no current EVM approval")
	}
	result := FinalFleetGenerationBatchEvidence{Batch: batch, Generation: generation, FirstFleet: first, LastFleet: last, Action: FinalFleetGenerationActionEvidence{ActionID: actionID, PlanHash: strings.ToLower(self.current.PlanHash), IntentHash: strings.ToLower(current.IntentHash)}}
	if generation == 1 {
		return self.initialBatch(result)
	}
	return self.refreshBatch(result)
}

// reconstructs the exact generation-one installed/carried partition. A
// partially pre-existing batch is permitted only when the public partition,
// bounded install receipt, and each carried predecessor action cover the ten
// fleet IDs exactly once.
func (self *finalFleetGenerationSource) initialBatch(batch FinalFleetGenerationBatchEvidence) (FinalFleetGenerationBatchEvidence, error) {
	path := fmt.Sprintf("public/fleet-install-batch-%d.json", batch.Batch)
	data, err := self.record(path)
	if err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	var installed FleetInstallBatchEvidence
	if err := decodeStrictJSONBytes(data, &installed); err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	if installed.Schema != fleetInstallBatchEvidenceSchema || installed.Batch != int(batch.Batch) || installed.FirstFleet != int(batch.FirstFleet) || installed.LastFleet != int(batch.LastFleet) || installed.Generation != 1 {
		return FinalFleetGenerationBatchEvidence{}, errors.New("ordinary fleet generation-one batch identity differs")
	}
	if err := finalFleetGenerationInstallEvidenceMembers(installed, batch); err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	installedFleets, carriedFleets, err := finalFleetGenerationInstallEvidencePartition(installed, batch)
	if err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	batch.InstalledFleets, batch.CarriedFleets = installedFleets, carriedFleets
	batch.Carried = len(installedFleets) == 0
	for _, fleetID := range carriedFleets {
		version, manifest, err := self.version(uint64(fleetID), 1, batch.Batch)
		if err != nil {
			return FinalFleetGenerationBatchEvidence{}, err
		}
		commitmentHash, err := decodeHex32("ordinary fleet generation-one commitment", version.CommitmentHash)
		if err != nil {
			return FinalFleetGenerationBatchEvidence{}, err
		}
		finalizedBlockHash, hashErr := finalFleetGenerationHex32(version.NativeHead.Hash)
		if hashErr != nil {
			return FinalFleetGenerationBatchEvidence{}, hashErr
		}
		mirrorData, err := stabi.NewSTCoordinator().TryPackMirrorCommitment(manifest.Hotkey, commitmentHash, version.NativeHead.Number, finalizedBlockHash)
		if err != nil {
			return FinalFleetGenerationBatchEvidence{}, err
		}
		mirror, err := self.carriedWrite(uint64(fleetID), 0, manifest, version, mirrorData)
		if err != nil {
			return FinalFleetGenerationBatchEvidence{}, err
		}
		batch.CarriedHistory = append(batch.CarriedHistory, mirror)
		for member := uint64(1); member <= finalFleetGenerationMembersPerFleet; member++ {
			bindingData, err := self.initialBindingCalldata(uint64(fleetID), member, manifest)
			if err != nil {
				return FinalFleetGenerationBatchEvidence{}, err
			}
			write, err := self.carriedWrite(uint64(fleetID), member, manifest, version, bindingData)
			if err != nil {
				return FinalFleetGenerationBatchEvidence{}, err
			}
			batch.CarriedHistory = append(batch.CarriedHistory, write)
		}
	}
	if len(installedFleets) == 0 {
		if installed.TransactionHash != "" || installed.CalldataHash != "" || installed.BlockNumber != 0 || installed.BlockHash != "" || installed.EffectiveEpoch != 0 || installed.ValidToEpoch != 0 {
			return FinalFleetGenerationBatchEvidence{}, errors.New("ordinary fleet generation all-carried partition names install transaction fields")
		}
		return batch, nil
	}
	if installed.TransactionHash == "" || installed.CalldataHash == "" || installed.BlockNumber == 0 || installed.BlockHash == "" || installed.EffectiveEpoch == 0 || installed.ValidToEpoch < installed.EffectiveEpoch {
		return FinalFleetGenerationBatchEvidence{}, errors.New("ordinary fleet generation installed partition has incomplete batch evidence")
	}
	calldata, err := self.installCalldata(batch, installedFleets)
	if err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	if !strings.EqualFold(crypto.Keccak256Hash(calldata).Hex(), installed.CalldataHash) {
		return FinalFleetGenerationBatchEvidence{}, errors.New("ordinary fleet generation install calldata hash differs from public evidence")
	}
	write, err := self.evmWrite(batch.Action.ActionID, installed.TransactionHash, installed.BlockNumber, installed.BlockHash, calldata, true)
	if err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	proof, err := self.postconditionProofForWrite(write)
	if err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	batch.Action = write.Action
	batch.CalldataHash, batch.EventHash, batch.CoordinatorRuntimeHash, batch.BatcherRuntimeHash = write.CalldataHash, write.EventHash, write.CoordinatorRuntimeHash, write.BatcherRuntimeHash
	batch.Postcondition, batch.BatchWrite = proof, &write
	return batch, nil
}

// checks the ordered public member-file namespace before source replay opens
// individual binding bytes. The list covers the entire ten-fleet partition,
// including carried fleets, so a mixed batch cannot drop an initial member.
func finalFleetGenerationInstallEvidenceMembers(installed FleetInstallBatchEvidence, batch FinalFleetGenerationBatchEvidence) error {
	want := int(finalFleetGenerationBatchSize * finalFleetGenerationMembersPerFleet)
	if len(installed.MemberEvidence) != want {
		return errors.New("ordinary fleet generation install member evidence count differs")
	}
	index := 0
	for fleetID := batch.FirstFleet; fleetID <= batch.LastFleet; fleetID++ {
		for member := uint64(1); member <= finalFleetGenerationMembersPerFleet; member++ {
			if installed.MemberEvidence[index] != fmt.Sprintf("fleet-%d-member-%d.binding.json", fleetID, member) {
				return errors.New("ordinary fleet generation install member evidence order differs")
			}
			index++
		}
	}
	return nil
}

// converts the archived public partition to fixed-width source coordinates.
// The common verifier rejects an overlap, gap, range escape, or ordering alias
// before the two provenance paths are materialized.
func finalFleetGenerationInstallEvidencePartition(installed FleetInstallBatchEvidence, batch FinalFleetGenerationBatchEvidence) ([]uint64, []uint64, error) {
	installedFleets := make([]uint64, len(installed.InstalledFleets))
	for index, fleetID := range installed.InstalledFleets {
		if fleetID < 1 {
			return nil, nil, errors.New("ordinary fleet generation installed partition has a nonpositive fleet")
		}
		installedFleets[index] = uint64(fleetID)
	}
	carriedFleets := make([]uint64, len(installed.CarriedFleets))
	for index, fleetID := range installed.CarriedFleets {
		if fleetID < 1 {
			return nil, nil, errors.New("ordinary fleet generation carried partition has a nonpositive fleet")
		}
		carriedFleets[index] = uint64(fleetID)
	}
	if err := verifyFinalFleetGenerationFleetPartition(batch, installedFleets, carriedFleets); err != nil {
		return nil, nil, err
	}
	return installedFleets, carriedFleets, nil
}

// decodes a native block hash without allowing a malformed source field to
// become an unrecoverable producer panic.
func finalFleetGenerationHex32(value string) ([32]byte, error) {
	decoded, err := decodeHex32("ordinary fleet generation native block", value)
	if err != nil {
		return [32]byte{}, err
	}
	return decoded, nil
}

// reconstructs the original dual-signature coordinator calldata using only
// public binding evidence, never a private prepared-transaction file.
func (self *finalFleetGenerationSource) initialBindingCalldata(fleetID, memberNumber uint64, manifest protocol.FleetManifest) ([]byte, error) {
	path := fmt.Sprintf("public/fleet-%d-member-%d.binding.json", fleetID, memberNumber)
	data, err := self.record(path)
	if err != nil {
		return nil, err
	}
	var evidence FleetBindingEvidence
	if err := decodeStrictJSONBytes(data, &evidence); err != nil {
		return nil, err
	}
	binding, err := manifest.Binding(manifest.Members[memberNumber-1], evidence.ValidFromEpoch, evidence.ValidToEpoch)
	if err != nil {
		return nil, err
	}
	clientSignature, clientOK := evidenceFixedHex(evidence.ClientSignature, ed25519.SignatureSize)
	hotkeySignature, hotkeyOK := evidenceFixedHex(evidence.HotkeySignature, ed25519.SignatureSize)
	if !clientOK || !hotkeyOK {
		return nil, errors.New("ordinary fleet generation-one calldata signatures are invalid")
	}
	contractBinding := stabi.STCoordinatorFleetBinding{ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: common.BytesToAddress(manifest.Coordinator[:]), FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID, ClientKey: binding.ClientKey, Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: binding.CommitmentHash}
	return stabi.NewSTCoordinator().TryPackBindFleetMember(contractBinding, clientSignature, hotkeySignature)
}

// rebuilds the exact bounded install tuple from public generation-one
// manifests and binding signatures. It deliberately includes only the source
// evidence's installed subset, so a mixed batch cannot quietly install a
// carried fleet or omit a newly installed one.
func (self *finalFleetGenerationSource) installCalldata(batch FinalFleetGenerationBatchEvidence, installedFleets []uint64) ([]byte, error) {
	if self == nil || len(installedFleets) == 0 || len(installedFleets) > int(finalFleetGenerationBatchSize) {
		return nil, errors.New("ordinary fleet generation install calldata is unavailable")
	}
	parsed, err := abi.JSON(strings.NewReader(FleetBatcherABI))
	if err != nil {
		return nil, err
	}
	fleets := make([]fleetBatcherFleetRefresh, 0, len(installedFleets))
	for _, fleetID := range installedFleets {
		version, manifest, versionErr := self.version(fleetID, 1, batch.Batch)
		if versionErr != nil {
			return nil, versionErr
		}
		commitmentHash, hashErr := decodeHex32("ordinary fleet generation install commitment", version.CommitmentHash)
		if hashErr != nil {
			return nil, hashErr
		}
		finalizedBlockHash, blockErr := finalFleetGenerationHex32(version.NativeHead.Hash)
		if blockErr != nil {
			return nil, blockErr
		}
		fleet := fleetBatcherFleetRefresh{Hotkey: manifest.Hotkey, CommitmentHash: commitmentHash, FinalizedBlock: version.NativeHead.Number, FinalizedBlockHash: finalizedBlockHash}
		for member := uint64(1); member <= finalFleetGenerationMembersPerFleet; member++ {
			path := fmt.Sprintf("public/fleet-%d-member-%d.binding.json", fleetID, member)
			data, readErr := self.record(path)
			if readErr != nil {
				return nil, readErr
			}
			var memberEvidence FleetBindingEvidence
			if decodeErr := decodeStrictJSONBytes(data, &memberEvidence); decodeErr != nil {
				return nil, decodeErr
			}
			binding, bindingErr := manifest.Binding(manifest.Members[member-1], memberEvidence.ValidFromEpoch, memberEvidence.ValidToEpoch)
			if bindingErr != nil || binding.Generation != 1 || binding.Hotkey != manifest.Hotkey || binding.CommitmentHash != commitmentHash {
				return nil, stateMismatchError(bindingErr, "ordinary fleet generation install member binding differs")
			}
			clientSignature, clientOK := evidenceFixedHex(memberEvidence.ClientSignature, ed25519.SignatureSize)
			hotkeySignature, hotkeyOK := evidenceFixedHex(memberEvidence.HotkeySignature, ed25519.SignatureSize)
			if !clientOK || !hotkeyOK || !binding.VerifyClient(clientSignature) || !binding.VerifyHotkey(hotkeySignature) {
				return nil, errors.New("ordinary fleet generation install member signatures are invalid")
			}
			contractBinding := stabi.STCoordinatorFleetBinding{ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: common.BytesToAddress(manifest.Coordinator[:]), FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID, ClientKey: binding.ClientKey, Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: binding.CommitmentHash}
			fleet.Members = append(fleet.Members, fleetBatcherMemberRefresh{Binding: contractBinding, ClientSignature: clientSignature, HotkeySignature: hotkeySignature})
		}
		fleets = append(fleets, fleet)
	}
	return parsed.Pack("install", fleets)
}

// derives one carried coordinator receipt plus its exact finality boundaries.
func (self *finalFleetGenerationSource) carriedWrite(fleetID, memberNumber uint64, manifest protocol.FleetManifest, version FinalFleetGenerationVersionEvidence, calldata []byte) (FinalFleetGenerationWriteEvidence, error) {
	actionID := fmt.Sprintf("fleet.mirror.%d", fleetID)
	if memberNumber != 0 {
		actionID = fmt.Sprintf("fleet.bind.%d.%d", fleetID, memberNumber)
	}
	var expectedTx string
	var expectedBlock uint64
	var expectedHash string
	if memberNumber != 0 {
		path := fmt.Sprintf("public/fleet-%d-member-%d.binding.json", fleetID, memberNumber)
		data, err := self.record(path)
		if err != nil {
			return FinalFleetGenerationWriteEvidence{}, err
		}
		var binding FleetBindingEvidence
		if err := decodeStrictJSONBytes(data, &binding); err != nil {
			return FinalFleetGenerationWriteEvidence{}, err
		}
		expectedTx, expectedBlock, expectedHash = binding.TransactionHash, binding.BlockNumber, binding.BlockHash
	} else {
		entry, err := self.findMirrorMutation(actionID, version.CommitmentHash)
		if err != nil {
			return FinalFleetGenerationWriteEvidence{}, err
		}
		expectedTx, expectedBlock, expectedHash = entry.TransactionHash, entry.BlockNumber, entry.BlockHash
	}
	return self.evmWrite(actionID, expectedTx, expectedBlock, expectedHash, calldata, false)
}

// finds the unique historical mirror action whose verified observed state
// names the exact generation commitment. This avoids selecting a same-ID
// retry that mirrored an earlier or later native record.
func (self *finalFleetGenerationSource) findMirrorMutation(actionID, commitmentHash string) (JournalEntry, error) {
	if self == nil {
		return JournalEntry{}, errors.New("ordinary fleet generation mirror source is unavailable")
	}
	allowed := self.current.allowedPlanHashes()
	var found *JournalEntry
	for _, verified := range self.entries {
		if verified.Stage != StageVerified || verified.ActionID != actionID || !allowed[verified.PlanHash] {
			continue
		}
		post, _, err := self.verifiedPostcondition(verified)
		if err != nil {
			return JournalEntry{}, err
		}
		observed, ok := post.Observed["commitment_hash"].(string)
		if !ok || !strings.EqualFold(observed, commitmentHash) {
			continue
		}
		for index := range self.entries {
			candidate := &self.entries[index]
			if candidate.Stage != StageFinalized || candidate.PlanHash != verified.PlanHash || candidate.ActionID != verified.ActionID || candidate.IntentHash != verified.IntentHash {
				continue
			}
			if found != nil {
				return JournalEntry{}, fmt.Errorf("ordinary fleet generation mirror %s has multiple exact finalizations", actionID)
			}
			copy := *candidate
			found = &copy
		}
	}
	if found == nil {
		return JournalEntry{}, fmt.Errorf("ordinary fleet generation mirror %s has no commitment-matched finalized mutation", actionID)
	}
	return *found, nil
}

// reconstructs the atomic generation-two helper calldata, validates its
// public batch proof, and retains all receipt logs as an ordered projection.
func (self *finalFleetGenerationSource) refreshBatch(batch FinalFleetGenerationBatchEvidence) (FinalFleetGenerationBatchEvidence, error) {
	path := fmt.Sprintf("public/fleet-refresh-batch-%d.json", batch.Batch)
	data, err := self.record(path)
	if err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	var refreshed FleetRefreshBatchEvidence
	if err := decodeStrictJSONBytes(data, &refreshed); err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	if refreshed.Schema != fleetRefreshBatchEvidenceSchema || refreshed.Batch != int(batch.Batch) || refreshed.FirstFleet != int(batch.FirstFleet) || refreshed.LastFleet != int(batch.LastFleet) || refreshed.Generation != 2 || refreshed.FleetCount != int(finalFleetGenerationBatchSize) || refreshed.MemberCount != int(finalFleetGenerationBatchSize*finalFleetGenerationMembersPerFleet) || len(refreshed.MemberEvidence) != refreshed.MemberCount || refreshed.TransactionHash == "" || refreshed.BlockNumber == 0 || refreshed.BlockHash == "" {
		return FinalFleetGenerationBatchEvidence{}, errors.New("ordinary fleet generation-two batch evidence is incomplete")
	}
	calldata, err := self.refreshCalldata(batch, refreshed)
	if err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	if !strings.EqualFold(crypto.Keccak256Hash(calldata).Hex(), refreshed.CalldataHash) {
		return FinalFleetGenerationBatchEvidence{}, errors.New("ordinary fleet generation-two batch calldata hash differs from public evidence")
	}
	write, err := self.evmWrite(batch.Action.ActionID, refreshed.TransactionHash, refreshed.BlockNumber, refreshed.BlockHash, calldata, true)
	if err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	proof, err := self.postconditionProofForWrite(write)
	if err != nil {
		return FinalFleetGenerationBatchEvidence{}, err
	}
	write.Postcondition = proof
	batch.Action = write.Action
	batch.CalldataHash, batch.EventHash, batch.CoordinatorRuntimeHash, batch.BatcherRuntimeHash = write.CalldataHash, write.EventHash, write.CoordinatorRuntimeHash, write.BatcherRuntimeHash
	batch.Postcondition, batch.BatchWrite = proof, &write
	return batch, nil
}

// rebuilds the exact ABI tuple passed to the bounded batcher from public
// signed successor/revoke evidence. The retained data never depends on seeds.
func (self *finalFleetGenerationSource) refreshCalldata(batch FinalFleetGenerationBatchEvidence, refreshed FleetRefreshBatchEvidence) ([]byte, error) {
	if self == nil {
		return nil, errors.New("ordinary fleet generation refresh source is unavailable")
	}
	parsed, err := abi.JSON(strings.NewReader(FleetBatcherABI))
	if err != nil {
		return nil, err
	}
	fleets := make([]fleetBatcherFleetRefresh, 0, finalFleetGenerationBatchSize)
	expectedMemberPath := 0
	for fleetID := batch.FirstFleet; fleetID <= batch.LastFleet; fleetID++ {
		version, manifest, err := self.version(fleetID, 2, batch.Batch)
		if err != nil {
			return nil, err
		}
		commitmentHash, err := decodeHex32("ordinary fleet generation refresh commitment", version.CommitmentHash)
		if err != nil {
			return nil, err
		}
		finalizedBlockHash, hashErr := finalFleetGenerationHex32(version.NativeHead.Hash)
		if hashErr != nil {
			return nil, hashErr
		}
		fleet := fleetBatcherFleetRefresh{Hotkey: manifest.Hotkey, CommitmentHash: commitmentHash, FinalizedBlock: version.NativeHead.Number, FinalizedBlockHash: finalizedBlockHash}
		for member := uint64(1); member <= finalFleetGenerationMembersPerFleet; member++ {
			path := fmt.Sprintf("fleet-%d-member-%d.refresh.binding.json", fleetID, member)
			if expectedMemberPath >= len(refreshed.MemberEvidence) || refreshed.MemberEvidence[expectedMemberPath] != path {
				return nil, errors.New("ordinary fleet generation refresh member evidence ordering differs")
			}
			expectedMemberPath++
			data, err := self.record("public/" + path)
			if err != nil {
				return nil, err
			}
			var memberEvidence FleetRefreshBindingEvidence
			if err := decodeStrictJSONBytes(data, &memberEvidence); err != nil {
				return nil, err
			}
			binding, _, err := fleetRefreshEvidenceBindings(memberEvidence, common.HexToAddress(self.evidence.Deployment.CoordinatorProxy), self.evidence.ChainID, self.evidence.Netuid)
			if err != nil {
				return nil, err
			}
			clientSignature, clientOK := evidenceFixedHex(memberEvidence.ClientSignature, ed25519.SignatureSize)
			hotkeySignature, hotkeyOK := evidenceFixedHex(memberEvidence.HotkeySignature, ed25519.SignatureSize)
			revokeSignature, revokeOK := evidenceFixedHex(memberEvidence.RevokeSignature, ed25519.SignatureSize)
			if !clientOK || !hotkeyOK || !revokeOK {
				return nil, errors.New("ordinary fleet generation refresh signatures are invalid")
			}
			contractBinding := stabi.STCoordinatorFleetBinding{ChainId: binding.ChainID, Netuid: binding.Netuid, Coordinator: common.HexToAddress(self.evidence.Deployment.CoordinatorProxy), FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientId: binding.ClientID, ClientKey: binding.ClientKey, Generation: binding.Generation, ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, CommitmentHash: binding.CommitmentHash}
			fleet.Members = append(fleet.Members, fleetBatcherMemberRefresh{PriorGeneration: memberEvidence.PriorGeneration, Binding: contractBinding, RevokeSignature: revokeSignature, ClientSignature: clientSignature, HotkeySignature: hotkeySignature})
		}
		fleets = append(fleets, fleet)
	}
	if expectedMemberPath != len(refreshed.MemberEvidence) {
		return nil, errors.New("ordinary fleet generation refresh member evidence has excess paths")
	}
	return parsed.Pack("refresh", fleets)
}

// projects one exact EVM receipt and all of its captured release logs from a
// selected journal mutation. Helper and coordinator runtime roots are bound
// independently so an upgrade at a historical write head cannot be hidden.
func (self *finalFleetGenerationSource) evmWrite(actionID, transactionHash string, block uint64, blockHash string, calldata []byte, requireBatcher bool) (FinalFleetGenerationWriteEvidence, error) {
	action, finalized, post, postData, err := self.verifiedMutation(actionID, strings.ToLower(transactionHash), block, strings.ToLower(blockHash), nil)
	if err != nil {
		return FinalFleetGenerationWriteEvidence{}, err
	}
	if action.Kind != "evm-transaction" || len(calldata) < 4 {
		return FinalFleetGenerationWriteEvidence{}, errors.New("ordinary fleet generation EVM action or calldata is invalid")
	}
	logs := self.events.byTx[strings.ToLower(transactionHash)]
	if len(logs) == 0 {
		return FinalFleetGenerationWriteEvidence{}, fmt.Errorf("ordinary fleet generation receipt %s has no captured release logs", transactionHash)
	}
	receipt, err := self.archive.receiptFromIndex(self.events, finalSemanticEvent{Log: logs[0]}, filepath.ToSlash(filepath.Join("fleet-generation", "receipts", stringsTrim0x(finalized.PlanHash), actionID)))
	if err != nil || !strings.EqualFold(receipt.TransactionHash, transactionHash) || receipt.Block.Number != block || !strings.EqualFold(receipt.Block.Hash, blockHash) {
		return FinalFleetGenerationWriteEvidence{}, stateMismatchError(err, "ordinary fleet generation receipt differs from finalized journal mutation")
	}
	runtime, err := self.writeRuntime(finalized.PlanHash, requireBatcher)
	if err != nil {
		return FinalFleetGenerationWriteEvidence{}, err
	}
	if requireBatcher {
		plan, planErr := self.recordPlan(finalized.PlanHash)
		if planErr != nil {
			return FinalFleetGenerationWriteEvidence{}, planErr
		}
		batcher, _, batcherErr := finalPlanFleetBatcher(plan)
		if batcherErr != nil {
			return FinalFleetGenerationWriteEvidence{}, batcherErr
		}
		if err := verifyFinalFleetGenerationCurrentBatchTarget(action, batcher); err != nil {
			return FinalFleetGenerationWriteEvidence{}, err
		}
	}
	events := make([]FinalFleetGenerationEventEvidence, 0, len(logs))
	for _, log := range logs {
		decoded, decodeErr := finalFleetGenerationDecodeEventForBatcher(self.evidence, actionID, runtime.batcherAddress, log)
		if decodeErr != nil {
			return FinalFleetGenerationWriteEvidence{}, fmt.Errorf("ordinary fleet generation receipt %s event %d: %w", transactionHash, len(events), decodeErr)
		}
		events = append(events, decoded.Evidence)
	}
	eventHash, err := canonicalHashHex(events)
	if err != nil {
		return FinalFleetGenerationWriteEvidence{}, err
	}
	result := FinalFleetGenerationWriteEvidence{
		Action: FinalFleetGenerationActionEvidence{ActionID: actionID, PlanHash: strings.ToLower(finalized.PlanHash), IntentHash: strings.ToLower(finalized.IntentHash)}, Receipt: receipt,
		Calldata: "0x" + hex.EncodeToString(calldata), CalldataHash: strings.ToLower(crypto.Keccak256Hash(calldata).Hex()), EventHash: eventHash, Events: events,
		CoordinatorProxy: runtime.coordinatorProxy, CoordinatorImplementation: runtime.coordinatorImplementation, CoordinatorImplementationSlot: runtime.implementationSlot,
		CoordinatorProxyRuntimeHash: runtime.proxyRuntimeHash, CoordinatorRuntimeHash: runtime.implementationRuntimeHash, BatcherAddress: runtime.batcherAddress, BatcherRuntimeHash: runtime.batcherRuntimeHash,
		EVMHead: post.EVMFinalized, NativeHead: post.SubstrateFinalized,
	}
	proof, err := self.postconditionProof(finalized, postData)
	if err != nil {
		return FinalFleetGenerationWriteEvidence{}, err
	}
	result.Postcondition = proof
	return result, nil
}

// Rejects a direct installed or refreshed batch call when its approving plan
// does not name the exact helper whose calldata and receipt are retained.
// Individual carried actions deliberately do not use this rule because their
// historical plans may name logical fleet targets while their authenticated
// receipt names the predecessor coordinator proxy.
func verifyFinalFleetGenerationCurrentBatchTarget(action Action, batcher common.Address) error {
	if action.Kind != "evm-transaction" || batcher == (common.Address{}) || !common.IsHexAddress(action.Target) || !strings.EqualFold(action.Target, batcher.Hex()) {
		return errors.New("ordinary fleet generation current batch action target differs from the approved batcher")
	}
	return nil
}

// Holds the exact executable identities expected at one generation-write
// head. A predecessor transaction is intentionally anchored to its approved
// plan baseline instead of a later release binary that did not yet exist.
type finalFleetGenerationWriteRuntime struct {
	coordinatorProxy          string
	coordinatorImplementation string
	implementationSlot        string
	proxyRuntimeHash          string
	implementationRuntimeHash string
	batcherAddress            string
	batcherRuntimeHash        string
}

// Derives the only allowed proxy dispatch identity from the plan which
// authorized the mutation. Refreshes are current-plan writes and therefore
// require the current batcher; carried writes require a fully authenticated
// repeated-upgrade baseline and reject an invented later executable census.
func (self *finalFleetGenerationSource) writeRuntime(planHash string, requireBatcher bool) (finalFleetGenerationWriteRuntime, error) {
	if self == nil || self.evidence == nil || planHash == "" {
		return finalFleetGenerationWriteRuntime{}, errors.New("ordinary fleet generation runtime source is unavailable")
	}
	plan, err := self.recordPlan(planHash)
	if err != nil {
		return finalFleetGenerationWriteRuntime{}, err
	}
	current := strings.EqualFold(plan.PlanHash, self.current.PlanHash)
	if current {
		proxy, proxyErr := finalReleaseRuntimeRootByName(self.evidence, "coordinator_proxy")
		implementation, implementationErr := finalReleaseRuntimeRootByName(self.evidence, "coordinator_upgrade_implementation")
		if proxyErr != nil || implementationErr != nil {
			return finalFleetGenerationWriteRuntime{}, stateMismatchError(errors.Join(proxyErr, implementationErr), "ordinary fleet generation current runtime roots are unavailable")
		}
		result := finalFleetGenerationWriteRuntime{
			coordinatorProxy: strings.ToLower(proxy.Address), coordinatorImplementation: strings.ToLower(implementation.Address),
			implementationSlot: finalFleetGenerationImplementationSlot(implementation.Address), proxyRuntimeHash: strings.ToLower(proxy.RuntimeCodeHash), implementationRuntimeHash: strings.ToLower(implementation.RuntimeCodeHash),
		}
		if requireBatcher {
			batcher, batcherErr := finalReleaseRuntimeRootByName(self.evidence, "fleet_batcher")
			if batcherErr != nil {
				return finalFleetGenerationWriteRuntime{}, batcherErr
			}
			approved, _, approvedErr := finalPlanFleetBatcher(plan)
			if approvedErr != nil || !strings.EqualFold(approved.Hex(), batcher.Address) {
				return finalFleetGenerationWriteRuntime{}, stateMismatchError(approvedErr, "ordinary fleet generation current batcher root differs from approved plan")
			}
			result.batcherAddress, result.batcherRuntimeHash = strings.ToLower(batcher.Address), strings.ToLower(batcher.RuntimeCodeHash)
		}
		return result, nil
	}
	baseline := plan.CoordinatorUpgradeBaseline
	if !baseline.isRepeated() || plan.Deployment.CoordinatorProxy == (common.Address{}) || !common.IsHexAddress(baseline.ActiveImplementation) || common.HexToAddress(baseline.ActiveImplementation) == (common.Address{}) {
		return finalFleetGenerationWriteRuntime{}, errors.New("ordinary fleet generation carried plan has no authenticated active coordinator baseline")
	}
	if !strings.EqualFold(plan.Deployment.CoordinatorProxy.Hex(), self.evidence.Deployment.CoordinatorProxy) {
		return finalFleetGenerationWriteRuntime{}, errors.New("ordinary fleet generation carried plan changes the coordinator proxy")
	}
	for _, field := range []struct {
		label string
		value string
	}{
		{label: "ordinary fleet generation carried proxy runtime", value: baseline.CoordinatorProxyExecutableHash},
		{label: "ordinary fleet generation carried implementation runtime", value: baseline.ActiveImplementationHash},
	} {
		if err := requireFinalHex32(field.label, field.value); err != nil {
			return finalFleetGenerationWriteRuntime{}, err
		}
	}
	result := finalFleetGenerationWriteRuntime{
		coordinatorProxy: strings.ToLower(plan.Deployment.CoordinatorProxy.Hex()), coordinatorImplementation: strings.ToLower(common.HexToAddress(baseline.ActiveImplementation).Hex()),
		implementationSlot: finalFleetGenerationImplementationSlot(baseline.ActiveImplementation), proxyRuntimeHash: strings.ToLower(baseline.CoordinatorProxyExecutableHash), implementationRuntimeHash: strings.ToLower(baseline.ActiveImplementationHash),
	}
	if requireBatcher {
		batcher, batcherHash, batcherErr := finalPlanFleetBatcher(plan)
		if batcherErr != nil {
			return finalFleetGenerationWriteRuntime{}, batcherErr
		}
		result.batcherAddress, result.batcherRuntimeHash = strings.ToLower(batcher.Hex()), strings.ToLower(batcherHash)
	}
	return result, nil
}

// Encodes the exact ERC1967 slot value expected for a selected implementation
// address. The slot has a 12-byte zero prefix and remains a full bytes32 in
// evidence so an RPC response cannot discard high-order data.
func finalFleetGenerationImplementationSlot(address string) string {
	return "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(strings.ToLower(common.HexToAddress(address).Hex()), "0x")
}

// returns the already authenticated postcondition locator carried by a write.
func (self *finalFleetGenerationSource) postconditionProofForWrite(write FinalFleetGenerationWriteEvidence) (FinalArtifactLocator, error) {
	if self == nil || write.Postcondition == (FinalArtifactLocator{}) {
		return FinalArtifactLocator{}, errors.New("ordinary fleet generation write postcondition is unavailable")
	}
	return write.Postcondition, nil
}

// binds the two challenger-only initial registrations to their already sealed
// terminal tournament transition artifacts. Challengers deliberately have no
// generation-two batch row.
func (self *finalFleetGenerationSource) challenger(fleetID uint64) (FinalNativeReceipt, FinalArtifactLocator, error) {
	if self == nil || fleetID < finalFleetGenerationSetupFleetCount+1 || fleetID > finalFleetGenerationSetupFleetCount+finalFleetGenerationChallengerFleetCount {
		return FinalNativeReceipt{}, FinalArtifactLocator{}, errors.New("ordinary fleet generation challenger identity is invalid")
	}
	registration, postcondition, err := self.archive.nativeActionReceipt(fmt.Sprintf("fleet.register.%d", fleetID), fmt.Sprintf("fleet-generation-challenger-%d-registration", fleetID))
	if err != nil {
		return FinalNativeReceipt{}, FinalArtifactLocator{}, err
	}
	// The shared native builder authenticates through archive.files. Retain
	// that exact input here as well so the sealed source can replay it alone.
	path, err := postconditionRelativePath(postcondition.PlanHash, postcondition.ActionID)
	if err != nil {
		return FinalNativeReceipt{}, FinalArtifactLocator{}, err
	}
	if _, err := self.record(path); err != nil {
		return FinalNativeReceipt{}, FinalArtifactLocator{}, err
	}
	for _, transition := range self.evidence.HeadTransitions {
		if transition.ChallengerFleetID == fleetID {
			return registration, transition.Artifact, nil
		}
	}
	return FinalNativeReceipt{}, FinalArtifactLocator{}, fmt.Errorf("ordinary fleet generation challenger %d has no tournament transition", fleetID)
}
