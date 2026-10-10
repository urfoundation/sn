// Test-owned fixture admission precedes worker launch. Failure observations and
// cancellation joins have finite backstops without replacing positive barriers.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Explicit completion and cancellation accompany every success-only barrier.
func waitMonitorTestEvent[T, D any](ctx context.Context, done <-chan D, events <-chan T) (T, error) {
	select {
	case event := <-events:
		return event, nil
	case result, present := <-done:
		var zero T
		return zero, fmt.Errorf("monitor worker stopped before event (result=%t, value=%v)", present, result)
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// Only the failed-control path uses the deadline; the real event orders work.
func monitorTestEvent[T any](t *testing.T, done <-chan int, events <-chan T) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	event, err := waitMonitorTestEvent(ctx, done, events)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

// A failed worker cannot leave its test blocked while offering another sample.
func resumeMonitorTestWorker(t *testing.T, done <-chan int, resume chan<- struct{}) {
	t.Helper()
	select {
	case resume <- struct{}{}:
	case code, present := <-done:
		t.Fatalf("monitor worker stopped before resume (result=%t, code=%d)", present, code)
	case <-t.Context().Done():
		t.Fatal("test canceled before monitor resume")
	case <-time.After(30 * time.Second):
		t.Fatal("monitor worker did not accept resume")
	}
}

// Cleanup cancels first and reports a worker that fails to join, even after Fatal.
func joinMonitorTestWorker[T any](t *testing.T, cancel context.CancelFunc, done <-chan T) {
	t.Helper()
	cancel()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Error("monitor worker survived cancellation cleanup")
	}
}

// Corrupt real retained metadata invokes the existing fatal enrollment path.
// A child test isolates that expected failure, and a returned starter is a bug.
func monitorTestStarterRefusesMalformedFixture(t *testing.T, start func(*monitorServicesFixture)) {
	t.Helper()
	const childKey = "URNETWORK_MONITOR_FIXTURE_FAILURE_CHILD"
	const returned = "fixture starter returned after malformed generation"
	if os.Getenv(childKey) == t.Name() {
		fixture := newMonitorServicesFixture(t, "alpha")
		if err := syscall.Setxattr(fixture.directory, durablevolume.RootGenerationAttribute, []byte{1}, 1); err != nil {
			t.Fatal("malformed generation control could not be installed", err)
		}
		start(fixture)
		t.Fatal(returned)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.timeout=5s")
	command.Env = append(os.Environ(), childKey+"="+t.Name())
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytes.Contains(output, []byte("synthetic root generation: size=1 err=<nil>")) || bytes.Contains(output, []byte(returned)) || bytes.Contains(output, []byte("test timed out")) {
		t.Fatalf("fixture admission did not fail on its owning test goroutine: err=%v context=%v output=%s", err, ctx.Err(), output)
	}
}

// This actual shared starter previously returned before its worker called Fatal.
func TestMonitorServicesFixtureFailureStaysOnTestGoroutine(t *testing.T) {
	monitorTestStarterRefusesMalformedFixture(t, func(fixture *monitorServicesFixture) {
		fixture.start(t, "http://rpc.example", monitorServiceHooks{})
	})
}

// The operator starter has a separate lifecycle and must keep the same boundary.
func TestMonitorOperatorFixtureFailureStaysOnTestGoroutine(t *testing.T) {
	monitorTestStarterRefusesMalformedFixture(t, func(fixture *monitorServicesFixture) {
		startMonitorOperatorTest(t, fixture, "http://rpc.example", monitorServiceHooks{})
	})
}

// Every audited fixture entry point is forbidden inside a background call.
// Runtime malformed-generation controls above prove why this boundary matters.
func TestMonitorFixtureAdmissionPrecedesEveryWorker(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := token.NewFileSet()
	parsed := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, entry.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed++
		ast.Inspect(file, func(node ast.Node) bool {
			worker, ok := node.(*ast.GoStmt)
			if !ok {
				return true
			}
			ast.Inspect(worker.Call, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				switch name.Name {
				case "monitorTestStorageContext", "runMonitorStorageTestWithHooks", "runMonitorTestWithClock", "runMonitorTest", "prepareMonitorTestWithClock", "provisionMonitorTestCustody", "provisionMonitorTestCustodyProfile", "newMonitorServicesFixture":
					t.Errorf("fatal fixture admission %s runs inside worker at %s", name.Name, files.Position(call.Pos()))
				}
				return true
			})
			return false
		})
	}
	if parsed == 0 {
		t.Fatal("worker admission audit found no test sources")
	}
}
