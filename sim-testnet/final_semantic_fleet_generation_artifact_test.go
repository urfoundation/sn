package main

// final_semantic_fleet_generation_artifact_test.go covers the no-write
// reconstruction boundary used by offline reviewers of the ordinary-fleet
// generation lineage.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Rejects source-file aliases before an offline replay can use a traversal,
// duplicate, reordered item, or bytes that disagree with its content digest.
func TestFinalFleetGenerationArtifactFilesRejectsUnsafeOrUnorderedSources(t *testing.T) {
	evidence := &FinalSemanticEvidence{DeploymentID: "generation-artifact-test", PlanHash: finalFleetGenerationTestHash(1)}
	first := []byte("first")
	second := []byte("second")
	valid := finalFleetGenerationLineageArtifact{
		Schema: finalFleetGenerationLineageSchema, DeploymentID: evidence.DeploymentID, PlanHash: evidence.PlanHash,
		Files: []finalFleetGenerationLineageFile{
			{Path: "launch-foundation/journal.jsonl", ContentHash: bytesSHA256(first), SizeBytes: uint64(len(first)), Data: first},
			{Path: "launch-foundation/plan.json", ContentHash: bytesSHA256(second), SizeBytes: uint64(len(second)), Data: second},
		},
	}
	validData, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finalFleetGenerationArtifactFiles(evidence, validData); err != nil {
		t.Fatalf("accept exact source namespace: %v", err)
	}
	unsafe := valid
	unsafe.Files[0].Path = "../journal.jsonl"
	unsafeData, err := json.Marshal(unsafe)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finalFleetGenerationArtifactFiles(evidence, unsafeData); err == nil {
		t.Fatal("accepted a traversal source path")
	}
	unordered := valid
	unordered.Files[0], unordered.Files[1] = unordered.Files[1], unordered.Files[0]
	unorderedData, err := json.Marshal(unordered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finalFleetGenerationArtifactFiles(evidence, unorderedData); err == nil {
		t.Fatal("accepted an unordered source namespace")
	}
	corrupt := valid
	corrupt.Files[1].Data = []byte("substituted")
	corruptData, err := json.Marshal(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finalFleetGenerationArtifactFiles(evidence, corruptData); err == nil {
		t.Fatal("accepted source bytes with another digest")
	}
}

// Requires the reconstructed output to use an already retained immutable
// byte stream, rather than permitting a new derived locator during review.
func TestFinalFleetGenerationArtifactDeriverRejectsAbsentOrSubstitutedBytes(t *testing.T) {
	data := []byte("sealed generation bytes")
	cache := map[string][]byte{"final-derived/fleet-generation-lineage.json": data}
	derive := finalFleetGenerationArtifactDeriver(cache)
	locator, err := derive("fleet-generation-lineage", "final-derived/fleet-generation-lineage.json", data)
	if err != nil {
		t.Fatalf("resolve sealed output: %v", err)
	}
	if locator.ContentHash != bytesSHA256(data) || locator.SizeBytes != uint64(len(data)) {
		t.Fatalf("derived locator differs: %+v", locator)
	}
	if _, err := derive("fleet-generation-lineage", "final-derived/fleet-generation-lineage.json", []byte("different")); err == nil {
		t.Fatal("accepted substituted derived bytes")
	}
	if _, err := derive("fleet-generation-lineage", "final-derived/missing.json", data); err == nil {
		t.Fatal("accepted an unretained derived locator")
	}
}

// Preserves receipt order and event membership while rebuilding the source
// event index, so a stale or partial proof cannot produce the same lineage.
func TestFinalFleetGenerationArtifactEventsRejectsReceiptSubstitution(t *testing.T) {
	action := FinalFleetGenerationActionEvidence{ActionID: "fleet.mirror.1", PlanHash: finalFleetGenerationTestHash(5), IntentHash: finalFleetGenerationTestHash(6)}
	write := finalFleetGenerationTestWrite(7, action, finalFleetGenerationTestCoordinator, "")
	proof := finalFleetGenerationReceiptArtifact{Status: write.Receipt.Status, TransactionHash: write.Receipt.TransactionHash, Block: write.Receipt.Block, Logs: []finalCanonicalEVMLog{write.Events[0].Log}}
	proofData, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	write.Receipt.Proof = FinalArtifactLocator{Kind: "evm-receipt", URI: "final-derived/fleet-generation-receipt.json", ContentHash: bytesSHA256(proofData), SizeBytes: uint64(len(proofData))}
	lineage := &FinalFleetGenerationLineageEvidence{Batches: []FinalFleetGenerationBatchEvidence{{Batch: 1, Generation: 1, Carried: true, CarriedFleets: []uint64{1}, CarriedHistory: []FinalFleetGenerationWriteEvidence{write}}}}
	cache := map[string][]byte{write.Receipt.Proof.URI: proofData}
	if _, err := finalFleetGenerationArtifactEvents(lineage, cache); err != nil {
		t.Fatalf("accept exact receipt event proof: %v", err)
	}
	proof.Logs = nil
	partialData, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	cache[write.Receipt.Proof.URI] = partialData
	if _, err := finalFleetGenerationArtifactEvents(lineage, cache); err == nil {
		t.Fatal("accepted a receipt proof missing its lineage event")
	}
	proof.Logs = []finalCanonicalEVMLog{write.Events[0].Log}
	proof.TransactionHash = finalFleetGenerationTestHash(8)
	swappedData, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	cache[write.Receipt.Proof.URI] = swappedData
	if _, err := finalFleetGenerationArtifactEvents(lineage, cache); err == nil {
		t.Fatal("accepted a substituted receipt transaction")
	}
}

// Keeps the all-carried generation-one form free of a phantom batch
// postcondition. The real retained form has no BatchWrite in this case, so
// VerifyFinalSemanticArtifacts must start at the first predecessor receipt
// rather than attempting to load an empty locator.
func TestFinalFleetGenerationAllCarriedArtifactsSkipEmptyBatchPostcondition(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	source.FleetGeneration = finalFleetGenerationAllCarriedArtifactLineage(t, &source)
	lineageData := []byte("all-carried fleet generation artifact")
	source.FleetGeneration.Artifact = FinalArtifactLocator{
		Kind: "fleet-generation-lineage", URI: "final-derived/all-carried-fleet-generation.json",
		ContentHash: bytesSHA256(lineageData), SizeBytes: uint64(len(lineageData)),
	}
	artifacts[source.FleetGeneration.Artifact.URI] = lineageData
	oracleWindowData := []byte("all-carried fleet refresh oracle window")
	batcher, err := finalReleaseRuntimeRootByName(&source, "fleet_batcher")
	if err != nil {
		t.Fatal(err)
	}
	source.FleetRefreshOracleWindow = FinalFleetRefreshOracleWindowEvidence{Artifact: FinalArtifactLocator{
		Kind: "historical-fleet-refresh-oracle-window", URI: "final-derived/all-carried-oracle-window.json",
		ContentHash: bytesSHA256(oracleWindowData), SizeBytes: uint64(len(oracleWindowData)),
	}, Checkpoints: FinalFleetRefreshOracleWindowCheckpoints{
		CoordinatorProxy:         source.Deployment.CoordinatorProxy,
		AwaitActiveOperational:   FinalFleetRefreshOracleCheckpointEvidence{Head: source.EVMCampaignStartHead, Oracle: batcher.Address},
		AwaitActiveIndependent:   FinalFleetRefreshOracleCheckpointEvidence{Head: source.EVMCampaignStartHead, Oracle: batcher.Address},
		AwaitRestoredOperational: FinalFleetRefreshOracleCheckpointEvidence{Head: source.EVMTerminalHead, Oracle: source.Deployment.CoordinatorActiveCommitmentOracle},
		AwaitRestoredIndependent: FinalFleetRefreshOracleCheckpointEvidence{Head: source.EVMTerminalHead, Oracle: source.Deployment.CoordinatorActiveCommitmentOracle},
	}}
	artifacts[source.FleetRefreshOracleWindow.Artifact.URI] = oracleWindowData
	evidence, err := BuildFinalSemanticEvidence(source)
	if err != nil {
		t.Fatalf("build all-carried fleet evidence: %v", err)
	}
	firstReceipt := evidence.FleetGeneration.Batches[0].CarriedHistory[0].Receipt.Proof.URI
	carriedReceipt := errors.New("first carried receipt requested")
	emptyPostcondition := errors.New("empty batch postcondition requested")
	requestedEmptyPostcondition := false
	err = VerifyFinalSemanticArtifacts(context.Background(), evidence, func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		if locator == (FinalArtifactLocator{}) {
			requestedEmptyPostcondition = true
			return nil, emptyPostcondition
		}
		if locator.URI == firstReceipt {
			return nil, carriedReceipt
		}
		data, found := artifacts[locator.URI]
		if !found {
			return nil, fmt.Errorf("unexpected artifact %s", locator.URI)
		}
		return data, nil
	})
	if !errors.Is(err, carriedReceipt) {
		t.Fatalf("artifact verification did not reach the first carried receipt: %v", err)
	}
	if requestedEmptyPostcondition {
		t.Fatal("artifact verification requested an all-carried batch postcondition")
	}
}

// Builds the full fixed release topology with every generation-one batch
// carried. Its events are real ABI encodings, so the regression reaches the
// artifact loader only after the same semantic topology checks as production
// evidence. Values are intentionally stable across fleets: uniqueness is not
// the behavior under test, while the complete forty-batch shape is.
func finalFleetGenerationAllCarriedArtifactLineage(t *testing.T, evidence *FinalSemanticEvidence) *FinalFleetGenerationLineageEvidence {
	t.Helper()
	batcher, err := finalReleaseRuntimeRootByName(evidence, "fleet_batcher")
	if err != nil {
		t.Fatal(err)
	}
	initial := finalFleetGenerationArtifactTestValues{
		hotkey: finalFleetGenerationTestBytes32(101), commitment: finalFleetGenerationTestBytes32(102),
		fleet: finalFleetGenerationTestBytes32(103), client: finalFleetGenerationTestBytes16(104),
		nativeHead: evidence.NativeTerminalHead, generation: 1, validFrom: 0, validTo: 90, uid: 7,
	}
	refresh := initial
	refresh.commitment = finalFleetGenerationTestBytes32(105)
	refresh.generation, refresh.validFrom, refresh.validTo = 2, 1, 91
	lineage := &FinalFleetGenerationLineageEvidence{
		Schema:           finalFleetGenerationLineageSchema,
		Batches:          make([]FinalFleetGenerationBatchEvidence, 0, finalFleetGenerationBatchCount*2),
		SetupFleets:      make([]FinalFleetGenerationFleetEvidence, 0, finalFleetGenerationSetupFleetCount),
		ChallengerFleets: make([]FinalFleetGenerationChallengerEvidence, 0, finalFleetGenerationChallengerFleetCount),
	}
	sequence := uint64(1)
	for batch := uint64(1); batch <= finalFleetGenerationBatchCount; batch++ {
		first := (batch-1)*finalFleetGenerationBatchSize + 1
		initialBatch := FinalFleetGenerationBatchEvidence{
			Batch: batch, Generation: 1, FirstFleet: first, LastFleet: first + finalFleetGenerationBatchSize - 1,
			Action:  finalFleetGenerationArtifactTestAction(finalFleetGenerationActionID(1, batch), sequence),
			Carried: true, CarriedFleets: make([]uint64, 0, finalFleetGenerationBatchSize),
			CarriedHistory: make([]FinalFleetGenerationWriteEvidence, 0, finalFleetGenerationBatchSize*(finalFleetGenerationMembersPerFleet+1)),
		}
		for fleetID := first; fleetID <= initialBatch.LastFleet; fleetID++ {
			initialBatch.CarriedFleets = append(initialBatch.CarriedFleets, fleetID)
			mirror := finalFleetGenerationArtifactTestAction(fmt.Sprintf("fleet.mirror.%d", fleetID), sequence)
			initialBatch.CarriedHistory = append(initialBatch.CarriedHistory, finalFleetGenerationArtifactTestWrite(t, evidence, mirror, initial, []string{"CommitmentMirrored"}, "", sequence))
			sequence++
			for member := uint64(1); member <= finalFleetGenerationMembersPerFleet; member++ {
				binding := finalFleetGenerationArtifactTestAction(fmt.Sprintf("fleet.bind.%d.%d", fleetID, member), sequence)
				initialBatch.CarriedHistory = append(initialBatch.CarriedHistory, finalFleetGenerationArtifactTestWrite(t, evidence, binding, initial, []string{"FleetBound"}, "", sequence))
				sequence++
			}
		}
		lineage.Batches = append(lineage.Batches, initialBatch)
	}
	for batch := uint64(1); batch <= finalFleetGenerationBatchCount; batch++ {
		first := (batch-1)*finalFleetGenerationBatchSize + 1
		action := finalFleetGenerationArtifactTestAction(finalFleetGenerationActionID(2, batch), sequence)
		names := make([]string, 0, finalFleetGenerationBatchSize*(1+finalFleetGenerationMembersPerFleet*3+1))
		for fleetID := first; fleetID < first+finalFleetGenerationBatchSize; fleetID++ {
			names = append(names, "CommitmentMirrored")
			for member := uint64(1); member <= finalFleetGenerationMembersPerFleet; member++ {
				names = append(names, "FleetBindingRevoked", "FleetBound", "FleetMemberBound")
			}
			names = append(names, "FleetRefreshed")
		}
		write := finalFleetGenerationArtifactTestWrite(t, evidence, action, refresh, names, batcher.Address, sequence)
		lineage.Batches = append(lineage.Batches, FinalFleetGenerationBatchEvidence{
			Batch: batch, Generation: 2, FirstFleet: first, LastFleet: first + finalFleetGenerationBatchSize - 1,
			Action: action, CalldataHash: write.CalldataHash, EventHash: write.EventHash,
			CoordinatorRuntimeHash: write.CoordinatorRuntimeHash, BatcherRuntimeHash: write.BatcherRuntimeHash,
			BatchWrite: &write, Postcondition: write.Postcondition,
		})
		sequence++
	}
	for fleetID := uint64(1); fleetID <= finalFleetGenerationSetupFleetCount; fleetID++ {
		batch := (fleetID-1)/finalFleetGenerationBatchSize + 1
		lineage.SetupFleets = append(lineage.SetupFleets, FinalFleetGenerationFleetEvidence{
			FleetID: fleetID,
			Initial: finalFleetGenerationArtifactTestVersion(fleetID, 1, batch, initial),
			Refresh: finalFleetGenerationArtifactTestVersion(fleetID, 2, batch, refresh),
		})
	}
	for index := uint64(0); index < finalFleetGenerationChallengerFleetCount; index++ {
		fleetID := finalFleetGenerationSetupFleetCount + index + 1
		lineage.ChallengerFleets = append(lineage.ChallengerFleets, FinalFleetGenerationChallengerEvidence{
			FleetID: fleetID, Initial: finalFleetGenerationArtifactTestVersion(fleetID, 1, 0, initial),
			Registration: evidence.HeadTransitions[index].Registration,
			Transition:   finalFleetGenerationTestArtifact("head-tournament-transition", fmt.Sprintf("all-carried-challenger-%d", fleetID)),
		})
	}
	return lineage
}

type finalFleetGenerationArtifactTestValues struct {
	hotkey, commitment, fleet [32]byte
	client                    [16]byte
	nativeHead                ChainHead
	generation, validFrom     uint64
	validTo                   uint64
	uid                       uint16
}

func finalFleetGenerationArtifactTestAction(actionID string, sequence uint64) FinalFleetGenerationActionEvidence {
	return FinalFleetGenerationActionEvidence{
		ActionID: actionID, PlanHash: finalFleetGenerationTestHash(1_000_000 + sequence), IntentHash: finalFleetGenerationTestHash(2_000_000 + sequence),
	}
}

func finalFleetGenerationArtifactTestVersion(fleetID, generation, batch uint64, values finalFleetGenerationArtifactTestValues) FinalFleetGenerationVersionEvidence {
	version := finalFleetGenerationTestVersion(fleetID, generation, batch)
	version.Hotkey = finalFleetGenerationArtifactTestHex32(values.hotkey)
	version.CommitmentHash = finalFleetGenerationArtifactTestHex32(values.commitment)
	version.NativeHead = values.nativeHead
	for index := range version.Members {
		member := &version.Members[index]
		member.ClientID = fmt.Sprintf("0x%x", values.client[:])
		member.FleetKey = finalFleetGenerationArtifactTestHex32(values.fleet)
		member.Hotkey = version.Hotkey
		member.CommitmentHash = version.CommitmentHash
		member.Generation = values.generation
		member.ValidFromEpoch = values.validFrom
		member.ValidToEpoch = values.validTo
		member.UID = values.uid
	}
	return version
}

func finalFleetGenerationArtifactTestWrite(t *testing.T, evidence *FinalSemanticEvidence, action FinalFleetGenerationActionEvidence, values finalFleetGenerationArtifactTestValues, names []string, batcherAddress string, sequence uint64) FinalFleetGenerationWriteEvidence {
	t.Helper()
	if len(names) == 0 {
		t.Fatal("fleet generation test write has no events")
	}
	head := finalFleetGenerationTestHead(500 + sequence)
	transactionHash := finalFleetGenerationTestHash(3_000_000 + sequence)
	events := make([]FinalFleetGenerationEventEvidence, 0, len(names))
	logs := make([]finalCanonicalEVMLog, 0, len(names))
	for index, name := range names {
		event := finalFleetGenerationArtifactTestEvent(t, evidence, action.ActionID, name, values, batcherAddress, head, transactionHash, uint64(index))
		events = append(events, event)
		logs = append(logs, event.Log)
	}
	logsHash, err := finalCanonicalReceiptLogsHash(logs)
	if err != nil {
		t.Fatal(err)
	}
	eventHash, err := canonicalHashHex(events)
	if err != nil {
		t.Fatal(err)
	}
	calldata := "0x01020304"
	calldataBytes, err := decodeEvidenceHex(calldata)
	if err != nil {
		t.Fatal(err)
	}
	batcherRuntimeHash := ""
	if batcherAddress != "" {
		batcher, rootErr := finalReleaseRuntimeRootByName(evidence, "fleet_batcher")
		if rootErr != nil || !strings.EqualFold(batcher.Address, batcherAddress) {
			t.Fatalf("fleet batcher root differs: %v", rootErr)
		}
		batcherRuntimeHash = batcher.RuntimeCodeHash
	}
	return FinalFleetGenerationWriteEvidence{
		Action: action,
		Receipt: FinalEVMReceipt{
			TransactionHash: transactionHash, Block: head, Status: "success", LogsHash: logsHash,
			Proof: finalFleetGenerationTestArtifact("evm-receipt", fmt.Sprintf("all-carried-receipt-%d", sequence)),
		},
		Calldata: calldata, CalldataHash: crypto.Keccak256Hash(calldataBytes).Hex(), EventHash: eventHash, Events: events,
		CoordinatorProxy: evidence.Deployment.CoordinatorProxy, CoordinatorImplementation: evidence.Deployment.CoordinatorImplementation,
		CoordinatorImplementationSlot: evidence.Deployment.ObservedImplementationSlot, CoordinatorProxyRuntimeHash: evidence.Deployment.CoordinatorProxyCodeHash,
		CoordinatorRuntimeHash: evidence.Deployment.ImplementationCodeHash, BatcherAddress: batcherAddress, BatcherRuntimeHash: batcherRuntimeHash,
		Postcondition: finalFleetGenerationTestArtifact("fleet-generation-postcondition", fmt.Sprintf("all-carried-write-%d", sequence)),
		EVMHead:       head, NativeHead: values.nativeHead,
	}
}

func finalFleetGenerationArtifactTestEvent(t *testing.T, evidence *FinalSemanticEvidence, actionID, name string, values finalFleetGenerationArtifactTestValues, batcherAddress string, head ChainHead, transactionHash string, logIndex uint64) FinalFleetGenerationEventEvidence {
	t.Helper()
	coordinator, batcher, err := finalFleetGenerationABIs()
	if err != nil {
		t.Fatal(err)
	}
	contract, address := coordinator, strings.ToLower(evidence.Deployment.CoordinatorProxy)
	if name == "FleetMemberBound" || name == "FleetInstalled" || name == "FleetRefreshed" {
		contract, address = batcher, strings.ToLower(batcherAddress)
	}
	event, found := contract.Events[name]
	if !found {
		t.Fatalf("ABI lacks %s", name)
	}
	indexed := make([]common.Hash, 0, 3)
	dataValues := make([]any, 0, len(event.Inputs))
	for _, input := range event.Inputs {
		if input.Indexed {
			switch input.Name {
			case "hotkey":
				indexed = append(indexed, common.BytesToHash(values.hotkey[:]))
			case "commitmentHash":
				indexed = append(indexed, common.BytesToHash(values.commitment[:]))
			case "fleetId":
				indexed = append(indexed, common.BytesToHash(values.fleet[:]))
			case "clientId":
				var topic [32]byte
				copy(topic[:], values.client[:])
				indexed = append(indexed, common.BytesToHash(topic[:]))
			default:
				t.Fatalf("unsupported indexed %s.%s", name, input.Name)
			}
			continue
		}
		switch input.Name {
		case "finalizedBlock":
			dataValues = append(dataValues, values.nativeHead.Number)
		case "finalizedBlockHash":
			var hash [32]byte
			copy(hash[:], common.HexToHash(values.nativeHead.Hash).Bytes())
			dataValues = append(dataValues, hash)
		case "uid":
			dataValues = append(dataValues, values.uid)
		case "generation":
			generation := values.generation
			if name == "FleetBindingRevoked" {
				generation = 1
			}
			dataValues = append(dataValues, generation)
		case "validFromEpoch", "effectiveEpoch":
			dataValues = append(dataValues, values.validFrom)
		case "validToEpoch":
			dataValues = append(dataValues, values.validTo)
		case "members":
			dataValues = append(dataValues, big.NewInt(int64(finalFleetGenerationMembersPerFleet)))
		default:
			t.Fatalf("unsupported non-indexed %s.%s", name, input.Name)
		}
	}
	data, err := event.Inputs.NonIndexed().Pack(dataValues...)
	if err != nil {
		t.Fatal(err)
	}
	topics := make([]string, 1, len(indexed)+1)
	topics[0] = strings.ToLower(event.ID.Hex())
	for _, topic := range indexed {
		topics = append(topics, strings.ToLower(topic.Hex()))
	}
	log := finalCanonicalEVMLog{
		Address: address, Topics: topics, Data: "0x" + common.Bytes2Hex(data),
		BlockNumber: head.Number, BlockHash: head.Hash, TransactionHash: transactionHash, TransactionIndex: 0, LogIndex: logIndex,
	}
	decoded, err := finalFleetGenerationDecodeEventForBatcher(evidence, actionID, batcherAddress, log)
	if err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return decoded.Evidence
}

func finalFleetGenerationArtifactTestHex32(value [32]byte) string {
	return strings.ToLower(common.BytesToHash(value[:]).Hex())
}
