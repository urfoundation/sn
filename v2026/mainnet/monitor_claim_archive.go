// Claim archives move exact evidence bytes, never the ownership of an unpaid
// obligation. Admission hydrates every expected epoch once; hot samples reuse
// that bounded state and check held archive custody without rereading payloads.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

type monitorClaimArchivedEpoch struct {
	Epoch       int64  `json:"epoch"`
	StateSha256 string `json:"state_sha256"`
}

// Epoch commitments refer to the latest original checkpoint, whose complete
// prefix remains in Segments. A compact placeholder is not unknown evidence.
type monitorClaimArchiveBoundary struct {
	HighWaterAt        time.Time `json:"high_water_at"`
	LastAcceptedAt     time.Time `json:"last_accepted_at"`
	SemanticProgressAt time.Time `json:"semantic_progress_at"`
	Incidents          uint64    `json:"incidents"`
	Restarts           uint64    `json:"restarts"`
	InstanceId         string    `json:"instance_id"`
	Sequence           uint64    `json:"sequence"`
	StartedAt          string    `json:"started_at"`
	PublishedAt        string    `json:"published_at"`
	RecordSha256       string    `json:"record_sha256"`
}

type monitorClaimArchive struct {
	Boundary monitorClaimArchiveBoundary `json:"boundary"`
	Segments []monitorHistoryReference   `json:"segments"`
	Epochs   []monitorClaimArchivedEpoch `json:"epochs"`
}

type monitorClaimArchiveAdmission struct {
	owners        []*monitorHistorySnapshot
	epochStateKVs map[int64]monitorClaimEpochState
	commitments   *monitorClaimEpochCommitments
	windows       *monitorClaimWindowAdmission
	references    []monitorHistoryReference
	work          func(string, uint64)
}

// Admission already checked the ordered, duplicate-free epoch census. Keep
// its immutable commitments once; every later save performs one lookup per
// live epoch. The optional observer counts actual lookups without supplying a
// verdict or replacing the original content hash.
type monitorClaimEpochCommitments struct {
	ordered []monitorClaimArchivedEpoch
	hashes  map[int64]string
	visit   func()
}

func newMonitorClaimEpochCommitments(archive *monitorClaimArchive) *monitorClaimEpochCommitments {
	if archive == nil {
		return nil
	}
	index := &monitorClaimEpochCommitments{ordered: append([]monitorClaimArchivedEpoch(nil), archive.Epochs...), hashes: make(map[int64]string, len(archive.Epochs))}
	for _, epoch := range index.ordered {
		index.hashes[epoch.Epoch] = epoch.StateSha256
	}
	return index
}

func (self *monitorClaimEpochCommitments) matches(epoch monitorClaimEpochState) bool {
	if self == nil {
		return false
	}
	if self.visit != nil {
		self.visit()
	}
	hash, present := self.hashes[epoch.Epoch]
	return present && rootObjectHash(epoch) == hash
}

func (self *monitorClaimArchiveAdmission) externalize(record monitorClaimCheckpointRecord) monitorClaimCheckpointRecord {
	if self == nil {
		return record
	}
	return externalizeMonitorClaimRecord(record, self.commitments)
}

func (self monitorClaimPolicy) archiveIdentityHash() string {
	if self.Window != nil {
		return self.Window.OriginalPolicyHash
	}
	resources := self.resources()
	if self.Renewal != nil {
		resources = self.Renewal.Original
	}
	return "sha256:" + self.policyHashAt(resources)
}

func cloneMonitorClaimEpoch(value monitorClaimEpochState) monitorClaimEpochState {
	value.Observation = cloneMonitorClaimObservation(value.Observation)
	value.Proof = cloneMonitorClaimObservation(value.Proof)
	return value
}

func monitorClaimEpochPlaceholder(value monitorClaimEpochState) bool {
	return value.Archived && value.Observation == nil && value.Proof == nil && value.FirstSeenAt.IsZero() && value.ProgressAt.IsZero()
}

// Original grammar remains byte-for-byte hash compatible when both new fields
// are absent. Archive and policy validation precede hydration of placeholders.
func decodeMonitorClaimCheckpoint(raw []byte, policy monitorClaimPolicy) (record monitorClaimCheckpointRecord, resultErr error) {
	if len(raw) == 0 || len(raw) > maxMonitorClaimCheckpointBytes {
		return record, errors.New("claim checkpoint exceeds its byte bound")
	}
	if err := decodeMonitorHistoryInput(raw, &record); err != nil {
		return record, err
	}
	hash, err := hashMonitorClaimCheckpoint(record)
	if err != nil {
		return record, err
	}
	if record.Schema != monitorClaimCheckpointSchema || record.ContentHash != hash {
		return record, errors.New("claim checkpoint differs from its checksum")
	}
	history, err := record.retainedClaimPolicyHistory(policy)
	if err != nil {
		return record, err
	}
	if err := record.Window.validate(policy, history); err != nil {
		return record, err
	}
	if err := record.Catalog.validate(policy.HistoryCatalog, policy.Role, policy.archiveIdentityHash(), ""); err != nil {
		return record, err
	}
	if policy.HistoryCatalog != nil {
		if err := monitorClaimCatalogHeadBudget(record, policy, record.Catalog.capacity(policy.HistoryCatalog)); err != nil {
			return record, err
		}
	}
	return record, record.Archive.validate(policy, record)
}

func (self monitorClaimCheckpointRecord) retainedClaimPolicyHistory(policy monitorClaimPolicy) (*monitorProgressPolicyHistory, error) {
	history := self.PolicyHistory
	if history == nil {
		original := policy.resources()
		if policy.Renewal != nil {
			original = policy.Renewal.Original
		}
		if self.PolicyHash != policy.policyHashAt(original) {
			return nil, errors.New("claim original policy differs from independently retained expectations")
		}
		history = newMonitorProgressPolicyHistory(original, self.PolicyHash, self.ContentHash)
	}
	if err := history.validate(true, self.PolicyHash, policy.policyHashAt); err != nil {
		return nil, err
	}
	if !policy.resources().includes(history.Entries[len(history.Entries)-1].Resources) {
		return nil, errors.New("claim current policy shrank its retained census or resources")
	}
	return history, nil
}

func monitorClaimReviewsRetain(later, prior *monitorProgressPolicyHistory) bool {
	return later != nil && prior != nil && later.LegacyCheckpointSha256 == prior.LegacyCheckpointSha256 && len(later.Entries) >= len(prior.Entries) && reflect.DeepEqual(later.Entries[:len(prior.Entries)], prior.Entries)
}

func (self *monitorClaimArchive) validate(policy monitorClaimPolicy, record monitorClaimCheckpointRecord) error {
	if self == nil {
		return nil
	}
	capacity := record.Catalog.capacity(policy.HistoryCatalog)
	boundary := self.Boundary
	if boundary.HighWaterAt.IsZero() || boundary.LastAcceptedAt.IsZero() || boundary.Sequence == 0 || !planSha256(boundary.RecordSha256) || record.State.Record == nil || record.State.HighWaterAt.Before(boundary.HighWaterAt) || record.State.LastAcceptedAt.Before(boundary.LastAcceptedAt) || record.State.SemanticProgressAt.Before(boundary.SemanticProgressAt) || record.State.Incidents < boundary.Incidents || record.State.Restarts < boundary.Restarts {
		return errors.New("claim archive lost retained observation progress or incident census")
	}
	current := record.State.Record
	priorStarted, startErr := time.Parse(time.RFC3339Nano, boundary.StartedAt)
	priorPublished, publishedErr := time.Parse(time.RFC3339Nano, boundary.PublishedAt)
	currentStarted, currentStartErr := time.Parse(time.RFC3339Nano, current.StartedAt)
	currentPublished, currentPublishedErr := time.Parse(time.RFC3339Nano, current.PublishedAt)
	if errors.Join(startErr, publishedErr, currentStartErr, currentPublishedErr) != nil || currentPublished.Before(priorPublished) {
		return errors.New("claim archive lost original producer publication boundary")
	}
	if current.InstanceId == boundary.InstanceId {
		if current.StartedAt != boundary.StartedAt || current.Sequence < boundary.Sequence || current.Sequence == boundary.Sequence && rootObjectHash(current) != boundary.RecordSha256 {
			return errors.New("claim archive reset or changed retained producer sequence")
		}
	} else if !currentStarted.After(priorStarted) || record.State.Restarts <= boundary.Restarts {
		return errors.New("claim archive lost acknowledged producer restart continuity")
	}
	if len(self.Segments) == 0 || uint64(len(self.Segments)) > capacity.Segments || uint64(len(self.Segments)) > capacity.HeldReaders || len(self.Epochs) == 0 || len(self.Epochs) > len(record.State.Epochs) {
		return errors.New("claim archive lacks a bounded original evidence census")
	}
	seenPaths := map[string]bool{}
	for _, reference := range self.Segments {
		if err := reference.validate(); err != nil {
			return err
		}
		if seenPaths[reference.Path] || seenPaths[reference.Path+".lock"] {
			return errors.New("claim archive repeats or aliases original segment custody")
		}
		seenPaths[reference.Path], seenPaths[reference.Path+".lock"] = true, true
	}
	position := 0
	for _, epoch := range self.Epochs {
		for position < len(record.State.Epochs) && record.State.Epochs[position].Epoch != epoch.Epoch {
			position++
		}
		if position == len(record.State.Epochs) || !planSha256(epoch.StateSha256) {
			return errors.New("claim archive changed the ordered expected epoch census")
		}
		position++
	}
	raw, err := monitorClaimCatalogBytes(record)
	if err != nil {
		return err
	}
	if uint64(len(raw)) > capacity.CatalogBytes {
		return errors.New("claim archive exceeds retained catalog metadata capacity")
	}
	return nil
}

func monitorClaimCatalogBytes(record monitorClaimCheckpointRecord) ([]byte, error) {
	return json.Marshal(struct {
		Archive *monitorClaimArchive        `json:"archive"`
		Catalog *monitorHistoryCatalogState `json:"catalog"`
		Window  *monitorClaimWindowState    `json:"window,omitempty"`
	}{Archive: record.Archive, Catalog: record.Catalog, Window: record.Window})
}

// The caller owns these clones. A later HTTP sample cannot mutate the admitted
// original basis that decides which exact evidence can remain externalized.
func hydrateMonitorClaimRecord(record monitorClaimCheckpointRecord, basis map[int64]monitorClaimEpochState, policy monitorClaimPolicy) (monitorClaimCheckpointRecord, error) {
	record.State.Epochs = append([]monitorClaimEpochState(nil), record.State.Epochs...)
	references := map[int64]string{}
	if record.Archive != nil {
		for _, reference := range record.Archive.Epochs {
			policy.observeWork("hydrate-commitment", 1)
			references[reference.Epoch] = reference.StateSha256
			original, ok := basis[reference.Epoch]
			if !ok || rootObjectHash(original) != reference.StateSha256 {
				return record, errors.New("claim archive epoch commitment lacks its exact original evidence")
			}
		}
	}
	for index, value := range record.State.Epochs {
		policy.observeWork("hydrate-epoch", 1)
		original, retained := basis[value.Epoch]
		if value.Archived {
			if !monitorClaimEpochPlaceholder(value) || !retained || references[value.Epoch] == "" {
				return record, errors.New("claim compact epoch lost its exact retained archive")
			}
			record.State.Epochs[index] = cloneMonitorClaimEpoch(original)
		} else if retained && !monitorClaimEpochRetains(value, original) {
			return record, errors.New("claim active epoch contradicted or erased archived evidence")
		}
	}
	history, err := record.retainedClaimPolicyHistory(policy)
	if err != nil {
		return record, err
	}
	validation := policy.atResources(history.Entries[len(history.Entries)-1].Resources)
	if err := monitorClaimActiveBudget(record.State, policy); err != nil {
		return record, err
	}
	return record, validateMonitorClaimState(validation, record.State)
}

// A signed receipt's payment fields describe that ORIGINAL receipt, not later
// aggregate credit activity. Their authority and the first proof stay immutable.
func monitorClaimEpochRetains(current, prior monitorClaimEpochState) bool {
	if current.FirstSeenAt != prior.FirstSeenAt || current.ProgressAt.Before(prior.ProgressAt) || current.Observation == nil || prior.Proof != nil && !reflect.DeepEqual(current.Proof, prior.Proof) {
		return false
	}
	before, after := prior.Observation, current.Observation
	if before == nil {
		return true
	}
	if monitorClaimAccepted(before) && !monitorClaimAccepted(after) {
		return false
	}
	if before.EvidenceKind == "signed-receipt" {
		if after.EvidenceKind != "signed-receipt" || before.TransactionHash != after.TransactionHash || before.BlockHash != after.BlockHash || before.AcceptedAmountRao != after.AcceptedAmountRao {
			return false
		}
		if monitorClaimPaymentKnown(before) && (before.PaymentStatus != after.PaymentStatus || before.UnpaidCreditRao != after.UnpaidCreditRao || before.AggregatePaidRao != after.AggregatePaidRao) {
			return false
		}
	}
	return true
}

// Unchanged evidence alone becomes a placeholder. Changed and unresolved
// observations stay in the active record or their authenticated archive basis.
func externalizeMonitorClaimRecord(record monitorClaimCheckpointRecord, commitments *monitorClaimEpochCommitments) monitorClaimCheckpointRecord {
	if record.Archive == nil {
		return record
	}
	record.State.Epochs = append([]monitorClaimEpochState(nil), record.State.Epochs...)
	for index, value := range record.State.Epochs {
		if commitments.matches(value) {
			record.State.Epochs[index] = monitorClaimEpochState{Epoch: value.Epoch, Archived: true}
		}
	}
	return record
}

// This receives a fully hydrated, validated original. Every previous review
// remains in the active head; importing old grammar binds its ORIGINAL digest.
func compactMonitorClaim(record monitorClaimCheckpointRecord, reference monitorHistoryReference, policy monitorClaimPolicy) (monitorClaimCheckpointRecord, map[int64]monitorClaimEpochState, error) {
	if err := reference.validate(); err != nil {
		return record, nil, err
	}
	history, err := record.retainedClaimPolicyHistory(policy)
	if err != nil {
		return record, nil, err
	}
	if err := validateMonitorClaimState(policy.atResources(history.Entries[len(history.Entries)-1].Resources), record.State); err != nil {
		return record, nil, err
	}
	if record.State.Record == nil {
		return record, nil, errors.New("claim archive has no original producer publication")
	}
	archive := &monitorClaimArchive{Boundary: monitorClaimArchiveBoundary{HighWaterAt: record.State.HighWaterAt, LastAcceptedAt: record.State.LastAcceptedAt, SemanticProgressAt: record.State.SemanticProgressAt, Incidents: record.State.Incidents, Restarts: record.State.Restarts, InstanceId: record.State.Record.InstanceId, Sequence: record.State.Record.Sequence, StartedAt: record.State.Record.StartedAt, PublishedAt: record.State.Record.PublishedAt, RecordSha256: rootObjectHash(record.State.Record)}}
	priorEpochHashes := map[int64]string{}
	if record.Archive != nil {
		archive.Segments = append(archive.Segments, record.Archive.Segments...)
		for _, epoch := range record.Archive.Epochs {
			priorEpochHashes[epoch.Epoch] = epoch.StateSha256
		}
	}
	basis := make(map[int64]monitorClaimEpochState)
	changed := false
	for _, epoch := range record.State.Epochs {
		policy.observeWork("compact-epoch", 1)
		if epoch.Observation == nil {
			continue
		}
		hash := rootObjectHash(epoch)
		archive.Epochs = append(archive.Epochs, monitorClaimArchivedEpoch{Epoch: epoch.Epoch, StateSha256: hash})
		basis[epoch.Epoch] = cloneMonitorClaimEpoch(epoch)
		changed = changed || priorEpochHashes[epoch.Epoch] != hash
	}
	if !changed {
		return record, nil, errors.New("claim archive has no new retained evidence to move")
	}
	archive.Segments = append(archive.Segments, reference)
	record.Archive, record.PolicyHistory = archive, history
	if err := archive.validate(policy, record); err != nil {
		return record, nil, err
	}
	record = externalizeMonitorClaimRecord(record, newMonitorClaimEpochCommitments(archive))
	record.ContentHash, err = hashMonitorClaimCheckpoint(record)
	return record, basis, err
}

// One old payload at a time is decoded. Only the bounded current epoch basis
// remains resident; all original owners remain held for later custody checks.
func openMonitorClaimArchive(ctx context.Context, policy monitorClaimPolicy, record monitorClaimCheckpointRecord, protected ...string) (admission *monitorClaimArchiveAdmission, resultErr error) {
	admission = &monitorClaimArchiveAdmission{epochStateKVs: map[int64]monitorClaimEpochState{}, work: policy.observeWork}
	defer func() {
		if resultErr != nil {
			resultErr = monitorAdmissionFailure(resultErr, admission.close())
			admission = nil
		}
	}()
	basis, windows, references, err := replayMonitorClaimHistory(ctx, policy, record, func(reference monitorHistoryReference) ([]byte, error) {
		for _, path := range protected {
			if monitorHistoryPathsAlias(path, reference.Path) {
				return nil, errors.New("Claim retained history aliases the active or next checkpoint owner")
			}
		}
		owner, raw, err := openMonitorHistoryReader(ctx, reference)
		if err != nil {
			return nil, err
		}
		admission.owners = append(admission.owners, owner)
		return raw, nil
	})
	if err != nil {
		return admission, err
	}
	admission.epochStateKVs, admission.windows, admission.references = basis, windows, references
	admission.commitments = newMonitorClaimEpochCommitments(record.Archive)
	return admission, ctx.Err()
}

// Runtime reopening and copied-source restore use the same complete replay.
// The caller owns read custody; a partial page never publishes an epoch basis.
// Context checks separate bounded payload operations and retain read causes.
func replayMonitorClaimArchive(ctx context.Context, policy monitorClaimPolicy, record monitorClaimCheckpointRecord, read func(monitorHistoryReference) ([]byte, error)) (map[int64]monitorClaimEpochState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := record.Archive.validate(policy, record); err != nil {
		return nil, err
	}
	currentReviews, err := record.retainedClaimPolicyHistory(policy)
	if err != nil {
		return nil, err
	}
	basis := map[int64]monitorClaimEpochState{}
	if record.Archive == nil {
		return basis, ctx.Err()
	}
	var prior *monitorClaimArchive
	var priorReviews *monitorProgressPolicyHistory
	var priorCatalog *monitorHistoryCatalogState
	for _, reference := range record.Archive.Segments {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := read(reference)
		policy.observeWork("archive-read", 1)
		policy.observeWork("archive-read-bytes", uint64(len(raw)))
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		original, err := decodeMonitorClaimCheckpoint(raw, policy)
		policy.observeWork("archive-decode", 1)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(original.Window, record.Window) || !reflect.DeepEqual(original.Archive, prior) || !original.Catalog.retains(priorCatalog) || !record.Catalog.retains(original.Catalog) {
			return nil, errors.New("claim archive omitted a predecessor or signed catalog acknowledgment")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		original, err = hydrateMonitorClaimRecord(original, basis, policy)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		next, nextBasis, err := compactMonitorClaim(original, reference, policy)
		if err != nil {
			return nil, err
		}
		if !monitorClaimReviewsRetain(currentReviews, next.PolicyHistory) || priorReviews != nil && !monitorClaimReviewsRetain(next.PolicyHistory, priorReviews) {
			return nil, errors.New("claim archive lost independently reviewed expectations or policy history")
		}
		prior, priorReviews, priorCatalog = next.Archive, next.PolicyHistory, original.Catalog
		basis = nextBasis
	}
	if !reflect.DeepEqual(prior, record.Archive) {
		return nil, errors.New("claim archive commitments differ from original checkpoints")
	}
	return basis, ctx.Err()
}

func (self *monitorClaimArchiveAdmission) check() error {
	if self == nil {
		return nil
	}
	for _, owner := range self.owners {
		if self.work != nil {
			self.work("archive-custody-check", 1)
		}
		if err := owner.check(); err != nil {
			return err
		}
	}
	return nil
}

func (self *monitorClaimArchiveAdmission) close() error {
	if self == nil {
		return nil
	}
	var result error
	for _, owner := range self.owners {
		result = errors.Join(result, owner.close())
	}
	self.owners, self.epochStateKVs, self.commitments, self.windows, self.references = nil, nil, nil, nil, nil
	return result
}

// Legacy policies retain their original envelope. New catalog authority opts
// into this separate state bound; it cannot consume its reserved review space.
func monitorClaimActiveBudget(state monitorClaimState, policy monitorClaimPolicy) error {
	if policy.HistoryCatalog == nil {
		return nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(raw) > maximumMonitorClaimActiveBytes {
		return errors.New("claim active state exceeds its opted-in retained payload capacity")
	}
	return nil
}

// Retained evidence does not disappear from the census when bytes move to an
// archive. Fixed metrics expose impending limits without path/hash labels.
type monitorClaimArchiveStatus struct {
	Segments           int                    `json:"segments"`
	RetainedEpochBases int                    `json:"retained_epoch_bases"`
	EpochCapacity      uint64                 `json:"epoch_capacity"`
	CatalogBytes       uint64                 `json:"catalog_bytes"`
	Capacity           monitorHistoryCapacity `json:"capacity"`
	CapacityWarning    bool                   `json:"capacity_warning"`
}

func (self *monitorClaimWorker) archiveStatus() monitorClaimArchiveStatus {
	status := monitorClaimArchiveStatus{EpochCapacity: self.policy.resources().epochCapacity(), Capacity: self.catalog.capacity(self.policy.HistoryCatalog)}
	if self.archive != nil {
		status.Segments, status.RetainedEpochBases = len(self.archive.Segments), len(self.archive.Epochs)
	}
	raw, err := monitorClaimCatalogBytes(monitorClaimCheckpointRecord{Archive: self.archive, Catalog: self.catalog, Window: self.window})
	if err == nil {
		status.CatalogBytes = uint64(len(raw))
	}
	if self.archiveAdmission != nil {
		status.Segments = len(self.archiveAdmission.references)
	}
	status.CapacityWarning = err != nil || 2*(uint64(status.Segments)+1) >= status.Capacity.Segments || 2*(uint64(status.Segments)+1) >= status.Capacity.HeldReaders || 2*(status.CatalogBytes+6*maximumMonitorHistoryPath+256) >= status.Capacity.CatalogBytes
	return status
}

func (self *monitorClaimWorker) appendArchiveMetrics(raw []byte) []byte {
	status := self.archiveStatus()
	warning := 0
	if status.CapacityWarning {
		warning = 1
	}
	var output strings.Builder
	for _, metric := range []struct {
		name  string
		value any
	}{
		{"archive_segments", status.Segments}, {"archive_retained_epoch_bases", status.RetainedEpochBases},
		{"archive_segment_capacity", status.Capacity.Segments}, {"archive_reader_capacity", status.Capacity.HeldReaders},
		{"archive_catalog_bytes", status.CatalogBytes}, {"archive_catalog_byte_capacity", status.Capacity.CatalogBytes},
		{"archive_capacity_warning", warning},
	} {
		fmt.Fprintf(&output, "# TYPE sn_mainnet_claim_%s gauge\nsn_mainnet_claim_%s{role=%q} %v\n", metric.name, metric.name, self.policy.Role, metric.value)
	}
	return append(raw, output.String()...)
}
