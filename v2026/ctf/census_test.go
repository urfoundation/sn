package ctf

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/ledger"
)

// §4.3: a census row is role ‖ country ‖ unit ‖ family ‖ client id, the
// header is canonical JSON, and the census hash is the header's sha256.
func TestCensusFormat(t *testing.T) {
	c := &Census{NoID: 1, Epoch: 8, Candidates: []Candidate{provider(1, "de", 1)}}
	objects, err := EncodeCensus(c)
	if err != nil {
		t.Fatal(err)
	}
	row := "01" + "6465" + "0b" + strings.Repeat("00", 23) + "0000000000000001" + "04" + "00000000000000000000000000000001"
	if len(objects.Rows) != 1 || hex.EncodeToString(objects.Objects[objects.Rows[0]]) != row {
		t.Fatalf("row %x", objects.Objects[objects.Rows[0]])
	}
	if objects.Excluded != ledger.RefOf(nil) || len(objects.Objects[objects.Excluded]) != 0 {
		t.Fatal("no exclusion counts is the empty object")
	}
	want := `{"schema":"urnetwork-ctf-census-v1","no_id":1,"pool_id":4294967297,"epoch":8,` +
		`"countries":"sha256:1dc01d610882808f4f3c56e8025d6fdac9d2b1cb8ca12c0458f4a367d2a17986",` +
		`"rows":["` + objects.Rows[0].String() + `"],"candidates":1,"excused_samples":[],` +
		`"excluded":"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"}`
	if string(objects.Header) != want {
		t.Fatalf("header\n%s\nwant\n%s", objects.Header, want)
	}
	if objects.Hash != ledger.RefOf(objects.Header) || !bytes.Equal(objects.Objects[objects.Hash], objects.Header) {
		t.Fatal("the census hash is not the header's sha256")
	}
	if !bytes.Equal(objects.Objects[CountriesRef], CountriesObject()) {
		t.Fatal("the countries object is not published with the census")
	}

	x := &Census{NoID: 1, Epoch: 8, ExcusedSamples: []uint16{671, 0}, Excluded: []Exclusion{
		{Country: CountryUnknown, Role: RoleExtender, Reason: ReasonNoLocation, Count: 0x01020304},
		{Country: cc("ad"), Role: RoleProvider, Reason: ReasonPresence, Count: 9},
	}}
	objects, err = EncodeCensus(x)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(objects.Objects[objects.Excluded]) != "6164010a00000009"+"7a7a020401020304" {
		t.Fatalf("exclusion rows %x", objects.Objects[objects.Excluded])
	}
	if !bytes.Contains(objects.Header, []byte(`"rows":[],"candidates":0,"excused_samples":[0,671],`)) {
		t.Fatalf("header %s", objects.Header)
	}
}

// The census decodes to exactly what was encoded, sorted; the input order
// changes nothing.
func TestCensusRoundTrip(t *testing.T) {
	c := epochCensus(11)
	objects, err := EncodeCensus(c)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCensus(objects.Header, objects.Objects)
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Clone(c.Candidates)
	slices.SortFunc(want, func(a, b Candidate) int { return compareCandidates(&a, &b) })
	if decoded.NoID != c.NoID || decoded.Epoch != c.Epoch || !slices.Equal(decoded.Candidates, want) ||
		!slices.Equal(decoded.ExcusedSamples, c.ExcusedSamples) || !slices.Equal(decoded.Excluded, c.Excluded) {
		t.Fatalf("decoded %+v", decoded)
	}
	random := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		shuffled := *c
		shuffled.Candidates = slices.Clone(c.Candidates)
		random.Shuffle(len(shuffled.Candidates), func(i, j int) {
			shuffled.Candidates[i], shuffled.Candidates[j] = shuffled.Candidates[j], shuffled.Candidates[i]
		})
		again, err := EncodeCensus(&shuffled)
		if err != nil {
			t.Fatal(err)
		}
		if again.Hash != objects.Hash {
			t.Fatal("the census hash depends on the input order")
		}
	}
	if _, err := DecodeCensus(objects.Header, ledger.Objects{}); !errors.Is(err, ErrCensusUnavailable) {
		t.Fatalf("missing rows: %v", err)
	}
}

// §8 check 1: a census is canonical only when sorted and unique, with one
// country per client per role, consistent unit families and countries in L.
func TestCensusRules(t *testing.T) {
	base := func() []Candidate {
		return []Candidate{provider(1, "de", 1), provider(2, "is", 1), extender(1, "us", 2)}
	}
	if _, err := EncodeCensus(&Census{NoID: 1, Epoch: 8, Candidates: base()}); err != nil {
		t.Fatalf("a unit of two countries and a client in both roles: %v", err)
	}
	cases := []struct {
		name   string
		mutate func([]Candidate) []Candidate
		want   error
	}{
		{"role 0", func(r []Candidate) []Candidate { r[0].Role = 0; return r }, ErrCensusRow},
		{"role 3", func(r []Candidate) []Candidate { r[0].Role = 3; return r }, ErrCensusRow},
		{"Kosovo", func(r []Candidate) []Candidate { r[0].Country = Country{'x', 'k'}; return r }, ErrCensusRow},
		{"unknown country", func(r []Candidate) []Candidate { r[0].Country = CountryUnknown; return r }, ErrCensusRow},
		{"uppercase country", func(r []Candidate) []Candidate { r[0].Country = Country{'D', 'E'}; return r }, ErrCensusRow},
		{"family 5", func(r []Candidate) []Candidate { r[0].Family = 5; return r }, ErrCensusRow},
		{"zero unit", func(r []Candidate) []Candidate { r[0].Unit = [32]byte{}; return r }, ErrCensusRow},
		{"zero client", func(r []Candidate) []Candidate { r[0].ClientID = [16]byte{}; return r }, ErrCensusRow},
		{"duplicate row", func(r []Candidate) []Candidate { return append(r, r[0]) }, ErrCensusOrder},
		{"client twice in one role", func(r []Candidate) []Candidate { return append(r, provider(1, "de", 9)) }, ErrCensusClient},
		{"client in two countries", func(r []Candidate) []Candidate { return append(r, provider(1, "fr", 1)) }, ErrCensusClient},
		{"unit of two families", func(r []Candidate) []Candidate {
			x := provider(3, "de", 1)
			x.Family = FamilyIPv6
			return append(r, x)
		}, ErrCensusFamily},
		{"unit of two families across roles", func(r []Candidate) []Candidate {
			x := extender(3, "de", 1)
			x.Family = FamilyIPv6
			return append(r, x)
		}, ErrCensusFamily},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := c.mutate(base())
			if _, err := EncodeCensus(&Census{NoID: 1, Epoch: 8, Candidates: rows}); !errors.Is(err, c.want) {
				t.Fatalf("encode: %v, want %v", err, c.want)
			}
			// The decoder refuses the same rows, in any order the encoder
			// would not write.
			slices.SortFunc(rows, func(a, b Candidate) int { return compareCandidates(&a, &b) })
			chunk := []byte{}
			for i := range rows {
				chunk = appendCandidate(chunk, &rows[i])
			}
			if _, err := DecodeCensusRows([][]byte{chunk}); !errors.Is(err, c.want) {
				t.Fatalf("decode: %v, want %v", err, c.want)
			}
		})
	}
	t.Run("unsorted", func(t *testing.T) {
		rows := base()
		chunk := appendCandidate(appendCandidate(nil, &rows[1]), &rows[0])
		if _, err := DecodeCensusRows([][]byte{chunk}); !errors.Is(err, ErrCensusOrder) {
			t.Fatal(err)
		}
	})
	t.Run("chunk shape", func(t *testing.T) {
		rows := base()
		chunk := appendCandidate(nil, &rows[0])
		for _, chunks := range [][][]byte{{chunk[:51]}, {chunk, chunk[:0]}, {chunk, chunk}} {
			_, err := DecodeCensusRows(chunks)
			if !errors.Is(err, ledger.ErrChunkLength) && !errors.Is(err, ledger.ErrChunkEmpty) && !errors.Is(err, ledger.ErrChunkShort) {
				t.Fatalf("%d chunks: %v", len(chunks), err)
			}
		}
	})
	t.Run("samples", func(t *testing.T) {
		for _, samples := range [][]uint16{{672}, {5, 5}} {
			if _, err := EncodeCensus(&Census{NoID: 1, ExcusedSamples: samples}); !errors.Is(err, ErrCensusSamples) {
				t.Fatalf("%v: %v", samples, err)
			}
		}
	})
	t.Run("exclusions", func(t *testing.T) {
		for _, x := range []Exclusion{
			{Country: Country{'x', 'k'}, Role: RoleProvider, Reason: ReasonPresence, Count: 1},
			{Country: cc("de"), Role: 3, Reason: ReasonPresence, Count: 1},
			{Country: cc("de"), Role: RoleProvider, Reason: 0, Count: 1},
			{Country: cc("de"), Role: RoleProvider, Reason: Reason(NumReasons + 1), Count: 1},
			{Country: cc("de"), Role: RoleProvider, Reason: ReasonPresence, Count: 0},
		} {
			if _, err := EncodeCensus(&Census{NoID: 1, Excluded: []Exclusion{x}}); !errors.Is(err, ErrExcluded) {
				t.Fatalf("%+v: %v", x, err)
			}
		}
		x := Exclusion{Country: cc("de"), Role: RoleProvider, Reason: ReasonPresence, Count: 1}
		if _, err := EncodeCensus(&Census{NoID: 1, Excluded: []Exclusion{x, x}}); !errors.Is(err, ErrExcluded) {
			t.Fatalf("a repeated count: %v", err)
		}
		row := encodeExclusions([]Exclusion{x})
		for _, data := range [][]byte{row[:7], slices.Concat(row, row), bytes.Repeat([]byte{0}, 8)} {
			if _, err := DecodeExclusions(data); !errors.Is(err, ErrExcluded) {
				t.Fatalf("%x: %v", data, err)
			}
		}
	})
	t.Run("operator", func(t *testing.T) {
		if _, err := EncodeCensus(&Census{NoID: PoolIDBase}); !errors.Is(err, ErrInput) {
			t.Fatal(err)
		}
	})
}

// §4.3: the census header decodes only from its canonical bytes; every other
// spelling of it is refused, and so is a header that is canonical but names
// another pool, other countries or bad samples.
func TestCensusHeaderCanonical(t *testing.T) {
	objects, err := EncodeCensus(epochCensus(10))
	if err != nil {
		t.Fatal(err)
	}
	header := string(objects.Header)
	if _, err := DecodeCensusHeader(objects.Header); err != nil {
		t.Fatal(err)
	}
	replace := func(old, new string) []byte {
		if !strings.Contains(header, old) {
			t.Fatalf("%q not in header", old)
		}
		return []byte(strings.Replace(header, old, new, 1))
	}
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"whitespace", replace(`"no_id":1`, `"no_id": 1`), ErrCensusNotCanonical},
		{"trailing newline", []byte(header + "\n"), ErrCensusTrailingData},
		{"unknown field", replace(`{"schema"`, `{"x":1,"schema"`), ErrCensusDecode},
		{"integer as float", replace(`"epoch":10`, `"epoch":10.0`), ErrCensusDecode},
		{"negative", replace(`"epoch":10`, `"epoch":-10`), ErrCensusDecode},
		{"leading zero", replace(`"epoch":10`, `"epoch":010`), ErrCensusDecode},
		{"escaped string", replace(`"schema":"urnetwork`, `"schema":"`+"\\"+`u0075rnetwork`), ErrCensusNotCanonical},
		{"key order", replace(`"no_id":1,"pool_id":4294967297`, `"pool_id":4294967297,"no_id":1`), ErrCensusNotCanonical},
		{"null rows", replace(`"rows":[`+refList(objects.Rows)+`]`, `"rows":null`), ErrCensusNull},
		{"null samples", replace(`"excused_samples":[3,4]`, `"excused_samples":null`), ErrCensusNull},
		{"schema", replace(CensusSchema, "urnetwork-ctf-census-v2"), ErrCensusSchema},
		{"pool", replace(`"pool_id":4294967297`, `"pool_id":4294967298`), ErrCensusPool},
		{"operator at 2^32", replace(`"no_id":1,"pool_id":4294967297`, `"no_id":4294967296,"pool_id":8589934592`), ErrCensusPool},
		{"countries", replace(CountriesRef.String(), ledger.RefOf([]byte("ad")).String()), ErrCensusCountries},
		{"sample 672", replace(`"excused_samples":[3,4]`, `"excused_samples":[3,672]`), ErrCensusSamples},
		{"samples unsorted", replace(`"excused_samples":[3,4]`, `"excused_samples":[4,3]`), ErrCensusSamples},
		{"reference case", replace(objects.Rows[0].String(), strings.ToUpper(objects.Rows[0].String())), ledger.ErrRef},
		{"too large", []byte(header + strings.Repeat(" ", ledger.MaxHeaderBytes)), ErrCensusTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := DecodeCensusHeader(c.data); !errors.Is(err, c.want) {
				t.Fatalf("%v, want %v", err, c.want)
			}
		})
	}
	t.Run("candidates", func(t *testing.T) {
		data := replace(`"candidates":18`, `"candidates":19`)
		objects.Objects[ledger.RefOf(data)] = data
		if _, err := DecodeCensus(data, objects.Objects); !errors.Is(err, ErrCensusCandidates) {
			t.Fatal(err)
		}
	})
}

func refList(refs []ledger.Ref) string {
	values := refStrings(refs)
	for i := range values {
		values[i] = `"` + values[i] + `"`
	}
	return strings.Join(values, ",")
}

// The census hash is canonical: a header with any byte changed is refused,
// or is a canonical header of another census, with another hash.
func TestCensusHeaderMutations(t *testing.T) {
	objects, err := EncodeCensus(epochCensus(10))
	if err != nil {
		t.Fatal(err)
	}
	accepted := 0
	for i := range objects.Header {
		for _, delta := range []byte{1, 0x20, 0x80} {
			data := bytes.Clone(objects.Header)
			data[i] ^= delta
			h, err := DecodeCensusHeader(data)
			if err != nil {
				continue
			}
			accepted++
			if ledger.RefOf(data) == objects.Hash {
				t.Fatal("a mutation kept the hash")
			}
			raw := censusHeaderJSON{Schema: CensusSchema, NoID: h.NoID, PoolID: h.PoolID, Epoch: h.Epoch, Countries: CountriesRef.String(),
				Rows: refStrings(h.Rows), Candidates: h.Candidates, ExcusedSamples: []uint64{}, Excluded: h.Excluded.String()}
			for _, s := range h.ExcusedSamples {
				raw.ExcusedSamples = append(raw.ExcusedSamples, uint64(s))
			}
			if !bytes.Equal(raw.appendJSON(nil), data) {
				t.Fatalf("byte %d: accepted bytes that are not the canonical form of what they decode to", i)
			}
		}
	}
	t.Logf("%d of %d single-byte mutations decode, each as another census", accepted, 3*len(objects.Header))
}
