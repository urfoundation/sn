// Work-count contexts cancel the actual owner without timing or scheduler assumptions.
package payoutartifact

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
)

// The inherited Done/Err belongs to a real cancellation, triggered at a fixed work boundary.
type artifactWorkContext struct {
	context.Context
	cancel context.CancelFunc
	calls  atomic.Int32
	stop   int32
}

// Cancellation never depends on elapsed time or goroutine scheduling.
func (self *artifactWorkContext) Err() error {
	if self.calls.Add(1) == self.stop {
		self.cancel()
	}
	return self.Context.Err()
}

// Each test owns and releases the genuine operation context.
func artifactCancelAt(t *testing.T, stop int32) *artifactWorkContext {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	return &artifactWorkContext{Context: ctx, cancel: cancel, stop: stop}
}

// Stop inside the original row clone, before publishing any partial evidence.
func TestClosedWorkCloneStopsAtOwnedRowBoundary(t *testing.T) {
	original := closedWorkTestArtifact(t).ClosedWork
	ctx := artifactCancelAt(t, 2)
	copied, err := cloneClosedWork(ctx, original)
	if copied != nil || !errors.Is(err, context.Canceled) || ctx.calls.Load() != 2 {
		t.Fatal("original row clone ignored its canceled owner", copied, err, ctx.calls.Load())
	}
	copied, err = cloneClosedWork(t.Context(), original)
	if err != nil || !reflect.DeepEqual(original, copied) {
		t.Fatal("healthy original clone changed", err)
	}
}

// All public canonical APIs keep the exact legacy bytes and mathematical result.
func TestArtifactContextCanonicalCompatibility(t *testing.T) {
	for _, artifact := range []*Artifact{testArtifact(t), closedWorkTestArtifact(t)} {
		legacy, err := Bytes(artifact)
		if err != nil {
			t.Fatal(err)
		}
		owned, err := BytesWithContext(t.Context(), artifact)
		if err != nil || !reflect.DeepEqual(legacy, owned) {
			t.Fatal("owned canonical bytes changed", err)
		}
		decoded, err := DecodeWithContext(t.Context(), owned)
		if err != nil || !reflect.DeepEqual(artifact, decoded) {
			t.Fatal("owned decode changed original", err)
		}
	}
}

// A cancellation during signature reconstruction remains operational, never integrity evidence.
func TestClosedWorkReconstructionPreservesOwnedCancellation(t *testing.T) {
	artifact := closedWorkTestArtifact(t)
	ctx := artifactCancelAt(t, 7)
	result, err := VerifyClosedWork(ctx, artifact)
	if result != nil || !errors.Is(err, context.Canceled) || errors.Is(err, ErrClosedWorkIntegrity) || ctx.calls.Load() != 7 {
		t.Fatal("owned reconstruction synthesized integrity or continued", result, err, ctx.calls.Load())
	}
	if _, err := VerifyClosedWork(t.Context(), artifact); err != nil {
		t.Fatal("healthy reconstruction did not recover", err)
	}
}

// Cancellation already owned at ingress takes precedence over parsing untrusted bytes.
func TestArtifactCanceledDecodeRefusesBeforeParsing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	decoded, err := DecodeWithContext(ctx, []byte("synthetic malformed wire"))
	if decoded != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled decoder entered untrusted parsing", decoded, err)
	}
	raw, err := BytesWithContext(ctx, closedWorkTestArtifact(t))
	if raw != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled publication emitted canonical bytes", err)
	}
}
