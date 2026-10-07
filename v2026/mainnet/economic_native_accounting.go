// One immutable window accounts for each original execution once. Cumulative
// references carry integer tenths across pages, while truncation, redirected
// emission and residual miner entitlement remain separate quantities.
package main

import (
	"errors"
	"math/big"
	"reflect"
)

type nativeExecutionWindow struct {
	Treasury               *nativeTreasuryAmounts   `json:"treasury_income,omitempty"`
	TreasuryReference      *string                  `json:"treasury_reference_alpha,omitempty"`
	TreasuryDeviation      *string                  `json:"treasury_deviation_alpha,omitempty"`
	From                   economicEmissionBoundary `json:"from_exclusive"`
	Through                economicEmissionBoundary `json:"through_inclusive"`
	Blocks                 uint64                   `json:"blocks"`
	EvidenceChain          string                   `json:"evidence_chain"`
	MinerAllocation        string                   `json:"miner_allocation_alpha"`
	ProviderEntitlement    string                   `json:"provider_entitlement_alpha"`
	OwnerRecycled          string                   `json:"owner_recycled_alpha"`
	ResidualEntitlement    string                   `json:"residual_entitlement_alpha"`
	CollateralCapture      string                   `json:"reward_collateral_capture_alpha"`
	RedirectedToValidators string                   `json:"redirected_to_validators_alpha"`
	AllocationDifference   string                   `json:"allocation_difference_alpha"`
	FixedPointDust         string                   `json:"fixed_point_dust_alpha"`
	FixedPointTolerance    string                   `json:"fixed_point_tolerance_alpha"`
	ProviderReference      string                   `json:"provider_reference_alpha"`
	OwnerRecycleReference  string                   `json:"owner_recycle_reference_alpha"`
	ProviderDeviation      string                   `json:"provider_deviation_alpha"`
	OwnerRecycleDeviation  string                   `json:"owner_recycle_deviation_alpha"`
	PolicyConformance      string                   `json:"policy_conformance"`
	ContentHash            string                   `json:"content_hash"`
}

func (self nativeExecutionWindow) hash() string { self.ContentHash = ""; return rootObjectHash(self) }

func nativeExecutionAdd(target *string, value string, signed bool) error {
	amount, ok := new(big.Int).SetString(value, 10)
	prior, priorOK := new(big.Int).SetString(*target, 10)
	if !ok || !priorOK || amount.String() != value || prior.String() != *target || !signed && (amount.Sign() < 0 || prior.Sign() < 0) || amount.BitLen() > 256 || prior.BitLen() > 256 {
		return errors.New("native execution accounting integer is invalid")
	}
	prior.Add(prior, amount)
	if prior.BitLen() > 256 {
		return errors.New("native execution cumulative integer exceeds bounded profile")
	}
	*target = prior.String()
	return nil
}

func nativeExecutionEmpty(from economicEmissionBoundary) nativeExecutionWindow {
	return nativeExecutionWindow{From: from, Through: from, EvidenceChain: rootObjectHash(from), MinerAllocation: "0", ProviderEntitlement: "0", OwnerRecycled: "0", ResidualEntitlement: "0", CollateralCapture: "0", RedirectedToValidators: "0", AllocationDifference: "0", FixedPointDust: "0", FixedPointTolerance: "0", ProviderReference: "0", OwnerRecycleReference: "0"}
}

func (self *nativeExecutionWindow) references() error {
	var amounts []*big.Int
	for _, raw := range []string{self.MinerAllocation, self.ProviderEntitlement, self.OwnerRecycled} {
		value, ok := new(big.Int).SetString(raw, 10)
		if !ok || value.String() != raw || value.Sign() < 0 || value.BitLen() > 256 {
			return errors.New("native reference input is not a bounded canonical amount")
		}
		amounts = append(amounts, value)
	}
	miner := amounts[0]
	provider := new(big.Int).Quo(miner, big.NewInt(10))
	owner := new(big.Int).Sub(miner, provider)
	self.ProviderReference = provider.String()
	self.OwnerRecycleReference = owner.String()
	self.ProviderDeviation = new(big.Int).Sub(amounts[1], provider).String()
	self.OwnerRecycleDeviation = new(big.Int).Sub(amounts[2], owner).String()
	self.TreasuryReference, self.TreasuryDeviation = nil, nil
	if self.Treasury != nil {
		if err := self.Treasury.validate(); err != nil {
			return err
		}
		gross, _ := new(big.Int).SetString(self.Treasury.Gross, 10)
		reference, deviation := owner.String(), new(big.Int).Sub(gross, owner).String()
		self.TreasuryReference, self.TreasuryDeviation = &reference, &deviation
		self.OwnerRecycleReference, self.OwnerRecycleDeviation = "0", self.OwnerRecycled
	}
	// Final I32F32 normalization loss alone is not the full governed tolerance
	// for preceding runtime quantization, fee, vault or independent Claim effects.
	self.PolicyConformance = "unresolved-full-runtime-tolerance-and-conservation"
	self.ContentHash = self.hash()
	return nil
}

func summarizeNativeExecution(observation economicEmissionObservation) (*nativeExecutionWindow, error) {
	window := nativeExecutionEmpty(observation.Policy.From)
	if observation.Policy.Execution != nil {
		window.Treasury = newNativeTreasuryAmounts(observation.Policy.Execution.Treasury)
	}
	for _, block := range observation.Blocks {
		outcome := block.ExecutionOutcome
		if outcome == nil || !outcome.AmountsAuthenticated || outcome.ContentHash != outcome.hash() || outcome.Boundary != block.Boundary || block.Boundary.Number != window.Through.Number+1 || block.Header.ParentHash != window.Through.Hash {
			return nil, errors.New("native window omitted or repeated an authenticated execution")
		}
		if outcome.RecipientEffects != nil {
			if err := outcome.RecipientEffects.validate(*outcome); err != nil {
				return nil, err
			}
		}
		if !sameNativeTreasuryAuthority(window.Treasury, outcome.Treasury) {
			return nil, errors.New("native execution window changed original treasury policy")
		}
		if err := addNativeTreasuryAmounts(&window.Treasury, outcome.Treasury); err != nil {
			return nil, err
		}
		for _, item := range []struct {
			target *string
			value  string
			signed bool
		}{
			{&window.MinerAllocation, outcome.MinerAllocation, false}, {&window.ProviderEntitlement, outcome.ProviderEntitlement, false}, {&window.OwnerRecycled, outcome.OwnerRecycled, false}, {&window.ResidualEntitlement, outcome.ResidualEntitlement, false}, {&window.CollateralCapture, outcome.CollateralCapture, false}, {&window.RedirectedToValidators, outcome.RedirectedToValidators, false}, {&window.AllocationDifference, outcome.AllocationDifference, true}, {&window.FixedPointDust, outcome.FixedPointDust, false}, {&window.FixedPointTolerance, outcome.FixedPointTolerance, false},
		} {
			if err := nativeExecutionAdd(item.target, item.value, item.signed); err != nil {
				return nil, err
			}
		}
		window.EvidenceChain = rootObjectHash([]string{window.EvidenceChain, outcome.ContentHash})
		window.Through, window.Blocks = block.Boundary, window.Blocks+1
	}
	if window.Through != observation.Policy.Through {
		return nil, errors.New("native execution window did not reach its original boundary")
	}
	if err := window.references(); err != nil {
		return nil, err
	}
	return &window, window.validate()
}

func (self nativeExecutionWindow) validate() error {
	if self.Through.Number <= self.From.Number || self.Blocks != self.Through.Number-self.From.Number || !rootCanonicalHash(self.From.Hash) || !rootCanonicalHash(self.Through.Hash) || !planSha256(self.EvidenceChain) || self.ContentHash != self.hash() {
		return errors.New("native execution accounting boundary or hash differs")
	}
	for _, value := range []string{self.MinerAllocation, self.ProviderEntitlement, self.OwnerRecycled, self.ResidualEntitlement, self.CollateralCapture, self.RedirectedToValidators, self.FixedPointDust, self.FixedPointTolerance, self.ProviderReference, self.OwnerRecycleReference} {
		zero := "0"
		if err := nativeExecutionAdd(&zero, value, false); err != nil {
			return err
		}
	}
	zero := "0"
	if err := nativeExecutionAdd(&zero, self.AllocationDifference, true); err != nil {
		return err
	}
	copy := self
	if err := copy.references(); err != nil {
		return err
	}
	if copy.ProviderReference != self.ProviderReference || copy.OwnerRecycleReference != self.OwnerRecycleReference || copy.ProviderDeviation != self.ProviderDeviation || copy.OwnerRecycleDeviation != self.OwnerRecycleDeviation || copy.PolicyConformance != self.PolicyConformance || !reflect.DeepEqual(copy.TreasuryReference, self.TreasuryReference) || !reflect.DeepEqual(copy.TreasuryDeviation, self.TreasuryDeviation) {
		return errors.New("native cumulative 10/90 reference did not carry exact rounding")
	}
	allocation := "0"
	for _, value := range []string{self.ProviderEntitlement, self.OwnerRecycled, self.ResidualEntitlement, self.AllocationDifference} {
		if err := nativeExecutionAdd(&allocation, value, true); err != nil {
			return err
		}
	}
	if self.Treasury != nil {
		// AllocationDifference is signed; a temporary negative subtotal does
		// not invalidate the separately authenticated nonnegative treasury mint.
		if err := nativeExecutionAdd(&allocation, self.Treasury.Gross, true); err != nil {
			return err
		}
	}
	if allocation != self.MinerAllocation {
		return errors.New("native recipient accounting does not conserve original miner tranche")
	}
	return nil
}

// Archive rollover keeps the full cumulative prefix even when active event
// pages become empty. No restart may lower a positive amount or replace the
// exact same-boundary evidence chain with a freshly constructed summary.
func nativeExecutionRetains(current, prior *nativeExecutionWindow) error {
	if current == nil || prior == nil {
		return errors.New("native retained execution window is absent")
	}
	if err := current.validate(); err != nil {
		return err
	}
	if err := prior.validate(); err != nil {
		return err
	}
	if current.From != prior.From || current.Through.Number < prior.Through.Number || current.Through.Number == prior.Through.Number && !reflect.DeepEqual(current, prior) {
		return errors.New("native archived execution predecessor was replaced")
	}
	if !sameNativeTreasuryAuthority(current.Treasury, prior.Treasury) {
		return errors.New("native archive changed treasury authority")
	}
	if current.Treasury != nil {
		for _, pair := range [][2]string{{current.Treasury.Gross, prior.Treasury.Gross}, {current.Treasury.Liquid, prior.Treasury.Liquid}, {current.Treasury.Collateral, prior.Treasury.Collateral}} {
			next, _ := new(big.Int).SetString(pair[0], 10)
			old, _ := new(big.Int).SetString(pair[1], 10)
			if next.Cmp(old) < 0 {
				return errors.New("native archive reduced treasury income")
			}
		}
	}
	for _, pair := range [][2]string{{current.MinerAllocation, prior.MinerAllocation}, {current.ProviderEntitlement, prior.ProviderEntitlement}, {current.OwnerRecycled, prior.OwnerRecycled}, {current.ResidualEntitlement, prior.ResidualEntitlement}, {current.CollateralCapture, prior.CollateralCapture}, {current.RedirectedToValidators, prior.RedirectedToValidators}, {current.FixedPointDust, prior.FixedPointDust}, {current.FixedPointTolerance, prior.FixedPointTolerance}} {
		next, nextOK := new(big.Int).SetString(pair[0], 10)
		old, oldOK := new(big.Int).SetString(pair[1], 10)
		if !nextOK || !oldOK || next.Cmp(old) < 0 {
			return errors.New("native archive reduced a retained cumulative amount")
		}
	}
	return nil
}

func appendNativeExecution(previous *nativeExecutionWindow, observation economicEmissionObservation, activation economicEmissionBoundary) (*nativeExecutionWindow, error) {
	window := observation.ExecutionWindow
	if window == nil {
		return nil, errors.New("native execution window is absent")
	}
	if err := window.validate(); err != nil {
		return nil, err
	}
	if previous == nil {
		if window.From != activation || observation.InitialState == nil || observation.InitialState.PendingServerAlpha != "0" {
			return nil, errors.New("native execution accounting lacks its original drained activation boundary")
		}
		value := *window
		value.Treasury = cloneNativeTreasuryAmounts(window.Treasury)
		return &value, nil
	}
	if err := previous.validate(); err != nil {
		return nil, err
	}
	if previous.Through != window.From || previous.From != activation {
		return nil, errors.New("native execution accounting skipped or repeated its retained predecessor")
	}
	if !sameNativeTreasuryAuthority(previous.Treasury, window.Treasury) {
		return nil, errors.New("native append changed treasury authority")
	}
	next := *previous
	next.Treasury = cloneNativeTreasuryAmounts(previous.Treasury)
	if err := addNativeTreasuryAmounts(&next.Treasury, window.Treasury); err != nil {
		return nil, err
	}
	for _, item := range []struct {
		target *string
		value  string
		signed bool
	}{
		{&next.MinerAllocation, window.MinerAllocation, false}, {&next.ProviderEntitlement, window.ProviderEntitlement, false}, {&next.OwnerRecycled, window.OwnerRecycled, false}, {&next.ResidualEntitlement, window.ResidualEntitlement, false}, {&next.CollateralCapture, window.CollateralCapture, false}, {&next.RedirectedToValidators, window.RedirectedToValidators, false}, {&next.AllocationDifference, window.AllocationDifference, true}, {&next.FixedPointDust, window.FixedPointDust, false}, {&next.FixedPointTolerance, window.FixedPointTolerance, false},
	} {
		if err := nativeExecutionAdd(item.target, item.value, item.signed); err != nil {
			return nil, err
		}
	}
	next.Blocks = window.Through.Number - next.From.Number
	next.Through = window.Through
	next.EvidenceChain = rootObjectHash([]string{previous.EvidenceChain, window.ContentHash})
	if err := next.references(); err != nil {
		return nil, err
	}
	return &next, next.validate()
}
