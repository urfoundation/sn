// Exact original checkpoints are retained before compacting active event and
// fee history. Contract credit/carry and the pending range remain in the head.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"reflect"
)

type monitorEconomicEvmArchive struct {
	Segments       []monitorHistoryReference  `json:"segments"`
	Cursor         economicEmissionBoundary   `json:"cursor"`
	BatchCount     uint64                     `json:"batch_count"`
	BatchChainHash string                     `json:"batch_chain_hash"`
	Snapshot       monitorEconomicEvmSnapshot `json:"snapshot"`
	Events         uint64                     `json:"events"`
	Fees           uint64                     `json:"fees"`
	FeeWei         string                     `json:"fee_wei"`
}

// The cumulative census remains in the active head when entries move to an
// archive, so omission of an archived prefix cannot resemble a quiet interval.
type monitorEvmRetainedCounts struct {
	Events uint64 `json:"events"`
	Fees   uint64 `json:"fees"`
}

func (self *monitorEconomicEvmState) retainedCounts() (monitorEvmRetainedCounts, error) {
	counts := monitorEvmRetainedCounts{Events: uint64(len(self.History)), Fees: uint64(len(self.Fees))}
	if self.Archive != nil {
		if counts.Events > math.MaxUint64-self.Archive.Events || counts.Fees > math.MaxUint64-self.Archive.Fees {
			return counts, errors.New("EVM retained observation census overflows")
		}
		counts.Events += self.Archive.Events
		counts.Fees += self.Archive.Fees
	}
	return counts, nil
}

// Validate each original head against its acknowledged resource basis. A later
// reviewed increase does not rewrite the checksum or prior resource reviews.
func decodeMonitorEconomicEvmCheckpoint(raw []byte, policy monitorEconomicEvmPolicy) (record monitorEconomicEvmCheckpoint, resultErr error) {
	if len(raw) == 0 || len(raw) > maxRpcReplyBytes {
		return record, errors.New("EVM checkpoint exceeds its byte bound")
	}
	if err := decodeMonitorHistoryInput(raw, &record); err != nil {
		return record, err
	}
	if record.Schema != monitorEconomicEvmCheckpointSchema || record.PolicyHash != policy.identityHash() || record.ContentHash != record.hash() {
		return record, errors.New("EVM economic checkpoint differs from its retained policy or checksum")
	}
	validation, err := record.validationPolicy(policy)
	if err != nil {
		return record, err
	}
	if err := record.State.Catalog.validate(policy.HistoryCatalog, policy.Role, policy.identityHash(), ""); err != nil {
		return record, err
	}
	if _, err := record.retainedResourceHistory(policy, validation.resources()); err != nil {
		return record, err
	}
	return record, record.State.validate(validation)
}

func (self monitorEconomicEvmCheckpoint) validationPolicy(policy monitorEconomicEvmPolicy) (monitorEconomicEvmPolicy, error) {
	if self.Resources != nil {
		if self.Resources.validate() != nil || !policy.resources().includes(*self.Resources) || self.ResourceReviewSha256 != "" && !planSha256(self.ResourceReviewSha256) {
			return policy, errors.New("EVM economic renewal shrank acknowledged resources or changed their review")
		}
		policy.setResources(*self.Resources)
	} else if policy.ResourceRevision != nil {
		policy.setResources(policy.ResourceRevision.Original)
	}
	return policy, nil
}

func (self *monitorEconomicEvmArchive) validate(policy monitorEconomicEvmPolicy, state *monitorEconomicEvmState) error {
	if self == nil {
		return nil
	}
	capacity := state.Catalog.capacity(policy.HistoryCatalog)
	if len(self.Segments) == 0 || uint64(len(self.Segments)) > capacity.Segments || uint64(len(self.Segments)) > capacity.HeldReaders || self.Cursor.Number <= policy.From.Number || self.Cursor.Number > state.Cursor.Number || !rootCanonicalHash(self.Cursor.Hash) || self.BatchCount == 0 || self.BatchCount > state.BatchCount || !planSha256(self.BatchChainHash) || self.Events == 0 && self.Fees == 0 {
		return errors.New("EVM archive lost its bounded original prefix")
	}
	if self.Cursor.Number == state.Cursor.Number && (self.Cursor != state.Cursor || self.BatchCount != state.BatchCount || self.BatchChainHash != state.BatchChainHash || state.Snapshot == nil || !reflect.DeepEqual(self.Snapshot, *state.Snapshot)) {
		return errors.New("EVM archive boundary differs from active cursor or retained credit")
	}
	if err := self.Snapshot.conservation(policy); err != nil {
		return err
	}
	if _, err := monitorEconomicInteger(self.FeeWei); err != nil {
		return err
	}
	if len(policy.FeePayers) == 0 && (self.Fees != 0 || self.FeeWei != "0") || self.Fees == 0 && self.FeeWei != "0" {
		return errors.New("EVM archive invented fee observations")
	}
	seen := map[string]bool{}
	for _, reference := range self.Segments {
		if err := reference.validateLimit(policy.archiveReferenceBytes); err != nil {
			return err
		}
		if seen[reference.Path] || seen[reference.Path+".lock"] {
			return errors.New("EVM archive repeats or aliases an original segment")
		}
		seen[reference.Path], seen[reference.Path+".lock"] = true, true
	}
	if policy.HistoryCatalog != nil {
		raw, err := monitorEvmCatalogBytes(*state)
		if err != nil || uint64(len(raw)) > capacity.CatalogBytes {
			return errors.Join(errors.New("EVM archive exceeds signed catalog metadata capacity"), err)
		}
	}
	return nil
}

// This total is receipt-derived cost, not native withdrawal/refund authority.
func (self *monitorEconomicEvmState) feeTotal() *big.Int {
	total := new(big.Int)
	if self.Archive != nil {
		total.SetString(self.Archive.FeeWei, 10)
	}
	for _, fee := range self.Fees {
		amount, _ := monitorEconomicInteger(fee.FeeWei)
		total.Add(total, amount)
	}
	return total
}

func compactMonitorEconomicEvm(record monitorEconomicEvmCheckpoint, reference monitorHistoryReference, policy monitorEconomicEvmPolicy) (monitorEconomicEvmCheckpoint, error) {
	if err := reference.validateLimit(policy.archiveReferenceBytes); err != nil {
		return record, err
	}
	validation, err := record.validationPolicy(policy)
	if err != nil {
		return record, err
	}
	if err := record.State.validate(validation); err != nil {
		return record, err
	}
	state := record.State
	if len(state.History)+len(state.Fees) == 0 || state.Snapshot == nil {
		return record, errors.New("EVM archive has no acknowledged history to move")
	}
	archive := &monitorEconomicEvmArchive{Cursor: state.Cursor, BatchCount: state.BatchCount, BatchChainHash: state.BatchChainHash, Snapshot: state.Snapshot.clone(), Events: uint64(len(state.History)), Fees: uint64(len(state.Fees)), FeeWei: state.feeTotal().String()}
	if state.Archive != nil {
		archive.Segments = append([]monitorHistoryReference(nil), state.Archive.Segments...)
		if archive.Events > math.MaxUint64-state.Archive.Events || archive.Fees > math.MaxUint64-state.Archive.Fees {
			return record, errors.New("EVM archived observation count overflows")
		}
		archive.Events += state.Archive.Events
		archive.Fees += state.Archive.Fees
	}
	archive.Segments = append(archive.Segments, reference)
	state.Archive, state.History, state.Fees = archive, []monitorEconomicEvmEvent{}, []monitorEconomicEvmFee{}
	counts, err := state.retainedCounts()
	if err != nil {
		return record, err
	}
	state.Retained = &counts
	state.CapacityRemaining = validation.HistoryEntries
	if state.Status == "capacity-held" {
		state.Status = "archive-ready"
	}
	if err := state.validate(validation); err != nil {
		return record, err
	}
	// A latest-only legacy receipt is imported using its ORIGINAL checksum,
	// before the compact publication acquires a different content hash.
	history, err := record.retainedResourceHistory(policy, validation.resources())
	if err != nil {
		return record, err
	}
	record.ResourceHistory, record.State = history, state
	record.ContentHash = record.hash()
	return record, nil
}

func monitorEvmReviewsRetain(later, prior *monitorEvmResourceHistory) bool {
	return later != nil && prior != nil && later.LegacyCheckpointSha256 == prior.LegacyCheckpointSha256 && len(later.Entries) >= len(prior.Entries) && reflect.DeepEqual(later.Entries[:len(prior.Entries)], prior.Entries)
}

// At most one archived payload is resident during admission. Held SH owners
// then enforce original named custody without replaying or rereading history.
func openMonitorEconomicEvmArchive(ctx context.Context, policy monitorEconomicEvmPolicy, record monitorEconomicEvmCheckpoint) (owners []*monitorHistorySnapshot, resultErr error) {
	state := &record.State
	if state.Archive == nil {
		return nil, nil
	}
	if err := state.Archive.validate(policy, state); err != nil {
		return nil, err
	}
	validation, err := record.validationPolicy(policy)
	if err != nil {
		return nil, err
	}
	currentReviews, err := record.retainedResourceHistory(policy, validation.resources())
	if err != nil {
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
	var prior *monitorEconomicEvmArchive
	var priorReviews *monitorEvmResourceHistory
	for _, reference := range state.Archive.Segments {
		owner, raw, err := openMonitorHistoryReader(ctx, reference)
		if err != nil {
			return owners, err
		}
		owners = append(owners, owner)
		original, err := decodeMonitorEconomicEvmCheckpoint(raw, policy)
		if err != nil {
			return owners, err
		}
		if !state.Catalog.retains(original.State.Catalog) || !reflect.DeepEqual(original.State.Archive, prior) {
			return owners, errors.New("EVM archive omitted its predecessor or signed catalog history")
		}
		next, err := compactMonitorEconomicEvm(original, reference, policy)
		if err != nil {
			return owners, err
		}
		if !monitorEvmReviewsRetain(currentReviews, next.ResourceHistory) || priorReviews != nil && !monitorEvmReviewsRetain(next.ResourceHistory, priorReviews) {
			return owners, errors.New("EVM archive lost acknowledged resource review provenance")
		}
		prior, priorReviews = next.State.Archive, next.ResourceHistory
	}
	if !reflect.DeepEqual(prior, state.Archive) {
		return owners, errors.New("EVM archive summary differs from exact original checkpoints")
	}
	return owners, ctx.Err()
}

func (self *monitorEconomicEvmWorker) checkArchive() error {
	for _, owner := range self.archive {
		if err := owner.check(); err != nil {
			return err
		}
	}
	return nil
}

func (self *monitorEconomicEvmWorker) closeArchive() error {
	var result error
	for _, owner := range self.archive {
		result = errors.Join(result, owner.close())
	}
	self.archive = nil
	return result
}

func monitorEvmCatalogBytes(state monitorEconomicEvmState) ([]byte, error) {
	return json.Marshal(struct {
		Archive *monitorEconomicEvmArchive  `json:"archive"`
		Catalog *monitorHistoryCatalogState `json:"catalog"`
	}{Archive: state.Archive, Catalog: state.Catalog})
}
