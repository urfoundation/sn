// Failed source commitments retain every historical recovery field without
// promoting pending, failed, or unverified availability records into coverage.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScenarioRecoveryPreservesHistoricalAdversaryAvailability(t *testing.T) {
	fixture := newCampaignSuccessionFixture(t)
	prior, runDir := bindCampaignRecoveryFixture(t, fixture)
	result, _, err := readScenarioCampaignRecoveryResult(fixture.cfg, fixture.stateDir, prior)
	if err != nil {
		t.Fatal(err)
	}
	started := fixture.now.Format(time.RFC3339Nano)
	read := AdversaryRpcRecoveryRead{RequestHash: bytesSHA256([]byte("synthetic request")), ResponseHash: bytesSHA256([]byte("synthetic response"))}
	attempt := AdversaryRpcRecoveryAttempt{Sequence: 1, StartedAt: started, CompletedAt: started, Outcome: "availability-pending", ReadSetHash: bytesSHA256([]byte("synthetic read set")), ObservedReads: []AdversaryRpcRecoveryRead{read}}
	rpc := AdversaryRpcRecoveryEvidence{
		AuthorityHash: bytesSHA256([]byte("synthetic authority")), Schema: "urnetwork-rpc-availability-recovery-v1", Id: bytesSHA256([]byte("synthetic observation")),
		OriginalSequence: 1, CreditedSequence: 2, Phase: adversaryAttackPhase, StartedAt: started, Status: "recovered",
		Checkpoints: []AdversaryRpcRecoveryCheckpoint{{Endpoint: "http://rpc.example", FinalizedHash: "0x" + strings.Repeat("a1", 32), Finalized: 10}},
		Reads:       []AdversaryRpcRecoveryRead{read}, Attempts: []AdversaryRpcRecoveryAttempt{attempt},
	}
	request := AdversaryHttpRecoveryRequest{Kind: "history", Endpoint: "http://operator.example/history", MaximumBytes: 1024, RequestHash: read.RequestHash}
	httpAttempt := AdversaryHttpRecoveryAttempt{AdversaryRpcRecoveryAttempt: attempt, FailedRequest: &request, SnapshotReads: 1, FreshReads: []string{read.RequestHash}}
	var httpRecords []AdversaryHttpRecoveryEvidence
	for _, schema := range []string{"urnetwork-http-availability-recovery-v1", "urnetwork-http-availability-recovery-v5", "urnetwork-http-availability-recovery-v7"} {
		for _, status := range []string{"recovered", "failed"} {
			httpRecords = append(httpRecords, AdversaryHttpRecoveryEvidence{
				Schema: schema, Id: bytesSHA256([]byte(schema + status)), AuthorityHash: rpc.AuthorityHash, OriginalSequence: 1, Phase: adversaryAttackPhase,
				StartedAt: started, Status: status, Operator: 1, Operators: 2, DeploymentId: "synthetic-deployment", Netuid: 1,
				HistoryHash: read.ResponseHash, HistoryObjects: 1,
				Reads: []AdversaryHttpRecoveryRead{{AdversaryHttpRecoveryRequest: request, ResponseHash: read.ResponseHash, Status: 200}}, Attempts: []AdversaryHttpRecoveryAttempt{httpAttempt},
				Verify: &AdversaryVerifyHttpCheckpoint{Kind: "synthetic-checkpoint", ValidatorPublic: []byte{1, 2}, Keys: json.RawMessage(`{"keys":[]}`), Final: json.RawMessage(`{"synthetic":true}`)}, VerifyHash: read.ResponseHash,
				Custody: &AdversaryCustodyHttpCheckpoint{PriorPassed: []int{1}, DeploymentHash: read.ResponseHash, Artifacts: map[string]AdversaryCustodyHttpArtifact{read.ResponseHash: {Epoch: 1, ResponseHash: read.ResponseHash}}}, CustodyHash: read.ResponseHash,
				Stages: []AdversaryHttpRecoveryStage{{RequestHash: read.RequestHash, StartedAt: started, DeadlineAt: fixture.now.Add(time.Minute).Format(time.RFC3339Nano)}},
			})
		}
	}
	result.Adversaries = &AdversaryCampaignEvidence{Actors: []AdversaryActorEvidence{
		{ID: "rpc-consistency-pressure", RpcAvailability: []AdversaryRpcRecoveryEvidence{rpc}, AvailabilityAttempts: 1},
		{ID: "artifact-integrity-pressure", HttpAvailability: httpRecords, AvailabilityAttempts: 6},
	}}
	result.EvidenceHash, err = canonicalScenarioResultHash(result)
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(runDir, "result.json")
	if err := writePublicJSON(resultPath, result); err != nil {
		t.Fatal(err)
	}
	replayed, raw, err := readScenarioCampaignRecoveryResult(fixture.cfg, fixture.stateDir, prior)
	if err != nil {
		t.Fatalf("historical availability fields lost their exact result hash: %v", err)
	}
	// Public pretty-printing indents retained RawMessage checkpoints. Their
	// canonical bytes, including field order and values, must still be exact.
	want, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(replayed)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("historical availability canonical wire bytes changed: %v", err)
	}
	next, err := loadOrCreateScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0", nil, fixture.now.Add(time.Hour), fixture.journal)
	if err != nil || next.payload.Recovery == nil || next.payload.Recovery.PriorResultSha256 != bytesSHA256(raw) {
		t.Fatalf("successor did not authenticate original availability evidence: %v", err)
	}
	after, err := os.ReadFile(resultPath)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("historical result was rewritten")
	}
	if _, err := readScenarioCampaignAttempt(fixture.cfg, fixture.stateDir, fixture.roles, fixture.current.PlanHash, "release-1.0"); err != nil {
		t.Fatal(err)
	}
	result.Adversaries.Actors[0].RpcAvailability[0].Attempts[0].ObservedReads[0].ResponseHash = bytesSHA256([]byte("substitution"))
	if err := writePublicJSON(resultPath, result); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readScenarioCampaignRecoveryResult(fixture.cfg, fixture.stateDir, prior); err == nil {
		t.Fatal("nested availability substitution retained the original result commitment")
	}
}

func TestHistoricalAdversaryAvailabilityRemainsStrictAndNonAccepting(t *testing.T) {
	for _, raw := range []string{
		`{"rpc_availability_recovery":[{"schema":"urnetwork-rpc-availability-recovery-v1","unknown_reply":1}]}`,
		`{"http_availability_recovery":[{"schema":"urnetwork-http-availability-recovery-v7","get_stages":[{"unknown_deadline":1}]}]}`,
	} {
		var actor AdversaryActorEvidence
		if err := decodeStrictJSONBytes([]byte(raw), &actor); err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("unknown nested availability evidence was discarded: %v", err)
		}
	}
	if err := validateCurrentAdversaryAvailability(AdversaryActorEvidence{}); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []AdversaryActorEvidence{
		{ID: "rpc-consistency-pressure", RpcAvailability: []AdversaryRpcRecoveryEvidence{{Status: "recovered"}}},
		{ID: "artifact-integrity-pressure", HttpAvailability: []AdversaryHttpRecoveryEvidence{{Status: "recovered"}}},
		{ID: "rpc-consistency-pressure", AvailabilityAttempts: 1},
	} {
		evidence := healthyAdversaryEvidence()
		evidence.Actors = append(evidence.Actors, actor)
		if err := validateCurrentAdversaryAvailability(actor); err == nil {
			t.Fatal("historical availability status granted current completion credit")
		}
		if _, err := summarizeFinalAdversarialCampaign(evidence, nil); err == nil {
			t.Fatal("final semantic evidence accepted unverified historical availability")
		}
		found := false
		for _, assertion := range adversaryAssertions(evidence, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "") {
			if assertion.ID == "adversary_"+actor.ID+"_availability_recovery" && !assertion.Passed {
				found = true
			}
		}
		if !found {
			t.Fatal("current campaign assertions omitted unverified availability evidence")
		}
	}
}
