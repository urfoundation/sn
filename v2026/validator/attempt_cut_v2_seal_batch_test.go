//go:build linux || darwin

package validator

// Real signed disk prefixes exercise the sealer's admission cost and the
// immutable copy boundary. Kernel facts are synthetic; file I/O is real.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Unrelated mounts make every real custody admission enumerate a finite census.
type attemptSealBatchHost struct {
	durablevolume.Host
	mountCalls atomic.Uint64
}

// Only kernel facts are substituted; directory, lease and marker checks run.
func (self *attemptSealBatchHost) Mounts() ([]durablevolume.Mount, error) {
	self.mountCalls.Add(1)
	mounts, err := self.Host.Mounts()
	if err != nil {
		return nil, err
	}
	for index := 0; index < 62; index++ {
		mounts = append(mounts, durablevolume.Mount{Id: uint64(100 + index), ParentId: 1, Root: "/", Path: fmt.Sprintf("/synthetic-unrelated-mount-%d", index), Device: durablevolume.Device{Major: 211, Minor: uint32(index)}, FilesystemType: "tmpfs"})
	}
	return mounts, nil
}

// Re-open genuine signed records under a declared volume without retaining
// test-only hooks on the hot decoder. No acceptance result is substituted.
func newAttemptSealBatchFixture(t *testing.T, trails int) (*attemptCutV2SealTestFixture, context.Context, *attemptSealBatchHost) {
	t.Helper()
	fixture := newAttemptCutV2SealTestFixture(t, 8, trails, 0)
	state := newAttemptLedgerDiskTestStateDir(t)
	volume := durablefixture.New(t, t.Context(), state)
	host := &attemptSealBatchHost{Host: volume.Host}
	ctx := durablepath.WithHost(volume.Context, host)
	var legacy bytes.Buffer
	for _, record := range fixture.recordTs {
		if err := json.NewEncoder(&legacy).Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(state, attemptLedgerLegacyName), legacy.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	prepareAttemptLedgerCustodyTest(t, ctx, state, fixture.ledger.identity, fixture.key)
	ledger, err := NewDiskAttemptLedger(ctx, state, fixture.ledger.identity, attemptLedgerDiskTestCoordinator, fixture.key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	fixture.ledger = ledger
	return fixture, ctx, host
}

// The existing sealer entry must not repeat a complete volume/path census for
// every copied record. Count actual guarded admissions, never elapsed time.
func TestAttemptCutV2SealPrefixAmortizesDurableAdmissions(t *testing.T) {
	fixture, ctx, host := newAttemptSealBatchFixture(t, 4)
	store := fixture.ledger.disk.(*attemptRecordStore)
	head, err := fixture.ledger.Head()
	if err != nil {
		t.Fatal(err)
	}
	// Warm immutable backend blocks before comparing admission multiplicity.
	for sequence := uint64(1); sequence <= head.LastSequence; sequence++ {
		if _, err := store.Read(ctx, sequence); err != nil {
			t.Fatal(err)
		}
	}
	host.mountCalls.Store(0)
	for sequence := uint64(1); sequence <= head.LastSequence; sequence++ {
		if _, err := store.Read(ctx, sequence); err != nil {
			t.Fatal(err)
		}
	}
	singleReads := host.mountCalls.Swap(0)
	visited := 0
	err = walkAttemptCutV2SealPrefix(ctx, fixture.ledger, head, fixture.expected, 8, func(record AttemptRecord) error {
		if !reflect.DeepEqual(record, fixture.recordTs[visited]) {
			return fmt.Errorf("signed prefix record %d changed", visited+1)
		}
		visited++
		return nil
	})
	batchReads := host.mountCalls.Load()
	if err != nil || visited != len(fixture.recordTs) {
		t.Fatalf("exact signed prefix: visits=%d: %v", visited, err)
	}
	if singleReads == 0 || batchReads*4 > singleReads {
		t.Fatalf("sealer repeats per-record custody: signed_records=%d single_read_mount_censuses=%d sealer_mount_censuses=%d", visited, singleReads, batchReads)
	}
	t.Logf("signed_records=%d single_read_mount_censuses=%d sealer_mount_censuses=%d", visited, singleReads, batchReads)
}

// Copied records do not override a later instance closure from a callback.
func TestAttemptCutV2SealBatchVisitorCloseRefusesRemainingCopies(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	store := fixture.ledger.disk.(*attemptRecordStore)
	head, err := fixture.ledger.Head()
	if err != nil {
		t.Fatal(err)
	}
	visited := 0
	err = walkAttemptCutV2SealPrefix(t.Context(), fixture.ledger, head, fixture.expected, 8, func(AttemptRecord) error {
		visited++
		return store.Close()
	})
	if visited != 1 || !errors.Is(err, errAttemptRecordStoreClosed) {
		t.Fatalf("cached copies ignored callback close: visits=%d: %v", visited, err)
	}
}

// A later fault also retires already copied bytes before the next visitor.
func TestAttemptCutV2SealBatchVisitorFaultRefusesRemainingCopies(t *testing.T) {
	fixture := newAttemptCutV2SealTestFixture(t, 8, 2, 0)
	store := fixture.ledger.disk.(*attemptRecordStore)
	head, err := fixture.ledger.Head()
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("synthetic later storage fault")
	visited := 0
	err = walkAttemptCutV2SealPrefix(t.Context(), fixture.ledger, head, fixture.expected, 8, func(AttemptRecord) error {
		visited++
		store.latchFault(cause)
		return nil
	})
	if visited != 1 || !errors.Is(err, errAttemptRecordStoreFaulted) || !errors.Is(err, cause) {
		t.Fatalf("cached copies ignored callback fault: visits=%d: %v", visited, err)
	}
}

// Crossing the finite copy limit must preserve each original signed record
// and the captured root, including the final short batch.
func TestAttemptCutV2SealBatchCrossesCopyBoundWithoutChangingPrefix(t *testing.T) {
	limits := attemptLedgerDiskTestLimits()
	limits.MaxRecordCount, limits.MaxTrailCount = 512, 64
	identity := AttemptLedgerIdentity{DeploymentID: "synthetic-batch-prefix", ChainID: 945, GenesisHash: attemptHex32([32]byte{4}), Netuid: 521, ValidatorID: 1, ValidatorUID: 7, NoID: 9}
	fixture := newAttemptCutV2SealTestFixtureForDomainWithLimits(t, 8, 33, 0, false, exactPolicy(t), identity, limits)
	head, err := fixture.ledger.Head()
	if err != nil {
		t.Fatal(err)
	}
	visited := 0
	err = walkAttemptCutV2SealPrefix(t.Context(), fixture.ledger, head, fixture.expected, 8, func(record AttemptRecord) error {
		if !reflect.DeepEqual(record, fixture.recordTs[visited]) {
			return fmt.Errorf("record %d changed across a copy boundary", visited+1)
		}
		visited++
		return nil
	})
	if err != nil || visited != 264 {
		t.Fatalf("multi-batch exact prefix: visits=%d: %v", visited, err)
	}
}

// External I/O is outside all store ownership. If volume identity changes in
// that callback, the closing handoff fence prevents another write/signature.
func TestAttemptCutV2SealBatchRefusesCustodyChangeDuringWrite(t *testing.T) {
	fixture, ctx, host := newAttemptSealBatchFixture(t, 2)
	store := fixture.ledger.disk.(*attemptRecordStore)
	options, _ := newAttemptCutV2SealTestOptions(t, fixture)
	options.ScratchDirectory = filepath.Join(filepath.Dir(store.disk.path), "changed-volume-seal")
	writes := 0
	options.WriteRecords = func(ctx context.Context, _ string, _ []byte) error {
		writes++
		// Reentrant ledger walking must remain available during an upload.
		if err := store.Walk(ctx, 1, 1, func(AttemptRecord) error { return nil }); err != nil {
			return err
		}
		host.Host.(*durablefixture.Host).ReplaceMount()
		return nil
	}
	cut, verified, err := SealAttemptCutV2(ctx, fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	if writes != 1 || cut != nil || verified != (AttemptCutV2ReplayResult{}) || !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatalf("write custody change escaped: writes=%d signed=%v replay=%+v: %v", writes, cut != nil, verified, err)
	}
}

// Full seal and independent fetched policy replay remain mandatory. Record,
// proof and replay census counts make moving work between phases visible.
func TestAttemptCutV2SealBatchFullReplayAdmissionCensus(t *testing.T) {
	fixture, ctx, host := newAttemptSealBatchFixture(t, 4)
	store := fixture.ledger.disk.(*attemptRecordStore)
	options, objects := newAttemptCutV2SealTestOptions(t, fixture)
	options.ScratchDirectory = filepath.Join(filepath.Dir(store.disk.path), "full-batch-seal")
	writeMetadata := options.WriteMetadata
	var recordCensus, proofCensus uint64
	var recordsElapsed, proofsElapsed time.Duration
	started := time.Now()
	options.WriteMetadata = func(ctx context.Context, hash string, raw []byte) error {
		if err := writeMetadata(ctx, hash, raw); err != nil {
			return err
		}
		var manifest AttemptStreamV2Manifest
		if json.Unmarshal(raw, &manifest) == nil && manifest.Schema == AttemptStreamV2Schema {
			if manifest.Kind == AttemptStreamV2Records {
				recordCensus = host.mountCalls.Load()
				recordsElapsed = time.Since(started)
			} else if manifest.Kind == AttemptStreamV2Proofs {
				proofCensus = host.mountCalls.Load()
				proofsElapsed = time.Since(started)
			}
		}
		return nil
	}
	host.mountCalls.Store(0)
	cut, verified, err := SealAttemptCutV2(ctx, fixture.ledger, fixture.expected, fixture.policy, fixture.key, fixture.bounds, options)
	fullElapsed, totalCensus := time.Since(started), host.mountCalls.Load()
	if err != nil {
		t.Fatal(err)
	}
	if recordCensus == 0 || proofCensus <= recordCensus || totalCensus < proofCensus || objects.reads == 0 {
		t.Fatalf("missing full-stage observations: records=%d proofs=%d full=%d fetches=%d", recordCensus, proofCensus, totalCensus, objects.reads)
	}
	assertAttemptCutV2SealTestSuccess(t, fixture, options, cut, verified, fixture.recordTs, 4, 0)
	t.Logf("signed_records=%d records_mount_censuses=%d proofs_mount_censuses=%d fetched_replay_mount_censuses=%d total_mount_censuses=%d records_ns=%d proofs_ns=%d fetched_replay_ns=%d full_ns=%d immutable_writes=%d immutable_reads=%d", len(fixture.recordTs), recordCensus, proofCensus-recordCensus, totalCensus-proofCensus, totalCensus, recordsElapsed.Nanoseconds(), (proofsElapsed - recordsElapsed).Nanoseconds(), (fullElapsed - proofsElapsed).Nanoseconds(), fullElapsed.Nanoseconds(), objects.writes, objects.reads)
}
