// Probe stake movement distinguishes requested units, source residue and
// destination share quantization. Each conversion has an explicit one-rao bound.
package main

import (
	"errors"
	"fmt"
)

// Returns the exact observed credit after accounting for independently bounded
// source residue and destination share quantization. Unrecorded loss is rejected.
func precompileMoveObservedAmount(step PrecompileMoveStep) (uint64, error) {
	if step.AmountRao == 0 || step.FromBeforeRao < step.AmountRao || step.FromAfterRao > step.FromBeforeRao || step.ToAfterRao < step.ToBeforeRao {
		return 0, errors.New("precompile move has invalid direction or amount")
	}
	debit, credit := step.FromBeforeRao-step.FromAfterRao, step.ToAfterRao-step.ToBeforeRao
	if debit == 0 || credit == 0 || debit < credit || debit-credit > 1 || step.NativeShareCreditRoundingRao != debit-credit || debit > step.AmountRao || step.AmountRao-debit > 1 || step.NativeShareResidueRao != step.AmountRao-debit {
		return 0, errors.New("precompile move does not account for its explicit native-share conversion")
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
	if !amountOk || !fromBeforeOk || !fromAfterOk || !toBeforeOk || !toAfterOk || amount != step.AmountRao || fromBefore != step.FromBeforeRao || toBefore != step.ToBeforeRao || fromAfter > fromBefore || fromBefore-fromAfter > amount || toAfter < toBefore || toAfter-toBefore > fromBefore-fromAfter {
		return PrecompileMoveStep{}, fmt.Errorf("precompile move event changed its approved intent: from=%d->%d to=%d->%d amount=%d", fromBefore, fromAfter, toBefore, toAfter, amount)
	}
	step.FromAfterRao, step.ToAfterRao = fromAfter, toAfter
	step.NativeShareResidueRao = amount - (fromBefore - fromAfter)
	step.NativeShareCreditRoundingRao = (fromBefore - fromAfter) - (toAfter - toBefore)
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

// Both source positions must close exactly, with any native credit conversion
// explicitly subtracted from the returned stake. Source dust cannot pass.
func precompileRoundTripRecovered(evidence *PrecompileConformanceEvidence) bool {
	if !precompileRoundTripAccounted(evidence) {
		return false
	}
	if evidence.Recovery != nil {
		_, move, settled, err := precompileRecoveryPositions(evidence)
		return err == nil && settled && move == 0
	}
	return evidence.Back.FromAfterRao == 0
}

// Transfers across coldkeys use the same native share conversions as hotkey
// moves, while retaining their own exact recovery recipient and request.
func precompileTransferMoveStep(step PrecompileTransferStep) PrecompileMoveStep {
	return PrecompileMoveStep{AmountRao: step.AmountRao, FromBeforeRao: step.ProbeBeforeRao, FromAfterRao: step.ProbeAfterRao,
		ToBeforeRao: step.ProviderBeforeRao, ToAfterRao: step.ProviderAfterRao,
		NativeShareResidueRao: step.NativeShareResidueRao, NativeShareCreditRoundingRao: step.NativeShareCreditRoundingRao}
}

// Reuses exact receipt accounting; no different rounding allowance exists for
// payout recovery and a caller must still require zero remaining probe custody.
func reconcilePrecompileTransferEvent(step PrecompileTransferStep, values map[string]any) (PrecompileTransferStep, error) {
	observed, err := reconcilePrecompileMoveEvent(precompileTransferMoveStep(step), map[string]any{
		"amount": values["amount"], "fromBefore": values["sourceBefore"], "fromAfter": values["sourceAfter"],
		"toBefore": values["destinationBefore"], "toAfter": values["destinationAfter"],
	})
	if err != nil {
		return PrecompileTransferStep{}, err
	}
	step.ProbeAfterRao, step.ProviderAfterRao = observed.FromAfterRao, observed.ToAfterRao
	step.NativeShareResidueRao, step.NativeShareCreditRoundingRao = observed.NativeShareResidueRao, observed.NativeShareCreditRoundingRao
	return step, nil
}

// Final recovery accounts for the explicit credit conversion and still requires
// the complete originally observed probe position to be debited with no dust.
func precompileTransferRecovered(evidence *PrecompileConformanceEvidence) bool {
	if evidence != nil && evidence.Recovery != nil {
		return precompileRecoveryTransferAccounted(evidence)
	}
	if evidence == nil || evidence.Transfer.ProbeBeforeRao <= evidence.Seed.BeforeRao || evidence.Transfer.ProbeAfterRao != evidence.Seed.BeforeRao || evidence.Transfer.AmountRao != evidence.Transfer.ProbeBeforeRao-evidence.Seed.BeforeRao {
		return false
	}
	_, err := precompileMoveObservedAmount(precompileTransferMoveStep(evidence.Transfer))
	return err == nil && evidence.Transfer.NativeShareResidueRao == 0
}
