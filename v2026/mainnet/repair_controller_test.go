// Actual signed repair/store commands and explicit scheduler barriers exercise
// controller custody without touching any live unit, approver or service manager.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The existing fixture supplies real original approvals and prepared storage.
func repairControllerTestCommand(t *testing.T, f *repairValidatorFixture, kind string, run bool) ([]string, repairControllerManifest) {
	t.Helper()
	raw, err := os.ReadFile(f.approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := repairControllerManifest{Schema: repairControllerSchema, MaximumParallel: 4, Entries: []repairControllerEntry{{Id: "synthetic-incident", Kind: kind, Approval: planFileReference{Path: f.approvalPath, Sha256: monitorReadDigest(raw)}, PublicKey: f.publicKey}}}
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.directory, "repair-manifest.json")
	repairValidatorTestWrite(t, path, raw, 0600)
	checkpoint := filepath.Join(f.directory, "controller.json")
	prepareMainnetSnapshotTest(t, checkpoint, "mainnet-host-action", 64*1024)
	args := []string{"--manifest", path, "--manifest-sha256", monitorReadDigest(raw), "--checkpoint", checkpoint, "--metrics-file", filepath.Join(f.directory, "controller.prom")}
	if run {
		var stdout, stderr bytes.Buffer
		if code := runRepairControllerCommandWithHost(f.storage.Context, args, &stdout, &stderr, func() time.Time { return f.now }, f.host); code != 0 {
			t.Fatal("public repair controller failed", code, stderr.String())
		}
		var record repairControllerRecord
		if err := json.Unmarshal(stdout.Bytes(), &record); err != nil || record.validate(manifest, monitorReadDigest(raw)) != nil || !record.Entries[0].Attempted || record.Entries[0].Status != "completed" {
			t.Fatal("controller did not retain original completed intent", err, record)
		}
	}
	return args, manifest
}

// Public invocation reaches the original one-start journal and exact signature.
func TestRepairControllerSignedValidatorCompletesAndReopensWithoutAllowance(t *testing.T) {
	f := newRepairValidatorFixture(t)
	args, _ := repairControllerTestCommand(t, f, "validator", true)
	var out, diagnostic bytes.Buffer
	if code := runRepairControllerCommandWithHost(f.storage.Context, args, &out, &diagnostic, func() time.Time { return f.now }, f.host); code != 0 || f.starts != 1 {
		t.Fatal("controller reopen replenished a repair allowance", code, f.starts, diagnostic.String())
	}
	raw, err := os.ReadFile(filepath.Join(f.directory, "controller.prom"))
	if err != nil || !bytes.Contains(raw, []byte("sn_mainnet_repair_controller_completed 1")) || bytes.Contains(raw, []byte(f.approval.Plan.IncidentId)) {
		t.Fatal("controller metrics lost finite disposition or disclosed identity", err, string(raw))
	}
}

// Active repair retains separate stop, join and start cuts under its own domain.
func TestRepairControllerSignedActiveValidatorRetainsOriginalCuts(t *testing.T) {
	f := newRepairActiveValidatorFixture(t)
	args, _ := repairControllerTestCommand(t, f.repairValidatorFixture, "active-validator", true)
	var out, diagnostic bytes.Buffer
	if code := runRepairControllerCommandWithHost(f.storage.Context, args, &out, &diagnostic, func() time.Time { return f.now }, f.host); code != 0 || f.stops != 1 || f.starts != 1 {
		t.Fatal("active controller repeated or lost original cuts", code, f.stops, f.starts, diagnostic.String())
	}
}

// A checkpoint and a per-envelope journal have independent permanent markers.
func TestRepairControllerLostCheckpointOrAttemptedJournalNeverReclaims(t *testing.T) {
	for _, lost := range []string{"controller", "envelope"} {
		f := newRepairValidatorFixture(t)
		args, manifest := repairControllerTestCommand(t, f, "validator", false)
		checkpoint := filepath.Join(f.directory, "controller.json")
		if lost == "controller" {
			var out, diagnostic bytes.Buffer
			if code := runRepairControllerCommandWithHost(f.storage.Context, args, &out, &diagnostic, func() time.Time { return f.now }, f.host); code != 0 {
				t.Fatal(code, diagnostic.String())
			}
			if err := os.Remove(checkpoint); err != nil {
				t.Fatal(err)
			}
			out.Reset()
			diagnostic.Reset()
			if code := runRepairControllerCommandWithHost(f.storage.Context, args, &out, &diagnostic, func() time.Time { return f.now }, f.host); code == 0 || f.starts != 1 {
				t.Fatal("missing controller checkpoint recreated original custody", code, f.starts)
			}
		} else {
			step := repairControllerHostStep(f.host, func() time.Time { return f.now })
			var reserved bool
			_, _, err := step(f.storage.Context, manifest.Entries[0], true, func() error { reserved = true; return nil })
			if err == nil || !errors.Is(err, durablevolume.ErrIdentity) || reserved || f.starts != 0 {
				t.Fatal("attempted missing journal was recreated", err, reserved, f.starts)
			}
		}
	}
}

// Read unavailability is pending before intent; malformed custody is held.
func TestRepairControllerIncidentPendingDoesNotConsumeClaimIntent(t *testing.T) {
	for _, mode := range []string{"missing", "stale", "malformed"} {
		f := newRepairValidatorFixture(t)
		_, manifest := repairControllerTestCommand(t, f, "validator", false)
		if mode == "missing" {
			if err := os.Remove(f.approval.Plan.MonitorCheckpoint); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "stale" {
			f.now = f.now.Add(3 * time.Minute)
		}
		if mode == "malformed" {
			repairValidatorTestWrite(t, f.approval.Plan.MonitorCheckpoint, []byte("{invalid"), 0600)
		}
		reserved := false
		_, _, err := repairControllerHostStep(f.host, func() time.Time { return f.now })(f.storage.Context, manifest.Entries[0], false, func() error { reserved = true; return nil })
		status, _ := repairControllerCause(err)
		if err == nil || reserved || f.starts != 0 || mode != "malformed" && status != "pending" || mode == "malformed" && status != "held" {
			t.Fatal("pending/integrity incident crossed claim boundary", mode, err, reserved, status)
		}
	}
}

// The scheduler fixture has no host capability; its persistence callback still
// validates the exact ordered checkpoint at every pre-effect reservation.
func repairControllerTestScheduler(t *testing.T, count int) *repairController {
	t.Helper()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	manifest := repairControllerManifest{Schema: repairControllerSchema, MaximumParallel: 4}
	record := repairControllerRecord{Schema: repairControllerSchema, ManifestHash: monitorReadDigest([]byte("synthetic fixed manifest")), HighWaterAt: now}
	locks := []*sync.Mutex{}
	for index := range count {
		id := fmt.Sprintf("incident-%02d", index)
		entry := repairControllerEntry{Id: id, Kind: "validator", Approval: planFileReference{Path: fmt.Sprintf("/synthetic/approval-%d.json", index), Sha256: monitorReadDigest([]byte(id))}, PublicKey: "0x" + strings.Repeat("7", 64)}
		manifest.Entries = append(manifest.Entries, entry)
		record.Entries = append(record.Entries, repairControllerEntryState{Id: id, ApprovalHash: entry.Approval.Sha256, Status: "pending", ObservedAt: now})
		locks = append(locks, &sync.Mutex{})
	}
	return &repairController{manifest: manifest, record: record, unitLocks: locks, now: func() time.Time { return now }, save: func(r repairControllerRecord) error { return r.validate(manifest, record.ManifestHash) }}
}

// Four explicit active barriers prove bounded admission and joined cancellation.
func TestRepairControllerMaximumFourAndCancellationJoinsEveryChild(t *testing.T) {
	c := repairControllerTestScheduler(t, 9)
	entered := make(chan string, 9)
	var active, joined atomic.Int32
	c.step = func(ctx context.Context, e repairControllerEntry, attempted bool, reserve func() error) (string, bool, error) {
		if err := reserve(); err != nil {
			return "reserve-refused", false, err
		}
		active.Add(1)
		entered <- e.Id
		<-ctx.Done()
		active.Add(-1)
		joined.Add(1)
		return "cancelled", false, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.cycle(ctx) }()
	for range 4 {
		<-entered
	}
	if active.Load() != 4 {
		t.Fatal("bounded controller did not retain four owned children", active.Load())
	}
	for index := range 4 {
		if c.unitLocks[index].TryLock() {
			c.unitLocks[index].Unlock()
			t.Fatal("active child lost its per-unit owner")
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || active.Load() != 0 || joined.Load() != 4 {
		t.Fatal("controller returned before its owned children joined", err, active.Load(), joined.Load())
	}
}

// Pending and hard refusals are per-envelope; integrity wins mixed causes.
func TestRepairControllerPendingAndIntegrityDoNotStarveOtherEnvelopes(t *testing.T) {
	c := repairControllerTestScheduler(t, 6)
	var completed atomic.Int32
	c.step = func(ctx context.Context, e repairControllerEntry, attempted bool, reserve func() error) (string, bool, error) {
		if e.Id == "incident-00" {
			return "incident-pending", false, errRepairControllerPending
		}
		if e.Id == "incident-01" {
			return "source-refused", false, errors.Join(errRpcIntegrity, context.DeadlineExceeded)
		}
		if err := reserve(); err != nil {
			return "reserve-refused", false, err
		}
		completed.Add(1)
		return "resumed-generation-observed", true, nil
	}
	if err := c.cycle(t.Context()); err != nil || completed.Load() != 4 || c.record.Entries[0].Status != "pending" || c.record.Entries[0].Attempted || c.record.Entries[1].Status != "held" || c.record.Entries[1].Cause != "integrity" {
		t.Fatal("one envelope starved others or weakened integrity", err, completed.Load(), c.record)
	}
}

// Failed intent publication must precede and prohibit any effect callback.
func TestRepairControllerUncertainIntentCancelsBeforeAnyEffect(t *testing.T) {
	c := repairControllerTestScheduler(t, 4)
	c.save = func(repairControllerRecord) error { return errMainnetDurablePublicationUncertain }
	var effects atomic.Int32
	c.step = func(ctx context.Context, e repairControllerEntry, attempted bool, reserve func() error) (string, bool, error) {
		if err := reserve(); err != nil {
			return "reserve-refused", false, err
		}
		effects.Add(1)
		return "unexpected-effect", true, nil
	}
	if err := c.cycle(t.Context()); !errors.Is(err, errMainnetDurablePublicationUncertain) || effects.Load() != 0 {
		t.Fatal("uncertain controller intent permitted an effect", err, effects.Load())
	}
}

// Manifest mutation cannot add another action, key, entry or parallel allowance.
func TestRepairControllerManifestAndCheckpointRejectExpandedAuthority(t *testing.T) {
	c := repairControllerTestScheduler(t, 2)
	if err := c.manifest.validate(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"parallel", "kind", "key", "duplicate", "roster"} {
		m := c.manifest
		m.Entries = append([]repairControllerEntry(nil), m.Entries...)
		switch mode {
		case "parallel":
			m.MaximumParallel = 5
		case "kind":
			m.Entries[0].Kind = "blanket-restart"
		case "key":
			m.Entries[0].PublicKey = "0x01"
		case "duplicate":
			m.Entries[1].Id = m.Entries[0].Id
		case "roster":
			for len(m.Entries) <= 64 {
				m.Entries = append(m.Entries, m.Entries[0])
			}
		}
		if m.validate() == nil {
			t.Fatal("expanded controller authority admitted", mode)
		}
	}
	c.record.ContentHash = rootObjectHash(c.record)
	if err := c.record.validate(c.manifest, c.record.ManifestHash); err != nil {
		t.Fatal(err)
	}
	c.record.Entries[0].Status = "completed"
	c.record.ContentHash = ""
	c.record.ContentHash = rootObjectHash(c.record)
	if c.record.validate(c.manifest, c.record.ManifestHash) == nil {
		t.Fatal("completion lost its durable claim intent")
	}
}

// Soft admission can restart under the same manifest; integrity never inherits
// that retry class merely because cancellation appears in the same error tree.
func TestRepairControllerSharedSoftFailureRetainsRetryableExit(t *testing.T) {
	if repairControllerCommandExit(mainnetDurableUnavailable("synthetic observation", os.ErrClosed)) != 1 || repairControllerCommandExit(context.Canceled) != 0 || repairControllerCommandExit(errors.Join(durablevolume.ErrIdentity, context.Canceled)) != 3 || repairControllerCommandExit(errMainnetDurablePublicationUncertain) != 3 {
		t.Fatal("shared observation became terminal or integrity borrowed retry")
	}
}

// The first unit cannot finish until an unrelated fifth envelope runs. Four
// entries for that unit must not occupy every worker behind a blocking lock.
func TestRepairControllerBusyUnitCannotStarveUnrelatedEnvelope(t *testing.T) {
	c := repairControllerTestScheduler(t, 5)
	shared := &sync.Mutex{}
	for index := range 4 {
		c.unitLocks[index] = shared
	}
	healthy := make(chan struct{})
	var observed atomic.Bool
	c.step = func(ctx context.Context, entry repairControllerEntry, attempted bool, reserve func() error) (string, bool, error) {
		if err := reserve(); err != nil {
			return "reserve-refused", false, err
		}
		if entry.Id == "incident-04" {
			observed.Store(true)
			close(healthy)
			return "resumed-generation-observed", true, nil
		}
		select {
		case <-healthy:
			return "resumed-generation-observed", true, nil
		case <-ctx.Done():
			return "cancelled", false, ctx.Err()
		}
	}
	// This is a deadlock backstop, not the positive ordering assertion: no owner
	// can release the first unit until the fifth envelope's explicit signal.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := c.cycle(ctx); err != nil || !observed.Load() || c.record.Entries[4].Status != "completed" {
		t.Fatal("busy unit starved an unrelated signed envelope", err, observed.Load(), c.record)
	}
	busy := 0
	for _, state := range c.record.Entries[:4] {
		if state.Disposition == "original-unit-busy" {
			busy++
			if state.Attempted {
				t.Fatal("busy unit acquired a claim intent")
			}
		}
	}
	if busy == 0 {
		t.Fatal("same-unit workload did not exercise bounded pending admission")
	}
}
