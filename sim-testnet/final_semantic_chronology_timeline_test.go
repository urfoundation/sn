package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Supplies only the narrow archive observations used by baseline replay.
// The optional corruption makes the test prove the verifier compares every
// executable identity field rather than merely accepting a pinned request.
type finalHistoricalCoordinatorBaselineTestReader struct {
	corrupt bool
}

// Returns the sealed post-construction identity or one deliberate runtime
// substitution for the adjacent fail-closed regression.
func (self *finalHistoricalCoordinatorBaselineTestReader) HistoricalCoordinatorBaseline(_ context.Context, timeline FinalHistoricalCoordinatorProxyTimelineEvidence) (FinalHistoricalCoordinatorBaselineState, []FinalRPCExchange, error) {
	runtimeHash := timeline.Initialization.Post.RuntimeHash
	if self.corrupt {
		runtimeHash = finalTestHex(0x87)
	}
	implementation := timeline.Initialization.Post.Implementation
	return FinalHistoricalCoordinatorBaselineState{
		Proxy:                      timeline.Proxy,
		Implementation:             implementation,
		ObservedImplementationSlot: "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(implementation, "0x"),
		ProxyRuntimeHash:           timeline.ProxyRuntimeHash,
		ImplementationRuntimeHash:  runtimeHash,
		Block:                      timeline.Baseline,
	}, []FinalRPCExchange{{Chain: "evm", Method: "eth_getStorageAt", PinnedHead: timeline.Baseline}}, nil
}

// Satisfies the narrow chronology capability; baseline-specific tests never
// invoke carried-receipt replay.
func (self *finalHistoricalCoordinatorBaselineTestReader) HistoricalCoordinatorReceipt(_ context.Context, _ FinalHistoricalCoordinatorReceiptEvidence) (FinalHistoricalCoordinatorReceiptState, []FinalRPCExchange, error) {
	return FinalHistoricalCoordinatorReceiptState{}, nil, nil
}

// Satisfies the narrow chronology capability; baseline-specific tests never
// invoke the range census.
func (self *finalHistoricalCoordinatorBaselineTestReader) CoordinatorUpgradeRange(_ context.Context, _ FinalCoordinatorUpgradeRangeEvidence) ([]FinalCoordinatorUpgradeRangeState, []FinalRPCExchange, error) {
	return nil, nil, nil
}

// Builds two retained proxy histories with one upgrade, enough to exercise
// constructor, pre-upgrade, activation, and post-upgrade execution state.
func finalHistoricalCoordinatorTimelineTestFixture(t *testing.T) (*FinalSemanticEvidence, *SetupPlan, map[string]*SetupPlan, []JournalEntry, map[string][]finalCanonicalEVMLog, []FinalCollectedCoordinatorBaseline, map[string]JournalEntry) {
	t.Helper()
	proxyOld := strings.ToLower(common.HexToAddress("0x1000000000000000000000000000000000000001").Hex())
	proxyCurrent := strings.ToLower(common.HexToAddress("0x2000000000000000000000000000000000000002").Hex())
	implementationOld := strings.ToLower(common.HexToAddress("0x3000000000000000000000000000000000000003").Hex())
	implementationUpgraded := strings.ToLower(common.HexToAddress("0x4000000000000000000000000000000000000004").Hex())
	implementationCurrent := strings.ToLower(common.HexToAddress("0x5000000000000000000000000000000000000005").Hex())
	planOldHash := finalTestHex(0x11)
	planCurrentHash := finalTestHex(0x12)
	deploymentID := "timeline-fixture"
	oldInitialization := Action{ID: "evm.coordinator-proxy", Kind: "evm-transaction", Target: proxyOld, IntentHash: finalTestHex(0x21)}
	oldBefore := Action{ID: "policy.before-upgrade", Kind: "evm-transaction", Target: proxyOld, IntentHash: finalTestHex(0x22)}
	oldUpgrade := Action{ID: "evm.coordinator-upgrade-activate", Kind: "evm-transaction", Target: proxyOld, IntentHash: finalTestHex(0x23)}
	oldAfter := Action{ID: "policy.after-upgrade", Kind: "evm-transaction", Target: proxyOld, IntentHash: finalTestHex(0x24)}
	currentInitialization := Action{ID: "evm.coordinator-proxy", Kind: "evm-transaction", Target: proxyCurrent, IntentHash: finalTestHex(0x25)}
	old := &SetupPlan{
		PlanHash: planOldHash, DeploymentID: deploymentID,
		Deployment:         ContractDeployment{CoordinatorProxy: common.HexToAddress(proxyOld), CoordinatorImplementation: common.HexToAddress(implementationOld), RuntimeHashes: map[string]string{implementationOld: finalTestHex(0x31)}},
		CoordinatorUpgrade: CoordinatorUpgrade{Implementation: common.HexToAddress(implementationUpgraded), RuntimeCodeHash: finalTestHex(0x32)},
		Actions:            []Action{oldInitialization, oldBefore, oldUpgrade, oldAfter},
	}
	current := &SetupPlan{
		PlanHash: planCurrentHash, DeploymentID: deploymentID, PriorPlanHashes: []string{planOldHash},
		Deployment: ContractDeployment{CoordinatorProxy: common.HexToAddress(proxyCurrent), CoordinatorImplementation: common.HexToAddress(implementationCurrent), RuntimeHashes: map[string]string{implementationCurrent: finalTestHex(0x33)}},
		Actions:    []Action{currentInitialization},
	}
	entry := func(action Action, transactionByte byte, block uint64, transactionIndex uint64) JournalEntry {
		return JournalEntry{
			Schema: "urnetwork-sim-journal-v1", DeploymentID: deploymentID, PlanHash: planOldHash, ActionID: action.ID, IntentHash: action.IntentHash,
			Stage: StageFinalized, TransactionHash: finalTestHex(transactionByte), BlockNumber: block, BlockHash: finalTestHex(byte(block)),
		}
	}
	oldInitializationEntry := entry(oldInitialization, 0x41, 10, 0)
	oldBeforeEntry := entry(oldBefore, 0x42, 12, 1)
	oldUpgradeEntry := entry(oldUpgrade, 0x43, 15, 0)
	oldAfterEntry := entry(oldAfter, 0x44, 17, 1)
	currentInitializationEntry := entry(currentInitialization, 0x45, 20, 0)
	currentInitializationEntry.PlanHash = planCurrentHash
	entries := []JournalEntry{oldInitializationEntry, oldBeforeEntry, oldUpgradeEntry, oldAfterEntry, currentInitializationEntry}
	upgradedTopic := strings.ToLower(crypto.Keccak256Hash([]byte("Upgraded(address)")).Hex())
	upgradedLog := func(entry JournalEntry, transactionIndex, logIndex uint64, proxy, implementation string) finalCanonicalEVMLog {
		return finalCanonicalEVMLog{
			Address: proxy, Topics: []string{upgradedTopic, common.BytesToHash(common.HexToAddress(implementation).Bytes()).Hex()}, Data: "0x",
			BlockNumber: entry.BlockNumber, BlockHash: entry.BlockHash, TransactionHash: entry.TransactionHash, TransactionIndex: transactionIndex, LogIndex: logIndex,
		}
	}
	nonUpgradeLog := func(entry JournalEntry, transactionIndex uint64, proxy string) finalCanonicalEVMLog {
		return finalCanonicalEVMLog{
			Address: proxy, Topics: []string{finalTestHex(0x99)}, Data: "0x",
			BlockNumber: entry.BlockNumber, BlockHash: entry.BlockHash, TransactionHash: entry.TransactionHash, TransactionIndex: transactionIndex, LogIndex: 0,
		}
	}
	logs := map[string][]finalCanonicalEVMLog{
		oldInitializationEntry.TransactionHash:     {upgradedLog(oldInitializationEntry, 0, 0, proxyOld, implementationOld)},
		oldBeforeEntry.TransactionHash:             {nonUpgradeLog(oldBeforeEntry, 1, proxyOld)},
		oldUpgradeEntry.TransactionHash:            {upgradedLog(oldUpgradeEntry, 0, 0, proxyOld, implementationUpgraded)},
		oldAfterEntry.TransactionHash:              {nonUpgradeLog(oldAfterEntry, 1, proxyOld)},
		currentInitializationEntry.TransactionHash: {upgradedLog(currentInitializationEntry, 0, 0, proxyCurrent, implementationCurrent)},
	}
	baselines := []FinalCollectedCoordinatorBaseline{
		{Proxy: proxyOld, Head: ChainHead{Number: oldInitializationEntry.BlockNumber, Hash: oldInitializationEntry.BlockHash}, Implementation: implementationOld, ImplementationRuntimeHash: finalTestHex(0x31), ProxyRuntimeHash: finalTestHex(0x51)},
		{Proxy: proxyCurrent, Head: ChainHead{Number: currentInitializationEntry.BlockNumber, Hash: currentInitializationEntry.BlockHash}, Implementation: implementationCurrent, ImplementationRuntimeHash: finalTestHex(0x33), ProxyRuntimeHash: finalTestHex(0x52)},
	}
	evidence := &FinalSemanticEvidence{DeploymentID: deploymentID, EVMCampaignStartHead: ChainHead{Number: 30, Hash: finalTestHex(0x70)}}
	return evidence, current, map[string]*SetupPlan{planOldHash: old, planCurrentHash: current}, entries, logs, baselines, map[string]JournalEntry{
		"initialization": oldInitializationEntry, "before": oldBeforeEntry, "upgrade": oldUpgradeEntry, "after": oldAfterEntry,
	}
}

// Confirms that historical dispatch identity follows actual proxy transitions
// rather than the static implementation field retained by a later plan.
func TestFinalSemanticHistoricalCoordinatorTimelineBindsExecutionAndPostState(t *testing.T) {
	evidence, current, plans, entries, logs, baselines, rows := finalHistoricalCoordinatorTimelineTestFixture(t)
	timeline, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil {
		t.Fatal(err)
	}
	serialized := timeline.evidence()
	if err := verifyFinalHistoricalCoordinatorTimeline(serialized); err != nil {
		t.Fatalf("timeline rejected: %v", err)
	}
	old := plans[current.PriorPlanHashes[0]]
	lineage := timeline.proxies[strings.ToLower(old.Deployment.CoordinatorProxy.Hex())]
	if lineage.Initialization.Execution != (FinalHistoricalCoordinatorRuntimeIdentity{}) || lineage.Initialization.Post.Implementation != strings.ToLower(old.Deployment.CoordinatorImplementation.Hex()) {
		t.Fatalf("initializer execution/post=%+v/%+v, want undelegated/%s", lineage.Initialization.Execution, lineage.Initialization.Post, old.Deployment.CoordinatorImplementation.Hex())
	}
	if len(lineage.Upgrades) != 1 || lineage.Upgrades[0].Execution.Implementation != strings.ToLower(old.Deployment.CoordinatorImplementation.Hex()) || lineage.Upgrades[0].Post.Implementation != strings.ToLower(old.CoordinatorUpgrade.Implementation.Hex()) {
		t.Fatalf("upgrade transition=%+v, want old-to-new implementation", lineage.Upgrades)
	}
	beforeAction, err := exactPlanActionByID(old, rows["before"].ActionID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := timeline.receiptRuntime(old, beforeAction, rows["before"], logs[rows["before"].TransactionHash])
	if err != nil || before.execution != lineage.Initialization.Post || before.post != lineage.Initialization.Post || before.executionHead != lineage.Baseline {
		t.Fatalf("pre-upgrade runtime=%+v err=%v, want initial runtime at baseline", before, err)
	}
	upgradeAction, err := exactPlanActionByID(old, rows["upgrade"].ActionID)
	if err != nil {
		t.Fatal(err)
	}
	upgrade, err := timeline.receiptRuntime(old, upgradeAction, rows["upgrade"], logs[rows["upgrade"].TransactionHash])
	if err != nil || upgrade.execution != lineage.Initialization.Post || upgrade.post != lineage.Upgrades[0].Post || upgrade.executionHead != lineage.Baseline {
		t.Fatalf("upgrade runtime=%+v err=%v, want old execution/new post state", upgrade, err)
	}
	afterAction, err := exactPlanActionByID(old, rows["after"].ActionID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := timeline.receiptRuntime(old, afterAction, rows["after"], logs[rows["after"].TransactionHash])
	if err != nil || after.execution != lineage.Upgrades[0].Post || after.post != lineage.Upgrades[0].Post || after.executionHead != lineage.Upgrades[0].Block {
		t.Fatalf("post-upgrade runtime=%+v err=%v, want upgraded runtime", after, err)
	}
}

// Keeps a later approved revision from falling back to its static deployment
// implementation after the retained proxy has already crossed an upgrade.
func TestFinalSemanticHistoricalCoordinatorTimelineCarriesLaterPlanCallAcrossOldUpgrade(t *testing.T) {
	evidence, current, plans, entries, logs, baselines, _ := finalHistoricalCoordinatorTimelineTestFixture(t)
	old := plans[current.PriorPlanHashes[0]]
	laterHash := finalTestHex(0x13)
	laterAction := Action{
		ID:         "policy.later-plan-call",
		Kind:       "evm-transaction",
		Target:     strings.ToLower(old.Deployment.CoordinatorProxy.Hex()),
		IntentHash: finalTestHex(0x26),
	}
	later := &SetupPlan{
		PlanHash:     laterHash,
		DeploymentID: old.DeploymentID,
		// This deliberately preserves the obsolete static value: timeline
		// attribution must instead use the old proxy's observed upgrade.
		Deployment: ContractDeployment{
			CoordinatorProxy:          old.Deployment.CoordinatorProxy,
			CoordinatorImplementation: old.Deployment.CoordinatorImplementation,
			RuntimeHashes: map[string]string{
				old.Deployment.CoordinatorImplementation.Hex(): finalTestHex(0x31),
			},
		},
		Actions: []Action{laterAction},
	}
	current.PriorPlanHashes = append(current.PriorPlanHashes, laterHash)
	plans[laterHash] = later
	laterEntry := JournalEntry{
		Schema:          "urnetwork-sim-journal-v1",
		DeploymentID:    old.DeploymentID,
		PlanHash:        laterHash,
		ActionID:        laterAction.ID,
		IntentHash:      laterAction.IntentHash,
		Stage:           StageFinalized,
		TransactionHash: finalTestHex(0x46),
		BlockNumber:     19,
		BlockHash:       finalTestHex(19),
	}
	entries = append(entries, laterEntry)
	logs[laterEntry.TransactionHash] = []finalCanonicalEVMLog{{
		Address:          strings.ToLower(old.Deployment.CoordinatorProxy.Hex()),
		Topics:           []string{finalTestHex(0x99)},
		Data:             "0x",
		BlockNumber:      laterEntry.BlockNumber,
		BlockHash:        laterEntry.BlockHash,
		TransactionHash:  laterEntry.TransactionHash,
		TransactionIndex: 1,
		LogIndex:         0,
	}}
	timeline, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := timeline.receiptRuntime(later, laterAction, laterEntry, logs[laterEntry.TransactionHash])
	if err != nil {
		t.Fatal(err)
	}
	want := timeline.proxies[strings.ToLower(old.Deployment.CoordinatorProxy.Hex())].Upgrades[0].Post
	if runtime.execution != want || runtime.post != want {
		t.Fatalf("later-plan execution/post=%+v/%+v, want observed upgraded identity %+v", runtime.execution, runtime.post, want)
	}
	if runtime.execution.Implementation == strings.ToLower(later.Deployment.CoordinatorImplementation.Hex()) {
		t.Fatal("later-plan call fell back to its stale static deployment implementation")
	}
}

// Rejects a claimed baseline which does not reproduce the initializer event's
// observed implementation and runtime hash.
func TestFinalSemanticHistoricalCoordinatorTimelineRejectsSubstitutedBaseline(t *testing.T) {
	evidence, current, plans, entries, logs, baselines, _ := finalHistoricalCoordinatorTimelineTestFixture(t)
	baselines[0].ImplementationRuntimeHash = finalTestHex(0x71)
	if _, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines); err == nil {
		t.Fatal("accepted a substituted historical coordinator baseline")
	}
}

// Refuses an ordinary call in an upgrade block because archive block-state
// queries expose only the post-block slot, not the earlier transaction state.
func TestFinalSemanticHistoricalCoordinatorTimelineRejectsMixedUpgradeBlock(t *testing.T) {
	evidence, current, plans, entries, logs, baselines, rows := finalHistoricalCoordinatorTimelineTestFixture(t)
	timeline, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil {
		t.Fatal(err)
	}
	old := plans[current.PriorPlanHashes[0]]
	action, err := exactPlanActionByID(old, rows["before"].ActionID)
	if err != nil {
		t.Fatal(err)
	}
	entry := rows["before"]
	entry.BlockNumber = rows["upgrade"].BlockNumber
	entry.BlockHash = rows["upgrade"].BlockHash
	entry.TransactionHash = finalTestHex(0x88)
	logs[entry.TransactionHash] = []finalCanonicalEVMLog{{Address: strings.ToLower(old.Deployment.CoordinatorProxy.Hex()), Topics: []string{finalTestHex(0x99)}, Data: "0x", BlockNumber: entry.BlockNumber, BlockHash: entry.BlockHash, TransactionHash: entry.TransactionHash, TransactionIndex: 1, LogIndex: 0}}
	if _, err := timeline.receiptRuntime(old, action, entry, logs[entry.TransactionHash]); err == nil {
		t.Fatal("accepted an ordinary coordinator call sharing an upgrade block")
	}
}

// Rejects serialized transitions that no longer form one observable
// block-by-block implementation state machine before artifact replay trusts
// their claimed execution identities.
func TestFinalSemanticHistoricalCoordinatorTimelineRejectsBrokenTransitionChain(t *testing.T) {
	evidence, current, plans, entries, logs, baselines, _ := finalHistoricalCoordinatorTimelineTestFixture(t)
	timeline, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil {
		t.Fatal(err)
	}
	baseline := timeline.evidence()
	mutations := []struct {
		name   string
		mutate func([]FinalHistoricalCoordinatorProxyTimelineEvidence)
	}{
		{name: "initializer and upgrade share a block", mutate: func(values []FinalHistoricalCoordinatorProxyTimelineEvidence) {
			values[0].Upgrades[0].Block = values[0].Initialization.Block
		}},
		{name: "two upgrades share a block", mutate: func(values []FinalHistoricalCoordinatorProxyTimelineEvidence) {
			copy := values[0].Upgrades[0]
			copy.TransactionHash = finalTestHex(0x85)
			copy.TransactionIndex++
			copy.Execution = values[0].Upgrades[0].Post
			copy.Post = values[0].Upgrades[0].Post
			values[0].Upgrades = append(values[0].Upgrades, copy)
		}},
		{name: "execution does not match prior post", mutate: func(values []FinalHistoricalCoordinatorProxyTimelineEvidence) {
			values[0].Upgrades[0].Execution = FinalHistoricalCoordinatorRuntimeIdentity{Implementation: "0x6000000000000000000000000000000000000006", RuntimeHash: finalTestHex(0x86)}
		}},
	}
	for _, mutation := range mutations {
		data, marshalErr := json.Marshal(baseline)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		var value []FinalHistoricalCoordinatorProxyTimelineEvidence
		if unmarshalErr := json.Unmarshal(data, &value); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		mutation.mutate(value)
		if verifyErr := verifyFinalHistoricalCoordinatorTimeline(value); verifyErr == nil {
			t.Errorf("%s mutation was accepted", mutation.name)
		}
	}
}

// Ensures every constructor baseline is independently reread even when no
// ordinary carried receipt happens to request its exact canonical head.
func TestFinalSemanticHistoricalCoordinatorBaselinesReplayEveryInitialization(t *testing.T) {
	evidence, current, plans, entries, logs, baselines, _ := finalHistoricalCoordinatorTimelineTestFixture(t)
	timeline, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil {
		t.Fatal(err)
	}
	timelines := timeline.evidence()
	reader := &finalHistoricalCoordinatorBaselineTestReader{}
	appendExchanges := func(chain string, head ChainHead, exchanges []FinalRPCExchange) error {
		if chain != "evm" || len(exchanges) != 1 || exchanges[0].PinnedHead != head {
			t.Fatalf("baseline transcript=%+v chain=%q head=%+v", exchanges, chain, head)
		}
		return nil
	}
	if err := verifyFinalSemanticCoordinatorBaselinesOnChain(context.Background(), timelines, reader, appendExchanges); err != nil {
		t.Fatalf("exact historical coordinator baselines rejected: %v", err)
	}
	reader.corrupt = true
	if err := verifyFinalSemanticCoordinatorBaselinesOnChain(context.Background(), timelines, reader, appendExchanges); err == nil {
		t.Fatal("substituted historical coordinator baseline runtime was accepted")
	}
}

// Rebuilds every retained transition from approved plan actions and raw
// Upgraded logs, so a self-consistent top-level timeline cannot replace a
// historical executable identity after source capture.
func TestFinalSemanticHistoricalCoordinatorTimelineArtifactRebuildsTransitions(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*FinalSemanticEvidence, *finalHistoricalCoordinatorTimelineArtifact, map[string]JournalEntry)
	}{
		{name: "substituted upgrade runtime", mutate: func(evidence *FinalSemanticEvidence, artifact *finalHistoricalCoordinatorTimelineArtifact, _ map[string]JournalEntry) {
			artifact.Timelines[0].Upgrades[0].Post.RuntimeHash = finalTestHex(0x91)
			evidence.HistoricalCoordinatorTimeline = artifact.Timelines
		}},
		{name: "substituted upgrade event implementation", mutate: func(_ *FinalSemanticEvidence, artifact *finalHistoricalCoordinatorTimelineArtifact, rows map[string]JournalEntry) {
			for index := range artifact.UpgradedLogs {
				if artifact.UpgradedLogs[index].TransactionHash != rows["upgrade"].TransactionHash {
					continue
				}
				artifact.UpgradedLogs[index].Topics[1] = strings.ToLower(common.BytesToHash(common.HexToAddress("0x6000000000000000000000000000000000000006").Bytes()).Hex())
				return
			}
			t.Fatal("fixture has no upgrade log")
		}},
	}
	for _, mutation := range mutations {
		evidence, current, plans, entries, logs, baselines, rows := finalHistoricalCoordinatorTimelineTestFixture(t)
		timeline, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
		if err != nil {
			t.Fatal(err)
		}
		evidence.HistoricalCoordinatorTimeline = timeline.evidence()
		artifact, err := finalHistoricalCoordinatorTimelineArtifactFromSource(timeline, baselines, logs)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyFinalHistoricalCoordinatorTimelineArtifact(evidence, current, plans, entries, data); err != nil {
			t.Fatalf("exact timeline artifact rejected: %v", err)
		}
		mutation.mutate(evidence, &artifact, rows)
		data, err = json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyFinalHistoricalCoordinatorTimelineArtifact(evidence, current, plans, entries, data); err == nil {
			t.Errorf("%s mutation was accepted", mutation.name)
		}
	}
}

// Projects every constructor and activation into proxy-specific archive
// ranges, so an unrecorded upgrade between retained receipts cannot hide in
// a current-campaign-only log query.
func TestFinalSemanticHistoricalCoordinatorTimelineBuildsCompleteArchiveRanges(t *testing.T) {
	evidence, current, plans, entries, logs, baselines, _ := finalHistoricalCoordinatorTimelineTestFixture(t)
	timeline, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil {
		t.Fatal(err)
	}
	evidence.HistoricalCoordinatorTimeline = timeline.evidence()
	ranges, events, err := finalHistoricalCoordinatorUpgradeRanges(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 2 || len(events) != 3 {
		t.Fatalf("ranges/events=%d/%d, want two proxies and three transitions", len(ranges), len(events))
	}
	for _, rangeEvidence := range ranges {
		if rangeEvidence.From.Number == 0 || rangeEvidence.To != evidence.EVMCampaignStartHead || len(finalCoordinatorUpgradeEventsForRange(events, rangeEvidence)) == 0 {
			t.Fatalf("incomplete archived proxy range: %+v events=%+v", rangeEvidence, events)
		}
	}
	if err := verifyFinalHistoricalCoordinatorUpgradeEventCoverage(ranges, events[:2]); err == nil {
		t.Fatal("accepted a historical proxy range with its constructor event omitted")
	}
}

// Separates post-receipt and pre-execution archive reads into the exact
// canonical transcript heads instead of falsely pinning both to the receipt.
func TestFinalSemanticHistoricalCoordinatorReceiptTranscriptPinsExecutionHead(t *testing.T) {
	receiptHead := ChainHead{Number: 20, Hash: finalTestHex(0x81)}
	executionHead := ChainHead{Number: 19, Hash: finalTestHex(0x82)}
	row := FinalHistoricalCoordinatorReceiptEvidence{
		ActionID:      "evm.coordinator-upgrade-activate",
		Receipt:       FinalEVMReceipt{TransactionHash: finalTestHex(0x83), Block: receiptHead},
		ExecutionHead: executionHead,
	}
	exchanges := []FinalRPCExchange{
		{Chain: "evm", Method: "eth_getTransactionReceipt", PinnedHead: receiptHead},
		{Chain: "evm", Method: "eth_getStorageAt", PinnedHead: receiptHead},
		{Chain: "evm", Method: "eth_getStorageAt", PinnedHead: executionHead},
	}
	var heads []ChainHead
	var groupSizes []int
	appendExchanges := func(chain string, head ChainHead, values []FinalRPCExchange) error {
		if chain != "evm" {
			t.Fatalf("chain=%q, want evm", chain)
		}
		heads = append(heads, head)
		groupSizes = append(groupSizes, len(values))
		return nil
	}
	if err := appendFinalHistoricalCoordinatorReceiptExchanges(row, exchanges, appendExchanges); err != nil {
		t.Fatal(err)
	}
	if len(heads) != 2 || heads[0] != receiptHead || heads[1] != executionHead || len(groupSizes) != 2 || groupSizes[0] != 2 || groupSizes[1] != 1 {
		t.Fatalf("heads=%+v groups=%v, want receipt/execution groups 2/1", heads, groupSizes)
	}
	exchanges[2].PinnedHead = ChainHead{Number: 18, Hash: finalTestHex(0x84)}
	if err := appendFinalHistoricalCoordinatorReceiptExchanges(row, exchanges, appendExchanges); err == nil {
		t.Fatal("accepted transcript exchange at an unsealed execution head")
	}
}
