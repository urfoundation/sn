// Approval v1 uses the existing bounded bare-hex signature grammar. These
// tests round-trip the actual artifact formatter through request admission;
// malformed wire text and a correctly formatted foreign signature differ.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// The actual signed fixture file is decoded before checking the same approval
// path used by the public producer, without dispatching an expensive replay.
func economicNativeFeeSignatureFixture(t *testing.T) (economicNativeFeeRequest, economicNativeFeeApproval, historicalReplayJob, ed25519.PrivateKey) {
	t.Helper()
	input, _, job := historicalFeeContextTestFixture(t, "success", "pair")
	request, _, key := economicNativeFeeTestRequestForContext(t, input, job)
	raw, err := os.ReadFile(request.Approval.Path)
	if err != nil || monitorReadDigest(raw) != request.Approval.Sha256 {
		t.Fatal("actual approval fixture differs from its source pin", err)
	}
	var approval economicNativeFeeApproval
	if err := decodePlanJson(raw, &approval); err != nil {
		t.Fatal(err)
	}
	return request, approval, job, key
}

func TestEconomicNativeFeeApprovalCanonicalSignatureRoundTrip(t *testing.T) {
	request, approval, job, key := economicNativeFeeSignatureFixture(t)
	message, err := approval.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := rootOfflineSignatureBytes(approval.Signature)
	if err != nil || len(approval.Signature) != 2*ed25519.SignatureSize || approval.Signature != hex.EncodeToString(signature) || !bytes.Equal(signature, ed25519.Sign(key, message)) {
		t.Fatal("native fee v1 formatter and canonical signature decoder disagree", err)
	}
	if err := request.validate(approval, job); err != nil {
		t.Fatal("actual canonical approval did not reach native fee admission", err)
	}
}

func TestEconomicNativeFeeApprovalRejectsNoncanonicalSignatureWire(t *testing.T) {
	request, approval, job, _ := economicNativeFeeSignatureFixture(t)
	if err := request.validate(approval, job); err != nil {
		t.Fatal("positive signature counterpart did not admit", err)
	}
	for _, signature := range []string{
		"0x" + approval.Signature,
		strings.ToUpper(approval.Signature),
		approval.Signature[:len(approval.Signature)-2],
		approval.Signature + "00",
		"gg" + approval.Signature[2:],
		" " + approval.Signature,
		approval.Signature + "\n",
		"",
	} {
		changed := approval
		changed.Signature = signature
		if _, err := rootOfflineSignatureBytes(signature); err == nil {
			t.Fatal("noncanonical native fee signature decoded", signature)
		}
		if err := request.validate(changed, job); err == nil || !strings.Contains(err.Error(), "signature is invalid") {
			t.Fatal("noncanonical native fee signature reached admission", signature, err)
		}
	}
}

func TestEconomicNativeFeeApprovalRejectsCanonicalForeignSignature(t *testing.T) {
	request, approval, job, _ := economicNativeFeeSignatureFixture(t)
	if err := request.validate(approval, job); err != nil {
		t.Fatal("positive original-key counterpart did not admit", err)
	}
	foreign := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x68}, ed25519.SeedSize))
	economicNativeFeeTestSign(t, &request, &approval, foreign)
	if _, err := rootOfflineSignatureBytes(approval.Signature); err != nil {
		t.Fatal("foreign signature must pass the unchanged canonical wire decoder", err)
	}
	if err := request.validate(approval, job); err == nil || !strings.Contains(err.Error(), "signature is invalid") {
		t.Fatal("canonical foreign signer replaced independent native fee authority", err)
	}
}
