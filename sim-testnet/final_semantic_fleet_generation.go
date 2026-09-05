package main

// final_semantic_fleet_generation.go seals the ordinary fleet setup lineage.
// The terminal fleet census proves what is active; this companion proof also
// proves how every one of the 200 setup fleets reached generation two without
// silently replacing, skipping, or inventing a generation.

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	finalFleetGenerationLineageSchema        = "urnetwork-final-fleet-generation-lineage-v1"
	finalFleetGenerationSetupFleetCount      = uint64(200)
	finalFleetGenerationChallengerFleetCount = uint64(2)
	finalFleetGenerationBatchSize            = uint64(10)
	finalFleetGenerationBatchCount           = finalFleetGenerationSetupFleetCount / finalFleetGenerationBatchSize
	finalFleetGenerationMembersPerFleet      = uint64(4)
	finalPublicFleetGenerationAuditSchema    = "urnetwork-final-public-fleet-generation-audit-v1"
)

// FinalPublicFleetGenerationAudit seals the public replay scope for setup
// lineage independently of the raw RPC transcript. It makes an omitted batch
// or predecessor history set structurally visible to an offline reviewer.
type FinalPublicFleetGenerationAudit struct {
	Schema           string `json:"schema"`
	SetupFleets      uint64 `json:"setup_fleets"`
	Generations      uint64 `json:"generations"`
	Batches          uint64 `json:"batches"`
	CarriedWrites    uint64 `json:"carried_writes"`
	ChallengerFleets uint64 `json:"challenger_fleets"`
	ProjectionHash   string `json:"projection_hash"`
}

// finalFleetGenerationAuditProjection is the compact deterministic preimage
// used to seal the exact source namespace before public archive replay.
type finalFleetGenerationAuditProjection struct {
	Schema           string                                   `json:"schema"`
	Batches          []FinalFleetGenerationBatchEvidence      `json:"batches"`
	SetupFleets      []FinalFleetGenerationFleetEvidence      `json:"setup_fleets"`
	ChallengerFleets []FinalFleetGenerationChallengerEvidence `json:"challenger_fleets"`
}

// finalFleetGenerationLineageArtifact retains the exact immutable public
// inputs used to reconstruct every setup-generation edge. Keeping the source
// graph in one content-addressed object means an offline reviewer can replay
// predecessor-plan intent acceptance without consulting a mutable run tree.
type finalFleetGenerationLineageArtifact struct {
	Schema       string                            `json:"schema"`
	DeploymentID string                            `json:"deployment_id"`
	PlanHash     string                            `json:"plan_hash"`
	Files        []finalFleetGenerationLineageFile `json:"files"`
}

// finalFleetGenerationLineageFile records one source byte stream with its
// canonical run-relative name. The ordered list deliberately rejects both an
// omitted predecessor plan and an unreferenced duplicate source object.
type finalFleetGenerationLineageFile struct {
	Path        string `json:"path"`
	ContentHash string `json:"content_sha256"`
	SizeBytes   uint64 `json:"size_bytes"`
	Data        []byte `json:"data"`
}

// derives the public replay summary only from the already validated semantic
// lineage, rather than letting a producer report independent count claims.
func finalPublicFleetGenerationAuditForEvidence(evidence *FinalSemanticEvidence) (FinalPublicFleetGenerationAudit, error) {
	if evidence == nil || evidence.FleetGeneration == nil {
		return FinalPublicFleetGenerationAudit{}, errors.New("ordinary fleet generation lineage is unavailable for public audit")
	}
	lineage := evidence.FleetGeneration
	if err := verifyFinalFleetGenerationLineage(evidence, lineage); err != nil {
		return FinalPublicFleetGenerationAudit{}, err
	}
	var carriedWrites uint64
	for _, batch := range lineage.Batches {
		if uint64(len(batch.CarriedHistory)) > ^uint64(0)-carriedWrites {
			return FinalPublicFleetGenerationAudit{}, errors.New("ordinary fleet generation carried-write count overflows uint64")
		}
		carriedWrites += uint64(len(batch.CarriedHistory))
	}
	projection := finalFleetGenerationAuditProjection{
		Schema:           finalPublicFleetGenerationAuditSchema,
		Batches:          append([]FinalFleetGenerationBatchEvidence(nil), lineage.Batches...),
		SetupFleets:      append([]FinalFleetGenerationFleetEvidence(nil), lineage.SetupFleets...),
		ChallengerFleets: append([]FinalFleetGenerationChallengerEvidence(nil), lineage.ChallengerFleets...),
	}
	projectionHash, err := canonicalHashHex(projection)
	if err != nil {
		return FinalPublicFleetGenerationAudit{}, err
	}
	return FinalPublicFleetGenerationAudit{
		Schema:      finalPublicFleetGenerationAuditSchema,
		SetupFleets: uint64(len(lineage.SetupFleets)), Generations: uint64(len(lineage.SetupFleets) * 2),
		Batches: uint64(len(lineage.Batches)), CarriedWrites: carriedWrites,
		ChallengerFleets: uint64(len(lineage.ChallengerFleets)), ProjectionHash: projectionHash,
	}, nil
}

// rejects an absent or arithmetic-inconsistent summary before recomputing its
// projection. The expected dimensions intentionally remain constants.
func verifyFinalPublicFleetGenerationAuditShape(audit FinalPublicFleetGenerationAudit) error {
	if audit.Schema != finalPublicFleetGenerationAuditSchema || audit.SetupFleets != finalFleetGenerationSetupFleetCount || audit.Generations != finalFleetGenerationSetupFleetCount*2 || audit.Batches != finalFleetGenerationBatchCount*2 || audit.ChallengerFleets != finalFleetGenerationChallengerFleetCount {
		return errors.New("public ordinary fleet generation audit summary is incomplete")
	}
	return requireFinalHex32("public ordinary fleet generation audit projection hash", audit.ProjectionHash)
}

// compares a transcript summary to the exact artifact-bound source scope.
func verifyFinalPublicFleetGenerationAudit(evidence *FinalSemanticEvidence, audit FinalPublicFleetGenerationAudit) error {
	if err := verifyFinalPublicFleetGenerationAuditShape(audit); err != nil {
		return err
	}
	want, err := finalPublicFleetGenerationAuditForEvidence(evidence)
	if err != nil {
		return err
	}
	if audit != want {
		return errors.New("public ordinary fleet generation audit differs from sealed projection")
	}
	return nil
}

// FinalFleetGenerationLineageEvidence records both immutable setup
// generations and the two challenger registrations. The public audit replays
// this sealed scope against archived native and EVM state.
type FinalFleetGenerationLineageEvidence struct {
	Schema           string                                   `json:"schema"`
	Batches          []FinalFleetGenerationBatchEvidence      `json:"batches"`
	SetupFleets      []FinalFleetGenerationFleetEvidence      `json:"setup_fleets"`
	ChallengerFleets []FinalFleetGenerationChallengerEvidence `json:"challenger_fleets"`
	Artifact         FinalArtifactLocator                     `json:"artifact"`
}

// FinalFleetGenerationFleetEvidence binds one setup fleet's exact generation
// one to generation two replacement. There is deliberately no open-ended
// generation slice: a terminal generation three is an invalid release shape.
type FinalFleetGenerationFleetEvidence struct {
	FleetID uint64                              `json:"fleet_id"`
	Initial FinalFleetGenerationVersionEvidence `json:"initial"`
	Refresh FinalFleetGenerationVersionEvidence `json:"refresh"`
}

// FinalFleetGenerationChallengerEvidence preserves the generation-one-only
// challenger registration and its authenticated tournament transition.
type FinalFleetGenerationChallengerEvidence struct {
	FleetID      uint64                              `json:"fleet_id"`
	Initial      FinalFleetGenerationVersionEvidence `json:"initial"`
	Registration FinalNativeReceipt                  `json:"registration"`
	Transition   FinalArtifactLocator                `json:"transition"`
}

// FinalFleetGenerationVersionEvidence joins a signed manifest, native
// commitment, and every member binding for one immutable generation.
type FinalFleetGenerationVersionEvidence struct {
	Generation              uint64                               `json:"generation"`
	Batch                   uint64                               `json:"batch"`
	Manifest                FinalArtifactLocator                 `json:"manifest"`
	Commitment              FinalArtifactLocator                 `json:"commitment"`
	CommitmentAction        FinalFleetGenerationActionEvidence   `json:"commitment_action"`
	CommitmentExtrinsicHash string                               `json:"commitment_extrinsic_hash"`
	CommitmentPostcondition FinalArtifactLocator                 `json:"commitment_postcondition"`
	CommitmentHash          string                               `json:"commitment_hash"`
	Hotkey                  string                               `json:"hotkey"`
	NativeHead              ChainHead                            `json:"native_head"`
	Members                 []FinalFleetGenerationMemberEvidence `json:"members"`
}

// FinalFleetGenerationMemberEvidence binds one stable client identity across
// the two signed generations and their exact validity interval.
type FinalFleetGenerationMemberEvidence struct {
	Member         uint64 `json:"member"`
	ClientID       string `json:"client_id"`
	ClientKey      string `json:"client_key"`
	FleetKey       string `json:"fleet_key"`
	Hotkey         string `json:"hotkey"`
	CommitmentHash string `json:"commitment_hash"`
	Generation     uint64 `json:"generation"`
	ValidFromEpoch uint64 `json:"valid_from_epoch"`
	ValidToEpoch   uint64 `json:"valid_to_epoch"`
	UID            uint16 `json:"uid"`
}

// FinalFleetGenerationBatchEvidence retains the plan action and exact write
// proof for a ten-fleet partition. Generation one carries an explicit,
// ordered installed/carried partition: installed fleets share one atomic
// batcher receipt while carried fleets retain individual predecessor writes.
// This permits the authenticated batch-three split without allowing an
// unaccounted-for fleet to move between the two provenance domains.
type FinalFleetGenerationBatchEvidence struct {
	Batch                  uint64                              `json:"batch"`
	Generation             uint64                              `json:"generation"`
	FirstFleet             uint64                              `json:"first_fleet"`
	LastFleet              uint64                              `json:"last_fleet"`
	Action                 FinalFleetGenerationActionEvidence  `json:"action"`
	Carried                bool                                `json:"carried"`
	CalldataHash           string                              `json:"calldata_hash,omitempty"`
	EventHash              string                              `json:"event_hash,omitempty"`
	CoordinatorRuntimeHash string                              `json:"coordinator_runtime_hash,omitempty"`
	BatcherRuntimeHash     string                              `json:"batcher_runtime_hash,omitempty"`
	BatchWrite             *FinalFleetGenerationWriteEvidence  `json:"batch_write,omitempty"`
	InstalledFleets        []uint64                            `json:"installed_fleets,omitempty"`
	CarriedFleets          []uint64                            `json:"carried_fleets,omitempty"`
	CarriedHistory         []FinalFleetGenerationWriteEvidence `json:"carried_history,omitempty"`
	Postcondition          FinalArtifactLocator                `json:"postcondition"`
}

// FinalFleetGenerationActionEvidence identifies the approved action that
// owns a batch, including an accepted predecessor-plan intent when carried.
type FinalFleetGenerationActionEvidence struct {
	ActionID   string `json:"action_id"`
	PlanHash   string `json:"plan_hash"`
	IntentHash string `json:"intent_hash"`
}

// FinalFleetGenerationWriteEvidence carries one exact EVM mutation or
// historical mutation. Calldata and decoded event projections are hashed
// separately so a receipt hash alone cannot hide a substituted payload.
type FinalFleetGenerationWriteEvidence struct {
	Action                        FinalFleetGenerationActionEvidence  `json:"action"`
	Receipt                       FinalEVMReceipt                     `json:"receipt"`
	Calldata                      string                              `json:"calldata"`
	CalldataHash                  string                              `json:"calldata_hash"`
	EventHash                     string                              `json:"event_hash"`
	Events                        []FinalFleetGenerationEventEvidence `json:"events"`
	CoordinatorProxy              string                              `json:"coordinator_proxy"`
	CoordinatorImplementation     string                              `json:"coordinator_implementation"`
	CoordinatorImplementationSlot string                              `json:"coordinator_implementation_slot"`
	CoordinatorProxyRuntimeHash   string                              `json:"coordinator_proxy_runtime_hash"`
	CoordinatorRuntimeHash        string                              `json:"coordinator_runtime_hash"`
	BatcherAddress                string                              `json:"batcher_address,omitempty"`
	BatcherRuntimeHash            string                              `json:"batcher_runtime_hash,omitempty"`
	Postcondition                 FinalArtifactLocator                `json:"postcondition"`
	EVMHead                       ChainHead                           `json:"evm_head"`
	NativeHead                    ChainHead                           `json:"native_head"`
}

// FinalFleetGenerationRuntimeState is a focused historical proxy dispatch
// observation for one fleet write. Carried predecessor mutations deliberately
// use this narrower proof instead of the terminal release-root census, whose
// batcher and replacement implementation did not yet exist at those heads.
type FinalFleetGenerationRuntimeState struct {
	CoordinatorProxy              string    `json:"coordinator_proxy"`
	CoordinatorImplementation     string    `json:"coordinator_implementation"`
	CoordinatorImplementationSlot string    `json:"coordinator_implementation_slot"`
	CoordinatorProxyRuntimeHash   string    `json:"coordinator_proxy_runtime_hash"`
	CoordinatorRuntimeHash        string    `json:"coordinator_runtime_hash"`
	BatcherAddress                string    `json:"batcher_address,omitempty"`
	BatcherRuntimeHash            string    `json:"batcher_runtime_hash,omitempty"`
	Block                         ChainHead `json:"block"`
}

// FinalFleetGenerationEventEvidence describes one release-contract event in
// receipt order. The public verifier rejects every unrepresented coordinator
// or batcher event instead of treating a receipt hash as an opaque witness.
type FinalFleetGenerationEventEvidence struct {
	Contract           string               `json:"contract"`
	Kind               string               `json:"kind"`
	Log                finalCanonicalEVMLog `json:"log"`
	FleetID            string               `json:"fleet_id,omitempty"`
	ClientID           string               `json:"client_id,omitempty"`
	Hotkey             string               `json:"hotkey,omitempty"`
	CommitmentHash     string               `json:"commitment_hash,omitempty"`
	Generation         uint64               `json:"generation,omitempty"`
	ValidFromEpoch     uint64               `json:"valid_from_epoch,omitempty"`
	ValidToEpoch       uint64               `json:"valid_to_epoch,omitempty"`
	FinalizedBlock     uint64               `json:"finalized_block,omitempty"`
	FinalizedBlockHash string               `json:"finalized_block_hash,omitempty"`
	UID                uint16               `json:"uid,omitempty"`
	MemberCount        uint64               `json:"member_count,omitempty"`
}

// verifies the immutable release topology before any public-RPC replay. It
// intentionally treats a missing historical carried write as a hard failure:
// no current terminal state can recreate a lost predecessor transaction.
func verifyFinalFleetGenerationLineage(evidence *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence) error {
	if evidence == nil || lineage == nil {
		return errors.New("ordinary fleet generation lineage is unavailable")
	}
	if lineage.Schema != finalFleetGenerationLineageSchema {
		return fmt.Errorf("ordinary fleet generation lineage schema %q is unsupported", lineage.Schema)
	}
	if err := verifyFinalArtifact("ordinary fleet generation lineage", lineage.Artifact, "fleet-generation-lineage"); err != nil {
		return err
	}
	if evidence.ExpectedCandidates != int(finalFleetGenerationSetupFleetCount+finalFleetGenerationChallengerFleetCount) || evidence.ExpectedHeadSlots != int(finalFleetGenerationSetupFleetCount) || evidence.ExpectedMiners != 1000 {
		return errors.New("ordinary fleet generation topology is not the 1000-miner 202/200 release shape")
	}
	if len(lineage.Batches) != int(finalFleetGenerationBatchCount*2) || len(lineage.SetupFleets) != int(finalFleetGenerationSetupFleetCount) || len(lineage.ChallengerFleets) != int(finalFleetGenerationChallengerFleetCount) {
		return errors.New("ordinary fleet generation lineage has an incomplete topology partition")
	}
	if err := verifyFinalFleetGenerationTopologyBoundary(evidence); err != nil {
		return err
	}
	if err := verifyFinalFleetGenerationBatches(evidence, lineage.Batches); err != nil {
		return err
	}
	if err := verifyFinalFleetGenerationSetupFleets(evidence, lineage); err != nil {
		return err
	}
	return verifyFinalFleetGenerationChallengers(evidence, lineage)
}

// confirms that lineage retains the same fixed 202-candidate namespace as
// the terminal census. Lifecycle fleets five and six are continuations of the
// setup partition, not a third partition that could hide a missing fleet.
func verifyFinalFleetGenerationTopologyBoundary(evidence *FinalSemanticEvidence) error {
	if len(evidence.HeadFleets) != int(finalFleetGenerationSetupFleetCount+finalFleetGenerationChallengerFleetCount) || len(evidence.HeadTransitions) != int(finalFleetGenerationChallengerFleetCount) {
		return errors.New("ordinary fleet generation terminal topology is incomplete")
	}
	for index, fleet := range evidence.HeadFleets {
		if fleet.FleetID != uint64(index+1) {
			return fmt.Errorf("ordinary fleet generation terminal fleet %d is missing, reordered, or duplicated", index+1)
		}
	}
	for index, transition := range evidence.HeadTransitions {
		if transition.ChallengerFleetID != finalFleetGenerationSetupFleetCount+uint64(index)+1 {
			return fmt.Errorf("ordinary fleet generation challenger transition %d is missing, reordered, or duplicated", index+1)
		}
	}
	return nil
}

// validates all forty fixed ten-fleet partitions and their action identities.
func verifyFinalFleetGenerationBatches(evidence *FinalSemanticEvidence, batches []FinalFleetGenerationBatchEvidence) error {
	batchesByGenerationBatch := make(map[string]FinalFleetGenerationBatchEvidence, len(batches))
	for index, batch := range batches {
		wantGeneration := uint64(1)
		wantBatch := uint64(index + 1)
		if wantBatch > finalFleetGenerationBatchCount {
			wantGeneration = 2
			wantBatch -= finalFleetGenerationBatchCount
		}
		if batch.Generation != wantGeneration || batch.Batch != wantBatch {
			return fmt.Errorf("ordinary fleet generation batch %d is reordered, missing, or duplicated", index)
		}
		if batch.Generation != 1 && batch.Generation != 2 || batch.Batch == 0 || batch.Batch > finalFleetGenerationBatchCount {
			return errors.New("ordinary fleet generation batch coordinate is invalid")
		}
		key := finalFleetGenerationBatchKey(batch.Generation, batch.Batch)
		if _, duplicate := batchesByGenerationBatch[key]; duplicate {
			return fmt.Errorf("ordinary fleet generation batch %s is duplicated", key)
		}
		firstFleet := (batch.Batch-1)*finalFleetGenerationBatchSize + 1
		lastFleet := firstFleet + finalFleetGenerationBatchSize - 1
		if batch.FirstFleet != firstFleet || batch.LastFleet != lastFleet {
			return fmt.Errorf("ordinary fleet generation batch %s range is not canonical", key)
		}
		wantActionID := finalFleetGenerationActionID(batch.Generation, batch.Batch)
		if batch.Action.ActionID != wantActionID || batch.Action.PlanHash == "" || batch.Action.IntentHash == "" {
			return fmt.Errorf("ordinary fleet generation batch %s action is incomplete", key)
		}
		if err := requireFinalHex32("ordinary fleet generation batch plan", batch.Action.PlanHash); err != nil {
			return err
		}
		if err := requireFinalHex32("ordinary fleet generation batch intent", batch.Action.IntentHash); err != nil {
			return err
		}
		if batch.Generation == 1 {
			if err := verifyFinalFleetGenerationInitialPartition(evidence, batch); err != nil {
				return fmt.Errorf("ordinary fleet generation batch %s: %w", key, err)
			}
		} else {
			if batch.Carried || len(batch.InstalledFleets) != 0 || len(batch.CarriedFleets) != 0 || len(batch.CarriedHistory) != 0 || batch.BatchWrite == nil {
				return fmt.Errorf("ordinary fleet generation batch %s has an invalid refresh form", key)
			}
			if err := verifyFinalFleetGenerationBatchWrite(evidence, batch); err != nil {
				return err
			}
		}
		batchesByGenerationBatch[key] = batch
	}
	for generation := uint64(1); generation <= 2; generation++ {
		for batch := uint64(1); batch <= finalFleetGenerationBatchCount; batch++ {
			if _, found := batchesByGenerationBatch[finalFleetGenerationBatchKey(generation, batch)]; !found {
				return fmt.Errorf("ordinary fleet generation batch %d/%d is absent", generation, batch)
			}
		}
	}
	return nil
}

// verifies the generation-one source partition before either provenance path
// is accepted. The only mixed shape is one that names every exact fleet in
// its installed or carried slice, has one authenticated install receipt for
// the installed side, and has individual predecessor writes for the carried
// side.
func verifyFinalFleetGenerationInitialPartition(evidence *FinalSemanticEvidence, batch FinalFleetGenerationBatchEvidence) error {
	if evidence == nil || batch.Generation != 1 {
		return errors.New("ordinary fleet generation initial partition is unavailable")
	}
	if err := verifyFinalFleetGenerationFleetPartition(batch, batch.InstalledFleets, batch.CarriedFleets); err != nil {
		return err
	}
	if batch.Carried != (len(batch.InstalledFleets) == 0) {
		return errors.New("ordinary fleet generation initial carried marker differs from its partition")
	}
	if len(batch.InstalledFleets) == 0 {
		if batch.BatchWrite != nil || batch.CalldataHash != "" || batch.EventHash != "" || batch.CoordinatorRuntimeHash != "" || batch.BatcherRuntimeHash != "" || batch.Postcondition != (FinalArtifactLocator{}) {
			return errors.New("ordinary fleet generation all-carried batch names an install write")
		}
	} else {
		if batch.BatchWrite == nil {
			return errors.New("ordinary fleet generation installed partition has no batch receipt")
		}
		if err := verifyFinalFleetGenerationBatchWrite(evidence, batch); err != nil {
			return err
		}
	}
	if len(batch.CarriedFleets) == 0 {
		if len(batch.CarriedHistory) != 0 {
			return errors.New("ordinary fleet generation installed partition has carried writes")
		}
		return nil
	}
	if len(batch.CarriedHistory) != len(batch.CarriedFleets)*int(finalFleetGenerationMembersPerFleet+1) {
		return errors.New("ordinary fleet generation carried partition has incomplete history")
	}
	return verifyFinalFleetGenerationCarriedHistory(evidence, batch)
}

// checks one initial generation's explicitly ordered installed/carried split.
// It rejects aliases and a missing or duplicate fleet before receipt parsing
// can make two different provenance paths appear interchangeable.
func verifyFinalFleetGenerationFleetPartition(batch FinalFleetGenerationBatchEvidence, installed, carried []uint64) error {
	if len(installed)+len(carried) != int(finalFleetGenerationBatchSize) {
		return errors.New("ordinary fleet generation initial partition has the wrong fleet count")
	}
	seen := make(map[uint64]struct{}, finalFleetGenerationBatchSize)
	previous := uint64(0)
	for _, fleetID := range installed {
		if fleetID < batch.FirstFleet || fleetID > batch.LastFleet || fleetID <= previous {
			return errors.New("ordinary fleet generation installed partition is unordered or out of range")
		}
		if _, duplicate := seen[fleetID]; duplicate {
			return errors.New("ordinary fleet generation initial partition duplicates a fleet")
		}
		seen[fleetID] = struct{}{}
		previous = fleetID
	}
	previous = 0
	for _, fleetID := range carried {
		if fleetID < batch.FirstFleet || fleetID > batch.LastFleet || fleetID <= previous {
			return errors.New("ordinary fleet generation carried partition is unordered or out of range")
		}
		if _, duplicate := seen[fleetID]; duplicate {
			return errors.New("ordinary fleet generation initial partition duplicates a fleet")
		}
		seen[fleetID] = struct{}{}
		previous = fleetID
	}
	for fleetID := batch.FirstFleet; fleetID <= batch.LastFleet; fleetID++ {
		if _, found := seen[fleetID]; !found {
			return errors.New("ordinary fleet generation initial partition omits a fleet")
		}
	}
	return nil
}

// checks the historical mirror-plus-four-bind action partition of a carried
// setup batch. Historical actions are intentionally ordered by fleet then
// mirror/member so an attacker cannot hide a substitution in an unordered set.
func verifyFinalFleetGenerationCarriedHistory(evidence *FinalSemanticEvidence, batch FinalFleetGenerationBatchEvidence) error {
	for offset, fleetID := range batch.CarriedFleets {
		for member := uint64(0); member <= finalFleetGenerationMembersPerFleet; member++ {
			index := offset*int(finalFleetGenerationMembersPerFleet+1) + int(member)
			write := batch.CarriedHistory[index]
			wantActionID := fmt.Sprintf("fleet.mirror.%d", fleetID)
			if member != 0 {
				wantActionID = fmt.Sprintf("fleet.bind.%d.%d", fleetID, member)
			}
			if write.Action.ActionID != wantActionID {
				return fmt.Errorf("ordinary fleet carried history %d/%d action is not canonical", fleetID, member)
			}
			if err := verifyFinalFleetGenerationWrite(evidence, write); err != nil {
				return err
			}
			if write.BatcherRuntimeHash != "" {
				return fmt.Errorf("ordinary fleet carried history %d/%d unexpectedly names a batcher runtime", fleetID, member)
			}
		}
	}
	return nil
}

// validates one raw mutation independently of the batch which groups it. A
// carried predecessor write is intentionally checked here instead of being
// allowed to inherit its payload from a later refresh batch.
func verifyFinalFleetGenerationWrite(evidence *FinalSemanticEvidence, write FinalFleetGenerationWriteEvidence) error {
	if write.Action.ActionID == "" || write.Action.PlanHash == "" || write.Action.IntentHash == "" {
		return errors.New("ordinary fleet generation write action is incomplete")
	}
	if !strings.HasPrefix(write.Calldata, "0x") || write.Calldata != strings.ToLower(write.Calldata) {
		return errors.New("ordinary fleet generation write calldata is not canonical hex")
	}
	calldata, err := decodeEvidenceHex(write.Calldata)
	if err != nil || len(calldata) < 4 || !strings.EqualFold(crypto.Keccak256Hash(calldata).Hex(), write.CalldataHash) {
		return stateMismatchError(err, "ordinary fleet generation write calldata hash differs")
	}
	if err := requireFinalHex32("ordinary fleet generation write calldata", write.CalldataHash); err != nil {
		return err
	}
	if err := requireFinalHex32("ordinary fleet generation write events", write.EventHash); err != nil {
		return err
	}
	if len(write.Events) == 0 {
		return errors.New("ordinary fleet generation write has no decoded release-contract events")
	}
	eventHash, err := canonicalHashHex(write.Events)
	if err != nil || !strings.EqualFold(eventHash, write.EventHash) {
		return stateMismatchError(err, "ordinary fleet generation write event projection hash differs")
	}
	logs := make([]finalCanonicalEVMLog, len(write.Events))
	for index, event := range write.Events {
		if event.Contract != event.Log.Address || event.Kind == "" || len(event.Log.Topics) == 0 || event.Kind != event.Log.Topics[0] || event.Log.TransactionHash != write.Receipt.TransactionHash || event.Log.BlockNumber != write.Receipt.Block.Number || event.Log.BlockHash != write.Receipt.Block.Hash {
			return fmt.Errorf("ordinary fleet generation write event %d differs from its receipt identity", index)
		}
		logs[index] = event.Log
	}
	canonicalLogs, err := finalCanonicalizeLogs(logs)
	if err != nil {
		return fmt.Errorf("ordinary fleet generation write event logs: %w", err)
	}
	for index := range logs {
		if !finalSemanticCanonicalLogEqual(logs[index], canonicalLogs[index]) {
			return errors.New("ordinary fleet generation write event logs are not in receipt order")
		}
	}
	logsHash, err := finalCanonicalReceiptLogsHash(canonicalLogs)
	if err != nil || !strings.EqualFold(logsHash, write.Receipt.LogsHash) {
		return stateMismatchError(err, "ordinary fleet generation write receipt logs differ from its event projection")
	}
	if !common.IsHexAddress(write.CoordinatorProxy) || common.HexToAddress(write.CoordinatorProxy) == (common.Address{}) || !common.IsHexAddress(write.CoordinatorImplementation) || common.HexToAddress(write.CoordinatorImplementation) == (common.Address{}) {
		return errors.New("ordinary fleet generation write coordinator identity is invalid")
	}
	for _, field := range []struct {
		label string
		hash  string
	}{
		{label: "ordinary fleet generation coordinator implementation slot", hash: write.CoordinatorImplementationSlot},
		{label: "ordinary fleet generation coordinator proxy runtime", hash: write.CoordinatorProxyRuntimeHash},
		{label: "ordinary fleet generation coordinator runtime", hash: write.CoordinatorRuntimeHash},
	} {
		if err := requireFinalHex32(field.label, field.hash); err != nil {
			return err
		}
	}
	wantSlot := "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(strings.ToLower(common.HexToAddress(write.CoordinatorImplementation).Hex()), "0x")
	if !strings.EqualFold(write.CoordinatorImplementationSlot, wantSlot) {
		return errors.New("ordinary fleet generation write implementation slot differs from its implementation")
	}
	if write.BatcherRuntimeHash != "" {
		if !common.IsHexAddress(write.BatcherAddress) || common.HexToAddress(write.BatcherAddress) == (common.Address{}) {
			return errors.New("ordinary fleet generation write batcher address is invalid")
		}
		if err := requireFinalHex32("ordinary fleet generation batcher runtime", write.BatcherRuntimeHash); err != nil {
			return err
		}
	} else if write.BatcherAddress != "" {
		return errors.New("ordinary fleet generation carried write names a batcher address")
	}
	if err := verifyFinalEVMReceipt("ordinary fleet generation write", write.Receipt, 1, evidence.EVMTerminalHead.Number); err != nil {
		return err
	}
	if write.EVMHead.Number < write.Receipt.Block.Number || write.EVMHead.Number > evidence.EVMTerminalHead.Number || write.EVMHead.Number == write.Receipt.Block.Number && !strings.EqualFold(write.EVMHead.Hash, write.Receipt.Block.Hash) {
		return errors.New("ordinary fleet generation write EVM boundary precedes or conflicts with its receipt")
	}
	if err := verifyFinalHead("ordinary fleet generation write EVM head", write.EVMHead); err != nil {
		return err
	}
	if err := verifyFinalHead("ordinary fleet generation write native head", write.NativeHead); err != nil {
		return err
	}
	if write.NativeHead.Number > evidence.NativeTerminalHead.Number {
		return errors.New("ordinary fleet generation write native head follows terminal evidence")
	}
	if err := verifyFinalArtifact("ordinary fleet generation write postcondition", write.Postcondition, "fleet-generation-postcondition"); err != nil {
		return err
	}
	return nil
}

// checks that an atomic install or refresh receipt has not been swapped from
// another batch after its source fields were independently sealed.
func verifyFinalFleetGenerationBatchWrite(evidence *FinalSemanticEvidence, batch FinalFleetGenerationBatchEvidence) error {
	if batch.BatchWrite == nil {
		return errors.New("ordinary fleet generation batch write is unavailable")
	}
	write := *batch.BatchWrite
	if write.Action != batch.Action || write.CalldataHash != batch.CalldataHash || write.EventHash != batch.EventHash || write.CoordinatorRuntimeHash != batch.CoordinatorRuntimeHash || write.BatcherRuntimeHash != batch.BatcherRuntimeHash || write.Postcondition != batch.Postcondition {
		return errors.New("ordinary fleet generation batch write differs from its sealed envelope")
	}
	return verifyFinalFleetGenerationWrite(evidence, write)
}

// validates the exact fixed 200-fleet setup partition and both generation
// records. Client, fleet, and hotkey identity must remain stable across renew.
func verifyFinalFleetGenerationSetupFleets(evidence *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence) error {
	for index, fleet := range lineage.SetupFleets {
		fleetID := uint64(index + 1)
		if fleet.FleetID != fleetID {
			return fmt.Errorf("ordinary fleet generation setup fleet %d is missing, reordered, or duplicated", fleetID)
		}
		if err := verifyFinalFleetGenerationVersion(evidence, fleet.FleetID, fleet.Initial, 1, true); err != nil {
			return err
		}
		if err := verifyFinalFleetGenerationVersion(evidence, fleet.FleetID, fleet.Refresh, 2, true); err != nil {
			return err
		}
		if fleet.Initial.Batch != (fleet.FleetID-1)/finalFleetGenerationBatchSize+1 || fleet.Refresh.Batch != fleet.Initial.Batch {
			return fmt.Errorf("ordinary fleet generation setup fleet %d batch differs", fleet.FleetID)
		}
		if err := verifyFinalFleetGenerationReplacement(fleet.FleetID, fleet.Initial, fleet.Refresh); err != nil {
			return err
		}
	}
	return nil
}

// validates a signed generation and its four member bindings without assuming
// epochs are positive: epoch zero is valid contract state and must remain
// representable in a historical proof.
func verifyFinalFleetGenerationVersion(evidence *FinalSemanticEvidence, fleetID uint64, version FinalFleetGenerationVersionEvidence, wantGeneration uint64, requireBatch bool) error {
	if version.Generation != wantGeneration || version.Hotkey == "" || requireBatch && (version.Batch == 0 || version.Batch > finalFleetGenerationBatchCount) || !requireBatch && version.Batch != 0 {
		return fmt.Errorf("ordinary fleet generation %d/%d identity is incomplete", fleetID, wantGeneration)
	}
	if err := requireFinalHex32("ordinary fleet generation commitment", version.CommitmentHash); err != nil {
		return err
	}
	if version.CommitmentAction.ActionID == "" || version.CommitmentAction.PlanHash == "" || version.CommitmentAction.IntentHash == "" {
		return fmt.Errorf("ordinary fleet generation %d/%d commitment action is incomplete", fleetID, wantGeneration)
	}
	for _, field := range []struct {
		label string
		hash  string
	}{
		{label: "ordinary fleet generation commitment plan", hash: version.CommitmentAction.PlanHash},
		{label: "ordinary fleet generation commitment intent", hash: version.CommitmentAction.IntentHash},
		{label: "ordinary fleet generation commitment extrinsic", hash: version.CommitmentExtrinsicHash},
	} {
		if err := requireFinalHex32(field.label, field.hash); err != nil {
			return err
		}
	}
	if err := verifyFinalArtifact("ordinary fleet generation commitment postcondition", version.CommitmentPostcondition, "fleet-generation-postcondition"); err != nil {
		return err
	}
	wantActionID := fmt.Sprintf("fleet.commitment.%d", fleetID)
	if wantGeneration == 2 {
		wantActionID = fmt.Sprintf("fleet.refresh.commitment.%d", fleetID)
	}
	if version.CommitmentAction.ActionID != wantActionID {
		return fmt.Errorf("ordinary fleet generation %d/%d commitment action is not canonical", fleetID, wantGeneration)
	}
	if err := verifyFinalHead("ordinary fleet generation native commitment head", version.NativeHead); err != nil {
		return err
	}
	if version.NativeHead.Number > evidence.NativeTerminalHead.Number {
		return errors.New("ordinary fleet generation native commitment follows terminal evidence")
	}
	if err := verifyFinalArtifact("ordinary fleet generation manifest", version.Manifest, "fleet-generation-manifest"); err != nil {
		return err
	}
	if err := verifyFinalArtifact("ordinary fleet generation commitment", version.Commitment, "fleet-generation-commitment"); err != nil {
		return err
	}
	if len(version.Members) != int(finalFleetGenerationMembersPerFleet) {
		return fmt.Errorf("ordinary fleet generation %d/%d has %d members", fleetID, wantGeneration, len(version.Members))
	}
	for index, member := range version.Members {
		if member.Member != uint64(index+1) || member.Generation != wantGeneration || member.ClientID == "" || member.ClientKey == "" || member.FleetKey == "" || member.Hotkey != version.Hotkey || member.CommitmentHash != version.CommitmentHash || member.ValidToEpoch < member.ValidFromEpoch {
			return fmt.Errorf("ordinary fleet generation %d/%d member %d is incomplete", fleetID, wantGeneration, index+1)
		}
		if err := requireFinalHex32("ordinary fleet member client key", member.ClientKey); err != nil {
			return err
		}
		if err := requireFinalHex32("ordinary fleet member fleet key", member.FleetKey); err != nil {
			return err
		}
		if err := requireFinalHex32("ordinary fleet member hotkey", member.Hotkey); err != nil {
			return err
		}
		if err := requireFinalHex32("ordinary fleet member commitment", member.CommitmentHash); err != nil {
			return err
		}
	}
	return nil
}

// checks that renewal retains immutable identities while replacing exactly
// generation one, rather than skipping a generation or changing a member.
func verifyFinalFleetGenerationReplacement(fleetID uint64, initial, refresh FinalFleetGenerationVersionEvidence) error {
	if initial.Generation != 1 || refresh.Generation != 2 || initial.Hotkey != refresh.Hotkey || initial.CommitmentHash == refresh.CommitmentHash || len(initial.Members) != len(refresh.Members) {
		return fmt.Errorf("ordinary fleet generation %d replacement has an invalid topology", fleetID)
	}
	for index := range initial.Members {
		before, after := initial.Members[index], refresh.Members[index]
		if before.Member != after.Member || before.ClientID != after.ClientID || before.ClientKey != after.ClientKey || before.FleetKey != after.FleetKey || before.Hotkey != after.Hotkey || before.Generation != 1 || after.Generation != 2 || after.ValidFromEpoch < before.ValidFromEpoch {
			return fmt.Errorf("ordinary fleet generation %d replacement member %d differs", fleetID, index+1)
		}
	}
	return nil
}

// verifies the two challenger-only entries and binds them to the existing
// tournament transition evidence. Challenger IDs are outside the setup batch
// space and therefore must never claim a second setup generation.
func verifyFinalFleetGenerationChallengers(evidence *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence) error {
	for index, challenger := range lineage.ChallengerFleets {
		wantFleetID := finalFleetGenerationSetupFleetCount + uint64(index) + 1
		if challenger.FleetID != wantFleetID || challenger.Initial.Generation != 1 || challenger.Initial.Batch != 0 {
			return fmt.Errorf("ordinary fleet generation challenger %d is invalid", wantFleetID)
		}
		if err := verifyFinalFleetGenerationVersion(evidence, challenger.FleetID, challenger.Initial, 1, false); err != nil {
			return err
		}
		if err := verifyFinalNativeReceipt("ordinary fleet generation challenger registration", challenger.Registration, 1, evidence.NativeTerminalHead.Number, true, finalNativeOperationRegistration); err != nil {
			return err
		}
		transition := evidence.HeadTransitions[index]
		if transition.ChallengerFleetID != challenger.FleetID || !strings.EqualFold(transition.Registration.ExtrinsicHash, challenger.Registration.ExtrinsicHash) || transition.Registration.Block != challenger.Registration.Block {
			return fmt.Errorf("ordinary fleet generation challenger %d registration differs from tournament transition", challenger.FleetID)
		}
		if err := verifyFinalArtifact("ordinary fleet generation challenger transition", challenger.Transition, "head-tournament-transition"); err != nil {
			return err
		}
	}
	return nil
}

// constructs the only permitted action identity for a setup batch.
func finalFleetGenerationActionID(generation, batch uint64) string {
	if generation == 1 {
		return "fleet.install.batch." + strconv.FormatUint(batch, 10)
	}
	return "fleet.refresh.batch." + strconv.FormatUint(batch, 10)
}

// makes a collision-free generation/batch map key without accepting textual
// aliases such as leading-zero action identifiers.
func finalFleetGenerationBatchKey(generation, batch uint64) string {
	return strconv.FormatUint(generation, 10) + "/" + strconv.FormatUint(batch, 10)
}

// canonicalizes the nested lineages before semantic-object hashing. Sorting
// is intentionally narrow: fixed-coordinate slices still reject a producer
// that tries to hide a duplicate or gap behind arbitrary input ordering.
func canonicalizeFinalFleetGenerationLineage(lineage *FinalFleetGenerationLineageEvidence) {
	if lineage == nil {
		return
	}
	sort.Slice(lineage.Batches, func(left, right int) bool {
		if lineage.Batches[left].Generation != lineage.Batches[right].Generation {
			return lineage.Batches[left].Generation < lineage.Batches[right].Generation
		}
		return lineage.Batches[left].Batch < lineage.Batches[right].Batch
	})
	sort.Slice(lineage.SetupFleets, func(left, right int) bool {
		return lineage.SetupFleets[left].FleetID < lineage.SetupFleets[right].FleetID
	})
	sort.Slice(lineage.ChallengerFleets, func(left, right int) bool {
		return lineage.ChallengerFleets[left].FleetID < lineage.ChallengerFleets[right].FleetID
	})
	for index := range lineage.SetupFleets {
		fleet := &lineage.SetupFleets[index]
		sort.Slice(fleet.Initial.Members, func(left, right int) bool {
			return fleet.Initial.Members[left].Member < fleet.Initial.Members[right].Member
		})
		sort.Slice(fleet.Refresh.Members, func(left, right int) bool {
			return fleet.Refresh.Members[left].Member < fleet.Refresh.Members[right].Member
		})
	}
	for index := range lineage.ChallengerFleets {
		challenger := &lineage.ChallengerFleets[index]
		sort.Slice(challenger.Initial.Members, func(left, right int) bool {
			return challenger.Initial.Members[left].Member < challenger.Initial.Members[right].Member
		})
	}
}
