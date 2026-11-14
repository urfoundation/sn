// Owner trim planning ranks permitted partial removals from one authenticated
// census. Unsigned method bytes never confer an origin or execution authority.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
)

const ownerTrimPlanSchema = "urnetwork-mainnet-owner-trim-plan-v1"

// Capacity-only summaries keep ranking storage linear in the bounded census.
type ownerTrimCandidate struct {
	MaximumUids        uint16   `json:"maximum_uids"`
	ApprovedRemovals   uint16   `json:"approved_old_generations_selected"`
	RequestedResiduals uint16   `json:"requested_old_generations_retained"`
	ImmunePercentage   uint8    `json:"immune_percentage"`
	SelectionSafe      bool     `json:"selection_safe_at_observed_block"`
	RuntimeReady       bool     `json:"runtime_checks_pass_at_observed_block"`
	Blockers           []string `json:"blockers"`
}

// An unchanged membership range is represented once, never 65,536 no-op rows.
type ownerTrimCapacityRange struct {
	Minimum uint16 `json:"minimum"`
	Maximum uint16 `json:"maximum"`
}

// The original hotkey/coldkey/registration generation identifies each residual.
// The status is a predicted disposition, not a claim of executed removal.
type ownerTrimResidual struct {
	subnetIdentityExpectation
	ObservedUid *uint16  `json:"observed_uid"`
	Disposition string   `json:"expected_disposition"`
	Reasons     []string `json:"reasons"`
}

// Complete mappings and unsigned bytes are retained for only the best choice.
type ownerTrimSelection struct {
	ownerTrimCandidate
	ExpectedRemoved  []subnetRegistration `json:"expected_removed_generations"`
	Survivors        []subnetUidMapping   `json:"expected_survivor_uid_mapping"`
	UnsignedCallHex  string               `json:"unsigned_call_hex"`
	UnsignedCallHash string               `json:"unsigned_call_sha256"`
}

// This separate schema cannot be consumed as an executable bootstrap plan.
// Source, custody and execution-time selection remain unresolved regardless of
// how many requested generations the observed candidate happens to remove.
type ownerTrimPlan struct {
	Schema                 string                  `json:"schema"`
	Status                 string                  `json:"status"`
	Origin                 string                  `json:"planned_origin"`
	OwnerColdkey           string                  `json:"owner_coldkey_account_id"`
	ResetReady             bool                    `json:"reset_ready"`
	ApplyAuthority         bool                    `json:"apply_authority"`
	FullResetCompleted     bool                    `json:"full_reset_completed"`
	RequestedCapacity      uint16                  `json:"originally_proposed_capacity"`
	RequestedCount         uint16                  `json:"requested_old_generations"`
	RankingRule            string                  `json:"ranking_rule"`
	Census                 subnetPreviewEnvelope   `json:"census"`
	Candidates             []ownerTrimCandidate    `json:"capacity_candidates"`
	NoRemovalCapacityRange *ownerTrimCapacityRange `json:"no_removal_capacity_range"`
	Best                   *ownerTrimSelection     `json:"best_observed_selection"`
	AllRequestedSelected   bool                    `json:"all_requested_generations_selected_at_observed_block"`
	Residual               []ownerTrimResidual     `json:"requested_residual_generations"`
	ExecutionBlockers      []string                `json:"execution_blockers"`
	ContentHash            string                  `json:"content_hash"`
}

// The caller obtains this census from the real reader; no imported hash or
// caller-supplied approval/health flag enters the command path. One sort plus
// prefix counts evaluates all removal capacities in bounded linear storage.
func buildOwnerTrimPlan(ctx context.Context, policy subnetCensusPolicy, preview subnetPreview) (ownerTrimPlan, error) {
	if ctx == nil {
		return ownerTrimPlan{}, errors.New("owner trim requires a context")
	}
	if err := errors.Join(ctx.Err(), policy.validate()); err != nil {
		return ownerTrimPlan{}, err
	}
	if !preview.CensusComplete || preview.Schema != subnetPreviewSchema || len(preview.Seats) > rootCensusLimit ||
		preview.Netuid != policy.Netuid || preview.Identity.EvmChainId != mainnetEvmChainId ||
		preview.Identity.NativeChain != policy.NativeChain || preview.RuntimeVersion != policy.RuntimeVersion ||
		preview.RuntimeSourceCommit != policy.RuntimeSourceCommit ||
		!strings.EqualFold(preview.RuntimeCodeHash, policy.RuntimeCodeHash) ||
		!strings.EqualFold(preview.RuntimeMetadataHash, policy.RuntimeMetadataHash) ||
		!strings.EqualFold(preview.Identity.GenesisHash, policy.GenesisHash) ||
		preview.MinimumUids > preview.MaximumUids || len(preview.Seats) > int(preview.MaximumUids) {
		return ownerTrimPlan{}, errors.New("owner trim requires a complete approved mainnet census and valid capacity")
	}
	census, err := sealSubnetPreview(preview)
	if err != nil {
		return ownerTrimPlan{}, err
	}
	plan := ownerTrimPlan{Schema: ownerTrimPlanSchema, Status: "selection-ranked-execution-blocked", Origin: "subnet-owner-coldkey-only",
		OwnerColdkey: preview.SubnetOwnerColdkey, RequestedCapacity: *policy.TrimMaximumUids, RequestedCount: uint16(len(policy.Remove)),
		RankingRule: "most-approved-old-generations-removed; least-capacity-reduction; larger-capacity", Census: census,
		Candidates: []ownerTrimCandidate{}, Residual: []ownerTrimResidual{},
		ExecutionBlockers: []string{"SOURCE_TO_WASM_PROVENANCE_REQUIRES_INDEPENDENT_REVIEW", "EXECUTION_TIME_SELECTION_GUARD_NOT_PROVEN", "CUSTODY_COLLATERAL_STAKE_CLAIM_AND_HISTORY_AUDIT_NOT_COMPLETE", "OWNER_TRIM_SIGNING_AND_EXECUTION_NOT_IMPLEMENTED", "POST_TRIM_GENERATION_AND_ROOT_CENSUS_REQUIRED"}}
	plan.ExecutionBlockers = append(plan.ExecutionBlockers, preview.Blockers...)
	// A caller cannot erase a stale owner/generation by dropping reader blockers.
	scopeComplete := len(preview.Blockers) == 0 && strings.EqualFold(preview.SubnetOwnerColdkey, policy.SubnetOwnerColdkey) &&
		preview.SubnetRegistrationBlock == *policy.SubnetRegistrationBlock && preview.SubnetGeneration == *policy.SubnetGeneration
	if !scopeComplete {
		plan.ExecutionBlockers = append(plan.ExecutionBlockers, "RESET_REMOVAL_SCOPE_NOT_COMPLETE")
	}
	timingBlockers := []string{}
	if preview.Trim.OwnerLastTrimBlock != 0 {
		timingBlockers = append(timingBlockers, "RESET_OWNER_TRIM_RATE_LIMIT_BUILD_VALUE_REQUIRES_REVIEW")
	}
	if !preview.Trim.AdminWindowOpen {
		timingBlockers = append(timingBlockers, "RESET_ADMIN_WINDOW_CLOSED")
	}
	plan.ExecutionBlockers = append(plan.ExecutionBlockers, timingBlockers...)
	if preview.RegistrationAllowed || preview.PowRegistrationAllowed {
		plan.ExecutionBlockers = append(plan.ExecutionBlockers, "COMPETING_REGISTRATION_AND_REENTRY_POLICY_NOT_ENFORCED")
	}
	noRemovalMinimum := max(int(preview.MinimumUids), len(preview.Seats))
	if noRemovalMinimum <= int(preview.MaximumUids) {
		plan.NoRemovalCapacityRange = &ownerTrimCapacityRange{Minimum: uint16(noRemovalMinimum), Maximum: preview.MaximumUids}
	}
	indices := make([]int, 0, len(preview.Seats))
	immune := 0
	seatKVs := map[string]subnetSeat{}
	requestedKVs := map[string]subnetIdentityExpectation{}
	protectedKVs := map[string]bool{}
	for _, requested := range policy.Remove {
		requestedKVs[strings.ToLower(requested.Hotkey)] = requested
	}
	for _, protected := range policy.Preserve {
		protectedKVs[strings.ToLower(protected.Hotkey)] = true
	}
	for index, seat := range preview.Seats {
		hotkey := strings.ToLower(seat.Hotkey)
		emission, err := strconv.ParseUint(seat.EmissionRao, 10, 64)
		if err != nil || emission != seat.emission {
			return ownerTrimPlan{}, errors.New("owner trim requires an exact decoded emission census")
		}
		if seat.Uid != uint16(index) || seatKVs[hotkey].Hotkey != "" {
			return ownerTrimPlan{}, errors.New("owner trim census is not complete, unique and in UID order")
		}
		seatKVs[hotkey] = seat
		if seat.OwnerImmune || seat.TemporarilyImmune {
			immune++
		} else {
			indices = append(indices, index)
		}
	}
	// This is the source's backwards visit of a stable descending sort.
	sort.Slice(indices, func(i, j int) bool {
		left, right := preview.Seats[indices[i]], preview.Seats[indices[j]]
		if left.emission != right.emission {
			return left.emission < right.emission
		}
		return left.Uid > right.Uid
	})
	unsafePrefixes := make([]int, len(indices)+1)
	for index, position := range indices {
		unsafePrefixes[index+1] = unsafePrefixes[index]
		seat := preview.Seats[position]
		requested, exists := requestedKVs[strings.ToLower(seat.Hotkey)]
		exact := exists && strings.EqualFold(requested.Coldkey, seat.Coldkey) && *requested.RegistrationBlock == seat.RegistrationBlock
		if !exact || protectedKVs[strings.ToLower(seat.Hotkey)] || seat.OwnerRecognized || seat.ValidatorPermit ||
			seat.Disposition != "remove" || !seat.RequestedRemoval || len(seat.ProtectionReasons) != 0 {
			unsafePrefixes[index+1]++
		}
	}
	upper := min(int(preview.MaximumUids), len(preview.Seats)-1)
	for capacity := int(preview.MinimumUids); capacity <= upper; capacity++ {
		if err := ctx.Err(); err != nil {
			return ownerTrimPlan{}, err
		}
		needed := len(preview.Seats) - capacity
		candidate := ownerTrimCandidate{MaximumUids: uint16(capacity), RequestedResiduals: uint16(len(policy.Remove)), Blockers: []string{}}
		percentage := 100
		if capacity != 0 {
			percentage = min(100, 100*immune/capacity)
		}
		candidate.ImmunePercentage = uint8(percentage)
		if preview.Trim.Call == nil {
			candidate.Blockers = append(candidate.Blockers, "RESET_OWNER_TRIM_CALL_NOT_VERIFIED")
		}
		if preview.Trim.MaximumImmunePercentage == nil {
			candidate.Blockers = append(candidate.Blockers, "RESET_IMMUNITY_THRESHOLD_NOT_VERIFIED")
		} else if candidate.ImmunePercentage >= *preview.Trim.MaximumImmunePercentage {
			candidate.Blockers = append(candidate.Blockers, "RESET_TRIM_IMMUNITY_THRESHOLD_REACHED")
		}
		if needed > len(indices) {
			candidate.Blockers = append(candidate.Blockers, "RESET_TRIM_CANNOT_REACH_REQUESTED_CAPACITY")
		} else if unsafePrefixes[needed] != 0 {
			candidate.Blockers = append(candidate.Blockers, "RESET_TRIM_WOULD_REMOVE_PROTECTED_OR_UNRESOLVED_IDENTITY")
		} else {
			candidate.ApprovedRemovals = uint16(needed)
			candidate.RequestedResiduals -= uint16(needed)
		}
		if !scopeComplete {
			candidate.Blockers = append(candidate.Blockers, "RESET_REMOVAL_SCOPE_NOT_COMPLETE")
		}
		candidate.SelectionSafe = len(candidate.Blockers) == 0
		candidate.Blockers = append(candidate.Blockers, timingBlockers...)
		candidate.RuntimeReady = candidate.SelectionSafe && len(timingBlockers) == 0
		plan.Candidates = append(plan.Candidates, candidate)
	}
	sort.SliceStable(plan.Candidates, func(i, j int) bool {
		left, right := plan.Candidates[i], plan.Candidates[j]
		if left.SelectionSafe != right.SelectionSafe {
			return left.SelectionSafe
		}
		if left.ApprovedRemovals != right.ApprovedRemovals {
			return left.ApprovedRemovals > right.ApprovedRemovals
		}
		return left.MaximumUids > right.MaximumUids
	})
	removedKVs := map[uint16]bool{}
	if len(plan.Candidates) != 0 && plan.Candidates[0].SelectionSafe {
		candidate := plan.Candidates[0]
		best := &ownerTrimSelection{ownerTrimCandidate: candidate, ExpectedRemoved: []subnetRegistration{}, Survivors: []subnetUidMapping{}}
		for _, index := range indices[:int(candidate.ApprovedRemovals)] {
			removedKVs[preview.Seats[index].Uid] = true
		}
		for _, seat := range preview.Seats {
			if removedKVs[seat.Uid] {
				best.ExpectedRemoved = append(best.ExpectedRemoved, seat.subnetRegistration)
			} else {
				best.Survivors = append(best.Survivors, subnetUidMapping{subnetRegistration: seat.subnetRegistration, NewUid: uint16(len(best.Survivors))})
			}
		}
		call := []byte{preview.Trim.Call.PalletIndex, preview.Trim.Call.CallIndex}
		call = binary.LittleEndian.AppendUint16(call, policy.Netuid)
		call = binary.LittleEndian.AppendUint16(call, candidate.MaximumUids)
		digest := sha256.Sum256(call)
		best.UnsignedCallHex, best.UnsignedCallHash = "0x"+hex.EncodeToString(call), "sha256:"+hex.EncodeToString(digest[:])
		plan.Best = best
		plan.AllRequestedSelected = candidate.RequestedResiduals == 0
	} else {
		plan.ExecutionBlockers = append(plan.ExecutionBlockers, "OWNER_TRIM_NO_SAFE_REMOVAL_CANDIDATE")
	}
	for _, requested := range policy.Remove {
		seat, found := seatKVs[strings.ToLower(requested.Hotkey)]
		exact := found && strings.EqualFold(requested.Coldkey, seat.Coldkey) && *requested.RegistrationBlock == seat.RegistrationBlock
		if exact && removedKVs[seat.Uid] {
			continue
		}
		registration := *requested.RegistrationBlock
		owned := subnetIdentityExpectation{Hotkey: strings.ToLower(requested.Hotkey), Coldkey: strings.ToLower(requested.Coldkey), RegistrationBlock: &registration}
		residual := ownerTrimResidual{subnetIdentityExpectation: owned, Disposition: "unresolved", Reasons: []string{}}
		if exact {
			uid := seat.Uid
			residual.ObservedUid = &uid
			residual.Disposition = "retained_by_runtime"
			if seat.OwnerImmune {
				residual.Reasons = append(residual.Reasons, "runtime-owner-immunity")
			}
			if seat.TemporarilyImmune {
				residual.Reasons = append(residual.Reasons, "temporary-immunity")
			}
			if len(seat.ProtectionReasons) != 0 || seat.Disposition != "remove" {
				residual.Disposition = "unresolved"
				residual.Reasons = append(residual.Reasons, "protected-or-unresolved-removal-scope")
			}
			if len(residual.Reasons) == 0 {
				residual.Reasons = append(residual.Reasons, "no-larger-safe-removal-prefix-within-runtime-capacity-and-immunity-limits")
			}
		} else {
			residual.Reasons = append(residual.Reasons, "requested-owner-or-registration-generation-not-in-census")
		}
		plan.Residual = append(plan.Residual, residual)
	}
	sort.Slice(plan.Residual, func(i, j int) bool { return plan.Residual[i].Hotkey < plan.Residual[j].Hotkey })
	if err := ctx.Err(); err != nil {
		return ownerTrimPlan{}, err
	}
	plan.ContentHash, err = ownerTrimPlanHash(plan)
	return plan, err
}

// The seal is reproducible after decoding, but supplies no policy authority.
func ownerTrimPlanHash(plan ownerTrimPlan) (string, error) {
	plan.ContentHash = ""
	raw, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(append([]byte(ownerTrimPlanSchema+"\x00"), raw...))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
