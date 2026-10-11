// Combined Claim windows preserve original expectations and receipt custody.
// The original Claim approver signs each exact checkpoint transition; an
// endpoint publication never supplies a new expected epoch or approval key.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/urfoundation/sn/v2026/protocol"
)

const economicConservationClaimWindowSchema = "urnetwork-economic-claim-window-v1"

// Previous signs the prior cumulative head, while Original retains the exact
// combined state before this adoption. Retirement is reconstructed from that
// state and its verified vault receipt matches, never trusted from this list.
type economicConservationClaimWindow struct {
	Schema       string                      `json:"schema"`
	PolicyHash   string                      `json:"original_conservation_policy_hash"`
	Role         string                      `json:"role"`
	Original     monitorHistoryReference     `json:"original_checkpoint"`
	Ordinal      uint64                      `json:"ordinal"`
	Previous     string                      `json:"previous_window_hash"`
	ReviewSha256 string                      `json:"review_sha256"`
	Next         monitorClaimPolicy          `json:"next_policy"`
	HighestEpoch int64                       `json:"highest_epoch"`
	Retired      []monitorClaimArchivedEpoch `json:"retired_epochs"`
	Signature    string                      `json:"signature_ed25519,omitempty"`
}

// Only this cumulative head remains after the signed transition itself moves
// into a complete original snapshot. It cannot admit a reader by itself.
type economicConservationClaimHead struct {
	Role         string             `json:"role"`
	Hash         string             `json:"window_hash"`
	Ordinal      uint64             `json:"ordinal"`
	HighestEpoch int64              `json:"highest_epoch"`
	Retired      uint64             `json:"retired_epochs"`
	Policy       monitorClaimPolicy `json:"policy"`
}

// At most one predecessor's Claim state is resident during archive admission.
// It is replaced after each sequential segment; the full archive is not kept
// or hydrated again for every foreground sample.
type economicConservationClaimBasis struct {
	reference monitorHistoryReference
	heads     []economicConservationClaimHead
	states    []monitorClaimState
	eligible  map[string]string
	work      func(role, stage string, units uint64)
}

func economicConservationClaimBasisPolicy(policy economicConservationPolicy) economicConservationPolicy {
	if policy.resourceBasis != nil {
		return *policy.resourceBasis
	}
	return policy
}

func economicConservationClaimRole(policy economicConservationPolicy, role string) (int, monitorClaimPolicy, bool) {
	policy = economicConservationClaimBasisPolicy(policy)
	for index, claim := range policy.Claims {
		if claim.Role == role {
			return index, claim, true
		}
	}
	return 0, monitorClaimPolicy{}, false
}

// Neither component renewal nor a component window can be transplanted into
// this owner. The exact original pool, member, endpoint and approver stay fixed.
func economicConservationClaimPolicyIncludes(original, next monitorClaimPolicy) bool {
	before, after := original.resources(), next.resources()
	before.Epochs, after.Epochs = 0, 0
	return next.Renewal == nil && next.Window == nil && monitorClaimWindowIdentity(original) == monitorClaimWindowIdentity(next) && after.includes(before)
}

func (self economicConservationClaimWindow) signingBytes() ([]byte, error) {
	if self.Schema != economicConservationClaimWindowSchema || !planSha256(self.PolicyHash) || !monitorRolePattern.MatchString(self.Role) || self.Next.Role != self.Role || self.Ordinal == 0 || !planSha256(self.Previous) || !planSha256(self.ReviewSha256) || self.HighestEpoch < 0 || len(self.Retired) > maximumMonitorRetainedClaimEpochs {
		return nil, errors.New("economic Claim window lacks exact bounded original lineage")
	}
	if err := self.Original.validateLimit(economicConservationStorageMaximum); err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	for _, epoch := range self.Retired {
		if epoch.Epoch < 0 || seen[epoch.Epoch] || !planSha256(epoch.StateSha256) {
			return nil, errors.New("economic Claim window repeats or changes retired epoch evidence")
		}
		seen[epoch.Epoch] = true
	}
	self.Signature = ""
	raw, err := json.Marshal(self)
	if err != nil {
		return nil, err
	}
	return append([]byte(economicConservationClaimWindowSchema+"\x00"), raw...), nil
}

func (self economicConservationClaimWindow) verify(policy economicConservationPolicy) error {
	if err := policy.validateReference(self.Original); err != nil {
		return err
	}
	_, original, exists := economicConservationClaimRole(policy, self.Role)
	if !exists || original.HistoryCatalog == nil {
		return errors.New("legacy economic Claim policy cannot enroll a window approver")
	}
	network := policy.Native.Observation.Network
	if err := self.Next.validate(identityExpectation{NativeChain: network.NativeChain, GenesisHash: network.GenesisHash, EvmChainId: network.EvmChainId}); err != nil {
		return err
	}
	message, err := self.signingBytes()
	key, keyErr := rootReceiptHex(original.HistoryCatalog.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if err != nil || keyErr != nil || signatureErr != nil || self.PolicyHash != policy.identityHash() || !economicConservationClaimPolicyIncludes(original, self.Next) || self.ReviewSha256 == original.HistoryCatalog.ReviewSha256 || !ed25519.Verify(key, message, signature) {
		return errors.New("economic Claim window lacks original independent authority or fresh review")
	}
	return nil
}

// Structural decoding precedes archive admission. Original signed transitions
// and their derived retained state are checked separately against held bytes
// before any source read or checkpoint publication.
func (self *economicConservationState) claimHeads(policy economicConservationPolicy) ([]economicConservationClaimHead, error) {
	basis := economicConservationClaimBasisPolicy(policy)
	heads := make([]economicConservationClaimHead, len(basis.Claims))
	for index, claim := range basis.Claims {
		head := economicConservationClaimHead{Role: claim.Role, Hash: rootObjectHash(claim), HighestEpoch: -1, Policy: claim}
		for _, epoch := range claim.Epochs {
			head.HighestEpoch = max(head.HighestEpoch, epoch.Epoch)
		}
		heads[index] = head
	}
	seen := map[string]bool{}
	if self.Archive != nil {
		for _, head := range self.Archive.ClaimHeads {
			index, original, exists := economicConservationClaimRole(policy, head.Role)
			if !exists || seen[head.Role] || head.Ordinal == 0 || !planSha256(head.Hash) || head.HighestEpoch < heads[index].HighestEpoch || !economicConservationClaimPolicyIncludes(original, head.Policy) {
				return nil, errors.New("economic archived Claim window changed original identity or high water")
			}
			network := policy.Native.Observation.Network
			if err := head.Policy.validate(identityExpectation{NativeChain: network.NativeChain, GenesisHash: network.GenesisHash, EvmChainId: network.EvmChainId}); err != nil {
				return nil, err
			}
			seen[head.Role] = true
			heads[index] = head
		}
	}
	seen = map[string]bool{}
	for _, window := range self.ClaimWindows {
		index, _, exists := economicConservationClaimRole(policy, window.Role)
		if !exists || seen[window.Role] || self.Archive == nil || len(self.Archive.Segments) == 0 {
			return nil, errors.New("economic Claim window lacks unique original snapshot custody")
		}
		seen[window.Role] = true
		prior := heads[index]
		reference := self.Archive.Segments[len(self.Archive.Segments)-1]
		if prior.Ordinal == ^uint64(0) || window.Ordinal != prior.Ordinal+1 || window.Previous != prior.Hash || window.HighestEpoch < prior.HighestEpoch || window.Original.Sha256 != reference.Sha256 || window.Original.Bytes != reference.Bytes || uint64(len(window.Retired)) > ^uint64(0)-prior.Retired || !economicConservationClaimPolicyIncludes(prior.Policy, window.Next) {
			return nil, errors.New("economic Claim window changed its exact original predecessor")
		}
		if err := window.verify(basis); err != nil {
			return nil, err
		}
		heads[index] = economicConservationClaimHead{Role: window.Role, Hash: rootObjectHash(window), Ordinal: window.Ordinal, HighestEpoch: window.HighestEpoch, Retired: prior.Retired + uint64(len(window.Retired)), Policy: window.Next}
	}
	return heads, nil
}

func economicConservationClaimObservationHash(observation protocol.ClaimObservation) string {
	observation.ObservedAt = ""
	return rootObjectHash(observation)
}

// A copied checkpoint can retain exactly the same bytes while belonging to a
// different prepared owner. The signed logical path survives ordinary archive
// compaction; physical restore keeps that path and replaces only its custody.
func (self *economicConservationState) validateClaimCheckpointPath(path string) error {
	if self.archiveView != nil && self.archiveView.claimCheckpointPath != "" && self.archiveView.claimCheckpointPath != path {
		return errors.New("economic Claim history moved the original checkpoint")
	}
	for _, window := range self.ClaimWindows {
		if window.Original.Path != path {
			return errors.New("economic Claim window moved the original checkpoint")
		}
	}
	return nil
}

// Only matched original receipt evidence permits retiring an epoch. A signed
// supplied retirement list cannot erase an unmatched receipt or unknown epoch.
func deriveEconomicConservationClaimWindow(policy economicConservationPolicy, basis *economicConservationClaimBasis, supplied economicConservationClaimWindow) (economicConservationClaimWindow, monitorClaimState, error) {
	if basis == nil || supplied.Original.Sha256 != basis.reference.Sha256 || supplied.Original.Bytes != basis.reference.Bytes {
		return supplied, monitorClaimState{}, errors.New("economic Claim window original checkpoint is not admitted")
	}
	index, _, exists := economicConservationClaimRole(policy, supplied.Role)
	if !exists || index >= len(basis.heads) || index >= len(basis.states) {
		return supplied, monitorClaimState{}, errors.New("economic Claim window role is outside original census")
	}
	prior, state := basis.heads[index], basis.states[index]
	if !economicConservationClaimPolicyIncludes(prior.Policy, supplied.Next) {
		return supplied, state, errors.New("economic Claim window replaced authority or shrank resources")
	}
	derived := supplied
	derived.Ordinal, derived.Previous, derived.HighestEpoch, derived.Retired = prior.Ordinal+1, prior.Hash, prior.HighestEpoch, nil
	state.Epochs = nil
	for offset, epoch := range basis.states[index].Epochs {
		if basis.work != nil {
			basis.work(prior.Role, "original-epoch", 1)
		}
		key := prior.Role + "/" + fmt.Sprint(epoch.Epoch)
		if monitorClaimWindowRetirable(epoch) && basis.eligible[key] == economicConservationClaimObservationHash(*epoch.Observation) {
			derived.Retired = append(derived.Retired, monitorClaimArchivedEpoch{Epoch: epoch.Epoch, StateSha256: rootObjectHash(epoch)})
			continue
		}
		if len(state.Epochs) >= len(supplied.Next.Epochs) || offset >= len(prior.Policy.Epochs) || prior.Policy.Epochs[offset] != supplied.Next.Epochs[len(state.Epochs)] {
			return derived, state, errors.New("economic Claim window omitted an unresolved or unmatched original epoch")
		}
		state.Epochs = append(state.Epochs, cloneMonitorClaimEpoch(epoch))
	}
	appended := len(supplied.Next.Epochs) - len(state.Epochs)
	if appended == 0 && len(derived.Retired) == 0 && supplied.Next.resources() == prior.Policy.resources() {
		return derived, state, errors.New("economic Claim window did not advance expectations or reviewed resources")
	}
	for _, epoch := range supplied.Next.Epochs[len(state.Epochs):] {
		if epoch.Epoch <= prior.HighestEpoch {
			return derived, state, errors.New("economic Claim window reused an original expected epoch")
		}
		state.Epochs = append(state.Epochs, monitorClaimEpochState{Epoch: epoch.Epoch})
		derived.HighestEpoch = max(derived.HighestEpoch, epoch.Epoch)
	}
	if prior.Ordinal == ^uint64(0) {
		return derived, state, errors.New("economic Claim window ordinal is exhausted")
	}
	return derived, state, validateMonitorClaimState(supplied.Next, state)
}

func (self *economicConservationArchiveView) setClaimBasis(policy economicConservationPolicy, original *economicConservationState, reference monitorHistoryReference) error {
	if self == nil || original == nil {
		return errors.New("economic Claim basis requires admitted original state")
	}
	heads, err := original.claimHeads(policy)
	if err != nil {
		return err
	}
	if len(heads) != len(original.ClaimStates) {
		return errors.New("economic Claim basis changed its original role census")
	}
	basis := &economicConservationClaimBasis{reference: reference, heads: heads, states: original.ClaimStates, eligible: map[string]string{}, work: self.claimWork}
	for index, head := range heads {
		for _, epoch := range original.ClaimStates[index].Epochs {
			if self.claimWork != nil {
				self.claimWork(head.Role, "basis-epoch", 1)
			}
			key := head.Role + "/" + fmt.Sprint(epoch.Epoch)
			if receipt, exists := self.receipts[key]; exists && economicConservationReceiptArchivable(receipt) {
				basis.eligible[key] = economicConservationClaimObservationHash(receipt.Observation)
			}
		}
	}
	for _, receipt := range original.Receipts {
		if economicConservationReceiptArchivable(receipt) {
			basis.eligible[economicConservationReceiptKey(receipt)] = economicConservationClaimObservationHash(receipt.Observation)
		}
	}
	// The current predecessor shares the reviewed index budget, including
	// fixed per-entry bookkeeping. Replacement also reserves the larger old
	// predecessor; the two-times margin covers their temporary overlap.
	encoded, err := json.Marshal(struct {
		Heads    []economicConservationClaimHead `json:"heads"`
		States   []monitorClaimState             `json:"states"`
		Eligible map[string]string               `json:"eligible"`
	}{Heads: basis.heads, States: basis.states, Eligible: basis.eligible})
	if err != nil {
		return err
	}
	if uint64(len(encoded)) > policy.storageMaximum() {
		return errors.New("economic Claim predecessor exceeds its separate resident byte profile")
	}
	entries := uint64(len(basis.heads) + len(basis.states) + len(basis.eligible))
	for _, state := range basis.states {
		entries += uint64(len(state.Epochs))
		if state.Record != nil {
			entries += 1 + uint64(len(state.Record.Entries))
		}
	}
	bytes := uint64(len(encoded)) + 256*entries
	entryLimit, byteLimit := self.resources.IndexEntries/2, self.resources.IndexBytes/2
	if self.principalReservedBytes > byteLimit {
		return errMonitorEconomicCapacity
	}
	byteLimit -= self.principalReservedBytes
	peakEntries, peakBytes := max(entries, self.claimBasisEntries), max(bytes, self.claimBasisBytes)
	if peakEntries > entryLimit || self.entries > entryLimit-peakEntries || peakBytes > byteLimit || self.bytes > byteLimit-peakBytes {
		return errors.New("economic Claim predecessor needs reviewed entry/byte capacity before admission")
	}
	self.claimBasis = basis
	self.claimBasisEntries, self.claimBasisBytes = entries, bytes
	return nil
}

// Reconstruct each retained prefix from the one admitted predecessor. Status,
// incident, read cursor and original producer birth remain ordinary hot state;
// later observations may only strengthen the retained epoch evidence.
func (self *economicConservationState) validateClaimWindows(policy economicConservationPolicy) error {
	if _, err := self.claimHeads(policy); err != nil {
		return err
	}
	if self.archiveView == nil {
		return nil // Structural decode only; command admission supplies custody.
	}
	for _, window := range self.ClaimWindows {
		if self.archiveView.claimCheckpointPath != "" && self.archiveView.claimCheckpointPath != window.Original.Path {
			return errors.New("economic Claim window moved the original checkpoint")
		}
		if self.archiveView.claimReviews[window.Role+"/"+window.ReviewSha256] {
			return errors.New("economic Claim window reused an archived independent review")
		}
		derived, retained, err := deriveEconomicConservationClaimWindow(policy, self.archiveView.claimBasis, window)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(derived, window) {
			return errors.New("economic Claim window differs from original matched receipts and expectations")
		}
		index, _, _ := economicConservationClaimRole(policy, window.Role)
		if len(retained.Epochs) != len(self.ClaimStates[index].Epochs) {
			return errors.New("economic Claim window changed retained epoch census")
		}
		for offset, prior := range retained.Epochs {
			if self.archiveView.claimWork != nil {
				self.archiveView.claimWork(window.Role, "retained-epoch", 1)
			}
			current := self.ClaimStates[index].Epochs[offset]
			if current.Epoch != prior.Epoch || prior.Observation != nil && !monitorClaimEpochRetains(current, prior) {
				return errors.New("economic Claim window discarded original unresolved evidence")
			}
		}
		current := self.ClaimStates[index]
		if current.Incidents < retained.Incidents || current.Restarts < retained.Restarts || current.HighWaterAt.Before(retained.HighWaterAt) || current.SampleAt.Before(retained.SampleAt) {
			return errors.New("economic Claim window reset original incident or source progress")
		}
	}
	return nil
}

func (self *economicConservationArchiveView) retainClaimWindows(policy economicConservationPolicy, original *economicConservationState) error {
	for _, window := range original.ClaimWindows {
		if self.claimCheckpointPath != "" && self.claimCheckpointPath != window.Original.Path {
			return errors.New("economic Claim history moved the original checkpoint")
		}
		key := window.Role + "/" + window.ReviewSha256
		if self.claimReviews[key] {
			return errors.New("economic Claim archive reused an original window review")
		}
		if err := self.charge(window); err != nil {
			return err
		}
		// The existing window charge includes this bounded original path.
		self.claimCheckpointPath = window.Original.Path
		self.claimReviews[key] = true
		index, _, exists := economicConservationClaimRole(policy, window.Role)
		if !exists || self.claimBasis == nil {
			return errors.New("economic Claim archive lost its admitted original basis")
		}
		lookup := self.claimRetired[window.Role]
		if lookup == nil {
			lookup = &monitorClaimWindowAdmission{retired: map[int64]monitorClaimEpochState{}}
			if self.claimWork != nil {
				lookup.visit = func() { self.claimWork(window.Role, "retired-lookup", 1) }
			}
			self.claimRetired[window.Role] = lookup
		}
		retired := make(map[int64]string, len(window.Retired))
		for _, epoch := range window.Retired {
			retired[epoch.Epoch] = epoch.StateSha256
		}
		for _, epoch := range self.claimBasis.states[index].Epochs {
			if hash, exists := retired[epoch.Epoch]; exists {
				if _, duplicate := lookup.retired[epoch.Epoch]; duplicate || hash != rootObjectHash(epoch) {
					return errors.New("economic Claim archive repeated or substituted a retired receipt")
				}
				if err := self.charge(epoch); err != nil {
					return err
				}
				lookup.retired[epoch.Epoch] = cloneMonitorClaimEpoch(epoch)
			}
		}
	}
	return nil
}

// The newest window is not yet folded into the archive index. Inspect only
// its at-most128 retired entries plus the current producer census; no complete
// historical payload is read or rehydrated on a sample.
func (self *economicConservationState) claimPublicationCode(policy monitorClaimPolicy, value *protocol.ClaimProgress) string {
	if self.archiveView == nil {
		return "ok"
	}
	if code := self.archiveView.claimRetired[policy.Role].publicationCode(value); code != "ok" {
		return code
	}
	for _, window := range self.ClaimWindows {
		if window.Role != policy.Role {
			continue
		}
		if self.archiveView.claimBasis == nil {
			return "identity"
		}
		lookup := &monitorClaimWindowAdmission{retired: map[int64]monitorClaimEpochState{}}
		if self.archiveView.claimWork != nil {
			lookup.visit = func() { self.archiveView.claimWork(policy.Role, "retired-lookup", 1) }
		}
		index := slices.IndexFunc(self.archiveView.claimBasis.heads, func(head economicConservationClaimHead) bool { return head.Role == policy.Role })
		if index < 0 {
			return "identity"
		}
		retired := make(map[int64]bool, len(window.Retired))
		for _, epoch := range window.Retired {
			retired[epoch.Epoch] = true
		}
		for _, epoch := range self.archiveView.claimBasis.states[index].Epochs {
			if self.archiveView.claimWork != nil {
				self.archiveView.claimWork(policy.Role, "hot-retired-epoch", 1)
			}
			if retired[epoch.Epoch] {
				lookup.retired[epoch.Epoch] = epoch
			}
		}
		if code := lookup.publicationCode(value); code != "ok" {
			return code
		}
	}
	return "ok"
}

// A new signed window is applied only after the exact old combined snapshot
// is the next archive segment. Plan/apply use the same deterministic derivation.
func applyEconomicConservationClaimWindows(ctx context.Context, policy economicConservationPolicy, next *economicConservationState, windows []economicConservationClaimWindow) error {
	if next == nil || next.archiveView == nil || next.archiveView.claimBasis == nil {
		return errors.New("economic Claim adoption requires original checkpoint admission")
	}
	seen := map[string]bool{}
	for _, window := range windows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if seen[window.Role] {
			return errors.New("economic Claim adoption repeats an original role")
		}
		seen[window.Role] = true
		if err := window.verify(policy); err != nil {
			return err
		}
		derived, state, err := deriveEconomicConservationClaimWindow(policy, next.archiveView.claimBasis, window)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(derived, window) {
			return errors.New("economic Claim adoption differs from exact original liabilities")
		}
		index, _, _ := economicConservationClaimRole(policy, window.Role)
		next.ClaimStates[index] = state
		next.ClaimWindows = append(next.ClaimWindows, window)
	}
	next.ContentHash = next.hash()
	return next.validate(ctx, policy)
}
