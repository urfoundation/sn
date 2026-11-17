// The Go report decoder independently rechecks the exact canonical SCALE
// value returned by the original runtime; a subprocess's scalar is no oracle.
package main

import (
	"bytes"
	"strconv"
	"testing"
)

// Independently encode the reviewed compact layout rather than deriving test
// expectations with the production report parser.
func historicalPrincipalTestValue(stake uint64) historicalPrincipalObservation {
	query := historicalPrincipalQuery{Netuid: 25}
	copy(query.Hotkey[:], bytes.Repeat([]byte{0x11}, 32))
	copy(query.Coldkey[:], bytes.Repeat([]byte{0x33}, 32))
	raw := append([]byte{1}, query.Hotkey[:]...)
	raw = append(raw, query.Coldkey[:]...)
	for _, value := range []uint64{25, stake, 0, 0, 0, 0} {
		raw = append(raw, rootCompact(value)...)
	}
	raw = append(raw, 1)
	amount, registered := strconv.FormatUint(stake, 10), true
	return historicalPrincipalObservation{Query: query, ResultHex: nativeExecutionTestHex(raw), OpeningStakeAlpha: &amount, Registered: &registered}
}

func TestHistoricalPrincipalCanonicalResultsKeepAbsentAndZeroDistinct(t *testing.T) {
	zero := historicalPrincipalTestValue(0)
	absent := historicalPrincipalObservation{Query: zero.Query, ResultHex: "0x00"}
	for _, observation := range []historicalPrincipalObservation{zero, absent, historicalPrincipalTestValue(1<<63 + 7)} {
		if err := validateHistoricalPrincipalReport([]historicalPrincipalQuery{observation.Query}, []historicalPrincipalObservation{observation}); err != nil {
			t.Fatal("valid original principal result was refused", err)
		}
	}
	absent.OpeningStakeAlpha = zero.OpeningStakeAlpha
	if err := validateHistoricalPrincipalReport([]historicalPrincipalQuery{absent.Query}, []historicalPrincipalObservation{absent}); err == nil {
		t.Fatal("absent original API result became observed zero")
	}
}

func TestHistoricalPrincipalReportRefusesForeignLayoutAndDerivedScalar(t *testing.T) {
	valid := historicalPrincipalTestValue(14)
	if err := validateHistoricalPrincipalReport([]historicalPrincipalQuery{valid.Query}, []historicalPrincipalObservation{valid}); err != nil {
		t.Fatal("canonical original result baseline", err)
	}
	for _, mutate := range []func(*historicalPrincipalObservation){
		func(value *historicalPrincipalObservation) { amount := "15"; value.OpeningStakeAlpha = &amount },
		func(value *historicalPrincipalObservation) { registered := false; value.Registered = &registered },
		func(value *historicalPrincipalObservation) { value.Query.Coldkey[0] ^= 1 },
		func(value *historicalPrincipalObservation) { value.Query.Netuid++ },
		func(value *historicalPrincipalObservation) { value.ResultHex += "00" },
		func(value *historicalPrincipalObservation) {
			raw, _ := historicalReplayHex(value.ResultHex, 256)
			value.ResultHex = nativeExecutionTestHex(append(append(raw[:66:66], 57, 0), raw[67:]...))
		},
		func(value *historicalPrincipalObservation) {
			raw, _ := historicalReplayHex(value.ResultHex, 256)
			raw[33] ^= 1
			value.ResultHex = nativeExecutionTestHex(raw)
		},
	} {
		value := valid
		mutate(&value)
		if err := validateHistoricalPrincipalReport([]historicalPrincipalQuery{valid.Query}, []historicalPrincipalObservation{value}); err == nil {
			t.Fatal("principal report replaced canonical original identity/layout/scalar", value)
		}
	}
}

func TestHistoricalPrincipalLegacyNilCannotEnrollAQueryResult(t *testing.T) {
	valid := historicalPrincipalTestValue(14)
	if err := validateHistoricalPrincipalReport(nil, nil); err != nil {
		t.Fatal("legacy nil query grammar changed", err)
	}
	for _, queries := range [][]historicalPrincipalQuery{nil, {}, {valid.Query, valid.Query}, {{Netuid: 0}}, {valid.Query}} {
		results := []historicalPrincipalObservation{valid}
		if len(queries) == 1 && queries[0] == valid.Query {
			results = nil
		}
		if err := validateHistoricalPrincipalReport(queries, results); err == nil {
			t.Fatal("principal report enrolled or omitted an original query census", queries, results)
		}
	}
}
