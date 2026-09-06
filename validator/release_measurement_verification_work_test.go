package validator

// Observes complete signed-cut work in an enclosing release measurement, with
// real M8 trails, full policy reliability, durable closure and detached inputs.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urnetwork/connect"
)

// Retains immutable canonical fixture bytes only, never a validation result or
// an open ledger. Every caller receives a freshly decoded, independently owned
// graph, so later mutation cannot inherit authority from an earlier call.
var releaseVerificationWorkFixtureCache struct {
	stateLock sync.Mutex
	wire      []byte
}

// Uses a stable validator key for genuine signatures on adjacency mutations.
// The server signatures and all pending/terminal records remain real.
func releaseVerificationWorkValidatorKey() ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x69}, ed25519.SeedSize))
}

// Completes eight M8 trails per operator, atomically folds epoch 42, then makes
// a genuine successor trail and ordinary cut. The legacy ledger is deliberate:
// the v2 disk backend refuses the materialized v1 cut used by this public API.
func buildReleaseVerificationWorkFixture(t *testing.T) []byte {
	t.Helper()
	policy := exactPolicy(t)
	if policy.Verify.TrailDepth != 8 || policy.Verify.ReliabilityAMin != 8 || policy.Safety.MinimumHealthyNOCount != 2 {
		t.Fatal("verification fixture no longer uses the complete configured M8/AMin8 policy")
	}
	policyHash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	validatorKey := releaseVerificationWorkValidatorKey()
	boundary := attemptLedgerTestBoundary()
	artifact := &ReleaseMeasurementArtifact{
		Schema: ReleaseMeasurementSchema, DeploymentID: "measurement-verification-work", ChainID: 945,
		GenesisHash: releaseHex32(releaseMeasurementTestHash(1)), Netuid: 521, ValidatorID: 1, SelfUID: 3,
		Coordinator:     "0x0000000000000000000000000000000000000001",
		SettlementVault: "0x0000000000000000000000000000000000000002",
		SubnetEpoch:     7, NativeSnapshotBlock: 100, NativeSnapshotHash: releaseHex32(releaseMeasurementTestHash(2)),
		EVMSnapshotBlock: 105, EVMSnapshotHash: releaseHex32(releaseMeasurementTestHash(3)), SettlementEpoch: 43,
		Policy: policy, PolicyHash: releaseHex32(policyHash), ControlledNOIDs: []uint64{},
		Inputs: []ReleaseMeasurementInput{}, Bindings: []ReleaseBindingMeasurement{}, HeadEMA: []HeadEMAMeasurement{},
		Pools: []ReleasePoolMeasurement{}, DepositAudits: []DepositAudit{},
	}
	participants := make([]AttemptSettlementParticipant, 0, 2)
	engines := make([]*TrailEngine, 0, 2)
	ledgers := make([]*AttemptLedger, 0, 2)
	serverKeyKVs := make(map[uint64]map[byte]ed25519.PublicKey, 2)
	for noID := uint64(1); noID <= 2; noID++ {
		server, _, clientID := newMockVerifyServer(t, policy.Verify.TrailDepth)
		server.validatorVpk = validatorKey.Public().(ed25519.PublicKey)
		stateDir := filepath.Join(t.TempDir(), "state")
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		ledger, err := NewAttemptLedger(stateDir, AttemptLedgerIdentity{
			DeploymentID: artifact.DeploymentID, ChainID: artifact.ChainID, GenesisHash: artifact.GenesisHash,
			Netuid: artifact.Netuid, ValidatorID: artifact.ValidatorID, ValidatorUID: artifact.SelfUID, NoID: noID,
		}, validatorKey)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := ledger.Close(); err != nil {
				t.Error(err)
			}
		})
		stats := NewStatsEngine(StatsConfig{AMin: policy.Verify.ReliabilityAMin})
		if err := stats.AdvanceSettlementEpoch(boundary.SettlementEpoch, stateDir); err != nil {
			t.Fatal(err)
		}
		if err := stats.AttachAttemptLedger(ledger, stateDir); err != nil {
			t.Fatal(err)
		}
		seedHop := server.providers[0]
		engine := NewTrailEngine(clientID, validatorKey, server, NewStaticServerKeyRing(server.serverPublicKeys()),
			func(context.Context) (connect.Id, error) { return seedHop, nil }, stats, nil,
			func() uint64 { return boundary.SettlementEpoch }, TrailEngineConfig{
				M: policy.Verify.TrailDepth, StepTimeout: time.Duration(policy.Verify.StepTimeoutSeconds) * time.Second,
				AttemptLedger: ledger,
				AttemptBoundaryResolver: func(_ context.Context, pinned *AttemptBoundary, clientIDs []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
					if pinned != nil && *pinned != boundary {
						return AttemptBoundary{}, nil, errors.New("verification work fixture changed its pinned boundary")
					}
					bindings := make([]AttemptBinding, len(clientIDs))
					for index, id := range clientIDs {
						bindings[index] = AttemptBinding{ClientID: id, FleetID: releaseHex32([32]byte{}), Hotkey: releaseHex32([32]byte{})}
					}
					return boundary, bindings, nil
				},
			})
		for trail := uint64(0); trail < policy.Verify.ReliabilityAMin; trail++ {
			proof, err := engine.RunTrail(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := VerifyProofRecord(proof, validatorKey.Public().(ed25519.PublicKey), server.serverPublicKeys(), policy.Verify.TrailDepth); err != nil {
				t.Fatal(err)
			}
		}
		participants = append(participants, AttemptSettlementParticipant{NoID: noID, StateDir: stateDir, Stats: stats})
		engines, ledgers = append(engines, engine), append(ledgers, ledger)
		serverKeyKVs[noID] = server.serverPublicKeys()
	}
	closureDir := t.TempDir()
	if err := AdvanceAttemptSettlementEpoch(closureDir, artifact.SettlementEpoch, boundary, participants); err != nil {
		t.Fatal(err)
	}
	closureWire, err := ReadAttemptSettlementClosure(closureDir, boundary.SettlementEpoch)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := DecodeAttemptSettlementClosureWithServerKeys(closureWire, serverKeyKVs)
	if err != nil || len(closure.Transitions) != 2 {
		t.Fatalf("complete two-operator closure: %v", err)
	}
	for _, transition := range closure.Transitions {
		if len(transition.PreFold.AttemptCut.Records) != 64 || len(transition.PreFold.Providers) != 7 || len(transition.PostFold) != 7 {
			t.Fatal("verification fixture lost M8 pending checkpoints or the complete eligible provider census")
		}
		for _, provider := range transition.PreFold.Providers {
			if provider.Assignments != policy.Verify.ReliabilityAMin || provider.Confirmations != policy.Verify.ReliabilityAMin {
				t.Fatal("verification fixture lowered the actual reliability workload")
			}
		}
	}
	boundary = AttemptBoundary{SettlementEpoch: artifact.SettlementEpoch, EVMBlock: artifact.EVMSnapshotBlock, EVMBlockHash: artifact.EVMSnapshotHash}
	for index, participant := range participants {
		proof, err := engines[index].RunTrail(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := VerifyProofRecord(proof, validatorKey.Public().(ed25519.PublicKey), serverKeyKVs[participant.NoID], policy.Verify.TrailDepth); err != nil {
			t.Fatal(err)
		}
		var persisted []byte
		var generation uint64
		measurement, err := participant.Stats.detachReleaseStatsMeasurementWithAttemptCut(participant.StateDir, boundary, func(value ReleaseStatsMeasurement, cutGeneration uint64) error {
			generation = cutGeneration
			var encodeErr error
			persisted, encodeErr = json.Marshal(value)
			if encodeErr != nil {
				return encodeErr
			}
			return atomicStateWrite(filepath.Join(participant.StateDir, "verification-work-cut.json"), persisted, 0o600)
		})
		if err != nil {
			t.Fatal(err)
		}
		actual, err := json.Marshal(measurement)
		if err != nil || !bytes.Equal(actual, persisted) {
			t.Fatalf("ordinary cut differs from its durable atomic snapshot: %v", err)
		}
		cut := measurement.AttemptCut
		if cut == nil || measurement.SettlementTransition == nil || cut.FirstSequence != 65 || cut.LastSequence != 72 || len(cut.Records) != 8 || cut.PriorRoot != measurement.SettlementTransition.PreFold.AttemptCut.Root {
			t.Fatal("verification fixture lost the signed terminal-to-successor range")
		}
		if err := VerifyAttemptLedgerCut(cut, validatorKey.Public().(ed25519.PublicKey), serverKeyKVs[participant.NoID]); err != nil {
			t.Fatal(err)
		}
		artifact.Inputs = append(artifact.Inputs, ReleaseMeasurementInput{
			NoID: participant.NoID, SettlementEpoch: artifact.SettlementEpoch, CutNativeBlock: 99,
			CutNativeBlockHash: releaseHex32(releaseMeasurementTestHash(4)), CutEVMSnapshotBlock: boundary.EVMBlock,
			CutEVMSnapshotHash: boundary.EVMBlockHash, EgressGeneration: generation, Stats: measurement,
		})
		for _, provider := range measurement.Providers {
			zero := releaseHex32([32]byte{})
			artifact.Bindings = append(artifact.Bindings, ReleaseBindingMeasurement{
				NoID: participant.NoID, ClientID: provider.ClientID, FleetID: zero, Hotkey: zero,
				ClientKey: zero, LocalClientKey: zero, CommitmentHash: zero,
			})
		}
		artifact.Pools = append(artifact.Pools, ReleasePoolMeasurement{NoID: participant.NoID, UID: uint16(participant.NoID), PoolHotkey: releaseHex32(releaseMeasurementTestHash(10 + participant.NoID))})
		audit := releaseMeasurementDepositAudit(t, policy, participant.NoID)
		audit.Epoch, audit.SourceEpoch, audit.ObservedAtBlock = artifact.SettlementEpoch, artifact.SettlementEpoch-policy.Deposit.UsageLagEpochs, artifact.EVMSnapshotBlock
		artifact.DepositAudits = append(artifact.DepositAudits, audit)
		if err := ledgers[index].Close(); err != nil {
			t.Fatal(err)
		}
	}
	wire, _, verified, err := SealReleaseMeasurementArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if len(verified.StatsByNO) != 2 || len(verified.Pools) != 2 || !reflect.DeepEqual(verified.UIDs, []uint16{1, 2}) || len(verified.Scores) != 2 || verified.Scores[0].Sign() <= 0 || verified.Scores[1].Sign() <= 0 {
		t.Fatal("verification fixture lost the complete two-operator positive decision")
	}
	if _, _, err := DecodeReleaseMeasurementArtifact(wire); err != nil {
		t.Fatal(err)
	}
	return wire
}

// Copies all nested signed messages before each independent public call.
func releaseVerificationWorkFixture(t *testing.T) *ReleaseMeasurementArtifact {
	t.Helper()
	var wire []byte
	func() {
		releaseVerificationWorkFixtureCache.stateLock.Lock()
		defer releaseVerificationWorkFixtureCache.stateLock.Unlock()
		if len(releaseVerificationWorkFixtureCache.wire) == 0 {
			releaseVerificationWorkFixtureCache.wire = buildReleaseVerificationWorkFixture(t)
		}
		wire = releaseVerificationWorkFixtureCache.wire
	}()
	var artifact ReleaseMeasurementArtifact
	if err := json.Unmarshal(wire, &artifact); err != nil {
		t.Fatal(err)
	}
	return &artifact
}

// One public reconstruction already authenticates the current and terminal
// cuts through each input. Joining their batch must not repeat the terminal.
func TestReleaseMeasurementAuthenticatesEachCompleteCutOnce(t *testing.T) {
	artifact := releaseVerificationWorkFixture(t)
	cutCountKVs := make(map[*AttemptLedgerCut]int, 4)
	for _, input := range artifact.Inputs {
		cutCountKVs[input.Stats.AttemptCut] = 0
		cutCountKVs[input.Stats.SettlementTransition.PreFold.AttemptCut] = 0
	}
	cutCalls := 0
	_, err := verifyReleaseMeasurementArtifactWithCutVerifier(artifact, func(cut *AttemptLedgerCut, vpk ed25519.PublicKey, keys map[byte]ed25519.PublicKey, requireServerKeys bool) error {
		if _, exists := cutCountKVs[cut]; !exists || keys != nil || requireServerKeys {
			t.Fatal("measurement cut verification changed its exact input or public key mode")
		}
		cutCalls++
		cutCountKVs[cut]++
		return verifyAttemptLedgerCut(cut, vpk, keys, requireServerKeys)
	})
	if err != nil {
		t.Fatal(err)
	}
	if cutCalls != 4 {
		t.Fatalf("authenticated complete ordinary/terminal cuts %d times, want exactly 4 within one release measurement", cutCalls)
	}
	for _, count := range cutCountKVs {
		if count != 1 {
			t.Fatalf("complete signed cut authenticated %d times, want 1", count)
		}
	}
}

// Independent statistics calls remain full verifiers, including a repeated
// terminal signature after the caller edits and restores the same graph.
func TestReleaseMeasurementStandaloneStatsAuthenticateEveryCall(t *testing.T) {
	artifact := releaseVerificationWorkFixture(t)
	measurement := artifact.Inputs[0].Stats
	signature := measurement.SettlementTransition.PreFold.AttemptCut.Records[0].Signature
	for _, invalid := range []bool{false, true, false} {
		if invalid {
			signature[0] ^= 1
		}
		cutCalls := 0
		_, err := verifyReleaseStatsMeasurementWithCutVerifier(measurement, func(cut *AttemptLedgerCut, vpk ed25519.PublicKey, keys map[byte]ed25519.PublicKey, requireServerKeys bool) error {
			cutCalls++
			return verifyAttemptLedgerCut(cut, vpk, keys, requireServerKeys)
		})
		if invalid {
			signature[0] ^= 1
		}
		if cutCalls != 2 || (err != nil) != invalid || invalid && !strings.Contains(err.Error(), "attempt record validator signature is invalid") {
			t.Fatalf("invalid=%t standalone statistics cut calls=%d error=%v", invalid, cutCalls, err)
		}
	}
}

// Standalone batch verification authenticates every transition in sorted
// participant order, without relying on an earlier measurement call.
func TestAttemptSettlementBatchStandaloneAuthenticatesEveryCall(t *testing.T) {
	artifact := releaseVerificationWorkFixture(t)
	transitions := []*AttemptSettlementTransition{artifact.Inputs[1].Stats.SettlementTransition, artifact.Inputs[0].Stats.SettlementTransition}
	for _, invalid := range []bool{false, true, false} {
		if invalid {
			transitions[0].Signature[0] ^= 1
		}
		cutCalls := 0
		err := verifyAttemptSettlementBatchWithCutVerifier(transitions, func(cut *AttemptLedgerCut, vpk ed25519.PublicKey, keys map[byte]ed25519.PublicKey, requireServerKeys bool) error {
			cutCalls++
			return verifyAttemptLedgerCut(cut, vpk, keys, requireServerKeys)
		})
		if invalid {
			transitions[0].Signature[0] ^= 1
		}
		if cutCalls != 2 || (err != nil) != invalid || invalid && !strings.Contains(err.Error(), "settlement transition validator signature is invalid") {
			t.Fatalf("invalid=%t standalone batch cut calls=%d error=%v", invalid, cutCalls, err)
		}
		if transitions[0].Identity.NoID != 2 || transitions[1].Identity.NoID != 1 {
			t.Fatal("standalone batch reordered the caller's participant slice")
		}
	}
}

// Same-pointer success never authorizes a later changed transition, nor can a
// failed call poison the restored genuine graph.
func TestReleaseMeasurementRechecksTransitionsAcrossCalls(t *testing.T) {
	artifact := releaseVerificationWorkFixture(t)
	for _, invalid := range []bool{false, true, false} {
		if invalid {
			artifact.Inputs[1].Stats.SettlementTransition.Signature[0] ^= 1
		}
		cutCalls := 0
		_, err := verifyReleaseMeasurementArtifactWithCutVerifier(artifact, func(cut *AttemptLedgerCut, vpk ed25519.PublicKey, keys map[byte]ed25519.PublicKey, requireServerKeys bool) error {
			cutCalls++
			return verifyAttemptLedgerCut(cut, vpk, keys, requireServerKeys)
		})
		if invalid {
			artifact.Inputs[1].Stats.SettlementTransition.Signature[0] ^= 1
		}
		if cutCalls < 4 || (err != nil) != invalid || invalid && !strings.Contains(err.Error(), "settlement transition validator signature is invalid") {
			t.Fatalf("invalid=%t public measurement cut calls=%d error=%v", invalid, cutCalls, err)
		}
	}
}

// A valid per-member signature does not make two different manifests one
// transaction. Only the complete containing join can reject this adjacency.
func TestReleaseMeasurementRejectsIndividuallyValidDifferentBatches(t *testing.T) {
	artifact := releaseVerificationWorkFixture(t)
	transition := artifact.Inputs[0].Stats.SettlementTransition
	transition.Batch[1].Digest = releaseHex32(releaseMeasurementTestHash(900))
	message, err := attemptSettlementTransitionMessage(transition)
	if err != nil {
		t.Fatal(err)
	}
	transition.Signature = ed25519.Sign(releaseVerificationWorkValidatorKey(), message)
	for _, input := range artifact.Inputs {
		if _, err := VerifyReleaseStatsMeasurement(input.Stats); err != nil {
			t.Fatalf("manifest drift invalidated an independently signed member: %v", err)
		}
	}
	if _, err := VerifyReleaseMeasurementArtifact(artifact); err == nil || !strings.Contains(err.Error(), "settlement transitions do not share one transaction") {
		t.Fatalf("individually valid but different settlement batches were accepted: %v", err)
	}
}

// Removing just one transition still leaves individually legal statistics;
// the containing artifact must require complete all-operator coverage.
func TestReleaseMeasurementRetainsCompleteSettlementCoverage(t *testing.T) {
	artifact := releaseVerificationWorkFixture(t)
	artifact.Inputs[1].Stats.SettlementTransition = nil
	for _, input := range artifact.Inputs {
		if _, err := VerifyReleaseStatsMeasurement(input.Stats); err != nil {
			t.Fatalf("coverage omission invalidated individual statistics: %v", err)
		}
	}
	if _, err := VerifyReleaseMeasurementArtifact(artifact); err == nil || !strings.Contains(err.Error(), "settlement transition coverage is partial") {
		t.Fatalf("partial settlement transition coverage was accepted: %v", err)
	}
	artifact = releaseVerificationWorkFixture(t)
	transitions := []*AttemptSettlementTransition{artifact.Inputs[0].Stats.SettlementTransition}
	if err := VerifyAttemptSettlementBatch(transitions); err == nil || !strings.Contains(err.Error(), "settlement transition batch coverage differs") {
		t.Fatalf("standalone incomplete settlement membership was accepted: %v", err)
	}
}

// Ordinary and terminal signatures, raw counters and both sides of the EMA
// fold remain required even when the enclosing batch avoids duplicate work.
func TestReleaseMeasurementRetainsTerminalAndSuccessorChecks(t *testing.T) {
	for _, mutation := range []struct {
		name string
		edit func(*ReleaseMeasurementArtifact)
	}{
		{name: "ordinary record", edit: func(artifact *ReleaseMeasurementArtifact) {
			artifact.Inputs[0].Stats.AttemptCut.Records[0].Signature[0] ^= 1
		}},
		{name: "ordinary cut", edit: func(artifact *ReleaseMeasurementArtifact) { artifact.Inputs[0].Stats.AttemptCut.Signature[0] ^= 1 }},
		{name: "terminal record", edit: func(artifact *ReleaseMeasurementArtifact) {
			artifact.Inputs[0].Stats.SettlementTransition.PreFold.AttemptCut.Records[0].Signature[0] ^= 1
		}},
		{name: "terminal cut", edit: func(artifact *ReleaseMeasurementArtifact) {
			artifact.Inputs[0].Stats.SettlementTransition.PreFold.AttemptCut.Signature[0] ^= 1
		}},
		{name: "terminal counters", edit: func(artifact *ReleaseMeasurementArtifact) {
			artifact.Inputs[0].Stats.SettlementTransition.PreFold.Providers[0].Assignments++
		}},
		{name: "folded quality", edit: func(artifact *ReleaseMeasurementArtifact) {
			artifact.Inputs[0].Stats.SettlementTransition.PostFold[0].QualityPPM++
		}},
		{name: "prior quality", edit: func(artifact *ReleaseMeasurementArtifact) { artifact.Inputs[0].Stats.Providers[0].PriorQualityPPM++ }},
	} {
		artifact := releaseVerificationWorkFixture(t)
		mutation.edit(artifact)
		if _, err := VerifyReleaseMeasurementArtifact(artifact); err == nil {
			t.Fatalf("%s mutation passed public measurement verification", mutation.name)
		}
	}
	if _, err := VerifyReleaseMeasurementArtifact(releaseVerificationWorkFixture(t)); err != nil {
		t.Fatalf("complete unmodified control failed: %v", err)
	}
}

// Both successful and rejected reconstructions leave every signed input byte
// unchanged; derived maps and sorting must remain private to the verifier.
func TestReleaseMeasurementDoesNotMutateVerificationInputs(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		artifact := releaseVerificationWorkFixture(t)
		if invalid {
			artifact.Inputs[0].Stats.SettlementTransition.Signature[0] ^= 1
		}
		before, err := json.Marshal(artifact)
		if err != nil {
			t.Fatal(err)
		}
		_, verifyErr := VerifyReleaseMeasurementArtifact(artifact)
		after, err := json.Marshal(artifact)
		if err != nil || (verifyErr != nil) != invalid || !bytes.Equal(before, after) {
			t.Fatalf("invalid=%t public verification mutated input or changed validity: verify=%v encode=%v", invalid, verifyErr, err)
		}
	}
}
