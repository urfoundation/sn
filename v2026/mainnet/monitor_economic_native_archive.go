// Rollover keeps every original checkpoint as an immutable bounded segment.
// Only acknowledged history moves; cursor, pending range, aggregate amounts,
// runtime reviews and the original observation authority never restart.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"

	"github.com/urfoundation/sn/v2026/protocol"
)

// The compact prefix is authenticated against every referenced checkpoint on
// reopen. It cannot introduce a financial fact independently of those bytes.
type monitorEconomicNativeArchive struct {
	ExecutionAccounting *nativeExecutionWindow    `json:"execution_accounting,omitempty"`
	Segments            []monitorHistoryReference `json:"segments"`
	Cursor              economicEmissionBoundary  `json:"cursor"`
	BatchCount          uint64                    `json:"batch_count"`
	BatchChainHash      string                    `json:"batch_chain_hash"`
	ObservedAlpha       string                    `json:"observed_alpha"`
	ObservedFeesRao     string                    `json:"observed_fees_rao"`
	Events              uint64                    `json:"events"`
}

func decodeMonitorEconomicNativeCheckpoint(raw []byte, policy monitorEconomicNativePolicy, contexts ...context.Context) (record monitorEconomicNativeCheckpoint, resultErr error) {
	if len(raw) == 0 || len(raw) > maxRpcReplyBytes {
		return record, errors.New("native checkpoint exceeds its byte bound")
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return record, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return record, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return record, errors.New("native economic checkpoint has trailing JSON")
	}
	if record.Schema != monitorEconomicNativeCheckpointSchema || record.PolicyHash != policy.identityHash() || record.ContentHash != record.hash() {
		return record, errors.New("native economic checkpoint differs from its retained policy or checksum")
	}
	if err := policy.retainsRuntimePolicy(record); err != nil {
		return record, err
	}
	if err := record.State.Catalog.validate(policy.HistoryCatalog, policy.Role, policy.identityHash(), ""); err != nil {
		return record, err
	}
	if err := record.State.admitRuntime(nativeRuntimeAdmissionContext(contexts), policy); err != nil {
		return record, err
	}
	return record, record.State.validate(policy)
}

func (self *monitorEconomicNativeArchive) validate(policy monitorEconomicNativePolicy, state *monitorEconomicNativeState) error {
	if self == nil {
		return nil
	}
	if policy.Observation.Execution == nil {
		if self.ExecutionAccounting != nil {
			return errors.New("legacy native archive acquired execution authority")
		}
	} else {
		if self.ExecutionAccounting == nil || state.ExecutionAccounting == nil || self.ExecutionAccounting.Through != self.Cursor {
			return errors.New("native archive lost its original execution accounting prefix")
		}
		if err := nativeExecutionRetains(state.ExecutionAccounting, self.ExecutionAccounting); err != nil {
			return err
		}
	}
	capacity := state.Catalog.capacity(policy.HistoryCatalog)
	if len(self.Segments) == 0 || uint64(len(self.Segments)) > capacity.Segments || uint64(len(self.Segments)) > capacity.HeldReaders || self.Cursor.Number <= policy.Observation.From.Number || self.Cursor.Number > state.Cursor.Number || !rootCanonicalHash(self.Cursor.Hash) || self.BatchCount == 0 || self.BatchCount > state.BatchCount || !planSha256(self.BatchChainHash) || self.Events == 0 {
		return errors.New("native economic archive lost its bounded original prefix")
	}
	if self.Cursor.Number == state.Cursor.Number && (self.Cursor != state.Cursor || self.BatchCount != state.BatchCount || self.BatchChainHash != state.BatchChainHash) {
		return errors.New("native archive boundary differs from active cursor")
	}
	seen := map[string]bool{}
	for _, reference := range self.Segments {
		if err := reference.validateLimit(policy.archiveReferenceBytes); err != nil {
			return err
		}
		if seen[reference.Path] || seen[reference.Path+".lock"] {
			return errors.New("native archive repeats or aliases an original segment")
		}
		seen[reference.Path], seen[reference.Path+".lock"] = true, true
	}
	for _, amount := range []string{self.ObservedAlpha, self.ObservedFeesRao} {
		if _, err := monitorEconomicInteger(amount); err != nil {
			return err
		}
	}
	if policy.HistoryCatalog != nil {
		raw, err := json.Marshal(struct {
			Archive *monitorEconomicNativeArchive `json:"archive"`
			Catalog *monitorHistoryCatalogState   `json:"catalog"`
		}{Archive: self, Catalog: state.Catalog})
		if err != nil || uint64(len(raw)) > capacity.CatalogBytes {
			return errors.Join(errors.New("native economic archive exceeds signed catalog metadata capacity"), err)
		}
	}
	return nil
}

// Rollover appends exactly one immutable prefix; it never discards earlier
// segments or changes a retained incomplete batch's original high-water.
func compactMonitorEconomicNative(record monitorEconomicNativeCheckpoint, reference monitorHistoryReference, policy monitorEconomicNativePolicy) (monitorEconomicNativeCheckpoint, error) {
	if err := reference.validateLimit(policy.archiveReferenceBytes); err != nil {
		return record, err
	}
	if err := record.State.validate(policy); err != nil {
		return record, err
	}
	state := record.State
	if len(state.History) == 0 {
		return record, errors.New("native archive has no acknowledged events to move")
	}
	archive := &monitorEconomicNativeArchive{Cursor: state.Cursor, BatchCount: state.BatchCount, BatchChainHash: state.BatchChainHash, ObservedAlpha: state.ObservedAlpha, ObservedFeesRao: state.ObservedFeesRao, Events: uint64(len(state.History))}
	if state.ExecutionAccounting != nil {
		value := *state.ExecutionAccounting
		value.Treasury = cloneNativeTreasuryAmounts(state.ExecutionAccounting.Treasury)
		archive.ExecutionAccounting = &value
	}
	if state.Archive != nil {
		archive.Segments = append([]monitorHistoryReference(nil), state.Archive.Segments...)
		if state.Archive.Events > math.MaxUint64-archive.Events {
			return record, errors.New("native archived event count overflows")
		}
		archive.Events += state.Archive.Events
	}
	archive.Segments = append(archive.Segments, reference)
	state.Archive, state.History = archive, []monitorEconomicNativeEvent{}
	state.CapacityRemaining, state.CapacityBytesRemaining = policy.HistoryEntries, maximumMonitorEconomicBytes-2
	// The retained archive preserves the previous capacity incident. No new
	// observation or time is invented when permitting the next bounded read.
	if state.Status == "capacity-held" {
		state.Status = "archive-ready"
	}
	if err := state.validate(policy); err != nil {
		return record, err
	}
	record.State = state
	record.ContentHash = record.hash()
	return record, nil
}

// Sequential admission keeps at most one segment payload resident. All shared
// descriptor leases remain held, so deletion or mutation refuses continuation.
func openMonitorEconomicNativeArchive(ctx context.Context, policy monitorEconomicNativePolicy, state *monitorEconomicNativeState) (owners []*monitorHistorySnapshot, resultErr error) {
	if state.Archive == nil {
		return nil, nil
	}
	if err := state.Archive.validate(policy, state); err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			for _, owner := range owners {
				resultErr = monitorAdmissionFailure(resultErr, owner.close())
			}
			owners = nil
		}
	}()
	var prior *monitorEconomicNativeArchive
	for _, reference := range state.Archive.Segments {
		owner, raw, err := openMonitorHistoryReader(ctx, reference)
		if err != nil {
			return owners, err
		}
		owners = append(owners, owner)
		record, err := decodeMonitorEconomicNativeCheckpoint(raw, policy, ctx)
		if err != nil {
			return owners, err
		}
		if !state.Catalog.retains(record.State.Catalog) {
			return owners, errors.New("native archive discarded a retained signed catalog revision")
		}
		if !reflect.DeepEqual(record.State.Archive, prior) {
			return owners, errors.New("native archive omitted or changed its complete predecessor chain")
		}
		next, err := compactMonitorEconomicNative(record, reference, policy)
		if err != nil {
			return owners, err
		}
		prior = next.State.Archive
	}
	if !reflect.DeepEqual(prior, state.Archive) {
		return owners, errors.New("native archive summary differs from exact retained checkpoints")
	}
	return owners, ctx.Err()
}

func (self *monitorEconomicNativeWorker) checkArchive() error {
	for _, owner := range self.archive {
		if err := owner.check(); err != nil {
			return err
		}
	}
	return nil
}

func (self *monitorEconomicNativeWorker) closeArchive() error {
	var result error
	for _, owner := range self.archive {
		result = errors.Join(result, owner.close())
	}
	self.archive = nil
	return result
}
