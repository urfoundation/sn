// Complete fee accounting follows the original native body interval. The
// active head retains every unresolved original entry; complete facts retire
// only through the exact precompaction checkpoint and held archive owners.
package main

import (
	"context"
	"errors"
	"maps"
	"reflect"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/crypto/blake2b"
)

// Counts cover all extrinsics; amounts cover only the independently admitted
// original participant domain. Unknown refunds never contribute a zero.
type economicWholeFeeHead struct {
	PolicyHash         string                   `json:"original_policy_hash"`
	From               economicEmissionBoundary `json:"from"`
	Through            economicEmissionBoundary `json:"through"`
	Blocks             uint64                   `json:"blocks"`
	Extrinsics         uint64                   `json:"extrinsics"`
	Missing            uint64                   `json:"unresolved_extrinsics"`
	Unplaced           uint64                   `json:"unplaced_fee_events"`
	ParticipantCharges uint64                   `json:"participant_charges"`
	WithdrawalRao      string                   `json:"known_withdrawal_rao"`
	RefundRao          string                   `json:"known_refund_rao"`
	DebitRao           string                   `json:"known_debit_rao"`
	EvidenceChain      string                   `json:"original_evidence_chain"`
}

// Complete amounts are deliberately nullable even though known subtotals
// remain useful diagnostics while one original path is unresolved.
type economicWholeFeeSummary struct {
	Head          economicWholeFeeHead `json:"original_census"`
	Complete      bool                 `json:"complete_original_interval"`
	WithdrawalRao *string              `json:"withdrawal_rao"`
	RefundRao     *string              `json:"refund_rao"`
	DebitRao      *string              `json:"debit_rao"`
}

// Published indexes are flat and immutable. A candidate borrows its prefix
// and allocates only the new pending delta; it cannot mutate a live owner.
type economicWholeFeeIndex struct {
	head    economicWholeFeeHead
	pending map[string]string
}

// The signed participant scope includes every configured provider and payer.
// EVM senders use the original runtime's admitted evm: AccountId mapping.
func (self economicConservationPolicy) validateWholeFeeAuthority() error {
	execution := self.Native.Observation.Execution
	if execution == nil || execution.FeeCensus == nil {
		return nil
	}
	policy := execution.FeeCensus
	if err := policy.validate(); err != nil {
		return err
	}
	accounts := append([]string(nil), self.Native.Observation.FeePayers...)
	accounts = append(accounts, self.Vault.Coldkeys...)
	for _, route := range self.Routes {
		accounts = append(accounts, route.Hotkey, route.Coldkey)
	}
	for _, payer := range self.Vault.FeePayers {
		address := common.HexToAddress(payer)
		digest := blake2b.Sum256(append([]byte("evm:"), address[:]...))
		accounts = append(accounts, economicNativeFeeHash(historicalReplayDigest(digest)))
	}
	for _, account := range accounts {
		if !policy.participant(account) {
			return errors.New("economic complete fee authority omits an original route, pool or selected payer")
		}
	}
	return nil
}

// Count retained evidence, including explicit branch observations, in the
// same signed fact budget as original earnings and payment liabilities.
func (self economicConservationState) wholeFeeFacts() uint64 {
	result := uint64(len(self.OriginalFees))
	for _, block := range self.OriginalFees {
		result += uint64(len(block.Extrinsics) + len(block.Unplaced))
		for _, entry := range block.Extrinsics {
			result += uint64(len(entry.Events))
			if entry.Exemption != nil {
				result++
			}
			if entry.RefundZero != nil {
				result++
			}
		}
	}
	return result
}

// An unresolved block keeps its complete original projection in the hot head.
func (self nativeFeeCensusProjection) unresolved() bool {
	if len(self.Unplaced) != 0 {
		return true
	}
	for _, entry := range self.Extrinsics {
		if entry.Status != "original-pair" && entry.Status != "original-exempt" {
			return true
		}
	}
	return false
}

// Only an owned replay outcome is new ingress. There is no JSON projection
// import and no inferred binding from a matching aggregate amount.
func (self *economicConservationState) appendWholeFees(ctx context.Context, policy economicConservationPolicy, outcome nativeExecutionOutcome, block economicEmissionBlock) error {
	execution := policy.Native.Observation.Execution
	if execution == nil || execution.FeeCensus == nil {
		if outcome.FeeCensus != nil {
			return errors.New("economic observation cannot enroll complete fee authority")
		}
		return nil
	}
	projection := outcome.FeeCensus
	if projection == nil || !outcome.AmountsAuthenticated || outcome.ContentHash != outcome.hash() || projection.OutcomeHash != outcome.ContentHash || projection.JobHash != outcome.JobHash || projection.TraceHash != outcome.TraceHash || projection.Boundary != block.Boundary || projection.Parent.Hash != block.Header.ParentHash {
		return errors.Join(errRpcIntegrity, errors.New("economic fee companion differs from the admitted original replay"))
	}
	if err := projection.validate(ctx, execution.FeeCensus); err != nil {
		return nativeExecutionDerivationError(err)
	}
	if self.archiveView == nil && self.Archive == nil {
		resources, err := self.resources(policy)
		if err != nil {
			return err
		}
		self.archiveView = newEconomicConservationArchiveView(resources)
		original := economicConservationNativeBasis(policy).Native.Observation
		self.archiveView.finalityPolicy = &original
	}
	self.OriginalFees = append(self.OriginalFees, *projection)
	if self.facts() > policy.MaximumFacts {
		return errMonitorEconomicCapacity
	}
	return ctx.Err()
}

// Decode checks structure before archive admission. Only the admitted owner
// below can turn those retained facts into complete interval authority.
func (self *economicConservationState) validateWholeFees(ctx context.Context, policy economicConservationPolicy) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var cold *economicWholeFeeHead
	if self.Archive != nil {
		cold = self.Archive.OriginalFees
	}
	execution := economicConservationNativeBasis(policy).Native.Observation.Execution
	if execution == nil || execution.FeeCensus == nil {
		if len(self.OriginalFees) != 0 || cold != nil {
			return errors.New("economic retained fee census has no original complete authority")
		}
		return nil
	}
	if self.archiveView != nil {
		_, _, err := self.prepareWholeFees(ctx)
		return err
	}
	for _, projection := range self.OriginalFees {
		if err := projection.validate(ctx, execution.FeeCensus); err != nil {
			return err
		}
	}
	return nil
}

// Whole-body interval reduction borrows the already admitted cold prefix.
// Work is bounded by hot facts plus hot unresolved entries, not full history.
func (self *economicConservationState) prepareWholeFees(ctx context.Context) (*economicWholeFeeIndex, map[string]string, error) {
	view := self.archiveView
	if ctx == nil || view == nil || view.finalityPolicy == nil || view.finalityPolicy.Execution == nil || view.finalityPolicy.Execution.FeeCensus == nil {
		return nil, nil, errors.New("economic whole-fee census lacks original owner admission")
	}
	if err := errors.Join(ctx.Err(), view.checkAdmission()); err != nil {
		return nil, nil, err
	}
	policy := view.finalityPolicy.Execution.FeeCensus
	policyHash, from := rootObjectHash(policy), view.finalityPolicy.From
	result := &economicWholeFeeIndex{head: economicWholeFeeHead{PolicyHash: policyHash, From: from, Through: from, WithdrawalRao: "0", RefundRao: "0", DebitRao: "0", EvidenceChain: policyHash}, pending: map[string]string{}}
	var cold *economicWholeFeeHead
	if self.Archive != nil {
		cold = self.Archive.OriginalFees
	}
	if view.wholeFees != nil {
		if cold == nil || !reflect.DeepEqual(*cold, view.wholeFees.head) || cold.PolicyHash != policyHash || cold.From != from {
			return nil, nil, errors.New("economic whole-fee archive changed original interval or authority")
		}
		result.head = view.wholeFees.head
	} else if cold != nil {
		return nil, nil, errors.New("economic whole-fee head is not admitted from original snapshots")
	}
	found := make(map[string]string)
	for _, projection := range self.OriginalFees {
		if err := projection.validate(ctx, policy); err != nil {
			return nil, nil, err
		}
		if _, duplicate := found[projection.Boundary.Hash]; duplicate {
			return nil, nil, errors.New("economic whole-fee census repeats an original block")
		}
		found[projection.Boundary.Hash] = projection.ContentHash
		if cold != nil && projection.Boundary.Number <= cold.Through.Number {
			if view.wholeFees.pending[projection.Boundary.Hash] != projection.ContentHash || !projection.unresolved() {
				return nil, nil, errors.New("economic whole-fee retained pending block changed original evidence")
			}
			continue
		}
		if projection.Parent != result.head.Through {
			return nil, nil, errors.New("economic whole-fee census skipped or replaced an original body")
		}
		result.head.Through = projection.Boundary
		result.head.Blocks++
		result.head.Extrinsics += uint64(len(projection.Extrinsics))
		result.head.Unplaced += uint64(len(projection.Unplaced))
		result.head.EvidenceChain = rootObjectHash([]string{result.head.EvidenceChain, projection.ContentHash})
		for _, entry := range projection.Extrinsics {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			if entry.Status != "original-pair" && entry.Status != "original-exempt" {
				result.head.Missing++
				continue
			}
			if !entry.Participant {
				continue
			}
			result.head.ParticipantCharges++
			for _, pair := range []struct {
				target *string
				value  *string
			}{{target: &result.head.WithdrawalRao, value: entry.WithdrawalRao}, {target: &result.head.RefundRao, value: entry.RefundRao}, {target: &result.head.DebitRao, value: entry.DebitRao}} {
				if pair.value == nil {
					return nil, nil, errors.New("economic complete fee entry lost its original amount")
				}
				var err error
				*pair.target, err = economicConservationSum(*pair.target, *pair.value)
				if err != nil {
					return nil, nil, err
				}
			}
		}
		if projection.unresolved() {
			result.pending[projection.Boundary.Hash] = projection.ContentHash
		}
	}
	if view.wholeFees != nil {
		for block, hash := range view.wholeFees.pending {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			if found[block] != hash {
				return nil, nil, errors.New("economic whole-fee archive lost an unresolved original block")
			}
		}
	}
	if result.head.Through != self.Native.Cursor || result.head.Through.Number < from.Number || result.head.Blocks != result.head.Through.Number-from.Number {
		return nil, nil, errors.New("economic whole-fee coverage differs from the original native interval")
	}
	if view.wholeFeeWork != nil {
		view.wholeFeeWork(ctx, self.wholeFeeFacts())
	}
	if err := errors.Join(ctx.Err(), view.checkAdmission()); err != nil {
		return nil, nil, err
	}
	return result, found, nil
}

// Report totals only after both owner fences and exact original cursor checks.
func (self *economicConservationState) wholeFeeSummary(ctx context.Context, policy economicConservationPolicy) (*economicWholeFeeSummary, error) {
	execution := economicConservationNativeBasis(policy).Native.Observation.Execution
	if execution == nil || execution.FeeCensus == nil {
		return nil, ctx.Err()
	}
	index, _, err := self.prepareWholeFees(ctx)
	if err != nil {
		return nil, err
	}
	result := &economicWholeFeeSummary{Head: index.head, Complete: index.head.Blocks != 0 && index.head.Missing == 0 && index.head.Unplaced == 0}
	if result.Complete {
		w, r, d := index.head.WithdrawalRao, index.head.RefundRao, index.head.DebitRao
		result.WithdrawalRao, result.RefundRao, result.DebitRao = &w, &r, &d
	}
	return result, nil
}

// Immutable prefix sharing ends at retirement. New pending identities are
// flattened once per archive transition, never copied on a foreground read.
func economicWholeFeeFlatten(prefix, delta *economicWholeFeeIndex) *economicWholeFeeIndex {
	result := &economicWholeFeeIndex{head: delta.head, pending: map[string]string{}}
	if prefix != nil {
		result.pending = maps.Clone(prefix.pending)
	}
	for key, value := range delta.pending {
		result.pending[key] = value
	}
	return result
}

// The precompaction snapshot proves removed complete bodies. Unresolved bodies
// remain in the active checkpoint even if all later blocks are complete.
func (self *economicConservationState) retireWholeFees(ctx context.Context, original *economicConservationState) error {
	if len(original.OriginalFees) == 0 && (original.Archive == nil || original.Archive.OriginalFees == nil) {
		return nil
	}
	index, _, err := original.prepareWholeFees(ctx)
	if err != nil {
		return err
	}
	head := index.head
	self.Archive.OriginalFees = &head
	self.OriginalFees = nil
	for _, block := range original.OriginalFees {
		if block.unresolved() {
			self.OriginalFees = append(self.OriginalFees, block)
		}
	}
	view := *self.archiveView
	view.wholeFees = economicWholeFeeFlatten(original.archiveView.wholeFees, index)
	self.archiveView = &view
	return nil
}

// Charge the new head and pending delta before publication. A rejected later
// Claim/native adoption cannot alter a separately held prefix map or totals.
func (self *economicConservationArchiveView) retainWholeFees(ctx context.Context, original, compacted *economicConservationState) error {
	if compacted.Archive == nil || compacted.Archive.OriginalFees == nil {
		if self.wholeFees != nil {
			return errors.New("economic archive dropped its original whole-fee head")
		}
		return nil
	}
	index, _, err := original.prepareWholeFees(ctx)
	if err != nil {
		return err
	}
	retained := []nativeFeeCensusProjection(nil)
	for _, block := range original.OriginalFees {
		if block.unresolved() {
			retained = append(retained, block)
		}
	}
	if !reflect.DeepEqual(*compacted.Archive.OriginalFees, index.head) || !reflect.DeepEqual(compacted.OriginalFees, retained) {
		return errors.New("economic archive changed original whole-fee obligations")
	}
	charged := *self
	if err := charged.charge(index.head); err != nil {
		return err
	}
	for block, hash := range index.pending {
		if err := errors.Join(ctx.Err(), charged.charge(struct {
			Block string
			Hash  string
		}{Block: block, Hash: hash})); err != nil {
			return err
		}
	}
	if err := errors.Join(ctx.Err(), self.checkAdmission()); err != nil {
		return err
	}
	self.wholeFees = economicWholeFeeFlatten(self.wholeFees, index)
	self.entries, self.bytes = charged.entries, charged.bytes
	return nil
}
