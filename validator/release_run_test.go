// Deterministic tests for release-validator startup retry boundaries.
package validator

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

// Only conditions that can recover without a configuration change are retried.
func TestTransientReleaseSnapshotErrorSeparatesCapacityFromContractFailure(t *testing.T) {
	transientErrors := []error{
		context.DeadlineExceeded,
		gethrpc.HTTPError{StatusCode: http.StatusTooManyRequests, Status: "429 Too Many Requests"},
		errors.New("rpc: upstream overloaded"),
		errors.New("read: connection reset by peer"),
	}
	for _, err := range transientErrors {
		if !transientReleaseSnapshotError(err) {
			t.Errorf("transient error was classified permanent: %v", err)
		}
	}
	if !transientReleaseSnapshotError(errors.New(`attempt upload response status is 502: "upstream timeout"`)) {
		t.Fatal("transient immutable upload 502 was classified permanent")
	}
	for _, err := range []error{context.Canceled, errors.New("execution reverted: InvalidPolicy"), errors.New("abi: cannot unmarshal tuple")} {
		if transientReleaseSnapshotError(err) {
			t.Errorf("permanent error was classified transient: %v", err)
		}
	}
}

func TestInitialReleasePublicationRetriesTransientUploadWithoutRefolding(t *testing.T) {
	initial := &ReleaseSnapshot{}
	fresh := &ReleaseSnapshot{}
	advances := 0
	loads := 0
	waits := 0
	gotSnapshots := make([]*ReleaseSnapshot, 0, 3)
	err := advanceInitialReleaseWithRetry(context.Background(), initial, func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		return fresh, nil
	}, func(_ context.Context, snapshot *ReleaseSnapshot) error {
		advances++
		gotSnapshots = append(gotSnapshots, snapshot)
		if advances < 3 {
			return errors.New("attempt upload response status is 502: upstream timeout")
		}
		return nil
	}, func(_ context.Context, delay time.Duration) error {
		waits++
		if delay != releaseSnapshotStartupRetryDelay {
			t.Fatalf("retry delay=%s, want=%s", delay, releaseSnapshotStartupRetryDelay)
		}
		return nil
	})
	if err != nil || advances != 3 || loads != 2 || waits != 2 || gotSnapshots[0] != initial || gotSnapshots[1] != fresh || gotSnapshots[2] != fresh {
		t.Fatalf("error=%v advances=%d loads=%d waits=%d snapshots=%p", err, advances, loads, waits, gotSnapshots)
	}
}

func TestInitialReleasePublicationFailsPermanentUploadIntegrityError(t *testing.T) {
	advances := 0
	waits := 0
	want := errors.New("attempt upload response status is 409: immutable object differs")
	err := advanceInitialReleaseWithRetry(context.Background(), &ReleaseSnapshot{}, func(context.Context) (*ReleaseSnapshot, error) {
		t.Fatal("permanent publication error loaded a replacement snapshot")
		return nil, nil
	}, func(context.Context, *ReleaseSnapshot) error {
		advances++
		return want
	}, func(context.Context, time.Duration) error {
		waits++
		return nil
	})
	if !errors.Is(err, want) || advances != 1 || waits != 0 {
		t.Fatalf("error=%v advances=%d waits=%d", err, advances, waits)
	}
}

// A short public-provider outage must not force the supervisor to restart an
// otherwise valid validator process.
func TestInitialReleaseSnapshotRetriesTransientFailureWithoutRestart(t *testing.T) {
	want := &ReleaseSnapshot{}
	loads := 0
	waits := 0
	load := func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		if loads < 3 {
			return nil, context.DeadlineExceeded
		}
		return want, nil
	}
	wait := func(_ context.Context, delay time.Duration) error {
		waits++
		if delay != releaseSnapshotStartupRetryDelay {
			t.Fatalf("retry delay = %s, want %s", delay, releaseSnapshotStartupRetryDelay)
		}
		return nil
	}
	got, err := loadInitialReleaseSnapshot(context.Background(), load, wait)
	if err != nil || got != want || loads != 3 || waits != 2 {
		t.Fatalf("snapshot=%p error=%v loads=%d waits=%d", got, err, loads, waits)
	}
}

// Contract and ABI failures remain fail-fast so retries cannot hide drift.
func TestInitialReleaseSnapshotFailsPermanentErrorImmediately(t *testing.T) {
	wantErr := errors.New("execution reverted: InvalidPolicy")
	loads := 0
	waits := 0
	_, err := loadInitialReleaseSnapshot(context.Background(), func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		return nil, wantErr
	}, func(context.Context, time.Duration) error {
		waits++
		return nil
	})
	if !errors.Is(err, wantErr) || loads != 1 || waits != 0 {
		t.Fatalf("error=%v loads=%d waits=%d", err, loads, waits)
	}
}

// A provider that stays unavailable cannot leave startup retrying forever.
func TestInitialReleaseSnapshotBoundsPersistentTransientFailure(t *testing.T) {
	loads := 0
	waits := 0
	_, err := loadInitialReleaseSnapshot(context.Background(), func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		return nil, context.DeadlineExceeded
	}, func(context.Context, time.Duration) error {
		waits++
		return nil
	})
	if err == nil || loads != releaseSnapshotStartupAttempts || waits != releaseSnapshotStartupAttempts-1 {
		t.Fatalf("error=%v loads=%d waits=%d", err, loads, waits)
	}
}

// Lifecycle cancellation wins before another retry delay or RPC attempt.
func TestInitialReleaseSnapshotStopsOnParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loads := 0
	_, err := loadInitialReleaseSnapshot(ctx, func(context.Context) (*ReleaseSnapshot, error) {
		loads++
		return nil, context.DeadlineExceeded
	}, func(context.Context, time.Duration) error {
		t.Fatal("canceled startup waited for another attempt")
		return nil
	})
	if !errors.Is(err, context.Canceled) || loads != 1 {
		t.Fatalf("error=%v loads=%d", err, loads)
	}
}

// Four production workers share one request budget; retries do not multiply
// the configured per-minute rate because every SEED attempt is spaced by the
// same policy-derived gate with explicit headroom.
func TestReleaseSeedAttemptIntervalReservesHardLimitHeadroom(t *testing.T) {
	tests := []struct {
		hardLimit int
		want      time.Duration
	}{
		{hardLimit: 40, want: 2 * time.Second},
		{hardLimit: 4, want: 20 * time.Second},
		{hardLimit: 2, want: time.Minute},
		{hardLimit: 1, want: time.Minute},
	}
	for _, test := range tests {
		got, err := releaseSeedAttemptInterval(test.hardLimit)
		if err != nil || got != test.want {
			t.Errorf("hard limit %d interval = %s, error = %v, want %s", test.hardLimit, got, err, test.want)
		}
	}
	if _, err := releaseSeedAttemptInterval(0); err == nil {
		t.Fatal("zero hard seed rate limit was accepted")
	}
}

// Native startup allows a full ten public block intervals for the metadata
// handshake, while a deliberately slower configured polling window can widen
// that bounded budget. It never relies on the former 15-second constant.
func TestReleaseNativeEndpointTimeoutReservesMetadataHeadroom(t *testing.T) {
	blockBudget := time.Duration(releaseExpectedBlockSeconds*releaseNativeAuthenticationBlocks) * time.Second
	for _, test := range []struct {
		name        string
		cfg         *ReleaseConfig
		wantTimeout time.Duration
	}{
		{name: "nil config", wantTimeout: blockBudget},
		{name: "default polling", cfg: &ReleaseConfig{PollSeconds: 3}, wantTimeout: blockBudget},
		{name: "slow configured polling", cfg: &ReleaseConfig{PollSeconds: 60}, wantTimeout: 4 * time.Minute},
	} {
		if got := releaseNativeEndpointTimeout(test.cfg); got != test.wantTimeout {
			t.Errorf("%s: native endpoint timeout=%s, want %s", test.name, got, test.wantTimeout)
		}
	}
}

type failingReleaseTrailRunner struct{ err error }

func (runner failingReleaseTrailRunner) Run(context.Context, int) error { return runner.err }

func TestReleaseTrailDurabilityFailureReachesProcessLifecycle(t *testing.T) {
	want := errors.New("durable attempt ledger failed")
	output := make(chan error, 1)
	reportReleaseTrailEngineError(context.Background(), failingReleaseTrailRunner{err: want}, 2, 4, output)
	select {
	case err := <-output:
		if !errors.Is(err, want) || err.Error() != "validator no_id 2 trail engine: durable attempt ledger failed" {
			t.Fatalf("error = %v", err)
		}
	default:
		t.Fatal("trail durability failure was not reported")
	}
}

func TestReleaseTrailCancellationDoesNotReportAnExpectedShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	output := make(chan error, 1)
	reportReleaseTrailEngineError(ctx, failingReleaseTrailRunner{err: context.Canceled}, 1, 1, output)
	select {
	case err := <-output:
		t.Fatalf("expected shutdown was reported as a failure: %v", err)
	default:
	}
}
