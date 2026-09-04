package main

// This file turns the release fleet-lifecycle exercise into immutable final
// semantic evidence.  The lifecycle producer writes several public files over
// time; accepting only the terminal summary would let a coherent loose file
// conceal a missing, swapped, or unapproved mutation.  The semantic source
// therefore carries a typed replay index and one content-addressed artifact
// containing the exact captured bytes of the complete plan/journal/artifact
// graph.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

type finalLifecycleAppendExchanges func(string, ChainHead, []FinalRPCExchange) error

const finalFleetLifecycleLineageSchema = "urnetwork-final-fleet-lifecycle-lineage-v1"

type FinalFleetLifecycleRole struct {
	Label     string `json:"label"`
	PublicKey string `json:"public_key"`
	SS58      string `json:"ss58"`
}

type FinalFleetLifecycleMember struct {
	ClientID  string `json:"client_id"`
	ClientKey string `json:"client_key"`
}

type FinalFleetLifecycleRegistrationPreparation struct {
	Schema       string                      `json:"schema"`
	PlanHash     string                      `json:"plan_hash"`
	ActionID     string                      `json:"action_id"`
	IntentHash   string                      `json:"intent_hash"`
	VictimFleet  int                         `json:"victim_fleet"`
	VictimHotkey string                      `json:"victim_hotkey"`
	Snapshot     FleetLifecyclePruneSnapshot `json:"snapshot"`
}

// FinalFleetLifecycleVariantEvidence is the exact mutation/replay index for a
// single installed generation. ManifestHash binds the canonical manifest bytes
// retained in LineageArtifact; the decoded identities are repeated here so a
// public reader can replay historical state without loading loose files.
type FinalFleetLifecycleVariantEvidence struct {
	Name                    string                                      `json:"name"`
	FleetID                 string                                      `json:"fleet_id"`
	Hotkey                  string                                      `json:"hotkey"`
	Generation              uint64                                      `json:"generation"`
	ManifestHash            string                                      `json:"manifest_sha256"`
	Members                 []FinalFleetLifecycleMember                 `json:"members"`
	Commitment              FleetCommitmentEvidence                     `json:"commitment"`
	Mirror                  FleetLifecycleMirrorEvidence                `json:"mirror"`
	Bindings                []FleetBindingEvidence                      `json:"bindings"`
	RegistrationPreparation *FinalFleetLifecycleRegistrationPreparation `json:"registration_preparation,omitempty"`
	Registration            *FleetLifecycleRegistrationEvidence         `json:"registration,omitempty"`
	Cleanups                []FleetLifecycleCleanupEvidence             `json:"cleanups,omitempty"`
}

type FinalFleetLifecycleEvidence struct {
	ClientsPerHeadFleet int                                  `json:"clients_per_head_fleet"`
	ReleaseHandoffHash  string                               `json:"release_handoff_hash"`
	ReleaseHandoffSize  uint64                               `json:"release_handoff_size"`
	Roles               []FinalFleetLifecycleRole            `json:"roles"`
	State               FleetLifecycleEvidence               `json:"state"`
	Variants            []FinalFleetLifecycleVariantEvidence `json:"variants"`
	PayoutArtifacts     []FinalFleetLifecyclePayoutArtifact  `json:"payout_artifacts"`
	AppliedDecisions    []FinalFleetLifecycleAppliedDecision `json:"applied_decisions"`
	LineageArtifact     FinalArtifactLocator                 `json:"lineage_artifact"`
}

type FinalFleetLifecyclePayoutArtifact struct {
	Epoch    uint64               `json:"epoch"`
	NoID     uint64               `json:"no_id"`
	Root     FinalEVMReceipt      `json:"root_receipt"`
	Artifact FinalArtifactLocator `json:"artifact"`
}

// FinalFleetLifecycleAppliedDecision binds every lifecycle census row to the
// exact validator-signed intent, independently replayable measurement, and
// owner-pinned measurement envelope captured for that row.  CensusIndex is an
// index into State.CandidateCensuses; it deliberately does not collapse the
// independent settlement and native epoch domains into one synthetic epoch.
type FinalFleetLifecycleAppliedDecision struct {
	CensusIndex     uint64               `json:"census_index"`
	ValidatorID     uint64               `json:"validator_id"`
	SettlementEpoch uint64               `json:"settlement_epoch"`
	SubnetEpoch     uint64               `json:"subnet_epoch"`
	VectorHash      string               `json:"vector_hash"`
	Intent          FinalArtifactLocator `json:"intent"`
	Measurement     FinalArtifactLocator `json:"measurement"`
	Envelope        FinalArtifactLocator `json:"measurement_envelope"`
}

type finalFleetLifecycleLineageFile struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_sha256"`
	SizeBytes   uint64 `json:"size_bytes"`
	Data        []byte `json:"data"`
}

type finalFleetLifecycleLineageArtifact struct {
	Schema       string                           `json:"schema"`
	DeploymentID string                           `json:"deployment_id"`
	PlanHash     string                           `json:"plan_hash"`
	RunID        string                           `json:"run_id"`
	Files        []finalFleetLifecycleLineageFile `json:"files"`
}

func finalFleetLifecycleVariantNames() []string {
	return []string{
		fleetLifecycleVariantTargetTakeover,
		fleetLifecycleVariantCompanionTakeover,
		fleetLifecycleVariantFallback,
		fleetLifecycleVariantProvider,
		fleetLifecycleVariantTerminal,
	}
}

func canonicalizeFinalFleetLifecycleVariants(variants []FinalFleetLifecycleVariantEvidence) {
	sort.Slice(variants, func(i, j int) bool { return variants[i].Name < variants[j].Name })
	for index := range variants {
		variant := &variants[index]
		sort.Slice(variant.Members, func(i, j int) bool { return variant.Members[i].ClientID < variant.Members[j].ClientID })
		sort.Slice(variant.Bindings, func(i, j int) bool { return variant.Bindings[i].ClientID < variant.Bindings[j].ClientID })
		sort.Slice(variant.Cleanups, func(i, j int) bool { return variant.Cleanups[i].ClientID < variant.Cleanups[j].ClientID })
	}
}

func finalFleetLifecycleExpectedPaths(clients int) ([]string, error) {
	if clients < 1 || clients > 64 {
		return nil, fmt.Errorf("fleet lifecycle clients per head fleet=%d is invalid", clients)
	}
	paths := []string{
		"launch-foundation/plan.json",
		"launch-foundation/journal.jsonl",
		"public/identities.json",
		"public/fleet-lifecycle.json",
	}
	for _, name := range finalFleetLifecycleVariantNames() {
		variant, err := fleetLifecycleVariantFor(name)
		if err != nil {
			return nil, err
		}
		paths = append(paths,
			"public/"+variant.ManifestName,
			"public/"+variant.CommitmentName,
			"public/"+fleetLifecycleMirrorEvidenceName(name),
		)
		for member := 1; member <= clients; member++ {
			paths = append(paths, "public/"+variant.BindingName(member))
		}
	}
	for _, name := range []string{fleetLifecycleVariantFallback, fleetLifecycleVariantProvider, fleetLifecycleVariantTerminal} {
		pre, registration := fleetLifecycleRegistrationNames(name)
		paths = append(paths, "public/"+pre, "public/"+registration)
	}
	for _, name := range []string{fleetLifecycleVariantTargetTakeover, fleetLifecycleVariantCompanionTakeover, fleetLifecycleVariantFallback} {
		for member := 1; member <= clients; member++ {
			paths = append(paths, "public/"+fleetLifecycleCleanupEvidenceName(name, member))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func finalFleetLifecycleRoles(identities *finalPublicIdentities) ([]FinalFleetLifecycleRole, error) {
	if identities == nil {
		return nil, errors.New("fleet lifecycle public identities are unavailable")
	}
	labels := make([]string, 0, 16)
	for churn := 1; churn <= fleetLifecycleTerminalVictimChurn; churn++ {
		labels = append(labels, churnHotkeyLabel(churn), churnColdkeyLabel(churn))
	}
	result := make([]FinalFleetLifecycleRole, 0, len(labels))
	for _, label := range labels {
		identity, ok := identities.Substrate[label]
		if !ok {
			return nil, fmt.Errorf("fleet lifecycle public identity %s is absent", label)
		}
		key, err := decodeHex32("fleet lifecycle "+label, strings.ToLower(identity.PublicKey))
		if err != nil || identity.SS58 == "" {
			return nil, stateMismatchError(err, "fleet lifecycle public identity %s is invalid", label)
		}
		result = append(result, FinalFleetLifecycleRole{Label: label, PublicKey: fleetLifecycleHex(key), SS58: identity.SS58})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Label < result[j].Label })
	return result, nil
}

func finalFleetLifecycleRoleSecrets(identities *finalPublicIdentities) (*RoleSecrets, error) {
	if identities == nil {
		return nil, errors.New("fleet lifecycle identity graph is unavailable")
	}
	roles := &RoleSecrets{
		Schema: identities.Schema, DeploymentID: identities.DeploymentID,
		Substrate: map[string]SubstrateRoleSecret{}, EVM: map[string]EVMRoleSecret{}, Clients: map[string]ClientRoleSecret{},
	}
	for label, identity := range identities.Substrate {
		key, err := decodeHex32("fleet lifecycle role "+label, strings.ToLower(identity.PublicKey))
		if err != nil {
			return nil, err
		}
		roles.Substrate[label] = SubstrateRoleSecret{Label: label, PublicKeyHex: hex.EncodeToString(key[:]), SS58: identity.SS58}
	}
	for label, identity := range identities.Clients {
		clientID, ok := evidenceFixedHex(strings.ToLower(identity.ClientID), 16)
		clientKey, keyOK := evidenceFixedHex(strings.ToLower(identity.ClientKey), ed25519.PublicKeySize)
		if !ok || !keyOK {
			return nil, fmt.Errorf("fleet lifecycle client identity %s is invalid", label)
		}
		roles.Clients[label] = ClientRoleSecret{Label: label, ClientIDHex: hex.EncodeToString(clientID), PublicKeyHex: hex.EncodeToString(clientKey)}
	}
	for label, address := range identities.EVM {
		if !common.IsHexAddress(address) {
			return nil, fmt.Errorf("fleet lifecycle EVM identity %s is invalid", label)
		}
		roles.EVM[label] = EVMRoleSecret{Label: label, Address: strings.ToLower(common.HexToAddress(address).Hex())}
	}
	return roles, nil
}

func finalFleetLifecyclePlanAction(plan *SetupPlan, actionID string) (Action, error) {
	if plan == nil || actionID == "" {
		return Action{}, errors.New("fleet lifecycle plan action identity is incomplete")
	}
	var found *Action
	for index := range plan.Actions {
		if plan.Actions[index].ID != actionID {
			continue
		}
		if found != nil {
			return Action{}, fmt.Errorf("fleet lifecycle plan duplicates action %s", actionID)
		}
		copy := plan.Actions[index]
		found = &copy
	}
	if found == nil {
		return Action{}, fmt.Errorf("fleet lifecycle plan lacks action %s", actionID)
	}
	return *found, nil
}

func finalFleetLifecycleJournalTransaction(entries []JournalEntry, planHash string, action Action) (JournalEntry, error) {
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.PlanHash == planHash && entry.ActionID == action.ID && entry.IntentHash == action.IntentHash && entry.TransactionHash != "" {
			return entry, nil
		}
	}
	return JournalEntry{}, fmt.Errorf("fleet lifecycle journal lacks transaction for %s", action.ID)
}

func decodeFinalSemanticJournalBytes(data []byte) ([]JournalEntry, error) {
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	if len(lines) == 0 || len(lines) == 1 && len(lines[0]) == 0 {
		return nil, errors.New("captured setup journal is empty")
	}
	entries := make([]JournalEntry, 0, len(lines))
	previous := ""
	for index, line := range lines {
		var entry JournalEntry
		if err := decodeStrictJSONBytes(line, &entry); err != nil {
			return nil, fmt.Errorf("decode captured journal line %d: %w", index+1, err)
		}
		want := entry.EntryHash
		entry.EntryHash = ""
		got, err := canonicalHashHex(entry)
		if err != nil {
			return nil, err
		}
		if want == "" || want != got || entry.PreviousHash != previous || entry.Sequence != uint64(index+1) {
			return nil, fmt.Errorf("captured journal line %d failed hash-chain validation", index+1)
		}
		if err := (&Journal{}).validateEntry(entry); err != nil {
			return nil, fmt.Errorf("captured journal line %d: %w", index+1, err)
		}
		entry.EntryHash = want
		entries = append(entries, entry)
		previous = want
	}
	return entries, nil
}

func finalFleetLifecycleVariantRange(state *FleetLifecycleEvidence, name string) (uint64, uint64, uint64, uint64, error) {
	if state == nil {
		return 0, 0, 0, 0, errors.New("fleet lifecycle state is unavailable")
	}
	switch name {
	case fleetLifecycleVariantTargetTakeover, fleetLifecycleVariantCompanionTakeover:
		if state.AcceptanceStartBlock < 2 {
			return 0, 0, 0, 0, errors.New("fleet lifecycle pre-acceptance range is unavailable")
		}
		return 1, state.AcceptanceStartBlock, state.AcceptanceStartBlock, state.TakeoverEffectiveEpoch, nil
	}
	if state.ReleaseHandoffSchedule == nil || state.ReleaseHandoffSchedule.ApplicationDeadlineBlock < state.AcceptanceTerminalBlock || state.ReleaseEVMEvidenceDeadlineBlock < state.AcceptanceTerminalBlock {
		return 0, 0, 0, 0, errors.New("fleet lifecycle release lineage bounds are unavailable")
	}
	nativeEnd, nativeOK := checkedAdd(state.ReleaseHandoffSchedule.ApplicationDeadlineBlock, 1)
	evmEnd, evmOK := checkedAdd(state.ReleaseEVMEvidenceDeadlineBlock, 1)
	if !nativeOK || !evmOK {
		return 0, 0, 0, 0, errors.New("fleet lifecycle release lineage bounds overflow")
	}
	var predecessor string
	var effective uint64
	switch name {
	case fleetLifecycleVariantFallback:
		predecessor, effective = fleetLifecycleMilestoneTakeoverRejected, state.FallbackEffectiveEpoch
	case fleetLifecycleVariantProvider:
		predecessor, effective = fleetLifecycleMilestoneFallbackActive, state.ProviderEffectiveEpoch
	case fleetLifecycleVariantTerminal:
		predecessor, effective = fleetLifecycleMilestoneProviderActive, state.TerminalEffectiveEpoch
	default:
		return 0, 0, 0, 0, fmt.Errorf("unknown fleet lifecycle variant %q", name)
	}
	start := fleetLifecycleApplicationHead(fleetLifecycleMilestone(state, predecessor))
	if start == 0 || start >= nativeEnd || start >= evmEnd {
		return 0, 0, 0, 0, fmt.Errorf("fleet lifecycle %s predecessor application is outside its signed tail bounds", name)
	}
	return start, nativeEnd, evmEnd, effective, nil
}

func finalFleetLifecycleVariantUID(name string) (uint16, error) {
	switch name {
	case fleetLifecycleVariantTargetTakeover, fleetLifecycleVariantFallback:
		return fleetLifecycleTargetExpectedUID, nil
	case fleetLifecycleVariantCompanionTakeover:
		return fleetLifecycleCompanionExpectedUID, nil
	case fleetLifecycleVariantProvider:
		return fleetLifecycleCompanionExpectedUID, nil
	case fleetLifecycleVariantTerminal:
		return fleetLifecycleTerminalVictimUID, nil
	default:
		return 0, fmt.Errorf("unknown fleet lifecycle variant %q", name)
	}
}

func (a *finalSemanticArchive) buildFleetLifecycle(source *FinalSemanticEvidence, result *ScenarioResult, terminal *ScenarioObservation, identities *finalPublicIdentities, events *finalSemanticEventIndex) error {
	if source == nil || result == nil || terminal == nil || identities == nil || events == nil || a == nil || a.cfg == nil || a.cfg.Config == nil {
		return errors.New("fleet lifecycle semantic construction context is incomplete")
	}
	if source.Phase != "release-1.0" && source.Phase != "production-soak" {
		return fmt.Errorf("phase %q has no fleet lifecycle semantic construction", source.Phase)
	}
	stateBytes, _, err := a.file("public/fleet-lifecycle.json")
	if err != nil {
		return err
	}
	var state FleetLifecycleEvidence
	if err := decodeStrictJSONBytes(stateBytes, &state); err != nil {
		return fmt.Errorf("decode closed fleet lifecycle: %w", err)
	}
	if terminal.FleetLifecycle == nil || !finalJSONEqual(state, *terminal.FleetLifecycle) {
		return errors.New("closed fleet lifecycle differs from the terminal scenario observation")
	}
	planBytes, _, err := a.file("launch-foundation/plan.json")
	if err != nil {
		return err
	}
	plan, err := decodePersistedPlanBytes(planBytes)
	if err != nil {
		return fmt.Errorf("authenticate fleet lifecycle plan: %w", err)
	}
	journalBytes, _, err := a.file("launch-foundation/journal.jsonl")
	if err != nil {
		return err
	}
	entries, err := decodeFinalSemanticJournalBytes(journalBytes)
	if err != nil {
		return err
	}
	roles, err := finalFleetLifecycleRoleSecrets(identities)
	if err != nil {
		return err
	}
	lifecycle := &liveFleetLifecycle{cfg: a.cfg, executor: &Executor{cfg: a.cfg, plan: plan, roles: roles}}
	if err := lifecycle.validatePersistedStateForPhase(source.Phase, result.RunID, &state); err != nil {
		return fmt.Errorf("verify closed fleet lifecycle state: %w", err)
	}
	var handoffHash string
	var handoffSize uint64
	switch source.Phase {
	case "release-1.0":
		if result.LifecycleHandoff == nil || result.PriorRelease != nil || state.Stage != fleetLifecycleStageReleaseHandoff || state.RunID != result.RunID || fleetLifecycleHasProductionState(&state) || state.FirstAcceptedEpoch != source.Window.FirstEpoch || state.AcceptanceStartBlock != source.Window.StartBlock || state.AcceptanceEndBlock != source.Window.EndBlock || state.AcceptanceTerminalBlock != source.Window.TerminalBlock {
			return errors.New("closed fleet lifecycle is not the exact release handoff state")
		}
		handoffHash, handoffSize = bytesSHA256(stateBytes), uint64(len(stateBytes))
		if result.LifecycleHandoff.Schema != scenarioLifecycleHandoffSchema || result.LifecycleHandoff.ReleaseRunID != result.RunID || result.LifecycleHandoff.Stage != fleetLifecycleStageReleaseHandoff || result.LifecycleHandoff.File != scenarioLifecycleHandoffFilename || result.LifecycleHandoff.ContentHash != handoffHash || result.LifecycleHandoff.SizeBytes != handoffSize {
			return errors.New("closed fleet lifecycle differs from the result's authenticated release handoff")
		}
	case "production-soak":
		if result.LifecycleHandoff != nil || result.PriorRelease == nil || source.PriorPhase == nil || a.priorSemantic == nil || a.priorSemantic.FleetLifecycle == nil || state.Stage != fleetLifecycleStageComplete || state.RunID != source.PriorPhase.RunID || state.ProductionRunID != result.RunID || state.ProductionFirstSettlementEpoch != source.Window.FirstEpoch || state.ProductionAcceptanceStartBlock != source.Window.StartBlock || state.ProductionAcceptanceEndBlock != source.Window.EndBlock || state.ProductionAcceptanceTerminalBlock != source.Window.TerminalBlock {
			return errors.New("closed fleet lifecycle is not the exact composite production state")
		}
		handoffHash, handoffSize = a.priorSemantic.FleetLifecycle.ReleaseHandoffHash, a.priorSemantic.FleetLifecycle.ReleaseHandoffSize
		gate := &result.PriorRelease.LifecycleHandoff
		if result.PriorRelease.RunID != state.RunID || result.PriorRelease.ResultHash != source.PriorPhase.ResultHash || gate.ReleaseRunID != state.RunID || gate.Stage != fleetLifecycleStageReleaseHandoff || gate.ContentHash != handoffHash || gate.SizeBytes != handoffSize || state.ReleaseHandoffHash != handoffHash {
			return errors.New("production fleet lifecycle differs from its owner-authenticated release handoff")
		}
	}
	paths, err := finalFleetLifecycleExpectedPaths(a.cfg.Config.Topology.ClientsPerHeadFleet)
	if err != nil {
		return err
	}
	lineage := finalFleetLifecycleLineageArtifact{Schema: finalFleetLifecycleLineageSchema, DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, RunID: source.RunID}
	files := make(map[string][]byte, len(paths))
	for _, name := range paths {
		data, _, err := a.file(name)
		if err != nil {
			return fmt.Errorf("fleet lifecycle lineage %s: %w", name, err)
		}
		files[name] = data
		lineage.Files = append(lineage.Files, finalFleetLifecycleLineageFile{Path: name, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data)), Data: data})
	}
	semantic := &FinalFleetLifecycleEvidence{ClientsPerHeadFleet: a.cfg.Config.Topology.ClientsPerHeadFleet, ReleaseHandoffHash: handoffHash, ReleaseHandoffSize: handoffSize, State: state}
	semantic.Roles, err = finalFleetLifecycleRoles(identities)
	if err != nil {
		return err
	}
	semantic.Variants, err = verifyAndIndexFinalFleetLifecycle(source, semantic, plan, entries, identities, files)
	if err != nil {
		return err
	}
	semantic.PayoutArtifacts, err = a.buildFleetLifecyclePayoutArtifacts(source, semantic, events)
	if err != nil {
		return err
	}
	if source.Phase == "production-soak" {
		if err := verifyFinalFleetLifecycleContinuity(a.priorSemantic.FleetLifecycle, semantic); err != nil {
			return fmt.Errorf("fleet lifecycle release/production continuity: %w", err)
		}
	}
	semantic.LineageArtifact, err = a.derived("fleet-lifecycle-lineage", "fleet-lifecycle-lineage.json", lineage)
	if err != nil {
		return err
	}
	source.FleetLifecycle = semantic
	return nil
}

func (a *finalSemanticArchive) buildFleetLifecyclePayoutArtifacts(source *FinalSemanticEvidence, lifecycle *FinalFleetLifecycleEvidence, events *finalSemanticEventIndex) ([]FinalFleetLifecyclePayoutArtifact, error) {
	if a == nil || a.collected == nil || source == nil || lifecycle == nil || events == nil {
		return nil, errors.New("fleet lifecycle payout construction context is incomplete")
	}
	records := make(map[string][]FleetLifecyclePayoutEvidence)
	for _, payout := range lifecycle.State.Payouts {
		key := fmt.Sprintf("%d/%d", payout.Epoch, payout.NoID)
		records[key] = append(records[key], payout)
	}
	if len(a.collected.LifecyclePayouts) != len(records) {
		return nil, fmt.Errorf("collected lifecycle payout count=%d, want %d", len(a.collected.LifecyclePayouts), len(records))
	}
	result := make([]FinalFleetLifecyclePayoutArtifact, 0, len(records))
	seen := map[string]bool{}
	for _, collected := range a.collected.LifecyclePayouts {
		key := fmt.Sprintf("%d/%d", collected.Epoch, collected.NoID)
		stateRecords := records[key]
		if len(stateRecords) == 0 || len(stateRecords) > 2 || seen[key] {
			return nil, fmt.Errorf("collected lifecycle payout %s has no exact state record set or is duplicate", key)
		}
		data, _, err := a.file(collected.Artifact.URI)
		if err != nil {
			return nil, err
		}
		artifact, err := payoutartifact.Decode(data)
		if err != nil || artifact.Epoch != collected.Epoch || artifact.NoID != collected.NoID {
			return nil, stateMismatchError(err, "collected lifecycle payout %s differs from its terminal state pair", key)
		}
		artifactRoot := "0x" + hex.EncodeToString(artifact.PayoutRoot[:])
		for _, stateRecord := range stateRecords {
			if artifact.ContentHash != stateRecord.ContentHash || !strings.EqualFold(artifactRoot, stateRecord.PayoutRoot) {
				return nil, fmt.Errorf("collected lifecycle payout %s differs from terminal disposition %s", key, stateRecord.Disposition)
			}
		}
		rootEvent, err := finalSemanticUniqueEvent(events, []string{"OperatorRootCommitted"}, collected.Epoch, collected.NoID, true)
		if err != nil {
			return nil, err
		}
		root, rootOK := finalSemanticHex32(rootEvent.Args, "payoutRoot")
		artifactHash, artifactOK := finalSemanticHex32(rootEvent.Args, "artifactHash")
		if !rootOK || !artifactOK || !strings.EqualFold(root, stateRecords[0].PayoutRoot) || !strings.EqualFold(artifactHash, "0x"+strings.TrimPrefix(artifact.ContentHash, "sha256:")) {
			return nil, fmt.Errorf("fleet lifecycle payout %s root event differs from the full artifact", key)
		}
		receipt, err := a.receiptFromIndex(events, *rootEvent, fmt.Sprintf("fleet-lifecycle-epoch-%d-pool-%d-root", collected.Epoch, collected.NoID))
		if err != nil {
			return nil, err
		}
		result = append(result, FinalFleetLifecyclePayoutArtifact{Epoch: collected.Epoch, NoID: collected.NoID, Root: receipt, Artifact: collected.Artifact})
		seen[key] = true
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Epoch != result[j].Epoch {
			return result[i].Epoch < result[j].Epoch
		}
		return result[i].NoID < result[j].NoID
	})
	return result, nil
}

func finalFleetLifecycleValidator(evidence *FinalSemanticEvidence, validatorID uint64) *FinalValidatorIdentityEvidence {
	if evidence == nil {
		return nil
	}
	for index := range evidence.Validators {
		if evidence.Validators[index].ValidatorID == validatorID {
			return &evidence.Validators[index]
		}
	}
	return nil
}

// buildFleetLifecycleAppliedDecisions runs after the ordinary accepted-window
// validator cycles have established the pinned validator identities. It binds
// every current-phase lifecycle census, including post-acceptance tail rows,
// to its exact signed source artifacts. No loose validator state is consulted.
func (a *finalSemanticArchive) buildFleetLifecycleAppliedDecisions(source *FinalSemanticEvidence) error {
	if a == nil || a.collected == nil || source == nil || source.FleetLifecycle == nil {
		return errors.New("fleet lifecycle applied-decision construction context is incomplete")
	}
	collectedByValidator := make(map[uint64][]FinalCollectedValidatorIntent, len(a.collected.Validators))
	for _, validator := range a.collected.Validators {
		if validator.ValidatorID == 0 || collectedByValidator[validator.ValidatorID] != nil {
			return errors.New("collected lifecycle validator identity is zero or duplicate")
		}
		collectedByValidator[validator.ValidatorID] = validator.LifecycleIntents
	}
	seenArtifacts := map[string]bool{}
	for censusIndex := range source.FleetLifecycle.State.CandidateCensuses {
		census := &source.FleetLifecycle.State.CandidateCensuses[censusIndex]
		if census.Phase != source.Phase {
			continue
		}
		for validatorIndex := range census.Validators {
			row := census.Validators[validatorIndex]
			matches := 0
			var matched FinalCollectedValidatorIntent
			for _, candidate := range collectedByValidator[uint64(row.ValidatorID)] {
				intentData, _, err := a.file(candidate.Artifact.URI)
				if err != nil {
					return err
				}
				var intent validatorpkg.SteeringIntent
				if err := decodeStrictJSONBytes(intentData, &intent); err != nil {
					return fmt.Errorf("decode lifecycle validator %d intent: %w", row.ValidatorID, err)
				}
				if finalLifecycleIntentMatches(&intent, row) {
					matches++
					matched = candidate
				}
			}
			if matches != 1 {
				return fmt.Errorf("fleet lifecycle census %d validator %d signed artifact matches=%d, want 1", censusIndex, row.ValidatorID, matches)
			}
			if seenArtifacts[matched.Artifact.URI] {
				return fmt.Errorf("fleet lifecycle signed intent %s is reused by multiple census rows", matched.Artifact.URI)
			}
			seenArtifacts[matched.Artifact.URI] = true
			decision := FinalFleetLifecycleAppliedDecision{
				CensusIndex: uint64(censusIndex), ValidatorID: uint64(row.ValidatorID), SettlementEpoch: row.SettlementEpoch,
				SubnetEpoch: row.SubnetEpoch, VectorHash: strings.ToLower(row.VectorHash), Intent: matched.Artifact,
				Measurement: matched.Measurement, Envelope: matched.Envelope,
			}
			intentData, _, err := a.file(decision.Intent.URI)
			if err != nil {
				return err
			}
			measurementData, _, err := a.file(decision.Measurement.URI)
			if err != nil {
				return err
			}
			envelopeData, _, err := a.file(decision.Envelope.URI)
			if err != nil {
				return err
			}
			if err := verifyFinalFleetLifecycleAppliedDecisionArtifacts(source, &decision, &row, finalFleetLifecycleValidator(source, decision.ValidatorID), intentData, measurementData, envelopeData); err != nil {
				return fmt.Errorf("fleet lifecycle census %d validator %d: %w", censusIndex, row.ValidatorID, err)
			}
			source.FleetLifecycle.AppliedDecisions = append(source.FleetLifecycle.AppliedDecisions, decision)
		}
	}
	return nil
}

func finalFleetLifecycleWeightMap(uids, values []uint16) (map[uint16]uint16, error) {
	if len(uids) != len(values) {
		return nil, errors.New("fleet lifecycle signed UID/value vectors have different lengths")
	}
	result := make(map[uint16]uint16, len(uids))
	for index, uid := range uids {
		if _, duplicate := result[uid]; duplicate {
			return nil, fmt.Errorf("fleet lifecycle signed vector duplicates UID %d", uid)
		}
		result[uid] = values[index]
	}
	return result, nil
}

func finalFleetLifecycleVerifiedUIDs(values []validatorpkg.ExactWeightInput) []uint16 {
	result := make([]uint16, len(values))
	for index := range values {
		result[index] = values[index].UID
	}
	return result
}

func verifyFinalFleetLifecycleAppliedDecisionArtifacts(evidence *FinalSemanticEvidence, decision *FinalFleetLifecycleAppliedDecision, census *FleetLifecycleValidatorCensus, validator *FinalValidatorIdentityEvidence, intentData, measurementData, envelopeData []byte) error {
	if evidence == nil || decision == nil || census == nil || validator == nil {
		return errors.New("fleet lifecycle signed-decision verification context is incomplete")
	}
	if decision.ValidatorID != uint64(census.ValidatorID) || decision.ValidatorID != validator.ValidatorID || decision.SettlementEpoch != census.SettlementEpoch || decision.SubnetEpoch != census.SubnetEpoch || !strings.EqualFold(decision.VectorHash, census.VectorHash) {
		return errors.New("fleet lifecycle signed-decision index differs from its census")
	}
	for label, item := range map[string]struct {
		locator FinalArtifactLocator
		kind    string
	}{
		"intent": {decision.Intent, "steering-intent"}, "measurement": {decision.Measurement, "validator-release-measurement"}, "measurement envelope": {decision.Envelope, "validator-release-measurement-envelope"},
	} {
		if err := verifyFinalArtifact("fleet lifecycle "+label, item.locator, item.kind); err != nil {
			return err
		}
	}
	var intent validatorpkg.SteeringIntent
	if err := decodeStrictJSONBytes(intentData, &intent); err != nil {
		return fmt.Errorf("decode fleet lifecycle steering intent: %w", err)
	}
	if mismatch := finalLifecycleIntentMismatch(&intent, *census); mismatch != "" || intent.SelfUID != validator.UID || intent.Prepared == nil {
		return fmt.Errorf("fleet lifecycle steering intent differs from its public census or validator identity: field=%s self_uid=%d want=%d prepared=%t", mismatch, intent.SelfUID, validator.UID, intent.Prepared != nil)
	}
	if _, err := intent.Prepared.Validate(); err != nil {
		return fmt.Errorf("fleet lifecycle prepared CRv4 submission: %w", err)
	}
	if err := intent.VerifyVectorHash(); err != nil {
		return fmt.Errorf("fleet lifecycle steering vector: %w", err)
	}
	if decision.Measurement.ContentHash != intent.MeasurementArtifactHash || decision.Measurement.SizeBytes != intent.MeasurementArtifactSize || validatorpkg.ReleaseMeasurementContentHash(measurementData) != intent.MeasurementArtifactHash || uint64(len(measurementData)) != intent.MeasurementArtifactSize {
		return errors.New("fleet lifecycle measurement content address differs from its signed intent")
	}
	measurement, verified, err := validatorpkg.DecodeReleaseMeasurementArtifact(measurementData)
	if err != nil {
		return fmt.Errorf("decode fleet lifecycle release measurement: %w", err)
	}
	if err := validatorpkg.VerifyReleaseMeasurementIntent(&intent, measurement, verified); err != nil {
		return fmt.Errorf("verify fleet lifecycle release measurement: %w", err)
	}
	if measurement.DeploymentID != evidence.DeploymentID || measurement.ChainID != evidence.ChainID || !strings.EqualFold(measurement.GenesisHash, evidence.GenesisHash) || !strings.EqualFold(measurement.Coordinator, evidence.Deployment.CoordinatorProxy) || !strings.EqualFold(measurement.SettlementVault, evidence.Deployment.SettlementVault) || measurement.ValidatorID != validator.ValidatorID || measurement.Netuid != evidence.Netuid || measurement.SettlementEpoch != census.SettlementEpoch || measurement.SubnetEpoch != census.SubnetEpoch || !strings.EqualFold(measurement.PolicyHash, evidence.PolicyHash) || measurement.NativeSnapshotBlock != census.NativeSnapshot.Number || !strings.EqualFold(measurement.NativeSnapshotHash, census.NativeSnapshot.Hash) || measurement.EVMSnapshotBlock != census.EVMSnapshot.Number || !strings.EqualFold(measurement.EVMSnapshotHash, census.EVMSnapshot.Hash) || measurement.SelfUID != validator.UID {
		return errors.New("fleet lifecycle measurement identity or checkpoint differs from semantic evidence")
	}
	if !finalJSONEqual(finalFleetLifecycleVerifiedUIDs(verified.EligibleHead), census.EligibleUIDs) || !finalJSONEqual(finalFleetLifecycleVerifiedUIDs(verified.SelectedHead), census.SelectedUIDs) || !finalJSONEqual(finalFleetLifecycleVerifiedUIDs(verified.RejectedHead), census.RejectedUIDs) {
		return errors.New("fleet lifecycle measurement-derived head partition differs from its public census")
	}
	weights, err := finalFleetLifecycleWeightMap(intent.UIDs, intent.Values)
	if err != nil {
		return err
	}
	if len(census.AppliedWeights) != len(census.EligibleUIDs) {
		return errors.New("fleet lifecycle public applied-weight census is incomplete")
	}
	for _, expected := range census.AppliedWeights {
		got, ok := weights[expected.UID]
		if got != expected.Value || !ok && expected.Value != 0 {
			return fmt.Errorf("fleet lifecycle signed applied weight UID %d differs from public census", expected.UID)
		}
	}
	if decision.Envelope.ContentHash != intent.MeasurementEnvelopeHash || decision.Envelope.SizeBytes != intent.MeasurementEnvelopeSize || validatorpkg.ReleaseMeasurementEnvelopeContentHash(envelopeData) != intent.MeasurementEnvelopeHash || uint64(len(envelopeData)) != intent.MeasurementEnvelopeSize {
		return errors.New("fleet lifecycle measurement envelope content address differs from its signed intent")
	}
	envelope, err := validatorpkg.DecodeReleaseMeasurementEnvelope(envelopeData)
	if err != nil {
		return fmt.Errorf("decode fleet lifecycle measurement envelope: %w", err)
	}
	hotkeyBytes, err := hex.DecodeString(strings.TrimPrefix(envelope.ValidatorHotkey, "0x"))
	if err != nil || len(hotkeyBytes) != 32 || finalAccountMatches(validator.Hotkey, hotkeyBytes) != nil || intent.Prepared.HotkeyHex != envelope.ValidatorHotkey {
		return errors.New("fleet lifecycle measurement envelope signer differs from the pinned validator hotkey")
	}
	var hotkey [32]byte
	copy(hotkey[:], hotkeyBytes)
	sealedMeasurement, _, err := validatorpkg.VerifyReleaseMeasurementEnvelope(envelope, measurementData, hotkey, validator.UID, intent.Prepared.ExtrinsicHash)
	if err != nil || !finalJSONEqual(sealedMeasurement, measurement) {
		return stateMismatchError(err, "fleet lifecycle measurement envelope does not authenticate the exact measurement")
	}
	signedAt, err := time.Parse(time.RFC3339Nano, envelope.SignedAt)
	startedAt, startErr := time.Parse(time.RFC3339Nano, evidence.CampaignStartedAt)
	completedAt, completeErr := time.Parse(time.RFC3339Nano, evidence.CampaignCompletedAt)
	if err != nil || startErr != nil || completeErr != nil || signedAt.Before(startedAt) || signedAt.After(completedAt) {
		return errors.New("fleet lifecycle measurement envelope signing time is outside the campaign")
	}
	return nil
}

func verifyAndIndexFinalFleetLifecycle(evidence *FinalSemanticEvidence, semantic *FinalFleetLifecycleEvidence, plan *SetupPlan, entries []JournalEntry, identities *finalPublicIdentities, files map[string][]byte) ([]FinalFleetLifecycleVariantEvidence, error) {
	if evidence == nil || semantic == nil || plan == nil || identities == nil {
		return nil, errors.New("fleet lifecycle lineage verification context is incomplete")
	}
	roles, err := finalFleetLifecycleRoleSecrets(identities)
	if err != nil {
		return nil, err
	}
	result := make([]FinalFleetLifecycleVariantEvidence, 0, 5)
	manifests := map[string]*protocol.FleetManifest{}
	for _, name := range finalFleetLifecycleVariantNames() {
		variant, _ := fleetLifecycleVariantFor(name)
		manifestBytes := files["public/"+variant.ManifestName]
		manifest, err := protocol.ParseFleetManifest(manifestBytes)
		if err != nil {
			return nil, fmt.Errorf("fleet lifecycle %s manifest: %w", name, err)
		}
		if manifest.ChainID != evidence.ChainID || manifest.Netuid != evidence.Netuid || common.BytesToAddress(manifest.Coordinator[:]) != common.HexToAddress(evidence.Deployment.CoordinatorProxy) || manifest.Generation != variant.Generation || len(manifest.Members) != semantic.ClientsPerHeadFleet {
			return nil, fmt.Errorf("fleet lifecycle %s manifest differs from chain, coordinator, generation, or topology", name)
		}
		expectedHotkey, err := roleBytes32(roles, variant.HotkeyLabel)
		if err != nil || manifest.Hotkey != expectedHotkey {
			return nil, stateMismatchError(err, "fleet lifecycle %s manifest uses another hotkey", name)
		}
		descriptor, err := fleetLifecycleVariantDescriptor(&ResolvedConfig{Config: &HarnessConfig{Topology: TopologyConfig{ClientsPerHeadFleet: semantic.ClientsPerHeadFleet, Operators: evidence.ExpectedOperators, Miners: evidence.ExpectedMiners, HeadFleets: evidence.ExpectedHeadSlots, ChallengerFleets: evidence.ExpectedCandidates - evidence.ExpectedHeadSlots}}}, name)
		if err != nil {
			return nil, err
		}
		members := make([]FinalFleetLifecycleMember, len(manifest.Members))
		for index, member := range manifest.Members {
			identity, ok := identities.Clients[fmt.Sprintf("miner-%d", descriptor.MinerIDs[index])]
			if !ok || !strings.EqualFold(identity.ClientID, fleetLifecycleHex16(member.ClientID)) || !strings.EqualFold(identity.ClientKey, fleetLifecycleHex(member.ClientKey)) {
				return nil, fmt.Errorf("fleet lifecycle %s member %d differs from public miner identity", name, index+1)
			}
			members[index] = FinalFleetLifecycleMember{ClientID: fleetLifecycleHex16(member.ClientID), ClientKey: fleetLifecycleHex(member.ClientKey)}
		}
		commitmentHash, err := manifest.CommitmentHash()
		if err != nil {
			return nil, err
		}
		var commitment FleetCommitmentEvidence
		if err := decodeStrictJSONBytes(files["public/"+variant.CommitmentName], &commitment); err != nil {
			return nil, fmt.Errorf("decode fleet lifecycle %s commitment: %w", name, err)
		}
		commitmentActionID, _ := fleetLifecycleCommitmentActionID(name)
		commitmentAction, err := finalFleetLifecyclePlanAction(plan, commitmentActionID)
		if err != nil {
			return nil, err
		}
		commitmentTransaction, err := finalFleetLifecycleJournalTransaction(entries, plan.PlanHash, commitmentAction)
		if err != nil {
			return nil, err
		}
		if commitment.Schema != fleetCommitmentEvidenceSchemaV2 || commitment.ManifestURI != variant.ManifestName || !strings.EqualFold(commitment.CommitmentHash, fleetLifecycleHex(commitmentHash)) || !strings.EqualFold(commitment.Hotkey, fleetLifecycleHex(manifest.Hotkey)) {
			return nil, fmt.Errorf("fleet lifecycle %s commitment differs from its canonical manifest", name)
		}
		if err := validateFleetLifecycleCommitmentLineage(commitment, commitmentAction, name, evidence.DeploymentID, evidence.PlanHash, commitmentTransaction); err != nil {
			return nil, fmt.Errorf("fleet lifecycle %s commitment lineage: %w", name, err)
		}
		start, nativeEnd, evmEnd, effectiveEpoch, err := finalFleetLifecycleVariantRange(&semantic.State, name)
		if err != nil || !fleetLifecycleBlockInRange(commitment.FinalizedBlock, start, nativeEnd) {
			return nil, stateMismatchError(err, "fleet lifecycle %s commitment is outside its exact action range", name)
		}
		var mirror FleetLifecycleMirrorEvidence
		if err := decodeStrictJSONBytes(files["public/"+fleetLifecycleMirrorEvidenceName(name)], &mirror); err != nil {
			return nil, fmt.Errorf("decode fleet lifecycle %s mirror: %w", name, err)
		}
		mirrorActionID, _ := fleetLifecycleMirrorActionID(name)
		mirrorAction, err := finalFleetLifecyclePlanAction(plan, mirrorActionID)
		if err != nil {
			return nil, err
		}
		mirrorTransaction, err := finalFleetLifecycleJournalTransaction(entries, plan.PlanHash, mirrorAction)
		if err != nil {
			return nil, err
		}
		if err := validateFleetLifecycleMirrorLineage(mirror, mirrorAction, name, evidence.DeploymentID, evidence.PlanHash, mirrorTransaction); err != nil {
			return nil, fmt.Errorf("fleet lifecycle %s mirror lineage: %w", name, err)
		}
		if !strings.EqualFold(mirror.Hotkey, commitment.Hotkey) || !strings.EqualFold(mirror.CommitmentHash, commitment.CommitmentHash) || mirror.FinalizedBlock != commitment.FinalizedBlock || !strings.EqualFold(mirror.FinalizedBlockHash, commitment.FinalizedBlockHash) || !fleetLifecycleBlockInRange(mirror.BlockNumber, start, evmEnd) || !strings.EqualFold(mirrorAction.Target, evidence.Deployment.CoordinatorProxy) {
			return nil, fmt.Errorf("fleet lifecycle %s mirror differs from its exact native commitment/action", name)
		}
		indexed := FinalFleetLifecycleVariantEvidence{Name: name, FleetID: fleetLifecycleHex(manifest.FleetID), Hotkey: fleetLifecycleHex(manifest.Hotkey), Generation: manifest.Generation, ManifestHash: bytesSHA256(manifestBytes), Members: members, Commitment: commitment, Mirror: mirror}
		wantUID, _ := finalFleetLifecycleVariantUID(name)
		for memberIndex, member := range manifest.Members {
			memberNumber := memberIndex + 1
			var binding FleetBindingEvidence
			if err := decodeStrictJSONBytes(files["public/"+variant.BindingName(memberNumber)], &binding); err != nil {
				return nil, fmt.Errorf("decode fleet lifecycle %s binding %d: %w", name, memberNumber, err)
			}
			if err := verifyFinalFleetLifecycleBinding(evidence, plan, entries, name, memberNumber, variant, *manifest, commitmentHash, member, binding, effectiveEpoch, wantUID, start, evmEnd); err != nil {
				return nil, err
			}
			indexed.Bindings = append(indexed.Bindings, binding)
		}
		manifests[name] = manifest
		result = append(result, indexed)
	}
	byName := make(map[string]*FinalFleetLifecycleVariantEvidence, len(result))
	for index := range result {
		byName[result[index].Name] = &result[index]
	}
	if byName[fleetLifecycleVariantTargetTakeover].FleetID != byName[fleetLifecycleVariantProvider].FleetID || byName[fleetLifecycleVariantCompanionTakeover].FleetID != byName[fleetLifecycleVariantTerminal].FleetID || byName[fleetLifecycleVariantFallback].FleetID == byName[fleetLifecycleVariantTargetTakeover].FleetID || byName[fleetLifecycleVariantFallback].FleetID == byName[fleetLifecycleVariantCompanionTakeover].FleetID {
		return nil, errors.New("fleet lifecycle generations do not preserve two provider fleets and one distinct fallback fleet")
	}
	for _, name := range []string{fleetLifecycleVariantFallback, fleetLifecycleVariantProvider, fleetLifecycleVariantTerminal} {
		preName, registrationName := fleetLifecycleRegistrationNames(name)
		var pre FinalFleetLifecycleRegistrationPreparation
		if err := decodeStrictJSONBytes(files["public/"+preName], &pre); err != nil {
			return nil, fmt.Errorf("decode fleet lifecycle %s preparation: %w", name, err)
		}
		var registration FleetLifecycleRegistrationEvidence
		if err := decodeStrictJSONBytes(files["public/"+registrationName], &registration); err != nil {
			return nil, fmt.Errorf("decode fleet lifecycle %s registration: %w", name, err)
		}
		actionID, _ := fleetLifecycleRegistrationActionIDFor(name)
		action, err := finalFleetLifecyclePlanAction(plan, actionID)
		if err != nil {
			return nil, err
		}
		transaction, err := finalFleetLifecycleJournalTransaction(entries, plan.PlanHash, action)
		if err != nil {
			return nil, err
		}
		if pre.Schema != "urnetwork-sim-fleet-registration-pre-v1" || pre.PlanHash != evidence.PlanHash || pre.ActionID != action.ID || pre.IntentHash != action.IntentHash || pre.VictimFleet != registration.VictimFleet || !strings.EqualFold(pre.VictimHotkey, registration.VictimHotkey) || !finalJSONEqual(pre.Snapshot, registration.PrePrune) {
			return nil, fmt.Errorf("fleet lifecycle %s registration preparation differs from the finalized mutation", name)
		}
		if err := validateFleetLifecycleRegistrationLineage(registration, action, name, evidence.DeploymentID, evidence.PlanHash, roles, transaction); err != nil {
			return nil, fmt.Errorf("fleet lifecycle %s registration lineage: %w", name, err)
		}
		if err := validateFleetLifecycleRegistrationRecoverySnapshot(registration.PrePrune, name, roles); err != nil {
			return nil, fmt.Errorf("fleet lifecycle %s registration recovery state: %w", name, err)
		}
		indexed := byName[name]
		indexed.RegistrationPreparation, indexed.Registration = &pre, &registration
	}
	for _, item := range []struct {
		name     string
		embedded []FleetLifecycleCleanupEvidence
	}{
		{name: fleetLifecycleVariantTargetTakeover, embedded: semantic.State.TargetCleanup},
		{name: fleetLifecycleVariantCompanionTakeover, embedded: semantic.State.CompanionCleanup},
		{name: fleetLifecycleVariantFallback, embedded: semantic.State.FallbackCleanup},
	} {
		manifest := manifests[item.name]
		if manifest == nil || len(item.embedded) != semantic.ClientsPerHeadFleet {
			return nil, fmt.Errorf("fleet lifecycle %s cleanup census is incomplete", item.name)
		}
		decoded := make([]FleetLifecycleCleanupEvidence, 0, semantic.ClientsPerHeadFleet)
		variant, _ := fleetLifecycleVariantFor(item.name)
		for memberIndex, member := range manifest.Members {
			memberNumber := memberIndex + 1
			var cleanup FleetLifecycleCleanupEvidence
			if err := decodeStrictJSONBytes(files["public/"+fleetLifecycleCleanupEvidenceName(item.name, memberNumber)], &cleanup); err != nil {
				return nil, fmt.Errorf("decode fleet lifecycle %s cleanup %d: %w", item.name, memberNumber, err)
			}
			if err := validateFleetLifecycleCleanupEvidence(cleanup, member, manifest.FleetID, manifest.Generation); err != nil {
				return nil, fmt.Errorf("fleet lifecycle %s cleanup %d: %w", item.name, memberNumber, err)
			}
			actionID, _ := fleetLifecycleCleanupActionID(item.name, memberNumber)
			action, err := finalFleetLifecyclePlanAction(plan, actionID)
			if err != nil {
				return nil, err
			}
			transaction, err := finalFleetLifecycleJournalTransaction(entries, plan.PlanHash, action)
			if err != nil {
				return nil, err
			}
			if err := validateFleetLifecycleCleanupLineage(cleanup, action, evidence.DeploymentID, evidence.PlanHash, transaction); err != nil {
				return nil, fmt.Errorf("fleet lifecycle %s cleanup %d lineage: %w", item.name, memberNumber, err)
			}
			wantMiner := fleetMemberMinerIndex(&ResolvedConfig{Config: &HarnessConfig{Topology: TopologyConfig{ClientsPerHeadFleet: semantic.ClientsPerHeadFleet}}}, variant.Fleet, memberNumber)
			if variant.Fallback {
				wantMiner = (evidence.ExpectedCandidates * semantic.ClientsPerHeadFleet) + 1 + (memberNumber-1)*evidence.ExpectedOperators
			}
			if action.Kind != "evm-transaction" || action.Target != "miner:"+strconv.Itoa(wantMiner) {
				return nil, fmt.Errorf("fleet lifecycle %s cleanup %d targets another member", item.name, memberNumber)
			}
			decoded = append(decoded, cleanup)
		}
		if !finalJSONEqual(decoded, item.embedded) {
			return nil, fmt.Errorf("fleet lifecycle %s cleanup artifacts differ from terminal state", item.name)
		}
		byName[item.name].Cleanups = decoded
	}
	if !finalJSONEqual(*byName[fleetLifecycleVariantFallback].Registration, *semantic.State.FallbackRegistration) || !finalJSONEqual(*byName[fleetLifecycleVariantProvider].Registration, *semantic.State.ProviderRegistration) || !finalJSONEqual(*byName[fleetLifecycleVariantTerminal].Registration, *semantic.State.TerminalRegistration) {
		return nil, errors.New("fleet lifecycle registration artifacts differ from terminal state")
	}
	canonicalizeFinalFleetLifecycleVariants(result)
	return result, nil
}

func verifyFinalFleetLifecycleBinding(evidence *FinalSemanticEvidence, plan *SetupPlan, entries []JournalEntry, variantName string, memberNumber int, variant fleetLifecycleVariant, manifest protocol.FleetManifest, commitmentHash [32]byte, member protocol.FleetMember, binding FleetBindingEvidence, effectiveEpoch uint64, wantUID uint16, blockStart, blockEnd uint64) error {
	if binding.Schema != "urnetwork-fleet-binding-evidence-v1" || binding.DeploymentID != evidence.DeploymentID || binding.PlanHash != evidence.PlanHash || binding.Generation != manifest.Generation || binding.ValidFromEpoch != effectiveEpoch || binding.ValidToEpoch < binding.ValidFromEpoch || binding.UID != wantUID || !fleetLifecycleBlockInRange(binding.BlockNumber, blockStart, blockEnd) {
		return fmt.Errorf("fleet lifecycle %s binding %d has invalid identity, epoch, UID, or block", variantName, memberNumber)
	}
	clientID, idOK := evidenceFixedHex(strings.ToLower(binding.ClientID), 16)
	clientKey, keyOK := evidenceFixedHex(strings.ToLower(binding.ClientKey), 32)
	fleetID, fleetOK := evidenceFixedHex(strings.ToLower(binding.FleetID), 32)
	hotkey, hotkeyOK := evidenceFixedHex(strings.ToLower(binding.Hotkey), 32)
	commitment, commitmentOK := evidenceFixedHex(strings.ToLower(binding.CommitmentHash), 32)
	digest, digestOK := evidenceFixedHex(strings.ToLower(binding.BindingDigest), 32)
	clientSignature, clientSigOK := evidenceFixedHex(strings.ToLower(binding.ClientSignature), 64)
	hotkeySignature, hotkeySigOK := evidenceFixedHex(strings.ToLower(binding.HotkeySignature), 64)
	if !idOK || !keyOK || !fleetOK || !hotkeyOK || !commitmentOK || !digestOK || !clientSigOK || !hotkeySigOK {
		return fmt.Errorf("fleet lifecycle %s binding %d has malformed cryptographic fields", variantName, memberNumber)
	}
	var value protocol.FleetBinding
	value.ChainID, value.Netuid, value.Coordinator = manifest.ChainID, manifest.Netuid, manifest.Coordinator
	copy(value.ClientID[:], clientID)
	copy(value.ClientKey[:], clientKey)
	copy(value.FleetID[:], fleetID)
	copy(value.Hotkey[:], hotkey)
	copy(value.CommitmentHash[:], commitment)
	value.Generation, value.ValidFromEpoch, value.ValidToEpoch = binding.Generation, binding.ValidFromEpoch, binding.ValidToEpoch
	wantDigest, err := value.Digest()
	if err != nil || value.ClientID != member.ClientID || value.ClientKey != member.ClientKey || value.FleetID != manifest.FleetID || value.Hotkey != manifest.Hotkey || value.CommitmentHash != commitmentHash || !bytes.Equal(wantDigest[:], digest) || !value.VerifyClient(clientSignature) || !value.VerifyHotkey(hotkeySignature) {
		return stateMismatchError(err, "fleet lifecycle %s binding %d differs cryptographically from its manifest", variantName, memberNumber)
	}
	actionID, _ := fleetLifecycleBindingActionID(variantName, memberNumber)
	action, err := finalFleetLifecyclePlanAction(plan, actionID)
	if err != nil {
		return err
	}
	transaction, err := finalFleetLifecycleJournalTransaction(entries, plan.PlanHash, action)
	if err != nil {
		return err
	}
	if err := validateFleetLifecycleBindingLineage(binding, action, variantName, memberNumber, evidence.DeploymentID, evidence.PlanHash, transaction); err != nil {
		return fmt.Errorf("fleet lifecycle %s binding %d lineage: %w", variantName, memberNumber, err)
	}
	clients := evidence.ExpectedCandidates // use the top-level exact topology below
	_ = clients
	wantMiner := (variant.Fleet-1)*len(manifest.Members) + memberNumber
	if variant.Fallback {
		wantMiner = evidence.ExpectedCandidates*len(manifest.Members) + 1 + (memberNumber-1)*evidence.ExpectedOperators
	}
	if action.Kind != "evm-transaction" || action.Target != "miner:"+strconv.Itoa(wantMiner) {
		return fmt.Errorf("fleet lifecycle %s binding %d targets another member", variantName, memberNumber)
	}
	return nil
}

func finalFleetLifecycleReleasePrefix(state FleetLifecycleEvidence) FleetLifecycleEvidence {
	return *fleetLifecycleReleaseProjection(&state)
}

func verifyFinalFleetLifecycleContinuity(prior, current *FinalFleetLifecycleEvidence) error {
	if prior == nil || current == nil || prior.State.Stage != fleetLifecycleStageReleaseHandoff || current.State.Stage != fleetLifecycleStageComplete || !validSHA256ContentHash(prior.ReleaseHandoffHash) || prior.ReleaseHandoffSize == 0 || current.ReleaseHandoffHash != prior.ReleaseHandoffHash || current.ReleaseHandoffSize != prior.ReleaseHandoffSize || current.ClientsPerHeadFleet != prior.ClientsPerHeadFleet || !finalJSONEqual(current.Roles, prior.Roles) || !finalJSONEqual(current.Variants, prior.Variants) {
		return errors.New("fleet lifecycle immutable handoff identity, roles, or mutation graph changed")
	}
	if !finalJSONEqual(finalFleetLifecycleReleasePrefix(current.State), prior.State) {
		return errors.New("fleet lifecycle release handoff state changed during production")
	}
	productionCensuses := 0
	for _, census := range current.State.CandidateCensuses {
		if census.Phase == "production-soak" {
			productionCensuses++
		}
	}
	if productionCensuses == 0 || fleetLifecycleMilestone(&current.State, fleetLifecycleMilestoneTerminalActive) == nil {
		return errors.New("fleet lifecycle production continuation has no terminal-active public decision")
	}
	return nil
}

func verifyFinalFleetLifecycle(evidence *FinalSemanticEvidence) error {
	if evidence == nil {
		return errors.New("final semantic evidence is unavailable")
	}
	lifecycle := evidence.FleetLifecycle
	if lifecycle == nil {
		return errors.New("final semantic evidence has no fleet lifecycle")
	}
	state := &lifecycle.State
	if lifecycle.ClientsPerHeadFleet < 1 || len(lifecycle.Roles) != 2*fleetLifecycleTerminalVictimChurn || !validSHA256ContentHash(lifecycle.ReleaseHandoffHash) || lifecycle.ReleaseHandoffSize == 0 || state.Schema != fleetLifecycleEvidenceSchema || state.DeploymentID != evidence.DeploymentID || state.PlanHash != evidence.PlanHash || state.RunID == "" {
		return errors.New("fleet lifecycle semantic identity, handoff, roles, or topology is invalid")
	}
	switch evidence.Phase {
	case "release-1.0":
		if state.Stage != fleetLifecycleStageReleaseHandoff || state.RunID != evidence.RunID || fleetLifecycleHasProductionState(state) || state.FirstAcceptedEpoch != evidence.Window.FirstEpoch || state.AcceptanceStartBlock != evidence.Window.StartBlock || state.AcceptanceEndBlock != evidence.Window.EndBlock || state.AcceptanceTerminalBlock != evidence.Window.TerminalBlock {
			return errors.New("fleet lifecycle semantic release handoff differs from its fixed acceptance window")
		}
	case "production-soak":
		if evidence.PriorPhase == nil || state.Stage != fleetLifecycleStageComplete || state.RunID != evidence.PriorPhase.RunID || state.ProductionRunID != evidence.RunID || state.ReleaseHandoffHash != lifecycle.ReleaseHandoffHash || state.ProductionFirstSettlementEpoch != evidence.Window.FirstEpoch || state.ProductionAcceptanceStartBlock != evidence.Window.StartBlock || state.ProductionAcceptanceEndBlock != evidence.Window.EndBlock || state.ProductionAcceptanceTerminalBlock != evidence.Window.TerminalBlock || state.ProductionNativeSchedule == nil {
			return errors.New("fleet lifecycle semantic production completion differs from its release predecessor or fixed acceptance window")
		}
	default:
		return fmt.Errorf("phase %q has no fleet lifecycle semantic evidence", evidence.Phase)
	}
	if state.ReleaseHandoffSchedule == nil || state.PostRegistrationRewardBaseline.Number == 0 || requireFinalHex32("fleet lifecycle reward fence", strings.ToLower(state.PostRegistrationRewardBaseline.Hash)) != nil {
		return errors.New("fleet lifecycle semantic schedule or reward fence is incomplete")
	}
	if err := validateFleetLifecycleNativeSchedule(state.ReleaseHandoffSchedule, "release-1.0", state.AcceptanceStartBlock, state.AcceptanceTerminalBlock); err != nil {
		return err
	}
	if state.ReleaseEVMEvidenceDeadlineBlock != state.ReleaseHandoffSchedule.ApplicationDeadlineBlock || state.ReleaseEVMEvidenceDeadlineBlock < state.AcceptanceTerminalBlock {
		return errors.New("fleet lifecycle release EVM evidence-tail bound differs from its authenticated schedule")
	}
	if evidence.Phase == "production-soak" {
		if err := validateFleetLifecycleNativeSchedule(state.ProductionNativeSchedule, "production-soak", state.ProductionAcceptanceStartBlock, state.ProductionAcceptanceTerminalBlock); err != nil {
			return err
		}
		if state.ProductionEVMEvidenceDeadlineBlock != state.ProductionNativeSchedule.ApplicationDeadlineBlock || state.ProductionEVMEvidenceDeadlineBlock < state.ProductionAcceptanceTerminalBlock {
			return errors.New("fleet lifecycle production EVM evidence-tail bound differs from its authenticated schedule")
		}
	}
	seenRoles := map[string]bool{}
	for _, role := range lifecycle.Roles {
		if role.Label == "" || seenRoles[role.Label] {
			return errors.New("fleet lifecycle semantic role census is invalid")
		}
		seenRoles[role.Label] = true
		if _, err := decodeHex32("fleet lifecycle role", strings.ToLower(role.PublicKey)); err != nil || role.SS58 == "" {
			return stateMismatchError(err, "fleet lifecycle semantic role %s is invalid", role.Label)
		}
	}
	if err := verifyFinalArtifact("fleet lifecycle lineage", lifecycle.LineageArtifact, "fleet-lifecycle-lineage"); err != nil {
		return err
	}
	if len(lifecycle.Variants) != len(finalFleetLifecycleVariantNames()) {
		return errors.New("fleet lifecycle semantic variant census is incomplete")
	}
	seenVariants := map[string]bool{}
	for _, variant := range lifecycle.Variants {
		if seenVariants[variant.Name] || len(variant.Members) != lifecycle.ClientsPerHeadFleet || len(variant.Bindings) != lifecycle.ClientsPerHeadFleet || variant.FleetID == "" || variant.Hotkey == "" || variant.Generation == 0 {
			return errors.New("fleet lifecycle semantic variant is duplicate or incomplete")
		}
		seenVariants[variant.Name] = true
		if _, err := fleetLifecycleVariantFor(variant.Name); err != nil {
			return err
		}
		if err := requireFinalSHA256("fleet lifecycle manifest hash", variant.ManifestHash); err != nil {
			return err
		}
	}
	for _, name := range finalFleetLifecycleVariantNames() {
		if !seenVariants[name] {
			return fmt.Errorf("fleet lifecycle semantic variant %s is absent", name)
		}
	}
	if state.LaunchPrune == nil || state.FallbackRegistration == nil || state.ProviderRegistration == nil || state.TerminalRegistration == nil || len(state.TargetCleanup) != lifecycle.ClientsPerHeadFleet || len(state.CompanionCleanup) != lifecycle.ClientsPerHeadFleet || len(state.FallbackCleanup) != lifecycle.ClientsPerHeadFleet || len(state.Payouts) != 4 || len(state.CandidateCensuses) == 0 {
		return errors.New("fleet lifecycle state lacks launch, mutation, payout, or applied-decision evidence")
	}
	for _, registration := range []*FleetLifecycleRegistrationEvidence{state.FallbackRegistration, state.ProviderRegistration, state.TerminalRegistration} {
		if err := validateFleetLifecycleRegistrationEvidence(*registration); err != nil {
			return err
		}
	}
	for _, census := range state.CandidateCensuses {
		if err := validateFleetLifecyclePersistedCensus(census); err != nil {
			return err
		}
		if err := validateFleetLifecycleCensusBlockRange(state, census); err != nil {
			return err
		}
		if census.ObservedHead.Number > evidence.EVMTerminalHead.Number || census.NativeObservedHead.Number > evidence.NativeTerminalHead.Number {
			return errors.New("fleet lifecycle census follows the closed semantic terminal checkpoints")
		}
		for _, validator := range census.Validators {
			if validator.EVMSnapshot.Number > evidence.EVMTerminalHead.Number || validator.Application.Number > evidence.NativeTerminalHead.Number {
				return errors.New("fleet lifecycle validator receipt follows the closed semantic terminal checkpoints")
			}
		}
	}
	rank, _ := fleetLifecycleStageRank(state.Stage)
	if err := validateFleetLifecycleArtifactBlocks(state, rank); err != nil {
		return err
	}
	wantPayouts := map[string]bool{
		"pruned-provider-returned-to-operator-pool":        true,
		"fallback-provider-head-excluded":                  true,
		"reregistered-provider-head-excluded":              true,
		"second-pruned-provider-returned-to-operator-pool": true,
	}
	payoutEpochs := make(map[string]uint64, len(wantPayouts))
	for _, payout := range state.Payouts {
		if !wantPayouts[payout.Disposition] || payoutEpochs[payout.Disposition] != 0 || payout.NoID < 1 || payout.NoID > evidence.ExpectedOperators || len(payout.ClientIDs) != lifecycle.ClientsPerHeadFleet || !validSHA256ContentHash(payout.ContentHash) || requireFinalHex32("fleet lifecycle payout root", strings.ToLower(payout.PayoutRoot)) != nil {
			return errors.New("fleet lifecycle payout disposition, epoch, operator, or client census is invalid")
		}
		payoutEpochs[payout.Disposition] = payout.Epoch
	}
	first := payoutEpochs["pruned-provider-returned-to-operator-pool"]
	second := payoutEpochs["reregistered-provider-head-excluded"]
	if first == 0 || first != state.FallbackEffectiveEpoch || second != state.ProviderEffectiveEpoch || second <= first || payoutEpochs["fallback-provider-head-excluded"] != first || payoutEpochs["second-pruned-provider-returned-to-operator-pool"] != second {
		return errors.New("fleet lifecycle payout dispositions are incomplete, unpaired, or unordered")
	}
	wantPayoutArtifacts := make(map[string]int, len(state.Payouts))
	for _, payout := range state.Payouts {
		wantPayoutArtifacts[fmt.Sprintf("%d/%d", payout.Epoch, payout.NoID)]++
	}
	if len(lifecycle.PayoutArtifacts) != len(wantPayoutArtifacts) {
		return fmt.Errorf("fleet lifecycle exact payout artifact count=%d, want %d", len(lifecycle.PayoutArtifacts), len(wantPayoutArtifacts))
	}
	seenPayoutArtifacts := map[string]bool{}
	for index, item := range lifecycle.PayoutArtifacts {
		key := fmt.Sprintf("%d/%d", item.Epoch, item.NoID)
		if item.Epoch == 0 || item.NoID == 0 || seenPayoutArtifacts[key] || index > 0 && (item.Epoch < lifecycle.PayoutArtifacts[index-1].Epoch || item.Epoch == lifecycle.PayoutArtifacts[index-1].Epoch && item.NoID <= lifecycle.PayoutArtifacts[index-1].NoID) {
			return errors.New("fleet lifecycle payout artifact identity is duplicate or noncanonical")
		}
		if err := verifyFinalArtifact("fleet lifecycle payout", item.Artifact, "payout-artifact"); err != nil {
			return err
		}
		if err := verifyFinalEVMReceipt("fleet lifecycle payout root", item.Root, state.AcceptanceStartBlock, evidence.EVMTerminalHead.Number); err != nil {
			return err
		}
		matches := wantPayoutArtifacts[key]
		if matches == 0 || matches > 2 {
			return fmt.Errorf("fleet lifecycle payout artifact %s has %d terminal disposition rows, want 1 or 2", key, matches)
		}
		seenPayoutArtifacts[key] = true
	}
	wantMilestones := map[string]string{
		fleetLifecycleMilestoneTakeoverRejected: "release-1.0",
		fleetLifecycleMilestoneFallbackActive:   "release-1.0",
		fleetLifecycleMilestoneProviderActive:   "release-1.0",
	}
	if evidence.Phase == "production-soak" {
		wantMilestones[fleetLifecycleMilestoneTerminalActive] = "production-soak"
	}
	seenMilestones := map[string]bool{}
	for _, census := range state.CandidateCensuses {
		if census.Milestone == "" {
			continue
		}
		phase, ok := wantMilestones[census.Milestone]
		if !ok || seenMilestones[census.Milestone] || census.Phase != phase {
			return errors.New("fleet lifecycle milestone is unknown, duplicate, or assigned to another phase")
		}
		seenMilestones[census.Milestone] = true
	}
	if len(seenMilestones) != len(wantMilestones) {
		return errors.New("fleet lifecycle semantic evidence lacks an ordered required milestone")
	}
	wantDecisions := 0
	for _, census := range state.CandidateCensuses {
		if census.Phase == evidence.Phase {
			wantDecisions += len(census.Validators)
		}
	}
	if len(lifecycle.AppliedDecisions) != wantDecisions {
		return fmt.Errorf("fleet lifecycle signed applied-decision count=%d, want %d", len(lifecycle.AppliedDecisions), wantDecisions)
	}
	decisionIndex := 0
	for censusIndex := range state.CandidateCensuses {
		census := &state.CandidateCensuses[censusIndex]
		if census.Phase != evidence.Phase {
			continue
		}
		for validatorIndex := range census.Validators {
			row := &census.Validators[validatorIndex]
			decision := &lifecycle.AppliedDecisions[decisionIndex]
			if decision.CensusIndex != uint64(censusIndex) || decision.ValidatorID != uint64(row.ValidatorID) || decision.SettlementEpoch != row.SettlementEpoch || decision.SubnetEpoch != row.SubnetEpoch || !strings.EqualFold(decision.VectorHash, row.VectorHash) {
				return errors.New("fleet lifecycle signed applied-decision index is noncanonical or differs from its census")
			}
			if err := requireFinalHex32("fleet lifecycle applied-decision vector", decision.VectorHash); err != nil {
				return err
			}
			for label, item := range map[string]struct {
				locator FinalArtifactLocator
				kind    string
			}{
				"intent": {decision.Intent, "steering-intent"}, "measurement": {decision.Measurement, "validator-release-measurement"}, "measurement envelope": {decision.Envelope, "validator-release-measurement-envelope"},
			} {
				if err := verifyFinalArtifact("fleet lifecycle "+label, item.locator, item.kind); err != nil {
					return err
				}
			}
			decisionIndex++
		}
	}
	return nil
}

func verifyFinalFleetLifecycleArtifacts(evidence *FinalSemanticEvidence, data []byte) error {
	if evidence == nil || evidence.FleetLifecycle == nil {
		return errors.New("fleet lifecycle artifact evidence is unavailable")
	}
	var lineage finalFleetLifecycleLineageArtifact
	if err := decodeStrictJSONBytes(data, &lineage); err != nil {
		return fmt.Errorf("decode fleet lifecycle lineage artifact: %w", err)
	}
	lifecycle := evidence.FleetLifecycle
	if lineage.Schema != finalFleetLifecycleLineageSchema || lineage.DeploymentID != evidence.DeploymentID || lineage.PlanHash != evidence.PlanHash || lineage.RunID != evidence.RunID {
		return errors.New("fleet lifecycle lineage artifact identity differs from semantic evidence")
	}
	wantPaths, err := finalFleetLifecycleExpectedPaths(lifecycle.ClientsPerHeadFleet)
	if err != nil {
		return err
	}
	if len(lineage.Files) != len(wantPaths) {
		return fmt.Errorf("fleet lifecycle lineage artifact file count=%d, want %d", len(lineage.Files), len(wantPaths))
	}
	files := make(map[string][]byte, len(lineage.Files))
	for index, item := range lineage.Files {
		if index >= len(wantPaths) || item.Path != wantPaths[index] || files[item.Path] != nil || item.SizeBytes != uint64(len(item.Data)) || item.ContentHash != bytesSHA256(item.Data) {
			return fmt.Errorf("fleet lifecycle lineage file %d is unexpected, duplicate, or content-address mismatch", index)
		}
		files[item.Path] = append([]byte(nil), item.Data...)
	}
	var state FleetLifecycleEvidence
	if err := decodeStrictJSONBytes(files["public/fleet-lifecycle.json"], &state); err != nil || !finalJSONEqual(state, lifecycle.State) {
		return stateMismatchError(err, "fleet lifecycle terminal state differs from its lineage artifact")
	}
	plan, err := decodePersistedPlanBytes(files["launch-foundation/plan.json"])
	if err != nil || plan.PlanHash != evidence.PlanHash || plan.DeploymentID != evidence.DeploymentID || plan.ChainID != evidence.ChainID || plan.Netuid != evidence.Netuid {
		return stateMismatchError(err, "fleet lifecycle signed plan differs from semantic identity")
	}
	entries, err := decodeFinalSemanticJournalBytes(files["launch-foundation/journal.jsonl"])
	if err != nil {
		return err
	}
	var identities finalPublicIdentities
	if err := decodeStrictJSONBytes(files["public/identities.json"], &identities); err != nil || identities.DeploymentID != evidence.DeploymentID {
		return stateMismatchError(err, "fleet lifecycle public identities differ from semantic deployment")
	}
	roles, err := finalFleetLifecycleRoles(&identities)
	if err != nil || !finalJSONEqual(roles, lifecycle.Roles) {
		return stateMismatchError(err, "fleet lifecycle role census differs from captured public identities")
	}
	indexed, err := verifyAndIndexFinalFleetLifecycle(evidence, lifecycle, plan, entries, &identities, files)
	if err != nil {
		return err
	}
	if !finalJSONEqual(indexed, lifecycle.Variants) {
		if len(indexed) != len(lifecycle.Variants) {
			return fmt.Errorf("fleet lifecycle replay index count=%d, recorded %d", len(indexed), len(lifecycle.Variants))
		}
		for index := range indexed {
			if !finalJSONEqual(indexed[index], lifecycle.Variants[index]) {
				left, right := indexed[index], lifecycle.Variants[index]
				return fmt.Errorf("fleet lifecycle replay index differs for variant %s: identity=%t members=%t commitment=%t mirror=%t bindings=%t registration_preparation=%t registration=%t cleanups=%t", right.Name,
					left.Name == right.Name && left.FleetID == right.FleetID && left.Hotkey == right.Hotkey && left.Generation == right.Generation && left.ManifestHash == right.ManifestHash,
					finalJSONEqual(left.Members, right.Members), finalJSONEqual(left.Commitment, right.Commitment), finalJSONEqual(left.Mirror, right.Mirror), finalJSONEqual(left.Bindings, right.Bindings),
					finalJSONEqual(left.RegistrationPreparation, right.RegistrationPreparation), finalJSONEqual(left.Registration, right.Registration), finalJSONEqual(left.Cleanups, right.Cleanups))
			}
		}
		return errors.New("fleet lifecycle replay index differs from its exact plan/journal/artifact graph")
	}
	roleSecrets, err := finalFleetLifecycleRoleSecrets(&identities)
	if err != nil {
		return err
	}
	cfg := &ResolvedConfig{Config: &HarnessConfig{Deployment: DeploymentConfig{DeploymentID: evidence.DeploymentID}, Topology: TopologyConfig{
		Operators: evidence.ExpectedOperators, Miners: evidence.ExpectedMiners, Validators: evidence.ExpectedValidators,
		HeadSlots: evidence.ExpectedHeadSlots, HeadFleets: evidence.ExpectedHeadSlots,
		ChallengerFleets: evidence.ExpectedCandidates - evidence.ExpectedHeadSlots, ClientsPerHeadFleet: lifecycle.ClientsPerHeadFleet,
		ChurnFloorUIDs: fleetLifecycleTerminalVictimChurn,
	}}, ChainID: evidence.ChainID, Netuid: evidence.Netuid}
	validator := &liveFleetLifecycle{cfg: cfg, executor: &Executor{cfg: cfg, plan: plan, roles: roleSecrets}}
	if err := validator.validatePersistedStateForPhase(evidence.Phase, evidence.RunID, &state); err != nil {
		return fmt.Errorf("fleet lifecycle reconstructed terminal state: %w", err)
	}
	if evidence.Phase == "release-1.0" {
		stateBytes := files["public/fleet-lifecycle.json"]
		if lifecycle.ReleaseHandoffHash != bytesSHA256(stateBytes) || lifecycle.ReleaseHandoffSize != uint64(len(stateBytes)) {
			return errors.New("fleet lifecycle release handoff hash/size differs from its exact canonical state bytes")
		}
	}
	return nil
}

func finalFleetLifecycleNativeReceipt(transactionHash string, head ChainHead) FinalNativeReceipt {
	return FinalNativeReceipt{ExtrinsicHash: strings.ToLower(transactionHash), Block: ChainHead{Number: head.Number, Hash: strings.ToLower(head.Hash)}}
}

func finalFleetLifecycleEvent(events []FinalFleetLifecycleEventState, kind, transactionHash string, head ChainHead) (FinalFleetLifecycleEventState, error) {
	var found *FinalFleetLifecycleEventState
	for index := range events {
		event := &events[index]
		if event.Kind != kind {
			continue
		}
		if found != nil {
			return FinalFleetLifecycleEventState{}, fmt.Errorf("fleet lifecycle transaction %s duplicates %s", transactionHash, kind)
		}
		copy := *event
		found = &copy
	}
	if found == nil || !strings.EqualFold(found.TransactionHash, transactionHash) || found.Block.Number != head.Number || !strings.EqualFold(found.Block.Hash, head.Hash) {
		return FinalFleetLifecycleEventState{}, fmt.Errorf("fleet lifecycle transaction %s lacks its exact %s event", transactionHash, kind)
	}
	if len(events) != 1 {
		return FinalFleetLifecycleEventState{}, fmt.Errorf("fleet lifecycle transaction %s contains %d recognized mutation events, want one", transactionHash, len(events))
	}
	return *found, nil
}

func finalFleetLifecycleBindingStateMatches(got FinalFleetBindingChainState, binding FleetBindingEvidence, head ChainHead, cleaned bool, cleanedAt uint64) bool {
	return got.Active == !cleaned && got.Cleaned == cleaned && got.CleanedAtEpoch == cleanedAt && got.Generation == binding.Generation && got.ValidFromEpoch == binding.ValidFromEpoch && got.ValidToEpoch == binding.ValidToEpoch && got.UID == binding.UID && got.Block.Number == head.Number && strings.EqualFold(got.Block.Hash, head.Hash) && strings.EqualFold(got.ClientID, binding.ClientID) && strings.EqualFold(got.FleetID, binding.FleetID) && strings.EqualFold(got.Hotkey, binding.Hotkey) && strings.EqualFold(got.ClientKey, binding.ClientKey) && strings.EqualFold(got.CommitmentHash, binding.CommitmentHash)
}

func finalFleetLifecycleVariantMap(lifecycle *FinalFleetLifecycleEvidence) (map[string]*FinalFleetLifecycleVariantEvidence, error) {
	if lifecycle == nil {
		return nil, errors.New("fleet lifecycle replay index is unavailable")
	}
	result := make(map[string]*FinalFleetLifecycleVariantEvidence, len(lifecycle.Variants))
	for index := range lifecycle.Variants {
		variant := &lifecycle.Variants[index]
		if variant.Name == "" || result[variant.Name] != nil {
			return nil, errors.New("fleet lifecycle replay index contains an unnamed or duplicate variant")
		}
		result[variant.Name] = variant
	}
	for _, name := range finalFleetLifecycleVariantNames() {
		if result[name] == nil {
			return nil, fmt.Errorf("fleet lifecycle replay index lacks variant %s", name)
		}
	}
	return result, nil
}

func finalFleetLifecycleCandidateHotkeys(snapshot FleetLifecyclePruneSnapshot, census FleetLifecycleCandidateCensus) error {
	byUID := make(map[uint16]string, len(snapshot.Inputs))
	for _, input := range snapshot.Inputs {
		if _, exists := byUID[input.UID]; exists {
			return fmt.Errorf("public lifecycle candidate snapshot duplicates UID %d", input.UID)
		}
		byUID[input.UID] = input.Hotkey
	}
	if len(census.CandidateUIDs) != len(census.CandidateHotkeys) {
		return errors.New("fleet lifecycle candidate UID/hotkey vectors differ in length")
	}
	for index, uid := range census.CandidateUIDs {
		if observed, ok := byUID[uid]; !ok || !strings.EqualFold(observed, census.CandidateHotkeys[index]) {
			return fmt.Errorf("public lifecycle candidate UID %d has another hotkey", uid)
		}
	}
	return nil
}

func finalFleetLifecycleAppliedWeights(got FinalNativeWeightState, validator FleetLifecycleValidatorCensus) error {
	if got.Block != validator.Application || len(got.UIDs) != len(got.Values) {
		return errors.New("public lifecycle applied vector has an invalid block or vector shape")
	}
	public := make(map[uint16]uint16, len(got.UIDs))
	for index, uid := range got.UIDs {
		if _, exists := public[uid]; exists {
			return fmt.Errorf("public lifecycle applied vector duplicates UID %d", uid)
		}
		public[uid] = got.Values[index]
	}
	if len(validator.AppliedWeights) != len(validator.EligibleUIDs) {
		return errors.New("fleet lifecycle candidate weight census is incomplete")
	}
	for _, expected := range validator.AppliedWeights {
		value, exists := public[expected.UID]
		if value != expected.Value || !exists && expected.Value != 0 {
			return fmt.Errorf("public lifecycle applied weight for UID %d=%d, want %d", expected.UID, value, expected.Value)
		}
	}
	return nil
}

// executeFinalSemanticLifecycleOnChain independently replays every lifecycle
// mutation and every captured applied decision against immutable public archive
// heads. The signed plan/journal graph proves authorization; these reads prove
// that the authorized mutations and vectors actually became canonical state.
func executeFinalSemanticLifecycleOnChain(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader, appendExchanges finalLifecycleAppendExchanges) error {
	if evidence == nil || evidence.FleetLifecycle == nil {
		return nil
	}
	if ctx == nil || reader == nil || appendExchanges == nil {
		return errors.New("fleet lifecycle public replay context is incomplete")
	}
	lifecycleReader, ok := reader.(FinalSemanticLifecycleChainReader)
	if !ok {
		return errors.New("public reader does not implement strict fleet lifecycle replay")
	}
	lifecycle := evidence.FleetLifecycle
	state := &lifecycle.State
	variants, err := finalFleetLifecycleVariantMap(lifecycle)
	if err != nil {
		return err
	}
	for _, registration := range []*FleetLifecycleRegistrationEvidence{state.FallbackRegistration, state.ProviderRegistration, state.TerminalRegistration} {
		if registration == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		pre, exchanges, err := lifecycleReader.NativePruneSnapshot(ctx, evidence.Netuid, registration.PrePrune.Head)
		if err != nil {
			return fmt.Errorf("public fleet lifecycle pre-registration snapshot %s: %w", registration.ActionID, err)
		}
		if err := appendExchanges("substrate", registration.PrePrune.Head, exchanges); err != nil {
			return err
		}
		post, exchanges, err := lifecycleReader.NativePruneSnapshot(ctx, evidence.Netuid, registration.PostRegistration.Head)
		if err != nil {
			return fmt.Errorf("public fleet lifecycle post-registration snapshot %s: %w", registration.ActionID, err)
		}
		if err := appendExchanges("substrate", registration.PostRegistration.Head, exchanges); err != nil {
			return err
		}
		if !finalJSONEqual(pre, registration.PrePrune) || !finalJSONEqual(post, registration.PostRegistration) {
			return fmt.Errorf("public fleet lifecycle registration %s has another exact pre/post UID census", registration.ActionID)
		}
		receipt := finalFleetLifecycleNativeReceipt(registration.TransactionHash, registration.PostRegistration.Head)
		if err := verifyFinalNativeEventOnChain(ctx, reader, receipt, "registration", appendExchanges); err != nil {
			return fmt.Errorf("public fleet lifecycle registration %s: %w", registration.ActionID, err)
		}
	}
	for _, name := range finalFleetLifecycleVariantNames() {
		variant := variants[name]
		if err := ctx.Err(); err != nil {
			return err
		}
		commitHead := ChainHead{Number: variant.Commitment.FinalizedBlock, Hash: strings.ToLower(variant.Commitment.FinalizedBlockHash)}
		if err := verifyFinalNativeEventOnChain(ctx, reader, finalFleetLifecycleNativeReceipt(variant.Commitment.ExtrinsicHash, commitHead), "commitment", appendExchanges); err != nil {
			return fmt.Errorf("public fleet lifecycle %s commitment dispatch: %w", name, err)
		}
		commitment, exchanges, err := lifecycleReader.NativeFleetCommitment(ctx, evidence.Netuid, variant.Hotkey, commitHead)
		if err != nil {
			return fmt.Errorf("public fleet lifecycle %s native commitment: %w", name, err)
		}
		if err := appendExchanges("substrate", commitHead, exchanges); err != nil {
			return err
		}
		if commitment.Block != commitHead || commitment.CommitmentBlock != variant.Commitment.CommitmentBlock || !strings.EqualFold(commitment.Hotkey, variant.Hotkey) || !strings.EqualFold(commitment.CommitmentHash, variant.Commitment.CommitmentHash) {
			return fmt.Errorf("public fleet lifecycle %s native commitment differs from the captured manifest", name)
		}

		mirrorHead := ChainHead{Number: variant.Mirror.BlockNumber, Hash: strings.ToLower(variant.Mirror.BlockHash)}
		events, eventExchanges, err := lifecycleReader.FleetLifecycleEvents(ctx, variant.Mirror.TransactionHash, mirrorHead)
		if err != nil {
			return fmt.Errorf("public fleet lifecycle %s mirror receipt: %w", name, err)
		}
		if err := appendExchanges("evm", mirrorHead, eventExchanges); err != nil {
			return err
		}
		event, err := finalFleetLifecycleEvent(events, "commitment-mirrored", variant.Mirror.TransactionHash, mirrorHead)
		if err != nil || !strings.EqualFold(event.Hotkey, variant.Mirror.Hotkey) || !strings.EqualFold(event.CommitmentHash, variant.Mirror.CommitmentHash) || event.FinalizedBlock != variant.Mirror.FinalizedBlock || !strings.EqualFold(event.FinalizedBlockHash, variant.Mirror.FinalizedBlockHash) {
			return stateMismatchError(err, "public fleet lifecycle %s mirror event differs", name)
		}
		mirror, exchanges, err := lifecycleReader.FleetMirror(ctx, variant.Hotkey, mirrorHead)
		if err != nil {
			return fmt.Errorf("public fleet lifecycle %s mirror state: %w", name, err)
		}
		if err := appendExchanges("evm", mirrorHead, exchanges); err != nil {
			return err
		}
		if mirror.Block != mirrorHead || !strings.EqualFold(mirror.Hotkey, variant.Mirror.Hotkey) || !strings.EqualFold(mirror.CommitmentHash, variant.Mirror.CommitmentHash) || mirror.FinalizedBlock != variant.Mirror.FinalizedBlock || !strings.EqualFold(mirror.FinalizedBlockHash, variant.Mirror.FinalizedBlockHash) {
			return fmt.Errorf("public fleet lifecycle %s mirror state differs", name)
		}

		for memberIndex := range variant.Bindings {
			binding := variant.Bindings[memberIndex]
			bindingHead := ChainHead{Number: binding.BlockNumber, Hash: strings.ToLower(binding.BlockHash)}
			events, eventExchanges, err := lifecycleReader.FleetLifecycleEvents(ctx, binding.TransactionHash, bindingHead)
			if err != nil {
				return fmt.Errorf("public fleet lifecycle %s binding %d receipt: %w", name, memberIndex+1, err)
			}
			if err := appendExchanges("evm", bindingHead, eventExchanges); err != nil {
				return err
			}
			event, err := finalFleetLifecycleEvent(events, "fleet-bound", binding.TransactionHash, bindingHead)
			if err != nil || !strings.EqualFold(event.ClientID, binding.ClientID) || !strings.EqualFold(event.FleetID, binding.FleetID) || !strings.EqualFold(event.Hotkey, binding.Hotkey) || event.Generation != binding.Generation || event.UID != binding.UID || event.ValidFromEpoch != binding.ValidFromEpoch || event.ValidToEpoch != binding.ValidToEpoch {
				return stateMismatchError(err, "public fleet lifecycle %s binding %d event differs", name, memberIndex+1)
			}
			bound, exchanges, err := lifecycleReader.FleetBinding(ctx, binding.ClientID, binding.ValidFromEpoch, bindingHead)
			if err != nil {
				return fmt.Errorf("public fleet lifecycle %s binding %d state: %w", name, memberIndex+1, err)
			}
			if err := appendExchanges("evm", bindingHead, exchanges); err != nil {
				return err
			}
			if !finalFleetLifecycleBindingStateMatches(bound, binding, bindingHead, false, 0) {
				return fmt.Errorf("public fleet lifecycle %s binding %d state differs", name, memberIndex+1)
			}
		}
		bindingByClient := make(map[string]FleetBindingEvidence, len(variant.Bindings))
		for _, binding := range variant.Bindings {
			bindingByClient[strings.ToLower(binding.ClientID)] = binding
		}
		for cleanupIndex := range variant.Cleanups {
			cleanup := variant.Cleanups[cleanupIndex]
			binding, exists := bindingByClient[strings.ToLower(cleanup.ClientID)]
			if !exists {
				return fmt.Errorf("fleet lifecycle %s cleanup %d has no exact prior binding", name, cleanupIndex+1)
			}
			beforeHead := ChainHead{Number: cleanup.BeforeBlock.Number, Hash: strings.ToLower(cleanup.BeforeBlock.Hash)}
			cleanupHead := ChainHead{Number: cleanup.BlockNumber, Hash: strings.ToLower(cleanup.BlockHash)}
			if cleanupHead.Number == 0 || beforeHead.Number != cleanupHead.Number-1 {
				return fmt.Errorf("fleet lifecycle %s cleanup %d has no exact parent checkpoint", name, cleanupIndex+1)
			}
			before, exchanges, err := lifecycleReader.FleetBindingRecord(ctx, cleanup.ClientID, beforeHead)
			if err != nil {
				return fmt.Errorf("public fleet lifecycle %s cleanup %d pre-state: %w", name, cleanupIndex+1, err)
			}
			if err := appendExchanges("evm", beforeHead, exchanges); err != nil {
				return err
			}
			if !finalFleetLifecycleBindingStateMatches(before, binding, beforeHead, false, 0) {
				return fmt.Errorf("public fleet lifecycle %s cleanup %d pre-state differs", name, cleanupIndex+1)
			}
			beforeCount, exchanges, err := lifecycleReader.FleetMemberCount(ctx, cleanup.FleetID, beforeHead)
			if err != nil {
				return fmt.Errorf("public fleet lifecycle %s cleanup %d pre-count: %w", name, cleanupIndex+1, err)
			}
			if err := appendExchanges("evm", beforeHead, exchanges); err != nil {
				return err
			}
			if beforeCount != cleanup.MemberCountBefore {
				return fmt.Errorf("public fleet lifecycle %s cleanup %d member count before=%d, want %d", name, cleanupIndex+1, beforeCount, cleanup.MemberCountBefore)
			}
			events, eventExchanges, err := lifecycleReader.FleetLifecycleEvents(ctx, cleanup.TransactionHash, cleanupHead)
			if err != nil {
				return fmt.Errorf("public fleet lifecycle %s cleanup %d receipt: %w", name, cleanupIndex+1, err)
			}
			if err := appendExchanges("evm", cleanupHead, eventExchanges); err != nil {
				return err
			}
			event, err := finalFleetLifecycleEvent(events, "fleet-binding-cleaned", cleanup.TransactionHash, cleanupHead)
			if err != nil || !strings.EqualFold(event.ClientID, cleanup.ClientID) || event.CleanedAtEpoch != cleanup.CleanedAtEpoch {
				return stateMismatchError(err, "public fleet lifecycle %s cleanup %d event differs", name, cleanupIndex+1)
			}
			after, exchanges, err := lifecycleReader.FleetBindingRecord(ctx, cleanup.ClientID, cleanupHead)
			if err != nil {
				return fmt.Errorf("public fleet lifecycle %s cleanup %d post-state: %w", name, cleanupIndex+1, err)
			}
			if err := appendExchanges("evm", cleanupHead, exchanges); err != nil {
				return err
			}
			if !finalFleetLifecycleBindingStateMatches(after, binding, cleanupHead, true, cleanup.CleanedAtEpoch) {
				return fmt.Errorf("public fleet lifecycle %s cleanup %d post-state differs", name, cleanupIndex+1)
			}
			afterCount, exchanges, err := lifecycleReader.FleetMemberCount(ctx, cleanup.FleetID, cleanupHead)
			if err != nil {
				return fmt.Errorf("public fleet lifecycle %s cleanup %d post-count: %w", name, cleanupIndex+1, err)
			}
			if err := appendExchanges("evm", cleanupHead, exchanges); err != nil {
				return err
			}
			if afterCount != cleanup.MemberCountAfter {
				return fmt.Errorf("public fleet lifecycle %s cleanup %d member count after=%d, want %d", name, cleanupIndex+1, afterCount, cleanup.MemberCountAfter)
			}
		}
	}

	pruneAt := make(map[ChainHead]FleetLifecyclePruneSnapshot)
	validatorUID := make(map[int]uint16, len(evidence.Validators))
	for _, validator := range evidence.Validators {
		validatorUID[int(validator.ValidatorID)] = validator.UID
	}
	for censusIndex := range state.CandidateCensuses {
		census := state.CandidateCensuses[censusIndex]
		for validatorIndex := range census.Validators {
			validator := census.Validators[validatorIndex]
			uid, exists := validatorUID[validator.ValidatorID]
			if !exists {
				return fmt.Errorf("fleet lifecycle census names unknown validator %d", validator.ValidatorID)
			}
			snapshot, cached := pruneAt[validator.NativeSnapshot]
			if !cached {
				var exchanges []FinalRPCExchange
				snapshot, exchanges, err = lifecycleReader.NativePruneSnapshot(ctx, evidence.Netuid, validator.NativeSnapshot)
				if err != nil {
					return fmt.Errorf("public fleet lifecycle census %d validator %d candidate snapshot: %w", censusIndex+1, validator.ValidatorID, err)
				}
				if err := appendExchanges("substrate", validator.NativeSnapshot, exchanges); err != nil {
					return err
				}
				pruneAt[validator.NativeSnapshot] = snapshot
			}
			if err := finalFleetLifecycleCandidateHotkeys(snapshot, census); err != nil {
				return fmt.Errorf("fleet lifecycle census %d validator %d: %w", censusIndex+1, validator.ValidatorID, err)
			}
			if err := verifyFinalNativeEventOnChain(ctx, reader, finalFleetLifecycleNativeReceipt(validator.ExtrinsicHash, validator.Commit), "commit", appendExchanges); err != nil {
				return fmt.Errorf("public fleet lifecycle census %d validator %d commit: %w", censusIndex+1, validator.ValidatorID, err)
			}
			if err := verifyFinalNativeEventOnChain(ctx, reader, finalFleetLifecycleNativeReceipt("", ChainHead{Number: validator.RevealBlock, Hash: validator.RevealBlockHash}), "reveal", appendExchanges); err != nil {
				return fmt.Errorf("public fleet lifecycle census %d validator %d reveal: %w", censusIndex+1, validator.ValidatorID, err)
			}
			if err := verifyFinalNativeEventOnChain(ctx, reader, finalFleetLifecycleNativeReceipt("", validator.Application), "application", appendExchanges); err != nil {
				return fmt.Errorf("public fleet lifecycle census %d validator %d application: %w", censusIndex+1, validator.ValidatorID, err)
			}
			weights, exchanges, err := reader.NativeWeights(ctx, evidence.Netuid, uid, validator.Application)
			if err != nil {
				return fmt.Errorf("public fleet lifecycle census %d validator %d applied weights: %w", censusIndex+1, validator.ValidatorID, err)
			}
			if err := appendExchanges("substrate", validator.Application, exchanges); err != nil {
				return err
			}
			if weights.ValidatorUID != uid {
				return fmt.Errorf("public fleet lifecycle census %d resolves validator %d to UID %d, want %d", censusIndex+1, validator.ValidatorID, weights.ValidatorUID, uid)
			}
			if err := finalFleetLifecycleAppliedWeights(weights, validator); err != nil {
				return fmt.Errorf("public fleet lifecycle census %d validator %d: %w", censusIndex+1, validator.ValidatorID, err)
			}
		}
	}
	return nil
}

func finalFleetLifecycleRole(lifecycle *FinalFleetLifecycleEvidence, label string) (FinalFleetLifecycleRole, error) {
	if lifecycle == nil {
		return FinalFleetLifecycleRole{}, errors.New("fleet lifecycle roles are unavailable")
	}
	for _, role := range lifecycle.Roles {
		if role.Label == label {
			return role, nil
		}
	}
	return FinalFleetLifecycleRole{}, fmt.Errorf("fleet lifecycle role %s is absent", label)
}

// finalFleetLifecycleHeadAt returns the exact owner/UID for a logical provider
// at one coordinator settlement epoch. Native subnet epochs are deliberately
// not used here: the two counters have independent cadences, and every signed
// validator cycle carries the settlement epoch at which its binding snapshot
// was taken. This prevents both terminal-owner backdating and accidental
// ordinal-offset arithmetic across the two domains.
func finalFleetLifecycleHeadAt(lifecycle *FinalFleetLifecycleEvidence, fleetID, epoch uint64) (uint16, string, string, error) {
	if lifecycle == nil || fleetID != fleetLifecycleTargetFleet && fleetID != fleetLifecycleCompanionFleet {
		return 0, "", "", errors.New("fleet lifecycle head lookup is unavailable")
	}
	state := &lifecycle.State
	if state.TakeoverEffectiveEpoch == 0 || state.FallbackEffectiveEpoch <= state.TakeoverEffectiveEpoch || state.ProviderEffectiveEpoch <= state.FallbackEffectiveEpoch || state.TerminalEffectiveEpoch <= state.ProviderEffectiveEpoch {
		return 0, "", "", errors.New("fleet lifecycle settlement-epoch transitions are incomplete or unordered")
	}
	if epoch < state.TakeoverEffectiveEpoch {
		return 0, "", "", fmt.Errorf("fleet lifecycle settlement epoch %d predates takeover epoch %d", epoch, state.TakeoverEffectiveEpoch)
	}
	churn, uid := fleetLifecycleTargetChurn, uint16(fleetLifecycleTargetExpectedUID)
	if fleetID == fleetLifecycleCompanionFleet {
		churn, uid = fleetLifecycleCompanionChurn, fleetLifecycleCompanionExpectedUID
	}
	if epoch >= state.FallbackEffectiveEpoch && fleetID == fleetLifecycleTargetFleet && epoch < state.ProviderEffectiveEpoch {
		churn, uid = fleetLifecycleFallbackChurn, fleetLifecycleTargetExpectedUID
	}
	if epoch >= state.ProviderEffectiveEpoch {
		if fleetID == fleetLifecycleTargetFleet {
			churn, uid = fleetLifecycleTargetChurn, fleetLifecycleCompanionExpectedUID
		} else if epoch < state.TerminalEffectiveEpoch {
			churn, uid = fleetLifecycleFallbackChurn, fleetLifecycleTargetExpectedUID
		} else {
			churn, uid = fleetLifecycleCompanionChurn, fleetLifecycleTerminalVictimUID
		}
	}
	hotkey, err := finalFleetLifecycleRole(lifecycle, churnHotkeyLabel(churn))
	if err != nil {
		return 0, "", "", err
	}
	coldkey, err := finalFleetLifecycleRole(lifecycle, churnColdkeyLabel(churn))
	if err != nil {
		return 0, "", "", err
	}
	return uid, hotkey.PublicKey, coldkey.PublicKey, nil
}
