package main

// Small durable fixtures exercise terminal export and public replay without
// requiring the release-scale semantic fixture for each adjacent control.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	validatorpkg "github.com/urfoundation/sn/validator"
	"github.com/urnetwork/connect"
)

// Appends complete canonical four-hop trails with every durable checkpoint.
func appendFinalClosureTestTrail(t *testing.T, ledger *validatorpkg.AttemptLedger, validatorKey, serverKey ed25519.PrivateKey, boundary validatorpkg.AttemptBoundary, ordinal uint64) {
	t.Helper()
	trailID := finalAttemptFixtureID(ordinal)
	nonce := bytes.Repeat([]byte{byte(ordinal)}, connect.VerifyNonceSize)
	vpk := validatorKey.Public().(ed25519.PublicKey)
	trail := make([]connect.Id, connect.VerifyMMin)
	hops := make([]connect.VerifyProofHop, len(trail))
	for index := range trail {
		trail[index] = finalAttemptFixtureID(ordinal*10 + uint64(index) + 1)
		hops[index] = connect.VerifyProofHop{ClientId: trail[index], TimeMs: uint64(1000 + index)}
	}
	record := validatorpkg.AttemptRecord{Boundary: boundary, TrailID: trailID, ServerNonce: nonce, M: len(trail), Disposition: validatorpkg.AttemptDispositionPending}
	for index := 1; index < len(trail); index++ {
		message, err := connect.BuildVerifyAssignMessage(1, trailID, nonce, vpk, byte(len(trail)), trail[:index+1])
		if err != nil {
			t.Fatal(err)
		}
		// Unbound providers still carry explicit canonical zero identities.
		record.Assignments = append(record.Assignments, validatorpkg.AttemptAssignment{Trail: append([]connect.Id(nil), trail[:index]...), NextHop: trail[index], ServerKeyID: 1, AssignMessage: message, AssignSignature: ed25519.Sign(serverKey, message), Binding: validatorpkg.AttemptBinding{ClientID: trail[index], FleetID: finalTestHex(0), Hotkey: finalTestHex(0)}})
		if _, err := ledger.Append(record); err != nil {
			t.Fatal(err)
		}
		record.Assignments[index-1].Confirmed, record.Assignments[index-1].HasLatency = true, true
	}
	finalMessage, err := connect.BuildVerifyFinalMessage(1, trailID, nonce, vpk, byte(len(trail)), hops)
	if err != nil {
		t.Fatal(err)
	}
	extend, err := connect.BuildVerifyExtendMessage(trailID, nonce, vpk, byte(len(trail)), trail)
	if err != nil {
		t.Fatal(err)
	}
	digest, pathID := connect.VerifyFinalDigest(finalMessage), validatorpkg.TrailPathId(trailID, vpk, 1)
	record.Proof = &validatorpkg.ProofRecord{Version: 1, Epoch: boundary.SettlementEpoch, TrailId: trailID, ServerNonce: nonce, Vpk: vpk, M: len(trail), Hops: hops, ServerKeyId: 1, FinalSig: ed25519.Sign(serverKey, finalMessage), VerifierSig: ed25519.Sign(validatorKey, extend), FinalDigest: digest[:], VpkSig: ed25519.Sign(validatorKey, finalMessage), Coverage: uint64(len(trail) - 1), PathId: pathID[:], CompleteTimeMs: hops[len(hops)-1].TimeMs}
	record.Disposition = validatorpkg.AttemptDispositionComplete
	if _, err := ledger.Append(record); err != nil {
		t.Fatal(err)
	}
}

// Produces both operators' two epochs through the actual durable transaction;
// no successor measurement or steering intent is ever created.
func finalSettlementClosureTestFixture(t *testing.T) (*FinalSemanticEvidence, map[string][]byte, string) {
	t.Helper()
	root := t.TempDir()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize))
	vpk := "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey))
	evidence := &FinalSemanticEvidence{DeploymentID: "closure-test", ChainID: 945, Netuid: 521, GenesisHash: finalTestHex(5), Window: ScenarioAcceptanceWindow{FirstEpoch: 42, EpochCount: 2, StartBlock: 100, EpochBlocks: 10}, Validators: []FinalValidatorIdentityEvidence{{ValidatorID: 1, UID: 12, PathVPK: vpk}}}
	participants := make([]validatorpkg.AttemptSettlementParticipant, 2)
	ledgers := make([]*validatorpkg.AttemptLedger, 2)
	keys := make([]ed25519.PrivateKey, 2)
	for index := range participants {
		noID := uint64(index + 1)
		keys[index] = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0x51 + index)}, ed25519.SeedSize))
		evidence.Pools = append(evidence.Pools, FinalPoolUIDEvidence{NoID: noID, ServerKeyHistory: []FinalServerKey{{KeyID: 1, PublicKey: "0x" + hex.EncodeToString(keys[index].Public().(ed25519.PublicKey))}}})
		stateDir := t.TempDir()
		stats := validatorpkg.NewStatsEngine(validatorpkg.StatsConfig{AMin: 1})
		ledger, err := validatorpkg.NewAttemptLedger(stateDir, validatorpkg.AttemptLedgerIdentity{DeploymentID: evidence.DeploymentID, ChainID: evidence.ChainID, GenesisHash: evidence.GenesisHash, Netuid: evidence.Netuid, ValidatorID: 1, ValidatorUID: 12, NoID: noID}, key)
		if err != nil {
			t.Fatal(err)
		}
		if err := stats.AttachAttemptLedger(ledger, stateDir); err != nil {
			t.Fatal(err)
		}
		participants[index] = validatorpkg.AttemptSettlementParticipant{NoID: noID, StateDir: stateDir, Stats: stats}
		ledgers[index] = ledger
	}
	if err := validatorpkg.AdvanceAttemptSettlementEpoch(root, 42, validatorpkg.AttemptBoundary{}, participants); err != nil {
		t.Fatal(err)
	}
	loaded := map[string][]byte{}
	records := map[uint64]map[uint64]validatorpkg.AttemptRecord{1: {}, 2: {}}
	var closures []FinalCollectedSettlementClosure
	for epoch := uint64(42); epoch <= 43; epoch++ {
		boundary := validatorpkg.AttemptBoundary{SettlementEpoch: epoch, EVMBlock: 100 + (epoch-42)*10, EVMBlockHash: finalTestHex(byte(epoch))}
		for index, participant := range participants {
			for trail := uint64(1); trail <= 2; trail++ {
				appendFinalClosureTestTrail(t, ledgers[index], key, keys[index], boundary, epoch*100+participant.NoID*10+trail)
			}
			if err := participant.Stats.AttachAttemptLedger(ledgers[index], participant.StateDir); err != nil {
				t.Fatal(err)
			}
		}
		boundary.EVMBlock += 9
		if err := validatorpkg.AdvanceAttemptSettlementEpoch(root, epoch+1, boundary, participants); err != nil {
			t.Fatal(err)
		}
		data, err := validatorpkg.ReadAttemptSettlementClosure(root, epoch)
		if err != nil {
			t.Fatal(err)
		}
		closure, err := validatorpkg.DecodeAttemptSettlementClosure(data)
		if err != nil {
			t.Fatal(err)
		}
		uri := fmt.Sprintf("closure-%d.json", epoch)
		loaded[uri] = data
		closures = append(closures, FinalCollectedSettlementClosure{Epoch: epoch, Boundary: ChainHead{Number: boundary.EVMBlock, Hash: boundary.EVMBlockHash}, Artifact: FinalArtifactLocator{Kind: "validator-settlement-closure", URI: uri, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}})
		for _, transition := range closure.Transitions {
			if err := mergeFinalAttemptCut(transition.PreFold.AttemptCut, records[transition.Identity.NoID]); err != nil {
				t.Fatal(err)
			}
		}
	}
	for noID := uint64(1); noID <= 2; noID++ {
		data, count, err := finalAcceptedAttemptProofBytes(records[noID], 42, 43)
		if err != nil {
			t.Fatal(err)
		}
		uri := fmt.Sprintf("proofs-%d.jsonl", noID)
		loaded[uri] = data
		locator := FinalArtifactLocator{Kind: "validator-path-proofs", URI: uri, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
		evidence.PathProofs = append(evidence.PathProofs, FinalValidatorPathProofEvidence{ValidatorID: 1, NoID: noID, FirstEpoch: 42, LastEpoch: 43, ProofCount: count, TrailDepth: connect.VerifyMMin, Artifact: locator, ProofsHash: locator.ContentHash, SettlementClosures: append([]FinalCollectedSettlementClosure(nil), closures...)})
	}
	if err := verifyFinalSettlementClosureArtifacts(evidence, loaded); err != nil {
		t.Fatalf("production closure fixture: %v", err)
	}
	return evidence, loaded, root
}

// The last required epoch is exported without waiting for a next native intent.
func TestFinalSettlementClosureLastWindowNeedsNoNextIntent(t *testing.T) {
	evidence, loaded, root := finalSettlementClosureTestFixture(t)
	if _, err := os.Stat(filepath.Join(root, "steering-intents.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture unexpectedly created a native intent: %v", err)
	}
	if _, err := validatorpkg.ReadAttemptSettlementClosure(root, 43); err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalSettlementClosureArtifacts(evidence, loaded); err != nil {
		t.Fatal(err)
	}
	for _, proof := range evidence.PathProofs {
		if proof.ProofCount != 4 {
			t.Fatalf("operator %d lost a completed trail: %d", proof.NoID, proof.ProofCount)
		}
	}
}

// Rehashing a shortened, otherwise valid proof file cannot erase a signed tail.
func TestFinalSettlementClosureRejectsRehashedProofOmission(t *testing.T) {
	evidence, loaded, _ := finalSettlementClosureTestFixture(t)
	proof := &evidence.PathProofs[0]
	data := loaded[proof.Artifact.URI]
	data = append([]byte(nil), data[bytes.IndexByte(data, '\n')+1:]...)
	loaded[proof.Artifact.URI] = data
	proof.ProofCount--
	proof.Artifact.SizeBytes, proof.Artifact.ContentHash = uint64(len(data)), bytesSHA256(data)
	proof.ProofsHash = proof.Artifact.ContentHash
	if err := verifyFinalSettlementClosureArtifacts(evidence, loaded); err == nil || !strings.Contains(err.Error(), "complete signed settlement census") {
		t.Fatalf("rehashed omitted valid tail: %v", err)
	}
}

// Matching proof records do not authorize an omitted, partial or differently
// signed successor transition; the public graph retains one exact transaction.
func TestFinalSettlementClosureRejectsConflictingSuccessorTransition(t *testing.T) {
	evidence, loaded, _ := finalSettlementClosureTestFixture(t)
	closure, err := validatorpkg.DecodeAttemptSettlementClosure(loaded[evidence.PathProofs[0].SettlementClosures[0].Artifact.URI])
	if err != nil {
		t.Fatal(err)
	}
	closures := map[uint64]*validatorpkg.AttemptSettlementClosure{closure.Epoch: closure}
	measurement := validatorpkg.ReleaseMeasurementArtifact{ValidatorID: 1}
	for _, transition := range closure.Transitions {
		measurement.Inputs = append(measurement.Inputs, validatorpkg.ReleaseMeasurementInput{NoID: transition.Identity.NoID, SettlementEpoch: transition.ToEpoch, Stats: validatorpkg.ReleaseStatsMeasurement{SettlementTransition: transition}})
	}
	good, err := json.Marshal(measurement)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalMeasurementSettlementClosures(good, closures); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"omitted", "boundary", "participant", "signature", "tail"} {
		var changed validatorpkg.ReleaseMeasurementArtifact
		if err := json.Unmarshal(good, &changed); err != nil {
			t.Fatal(err)
		}
		transition := changed.Inputs[0].Stats.SettlementTransition
		switch kind {
		case "omitted":
			changed.Inputs[0].Stats.SettlementTransition = nil
		case "boundary":
			transition.FromBoundary.EVMBlock--
		case "participant":
			transition.Batch = transition.Batch[:1]
		case "signature":
			transition.Signature[0] ^= 1
		case "tail":
			transition.PreFold.AttemptCut.Records = transition.PreFold.AttemptCut.Records[:len(transition.PreFold.AttemptCut.Records)-1]
		}
		data, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyFinalMeasurementSettlementClosures(data, closures); err == nil {
			t.Fatalf("%s successor transition was accepted", kind)
		}
	}
	// Both variants retain valid server proofs and receive fresh valid validator
	// cut/batch signatures. Rehashing and re-signing cannot authorize equivocation.
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize))
	for _, kind := range []string{"resigned-boundary", "resigned-omitted-tail"} {
		var changed validatorpkg.ReleaseMeasurementArtifact
		if err := json.Unmarshal(good, &changed); err != nil {
			t.Fatal(err)
		}
		transitions := make([]*validatorpkg.AttemptSettlementTransition, len(changed.Inputs))
		for index := range changed.Inputs {
			transition := changed.Inputs[index].Stats.SettlementTransition
			transitions[index] = transition
			cut := transition.PreFold.AttemptCut
			if kind == "resigned-boundary" {
				transition.FromBoundary.EVMBlock++
				cut.Boundary = transition.FromBoundary
			} else {
				cut.Records = cut.Records[:len(cut.Records)-connect.VerifyMMin]
				cut.RecordCount = uint64(len(cut.Records))
				cut.LastSequence = cut.Records[len(cut.Records)-1].Sequence
				cut.Root = cut.Records[len(cut.Records)-1].RecordHash
				remaining := map[string]validatorpkg.AttemptAssignment{}
				for _, record := range cut.Records {
					if record.Disposition != validatorpkg.AttemptDispositionComplete {
						continue
					}
					for _, assignment := range record.Assignments {
						remaining[assignment.NextHop.String()] = assignment
					}
				}
				var providers []validatorpkg.ReleaseProviderMeasurement
				for _, provider := range transition.PreFold.Providers {
					if _, ok := remaining[provider.ClientID]; ok {
						providers = append(providers, provider)
					}
				}
				transition.PreFold.Providers = providers
			}
		}
		resignFinalSettlementClosureTestBatch(t, transitions, key)
		data, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyFinalMeasurementSettlementClosures(data, closures); err == nil {
			t.Fatalf("%s valid conflicting authority was accepted", kind)
		}
	}
}

// Re-signs an internally consistent alternate transaction for negative replay
// controls; production never synthesizes or rewrites a published terminal cut.
func resignFinalSettlementClosureTestBatch(t *testing.T, transitions []*validatorpkg.AttemptSettlementTransition, key ed25519.PrivateKey) {
	t.Helper()
	for _, transition := range transitions {
		cut := transition.PreFold.AttemptCut
		hashes := make([]string, len(cut.Records))
		for index := range cut.Records {
			hashes[index] = cut.Records[index].RecordHash
		}
		payload := finalAttemptLedgerCutSignaturePayload{Schema: cut.Schema, Identity: cut.Identity, Boundary: cut.Boundary, FirstSequence: cut.FirstSequence, EgressFirstSequence: cut.EgressFirstSequence, LastSequence: cut.LastSequence, RecordCount: cut.RecordCount, PriorRoot: cut.PriorRoot, Root: cut.Root, RecordHashes: hashes}
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		cut.Signature = ed25519.Sign(key, append([]byte(finalAttemptLedgerCutSignDomain), encoded...))
		verified, err := validatorpkg.VerifyReleaseStatsMeasurement(transition.PreFold)
		if err != nil {
			t.Fatalf("alternate terminal cut is not a valid replay: %v", err)
		}
		transition.PostFold = nil
		for id, provider := range verified.Providers {
			if provider.HasQuality {
				transition.PostFold = append(transition.PostFold, validatorpkg.AttemptSettlementQuality{ClientID: id.String(), HasQuality: true, QualityPPM: provider.QualityPPM})
			}
		}
		sort.Slice(transition.PostFold, func(i, j int) bool { return transition.PostFold[i].ClientID < transition.PostFold[j].ClientID })
	}
	batch := make([]validatorpkg.AttemptSettlementMember, len(transitions))
	for index, transition := range transitions {
		batch[index] = validatorpkg.AttemptSettlementMember{NoID: transition.Identity.NoID, Digest: finalAttemptSettlementDigest(t, transition)}
	}
	for _, transition := range transitions {
		transition.Batch = append([]validatorpkg.AttemptSettlementMember(nil), batch...)
		transition.Signature = ed25519.Sign(key, finalAttemptSettlementMessage(t, transition))
	}
	if err := validatorpkg.VerifyAttemptSettlementBatch(transitions); err != nil {
		t.Fatalf("alternate signed batch is not independently valid: %v", err)
	}
}

// Signed domain, exact operator membership and every terminal epoch are required.
func TestFinalSettlementClosureRejectsDomainCensusAndBoundaryChanges(t *testing.T) {
	for _, kind := range []string{"domain", "validator-key", "last-epoch", "operator", "signature", "boundary"} {
		evidence, loaded, _ := finalSettlementClosureTestFixture(t)
		switch kind {
		case "domain":
			evidence.DeploymentID += "-other"
		case "validator-key":
			evidence.Validators[0].PathVPK = finalTestHex(0x71)
		case "last-epoch":
			for index := range evidence.PathProofs {
				evidence.PathProofs[index].SettlementClosures = evidence.PathProofs[index].SettlementClosures[:1]
			}
		case "boundary":
			for index := range evidence.PathProofs {
				evidence.PathProofs[index].SettlementClosures[0].Boundary.Number--
			}
		default:
			uri := evidence.PathProofs[0].SettlementClosures[0].Artifact.URI
			closure, err := validatorpkg.DecodeAttemptSettlementClosure(loaded[uri])
			if err != nil {
				t.Fatal(err)
			}
			if kind == "operator" {
				closure.Transitions = closure.Transitions[:1]
			} else {
				closure.Transitions[0].Signature[0] ^= 1
			}
			data, err := json.Marshal(closure)
			if err != nil {
				t.Fatal(err)
			}
			loaded[uri] = append(data, '\n')
		}
		if err := verifyFinalSettlementClosureArtifacts(evidence, loaded); err == nil {
			t.Fatalf("accepted %s mutation", kind)
		}
	}
}

// Lifecycle continuity remains available without exporting outside-window proofs.
func TestFinalSettlementClosureProofProjectionUsesExactEpochBounds(t *testing.T) {
	evidence, loaded, _ := finalSettlementClosureTestFixture(t)
	proof := evidence.PathProofs[0]
	records := map[uint64]validatorpkg.AttemptRecord{}
	for _, declared := range proof.SettlementClosures {
		closure, err := validatorpkg.DecodeAttemptSettlementClosure(loaded[declared.Artifact.URI])
		if err != nil {
			t.Fatal(err)
		}
		if err := mergeFinalAttemptCut(closure.Transitions[0].PreFold.AttemptCut, records); err != nil {
			t.Fatal(err)
		}
	}
	data, count, err := finalAcceptedAttemptProofBytes(records, 43, 43)
	if err != nil || count != 2 {
		t.Fatalf("exact epoch projection count=%d: %v", count, err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var envelope FinalCollectedProofRecord
		if err := json.Unmarshal(line, &envelope); err != nil || envelope.Record.Epoch != 43 {
			t.Fatalf("outside-window projection: %v", err)
		}
	}
	if len(records) != 16 {
		t.Fatalf("continuity records were dropped: %d", len(records))
	}
}

// A scan cannot complete before the publication barrier, and cancellation joins it.
func TestFinalSettlementClosureWaitHonorsPublicationAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, publish := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	scans := 0
	go func() {
		done <- runFinalSettlementClosureWait(ctx, time.Now().Add(time.Hour), time.Hour, func(context.Context) (bool, error) { scans++; return scans > 1, nil }, func(ctx context.Context, _ time.Duration) error {
			close(entered)
			select {
			case <-publish:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	<-entered
	select {
	case <-done:
		t.Fatal("closed capture before terminal publication")
	default:
	}
	close(publish)
	if err := <-done; err != nil || scans != 2 {
		t.Fatalf("publication handoff scans=%d: %v", scans, err)
	}
	cancel()
	if err := runFinalSettlementClosureWait(ctx, time.Now().Add(time.Hour), time.Hour, func(context.Context) (bool, error) { t.Error("cancelled wait scanned"); return true, nil }, func(context.Context, time.Duration) error { t.Error("cancelled wait polled"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait: %v", err)
	}
}

// The real wait authenticates the actual producer's last epoch bytes before
// handing off to capture, with publication forced between its two scans.
func TestFinalSettlementClosureWaitAuthenticatesLastWindow(t *testing.T) {
	evidence, loaded, _ := finalSettlementClosureTestFixture(t)
	cfg := testResolvedConfig(t)
	cfg.Config.Deployment.DeploymentID, cfg.ChainID, cfg.Netuid, cfg.Public.Chain.GenesisHash = evidence.DeploymentID, evidence.ChainID, evidence.Netuid, evidence.GenesisHash
	cfg.Config.Topology.Validators, cfg.Config.Topology.Operators = 1, 2
	terminal := &ScenarioObservation{Validators: []ValidatorObservation{{ValidatorID: 1, SelfUID: 12}}}
	for _, pool := range evidence.Pools {
		key, err := finalEd25519PublicKey("fixture server", pool.ServerKeyHistory[0].PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		terminal.Operators = append(terminal.Operators, OperatorObservation{NoID: int(pool.NoID), VerifyKeys: []VerifyKeyObservation{{ServerKeyID: 1, PublicKey: key}}})
	}
	stateRoot := t.TempDir()
	root := filepath.Join(stateRoot, "runtime", "validator-1", "state")
	if err := os.MkdirAll(filepath.Join(root, "operators", "no-1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "operators", "no-1", "client.key"), bytes.Repeat([]byte{0x41}, ed25519.SeedSize), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "settlement-closures"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(validatorpkg.AttemptSettlementClosurePath(root, 42), loaded["closure-42.json"], 0o400); err != nil {
		t.Fatal(err)
	}
	waits := 0
	err := waitFinalValidatorSettlementClosuresWithWait(context.Background(), cfg, stateRoot, terminal, &evidence.Window, time.Now().Add(time.Hour), time.Hour, func(context.Context, time.Duration) error {
		waits++
		if waits != 1 {
			t.Fatal("last terminal export did not complete the wait")
		}
		return os.WriteFile(validatorpkg.AttemptSettlementClosurePath(root, 43), loaded["closure-43.json"], 0o400)
	})
	if err != nil || waits != 1 {
		t.Fatalf("actual terminal handoff waits=%d: %v", waits, err)
	}
	cfg.Config.Deployment.DeploymentID += "-other"
	if err := waitFinalValidatorSettlementClosuresWithWait(context.Background(), cfg, stateRoot, terminal, &evidence.Window, time.Now().Add(time.Hour), time.Hour, func(context.Context, time.Duration) error {
		t.Error("permanent wrong domain retried")
		return errors.New("unexpected retry")
	}); err == nil {
		t.Fatal("terminal wait accepted a signed foreign domain")
	}
}

// The collected graph joins the complete attempt projection, not only its
// unsigned count/shape summary, before source reconstruction can consume it.
func TestFinalSettlementClosureCollectedGraphRejectsAttemptOmission(t *testing.T) {
	evidence, loaded, _ := finalSettlementClosureTestFixture(t)
	cfg := testResolvedConfig(t)
	cfg.Config.Deployment.DeploymentID, cfg.ChainID, cfg.Netuid, cfg.Public.Chain.GenesisHash = evidence.DeploymentID, evidence.ChainID, evidence.Netuid, evidence.GenesisHash
	cfg.Config.Topology.Validators, cfg.Config.Topology.Operators = 1, 2
	terminal := &ScenarioObservation{Validators: []ValidatorObservation{{ValidatorID: 1, SelfUID: 12}}}
	for _, pool := range evidence.Pools {
		key, err := finalEd25519PublicKey("fixture server", pool.ServerKeyHistory[0].PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		terminal.Operators = append(terminal.Operators, OperatorObservation{NoID: int(pool.NoID), VerifyKeys: []VerifyKeyObservation{{ServerKeyID: 1, PublicKey: key}}})
	}
	validator := FinalCollectedValidatorInputs{ValidatorID: 1, PathVPK: evidence.Validators[0].PathVPK, SettlementClosures: evidence.PathProofs[0].SettlementClosures}
	for _, proof := range evidence.PathProofs {
		records := map[uint64]validatorpkg.AttemptRecord{}
		for _, declared := range validator.SettlementClosures {
			closure, err := validatorpkg.DecodeAttemptSettlementClosure(loaded[declared.Artifact.URI])
			if err != nil {
				t.Fatal(err)
			}
			if err := mergeFinalAttemptCut(closure.Transitions[proof.NoID-1].PreFold.AttemptCut, records); err != nil {
				t.Fatal(err)
			}
		}
		runRoot := t.TempDir()
		_, summary, err := persistFinalAttemptRecords(runRoot, 1, int(proof.NoID), records)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(runRoot, filepath.FromSlash(summary.Artifact.URI)))
		if err != nil {
			t.Fatal(err)
		}
		loaded[summary.Artifact.URI] = data
		validator.Attempts = append(validator.Attempts, summary)
		validator.PathProofs = append(validator.PathProofs, FinalCollectedValidatorPathProof{NoID: proof.NoID, FirstEpoch: proof.FirstEpoch, LastEpoch: proof.LastEpoch, ProofCount: proof.ProofCount, Artifact: proof.Artifact})
	}
	value := &FinalSemanticCollectedInputs{Window: evidence.Window, Validators: []FinalCollectedValidatorInputs{validator}}
	if err := verifyFinalCollectedSettlementAuthority(cfg, value, terminal, loaded); err != nil {
		t.Fatal(err)
	}
	uri := validator.Attempts[0].Artifact.URI
	var attempts FinalCollectedAttemptRecords
	if err := json.Unmarshal(loaded[uri], &attempts); err != nil {
		t.Fatal(err)
	}
	attempts.Records = attempts.Records[:len(attempts.Records)-4]
	data, err := json.Marshal(attempts)
	if err != nil {
		t.Fatal(err)
	}
	loaded[uri] = data
	if err := verifyFinalCollectedSettlementAuthority(cfg, value, terminal, loaded); err == nil || !strings.Contains(err.Error(), "omits signed authority") {
		t.Fatalf("omitted complete attempt: %v", err)
	}
}
