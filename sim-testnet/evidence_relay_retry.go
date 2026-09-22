//go:build linux || darwin

// A relay step retries through its existing durable transaction owner. Read-only
// census uses the same finite recovery without acquiring submission authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	gethrpc "github.com/ethereum/go-ethereum/rpc"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

const evidenceRelayStepMaximumAttempts = 5

// Each incident is bounded independently and never supplies acceptance or
// transaction authority. Existing signed bytes and receipts remain authoritative.
type evidenceRelayRetryObservation struct {
	Attempt int    `json:"attempt"`
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
}

// Exhausted lower read budgets may enter this finite operation budget. Mixed
// failures, local file errors and untyped timeout-looking text cannot enter it.
func evidenceRelayTransientError(err error) bool {
	if validatorcomponent.RetryableEvidenceTransportError(err) {
		return true
	}
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if _, fileError := err.(*os.PathError); fileError {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !evidenceRelayTransientError(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return evidenceRelayTransientError(wrapped.Unwrap())
	}
	if err == gethrpc.ErrMissingBatchResponse {
		return true
	}
	if _, rpcError := err.(gethrpc.Error); rpcError {
		return evmReadRpcErrorIsTransient(err)
	}
	return false
}

// Callbacks force retry, cancellation and persistence order in tests. The next
// call is never authorized before its preceding failure has become durable.
func runEvidenceRelayStep(ctx context.Context, call func() error, wait func(context.Context, time.Duration) error, observe func(evidenceRelayRetryObservation) error) error {
	if ctx == nil || call == nil || wait == nil || observe == nil {
		return errors.New("evidence relay retry owner is incomplete")
	}
	for attempt := 1; attempt <= evidenceRelayStepMaximumAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := call()
		if err == nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			if attempt > 1 {
				return observe(evidenceRelayRetryObservation{Attempt: attempt, Outcome: "recovered"})
			}
			return ctx.Err()
		}
		outcome := "retrying"
		if ctx.Err() != nil {
			outcome = "canceled"
		} else if !evidenceRelayTransientError(err) {
			outcome = "permanent"
		} else if attempt == evidenceRelayStepMaximumAttempts {
			outcome = "exhausted"
		}
		if outcome == "retrying" || attempt > 1 || outcome == "exhausted" {
			message := err.Error()
			if len(message) > 4*1024 {
				message = message[:4*1024] + " [diagnostic truncated]"
			}
			if recordErr := observe(evidenceRelayRetryObservation{Attempt: attempt, Outcome: outcome, Error: message}); recordErr != nil {
				return errors.Join(err, fmt.Errorf("persist evidence relay retry: %w", recordErr))
			}
		}
		if outcome != "retrying" {
			return errors.Join(err, ctx.Err())
		}
		delay := time.Second * time.Duration(1<<(attempt-1))
		if waitErr := wait(ctx, delay); waitErr != nil {
			return errors.Join(err, waitErr)
		}
	}
	return errors.New("evidence relay retry budget is exhausted")
}

// Diagnostics have one private owner per incident. No successful step performs
// disk I/O, and neither an audit nor a retry edits the action journal directly.
func (self *evidenceRelayRuntime) retryStep(ctx context.Context, operation string, call func() error) error {
	return self.retryStepWithWait(ctx, operation, call, waitFinalSemanticRPCRetry)
}

// The explicit wait seam exercises durable ordering without a wall-clock delay.
func (self *evidenceRelayRuntime) retryStepWithWait(ctx context.Context, operation string, call func() error, wait func(context.Context, time.Duration) error) error {
	if self == nil || self.executor == nil || self.executor.cfg == nil || self.executor.plan == nil || self.executor.stateDir == "" || operation == "" {
		return errors.New("evidence relay retry provenance is absent")
	}
	report := struct {
		Schema          string                          `json:"schema"`
		DeploymentId    string                          `json:"deployment_id"`
		PlanHash        string                          `json:"plan_hash"`
		Phase           string                          `json:"phase"`
		Operation       string                          `json:"operation"`
		StartedAt       string                          `json:"started_at"`
		MaximumAttempts int                             `json:"maximum_attempts"`
		FinalAcceptance bool                            `json:"final_acceptance"`
		Observations    []evidenceRelayRetryObservation `json:"observations"`
	}{Schema: "urnetwork-sim-evidence-relay-retries-v1", DeploymentId: self.executor.plan.DeploymentID,
		PlanHash: self.executor.plan.PlanHash, Phase: self.phase, Operation: operation,
		MaximumAttempts: evidenceRelayStepMaximumAttempts}
	path := ""
	return runEvidenceRelayStep(ctx, call, wait, func(observation evidenceRelayRetryObservation) error {
		if path == "" {
			root := filepath.Join(self.executor.stateDir, "evidence-relay-retries")
			if err := ensurePrivateDir(root); err != nil {
				return err
			}
			dir, err := os.MkdirTemp(root, "incident-")
			if err != nil {
				return err
			}
			path = filepath.Join(dir, "retry.json")
			report.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		report.Observations = append(report.Observations, observation)
		raw, err := json.Marshal(report)
		if err != nil {
			return err
		}
		if err := atomicWrite(path, append(raw, '\n'), 0o600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "sim-testnet: evidence relay %s transport recovery; attempt=%d maximum=%d outcome=%s durable=%s final_acceptance=false\n", operation, observation.Attempt, evidenceRelayStepMaximumAttempts, observation.Outcome, path)
		return nil
	})
}
