package headsummary_test

// Wire regressions retain v1 compatibility and prove that the signer owns every
// exact-cut, policy, recipient and historical binding field in v2.

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/protocol/headsummary"
)

// Reuse the existing synthetic result with a valid public policy and one cut.
func unsignedEvidence(t *testing.T) *headsummary.Summary {
	t.Helper()
	summary := unsigned(publicKey(t, testKey(t)))
	policy, err := protocol.LoadPolicy("../../deploy/testnet/policy-v1.yml")
	if err != nil {
		t.Fatal(err)
	}
	policy.Steering.MaximumHeadFleets = summary.MaximumHeadFleets
	hash, err := policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	summary.Schema, summary.PolicyHash, summary.EvidencePolicy = headsummary.SchemaV2, hash, policy
	summary.PreviousMeasurementArtifactHash = "sha256:" + hash32(0x70)[2:]
	summary.EvidenceRecipient = 9
	summary.EvidenceOperators = []uint64{9, 10}
	summary.EvidenceCuts = []headsummary.EvidenceCut{{
		NoId: 9, NativeBlock: summary.NativeSnapshotBlock - 2, NativeBlockHash: hash32(0x61), ContentHash: hash32(0x62),
		Bindings: []headsummary.EvidenceBinding{{ClientId: "00000000-0000-0000-0000-000000000001", FleetId: hash32(0x11), Hotkey: hash32(0x07), Generation: 1, Uid: 7}},
	}}
	return summary
}

// The exact cut reference survives delivery without deriving a window from
// signed_at. Every evidence field, its recipient and the predecessor are signed.
func TestEvidenceSummarySignsExactCutAndCensus(t *testing.T) {
	summary := unsignedEvidence(t)
	body, err := headsummary.Sign(summary, publicKey(t, testKey(t)), time.Unix(1_800_000_000, 0), testKey(t).Sign)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := headsummary.Decode(body)
	if err != nil || !reflect.DeepEqual(decoded.EvidenceCuts, summary.EvidenceCuts) || !reflect.DeepEqual(decoded.EvidenceOperators, []uint64{9, 10}) {
		t.Fatalf("signed exact evidence did not round-trip: %v", err)
	}
	for _, change := range []func(*headsummary.Summary){
		func(s *headsummary.Summary) { s.EvidenceCuts[0].NativeBlock-- },
		func(s *headsummary.Summary) { s.EvidenceCuts[0].NativeBlockHash = hash32(0x63) },
		func(s *headsummary.Summary) { s.EvidenceCuts[0].ContentHash = hash32(0x64) },
		func(s *headsummary.Summary) { s.EvidenceCuts[0].Bindings[0].Generation++ },
		func(s *headsummary.Summary) {
			s.EvidenceCuts[0].Bindings[0].ClientId = "00000000-0000-0000-0000-000000000002"
		},
		func(s *headsummary.Summary) { s.EvidenceOperators = []uint64{9} },
		func(s *headsummary.Summary) { s.PreviousMeasurementArtifactHash = "sha256:" + hash32(0x71)[2:] },
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
			t.Fatal("changed evidence retained the original signature")
		}
	}
}

func TestEvidenceSummaryRejectsMissingDuplicateOrUnscopedEvidence(t *testing.T) {
	for index, change := range []func(*headsummary.Summary){
		func(s *headsummary.Summary) { s.Schema = headsummary.Schema },
		func(s *headsummary.Summary) { s.EvidenceCuts = nil },
		func(s *headsummary.Summary) { s.EvidenceCuts = []headsummary.EvidenceCut{} },
		func(s *headsummary.Summary) { s.EvidenceCuts = append(s.EvidenceCuts, s.EvidenceCuts[0]) },
		func(s *headsummary.Summary) { s.EvidenceCuts[0].Bindings = nil },
		func(s *headsummary.Summary) {
			s.EvidenceCuts[0].Bindings = append(s.EvidenceCuts[0].Bindings, s.EvidenceCuts[0].Bindings[0])
		},
		func(s *headsummary.Summary) { s.EvidenceCuts[0].NativeBlock = s.NativeSnapshotBlock + 1 },
		func(s *headsummary.Summary) { s.EvidenceCuts[0].ContentHash = headsummary.Hex32([32]byte{}) },
		func(s *headsummary.Summary) { s.EvidenceRecipient = 10 },
		func(s *headsummary.Summary) { s.EvidenceOperators = nil },
		func(s *headsummary.Summary) { s.EvidenceOperators = []uint64{10} },
		func(s *headsummary.Summary) { s.EvidenceOperators = []uint64{9, 9} },
		func(s *headsummary.Summary) { s.EvidenceOperators = []uint64{10, 9} },
		func(s *headsummary.Summary) { s.EvidenceOperators = make([]uint64, headsummary.MaxEvidenceCuts+1) },
		func(s *headsummary.Summary) { s.EvidencePolicy = nil },
		func(s *headsummary.Summary) { s.EvidencePolicy.PolicyID++ },
		func(s *headsummary.Summary) { s.PreviousMeasurementArtifactHash = s.MeasurementArtifactHash },
		func(s *headsummary.Summary) { s.EvidenceEmptyPrior = true },
	} {
		summary := unsignedEvidence(t)
		change(summary)
		if _, err := headsummary.Sign(summary, publicKey(t, testKey(t)), time.Unix(1_800_000_000, 0), testKey(t).Sign); err == nil {
			t.Fatalf("invalid evidence case %d was signed", index)
		}
	}
}

// A first measurement can inherit migrated state. Only the explicit signed
// empty-prior attestation permits a consumer to bootstrap without that state.
func TestEvidenceSummaryAuthenticatesEmptyPrior(t *testing.T) {
	summary := unsignedEvidence(t)
	summary.PreviousMeasurementArtifactHash = ""
	summary.EvidenceEmptyPrior = true
	body, err := headsummary.Sign(summary, publicKey(t, testKey(t)), time.Unix(1_800_000_000, 0), testKey(t).Sign)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := headsummary.Decode(body)
	if err != nil || !decoded.EvidenceEmptyPrior {
		t.Fatalf("empty-prior attestation was lost: %v", err)
	}
	decoded.EvidenceEmptyPrior = false
	changed, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := headsummary.Decode(changed); err == nil {
		t.Fatal("removing empty-prior attestation retained its signature")
	}
	summary = unsigned(publicKey(t, testKey(t)))
	summary.EvidenceEmptyPrior = true
	if _, err := headsummary.Sign(summary, publicKey(t, testKey(t)), time.Unix(1_800_000_000, 0), testKey(t).Sign); err == nil {
		t.Fatal("version 1 signed an empty-prior attestation")
	}
}

func TestEvidenceSummaryAllowsExplicitEmptyEligibleCensus(t *testing.T) {
	summary := unsignedEvidence(t)
	summary.EvidenceCuts[0].Bindings = []headsummary.EvidenceBinding{}
	body, err := headsummary.Sign(summary, publicKey(t, testKey(t)), time.Unix(1_800_000_000, 0), testKey(t).Sign)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := headsummary.Decode(body)
	if err != nil || decoded.EvidenceCuts[0].Bindings == nil || len(decoded.EvidenceCuts[0].Bindings) != 0 {
		t.Fatalf("explicit empty eligible census was lost: %v", err)
	}
}
