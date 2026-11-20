package headscore_test

// The validator recorded testdata/head-arithmetic-v1.json from its own head
// arithmetic (validator/head_arithmetic_golden_test.go). Replaying the same
// inputs through this package's exported API, as the operator will, must give
// the same outputs and the same exact transcript bytes.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"slices"
	"sort"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol/headscore"
)

type goldenKey struct {
	FleetID    string `json:"fleet_id"`
	Hotkey     string `json:"hotkey"`
	Generation uint64 `json:"generation"`
	UID        uint16 `json:"uid"`
}

type goldenFleet struct {
	Key      int   `json:"key"`
	Prefixes []int `json:"prefixes"`
}

type goldenKeyScore struct {
	Key   int                `json:"key"`
	Score headscore.Rational `json:"score"`
}

type goldenUIDScore struct {
	UID   uint16             `json:"uid"`
	Score headscore.Rational `json:"score"`
}

type goldenRecord struct {
	Key      int                `json:"key"`
	HasRaw   bool               `json:"has_raw"`
	Raw      headscore.Rational `json:"raw"`
	HasPrior bool               `json:"has_prior"`
	Prior    headscore.Rational `json:"prior"`
	Next     headscore.Rational `json:"next"`
}

type goldenAlpha struct {
	Numerator   uint64 `json:"numerator"`
	Denominator uint64 `json:"denominator"`
}

func (self goldenAlpha) ratio() headscore.Ratio {
	return headscore.Ratio{Numerator: self.Numerator, Denominator: self.Denominator}
}

type golden struct {
	Schema   string `json:"schema"`
	Censuses []struct {
		Name     string      `json:"name"`
		Keys     []goldenKey `json:"keys"`
		Prefixes []string    `json:"prefixes"`
		Fleets   []goldenFleet
		Claims   []struct {
			Prefix int    `json:"prefix"`
			Claim  uint64 `json:"claim"`
		} `json:"claims"`
		Raw   []goldenKeyScore `json:"raw"`
		ByUID []goldenUIDScore `json:"by_uid"`
	} `json:"censuses"`
	Sequences []struct {
		Name     string      `json:"name"`
		Alpha    goldenAlpha `json:"alpha"`
		Keys     []goldenKey `json:"keys"`
		Prefixes []string    `json:"prefixes"`
		Tempos   []struct {
			Fleets           []goldenFleet    `json:"fleets"`
			Transcript       []goldenRecord   `json:"transcript"`
			TranscriptSHA256 string           `json:"transcript_sha256"`
			Scores           []goldenUIDScore `json:"scores"`
			Ranked           []uint16         `json:"ranked"`
			Selected         []struct {
				Maximum  uint16 `json:"maximum"`
				Selected int    `json:"selected"`
			} `json:"selected"`
		} `json:"tempos"`
	} `json:"sequences"`
	Rationals []struct {
		Input  headscore.Rational `json:"input"`
		Error  string             `json:"error"`
		Output headscore.Rational `json:"output"`
	} `json:"rationals"`
	Evidence []struct {
		Name     string               `json:"name"`
		Eligible []uint16             `json:"eligible"`
		Scores   []headscore.Rational `json:"scores"`
		Selected []uint16             `json:"selected"`
		Rejected []uint16             `json:"rejected"`
		Maximum  uint16               `json:"maximum"`
		Error    string               `json:"error"`
	} `json:"evidence"`
	FoldChecks []struct {
		Name       string          `json:"name"`
		Alpha      goldenAlpha     `json:"alpha"`
		Transcript json.RawMessage `json:"transcript"`
		Error      string          `json:"error"`
	} `json:"fold_checks"`
	SelectionChecks []struct {
		Name    string           `json:"name"`
		Inputs  []goldenUIDScore `json:"inputs"`
		Maximum uint16           `json:"maximum"`
		Error   string           `json:"error"`
	} `json:"selection_checks"`
}

// The validator's transcript record (validator.HeadEMAMeasurement), whose
// exact JSON bytes the golden digests pin.
type validatorRecord struct {
	Key      headscore.FleetKey `json:"key"`
	HasRaw   bool               `json:"has_raw"`
	Raw      headscore.Rational `json:"raw"`
	HasPrior bool               `json:"has_prior"`
	Prior    headscore.Rational `json:"prior"`
	Next     headscore.Rational `json:"next"`
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	raw, err := os.ReadFile("testdata/head-arithmetic-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var value golden
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if value.Schema != "urnetwork-sn-head-arithmetic-golden-v1" || len(value.Censuses) == 0 || len(value.Sequences) == 0 {
		t.Fatal("golden file is incomplete")
	}
	return value
}

func hash32(t *testing.T, value string) [32]byte {
	t.Helper()
	var out [32]byte
	raw, err := hex.DecodeString(value[2:])
	if err != nil || len(raw) != 32 {
		t.Fatalf("golden hash %q is invalid", value)
	}
	copy(out[:], raw)
	return out
}

func goldenKeys(t *testing.T, keys []goldenKey) []headscore.FleetKey {
	t.Helper()
	out := make([]headscore.FleetKey, len(keys))
	for index, key := range keys {
		out[index] = headscore.FleetKey{FleetID: hash32(t, key.FleetID), Hotkey: hash32(t, key.Hotkey), Generation: key.Generation, UID: key.UID}
	}
	return out
}

func goldenPrefixes(t *testing.T, prefixes []string) [][32]byte {
	t.Helper()
	out := make([][32]byte, len(prefixes))
	for index, prefix := range prefixes {
		out[index] = hash32(t, prefix)
	}
	return out
}

func goldenCensus(keys []headscore.FleetKey, prefixes [][32]byte, fleets []goldenFleet) map[headscore.FleetKey]map[[32]byte]bool {
	out := make(map[headscore.FleetKey]map[[32]byte]bool, len(fleets))
	for _, fleet := range fleets {
		set := map[[32]byte]bool{}
		for _, prefix := range fleet.Prefixes {
			set[prefixes[prefix]] = true
		}
		out[keys[fleet.Key]] = set
	}
	return out
}

func encode(t *testing.T, value *big.Rat) headscore.Rational {
	t.Helper()
	encoded, err := headscore.EncodeRational(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func decode(t *testing.T, value headscore.Rational) *big.Rat {
	t.Helper()
	decoded, err := headscore.DecodeRational(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func keyScores(t *testing.T, keys []headscore.FleetKey, values map[headscore.FleetKey]*big.Rat) []goldenKeyScore {
	t.Helper()
	index := map[headscore.FleetKey]int{}
	for position, key := range keys {
		index[key] = position
	}
	out := make([]goldenKeyScore, 0, len(values))
	for key, value := range values {
		out = append(out, goldenKeyScore{Key: index[key], Score: encode(t, value)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func uidScores(t *testing.T, values map[uint16]*big.Rat) []goldenUIDScore {
	t.Helper()
	out := make([]goldenUIDScore, 0, len(values))
	for uid, value := range values {
		out = append(out, goldenUIDScore{UID: uid, Score: encode(t, value)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Every census gives the recorded claims and raw scores through each of the
// package's raw-score entry points.
func TestGoldenRawScores(t *testing.T) {
	for _, census := range loadGolden(t).Censuses {
		keys, prefixes := goldenKeys(t, census.Keys), goldenPrefixes(t, census.Prefixes)
		fleets := goldenCensus(keys, prefixes, census.Fleets)
		claims := headscore.Claims(fleets)
		if len(claims) != len(census.Claims) {
			t.Fatalf("census %q has %d claims, recorded %d", census.Name, len(claims), len(census.Claims))
		}
		for _, claim := range census.Claims {
			if claims[prefixes[claim.Prefix]] != claim.Claim {
				t.Fatalf("census %q prefix %d claim differs", census.Name, claim.Prefix)
			}
		}
		raw := headscore.RawScores(fleets)
		if got := keyScores(t, keys, raw); !slices.Equal(got, census.Raw) {
			t.Fatalf("census %q raw scores differ", census.Name)
		}
		counted, err := headscore.RawScoresWithClaims(context.Background(), fleets, claims)
		if err != nil || !slices.Equal(keyScores(t, keys, counted), census.Raw) {
			t.Fatalf("census %q raw scores over a counted census differ: %v", census.Name, err)
		}
		byUID := map[uint16]map[[32]byte]bool{}
		for _, key := range keys {
			if set, live := fleets[key]; live && byUID[key.UID] == nil {
				byUID[key.UID] = set
			}
		}
		if got := uidScores(t, headscore.RawScores(byUID)); !slices.Equal(got, census.ByUID) {
			t.Fatalf("census %q UID-keyed raw scores differ", census.Name)
		}
	}
}

// Step, tempo by tempo from an empty state, reproduces every recorded
// transcript record, the transcript's exact bytes, the per-UID scores, the
// rank order and each selection boundary.
func TestGoldenTempoSequences(t *testing.T) {
	for _, sequence := range loadGolden(t).Sequences {
		keys, prefixes := goldenKeys(t, sequence.Keys), goldenPrefixes(t, sequence.Prefixes)
		index := map[headscore.FleetKey]int{}
		for position, key := range keys {
			index[key] = position
		}
		alpha := sequence.Alpha.ratio()
		state := map[headscore.FleetKey]*big.Rat{}
		for tempoIndex, tempo := range sequence.Tempos {
			result, err := headscore.Step(state, goldenCensus(keys, prefixes, tempo.Fleets), alpha, 200)
			if err != nil {
				t.Fatalf("%s tempo %d: %v", sequence.Name, tempoIndex, err)
			}
			if err := headscore.VerifyFold(result.Records, alpha); err != nil {
				t.Fatalf("%s tempo %d: own fold does not verify: %v", sequence.Name, tempoIndex, err)
			}
			if len(result.Records) != len(tempo.Transcript) {
				t.Fatalf("%s tempo %d: %d records, recorded %d", sequence.Name, tempoIndex, len(result.Records), len(tempo.Transcript))
			}
			exact := make([]validatorRecord, len(result.Records))
			for position, record := range result.Records {
				got := goldenRecord{Key: index[record.Key], HasRaw: record.HasRaw, Raw: encode(t, record.Raw), HasPrior: record.HasPrior, Prior: encode(t, record.Prior), Next: encode(t, record.Next)}
				if got != tempo.Transcript[position] {
					t.Fatalf("%s tempo %d record %d differs", sequence.Name, tempoIndex, position)
				}
				exact[position] = validatorRecord{Key: record.Key, HasRaw: got.HasRaw, Raw: got.Raw, HasPrior: got.HasPrior, Prior: got.Prior, Next: got.Next}
			}
			encoded, err := json.Marshal(exact)
			if err != nil {
				t.Fatal(err)
			}
			if digest := sha256.Sum256(encoded); hex.EncodeToString(digest[:]) != tempo.TranscriptSHA256 {
				t.Fatalf("%s tempo %d transcript bytes differ from the validator's", sequence.Name, tempoIndex)
			}
			if got := uidScores(t, result.Scores); !slices.Equal(got, tempo.Scores) {
				t.Fatalf("%s tempo %d scores differ", sequence.Name, tempoIndex)
			}
			ranked, err := headscore.Rank(result.Scores)
			if err != nil || !slices.Equal(headscore.UIDs(ranked), tempo.Ranked) || !slices.Equal(headscore.UIDs(result.Selection.Ranked()), tempo.Ranked) {
				t.Fatalf("%s tempo %d rank order differs: %v", sequence.Name, tempoIndex, err)
			}
			for _, boundary := range tempo.Selected {
				selection, err := headscore.Select(ranked, boundary.Maximum)
				if err != nil || len(selection.Selected) != boundary.Selected || !slices.Equal(headscore.UIDs(selection.Ranked()), tempo.Ranked) {
					t.Fatalf("%s tempo %d boundary at %d differs: %v", sequence.Name, tempoIndex, boundary.Maximum, err)
				}
				scores := make([]headscore.Rational, len(ranked))
				for position, score := range ranked {
					scores[position] = encode(t, score.Score)
				}
				if len(ranked) != 0 {
					if err := headscore.ValidateSelection(tempo.Ranked, scores, headscore.UIDs(selection.Selected), headscore.UIDs(selection.Rejected), boundary.Maximum); err != nil {
						t.Fatalf("%s tempo %d evidence at %d: %v", sequence.Name, tempoIndex, boundary.Maximum, err)
					}
				}
			}
			for key, value := range result.State {
				if value.Sign() <= 0 || decode(t, tempo.Transcript[slices.IndexFunc(result.Records, func(record headscore.FoldRecord) bool { return record.Key == key })].Next).Cmp(value) != 0 {
					t.Fatalf("%s tempo %d state of %s differs from its record", sequence.Name, tempoIndex, key)
				}
			}
			state = result.State
		}
	}
}

// Decoding, evidence, fold verification and selection refuse exactly what the
// validator refused, with the same messages.
func TestGoldenRefusals(t *testing.T) {
	value := loadGolden(t)
	for _, item := range value.Rationals {
		decoded, err := headscore.DecodeRational(item.Input)
		if errorText(err) != item.Error || (err == nil && encode(t, decoded) != item.Output) {
			t.Errorf("rational %+v: %v", item.Input, err)
		}
	}
	for _, item := range value.Evidence {
		if got := errorText(headscore.ValidateSelection(item.Eligible, item.Scores, item.Selected, item.Rejected, item.Maximum)); got != item.Error {
			t.Errorf("evidence %q: %q, recorded %q", item.Name, got, item.Error)
		}
	}
	for _, item := range value.FoldChecks {
		var records []validatorRecord
		if err := json.Unmarshal(item.Transcript, &records); err != nil {
			t.Fatal(err)
		}
		folds := make([]headscore.FoldRecord, len(records))
		encoded := true
		for position, record := range records {
			raw, rawErr := headscore.DecodeRational(record.Raw)
			prior, priorErr := headscore.DecodeRational(record.Prior)
			next, nextErr := headscore.DecodeRational(record.Next)
			if rawErr != nil || priorErr != nil || nextErr != nil {
				encoded = false
				break
			}
			folds[position] = headscore.FoldRecord{Key: record.Key, HasRaw: record.HasRaw, Raw: raw, HasPrior: record.HasPrior, Prior: prior, Next: next}
		}
		if !encoded {
			// A transcript this package cannot even decode was refused.
			if item.Error == "" {
				t.Errorf("fold check %q: undecodable transcript was accepted", item.Name)
			}
			continue
		}
		if got := errorText(headscore.VerifyFold(folds, item.Alpha.ratio())); got != item.Error {
			t.Errorf("fold check %q: %q, recorded %q", item.Name, got, item.Error)
		}
	}
	for _, item := range value.SelectionChecks {
		inputs := make([]headscore.UIDScore, len(item.Inputs))
		for position, input := range item.Inputs {
			inputs[position] = headscore.UIDScore{UID: input.UID, Score: decode(t, input.Score)}
		}
		_, err := headscore.Select(inputs, item.Maximum)
		if got := errorText(err); got != item.Error {
			t.Errorf("selection check %q: %q, recorded %q", item.Name, got, item.Error)
		}
	}
}
