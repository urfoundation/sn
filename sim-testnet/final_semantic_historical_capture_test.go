package main

// These tests keep the live historical-log query tied to signed plan lineage
// and ensure ordinary semantic selectors cannot consume a prior generation.

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Holds a minimal approved current/predecessor pair with one finalized prior
// EVM action. It intentionally uses a journal coordinate before the active
// deployment checkpoint to exercise the historical query boundary.
type finalHistoricalCaptureFixture struct {
	current    *SetupPlan
	prior      *SetupPlan
	deployment *ContractDeployment
	batcher    common.Address
	plans      map[string]*SetupPlan
	entries    []JournalEntry
}

// Builds a compact lineage whose only historical contract graph comes from
// the explicitly retained predecessor. The real plan decoder authenticates
// this map before production passes it to the pure census function.
func newFinalHistoricalCaptureFixture() finalHistoricalCaptureFixture {
	currentHash := finalTestHex(0x11)
	priorHash := finalTestHex(0x22)
	intent := finalTestHex(0x33)
	currentDeployment := ContractDeployment{
		DeploymentID: "deployment", DeployBlock: 100,
		CoordinatorProxy:          common.HexToAddress("0x1000000000000000000000000000000000000001"),
		SettlementVault:           common.HexToAddress("0x2000000000000000000000000000000000000002"),
		ReserveSink:               common.HexToAddress("0x3000000000000000000000000000000000000003"),
		CoordinatorImplementation: common.HexToAddress("0x4000000000000000000000000000000000000004"),
	}
	priorDeployment := ContractDeployment{
		DeploymentID: "deployment", DeployBlock: 60,
		CoordinatorProxy:          common.HexToAddress("0x5000000000000000000000000000000000000005"),
		SettlementVault:           common.HexToAddress("0x6000000000000000000000000000000000000006"),
		ReserveSink:               common.HexToAddress("0x7000000000000000000000000000000000000007"),
		CoordinatorImplementation: common.HexToAddress("0x8000000000000000000000000000000000000008"),
	}
	prior := &SetupPlan{
		PlanHash: priorHash, DeploymentID: "deployment", ChainID: testnetChainID, Netuid: 521, Deployment: priorDeployment,
		Actions: []Action{{ID: "operator.register.1", Kind: "evm-transaction", IntentHash: intent}},
	}
	current := &SetupPlan{
		PlanHash: currentHash, DeploymentID: "deployment", ChainID: testnetChainID, Netuid: 521, Deployment: currentDeployment,
		PriorPlanHashes: []string{priorHash},
	}
	return finalHistoricalCaptureFixture{
		current: current, prior: prior, deployment: &currentDeployment,
		batcher: common.HexToAddress("0x9000000000000000000000000000000000000009"),
		plans:   map[string]*SetupPlan{currentHash: current, priorHash: prior},
		entries: []JournalEntry{{
			DeploymentID: "deployment", PlanHash: priorHash, ActionID: "operator.register.1", IntentHash: intent,
			Stage: StageFinalized, BlockNumber: 41,
		}},
	}
}

// Requires the query to include each retained predecessor emitter and start
// at the earliest approved EVM action, not merely the active deployment.
func TestFinalSemanticHistoricalCaptureCensusBindsAllowedLineageAndBoundary(t *testing.T) {
	fixture := newFinalHistoricalCaptureFixture()
	fixture.entries = append(fixture.entries, JournalEntry{
		DeploymentID: "deployment", PlanHash: finalTestHex(0x44), ActionID: "foreign", IntentHash: finalTestHex(0x55), Stage: StageFinalized, BlockNumber: 1,
	})
	census, err := finalCaptureReleaseContractCensusForLineage(fixture.current, fixture.deployment, fixture.batcher, fixture.plans, fixture.entries)
	if err != nil {
		t.Fatal(err)
	}
	if census.fromBlock != 41 {
		t.Fatalf("historical capture starts at %d, want earliest authenticated EVM action 41", census.fromBlock)
	}
	for _, address := range []common.Address{
		fixture.deployment.CoordinatorProxy, fixture.deployment.SettlementVault, fixture.deployment.ReserveSink,
		fixture.prior.Deployment.CoordinatorProxy, fixture.prior.Deployment.SettlementVault, fixture.prior.Deployment.ReserveSink,
		fixture.batcher,
	} {
		if !finalHistoricalCaptureContains(census.releaseAddresses, address) {
			t.Fatalf("historical census omitted approved address %s: %v", address, census.releaseAddresses)
		}
	}
	if len(census.currentAddresses) != 4 || finalHistoricalCaptureContains(census.currentAddresses, fixture.prior.Deployment.SettlementVault) {
		t.Fatalf("current census=%v unexpectedly includes predecessor graph", census.currentAddresses)
	}
}

// Rejects both a missing authenticated predecessor and a foreign plan injected
// into the in-memory lineage map. Neither mutation may extend or shrink the
// query set which determines the immutable live capture.
func TestFinalSemanticHistoricalCaptureCensusRejectsOmissionAndUnknownPlanInjection(t *testing.T) {
	fixture := newFinalHistoricalCaptureFixture()
	missing := fixture
	missing.plans = map[string]*SetupPlan{fixture.current.PlanHash: fixture.current}
	if _, err := finalCaptureReleaseContractCensusForLineage(missing.current, missing.deployment, missing.batcher, missing.plans, missing.entries); err == nil {
		t.Fatal("accepted a capture census without its approved predecessor")
	}
	foreign := fixture
	foreign.plans = make(map[string]*SetupPlan, len(fixture.plans)+1)
	for hash, plan := range fixture.plans {
		foreign.plans[hash] = plan
	}
	foreign.plans[finalTestHex(0x66)] = &SetupPlan{
		PlanHash: finalTestHex(0x66), DeploymentID: fixture.current.DeploymentID, ChainID: fixture.current.ChainID, Netuid: fixture.current.Netuid,
		Deployment: ContractDeployment{
			CoordinatorProxy: common.HexToAddress("0xa00000000000000000000000000000000000000a"),
			SettlementVault:  common.HexToAddress("0xb00000000000000000000000000000000000000b"),
			ReserveSink:      common.HexToAddress("0xc00000000000000000000000000000000000000c"),
		},
	}
	if _, err := finalCaptureReleaseContractCensusForLineage(foreign.current, foreign.deployment, foreign.batcher, foreign.plans, foreign.entries); err == nil {
		t.Fatal("accepted a foreign predecessor emitter graph")
	}
}

// Rejects a post-capture census rewrite even when all inserted addresses are
// well-formed. The source replays the approved lineage rather than trusting
// the snapshot's self-described query list or starting block.
func TestFinalSemanticHistoricalCaptureRejectsSnapshotCensusMutation(t *testing.T) {
	fixture := newFinalHistoricalCaptureFixture()
	census, err := finalCaptureReleaseContractCensusForLineage(fixture.current, fixture.deployment, fixture.batcher, fixture.plans, fixture.entries)
	if err != nil {
		t.Fatal(err)
	}
	chain := &FinalCollectedChainSnapshot{
		FleetBatcher:             strings.ToLower(fixture.batcher.Hex()),
		EVMFromBlock:             census.fromBlock,
		CurrentReleaseFromBlock:  fixture.deployment.DeployBlock,
		CurrentReleaseAddresses:  append([]string(nil), census.currentAddresses...),
		ReleaseContractAddresses: append([]string(nil), census.releaseAddresses...),
	}
	source := &finalHistoricalCoordinatorSource{current: fixture.current, deployment: fixture.deployment, chain: chain, plans: fixture.plans, entries: fixture.entries}
	if err := source.verifyReleaseCaptureCensus(); err != nil {
		t.Fatalf("exact release census rejected: %v", err)
	}
	chain.ReleaseContractAddresses = append(chain.ReleaseContractAddresses, "0xa00000000000000000000000000000000000000a")
	if err := source.verifyReleaseCaptureCensus(); err == nil {
		t.Fatal("accepted a foreign historical emitter in the captured census")
	}
	chain.ReleaseContractAddresses = append([]string(nil), census.releaseAddresses...)
	chain.ReleaseContractAddresses = chain.ReleaseContractAddresses[1:]
	if err := source.verifyReleaseCaptureCensus(); err == nil {
		t.Fatal("accepted a captured census that omitted an approved predecessor emitter")
	}
	chain.ReleaseContractAddresses = append([]string(nil), census.releaseAddresses...)
	chain.EVMFromBlock++
	if err := source.verifyReleaseCaptureCensus(); err == nil {
		t.Fatal("accepted a captured census that starts after its earliest carried action")
	}
}

// Preserves the approved plan's intentional pre-deployment zero while using
// the separately authenticated public deployment checkpoint for capture. A
// release revision is valid before it broadcasts, so replay must never treat
// the plan placeholder as the historical query boundary.
func TestFinalSemanticHistoricalCaptureUsesLiveDeploymentForZeroPlanBlock(t *testing.T) {
	fixture := newFinalHistoricalCaptureFixture()
	fixture.current.Deployment.DeployBlock = 0
	fixture.prior.Deployment.DeployBlock = 0
	census, err := finalCaptureReleaseContractCensusForLineage(fixture.current, fixture.deployment, fixture.batcher, fixture.plans, fixture.entries)
	if err != nil {
		t.Fatalf("live deployment checkpoint rejected zero-block plans: %v", err)
	}
	if census.fromBlock != fixture.entries[0].BlockNumber {
		t.Fatalf("capture boundary=%d, want finalized historical block %d", census.fromBlock, fixture.entries[0].BlockNumber)
	}
	chain := &FinalCollectedChainSnapshot{
		FleetBatcher:             strings.ToLower(fixture.batcher.Hex()),
		EVMFromBlock:             census.fromBlock,
		CurrentReleaseFromBlock:  fixture.deployment.DeployBlock,
		CurrentReleaseAddresses:  append([]string(nil), census.currentAddresses...),
		ReleaseContractAddresses: append([]string(nil), census.releaseAddresses...),
	}
	source := &finalHistoricalCoordinatorSource{
		current:    fixture.current,
		deployment: fixture.deployment,
		chain:      chain,
		plans:      fixture.plans,
		entries:    fixture.entries,
	}
	if err := source.verifyReleaseCaptureCensus(); err != nil {
		t.Fatalf("authenticated live deployment census rejected: %v", err)
	}
	if _, err := finalCaptureReleaseContractCensusForLineage(fixture.current, &fixture.current.Deployment, fixture.batcher, fixture.plans, fixture.entries); err == nil {
		t.Fatal("accepted a persisted zero deployment block as the live capture checkpoint")
	}
}

// Proves that older logs remain available by transaction for exact receipt
// replay while same-address prior-generation events cannot enter ordinary
// by-name pool selection after the active deployment boundary.
func TestFinalSemanticHistoricalCaptureScopesCurrentAndPredecessorEventCollisions(t *testing.T) {
	payload := newFinalReceiptPayloadFixture(t)
	currentRegistration := payload.logs[payload.registration.TransactionHash][0]
	historical := currentRegistration
	historical.TransactionHash = finalTestHex(0xed)
	historical.BlockNumber = 49
	historical.BlockHash = finalTestHex(0xee)
	historical.LogIndex = 0
	snapshot := &FinalCollectedChainSnapshot{
		CurrentReleaseFromBlock: 50,
		CurrentReleaseAddresses: []string{
			"0x1000000000000000000000000000000000000001",
			"0x1000000000000000000000000000000000000013",
			"0x2000000000000000000000000000000000000002",
			"0x3000000000000000000000000000000000000003",
		},
		EVMLogs: []finalCanonicalEVMLog{historical, currentRegistration},
	}
	index, err := indexFinalSemanticEvents(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(index.byName["PoolRegistered"]); got != 1 || index.byName["PoolRegistered"][0].Log.TransactionHash != currentRegistration.TransactionHash {
		t.Fatalf("current PoolRegistered projection=%+v, want only active-generation event", index.byName["PoolRegistered"])
	}
	if got := len(index.byTx[historical.TransactionHash]); got != 1 || !finalSemanticCanonicalLogEqual(index.byTx[historical.TransactionHash][0], historical) {
		t.Fatalf("historical exact-replay logs=%+v, want retained collision event", index.byTx[historical.TransactionHash])
	}
}

// Matches a canonical address against the serialized census without allowing
// checksum or case aliases to make a passing assertion ambiguous.
func finalHistoricalCaptureContains(values []string, address common.Address) bool {
	needle := strings.ToLower(address.Hex())
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
