// Owner trim guards compare authenticated censuses without signing or granting
// authority. The reviewed capacity-only call has no atomic identity predicate.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
)

const ownerTrimGuardSchema = "urnetwork-mainnet-owner-trim-guard-v1"
const maximumOwnerTrimPlanBytes = 32 * 1024 * 1024

// Every requested original generation remains visible, including an absent old
// generation whose hotkey has since acquired a different owner or registration.
type ownerTrimGenerationObservation struct {
	subnetRegistration
	ExpectedDisposition string              `json:"expected_disposition"`
	ObservedDisposition string              `json:"observed_disposition"`
	CurrentRegistration *subnetRegistration `json:"current_registration"`
}

// State correspondence does not establish a trim receipt or its dispatch phase.
// All absent registrations retain their original UID, ownership and generation.
type ownerTrimReconciliation struct {
	RequestedGenerations    []ownerTrimGenerationObservation `json:"requested_generations"`
	AbsentOldGenerations    []subnetRegistration             `json:"absent_old_generations"`
	MissingSurvivors        []subnetUidMapping               `json:"missing_or_changed_survivor_mappings"`
	MissingProtected        []subnetRegistration             `json:"missing_protected_generations"`
	UnexpectedRegistrations []subnetRegistration             `json:"unexpected_registrations"`
	RootUnchanged           bool                             `json:"excluded_root_generations_unchanged"`
}

// Favorable observations are evidence only, never a reusable execution token.
// A later dispatch can select different identities even when both reads agree.
type ownerTrimGuard struct {
	Schema             string                   `json:"schema"`
	Mode               string                   `json:"mode"`
	Status             string                   `json:"status"`
	PlanContentHash    string                   `json:"plan_content_hash"`
	BaselineCensusHash string                   `json:"revalidated_baseline_census_hash"`
	CurrentCensus      subnetPreviewEnvelope    `json:"current_census"`
	ExpectedResidual   []ownerTrimResidual      `json:"expected_old_generation_residuals"`
	ObservationMatches bool                     `json:"observation_matches"`
	Reconciliation     *ownerTrimReconciliation `json:"reconciliation,omitempty"`
	ComparisonBlockers []string                 `json:"comparison_blockers"`
	ExecutionBlockers  []string                 `json:"execution_blockers"`
	ResetReady         bool                     `json:"reset_ready"`
	ApplyAuthority     bool                     `json:"apply_authority"`
	FullResetCompleted bool                     `json:"full_reset_completed"`
	ContentHash        string                   `json:"content_hash"`
}

// Validate bounded imports before any RPC. Their seals are checked for
// consistency; only a later historical reread establishes the baseline facts.
func validateOwnerTrimGuardPlan(policy subnetCensusPolicy, policyHash string, plan ownerTrimPlan) error {
	if err := policy.validate(); err != nil {
		return err
	}
	if !planSha256(policyHash) || plan.Census.Observation.PolicyHash != policyHash ||
		plan.Schema != ownerTrimPlanSchema || !planSha256(plan.ContentHash) ||
		plan.ResetReady || plan.ApplyAuthority || plan.FullResetCompleted || plan.Best == nil ||
		!plan.Best.SelectionSafe || plan.Best.ApprovedRemovals == 0 ||
		!rootCanonicalHash(plan.Census.Observation.Identity.FinalizedHash) ||
		len(plan.Census.Observation.Seats) > rootCensusLimit || len(plan.Candidates) > rootCensusLimit {
		return errors.New("owner trim guard requires a sealed safe partial plan and its exact original policy file")
	}
	sealed, err := sealSubnetPreview(plan.Census.Observation)
	if err != nil || sealed.ContentHash != plan.Census.ContentHash {
		return errors.Join(errors.New("owner trim baseline census seal differs"), err)
	}
	digest, err := ownerTrimPlanHash(plan)
	if err != nil || digest != plan.ContentHash {
		return errors.Join(errors.New("owner trim retained plan seal differs"), err)
	}
	return nil
}

// Both complete censuses and final canonical/head rechecks share one deadline.
// Interruption or a moving head returns no partial or previously good result.
func (self *rpcClient) readOwnerTrimGuard(ctx context.Context, policy subnetCensusPolicy, policyHash string, plan ownerTrimPlan, mode string) (ownerTrimGuard, error) {
	if ctx == nil || mode != "recheck" && mode != "reconcile" {
		return ownerTrimGuard{}, errors.New("owner trim guard requires context and recheck or reconcile mode")
	}
	if err := errors.Join(ctx.Err(), validateOwnerTrimGuardPlan(policy, policyHash, plan)); err != nil {
		return ownerTrimGuard{}, err
	}
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	retained := plan.Census.Observation
	baseline, err := self.readSubnetPreviewAt(sampleCtx, policy, policyHash, retained.Identity.FinalizedHash)
	if err != nil {
		return ownerTrimGuard{}, err
	}
	rebuilt, err := rebuildOwnerTrimGuardPlan(sampleCtx, policy, baseline, plan)
	if err != nil {
		return ownerTrimGuard{}, err
	}
	current, err := self.readSubnetPreview(sampleCtx, policy, policyHash)
	if err != nil {
		return ownerTrimGuard{}, err
	}
	if current.Identity.FinalizedNumber < baseline.Identity.FinalizedNumber ||
		current.Identity.FinalizedNumber == baseline.Identity.FinalizedNumber && current.Identity.FinalizedHash != baseline.Identity.FinalizedHash {
		return ownerTrimGuard{}, fmt.Errorf("%w: owner trim finalized head regressed or forked", errRpcIntegrity)
	}
	result := ownerTrimGuard{Schema: ownerTrimGuardSchema, Mode: mode, PlanContentHash: plan.ContentHash,
		BaselineCensusHash: rebuilt.Census.ContentHash, ExpectedResidual: rebuilt.Residual, ComparisonBlockers: []string{},
		ExecutionBlockers: []string{"OWNER_TRIM_EXECUTION_TIME_SELECTION_SAFETY_NOT_ESTABLISHED",
			"SOURCE_TO_WASM_PROVENANCE_REQUIRES_INDEPENDENT_REVIEW", "CUSTODY_COLLATERAL_STAKE_CLAIM_AND_HISTORY_AUDIT_NOT_COMPLETE",
			"OWNER_TRIM_SIGNING_AND_EXECUTION_NOT_IMPLEMENTED", "EXACT_TRIM_RECEIPT_AND_DISPATCH_PHASE_NOT_VERIFIED"}}
	if mode == "recheck" {
		currentPlan, err := buildOwnerTrimPlan(sampleCtx, policy, current)
		if err != nil {
			return ownerTrimGuard{}, err
		}
		result.ComparisonBlockers = compareOwnerTrimRecheck(rebuilt, currentPlan)
	} else {
		result.Reconciliation, result.ComparisonBlockers = reconcileOwnerTrimCensus(rebuilt, current)
	}
	result.CurrentCensus, err = sealSubnetPreview(current)
	if err != nil {
		return ownerTrimGuard{}, err
	}
	// Check both retained and new heights again, then require the sampled head
	// to remain latest. Never retry a moving head into a different approved set.
	for _, identity := range []chainIdentity{baseline.Identity, current.Identity} {
		var canonical string
		if err := self.call(sampleCtx, "chain_getBlockHash", []any{identity.FinalizedNumber}, &canonical); err != nil {
			return ownerTrimGuard{}, err
		}
		if !validHash(canonical) || !strings.EqualFold(canonical, identity.FinalizedHash) {
			return ownerTrimGuard{}, fmt.Errorf("%w: owner trim census is no longer canonical", errRpcIntegrity)
		}
	}
	var latest string
	if err := self.call(sampleCtx, "chain_getFinalizedHead", []any{}, &latest); err != nil {
		return ownerTrimGuard{}, err
	}
	if !validHash(latest) || !strings.EqualFold(latest, current.Identity.FinalizedHash) {
		return ownerTrimGuard{}, fmt.Errorf("%w: owner trim sampled head became stale during recheck", errRpcIntegrity)
	}
	if err := sampleCtx.Err(); err != nil {
		return ownerTrimGuard{}, err
	}
	result.ObservationMatches = len(result.ComparisonBlockers) == 0
	result.Status = mode + "-drift-execution-blocked"
	if result.ObservationMatches {
		result.Status = mode + "-matches-execution-blocked"
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return ownerTrimGuard{}, err
	}
	digest := sha256.Sum256(append([]byte(ownerTrimGuardSchema+"\x00"), raw...))
	result.ContentHash = "sha256:" + hex.EncodeToString(digest[:])
	if err := sampleCtx.Err(); err != nil {
		return ownerTrimGuard{}, err
	}
	return result, nil
}

// Only a completed reconstruction can contradict retained history. An
// interrupted computation preserves its cause and produces no replacement plan.
func rebuildOwnerTrimGuardPlan(ctx context.Context, policy subnetCensusPolicy, baseline subnetPreview, retained ownerTrimPlan) (ownerTrimPlan, error) {
	// Sampling time, route spelling and node release are not historical state.
	baseline.Identity.ObservedAt = retained.Census.Observation.Identity.ObservedAt
	baseline.Identity.RpcUrl = retained.Census.Observation.Identity.RpcUrl
	baseline.Identity.NodeVersion = retained.Census.Observation.Identity.NodeVersion
	rebuilt, err := buildOwnerTrimPlan(ctx, policy, baseline)
	if err != nil {
		return ownerTrimPlan{}, fmt.Errorf("rebuild retained owner trim plan: %w", err)
	}
	if rebuilt.ContentHash != retained.ContentHash {
		return ownerTrimPlan{}, fmt.Errorf("%w: retained owner trim plan differs from its authenticated historical census", errRpcIntegrity)
	}
	return rebuilt, nil
}

// A fixed retained selection never silently grows, shrinks or reranks when
// emissions, immunity, role/custody, membership or runtime timing changes.
func compareOwnerTrimRecheck(plan, currentPlan ownerTrimPlan) []string {
	before, after := plan.Census.Observation, currentPlan.Census.Observation
	blockers := ownerTrimScopeChanges(before, after)
	if before.MaximumUids != after.MaximumUids || before.MinimumUids != after.MinimumUids {
		blockers = append(blockers, "OWNER_TRIM_CAPACITY_CHANGED")
	}
	if before.ImmunityBlocks != after.ImmunityBlocks || before.ImmuneOwnerUidsLimit != after.ImmuneOwnerUidsLimit {
		blockers = append(blockers, "OWNER_TRIM_IMMUNITY_CHANGED")
	}
	if len(before.Seats) != len(after.Seats) {
		blockers = append(blockers, "OWNER_TRIM_MEMBERSHIP_OR_GENERATION_CHANGED")
	}
	for index := 0; index < min(len(before.Seats), len(after.Seats)); index++ {
		left, right := before.Seats[index], after.Seats[index]
		if left.subnetRegistration != right.subnetRegistration {
			blockers = append(blockers, "OWNER_TRIM_MEMBERSHIP_OR_GENERATION_CHANGED")
		}
		if left.emission != right.emission {
			blockers = append(blockers, "OWNER_TRIM_EMISSIONS_CHANGED")
		}
		if left.OwnerImmune != right.OwnerImmune || left.TemporarilyImmune != right.TemporarilyImmune {
			blockers = append(blockers, "OWNER_TRIM_IMMUNITY_CHANGED")
		}
		if left.OwnerRecognized != right.OwnerRecognized || left.ValidatorPermit != right.ValidatorPermit ||
			left.Disposition != right.Disposition || !slices.Equal(left.ProtectionReasons, right.ProtectionReasons) {
			blockers = append(blockers, "OWNER_TRIM_PROTECTED_ROLE_OR_CUSTODY_CHANGED")
		}
	}
	if before.Trim.OwnerLastTrimBlock != after.Trim.OwnerLastTrimBlock || before.Trim.AdminWindowOpen != after.Trim.AdminWindowOpen {
		blockers = append(blockers, "OWNER_TRIM_TIMING_CHANGED")
	}
	if len(after.Blockers) != 0 {
		blockers = append(blockers, "OWNER_TRIM_CURRENT_SCOPE_NOT_COMPLETE")
	}
	if currentPlan.Best == nil || !currentPlan.Best.RuntimeReady {
		blockers = append(blockers, "OWNER_TRIM_CURRENT_RUNTIME_CHECKS_NOT_READY")
	}
	if !reflect.DeepEqual(plan.Best, currentPlan.Best) || !reflect.DeepEqual(plan.Residual, currentPlan.Residual) {
		blockers = append(blockers, "OWNER_TRIM_REVIEWED_SELECTION_OR_RESIDUAL_CHANGED")
	}
	return ownerTrimUniqueBlockers(blockers)
}

// These invariants apply before dispatch and to post-state. Registration must
// remain closed; that still cannot freeze emissions or prevent privileged churn.
func ownerTrimScopeChanges(before, after subnetPreview) []string {
	blockers := []string{}
	if before.SubnetOwnerColdkey != after.SubnetOwnerColdkey || !reflect.DeepEqual(before.SubnetOwnerHotkey, after.SubnetOwnerHotkey) ||
		before.SubnetRegistrationBlock != after.SubnetRegistrationBlock || before.SubnetGeneration != after.SubnetGeneration {
		blockers = append(blockers, "OWNER_TRIM_SUBNET_OWNER_OR_GENERATION_CHANGED")
	}
	if before.RegistrationAllowed || before.PowRegistrationAllowed || after.RegistrationAllowed || after.PowRegistrationAllowed {
		blockers = append(blockers, "OWNER_TRIM_COMPETING_REGISTRATION_OR_REENTRY_NOT_FENCED")
	}
	if !reflect.DeepEqual(before.RootRegistrations, after.RootRegistrations) {
		blockers = append(blockers, "OWNER_TRIM_EXCLUDED_ROOT_GENERATIONS_CHANGED")
	}
	return blockers
}

// Reconcile the fixed original prediction by identity and generation. A lower
// seat count, an absent hotkey or a matching capacity alone never passes.
func reconcileOwnerTrimCensus(plan ownerTrimPlan, current subnetPreview) (*ownerTrimReconciliation, []string) {
	baseline := plan.Census.Observation
	result := &ownerTrimReconciliation{RequestedGenerations: []ownerTrimGenerationObservation{}, AbsentOldGenerations: []subnetRegistration{},
		MissingSurvivors: []subnetUidMapping{}, MissingProtected: []subnetRegistration{}, UnexpectedRegistrations: []subnetRegistration{},
		RootUnchanged: reflect.DeepEqual(baseline.RootRegistrations, current.RootRegistrations)}
	blockers := ownerTrimScopeChanges(baseline, current)
	currentHotkeyKVs := map[string]subnetRegistration{}
	baselineHotkeyKVs := map[string]subnetRegistration{}
	removedHotkeyKVs := map[string]bool{}
	for _, seat := range current.Seats {
		currentHotkeyKVs[seat.Hotkey] = seat.subnetRegistration
	}
	for _, removed := range plan.Best.ExpectedRemoved {
		removedHotkeyKVs[removed.Hotkey] = true
	}
	for _, seat := range baseline.Seats {
		baselineHotkeyKVs[seat.Hotkey] = seat.subnetRegistration
		observed, found := currentHotkeyKVs[seat.Hotkey]
		same := found && ownerTrimSameGeneration(seat.subnetRegistration, observed)
		if !same {
			result.AbsentOldGenerations = append(result.AbsentOldGenerations, seat.subnetRegistration)
			if seat.Disposition == "preserve" || len(seat.ProtectionReasons) != 0 {
				result.MissingProtected = append(result.MissingProtected, seat.subnetRegistration)
			}
		}
		if seat.RequestedRemoval {
			row := ownerTrimGenerationObservation{subnetRegistration: seat.subnetRegistration, ExpectedDisposition: "retained", ObservedDisposition: "old-generation-absent"}
			if removedHotkeyKVs[seat.Hotkey] {
				row.ExpectedDisposition = "absent"
				if same {
					blockers = append(blockers, "OWNER_TRIM_EXPECTED_REMOVAL_STILL_PRESENT")
				}
			}
			if found {
				row.CurrentRegistration = &observed
				row.ObservedDisposition = "hotkey-has-different-generation"
				if same {
					row.ObservedDisposition = "old-generation-retained"
				}
			}
			result.RequestedGenerations = append(result.RequestedGenerations, row)
		}
	}
	for _, mapping := range plan.Best.Survivors {
		observed, found := currentHotkeyKVs[mapping.Hotkey]
		if !found || !ownerTrimSameGeneration(mapping.subnetRegistration, observed) || observed.Uid != mapping.NewUid {
			result.MissingSurvivors = append(result.MissingSurvivors, mapping)
		}
	}
	for _, seat := range current.Seats {
		original, found := baselineHotkeyKVs[seat.Hotkey]
		if !found || !ownerTrimSameGeneration(original, seat.subnetRegistration) {
			result.UnexpectedRegistrations = append(result.UnexpectedRegistrations, seat.subnetRegistration)
		}
	}
	if len(result.MissingSurvivors) != 0 {
		blockers = append(blockers, "OWNER_TRIM_SURVIVOR_GENERATION_OR_UID_MAPPING_DIFFERS")
	}
	if len(result.MissingProtected) != 0 {
		blockers = append(blockers, "OWNER_TRIM_PROTECTED_GENERATION_MISSING")
	}
	if len(result.UnexpectedRegistrations) != 0 {
		blockers = append(blockers, "OWNER_TRIM_UNEXPECTED_REGISTRATION_OR_REENTRY")
	}
	if current.MaximumUids != plan.Best.MaximumUids || len(current.Seats) != len(plan.Best.Survivors) {
		blockers = append(blockers, "OWNER_TRIM_POST_CAPACITY_OR_COUNT_DIFFERS")
	}
	return result, ownerTrimUniqueBlockers(blockers)
}

// UID compression does not replace a registration; custody or age changes do.
func ownerTrimSameGeneration(left, right subnetRegistration) bool {
	return left.Hotkey == right.Hotkey && left.Coldkey == right.Coldkey && left.RegistrationBlock == right.RegistrationBlock
}

// Repeated affected rows produce one deterministic refusal reason.
func ownerTrimUniqueBlockers(blockers []string) []string {
	sort.Strings(blockers)
	return slices.Compact(blockers)
}
