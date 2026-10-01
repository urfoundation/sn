// Exact reference accounting conserves the 10/90 target across native intervals.
// Input amounts are caller-supplied reference data, not authenticated emissions.
package main

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
)

const economicReferenceInputSchema = "urnetwork-native-miner-reference-input-v1"
const economicReferenceSchema = "urnetwork-native-miner-reference-v1"
const maximumReferenceIntervals = 10000

// Each input is one native interval's pre-withholding miner tranche in alpha
// atomic units. It excludes owner/validator emission and custody principal.
type economicReferenceInput struct {
	Schema                 string   `json:"schema"`
	NativeMinerAllocations []string `json:"native_miner_allocations_alpha"`
}

// Cumulative division carries policy rounding between intervals. The reference
// remainder is not runtime truncation dust or a future provider liability.
type economicReferenceInterval struct {
	Index                  int    `json:"index"`
	NativeMinerAlpha       string `json:"native_miner_alpha"`
	ProviderAlpha          string `json:"provider_reference_alpha"`
	OwnerRecycleAlpha      string `json:"owner_recycle_reference_alpha"`
	NativeMinerTotalAlpha  string `json:"native_miner_total_alpha"`
	ProviderTotalAlpha     string `json:"provider_reference_total_alpha"`
	OwnerRecycleTotalAlpha string `json:"owner_recycle_reference_total_alpha"`
}

// Reference success never certifies a native outcome or authorizes activation.
type economicReference struct {
	Schema                         string                      `json:"schema"`
	InputHash                      string                      `json:"input_hash"`
	Units                          string                      `json:"units"`
	RemainderPolicy                string                      `json:"remainder_policy"`
	Intervals                      []economicReferenceInterval `json:"intervals"`
	ReserveCreditAlpha             string                      `json:"reserve_credit_alpha"`
	DeferredProviderLiabilityAlpha string                      `json:"deferred_provider_liability_alpha"`
	ActualNativeOutcomeVerified    bool                        `json:"actual_native_outcome_verified"`
	QuantizationToleranceKnown     bool                        `json:"quantization_tolerance_established"`
	ActivationReady                bool                        `json:"activation_ready"`
}

// Runtime AlphaBalance inputs fit u64; cumulative totals use arbitrary-precision
// integers so a long observation window cannot wrap at the per-interval width.
func calculateEconomicReference(input economicReferenceInput, inputHash string) (economicReference, error) {
	if input.Schema != economicReferenceInputSchema || len(input.NativeMinerAllocations) == 0 || len(input.NativeMinerAllocations) > maximumReferenceIntervals {
		return economicReference{}, errors.New("reference requires its exact schema and 1..10000 native miner interval amounts")
	}
	reference := economicReference{
		Schema: economicReferenceSchema, InputHash: inputHash, Units: "alpha-atomic", RemainderPolicy: "owner-recycle",
		Intervals:          make([]economicReferenceInterval, 0, len(input.NativeMinerAllocations)),
		ReserveCreditAlpha: "0", DeferredProviderLiabilityAlpha: "0",
		ActualNativeOutcomeVerified: false, QuantizationToleranceKnown: false, ActivationReady: false,
	}
	nativeTotal := new(big.Int)
	priorProviderTotal := new(big.Int)
	for index, encoded := range input.NativeMinerAllocations {
		amount, err := strconv.ParseUint(encoded, 10, 64)
		if err != nil || strconv.FormatUint(amount, 10) != encoded {
			return economicReference{}, fmt.Errorf("native interval %d is not a canonical u64 decimal alpha amount", index)
		}
		nativeAlpha := new(big.Int).SetUint64(amount)
		nativeTotal.Add(nativeTotal, nativeAlpha)
		providerTotal := new(big.Int).Quo(new(big.Int).Set(nativeTotal), big.NewInt(10))
		providerAlpha := new(big.Int).Sub(providerTotal, priorProviderTotal)
		reference.Intervals = append(reference.Intervals, economicReferenceInterval{
			Index: index, NativeMinerAlpha: encoded,
			ProviderAlpha: providerAlpha.String(), OwnerRecycleAlpha: new(big.Int).Sub(nativeAlpha, providerAlpha).String(),
			NativeMinerTotalAlpha: nativeTotal.String(), ProviderTotalAlpha: providerTotal.String(),
			OwnerRecycleTotalAlpha: new(big.Int).Sub(nativeTotal, providerTotal).String(),
		})
		priorProviderTotal.Set(providerTotal)
	}
	return reference, nil
}
