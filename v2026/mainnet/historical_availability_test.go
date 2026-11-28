// Availability is authenticated coldkey-wide stock. These tests reparse its
// original SCALE bytes and preserve the legacy principal wire when unselected.
package main

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
)

func historicalAvailabilityTestValue(total, locked, available uint64, present bool) historicalPrincipalObservation {
	value := historicalPrincipalTestValue(14)
	value.Query.Availability = true
	raw := append([]byte{4}, value.Query.Coldkey[:]...)
	observation := &historicalStakeAvailabilityObservation{}
	if present {
		raw = append(raw, 4, 25, 0)
		for _, amount := range []uint64{total, locked, available} {
			raw = append(raw, rootCompact(amount)...)
		}
		totalText, lockedText, availableText := strconv.FormatUint(total, 10), strconv.FormatUint(locked, 10), strconv.FormatUint(available, 10)
		observation.TotalAlpha, observation.LockedAlpha, observation.AvailableAlpha = &totalText, &lockedText, &availableText
	} else {
		raw = append(raw, 0)
	}
	observation.ResultHex = nativeExecutionTestHex(raw)
	value.Availability = observation
	return value
}

func TestHistoricalAvailabilityOriginalLayoutSeparatesLockCollateralAndAbsence(t *testing.T) {
	for _, value := range []historicalPrincipalObservation{
		historicalAvailabilityTestValue(14, 2, 9, true),  // Three units are collateral, not conviction lock.
		historicalAvailabilityTestValue(14, 20, 0, true), // Lock can exceed current total.
		historicalAvailabilityTestValue(0, 0, 0, true),
		historicalAvailabilityTestValue(0, 0, 0, false),
		historicalAvailabilityTestValue(^uint64(0), 2, ^uint64(0)-3, true),
	} {
		if err := validateHistoricalPrincipalReport([]historicalPrincipalQuery{value.Query}, []historicalPrincipalObservation{value}); err != nil {
			t.Fatal("canonical original availability result was refused", err)
		}
	}
	absent := historicalAvailabilityTestValue(0, 0, 0, false)
	zero := "0"
	absent.Availability.AvailableAlpha = &zero
	if err := validateHistoricalPrincipalReport([]historicalPrincipalQuery{absent.Query}, []historicalPrincipalObservation{absent}); err == nil {
		t.Fatal("omitted subnet result became spendable zero")
	}
	unselected := historicalAvailabilityTestValue(14, 2, 9, true)
	unselected.Query.Availability = false
	if err := validateHistoricalPrincipalReport([]historicalPrincipalQuery{unselected.Query}, []historicalPrincipalObservation{unselected}); err == nil {
		t.Fatal("legacy query enrolled an unsolicited availability result")
	}
}

func TestHistoricalAvailabilityRefusesSubstitutionAndScalarForgery(t *testing.T) {
	for _, mutate := range []func(*historicalPrincipalObservation){
		func(value *historicalPrincipalObservation) { value.Availability = nil },
		func(value *historicalPrincipalObservation) { value.Query.Availability = false },
		func(value *historicalPrincipalObservation) {
			amount := "10"
			value.Availability.AvailableAlpha = &amount
		},
		func(value *historicalPrincipalObservation) { value.Availability.TotalAlpha = nil },
		func(value *historicalPrincipalObservation) { amount := "02"; value.Availability.LockedAlpha = &amount },
		func(value *historicalPrincipalObservation) { value.Availability.ResultHex += "00" },
		func(value *historicalPrincipalObservation) {
			raw, _ := historicalReplayHex(value.Availability.ResultHex, 256)
			raw[1] ^= 1
			value.Availability.ResultHex = nativeExecutionTestHex(raw)
		},
		func(value *historicalPrincipalObservation) {
			raw, _ := historicalReplayHex(value.Availability.ResultHex, 256)
			raw[34] = 26
			value.Availability.ResultHex = nativeExecutionTestHex(raw)
		},
		func(value *historicalPrincipalObservation) {
			raw, _ := historicalReplayHex(value.Availability.ResultHex, 256)
			raw[0] = 0
			value.Availability.ResultHex = nativeExecutionTestHex(raw)
		},
		func(value *historicalPrincipalObservation) {
			raw, _ := historicalReplayHex(value.Availability.ResultHex, 256)
			value.Availability.ResultHex = nativeExecutionTestHex(append([]byte{5, 0}, raw[1:]...))
		},
		func(value *historicalPrincipalObservation) { *value = historicalAvailabilityTestValue(14, 2, 13, true) },
	} {
		value := historicalAvailabilityTestValue(14, 2, 9, true)
		query := value.Query
		mutate(&value)
		if err := validateHistoricalPrincipalReport([]historicalPrincipalQuery{query}, []historicalPrincipalObservation{value}); err == nil {
			t.Fatal("availability report replaced original selection, identity, layout or scalar")
		}
	}
}

func TestHistoricalAvailabilitySelectionCannotDuplicatePrincipalIdentity(t *testing.T) {
	legacy := historicalPrincipalTestValue(14)
	selected := legacy.Query
	selected.Availability = true
	for _, queries := range [][]historicalPrincipalQuery{{legacy.Query, selected}, {selected, legacy.Query}} {
		if err := validateHistoricalPrincipalQueries(queries); err == nil {
			t.Fatal("optional availability flag manufactured a second principal identity")
		}
	}
}

func TestHistoricalAvailabilityUnselectedPreservesLegacyWire(t *testing.T) {
	value := historicalPrincipalTestValue(14)
	legacyQuery := struct {
		Hotkey  historicalReplayDigest `json:"hotkey"`
		Coldkey historicalReplayDigest `json:"coldkey"`
		Netuid  uint16                 `json:"netuid"`
	}{Hotkey: value.Query.Hotkey, Coldkey: value.Query.Coldkey, Netuid: value.Query.Netuid}
	legacy := struct {
		Query             any     `json:"query"`
		ResultHex         string  `json:"result_hex"`
		OpeningStakeAlpha *string `json:"opening_stake_alpha"`
		Registered        *bool   `json:"registered"`
	}{Query: legacyQuery, ResultHex: value.ResultHex, OpeningStakeAlpha: value.OpeningStakeAlpha, Registered: value.Registered}
	before, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("unselected availability changed historical principal bytes")
	}
}
