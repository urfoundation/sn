package protocol

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

func validatorClassTestIntent(t *testing.T) (ValidatorClassIntent, ed25519.PrivateKey) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(append([]byte{0x41}, make([]byte, 31)...))
	intent := ValidatorClassIntent{ActivationHash: [32]byte{1}, ReplicaNoID: 9, SessionHash: [32]byte{2}, NotAfter: 1700000000}
	copy(intent.VPK[:], key[ed25519.SeedSize:])
	return intent, key
}

// A signed intent verifies to itself; any change to the envelope does not.
func TestValidatorClassIntentSignsAndVerifies(t *testing.T) {
	intent, key := validatorClassTestIntent(t)
	header, err := intent.Sign(key)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyValidatorClassHeader(header)
	if err != nil || verified != intent {
		t.Fatalf("signed intent did not verify to itself: %+v / %v", verified, err)
	}
	tampered := []byte(header)
	tampered[len(tampered)/2] ^= 1
	if _, err := VerifyValidatorClassHeader(string(tampered)); err == nil {
		t.Fatal("a tampered class intent verified")
	}
	if _, err := VerifyValidatorClassHeader(header + "A"); err == nil {
		t.Fatal("an overlong class intent verified")
	}
}

// Only the VPK's own key signs, and an incomplete intent never does.
func TestValidatorClassIntentRefusesOtherKeysAndGaps(t *testing.T) {
	intent, _ := validatorClassTestIntent(t)
	other := ed25519.NewKeyFromSeed(append([]byte{0x42}, make([]byte, 31)...))
	if _, err := intent.Sign(other); err == nil {
		t.Fatal("another key signed the VPK's consent")
	}
	_, key := validatorClassTestIntent(t)
	for name, change := range map[string]func(*ValidatorClassIntent){
		"activation": func(i *ValidatorClassIntent) { i.ActivationHash = [32]byte{} },
		"replica":    func(i *ValidatorClassIntent) { i.ReplicaNoID = 0 },
		"session":    func(i *ValidatorClassIntent) { i.SessionHash = [32]byte{} },
		"expiry":     func(i *ValidatorClassIntent) { i.NotAfter = 0 },
	} {
		changed := intent
		change(&changed)
		if _, err := changed.Sign(key); err == nil {
			t.Errorf("an intent without its %s was signed", name)
		}
	}
}

// An upload consent and a class consent cannot stand in for each other.
func TestValidatorClassIntentIsNotAnUploadConsent(t *testing.T) {
	intent, key := validatorClassTestIntent(t)
	header, err := intent.Sign(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyValidatorAttemptUploadHeader(header); err == nil {
		t.Fatal("a class consent verified as an upload consent")
	}
	upload := ValidatorAttemptUploadIntent{ActivationHash: intent.ActivationHash, VPK: intent.VPK, ReplicaNoID: 9, SessionHash: intent.SessionHash, Kind: 1, ContentHash: [32]byte{3}, Size: 4, NotAfter: intent.NotAfter}
	uploadHeader, err := upload.Sign(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyValidatorClassHeader(uploadHeader); err == nil || !strings.Contains(err.Error(), "length") {
		t.Fatalf("an upload consent verified as a class consent: %v", err)
	}
}
