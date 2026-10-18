package protocol

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/holiman/uint256"
)

// ledgerKey returns a coldkey with only its first and last bytes set.
func ledgerKey(first, last byte) (key [32]byte) {
	key[0], key[31] = first, last
	return key
}

func ledgerRow(id uint64, coldkey [32]byte, usage uint64) LedgerUsageRow {
	row := LedgerUsageRow{Coldkey: coldkey, UsageBytes: usage}
	binary.BigEndian.PutUint64(row.ClientID[8:], id)
	return row
}

func ledgerDelta(coldkey [32]byte, delta string) LedgerDelta {
	return LedgerDelta{Coldkey: coldkey, Delta: *uint256.MustFromDecimal(delta)}
}

func ledgerDeltasString(deltas []LedgerDelta) string {
	parts := make([]string, len(deltas))
	for i := range deltas {
		parts[i] = fmt.Sprintf("%x:%s", deltas[i].Coldkey, deltas[i].Delta.Dec())
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// ledgerWeights sums the eligible rows' usage per coldkey in math/big. A
// coldkey whose rows carry no usage maps to zero.
func ledgerWeights(rows []LedgerUsageRow) (map[[32]byte]*big.Int, *big.Int) {
	weights := map[[32]byte]*big.Int{}
	total := new(big.Int)
	for _, row := range rows {
		if row.Coldkey == ([32]byte{}) || row.HeadExcluded {
			continue
		}
		if weights[row.Coldkey] == nil {
			weights[row.Coldkey] = new(big.Int)
		}
		usage := new(big.Int).SetUint64(row.UsageBytes)
		weights[row.Coldkey].Add(weights[row.Coldkey], usage)
		total.Add(total, usage)
	}
	return weights, total
}

// ledgerAllocationReference restates §6.3 naively in math/big, sharing no
// code with AllocateLedgerEpoch: floor shares and remainders per coldkey, then
// the leftover rao handed out one at a time, each to the coldkey not yet
// served with the largest remainder, the smallest coldkey winning a tie.
func ledgerAllocationReference(amount *big.Int, rows []LedgerUsageRow) ([]LedgerDelta, error) {
	if amount.BitLen() > 128 {
		return nil, ErrLedgerAmountRange
	}
	weights, total := ledgerWeights(rows)
	if total.BitLen() > 64 {
		return nil, ErrLedgerUsageOverflow
	}
	if total.Sign() == 0 {
		return nil, ErrNoEligibleUsage
	}
	var coldkeys [][32]byte
	for coldkey, weight := range weights {
		if weight.Sign() > 0 {
			coldkeys = append(coldkeys, coldkey)
		}
	}
	slices.SortFunc(coldkeys, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	shares := make([]*big.Int, len(coldkeys))
	remainders := make([]*big.Int, len(coldkeys))
	left := new(big.Int).Set(amount)
	for i, coldkey := range coldkeys {
		shares[i], remainders[i] = new(big.Int).QuoRem(new(big.Int).Mul(amount, weights[coldkey]), total, new(big.Int))
		left.Sub(left, shares[i])
	}
	served := make([]bool, len(coldkeys))
	for ; left.Sign() > 0; left.Sub(left, big.NewInt(1)) {
		best := -1
		for i := range coldkeys {
			if !served[i] && (best < 0 || remainders[i].Cmp(remainders[best]) > 0) {
				best = i
			}
		}
		served[best] = true
		shares[best].Add(shares[best], big.NewInt(1))
	}
	out := make([]LedgerDelta, len(coldkeys))
	for i, coldkey := range coldkeys {
		out[i] = LedgerDelta{Coldkey: coldkey, Delta: *uint256.MustFromBig(shares[i])}
	}
	return out, nil
}

// checkLedgerAllocation allocates amount over rows and checks the outcome
// against the reference, the deltas against GA-1 (they sum to the amount) and
// GA-2 (each is within one rao of A·W_c/ΣW, as a rational), and that reversed
// rows (GA-6) and rows split in two (GA-3) give the same outcome. The amount
// must be left unchanged.
func checkLedgerAllocation(t *testing.T, amount *uint256.Int, rows []LedgerUsageRow) ([]LedgerDelta, error) {
	t.Helper()
	before := *amount
	deltas, err := AllocateLedgerEpoch(t.Context(), amount, rows)
	if *amount != before {
		t.Fatalf("amount changed from %s to %s", before.Dec(), amount.Dec())
	}
	want, wantErr := ledgerAllocationReference(amount.ToBig(), rows)
	if err != wantErr || !reflect.DeepEqual(deltas, want) {
		t.Fatalf("amount %s rows %v: got %s, %v; reference %s, %v", amount.Dec(), rows, ledgerDeltasString(deltas), err, ledgerDeltasString(want), wantErr)
	}
	if err == nil {
		weights, total := ledgerWeights(rows)
		sum := new(big.Int)
		for _, d := range deltas {
			delta := d.Delta.ToBig()
			sum.Add(sum, delta)
			exact := new(big.Rat).SetFrac(new(big.Int).Mul(amount.ToBig(), weights[d.Coldkey]), total)
			gap := new(big.Rat).Sub(new(big.Rat).SetInt(delta), exact)
			if gap.Abs(gap).Cmp(big.NewRat(1, 1)) >= 0 {
				t.Fatalf("GA-2: coldkey %x gets %s, exact share %s", d.Coldkey, delta, exact.RatString())
			}
		}
		if sum.Cmp(amount.ToBig()) != 0 {
			t.Fatalf("GA-1: deltas sum to %s, want %s", sum, amount.Dec())
		}
	}
	reversed := slices.Clone(rows)
	slices.Reverse(reversed)
	split := slices.Clone(rows)
	for i, row := range rows {
		if row.UsageBytes >= 2 {
			split[i].UsageBytes = row.UsageBytes / 2
			row.UsageBytes -= split[i].UsageBytes
			row.ClientID[0] ^= 0xff
			split = append(split, row)
		}
	}
	for _, variant := range []struct {
		id   string
		rows []LedgerUsageRow
	}{{"GA-6 reversed", reversed}, {"GA-3 split", split}} {
		got, gotErr := AllocateLedgerEpoch(t.Context(), amount, variant.rows)
		if gotErr != err || !reflect.DeepEqual(got, deltas) {
			t.Fatalf("%s rows %v: got %s, %v; want %s, %v", variant.id, variant.rows, ledgerDeltasString(got), gotErr, ledgerDeltasString(deltas), err)
		}
	}
	return deltas, err
}

// GA-1, GA-2: A = 5 over usage 4, 2, 1 has exact shares 20/7, 10/7, 5/7. The
// floors 2, 1, 0 leave R = 2 for the remainders 6, 3, 5, so the largest
// remainders, not the largest usages, get the extra rao: 3, 1, 1.
func TestLedgerAllocationLargestRemainder(t *testing.T) {
	a, b, c := ledgerKey(1, 0), ledgerKey(2, 0), ledgerKey(3, 0)
	deltas, err := checkLedgerAllocation(t, uint256.NewInt(5), []LedgerUsageRow{ledgerRow(1, c, 1), ledgerRow(2, a, 4), ledgerRow(3, b, 2)})
	want := []LedgerDelta{ledgerDelta(a, "3"), ledgerDelta(b, "1"), ledgerDelta(c, "1")}
	if err != nil || !reflect.DeepEqual(deltas, want) {
		t.Fatalf("got %s, %v; want %s", ledgerDeltasString(deltas), err, ledgerDeltasString(want))
	}
}

// GA-3: splitting providers into several rows of the same coldkey, each with
// its own client id and not adjacent, leaves every delta unchanged.
func TestLedgerAllocationSplitProvider(t *testing.T) {
	a, b, c := ledgerKey(1, 0), ledgerKey(2, 0), ledgerKey(3, 0)
	amount := uint256.NewInt(10)
	whole := []LedgerUsageRow{ledgerRow(1, a, 7), ledgerRow(2, b, 5), ledgerRow(3, c, 1)}
	split := []LedgerUsageRow{ledgerRow(1, a, 2), ledgerRow(2, b, 1), ledgerRow(4, a, 2), ledgerRow(3, c, 1), ledgerRow(5, b, 4), ledgerRow(6, a, 3)}
	// 70/13, 50/13, 10/13: floors 5, 3, 0 with remainders 5, 11, 10 and R = 2.
	want := []LedgerDelta{ledgerDelta(a, "5"), ledgerDelta(b, "4"), ledgerDelta(c, "1")}
	for _, rows := range [][]LedgerUsageRow{whole, split} {
		deltas, err := checkLedgerAllocation(t, amount, rows)
		if err != nil || !reflect.DeepEqual(deltas, want) {
			t.Fatalf("rows %v: got %s, %v; want %s", rows, ledgerDeltasString(deltas), err, ledgerDeltasString(want))
		}
	}
}

// GA-4: the leftover rao go to the largest remainders, and a tie goes to the
// ascending coldkey, compared bytewise over all 32 bytes. Rows arrive in
// descending coldkey order with ascending client ids, so neither input order,
// client id nor a comparison from the last byte would pick the same winners.
func TestLedgerAllocationTiesAscendingColdkey(t *testing.T) {
	keys := [4][32]byte{ledgerKey(1, 4), ledgerKey(2, 3), ledgerKey(3, 2), ledgerKey(4, 1)}
	top := new(uint256.Int).SubUint64(new(uint256.Int).Lsh(uint256.NewInt(1), 128), 1)
	for _, c := range []struct {
		name    string
		amount  *uint256.Int
		weights [4]uint64
		want    [4]string
	}{
		{"one rao, four-way tie", uint256.NewInt(1), [4]uint64{1, 1, 1, 1}, [4]string{"1", "0", "0", "0"}},
		{"three rao, four-way tie", uint256.NewInt(3), [4]uint64{1, 1, 1, 1}, [4]string{"1", "1", "1", "0"}},
		{"larger remainder before smaller coldkey", uint256.NewInt(1), [4]uint64{1, 1, 1, 2}, [4]string{"0", "0", "0", "1"}},
		{"tie cut behind a larger remainder", uint256.NewInt(2), [4]uint64{1, 1, 1, 2}, [4]string{"1", "0", "0", "1"}},
		{"tie at 2^128 - 1", top, [4]uint64{1, 1, 0, 0}, [4]string{"170141183460469231731687303715884105728", "170141183460469231731687303715884105727", "", ""}},
	} {
		var rows []LedgerUsageRow
		var want []LedgerDelta
		for i := range keys {
			if c.weights[3-i] != 0 {
				rows = append(rows, ledgerRow(uint64(i+1), keys[3-i], c.weights[3-i]))
			}
			if c.want[i] != "" {
				want = append(want, ledgerDelta(keys[i], c.want[i]))
			}
		}
		deltas, err := checkLedgerAllocation(t, c.amount, rows)
		if err != nil || !reflect.DeepEqual(deltas, want) {
			t.Fatalf("%s: got %s, %v; want %s", c.name, ledgerDeltasString(deltas), err, ledgerDeltasString(want))
		}
	}
	// Ties among more coldkeys than sorting leaves in insertion order, so an
	// unstable sort that dropped the tie-break would pick other winners. Of
	// 1,000 coldkeys, the even ones have usage 2 and the odd ones usage 1:
	// 700 rao leave remainders 1,400 and 700 of ΣW = 1,500, so every even
	// coldkey gets one rao and the other 200 go to the smallest odd ones.
	var rows []LedgerUsageRow
	want := make([]LedgerDelta, 1_000)
	for i := range want {
		want[i].Coldkey = ledgerKey(byte(i>>8)+1, byte(i))
		if i%2 == 0 || i < 400 {
			want[i].Delta.SetOne()
		}
		rows = append(rows, ledgerRow(uint64(1_000-i), want[i].Coldkey, uint64(2-i%2)))
	}
	slices.Reverse(rows)
	if deltas, err := checkLedgerAllocation(t, uint256.NewInt(700), rows); err != nil || !reflect.DeepEqual(deltas, want) {
		t.Fatalf("1,000 coldkeys: got %s, %v", ledgerDeltasString(deltas), err)
	}
}

// GA-5: ΣW = 0 is refused with ErrNoEligibleUsage when there are no rows, and
// when every row is ineligible or carries no usage.
func TestLedgerAllocationRefusesNoEligibleUsage(t *testing.T) {
	key := ledgerKey(1, 1)
	head := ledgerRow(2, key, 10)
	head.HeadExcluded = true
	for _, c := range []struct {
		name string
		rows []LedgerUsageRow
	}{
		{"nil", nil},
		{"empty", []LedgerUsageRow{}},
		{"zero coldkey", []LedgerUsageRow{ledgerRow(1, [32]byte{}, 10)}},
		{"head excluded", []LedgerUsageRow{head}},
		{"zero usage", []LedgerUsageRow{ledgerRow(3, key, 0)}},
	} {
		deltas, err := AllocateLedgerEpoch(t.Context(), uint256.NewInt(5), c.rows)
		if deltas != nil || err != ErrNoEligibleUsage {
			t.Fatalf("%s: got %s, %v", c.name, ledgerDeltasString(deltas), err)
		}
	}
}

// GA-5: one coldkey takes the whole amount, over several rows and beside
// ineligible rows and another coldkey's zero-usage row.
func TestLedgerAllocationOneColdkeyTakesAll(t *testing.T) {
	key, other := ledgerKey(7, 7), ledgerKey(1, 1)
	head := ledgerRow(4, other, 1<<40)
	head.HeadExcluded = true
	amount := new(uint256.Int).SubUint64(new(uint256.Int).Lsh(uint256.NewInt(1), 128), 1)
	rows := []LedgerUsageRow{ledgerRow(1, key, 3), ledgerRow(2, [32]byte{}, 1<<40), ledgerRow(3, key, 1<<62), head, ledgerRow(5, other, 0)}
	deltas, err := checkLedgerAllocation(t, amount, rows)
	if want := []LedgerDelta{{Coldkey: key, Delta: *amount}}; err != nil || !reflect.DeepEqual(deltas, want) {
		t.Fatalf("got %s, %v; want %s", ledgerDeltasString(deltas), err, ledgerDeltasString(want))
	}
}

// GA-5: A = 0 gives every coldkey with usage a zero delta.
func TestLedgerAllocationZeroAmount(t *testing.T) {
	a, b, c := ledgerKey(1, 0), ledgerKey(2, 0), ledgerKey(3, 0)
	deltas, err := checkLedgerAllocation(t, new(uint256.Int), []LedgerUsageRow{ledgerRow(1, c, 3), ledgerRow(2, a, 1), ledgerRow(3, b, 2), ledgerRow(4, a, 5)})
	if want := []LedgerDelta{ledgerDelta(a, "0"), ledgerDelta(b, "0"), ledgerDelta(c, "0")}; err != nil || !reflect.DeepEqual(deltas, want) {
		t.Fatalf("got %s, %v; want %s", ledgerDeltasString(deltas), err, ledgerDeltasString(want))
	}
}

// GA-5: A = 2^128 − 1 is allocated; 2^128 and larger amounts are refused with
// ErrLedgerAmountRange.
func TestLedgerAllocationAmountBound(t *testing.T) {
	a, b, c := ledgerKey(1, 0), ledgerKey(2, 0), ledgerKey(3, 0)
	rows := []LedgerUsageRow{ledgerRow(1, a, 1), ledgerRow(2, b, 2), ledgerRow(3, c, 4)}
	limit := new(uint256.Int).Lsh(uint256.NewInt(1), 128)
	// 2^128 − 1 = 7Q + 3: floors Q, 2Q, 4Q + 1 with remainders 3, 6, 5 and R = 2.
	deltas, err := checkLedgerAllocation(t, new(uint256.Int).SubUint64(limit, 1), rows)
	want := []LedgerDelta{
		ledgerDelta(a, "48611766702991209066196372490252601636"),
		ledgerDelta(b, "97223533405982418132392744980505203273"),
		ledgerDelta(c, "194447066811964836264785489961010406546"),
	}
	if err != nil || !reflect.DeepEqual(deltas, want) {
		t.Fatalf("got %s, %v; want %s", ledgerDeltasString(deltas), err, ledgerDeltasString(want))
	}
	for _, amount := range []*uint256.Int{limit, new(uint256.Int).Lsh(uint256.NewInt(1), 192), new(uint256.Int).SetAllOne()} {
		if deltas, err := AllocateLedgerEpoch(t.Context(), amount, rows); deltas != nil || err != ErrLedgerAmountRange {
			t.Fatalf("amount %s: got %s, %v", amount.Dec(), ledgerDeltasString(deltas), err)
		}
	}
}

// GA-5: eligible usage summing to 2^64 − 1 is allocated, and summing to 2^64
// is refused with ErrLedgerUsageOverflow, across coldkeys or within one.
// Ineligible rows' usage does not count toward the bound.
func TestLedgerAllocationUsageBound(t *testing.T) {
	a, b := ledgerKey(1, 0), ledgerKey(2, 0)
	head := ledgerRow(3, b, math.MaxUint64)
	head.HeadExcluded = true
	amount := new(uint256.Int).SubUint64(new(uint256.Int).Lsh(uint256.NewInt(1), 128), 1)
	rows := []LedgerUsageRow{ledgerRow(1, a, 1<<63), ledgerRow(2, b, 1<<63-1), head, ledgerRow(4, [32]byte{}, math.MaxUint64)}
	// ΣW = 2^64 − 1 divides A = (2^64 − 1)(2^64 + 1) exactly.
	deltas, err := checkLedgerAllocation(t, amount, rows)
	want := []LedgerDelta{ledgerDelta(a, "170141183460469231740910675752738881536"), ledgerDelta(b, "170141183460469231722463931679029329919")}
	if err != nil || !reflect.DeepEqual(deltas, want) {
		t.Fatalf("got %s, %v; want %s", ledgerDeltasString(deltas), err, ledgerDeltasString(want))
	}
	for _, rows := range [][]LedgerUsageRow{
		{ledgerRow(1, a, 1<<63), ledgerRow(2, b, 1<<63)},
		{ledgerRow(1, a, 1<<63), ledgerRow(2, a, 1<<63)},
	} {
		if deltas, err := AllocateLedgerEpoch(t.Context(), amount, rows); deltas != nil || err != ErrLedgerUsageOverflow {
			t.Fatalf("rows %v: got %s, %v", rows, ledgerDeltasString(deltas), err)
		}
	}
}

// GA-6: every permutation of the rows gives the same result.
func TestLedgerAllocationInputOrder(t *testing.T) {
	a, b, c := ledgerKey(1, 0), ledgerKey(2, 0), ledgerKey(3, 0)
	head := ledgerRow(5, c, 9)
	head.HeadExcluded = true
	rows := []LedgerUsageRow{ledgerRow(1, a, 2), ledgerRow(2, b, 3), ledgerRow(3, c, 3), ledgerRow(4, a, 1), head, ledgerRow(6, [32]byte{}, 4)}
	amount := uint256.NewInt(1_000_000_007)
	want, err := checkLedgerAllocation(t, amount, rows)
	if err != nil {
		t.Fatal(err)
	}
	permutations := 0
	var permute func(int)
	permute = func(k int) {
		if k == len(rows) {
			permutations++
			if deltas, err := AllocateLedgerEpoch(t.Context(), amount, rows); err != nil || !reflect.DeepEqual(deltas, want) {
				t.Fatalf("rows %v: got %s, %v; want %s", rows, ledgerDeltasString(deltas), err, ledgerDeltasString(want))
			}
			return
		}
		for i := k; i < len(rows); i++ {
			rows[k], rows[i] = rows[i], rows[k]
			permute(k + 1)
			rows[k], rows[i] = rows[i], rows[k]
		}
	}
	permute(0)
	if permutations != 720 {
		t.Fatalf("checked %d permutations, want 720", permutations)
	}
}

// GA-1, GA-2, GA-3, GA-6 and agreement with the reference on seeded random
// epochs: up to 48 coldkeys, tiny usage for exact ties, usage near 2^64 for
// the overflow bound, amounts from 0 to 2^128 − 1 and sometimes 2^128.
func TestLedgerAllocationRandomized(t *testing.T) {
	r := rand.New(rand.NewPCG(0x5e771e, 2))
	limit := new(uint256.Int).Lsh(uint256.NewInt(1), 128)
	for range 5_000 {
		keys := make([][32]byte, 1+r.IntN(48))
		for i := range keys {
			keys[i] = ledgerKey(byte(r.IntN(3)), byte(r.IntN(256)))
		}
		bits := 1 + r.IntN(64)
		rows := make([]LedgerUsageRow, r.IntN(64))
		for i := range rows {
			rows[i] = ledgerRow(uint64(i+1), keys[r.IntN(len(keys))], r.Uint64()>>(64-bits))
			rows[i].HeadExcluded = r.IntN(8) == 0
			if r.IntN(10) == 0 {
				rows[i].Coldkey = [32]byte{}
			}
		}
		amount := new(uint256.Int).SetUint64(r.Uint64())
		amount.Lsh(amount, 64).Or(amount, uint256.NewInt(r.Uint64())).Rsh(amount, uint(r.IntN(129)))
		switch r.IntN(32) {
		case 0:
			amount.Set(limit)
		case 1:
			amount.SubUint64(limit, 1)
		}
		checkLedgerAllocation(t, amount, rows)
	}
}

// GA-1, GA-2, GA-3, GA-6 and agreement with the reference, fuzzed. The amount
// is big-endian, up to 32 bytes. Every 10 bytes of data are a row: a coldkey
// selector s (0 is the zero coldkey; the key has first byte s mod 16 and last
// byte s / 16, so key order is not selector order), a flag byte (bit 0 is
// head-excluded) and 8 bytes of big-endian usage.
func FuzzLedgerAllocation(f *testing.F) {
	row := func(selector, flags byte, usage uint64) []byte {
		return binary.BigEndian.AppendUint64([]byte{selector, flags}, usage)
	}
	top := bytes.Repeat([]byte{0xff}, 16)
	f.Add([]byte{100}, slices.Concat(row(1, 0, 1), row(2, 0, 1), row(3, 0, 1)))
	f.Add([]byte{0x03, 0xe8}, slices.Concat(row(0x21, 0, 3), row(0x12, 0, 3), row(0x21, 0, 4), row(0x12, 1, 50)))
	f.Add(top, slices.Concat(row(1, 0, 1<<63), row(2, 0, 1<<63-1)))
	f.Add([]byte{1}, slices.Concat(row(1, 0, 1<<63), row(2, 0, 1<<63)))
	f.Add(append([]byte{1}, make([]byte, 16)...), row(1, 0, 1))
	f.Add([]byte{}, slices.Concat(row(1, 0, 5), row(2, 0, 7)))
	f.Add([]byte{7}, []byte{})
	f.Add([]byte{7}, slices.Concat(row(0, 0, 9), row(1, 1, 9), row(2, 0, 0)))
	var tie []byte
	for s := range byte(16) {
		tie = append(tie, row(s+1, 0, 1)...)
	}
	f.Add([]byte{7}, tie)
	f.Fuzz(func(t *testing.T, amount, data []byte) {
		if len(amount) > 32 {
			t.Skip()
		}
		var rows []LedgerUsageRow
		for ; len(data) >= 10; data = data[10:] {
			row := ledgerRow(uint64(len(rows)+1), ledgerKey(data[0]&0x0f, data[0]>>4), binary.BigEndian.Uint64(data[2:10]))
			row.HeadExcluded = data[1]&1 == 1
			rows = append(rows, row)
		}
		checkLedgerAllocation(t, new(uint256.Int).SetBytes(amount), rows)
	})
}

// §15 rules (every refusal has its test): the nil context is refused, and a
// cancelled context is returned, with no result, from each place the
// allocation checks it: the first row, row 65,536, the first coldkey and
// coldkey 65,536.
func TestLedgerAllocationContext(t *testing.T) {
	rows := []LedgerUsageRow{ledgerRow(1, ledgerKey(1, 0), 1)}
	if deltas, err := AllocateLedgerEpoch(nil, uint256.NewInt(1), rows); deltas != nil || err == nil || err.Error() != "allocation requires an owner context" {
		t.Fatalf("nil context: got %s, %v", ledgerDeltasString(deltas), err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if deltas, err := AllocateLedgerEpoch(cancelled, uint256.NewInt(1), rows); deltas != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: got %s, %v", ledgerDeltasString(deltas), err)
	}
	// Row 65,536 would overflow the usage if its cancellation check were missed.
	manyRows := make([]LedgerUsageRow, 1<<16+1)
	for i := range manyRows {
		manyRows[i] = ledgerRow(uint64(i+1), ledgerKey(1, 0), 1)
	}
	manyRows[1<<16].UsageBytes = math.MaxUint64
	manyColdkeys := make([]LedgerUsageRow, 1<<16+1)
	for i := range manyColdkeys {
		manyColdkeys[i] = ledgerRow(uint64(i+1), ledgerKey(1, 0), 1)
		binary.BigEndian.PutUint32(manyColdkeys[i].Coldkey[27:31], uint32(i))
	}
	for _, c := range []struct {
		name string
		rows []LedgerUsageRow
		stop int32
	}{
		{"row 65536", manyRows, 2},
		{"first coldkey", rows, 2},
		{"coldkey 65536", manyColdkeys, 4},
	} {
		ctx := allocationCancelAt(t, c.stop)
		if deltas, err := AllocateLedgerEpoch(ctx, uint256.NewInt(1), c.rows); deltas != nil || !errors.Is(err, context.Canceled) || ctx.calls.Load() != c.stop {
			t.Fatalf("%s: got %s, %v after %d checks", c.name, ledgerDeltasString(deltas), err, ctx.calls.Load())
		}
	}
}
