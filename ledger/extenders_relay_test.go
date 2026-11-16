package ledger

// The extender pool's relay half (WHITEPAPER.md §8.6.2, owner 2026-11-16): half
// of Z' is split over the eligible extenders by the bytes each relayed in the
// epoch, the other half by unit as before. A fleet spread over many blocks that
// relays nothing takes the unit half only, and an extender that carries the
// clients takes most of the relay half. The numbers are exact, so a failure
// names the rule that broke.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/urfoundation/sn/protocol"
)

// A relay row of client's bytes.
func relayRow(client int, relayBytes uint64) RelayRow {
	return RelayRow{ClientID: id(client), RelayBytes: relayBytes}
}

// The split's shares by client, failing on rows out of client order, a zero
// or head-excluded row, a share paid to another coldkey than the census row's,
// or rows that do not sum to Z'.
func relaySplitShares(t *testing.T, split *ExtenderSplit, census []CensusRow) map[int]uint64 {
	t.Helper()
	shares := map[int]uint64{}
	var sum uint64
	for i, r := range split.Rows {
		if i > 0 && bytes.Compare(split.Rows[i-1].ClientID[:], r.ClientID[:]) >= 0 {
			t.Fatalf("Z' = %d: share rows out of client order", split.Weight)
		}
		if r.UsageBytes == 0 || r.HeadExcluded {
			t.Fatalf("Z' = %d: share row %+v", split.Weight, r)
		}
		index := slices.IndexFunc(census, func(cr CensusRow) bool { return cr.ClientID == r.ClientID })
		if index < 0 || census[index].Coldkey != r.Coldkey {
			t.Fatalf("Z' = %d: share row %+v is not a census client's", split.Weight, r)
		}
		shares[int(binary.BigEndian.Uint64(r.ClientID[8:]))] = r.UsageBytes
		sum += r.UsageBytes
	}
	if sum != split.Weight || split.UnitWeight+split.RelayWeight != split.Weight {
		t.Fatalf("Z' = %d: rows sum to %d, unit %d + relay %d", split.Weight, sum, split.UnitWeight, split.RelayWeight)
	}
	return shares
}

// The relay half: floor(Z'/2) over the eligible extenders in proportion to
// their relay bytes, the leftover bytes one each to the largest remainders,
// the lower client id first among equal ones; the unit part, the rest, by
// unit as before. Relay bytes of an ineligible census row count for nothing,
// and with no eligible extender that relayed all of Z' is split by unit.
func TestExtenderPoolRelaySplit(t *testing.T) {
	providers := []protocol.LedgerUsageRow{row(1, 1, 1<<50, false)}
	v6 := active(5, 35, 8, 9)
	v6.Family = UnitIPv6
	// eligible: 31, 33 and 36 in unit 4, 32 in unit 2, 35 in unit 9; 34 is
	// gated and 38 head-bound, so neither is eligible
	census := []CensusRow{
		active(1, 31, 3, 4),
		active(2, 32, 4, 2),
		active(3, 33, 5, 4),
		censusRow(4, 34, 6, 1, CensusActive|CensusGated),
		v6,
		active(6, 36, 7, 4),
		censusRow(8, 38, 3, 2, CensusActive|CensusHeadBound),
	}
	for _, c := range []struct {
		name        string
		weight      uint64
		relay       []RelayRow
		relayWeight uint64
		relayBytes  uint64
		relaying    uint64
		want        map[int]uint64
	}{
		{
			// unit part 501: units 2, 4, 9 take 167 each, unit 4's as 56,
			// 56, 55; relay part 500 over R = 400: 125 and 375, nothing left
			name:   "exact proportions",
			weight: 1001,
			relay:  []RelayRow{relayRow(31, 100), relayRow(33, 300), relayRow(34, 500), relayRow(38, 1000)},
			// 34 and 38 relayed, but they are not eligible
			relayWeight: 500, relayBytes: 400, relaying: 2,
			want: map[int]uint64{31: 56 + 125, 32: 167, 33: 56 + 375, 35: 167, 36: 55},
		},
		{
			// relay part 500 over three equal relayers: 166 each, 2 left,
			// remainders equal, so the two lowest client ids take them; unit
			// part 500: 167, 167, 166, unit 4's 167 as 56, 56, 55
			name:        "equal remainders go to the lower client ids",
			weight:      1000,
			relay:       []RelayRow{relayRow(31, 1), relayRow(33, 1), relayRow(36, 1)},
			relayWeight: 500, relayBytes: 3, relaying: 3,
			want: map[int]uint64{31: 56 + 167, 32: 167, 33: 56 + 167, 35: 166, 36: 55 + 166},
		},
		{
			// relay part 500 over 1 and 2: 166 rem 2 and 333 rem 1, the one
			// byte left to the larger remainder, the lower relayer's
			name:        "the larger remainder first",
			weight:      1000,
			relay:       []RelayRow{relayRow(31, 1), relayRow(33, 2)},
			relayWeight: 500, relayBytes: 3, relaying: 2,
			want: map[int]uint64{31: 56 + 167, 32: 167, 33: 56 + 333, 35: 166, 36: 55},
		},
		{
			// only ineligible extenders relayed: all of Z' by unit
			name:        "no eligible extender relayed",
			weight:      1001,
			relay:       []RelayRow{relayRow(34, 500), relayRow(38, 1000)},
			relayWeight: 0, relayBytes: 0, relaying: 0,
			want: map[int]uint64{31: 112, 32: 334, 33: 111, 35: 333, 36: 111},
		},
		{
			name:        "no relay rows",
			weight:      1001,
			relayWeight: 0, relayBytes: 0, relaying: 0,
			want: map[int]uint64{31: 112, 32: 334, 33: 111, 35: 333, 36: 111},
		},
		{
			// floor(1/2) is 0, so the one byte is the unit part's
			name:        "one byte",
			weight:      1,
			relay:       []RelayRow{relayRow(33, 7)},
			relayWeight: 0, relayBytes: 7, relaying: 1,
			want: map[int]uint64{32: 1},
		},
		{
			name:        "relay bytes near 2^64 split exactly",
			weight:      3_000_000_000_001,
			relay:       []RelayRow{relayRow(31, 1<<62), relayRow(36, 3<<62-1)},
			relayWeight: 1_500_000_000_000, relayBytes: 1<<64 - 1, relaying: 2,
			// unit part 1,500,000,000,001: unit 2 takes the odd byte, unit 4's
			// 500e9 splits 166,666,666,667, 166,666,666,667, 166,666,666,666;
			// relay part 1.5e12 over R = 2^64-1: 31 takes 375e9 exactly (its
			// remainder 375e9), 36 takes 1,124,999,999,999 and, with the
			// larger remainder, the one byte left over
			want: map[int]uint64{31: 166_666_666_667 + 375_000_000_000, 32: 500_000_000_001, 33: 166_666_666_667, 35: 500_000_000_000, 36: 166_666_666_666 + 1_125_000_000_000},
		},
	} {
		split, err := SplitExtenderPool(c.weight, providers, census, c.relay)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if split.Weight != c.weight || split.RelayWeight != c.relayWeight || split.UnitWeight != c.weight-c.relayWeight ||
			split.RelayBytes != c.relayBytes || split.Relaying != c.relaying || split.Units != 3 || split.Eligible != 5 {
			t.Fatalf("%s: weight %d (unit %d, relay %d), R %d from %d relaying, M %d, N %d", c.name, split.Weight, split.UnitWeight, split.RelayWeight, split.RelayBytes, split.Relaying, split.Units, split.Eligible)
		}
		if got := relaySplitShares(t, split, census); !maps.Equal(got, c.want) {
			t.Fatalf("%s: shares %v, want %v", c.name, got, c.want)
		}
	}
}

// What the relay half is for (§8.6.3 S4): a fleet that holds many units and
// relays nothing takes the unit half only, and the extender that carries the
// clients takes most of the relay half. Split by unit alone, the relaying
// extender in one of ten units took a tenth of the pool; split half and half,
// it takes more than half.
func TestExtenderPoolRelayHalfOutweighsUnits(t *testing.T) {
	providers := []protocol.LedgerUsageRow{row(1, 1, 1<<50, false)}
	// client 40 relays, alone in unit 40; a fleet of 41 to 49 holds units 41
	// to 49 and relays nothing
	census := []CensusRow{active(1, 40, 2, 40)}
	for n := 41; n <= 49; n++ {
		census = append(census, active(n-39, n, 3, n))
	}
	byUnit, err := SplitExtenderPool(1000, providers, census, nil)
	if err != nil {
		t.Fatal(err)
	}
	halfAndHalf, err := SplitExtenderPool(1000, providers, census, []RelayRow{relayRow(40, 1)})
	if err != nil {
		t.Fatal(err)
	}
	fleetShare := func(split *ExtenderSplit) (relayer uint64, fleet uint64) {
		for _, r := range split.Rows {
			if r.ClientID == id(40) {
				relayer += r.UsageBytes
			} else {
				fleet += r.UsageBytes
			}
		}
		return
	}
	if relayer, fleet := fleetShare(byUnit); relayer != 100 || fleet != 900 {
		t.Fatalf("by unit: the relayer takes %d and the fleet %d, want 100 and 900", relayer, fleet)
	}
	if relayer, fleet := fleetShare(halfAndHalf); relayer != 550 || fleet != 450 {
		t.Fatalf("half and half: the relayer takes %d and the fleet %d, want 550 and 450", relayer, fleet)
	}
}

// The relay rules a validator can check: rows sorted and unique by client id,
// with a nonzero client id and positive relay bytes, each of a census client.
// A relay list round-trips through its chunks.
func TestCheckRelayRows(t *testing.T) {
	census := []CensusRow{active(1, 21, 9, 1), censusRow(2, 22, 9, 0, 0), active(3, 23, 8, 2)}
	valid := []RelayRow{relayRow(21, 5), relayRow(22, 1), relayRow(23, 1<<64-1)}
	if err := CheckRelayRows(valid, census); err != nil {
		t.Fatalf("valid relay rows: %v", err)
	}
	if err := CheckRelayRows(nil, census); err != nil {
		t.Fatalf("no relay rows: %v", err)
	}
	for name, rows := range map[string][]RelayRow{
		"unsorted":                  {relayRow(22, 1), relayRow(21, 5)},
		"a client twice":            {relayRow(21, 5), relayRow(21, 6)},
		"zero relay bytes":          {relayRow(21, 0)},
		"a zero client id":          {{RelayBytes: 1}},
		"a client the census lacks": {relayRow(21, 5), relayRow(24, 1)},
	} {
		if err := CheckRelayRows(rows, census); !errors.Is(err, ErrRelayRows) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	chunks := EncodeRelayChunks(valid)
	decoded, err := DecodeRelayChunks(chunks)
	if err != nil || !slices.Equal(decoded, valid) || len(chunks) != 1 || len(chunks[0]) != 3*RelayRowBytes {
		t.Fatalf("round trip: %v %+v", err, decoded)
	}
	if _, err := DecodeRelayChunks([][]byte{chunks[0][:RelayRowBytes+1]}); !errors.Is(err, ErrChunkLength) {
		t.Fatalf("a short row: %v", err)
	}
	if len(EncodeRelayChunks(nil)) != 0 {
		t.Fatal("an empty relay list has chunks")
	}
	// A build refuses relay rows the rules refuse.
	f := firstRoot(t, epoch10Rows(), 1_000)
	rows := []protocol.LedgerUsageRow{row(1, 1, 1_000, false)}
	if _, err := buildOver(f, []EpochInput{{Epoch: 11, Rows: rows, Extenders: &ExtenderInput{Bytes: 600, Census: census, Relay: []RelayRow{relayRow(24, 1)}}}}, funded(10, f.Commit.Total.Uint64(), 11, f.Commit.Total.Uint64()+1_000_000)); !errors.Is(err, ErrRelayRows) {
		t.Fatalf("built relay rows of a client the census lacks: %v", err)
	}
}
