//go:build linux

// A subprocess gives the actual legacy stdout call its own physically full
// descriptor. The parent never redirects process-global output in its test tree.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026"
	"golang.org/x/sys/unix"
)

// Inspect the original flags without File.Fd changing its Go polling mode.
func trailDiagnosticFileFlags(t *testing.T, file *os.File) int {
	t.Helper()
	connection, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags int
	var readErr error
	err = connection.Control(func(fd uintptr) { flags, readErr = unix.FcntlInt(fd, unix.F_GETFL, 0) })
	if err := errors.Join(err, readErr); err != nil {
		t.Fatal(err)
	}
	return flags
}

// EAGAIN establishes actual congestion before the worker process is started.
// A separate nonblocking handle leaves the caller's flags untouched.
func trailDiagnosticFullPipe(t *testing.T) (*os.File, *os.File) {
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
	var fillErr error
	err = connection.Control(func(raw uintptr) {
		fd, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", raw), unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		if err != nil {
			fillErr = err
			return
		}
		defer unix.Close(fd)
		buffer := make([]byte, 4096)
		for range 1024 {
			_, err := unix.Write(fd, buffer)
			if errors.Is(err, unix.EAGAIN) {
				return
			}
			if err != nil {
				fillErr = err
				return
			}
		}
		fillErr = errors.New("synthetic pipe exceeded bounded fill")
	})
	if err := errors.Join(err, fillErr); err != nil {
		t.Fatal(err)
	}
	return reader, writer
}

// A genuine complete trail reaches the next picker and cancellation, despite
// physical stdout congestion. Its original signed proof and ledger survive.
func trailDiagnosticStdoutChild(t *testing.T) {
	finished := os.NewFile(3, "synthetic-trail-result")
	defer finished.Close()
	defer func() {
		result := byte(1)
		if t.Failed() {
			result = 0
		}
		_, _ = finished.Write([]byte{result})
	}()
	fixture := newTrailDiagnosticFixture(t)
	flags := trailDiagnosticFileFlags(t, os.Stdout)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx, owner, err := newReleaseDiagnostics(ctx, os.Stdout, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	pick := fixture.engine.pickSeed
	calls := 0
	fixture.engine.pickSeed = func(ctx context.Context) (connect.Id, error) {
		calls++
		if calls == 2 {
			cancel()
			return connect.Id{}, ctx.Err()
		}
		return pick(ctx)
	}
	if err := fixture.engine.Run(ctx, 1); err != nil {
		t.Fatal("physical stdout changed trail termination")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	state := owner.snapshot(time.Now())
	records, skipped, err := fixture.store.Load()
	if err != nil || skipped != 0 || len(records) != 1 || fixture.ledger.LastSequence() != 8 || calls != 2 || fixture.engine.Completed() != 1 || state.Runtime.Delivered != 0 || state.Runtime.Dropped != 1 {
		t.Fatal("physical stdout changed bounded delivery or original trail proof")
	}
	if trailDiagnosticFileFlags(t, os.Stdout) != flags {
		t.Fatal("trail exporter changed original stdout flags")
	}
}

// The child shares only the retained test executable, never global output
// mutations. Its one-byte completion barrier precedes any pipe drain. A timeout
// is a generous deadlock backstop; congestion itself is established by EAGAIN.
func TestTrailDiagnosticsPhysicalStdoutCannotHoldRun(t *testing.T) {
	if os.Getenv("SN_TEST_TRAIL_BLOCKED_STDOUT") == "1" {
		trailDiagnosticStdoutChild(t)
		return
	}
	reader, writer := trailDiagnosticFullPipe(t)
	resultReader, resultWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer resultReader.Close()
	defer resultWriter.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTrailDiagnosticsPhysicalStdoutCannotHoldRun$", "-test.timeout=60s")
	command.Env = append(os.Environ(), "SN_TEST_TRAIL_BLOCKED_STDOUT=1")
	command.Stdout = writer
	var stderr bytes.Buffer
	command.Stderr = &stderr
	command.ExtraFiles = []*os.File{resultWriter}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	joined := false
	defer func() {
		if !joined {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	if err := errors.Join(writer.Close(), resultWriter.Close()); err != nil {
		t.Fatal(err)
	}
	result := make(chan byte, 1)
	go func() {
		var value [1]byte
		if _, err := io.ReadFull(resultReader, value[:]); err != nil {
			value[0] = 0
		}
		result <- value[0]
	}()
	var complete byte
	select {
	case complete = <-result:
	case <-ctx.Done():
		_ = command.Process.Kill()
		_ = command.Wait()
		joined = true
		<-result
		t.Fatal("physical stdout blocked trail continuation")
	}
	// Only a completed child may release stdout. This finite drain also makes
	// its ordinary Go test terminal result visible without leaking a descriptor.
	drained := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reader); drained <- err }()
	waitErr := command.Wait()
	joined = true
	if err := <-drained; err != nil {
		t.Fatal("completed child stdout failed to close", err)
	}
	if waitErr != nil || complete != 1 {
		t.Fatal("trail stdout child failed its real persistence checks", waitErr, stderr.String())
	}
}

// A disconnected physical destination is an optional operational fault, not
// permission to lose a completed proof or to repeat its original signed work.
func TestTrailDiagnosticsDisconnectedSinkStillStoresProof(t *testing.T) {
	fixture := newTrailDiagnosticFixture(t)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx, owner, err := newReleaseDiagnostics(ctx, writer, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.close() })
	pick, calls := fixture.engine.pickSeed, 0
	fixture.engine.pickSeed = func(ctx context.Context) (connect.Id, error) {
		calls++
		if calls == 2 {
			cancel()
			return connect.Id{}, ctx.Err()
		}
		return pick(ctx)
	}
	if err := fixture.engine.Run(ctx, 1); err != nil {
		t.Fatal("disconnected output stopped independent trail work")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	state := owner.snapshot(time.Now())
	records, skipped, err := fixture.store.Load()
	if err != nil || skipped != 0 || len(records) != 1 || calls != 2 || fixture.ledger.LastSequence() != 8 || state.Runtime.Delivered != 0 || state.Runtime.Dropped != 1 {
		t.Fatal("disconnected output changed original proof or claimed delivery")
	}
}
