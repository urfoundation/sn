// These oracles retain the original RawMessage encoder and full-envelope
// signer. The streaming reader must preserve their exact bytes and authority.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urnetwork/server/v2026/startifact"
)

// The downstream observer owns its copy, never the streaming writer's buffer.
type evidenceCanonicalStreamWriterTest struct {
	bytes.Buffer
	calls             int
	maximumWriteBytes int
	failCall          int
	shortCall         int
	failure           error
}

func (self *evidenceCanonicalStreamWriterTest) Write(value []byte) (int, error) {
	self.calls++
	self.maximumWriteBytes = max(self.maximumWriteBytes, len(value))
	if self.calls == self.failCall {
		return 0, self.failure
	}
	if self.calls == self.shortCall {
		return len(value) - 1, nil
	}
	return self.Buffer.Write(value)
}

// This whole-envelope oracle does not call either production digest helper or
// the optimized signer. All keys and identities are generated synthetic data.
func evidenceCanonicalStreamSignedTest(t *testing.T, payload json.RawMessage) (*ReleaseEvidenceEnvelope, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	envelope := &ReleaseEvidenceEnvelope{
		Schema: releaseEvidenceSchema, DeploymentID: "synthetic-stream-deployment",
		ChainID: 1337, GenesisHash: "0x" + strings.Repeat("1", 64), Netuid: 7,
		Kind: "synthetic-stream", RunID: "synthetic-stream-run", CreatedAt: `synthetic-<>&-"payload":null`,
		Payload: bytes.Clone(payload), Signer: crypto.PubkeyToAddress(key.PublicKey),
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
	return envelope, key
}

// Number spelling, duplicate fields and existing escape spelling are signed
// bytes. Invalid Utf8 within an otherwise valid quoted RawMessage stays raw.
func evidenceCanonicalStreamPayloadCasesTest() []json.RawMessage {
	return []json.RawMessage{
		json.RawMessage(`null`),
		json.RawMessage(" \t [true, false, null, -0, 1e+02, 1E-0002, 0.1000, -12.3400, 1e999999] \r\n"),
		json.RawMessage(` { "same": 1, "same": 2, "\u0061": 3, "a": 4 } `),
		json.RawMessage(`{"value":"\uD800\uDC00\uDFFF\uD800\u003C\u003e\/\\\""}`),
		json.RawMessage(`{"value":"before \" <>& after \\ \u003C \/ \b \f \n \r \t","spaces":" \t "}`),
		json.RawMessage("{\"<>&\u2028\u2029\":\"<>&\u2028\u2029\"}"),
		json.RawMessage{'"', 0xff, 0xc0, 0xaf, 0xe2, 0x80, 'x', '"'},
	}
}

func assertEvidenceCanonicalStreamOracleTest(t *testing.T, payload json.RawMessage) {
	t.Helper()
	want, err := json.Marshal(payload)
	if err != nil || !json.Valid(payload) {
		t.Fatalf("stream fixture is not an admitted RawMessage: %v", err)
	}
	before := bytes.Clone(payload)
	writer := &evidenceCanonicalStreamWriterTest{}
	if err := writeValidatedEvidencePayload(writer, payload); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(writer.Bytes(), want) {
		t.Fatalf("stream changed original RawMessage bytes: payload=%d canonical=%d got=%d", len(payload), len(want), writer.Len())
	}
	if !bytes.Equal(payload, before) {
		t.Fatal("stream changed borrowed input bytes")
	}
	const maximumChunkBytes = 64 * 1024
	maximumCalls := (len(want) + maximumChunkBytes - 1) / maximumChunkBytes
	if writer.calls == 0 || writer.calls > maximumCalls || writer.maximumWriteBytes > maximumChunkBytes {
		t.Fatalf("stream flushed per escape or wrote an unbounded span: calls=%d maximum=%d largest=%d", writer.calls, maximumCalls, writer.maximumWriteBytes)
	}
}

func TestCampaignEvidenceCanonicalStreamMatchesRawMessageByteOracle(t *testing.T) {
	t.Parallel()
	for _, payload := range evidenceCanonicalStreamPayloadCasesTest() {
		assertEvidenceCanonicalStreamOracleTest(t, payload)
	}
}

// Cover every literal byte in a quoted RawMessage, including invalid Utf8,
// and every encoder-produced escaped form. Invalid grammar cannot reach hash.
func TestCampaignEvidenceCanonicalStreamMatchesAllStringByteVariants(t *testing.T) {
	t.Parallel()
	envelope, _ := evidenceCanonicalStreamSignedTest(t, json.RawMessage(`null`))
	for value := 0; value < 256; value++ {
		payload := json.RawMessage{'"', byte(value), '"'}
		if _, err := json.Marshal(payload); err == nil {
			assertEvidenceCanonicalStreamOracleTest(t, payload)
		} else {
			changed := *envelope
			changed.Payload = payload
			hashes := 0
			err := verifyEvidenceWithDigest(&changed, nil, func(value *ReleaseEvidenceEnvelope, raw []byte) ([sha256.Size]byte, error) {
				hashes++
				return evidenceDigestFromValidatedPayload(value, raw)
			})
			if json.Valid(payload) || err == nil || hashes != 0 {
				t.Fatalf("literal byte %d bypassed whole-value grammar admission: hashes=%d error=%v", value, hashes, err)
			}
		}
		escaped, err := json.Marshal(string([]byte{byte(value)}))
		if err != nil {
			t.Fatal(err)
		}
		assertEvidenceCanonicalStreamOracleTest(t, json.RawMessage(escaped))
	}
}

// Test state transitions and multi-byte escapes on both sides of each output
// boundary. Dense escaping must still use one downstream write per chunk.
func TestCampaignEvidenceCanonicalStreamBoundsWritesAcrossChunkEdges(t *testing.T) {
	t.Parallel()
	const chunkBytes = 64 * 1024
	for offset := chunkBytes - 8; offset <= chunkBytes+8; offset++ {
		for _, edge := range []string{"<>&\u2028\u2029", `\\\"<>\u003C\/`, "\u2028", `\t\n`} {
			payload := json.RawMessage(`{"p":"` + strings.Repeat("x", offset-len(`{"p":"`)) + edge + strings.Repeat("z", chunkBytes+3) + `","q": [1, 2, 3] }`)
			assertEvidenceCanonicalStreamOracleTest(t, payload)
		}
	}
	assertEvidenceCanonicalStreamOracleTest(t, json.RawMessage(`"`+strings.Repeat("<>&\u2028\u2029", 32*1024)+`"`))
}

// Actual default verification must accept independent original signatures,
// then reject fresh mutations and another envelope's authority without cache.
func TestCampaignEvidenceCanonicalStreamPreservesIndependentSignaturesAndMutableInputs(t *testing.T) {
	t.Parallel()
	for _, payload := range evidenceCanonicalStreamPayloadCasesTest() {
		envelope, key := evidenceCanonicalStreamSignedTest(t, payload)
		before := *envelope
		before.Payload = bytes.Clone(envelope.Payload)
		for range 2 {
			if err := verifyEvidence(envelope, &key.PublicKey); err != nil {
				t.Fatal("default verifier refused original full-envelope signature", err)
			}
		}
		legacy := startifact.EvidenceEnvelope(*envelope)
		if err := startifact.VerifyEvidence(&legacy); err != nil {
			t.Fatal("independent server rejected the original signer oracle", err)
		}
		if !reflect.DeepEqual(envelope, &before) {
			t.Fatal("default verification changed mutable envelope or payload")
		}
	}
	envelope, key := evidenceCanonicalStreamSignedTest(t, json.RawMessage(`{"value":1}`))
	other, otherKey := evidenceCanonicalStreamSignedTest(t, json.RawMessage(`{"value":2}`))
	for _, mutation := range []struct {
		name   string
		change func(*ReleaseEvidenceEnvelope)
	}{
		{name: "payload backing bytes", change: func(value *ReleaseEvidenceEnvelope) { value.Payload[bytes.IndexByte(value.Payload, '1')] = '2' }},
		{name: "cross-envelope payload and hash", change: func(value *ReleaseEvidenceEnvelope) {
			value.Payload, value.ContentHash = bytes.Clone(other.Payload), other.ContentHash
		}},
		{name: "independent bad signature", change: func(value *ReleaseEvidenceEnvelope) {
			value.Signature = value.Signature[:len(value.Signature)-2] + "ff"
		}},
		{name: "signature from another envelope", change: func(value *ReleaseEvidenceEnvelope) { value.Signature = other.Signature }},
		{name: "content hash", change: func(value *ReleaseEvidenceEnvelope) { value.ContentHash = "sha256:" + strings.Repeat("0", 64) }},
		{name: "signer", change: func(value *ReleaseEvidenceEnvelope) { value.Signer = other.Signer }},
		{name: "schema", change: func(value *ReleaseEvidenceEnvelope) { value.Schema += "-changed" }},
		{name: "deployment", change: func(value *ReleaseEvidenceEnvelope) { value.DeploymentID += "-changed" }},
		{name: "chain", change: func(value *ReleaseEvidenceEnvelope) { value.ChainID++ }},
		{name: "genesis", change: func(value *ReleaseEvidenceEnvelope) { value.GenesisHash += "0" }},
		{name: "netuid", change: func(value *ReleaseEvidenceEnvelope) { value.Netuid++ }},
		{name: "kind", change: func(value *ReleaseEvidenceEnvelope) { value.Kind += "-changed" }},
		{name: "run", change: func(value *ReleaseEvidenceEnvelope) { value.RunID += "-changed" }},
		{name: "creation", change: func(value *ReleaseEvidenceEnvelope) { value.CreatedAt += "0" }},
	} {
		changed := *envelope
		changed.Payload = bytes.Clone(envelope.Payload)
		mutation.change(&changed)
		if err := verifyEvidence(&changed, &key.PublicKey); err == nil {
			t.Fatalf("default verifier accepted fresh %s mutation", mutation.name)
		}
		if err := verifyEvidence(envelope, &key.PublicKey); err != nil {
			t.Fatal("rejected mutation contaminated independent original", err)
		}
	}
	position := bytes.IndexByte(envelope.Payload, '1')
	envelope.Payload[position] = '2'
	if err := verifyEvidence(envelope, &key.PublicKey); err == nil {
		t.Fatal("default verifier reused authority for the same mutated backing owner")
	}
	envelope.Payload[position] = '1'
	if err := verifyEvidence(envelope, &key.PublicKey); err != nil {
		t.Fatal("restored exact original failed fresh independent verification", err)
	}
	if err := verifyEvidence(envelope, &otherKey.PublicKey); err == nil {
		t.Fatal("default verifier ignored independently required signer")
	}
}

// Keep the standard grammar boundary, including the maximum nesting depth;
// malformed and over-depth input must fail before even the real digest hook.
func TestCampaignEvidenceCanonicalStreamRejectsInvalidGrammarBeforeDigest(t *testing.T) {
	t.Parallel()
	envelope, _ := evidenceCanonicalStreamSignedTest(t, json.RawMessage(`null`))
	invalid := []json.RawMessage{
		nil, {}, []byte(`{`), []byte(`null null`), []byte(`{"a":NaN}`),
		[]byte(`[1,]`), []byte(`{"a":1,}`), []byte(`01`), []byte(`+1`),
		[]byte(`"\x20"`), []byte(`"\uZZZZ"`), []byte{'"', '\n', '"'},
		append([]byte{0xef, 0xbb, 0xbf}, []byte(`null`)...),
		[]byte(strings.Repeat("[", 10001) + "0" + strings.Repeat("]", 10001)),
	}
	for index, payload := range invalid {
		changed := *envelope
		changed.Payload = payload
		hashes := 0
		err := verifyEvidenceWithDigest(&changed, nil, func(value *ReleaseEvidenceEnvelope, raw []byte) ([sha256.Size]byte, error) {
			hashes++
			return evidenceDigestFromValidatedPayload(value, raw)
		})
		if json.Valid(payload) || err == nil || hashes != 0 {
			t.Fatalf("invalid grammar case %d reached digest: hashes=%d error=%v", index, hashes, err)
		}
	}
	boundary, key := evidenceCanonicalStreamSignedTest(t, json.RawMessage(strings.Repeat("[", 10000)+"0"+strings.Repeat("]", 10000)))
	if err := verifyEvidence(boundary, &key.PublicKey); err != nil {
		t.Fatal("standard maximum valid nesting depth was reduced", err)
	}
}

func TestCampaignEvidenceCanonicalStreamPropagatesWriterFailures(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`"` + strings.Repeat("x", 3*64*1024) + `"`)
	refused := errors.New("synthetic canonical writer refusal")
	for _, failedCall := range []int{1, 2, 4} {
		writer := &evidenceCanonicalStreamWriterTest{failCall: failedCall, failure: refused}
		if err := writeValidatedEvidencePayload(writer, payload); !errors.Is(err, refused) || writer.calls != failedCall {
			t.Fatalf("canonical writer swallowed downstream refusal: call=%d got=%d error=%v", failedCall, writer.calls, err)
		}
	}
	writer := &evidenceCanonicalStreamWriterTest{shortCall: 1}
	if err := writeValidatedEvidencePayload(writer, payload); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("canonical writer accepted an incomplete downstream write", err)
	}
}

// Canonically equivalent signed objects are not equivalent original wires.
// This includes harmless outer whitespace, field order and duplicate fields.
func TestCampaignEvidenceCanonicalStreamRequiresExactCanonicalWire(t *testing.T) {
	t.Parallel()
	envelope, key := evidenceCanonicalStreamSignedTest(t, json.RawMessage(` { "value" : 1, "text" : "<>&" } `))
	if err := verifyEvidence(envelope, &key.PublicKey); err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(wire)
	if err := verifyValidatedEvidenceWire(envelope, wire); err != nil {
		t.Fatal("comparator refused original standard-library wire", err)
	}
	schema, err := json.Marshal(envelope.Schema)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := json.Marshal(envelope.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	schemaField := `"schema":` + string(schema)
	deploymentField := `"deployment_id":` + string(deployment)
	originalOrder := []byte("{" + schemaField + "," + deploymentField + ",")
	reversedOrder := []byte("{" + deploymentField + "," + schemaField + ",")
	for _, alternate := range [][]byte{
		append([]byte(" "), wire...),
		append(bytes.Clone(wire), '\n'),
		bytes.Replace(wire, originalOrder, reversedOrder, 1),
		append([]byte("{"+schemaField+","), wire[1:]...),
		bytes.Replace(wire, []byte(`"payload":{`), []byte(`"payload": { `), 1),
		bytes.Replace(wire, []byte(`\u003c`), []byte(`<`), 1),
	} {
		if bytes.Equal(alternate, wire) {
			t.Fatal("noncanonical wire fixture did not change its intended bytes")
		}
		var decoded ReleaseEvidenceEnvelope
		if err := json.Unmarshal(alternate, &decoded); err != nil {
			t.Fatal("equivalent wire fixture lost valid Json", err)
		}
		if err := verifyEvidence(&decoded, &key.PublicKey); err != nil {
			t.Fatal("equivalent wire fixture lost its original signature", err)
		}
		alternateBefore := bytes.Clone(alternate)
		if err := verifyValidatedEvidenceWire(&decoded, alternate); err == nil {
			t.Fatal("canonical comparator accepted equivalent but different original wire")
		}
		if !bytes.Equal(alternate, alternateBefore) {
			t.Fatal("canonical comparator changed borrowed wire")
		}
	}
	for _, alternate := range [][]byte{
		nil, wire[:1], wire[:len(wire)/2], wire[:len(wire)-1],
		append(bytes.Clone(wire), 'x'),
		bytes.Replace(wire, []byte(`"value":1`), []byte(`"value":2`), 1),
	} {
		if err := verifyValidatedEvidenceWire(envelope, alternate); err == nil {
			t.Fatal("canonical comparator accepted truncated, extended or changed wire")
		}
	}
	if !bytes.Equal(wire, before) {
		t.Fatal("canonical comparator changed the original immutable wire")
	}
}

func evidenceCanonicalStreamLargePayloadsTest() []json.RawMessage {
	const payloadBytes = 1024 * 1024
	const dense = "<>&\u2028\u2029"
	return []json.RawMessage{
		json.RawMessage(`"` + strings.Repeat("x", payloadBytes) + `"`),
		json.RawMessage(`"` + strings.Repeat(dense, payloadBytes/len(dense)+1) + `"`),
	}
}

// Serial allocation volume measures the actual default reader, with no hook
// that a restored wrapper could bypass. A payload-sized Marshal must fail.
func TestCampaignEvidenceCanonicalStreamVerificationKeepsAllocationBelowPayload(t *testing.T) {
	for index, payload := range evidenceCanonicalStreamLargePayloadsTest() {
		envelope, key := evidenceCanonicalStreamSignedTest(t, payload)
		var verifyErr error
		result := testing.Benchmark(func(b *testing.B) {
			for range b.N {
				verifyErr = verifyEvidence(envelope, &key.PublicKey)
				if verifyErr != nil {
					b.Fatal(verifyErr)
				}
			}
		})
		if verifyErr != nil || result.N == 0 {
			t.Fatalf("actual default verifier did not complete measured case %d: %v", index, verifyErr)
		}
		maximumBytes := int64(len(payload)) / 4
		allocatedBytes := result.AllocedBytesPerOp()
		if allocatedBytes <= 0 || allocatedBytes >= maximumBytes {
			t.Fatalf("actual default verification allocated a payload-sized canonical copy: case=%d allocated=%d maximum=%d payload=%d", index, allocatedBytes, maximumBytes, len(payload))
		}
	}
}

// Prior readback compares the existing original wire without allocating a
// second envelope-sized canonical copy. Signature admission remains separate.
func TestCampaignEvidenceCanonicalStreamWireComparisonKeepsAllocationBelowPayload(t *testing.T) {
	for index, payload := range evidenceCanonicalStreamLargePayloadsTest() {
		envelope, key := evidenceCanonicalStreamSignedTest(t, payload)
		if err := verifyEvidence(envelope, &key.PublicKey); err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		var compareErr error
		result := testing.Benchmark(func(b *testing.B) {
			for range b.N {
				compareErr = verifyValidatedEvidenceWire(envelope, wire)
				if compareErr != nil {
					b.Fatal(compareErr)
				}
			}
		})
		if compareErr != nil || result.N == 0 {
			t.Fatalf("actual wire comparator did not complete measured case %d: %v", index, compareErr)
		}
		maximumBytes := int64(len(payload)) / 4
		allocatedBytes := result.AllocedBytesPerOp()
		if allocatedBytes <= 0 || allocatedBytes >= maximumBytes {
			t.Fatalf("actual wire comparison allocated a payload-sized canonical copy: case=%d allocated=%d maximum=%d payload=%d", index, allocatedBytes, maximumBytes, len(payload))
		}
	}
}
