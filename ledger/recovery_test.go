package ledger

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/protocol"
)

// The phase C recovery component (VERSION2-FINAL.md §16 step 6, owner
// decision 2026-10-31): the swept v1 funding reaches vault2 as surplus
// assigned to a v2 epoch's funding. Epoch 3's missed-root funding is
// allocated by epoch 3's own rows; everything else in the surplus, epoch 1's
// share included, is ordinary funding of the receiving epoch, whose rows
// allocate f_k − R.
const (
	recoveryEpoch3 = 4_132_715_621_039 // vault1 entitlement(3,1).funded
	recoveryEpoch1 = 3_482_257_057_872 // vault1 entitlement(1,1).funded, ordinary
	recoveryReveal = 14_760_000_000    // reveal-lag capture, ordinary
	recoveryEmit   = 1_450_000_000_000 // the receiving epoch's own capture
	recoverySource = 3
)

// recoverySourceRows are epoch 3's provider rows: a shared coldkey (clients
// 21 and 22), a provider without a wallet, a head-excluded provider and a
// coldkey that the receiving epoch's rows also pay (coldkey 2).
func recoverySourceRows() []protocol.LedgerUsageRow {
	return []protocol.LedgerUsageRow{
		row(3, 2, 7_000_001, false),
		row(21, 31, 3_000_000, false),
		row(22, 31, 1_000_003, false),
		row(23, 0, 9_000_000, false),
		row(24, 32, 5_000_000, true),
		row(25, 33, 11, false),
	}
}

// recoveryHeads are epoch 3's head candidates: client 24 bound at a
// boundary, client 25 a candidate without an active binding.
func recoveryHeads() heads {
	h := scenarioHeads()
	h[recoverySource] = map[[16]byte]bool{id(24): true, id(25): false}
	return h
}

// The receiving epoch is 12, the pending root's epoch. Its funding holds the
// swept surplus and its own capture.
func recoveryFunding() map[uint64]uint256.Int {
	f := scenarioFunding()
	f[12] = rao(at(f, 11) + recoveryEmit + recoveryEpoch3 + recoveryEpoch1 + recoveryReveal)
	return f
}

// at returns fundedThrough[k] as a uint64.
func at(m map[uint64]uint256.Int, k uint64) uint64 {
	v := m[k]
	return v.Uint64()
}

// fundingOf returns f_k = fundedThrough[k] − fundedThrough[k−1].
func fundingOf(m map[uint64]uint256.Int, k uint64) uint64 {
	return at(m, k) - at(m, k-1)
}

func recoveryExpected() map[uint64][]Recovery {
	return map[uint64][]Recovery{12: {{Epoch: recoverySource, Amount: recoveryEpoch3}}}
}

type recoveryScenario struct {
	*scenario
	sourceRows []protocol.LedgerUsageRow
}

func newRecoveryScenario(t *testing.T) *recoveryScenario {
	t.Helper()
	s := newScenario(t)
	s.funded, s.heads = recoveryFunding(), recoveryHeads()
	return &recoveryScenario{scenario: s, sourceRows: recoverySourceRows()}
}

// buildWith builds the root for epoch 12 over F with recovery components at
// epoch 11 and epoch 12.
func (s *recoveryScenario) buildWith(t *testing.T, at11, at12 []RecoveryInput) (*Ledger, error) {
	t.Helper()
	return Build(context.Background(), BuildInput{
		NoID: testNoID, Epoch: 12, FirstEpoch: testFirst,
		Previous:      &Previous{Root: s.f.Commit, Accounts: s.f.Accounts},
		Epochs:        []EpochInput{{Epoch: 11, Rows: epoch11Rows(), Recovery: at11}, {Epoch: 12, Rows: epoch12Rows(), Recovery: at12}},
		FundedThrough: s.funded,
	})
}

func (s *recoveryScenario) honest(t *testing.T) *Ledger {
	t.Helper()
	l, err := s.buildWith(t, nil, []RecoveryInput{{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3}, Rows: s.sourceRows}})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// withoutSourceRows is the custody of a validator that audited F but holds
// no rows of the source epoch.
func (s *recoveryScenario) withoutSourceRows() *MemoryCustody {
	c := NewMemoryCustody()
	maps.Copy(c.Objects, s.custody.Objects)
	for k, v := range s.custody.Rows {
		if k.Epoch != recoverySource {
			c.Rows[k] = v
		}
	}
	return c
}

func (s *recoveryScenario) input(pending Root, objects Objects) VerifyInput {
	in := s.scenario.input(pending, objects)
	in.Recoveries = recoveryExpected()
	return in
}

func sumDeltas(deltas []protocol.LedgerDelta) *uint256.Int {
	sum := new(uint256.Int)
	for i := range deltas {
		sum.Add(sum, &deltas[i].Delta)
	}
	return sum
}

func mustAllocate(t *testing.T, amount uint64, rows []protocol.LedgerUsageRow) []protocol.LedgerDelta {
	t.Helper()
	deltas, err := protocol.AllocateLedgerEpoch(context.Background(), uint256.NewInt(amount), rows)
	if err != nil {
		t.Fatal(err)
	}
	return deltas
}

// The canonical header text of a recovery component, and its round trip.
func TestHeaderRecoveryRoundTrip(t *testing.T) {
	a, b, c, d := RefOf([]byte("a")), RefOf([]byte("b")), RefOf([]byte("c")), RefOf([]byte("d"))
	h := &Header{
		NoID: 7, Epoch: 12,
		Epochs: []HeaderEpoch{
			{Epoch: 11, Providers: []Ref{}},
			{Epoch: 12, Providers: []Ref{a}, Recovery: []HeaderRecovery{
				{Epoch: 1, Amount: 5, Null: true, Providers: []Ref{}},
				{Epoch: 2, Amount: 6, Providers: []Ref{}},
				{Epoch: 3, Amount: recoveryEpoch3, Providers: []Ref{b, c}},
			}},
		},
		Accounts: []Ref{d},
	}
	want := fmt.Sprintf(`{"schema":"urnetwork-payout-ledger-v2","no_id":7,"epoch":12,"epochs":[{"epoch":11,"providers":[]},{"epoch":12,"providers":["%v"],"recovery":[{"epoch":1,"amount":5,"providers":null},{"epoch":2,"amount":6,"providers":[]},{"epoch":3,"amount":4132715621039,"providers":["%v","%v"]}]}],"accounts":["%v"]}`, a, b, c, d)
	data, err := EncodeHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("header\n%s\nwant\n%s", data, want)
	}
	decoded, err := DecodeHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, h) {
		t.Fatalf("decoded %+v, want %+v", decoded, h)
	}
	// A header without recovery is byte-identical to the format before the
	// component existed (TestHeaderRoundTrip's text).
	plain := testHeader()
	if data, err := EncodeHeader(plain); err != nil || string(data) != testHeaderText() || strings.Contains(string(data), "recovery") {
		t.Fatalf("plain header %s, %v", data, err)
	}
}

// Each change to a canonical recovery component is refused.
func TestHeaderRecoveryRefusals(t *testing.T) {
	b := RefOf([]byte("b")).String()
	canonical := fmt.Sprintf(`{"schema":"urnetwork-payout-ledger-v2","no_id":7,"epoch":12,"epochs":[{"epoch":12,"providers":[],"recovery":[{"epoch":3,"amount":9,"providers":["%v"]}]}],"accounts":[]}`, b)
	if _, err := DecodeHeader([]byte(canonical)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		header string
		want   error
	}{
		{"empty list", strings.Replace(canonical, `"recovery":[{"epoch":3,"amount":9,"providers":["`+b+`"]}]`, `"recovery":[]`, 1), ErrHeaderNotCanonical},
		{"null list", strings.Replace(canonical, `"recovery":[{"epoch":3,"amount":9,"providers":["`+b+`"]}]`, `"recovery":null`, 1), ErrHeaderNotCanonical},
		{"before providers", strings.Replace(canonical, `"providers":[],"recovery":[{"epoch":3,"amount":9,"providers":["`+b+`"]}]`, `"recovery":[{"epoch":3,"amount":9,"providers":["`+b+`"]}],"providers":[]`, 1), ErrHeaderNotCanonical},
		{"key order", strings.Replace(canonical, `{"epoch":3,"amount":9`, `{"amount":9,"epoch":3`, 1), ErrHeaderNotCanonical},
		{"missing amount", strings.Replace(canonical, `"amount":9,`, ``, 1), ErrHeaderNotCanonical},
		{"missing providers", strings.Replace(canonical, `,"providers":["`+b+`"]}]`, `}]`, 1), ErrHeaderNotCanonical},
		{"null entry", strings.Replace(canonical, `[{"epoch":3,"amount":9,"providers":["`+b+`"]}]`, `[null]`, 1), ErrHeaderNotCanonical},
		{"spacing", strings.Replace(canonical, `"amount":9`, `"amount": 9`, 1), ErrHeaderNotCanonical},
		{"unknown field", strings.Replace(canonical, `"amount":9,`, `"amount":9,"rows":1,`, 1), ErrHeaderDecode},
		{"amount as string", strings.Replace(canonical, `"amount":9`, `"amount":"9"`, 1), ErrHeaderDecode},
		{"amount as decimal", strings.Replace(canonical, `"amount":9`, `"amount":9.0`, 1), ErrHeaderDecode},
		{"negative amount", strings.Replace(canonical, `"amount":9`, `"amount":-9`, 1), ErrHeaderDecode},
		{"amount of 2^64", strings.Replace(canonical, `"amount":9`, `"amount":18446744073709551616`, 1), ErrHeaderDecode},
		{"bad reference", strings.Replace(canonical, b, b[7:], 1), ErrRef},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := DecodeHeader([]byte(c.header)); !errors.Is(err, c.want) {
				t.Fatalf("%s: %v, want %v", c.header, err, c.want)
			}
		})
	}
}

// The receiving epoch is the first epoch at or after the pool's first whose
// own assigned surplus reaches the scheduled total; a root carries the
// recovery only when it covers that epoch.
func TestPlanRecovery(t *testing.T) {
	schedule := &RecoverySchedule{NoID: 1, Sources: []Recovery{{Epoch: 3, Amount: 100}}}
	two := &RecoverySchedule{NoID: 1, Sources: []Recovery{{Epoch: 1, Amount: 30}, {Epoch: 3, Amount: 70}}}
	surplus := func(pairs ...uint64) map[uint64]uint256.Int { return funded(pairs...) }
	at := func(epoch uint64, sources ...Recovery) map[uint64][]Recovery {
		return map[uint64][]Recovery{epoch: sources}
	}
	cases := []struct {
		name     string
		schedule *RecoverySchedule
		surplus  map[uint64]uint256.Int
		p, e     uint64
		want     map[uint64][]Recovery
	}{
		{"no schedule", nil, surplus(5, 1000), 4, 5, nil},
		{"no surplus", schedule, nil, 4, 5, nil},
		{"surplus below the total", schedule, surplus(5, 99), 4, 5, nil},
		{"exactly the total", schedule, surplus(5, 100), 4, 5, at(5, Recovery{3, 100})},
		{"above the total", schedule, surplus(5, 7_615), 4, 5, at(5, Recovery{3, 100})},
		{"covered among several epochs", schedule, surplus(6, 100), 4, 7, at(6, Recovery{3, 100})},
		{"first epoch to reach it", schedule, surplus(5, 100, 6, 500), 4, 6, at(5, Recovery{3, 100})},
		{"an earlier small surplus does not count", schedule, surplus(5, 99, 6, 100), 4, 6, at(6, Recovery{3, 100})},
		{"split surplus never reaches it", schedule, surplus(5, 60, 6, 60), 4, 6, nil},
		{"already covered by a finalized root", schedule, surplus(5, 100), 5, 6, nil},
		{"not yet covered", schedule, surplus(6, 100), 4, 5, nil},
		{"a later root after the receiving one", schedule, surplus(5, 100, 7, 100), 6, 7, nil},
		{"before the pool's first epoch", schedule, surplus(3, 100), 3, 5, nil},
		{"two sources together", two, surplus(5, 100), 4, 5, at(5, Recovery{1, 30}, Recovery{3, 70})},
		{"two sources, not enough", two, surplus(5, 99), 4, 5, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := PlanRecovery(c.schedule, c.surplus, 4, c.p, c.e)
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("plan %v, want %v", got, c.want)
			}
		})
	}
}

// The mainnet schedule: pool 1 of the mainnet vault2 recovers exactly epoch
// 3's v1 funding; every other pool and vault has none.
func TestScheduledRecovery(t *testing.T) {
	var mainnet [20]byte
	for _, schedule := range recoverySchedules {
		mainnet = schedule.Vault
	}
	schedule := ScheduledRecovery(mainnet, 1)
	if schedule == nil || schedule.NoID != 1 || !slices.Equal(schedule.Sources, []Recovery{{Epoch: 3, Amount: recoveryEpoch3}}) || schedule.FromBlock != 9_386_659 {
		t.Fatalf("mainnet schedule %+v", schedule)
	}
	if ScheduledRecovery(mainnet, 2) != nil || ScheduledRecovery([20]byte{1}, 1) != nil {
		t.Fatal("a schedule for another pool or vault")
	}
	// A returned schedule is a copy: changing it changes nothing.
	schedule.Sources[0].Amount++
	if ScheduledRecovery(mainnet, 1).Sources[0].Amount != recoveryEpoch3 {
		t.Fatal("the schedule is shared")
	}
	// vault1's terminal RootMissed entitlement of the scheduled amount checks.
	if err := schedule.CheckSource(3, V1EpochRootMissed, uint256.NewInt(recoveryEpoch3+1)); err != nil {
		t.Fatal(err)
	}
	original := ScheduledRecovery(mainnet, 1)
	if err := original.CheckSource(3, V1EpochRootMissed, uint256.NewInt(recoveryEpoch3)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		epoch  uint64
		status uint8
		funded uint64
	}{
		{3, 1, recoveryEpoch3}, // Funded: the finalize poke has not run
		{3, V1EpochRootMissed, recoveryEpoch3 - 1},
		{1, V1EpochRootMissed, recoveryEpoch1}, // epoch 1 is ordinary funding
	} {
		if err := original.CheckSource(c.epoch, c.status, uint256.NewInt(c.funded)); !errors.Is(err, ErrRecoverySource) {
			t.Fatalf("source %d status %d funded %d: %v", c.epoch, c.status, c.funded, err)
		}
	}
}

// The mainnet schedule names the vault2 that mainnet/TIMELINE-UPDATE.md
// publishes.
func TestScheduledRecoveryMainnetVault(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "mainnet", "TIMELINE-UPDATE.md"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile("vault2 `(0x[0-9a-fA-F]{40})`").FindSubmatch(doc)
	if match == nil {
		t.Fatal("the timeline publishes no vault2 address")
	}
	published, err := hex.DecodeString(strings.ToLower(string(match[1][2:])))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(published, mainnetVault2[:]) {
		t.Fatal("the mainnet schedule's vault2 is not the published one")
	}
}

// Epoch 3's share is allocated exactly by epoch 3's rows; epoch 1's share,
// the reveal-lag capture and the epoch's own capture are ordinary funding
// allocated by the receiving epoch's rows (f_k − R).
func TestBuildRecovery(t *testing.T) {
	s := newRecoveryScenario(t)
	l := s.honest(t)
	if total, want := l.Commit.Total, s.funded[12]; total != want {
		t.Fatalf("total %s, want fundedThrough[12] %s", total.Dec(), want.Dec())
	}
	at12 := l.Epochs[1]
	if len(at12.Recovery) != 1 || len(l.Epochs[0].Recovery) != 0 {
		t.Fatalf("recovery components %+v / %+v", l.Epochs[0].Recovery, at12.Recovery)
	}
	component := at12.Recovery[0]
	if component.Epoch != recoverySource || component.Amount != recoveryEpoch3 || !component.Allocated || component.Null {
		t.Fatalf("component %+v", component)
	}
	// The recovery leaves sum exactly to epoch 3's share.
	if sum := sumDeltas(component.Deltas); sum.Uint64() != recoveryEpoch3 {
		t.Fatalf("recovery deltas sum %s, want %d", sum.Dec(), recoveryEpoch3)
	}
	// Each is the §6.3 largest-remainder share of epoch 3's rows.
	if want := mustAllocate(t, recoveryEpoch3, s.sourceRows); !reflect.DeepEqual(component.Deltas, want) {
		t.Fatalf("recovery deltas %v, want %v", component.Deltas, want)
	}
	if !slices.Equal(component.Providers, refsOf(EncodeProviderChunks(s.sourceRows))) {
		t.Fatal("recovery chunks")
	}
	// The no-wallet and head-excluded providers get nothing; the shared
	// coldkey is paid once for both clients.
	for _, d := range component.Deltas {
		if d.Coldkey == key(32) || d.Coldkey == ([32]byte{}) {
			t.Fatalf("ineligible coldkey %x paid", d.Coldkey)
		}
	}
	// The ordinary part, epoch 1's share included, is f_12 − R, allocated by
	// epoch 12's rows.
	f12 := fundingOf(s.funded, 12)
	if at12.Funding.Uint64() != f12 || !at12.Allocated || at12.Amount.Uint64() != f12-recoveryEpoch3 {
		t.Fatalf("epoch 12 funding %s amount %s, want %d and %d", at12.Funding.Dec(), at12.Amount.Dec(), f12, f12-recoveryEpoch3)
	}
	if at12.Amount.Uint64() != recoveryEmit+recoveryEpoch1+recoveryReveal {
		t.Fatal("the ordinary amount is not the capture plus epoch 1's share and the reveal-lag stake")
	}
	if want := mustAllocate(t, f12-recoveryEpoch3, epoch12Rows()); !reflect.DeepEqual(at12.Deltas, want) {
		t.Fatalf("epoch 12 deltas %v, want %v", at12.Deltas, want)
	}
	// Coldkey 2 works in both epochs: its account grows by both deltas.
	previous, next := s.f.Accounts[accountIndex(t, s.f.Accounts, 2)], l.Accounts[accountIndex(t, l.Accounts, 2)]
	var want uint256.Int
	for _, epoch := range l.Epochs {
		for _, deltas := range append([][]protocol.LedgerDelta{epoch.Deltas}, recoveryDeltaLists(epoch)...) {
			for _, d := range deltas {
				if d.Coldkey == key(2) {
					want.Add(&want, &d.Delta)
				}
			}
		}
	}
	if increase := new(uint256.Int).Sub(&next.Cumulative, &previous.Cumulative); !increase.Eq(&want) || want.IsZero() {
		t.Fatalf("coldkey 2 increased %s, want %s", increase.Dec(), want.Dec())
	}
	// New recovery coldkeys get accounts, appended in ascending order.
	for _, n := range []int{31, 33} {
		accountIndex(t, l.Accounts, n)
	}
	// The header lists the component after epoch 12's providers.
	h, err := DecodeHeader(l.Header)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Epochs[1].Recovery) != 1 || h.Epochs[1].Recovery[0].Amount != recoveryEpoch3 || !slices.Equal(h.Epochs[1].Recovery[0].Providers, component.Providers) || len(h.Epochs[0].Recovery) != 0 {
		t.Fatalf("header recovery %+v", h.Epochs)
	}
	for _, ref := range component.Providers {
		if _, ok := l.Objects[ref]; !ok {
			t.Fatal("a recovery chunk is not among the objects")
		}
	}
	// The same input builds the same bytes.
	again := s.honest(t)
	if !slices.Equal(again.Header, l.Header) || again.Commit != l.Commit {
		t.Fatal("not deterministic")
	}
}

func refsOf(chunks [][]byte) []Ref {
	refs := make([]Ref, len(chunks))
	for i, chunk := range chunks {
		refs[i] = RefOf(chunk)
	}
	return refs
}

func recoveryDeltaLists(epoch EpochAllocation) [][]protocol.LedgerDelta {
	var lists [][]protocol.LedgerDelta
	for _, component := range epoch.Recovery {
		lists = append(lists, component.Deltas)
	}
	return lists
}

// The zero-row edge case: when epoch 3 has no rows, no eligible usage, or is
// declared unrecoverable, its share joins the receiving epoch's ordinary
// funding and the receiving epoch's rows allocate all of f_k.
func TestBuildRecoveryZeroRows(t *testing.T) {
	s := newRecoveryScenario(t)
	f12 := fundingOf(s.funded, 12)
	ineligible := []protocol.LedgerUsageRow{row(23, 0, 9_000_000, false), row(24, 32, 5_000_000, true)}
	cases := []struct {
		name      string
		component RecoveryInput
		providers []Ref
	}{
		{"no rows", RecoveryInput{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3}}, []Ref{}},
		{"no eligible usage", RecoveryInput{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3}, Rows: ineligible}, refsOf(EncodeProviderChunks(ineligible))},
		{"unrecoverable", RecoveryInput{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3}, Null: true}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l, err := s.buildWith(t, nil, []RecoveryInput{c.component})
			if err != nil {
				t.Fatal(err)
			}
			at12 := l.Epochs[1]
			component := at12.Recovery[0]
			if component.Allocated || len(component.Deltas) != 0 || component.Null != c.component.Null || !slices.Equal(component.Providers, c.providers) {
				t.Fatalf("component %+v", component)
			}
			if at12.Amount.Uint64() != f12 {
				t.Fatalf("ordinary amount %s, want all of f_12 %d", at12.Amount.Dec(), f12)
			}
			if want := mustAllocate(t, f12, epoch12Rows()); !reflect.DeepEqual(at12.Deltas, want) {
				t.Fatalf("epoch 12 deltas %v, want %v", at12.Deltas, want)
			}
			if l.Commit.Total != s.funded[12] {
				t.Fatal("total")
			}
			h, err := DecodeHeader(l.Header)
			if err != nil {
				t.Fatal(err)
			}
			listed := h.Epochs[1].Recovery
			if len(listed) != 1 || listed[0].Null != c.component.Null || (!c.component.Null && !slices.Equal(listed[0].Providers, c.providers)) {
				t.Fatalf("header recovery %+v", listed)
			}
			// The validator accepts the same root (VA-2), and records the
			// null declaration of an unrecoverable source.
			custody := NewMemoryCustody()
			maps.Copy(custody.Rows, s.custody.Rows)
			maps.Copy(custody.Objects, s.custody.Objects)
			in := (&recoveryScenario{scenario: s.scenario}).input(l.Commit, l.Objects)
			in.Custody = custody
			expectPass(t, mustVerify(t, in))
			if c.component.Null != (len(custody.Nulls[PoolEpoch{testNoID, recoverySource}]) == 1) {
				t.Fatalf("null declarations %v", custody.Nulls)
			}
		})
	}
}

// A recovery component sits in its epoch's walk: an epoch whose ordinary rows
// carry still allocates its recovery, and its ordinary part carries on.
func TestBuildRecoveryCarry(t *testing.T) {
	s := newRecoveryScenario(t)
	// Recovery at epoch 11, whose rows have no eligible usage.
	s.funded[11] = rao(at(s.funded, 10) + 600_000 + recoveryEpoch3)
	s.funded[12] = rao(at(s.funded, 11) + 900_000)
	l, err := Build(context.Background(), BuildInput{
		NoID: testNoID, Epoch: 12, FirstEpoch: testFirst,
		Previous: &Previous{Root: s.f.Commit, Accounts: s.f.Accounts},
		Epochs: []EpochInput{
			{Epoch: 11, Rows: []protocol.LedgerUsageRow{row(10, 0, 50, false)}, Recovery: []RecoveryInput{{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3}, Rows: s.sourceRows}}},
			{Epoch: 12, Rows: epoch12Rows()},
		},
		FundedThrough: s.funded,
	})
	if err != nil {
		t.Fatal(err)
	}
	at11, at12 := l.Epochs[0], l.Epochs[1]
	if at11.Allocated || !at11.Recovery[0].Allocated || sumDeltas(at11.Recovery[0].Deltas).Uint64() != recoveryEpoch3 {
		t.Fatalf("epoch 11 %+v", at11)
	}
	// Epoch 12 allocates its own funding plus epoch 11's ordinary carry.
	if at12.Amount.Uint64() != 900_000+600_000 {
		t.Fatalf("epoch 12 amount %s", at12.Amount.Dec())
	}
	if l.Commit.Total != s.funded[12] {
		t.Fatal("total")
	}
	expectPass(t, mustVerify(t, func() VerifyInput {
		in := s.scenario.input(l.Commit, l.Objects)
		in.Recoveries = map[uint64][]Recovery{11: {{Epoch: recoverySource, Amount: recoveryEpoch3}}}
		return in
	}()))
	// The root's own epoch must still allocate its ordinary rows.
	s.funded[12] = rao(at(s.funded, 11) + 900_000 + recoveryEpoch3)
	_, err = Build(context.Background(), BuildInput{
		NoID: testNoID, Epoch: 12, FirstEpoch: testFirst,
		Previous: &Previous{Root: s.f.Commit, Accounts: s.f.Accounts},
		Epochs: []EpochInput{
			{Epoch: 11, Rows: epoch11Rows()},
			{Epoch: 12, Rows: []protocol.LedgerUsageRow{row(10, 0, 50, false)}, Recovery: []RecoveryInput{{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3}, Rows: s.sourceRows}}},
		},
		FundedThrough: s.funded,
	})
	if !errors.Is(err, ErrEpochNotAllocated) {
		t.Fatalf("a root whose epoch only recovers: %v", err)
	}
}

func TestBuildRecoveryRefusals(t *testing.T) {
	s := newRecoveryScenario(t)
	component := func(epoch, amount uint64) RecoveryInput {
		return RecoveryInput{Recovery: Recovery{Epoch: epoch, Amount: amount}, Rows: s.sourceRows}
	}
	f12 := fundingOf(s.funded, 12)
	cases := []struct {
		name string
		at12 []RecoveryInput
		want error
	}{
		{"more than the epoch's funding", []RecoveryInput{component(3, f12+1)}, ErrRecoveryFunding},
		{"together more than the funding", []RecoveryInput{component(2, f12/2+1), component(3, f12/2+1)}, ErrRecoveryFunding},
		{"zero amount", []RecoveryInput{component(3, 0)}, ErrRecoveryInput},
		{"source at the epoch", []RecoveryInput{component(12, 1)}, ErrRecoveryInput},
		{"source after the epoch", []RecoveryInput{component(13, 1)}, ErrRecoveryInput},
		{"unsorted sources", []RecoveryInput{component(3, 1), component(2, 1)}, ErrRecoveryInput},
		{"repeated source", []RecoveryInput{component(3, 1), component(3, 1)}, ErrRecoveryInput},
		{"unrecoverable with rows", []RecoveryInput{{Recovery: Recovery{Epoch: 3, Amount: 1}, Null: true, Rows: s.sourceRows}}, ErrEpochInput},
		{"rows unlike their fixed chunks", []RecoveryInput{{Recovery: Recovery{Epoch: 3, Amount: 1}, Rows: s.sourceRows, Chunks: []Ref{RefOf(nil)}}}, ErrFixedInputs},
		{"unsorted rows", []RecoveryInput{{Recovery: Recovery{Epoch: 3, Amount: 1}, Rows: []protocol.LedgerUsageRow{row(2, 1, 1, false), row(1, 1, 1, false)}}}, ErrRowsUnsorted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := s.buildWith(t, nil, c.at12); !errors.Is(err, c.want) {
				t.Fatalf("%v, want %v", err, c.want)
			}
		})
	}
	// Rows equal to their fixed chunks build.
	fixed := component(3, recoveryEpoch3)
	fixed.Chunks = refsOf(EncodeProviderChunks(s.sourceRows))
	if _, err := s.buildWith(t, nil, []RecoveryInput{fixed}); err != nil {
		t.Fatal(err)
	}
}

// The validator accepts the honest root with its recovery component, keeps
// epoch 3's rows as fixed inputs, and a later root must list them again.
func TestVerifyRecovery(t *testing.T) {
	s := newRecoveryScenario(t)
	l := s.honest(t)
	expectPass(t, mustVerify(t, s.input(l.Commit, l.Objects)))
	chunks, ok, _ := s.custody.ValidRows(context.Background(), testNoID, recoverySource)
	if !ok || !slices.Equal(chunks, l.Epochs[1].Recovery[0].Providers) {
		t.Fatalf("custody holds %v %v for the source epoch", chunks, ok)
	}
	// A fresh validator with no custody accepts it too.
	objects := maps.Clone(l.Objects)
	maps.Copy(objects, s.f.Objects)
	fresh := s.input(l.Commit, objects)
	fresh.Custody = NewMemoryCustody()
	expectPass(t, mustVerify(t, fresh))
}

// Every way an operator can tamper with the recovery component is refused,
// each by its own check. A recovery dispute is found once the published
// accounts reproduce the root, so it refuses approval without demands.
func TestVerifyRecoveryMutations(t *testing.T) {
	s := newRecoveryScenario(t)
	honest := s.honest(t)
	// The validator first audits the honest root, which records epoch 3's
	// valid rows; each mutation is then a replacement root over F.
	expectPass(t, mustVerify(t, s.input(honest.Commit, honest.Objects)))
	honestComponent := []RecoveryInput{{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3}, Rows: s.sourceRows}}

	t.Run("no recovery expected", func(t *testing.T) {
		in := s.scenario.input(honest.Commit, honest.Objects)
		expectFailure(t, mustVerify(t, in), CheckRecovery, ActionRefuse, ErrRecoveryUnexpected)
	})
	t.Run("recovery omitted", func(t *testing.T) {
		l, err := s.buildWith(t, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		expectFailure(t, mustVerify(t, s.input(l.Commit, l.Objects)), CheckRecovery, ActionRefuse, ErrRecoveryMissing)
	})
	t.Run("amount inflated", func(t *testing.T) {
		l, err := s.buildWith(t, nil, []RecoveryInput{{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3 + 1}, Rows: s.sourceRows}})
		if err != nil {
			t.Fatal(err)
		}
		expectFailure(t, mustVerify(t, s.input(l.Commit, l.Objects)), CheckRecovery, ActionRefuse, ErrRecoveryAmount)
	})
	t.Run("epoch 1 recovered by its rows", func(t *testing.T) {
		l, err := s.buildWith(t, nil, append([]RecoveryInput{{Recovery: Recovery{Epoch: 1, Amount: recoveryEpoch1}, Rows: epoch11Rows()}}, honestComponent...))
		if err != nil {
			t.Fatal(err)
		}
		expectFailure(t, mustVerify(t, s.input(l.Commit, l.Objects)), CheckRecovery, ActionRefuse, ErrRecoveryUnexpected)
	})
	t.Run("placed at another epoch", func(t *testing.T) {
		pub := publish(t, honest)
		pub.header.Epochs[0].Recovery, pub.header.Epochs[1].Recovery = pub.header.Epochs[1].Recovery, nil
		expectFailure(t, mustVerify(t, s.input(pub.seal(t), pub.objects)), CheckRecovery, ActionRefuse, ErrRecoveryUnexpected)
	})
	t.Run("recovery leaves moved", func(t *testing.T) {
		// The operator publishes accounts that shift rao between two recovery
		// coldkeys and commits their tree.
		pub := publish(t, honest)
		accounts := slices.Clone(honest.Accounts)
		from, to := accountIndex(t, accounts, 31), accountIndex(t, accounts, 33)
		accounts[from].Cumulative.SubUint64(&accounts[from].Cumulative, 1_000)
		accounts[to].Cumulative.AddUint64(&accounts[to].Cumulative, 1_000)
		pub.setAccounts(t, accounts)
		res := mustVerify(t, s.input(pub.seal(t), pub.objects))
		expectFailure(t, res, CheckAllocation, ActionRefuse, ErrAccountDelta)
		if !res.Reproduced {
			t.Fatal("the mutated accounts should reproduce the committed root")
		}
	})
	t.Run("recovery leaves inflated", func(t *testing.T) {
		pub := publish(t, honest)
		accounts := slices.Clone(honest.Accounts)
		i := accountIndex(t, accounts, 31)
		accounts[i].Cumulative.AddUint64(&accounts[i].Cumulative, 1)
		pub.setAccounts(t, accounts)
		expectFailure(t, mustVerify(t, s.input(pub.seal(t), pub.objects)), CheckAllocation, ActionRefuse, ErrAccountDelta)
	})
	t.Run("rows changed against held rows", func(t *testing.T) {
		rows := slices.Clone(s.sourceRows)
		rows[1].UsageBytes++
		l, err := s.buildWith(t, nil, []RecoveryInput{{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3}, Rows: rows}})
		if err != nil {
			t.Fatal(err)
		}
		expectFailure(t, mustVerify(t, s.input(l.Commit, l.Objects)), CheckFixedInputs, ActionRefuse, ErrFixedInputs)
	})
	t.Run("null against held rows", func(t *testing.T) {
		l, err := s.buildWith(t, nil, []RecoveryInput{{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3}, Null: true}})
		if err != nil {
			t.Fatal(err)
		}
		expectFailure(t, mustVerify(t, s.input(l.Commit, l.Objects)), CheckFixedInputs, ActionRefuse, ErrNullAgainstValidRows)
	})
	// Without held rows, the rows themselves are checked.
	fresh := func(l *Ledger) VerifyInput {
		in := s.input(l.Commit, l.Objects)
		in.Custody = s.withoutSourceRows()
		return in
	}
	t.Run("head flag cleared", func(t *testing.T) {
		rows := slices.Clone(s.sourceRows)
		rows[4].HeadExcluded = false
		l, err := s.buildWith(t, nil, []RecoveryInput{{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3}, Rows: rows}})
		if err != nil {
			t.Fatal(err)
		}
		expectFailure(t, mustVerify(t, fresh(l)), CheckHeadExclusion, ActionRefuse, ErrHeadFlagCleared)
	})
	t.Run("head flag set", func(t *testing.T) {
		rows := slices.Clone(s.sourceRows)
		rows[5].HeadExcluded = true
		l, err := s.buildWith(t, nil, []RecoveryInput{{Recovery: Recovery{Epoch: recoverySource, Amount: recoveryEpoch3}, Rows: rows}})
		if err != nil {
			t.Fatal(err)
		}
		expectFailure(t, mustVerify(t, fresh(l)), CheckHeadExclusion, ActionRefuse, ErrHeadFlagSet)
	})
	t.Run("rows out of order", func(t *testing.T) {
		pub := publish(t, honest)
		rows := slices.Clone(s.sourceRows)
		rows[0], rows[1] = rows[1], rows[0]
		pub.header.Epochs[1].Recovery[0].Providers = pub.put(EncodeProviderChunks(rows))
		expectFailure(t, mustVerify(t, fresh(&Ledger{Commit: pub.seal(t), Objects: pub.objects})), CheckRows, ActionRefuse, ErrRowsUnsorted)
	})
	t.Run("chunk missing", func(t *testing.T) {
		pub := publish(t, honest)
		chunk := pub.header.Epochs[1].Recovery[0].Providers[0]
		delete(pub.objects, chunk)
		in := fresh(&Ledger{Commit: pub.seal(t), Objects: pub.objects})
		delete(in.Custody.(*MemoryCustody).Objects, chunk)
		res := mustVerify(t, in)
		expectFailure(t, res, CheckFetch, ActionDemand, ErrObjectUnavailable)
		if !slices.Equal(res.Missing, []Ref{chunk}) {
			t.Fatalf("missing %v", res.Missing)
		}
	})
	t.Run("recovery rows that do not allocate the published accounts", func(t *testing.T) {
		// Rows that differ from those the accounts were built with, with no
		// held rows to compare against: the allocation check refuses.
		pub := publish(t, honest)
		rows := slices.Clone(s.sourceRows)
		rows[1].UsageBytes += 1_000_000
		pub.header.Epochs[1].Recovery[0].Providers = pub.put(EncodeProviderChunks(rows))
		expectFailure(t, mustVerify(t, fresh(&Ledger{Commit: pub.seal(t), Objects: pub.objects})), CheckAllocation, ActionRefuse, ErrAccountDelta)
	})
}

// A covered epoch whose expected recovery outruns its funding is the
// caller's input error, never a finding against the root.
func TestVerifyRecoveryFundingInput(t *testing.T) {
	s := newRecoveryScenario(t)
	l := s.honest(t)
	in := s.input(l.Commit, l.Objects)
	in.FundedThrough = maps.Clone(s.funded)
	in.FundedThrough[12] = rao(at(s.funded, 11) + recoveryEpoch3 - 1)
	if _, err := Verify(context.Background(), in); !errors.Is(err, ErrFunding) {
		t.Fatalf("%v, want ErrFunding", err)
	}
}

// Check 10's name.
func TestRecoveryCheckName(t *testing.T) {
	if CheckRecovery.String() != "recovery" || CheckRecovery <= CheckFetch {
		t.Fatalf("CheckRecovery %d %q", CheckRecovery, CheckRecovery)
	}
	_ = merkle.LedgerAccount{}
}
