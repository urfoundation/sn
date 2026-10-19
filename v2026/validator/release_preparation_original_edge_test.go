// A preparation decision owns one observation of each original error edge.
// A later mutable child cannot replace the hard cause already observed.
package validator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// The second child would turn the original hard result into a preparation wait.
type releasePreparationChangingCause struct {
	first        error
	later        error
	observations int
}

// Formatting does not itself observe or change the original edge.
func (*releasePreparationChangingCause) Error() string { return "synthetic changing preparation edge" }

// Deliberately expose a different child if a classifier asks twice.
func (self *releasePreparationChangingCause) Unwrap() error {
	self.observations++
	if self.observations == 1 {
		return self.first
	}
	return self.later
}

// Joined originals exercise the separate multi-cause branch with the same rule.
type releasePreparationChangingJoin struct {
	first        []error
	later        []error
	observations int
}

// Keep the diagnostic independent of the mutable child vector.
func (*releasePreparationChangingJoin) Error() string { return "synthetic changing preparation join" }

// A replacement vector must never be consulted after its hard original.
func (self *releasePreparationChangingJoin) Unwrap() []error {
	self.observations++
	if self.observations == 1 {
		return self.first
	}
	return self.later
}

// The actual pre-intent marker producer must refuse its first hard observation,
// even if a second traversal could replace it with a mixed cut/read retry.
func TestReleasePreIntentPreparationKeepsFirstObservedHardCause(t *testing.T) {
	hard := &os.PathError{Op: "read", Path: "synthetic-original-intent", Err: context.DeadlineExceeded}
	for _, later := range []error{errAttemptCutPending, errors.Join(errAttemptCutPending, context.DeadlineExceeded)} {
		probe := &releasePreparationChangingCause{first: hard, later: later}
		if retry, transport := classifyReleasePreparationRetry(probe); retry || transport || probe.observations != 1 {
			t.Fatalf("preparation replaced its original hard edge: retry=%t transport=%t observations=%d", retry, transport, probe.observations)
		}
		probe = &releasePreparationChangingCause{first: hard, later: later}
		original := fmt.Errorf("original assembly: %w", probe)
		if result := classifyProvisionalNativeRead(true, 7, original); !errors.Is(result, original) || releaseErrorMarker[*provisionalNativeReadInterruption](result) != nil || probe.observations != 1 {
			t.Fatalf("pre-intent owner minted authority from a replacement edge: observations=%d", probe.observations)
		}
	}
}

// A joined graph also retains its first full child vector. The positive control
// proves that a valid first vector does not get replaced by a later hard one.
func TestReleasePreparationKeepsFirstObservedJoinedCauses(t *testing.T) {
	hard := &os.PathError{Op: "write", Path: "synthetic-original-journal", Err: context.Canceled}
	valid := []error{errAttemptCutPending, &attemptReplicaPublicationError{causes: []error{context.DeadlineExceeded, context.Canceled}}}
	probe := &releasePreparationChangingJoin{first: []error{hard}, later: valid}
	if retry, transport := classifyReleasePreparationRetry(probe); retry || transport || probe.observations != 1 {
		t.Fatal("preparation replaced an original hard joined cause")
	}
	probe = &releasePreparationChangingJoin{first: valid, later: []error{hard}}
	if retry, transport := classifyReleasePreparationRetry(probe); !retry || !transport || probe.observations != 1 {
		t.Fatal("preparation did not decide from its single complete original vector")
	}
}

// Separately owned publication transport can wait beside a cut through the
// real legacy loop, without consuming its hard-failure budget or resubmitting.
func TestReleasePreparationOriginalMixedOwnersKeepLoopProgress(t *testing.T) {
	original := errors.Join(errAttemptCutPending, &attemptReplicaPublicationError{causes: []error{context.DeadlineExceeded, context.Canceled}})
	reads, submissions := 0, 0
	err := runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) { reads++; return 7, nil }, func() error {
		submissions++
		if submissions <= releaseSteeringFailureLimit+1 {
			return original
		}
		return nil
	}, func() bool { return reads < releaseSteeringFailureLimit+3 }, true)
	if err != nil || submissions != releaseSteeringFailureLimit+2 || reads != releaseSteeringFailureLimit+3 {
		t.Fatalf("one-pass preparation lost valid mixed-owner progress: submissions=%d reads=%d error=%v", submissions, reads, err)
	}
}

// Publication sibling cancellation belongs to actual publication transport.
// A cut inside that subtree, or a bare sibling cancellation, grants no retry.
func TestReleasePreparationCannotBorrowTransportOwnerCancellation(t *testing.T) {
	for _, cause := range []error{
		&attemptReplicaPublicationError{causes: []error{context.Canceled}},
		&attemptReplicaPublicationError{causes: []error{errAttemptCutPending}},
		&attemptReplicaPublicationError{causes: []error{errAttemptCutPending, context.DeadlineExceeded, context.Canceled}},
		errors.Join(errAttemptCutPending, context.DeadlineExceeded, context.Canceled),
		&net.DNSError{Name: "resolver.example", UnwrapErr: errAttemptCutPending},
		&crv4.SubstrateReadHttpStatusError{},
	} {
		if retry, transport := classifyReleasePreparationRetry(cause); retry || transport {
			t.Fatal("preparation borrowed a different reader's retry or cancellation authority")
		}
	}
}

// The retained-read owner shares its first valid transport observation with
// marker search and the actual loop; a hypothetical later edge is never read.
func TestProductionRetainedReadFailurePreservesFirstMarkerObservation(t *testing.T) {
	cycle := &releaseCauseJoin{}
	cycle.causes = []error{cycle}
	probe := &releasePreparationChangingCause{first: context.DeadlineExceeded, later: cycle}
	self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
	result := self.productionRetainedReadFailure(t.Context(), productionReadReceipt, nil, probe)
	retained, ok := result.(*productionSteeringReadWait)
	if !ok || !errors.Is(retained.cause, probe) || probe.observations != 1 || cycle.visits != 0 {
		t.Fatal("retained read marker search changed its first complete original")
	}
	submissions := 0
	err := runReleaseProductionSteeringLoopWithWait(t.Context(), func() error { submissions++; return result }, func() bool { return false }, nil)
	if err != nil || submissions != 1 || probe.observations != 1 || cycle.visits != 0 {
		t.Fatal("production discarded or reread the first complete transport original")
	}
}
