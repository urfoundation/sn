// Real Ed25519 and exact wire mutations protect every signed staging input.
package protocol

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

// Small valid bytes select no chain authority: these tests cover consent only.
func validatorAttemptUploadIntentFixture() (ValidatorAttemptUploadIntent, ed25519.PrivateKey) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize))
	var vpk [32]byte
	copy(vpk[:], key[ed25519.SeedSize:])
	return ValidatorAttemptUploadIntent{ActivationHash: sha256.Sum256([]byte("activation")), VPK: vpk, ReplicaNoID: 2, SessionHash: sha256.Sum256([]byte("current-client-jwt")), Kind: 2, ContentHash: sha256.Sum256([]byte("records")), Size: 7, NotAfter: 7201}, key
}

// Every field roundtrips; foreign keys and mutations cannot inherit consent.
func TestValidatorAttemptUploadIntentSignsEveryField(t *testing.T) {
	t.Parallel()
	intent, key := validatorAttemptUploadIntentFixture()
	header, err := intent.Sign(key)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := VerifyValidatorAttemptUploadHeader(header)
	if err != nil || decoded != intent {
		t.Fatalf("real signed prerequisite: %v", err)
	}
	encoded, err := base64.RawURLEncoding.DecodeString(header)
	if err != nil {
		t.Fatal(err)
	}
	for index := range encoded {
		mutated := bytes.Clone(encoded)
		mutated[index] ^= 1
		if _, err := VerifyValidatorAttemptUploadHeader(base64.RawURLEncoding.EncodeToString(mutated)); err == nil {
			t.Fatalf("signed byte %d mutation inherited consent", index)
		}
	}
	for _, invalid := range []ed25519.PrivateKey{nil, ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, ed25519.SeedSize)), append(bytes.Clone(key[:32]), bytes.Repeat([]byte{3}, 32)...)} {
		if _, err := intent.Sign(invalid); err == nil {
			t.Fatal("foreign or inconsistent signer accepted")
		}
	}
}

// Narrow canonical decoding cannot allocate from an untrusted length field.
func TestValidatorAttemptUploadIntentRejectsMalformedAndZeroFields(t *testing.T) {
	t.Parallel()
	good, key := validatorAttemptUploadIntentFixture()
	header, err := good.Sign(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []string{"", header[:len(header)-1], header + "=", header + "A", header[:len(header)-1] + "!"} {
		if _, err := VerifyValidatorAttemptUploadHeader(malformed); err == nil {
			t.Fatal("malformed fixed envelope accepted")
		}
	}
	for _, field := range []string{"activation", "vpk", "replica", "session", "kind-zero", "kind-other", "hash", "size", "size-overflow", "expiry", "expiry-overflow"} {
		next := good
		switch field {
		case "activation":
			next.ActivationHash = [32]byte{}
		case "vpk":
			next.VPK = [32]byte{}
		case "replica":
			next.ReplicaNoID = 0
		case "session":
			next.SessionHash = [32]byte{}
		case "kind-zero":
			next.Kind = 0
		case "kind-other":
			next.Kind = 4
		case "hash":
			next.ContentHash = [32]byte{}
		case "size":
			next.Size = 0
		case "size-overflow":
			next.Size = 9007199254740992
		case "expiry":
			next.NotAfter = 0
		case "expiry-overflow":
			next.NotAfter = 9007199254740992
		}
		if _, err := next.Payload(); err == nil {
			t.Fatalf("%s invalid field accepted", field)
		}
	}
}

// Idempotency excludes mutable API sessions/time but retains exact object and
// activation identity. Full signature validation still binds those exclusions.
func TestValidatorAttemptUploadIntentRetryPreservesObjectReservation(t *testing.T) {
	t.Parallel()
	good, key := validatorAttemptUploadIntentFixture()
	reservation, err := good.ObjectReservationHash()
	if err != nil {
		t.Fatal(err)
	}
	next := good
	next.SessionHash = sha256.Sum256([]byte("refreshed-client-jwt"))
	next.NotAfter++
	hash, err := next.ObjectReservationHash()
	if err != nil || hash != reservation {
		t.Fatalf("refresh minted an object reservation: %v", err)
	}
	first, err := good.Sign(key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := next.Sign(key)
	if err != nil || first == second {
		t.Fatalf("refresh reused old session consent: %v", err)
	}
	for _, field := range []string{"activation", "kind", "hash", "size"} {
		changed := good
		switch field {
		case "activation":
			changed.ActivationHash[0] ^= 1
		case "kind":
			changed.Kind = 3
		case "hash":
			changed.ContentHash[0] ^= 1
		case "size":
			changed.Size++
		}
		hash, err := changed.ObjectReservationHash()
		if err != nil || hash == reservation {
			t.Fatalf("%s object substitution reused reservation: %v", field, err)
		}
	}
}

// Token hashing is bounded and exact; parsing JWT authority remains exclusively
// the server's existing real authentication and live account-state boundary.
func TestValidatorAttemptUploadSessionHashRejectsCredentialAliases(t *testing.T) {
	t.Parallel()
	first, err := ValidatorAttemptUploadSessionHash("header.payload.signature")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ValidatorAttemptUploadSessionHash("header.other.signature")
	if err != nil || first == second {
		t.Fatal("different authenticated accounts share intent binding")
	}
	for _, credential := range []string{"", " token", "token ", "token\n", string(bytes.Repeat([]byte{'a'}, 16*1024+1)), "non-ascii-é"} {
		if _, err := ValidatorAttemptUploadSessionHash(credential); err == nil {
			t.Fatal("ambiguous or unbounded credential accepted")
		}
	}
}
