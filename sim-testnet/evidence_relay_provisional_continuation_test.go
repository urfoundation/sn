//go:build linux || darwin

package main

// The real phase reader owns synthetic signed activations, persisted plan
// ancestry and metadata-qualified Rpc. It never starts a relay or spends.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Empty publication history is real absence. Capacity is a captured syntax
// fixture; startup ledger verification is not replaced or claimed here.
func provisionalRelayContinuationRuntimeTest(t *testing.T) (*runtimeEvidenceActivationRpcV2TestFixture, *evidenceRelayRuntime) {
	t.Helper()
	fixture := newRuntimeEvidenceActivationConfiguredRpcV2TestFixture(t, func(cfg *ResolvedConfig) {
		cfg.Config.ValidatorEvidenceRelay.MaxSlots = 256
	})
	executor := fixture.executor
	journal, err := OpenJournal(executor.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { journal.Close() })
	executor.journal = journal
	prepared, preparedBytes, err := executor.prepareRuntimeEvidenceActivationsV2(t.Context(), fixture.chain)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(executor.cfg, executor.stateDir, executor.plan, executor.roles); err != nil {
		t.Fatal(err)
	}
	action := actionByID(t, executor.plan, "config.render")
	if err := journal.Append(JournalEntry{DeploymentID: executor.plan.DeploymentID, PlanHash: executor.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	work, err := evidenceRelayConfiguredWork(executor.cfg)
	if err != nil {
		t.Fatal(err)
	}
	required, err := work.remaining("release-1.0", false)
	if err != nil {
		t.Fatal(err)
	}
	reserve, err := exactPlanActionByID(executor.plan, evidenceRelayReserveId)
	if err != nil {
		t.Fatal(err)
	}
	span := required + 720
	c := EvidenceRelayContinuation{
		Schema: evidenceRelayContinuationSchema, SourcePlanHash: executor.plan.PlanHash, ActivationPlanHash: executor.plan.PlanHash,
		ConfigHash: executor.plan.ConfigHash, PreparedSHA256: bytesSHA256(preparedBytes), CompletedSHA256: bytesSHA256([]byte("synthetic completed activation")),
		TransactionsSHA256: bytesSHA256([]byte("[]")), JournalHash: journal.Entries()[0].EntryHash, OriginalReserve: reserve,
		EVMHead: ChainHead{Number: fixture.evmNumber, Hash: fixture.evmHash.Hex()}, NativeHead: ChainHead{Number: fixture.nativeNumber, Hash: fixture.nativeHash.Hex()},
		SettlementEpoch: prepared.Epoch, NativeEpoch: 77, EndBlock: fixture.evmNumber + span, RequiredWorkBlocks: required,
	}
	closed, err := evidenceRelayContinuationCeil(span, work.settlementCadence)
	if err != nil {
		t.Fatal(err)
	}
	native, err := evidenceRelayContinuationCeil(span, work.nativeCadence)
	if err != nil {
		t.Fatal(err)
	}
	c.EndSettlementEpoch, c.EndNativeEpoch = c.SettlementEpoch+closed, c.NativeEpoch+native
	runtime := &evidenceRelayRuntime{ctx: t.Context(), executor: executor, chain: fixture.chain, phase: "release-1.0",
		origins: [2]string{executor.cfg.OperatorAPIOrigins[0], executor.cfg.OperatorAPIOrigins[1]}}
	for _, configured := range executor.cfg.Config.ValidatorEvidenceV2 {
		source := evidenceRelaySource{validatorId: configured.ValidatorID, bounds: configured.Evidence.Bounds,
			stateDir: filepath.Join(executor.stateDir, "runtime", fmt.Sprintf("validator-%d", configured.ValidatorID), "coordinator-state-v2")}
		for _, member := range prepared.Members {
			if member.ValidatorId != configured.ValidatorID {
				continue
			}
			source.activations = append(source.activations, member.Activation)
			identity := validatorcomponent.AttemptLedgerIdentity{DeploymentID: executor.plan.DeploymentID, ChainID: executor.plan.ChainID, GenesisHash: executor.plan.GenesisHash,
				Netuid: executor.plan.Netuid, ValidatorID: member.ValidatorId, ValidatorUID: member.ValidatorUid, NoID: member.NoId, ValidatorVPK: fmt.Sprintf("0x%x", member.Activation.VPK)}
			c.Sources = append(c.Sources, EvidenceRelayContinuationSource{ValidatorID: member.ValidatorId, NoID: member.NoId, CoordinatorStateDir: source.stateDir,
				IntentPrefixSHA256: bytesSHA256(nil), Activation: member.Activation,
				Capacity: validatorcomponent.StoppedAttemptLedgerCapacity{Identity: identity, Coordinator: executor.plan.Deployment.CoordinatorProxy.Hex(), Head: validatorcomponent.AttemptLedgerHead{Root: common.Hash{}.Hex()}}})
		}
		runtime.sources = append(runtime.sources, source)
	}
	c.NewSlots, c.HistoricalLiabilityWei, err = c.remainingSlots()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := appendEvidenceRelayContinuationPlan(executor.plan, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(executor.cfg, executor.stateDir, plan, executor.roles); err != nil {
		t.Fatal(err)
	}
	executor.plan = plan
	executor.cfg.provisionalResume = &provisionalResumeState{RecordPath: filepath.Join(t.TempDir(), "provenance.json"), Record: &provisionalResumeRecord{Schema: "urnetwork-sim-provisional-resume-v1", PlanHash: plan.PlanHash, ConfigHash: plan.ConfigHash, DeploymentID: plan.DeploymentID, Provisional: true, FinalAcceptance: false}}
	fixture.stateLock.Lock()
	fixture.finalizedEvmNumber, fixture.finalizedEvmHash = 210, common.Hash{0x33}
	fixture.stateLock.Unlock()
	return fixture, runtime
}

// The full phase reader retains strict approval geometry, real native permit
// reads and fixed ends while admitting only the next provisional work block.
func TestProvisionalRelayContinuationPreparesActualHorizonWithoutReapproval(t *testing.T) {
	fixture, runtime := provisionalRelayContinuationRuntimeTest(t)
	before := validatorNamespaceTreeSnapshot(t, runtime.executor.stateDir)
	approval, err := json.Marshal(runtime.executor.plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.prepareHorizon(); err != nil {
		t.Fatalf("approved provisional continuation could not enter actual phase preparation: %v", err)
	}
	c := runtime.executor.plan.EvidenceRelayContinuation
	if runtime.horizon == nil || runtime.horizon.continuation != c || runtime.horizon.minimumEnd != 211 || runtime.horizon.minimumNativeEnd != 78 || runtime.nativeWarmupBudget != nil || !provisionalResumeEnabled(runtime.executor.cfg) || runtime.work.releaseWarmup == 0 || runtime.work.productionWarmup == 0 {
		t.Fatal("provisional execution changed approval clocks, credited epochs or installed strict readiness")
	}
	if err := c.validateClocks(runtime.work); err != nil {
		t.Fatal(err)
	}
	end, epoch, native, err := runtime.horizon.ceilings(nil)
	if err != nil || end != c.EndBlock || epoch != c.EndSettlementEpoch || native != c.EndNativeEpoch || runtime.horizon.maximum != c.NewSlots {
		t.Fatal("provisional phase changed fixed funded ceilings", err)
	}
	fixture.stateLock.Lock()
	fixture.permits[1] = false
	fixture.stateLock.Unlock()
	refused := *runtime
	refused.horizon = nil
	if err := refused.prepareHorizon(); err == nil || !strings.Contains(err.Error(), "independent finalized eligibility/schedule") || refused.horizon != nil {
		t.Fatal("provisional continuation waived actual native signing eligibility", err)
	}
	after, err := json.Marshal(runtime.executor.plan)
	if err != nil || string(after) != string(approval) || !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, runtime.executor.stateDir)) {
		t.Fatal("phase preparation changed approved source, money or history", err)
	}
}

// A continuation requires actual pending-file discovery even in provisional
// mode. Corrupt new source bytes cannot hide behind the ordinary preview waiver.
func TestProvisionalRelayContinuationRejectsCorruptPendingCensus(t *testing.T) {
	_, runtime := provisionalRelayContinuationRuntimeTest(t)
	path, err := validatorcomponent.ValidatorEvidencePublicationV2ManifestPath(runtime.sources[0].stateDir, runtime.sources[0].activations[0].Domain.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := validatorNamespaceTreeSnapshot(t, runtime.executor.stateDir)
	if err := runtime.prepareHorizon(); err == nil || !strings.Contains(err.Error(), "invalid character") || runtime.horizon != nil {
		t.Fatal("provisional continuation omitted its actual pending census reader", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, runtime.executor.stateDir)) {
		t.Fatal("refused census mutated retained sources or admissions")
	}
}

// The stored end is a capture-time workload forecast for an exact provisional
// invocation. Boundary and restarted workers keep all actual admission limits.
func TestProvisionalRelayContinuationForecastEndIsAdvisoryAcrossRestart(t *testing.T) {
	fixture, runtime := provisionalRelayContinuationRuntimeTest(t)
	c := runtime.executor.plan.EvidenceRelayContinuation
	beforePlan, err := json.Marshal(runtime.executor.plan)
	if err != nil {
		t.Fatal(err)
	}
	beforeSources := validatorNamespaceTreeSnapshot(t, runtime.executor.stateDir)
	cutoff := c.EndBlock - c.RequiredWorkBlocks
	for _, block := range []uint64{cutoff + 1, c.EndBlock, c.EndBlock + 1} {
		if err := validateEvidenceRelayContinuationRunway(c, block); err == nil {
			t.Fatalf("strict continuation admitted elapsed forecast at %d", block)
		}
		if err := validateEvidenceRelayContinuationStartupRunway(runtime.executor.cfg, runtime.executor.plan, c, block); err != nil {
			t.Fatalf("exact provisional continuation refused elapsed forecast at %d: %v", block, err)
		}
		span, err := evidenceRelayContinuationCapacitySpan(c, block)
		if err != nil || span == 0 || block >= c.EndBlock && span != 1 {
			t.Fatalf("elapsed continuation produced unsafe capacity span at %d: span=%d error=%v", block, span, err)
		}
	}
	strict := *runtime.executor.cfg
	strict.provisionalResume = nil
	if err := validateEvidenceRelayContinuationStartupRunway(&strict, runtime.executor.plan, c, c.EndBlock); err == nil {
		t.Fatal("strict continuation treated its elapsed forecast as advisory")
	}
	invalidRecord := *runtime.executor.cfg.provisionalResume.Record
	invalidRecord.FinalAcceptance = true
	invalidState := *runtime.executor.cfg.provisionalResume
	invalidState.Record = &invalidRecord
	invalid := *runtime.executor.cfg
	invalid.provisionalResume = &invalidState
	if err := validateEvidenceRelayContinuationStartupRunway(&invalid, runtime.executor.plan, c, c.EndBlock); err == nil || !strings.Contains(err.Error(), "exact non-accepting testnet approval") {
		t.Fatal("accepting provisional record waived the elapsed forecast", err)
	}
	for _, boundary := range []struct {
		block uint64
		hash  common.Hash
	}{
		{block: c.EndBlock, hash: common.Hash{0x34}},
		{block: c.EndBlock + 1, hash: common.Hash{0x35}},
	} {
		block := boundary.block
		fixture.stateLock.Lock()
		fixture.finalizedEvmNumber, fixture.finalizedEvmHash = block, boundary.hash
		fixture.stateLock.Unlock()
		restarted := *runtime
		restarted.horizon = nil
		restarted.prepared = false
		if err := restarted.prepareHorizon(); err != nil {
			t.Fatalf("provisional continuation restart refused forecast boundary %d: %v", block, err)
		}
		if restarted.horizon == nil || !restarted.horizon.forecastAdvisory || restarted.horizon.continuation != c || restarted.horizon.maximum != uint64(len(c.Debits))+c.NewSlots || restarted.horizon.minimumEnd != block+1 {
			t.Fatalf("restart at %d changed continuation ownership or exact slot count", block)
		}
		if err := restarted.checkHorizonBlock(block + 1); err != nil {
			t.Fatalf("worker poll after forecast boundary %d failed: %v", block, err)
		}
		candidate := evidenceRelayLaunchRequestTest(t, fixture.base, fixture.base.prepared.Members[0], c.EndSettlementEpoch+1, false, 0)
		if candidate.Evidence.Header.BoundaryBlock <= c.EndBlock {
			t.Fatal("post-forecast regression candidate did not cross the stored end")
		}
		reserve, err := exactPlanActionByID(runtime.executor.plan, evidenceRelayReserveId)
		if err != nil {
			t.Fatal(err)
		}
		action, err := buildEvidenceRelayAction(runtime.executor.plan, reserve, candidate)
		gas, fee, slots, allowanceErr := evidenceRelayPlanAllowance(runtime.executor.plan, reserve)
		if err != nil || allowanceErr != nil || gas != evidenceRelayContinuationGas || fee != evidenceRelayContinuationFee || slots != evidenceRelayContinuationSlots || action.Spend.EVMGasWei != multiplyUint64Decimal(gas, fee) || candidate.MaxTransactionBytes != 64*1024 || candidate.MaxReceiptLogs != 1024 {
			t.Fatal("forecast advisory changed signature, spend or transaction limits", action.Spend, err, allowanceErr)
		}
		invalidSignature := candidate
		invalidSignature.Evidence.HotkeySignature = append([]byte(nil), candidate.Evidence.HotkeySignature...)
		invalidSignature.Evidence.HotkeySignature[0] ^= 1
		if _, err := buildEvidenceRelayAction(runtime.executor.plan, reserve, invalidSignature); err == nil {
			t.Fatal("forecast advisory accepted a changed source signature")
		}
		if err := restarted.horizon.admit(candidate.Evidence.Header, candidate.Evidence.Header.BoundaryBlock); err != nil {
			t.Fatalf("valid signed post-forecast slot was refused: %v", err)
		}
		changed := candidate.Evidence.Header
		changed.Hotkey[0] ^= 1
		if err := restarted.horizon.admit(changed, changed.BoundaryBlock); err == nil || !strings.Contains(err.Error(), "source differs") {
			t.Fatal("forecast advisory changed exact source admission", err)
		}
		limited := *restarted.horizon
		limited.maximum = uint64(len(limited.headerKVs))
		next := evidenceRelayLaunchRequestTest(t, fixture.base, fixture.base.prepared.Members[0], c.EndSettlementEpoch+2, false, 0)
		if err := limited.admit(next.Evidence.Header, next.Evidence.Header.BoundaryBlock); err == nil || !strings.Contains(err.Error(), "no remaining original slots") {
			t.Fatal("forecast advisory changed exact slot exhaustion", err)
		}
	}
	afterPlan, err := json.Marshal(runtime.executor.plan)
	if err != nil || string(afterPlan) != string(beforePlan) || !reflect.DeepEqual(beforeSources, validatorNamespaceTreeSnapshot(t, runtime.executor.stateDir)) {
		t.Fatal("forecast advisory mutated retained plan or source state", err)
	}
}

// A separate chain/client keeps block hashes immutable while checking the
// missing request. A previous failed debit cannot become a refunded slot.
func TestProvisionalRelayContinuationRejectsCorruptDebit(t *testing.T) {
	_, runtime := provisionalRelayContinuationRuntimeTest(t)
	if err := runtime.executor.journal.Append(JournalEntry{DeploymentID: runtime.executor.plan.DeploymentID, PlanHash: runtime.executor.plan.PlanHash,
		ActionID: evidenceRelayActionPrefix + strings.Repeat("ab", 32), IntentHash: "0x" + strings.Repeat("cd", 32), Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	before := validatorNamespaceTreeSnapshot(t, runtime.executor.stateDir)
	if err := runtime.prepareHorizon(); !errors.Is(err, os.ErrNotExist) || runtime.horizon != nil {
		t.Fatal("provisional continuation refunded a debit without its original request", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, runtime.executor.stateDir)) {
		t.Fatal("failed source admission changed original liabilities")
	}
}

// Canonical retained subjects still require their original signed bytes;
// neither missing entries nor equivalent slots with another signature qualify.
func TestProvisionalRelayContinuationRetainsExactSignedCensus(t *testing.T) {
	_, _, continuation := evidenceRelayContinuationTest(t)
	if err := validateEvidenceRelayContinuationRetained(&continuation, continuation.Retained); err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRelayContinuationRetained(&continuation, continuation.Retained[1:]); err == nil {
		t.Fatal("continuation omitted an original retained subject")
	}
	changed := append([]validatorcomponent.ValidatorEvidenceTransactionV2Expected(nil), continuation.Retained...)
	changed[0].Evidence.HotkeySignature = append([]byte(nil), changed[0].Evidence.HotkeySignature...)
	changed[0].Evidence.HotkeySignature[0] ^= 1
	if err := validateEvidenceRelayContinuationRetained(&continuation, changed); err == nil {
		t.Fatal("continuation accepted replacement original signed bytes")
	}
}
