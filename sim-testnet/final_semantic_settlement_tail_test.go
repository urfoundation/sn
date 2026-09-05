package main

// Settlement-tail regressions use the durable production ledger and actual
// multi-operator transition before invoking the collector's real cut merger.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

// Appends a distinct completed trail after an already detached ordinary cut.
// All wire signatures are real; ordering is explicit and requires no timers.
func appendFinalSettlementTail(t *testing.T, ledger *validatorpkg.AttemptLedger, template []validatorpkg.AttemptRecord, validatorKey, serverKey ed25519.PrivateKey, trailID connect.Id) *validatorpkg.ProofRecord {
	t.Helper()
	firstTrailID := template[0].TrailID
	var completed *validatorpkg.ProofRecord
	for _, original := range template {
		if original.TrailID != firstTrailID {
			break
		}
		encoded, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var record validatorpkg.AttemptRecord
		if err := json.Unmarshal(encoded, &record); err != nil {
			t.Fatal(err)
		}
		record.TrailID = trailID
		for index := range record.Assignments {
			assignment := &record.Assignments[index]
			trail := append(append([]connect.Id(nil), assignment.Trail...), assignment.NextHop)
			message, err := connect.BuildVerifyAssignMessage(assignment.ServerKeyID, trailID, record.ServerNonce, record.VPK, byte(record.M), trail)
			if err != nil {
				t.Fatal(err)
			}
			assignment.AssignMessage = message
			assignment.AssignSignature = ed25519.Sign(serverKey, message)
		}
		if proof := record.Proof; proof != nil {
			proof.TrailId = trailID
			finalMessage, err := connect.BuildVerifyFinalMessage(proof.ServerKeyId, trailID, proof.ServerNonce, proof.Vpk, byte(proof.M), proof.Hops)
			if err != nil {
				t.Fatal(err)
			}
			trail := make([]connect.Id, len(proof.Hops))
			for index, hop := range proof.Hops {
				trail[index] = hop.ClientId
			}
			extendMessage, err := connect.BuildVerifyExtendMessage(trailID, proof.ServerNonce, proof.Vpk, byte(proof.M), trail)
			if err != nil {
				t.Fatal(err)
			}
			digest := connect.VerifyFinalDigest(finalMessage)
			pathID := validatorpkg.TrailPathId(trailID, proof.Vpk, proof.ServerKeyId)
			proof.FinalDigest, proof.PathId = digest[:], pathID[:]
			proof.FinalSig = ed25519.Sign(serverKey, finalMessage)
			proof.VpkSig = ed25519.Sign(validatorKey, finalMessage)
			proof.VerifierSig = ed25519.Sign(validatorKey, extendMessage)
			completed = proof
		}
		if _, err := ledger.Append(record); err != nil {
			t.Fatal(err)
		}
	}
	if completed == nil {
		t.Fatal("tail template never completed a trail")
	}
	return completed
}

func TestFinalCollectorIncludesCompletedSettlementTail(t *testing.T) {
	t.Parallel()
	source, artifacts := finalSemanticFixture(t)
	cycles := source.Validators[0].Cycles
	if len(cycles) < 2 || cycles[1].SettlementEpoch != cycles[0].SettlementEpoch+1 {
		t.Fatal("fixture must provide consecutive accepted settlement epochs")
	}
	ordinary, _, err := validatorpkg.DecodeReleaseMeasurementArtifact(artifacts[cycles[0].MeasurementArtifact.URI])
	if err != nil {
		t.Fatal(err)
	}
	successor, _, err := validatorpkg.DecodeReleaseMeasurementArtifact(artifacts[cycles[1].MeasurementArtifact.URI])
	if err != nil {
		t.Fatal(err)
	}
	validatorKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize))
	vpk := validatorKey.Public().(ed25519.PublicKey)
	coordinatorDir := t.TempDir()
	participants := make([]validatorpkg.AttemptSettlementParticipant, len(ordinary.Inputs))
	ledgers := make(map[uint64]*validatorpkg.AttemptLedger, len(participants))
	keys := make(map[uint64]map[byte]ed25519.PublicKey, len(participants))
	tails := make(map[uint64]*validatorpkg.ProofRecord, len(participants))
	for index := range ordinary.Inputs {
		input := &ordinary.Inputs[index]
		stateDir := t.TempDir()
		config := input.Stats.Config
		stats := validatorpkg.NewStatsEngine(validatorpkg.StatsConfig{AMin: config.AMin, AlphaNumerator: config.AlphaNumerator, AlphaDenominator: config.AlphaDenominator, LatRefMillis: config.LatRefMillis})
		ledger, err := validatorpkg.NewAttemptLedger(stateDir, input.Stats.AttemptCut.Identity, validatorKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := stats.AttachAttemptLedger(ledger, stateDir); err != nil {
			t.Fatal(err)
		}
		participants[index] = validatorpkg.AttemptSettlementParticipant{NoID: input.NoID, StateDir: stateDir, Stats: stats}
		ledgers[input.NoID] = ledger
		serverKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0x50 + input.NoID)}, ed25519.SeedSize))
		keys[input.NoID] = map[byte]ed25519.PublicKey{1: serverKey.Public().(ed25519.PublicKey)}
		for providerIndex := range input.Stats.Providers {
			input.Stats.Providers[providerIndex].HasPriorQuality = false
			input.Stats.Providers[providerIndex].PriorQualityPPM = 0
		}
	}
	if err := validatorpkg.AdvanceAttemptSettlementEpoch(coordinatorDir, ordinary.SettlementEpoch, validatorpkg.AttemptBoundary{}, participants); err != nil {
		t.Fatal(err)
	}
	for index := range ordinary.Inputs {
		input := &ordinary.Inputs[index]
		participant, ledger := participants[index], ledgers[input.NoID]
		template := input.Stats.AttemptCut.Records
		for _, record := range template {
			if _, err := ledger.Append(record); err != nil {
				t.Fatal(err)
			}
		}
		if err := participant.Stats.AttachAttemptLedger(ledger, participant.StateDir); err != nil {
			t.Fatal(err)
		}
		cut, err := ledger.BuildCut(input.Stats.AttemptCut.Boundary, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		input.Stats.AttemptCut = cut
		input.Stats.SettlementTransition = nil
		serverKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0x50 + input.NoID)}, ed25519.SeedSize))
		tails[input.NoID] = appendFinalSettlementTail(t, ledger, template, validatorKey, serverKey, finalAttemptFixtureID(99_000_000_000+input.NoID))
		if err := participant.Stats.AttachAttemptLedger(ledger, participant.StateDir); err != nil {
			t.Fatal(err)
		}
		if input.Stats.AttemptCut.LastSequence >= ledger.LastSequence() {
			t.Fatal("ordinary cut unexpectedly includes the later completed trail")
		}
	}
	ordinary.PreviousArtifactHash = ""
	ordinaryBytes, ordinaryHash, _, err := validatorpkg.SealReleaseMeasurementArtifact(ordinary)
	if err != nil {
		t.Fatalf("ordinary measurement: %v", err)
	}
	terminalBoundary := ordinary.Inputs[0].Stats.AttemptCut.Boundary
	terminalBoundary.EVMBlock++
	terminalBoundary.EVMBlockHash = finalTestHex(0x77)
	if err := validatorpkg.AdvanceAttemptSettlementEpoch(coordinatorDir, successor.SettlementEpoch, terminalBoundary, participants); err != nil {
		t.Fatal(err)
	}
	for index := range successor.Inputs {
		input := &successor.Inputs[index]
		participant, ledger := participants[index], ledgers[input.NoID]
		encoded, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			Transition *validatorpkg.AttemptSettlementTransition `json:"settlement_transition"`
		}
		if err := json.Unmarshal(encoded, &state); err != nil || state.Transition == nil {
			t.Fatalf("production terminal transition: %v", err)
		}
		transition := state.Transition
		if err := validatorpkg.VerifyAttemptSettlementTransition(transition); err != nil {
			t.Fatal(err)
		}
		if err := validatorpkg.VerifyAttemptLedgerCut(transition.PreFold.AttemptCut, vpk, keys[input.NoID]); err != nil {
			t.Fatal(err)
		}
		terminalCut := transition.PreFold.AttemptCut
		if terminalCut.LastSequence != ledger.LastSequence() || terminalCut.Records[len(terminalCut.Records)-1].Proof.TrailId != tails[input.NoID].TrailId {
			t.Fatal("production terminal cut omitted the post-ordinary-cut proof")
		}
		firstSequence := ledger.LastSequence() + 1
		for _, record := range input.Stats.AttemptCut.Records {
			if _, err := ledger.Append(record); err != nil {
				t.Fatal(err)
			}
		}
		cut, err := ledger.BuildCut(input.Stats.AttemptCut.Boundary, firstSequence, firstSequence)
		if err != nil {
			t.Fatal(err)
		}
		input.Stats.AttemptCut, input.Stats.SettlementTransition = cut, transition
		qualities := map[string]validatorpkg.AttemptSettlementQuality{}
		for _, quality := range transition.PostFold {
			qualities[quality.ClientID] = quality
		}
		for providerIndex := range input.Stats.Providers {
			provider := &input.Stats.Providers[providerIndex]
			quality, exists := qualities[provider.ClientID]
			provider.HasPriorQuality, provider.PriorQualityPPM = exists, quality.QualityPPM
		}
	}
	successor.PreviousArtifactHash = ordinaryHash
	successorBytes, _, _, err := validatorpkg.SealReleaseMeasurementArtifact(successor)
	if err != nil {
		t.Fatalf("successor measurement: %v", err)
	}
	if err := validatorpkg.VerifyReleaseMeasurementLineage(ordinaryBytes, successor); err != nil {
		t.Fatalf("real settlement lineage: %v", err)
	}
	recordsByNO := make(map[uint64]map[uint64]validatorpkg.AttemptRecord, len(participants))
	for _, participant := range participants {
		recordsByNO[participant.NoID] = map[uint64]validatorpkg.AttemptRecord{}
	}
	for _, encoded := range [][]byte{ordinaryBytes, successorBytes} {
		if err := collectFinalAttemptCuts(int(ordinary.ValidatorID), encoded, vpk, keys, recordsByNO); err != nil {
			t.Fatal(err)
		}
	}
	for _, participant := range participants {
		records, _, err := persistFinalAttemptRecords(t.TempDir(), int(ordinary.ValidatorID), int(participant.NoID), recordsByNO[participant.NoID])
		if err != nil {
			t.Fatalf("collector lost a completed accepted-epoch tail retained in the verified production terminal cut: %v", err)
		}
		found := false
		for _, record := range records {
			found = found || record.Proof != nil && record.Proof.TrailId == tails[participant.NoID].TrailId
		}
		if !found {
			t.Fatalf("operator %d accepted-epoch tail proof was omitted", participant.NoID)
		}
	}
}
