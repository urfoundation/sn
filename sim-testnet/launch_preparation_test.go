package main

// Exercise the actual retained receipt readers and final action gate with
// synthetic approvals. No live node, signer or process supervisor is needed.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

func launchPreparationTestExecutor(t *testing.T) *Executor {
	t.Helper()
	stateDir := t.TempDir()
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { journal.Close() })
	cfg := testResolvedConfig(t)
	cfg.OperationalRPCMode = rpcModePublicOverride
	return &Executor{cfg: cfg, stateDir: stateDir, journal: journal, roles: &RoleSecrets{}, plan: &SetupPlan{PlanHash: "0x" + strings.Repeat("41", 32), PriorPlanHashes: []string{"0x" + strings.Repeat("42", 32)}}}
}

func appendLaunchPreparationTestReceipt(t *testing.T, executor *Executor, action Action) JournalEntry {
	t.Helper()
	executor.plan.Actions = append(executor.plan.Actions, action)
	head := ChainHead{Number: 10, Hash: fleetHistoryBatchBlockHash(10)}
	record := &ActionPostcondition{Schema: "urnetwork-sim-action-postcondition-v4", DeploymentID: executor.cfg.Config.Deployment.DeploymentID,
		PlanHash: executor.plan.PriorPlanHashes[0], ActionID: action.ID, IntentHash: action.IntentHash,
		OperationalRPCMode: executor.cfg.OperationalRPCMode, IndependentRPC: independentRPCRequired(executor.cfg),
		SubstrateFinalized: head, EVMFinalized: head, EVMHashDomain: "evm-rpc", Observed: map[string]any{"retained": true},
		IndependentSubstrateFinalized: head, IndependentEVMFinalized: head, IndependentEVMHashDomain: "evm-rpc", IndependentObserved: map[string]any{"retained": true}}
	path, hash, err := executor.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{DeploymentID: record.DeploymentID, PlanHash: record.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionPath: path, PostconditionHash: hash}
	if err := executor.journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

// A missing early receipt used to suppress every later local and chain check.
// Valid local proofs beyond two failed batches must remain exactly reusable.
func TestCarriedPreparationCollectsFailuresAndKeepsOnlyVerifiedKeys(t *testing.T) {
	executor := launchPreparationTestExecutor(t)
	entries := make([]JournalEntry, 18)
	for index := range entries {
		action := Action{ID: fmt.Sprintf("preparation.reserve.%02d", index), Kind: "budget-reserve", IntentHash: fmt.Sprintf("intent-%02d", index)}
		if index == 3 {
			action.ID, action.Kind = "accounts.provision", "local"
		}
		if index == 16 {
			action.ID, action.Kind = "subnet.verify-owner", "substrate-read"
		}
		entries[index] = appendLaunchPreparationTestReceipt(t, executor, action)
	}
	for _, index := range []int{0, 8} {
		if err := os.Remove(filepath.Join(executor.stateDir, entries[index].PostconditionPath)); err != nil {
			t.Fatal(err)
		}
	}
	executor.carriedVerificationKeys = map[string]bool{"stale-authority": true, carriedVerificationKey(entries[0]): true}
	before, err := json.Marshal(executor.journal.Entries())
	if err != nil {
		t.Fatal(err)
	}
	err = executor.verifyCarriedActionHistory(t.Context())
	if err == nil {
		t.Fatal("independent preparation failures were discarded")
	}
	previous := -1
	for _, index := range []int{0, 3, 8, 16} {
		position := strings.Index(err.Error(), "action "+entries[index].ActionID+":")
		if position <= previous {
			t.Fatalf("action %s missing or out of canonical order: %v", entries[index].ActionID, err)
		}
		previous = position
	}
	if !strings.Contains(err.Error(), "blocked by native reader") || !strings.Contains(err.Error(), "client id is not provisioned") {
		t.Fatalf("available failure and dependency refusal were conflated: %v", err)
	}
	for index, entry := range entries {
		want := index != 0 && index != 3 && index != 8 && index != 16
		if got := executor.carriedVerificationKeys[carriedVerificationKey(entry)]; got != want {
			t.Fatalf("action %s cached=%t want=%t", entry.ActionID, got, want)
		}
	}
	if executor.carriedVerificationKeys["stale-authority"] {
		t.Fatal("another invocation's verification authority survived")
	}
	if err := executor.Execute(t.Context(), executor.plan.Actions[17]); err != nil {
		t.Fatalf("exact authenticated immutable success was not reusable: %v", err)
	}
	after, err := json.Marshal(executor.journal.Entries())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("read-only preparation changed journal custody: %v", err)
	}
}

func TestCarriedPreparationCancellationNamesEveryBlockedAction(t *testing.T) {
	executor := launchPreparationTestExecutor(t)
	for index := 0; index < 4; index++ {
		appendLaunchPreparationTestReceipt(t, executor, Action{ID: fmt.Sprintf("preparation.reserve.%d", index), Kind: "budget-reserve", IntentHash: "original"})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := executor.verifyCarriedActionHistory(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled preparation lost its cause: %v", err)
	}
	for _, action := range executor.plan.Actions {
		if !strings.Contains(err.Error(), "action "+action.ID+": blocked") {
			t.Fatalf("cancellation omitted exact action %s: %v", action.ID, err)
		}
	}
	if len(executor.carriedVerificationKeys) != 0 {
		t.Fatal("unstarted actions became verified")
	}
}

// Real partial owners must turn unavailable services into blocked checks while
// a later independent immutable receipt still passes, without a nil panic.
func TestCarriedPreparationPartialReadersKeepIndependentLocalProofs(t *testing.T) {
	fixture := &fleetHistoryBatchRPC{t: t, finalizedBlock: 100}
	server := httptest.NewServer(fixture)
	defer server.Close()
	raw, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	manager := &EvmTxManager{client: ethclient.NewClient(raw)}
	for _, name := range []string{"native", "deployer", "owner", "oracle", "keeper", "independent-evm", "independent-native"} {
		executor := launchPreparationTestExecutor(t)
		executor.preparationIncomplete = true
		action := Action{ID: "preparation.evm", Kind: "evm-call", IntentHash: "original"}
		want := "carried EVM checkpoint"
		if name == "native" || name == "independent-native" {
			action.ID, action.Kind, want = "subnet.verify-owner", "substrate-read", "native reader"
		}
		if name == "owner" {
			executor.deployer = manager
			want = "owner EVM reader"
		}
		if name == "oracle" || name == "keeper" {
			executor.deployer, executor.owner = manager, manager
			if name == "oracle" {
				action.ID, want = "fleet.mirror.1", "commitment-oracle EVM reader"
			} else {
				action.ID, want = "fleet.bind.1.1", "keeper EVM reader"
			}
		}
		if strings.HasPrefix(name, "independent-") {
			executor.cfg.OperationalRPCMode = rpcModePrivateAuthority
			executor.deployer, executor.owner = manager, manager
			if name == "independent-evm" {
				want = "independent EVM reader"
			} else {
				want = "independent native reader"
			}
		}
		bad := appendLaunchPreparationTestReceipt(t, executor, action)
		good := appendLaunchPreparationTestReceipt(t, executor, Action{ID: "preparation.reserve", Kind: "budget-reserve", IntentHash: "original-local"})
		err := executor.verifyCarriedActionHistory(t.Context())
		if err == nil || !strings.Contains(err.Error(), "action "+action.ID+": blocked by "+want) {
			t.Fatalf("partial %s reader did not identify its blocked action: %v", name, err)
		}
		if executor.carriedVerificationKeys[carriedVerificationKey(bad)] || !executor.carriedVerificationKeys[carriedVerificationKey(good)] {
			t.Fatalf("partial %s reader published failed authority or lost independent success", name)
		}
	}
}

// A real approved deployment can reopen a saved manifest for event-boundary
// reconciliation or registration promotion. Missing readers must stop that
// payload path while a separate retained local action remains verifiable.
func TestCarriedPreparationDeploymentEnvelopeBlocksMissingPayloadReader(t *testing.T) {
	executor, reader := deploymentBoundaryTestExecution(t)
	if !planUsesContractDeploymentEnvelope(executor.plan.Schema) || executor.payloads != nil {
		t.Fatal("fixture does not reach approved payload reconstruction")
	}
	executor.cfg.OperationalRPCMode = rpcModePublicOverride
	executor.preparationIncomplete = true
	executor.deployer = nil
	executor.plan.PriorPlanHashes = []string{"0x" + strings.Repeat("42", 32)}
	action, err := exactPlanActionByID(executor.plan, "evm.coordinator-proxy")
	if err != nil {
		t.Fatal(err)
	}
	executor.plan.Actions = nil
	blocked := appendLaunchPreparationTestReceipt(t, executor, action)
	good := appendLaunchPreparationTestReceipt(t, executor, Action{ID: "preparation.reserve", Kind: "budget-reserve", IntentHash: "independent-local"})
	before := validatorNamespaceTreeSnapshot(t, executor.stateDir)
	if err := executor.ensurePayloads(t.Context()); err == nil || !strings.Contains(err.Error(), "blocked by deployer EVM reader") {
		t.Fatalf("approved envelope dereferenced or bypassed missing payload reader: %v", err)
	}
	err = executor.verifyCarriedActionHistory(t.Context())
	if err == nil || !strings.Contains(err.Error(), "prepare carried contract payloads: blocked by deployer EVM reader") || !strings.Contains(err.Error(), "action "+action.ID+": blocked by carried contract payloads") {
		t.Fatalf("payload dependency hid carried action diagnostics: %v", err)
	}
	if executor.payloads != nil || executor.carriedVerificationKeys[carriedVerificationKey(blocked)] || !executor.carriedVerificationKeys[carriedVerificationKey(good)] {
		t.Fatal("failed payload reconstruction acquired authority or lost independent success")
	}
	if reader.unexpectedRequests.Load() != 0 || !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, executor.stateDir)) {
		t.Fatal("blocked payload preparation made an RPC request or changed retained state")
	}
}

func TestProvisionalPreparationCollectsEveryRetainedReceiptFailure(t *testing.T) {
	executor, _, first := provisionalVerifiedExecutor(t, "evidence.activate.first", true)
	last := appendLaunchPreparationTestReceipt(t, executor, Action{ID: "evidence.activate.last", IntentHash: "second-original"})
	for _, entry := range []JournalEntry{first, last} {
		if err := os.WriteFile(filepath.Join(executor.stateDir, entry.PostconditionPath), []byte("changed"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := json.Marshal(executor.journal.Entries())
	if err != nil {
		t.Fatal(err)
	}
	err = executor.verifyCarriedActionHistory(t.Context())
	if err == nil || !strings.Contains(err.Error(), first.ActionID) || !strings.Contains(err.Error(), last.ActionID) {
		t.Fatalf("provisional preparation stopped at first receipt: %v", err)
	}
	after, marshalErr := json.Marshal(executor.journal.Entries())
	if marshalErr != nil || !bytes.Equal(before, after) {
		t.Fatal("provisional diagnostic rewrote retained custody")
	}
}

func TestCarriedPreparationReadOnlyBatchesRetainAllFailures(t *testing.T) {
	var active, maximum atomic.Int64
	started := make(chan int, 3)
	release := make(chan struct{})
	done := make(chan []error, 1)
	go func() {
		done <- collectOrderedReadOnlyAudits(t.Context(), 9, 3, func(index int) error {
			current := active.Add(1)
			for observed := maximum.Load(); current > observed && !maximum.CompareAndSwap(observed, current); observed = maximum.Load() {
			}
			defer active.Add(-1)
			if index < 3 {
				started <- index
				<-release
			}
			return fmt.Errorf("independent-%d", index)
		})
	}()
	for index := 0; index < 3; index++ {
		<-started
	}
	close(release)
	errs := <-done
	if maximum.Load() > 3 || len(errs) != 9 {
		t.Fatalf("unbounded or incomplete read-only batches: max=%d results=%d", maximum.Load(), len(errs))
	}
	for index, err := range errs {
		if err == nil || err.Error() != fmt.Sprintf("independent-%d", index) {
			t.Fatalf("canonical independent failure %d was discarded: %v", index, err)
		}
	}
}

func TestLaunchPreparationGateRetainsDoctorDetailAndNeverSubmitsOnFailure(t *testing.T) {
	for _, prepareOnly := range []bool{false, true} {
		report := &launchPreparationReport{Ready: true, PrepareOnly: prepareOnly, Doctor: &DoctorReport{Ready: false, Checks: []Check{{Name: "tool/example", Hard: true, Detail: "synthetic tool unavailable"}}}}
		report.add("doctor", report.Doctor.Error())
		report.add("release-host", errors.New("synthetic host failure"))
		report.add("carried-history", errors.New("action original: invalid retained receipt"))
		report.blocked("packet-listeners", "release-binaries")
		published, submitted := 0, 0
		err := finishLaunchPreparation(report, func(got *launchPreparationReport, err error) error {
			published++
			wire, marshalErr := json.Marshal(got)
			if marshalErr != nil || !bytes.Contains(wire, []byte("synthetic tool unavailable")) || !bytes.Contains(wire, []byte("invalid retained receipt")) || !got.StoppedBeforeActions {
				t.Fatalf("full preparation report lost detail: %s %v", wire, marshalErr)
			}
			return err
		}, func() error { submitted++; return nil })
		if err == nil || submitted != 0 || published != 1 {
			t.Fatalf("failed preparation crossed action boundary: prepare_only=%t submitted=%d published=%d error=%v", prepareOnly, submitted, published, err)
		}
	}
}

func TestLaunchPreparationOnlyStopsCleanApprovedPass(t *testing.T) {
	for _, prepareOnly := range []bool{false, true} {
		report := &launchPreparationReport{Ready: true, PrepareOnly: prepareOnly}
		published, submitted := 0, 0
		err := finishLaunchPreparation(report, func(got *launchPreparationReport, err error) error {
			published++
			if !got.StoppedBeforeActions || err != nil {
				t.Fatalf("clean preparation stop differs: %+v %v", got, err)
			}
			return nil
		}, func() error { submitted++; return nil })
		if err != nil || prepareOnly && (published != 1 || submitted != 0) || !prepareOnly && (published != 0 || submitted != 1) {
			t.Fatalf("clean action gate prepare_only=%t submitted=%d published=%d error=%v", prepareOnly, submitted, published, err)
		}
	}
}

func TestLaunchPreparationOnlyRequiresExistingCommandAndExactApproval(t *testing.T) {
	hash := "0x" + strings.Repeat("13", 32)
	for _, command := range []string{"setup", "launch", "resume"} {
		if _, options, err := parseCLI([]string{command, "--prepare-only", "--apply", "--plan-hash", hash}); err != nil || !options.PrepareOnly {
			t.Fatalf("approved %s preparation: %v", command, err)
		}
	}
	for _, args := range [][]string{{"launch", "--prepare-only"}, {"launch", "--prepare-only", "--apply", "--plan-hash", "wrong"}, {"doctor", "--prepare-only", "--apply", "--plan-hash", hash}} {
		if _, _, err := parseCLI(args); err == nil {
			t.Fatalf("preparation bypassed command/approval authority: %v", args)
		}
	}
}

// The actual fleet RPC batch must retain a later independent proof even if an
// earlier canonical block and a different action's decoder fail independently.
func TestHistoricalFleetPreparationCollectsCheckpointAndLaterObservationFailures(t *testing.T) {
	fixture := &fleetHistoryBatchRPC{t: t, finalizedBlock: 1000, tamperedBlock: 10}
	server := httptest.NewServer(fixture)
	defer server.Close()
	raw, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	executor := launchPreparationTestExecutor(t)
	executor.deployer = &EvmTxManager{client: ethclient.NewClient(raw)}
	calls := make([]historicalFleetGenerationOneCall, maximumEVMRPCBatchCalls+3)
	for index := range calls {
		block := uint64(index + 10)
		action := Action{ID: fmt.Sprintf("fleet.synthetic.%03d", index), IntentHash: fmt.Sprintf("original-%d", index)}
		head := ChainHead{Number: block, Hash: fleetHistoryBatchBlockHash(block)}
		fails := index == 1 || index == len(calls)-1
		calls[index] = historicalFleetGenerationOneCall{action: action, entry: JournalEntry{ActionID: action.ID, IntentHash: action.IntentHash}, address: common.HexToAddress("0x1234"), data: []byte{1}, expectation: map[string]any{"original": index},
			record: &ActionPostcondition{EVMFinalized: head, IndependentEVMFinalized: head, Observed: map[string]any{"ok": true}, IndependentObserved: map[string]any{"ok": true}},
			observe: func([]byte) (map[string]any, error) {
				if fails {
					return nil, errors.New("synthetic observation differs")
				}
				return map[string]any{"ok": true}, nil
			},
		}
	}
	keys, err := executor.verifyHistoricalFleetGenerationOneCalls(t.Context(), calls)
	if err == nil || !strings.Contains(err.Error(), calls[0].action.ID) || !strings.Contains(err.Error(), calls[1].action.ID) || !strings.Contains(err.Error(), calls[len(calls)-1].action.ID) {
		t.Fatalf("fleet preparation hid independent failures: %v", err)
	}
	for index, call := range calls {
		want := index != 0 && index != 1 && index != len(calls)-1
		if keys[carriedVerificationKey(call.entry)] != want {
			t.Fatalf("fleet action %s inherited failed authority or lost success", call.action.ID)
		}
	}
	if fixture.contractBatchRequests < 2 || len(fixture.contractSelectors) != len(calls)-1 {
		t.Fatalf("fleet preparation stopped an independent later batch: batches=%d calls=%d", fixture.contractBatchRequests, len(fixture.contractSelectors))
	}
}
