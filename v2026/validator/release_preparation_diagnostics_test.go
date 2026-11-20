//go:build linux || darwin

// Preparation observations retain closed facts without acquiring error or I/O authority.
package validator

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Every foreign error inspection is a deterministic failure.
type preparationDiagnosticHostileError struct{}

func (*preparationDiagnosticHostileError) Error() string { panic("error formatted") }
func (*preparationDiagnosticHostileError) Is(error) bool { panic("error matched") }
func (*preparationDiagnosticHostileError) As(any) bool   { panic("error converted") }
func (*preparationDiagnosticHostileError) Unwrap() error { panic("error unwrapped") }

// The actual exporter acknowledges each finite record before output is inspected.
func TestPreparationDiagnosticsClosedFactsAndElapsed(t *testing.T) {
	sink := &releaseDiagnosticCapture{written: make(chan struct{}, 8)}
	var millis atomic.Int64
	now := func() time.Time { return time.Unix(0, millis.Load()*int64(time.Millisecond)) }
	ctx, owner, err := newReleaseDiagnostics(t.Context(), sink, now)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	token := newReleasePreparationDiagnostic(ctx, releasePreparationCollectGate, 9, false, 7, false)
	<-sink.written
	millis.Store(15)
	token.setNativeEpoch(11)
	token.setOperator(23)
	token.move(releasePreparationInputDetach)
	<-sink.written
	<-sink.written
	millis.Store(30)
	token.finish(&preparationDiagnosticHostileError{})
	<-sink.written
	text := sink.String()
	for _, expected := range []string{
		"native_epoch=0 epoch_known=false operator_id=0 operator_known=false stage=collect_gate event=begin elapsed_ms=0 operation_elapsed_ms=0 cancel_cause=none",
		"stage=collect_gate event=done elapsed_ms=15 operation_elapsed_ms=15",
		"native_epoch=11 epoch_known=true operator_id=23 operator_known=true stage=input_detach event=failed elapsed_ms=15 operation_elapsed_ms=30 cancel_cause=none",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing closed observation %q in %s", expected, text)
		}
	}
}

// Context causes are compared by identity; foreign methods are never consulted.
func TestPreparationDiagnosticsCancellationAndDurationBounds(t *testing.T) {
	for _, candidate := range []struct {
		cause error
		label string
	}{
		{cause: context.DeadlineExceeded, label: "deadline"},
		{cause: context.Canceled, label: "canceled"},
		{cause: &preparationDiagnosticHostileError{}, label: "other"},
	} {
		sink := &releaseDiagnosticCapture{written: make(chan struct{}, 8)}
		ctx, owner, err := newReleaseDiagnostics(t.Context(), sink, func() time.Time { return time.Unix(0, 0) })
		if err != nil {
			t.Fatal(err)
		}
		canceled, cancel := context.WithCancelCause(ctx)
		cancel(candidate.cause)
		token := newReleasePreparationHttpDiagnostic(canceled, releasePreparationStage(255))
		token.started, token.operationAt = time.Unix(-int64((48*time.Hour)/time.Second), 0), time.Unix(1, 0)
		token.finish(&preparationDiagnosticHostileError{})
		<-sink.written
		text := sink.String()
		if !strings.Contains(text, "stage=unknown event=failed elapsed_ms=86400000 operation_elapsed_ms=0 cancel_cause="+candidate.label) {
			t.Fatal("unbounded or foreign cancellation observation", text)
		}
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Disabled observations allocate nothing, and successful HTTP reads offer nothing.
func TestPreparationDiagnosticsAbsentOwnerAndHttpSuccess(t *testing.T) {
	ctx := context.Background()
	if allocations := testing.AllocsPerRun(10, func() {
		token := newReleasePreparationDiagnostic(ctx, releasePreparationCollectGate, 0, false, 0, false)
		if token != nil {
			panic("unexpected diagnostic owner")
		}
		token.move(releasePreparationInputDetach)
		token.setNativeEpoch(3)
		token.setOperator(5)
		token.finish(&preparationDiagnosticHostileError{})
	}); allocations != 0 {
		t.Fatal("disabled diagnostic allocated", allocations)
	}
	sink := &releaseDiagnosticCapture{written: make(chan struct{}, 8)}
	enabled, owner, err := newReleaseDiagnostics(t.Context(), sink, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	newReleasePreparationHttpDiagnostic(enabled, releasePreparationHttpRead).finish(nil)
	newReleasePreparationHttpDiagnostic(enabled, releasePreparationHttpClose).finish(&preparationDiagnosticHostileError{})
	<-sink.written
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	if strings.Count(sink.String(), "code=preparation_stage") != 1 || !strings.Contains(sink.String(), "stage=http_close event=failed") {
		t.Fatal("successful HTTP read emitted or failure missing", sink.String())
	}

}

// A barrier holds the real destination while offers fill and overflow its finite queue.
func TestPreparationDiagnosticsBlockedExporterPreservesOriginalError(t *testing.T) {
	sink := &releaseDiagnosticBlockedSink{entered: make(chan struct{}), left: make(chan struct{})}
	ctx, owner, err := newReleaseDiagnostics(t.Context(), sink, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.close()
	token := newReleasePreparationDiagnostic(ctx, releasePreparationCollectGate, 0, false, 0, false)
	<-sink.entered
	sentinel := &preparationDiagnosticHostileError{}
	operation := func() error {
		defer token.finish(sentinel)
		for index := 0; index < 20; index++ {
			token.move(releasePreparationInputDetach)
		}
		return sentinel
	}
	if got := operation(); got != sentinel {
		t.Fatal("diagnostics replaced original error")
	}
	if owner.exporter.Snapshot("steering").Dropped == 0 {
		t.Fatal("actual bounded queue was not saturated")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	<-sink.left
}
