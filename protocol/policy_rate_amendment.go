// The testnet rate revision preserves deposit semantics and custody bounds.
package protocol

import (
	"errors"
	"fmt"
	"reflect"
)

// This reviewed testnet correction changes only the rate scale and policy
// version. Caps, conviction thresholds, quality, usage lag and custody stay exact.
func ValidateTestnetRateAmendment(previous, next *Policy) error {
	if previous == nil || next == nil || previous.NetworkProfile != "testnet" || next.NetworkProfile != "testnet" || previous.PolicyID == ^uint64(0) || next.PolicyID != previous.PolicyID+1 {
		return errors.New("rate amendment requires the next testnet policy version")
	}
	if err := next.Validate(); err != nil {
		return err
	}
	oldRates := []uint64{1_000_000, 800_000, 600_000}
	newRates := []uint64{40_000_000_000, 32_000_000_000, 24_000_000_000}
	thresholds := []uint64{0, 1_000_000_000, 10_000_000_000}
	if len(previous.Deposit.Tiers) != len(oldRates) || len(next.Deposit.Tiers) != len(newRates) {
		return errors.New("rate amendment tier inventory differs")
	}
	for index, before := range previous.Deposit.Tiers {
		after := next.Deposit.Tiers[index]
		if before.MinConvictionRao != thresholds[index] || after.MinConvictionRao != thresholds[index] || before.RateDenominator != 1 || after.RateDenominator != 1 || before.RateNumeratorRaoPerGiB != oldRates[index] || after.RateNumeratorRaoPerGiB != newRates[index] {
			return fmt.Errorf("rate amendment tier %d is not the exact reviewed scale", index)
		}
	}
	normalized := *previous
	normalized.PolicyID = next.PolicyID
	normalized.Deposit = previous.Deposit
	normalized.Deposit.Tiers = next.Deposit.Tiers
	if !reflect.DeepEqual(normalized, *next) {
		return errors.New("rate amendment changes fields outside the exact rates and policy version")
	}
	return nil
}
