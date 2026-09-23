// Synthetic rounded receipts prove exact conservation, restart admission, and
// unchanged final custody requirements without runtime or timing assumptions.
package main

import (
	"encoding/json"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// A requested odd amount may leave one unit at its source while transferring
// exactly the same positive amount between both observed positions.
func TestPrecompileMoveRoundingReconcilesConservedReceipt(t *testing.T) {
	intent := PrecompileMoveStep{AmountRao: 101, FromBeforeRao: 203, ToBeforeRao: 17}
	values := map[string]any{"amount": uint64(101), "fromBefore": uint64(203), "fromAfter": uint64(103), "toBefore": uint64(17), "toAfter": uint64(117)}
	if exactDecrease(203, 103, 101) || exactIncrease(17, 117, 101) {
		t.Fatal("fixture no longer reproduces the rejected amount-versus-delta boundary")
	}
	observed, err := reconcilePrecompileMoveEvent(intent, values)
	if err != nil || observed.NativeShareResidueRao != 1 || observed.AmountRao != 101 {
		t.Fatalf("conserved event was not retained: %+v %v", observed, err)
	}
	if amount, err := precompileMoveObservedAmount(observed); err != nil || amount != 100 {
		t.Fatalf("reverse amount borrowed an unreceived unit: %d %v", amount, err)
	}
	if intent.FromAfterRao != 0 || intent.ToAfterRao != 0 || intent.NativeShareResidueRao != 0 {
		t.Fatal("receipt reconciliation mutated the saved intent")
	}
}

// Each rejected variant violates conservation or the fixed one-unit bound;
// labeling the difference as rounding cannot authenticate it.
func TestPrecompileMoveRoundingRejectsLossInflationAndForgedResidue(t *testing.T) {
	original := PrecompileMoveStep{AmountRao: 101, FromBeforeRao: 203, FromAfterRao: 103, ToBeforeRao: 17, ToAfterRao: 117, NativeShareResidueRao: 1}
	for _, fault := range []string{"lost", "minted", "two units", "unrecorded", "forged", "overdraw", "reverse debit", "reverse credit", "zero", "overspend"} {
		step := original
		switch fault {
		case "lost":
			step.ToAfterRao--
		case "minted":
			step.ToAfterRao++
		case "two units":
			step.FromAfterRao++
			step.ToAfterRao--
			step.NativeShareResidueRao++
		case "unrecorded":
			step.NativeShareResidueRao = 0
		case "forged":
			step.NativeShareResidueRao = 2
		case "overdraw":
			step.AmountRao = 204
		case "reverse debit":
			step.FromAfterRao = 204
		case "reverse credit":
			step.ToAfterRao = 16
		case "zero":
			step.FromAfterRao = step.FromBeforeRao
			step.ToAfterRao = step.ToBeforeRao
		case "overspend":
			step.FromAfterRao -= 2
			step.ToAfterRao += 2
		}
		if _, err := precompileMoveObservedAmount(step); err == nil {
			t.Fatalf("%s move passed conserved rounding", fault)
		}
	}
}

// Balances can approach uint64's maximum; comparison uses guarded differences
// and never an overflowing sum or a signed narrowing conversion.
func TestPrecompileMoveRoundingKeepsUint64BoundariesExact(t *testing.T) {
	step := PrecompileMoveStep{AmountRao: 2, FromBeforeRao: math.MaxUint64, FromAfterRao: math.MaxUint64 - 1, ToBeforeRao: math.MaxUint64 - 1, ToAfterRao: math.MaxUint64, NativeShareResidueRao: 1}
	if amount, err := precompileMoveObservedAmount(step); err != nil || amount != 1 {
		t.Fatalf("valid maximum-boundary transfer failed: %d %v", amount, err)
	}
	step.ToAfterRao = 0
	if _, err := precompileMoveObservedAmount(step); err == nil {
		t.Fatal("wrapped destination balance passed")
	}
}

// Receipt fields remain exact even when stake conversion itself quantizes.
func TestPrecompileMoveRoundingRejectsChangedEventIntent(t *testing.T) {
	intent := PrecompileMoveStep{AmountRao: 101, FromBeforeRao: 203, ToBeforeRao: 17}
	for _, fault := range []string{"amount", "fromBefore", "toBefore", "missing", "overflow", "negative", "lost"} {
		values := map[string]any{"amount": uint64(101), "fromBefore": uint64(203), "fromAfter": uint64(103), "toBefore": uint64(17), "toAfter": uint64(117)}
		switch fault {
		case "amount", "fromBefore", "toBefore":
			values[fault] = values[fault].(uint64) + 1
		case "missing":
			delete(values, "fromAfter")
		case "overflow":
			values["amount"] = new(big.Int).Lsh(big.NewInt(1), 64)
		case "negative":
			values["amount"] = big.NewInt(-1)
		case "lost":
			values["toAfter"] = uint64(116)
		}
		if _, err := reconcilePrecompileMoveEvent(intent, values); err == nil {
			t.Fatalf("%s changed receipt passed", fault)
		}
	}
}

// Complete evidence permits conserved forward quantization only after all
// positions are restored and every recovery unit reaches its original owner.
func TestPrecompileMoveRoundingFinalEvidenceStillRequiresFullRecovery(t *testing.T) {
	evidence := completePrecompileEvidence()
	evidence.Forward.FromAfterRao++
	evidence.Forward.ToAfterRao--
	evidence.Forward.NativeShareResidueRao = 1
	evidence.Back.AmountRao--
	evidence.Back.FromBeforeRao--
	evidence.Back.ToBeforeRao++
	if !precompileEvidenceComplete(evidence) {
		t.Fatal("fully restored conserved round trip was rejected")
	}
	evidence.Back.FromAfterRao++
	evidence.Back.ToAfterRao--
	evidence.Back.NativeShareResidueRao = 1
	if !precompileMoveBackValid(evidence) {
		t.Fatal("explicit conserved reverse remainder was lost")
	}
	if precompileEvidenceComplete(evidence) {
		t.Fatal("reverse remainder passed final acceptance with unrecovered probe funds")
	}
}

// Omitted zero remainders preserve canonical legacy evidence hashes; nonzero
// remainders become explicit checksum-bound facts rather than inferred waivers.
func TestPrecompileMoveRoundingPreservesLegacyEncoding(t *testing.T) {
	step := PrecompileMoveStep{AmountRao: 50, FromBeforeRao: 100, FromAfterRao: 50, ToAfterRao: 50}
	encoded, err := json.Marshal(step)
	if err != nil || strings.Contains(string(encoded), "native_share_residue_rao") {
		t.Fatalf("legacy exact move encoding changed: %s %v", encoded, err)
	}
	step.FromAfterRao++
	step.ToAfterRao--
	step.NativeShareResidueRao = 1
	encoded, err = json.Marshal(step)
	if err != nil || !strings.Contains(string(encoded), `"native_share_residue_rao":1`) {
		t.Fatalf("quantized move lost explicit remainder: %s %v", encoded, err)
	}
}

// Startup can authenticate a finalized quantized transaction before its failed
// postcondition was written, then retain the same exact receipt and nonce.
func TestPrecompileMoveRoundingResumesFinalizedSuccessor(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	evidence, entries, reader := precompileProbeSuccessorCallFixtureWithMoveResidue(t, fixture, 1)
	forward := evidence.Forward
	truncatePrecompileProbeSuccessorEvidence(evidence, 3)
	prefix := entries[:len(fixture.entries)+3]
	entry := prefix[len(prefix)-1]
	evidence.Forward.TransactionHash, evidence.Forward.BlockHash, evidence.Forward.BlockNumber = "", "", 0
	evidence.Forward.FromAfterRao, evidence.Forward.ToAfterRao, evidence.Forward.NativeShareResidueRao = 0, 0, 0
	if err := writePrecompileEvidence(fixture.stateDir, evidence); err != nil {
		t.Fatal(err)
	}
	nonce := fixture.plan.PrecompileProbeSuccessor.DeployerNonce + 3
	if err := verifyPrecompileProbeSuccessorCalls(t.Context(), fixture.cfg, fixture.stateDir, fixture.plan, prefix, reader, reader.finalized, nonce); err != nil {
		t.Fatalf("finalized quantized receipt lost restart admission: %v", err)
	}
	parsed, err := (&Executor{}).precompileABI()
	if err != nil {
		t.Fatal(err)
	}
	receipt := reader.receipts[common.HexToHash(entry.TransactionHash)]
	values, err := conformanceEventValues(parsed, "MoveRoundTrip", receipt, common.HexToHash(evidence.SampleHotkey), common.HexToHash(evidence.MoveHotkey))
	if err != nil {
		t.Fatal(err)
	}
	observed, err := reconcilePrecompileMoveEvent(evidence.Forward, values)
	if err != nil {
		t.Fatal(err)
	}
	observed.TransactionHash, observed.BlockNumber, observed.BlockHash = receiptFields(receipt)
	if observed != forward {
		t.Fatalf("recovery changed original finalized move: %+v", observed)
	}
	evidence.Forward = observed
	if err := writePrecompileEvidence(fixture.stateDir, evidence); err != nil {
		t.Fatal(err)
	}
	if err := verifyPrecompileProbeSuccessorCalls(t.Context(), fixture.cfg, fixture.stateDir, fixture.plan, prefix, reader, reader.finalized, nonce); err != nil {
		t.Fatalf("reconciled quantized receipt lost durable authentication: %v", err)
	}
	evidence.Forward.NativeShareResidueRao = 0
	if err := verifyPrecompileProbeSuccessorEvent(evidence, entry.ActionID, receipt, false); err == nil {
		t.Fatal("replay accepted an unrecorded one-unit remainder")
	}
}

// Reverse calldata is derived from the authenticated credit; a requested but
// unreceived forward unit can never be borrowed from other probe balances.
func TestPrecompileMoveRoundingSuccessorCallUsesActualCredit(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	evidence, entries, reader := precompileProbeSuccessorCallFixtureWithMoveResidue(t, fixture, 1)
	nonce := fixture.plan.PrecompileProbeSuccessor.DeployerNonce + 6
	if err := verifyPrecompileProbeSuccessorCalls(t.Context(), fixture.cfg, fixture.stateDir, fixture.plan, entries, reader, reader.finalized, nonce); err != nil {
		t.Fatalf("conserved full successor call sequence failed: %v", err)
	}
	evidence.Back.AmountRao++
	if _, _, _, err := precompileProbeSuccessorCall(fixture.plan, evidence, "precompile.move-back"); err == nil {
		t.Fatal("reverse calldata overspent actual forward credit")
	}
}
