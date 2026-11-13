// Bounded decoding distinguishes known amounts from missing authority and
// rejects source assertions that contradict their own census or publication.
package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func claimProgressProtocolFixture() ClaimProgress {
	pool := ClaimProgressPool{ChainId: 945, Vault: "0x1111111111111111111111111111111111111111", NoId: "7", Coldkey: "0x" + strings.Repeat("22", 32)}
	observation := &ClaimObservation{Schema: ClaimObservationSchema, EvidenceKind: "signed-receipt", Authority: "configured-rpc-assertion", GenesisStatus: "unverified", Epoch: 70, ObservedAt: "2026-01-01T00:00:00Z", Pool: pool, ShareBps: 1, ProofStatus: "contract-accepted", BlockNumber: 80, BlockHash: "0x" + strings.Repeat("33", 32), TransactionHash: "0x" + strings.Repeat("44", 32), Relayer: "0x" + strings.Repeat("55", 20), AcceptedAmountRao: "25", PaymentStatus: "aggregate-paid", AggregatePaidRao: "1901"}
	return ClaimProgress{Schema: ClaimProgressSchema, Member: "synthetic", Status: "active", InstanceId: strings.Repeat("11", 16), StartedAt: "2026-01-01T00:00:00Z", PublishedAt: "2026-01-01T00:00:01Z", Sequence: 1, QueueSha256: strings.Repeat("66", 32), DeclaredPool: &pool, TotalEntries: 1, FinalizedEntries: 1, Entries: []ClaimProgressEntry{{Epoch: 70, QueueStatus: "finalized", ObservationStatus: "retained", DomainStatus: "match", Observation: observation}}}
}

func TestClaimProgressDecodeBoundsEntryCountAndGrammar(t *testing.T) {
	value := claimProgressProtocolFixture()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeClaimProgress(raw); err != nil {
		t.Fatal("valid published sample does not decode", err)
	}
	tooMany := value
	tooMany.Entries = make([]ClaimProgressEntry, MaxClaimProgressEntries+1)
	for index := range tooMany.Entries {
		tooMany.Entries[index] = value.Entries[0]
	}
	crowded, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeClaimProgress(crowded); err == nil || !strings.Contains(err.Error(), "count bound") {
		t.Fatal("untrusted entry array escaped count bound", err)
	}
	for _, invalid := range [][]byte{bytes.Repeat([]byte(" "), MaxClaimProgressBytes+1), append(append([]byte(nil), raw...), []byte(" {}")...), bytes.Replace(raw, []byte(`"schema":`), []byte(`"schema":"duplicate","schema":`), 1), bytes.Replace(raw, []byte(`"member":`), []byte(`"unknown_field":1,"member":`), 1)} {
		if _, err := DecodeClaimProgress(invalid); err == nil {
			t.Fatal("invalid public grammar was accepted")
		}
	}
}

func TestClaimProgressDecodeRejectsForgedAuthorityAndPayment(t *testing.T) {
	for _, mutate := range []func(*ClaimObservation){
		func(value *ClaimObservation) { value.Authority = "independent-native-finality" },
		func(value *ClaimObservation) { value.GenesisStatus = "verified-from-chain-id" },
		func(value *ClaimObservation) { value.AggregatePaidRao = "24" },
		func(value *ClaimObservation) { value.AggregatePaidRao = "01901" },
		func(value *ClaimObservation) { value.AggregatePaidRao = "-1" },
		func(value *ClaimObservation) { value.AggregatePaidRao = strings.Repeat("9", 78) },
		func(value *ClaimObservation) {
			value.PaymentStatus = "deferred"
			value.AggregatePaidRao = ""
			value.UnpaidCreditRao = "24"
		},
		func(value *ClaimObservation) { value.EvidenceKind = "finalized-leaf" },
		func(value *ClaimObservation) { value.EvidenceKind = "api-no-claim"; value.Authority = "api-assertion" },
	} {
		value := claimProgressProtocolFixture()
		mutate(value.Entries[0].Observation)
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeClaimProgress(raw); err == nil {
			t.Fatal("source assertion fabricated payment or trust", string(raw))
		}
	}
	value := claimProgressProtocolFixture()
	value.Entries[0].Observation.AcceptedAmountRao = "0"
	value.Entries[0].Observation.AggregatePaidRao = "0"
	raw, _ := json.Marshal(value)
	if _, err := DecodeClaimProgress(raw); err != nil {
		t.Fatal("known zero was confused with absent amount", err)
	}
}

func TestClaimProgressDecodeRejectsFutureOrContradictoryCensus(t *testing.T) {
	for _, mutate := range []func(*ClaimProgress){
		func(value *ClaimProgress) { value.Entries[0].Observation.ObservedAt = "2026-01-01T00:00:02Z" },
		func(value *ClaimProgress) { value.DeclaredPool.NoId = "8" },
		func(value *ClaimProgress) { value.DeclaredPool = nil },
		func(value *ClaimProgress) { value.TotalEntries = 2 },
		func(value *ClaimProgress) { value.FinalizedEntries = 0; value.NoClaimEntries = 1 },
		func(value *ClaimProgress) { value.UnresolvedEntries = 1 },
		func(value *ClaimProgress) { value.OmittedUnresolvedEntries = 1 },
		func(value *ClaimProgress) { oldest := int64(69); value.OldestUnresolvedEpoch = &oldest },
		func(value *ClaimProgress) { value.Sequence = 0 },
	} {
		value := claimProgressProtocolFixture()
		mutate(&value)
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeClaimProgress(raw); err == nil {
			t.Fatal("contradictory public census was accepted", string(raw))
		}
	}
}

func TestClaimProgressDecodeRetainsUnavailableAndClosedEvidence(t *testing.T) {
	for _, status := range []string{"active", "unavailable", "closed"} {
		value := claimProgressProtocolFixture()
		value.Status = status
		raw, _ := json.Marshal(value)
		decoded, err := DecodeClaimProgress(raw)
		if err != nil || decoded.Status != status || decoded.QueueSha256 != value.QueueSha256 || decoded.Entries[0].Observation.AggregatePaidRao != "1901" {
			t.Fatal("writer lifecycle altered retained evidence", status, err)
		}
	}
	for _, status := range []string{"unknown", "unavailable", "closed"} {
		value := ClaimProgress{Schema: ClaimProgressSchema, Member: "unadmitted", Status: status}
		raw, _ := json.Marshal(value)
		if _, err := DecodeClaimProgress(raw); err != nil {
			t.Fatal("unadmitted lifecycle requires fabricated evidence", status, err)
		}
	}
}
