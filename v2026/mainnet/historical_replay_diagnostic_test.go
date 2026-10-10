// The actual process owner returns bounded stderr alongside its original
// typed cause. Diagnostic words never grant a retry or integrity verdict.
package main

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestHistoricalReplayPublicRefusalRetainsBoundedOriginalDiagnostic(t *testing.T) {
	request := historicalReplayTestRequest(t, "0xdb")
	var output, diagnostic bytes.Buffer
	code := runMain(t.Context(), []string{"verify-historical-execution", "--engine", request.Engine.Path, "--engine-sha256", request.Engine.Sha256, "--job", request.Job.Path, "--job-sha256", request.Job.Sha256}, &output, &diagnostic)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "principal API refusal") || diagnostic.Len() > 2300 || strings.ContainsAny(diagnostic.String(), "\x00\r") {
		t.Fatal("public original child diagnostic was dropped or unbounded", code, output.String(), diagnostic.Len(), diagnostic.String())
	}
}

func TestHistoricalReplayDiagnosticWordsDoNotReplaceProcessCause(t *testing.T) {
	request := historicalReplayTestRequest(t, "0xdb")
	report, err := runHistoricalReplay(t.Context(), request, historicalReplayHooks{})
	var exit *exec.ExitError
	if report != nil || !errors.As(err, &exit) || exit.ExitCode() != 1 || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errRpcIntegrity) || !strings.Contains(err.Error(), "principal API refusal") || len(err.Error()) > 2300 {
		t.Fatal("diagnostic words changed original process cause or bound", report, err)
	}
}

func TestHistoricalReplayDiagnosticCancellationKeepsOwnerCauseAndJoins(t *testing.T) {
	request := historicalReplayTestRequest(t, "0xdc")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var once sync.Once
	pid := 0
	report, err := runHistoricalReplay(ctx, request, historicalReplayHooks{afterStart: func(_ context.Context, child int) { pid = child }, afterOutput: func() { once.Do(cancel) }})
	if report != nil || !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "principal API execution interrupted") || pid == 0 || !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		t.Fatal("diagnostic propagation lost actual cancellation or its child owner", report, pid, err)
	}
}
