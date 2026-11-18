//go:build linux || darwin

package validator

// Policy admission is distinct from generic protocol replay: valid signed
// M4/M16 records are not authority for an authenticated M8 activation, and
// failures cannot avoid that binding by having an empty proof projection.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Legal policy depths retain complete real signatures and the release A_min.
func TestAttemptCutV2SealAuthenticatedPolicyDepthControls(t *testing.T) {
	for _, depth := range []int{4, 8, 16} {
		fixture := newAttemptCutV2SealTestFixture(t, depth, 1, 1)
		options, _ := newAttemptCutV2SealTestOptions(t, fixture)
		cut, verified, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
		if err != nil {
			t.Fatalf("authenticated M%d: %v", depth, err)
		}
		assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, fixture.recordTs, 1, 1)
		if cut.Context.Activation.Domain.PolicyHash != fixture.expected.Activation.Domain.PolicyHash {
			t.Fatalf("M%d sealer changed the authenticated policy", depth)
		}
	}
}

// Both completed and failed valid alternative-depth source prefixes are
// refused under M8 before the first staged row, not merely at proof replay.
func TestAttemptCutV2SealRejectsOtherDepthUnderM8Policy(t *testing.T) {
	for _, depth := range []int{4, 16} {
		fixture := newAttemptCutV2SealTestFixture(t, depth, 1, 1)
		policy := exactPolicy(t)
		hash, err := policy.Hash()
		if err != nil {
			t.Fatal(err)
		}
		for _, failedOnly := range []bool{false, true} {
			expected := fixture.expected
			expected.Activation.Domain.PolicyHash = hash
			if failedOnly {
				expected.FirstSequence, expected.EgressFirstSequence = uint64(depth+1), uint64(depth+1)
				expected.PriorRoot = fixture.recordTs[depth-1].RecordHash
			}
			options, objects := newAttemptCutV2SealTestOptions(t, fixture)
			cut, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, expected, policy, fixture.key, fixture.bounds, options)
			if err == nil || !strings.Contains(err.Error(), "policy depth") || cut != nil || result != (AttemptCutV2ReplayResult{}) || objects.writes != 0 || objects.reads != 0 {
				t.Fatalf("M%d failedOnly=%t admitted under M8: cut=%+v result=%+v writes=%d reads=%d: %v", depth, failedOnly, cut, result, objects.writes, objects.reads, err)
			}
		}
	}
}

// The same genuine alternative-depth archives pass generic replay, proving
// rejection is the policy boundary rather than a damaged proof or signature.
func TestAttemptCutV2ReplayRejectsOtherDepthUnderM8Policy(t *testing.T) {
	for _, depth := range []int{4, 16} {
		fixture := newAttemptCutV2SealTestFixture(t, depth, 1, 1)
		policy := exactPolicy(t)
		hash, err := policy.Hash()
		if err != nil {
			t.Fatal(err)
		}
		for _, failedOnly := range []bool{false, true} {
			records := fixture.recordTs
			if failedOnly {
				records = records[depth:]
			}
			cut, recordStream, proofStream := attemptReplayV2TestCut(t, records, fixture.key, nil, nil)
			cut.Context.Activation.Domain.PolicyHash = hash
			cut.Context.Activation.Domain.Coordinator = fixture.expected.Activation.Domain.Coordinator
			bounds, _ := attemptReplayV2TestBounds()
			cut.Signature, err = cut.Sign(fixture.key, bounds)
			if err != nil {
				t.Fatal(err)
			}
			generic, err := ReplayAttemptCutV2(context.Background(), cut, cut.Context, bounds, attemptReplayV2TestOptions(t, recordStream, proofStream, fixture.server.serverPublicKeys()))
			if err != nil || generic.Records.ItemCount != uint64(len(records)) || generic.FailedCount != 1 {
				t.Fatalf("generic M%d failedOnly=%t control %+v: %v", depth, failedOnly, generic, err)
			}
			options := attemptReplayV2TestOptions(t, recordStream, proofStream, fixture.server.serverPublicKeys())
			visits := 0
			options.VisitRecord = func(AttemptRecord) error { visits++; return nil }
			result, err := ReplayAttemptCutV2WithPolicy(context.Background(), cut, cut.Context, policy, bounds, options)
			if err == nil || !strings.Contains(err.Error(), "depth differs from authenticated policy") || result != (AttemptCutV2ReplayResult{}) || visits != 0 {
				t.Fatalf("M%d failedOnly=%t public policy admission result=%+v visits=%d: %v", depth, failedOnly, result, visits, err)
			}
		}
	}
}

// Decoded policy must hash to caller-authenticated activation before any
// metadata/data callback or scratch mutation, even for a checked empty cut.
func TestAttemptCutV2PolicyHashMismatchPrecedesIO(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 0)
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	cut, _, err := SealAttemptCutV2(context.Background(), fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, variation := range []string{"decoded-depth", "activation-hash", "invalid-policy"} {
		policy, expected, candidate := fixture.policy, fixture.expected, *cut
		switch variation {
		case "decoded-depth":
			policy.Verify.TrailDepth = 4
		case "activation-hash":
			expected.Activation.Domain.PolicyHash[0] ^= 1
			candidate.Context = expected
			candidate.Signature, err = candidate.Sign(fixture.key, fixture.bounds)
			if err != nil {
				t.Fatal(err)
			}
		case "invalid-policy":
			policy.Verify.TrailDepth = 0
		}
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		sealed, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, expected, policy, fixture.key, fixture.bounds, options)
		if err == nil || !strings.Contains(err.Error(), "decoded policy differs") || sealed != nil || result != (AttemptCutV2ReplayResult{}) || objects.writes != 0 || objects.reads != 0 {
			t.Fatalf("%s sealer reached I/O or success: %v", variation, err)
		}
		replayPath := filepath.Join(t.TempDir(), "public-scratch")
		result, err = ReplayAttemptCutV2WithPolicy(context.Background(), candidate, expected, policy, fixture.bounds, AttemptCutV2ReplayOptions{Bounds: options.ReplayBounds, ScratchDirectory: replayPath, ServerKeys: options.ServerKeys, ReadMetadata: options.ReadMetadata, OpenData: options.OpenData})
		if err == nil || !strings.Contains(err.Error(), "decoded policy differs") || result != (AttemptCutV2ReplayResult{}) || objects.reads != 0 {
			t.Fatalf("%s replay reached I/O or success: %v", variation, err)
		}
		for _, path := range []string{options.ScratchDirectory, replayPath} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s policy refusal mutated %s: %v", variation, path, err)
			}
		}
	}
}

// A canonical policy hash cannot authorize an unsupported wire depth even
// when no records exist. Legal M4/M8/M16 controls remain separately covered.
func TestAttemptCutV2PolicyRejectsUnsupportedProtocolDepth(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 0, 0)
	for _, depth := range []int{2, 17} {
		policy, expected := fixture.policy, fixture.expected
		policy.Verify.TrailDepth = depth
		hash, err := policy.Hash()
		if err != nil {
			t.Fatalf("policy M%d does not provide the intended hash-valid control: %v", depth, err)
		}
		expected.Activation.Domain.PolicyHash = hash
		options, objects := newAttemptCutV2SealTestOptions(t, fixture)
		cut, result, err := SealAttemptCutV2(context.Background(), fixture.ledger, expected, policy, fixture.key, fixture.bounds, options)
		if err == nil || !strings.Contains(err.Error(), "outside the supported protocol range") || cut != nil || result != (AttemptCutV2ReplayResult{}) || objects.writes != 0 || objects.reads != 0 {
			t.Fatalf("unsupported M%d policy returned cut=%+v result=%+v: %v", depth, cut, result, err)
		}
	}
}
