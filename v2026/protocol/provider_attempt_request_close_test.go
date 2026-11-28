// Original request consent and Server execution fences use distinct keys.
package protocol

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/urnetwork/connect/v2026"
)

// Synthetic original wire is created before the later closed-window consent.
func providerRequestCloseFixture(t *testing.T) (ProviderAttemptRequestClosure, ed25519.PrivateKey) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{31}, 32))
	message, err := connect.BuildVerifySeedMessage(key.Public().(ed25519.PublicKey), bytes.Repeat([]byte{32}, 32), 8)
	if err != nil {
		t.Fatal(err)
	}
	value := ProviderAttemptRequestClosure{Schema: ProviderAttemptRequestCloseDomain, Scope: ProviderAttemptReceiptScope{Profile: "synthetic", GenesisHash: [32]byte{1}, DeploymentId: "synthetic-close", DeploymentKey: "synthetic-close:1", PolicyHash: [32]byte{2}, Netuid: 1, NoId: 9}, ClientId: connect.Id{3}, Message: message, RequestSignature: ed25519.Sign(key, message), CutHash: [32]byte{4}, Epoch: 5, EndBlock: 60}
	signed, err := SealProviderAttemptRequestClosure(t.Context(), value, key)
	if err != nil {
		t.Fatal(err)
	}
	return *signed, key
}

// A valid copied request signature does not grant another caller close consent.
func TestProviderAttemptCloseRequiresOriginalOwnerAndExactDomain(t *testing.T) {
	value, key := providerRequestCloseFixture(t)
	if err := VerifyProviderAttemptRequestClosure(t.Context(), value, value.Scope); err != nil {
		t.Fatal(err)
	}
	foreign := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{33}, 32))
	if signed, err := SealProviderAttemptRequestClosure(t.Context(), value, foreign); err == nil || signed != nil {
		t.Fatal("foreign close key was admitted")
	}
	changed := value
	changed.Scope.NoId++
	if err := VerifyProviderAttemptRequestClosure(t.Context(), changed, value.Scope); !errors.Is(err, ErrProviderAttemptsIntegrity) {
		t.Fatal("foreign close domain was admitted", err)
	}
	changed = value
	changed.CutHash[0]++
	if err := VerifyProviderAttemptRequestClosure(t.Context(), changed, value.Scope); !errors.Is(err, ErrProviderAttemptsIntegrity) {
		t.Fatal("changed close cut was admitted", err)
	}
	value.Message[0] ^= 1
	if signed, err := SealProviderAttemptRequestClosure(t.Context(), value, key); err == nil || signed != nil {
		t.Fatal("invalid original wire was re-signed into authority")
	}
}

// An immutable tombstone cannot migrate to another request/cut after a lost reply.
func TestProviderAttemptClosedUnreceivedBindsFirstOriginalAndHistoricalKey(t *testing.T) {
	value, _ := providerRequestCloseFixture(t)
	serverKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{34}, 32))
	closed, err := SealProviderAttemptClosedUnreceived(t.Context(), value, 7, serverKey)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[byte]ed25519.PublicKey{7: serverKey.Public().(ed25519.PublicKey)}
	if err := VerifyProviderAttemptClosedUnreceived(t.Context(), *closed, value, keys); err != nil {
		t.Fatal(err)
	}
	changed := value
	changed.EndBlock++
	if err := VerifyProviderAttemptClosedUnreceived(t.Context(), *closed, changed, keys); !errors.Is(err, ErrProviderAttemptsIntegrity) {
		t.Fatal("tombstone rebound to another close", err)
	}
	if err := VerifyProviderAttemptClosedUnreceived(t.Context(), *closed, value, map[byte]ed25519.PublicKey{}); !errors.Is(err, ErrProviderAttemptsIntegrity) {
		t.Fatal("missing original Server key became authority", err)
	}
}
