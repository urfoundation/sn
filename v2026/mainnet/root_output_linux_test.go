//go:build linux

// Physical full pipes exercise the public command's actual descriptor adapter.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A separate nonblocking handle fills the pipe without changing caller flags.
func rootOutputFullPipe(t *testing.T) (*os.File, *os.File, int) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close(); writer.Close() })
	connection, err := writer.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	flags := 0
	var fillErr error
	err = connection.Control(func(raw uintptr) {
		flags, fillErr = unix.FcntlInt(raw, unix.F_GETFL, 0)
		if fillErr != nil {
			return
		}
		fd, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", raw), unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		if err != nil {
			fillErr = err
			return
		}
		defer unix.Close(fd)
		data := make([]byte, 4096)
		for count := 0; count < 1024; count++ {
			_, err = unix.Write(fd, data)
			if errors.Is(err, unix.EAGAIN) {
				return
			}
			if err != nil {
				fillErr = err
				return
			}
		}
		fillErr = errors.New("fixture pipe exceeded bounded fill")
	})
	if err != nil || fillErr != nil {
		t.Fatal("physical pipe fixture failed", err, fillErr)
	}
	return reader, writer, flags
}

// Real command samples/checkpoints/metrics continue while both log streams are
// physically full. Cancellation releases the file owners and diagnostic handle.
func TestRootMonitorOutputPhysicalBlockedSinkKeepsFilesAndJoins(t *testing.T) {
	_, fixture := newRootFixture(t)
	server := rootFixtureServer(t, fixture)
	reader, writer, originalFlags := rootOutputFullPipe(t)
	directory := monitorMetricsTestDir(t)
	checkpoint, metrics := filepath.Join(directory, "root.json"), filepath.Join(directory, "root.prom")
	clock := &monitorServicesTestClock{}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock.seconds.Store(base.Unix())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	completed, resume := make(chan struct{}), make(chan struct{})
	hooks := monitorServiceHooks{
		afterEvent: func(ctx context.Context, _ string) {
			select {
			case completed <- struct{}{}:
			case <-ctx.Done():
				return
			}
			select {
			case <-resume:
			case <-ctx.Done():
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool { return ctx.Err() == nil },
	}
	done := make(chan int, 1)
	args := []string{"root-monitor", "--rpc", server.URL, "--policy", rootTestPolicyFile(t, fixture.policy), "--checkpoint", checkpoint, "--metrics-file", metrics, "--metrics-role", "primary", "--samples", "8"}
	go func() { done <- runMainWithMonitorHooks(ctx, args, writer, writer, clock.now, hooks) }()
	for sample := 0; sample < 3; sample++ {
		<-completed
		values := monitorOutputTestMetrics(t, metrics)
		if values[`sn_mainnet_root_monitor_sample_timestamp_seconds{role="primary"}`] != float64(base.Unix()+int64(sample)*10) || values[`sn_mainnet_root_monitor_current_observation{role="primary"}`] != 1 || values[`sn_mainnet_root_monitor_read_only_ready{role="primary"}`] != 1 {
			t.Fatal("physically blocked output starved root observation")
		}
		if values[`sn_mainnet_root_monitor_finalized_progress_timestamp_seconds{role="primary"}`] != float64(base.Unix()) || values[`sn_mainnet_root_monitor_output_delivered_total{role="primary",stream="events"}`] != 0 {
			t.Fatal("output or unchanged head fabricated progress")
		}
		if sample < 2 {
			clock.seconds.Add(10)
			resume <- struct{}{}
		}
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatal("physical output fault changed cancellation exit", code)
	}
	connection, err := writer.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	flags := 0
	if err := connection.Control(func(raw uintptr) { flags, err = unix.FcntlInt(raw, unix.F_GETFL, 0) }); err != nil {
		t.Fatal(err)
	}
	if err != nil || flags != originalFlags {
		t.Fatal("command changed caller descriptor flags")
	}
	// Closing the caller's write end must produce EOF after draining the exact
	// prefill. An unjoined duplicate would keep this actual read open forever.
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(make([]byte, 4096)); err != nil {
		t.Fatal("prefill disappeared", err)
	}
	if err := reader.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4096)
	for count := 0; count < 1024; count++ {
		_, err := reader.Read(buffer)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatal("root output descriptor remained live after command joined")
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatal("root pipe drain failed", err)
			}
			break
		}
		if count == 1023 {
			t.Fatal("root pipe drain exceeded fixture bound")
		}
	}
	store, err := openMonitorCheckpoint(checkpoint, identityExpectation{NativeChain: fixture.policy.NativeChain, GenesisHash: fixture.policy.GenesisHash, EvmChainId: 964})
	if err != nil {
		t.Fatal("root checkpoint owner leaked")
	}
	store.close()
}
