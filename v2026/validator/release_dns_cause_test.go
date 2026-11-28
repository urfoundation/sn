// Concrete resolver results retain their own flags and complete underlying
// causes across actual startup, preparation and production read owners.
package validator

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

// A real standard resolver timeout may have no child. Each owner recovers
// through its normal retry path; the test replaces only the outbound read.
func TestReleaseReadOwnersRetryConcreteDnsTimeoutWithoutChild(t *testing.T) {
	for _, cause := range []error{
		&net.DNSError{Name: "resolver.example", Err: "synthetic resolver timeout", IsTimeout: true},
		&net.DNSError{Name: "resolver.example", Err: "synthetic resolver unavailable", IsTemporary: true},
		&net.DNSError{Name: "resolver.example", UnwrapErr: context.DeadlineExceeded},
		&net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ETIMEDOUT}},
		syscall.EAGAIN,
	} {
		want := &ReleaseSnapshot{}
		loads, waits := 0, 0
		got, err := loadInitialReleaseSnapshot(t.Context(), func(context.Context) (*ReleaseSnapshot, error) {
			loads++
			if loads == 1 {
				return nil, cause
			}
			return want, nil
		}, func(context.Context, time.Duration) error { waits++; return nil })
		if err != nil || got != want || loads != 2 || waits != 1 {
			t.Fatalf("startup lost concrete transport recovery: loads=%d waits=%d error=%v", loads, waits, err)
		}
		self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
		reads, polls := 0, 0
		self.productionReadHooks.wait = func(context.Context, time.Duration) error { polls++; return nil }
		err = self.productionRead(t.Context(), productionReadReceipt, nil, func(context.Context) error {
			reads++
			if reads == 1 {
				return cause
			}
			return nil
		})
		if err != nil || reads != 2 || polls != 1 {
			t.Fatalf("production read lost concrete transport recovery: reads=%d polls=%d error=%v", reads, polls, err)
		}
		attempts := 0
		err = runReleaseSteeringLoopWithWaitAndDeferral(t.Context(), func() (uint64, error) { return 7, nil }, func() error {
			attempts++
			if attempts <= releaseSteeringFailureLimit+1 {
				return cause
			}
			return nil
		}, func() bool { return attempts < releaseSteeringFailureLimit+2 }, true)
		if err != nil || attempts != releaseSteeringFailureLimit+2 {
			t.Fatalf("preparation spent hard failures on a concrete transport result: attempts=%d error=%v", attempts, err)
		}
	}
}

// Completed negative resolver answers and actual child defects take precedence
// over timeout flags. No read owner may hide them in a retryable wait.
func TestReleaseReadOwnersRejectDnsNegativeAndHardChildren(t *testing.T) {
	var typedNil *net.DNSError
	cycle := &releaseCauseJoin{}
	cycle.causes = []error{cycle}
	for _, cause := range []error{
		typedNil,
		&net.DNSError{Name: "resolver.example", IsNotFound: true, IsTimeout: true, IsTemporary: true},
		&net.DNSError{Name: "resolver.example", IsNotFound: true, UnwrapErr: context.DeadlineExceeded},
		&net.DNSError{Name: "resolver.example"},
		&net.DNSError{Name: "resolver.example", IsTimeout: true, UnwrapErr: &os.PathError{Op: "read", Path: "synthetic-resolver-state", Err: context.DeadlineExceeded}},
		&net.DNSError{Name: "resolver.example", IsTimeout: true, UnwrapErr: &os.LinkError{Op: "rename", Old: "synthetic-a", New: "synthetic-b", Err: context.DeadlineExceeded}},
		&net.DNSError{Name: "resolver.example", IsTimeout: true, UnwrapErr: cycle},
		&net.OpError{Op: "read", Net: "tcp", Err: &releaseCauseMatcher{cause: context.DeadlineExceeded}},
	} {
		loads, waits := 0, 0
		got, err := loadInitialReleaseSnapshot(t.Context(), func(context.Context) (*ReleaseSnapshot, error) {
			loads++
			return nil, cause
		}, func(context.Context, time.Duration) error { waits++; return context.Canceled })
		if got != nil || err != cause || loads != 1 || waits != 0 {
			t.Fatalf("startup downgraded a completed or malformed resolver result: loads=%d waits=%d", loads, waits)
		}
		self := &ReleaseSteerer{cfg: &ReleaseConfig{SchemaVersion: ReleaseMainnetProductionSchemaVersion}}
		reads, polls := 0, 0
		self.productionReadHooks.wait = func(context.Context, time.Duration) error { polls++; return context.Canceled }
		err = self.productionRead(t.Context(), productionReadReceipt, nil, func(context.Context) error { reads++; return cause })
		if err == nil || reads != 1 || polls != 0 || retryableProductionSteeringRead(err) {
			t.Fatalf("production read hid a completed or malformed resolver result: reads=%d polls=%d", reads, polls)
		}
		if retry, transport := classifyReleasePreparationRetry(cause); retry || transport {
			t.Fatal("preparation reopened the refused resolver subtree")
		}
	}
}

// The artifact reader's own matching wrapper preserves generic startup and
// preparation compatibility. Its pending projection never replaces its cause.
func TestReleaseKnownArtifactWrapperPreservesCompleteTransportCause(t *testing.T) {
	original := newArtifactUnavailable(context.DeadlineExceeded)
	want := &ReleaseSnapshot{}
	loads, waits := 0, 0
	got, err := loadInitialReleaseSnapshot(t.Context(), func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		if loads == 1 {
			return nil, original
		}
		return want, nil
	}, func(context.Context, time.Duration) error { waits++; return nil })
	if err != nil || got != want || loads != 2 || waits != 1 {
		t.Fatal("known original artifact wrapper lost generic startup recovery")
	}
	if retry, transport := classifyReleasePreparationRetry(original); !retry || !transport {
		t.Fatal("known original artifact wrapper lost preparation transport recovery")
	}
	for _, cause := range []error{
		&artifactUnavailable{pending: true, cause: &os.PathError{Op: "read", Path: "synthetic-artifact", Err: context.DeadlineExceeded}},
		&artifactUnavailable{pending: true, cause: errors.New("synthetic immutable content contradiction")},
		&artifactUnavailable{pending: true},
	} {
		if RetryableEvidenceTransportError(cause) || transientReleaseSnapshotError(cause) {
			t.Fatal("artifact pending projection concealed an absent or hard cause")
		}
		if retry, transport := classifyReleasePreparationRetry(cause); retry || transport {
			t.Fatal("preparation reopened a refused artifact original")
		}
	}
}
