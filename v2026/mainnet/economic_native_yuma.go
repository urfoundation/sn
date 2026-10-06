// A closed full-UID allocation projection is a companion to the original
// aggregate. Missing stage evidence remains explicit unknown and cannot be
// replaced by the older final-normalization tolerance or a provider subtotal.
package main

import (
	"context"
	"errors"
	"math/big"
	"reflect"
)

type nativeYumaAllocation struct {
	Uid                uint16 `json:"uid"`
	Hotkey             string `json:"hotkey"`
	Registered         uint64 `json:"registered_at"`
	ActualMiner        string `json:"actual_miner_alpha"`
	ActualValidator    string `json:"actual_validator_alpha"`
	ReferenceMiner     string `json:"dequantized_source_miner_alpha_rational"`
	ReferenceValidator string `json:"dequantized_source_validator_alpha_rational"`
	AbsoluteMinerError string `json:"absolute_miner_quantization_rational"`
}

// SourceReview fixes an algorithm family, not a runtime-number allowlist.
// The enclosing signed replay independently pins the actual code and callsites.
type nativeYumaProjection struct {
	Schema                    string                        `json:"schema"`
	Authority                 nativeYumaPolicy              `json:"authority"`
	Parent                    economicEmissionBoundary      `json:"parent"`
	Boundary                  economicEmissionBoundary      `json:"boundary"`
	AggregateHash             string                        `json:"aggregate_basis_hash"`
	AdmissionHash             string                        `json:"admission_hash"`
	JobHash                   string                        `json:"job_hash"`
	TraceHash                 string                        `json:"trace_hash"`
	Records                   []historicalReplayObservation `json:"original_input_observations"`
	Epoch                     *historicalReplayObservation  `json:"original_epoch_observation"`
	Issue                     string                        `json:"unknown_reason,omitempty"`
	Allocations               []nativeYumaAllocation        `json:"complete_uid_allocations,omitempty"`
	MinerDenominator          *string                       `json:"complete_miner_denominator_alpha"`
	FullQuantizationTolerance *string                       `json:"full_miner_quantization_tolerance_alpha"`
	ContentHash               string                        `json:"content_hash"`
}

func (self nativeYumaProjection) hash() string { self.ContentHash = ""; return rootObjectHash(self) }

// No-epoch blocks are explicit zero allocation only after the existing actual
// native replay kernel proved the absence of the original incentive event.
func deriveNativeYuma(ctx context.Context, policy economicEmissionPolicy, admission nativeExecutionAdmission, report *historicalReplayReport, outcome nativeExecutionOutcome) (*nativeYumaProjection, error) {
	if policy.Execution == nil || report == nil || report.HookObservations == nil {
		return nil, errors.New("native Yuma has no admitted original replay")
	}
	authority := policy.Execution.Yuma
	if !reflect.DeepEqual(authority, admission.Yuma) {
		return nil, errors.New("native Yuma input changed its signed complete algorithm authority")
	}
	if err := authority.validate(); err != nil {
		return nil, err
	}
	if authority == nil {
		return nil, nil
	}
	result := &nativeYumaProjection{Schema: nativeYumaSchema, Authority: *authority, Parent: admission.Parent, Boundary: outcome.Boundary, AggregateHash: nativeExecutionEffectBasis(outcome), AdmissionHash: outcome.AdmissionHash, JobHash: outcome.JobHash, TraceHash: outcome.TraceHash, Records: []historicalReplayObservation{}}
	for _, record := range report.HookObservations.Observations {
		if !historicalYumaPurpose(record.Purpose) && record.Purpose != "native-epoch" {
			continue
		}
		netuid, err := nativeCaptureUint(record, "netuid", 2)
		if err != nil {
			return nil, err
		}
		if netuid != uint64(policy.Netuid) {
			continue
		}
		if record.Purpose == "native-epoch" {
			if result.Epoch != nil {
				return nil, errors.New("native Yuma has multiple original target epochs")
			}
			copy := record
			result.Epoch = &copy
		} else {
			result.Records = append(result.Records, record)
		}
	}
	nativeYumaOwnerStep(ctx, "derive")
	if err := result.calculate(ctx, policy.Netuid, outcome); err != nil {
		return nil, err
	}
	result.ContentHash = result.hash()
	if bytes, err := nativeYumaWitnessBytes(*result, outcome); err != nil || bytes > authority.MaximumWitnessBytes {
		return nil, errors.Join(errMonitorEconomicCapacity, err, errors.New("native Yuma original witness exceeds its independently admitted retained byte capacity"))
	}
	return result, nil
}

// Unsupported/incomplete selected evidence is unknown. Cancellation propagates
// to the real owner; an arithmetic refusal never manufactures a wider bound.
func (self *nativeYumaProjection) calculate(ctx context.Context, netuid uint16, outcome nativeExecutionOutcome) error {
	if ctx == nil {
		return errors.New("native Yuma calculation has no lifecycle owner")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	self.Issue = ""
	self.Allocations = nil
	self.MinerDenominator = nil
	self.FullQuantizationTolerance = nil
	if self.Epoch == nil {
		if len(self.Records) != 0 || len(outcome.Recipients) != 0 || outcome.MinerAllocation != "0" || outcome.EpochInputs != nil {
			return errors.New("native Yuma missing original epoch contradicts native execution")
		}
		zero := "0"
		self.MinerDenominator = &zero
		self.FullQuantizationTolerance = &zero
		return nil
	}
	var total uint64
	if outcome.EpochInputs != nil {
		var err error
		total, err = nativeYumaJoinedEpochTotal(*self.Epoch, outcome.EpochInputs, outcome.Recipients)
		if err != nil {
			return err
		}
	}
	input, err := decodeNativeYuma(self.Authority, netuid, self.Boundary, self.Records, *self.Epoch, outcome.EpochInputs, outcome.Recipients)
	if err != nil {
		if errors.Is(err, errMonitorEconomicCapacity) {
			return err
		}
		self.Issue = err.Error()
		return nil
	}
	if outcome.EpochInputs == nil {
		total, err = nativeCaptureUint(*self.Epoch, "total-alpha", 8)
		if err != nil {
			self.Issue = err.Error()
			return nil
		}
	}
	fixed, err := evaluateNativeYuma(ctx, input, total, false, self.Authority.MaximumOperations)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, errMonitorEconomicCapacity) {
			return err
		}
		self.Issue = err.Error()
		return nil
	}
	// Close all amount-producing stages against the actual original memory,
	// including fallback validator allocation and the complete miner vector.
	stages := []struct {
		name     string
		values   []*big.Rat
		fraction uint
	}{
		{name: "stake-q32", values: fixed.stake, fraction: 32},
		{name: "active-stake-q32", values: fixed.active, fraction: 32},
		{name: "consensus-q32", values: fixed.consensus, fraction: 32},
		{name: "incentive-q32", values: fixed.incentive, fraction: 32},
		{name: "dividends-q32", values: fixed.dividend, fraction: 32},
		{name: "normalized-q32", values: fixed.server, fraction: 32},
		{name: "validator-normalized-q32", values: fixed.validator, fraction: 32},
		{name: "emission", values: fixed.serverAlpha},
		{name: "validator-emission", values: fixed.validatorAlpha},
	}
	for _, stage := range stages {
		actual, err := nativeCaptureVector(*self.Epoch, stage.name, int(input.Count))
		if err != nil {
			self.Issue = err.Error()
			return nil
		}
		for index, value := range stage.values {
			bits := new(big.Rat).Mul(value, new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), stage.fraction)))
			if !bits.IsInt() || !bits.Num().IsUint64() || bits.Num().Uint64() != actual[index] {
				self.Issue = "native Yuma original " + stage.name + " differs from complete fixed source calculation"
				return nil
			}
		}
	}
	if len(outcome.Recipients) != int(input.Count) {
		return errors.New("native Yuma denominator omitted an original recipient")
	}
	if err := outcome.RecipientEffects.validate(outcome); err != nil {
		return err
	}
	for index, node := range input.Nodes {
		recipient := outcome.Recipients[index]
		if recipient.Uid != node.Uid || recipient.Hotkey != node.Hotkey || recipient.Registered != node.Registered {
			return errors.New("native Yuma allocation changed original recipient generation")
		}
	}
	reference, err := evaluateNativeYuma(ctx, input, total, true, self.Authority.MaximumOperations-fixed.steps)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, errMonitorEconomicCapacity) {
			return err
		}
		self.Issue = err.Error()
		return nil
	}
	denominator, loss := new(big.Int), new(big.Rat)
	remaining := self.Authority.MaximumOperations - fixed.steps - reference.steps
	accumulator := &nativeYumaArithmetic{context: ctx, exact: true, maximum: remaining}
	for index, node := range input.Nodes {
		if !accumulator.check(loss, fixed.serverAlpha[index], reference.serverAlpha[index]) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return accumulator.err
		}
		amount := fixed.serverAlpha[index].Num()
		denominator.Add(denominator, amount)
		difference := new(big.Rat).Sub(fixed.serverAlpha[index], reference.serverAlpha[index])
		difference.Abs(difference)
		loss.Add(loss, difference)
		if !accumulator.check(loss) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return accumulator.err
		}
		self.Allocations = append(self.Allocations, nativeYumaAllocation{Uid: node.Uid, Hotkey: node.Hotkey, Registered: node.Registered, ActualMiner: amount.String(), ActualValidator: fixed.validatorAlpha[index].Num().String(), ReferenceMiner: reference.serverAlpha[index].RatString(), ReferenceValidator: reference.validatorAlpha[index].RatString(), AbsoluteMinerError: difference.RatString()})
	}
	treasury := "0"
	if outcome.Treasury != nil {
		treasury = outcome.Treasury.Gross
	}
	amounts, err := economicConservationSum(outcome.ProviderEntitlement, outcome.OwnerRecycled, outcome.ResidualEntitlement, treasury)
	if err != nil || amounts != denominator.String() {
		return errors.New("native Yuma full denominator contradicts original recipient effects")
	}
	bound, remainder := new(big.Int).QuoRem(loss.Num(), loss.Denom(), new(big.Int))
	if remainder.Sign() != 0 {
		bound.Add(bound, big.NewInt(1))
	}
	denominatorText, boundText := denominator.String(), bound.String()
	self.MinerDenominator = &denominatorText
	self.FullQuantizationTolerance = &boundText
	return nil
}

// Original checkpoint custody is still required by consumers. Recalculation
// prevents a summary or self-sealed companion from inventing a tolerance.
func (self *nativeYumaProjection) validate(ctx context.Context, policy economicEmissionPolicy, outcome nativeExecutionOutcome) error {
	if self == nil || policy.Execution == nil || policy.Execution.Yuma == nil || !reflect.DeepEqual(&self.Authority, policy.Execution.Yuma) || self.Schema != nativeYumaSchema || self.ContentHash != self.hash() || self.Parent.Number+1 != self.Boundary.Number || !rootCanonicalHash(self.Parent.Hash) || self.Boundary != outcome.Boundary || self.AggregateHash != nativeExecutionEffectBasis(outcome) || self.AdmissionHash != outcome.AdmissionHash || self.JobHash != outcome.JobHash || self.TraceHash != outcome.TraceHash || !outcome.AmountsAuthenticated || outcome.ContentHash != outcome.hash() {
		return errors.New("native Yuma companion differs from admitted original allocation")
	}
	if err := self.Authority.validate(); err != nil {
		return err
	}
	if bytes, err := nativeYumaWitnessBytes(*self, outcome); err != nil || bytes > self.Authority.MaximumWitnessBytes {
		return errors.Join(errMonitorEconomicCapacity, err, errors.New("native Yuma retained original witness exceeds its admitted byte capacity"))
	}
	copy := *self
	nativeYumaOwnerStep(ctx, "validate")
	if err := copy.calculate(ctx, policy.Netuid, outcome); err != nil {
		return err
	}
	if !reflect.DeepEqual(copy, *self) {
		return errors.New("native Yuma companion changed its original complete calculation")
	}
	return nil
}
