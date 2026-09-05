// Exercise durable proof framing with locally generated synthetic signatures;
// the locator, JSON decoder, and cryptographic verifier remain production code.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

// Reject malformed bytes after a complete signed record, even when the
// artifact's exact size and digest have been recomputed for those bytes.
func TestFinalSemanticPathProofArtifactRejectsMalformedTrailingJSON(t *testing.T) {
	serverPrivateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, ed25519.SeedSize))
	validatorPrivateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, ed25519.SeedSize))
	serverPublicKey := serverPrivateKey.Public().(ed25519.PublicKey)
	validatorPublicKey := validatorPrivateKey.Public().(ed25519.PublicKey)
	trailId := connect.Id{0: 1, 15: 7}
	trailIds := []connect.Id{{0: 2, 15: 1}, {0: 2, 15: 2}}
	hops := []connect.VerifyProofHop{
		{ClientId: trailIds[0], TimeMs: 1_000},
		{ClientId: trailIds[1], TimeMs: 1_001},
	}
	nonce := bytes.Repeat([]byte{0x73}, connect.VerifyNonceSize)
	finalMessage, err := connect.BuildVerifyFinalMessage(1, trailId, nonce, validatorPublicKey, byte(len(hops)), hops)
	if err != nil {
		t.Fatal(err)
	}
	extendMessage, err := connect.BuildVerifyExtendMessage(trailId, nonce, validatorPublicKey, byte(len(hops)), trailIds)
	if err != nil {
		t.Fatal(err)
	}
	finalDigest := connect.VerifyFinalDigest(finalMessage)
	pathId := validatorpkg.TrailPathId(trailId, validatorPublicKey, 1)
	record := validatorpkg.ProofRecord{
		Version: 1, Epoch: 10, TrailId: trailId, ServerNonce: nonce,
		Vpk: validatorPublicKey, M: len(hops), Hops: hops, ServerKeyId: 1,
		FinalSig:    ed25519.Sign(serverPrivateKey, finalMessage),
		VerifierSig: ed25519.Sign(validatorPrivateKey, extendMessage),
		FinalDigest: finalDigest[:], VpkSig: ed25519.Sign(validatorPrivateKey, finalMessage),
		Coverage: uint64(len(hops) - 1), PathId: pathId[:], CompleteTimeMs: hops[len(hops)-1].TimeMs,
	}
	if err := validatorpkg.VerifyProofRecord(&record, validatorPublicKey, map[byte]ed25519.PublicKey{1: serverPublicKey}, len(hops)); err != nil {
		t.Fatalf("synthetic signed proof control: %v", err)
	}
	validLine, err := json.Marshal(FinalCollectedProofRecord{Schema: finalCollectedProofRecordSchema, Record: record})
	if err != nil {
		t.Fatal(err)
	}
	invalidSignatureRecord := record
	invalidSignatureRecord.FinalSig = append([]byte(nil), record.FinalSig...)
	invalidSignatureRecord.FinalSig[0] ^= 1
	invalidSignatureLine, err := json.Marshal(FinalCollectedProofRecord{Schema: finalCollectedProofRecordSchema, Record: invalidSignatureRecord})
	if err != nil {
		t.Fatal(err)
	}
	validator := FinalValidatorIdentityEvidence{ValidatorID: 1, PathVPK: "0x" + hex.EncodeToString(validatorPublicKey)}
	pool := FinalPoolUIDEvidence{NoID: 1, ServerKeyHistory: []FinalServerKey{{KeyID: 1, PublicKey: "0x" + hex.EncodeToString(serverPublicKey)}}}
	cases := []struct {
		name      string
		line      []byte
		suffix    string
		wantError string
	}{
		{name: "complete signed record", line: validLine},
		{name: "trailing whitespace", line: validLine, suffix: " \t\r"},
		{name: "second JSON value", line: validLine, suffix: " {}", wantError: "line 1 contains trailing JSON"},
		{name: "incomplete object", line: validLine, suffix: " {", wantError: "line 1 contains trailing JSON"},
		{name: "incomplete array", line: validLine, suffix: " [", wantError: "line 1 contains trailing JSON"},
		{name: "invalid trailing token", line: validLine, suffix: " garbage", wantError: "line 1 contains trailing JSON"},
		{name: "incomplete string", line: validLine, suffix: " \"", wantError: "line 1 contains trailing JSON"},
		{name: "invalid server signature", line: invalidSignatureLine, wantError: "server FINAL signature is invalid"},
	}
	for _, testCase := range cases {
		data := append(append(append([]byte(nil), testCase.line...), testCase.suffix...), '\n')
		locator := FinalArtifactLocator{Kind: "validator-path-proofs", URI: "synthetic-signed-proofs.jsonl", ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
		proof := FinalValidatorPathProofEvidence{ValidatorID: 1, NoID: 1, FirstEpoch: 10, LastEpoch: 10, ProofCount: 1, TrailDepth: len(hops), ProofsHash: locator.ContentHash, Artifact: locator}
		loaderCalls := 0
		artifactURIBytes, err := loadFinalSemanticArtifactUses(context.Background(), []finalSemanticArtifactUse{{locator: locator, pathProof: &proof}}, func(_ context.Context, loadedLocator FinalArtifactLocator) ([]byte, error) {
			loaderCalls++
			if loadedLocator != locator {
				t.Fatalf("%s: loader changed the expected locator", testCase.name)
			}
			return append([]byte(nil), data...), nil
		})
		if err != nil || loaderCalls != 1 || !bytes.Equal(artifactURIBytes[locator.URI], data) {
			t.Fatalf("%s: rehashed artifact load calls=%d error=%v", testCase.name, loaderCalls, err)
		}
		seenPathIds, seenTrailIds := map[string]bool{}, map[string]bool{}
		err = verifyFinalPathProofArtifactBound(&proof, artifactURIBytes[locator.URI], &validator, &pool, seenPathIds, seenTrailIds)
		if testCase.wantError == "" {
			if err != nil || len(seenPathIds) != 1 || len(seenTrailIds) != 1 {
				t.Errorf("%s: valid signed proof rejected: error=%v paths=%d trails=%d", testCase.name, err, len(seenPathIds), len(seenTrailIds))
			}
		} else if err == nil || !strings.Contains(err.Error(), testCase.wantError) || len(seenPathIds) != 0 || len(seenTrailIds) != 0 {
			t.Errorf("%s: signed proof framing error=%v paths=%d trails=%d, want %q before accepting identities", testCase.name, err, len(seenPathIds), len(seenTrailIds), testCase.wantError)
		}
	}
}
