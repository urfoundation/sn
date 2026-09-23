// Claim polls batch retry diagnostics while keeping every transaction boundary
// durable. The queue has one daemon owner; hooks expose time and side effects.
package miner

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Each callback runs synchronously under the daemon's queue ownership. The
// production callbacks retain their existing chain and nonce lock boundaries.
type claimQueuePollHooks struct {
	now       func() time.Time
	save      func(*ClaimQueue) error
	reconcile func(context.Context, *ClaimQueueEntry) (string, error)
	submit    func(context.Context, *ClaimQueueEntry) error
}

// A reconciliation failure has its own bounded backoff: failed readiness reads
// do not increment the count of actual transaction submission attempts.
func deferClaimReconciliation(entry *ClaimQueueEntry, now time.Time, failure error, recent bool) {
	if entry.ReconcileAttempts < 0 {
		entry.ReconcileAttempts = 0
	}
	if entry.ReconcileAttempts < 7 {
		entry.ReconcileAttempts++
	}
	delay := claimRetry(entry.ReconcileAttempts)
	if recent || entry.Status == "uncertain" {
		// Current payout roots and exact transaction outcomes must not wait
		// behind the longer backoff assigned to historical repair work.
		delay = min(delay, time.Minute)
	}
	if entry.Status == "uncertain" {
		entry.LastError = "uncertain transaction reconciliation: " + failure.Error()
	} else {
		entry.Status = "retry"
		entry.LastError = "claim is not ready for finalized reconciliation: " + failure.Error()
	}
	entry.NextRetryAt = now.Add(delay).UTC().Format(time.RFC3339Nano)
	entry.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
}

// Retry admission precedes any API/RPC call. Retry-only changes are coalesced
// into one checkpoint; submitting, signed bytes and final receipts never wait
// for that batch. Recent claims are considered before historical repair work.
func pollClaimQueue(ctx context.Context, queue *ClaimQueue, hooks claimQueuePollHooks) error {
	dirty := false
	flush := func() error {
		if !dirty {
			return nil
		}
		if err := hooks.save(queue); err != nil {
			return err
		}
		dirty = false
		return nil
	}
	for epoch := queue.LastDiscovered; epoch >= 0; epoch-- {
		if ctx.Err() != nil {
			break
		}
		entry := queue.Entries[fmt.Sprint(epoch)]
		if entry == nil || entry.Status == "finalized" || entry.Status == "no-claim" {
			continue
		}
		if entry.NextRetryAt != "" {
			when, err := time.Parse(time.RFC3339Nano, entry.NextRetryAt)
			if err != nil {
				return errors.Join(fmt.Errorf("claim epoch %d has an invalid retry deadline: %w", epoch, err), flush())
			}
			if hooks.now().Before(when) {
				continue
			}
		}
		reconciled, reconcileErr := hooks.reconcile(ctx, entry)
		if reconciled != "" {
			entry.Status = reconciled
			entry.UpdatedAt = hooks.now().UTC().Format(time.RFC3339Nano)
			entry.LastError, entry.NextRetryAt = "", ""
			entry.ReconcileAttempts = 0
			dirty = true
			if err := flush(); err != nil {
				return err
			}
			if reconciled == "finalized" || reconciled == "no-claim" {
				continue
			}
		}
		if reconcileErr != nil {
			deferClaimReconciliation(entry, hooks.now(), reconcileErr, epoch >= queue.LastDiscovered-1)
			dirty = true
			continue
		}
		if entry.Status == "uncertain" {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		entry.ReconcileAttempts = 0
		entry.Status = "submitting"
		entry.Attempts++
		entry.TxHash, entry.RawTxHex = "", ""
		entry.UpdatedAt = hooks.now().UTC().Format(time.RFC3339Nano)
		entry.LastError, entry.NextRetryAt = "", ""
		dirty = true
		if err := flush(); err != nil {
			return err
		}
		claimErr := hooks.submit(ctx, entry)
		entry.UpdatedAt = hooks.now().UTC().Format(time.RFC3339Nano)
		if claimErr == nil {
			entry.Status = "finalized"
		} else if entry.TxHash != "" {
			entry.Status = "uncertain"
			entry.LastError = "transaction was broadcast but finality was not confirmed: " + claimErr.Error()
		} else {
			entry.Status = "retry"
			entry.LastError = claimErr.Error()
			entry.NextRetryAt = hooks.now().Add(claimRetry(entry.Attempts)).UTC().Format(time.RFC3339Nano)
		}
		dirty = true
		if err := flush(); err != nil {
			return err
		}
	}
	return flush()
}
