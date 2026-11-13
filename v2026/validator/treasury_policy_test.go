//go:build linux || darwin

// Public-only synthetic custody exercises finite policy, canonical hashing and
// metadata shape checks independently of a claimed runtime version number.
package validator

import (
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// These generated account bytes identify no device or production signer.
func treasuryPolicyTestValue(t *testing.T) TreasuryPolicy {
	t.Helper()
	policy := TreasuryPolicy{Schema: TreasuryPolicySchema, Threshold: 2, Signatories: [][32]byte{{1}, {2}, {3}},
		Recipients:    []TreasuryRecipient{{Uid: 10, Hotkey: [32]byte{10}, RegistrationBlock: 1}, {Uid: 20, Hotkey: [32]byte{20}, RegistrationBlock: 2}},
		ProviderShare: protocol.Rational{Numerator: 1, Denominator: 10}, TreasuryShare: protocol.Rational{Numerator: 9, Denominator: 10}, MaxWeightLimitU16: 32768}
	var err error
	policy.MultisigAccount, err = crv4.DeriveNativeMultisigAccount(policy.Signatories, policy.Threshold)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

// Every recipient and custody choice participates in the signed public hash.
func TestTreasuryPolicyRequiresCompleteCanonicalCustodyAndSplit(t *testing.T) {
	valid := treasuryPolicyTestValue(t)
	raw, _ := json.Marshal(valid)
	expected := sha256.Sum256(append([]byte("urnetwork-native-treasury-policy-v1\n"), raw...))
	if actual, err := valid.Hash(); err != nil || actual != expected {
		t.Fatalf("canonical treasury policy hash differs: %v", err)
	}
	for _, fault := range []string{"schema", "threshold", "signatory-order", "signatory-duplicate", "account", "recipient-count", "recipient-order", "recipient-hotkey", "generation", "provider-share", "treasury-share", "cap", "destination"} {
		var policy TreasuryPolicy
		if err := json.Unmarshal(raw, &policy); err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "schema":
			policy.Schema = ownerRecycleProposalSchema
		case "threshold":
			policy.Threshold = 1
		case "signatory-order":
			policy.Signatories[0], policy.Signatories[1] = policy.Signatories[1], policy.Signatories[0]
		case "signatory-duplicate":
			policy.Signatories[1] = policy.Signatories[0]
		case "account":
			policy.MultisigAccount[0] ^= 1
		case "recipient-count":
			policy.Recipients = policy.Recipients[:1]
		case "recipient-order":
			policy.Recipients[0], policy.Recipients[1] = policy.Recipients[1], policy.Recipients[0]
		case "recipient-hotkey":
			policy.Recipients[1].Hotkey = policy.Recipients[0].Hotkey
		case "generation":
			policy.Recipients[0].RegistrationBlock = 0
		case "provider-share":
			policy.ProviderShare = protocol.Rational{Numerator: 2, Denominator: 10}
		case "treasury-share":
			policy.TreasuryShare = protocol.Rational{Numerator: 8, Denominator: 10}
		case "cap":
			policy.MaxWeightLimitU16++
		case "destination":
			policy.AutoStakeDestination = &[32]byte{}
		}
		if hash, err := policy.Hash(); err == nil || hash != ([32]byte{}) {
			t.Fatalf("%s acquired a valid treasury policy hash", fault)
		}
	}
	if reflect.DeepEqual(valid.MultisigAccount, valid.Signatories[0]) {
		t.Fatal("synthetic derivation returned one signatory")
	}
}

// An authenticated metadata digest cannot reinterpret Owner or destination
// bytes through a changed hasher, key tuple, value type or query modifier.
func TestTreasuryStorageProfileRejectsChangedCustodyInterface(t *testing.T) {
	fixture := newTreasuryProductionTestFixture(t)
	entries, err := ownerRecycleStorageProfile(fixture.metadata)
	if err != nil || treasuryStorageProfile(fixture.metadata, entries) != nil {
		t.Fatalf("healthy treasury storage profile differs: %v", err)
	}
	for _, fault := range []string{"owner-hasher", "owner-query", "owner-value", "destination-hasher", "destination-query", "destination-key", "destination-value"} {
		changed := make(map[string]types.StorageEntryMetadataV14, len(entries))
		for name, entry := range entries {
			changed[name] = entry
		}
		owner, destination := changed["Owner"], changed["AutoStakeDestination"]
		switch fault {
		case "owner-hasher":
			owner.Type.AsMap.Hashers = []types.StorageHasherV10{{IsIdentity: true}}
		case "owner-query":
			owner.Modifier = types.StorageFunctionModifierV0{IsOptional: true}
		case "owner-value":
			owner.Type.AsMap.Value = entries["SubnetworkN"].Type.AsMap.Value
		case "destination-hasher":
			destination.Type.AsMap.Hashers = []types.StorageHasherV10{{IsIdentity: true}, {IsIdentity: true}}
		case "destination-query":
			destination.Modifier = types.StorageFunctionModifierV0{IsDefault: true}
		case "destination-key":
			destination.Type.AsMap.Key = entries["Keys"].Type.AsMap.Key
		case "destination-value":
			destination.Type.AsMap.Value = entries["SubnetworkN"].Type.AsMap.Value
		}
		changed["Owner"], changed["AutoStakeDestination"] = owner, destination
		if err := treasuryStorageProfile(fixture.metadata, changed); err == nil {
			t.Fatalf("%s reinterpreted treasury custody storage", fault)
		}
	}
}
