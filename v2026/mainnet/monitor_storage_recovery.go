// Each monitor role owns its recovery budget. Only joined uncertain snapshots
// reopen; resource pressure does not discard the current descriptor generation.
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
)

const monitorStorageReconciliations = 3

type monitorStorageRecovery struct{ attempts int }

// The caller has completed its synchronous read/publication cycle. close joins
// the old storage owner; reopen must load and validate original actual bytes
// before replacing any process-local state. Unrelated roles retain their owners.
func (self *monitorStorageRecovery) resume(ctx context.Context, role string, close func() error, reopen func() error, hooks monitorServiceHooks) error {
	if err := close(); err != nil {
		return monitorAdmissionFailure(nil, err)
	}
	if self.attempts == monitorStorageReconciliations {
		return fmt.Errorf("%s storage reconciliation budget exhausted: %w", role, durablehead.ErrUncertain)
	}
	self.attempts++
	backoff := time.Second
	for ctx.Err() == nil {
		if !waitMonitorService(ctx, role, backoff, hooks) {
			return context.Canceled
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		err := reopen()
		if err == nil {
			return nil
		}
		// Reopening already attempts one exact pending-byte reconciliation.
		// Partial or unknown custody must remain stopped rather than rescanned
		// indefinitely or reconstructed from a cached observation.
		if !monitorStoragePending(err) || errors.Is(err, durablehead.ErrUncertain) {
			return err
		}
		backoff = min(2*backoff, time.Minute)
	}
	return ctx.Err()
}
