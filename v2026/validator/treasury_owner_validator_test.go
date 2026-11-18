//go:build linux || darwin

// The owner-validator form: the treasury producer runs on the subnet owner
// hotkey. Its signed owner census holds the validator itself, which never
// receives weight, and only the explicit owner hotkey may take that form.
package validator

import (
	"bytes"
	"encoding/binary"
	"maps"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// Encodes an exact OwnedHotkeys vector in the runtime's SCALE layout.
func treasuryOwnerValidatorTestOwned(t *testing.T, hotkeys [][32]byte) []byte {
	t.Helper()
	raw := releaseNativeValidatorTestCompact(t, uint64(len(hotkeys)))
	for _, hotkey := range hotkeys {
		raw = append(raw, hotkey[:]...)
	}
	return raw
}

// Before the independent signature, the validator's own hotkey becomes the
// explicit subnet owner hotkey, held by the subnet owner coldkey and listed in
// its OwnedHotkeys; the signed census then includes the validator itself.
func configureTreasuryOwnerValidator(t *testing.T, fixture *ownerRecycleProductionTestFixture) {
	t.Helper()
	approval := &fixture.operator.measurement.admission.approval
	self := fixture.hotkey.PublicKey()
	netuid := binary.LittleEndian.AppendUint16(nil, fixture.cfg.Netuid)
	owners := append(slices.Clone(approval.OwnerHotkeys), self)
	slices.SortFunc(owners, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	treasuryProductionTestPut(t, fixture, "OwnedHotkeys", treasuryOwnerValidatorTestOwned(t, owners), approval.SubnetOwner[:])
	treasuryProductionTestPut(t, fixture, "SubnetOwnerHotkey", self[:], netuid)
	treasuryProductionTestPut(t, fixture, "Owner", approval.SubnetOwner[:], self[:])
	approval.OwnerHotkeys = owners
}

// The complete treasury producer with the owner-validator identity, signed and
// loaded through the actual production admission.
func newTreasuryOwnerValidatorTestFixture(t *testing.T) *ownerRecycleProductionTestFixture {
	t.Helper()
	return newOwnerRecycleProductionTestFixtureWithInputs(t, newRecycleOperatorFixtureWithHotkey, func(fixture *ownerRecycleProductionTestFixture) {
		configureTreasuryProductionTest(t, fixture)
		configureTreasuryOwnerValidator(t, fixture)
	})
}

// The owner-validator reaches a signed measured treasury intent. Its census
// recognizes it as the explicit owner hotkey, its eligibility rests on a real
// permit and stake, and neither its own UID nor any owner UID gets weight.
func TestTreasuryOwnerValidatorPreparesMeasuredSuccessor(t *testing.T) {
	fixture := newTreasuryOwnerValidatorTestFixture(t)
	measurement := fixture.operator.measurement
	approval := measurement.admission.approval
	self, selfUid := fixture.hotkey.PublicKey(), measurement.provider.artifact.SelfUID
	stage, provider := fixture.stage(t)
	census := stage.authority.observation
	if census.Snapshot.SubnetOwnerHotkey == nil || *census.Snapshot.SubnetOwnerHotkey != self || len(census.RecognizedOwners) != len(approval.OwnerHotkeys) ||
		!slices.ContainsFunc(census.RecognizedOwners, func(owner OwnerRecycleRegistration) bool { return owner.Uid == selfUid && owner.Hotkey == self }) {
		t.Fatalf("owner census lost the validator's own subnet owner hotkey: %+v", census.RecognizedOwners)
	}
	validators := stage.proof.Eligibility.Validators
	index := slices.IndexFunc(validators, func(validator OwnerRecycleProductionValidator) bool { return validator.Hotkey == self })
	if index < 0 || validators[index].Uid != selfUid || !validators[index].SubnetOwnerException || validators[index].Coldkey != approval.SubnetOwner ||
		!validators[index].ValidatorPermit || validators[index].TotalStakeRao < validators[index].StakeThresholdRao {
		t.Fatalf("owner-validator eligibility lost its owner identity, permit or stake: %+v", validators)
	}
	intent := fixture.intent(t, stage, provider)
	verified, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, intent, measurement.encoded, measurement.provider.artifact, provider)
	if err != nil || intent.Treasury == nil || intent.OwnerRecycle != nil || intent.Prepared == nil || intent.Prepared.HotkeyHex != releaseHex32(self) {
		t.Fatalf("owner-validator treasury intent lost its signer or authority: %v", err)
	}
	policy := approval.Proposal.Treasury
	if !slices.Equal(stage.proof.Row.TreasuryUids, []uint16{policy.Recipients[0].Uid, policy.Recipients[1].Uid}) || len(stage.proof.Row.OwnerUids) != 0 || len(stage.proof.Row.MaskedOwnerUids) != 0 {
		t.Fatal("owner-validator row relabeled owners or lost its treasury roster")
	}
	for name, uids := range map[string][]uint16{"row": stage.proof.Row.Uids, "verified": verified.UIDs, "prepared": intent.Prepared.UIDs} {
		if len(uids) == 0 || slices.Contains(uids, selfUid) {
			t.Fatalf("%s weights the validator itself: %v", name, uids)
		}
		for _, owner := range census.RecognizedOwners {
			if slices.Contains(uids, owner.Uid) {
				t.Fatalf("%s weights recognized owner uid %d", name, owner.Uid)
			}
		}
	}
	providerSum := new(big.Rat)
	for index, uid := range verified.UIDs {
		if slices.Contains(stage.proof.Row.TreasuryUids, uid) {
			if verified.Scores[index].Cmp(big.NewRat(9, 20)) != 0 {
				t.Fatal("treasury recipient did not receive exactly 45 percent")
			}
		} else {
			providerSum.Add(providerSum, verified.Scores[index])
		}
	}
	if providerSum.Cmp(big.NewRat(1, 10)) != 0 {
		t.Fatal("owner-validator changed the 10/90 provider and treasury split")
	}
}

// The census admits the validator as an owner only as the runtime's explicit
// subnet owner hotkey held by the subnet owner coldkey. Another owned hotkey,
// another coldkey or an unrecognized hotkey cannot take the owner-validator form.
func TestTreasuryOwnerValidatorRequiresExplicitOwnerHotkeyAndColdkey(t *testing.T) {
	fixture := newTreasuryOwnerValidatorTestFixture(t)
	admission := fixture.operator.measurement.admission
	approval := admission.approval
	self := fixture.hotkey.PublicKey()
	netuid := binary.LittleEndian.AppendUint16(nil, fixture.cfg.Netuid)
	others := slices.DeleteFunc(slices.Clone(approval.OwnerHotkeys), func(hotkey [32]byte) bool { return hotkey == self })
	original := maps.Clone(admission.storage)
	for _, fault := range []struct{ name, expected string }{
		{name: "owned-without-explicit", expected: "not the explicit subnet owner hotkey"},
		{name: "other-owned-explicit", expected: "not the explicit subnet owner hotkey"},
		{name: "other-coldkey", expected: "not held by the subnet owner coldkey"},
		{name: "unrecognized", expected: "census differs"},
	} {
		admission.storage = maps.Clone(original)
		switch fault.name {
		case "owned-without-explicit":
			treasuryProductionTestPut(t, fixture, "SubnetOwnerHotkey", nil, netuid)
		case "other-owned-explicit":
			treasuryProductionTestPut(t, fixture, "SubnetOwnerHotkey", others[0][:], netuid)
		case "other-coldkey":
			other := recycleTestId(6200)
			treasuryProductionTestPut(t, fixture, "Owner", other[:], self[:])
		case "unrecognized":
			treasuryProductionTestPut(t, fixture, "SubnetOwnerHotkey", nil, netuid)
			treasuryProductionTestPut(t, fixture, "OwnedHotkeys", treasuryOwnerValidatorTestOwned(t, others), approval.SubnetOwner[:])
		}
		got, err := ObserveTreasuryAdmissionAt(t.Context(), fixture.cfg, admission.chain, [32]byte(admission.finalized))
		if got != nil || err == nil || retryableProductionSteeringRead(err) || !strings.Contains(err.Error(), fault.expected) {
			t.Fatalf("%s admitted an owner-validator census: %v", fault.name, err)
		}
	}
	admission.storage = original
	got, err := ObserveTreasuryAdmission(t.Context(), fixture.cfg, admission.chain)
	if err != nil || got.Snapshot.SubnetOwnerHotkey == nil || *got.Snapshot.SubnetOwnerHotkey != self {
		t.Fatalf("restored owner-validator census did not recover: %v", err)
	}
}

// The runtime lets the subnet owner hotkey submit without a permit or stake;
// production eligibility does not. Restored state is the positive control.
func TestTreasuryOwnerValidatorEligibilityRequiresPermitAndStake(t *testing.T) {
	fixture := newTreasuryOwnerValidatorTestFixture(t)
	stage, _ := fixture.stage(t)
	admission := fixture.operator.measurement.admission
	selfUid := fixture.validatorUids[0]
	registrations := stage.authority.observation.Snapshot.Registrations
	permits := func() string {
		raw := releaseNativeValidatorTestCompact(t, uint64(len(registrations)))
		for _, registration := range registrations {
			permit := byte(0)
			if fixture.permitKVs[registration.Uid] {
				permit = 1
			}
			raw = append(raw, permit)
		}
		return codec.HexEncodeToString(raw)
	}
	original := maps.Clone(admission.storage)
	for _, fault := range []string{"stake", "permit"} {
		admission.storage = maps.Clone(original)
		fixture.stakeKVs[selfUid], fixture.permitKVs[selfUid] = 100, true
		switch fault {
		case "stake":
			fixture.stakeKVs[selfUid] = 9
		case "permit":
			fixture.permitKVs[selfUid] = false
			admission.storage[fixture.storageNameKVs["ValidatorPermit"]] = permits()
		}
		if _, err := observeOwnerRecycleProductionEligibility(t.Context(), fixture.cfg, admission.chain, stage.authority); err == nil || !strings.Contains(err.Error(), "owner-validator") {
			t.Fatalf("%s: the subnet owner exception granted owner-validator eligibility: %v", fault, err)
		}
	}
	admission.storage = original
	fixture.stakeKVs[selfUid], fixture.permitKVs[selfUid] = 100, true
	if _, err := observeOwnerRecycleProductionEligibility(t.Context(), fixture.cfg, admission.chain, stage.authority); err != nil {
		t.Fatalf("restored owner-validator eligibility stayed refused: %v", err)
	}
}

// Signed scope: only a treasury approval's own validator may be an owner. No
// other approved validator may be one, and owner-recycle, which weights its
// owner census, never admits its validator into that census.
func TestOwnerValidatorApprovalScope(t *testing.T) {
	treasury := newTreasuryOwnerValidatorTestFixture(t)
	approval := treasury.operator.measurement.admission.approval
	if _, err := admitOwnerRecycleApproval(treasury.cfg, &approval); err != nil {
		t.Fatal("treasury owner-validator approval refused", err)
	}
	self := treasury.hotkey.PublicKey()
	other := slices.DeleteFunc(slices.Clone(approval.Production.ValidatorHotkeys), func(hotkey [32]byte) bool { return hotkey == self })[0]
	changed := approval
	changed.OwnerHotkeys = append(slices.Clone(approval.OwnerHotkeys), other)
	slices.SortFunc(changed.OwnerHotkeys, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	if _, err := admitOwnerRecycleApproval(treasury.cfg, &changed); err == nil || !strings.Contains(err.Error(), "cannot be an owner recipient") {
		t.Fatal("another approved validator joined the owner census", err)
	}
	recycle := newOwnerRecycleProductionTestFixture(t)
	original := recycle.operator.measurement.admission.approval
	if _, err := admitOwnerRecycleApproval(recycle.cfg, &original); err != nil {
		t.Fatal("owner-recycle baseline approval refused", err)
	}
	changed = original
	changed.OwnerHotkeys = append(slices.Clone(original.OwnerHotkeys), original.ValidatorHotkey)
	slices.SortFunc(changed.OwnerHotkeys, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	if _, err := admitOwnerRecycleApproval(recycle.cfg, &changed); err == nil || !strings.Contains(err.Error(), "cannot be an owner recipient") {
		t.Fatal("owner-recycle approval admitted its validator as an owner destination", err)
	}
}
