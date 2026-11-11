package ctf

import (
	"errors"
	"fmt"

	"github.com/urfoundation/sn/ledger"
)

const (
	// NumCountries is |L|, the ISO 3166-1 alpha-2 codes (§4.1).
	NumCountries = 249
	// Ranks is the slot count per country and role.
	Ranks = 5
	// NumSlots is S = 10·|L| = 2,490: a slot is (country, role, rank).
	NumSlots = 2 * Ranks * NumCountries
	// NumPots is the pot count: one per country and role.
	NumPots = 2 * NumCountries
	// PoolIDBase is the offset of the CTF pool ids: NO n's CTF pool is the
	// vault2 pool 2^32 + n (§3.1), so UR's is 4,294,967,297.
	PoolIDBase = 1 << 32
	// SamplesPerEpoch is the presence samples of an epoch, one per quarter
	// hour (§4.2); an excused sample's index is below it.
	SamplesPerEpoch = 672
)

// ErrInput is returned for an input that the caller must fix: a nil context,
// a missing source, an operator id at or above 2^32, a zero carry coldkey,
// or chain facts that are missing or that the chain makes impossible.
var ErrInput = errors.New("ctf: input is missing or invalid")

// Role is a slot's role: 1 provider, 2 extender. A role byte of a row is
// this value.
type Role uint8

const (
	RoleProvider Role = 1
	RoleExtender Role = 2
)

// Valid reports whether r is a role.
func (r Role) Valid() bool {
	return r == RoleProvider || r == RoleExtender
}

func (r Role) String() string {
	switch r {
	case RoleProvider:
		return "provider"
	case RoleExtender:
		return "extender"
	default:
		return fmt.Sprintf("role(%d)", uint8(r))
	}
}

// PoolID returns ctf(n) = 2^32 + n, NO n's CTF pool id. n is below 2^32.
func PoolID(noID uint64) uint64 {
	return PoolIDBase + noID
}

// Country is a country code as rows write it: two lowercase ASCII letters.
type Country [2]byte

// CountryUnknown is "zz", which an exclusion count uses for a miner without
// a country. It is not in L.
var CountryUnknown = Country{'z', 'z'}

// countryIndex[a][b] is the position in L of the code (a+'a', b+'a'), or −1.
var countryIndex [26][26]int16

// countriesObject is the countries object, and CountriesRef its reference.
var countriesObject []byte

// CountriesRef names the countries object (CountriesObject), which census
// and ledger headers name as "countries".
var CountriesRef ledger.Ref

func init() {
	for a := range countryIndex {
		for b := range countryIndex[a] {
			countryIndex[a][b] = -1
		}
	}
	countriesObject = make([]byte, 0, 2*NumCountries)
	for i, entry := range countryTable {
		code := entry.code
		if len(code) != 2 || code[0] < 'a' || code[0] > 'z' || code[1] < 'a' || code[1] > 'z' {
			panic("ctf: bad country code " + code)
		}
		if i > 0 && countryTable[i-1].code >= code {
			panic("ctf: country codes are not in ascending order at " + code)
		}
		countryIndex[code[0]-'a'][code[1]-'a'] = int16(i)
		countriesObject = append(countriesObject, code...)
	}
	CountriesRef = ledger.RefOf(countriesObject)
}

// Index returns c's position in L, or −1 when c is not in L.
func (c Country) Index() int {
	if c[0] < 'a' || c[0] > 'z' || c[1] < 'a' || c[1] > 'z' {
		return -1
	}
	return int(countryIndex[c[0]-'a'][c[1]-'a'])
}

// InL reports whether c is in L.
func (c Country) InL() bool {
	return c.Index() >= 0
}

// Name returns c's English name, or "" when c is not in L.
func (c Country) Name() string {
	if i := c.Index(); i >= 0 {
		return countryTable[i].name
	}
	return ""
}

func (c Country) String() string {
	return string(c[:])
}

// ParseCountry returns the country of an alpha-2 code in L, in either case.
func ParseCountry(code string) (Country, bool) {
	if len(code) != 2 {
		return Country{}, false
	}
	c := Country{lower(code[0]), lower(code[1])}
	return c, c.InL()
}

func lower(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// CountryAt returns the country at position i of L, 0 <= i < NumCountries.
func CountryAt(i int) Country {
	code := countryTable[i].code
	return Country{code[0], code[1]}
}

// Countries returns L in order.
func Countries() []Country {
	out := make([]Country, NumCountries)
	for i := range out {
		out[i] = CountryAt(i)
	}
	return out
}

// CountriesObject returns the countries object: L's 249 codes in order, two
// ASCII bytes each, 498 bytes.
func CountriesObject() []byte {
	return append([]byte(nil), countriesObject...)
}

// SlotIndex returns the position in slot order of the slot (country, role,
// rank), for country the position in L and rank 1 to 5. Slots are ordered by
// country code, then provider before extender, then rank (§4.1); this order
// decides which slots take the remainder rao (§4.5).
func SlotIndex(country int, role Role, rank int) int {
	return PotIndex(country, role)*Ranks + rank - 1
}

// PotIndex returns the position of the pot (country, role) in pot order:
// country code, then provider before extender. A pot's slots are slots
// PotIndex·5 … PotIndex·5 + 4.
func PotIndex(country int, role Role) int {
	return 2*country + int(role) - 1
}

// PotAt returns the country position and role of pot j.
func PotAt(j int) (country int, role Role) {
	return j / 2, Role(j%2 + 1)
}
