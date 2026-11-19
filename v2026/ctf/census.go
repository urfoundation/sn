package ctf

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/urfoundation/sn/v2026/ledger"
)

const (
	// CensusSchema names the census header format (§4.3).
	CensusSchema = "urnetwork-ctf-census-v1"
	// CensusRowBytes is the size of a census row: role(1) ‖ country(2) ‖
	// unit(32) ‖ unit family(1) ‖ client_id(16).
	CensusRowBytes = 52
	// ExclusionRowBytes is the size of an exclusion row: country(2) ‖ role(1)
	// ‖ reason(1) ‖ count(4).
	ExclusionRowBytes = 8
	// FamilyIPv4 and FamilyIPv6 are a unit's family byte: its prefix is a
	// keyed IPv4 /29 or IPv6 /48.
	FamilyIPv4 = 4
	FamilyIPv6 = 6
	// maxExclusionRows bounds an exclusion object: one row per country of L
	// or CountryUnknown, role and reason.
	maxExclusionRows = (NumCountries + 1) * 2 * NumReasons
)

var (
	// ErrCensusTooLarge is returned for a census header over 256 KiB.
	ErrCensusTooLarge = errors.New("ctf: census header exceeds 256 KiB")
	// ErrCensusDecode is returned for a census header that is not JSON of the
	// header's shape: a syntax error, an unknown field, or a value of the
	// wrong type.
	ErrCensusDecode = errors.New("ctf: census header does not decode")
	// ErrCensusTrailingData is returned for any byte after the census header.
	ErrCensusTrailingData = errors.New("ctf: census header has trailing data")
	// ErrCensusNotCanonical is returned when re-encoding the decoded census
	// header does not reproduce its bytes.
	ErrCensusNotCanonical = errors.New("ctf: census header is not canonical")
	// ErrCensusSchema is returned for a census schema other than CensusSchema.
	ErrCensusSchema = errors.New("ctf: census header schema is not " + CensusSchema)
	// ErrCensusNull is returned for a census header with null in place of a list.
	ErrCensusNull = errors.New("ctf: census header has null")
	// ErrCensusPool is returned for a census whose pool_id is not 2^32 +
	// no_id, or whose no_id is not below 2^32.
	ErrCensusPool = errors.New("ctf: census pool_id is not 2^32 + no_id")
	// ErrCensusEpoch is returned for a census committed for one pool and
	// epoch whose header names another.
	ErrCensusEpoch = errors.New("ctf: census header is not the committed pool and epoch's")
	// ErrCensusCountries is returned for a census whose countries reference
	// is not the countries object of L.
	ErrCensusCountries = errors.New("ctf: census countries is not the list L")
	// ErrCensusSamples is returned for excused samples that are not ascending
	// distinct sample indexes below SamplesPerEpoch.
	ErrCensusSamples = errors.New("ctf: excused samples are not ascending distinct samples below 672")
	// ErrCensusCandidates is returned for a candidates count that is not the
	// row count.
	ErrCensusCandidates = errors.New("ctf: census candidates is not its row count")
	// ErrCensusRow is returned for a census row with a role that is not 1 or
	// 2, a country not in L, a family that is not 4 or 6, or a zero unit or
	// client id.
	ErrCensusRow = errors.New("ctf: census row is not valid")
	// ErrCensusOrder is returned for census rows that are not in strictly
	// ascending role, country, unit and client id order.
	ErrCensusOrder = errors.New("ctf: census rows are not in strictly ascending order")
	// ErrCensusClient is returned for a client listed twice in one role.
	ErrCensusClient = errors.New("ctf: a client is a candidate twice in one role")
	// ErrCensusFamily is returned for a unit listed with two families.
	ErrCensusFamily = errors.New("ctf: a unit has two families")
	// ErrExcluded is returned for an exclusion object that is not whole rows
	// of a country in L or "zz", a role, a reason and a positive count, in
	// strictly ascending country, role and reason order.
	ErrExcluded = errors.New("ctf: exclusion counts are not valid")
	// ErrCensusUnavailable is returned by DecodeCensus for an object it was
	// not given.
	ErrCensusUnavailable = errors.New("ctf: a census object is unavailable")
)

// Candidate is one census row (§4.2, §4.3): a provider or extender client of
// the operator that its own data shows eligible for the epoch.
type Candidate struct {
	Role Role
	// Country is the candidate's own published country at end(k), in L. A
	// unit enters the draw of its lowest client id's country (§4.2), so
	// candidates of one unit may list different countries.
	Country Country
	// Unit is the candidate's unit, the keyed egress prefix hash
	// HMAC-SHA256(egress_hash_key, "urnetwork/egress-prefix/v1" ‖
	// masked_prefix) of WHITEPAPER.md §8.4 and §8.6.2: its IPv4 /29 first,
	// else its IPv6 /48, as server/model VerifyEgressIpHashWithSettings keys
	// it under the signed policy's granularity.
	Unit [32]byte
	// Family is FamilyIPv4 or FamilyIPv6, the family of Unit's prefix.
	Family uint8
	// ClientID is the provider client id: for an extender, its record's
	// provider client.
	ClientID [16]byte
}

// Reason is why the census left a miner out (§4.3). Each excluded miner is
// counted once, under the lowest-numbered reason that excludes it, so
// ReasonCountryChanged and ReasonPresence count exactly the miners that
// every other rule admitted: the near misses of §9.1.
type Reason uint8

const (
	// ReasonRevoked: an extender record revoked or inactive at end(k), or
	// with no active address (X1).
	ReasonRevoked Reason = iota + 1
	// ReasonOffline: a provider not online, connected or passing the
	// operator's eligibility at end(k) (P1).
	ReasonOffline
	// ReasonNotPublic: a provider without a Public provide key (P1).
	ReasonNotPublic
	// ReasonNoLocation: no valid location at end(k) (P1, X3).
	ReasonNoLocation
	// ReasonOwnNetwork: on one of the operator's own networks (P6, X6).
	ReasonOwnNetwork
	// ReasonHeadBound: an active fleet binding at either boundary (P5, X5).
	ReasonHeadBound
	// ReasonNoWallet: no payout coldkey under the wallet selection (P5, X5).
	ReasonNoWallet
	// ReasonDuplicate: an address another candidate with a lower client id
	// holds (P4, X4).
	ReasonDuplicate
	// ReasonCountryChanged: a country at end(k), or an activation in k, other
	// than the country of its presence (P3, X3).
	ReasonCountryChanged
	// ReasonPresence: present in its country for less than half of the
	// epoch's non-excused samples (P2, X2).
	ReasonPresence
	// NumReasons is the count of reasons.
	NumReasons = int(ReasonPresence)
)

// Exclusion counts the miners of one role that one reason left out in one
// country: the country at end(k), or CountryUnknown for a miner without one.
type Exclusion struct {
	Country Country
	Role    Role
	Reason  Reason
	Count   uint32
}

// Census is an epoch's census (§4.3): the operator's candidates, the
// presence samples it excused for a platform-wide outage it declared, and
// the per-country exclusion counts. It holds no coldkeys and no addresses.
type Census struct {
	NoID  uint64
	Epoch uint64
	// Candidates are sorted by role, country, unit and client id.
	Candidates []Candidate
	// ExcusedSamples are the excused sample indexes, ascending.
	ExcusedSamples []uint16
	// Excluded are the exclusion counts, by country, role and reason.
	Excluded []Exclusion
}

// CensusObjects is an encoded census.
type CensusObjects struct {
	// Hash is the census hash, sha256(Header), which commitCtfCensus commits
	// and the ledger header names as the epoch's "census".
	Hash ledger.Ref
	// Header is the census header's canonical JSON.
	Header []byte
	// Rows are the row chunks' references, and Excluded the exclusion
	// object's.
	Rows     []ledger.Ref
	Excluded ledger.Ref
	// Objects holds the header, the row chunks, the exclusion object and the
	// countries object, by reference. Publish them before the commitment.
	Objects ledger.Objects
}

// CensusHeader is a decoded census header.
type CensusHeader struct {
	NoID   uint64
	PoolID uint64
	Epoch  uint64
	// Rows lists the row chunks.
	Rows           []ledger.Ref
	Candidates     uint64
	ExcusedSamples []uint16
	Excluded       ledger.Ref
}

func compareCandidates(a, b *Candidate) int {
	if c := cmp.Compare(a.Role, b.Role); c != 0 {
		return c
	}
	if c := bytes.Compare(a.Country[:], b.Country[:]); c != 0 {
		return c
	}
	if c := bytes.Compare(a.Unit[:], b.Unit[:]); c != 0 {
		return c
	}
	return bytes.Compare(a.ClientID[:], b.ClientID[:])
}

func compareExclusions(a, b *Exclusion) int {
	if c := bytes.Compare(a.Country[:], b.Country[:]); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Role, b.Role); c != 0 {
		return c
	}
	return cmp.Compare(a.Reason, b.Reason)
}

// checkCandidates checks census rows (§8 check 1): each row valid, the rows
// in strictly ascending order, a client at most once per role, and each unit
// with one family.
func checkCandidates(rows []Candidate) error {
	roles := make(map[[16]byte]uint8, len(rows))
	families := make(map[[32]byte]uint8, len(rows))
	for i := range rows {
		r := &rows[i]
		if !r.Role.Valid() || !r.Country.InL() || (r.Family != FamilyIPv4 && r.Family != FamilyIPv6) || r.Unit == ([32]byte{}) || r.ClientID == ([16]byte{}) {
			return fmt.Errorf("%w: row %d", ErrCensusRow, i)
		}
		if i > 0 && compareCandidates(&rows[i-1], r) >= 0 {
			return fmt.Errorf("%w: row %d", ErrCensusOrder, i)
		}
		bit := uint8(1) << r.Role
		if roles[r.ClientID]&bit != 0 {
			return fmt.Errorf("%w: row %d", ErrCensusClient, i)
		}
		roles[r.ClientID] |= bit
		if family, ok := families[r.Unit]; ok && family != r.Family {
			return fmt.Errorf("%w: row %d", ErrCensusFamily, i)
		}
		families[r.Unit] = r.Family
	}
	return nil
}

func checkExclusions(rows []Exclusion) error {
	for i := range rows {
		x := &rows[i]
		if !(x.Country.InL() || x.Country == CountryUnknown) || !x.Role.Valid() || x.Reason < 1 || int(x.Reason) > NumReasons || x.Count == 0 {
			return fmt.Errorf("%w: row %d", ErrExcluded, i)
		}
		if i > 0 && compareExclusions(&rows[i-1], x) >= 0 {
			return fmt.Errorf("%w: row %d is out of order", ErrExcluded, i)
		}
	}
	return nil
}

func checkSamples(samples []uint16) error {
	for i, s := range samples {
		if s >= SamplesPerEpoch || (i > 0 && samples[i-1] >= s) {
			return fmt.Errorf("%w: sample %d", ErrCensusSamples, i)
		}
	}
	return nil
}

// EncodeCensus encodes a census (§4.3). It sorts copies of the candidates,
// the excused samples and the exclusion counts, and refuses a census that
// would not decode: a row, sample or count that is not valid, a client twice
// in one role, a unit with two families, or an operator id at or above 2^32.
// The candidates of a unit may list different countries.
func EncodeCensus(c *Census) (*CensusObjects, error) {
	if c.NoID >= PoolIDBase {
		return nil, fmt.Errorf("%w: operator %d is not below 2^32", ErrInput, c.NoID)
	}
	rows := slices.Clone(c.Candidates)
	slices.SortFunc(rows, func(a, b Candidate) int { return compareCandidates(&a, &b) })
	if err := checkCandidates(rows); err != nil {
		return nil, err
	}
	samples := slices.Clone(c.ExcusedSamples)
	slices.Sort(samples)
	if err := checkSamples(samples); err != nil {
		return nil, err
	}
	excluded := slices.Clone(c.Excluded)
	slices.SortFunc(excluded, func(a, b Exclusion) int { return compareExclusions(&a, &b) })
	if err := checkExclusions(excluded); err != nil {
		return nil, err
	}

	out := &CensusObjects{Objects: ledger.Objects{CountriesRef: CountriesObject()}}
	for _, chunk := range encodeChunks(len(rows), CensusRowBytes, func(b []byte, i int) []byte { return appendCandidate(b, &rows[i]) }) {
		ref := ledger.RefOf(chunk)
		out.Rows = append(out.Rows, ref)
		out.Objects[ref] = chunk
	}
	excludedBytes := encodeExclusions(excluded)
	out.Excluded = ledger.RefOf(excludedBytes)
	out.Objects[out.Excluded] = excludedBytes
	raw := censusHeaderJSON{
		Schema: CensusSchema, NoID: c.NoID, PoolID: PoolID(c.NoID), Epoch: c.Epoch,
		Countries: CountriesRef.String(), Rows: refStrings(out.Rows), Candidates: uint64(len(rows)),
		ExcusedSamples: make([]uint64, len(samples)), Excluded: out.Excluded.String(),
	}
	for i, s := range samples {
		raw.ExcusedSamples[i] = uint64(s)
	}
	out.Header = raw.appendJSON(nil)
	if len(out.Header) > ledger.MaxHeaderBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrCensusTooLarge, len(out.Header))
	}
	out.Hash = ledger.RefOf(out.Header)
	out.Objects[out.Hash] = out.Header
	return out, nil
}

func appendCandidate(b []byte, r *Candidate) []byte {
	b = append(b, byte(r.Role))
	b = append(b, r.Country[:]...)
	b = append(b, r.Unit[:]...)
	b = append(b, r.Family)
	return append(b, r.ClientID[:]...)
}

func encodeExclusions(rows []Exclusion) []byte {
	b := make([]byte, 0, len(rows)*ExclusionRowBytes)
	for i := range rows {
		x := &rows[i]
		b = append(b, x.Country[:]...)
		b = append(b, byte(x.Role), byte(x.Reason))
		b = binary.BigEndian.AppendUint32(b, x.Count)
	}
	return b
}

// DecodeCensusRows decodes census row chunks, checking the chunk shape of a
// ledger chunk list and the rows (§8 check 1): each valid, in strictly
// ascending order, a client at most once per role and each unit with one
// family.
func DecodeCensusRows(chunks [][]byte) ([]Candidate, error) {
	var n int
	for j, chunk := range chunks {
		if err := checkChunk(len(chunk), CensusRowBytes, j, len(chunks)); err != nil {
			return nil, err
		}
		n += len(chunk) / CensusRowBytes
	}
	rows := make([]Candidate, 0, n)
	for _, chunk := range chunks {
		for offset := 0; offset < len(chunk); offset += CensusRowBytes {
			b := chunk[offset : offset+CensusRowBytes]
			var r Candidate
			r.Role = Role(b[0])
			copy(r.Country[:], b[1:3])
			copy(r.Unit[:], b[3:35])
			r.Family = b[35]
			copy(r.ClientID[:], b[36:52])
			rows = append(rows, r)
		}
	}
	if err := checkCandidates(rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// DecodeExclusions decodes an exclusion object.
func DecodeExclusions(data []byte) ([]Exclusion, error) {
	if len(data)%ExclusionRowBytes != 0 || len(data) > maxExclusionRows*ExclusionRowBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrExcluded, len(data))
	}
	rows := make([]Exclusion, len(data)/ExclusionRowBytes)
	for i := range rows {
		b := data[i*ExclusionRowBytes:]
		rows[i] = Exclusion{Country: Country{b[0], b[1]}, Role: Role(b[2]), Reason: Reason(b[3]), Count: binary.BigEndian.Uint32(b[4:8])}
	}
	if err := checkExclusions(rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// censusHeaderJSON is a census header as JSON decodes it. A nil slice
// stands for null.
type censusHeaderJSON struct {
	Schema         string   `json:"schema"`
	NoID           uint64   `json:"no_id"`
	PoolID         uint64   `json:"pool_id"`
	Epoch          uint64   `json:"epoch"`
	Countries      string   `json:"countries"`
	Rows           []string `json:"rows"`
	Candidates     uint64   `json:"candidates"`
	ExcusedSamples []uint64 `json:"excused_samples"`
	Excluded       string   `json:"excluded"`
}

// appendJSON writes the canonical form: compact, keys in this order,
// integers as JSON numbers, strings unescaped. A header whose strings would
// need escaping fails the schema or reference rules, so the bytes accepted
// are exactly the canonical JSON.
func (h *censusHeaderJSON) appendJSON(b []byte) []byte {
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
	b = append(b, `","rows":`...)
	b = appendStrings(b, h.Rows)
	b = append(b, `,"candidates":`...)
	b = strconv.AppendUint(b, h.Candidates, 10)
	b = append(b, `,"excused_samples":`...)
	if h.ExcusedSamples == nil {
		b = append(b, "null"...)
	} else {
		b = append(b, '[')
		for i, s := range h.ExcusedSamples {
			if i > 0 {
				b = append(b, ',')
			}
			b = strconv.AppendUint(b, s, 10)
		}
		b = append(b, ']')
	}
	b = append(b, `,"excluded":"`...)
	b = append(b, h.Excluded...)
	return append(b, `"}`...)
}

// DecodeCensusHeader decodes a canonical census header (§4.3). It decodes
// with unknown fields refused, refuses trailing bytes, and requires that
// re-encoding reproduce the input; then it checks the schema, that no list is
// null, the pool id, the countries reference, the excused samples and every
// reference.
func DecodeCensusHeader(data []byte) (*CensusHeader, error) {
	if len(data) > ledger.MaxHeaderBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrCensusTooLarge, len(data))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var raw censusHeaderJSON
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCensusDecode, err)
	}
	if decoder.InputOffset() != int64(len(data)) {
		return nil, ErrCensusTrailingData
	}
	if !bytes.Equal(raw.appendJSON(nil), data) {
		return nil, ErrCensusNotCanonical
	}
	if raw.Schema != CensusSchema {
		return nil, ErrCensusSchema
	}
	if raw.Rows == nil || raw.ExcusedSamples == nil {
		return nil, ErrCensusNull
	}
	if raw.NoID >= PoolIDBase || raw.PoolID != PoolID(raw.NoID) {
		return nil, fmt.Errorf("%w: no_id %d, pool_id %d", ErrCensusPool, raw.NoID, raw.PoolID)
	}
	if raw.Countries != CountriesRef.String() {
		return nil, ErrCensusCountries
	}
	h := &CensusHeader{NoID: raw.NoID, PoolID: raw.PoolID, Epoch: raw.Epoch, Candidates: raw.Candidates, ExcusedSamples: make([]uint16, len(raw.ExcusedSamples))}
	for i, s := range raw.ExcusedSamples {
		if s >= SamplesPerEpoch {
			return nil, fmt.Errorf("%w: sample %d", ErrCensusSamples, i)
		}
		h.ExcusedSamples[i] = uint16(s)
	}
	if err := checkSamples(h.ExcusedSamples); err != nil {
		return nil, err
	}
	var err error
	if h.Rows, err = parseRefs(raw.Rows); err != nil {
		return nil, err
	}
	if h.Excluded, err = ledger.ParseRef(raw.Excluded); err != nil {
		return nil, err
	}
	return h, nil
}

// DecodeCensus decodes a census from its canonical header and the objects
// the header names, held in objects, and checks it is canonical: the header
// (DecodeCensusHeader), the rows (DecodeCensusRows), their count and the
// exclusion counts (DecodeExclusions). An object missing from objects gives
// ErrCensusUnavailable, except the empty object, which needs no bytes; any
// other error means the census is not canonical.
func DecodeCensus(header []byte, objects ledger.Objects) (*Census, error) {
	h, err := DecodeCensusHeader(header)
	if err != nil {
		return nil, err
	}
	get := func(ref ledger.Ref) ([]byte, error) {
		if data, ok := objects[ref]; ok {
			return data, nil
		}
		if ref == emptyRef {
			return []byte{}, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrCensusUnavailable, ref)
	}
	chunks := make([][]byte, len(h.Rows))
	for j, ref := range h.Rows {
		if chunks[j], err = get(ref); err != nil {
			return nil, err
		}
	}
	excluded, err := get(h.Excluded)
	if err != nil {
		return nil, err
	}
	return decodeCensusObjects(h, chunks, excluded)
}

const (
	// censusChunkLimit and exclusionLimit are the most bytes a canonical
	// census's row chunk and exclusion object hold.
	censusChunkLimit = ledger.ChunkRows * CensusRowBytes
	exclusionLimit   = maxExclusionRows * ExclusionRowBytes
)

// censusGetter returns a census object whose bytes match ref: its bytes, or
// oversize when it is longer than limit, or an error when it cannot be had.
type censusGetter func(ref ledger.Ref, limit int) (data []byte, oversize bool, err error)

// readCensus reads the census committed as hash for operator noID's epoch,
// in one order: the header, then each row chunk, then the exclusion object.
// It returns the census; or, with a nil census, invalid, why the committed
// census is not canonical: a header that is not, one for another pool or
// epoch, or an object longer than any canonical census holds, each of which
// ends the reading; or get's error. Build and Verify both read through it, so
// they agree on every census whose objects match their references.
func readCensus(hash ledger.Ref, noID, epoch uint64, get censusGetter) (census *Census, invalid error, err error) {
	header, oversize, err := get(hash, ledger.MaxHeaderBytes)
	if err != nil {
		return nil, nil, err
	}
	if oversize {
		return nil, fmt.Errorf("%w: %v", ErrCensusTooLarge, hash), nil
	}
	h, invalid := DecodeCensusHeader(header)
	if invalid != nil {
		return nil, invalid, nil
	}
	if h.NoID != noID || h.Epoch != epoch {
		return nil, fmt.Errorf("%w: no_id %d epoch %d, committed for %d epoch %d", ErrCensusEpoch, h.NoID, h.Epoch, noID, epoch), nil
	}
	chunks := make([][]byte, len(h.Rows))
	for j, ref := range h.Rows {
		if chunks[j], oversize, err = get(ref, censusChunkLimit); err != nil {
			return nil, nil, err
		}
		if oversize {
			return nil, fmt.Errorf("%w: chunk %d is over %d rows", ledger.ErrChunkLength, j, ledger.ChunkRows), nil
		}
	}
	excluded, oversize, err := get(h.Excluded, exclusionLimit)
	if err != nil {
		return nil, nil, err
	}
	if oversize {
		return nil, fmt.Errorf("%w: over %d bytes", ErrExcluded, exclusionLimit), nil
	}
	census, invalid = decodeCensusObjects(h, chunks, excluded)
	return census, invalid, nil
}

// decodeCensusObjects decodes a census whose header h decoded, from its row
// chunks and exclusion object, which match their references.
func decodeCensusObjects(h *CensusHeader, chunks [][]byte, excluded []byte) (*Census, error) {
	rows, err := DecodeCensusRows(chunks)
	if err != nil {
		return nil, err
	}
	if uint64(len(rows)) != h.Candidates {
		return nil, fmt.Errorf("%w: %d rows, %d candidates", ErrCensusCandidates, len(rows), h.Candidates)
	}
	exclusions, err := DecodeExclusions(excluded)
	if err != nil {
		return nil, err
	}
	return &Census{NoID: h.NoID, Epoch: h.Epoch, Candidates: rows, ExcusedSamples: h.ExcusedSamples, Excluded: exclusions}, nil
}

func appendStrings(b []byte, values []string) []byte {
	if values == nil {
		return append(b, "null"...)
	}
	b = append(b, '[')
	for i, s := range values {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '"')
		b = append(b, s...)
		b = append(b, '"')
	}
	return append(b, ']')
}

func refStrings(refs []ledger.Ref) []string {
	values := make([]string, len(refs))
	for i := range refs {
		values[i] = refs[i].String()
	}
	return values
}

func parseRefs(values []string) ([]ledger.Ref, error) {
	refs := make([]ledger.Ref, len(values))
	for i, s := range values {
		var err error
		if refs[i], err = ledger.ParseRef(s); err != nil {
			return nil, fmt.Errorf("%w: reference %d", err, i)
		}
	}
	return refs, nil
}

// encodeChunks encodes n rows as a ledger chunk list: ledger.ChunkRows rows
// in every chunk but a non-empty last one, and no chunk for no rows.
func encodeChunks(n, rowBytes int, appendRow func([]byte, int) []byte) [][]byte {
	chunks := make([][]byte, 0, (n+ledger.ChunkRows-1)/ledger.ChunkRows)
	for start := 0; start < n; start += ledger.ChunkRows {
		end := min(start+ledger.ChunkRows, n)
		chunk := make([]byte, 0, (end-start)*rowBytes)
		for i := start; i < end; i++ {
			chunk = appendRow(chunk, i)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

// checkChunk checks the length of chunk j of a list of m chunks, as the
// ledger checks its chunk lists.
func checkChunk(length, rowBytes, j, m int) error {
	if length%rowBytes != 0 || length > ledger.ChunkRows*rowBytes {
		return fmt.Errorf("%w: chunk %d has %d bytes", ledger.ErrChunkLength, j, length)
	}
	if j < m-1 && length != ledger.ChunkRows*rowBytes {
		return fmt.Errorf("%w: chunk %d of %d has %d rows", ledger.ErrChunkShort, j, m, length/rowBytes)
	}
	if j == m-1 && length == 0 {
		return fmt.Errorf("%w: chunk %d", ledger.ErrChunkEmpty, j)
	}
	return nil
}
