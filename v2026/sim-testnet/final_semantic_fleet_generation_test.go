package main

// final_semantic_fleet_generation_test.go exercises the sealed ordinary-fleet
// generation partition without a live chain. Every negative case starts from
// a fully valid 202-candidate fixture so one substituted field cannot hide
// behind an unrelated incomplete setup.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

// accepts the exact setup/head/challenger topology with generations one and
// two only for the 200 setup fleets.
func TestFinalFleetGenerationAcceptsExactTwoGenerationTopology(t *testing.T) {
	evidence, lineage := finalFleetGenerationTestFixture(t)
	if err := verifyFinalFleetGenerationLineage(evidence, lineage); err != nil {
		t.Fatalf("verify exact generation topology: %v", err)
	}
}

// rejects a missing initial record and a corrupted initial commitment without
// weakening the independently valid remainder of the topology.
func TestFinalFleetGenerationRejectsMissingOrCorruptGenerationOne(t *testing.T) {
	evidence, missing := finalFleetGenerationTestFixture(t)
	missing.SetupFleets = missing.SetupFleets[1:]
	if err := verifyFinalFleetGenerationLineage(evidence, missing); err == nil {
		t.Fatal("accepted a topology with no generation-one fleet 1")
	}
	evidence, corrupt := finalFleetGenerationTestFixture(t)
	corrupt.SetupFleets[0].Initial.Members[0].CommitmentHash = finalFleetGenerationTestHash(9_001)
	if err := verifyFinalFleetGenerationLineage(evidence, corrupt); err == nil {
		t.Fatal("accepted a generation-one member whose commitment differs from its manifest")
	}
	evidence, missingProof := finalFleetGenerationTestFixture(t)
	missingProof.SetupFleets[0].Initial.CommitmentPostcondition = FinalArtifactLocator{}
	if err := verifyFinalFleetGenerationLineage(evidence, missingProof); err == nil {
		t.Fatal("accepted a generation-one commitment without its authenticated postcondition")
	}
}

// rejects an invented terminal third generation rather than accepting a
// terminal state that merely happens to have an active binding.
func TestFinalFleetGenerationRejectsTerminalGenerationThree(t *testing.T) {
	evidence, lineage := finalFleetGenerationTestFixture(t)
	lineage.SetupFleets[0].Refresh.Generation = 3
	for index := range lineage.SetupFleets[0].Refresh.Members {
		lineage.SetupFleets[0].Refresh.Members[index].Generation = 3
	}
	if err := verifyFinalFleetGenerationLineage(evidence, lineage); err == nil {
		t.Fatal("accepted terminal generation three")
	}
}

// Rejects a reordered fixed batch partition, because an unordered map would
// otherwise let a valid carried history and refresh receipt be relabeled.
func TestFinalFleetGenerationRejectsReorderedBatchPartition(t *testing.T) {
	evidence, lineage := finalFleetGenerationTestFixture(t)
	lineage.Batches[0], lineage.Batches[1] = lineage.Batches[1], lineage.Batches[0]
	if err := verifyFinalFleetGenerationLineage(evidence, lineage); err == nil {
		t.Fatal("accepted a reordered generation batch partition")
	}
}

// rejects every separately sealed mutation projection when it no longer
// matches its batch envelope.
func TestFinalFleetGenerationRejectsSubstitutedBatchReceiptCalldataEventOrPostcondition(t *testing.T) {
	evidence, receipt := finalFleetGenerationTestFixture(t)
	receipt.Batches[20].BatchWrite.Receipt.TransactionHash = finalFleetGenerationTestHash(9_001)
	if err := verifyFinalFleetGenerationLineage(evidence, receipt); err == nil {
		t.Fatal("accepted substituted batch receipt")
	}
	evidence, calldata := finalFleetGenerationTestFixture(t)
	calldata.Batches[20].BatchWrite.CalldataHash = finalFleetGenerationTestHash(9_002)
	if err := verifyFinalFleetGenerationLineage(evidence, calldata); err == nil {
		t.Fatal("accepted substituted batch calldata")
	}
	evidence, events := finalFleetGenerationTestFixture(t)
	events.Batches[20].BatchWrite.EventHash = finalFleetGenerationTestHash(9_003)
	if err := verifyFinalFleetGenerationLineage(evidence, events); err == nil {
		t.Fatal("accepted substituted batch events")
	}
	evidence, postcondition := finalFleetGenerationTestFixture(t)
	postcondition.Batches[20].BatchWrite.Postcondition = finalFleetGenerationTestArtifact("fleet-generation-postcondition", "substituted-postcondition")
	if err := verifyFinalFleetGenerationLineage(evidence, postcondition); err == nil {
		t.Fatal("accepted substituted batch postcondition")
	}
}

// rejects a coordinator or batcher runtime projection that differs from the
// immutable batch envelope at its exact historical write head.
func TestFinalFleetGenerationRejectsWriteHeadRuntimeSubstitution(t *testing.T) {
	evidence, coordinator := finalFleetGenerationTestFixture(t)
	coordinator.Batches[20].BatchWrite.CoordinatorRuntimeHash = finalFleetGenerationTestHash(9_004)
	if err := verifyFinalFleetGenerationLineage(evidence, coordinator); err == nil {
		t.Fatal("accepted substituted coordinator runtime at a batch write head")
	}
	evidence, batcher := finalFleetGenerationTestFixture(t)
	batcher.Batches[20].BatchWrite.BatcherRuntimeHash = finalFleetGenerationTestHash(9_005)
	if err := verifyFinalFleetGenerationLineage(evidence, batcher); err == nil {
		t.Fatal("accepted substituted batcher runtime at a batch write head")
	}
}

// accepts the authenticated five-installed/five-carried batch-three shape
// while rejecting every ambiguous initial partition. This protects the exact
// historical boundary where the helper installed only fleets 26 through 30.
func TestFinalFleetGenerationInitialPartitionRejectsOmissionDuplicateAndUnexplainedMixed(t *testing.T) {
	evidence, lineage := finalFleetGenerationTestFixture(t)
	batch := &lineage.Batches[2]
	batch.Carried = false
	batch.CarriedFleets = append([]uint64(nil), batch.CarriedFleets[:5]...)
	batch.InstalledFleets = []uint64{26, 27, 28, 29, 30}
	batch.CarriedHistory = append([]FinalFleetGenerationWriteEvidence(nil), batch.CarriedHistory[:25]...)
	write := finalFleetGenerationTestWrite(30_001, batch.Action, finalFleetGenerationTestBatcher, finalFleetGenerationTestHash(30_002))
	write.Postcondition = finalFleetGenerationTestArtifact("fleet-generation-postcondition", "mixed-install-postcondition")
	batch.CalldataHash, batch.EventHash = write.CalldataHash, write.EventHash
	batch.CoordinatorRuntimeHash, batch.BatcherRuntimeHash = write.CoordinatorRuntimeHash, write.BatcherRuntimeHash
	batch.Postcondition, batch.BatchWrite = write.Postcondition, &write
	if err := verifyFinalFleetGenerationInitialPartition(evidence, *batch); err != nil {
		t.Fatalf("accept authenticated mixed initial partition: %v", err)
	}
	missing := *batch
	missing.InstalledFleets = missing.InstalledFleets[:4]
	if err := verifyFinalFleetGenerationInitialPartition(evidence, missing); err == nil {
		t.Fatal("accepted an initial partition with an omitted fleet")
	}
	duplicate := *batch
	duplicate.CarriedFleets = append([]uint64(nil), duplicate.CarriedFleets...)
	duplicate.CarriedFleets[4] = duplicate.InstalledFleets[0]
	if err := verifyFinalFleetGenerationInitialPartition(evidence, duplicate); err == nil {
		t.Fatal("accepted an initial partition with a duplicated fleet")
	}
	unexplained := *batch
	unexplained.Carried = true
	if err := verifyFinalFleetGenerationInitialPartition(evidence, unexplained); err == nil {
		t.Fatal("accepted a mixed partition marked as all-carried")
	}
}

// binds an installed generation-one batch receipt to its envelope exactly as
// a refresh receipt is bound, preventing a substituted calldata, receipt, or
// postcondition from being hidden behind a valid carried subset.
func TestFinalFleetGenerationRejectsInstalledBatchReceiptCalldataOrPostconditionSubstitution(t *testing.T) {
	evidence, lineage := finalFleetGenerationTestFixture(t)
	batch := &lineage.Batches[0]
	batch.Carried = false
	batch.CarriedFleets = nil
	batch.InstalledFleets = []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	batch.CarriedHistory = nil
	write := finalFleetGenerationTestWrite(31_001, batch.Action, finalFleetGenerationTestBatcher, finalFleetGenerationTestHash(31_002))
	write.Postcondition = finalFleetGenerationTestArtifact("fleet-generation-postcondition", "installed-batch-postcondition")
	batch.CalldataHash, batch.EventHash = write.CalldataHash, write.EventHash
	batch.CoordinatorRuntimeHash, batch.BatcherRuntimeHash = write.CoordinatorRuntimeHash, write.BatcherRuntimeHash
	batch.Postcondition, batch.BatchWrite = write.Postcondition, &write
	if err := verifyFinalFleetGenerationInitialPartition(evidence, *batch); err != nil {
		t.Fatalf("accept installed batch envelope: %v", err)
	}
	badReceipt := *batch
	badReceipt.BatchWrite = new(FinalFleetGenerationWriteEvidence)
	*badReceipt.BatchWrite = *batch.BatchWrite
	badReceipt.BatchWrite.Receipt.TransactionHash = finalFleetGenerationTestHash(31_003)
	if err := verifyFinalFleetGenerationInitialPartition(evidence, badReceipt); err == nil {
		t.Fatal("accepted a substituted installed batch receipt")
	}
	badCalldata := *batch
	badCalldata.BatchWrite = new(FinalFleetGenerationWriteEvidence)
	*badCalldata.BatchWrite = *batch.BatchWrite
	badCalldata.BatchWrite.CalldataHash = finalFleetGenerationTestHash(31_004)
	if err := verifyFinalFleetGenerationInitialPartition(evidence, badCalldata); err == nil {
		t.Fatal("accepted substituted installed batch calldata")
	}
	badPostcondition := *batch
	badPostcondition.BatchWrite = new(FinalFleetGenerationWriteEvidence)
	*badPostcondition.BatchWrite = *batch.BatchWrite
	badPostcondition.BatchWrite.Postcondition = finalFleetGenerationTestArtifact("fleet-generation-postcondition", "substituted-installed-postcondition")
	if err := verifyFinalFleetGenerationInitialPartition(evidence, badPostcondition); err == nil {
		t.Fatal("accepted a substituted installed batch postcondition")
	}
}

// builds a complete, shape-valid proof that each negative test can mutate in
// isolation. The fixture does not stand in for source/artifact replay tests.
func finalFleetGenerationTestFixture(t *testing.T) (*FinalSemanticEvidence, *FinalFleetGenerationLineageEvidence) {
	t.Helper()
	evidence := &FinalSemanticEvidence{
		ExpectedCandidates: 202,
		ExpectedHeadSlots:  200,
		ExpectedMiners:     1000,
		EVMTerminalHead:    finalFleetGenerationTestHead(100_000),
		NativeTerminalHead: finalFleetGenerationTestHead(100_000),
		HeadFleets:         make([]FinalHeadFleetEvidence, 0, 202),
		HeadTransitions:    make([]FinalHeadTournamentTransition, 0, 2),
	}
	for fleetID := uint64(1); fleetID <= 202; fleetID++ {
		evidence.HeadFleets = append(evidence.HeadFleets, FinalHeadFleetEvidence{FleetID: fleetID})
	}
	for fleetID := uint64(201); fleetID <= 202; fleetID++ {
		evidence.HeadTransitions = append(evidence.HeadTransitions, FinalHeadTournamentTransition{ChallengerFleetID: fleetID})
	}
	lineage := &FinalFleetGenerationLineageEvidence{
		Schema:           finalFleetGenerationLineageSchema,
		Batches:          make([]FinalFleetGenerationBatchEvidence, 0, 40),
		SetupFleets:      make([]FinalFleetGenerationFleetEvidence, 0, 200),
		ChallengerFleets: make([]FinalFleetGenerationChallengerEvidence, 0, 2),
		Artifact:         finalFleetGenerationTestArtifact("fleet-generation-lineage", "lineage"),
	}
	for generation := uint64(1); generation <= 2; generation++ {
		for batch := uint64(1); batch <= 20; batch++ {
			lineage.Batches = append(lineage.Batches, finalFleetGenerationTestBatch(generation, batch))
		}
	}
	for fleetID := uint64(1); fleetID <= 200; fleetID++ {
		batch := (fleetID-1)/10 + 1
		lineage.SetupFleets = append(lineage.SetupFleets, FinalFleetGenerationFleetEvidence{
			FleetID: fleetID,
			Initial: finalFleetGenerationTestVersion(fleetID, 1, batch),
			Refresh: finalFleetGenerationTestVersion(fleetID, 2, batch),
		})
	}
	for fleetID := uint64(201); fleetID <= 202; fleetID++ {
		initial := finalFleetGenerationTestVersion(fleetID, 1, 0)
		registration := finalFleetGenerationTestRegistration(t, fleetID, initial.Hotkey)
		evidence.HeadTransitions[int(fleetID-finalFleetGenerationSetupFleetCount-1)].Registration = registration
		lineage.ChallengerFleets = append(lineage.ChallengerFleets, FinalFleetGenerationChallengerEvidence{
			FleetID: fleetID, Initial: initial, Registration: registration,
			Transition: finalFleetGenerationTestArtifact("head-tournament-transition", fmt.Sprintf("challenger-%d-transition", fleetID)),
		})
	}
	return evidence, lineage
}

// creates one exact ten-fleet transaction envelope and its matching receipt.
func finalFleetGenerationTestBatch(generation, batch uint64) FinalFleetGenerationBatchEvidence {
	action := FinalFleetGenerationActionEvidence{
		ActionID:   finalFleetGenerationActionID(generation, batch),
		PlanHash:   finalFleetGenerationTestHash(10_000 + generation*100 + batch),
		IntentHash: finalFleetGenerationTestHash(20_000 + generation*100 + batch),
	}
	firstFleet := (batch-1)*10 + 1
	if generation == 1 {
		history := make([]FinalFleetGenerationWriteEvidence, 0, 50)
		carried := make([]uint64, 0, 10)
		for fleetID := firstFleet; fleetID < firstFleet+10; fleetID++ {
			carried = append(carried, fleetID)
			mirrorAction := FinalFleetGenerationActionEvidence{ActionID: fmt.Sprintf("fleet.mirror.%d", fleetID), PlanHash: finalFleetGenerationTestHash(11_000 + fleetID), IntentHash: finalFleetGenerationTestHash(12_000 + fleetID)}
			history = append(history, finalFleetGenerationTestWrite(13_000+fleetID, mirrorAction, finalFleetGenerationTestCoordinator, ""))
			for member := uint64(1); member <= 4; member++ {
				bindAction := FinalFleetGenerationActionEvidence{ActionID: fmt.Sprintf("fleet.bind.%d.%d", fleetID, member), PlanHash: finalFleetGenerationTestHash(14_000 + fleetID*10 + member), IntentHash: finalFleetGenerationTestHash(15_000 + fleetID*10 + member)}
				history = append(history, finalFleetGenerationTestWrite(16_000+fleetID*10+member, bindAction, finalFleetGenerationTestCoordinator, ""))
			}
		}
		return FinalFleetGenerationBatchEvidence{
			Batch: batch, Generation: generation, FirstFleet: firstFleet, LastFleet: firstFleet + 9,
			Action: action, Carried: true, CarriedFleets: carried, CarriedHistory: history,
		}
	}
	postcondition := finalFleetGenerationTestArtifact("fleet-generation-postcondition", fmt.Sprintf("batch-%d-%d-postcondition", generation, batch))
	write := finalFleetGenerationTestWrite(17_000+generation*100+batch, action, finalFleetGenerationTestBatcher, finalFleetGenerationTestHash(18_000+generation*100+batch))
	write.Postcondition = postcondition
	return FinalFleetGenerationBatchEvidence{
		Batch:                  batch,
		Generation:             generation,
		FirstFleet:             firstFleet,
		LastFleet:              firstFleet + 9,
		Action:                 action,
		CalldataHash:           write.CalldataHash,
		EventHash:              write.EventHash,
		CoordinatorRuntimeHash: write.CoordinatorRuntimeHash,
		BatcherRuntimeHash:     write.BatcherRuntimeHash,
		BatchWrite:             &write,
		Postcondition:          postcondition,
	}
}

// creates a valid independently checkable EVM mutation envelope for a fixture.
func finalFleetGenerationTestWrite(value uint64, action FinalFleetGenerationActionEvidence, contract, batcherRuntimeHash string) FinalFleetGenerationWriteEvidence {
	calldata := fmt.Sprintf("0x%08x", value)
	calldataBytes, err := decodeEvidenceHex(calldata)
	if err != nil {
		panic(err)
	}
	head := finalFleetGenerationTestHead(20_000 + value)
	receipt := finalFleetGenerationTestReceipt(21_000+value, head)
	topic := finalFleetGenerationTestHash(value + 1)
	log := finalCanonicalEVMLog{
		Address: contract, Topics: []string{topic}, Data: "0x", BlockNumber: head.Number, BlockHash: head.Hash,
		TransactionHash: receipt.TransactionHash, TransactionIndex: 0, LogIndex: 0,
	}
	events := []FinalFleetGenerationEventEvidence{{Contract: contract, Kind: topic, Log: log, FleetID: finalFleetGenerationTestHash(value + 2)}}
	eventHash, err := canonicalHashHex(events)
	if err != nil {
		panic(err)
	}
	logsHash, err := finalCanonicalReceiptLogsHash([]finalCanonicalEVMLog{log})
	if err != nil {
		panic(err)
	}
	receipt.LogsHash = logsHash
	batcherAddress := ""
	if batcherRuntimeHash != "" {
		batcherAddress = contract
	}
	return FinalFleetGenerationWriteEvidence{
		Action: action, Receipt: receipt, Calldata: calldata,
		CalldataHash: crypto.Keccak256Hash(calldataBytes).Hex(), EventHash: eventHash, Events: events,
		CoordinatorProxy: finalFleetGenerationTestCoordinator, CoordinatorImplementation: finalFleetGenerationTestImplementation,
		CoordinatorImplementationSlot: "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(finalFleetGenerationTestImplementation, "0x"),
		CoordinatorProxyRuntimeHash:   finalFleetGenerationTestHash(21_500 + value), CoordinatorRuntimeHash: finalFleetGenerationTestHash(22_000 + value), BatcherAddress: batcherAddress, BatcherRuntimeHash: batcherRuntimeHash,
		Postcondition: finalFleetGenerationTestArtifact("fleet-generation-postcondition", fmt.Sprintf("write-%d-postcondition", value)),
		EVMHead:       head, NativeHead: finalFleetGenerationTestHead(23_000 + value),
	}
}

const finalFleetGenerationTestCoordinator = "0x0000000000000000000000000000000000000001"
const finalFleetGenerationTestBatcher = "0x0000000000000000000000000000000000000002"
const finalFleetGenerationTestImplementation = "0x0000000000000000000000000000000000000003"

// creates one stable four-member signed generation for a fixed fleet ID.
func finalFleetGenerationTestVersion(fleetID, generation, batch uint64) FinalFleetGenerationVersionEvidence {
	hotkey := finalFleetGenerationTestHash(300_000 + fleetID)
	commitmentHash := finalFleetGenerationTestHash(400_000 + fleetID*10 + generation)
	fleetKey := finalFleetGenerationTestHash(500_000 + fleetID)
	members := make([]FinalFleetGenerationMemberEvidence, 0, 4)
	for member := uint64(1); member <= 4; member++ {
		members = append(members, FinalFleetGenerationMemberEvidence{
			Member: member, ClientID: fmt.Sprintf("client-%d-%d", fleetID, member),
			ClientKey: finalFleetGenerationTestHash(600_000 + fleetID*10 + member), FleetKey: fleetKey,
			Hotkey: hotkey, CommitmentHash: commitmentHash, Generation: generation,
			ValidFromEpoch: generation - 1, ValidToEpoch: generation + 50,
		})
	}
	return FinalFleetGenerationVersionEvidence{
		Generation: generation, Batch: batch,
		Manifest:   finalFleetGenerationTestArtifact("fleet-generation-manifest", fmt.Sprintf("fleet-%d-generation-%d-manifest", fleetID, generation)),
		Commitment: finalFleetGenerationTestArtifact("fleet-generation-commitment", fmt.Sprintf("fleet-%d-generation-%d-commitment", fleetID, generation)),
		CommitmentAction: FinalFleetGenerationActionEvidence{
			ActionID:   finalFleetGenerationTestCommitmentActionID(fleetID, generation),
			PlanHash:   finalFleetGenerationTestHash(700_000 + fleetID*10 + generation),
			IntentHash: finalFleetGenerationTestHash(800_000 + fleetID*10 + generation),
		},
		CommitmentExtrinsicHash: finalFleetGenerationTestHash(900_000 + fleetID*10 + generation),
		CommitmentPostcondition: finalFleetGenerationTestArtifact("fleet-generation-postcondition", fmt.Sprintf("fleet-%d-generation-%d-commitment", fleetID, generation)),
		CommitmentHash:          commitmentHash,
		Hotkey:                  hotkey,
		NativeHead:              finalFleetGenerationTestHead(3_000 + fleetID*10 + generation),
		Members:                 members,
	}
}

// makes the only native commitment action name allowed for a fleet generation.
func finalFleetGenerationTestCommitmentActionID(fleetID, generation uint64) string {
	if generation == 2 {
		return fmt.Sprintf("fleet.refresh.commitment.%d", fleetID)
	}
	return fmt.Sprintf("fleet.commitment.%d", fleetID)
}

// creates the exact native registration call required by challenger proof.
func finalFleetGenerationTestRegistration(t *testing.T, fleetID uint64, hotkey string) FinalNativeReceipt {
	t.Helper()
	call, err := finalNativeRegistrationCallEvidence(finalFleetGenerationTestHash(700_000+fleetID), uint32(fleetID), 521, hotkey, uint16(fleetID), 600_000)
	if err != nil {
		t.Fatalf("build challenger %d registration: %v", fleetID, err)
	}
	head := finalFleetGenerationTestHead(4_000 + fleetID)
	return FinalNativeReceipt{
		ExtrinsicHash: finalFleetGenerationTestHash(800_000 + fleetID), Block: head,
		Proof: finalFleetGenerationTestArtifact("native-receipt", fmt.Sprintf("challenger-%d-registration", fleetID)), Call: &call,
	}
}

// creates a structurally valid retained EVM receipt at one pinned head.
func finalFleetGenerationTestReceipt(value uint64, head ChainHead) FinalEVMReceipt {
	return FinalEVMReceipt{
		TransactionHash: finalFleetGenerationTestHash(value), Block: head, Status: "success",
		LogsHash: finalFleetGenerationTestHash(value + 1),
		Proof:    finalFleetGenerationTestArtifact("evm-receipt", fmt.Sprintf("receipt-%d", value)),
	}
}

// makes one nonzero canonical block/hash pair for a fixture checkpoint.
func finalFleetGenerationTestHead(number uint64) ChainHead {
	return ChainHead{Number: number, Hash: finalFleetGenerationTestHash(number)}
}

// makes one immutable-looking content locator for a pure shape fixture.
func finalFleetGenerationTestArtifact(kind, name string) FinalArtifactLocator {
	return FinalArtifactLocator{Kind: kind, URI: "final-derived/" + name + ".json", ContentHash: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1}
}

// makes a fixed-width nonzero lower-case hexadecimal hash for test values.
func finalFleetGenerationTestHash(value uint64) string {
	return fmt.Sprintf("0x%064x", value)
}
