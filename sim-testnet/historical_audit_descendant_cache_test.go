package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Use the production ninety-read ABI verifier, private cache storage and full
// archived-plan decoder. Only the chain is synthetic and locally served.
func newHistoricalAuditDescendantFixture(t *testing.T) fleetInstallHistoryCacheFixture {
	t.Helper()
	fixture := newFleetInstallHistoryCacheFixture(t)
	var err error
	fixture.action.IntentHash, err = actionIntentHash(fixture.action)
	if err != nil {
		t.Fatal(err)
	}
	cfg := fixture.executor.cfg
	plan := &SetupPlan{
		Schema: "urnetwork-sim-plan-v1", Release: "1.0", DeploymentID: cfg.Config.Deployment.DeploymentID,
		ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, Owner: cfg.WalletPublic,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, Actions: []Action{fixture.action}, Limits: configuredPlanLimits(cfg),
	}
	plan.ReleaseLockHash, err = canonicalHashHex(cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	plan.ResolvedInputsHash, err = resolvedInputsHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	fixture.executor.plan = plan
	historicalAuditDescendantArchive(t, fixture.executor)
	return fixture
}

func historicalAuditDescendantArchive(t *testing.T, executor *Executor) {
	t.Helper()
	wire, err := json.Marshal(executor.plan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(executor.stateDir, "plans", stringsTrim0x(executor.plan.PlanHash)+".json")
	if err := atomicWrite(path, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readValidatorEvidenceHistoricalPlan(executor.stateDir, executor.plan.PlanHash); err != nil {
		t.Fatalf("synthetic approval does not pass historical decoding: %v", err)
	}
}

func historicalAuditDescendantRelease(t *testing.T, executor *Executor, descendant bool) *Executor {
	t.Helper()
	resumed := *executor
	cfg := *executor.cfg
	release := *cfg.Release
	release.Repositories = maps.Clone(release.Repositories)
	release.Repositories["sn_go_source_hash"] = "sha256:" + strings.Repeat("a", 64)
	release.Repositories["sn_audited_base_commit"] = strings.Repeat("b", 40)
	cfg.Release = &release
	resumed.cfg = &cfg
	if descendant {
		plan := *executor.plan
		plan.PriorPlanHashes = append(append([]string{}, plan.PriorPlanHashes...), plan.PlanHash)
		var err error
		plan.ReleaseLockHash, err = canonicalHashHex(&release)
		if err != nil {
			t.Fatal(err)
		}
		plan.ResolvedInputsHash, err = resolvedInputsHash(&cfg)
		if err != nil {
			t.Fatal(err)
		}
		plan.PlanHash, err = plan.hash()
		if err != nil {
			t.Fatal(err)
		}
		resumed.plan = &plan
	}
	return &resumed
}

func historicalAuditDescendantProof(t *testing.T, executor *Executor) *historicalAuditCacheEntry {
	t.Helper()
	input := map[string]any{"action": executor.plan.Actions[0], "decoder_input": "synthetic-exact-input"}
	entry, hit := executor.lookupHistoricalAuditCache(t.Context(), "fleet-install-pinned-state-v1", input)
	if hit || entry == nil || entry.proof.CompatibilityHash == "" {
		t.Fatalf("cold eligible proof entry=%+v hit=%t", entry, hit)
	}
	entry.saveSuccess(t.Context())
	return entry
}

func TestHistoricalAuditDescendantRetainsNinetyReadsAcrossHotfix(t *testing.T) {
	fixture := newHistoricalAuditDescendantFixture(t)
	if err := fixture.verify(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.executor.oracle.client.TransactionReceipt(t.Context(), common.HexToHash(fixture.evidence.TransactionHash)); err == nil {
		t.Fatal("later receipt failure was not injected")
	}
	fixture.executor = historicalAuditDescendantRelease(t, fixture.executor, false)
	fixture.rpc.mu.Lock()
	fixture.rpc.badState = true
	fixture.rpc.mu.Unlock()
	if err := fixture.verify(t.Context()); err != nil {
		t.Fatalf("compatible source hotfix repeated immutable state: %v", err)
	}
	fixture.rpc.mu.Lock()
	defer fixture.rpc.mu.Unlock()
	if fixture.rpc.contractReads != 90 || fixture.rpc.finalizedReads != 2 || fixture.rpc.canonicalReads != 2 || fixture.rpc.receiptReads != 1 {
		t.Fatalf("contract/finalized/canonical/receipt reads=%d/%d/%d/%d", fixture.rpc.contractReads, fixture.rpc.finalizedReads, fixture.rpc.canonicalReads, fixture.rpc.receiptReads)
	}
}

func TestHistoricalAuditDescendantRetainsCompletedGroupsAcrossInterruption(t *testing.T) {
	fixture := newHistoricalAuditDescendantFixture(t)
	if err := fixture.verify(t.Context()); err != nil {
		t.Fatal(err)
	}
	second := fixture
	second.head = ChainHead{Number: 101, Hash: fleetHistoryBatchBlockHash(101)}
	if err := second.verify(t.Context()); err != nil {
		t.Fatal(err)
	}
	third := fixture
	third.head = ChainHead{Number: 102, Hash: fleetHistoryBatchBlockHash(102)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := third.verify(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("interruption=%v", err)
	}
	fixture.executor = historicalAuditDescendantRelease(t, fixture.executor, true)
	second.executor, third.executor = fixture.executor, fixture.executor
	fixture.rpc.mu.Lock()
	fixture.rpc.badState = true
	fixture.rpc.mu.Unlock()
	for _, completed := range []fleetInstallHistoryCacheFixture{fixture, second} {
		if err := completed.verify(t.Context()); err != nil {
			t.Fatalf("completed group %d repeated after descendant recovery: %v", completed.head.Number, err)
		}
	}
	if err := third.verify(t.Context()); err == nil {
		t.Fatal("uncompleted group inherited success")
	}
	fixture.rpc.mu.Lock()
	if fixture.rpc.contractReads != 270 {
		t.Errorf("contract reads=%d want180 completed +90 uncompleted", fixture.rpc.contractReads)
	}
	fixture.rpc.badState = false
	fixture.rpc.mu.Unlock()
	if err := third.verify(t.Context()); err != nil {
		t.Fatalf("repair of affected group failed: %v", err)
	}
}

func TestHistoricalAuditDescendantRetainsDualObserverPartialProgress(t *testing.T) {
	fixture := newHistoricalFleetCacheFixture(t, 3, true)
	approval := newHistoricalAuditDescendantFixture(t)
	plan := *approval.executor.plan
	plan.Actions = nil
	for index := range fixture.calls {
		call := &fixture.calls[index]
		call.action.Kind = "evm-transaction"
		call.action.Target = call.address.Hex()
		var err error
		call.action.IntentHash, err = actionIntentHash(call.action)
		if err != nil {
			t.Fatal(err)
		}
		call.entry.IntentHash = call.action.IntentHash
		plan.Actions = append(plan.Actions, call.action)
	}
	// The fixture endpoints differ, but all approval/configuration inputs are
	// stable across the hotfix itself.
	var err error
	plan.ConfigHash = fixture.executor.cfg.ConfigHash
	plan.ReleaseLockHash, err = canonicalHashHex(fixture.executor.cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	plan.ResolvedInputsHash, err = resolvedInputsHash(fixture.executor.cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	fixture.executor.plan = &plan
	for index := range fixture.calls {
		fixture.calls[index].entry.PlanHash = plan.PlanHash
	}
	historicalAuditDescendantArchive(t, fixture.executor)
	fixture.independent.failureBlock = fixture.calls[2].record.IndependentEVMFinalized.Number
	keys, err := fixture.executor.verifyHistoricalFleetGenerationOneCalls(t.Context(), fixture.calls)
	if err == nil || len(keys) != 2 {
		t.Fatalf("injected independent failure keys=%d err=%v", len(keys), err)
	}
	fixture.executor = historicalAuditDescendantRelease(t, fixture.executor, true)
	fixture.independent.failureBlock = 0
	keys, err = fixture.executor.verifyHistoricalFleetGenerationOneCalls(t.Context(), fixture.calls)
	if err != nil || len(keys) != 3 {
		t.Fatalf("descendant recovery keys=%d err=%v", len(keys), err)
	}
	for _, observer := range []*fleetHistoryCacheRPC{fixture.operational, fixture.independent} {
		if len(observer.contractSelectors) != 4 || observer.blockBatchRequests != 2 {
			t.Fatalf("recovery repeated completed state or skipped checkpoints: contract=%v checkpoint batches=%d", observer.contractSelectors, observer.blockBatchRequests)
		}
	}
	// A later decoder-evidence change must replay only that exact action.
	fixture.calls[0].expectation = map[string]any{"result": 2}
	fixture.operational.contractResult = "0x02"
	if _, err := fixture.executor.verifyHistoricalFleetGenerationOneCalls(t.Context(), fixture.calls); err == nil {
		t.Fatal("changed decoder evidence inherited the original proof")
	}
	if len(fixture.operational.contractSelectors) != 5 || len(fixture.independent.contractSelectors) != 5 {
		t.Fatal("changed decoder evidence did not replay exactly one dual-observer action")
	}
}

func TestHistoricalAuditDescendantFreshCheckpointFailuresRemainBlocking(t *testing.T) {
	for _, failure := range []string{"canonical", "finalized"} {
		fixture := newHistoricalAuditDescendantFixture(t)
		if err := fixture.verify(t.Context()); err != nil {
			t.Fatal(err)
		}
		fixture.executor = historicalAuditDescendantRelease(t, fixture.executor, true)
		fixture.rpc.mu.Lock()
		if failure == "canonical" {
			fixture.rpc.noncanonical = true
		} else {
			fixture.rpc.finalized = fixture.head.Number - 1
		}
		fixture.rpc.mu.Unlock()
		if err := fixture.verify(t.Context()); err == nil || !strings.Contains(err.Error(), "checkpoint") {
			t.Fatalf("%s: stale checkpoint accepted: %v", failure, err)
		}
		fixture.rpc.mu.Lock()
		if fixture.rpc.contractReads != 90 || fixture.rpc.finalizedReads != 2 || fixture.rpc.canonicalReads != 2 {
			t.Errorf("%s: freshness/state counts=%d/%d/%d", failure, fixture.rpc.finalizedReads, fixture.rpc.canonicalReads, fixture.rpc.contractReads)
		}
		fixture.rpc.mu.Unlock()
	}
}

func TestHistoricalAuditDescendantRechecksChangedExactInputs(t *testing.T) {
	for _, mutation := range []string{"action", "receipt", "checkpoint", "binding", "target"} {
		fixture := newHistoricalAuditDescendantFixture(t)
		if err := fixture.verify(t.Context()); err != nil {
			t.Fatal(err)
		}
		fixture.executor = historicalAuditDescendantRelease(t, fixture.executor, true)
		switch mutation {
		case "action":
			fixture.action.IntentHash = "0x" + strings.Repeat("f", 64)
		case "receipt":
			fixture.evidence.TransactionHash = "0x" + strings.Repeat("f", 64)
		case "checkpoint":
			fixture.head = ChainHead{Number: 101, Hash: fleetHistoryBatchBlockHash(101)}
		case "binding":
			fixture.snapshots[0].Members[0].Binding.Generation++
		case "target":
			fixture.executor.payloads.Manifest.CoordinatorProxy = common.HexToAddress("0x8888888888888888888888888888888888888888")
		}
		fixture.rpc.mu.Lock()
		fixture.rpc.badState = true
		fixture.rpc.mu.Unlock()
		if err := fixture.verify(t.Context()); err == nil {
			t.Fatalf("changed %s inherited old success", mutation)
		}
		fixture.rpc.mu.Lock()
		if fixture.rpc.contractReads != 180 {
			t.Errorf("%s: contract reads=%d want180", mutation, fixture.rpc.contractReads)
		}
		fixture.rpc.mu.Unlock()
	}
}

func TestHistoricalAuditDescendantRejectsChangedSecurityContext(t *testing.T) {
	for _, mutation := range []string{"runtime", "abi", "protocol", "dependency", "rpc", "owner", "policy", "config", "release-approval"} {
		fixture := newHistoricalAuditDescendantFixture(t)
		old := historicalAuditDescendantProof(t, fixture.executor)
		resumed := historicalAuditDescendantRelease(t, fixture.executor, true)
		switch mutation {
		case "runtime":
			resumed.cfg.Release.Runtime.SpecVersion++
		case "abi":
			resumed.cfg.Release.Interfaces = map[string]any{"synthetic_abi": "changed"}
		case "protocol":
			resumed.cfg.Release.Repositories["sn_protocol_source_hash"] = "changed"
		case "dependency":
			resumed.cfg.Release.Dependencies = map[string]string{"synthetic": "changed"}
		case "rpc":
			resumed.cfg.OperationalEVM = "https://different-observer.example"
		case "owner":
			resumed.cfg.WalletPublic += "changed"
		case "policy":
			resumed.cfg.PolicyHash = "0x" + strings.Repeat("e", 64)
		case "config":
			resumed.cfg.ConfigHash = "0x" + strings.Repeat("e", 64)
		case "release-approval":
			plan := *fixture.executor.plan
			plan.ReleaseLockHash = "0x" + strings.Repeat("e", 64)
			resumed.plan = &plan // Same claimed plan hash with a changed release.
		}
		input := map[string]any{"action": fixture.action, "decoder_input": "synthetic-exact-input"}
		entry, hit := resumed.lookupHistoricalAuditCache(t.Context(), old.proof.Kind, input)
		if entry == nil || hit {
			t.Fatalf("changed %s inherited cached success, hit=%t", mutation, hit)
		}
	}
}

func TestHistoricalAuditDescendantRejectsForeignOrTamperedApproval(t *testing.T) {
	for _, mutation := range []string{"foreign", "missing", "tampered", "wrong-release", "wrong-mac", "wrong-verifier"} {
		fixture := newHistoricalAuditDescendantFixture(t)
		old := historicalAuditDescendantProof(t, fixture.executor)
		resumed := historicalAuditDescendantRelease(t, fixture.executor, true)
		path := filepath.Join(fixture.executor.stateDir, "plans", stringsTrim0x(fixture.executor.plan.PlanHash)+".json")
		switch mutation {
		case "foreign":
			resumed.plan.PriorPlanHashes = nil
		case "missing":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		case "tampered":
			wire, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			wire = []byte(strings.Replace(string(wire), `"release":"1.0"`, `"release":"tampered"`, 1))
			if err := os.WriteFile(path, wire, 0o600); err != nil {
				t.Fatal(err)
			}
		case "wrong-release", "wrong-mac", "wrong-verifier":
			if err := os.Remove(historicalAuditCacheTestPath(old)); err != nil {
				t.Fatal(err)
			}
			if mutation == "wrong-release" {
				old.proof.ApprovalReleaseLockHash = "0x" + strings.Repeat("e", 64)
			} else if mutation == "wrong-verifier" {
				old.proof.VerifierVersion += "-different"
			} else {
				old.key[0] ^= 1
			}
			name, err := canonicalHashHex(old.proof)
			if err != nil {
				t.Fatal(err)
			}
			old.name = stringsTrim0x(name) + ".json"
			old.saveSuccess(t.Context())
		}
		input := map[string]any{"action": fixture.action, "decoder_input": "synthetic-exact-input"}
		if _, hit := resumed.lookupHistoricalAuditCache(t.Context(), old.proof.Kind, input); hit {
			t.Fatalf("%s approval proof inherited success", mutation)
		}
	}
}

func TestHistoricalAuditDescendantEnrichesOnlyExactLegacyContext(t *testing.T) {
	for _, compatibleContext := range []bool{false, true} {
		fixture := newHistoricalAuditDescendantFixture(t)
		old := historicalAuditDescendantProof(t, fixture.executor)
		if err := os.Remove(historicalAuditCacheTestPath(old)); err != nil {
			t.Fatal(err)
		}
		old.proof.ApprovalPlanHash, old.proof.ApprovalReleaseLockHash, old.proof.CompatibilityHash = "", "", ""
		name, err := canonicalHashHex(old.proof)
		if err != nil {
			t.Fatal(err)
		}
		old.name = stringsTrim0x(name) + ".json"
		old.saveSuccess(t.Context())
		resumed := fixture.executor
		if compatibleContext {
			resumed = historicalAuditDescendantRelease(t, resumed, true)
		}
		input := map[string]any{"action": fixture.action, "decoder_input": "synthetic-exact-input"}
		entry, hit := resumed.lookupHistoricalAuditCache(t.Context(), old.proof.Kind, input)
		if hit == compatibleContext {
			t.Fatalf("legacy cross-context=%t hit=%t", compatibleContext, hit)
		}
		if !compatibleContext && (entry.proof.CompatibilityHash == "" || !entry.readSuccess()) {
			t.Fatal("exact legacy proof was not authenticated and enriched")
		}
	}
}

func TestHistoricalAuditDescendantReadOnlyDoesNotPromote(t *testing.T) {
	fixture := newHistoricalAuditDescendantFixture(t)
	old := historicalAuditDescendantProof(t, fixture.executor)
	resumed := historicalAuditDescendantRelease(t, fixture.executor, true)
	resumed.cfg.readOnlyAudit = true
	input := map[string]any{"action": fixture.action, "decoder_input": "synthetic-exact-input"}
	entry, hit := resumed.lookupHistoricalAuditCache(t.Context(), old.proof.Kind, input)
	if !hit || entry == nil || entry.readSuccess() {
		t.Fatalf("read-only reuse hit=%t entry=%+v", hit, entry)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, hit := resumed.lookupHistoricalAuditCache(ctx, old.proof.Kind, input); hit {
		t.Fatal("canceled recovery inherited success")
	}
}

func TestHistoricalAuditDescendantDoesNotCacheDynamicOrUnapprovedActions(t *testing.T) {
	for _, mutation := range []string{"dynamic", "unapproved", "duplicate"} {
		fixture := newHistoricalAuditDescendantFixture(t)
		kind := "fleet-install-pinned-state-v1"
		action := fixture.action
		switch mutation {
		case "dynamic":
			kind = "current-postcondition"
		case "unapproved":
			action.IntentHash = "0x" + strings.Repeat("e", 64)
		case "duplicate":
			fixture.executor.plan.Actions = append(fixture.executor.plan.Actions, action)
		}
		entry, _ := fixture.executor.lookupHistoricalAuditCache(t.Context(), kind, map[string]any{"action": action})
		if entry == nil || entry.proof.CompatibilityHash != "" {
			t.Fatalf("%s action received descendant capability: %+v", mutation, entry)
		}
	}
}

func TestHistoricalAuditPlanReaderRejectsCanceledOrDifferentSource(t *testing.T) {
	for _, failure := range []string{"cancel", "identity", "error"} {
		fixture := newHistoricalAuditDescendantFixture(t)
		ctx, cancel := context.WithCancel(t.Context())
		reads := 0
		reader := &historicalAuditPlanReader{plans: map[string]*SetupPlan{}, readSource: func(string, string) (*SetupPlan, error) {
			reads++
			switch failure {
			case "cancel":
				cancel()
				return fixture.executor.plan, nil
			case "identity":
				return &SetupPlan{PlanHash: "different"}, nil
			default:
				return nil, fmt.Errorf("synthetic decoder failure")
			}
		}}
		if _, err := reader.read(ctx, fixture.executor.stateDir, fixture.executor.plan.PlanHash); err == nil {
			t.Fatalf("%s decoder result accepted", failure)
		}
		cancel()
		if reads != 1 || len(reader.plans) != 0 {
			t.Fatalf("%s decoder retained a failed source", failure)
		}
	}
}

func TestHistoricalAuditPlanReaderSharesOnlyOneInvocation(t *testing.T) {
	fixture := newHistoricalAuditDescendantFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var reads atomic.Int64
	reader := &historicalAuditPlanReader{readSource: func(string, string) (*SetupPlan, error) {
		if reads.Add(1) == 1 {
			close(entered)
			<-release
		}
		return fixture.executor.plan, nil
	}}
	results := make(chan error, 16)
	go func() {
		_, err := reader.read(t.Context(), fixture.executor.stateDir, fixture.executor.plan.PlanHash)
		results <- err
	}()
	<-entered
	for index := 1; index < cap(results); index++ {
		go func() {
			_, err := reader.read(t.Context(), fixture.executor.stateDir, fixture.executor.plan.PlanHash)
			results <- err
		}()
	}
	close(release)
	for index := 0; index < cap(results); index++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if reads.Load() != 1 {
		t.Fatalf("concurrent source decodes=%d want1", reads.Load())
	}
	// A new reconciliation cannot reuse this process-local authority; it must
	// decode the archival source again even with an identical requested hash.
	reopened := &historicalAuditPlanReader{readSource: reader.readSource}
	if _, err := reopened.read(t.Context(), fixture.executor.stateDir, fixture.executor.plan.PlanHash); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 2 {
		t.Fatalf("new invocation source decodes=%d want2", reads.Load())
	}
}
