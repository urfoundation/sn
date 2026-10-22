// The combined owner retains original consensus inputs alongside economic
// observations. Only complete certificate coverage promotes finality; missing
// proofs remain pending without relabeling stock, fees or provider measurements.
package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"

	"github.com/urnetwork/server/v2026/strecovery"
)

// Count the real active proof/header census in the existing signed resource
// budget. Encoded head and cold index bytes have their separate physical bounds.
func (self economicConservationState) finalityFacts() uint64 {
	result := uint64(len(self.FinalityVaultBlocks) + len(self.FinalityWindows))
	for _, window := range self.FinalityWindows {
		for _, segment := range window.Proof.Segments {
			result += uint64(len(segment.Headers))
		}
	}
	return result
}

// Legacy checkpoints have no new authority. A present consensus census cannot
// borrow a policy without a native producer or lose its retained original key.
func (self economicConservationState) validateFinality(ctx context.Context, policy economicConservationPolicy) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var cold *economicConservationFinalityHead
	if self.Archive != nil {
		cold = self.Archive.Finality
	}
	if len(self.FinalityApproval) == 0 && len(self.FinalityWindows) == 0 && len(self.FinalityVaultBlocks) == 0 && self.FinalityFrom == nil && cold == nil {
		return nil
	}
	original := economicConservationNativeBasis(policy).Native.Observation
	if original.Execution == nil || original.Execution.Producer == nil {
		return errors.New("economic consensus census lacks its original producer policy")
	}
	if len(self.FinalityApproval) > nativeProducerAuthorityMaximum(original.Execution.FeeCensus) || len(self.FinalityApproval) != 0 && monitorReadDigest(self.FinalityApproval) != original.Execution.Producer.Authority.Sha256 {
		return errors.New("economic consensus retained approval differs from original policy")
	}
	if (len(self.FinalityWindows) != 0 || cold != nil && cold.Windows != 0) && len(self.FinalityApproval) == 0 {
		return errors.New("economic consensus lost its original signed approval")
	}
	if self.FinalityFrom != nil && (!rootCanonicalHash(self.FinalityFrom.Hash) || self.FinalityFrom.Number < policy.Vault.From.Number || self.FinalityFrom.Number > self.Vault.Cursor.Number) {
		return errors.New("economic consensus changed original vault coverage start")
	}
	if cold != nil && (cold.Covered > cold.Required || cold.AuthorityHash != "" && cold.AuthorityHash != original.Execution.Producer.Authority.Sha256) {
		return errors.New("economic consensus cold counts or original authority differ")
	}
	if self.archiveView != nil {
		_, err := self.prepareFinality(ctx)
		return err
	}
	// Decoding precedes actual archive admission. No decoded head is itself
	// usable finality evidence; the public owner always admits it before use.
	return nil
}

// The companion comes only from the original producer's actual replay result.
// Its selected proof hash, parent and child stay distinct from the certified tip.
func (self *economicConservationState) appendFinality(ctx context.Context, policy economicConservationPolicy, outcome nativeExecutionOutcome, block economicEmissionBlock) error {
	projection := outcome.CertifiedWindow
	if projection == nil {
		return nil
	}
	if self.archiveView == nil && self.Archive == nil {
		resources, err := self.resources(policy)
		if err != nil {
			return err
		}
		view := newEconomicConservationArchiveView(resources)
		original := economicConservationNativeBasis(policy).Native.Observation
		view.finalityPolicy = &original
		self.archiveView = view
	}
	if self.archiveView == nil || !outcome.AmountsAuthenticated || outcome.ContentHash != outcome.hash() || projection.OutcomeHash != outcome.ContentHash || projection.ProofHash != outcome.FinalityProofHash || projection.Child != outcome.Boundary || projection.Child != block.Boundary || projection.Parent.Hash != block.Header.ParentHash || projection.Parent.Number >= uint64(^uint32(0)) || projection.Parent.Number+1 != projection.Child.Number {
		return errors.Join(errRpcIntegrity, errors.New("economic consensus companion differs from actual original execution"))
	}
	if len(self.FinalityApproval) == 0 {
		self.FinalityApproval = append([]byte(nil), projection.Approval...)
	}
	if !bytes.Equal(self.FinalityApproval, projection.Approval) {
		return errors.Join(errRpcIntegrity, errors.New("economic consensus replaced its original signed producer approval"))
	}
	selected := projection.Window.Proof
	selected.Parent = strecovery.ObservedBlockIdentity{Number: projection.Parent.Number, Hash: projection.Parent.Hash}
	selected.Child = strecovery.ObservedBlockIdentity{Number: projection.Child.Number, Hash: projection.Child.Hash}
	if rootObjectHash(selected) != projection.ProofHash {
		return errors.Join(errRpcIntegrity, errors.New("economic consensus selected proof differs from original replay"))
	}
	key, found := rootObjectHash(projection.Window), false
	if self.archiveView.finality != nil {
		found = self.archiveView.finality.hasWindow(key)
	}
	for _, prior := range self.FinalityWindows {
		found = found || rootObjectHash(prior) == key
	}
	if !found {
		self.FinalityWindows = append(self.FinalityWindows, projection.Window)
	}
	if self.facts() > policy.MaximumFacts {
		return errMonitorEconomicCapacity
	}
	index, err := self.prepareFinality(ctx)
	if err != nil {
		return err
	}
	parent, parentKnown := index.header(projection.Parent.Hash)
	child, childKnown := index.header(projection.Child.Hash)
	if !parentKnown || !childKnown || parent.Native != projection.Parent || child.Native != projection.Child {
		return errors.Join(errRpcIntegrity, errors.New("economic original execution is outside its admitted certified ancestry"))
	}
	return nil
}

// Quiet blocks matter too: the independently authenticated interval must cover
// the complete vault reader progression, not merely blocks containing payouts.
func (self *economicFinalityIndex) requireOriginalBoundaries(ctx context.Context, state *economicConservationState) error {
	if state.FinalityFrom != nil {
		if self.head.VaultFrom.Hash == "" {
			self.head.VaultFrom, self.head.VaultThrough = *state.FinalityFrom, *state.FinalityFrom
		} else if self.head.VaultFrom != *state.FinalityFrom {
			return errors.New("economic consensus rewrote the original vault coverage start")
		}
		if err := self.require(*state.FinalityFrom); err != nil {
			return err
		}
	} else if len(state.FinalityVaultBlocks) != 0 || self.head.VaultFrom.Hash != "" {
		return errors.New("economic consensus lost its original vault coverage start")
	}
	for _, boundary := range state.FinalityVaultBlocks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if self.head.VaultThrough.Number == ^uint64(0) || boundary.Number != self.head.VaultThrough.Number+1 || boundary.Number > state.Vault.Cursor.Number {
			return errors.New("economic consensus vault census skipped an original block")
		}
		if err := self.require(boundary); err != nil {
			return err
		}
		self.head.VaultThrough = boundary
		self.head.VaultBlocks++
	}
	if state.FinalityFrom != nil && self.head.VaultThrough != state.Vault.Cursor {
		return errors.New("economic consensus vault census differs from original observed cursor")
	}
	for _, event := range state.Vault.History {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := self.require(event.Block); err != nil {
			return err
		}
	}
	for _, capture := range state.Captures {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := self.require(capture.Event.Block); err != nil {
			return err
		}
	}
	for _, entitlement := range state.Entitlements {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, event := range []*monitorEconomicEvmEvent{entitlement.Finalization, entitlement.CarryEvent} {
			if event != nil {
				if err := self.require(event.Block); err != nil {
					return err
				}
			}
		}
		if census := entitlement.Census; census != nil {
			for _, boundary := range []economicEmissionBoundary{census.Finalization.Block, census.Commitment.Block, census.CoordinatorFinalization.Block, {Number: census.Start.Number, Hash: census.Start.Hash}, {Number: census.End.Number, Hash: census.End.Hash}} {
				if err := self.require(boundary); err != nil {
					return err
				}
			}
		}
	}
	for _, claim := range state.Claims {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := self.require(claim.Event.Block); err != nil {
			return err
		}
	}
	for _, payment := range state.Payments {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := self.require(payment.Event.Block); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// A summary is independently complete only for the exact original interval.
// A certificate for a later native tip never advances the economic cursors.
func (self *economicConservationState) finalitySummary(ctx context.Context, policy economicConservationPolicy) (*economicConservationFinalitySummary, error) {
	var cold *economicConservationFinalityHead
	if self.Archive != nil {
		cold = self.Archive.Finality
	}
	if self.FinalityFrom == nil && len(self.FinalityWindows) == 0 && cold == nil {
		return nil, ctx.Err()
	}
	index, err := self.prepareFinality(ctx)
	if err != nil {
		return nil, err
	}
	if index.head.Covered > index.head.Required {
		return nil, errors.New("economic consensus covered count exceeds original requirements")
	}
	result := &economicConservationFinalitySummary{Head: economicFinalityHead(index), Missing: index.head.Required - index.head.Covered}
	result.Complete = index.head.Windows != 0 && result.Missing == 0 && index.head.VaultFrom == policy.Vault.From && index.head.VaultThrough == self.Vault.Cursor && index.head.VaultThrough.Number >= index.head.VaultFrom.Number && index.head.VaultBlocks == index.head.VaultThrough.Number-index.head.VaultFrom.Number
	if err := errors.Join(ctx.Err(), self.archiveView.checkAdmission()); err != nil {
		return nil, err
	}
	return result, nil
}

// The exact precompaction snapshot retains the signed approval and original
// proof bytes. A candidate borrows a private derived index until full admission.
func (self *economicConservationState) retireFinality(ctx context.Context, original *economicConservationState) error {
	var cold *economicConservationFinalityHead
	if original.Archive != nil {
		cold = original.Archive.Finality
	}
	if original.FinalityFrom == nil && len(original.FinalityWindows) == 0 && cold == nil {
		return nil
	}
	index, err := original.prepareFinality(ctx)
	if err != nil {
		return err
	}
	self.Archive.Finality = economicFinalityHead(index)
	self.FinalityWindows, self.FinalityVaultBlocks = nil, nil
	view := *self.archiveView
	view.finality = index
	self.archiveView = &view
	return nil
}

// Charge and fence the complete delta before committing any financial index.
// The stable prefix remains flat, so repeated hot reads do not grow a chain of
// indexes or copy the complete original history on every observation.
func (self *economicConservationArchiveView) retainFinality(ctx context.Context, original, compacted *economicConservationState) error {
	if compacted.Archive == nil || compacted.Archive.Finality == nil {
		if self.finality != nil && economicFinalityHead(self.finality) != nil {
			return errors.New("economic archive dropped its original consensus head")
		}
		return nil
	}
	index, err := original.prepareFinality(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(compacted.Archive.Finality, economicFinalityHead(index)) || len(compacted.FinalityWindows) != 0 || len(compacted.FinalityVaultBlocks) != 0 || !bytes.Equal(compacted.FinalityApproval, original.FinalityApproval) {
		return errors.New("economic archive changed original consensus obligations")
	}
	charged := *self
	for key := range index.windows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := charged.charge(key); err != nil {
			return err
		}
	}
	for key, value := range index.headers {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := charged.charge(struct {
			Hash   string
			Header economicCertifiedHeader
		}{Hash: key, Header: value}); err != nil {
			return err
		}
	}
	for key, value := range index.evm {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := charged.charge(struct {
			Hash   string
			Native economicEmissionBoundary
		}{Hash: key, Native: value}); err != nil {
			return err
		}
	}
	for key, value := range index.required {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := charged.charge(struct {
			Hash     string
			Boundary economicEmissionBoundary
		}{Hash: key, Boundary: value}); err != nil {
			return err
		}
	}
	for key := range index.covered {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := charged.charge(key); err != nil {
			return err
		}
	}
	if err := errors.Join(ctx.Err(), self.checkAdmission()); err != nil {
		return err
	}
	if self.finality == nil {
		self.finality = newEconomicFinalityIndex(nil)
	}
	self.entries, self.bytes = charged.entries, charged.bytes
	self.finality.head, self.finality.checkpoint = index.head, index.checkpoint
	for key, value := range index.windows {
		self.finality.windows[key] = value
	}
	for key, value := range index.headers {
		self.finality.headers[key] = value
	}
	for key, value := range index.evm {
		self.finality.evm[key] = value
	}
	for key, value := range index.required {
		self.finality.required[key] = value
	}
	for key, value := range index.covered {
		self.finality.covered[key] = value
	}
	return nil
}
