//go:build linux || darwin

package validator

// Real M8 production trails, signed disk records, typed object writers and
// full public replay remain in every nonempty fixture. Only transport storage
// is in memory; no acceptance, quality or complete-record result is substituted.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/urnetwork/connect"
)

// Separate real constructors bind the requested operator before the first
// record exists; changing a signed cut's claimed NoID is not a fixture setup.
func newAttemptSettlementV2TestOperator(t *testing.T, noID uint64, completed, failed int) *attemptCutV2StatsTestFixture {
	t.Helper()
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 0)
	if noID != fixture.expected.Identity.NoID {
		if err := fixture.ledger.Close(); err != nil {
			t.Fatal(err)
		}
		state := newAttemptLedgerDiskTestStateDir(t)
		identity := fixture.expected.Identity
		identity.NoID = noID
		ledger, err := NewDiskAttemptLedger(t.Context(), state, identity, attemptLedgerDiskTestCoordinator, fixture.key, attemptLedgerDiskTestLimits())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ledger.Close() })
		stats := NewStatsEngine(StatsConfig{AMin: fixture.policy.Verify.ReliabilityAMin})
		if err := stats.AdvanceSettlementEpoch(42, state); err != nil {
			t.Fatal(err)
		}
		if err := stats.AttachAttemptLedger(ledger, state); err != nil {
			t.Fatal(err)
		}
		store, err := NewProofStore(state)
		if err != nil {
			t.Fatal(err)
		}
		cfg := fixture.engine.cfg
		cfg.AttemptLedger = ledger
		fixture.engine = NewTrailEngine(fixture.engine.clientId, fixture.key, fixture.server, fixture.engine.keys, fixture.engine.pickSeed, stats, store, fixture.engine.epochFn, cfg)
		fixture.ledger, fixture.expected.Identity = ledger, ledger.identity
	}
	// Independently supplied test activation anchors are deliberately distinct.
	// Their exact values stay pinned in the external options, not normalized to
	// a shared hash or copied from an untrusted candidate by the verifier.
	fixture.expected.Activation.Domain.ActivationHash = sha256.Sum256([]byte(fmt.Sprintf("terminal-test/operator/%d/%s", noID, fixture.expected.Identity.ValidatorVPK)))
	for index := 0; index < completed; index++ {
		proof, err := fixture.engine.RunTrail(t.Context())
		if err != nil || proof == nil || proof.M != 8 || len(proof.Hops) != 8 {
			t.Fatalf("real M8 terminal fixture: %v", err)
		}
	}
	fixture.engine.transport = attemptCutV2SealTestTransport(func(ctx context.Context, hop connect.Id, raw []byte) ([]byte, error) {
		var envelope struct {
			TrailID *connect.Id `json:"trail_id"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, err
		}
		if envelope.TrailID != nil {
			return nil, errors.New("terminal fixture actual extension refusal")
		}
		return fixture.server.PostVerify(ctx, hop, raw)
	})
	for index := 0; index < failed; index++ {
		proof, err := fixture.engine.RunTrail(t.Context())
		if err == nil || proof != nil {
			t.Fatal("failed real terminal produced a proof")
		}
	}
	fixture.engine.transport = fixture.server
	head, err := fixture.ledger.Head()
	if err != nil || head.LastSequence != uint64(8*completed+2*failed) {
		t.Fatalf("real terminal head: %+v %v", head, err)
	}
	if head.LastSequence > 0 {
		if err := fixture.ledger.Walk(t.Context(), 1, head.LastSequence, func(record AttemptRecord) error {
			fixture.recordTs = append(fixture.recordTs, record)
			return VerifyAttemptRecord(&record, fixture.expected.Identity, fixture.key.Public().(ed25519.PublicKey), fixture.server.serverPublicKeys())
		}); err != nil {
			t.Fatal(err)
		}
	}
	measurement := func() ReleaseStatsMeasurement {
		fixture.engine.stats.mu.Lock()
		defer fixture.engine.stats.mu.Unlock()
		return fixture.engine.stats.releaseStatsMeasurementWithLock()
	}()
	sealOptions, _ := newAttemptCutV2SealTestOptions(t, fixture)
	cut, _, err := SealAttemptCutV2(t.Context(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, sealOptions)
	if err != nil || cut == nil {
		t.Fatalf("actual terminal compact sealer: %v", err)
	}
	return &attemptCutV2StatsTestFixture{seal: fixture, cut: *cut, measurement: measurement, metadata: sealOptions.ReadMetadata, data: sealOptions.OpenData}
}

// Exact input census sizes are test budgets; original record/trail/storage
// limits and real policy M8/reliability minimum are unchanged.
func attemptSettlementV2TestOptions(t *testing.T, fixtures ...*attemptCutV2StatsTestFixture) AttemptSettlementV2Options {
	t.Helper()
	options := AttemptSettlementV2Options{Operators: map[uint64]AttemptSettlementV2OperatorOptions{}, MaxParticipants: uint64(len(fixtures)), MaxTransitionBytes: 256 * 1024, MaxClosureBytes: 1024 * 1024}
	for _, fixture := range fixtures {
		options.Operators[fixture.cut.Context.Identity.NoID] = AttemptSettlementV2OperatorOptions{Expected: fixture.cut.Context, Policy: fixture.seal.policy, Bounds: fixture.seal.bounds, Measurement: attemptCutV2MeasurementTestOptions(t, fixture)}
	}
	return options
}

// Every producer operation receives separately owned keys and raw inputs.
func sealAttemptSettlementV2Test(t *testing.T, fixtures ...*attemptCutV2StatsTestFixture) *AttemptSettlementClosureV2 {
	t.Helper()
	inputs := make([]AttemptSettlementV2Input, len(fixtures))
	keys := map[uint64]ed25519.PrivateKey{}
	for index, fixture := range fixtures {
		inputs[index] = AttemptSettlementV2Input{PreFold: fixture.measurement, Cut: fixture.cut}
		keys[fixture.cut.Context.Identity.NoID] = fixture.seal.key
	}
	closure, err := SealAttemptSettlementBatchV2(t.Context(), inputs, keys, attemptSettlementV2TestOptions(t, fixtures...))
	if err != nil || closure == nil {
		t.Fatalf("complete real terminal batch: %v", err)
	}
	return closure
}

// No source mutation is needed to attack a independently signed public copy.
func cloneAttemptSettlementV2Test(t *testing.T, closure *AttemptSettlementClosureV2) *AttemptSettlementClosureV2 {
	t.Helper()
	raw, err := json.Marshal(closure)
	if err != nil {
		t.Fatal(err)
	}
	var result AttemptSettlementClosureV2
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return &result
}

// Re-signing genuine candidate keys cannot bypass the independently supplied
// expected context or complete stream-derived statistics/post-fold census.
func resignAttemptSettlementV2Test(t *testing.T, closure *AttemptSettlementClosureV2, fixtures ...*attemptCutV2StatsTestFixture) {
	t.Helper()
	members, err := attemptSettlementV2Members(closure)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[uint64]ed25519.PrivateKey{}
	for _, fixture := range fixtures {
		keys[fixture.cut.Context.Identity.NoID] = fixture.seal.key
	}
	for _, transition := range closure.Transitions {
		transition.Batch = slices.Clone(members)
		message, err := attemptSettlementTransitionMessageV2(transition)
		if err != nil {
			t.Fatal(err)
		}
		transition.Signature = ed25519.Sign(keys[transition.Identity.NoID], message)
	}
}

// Positive quality is guaranteed at the unchanged AMin8 by106 assignments
// across at most15 providers. The second operator has its own actual key/ledger.
func TestAttemptSettlementV2RealM8CompleteBatchAndDistinctActivations(t *testing.T) {
	first := newAttemptSettlementV2TestOperator(t, 9, 15, 1)
	second := newAttemptSettlementV2TestOperator(t, 10, 1, 0)
	if first.cut.Context.Activation.Domain.ActivationHash == second.cut.Context.Activation.Domain.ActivationHash || first.cut.Context.Identity.ValidatorVPK == second.cut.Context.Identity.ValidatorVPK {
		t.Fatal("fixture lacks distinct real operator identities and expected anchors")
	}
	closure := sealAttemptSettlementV2Test(t, second, first)
	result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, attemptSettlementV2TestOptions(t, first, second))
	if err != nil || len(result.Operators) != 2 || result.Operators[9].Replay.Records.ItemCount != 122 || result.Operators[9].Replay.CompleteCount != 15 || result.Operators[9].Replay.FailedCount != 1 || result.Operators[10].Replay.Records.ItemCount != 8 {
		t.Fatalf("complete real terminal replay: %+v %v", result, err)
	}
	if first.measurement.Config.AMin != 8 || first.seal.policy.Verify.TrailDepth != 8 || len(closure.Transitions[0].PostFold) == 0 {
		t.Fatal("terminal fixture reduced M/minimum or lost actual positive quality")
	}
	if !reflect.DeepEqual(closure.Transitions[0].PreFold, first.measurement) || !reflect.DeepEqual(closure.Transitions[0].Cut, first.cut) {
		t.Fatal("terminal signer replaced pre-fold complete signed evidence")
	}
	for _, transition := range closure.Transitions {
		if transition.ToEpoch != 43 || !slices.Equal(transition.Batch, closure.Transitions[0].Batch) || !slices.Equal(transition.PostFold, attemptSettlementV2Qualities(result.Operators[transition.Identity.NoID].Stats)) {
			t.Fatal("terminal fold or complete signed census differs")
		}
	}
}

// Genuine empty signed cuts require no fabricated rows or proofs; zero-quality
// priors are distinct from absent priors and survive the complete fold.
func TestAttemptSettlementV2EmptySignedWindowRetainsIdleAndZeroPriors(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 0, 0)
	fixture.measurement.Providers = []ReleaseProviderMeasurement{
		{ClientID: connect.Id{1}.String(), HasPriorQuality: true, PriorQualityPPM: 0, LatencyBuckets: make([]uint64, statsLatencyBuckets)},
		{ClientID: connect.Id{2}.String(), HasPriorQuality: true, PriorQualityPPM: 765432, LatencyBuckets: make([]uint64, statsLatencyBuckets)},
	}
	closure := sealAttemptSettlementV2Test(t, fixture)
	result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, attemptSettlementV2TestOptions(t, fixture))
	if err != nil || result.Operators[9].Replay.Records.ItemCount != 0 || len(closure.Transitions[0].PostFold) != 2 || !closure.Transitions[0].PostFold[0].HasQuality || closure.Transitions[0].PostFold[0].QualityPPM != 0 || closure.Transitions[0].PostFold[1].QualityPPM != 765432 {
		t.Fatalf("empty terminal lost exact priors: %v", err)
	}
}

// A real sparse M8 period retains its actual exposure without inventing EMA.
func TestAttemptSettlementV2SparseTerminalDoesNotFabricateQuality(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 1, 1)
	closure := sealAttemptSettlementV2Test(t, fixture)
	result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, attemptSettlementV2TestOptions(t, fixture))
	if err != nil || len(closure.Transitions[0].PostFold) != 0 || result.Operators[9].Replay.Records.ItemCount != 10 {
		t.Fatalf("sparse terminal quality: %v", err)
	}
	var exposure uint64
	for _, provider := range result.Operators[9].Stats.Providers {
		exposure += provider.Exposure
	}
	if exposure != 8 {
		t.Fatalf("sparse actual exposure=%d want8", exposure)
	}
}

// All independently configured members must agree before any object fetch.
func TestAttemptSettlementV2RejectsChangedOrMissingMemberBeforeReads(t *testing.T) {
	first := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	second := newAttemptSettlementV2TestOperator(t, 10, 1, 0)
	closure := sealAttemptSettlementV2Test(t, first, second)
	for _, edit := range []func(*AttemptSettlementClosureV2){
		func(value *AttemptSettlementClosureV2) { value.Transitions = value.Transitions[:1] },
		func(value *AttemptSettlementClosureV2) {
			value.Transitions[0], value.Transitions[1] = value.Transitions[1], value.Transitions[0]
		},
		func(value *AttemptSettlementClosureV2) { value.Transitions[1].Signature[0] ^= 1 },
		func(value *AttemptSettlementClosureV2) { value.Transitions[1].Batch[0].Digest = zeroAttemptHash() },
		func(value *AttemptSettlementClosureV2) { value.Epoch++ },
	} {
		candidate := cloneAttemptSettlementV2Test(t, closure)
		edit(candidate)
		options := attemptSettlementV2TestOptions(t, first, second)
		reads := 0
		for noID, operator := range options.Operators {
			operator.Measurement.Replay.ReadMetadata = func(context.Context, string, uint64) ([]byte, error) {
				reads++
				return nil, errors.New("unexpected admission read")
			}
			options.Operators[noID] = operator
		}
		result, err := VerifyAttemptSettlementClosureV2(t.Context(), candidate, options)
		if err == nil || result.Operators != nil || reads != 0 {
			t.Fatalf("partial/changed member escaped admission: reads=%d error=%v", reads, err)
		}
	}
}

// Complete valid signatures over a changed fold are still not valid evidence.
func TestAttemptSettlementV2ResignedFoldDriftPublishesNoParticipant(t *testing.T) {
	first := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	second := newAttemptSettlementV2TestOperator(t, 10, 1, 0)
	closure := sealAttemptSettlementV2Test(t, first, second)
	closure.Transitions[1].PostFold = []AttemptSettlementQuality{{ClientID: second.measurement.Providers[0].ClientID, HasQuality: true, QualityPPM: 123}}
	resignAttemptSettlementV2Test(t, closure, first, second)
	result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, attemptSettlementV2TestOptions(t, first, second))
	if err == nil || result.Operators != nil {
		t.Fatal("re-signed invented fold published a complete or partial batch")
	}
}

// Exact per-operator hashes stay pinned even though their common fields match.
func TestAttemptSettlementV2RejectsSwappedActivationAndCommonDomainDrift(t *testing.T) {
	first := newAttemptSettlementV2TestOperator(t, 9, 0, 0)
	second := newAttemptSettlementV2TestOperator(t, 10, 0, 0)
	closure := sealAttemptSettlementV2Test(t, first, second)
	for _, edit := range []func(*AttemptSettlementV2OperatorOptions){
		func(operator *AttemptSettlementV2OperatorOptions) {
			operator.Expected.Activation.Domain.ActivationHash = first.cut.Context.Activation.Domain.ActivationHash
		},
		func(operator *AttemptSettlementV2OperatorOptions) {
			operator.Expected.Activation.Domain.ActivationHash[0] ^= 1
		},
		func(operator *AttemptSettlementV2OperatorOptions) {
			operator.Expected.Activation.Domain.SettlementVault[0] ^= 1
		},
		func(operator *AttemptSettlementV2OperatorOptions) { operator.Expected.Activation.Hotkey[0] ^= 1 },
	} {
		options := attemptSettlementV2TestOptions(t, first, second)
		operator := options.Operators[10]
		edit(&operator)
		options.Operators[10] = operator
		result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, options)
		if err == nil || result.Operators != nil {
			t.Fatal("foreign independently expected activation/domain was accepted")
		}
	}
}

// Member digests exclude their final census/signature but those fields remain
// in the signature message; a digest of the signed envelope would form a cycle.
func TestAttemptSettlementV2DigestAndSignatureAvoidHashCycle(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 0, 0)
	closure := sealAttemptSettlementV2Test(t, fixture)
	transition := closure.Transitions[0]
	before, _ := attemptSettlementTransitionDigestV2(transition)
	message, _ := attemptSettlementTransitionMessageV2(transition)
	transition.Batch[0].Digest = zeroAttemptHash()
	transition.Signature[0] ^= 1
	after, _ := attemptSettlementTransitionDigestV2(transition)
	changedMessage, _ := attemptSettlementTransitionMessageV2(transition)
	if before != after || bytes.Equal(message, changedMessage) {
		t.Fatal("digest/signature roles created a cycle or lost member binding")
	}
}

// This helper wraps real object readers without changing their data verdict.
func attemptSettlementV2TestObserveReads(options *AttemptSettlementV2Options, fixture *attemptCutV2StatsTestFixture) map[string]int {
	operator := options.Operators[fixture.cut.Context.Identity.NoID]
	reads := attemptCutV2MeasurementTestObserveReads(fixture, &operator.Measurement.Replay)
	options.Operators[fixture.cut.Context.Identity.NoID] = operator
	return reads
}

// The containing settlement reuses the same full replay, not a second head or
// stats pass over every cut. The private observer observes actual checked rows.
func TestAttemptSettlementV2SharesOnePolicyReplayPerMember(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 2, 1)
	standalone := attemptCutV2MeasurementTestOptions(t, fixture)
	wantReads := attemptCutV2MeasurementTestObserveReads(fixture, &standalone.Replay)
	want, err := VerifyReleaseStatsAndHeadWithAttemptCutV2(t.Context(), fixture.measurement, fixture.cut, fixture.cut.Context, fixture.seal.policy, fixture.seal.bounds, standalone)
	if err != nil {
		t.Fatal(err)
	}
	producerOptions := attemptSettlementV2TestOptions(t, fixture)
	producerReads := attemptSettlementV2TestObserveReads(&producerOptions, fixture)
	closure, err := SealAttemptSettlementBatchV2(t.Context(), []AttemptSettlementV2Input{{PreFold: fixture.measurement, Cut: fixture.cut}}, map[uint64]ed25519.PrivateKey{9: fixture.seal.key}, producerOptions)
	if err != nil || closure == nil || !reflect.DeepEqual(wantReads, producerReads) {
		t.Fatalf("terminal producer duplicated or skipped complete replay: %v", err)
	}
	options := attemptSettlementV2TestOptions(t, fixture)
	reads := attemptSettlementV2TestObserveReads(&options, fixture)
	visited := 0
	result, err := verifyAttemptSettlementClosureV2(t.Context(), closure, options, func(noID uint64, record AttemptRecord) error {
		if noID != 9 || visited >= len(fixture.seal.recordTs) || !reflect.DeepEqual(record, fixture.seal.recordTs[visited]) {
			return errors.New("terminal prefix observer differs")
		}
		visited++
		return nil
	})
	if err != nil || !reflect.DeepEqual(wantReads, reads) || !reflect.DeepEqual(want, result.Operators[9]) || visited != 18 {
		t.Fatalf("terminal replay duplicated/skipped checked work: visits=%d %v", visited, err)
	}
}

// Cancellation after a real Close must discard every already replayed member.
func TestAttemptSettlementV2LateCloseCancellationDiscardsCompleteBatch(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	closure := sealAttemptSettlementV2Test(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	options := attemptSettlementV2TestOptions(t, fixture)
	operator := options.Operators[9]
	closes := 0
	operator.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := fixture.data(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &attemptCutV2MeasurementCancelClose{ReadCloser: reader, cancel: cancel, closes: &closes}, nil
	}
	options.Operators[9] = operator
	result, err := VerifyAttemptSettlementClosureV2(ctx, closure, options)
	if !errors.Is(err, context.Canceled) || closes == 0 || result.Operators != nil {
		t.Fatalf("late terminal cancellation escaped: closes=%d %v", closes, err)
	}
}

// An already canceled operation performs no fetch or scratch creation.
func TestAttemptSettlementV2PreCanceledAdmissionHasNoReplay(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 0, 0)
	closure := sealAttemptSettlementV2Test(t, fixture)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	options := attemptSettlementV2TestOptions(t, fixture)
	reads := attemptSettlementV2TestObserveReads(&options, fixture)
	result, err := VerifyAttemptSettlementClosureV2(ctx, closure, options)
	if !errors.Is(err, context.Canceled) || result.Operators != nil || len(reads) != 0 {
		t.Fatalf("canceled terminal admission read or published: %v", err)
	}
	if _, err := os.Lstat(options.Operators[9].Measurement.Replay.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled terminal admission created scratch: %v", err)
	}
}
