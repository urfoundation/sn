package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	servermodel "github.com/urnetwork/server/v2026/model"
)

const (
	serverContractCleanupBatchSize = 25_000
	serverContractCleanupParallel  = 92
	serverContractCleanupMaxPasses = 100
)

// Records the bounded reconciliation performed before any new client can
// create a transfer contract in an operator database.
type serverContractCleanupResult struct {
	Schema    string `json:"schema"`
	Cutoff    string `json:"cutoff"`
	Passes    int    `json:"passes"`
	Closed    int64  `json:"closed"`
	Converged bool   `json:"converged"`
}

type serverContractCleanupRunner func(context.Context, ProcessSpec, time.Time, string) ([]byte, error)
type staleServerContractCloser func(context.Context, time.Time, int, int, int, int) (int64, error)

// Drains every contract owned by an earlier topology generation. The caller
// supplies a cutoff taken before any new client starts, which keeps concurrent
// or future-generation contracts outside this recovery boundary.
func closeStaleServerContracts(ctx context.Context, cutoff time.Time) (serverContractCleanupResult, error) {
	return closeStaleServerContractsWithCloser(ctx, cutoff, servermodel.ForceCloseOpenContractIds)
}

func closeStaleServerContractsWithCloser(ctx context.Context, cutoff time.Time, closer staleServerContractCloser) (serverContractCleanupResult, error) {
	result := serverContractCleanupResult{
		Schema: "urnetwork-sim-server-contract-cleanup-v1",
		Cutoff: cutoff.UTC().Format(time.RFC3339Nano),
	}
	if ctx == nil || closer == nil {
		return result, errors.New("server contract cleanup callback or context is nil")
	}
	if cutoff.IsZero() {
		return result, errors.New("server contract cleanup cutoff is missing")
	}
	for pass := 1; pass <= serverContractCleanupMaxPasses; pass++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		closed, err := closer(
			ctx,
			cutoff,
			serverContractCleanupBatchSize,
			serverContractCleanupParallel,
			0,
			0,
		)
		if err != nil {
			return result, fmt.Errorf("close stale server contracts pass %d: %w", pass, err)
		}
		if closed < 0 || closed > serverContractCleanupBatchSize*2 {
			return result, fmt.Errorf("close stale server contracts pass %d returned invalid count %d", pass, closed)
		}
		result.Passes = pass
		result.Closed += closed
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		// ForceCloseOpenContractIds reports selected attempts, not the number
		// proven absent afterward. Only a separate empty pass establishes that
		// no old open or disputed contract remains.
		if closed == 0 {
			result.Converged = true
			return result, nil
		}
	}
	return result, fmt.Errorf("server contract cleanup did not converge after %d passes", serverContractCleanupMaxPasses)
}

// Runs the one-shot server/model recovery under one operator's exact runtime
// environment. Stdout and stderr remain an append-only diagnostic artifact;
// the result uses a separate atomic JSON file so logs cannot corrupt it.
func runServerContractCleanupCommand(ctx context.Context, spec ProcessSpec, cutoff time.Time, resultPath string) ([]byte, error) {
	command := serverContractCleanupCommand(ctx, spec, cutoff, resultPath)
	return command.CombinedOutput()
}

// Gives the one-shot database mutator the same kernel parent-death boundary as
// long-lived supervisor children. A killed supervisor therefore cannot orphan
// one cleanup while its replacement starts another.
func serverContractCleanupCommand(ctx context.Context, spec ProcessSpec, cutoff time.Time, resultPath string) *exec.Cmd {
	command := exec.CommandContext(
		ctx,
		spec.Command,
		"__server_cleanup_contracts",
		fmt.Sprintf("--cutoff-unix-nano=%d", cutoff.UnixNano()),
		"--result="+resultPath,
	)
	command.Dir = spec.WorkDir
	command.Env = envList(spec.Env)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	return command
}

// Rejects a partial recovery result before any provider process can start.
func validateServerContractCleanupResult(result serverContractCleanupResult, cutoff time.Time) error {
	if result.Schema != "urnetwork-sim-server-contract-cleanup-v1" {
		return errors.New("server contract cleanup result has an invalid schema")
	}
	wantCutoff := cutoff.UTC().Format(time.RFC3339Nano)
	if result.Cutoff != wantCutoff {
		return fmt.Errorf("server contract cleanup cutoff %s, want %s", result.Cutoff, wantCutoff)
	}
	if !result.Converged || result.Passes < 1 || result.Passes > serverContractCleanupMaxPasses || result.Closed < 0 {
		return errors.New("server contract cleanup result did not converge within its bound")
	}
	return nil
}

// Reconciles every independent operator database before listener or client
// startup. Selecting the taskworker specs reuses the exact production DB,
// Redis, vault, and environment identity without starting a second worker.
func runSupervisorServerContractCleanupWithRunner(ctx context.Context, stateDir string, specs []ProcessSpec, cutoff time.Time, runner serverContractCleanupRunner) error {
	if ctx == nil || runner == nil {
		return errors.New("supervisor server contract cleanup callbacks are incomplete")
	}
	seenIdentities := map[string]bool{}
	cleanupCount := 0
	for _, spec := range specs {
		if spec.Role != "operator-taskworker" {
			continue
		}
		if spec.ID == "" || spec.Identity == "" || spec.Command == "" || seenIdentities[spec.Identity] {
			return fmt.Errorf("operator cleanup spec %q has an invalid or duplicate identity", spec.ID)
		}
		seenIdentities[spec.Identity] = true
		cleanupCount++
		generation := strconv.FormatInt(cutoff.UnixNano(), 10)
		resultPath := filepath.Join(stateDir, "processes", spec.ID+"-contract-cleanup-"+generation+".json")
		logPath := filepath.Join(stateDir, "processes", spec.ID+"-contract-cleanup-"+generation+".log")
		output, err := runner(ctx, spec, cutoff, resultPath)
		if writeErr := atomicWrite(logPath, output, 0o600); writeErr != nil {
			return fmt.Errorf("write operator cleanup log %s: %w", spec.ID, writeErr)
		}
		if err != nil {
			return fmt.Errorf("operator cleanup %s: %w", spec.ID, err)
		}
		encoded, err := os.ReadFile(resultPath)
		if err != nil {
			return fmt.Errorf("read operator cleanup result %s: %w", spec.ID, err)
		}
		var result serverContractCleanupResult
		if err := json.Unmarshal(encoded, &result); err != nil {
			return fmt.Errorf("decode operator cleanup result %s: %w", spec.ID, err)
		}
		if err := validateServerContractCleanupResult(result, cutoff); err != nil {
			return fmt.Errorf("validate operator cleanup result %s: %w", spec.ID, err)
		}
	}
	if cleanupCount == 0 {
		return errors.New("supervisor has no operator contract cleanup specs")
	}
	return nil
}

func runSupervisorServerContractCleanup(ctx context.Context, stateDir string, specs []ProcessSpec, cutoff time.Time) error {
	return runSupervisorServerContractCleanupWithRunner(ctx, stateDir, specs, cutoff, runServerContractCleanupCommand)
}
