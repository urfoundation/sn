package main

// Pins duplicated work at the full release fixture and public transcript
// boundaries. Every fixture retains the complete release census; the generic
// transcript probes exercise JSON normalization, not new on-chain admission.

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Retains only the immutable wire emitted by a complete production fixture
// seal. Each regression receives its own decoded graph and raw byte slices.
var finalTranscriptWorkFixtureCache struct {
	stateLock   sync.Mutex
	wire        []byte
	chainID     uint64
	genesisHash string
}

// Builds and seals the full shared release fixture once, then detaches each
// caller. No authentication result is shared by the verifier under test.
func finalTranscriptWorkFixture(t *testing.T) (*FinalPublicChainVerification, uint64, string) {
	t.Helper()
	var wire []byte
	var chainID uint64
	var genesisHash string
	func() {
		finalTranscriptWorkFixtureCache.stateLock.Lock()
		defer finalTranscriptWorkFixtureCache.stateLock.Unlock()
		if len(finalTranscriptWorkFixtureCache.wire) == 0 {
			source, _ := finalSemanticFixture(t)
			draft, err := BuildFinalSemanticEvidence(source)
			if err != nil {
				t.Fatal(err)
			}
			sealed, err := SealFinalSemanticEvidenceOnChain(context.Background(), draft, &finalTestChainReader{evidence: draft})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(sealed.PublicVerification)
			if err != nil || sealed.PublicVerification == nil || len(sealed.PublicVerification.Exchanges) == 0 {
				t.Fatalf("full public transcript fixture is unavailable: %v", err)
			}
			finalTranscriptWorkFixtureCache.wire = encoded
			finalTranscriptWorkFixtureCache.chainID = sealed.ChainID
			finalTranscriptWorkFixtureCache.genesisHash = sealed.GenesisHash
		}
		wire = finalTranscriptWorkFixtureCache.wire
		chainID = finalTranscriptWorkFixtureCache.chainID
		genesisHash = finalTranscriptWorkFixtureCache.genesisHash
	}()
	var verification FinalPublicChainVerification
	if err := json.Unmarshal(wire, &verification); err != nil {
		t.Fatal(err)
	}
	return &verification, chainID, genesisHash
}

// Reconstructs the pre-repair normalization/hash algorithm independently of
// the candidate verification entry point, including RawMessage's null rules.
func finalTranscriptLegacyNormalizedHash(t *testing.T, verification *FinalPublicChainVerification, chainID uint64, genesisHash string) string {
	t.Helper()
	wire, err := json.Marshal(verification)
	if err != nil {
		t.Fatal(err)
	}
	var normalized FinalPublicChainVerification
	if err := json.Unmarshal(wire, &normalized); err != nil {
		t.Fatal(err)
	}
	if err := finalizePublicChainVerification(&normalized, chainID, genesisHash); err != nil {
		t.Fatal(err)
	}
	return normalized.TranscriptHash
}

// Appends a generic JSON probe without removing any actual public exchange.
// Request/result hashes are those of the legacy normalized exchange, whereas
// the retained raw buffers deliberately preserve the caller's original bytes.
func finalTranscriptAppendRawProbe(t *testing.T, verification *FinalPublicChainVerification, params, result json.RawMessage) {
	t.Helper()
	exchange := FinalRPCExchange{
		Sequence: uint64(len(verification.Exchanges) + 1), Chain: "evm", Method: "normalization_probe",
		Params: bytes.Clone(params), PinnedHead: verification.Exchanges[0].PinnedHead, Result: bytes.Clone(result),
	}
	wire, err := json.Marshal(exchange)
	if err != nil {
		t.Fatal(err)
	}
	var normalized FinalRPCExchange
	if err := json.Unmarshal(wire, &normalized); err != nil {
		t.Fatal(err)
	}
	exchange.RequestHash, exchange.ResponseHash, err = finalRPCExchangeHashes(normalized)
	if err != nil {
		t.Fatal(err)
	}
	verification.Exchanges = append(verification.Exchanges, exchange)
}

// The detached JSON normalization already emits the complete unsigned wire;
// serializing the same full transcript again is redundant canonicalization.
func TestFinalPublicTranscriptSerializesUnsignedBodyOnce(t *testing.T) {
	t.Parallel()
	verification, chainID, genesisHash := finalTranscriptWorkFixture(t)
	marshalCount := 0
	err := verifyFinalPublicChainVerificationWithMarshal(verification, chainID, genesisHash, func(value any) ([]byte, error) {
		marshalCount++
		return json.Marshal(value)
	})
	if err != nil {
		t.Fatal(err)
	}
	if marshalCount != 1 {
		t.Fatalf("serialized the full public transcript %d times, want exactly 1 unsigned normalization", marshalCount)
	}
}

// The early deployment plan and its independent full reconstruction remain
// mandatory. Lifecycle attachment must not construct that same third plan.
func TestFinalSemanticFixtureBuildsOnlyIndependentCompletePlans(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	if source.ExpectedMiners != 1000 || source.ExpectedCandidates != 202 || source.ExpectedHeadSlots != 200 || source.ExpectedValidators != 2 || source.ExpectedOperators != 2 || source.FleetGeneration == nil || source.FleetLifecycle == nil || len(artifacts[source.PlanArtifact.URI]) == 0 {
		t.Fatal("plan work count lost the complete release fixture")
	}
	var planBuildCount int
	func() {
		finalSemanticFixtureCache.stateLock.Lock()
		defer finalSemanticFixtureCache.stateLock.Unlock()
		planBuildCount = finalSemanticFixtureCache.planBuildCount
	}()
	if planBuildCount != 2 {
		t.Fatalf("constructed the complete production fixture plan %d times, want exactly 2 independent builds", planBuildCount)
	}
}

// The full marshal/unmarshal semantics include HTML escaping, raw numeric
// spelling, duplicate object fields, Unicode and nil RawMessage as JSON null.
func TestFinalPublicTranscriptNormalizationPreservesRawMessages(t *testing.T) {
	t.Parallel()
	verification, chainID, genesisHash := finalTranscriptWorkFixture(t)
	// Typed strings normalize invalid UTF-8 differently from RawMessage. The
	// generic shape check does not interpret this independently bound hotkey.
	verification.NativePayouts[0].UIDs[0].Hotkey = "invalid-\xff"
	for _, probe := range []struct {
		params string
		result json.RawMessage
	}{
		{params: ` [ { "z": -0, "a": 1e+3, "a": 2, "large": 9007199254740993 } ] `, result: json.RawMessage(` { "last": 1.0, "first": 1e-7 } `)},
		{params: `["<>&", "\u2028\u2029", "\ud800"]`, result: json.RawMessage("\"<>&\u2028\u2029\" ")},
		{params: "[\"invalid-\xff\"]", result: json.RawMessage("\"invalid-\xfe\"")},
		{params: `[]`, result: nil},
		{params: `[null,true,false,0.000001,1e21]`, result: json.RawMessage(` null `)},
	} {
		finalTranscriptAppendRawProbe(t, verification, json.RawMessage(probe.params), probe.result)
	}
	verification.TranscriptHash = finalTranscriptLegacyNormalizedHash(t, verification, chainID, genesisHash)
	if err := verifyFinalPublicChainVerification(verification, chainID, genesisHash); err != nil {
		t.Fatalf("legacy-normalized complete transcript was rejected: %v", err)
	}
}

// Chronology intentionally matches canonical request bytes. Indented caller
// params must normalize before those exact oracle/range checks, not bypass them.
func TestFinalPublicTranscriptNormalizationPreservesChronologyParams(t *testing.T) {
	t.Parallel()
	verification, chainID, genesisHash := finalTranscriptWorkFixture(t)
	for index := range verification.Exchanges {
		var indented bytes.Buffer
		if err := json.Indent(&indented, verification.Exchanges[index].Params, " ", "  "); err != nil {
			t.Fatal(err)
		}
		verification.Exchanges[index].Params = append([]byte(" \n"), indented.Bytes()...)
	}
	if err := verifyFinalPublicChainVerification(verification, chainID, genesisHash); err != nil {
		t.Fatalf("indented complete chronology transcript was rejected: %v", err)
	}
}

// Exchange hashes normalize number values, but the signed transcript retains
// RawMessage numeric spelling. Reusing one hash for those different wires fails.
func TestFinalPublicTranscriptNormalizationPreservesNumericLexemes(t *testing.T) {
	t.Parallel()
	verification, chainID, genesisHash := finalTranscriptWorkFixture(t)
	finalTranscriptAppendRawProbe(t, verification, json.RawMessage(`[1]`), json.RawMessage(`1`))
	verification.TranscriptHash = finalTranscriptLegacyNormalizedHash(t, verification, chainID, genesisHash)
	if err := verifyFinalPublicChainVerification(verification, chainID, genesisHash); err != nil {
		t.Fatal(err)
	}
	probe := &verification.Exchanges[len(verification.Exchanges)-1]
	probe.Params = json.RawMessage(`[1.0]`)
	probe.Result = json.RawMessage(`1e0`)
	requestHash, responseHash, err := finalRPCExchangeHashes(*probe)
	if err != nil || requestHash != probe.RequestHash || responseHash != probe.ResponseHash {
		t.Fatalf("numeric probe did not retain equivalent exchange hashes: %v", err)
	}
	if err := verifyFinalPublicChainVerification(verification, chainID, genesisHash); err == nil || !strings.Contains(err.Error(), "reconstructed") {
		t.Fatalf("different raw numeric spelling reused the transcript hash: %v", err)
	}
	verification.TranscriptHash = finalTranscriptLegacyNormalizedHash(t, verification, chainID, genesisHash)
	if err := verifyFinalPublicChainVerification(verification, chainID, genesisHash); err != nil {
		t.Fatalf("correctly rebound numeric spelling was rejected: %v", err)
	}
}

// Success and rejection both leave exact raw buffers, nested observations and
// the original hash unchanged; canonical JSON equality alone would miss this.
func TestFinalPublicTranscriptVerificationDoesNotMutateInputs(t *testing.T) {
	t.Parallel()
	verification, chainID, genesisHash := finalTranscriptWorkFixture(t)
	for index := range verification.Exchanges {
		verification.Exchanges[index].Params = append([]byte(" \n"), verification.Exchanges[index].Params...)
		verification.Exchanges[index].Result = append([]byte(" \t"), verification.Exchanges[index].Result...)
	}
	for _, invalid := range []bool{false, true} {
		if invalid {
			verification.Exchanges[0].ResponseHash = finalTestHex(0x81)
		}
		beforeWire, err := json.Marshal(verification)
		if err != nil {
			t.Fatal(err)
		}
		beforeExchanges := append([]FinalRPCExchange(nil), verification.Exchanges...)
		for index := range beforeExchanges {
			beforeExchanges[index].Params = bytes.Clone(beforeExchanges[index].Params)
			beforeExchanges[index].Result = bytes.Clone(beforeExchanges[index].Result)
		}
		err = verifyFinalPublicChainVerification(verification, chainID, genesisHash)
		if (err != nil) != invalid {
			t.Fatalf("invalid=%t verification error=%v", invalid, err)
		}
		afterWire, marshalErr := json.Marshal(verification)
		if marshalErr != nil || !bytes.Equal(beforeWire, afterWire) || !reflect.DeepEqual(beforeExchanges, verification.Exchanges) {
			t.Fatalf("invalid=%t verification changed caller data: %v", invalid, marshalErr)
		}
	}
}

// Malformed, trailing and empty nonnil RawMessages remain errors. The params
// array check and floating-point range check are still mandatory after decode.
func TestFinalPublicTranscriptVerificationRejectsInvalidRawJSON(t *testing.T) {
	t.Parallel()
	verification, chainID, genesisHash := finalTranscriptWorkFixture(t)
	original := verification.Exchanges[0]
	for _, mutation := range []struct {
		name   string
		params json.RawMessage
		result json.RawMessage
		want   string
	}{
		{name: "empty", params: json.RawMessage{}, result: original.Result, want: "unexpected end"},
		{name: "trailing params", params: json.RawMessage(`[] {}`), result: original.Result, want: "invalid character"},
		{name: "trailing result", params: original.Params, result: json.RawMessage(`null true`), want: "invalid character"},
		{name: "null params", params: nil, result: original.Result, want: "params are not an array"},
		{name: "object params", params: json.RawMessage(`{}`), result: original.Result, want: "params are not an array"},
		{name: "overflow result", params: original.Params, result: json.RawMessage(`1e400`), want: "cannot unmarshal number"},
	} {
		verification.Exchanges[0] = original
		verification.Exchanges[0].Params = bytes.Clone(mutation.params)
		verification.Exchanges[0].Result = bytes.Clone(mutation.result)
		if err := verifyFinalPublicChainVerification(verification, chainID, genesisHash); err == nil || !strings.Contains(err.Error(), mutation.want) {
			t.Errorf("%s error=%v, want %q", mutation.name, err, mutation.want)
		}
	}
}

// A successful call cannot authorize changed bytes or suppress a later full
// check. Correctly rehashed generic probes remain admissible independently.
func TestFinalPublicTranscriptVerificationDoesNotReuseAcrossCalls(t *testing.T) {
	t.Parallel()
	verification, chainID, genesisHash := finalTranscriptWorkFixture(t)
	finalTranscriptAppendRawProbe(t, verification, json.RawMessage(`[]`), json.RawMessage(`1`))
	verification.TranscriptHash = finalTranscriptLegacyNormalizedHash(t, verification, chainID, genesisHash)
	if err := verifyFinalPublicChainVerification(verification, chainID, genesisHash); err != nil {
		t.Fatal(err)
	}
	probe := &verification.Exchanges[len(verification.Exchanges)-1]
	probe.Result[0] = '2'
	if err := verifyFinalPublicChainVerification(verification, chainID, genesisHash); err == nil || !strings.Contains(err.Error(), "request/result hash mismatch") {
		t.Fatalf("changed result reused a previous verification: %v", err)
	}
	var err error
	probe.RequestHash, probe.ResponseHash, err = finalRPCExchangeHashes(*probe)
	if err != nil {
		t.Fatal(err)
	}
	verification.TranscriptHash = finalTranscriptLegacyNormalizedHash(t, verification, chainID, genesisHash)
	if err := verifyFinalPublicChainVerification(verification, chainID, genesisHash); err != nil {
		t.Fatalf("independently rebound probe was rejected: %v", err)
	}
}
