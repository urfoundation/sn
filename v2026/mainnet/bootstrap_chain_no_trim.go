// A v5 preparation may explicitly select no owner trim. It still authenticates
// the retained census and protection set, but selects and authorizes no removal.
package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// The sole explicit owner-trim mode. Absence keeps the retained trim review.
const bootstrapChainOwnerTrimNone = "none"

// The owner-trim phase keeps its sealed name; this records why it stays pending.
const bootstrapChainOwnerTrimNotSelected = "not-selected-owner-decision"

// No older schema, other spelling or absent field can acquire the no-trim mode.
func (self bootstrapChainConfig) validOwnerTrimMode() bool {
	return self.OwnerTrimMode == "" || self.Schema == bootstrapChainConfigSchemaV5 && self.OwnerTrimMode == bootstrapChainOwnerTrimNone
}

// Only the explicit sealed mode selects no trim.
func (self bootstrapChainConfig) noOwnerTrim() bool {
	return self.OwnerTrimMode == bootstrapChainOwnerTrimNone
}

// Trim-mode outputs omit the phase decision, so their bytes are unchanged.
func (self bootstrapChainConfig) ownerTrimPhase() string {
	if self.noOwnerTrim() {
		return bootstrapChainOwnerTrimNotSelected
	}
	return ""
}

// The retained review is the unchanged owner-trim plan grammar built from the
// census under a policy without removals, so it carries no selection. Seats the
// policy leaves unlisted stay unresolved because nothing is removed; any
// explicit removal or trimmed capacity is refused rather than ignored.
func validateBootstrapChainNoTrimReview(policy subnetCensusPolicy, policyHash string, plan ownerTrimPlan) error {
	if err := policy.validate(); err != nil {
		return err
	}
	census := plan.Census.Observation
	if len(policy.Remove) != 0 || len(census.Remove) != 0 || slices.ContainsFunc(census.Seats, func(seat subnetSeat) bool {
		return seat.RequestedRemoval || seat.Disposition == "remove"
	}) {
		return errors.New("bootstrap chain no-trim preparation refuses a census policy or census that lists removals")
	}
	if *policy.TrimMaximumUids != census.MaximumUids {
		return errors.New("bootstrap chain no-trim census policy must propose the observed capacity, not a trimmed one")
	}
	if !planSha256(policyHash) || census.PolicyHash != policyHash || plan.Schema != ownerTrimPlanSchema || !planSha256(plan.ContentHash) ||
		plan.ResetReady || plan.ApplyAuthority || plan.FullResetCompleted || plan.Best != nil || !census.CensusComplete ||
		!rootCanonicalHash(census.Identity.FinalizedHash) || len(census.Seats) > rootCensusLimit || len(plan.Candidates) > rootCensusLimit {
		return errors.New("bootstrap chain no-trim preparation requires a sealed complete census without a removal selection and its exact original policy file")
	}
	for _, blocker := range census.Blockers {
		if blocker != "NO_EXPLICIT_REMOVAL_SCOPE" && !strings.HasPrefix(blocker, "IDENTITY_ROLE_UNRESOLVED:") {
			return fmt.Errorf("bootstrap chain no-trim census does not authenticate the approved owner, generation or protected identities: %s", blocker)
		}
	}
	sealed, err := sealSubnetPreview(census)
	if err != nil || sealed.ContentHash != plan.Census.ContentHash {
		return errors.Join(errors.New("bootstrap chain no-trim census seal differs"), err)
	}
	digest, err := ownerTrimPlanHash(plan)
	if err != nil || digest != plan.ContentHash {
		return errors.Join(errors.New("bootstrap chain no-trim retained review seal differs"), err)
	}
	return nil
}

// Without a selection, the exact UR generation must be a finalized census seat
// preserved for its declared ur-validator role.
func bootstrapChainNoTrimCensusPreserves(census subnetPreview, role bootstrapChainValidator) bool {
	return slices.ContainsFunc(census.Seats, func(seat subnetSeat) bool {
		return seat.Hotkey == role.Hotkey && seat.Coldkey == role.Coldkey && seat.RegistrationBlock == *role.RegistrationBlock &&
			seat.Disposition == "preserve" && slices.Contains(seat.ProtectionReasons, "ur-validator")
	})
}
