// Deterministic queue polls prove retry admission and durable transaction
// boundaries without depending on wall-clock sleeps or live chain state.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Unexpected side effects fail immediately; individual tests install only
// the callbacks permitted by the queue state they exercise.
func claimPollTestHooks(t *testing.T, now time.Time) claimQueuePollHooks {
	t.Helper()
	return claimQueuePollHooks{
		now: func() time.Time { return now },
		save: func(*ClaimQueue) error {
			t.Fatal("unexpected durable write")
			return nil
		},
		reconcile: func(context.Context, *ClaimQueueEntry) (string, error) {
			t.Fatal("unexpected reconciliation")
			return "", nil
		},
		submit: func(context.Context, *ClaimQueueEntry) error {
			t.Fatal("unexpected transaction submission")
			return nil
		},
	}
}

// Future deadlines suppress reconciliation, including exact-outcome retries;
// repeated polling cannot extend the deadline or rewrite unchanged metadata.
func TestClaimQueuePollHonorsDeadlineBeforeAnySideEffect(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, status := range []string{"retry", "uncertain"} {
		entry := &ClaimQueueEntry{Epoch: 7, Status: status, Attempts: 2, ReconcileAttempts: 3, NextRetryAt: now.Add(time.Minute).Format(time.RFC3339Nano), TxHash: "synthetic retained hash", RawTxHex: "synthetic retained bytes"}
		queue := &ClaimQueue{LastDiscovered: 7, Entries: map[string]*ClaimQueueEntry{"7": entry}}
		before := *entry
		if err := pollClaimQueue(context.Background(), queue, claimPollTestHooks(t, now)); err != nil {
			t.Fatal(err)
		}
		if *entry != before {
			t.Fatalf("%s queue changed before its retry deadline", status)
		}
	}
}

// A large historical backlog causes one retry checkpoint, not one complete
// file rewrite per epoch. Current claims receive the first reconciliation.
func TestClaimQueuePollBatchesHistoricalFailuresAndPrioritizesRecentClaims(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	queue := &ClaimQueue{LastDiscovered: 63, Entries: map[string]*ClaimQueueEntry{}}
	for epoch := int64(0); epoch <= queue.LastDiscovered; epoch++ {
		queue.Entries[fmt.Sprint(epoch)] = &ClaimQueueEntry{Epoch: epoch, Status: "retry"}
	}
	hooks := claimPollTestHooks(t, now)
	var epochs []int64
	writes := 0
	hooks.reconcile = func(_ context.Context, entry *ClaimQueueEntry) (string, error) {
		epochs = append(epochs, entry.Epoch)
		return "", errors.New("synthetic payout is not ready")
	}
	hooks.save = func(*ClaimQueue) error { writes++; return nil }
	if err := pollClaimQueue(context.Background(), queue, hooks); err != nil {
		t.Fatal(err)
	}
	if len(epochs) != 64 || epochs[0] != 63 || epochs[63] != 0 || writes != 1 {
		t.Fatalf("epochs=%v writes=%d", epochs, writes)
	}
	for _, entry := range queue.Entries {
		if entry.Attempts != 0 || entry.ReconcileAttempts != 1 || entry.NextRetryAt != now.Add(time.Minute).Format(time.RFC3339Nano) {
			t.Fatalf("readiness failure changed submission accounting: %+v", entry)
		}
	}
	if err := pollClaimQueue(context.Background(), queue, claimPollTestHooks(t, now)); err != nil {
		t.Fatal(err)
	}
}

// Reconciliation backoff survives restart and grows even when no transaction
// has ever been submitted. The bounded counter cannot overflow after retries.
func TestClaimQueuePollPersistsIndependentBoundedReconciliationBackoff(t *testing.T) {
	store, err := newClaimQueueStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 9, Entries: map[string]*ClaimQueueEntry{"4": {Epoch: 4, Status: "retry"}}}
	for attempt := 1; attempt <= 9; attempt++ {
		hooks := claimPollTestHooks(t, now)
		hooks.save = store.save
		hooks.reconcile = func(context.Context, *ClaimQueueEntry) (string, error) {
			return "", errors.New("synthetic pending root")
		}
		if err := pollClaimQueue(context.Background(), queue, hooks); err != nil {
			t.Fatal(err)
		}
		queue, err = store.load()
		if err != nil {
			t.Fatal(err)
		}
		entry := queue.Entries["4"]
		deadline, err := time.Parse(time.RFC3339Nano, entry.NextRetryAt)
		if err != nil || entry.ReconcileAttempts != min(attempt, 7) || entry.Attempts != 0 || deadline.Sub(now) != claimRetry(attempt) {
			t.Fatalf("attempt %d entry=%+v deadline=%s error=%v", attempt, entry, deadline, err)
		}
		now = deadline
	}
}

// Recent payout roots retain prompt polling after repeated not-ready results;
// only older repair work may receive the maximum hourly readiness backoff.
func TestClaimQueuePollKeepsRecentEpochReadinessPrompt(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	queue := &ClaimQueue{LastDiscovered: 8, Entries: map[string]*ClaimQueueEntry{}}
	for _, epoch := range []int64{6, 7, 8} {
		queue.Entries[fmt.Sprint(epoch)] = &ClaimQueueEntry{Epoch: epoch, Status: "retry", ReconcileAttempts: 7}
	}
	hooks := claimPollTestHooks(t, now)
	hooks.reconcile = func(context.Context, *ClaimQueueEntry) (string, error) {
		return "", errors.New("synthetic root pending")
	}
	writes := 0
	hooks.save = func(*ClaimQueue) error { writes++; return nil }
	if err := pollClaimQueue(context.Background(), queue, hooks); err != nil {
		t.Fatal(err)
	}
	for key, entry := range queue.Entries {
		wantDelay := time.Minute
		if entry.Epoch == 6 {
			wantDelay = time.Hour
		}
		if entry.NextRetryAt != now.Add(wantDelay).Format(time.RFC3339Nano) || entry.Attempts != 0 {
			t.Fatalf("epoch %s has wrong readiness deadline: %+v", key, entry)
		}
	}
	if writes != 1 {
		t.Fatalf("recent and historical retry diagnostics were not batched: %d", writes)
	}
}

// Exact uncertain transactions remain intact and are checked at least once a
// minute even when ordinary historical readiness has backed off for an hour.
func TestClaimQueuePollRetainsUncertainTransactionAndBoundsOutcomeDelay(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	entry := &ClaimQueueEntry{Epoch: 2, Status: "uncertain", Attempts: 1, ReconcileAttempts: 7, TxHash: "synthetic retained hash", RawTxHex: "synthetic retained bytes"}
	queue := &ClaimQueue{LastDiscovered: 2, Entries: map[string]*ClaimQueueEntry{"2": entry}}
	hooks := claimPollTestHooks(t, now)
	hooks.reconcile = func(context.Context, *ClaimQueueEntry) (string, error) { return "", context.DeadlineExceeded }
	writes := 0
	hooks.save = func(*ClaimQueue) error { writes++; return nil }
	if err := pollClaimQueue(context.Background(), queue, hooks); err != nil {
		t.Fatal(err)
	}
	if writes != 1 || entry.Status != "uncertain" || entry.TxHash != "synthetic retained hash" || entry.RawTxHex != "synthetic retained bytes" || entry.Attempts != 1 || entry.NextRetryAt != now.Add(time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("uncertain boundary changed: writes=%d entry=%+v", writes, entry)
	}
}

// Save failures, including uncertain-result diagnostics formerly ignored,
// reach the owner and never authorize transaction submission.
func TestClaimQueuePollPropagatesRetryCheckpointFailure(t *testing.T) {
	for _, status := range []string{"retry", "uncertain"} {
		queue := &ClaimQueue{LastDiscovered: 0, Entries: map[string]*ClaimQueueEntry{"0": {Epoch: 0, Status: status}}}
		hooks := claimPollTestHooks(t, time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
		hooks.reconcile = func(context.Context, *ClaimQueueEntry) (string, error) { return "", context.DeadlineExceeded }
		want := errors.New("synthetic checkpoint refusal")
		hooks.save = func(*ClaimQueue) error { return want }
		if err := pollClaimQueue(context.Background(), queue, hooks); !errors.Is(err, want) {
			t.Fatalf("%s checkpoint error=%v", status, err)
		}
	}
}

// Cancellation stops new work but checkpoints already observed retry state.
func TestClaimQueuePollCancellationFlushesObservedDiagnostics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := &ClaimQueue{LastDiscovered: 1, Entries: map[string]*ClaimQueueEntry{"0": {Epoch: 0, Status: "pending"}, "1": {Epoch: 1, Status: "pending"}}}
	hooks := claimPollTestHooks(t, time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	hooks.reconcile = func(_ context.Context, entry *ClaimQueueEntry) (string, error) {
		if entry.Epoch != 1 {
			t.Fatal("canceled poll admitted another epoch")
		}
		cancel()
		return "", context.Canceled
	}
	writes := 0
	hooks.save = func(*ClaimQueue) error { writes++; return nil }
	if err := pollClaimQueue(ctx, queue, hooks); err != nil || writes != 1 || queue.Entries["0"].Status != "pending" || queue.Entries["1"].Status != "retry" {
		t.Fatalf("canceled poll writes=%d error=%v queue=%+v", writes, err, queue)
	}
}

// Batching diagnostics must not move the durable intent after submission or
// defer a retained transaction outcome until another queue entry completes.
func TestClaimQueuePollPreservesImmediateTransactionCheckpoints(t *testing.T) {
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	entry := &ClaimQueueEntry{Epoch: 1, Status: "retry", ReconcileAttempts: 5, NextRetryAt: now.Format(time.RFC3339Nano)}
	queue := &ClaimQueue{LastDiscovered: 1, Entries: map[string]*ClaimQueueEntry{"1": entry}}
	hooks := claimPollTestHooks(t, now)
	var saved []ClaimQueueEntry
	hooks.save = func(*ClaimQueue) error { saved = append(saved, *entry); return nil }
	hooks.reconcile = func(context.Context, *ClaimQueueEntry) (string, error) { return "", nil }
	hooks.submit = func(_ context.Context, submitted *ClaimQueueEntry) error {
		if len(saved) != 1 || saved[0].Status != "submitting" || saved[0].Attempts != 1 || saved[0].ReconcileAttempts != 0 || saved[0].NextRetryAt != "" {
			t.Fatalf("submission preceded durable intent: %+v", saved)
		}
		submitted.TxHash, submitted.RawTxHex = "synthetic prepared hash", "synthetic prepared bytes"
		return context.DeadlineExceeded
	}
	if err := pollClaimQueue(context.Background(), queue, hooks); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 || saved[1].Status != "uncertain" || saved[1].TxHash != "synthetic prepared hash" || saved[1].RawTxHex != "synthetic prepared bytes" {
		t.Fatalf("uncertain outcome was not durable: %+v", saved)
	}
	hooks.submit = claimPollTestHooks(t, now).submit
	hooks.reconcile = func(context.Context, *ClaimQueueEntry) (string, error) { return "finalized", nil }
	if err := pollClaimQueue(context.Background(), queue, hooks); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 3 || saved[2].Status != "finalized" || saved[2].TxHash != saved[1].TxHash {
		t.Fatalf("finalized outcome was not checkpointed immediately: %+v", saved)
	}
}

// A refused durable submission boundary must prevent the callback entirely.
func TestClaimQueuePollCannotSubmitAfterIntentCheckpointFailure(t *testing.T) {
	queue := &ClaimQueue{LastDiscovered: 0, Entries: map[string]*ClaimQueueEntry{"0": {Epoch: 0, Status: "pending"}}}
	hooks := claimPollTestHooks(t, time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	hooks.reconcile = func(context.Context, *ClaimQueueEntry) (string, error) { return "", nil }
	want := errors.New("synthetic durable intent refusal")
	hooks.save = func(*ClaimQueue) error { return want }
	if err := pollClaimQueue(context.Background(), queue, hooks); !errors.Is(err, want) {
		t.Fatalf("intent checkpoint error=%v", err)
	}
}

// A malformed stored deadline is not permission to retry indefinitely.
func TestClaimQueuePollRejectsMalformedRetryDeadline(t *testing.T) {
	queue := &ClaimQueue{LastDiscovered: 0, Entries: map[string]*ClaimQueueEntry{"0": {Epoch: 0, Status: "retry", NextRetryAt: "not-a-deadline"}}}
	if err := pollClaimQueue(context.Background(), queue, claimPollTestHooks(t, time.Time{})); err == nil {
		t.Fatal("malformed deadline admitted reconciliation")
	}
}

// File identity proves unchanged saves do not replace/fsync the queue. A
// missing or externally changed file still requires a fresh durable write.
func TestClaimQueueStoreSkipsOnlyCurrentIdenticalBytes(t *testing.T) {
	store, err := newClaimQueueStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 0, Entries: map[string]*ClaimQueueEntry{"0": {Epoch: 0, Status: "pending"}}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	prior, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := store.save(queue); err != nil {
			t.Fatal(err)
		}
		current, err := os.Stat(store.path)
		if err != nil || !os.SameFile(prior, current) {
			t.Fatalf("unchanged queue was replaced: %v", err)
		}
	}
	if err := os.WriteFile(store.path, []byte("synthetic replaced bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	restored, err := store.load()
	if err != nil || !reflect.DeepEqual(restored, queue) {
		t.Fatalf("queue replacement was trusted: %+v %v", restored, err)
	}
	if err := os.Remove(store.path); err != nil {
		t.Fatal(err)
	}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.path)
	want, marshalErr := json.MarshalIndent(queue, "", "  ")
	if err != nil || marshalErr != nil || !bytes.Equal(raw, append(want, '\n')) {
		t.Fatalf("missing queue was not durably restored: read=%v encode=%v", err, marshalErr)
	}
}

// Equal bytes cannot bypass the existing private-file boundary or turn an
// external symlink into an acknowledged durable queue generation.
func TestClaimQueueStoreRepairsChangedPrivacyBeforeSkippingSave(t *testing.T) {
	store, err := newClaimQueueStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: -1, Entries: map[string]*ClaimQueueEntry{}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(store.path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("private queue invariant was skipped: %v %v", info, err)
	}
	retained := filepath.Join(filepath.Dir(store.path), "synthetic-retained.json")
	if err := os.Rename(store.path, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(retained, store.path); err != nil {
		t.Fatal(err)
	}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	info, err = os.Lstat(store.path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("symlink replaced a durable queue generation: %v %v", info, err)
	}
}

// A new owner cannot skip its durability boundary just because a previous
// owner's bytes are visible; this also models an unacknowledged prior write.
func TestClaimQueueStoreReacknowledgesPreviouslyVisibleBytes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store, err := newClaimQueueStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: -1, Entries: map[string]*ClaimQueueEntry{}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	prior, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := newClaimQueueStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.save(queue); err != nil {
		t.Fatal(err)
	}
	current, err := os.Stat(reopened.path)
	if err != nil || os.SameFile(prior, current) {
		t.Fatalf("unacknowledged bytes skipped the durability boundary: %v", err)
	}
}
