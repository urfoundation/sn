//go:build linux || darwin

// Receiving authority needs only a public native account and exact registered
// recipients. It still replays every independent production admission gate.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"math/big"
	"reflect"
	"slices"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Synthetic destination bytes carry no threshold, signatory or device claim.
func treasuryReceivePolicyTestValue() TreasuryPolicy {
	return TreasuryPolicy{Schema: TreasuryReceivePolicySchema, MultisigAccount: [32]byte{0x61},
		Recipients:    []TreasuryRecipient{{Uid: 10, Hotkey: [32]byte{10}, RegistrationBlock: 1}, {Uid: 20, Hotkey: [32]byte{20}, RegistrationBlock: 2}},
		ProviderShare: protocol.Rational{Numerator: 1, Denominator: 10}, TreasuryShare: protocol.Rational{Numerator: 9, Denominator: 10}, MaxWeightLimitU16: 32768}
}

// A separate schema and digest bind the exact destination without deriving it.
// Canonical replay preserves nil signatories, not an alternative empty array.
func TestTreasuryReceivePolicyUsesExplicitPublicAccountAndHashDomain(t *testing.T) {
	policy := treasuryReceivePolicyTestValue()
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(append([]byte(TreasuryReceivePolicySchema+"\n"), raw...))
	actual, err := policy.Hash()
	if err != nil || actual != expected || actual == sha256.Sum256(append([]byte(TreasuryPolicySchema+"\n"), raw...)) {
		t.Fatalf("receive-only policy did not use its explicit account and hash domain: %v", err)
	}
	var restored TreasuryPolicy
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if hash, err := restored.Hash(); err != nil || hash != actual || !reflect.DeepEqual(policy, restored) || restored.Signatories != nil {
		t.Fatalf("receive-only policy changed during canonical replay: %v", err)
	}
	restored.MultisigAccount[0]++
	if hash, err := restored.Hash(); err != nil || hash == actual {
		t.Fatalf("receive-only policy did not bind its destination account: %v", err)
	}
}

// Receiving policy cannot carry spending metadata or relax the complete roster,
// registration generations, split, cap or auto-stake observation requirements.
func TestTreasuryReceivePolicyRejectsSigningMetadataAndIncompleteRoster(t *testing.T) {
	for _, fault := range []string{"schema", "account", "threshold", "signatories", "empty-signatories", "recipient-count", "recipient-order", "recipient-hotkey", "account-hotkey", "generation", "provider-share", "treasury-share", "cap", "destination"} {
		policy := treasuryReceivePolicyTestValue()
		switch fault {
		case "schema":
			policy.Schema = TreasuryPolicySchema
		case "account":
			policy.MultisigAccount = [32]byte{}
		case "threshold":
			policy.Threshold = 2
		case "signatories":
			policy.Signatories = [][32]byte{{1}, {2}}
		case "empty-signatories":
			policy.Signatories = [][32]byte{}
		case "recipient-count":
			policy.Recipients = policy.Recipients[:1]
		case "recipient-order":
			policy.Recipients[0], policy.Recipients[1] = policy.Recipients[1], policy.Recipients[0]
		case "recipient-hotkey":
			policy.Recipients[1].Hotkey = policy.Recipients[0].Hotkey
		case "account-hotkey":
			policy.Recipients[0].Hotkey = policy.MultisigAccount
		case "generation":
			policy.Recipients[0].RegistrationBlock = 0
		case "provider-share":
			policy.ProviderShare.Numerator++
		case "treasury-share":
			policy.TreasuryShare.Numerator--
		case "cap":
			policy.MaxWeightLimitU16++
		case "destination":
			policy.AutoStakeDestination = &[32]byte{}
		}
		if hash, err := policy.Hash(); err == nil || hash != ([32]byte{}) {
			t.Fatalf("%s acquired receive-only policy authority", fault)
		}
	}
	legacy := treasuryPolicyTestValue(t)
	legacy.Threshold, legacy.Signatories = 0, nil
	if err := legacy.Validate(); err == nil {
		t.Fatal("custody schema no longer requires actual public multisig derivation")
	}
	legacy = treasuryPolicyTestValue(t)
	legacy.Schema = TreasuryReceivePolicySchema
	if err := legacy.Validate(); err == nil {
		t.Fatal("custody signing metadata acquired receive-only authority")
	}
}

// Registration selection happens before the same independent production seal;
// this fixture never derives or possesses the receiving account's signing keys.
func configureTreasuryReceiveProductionTest(t *testing.T, fixture *ownerRecycleProductionTestFixture) {
	t.Helper()
	policy := treasuryReceivePolicyTestValue()
	policy.Recipients = nil
	configureTreasuryProductionTestWithPolicy(t, fixture, &policy)
}

// A real signed approval selects receiving policy before any observation.
func newTreasuryReceiveProductionTestFixture(t *testing.T) *ownerRecycleProductionTestFixture {
	t.Helper()
	return newOwnerRecycleProductionTestFixtureWithInputs(t, newRecycleOperatorFixtureWithHotkey, func(fixture *ownerRecycleProductionTestFixture) {
		configureTreasuryReceiveProductionTest(t, fixture)
	})
}

// Public observation, signed policy projection, native preparation and later
// historical replay all retain one account without demanding spending custody.
func TestTreasuryReceiveProductionPreparesAndReplaysExactMeasuredSuccessor(t *testing.T) {
	fixture := newTreasuryReceiveProductionTestFixture(t)
	measurement := fixture.operator.measurement
	admission := measurement.admission
	policy := admission.approval.Proposal.Treasury
	observation, err := ObserveTreasuryAdmission(t.Context(), fixture.cfg, admission.chain)
	if err != nil || !reflect.DeepEqual(observation.TreasuryRecipients, policy.Recipients) {
		t.Fatalf("receive-only policy did not authenticate its complete native roster: %v", err)
	}
	public := [32]byte(admission.private.Public().(ed25519.PublicKey))
	projected, err := VerifyTreasuryApprovalPolicy(admission.raw, public, fixture.cfg.TreasuryApproval.Approval.SHA256)
	if err != nil || !reflect.DeepEqual(projected, policy) || projected.Threshold != 0 || projected.Signatories != nil {
		t.Fatalf("receive-only policy projection acquired signing metadata: %v", err)
	}
	original := bytes.Clone(measurement.encoded)
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	verified, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, stage, intent, original, measurement.provider.artifact, provider)
	if err != nil || intent.Treasury == nil || intent.OwnerRecycle != nil || stage.proof.Schema != treasuryProductionDecisionSchema {
		t.Fatalf("receive-only policy failed actual native preparation and intent verification: %v", err)
	}
	if err := VerifyReleaseMeasurementIntent(intent, measurement.provider.artifact, verified); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(stage.proof.Row.TreasuryUids, []uint16{policy.Recipients[0].Uid, policy.Recipients[1].Uid}) || len(stage.proof.Row.OwnerUids) != 0 {
		t.Fatal("receive-only policy omitted a recipient or relabeled subnet owners")
	}
	providerSum := new(big.Rat)
	for index, uid := range verified.UIDs {
		if slices.Contains(stage.proof.Row.TreasuryUids, uid) {
			if verified.Scores[index].Cmp(big.NewRat(9, 20)) != 0 {
				t.Fatal("receive-only treasury recipient did not receive exactly 45 percent")
			}
		} else {
			providerSum.Add(providerSum, verified.Scores[index])
		}
	}
	if providerSum.Cmp(big.NewRat(1, 10)) != 0 || !bytes.Equal(original, measurement.encoded) || !slices.Equal(provider.MaskedUIDs, verified.MaskedUIDs) {
		t.Fatal("receive-only policy changed provider proof, masks or exact split")
	}
	if err := validateOwnerRecyclePreparedAuthorization(fixture.cfg, intent.Prepared); err == nil {
		t.Fatal("receive-only policy granted durable submission authority")
	}
	fixture.head++
	replayed, replayedProvider := fixture.stage(t)
	if !bytes.Equal(stage.encoded, replayed.encoded) || stage.sourceHash != replayed.sourceHash {
		t.Fatal("receive-only historical replay changed the original authenticated decision")
	}
	if _, err := verifyOwnerRecycleProductionIntent(t.Context(), fixture.cfg, replayed, intent, original, measurement.provider.artifact, replayedProvider); err != nil {
		t.Fatalf("receive-only original intent failed later replay: %v", err)
	}
}

// Receive-only admission executes the same raw native drift refusals as the
// existing custody schema, including Owner, uid, generation and owner set.
func TestTreasuryReceiveAdmissionRejectsRecipientAndOwnerDrift(t *testing.T) {
	testTreasuryAdmissionRejectsRecipientAndOwnerDrift(t, newTreasuryReceiveProductionTestFixture(t))
}

// A receiving account cannot also own a selected production validator.
func TestTreasuryReceiveProductionRejectsValidatorOwnedByDestination(t *testing.T) {
	fixture := newTreasuryReceiveProductionTestFixture(t)
	stage, provider := fixture.stage(t)
	measurement := fixture.operator.measurement
	policy := measurement.admission.approval.Proposal.Treasury
	validator := stage.proof.Eligibility.Validators[0]
	treasuryProductionTestPut(t, fixture, "Owner", policy.MultisigAccount[:], validator.Hotkey[:])
	rejected, err := prepareOwnerRecycleProductionDecision(t.Context(), fixture.cfg, measurement.admission.chain, fixture.operator.chain,
		measurement.encoded, measurement.provider.artifact, provider, measurement.provider.options(t))
	if err == nil || rejected != nil || retryableProductionSteeringRead(err) {
		t.Fatalf("receiving account also acquired production validator ownership: %v", err)
	}
}

// Removing custody data makes a valid receiving policy, but it cannot reuse an
// actual custody approval's signature even when the destination stays exact.
func TestTreasuryReceiveApprovalRejectsCustodySchemaRelabel(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	admission := fixture.operator.measurement.admission
	public := [32]byte(admission.private.Public().(ed25519.PublicKey))
	envelope, err := VerifyTreasuryApproval(admission.raw, public, fixture.cfg.TreasuryApproval.Approval.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	policy := envelope.Approval.Proposal.Treasury
	policy.Schema, policy.Threshold, policy.Signatories = TreasuryReceivePolicySchema, 0, nil
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	if changed, err := VerifyTreasuryApproval(raw, public, attemptHex32(sha256.Sum256(raw))); err == nil || changed != nil {
		t.Fatal("custody signature approved receive-only policy after a schema change")
	}
}
