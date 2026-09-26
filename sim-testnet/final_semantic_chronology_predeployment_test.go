package main

// Predeployment ancestors retain authenticated planning history without adding
// a proxy, initialization, receipt, or archive query to the coordinator graph.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Authenticates a small historical wire image through the production decoder
// before its hash is retained by the synthetic deployed successor.
func finalHistoricalPredeploymentTestPlan(t *testing.T, current *SetupPlan, actions ...Action) *SetupPlan {
	t.Helper()
	actions = append([]Action{{ID: "subnet.verify-owner", Kind: "substrate-read", Target: "netuid:521"}}, actions...)
	for index := range actions {
		var err error
		actions[index].IntentHash, err = actionIntentHash(actions[index])
		if err != nil {
			t.Fatal(err)
		}
	}
	plan := &SetupPlan{
		Schema: "urnetwork-sim-plan-v1", DeploymentID: current.DeploymentID,
		ChainID: current.ChainID, GenesisHash: current.GenesisHash, Netuid: current.Netuid,
		Actions: actions,
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash, err = persistedSetupPlanHash(data, plan.Schema)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeFinalHistoricalPlanBytes(data)
	if err != nil {
		t.Fatalf("authenticate predeployment fixture: %v", err)
	}
	return decoded
}

// Keeps a complete deployed graph beside an authenticated legacy ancestor
// whose symbolic proxy action is planned while native and implementation
// preparation have already finalized.
type finalHistoricalPredeploymentTestFixture struct {
	evidence   *FinalSemanticEvidence
	current    *SetupPlan
	prior      *SetupPlan
	plans      map[string]*SetupPlan
	entries    []JournalEntry
	logs       map[string][]finalCanonicalEVMLog
	baselines  []FinalCollectedCoordinatorBaseline
	deployment ContractDeployment
	batcher    common.Address
}

// Separates native preparation from the lower Evm capture boundary without
// assigning either receipt to a nonexistent coordinator proxy.
func newFinalHistoricalPredeploymentTestFixture(t *testing.T) *finalHistoricalPredeploymentTestFixture {
	t.Helper()
	evidence, current, plans, entries, logs, baselines, _ := finalHistoricalCoordinatorTimelineTestFixture(t)
	old := plans[current.PriorPlanHashes[0]]
	old.Deployment.ReserveSink = common.Address{0x81}
	old.Deployment.SettlementVault = common.Address{0x82}
	current.Deployment.ReserveSink = common.Address{0x83}
	current.Deployment.SettlementVault = common.Address{0x84}
	deployment := current.Deployment
	deployment.DeployBlock = 20
	prior := finalHistoricalPredeploymentTestPlan(t, current,
		Action{ID: "native.prepare", Kind: "substrate-extrinsic", Target: "netuid:521"},
		Action{ID: "evm.coordinator-implementation", Kind: "evm-transaction", Target: "coordinator-implementation"},
		Action{ID: "evm.coordinator-proxy", Kind: "evm-transaction", Target: "coordinator-proxy"},
		Action{ID: "evm.vault-fix-coordinator", Kind: "evm-transaction", Target: "vault-fix-coordinator"},
		Action{ID: "governance.restore-coordinator", Kind: "evm-transaction", Target: "coordinator"},
	)
	current.PriorPlanHashes = append(current.PriorPlanHashes, prior.PlanHash)
	plans[prior.PlanHash] = prior
	for index, id := range []string{"native.prepare", "evm.coordinator-implementation"} {
		action, err := exactPlanActionByID(prior, id)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, JournalEntry{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash,
			ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized,
			TransactionHash: finalTestHex(0x91 + byte(index)), BlockNumber: 1 + uint64(index)*4, BlockHash: finalTestHex(0x93 + byte(index))})
	}
	return &finalHistoricalPredeploymentTestFixture{evidence: evidence, current: current, prior: prior, plans: plans,
		entries: entries, logs: logs, baselines: baselines, deployment: deployment, batcher: common.Address{0x85}}
}

// Returns a real transition identity under the zero-proxy predecessor. The
// guard must reject the row before its kind, height, target, or intent could
// make it disappear from the coordinator selector.
func (self *finalHistoricalPredeploymentTestFixture) transitionEntry(actionId string) JournalEntry {
	return JournalEntry{DeploymentID: self.prior.DeploymentID, PlanHash: self.prior.PlanHash, ActionID: actionId,
		IntentHash: finalTestHex(0x95), Stage: StageFinalized, TransactionHash: finalTestHex(0x96), BlockNumber: 6, BlockHash: finalTestHex(0x97)}
}

// A finalized implementation is preparation, not a proxy transition; it still
// lowers the release log floor, while the earlier native height does not.
func TestFinalSemanticHistoricalCoordinatorPredeploymentKeepsPreparatoryReceipts(t *testing.T) {
	fixture := newFinalHistoricalPredeploymentTestFixture(t)
	beforeEntries := fixture.entries[:len(fixture.entries)-2]
	want, err := finalHistoricalCoordinatorBuildTimeline(fixture.evidence, fixture.current, fixture.plans, beforeEntries, fixture.logs, fixture.baselines)
	if err != nil {
		t.Fatal(err)
	}
	wantTargets, err := finalHistoricalCoordinatorJournalActions(fixture.evidence, fixture.current, fixture.plans, beforeEntries)
	if err != nil {
		t.Fatal(err)
	}
	got, err := finalHistoricalCoordinatorBuildTimeline(fixture.evidence, fixture.current, fixture.plans, fixture.entries, fixture.logs, fixture.baselines)
	if err != nil || !finalHistoricalCoordinatorTimelinesEqual(want.evidence(), got.evidence()) {
		t.Fatal("preparatory receipts changed proxy chronology", err)
	}
	gotTargets, err := finalHistoricalCoordinatorJournalActions(fixture.evidence, fixture.current, fixture.plans, fixture.entries)
	if err != nil || !reflect.DeepEqual(wantTargets, gotTargets) {
		t.Fatal("preparatory receipts changed coordinator target census", err)
	}
	census, err := finalCaptureReleaseContractCensusForLineage(fixture.current, &fixture.deployment, fixture.batcher, fixture.plans, fixture.entries)
	if err != nil || census.fromBlock != 5 || len(census.releaseAddresses) != 7 {
		t.Fatalf("preparatory census=%+v err=%v, want Evm floor 5 and 7 deployed emitters", census, err)
	}
	if finalHistoricalCaptureContains(census.releaseAddresses, common.Address{}) {
		t.Fatal("predeployment placeholder introduced a zero emitter")
	}
}

// These mutations must be refused by the shared live/replay proxy census,
// timeline, receipt selector, and release capture before any RPC is possible.
func TestFinalSemanticHistoricalCoordinatorPredeploymentRejectsUnboundHistory(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*finalHistoricalPredeploymentTestFixture)
	}{
		{name: "unapproved ancestor", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.current.PriorPlanHashes = fixture.current.PriorPlanHashes[:1]
		}},
		{name: "missing approved ancestor", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			delete(fixture.plans, fixture.prior.PlanHash)
		}},
		{name: "substituted plan hash", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.PlanHash = finalTestHex(0xa1)
		}},
		{name: "foreign deployment", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.DeploymentID = "foreign"
		}},
		{name: "foreign chain", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.ChainID++
		}},
		{name: "foreign subnet", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.Netuid++
		}},
		{name: "current zero proxy", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.current.Deployment = ContractDeployment{}
		}},
		{name: "modern zero deployment", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.Schema = currentSetupPlanSchema
		}},
		{name: "unknown schema", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.Schema = "unapproved"
		}},
		{name: "partial emitter graph", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.Deployment.SettlementVault = common.Address{0xa2}
		}},
		{name: "partial implementation identity", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.Deployment.CoordinatorImplementation = common.Address{0xa3}
		}},
		{name: "deployment checkpoint", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.Deployment.DeployBlock = 1
		}},
		{name: "upgrade identity", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.CoordinatorUpgrade.Implementation = common.Address{0xa4}
		}},
		{name: "upgrade baseline", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.CoordinatorUpgradeBaseline.FinalizedBlock = 1
		}},
		{name: "retired deployment", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.prior.SupersededDeployments = []ContractDeployment{fixture.deployment}
		}},
		{name: "wrong preparatory intent", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.entries[len(fixture.entries)-1].IntentHash = finalTestHex(0xa5)
		}},
		{name: "unknown preparatory action", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			fixture.entries[len(fixture.entries)-1].ActionID = "unapproved.prepare"
		}},
		{name: "native disguised transition", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			for index := range fixture.prior.Actions {
				if fixture.prior.Actions[index].ID == "evm.coordinator-proxy" {
					fixture.prior.Actions[index].Kind = "substrate-extrinsic"
				}
			}
			fixture.entries = append(fixture.entries, fixture.transitionEntry("evm.coordinator-proxy"))
		}},
		{name: "foreign deployment transition", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			entry := fixture.transitionEntry("evm.coordinator-proxy")
			entry.DeploymentID = "foreign"
			fixture.entries = append(fixture.entries, entry)
		}},
		{name: "aliased transition plan hash", mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
			entry := fixture.transitionEntry("evm.coordinator-proxy")
			entry.PlanHash = strings.ToUpper(entry.PlanHash)
			fixture.entries = append(fixture.entries, entry)
		}},
	}
	for _, actionId := range []string{"evm.coordinator-proxy", "evm.coordinator-upgrade-activate", "repair.coordinator-rounding.activate"} {
		for _, height := range []uint64{0, 6, 30, 31} {
			tests = append(tests, struct {
				name   string
				mutate func(*finalHistoricalPredeploymentTestFixture)
			}{name: actionId + "/height" + strconv.FormatUint(height, 10), mutate: func(fixture *finalHistoricalPredeploymentTestFixture) {
				entry := fixture.transitionEntry(actionId)
				entry.BlockNumber = height
				fixture.entries = append(fixture.entries, entry)
			}})
		}
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFinalHistoricalPredeploymentTestFixture(t)
			test.mutate(fixture)
			if _, err := finalHistoricalCoordinatorBuildTimeline(fixture.evidence, fixture.current, fixture.plans, fixture.entries, fixture.logs, fixture.baselines); err == nil {
				t.Fatal("timeline accepted unbound predeployment history")
			}
			if _, err := finalHistoricalCoordinatorJournalActions(fixture.evidence, fixture.current, fixture.plans, fixture.entries); err == nil {
				t.Fatal("receipt selector accepted unbound predeployment history")
			}
			if _, err := finalCaptureReleaseContractCensusForLineage(fixture.current, &fixture.deployment, fixture.batcher, fixture.plans, fixture.entries); err == nil {
				t.Fatal("release census accepted unbound predeployment history")
			}
			if strings.Contains(test.name, "/height") {
				if _, err := finalHistoricalCoordinatorProxyCensus(fixture.current, fixture.plans, fixture.entries); err == nil || !strings.Contains(err.Error(), "finalized transition") {
					t.Fatalf("unbound transition escaped the shared pre-RPC census: %v", err)
				}
			}
		})
	}
}

// Live baseline capture accepts preparatory work but rejects an unbound proxy
// initialization before it can dial even the synthetic archive endpoint.
func TestFinalSemanticHistoricalCoordinatorPredeploymentBaselineCaptureBindsProxyBeforeRpc(t *testing.T) {
	for _, finalizedProxy := range []bool{false, true} {
		t.Run(strconv.FormatBool(finalizedProxy), func(t *testing.T) {
			cfg, root, current, head, logs := syntheticEVMBaselineFixture(t, false)
			prior := finalHistoricalPredeploymentTestPlan(t, current,
				Action{ID: "evm.coordinator-implementation", Kind: "evm-transaction", Target: "coordinator-implementation"},
				Action{ID: "evm.coordinator-proxy", Kind: "evm-transaction", Target: "coordinator-proxy"})
			current.PriorPlanHashes = []string{prior.PlanHash}
			data, err := json.Marshal(prior)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, "plans"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "plans", stringsTrim0x(prior.PlanHash)+".json"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			actionId := "evm.coordinator-implementation"
			if finalizedProxy {
				actionId = "evm.coordinator-proxy"
			}
			action, err := exactPlanActionByID(prior, actionId)
			if err != nil {
				t.Fatal(err)
			}
			journal, err := OpenJournal(root)
			if err != nil {
				t.Fatal(err)
			}
			entry := JournalEntry{DeploymentID: prior.DeploymentID, PlanHash: prior.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
				Stage: StageFinalized, TransactionHash: finalTestHex(0xa8), BlockNumber: 1, BlockHash: finalTestHex(0xa9)}
			appendErr := journal.Append(entry)
			if err := errors.Join(appendErr, journal.Close()); err != nil {
				t.Fatal(err)
			}
			var rpcCalls atomic.Int64
			if finalizedProxy {
				cfg.OperationalEVM = syntheticIdentityTestRPC(t, func(_ string, _ []json.RawMessage) (any, error) {
					rpcCalls.Add(1)
					return nil, errors.New("unexpected RPC for a zero-proxy transition")
				})
			}
			baselines, err := captureFinalHistoricalCoordinatorBaselines(t.Context(), cfg, root, current, ChainHead{Number: head.Number + 1, Hash: finalTestHex(0xaa)}, logs)
			if finalizedProxy {
				if err == nil || !strings.Contains(err.Error(), "finalized transition") || rpcCalls.Load() != 0 || baselines != nil {
					t.Fatalf("unbound initializer baselines=%+v RPC calls=%d err=%v", baselines, rpcCalls.Load(), err)
				}
			} else if err != nil || len(baselines) != 1 || baselines[0].Proxy != strings.ToLower(current.Deployment.CoordinatorProxy.Hex()) {
				t.Fatalf("preparatory capture baselines=%+v err=%v", baselines, err)
			}
		})
	}
}

// A placeholder cannot hide a transition behind an otherwise valid timeline
// artifact, and a nonzero proxy still requires its observed initialization.
func TestFinalSemanticHistoricalCoordinatorPredeploymentRetainsArtifactAndInitializationChecks(t *testing.T) {
	fixture := newFinalHistoricalPredeploymentTestFixture(t)
	timeline, err := finalHistoricalCoordinatorBuildTimeline(fixture.evidence, fixture.current, fixture.plans, fixture.entries, fixture.logs, fixture.baselines)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := finalHistoricalCoordinatorTimelineArtifactFromSource(timeline, fixture.baselines, fixture.logs)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	fixture.evidence.HistoricalCoordinatorTimeline = timeline.evidence()
	if err := verifyFinalHistoricalCoordinatorTimelineArtifact(fixture.evidence, fixture.current, fixture.plans, fixture.entries, data); err != nil {
		t.Fatal(err)
	}
	entries := append(append([]JournalEntry(nil), fixture.entries...), fixture.transitionEntry("evm.coordinator-proxy"))
	if err := verifyFinalHistoricalCoordinatorTimelineArtifact(fixture.evidence, fixture.current, fixture.plans, entries, data); err == nil || !strings.Contains(err.Error(), "finalized transition") {
		t.Fatalf("artifact accepted an unbound finalized transition: %v", err)
	}
	fixture.prior.Deployment.CoordinatorProxy = common.Address{0xa6}
	if _, err := finalHistoricalCoordinatorBuildTimeline(fixture.evidence, fixture.current, fixture.plans, fixture.entries, fixture.logs, fixture.baselines); err == nil || !strings.Contains(err.Error(), "no approved initialization") {
		t.Fatalf("newly claimed proxy escaped initialization evidence: %v", err)
	}
}

// Adding an explicitly retained, unexecuted ancestor must preserve the exact
// two-proxy transition graph and its independent artifact reconstruction.
func TestFinalSemanticHistoricalCoordinatorTimelineAcceptsAuthenticatedPredeployment(t *testing.T) {
	evidence, current, plans, entries, logs, baselines, _ := finalHistoricalCoordinatorTimelineTestFixture(t)
	want, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil {
		t.Fatal(err)
	}
	prior := finalHistoricalPredeploymentTestPlan(t, current)
	current.PriorPlanHashes = append(current.PriorPlanHashes, prior.PlanHash)
	plans[prior.PlanHash] = prior
	got, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil {
		t.Fatalf("authenticated predeployment ancestor was refused: %v", err)
	}
	if !finalHistoricalCoordinatorTimelinesEqual(got.evidence(), want.evidence()) {
		t.Fatal("predeployment ancestor changed the deployed proxy timeline")
	}
	artifact, err := finalHistoricalCoordinatorTimelineArtifactFromSource(want, baselines, logs)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	evidence.HistoricalCoordinatorTimeline = want.evidence()
	if err := verifyFinalHistoricalCoordinatorTimelineArtifact(evidence, current, plans, entries, data); err != nil {
		t.Fatalf("artifact reconstruction refused predeployment ancestor: %v", err)
	}
}
