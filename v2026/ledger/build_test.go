package ledger

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
)

// firstRoot builds pool 7's first root, for epoch 10.
func firstRoot(t *testing.T, rows []protocol.LedgerUsageRow, fundedThrough uint64) *Ledger {
	t.Helper()
	l, err := Build(context.Background(), BuildInput{NoID: testNoID, Epoch: 10, FirstEpoch: testFirst, Epochs: []EpochInput{{Epoch: 10, Rows: rows}}, FundedThrough: funded(10, fundedThrough)})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// buildOver builds the root for the last of epochs over the finalized root f.
func buildOver(f *Ledger, epochs []EpochInput, fundedThrough map[uint64]uint256.Int) (*Ledger, error) {
	return Build(context.Background(), BuildInput{
		NoID: testNoID, Epoch: epochs[len(epochs)-1].Epoch, FirstEpoch: testFirst,
		Previous: &Previous{Root: f.Commit, Accounts: f.Accounts}, Epochs: epochs, FundedThrough: fundedThrough,
	})
}

func cumulative(t *testing.T, l *Ledger, n int) uint64 {
	t.Helper()
	account := l.Accounts[accountIndex(t, l.Accounts, n)]
	return account.Cumulative.Uint64()
}

func hasAccount(l *Ledger, n int) bool {
	return slices.ContainsFunc(l.Accounts, func(a merkle.LedgerAccount) bool { return a.Coldkey == key(n) })
}

// GL-2: the builder's output is byte-identical across runs.
func TestBuildDeterministic(t *testing.T) {
	s := newScenario(t)
	for run := 0; run < 5; run++ {
		again := s.build(t, epoch11Rows(), epoch12Rows())
		if !bytes.Equal(again.Header, s.p.Header) || again.Commit != s.p.Commit {
			t.Fatalf("run %d: header or commit differs", run)
		}
		if !maps.EqualFunc(again.Objects, s.p.Objects, bytes.Equal) {
			t.Fatalf("run %d: objects differ", run)
		}
		if !reflect.DeepEqual(again.Accounts, s.p.Accounts) || !reflect.DeepEqual(again.Epochs, s.p.Epochs) {
			t.Fatalf("run %d: accounts or allocations differ", run)
		}
		if root, _ := again.Tree.Root(); root != s.p.Commit.Root {
			t.Fatalf("run %d: tree differs", run)
		}
	}
}

// GL-3: a pool's first root, a continuity root over it, its new accounts
// appended in ascending coldkey order, and the objects the header lists.
func TestBuildFirstAndContinuityRoots(t *testing.T) {
	s := newScenario(t)
	f, p := s.f, s.p
	// First root: one account per paid coldkey, in ascending order; the
	// provider without a wallet and the head-excluded one are not paid.
	if f.Commit.Epoch != 10 || f.Commit.LeafCount != 3 || f.Commit.Total != rao(1_000_003) {
		t.Fatalf("first root %+v", f.Commit)
	}
	for i, want := range []struct {
		coldkey int
		value   uint64
	}{{1, 665_559}, {2, 332_780}, {4, 1_664}} {
		if f.Accounts[i].Coldkey != key(want.coldkey) || f.Accounts[i].Cumulative != rao(want.value) {
			t.Fatalf("first root account %d: %x %s", i, f.Accounts[i].Coldkey[:8], f.Accounts[i].Cumulative.Dec())
		}
	}
	// Continuity: previous positions keep their coldkeys and grow by the
	// epochs' deltas; coldkeys 3, 5, 7 and 8 are new, in ascending order.
	if p.Commit.Epoch != 12 || p.Commit.LeafCount != 7 || p.Commit.Total != rao(2_500_000) {
		t.Fatalf("continuity root %+v", p.Commit)
	}
	for i, want := range []struct {
		coldkey int
		value   uint64
	}{{1, 832_225}, {2, 941_924}, {4, 1_664}, {3, 66_666}, {5, 208_848}, {7, 442_478}, {8, 6_195}} {
		if p.Accounts[i].Coldkey != key(want.coldkey) || p.Accounts[i].Cumulative != rao(want.value) {
			t.Fatalf("continuity account %d: %x %s", i, p.Accounts[i].Coldkey[:8], p.Accounts[i].Cumulative.Dec())
		}
	}
	root, total := p.Tree.Root()
	if root != p.Commit.Root || total != p.Commit.Total || p.Tree.LeafCount() != p.Commit.LeafCount {
		t.Fatal("tree is not the commit")
	}
	// Per epoch: f_k, the amount and its deltas, which sum to the amount.
	for i, want := range []struct {
		epoch            uint64
		funding, amount  uint64
		coldkeys, deltas []uint64
	}{
		{11, 599_997, 599_997, []uint64{1, 2, 3, 5}, []uint64{166_666, 166_666, 66_666, 199_999}},
		{12, 900_000, 900_000, []uint64{2, 5, 7, 8}, []uint64{442_478, 8_849, 442_478, 6_195}},
	} {
		got := p.Epochs[i]
		if got.Epoch != want.epoch || got.Null || !got.Allocated || got.Funding != rao(want.funding) || got.Amount != rao(want.amount) || len(got.Deltas) != len(want.deltas) {
			t.Fatalf("epoch %d allocation %+v", want.epoch, got)
		}
		for j := range want.deltas {
			if got.Deltas[j].Coldkey != key(int(want.coldkeys[j])) || got.Deltas[j].Delta != rao(want.deltas[j]) {
				t.Fatalf("epoch %d delta %d: %x %s", want.epoch, j, got.Deltas[j].Coldkey[:8], got.Deltas[j].Delta.Dec())
			}
		}
	}
	// The header lists each epoch's chunks and the account chunks, and every
	// listed object is present.
	h, err := DecodeHeader(p.Header)
	if err != nil {
		t.Fatal(err)
	}
	if RefOf(p.Header) != Ref(p.Commit.LedgerHash) || !bytes.Equal(p.Objects[p.Commit.LedgerHash], p.Header) {
		t.Fatal("ledger hash does not name the header")
	}
	if h.NoID != testNoID || h.Epoch != 12 || len(h.Epochs) != 2 || h.Epochs[0].Epoch != 11 || h.Epochs[1].Epoch != 12 {
		t.Fatalf("header %+v", h)
	}
	listed := 1
	for i, epoch := range h.Epochs {
		if !slices.Equal(epoch.Providers, p.Epochs[i].Providers) {
			t.Fatalf("epoch %d chunks differ from the allocation", epoch.Epoch)
		}
		listed += len(epoch.Providers)
	}
	listed += len(h.Accounts)
	if len(p.Objects) != listed {
		t.Fatalf("%d objects for %d listed", len(p.Objects), listed)
	}
	var rows [][]byte
	for _, ref := range h.Epochs[0].Providers {
		rows = append(rows, p.Objects[ref])
	}
	if decoded, err := DecodeProviderChunks(rows); err != nil || !reflect.DeepEqual(decoded, epoch11Rows()) {
		t.Fatalf("epoch 11 chunks decode to %+v, %v", decoded, err)
	}
	var chunks [][]byte
	for _, ref := range h.Accounts {
		chunks = append(chunks, p.Objects[ref])
	}
	if decoded, err := DecodeAccountChunks(chunks); err != nil || !reflect.DeepEqual(decoded, p.Accounts) {
		t.Fatalf("account chunks decode to %+v, %v", decoded, err)
	}
}

// GL-3: no root is built, so nothing is committed, when the total does not
// grow: here epoch 11 was deferred and funded nothing.
func TestBuildTotalNotIncreased(t *testing.T) {
	f := firstRoot(t, epoch10Rows(), 1_000_003)
	_, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: epoch11Rows()}}, funded(11, 1_000_003))
	if !errors.Is(err, ErrTotalNotIncreased) {
		t.Fatalf("%v, want ErrTotalNotIncreased", err)
	}
}

// GL-4: the Alice case. Alice works only in epoch 11, whose root was
// dropped, and Bob only in epoch 12. The next root covers both, and each is
// paid their own epoch's funding.
func TestBuildAliceCase(t *testing.T) {
	f := firstRoot(t, epoch10Rows(), 1_000_003)
	alice, bob := 21, 22
	p, err := buildOver(f, []EpochInput{
		{Epoch: 11, Rows: []protocol.LedgerUsageRow{row(21, alice, 5, false)}},
		{Epoch: 12, Rows: []protocol.LedgerUsageRow{row(22, bob, 70, false)}},
	}, funded(11, 1_000_603, 12, 1_001_503))
	if err != nil {
		t.Fatal(err)
	}
	if cumulative(t, p, alice) != 600 || cumulative(t, p, bob) != 900 {
		t.Fatalf("alice %d, bob %d; want 600 and 900", cumulative(t, p, alice), cumulative(t, p, bob))
	}
	for i := range f.Accounts {
		if p.Accounts[i] != f.Accounts[i] {
			t.Fatalf("account %d changed", i)
		}
	}
}

// GL-5: the carry walk. An epoch without rows ("providers": []), one whose
// rows have no eligible usage (ΣW = 0) and an unrecoverable one
// ("providers": null) each carry their funding to the next epoch that
// allocates. A deferred close funds 0 for its epoch, whose rows allocate
// only the carry they receive; its emission funds the next close.
func TestBuildCarryWalk(t *testing.T) {
	f := firstRoot(t, epoch10Rows(), 1_000_003)
	ineligible := []protocol.LedgerUsageRow{row(1, 0, 10, false), row(5, 3, 10, true)}
	for _, c := range []struct {
		name  string
		epoch EpochInput
		json  string
	}{
		{"no rows", EpochInput{Epoch: 11, Rows: []protocol.LedgerUsageRow{}}, `{"epoch":11,"providers":[]}`},
		{"no eligible usage", EpochInput{Epoch: 11, Rows: ineligible}, `{"epoch":11,"providers":["` + RefOf(EncodeProviderChunks(ineligible)[0]).String() + `"]}`},
		{"unrecoverable", EpochInput{Epoch: 11, Null: true}, `{"epoch":11,"providers":null}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, err := buildOver(f, []EpochInput{c.epoch, {Epoch: 12, Rows: []protocol.LedgerUsageRow{row(22, 22, 1, false)}}}, funded(11, 1_000_603, 12, 1_001_503))
			if err != nil {
				t.Fatal(err)
			}
			if cumulative(t, p, 22) != 1_500 || p.Epochs[0].Allocated || !p.Epochs[0].Amount.IsZero() || p.Epochs[1].Amount != rao(1_500) {
				t.Fatalf("epoch 12 earner has %d; allocations %+v", cumulative(t, p, 22), p.Epochs)
			}
			if !bytes.Contains(p.Header, []byte(c.json)) {
				t.Fatalf("header %s lacks %s", p.Header, c.json)
			}
		})
	}
	t.Run("deferred close", func(t *testing.T) {
		// Epoch 11 has no rows and carries 600. Epoch 12's close was missed,
		// so it funds 0 and its earner gets the carry. Epoch 13's close
		// captured epoch 12's emission too.
		p, err := buildOver(f, []EpochInput{
			{Epoch: 11},
			{Epoch: 12, Rows: []protocol.LedgerUsageRow{row(22, 22, 1, false)}},
			{Epoch: 13, Rows: []protocol.LedgerUsageRow{row(23, 23, 1, false)}},
		}, funded(11, 1_000_603, 12, 1_000_603, 13, 1_002_403))
		if err != nil {
			t.Fatal(err)
		}
		if cumulative(t, p, 22) != 600 || cumulative(t, p, 23) != 1_800 || !p.Epochs[1].Funding.IsZero() || !p.Epochs[1].Allocated {
			t.Fatalf("deferred epoch earner %d, next %d; allocations %+v", cumulative(t, p, 22), cumulative(t, p, 23), p.Epochs)
		}
		// A deferred epoch with nothing to carry allocates zero deltas and
		// creates no account.
		p, err = buildOver(f, []EpochInput{
			{Epoch: 11, Rows: []protocol.LedgerUsageRow{row(22, 22, 1, false)}},
			{Epoch: 12, Rows: []protocol.LedgerUsageRow{row(23, 23, 1, false)}},
		}, funded(11, 1_000_003, 12, 1_001_003))
		if err != nil {
			t.Fatal(err)
		}
		if hasAccount(p, 22) || cumulative(t, p, 23) != 1_000 || len(p.Epochs[0].Deltas) != 1 || !p.Epochs[0].Deltas[0].Delta.IsZero() {
			t.Fatalf("zero allocation: accounts %d, allocations %+v", len(p.Accounts), p.Epochs)
		}
	})
	t.Run("root epoch does not allocate", func(t *testing.T) {
		for _, last := range []EpochInput{{Epoch: 12}, {Epoch: 12, Rows: ineligible}, {Epoch: 12, Null: true}} {
			_, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: epoch11Rows()}, last}, funded(11, 1_000_603, 12, 1_001_503))
			if !errors.Is(err, ErrEpochNotAllocated) {
				t.Fatalf("%+v: %v, want ErrEpochNotAllocated", last, err)
			}
		}
	})
}

// GL-6: fixed inputs. A later root covering an epoch lists the chunks the
// epoch was first built with, given its rows from those chunks; rows that
// encode differently are refused.
func TestBuildFixedInputs(t *testing.T) {
	s := newScenario(t)
	// P1 covered epoch 11 alone and was not finalized; P2 covers 11 and 12.
	p1, err := buildOver(s.f, []EpochInput{{Epoch: 11, Rows: epoch11Rows()}}, s.funded)
	if err != nil {
		t.Fatal(err)
	}
	fixed := p1.Epochs[0].Providers
	var chunks [][]byte
	for _, ref := range fixed {
		chunks = append(chunks, p1.Objects[ref])
	}
	rows, err := DecodeProviderChunks(chunks)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := buildOver(s.f, []EpochInput{{Epoch: 11, Rows: rows, Chunks: fixed}, {Epoch: 12, Rows: epoch12Rows()}}, s.funded)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p2.Epochs[0].Providers, fixed) || !bytes.Equal(p2.Header, s.p.Header) {
		t.Fatal("the later root does not list the same chunks")
	}
	changed := append(slices.Clone(rows), row(14, 9, 1, false))
	_, err = buildOver(s.f, []EpochInput{{Epoch: 11, Rows: changed, Chunks: fixed}, {Epoch: 12, Rows: epoch12Rows()}}, s.funded)
	if !errors.Is(err, ErrFixedInputs) {
		t.Fatalf("%v, want ErrFixedInputs", err)
	}
	// An epoch first built without rows is fixed to no chunks.
	_, err = buildOver(s.f, []EpochInput{{Epoch: 11, Rows: rows, Chunks: []Ref{}}, {Epoch: 12, Rows: epoch12Rows()}}, s.funded)
	if !errors.Is(err, ErrFixedInputs) {
		t.Fatalf("%v, want ErrFixedInputs", err)
	}
}

// GL-8: a wallet change, a head promotion, a dormant account and a shared
// coldkey behave as §6.1 says.
func TestBuildAccountLifecycle(t *testing.T) {
	f := firstRoot(t, []protocol.LedgerUsageRow{row(1, 1, 100, false), row(2, 2, 100, false), row(3, 3, 100, false)}, 3_000)
	t.Run("wallet change", func(t *testing.T) {
		// Client 1 now pays coldkey 30: the new coldkey's account takes its
		// deltas, and the old account keeps its balance.
		p, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: []protocol.LedgerUsageRow{row(1, 30, 100, false), row(2, 2, 100, false)}}}, funded(11, 5_000))
		if err != nil {
			t.Fatal(err)
		}
		if cumulative(t, p, 1) != 1_000 || cumulative(t, p, 30) != 1_000 || cumulative(t, p, 2) != 2_000 || p.Accounts[0].Coldkey != key(1) {
			t.Fatalf("accounts %+v", p.Accounts)
		}
	})
	t.Run("head promotion", func(t *testing.T) {
		// Client 1 joined a head fleet: it earns nothing, and its account is
		// unchanged.
		p, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: []protocol.LedgerUsageRow{row(1, 1, 100, true), row(2, 2, 100, false)}}}, funded(11, 5_000))
		if err != nil {
			t.Fatal(err)
		}
		if cumulative(t, p, 1) != 1_000 || cumulative(t, p, 2) != 3_000 {
			t.Fatalf("accounts %+v", p.Accounts)
		}
	})
	t.Run("dormant account", func(t *testing.T) {
		// Client 3 did no work: its account keeps its position and value.
		p, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: []protocol.LedgerUsageRow{row(1, 1, 100, false)}}}, funded(11, 4_000))
		if err != nil {
			t.Fatal(err)
		}
		if p.Accounts[2] != f.Accounts[2] || len(p.Accounts) != 3 || cumulative(t, p, 1) != 2_000 {
			t.Fatalf("accounts %+v", p.Accounts)
		}
	})
	t.Run("shared coldkey", func(t *testing.T) {
		// Clients 1 and 4 pay coldkey 1: one account, paid as one provider
		// with their summed usage would be.
		shared, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: []protocol.LedgerUsageRow{row(1, 1, 30, false), row(2, 2, 100, false), row(4, 1, 70, false)}}}, funded(11, 4_001))
		if err != nil {
			t.Fatal(err)
		}
		single, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: []protocol.LedgerUsageRow{row(1, 1, 100, false), row(2, 2, 100, false)}}}, funded(11, 4_001))
		if err != nil {
			t.Fatal(err)
		}
		if len(shared.Accounts) != 3 || !reflect.DeepEqual(shared.Accounts, single.Accounts) || cumulative(t, shared, 1) != 1_501 {
			t.Fatalf("accounts %+v, single provider %+v", shared.Accounts, single.Accounts)
		}
	})
}

// cancelAfter is a context whose Err reports cancellation from its n+1th call.
type cancelAfter struct {
	context.Context
	n int
}

func (c *cancelAfter) Err() error {
	if c.n <= 0 {
		return context.Canceled
	}
	c.n--
	return nil
}

// GL-3, GL-5, GL-6: every builder refusal, each triggered by one bad
// input.
func TestBuildRefusals(t *testing.T) {
	f := firstRoot(t, epoch10Rows(), 1_000_003)
	good := func() BuildInput {
		return BuildInput{
			NoID: testNoID, Epoch: 12, FirstEpoch: testFirst,
			Previous:      &Previous{Root: f.Commit, Accounts: slices.Clone(f.Accounts)},
			Epochs:        []EpochInput{{Epoch: 11, Rows: epoch11Rows()}, {Epoch: 12, Rows: epoch12Rows()}},
			FundedThrough: scenarioFunding(),
		}
	}
	if _, err := Build(context.Background(), good()); err != nil {
		t.Fatal(err)
	}
	big := new(uint256.Int).Set(valueLimit)
	cases := []struct {
		name   string
		mutate func(*BuildInput)
		want   error
	}{
		{"epoch not after the previous root", func(in *BuildInput) { in.Epoch, in.Epochs = 10, nil }, ErrCoveredEpochs},
		{"first epoch 0", func(in *BuildInput) { in.Previous, in.FirstEpoch = nil, 0 }, ErrCoveredEpochs},
		{"missing covered epoch", func(in *BuildInput) { in.Epochs = in.Epochs[1:] }, ErrCoveredEpochs},
		{"wrong covered epoch", func(in *BuildInput) { in.Epochs[0].Epoch = 13 }, ErrCoveredEpochs},
		{"more than 96 epochs", func(in *BuildInput) { in.Epoch = 10 + MaxCoveredEpochs + 1 }, ErrTooManyEpochs},
		{"previous without accounts", func(in *BuildInput) { in.Previous.Accounts = nil }, ErrPrevious},
		{"previous root differs", func(in *BuildInput) { in.Previous.Root.Root[0] ^= 1 }, ErrPrevious},
		{"previous total differs", func(in *BuildInput) { in.Previous.Root.Total = rao(1) }, ErrPrevious},
		{"previous leaf count differs", func(in *BuildInput) { in.Previous.Root.LeafCount++ }, ErrPrevious},
		{"previous account changed", func(in *BuildInput) { in.Previous.Accounts[0].Cumulative = rao(1) }, ErrPrevious},
		{"previous value 2^128", func(in *BuildInput) { in.Previous.Accounts[0].Cumulative.Set(big) }, merkle.ErrLedgerValueRange},
		{"previous coldkey repeated", func(in *BuildInput) {
			in.Previous.Accounts[1].Coldkey = in.Previous.Accounts[0].Coldkey
			tree, _ := merkle.BuildLedgerSumTree(context.Background(), in.Previous.Accounts)
			in.Previous.Root.Root, in.Previous.Root.Total = tree.Root()
		}, ErrPrevious},
		{"funding missing", func(in *BuildInput) { delete(in.FundedThrough, 11) }, ErrFunding},
		{"funding decreases", func(in *BuildInput) { in.FundedThrough[11] = rao(1) }, ErrFunding},
		{"funding reaches 2^128", func(in *BuildInput) { in.FundedThrough[12] = *big }, ErrFunding},
		{"total not increased", func(in *BuildInput) { in.FundedThrough[11], in.FundedThrough[12] = f.Commit.Total, f.Commit.Total }, ErrTotalNotIncreased},
		{"unrecoverable epoch with rows", func(in *BuildInput) { in.Epochs[0].Null = true }, ErrEpochInput},
		{"unrecoverable epoch with chunks", func(in *BuildInput) { in.Epochs[0] = EpochInput{Epoch: 11, Null: true, Chunks: []Ref{}} }, ErrEpochInput},
		{"zero client id", func(in *BuildInput) { in.Epochs[0].Rows[0].ClientID = [16]byte{} }, ErrRowZeroClientID},
		{"zero usage", func(in *BuildInput) { in.Epochs[0].Rows[1].UsageBytes = 0 }, ErrRowZeroUsage},
		{"unsorted rows", func(in *BuildInput) { r := in.Epochs[1].Rows; r[0], r[1] = r[1], r[0] }, ErrRowsUnsorted},
		{"duplicate client id", func(in *BuildInput) { in.Epochs[1].Rows[1].ClientID = in.Epochs[1].Rows[0].ClientID }, ErrRowDuplicate},
		{"total usage 2^64", func(in *BuildInput) { in.Epochs[1].Rows[0].UsageBytes = ^uint64(0) - 516 }, ErrUsageOverflow},
		{"rows differ from fixed chunks", func(in *BuildInput) { in.Epochs[0].Chunks = []Ref{{1}} }, ErrFixedInputs},
		{"root epoch does not allocate", func(in *BuildInput) { in.Epochs[1] = EpochInput{Epoch: 12} }, ErrEpochNotAllocated},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := good()
			c.mutate(&in)
			if _, err := Build(context.Background(), in); !errors.Is(err, c.want) {
				t.Fatalf("%v, want %v", err, c.want)
			}
		})
	}
}

// GL-9 (check 5), in the builder: the total usage bound is exact. 2^64 − 1
// bytes over all rows, eligible or not, are accepted; 2^64 are refused.
func TestBuildUsageBound(t *testing.T) {
	f := firstRoot(t, epoch10Rows(), 1_000_003)
	rows := []protocol.LedgerUsageRow{row(1, 1, 1<<63, false), row(2, 0, 1<<63-1, false)}
	if _, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: rows}}, funded(11, 2_000_000)); err != nil {
		t.Fatalf("2^64 − 1 bytes: %v", err)
	}
	rows[1].UsageBytes++
	if _, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: rows}}, funded(11, 2_000_000)); !errors.Is(err, ErrUsageOverflow) {
		t.Fatalf("2^64 bytes: %v", err)
	}
}

// GL-1, in the builder: a header over 256 KiB is refused, with the bound
// lowered so a small ledger reaches it.
func TestBuildHeaderTooLarge(t *testing.T) {
	defer func(limit int) { maxHeaderBytes = limit }(maxHeaderBytes)
	maxHeaderBytes = 200
	_, err := Build(context.Background(), BuildInput{NoID: testNoID, Epoch: 10, FirstEpoch: testFirst, Epochs: []EpochInput{{Epoch: 10, Rows: epoch10Rows()}}, FundedThrough: funded(10, 1)})
	if !errors.Is(err, ErrHeaderTooLarge) {
		t.Fatalf("%v, want ErrHeaderTooLarge", err)
	}
}

// GL-3: cancellation at any point is returned as the context's error; a
// nil context is refused.
func TestBuildCanceled(t *testing.T) {
	//lint:ignore SA1012 a nil context is the refusal under test
	if _, err := Build(nil, BuildInput{}); !errors.Is(err, ErrInput) {
		t.Fatalf("nil context: %v", err)
	}
	s := newScenario(t)
	in := BuildInput{
		NoID: testNoID, Epoch: 12, FirstEpoch: testFirst,
		Previous:      &Previous{Root: s.f.Commit, Accounts: s.f.Accounts},
		Epochs:        []EpochInput{{Epoch: 11, Rows: epoch11Rows()}, {Epoch: 12, Rows: epoch12Rows()}},
		FundedThrough: s.funded,
	}
	for n := 0; ; n++ {
		_, err := Build(&cancelAfter{Context: context.Background(), n: n}, in)
		if err == nil {
			if n < 5 {
				t.Fatalf("only %d context checks", n)
			}
			return
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled after %d checks: %v", n, err)
		}
	}
}
