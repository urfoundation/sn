package headscore_test

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol/headscore"
)

var quarter = headscore.Ratio{Numerator: 1, Denominator: 4}

func key(fleet byte, generation uint64, uid uint16) headscore.FleetKey {
	return headscore.FleetKey{FleetID: [32]byte{fleet}, Hotkey: [32]byte{byte(uid), 0x5e}, Generation: generation, UID: uid}
}

func rat(numerator, denominator int64) *big.Rat { return big.NewRat(numerator, denominator) }

// The validator persists FleetKey in its EMA state and transcripts. Its text
// order and JSON form are part of that format and must not drift.
func TestFleetKeyTextAndJSONArePinned(t *testing.T) {
	value := headscore.FleetKey{FleetID: [32]byte{0xab}, Hotkey: [32]byte{0x01, 0x02}, Generation: 1 << 40, UID: 25}
	wantText := "ab" + strings.Repeat("0", 62) + ":0102" + strings.Repeat("0", 60) + ":1099511627776:25"
	if value.String() != wantText {
		t.Fatalf("String() = %q", value.String())
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	array := func(prefix ...int) string {
		values := make([]string, 32)
		for index := range values {
			values[index] = "0"
			if index < len(prefix) {
				values[index] = strconv.Itoa(prefix[index])
			}
		}
		return "[" + strings.Join(values, ",") + "]"
	}
	want := `{"FleetID":` + array(171) + `,"Hotkey":` + array(1, 2) + `,"Generation":1099511627776,"UID":25}`
	if string(encoded) != want {
		t.Fatalf("JSON = %s", encoded)
	}
	// The canonical order is the text's byte order, so generation 10 sorts
	// before generation 9 of the same fleet.
	if headscore.CompareKeys(key(1, 10, 3), key(1, 9, 3)) >= 0 || headscore.CompareKeys(key(1, 9, 3), key(1, 9, 3)) != 0 {
		t.Fatal("CompareKeys is not the canonical text order")
	}
}

// The rank order is the validator's: a higher score first, ties to the lower
// UID; zero scores take no seat; the boundary keeps exactly maximum seats.
func TestSelectionOrderAndBoundary(t *testing.T) {
	inputs := []headscore.UIDScore{{UID: 9, Score: rat(3, 2)}, {UID: 4, Score: rat(3, 2)}, {UID: 7, Score: new(big.Rat)}, {UID: 1, Score: rat(1, 3)}, {UID: 30, Score: rat(5, 1)}}
	selection, err := headscore.Select(inputs, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(headscore.UIDs(selection.Selected), []uint16{30, 4}) || !slices.Equal(headscore.UIDs(selection.Rejected), []uint16{9, 1}) {
		t.Fatalf("selected %v rejected %v", headscore.UIDs(selection.Selected), headscore.UIDs(selection.Rejected))
	}
	// Scores are copied: changing an input cannot change the selection.
	inputs[4].Score.SetInt64(0)
	if selection.Selected[0].Score.Cmp(rat(5, 1)) != 0 {
		t.Fatal("selection shares its input's score")
	}
	if headscore.Compare(headscore.UIDScore{UID: 2, Score: rat(1, 1)}, headscore.UIDScore{UID: 1, Score: rat(1, 1)}) <= 0 ||
		headscore.Compare(headscore.UIDScore{UID: 2, Score: rat(2, 1)}, headscore.UIDScore{UID: 1, Score: rat(1, 1)}) >= 0 {
		t.Fatal("Compare is not score descending, then UID ascending")
	}
	for _, bad := range [][]headscore.UIDScore{{{UID: 1}}, {{UID: 1, Score: rat(-1, 1)}}, {{UID: 1, Score: rat(1, 1)}, {UID: 1, Score: rat(2, 1)}}} {
		if _, err := headscore.Select(bad, 200); err == nil {
			t.Fatalf("ambiguous inputs %v were accepted", bad)
		}
	}
	if _, err := headscore.Select(inputs, 0); err == nil {
		t.Fatal("a zero maximum was accepted")
	}
}

// One tempo at a time: a new identity starts at its raw score, a live one
// with no hops is multiplied by 1 − α, an identity that is not live keeps
// decaying but scores nothing, and a new generation inherits nothing.
func TestFoldFollowsTheEMARules(t *testing.T) {
	first, second := key(1, 1, 8), key(2, 1, 9)
	records, err := headscore.Fold(nil, map[headscore.FleetKey]*big.Rat{first: rat(4, 1), second: rat(0, 1)}, quarter)
	if err != nil {
		t.Fatal(err)
	}
	if state := headscore.State(records); len(state) != 1 || state[first].Cmp(rat(4, 1)) != 0 {
		t.Fatalf("a new identity did not start at its raw score, or a zero one was kept: %v", state)
	}
	// Live with no hops: 4 → 3.
	records, err = headscore.Fold(headscore.State(records), map[headscore.FleetKey]*big.Rat{first: new(big.Rat)}, quarter)
	if err != nil || headscore.Scores(records)[8].Cmp(rat(3, 1)) != 0 {
		t.Fatalf("live decay: %v %v", headscore.Scores(records), err)
	}
	// The next generation starts at its raw score while the old one decays
	// unscored: the UID's score is the new generation's alone.
	next := first
	next.Generation = 2
	records, err = headscore.Fold(headscore.State(records), map[headscore.FleetKey]*big.Rat{next: rat(2, 1)}, quarter)
	if err != nil {
		t.Fatal(err)
	}
	state := headscore.State(records)
	if headscore.Scores(records)[8].Cmp(rat(2, 1)) != 0 || state[first].Cmp(rat(9, 4)) != 0 || state[next].Cmp(rat(2, 1)) != 0 {
		t.Fatalf("generation isolation: scores %v state %v", headscore.Scores(records), state)
	}
	if err := headscore.VerifyFold(records, quarter); err != nil {
		t.Fatal(err)
	}
	// α·raw + (1 − α)·prior, exactly.
	value, err := headscore.Next(rat(1, 3), rat(2, 7), true, quarter)
	if err != nil || value.Cmp(new(big.Rat).Add(new(big.Rat).Mul(rat(1, 4), rat(1, 3)), new(big.Rat).Mul(rat(3, 4), rat(2, 7)))) != 0 {
		t.Fatalf("Next = %v, %v", value, err)
	}
	for _, alpha := range []headscore.Ratio{{Numerator: 1, Denominator: 0}, {Numerator: 5, Denominator: 4}} {
		if _, err := headscore.Fold(nil, nil, alpha); err == nil {
			t.Fatalf("alpha %+v was accepted", alpha)
		}
	}
	if _, err := headscore.Fold(map[headscore.FleetKey]*big.Rat{first: new(big.Rat)}, nil, quarter); err == nil {
		t.Fatal("a zero prior was accepted")
	}
	if _, err := headscore.Fold(nil, map[headscore.FleetKey]*big.Rat{first: rat(-1, 1)}, quarter); err == nil {
		t.Fatal("a negative raw score was accepted")
	}
}

// A tampered fold transcript fails verification.
func TestVerifyFoldRefusesTampering(t *testing.T) {
	records, err := headscore.Fold(map[headscore.FleetKey]*big.Rat{key(1, 1, 1): rat(1, 1)}, map[headscore.FleetKey]*big.Rat{key(2, 1, 2): rat(1, 2)}, quarter)
	if err != nil || len(records) != 2 {
		t.Fatal(err)
	}
	changed := slices.Clone(records)
	changed[0].Next = rat(1, 1)
	swapped := []headscore.FoldRecord{records[1], records[0]}
	for name, tampered := range map[string][]headscore.FoldRecord{"next": changed, "order": swapped} {
		if err := headscore.VerifyFold(tampered, quarter); err == nil {
			t.Fatalf("tampered %s was accepted", name)
		}
	}
}

// Step is exactly its parts.
func TestStepComposesTheParts(t *testing.T) {
	shared := [32]byte{0xee}
	census := map[headscore.FleetKey]map[[32]byte]bool{
		key(1, 1, 1): {shared: true, {1}: true},
		key(2, 1, 2): {shared: true},
		key(3, 1, 3): {},
	}
	prior := map[headscore.FleetKey]*big.Rat{key(2, 1, 2): rat(2, 1), key(4, 1, 4): rat(1, 1)}
	result, err := headscore.Step(prior, census, quarter, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw := headscore.RawScores(census)
	if raw[key(1, 1, 1)].Cmp(rat(3, 2)) != 0 || raw[key(2, 1, 2)].Cmp(rat(1, 2)) != 0 || raw[key(3, 1, 3)].Sign() != 0 {
		t.Fatalf("raw scores %v", raw)
	}
	// UID 2: ¼·½ + ¾·2 = 13/8 outranks UID 1's new 3/2; UID 4 is not live.
	if result.Scores[2].Cmp(rat(13, 8)) != 0 || result.Scores[1].Cmp(rat(3, 2)) != 0 || result.Scores[4] != nil || len(result.Scores) != 2 {
		t.Fatalf("scores %v", result.Scores)
	}
	if !slices.Equal(headscore.UIDs(result.Selection.Selected), []uint16{2}) || !slices.Equal(headscore.UIDs(result.Selection.Rejected), []uint16{1}) {
		t.Fatal("selection")
	}
	if result.State[key(4, 1, 4)].Cmp(rat(3, 4)) != 0 || result.State[key(3, 1, 3)] != nil {
		t.Fatalf("state %v", result.State)
	}
	// The arguments are not changed.
	if prior[key(2, 1, 2)].Cmp(rat(2, 1)) != 0 || len(prior) != 2 {
		t.Fatal("Step changed its prior state")
	}
}

// The counted-census entry point observes cancellation and refuses a prefix
// with no claim.
func TestRawScoresWithClaimsRefusals(t *testing.T) {
	fleets := map[headscore.FleetKey]map[[32]byte]bool{key(1, 1, 1): {{1}: true}}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := headscore.RawScoresWithClaims(canceled, fleets, headscore.Claims(fleets)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
	if _, err := headscore.RawScoresWithClaims(context.Background(), fleets, map[[32]byte]uint64{}); !errors.Is(err, headscore.ErrClaimCensusIncomplete) {
		t.Fatalf("incomplete census: %v", err)
	}
}

// The float view is for display; the exact form round-trips unchanged.
func TestFloatViewAndExactForm(t *testing.T) {
	third := rat(1, 3)
	if got := headscore.Float64(third); got < 0.3333333333 || got > 0.3333333334 {
		t.Fatalf("Float64 = %v", got)
	}
	if headscore.Decimal(rat(2, 3), 4) != "0.6667" || headscore.Decimal(nil, 2) != "0.00" || headscore.Float64(nil) != 0 {
		t.Fatal("Decimal or the nil view")
	}
	encoded, err := headscore.EncodeRational(rat(14, 8))
	if err != nil || encoded != (headscore.Rational{Numerator: "7", Denominator: "4"}) {
		t.Fatalf("encoded %+v %v", encoded, err)
	}
	decoded, err := headscore.DecodeRational(encoded)
	if err != nil || decoded.Cmp(rat(7, 4)) != 0 {
		t.Fatal("round trip")
	}
	for _, bad := range []headscore.Rational{{Numerator: "14", Denominator: "8"}, {Numerator: "-1", Denominator: "2"}, {Numerator: "1", Denominator: "0"}, {Numerator: "+1", Denominator: "2"}} {
		if _, err := headscore.DecodeRational(bad); err == nil {
			t.Fatalf("%+v was accepted", bad)
		}
	}
	if _, err := headscore.EncodeRational(rat(-1, 2)); err == nil {
		t.Fatal("a negative value was encoded")
	}
}

// The package stays importable by the operator with nothing but the Go
// standard library: no validator runtime, chain, file or network packages.
func TestPackageImportsOnlyTheStandardLibrary(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"os", "net", "net/http", "io/fs", "syscall", "os/exec"}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), source, raw, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.Split(path, "/")[0], ".") || slices.Contains(forbidden, path) {
				t.Errorf("%s imports %s", source, path)
			}
		}
	}
}
