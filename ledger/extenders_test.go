package ledger

// The extender pool (WHITEPAPER.md §8.6.2, §8.6.4, §8.6.5): the census, the
// cap, the two-level split, and an epoch's allocation with its share rows.
// The numbers are exact, so a failure names the rule that broke.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/protocol"
)

// unitHash returns unit n; units order as their n.
func unitHash(n int) [32]byte {
	var b [32]byte
	binary.BigEndian.PutUint64(b[:8], uint64(n))
	b[31] = 0xa5
	return b
}

// extenderID returns extender id n.
func extenderID(n int) [16]byte {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(n))
	b[15] = 0xe0
	return b
}

// censusRow returns a census row for extender ext of client in unit u, active
// unless flags say otherwise; cold 0 is no wallet. An inactive row has no
// unit.
func censusRow(ext, client, cold, u int, flags CensusFlags) CensusRow {
	r := CensusRow{ExtenderID: extenderID(ext), ClientID: id(client), Flags: flags}
	if cold != 0 {
		r.Coldkey = key(cold)
	}
	if flags&CensusActive != 0 {
		r.Unit, r.Family = unitHash(u), UnitIPv4
	}
	return r
}

func active(ext, client, cold, u int) CensusRow { return censusRow(ext, client, cold, u, CensusActive) }

func shareRow(client, cold int, usage uint64) protocol.LedgerUsageRow {
	return protocol.LedgerUsageRow{ClientID: id(client), Coldkey: key(cold), UsageBytes: usage}
}

// deltaOf returns coldkey n's delta in an epoch's allocation, 0 without one.
func deltaOf(alloc *EpochAllocation, n int) uint64 {
	for _, d := range alloc.Deltas {
		if d.Coldkey == key(n) {
			return d.Delta.Uint64()
		}
	}
	return 0
}

// previousRoot is a finalized root of epoch with the given accounts, and its
// published header and account chunks.
func previousRoot(t *testing.T, epoch uint64, accounts []merkle.LedgerAccount) (*Previous, Objects) {
	t.Helper()
	tree, err := merkle.BuildLedgerSumTree(context.Background(), accounts)
	if err != nil {
		t.Fatal(err)
	}
	objects := Objects{}
	chunks, err := EncodeAccountChunks(accounts)
	if err != nil {
		t.Fatal(err)
	}
	header := &Header{NoID: testNoID, Epoch: epoch, Epochs: []HeaderEpoch{{Epoch: epoch, Providers: []Ref{}}}, Accounts: putChunks(chunks, objects)}
	data, err := EncodeHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	objects[RefOf(data)] = data
	root, total := tree.Root()
	return &Previous{Root: Root{Epoch: epoch, Root: root, Total: total, LeafCount: tree.LeafCount(), LedgerHash: RefOf(data)}, Accounts: accounts}, objects
}

func account(cold int, cumulative uint64) merkle.LedgerAccount {
	return merkle.LedgerAccount{Coldkey: key(cold), Cumulative: rao(cumulative)}
}

// The worked example's rows (§8.6.5): coldkeys a = 1, b = 2, c = 3, d = 4 and
// e = 5. Providers a 6.0 TB and b 2.5 TB serve other networks; c serves 0.5
// TB to other networks and 0.4 TB to its own network's clients, which from
// E_x is intra-network; client 4 has no wallet.
func workedExampleRows(amended bool) []protocol.LedgerUsageRow {
	c := uint64(900_000_000_000)
	if amended {
		c = 500_000_000_000
	}
	return []protocol.LedgerUsageRow{
		row(1, 1, 6_000_000_000_000, false),
		row(2, 2, 2_500_000_000_000, false),
		row(3, 3, c, false),
		row(4, 0, 1_000_000_000_000, false),
	}
}

// The census: x1 and x2 share the /29 unit u1, x2 paid to b, which also runs a
// provider; x3 is alone in u2, after u1; x4 has no wallet (E4), in a unit of
// its own that therefore holds no eligible extender.
func workedExampleCensus() []CensusRow {
	return []CensusRow{
		active(1, 11, 4, 1), // x1, coldkey d
		active(2, 12, 2, 1), // x2, coldkey b
		active(3, 13, 5, 2), // x3, coldkey e
		active(4, 14, 0, 0), // x4, no wallet, unit u0
	}
}

// Z: c's 400,000,000,000 intra-network bytes and 600,000,000,001
// intra-platform bytes.
const workedExampleZ = 400_000_000_000 + 600_000_000_001

// §8.6.5, exactly: A = 1,000 α = 10^12 rao, Z = 1,000,000,000,001 below the
// cap of 9.0 TB, M = 2 units and N = 3 eligible extenders. Each unit takes
// half, u1 the remainder byte; x1 takes u1's remainder byte as the lower
// client id. The allocation is the table's, the leaves are the table's over
// the example's previous cumulatives, and the "today" column is the same
// epoch without the amendment, c's same-network bytes still provider usage
// and the intra-platform bytes dropped.
func TestExtenderPoolWorkedExample(t *testing.T) {
	ctx := context.Background()
	const amount = 1_000_000_000_000
	previous, previousObjects := previousRoot(t, 7, []merkle.LedgerAccount{
		account(1, 12_345_678_901), // a, 12.345678901 α
		account(2, 2_000_000_000),  // b, 2 α
		account(3, 500_000_000),    // c, 0.5 α
	})
	fundedThrough := funded(8, 14_845_678_901+amount)
	in := BuildInput{
		NoID: testNoID, Epoch: 8, Previous: previous, FundedThrough: fundedThrough,
		Epochs: []EpochInput{{Epoch: 8, Rows: workedExampleRows(true), Extenders: &ExtenderInput{Bytes: workedExampleZ, Census: workedExampleCensus()}}},
	}
	l, err := Build(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	alloc := &l.Epochs[0]
	split := alloc.Extenders
	if split == nil || split.Bytes != workedExampleZ || split.Weight != workedExampleZ || split.Units != 2 || split.Eligible != 3 {
		t.Fatalf("split = %+v, want Z = Z' = %d, M = 2, N = 3", split, uint64(workedExampleZ))
	}
	wantShares := []protocol.LedgerUsageRow{
		shareRow(11, 4, 250_000_000_001), // x1
		shareRow(12, 2, 250_000_000_000), // x2
		shareRow(13, 5, 500_000_000_000), // x3
	}
	if !slices.Equal(split.Rows, wantShares) {
		t.Fatalf("share rows = %+v, want %+v", split.Rows, wantShares)
	}
	// ΣW = 9,000,000,000,000 + 1,000,000,000,001
	if w := EligibleUsage(workedExampleRows(true)) + split.Weight; w != 10_000_000_000_001 {
		t.Fatalf("ΣW = %d", w)
	}
	for cold, want := range map[int]uint64{1: 600_000_000_000, 2: 275_000_000_000, 3: 50_000_000_000, 4: 25_000_000_000, 5: 50_000_000_000} {
		if got := deltaOf(alloc, cold); got != want {
			t.Fatalf("coldkey %d: δ = %d, want %d", cold, got, want)
		}
	}
	if sum := sumDeltas(alloc.Deltas); !sum.Eq(uint256.NewInt(amount)) || !alloc.Amount.Eq(uint256.NewInt(amount)) {
		t.Fatalf("Σδ = %s of amount %s, want %d", sum.Dec(), alloc.Amount.Dec(), uint64(amount))
	}
	// a, b and c keep their positions; d and e are appended in ascending order.
	wantLeaves := []merkle.LedgerAccount{
		account(1, 612_345_678_901), account(2, 277_000_000_000), account(3, 50_500_000_000),
		account(4, 25_000_000_000), account(5, 50_000_000_000),
	}
	if !slices.EqualFunc(l.Accounts, wantLeaves, func(a, b merkle.LedgerAccount) bool { return a.Coldkey == b.Coldkey && a.Cumulative.Eq(&b.Cumulative) }) {
		t.Fatalf("leaves = %+v", l.Accounts)
	}
	if total := l.Commit.Total; !total.Eq(uint256.NewInt(1_014_845_678_901)) {
		t.Fatalf("total = %s, want 1,014.845678901 α", total.Dec())
	}
	if !bytes.HasPrefix(l.Header, []byte(`{"schema":"`+SchemaV3+`"`)) {
		t.Fatalf("header %s is not %s", l.Header, SchemaV3)
	}

	// The validator recomputes it and approves.
	objects := maps.Clone(previousObjects)
	maps.Copy(objects, l.Objects)
	custody := NewMemoryCustody()
	res := mustVerify(t, VerifyInput{NoID: testNoID, Pending: l.Commit, Seq: 2, Previous: &previous.Root, FundedThrough: fundedThrough,
		Fetcher: objects, Custody: custody, MaxFetchBytes: DefaultMaxFetchBytes, ExtendersFrom: 8})
	expectPass(t, res)
	if component, ok, _ := custody.ValidExtenders(ctx, testNoID, 8); !ok || !component.Equal(alloc.Extenders.Header()) {
		t.Fatalf("custody holds %+v, %v as epoch 8's valid component", component, ok)
	}

	// Today: c's 0.4 TB is provider usage, the intra-platform bytes are
	// dropped and there is no extender component, for ΣW = 9.4 TB.
	in.Epochs = []EpochInput{{Epoch: 8, Rows: workedExampleRows(false)}}
	today, err := Build(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	for cold, want := range map[int]uint64{1: 638_297_872_340, 2: 265_957_446_809, 3: 95_744_680_851, 4: 0, 5: 0} {
		if got := deltaOf(&today.Epochs[0], cold); got != want {
			t.Fatalf("today, coldkey %d: δ = %d, want %d", cold, got, want)
		}
	}
	if today.Epochs[0].Extenders != nil || !bytes.HasPrefix(today.Header, []byte(`{"schema":"`+Schema+`"`)) {
		t.Fatalf("an epoch without an extender pool built %+v in %s", today.Epochs[0].Extenders, today.Header)
	}
}

// buildOne builds one epoch over F (pool 7's first root) with an extender
// pool, funded with amount.
func buildOne(t *testing.T, f *Ledger, amount uint64, rows []protocol.LedgerUsageRow, extenders *ExtenderInput) *Ledger {
	t.Helper()
	l, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: rows, Extenders: extenders}}, funded(10, f.Commit.Total.Uint64(), 11, f.Commit.Total.Uint64()+amount))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// The cap (M3, ζ = 1): Z enters the allocation as at most the epoch's
// eligible provider usage W_k, so extenders take at most half of the amount
// however large Z is. Bytes above the cap enter no weight: Z at the cap and
// at ten times it allocate identically, and only the header's bytes differ.
// W_k counts only eligible rows: a provider without a wallet or head-excluded
// raises no cap.
func TestExtenderPoolCap(t *testing.T) {
	f := firstRoot(t, epoch10Rows(), 1_000)
	rows := []protocol.LedgerUsageRow{
		row(1, 1, 600, false),
		row(2, 2, 400, false),
		row(3, 0, 5_000, false), // no wallet
		row(4, 3, 7_000, true),  // head-excluded
	}
	census := []CensusRow{active(1, 21, 9, 1)}
	if w := EligibleUsage(rows); w != 1_000 {
		t.Fatalf("W = %d, want 1,000", w)
	}
	for _, c := range []struct {
		z, weight uint64
	}{
		{z: 999, weight: 999},
		{z: 1_000, weight: 1_000},
		{z: 1_001, weight: 1_000},
		{z: 10_000, weight: 1_000},
		{z: 1 << 63, weight: 1_000},
	} {
		l := buildOne(t, f, 1_000_000, rows, &ExtenderInput{Bytes: c.z, Census: census})
		alloc := &l.Epochs[0]
		if alloc.Extenders.Bytes != c.z || alloc.Extenders.Weight != c.weight || !slices.Equal(alloc.Extenders.Rows, []protocol.LedgerUsageRow{shareRow(21, 9, c.weight)}) {
			t.Fatalf("Z = %d: split %+v, want Z' = %d", c.z, alloc.Extenders, c.weight)
		}
		// δ_x = A·Z'/(W + Z'): exactly half at the cap
		want := uint64(1_000_000) * c.weight / (1_000 + c.weight)
		if got := deltaOf(alloc, 9); got != want && got != want+1 {
			t.Fatalf("Z = %d: the extender's δ = %d, want %d", c.z, got, want)
		}
		if c.weight == 1_000 && (deltaOf(alloc, 9) != 500_000 || deltaOf(alloc, 1)+deltaOf(alloc, 2) != 500_000) {
			t.Fatalf("Z = %d: extenders %d and providers %d of 1,000,000, want half each", c.z, deltaOf(alloc, 9), deltaOf(alloc, 1)+deltaOf(alloc, 2))
		}
	}
	atCap := buildOne(t, f, 1_000_000, rows, &ExtenderInput{Bytes: 1_000, Census: census})
	above := buildOne(t, f, 1_000_000, rows, &ExtenderInput{Bytes: 10_000, Census: census})
	if !slices.Equal(atCap.Epochs[0].Deltas, above.Epochs[0].Deltas) || !slices.Equal(atCap.Epochs[0].Extenders.Chunks, above.Epochs[0].Extenders.Chunks) || !slices.Equal(atCap.Accounts, above.Accounts) {
		t.Fatal("bytes above the cap changed the allocation")
	}
	if bytes.Equal(atCap.Header, above.Header) {
		t.Fatal("the header does not record Z")
	}
}

// The empty set (Q6): with no eligible extender, and so no unit, Z is
// dropped. It enters no weight, the component has no share rows, and the
// epoch allocates exactly as it does without the amendment. That holds for an
// empty census and for a census whose every row fails one rule. With no
// eligible provider usage the cap is 0, so Z enters no weight either, and the
// epoch carries its funding as any epoch without eligible usage does.
func TestExtenderPoolEmptySet(t *testing.T) {
	f := firstRoot(t, epoch10Rows(), 1_000)
	rows := epoch11Rows()
	without := buildOne(t, f, 999_983, rows, nil)
	ineligible := []CensusRow{
		censusRow(1, 21, 9, 0, 0),                            // inactive at end(k) (E1)
		censusRow(2, 22, 9, 1, CensusActive|CensusGated),     // the operator's gated fleet (E5)
		censusRow(3, 23, 9, 2, CensusActive|CensusCanary),    // a canary (E5)
		censusRow(4, 24, 9, 3, CensusActive|CensusDeclared),  // a declared own network (E5)
		censusRow(5, 25, 9, 4, CensusRevoked),                // revoked
		censusRow(6, 26, 9, 5, CensusActive|CensusHeadBound), // head-bound (E4)
		active(7, 27, 0, 6),                                  // no wallet (E4)
		censusRow(8, 27, 0, 7, CensusActive|CensusDuplicate), // a second record of client 27 (E3)
		censusRow(9, 28, 9, 8, CensusActive|CensusDuplicate), // shares an address with a lower record (E3)
		censusRow(10, 29, 9, 9, CensusActive|CensusGated|CensusCanary|CensusDeclared|CensusHeadBound),
	}
	for name, census := range map[string][]CensusRow{"no extenders": {}, "no eligible extender": ineligible} {
		for _, z := range []uint64{0, 1, 123_456_789} {
			l := buildOne(t, f, 999_983, rows, &ExtenderInput{Bytes: z, Census: census})
			alloc := &l.Epochs[0]
			if split := alloc.Extenders; split.Bytes != z || split.Weight != 0 || split.Eligible != 0 || split.Units != 0 || len(split.Rows) != 0 || len(split.Chunks) != 0 {
				t.Fatalf("%s, Z = %d: split %+v, want Z dropped", name, z, split)
			}
			if !slices.Equal(alloc.Deltas, without.Epochs[0].Deltas) || !slices.Equal(l.Accounts, without.Accounts) || l.Commit.Root != without.Commit.Root {
				t.Fatalf("%s, Z = %d: the allocation differs from the epoch's without the amendment", name, z)
			}
		}
	}

	// No eligible provider usage: the cap is 0. The root's own epoch then
	// does not allocate, and an earlier one carries its funding.
	noUsage := []protocol.LedgerUsageRow{row(1, 0, 500, false), row(2, 1, 50, true)}
	census := []CensusRow{active(1, 21, 9, 1)}
	_, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: noUsage, Extenders: &ExtenderInput{Bytes: 1_000, Census: census}}}, funded(10, 1_000, 11, 2_000))
	if !errors.Is(err, ErrEpochNotAllocated) {
		t.Fatalf("an epoch without eligible provider usage allocated its extender pool: %v", err)
	}
	l, err := buildOver(f, []EpochInput{
		{Epoch: 11, Rows: noUsage, Extenders: &ExtenderInput{Bytes: 1_000, Census: census}},
		{Epoch: 12, Rows: epoch12Rows(), Extenders: &ExtenderInput{Census: census}},
	}, funded(10, 1_000, 11, 2_000, 12, 3_000))
	if err != nil {
		t.Fatal(err)
	}
	if e11, e12 := &l.Epochs[0], &l.Epochs[1]; e11.Allocated || e11.Extenders.Weight != 0 || len(e11.Extenders.Rows) != 0 || !e12.Allocated || !e12.Amount.Eq(uint256.NewInt(2_000)) {
		t.Fatalf("epoch 11 %+v, epoch 12 %+v: want epoch 11's funding carried", e11, e12)
	}
}

// The two-level split (decision 1): Z' is split evenly over the units that
// hold an eligible extender, by unit hash, the remainder bytes to the lowest
// hashes, then evenly over each unit's eligible extenders, by client id, the
// remainder to the lowest. Extenders sharing a /29 share its unit, so
// packing extenders into one block gains nothing; a unit holding only
// ineligible extenders is not a unit; a share of zero has no row; one IPv6
// /48 unit counts like an IPv4 /29 one; and the rows sum to Z' exactly.
func TestExtenderPoolUnitSplit(t *testing.T) {
	providers := []protocol.LedgerUsageRow{row(1, 1, 1<<50, false)}
	v6 := active(5, 35, 8, 9)
	v6.Family = UnitIPv6
	census := []CensusRow{
		active(1, 31, 3, 4), // unit 4
		active(2, 32, 4, 2), // unit 2
		active(3, 33, 5, 4), // unit 4
		censusRow(4, 34, 6, 1, CensusActive|CensusGated), // unit 1, gated: not a unit
		v6,                  // unit 9, IPv6
		active(6, 36, 7, 4), // unit 4
		censusRow(7, 37, 3, 3, CensusActive|CensusCanary),    // unit 3, a canary: not a unit
		censusRow(8, 38, 3, 2, CensusActive|CensusHeadBound), // unit 2, head-bound: does not count in it
	}
	for _, c := range []struct {
		weight uint64
		want   map[int]uint64 // client → share
	}{
		// units 2, 4 and 9: 3 + 2 + 2 bytes; unit 4's 2 bytes to 31 and 33
		{weight: 7, want: map[int]uint64{32: 3, 31: 1, 33: 1, 35: 2}},
		// 34 bytes: 12, 11, 11; unit 4's 11 as 4, 4, 3
		{weight: 34, want: map[int]uint64{32: 12, 31: 4, 33: 4, 36: 3, 35: 11}},
		// 2 bytes: units 2 and 4 take one each, unit 9 none
		{weight: 2, want: map[int]uint64{32: 1, 31: 1}},
		{weight: 1, want: map[int]uint64{32: 1}},
		{weight: 0, want: map[int]uint64{}},
		{weight: 3_000_000_000_001, want: map[int]uint64{32: 1_000_000_000_001, 31: 333_333_333_334, 33: 333_333_333_333, 36: 333_333_333_333, 35: 1_000_000_000_000}},
	} {
		split, err := SplitExtenderPool(c.weight, providers, census)
		if err != nil {
			t.Fatal(err)
		}
		if split.Units != 3 || split.Eligible != 5 || split.Weight != c.weight {
			t.Fatalf("Z' = %d: M = %d, N = %d, weight %d; want 3 units of 5 eligible extenders", c.weight, split.Units, split.Eligible, split.Weight)
		}
		got := map[int]uint64{}
		var sum uint64
		for i, r := range split.Rows {
			if i > 0 && bytes.Compare(split.Rows[i-1].ClientID[:], r.ClientID[:]) >= 0 {
				t.Fatalf("Z' = %d: share rows out of client order", c.weight)
			}
			if r.UsageBytes == 0 || r.HeadExcluded {
				t.Fatalf("Z' = %d: share row %+v", c.weight, r)
			}
			client := int(binary.BigEndian.Uint64(r.ClientID[8:]))
			got[client] = r.UsageBytes
			sum += r.UsageBytes
			if want := slices.IndexFunc(census, func(cr CensusRow) bool { return cr.ClientID == r.ClientID }); census[want].Coldkey != r.Coldkey {
				t.Fatalf("Z' = %d: share row of client %d is paid to another coldkey", c.weight, client)
			}
		}
		if !maps.Equal(got, c.want) || sum != c.weight {
			t.Fatalf("Z' = %d: shares %v (Σ %d), want %v", c.weight, got, sum, c.want)
		}
	}

	// One extender alone in a block gets what three sharing a block get
	// together.
	split, err := SplitExtenderPool(900, providers, []CensusRow{active(1, 31, 3, 1), active(2, 32, 4, 1), active(3, 33, 5, 1), active(4, 34, 6, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(split.Rows, []protocol.LedgerUsageRow{shareRow(31, 3, 150), shareRow(32, 4, 150), shareRow(33, 5, 150), shareRow(34, 6, 450)}) {
		t.Fatalf("shares %+v", split.Rows)
	}
}

// E3 to E5 and E1 decide eligibility by the census's own flags. Every
// exclusion keeps its row in the census, eligible or not, and takes nothing:
// the operator's own fleet (gated, canaries, declared networks), records
// inactive or revoked at end(k), duplicates of a provider client or an
// address, head-bound clients and clients without a wallet. Only the
// eligible rows share Z'.
func TestExtenderPoolExclusions(t *testing.T) {
	f := firstRoot(t, epoch10Rows(), 1_000)
	rows := []protocol.LedgerUsageRow{row(1, 1, 1_000, false)}
	for _, c := range []struct {
		name  string
		row   CensusRow
		count bool
	}{
		{name: "eligible", row: active(2, 22, 8, 2), count: true},
		{name: "inactive at end(k)", row: censusRow(2, 22, 8, 0, 0)},
		{name: "revoked", row: censusRow(2, 22, 8, 0, CensusRevoked)},
		{name: "the operator's gated fleet", row: censusRow(2, 22, 8, 2, CensusActive|CensusGated)},
		{name: "a canary", row: censusRow(2, 22, 8, 2, CensusActive|CensusCanary)},
		{name: "a declared own network", row: censusRow(2, 22, 8, 2, CensusActive|CensusDeclared)},
		{name: "a duplicate", row: censusRow(2, 22, 8, 2, CensusActive|CensusDuplicate)},
		{name: "head-bound", row: censusRow(2, 22, 8, 2, CensusActive|CensusHeadBound)},
		{name: "no wallet", row: active(2, 22, 0, 2)},
	} {
		census := []CensusRow{active(1, 21, 9, 1), c.row}
		l := buildOne(t, f, 1_000_000, rows, &ExtenderInput{Bytes: 600, Census: census})
		split := l.Epochs[0].Extenders
		want := []protocol.LedgerUsageRow{shareRow(21, 9, 600)}
		if c.count {
			want = []protocol.LedgerUsageRow{shareRow(21, 9, 300), shareRow(22, 8, 300)}
		}
		if !slices.Equal(split.Rows, want) {
			t.Fatalf("%s: shares %+v, want %+v", c.name, split.Rows, want)
		}
		if got := deltaOf(&l.Epochs[0], 8); (got != 0) != c.count {
			t.Fatalf("%s: δ = %d", c.name, got)
		}
	}
}

// The census rules a validator can check without addresses.
func TestCheckCensusRows(t *testing.T) {
	valid := []CensusRow{active(1, 21, 9, 1), censusRow(2, 21, 9, 0, 0), active(3, 22, 8, 1), censusRow(4, 22, 8, 2, CensusActive|CensusDuplicate)}
	if err := CheckCensusRows(valid); err != nil {
		t.Fatalf("valid census: %v", err)
	}
	if err := CheckCensusRows(nil); err != nil {
		t.Fatalf("empty census: %v", err)
	}
	mutate := func(f func([]CensusRow) []CensusRow) []CensusRow { return f(slices.Clone(valid)) }
	for _, c := range []struct {
		name   string
		census []CensusRow
		want   error
	}{
		{"unknown flag", mutate(func(r []CensusRow) []CensusRow { r[1].Flags |= 0x80; return r }), ErrCensusEncoding},
		{"unknown family", mutate(func(r []CensusRow) []CensusRow { r[0].Family = 5; return r }), ErrCensusEncoding},
		{"zero extender id", mutate(func(r []CensusRow) []CensusRow { r[0].ExtenderID = [16]byte{}; return r }), ErrCensusZeroID},
		{"zero client id", mutate(func(r []CensusRow) []CensusRow { r[0].ClientID = [16]byte{}; return r }), ErrCensusZeroID},
		{"clients unsorted", mutate(func(r []CensusRow) []CensusRow { r[0], r[2] = r[2], r[0]; return r }), ErrCensusUnsorted},
		{"extenders unsorted within a client", mutate(func(r []CensusRow) []CensusRow {
			r[0].ExtenderID, r[1].ExtenderID = r[1].ExtenderID, r[0].ExtenderID
			return r
		}), ErrCensusUnsorted},
		{"a repeated row", append(slices.Clone(valid[:1]), valid[0]), ErrCensusDuplicate},
		{"an extender id under two clients", mutate(func(r []CensusRow) []CensusRow { r[2].ExtenderID = r[0].ExtenderID; return r }), ErrCensusDuplicate},
		{"an active row without a unit", mutate(func(r []CensusRow) []CensusRow { r[0].Unit = [32]byte{}; return r }), ErrCensusUnit},
		{"an active row without a family", mutate(func(r []CensusRow) []CensusRow { r[0].Family = UnitNone; return r }), ErrCensusUnit},
		{"an inactive row with a unit", mutate(func(r []CensusRow) []CensusRow { r[1].Unit = unitHash(3); return r }), ErrCensusUnit},
		{"an inactive row with a family", mutate(func(r []CensusRow) []CensusRow { r[1].Family = UnitIPv6; return r }), ErrCensusUnit},
		{"a unit with two families", mutate(func(r []CensusRow) []CensusRow { r[2].Family = UnitIPv6; return r }), ErrCensusUnit},
		{"active and revoked", mutate(func(r []CensusRow) []CensusRow { r[0].Flags |= CensusRevoked; return r }), ErrCensusFlags},
		{"an inactive duplicate", mutate(func(r []CensusRow) []CensusRow { r[1].Flags |= CensusDuplicate; return r }), ErrCensusFlags},
		{"a client counted twice", mutate(func(r []CensusRow) []CensusRow { r[3].Flags = CensusActive; return r }), ErrCensusClient},
		{"a client's later record counted", mutate(func(r []CensusRow) []CensusRow {
			r[2].Flags, r[3].Flags = CensusActive|CensusDuplicate, CensusActive
			return r
		}), ErrCensusClient},
		{"a client with two coldkeys", mutate(func(r []CensusRow) []CensusRow { r[1].Coldkey = key(7); return r }), ErrCensusClient},
	} {
		if err := CheckCensusRows(c.census); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	// A census round-trips through its chunks.
	chunks := EncodeCensusChunks(valid)
	decoded, err := DecodeCensusChunks(chunks)
	if err != nil || !slices.Equal(decoded, valid) || len(chunks) != 1 || len(chunks[0]) != 4*CensusRowBytes {
		t.Fatalf("round trip: %v %+v", err, decoded)
	}
	if _, err := DecodeCensusChunks([][]byte{chunks[0][:CensusRowBytes+1]}); !errors.Is(err, ErrChunkLength) {
		t.Fatalf("a short row: %v", err)
	}
	if len(EncodeCensusChunks(nil)) != 0 {
		t.Fatal("an empty census has chunks")
	}
}

// The header: an epoch's extender component is its last key, a header with
// one is v3, and one without keeps the v2 bytes. Decoding refuses a schema
// that does not match, a null census or share list, and a component on an
// unrecoverable epoch.
func TestExtenderHeaderCodec(t *testing.T) {
	census, rows := RefOf([]byte("census")), RefOf([]byte("rows"))
	h := &Header{NoID: 1, Epoch: 8, Epochs: []HeaderEpoch{
		{Epoch: 7, Providers: []Ref{RefOf([]byte("p7"))}},
		{Epoch: 8, Providers: []Ref{RefOf([]byte("p8"))}, Recovery: []HeaderRecovery{{Epoch: 3, Amount: 5, Providers: []Ref{}}}, Extenders: &HeaderExtenders{Bytes: 42, Census: []Ref{census}, Rows: []Ref{rows}}},
	}, Accounts: []Ref{RefOf([]byte("a"))}}
	data, err := EncodeHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`{"schema":"urnetwork-payout-ledger-v3","no_id":1,"epoch":8,"epochs":[{"epoch":7,"providers":["%s"]},{"epoch":8,"providers":["%s"],"recovery":[{"epoch":3,"amount":5,"providers":[]}],"extenders":{"bytes":42,"census":["%s"],"rows":["%s"]}}],"accounts":["%s"]}`,
		RefOf([]byte("p7")), RefOf([]byte("p8")), census, rows, RefOf([]byte("a")))
	if string(data) != want {
		t.Fatalf("header\n%s\nwant\n%s", data, want)
	}
	decoded, err := DecodeHeader(data)
	if err != nil || !decoded.Epochs[1].Extenders.Equal(h.Epochs[1].Extenders) || decoded.Epochs[0].Extenders != nil {
		t.Fatalf("decode: %v %+v", err, decoded)
	}
	component := fmt.Sprintf(`,"extenders":{"bytes":42,"census":["%s"],"rows":["%s"]}`, census, rows)
	recovery := `,"recovery":[{"epoch":3,"amount":5,"providers":[]}]`
	for _, c := range []struct {
		name string
		data string
		want error
	}{
		{"v2 with a component", strings.Replace(want, "-v3", "-v2", 1), ErrHeaderSchema},
		{"v3 without one", strings.Replace(want, component, "", 1), ErrHeaderSchema},
		{"null census", strings.Replace(want, fmt.Sprintf(`"census":["%s"]`, census), `"census":null`, 1), ErrHeaderNull},
		{"null rows", strings.Replace(want, fmt.Sprintf(`"rows":["%s"]`, rows), `"rows":null`, 1), ErrHeaderNull},
		{"null component", strings.Replace(want, component, `,"extenders":null`, 1), ErrHeaderNotCanonical},
		{"keys out of order", strings.Replace(want, fmt.Sprintf(`"bytes":42,"census":["%s"]`, census), fmt.Sprintf(`"census":["%s"],"bytes":42`, census), 1), ErrHeaderNotCanonical},
		{"component before recovery", strings.Replace(want, recovery+component, component+recovery, 1), ErrHeaderNotCanonical},
		{"unknown key", strings.Replace(want, `"bytes":42,`, `"bytes":42,"units":2,`, 1), ErrHeaderDecode},
		{"a component on an unrecoverable epoch", `{"schema":"urnetwork-payout-ledger-v3","no_id":1,"epoch":8,"epochs":[{"epoch":8,"providers":null,"extenders":{"bytes":0,"census":[],"rows":[]}}],"accounts":[]}`, ErrHeaderExtenders},
	} {
		if _, err := DecodeHeader([]byte(c.data)); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	h.Epochs[1].Null = true
	if _, err := EncodeHeader(h); !errors.Is(err, ErrHeaderExtenders) {
		t.Fatalf("encoded a component on an unrecoverable epoch: %v", err)
	}
	// Without a component the header is v2, byte for byte as before.
	h.Epochs[1].Null, h.Epochs[1].Extenders = false, nil
	data, err = EncodeHeader(h)
	if err != nil || !bytes.HasPrefix(data, []byte(`{"schema":"urnetwork-payout-ledger-v2"`)) || bytes.Contains(data, []byte("extenders")) {
		t.Fatalf("header without a component: %v %s", err, data)
	}
}
