//go:build linux || darwin

// The head arithmetic golden pins every exact output of the validator's head
// score path (shared-prefix raw scores, the EMA fold and its transcript, the
// per-UID scores, the top-N selection and their evidence checks) over recorded
// constructed inputs. It was recorded from the validator's own implementation
// before that arithmetic moved to protocol/headscore, so any output change
// across the move, or later, fails here. protocol/headscore checks its API
// against the same file.
package validator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"math/rand/v2"
	"os"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

const headArithmeticGoldenPath = "../protocol/headscore/testdata/head-arithmetic-v1.json"

// Set SN_HEAD_GOLDEN_UPDATE=1 to rewrite the file from the current code.
const headArithmeticGoldenUpdateEnv = "SN_HEAD_GOLDEN_UPDATE"

const headArithmeticGoldenSchema = "urnetwork-sn-head-arithmetic-golden-v1"

// Keys and prefixes are listed once per census or sequence, in canonical
// order, and referenced by index.
type headGoldenKey struct {
	FleetID    string `json:"fleet_id"`
	Hotkey     string `json:"hotkey"`
	Generation uint64 `json:"generation"`
	UID        uint16 `json:"uid"`
}

type headGoldenFleet struct {
	Key      int   `json:"key"`
	Prefixes []int `json:"prefixes"`
}

type headGoldenKeyScore struct {
	Key   int          `json:"key"`
	Score RationalJSON `json:"score"`
}

type headGoldenUIDScore struct {
	UID   uint16       `json:"uid"`
	Score RationalJSON `json:"score"`
}

type headGoldenClaim struct {
	Prefix int    `json:"prefix"`
	Claim  uint64 `json:"claim"`
}

type headGoldenRecord struct {
	Key      int          `json:"key"`
	HasRaw   bool         `json:"has_raw"`
	Raw      RationalJSON `json:"raw"`
	HasPrior bool         `json:"has_prior"`
	Prior    RationalJSON `json:"prior"`
	Next     RationalJSON `json:"next"`
}

type headGoldenCount struct {
	Maximum  uint16 `json:"maximum"`
	Selected int    `json:"selected"`
}

type headGoldenCensus struct {
	Name     string               `json:"name"`
	Keys     []headGoldenKey      `json:"keys"`
	Prefixes []string             `json:"prefixes"`
	Fleets   []headGoldenFleet    `json:"fleets"`
	Claims   []headGoldenClaim    `json:"claims"`
	Raw      []headGoldenKeyScore `json:"raw"`
	// ExactHeadScores keys a census by UID alone: each UID takes its first
	// identity's prefixes in canonical order.
	ByUID []headGoldenUIDScore `json:"by_uid"`
}

// A tempo's raw scores are its transcript's live raw values, and the state it
// leaves is its transcript's positive next values; neither is listed twice.
type headGoldenTempo struct {
	Fleets     []headGoldenFleet  `json:"fleets"`
	Transcript []headGoldenRecord `json:"transcript"`
	// The SHA-256 of the validator's exact JSON transcript bytes.
	TranscriptSHA256 string               `json:"transcript_sha256"`
	Scores           []headGoldenUIDScore `json:"scores"`
	// Eligible UIDs in rank order; each selection takes a prefix of them.
	Ranked   []uint16          `json:"ranked"`
	Selected []headGoldenCount `json:"selected"`
}

type headGoldenSequence struct {
	Name     string            `json:"name"`
	Alpha    protocol.Rational `json:"alpha"`
	Keys     []headGoldenKey   `json:"keys"`
	Prefixes []string          `json:"prefixes"`
	Tempos   []headGoldenTempo `json:"tempos"`
}

type headGoldenRational struct {
	Input  RationalJSON `json:"input"`
	Error  string       `json:"error"`
	Output RationalJSON `json:"output"`
}

type headGoldenEvidence struct {
	Name     string         `json:"name"`
	Eligible []uint16       `json:"eligible"`
	Scores   []RationalJSON `json:"scores"`
	Selected []uint16       `json:"selected"`
	Rejected []uint16       `json:"rejected"`
	Maximum  uint16         `json:"maximum"`
	Error    string         `json:"error"`
}

type headGoldenFoldCheck struct {
	Name  string            `json:"name"`
	Alpha protocol.Rational `json:"alpha"`
	// The validator's own transcript JSON, tampered as named.
	Transcript json.RawMessage `json:"transcript"`
	Error      string          `json:"error"`
}

type headGoldenSelectionCheck struct {
	Name    string               `json:"name"`
	Inputs  []headGoldenUIDScore `json:"inputs"`
	Maximum uint16               `json:"maximum"`
	Error   string               `json:"error"`
}

type headArithmeticGolden struct {
	Schema          string                     `json:"schema"`
	Censuses        []headGoldenCensus         `json:"censuses"`
	Sequences       []headGoldenSequence       `json:"sequences"`
	Rationals       []headGoldenRational       `json:"rationals"`
	Evidence        []headGoldenEvidence       `json:"evidence"`
	FoldChecks      []headGoldenFoldCheck      `json:"fold_checks"`
	SelectionChecks []headGoldenSelectionCheck `json:"selection_checks"`
}

func headGoldenRat(numerator, denominator string) RationalJSON {
	return RationalJSON{Numerator: numerator, Denominator: denominator}
}

func headGoldenHex(value [32]byte) string { return "0x" + hex.EncodeToString(value[:]) }

func headGoldenParseHex(t *testing.T, value string) [32]byte {
	t.Helper()
	var out [32]byte
	if len(value) != 66 || value[:2] != "0x" {
		t.Fatalf("golden hash %q is not canonical", value)
	}
	raw, err := hex.DecodeString(value[2:])
	if err != nil || "0x"+hex.EncodeToString(raw) != value {
		t.Fatalf("golden hash %q is not canonical", value)
	}
	copy(out[:], raw)
	return out
}

func headGoldenError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func headGoldenEncode(t *testing.T, value *big.Rat) RationalJSON {
	t.Helper()
	encoded, err := encodeRationalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// The index tables of one census or sequence.
type headGoldenTables struct {
	keys     []FleetScoreKey
	keyIndex map[FleetScoreKey]int
	prefixes [][32]byte
	index    map[[32]byte]int
}

func newHeadGoldenTables(censuses []map[FleetScoreKey]map[[32]byte]bool) *headGoldenTables {
	self := &headGoldenTables{keyIndex: map[FleetScoreKey]int{}, index: map[[32]byte]int{}}
	keys, prefixes := map[FleetScoreKey]bool{}, map[[32]byte]bool{}
	for _, fleets := range censuses {
		for key, set := range fleets {
			keys[key] = true
			for prefix := range set {
				prefixes[prefix] = true
			}
		}
	}
	for key := range keys {
		self.keys = append(self.keys, key)
	}
	sort.Slice(self.keys, func(i, j int) bool { return self.keys[i].String() < self.keys[j].String() })
	for index, key := range self.keys {
		self.keyIndex[key] = index
	}
	for prefix := range prefixes {
		self.prefixes = append(self.prefixes, prefix)
	}
	sort.Slice(self.prefixes, func(i, j int) bool { return headGoldenHex(self.prefixes[i]) < headGoldenHex(self.prefixes[j]) })
	for index, prefix := range self.prefixes {
		self.index[prefix] = index
	}
	return self
}

func loadHeadGoldenTables(t *testing.T, keys []headGoldenKey, prefixes []string) *headGoldenTables {
	t.Helper()
	self := &headGoldenTables{keyIndex: map[FleetScoreKey]int{}, index: map[[32]byte]int{}}
	for index, key := range keys {
		value := FleetScoreKey{FleetID: headGoldenParseHex(t, key.FleetID), Hotkey: headGoldenParseHex(t, key.Hotkey), Generation: key.Generation, UID: key.UID}
		if index > 0 && self.keys[index-1].String() >= value.String() {
			t.Fatal("golden keys are not in canonical order")
		}
		self.keys = append(self.keys, value)
		self.keyIndex[value] = index
	}
	for index, prefix := range prefixes {
		value := headGoldenParseHex(t, prefix)
		if index > 0 && prefixes[index-1] >= prefix {
			t.Fatal("golden prefixes are not in canonical order")
		}
		self.prefixes = append(self.prefixes, value)
		self.index[value] = index
	}
	return self
}

func (self *headGoldenTables) keyList() []headGoldenKey {
	out := make([]headGoldenKey, len(self.keys))
	for index, key := range self.keys {
		out[index] = headGoldenKey{FleetID: headGoldenHex(key.FleetID), Hotkey: headGoldenHex(key.Hotkey), Generation: key.Generation, UID: key.UID}
	}
	return out
}

func (self *headGoldenTables) prefixList() []string {
	out := make([]string, len(self.prefixes))
	for index, prefix := range self.prefixes {
		out[index] = headGoldenHex(prefix)
	}
	return out
}

func (self *headGoldenTables) fleets(fleets map[FleetScoreKey]map[[32]byte]bool) []headGoldenFleet {
	out := make([]headGoldenFleet, 0, len(fleets))
	for key, set := range fleets {
		fleet := headGoldenFleet{Key: self.keyIndex[key], Prefixes: []int{}}
		for prefix := range set {
			fleet.Prefixes = append(fleet.Prefixes, self.index[prefix])
		}
		slices.Sort(fleet.Prefixes)
		out = append(out, fleet)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (self *headGoldenTables) fleetMap(t *testing.T, fleets []headGoldenFleet) map[FleetScoreKey]map[[32]byte]bool {
	t.Helper()
	out := make(map[FleetScoreKey]map[[32]byte]bool, len(fleets))
	for _, fleet := range fleets {
		key := self.keys[fleet.Key]
		if out[key] != nil {
			t.Fatalf("golden fleet %d is duplicated", fleet.Key)
		}
		out[key] = map[[32]byte]bool{}
		for _, prefix := range fleet.Prefixes {
			out[key][self.prefixes[prefix]] = true
		}
	}
	return out
}

func (self *headGoldenTables) keyScores(t *testing.T, values map[FleetScoreKey]*big.Rat) []headGoldenKeyScore {
	t.Helper()
	out := make([]headGoldenKeyScore, 0, len(values))
	for key, value := range values {
		index, ok := self.keyIndex[key]
		if !ok {
			t.Fatalf("golden key %s is missing from its table", key)
		}
		out = append(out, headGoldenKeyScore{Key: index, Score: headGoldenEncode(t, value)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func mustDecodeHeadGolden(t *testing.T, value RationalJSON) *big.Rat {
	t.Helper()
	decoded, err := decodeRationalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func headGoldenUIDScores(t *testing.T, values map[uint16]*big.Rat) []headGoldenUIDScore {
	t.Helper()
	out := make([]headGoldenUIDScore, 0, len(values))
	for uid, value := range values {
		out = append(out, headGoldenUIDScore{UID: uid, Score: headGoldenEncode(t, value)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out
}

// The census outputs of the three raw-score entry points. The compact entry
// point takes its census from the caller, as planReleaseHeadV2Raw supplies it.
func headGoldenCensusOutputs(t *testing.T, tables *headGoldenTables, fleets map[FleetScoreKey]map[[32]byte]bool) ([]headGoldenClaim, []headGoldenKeyScore, []headGoldenUIDScore) {
	t.Helper()
	claims := map[[32]byte]uint64{}
	for _, prefixes := range fleets {
		for prefix := range prefixes {
			claims[prefix]++
		}
	}
	claimList := make([]headGoldenClaim, 0, len(claims))
	for prefix, claim := range claims {
		claimList = append(claimList, headGoldenClaim{Prefix: tables.index[prefix], Claim: claim})
	}
	sort.Slice(claimList, func(i, j int) bool { return claimList[i].Prefix < claimList[j].Prefix })
	raw := releaseRawHeadScores(fleets)
	compact, err := buildReleaseHeadV2Raw(context.Background(), fleets, claims)
	if err != nil {
		t.Fatalf("compact raw scores: %v", err)
	}
	if len(compact) != len(raw) {
		t.Fatal("compact raw scores differ from the legacy census")
	}
	for key, value := range raw {
		if compact[key] == nil || compact[key].Cmp(value) != 0 {
			t.Fatalf("compact raw score of %s differs from the legacy census", key)
		}
	}
	keys := make([]FleetScoreKey, 0, len(fleets))
	for key := range fleets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	byUID := map[uint16]map[[32]byte]bool{}
	for _, key := range keys {
		if byUID[key.UID] == nil {
			byUID[key.UID] = fleets[key]
		}
	}
	return claimList, tables.keyScores(t, raw), headGoldenUIDScores(t, ExactHeadScores(byUID))
}

// One tempo of the exact store fold, from the store's state before it.
func headGoldenTempoOutputs(t *testing.T, tables *headGoldenTables, store *HeadEMAStore, fleets map[FleetScoreKey]map[[32]byte]bool, alpha protocol.Rational) headGoldenTempo {
	t.Helper()
	raw := releaseRawHeadScores(fleets)
	out, records, err := store.foldWithLock(raw, alpha)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if err := verifyHeadEMAFold(records, alpha); err != nil {
		t.Fatalf("fold verification: %v", err)
	}
	recomputed, err := headEMAOutput(records)
	if err != nil || len(recomputed) != len(out) {
		t.Fatalf("fold output: %v", err)
	}
	for uid, value := range out {
		if recomputed[uid] == nil || recomputed[uid].Cmp(value) != 0 {
			t.Fatalf("fold output of UID %d differs from its transcript", uid)
		}
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	tempo := headGoldenTempo{Fleets: tables.fleets(fleets), Transcript: []headGoldenRecord{}, TranscriptSHA256: hex.EncodeToString(digest[:]), Scores: headGoldenUIDScores(t, out), Ranked: []uint16{}}
	live, next := map[FleetScoreKey]*big.Rat{}, map[FleetScoreKey]*big.Rat{}
	for _, record := range records {
		index, ok := tables.keyIndex[record.Key]
		if !ok {
			t.Fatalf("transcript key %s is missing from its table", record.Key)
		}
		tempo.Transcript = append(tempo.Transcript, headGoldenRecord{Key: index, HasRaw: record.HasRaw, Raw: record.Raw, HasPrior: record.HasPrior, Prior: record.Prior, Next: record.Next})
		if record.HasRaw {
			live[record.Key] = mustDecodeHeadGolden(t, record.Raw)
		}
		if value := mustDecodeHeadGolden(t, record.Next); value.Sign() != 0 {
			next[record.Key] = value
		}
	}
	// The transcript carries exactly the raw scores and the state left behind.
	if len(live) != len(raw) {
		t.Fatal("the transcript's live identities differ from the raw census")
	}
	for key, value := range raw {
		if live[key] == nil || live[key].Cmp(value) != 0 {
			t.Fatalf("the transcript's raw score of %s differs", key)
		}
	}
	if len(next) != len(store.values) {
		t.Fatal("the transcript's positive next values differ from the state")
	}
	for _, entry := range store.values {
		if next[entry.Key] == nil || next[entry.Key].Cmp(entryRat(entry)) != 0 {
			t.Fatalf("the state of %s differs from its transcript", entry.Key)
		}
	}
	uids := make([]uint16, 0, len(out))
	for uid := range out {
		uids = append(uids, uid)
	}
	slices.Sort(uids)
	eligible := make([]ExactWeightInput, 0, len(uids))
	for _, uid := range uids {
		eligible = append(eligible, ExactWeightInput{UID: uid, Score: out[uid]})
	}
	for _, maximum := range []uint16{200, 7, 1} {
		selection, err := selectHeadFleets(eligible, maximum)
		if err != nil {
			t.Fatalf("selection: %v", err)
		}
		ranked := append(headSelectionUIDs(selection.Selected), headSelectionUIDs(selection.Rejected)...)
		if maximum == 200 {
			tempo.Ranked = append(tempo.Ranked, ranked...)
		} else if !slices.Equal(ranked, tempo.Ranked) {
			t.Fatal("the rank order depends on the head maximum")
		}
		scores := make([]RationalJSON, 0, len(ranked))
		for _, input := range append(append([]ExactWeightInput(nil), selection.Selected...), selection.Rejected...) {
			if input.Score.Cmp(out[input.UID]) != 0 {
				t.Fatal("selection changed a score")
			}
			scores = append(scores, headGoldenEncode(t, input.Score))
		}
		if len(ranked) != 0 {
			if err := ValidateHeadSelectionEvidence(ranked, scores, headSelectionUIDs(selection.Selected), headSelectionUIDs(selection.Rejected), maximum); err != nil {
				t.Fatalf("selection evidence: %v", err)
			}
		}
		tempo.Selected = append(tempo.Selected, headGoldenCount{Maximum: maximum, Selected: len(selection.Selected)})
	}
	return tempo
}

// Constructed inputs: a deterministic population with shared prefixes, empty
// prefix sets, several identities per UID, generation changes, UIDs that
// change hands, identities that leave and return, exact ties and more fleets
// than head seats. Only the recorded inputs are replayed by the test.
type headGoldenPopulation struct {
	random   *rand.Rand
	prefixes [][32]byte
	keys     []FleetScoreKey
}

func newHeadGoldenPopulation(seed uint64, fleets, prefixes int) *headGoldenPopulation {
	self := &headGoldenPopulation{random: rand.New(rand.NewPCG(seed, seed^0x5eed))}
	for range prefixes {
		self.prefixes = append(self.prefixes, self.bytes())
	}
	hotkeys := map[uint16][32]byte{}
	uids := self.random.Perm(1024)
	for index := range fleets {
		uid := uint16(uids[index%len(uids)])
		if index%9 == 0 && len(self.keys) != 0 {
			// A second identity of an existing UID: another fleet of the
			// same hotkey, or a later generation of the same fleet.
			previous := self.keys[self.random.IntN(len(self.keys))]
			uid = previous.UID
			if index%2 == 0 {
				next := previous
				next.Generation++
				self.keys = append(self.keys, next)
				continue
			}
		}
		hotkey, ok := hotkeys[uid]
		if !ok {
			hotkey = self.bytes()
			hotkeys[uid] = hotkey
		}
		generation := uint64(1 + self.random.IntN(3))
		if index%17 == 0 {
			generation = 1<<40 + uint64(index)
		}
		self.keys = append(self.keys, FleetScoreKey{FleetID: self.bytes(), Hotkey: hotkey, Generation: generation, UID: uid})
	}
	return self
}

func (self *headGoldenPopulation) bytes() [32]byte {
	var out [32]byte
	for index := range out {
		out[index] = byte(self.random.UintN(256))
	}
	return out
}

// One tempo's census: most identities are live, some with no hops; shared
// prefixes come from a small hot pool so claims range over several values.
func (self *headGoldenPopulation) tempo(live float64) map[FleetScoreKey]map[[32]byte]bool {
	fleets := map[FleetScoreKey]map[[32]byte]bool{}
	for _, key := range self.keys {
		if self.random.Float64() >= live {
			continue
		}
		prefixes := map[[32]byte]bool{}
		count := 1 + self.random.IntN(5)
		if self.random.IntN(15) == 0 {
			count = 0 // live, but no hops this tempo
		}
		for range count {
			if self.random.IntN(3) == 0 {
				prefixes[self.prefixes[self.random.IntN(min(10, len(self.prefixes)))]] = true
			} else {
				prefixes[self.prefixes[self.random.IntN(len(self.prefixes))]] = true
			}
		}
		fleets[key] = prefixes
	}
	return fleets
}

// Rebind some identities to a new generation and hand some UIDs to new
// hotkeys and fleets, as happens between tempos on chain.
func (self *headGoldenPopulation) churn() {
	for index := range self.keys {
		switch self.random.IntN(40) {
		case 0:
			self.keys[index].Generation++
		case 1:
			self.keys[index].Hotkey = self.bytes()
			self.keys[index].FleetID = self.bytes()
			self.keys[index].Generation = 1
		}
	}
}

func headGoldenSequenceOutputs(t *testing.T, name string, alpha protocol.Rational, censuses []map[FleetScoreKey]map[[32]byte]bool) headGoldenSequence {
	t.Helper()
	tables := newHeadGoldenTables(censuses)
	store := &HeadEMAStore{values: map[string]headEMAEntry{}}
	sequence := headGoldenSequence{Name: name, Alpha: alpha, Keys: tables.keyList(), Prefixes: tables.prefixList()}
	for _, fleets := range censuses {
		sequence.Tempos = append(sequence.Tempos, headGoldenTempoOutputs(t, tables, store, fleets, alpha))
	}
	return sequence
}

func headGoldenTieKey(fleet byte, uid uint16) FleetScoreKey {
	return FleetScoreKey{FleetID: [32]byte{fleet}, Hotkey: [32]byte{byte(uid), byte(uid >> 8), 0x7e}, Generation: 1, UID: uid}
}

func headGoldenDecode(t *testing.T, input RationalJSON) headGoldenRational {
	t.Helper()
	value, err := decodeRationalJSON(input)
	item := headGoldenRational{Input: input, Error: headGoldenError(err)}
	if err == nil {
		item.Output = headGoldenEncode(t, value)
	}
	return item
}

func headGoldenSelectionError(t *testing.T, check headGoldenSelectionCheck) string {
	t.Helper()
	inputs := make([]ExactWeightInput, len(check.Inputs))
	for index, input := range check.Inputs {
		value, err := decodeRationalJSON(input.Score)
		if err != nil {
			t.Fatal(err)
		}
		inputs[index] = ExactWeightInput{UID: input.UID, Score: value}
	}
	_, err := selectHeadFleets(inputs, check.Maximum)
	return headGoldenError(err)
}

func buildHeadArithmeticGolden(t *testing.T) headArithmeticGolden {
	t.Helper()
	golden := headArithmeticGolden{Schema: headArithmeticGoldenSchema}
	// Censuses for the raw score entry points.
	single := FleetScoreKey{FleetID: [32]byte{1}, Hotkey: [32]byte{2}, Generation: 1, UID: 3}
	other := FleetScoreKey{FleetID: [32]byte{4}, Hotkey: [32]byte{5}, Generation: 2, UID: 6}
	censuses := []struct {
		name   string
		fleets map[FleetScoreKey]map[[32]byte]bool
	}{
		{"empty", map[FleetScoreKey]map[[32]byte]bool{}},
		{"one fleet without hops", map[FleetScoreKey]map[[32]byte]bool{single: {}}},
		{"one fleet with three prefixes", map[FleetScoreKey]map[[32]byte]bool{single: {{0xa}: true, {0xb}: true, {0xc}: true}}},
		{"two fleets sharing every prefix", map[FleetScoreKey]map[[32]byte]bool{single: {{0xa}: true, {0xb}: true}, other: {{0xa}: true, {0xb}: true}}},
		{"population", newHeadGoldenPopulation(11, 205, 120).tempo(0.9)},
	}
	for _, census := range censuses {
		tables := newHeadGoldenTables([]map[FleetScoreKey]map[[32]byte]bool{census.fleets})
		claims, raw, byUID := headGoldenCensusOutputs(t, tables, census.fleets)
		golden.Censuses = append(golden.Censuses, headGoldenCensus{Name: census.name, Keys: tables.keyList(), Prefixes: tables.prefixList(), Fleets: tables.fleets(census.fleets), Claims: claims, Raw: raw, ByUID: byUID})
	}
	// The mainnet α over a churning population larger than the head.
	population := newHeadGoldenPopulation(25, 250, 110)
	var tempos []map[FleetScoreKey]map[[32]byte]bool
	for index := range 5 {
		live := 0.9
		if index == 3 {
			live = 0.25 // most fleets miss a tempo and decay
		}
		tempos = append(tempos, population.tempo(live))
		population.churn()
	}
	golden.Sequences = append(golden.Sequences, headGoldenSequenceOutputs(t, "mainnet alpha population", protocol.Rational{Numerator: 1, Denominator: 4}, tempos))
	// Exact ties at the boundary, a returning identity and a new generation.
	a, b, c, d := headGoldenTieKey(1, 40), headGoldenTieKey(2, 9), headGoldenTieKey(3, 12), headGoldenTieKey(4, 0)
	cNext := c
	cNext.Generation = 2
	shared, own := [32]byte{0xee}, [32]byte{0xdd}
	ties := []map[FleetScoreKey]map[[32]byte]bool{
		{a: {shared: true, {1}: true}, b: {shared: true, {2}: true}, c: {{3}: true}, d: {}},
		{a: {shared: true}, b: {shared: true}, c: {{3}: true, own: true}},
		{a: {}, cNext: {own: true}, d: {{4}: true}},
		{b: {shared: true}, c: {{3}: true}},
	}
	golden.Sequences = append(golden.Sequences, headGoldenSequenceOutputs(t, "ties and generations", protocol.Rational{Numerator: 3, Denominator: 10}, ties))
	small := newHeadGoldenPopulation(77, 12, 10)
	edges := []map[FleetScoreKey]map[[32]byte]bool{small.tempo(0.8), small.tempo(0.8), small.tempo(0.5)}
	golden.Sequences = append(golden.Sequences, headGoldenSequenceOutputs(t, "alpha one", protocol.Rational{Numerator: 1, Denominator: 1}, edges))
	golden.Sequences = append(golden.Sequences, headGoldenSequenceOutputs(t, "alpha zero", protocol.Rational{Numerator: 0, Denominator: 5}, edges))
	golden.Sequences = append(golden.Sequences, headGoldenSequenceOutputs(t, "unreduced alpha", protocol.Rational{Numerator: 2, Denominator: 8}, edges))
	// Canonical rational decoding.
	for _, pair := range [][2]string{
		{"0", "1"}, {"7", "4"}, {"14", "8"}, {"-1", "2"}, {"1", "0"}, {"1", "-3"}, {"+1", "2"}, {"01", "2"}, {"1", "02"},
		{"", "1"}, {"1", ""}, {"0", "5"}, {"1_0", "3"}, {"0x10", "3"}, {" 1", "3"}, {"123456789012345678901234567890", "7"},
	} {
		golden.Rationals = append(golden.Rationals, headGoldenDecode(t, headGoldenRat(pair[0], pair[1])))
	}
	// Selection evidence: the canonical boundary and each kind of refusal.
	eligible := []uint16{20, 5, 31, 7}
	scores := []RationalJSON{headGoldenRat("9", "2"), headGoldenRat("3", "1"), headGoldenRat("3", "1"), headGoldenRat("1", "3")}
	evidence := []headGoldenEvidence{
		{Name: "canonical", Eligible: eligible, Scores: scores, Selected: []uint16{20, 5}, Rejected: []uint16{31, 7}, Maximum: 2},
		{Name: "all selected", Eligible: eligible, Scores: scores, Selected: eligible, Rejected: []uint16{}, Maximum: 200},
		{Name: "swapped boundary", Eligible: eligible, Scores: scores, Selected: []uint16{20, 31}, Rejected: []uint16{5, 7}, Maximum: 2},
		{Name: "tie out of order", Eligible: []uint16{20, 31, 5, 7}, Scores: scores, Selected: []uint16{20, 31}, Rejected: []uint16{5, 7}, Maximum: 2},
		{Name: "non-canonical score", Eligible: eligible, Scores: []RationalJSON{headGoldenRat("18", "4"), scores[1], scores[2], scores[3]}, Selected: []uint16{20, 5}, Rejected: []uint16{31, 7}, Maximum: 2},
		{Name: "zero score", Eligible: eligible, Scores: []RationalJSON{scores[0], scores[1], scores[2], headGoldenRat("0", "1")}, Selected: []uint16{20, 5}, Rejected: []uint16{31, 7}, Maximum: 2},
		{Name: "duplicate uid", Eligible: []uint16{20, 5, 5, 7}, Scores: scores, Selected: []uint16{20, 5}, Rejected: []uint16{5, 7}, Maximum: 2},
		{Name: "zero maximum", Eligible: eligible, Scores: scores, Selected: []uint16{}, Rejected: eligible, Maximum: 0},
		{Name: "length mismatch", Eligible: eligible, Scores: scores[:3], Selected: []uint16{20, 5}, Rejected: []uint16{31, 7}, Maximum: 2},
	}
	for _, item := range evidence {
		item.Error = headGoldenError(ValidateHeadSelectionEvidence(item.Eligible, item.Scores, item.Selected, item.Rejected, item.Maximum))
		golden.Evidence = append(golden.Evidence, item)
	}
	// Fold verification over a recorded transcript and its tampered forms.
	tieStore := &HeadEMAStore{values: map[string]headEMAEntry{}}
	alpha := protocol.Rational{Numerator: 3, Denominator: 10}
	if _, _, err := tieStore.foldWithLock(releaseRawHeadScores(ties[0]), alpha); err != nil {
		t.Fatal(err)
	}
	_, records, err := tieStore.foldWithLock(releaseRawHeadScores(ties[1]), alpha)
	if err != nil || len(records) < 3 {
		t.Fatalf("tie transcript: %v", err)
	}
	tamper := func(name string, alpha protocol.Rational, change func([]HeadEMAMeasurement) []HeadEMAMeasurement) {
		changed := change(append([]HeadEMAMeasurement(nil), records...))
		transcript, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		golden.FoldChecks = append(golden.FoldChecks, headGoldenFoldCheck{Name: name, Alpha: alpha, Transcript: transcript, Error: headGoldenError(verifyHeadEMAFold(changed, alpha))})
	}
	same := func(value []HeadEMAMeasurement) []HeadEMAMeasurement { return value }
	tamper("recorded", alpha, same)
	tamper("another alpha", protocol.Rational{Numerator: 1, Denominator: 4}, same)
	tamper("alpha over one", protocol.Rational{Numerator: 5, Denominator: 4}, same)
	tamper("alpha zero denominator", protocol.Rational{Numerator: 0, Denominator: 0}, same)
	tamper("empty", alpha, func([]HeadEMAMeasurement) []HeadEMAMeasurement { return []HeadEMAMeasurement{} })
	tamper("reordered", alpha, func(value []HeadEMAMeasurement) []HeadEMAMeasurement {
		value[0], value[1] = value[1], value[0]
		return value
	})
	tamper("duplicated", alpha, func(value []HeadEMAMeasurement) []HeadEMAMeasurement {
		return append([]HeadEMAMeasurement{value[0]}, value...)
	})
	tamper("changed next", alpha, func(value []HeadEMAMeasurement) []HeadEMAMeasurement {
		value[0].Next = headGoldenRat("1", "1000003")
		return value
	})
	tamper("non-canonical next", alpha, func(value []HeadEMAMeasurement) []HeadEMAMeasurement {
		value[0].Next = headGoldenRat("0"+value[0].Next.Numerator, value[0].Next.Denominator)
		return value
	})
	tamper("neither raw nor prior", alpha, func(value []HeadEMAMeasurement) []HeadEMAMeasurement {
		value[0].HasRaw, value[0].HasPrior = false, false
		return value
	})
	tamper("absent raw with a value", alpha, func(value []HeadEMAMeasurement) []HeadEMAMeasurement {
		for index := range value {
			if value[index].HasRaw && value[index].HasPrior && value[index].Raw.Numerator != "0" {
				value[index].HasRaw = false
				return value
			}
		}
		t.Fatal("tie transcript has no live identity with a prior and hops")
		return value
	})
	tamper("zero prior", alpha, func(value []HeadEMAMeasurement) []HeadEMAMeasurement {
		value[0].HasPrior, value[0].Prior = true, headGoldenRat("0", "1")
		return value
	})
	tamper("absent prior with a value", alpha, func(value []HeadEMAMeasurement) []HeadEMAMeasurement {
		value[0].HasPrior = false
		return value
	})
	tamper("negative raw", alpha, func(value []HeadEMAMeasurement) []HeadEMAMeasurement {
		value[0].Raw = headGoldenRat("-1", "1")
		return value
	})
	// Selection refusals and the zero-score skip.
	for _, check := range []headGoldenSelectionCheck{
		{Name: "zero maximum", Inputs: []headGoldenUIDScore{{UID: 1, Score: headGoldenRat("1", "1")}}, Maximum: 0},
		{Name: "duplicate", Inputs: []headGoldenUIDScore{{UID: 1, Score: headGoldenRat("1", "1")}, {UID: 1, Score: headGoldenRat("2", "1")}}, Maximum: 3},
		{Name: "zero scores skipped", Inputs: []headGoldenUIDScore{{UID: 3, Score: headGoldenRat("0", "1")}, {UID: 2, Score: headGoldenRat("5", "2")}, {UID: 9, Score: headGoldenRat("5", "2")}}, Maximum: 1},
	} {
		check.Error = headGoldenSelectionError(t, check)
		golden.SelectionChecks = append(golden.SelectionChecks, check)
	}
	return golden
}

// Every recorded input, run through the current validator code, reproduces
// the recorded output. Only the recorded inputs are replayed, so a change in
// the input generator cannot hide or fake a difference.
func TestHeadArithmeticGoldenOutputsAreUnchanged(t *testing.T) {
	if os.Getenv(headArithmeticGoldenUpdateEnv) == "1" {
		encoded, err := json.Marshal(buildHeadArithmeticGolden(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("../protocol/headscore/testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(headArithmeticGoldenPath, append(encoded, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s (%d bytes)", headArithmeticGoldenPath, len(encoded)+1)
	}
	recorded, err := os.ReadFile(headArithmeticGoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var golden headArithmeticGolden
	if err := json.Unmarshal(recorded, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.Schema != headArithmeticGoldenSchema || len(golden.Censuses) == 0 || len(golden.Sequences) == 0 || len(golden.Rationals) == 0 || len(golden.Evidence) == 0 || len(golden.FoldChecks) == 0 || len(golden.SelectionChecks) == 0 {
		t.Fatal("the recorded golden file is incomplete")
	}
	for _, census := range golden.Censuses {
		tables := loadHeadGoldenTables(t, census.Keys, census.Prefixes)
		claims, raw, byUID := headGoldenCensusOutputs(t, tables, tables.fleetMap(t, census.Fleets))
		if !slices.Equal(claims, census.Claims) || !slices.Equal(raw, census.Raw) || !slices.Equal(byUID, census.ByUID) {
			t.Errorf("census %q replays to different raw scores", census.Name)
		}
	}
	for _, sequence := range golden.Sequences {
		tables := loadHeadGoldenTables(t, sequence.Keys, sequence.Prefixes)
		store := &HeadEMAStore{values: map[string]headEMAEntry{}}
		for index, tempo := range sequence.Tempos {
			replayed := headGoldenTempoOutputs(t, tables, store, tables.fleetMap(t, tempo.Fleets), sequence.Alpha)
			if !reflect.DeepEqual(replayed, tempo) {
				t.Errorf("sequence %q tempo %d replays to different outputs", sequence.Name, index)
			}
		}
	}
	for _, item := range golden.Rationals {
		if got := headGoldenDecode(t, item.Input); got != item {
			t.Errorf("rational %+v decodes to %+v, recorded %+v", item.Input, got, item)
		}
	}
	for _, item := range golden.Evidence {
		if got := headGoldenError(ValidateHeadSelectionEvidence(item.Eligible, item.Scores, item.Selected, item.Rejected, item.Maximum)); got != item.Error {
			t.Errorf("evidence %q: %q, recorded %q", item.Name, got, item.Error)
		}
	}
	for _, item := range golden.FoldChecks {
		var records []HeadEMAMeasurement
		if err := json.Unmarshal(item.Transcript, &records); err != nil {
			t.Fatal(err)
		}
		if got := headGoldenError(verifyHeadEMAFold(records, item.Alpha)); got != item.Error {
			t.Errorf("fold check %q: %q, recorded %q", item.Name, got, item.Error)
		}
	}
	for _, item := range golden.SelectionChecks {
		if got := headGoldenSelectionError(t, item); got != item.Error {
			t.Errorf("selection check %q: %q, recorded %q", item.Name, got, item.Error)
		}
	}
}

// The recorded transcript records name the same keys as their exact bytes.
func TestHeadArithmeticGoldenTranscriptsMatchTheirDigests(t *testing.T) {
	recorded, err := os.ReadFile(headArithmeticGoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var golden headArithmeticGolden
	if err := json.Unmarshal(recorded, &golden); err != nil {
		t.Fatal(err)
	}
	for _, sequence := range golden.Sequences {
		tables := loadHeadGoldenTables(t, sequence.Keys, sequence.Prefixes)
		for index, tempo := range sequence.Tempos {
			records := make([]HeadEMAMeasurement, len(tempo.Transcript))
			for position, record := range tempo.Transcript {
				records[position] = HeadEMAMeasurement{Key: tables.keys[record.Key], HasRaw: record.HasRaw, Raw: record.Raw, HasPrior: record.HasPrior, Prior: record.Prior, Next: record.Next}
			}
			encoded, err := json.Marshal(records)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(encoded)
			if hex.EncodeToString(digest[:]) != tempo.TranscriptSHA256 {
				t.Errorf("sequence %q tempo %d transcript differs from its recorded bytes", sequence.Name, index)
			}
		}
	}
}
