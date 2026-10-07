// The conservation owner retains every original allocation witness. Complete
// contiguous evidence may retire into authenticated original checkpoints;
// incomplete epochs remain hot and keep the full tolerance unknown.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

type economicConservationYumaEvidence struct {
	Projection nativeYumaProjection   `json:"original_allocation_witness"`
	Outcome    nativeExecutionOutcome `json:"original_native_outcome"`
}

type economicConservationYumaArchive struct {
	Blocks                    uint64                   `json:"complete_original_blocks"`
	Through                   economicEmissionBoundary `json:"through"`
	MinerDenominator          string                   `json:"complete_miner_denominator_alpha"`
	FullQuantizationTolerance string                   `json:"full_miner_quantization_tolerance_alpha"`
}

type economicConservationYumaSummary struct {
	Archived                  *economicConservationYumaArchive `json:"archived_original_allocations,omitempty"`
	Active                    []nativeYumaProjection           `json:"active_original_allocations"`
	Through                   economicEmissionBoundary         `json:"through"`
	Current                   bool                             `json:"complete_through_native_cursor"`
	MinerDenominator          *string                          `json:"complete_miner_denominator_alpha"`
	FullQuantizationTolerance *string                          `json:"full_miner_quantization_tolerance_alpha"`
	Authority                 string                           `json:"authority"`
}

// Admission reserves complete original witnesses before any engine or RPC is
// opened. The legacy one-MiB physical head is explicit: a larger source census
// needs a separately provisioned snapshot/history/restore profile, not a parser
// increase or a smaller UID sample. Other hot domains retain a separate reserve.
func (self economicConservationPolicy) validateYumaCapacity() error {
	authority := self.Native.Observation.Execution.Yuma
	if authority == nil {
		return nil
	}
	if err := authority.validate(); err != nil {
		return err
	}
	if authority.Workload != nil && authority.Workload.MaximumUids != uint64(self.Native.Observation.MaximumUids) {
		return errors.New("economic Yuma populated workload changed the complete original UID capacity")
	}
	// Every UID needs three original input record envelopes, a complete epoch
	// cell, allocation and recipient identities. This is a minimum feasibility
	// check; MaximumWitnessBytes is independently admitted and enforced on the
	// entire actual serialized companion, including additional stack/host bytes.
	minimum := uint64(self.Native.Observation.MaximumUids)*1024 + 4096
	if authority.MaximumWitnessBytes < minimum {
		return errors.New("economic Yuma witness byte authority cannot hold its complete admitted UID census")
	}
	bytes := authority.MaximumWitnessBytes * authority.HotBlockReserve
	facts := (1+(authority.MaximumWitnessBytes+31)/32)*authority.HotBlockReserve + 128
	if bytes+64*1024 > self.headBytes() || facts > self.MaximumFacts {
		return fmt.Errorf("economic Yuma complete witness reserve exceeds provisioned checkpoint profile: need %d bytes and %d facts, have %d bytes and %d facts; preserve full census and admit a complete physical head/history/restore profile", bytes+64*1024, facts, self.headBytes(), self.MaximumFacts)
	}
	return nil
}

// Full wire size includes byte-bearing epoch inputs and the original recipient
// outcome. Optional other companions are separately retained and charged.
func nativeYumaWitnessBytes(projection nativeYumaProjection, outcome nativeExecutionOutcome) (uint64, error) {
	outcome.Yuma, outcome.OpeningPrincipals, outcome.PrincipalEffects = nil, nil, nil
	outcome.CertifiedWindow = nil
	outcome.FeeCensus = nil
	raw, err := json.Marshal(economicConservationYumaEvidence{Projection: projection, Outcome: outcome})
	return uint64(len(raw)), err
}

// Charge the original byte-bearing captures, not only the number of epochs.
func (self economicConservationYumaEvidence) facts() uint64 {
	// Charge the full retained wire representation, including the original epoch,
	// complete recipient companion and exact rational denominator strings.
	raw, err := json.Marshal(self)
	if err != nil {
		return ^uint64(0)
	}
	return 1 + uint64(len(raw)+31)/32
}

func (self economicConservationState) yumaFacts() uint64 {
	count := uint64(0)
	for _, value := range self.Yuma {
		count += value.facts()
	}
	return count
}

func (self *economicConservationState) appendYuma(ctx context.Context, policy economicConservationPolicy, outcome nativeExecutionOutcome) error {
	if policy.Native.Observation.Execution.Yuma == nil {
		if outcome.Yuma != nil {
			return errors.New("economic Yuma witness appeared without original authority")
		}
		return nil
	}
	nativeYumaOwnerStep(ctx, "append")
	if err := outcome.Yuma.validate(ctx, policy.Native.Observation, outcome); err != nil {
		return err
	}
	value := economicConservationYumaEvidence{Projection: *outcome.Yuma, Outcome: outcome}
	value.Outcome.Yuma = nil
	value.Outcome.OpeningPrincipals = nil
	value.Outcome.CertifiedWindow = nil
	value.Outcome.FeeCensus = nil
	value.Outcome.PrincipalEffects = nil
	if self.facts()+value.facts() > policy.MaximumFacts {
		return errMonitorEconomicCapacity
	}
	self.Yuma = append(self.Yuma, value)
	return nil
}

func (self economicConservationState) validateYuma(ctx context.Context, policy economicConservationPolicy) error {
	var archived *economicConservationYumaArchive
	if self.Archive != nil {
		archived = self.Archive.Yuma
	}
	if policy.Native.Observation.Execution.Yuma == nil {
		if archived != nil || self.Yuma != nil {
			return errors.New("economic Yuma evidence acquired legacy authority")
		}
		return nil
	}
	previous := policy.Native.Observation.From
	if archived != nil {
		if archived.Blocks == 0 || archived.Through.Number <= previous.Number || archived.Through.Number-previous.Number != archived.Blocks || archived.Through.Number > self.Native.Cursor.Number || !rootCanonicalHash(archived.Through.Hash) {
			return errors.New("economic Yuma archived original interval changed")
		}
		if _, err := economicConservationSum(archived.MinerDenominator, archived.FullQuantizationTolerance); err != nil {
			return err
		}
		previous = archived.Through
	}
	for _, value := range self.Yuma {
		if value.Outcome.Yuma != nil || value.Outcome.OpeningPrincipals != nil || value.Outcome.PrincipalEffects != nil || value.Projection.Parent != previous {
			return errors.New("economic Yuma original execution census is not contiguous")
		}
		if err := value.Projection.validate(ctx, policy.Native.Observation, value.Outcome); err != nil {
			return err
		}
		if self.archiveView != nil && self.archiveView.yumaHashes[value.Projection.Boundary.Hash] != "" {
			return errors.New("economic Yuma repeats a retired original execution")
		}
		previous = value.Projection.Boundary
	}
	if previous != self.Native.Cursor {
		return errors.New("economic Yuma omitted an original executed block")
	}
	return nil
}

func mergeEconomicYumaArchive(archive *economicConservationYumaArchive, projection nativeYumaProjection) (*economicConservationYumaArchive, error) {
	if projection.Issue != "" || projection.MinerDenominator == nil || projection.FullQuantizationTolerance == nil {
		return nil, errors.New("economic Yuma retirement cannot discard an unclosed calculation")
	}
	next := &economicConservationYumaArchive{Blocks: 1, Through: projection.Boundary, MinerDenominator: *projection.MinerDenominator, FullQuantizationTolerance: *projection.FullQuantizationTolerance}
	if archive != nil {
		if archive.Through != projection.Parent {
			return nil, errors.New("economic Yuma retirement changed original predecessor")
		}
		next.Blocks += archive.Blocks
		var err error
		next.MinerDenominator, err = economicConservationSum(archive.MinerDenominator, next.MinerDenominator)
		if err != nil {
			return nil, err
		}
		next.FullQuantizationTolerance, err = economicConservationSum(archive.FullQuantizationTolerance, next.FullQuantizationTolerance)
		if err != nil {
			return nil, err
		}
	}
	return next, nil
}

func (self *economicConservationState) retireYuma() error {
	if self.Archive == nil {
		return errors.New("economic Yuma retirement requires an owned archive")
	}
	retired := 0
	for _, value := range self.Yuma {
		if value.Projection.Issue != "" || value.Projection.FullQuantizationTolerance == nil {
			break
		}
		next, err := mergeEconomicYumaArchive(self.Archive.Yuma, value.Projection)
		if err != nil {
			return err
		}
		self.Archive.Yuma = next
		retired++
	}
	if retired != 0 {
		self.Yuma = append([]economicConservationYumaEvidence{}, self.Yuma[retired:]...)
	}
	return nil
}

// The private index is rebuilt only from physically owned original checkpoints.
// A fabricated compact summary cannot authenticate itself during cold reopen.
func (self *economicConservationArchiveView) retainYuma(original, compacted *economicConservationState) error {
	if compacted.Archive == nil || compacted.Archive.Yuma == nil {
		return nil
	}
	for _, value := range original.Yuma {
		if value.Projection.Boundary.Number > compacted.Archive.Yuma.Through.Number {
			break
		}
		if self.yumaHashes[value.Projection.Boundary.Hash] != "" {
			return errors.New("economic Yuma archive repeats original allocation evidence")
		}
		next, err := mergeEconomicYumaArchive(self.yuma, value.Projection)
		if err != nil {
			return err
		}
		hash := value.Projection.ContentHash
		if err := self.charge(hash); err != nil {
			return err
		}
		self.yumaHashes[value.Projection.Boundary.Hash] = hash
		self.yuma = next
	}
	if !reflect.DeepEqual(self.yuma, compacted.Archive.Yuma) {
		return errors.New("economic Yuma archive summary differs from complete original evidence")
	}
	return nil
}

func (self economicConservationState) yumaSummary(ctx context.Context, policy economicConservationPolicy) (*economicConservationYumaSummary, error) {
	if policy.Native.Observation.Execution.Yuma == nil {
		return nil, nil
	}
	if err := self.validateYuma(ctx, policy); err != nil {
		return nil, err
	}
	result := &economicConservationYumaSummary{Through: policy.Native.Observation.From, Current: true, Active: []nativeYumaProjection{}, Authority: "independently-admitted-complete-original-allocation-arithmetic; vault-and-provider-policy-conformance-remain-separate"}
	denominator, tolerance := "0", "0"
	if self.Archive != nil && self.Archive.Yuma != nil {
		result.Archived = self.Archive.Yuma
		result.Through = result.Archived.Through
		denominator, tolerance = result.Archived.MinerDenominator, result.Archived.FullQuantizationTolerance
	}
	for _, value := range self.Yuma {
		projection := value.Projection
		result.Active = append(result.Active, projection)
		result.Through = projection.Boundary
		if projection.Issue != "" || projection.MinerDenominator == nil || projection.FullQuantizationTolerance == nil {
			result.Current = false
			continue
		}
		var err error
		denominator, err = economicConservationSum(denominator, *projection.MinerDenominator)
		if err != nil {
			return nil, err
		}
		tolerance, err = economicConservationSum(tolerance, *projection.FullQuantizationTolerance)
		if err != nil {
			return nil, err
		}
	}
	result.Current = result.Current && result.Through == self.Native.Cursor
	if result.Current {
		result.MinerDenominator = &denominator
		result.FullQuantizationTolerance = &tolerance
	}
	return result, nil
}
