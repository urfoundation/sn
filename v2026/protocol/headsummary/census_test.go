package headsummary_test

// The census count and bound-client coverage (VERIFY-COHORT-V2.md §2.1, §5.3)
// are optional signed v2 members: an operator bounds its verify cohort by the
// validator's reported census, so a forged or partial report must never decode.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol/headsummary"
)

func uint64Pointer(value uint64) *uint64 { return &value }

// Both members are signed, survive the round trip in canonical order, and a
// zero census is distinct from an absent one.
func TestCensusSummarySignsCensusAndCoverage(t *testing.T) {
	summary := unsignedEvidence(t)
	summary.CensusProviders, summary.BoundClientsObserved = uint64Pointer(1403), uint64Pointer(1)
	body, err := headsummary.Sign(summary, publicKey(t, testKey(t)), time.Unix(1_800_000_000, 0), testKey(t).Sign)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := headsummary.Decode(body)
	if err != nil || decoded.CensusProviders == nil || *decoded.CensusProviders != 1403 || decoded.BoundClientsObserved == nil || *decoded.BoundClientsObserved != 1 {
		t.Fatalf("signed census and coverage did not round-trip: %v", err)
	}
	if census, policy := strings.Index(string(body), `"census_providers":1403,"bound_clients_observed":1`), strings.Index(string(body), `"evidence_policy"`); census < 0 || census < policy || census > strings.Index(string(body), `"validator_hotkey"`) {
		t.Fatalf("census members are not in canonical order after the evidence policy: %s", body)
	}
	for _, change := range []func(*headsummary.Summary){
		func(s *headsummary.Summary) { *s.CensusProviders = 1402 },
		func(s *headsummary.Summary) { *s.BoundClientsObserved = 0 },
		func(s *headsummary.Summary) { s.CensusProviders, s.BoundClientsObserved = nil, nil },
	} {
		changed, err := headsummary.Decode(body)
		if err != nil {
			t.Fatal(err)
		}
		change(changed)
		encoded, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := headsummary.Decode(encoded); err == nil {
			t.Fatal("changed census retained the original signature")
		}
	}

	zero := unsignedEvidence(t)
	zero.CensusProviders = uint64Pointer(0)
	body, err = headsummary.Sign(zero, publicKey(t, testKey(t)), time.Unix(1_800_000_000, 0), testKey(t).Sign)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = headsummary.Decode(body)
	if err != nil || decoded.CensusProviders == nil || *decoded.CensusProviders != 0 || decoded.BoundClientsObserved != nil || !strings.Contains(string(body), `"census_providers":0`) {
		t.Fatalf("an explicit zero census was not kept apart from an absent one: %v", err)
	}
}

// A v2 envelope from a validator that predates the census encodes exactly as
// before, so its existing signature still verifies.
func TestCensusSummaryAbsentFromOlderEnvelopes(t *testing.T) {
	body, err := headsummary.Sign(unsignedEvidence(t), publicKey(t, testKey(t)), time.Unix(1_800_000_000, 0), testKey(t).Sign)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := headsummary.Decode(body)
	if err != nil || decoded.CensusProviders != nil || decoded.BoundClientsObserved != nil || strings.Contains(string(body), "census_providers") || strings.Contains(string(body), "bound_clients_observed") {
		t.Fatalf("an older envelope gained census members: %v", err)
	}
}

func TestCensusSummaryRejectsInvalidCensus(t *testing.T) {
	for index, change := range []func(*headsummary.Summary){
		// coverage without the census it is reported against
		func(s *headsummary.Summary) { s.BoundClientsObserved = uint64Pointer(0) },
		// more observed than the recipient's cut binds
		func(s *headsummary.Summary) {
			s.CensusProviders, s.BoundClientsObserved = uint64Pointer(10), uint64Pointer(2)
		},
		func(s *headsummary.Summary) { s.CensusProviders = uint64Pointer(headsummary.MaxCensusProviders + 1) },
	} {
		summary := unsignedEvidence(t)
		change(summary)
		if _, err := headsummary.Sign(summary, publicKey(t, testKey(t)), time.Unix(1_800_000_000, 0), testKey(t).Sign); err == nil {
			t.Fatalf("invalid census case %d was signed", index)
		}
	}
	for index, change := range []func(*headsummary.Summary){
		func(s *headsummary.Summary) { s.CensusProviders = uint64Pointer(1) },
		func(s *headsummary.Summary) {
			s.CensusProviders, s.BoundClientsObserved = uint64Pointer(1), uint64Pointer(0)
		},
	} {
		summary := unsigned(publicKey(t, testKey(t)))
		change(summary)
		if _, err := headsummary.Sign(summary, publicKey(t, testKey(t)), time.Unix(1_800_000_000, 0), testKey(t).Sign); err == nil {
			t.Fatalf("version 1 signed census case %d", index)
		}
	}
}
