//go:build linux || darwin

package validator

// Restart/lineage and ownership controls keep the actual terminal signer and
// replay. Negative candidates are re-signed where necessary so their rejection
// cannot be supplied by an unrelated invalid signature or missing fixture.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Empty successor evidence is genuinely sealed from the same persisted head,
// not a fabricated legacy cut or an invented proof. Runtime snapshot promotion
// is outside this fixture: prior EMA is the actual preceding signed fold.
func attemptSettlementV2TestEmptySuccessor(t *testing.T, previous *attemptCutV2StatsTestFixture, transition *AttemptSettlementTransitionV2, generations uint64) *attemptCutV2StatsTestFixture {
	t.Helper()
	context := previous.cut.Context
	context.Boundary.SettlementEpoch = transition.ToEpoch
	context.Boundary.EVMBlock++
	context.Boundary.EVMBlockHash = attemptHex32([32]byte{0x51})
	context.FirstSequence, context.EgressFirstSequence = previous.cut.LastSequence+1, previous.cut.LastSequence+1
	context.EgressGeneration += generations
	context.PriorRoot = previous.cut.Root
	measurement := ReleaseStatsMeasurement{Config: previous.measurement.Config}
	for _, quality := range transition.PostFold {
		measurement.Providers = append(measurement.Providers, ReleaseProviderMeasurement{ClientID: quality.ClientID, HasPriorQuality: true, PriorQualityPPM: quality.QualityPPM, LatencyBuckets: make([]uint64, statsLatencyBuckets)})
	}
	options, _ := newAttemptCutV2SealTestOptions(t, previous.seal)
	cut, _, err := SealAttemptCutV2(t.Context(), previous.seal.ledger, context, previous.seal.policy, previous.seal.key, previous.seal.bounds, options)
	if err != nil || cut == nil || cut.RecordCount != 0 || cut.Root != previous.cut.Root {
		t.Fatalf("real empty successor: %v", err)
	}
	return &attemptCutV2StatsTestFixture{seal: previous.seal, cut: *cut, measurement: measurement, metadata: options.ReadMetadata, data: options.OpenData}
}

// Decode/retry always executes the complete policy verifier against fresh
// scratch; canonical persisted bytes cannot establish a cached verdict.
func TestAttemptSettlementV2CanonicalRestartReplaysExactBytes(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 2, 1)
	closure := sealAttemptSettlementV2Test(t, fixture)
	raw, err := json.Marshal(closure)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	var firstReads map[string]int
	for attempt := 0; attempt < 2; attempt++ {
		options := attemptSettlementV2TestOptions(t, fixture)
		reads := attemptSettlementV2TestObserveReads(&options, fixture)
		decoded, result, err := DecodeAttemptSettlementClosureV2(t.Context(), raw, options)
		if err != nil || !reflect.DeepEqual(decoded, closure) || result.Operators[9].Replay.Records.ItemCount != 18 || len(reads) == 0 {
			t.Fatalf("canonical restart replay: %v", err)
		}
		if attempt == 0 {
			firstReads = reads
		} else if !reflect.DeepEqual(firstReads, reads) {
			t.Fatal("restart reused a verdict or changed full replay work")
		}
	}
}

// Unknown/duplicate/trailing fields and legacy recursive authority fail before
// data access; each canonical positive uses the same actual signed closure.
func TestAttemptSettlementV2DecodeRejectsCompetingAndNoncanonicalWire(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 0, 0)
	closure := sealAttemptSettlementV2Test(t, fixture)
	raw, err := json.Marshal(closure)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	for _, candidate := range [][]byte{
		raw[:len(raw)-1], append(bytes.Clone(raw), '\n'), append(bytes.Clone(raw), []byte("{}\n")...),
		bytes.Replace(raw, []byte(`"pre_fold":{`), []byte(`"pre_fold":{"attempt_cut":{},`), 1),
		bytes.Replace(raw, []byte(`"pre_fold":{`), []byte(`"pre_fold":{"settlement_transition":{},`), 1),
		bytes.Replace(raw, []byte(`"epoch":42`), []byte(`"epoch":42,"epoch":42`), 1),
	} {
		options := attemptSettlementV2TestOptions(t, fixture)
		reads := attemptSettlementV2TestObserveReads(&options, fixture)
		decoded, result, err := DecodeAttemptSettlementClosureV2(t.Context(), candidate, options)
		if err == nil || decoded != nil || result.Operators != nil || len(reads) != 0 {
			t.Fatalf("competing/noncanonical closure accepted or read: %v", err)
		}
	}
}

// A late fold boundary exactly connects the previous complete record root and
// quality census. Later native rotations do not become missing-settlement gaps.
func TestAttemptSettlementV2ConsecutiveClosureLineageAndLaterNativeRotation(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementV2TestOperator(t, 9, 15, 1)
	prior := sealAttemptSettlementV2Test(t, fixture)
	nextFixture := attemptSettlementV2TestEmptySuccessor(t, fixture, prior.Transitions[0], 3)
	next := sealAttemptSettlementV2Test(t, nextFixture)
	result, err := VerifyAttemptSettlementClosureV2Lineage(t.Context(), prior, next, attemptSettlementV2TestOptions(t, fixture), attemptSettlementV2TestOptions(t, nextFixture))
	if err != nil || result.Operators[9].Replay.Records.ItemCount != 0 || !slices.Equal(prior.Transitions[0].PostFold, next.Transitions[0].PostFold) {
		t.Fatalf("consecutive terminal lineage lost retained quality: %v", err)
	}
	if err := verifyAttemptSettlementV2Successor(prior.Transitions[0], nextFixture.measurement, nextFixture.cut, true); err == nil {
		t.Fatal("later native rotation was mistaken for the immediate successor")
	}
	immediateFixture := attemptSettlementV2TestEmptySuccessor(t, fixture, prior.Transitions[0], 1)
	if err := verifyAttemptSettlementV2Successor(prior.Transitions[0], immediateFixture.measurement, immediateFixture.cut, true); err != nil {
		t.Fatalf("exact immediate settlement rotation refused: %v", err)
	}
}

// Each closure is independently valid. Reusing any prior operator's scratch
// for either current operator must fail before the first complete prior trail.
func TestAttemptSettlementV2LineageRejectsCrossWindowScratchBeforeIO(t *testing.T) {
	t.Parallel()
	first := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	second := newAttemptSettlementV2TestOperator(t, 10, 1, 0)
	prior := sealAttemptSettlementV2Test(t, first, second)
	nextFirst := attemptSettlementV2TestEmptySuccessor(t, first, prior.Transitions[0], 1)
	nextSecond := attemptSettlementV2TestEmptySuccessor(t, second, prior.Transitions[1], 1)
	next := sealAttemptSettlementV2Test(t, nextFirst, nextSecond)
	for _, previousNO := range []uint64{9, 10} {
		for _, currentNO := range []uint64{9, 10} {
			previousOptions := attemptSettlementV2TestOptions(t, first, second)
			currentOptions := attemptSettlementV2TestOptions(t, nextFirst, nextSecond)
			operator := currentOptions.Operators[currentNO]
			operator.Measurement.Replay.ScratchDirectory = previousOptions.Operators[previousNO].Measurement.Replay.ScratchDirectory
			currentOptions.Operators[currentNO] = operator
			readKVs := []map[string]int{
				attemptSettlementV2TestObserveReads(&previousOptions, first),
				attemptSettlementV2TestObserveReads(&previousOptions, second),
				attemptSettlementV2TestObserveReads(&currentOptions, nextFirst),
				attemptSettlementV2TestObserveReads(&currentOptions, nextSecond),
			}
			result, err := VerifyAttemptSettlementClosureV2Lineage(t.Context(), prior, next, previousOptions, currentOptions)
			if err == nil || !strings.Contains(err.Error(), "replay namespace") || result.Operators != nil {
				t.Fatalf("prior %d/current %d scratch alias escaped admission: %v", previousNO, currentNO, err)
			}
			for _, reads := range readKVs {
				if len(reads) != 0 {
					t.Fatalf("prior %d/current %d alias read real evidence: %v", previousNO, currentNO, reads)
				}
			}
			for _, options := range []AttemptSettlementV2Options{previousOptions, currentOptions} {
				for _, operator := range options.Operators {
					if _, err := os.Lstat(operator.Measurement.Replay.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("cross-window alias created replay scratch: %v", err)
					}
				}
			}
		}
	}
}

// The actual replay namespace rules reject a filesystem root. Put it on the
// second member so a per-member-only check would already have read the first.
func TestAttemptSettlementV2AdmissionRejectsRootScratchBeforeIO(t *testing.T) {
	t.Parallel()
	first := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	second := newAttemptSettlementV2TestOperator(t, 10, 1, 0)
	closure := sealAttemptSettlementV2Test(t, first, second)
	for _, seal := range []bool{false, true} {
		options := attemptSettlementV2TestOptions(t, first, second)
		scratchTs := []string{options.Operators[9].Measurement.Replay.ScratchDirectory, options.Operators[10].Measurement.Replay.ScratchDirectory}
		operator := options.Operators[10]
		operator.Measurement.Replay.ScratchDirectory = "/"
		options.Operators[10] = operator
		firstReads := attemptSettlementV2TestObserveReads(&options, first)
		secondReads := attemptSettlementV2TestObserveReads(&options, second)
		var err error
		if seal {
			var result *AttemptSettlementClosureV2
			result, err = SealAttemptSettlementBatchV2(t.Context(), []AttemptSettlementV2Input{{PreFold: first.measurement, Cut: first.cut}, {PreFold: second.measurement, Cut: second.cut}}, map[uint64]ed25519.PrivateKey{9: first.seal.key, 10: second.seal.key}, options)
			if result != nil {
				t.Fatal("root namespace published a terminal seal")
			}
		} else {
			var result VerifiedAttemptSettlementV2
			result, err = VerifyAttemptSettlementClosureV2(t.Context(), closure, options)
			if result.Operators != nil {
				t.Fatal("root namespace published terminal replay")
			}
		}
		if err == nil || !strings.Contains(err.Error(), "replay namespaces") || len(firstReads) != 0 || len(secondReads) != 0 {
			t.Fatalf("root namespace passed complete admission, seal=%t: first=%v second=%v error=%v", seal, firstReads, secondReads, err)
		}
		for _, scratch := range scratchTs {
			if _, err := os.Lstat(scratch); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("root namespace created another member's scratch: %v", err)
			}
		}
	}
}

// Current candidate and expected context may agree with each other; genuine
// signatures still cannot break the separately replayed predecessor linkage.
func TestAttemptSettlementV2LineageRejectsResignedPriorEMADrift(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 0, 0)
	fixture.measurement.Providers = []ReleaseProviderMeasurement{{ClientID: "01000000-0000-0000-0000-000000000000", HasPriorQuality: true, PriorQualityPPM: 123456, LatencyBuckets: make([]uint64, statsLatencyBuckets)}}
	prior := sealAttemptSettlementV2Test(t, fixture)
	nextFixture := attemptSettlementV2TestEmptySuccessor(t, fixture, prior.Transitions[0], 1)
	nextFixture.measurement.Providers[0].PriorQualityPPM++
	next := sealAttemptSettlementV2Test(t, nextFixture)
	result, err := VerifyAttemptSettlementClosureV2Lineage(t.Context(), prior, next, attemptSettlementV2TestOptions(t, fixture), attemptSettlementV2TestOptions(t, nextFixture))
	if err == nil || result.Operators != nil {
		t.Fatal("validly signed new window replaced the actual preceding fold")
	}
}

// Candidate-only signatures cannot omit retained providers, regress/reset the
// egress cursor, change same-height hash or skip consecutive settlement epochs.
func TestAttemptSettlementV2SuccessorRejectsCursorBoundaryAndPriorOmission(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 0, 0)
	fixture.measurement.Providers = []ReleaseProviderMeasurement{{ClientID: "01000000-0000-0000-0000-000000000000", HasPriorQuality: true, PriorQualityPPM: 0, LatencyBuckets: make([]uint64, statsLatencyBuckets)}}
	prior := sealAttemptSettlementV2Test(t, fixture)
	next := attemptSettlementV2TestEmptySuccessor(t, fixture, prior.Transitions[0], 1)
	for _, edit := range []func(*ReleaseStatsMeasurement, *AttemptCutV2){
		func(raw *ReleaseStatsMeasurement, cut *AttemptCutV2) { raw.Providers = nil },
		func(raw *ReleaseStatsMeasurement, cut *AttemptCutV2) { raw.Config.AMin++ },
		func(raw *ReleaseStatsMeasurement, cut *AttemptCutV2) { cut.Context.FirstSequence++ },
		func(raw *ReleaseStatsMeasurement, cut *AttemptCutV2) {
			cut.Context.PriorRoot = attemptHex32([32]byte{1})
		},
		func(raw *ReleaseStatsMeasurement, cut *AttemptCutV2) { cut.Context.Boundary.SettlementEpoch++ },
		func(raw *ReleaseStatsMeasurement, cut *AttemptCutV2) {
			cut.Context.Boundary.EVMBlock = prior.Transitions[0].FromBoundary.EVMBlock
		},
		func(raw *ReleaseStatsMeasurement, cut *AttemptCutV2) {
			cut.Context.EgressGeneration = prior.Transitions[0].Cut.Context.EgressGeneration
		},
		func(raw *ReleaseStatsMeasurement, cut *AttemptCutV2) {
			cut.Context.Activation.Domain.ActivationHash[0] ^= 1
		},
	} {
		measurement := cloneAttemptCutV2StatsTestMeasurement(t, next.measurement)
		cut := next.cut
		edit(&measurement, &cut)
		if err := verifyAttemptSettlementV2Successor(prior.Transitions[0], measurement, cut, true); err == nil {
			t.Fatal("terminal successor lost exact fold/cursor/epoch continuity")
		}
	}
}

// Later readers cannot rewrite another member's already admitted authority or
// raw signed fields. A separate invocation must refuse those changed inputs.
func TestAttemptSettlementV2OwnsAllMembersBeforeFirstReader(t *testing.T) {
	first := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	second := newAttemptSettlementV2TestOperator(t, 10, 1, 0)
	closure := sealAttemptSettlementV2Test(t, first, second)
	options := attemptSettlementV2TestOptions(t, first, second)
	firstOptions := options.Operators[9]
	changed := false
	firstOptions.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		if !changed {
			changed = true
			closure.Transitions[1].PreFold.Providers[0].Assignments++
			closure.Transitions[1].Cut.Signature[0] ^= 1
			secondOptions := options.Operators[10]
			secondOptions.Expected.Activation.Domain.ActivationHash[0] ^= 1
			for id, key := range secondOptions.Measurement.Replay.ServerKeys {
				key[0] ^= 1
				secondOptions.Measurement.Replay.ServerKeys[id] = key
			}
			clear(secondOptions.Measurement.CurrentBindingKVs)
			options.Operators[10] = secondOptions
		}
		return first.metadata(ctx, hash, size)
	}
	options.Operators[9] = firstOptions
	result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, options)
	if err != nil || !changed || len(result.Operators) != 2 || result.Operators[10].Replay.Records.ItemCount != 8 {
		t.Fatalf("later candidate/authority mutation reached owned replay: %v", err)
	}
	result, err = VerifyAttemptSettlementClosureV2(t.Context(), closure, options)
	if err == nil || result.Operators != nil {
		t.Fatal("new operation reused an old verdict over changed authority")
	}
}

// Every key is copied/validated before any public callback; signing the second
// member cannot consume a key or candidate altered by the first member's I/O.
func TestAttemptSettlementV2ProducerOwnsKeysAndInputsBeforeReplay(t *testing.T) {
	first := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	second := newAttemptSettlementV2TestOperator(t, 10, 1, 0)
	inputs := []AttemptSettlementV2Input{{PreFold: first.measurement, Cut: first.cut}, {PreFold: second.measurement, Cut: second.cut}}
	keys := map[uint64]ed25519.PrivateKey{9: slices.Clone(first.seal.key), 10: slices.Clone(second.seal.key)}
	options := attemptSettlementV2TestOptions(t, first, second)
	operator := options.Operators[9]
	changed := false
	operator.Measurement.Replay.ReadMetadata = func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		if !changed {
			changed = true
			keys[10][0] ^= 1
			inputs[1].PreFold.Providers[0].Assignments++
			inputs[1].Cut.Signature[0] ^= 1
		}
		return first.metadata(ctx, hash, size)
	}
	options.Operators[9] = operator
	closure, err := SealAttemptSettlementBatchV2(t.Context(), inputs, keys, options)
	if err != nil || closure == nil || !changed {
		t.Fatalf("producer key ownership: %v", err)
	}
	result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, attemptSettlementV2TestOptions(t, first, second))
	if err != nil || len(result.Operators) != 2 {
		t.Fatalf("producer signed mutated instead of owned bytes: %v", err)
	}
}

// The untrusted byte budget is independent of count/stream limits; changing
// limits cannot cause an unbounded copy or a smaller partial accepted batch.
func TestAttemptSettlementV2IndependentMetadataBoundsBeforeReads(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	closure := sealAttemptSettlementV2Test(t, fixture)
	for _, edit := range []func(*AttemptSettlementV2Options){
		func(options *AttemptSettlementV2Options) { options.MaxParticipants = 0 },
		func(options *AttemptSettlementV2Options) { options.MaxTransitionBytes = 1 },
		func(options *AttemptSettlementV2Options) { options.MaxClosureBytes = 1; options.MaxTransitionBytes = 1 },
		func(options *AttemptSettlementV2Options) {
			operator := options.Operators[9]
			operator.Measurement.MaxProviders = 1
			options.Operators[9] = operator
		},
		func(options *AttemptSettlementV2Options) {
			operator := options.Operators[9]
			operator.Measurement.Replay.VisitRecord = func(AttemptRecord) error { return nil }
			options.Operators[9] = operator
		},
	} {
		options := attemptSettlementV2TestOptions(t, fixture)
		reads := attemptSettlementV2TestObserveReads(&options, fixture)
		edit(&options)
		result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, options)
		if err == nil || result.Operators != nil || len(reads) != 0 {
			t.Fatalf("terminal metadata cap read/published: %v", err)
		}
	}
}

// Actual canonical byte lengths, including their newline, define independent
// transition/closure limits; correct signatures do not waive either bound.
func TestAttemptSettlementV2CanonicalMetadataExactBounds(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	closure := sealAttemptSettlementV2Test(t, fixture)
	transitionRaw, err := json.Marshal(closure.Transitions[0])
	if err != nil {
		t.Fatal(err)
	}
	closureRaw, err := json.Marshal(closure)
	if err != nil {
		t.Fatal(err)
	}
	options := attemptSettlementV2TestOptions(t, fixture)
	options.MaxTransitionBytes, options.MaxClosureBytes = uint64(len(transitionRaw))+1, uint64(len(closureRaw))+1
	result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, options)
	if err != nil || len(result.Operators) != 1 {
		t.Fatalf("exact canonical metadata bounds refused genuine evidence: %v", err)
	}
	for _, transitionLimit := range []bool{true, false} {
		options := attemptSettlementV2TestOptions(t, fixture)
		options.MaxTransitionBytes, options.MaxClosureBytes = uint64(len(transitionRaw))+1, uint64(len(closureRaw))+1
		if transitionLimit {
			options.MaxTransitionBytes--
		} else {
			options.MaxClosureBytes--
		}
		reads := attemptSettlementV2TestObserveReads(&options, fixture)
		result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, options)
		if err == nil || result.Operators != nil || len(reads) != 0 {
			t.Fatalf("correct signed bytes waived independent terminal bound: transition=%t error=%v", transitionLimit, err)
		}
	}
}

// Each window independently has a real complete signed census, but a different
// expected operator cannot replace a member in consecutive settlement lineage.
func TestAttemptSettlementV2LineageRejectsGenuineChangedParticipant(t *testing.T) {
	first := newAttemptSettlementV2TestOperator(t, 9, 0, 0)
	second := newAttemptSettlementV2TestOperator(t, 10, 0, 0)
	replacement := newAttemptSettlementV2TestOperator(t, 11, 0, 0)
	prior := sealAttemptSettlementV2Test(t, first, second)
	nextFirst := attemptSettlementV2TestEmptySuccessor(t, first, prior.Transitions[0], 1)
	nextReplacement := attemptSettlementV2TestEmptySuccessor(t, replacement, prior.Transitions[1], 1)
	next := sealAttemptSettlementV2Test(t, nextFirst, nextReplacement)
	valid, err := VerifyAttemptSettlementClosureV2(t.Context(), next, attemptSettlementV2TestOptions(t, nextFirst, nextReplacement))
	if err != nil || len(valid.Operators) != 2 || next.Transitions[1].Identity.NoID != 11 {
		t.Fatalf("replacement fixture lacks its own genuine complete signed census: %v", err)
	}
	priorOptions := attemptSettlementV2TestOptions(t, first, second)
	nextOptions := attemptSettlementV2TestOptions(t, nextFirst, nextReplacement)
	result, err := VerifyAttemptSettlementClosureV2Lineage(t.Context(), prior, next, priorOptions, nextOptions)
	if err == nil || result.Operators != nil {
		t.Fatal("genuine replacement operator was accepted as consecutive complete membership")
	}
	for _, options := range []AttemptSettlementV2Options{priorOptions, nextOptions} {
		for _, operator := range options.Operators {
			if _, err := os.Lstat(operator.Measurement.Replay.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("changed-participant refusal created replay scratch: %v", err)
			}
		}
	}
}

// Genuine proof Close failures cannot publish the otherwise valid first member.
func TestAttemptSettlementV2SecondMemberCloseFailureIsAtomic(t *testing.T) {
	first := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	second := newAttemptSettlementV2TestOperator(t, 10, 1, 0)
	closure := sealAttemptSettlementV2Test(t, first, second)
	options := attemptSettlementV2TestOptions(t, first, second)
	operator := options.Operators[10]
	failure := errors.New("second real terminal proof close failed")
	closes := 0
	operator.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := second.data(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		closes++
		return &attemptCutV2StatsCloseFailure{ReadCloser: reader, failure: failure}, nil
	}
	options.Operators[10] = operator
	result, err := VerifyAttemptSettlementClosureV2(t.Context(), closure, options)
	if !errors.Is(err, failure) || closes == 0 || result.Operators != nil {
		t.Fatalf("second member close exposed first accepted member: %v", err)
	}
}

// Invalid seed/public halves are refused before even an empty-cut replay can
// reserve scratch. The fixture keeps its real key and source evidence intact.
func TestAttemptSettlementV2RejectsInconsistentSigningKeyBeforeIO(t *testing.T) {
	fixture := newAttemptSettlementV2TestOperator(t, 9, 1, 0)
	key := slices.Clone(fixture.seal.key)
	key[0] ^= 1
	options := attemptSettlementV2TestOptions(t, fixture)
	reads := attemptSettlementV2TestObserveReads(&options, fixture)
	closure, err := SealAttemptSettlementBatchV2(t.Context(), []AttemptSettlementV2Input{{PreFold: fixture.measurement, Cut: fixture.cut}}, map[uint64]ed25519.PrivateKey{9: key}, options)
	if err == nil || closure != nil || len(reads) != 0 {
		t.Fatalf("invalid key crossed terminal admission: %v", err)
	}
	if _, err := os.Lstat(options.Operators[9].Measurement.Replay.ScratchDirectory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid key allocated replay scratch: %v", err)
	}
}
