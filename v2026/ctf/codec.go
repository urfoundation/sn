package ctf

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/ledger"
)

const (
	// LedgerSchema names the CTF ledger header format (§5).
	LedgerSchema = "urnetwork-ctf-ledger-v1"
	// AwardRowBytes is the size of an award row: role(1) ‖ country(2) ‖
	// rank(1) ‖ unit(32) ‖ client_id(16) ‖ coldkey(32) ‖ slot_rao(16) ‖
	// pot_rao(16) ‖ deferred(1).
	AwardRowBytes = 117
	// PotRowBytes is the size of a pot row: country(2) ‖ role(1) ‖ units(4) ‖
	// winners(1) ‖ pot_in(16) ‖ pot_out(16).
	PotRowBytes = 40
	// PendingRowBytes is the size of a pending row: award epoch(8) ‖ role(1)
	// ‖ country(2) ‖ rank(1) ‖ coldkey(32) ‖ amount(16).
	PendingRowBytes = 60
)

var (
	// ErrAwardRows is returned for award rows that are not whole rows of a
	// role, a country of L, a rank and a deferred byte of 0 or 1, at most
	// one per slot, in strictly ascending role, country and rank order with
	// each country and role's ranks 1, 2, ….
	ErrAwardRows = errors.New("ctf: award rows do not decode")
	// ErrPotRows is returned for pot rows that are not the 498 pots in pot
	// order, each with at most min(5, units) winners.
	ErrPotRows = errors.New("ctf: pot rows do not decode")
	// ErrPendingRows is returned for pending rows that are not whole rows of a
	// role, a country of L, a rank, a nonzero coldkey and a positive amount,
	// in strictly ascending awarding epoch, role, country and rank order.
	ErrPendingRows = errors.New("ctf: pending rows do not decode")
	// ErrAmountRange is returned for an amount at or above 2^128.
	ErrAmountRange = errors.New("ctf: an amount is not below 2^128")

	// ErrHeaderSchema is returned for a header schema other than LedgerSchema.
	ErrHeaderSchema = errors.New("ctf: header schema is not " + LedgerSchema)
	// ErrHeaderPool is returned for a header whose pool_id is not 2^32 +
	// no_id, or whose no_id is not below 2^32.
	ErrHeaderPool = errors.New("ctf: header pool_id is not 2^32 + no_id")
	// ErrHeaderCountries is returned for a header whose countries reference
	// is not the countries object of L.
	ErrHeaderCountries = errors.New("ctf: header countries is not the list L")
	// ErrHeaderCarry is returned for a carry_coldkey that is not "0x" and 64
	// lowercase hex digits.
	ErrHeaderCarry = errors.New("ctf: header carry_coldkey is not 0x and 64 lowercase hex digits")
)

var amountLimit = new(uint256.Int).Lsh(uint256.NewInt(1), 128)

func appendAmount(b []byte, v *uint256.Int) []byte {
	value := v.Bytes32()
	return append(b, value[16:]...)
}

func checkAmount(v *uint256.Int) error {
	if !v.Lt(amountLimit) {
		return fmt.Errorf("%w: %s", ErrAmountRange, v.Dec())
	}
	return nil
}

// EncodeAwards encodes award rows, in the order given. Every amount must be
// below 2^128.
func EncodeAwards(awards []Award) ([]byte, error) {
	b := make([]byte, 0, len(awards)*AwardRowBytes)
	for i := range awards {
		a := &awards[i]
		if err := checkAmount(&a.Slot); err != nil {
			return nil, err
		}
		if err := checkAmount(&a.Pot); err != nil {
			return nil, err
		}
		b = append(b, byte(a.Role))
		b = append(b, a.Country[:]...)
		b = append(b, byte(a.Rank))
		b = append(b, a.Unit[:]...)
		b = append(b, a.ClientID[:]...)
		b = append(b, a.Coldkey[:]...)
		b = appendAmount(b, &a.Slot)
		b = appendAmount(b, &a.Pot)
		if a.Deferred {
			b = append(b, 1)
		} else {
			b = append(b, 0)
		}
	}
	return b, nil
}

// DecodeAwards decodes award rows (§5), checking their encoding and order.
func DecodeAwards(data []byte) ([]Award, error) {
	if len(data)%AwardRowBytes != 0 || len(data) > NumSlots*AwardRowBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrAwardRows, len(data))
	}
	awards := make([]Award, len(data)/AwardRowBytes)
	for i := range awards {
		b := data[i*AwardRowBytes : (i+1)*AwardRowBytes]
		a := &awards[i]
		a.Role = Role(b[0])
		copy(a.Country[:], b[1:3])
		a.Rank = int(b[3])
		copy(a.Unit[:], b[4:36])
		copy(a.ClientID[:], b[36:52])
		copy(a.Coldkey[:], b[52:84])
		a.Slot.SetBytes(b[84:100])
		a.Pot.SetBytes(b[100:116])
		switch b[116] {
		case 0:
		case 1:
			a.Deferred = true
		default:
			return nil, fmt.Errorf("%w: row %d deferred byte %d", ErrAwardRows, i, b[116])
		}
		if !a.Role.Valid() || !a.Country.InL() || a.Rank < 1 || a.Rank > Ranks {
			return nil, fmt.Errorf("%w: row %d", ErrAwardRows, i)
		}
		if i == 0 || awards[i-1].Role != a.Role || awards[i-1].Country != a.Country {
			if a.Rank != 1 || (i > 0 && compareSlots(&awards[i-1], a) >= 0) {
				return nil, fmt.Errorf("%w: row %d is out of order", ErrAwardRows, i)
			}
		} else if a.Rank != awards[i-1].Rank+1 {
			return nil, fmt.Errorf("%w: row %d is out of order", ErrAwardRows, i)
		}
	}
	return awards, nil
}

// compareSlots orders awards by role, then country.
func compareSlots(a, b *Award) int {
	if c := cmp.Compare(a.Role, b.Role); c != 0 {
		return c
	}
	return bytes.Compare(a.Country[:], b.Country[:])
}

// EncodePots encodes the 498 pot rows, which must be in pot order with
// amounts below 2^128.
func EncodePots(pots []Pot) ([]byte, error) {
	if len(pots) != NumPots {
		return nil, fmt.Errorf("%w: %d pots", ErrPotRows, len(pots))
	}
	b := make([]byte, 0, NumPots*PotRowBytes)
	for j := range pots {
		p := &pots[j]
		c, role := PotAt(j)
		if p.Country != CountryAt(c) || p.Role != role || p.Winners < 0 || p.Winners > Ranks {
			return nil, fmt.Errorf("%w: pot %d", ErrPotRows, j)
		}
		if err := checkAmount(&p.In); err != nil {
			return nil, err
		}
		if err := checkAmount(&p.Out); err != nil {
			return nil, err
		}
		b = append(b, p.Country[:]...)
		b = append(b, byte(p.Role))
		b = binary.BigEndian.AppendUint32(b, p.Units)
		b = append(b, byte(p.Winners))
		b = appendAmount(b, &p.In)
		b = appendAmount(b, &p.Out)
	}
	return b, nil
}

// DecodePots decodes pot rows (§5): exactly the 498 pots in pot order, each
// with at most min(5, units) winners.
func DecodePots(data []byte) ([]Pot, error) {
	if len(data) != NumPots*PotRowBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrPotRows, len(data))
	}
	pots := make([]Pot, NumPots)
	for j := range pots {
		b := data[j*PotRowBytes : (j+1)*PotRowBytes]
		p := &pots[j]
		copy(p.Country[:], b[0:2])
		p.Role = Role(b[2])
		p.Units = binary.BigEndian.Uint32(b[3:7])
		p.Winners = int(b[7])
		p.In.SetBytes(b[8:24])
		p.Out.SetBytes(b[24:40])
		c, role := PotAt(j)
		if p.Country != CountryAt(c) || p.Role != role || p.Winners > Ranks || uint32(p.Winners) > p.Units {
			return nil, fmt.Errorf("%w: pot %d", ErrPotRows, j)
		}
	}
	return pots, nil
}

func comparePending(a, b *PendingAward) int {
	if c := cmp.Compare(a.Epoch, b.Epoch); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Role, b.Role); c != 0 {
		return c
	}
	if c := bytes.Compare(a.Country[:], b.Country[:]); c != 0 {
		return c
	}
	return cmp.Compare(a.Rank, b.Rank)
}

func checkPending(pending []PendingAward) error {
	for i := range pending {
		p := &pending[i]
		if !p.Role.Valid() || !p.Country.InL() || p.Rank < 1 || p.Rank > Ranks || p.Coldkey == ([32]byte{}) || p.Amount.IsZero() {
			return fmt.Errorf("%w: row %d", ErrPendingRows, i)
		}
		if err := checkAmount(&p.Amount); err != nil {
			return err
		}
		if i > 0 && comparePending(&pending[i-1], p) >= 0 {
			return fmt.Errorf("%w: row %d is out of order", ErrPendingRows, i)
		}
	}
	return nil
}

// EncodePending encodes pending rows, which must satisfy DecodePending's
// rules.
func EncodePending(pending []PendingAward) ([]byte, error) {
	if err := checkPending(pending); err != nil {
		return nil, err
	}
	b := make([]byte, 0, len(pending)*PendingRowBytes)
	for i := range pending {
		p := &pending[i]
		b = binary.BigEndian.AppendUint64(b, p.Epoch)
		b = append(b, byte(p.Role))
		b = append(b, p.Country[:]...)
		b = append(b, byte(p.Rank))
		b = append(b, p.Coldkey[:]...)
		b = appendAmount(b, &p.Amount)
	}
	return b, nil
}

// DecodePending decodes pending rows (§5), checking each row and their order.
func DecodePending(data []byte) ([]PendingAward, error) {
	if len(data)%PendingRowBytes != 0 {
		return nil, fmt.Errorf("%w: %d bytes", ErrPendingRows, len(data))
	}
	pending := make([]PendingAward, len(data)/PendingRowBytes)
	for i := range pending {
		b := data[i*PendingRowBytes : (i+1)*PendingRowBytes]
		p := &pending[i]
		p.Epoch = binary.BigEndian.Uint64(b[0:8])
		p.Role = Role(b[8])
		copy(p.Country[:], b[9:11])
		p.Rank = int(b[11])
		copy(p.Coldkey[:], b[12:44])
		p.Amount.SetBytes(b[44:60])
	}
	if err := checkPending(pending); err != nil {
		return nil, err
	}
	return pending, nil
}

// Header is a CTF root's header (§5). ledgerHash, committed on chain, is the
// sha256 of its encoding. Its pool_id is PoolID(NoID), and its countries
// reference CountriesRef.
type Header struct {
	NoID         uint64
	Epoch        uint64
	CarryColdkey [32]byte
	Epochs       []HeaderEpoch
	// Accounts lists the account chunks.
	Accounts []ledger.Ref
}

// HeaderEpoch is one covered epoch of a header.
type HeaderEpoch struct {
	Epoch uint64
	// Census is the committed census's reference, its census hash, or nil
	// when none was committed.
	Census *ledger.Ref
	// Round is ρ_k for an epoch with a draw, otherwise 0.
	Round   uint64
	Awards  ledger.Ref
	Pots    ledger.Ref
	Pending ledger.Ref
}

// headerJSON is a header as JSON decodes it. A nil slice or pointer stands
// for null.
type headerJSON struct {
	Schema       string            `json:"schema"`
	NoID         uint64            `json:"no_id"`
	PoolID       uint64            `json:"pool_id"`
	Epoch        uint64            `json:"epoch"`
	Countries    string            `json:"countries"`
	CarryColdkey string            `json:"carry_coldkey"`
	Epochs       []headerEpochJSON `json:"epochs"`
	Accounts     []string          `json:"accounts"`
}

type headerEpochJSON struct {
	Epoch   uint64  `json:"epoch"`
	Census  *string `json:"census"`
	Round   uint64  `json:"round"`
	Awards  string  `json:"awards"`
	Pots    string  `json:"pots"`
	Pending string  `json:"pending"`
}

// appendJSON writes the canonical form: compact, keys in this order,
// integers as JSON numbers, strings unescaped, null only for a census.
func (h *headerJSON) appendJSON(b []byte) []byte {
	b = append(b, `{"schema":"`...)
	b = append(b, h.Schema...)
	b = append(b, `","no_id":`...)
	b = strconv.AppendUint(b, h.NoID, 10)
	b = append(b, `,"pool_id":`...)
	b = strconv.AppendUint(b, h.PoolID, 10)
	b = append(b, `,"epoch":`...)
	b = strconv.AppendUint(b, h.Epoch, 10)
	b = append(b, `,"countries":"`...)
	b = append(b, h.Countries...)
	b = append(b, `","carry_coldkey":"`...)
	b = append(b, h.CarryColdkey...)
	b = append(b, `","epochs":`...)
	if h.Epochs == nil {
		b = append(b, "null"...)
	} else {
		b = append(b, '[')
		for i := range h.Epochs {
			epoch := &h.Epochs[i]
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, `{"epoch":`...)
			b = strconv.AppendUint(b, epoch.Epoch, 10)
			b = append(b, `,"census":`...)
			if epoch.Census == nil {
				b = append(b, "null"...)
			} else {
				b = append(b, '"')
				b = append(b, *epoch.Census...)
				b = append(b, '"')
			}
			b = append(b, `,"round":`...)
			b = strconv.AppendUint(b, epoch.Round, 10)
			b = append(b, `,"awards":"`...)
			b = append(b, epoch.Awards...)
			b = append(b, `","pots":"`...)
			b = append(b, epoch.Pots...)
			b = append(b, `","pending":"`...)
			b = append(b, epoch.Pending...)
			b = append(b, `"}`...)
		}
		b = append(b, ']')
	}
	b = append(b, `,"accounts":`...)
	b = appendStrings(b, h.Accounts)
	return append(b, '}')
}

func coldkeyString(coldkey [32]byte) string {
	return "0x" + hex.EncodeToString(coldkey[:])
}

// EncodeHeader returns the canonical bytes of h.
func EncodeHeader(h *Header) ([]byte, error) {
	if h.NoID >= PoolIDBase {
		return nil, fmt.Errorf("%w: no_id %d", ErrHeaderPool, h.NoID)
	}
	if len(h.Epochs) > ledger.MaxCoveredEpochs {
		return nil, ledger.ErrTooManyEpochs
	}
	raw := headerJSON{
		Schema: LedgerSchema, NoID: h.NoID, PoolID: PoolID(h.NoID), Epoch: h.Epoch, Countries: CountriesRef.String(),
		CarryColdkey: coldkeyString(h.CarryColdkey), Epochs: make([]headerEpochJSON, len(h.Epochs)), Accounts: refStrings(h.Accounts),
	}
	for i := range h.Epochs {
		epoch := &h.Epochs[i]
		entry := &raw.Epochs[i]
		entry.Epoch, entry.Round = epoch.Epoch, epoch.Round
		if epoch.Census != nil {
			census := epoch.Census.String()
			entry.Census = &census
		}
		entry.Awards, entry.Pots, entry.Pending = epoch.Awards.String(), epoch.Pots.String(), epoch.Pending.String()
	}
	data := raw.appendJSON(nil)
	if len(data) > ledger.MaxHeaderBytes {
		return nil, fmt.Errorf("%w: %d bytes", ledger.ErrHeaderTooLarge, len(data))
	}
	return data, nil
}

// DecodeHeader decodes a canonical header (§5). It decodes with unknown
// fields refused, refuses trailing bytes, and requires that re-encoding
// reproduce the bytes exactly; then it checks the schema, that null appears
// only for a census, the epoch count, the pool id, the countries reference,
// the carry coldkey and every reference.
func DecodeHeader(data []byte) (*Header, error) {
	if len(data) > ledger.MaxHeaderBytes {
		return nil, fmt.Errorf("%w: %d bytes", ledger.ErrHeaderTooLarge, len(data))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var raw headerJSON
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ledger.ErrHeaderDecode, err)
	}
	if decoder.InputOffset() != int64(len(data)) {
		return nil, ledger.ErrHeaderTrailingData
	}
	if !bytes.Equal(raw.appendJSON(nil), data) {
		return nil, ledger.ErrHeaderNotCanonical
	}
	if raw.Schema != LedgerSchema {
		return nil, ErrHeaderSchema
	}
	if raw.Epochs == nil || raw.Accounts == nil {
		return nil, ledger.ErrHeaderNull
	}
	if len(raw.Epochs) > ledger.MaxCoveredEpochs {
		return nil, ledger.ErrTooManyEpochs
	}
	if raw.NoID >= PoolIDBase || raw.PoolID != PoolID(raw.NoID) {
		return nil, fmt.Errorf("%w: no_id %d, pool_id %d", ErrHeaderPool, raw.NoID, raw.PoolID)
	}
	if raw.Countries != CountriesRef.String() {
		return nil, ErrHeaderCountries
	}
	h := &Header{NoID: raw.NoID, Epoch: raw.Epoch, Epochs: make([]HeaderEpoch, len(raw.Epochs))}
	carry := raw.CarryColdkey
	if len(carry) != 66 || carry[:2] != "0x" || !lowerHex(carry[2:]) {
		return nil, ErrHeaderCarry
	}
	hex.Decode(h.CarryColdkey[:], []byte(carry[2:]))
	var err error
	for i := range raw.Epochs {
		entry, epoch := &raw.Epochs[i], &h.Epochs[i]
		epoch.Epoch, epoch.Round = entry.Epoch, entry.Round
		if entry.Census != nil {
			census, err := ledger.ParseRef(*entry.Census)
			if err != nil {
				return nil, fmt.Errorf("%w: epoch %d census", err, entry.Epoch)
			}
			epoch.Census = &census
		}
		for _, field := range []struct {
			value string
			ref   *ledger.Ref
		}{{entry.Awards, &epoch.Awards}, {entry.Pots, &epoch.Pots}, {entry.Pending, &epoch.Pending}} {
			if *field.ref, err = ledger.ParseRef(field.value); err != nil {
				return nil, fmt.Errorf("%w: epoch %d", err, entry.Epoch)
			}
		}
	}
	if h.Accounts, err = parseRefs(raw.Accounts); err != nil {
		return nil, err
	}
	return h, nil
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
