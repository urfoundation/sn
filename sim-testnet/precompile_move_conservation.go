// Probe moves retain the requested amount separately from conserved native
// share quantization. Only an explicit one-rao remainder may stay at the source.
package main

import (
	"errors"
	"fmt"
)

// Returns the exact observed credit, requiring an equal debit and an explicitly
// recorded remainder. This is not a tolerance for lost or manufactured stake.
func precompileMoveObservedAmount(step PrecompileMoveStep) (uint64, error) {
	if step.AmountRao == 0 || step.FromBeforeRao < step.AmountRao || step.FromAfterRao > step.FromBeforeRao || step.ToAfterRao < step.ToBeforeRao {
		return 0, errors.New("precompile move has invalid direction or amount")
	}
	debit, credit := step.FromBeforeRao-step.FromAfterRao, step.ToAfterRao-step.ToBeforeRao
	if debit == 0 || debit != credit || debit > step.AmountRao || step.AmountRao-debit > 1 || step.NativeShareResidueRao != step.AmountRao-debit {
		return 0, errors.New("precompile move does not conserve its explicit native-share remainder")
	}
	return credit, nil
}

// Builds a post-state from the receipt while preserving the exact approved
// request and pre-state. A failed parse leaves the saved intent unchanged.
func reconcilePrecompileMoveEvent(step PrecompileMoveStep, values map[string]any) (PrecompileMoveStep, error) {
	amount, amountOk := conformanceEventUint64(values, "amount")
	fromBefore, fromBeforeOk := conformanceEventUint64(values, "fromBefore")
	fromAfter, fromAfterOk := conformanceEventUint64(values, "fromAfter")
	toBefore, toBeforeOk := conformanceEventUint64(values, "toBefore")
	toAfter, toAfterOk := conformanceEventUint64(values, "toAfter")
	if !amountOk || !fromBeforeOk || !fromAfterOk || !toBeforeOk || !toAfterOk || amount != step.AmountRao || fromBefore != step.FromBeforeRao || toBefore != step.ToBeforeRao || fromAfter > fromBefore || fromBefore-fromAfter > amount {
		return PrecompileMoveStep{}, fmt.Errorf("precompile move event changed its approved intent: from=%d->%d to=%d->%d amount=%d", fromBefore, fromAfter, toBefore, toAfter, amount)
	}
	step.FromAfterRao, step.ToAfterRao = fromAfter, toAfter
	step.NativeShareResidueRao = amount - (fromBefore - fromAfter)
	if _, err := precompileMoveObservedAmount(step); err != nil {
		return PrecompileMoveStep{}, err
	}
	return step, nil
}

// The reverse request is the authenticated amount actually received, never an
// unreceived unit from the forward request. Historical exact moves remain exact.
func precompileMoveBackValid(evidence *PrecompileConformanceEvidence) bool {
	if evidence == nil {
		return false
	}
	amount, err := precompileMoveObservedAmount(evidence.Forward)
	if err != nil || evidence.Back.AmountRao != amount {
		return false
	}
	_, err = precompileMoveObservedAmount(evidence.Back)
	return err == nil
}

// A conformance round trip must still restore both original positions; a
// quantized reverse move leaving any probe dust is not final acceptance.
func precompileRoundTripRestored(evidence *PrecompileConformanceEvidence) bool {
	return evidence != nil && evidence.Forward.AmountRao == evidence.Seed.DeltaRao/2 && precompileMoveBackValid(evidence) &&
		evidence.Forward.FromBeforeRao == evidence.Seed.AfterRao &&
		evidence.Back.FromBeforeRao == evidence.Forward.ToAfterRao && evidence.Back.ToBeforeRao == evidence.Forward.FromAfterRao &&
		evidence.Back.FromAfterRao == evidence.Forward.ToBeforeRao && evidence.Back.ToAfterRao == evidence.Forward.FromBeforeRao
}
