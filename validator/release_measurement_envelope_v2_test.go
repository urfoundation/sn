//go:build linux || darwin

package validator

// These envelopes wrap genuine two-operator M8 records and typed proof streams.
// Native signatures use the real sr25519 implementation. Fixture activation
// pins are independent test inputs, not a substitute for on-chain authority.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/urfoundation/sn/crv4"
)

// Each verification obtains new scratch owners and a new independent options
// snapshot. No successful replay result is reused as an authorization token.
type releaseMeasurementEnvelopeV2TestFixture struct {
	measurement []byte
	artifact    *ReleaseMeasurementArtifact
	hotkey      *crv4.Keypair
	options     func() ReleaseMeasurementV2Options
	want        *VerifiedReleaseMeasurement
}

// Changing the test activation signer re-signs actual compact headers only;
// the same real records, stream bytes, VPKs, identities and scoring stay intact.
func bindReleaseMeasurementEnvelopeV2TestHotkey(t *testing.T, fixture *releaseMeasurementV2TestFixture, hotkey *crv4.Keypair) {
	t.Helper()
	for index := range fixture.artifact.Inputs {
		input := &fixture.artifact.Inputs[index]
		operator := fixture.operators[input.NoID]
		operator.seal.expected.Activation.Hotkey = hotkey.PublicKey()
		input.AttemptCutV2.Context.Activation.Hotkey = hotkey.PublicKey()
		signature, err := input.AttemptCutV2.Sign(operator.seal.key, operator.seal.bounds)
		if err != nil {
			t.Fatal(err)
		}
		input.AttemptCutV2.Signature = signature
	}
}

// The independent legacy oracle still supplies every exact score and weight.
func newReleaseMeasurementEnvelopeV2TestFixture(t *testing.T, completed int) *releaseMeasurementEnvelopeV2TestFixture {
	t.Helper()
	fixture := newReleaseMeasurementV2TestFixture(t, completed)
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x71})
	if err != nil {
		t.Fatal(err)
	}
	bindReleaseMeasurementEnvelopeV2TestHotkey(t, fixture, hotkey)
	measurement, _, err := SealReleaseMeasurementArtifactV2(t.Context(), fixture.artifact, fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	return &releaseMeasurementEnvelopeV2TestFixture{
		measurement: measurement, artifact: fixture.artifact, hotkey: hotkey,
		options: func() ReleaseMeasurementV2Options { return fixture.options(t) }, want: fixture.want,
	}
}

// Terminal envelopes re-seal the actual complete batch after binding the
// native test signer, then retain real successor work or real positive EMA.
func newReleaseMeasurementEnvelopeV2TerminalTestFixture(t *testing.T, nonempty bool) *releaseMeasurementEnvelopeV2TestFixture {
	t.Helper()
	var fixture *releaseMeasurementV2SettlementTestFixture
	if nonempty {
		fixture = newReleaseMeasurementV2SettlementNonemptyTestFixture(t)
	} else {
		fixture = newReleaseMeasurementV2SettlementTestFixture(t, 15, false, 1)
	}
	hotkey, err := crv4.KeypairFromSeed([32]byte{0x72})
	if err != nil {
		t.Fatal(err)
	}
	bindReleaseMeasurementEnvelopeV2TestHotkey(t, fixture.previous, hotkey)
	for _, terminal := range fixture.terminalTs {
		terminal.seal.expected.Activation.Hotkey = hotkey.PublicKey()
		terminal.cut.Context.Activation.Hotkey = hotkey.PublicKey()
		signature, err := terminal.cut.Sign(terminal.seal.key, terminal.seal.bounds)
		if err != nil {
			t.Fatal(err)
		}
		terminal.cut.Signature = signature
	}
	fixture.current.artifact.SettlementClosureV2 = sealAttemptSettlementV2Test(t, fixture.terminalTs...)
	bindReleaseMeasurementEnvelopeV2TestHotkey(t, fixture.current, hotkey)
	previous, _, err := SealReleaseMeasurementArtifactV2(t.Context(), fixture.previous.artifact, fixture.previous.options(t))
	if err != nil {
		t.Fatal(err)
	}
	fixture.current.artifact.PreviousArtifactHash = ReleaseMeasurementContentHash(previous)
	if _, err := VerifyReleaseMeasurementLineageV2(t.Context(), previous, fixture.previous.options(t), fixture.current.artifact, fixture.options(t)); err != nil {
		t.Fatal(err)
	}
	measurement, _, err := SealReleaseMeasurementArtifactV2(t.Context(), fixture.current.artifact, fixture.options(t))
	if err != nil {
		t.Fatal(err)
	}
	return &releaseMeasurementEnvelopeV2TestFixture{
		measurement: measurement, artifact: fixture.current.artifact, hotkey: hotkey,
		options: func() ReleaseMeasurementV2Options { return fixture.options(t) },
	}
}

// A fixed UTC time avoids wall-clock ordering as an acceptance condition.
func (self *releaseMeasurementEnvelopeV2TestFixture) seal(t *testing.T) ([]byte, *ReleaseMeasurementEnvelope) {
	t.Helper()
	raw, hash, envelope, err := SealReleaseMeasurementEnvelopeV2(t.Context(), self.measurement, self.artifact.SelfUID, self.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Date(2026, time.September, 6, 0, 0, 0, 0, time.UTC), self.options())
	if err != nil || envelope == nil || hash != ReleaseMeasurementEnvelopeContentHash(raw) {
		t.Fatalf("actual compact envelope seal: %v", err)
	}
	return raw, envelope
}

// A deliberate candidate mutation can be genuinely re-signed to distinguish
// cryptographic validity from the caller's independent decision authority.
func resignReleaseMeasurementEnvelopeV2Test(t *testing.T, envelope *ReleaseMeasurementEnvelope, hotkey *crv4.Keypair, domain string) {
	t.Helper()
	digest, err := releaseMeasurementEnvelopeSigningDigestWithDomain(envelope, domain)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	envelope.SigningHash = "sha256:" + hex.EncodeToString(digest[:])
	envelope.Signature = "0x" + hex.EncodeToString(signature)
}

// Successful signing, canonical decoding and trusted verification all use the
// real paths; both operator streams retain complete and failed M8 exposure.
func TestReleaseMeasurementEnvelopeV2RoundTripGenuineM8(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	raw, envelope := fixture.seal(t)
	decoded, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), raw, fixture.options().MaxControlBytes)
	if err != nil || !reflect.DeepEqual(decoded, envelope) {
		t.Fatalf("compact envelope round trip: %v", err)
	}
	options := fixture.options()
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), decoded, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, strings.ToUpper(releaseMeasurementEnvelopeTestPreparedHash), options)
	if err != nil || artifact == nil || *reads == 0 || len(verified.ReplayByNO) != 2 || verified.SettlementReplayByNO != nil {
		t.Fatalf("compact envelope real replay: reads=%d error=%v", *reads, err)
	}
	if !reflect.DeepEqual(verified.Decision, fixture.want) {
		t.Fatal("compact envelope changed the independent legacy decision")
	}
	for _, replay := range verified.ReplayByNO {
		if replay.Records.ItemCount != 18 || replay.CompleteCount != 2 || replay.FailedCount != 1 {
			t.Fatalf("actual M8 census changed: %+v", replay)
		}
	}
}

// The unchanged AMin8 case produces real quality; a green zero-weight-only
// envelope would not demonstrate preservation of the actual scoring path.
func TestReleaseMeasurementEnvelopeV2RetainsPositiveQuality(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 15)
	_, envelope := fixture.seal(t)
	_, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, fixture.options())
	if err != nil || !reflect.DeepEqual(verified.Decision, fixture.want) {
		t.Fatalf("positive-quality envelope changed exact decision: %v", err)
	}
	positive := 0
	for noID, replay := range verified.ReplayByNO {
		if replay.Records.ItemCount != 122 || replay.CompleteCount != 15 || replay.FailedCount != 1 {
			t.Fatal("positive M8 census changed")
		}
		for _, provider := range verified.Decision.StatsByNO[noID].Providers {
			if provider.HasQuality && provider.QualityPPM > 0 {
				positive++
			}
		}
	}
	if positive < 2 {
		t.Fatal("real operator quality disappeared")
	}
}

// Independent fresh replays retain the true terminal18/current8 record split.
func TestReleaseMeasurementEnvelopeV2TerminalNonemptyRestart(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TerminalTestFixture(t, true)
	raw, envelope := fixture.seal(t)
	for attempt := 0; attempt < 2; attempt++ {
		decoded, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), bytes.Clone(raw), fixture.options().MaxControlBytes)
		if err != nil || !reflect.DeepEqual(decoded, envelope) {
			t.Fatalf("restart envelope decode: %v", err)
		}
		_, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), decoded, bytes.Clone(fixture.measurement), fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, fixture.options())
		if err != nil || len(verified.ReplayByNO) != 2 || len(verified.SettlementReplayByNO) != 2 {
			t.Fatalf("complete terminal/current envelope: %v", err)
		}
		for noID, terminal := range verified.SettlementReplayByNO {
			current := verified.ReplayByNO[noID]
			if terminal.Records.ItemCount != 18 || terminal.CompleteCount != 2 || terminal.FailedCount != 1 || current.Records.ItemCount != 8 || current.CompleteCount != 1 || current.FailedCount != 0 {
				t.Fatalf("restart lost actual terminal/current work: terminal=%+v current=%+v", terminal, current)
			}
		}
	}
}

// A truly empty successor retains the complete positive terminal EMA fold.
func TestReleaseMeasurementEnvelopeV2TerminalRetainsPositiveQuality(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TerminalTestFixture(t, false)
	_, envelope := fixture.seal(t)
	_, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, fixture.options())
	if err != nil || len(verified.SettlementReplayByNO) != 2 {
		t.Fatalf("positive terminal envelope: %v", err)
	}
	positive := 0
	for noID, terminal := range verified.SettlementReplayByNO {
		if terminal.Records.ItemCount != 122 || verified.ReplayByNO[noID].Records.ItemCount != 0 {
			t.Fatal("terminal/empty-successor census changed")
		}
		for _, provider := range verified.Decision.StatsByNO[noID].Providers {
			if provider.HasQuality && provider.QualityPPM > 0 {
				positive++
			}
		}
	}
	if positive < 2 {
		t.Fatal("terminal envelope invented an empty pool-quality result")
	}
}

// Reflection reaches every current and future envelope field rather than a
// hand-picked subset. Each alteration keeps the original real signature.
func TestReleaseMeasurementEnvelopeV2SignatureBindsEveryField(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	_, original := fixture.seal(t)
	for index := 0; index < reflect.TypeOf(*original).NumField(); index++ {
		candidate := *original
		field := reflect.ValueOf(&candidate).Elem().Field(index)
		switch field.Kind() {
		case reflect.String:
			field.SetString(field.String() + "x")
		case reflect.Uint16, reflect.Uint64:
			field.SetUint(field.Uint() + 1)
		default:
			t.Fatalf("new unsigned field kind: %s", reflect.TypeOf(candidate).Field(index).Name)
		}
		raw, err := canonicalReleaseMeasurementEnvelopeBytes(&candidate)
		if err != nil {
			t.Fatal(err)
		}
		if decoded, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), raw, fixture.options().MaxControlBytes); err == nil || decoded != nil {
			t.Fatalf("field %s escaped signature binding", reflect.TypeOf(candidate).Field(index).Name)
		}
	}
}

// A legitimately re-signed competing decision must refuse before the first
// metadata/data callback, including the content and extrinsic cross-bindings.
func TestReleaseMeasurementEnvelopeV2ResignedDecisionRefusesBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	_, original := fixture.seal(t)
	for _, change := range []struct {
		name   string
		mutate func(*ReleaseMeasurementEnvelope)
	}{
		{name: "deployment", mutate: func(value *ReleaseMeasurementEnvelope) { value.DeploymentID += "-other" }},
		{name: "chain", mutate: func(value *ReleaseMeasurementEnvelope) { value.ChainID++ }},
		{name: "genesis", mutate: func(value *ReleaseMeasurementEnvelope) { value.GenesisHash = releaseHex32([32]byte{0x61}) }},
		{name: "coordinator", mutate: func(value *ReleaseMeasurementEnvelope) {
			value.Coordinator = "0x0000000000000000000000000000000000000061"
		}},
		{name: "vault", mutate: func(value *ReleaseMeasurementEnvelope) {
			value.SettlementVault = "0x0000000000000000000000000000000000000062"
		}},
		{name: "validator", mutate: func(value *ReleaseMeasurementEnvelope) { value.ValidatorID++ }},
		{name: "uid", mutate: func(value *ReleaseMeasurementEnvelope) { value.ValidatorUID++ }},
		{name: "netuid", mutate: func(value *ReleaseMeasurementEnvelope) { value.Netuid++ }},
		{name: "native epoch", mutate: func(value *ReleaseMeasurementEnvelope) { value.SubnetEpoch++ }},
		{name: "settlement epoch", mutate: func(value *ReleaseMeasurementEnvelope) { value.SettlementEpoch++ }},
		{name: "native block", mutate: func(value *ReleaseMeasurementEnvelope) { value.NativeSnapshotBlock++ }},
		{name: "native hash", mutate: func(value *ReleaseMeasurementEnvelope) { value.NativeSnapshotHash = releaseHex32([32]byte{0x63}) }},
		{name: "evm block", mutate: func(value *ReleaseMeasurementEnvelope) { value.EVMSnapshotBlock++ }},
		{name: "evm hash", mutate: func(value *ReleaseMeasurementEnvelope) { value.EVMSnapshotHash = releaseHex32([32]byte{0x64}) }},
		{name: "policy", mutate: func(value *ReleaseMeasurementEnvelope) { value.PolicyHash = releaseHex32([32]byte{0x65}) }},
		{name: "lineage", mutate: func(value *ReleaseMeasurementEnvelope) {
			value.PreviousArtifactHash = ReleaseMeasurementContentHash([]byte("another actual byte string"))
		}},
		{name: "artifact hash", mutate: func(value *ReleaseMeasurementEnvelope) {
			value.MeasurementArtifactHash = ReleaseMeasurementContentHash([]byte("different bytes"))
		}},
		{name: "artifact size", mutate: func(value *ReleaseMeasurementEnvelope) { value.MeasurementArtifactSize++ }},
		{name: "extrinsic", mutate: func(value *ReleaseMeasurementEnvelope) { value.PreparedExtrinsicHash = releaseHex32([32]byte{0x66}) }},
	} {
		candidate := *original
		change.mutate(&candidate)
		resignReleaseMeasurementEnvelopeV2Test(t, &candidate, fixture.hotkey, ReleaseMeasurementEnvelopeSigningDomainV2)
		options := fixture.options()
		if err := validateReleaseMeasurementEnvelopeV2(t.Context(), &candidate, options.MaxControlBytes); err != nil {
			t.Fatalf("%s is not genuinely self-signed: %v", change.name, err)
		}
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), &candidate, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
		if err == nil || artifact != nil || *reads != 0 {
			t.Fatalf("%s competing decision reached IO: reads=%d error=%v", change.name, *reads, err)
		}
		assertReleaseMeasurementV2Empty(t, verified)
	}
}

// The last operator is independently bound too; a valid first owner cannot
// authorize a missing, different or zero native signer in the complete census.
func TestReleaseMeasurementEnvelopeV2AuthorityRefusesBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	_, envelope := fixture.seal(t)
	for _, change := range []struct {
		name   string
		mutate func(*ReleaseMeasurementV2Options)
	}{
		{name: "zero native signer", mutate: func(options *ReleaseMeasurementV2Options) {
			op := options.Operators[10]
			op.Expected.Activation.Hotkey = [32]byte{}
			options.Operators[10] = op
		}},
		{name: "different native signer", mutate: func(options *ReleaseMeasurementV2Options) {
			op := options.Operators[10]
			op.Expected.Activation.Hotkey = [32]byte{0x77}
			options.Operators[10] = op
		}},
		{name: "uid", mutate: func(options *ReleaseMeasurementV2Options) { options.Expected.SelfUID++ }},
		{name: "missing census", mutate: func(options *ReleaseMeasurementV2Options) { options.Operators = nil }},
	} {
		options := fixture.options()
		change.mutate(&options)
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
		if err == nil || artifact != nil || *reads != 0 {
			t.Fatalf("%s reached IO: reads=%d error=%v", change.name, *reads, err)
		}
		assertReleaseMeasurementV2Empty(t, verified)
		raw, hash, signed, err := SealReleaseMeasurementEnvelopeV2(t.Context(), fixture.measurement, fixture.artifact.SelfUID, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Unix(1, 0), options)
		if err == nil || raw != nil || hash != "" || signed != nil || *reads != 0 {
			t.Fatalf("%s escaped producer admission: %v", change.name, err)
		}
	}
}

// Complete terminal consent cannot be borrowed from the current-window signer.
func TestReleaseMeasurementEnvelopeV2TerminalAuthorityRefusesBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TerminalTestFixture(t, true)
	_, envelope := fixture.seal(t)
	for _, missing := range []bool{false, true} {
		options := fixture.options()
		if missing {
			delete(options.Settlement.Operators, 10)
		} else {
			op := options.Settlement.Operators[10]
			op.Expected.Activation.Hotkey = [32]byte{0x73}
			options.Settlement.Operators[10] = op
		}
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
		if err == nil || artifact != nil || *reads != 0 {
			t.Fatalf("terminal signer omission/substitution reached IO: %v", err)
		}
		assertReleaseMeasurementV2Empty(t, verified)
	}
}

// Both old public entrypoints stay old-only. Neither a relabeled schema nor a
// genuine signature under the other domain is a compact verification fallback.
func TestReleaseMeasurementEnvelopeV2RejectsLegacyAndDomainReplay(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	raw, envelope := fixture.seal(t)
	if value, err := DecodeReleaseMeasurementEnvelope(raw); err == nil || value != nil {
		t.Fatal("legacy decoder accepted compact envelope")
	}
	if _, _, err := VerifyReleaseMeasurementEnvelope(envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash); err == nil {
		t.Fatal("legacy trusted verifier accepted compact evidence")
	}
	if value, hash, signed, err := SealReleaseMeasurementEnvelope(fixture.measurement, fixture.artifact.SelfUID, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Unix(1, 0)); err == nil || value != nil || hash != "" || signed != nil {
		t.Fatal("legacy producer accepted compact evidence")
	}
	legacyBytes, legacyKey, legacyUID, signedAt := testReleaseMeasurementEnvelopeInputs(t)
	legacyRaw, _, legacyEnvelope, err := SealReleaseMeasurementEnvelope(legacyBytes, legacyUID, legacyKey, releaseMeasurementEnvelopeTestPreparedHash, signedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := VerifyReleaseMeasurementEnvelope(legacyEnvelope, legacyBytes, legacyKey.PublicKey(), legacyUID, releaseMeasurementEnvelopeTestPreparedHash); err != nil {
		t.Fatalf("old route regressed: %v", err)
	}
	unsigned := *legacyEnvelope
	unsigned.SigningHash, unsigned.Signature = "", ""
	unsignedBytes, err := json.Marshal(&unsigned)
	if err != nil {
		t.Fatal(err)
	}
	oldDomainBytes := append([]byte("urnetwork/validator/release-measurement-envelope/v1\x00"), unsignedBytes...)
	oldDigest := sha256.Sum256(oldDomainBytes)
	legacyDigest, err := releaseMeasurementEnvelopeSigningDigest(legacyEnvelope)
	if err != nil || legacyDigest != oldDigest {
		t.Fatalf("common envelope factoring changed exact old signing bytes: %v", err)
	}
	compactDigest, err := releaseMeasurementEnvelopeSigningDigestWithDomain(legacyEnvelope, ReleaseMeasurementEnvelopeSigningDomainV2)
	if err != nil || compactDigest == oldDigest {
		t.Fatalf("compact signature domain is not distinct: %v", err)
	}
	if value, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), legacyRaw, fixture.options().MaxControlBytes); err == nil || value != nil {
		t.Fatal("compact decoder accepted legacy envelope")
	}
	for _, version := range []string{ReleaseMeasurementEnvelopeSchema, ReleaseMeasurementEnvelopeSchemaV2} {
		candidate := *envelope
		candidate.Schema = version
		resignReleaseMeasurementEnvelopeV2Test(t, &candidate, fixture.hotkey, ReleaseMeasurementEnvelopeSigningDomain)
		candidateRaw, err := canonicalReleaseMeasurementEnvelopeBytes(&candidate)
		if err != nil {
			t.Fatal(err)
		}
		if value, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), candidateRaw, fixture.options().MaxControlBytes); err == nil || value != nil {
			t.Fatal("legacy signature domain replayed into compact verifier")
		}
	}
}

// Canonical decoding rejects unknown/duplicate/trailing fields and alternate
// whitespace rather than normalizing a different signed byte representation.
func TestReleaseMeasurementEnvelopeV2RejectsNoncanonicalWire(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	raw, _ := fixture.seal(t)
	for _, candidate := range [][]byte{
		bytes.TrimSuffix(raw, []byte{'\n'}), append(bytes.Clone(raw), '\n'),
		append(bytes.Clone(raw), []byte("{}\n")...),
		append([]byte(" "), raw...),
		bytes.Replace(raw, []byte("{\"schema\":"), []byte("{\"unknown\":true,\"schema\":"), 1),
		bytes.Replace(raw, []byte("{\"schema\":"), []byte("{\"schema\":\"duplicate\",\"schema\":"), 1),
	} {
		if bytes.Equal(candidate, raw) {
			t.Fatal("wire mutation did not change the actual encoding")
		}
		if value, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), candidate, fixture.options().MaxControlBytes); err == nil || value != nil {
			t.Fatal("noncanonical compact envelope was accepted")
		}
	}
}

// All wire/control limits remain finite and explicit; no malformed envelope
// may open streams or leak an otherwise valid partially decoded result.
func TestReleaseMeasurementEnvelopeV2BoundsRefuseBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	raw, envelope := fixture.seal(t)
	for _, bound := range []uint64{0, 1, uint64(len(raw) - 1), releaseMeasurementEnvelopeMaxArtifactSize + 1} {
		if value, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), raw, bound); err == nil || value != nil {
			t.Fatalf("wire bound %d was ignored", bound)
		}
	}
	for _, control := range []bool{false, true} {
		options := fixture.options()
		if control {
			options.MaxControlBytes = 1
		} else {
			options.MaxArtifactBytes = uint64(len(fixture.measurement) - 1)
		}
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
		if err == nil || artifact != nil || *reads != 0 {
			t.Fatalf("bounded verification reached streams: %v", err)
		}
		assertReleaseMeasurementV2Empty(t, verified)
		value, hash, signed, err := SealReleaseMeasurementEnvelopeV2(t.Context(), fixture.measurement, fixture.artifact.SelfUID, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Unix(1, 0), options)
		if err == nil || value != nil || hash != "" || signed != nil || *reads != 0 {
			t.Fatalf("bounded seal reached streams: %v", err)
		}
	}
}

// The real proof reader closes first, followed by an injected error-contract
// failure. Producer output stays empty after that late, non-physical failure.
func TestReleaseMeasurementEnvelopeV2SealRetainsLateProofCloseFailure(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	options := fixture.options()
	op := options.Operators[10]
	open := op.Measurement.Replay.OpenData
	failure := errors.New("envelope producer injected post-close failure")
	closes := 0
	op.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &attemptCutV2SealTestClosingReader{ReadCloser: reader, afterClose: func() error { closes++; return failure }}, nil
	}
	options.Operators[10] = op
	raw, hash, envelope, err := SealReleaseMeasurementEnvelopeV2(t.Context(), fixture.measurement, fixture.artifact.SelfUID, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Unix(1, 0), options)
	if !errors.Is(err, failure) || closes == 0 || raw != nil || hash != "" || envelope != nil {
		t.Fatalf("late proof close escaped producer: closes=%d error=%v", closes, err)
	}
}

// Verification reopens actual proofs after prior successful sealing; cached
// header acceptance cannot hide an injected post-close error. The underlying
// real reader is closed first; this is an error-contract, not physical failure.
func TestReleaseMeasurementEnvelopeV2VerifyRetainsLateProofCloseFailure(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	_, envelope := fixture.seal(t)
	options := fixture.options()
	op := options.Operators[10]
	open := op.Measurement.Replay.OpenData
	failure := errors.New("envelope verifier injected post-close failure")
	closes := 0
	op.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &attemptCutV2SealTestClosingReader{ReadCloser: reader, afterClose: func() error { closes++; return failure }}, nil
	}
	options.Operators[10] = op
	artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
	if !errors.Is(err, failure) || closes == 0 || artifact != nil {
		t.Fatalf("late proof close escaped verifier: closes=%d error=%v", closes, err)
	}
	assertReleaseMeasurementV2Empty(t, verified)
}

// Cancellation is delivered at an owned real proof-close boundary, never by
// a sleep or scheduler race; every output must be cleared on that same exit.
func TestReleaseMeasurementEnvelopeV2CancellationAfterProofCloseClearsResults(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	_, envelope := fixture.seal(t)
	for _, producer := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		options := fixture.options()
		op := options.Operators[10]
		open, closes := op.Measurement.Replay.OpenData, 0
		op.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
			reader, err := open(ctx, kind, hash, size)
			if err != nil || kind != AttemptStreamV2Proofs {
				return reader, err
			}
			return &attemptCutV2SealTestClosingReader{ReadCloser: reader, afterClose: func() error { closes++; cancel(); return nil }}, nil
		}
		options.Operators[10] = op
		if producer {
			raw, hash, signed, err := SealReleaseMeasurementEnvelopeV2(ctx, fixture.measurement, fixture.artifact.SelfUID, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Unix(1, 0), options)
			if !errors.Is(err, context.Canceled) || closes == 0 || raw != nil || hash != "" || signed != nil {
				cancel()
				t.Fatalf("producer ignored proof-close cancellation: %v", err)
			}
		} else {
			artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(ctx, envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
			if !errors.Is(err, context.Canceled) || closes == 0 || artifact != nil {
				cancel()
				t.Fatalf("verifier ignored proof-close cancellation: %v", err)
			}
			assertReleaseMeasurementV2Empty(t, verified)
		}
		cancel()
	}
}

// Mutating the caller's envelope, byte slice and authority map inside the first
// real read cannot change the snapshot already admitted by the verifier.
func TestReleaseMeasurementEnvelopeV2OwnsInputsBeforeCallbacks(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	_, envelope := fixture.seal(t)
	measurement := bytes.Clone(fixture.measurement)
	options := fixture.options()
	op := options.Operators[9]
	read := op.Measurement.Replay.ReadMetadata
	mutated := false
	op.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		if !mutated {
			mutated = true
			envelope.NativeSnapshotBlock++
			measurement[len(measurement)/2] ^= 1
			delete(options.Operators, 10)
		}
		return read(ctx, hash, size)
	}
	options.Operators[9] = op
	artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), envelope, measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
	if err != nil || !mutated || artifact == nil || len(verified.ReplayByNO) != 2 || !reflect.DeepEqual(verified.Decision, fixture.want) {
		t.Fatalf("callback changed owned envelope inputs: mutated=%t error=%v", mutated, err)
	}
	if artifact.NativeSnapshotBlock != fixture.artifact.NativeSnapshotBlock {
		t.Fatal("callback changed the returned immutable decision")
	}
}

// Returning changed bytes from a real proof reader still reaches hash and
// policy verification, even after all envelope signatures were accepted.
func TestReleaseMeasurementEnvelopeV2RejectsChangedProofBytes(t *testing.T) {
	t.Parallel()
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	_, envelope := fixture.seal(t)
	options := fixture.options()
	op := options.Operators[10]
	open := op.Measurement.Replay.OpenData
	changed := false
	op.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &releaseMeasurementEnvelopeV2ChangedReader{ReadCloser: reader, changed: &changed}, nil
	}
	options.Operators[10] = op
	artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
	if err == nil || !changed || artifact != nil {
		t.Fatalf("changed actual proof bytes escaped full replay: changed=%t error=%v", changed, err)
	}
	assertReleaseMeasurementV2Empty(t, verified)
}

// The actual stream remains owned and closed by the production verifier.
type releaseMeasurementEnvelopeV2ChangedReader struct {
	io.ReadCloser
	changed *bool
}

// Change exactly one observed proof byte without substituting a replay verdict.
func (self *releaseMeasurementEnvelopeV2ChangedReader) Read(buffer []byte) (int, error) {
	count, err := self.ReadCloser.Read(buffer)
	if count > 0 && !*self.changed {
		buffer[0] ^= 1
		*self.changed = true
	}
	return count, err
}

// The existing independent accounting helper includes pointed-to root storage.
// This measures genuine already-admitted objects without raising their limits.
func releaseMeasurementEnvelopeV2TestControlBytes(t *testing.T, value any) uint64 {
	t.Helper()
	remaining := uint64(releaseMeasurementEnvelopeMaxArtifactSize)
	if err := releaseMeasurementV2ControlStorage(t.Context(), reflect.ValueOf(value), &remaining); err != nil {
		t.Fatal(err)
	}
	return uint64(releaseMeasurementEnvelopeMaxArtifactSize) - remaining
}

// A valid signature does not authorize omitting the fixed root struct from
// the declared control budget. This reaches the actual envelope validator.
func TestReleaseMeasurementEnvelopeV2AdmissionChargesFixedRoot(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ENVELOPE-ADMISSION-v1 PASS TestReleaseMeasurementEnvelopeV2AdmissionChargesFixedRoot")
		}
	})
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	_, envelope := fixture.seal(t)
	stringsOnly := uint64(0)
	value := reflect.ValueOf(*envelope)
	for index := 0; index < value.NumField(); index++ {
		if field := value.Field(index); field.Kind() == reflect.String {
			stringsOnly += uint64(field.Len())
		}
	}
	full := releaseMeasurementEnvelopeV2TestControlBytes(t, envelope)
	if full != stringsOnly+uint64(value.Type().Size()) {
		t.Fatal("flat envelope root census precondition differs")
	}
	if err := validateReleaseMeasurementEnvelopeV2(t.Context(), envelope, stringsOnly); err == nil || !strings.Contains(err.Error(), "control bound") {
		t.Fatalf("actual envelope validator omitted root storage: %v", err)
	}
}

// encoding/json itself is the independent byte-count oracle. Genuine native
// signatures remain valid at the exact wire allowance and refuse one byte
// less, including HTML/control escapes, separators and invalid UTF-8 sizing.
// Invalid UTF-8 is a counter contract only, not a canonical decode claim.
func TestReleaseMeasurementEnvelopeV2AdmissionCountsExactEscapedWire(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ENVELOPE-ADMISSION-v1 PASS TestReleaseMeasurementEnvelopeV2AdmissionCountsExactEscapedWire")
		}
	})
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	_, original := fixture.seal(t)
	for _, sample := range []string{"plain", "\"\\\b\f\n\r\t", "\x00\x01\x0b\x1f", "<>&", "\u2028\u2029", "é🌍�", string([]byte{0xff, 0xc0, 0xaf})} {
		candidate := *original
		candidate.DeploymentID = "prefix" + strings.Repeat(sample, 1024) + "suffix"
		resignReleaseMeasurementEnvelopeV2Test(t, &candidate, fixture.hotkey, ReleaseMeasurementEnvelopeSigningDomainV2)
		raw, err := canonicalReleaseMeasurementEnvelopeBytes(&candidate)
		if err != nil {
			t.Fatal(err)
		}
		wire, controls := uint64(len(raw)), releaseMeasurementEnvelopeV2TestControlBytes(t, &candidate)
		if wire <= controls || wire > fixture.options().MaxControlBytes {
			t.Fatalf("escape wire/control witness is outside existing bounds: wire=%d control=%d", wire, controls)
		}
		if err := validateReleaseMeasurementEnvelopeV2(t.Context(), &candidate, wire); err != nil {
			t.Fatalf("exact encoding/json wire was rejected: %v", err)
		}
		if err := validateReleaseMeasurementEnvelopeV2(t.Context(), &candidate, wire-1); err == nil || !strings.Contains(err.Error(), "wire bound") {
			t.Fatalf("escaped canonical wire escaped its exact byte bound: %v", err)
		}
		if utf8.ValidString(candidate.DeploymentID) {
			decoded, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), raw, wire)
			if err != nil || !reflect.DeepEqual(decoded, &candidate) {
				t.Fatalf("exact valid canonical envelope did not decode: %v", err)
			}
		}
	}
}

// The unsigned preflight reserves actual fixed-width ASCII fields, not a
// fabricated hash or signature. Real signed bytes establish both deltas.
func TestReleaseMeasurementEnvelopeV2AdmissionReservesExactSigningFields(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ENVELOPE-ADMISSION-v1 PASS TestReleaseMeasurementEnvelopeV2AdmissionReservesExactSigningFields")
		}
	})
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	signedRaw, envelope := fixture.seal(t)
	unsigned := *envelope
	unsigned.SigningHash, unsigned.Signature = "", ""
	unsignedRaw, err := canonicalReleaseMeasurementEnvelopeBytes(&unsigned)
	if err != nil {
		t.Fatal(err)
	}
	actualFields := uint64(len(envelope.SigningHash) + len(envelope.Signature))
	if actualFields != 71+130 || uint64(len(signedRaw)-len(unsignedRaw)) != actualFields || releaseMeasurementEnvelopeV2TestControlBytes(t, envelope)-releaseMeasurementEnvelopeV2TestControlBytes(t, &unsigned) != actualFields {
		t.Fatal("fixed signing-field reservation differs from actual signed control and JSON bytes")
	}
	if envelope.SigningHash == "" || envelope.Signature == "" {
		t.Fatal("reservation control did not produce real signature material")
	}
}

// Both ordinary and terminal/current replay succeed at their actual combined
// control/wire minimum and refuse one byte less before any stream callback.
func TestReleaseMeasurementEnvelopeV2AdmissionExactCombinedBudget(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ENVELOPE-ADMISSION-v1 PASS TestReleaseMeasurementEnvelopeV2AdmissionExactCombinedBudget")
		}
	})
	for _, terminal := range []bool{false, true} {
		var fixture *releaseMeasurementEnvelopeV2TestFixture
		if terminal {
			fixture = newReleaseMeasurementEnvelopeV2TerminalTestFixture(t, true)
		} else {
			fixture = newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
		}
		raw, envelope := fixture.seal(t)
		bound := releaseMeasurementEnvelopeV2TestControlBytes(t, fixture.artifact)
		if bound <= max(uint64(len(raw)), releaseMeasurementEnvelopeV2TestControlBytes(t, envelope)) {
			t.Fatal("the real artifact control census must dominate this adjacent unchanged-budget control")
		}
		if bound > fixture.options().MaxControlBytes || bound == 0 {
			t.Fatal("exact fixture budget would increase its original allowance")
		}
		signedAt, err := time.Parse(time.RFC3339Nano, envelope.SignedAt)
		if err != nil {
			t.Fatal(err)
		}
		for _, producer := range []bool{false, true} {
			for _, exact := range []bool{true, false} {
				options := fixture.options()
				options.MaxControlBytes = bound
				if !exact {
					options.MaxControlBytes--
				}
				reads := observeReleaseMeasurementV2SettlementTest(&options)
				if producer {
					encoded, hash, signed, err := SealReleaseMeasurementEnvelopeV2(t.Context(), fixture.measurement, fixture.artifact.SelfUID, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, signedAt, options)
					if exact {
						if err != nil || signed == nil || *reads == 0 || hash != ReleaseMeasurementEnvelopeContentHash(encoded) || uint64(len(encoded)) > bound {
							t.Fatalf("exact producer budget refused real replay: %v", err)
						}
					} else if err == nil || encoded != nil || hash != "" || signed != nil || *reads != 0 {
						t.Fatalf("short producer budget reached readers or outputs: %v", err)
					}
				} else {
					artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
					if exact {
						if err != nil || artifact == nil || verified.Decision == nil || len(verified.ReplayByNO) != 2 || *reads == 0 {
							t.Fatalf("exact verifier budget refused real replay: %v", err)
						}
						if terminal && len(verified.SettlementReplayByNO) != 2 {
							t.Fatal("exact budget omitted real terminal replay")
						}
					} else {
						if err == nil || artifact != nil || *reads != 0 {
							t.Fatalf("short verifier budget reached readers or output: %v", err)
						}
						assertReleaseMeasurementV2Empty(t, verified)
					}
				}
			}
		}
	}
}

// A structurally canonical candidate retains the real M8 streams while an
// escaped outer label exceeds the envelope wire allowance. This is an ordered
// admission negative, not a claim that its changed deployment owns those rows:
// the exact wire refusal must precede that later independent identity check.
func TestReleaseMeasurementEnvelopeV2AdmissionProducerWireBeforeReplay(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ENVELOPE-ADMISSION-v1 PASS TestReleaseMeasurementEnvelopeV2AdmissionProducerWireBeforeReplay")
		}
	})
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	artifact := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	artifact.DeploymentID = strings.Repeat("<&>", 4096)
	measurement, err := canonicalReleaseMeasurementBytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := parseReleaseHex32("actual prepared fixture", releaseMeasurementEnvelopeTestPreparedHash, false)
	if err != nil {
		t.Fatal(err)
	}
	signedAt := time.Unix(1, 0)
	unsigned := newReleaseMeasurementEnvelope(artifact, measurement, fixture.hotkey.PublicKey(), artifact.SelfUID, prepared, signedAt, ReleaseMeasurementEnvelopeSchemaV2)
	unsignedRaw, err := canonicalReleaseMeasurementEnvelopeBytes(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	bound := uint64(len(unsignedRaw)) + 71 + 130 - 1
	options := fixture.options()
	if bound >= options.MaxControlBytes || releaseMeasurementEnvelopeV2TestControlBytes(t, unsigned)+71+130 > bound || releaseMeasurementEnvelopeV2TestControlBytes(t, artifact) > bound {
		t.Fatal("wire amplification must be the first exhausted finite budget")
	}
	options.MaxControlBytes, options.Expected.DeploymentID = bound, artifact.DeploymentID
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	raw, hash, envelope, err := SealReleaseMeasurementEnvelopeV2(t.Context(), measurement, artifact.SelfUID, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, signedAt, options)
	if err == nil || !strings.Contains(err.Error(), "wire bound") || *reads != 0 || raw != nil || hash != "" || envelope != nil {
		t.Fatalf("producer reached identity/replay/signing before envelope wire admission: reads=%d error=%v", *reads, err)
	}
}

// A genuinely re-signed canonical envelope can be valid as a signed object
// yet over its caller's wire allowance. Direct verification must refuse it at
// that admission, not rely on a later different-artifact identity refusal.
func TestReleaseMeasurementEnvelopeV2AdmissionDirectVerifyWireBeforeReplay(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ENVELOPE-ADMISSION-v1 PASS TestReleaseMeasurementEnvelopeV2AdmissionDirectVerifyWireBeforeReplay")
		}
	})
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	_, original := fixture.seal(t)
	envelope := *original
	envelope.DeploymentID = strings.Repeat("<&>", 4096)
	resignReleaseMeasurementEnvelopeV2Test(t, &envelope, fixture.hotkey, ReleaseMeasurementEnvelopeSigningDomainV2)
	raw, err := canonicalReleaseMeasurementEnvelopeBytes(&envelope)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), raw, fixture.options().MaxControlBytes); err != nil || decoded == nil {
		t.Fatalf("wire negative is not genuinely canonical and self-signed: %v", err)
	}
	options := fixture.options()
	options.MaxControlBytes, options.Expected.DeploymentID = uint64(len(raw)-1), envelope.DeploymentID
	if releaseMeasurementEnvelopeV2TestControlBytes(t, &envelope) > options.MaxControlBytes {
		t.Fatal("direct wire witness exhausted control storage first")
	}
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), &envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
	if err == nil || !strings.Contains(err.Error(), "wire bound") || artifact != nil || *reads != 0 {
		t.Fatalf("direct verifier missed pre-replay wire admission: reads=%d error=%v", *reads, err)
	}
	assertReleaseMeasurementV2Empty(t, verified)
}

// Cancellation is already observable when admission starts; no callback or
// partial signed/verified result is permitted on any public boundary.
func TestReleaseMeasurementEnvelopeV2AdmissionCanceledBeforeReaders(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ENVELOPE-ADMISSION-v1 PASS TestReleaseMeasurementEnvelopeV2AdmissionCanceledBeforeReaders")
		}
	})
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	encoded, envelope := fixture.seal(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	options := fixture.options()
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	if value, err := DecodeReleaseMeasurementEnvelopeV2(ctx, encoded, options.MaxControlBytes); !errors.Is(err, context.Canceled) || value != nil {
		t.Fatalf("canceled decoder exposed an envelope: %v", err)
	}
	if raw, hash, signed, err := SealReleaseMeasurementEnvelopeV2(ctx, fixture.measurement, fixture.artifact.SelfUID, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Unix(1, 0), options); !errors.Is(err, context.Canceled) || raw != nil || hash != "" || signed != nil {
		t.Fatalf("canceled producer exposed output: %v", err)
	}
	artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(ctx, envelope, fixture.measurement, fixture.hotkey.PublicKey(), fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, options)
	if !errors.Is(err, context.Canceled) || artifact != nil || *reads != 0 {
		t.Fatalf("canceled verifier reached readers or output: %v", err)
	}
	assertReleaseMeasurementV2Empty(t, verified)
}

// The first real terminal/current metadata callback replaces the caller's
// signer with a different valid native key. Signing must retain its admitted
// owner; invalid initial owners refuse without opening any evidence stream.
func TestReleaseMeasurementEnvelopeV2AdmissionOwnsSignerBeforeCallbacks(t *testing.T) {
	t.Parallel()
	t.Cleanup(func() {
		if !t.Failed() {
			t.Log("ENVELOPE-ADMISSION-v1 PASS TestReleaseMeasurementEnvelopeV2AdmissionOwnsSignerBeforeCallbacks")
		}
	})
	for _, terminal := range []bool{false, true} {
		var fixture *releaseMeasurementEnvelopeV2TestFixture
		if terminal {
			fixture = newReleaseMeasurementEnvelopeV2TerminalTestFixture(t, true)
		} else {
			fixture = newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
		}
		expectedHotkey := fixture.hotkey.PublicKey()
		replacement, err := crv4.KeypairFromSeed([32]byte{0x7b})
		if err != nil {
			t.Fatal(err)
		}
		if replacement.PublicKey() == expectedHotkey {
			t.Fatal("real alternate signer precondition did not change the key")
		}
		options := fixture.options()
		reads := observeReleaseMeasurementV2SettlementTest(&options)
		changed := false
		replaceOnRead := func(replay *AttemptCutV2ReplayOptions) {
			read := replay.ReadMetadata
			replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
				if !changed {
					changed = true
					*fixture.hotkey = *replacement
				}
				return read(ctx, hash, size)
			}
		}
		for noID, operator := range options.Operators {
			replaceOnRead(&operator.Measurement.Replay)
			options.Operators[noID] = operator
		}
		if options.Settlement != nil {
			for noID, operator := range options.Settlement.Operators {
				replaceOnRead(&operator.Measurement.Replay)
				options.Settlement.Operators[noID] = operator
			}
		}
		encoded, hash, envelope, err := SealReleaseMeasurementEnvelopeV2(t.Context(), fixture.measurement, fixture.artifact.SelfUID, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Unix(1, 0), options)
		if err != nil || !changed || *reads == 0 || envelope == nil || envelope.ValidatorHotkey != releaseHex32(expectedHotkey) || hash != ReleaseMeasurementEnvelopeContentHash(encoded) || fixture.hotkey.PublicKey() != replacement.PublicKey() {
			t.Fatalf("callback replaced the admitted real envelope signer: terminal=%t changed=%t reads=%d error=%v", terminal, changed, *reads, err)
		}
		decoded, err := DecodeReleaseMeasurementEnvelopeV2(t.Context(), encoded, fixture.options().MaxControlBytes)
		if err != nil || !reflect.DeepEqual(decoded, envelope) {
			t.Fatalf("owned real signer did not produce canonical authentic bytes: %v", err)
		}
		verifyOptions := fixture.options()
		verifyReads := observeReleaseMeasurementV2SettlementTest(&verifyOptions)
		artifact, verified, err := VerifyReleaseMeasurementEnvelopeV2(t.Context(), decoded, bytes.Clone(fixture.measurement), expectedHotkey, fixture.artifact.SelfUID, releaseMeasurementEnvelopeTestPreparedHash, verifyOptions)
		if err != nil || artifact == nil || verified.Decision == nil || *verifyReads == 0 || len(verified.ReplayByNO) != 2 || terminal && len(verified.SettlementReplayByNO) != 2 {
			t.Fatalf("owned envelope lost fresh full replay: %v", err)
		}
		if !terminal && !reflect.DeepEqual(verified.Decision, fixture.want) {
			t.Fatal("owned signer changed exact genuine M8 scoring")
		}
		for _, invalid := range []struct {
			name   string
			hotkey *crv4.Keypair
		}{
			{name: "nil"}, {name: "zero", hotkey: &crv4.Keypair{}},
			{name: "ring without private signer", hotkey: &crv4.Keypair{Ring: replacement.Ring}},
			{name: "valid but independently unauthorized", hotkey: replacement},
		} {
			invalidOptions := fixture.options()
			invalidReads := observeReleaseMeasurementV2SettlementTest(&invalidOptions)
			raw, hash, signed, err := SealReleaseMeasurementEnvelopeV2(t.Context(), fixture.measurement, fixture.artifact.SelfUID, invalid.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Unix(1, 0), invalidOptions)
			if err == nil || raw != nil || hash != "" || signed != nil || *invalidReads != 0 {
				t.Fatalf("invalid initial %s signer reached readers or outputs: %v", invalid.name, err)
			}
		}
	}
}
