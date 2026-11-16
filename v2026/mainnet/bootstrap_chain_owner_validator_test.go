// The v5 sole UR validator may be the SN25 owner hotkey under the owner
// coldkey. Its census protection names both roles, the passive root seat keeps
// its own identity, and only a treasury approval may list the validator in its
// signed owner census.
package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// The launch form after the owner decision: a no-trim v5 preparation whose
// pending sole config runs on the synthetic subnet's explicit owner hotkey
// (UID 0, 0x41.., held by the subnet owner coldkey). UID 1's 0x42.. is another
// owned hotkey, so the signed owner census holds both. The root seat stays separate.
func newBootstrapChainOwnerValidatorFixture(t *testing.T, configureApproval func(*bootstrapChainValidatorFixture)) (*bootstrapChainFixture, error) {
	t.Helper()
	f := newBootstrapChainSoleFixture(t, false, configureApproval)
	// The owner hotkey holds a validator permit, as SN25's owner hotkey does.
	f.census.set(t, "ValidatorPermit", subnetTestVector([]byte{1, 0, 1, 1, 0, 0}, 1), []byte{25, 0})
	block := uint64(40)
	var identity subnetIdentityExpectation
	bootstrapChainNoTrimConvert(t, f, func(policy *subnetCensusPolicy) {
		identity = subnetIdentityExpectation{Hotkey: "0x" + strings.Repeat("41", 32), Coldkey: policy.SubnetOwnerColdkey, RegistrationBlock: &block}
		policy.Preserve[0] = subnetProtectedIdentity{subnetIdentityExpectation: identity, Roles: []string{"owner", "ur-validator"}}
	})
	v, hotkey := f.validators[0], bootstrapChainTestAccount(t, identity.Hotkey)
	v.approval.ValidatorHotkey, v.approval.Production.ValidatorHotkeys = hotkey, [][32]byte{hotkey}
	v.approval.OwnerHotkeys = [][32]byte{hotkey, bootstrapChainTestAccount(t, "0x"+strings.Repeat("42", 32))}
	bootstrapChainSolePendingEvidence(v)
	f.config.Validators[0].subnetIdentityExpectation = identity
	f.config.Validators[0].Config = v.publish(t)
	bootstrapRootTestWrite(t, f.path, f.config)
	var err error
	f.preparation, err = loadBootstrapChainPreparation(t.Context(), f.path)
	return f, err
}

// The owner-validator prepares offline and reads as ready: one sole role on
// the explicit owner hotkey, preserved as owner and ur-validator, beside a
// separate passive root seat, with a treasury approval naming itself as owner.
func TestBootstrapChainSoleOwnerValidatorPreparesOffline(t *testing.T) {
	f, err := newBootstrapChainOwnerValidatorFixture(t, bootstrapChainTreasuryApproval([32]byte(bytes.Repeat([]byte{0x77}, 32))))
	if err != nil {
		t.Fatal("owner-validator preparation refused", err)
	}
	plan, census := f.preparation.Plan, bootstrapChainNoTrimReview(t, f).Census.Observation
	role, inspection := plan.Config.Validators[0], plan.ValidatorInspections[0]
	hotkey := bootstrapChainTestAccount(t, role.Hotkey)
	seat := slices.IndexFunc(census.Seats, func(seat subnetSeat) bool { return seat.Hotkey == role.Hotkey })
	if plan.Schema != bootstrapChainPlanSchemaV5 || role.Role != "sole" || role.Coldkey != census.SubnetOwnerColdkey ||
		census.SubnetOwnerHotkey == nil || *census.SubnetOwnerHotkey != role.Hotkey ||
		plan.Config.RootValidator.Hotkey == role.Hotkey || plan.Config.RootValidator.Coldkey == role.Coldkey ||
		!bootstrapChainNoTrimCensusPreserves(census, role) || seat < 0 || !census.Seats[seat].OwnerRecognized ||
		!slices.Contains(census.Seats[seat].ProtectionReasons, "owner") || !slices.Contains(census.Seats[seat].ProtectionReasons, "runtime-owner-identity") {
		t.Fatalf("owner-validator preparation lost its identity, roles or census: %+v", plan)
	}
	if inspection.Approval.Proposal.Treasury == nil || !inspection.EvidenceActivationPending || inspection.Approval.ValidatorHotkey != hotkey ||
		!slices.Contains(inspection.Approval.OwnerHotkeys, hotkey) || !slices.Equal(inspection.Approval.Production.ValidatorHotkeys, [][32]byte{hotkey}) {
		t.Fatalf("owner-validator inspection lost its signed census: %+v", inspection.Approval)
	}
	result := f.result(t, "apply")
	if result.Schema != "urnetwork-mainnet-bootstrap-chain-result-v5" || result.PlanHash != plan.ContentHash || !result.UrValidatorConfigsVerified ||
		result.NetworkEffects || result.ActivationReady {
		t.Fatalf("owner-validator apply changed scope or claimed authority: %+v", result)
	}
	readiness, err := f.client.observeBootstrapChainReadiness(f.storageContext(t.Context()), f.preparation)
	if err != nil || readiness.Status != "observed-prerequisites" || len(readiness.UrValidators) != 1 || readiness.UrValidators[0].Observed == nil ||
		readiness.UrValidators[0].Observed.Hotkey != role.Hotkey || len(readiness.UrValidators[0].ObservationBlockers) != 0 || readiness.RootValidator.Observed == nil {
		t.Fatalf("owner-validator readiness: %+v %v", readiness, err)
	}
}

// An owner-recycle approval weights its owner census, so it can never list the
// validator itself as an owner; the sole config is refused before custody.
func TestBootstrapChainSoleOwnerValidatorRequiresTreasuryApproval(t *testing.T) {
	_, err := newBootstrapChainOwnerValidatorFixture(t, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot be an owner recipient") {
		t.Fatal("owner-recycle approval admitted its validator as an owner", err)
	}
}
