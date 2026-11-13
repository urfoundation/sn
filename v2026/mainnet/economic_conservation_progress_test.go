// Progress controls use ordered synthetic boundaries for arithmetic and the
// public command for ownership, durable publication, outage and restart paths.
// No synthetic projection supplies financial or provider authority.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Each synthetic height has a distinct canonical hash.
func economicProgressTestBoundary(number uint64) economicEmissionBoundary {
	return economicEmissionBoundary{Number: number, Hash: fmt.Sprintf("0x%064x", number)}
}

// A fresh command has no borrowed historical throughput or chain cadence.
func economicProgressTestTracker() (economicConservationProgressTracker, time.Time) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return economicConservationProgressTracker{cursor: economicProgressTestBoundary(100), anchorAt: now, lastAt: now, anchorValid: true}, now
}

// Pending samples must not reduce a ten-minute operation to its final poll.
func TestEconomicProgressPendingKeepsOriginalPreparationWindow(t *testing.T) {
	tracker, now := economicProgressTestTracker()
	target := economicProgressTestBoundary(200)
	first := tracker.observe(economicProgressTestBoundary(100), &target, now, now, true, false, false, "", true)
	if first.CatchupEtaSeconds != nil || first.BacklogBlocks == nil || *first.BacklogBlocks != 100 {
		t.Fatal("unmeasured backlog acquired an estimate", first)
	}
	for poll := 1; poll < 10; poll++ {
		pending := tracker.observe(economicProgressTestBoundary(100), &target, now, now.Add(time.Duration(poll)*time.Minute), false, true, false, "", true)
		if pending.Preparation != nil || pending.FinalizedCadence != nil || pending.CatchupEtaSeconds != nil || pending.Action != "await-owned-capture-or-replay" {
			t.Fatal("pending work produced new measurements", poll, pending)
		}
	}
	target = economicProgressTestBoundary(220)
	done := tracker.observe(economicProgressTestBoundary(110), &target, now.Add(550*time.Second), now.Add(600*time.Second), true, false, false, "", true)
	if done.Preparation == nil || done.Preparation.Blocks != 10 || done.Preparation.ElapsedNanoseconds != int64(600*time.Second) || !done.Preparation.FromAt.Equal(now) || done.CatchupEtaSeconds == nil || *done.CatchupEtaSeconds != 6600 {
		t.Fatal("pending polls shortened actual preparation or invented a moving-head ETA", done)
	}
	if done.FinalizedCadence == nil || done.FinalizedCadence.Blocks != 20 || done.FinalizedCadence.ElapsedNanoseconds != int64(550*time.Second) || done.FutureBoundaryEtaSeconds != nil || done.RepairEtaSeconds != nil {
		t.Fatal("completion delay became chain cadence or repair time", done)
	}
}

// Lack of progress withdraws an estimate while retaining backlog; its elapsed
// time remains part of the next genuinely completed preparation window.
func TestEconomicProgressStalledReadCannotReusePriorRate(t *testing.T) {
	tracker, now := economicProgressTestTracker()
	target := economicProgressTestBoundary(200)
	first := tracker.observe(economicProgressTestBoundary(110), &target, now.Add(10*time.Second), now.Add(10*time.Second), true, false, false, "", true)
	if first.CatchupEtaSeconds == nil || *first.CatchupEtaSeconds != 90 {
		t.Fatal("positive preparation control failed", first)
	}
	stalled := tracker.observe(economicProgressTestBoundary(110), &target, now.Add(10*time.Second), now.Add(20*time.Second), true, false, false, "", true)
	if stalled.CatchupEtaSeconds != nil || stalled.Preparation != nil || stalled.BacklogBlocks == nil || *stalled.BacklogBlocks != 90 {
		t.Fatal("unchanged cursor refreshed an old rate", stalled)
	}
	recovered := tracker.observe(economicProgressTestBoundary(120), &target, now.Add(40*time.Second), now.Add(40*time.Second), true, false, false, "", true)
	if recovered.Preparation == nil || recovered.Preparation.ElapsedNanoseconds != int64(30*time.Second) || recovered.CatchupEtaSeconds == nil || *recovered.CatchupEtaSeconds != 240 {
		t.Fatal("resumed preparation omitted its preceding stall", recovered)
	}
}

// A source outage or hard hold cannot lend an old rate to its first recovery.
func TestEconomicProgressUnavailableAndHeldInvalidateMeasurementContinuity(t *testing.T) {
	for _, fault := range []struct {
		name  string
		held  bool
		issue string
	}{
		{name: "unavailable", issue: "original RPC unavailable"},
		{name: "held", held: true, issue: "original witness contradicted"},
	} {
		tracker, now := economicProgressTestTracker()
		target := economicProgressTestBoundary(200)
		tracker.observe(economicProgressTestBoundary(110), &target, now.Add(time.Second), now.Add(time.Second), true, false, false, "", true)
		failed := tracker.observe(economicProgressTestBoundary(110), &target, now.Add(time.Second), now.Add(2*time.Second), false, false, fault.held, fault.issue, true)
		if failed.Preparation != nil || failed.CatchupEtaSeconds != nil || failed.FinalizedCadence != nil || failed.BacklogBlocks == nil || *failed.BacklogBlocks != 90 {
			t.Fatal("fault lost retained backlog or reused a rate", fault.name, failed)
		}
		first := tracker.observe(economicProgressTestBoundary(120), &target, now.Add(3*time.Second), now.Add(3*time.Second), true, false, false, "", true)
		if first.Preparation != nil || first.CatchupEtaSeconds != nil {
			t.Fatal("first recovery borrowed measurements across a source outage", fault.name, first)
		}
		second := tracker.observe(economicProgressTestBoundary(130), &target, now.Add(5*time.Second), now.Add(5*time.Second), true, false, false, "", true)
		if second.Preparation == nil || second.CatchupEtaSeconds == nil || *second.CatchupEtaSeconds != 14 {
			t.Fatal("comparable recovery could not measure progress", fault.name, second)
		}
	}
}

// The pending flag cannot bypass policy/checkpoint identity withdrawal.
func TestEconomicProgressPendingCannotBypassOriginalIdentity(t *testing.T) {
	for _, field := range []string{"state-policy", "summary-policy", "checkpoint"} {
		_, now := economicProgressTestTracker()
		target := economicProgressTestBoundary(200)
		state := &economicConservationState{PolicyHash: "sha256:" + strings.Repeat("1", 64), ContentHash: "sha256:" + strings.Repeat("2", 64), Native: monitorEconomicNativeState{Cursor: economicProgressTestBoundary(100), Finalized: &target, LastReadAt: now}}
		summary := economicConservationSummary{PolicyHash: state.PolicyHash, CheckpointHash: state.ContentHash, NativeCurrent: true}
		progress := newEconomicConservationProgress(state, "/synthetic/checkpoint.json", now)
		progress.observe(state, summary, now, time.Second)
		originalPolicy, originalCheckpoint := state.PolicyHash, state.ContentHash
		summary.NativeCurrent, summary.NativePending = false, true
		switch field {
		case "state-policy":
			state.PolicyHash = "sha256:" + strings.Repeat("3", 64)
		case "summary-policy":
			summary.PolicyHash = "sha256:" + strings.Repeat("3", 64)
		case "checkpoint":
			summary.CheckpointHash = "sha256:" + strings.Repeat("3", 64)
		}
		failed := progress.observe(state, summary, now.Add(time.Minute), time.Second)
		if failed.Native.BacklogBlocks != nil || failed.Native.CatchupEtaSeconds != nil || progress.native.anchorValid {
			t.Fatal("pending admitted a changed original identity", field, failed.Native)
		}
		state.PolicyHash, summary.PolicyHash, summary.CheckpointHash = originalPolicy, originalPolicy, originalCheckpoint
		state.Native.Cursor, state.Native.LastReadAt = economicProgressTestBoundary(110), now.Add(2*time.Minute)
		summary.NativeCurrent, summary.NativePending = true, false
		recovered := progress.observe(state, summary, now.Add(2*time.Minute), time.Second)
		if recovered.Native.Preparation != nil || recovered.Native.CatchupEtaSeconds != nil {
			t.Fatal("recovered identity reused its pre-mismatch anchor", field, recovered.Native)
		}
	}
}

// Defensive projection checks never become new financial acceptance gates.
func TestEconomicProgressIncomparableClocksAndBoundariesRemainUnknown(t *testing.T) {
	for _, fault := range []string{"clock-zero", "clock-backwards", "read-future", "read-regresses", "advanced-head-same-read", "cursor-regresses", "cursor-hash", "head-regresses", "head-hash"} {
		tracker, now := economicProgressTestTracker()
		target := economicProgressTestBoundary(200)
		tracker.observe(economicProgressTestBoundary(110), &target, now.Add(10*time.Second), now.Add(10*time.Second), true, false, false, "", true)
		cursor, target := economicProgressTestBoundary(120), economicProgressTestBoundary(210)
		readAt, completedAt := now.Add(20*time.Second), now.Add(25*time.Second)
		switch fault {
		case "clock-zero":
			readAt, completedAt = time.Time{}, time.Time{}
		case "clock-backwards":
			readAt, completedAt = now.Add(5*time.Second), now.Add(5*time.Second)
		case "read-future":
			readAt = now.Add(30 * time.Second)
		case "read-regresses":
			readAt = now.Add(9 * time.Second)
		case "advanced-head-same-read":
			readAt = now.Add(10 * time.Second)
		case "cursor-regresses":
			cursor = economicProgressTestBoundary(100)
		case "cursor-hash":
			cursor = economicProgressTestBoundary(110)
			cursor.Hash = economicProgressTestBoundary(111).Hash
		case "head-regresses":
			target = cursor
		case "head-hash":
			target = economicProgressTestBoundary(200)
			target.Hash = economicProgressTestBoundary(201).Hash
		}
		failed := tracker.observe(cursor, &target, readAt, completedAt, true, false, false, "", true)
		if failed.Preparation != nil || failed.FinalizedCadence != nil || failed.CatchupEtaSeconds != nil || tracker.anchorValid {
			t.Fatal("incomparable source acquired a rate or ETA0", fault, failed)
		}
	}
	tracker, now := economicProgressTestTracker()
	cursor, target := economicProgressTestBoundary(110), economicProgressTestBoundary(200)
	tracker.observe(cursor, &target, now.Add(time.Second), now.Add(10*time.Second), true, false, false, "", true)
	for _, elapsed := range []time.Duration{5 * time.Second, 7 * time.Second} {
		failed := tracker.observe(cursor, &target, now.Add(time.Second), now.Add(elapsed), true, false, false, "", true)
		if tracker.anchorValid || !tracker.lastAt.Equal(now.Add(10*time.Second)) || failed.CatchupEtaSeconds != nil {
			t.Fatal("repeated regressed completion lowered its high-water mark", elapsed, failed)
		}
	}
}

// Zero requires a current comparable target, not merely a retained empty queue.
func TestEconomicProgressCurrentZeroDoesNotPredictFutureOrRepair(t *testing.T) {
	tracker, now := economicProgressTestTracker()
	cursor := economicProgressTestBoundary(100)
	current := tracker.observe(cursor, &cursor, now, now, true, false, false, "", true)
	if current.BacklogBlocks == nil || *current.BacklogBlocks != 0 || current.CatchupEtaSeconds == nil || *current.CatchupEtaSeconds != 0 || current.Preparation != nil || current.FinalizedCadence != nil || current.RepairEtaSeconds != nil || current.FutureBoundaryEtaSeconds != nil {
		t.Fatal("current caught-up state invented a throughput or completion promise", current)
	}
	for _, state := range []struct {
		name    string
		current bool
		pending bool
		target  *economicEmissionBoundary
	}{
		{name: "missing-target", current: true},
		{name: "unavailable", target: &cursor},
		{name: "owned-pending", pending: true, target: &cursor},
	} {
		tracker, now := economicProgressTestTracker()
		unknown := tracker.observe(cursor, state.target, now, now.Add(time.Second), state.current, state.pending, false, "", true)
		if unknown.CatchupEtaSeconds != nil || unknown.Preparation != nil {
			t.Fatal("unknown queue became a zero ETA", state.name, unknown)
		}
	}
}

// The first unchanged-head cadence anchor must not hide a later regressing
// successful-read stamp, including a head advance after that newer read.
func TestEconomicProgressUnchangedHeadRetainsReadHighWater(t *testing.T) {
	for _, advance := range []bool{false, true} {
		tracker, now := economicProgressTestTracker()
		target := economicProgressTestBoundary(200)
		tracker.observe(economicProgressTestBoundary(110), &target, now.Add(10*time.Second), now.Add(10*time.Second), true, false, false, "", true)
		unchanged := tracker.observe(economicProgressTestBoundary(110), &target, now.Add(20*time.Second), now.Add(20*time.Second), true, false, false, "", true)
		if unchanged.FinalizedCadence != nil || !tracker.finalizedAt.Equal(now.Add(10*time.Second)) {
			t.Fatal("same head refreshed the cadence anchor", unchanged)
		}
		if advance {
			target = economicProgressTestBoundary(210)
		}
		failed := tracker.observe(economicProgressTestBoundary(120), &target, now.Add(17*time.Second), now.Add(30*time.Second), true, false, false, "", true)
		if failed.Preparation != nil || failed.FinalizedCadence != nil || failed.CatchupEtaSeconds != nil || tracker.anchorValid {
			t.Fatal("older read borrowed the first cadence timestamp", advance, failed)
		}
		repeated := tracker.observe(economicProgressTestBoundary(120), &target, now.Add(18*time.Second), now.Add(31*time.Second), true, false, false, "", true)
		if repeated.CatchupEtaSeconds != nil || tracker.anchorValid || !tracker.readHighWater.Equal(now.Add(20*time.Second)) {
			t.Fatal("refused read lowered its high-water mark", advance, repeated)
		}
	}
}

// Exact ceilings are safe even when multiplication exceeds machine width.
func TestEconomicProgressDurationCeilingAndOverflow(t *testing.T) {
	for _, row := range []struct {
		name          string
		count, blocks uint64
		elapsed       int64
		want          uint64
		known         bool
	}{
		{name: "fractional-second", count: 1, blocks: 3, elapsed: int64(time.Second), want: 1, known: true},
		{name: "exact-zero", count: 0, blocks: 1, elapsed: 1, want: 0, known: true},
		{name: "wide-product", count: math.MaxUint64, blocks: math.MaxUint64, elapsed: int64(2 * time.Second), want: 2, known: true},
		{name: "unrepresentable-result", count: math.MaxUint64, blocks: 1, elapsed: int64(2 * time.Second)},
		{name: "no-progress", count: 100, elapsed: int64(time.Second)},
		{name: "unordered-time", count: 100, blocks: 1, elapsed: -1},
	} {
		actual := economicConservationRoundedDuration(row.count, row.elapsed, row.blocks, int64(time.Second))
		if (actual != nil) != row.known || actual != nil && *actual != row.want {
			t.Fatal("elapsed projection changed exact ceiling or overflow semantics", row.name, actual)
		}
	}
	tracker, _ := economicProgressTestTracker()
	ancient, future := time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2400, 1, 1, 0, 0, 0, 0, time.UTC)
	tracker.anchorAt, tracker.lastAt = ancient, ancient
	target := economicProgressTestBoundary(200)
	value := tracker.observe(economicProgressTestBoundary(110), &target, future, future, true, false, false, "", true)
	if value.Preparation != nil || value.CatchupEtaSeconds != nil || value.BacklogBlocks == nil || *value.BacklogBlocks != 90 {
		t.Fatal("saturated time.Sub manufactured a finite preparation rate", value)
	}
}

// Public reads retain the same invocation through a real vault HTTP outage;
// assertions run after the joined command, never inside an owned callback.
func TestEconomicProgressPublicOutageRetainsOwnerAndIndependentDomain(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	path := filepath.Join(monitorMetricsTestDir(t), "conservation.prom")
	args := append(f.args(t), "--metrics-file", path, "--follow", "--interval", "1s")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	var output, diagnostic bytes.Buffer
	var snapshots [][]byte
	var readErr error
	events := 0
	hooks := monitorServiceHooks{
		rpcWait: economicConservationTestWait,
		afterEvent: func(_ context.Context, role string) {
			if role != economicConservationRole {
				return
			}
			events++
			raw, err := os.ReadFile(path)
			if err != nil {
				readErr = err
				cancel()
				return
			}
			snapshots = append(snapshots, raw)
			switch events {
			case 1:
				f.vault.unavailable.Store(true)
			case 2:
				f.vault.unavailable.Store(false)
			default:
				cancel()
			}
		},
		wait: func(ctx context.Context, _ string, _ time.Duration) bool {
			f.now = f.now.Add(time.Second)
			return ctx.Err() == nil
		},
	}
	code := runMonitorStorageTestWithHooks(t, ctx, args, &output, &diagnostic, func() time.Time { return f.now }, hooks)
	if code != 0 || readErr != nil || events != 3 || len(snapshots) != 3 {
		t.Fatal("public progress did not join outage and recovery", code, readErr, events, diagnostic.String())
	}
	decoder := json.NewDecoder(&output)
	var first *economicConservationProgressSummary
	for index := 0; index < 3; index++ {
		var summary economicConservationSummary
		if err := decoder.Decode(&summary); err != nil {
			t.Fatal(err)
		}
		progress := summary.Progress
		if progress == nil || progress.Schema != "urnetwork-economic-conservation-progress-v1" || progress.ProcessId != os.Getpid() || progress.PolicyHash != summary.PolicyHash || progress.CheckpointHash != summary.CheckpointHash || progress.CheckpointPath != f.checkpoint || progress.SampleAttempts != uint64(index+1) || progress.NextSampleAfterSeconds == nil || *progress.NextSampleAfterSeconds != 1 {
			t.Fatal("public status lost original owner, checkpoint or bounded next action", index, progress)
		}
		if first == nil {
			first = progress
		} else if !progress.StartedAt.Equal(first.StartedAt) || progress.ProcessId != first.ProcessId {
			t.Fatal("source outage restarted the reported owner", progress)
		}
		metrics := readEconomicMetricsTest(t, snapshots[index])
		if metrics["sample_timestamp_seconds"] != uint64(summary.SampleAt.Unix()) || metrics["entitlement_census_present"] != 0 || metrics["provider_bytes_known"] != 0 || progress.Native.RepairEtaSeconds != nil || progress.Native.FutureBoundaryEtaSeconds != nil {
			t.Fatal("public status invented absent provider evidence or a completion promise", index, metrics)
		}
		if index == 1 && (summary.VaultCurrent || !summary.NativeCurrent || progress.Vault.Preparation != nil || progress.Vault.CatchupEtaSeconds != nil || metrics["vault_catchup_eta_known"] != 0 || progress.Vault.Class != "observation-unavailable") {
			t.Fatal("vault outage hid the independent native read or acquired ETA", summary)
		}
		if index == 2 && (!summary.VaultCurrent || progress.Vault.Preparation != nil) {
			t.Fatal("first real recovery borrowed its pre-outage rate", summary)
		}
	}
}

// A restart must not turn an old checkpoint or downtime into preparation rate.
func TestEconomicProgressPublicRestartStartsNewMeasurementWindow(t *testing.T) {
	f := newEconomicConservationFixture(t, false)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	run := func() economicConservationSummary {
		var output, diagnostic bytes.Buffer
		code := runMonitorStorageTestWithHooks(t, ctx, f.args(t), &output, &diagnostic, func() time.Time { return f.now }, monitorServiceHooks{})
		if code != 0 {
			t.Fatal("public restart read failed", code, diagnostic.String())
		}
		var summary economicConservationSummary
		if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
			t.Fatal(err)
		}
		return summary
	}
	first := run()
	if first.Progress == nil {
		t.Fatal("original public sample lacked operational progress")
	}
	original := f.state(t)
	f.now = f.now.Add(time.Hour)
	next := run()
	if next.Progress == nil || !next.Progress.StartedAt.Equal(f.now) || next.Progress.StartedAt.Equal(first.Progress.StartedAt) || next.Progress.SampleAttempts != 1 || next.Progress.NextSampleAfterSeconds != nil {
		t.Fatal("restart reused an old runtime identity or attempt count", next.Progress)
	}
	if next.Progress.Native.Preparation != nil || next.Progress.Native.FinalizedCadence != nil || next.Progress.Vault.Preparation != nil || next.Progress.Vault.FinalizedCadence != nil || next.NativeCursor != original.Native.Cursor || len(f.state(t).Captures) != len(original.Captures) {
		t.Fatal("restart borrowed downtime throughput or replayed financial effects", next.Progress)
	}
}
