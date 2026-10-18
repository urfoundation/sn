// Public projection follows actual acknowledged queue bytes. Reporting must
// neither consume signing custody capacity nor acknowledge uncertain writes.
package miner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Decode the public bytes, not the owner's private representation.
func claimProgressSnapshot(t *testing.T, owner *claimProgressOwner) (*protocol.ClaimProgress, bool) {
	t.Helper()
	raw, active := owner.snapshot(t.Context())
	value, err := protocol.DecodeClaimProgress(raw)
	if err != nil {
		t.Fatalf("public projection does not decode: %v %s", err, raw)
	}
	return value, active
}

func TestClaimProgressCapacityCannotBlockSignedOutcome(t *testing.T) {
	cfg, _, entry, receipt, block := signedClaimFixture(t, 70, 23)
	cfg.RPC = []string{claimReceiptIdentityTestRPC(t, receipt, block)}
	// Preexisting receipt fields leave exactly the original status transition
	// (uncertain -> finalized, both nine bytes) for actual reconciliation.
	if err := recordFinalizedClaimReceipt(entry, receipt); err != nil {
		t.Fatal(err)
	}
	entry.Status, entry.LastError = "uncertain", "x"
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": entry}}
	raw, err := marshalClaimQueue(queue)
	if err != nil {
		t.Fatal(err)
	}
	entry.LastError = strings.Repeat("x", maximumClaimQueueBytes-len(raw)+1)
	store := newClaimQueueTestStore(t, cfg.StateDir)
	store.progress = newClaimProgressOwner("capacity", nil)
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	before, active := claimProgressSnapshot(t, store.progress)
	if !active {
		t.Fatal("actual original queue was not acknowledged")
	}
	originalRaw, originalHash, attempts := entry.RawTxHex, entry.TxHash, entry.Attempts
	status, err := reconcileSignedClaim(t.Context(), cfg, entry, store)
	if err != nil || status != "finalized" || entry.PublicObservation == nil {
		t.Fatal("actual receipt did not produce observation", status, err)
	}
	entry.Status = status
	withObservation, err := marshalClaimQueue(queue)
	if err != nil || len(withObservation) <= maximumClaimQueueBytes {
		t.Fatal("fixture did not exceed original cap solely with observation", len(withObservation), err)
	}
	if err := store.save(queue); err != nil {
		t.Fatal("optional reporting blocked signed outcome", err)
	}
	committed, present, err := store.head.Read()
	if err != nil || !present || len(committed) != maximumClaimQueueBytes {
		t.Fatal("original capacity or custody changed", len(committed), err)
	}
	var retained ClaimQueue
	if err := json.Unmarshal(committed, &retained); err != nil {
		t.Fatal(err)
	}
	if len(retained.Entries) != 1 || retained.Entries["70"].RawTxHex != originalRaw || retained.Entries["70"].TxHash != originalHash || retained.Entries["70"].Attempts != attempts || retained.Entries["70"].LastError != entry.LastError || retained.Entries["70"].Status != "finalized" || retained.Entries["70"].PublicObservation != nil || entry.PublicObservation != nil {
		t.Fatal("capacity fallback dropped or invented original custody")
	}
	after, active := claimProgressSnapshot(t, store.progress)
	if !active || after.Sequence != before.Sequence+1 || after.OmittedObservations != 1 || after.Entries[0].Observation != nil || after.FinalizedEntries != 1 || after.UnresolvedEntries != 0 {
		t.Fatalf("omitted evidence is not explicit: %+v", after)
	}
}

func TestClaimProgressPriorOptionalMetadataYieldsToCustodyGrowth(t *testing.T) {
	cfg, _, entry, receipt, block := signedClaimFixture(t, 70, 23)
	cfg.RPC = []string{claimReceiptIdentityTestRPC(t, receipt, block)}
	status, err := reconcileSignedClaimTest(t, t.Context(), cfg, entry)
	if err != nil || status != "finalized" || entry.PublicObservation == nil {
		t.Fatal(status, err)
	}
	entry.Status, entry.LastError = status, "x"
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": entry}}
	raw, err := marshalClaimQueue(queue)
	if err != nil {
		t.Fatal(err)
	}
	entry.LastError = strings.Repeat("x", maximumClaimQueueBytes-len(raw)+1)
	ctx := claimQueueTestContext(t, t.Context(), cfg.StateDir)
	store, err := newClaimQueueStore(cfg.StateDir, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	store.progress = newClaimProgressOwner("retained-capacity", nil)
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	before, _ := claimProgressSnapshot(t, store.progress)
	if before.Entries[0].Observation == nil {
		t.Fatal("prior optional evidence not actually retained")
	}
	originalRaw, originalHash := entry.RawTxHex, entry.TxHash
	entry.Attempts += 1000
	if err := store.save(queue); err != nil {
		t.Fatal("prior monitoring metadata blocked operational growth", err)
	}
	if entry.RawTxHex != originalRaw || entry.TxHash != originalHash || entry.PublicObservation != nil {
		t.Fatal("fallback changed signed history or failed to shed optional bytes")
	}
	after, active := claimProgressSnapshot(t, store.progress)
	if !active || after.OmittedObservations != 1 || after.Entries[0].Observation != nil || after.Sequence != before.Sequence+1 {
		t.Fatal("prior optional degradation is not explicit")
	}
	entry.LastError += strings.Repeat("x", 2048)
	if err := store.save(queue); !errors.Is(err, errClaimQueueCapacity) {
		t.Fatal("true operational capacity was weakened", err)
	}
	failed, active := claimProgressSnapshot(t, store.progress)
	if active || failed.Sequence != after.Sequence || failed.QueueSha256 != after.QueueSha256 {
		t.Fatal("true capacity failure changed public acknowledgement")
	}
}

func TestClaimProgressFailedDurableSaveCannotPublish(t *testing.T) {
	cfg, _, entry, _, _ := signedClaimFixture(t, 70, 23)
	store := newClaimQueueTestStore(t, cfg.StateDir)
	store.progress = newClaimProgressOwner("lost-ack", nil)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": entry}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	before, _ := claimProgressSnapshot(t, store.progress)
	entry.Attempts++
	entry.LastError = "private local path and secret diagnostic never leaves this queue"
	store.afterSync = func(*os.File) error { return syscall.EIO }
	if err := store.save(queue); !errors.Is(err, durablehead.ErrUncertain) {
		t.Fatal("actual post-sync loss did not retain uncertainty", err)
	}
	after, active := claimProgressSnapshot(t, store.progress)
	if active || after.Status != "unavailable" || after.Sequence != before.Sequence || after.QueueSha256 != before.QueueSha256 || !reflect.DeepEqual(after.Entries, before.Entries) {
		t.Fatal("failed acknowledgement advanced public evidence", before, after)
	}
	pending, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := store.ctx
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newClaimQueueStore(cfg.StateDir, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	loaded, err := reopened.load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Entries["70"].RawTxHex != entry.RawTxHex || loaded.Entries["70"].Attempts != entry.Attempts {
		t.Fatal("joined recovery replaced signed history")
	}
	reopened.progress = newClaimProgressOwner("lost-ack", nil)
	if err := reopened.save(loaded); err != nil {
		t.Fatal(err)
	}
	recovered, active := claimProgressSnapshot(t, reopened.progress)
	digest := sha256.Sum256(pending)
	if !active || recovered.InstanceId == before.InstanceId || recovered.Sequence != 1 || recovered.QueueSha256 != hex.EncodeToString(digest[:]) {
		t.Fatal("restart failed to distinguish original bytes from publication generation", recovered)
	}
	if err := reopened.close(); err != nil {
		t.Fatal(err)
	}
	reopened.progress.close()
	closed, active := claimProgressSnapshot(t, reopened.progress)
	if active || closed.Status != "closed" || closed.Sequence != recovered.Sequence || closed.QueueSha256 != recovered.QueueSha256 {
		t.Fatal("closed owner invented or forgot acknowledged evidence")
	}
}

func TestClaimProgressCensusKeepsOldestAndSeparatesHeartbeat(t *testing.T) {
	store := newClaimQueueTestStore(t, filepath.Join(t.TempDir(), "claims"))
	store.progress = newClaimProgressOwner("bounded", nil)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 99, Entries: map[string]*ClaimQueueEntry{}}
	for epoch := int64(0); epoch < 100; epoch++ {
		status := "retry"
		if epoch >= 90 {
			status = "finalized"
		}
		queue.Entries[fmt.Sprint(epoch)] = &ClaimQueueEntry{Epoch: epoch, Status: status, Attempts: 2}
	}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	first, active := claimProgressSnapshot(t, store.progress)
	if !active || len(first.Entries) != 64 || first.TotalEntries != 100 || first.UnresolvedEntries != 90 || first.FinalizedEntries != 10 || first.OmittedEntries != 36 || first.OmittedUnresolvedEntries != 26 || first.OldestUnresolvedEpoch == nil || *first.OldestUnresolvedEpoch != 0 || first.Entries[0].Epoch != 0 || first.Entries[63].Epoch != 63 {
		t.Fatalf("bounded census hides old unresolved work: %+v", first)
	}
	queue.Entries["99"].UpdatedAt = "2026-01-01T00:00:00Z"
	queue.Entries["0"].Attempts++
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	second, active := claimProgressSnapshot(t, store.progress)
	if !active || second.Sequence != first.Sequence+1 || second.QueueSha256 == first.QueueSha256 || !reflect.DeepEqual(second.Entries, first.Entries) || second.UnresolvedEntries != first.UnresolvedEntries || second.FinalizedEntries != first.FinalizedEntries || *second.OldestUnresolvedEpoch != *first.OldestUnresolvedEpoch {
		t.Fatal("heartbeat was confused with semantic settlement progress")
	}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	third, _ := claimProgressSnapshot(t, store.progress)
	if third.Sequence != second.Sequence+1 || third.QueueSha256 != second.QueueSha256 || !reflect.DeepEqual(third.Entries, second.Entries) {
		t.Fatal("same-byte acknowledgement fabricated a changed outcome")
	}
}

func TestClaimProgressFutureObservationDegradesWithoutStoppingSave(t *testing.T) {
	store := newClaimQueueTestStore(t, filepath.Join(t.TempDir(), "claims"))
	store.progress = newClaimProgressOwner("clock", nil)
	observation := absentClaimObservation(4)
	observation.ObservedAt = "9999-01-01T00:00:00Z"
	entry := &ClaimQueueEntry{Epoch: 4, Status: "no-claim", PublicObservation: observation}
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 4, Entries: map[string]*ClaimQueueEntry{"4": entry}}
	if err := store.save(queue); err != nil {
		t.Fatal("reporting clock refused original queue", err)
	}
	value, active := claimProgressSnapshot(t, store.progress)
	if !active || value.NoClaimEntries != 1 || value.OmittedObservations != 1 || value.Entries[0].ObservationStatus != "unknown" || value.Entries[0].Observation != nil {
		t.Fatal("future evidence acquired freshness", value)
	}
	raw, present, err := store.head.Read()
	if err != nil || !present || !bytes.Contains(raw, []byte("9999-01-01")) {
		t.Fatal("public degradation rewrote retained observation bytes", err)
	}
}

func TestClaimProgressCanceledReadAndMutationCannotExposeWriterState(t *testing.T) {
	store := newClaimQueueTestStore(t, filepath.Join(t.TempDir(), "claims"))
	owner := newClaimProgressOwner("copy", nil)
	store.progress = owner
	entry := &ClaimQueueEntry{Epoch: 8, Status: "no-claim", RawTxHex: "not-published-secret", LastError: "secret-diagnostic", PublicObservation: absentClaimObservation(8)}
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 8, Entries: map[string]*ClaimQueueEntry{"8": entry}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	before, _ := owner.snapshot(t.Context())
	entry.PublicObservation.Authority = "forged-independent-finality"
	entry.Status = "pending"
	after, _ := owner.snapshot(t.Context())
	if !bytes.Equal(before, after) || bytes.Contains(after, []byte("secret")) || bytes.Contains(after, []byte("raw_tx")) {
		t.Fatal("public snapshot aliases writer data or secrets")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if raw, active := owner.snapshot(ctx); raw != nil || active {
		t.Fatal("canceled reader received data")
	}
}
