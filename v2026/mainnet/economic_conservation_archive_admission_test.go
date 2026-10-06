//go:build linux

// Public signed full-window transitions build actual original snapshots.
// Counters observe source reads, decoding and physical fences; they do not
// substitute a validation result or infer performance from elapsed sleeps.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Every page contains the real128-epoch census,127 unresolved expectations
// and an independently signed successor. Small page counts expose recurrence
// deterministically; the existing129-review restore root remains unchanged.
func newEconomicConservationHistoryAdmissionFixture(t *testing.T, segments int) *economicConservationArchiveFixture {
	t.Helper()
	if segments < 1 || segments > 8 {
		t.Fatal("invalid explicit admission work profile")
	}
	f := newEconomicConservationClaimWindowFixture(t, true, false)
	for ordinal := 1; ordinal <= segments; ordinal++ {
		_, _, args := economicConservationClaimWindowTestPlan(t, f, economicConservationClaimWindowTestNext(t, f), monitorReadDigest([]byte(fmt.Sprintf("synthetic admission work review %d", ordinal))))
		if code, issue := f.apply(t, args, &bytes.Buffer{}, monitorServiceHooks{}); code != 0 {
			t.Fatal("actual original admission history preparation", ordinal, code, issue)
		}
		if ordinal != segments {
			f.reset(t)
		}
	}
	state := f.source.state(t)
	if state.Archive == nil || len(state.Archive.Segments) != segments || len(state.ClaimStates) != 1 || len(state.ClaimStates[0].Epochs) != 128 || len(state.ClaimWindows) != 1 || state.ClaimWindows[0].Ordinal != uint64(segments) || state.ClaimStates[0].Epochs[0].Epoch != 2 {
		t.Fatal("admission work fixture omitted original full-window lineage")
	}
	return f
}

// Counts only production call sites. Holding a snapshot or a cached index
// cannot satisfy these counters without its actual read/decoder/check call.
func economicConservationAdmissionWork(work map[string]uint64) monitorServiceHooks {
	return monitorServiceHooks{
		historyRead: func(role, stage string) {
			if role == economicConservationRole && stage == "conservation-archive-admission" {
				work["original-read"]++
			}
		},
		economicClaimWork: func(_ string, stage string, units uint64) { work[stage] += units },
	}
}

func TestEconomicConservationArchiveAdmissionChecksCompletePrefixOnce(t *testing.T) {
	for _, segments := range []int{2, 4, 8} {
		f := newEconomicConservationHistoryAdmissionFixture(t, segments)
		state := f.source.state(t)
		original := rootObjectHash(state)
		for attempt := 0; attempt < 2; attempt++ {
			work := map[string]uint64{}
			view, err := openEconomicConservationArchive(f.ctx, f.source.policy, &state, economicConservationAdmissionWork(work))
			if err != nil || view == nil {
				t.Fatal("actual full original admission refused", err)
			}
			if work["original-read"] != uint64(segments) || work["archive-checkpoint-decoded"] != uint64(segments) || work["archive-custody-check"] != uint64(segments) || work["archive-index-ready"] != 1 || len(view.owners) != segments || view.claimBasis == nil || len(view.claimBasis.states) != 1 || len(view.claimBasis.states[0].Epochs) != 128 {
				_ = view.close()
				t.Fatal("cold admission skipped originals or repeated growing custody prefixes", segments, attempt, work)
			}
			beforeEntries, beforeBytes := view.entries, view.bytes
			for range 3 {
				if err := view.checkAdmission(); err != nil {
					_ = view.close()
					t.Fatal("published index lost complete original custody", err)
				}
			}
			if work["archive-custody-check"] != 4*uint64(segments) || work["original-read"] != uint64(segments) || work["archive-checkpoint-decoded"] != uint64(segments) || view.entries != beforeEntries || view.bytes != beforeBytes || rootObjectHash(state) != original {
				_ = view.close()
				t.Fatal("successful owner reuse reread original payloads or waived live custody", work)
			}
			if err := view.close(); err != nil {
				t.Fatal(err)
			}
			if !errors.Is(view.check(), os.ErrClosed) || !errors.Is(view.checkAdmission(), os.ErrClosed) {
				t.Fatal("closed admitted index retained original custody")
			}
			t.Logf("full original census: pages=%d epochs=128 reopen=%d work=%v", segments, attempt, work)
		}
	}
}

// The first real page is read before cancellation. Its partial index and
// locks must be discarded, while a separately admitted owner stays healthy.
func TestEconomicConservationArchiveAdmissionCancellationHasNoReusablePrefix(t *testing.T) {
	f := newEconomicConservationHistoryAdmissionFixture(t, 4)
	state := f.source.state(t)
	healthy, err := openEconomicConservationArchive(f.ctx, f.source.policy, &state, monitorServiceHooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := healthy.close(); err != nil {
			t.Error(err)
		}
	}()
	var locks []*os.File
	ctx, cancel := context.WithCancel(context.WithValue(f.ctx, monitorHistoryAdmissionReadKey{}, func(_ string, lock *os.File) error {
		locks = append(locks, lock)
		return nil
	}))
	defer cancel()
	work := map[string]uint64{}
	hooks := economicConservationAdmissionWork(work)
	hooks.economicClaimWork = func(_ string, stage string, units uint64) {
		work[stage] += units
		if stage == "archive-checkpoint-decoded" {
			cancel()
		}
	}
	view, err := openEconomicConservationArchive(ctx, f.source.policy, &state, hooks)
	if view != nil {
		_ = view.close()
	}
	if view != nil || !errors.Is(err, context.Canceled) || work["original-read"] != 1 || work["archive-checkpoint-decoded"] != 1 || work["archive-index-ready"] != 0 || len(locks) != 1 {
		t.Fatal("canceled original prefix escaped admission or read later descendants", err, work)
	}
	for _, lock := range locks {
		if _, err := lock.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("canceled original prefix kept a physical owner", err)
		}
	}
	if err := healthy.check(); err != nil {
		t.Fatal("canceled independent admission invalidated a healthy owner", err)
	}
	clear(work)
	view, err = openEconomicConservationArchive(f.ctx, f.source.policy, &state, economicConservationAdmissionWork(work))
	if err != nil || view == nil {
		t.Fatal("fresh owner could not authenticate complete original history", err)
	}
	defer func() {
		if err := view.close(); err != nil {
			t.Error(err)
		}
	}()
	if work["original-read"] != 4 || work["archive-checkpoint-decoded"] != 4 || work["archive-custody-check"] != 4 || !reflect.DeepEqual(f.source.state(t), state) {
		t.Fatal("new owner inherited a canceled prefix or changed original state", work)
	}
}

// Replacement keeps equal bytes, so the final complete physical fence must
// reject the old original inode after all semantic work has already finished.
func economicConservationReplaceArchiveForTest(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".retained-test-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestEconomicConservationArchiveAdmissionLateReplacementCannotPublishIndex(t *testing.T) {
	f := newEconomicConservationHistoryAdmissionFixture(t, 3)
	state := f.source.state(t)
	var locks []*os.File
	ctx := context.WithValue(f.ctx, monitorHistoryAdmissionReadKey{}, func(_ string, lock *os.File) error {
		locks = append(locks, lock)
		return nil
	})
	work := map[string]uint64{}
	hooks := economicConservationAdmissionWork(work)
	hooks.economicClaimWork = func(_ string, stage string, units uint64) {
		work[stage] += units
		if stage == "archive-index-ready" {
			economicConservationReplaceArchiveForTest(t, state.Archive.Segments[0].Path)
		}
	}
	view, err := openEconomicConservationArchive(ctx, f.source.policy, &state, hooks)
	if view != nil {
		_ = view.close()
	}
	if view != nil || !errors.Is(err, durablevolume.ErrIdentity) || work["original-read"] != 3 || work["archive-checkpoint-decoded"] != 3 || work["archive-index-ready"] != 1 || len(locks) != 3 {
		t.Fatal("late original replacement published an admitted prefix index", err, work)
	}
	for _, lock := range locks {
		if _, err := lock.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("late original replacement leaked a descendant physical owner", err)
		}
	}
}

// The public owner performs no new source read or checkpoint publication when
// the completed admission loses an original immediately before its fence.
func TestEconomicConservationArchiveAdmissionPublicLateCustodyLossStopsBeforeSources(t *testing.T) {
	f := newEconomicConservationHistoryAdmissionFixture(t, 2)
	state := f.source.state(t)
	before, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	reads := f.source.claimReads.Load()
	var output, diagnostic bytes.Buffer
	ready := 0
	hooks := monitorServiceHooks{economicClaimWork: func(_ string, stage string, _ uint64) {
		if stage == "archive-index-ready" {
			ready++
			economicConservationReplaceArchiveForTest(t, state.Archive.Segments[0].Path)
		}
	}}
	code := runMainWithMonitorHooks(f.ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, hooks)
	after, err := os.ReadFile(f.source.checkpoint)
	if code != 3 || err != nil || ready != 1 || output.Len() != 0 || f.source.claimReads.Load() != reads || !bytes.Equal(before, after) || !strings.Contains(diagnostic.String(), "identity") {
		t.Fatal("public late custody refusal reached source observation or changed original head", code, diagnostic.String(), err)
	}
}

func TestEconomicConservationArchiveAdmissionCompletionCancellationReturnsNoIndex(t *testing.T) {
	f := newEconomicConservationHistoryAdmissionFixture(t, 2)
	state := f.source.state(t)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	ready := 0
	hooks := monitorServiceHooks{economicClaimWork: func(_ string, stage string, _ uint64) {
		if stage == "archive-index-ready" {
			ready++
			cancel()
		}
	}}
	view, err := openEconomicConservationArchive(ctx, f.source.policy, &state, hooks)
	if view != nil {
		_ = view.close()
	}
	if view != nil || ready != 1 || !errors.Is(err, context.Canceled) {
		t.Fatal("completed canceled admission returned a partial index", err, ready)
	}
}

// An empty original archive has no physical reader whose context check could
// accidentally mask an omitted owner cancellation check at this last boundary.
func TestEconomicConservationArchiveAdmissionEmptyCancellationReturnsNoIndex(t *testing.T) {
	f := newEconomicConservationArchiveFixture(t, false)
	state := f.source.state(t)
	if state.Archive != nil {
		t.Fatal("empty admission control acquired historical owners")
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	ready := 0
	hooks := monitorServiceHooks{economicClaimWork: func(_ string, stage string, _ uint64) {
		if stage == "archive-index-ready" {
			ready++
			cancel()
		}
	}}
	view, err := openEconomicConservationArchive(ctx, f.source.policy, &state, hooks)
	if view != nil {
		_ = view.close()
	}
	if view != nil || ready != 1 || !errors.Is(err, context.Canceled) {
		t.Fatal("empty canceled admission returned index authority", err, ready)
	}
}

func TestEconomicConservationArchiveAdmissionClosedIndexCannotSeedNewFacts(t *testing.T) {
	f := newEconomicConservationHistoryAdmissionFixture(t, 2)
	state := f.source.state(t)
	view, err := openEconomicConservationArchive(f.ctx, f.source.policy, &state, monitorServiceHooks{})
	if err != nil || view == nil {
		t.Fatal(err)
	}
	entries, retainedBytes := view.entries, view.bytes
	if err := view.close(); err != nil {
		t.Fatal(err)
	}
	if err := view.admit(t.Context(), &economicConservationState{}, &economicConservationState{}); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed original index admitted later descendants", err)
	}
	if err := view.admitEntitlementCensuses(f.ctx, f.source.policy, &state); !errors.Is(err, os.ErrClosed) || view.entries != entries || view.bytes != retainedBytes {
		t.Fatal("closed original census cache admitted later facts", err)
	}
	if err := view.close(); err != nil {
		t.Fatal("repeated close changed ownership result", err)
	}
}
