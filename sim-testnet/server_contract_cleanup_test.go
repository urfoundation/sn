package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Forces the full-batch boundary that previously let an old topology retain
// contracts into the next provider generation.
func TestStaleServerContractCleanupDrainsToAnEmptyBoundedPass(t *testing.T) {
	cutoff := time.Unix(1_700_000_000, 123).UTC()
	passes := 0
	result, err := closeStaleServerContractsWithCloser(
		context.Background(),
		cutoff,
		func(_ context.Context, observedCutoff time.Time, maxCount, parallel, blockSize, blockIndex int) (int64, error) {
			passes++
			if observedCutoff != cutoff || maxCount != serverContractCleanupBatchSize || parallel != serverContractCleanupParallel || blockSize != 0 || blockIndex != 0 {
				t.Fatalf("cleanup arguments = %s %d %d %d %d", observedCutoff, maxCount, parallel, blockSize, blockIndex)
			}
			switch passes {
			case 1:
				return serverContractCleanupBatchSize, nil
			case 2:
				return 7, nil
			default:
				return 0, nil
			}
		},
	)
	if err != nil || !result.Converged || result.Passes != 3 || result.Closed != serverContractCleanupBatchSize+7 {
		t.Fatalf("cleanup result=%+v calls=%d error=%v", result, passes, err)
	}
}

func TestStaleServerContractCleanupRejectsPersistentAttempt(t *testing.T) {
	passes := 0
	result, err := closeStaleServerContractsWithCloser(
		context.Background(),
		time.Unix(1_700_000_000, 0),
		func(context.Context, time.Time, int, int, int, int) (int64, error) {
			passes++
			return 1, nil
		},
	)
	if err == nil || result.Converged || passes != serverContractCleanupMaxPasses || !strings.Contains(err.Error(), "did not converge") {
		t.Fatalf("persistent cleanup result=%+v calls=%d error=%v", result, passes, err)
	}
}

func TestStaleServerContractCleanupRejectsCanceledEmptyPass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result, err := closeStaleServerContractsWithCloser(
		ctx,
		time.Unix(1_700_000_000, 0),
		func(context.Context, time.Time, int, int, int, int) (int64, error) {
			cancel()
			return 0, nil
		},
	)
	if !errors.Is(err, context.Canceled) || result.Converged {
		t.Fatalf("canceled empty cleanup result=%+v error=%v", result, err)
	}
}

func TestServerContractCleanupCommandDiesWithSupervisor(t *testing.T) {
	command := serverContractCleanupCommand(
		context.Background(),
		ProcessSpec{Command: "/bin/true", WorkDir: t.TempDir()},
		time.Unix(1_700_000_000, 0),
		filepath.Join(t.TempDir(), "result.json"),
	)
	if command.SysProcAttr == nil || !command.SysProcAttr.Setpgid || command.SysProcAttr.Pdeathsig != syscall.SIGTERM {
		t.Fatalf("cleanup process attributes=%+v", command.SysProcAttr)
	}
}

// The supervisor must prove every operator database converged before startup;
// one missing result cannot degrade into a best-effort maintenance delay.
func TestSupervisorContractCleanupCoversEveryOperatorBeforeStartup(t *testing.T) {
	dir := t.TempDir()
	cutoff := time.Unix(1_700_000_000, 456).UTC()
	specs := []ProcessSpec{
		{ID: "operator-1-api", Role: "operator-api", Identity: "no:1"},
		{ID: "operator-2-taskworker", Role: "operator-taskworker", Identity: "no:2", Command: "/release/sim-testnet"},
		{ID: "operator-1-taskworker", Role: "operator-taskworker", Identity: "no:1", Command: "/release/sim-testnet"},
	}
	called := []string{}
	runner := func(_ context.Context, spec ProcessSpec, observedCutoff time.Time, resultPath string) ([]byte, error) {
		called = append(called, spec.ID)
		if observedCutoff != cutoff || filepath.Dir(resultPath) != filepath.Join(dir, "processes") || !strings.HasSuffix(resultPath, "-contract-cleanup-1700000000000000456.json") {
			t.Fatalf("cleanup boundary=%s result=%s", observedCutoff, resultPath)
		}
		result := serverContractCleanupResult{
			Schema: "urnetwork-sim-server-contract-cleanup-v1", Cutoff: cutoff.Format(time.RFC3339Nano), Passes: 1, Closed: int64(len(called)), Converged: true,
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if err := atomicWrite(resultPath, encoded, 0o600); err != nil {
			return nil, err
		}
		return []byte("closed stale contracts\n"), nil
	}
	if err := runSupervisorServerContractCleanupWithRunner(context.Background(), dir, specs, cutoff, runner); err != nil {
		t.Fatal(err)
	}
	if want := []string{"operator-2-taskworker", "operator-1-taskworker"}; !reflect.DeepEqual(called, want) {
		t.Fatalf("cleanup order=%v want=%v", called, want)
	}
	for _, spec := range specs[1:] {
		logPath := filepath.Join(dir, "processes", spec.ID+"-contract-cleanup-1700000000000000456.log")
		if encoded, err := os.ReadFile(logPath); err != nil || string(encoded) != "closed stale contracts\n" {
			t.Fatalf("cleanup log %s=%q, %v", spec.ID, encoded, err)
		}
	}
}

func TestSupervisorContractCleanupFailsClosedOnUnprovenConvergence(t *testing.T) {
	dir := t.TempDir()
	cutoff := time.Unix(1_700_000_000, 789).UTC()
	runner := func(_ context.Context, _ ProcessSpec, _ time.Time, resultPath string) ([]byte, error) {
		result := serverContractCleanupResult{
			Schema: "urnetwork-sim-server-contract-cleanup-v1", Cutoff: cutoff.Format(time.RFC3339Nano), Passes: 1,
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		return nil, atomicWrite(resultPath, encoded, 0o600)
	}
	err := runSupervisorServerContractCleanupWithRunner(
		context.Background(),
		dir,
		[]ProcessSpec{{ID: "operator-1-taskworker", Role: "operator-taskworker", Identity: "no:1", Command: "/release/sim-testnet"}},
		cutoff,
		runner,
	)
	if err == nil || !strings.Contains(err.Error(), "did not converge") {
		t.Fatalf("unproven cleanup error=%v", err)
	}
}

func TestSupervisorContractCleanupPropagatesOperatorFailure(t *testing.T) {
	sentinel := errors.New("database unavailable")
	err := runSupervisorServerContractCleanupWithRunner(
		context.Background(),
		t.TempDir(),
		[]ProcessSpec{{ID: "operator-1-taskworker", Role: "operator-taskworker", Identity: "no:1", Command: "/release/sim-testnet"}},
		time.Unix(1_700_000_000, 0),
		func(context.Context, ProcessSpec, time.Time, string) ([]byte, error) {
			return []byte("failure\n"), sentinel
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("operator cleanup error=%v", err)
	}
}
