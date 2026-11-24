//go:build linux || darwin

// A closed discovery range ends at or below the finalized EVM head and never
// changes, so a process reads it once per permitted hotkey. These tests drive
// the real refresh against the fixture node and check which ranges and
// hotkeys each refresh asks the node for.
package validator

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Thirty-block ranges from the deployment block 1001. With the head at 1200,
// six ranges are closed and [1181, 1200] is open.
func validatorUploadRangeCacheTestConfig(fixture *validatorUploadAuthorityTestFixture) ValidatorUploadAdmissionConfig {
	config := fixture.config()
	config.BlocksPerRange = 30
	config.MaximumRanges = 7
	return config
}

var validatorUploadRangeCacheTestClosed = [][2]uint64{{1001, 1030}, {1031, 1060}, {1061, 1090}, {1091, 1120}, {1121, 1150}, {1151, 1180}}

// One process's admission, with its own empty cache.
func validatorUploadRangeCacheTestOwner(t *testing.T, fixture *validatorUploadAuthorityTestFixture) *ValidatorUploadAdmission {
	t.Helper()
	owner, err := newValidatorUploadAdmissionState(t.Context(), fixture.chain, fixture.native.chain, validatorUploadRangeCacheTestConfig(fixture))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.cancel(); owner.invalidate(errors.New("fixture closed")); owner.leases.Wait() })
	return owner
}

// One activation query: its block range and hotkey OR-list.
type validatorUploadRangeCacheTestQuery struct {
	from, to uint64
	hotkeys  []common.Hash
}

// The activation queries since the last call, sorted by range: a cold scan
// asks in parallel, so arrival order is not fixed.
func validatorUploadRangeCacheTestQueries(fixture *validatorUploadAuthorityTestFixture) []validatorUploadRangeCacheTestQuery {
	fixture.logLock.Lock()
	defer fixture.logLock.Unlock()
	queries := make([]validatorUploadRangeCacheTestQuery, 0, len(fixture.logRanges))
	for index, blocks := range fixture.logRanges {
		queries = append(queries, validatorUploadRangeCacheTestQuery{from: blocks[0], to: blocks[1], hotkeys: fixture.logHotkeys[index]})
	}
	fixture.logRanges, fixture.logHotkeys = nil, nil
	slices.SortStableFunc(queries, func(a, b validatorUploadRangeCacheTestQuery) int { return cmp.Compare(a.from, b.from) })
	return queries
}

func validatorUploadRangeCacheTestRanges(queries []validatorUploadRangeCacheTestQuery) [][2]uint64 {
	ranges := make([][2]uint64, 0, len(queries))
	for _, query := range queries {
		ranges = append(ranges, [2]uint64{query.from, query.to})
	}
	return ranges
}

func validatorUploadRangeCacheTestRefresh(t *testing.T, fixture *validatorUploadAuthorityTestFixture, owner *ValidatorUploadAdmission) []validatorUploadRangeCacheTestQuery {
	t.Helper()
	if err := owner.refresh(t.Context(), fixture.now); err != nil {
		// The production loop clears the admitted set on a failed refresh.
		owner.invalidate(err)
		t.Fatalf("refresh: %v", err)
	}
	return validatorUploadRangeCacheTestQueries(fixture)
}

func validatorUploadRangeCacheTestBlockReads(fixture *validatorUploadAuthorityTestFixture, block uint64) int {
	fixture.logLock.Lock()
	defer fixture.logLock.Unlock()
	return fixture.blockReads[block]
}

// After the first scan, a refresh asks only for the open range at the head.
// The validator's activation in the first, closed range stays admitted from
// the kept rows. Before the cache, every refresh read every range again.
func TestValidatorUploadDiscoverySteadyStateReadsOnlyTheOpenRange(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner := validatorUploadRangeCacheTestOwner(t, fixture)
	var reads []uint64
	owner.refreshReport = func(report validatorUploadRefreshReport) { reads = append(reads, report.rangeReads) }
	first := validatorUploadRangeCacheTestRefresh(t, fixture, owner)
	if want := append(slices.Clone(validatorUploadRangeCacheTestClosed), [2]uint64{1181, 1200}); !slices.Equal(validatorUploadRangeCacheTestRanges(first), want) {
		t.Fatalf("the first scan did not read every range once: %v", validatorUploadRangeCacheTestRanges(first))
	}
	for range 2 {
		steady := validatorUploadRangeCacheTestRefresh(t, fixture, owner)
		if got := validatorUploadRangeCacheTestRanges(steady); !slices.Equal(got, [][2]uint64{{1181, 1200}}) {
			t.Fatalf("a steady refresh read closed ranges again: %v", got)
		}
		if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, fixture.now); err != nil {
			t.Fatalf("the activation in a kept range lost its admission: %v", err)
		}
	}
	if !slices.Equal(reads, []uint64{7, 1, 1}) {
		t.Fatalf("refresh reports name %v range reads, not 7, 1, 1", reads)
	}
}

// A hotkey that gains a permit is read in every closed range once, alone in
// the OR-list, and its activation there is found and admitted. The hotkeys
// already read are not asked for again.
func TestValidatorUploadDiscoveryBackfillsANewlyPermittedHotkey(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	validator := common.Hash(fixture.authority.Expected.Hotkey)
	// The subnet owner's hotkey is in the census throughout and has no
	// activation; the validator's hotkey holds no permit yet.
	fixture.subnetOwnerHotkey = [32]byte{0x6f, 0x77, 0x6e}
	subnetOwner := common.Hash(fixture.subnetOwnerHotkey)
	fixture.currentPermit = false
	owner := validatorUploadRangeCacheTestOwner(t, fixture)
	for _, query := range validatorUploadRangeCacheTestRefresh(t, fixture, owner) {
		if !slices.Equal(query.hotkeys, []common.Hash{subnetOwner}) {
			t.Fatalf("prerequisite: the census is the subnet owner alone: %v", query.hotkeys)
		}
	}
	if len(owner.entries) != 0 {
		t.Fatal("prerequisite: nothing admitted without a permit")
	}
	fixture.currentPermit = true
	both := []common.Hash{validator, subnetOwner}
	slices.SortFunc(both, func(a, b common.Hash) int { return bytes.Compare(a[:], b[:]) })
	backfill := validatorUploadRangeCacheTestRefresh(t, fixture, owner)
	if got := validatorUploadRangeCacheTestRanges(backfill); !slices.Equal(got, append(slices.Clone(validatorUploadRangeCacheTestClosed), [2]uint64{1181, 1200})) {
		t.Fatalf("the backfill did not read each range once: %v", got)
	}
	for _, query := range backfill {
		want := []common.Hash{validator}
		if query.to == 1200 {
			want = both
		}
		if !slices.Equal(query.hotkeys, want) {
			t.Fatalf("range %d..%d asked for %v, not %v", query.from, query.to, query.hotkeys, want)
		}
	}
	if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, fixture.now); err != nil {
		t.Fatalf("the backfilled activation was not admitted: %v", err)
	}
	steady := validatorUploadRangeCacheTestRefresh(t, fixture, owner)
	if len(steady) != 1 || steady[0].from != 1181 || !slices.Equal(steady[0].hotkeys, both) {
		t.Fatalf("after the backfill a refresh read more than the open range: %v", steady)
	}
}

// The range that holds the head is read on each refresh while it is open.
// Once the head reaches its end it is read once more, whole, and kept.
func TestValidatorUploadDiscoveryRangeAtTheHeadStaysOpenUntilClosed(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner := validatorUploadRangeCacheTestOwner(t, fixture)
	for _, step := range []struct {
		head uint64
		want [][2]uint64
	}{
		{head: 1170, want: append(slices.Clone(validatorUploadRangeCacheTestClosed[:5]), [2]uint64{1151, 1170})},
		{head: 1175, want: [][2]uint64{{1151, 1175}}},
		{head: 1180, want: [][2]uint64{{1151, 1180}}},
		{head: 1180, want: nil},
		{head: 1190, want: [][2]uint64{{1181, 1190}}},
		{head: 1200, want: [][2]uint64{{1181, 1200}}},
	} {
		// Each finalized head has its own hash.
		fixture.block, fixture.blockHash = step.head, [32]byte{0xa1, byte(step.head), byte(step.head >> 8)}
		if got := validatorUploadRangeCacheTestRanges(validatorUploadRangeCacheTestRefresh(t, fixture, owner)); !slices.Equal(got, step.want) {
			t.Fatalf("head %d read %v, not %v", step.head, got, step.want)
		}
		if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, fixture.now); err != nil {
			t.Fatalf("head %d: the validator lost its admission: %v", step.head, err)
		}
	}
}

// A new process keeps nothing: its first refresh reads every range for the
// whole census, and its next only the open range. Another process's cache is
// its own.
func TestValidatorUploadDiscoveryRestartScansColdOnce(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	running := validatorUploadRangeCacheTestOwner(t, fixture)
	validatorUploadRangeCacheTestRefresh(t, fixture, running)
	validatorUploadRangeCacheTestRefresh(t, fixture, running)
	restarted := validatorUploadRangeCacheTestOwner(t, fixture)
	cold := validatorUploadRangeCacheTestRefresh(t, fixture, restarted)
	if got := validatorUploadRangeCacheTestRanges(cold); !slices.Equal(got, append(slices.Clone(validatorUploadRangeCacheTestClosed), [2]uint64{1181, 1200})) {
		t.Fatalf("a restarted process did not scan every range: %v", got)
	}
	for _, query := range cold {
		if !slices.Equal(query.hotkeys, []common.Hash{common.Hash(fixture.authority.Expected.Hotkey)}) {
			t.Fatalf("the cold scan did not ask for the whole census: %v", query.hotkeys)
		}
	}
	for _, owner := range []*ValidatorUploadAdmission{restarted, running} {
		if got := validatorUploadRangeCacheTestRanges(validatorUploadRangeCacheTestRefresh(t, fixture, owner)); !slices.Equal(got, [][2]uint64{{1181, 1200}}) {
			t.Fatalf("a warm process read closed ranges: %v", got)
		}
	}
}

// The six closed ranges of a cold scan are read four at a time, never more.
// Each read waits until four are in flight or every cold read has started, so
// a larger or smaller bound shows in the peak. The open range follows alone.
func TestValidatorUploadDiscoveryColdScanReadsFourAtATime(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner := validatorUploadRangeCacheTestOwner(t, fixture)
	var inFlight, peak, started atomic.Int64
	fixture.logGate = func(ctx context.Context, from, to uint64) error {
		started.Add(1)
		now := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			prior := peak.Load()
			if now <= prior || peak.CompareAndSwap(prior, now) {
				break
			}
		}
		deadline := time.Now().Add(2 * time.Second)
		for inFlight.Load() < validatorUploadColdScanParallelism && started.Load() < int64(len(validatorUploadRangeCacheTestClosed)) && time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
		return nil
	}
	validatorUploadRangeCacheTestRefresh(t, fixture, owner)
	if peak.Load() != validatorUploadColdScanParallelism {
		t.Fatalf("the cold scan peaked at %d concurrent reads, not %d", peak.Load(), validatorUploadColdScanParallelism)
	}
	peak.Store(0)
	validatorUploadRangeCacheTestRefresh(t, fixture, owner)
	if peak.Load() != 1 {
		t.Fatalf("a warm refresh read %d ranges at once", peak.Load())
	}
}

// The first failed read of a cold scan cancels the reads in flight and starts
// no other: the refresh fails at once with that read's error, and the
// ranges after the first four are never asked for.
func TestValidatorUploadDiscoveryColdScanFailureCancelsTheRest(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner := validatorUploadRangeCacheTestOwner(t, fixture)
	var started, cancelled, unreleased atomic.Int64
	fixture.logGate = func(ctx context.Context, from, to uint64) error {
		started.Add(1)
		if from == 1061 {
			return errors.New("fixture refuses this range read")
		}
		select {
		case <-ctx.Done():
			cancelled.Add(1)
			return ctx.Err()
		case <-time.After(10 * time.Second):
			unreleased.Add(1)
			return nil
		}
	}
	begin := time.Now()
	err := owner.refresh(t.Context(), fixture.now)
	if err == nil || !strings.Contains(err.Error(), "fixture refuses this range read") {
		t.Fatalf("the refresh did not fail with the failed read's error: %v", err)
	}
	if elapsed := time.Since(begin); elapsed > 5*time.Second {
		t.Fatalf("the refresh waited %s for reads the failure should have cancelled", elapsed)
	}
	for deadline := time.Now().Add(5 * time.Second); cancelled.Load() < started.Load()-1 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if unreleased.Load() != 0 || cancelled.Load() != started.Load()-1 {
		t.Fatalf("of %d started reads, %d were cancelled and %d ran on", started.Load(), cancelled.Load(), unreleased.Load())
	}
	for _, query := range validatorUploadRangeCacheTestQueries(fixture) {
		if query.from >= 1121 {
			t.Fatalf("range %d..%d was read after the failure", query.from, query.to)
		}
	}
}

// A kept row's block is rechecked against the canonical chain on every
// refresh, though its range is not read again. When the canonical hash at
// that block changes, the refresh fails rather than admit from the kept row,
// and every kept range is dropped: once the node answers consistently again,
// the next refresh reads every range afresh, as before the cache.
func TestValidatorUploadDiscoveryRechecksKeptRows(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner := validatorUploadRangeCacheTestOwner(t, fixture)
	validatorUploadRangeCacheTestRefresh(t, fixture, owner)
	block := fixture.events[0].Block
	before := validatorUploadRangeCacheTestBlockReads(fixture, block)
	if got := validatorUploadRangeCacheTestRanges(validatorUploadRangeCacheTestRefresh(t, fixture, owner)); !slices.Equal(got, [][2]uint64{{1181, 1200}}) {
		t.Fatalf("prerequisite: the kept range is not read again: %v", got)
	}
	if validatorUploadRangeCacheTestBlockReads(fixture, block) <= before {
		t.Fatal("a kept row's block was not rechecked")
	}
	fixture.logLock.Lock()
	fixture.canonicalRewrites = map[uint64]common.Hash{block: {0xee}}
	fixture.logLock.Unlock()
	err := owner.refresh(t.Context(), fixture.now)
	if err == nil || !strings.Contains(err.Error(), "canonical EVM boundary changed") {
		t.Fatalf("a kept row survived a changed canonical hash: %v", err)
	}
	if got := validatorUploadRangeCacheTestRanges(validatorUploadRangeCacheTestQueries(fixture)); !slices.Equal(got, [][2]uint64{{1181, 1200}}) {
		t.Fatalf("the recheck, not a fresh read, must catch the change: %v", got)
	}
	if len(owner.closedRanges) != 0 || owner.closedRangeRows != 0 {
		t.Fatalf("a changed canonical hash left %d ranges kept", len(owner.closedRanges))
	}
	fixture.logLock.Lock()
	fixture.canonicalRewrites = nil
	fixture.logLock.Unlock()
	if got := validatorUploadRangeCacheTestRanges(validatorUploadRangeCacheTestRefresh(t, fixture, owner)); !slices.Equal(got, append(slices.Clone(validatorUploadRangeCacheTestClosed), [2]uint64{1181, 1200})) {
		t.Fatalf("after the change, a refresh did not read every range afresh: %v", got)
	}
	if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, fixture.now); err != nil {
		t.Fatalf("the lane did not recover: %v", err)
	}
}

// Past the kept-row budget a range is not kept: it is read again on every
// refresh, as before the cache, and admission is unchanged.
func TestValidatorUploadDiscoveryPastTheBudgetReadsAgain(t *testing.T) {
	t.Parallel()
	fixture := newValidatorUploadAuthorityTestFixture(t)
	owner := validatorUploadRangeCacheTestOwner(t, fixture)
	owner.closedRangeRowBudget = 0
	for _, want := range [][][2]uint64{
		append(slices.Clone(validatorUploadRangeCacheTestClosed), [2]uint64{1181, 1200}),
		{{1001, 1030}, {1181, 1200}},
	} {
		if got := validatorUploadRangeCacheTestRanges(validatorUploadRangeCacheTestRefresh(t, fixture, owner)); !slices.Equal(got, want) {
			t.Fatalf("read %v, not %v", got, want)
		}
		if err := validatorUploadDiscoveryTestBegin(t, fixture, owner, fixture.authority.Expected, fixture.now); err != nil {
			t.Fatal(err)
		}
	}
	// Empty ranges fit any budget, so only the validator's range is read again.
	if len(owner.closedRanges) != len(validatorUploadRangeCacheTestClosed)-1 || owner.closedRanges[1001] != nil || owner.closedRangeRows != 0 {
		t.Fatalf("the budget kept %d ranges and %d rows", len(owner.closedRanges), owner.closedRangeRows)
	}
}

// Ranges are fixed from the deployment block; the one holding the head is
// open, and a head at a range's end closes it. More ranges than the budget
// is refused.
func TestValidatorUploadDiscoveryRanges(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		head, maximum uint64
		want          []validatorUploadRange
		err           bool
	}{
		{head: 1001, maximum: 7, want: []validatorUploadRange{{from: 1001, to: 1001}}},
		{head: 1030, maximum: 7, want: []validatorUploadRange{{from: 1001, to: 1030, closed: true}}},
		{head: 1031, maximum: 7, want: []validatorUploadRange{{from: 1001, to: 1030, closed: true}, {from: 1031, to: 1031}}},
		{head: 1210, maximum: 7, want: []validatorUploadRange{{1001, 1030, true}, {1031, 1060, true}, {1061, 1090, true}, {1091, 1120, true}, {1121, 1150, true}, {1151, 1180, true}, {1181, 1210, true}}},
		{head: 1211, maximum: 7, err: true},
		{head: 1000, maximum: 7, err: true},
	} {
		got, err := validatorUploadDiscoveryRanges(1001, test.head, 30, test.maximum)
		if (err != nil) != test.err || !slices.Equal(got, test.want) {
			t.Errorf("head %d: %v / %v, want %v", test.head, got, err, test.want)
		}
	}
}

// A backfill's rows join a range's kept rows under the checks of one read.
func TestValidatorUploadDiscoveryMergeKeepsReadChecks(t *testing.T) {
	t.Parallel()
	event := func(digest byte, block, index uint64, hash byte) ValidatorUploadActivationEvent {
		return ValidatorUploadActivationEvent{Digest: [32]byte{digest}, Hotkey: [32]byte{digest, 1}, NoID: 1, Block: block, BlockHash: [32]byte{hash}, Index: index}
	}
	merged, err := mergeValidatorUploadRangeEvents([]ValidatorUploadActivationEvent{event(1, 1005, 0, 5), event(2, 1009, 3, 9)}, []ValidatorUploadActivationEvent{event(3, 1005, 2, 5)})
	if err != nil || len(merged) != 3 || merged[1].Digest != [32]byte{3} {
		t.Fatalf("rows of one range did not merge in canonical order: %v / %v", merged, err)
	}
	for name, rows := range map[string][]ValidatorUploadActivationEvent{
		"duplicate digest":       {event(1, 1007, 0, 7)},
		"same log twice":         {event(3, 1005, 0, 5)},
		"two hashes for a block": {event(3, 1005, 2, 6)},
	} {
		_, err := mergeValidatorUploadRangeEvents([]ValidatorUploadActivationEvent{event(1, 1005, 0, 5)}, rows)
		if err == nil || errors.Is(err, errValidatorUploadCanonicalChanged) != (name == "two hashes for a block") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
