//go:build linux || darwin

package validator

// Borrowed state is used only while every live engine's writer token is held,
// or for a private unpublished candidate. No collection is copied to count it.

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/urnetwork/connect"
)

// These count-only map types have the wire representation of snapshotWithLock,
// not their native Go JSON representation. UUID keys and hex hashes have fixed
// encoded widths; no key strings, sorted key slice or provider copies are made.
type attemptSettlementV2BorrowedEMA map[connect.Id]float64
type attemptSettlementV2BorrowedPPM map[connect.Id]uint32
type attemptSettlementV2BorrowedWindow map[connect.Id]*ProviderWindow
type attemptSettlementV2BorrowedEgress map[connect.Id]map[[32]byte]bool

type attemptSettlementV2BorrowedSnapshot struct {
	Version int `json:"v"`
	SettlementEpoch *uint64 `json:"settlement_epoch,omitempty"`
	EgressGeneration uint64 `json:"egress_generation,omitempty"`
	AttemptLastAppliedSequence uint64 `json:"attempt_last_applied_sequence,omitempty"`
	AttemptSettlementFirstSequence uint64 `json:"attempt_settlement_first_sequence,omitempty"`
	AttemptEgressFirstSequence uint64 `json:"attempt_egress_first_sequence,omitempty"`
	SettlementTransition *AttemptSettlementTransition `json:"settlement_transition,omitempty"`
	AttemptV2 *attemptStatsV2State `json:"attempt_v2,omitempty"`
	Ema attemptSettlementV2BorrowedEMA `json:"ema"`
	EmaPPM attemptSettlementV2BorrowedPPM `json:"ema_ppm,omitempty"`
	Window attemptSettlementV2BorrowedWindow `json:"window"`
	Egress attemptSettlementV2BorrowedEgress `json:"egress,omitempty"`
}

// The short lock copies headers/scalars only. All live collection mutators
// (Record*, TakeEgress, Fold, commit, Load, detach/reconcile and publication)
// retain writer tokens. Owned helpers mutate detached candidates; getters do
// not expose mutable maps/windows. begin/abort may change control counters but
// never these collections. Callers must preserve this premise, not infer it
// merely from this shallow header snapshot. No observer runs during counting.
func borrowAttemptSettlementV2Snapshot(stats *StatsEngine) attemptSettlementV2BorrowedSnapshot {
	stats.mu.Lock()
	defer stats.mu.Unlock()
	version := 4
	if stats.attemptLedger != nil || stats.attemptLastAppliedSequence != 0 || stats.attemptSettlementFirstSequence != 0 || stats.attemptEgressFirstSequence != 0 { version = 5 }
	if stats.attemptV2 != nil { version = 6 }
	borrowed := attemptSettlementV2BorrowedSnapshot{
		Version: version, EgressGeneration: stats.egressGeneration,
		AttemptLastAppliedSequence: stats.attemptLastAppliedSequence,
		AttemptSettlementFirstSequence: stats.attemptSettlementFirstSequence,
		AttemptEgressFirstSequence: stats.attemptEgressFirstSequence,
		SettlementTransition: stats.settlementTransition, AttemptV2: stats.attemptV2,
		Ema: stats.ema, EmaPPM: stats.emaPPM, Window: stats.window, Egress: stats.egress,
	}
	if stats.settlementEpochKnown { epoch := stats.settlementEpoch; borrowed.SettlementEpoch = &epoch }
	return borrowed
}

// Counts the complete provider union without first allocating a union map.
// The reporting-float map also participates because clone/snapshot copies it.
func (self attemptSettlementV2BorrowedSnapshot) admitCensus(ctx context.Context, operator AttemptSettlementV2OperatorOptions) error {
	maxProviders, maxHashes := operator.Measurement.MaxProviders, operator.Measurement.MaxEgressHashes
	for _, size := range []int{len(self.Window), len(self.Ema), len(self.EmaPPM), len(self.Egress)} {
		if uint64(size) > maxProviders { return errors.New("compact borrowed provider census exceeds its bound before copying") }
	}
	var providers, hashes uint64
	add := func() error {
		if err := ctx.Err(); err != nil { return err }
		if providers >= maxProviders { return errors.New("compact borrowed provider union exceeds its bound before copying") }
		providers++
		return nil
	}
	for _, window := range self.Window {
		if window == nil { return errors.New("compact borrowed provider window is nil") }
		if err := add(); err != nil { return err }
	}
	for id := range self.Ema {
		if err := ctx.Err(); err != nil { return err }
		if _, present := self.Window[id]; !present { if err := add(); err != nil { return err } }
	}
	for id := range self.EmaPPM {
		if err := ctx.Err(); err != nil { return err }
		_, window := self.Window[id]; _, ema := self.Ema[id]
		if !window && !ema { if err := add(); err != nil { return err } }
	}
	for id, values := range self.Egress {
		if err := ctx.Err(); err != nil { return err }
		_, window := self.Window[id]; _, ema := self.Ema[id]; _, ppm := self.EmaPPM[id]
		if !window && !ema && !ppm { if err := add(); err != nil { return err } }
		if uint64(len(values)) > maxHashes-hashes { return errors.New("compact borrowed egress census exceeds its bound before copying") }
		hashes += uint64(len(values))
	}
	return ctx.Err()
}

// snapshotWithLock always produces nonnil EMA/window maps and [] hash values,
// even if their borrowed inputs are nil. Map order has no effect on byte size.
func (self *attemptSettlementV2JSONCount) borrowedMap(value reflect.Value, depth uint64, egress bool) error {
	if err := self.add(2); err != nil { return err }
	if uint64(value.Len()) > self.limit-self.used { return errors.New("compact borrowed map census exceeds its byte allowance") }
	iterator, count := value.MapRange(), 0
	for iterator.Next() {
		if count != 0 { if err := self.add(1); err != nil { return err } }
		if err := self.line(depth+1); err != nil { return err }
		colon := uint64(1); if self.indent { colon++ }
		if err := self.add(38+colon); err != nil { return err }
		if egress {
			hashes := iterator.Value()
			if err := self.add(2); err != nil { return err }
			if uint64(hashes.Len()) > (self.limit-self.used)/68 { return errors.New("compact borrowed hashes exceed their byte allowance") }
			for index := 0; index < hashes.Len(); index++ {
				if index != 0 { if err := self.add(1); err != nil { return err } }
				if err := self.line(depth+2); err != nil { return err }
				if err := self.add(68); err != nil { return err }
			}
			if hashes.Len() != 0 { if err := self.line(depth+1); err != nil { return err } }
		} else {
			if value.Type() == reflect.TypeFor[attemptSettlementV2BorrowedWindow]() && iterator.Value().IsNil() { return errors.New("compact borrowed provider window is nil") }
			if err := self.value(iterator.Value(), depth+1); err != nil { return err }
		}
		count++
	}
	if count != 0 { return self.line(depth) }
	return nil
}

// Admits the wire image before snapshotWithLock materializes strings/maps.
// The caller retains a live writer token or exclusively owns a private engine.
func countAttemptSettlementV2Engine(ctx context.Context, stats *StatsEngine, limit uint64) (uint64, error) {
	return countAttemptSettlementV2JSON(ctx, borrowAttemptSettlementV2Snapshot(stats), limit, true, true)
}

// Materialization uses the existing codec only after its full wire image fits.
func encodeAttemptSettlementV2Engine(ctx context.Context, stats *StatsEngine, limit uint64) ([]byte, error) {
	size, err := countAttemptSettlementV2Engine(ctx, stats, limit)
	if err != nil { return nil, err }
	encoded, err := encodeAttemptSettlementV2Stats(ctx, stats.snapshotStats(), limit)
	if err != nil { return nil, err }
	if uint64(len(encoded)) != size { return nil, errors.New("compact borrowed snapshot differs from its admitted exact size") }
	return encoded, nil
}

// Complete all-operator admission precedes the first clone. A per-call test
// observer can inspect the boundary only outside locks. Re-admit all borrowed
// inputs afterwards, then make every clone without any intervening callback.
func admitAttemptSettlementV2OwnedBorrowed(ctx context.Context, batch *attemptSettlementV2Owners, authority AttemptSettlementV2Options) error {
	for _, participant := range batch.ordered {
		borrowed := borrowAttemptSettlementV2Snapshot(participant.Stats)
		if err := borrowed.admitCensus(ctx, authority.Operators[participant.NoID]); err != nil { return fmt.Errorf("no_id %d: %w", participant.NoID, err) }
		if _, err := countAttemptSettlementV2JSON(ctx, borrowed, batch.persistence.MaxSnapshotBytes, true, true); err != nil { return fmt.Errorf("no_id %d borrowed snapshot: %w", participant.NoID, err) }
	}
	return ctx.Err()
}
