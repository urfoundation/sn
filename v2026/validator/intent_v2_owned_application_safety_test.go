//go:build linux || darwin

// Owned application reuse survives only complete publication and physical
// custody. All files and native responses below belong to synthetic fixtures.
package validator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The genuine original transaction reaches finalized before any fault is armed.
func ownedApplicationSafetyFinalized(t *testing.T) (*productionContinuationTestFixture, *productionContinuationNativeTestClient, *SteeringIntent) {
	t.Helper()
	fixture := newProductionContinuationTestFixture(t)
	native := installProductionContinuationNative(t, fixture)
	pending := fixture.beginAndLoseAcknowledgement(t, native)
	native.receiptNumber, fixture.production.head = 101, 102
	fixture.production.extrinsicsKVs = map[uint64][]string{101: {pending.Prepared.ExtrinsicHex}}
	if err := fixture.steerer.submitOnceV2(t.Context()); !errors.Is(err, ErrSteeringAlreadyFinal) {
		t.Fatalf("canonical original receipt: %v", err)
	}
	current, err := fixture.steerer.intents.currentV2(t.Context())
	if err != nil || current == nil || current.Status != "finalized" {
		t.Fatalf("authenticated finalized original: %v", err)
	}
	native.applied, fixture.production.head = true, max(uint64(102), current.RevealBlock)
	return fixture, native, current
}

// Reads only the fixture's own synthetic persisted intent for test assertions.
func ownedApplicationSafetyStored(t *testing.T, store *IntentStore) *steeringIntentFile {
	t.Helper()
	raw, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := decodeSteeringIntentFileV2(t.Context(), raw, store.v2.runtime.cfg.EvidenceV2.Bounds.IntentFileLimit())
	if err != nil || stored.Current == nil {
		t.Fatalf("synthetic stored current: %v", err)
	}
	return stored
}

// No negative boundary may leave the newly written lifecycle memoized.
func ownedApplicationSafetyUnpromoted(t *testing.T, store *IntentStore) {
	t.Helper()
	stored := ownedApplicationSafetyStored(t, store)
	if stored.Current.Status != "applied" {
		t.Fatalf("fault did not occur after the actual applied publication: %s", stored.Current.Status)
	}
	digests := intentV2VerifiedHistoryTestDigests(t, []*SteeringIntent{stored.Current})
	if store.v2.verified == nil || slices.Equal(store.v2.verified.digests, digests) {
		t.Fatal("refused owned publication promoted its changed tail")
	}
	if store.v2.active.Load() {
		t.Fatal("completed refused operation retained ownership")
	}
}

// The next normal read must actually repeat public verification after refusal.
func ownedApplicationSafetyReplay(t *testing.T, fixture *productionContinuationTestFixture) {
	t.Helper()
	store := fixture.steerer.intents
	store.v2.referenceReadHooks = releaseMeasurementInputV2ReadHooks{}
	before := fixture.artifactReads.Load()
	current, err := store.currentV2(t.Context())
	if err != nil || current == nil || current.Status != "applied" || fixture.artifactReads.Load() <= before {
		t.Fatalf("refused handoff did not leave normal full replay: objects=%d error=%v", fixture.artifactReads.Load()-before, err)
	}
}

// Actual late Close failures, cancellation and namespace replacement revoke the
// candidate even after the new lifecycle has been atomically published.
func TestIntentV2OwnedApplicationLateCustodyRefusesReuse(t *testing.T) {
	for _, name := range []string{"close error", "cancellation", "leaf replacement", "parent replacement"} {
		t.Logf("control: %s", name)
		fixture, native, current := ownedApplicationSafetyFinalized(t)
		store := fixture.steerer.intents
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		closed := false
		var restore func() error
		defer func() {
			if restore != nil {
				if err := restore(); err != nil {
					t.Error(err)
				}
			}
		}()
		store.v2.referenceReadHooks.afterClose = func(file *os.File) error {
			if closed || file.Name() != filepath.Dir(store.path) || ownedApplicationSafetyStored(t, store).Current.Status != "applied" {
				return nil
			}
			closed = true
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("control preceded the actual directory Close")
			}
			switch name {
			case "close error":
				return syscall.EIO
			case "cancellation":
				cancel()
				return nil
			case "leaf replacement":
				raw, err := os.ReadFile(store.path)
				if err != nil {
					return err
				}
				if err := os.WriteFile(store.path+".synthetic-replacement", raw, 0o600); err != nil {
					return err
				}
				return os.Rename(store.path+".synthetic-replacement", store.path)
			case "parent replacement":
				raw, err := os.ReadFile(store.path)
				if err != nil {
					return err
				}
				parent := filepath.Dir(store.path)
				if err := os.Rename(parent, parent+".synthetic-preserved"); err != nil {
					return err
				}
				restore = func() error {
					return errors.Join(os.RemoveAll(parent), os.Rename(parent+".synthetic-preserved", parent))
				}
				if err := os.Mkdir(parent, 0o700); err != nil {
					return err
				}
				return os.WriteFile(store.path, raw, 0o600)
			}
			return errors.New("unknown synthetic fault")
		}
		broadcasts := len(native.broadcasts)
		err := fixture.steerer.observeProductionApplicationV2(ctx, current)
		var transition *productionSteeringTransition
		if !closed || err == nil || errors.As(err, &transition) || len(native.broadcasts) != broadcasts {
			t.Fatalf("late custody accepted: closed=%t error=%v", closed, err)
		}
		if name == "close error" && !errors.Is(err, syscall.EIO) || name == "cancellation" && !errors.Is(err, context.Canceled) {
			t.Fatalf("late cause was lost: %v", err)
		}
		ownedApplicationSafetyUnpromoted(t, store)
		if restore != nil {
			if err := restore(); err != nil {
				t.Fatal(err)
			}
			restore = nil
		}
		ownedApplicationSafetyReplay(t, fixture)
	}
}

// A successful physical publication cannot bless a proof under changed
// authority or a changed predecessor digest, even if its own bytes are intact.
func TestIntentV2OwnedApplicationChangedProofRefusesReuse(t *testing.T) {
	for _, name := range []string{"authority", "previous digest"} {
		t.Logf("control: %s", name)
		fixture, _, current := ownedApplicationSafetyFinalized(t)
		store, changed := fixture.steerer.intents, false
		store.v2.referenceReadHooks.afterClose = func(file *os.File) error {
			if changed || file.Name() != filepath.Dir(store.path) || ownedApplicationSafetyStored(t, store).Current.Status != "applied" {
				return nil
			}
			changed = true
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("proof control preceded physical Close")
			}
			if name == "authority" {
				store.v2.nativeGapBridge = &releaseNativeGapBridgeV2{}
			} else {
				store.v2.verified.digests[0][0] ^= 1
			}
			return nil
		}
		var transition *productionSteeringTransition
		if err := fixture.steerer.observeProductionApplicationV2(t.Context(), current); !changed || !errors.As(err, &transition) || transition.revealWait {
			t.Fatalf("genuine applied publication did not finish: changed=%t error=%v", changed, err)
		}
		ownedApplicationSafetyUnpromoted(t, store)
		ownedApplicationSafetyReplay(t, fixture)
	}
}

// An exclusive competing marker is introduced after the old entry has been
// authenticated; the real write path, rather than an artificial verdict, fails.
func TestIntentV2OwnedApplicationWriteFailureRefusesReuse(t *testing.T) {
	fixture, _, current := ownedApplicationSafetyFinalized(t)
	store := fixture.steerer.intents
	previous, created := store.v2.verified, false
	marker := filepath.Join(filepath.Dir(store.path), releaseIntentV2Marker)
	store.v2.referenceReadHooks.step = func(operation string, _ *os.File, _ int) error {
		if created || operation != "path-admitted" || store.v2.verified == previous {
			return nil
		}
		file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		created = true
		return errors.Join(file.Sync(), file.Close())
	}
	err := fixture.steerer.observeProductionApplicationV2(t.Context(), current)
	if !created || !errors.Is(err, os.ErrExist) || ownedApplicationSafetyStored(t, store).Current.Status != "finalized" || store.v2.active.Load() {
		t.Fatalf("real exclusive marker failure was not preserved: created=%t error=%v", created, err)
	}
	store.v2.referenceReadHooks = releaseMeasurementInputV2ReadHooks{}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	before := fixture.artifactReads.Load()
	if current, err := store.currentV2(t.Context()); err != nil || current.Status != "finalized" || fixture.artifactReads.Load() != before {
		t.Fatalf("failed write changed the previously verified finalized memo: %v", err)
	}
}

// The owner stays held through the actual final Close and memo handoff.
func TestIntentV2OwnedApplicationCloseRetainsExclusiveOwner(t *testing.T) {
	fixture, _, current := ownedApplicationSafetyFinalized(t)
	store := fixture.steerer.intents
	entered, release := make(chan struct{}), make(chan struct{})
	blocked := false
	store.v2.referenceReadHooks.afterClose = func(file *os.File) error {
		if blocked || file.Name() != filepath.Dir(store.path) || ownedApplicationSafetyStored(t, store).Current.Status != "applied" {
			return nil
		}
		blocked = true
		close(entered)
		select {
		case <-release:
			return nil
		case <-t.Context().Done():
			return t.Context().Err()
		}
	}
	done := make(chan error, 1)
	go func() { done <- fixture.steerer.observeProductionApplicationV2(t.Context(), current) }()
	select {
	case <-entered:
		if concurrent, err := store.currentV2(t.Context()); concurrent != nil || err == nil || !strings.Contains(err.Error(), "already owned") {
			close(release)
			<-done
			t.Fatalf("concurrent reader crossed the late-close owner: %v", err)
		}
		close(release)
	case err := <-done:
		t.Fatalf("application never reached late Close: %v", err)
	case <-time.After(60 * time.Second):
		close(release)
		t.Fatal("application did not reach explicit close barrier")
	}
	select {
	case err := <-done:
		var transition *productionSteeringTransition
		if !errors.As(err, &transition) || transition.revealWait || store.v2.active.Load() {
			t.Fatalf("late-close application did not release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("released close barrier did not join")
	}
	store.v2.referenceReadHooks = releaseMeasurementInputV2ReadHooks{}
	before := fixture.artifactReads.Load()
	if current, err := store.currentV2(t.Context()); err != nil || current.Status != "applied" || fixture.artifactReads.Load() != before {
		t.Fatalf("complete exclusive handoff was not reusable: %v", err)
	}
}

// A generic lifecycle writer and an external canonical edit retain the old
// rule: the changed entry needs full replay before it may be reused.
func TestIntentV2OwnedApplicationDoesNotBlessGenericOrExternalChanges(t *testing.T) {
	fixture, _, current := ownedApplicationSafetyFinalized(t)
	store := fixture.steerer.intents
	if err := store.updateV2(t.Context(), current.VectorHash, "applied", func(intent *SteeringIntent) error {
		intent.ApplicationBlock, intent.ApplicationBlockHash = fixture.production.head, fixture.production.block(fixture.production.head).Hex()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ownedApplicationSafetyUnpromoted(t, store)
	ownedApplicationSafetyReplay(t, fixture)
	stored := ownedApplicationSafetyStored(t, store)
	updated, err := time.Parse(time.RFC3339Nano, stored.Current.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	stored.Current.UpdatedAt = updated.Add(time.Nanosecond).Format(time.RFC3339Nano)
	encoded, err := marshalAttemptSettlementV2JSON(t.Context(), stored, store.v2.runtime.cfg.EvidenceV2.Bounds.IntentFileLimit(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	ownedApplicationSafetyUnpromoted(t, store)
	ownedApplicationSafetyReplay(t, fixture)
}
