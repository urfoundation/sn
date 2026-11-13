// Startup ownership is local to one configured role. A pending admission holds
// no publisher and cannot block a healthy role's first observation or shutdown.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Constructed callbacks retain one exact typed worker, including its original
// loaded state. A failed constructor can return an owner for synchronous cleanup.
type monitorAdmittedRole struct {
	run   func() int
	close func() error
}

// An instance-local test barrier runs after actual reads and can only refuse
// admission. No policy, command or environment field installs it.
type monitorStartupObservationKey struct{}

// The parent already owns this finite role's output slot. Admission status is
// observable even when the role cannot acquire its checkpoint or metrics file.
// Acquiring custody is not a current sample and cannot make either file fresh.
type monitorAdmissionEvent struct {
	Schema            string `json:"schema"`
	Role              string `json:"role"`
	ObservedAt        string `json:"observed_at"`
	Status            string `json:"status"`
	Reason            string `json:"reason"`
	RetryAfterSeconds uint64 `json:"retry_after_seconds"`
	CheckpointCurrent bool   `json:"checkpoint_current"`
	MetricsCurrent    bool   `json:"metrics_current"`
}

func monitorAdmissionReason(err error) string {
	var cleanup *monitorAdmissionCleanupError
	var ownership *monitorOutputOwnershipError
	switch {
	case err == nil:
		return "custody-acquired"
	case errors.As(err, &cleanup):
		return "cleanup-unresolved"
	case errors.As(err, &ownership), errors.Is(err, durablevolume.ErrIdentity):
		return "custody-lost"
	case errors.Is(err, errRpcIntegrity), errors.Is(err, errRpcIdentityMismatch):
		return "identity-or-integrity"
	case errors.Is(err, durablehead.ErrUncertain), errors.Is(err, errMainnetDurablePublicationUncertain):
		return "publication-unresolved"
	case errors.Is(err, durablevolume.ErrBusy), errors.Is(err, syscall.EAGAIN), errors.Is(err, syscall.EBUSY):
		return "owner-busy"
	case monitorStartupPending(err):
		return "observation-unavailable"
	default:
		return "retained-admission-refused"
	}
}

// The closed fields above cannot grow with an underlying path or RPC error.
// Queue/output refusal remains diagnostic; it cannot acquire another owner.
func publishMonitorAdmission(role, status string, cause error, retry time.Duration, stdout, diagnostic io.Writer, now func() time.Time) {
	event := monitorAdmissionEvent{Schema: "urnetwork-mainnet-role-admission-v1", Role: role, ObservedAt: now().UTC().Format(time.RFC3339Nano), Status: status, Reason: monitorAdmissionReason(cause), RetryAfterSeconds: uint64(retry / time.Second)}
	raw, err := json.Marshal(event)
	if err == nil {
		raw = append(raw, '\n')
		var n int
		n, err = stdout.Write(raw)
		if err == nil && n != len(raw) {
			err = io.ErrShortWrite
		}
	}
	if err != nil {
		fmt.Fprintf(diagnostic, "monitor %s admission output: %v\n", role, err)
	}
}

// A failed release is distinct from an unavailable observation. Reacquiring the
// same role after an unjoined owner could duplicate custody, even for EIO.
type monitorAdmissionCleanupError struct{ cause error }

func (self *monitorAdmissionCleanupError) Error() string {
	return "monitor admission cleanup failed: " + self.cause.Error()
}
func (self *monitorAdmissionCleanupError) Unwrap() error { return self.cause }

// A later refused observation dominates earlier transient causes retained for
// diagnostics. Joining those causes cannot reopen a malformed checkpoint.
type monitorAdmissionRefusalError struct{ cause error }

// Diagnostics retain both the refused observation and any earlier outage.
func (self *monitorAdmissionRefusalError) Error() string {
	return "monitor admission refused: " + self.cause.Error()
}

// Cause matching preserves evidence without changing the terminal decision.
func (self *monitorAdmissionRefusalError) Unwrap() error { return self.cause }

func monitorAdmissionFailure(cause, cleanup error) error {
	if cleanup == nil {
		return cause
	}
	return &monitorAdmissionCleanupError{cause: errors.Join(cause, cleanup)}
}

// Only observation/resource failures retry. Missing original custody, malformed
// retained state and uncertain reconciliation remain a stopped role. A raw
// nonblocking flock conflict happens before the snapshot owner is constructed.
func monitorStartupPending(err error) bool {
	var cleanup *monitorAdmissionCleanupError
	var ownership *monitorOutputOwnershipError
	var refused *monitorAdmissionRefusalError
	if err == nil || errors.As(err, &cleanup) || errors.As(err, &ownership) || errors.As(err, &refused) || errors.Is(err, durablevolume.ErrIdentity) || errors.Is(err, errRpcIntegrity) || errors.Is(err, errRpcIdentityMismatch) || errors.Is(err, durablehead.ErrUncertain) || errors.Is(err, errMainnetDurablePublicationUncertain) {
		return false
	}
	if monitorStoragePending(err) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrClosed) {
		return true
	}
	for _, cause := range []error{syscall.EAGAIN, syscall.EBUSY, syscall.EIO, syscall.EMFILE, syscall.ENFILE, syscall.ENOSPC, syscall.EDQUOT, syscall.EINTR, syscall.ETIMEDOUT, syscall.EBADF} {
		if errors.Is(err, cause) {
			return true
		}
	}
	return false
}

// Admission performs real bounded reads and closes every partial owner before
// retry. No state is published until the unchanged role constructor succeeds.
// Backoff is per role, cancellation is joined, and no global retry clock exists.
func runMonitorRoleAdmission(ctx context.Context, role string, open func() (*monitorAdmittedRole, error), stdout, diagnostic io.Writer, now func() time.Time, hooks monitorServiceHooks) int {
	backoff := time.Second
	pending := false
	for ctx.Err() == nil {
		owner, err := open()
		// This private barrier follows all real constructor reads; it can only
		// withhold admission, never provide state or bypass physical checks.
		if after, ok := ctx.Value(monitorStartupObservationKey{}).(func(context.Context, string) error); err == nil && owner != nil && ok && after != nil {
			err = after(ctx, role)
		}
		if err == nil && owner != nil && owner.run != nil && owner.close != nil {
			if pending {
				publishMonitorAdmission(role, "admitted", nil, 0, stdout, diagnostic, now)
			}
			exit := owner.run()
			if closeErr := owner.close(); closeErr != nil {
				publishMonitorAdmission(role, "quarantined", monitorAdmissionFailure(nil, closeErr), 0, stdout, diagnostic, now)
				fmt.Fprintf(diagnostic, "monitor %s cleanup: %v\n", role, closeErr)
				return 3
			}
			return exit
		}
		if err == nil {
			err = errors.New("monitor role constructor omitted its owned lifecycle")
		}
		if owner != nil && owner.close != nil {
			err = monitorAdmissionFailure(err, owner.close())
		}
		var cleanup *monitorAdmissionCleanupError
		if !errors.As(err, &cleanup) && monitorCanceledCheckpointLoad(ctx, err) {
			return 0
		}
		fmt.Fprintf(diagnostic, "monitor %s admission: %v\n", role, err)
		if ctx.Err() != nil || !monitorStartupPending(err) {
			publishMonitorAdmission(role, "quarantined", err, 0, stdout, diagnostic, now)
			return 3
		}
		publishMonitorAdmission(role, "pending", err, backoff, stdout, diagnostic, now)
		pending = true
		if !waitMonitorService(ctx, role, backoff, hooks) {
			return 0
		}
		backoff = min(2*backoff, time.Minute)
	}
	return 0
}

// The validator uses the same constructor ownership boundary as other roles;
// partial admission never leaves a checkpoint owner for a parent-wide cleanup.
func openMonitorValidatorWorker(ctx context.Context, policy monitorValidatorPolicy, expected identityExpectation, checkpointPath, metricsPath string, hooks monitorServiceHooks) (*monitorValidatorWorker, error) {
	checkpointPath, metricsPath = monitorValidatorPaths(checkpointPath, metricsPath, policy.Role)
	checkpoint, err := openMonitorServiceCheckpoint(checkpointPath, expected, policy, ctx)
	if err != nil {
		return nil, err
	}
	worker := &monitorValidatorWorker{policy: policy, checkpoint: checkpoint}
	worker.metrics, err = openMonitorMetrics(metricsPath, ctx)
	if err != nil {
		return nil, monitorAdmissionFailure(err, closeMonitorServiceOwners(policy.Role, worker.metrics, checkpoint.owner, hooks))
	}
	if hooks.afterCheckpointOpen != nil {
		hooks.afterCheckpointOpen(ctx, policy.Role, checkpoint.owner.lock)
	}
	worker.state, err = checkpoint.load(ctx)
	if err != nil {
		return nil, monitorAdmissionFailure(err, closeMonitorServiceOwners(policy.Role, worker.metrics, checkpoint.owner, hooks))
	}
	if hooks.syncDirectory != nil {
		checkpoint.owner.syncDirectory = func(file *os.File) error { return hooks.syncDirectory(policy.Role, "checkpoint", file) }
		worker.metrics.syncDirectory = func(file *os.File) error { return hooks.syncDirectory(policy.Role, "metrics", file) }
	}
	return worker, nil
}
