// Synthetic receipts force stake growth at each observation boundary, including
// a delayed reverse call and a snapshot included after its pre-send read.
package main

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// The signed reverse request returns principal while both positions have
// accrued independently. The remaining move credit stays an explicit liability.
func TestPrecompileRoundTripCreditsPermitSnapshotWithoutCompletingCustody(t *testing.T) {
	evidence := completePrecompileEvidence()
	evidence.Forward.FromBeforeRao += 3
	evidence.Forward.FromAfterRao += 3
	evidence.Back.FromBeforeRao += 17
	evidence.Back.FromAfterRao += 17
	evidence.Back.ToBeforeRao += 24
	evidence.Back.ToAfterRao += 24
	if precompileRoundTripRecovered(evidence) {
		t.Fatal("fixture does not reproduce the cross-block equality failure")
	}
	if precompileRoundTripAccounted(evidence) {
		t.Fatal("unrecorded inter-block credits passed preparation")
	}
	if err := recordPrecompileRoundTripCredits(evidence); err != nil {
		t.Fatal(err)
	}
	want := PrecompileRoundTripCredits{SeedToForwardSampleRao: 3, ForwardToBackSampleRao: 21, ForwardToBackMoveRao: 17, UnrecoveredMoveRao: 17}
	if evidence.RoundTripCredits == nil || *evidence.RoundTripCredits != want || !precompileRoundTripAccounted(evidence) {
		t.Fatalf("credits did not reconcile: %+v", evidence.RoundTripCredits)
	}
	if precompileEvidenceComplete(evidence) || precompileRoundTripRecovered(evidence) {
		t.Fatal("preparation accounting waived the final recovery obligation")
	}
}

// Within-call loss cannot be disguised as accrual; every inter-block credit
// also retains both chronological boundaries and the exact outstanding amount.
func TestPrecompileRoundTripCreditsRejectLossPrincipalDustAndTampering(t *testing.T) {
	for _, fault := range []string{"source decrease", "sample decrease", "seed decrease", "within-call loss", "principal dust", "same block growth", "reverse chronology", "record changed", "liability changed"} {
		evidence := completePrecompileEvidence()
		evidence.Back.FromBeforeRao += 17
		evidence.Back.FromAfterRao += 17
		evidence.Back.ToBeforeRao += 21
		evidence.Back.ToAfterRao += 21
		if err := recordPrecompileRoundTripCredits(evidence); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "source decrease":
			evidence.Back.FromBeforeRao = evidence.Forward.ToAfterRao - 1
		case "sample decrease":
			evidence.Back.ToBeforeRao = evidence.Forward.FromAfterRao - 1
		case "seed decrease":
			evidence.Forward.FromBeforeRao--
		case "within-call loss":
			evidence.Back.ToAfterRao--
		case "principal dust":
			evidence.Back.FromAfterRao++
			evidence.Back.ToAfterRao--
			evidence.Back.NativeShareResidueRao = 1
		case "same block growth":
			evidence.Back.BlockNumber = evidence.Forward.BlockNumber
		case "reverse chronology":
			evidence.Back.BlockNumber = evidence.Forward.BlockNumber - 1
		case "record changed":
			evidence.RoundTripCredits.ForwardToBackSampleRao++
		case "liability changed":
			evidence.RoundTripCredits.UnrecoveredMoveRao--
		}
		if precompileRoundTripAccounted(evidence) || recordPrecompileRoundTripCredits(evidence) == nil {
			t.Fatalf("%s passed receipt-bound preparation accounting", fault)
		}
	}
}

// Native share quantization and inter-block credits are independent quantities;
// keeping them separate prevents accumulated earnings from widening rounding.
func TestPrecompileRoundTripCreditsKeepNativeRoundingBounded(t *testing.T) {
	evidence := completePrecompileEvidence()
	evidence.Forward.FromAfterRao++
	evidence.Forward.ToAfterRao--
	evidence.Forward.NativeShareResidueRao = 1
	evidence.Back.AmountRao--
	evidence.Back.FromBeforeRao += 16
	evidence.Back.FromAfterRao += 17
	evidence.Back.ToBeforeRao += 22
	evidence.Back.ToAfterRao += 20
	evidence.Back.NativeShareCreditRoundingRao = 1
	if err := recordPrecompileRoundTripCredits(evidence); err != nil || !precompileRoundTripAccounted(evidence) {
		t.Fatalf("bounded rounding and exact credits failed: %v", err)
	}
	if evidence.RoundTripCredits.ForwardToBackMoveRao != 17 || evidence.RoundTripCredits.ForwardToBackSampleRao != 21 {
		t.Fatalf("rounding was reclassified as growth: %+v", evidence.RoundTripCredits)
	}
	evidence.Back.ToAfterRao--
	evidence.Back.NativeShareCreditRoundingRao++
	if precompileRoundTripAccounted(evidence) {
		t.Fatal("earnings widened the native conversion bound")
	}
}

// Completed historical zero-credit records remain byte-identical and valid.
func TestPrecompileRoundTripCreditsPreserveLegacyEncoding(t *testing.T) {
	evidence := completePrecompileEvidence()
	before, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	if err := recordPrecompileRoundTripCredits(evidence); err != nil || !precompileRoundTripAccounted(evidence) || !precompileEvidenceComplete(evidence) {
		t.Fatalf("legacy evidence changed behavior: %v", err)
	}
	after, err := json.Marshal(evidence)
	if err != nil || string(before) != string(after) || strings.Contains(string(after), "round_trip_credits") {
		t.Fatalf("legacy canonical encoding changed: %v", err)
	}
}

// The event baseline is authoritative because snapshot has no amount argument;
// same-block receipt identity and nondecrease from the saved read remain strict.
func TestPrecompileSnapshotCreditsReconcileInclusionGrowth(t *testing.T) {
	step := PrecompileSnapshotStep{BaselineRao: 100}
	values := map[string]any{"baseline": uint64(107), "blockNumber": uint64(9)}
	observed, err := reconcilePrecompileSnapshotEvent(step, values, 9)
	if err != nil || observed.BaselineRao != 107 || observed.SinceBlock != 9 || step.BaselineRao != 100 {
		t.Fatalf("snapshot did not retain inclusion baseline: %+v %v", observed, err)
	}
	for _, fault := range []string{"decrease", "missing", "foreign block", "zero block", "overflow"} {
		changed := map[string]any{"baseline": uint64(107), "blockNumber": uint64(9)}
		switch fault {
		case "decrease":
			changed["baseline"] = uint64(99)
		case "missing":
			delete(changed, "baseline")
		case "foreign block":
			changed["blockNumber"] = uint64(10)
		case "zero block":
			changed["blockNumber"] = uint64(0)
		case "overflow":
			changed["baseline"] = new(big.Int).Lsh(big.NewInt(1), 64)
		}
		if _, err := reconcilePrecompileSnapshotEvent(step, changed, 9); err == nil {
			t.Fatalf("%s snapshot passed", fault)
		}
	}
}

// Authentication replays the same signed calls and exact receipts after a
// delayed reverse and after a crash between snapshot inclusion and persistence.
func TestPrecompileSnapshotCreditsResumeExactSuccessorPrefix(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	evidence, entries, reader := precompileProbeSuccessorCallFixture(t, fixture)
	truncatePrecompileProbeSuccessorEvidence(evidence, 5)
	evidence.Back.FromBeforeRao += 17
	evidence.Back.FromAfterRao += 17
	evidence.Back.ToBeforeRao += 21
	evidence.Back.ToAfterRao += 21
	evidence.Snapshot.BaselineRao += 21
	if err := recordPrecompileRoundTripCredits(evidence); err != nil {
		t.Fatal(err)
	}
	parsed, err := (&Executor{}).precompileABI()
	if err != nil {
		t.Fatal(err)
	}
	backReceipt := reader.receipts[common.HexToHash(evidence.Back.TransactionHash)]
	backReceipt.Logs[0].Data, err = parsed.Events["MoveRoundTrip"].Inputs.NonIndexed().Pack(
		new(big.Int).SetUint64(evidence.Back.AmountRao), new(big.Int).SetUint64(evidence.Back.FromBeforeRao),
		new(big.Int).SetUint64(evidence.Back.FromAfterRao), new(big.Int).SetUint64(evidence.Back.ToBeforeRao), new(big.Int).SetUint64(evidence.Back.ToAfterRao))
	if err != nil {
		t.Fatal(err)
	}
	snapshotReceipt := reader.receipts[common.HexToHash(evidence.Snapshot.TransactionHash)]
	snapshotReceipt.Logs[0].Data, err = parsed.Events["DividendSnapshot"].Inputs.NonIndexed().Pack(new(big.Int).SetUint64(evidence.Snapshot.BaselineRao), evidence.Snapshot.SinceBlock)
	if err != nil {
		t.Fatal(err)
	}
	prefix := entries[:len(fixture.entries)+5]
	nonce := fixture.plan.PrecompileProbeSuccessor.DeployerNonce + 5
	if err := writePrecompileEvidence(fixture.stateDir, evidence); err != nil {
		t.Fatal(err)
	}
	if err := verifyPrecompileProbeSuccessorCalls(t.Context(), fixture.cfg, fixture.stateDir, fixture.plan, prefix, reader, reader.finalized, nonce); err != nil {
		t.Fatalf("exact accrued prefix failed restart: %v", err)
	}
	evidence.Snapshot.TransactionHash, evidence.Snapshot.BlockHash, evidence.Snapshot.BlockNumber = "", "", 0
	evidence.Snapshot.SinceBlock = 0
	// A credit between read and inclusion does not change snapshot calldata.
	snapshotReceipt.Logs[0].Data, err = parsed.Events["DividendSnapshot"].Inputs.NonIndexed().Pack(new(big.Int).SetUint64(evidence.Snapshot.BaselineRao+3), uint64(214))
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrecompileEvidence(fixture.stateDir, evidence); err != nil {
		t.Fatal(err)
	}
	if err := verifyPrecompileProbeSuccessorCalls(t.Context(), fixture.cfg, fixture.stateDir, fixture.plan, prefix, reader, reader.finalized, nonce); err != nil {
		t.Fatalf("inclusion credit lost interrupted snapshot recovery: %v", err)
	}
	evidence.RoundTripCredits.UnrecoveredMoveRao--
	if _, _, _, err := precompileProbeSuccessorCall(fixture.plan, evidence, "precompile.snapshot"); err == nil {
		t.Fatal("snapshot replay accepted a forged residual liability")
	}
	evidence.RoundTripCredits.UnrecoveredMoveRao++
	evidence.Back.FromBeforeRao++
	if err := verifyPrecompileProbeSuccessorEvent(evidence, "precompile.move-back", backReceipt, false); err == nil {
		t.Fatal("receipt replay accepted forged extra inter-block credit")
	}
}
