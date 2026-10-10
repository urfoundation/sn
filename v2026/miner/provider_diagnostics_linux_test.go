//go:build linux

// A full physical stdout belongs to a child process. The parent waits for a
// separate completion byte before draining it, proving actual continuation.
package miner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/urfoundation/sn/v2026/clientauth"
)

// Inspect flags without File.Fd changing the caller's Go polling mode.
func providerDiagnosticFileFlags(t *testing.T, file *os.File) int {
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

// Fill through a separate nonblocking handle until the kernel reports EAGAIN.
// No sleep, queue-length guess or original descriptor flag mutation is used.
func providerDiagnosticFullPipe(t *testing.T) (*os.File, *os.File) {
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
		fillErr = errors.New("synthetic pipe exceeded its fill bound")
	})
	if err := errors.Join(err, fillErr); err != nil {
		t.Fatal(err)
	}
	return reader, writer
}

// A real token fault cancels before log delivery. The actual provide run owner
// also returns after real listener refusal instead of skipping cleanup via Exit.
func providerDiagnosticStdoutChild(t *testing.T) {
	finished := os.NewFile(3, "synthetic-provider-result")
	defer finished.Close()
	defer func() {
		value := byte(1)
		if t.Failed() {
			value = 0
		}
		_, _ = finished.Write([]byte{value})
	}()
	t.Setenv("URNETWORK_STATE_DIR", t.TempDir())
	t.Setenv("WARP_VERSION", "1.2.3")
	t.Setenv("WARP_HOST", "synthetic-provider.example")
	flags := providerDiagnosticFileFlags(t, os.Stdout)
	owner := newProviderDiagnosticTestOwner(t, os.Stdout)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	path := filepath.Join(t.TempDir(), "blocked.jwt")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	callbacks := &providerAuthenticationCallbacks{diagnostics: owner, provider: 5, clientJwtPath: path, cancel: cancel}
	callbacks.JwtRefreshed("synthetic-local-token")
	if ctx.Err() != context.Canceled || callbacks.failure() == nil {
		t.Fatal("full stdout changed token fault cancellation")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	state := owner.snapshot().Authentication
	if state.Delivered != 0 || state.Dropped != 1 || state.LastSuccessAt != "" {
		t.Fatal("full stdout fabricated token-fault diagnostic delivery")
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer endpoint.Close()
	settings := providerRunSettings{apiUrl: endpoint.URL, connectUrl: "ws" + strings.TrimPrefix(endpoint.URL, "http"), port: occupied.Addr().(*net.TCPAddr).Port}
	if err := settings.run(t.Context(), os.Stdout); !errors.Is(err, unix.EADDRINUSE) {
		t.Fatal("provide run lost its real listener refusal")
	}
	if providerDiagnosticFileFlags(t, os.Stdout) != flags {
		t.Fatal("provider exporter changed original stdout flags")
	}
}

// A generously bounded child is killed/joined on a causal deadlock; every
// assertion failure stays local to this root rather than aborting the matrix.
func TestProviderDiagnosticsPhysicalStdoutCannotHoldCallbacksOrCommand(t *testing.T) {
	if os.Getenv("SN_TEST_PROVIDER_BLOCKED_STDOUT") == "1" {
		providerDiagnosticStdoutChild(t)
		return
	}
	reader, writer := providerDiagnosticFullPipe(t)
	resultReader, resultWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer resultReader.Close()
	defer resultWriter.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProviderDiagnosticsPhysicalStdoutCannotHoldCallbacksOrCommand$", "-test.timeout=60s")
	command.Env = append(os.Environ(), "SN_TEST_PROVIDER_BLOCKED_STDOUT=1")
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
		_ = resultReader.Close()
		<-result
	}
	drained := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, reader); close(drained) }()
	err = command.Wait()
	joined = true
	<-drained
	if complete != 1 || err != nil {
		t.Fatalf("physical stdout blocked provider callbacks or command cleanup: child=%v stderr=%s", err, stderr.String())
	}
}

// A disconnected descriptor cannot change the real rejection/tombstone order.
// The exporter may report a failed attempt or a bounded close-time discard;
// neither is delivery, and no callback is allowed to wait for reconnection.
func TestProviderDiagnosticsDisconnectedOutputPreservesRejection(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	owner := newProviderDiagnosticTestOwner(t, writer)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	path := filepath.Join(t.TempDir(), "client.jwt")
	networkPath := filepath.Join(filepath.Dir(path), "network.jwt")
	if err := errors.Join(clientauth.WriteToken(path, "synthetic-client"), clientauth.WriteToken(networkPath, "synthetic-network")); err != nil {
		t.Fatal(err)
	}
	callbacks := &providerAuthenticationCallbacks{diagnostics: owner, provider: 6, clientJwtPath: path, networkJwtPath: networkPath, cancel: cancel}
	callbacks.AuthLogout()
	if ctx.Err() != context.Canceled || callbacks.failure() != nil {
		t.Fatal("disconnected output changed rejection cancellation")
	}
	if _, err := clientauth.ReadToken(path + ".rejected"); err != nil {
		t.Fatal("disconnected output prevented durable rejection")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	state := owner.snapshot().Authentication
	if state.Delivered != 0 || state.LastSuccessAt != "" || state.Dropped != 1 {
		t.Fatal("disconnected output fabricated rejection delivery")
	}
}
