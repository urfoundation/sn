// Original registry signatures, retained-head continuity and cancellation are
// exercised without replacing the canonical producer or signature verifier.
package protocol

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
)

// All identities and signing seeds are synthetic and local to these tests.
func providerAttemptRegistryFixture(t *testing.T) ([]ProviderAttemptRegistry, ProviderAttemptRegistryExpectation, ed25519.PrivateKey) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	expected := ProviderAttemptRegistryExpectation{Domain: ProviderAttemptDomain{ChainId: 945, GenesisHash: [32]byte{1}, Netuid: 17, Coordinator: [20]byte{2}, SettlementVault: [20]byte{3}, DeploymentIdHash: [32]byte{4}, PolicyHash: [32]byte{5}}, Signer: [32]byte(key.Public().(ed25519.PublicKey)), MaxRevisions: 4, MaxOwners: 4, MaxOperatorLanes: 8, MaxBytes: 32 * 1024}
	root, err := SealProviderAttemptRegistry(t.Context(), ProviderAttemptRegistry{Schema: ProviderAttemptRegistrySchema, Domain: expected.Domain, EffectiveEpoch: 7, Owners: []ProviderAttemptOwner{{Hotkey: [32]byte{6}, NoIds: []uint64{9, 11}}, {Hotkey: [32]byte{7}, NoIds: []uint64{9, 11}}}}, expected, key)
	if err != nil {
		t.Fatal(err)
	}
	expected.RootHash, err = root.Hash()
	if err != nil {
		t.Fatal(err)
	}
	expected.RequiredHeadHash = expected.RootHash
	return []ProviderAttemptRegistry{*root}, expected, key
}

// The expected registry keeps idle and no-payout lanes; candidate contents do
// not select the validator population or remove an unhelpful operator lane.
func TestProviderAttemptRegistryOriginalCompleteOwners(t *testing.T) {
	history, expected, _ := providerAttemptRegistryFixture(t)
	result, err := VerifyProviderAttemptRegistry(t.Context(), history, 7, expected)
	if err != nil || result == nil || len(result.Owners) != 2 || len(result.Owners[1].NoIds) != 2 {
		t.Fatalf("original complete owners were lost: %+v %v", result, err)
	}
	history[0].Owners[0].NoIds[0] = 23
	if result.Owners[0].NoIds[0] != 9 {
		t.Fatal("verified registry borrowed mutable candidate ownership")
	}
}

// A candidate may not replace its independent original pin with a new valid
// signature from the same deployment key over a smaller census.
func TestProviderAttemptRegistryRefusesResignedSubset(t *testing.T) {
	history, expected, key := providerAttemptRegistryFixture(t)
	value := history[0]
	value.Owners = value.Owners[:1]
	subset, err := SealProviderAttemptRegistry(t.Context(), value, expected, key)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := VerifyProviderAttemptRegistry(t.Context(), []ProviderAttemptRegistry{*subset}, 7, expected); result != nil || !errors.Is(err, ErrProviderAttemptsIntegrity) {
		t.Fatalf("resigned subset replaced independent owner census: %+v %v", result, err)
	}
}

// Original-key successors are admitted explicitly. A retained newer head may
// not disappear during restart or a response containing only an older prefix.
func TestProviderAttemptRegistryRetainsSignedSuccessorAndOldWindow(t *testing.T) {
	history, expected, key := providerAttemptRegistryFixture(t)
	successor := history[0]
	successor.Revision, successor.EffectiveEpoch, successor.PreviousHash = 1, 9, expected.RootHash
	successor.Owners = []ProviderAttemptOwner{{Hotkey: [32]byte{6}, NoIds: []uint64{9, 11}}, {Hotkey: [32]byte{8}, NoIds: []uint64{9, 11}}}
	signed, err := SealProviderAttemptRegistry(t.Context(), successor, expected, key)
	if err != nil {
		t.Fatal(err)
	}
	expected.RequiredHeadHash, err = signed.Hash()
	if err != nil {
		t.Fatal(err)
	}
	history = append(history, *signed)
	old, err := VerifyProviderAttemptRegistry(t.Context(), history, 8, expected)
	if err != nil || old == nil || old.Revision != 0 {
		t.Fatalf("future revision rewrote original window: %+v %v", old, err)
	}
	current, err := VerifyProviderAttemptRegistry(t.Context(), history, 9, expected)
	if err != nil || current == nil || current.Revision != 1 {
		t.Fatalf("signed exact predecessor did not activate: %+v %v", current, err)
	}
	if result, err := VerifyProviderAttemptRegistry(t.Context(), history[:1], 9, expected); result != nil || !errors.Is(err, ErrProviderAttemptsIntegrity) {
		t.Fatalf("retained registry head was rolled back: %+v %v", result, err)
	}
}

// A valid signature cannot change the fixed policy/domain or reuse an epoch
// for a later roster. The canonical producer itself rejects duplicates.
func TestProviderAttemptRegistryRefusesDuplicateAndForeignDomain(t *testing.T) {
	history, expected, key := providerAttemptRegistryFixture(t)
	for _, mutate := range []func(*ProviderAttemptRegistry){
		func(value *ProviderAttemptRegistry) { value.Domain.Netuid++ },
		func(value *ProviderAttemptRegistry) { value.Owners[1].Hotkey = value.Owners[0].Hotkey },
		func(value *ProviderAttemptRegistry) { value.Owners[0].NoIds = []uint64{9, 9} },
	} {
		value := history[0]
		value.Owners = append([]ProviderAttemptOwner(nil), value.Owners...)
		mutate(&value)
		if result, err := SealProviderAttemptRegistry(t.Context(), value, expected, key); result != nil || !errors.Is(err, ErrProviderAttemptsIntegrity) {
			t.Fatalf("foreign/duplicate original registry was signed: %+v %v", result, err)
		}
	}
}

// A missing original remains unknown; a signed changed byte is a contradiction.
func TestProviderAttemptRegistryMissingAndChangedSignatureStayDistinct(t *testing.T) {
	history, expected, _ := providerAttemptRegistryFixture(t)
	if result, err := VerifyProviderAttemptRegistry(t.Context(), nil, 7, expected); result != nil || !errors.Is(err, ErrProviderAttemptsUnavailable) || errors.Is(err, ErrProviderAttemptsIntegrity) {
		t.Fatalf("missing original was promoted to integrity: %+v %v", result, err)
	}
	history[0].Signature[0] ^= 1
	if result, err := VerifyProviderAttemptRegistry(t.Context(), history, 7, expected); result != nil || !errors.Is(err, ErrProviderAttemptsIntegrity) {
		t.Fatalf("changed signature was accepted: %+v %v", result, err)
	}
}

// Cancellation and capacity are operational causes and publish no stale result.
func TestProviderAttemptRegistryCancellationAndCapacity(t *testing.T) {
	history, expected, _ := providerAttemptRegistryFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := VerifyProviderAttemptRegistry(ctx, history, 7, expected); result != nil || !errors.Is(err, context.Canceled) || errors.Is(err, ErrProviderAttemptsIntegrity) {
		t.Fatalf("canceled registry acquired integrity: %+v %v", result, err)
	}
	expected.MaxOwners = 1
	if result, err := VerifyProviderAttemptRegistry(t.Context(), history, 7, expected); result != nil || !errors.Is(err, ErrProviderAttemptsCapacity) || errors.Is(err, ErrProviderAttemptsIntegrity) {
		t.Fatalf("registry capacity was misclassified: %+v %v", result, err)
	}
}
