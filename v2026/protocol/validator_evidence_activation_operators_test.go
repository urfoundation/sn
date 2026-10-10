package protocol

// Per-operator prefixes/VPKs produce distinct earlier anchors for one native
// validator. Their common deployment does not erase any individual authority.

import (
	"bytes"
	"crypto/ed25519"
	"testing"
)

// Two genuine operators sign and verify their own starting states. Later
// headers must retain those distinct hashes, even under one shared deployment.
func TestValidatorEvidenceActivationDistinctOperatorAnchors(t *testing.T) {
	t.Parallel()
	first, firstKey, hotkey := validatorEvidenceActivationFixture(t)
	second := first
	second.NoID++
	second.FirstSequence, second.PriorRoot = 13, [32]byte{0x25}
	secondKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x65}, ed25519.SeedSize))
	copy(second.VPK[:], secondKey.Public().(ed25519.PublicKey))
	if first.Domain != second.Domain || first.Hotkey != second.Hotkey || first.VPK == second.VPK {
		t.Fatal("fixture does not share one deployment/native validator with distinct operator VPKs")
	}
	firstDomain, err := first.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	secondDomain, err := second.EvidenceDomain()
	if err != nil || firstDomain.ActivationHash == secondDomain.ActivationHash {
		t.Fatalf("different genuine operator activations lost their individual anchors: %v", err)
	}
	for _, pair := range []struct {
		activation, other   ValidatorEvidenceActivation
		key                 ed25519.PrivateKey
		domain, otherDomain ValidatorEvidenceDomain
	}{
		{activation: first, other: second, key: firstKey, domain: firstDomain, otherDomain: secondDomain},
		{activation: second, other: first, key: secondKey, domain: secondDomain, otherDomain: firstDomain},
	} {
		vpkSignature, hotkeySignature := signValidatorEvidenceActivationTest(t, pair.activation, pair.key, hotkey)
		if err := pair.activation.Verify(pair.activation, vpkSignature, hotkeySignature); err != nil {
			t.Fatalf("real operator activation refused: %v", err)
		}
		if err := pair.activation.Verify(pair.other, vpkSignature, hotkeySignature); err == nil {
			t.Fatal("one operator's activation replaced the other's independent starting state")
		}
		header, _, _ := validatorEvidenceFixture(t)
		header.Domain, header.NoID, header.VPK = pair.domain, pair.activation.NoID, pair.activation.VPK
		vpkSignature, hotkeySignature = validatorEvidenceSign(t, header, pair.key, hotkey)
		if err := header.Verify(pair.domain, validatorEvidenceWindow(header), vpkSignature, hotkeySignature); err != nil {
			t.Fatalf("later header refused its genuine operator activation: %v", err)
		}
		if err := header.Verify(pair.otherDomain, validatorEvidenceWindow(header), vpkSignature, hotkeySignature); err == nil {
			t.Fatal("common deployment bypassed the exact per-operator activation hash")
		}
	}
}

// Empty bootstrap remains a real dual-signed record, not a signature exception
// or an inferred reset of the original nonempty migration prefix.
func TestValidatorEvidenceActivationEmptyEpochZeroRequiresRealConsent(t *testing.T) {
	t.Parallel()
	original, key, hotkey := validatorEvidenceActivationFixture(t)
	priorVPKSignature, priorHotkeySignature := signValidatorEvidenceActivationTest(t, original, key, hotkey)
	empty := original
	empty.Domain.Epoch, empty.FirstSequence, empty.PriorRoot = 0, 1, [32]byte{}
	vpkSignature, hotkeySignature := signValidatorEvidenceActivationTest(t, empty, key, hotkey)
	if err := empty.Verify(empty, vpkSignature, hotkeySignature); err != nil {
		t.Fatalf("dual-signed epoch-zero bootstrap refused: %v", err)
	}
	if empty.VerifyVPK(priorVPKSignature) || empty.VerifyHotkey(priorHotkeySignature) {
		t.Fatal("nonempty migration consent authorized an empty bootstrap")
	}
	if err := empty.Verify(original, vpkSignature, hotkeySignature); err == nil {
		t.Fatal("genuine empty consent reset an independently expected nonempty prefix")
	}
	payload, err := empty.Payload()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeValidatorEvidenceActivationPayload(payload)
	if err != nil || decoded != empty {
		t.Fatalf("signed empty bootstrap failed exact packed transport: %v", err)
	}
	if err := decoded.Verify(empty, vpkSignature, hotkeySignature); err != nil {
		t.Fatalf("packed empty bootstrap lost genuine consent: %v", err)
	}
}
