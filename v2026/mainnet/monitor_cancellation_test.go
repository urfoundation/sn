// Cancellation barriers run after actual physical admission, never in place
// of custody checks. Hard causes and close failures retain their own authority.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

func TestMonitorEconomicEvmPublicCancellationDuringChainCheckpointLoad(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	entered := make(chan struct{})
	evmExit := make(chan int, 1)
	run := fixture.start(t, monitorServiceHooks{afterCheckpointOpen: func(ctx context.Context, role string, file *os.File) {
		if role != "chain" {
			return
		}
		if file == nil {
			panic("checkpoint barrier ran without a retained file")
		}
		close(entered)
		<-ctx.Done()
	}, afterWorker: func(role string, exit int) {
		if role == fixture.policy.Role {
			evmExit <- exit
		}
	}})
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("chain checkpoint did not reach actual admitted descriptor barrier")
	}
	if event := run.next(t); !event.Current || event.State.Cursor.Number != 13 {
		t.Fatal("independent EVM role did not complete while chain load waited", event)
	}
	run.stop(t)
	if exit := <-evmExit; exit != 0 || run.exit != 0 {
		t.Fatal("canceled retained chain load became a terminal custody failure", exit, run.exit, run.diagnostic.String())
	}
	record := fixture.record(t)
	if record.State.Cursor.Number != 13 || record.State.Snapshot == nil || record.State.Snapshot.Counters["totalPaid"] != "15" {
		t.Fatal("joined cancellation lost completed independent economic evidence", record)
	}
}

func TestMonitorCheckpointCancellationRetainsHardCausePrecedence(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if monitorCanceledCheckpointLoad(ctx, context.DeadlineExceeded) || monitorCanceledCheckpointLoad(ctx, context.Canceled) {
		t.Fatal("active owner treated attempt timeout as joined cancellation")
	}
	cancel()
	if !monitorCanceledCheckpointLoad(ctx, context.Canceled) {
		t.Fatal("actual owner cancellation was not recognized")
	}
	for _, cause := range []error{errors.Join(context.Canceled, context.DeadlineExceeded), errors.Join(fmt.Errorf("child: %w", context.DeadlineExceeded), fmt.Errorf("parent: %w", context.Canceled))} {
		if !monitorCanceledCheckpointLoad(ctx, cause) {
			t.Fatal("joined child deadline and canceled owner invented an independent failure", cause)
		}
	}
	for _, hard := range []error{syscall.EIO, durablevolume.ErrIdentity, errRpcIntegrity, errRpcIdentityMismatch, &monitorOutputOwnershipError{reason: "synthetic proven named replacement"}} {
		if monitorCanceledCheckpointLoad(ctx, errors.Join(context.Canceled, context.DeadlineExceeded, hard)) {
			t.Fatal("cancellation erased an independently observed hard cause", hard)
		}
	}
	if monitorCanceledCheckpointLoad(ctx, errors.New("checkpoint checksum differs")) {
		t.Fatal("parent cancellation erased malformed retained checkpoint")
	}
}

func TestMonitorEconomicEvmPublicAdjacentAdmissionCancellationJoinsOwners(t *testing.T) {
	for _, kind := range []string{"validator", "provider", "claim", "evm"} {
		fixture := newMonitorEvmFixture(t, "settlement-vault", true)
		role := "validator-a"
		switch kind {
		case "provider":
			value := monitorProviderTestValue(fixture.services.clock.now())
			policy := monitorProviderTestPolicy(value, "https://synthetic.invalid/provider-progress")
			fixture.services.policy.Providers = []monitorProviderPolicy{policy}
			role = policy.Role
			path, _ := monitorProviderPaths(fixture.services.checkpointPath, fixture.services.metricsPath, role)
			provisionMonitorTestCustody(t, path)
		case "claim":
			value := monitorClaimTestValue(fixture.services.clock.now())
			policy := monitorClaimPolicy{Role: "claim-a", Endpoint: "https://synthetic.invalid/claim-progress", ExpectedMember: value.Member, ExpectedPool: *value.DeclaredPool, FreshnessSeconds: 60, Epochs: []monitorClaimEpochPolicy{{Epoch: 7, ShareBps: 5000, AcceptBy: fixture.services.clock.now().Add(time.Hour).Format(time.RFC3339Nano)}}}
			fixture.services.policy.Claims = []monitorClaimPolicy{policy}
			role = policy.Role
			path, _ := monitorClaimPaths(fixture.services.checkpointPath, fixture.services.metricsPath, role)
			provisionMonitorTestCustody(t, path)
		case "evm":
			role = fixture.policy.Role
		}
		entered := make(chan struct{})
		closes := make(chan string, 16)
		type roleResult struct {
			role string
			exit int
		}
		results := make(chan roleResult, 4)
		run := fixture.start(t, monitorServiceHooks{afterCheckpointOpen: func(ctx context.Context, opened string, file *os.File) {
			if opened == role {
				if file == nil {
					panic("admission barrier ran without actual owner")
				}
				close(entered)
				<-ctx.Done()
			}
		}, afterClose: func(closedRole, owner string, _ *os.File) error {
			if closedRole == role {
				closes <- owner
			}
			return nil
		}, afterWorker: func(role string, exit int) {
			results <- roleResult{role: role, exit: exit}
		}})
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("adjacent role did not admit real checkpoint", kind)
		}
		run.stop(t)
		close(results)
		joined := map[string]int{}
		for result := range results {
			joined[result.role] = result.exit
		}
		if run.exit != 0 || len(closes) != 2 {
			t.Fatal("canceled adjacent admission was fatal or left owners unjoined", kind, run.exit, len(closes), joined, run.diagnostic.String())
		}
		if exit, present := joined[role]; !present || exit != 0 {
			t.Fatal("canceled actual role did not report a joined lifecycle", kind, joined)
		}
		select {
		case event := <-run.sink.events:
			if kind == "evm" {
				t.Fatal("canceled EVM admission invented an economic sample", kind, event)
			}
			// The independent EVM role can publish while another role waits.
			if event.State.Cursor.Number != 13 {
				t.Fatal("peer publication lost its original cursor", kind, event)
			}
		default:
		}
	}
}

func TestMonitorNativePublicAdmissionCancellationKeepsCloseFailure(t *testing.T) {
	for _, closeFailure := range []bool{false, true} {
		fixture := newMonitorEconomicTestFixture(t, false)
		entered := make(chan struct{})
		run := fixture.start(t, monitorServiceHooks{afterCheckpointOpen: func(ctx context.Context, role string, file *os.File) {
			if role == fixture.policy.Role {
				if file == nil {
					panic("native admission lacks actual descriptor")
				}
				close(entered)
				<-ctx.Done()
			}
		}, afterClose: func(role, kind string, _ *os.File) error {
			if closeFailure && role == fixture.policy.Role && kind == "checkpoint" {
				return syscall.EIO
			}
			return nil
		}})
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("native real admission barrier not reached")
		}
		run.stop(t)
		expected := 0
		if closeFailure {
			expected = 3
		}
		if run.exit != expected {
			t.Fatal("native cancellation lost ordinary stop or genuine joined close failure", closeFailure, run.exit)
		}
	}
}

// Named metadata observation must not add a fabricated physical outage to a
// pure caller cancellation. Independent read/custody/cleanup errors dominate.
func TestMonitorCheckpointNamedObservationPreservesCancellationCauses(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, cause := range []error{context.Canceled, fmt.Errorf("named read: %w", context.Canceled), errors.Join(context.Canceled, fmt.Errorf("named postcheck: %w", context.Canceled)), errors.Join(context.Canceled, context.DeadlineExceeded), errors.Join(fmt.Errorf("child read: %w", context.DeadlineExceeded), fmt.Errorf("parent read: %w", context.Canceled))} {
		observed := monitorNamedObservation(cause)
		if !monitorCanceledCheckpointLoad(ctx, observed) || errors.Is(observed, durablevolume.ErrUnavailable) || errors.Is(observed, durablevolume.ErrIdentity) {
			t.Fatal("pure named-read cancellation acquired an invented custody cause", observed)
		}
	}
	deadline := fmt.Errorf("named read: %w", context.DeadlineExceeded)
	if observed := monitorNamedObservation(deadline); observed != deadline || !monitorStartupPending(observed) {
		t.Fatal("active named-read timeout lost its original retryable cause", observed)
	}
	for _, hard := range []error{syscall.EIO, durablevolume.ErrIdentity, errRpcIntegrity, &monitorAdmissionCleanupError{cause: syscall.EIO}} {
		observed := monitorNamedObservation(errors.Join(context.Canceled, context.DeadlineExceeded, hard))
		if monitorCanceledCheckpointLoad(ctx, observed) || !errors.Is(observed, hard) {
			t.Fatal("named-read cancellation erased an independent observed failure", observed)
		}
	}
}
