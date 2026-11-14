// A history replay that only ran out of time stays a retryable read wait, no
// matter how many object reads it joined. Every other leaf keeps its verdict,
// and the original graph is still opened exactly once.
package validator

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

// A replay-shaped graph: nested wrappers around joins of object reads that
// each hit the deadline, deeper than the ordinary 32-level capture bound.
func productionSteeringDeadlineTestGraph(depth int, leaf func(int) error) error {
	reads := make([]error, 0, 8)
	for index := 0; index < 8; index++ {
		reads = append(reads, &url.Error{Op: "Get", URL: fmt.Sprintf("https://reader:secret@api.example/sn/attempt-artifact?hash=0x%02x&kind=metadata", index), Err: leaf(index)})
	}
	err := errors.Join(reads...)
	for level := 0; level < depth; level++ {
		err = errors.Join(fmt.Errorf("V2 intent %d: %w", level, err), context.DeadlineExceeded)
	}
	return err
}

func TestProductionSteeringDeadlineOnlyGraphStaysRetryable(t *testing.T) {
	t.Parallel()
	deadline := func(int) error { return context.DeadlineExceeded }
	deep := productionSteeringDeadlineTestGraph(40, deadline)
	if retryableProductionSteeringRead(observeReleaseError(deep)) {
		t.Fatal("test graph is not beyond the ordinary capture bound")
	}
	observed := observeProductionSteeringRead(deep, false)
	compact, ok := releaseObservedValue(observed).(*productionSteeringReadDeadline)
	if !ok || !retryableProductionSteeringRead(observed) || compact.leaves != 48 || !errors.Is(observed, deep) {
		t.Fatalf("deadline-only graph did not become one retryable cause: %T %v", releaseObservedValue(observed), observed)
	}
	if strings.Contains(observed.Error(), "secret") || !strings.Contains(observed.Error(), "api.example/sn/attempt-artifact") || !strings.Contains(observed.Error(), "ran out of time") {
		t.Fatalf("sample leaked credentials or lost the read: %s", observed.Error())
	}
	if again := observeReleaseError(observed); again != observed || !retryableProductionSteeringRead(again) {
		t.Fatal("a later observation reopened the collapsed cause")
	}
	// Cancellation counts only when the bounded operation itself expired.
	canceled := productionSteeringDeadlineTestGraph(40, func(index int) error {
		if index%2 == 0 {
			return context.Canceled
		}
		return context.DeadlineExceeded
	})
	if retryableProductionSteeringRead(observeProductionSteeringRead(canceled, false)) {
		t.Fatal("unexpired cancellation collapsed into a deadline")
	}
	if !retryableProductionSteeringRead(observeProductionSteeringRead(canceled, true)) {
		t.Fatal("cancellation caused by the expired operation stayed hard")
	}
	onlyCanceled := productionSteeringDeadlineTestGraph(0, func(int) error { return context.Canceled })
	for level := 0; level < 40; level++ {
		onlyCanceled = fmt.Errorf("V2 intent %d: %w", level, onlyCanceled)
	}
	if retryableProductionSteeringRead(observeProductionSteeringRead(onlyCanceled, true)) {
		t.Fatal("a graph without any deadline collapsed")
	}
	// Within the ordinary bound the capture is the ordinary one: a small
	// graph keeps its own cause and verdict, retryable or not.
	small := &url.Error{Op: "Get", URL: "https://api.example/x", Err: context.DeadlineExceeded}
	for _, graph := range []error{small, errors.Join(context.DeadlineExceeded, context.Canceled)} {
		production, ordinary := observeProductionSteeringRead(graph, true), observeReleaseError(graph)
		if production.Error() != ordinary.Error() || retryableProductionSteeringRead(production) != retryableProductionSteeringRead(ordinary) {
			t.Fatalf("a graph within the ordinary bound changed: %v / %v", production, ordinary)
		}
		if _, collapsed := releaseObservedValue(production).(*productionSteeringReadDeadline); collapsed {
			t.Fatal("a graph within the ordinary bound collapsed")
		}
	}
}

// Any refusal, file, native, foreign or unknown leaf keeps the whole graph hard.
func TestProductionSteeringDeadlineCollapseKeepsHardCauses(t *testing.T) {
	t.Parallel()
	for name, hard := range map[string]error{
		"refusal":       &releaseObservedRefusal{original: errors.New("x")},
		"observed hard": &releaseObservedHard{cause: context.DeadlineExceeded},
		"file":          &os.PathError{Op: "open", Path: "/state", Err: os.ErrNotExist},
		"native":        &crv4.SubstrateReadHttpStatusError{},
		"foreign":       &productionSteeringCauseMatcher{cause: context.DeadlineExceeded},
		"contradiction": errors.New("V2 intent history bytes or finite census differ"),
		"nil child":     &productionSteeringCauseNode{children: []error{context.DeadlineExceeded, nil}},
		"too wide":      &productionSteeringCauseNode{children: make([]error, 129)},
		"empty join":    &productionSteeringCauseNode{},
	} {
		graph := productionSteeringDeadlineTestGraph(40, func(index int) error {
			if index == 5 {
				return hard
			}
			return context.DeadlineExceeded
		})
		observed := observeProductionSteeringRead(graph, true)
		if retryableProductionSteeringRead(observed) {
			t.Fatalf("%s leaf was hidden by the deadline collapse", name)
		}
		if _, collapsed := releaseObservedValue(observed).(*productionSteeringReadDeadline); collapsed {
			t.Fatalf("%s leaf collapsed", name)
		}
	}
	cycle := &productionSteeringCauseNode{}
	cycle.children = []error{cycle}
	if retryableProductionSteeringRead(observeProductionSteeringRead(cycle, true)) {
		t.Fatal("a cyclic graph collapsed")
	}
}

// The deep capture opens a changing original once: a hard first view stays
// hard even though a second look would show a deadline.
func TestProductionSteeringDeadlineCaptureObservesOnce(t *testing.T) {
	t.Parallel()
	for _, hardFirst := range []bool{true, false} {
		hard := &os.PathError{Op: "read", Path: "synthetic-deep-original", Err: context.DeadlineExceeded}
		probe := &releasePreparationChangingCause{first: hard, later: context.DeadlineExceeded}
		if !hardFirst {
			probe.first, probe.later = context.DeadlineExceeded, hard
		}
		graph := productionSteeringDeadlineTestGraph(40, func(index int) error {
			if index == 2 {
				return probe
			}
			return context.DeadlineExceeded
		})
		observed := observeProductionSteeringRead(graph, true)
		if probe.observations != 1 {
			t.Fatalf("deep capture opened a changing original %d times", probe.observations)
		}
		if retryableProductionSteeringRead(observed) == hardFirst || probe.observations != 1 {
			t.Fatalf("deep capture did not keep its first view: hardFirst=%t observations=%d", hardFirst, probe.observations)
		}
	}
}

// The actual read owner retries and then waits on a deadline-only replay
// instead of returning it as a hard failure.
func TestProductionSteeringReadKeepsDeepDeadlineRetryable(t *testing.T) {
	t.Parallel()
	self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
	reads, waits := 0, 0
	self.productionReadHooks.wait = func(context.Context, time.Duration) error {
		waits++
		return context.DeadlineExceeded
	}
	err := self.productionRead(t.Context(), productionReadIntent, nil, func(context.Context) error {
		reads++
		return productionSteeringDeadlineTestGraph(40, func(int) error { return context.DeadlineExceeded })
	})
	var wait *productionSteeringReadWait
	if !errors.As(err, &wait) || wait.phase != productionReadIntent || reads != 1 || waits != 1 {
		t.Fatalf("deep deadline replay became a hard read failure: reads=%d waits=%d err=%v", reads, waits, err)
	}
	// A mixed graph is still returned hard without a retry.
	reads, waits = 0, 0
	err = self.productionRead(t.Context(), productionReadIntent, nil, func(context.Context) error {
		reads++
		return productionSteeringDeadlineTestGraph(40, func(index int) error {
			if index == 3 {
				return errors.New("V2 prepared vector differs from the independently replayed decision")
			}
			return context.DeadlineExceeded
		})
	})
	if err == nil || errors.As(err, &wait) || reads != 1 || waits != 0 {
		t.Fatalf("mixed replay gained a retry: reads=%d waits=%d err=%v", reads, waits, err)
	}
}
