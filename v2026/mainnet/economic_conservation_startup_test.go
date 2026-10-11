package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

type economicStartupCause struct {
	child error
	calls *int
}

func (self *economicStartupCause) Error() string { return "synthetic startup cause" }
func (self *economicStartupCause) Unwrap() error {
	*self.calls++
	return self.child
}
func (self *economicStartupCause) Is(error) bool { panic("custom Is must not run") }
func (self *economicStartupCause) As(any) bool   { panic("custom As must not run") }

type economicStartupMany struct{ children []error }

func (self economicStartupMany) Error() string   { return "synthetic startup join" }
func (self economicStartupMany) Unwrap() []error { return self.children }

func TestEconomicConservationStartupOnlyCompleteTransientGraphsCanRetry(t *testing.T) {
	calls := 0
	cycle := &economicStartupCause{calls: &calls}
	cycle.child = cycle
	var typedNil *economicStartupCause
	deep := error(syscall.EIO)
	for range 40 {
		deep = &economicStartupCause{child: deep, calls: &calls}
	}
	wide := make([]error, 129)
	for index := range wide {
		wide[index] = syscall.EIO
	}
	for _, test := range []struct {
		name  string
		cause error
		want  bool
	}{
		{name: "io", cause: &os.PathError{Op: "read", Path: "/synthetic/original", Err: syscall.EIO}, want: true},
		{name: "bounded-wrapper", cause: &economicStartupCause{child: syscall.EIO, calls: &calls}, want: true},
		{name: "busy", cause: fmt.Errorf("original owner: %w", durablevolume.ErrBusy), want: true},
		{name: "deadline", cause: errors.Join(context.DeadlineExceeded, syscall.EINTR), want: true},
		{name: "unavailable-io", cause: errors.Join(durablevolume.ErrUnavailable, syscall.EIO), want: true},
		{name: "identity", cause: errors.Join(syscall.EIO, durablevolume.ErrIdentity)},
		{name: "uncertain", cause: errors.Join(syscall.EIO, durablehead.ErrUncertain)},
		{name: "cleanup", cause: &monitorAdmissionCleanupError{cause: syscall.EIO}},
		{name: "named-owner", cause: &monitorOutputOwnershipError{reason: "synthetic owner changed"}},
		{name: "unknown", cause: errors.Join(syscall.EIO, errors.New("synthetic original authority differs"))},
		{name: "canceled", cause: context.Canceled},
		{name: "nil"},
		{name: "typed-nil", cause: typedNil},
		{name: "all-nil", cause: economicStartupMany{children: []error{nil, nil}}},
		{name: "cycle", cause: cycle},
		{name: "deep", cause: deep},
		{name: "wide", cause: economicStartupMany{children: wide}},
	} {
		before := calls
		if got := economicConservationStartupPending(test.cause); got != test.want || calls-before > 32 {
			t.Fatal("startup retry converted unknown or hard evidence", test.name, got, test.want, calls-before)
		}
	}
}

func TestEconomicConservationPublicFollowBusyOwnerRetriesWithoutPublishing(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	metrics := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
	args := append(f.args(t), "--metrics-file", metrics, "--follow")
	ctx := monitorTestStorageContext(t, t.Context(), args)
	network := f.policy.Native.Observation.Network
	owner, err := f.policy.openCheckpoint(ctx, f.checkpoint, identityExpectation{NativeChain: network.NativeChain, GenesisHash: network.GenesisHash, EvmChainId: network.EvmChainId})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	var output, diagnostic bytes.Buffer
	code := runMainWithMonitorHooks(ctx, args, &output, &diagnostic, func() time.Time { return f.now }, monitorServiceHooks{})
	if code != 1 || output.Len() != 0 || f.claimReads.Load() != 0 {
		t.Fatal("busy original follow owner became permanent or published evidence", code, output.String(), diagnostic.String())
	}
	if _, err := os.Stat(metrics); !os.IsNotExist(err) {
		t.Fatal("unadmitted original owner refreshed metrics", err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	diagnostic.Reset()
	code = runMainWithMonitorHooks(ctx, args, &output, &diagnostic, func() time.Time { return f.now }, monitorServiceHooks{wait: func(context.Context, string, time.Duration) bool { return false }})
	if code != 0 || output.Len() == 0 || f.state(t).Native.Cursor.Number != 102 || economicMetricsTestFile(t, metrics)["sample_healthy"] != 1 {
		t.Fatal("released original owner did not resume actual public reads", code, diagnostic.String())
	}
}
