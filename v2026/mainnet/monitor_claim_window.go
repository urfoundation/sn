// A reviewed Claim window moves only immutable original receipt observations
// out of the hot census. Unknown and unresolved epochs stay active. Every prior
// policy, review and checkpoint remains authenticated by an original snapshot;
// retirement never means a deferred credit or aggregate event was paid per epoch.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/urfoundation/sn/v2026/protocol"
)

const monitorClaimWindowSchema = "urnetwork-claim-monitor-window-v1"
const maximumMonitorClaimRetiredBytes = 32 * 1024 * 1024

// The next independent configuration pins the accepted transition. It never
// embeds recursively growing policies or obtains expectations from a producer.
type monitorClaimWindowPolicy struct {
	Schema             string `json:"schema"`
	Ordinal            uint64 `json:"ordinal"`
	OriginalPolicyHash string `json:"original_policy_hash"`
	TransitionHash     string `json:"transition_hash"`
}

func (self *monitorClaimWindowPolicy) validate() error {
	if self == nil {
		return nil
	}
	if self.Schema != monitorClaimWindowSchema || self.Ordinal == 0 || self.Ordinal > maximumReviewedMonitorHistorySegments || !planSha256(self.OriginalPolicyHash) || !planSha256(self.TransitionHash) {
		return errors.New("Claim window lacks its bounded original policy and transition")
	}
	return nil
}

type monitorClaimWindowCounts struct {
	Receipts      uint64 `json:"original_receipts"`
	Deferred      uint64 `json:"original_deferred_receipts"`
	AggregatePaid uint64 `json:"original_aggregate_paid_events"`
	PolicyReviews uint64 `json:"retained_policy_reviews"`
}

// Signatures authorize an operational transition, not any financial assertion.
// Full original state must independently reproduce every commitment and count.
type monitorClaimWindowTransition struct {
	Schema             string                      `json:"schema"`
	Ordinal            uint64                      `json:"ordinal"`
	OriginalPolicyHash string                      `json:"original_policy_hash"`
	PreviousTransition string                      `json:"previous_transition"`
	Original           monitorHistoryReference     `json:"original"`
	PreviousPolicyHash string                      `json:"previous_policy_hash"`
	NextPolicyHash     string                      `json:"next_policy_hash"`
	Retired            []monitorClaimArchivedEpoch `json:"retired"`
	HighestEpoch       int64                       `json:"highest_expected_epoch"`
	Counts             monitorClaimWindowCounts    `json:"counts"`
	ReviewSha256       string                      `json:"review_sha256"`
}

func (self monitorClaimWindowTransition) hash() string { return rootObjectHash(self) }

func (self monitorClaimWindowTransition) signingBytes() ([]byte, error) {
	if self.Schema != monitorClaimWindowSchema || self.Ordinal == 0 || self.Ordinal > maximumReviewedMonitorHistorySegments || self.HighestEpoch < 0 || len(self.Retired) == 0 || len(self.Retired) > maximumMonitorRetainedClaimEpochs || self.Original.validate() != nil {
		return nil, errors.New("Claim window transition exceeds its original bounded scope")
	}
	for _, hash := range []string{self.OriginalPolicyHash, self.PreviousTransition, self.PreviousPolicyHash, self.NextPolicyHash, self.ReviewSha256} {
		if !planSha256(hash) {
			return nil, errors.New("Claim window transition has an invalid retained digest")
		}
	}
	seen := map[int64]bool{}
	for _, epoch := range self.Retired {
		if epoch.Epoch < 0 || seen[epoch.Epoch] || !planSha256(epoch.StateSha256) {
			return nil, errors.New("Claim window repeated or changed a retired epoch commitment")
		}
		seen[epoch.Epoch] = true
	}
	return json.Marshal(struct {
		Domain     string                       `json:"domain"`
		Transition monitorClaimWindowTransition `json:"transition"`
	}{Domain: "urnetwork-claim-window-approval-v1", Transition: self})
}

type monitorClaimWindowState struct {
	Transition     monitorClaimWindowTransition `json:"transition"`
	PreviousPolicy monitorClaimPolicy           `json:"previous_policy"`
	Signature      string                       `json:"signature_ed25519"`
}

// Only mutable resource/expectation fields can change across a window. The
// original catalog key, endpoint, pool, member and role remain exactly bound.
func monitorClaimWindowIdentity(policy monitorClaimPolicy) string {
	policy.Epochs, policy.Renewal, policy.Window = nil, nil, nil
	policy.FreshnessSeconds, policy.ReadBudgetSeconds, policy.EpochCapacity, policy.ReviewHistoryEntries = 0, 0, 0, 0
	return rootObjectHash(policy)
}

func (self *monitorClaimWindowState) validate(policy monitorClaimPolicy, history *monitorProgressPolicyHistory) error {
	if self == nil {
		if policy.Window != nil {
			return errors.New("Claim window configuration lost its retained transition")
		}
		return nil
	}
	if policy.Window == nil || policy.Window.validate() != nil || policy.HistoryCatalog == nil || self.PreviousPolicy.HistoryCatalog == nil || monitorClaimWindowIdentity(policy) != monitorClaimWindowIdentity(self.PreviousPolicy) || history == nil || len(history.Entries) == 0 {
		return errors.New("Claim window replaced independent identity or catalog authority")
	}
	transition := self.Transition
	next := policy.atResources(history.Entries[0].Resources)
	next.Window = nil
	if policy.Window.Ordinal != transition.Ordinal || policy.Window.OriginalPolicyHash != transition.OriginalPolicyHash || policy.Window.TransitionHash != transition.hash() || transition.PreviousPolicyHash != "sha256:"+self.PreviousPolicy.hash() || transition.NextPolicyHash != rootObjectHash(next) || transition.OriginalPolicyHash != self.PreviousPolicy.archiveIdentityHash() {
		return errors.New("Claim window differs from its accepted policy transition")
	}
	message, messageErr := transition.signingBytes()
	key, keyErr := rootReceiptHex(policy.HistoryCatalog.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := hex.DecodeString(self.Signature)
	if messageErr != nil || keyErr != nil || signatureErr != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(key, message, signature) {
		return errors.Join(errors.New("Claim window approval signature is invalid"), messageErr, keyErr, signatureErr)
	}
	return nil
}

func monitorClaimWindowRetirable(epoch monitorClaimEpochState) bool {
	return epoch.Observation != nil && epoch.Observation.EvidenceKind == "signed-receipt" && monitorClaimPaymentKnown(epoch.Observation)
}

// Derive the unique retained prefix and retired commitments from original
// hydrated evidence. No signed supplied count can replace this reconstruction.
func deriveMonitorClaimWindow(previous, next monitorClaimPolicy, original monitorHistoryReference, record monitorClaimCheckpointRecord, review string) (transition monitorClaimWindowTransition, state monitorClaimState, err error) {
	if previous.HistoryCatalog == nil || next.HistoryCatalog == nil || next.Window != nil || next.Renewal != nil || monitorClaimWindowIdentity(previous) != monitorClaimWindowIdentity(next) || !planSha256(review) {
		return transition, state, errors.New("Claim window requires the original independent key and a new explicit policy")
	}
	priorResources, nextResources := previous.resources(), next.resources()
	priorResources.Epochs, nextResources.Epochs = 0, 0
	if !nextResources.includes(priorResources) || next.resources().validate(true) != nil {
		return transition, state, errors.New("Claim window cannot shrink retained operational capacities")
	}
	history, err := record.retainedClaimPolicyHistory(previous)
	if err != nil {
		return transition, state, err
	}
	if history.Entries[len(history.Entries)-1].Resources != previous.resources() {
		return transition, state, errors.New("Claim window must first acknowledge its current policy")
	}
	if err := validateMonitorClaimState(previous, record.State); err != nil {
		return transition, state, err
	}
	transition = monitorClaimWindowTransition{Schema: monitorClaimWindowSchema, Ordinal: 1, OriginalPolicyHash: previous.archiveIdentityHash(), PreviousTransition: previous.archiveIdentityHash(), Original: original, PreviousPolicyHash: "sha256:" + previous.hash(), NextPolicyHash: rootObjectHash(next), HighestEpoch: -1, ReviewSha256: review}
	if record.Window != nil {
		transition.Ordinal = record.Window.Transition.Ordinal + 1
		transition.PreviousTransition = record.Window.Transition.hash()
		transition.HighestEpoch = record.Window.Transition.HighestEpoch
		transition.Counts = record.Window.Transition.Counts
	}
	if uint64(len(history.Entries)) > math.MaxUint64-transition.Counts.PolicyReviews {
		return transition, state, errors.New("Claim window review census overflows")
	}
	transition.Counts.PolicyReviews += uint64(len(history.Entries))
	state = record.State
	state.Epochs = nil
	for index, epoch := range record.State.Epochs {
		previous.observeWork("window-original-epoch", 1)
		transition.HighestEpoch = max(transition.HighestEpoch, epoch.Epoch)
		if monitorClaimWindowRetirable(epoch) {
			transition.Retired = append(transition.Retired, monitorClaimArchivedEpoch{Epoch: epoch.Epoch, StateSha256: rootObjectHash(epoch)})
			if transition.Counts.Receipts == math.MaxUint64 {
				return transition, state, errors.New("Claim window receipt census overflows")
			}
			transition.Counts.Receipts++
			if epoch.Observation.PaymentStatus == "deferred" {
				transition.Counts.Deferred++
			} else {
				transition.Counts.AggregatePaid++
			}
			continue
		}
		if len(state.Epochs) >= len(next.Epochs) || previous.Epochs[index] != next.Epochs[len(state.Epochs)] {
			return transition, state, errors.New("Claim window omitted or changed an unresolved original expectation")
		}
		state.Epochs = append(state.Epochs, cloneMonitorClaimEpoch(epoch))
	}
	if len(transition.Retired) == 0 {
		return transition, state, errors.New("Claim window capacity is held by unresolved original epochs")
	}
	for _, expected := range next.Epochs[len(state.Epochs):] {
		if expected.Epoch <= transition.HighestEpoch {
			return transition, state, errors.New("Claim window reused an original expected epoch")
		}
		state.Epochs = append(state.Epochs, monitorClaimEpochState{Epoch: expected.Epoch})
	}
	// HighestEpoch binds both past retired epochs and this newly accepted range.
	for _, expected := range next.Epochs {
		transition.HighestEpoch = max(transition.HighestEpoch, expected.Epoch)
	}
	if _, err := transition.signingBytes(); err != nil {
		return transition, state, err
	}
	if err := validateMonitorClaimState(next, state); err != nil {
		return transition, state, err
	}
	return transition, state, nil
}

type monitorClaimWindowAdmission struct {
	retired       map[int64]monitorClaimEpochState
	reviews       map[string]bool
	retainedBytes uint64
	windows       uint64
	counts        monitorClaimWindowCounts
	visit         func()
}

// All windows and ordinary segments share one owner and metadata envelope.
// The callback authenticates bytes and retains custody; no payload cache is
// published until every predecessor and the current compact head agree.
func replayMonitorClaimHistory(ctx context.Context, policy monitorClaimPolicy, record monitorClaimCheckpointRecord, read func(monitorHistoryReference) ([]byte, error)) (map[int64]monitorClaimEpochState, *monitorClaimWindowAdmission, []monitorHistoryReference, error) {
	capacity := record.Catalog.capacity(policy.HistoryCatalog)
	seen := map[string]bool{}
	var references []monitorHistoryReference
	var metadata uint64
	boundedRead := func(reference monitorHistoryReference) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := reference.validate(); err != nil {
			return nil, err
		}
		if seen[reference.Path] || seen[reference.Path+".lock"] {
			return nil, errors.New("Claim history repeats or aliases an original window owner")
		}
		if uint64(len(references)+1) > capacity.Segments || uint64(len(references)+1) > capacity.HeldReaders {
			return nil, errors.New("Claim complete history needs reviewed segment and reader capacity")
		}
		encoded, err := json.Marshal(reference)
		if err != nil {
			return nil, err
		}
		metadata += uint64(len(encoded)) + 1
		if metadata > capacity.CatalogBytes {
			return nil, errors.New("Claim complete history exceeds its retained reference byte capacity")
		}
		seen[reference.Path], seen[reference.Path+".lock"] = true, true
		raw, err := read(reference)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if uint64(len(raw)) != reference.Bytes || monitorReadDigest(raw) != reference.Sha256 {
			return nil, errors.New("Claim history bytes differ from their exact retained reference")
		}
		references = append(references, reference)
		return raw, nil
	}
	basis, err := replayMonitorClaimArchive(ctx, policy, record, boundedRead)
	if err != nil {
		return nil, nil, nil, err
	}
	windows, err := replayMonitorClaimWindows(ctx, policy, record, basis, boundedRead)
	if err != nil {
		return nil, nil, nil, err
	}
	windows.visit = func() { policy.observeWork("retired-receipt-lookup", 1) }
	return basis, windows, references, ctx.Err()
}

// Past review references remain unavailable for reuse after a hot window is
// replaced. Existing current acknowledgments are checked separately by their
// original history; only the archived windows populate this lookup.
func (self *monitorClaimWindowAdmission) checkReviews(history *monitorProgressPolicyHistory) error {
	if self == nil || history == nil {
		return nil
	}
	for _, entry := range history.Entries {
		if entry.ReviewSha256 != "" && self.reviews[entry.ReviewSha256] {
			return errors.New("Claim policy reused an archived independent review reference")
		}
	}
	return nil
}

// At most one old payload is resident during replay. The resulting bounded
// receipt lookup supports constant work per producer entry on later samples.
func replayMonitorClaimWindows(ctx context.Context, policy monitorClaimPolicy, record monitorClaimCheckpointRecord, currentBasis map[int64]monitorClaimEpochState, read func(monitorHistoryReference) ([]byte, error)) (*monitorClaimWindowAdmission, error) {
	admission := &monitorClaimWindowAdmission{retired: map[int64]monitorClaimEpochState{}, reviews: map[string]bool{}}
	if record.Window == nil {
		return admission, nil
	}
	capacity := record.Catalog.capacity(policy.HistoryCatalog)
	admission.counts = record.Window.Transition.Counts
	currentHistory, err := record.retainedClaimPolicyHistory(policy)
	if err != nil {
		return nil, err
	}
	for record.Window != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		window := record.Window
		history, err := record.retainedClaimPolicyHistory(policy)
		if err != nil {
			return nil, err
		}
		if err := window.validate(policy, history); err != nil {
			return nil, err
		}
		if admission.reviews[window.Transition.ReviewSha256] {
			return nil, errors.New("Claim window reused a retained independent review reference")
		}
		admission.reviews[window.Transition.ReviewSha256] = true
		raw, err := read(window.Transition.Original)
		policy.observeWork("window-read", 1)
		policy.observeWork("window-read-bytes", uint64(len(raw)))
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		oldPolicy := window.PreviousPolicy
		oldPolicy.work = policy.work
		old, err := decodeMonitorClaimCheckpoint(raw, oldPolicy)
		if err != nil {
			return nil, err
		}
		if !record.Catalog.retains(old.Catalog) {
			return nil, errors.New("Claim window discarded original catalog authority")
		}
		oldHistory, err := old.retainedClaimPolicyHistory(oldPolicy)
		if err != nil {
			return nil, err
		}
		for _, entry := range oldHistory.Entries {
			if entry.ReviewSha256 == "" {
				continue
			}
			if admission.reviews[entry.ReviewSha256] {
				return nil, errors.New("Claim window lost unique original review provenance")
			}
			admission.reviews[entry.ReviewSha256] = true
		}
		basis, err := replayMonitorClaimArchive(ctx, oldPolicy, old, read)
		if err != nil {
			return nil, err
		}
		old, err = hydrateMonitorClaimRecord(old, basis, oldPolicy)
		if err != nil {
			return nil, err
		}
		next := policy.atResources(history.Entries[0].Resources)
		next.Window = nil
		derived, retained, err := deriveMonitorClaimWindow(oldPolicy, next, window.Transition.Original, old, window.Transition.ReviewSha256)
		if err != nil || !reflect.DeepEqual(derived, window.Transition) {
			return nil, errors.Join(errors.New("Claim window differs from complete original obligations and reviews"), err)
		}
		for index, prior := range retained.Epochs {
			if index >= len(record.State.Epochs) || record.State.Epochs[index].Epoch != prior.Epoch {
				return nil, errors.New("Claim window discarded its retained original epoch prefix")
			}
			current := record.State.Epochs[index]
			if current.Archived {
				current = currentBasis[current.Epoch]
			}
			if prior.Observation != nil && !monitorClaimEpochRetains(current, prior) {
				return nil, errors.New("Claim window contradicted an unresolved original epoch")
			}
		}
		for _, epoch := range old.State.Epochs {
			if !monitorClaimWindowRetirable(epoch) {
				continue
			}
			if _, present := admission.retired[epoch.Epoch]; present {
				return nil, errors.New("Claim window retired the same original epoch twice")
			}
			raw, err := json.Marshal(epoch)
			if err != nil || uint64(len(raw)) > maximumMonitorClaimRetiredBytes-admission.retainedBytes {
				return nil, errors.Join(errors.New("Claim retained receipt lookup exceeds its explicit byte capacity"), err)
			}
			admission.retainedBytes += uint64(len(raw))
			admission.retired[epoch.Epoch] = cloneMonitorClaimEpoch(epoch)
		}
		admission.windows++
		if admission.windows > capacity.Segments {
			return nil, errors.New("Claim window chain requires reviewed retained owner capacity")
		}
		record, policy, currentBasis = old, oldPolicy, basis
	}
	if err := admission.checkReviews(currentHistory); err != nil {
		return nil, err
	}
	return admission, ctx.Err()
}

// An unchanged retired receipt is a retained fact. A later weaker report does
// not erase it, and a new complete contradictory tuple cannot become progress.
func (self *monitorClaimWindowAdmission) publicationCode(value *protocol.ClaimProgress) string {
	if self == nil || value == nil {
		return "ok"
	}
	for _, entry := range value.Entries {
		if self.visit != nil {
			self.visit()
		}
		prior, present := self.retired[entry.Epoch]
		if !present || entry.Observation == nil {
			continue
		}
		before, after := prior.Observation, entry.Observation
		if after.EvidenceKind != "api-no-claim" && (after.Pool != before.Pool || after.ShareBps != before.ShareBps) {
			return "identity"
		}
		if !monitorClaimAccepted(after) || prior.Proof != nil && after.EvidenceKind == "finalized-leaf" && (prior.Proof.PayoutRoot != after.PayoutRoot || prior.Proof.ArtifactHash != "" && after.ArtifactHash != "" && prior.Proof.ArtifactHash != after.ArtifactHash) {
			return "contradiction"
		}
		if after.EvidenceKind == "signed-receipt" && (before.TransactionHash != after.TransactionHash || before.BlockHash != after.BlockHash || before.AcceptedAmountRao != after.AcceptedAmountRao || monitorClaimPaymentKnown(after) && (before.PaymentStatus != after.PaymentStatus || before.UnpaidCreditRao != after.UnpaidCreditRao || before.AggregatePaidRao != after.AggregatePaidRao)) {
			return "contradiction"
		}
	}
	return "ok"
}

// These are immutable original receipt facts, not outstanding credit balances
// or allocated payments. They remain separate from the active epoch summary.
type monitorClaimWindowStatus struct {
	Windows       uint64                   `json:"retained_windows"`
	RetiredEpochs int                      `json:"retired_receipt_epochs"`
	RetainedBytes uint64                   `json:"retired_receipt_bytes"`
	Counts        monitorClaimWindowCounts `json:"original_facts"`
	Warning       bool                     `json:"capacity_warning"`
	Authority     string                   `json:"authority"`
}

func (self *monitorClaimWorker) windowStatus() monitorClaimWindowStatus {
	status := monitorClaimWindowStatus{Authority: "configured-source-assertions-original-receipts"}
	if self.archiveAdmission != nil && self.archiveAdmission.windows != nil {
		window := self.archiveAdmission.windows
		status.Windows, status.RetiredEpochs, status.RetainedBytes, status.Counts = window.windows, len(window.retired), window.retainedBytes, window.counts
		status.Warning = 2*(status.RetainedBytes+maximumMonitorClaimActiveBytes) >= maximumMonitorClaimRetiredBytes
	}
	return status
}

func (self *monitorClaimWorker) appendWindowMetrics(raw []byte) []byte {
	status := self.windowStatus()
	var output strings.Builder
	warning := 0
	if status.Warning {
		warning = 1
	}
	for _, metric := range []struct {
		name  string
		value any
	}{
		{"retired_windows", status.Windows}, {"retired_receipt_epochs", status.RetiredEpochs},
		{"retired_receipt_bytes", status.RetainedBytes}, {"retired_original_deferred_receipts", status.Counts.Deferred},
		{"retired_original_aggregate_paid_events", status.Counts.AggregatePaid}, {"archived_policy_reviews", status.Counts.PolicyReviews},
		{"window_capacity_warning", warning},
	} {
		fmt.Fprintf(&output, "# TYPE sn_mainnet_claim_%s gauge\nsn_mainnet_claim_%s{role=%q} %v\n", metric.name, metric.name, self.policy.Role, metric.value)
	}
	return append(raw, output.String()...)
}
