package validator

// Deterministic admission observations count actual lowercase execution rather
// than time or allocation estimates. Independent old parsers retain exact bytes.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urnetwork/connect"
)

// Counts only normalization of a shape that the fixed-width parser must refuse.
func canonicalHexOversizeWork(counter *int, width int) canonicalHexWork {
	return canonicalHexWork{normalized: func(length int) {
		if length > width {
			(*counter)++
		}
	}}
}

// Exact old code is an independent byte/error oracle, not a production alias.
func oldCanonicalHex32(name, encoded string, zeroAllowed bool) ([32]byte, error) {
	var value [32]byte
	if encoded != strings.ToLower(encoded) || len(encoded) != 66 || !strings.HasPrefix(encoded, "0x") {
		return value, fmt.Errorf("%s is not canonical 32-byte hex", name)
	}
	decoded, err := hex.DecodeString(encoded[2:])
	if err != nil || len(decoded) != 32 {
		return value, fmt.Errorf("%s is not 32-byte hex", name)
	}
	copy(value[:], decoded)
	if !zeroAllowed && value == ([32]byte{}) {
		return value, fmt.Errorf("%s is zero", name)
	}
	return value, nil
}

// Exact old content parser intentionally accepts the all-zero digest.
func oldCanonicalContentHash(encoded string) ([32]byte, error) {
	var value [32]byte
	if encoded != strings.ToLower(encoded) || len(encoded) != 71 || !strings.HasPrefix(encoded, "sha256:") {
		return value, errors.New("content hash is not canonical SHA-256")
	}
	decoded, err := hex.DecodeString(encoded[7:])
	if err != nil || len(decoded) != 32 {
		return value, errors.New("content hash is invalid")
	}
	copy(value[:], decoded)
	return value, nil
}

// Exact old signature parser returns decoded bytes only after shape admission.
func oldCanonicalEnvelopeSignature(encoded string) ([]byte, error) {
	if encoded != strings.ToLower(encoded) || len(encoded) != 130 || !strings.HasPrefix(encoded, "0x") {
		return nil, errors.New("release measurement envelope signature is not canonical 64-byte hex")
	}
	signature, err := hex.DecodeString(encoded[2:])
	if err != nil || len(signature) != 64 {
		return nil, errors.New("release measurement envelope signature is invalid")
	}
	return signature, nil
}

// Includes every byte value at every hex position, plus distinct malformed
// encodings and neighboring widths. Valid UTF-8 is not an admission prerequisite.
func canonicalHexVectors(prefix string, byteCount int) []string {
	good := prefix + strings.Repeat("ab", byteCount)
	vectors := []string{
		good, prefix + strings.Repeat("00", byteCount), "", prefix,
		strings.ToUpper(good), good[1:], good[:len(good)-1], good + "0",
		good + "00", " " + good, good + "\n", good + "\x00",
		prefix + strings.Repeat("é", byteCount), prefix + strings.Repeat("İ", byteCount),
		prefix + strings.Repeat("\xff", 2*byteCount), strings.Repeat("aB", 1024),
	}
	for position := len(prefix); position < len(good); position++ {
		for value := 0; value < 256; value++ {
			mutated := []byte(good)
			mutated[position] = byte(value)
			vectors = append(vectors, string(mutated))
		}
	}
	return vectors
}

// Keeps exact diagnostic strings, not merely a matching success/failure bit.
func canonicalHexErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// The real eight-hop trail is fully sealed through normal settlement promotion.
func canonicalHexRealM8Transition(t *testing.T) (*AttemptSettlementTransition, ed25519.PrivateKey) {
	t.Helper()
	stateDir := t.TempDir()
	server, key, clientID := newMockVerifyServer(t, 12)
	engine, stats, _ := newTestEngine(t, server, key, clientID, 8, nil)
	generation := uint64(1)
	ledger := configureAttemptLedgerTestEngine(t, engine, stats, stateDir, &generation)
	if _, err := engine.RunTrail(context.Background()); err != nil {
		t.Fatal(err)
	}
	participant := AttemptSettlementParticipant{NoID: ledger.identity.NoID, StateDir: stateDir, Stats: stats}
	if err := AdvanceAttemptSettlementEpoch(t.TempDir(), 43, attemptLedgerTestBoundary(), []AttemptSettlementParticipant{participant}); err != nil {
		t.Fatal(err)
	}
	transition := stats.settlementTransition
	if transition == nil || transition.PreFold.AttemptCut == nil || len(transition.Batch) != 1 {
		t.Fatal("real M8 fixture omitted its signed terminal batch")
	}
	cut := transition.PreFold.AttemptCut
	if cut.RecordCount != 8 || len(cut.Records) != 8 || len(cut.Records[7].Assignments) != 7 ||
		cut.Records[7].Proof == nil || len(cut.Records[7].Proof.Hops) != 8 || len(transition.PostFold) != 7 {
		t.Fatal("real M8 fixture changed its eight records, seven providers or full proof census")
	}
	for index, record := range cut.Records {
		if record.M != 8 || record.Sequence != uint64(index+1) {
			t.Fatal("real M8 record depth or sequence changed")
		}
		if index < 7 && (record.Disposition != AttemptDispositionPending || len(record.Assignments) != index+1) {
			t.Fatal("real M8 pending checkpoint missing")
		}
	}
	if err := VerifyAttemptLedgerCut(cut, key.Public().(ed25519.PublicKey), server.serverPublicKeys()); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAttemptSettlementTransition(transition); err != nil {
		t.Fatal(err)
	}
	return transition, key
}

// A signed real terminal reaches the shared parser after complete pre-fold
// replay; a malformed member must not normalize its arbitrary byte length.
func TestAttemptCanonicalHexRealM8MemberWidthBeforeNormalization(t *testing.T) {
	t.Parallel()
	transition, key := canonicalHexRealM8Transition(t)
	encoded, err := json.Marshal(transition)
	if err != nil {
		t.Fatal(err)
	}
	var changed AttemptSettlementTransition
	if err := json.Unmarshal(encoded, &changed); err != nil {
		t.Fatal(err)
	}
	changed.Batch[0].Digest = "0x" + strings.Repeat("aB", 64*1024)
	message, err := attemptSettlementTransitionMessage(&changed)
	if err != nil {
		t.Fatal(err)
	}
	changed.Signature = ed25519.Sign(key, message)
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), message, changed.Signature) {
		t.Fatal("changed batch lost its genuine signature")
	}
	normalizations := 0
	err = verifyAttemptSettlementTransitionWithHexWork(&changed, verifyAttemptLedgerCut, canonicalHexOversizeWork(&normalizations, 66))
	if canonicalHexErrorText(err) != "settlement transition member digest is not canonical 32-byte hex" {
		t.Fatalf("wrong actual member refusal: %v", err)
	}
	if normalizations != 0 {
		t.Fatalf("real M8 settlement digest normalized oversized input: got %d, want exactly 0", normalizations)
	}
}

// The shared standalone helper remains the exact entry used by stream/cut keys.
func TestAttemptCanonicalHexSharedWidthBeforeNormalization(t *testing.T) {
	t.Parallel()
	normalizations := 0
	value, err := canonicalAttemptHex32WithWork("shared", "0x"+strings.Repeat("aB", 64*1024), false, canonicalHexOversizeWork(&normalizations, 66))
	if value != ([32]byte{}) || canonicalHexErrorText(err) != "shared is not canonical 32-byte hex" {
		t.Fatalf("wrong shared refusal: %x %v", value, err)
	}
	if normalizations != 0 {
		t.Fatalf("shared hex parser normalized oversized input: got %d, want exactly 0", normalizations)
	}
}

// The release-local copy must not silently retain the old ordering.
func TestAttemptCanonicalHexReleaseWidthBeforeNormalization(t *testing.T) {
	t.Parallel()
	normalizations := 0
	value, err := parseReleaseHex32WithWork("release", "0x"+strings.Repeat("aB", 64*1024), false, canonicalHexOversizeWork(&normalizations, 66))
	if value != ([32]byte{}) || canonicalHexErrorText(err) != "release is not canonical 32-byte hex" {
		t.Fatalf("wrong release refusal: %x %v", value, err)
	}
	if normalizations != 0 {
		t.Fatalf("release hex parser normalized oversized input: got %d, want exactly 0", normalizations)
	}
}

// Content addresses have their independent 71-byte width and error contract.
func TestAttemptCanonicalHexContentWidthBeforeNormalization(t *testing.T) {
	t.Parallel()
	normalizations := 0
	value, err := parseReleaseContentHashWithWork("sha256:"+strings.Repeat("aB", 64*1024), canonicalHexOversizeWork(&normalizations, 71))
	if value != ([32]byte{}) || canonicalHexErrorText(err) != "content hash is not canonical SHA-256" {
		t.Fatalf("wrong content refusal: %x %v", value, err)
	}
	if normalizations != 0 {
		t.Fatalf("content hash parser normalized oversized input: got %d, want exactly 0", normalizations)
	}
}

// Both legitimate address widths remain independent from arbitrary input size.
func TestAttemptCanonicalHexDepositAddressWidthBeforeNormalization(t *testing.T) {
	t.Parallel()
	normalizations := 0
	err := verifyCanonicalDepositAddressWithWork("deposit", "0x"+strings.Repeat("aB", 64*1024), canonicalHexOversizeWork(&normalizations, 42))
	if canonicalHexErrorText(err) != "deposit is not a canonical nonzero address" {
		t.Fatalf("wrong address refusal: %v", err)
	}
	if normalizations != 0 {
		t.Fatalf("deposit address parser normalized oversized input: got %d, want exactly 0", normalizations)
	}
}

// A complete scalar identity reaches each actual contract address expression.
func TestAttemptCanonicalHexMeasurementAddressesBeforeNormalization(t *testing.T) {
	t.Parallel()
	normalizations := 0
	for _, field := range []string{"coordinator", "vault"} {
		artifact := &ReleaseMeasurementArtifact{Schema: ReleaseMeasurementSchema, DeploymentID: "hex-width", ValidatorID: 1, ChainID: 945, Netuid: 7,
			Coordinator: "0x" + strings.Repeat("ab", 20), SettlementVault: "0x" + strings.Repeat("cd", 20)}
		if field == "coordinator" {
			artifact.Coordinator = "0x" + strings.Repeat("aB", 64*1024)
		} else {
			artifact.SettlementVault = "0x" + strings.Repeat("aB", 64*1024)
		}
		err := verifyReleaseMeasurementIdentityWithHexWork(artifact, canonicalHexOversizeWork(&normalizations, 42))
		if canonicalHexErrorText(err) != "release measurement contract identity is invalid" {
			t.Fatalf("%s refused before actual address boundary: %v", field, err)
		}
	}
	if normalizations != 0 {
		t.Fatalf("measurement contract addresses normalized oversized input: got %d, want exactly 0", normalizations)
	}
}

// Envelope schema/identity preconditions must not mask either address boundary.
func TestAttemptCanonicalHexEnvelopeAddressesBeforeNormalization(t *testing.T) {
	t.Parallel()
	normalizations := 0
	for _, field := range []string{"coordinator", "vault"} {
		envelope := &ReleaseMeasurementEnvelope{Schema: ReleaseMeasurementEnvelopeSchema, MeasurementSchema: ReleaseMeasurementSchema,
			DeploymentID: "hex-width", ValidatorID: 1, ChainID: 945, Netuid: 7,
			Coordinator: "0x" + strings.Repeat("ab", 20), SettlementVault: "0x" + strings.Repeat("cd", 20)}
		if field == "coordinator" {
			envelope.Coordinator = "0x" + strings.Repeat("aB", 64*1024)
		} else {
			envelope.SettlementVault = "0x" + strings.Repeat("aB", 64*1024)
		}
		err := validateReleaseMeasurementEnvelopeWithHexWork(envelope, canonicalHexOversizeWork(&normalizations, 42))
		if canonicalHexErrorText(err) != "release measurement envelope contract identity is invalid" {
			t.Fatalf("%s refused before actual envelope boundary: %v", field, err)
		}
	}
	if normalizations != 0 {
		t.Fatalf("envelope contract addresses normalized oversized input: got %d, want exactly 0", normalizations)
	}
}

// Parsing canonical signature bytes is separate from subsequent sr25519 checks.
func TestAttemptCanonicalHexEnvelopeSignatureBeforeNormalization(t *testing.T) {
	t.Parallel()
	normalizations := 0
	value, err := parseReleaseMeasurementEnvelopeSignatureWithWork("0x"+strings.Repeat("aB", 64*1024), canonicalHexOversizeWork(&normalizations, 130))
	if value != nil || canonicalHexErrorText(err) != "release measurement envelope signature is not canonical 64-byte hex" {
		t.Fatalf("wrong signature refusal: %x %v", value, err)
	}
	if normalizations != 0 {
		t.Fatalf("envelope signature parser normalized oversized input: got %d, want exactly 0", normalizations)
	}
}

// This normalization is reached by real sealer, verifier and intent entrypoints;
// its output can only satisfy their comparisons when the fixed hash width fits.
func TestAttemptCanonicalHexPreparedWidthBeforeNormalization(t *testing.T) {
	t.Parallel()
	normalizations := 0
	encoded := "0x" + strings.Repeat("aB", 64*1024)
	normalized := normalizeReleasePreparedHex32(encoded, canonicalHexOversizeWork(&normalizations, 66))
	if _, err := parseReleaseHex32("prepared", normalized, false); err == nil {
		t.Fatal("oversized normalized prepared hash was admitted")
	}
	if normalizations != 0 {
		t.Fatalf("prepared hash boundary normalized oversized input: got %d, want exactly 0", normalizations)
	}
}

// Generic v1 raw-stat acceptance is retained; only fixed-width work is bounded.
func TestAttemptCanonicalHexStatsEgressBeforeNormalization(t *testing.T) {
	t.Parallel()
	measurement := ReleaseStatsMeasurement{Config: ReleaseStatsConfig{AMin: 8, AlphaNumerator: 1, AlphaDenominator: 10, LatRefMillis: 4000},
		Providers: []ReleaseProviderMeasurement{{ClientID: (connect.Id{1}).String(), LatencyBuckets: make([]uint64, statsLatencyBuckets),
			EgressIPHashHexes: []string{"0x" + strings.Repeat("aB", 64*1024)}}}}
	normalizations := 0
	verified, err := verifyReleaseStatsMeasurementWithHexWork(measurement, verifyAttemptLedgerCut, canonicalHexOversizeWork(&normalizations, 66))
	want := fmt.Sprintf("provider %s egress hash 0 is not canonical", measurement.Providers[0].ClientID)
	if canonicalHexErrorText(err) != want || !reflect.DeepEqual(verified, VerifiedReleaseStats{}) {
		t.Fatalf("wrong egress boundary refusal: %v %+v", err, verified)
	}
	if normalizations != 0 {
		t.Fatalf("statistics egress parser normalized oversized input: got %d, want exactly 0", normalizations)
	}
}

// Every hex byte position is checked against the independent old implementation.
func TestAttemptCanonicalHex32PreservesOldBytesAndErrors(t *testing.T) {
	t.Parallel()
	for _, encoded := range canonicalHexVectors("0x", 32) {
		for _, zeroAllowed := range []bool{false, true} {
			want, wantErr := oldCanonicalHex32("identity", encoded, zeroAllowed)
			got, gotErr := canonicalAttemptHex32("identity", encoded, zeroAllowed)
			release, releaseErr := parseReleaseHex32("identity", encoded, zeroAllowed)
			if got != want || release != want || canonicalHexErrorText(gotErr) != canonicalHexErrorText(wantErr) || canonicalHexErrorText(releaseErr) != canonicalHexErrorText(wantErr) {
				t.Fatalf("hex oracle changed for %x zero=%t: shared=%x/%v release=%x/%v old=%x/%v", encoded, zeroAllowed, got, gotErr, release, releaseErr, want, wantErr)
			}
		}
	}
}

// Independent content-hash comparison includes zero and malformed UTF-8 bytes.
func TestAttemptCanonicalHexContentPreservesOldBytesAndErrors(t *testing.T) {
	t.Parallel()
	for _, encoded := range canonicalHexVectors("sha256:", 32) {
		want, wantErr := oldCanonicalContentHash(encoded)
		got, gotErr := parseReleaseContentHash(encoded)
		if got != want || canonicalHexErrorText(gotErr) != canonicalHexErrorText(wantErr) {
			t.Fatalf("content oracle changed for %x: %x/%v vs %x/%v", encoded, got, gotErr, want, wantErr)
		}
	}
}

// Independent signature parsing includes every byte position and nil on error.
func TestAttemptCanonicalHexSignaturePreservesOldBytesAndErrors(t *testing.T) {
	t.Parallel()
	for _, encoded := range canonicalHexVectors("0x", 64) {
		want, wantErr := oldCanonicalEnvelopeSignature(encoded)
		got, gotErr := parseReleaseMeasurementEnvelopeSignature(encoded)
		if !bytes.Equal(got, want) || (got == nil) != (want == nil) || canonicalHexErrorText(gotErr) != canonicalHexErrorText(wantErr) {
			t.Fatalf("signature oracle changed for %x: %x/%v vs %x/%v", encoded, got, gotErr, want, wantErr)
		}
	}
}

// Bare40 and prefixed42 encode identical addresses and both must remain valid.
func TestAttemptCanonicalHexAddressesPreserveOldForms(t *testing.T) {
	t.Parallel()
	old := func(encoded string) error {
		if encoded != strings.ToLower(encoded) || !common.IsHexAddress(encoded) || common.HexToAddress(encoded) == (common.Address{}) {
			return errors.New("deposit is not a canonical nonzero address")
		}
		return nil
	}
	for _, prefix := range []string{"", "0x"} {
		for _, encoded := range canonicalHexVectors(prefix, 20) {
			got, want := verifyCanonicalDepositAddress("deposit", encoded), old(encoded)
			if canonicalHexErrorText(got) != canonicalHexErrorText(want) {
				t.Fatalf("address oracle changed for %x: %v vs %v", encoded, got, want)
			}
		}
	}
	bare := strings.Repeat("ab", 20)
	if verifyCanonicalDepositAddress("deposit", bare) != nil || verifyCanonicalDepositAddress("deposit", "0x"+bare) != nil ||
		common.HexToAddress(bare) != common.HexToAddress("0x"+bare) {
		t.Fatal("original 40/42 address acceptance changed")
	}
}

// The observation cannot replace real Unicode normalization or return a verdict.
func TestAttemptCanonicalHexObserverRunsActualNormalization(t *testing.T) {
	t.Parallel()
	for _, encoded := range []string{"aB", "İ", "K", "Σ", "\xffA", "0xABC", ""} {
		lengths := []int{}
		got := (canonicalHexWork{normalized: func(length int) { lengths = append(lengths, length) }}).lower(encoded)
		if got != strings.ToLower(encoded) || !reflect.DeepEqual(lengths, []int{len(encoded)}) {
			t.Fatalf("observer replaced actual normalization for %x: %x %v", encoded, got, lengths)
		}
	}
}

// Reentrant work is owned by each invocation, with no shared counter or verdict.
func TestAttemptCanonicalHexObserversAreInvocationLocal(t *testing.T) {
	t.Parallel()
	first, second := 0, 0
	other := canonicalHexWork{normalized: func(int) { second++ }}
	work := canonicalHexWork{normalized: func(int) {
		first++
		if _, err := canonicalAttemptHex32WithWork("nested", releaseHex32([32]byte{2}), false, other); err != nil {
			t.Fatal(err)
		}
	}}
	for index := 0; index < 3; index++ {
		if _, err := parseReleaseContentHashWithWork("sha256:"+strings.Repeat("ab", 32), work); err != nil {
			t.Fatal(err)
		}
	}
	if first != 3 || second != 3 {
		t.Fatalf("normalization escaped call ownership: first=%d second=%d", first, second)
	}
	if _, err := canonicalAttemptHex32("standalone", releaseHex32([32]byte{1}), false); err != nil {
		t.Fatal(err)
	}
	if first != 3 || second != 3 {
		t.Fatal("standalone call reused another observer")
	}
}

// Decoded results are owned values/slices and failed or changed calls recheck.
func TestAttemptCanonicalHexDecodedOutputsRemainOwned(t *testing.T) {
	t.Parallel()
	encoded := "0x" + strings.Repeat("ab", 64)
	first, err := parseReleaseMeasurementEnvelopeSignature(encoded)
	if err != nil {
		t.Fatal(err)
	}
	first[0] ^= 1
	second, err := parseReleaseMeasurementEnvelopeSignature(encoded)
	if err != nil || second[0] != 0xab {
		t.Fatal("signature decode reused mutable output")
	}
	hash := releaseHex32([32]byte{9})
	parsed, err := canonicalAttemptHex32("owned", hash, false)
	if err != nil {
		t.Fatal(err)
	}
	parsed[0] = 7
	again, err := canonicalAttemptHex32("owned", hash, false)
	if err != nil || again[0] != 9 {
		t.Fatal("hash decode reused mutable output")
	}
	for _, bad := range []string{strings.ToUpper(hash), hash + "0", "0x" + strings.Repeat("g", 64)} {
		if _, err := canonicalAttemptHex32("owned", bad, false); err == nil {
			t.Fatal("changed input reused prior admission")
		}
	}
}

// Full real signatures and every public terminal check remain authoritative.
func TestAttemptCanonicalHexRealM8RetainsFullVerification(t *testing.T) {
	t.Parallel()
	transition, _ := canonicalHexRealM8Transition(t)
	encoded, err := json.Marshal(transition)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"signature", "member", "record", "postfold", "identity"} {
		var changed AttemptSettlementTransition
		if err := json.Unmarshal(encoded, &changed); err != nil {
			t.Fatal(err)
		}
		switch mutation {
		case "signature":
			changed.Signature[0] ^= 1
		case "member":
			changed.Batch[0].Digest = releaseHex32([32]byte{0xab})
		case "record":
			changed.PreFold.AttemptCut.Records[0].Signature[0] ^= 1
		case "postfold":
			changed.PostFold[0].QualityPPM ^= 1
		case "identity":
			changed.Identity.ValidatorID++
		}
		if err := VerifyAttemptSettlementTransition(&changed); err == nil {
			t.Fatalf("full terminal check bypassed for %s", mutation)
		}
	}
	if err := VerifyAttemptSettlementBatch([]*AttemptSettlementTransition{transition}); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(transition)
	if err != nil || !bytes.Equal(encoded, after) {
		t.Fatal("verification mutated real M8 input")
	}
}

// Public identities keep both old address representations; envelope changes are
// freshly signed so signature damage cannot masquerade as shape refusal.
func TestAttemptCanonicalHexPublicAddressFormsRemainValid(t *testing.T) {
	t.Parallel()
	measurement, hotkey, uid, signedAt := testReleaseMeasurementEnvelopeInputs(t)
	artifact, _, err := DecodeReleaseMeasurementArtifact(measurement)
	if err != nil {
		t.Fatal(err)
	}
	_, _, original, err := SealReleaseMeasurementEnvelope(measurement, uid, hotkey, releaseMeasurementEnvelopeTestPreparedHash, signedAt)
	if err != nil {
		t.Fatal(err)
	}
	for _, bare := range []bool{false, true} {
		changedArtifact := *artifact
		envelope := *original
		if bare {
			changedArtifact.Coordinator = strings.TrimPrefix(changedArtifact.Coordinator, "0x")
			changedArtifact.SettlementVault = strings.TrimPrefix(changedArtifact.SettlementVault, "0x")
			envelope.Coordinator = strings.TrimPrefix(envelope.Coordinator, "0x")
			envelope.SettlementVault = strings.TrimPrefix(envelope.SettlementVault, "0x")
		}
		if err := verifyReleaseMeasurementIdentity(&changedArtifact); err != nil {
			t.Fatalf("old artifact address form bare=%t changed: %v", bare, err)
		}
		resignReleaseMeasurementEnvelope(t, &envelope, hotkey)
		if err := validateReleaseMeasurementEnvelope(&envelope); err != nil {
			t.Fatalf("old signed envelope address form bare=%t changed: %v", bare, err)
		}
		if common.HexToAddress(changedArtifact.Coordinator) != common.HexToAddress(artifact.Coordinator) ||
			common.HexToAddress(envelope.SettlementVault) != common.HexToAddress(original.SettlementVault) {
			t.Fatal("address representation changed decoded bytes")
		}
	}
	for _, encoded := range []string{
		strings.Repeat("ab", 20), "0x" + strings.Repeat("ab", 20),
		strings.Repeat("00", 20), "0x" + strings.Repeat("00", 20),
		"0X" + strings.Repeat("ab", 20), "0x" + strings.Repeat("AB", 20),
		"0x" + strings.Repeat("g", 40), "0x" + strings.Repeat("\xff", 40),
		"0x" + strings.Repeat("é", 20), strings.Repeat("ab", 19), "0x" + strings.Repeat("ab", 21),
	} {
		oldBad := encoded != strings.ToLower(encoded) || !common.IsHexAddress(encoded) || common.HexToAddress(encoded) == (common.Address{})
		for _, vault := range []bool{false, true} {
			changedArtifact := *artifact
			envelope := *original
			if vault {
				changedArtifact.SettlementVault = encoded
				envelope.SettlementVault = encoded
			} else {
				changedArtifact.Coordinator = encoded
				envelope.Coordinator = encoded
			}
			artifactErr := verifyReleaseMeasurementIdentity(&changedArtifact)
			resignReleaseMeasurementEnvelope(t, &envelope, hotkey)
			envelopeErr := validateReleaseMeasurementEnvelope(&envelope)
			if oldBad {
				if canonicalHexErrorText(artifactErr) != "release measurement contract identity is invalid" || canonicalHexErrorText(envelopeErr) != "release measurement envelope contract identity is invalid" {
					t.Fatalf("old address rejection changed: bytes=%x vault=%t artifact=%v envelope=%v", encoded, vault, artifactErr, envelopeErr)
				}
			} else if artifactErr != nil || envelopeErr != nil {
				t.Fatalf("old address accepted form changed: bytes=%x vault=%t artifact=%v envelope=%v", encoded, vault, artifactErr, envelopeErr)
			}
		}
	}
}

// Caller expectations historically accept ASCII uppercase, but candidate fields
// and sealer inputs are canonical-only. Both contracts must survive separately.
func TestAttemptCanonicalHexPreparedPublicCaseAndErrors(t *testing.T) {
	t.Parallel()
	measurement, hotkey, uid, signedAt := testReleaseMeasurementEnvelopeInputs(t)
	_, _, envelope, err := SealReleaseMeasurementEnvelope(measurement, uid, hotkey, releaseMeasurementEnvelopeTestPreparedHash, signedAt)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{releaseMeasurementEnvelopeTestPreparedHash, strings.ToUpper(releaseMeasurementEnvelopeTestPreparedHash)} {
		if _, _, err := VerifyReleaseMeasurementEnvelope(envelope, measurement, hotkey.PublicKey(), uid, expected); err != nil {
			t.Fatalf("accepted expected-hash case changed: %x %v", expected, err)
		}
	}
	for _, invalid := range []string{
		"", releaseMeasurementEnvelopeTestPreparedHash[1:], releaseMeasurementEnvelopeTestPreparedHash + "0",
		"0x" + strings.Repeat("\xff", 64), "0x" + strings.Repeat("İ", 32),
		"0x" + strings.Repeat("aB", 1024), strings.ToUpper(releaseMeasurementEnvelopeTestPreparedHash),
	} {
		if _, _, _, err := SealReleaseMeasurementEnvelope(measurement, uid, hotkey, invalid, signedAt); canonicalHexErrorText(err) != "release measurement envelope prepared extrinsic hash is not canonical" {
			t.Fatalf("sealer prepared-hash error changed for %x: %v", invalid, err)
		}
		if invalid == strings.ToUpper(releaseMeasurementEnvelopeTestPreparedHash) {
			continue
		}
		if _, _, err := VerifyReleaseMeasurementEnvelope(envelope, measurement, hotkey.PublicKey(), uid, invalid); canonicalHexErrorText(err) != "release measurement envelope prepared extrinsic hash differs" {
			t.Fatalf("expected-hash mismatch changed for %x: %v", invalid, err)
		}
	}
}

// Durable intent verification reaches the same normalized hash path without
// changing its canonical hotkey rule or its case-insensitive extrinsic rule.
func TestAttemptCanonicalHexIntentPreparedRulesRemainDistinct(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	intent := testSteeringIntent(t, stateDir, 3, "")
	measurement, err := os.ReadFile(filepath.Join(stateDir, filepath.FromSlash(intent.MeasurementArtifactPath)))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewIntentStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	verify := func(value *SteeringIntent) error {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.verifyMeasurementEnvelopeLocked(value, measurement)
	}
	if err := verify(&intent); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"uppercase extrinsic", "oversized extrinsic", "uppercase hotkey", "oversized hotkey"} {
		changed := intent
		prepared := *intent.Prepared
		changed.Prepared = &prepared
		switch mutation {
		case "uppercase extrinsic":
			prepared.ExtrinsicHash = strings.ToUpper(prepared.ExtrinsicHash)
		case "oversized extrinsic":
			prepared.ExtrinsicHash = "0x" + strings.Repeat("aB", 1024)
		case "uppercase hotkey":
			prepared.HotkeyHex = strings.ToUpper(prepared.HotkeyHex)
		case "oversized hotkey":
			prepared.HotkeyHex = "0x" + strings.Repeat("aB", 1024)
		}
		err := verify(&changed)
		switch mutation {
		case "uppercase extrinsic":
			if err != nil {
				t.Fatalf("intent stopped accepting expected uppercase extrinsic: %v", err)
			}
		case "oversized extrinsic":
			if canonicalHexErrorText(err) != "release measurement envelope prepared extrinsic hash differs" {
				t.Fatalf("intent extrinsic error changed: %v", err)
			}
		default:
			if canonicalHexErrorText(err) != "steering intent prepared validator hotkey is not canonical" {
				t.Fatalf("intent canonical hotkey error changed: %v", err)
			}
		}
	}
	if err := verify(&intent); err != nil {
		t.Fatalf("invalid intent damaged valid retry: %v", err)
	}
}

// The old egress loop independently pins order, duplicates, zero and bad hex,
// including errors that must not collapse into the shared helper's vocabulary.
func TestAttemptCanonicalHexStatsPreservesOrderZeroAndErrors(t *testing.T) {
	t.Parallel()
	clientID := (connect.Id{1}).String()
	valid := releaseHex32([32]byte{0xab})
	high := releaseHex32([32]byte{0xcd})
	old := func(hashes []string) error {
		prior := ""
		for index, encoded := range hashes {
			if encoded != strings.ToLower(encoded) || len(encoded) != 66 || !strings.HasPrefix(encoded, "0x") || (prior != "" && encoded <= prior) {
				return fmt.Errorf("provider %s egress hash %d is not canonical", clientID, index)
			}
			decoded, err := hex.DecodeString(encoded[2:])
			if err != nil || len(decoded) != 32 {
				return fmt.Errorf("provider %s egress hash %d is invalid", clientID, index)
			}
			var hash [32]byte
			copy(hash[:], decoded)
			if hash == ([32]byte{}) {
				return fmt.Errorf("provider %s contains the zero egress hash", clientID)
			}
			prior = encoded
		}
		return nil
	}
	for _, hashes := range [][]string{
		nil, {}, {valid}, {valid, high}, {high, valid}, {valid, valid}, {releaseHex32([32]byte{})},
		{strings.ToUpper(valid)}, {valid + "0"}, {"0x" + strings.Repeat("g", 64)}, {"0x" + strings.Repeat("\xff", 64)},
		{"0x" + strings.Repeat("aB", 1024)},
	} {
		measurement := ReleaseStatsMeasurement{Config: ReleaseStatsConfig{AMin: 8, AlphaNumerator: 1, AlphaDenominator: 10, LatRefMillis: 4000},
			Providers: []ReleaseProviderMeasurement{{ClientID: clientID, LatencyBuckets: make([]uint64, statsLatencyBuckets), EgressIPHashHexes: hashes}}}
		got, gotErr := VerifyReleaseStatsMeasurement(measurement)
		wantErr := old(hashes)
		if canonicalHexErrorText(gotErr) != canonicalHexErrorText(wantErr) {
			t.Fatalf("egress oracle changed for %x: got=%v old=%v", hashes, gotErr, wantErr)
		}
		if wantErr != nil {
			if !reflect.DeepEqual(got, VerifiedReleaseStats{}) {
				t.Fatal("invalid egress published partial scores")
			}
		} else {
			provider := got.Providers[connect.Id{1}]
			if provider.HasQuality || provider.QualityPPM != 0 || len(provider.EgressIPHashes) != len(hashes) {
				t.Fatal("valid sparse provider semantics changed")
			}
		}
	}
}

// Actual public wrappers, settlement replay, release fields and prepared-input
// callers must reach the observed boundaries rather than test-only substitutes.
func TestAttemptCanonicalHexActualEntrypointEdges(t *testing.T) {
	t.Parallel()
	for _, edge := range []struct{ file, function, callee string }{
		{file: "attempt_ledger.go", function: "canonicalAttemptHex32", callee: "canonicalAttemptHex32WithWork"},
		{file: "attempt_ledger.go", function: "canonicalAttemptHex32WithWork", callee: "lower"},
		{file: "attempt_ledger.go", function: "validateAttemptLedgerIdentity", callee: "canonicalAttemptHex32"},
		{file: "attempt_ledger.go", function: "validateAttemptBoundary", callee: "canonicalAttemptHex32"},
		{file: "attempt_ledger.go", function: "validateAttemptBinding", callee: "canonicalAttemptHex32"},
		{file: "attempt_transition.go", function: "VerifyAttemptSettlementTransition", callee: "verifyAttemptSettlementTransitionWithCutVerifier"},
		{file: "attempt_transition.go", function: "verifyAttemptSettlementTransitionWithCutVerifier", callee: "verifyAttemptSettlementTransitionWithHexWork"},
		{file: "attempt_transition.go", function: "verifyAttemptSettlementTransitionWithHexWork", callee: "canonicalAttemptHex32WithWork"},
		{file: "attempt_transition.go", function: "verifyAttemptSettlementTransitionWithHexWork", callee: "sortedAttemptSettlementQualities"},
		{file: "attempt_transition.go", function: "verifyAttemptSettlementTransitionWithHexWork", callee: "Verify"},
		{file: "release_measurement.go", function: "parseReleaseHex32", callee: "parseReleaseHex32WithWork"},
		{file: "release_measurement.go", function: "parseReleaseHex32WithWork", callee: "lower"},
		{file: "release_measurement.go", function: "parseReleaseContentHash", callee: "parseReleaseContentHashWithWork"},
		{file: "release_measurement.go", function: "parseReleaseContentHashWithWork", callee: "lower"},
		{file: "release_measurement.go", function: "verifyCanonicalDepositAddress", callee: "verifyCanonicalDepositAddressWithWork"},
		{file: "release_measurement.go", function: "verifyCanonicalDepositAddressWithWork", callee: "lower"},
		{file: "release_measurement.go", function: "VerifyReleaseMeasurementArtifact", callee: "verifyReleaseMeasurementIdentity"},
		{file: "release_measurement.go", function: "verifyReleaseMeasurementIdentity", callee: "verifyReleaseMeasurementIdentityWithHexWork"},
		{file: "release_measurement.go", function: "verifyReleaseMeasurementIdentityWithHexWork", callee: "verifyReleaseMeasurementCommonIdentityWithHexWork"},
		{file: "release_measurement.go", function: "verifyReleaseMeasurementCommonIdentity", callee: "verifyReleaseMeasurementCommonIdentityWithHexWork"},
		{file: "release_measurement.go", function: "verifyReleaseMeasurementCommonIdentityWithHexWork", callee: "lower"},
		{file: "release_measurement_v2.go", function: "ownReleaseMeasurementV2", callee: "verifyReleaseMeasurementCommonIdentity"},
		{file: "release_measurement_envelope.go", function: "parseReleaseMeasurementEnvelopeSignature", callee: "parseReleaseMeasurementEnvelopeSignatureWithWork"},
		{file: "release_measurement_envelope.go", function: "parseReleaseMeasurementEnvelopeSignatureWithWork", callee: "lower"},
		{file: "release_measurement_envelope.go", function: "validateReleaseMeasurementEnvelope", callee: "validateReleaseMeasurementEnvelopeWithHexWork"},
		{file: "release_measurement_envelope.go", function: "validateReleaseMeasurementEnvelopeWithHexWork", callee: "validateReleaseMeasurementEnvelopeFieldsWithHexWork"},
		{file: "release_measurement_envelope.go", function: "validateReleaseMeasurementEnvelopeFieldsWithHexWork", callee: "lower"},
		{file: "release_measurement_envelope.go", function: "validateReleaseMeasurementEnvelopeFieldsWithHexWork", callee: "Verify"},
		{file: "release_measurement_envelope_v2.go", function: "validateReleaseMeasurementEnvelopeV2", callee: "validateReleaseMeasurementEnvelopeFieldsWithHexWork"},
		{file: "release_measurement_envelope.go", function: "DecodeReleaseMeasurementEnvelope", callee: "validateReleaseMeasurementEnvelope"},
		{file: "release_measurement_envelope.go", function: "SealReleaseMeasurementEnvelope", callee: "sealReleaseMeasurementEnvelopeWithHexWork"},
		{file: "release_measurement_envelope.go", function: "sealReleaseMeasurementEnvelopeWithHexWork", callee: "normalizeReleasePreparedHex32"},
		{file: "release_measurement_envelope.go", function: "sealReleaseMeasurementEnvelopeWithHexWork", callee: "DecodeReleaseMeasurementArtifact"},
		{file: "release_measurement_envelope.go", function: "sealReleaseMeasurementEnvelopeWithHexWork", callee: "validateReleaseMeasurementEnvelope"},
		{file: "release_measurement_envelope.go", function: "VerifyReleaseMeasurementEnvelope", callee: "verifyReleaseMeasurementEnvelopeWithHexWork"},
		{file: "release_measurement_envelope.go", function: "verifyReleaseMeasurementEnvelopeWithHexWork", callee: "normalizeReleasePreparedHex32"},
		{file: "release_measurement_envelope.go", function: "verifyReleaseMeasurementEnvelopeWithHexWork", callee: "DecodeReleaseMeasurementArtifact"},
		{file: "release_measurement_envelope.go", function: "verifyReleaseMeasurementEnvelopeWithHexWork", callee: "Verify"},
		{file: "intent.go", function: "verifyMeasurementEnvelopeLocked", callee: "verifyMeasurementEnvelopeWithHexWorkLocked"},
		{file: "intent.go", function: "verifyMeasurementEnvelopeWithHexWorkLocked", callee: "normalizeReleasePreparedHex32"},
		{file: "intent.go", function: "verifyMeasurementEnvelopeWithHexWorkLocked", callee: "verifyReleaseMeasurementEnvelopeWithHexWork"},
		{file: "measurement_stats.go", function: "VerifyReleaseStatsMeasurement", callee: "verifyReleaseStatsMeasurementWithCutVerifier"},
		{file: "measurement_stats.go", function: "verifyReleaseStatsMeasurementWithCutVerifier", callee: "verifyReleaseStatsMeasurementWithHexWork"},
		{file: "measurement_stats.go", function: "verifyReleaseStatsMeasurementWithHexWork", callee: "lower"},
		{file: "measurement_stats.go", function: "verifyReleaseStatsMeasurementWithHexWork", callee: "verifyCut"},
		{file: "measurement_stats.go", function: "verifyReleaseStatsMeasurementWithHexWork", callee: "verifyReleaseStatsAgainstAttemptCut"},
		{file: "measurement_stats.go", function: "verifyReleaseStatsMeasurementWithHexWork", callee: "verifyAttemptSettlementTransitionForMeasurementWithCutVerifier"},
		{file: "canonical_hex_work.go", function: "normalizeReleasePreparedHex32", callee: "lower"},
		{file: "canonical_hex_work.go", function: "lower", callee: "ToLower"},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), edge.file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != edge.function {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch callee := call.Fun.(type) {
				case *ast.Ident:
					found = found || callee.Name == edge.callee
				case *ast.SelectorExpr:
					found = found || callee.Sel.Name == edge.callee
				}
				return true
			})
		}
		if !found {
			t.Errorf("actual canonical parser edge missing: %s %s -> %s", edge.file, edge.function, edge.callee)
		}
	}
}

// Genuine sealed measurement bytes pass every prerequisite before the actual
// public sealer body inspects its oversized prepared hash.
func TestAttemptCanonicalHexSealerPreparedBeforeNormalization(t *testing.T) {
	t.Parallel()
	measurement, hotkey, uid, signedAt := testReleaseMeasurementEnvelopeInputs(t)
	normalizations := 0
	raw, hash, envelope, err := sealReleaseMeasurementEnvelopeWithHexWork(measurement, uid, hotkey, "0x"+strings.Repeat("aB", 64*1024), signedAt, canonicalHexOversizeWork(&normalizations, 66))
	if raw != nil || hash != "" || envelope != nil || canonicalHexErrorText(err) != "release measurement envelope prepared extrinsic hash is not canonical" {
		t.Fatalf("wrong actual sealer refusal: %v", err)
	}
	if normalizations != 0 {
		t.Fatalf("measurement sealer normalized oversized prepared input: got %d, want exactly 0", normalizations)
	}
}

// The expected-hash boundary follows a fully verified genuine hotkey signature.
func TestAttemptCanonicalHexVerifierPreparedBeforeNormalization(t *testing.T) {
	t.Parallel()
	measurement, hotkey, uid, signedAt := testReleaseMeasurementEnvelopeInputs(t)
	_, _, envelope, err := SealReleaseMeasurementEnvelope(measurement, uid, hotkey, releaseMeasurementEnvelopeTestPreparedHash, signedAt)
	if err != nil {
		t.Fatal(err)
	}
	normalizations := 0
	artifact, verified, err := verifyReleaseMeasurementEnvelopeWithHexWork(envelope, measurement, hotkey.PublicKey(), uid, "0x"+strings.Repeat("aB", 64*1024), canonicalHexOversizeWork(&normalizations, 66))
	if artifact != nil || verified != nil || canonicalHexErrorText(err) != "release measurement envelope prepared extrinsic hash differs" {
		t.Fatalf("wrong actual verifier refusal: %v", err)
	}
	if normalizations != 0 {
		t.Fatalf("measurement verifier normalized oversized expected input: got %d, want exactly 0", normalizations)
	}
}

// Existing durable-file ownership, content hash and signature checks all run
// before either independently mutated prepared field reaches normalization.
func TestAttemptCanonicalHexIntentPreparedBeforeNormalization(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	intent := testSteeringIntent(t, stateDir, 3, "")
	measurement, err := os.ReadFile(filepath.Join(stateDir, filepath.FromSlash(intent.MeasurementArtifactPath)))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewIntentStore(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	normalizations := 0
	for _, field := range []string{"hotkey", "extrinsic"} {
		changed := intent
		prepared := *intent.Prepared
		changed.Prepared = &prepared
		want := "steering intent prepared validator hotkey is not canonical"
		if field == "hotkey" {
			prepared.HotkeyHex = "0x" + strings.Repeat("aB", 64*1024)
		} else {
			prepared.ExtrinsicHash = "0x" + strings.Repeat("aB", 64*1024)
			want = "release measurement envelope prepared extrinsic hash differs"
		}
		err := func() error {
			store.mu.Lock()
			defer store.mu.Unlock()
			return store.verifyMeasurementEnvelopeWithHexWorkLocked(&changed, measurement, canonicalHexOversizeWork(&normalizations, 66))
		}()
		if canonicalHexErrorText(err) != want {
			t.Fatalf("wrong actual intent %s refusal: %v", field, err)
		}
	}
	if normalizations != 0 {
		t.Fatalf("durable intent normalized oversized prepared fields: got %d, want exactly 0", normalizations)
	}
}
