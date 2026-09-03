package validator

// release_measurement_envelope_test.go exercises canonical encoding, complete
// field binding and explicit validator-hotkey/UID trust pinning.

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/urfoundation/sn/crv4"
)

var releaseMeasurementEnvelopeTestPreparedHash = releaseHex32([32]byte{31})

// testReleaseMeasurementEnvelopeInputs creates one canonical measurement and
// deterministic validator key for envelope tests.
func testReleaseMeasurementEnvelopeInputs(t *testing.T) ([]byte, *crv4.Keypair, uint16, time.Time) {
	t.Helper()
	stateDir := t.TempDir()
	intent := testSteeringIntent(t, stateDir, 3, "")
	measurement, err := os.ReadFile(filepath.Join(stateDir, filepath.FromSlash(intent.MeasurementArtifactPath)))
	if err != nil {
		t.Fatal(err)
	}
	var seed [32]byte
	for index := range seed {
		seed[index] = byte(index + 41)
	}
	hotkey, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return measurement, hotkey, intent.SelfUID, time.Date(2026, time.September, 3, 1, 2, 3, 456_000_000, time.FixedZone("offset", 2*60*60))
}

// resignReleaseMeasurementEnvelope updates a test envelope after a deliberate
// self-consistent mutation so artifact cross-binding is tested independently.
func resignReleaseMeasurementEnvelope(t *testing.T, envelope *ReleaseMeasurementEnvelope, hotkey *crv4.Keypair) {
	t.Helper()
	envelope.SigningHash = ""
	envelope.Signature = ""
	digest, err := releaseMeasurementEnvelopeSigningDigest(envelope)
	if err != nil {
		t.Fatal(err)
	}
	envelope.SigningHash = "sha256:" + hex.EncodeToString(digest[:])
	signature, err := hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	envelope.Signature = "0x" + hex.EncodeToString(signature)
}

// TestReleaseMeasurementEnvelopeRoundTrip verifies the successful standalone
// seal, strict decode and trusted measurement verification path.
func TestReleaseMeasurementEnvelopeRoundTrip(t *testing.T) {
	measurement, hotkey, uid, signedAt := testReleaseMeasurementEnvelopeInputs(t)
	encoded, contentHash, envelope, err := SealReleaseMeasurementEnvelope(measurement, uid, hotkey, releaseMeasurementEnvelopeTestPreparedHash, signedAt)
	if err != nil {
		t.Fatal(err)
	}
	if contentHash != ReleaseMeasurementEnvelopeContentHash(encoded) {
		t.Fatalf("content hash = %q", contentHash)
	}
	if envelope.SignedAt != signedAt.UTC().Format(time.RFC3339Nano) || envelope.MeasurementArtifactHash != ReleaseMeasurementContentHash(measurement) || envelope.MeasurementArtifactSize != uint64(len(measurement)) {
		t.Fatalf("envelope content binding = %+v", envelope)
	}
	decoded, err := DecodeReleaseMeasurementEnvelope(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, envelope) {
		t.Fatalf("decoded envelope differs\n got: %+v\nwant: %+v", decoded, envelope)
	}
	artifact, verified, err := VerifyReleaseMeasurementEnvelope(decoded, measurement, hotkey.PublicKey(), uid, releaseMeasurementEnvelopeTestPreparedHash)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.SelfUID != uid || verified == nil {
		t.Fatalf("verified measurement = artifact %+v, reconstruction %+v", artifact, verified)
	}

	nonCanonical := bytes.TrimSuffix(encoded, []byte{'\n'})
	if _, err := DecodeReleaseMeasurementEnvelope(nonCanonical); err == nil {
		t.Fatal("non-canonical envelope without final newline was accepted")
	}
	withUnknownField := append([]byte(nil), encoded[:len(encoded)-2]...)
	withUnknownField = append(withUnknownField, []byte(",\"unknown\":true}\n")...)
	if _, err := DecodeReleaseMeasurementEnvelope(withUnknownField); err == nil {
		t.Fatal("envelope with unknown field was accepted")
	}
}

// TestReleaseMeasurementEnvelopeSignatureBindsEveryField mutates each public
// field while retaining the original signature and requires rejection.
func TestReleaseMeasurementEnvelopeSignatureBindsEveryField(t *testing.T) {
	measurement, hotkey, uid, signedAt := testReleaseMeasurementEnvelopeInputs(t)
	_, _, original, err := SealReleaseMeasurementEnvelope(measurement, uid, hotkey, releaseMeasurementEnvelopeTestPreparedHash, signedAt)
	if err != nil {
		t.Fatal(err)
	}
	otherHotkey, err := crv4.KeypairFromSeed([32]byte{99})
	if err != nil {
		t.Fatal(err)
	}
	validHash := "0x" + hex.EncodeToString(bytes.Repeat([]byte{0xa5}, 32))
	validContentHash := "sha256:" + hex.EncodeToString(bytes.Repeat([]byte{0xb6}, 32))
	tests := []struct {
		name   string
		mutate func(*ReleaseMeasurementEnvelope)
	}{
		{name: "measurement schema", mutate: func(value *ReleaseMeasurementEnvelope) { value.MeasurementSchema += "-other" }},
		{name: "deployment", mutate: func(value *ReleaseMeasurementEnvelope) { value.DeploymentID += "-other" }},
		{name: "chain", mutate: func(value *ReleaseMeasurementEnvelope) { value.ChainID++ }},
		{name: "genesis", mutate: func(value *ReleaseMeasurementEnvelope) { value.GenesisHash = validHash }},
		{name: "coordinator", mutate: func(value *ReleaseMeasurementEnvelope) {
			value.Coordinator = "0x0000000000000000000000000000000000000003"
		}},
		{name: "settlement vault", mutate: func(value *ReleaseMeasurementEnvelope) {
			value.SettlementVault = "0x0000000000000000000000000000000000000004"
		}},
		{name: "validator ID", mutate: func(value *ReleaseMeasurementEnvelope) { value.ValidatorID++ }},
		{name: "validator hotkey", mutate: func(value *ReleaseMeasurementEnvelope) { value.ValidatorHotkey = releaseHex32(otherHotkey.PublicKey()) }},
		{name: "validator UID", mutate: func(value *ReleaseMeasurementEnvelope) { value.ValidatorUID++ }},
		{name: "netuid", mutate: func(value *ReleaseMeasurementEnvelope) { value.Netuid++ }},
		{name: "subnet epoch", mutate: func(value *ReleaseMeasurementEnvelope) { value.SubnetEpoch++ }},
		{name: "settlement epoch", mutate: func(value *ReleaseMeasurementEnvelope) { value.SettlementEpoch++ }},
		{name: "policy", mutate: func(value *ReleaseMeasurementEnvelope) { value.PolicyHash = validHash }},
		{name: "lineage", mutate: func(value *ReleaseMeasurementEnvelope) { value.PreviousArtifactHash = validContentHash }},
		{name: "native snapshot block", mutate: func(value *ReleaseMeasurementEnvelope) { value.NativeSnapshotBlock++ }},
		{name: "native snapshot hash", mutate: func(value *ReleaseMeasurementEnvelope) { value.NativeSnapshotHash = validHash }},
		{name: "EVM snapshot block", mutate: func(value *ReleaseMeasurementEnvelope) { value.EVMSnapshotBlock++ }},
		{name: "EVM snapshot hash", mutate: func(value *ReleaseMeasurementEnvelope) { value.EVMSnapshotHash = validHash }},
		{name: "artifact hash", mutate: func(value *ReleaseMeasurementEnvelope) { value.MeasurementArtifactHash = validContentHash }},
		{name: "artifact size", mutate: func(value *ReleaseMeasurementEnvelope) { value.MeasurementArtifactSize++ }},
		{name: "prepared extrinsic", mutate: func(value *ReleaseMeasurementEnvelope) { value.PreparedExtrinsicHash = validHash }},
		{name: "signing time", mutate: func(value *ReleaseMeasurementEnvelope) { value.SignedAt = "2026-09-03T01:02:04Z" }},
		{name: "signature scheme", mutate: func(value *ReleaseMeasurementEnvelope) { value.SignatureScheme = "other" }},
	}
	for _, test := range tests {
		mutated := *original
		test.mutate(&mutated)
		encoded, marshalErr := canonicalReleaseMeasurementEnvelopeBytes(&mutated)
		if marshalErr != nil {
			t.Fatalf("%s: %v", test.name, marshalErr)
		}
		if _, decodeErr := DecodeReleaseMeasurementEnvelope(encoded); decodeErr == nil {
			t.Fatalf("%s mutation retained a valid signature", test.name)
		}
	}
}

// TestReleaseMeasurementEnvelopeRejectsArtifactAndTrustSubstitution covers
// self-consistent field changes, wrong trusted identities and altered bytes.
func TestReleaseMeasurementEnvelopeRejectsArtifactAndTrustSubstitution(t *testing.T) {
	measurement, hotkey, uid, signedAt := testReleaseMeasurementEnvelopeInputs(t)
	_, _, envelope, err := SealReleaseMeasurementEnvelope(measurement, uid, hotkey, releaseMeasurementEnvelopeTestPreparedHash, signedAt)
	if err != nil {
		t.Fatal(err)
	}

	selfConsistentMutations := []struct {
		name   string
		mutate func(*ReleaseMeasurementEnvelope)
	}{
		{name: "identity", mutate: func(value *ReleaseMeasurementEnvelope) { value.DeploymentID += "-other" }},
		{name: "native snapshot", mutate: func(value *ReleaseMeasurementEnvelope) { value.NativeSnapshotBlock++ }},
		{name: "EVM snapshot", mutate: func(value *ReleaseMeasurementEnvelope) { value.EVMSnapshotBlock++ }},
		{name: "policy hash", mutate: func(value *ReleaseMeasurementEnvelope) {
			value.PolicyHash = "0x" + hex.EncodeToString(bytes.Repeat([]byte{0xc7}, 32))
		}},
		{name: "artifact hash", mutate: func(value *ReleaseMeasurementEnvelope) {
			value.MeasurementArtifactHash = "sha256:" + hex.EncodeToString(bytes.Repeat([]byte{0xd8}, 32))
		}},
		{name: "artifact size", mutate: func(value *ReleaseMeasurementEnvelope) { value.MeasurementArtifactSize++ }},
	}
	for _, test := range selfConsistentMutations {
		mutated := *envelope
		test.mutate(&mutated)
		resignReleaseMeasurementEnvelope(t, &mutated, hotkey)
		if err := validateReleaseMeasurementEnvelope(&mutated); err != nil {
			t.Fatalf("%s mutation did not produce a valid signed envelope: %v", test.name, err)
		}
		if _, _, verifyErr := VerifyReleaseMeasurementEnvelope(&mutated, measurement, hotkey.PublicKey(), uid, releaseMeasurementEnvelopeTestPreparedHash); verifyErr == nil {
			t.Fatalf("self-consistent %s substitution was accepted", test.name)
		}
	}

	wrongHotkey, err := crv4.KeypairFromSeed([32]byte{77})
	if err != nil {
		t.Fatal(err)
	}
	_, _, wrongKeyEnvelope, err := SealReleaseMeasurementEnvelope(measurement, uid, wrongHotkey, releaseMeasurementEnvelopeTestPreparedHash, signedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseMeasurementEnvelope(wrongKeyEnvelope); err != nil {
		t.Fatalf("self-signed wrong-key envelope is malformed: %v", err)
	}
	if _, _, err := VerifyReleaseMeasurementEnvelope(wrongKeyEnvelope, measurement, hotkey.PublicKey(), uid, releaseMeasurementEnvelopeTestPreparedHash); err == nil {
		t.Fatal("self-consistent envelope from wrong hotkey was accepted")
	}
	if _, _, err := VerifyReleaseMeasurementEnvelope(envelope, measurement, hotkey.PublicKey(), uid+1, releaseMeasurementEnvelopeTestPreparedHash); err == nil {
		t.Fatal("envelope was accepted for the wrong pinned UID")
	}
	alteredMeasurement := append([]byte(nil), measurement...)
	alteredMeasurement[len(alteredMeasurement)/2] ^= 1
	if _, _, err := VerifyReleaseMeasurementEnvelope(envelope, alteredMeasurement, hotkey.PublicKey(), uid, releaseMeasurementEnvelopeTestPreparedHash); err == nil {
		t.Fatal("altered measurement bytes were accepted")
	}
	if _, _, err := VerifyReleaseMeasurementEnvelope(envelope, measurement, [32]byte{}, uid, releaseMeasurementEnvelopeTestPreparedHash); err == nil {
		t.Fatal("zero expected hotkey was accepted")
	}
	if _, _, err := VerifyReleaseMeasurementEnvelope(envelope, measurement, hotkey.PublicKey(), uid, releaseHex32([32]byte{32})); err == nil {
		t.Fatal("envelope was accepted for a different prepared extrinsic")
	}
}

// TestReleaseMeasurementEnvelopeSealRejectsWrongUID requires the signer to
// bind the UID actually contained in the canonical measurement artifact.
func TestReleaseMeasurementEnvelopeSealRejectsWrongUID(t *testing.T) {
	measurement, hotkey, uid, signedAt := testReleaseMeasurementEnvelopeInputs(t)
	if _, _, _, err := SealReleaseMeasurementEnvelope(measurement, uid+1, hotkey, releaseMeasurementEnvelopeTestPreparedHash, signedAt); err == nil {
		t.Fatal("measurement was sealed with a different validator UID")
	}
}
