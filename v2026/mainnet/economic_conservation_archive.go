// One combined archive retains exact original checkpoints. Only facts with
// an observed continuation move out of the active head; unmatched source lots,
// carry, unpaid credits and unresolved receipts remain hot. Admission decodes
// each segment once, builds a bounded index and retains every shared owner.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"

	"github.com/urnetwork/server/v2026/strecovery"
)

type economicConservationCounts struct {
	CausalCaptures uint64 `json:"original_native_receipt_captures,omitempty"`
	Mappings       uint64 `json:"mappings"`
	Lots           uint64 `json:"lots"`
	Captures       uint64 `json:"captures"`
	Entitlements   uint64 `json:"entitlements"`
	Claims         uint64 `json:"claims"`
	Payments       uint64 `json:"payments"`
	Receipts       uint64 `json:"receipts"`
}

type economicConservationArchivedAmounts struct {
	Direct     string `json:"direct_gross_alpha"`
	Tail       string `json:"tail_gross_alpha"`
	Unrouted   string `json:"unrouted_gross_alpha"`
	Collateral string `json:"reward_collateral_alpha"`
}

type economicConservationArchive struct {
	PrincipalRetained   *economicConservationPrincipalRetained  `json:"retained_original_principal,omitempty"`
	PrincipalRetentions []monitorHistoryReference               `json:"principal_retention_segments,omitempty"`
	OriginalFees        *economicWholeFeeHead                   `json:"original_complete_fee_census,omitempty"`
	Finality            *economicConservationFinalityHead       `json:"original_consensus_head,omitempty"`
	Yuma                *economicConservationYumaArchive        `json:"original_yuma_summary,omitempty"`
	NativeApprovalHead  *economicConservationNativeApprovalHead `json:"native_approval_head,omitempty"`
	PrincipalEffects    *economicConservationPrincipalArchive   `json:"original_principal_effects,omitempty"`
	ClaimHeads          []economicConservationClaimHead         `json:"claim_window_heads,omitempty"`
	FeeRevisionHead     *economicConservationFeeRevisionHead    `json:"native_fee_revision_head,omitempty"`
	FeeRetirements      []monitorHistoryReference               `json:"native_fee_retirements,omitempty"`
	NativeFees          *economicConservationFeeSummary         `json:"native_fee_census,omitempty"`
	Segments            []monitorHistoryReference               `json:"segments"`
	Resources           economicConservationResources           `json:"resources"`
	LastRenewalHash     string                                  `json:"last_renewal_hash"`
	LastRenewalOrdinal  uint64                                  `json:"last_renewal_ordinal"`
	Native              economicEmissionBoundary                `json:"native_cursor"`
	Vault               economicEmissionBoundary                `json:"vault_cursor"`
	PoolBoundaries      map[string]economicEmissionBoundary     `json:"original_pool_source_boundaries"`
	Counts              economicConservationCounts              `json:"counts"`
	Amounts             economicConservationArchivedAmounts     `json:"amounts"`
}

func (self *economicConservationArchive) validate(policy economicConservationPolicy, state *economicConservationState) error {
	if self == nil {
		return nil
	}
	resources, err := state.resources(policy)
	if err != nil {
		return err
	}
	if len(self.Segments) == 0 || uint64(len(self.Segments)) > resources.ArchiveSegments || self.Native.Number < policy.Native.Observation.From.Number || self.Native.Number > state.Native.Cursor.Number || self.Vault.Number < policy.Vault.From.Number || self.Vault.Number > state.Vault.Cursor.Number || !rootCanonicalHash(self.Native.Hash) || !rootCanonicalHash(self.Vault.Hash) {
		return errors.New("economic archive lost original progress or bounded catalog")
	}
	if self.Counts.CausalCaptures > self.Counts.Captures {
		return errors.New("economic archive causal capture count exceeds original captures")
	}
	seen := map[string]monitorHistoryReference{}
	for _, reference := range self.Segments {
		if err := policy.validateReference(reference); err != nil {
			return err
		}
		if _, ok := seen[reference.Path]; ok {
			return errors.New("economic archive repeats an original snapshot owner")
		}
		if _, ok := seen[reference.Path+".lock"]; ok {
			return errors.New("economic archive aliases a retained snapshot owner")
		}
		seen[reference.Path], seen[reference.Path+".lock"] = reference, reference
	}
	var componentReferences []monitorHistoryReference
	if state.Native.Archive != nil {
		componentReferences = append(componentReferences, state.Native.Archive.Segments...)
	}
	if state.Vault.Archive != nil {
		componentReferences = append(componentReferences, state.Vault.Archive.Segments...)
	}
	for _, reference := range componentReferences {
		if seen[reference.Path] != reference {
			return errors.New("economic component archive borrowed another owner's history")
		}
	}
	if err := self.validateFeeRetirements(seen); err != nil {
		return err
	}
	if err := self.validatePrincipalRetentions(seen); err != nil {
		return err
	}
	for _, amount := range []string{self.Amounts.Direct, self.Amounts.Tail, self.Amounts.Unrouted, self.Amounts.Collateral} {
		if _, err := monitorEconomicInteger(amount); err != nil {
			return err
		}
	}
	for pool, boundary := range self.PoolBoundaries {
		if !slices.Contains(policy.Vault.PoolIds, pool) || boundary.Number < policy.Native.Observation.From.Number || boundary.Number > self.Native.Number || !rootCanonicalHash(boundary.Hash) {
			return errors.New("economic archive changed an original capture boundary")
		}
	}
	return nil
}

// The index contains facts already admitted from retained original bytes.
// It never supplies evidence to an external caller, and survives neither owner
// replacement nor restart without authenticating the complete bounded chain.
type economicConservationArchiveView struct {
	principalSegments        []economicConservationPrincipalSegment
	principalRetained        *economicConservationPrincipalRetained
	principalCache           *economicConservationPrincipalCache
	principalReservedBytes   uint64
	providerCandidate        *economicProviderCandidate
	providerCensuses         map[string]*economicProviderAdmitted
	providerContracts        map[economicProviderContractKey]map[string]struct{}
	wholeFees                *economicWholeFeeIndex
	wholeFeeWork             func(context.Context, uint64)
	finality                 *economicFinalityIndex
	finalityVerified         map[string]*economicVerifiedFinalityWindow
	finalityAnchor           *strecovery.NativeFinalityCheckpoint
	finalityApprovalHash     string
	finalityPolicy           *economicEmissionPolicy
	finalityWork             func(context.Context)
	funding                  *economicConservationFundingIndex
	fundingWork              func(context.Context)
	admission                context.Context
	closed                   bool
	entitlementEnabled       bool
	entitlementVerified      map[string]string
	entitlementOriginal      map[string]string
	entitlementReferences    map[string]*economicConservationEntitlementReference
	entitlementRequired      map[string]string
	entitlementLeaves        map[string]map[string]string
	entitlementClaimIds      map[string][]string
	entitlementFundingUses   map[string]string
	entitlementCold          *economicConservationEntitlementSummary
	nativeReviews            map[string]bool
	principalExecutions      map[uint64]economicConservationPrincipalExecution
	captureEffectHeads       map[string]economicConservationCaptureEffects
	yuma                     *economicConservationYumaArchive
	yumaHashes               map[string]string
	principalEffects         *economicConservationPrincipalArchive
	principalExecutionHashes map[string]string
	openingPrincipalHash     string
	claimBasis               *economicConservationClaimBasis
	claimBasisEntries        uint64
	claimBasisBytes          uint64
	claimCheckpointPath      string
	claimWork                func(role, stage string, units uint64)
	claimReviews             map[string]bool
	claimRetired             map[string]*monitorClaimWindowAdmission
	feePolicies              map[string]economicNativeFeePolicy
	feeReviews               map[string]bool
	feeEvidence              map[string]string
	feeRetired               map[string]string
	feeTransactions          map[string]historicalFeeContextTransaction
	feeOrigins               map[string]economicConservationFeeObligation
	feeSummary               *economicConservationFeeSummary
	owners                   []*monitorHistorySnapshot
	copiedPrincipalRead      func(context.Context, monitorHistoryReference) ([]byte, error)
	copiedSourceContext      context.Context
	copiedSourceOnly         bool
	resources                economicConservationResources
	entries                  uint64
	bytes                    uint64
	mappings                 map[string]economicConservationMapping
	lotIds                   map[string]bool
	captureKeys              map[string]string
	claimKeys                map[string]string
	claims                   map[string]economicConservationClaim
	entitlements             map[string]economicConservationEntitlement
	receipts                 map[string]economicConservationReceipt
	reviews                  map[string]bool
}

func newEconomicConservationArchiveView(resources economicConservationResources) *economicConservationArchiveView {
	return &economicConservationArchiveView{finalityVerified: map[string]*economicVerifiedFinalityWindow{}, principalExecutions: map[uint64]economicConservationPrincipalExecution{}, captureEffectHeads: map[string]economicConservationCaptureEffects{}, yumaHashes: map[string]string{}, entitlementVerified: map[string]string{}, entitlementOriginal: map[string]string{}, entitlementReferences: map[string]*economicConservationEntitlementReference{}, entitlementRequired: map[string]string{}, entitlementLeaves: map[string]map[string]string{}, entitlementClaimIds: map[string][]string{}, entitlementFundingUses: map[string]string{}, nativeReviews: map[string]bool{}, principalExecutionHashes: map[string]string{}, claimReviews: map[string]bool{}, claimRetired: map[string]*monitorClaimWindowAdmission{}, feePolicies: map[string]economicNativeFeePolicy{}, feeReviews: map[string]bool{}, feeEvidence: map[string]string{}, feeRetired: map[string]string{}, feeTransactions: map[string]historicalFeeContextTransaction{}, feeOrigins: map[string]economicConservationFeeObligation{}, resources: resources, mappings: map[string]economicConservationMapping{}, lotIds: map[string]bool{}, captureKeys: map[string]string{}, claimKeys: map[string]string{}, claims: map[string]economicConservationClaim{}, entitlements: map[string]economicConservationEntitlement{}, receipts: map[string]economicConservationReceipt{}, reviews: map[string]bool{}}
}

// The encoded facts and fixed per-entry bookkeeping have separate bounds.
// The two-times margin is explicit; it is not a claim about measured host RSS.
func (self *economicConservationArchiveView) charge(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	bytes := uint64(len(raw)) + 256
	entryLimit, byteLimit := self.resources.IndexEntries/2, self.resources.IndexBytes/2
	if self.principalReservedBytes > byteLimit {
		return errMonitorEconomicCapacity
	}
	byteLimit -= self.principalReservedBytes
	if self.claimBasisEntries >= entryLimit || self.entries >= entryLimit-self.claimBasisEntries || self.claimBasisBytes > byteLimit || self.bytes > byteLimit-self.claimBasisBytes || bytes > byteLimit-self.claimBasisBytes-self.bytes {
		return errors.Join(errMonitorEconomicCapacity, errors.New("economic archive index needs reviewed entry/byte capacity before admission"))
	}
	self.entries++
	self.bytes += bytes
	return nil
}

func (self *economicConservationArchiveView) check() error {
	if self == nil {
		return nil
	}
	if self.closed {
		return os.ErrClosed
	}
	if self.copiedSourceOnly {
		if self.copiedSourceContext == nil || self.copiedPrincipalRead == nil {
			return errors.New("economic copied-source review lost its original reader")
		}
		if err := self.copiedSourceContext.Err(); err != nil {
			return err
		}
	}
	for _, owner := range self.owners {
		if self.claimWork != nil {
			self.claimWork(economicConservationRole, "archive-custody-check", 1)
		}
		if err := owner.check(); err != nil {
			return err
		}
	}
	return nil
}

// Copied originals can prove restore contents, but only reopened physical
// snapshot owners authorize a live worker. The caller's existing complete
// custody fence still runs immediately before its dependent operation.
func (self *economicConservationArchiveView) requireLive() error {
	if self != nil && self.copiedSourceOnly {
		return errors.New("economic copied-source review cannot authorize live work")
	}
	if self != nil && self.closed {
		return os.ErrClosed
	}
	return nil
}

func (self *economicConservationArchiveView) close() error {
	if self == nil || self.closed {
		return nil
	}
	self.closed, self.admission = true, nil
	var result error
	for _, owner := range self.owners {
		result = errors.Join(result, owner.close())
	}
	self.owners, self.principalCache = nil, nil
	self.copiedPrincipalRead = nil
	self.copiedSourceContext = nil
	return result
}

func economicConservationReceiptKey(value economicConservationReceipt) string {
	return value.Role + "/" + fmt.Sprint(value.Epoch)
}

// Rechecking the same original transaction may refresh observation time. It
// cannot rewrite acceptance, payer, payment or original block evidence. Keep
// the first archived bytes instead of charging another slot for bookkeeping.
func economicConservationSameReceipt(prior, current economicConservationReceipt) bool {
	prior.Observation.ObservedAt, current.Observation.ObservedAt = "", ""
	return reflect.DeepEqual(prior, current)
}

func (self *economicConservationArchiveView) retainedReceipt(receipt economicConservationReceipt) (bool, error) {
	if self == nil {
		return false, nil
	}
	prior, exists := self.receipts[economicConservationReceiptKey(receipt)]
	if exists && !economicConservationSameReceipt(prior, receipt) {
		return false, errors.New("economic archived original Claim receipt contradicts retained evidence")
	}
	return exists, nil
}

// Unknown payment details remain an active obligation. A later observation
// of the same receipt may fill them; later separate payments cannot revise a
// known receipt's deferred/paid fields.
func economicConservationReceiptArchivable(receipt economicConservationReceipt) bool {
	return receipt.ClaimId != "" && monitorClaimPaymentKnown(&receipt.Observation)
}

// Pure compaction retains all active liabilities and exact source references.
// Component summaries refer to this combined snapshot; their earlier prefix
// is retained by the outer complete chain rather than a second owner catalog.
func compactEconomicConservation(ctx context.Context, policy economicConservationPolicy, original *economicConservationState, reference monitorHistoryReference, renewal *economicConservationRenewal) (*economicConservationState, error) {
	return compactEconomicConservationWithFeeRetirement(ctx, policy, original, reference, renewal, false)
}

// Retirement is recorded for this exact snapshot. Earlier snapshots retain
// their original compaction grammar, including fee reports deliberately hot.
func compactEconomicConservationWithFeeRetirement(ctx context.Context, policy economicConservationPolicy, original *economicConservationState, reference monitorHistoryReference, renewal *economicConservationRenewal, retireFees bool) (*economicConservationState, error) {
	return compactEconomicConservationWithFeeUpdates(ctx, policy, original, reference, renewal, retireFees, nil)
}

func compactEconomicConservationWithFeeUpdates(ctx context.Context, policy economicConservationPolicy, original *economicConservationState, reference monitorHistoryReference, renewal *economicConservationRenewal, retireFees bool, feeRevision *economicConservationFeeRevision) (*economicConservationState, error) {
	return compactEconomicConservationWithPrincipalRetention(ctx, policy, original, reference, renewal, retireFees, feeRevision, false)
}

// The explicit selector retains unresolved originals without claiming their
// causes complete. Its per-segment marker preserves every older replay grammar.
func compactEconomicConservationWithPrincipalRetention(ctx context.Context, policy economicConservationPolicy, original *economicConservationState, reference monitorHistoryReference, renewal *economicConservationRenewal, retireFees bool, feeRevision *economicConservationFeeRevision, retainPrincipal bool) (*economicConservationState, error) {
	policy = policy.withStorageProfile()
	native, err := original.nativeOperatingPolicy(policy)
	if err != nil {
		return nil, err
	}
	if err := original.Native.admitRuntime(ctx, native); err != nil {
		return nil, err
	}
	if err := errors.Join(original.validate(ctx, policy), policy.validateReference(reference)); err != nil {
		return nil, err
	}
	next, err := cloneEconomicConservation(original, policy)
	if err != nil {
		return nil, err
	}
	resources, err := original.resources(policy)
	if err != nil {
		return nil, err
	}
	archive := &economicConservationArchive{Resources: resources, LastRenewalHash: policy.identityHash(), Native: original.Native.Cursor, Vault: original.Vault.Cursor, PoolBoundaries: map[string]economicEmissionBoundary{}, Amounts: economicConservationArchivedAmounts{Direct: "0", Tail: "0", Unrouted: "0", Collateral: "0"}}
	if next.Archive != nil {
		archive.PrincipalRetained = next.Archive.PrincipalRetained
		archive.PrincipalRetentions = slices.Clone(next.Archive.PrincipalRetentions)
		archive.NativeApprovalHead = next.Archive.NativeApprovalHead
		archive.PrincipalEffects = next.Archive.PrincipalEffects
		archive.FeeRevisionHead = next.Archive.FeeRevisionHead
		archive.FeeRetirements, archive.NativeFees = slices.Clone(next.Archive.FeeRetirements), next.Archive.NativeFees
		archive.Segments = slices.Clone(next.Archive.Segments)
		archive.Counts, archive.Amounts = next.Archive.Counts, next.Archive.Amounts
		archive.LastRenewalHash, archive.LastRenewalOrdinal = next.Archive.LastRenewalHash, next.Archive.LastRenewalOrdinal
		for pool, boundary := range next.Archive.PoolBoundaries {
			archive.PoolBoundaries[pool] = boundary
		}
	}
	if original.Renewal != nil {
		archive.LastRenewalHash, archive.LastRenewalOrdinal = rootObjectHash(original.Renewal), original.Renewal.Ordinal
	}
	if original.FeeRevision != nil {
		archive.FeeRevisionHead = &economicConservationFeeRevisionHead{Hash: rootObjectHash(original.FeeRevision), Ordinal: original.FeeRevision.Ordinal, Policy: original.FeeRevision.To}
	}
	if original.NativeRenewal != nil {
		head, err := original.nativeApprovalHead(policy)
		if err != nil {
			return nil, err
		}
		archive.NativeApprovalHead = &head
	}
	claimHeads, err := original.claimHeads(policy)
	if err != nil {
		return nil, err
	}
	for _, head := range claimHeads {
		if head.Ordinal != 0 {
			archive.ClaimHeads = append(archive.ClaimHeads, head)
		}
	}
	for _, prior := range archive.Segments {
		if monitorHistoryPathsAlias(prior.Path, reference.Path) {
			return nil, errors.New("economic archive aliases an original retained segment")
		}
	}
	archive.Segments = append(archive.Segments, reference)
	next.Archive, next.Renewal = archive, renewal
	next.FeeRevision = feeRevision
	next.NativeRenewal = nil
	next.ClaimWindows = nil
	// Only the exact newly admitted policy can release this typed policy hold.
	// Hash, signature and original-proof contradictions remain quarantined.
	if feeRevision != nil && next.NativeFeeHeldPolicy == rootObjectHash(feeRevision.To) {
		next.NativeFeeHeldPolicy, next.NativeFeeHeldRequest, next.NativeFeeIssue = "", "", ""
		next.NativeFeePending = false
	}
	if len(next.Native.History) != 0 {
		nativePolicy, err := original.nativeOperatingPolicy(policy)
		if err != nil {
			return nil, err
		}
		if err := next.Native.admitRuntime(ctx, nativePolicy); err != nil {
			return nil, err
		}
		checkpoint := monitorEconomicNativeCheckpoint{Schema: monitorEconomicNativeCheckpointSchema, PolicyHash: nativePolicy.identityHash(), State: next.Native}
		checkpoint.ContentHash = checkpoint.hash()
		record, err := compactMonitorEconomicNative(checkpoint, reference, nativePolicy)
		if err != nil {
			return nil, err
		}
		next.Native = record.State
		next.Native.Archive.Segments = []monitorHistoryReference{reference}
	}
	if len(next.Vault.History)+len(next.Vault.Fees) != 0 {
		checkpoint := monitorEconomicEvmCheckpoint{Schema: monitorEconomicEvmCheckpointSchema, PolicyHash: policy.Vault.identityHash(), State: next.Vault}
		checkpoint.ContentHash = checkpoint.hash()
		record, err := compactMonitorEconomicEvm(checkpoint, reference, policy.Vault)
		if err != nil {
			return nil, err
		}
		next.Vault = record.State
		next.Vault.Archive.Segments = []monitorHistoryReference{reference}
	}
	usedLots, blockedPools := map[string]bool{}, map[string]bool{}
	next.Captures = nil
	for _, capture := range original.Captures {
		pool := capture.Event.Values["noId"]
		// A mapped difference is still an unmatched obligation. Keep the
		// complete unresolved suffix and its lots until evidence resolves it.
		if blockedPools[pool] || capture.Native == nil || capture.AmountDifferenceAlpha == nil || *capture.AmountDifferenceAlpha != "0" && !capture.causalComplete() {
			blockedPools[pool] = true
			next.Captures = append(next.Captures, capture)
			continue
		}
		if capture.causalComplete() {
			archive.Counts.CausalCaptures++
		}
		archive.Counts.Captures++
		archive.PoolBoundaries[pool] = *capture.Native
		for _, id := range capture.Lots {
			usedLots[id] = true
		}
	}
	next.Lots = nil
	for _, lot := range original.Lots {
		if lot.Effect.Provider && (lot.Route == nil || lot.Route.Kind == "tail-pool" && !usedLots[lot.Id]) {
			next.Lots = append(next.Lots, lot)
			continue
		}
		archive.Counts.Lots++
		if !lot.Effect.Provider {
			continue
		}
		target := &archive.Amounts.Direct
		if lot.Route == nil {
			target = &archive.Amounts.Unrouted
		} else if lot.Route.Kind == "tail-pool" {
			target = &archive.Amounts.Tail
		}
		*target, err = economicConservationSum(*target, lot.Effect.Gross)
		if err != nil {
			return nil, err
		}
		archive.Amounts.Collateral, err = economicConservationSum(archive.Amounts.Collateral, lot.Effect.Collateral)
		if err != nil {
			return nil, err
		}
	}
	archive.Counts.Mappings += uint64(len(original.Mappings))
	next.Mappings = nil
	pendingClaims, paidClaims, pendingEntitlements := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, credit := range original.Credits {
		for _, id := range credit.Claims {
			pendingClaims[id] = true
		}
	}
	for _, carry := range original.Carry {
		for _, source := range carry {
			pendingEntitlements[source.Id] = true
		}
	}
	next.Payments = nil
	for _, payment := range original.Payments {
		if payment.Status != "aggregate-credit-observed" {
			next.Payments = append(next.Payments, payment)
			continue
		}
		archive.Counts.Payments++
		for _, id := range payment.Credit.Claims {
			paidClaims[id] = true
		}
	}
	next.Claims = nil
	for _, claim := range original.Claims {
		if pendingClaims[claim.Id] || !paidClaims[claim.Id] {
			next.Claims = append(next.Claims, claim)
			pendingEntitlements[claim.Entitlement] = true
			continue
		}
		archive.Counts.Claims++
	}
	next.Entitlements = nil
	for _, entitlement := range original.Entitlements {
		terminal := entitlement.Status == "root-missed" || entitlement.Status == "carried" || entitlement.Status == "finalized" && entitlement.Total != nil && entitlement.Claimed == *entitlement.Total
		// A fully paid root is still Finalized in the actual contract and may
		// emit EntitlementExpired later. Keep its compact lifecycle until then.
		if policy.EntitlementSources != nil && entitlement.Status == "finalized" {
			terminal = false
		}
		if !terminal || pendingEntitlements[entitlement.Id] || policy.EntitlementSources != nil && entitlement.PayoutRoot != "" && entitlement.censusHash() == "" {
			entitlement.retireCensus(reference)
			next.Entitlements = append(next.Entitlements, entitlement)
			continue
		}
		archive.Counts.Entitlements++
	}
	next.Receipts = nil
	for _, receipt := range original.Receipts {
		if !economicConservationReceiptArchivable(receipt) {
			next.Receipts = append(next.Receipts, receipt)
			continue
		}
		known, err := original.archiveView.retainedReceipt(receipt)
		if err != nil {
			return nil, err
		}
		if !known {
			archive.Counts.Receipts++
		}
	}
	if retireFees && len(original.NativeFees) != 0 {
		if err := next.retireNativeFees(policy, original, reference); err != nil {
			return nil, err
		}
	}
	if err := next.retireFinality(ctx, original); err != nil {
		return nil, err
	}
	if err := next.retireWholeFees(ctx, original); err != nil {
		return nil, err
	}
	if err := next.retireYuma(); err != nil {
		return nil, err
	}
	if err := next.retainPrincipalOriginals(reference, retainPrincipal); err != nil {
		return nil, err
	}
	if archive.retainsPrincipal(reference) {
		// Candidate validation must still see the authenticated originals
		// removed from its hot head before the new snapshot is published.
		next.principalProvisional = &economicConservationPrincipalProvisional{ctx: ctx, view: original.archiveView, reference: reference, originalHash: original.ContentHash, valuesHash: rootObjectHash(original.PrincipalExecutions), values: original.PrincipalExecutions}
	}
	next.entitlementIds, next.claimIds, next.captureKeys, next.claimKeys = nil, nil, nil, nil
	next.ContentHash = next.hash()
	return next, next.validate(ctx, policy)
}

func economicConservationRetainedIds[T any](values []T, id func(T) string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[id(value)] = true
	}
	return result
}

// Admission indexes only the facts removed by the deterministic compaction.
// An old unresolved record appearing in multiple snapshots is not new income.
func (self *economicConservationArchiveView) admit(ctx context.Context, original, compacted *economicConservationState) error {
	retireFees, err := self.indexAdmission(original, compacted)
	if err != nil {
		return err
	}
	if err := self.retainFinality(ctx, original, compacted); err != nil {
		return err
	}
	if err := self.retainWholeFees(ctx, original, compacted); err != nil {
		return err
	}
	if err := self.retainFundingComposition(ctx, original, compacted); err != nil {
		return err
	}
	if err := self.retainEntitlementCensuses(original, compacted); err != nil {
		return err
	}
	if err := self.retainYuma(original, compacted); err != nil {
		return err
	}
	if err := self.retainPrincipalEffects(original, compacted); err != nil {
		return err
	}
	if err := self.retainOpeningPrincipal(original); err != nil {
		return err
	}
	if err := self.retainFeeRevision(original); err != nil {
		return err
	}
	if err := self.retainNativeApproval(original); err != nil {
		return err
	}
	if err := self.retainFeeEvidence(original); err != nil {
		return err
	}
	if retireFees {
		if err := self.indexRetiredNativeFees(original, compacted.Archive.NativeFees); err != nil {
			return err
		}
	}
	if original.Renewal != nil {
		if self.reviews[original.Renewal.ReviewSha256] {
			return errors.New("economic archive reused an original operational review")
		}
		if err := self.charge(original.Renewal); err != nil {
			return err
		}
		self.reviews[original.Renewal.ReviewSha256] = true
	}
	for _, mapping := range original.Mappings {
		if prior, ok := self.mappings[mapping.EvmHash]; ok {
			if prior != mapping {
				return errors.New("economic archived native/EVM identity conflicts")
			}
			continue
		}
		if err := self.charge(mapping); err != nil {
			return err
		}
		self.mappings[mapping.EvmHash] = mapping
	}
	retainedLots := economicConservationRetainedIds(compacted.Lots, func(value economicConservationLot) string { return value.Id })
	for _, lot := range original.Lots {
		if retainedLots[lot.Id] {
			continue
		}
		if self.lotIds[lot.Id] {
			return errors.New("economic archived earning occurrence is duplicated")
		}
		if err := self.charge(lot.Id); err != nil {
			return err
		}
		self.lotIds[lot.Id] = true
	}
	retainedCaptures := economicConservationRetainedIds(compacted.Captures, func(value economicConservationCapture) string { return value.Id })
	for _, capture := range original.Captures {
		if retainedCaptures[capture.Id] {
			continue
		}
		key := economicConservationEntitlementId(capture.Event.Values["epoch"], capture.Event.Values["noId"])
		if _, exists := self.captureKeys[key]; exists {
			return errors.New("economic archived capture interval is duplicated")
		}
		if err := self.charge([]string{key, capture.Id}); err != nil {
			return err
		}
		if capture.causalComplete() {
			if err := self.charge(capture.PrincipalEffects); err != nil {
				return err
			}
			self.captureEffectHeads[capture.Event.Values["noId"]] = *capture.PrincipalEffects
		}
		self.captureKeys[key] = capture.Id
	}
	retainedClaims := economicConservationRetainedIds(compacted.Claims, func(value economicConservationClaim) string { return value.Id })
	for _, claim := range original.Claims {
		if retainedClaims[claim.Id] {
			continue
		}
		key := claim.Entitlement + "/" + claim.Event.Values["coldkey"]
		if _, exists := self.claimKeys[key]; exists {
			return errors.New("economic archived accepted leaf is duplicated")
		}
		if err := self.charge(claim); err != nil {
			return err
		}
		if self.entitlementEnabled {
			if err := self.charge([]string{claim.Entitlement, claim.Id}); err != nil {
				return err
			}
			self.entitlementClaimIds[claim.Entitlement] = append(self.entitlementClaimIds[claim.Entitlement], claim.Id)
		}
		self.claimKeys[key], self.claims[claim.Id] = claim.Id, claim
	}
	retainedEntitlements := economicConservationRetainedIds(compacted.Entitlements, func(value economicConservationEntitlement) string { return value.Id })
	for _, entitlement := range original.Entitlements {
		if retainedEntitlements[entitlement.Id] {
			continue
		}
		if _, exists := self.entitlements[entitlement.Id]; exists {
			return errors.New("economic archived terminal entitlement was rewritten")
		}
		if err := self.charge(entitlement); err != nil {
			return err
		}
		if self.entitlementEnabled {
			if err := self.indexColdEntitlement(entitlement); err != nil {
				return err
			}
			for _, source := range entitlement.Sources {
				key := source.Kind + "/" + source.Id
				if prior := self.entitlementFundingUses[key]; prior != "" {
					if prior != entitlement.Id {
						return errors.New("economic archived funding was consumed by two original entitlements")
					}
					continue
				}
				if err := self.charge([]string{key, entitlement.Id}); err != nil {
					return err
				}
				self.entitlementFundingUses[key] = entitlement.Id
			}
		}
		self.entitlements[entitlement.Id] = entitlement
		delete(self.entitlementRequired, entitlement.Id)
	}
	for _, receipt := range original.Receipts {
		if !economicConservationReceiptArchivable(receipt) {
			continue
		}
		key := economicConservationReceiptKey(receipt)
		known, err := self.retainedReceipt(receipt)
		if err != nil {
			return err
		}
		if known {
			continue
		}
		if err := self.charge(receipt); err != nil {
			return err
		}
		self.receipts[key] = receipt
	}
	return nil
}

// Receipt-only legacy indexing has no archive or fee retirement. A declared
// archive must name valid original segments before any derived index changes;
// nil operands, empty catalogs and lost custody are errors, never panics.
func (self *economicConservationArchiveView) indexAdmission(original, compacted *economicConservationState) (bool, error) {
	if self == nil || original == nil || compacted == nil {
		return false, errors.New("economic archive index requires original and compacted states")
	}
	if err := self.checkAdmission(); err != nil {
		return false, err
	}
	archive := compacted.Archive
	if archive == nil {
		return false, nil
	}
	if len(archive.Segments) == 0 {
		return false, errors.New("economic archive index requires a nonempty original segment catalog")
	}
	segments := make(map[string]monitorHistoryReference, len(archive.Segments))
	for _, reference := range archive.Segments {
		if err := reference.validateLimit(self.resources.headBytes()); err != nil {
			return false, err
		}
		if _, repeated := segments[reference.Path]; repeated {
			return false, errors.New("economic archive index repeats an original segment")
		}
		segments[reference.Path] = reference
	}
	if err := archive.validateFeeRetirements(segments); err != nil {
		return false, err
	}
	if err := archive.validatePrincipalRetentions(segments); err != nil {
		return false, err
	}
	return archive.retiresFees(archive.Segments[len(archive.Segments)-1]), nil
}

func openEconomicConservationArchive(ctx context.Context, policy economicConservationPolicy, state *economicConservationState, hooks monitorServiceHooks) (*economicConservationArchiveView, error) {
	return readEconomicConservationArchive(ctx, policy, state, hooks, policy.openHistoryReader, nil)
}

// Foreground admission retains each original snapshot owner. Offline restore
// supplies a copied-source reader instead and discards the returned index before
// producing a preparation request; it cannot turn that index into a live owner.
func readEconomicConservationArchive(ctx context.Context, policy economicConservationPolicy, state *economicConservationState, hooks monitorServiceHooks, read func(context.Context, monitorHistoryReference) (*monitorHistorySnapshot, []byte, error), copiedPrincipalRead func(context.Context, monitorHistoryReference) ([]byte, error)) (_ *economicConservationArchiveView, resultErr error) {
	if ctx == nil || state == nil || read == nil {
		return nil, errors.New("economic history requires an exact current checkpoint and reader")
	}
	resources, err := state.resources(policy)
	if err != nil {
		return nil, err
	}
	view := newEconomicConservationArchiveView(resources)
	view.admission = ctx
	view.copiedPrincipalRead, view.copiedSourceOnly = copiedPrincipalRead, copiedPrincipalRead != nil
	if view.copiedSourceOnly {
		view.copiedSourceContext = ctx
	}
	finalityPolicy := economicConservationNativeBasis(policy).Native.Observation
	view.finalityPolicy = &finalityPolicy
	view.entitlementEnabled = policy.EntitlementSources != nil
	view.claimWork = hooks.economicClaimWork
	view.fundingWork = hooks.economicFundingWork
	view.finalityWork = hooks.economicFinalityWork
	if policy.FeeAuthority != nil {
		view.feeReviews[policy.FeeAuthority.ReviewSha256] = true
	}
	if policy.Continuation != nil {
		view.reviews[policy.Continuation.ReviewSha256] = true
	}
	if execution := policy.Native.Observation.Execution; execution != nil {
		view.nativeReviews[execution.ReviewSha256] = true
	}
	for _, claim := range economicConservationClaimBasisPolicy(policy).Claims {
		if claim.HistoryCatalog != nil {
			view.claimReviews[claim.Role+"/"+claim.HistoryCatalog.ReviewSha256] = true
		}
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, view.close())
		}
	}()
	if state.Archive == nil {
		if err := view.admitEntitlementCensuses(ctx, policy, state); err != nil {
			return nil, err
		}
		checked := *state
		checked.archiveView = view
		if err := checked.validateFinality(ctx, policy); err != nil {
			return nil, err
		}
		if err := checked.validateWholeFees(ctx, policy); err != nil {
			return nil, err
		}
		if err := checked.reconcileEntitlementLeaves(ctx); err != nil {
			return nil, err
		}
		if err := view.finishAdmission(); err != nil {
			return nil, err
		}
		return view, nil
	}
	if err := state.Archive.validate(policy, state); err != nil {
		return nil, err
	}
	var prior *economicConservationArchive
	for _, reference := range state.Archive.Segments {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hooks.beforeHistoryRead(economicConservationRole, "conservation-archive-admission")
		owner, raw, err := read(ctx, reference)
		if err != nil {
			return nil, err
		}
		if owner != nil {
			view.owners = append(view.owners, owner)
			if view.copiedSourceOnly {
				return nil, errors.New("economic copied-source review cannot borrow live snapshot custody")
			}
		}
		var original economicConservationState
		if err := decodeMonitorHistoryInput(raw, &original); err != nil {
			return nil, err
		}
		if view.claimWork != nil {
			view.claimWork(economicConservationRole, "archive-checkpoint-decoded", 1)
		}
		if !reflect.DeepEqual(original.Archive, prior) {
			return nil, errors.New("economic archive omitted or changed its complete predecessor")
		}
		original.archiveView = view
		if err := view.admitEntitlementCensuses(ctx, policy, &original); err != nil {
			return nil, err
		}
		compacted, err := compactEconomicConservationWithPrincipalRetention(ctx, policy, &original, reference, nil, state.Archive.retiresFees(reference), nil, state.Archive.retainsPrincipal(reference))
		if err != nil {
			return nil, err
		}
		if err := view.admit(ctx, &original, compacted); err != nil {
			return nil, err
		}
		if err := view.retainClaimWindows(policy, &original); err != nil {
			return nil, err
		}
		if err := view.setClaimBasis(policy, &original, reference); err != nil {
			return nil, err
		}
		prior = compacted.Archive
	}
	if !reflect.DeepEqual(prior, state.Archive) {
		return nil, errors.New("economic archive summary differs from exact original checkpoints")
	}
	if state.Renewal != nil && view.reviews[state.Renewal.ReviewSha256] {
		return nil, errors.New("economic continuation reused an archived operational review")
	}
	if state.FeeRevision != nil && view.feeReviews[state.FeeRevision.ReviewSha256] {
		return nil, errors.New("economic current fee revision reused an archived review")
	}
	if state.NativeRenewal != nil && view.nativeReviews[state.NativeRenewal.ReviewSha256] {
		return nil, errors.New("economic current native adoption reused an archived review")
	}
	// Check the active head against the admitted original receipt index before
	// the observer can perform a new source read or publish another snapshot.
	if err := view.admitEntitlementCensuses(ctx, policy, state); err != nil {
		return nil, err
	}
	checked := *state
	checked.archiveView = view
	if err := checked.validate(ctx, policy); err != nil {
		return nil, err
	}
	if err := checked.reconcileEntitlementLeaves(ctx); err != nil {
		return nil, err
	}
	if err := view.finishAdmission(); err != nil {
		return nil, err
	}
	return view, nil
}
