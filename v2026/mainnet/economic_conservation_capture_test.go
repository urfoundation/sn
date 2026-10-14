// These public joins use original Wasm execution and receipts emitted by the
// retained vault bytecode. The local runtime/precompile identities are test
// authority only; no observed stock is silently classified as provider income.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
)

func newEconomicConservationCaptureFixture(t *testing.T, name string, reverted bool, change func(*historicalReplayJob), configure ...func(*monitorEvmFixture)) (*economicConservationArchiveFixture, *economicCaptureContractFixture) {
	t.Helper()
	var contract *economicCaptureContractFixture
	amount := uint64(20)
	if name == "capture-causes" {
		amount = 24
	}
	f, _ := newEconomicConservationPrincipalFixtureWithVault(t, name, true, func(job *historicalReplayJob) {
		if contract == nil || len(job.ExtrinsicsHex) != 1 {
			t.Fatal("original capture fixture lost its contract or extrinsic")
		}
		// The original Wasm reads this opaque transaction hash from the body.
		// The independently computed native post-state remains unchanged.
		body := append(rootCompact(32), contract.transaction.Hash().Bytes()...)
		job.ExtrinsicsHex = []string{nativeExecutionTestHex(body)}
		if change != nil {
			change(job)
		}
	}, func(vault *monitorEvmFixture) {
		contract = economicCaptureExecuteContract(t, vault, amount, reverted)
		for _, mutate := range configure {
			mutate(vault)
		}
	})
	return f, contract
}

func economicCaptureTestWitness(t *testing.T, f *economicConservationArchiveFixture, summary economicConservationSummary) economicConservationCapture {
	t.Helper()
	state := f.source.state(t)
	if !summary.NativeCurrent || !summary.VaultCurrent || summary.JoinIssue != "" || len(state.Captures) != 1 || summary.CausallyJoinedCaptures != 1 || state.Captures[0].PrincipalEffects == nil {
		t.Fatal("actual native execution and vault receipt did not form the original capture join", summary, state.Captures)
	}
	if summary.TargetMet != nil || summary.ActualNativeOutcomeVerified || summary.ActivationReady || summary.FullQuantizationToleranceAlpha != nil || summary.NativeFeeWithdrawalRao != nil || summary.NativeFeeRefundRao != nil || summary.AggregatePayments != 0 || summary.AcceptedClaims != 0 || f.source.claimReads.Load() == 0 {
		t.Fatal("one original capture became whole-provider payment or policy authority", summary)
	}
	return state.Captures[0]
}

func TestEconomicConservationPublicOriginalCaptureKeepsStockSeparateFromEarnings(t *testing.T) {
	f, contract := newEconomicConservationCaptureFixture(t, "capture", false, nil)
	summary := f.sample(t, monitorServiceHooks{})
	capture := economicCaptureTestWitness(t, f, summary)
	proof := capture.PrincipalEffects
	if contract.poolAfter != "0" || contract.escrowAfter != "20" || contract.receipt.Status != types.ReceiptStatusSuccessful || proof.OpeningStock != "14" || proof.LiquidEarnings != "6" || proof.Captured != "20" || proof.After != "0" || proof.Deposits != "0" || proof.Withdrawals != "0" || proof.Refunds != "0" || capture.AmountDifferenceAlpha == nil || *capture.AmountDifferenceAlpha != "14" || capture.OpeningPrincipalAlpha == nil || *capture.OpeningPrincipalAlpha != "14" || summary.TailGrossAlpha == nil || *summary.TailGrossAlpha != "9" {
		t.Fatal("original pool stock was earned again or the actual vault move was lost", proof, capture, summary)
	}
	if proof.TransactionHash != contract.transaction.Hash().Hex() || proof.TransactionHash != capture.Event.TransactionHash || proof.ReceiptHash != capture.Event.ReceiptHash || proof.ExtrinsicIndex != 0 || len(proof.Sources) != 1 || proof.Through.Boundary != *capture.Native || proof.From.Boundary != f.source.policy.Native.Observation.Execution.Principal.Parent || !planSha256(proof.Sources[0].Projection) || !rootCanonicalHash(proof.ExtrinsicHash) {
		t.Fatal("capture borrowed an amount without its original native, body and receipt identities", proof, capture)
	}
}

func TestEconomicConservationPublicOriginalCaptureAccountsForDepositsWithdrawalsAndRefunds(t *testing.T) {
	f, contract := newEconomicConservationCaptureFixture(t, "capture-causes", false, nil)
	capture := economicCaptureTestWitness(t, f, f.sample(t, monitorServiceHooks{}))
	proof := capture.PrincipalEffects
	if proof.OpeningStock != "14" || proof.Deposits != "5" || proof.Withdrawals != "3" || proof.Refunds != "2" || proof.LiquidEarnings != "6" || proof.Captured != "24" || proof.After != "0" || contract.escrowAfter != "24" || *capture.AmountDifferenceAlpha != "18" {
		t.Fatal("original principal flows were replaced by a before/after earnings estimate", proof, capture)
	}
}

func TestEconomicConservationPublicCaptureRequiresOriginalGetterPolicy(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture", false, nil, func(vault *monitorEvmFixture) { vault.policy.CaptureIdentity = false })
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if !summary.NativeCurrent || !summary.VaultCurrent || summary.CausallyJoinedCaptures != 0 || len(state.Captures) != 1 || state.Captures[0].PrincipalEffects != nil || state.Captures[0].OpeningPrincipalAlpha != nil || state.Captures[0].Event.CaptureIdentity != nil || *state.Captures[0].AmountDifferenceAlpha != "14" {
		t.Fatal("a legacy amount-only policy acquired unrequested original vault identity", summary, state.Captures)
	}
}

func TestEconomicConservationPublicCaptureRefusesForeignOriginalTransaction(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture", false, func(job *historicalReplayJob) {
		job.ExtrinsicsHex[0] = nativeExecutionTestHex(append(rootCompact(32), bytes.Repeat([]byte{0x98}, 32)...))
	})
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if !summary.NativeCurrent || !summary.VaultCurrent || summary.CausallyJoinedCaptures != 0 || len(state.Captures) != 1 || state.Captures[0].PrincipalEffects != nil || state.PrincipalExecutions[0].Projection.Effects[1].EvmTransactionHash == state.Captures[0].Event.TransactionHash {
		t.Fatal("same amounts borrowed another original native transaction", summary, state)
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	if state := f.source.state(t); len(state.Captures) != 1 || state.Captures[0].PrincipalEffects != nil || state.Archive.Counts.CausalCaptures != 0 {
		t.Fatal("unjoined original capture disappeared during retirement", state)
	}
}

func TestEconomicConservationPublicCaptureRefusesForeignOriginalVaultColdkey(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture", false, nil, func(vault *monitorEvmFixture) {
		var foreign [32]byte
		copy(foreign[:], bytes.Repeat([]byte{0x34}, 32))
		vault.fixtureGetters["selfColdkey"] = []any{foreign}
	})
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if !summary.NativeCurrent || !summary.VaultCurrent || summary.CausallyJoinedCaptures != 0 || len(state.Captures) != 1 || state.Captures[0].PrincipalEffects != nil || state.Captures[0].Event.CaptureIdentity == nil || state.Captures[0].Event.CaptureIdentity.Coldkey == f.source.policy.Routes[0].Coldkey {
		t.Fatal("original stake was joined through a foreign block-pinned vault coldkey", summary, state.Captures)
	}
}

func TestEconomicConservationPublicOriginalCaptureRevertRollsBackStakeAndReceipt(t *testing.T) {
	f, contract := newEconomicConservationCaptureFixture(t, "capture-rollback", true, nil)
	summary := f.sample(t, monitorServiceHooks{})
	state := f.source.state(t)
	if contract.receipt.Status != types.ReceiptStatusFailed || len(contract.receipt.Logs) != 0 || contract.poolAfter != "20" || contract.escrowAfter != "0" || summary.Captures != 0 || summary.CausallyJoinedCaptures != 0 || state.Vault.Snapshot.Counters["totalCaptured"] != "0" || len(state.PrincipalExecutions) != 1 || *state.PrincipalExecutions[0].Projection.After[0].OpeningStakeAlpha != "20" || len(state.Vault.Fees) != 1 || state.Vault.Fees[0].Success || summary.TargetMet != nil {
		t.Fatal("actual reverted vault move survived in native stock, receipt or conservation", contract, summary, state)
	}
}

func TestEconomicConservationPublicCaptureUnclassifiedNetZeroEffectsStayHot(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture-unclassified", false, nil)
	summary := f.sample(t, monitorServiceHooks{})
	if !summary.NativeCurrent || !summary.VaultCurrent || summary.CausallyJoinedCaptures != 0 || summary.PrincipalEffects == nil || summary.PrincipalEffects.Current || len(summary.PrincipalEffects.Active[0].UnmatchedMutations) != 2 {
		t.Fatal("equal stock and receipt amounts concealed two original unclassified mutations", summary)
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	if len(state.Captures) != 1 || len(state.PrincipalExecutions) != 1 || state.Captures[0].PrincipalEffects != nil {
		t.Fatal("unexplained original effects or capture retired as complete", state)
	}
}

func TestEconomicConservationPublicOriginalCaptureSurvivesColdArchive(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture-causes", false, nil)
	first := f.sample(t, monitorServiceHooks{})
	original := economicCaptureTestWitness(t, f, first)
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	state := f.source.state(t)
	if len(state.Captures) != 0 || len(state.PrincipalExecutions) != 0 || state.Archive.Counts.CausalCaptures != 1 || state.Archive.Counts.Captures != 1 {
		t.Fatal("complete original capture did not retire with its exact original effects", state)
	}
	second := f.sample(t, monitorServiceHooks{})
	if second.CausallyJoinedCaptures != 1 || second.Captures != 1 || second.OpeningPrincipalAlpha == nil || *second.OpeningPrincipalAlpha != original.PrincipalEffects.OpeningStock || second.TargetMet != nil || second.ActualNativeOutcomeVerified || *second.TailGrossAlpha != *first.TailGrossAlpha {
		t.Fatal("cold original capture was recounted or converted into earnings", first, second)
	}
}

func TestEconomicConservationPublicLaggingVaultJoinsRetiredOriginalNativeEffects(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture", false, nil)
	f.source.vault.unavailable.Store(true)
	var output, issue bytes.Buffer
	var waits atomic.Uint64
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &issue, func() time.Time { return f.source.now }, monitorServiceHooks{rpcWait: func(ctx context.Context, role string, _ time.Duration) error {
		if role != f.source.policy.Vault.Role {
			t.Error("unrelated role retried in the vault outage fixture", role)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("vault retry lost its owned read deadline")
		}
		waits.Add(1)
		return context.DeadlineExceeded
	}})
	var first economicConservationSummary
	if err := json.Unmarshal(output.Bytes(), &first); err != nil {
		t.Fatal(err, issue.String())
	}
	if code != 3 || !first.NativeCurrent || first.VaultCurrent || first.VaultHeld || first.VaultCursor != f.source.policy.Vault.From || waits.Load() != 1 || first.Captures != 0 || len(f.source.state(t).PrincipalExecutions) != 1 || f.source.claimReads.Load() == 0 {
		t.Fatal("vault outage blocked original native/Claim progress or lost the pending page", code, first, issue.String())
	}
	f.reset(t)
	_, args := f.plan(t)
	if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
		t.Fatal(code, issue)
	}
	if state := f.source.state(t); len(state.PrincipalExecutions) != 0 || state.Archive.PrincipalEffects == nil || len(state.Captures) != 0 {
		t.Fatal("native execution did not actually retire before vault recovery", state)
	}
	f.source.vault.unavailable.Store(false)
	second := f.sample(t, monitorServiceHooks{})
	capture := economicCaptureTestWitness(t, f, second)
	if capture.PrincipalEffects.OpeningStock != "14" || capture.PrincipalEffects.LiquidEarnings != "6" || second.NativeCursor != first.NativeCursor || len(f.source.state(t).PrincipalExecutions) != 0 {
		t.Fatal("delayed original receipt required replay or lost its retired causal input", first, second, capture)
	}
}

func TestEconomicConservationPublicCaptureCompanionCannotSelfSealDifferentStock(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture", false, nil)
	economicCaptureTestWitness(t, f, f.sample(t, monitorServiceHooks{}))
	state := f.source.state(t)
	state.Captures[0].PrincipalEffects.OpeningStock = "15"
	state.Captures[0].OpeningPrincipalAlpha = &state.Captures[0].PrincipalEffects.OpeningStock
	state.ContentHash = state.hash()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := openMonitorHistorySnapshot(f.ctx, f.source.checkpoint, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(writer.publish(append(raw, '\n'), nil), writer.close()); err != nil {
		t.Fatal(err)
	}
	before := f.source.claimReads.Load()
	var output, issue bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &issue, func() time.Time { return f.source.now }, monitorServiceHooks{})
	if code != 3 || output.Len() != 0 || !strings.Contains(issue.String(), "economic capture causal join differs from original native execution and receipt") || f.source.claimReads.Load() != before {
		t.Fatal("self-sealed capture stock replaced its actual original execution", code, output.String(), issue.String())
	}
}

func TestEconomicConservationPublicFailedReceiptCannotRetainCaptureLogs(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture", false, nil, func(vault *monitorEvmFixture) {
		economicConservationTestEvmRehash(t, vault, func(_ uint64, receipt *types.Receipt) { receipt.Status = types.ReceiptStatusFailed })
	})
	var output, issue bytes.Buffer
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &issue, func() time.Time { return f.source.now }, monitorServiceHooks{})
	var summary economicConservationSummary
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatal(err, issue.String())
	}
	if code != 3 || !summary.NativeCurrent || summary.VaultCurrent || !summary.VaultHeld || summary.Captures != 0 || summary.CausallyJoinedCaptures != 0 || !strings.Contains(summary.VaultIssue, "failed economic receipt retained committed contract logs") || f.source.claimReads.Load() == 0 {
		t.Fatal("failed original receipt logs became a committed capture or stopped healthy siblings", code, summary)
	}
}

func TestEconomicConservationOriginalCaptureCancellationRetainsEvidence(t *testing.T) {
	f, _ := newEconomicConservationCaptureFixture(t, "capture", false, nil)
	economicCaptureTestWitness(t, f, f.sample(t, monitorServiceHooks{}))
	state := f.source.state(t)
	before, err := cloneEconomicConservation(&state)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if err := state.reconcileCaptureEffects(ctx, f.source.policy, false); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(state.Captures, before.Captures) {
		t.Fatal("canceled capture owner continued work or replaced original evidence", err, state.Captures)
	}
}
