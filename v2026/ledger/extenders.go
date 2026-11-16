package ledger

// extenders.go — an epoch's extender component (WHITEPAPER.md §8.6.2,
// §8.6.4), for the epochs the extender pool amendment covers (from E_x, the
// first epoch of its signed amendment entry). It holds:
//
//   - Z, the epoch's extender pool bytes: the funded intra-platform and
//     intra-network shares of the contracts that closed in its earning
//     window, which the operator's epoch reader takes out of the provider
//     rows;
//   - the census: one row per extender of the operator known in the epoch,
//     eligible or not, captured at end(k) and operator-attested;
//   - the share rows: Z' = min(Z, ζ·W_k) with ζ = 1, W_k the epoch's
//     eligible provider usage, split evenly over the units that hold an
//     eligible extender, by unit hash, then evenly over the eligible
//     extenders of each unit, by client id. They are 57-byte rows like
//     provider rows, and enter the epoch's allocation beside them.
//
// With no eligible extender Z is dropped: it enters no weight, and the epoch
// allocates exactly as it would without the amendment.

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	"github.com/urfoundation/sn/v2026/protocol"
)

const (
	// The header format of a root that covers at least one epoch with an
	// extender component. A root that covers none keeps Schema, byte for byte.
	SchemaV3 = "urnetwork-payout-ledger-v3"
	// The size of a census row, big-endian: extender_id(16) ‖ client_id(16) ‖
	// coldkey(32) ‖ unit(32) ‖ unit_family(1) ‖ flags(1).
	CensusRowBytes = 98
)

// A census row's flags (WHITEPAPER.md §8.6.2), one bit each.
type CensusFlags uint8

const (
	// E1: at end(k) the record is active and unrevoked, and at least one
	// of its addresses is active.
	CensusActive CensusFlags = 1 << iota
	// E5: the record is in the gated tier, the operator's own durable
	// fleet (durable_network_ids).
	CensusGated
	// E5: the record is on a canary channel.
	CensusCanary
	// E5: the record's network is one the operator declares it controls.
	CensusDeclared
	// Marks a record revoked by end(k).
	CensusRevoked
	// E3: the record shares a provider client, an IPv4 address or an IPv6
	// /64 with an active record of a lower (client id, extender id),
	// which is the one that counts.
	CensusDuplicate
	// E4's second half: the record's provider client has an active fleet
	// binding at either of the epoch's boundaries.
	CensusHeadBound

	censusFlagsKnown = CensusActive | CensusGated | CensusCanary | CensusDeclared | CensusRevoked | CensusDuplicate | CensusHeadBound
)

// The unit families of a census row.
const (
	// The family of a row without an active address, whose unit is zero.
	UnitNone uint8 = 0
	// A unit keyed from an active IPv4 address's /29.
	UnitIPv4 uint8 = 4
	// A unit keyed from an active IPv6 address's /48, for a record
	// without an active IPv4 address.
	UnitIPv6 uint8 = 6
)

// One extender record of the operator in an epoch's census.
type CensusRow struct {
	ExtenderID [16]byte
	// The record's provider client at end(k).
	ClientID [16]byte
	// The provider client's payout coldkey under the wallet selection
	// provider rows use, zero without one (E4).
	Coldkey [32]byte
	// The keyed /29 or /48 prefix hash of the record's active address,
	// the unit of §8.4 under the same key and granularity; zero without
	// an active address.
	Unit   [32]byte
	Family uint8
	Flags  CensusFlags
}

// Reports E1 and E3–E5: active, with no other flag set, and a payout
// coldkey.
func (self *CensusRow) Eligible() bool {
	return self.Flags == CensusActive && self.Coldkey != ([32]byte{})
}

var (
	// Returned for a census row with an unknown flag bit or a unit
	// family other than 0, 4 and 6.
	ErrCensusEncoding = errors.New("ledger: census row has unknown flags or unit family")
	// Returned for a census row with a zero extender id or client id.
	ErrCensusZeroId = errors.New("ledger: census row has a zero extender id or client id")
	// Returned for census rows out of (client id, extender id) order.
	ErrCensusUnsorted = errors.New("ledger: census rows are not sorted by client id and extender id")
	// Returned for two census rows of one extender.
	ErrCensusDuplicate = errors.New("ledger: census rows repeat an extender id")
	// Returned for a census row whose unit does not match its activity,
	// an active row with no unit or an inactive one with a unit, and for
	// a unit hash given two families.
	ErrCensusUnit = errors.New("ledger: census row unit does not match its activity, or a unit has two families")
	// Returned for a census row that is both active and revoked, or a
	// duplicate without being active.
	ErrCensusFlags = errors.New("ledger: census row flags contradict each other")
	// Returned for a provider client whose active census rows count more
	// than one record (E3), or whose rows name more than one coldkey.
	ErrCensusClient = errors.New("ledger: a provider client's census rows count twice or name two coldkeys")
	// Returned when an epoch's eligible usage with its extender share
	// rows reaches 2^64.
	ErrExtenderOverflow = errors.New("ledger: eligible usage with the extender share rows reaches 2^64")
)

// Encodes census rows, in the order given, as a chunk list.
func EncodeCensusChunks(rows []CensusRow) [][]byte {
	return encodeChunks(len(rows), CensusRowBytes, func(b []byte, i int) []byte {
		row := &rows[i]
		b = append(b, row.ExtenderID[:]...)
		b = append(b, row.ClientID[:]...)
		b = append(b, row.Coldkey[:]...)
		b = append(b, row.Unit[:]...)
		return append(b, row.Family, byte(row.Flags))
	})
}

// Decodes a chunk list of census rows, checking its shape. It does
// not check the census rules; CheckCensusRows does.
func DecodeCensusChunks(chunks [][]byte) ([]CensusRow, error) {
	var rows []CensusRow
	for j, chunk := range chunks {
		if err := checkChunk(len(chunk), CensusRowBytes, j, len(chunks)); err != nil {
			return nil, err
		}
		rows = decodeCensusRows(rows, chunk)
	}
	return rows, nil
}

// Appends the census rows of one chunk, whose shape has been checked, to rows.
func decodeCensusRows(rows []CensusRow, chunk []byte) []CensusRow {
	for offset := 0; offset < len(chunk); offset += CensusRowBytes {
		b := chunk[offset : offset+CensusRowBytes]
		var row CensusRow
		copy(row.ExtenderID[:], b[:16])
		copy(row.ClientID[:], b[16:32])
		copy(row.Coldkey[:], b[32:64])
		copy(row.Unit[:], b[64:96])
		row.Family, row.Flags = b[96], CensusFlags(b[97])
		rows = append(rows, row)
	}
	return rows
}

// Checks the census rules a verifier can check without addresses
// (§8.6.4 check 2):
//
//   - every row has known flags, a unit family of 0, 4 or 6, and nonzero
//     extender and client ids;
//   - the rows are sorted by (client id, extender id), and no extender id
//     repeats;
//   - an active row has a unit and a family of 4 or 6, and an inactive one
//     has neither; one unit hash never has two families;
//   - no row is both active and revoked, and only an active row is a
//     duplicate;
//   - of a provider client's active rows only the first, by extender id, can
//     count (E3: one record per provider client, the lowest), and all of a
//     client's rows name one coldkey.
//
// Which records share an address (the rest of E3), which are active, gated,
// canaries or declared, and the wallets are the operator's attestation.
func CheckCensusRows(rows []CensusRow) error {
	seenExtenderIds := make(map[[16]byte]bool, len(rows))
	unitFamilies := map[[32]byte]uint8{}
	for i := range rows {
		row := &rows[i]
		if row.Flags&^censusFlagsKnown != 0 || (row.Family != UnitNone && row.Family != UnitIPv4 && row.Family != UnitIPv6) {
			return fmt.Errorf("%w: row %d", ErrCensusEncoding, i)
		}
		if row.ExtenderID == ([16]byte{}) || row.ClientID == ([16]byte{}) {
			return fmt.Errorf("%w: row %d", ErrCensusZeroId, i)
		}
		if seenExtenderIds[row.ExtenderID] {
			return fmt.Errorf("%w: row %d", ErrCensusDuplicate, i)
		}
		seenExtenderIds[row.ExtenderID] = true
		active := row.Flags&CensusActive != 0
		if active != (row.Family != UnitNone) || active != (row.Unit != [32]byte{}) {
			return fmt.Errorf("%w: row %d", ErrCensusUnit, i)
		}
		if active {
			if family, ok := unitFamilies[row.Unit]; ok && family != row.Family {
				return fmt.Errorf("%w: row %d", ErrCensusUnit, i)
			}
			unitFamilies[row.Unit] = row.Family
		}
		if (active && row.Flags&CensusRevoked != 0) || (!active && row.Flags&CensusDuplicate != 0) {
			return fmt.Errorf("%w: row %d", ErrCensusFlags, i)
		}
		if i == 0 {
			continue
		}
		previous := &rows[i-1]
		switch c := bytes.Compare(previous.ClientID[:], row.ClientID[:]); {
		case c > 0, c == 0 && bytes.Compare(previous.ExtenderID[:], row.ExtenderID[:]) >= 0:
			return fmt.Errorf("%w: row %d", ErrCensusUnsorted, i)
		case c == 0 && previous.Coldkey != row.Coldkey:
			return fmt.Errorf("%w: row %d", ErrCensusClient, i)
		}
	}
	// A client's rows are adjacent, so its first active row is the first
	// active row since its client changed.
	var clientId [16]byte
	seenActive := false
	for i := range rows {
		row := &rows[i]
		if i == 0 || row.ClientID != clientId {
			clientId, seenActive = row.ClientID, false
		}
		if row.Flags&CensusActive == 0 {
			continue
		}
		if seenActive && row.Flags&CensusDuplicate == 0 {
			return fmt.Errorf("%w: row %d", ErrCensusClient, i)
		}
		seenActive = true
	}
	return nil
}

// The even split of an epoch's extender pool (§8.6.2).
type ExtenderSplit struct {
	// Z, the epoch's extender pool bytes.
	Bytes uint64
	// Z', what Z enters the allocation as: min(Z, W_k) with W_k the epoch's
	// eligible provider usage (ζ = 1), and 0 when no extender is eligible.
	// The share rows sum to it.
	Weight uint64
	// N, the census rows that pass E1 and E3–E5, and M, the units that hold
	// them.
	Eligible uint64
	Units    uint64
	// The share rows: one per eligible extender with a positive share, sorted
	// by client id, each with its census row's coldkey and head_excluded 0.
	Rows []protocol.LedgerUsageRow
}

// W_k: the usage of the provider rows that are eligible for the
// allocation, with a coldkey and not head-excluded. The rows' total is
// below 2^64 once checkProviderRows passed, so it cannot overflow.
func EligibleUsage(rows []protocol.LedgerUsageRow) uint64 {
	var total uint64
	for i := range rows {
		if rows[i].Coldkey != ([32]byte{}) && !rows[i].HeadExcluded {
			total += rows[i].UsageBytes
		}
	}
	return total
}

// Splits an epoch's extender pool bytes z (§8.6.2). The weight Z' is z capped
// at the eligible usage of the epoch's provider rows (ζ = 1), so extenders
// take at most half of the epoch's amount. The eligible census rows group
// into units by unit hash; with M units, the j-th by unit hash takes
// floor(Z'/M), plus one byte when j < Z' mod M, and within it the i-th of its
// m eligible extenders by client id takes floor(z_j/m), plus one byte when i
// < z_j mod m. A share of zero has no row. With no eligible extender Z is
// dropped: Weight is 0 and there are no rows.
//
// census must pass CheckCensusRows, which makes each eligible row's client id
// unique, and providers checkProviderRows.
func SplitExtenderPool(z uint64, providers []protocol.LedgerUsageRow, census []CensusRow) (*ExtenderSplit, error) {
	// the share of index in an even split of total over count, the remainder
	// going to the lowest indexes: the rule a contract's bytes split over its
	// hops by (the server's evenContractPayoutShare)
	evenShare := func(total uint64, index, count uint64) uint64 {
		share := total / count
		if index < total%count {
			share++
		}
		return share
	}
	split := &ExtenderSplit{Bytes: z, Rows: []protocol.LedgerUsageRow{}}
	unitMemberIndexes := map[[32]byte][]int{}
	for i := range census {
		if census[i].Eligible() {
			unitMemberIndexes[census[i].Unit] = append(unitMemberIndexes[census[i].Unit], i)
			split.Eligible++
		}
	}
	if split.Eligible == 0 {
		return split, nil
	}
	units := make([][32]byte, 0, len(unitMemberIndexes))
	for unit := range unitMemberIndexes {
		units = append(units, unit)
	}
	slices.SortFunc(units, func(a, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	split.Units = uint64(len(units))

	w := EligibleUsage(providers)
	split.Weight = min(z, w)
	if w+split.Weight < w {
		return nil, fmt.Errorf("%w: %d bytes of eligible usage and %d of extender weight", ErrExtenderOverflow, w, split.Weight)
	}
	for j, unit := range units {
		zj := evenShare(split.Weight, uint64(j), split.Units)
		// census order is client id order, so a unit's members are too
		for i, index := range unitMemberIndexes[unit] {
			share := evenShare(zj, uint64(i), uint64(len(unitMemberIndexes[unit])))
			if share == 0 {
				continue
			}
			split.Rows = append(split.Rows, protocol.LedgerUsageRow{ClientID: census[index].ClientID, Coldkey: census[index].Coldkey, UsageBytes: share})
		}
	}
	slices.SortFunc(split.Rows, func(a, b protocol.LedgerUsageRow) int { return bytes.Compare(a.ClientID[:], b.ClientID[:]) })
	return split, nil
}

// Builds a covered epoch's extender component into objects: its census
// chunks, held to the chunks it was first built with, and the share rows
// of its split.
func buildExtenders(input *EpochInput, objects Objects) (*ExtenderAllocation, error) {
	extenders := input.Extenders
	if input.Null {
		return nil, fmt.Errorf("%w: epoch %d has an extender pool", ErrEpochInput, input.Epoch)
	}
	if err := CheckCensusRows(extenders.Census); err != nil {
		return nil, fmt.Errorf("epoch %d census: %w", input.Epoch, err)
	}
	census := putChunks(EncodeCensusChunks(extenders.Census), objects)
	if extenders.Chunks != nil && !slices.Equal(extenders.Chunks, census) {
		return nil, fmt.Errorf("%w: epoch %d census", ErrFixedInputs, input.Epoch)
	}
	split, err := SplitExtenderPool(extenders.Bytes, input.Rows, extenders.Census)
	if err != nil {
		return nil, fmt.Errorf("epoch %d: %w", input.Epoch, err)
	}
	return &ExtenderAllocation{ExtenderSplit: *split, Census: census, Chunks: putChunks(EncodeProviderChunks(split.Rows), objects)}, nil
}

// Adds chunks to objects and returns their references.
func putChunks(chunks [][]byte, objects Objects) []Ref {
	refs := make([]Ref, len(chunks))
	for j, chunk := range chunks {
		refs[j] = RefOf(chunk)
		objects[refs[j]] = chunk
	}
	return refs
}

// The rows an epoch with an extender component allocates by: its provider
// rows and its share rows. AllocateLedgerEpoch sums them by coldkey, so a
// coldkey that holds both adds them, and their order does not matter.
func allocationRows(providers, shares []protocol.LedgerUsageRow) []protocol.LedgerUsageRow {
	if len(shares) == 0 {
		return providers
	}
	return append(slices.Clip(providers), shares...)
}
