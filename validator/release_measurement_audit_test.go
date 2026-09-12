package validator

import (
	"bytes"
	"math/big"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/crv4"
)

type releaseMeasurementAuditTestInput struct {
	body     []byte
	envelope *ReleaseMeasurementEnvelope
	hotkey   [32]byte
	uid      uint16
}

func releaseMeasurementAuditTestKey(t *testing.T, namespace byte) *crv4.Keypair {
	t.Helper()
	key, err := crv4.KeypairFromSeed([32]byte{namespace})
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// Sign the exact candidate without granting it body validity. Negative tests
// therefore exercise the public verifier after genuine envelope authentication.
func releaseMeasurementAuditTestEncode(t *testing.T, artifact *ReleaseMeasurementArtifact, key *crv4.Keypair) releaseMeasurementAuditTestInput {
	t.Helper()
	body, err := canonicalReleaseMeasurementBytes(artifact)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := parseReleaseHex32("prepared test extrinsic", releaseMeasurementEnvelopeTestPreparedHash, false)
	if err != nil {
		t.Fatal(err)
	}
	envelope := newReleaseMeasurementEnvelope(artifact, body, key.PublicKey(), artifact.SelfUID, prepared, time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC), ReleaseMeasurementEnvelopeSchema)
	resignReleaseMeasurementEnvelope(t, envelope, key)
	return releaseMeasurementAuditTestInput{body: body, envelope: envelope, hotkey: key.PublicKey(), uid: artifact.SelfUID}
}

func releaseMeasurementAuditTestPair(t *testing.T) (*ReleaseMeasurementArtifact, *ReleaseMeasurementArtifact) {
	t.Helper()
	previous := releaseMeasurementTopFixture(t, 2)
	encoded, err := canonicalReleaseMeasurementBytes(previous)
	if err != nil {
		t.Fatal(err)
	}
	current := cloneReleaseMeasurementArtifact(t, previous)
	current.PreviousArtifactHash = ReleaseMeasurementContentHash(encoded)
	return previous, current
}

func releaseMeasurementAuditTestCapture(t *testing.T, audit *ReleaseMeasurementAudit, input releaseMeasurementAuditTestInput) (*ReleaseMeasurementArtifact, *VerifiedReleaseMeasurement, *ReleaseMeasurementSnapshot) {
	t.Helper()
	artifact, decision, snapshot, err := audit.VerifyEnvelope(input.envelope, input.body, input.hotkey, input.uid, releaseMeasurementEnvelopeTestPreparedHash)
	if err != nil || artifact == nil || decision == nil || snapshot == nil {
		t.Fatalf("authenticate real measurement snapshot: %v", err)
	}
	return artifact, decision, snapshot
}

// Two complete signed bodies enter the same operation concurrently. All later
// lineage comparisons reuse those completions and own their mutable outputs.
func TestReleaseMeasurementAuditAuthenticatesEachBodyOnceAndOwnsLineage(t *testing.T) {
	previous, current := releaseMeasurementAuditTestPair(t)
	key := releaseMeasurementAuditTestKey(t, 0x91)
	inputs := []releaseMeasurementAuditTestInput{releaseMeasurementAuditTestEncode(t, previous, key), releaseMeasurementAuditTestEncode(t, current, key)}
	audit := NewReleaseMeasurementAudit()
	var calls atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	verify := func(envelope *ReleaseMeasurementEnvelope, body []byte, hotkey [32]byte, uid uint16, prepared string) (*ReleaseMeasurementArtifact, *VerifiedReleaseMeasurement, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return VerifyReleaseMeasurementEnvelope(envelope, body, hotkey, uid, prepared)
	}
	type result struct {
		artifact *ReleaseMeasurementArtifact
		snapshot *ReleaseMeasurementSnapshot
		err      error
	}
	results := make([]result, 2)
	joined := make(chan int, 2)
	for index, input := range inputs {
		go func() {
			results[index].artifact, _, results[index].snapshot, results[index].err = audit.verifyEnvelope(input.envelope, input.body, input.hotkey, input.uid, releaseMeasurementEnvelopeTestPreparedHash, verify)
			joined <- index
		}()
	}
	<-entered
	<-entered
	close(release)
	<-joined
	<-joined
	for index, result := range results {
		if result.err != nil || result.artifact == nil || result.snapshot == nil {
			t.Fatalf("body %d authentication: %v", index, result.err)
		}
		// Damage every caller-owned lineage container after capture. Strings
		// are immutable, while all slices and cut pointers must be detached.
		inputs[index].body[0] = '!'
		inputs[index].envelope.ValidatorHotkey = releaseHex32([32]byte{0xee})
		result.artifact.DeploymentID = "changed"
		result.artifact.PreviousArtifactHash = "changed"
		result.artifact.HeadEMA[0].Next = RationalJSON{Numerator: "9", Denominator: "1"}
		result.artifact.Inputs[0].Stats.Providers[0].PriorQualityPPM++
		result.artifact.Inputs[0].Stats.AttemptCut.Boundary.EVMBlock++
		result.artifact.Inputs[0].Stats.AttemptCut.Records[0].RecordHash = "changed"
	}
	for range 3 {
		if err := audit.VerifyLineage(results[0].snapshot, results[1].snapshot); err != nil {
			t.Fatalf("owned authenticated lineage was changed or reauthenticated: %v", err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("two completed bodies required %d authentications", calls.Load())
	}
}

// A successful earlier call never turns later changed bytes or expectations
// into trusted inputs, and failure exposes neither a body nor a snapshot.
func TestReleaseMeasurementAuditRejectsChangedBytesAndTrustOptions(t *testing.T) {
	previous, current := releaseMeasurementAuditTestPair(t)
	key := releaseMeasurementAuditTestKey(t, 0x92)
	input := releaseMeasurementAuditTestEncode(t, previous, key)
	audit := NewReleaseMeasurementAudit()
	_, _, accepted := releaseMeasurementAuditTestCapture(t, audit, input)
	for _, mutation := range []string{"bytes", "noncanonical", "hotkey", "uid", "prepared", "envelope", "signed-invalid-body"} {
		candidate := input
		candidate.body = bytes.Clone(input.body)
		envelope := *input.envelope
		candidate.envelope = &envelope
		prepared := releaseMeasurementEnvelopeTestPreparedHash
		switch mutation {
		case "bytes":
			candidate.body[0] = '!'
		case "noncanonical":
			candidate.body = append(candidate.body, '\n')
			candidate.envelope.MeasurementArtifactHash = ReleaseMeasurementContentHash(candidate.body)
			candidate.envelope.MeasurementArtifactSize = uint64(len(candidate.body))
			resignReleaseMeasurementEnvelope(t, candidate.envelope, key)
		case "hotkey":
			candidate.hotkey[0] ^= 1
		case "uid":
			candidate.uid++
		case "prepared":
			prepared = releaseHex32([32]byte{0xef})
		case "envelope":
			candidate.envelope.DeploymentID = "foreign-signed-domain"
			resignReleaseMeasurementEnvelope(t, candidate.envelope, key)
		case "signed-invalid-body":
			broken := cloneReleaseMeasurementArtifact(t, previous)
			broken.Inputs[0].Stats.AttemptCut.Signature[0] ^= 1
			candidate = releaseMeasurementAuditTestEncode(t, broken, key)
		}
		artifact, decision, snapshot, err := audit.VerifyEnvelope(candidate.envelope, candidate.body, candidate.hotkey, candidate.uid, prepared)
		if err == nil || artifact != nil || decision != nil || snapshot != nil {
			t.Fatalf("%s reused a prior authentication or exposed a failed snapshot: %v", mutation, err)
		}
	}
	_, _, next := releaseMeasurementAuditTestCapture(t, audit, releaseMeasurementAuditTestEncode(t, current, key))
	if err := audit.VerifyLineage(accepted, next); err != nil {
		t.Fatalf("rejected inputs poisoned the original completed audit: %v", err)
	}
}

// Even identical bytes authenticated elsewhere cannot supply this invocation's
// completion. A foreign trusted signer cannot be spliced into one lineage.
func TestReleaseMeasurementAuditRejectsCrossInvocationAndAuthorityReuse(t *testing.T) {
	previous, current := releaseMeasurementAuditTestPair(t)
	key := releaseMeasurementAuditTestKey(t, 0x93)
	first, second := NewReleaseMeasurementAudit(), NewReleaseMeasurementAudit()
	priorInput := releaseMeasurementAuditTestEncode(t, previous, key)
	currentInput := releaseMeasurementAuditTestEncode(t, current, key)
	_, _, prior := releaseMeasurementAuditTestCapture(t, first, priorInput)
	_, _, next := releaseMeasurementAuditTestCapture(t, first, currentInput)
	_, _, foreign := releaseMeasurementAuditTestCapture(t, second, currentInput)
	for _, pair := range [][2]*ReleaseMeasurementSnapshot{{nil, next}, {prior, nil}, {prior, foreign}, {&ReleaseMeasurementSnapshot{}, next}} {
		if err := first.VerifyLineage(pair[0], pair[1]); err == nil {
			t.Fatal("absent or foreign audit completion was accepted")
		}
	}
	if err := second.VerifyLineage(prior, next); err == nil {
		t.Fatal("another invocation borrowed both completed snapshots")
	}
	var zero ReleaseMeasurementAudit
	if err := zero.VerifyLineage(prior, next); err == nil {
		t.Fatal("zero-value audit accepted authenticated snapshots")
	}
	if _, _, snapshot, err := zero.VerifyEnvelope(priorInput.envelope, priorInput.body, priorInput.hotkey, priorInput.uid, releaseMeasurementEnvelopeTestPreparedHash); err == nil || snapshot != nil {
		t.Fatal("zero-value audit issued a snapshot")
	}
	otherKey := releaseMeasurementAuditTestKey(t, 0x94)
	_, _, otherAuthority := releaseMeasurementAuditTestCapture(t, first, releaseMeasurementAuditTestEncode(t, current, otherKey))
	if err := first.VerifyLineage(prior, otherAuthority); err == nil || !strings.Contains(err.Error(), "authority") {
		t.Fatalf("different pinned envelope authority was accepted: %v", err)
	}
}

// Every candidate here is independently valid and genuinely signed; only
// its relationship to the prior epoch is invalid. Raw and snapshot callers
// must return the same cross-record failure.
func TestReleaseMeasurementAuditPreservesLineageRejections(t *testing.T) {
	previous, current := releaseMeasurementAuditTestPair(t)
	key := releaseMeasurementAuditTestKey(t, 0x95)
	priorInput := releaseMeasurementAuditTestEncode(t, previous, key)
	for _, mutation := range []string{"content", "identity", "native-gap", "head-reset", "pool-prior", "cut-prefix"} {
		candidate := cloneReleaseMeasurementArtifact(t, current)
		switch mutation {
		case "content":
			candidate.PreviousArtifactHash = ReleaseMeasurementContentHash([]byte("other measurement"))
		case "identity":
			candidate.DeploymentID = "other-deployment"
			attachReleaseMeasurementAttemptCuts(t, candidate)
		case "native-gap":
			candidate.SubnetEpoch += 2
		case "head-reset":
			candidate.SubnetEpoch++
		case "pool-prior":
			changed := false
			for index := range candidate.Inputs[0].Stats.Providers {
				provider := &candidate.Inputs[0].Stats.Providers[index]
				if provider.HasPriorQuality && provider.PriorQualityPPM > 0 {
					provider.PriorQualityPPM--
					changed = true
					break
				}
			}
			if !changed {
				t.Fatal("fixture has no retained pool quality to change")
			}
		case "cut-prefix":
			candidate.Inputs[0].CutEVMSnapshotHash = releaseHex32([32]byte{0xf1})
			attachReleaseMeasurementAttemptCuts(t, candidate)
		}
		input := releaseMeasurementAuditTestEncode(t, candidate, key)
		audit := NewReleaseMeasurementAudit()
		_, _, prior := releaseMeasurementAuditTestCapture(t, audit, priorInput)
		_, _, next := releaseMeasurementAuditTestCapture(t, audit, input)
		want := VerifyReleaseMeasurementLineage(priorInput.body, candidate)
		got := audit.VerifyLineage(prior, next)
		if want == nil || got == nil || got.Error() != want.Error() {
			t.Fatalf("%s lineage verification differs: raw=%v snapshot=%v", mutation, want, got)
		}
	}
}

// The penalty measurement and its following accepted cycle remain linked
// across a native epoch, including the recovered pool's positive decision.
func TestReleaseMeasurementAuditPreservesDishonestDepositRecovery(t *testing.T) {
	previous, current := releaseMeasurementAuditTestPair(t)
	previous.DepositAudits[1].Status = DepositAuditMismatch
	previous.DepositAudits[1].Compliant = false
	previous.DepositAudits[1].Disposition = "zero_pool_weight"
	previous.DepositAudits[1].ObservedDepositRao = "0"
	previous.DepositAudits[1].Error = "observed deposit does not equal the signed-usage requirement"
	key := releaseMeasurementAuditTestKey(t, 0x96)
	priorInput := releaseMeasurementAuditTestEncode(t, previous, key)
	current.PreviousArtifactHash = ReleaseMeasurementContentHash(priorInput.body)
	current.SubnetEpoch++
	alpha := new(big.Rat).SetFrac(new(big.Int).SetUint64(current.Policy.Steering.HeadScoreEMA.Numerator), new(big.Int).SetUint64(current.Policy.Steering.HeadScoreEMA.Denominator))
	oneMinus := new(big.Rat).Sub(big.NewRat(1, 1), alpha)
	for index := range current.HeadEMA {
		record := &current.HeadEMA[index]
		record.HasPrior = true
		record.Prior = previous.HeadEMA[index].Next
		raw, err := decodeRationalJSON(record.Raw)
		if err != nil {
			t.Fatal(err)
		}
		prior, err := decodeRationalJSON(record.Prior)
		if err != nil {
			t.Fatal(err)
		}
		record.Next, err = encodeRationalJSON(new(big.Rat).Add(new(big.Rat).Mul(alpha, raw), new(big.Rat).Mul(oneMinus, prior)))
		if err != nil {
			t.Fatal(err)
		}
	}
	audit := NewReleaseMeasurementAudit()
	_, penalized, prior := releaseMeasurementAuditTestCapture(t, audit, priorInput)
	_, recovered, next := releaseMeasurementAuditTestCapture(t, audit, releaseMeasurementAuditTestEncode(t, current, key))
	if err := audit.VerifyLineage(prior, next); err != nil {
		t.Fatal(err)
	}
	if penalized.Pools[1].Eligible || penalized.Pools[1].Score.Sign() != 0 || !recovered.Pools[1].Eligible || recovered.Pools[1].Score.Sign() <= 0 {
		t.Fatal("authenticated lineage changed dishonest-deposit or recovery decisions")
	}
}
