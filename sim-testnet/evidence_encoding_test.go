// Canonical-wire and real-digest observers pin the encoding optimization to
// the original signed format. Inputs and mutation boundaries are per test.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urnetwork/server/v2026/startifact"
)

// Generate only synthetic signing material; tests never need wallet access.
func evidenceEncodingOwnerTest(t *testing.T) EVMRoleSecret {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return EVMRoleSecret{PrivateKeyHex: hex.EncodeToString(crypto.FromECDSA(key))}
}

// Full standard-library serialization remains the independent wire oracle.
func TestCampaignEvidenceEncodingMatchesOriginalCanonicalWire(t *testing.T) {
	t.Parallel()
	for _, payload := range []json.RawMessage{
		json.RawMessage(`null`),
		json.RawMessage(` { "value" : "<>&\u2028\u2029", "number" : 1e+02 } `),
		json.RawMessage(`{"duplicate":1,"duplicate":2,"array":[true,false,null]}`),
		json.RawMessage("{\"value\":\"\u2028\u2029\"}"),
		json.RawMessage(`"quoted \" payload \\ \b \f \n \r \t"`),
		json.RawMessage(`{"bytes":"` + strings.Repeat("YWJj", 1024) + `"}`),
	} {
		for _, runId := range []string{"", "synthetic-run"} {
			envelope := &ReleaseEvidenceEnvelope{Schema: releaseEvidenceSchema, DeploymentID: "synthetic-<>&-deployment", ChainID: ^uint64(0), GenesisHash: "0x" + strings.Repeat("1", 64), Netuid: ^uint16(0), Kind: "synthetic-kind", RunID: runId, CreatedAt: "synthetic-\"payload\":null-time", Payload: payload, Signer: common.HexToAddress("0x" + strings.Repeat("2", 40)), ContentHash: "sha256:" + strings.Repeat("3", 64), Signature: "0x" + strings.Repeat("4", 130)}
			want, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			got, err := marshalEvidenceEnvelope(envelope)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("canonical wire differs for run %q and payload %q: %v", runId, payload, err)
			}
			canonical, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			unsigned := *envelope
			unsigned.ContentHash, unsigned.Signature = "", ""
			legacy, err := json.Marshal(&unsigned)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := evidenceDigestFromCanonicalPayload(envelope, canonical)
			if err != nil || digest != sha256.Sum256(legacy) {
				t.Fatalf("segmented digest changed original signed bytes: %v", err)
			}
		}
	}
}

// Count the real digest calls routed through this invocation's observer.
// An instrumented self-verification would count twice; the separate allocation
// control also catches a restored default verification outside this observer.
func TestCampaignEvidenceEncodingSigningHashesOwnedPayloadOnce(t *testing.T) {
	cfg := testResolvedConfig(t)
	owner := evidenceEncodingOwnerTest(t)
	owned, err := marshalEvidencePayload(cfg, "unit", map[string]string{"data": strings.Repeat("synthetic-<>&", 4096)})
	if err != nil {
		t.Fatal(err)
	}
	hashes := 0
	envelope, err := signEvidenceOwnedPayloadWithDigest(cfg, "unit", "encoding-owner", owned, owner, func(value *ReleaseEvidenceEnvelope, payload []byte) ([sha256.Size]byte, error) {
		hashes++
		if !bytes.Equal(payload, owned.encoded) {
			t.Fatal("signer hashed different payload bytes")
		}
		return evidenceDigestFromCanonicalPayload(value, payload)
	})
	if err != nil || hashes != 1 {
		t.Fatalf("actual owned signing performed %d payload hashes: %v", hashes, err)
	}
	legacy := startifact.EvidenceEnvelope(*envelope)
	if err := startifact.VerifyEvidence(&legacy); err != nil {
		t.Fatal("original independent server verifier refused new signature", err)
	}
}

// Authentication is never memoized across calls or backing-slice mutation.
func TestCampaignEvidenceEncodingUntrustedReadsRehashEveryInvocation(t *testing.T) {
	cfg := testResolvedConfig(t)
	envelope, err := signEvidence(cfg, "unit", "encoding-mutable", map[string]int{"value": 1}, evidenceEncodingOwnerTest(t))
	if err != nil {
		t.Fatal(err)
	}
	hashes := 0
	digest := func(value *ReleaseEvidenceEnvelope, payload []byte) ([sha256.Size]byte, error) {
		hashes++
		return evidenceDigestFromValidatedPayload(value, payload)
	}
	for index := 1; index <= 2; index++ {
		if err := verifyEvidenceWithDigest(envelope, nil, digest); err != nil || hashes != index {
			t.Fatalf("external read borrowed prior authentication: hashes=%d error=%v", hashes, err)
		}
	}
	envelope.Payload[bytes.IndexByte(envelope.Payload, '1')] = '2'
	if err := verifyEvidenceWithDigest(envelope, nil, digest); err == nil || hashes != 3 {
		t.Fatalf("changed backing payload reused a signed digest: hashes=%d error=%v", hashes, err)
	}
}

// Invalid Json must fail before hashing, including adjacent trailing values.
func TestCampaignEvidenceEncodingRejectsMalformedMutablePayload(t *testing.T) {
	cfg := testResolvedConfig(t)
	envelope, err := signEvidence(cfg, "unit", "encoding-malformed", nil, evidenceEncodingOwnerTest(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []json.RawMessage{nil, {}, []byte("{"), []byte("null null"), []byte(`{"data":NaN}`)} {
		changed := *envelope
		changed.Payload = payload
		hashes := 0
		err := verifyEvidenceWithDigest(&changed, nil, func(value *ReleaseEvidenceEnvelope, raw []byte) ([sha256.Size]byte, error) {
			hashes++
			return evidenceDigestFromCanonicalPayload(value, raw)
		})
		if err == nil || hashes != 0 {
			t.Fatalf("invalid mutable input reached hashing: %q count=%d error=%v", payload, hashes, err)
		}
		if len(payload) != 0 {
			if _, err := marshalEvidenceEnvelope(&changed); err == nil {
				t.Fatalf("invalid mutable input reached wire encoding: %q", payload)
			}
		}
	}
}

// A large payload must not become an unsigned-envelope-sized framing buffer.
// This measures the exact byte owner, not a timing or heap-size prediction.
func TestCampaignEvidenceEncodingFramingExcludesOwnedPayload(t *testing.T) {
	t.Parallel()
	envelope := &ReleaseEvidenceEnvelope{Schema: releaseEvidenceSchema, DeploymentID: "synthetic", ChainID: 1, Netuid: 1, Kind: "unit", Payload: json.RawMessage(`"` + strings.Repeat("x", 4*1024*1024) + `"`)}
	prefix, suffix, err := evidenceWireFraming(envelope)
	if err != nil || len(prefix)+len(suffix) >= 1024 || !bytes.HasSuffix(prefix, []byte(`"payload":`)) || !bytes.HasPrefix(suffix, []byte(`,"signer":`)) {
		t.Fatalf("framing retained the complete owned payload: prefix=%d suffix=%d error=%v", len(prefix), len(suffix), err)
	}
	legacy, err := evidenceUnsignedBytes(envelope)
	if err != nil || len(legacy) <= len(envelope.Payload) {
		t.Fatal("original unsigned allocation no longer reproduces the redundant payload copy", err)
	}
}

// A real hashing failure cannot produce a partially signed result or be
// swallowed by the one-pass signing path.
func TestCampaignEvidenceEncodingPreservesDigestAndOwnerFailures(t *testing.T) {
	cfg := testResolvedConfig(t)
	owner := evidenceEncodingOwnerTest(t)
	owned, err := marshalEvidencePayload(cfg, "unit", map[string]int{"value": 1})
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("synthetic digest refusal")
	envelope, err := signEvidenceOwnedPayloadWithDigest(cfg, "unit", "encoding-failure", owned, owner, func(*ReleaseEvidenceEnvelope, []byte) ([sha256.Size]byte, error) {
		return [sha256.Size]byte{}, refused
	})
	if envelope != nil || !errors.Is(err, refused) {
		t.Fatal("digest refusal produced signing authority", err)
	}
	if _, err := signEvidenceOwnedPayload(cfg, "unit", "encoding-failure", nil, owner); err == nil {
		t.Fatal("missing canonical owner reached signing")
	}
	if _, err := marshalEvidencePayload(nil, "unit", nil); err == nil {
		t.Fatal("missing deployment reached canonical admission")
	}
	if _, err := marshalEvidencePayload(cfg, "unit", json.RawMessage(`{"invalid":`)); err == nil {
		t.Fatal("invalid source Json obtained an owned canonical payload")
	}
}

// Full signing must not allocate another payload-sized buffer after its input
// is already owned. This serial test uses allocation volume, not elapsed time;
// the default verification wrapper's canonical copy is observable without any
// digest hook. Key, signature and metadata allocations have a wide allowance.
func TestCampaignEvidenceEncodingSigningKeepsAllocationBelowPayload(t *testing.T) {
	cfg := testResolvedConfig(t)
	owner := evidenceEncodingOwnerTest(t)
	owned, err := marshalEvidencePayload(cfg, "unit", map[string]string{"data": strings.Repeat("x", 1024*1024)})
	if err != nil {
		t.Fatal(err)
	}
	var envelope *ReleaseEvidenceEnvelope
	var signErr error
	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			envelope, signErr = signEvidenceOwnedPayload(cfg, "unit", "encoding-allocation", owned, owner)
			if signErr != nil {
				b.Fatal(signErr)
			}
		}
	})
	if signErr != nil || envelope == nil || result.N == 0 {
		t.Fatalf("actual signing did not produce a measured envelope: %v", signErr)
	}
	maximumBytes := int64(len(owned.encoded)) / 4
	allocatedBytes := result.AllocedBytesPerOp()
	if allocatedBytes <= 0 || allocatedBytes >= maximumBytes {
		t.Fatalf("actual signing allocated a payload-sized canonical copy: allocated=%d maximum=%d payload=%d", allocatedBytes, maximumBytes, len(owned.encoded))
	}
	legacy := startifact.EvidenceEnvelope(*envelope)
	if err := startifact.VerifyEvidence(&legacy); err != nil {
		t.Fatal("independent server verifier refused measured signing output", err)
	}
}
