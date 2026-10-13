// Bounded qualification proves selection predicates under explicit, still
// unproved window assumptions. It neither constructs nor authorizes a signed call.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

const ownerTrimWindowSchema = "urnetwork-mainnet-owner-trim-window-v1"
const ownerTrimBoundedSchema = "urnetwork-mainnet-owner-trim-bounded-qualification-v1"
const ownerTrimSubsetRule = "any-subset-of-approved-old-generations"

// The proposed window binds an independently supplied policy and exact finalized
// anchor. No approval, health, custody or future-stability boolean is accepted.
type ownerTrimWindow struct {
	Schema          string `json:"schema"`
	PolicyHash      string `json:"policy_file_sha256"`
	FinalizedHash   string `json:"finalized_hash"`
	FinalizedNumber uint64 `json:"finalized_number"`
	MortalPeriod    uint64 `json:"mortal_period_blocks"`
	SelectionRule   string `json:"selection_rule"`
}

// Only periods with unit phase quantization are supported, so the exact
// finalized anchor is also the era birth; death is exclusive and never renewed.
func (self ownerTrimWindow) validate(policyHash string) error {
	if self.Schema != ownerTrimWindowSchema || !planSha256(policyHash) || self.PolicyHash != policyHash ||
		!rootCanonicalHash(self.FinalizedHash) || self.SelectionRule != ownerTrimSubsetRule {
		return errors.New("owner trim window requires its schema, exact policy hash, finalized anchor and explicit subset selection rule")
	}
	if _, err := rootMortalEra(self.FinalizedNumber, self.MortalPeriod); err != nil {
		return fmt.Errorf("owner trim window: %w", err)
	}
	return nil
}

// Cooldowns bind every registered coldkey, including protected seats and the
// subnet owner. A zero last-swap value does not prove a first swap is blocked.
type ownerTrimColdkeyWindow struct {
	Coldkey                string  `json:"coldkey_account_id"`
	LastHotkeySwapBlock    uint64  `json:"last_hotkey_swap_block"`
	ColdkeySwapAvailableAt *uint64 `json:"coldkey_swap_available_at"`
	ColdkeySwapDisputedAt  *uint64 `json:"coldkey_swap_disputed_at"`
}

// All values originate from metadata or exact-block storage reads. The private
// type has no import path from a caller-authored proof bundle.
type ownerTrimWindowState struct {
	Tempo               uint16                   `json:"tempo"`
	LastEpochBlock      uint64                   `json:"last_epoch_block"`
	PendingEpochAt      uint64                   `json:"pending_epoch_at"`
	BlocksSinceLastStep uint64                   `json:"blocks_since_last_step"`
	AdminFreezeWindow   uint16                   `json:"admin_freeze_window"`
	HotkeySwapInterval  uint64                   `json:"hotkey_swap_on_subnet_interval"`
	ColdkeySwapDelay    uint32                   `json:"coldkey_swap_announcement_delay"`
	SubnetLeaseId       *uint32                  `json:"subnet_lease_id"`
	Coldkeys            []ownerTrimColdkeyWindow `json:"coldkeys"`
	Storage             []rootStorageValue       `json:"storage"`
}

// Every requested original remains visible. Uncertain residual identity is
// intentional: any subset is permitted, never a silently substituted prefix.
type ownerTrimBoundedResidual struct {
	subnetIdentityExpectation
	ObservedUid *uint16 `json:"observed_uid"`
	Disposition string  `json:"conditional_disposition"`
}

// Passing predicates are conditional evidence, not a transaction authorization.
// Required assumptions remain unproved even when every observed guard passes.
type ownerTrimBoundedQualification struct {
	Schema                   string                     `json:"schema"`
	Status                   string                     `json:"status"`
	Window                   ownerTrimWindow            `json:"window"`
	WindowFileHash           string                     `json:"window_file_sha256"`
	Census                   subnetPreviewEnvelope      `json:"census"`
	WindowState              ownerTrimWindowState       `json:"authenticated_window_state"`
	MortalEraHex             string                     `json:"proposed_mortal_era_hex"`
	DeathExclusive           uint64                     `json:"death_block_exclusive"`
	MaximumUids              uint16                     `json:"target_maximum_uids"`
	MaximumImmuneCount       uint16                     `json:"maximum_immune_count_during_window"`
	ImmunePercentage         uint8                      `json:"maximum_immune_percentage_at_target"`
	ConditionalRemovals      *uint16                    `json:"conditional_removed_generation_count"`
	ConditionalResidualCount *uint16                    `json:"conditional_requested_residual_count"`
	RemovableGenerations     []subnetRegistration       `json:"generations_removable_before_expiry"`
	RequestedGenerations     []ownerTrimBoundedResidual `json:"requested_old_generation_dispositions"`
	ConditionalSafeSet       bool                       `json:"conditional_safe_set_qualified"`
	QualificationBlockers    []string                   `json:"qualification_blockers"`
	RequiredAssumptions      []string                   `json:"unproved_window_assumptions"`
	ExecutionBlockers        []string                   `json:"execution_blockers"`
	ResetReady               bool                       `json:"reset_ready"`
	ApplyAuthority           bool                       `json:"apply_authority"`
	FullResetCompleted       bool                       `json:"full_reset_completed"`
	ContentHash              string                     `json:"content_hash"`
}

// The evaluator ignores emission order: a protected low emitter is safe only
// through actual immunity. No epoch is allowed, preserving roles and preventing
// conviction takeover registration under the listed unchanged-state assumptions.
func qualifyOwnerTrimWindow(ctx context.Context, policy subnetCensusPolicy, policyHash string, window ownerTrimWindow, windowHash string, census subnetPreview, state ownerTrimWindowState) (ownerTrimBoundedQualification, error) {
	if err := policy.validate(); err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	return qualifyOwnerTrimWindowTarget(ctx, policy, policyHash, window, windowHash, census, state, *policy.TrimMaximumUids)
}

// The action path may choose only its separately approved retained best capacity.
// It does not rewrite the original policy file or its historical qualification.
func qualifyOwnerTrimWindowTarget(ctx context.Context, policy subnetCensusPolicy, policyHash string, window ownerTrimWindow, windowHash string, census subnetPreview, state ownerTrimWindowState, maximum uint16) (ownerTrimBoundedQualification, error) {
	if ctx == nil {
		return ownerTrimBoundedQualification{}, errors.New("owner trim qualification requires a context")
	}
	if err := errors.Join(ctx.Err(), policy.validate(), window.validate(policyHash)); err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	if !planSha256(windowHash) || !census.CensusComplete || census.Schema != subnetPreviewSchema || census.PolicyHash != policyHash ||
		census.Identity.FinalizedNumber != window.FinalizedNumber || census.Identity.FinalizedHash != window.FinalizedHash {
		return ownerTrimBoundedQualification{}, fmt.Errorf("%w: owner trim window differs from its authenticated census", errRpcIntegrity)
	}
	// Reuse the planner's complete runtime, capacity and decoded-census validation;
	// none of its fixed emission-prefix prediction becomes subset authorization.
	if _, err := buildOwnerTrimPlan(ctx, policy, census); err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	result := ownerTrimBoundedQualification{
		Schema: ownerTrimBoundedSchema, Status: "bounded-predicates-failed-execution-blocked", Window: window, WindowFileHash: windowHash,
		WindowState: state, DeathExclusive: window.FinalizedNumber + window.MortalPeriod, MaximumUids: maximum,
		RemovableGenerations: []subnetRegistration{}, RequestedGenerations: []ownerTrimBoundedResidual{}, QualificationBlockers: []string{},
		RequiredAssumptions: []string{
			"OWNER_PROXY_MULTISIG_AND_PENDING_ACTIONS_PRESERVE_SCOPE_IMMUNITY_CAPACITY_AND_EPOCH_SETTINGS_THROUGH_EXPIRY",
			"GOVERNANCE_RUNTIME_AND_PRIVILEGED_ACTIONS_PRESERVE_REVIEWED_SEMANTICS_AND_WINDOW_STATE_THROUGH_EXPIRY",
			"PUBLIC_NETWORK_REGISTRATION_PRUNING_CANNOT_REMOVE_OR_REUSE_APPROVED_SUBNET_GENERATION_THROUGH_EXPIRY",
		},
		ExecutionBlockers: []string{"WINDOW_ASSUMPTIONS_NOT_AUTHENTICATED", "SOURCE_TO_WASM_PROVENANCE_REQUIRES_INDEPENDENT_REVIEW",
			"CUSTODY_COLLATERAL_STAKE_CLAIM_AND_HISTORY_AUDIT_NOT_COMPLETE", "OWNER_TRIM_SIGNING_AND_EXECUTION_NOT_IMPLEMENTED",
			"EXACT_TRIM_RECEIPT_AND_ACTUAL_SUBSET_RECONCILIATION_NOT_IMPLEMENTED"},
	}
	era, _ := rootMortalEra(window.FinalizedNumber, window.MortalPeriod)
	result.MortalEraHex = "0x" + hex.EncodeToString(era)
	first, last := window.FinalizedNumber+1, result.DeathExclusive-1
	block := func(reason string) { result.QualificationBlockers = append(result.QualificationBlockers, reason) }
	if len(census.Blockers) != 0 || len(census.Unresolved) != 0 {
		block("OWNER_TRIM_SCOPE_NOT_COMPLETE")
	}
	if census.SubnetOwnerColdkey != strings.ToLower(policy.SubnetOwnerColdkey) ||
		census.SubnetRegistrationBlock != *policy.SubnetRegistrationBlock || census.SubnetGeneration != *policy.SubnetGeneration {
		block("OWNER_TRIM_OWNER_OR_SUBNET_GENERATION_CHANGED")
	}
	if census.RegistrationAllowed || census.PowRegistrationAllowed {
		block("OWNER_TRIM_REGISTRATION_NOT_CLOSED")
	}
	if state.SubnetLeaseId != nil {
		block("OWNER_TRIM_LEASE_LIFECYCLE_NOT_FENCED")
	}
	if census.Trim.Call == nil {
		block("OWNER_TRIM_CALL_NOT_AUTHENTICATED")
	}
	if census.Trim.OwnerLastTrimBlock != 0 {
		block("OWNER_TRIM_RATE_LIMIT_BUILD_VALUE_NOT_AUTHENTICATED")
	}
	if state.LastEpochBlock > window.FinalizedNumber {
		return ownerTrimBoundedQualification{}, fmt.Errorf("%w: epoch lies after the census", errRpcIntegrity)
	}
	if state.Tempo != 0 {
		next := state.LastEpochBlock + uint64(state.Tempo)
		if next <= last || state.PendingEpochAt != 0 && state.PendingEpochAt <= last ||
			state.BlocksSinceLastStep > uint64(state.Tempo) || last-window.FinalizedNumber > uint64(state.Tempo)-min(state.BlocksSinceLastStep, uint64(state.Tempo)) {
			block("OWNER_TRIM_EPOCH_CAN_OCCUR_BEFORE_EXPIRY")
		}
		if state.PendingEpochAt > window.FinalizedNumber || next <= last || next-last < uint64(state.AdminFreezeWindow) {
			block("OWNER_TRIM_ADMIN_WINDOW_NOT_OPEN_THROUGH_EXPIRY")
		}
	}
	requestedKVs := map[string]subnetIdentityExpectation{}
	for _, requested := range policy.Remove {
		requestedKVs[strings.ToLower(requested.Hotkey)] = requested
	}
	relevantColdkeyKVs := map[string]bool{census.SubnetOwnerColdkey: false}
	seatKVs := map[string]subnetSeat{}
	seenRequested := 0
	for _, seat := range census.Seats {
		if err := ctx.Err(); err != nil {
			return ownerTrimBoundedQualification{}, err
		}
		relevantColdkeyKVs[seat.Coldkey] = false
		seatKVs[seat.Hotkey] = seat
		if seat.RegistrationBlock > window.FinalizedNumber {
			return ownerTrimBoundedQualification{}, fmt.Errorf("%w: registration lies after the census", errRpcIntegrity)
		}
		requested, found := requestedKVs[seat.Hotkey]
		exact := found && strings.EqualFold(requested.Coldkey, seat.Coldkey) && *requested.RegistrationBlock == seat.RegistrationBlock
		approved := exact && seat.RequestedRemoval && seat.Disposition == "remove" && !seat.OwnerRecognized && !seat.ValidatorPermit && len(seat.ProtectionReasons) == 0
		immuneAtFirst := seat.OwnerImmune || first-seat.RegistrationBlock < uint64(census.ImmunityBlocks)
		immuneAtLast := seat.OwnerImmune || last-seat.RegistrationBlock < uint64(census.ImmunityBlocks)
		if immuneAtFirst {
			result.MaximumImmuneCount++
		}
		if !immuneAtLast {
			result.RemovableGenerations = append(result.RemovableGenerations, seat.subnetRegistration)
			if !approved {
				block("OWNER_TRIM_NONIMMUNE_GENERATION_OUTSIDE_APPROVED_SET:" + seat.Hotkey)
			}
		}
		if exact {
			seenRequested++
		}
	}
	for _, requested := range policy.Remove {
		registration := *requested.RegistrationBlock
		residual := ownerTrimBoundedResidual{subnetIdentityExpectation: subnetIdentityExpectation{Hotkey: strings.ToLower(requested.Hotkey), Coldkey: strings.ToLower(requested.Coldkey), RegistrationBlock: &registration}, Disposition: "unresolved-original-generation"}
		if seat, found := seatKVs[residual.Hotkey]; found && seat.Coldkey == residual.Coldkey && seat.RegistrationBlock == registration {
			uid := seat.Uid
			residual.ObservedUid = &uid
			residual.Disposition = "may-be-removed-or-retained"
			if seat.OwnerImmune || last-seat.RegistrationBlock < uint64(census.ImmunityBlocks) {
				residual.Disposition = "retained-through-runtime-immunity"
			}
		}
		result.RequestedGenerations = append(result.RequestedGenerations, residual)
	}
	if seenRequested != len(policy.Remove) {
		block("OWNER_TRIM_REQUESTED_GENERATION_NOT_REGISTERED")
	}
	if uint64(state.ColdkeySwapDelay) < result.DeathExclusive-first {
		block("OWNER_TRIM_NEW_COLDKEY_SWAP_CAN_MATURE_BEFORE_EXPIRY")
	}
	for _, coldkey := range state.Coldkeys {
		covered, exists := relevantColdkeyKVs[coldkey.Coldkey]
		if !exists || covered || coldkey.LastHotkeySwapBlock > window.FinalizedNumber ||
			coldkey.ColdkeySwapDisputedAt != nil && *coldkey.ColdkeySwapDisputedAt > window.FinalizedNumber {
			return ownerTrimBoundedQualification{}, fmt.Errorf("%w: invalid or duplicate coldkey window evidence", errRpcIntegrity)
		}
		relevantColdkeyKVs[coldkey.Coldkey] = true
		cooldownEnd := coldkey.LastHotkeySwapBlock + state.HotkeySwapInterval
		if cooldownEnd < coldkey.LastHotkeySwapBlock {
			cooldownEnd = math.MaxUint64
		}
		if coldkey.LastHotkeySwapBlock == 0 || cooldownEnd < last {
			block("OWNER_TRIM_HOTKEY_SWAP_NOT_FENCED:" + coldkey.Coldkey)
		}
		if coldkey.ColdkeySwapAvailableAt != nil && *coldkey.ColdkeySwapAvailableAt < result.DeathExclusive {
			block("OWNER_TRIM_ANNOUNCED_COLDKEY_SWAP_CAN_MATURE_BEFORE_EXPIRY:" + coldkey.Coldkey)
		}
		if coldkey.Coldkey == census.SubnetOwnerColdkey && (coldkey.ColdkeySwapAvailableAt != nil || coldkey.ColdkeySwapDisputedAt != nil) {
			block("OWNER_TRIM_OWNER_COLDKEY_HAS_SWAP_RESTRICTION")
		}
	}
	for _, covered := range relevantColdkeyKVs {
		if !covered {
			return ownerTrimBoundedQualification{}, fmt.Errorf("%w: coldkey window evidence is incomplete", errRpcIntegrity)
		}
	}
	result.ImmunePercentage = 100
	if result.MaximumUids != 0 {
		result.ImmunePercentage = uint8(min(100, 100*int(result.MaximumImmuneCount)/int(result.MaximumUids)))
	}
	if result.MaximumUids < census.MinimumUids || result.MaximumUids > census.MaximumUids || int(result.MaximumUids) >= len(census.Seats) {
		block("OWNER_TRIM_TARGET_IS_NOT_A_PERMITTED_CAPACITY_REDUCTION")
	}
	if census.Trim.MaximumImmunePercentage == nil {
		block("OWNER_TRIM_IMMUNITY_THRESHOLD_NOT_AUTHENTICATED")
	} else if result.ImmunePercentage >= *census.Trim.MaximumImmunePercentage {
		block("OWNER_TRIM_IMMUNITY_THRESHOLD_REACHED_DURING_WINDOW")
	}
	if result.MaximumImmuneCount > result.MaximumUids {
		block("OWNER_TRIM_TARGET_UNREACHABLE_DURING_WINDOW")
	}
	result.QualificationBlockers = ownerTrimUniqueBlockers(result.QualificationBlockers)
	result.ConditionalSafeSet = len(result.QualificationBlockers) == 0
	if result.ConditionalSafeSet {
		removed := uint16(len(census.Seats)) - result.MaximumUids
		if int(removed) > len(policy.Remove) {
			return ownerTrimBoundedQualification{}, fmt.Errorf("%w: removable set exceeded approved generations", errRpcIntegrity)
		}
		residual := uint16(len(policy.Remove)) - removed
		result.ConditionalRemovals, result.ConditionalResidualCount = &removed, &residual
		result.Status = "bounded-predicates-match-assumptions-unproved-execution-blocked"
	} else {
		result.ExecutionBlockers = append(result.ExecutionBlockers, "BOUNDED_SAFE_SET_NOT_QUALIFIED")
	}
	var err error
	result.Census, err = sealSubnetPreview(census)
	if err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	digest := sha256.Sum256(append([]byte(ownerTrimBoundedSchema+"\x00"), raw...))
	result.ContentHash = "sha256:" + hex.EncodeToString(digest[:])
	if err := ctx.Err(); err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	return result, nil
}
