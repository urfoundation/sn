//go:build linux || darwin

// Real streamed M8 checkpoints exercise bounded assignment reuse without
// bypassing record signatures, scratch lifecycle, public bytes or final EOF.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Each call and replica authenticates its own unique assignment tuples even
// when the exact same cut, key map and transport objects are supplied again.
func TestAttemptCutV2ReplayAssignmentVerificationIsCutLocal(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 2)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	bounds, _ := attemptReplayV2TestBounds()
	checks := 0
	for pass := 1; pass <= 2; pass++ {
		options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
		visits := 0
		options.VisitRecord = func(AttemptRecord) error { visits++; return nil }
		result, err := replayAttemptCutV2WithHooks(t.Context(), cut, cut.Context, bounds, options, attemptCutV2ReplayHooks{
			AssignmentVerified: func() { checks++ },
		})
		if err != nil || result.TrailCount != 2 || result.CompleteCount != 2 || visits != len(records) {
			t.Fatalf("cut %d: result=%+v visits=%d error=%v", pass, result, visits, err)
		}
		if checks != pass*14 {
			t.Fatalf("cut %d performed %d assignment verifications, want %d", pass, checks, pass*14)
		}
	}
}

// An earlier authentic checkpoint cannot lend its success to an altered
// server signature, canonical assignment context or validator-signed boundary.
func TestAttemptCutV2ReplayAssignmentReuseRejectsChangedCheckpoint(t *testing.T) {
	original, key, keys := attemptReplayV2TestRecords(t, 1)
	for _, c := range []struct {
		name string
		edit func(*AttemptRecord)
	}{
		{name: "assignment signature", edit: func(record *AttemptRecord) { record.Assignments[0].AssignSignature[0] ^= 1 }},
		{name: "assignment context", edit: func(record *AttemptRecord) { record.ServerNonce[0] ^= 1 }},
		{name: "boundary", edit: func(record *AttemptRecord) { record.Boundary.EVMBlockHash = attemptHex32([32]byte{0x72}) }},
	} {
		records := attemptReplayV2TestRechain(t, original, key)
		c.edit(&records[1])
		records = attemptReplayV2TestRechain(t, records, key)
		cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
		options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
		visits, checks := 0, 0
		options.VisitRecord = func(AttemptRecord) error { visits++; return nil }
		bounds, _ := attemptReplayV2TestBounds()
		result, err := replayAttemptCutV2WithHooks(t.Context(), cut, cut.Context, bounds, options, attemptCutV2ReplayHooks{
			AssignmentVerified: func() { checks++ },
		})
		if err == nil || result != (AttemptCutV2ReplayResult{}) || visits != 1 || checks == 0 {
			t.Fatalf("%s: prior success accepted changed checkpoint: result=%+v visits=%d checks=%d error=%v", c.name, result, visits, checks, err)
		}
	}
}

// Key IDs are lookup labels. A changed key behind the same ID must receive a
// new real verification and cannot inherit an earlier key's successful tuple.
func TestAttemptCutV2ReplayAssignmentReuseRejectsChangedServerKey(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	bounds, _ := attemptReplayV2TestBounds()
	checks, indexed := 0, 0
	replacement := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x6a}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	result, err := replayAttemptCutV2WithHooks(t.Context(), cut, cut.Context, bounds, options, attemptCutV2ReplayHooks{
		AssignmentVerified: func() { checks++ },
		RecordIndexed: func() {
			indexed++
			if indexed == 1 {
				keys[records[0].Assignments[0].ServerKeyID] = replacement
			}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "attempt assignment 0 server signature is invalid") || result != (AttemptCutV2ReplayResult{}) || indexed != 1 || checks != 2 {
		t.Fatalf("changed server key: indexed=%d checks=%d result=%+v error=%v", indexed, checks, result, err)
	}
}

// Complete cache hits authenticate only assignments. The independent record
// signature is still required on the final checkpoint and every earlier row.
func TestAttemptCutV2ReplayAssignmentReuseRetainsValidatorSignature(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	records[len(records)-1].Signature[0] ^= 1
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	bounds, _ := attemptReplayV2TestBounds()
	checks := 0
	result, err := replayAttemptCutV2WithHooks(t.Context(), cut, cut.Context, bounds, options, attemptCutV2ReplayHooks{
		AssignmentVerified: func() { checks++ },
	})
	if err == nil || !strings.Contains(err.Error(), "validator signature is invalid") || result != (AttemptCutV2ReplayResult{}) || checks != 7 {
		t.Fatalf("independent record signature: checks=%d result=%+v error=%v", checks, result, err)
	}
}

// Even a newly signed public descriptor cannot replace the proof projected
// from fully verified records after all assignment tuples have become hits.
func TestAttemptCutV2ReplayAssignmentReuseRetainsLateProofProjection(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, func(_ int, _ *AttemptStreamV2Chunk, raw *[]byte) {
		var proof ProofRecord
		if err := json.Unmarshal(*raw, &proof); err != nil {
			t.Fatal(err)
		}
		proof.Coverage--
		encoded, err := json.Marshal(proof)
		if err != nil {
			t.Fatal(err)
		}
		*raw = append(encoded, '\n')
	})
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	bounds, _ := attemptReplayV2TestBounds()
	checks, indexed := 0, 0
	result, err := replayAttemptCutV2WithHooks(t.Context(), cut, cut.Context, bounds, options, attemptCutV2ReplayHooks{
		AssignmentVerified: func() { checks++ },
		RecordIndexed:      func() { indexed++ },
	})
	if err == nil || !strings.Contains(err.Error(), "proof differs from its signed record projection") || result != (AttemptCutV2ReplayResult{}) || checks != 7 || indexed != len(records) {
		t.Fatalf("late proof projection: indexed=%d checks=%d result=%+v error=%v", indexed, checks, result, err)
	}
}

// Cancellation at the final indexed row invalidates every staged result even
// after all unique assignment checks and the complete record chain succeeded.
func TestAttemptCutV2ReplayAssignmentReuseRetainsLateCancellation(t *testing.T) {
	records, key, keys := attemptReplayV2TestRecords(t, 1)
	cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, key, nil, nil)
	options := attemptReplayV2TestOptions(t, recordStream, proofStream, keys)
	bounds, _ := attemptReplayV2TestBounds()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checks, indexed := 0, 0
	result, err := replayAttemptCutV2WithHooks(ctx, cut, cut.Context, bounds, options, attemptCutV2ReplayHooks{
		AssignmentVerified: func() { checks++ },
		RecordIndexed: func() {
			indexed++
			if indexed == len(records) {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) || result != (AttemptCutV2ReplayResult{}) || checks != 7 || indexed != len(records) {
		t.Fatalf("late cancellation: indexed=%d checks=%d result=%+v error=%v", indexed, checks, result, err)
	}
}
