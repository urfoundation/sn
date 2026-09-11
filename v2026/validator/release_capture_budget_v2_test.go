//go:build linux || darwin

// Tiny exact budgets prove provenance and physical-content accounting without
// allocating configured multi-gigabyte tapes.
package validator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Both genuine sink writes remain distinct observations of identical bytes.
func TestReleaseCaptureV2BudgetDeduplicatesBytesNotOrigins(t *testing.T) {
	root := t.TempDir()
	writes := 0
	owner, err := newReleaseEvidenceCaptureBudgetV2(t.Context(), ReleaseEvidenceV2CaptureOptions{MaximumBytes: 3, MaximumObjects: 2}, func(ctx context.Context, source ReleaseEvidenceV2CaptureSource, raw []byte) error {
		writes++
		return os.WriteFile(filepath.Join(root, source.Origin), raw, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"one", "two"} {
		if err := owner.emit(ReleaseEvidenceV2CaptureSource{Kind: AttemptStreamV2Records, Name: "same", Origin: origin}, []byte{1, 2, 3}); err != nil {
			t.Fatal(err)
		}
	}
	if writes != 2 || len(owner.sources) != 2 || len(owner.content) != 1 || owner.remaining != 0 {
		t.Fatal("equal replicas lost provenance or consumed duplicate bytes")
	}
	if err := owner.emit(ReleaseEvidenceV2CaptureSource{Kind: AttemptStreamV2Records, Name: "same", Origin: "one"}, []byte{3, 2, 1}); err == nil || writes != 2 {
		t.Fatal("changed original provenance reached sink")
	}
	if err := owner.emit(ReleaseEvidenceV2CaptureSource{Kind: AttemptStreamV2Records, Name: "same", Origin: "three"}, []byte{1, 2, 3}); err == nil || writes != 2 {
		t.Fatal("duplicate content bypassed provenance count")
	}
}

// Class ceilings are independent even if two classes present the same hash.
func TestReleaseCaptureV2BudgetControlCannotBorrowTapeBytes(t *testing.T) {
	writes := 0
	owner, err := newReleaseEvidenceCaptureBudgetV2(t.Context(), ReleaseEvidenceV2CaptureOptions{MaximumBytes: 7, MaximumObjects: 4, MaximumDataBytes: 5, MaximumControlBytes: 2}, func(context.Context, ReleaseEvidenceV2CaptureSource, []byte) error { writes++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.emit(ReleaseEvidenceV2CaptureSource{Kind: AttemptStreamV2Records, Name: "record"}, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := owner.emit(ReleaseEvidenceV2CaptureSource{Kind: "native-rpc", Name: "same-bytes"}, []byte{1, 2, 3}); err == nil || writes != 1 {
		t.Fatal("control hid behind an existing data hash")
	}
	if err := owner.emit(ReleaseEvidenceV2CaptureSource{Kind: "native-rpc", Name: "read"}, []byte{4, 5}); err != nil {
		t.Fatal(err)
	}
	if owner.controlRemaining != 0 || owner.dataRemaining != 2 || owner.remaining != 2 {
		t.Fatal("separate exact byte owners drifted")
	}
	if err := owner.emit(ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "one-over"}, []byte{6}); err == nil || writes != 2 {
		t.Fatal("one-over control wrote to sink")
	}
}

// A rejected durable write does not consume a successful-capture allowance.
func TestReleaseCaptureV2BudgetSinkFailureRemainsRetryable(t *testing.T) {
	writes := 0
	failure := errors.New("durable source refused")
	owner, err := newReleaseEvidenceCaptureBudgetV2(t.Context(), ReleaseEvidenceV2CaptureOptions{MaximumBytes: 1, MaximumObjects: 1}, func(context.Context, ReleaseEvidenceV2CaptureSource, []byte) error {
		writes++
		if writes == 1 {
			return failure
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	source := ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "original"}
	if err := owner.emit(source, []byte{1}); !errors.Is(err, failure) || owner.remaining != 1 || len(owner.sources) != 0 {
		t.Fatal("failed write consumed capture identity")
	}
	if err := owner.emit(source, []byte{1}); err != nil || writes != 2 || owner.remaining != 0 {
		t.Fatal("exact retry did not finish")
	}
}

// A successful sink cannot advance its owner after cancellation.
func TestReleaseCaptureV2BudgetCancellationAfterSink(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writes := 0
	owner, err := newReleaseEvidenceCaptureBudgetV2(ctx, ReleaseEvidenceV2CaptureOptions{MaximumBytes: 1, MaximumObjects: 1}, func(context.Context, ReleaseEvidenceV2CaptureSource, []byte) error { writes++; cancel(); return nil })
	if err != nil {
		t.Fatal(err)
	}
	source := ReleaseEvidenceV2CaptureSource{Kind: "setup", Name: "original"}
	if err := owner.emit(source, []byte{1}); !errors.Is(err, context.Canceled) || len(owner.sources) != 0 {
		t.Fatal("cancelled durable boundary became a completed source")
	}
	if err := owner.emit(source, []byte{1}); !errors.Is(err, context.Canceled) || writes != 1 {
		t.Fatal("cancelled owner continued sink calls")
	}
}

// Impossible counters/classes are rejected before any sink call or census
// allocation; the small accepted owner still refuses one new byte at its cap.
func TestReleaseCaptureV2BudgetRejectsImpossibleCountersAndOneOver(t *testing.T) {
	writes := 0
	retain := func(context.Context, ReleaseEvidenceV2CaptureSource, []byte) error { writes++; return nil }
	for _, options := range []ReleaseEvidenceV2CaptureOptions{
		{MaximumBytes: ^uint64(0), MaximumObjects: 1},
		{MaximumBytes: 1, MaximumObjects: ^uint64(0)},
		{MaximumBytes: 1, MaximumObjects: 1, MaximumDataBytes: 2},
		{MaximumBytes: 1, MaximumObjects: 1, MaximumControlBytes: 2},
	} {
		if owner, err := newReleaseEvidenceCaptureBudgetV2(t.Context(), options, retain); owner != nil || err == nil || writes != 0 {
			t.Fatal("impossible owner reached allocation/sink")
		}
	}
	owner, err := newReleaseEvidenceCaptureBudgetV2(t.Context(), ReleaseEvidenceV2CaptureOptions{MaximumBytes: 2, MaximumObjects: 2}, retain)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.emit(ReleaseEvidenceV2CaptureSource{Kind: AttemptStreamV2Records, Name: "exact"}, []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := owner.emit(ReleaseEvidenceV2CaptureSource{Kind: AttemptStreamV2Records, Name: "one-over"}, []byte{3}); err == nil || writes != 1 {
		t.Fatal("one new byte escaped the exact owner ceiling")
	}
}
