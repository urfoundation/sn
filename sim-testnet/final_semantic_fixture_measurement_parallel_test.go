// Real measurement/record workers retain full authentication, deterministic
// bounded joins and caller ownership. No test prewarms a separate fixture.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/ss58"
	validatorpkg "github.com/urfoundation/sn/validator"
)

// Reauthenticate selected real wire inputs for a direct worker control. Ten
// inputs are used by complete-join controls; single-worker controls select one.
// The underlying cold graph and all its original populations are always full.
func finalSemanticFixtureMeasurementTestInputs(t *testing.T, count int) ([]finalSemanticFixtureMeasurementJob, []*validatorpkg.ReleaseMeasurementArtifact) {
	t.Helper()
	if count != 1 && count != 10 {
		t.Fatal("measurement worker control requires one or all ten real inputs")
	}
	source, artifacts := finalSemanticFixture(t)
	jobs := make([]finalSemanticFixtureMeasurementJob, 0, 10)
	for _, validator := range source.Validators {
		hotkey, _, err := ss58.Decode(validator.Hotkey)
		if err != nil {
			t.Fatal(err)
		}
		for _, cycle := range validator.Cycles {
			var seed [32]byte
			seed[0], seed[31] = byte(0x60+validator.ValidatorID), byte(0x70+validator.ValidatorID)
			jobs = append(jobs, finalSemanticFixtureMeasurementJob{validatorID: validator.ValidatorID, cycle: cycle, measurementBytes: append([]byte(nil), artifacts[cycle.MeasurementArtifact.URI]...), measurementHash: cycle.MeasurementArtifact.ContentHash, policyHash: source.PolicyHash, hotkeySeed: seed, expectedHotkey: hotkey})
		}
	}
	if len(jobs) != 10 || source.ExpectedMiners != 1000 || source.ExpectedCandidates != 202 || source.ExpectedHeadSlots != 200 {
		t.Fatal("measurement worker control lost the complete fixture")
	}
	jobs = jobs[:count]
	measurements := make([]*validatorpkg.ReleaseMeasurementArtifact, count)
	work := finalSemanticFixtureWorkControl{ctx: t.Context()}
	if err := work.run("measurement-control-inputs", count, 4, func(ctx context.Context, index int) error {
		measurement, verified, err := validatorpkg.DecodeReleaseMeasurementArtifact(jobs[index].measurementBytes)
		if err != nil {
			return err
		}
		if measurement.ValidatorID != jobs[index].validatorID || measurement.SettlementEpoch != jobs[index].cycle.SettlementEpoch {
			return errors.New("measurement control identity differs from its real cycle")
		}
		jobs[index].verified = verified
		measurements[index] = measurement
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return jobs, measurements
}

// Read detached actual-body counts without rebuilding any signed artifact.
func finalSemanticFixtureMeasurementTestStages(t *testing.T) map[finalSemanticFixtureMeasurementOwner]finalSemanticFixtureStageObservation {
	t.Helper()
	_, _ = finalSemanticFixture(t)
	finalSemanticFixtureCache.stateLock.Lock()
	defer finalSemanticFixtureCache.stateLock.Unlock()
	result := make(map[finalSemanticFixtureMeasurementOwner]finalSemanticFixtureStageObservation, len(finalSemanticFixtureCache.measurementStages))
	for key, value := range finalSemanticFixtureCache.measurementStages {
		result[key] = value
	}
	return result
}

// The first four owners really enter the public envelope seal together.
func TestFinalSemanticFixtureEnvelopeBodiesOverlap(t *testing.T) {
	t.Parallel()
	stages := finalSemanticFixtureMeasurementTestStages(t)
	key := finalSemanticFixtureMeasurementOwner{stage: finalSemanticFixtureEnvelopeStart}
	value := stages[key]
	if len(stages) != 11 || value.entered != 10 || value.completed != 10 || value.active != 0 || value.maximum != 4 {
		t.Fatalf("actual envelope bodies are not bounded/joined: scopes=%d value=%+v", len(stages), value)
	}
	stages[key] = finalSemanticFixtureStageObservation{}
	if finalSemanticFixtureMeasurementTestStages(t)[key] != value {
		t.Fatal("envelope stage reader changed the published audit")
	}
}

// Every validator/epoch has its own two real operator record owners; no
// unrelated epoch can satisfy another epoch's overlap barrier or census.
func TestFinalSemanticFixtureOperatorBodiesOverlap(t *testing.T) {
	t.Parallel()
	stages := finalSemanticFixtureMeasurementTestStages(t)
	if len(stages) != 11 {
		t.Fatalf("measurement owner scopes=%d, want11", len(stages))
	}
	for validatorID := uint64(1); validatorID <= 2; validatorID++ {
		for epoch := uint64(10); epoch <= 14; epoch++ {
			key := finalSemanticFixtureMeasurementOwner{stage: finalSemanticFixtureOperatorBody, validatorID: validatorID, settlementEpoch: epoch}
			value := stages[key]
			if value.entered != 2 || value.completed != 2 || value.active != 0 || value.maximum != 2 {
				t.Errorf("actual operator bodies %v=%+v, want two joined overlapping owners", key, value)
			}
		}
	}
}

// Genuine bad record signatures reach both independent public envelope
// decoders. Later slot3 exits first; the joined error must still name slot1.
func TestFinalSemanticFixtureEnvelopeFailureJoinsCanonicalOwners(t *testing.T) {
	t.Parallel()
	jobs, measurements := finalSemanticFixtureMeasurementTestInputs(t, 10)
	for _, index := range []int{1, 3} {
		record := &measurements[index].Inputs[0].Stats.AttemptCut.Records[0]
		if len(record.Signature) != ed25519.SignatureSize {
			t.Fatal("real signed measurement lacks its record signature")
		}
		record.Signature[0] ^= 1
		encoded, err := json.Marshal(measurements[index])
		if err != nil {
			t.Fatal(err)
		}
		jobs[index].measurementBytes = append(encoded, '\n')
		jobs[index].measurementHash = validatorpkg.ReleaseMeasurementContentHash(jobs[index].measurementBytes)
	}
	enteredAll, laterFinished := make(chan struct{}), make(chan struct{})
	var entered, completed, publicCalls atomic.Int32
	work := finalSemanticFixtureWorkControl{ctx: t.Context(), measurementObserved: func(event finalSemanticFixtureMeasurementWorkEvent) {
		if event.stage == finalSemanticFixtureEnvelopeStart {
			publicCalls.Add(1)
		}
	}, measurementEntered: func(ctx context.Context, event finalSemanticFixtureMeasurementWorkEvent) (func(), error) {
		if event.stage != finalSemanticFixtureEnvelopeStart || event.validatorID != 1 || event.settlementEpoch < 10 || event.settlementEpoch > 13 {
			return nil, fmt.Errorf("unexpected admitted envelope owner: %+v", event)
		}
		if entered.Add(1) == 4 {
			close(enteredAll)
		}
		select {
		case <-enteredAll:
		case <-ctx.Done():
			completed.Add(1)
			return nil, ctx.Err()
		}
		if event.settlementEpoch == 13 {
			return func() { completed.Add(1); close(laterFinished) }, nil
		}
		<-laterFinished
		if event.settlementEpoch == 11 {
			return func() { completed.Add(1) }, nil
		}
		completed.Add(1)
		return nil, context.Canceled
	}}
	outputs, err := completeFinalSemanticFixtureMeasurements(jobs, work)
	if err == nil || !strings.Contains(err.Error(), "job 1:") || !strings.Contains(err.Error(), "attempt record validator signature is invalid") || outputs != nil || entered.Load() != 4 || completed.Load() != 4 || publicCalls.Load() != 2 {
		t.Fatalf("real envelope failure lost canonical join: error=%v outputs=%d entered=%d joined=%d public_calls=%d", err, len(outputs), entered.Load(), completed.Load(), publicCalls.Load())
	}
}

// Cancellation after four actual full seals have completed still prevents
// any completion result from escaping or a second wave from being admitted.
func TestFinalSemanticFixtureEnvelopeCancellationAfterFullSealJoins(t *testing.T) {
	t.Parallel()
	jobs, _ := finalSemanticFixtureMeasurementTestInputs(t, 10)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reached := make(chan struct{})
	var entered, completed, publicEnds atomic.Int32
	work := finalSemanticFixtureWorkControl{ctx: ctx, measurementObserved: func(event finalSemanticFixtureMeasurementWorkEvent) {
		if event.stage == finalSemanticFixtureEnvelopeEnd {
			publicEnds.Add(1)
		}
	}, measurementEntered: func(ctx context.Context, event finalSemanticFixtureMeasurementWorkEvent) (func(), error) {
		if event.stage != finalSemanticFixtureEnvelopeStart {
			return nil, errors.New("unexpected measurement stage")
		}
		if entered.Add(1) == 4 {
			close(reached)
		}
		select {
		case <-reached:
		case <-ctx.Done():
			completed.Add(1)
			return nil, ctx.Err()
		}
		return func() {
			if completed.Add(1) == 4 {
				cancel()
			}
		}, nil
	}}
	outputs, err := completeFinalSemanticFixtureMeasurements(jobs, work)
	if !errors.Is(err, context.Canceled) || outputs != nil || entered.Load() != 4 || completed.Load() != 4 || publicEnds.Load() != 4 {
		t.Fatalf("post-seal cancellation published or leaked owners: error=%v outputs=%d entered=%d joined=%d full_ends=%d", err, len(outputs), entered.Load(), completed.Load(), publicEnds.Load())
	}
}

// Invalid census/context must fail before any real signing body can enter.
func TestFinalSemanticFixtureEnvelopeAdmissionRejectsMissingOwners(t *testing.T) {
	t.Parallel()
	var entries atomic.Int32
	work := finalSemanticFixtureWorkControl{ctx: t.Context(), measurementEntered: func(context.Context, finalSemanticFixtureMeasurementWorkEvent) (func(), error) {
		entries.Add(1)
		return func() {}, nil
	}}
	for _, count := range []int{0, 1, 9, 11} {
		if output, err := completeFinalSemanticFixtureMeasurements(make([]finalSemanticFixtureMeasurementJob, count), work); err == nil || output != nil {
			t.Fatalf("missing/extra completion owners=%d admitted: %v", count, err)
		}
	}
	jobs := make([]finalSemanticFixtureMeasurementJob, 10)
	for index := range jobs {
		jobs[index].validatorID = uint64(index/5 + 1)
		jobs[index].cycle.SettlementEpoch = uint64(index%5 + 10)
	}
	jobs[9] = jobs[8]
	if output, err := completeFinalSemanticFixtureMeasurements(jobs, work); err == nil || output != nil || entries.Load() != 0 {
		t.Fatalf("duplicate completion owner admitted: error=%v entries=%d", err, entries.Load())
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	work.ctx = ctx
	if output, err := completeFinalSemanticFixtureMeasurements(jobs, work); !errors.Is(err, context.Canceled) || output != nil || entries.Load() != 0 {
		t.Fatalf("canceled completion admitted work: error=%v entries=%d", err, entries.Load())
	}
}

// A separately derived real key seals an unchanged owned input; its public
// verifier authenticates the fresh result without comparing randomized bytes.
func TestFinalSemanticFixtureEnvelopeCompletionOwnsInputsAndKey(t *testing.T) {
	t.Parallel()
	jobs, _ := finalSemanticFixtureMeasurementTestInputs(t, 1)
	job := jobs[0]
	before, err := json.Marshal(job.cycle)
	if err != nil {
		t.Fatal(err)
	}
	measurementBefore := append([]byte(nil), job.measurementBytes...)
	scoresBefore := make([]string, len(job.verified.Scores))
	for index, score := range job.verified.Scores {
		scoresBefore[index] = score.RatString()
	}
	output, err := completeFinalSemanticFixtureMeasurement(job, finalSemanticFixtureWorkControl{ctx: t.Context()})
	if err != nil {
		t.Fatal(err)
	}
	encoded := output.artifacts.values[output.cycle.MeasurementEnvelope.URI].data
	envelope, err := validatorpkg.DecodeReleaseMeasurementEnvelope(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := validatorpkg.VerifyReleaseMeasurementEnvelope(envelope, measurementBefore, job.expectedHotkey, 12, output.cycle.Commit.ExtrinsicHash); err != nil {
		t.Fatalf("fresh owned envelope failed its complete public verifier: %v", err)
	}
	after, err := json.Marshal(job.cycle)
	if err != nil || !bytes.Equal(before, after) || !bytes.Equal(measurementBefore, job.measurementBytes) {
		t.Fatalf("completion mutated caller input: %v", err)
	}
	for index, score := range job.verified.Scores {
		if score.RatString() != scoresBefore[index] {
			t.Fatal("completion mutated its read-only authenticated score")
		}
	}
	job.hotkeySeed[0] ^= 1
	if rejected, err := completeFinalSemanticFixtureMeasurement(job, finalSemanticFixtureWorkControl{ctx: t.Context()}); err == nil || len(rejected.artifacts.values) != 0 || !strings.Contains(err.Error(), "hotkey identity differs") {
		t.Fatalf("another genuine hotkey replaced the owned validator: %v", err)
	}
}

// A valid signature over a different owner context cannot publish a cycle
// under the original identity, even though full envelope sealing succeeds.
func TestFinalSemanticFixtureEnvelopeCompletionRejectsContextSubstitution(t *testing.T) {
	t.Parallel()
	jobs, _ := finalSemanticFixtureMeasurementTestInputs(t, 1)
	job := jobs[0]
	job.policyHash = finalLineageWorkChangedHash(t, job.policyHash)
	var fullEnds atomic.Int32
	work := finalSemanticFixtureWorkControl{ctx: t.Context(), measurementObserved: func(event finalSemanticFixtureMeasurementWorkEvent) {
		if event.stage == finalSemanticFixtureEnvelopeEnd {
			fullEnds.Add(1)
		}
	}}
	output, err := completeFinalSemanticFixtureMeasurement(job, work)
	if err == nil || !strings.Contains(err.Error(), "owned measurement context") || len(output.artifacts.values) != 0 || fullEnds.Load() != 1 {
		t.Fatalf("context substitution escaped its full sealed join: error=%v full_ends=%d", err, fullEnds.Load())
	}
}

// Real first-epoch inputs keep every provider and genuinely signed record.
// The pending input is the same graph with only its future cut slots cleared.
func finalSemanticFixtureOperatorTestInput(t *testing.T) (*validatorpkg.ReleaseMeasurementArtifact, ed25519.PrivateKey, []ed25519.PrivateKey) {
	t.Helper()
	_, measurements := finalSemanticFixtureMeasurementTestInputs(t, 1)
	measurement := measurements[0]
	if len(measurement.Inputs) != 2 || len(measurement.Inputs[0].Stats.Providers)+len(measurement.Inputs[1].Stats.Providers) != 1000 {
		t.Fatal("operator worker control lost its full provider census")
	}
	for index := range measurement.Inputs {
		measurement.Inputs[index].Stats.AttemptCut = nil
		measurement.Inputs[index].Stats.SettlementTransition = nil
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize))
	servers := []ed25519.PrivateKey{ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize)), ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, ed25519.SeedSize))}
	return measurement, key, servers
}

// Operator1 completes its entire real record/cut body before operator2's
// invalid egress fails. Neither staged counters nor append tails may escape.
func TestFinalSemanticFixtureOperatorFailureJoinsWithoutPublication(t *testing.T) {
	t.Parallel()
	measurement, key, servers := finalSemanticFixtureOperatorTestInput(t)
	for index := range measurement.Inputs[1].Stats.Providers {
		provider := &measurement.Inputs[1].Stats.Providers[index]
		if len(provider.EgressIPHashHexes) != 0 {
			provider.EgressIPHashHexes[0] = "invalid-egress"
			break
		}
	}
	before, err := json.Marshal(measurement)
	if err != nil {
		t.Fatal(err)
	}
	ledgers := map[uint64]*finalAttemptFixtureLedger{}
	oneFinished := make(chan struct{})
	var entered, completed atomic.Int32
	work := finalSemanticFixtureWorkControl{ctx: t.Context(), measurementEntered: func(ctx context.Context, event finalSemanticFixtureMeasurementWorkEvent) (func(), error) {
		entered.Add(1)
		if event.noID == 1 {
			return func() { completed.Add(1); close(oneFinished) }, nil
		}
		select {
		case <-oneFinished:
		case <-ctx.Done():
			completed.Add(1)
			return nil, ctx.Err()
		}
		return func() { completed.Add(1) }, nil
	}}
	err = attachFinalAttemptCutsResultWithWork(measurement, key, servers, ledgers, nil, work)
	after, marshalErr := json.Marshal(measurement)
	if err == nil || !strings.Contains(err.Error(), "invalid fixture egress") || marshalErr != nil || !bytes.Equal(before, after) || len(ledgers) != 0 || entered.Load() != 2 || completed.Load() != 2 {
		t.Fatalf("operator failure published or leaked owners: error=%v marshal=%v ledgers=%d entered=%d joined=%d", err, marshalErr, len(ledgers), entered.Load(), completed.Load())
	}
}

// Both real operator cuts finish before cancellation, exercising the caller's
// final joined cancellation check rather than an early skipped body.
func TestFinalSemanticFixtureOperatorCancellationAfterCutJoins(t *testing.T) {
	t.Parallel()
	measurement, key, servers := finalSemanticFixtureOperatorTestInput(t)
	before, err := json.Marshal(measurement)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var entered, completed atomic.Int32
	work := finalSemanticFixtureWorkControl{ctx: ctx, measurementEntered: func(context.Context, finalSemanticFixtureMeasurementWorkEvent) (func(), error) {
		entered.Add(1)
		return func() {
			if completed.Add(1) == 2 {
				cancel()
			}
		}, nil
	}}
	ledgers := map[uint64]*finalAttemptFixtureLedger{}
	err = attachFinalAttemptCutsResultWithWork(measurement, key, servers, ledgers, nil, work)
	after, marshalErr := json.Marshal(measurement)
	if !errors.Is(err, context.Canceled) || marshalErr != nil || !bytes.Equal(before, after) || len(ledgers) != 0 || entered.Load() != 2 || completed.Load() != 2 {
		t.Fatalf("post-cut cancellation published or leaked: error=%v marshal=%v ledgers=%d entered=%d joined=%d", err, marshalErr, len(ledgers), entered.Load(), completed.Load())
	}
}

// A failed joined update cannot overwrite an existing ledger's unadmitted
// backing tail. The sentinel is an actual signed fixture record, not a proof
// admitted to the empty chain. Corrupt stored signing ownership also fails.
func TestFinalSemanticFixtureOperatorFailurePreservesExistingLedgerTail(t *testing.T) {
	t.Parallel()
	_, measurements := finalSemanticFixtureMeasurementTestInputs(t, 1)
	measurement := measurements[0]
	cut := measurement.Inputs[0].Stats.AttemptCut
	if cut == nil || len(cut.Records) == 0 {
		t.Fatal("real operator source lacks its signed tail sentinel")
	}
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize))
	servers := []ed25519.PrivateKey{ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize)), ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, ed25519.SeedSize))}
	ledger, err := newFinalAttemptFixtureLedger(cut.Identity, key)
	if err != nil {
		t.Fatal(err)
	}
	ledger.records = make([]validatorpkg.AttemptRecord, 0, 1)
	ledger.records[:1][0] = cut.Records[0]
	tailBefore, err := json.Marshal(ledger.records[:1])
	if err != nil {
		t.Fatal(err)
	}
	for index := range measurement.Inputs {
		measurement.Inputs[index].Stats.AttemptCut = nil
		measurement.Inputs[index].Stats.SettlementTransition = nil
	}
	changed := false
	for index := range measurement.Inputs[1].Stats.Providers {
		provider := &measurement.Inputs[1].Stats.Providers[index]
		if len(provider.EgressIPHashHexes) != 0 {
			provider.EgressIPHashHexes[0] = "invalid-egress"
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("real operator2 fixture has no egress claim")
	}
	oneFinished := make(chan struct{})
	var joined atomic.Int32
	work := finalSemanticFixtureWorkControl{ctx: t.Context(), measurementEntered: func(ctx context.Context, event finalSemanticFixtureMeasurementWorkEvent) (func(), error) {
		if event.noID == 1 {
			return func() { joined.Add(1); close(oneFinished) }, nil
		}
		select {
		case <-oneFinished:
		case <-ctx.Done():
			joined.Add(1)
			return nil, ctx.Err()
		}
		return func() { joined.Add(1) }, nil
	}}
	ledgers := map[uint64]*finalAttemptFixtureLedger{1: ledger}
	err = attachFinalAttemptCutsResultWithWork(measurement, key, servers, ledgers, nil, work)
	tailAfter, marshalErr := json.Marshal(ledger.records[:1])
	if err == nil || !strings.Contains(err.Error(), "invalid fixture egress") || marshalErr != nil || len(ledger.records) != 0 || len(ledgers) != 1 || ledgers[1] != ledger || joined.Load() != 2 || !bytes.Equal(tailBefore, tailAfter) || !bytes.Equal(ledger.validatorKey, key) {
		t.Fatalf("failed operator work rewrote existing private state: error=%v marshal=%v records=%d ledgers=%d joined=%d", err, marshalErr, len(ledger.records), len(ledgers), joined.Load())
	}
	ledger.validatorKey[0] ^= 1
	var entered atomic.Int32
	work.measurementEntered = func(context.Context, finalSemanticFixtureMeasurementWorkEvent) (func(), error) {
		entered.Add(1)
		return func() {}, nil
	}
	if err := attachFinalAttemptCutsResultWithWork(measurement, key, servers, ledgers, nil, work); err == nil || !strings.Contains(err.Error(), "ledger identity changed") || entered.Load() != 0 {
		t.Fatalf("corrupt stored signing ownership was silently repaired: error=%v entries=%d", err, entered.Load())
	}
}

// The two mutable provider slices can initially alias without giving workers
// shared writers. Nil and empty wire representations must remain distinct.
func TestFinalSemanticFixtureOperatorInputCopiesPreserveWireOwnership(t *testing.T) {
	t.Parallel()
	for _, provider := range []validatorpkg.ReleaseProviderMeasurement{
		{ClientID: "nil"},
		{ClientID: "empty", LatencyBuckets: []uint64{}, EgressIPHashHexes: []string{}},
		{ClientID: "populated", LatencyBuckets: []uint64{1, 2}, EgressIPHashHexes: []string{"one", "two"}},
	} {
		input := validatorpkg.ReleaseMeasurementInput{Stats: validatorpkg.ReleaseStatsMeasurement{Providers: []validatorpkg.ReleaseProviderMeasurement{provider}}}
		before, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		first, second := cloneFinalSemanticFixtureMeasurementInput(input), cloneFinalSemanticFixtureMeasurementInput(input)
		if !reflect.DeepEqual(first, input) || !reflect.DeepEqual(second, input) {
			t.Fatal("provider copy changed nil/empty or actual wire values")
		}
		first.Stats.Providers[0].ClientID = "changed"
		if len(first.Stats.Providers[0].LatencyBuckets) != 0 {
			first.Stats.Providers[0].LatencyBuckets[0]++
			first.Stats.Providers[0].EgressIPHashHexes[0] = "changed"
		}
		after, err := json.Marshal(input)
		if err != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(second, input) {
			t.Fatalf("provider copies share mutable ownership: %v", err)
		}
	}
}

// Canceled input and duplicate operator ownership fail before body admission
// and cannot leave even an empty new ledger inserted in the caller's map.
func TestFinalSemanticFixtureOperatorAdmissionRejectsAmbiguousOwners(t *testing.T) {
	t.Parallel()
	measurement, key, servers := finalSemanticFixtureOperatorTestInput(t)
	measurement.Inputs[1].NoID = measurement.Inputs[0].NoID
	ledgers := map[uint64]*finalAttemptFixtureLedger{}
	var entered atomic.Int32
	work := finalSemanticFixtureWorkControl{ctx: t.Context(), measurementEntered: func(context.Context, finalSemanticFixtureMeasurementWorkEvent) (func(), error) {
		entered.Add(1)
		return func() {}, nil
	}}
	if err := attachFinalAttemptCutsResultWithWork(measurement, key, servers, ledgers, nil, work); err == nil || !strings.Contains(err.Error(), "duplicated fixture operator") || entered.Load() != 0 || len(ledgers) != 0 {
		t.Fatalf("ambiguous operator admission mutated its caller: error=%v entries=%d ledgers=%d", err, entered.Load(), len(ledgers))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	work.ctx = ctx
	if err := attachFinalAttemptCutsResultWithWork(measurement, key, servers, ledgers, nil, work); !errors.Is(err, context.Canceled) || entered.Load() != 0 || len(ledgers) != 0 {
		t.Fatalf("canceled operator admitted work: %v", err)
	}
}

// Invalid owner metadata and a canceled actual first-wave wait cannot strand
// the observer's active count or return a callable duplicate exit.
func TestFinalSemanticFixtureMeasurementStageCancellationOwnsExit(t *testing.T) {
	t.Parallel()
	audit := newFinalSemanticFixtureMeasurementStageAudit()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	event := finalSemanticFixtureMeasurementWorkEvent{stage: finalSemanticFixtureOperatorBody, validatorID: 1, settlementEpoch: 10, noID: 1}
	leave, err := audit.enter(ctx, event)
	key := finalSemanticFixtureMeasurementOwner{stage: event.stage, validatorID: 1, settlementEpoch: 10}
	value := audit.snapshot()[key]
	if !errors.Is(err, context.Canceled) || leave != nil || value.entered != 1 || value.completed != 1 || value.active != 0 {
		t.Fatalf("canceled stage exit was lost or duplicated: error=%v leave=%v counts=%+v", err, leave != nil, value)
	}
	event.noID = 0
	if leave, err := audit.enter(t.Context(), event); err == nil || leave != nil || audit.snapshot()[key] != value {
		t.Fatalf("invalid stage owner changed active state: %v", err)
	}
}
