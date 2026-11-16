// Legacy Json/signature oracles exercise the actual discard-only prior reader.
// Full-census qualification remains separate and keeps its original deadline.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Signing deliberately uses the original whole-envelope Json oracle, not
// the borrowed decoder, streaming hash or source-data implementation.
func priorCarrierDecodeSignedWireTestV2(t *testing.T, cfg *ResolvedConfig, key *ecdsa.PrivateKey, runId string, payload []byte) (*ReleaseEvidenceEnvelope, []byte) {
	t.Helper()
	envelope := &ReleaseEvidenceEnvelope{
		Schema: releaseEvidenceSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
		ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid,
		Kind: campaignEvidenceFileKind, RunID: runId, Payload: payload,
		CreatedAt: `synthetic-,"payload":null,"signer":0,"data":"-created`,
		Signer:    crypto.PubkeyToAddress(key.PublicKey),
	}
	unsigned, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(unsigned)
	signature, err := crypto.Sign(digest[:], key)
	if err != nil {
		t.Fatal(err)
	}
	envelope.ContentHash = "sha256:" + hex.EncodeToString(digest[:])
	envelope.Signature = "0x" + hex.EncodeToString(signature)
	wire, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return envelope, wire
}

// The retained pre-fix reader is an independent compatibility oracle. It
// intentionally owns both full Decoder buffers and the final Marshal copy.
func priorCarrierDecodeLegacyVerifyTestV2(cfg *ResolvedConfig, runId, scope string, entry campaignEvidenceFileEntry, owner common.Address, wire []byte) error {
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		return err
	}
	limit, err := limits.fileEnvelopeBytes(entry.Path, entry.Size)
	if err != nil || len(wire) == 0 || uint64(len(wire)) > uint64(limit) {
		return errors.Join(errors.New("legacy prior source bound"), err)
	}
	var envelope ReleaseEvidenceEnvelope
	if err := decodeStrictJSONBytes(wire, &envelope); err != nil {
		return err
	}
	if err := validateReleaseEvidenceIdentity(&envelope); err != nil {
		return err
	}
	unsigned, err := evidenceUnsignedBytes(&envelope)
	if err != nil {
		return err
	}
	if err := verifyEvidenceSignature(&envelope, sha256.Sum256(unsigned), nil); err != nil {
		return err
	}
	if envelope.Signer != owner || envelope.ContentHash != entry.EnvelopeHash || envelope.Kind != campaignEvidenceFileKind || envelope.RunID != runId || envelope.DeploymentID != cfg.Config.Deployment.DeploymentID || envelope.ChainID != cfg.ChainID || envelope.Netuid != cfg.Netuid || !strings.EqualFold(envelope.GenesisHash, cfg.Public.Chain.GenesisHash) {
		return errors.New("legacy prior signed identity")
	}
	var payload campaignEvidenceFilePayload
	if err := decodeStrictJSONBytes(envelope.Payload, &payload); err != nil {
		return err
	}
	if payload.Schema != campaignEvidenceFileSchema || payload.RunID != runId || payload.Scope != scope || payload.Path != entry.Path || payload.ContentHash != entry.ContentHash || payload.Size != entry.Size || uint64(len(payload.Data)) != entry.Size || bytesSHA256(payload.Data) != entry.ContentHash {
		return errors.New("legacy prior exact source")
	}
	if err := validateCampaignMetadataRawV2(limits, entry.Path, payload.Data); err != nil {
		return err
	}
	canonical, err := json.Marshal(&envelope)
	if err != nil || !bytes.Equal(canonical, wire) {
		return errors.Join(errors.New("legacy prior canonical wire"), err)
	}
	return nil
}

// Every case enters the real production verifier and the old independent
// whole-value oracle. Neither path may mutate its caller-owned wire.
func assertPriorCarrierDecodeTestV2(t *testing.T, name string, cfg *ResolvedConfig, runId string, entry campaignEvidenceFileEntry, owner common.Address, wire []byte, accepted bool) {
	t.Helper()
	before := bytes.Clone(wire)
	legacyErr := priorCarrierDecodeLegacyVerifyTestV2(cfg, runId, "run", entry, owner, wire)
	actualErr := verifyFinalPriorCarrierWireV2(cfg, runId, "run", entry, owner, wire)
	if (legacyErr == nil) != accepted || (actualErr == nil) != accepted {
		t.Fatalf("%s accepted=%t legacy=%v actual=%v", name, accepted, legacyErr, actualErr)
	}
	if !bytes.Equal(before, wire) {
		t.Fatalf("%s changed borrowed wire", name)
	}
}

// Exact outer-byte custody is stricter than a valid signature. Whitespace,
// reordered/duplicate/escaped keys and truncation cannot acquire original
// custody even when their decoded signed object would otherwise match.
func TestFinalCaptureCapacityPriorCarrierDecodeV2RejectsNoncanonicalOuterWire(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "prior-decode-framing", Scope: "run", Path: campaignCollectedIndexPathV2, ContentHash: bytesSHA256([]byte{0}), Size: 1, Data: []byte{0}}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	envelope, wire := priorCarrierDecodeSignedWireTestV2(t, cfg, key, payload.RunID, body)
	entry := campaignEvidenceFileEntry{Path: payload.Path, ContentHash: payload.ContentHash, Size: payload.Size, EnvelopeHash: envelope.ContentHash}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatal(err)
	}
	reordered, err := json.Marshal(fields)
	if err != nil || bytes.Equal(reordered, wire) {
		t.Fatalf("outer field-order mutation: %v", err)
	}
	for _, item := range []struct {
		name string
		wire []byte
	}{
		{name: "leading-space", wire: append([]byte{' '}, wire...)},
		{name: "trailing-space", wire: append(bytes.Clone(wire), ' ')},
		{name: "trailing-value", wire: append(bytes.Clone(wire), []byte(" null")...)},
		{name: "truncated", wire: bytes.Clone(wire[:len(wire)-1])},
		{name: "reordered", wire: reordered},
		{name: "unknown", wire: append([]byte(`{"unknown":0,`), wire[1:]...)},
		{name: "duplicate-payload", wire: bytes.Replace(wire, []byte(`,"payload":`), []byte(`,"payload":null,"payload":`), 1)},
		{name: "escaped-payload-key", wire: bytes.Replace(wire, []byte(`,"payload":`), []byte(`,"pay\u006coad":`), 1)},
		{name: "case-payload-key", wire: bytes.Replace(wire, []byte(`,"payload":`), []byte(`,"Payload":`), 1)},
		{name: "payload-space", wire: bytes.Replace(wire, []byte(`,"payload":`), []byte(",\"payload\": \n"), 1)},
	} {
		assertPriorCarrierDecodeTestV2(t, item.name, cfg, payload.RunID, entry, envelope.Signer, item.wire, false)
	}
}

// The borrowed split is not a grammar or authentication cache. Invalid Json
// and the inherited depth limit are checked before any payload digest work.
func TestFinalCaptureCapacityPriorCarrierDecodeV2RejectsMalformedPayloadBeforeDigest(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"schema":"synthetic"}`)
	envelope, wire := priorCarrierDecodeSignedWireTestV2(t, cfg, key, "prior-decode-grammar", body)
	payloadStart := bytes.Index(wire, []byte(`,"payload":`)) + len(`,"payload":`)
	payloadEnd := bytes.LastIndex(wire, []byte(`,"signer":`))
	entry := campaignEvidenceFileEntry{Path: campaignCollectedIndexPathV2, ContentHash: bytesSHA256(nil), EnvelopeHash: envelope.ContentHash}
	for _, malformed := range [][]byte{
		[]byte(`{"data":"}`),
		[]byte(`{} {}`),
		[]byte(`{"data":[}`),
		[]byte(strings.Repeat("[", 10001) + "0" + strings.Repeat("]", 10001)),
	} {
		changed := append(bytes.Clone(wire[:payloadStart]), malformed...)
		changed = append(changed, wire[payloadEnd:]...)
		borrowed, err := decodeFinalPriorCarrierEnvelopeV2(changed)
		if err != nil {
			t.Fatalf("fixture does not reach the payload grammar boundary: %v", err)
		}
		calls := 0
		err = verifyEvidenceWithDigest(&borrowed, nil, func(value *ReleaseEvidenceEnvelope, raw []byte) ([sha256.Size]byte, error) {
			calls++
			return evidenceDigestFromValidatedPayload(value, raw)
		})
		if err == nil || calls != 0 {
			t.Fatalf("malformed payload reached digest work: calls=%d err=%v", calls, err)
		}
		assertPriorCarrierDecodeTestV2(t, "malformed-payload", cfg, envelope.RunID, entry, envelope.Signer, changed, false)
	}
}

// Every invocation authenticates its current borrowed bytes independently.
// Correctly signed other originals and unchanged payloads with bad signatures
// still fail the exact manifest binding; successful calls retain no authority.
func TestFinalCaptureCapacityPriorCarrierDecodeV2RevalidatesMutatedAndCrossEnvelopeWire(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "prior-decode-fresh", Scope: "run", Path: campaignCollectedIndexPathV2, ContentHash: bytesSHA256([]byte{0}), Size: 1, Data: []byte{0}}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	envelope, wire := priorCarrierDecodeSignedWireTestV2(t, cfg, key, payload.RunID, body)
	entry := campaignEvidenceFileEntry{Path: payload.Path, ContentHash: payload.ContentHash, Size: payload.Size, EnvelopeHash: envelope.ContentHash}
	assertPriorCarrierDecodeTestV2(t, "original", cfg, payload.RunID, entry, envelope.Signer, wire, true)
	original := bytes.Clone(wire)
	dataStart := bytes.Index(wire, []byte(`,"data":"`))
	if dataStart < 0 {
		t.Fatal("fixture has no source bytes")
	}
	wire[dataStart+len(`,"data":"`)] = 'B'
	assertPriorCarrierDecodeTestV2(t, "same-owner-mutated", cfg, payload.RunID, entry, envelope.Signer, wire, false)
	copy(wire, original)
	assertPriorCarrierDecodeTestV2(t, "restored-owner", cfg, payload.RunID, entry, envelope.Signer, wire, true)
	badSignature := *envelope
	badSignature.Signature = "0x" + strings.Repeat("0", 130)
	badWire, err := json.Marshal(&badSignature)
	if err != nil {
		t.Fatal(err)
	}
	assertPriorCarrierDecodeTestV2(t, "bad-signature", cfg, payload.RunID, entry, envelope.Signer, badWire, false)
	wrongOwner := envelope.Signer
	wrongOwner[0] ^= 1
	assertPriorCarrierDecodeTestV2(t, "wrong-owner", cfg, payload.RunID, entry, wrongOwner, wire, false)
	payload.Data = []byte{1}
	payload.ContentHash = bytesSHA256(payload.Data)
	otherBody, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	other, otherWire := priorCarrierDecodeSignedWireTestV2(t, cfg, key, payload.RunID, otherBody)
	assertPriorCarrierDecodeTestV2(t, "other-envelope", cfg, payload.RunID, entry, envelope.Signer, otherWire, false)
	entry.EnvelopeHash = other.ContentHash
	assertPriorCarrierDecodeTestV2(t, "other-source", cfg, payload.RunID, entry, envelope.Signer, otherWire, false)
}

// The discard-only path does not inherit another path's raw-byte allowance.
// Oversized hash-addressed intent controls keep the actual decoded schema
// check, not merely a valid signature and a matching content-addressed name.
func TestFinalCaptureCapacityPriorCarrierDecodeV2KeepsTypedLimitsAndIntentSchema(t *testing.T) {
	t.Parallel()
	cfg := campaignMetadataConfigTestV2(t)
	limits, err := campaignEvidenceLimitsForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	small := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "prior-decode-limit", Scope: "run", Path: campaignCollectedIndexPathV2, ContentHash: bytesSHA256(nil), Data: []byte{}}
	smallBody, err := json.Marshal(small)
	if err != nil {
		t.Fatal(err)
	}
	envelope, wire := priorCarrierDecodeSignedWireTestV2(t, cfg, key, small.RunID, smallBody)
	for _, name := range []string{"ordinary.bin", campaignCollectedIndexPathV2, campaignPriorIndexPathV2, campaignPriorManifestPathV2, campaignPriorCompletionPathV2, campaignDerivedPriorManifestPathV2, campaignDerivedPriorCompletionPathV2} {
		entry := campaignEvidenceFileEntry{Path: name, ContentHash: small.ContentHash, Size: limits.rawFileBytes(name) + 1, EnvelopeHash: envelope.ContentHash}
		assertPriorCarrierDecodeTestV2(t, name+"-one-over", cfg, small.RunID, entry, envelope.Signer, wire, false)
		if err := verifyFinalPriorCarrierWireV2(cfg, small.RunID, "run", entry, envelope.Signer, wire); err == nil || !strings.Contains(err.Error(), "signed source-size bound") {
			t.Fatalf("%s escaped its byte owner before typed identity checks: %v", name, err)
		}
	}
	for _, valid := range []bool{true, false} {
		prefix := []byte(fmt.Sprintf("{\"schema\":%q,\"current\":{},\"history\":[]}", validatorpkg.SteeringIntentSchema))
		if !valid {
			prefix = []byte("{}")
		}
		raw := append(prefix, bytes.Repeat([]byte{' '}, maximumCampaignEvidenceRawFileBytes+1-len(prefix))...)
		contentHash := bytesSHA256(raw)
		name := "final-inputs/validators/v2/" + strings.TrimPrefix(contentHash, "sha256:") + ".bin"
		payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "prior-decode-intent", Scope: "run", Path: name, ContentHash: contentHash, Size: uint64(len(raw)), Data: raw}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		envelope, wire := priorCarrierDecodeSignedWireTestV2(t, cfg, key, payload.RunID, body)
		entry := campaignEvidenceFileEntry{Path: name, ContentHash: contentHash, Size: payload.Size, EnvelopeHash: envelope.ContentHash}
		assertPriorCarrierDecodeTestV2(t, fmt.Sprintf("oversized-intent-%t", valid), cfg, payload.RunID, entry, envelope.Signer, wire, valid)
	}
}

// This serial measurement exercises the default real verifier, not a copied
// callback or elapsed-time threshold. The caller already owns the complete
// wire: decoding it must not allocate another payload-sized owner. Restoring
// either full Decoder in the production call necessarily exceeds this budget.
func TestFinalCaptureCapacityPriorCarrierDecodeV2KeepsBoundedVerificationAllocation(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	raw := bytes.Repeat([]byte{0, 127, 128, 255}, 512*1024)
	payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "prior-decode-allocation", Scope: "run", Path: campaignCollectedIndexPathV2, ContentHash: bytesSHA256(raw), Size: uint64(len(raw)), Data: raw}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	envelope, wire := priorCarrierDecodeSignedWireTestV2(t, cfg, key, payload.RunID, body)
	entry := campaignEvidenceFileEntry{Path: payload.Path, ContentHash: payload.ContentHash, Size: payload.Size, EnvelopeHash: envelope.ContentHash}
	assertPriorCarrierDecodeTestV2(t, "allocation-fixture", cfg, payload.RunID, entry, envelope.Signer, wire, true)
	before := bytesSHA256(wire)
	var verifyErr error
	result := testing.Benchmark(func(b *testing.B) {
		for index := 0; index < b.N; index++ {
			verifyErr = verifyFinalPriorCarrierWireV2(cfg, payload.RunID, "run", entry, envelope.Signer, wire)
			if verifyErr != nil {
				b.Fatal(verifyErr)
			}
		}
	})
	if verifyErr != nil || result.N <= 0 {
		t.Fatal("actual prior verification allocation measurement failed", verifyErr)
	}
	if maximum := int64(len(wire) / 4); result.AllocedBytesPerOp() >= maximum {
		t.Fatalf("prior verification allocated %d bytes, want below %d for already-owned %d-byte wire", result.AllocedBytesPerOp(), maximum, len(wire))
	}
	if bytesSHA256(wire) != before {
		t.Fatal("actual repeated verification mutated caller-owned wire")
	}
}

// Base64 padding and each side of the real decoding chunk boundaries preserve
// all original source bytes, including every possible byte value.
func TestFinalCaptureCapacityPriorCarrierDecodeV2PreservesExactSourceChunks(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 1, 2, 3, 767, 768, 769, 48*1024 - 1, 48 * 1024, 48*1024 + 1, 96*1024 + 1} {
		raw := make([]byte, size)
		for index := range raw {
			raw[index] = byte(index)
		}
		payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "prior-decode-chunks", Scope: "run", Path: campaignCollectedIndexPathV2, ContentHash: bytesSHA256(raw), Size: uint64(len(raw)), Data: raw}
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		envelope, wire := priorCarrierDecodeSignedWireTestV2(t, cfg, key, payload.RunID, encoded)
		entry := campaignEvidenceFileEntry{Path: payload.Path, ContentHash: payload.ContentHash, Size: payload.Size, EnvelopeHash: envelope.ContentHash}
		assertPriorCarrierDecodeTestV2(t, fmt.Sprint(size), cfg, payload.RunID, entry, envelope.Signer, wire, true)
	}
}

// RawMessage signatures preserve key spelling/order and duplicate-key rules.
// Escaped base64, byte arrays and non-zero unused padding bits were accepted
// by the old decoder and must not become accidental format migrations.
func TestFinalCaptureCapacityPriorCarrierDecodeV2KeepsHistoricalPayloadSpellings(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "prior-decode-spellings", Scope: "run", Path: campaignCollectedIndexPathV2, ContentHash: bytesSHA256([]byte{0}), Size: 1, Data: []byte{0}}
	base, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	dataStart := bytes.Index(base, []byte(`,"data":`))
	if dataStart < 0 {
		t.Fatal("legacy fixture has no data field")
	}
	for _, item := range []struct {
		name string
		body []byte
	}{
		{name: "canonical", body: base},
		{name: "escaped-key", body: bytes.Replace(base, []byte(`"data"`), []byte(`"da\u0074a"`), 1)},
		{name: "case-insensitive-key", body: bytes.Replace(base, []byte(`"data"`), []byte(`"DATA"`), 1)},
		{name: "escaped-base64", body: bytes.Replace(base, []byte(`"AA=="`), []byte(`"\u0041A=="`), 1)},
		{name: "escaped-newline", body: bytes.Replace(base, []byte(`"AA=="`), []byte(`"AA\u000a=="`), 1)},
		{name: "unused-padding-bits", body: bytes.Replace(base, []byte(`"AA=="`), []byte(`"AB=="`), 1)},
		{name: "byte-array", body: bytes.Replace(base, []byte(`"AA=="`), []byte(`[0]`), 1)},
		{name: "data-first", body: []byte(`{"data":"AA==",` + string(base[1:dataStart]) + `}`)},
		{name: "string-field-after-data", body: []byte(string(base[:len(base)-1]) + `,"scope":"run"}`)},
		{name: "duplicate-data-last-wins", body: []byte(string(base[:dataStart]) + `,"data":"AQ==","data":"AA=="}`)},
		{name: "duplicate-schema-last-wins", body: bytes.Replace(base, []byte(`"schema":`), []byte(`"schema":"discarded","schema":`), 1)},
	} {
		envelope, wire := priorCarrierDecodeSignedWireTestV2(t, cfg, key, payload.RunID, item.body)
		entry := campaignEvidenceFileEntry{Path: payload.Path, ContentHash: payload.ContentHash, Size: payload.Size, EnvelopeHash: envelope.ContentHash}
		assertPriorCarrierDecodeTestV2(t, item.name, cfg, payload.RunID, entry, envelope.Signer, wire, true)
	}
}

// Correct recomputed signatures cannot excuse schema/route/size/hash drift,
// malformed base64, unknown fields, or an earlier duplicate's type error.
func TestFinalCaptureCapacityPriorCarrierDecodeV2RejectsSignedSourceMutations(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "prior-decode-reject", Scope: "run", Path: campaignCollectedIndexPathV2, ContentHash: bytesSHA256([]byte{0}), Size: 1, Data: []byte{0}}
	base, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name string
		old  string
		new  string
	}{
		{name: "schema", old: campaignEvidenceFileSchema, new: "synthetic-wrong-schema"},
		{name: "run", old: payload.RunID, new: "synthetic-wrong-run"},
		{name: "scope", old: `"scope":"run"`, new: `"scope":"reference"`},
		{name: "path", old: payload.Path, new: "synthetic-other.bin"},
		{name: "hash", old: payload.ContentHash, new: bytesSHA256([]byte{1})},
		{name: "zero-size", old: `"size":1`, new: `"size":0`},
		{name: "larger-size", old: `"size":1`, new: `"size":2`},
		{name: "changed-data", old: `"AA=="`, new: `"AQ=="`},
		{name: "invalid-base64", old: `"AA=="`, new: `"@@=="`},
		{name: "truncated-base64", old: `"AA=="`, new: `"AA="`},
		{name: "negative-byte", old: `"AA=="`, new: `[-1]`},
		{name: "overflow-byte", old: `"AA=="`, new: `[256]`},
		{name: "null-byte", old: `"AA=="`, new: `null`},
		{name: "unknown-field", old: `"schema":`, new: `"unknown":0,"schema":`},
		{name: "duplicate-invalid-type", old: `"size":1`, new: `"size":"invalid","size":1`},
		{name: "duplicate-invalid-data-type", old: `"data":`, new: `"data":1,"data":`},
	} {
		if !bytes.Contains(base, []byte(item.old)) {
			t.Fatalf("%s has no causal mutation target", item.name)
		}
		body := bytes.Replace(base, []byte(item.old), []byte(item.new), 1)
		envelope, wire := priorCarrierDecodeSignedWireTestV2(t, cfg, key, payload.RunID, body)
		entry := campaignEvidenceFileEntry{Path: payload.Path, ContentHash: payload.ContentHash, Size: payload.Size, EnvelopeHash: envelope.ContentHash}
		assertPriorCarrierDecodeTestV2(t, item.name, cfg, payload.RunID, entry, envelope.Signer, wire, false)
	}
}

// Whole-string decoding rejects any data after padding, even at a streaming
// reader boundary. Each malformed tail remains signed Json and reaches the
// actual typed source decoder rather than failing the outer grammar check.
func TestFinalCaptureCapacityPriorCarrierDecodeV2RejectsPaddingAcrossChunkBoundaries(t *testing.T) {
	cfg := campaignMetadataConfigTestV2(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []int{1024, 64 * 1024} {
		raw := make([]byte, boundary/4*3)
		payload := campaignEvidenceFilePayload{Schema: campaignEvidenceFileSchema, RunID: "prior-decode-padding", Scope: "run", Path: campaignCollectedIndexPathV2, ContentHash: bytesSHA256(raw), Size: uint64(len(raw)), Data: raw}
		encoded := base64.StdEncoding.EncodeToString(raw)
		for _, item := range []struct {
			name         string
			data         string
			claimedBytes int
		}{
			{name: "padding-then-quantum", data: strings.Repeat("A", boundary-4) + "AA==AAAA", claimedBytes: len(raw) + 1},
			{name: "padding-then-padded-quantum", data: strings.Repeat("A", boundary-4) + "AA==AA==", claimedBytes: len(raw) - 1},
			{name: "malformed-tail", data: encoded + "!!!!", claimedBytes: len(raw)},
			{name: "incomplete-tail", data: encoded + "A", claimedBytes: len(raw)},
			{name: "one-over", data: encoded + "AA==", claimedBytes: len(raw)},
		} {
			// Match the exact concatenated bytes a broken per-chunk decoder
			// would yield, so size/hash checks cannot mask padding acceptance.
			payload.Size = uint64(item.claimedBytes)
			payload.ContentHash = bytesSHA256(make([]byte, item.claimedBytes))
			base, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			body := bytes.Replace(base, []byte(encoded), []byte(item.data), 1)
			envelope, wire := priorCarrierDecodeSignedWireTestV2(t, cfg, key, payload.RunID, body)
			entry := campaignEvidenceFileEntry{Path: payload.Path, ContentHash: payload.ContentHash, Size: payload.Size, EnvelopeHash: envelope.ContentHash}
			assertPriorCarrierDecodeTestV2(t, fmt.Sprintf("%d-%s", boundary, item.name), cfg, payload.RunID, entry, envelope.Signer, wire, false)
		}
	}
}
