package ledger

import (
	"context"
	"encoding/binary"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/protocol"
)

const (
	testNoID  = 7
	testFirst = 10
)

// id returns client id n; ids order as their n.
func id(n int) [16]byte {
	var b [16]byte
	binary.BigEndian.PutUint64(b[8:], uint64(n))
	return b
}

// key returns coldkey n; coldkeys order as their n.
func key(n int) [32]byte {
	var b [32]byte
	binary.BigEndian.PutUint64(b[:8], uint64(n))
	b[31] = 0x5a
	return b
}

// row returns a provider row; cold 0 is the zero coldkey (no wallet).
func row(client, cold int, usage uint64, head bool) protocol.LedgerUsageRow {
	r := protocol.LedgerUsageRow{ClientID: id(client), UsageBytes: usage, HeadExcluded: head}
	if cold != 0 {
		r.Coldkey = key(cold)
	}
	return r
}

func rao(v uint64) uint256.Int { return *uint256.NewInt(v) }

// funded returns fundedThrough snapshots from epoch, value pairs.
func funded(pairs ...uint64) map[uint64]uint256.Int {
	out := map[uint64]uint256.Int{}
	for i := 0; i < len(pairs); i += 2 {
		out[pairs[i]] = rao(pairs[i+1])
	}
	return out
}

// heads is a HeadOracle: per epoch, candidates mapped to an active binding.
type heads map[uint64]map[[16]byte]bool

func (h heads) HeadBindings(ctx context.Context, epoch uint64) (map[[16]byte]bool, error) {
	return h[epoch], nil
}

// The standard scenario. Pool 7's first root F covers epoch 10 and is
// finalized after the validator audited it; the pending root P covers 11 and
// 12. It has a shared coldkey (clients 1 and 2), a provider without a
// wallet, head-excluded providers, a dormant account (coldkey 4 in P) and
// new accounts (coldkeys 3, 5, 7 and 8 in P).

func epoch10Rows() []protocol.LedgerUsageRow {
	return []protocol.LedgerUsageRow{
		row(1, 1, 300, false),
		row(2, 1, 100, false),
		row(3, 2, 200, false),
		row(4, 0, 500, false),
		row(5, 3, 400, true),
		row(6, 4, 1, false),
	}
}

func epoch11Rows() []protocol.LedgerUsageRow {
	return []protocol.LedgerUsageRow{
		row(1, 1, 250, false),
		row(3, 2, 250, false),
		row(5, 3, 100, false),
		row(8, 5, 300, false),
		row(9, 6, 100, true),
		row(10, 0, 50, false),
	}
}

func epoch12Rows() []protocol.LedgerUsageRow {
	return []protocol.LedgerUsageRow{
		row(3, 2, 500, false),
		row(11, 7, 500, false),
		row(12, 5, 10, false),
		row(13, 8, 7, false),
	}
}

func scenarioHeads() heads {
	return heads{
		10: {id(5): true, id(7): false},
		11: {id(9): true, id(5): false},
	}
}

func scenarioFunding() map[uint64]uint256.Int {
	return funded(10, 1_000_003, 11, 1_600_000, 12, 2_500_000)
}

type scenario struct {
	f, p    *Ledger
	custody *MemoryCustody
	heads   heads
	funded  map[uint64]uint256.Int
}

func newScenario(t *testing.T) *scenario {
	t.Helper()
	ctx := context.Background()
	s := &scenario{custody: NewMemoryCustody(), heads: scenarioHeads(), funded: scenarioFunding()}
	var err error
	s.f, err = Build(ctx, BuildInput{NoID: testNoID, Epoch: 10, FirstEpoch: testFirst, Epochs: []EpochInput{{Epoch: 10, Rows: epoch10Rows()}}, FundedThrough: s.funded})
	if err != nil {
		t.Fatal(err)
	}
	// The validator audited F while it was pending.
	res, err := Verify(ctx, VerifyInput{NoID: testNoID, FirstEpoch: testFirst, Pending: s.f.Commit, Seq: 1, FundedThrough: s.funded, Fetcher: s.f.Objects, Custody: s.custody, Heads: s.heads, MaxFetchBytes: DefaultMaxFetchBytes})
	if err != nil || res.Err != nil {
		t.Fatalf("auditing F: %v %v", err, res)
	}
	s.p = s.build(t, epoch11Rows(), epoch12Rows())
	return s
}

// build builds a root for epoch 12 over F from epoch 11 and 12 rows.
func (s *scenario) build(t *testing.T, rows11, rows12 []protocol.LedgerUsageRow) *Ledger {
	t.Helper()
	l, err := Build(context.Background(), BuildInput{
		NoID: testNoID, Epoch: 12, FirstEpoch: testFirst,
		Previous:      &Previous{Root: s.f.Commit, Accounts: s.f.Accounts},
		Epochs:        []EpochInput{{Epoch: 11, Rows: rows11}, {Epoch: 12, Rows: rows12}},
		FundedThrough: s.funded,
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// input is the validator's input for a pending root over F, published as objects.
func (s *scenario) input(pending Root, objects Objects) VerifyInput {
	return VerifyInput{
		NoID: testNoID, FirstEpoch: testFirst, Pending: pending, Seq: 2, Previous: &s.f.Commit,
		FundedThrough: s.funded, Fetcher: objects, Custody: s.custody, Heads: s.heads, MaxFetchBytes: DefaultMaxFetchBytes,
	}
}

func (s *scenario) verify(t *testing.T, pending Root, objects Objects) *Result {
	t.Helper()
	return mustVerify(t, s.input(pending, objects))
}

func mustVerify(t *testing.T, in VerifyInput) *Result {
	t.Helper()
	res, err := Verify(context.Background(), in)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return res
}

// publication is a pending root's published objects, open to mutation.
type publication struct {
	header  *Header
	objects Objects
	commit  Root
}

func publish(t *testing.T, l *Ledger) *publication {
	t.Helper()
	h, err := DecodeHeader(l.Header)
	if err != nil {
		t.Fatal(err)
	}
	return &publication{header: h, objects: maps.Clone(l.Objects), commit: l.Commit}
}

func (pub *publication) put(chunks [][]byte) []Ref {
	refs := make([]Ref, len(chunks))
	for j, chunk := range chunks {
		refs[j] = RefOf(chunk)
		pub.objects[refs[j]] = chunk
	}
	return refs
}

// setChunks publishes covered epoch i's provider chunks.
func (pub *publication) setChunks(i int, chunks [][]byte) {
	pub.header.Epochs[i].Providers = pub.put(chunks)
}

// setRows publishes covered epoch i's rows.
func (pub *publication) setRows(i int, rows []protocol.LedgerUsageRow) {
	pub.setChunks(i, EncodeProviderChunks(rows))
}

// setAccounts publishes accounts and commits their tree, as an operator
// committing a mutated ledger would.
func (pub *publication) setAccounts(t *testing.T, accounts []merkle.LedgerAccount) {
	t.Helper()
	chunks, err := EncodeAccountChunks(accounts)
	if err != nil {
		t.Fatal(err)
	}
	pub.header.Accounts = pub.put(chunks)
	tree, err := merkle.BuildLedgerSumTree(context.Background(), accounts)
	if err != nil {
		t.Fatal(err)
	}
	pub.commit.Root, pub.commit.Total = tree.Root()
	pub.commit.LeafCount = tree.LeafCount()
}

// seal publishes the header and commits its hash.
func (pub *publication) seal(t *testing.T) Root {
	t.Helper()
	data, err := EncodeHeader(pub.header)
	if err != nil {
		t.Fatal(err)
	}
	return pub.sealBytes(data)
}

func (pub *publication) sealBytes(data []byte) Root {
	ref := RefOf(data)
	pub.objects[ref] = data
	pub.commit.LedgerHash = ref
	return pub.commit
}

// accountIndex returns the position of coldkey n's account.
func accountIndex(t *testing.T, accounts []merkle.LedgerAccount, n int) int {
	t.Helper()
	i := slices.IndexFunc(accounts, func(a merkle.LedgerAccount) bool { return a.Coldkey == key(n) })
	if i < 0 {
		t.Fatalf("coldkey %d has no account", n)
	}
	return i
}

// checkErrors lists every error value a check reports.
var checkErrors = []error{
	ErrObjectUnavailable, ErrByteBudget,
	ErrLedgerHash, ErrHeaderTooLarge, ErrHeaderDecode, ErrHeaderTrailingData, ErrHeaderNotCanonical, ErrHeaderSchema,
	ErrHeaderNull, ErrRef, ErrHeaderNoID, ErrHeaderEpoch, ErrCoveredEpochs, ErrTooManyEpochs,
	ErrPreviousNotTight, ErrPreviousRebuild,
	ErrFixedInputs, ErrNullAgainstValidRows,
	ErrChunkHash, ErrChunkLength, ErrChunkShort, ErrChunkEmpty, ErrRowEncoding,
	ErrRowZeroClientID, ErrRowZeroUsage, ErrRowsUnsorted, ErrRowDuplicate, ErrUsageOverflow,
	ErrHeadFlagSet, ErrHeadFlagCleared,
	ErrEpochNotAllocated, ErrAccountOmitted, ErrAccountDelta,
	ErrAccountMoved, ErrAccountLowered, ErrAccountDuplicate, ErrAccountUnsorted, ErrAccountZeroDelta,
	ErrTreeLeafCount, ErrTreeTotal, ErrTreeRoot,
	ErrRecoveryUnexpected, ErrRecoveryMissing, ErrRecoveryAmount,
}

// expectFailure requires that res failed exactly check with exactly the
// error values want, and that it calls for action.
func expectFailure(t *testing.T, res *Result, check Check, action Action, want ...error) {
	t.Helper()
	if res.Check != check {
		t.Fatalf("check %v (%v), want %v", res.Check, res.Err, check)
	}
	var matched []error
	for _, err := range checkErrors {
		if errors.Is(res.Err, err) {
			matched = append(matched, err)
		}
	}
	if len(matched) != len(want) {
		t.Fatalf("error %v matches %v, want %v", res.Err, matched, want)
	}
	for _, err := range want {
		if !slices.Contains(matched, err) {
			t.Fatalf("error %v matches %v, want %v", res.Err, matched, want)
		}
	}
	if got := res.Action(); got != action {
		t.Fatalf("action %v, want %v (%v)", got, action, res.Err)
	}
}

func expectPass(t *testing.T, res *Result) {
	t.Helper()
	if res.Err != nil || res.Check != CheckNone || !res.Reproduced || len(res.Faults) != 0 || len(res.Missing) != 0 || res.Action() != ActionApprove {
		t.Fatalf("root refused: check %v: %v (reproduced %v, faults %v, missing %v)", res.Check, res.Err, res.Reproduced, res.Faults, res.Missing)
	}
}
