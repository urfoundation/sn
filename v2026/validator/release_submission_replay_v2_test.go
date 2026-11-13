//go:build linux || darwin

package validator

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

func TestReleaseSubmissionReplayV2ReusesCompletedTerminalAndOrdinary(t *testing.T) {
	fixture := newReleaseMeasurementEnvelopeV2TerminalTestFixture(t, true)
	options := fixture.options()
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	runtime := &releaseRuntimeV2{history: &releaseEvidenceV2StartupHistory{retainedStartup: true}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	measurement, hash, reuse, err := runtime.sealSubmissionArtifactV2(ctx, fixture.artifact, options)
	if err != nil || reuse == nil || *reads == 0 || hash != ReleaseMeasurementContentHash(measurement) || len(reuse.verified.ReplayByNO) != 2 || len(reuse.verified.SettlementReplayByNO) != 2 {
		t.Fatalf("complete submission replay: reads=%d reuse=%v err=%v", *reads, reuse != nil, err)
	}
	defer reuse.close()
	firstReads := *reads
	first, err := reuse.verify(ctx, measurement)
	if err != nil {
		t.Fatal(err)
	}
	want := cloneReleaseSubmissionResultV2(first)
	first.Decision.UIDs[0]++
	first.Decision.Scores[0].SetInt64(999)
	for _, stats := range first.Decision.StatsByNO {
		for id, provider := range stats.Providers {
			clear(provider.EgressIPHashes)
			delete(stats.Providers, id)
		}
	}
	clear(first.ReplayByNO)
	clear(first.SettlementReplayByNO)
	second, err := reuse.verify(ctx, measurement)
	if err != nil || !reflect.DeepEqual(second, want) {
		t.Fatalf("caller mutated retained result: %v", err)
	}
	raw, _, envelope, err := reuse.sealEnvelope(ctx, measurement, fixture.artifact.SelfUID, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Unix(1, 0))
	if err != nil || envelope == nil {
		t.Fatalf("reused envelope: %v", err)
	}
	if _, err := DecodeReleaseMeasurementEnvelopeV2(ctx, raw, options.MaxControlBytes); err != nil {
		t.Fatalf("real envelope signature: %v", err)
	}
	if *reads != firstReads {
		t.Fatalf("decision/envelope repeated streams: first=%d final=%d", firstReads, *reads)
	}
	// A different cut may remain structurally admissible; its exact canonical
	// identity must still miss the completed result before signing anything.
	changed := cloneReleaseMeasurementArtifact(t, fixture.artifact)
	changed.Inputs[0].AttemptCutV2.Signature[0] ^= 1
	changedBytes, err := canonicalReleaseMeasurementBytes(changed)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := reuse.verify(ctx, changedBytes); err == nil {
		t.Fatal("changed signed input reused completed replay")
	} else {
		assertReleaseMeasurementV2Empty(t, result)
	}
	// Even an internal authority drift cannot reuse a result obtained with
	// different server keys. Original caller maps are separately owned.
	noID := fixture.artifact.Inputs[0].NoID
	op := reuse.options.Operators[noID]
	for id, key := range op.Measurement.Replay.ServerKeys {
		key[0] ^= 1
		if _, err := reuse.verify(ctx, measurement); err == nil {
			t.Fatal("changed independent server key reused completed replay")
		}
		key[0] ^= 1
		op.Measurement.Replay.ServerKeys[id] = key
		break
	}
	if _, err := reuse.verify(context.WithValue(ctx, struct{}{}, true), measurement); err == nil {
		t.Fatal("another submission context reused completed replay")
	}
	if _, _, signed, err := reuse.sealEnvelope(ctx, measurement, fixture.artifact.SelfUID+1, fixture.hotkey, releaseMeasurementEnvelopeTestPreparedHash, time.Unix(1, 0)); err == nil || signed != nil {
		t.Fatal("envelope signer authority was waived")
	}
	cancel()
	if result, err := reuse.verify(ctx, measurement); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reuse: %v", err)
	} else {
		assertReleaseMeasurementV2Empty(t, result)
	}
	reuse.close()
	if _, err := reuse.verify(ctx, measurement); err == nil {
		t.Fatal("closed submission reused a prior attempt")
	}
}

func TestReleaseSubmissionReplayV2FailureAndStrictPath(t *testing.T) {
	fixture := newReleaseMeasurementEnvelopeV2TestFixture(t, 2)
	ctx := t.Context()
	runtime := &releaseRuntimeV2{history: &releaseEvidenceV2StartupHistory{}}
	options := fixture.options()
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	measurement, _, reuse, err := runtime.sealSubmissionArtifactV2(ctx, fixture.artifact, options)
	if err != nil || reuse != nil || len(measurement) == 0 || *reads == 0 {
		t.Fatalf("strict path changed: reuse=%v err=%v", reuse != nil, err)
	}
	runtime.history.retainedStartup = true
	options = fixture.options()
	op := options.Operators[10]
	open := op.Measurement.Replay.OpenData
	failure := errors.New("submission final proof close failed")
	closes := 0
	op.Measurement.Replay.OpenData = func(ctx context.Context, kind, hash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, hash, size)
		if err != nil || kind != AttemptStreamV2Proofs {
			return reader, err
		}
		return &attemptCutV2SealTestClosingReader{ReadCloser: reader, afterClose: func() error { closes++; return failure }}, nil
	}
	options.Operators[10] = op
	measurement, hash, reuse, err := runtime.sealSubmissionArtifactV2(ctx, fixture.artifact, options)
	if !errors.Is(err, failure) || closes == 0 || measurement != nil || hash != "" || reuse != nil {
		t.Fatalf("late failure published reusable result: closes=%d reuse=%v err=%v", closes, reuse != nil, err)
	}
}
