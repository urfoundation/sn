// Public pruning and registration risks require independent, explicit policy
// choices. Synthetic fixtures retain the real offline and owned-post boundaries.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Choice changes happen before the policy is attached, with a fresh signature.
func (self *ownerTrimBestEffortTestFixture) approveRisks(pruning, registration string) {
	self.approval.PublicPruningPolicy, self.approval.RegistrationPolicy = pruning, registration
	self.approval.ResidualRisks = self.approval.residuals()
	self.approval.Signature = hex.EncodeToString(ed25519.Sign(self.approvalKey, self.approval.signingBytes()))
}

// Expired immunity and open registration are independent gates. Accepting one
// does not silently accept the other, and explicit acceptance changes no bytes.
func TestOwnerTrimBestEffortPruningAndRegistrationChoicesAreIndependent(t *testing.T) {
	for _, choice := range []struct {
		pruning      string
		registration string
		wantPost     bool
	}{
		{pruning: ownerTrimAcceptPruningRisk, registration: ownerTrimRequireClosedRegistration},
		{pruning: ownerTrimRequirePruningImmunity, registration: ownerTrimAcceptRegistrationRisk},
		{pruning: ownerTrimAcceptPruningRisk, registration: ownerTrimAcceptRegistrationRisk, wantPost: true},
	} {
		f := newOwnerTrimBestEffortTestFixture(t, true)
		f.guard.after.set(t, "NetworkImmunityPeriod", binary.LittleEndian.AppendUint64(nil, 1))
		f.approveRisks(choice.pruning, choice.registration)
		store, _, owner := f.open(t)
		_, err := owner.step(t.Context())
		record, loadErr := store.load()
		if loadErr != nil || (err == nil) != choice.wantPost || (len(f.sent()) == 1) != choice.wantPost ||
			record.RawExtrinsic != "0x"+hex.EncodeToString(f.raw) || !reflect.DeepEqual(record.Config, f.action.config) {
			t.Fatal("explicit independent risks changed admission or original bytes", choice, err, loadErr, f.sent())
		}
		if !choice.wantPost && record.Broadcasts != 0 {
			t.Fatal("unaccepted risk consumed native send allowance", choice)
		}
	}
}

// Accepted future risk cannot hide observed generation drift, a changed flag,
// or a proxy at the sampled block. The historical baseline stays unchanged.
func TestOwnerTrimBestEffortAcceptedRisksStillRejectCurrentDrift(t *testing.T) {
	for _, fault := range []string{"protected-generation", "registration-flag", "proxy"} {
		f := newOwnerTrimBestEffortTestFixture(t, true)
		f.guard.after.set(t, "NetworkImmunityPeriod", binary.LittleEndian.AppendUint64(nil, 1))
		f.approveRisks(ownerTrimAcceptPruningRisk, ownerTrimAcceptRegistrationRisk)
		switch fault {
		case "protected-generation":
			f.guard.after.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 99), []byte{25, 0}, []byte{2, 0})
		case "registration-flag":
			f.guard.after.set(t, "NetworkRegistrationAllowed", []byte{0}, []byte{25, 0})
		case "proxy":
			owner, _ := hex.DecodeString(f.action.config.Action.Coldkey[2:])
			key, err := types.CreateStorageKey(f.guard.after.metadata, "Proxy", "Proxies", owner)
			if err != nil {
				t.Fatal(err)
			}
			f.guard.after.storageKVs[key.Hex()] = "0x" + strings.Repeat("00", 8) + "01"
		}
		store, _, owner := f.open(t)
		_, err := owner.step(t.Context())
		record, loadErr := store.load()
		if err == nil || loadErr != nil || record.Broadcasts != 0 || len(f.sent()) != 0 {
			t.Fatal("accepted future risk hid observed drift", fault, err, loadErr, f.sent())
		}
	}
}

// Legacy zero-value policies retain their exact domain bytes and conservative
// gate. Adding the new fields never invalidates or weakens their old signatures.
func TestOwnerTrimBestEffortLegacyPolicyRemainsConservative(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t)
	f.approveRisks("", "")
	encoded, err := json.Marshal(f.approval)
	if err != nil || bytes.Contains(encoded, []byte("public_pruning_policy")) || bytes.Contains(encoded, []byte("registration_policy")) {
		t.Fatal("empty risk options changed legacy approval wire bytes", err)
	}
	// Removing the new zero fields yields exactly the old schema's JSON order.
	var legacy struct {
		Schema             string             `json:"schema"`
		ConfigHash         string             `json:"original_action_config_hash"`
		ExtrinsicHash      string             `json:"original_signed_extrinsic_hash"`
		Runtime            rootReceiptProfile `json:"exact_runtime"`
		BaselineCensusHash string             `json:"reviewed_baseline_census_hash"`
		ProtectedScopeHash string             `json:"reviewed_protected_generation_scope_hash"`
		InitialBroadcasts  uint8              `json:"already_consumed_broadcasts"`
		ValidFromBlock     uint64             `json:"valid_from_finalized_block"`
		ValidThroughBlock  uint64             `json:"valid_through_finalized_block"`
		ResidualRisks      []string           `json:"explicitly_accepted_residual_risks"`
		Signature          string             `json:"approval_signature_ed25519"`
	}
	if err := json.Unmarshal(encoded, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.Signature = ""
	old, _ := json.Marshal(legacy)
	if !bytes.Equal(f.approval.signingBytes(), append([]byte(ownerTrimBestEffortApprovalSchema+"\x00"), old...)) {
		t.Fatal("legacy independent policy signing bytes changed")
	}
	f.guard.after.set(t, "NetworkImmunityPeriod", binary.LittleEndian.AppendUint64(nil, 1))
	store, _, owner := f.open(t)
	if _, err := owner.step(t.Context()); err == nil || len(f.sent()) != 0 {
		t.Fatal("legacy policy silently accepted public pruning risk", err)
	}
	if _, err := store.load(); err != nil {
		t.Fatal("recoverable unmet predicate poisoned original custody", err)
	}
}

// The exact residual and choice are both independently signed. Neither a new
// flag nor an otherwise valid policy signature can omit their required text.
func TestOwnerTrimBestEffortRiskApprovalRequiresExactSignedResiduals(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t)
	key := "0x" + hex.EncodeToString(f.approvalKey.Public().(ed25519.PublicKey))
	for _, choice := range []string{"pruning", "registration"} {
		approval := ownerTrimTestCopy(t, f.approval)
		if choice == "pruning" {
			approval.PublicPruningPolicy = ownerTrimAcceptPruningRisk
		} else {
			approval.RegistrationPolicy = ownerTrimAcceptRegistrationRisk
		}
		approval.Signature = hex.EncodeToString(ed25519.Sign(f.approvalKey, approval.signingBytes()))
		if err := approval.validate(f.action.config, key, rootExtrinsicHash(f.raw)); err == nil {
			t.Fatal("accepted risk omitted mandatory explicit residual", choice)
		}
		approval.ResidualRisks = approval.residuals()
		if err := approval.validate(f.action.config, key, rootExtrinsicHash(f.raw)); err == nil {
			t.Fatal("unsigned residual upgrade inherited old approval", choice)
		}
		approval.Signature = hex.EncodeToString(ed25519.Sign(f.approvalKey, approval.signingBytes()))
		if err := approval.validate(f.action.config, key, rootExtrinsicHash(f.raw)); err != nil {
			t.Fatal("separately signed exact residual was refused", choice, err)
		}
	}
}

// Draft flags publish explicit unsigned choices and bytes; only the separately
// signed policy can make the public submit command acquire admission capability.
func TestOwnerTrimBestEffortPublicRiskPlanAndSubmit(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t, true)
	f.guard.after.set(t, "NetworkImmunityPeriod", binary.LittleEndian.AppendUint64(nil, 1))
	original, err := os.ReadFile(f.action.config.Action.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	raw, code, diagnostic := f.command(t, "trim-submit-plan", "--public-pruning-policy", ownerTrimAcceptPruningRisk, "--registration-policy", ownerTrimAcceptRegistrationRisk)
	var plan struct {
		Approval     ownerTrimBestEffortApproval `json:"approval_template"`
		SigningBytes string                      `json:"signing_bytes"`
	}
	if code != 0 || decodePlanJson(raw, &plan) != nil || plan.Approval.PublicPruningPolicy != ownerTrimAcceptPruningRisk || plan.Approval.RegistrationPolicy != ownerTrimAcceptRegistrationRisk ||
		plan.Approval.Signature != "" || plan.SigningBytes != "0x"+hex.EncodeToString(plan.Approval.signingBytes()) {
		t.Fatal("unsigned risk choices changed or acquired authority", code, diagnostic)
	}
	if _, code, _ := f.command(t, "trim-submit-plan", "--public-pruning-policy", "accept-everything"); code != 2 {
		t.Fatal("unknown risk choice admitted", code)
	}
	retained, err := os.ReadFile(f.action.config.Action.StatePath)
	if err != nil || !bytes.Equal(original, retained) || len(f.sent()) != 0 {
		t.Fatal("unsigned plan changed native custody", err)
	}
	f.approval = plan.Approval
	f.approval.Signature = hex.EncodeToString(ed25519.Sign(f.approvalKey, f.approval.signingBytes()))
	f.policyRef = bootstrapRootTestWrite(t, f.policyRef.Path, f.approval)
	if _, code, diagnostic := f.command(t, "trim-submit"); code != 0 || len(f.sent()) != 1 {
		t.Fatal("separately approved public risk workflow could not post retained bytes", code, diagnostic)
	}
}

// After finality, exact generation correspondence is a fact distinct from the
// pre-send registration fence. Unknown or changed generations still conflict;
// strict action domains retain their old closed-registration result.
func TestOwnerTrimBestEffortReceiptCorrespondenceRetainsRegistrationRisk(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t, true)
	f.approveRisks(ownerTrimAcceptPruningRisk, ownerTrimAcceptRegistrationRisk)
	store, _, _ := f.open(t)
	before := ownerTrimTestCopy(t, store.review.Census.Observation)
	before.Identity.FinalizedNumber, before.Identity.FinalizedHash = 101, f.guard.afterHash
	after := ownerTrimTestCopy(t, before)
	after.MaximumUids = f.action.config.Action.MaximumUids
	after.Seats = after.Seats[:int(after.MaximumUids)]
	after.Identity.FinalizedNumber, after.Identity.FinalizedHash = 102, "0x"+strings.Repeat("da", 32)
	result, err := reconcileOwnerTrimActualSubset(f.action.config.Action, store.policy, before, after)
	if err != nil || !result.Matches || len(result.AbsentOld) != 2 || len(result.Survivors) != 4 || len(result.Unexpected) != 0 || result.FullReset || result.AttributedToTrim {
		t.Fatal("accepted registration risk erased exact observed generation outcome", result, err)
	}
	strict := f.action.config.Action
	strict.Schema, strict.SelectionRule = ownerTrimLedgerActionSchema, ownerTrimSubsetRule
	strict, err = prepareOwnerTrimAction(strict, f.action.metadata)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := reconcileOwnerTrimActualSubset(strict, store.policy, before, after); err != nil || result.Matches {
		t.Fatal("best-effort observation weakened strict registration semantics", result, err)
	}
	after.Seats[2].RegistrationBlock++
	if result, err := reconcileOwnerTrimActualSubset(f.action.config.Action, store.policy, before, after); err != nil || result.Matches || len(result.MissingProtected) == 0 || len(result.Unexpected) == 0 {
		t.Fatal("registration residual hid changed protected generation after dispatch", result, err)
	}
}
