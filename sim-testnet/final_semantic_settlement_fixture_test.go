package main

// Release-scale fixtures use the same signed attempt records for ordinary
// measurements and terminal proofs. No standalone unrelated proof census can
// accidentally satisfy the public closure verifier.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// The public closure is the producer's exact transaction, including its
// terminal boundary, not a second independently signed equivalent replay.
func TestFinalSemanticFixtureTerminalTransitionMatchesSuccessorMeasurement(t *testing.T) {
	validatorKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize))
	serverKeys := []ed25519.PrivateKey{
		ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize)),
		ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, ed25519.SeedSize)),
	}
	measurement := func(epoch uint64) *validatorpkg.ReleaseMeasurementArtifact {
		value := &validatorpkg.ReleaseMeasurementArtifact{
			DeploymentID: "fixture-terminal-transition", ChainID: 945, GenesisHash: finalTestHex(5),
			Netuid: 521, ValidatorID: 1, SelfUID: 12, SettlementEpoch: epoch,
		}
		for noID := uint64(1); noID <= 2; noID++ {
			input := validatorpkg.ReleaseMeasurementInput{
				NoID: noID, SettlementEpoch: epoch, CutEVMSnapshotBlock: 105 + (epoch-10)*finalReleaseEpochBlocks,
				CutEVMSnapshotHash: finalTestHex(byte(epoch)),
				Stats: validatorpkg.ReleaseStatsMeasurement{Config: validatorpkg.ReleaseStatsConfig{
					AMin: 8, AlphaNumerator: 1, AlphaDenominator: 10, LatRefMillis: 4000,
				}},
			}
			for index := uint64(1); index <= 4; index++ {
				clientID := finalAttemptFixtureID(noID*10 + index).String()
				input.Stats.Providers = append(input.Stats.Providers, validatorpkg.ReleaseProviderMeasurement{
					ClientID: clientID, LatencyBuckets: make([]uint64, 31), HasPriorQuality: true,
					PriorQualityPPM: 500_000, EgressIPHashHexes: []string{finalTestHex(byte(noID*10 + index))},
				})
				value.Bindings = append(value.Bindings, validatorpkg.ReleaseBindingMeasurement{NoID: noID, ClientID: clientID})
			}
			value.Inputs = append(value.Inputs, input)
		}
		return value
	}
	ledgers := map[uint64]*finalAttemptFixtureLedger{}
	first, second := measurement(10), measurement(11)
	attachFinalAttemptCuts(t, first, validatorKey, serverKeys, ledgers, nil)
	attachFinalAttemptCuts(t, second, validatorKey, serverKeys, ledgers, first)
	firstBytes, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := map[string][]byte{"measurement-10.json": firstBytes}
	validators := []FinalValidatorIdentityEvidence{{ValidatorID: 1, Cycles: []FinalCRv4Cycle{{
		MeasurementArtifact: FinalArtifactLocator{URI: "measurement-10.json"},
	}}}}
	proofs := finalSemanticFixtureClosedProofs(t, validators, []ed25519.PrivateKey{validatorKey}, finalSemanticFixtureMaximumAttemptM, artifacts, func(kind, name string, data []byte) FinalArtifactLocator {
		artifacts[name] = append([]byte(nil), data...)
		return FinalArtifactLocator{Kind: kind, URI: name, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	})
	if len(proofs) != 2 {
		t.Fatalf("fixture closure operator census=%d, want 2", len(proofs))
	}
	if len(proofs[0].SettlementClosures) != 1 {
		t.Fatalf("fixture closure epoch census=%d, want 1", len(proofs[0].SettlementClosures))
	}
	closure, err := validatorpkg.DecodeAttemptSettlementClosure(artifacts[proofs[0].SettlementClosures[0].Artifact.URI])
	if err != nil {
		t.Fatal(err)
	}
	if len(closure.Transitions) != len(second.Inputs) {
		t.Fatalf("fixture closure participant count=%d, want %d", len(closure.Transitions), len(second.Inputs))
	}
	for index, transition := range closure.Transitions {
		got := second.Inputs[index].Stats.SettlementTransition
		if got == nil {
			t.Fatalf("operator %d successor transition is absent", second.Inputs[index].NoID)
		}
		if !finalJSONEqual(got, transition) {
			t.Fatalf("operator %d successor transition differs from the exact exported terminal transaction: successor=%+v closure=%+v", second.Inputs[index].NoID, got.FromBoundary, transition.FromBoundary)
		}
	}
}

// Both successor measurements and public closures carry this exact terminal
// transaction. Only the cut boundary changes; its signed records are untouched.
func finalSemanticFixtureTerminalTransitions(t *testing.T, measurement *validatorpkg.ReleaseMeasurementArtifact, key ed25519.PrivateKey) []*validatorpkg.AttemptSettlementTransition {
	t.Helper()
	result, err := finalSemanticFixtureTerminalTransitionsResult(measurement, key)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Full terminal authentication returns errors to its joined preparation owner.
func finalSemanticFixtureTerminalTransitionsResult(measurement *validatorpkg.ReleaseMeasurementArtifact, key ed25519.PrivateKey) ([]*validatorpkg.AttemptSettlementTransition, error) {
	if len(key) != ed25519.PrivateKeySize || measurement == nil || measurement.SettlementEpoch < 9 || len(measurement.Inputs) == 0 {
		return nil, fmt.Errorf("fixture terminal transition measurement is incomplete")
	}
	epoch := measurement.SettlementEpoch
	boundary := validatorpkg.AttemptBoundary{SettlementEpoch: epoch, EVMBlock: 100 + (epoch-9)*finalReleaseEpochBlocks - 1, EVMBlockHash: finalTestHex(byte(0xe0 + epoch))}
	transitions := make([]*validatorpkg.AttemptSettlementTransition, 0, len(measurement.Inputs))
	for _, input := range measurement.Inputs {
		preFold := input.Stats
		preFold.SettlementTransition = nil
		if preFold.AttemptCut == nil || input.SettlementEpoch != epoch {
			return nil, fmt.Errorf("fixture terminal transition operator %d cut is incomplete", input.NoID)
		}
		cut := *preFold.AttemptCut
		cut.Boundary = boundary
		hashes := make([]string, len(cut.Records))
		for index := range cut.Records {
			hashes[index] = cut.Records[index].RecordHash
		}
		payload := finalAttemptLedgerCutSignaturePayload{Schema: cut.Schema, Identity: cut.Identity, Boundary: boundary, FirstSequence: cut.FirstSequence, EgressFirstSequence: cut.EgressFirstSequence, LastSequence: cut.LastSequence, RecordCount: cut.RecordCount, PriorRoot: cut.PriorRoot, Root: cut.Root, RecordHashes: hashes}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		cut.Signature = ed25519.Sign(key, append([]byte(finalAttemptLedgerCutSignDomain), data...))
		preFold.AttemptCut = &cut
		verified, err := validatorpkg.VerifyReleaseStatsMeasurement(preFold)
		if err != nil {
			return nil, err
		}
		qualities := make([]validatorpkg.AttemptSettlementQuality, 0, len(verified.Providers))
		for id, provider := range verified.Providers {
			if provider.HasQuality {
				qualities = append(qualities, validatorpkg.AttemptSettlementQuality{ClientID: id.String(), HasQuality: true, QualityPPM: provider.QualityPPM})
			}
		}
		sort.Slice(qualities, func(i, j int) bool { return qualities[i].ClientID < qualities[j].ClientID })
		transitions = append(transitions, &validatorpkg.AttemptSettlementTransition{Schema: finalAttemptSettlementSchema, Identity: cut.Identity, FromBoundary: boundary, ToEpoch: epoch + 1, PreFold: preFold, PostFold: qualities})
	}
	sort.Slice(transitions, func(i, j int) bool { return transitions[i].Identity.NoID < transitions[j].Identity.NoID })
	batch := make([]validatorpkg.AttemptSettlementMember, len(transitions))
	for index, transition := range transitions {
		digest, err := finalAttemptSettlementDigestResult(transition)
		if err != nil {
			return nil, err
		}
		batch[index] = validatorpkg.AttemptSettlementMember{NoID: transition.Identity.NoID, Digest: digest}
	}
	for _, transition := range transitions {
		transition.Batch = append([]validatorpkg.AttemptSettlementMember(nil), batch...)
		message, err := finalAttemptSettlementMessageResult(transition)
		if err != nil {
			return nil, err
		}
		transition.Signature = ed25519.Sign(key, message)
	}
	return transitions, nil
}

// Publishes every accepted terminal transaction and its exact proof projection;
// no proof is introduced independently of the signed measurement record chain.
func finalSemanticFixtureClosedProofs(t *testing.T, validators []FinalValidatorIdentityEvidence, keys []ed25519.PrivateKey, depth int, artifacts map[string][]byte, artifact func(string, string, []byte) FinalArtifactLocator) []FinalValidatorPathProofEvidence {
	t.Helper()
	return finalSemanticFixtureClosedProofsWithWorkObserver(t, validators, keys, depth, artifacts, artifact, nil)
}

// Observe the real terminal preparation slots while retaining their exact
// validator/epoch order and the complete signed-record proof projection.
func finalSemanticFixtureClosedProofsWithWorkObserver(t *testing.T, validators []FinalValidatorIdentityEvidence, keys []ed25519.PrivateKey, depth int, artifacts map[string][]byte, artifact func(string, string, []byte) FinalArtifactLocator, observer finalSemanticFixtureWorkObserver) []FinalValidatorPathProofEvidence {
	t.Helper()
	return finalSemanticFixtureClosedProofsWithWorkControl(t, validators, keys, depth, artifacts, artifact, finalSemanticFixtureWorkControl{ctx: t.Context(), observer: observer})
}

// Publish only after every terminal owner and canonical projection has joined.
func finalSemanticFixtureClosedProofsWithWorkControl(t *testing.T, validators []FinalValidatorIdentityEvidence, keys []ed25519.PrivateKey, depth int, artifacts map[string][]byte, artifact func(string, string, []byte) FinalArtifactLocator, work finalSemanticFixtureWorkControl) []FinalValidatorPathProofEvidence {
	t.Helper()
	proofs, owner, err := prepareFinalSemanticFixtureClosedProofs(validators, keys, depth, artifacts, work)
	if err != nil {
		t.Fatal(err)
	}
	joined, err := joinFinalSemanticFixtureArtifacts(artifacts, []*finalSemanticFixtureArtifacts{owner})
	if err != nil {
		t.Fatal(err)
	}
	if err := work.ctx.Err(); err != nil {
		t.Fatal(err)
	}
	locators := make(map[string]FinalArtifactLocator, len(joined))
	for _, value := range joined {
		locators[value.locator.URI] = artifact(value.locator.Kind, strings.TrimPrefix(value.locator.URI, "final-derived/"), value.data)
	}
	for index := range proofs {
		proofs[index].Artifact = locators[proofs[index].Artifact.URI]
		for closureIndex := range proofs[index].SettlementClosures {
			closure := &proofs[index].SettlementClosures[closureIndex]
			closure.Artifact = locators[closure.Artifact.URI]
		}
	}
	return proofs
}

// One owner decodes and authenticates an independent terminal transaction.
type finalSemanticFixtureTerminalJob struct {
	validatorID uint64
	data        []byte
	key         ed25519.PrivateKey
	closure     *validatorpkg.AttemptSettlementClosure
	wire        []byte
}

// The real-stage barrier follows full transition authentication. Queued
// goroutines alone cannot satisfy the preparation overlap control.
func prepareFinalSemanticFixtureTerminal(job *finalSemanticFixtureTerminalJob, work finalSemanticFixtureWorkControl, index int) error {
	var measurement validatorpkg.ReleaseMeasurementArtifact
	if err := json.Unmarshal(job.data, &measurement); err != nil {
		return err
	}
	epoch := measurement.SettlementEpoch
	if epoch < 10 || epoch > 14 {
		return nil
	}
	transitions, err := finalSemanticFixtureTerminalTransitionsResult(&measurement, job.key)
	if err != nil {
		return err
	}
	leave, err := work.enter(finalSemanticFixtureTerminalClosures, index)
	if err != nil {
		return err
	}
	defer leave()
	closure := &validatorpkg.AttemptSettlementClosure{Schema: validatorpkg.AttemptSettlementClosureSchema, Epoch: epoch, Transitions: transitions}
	data, err := json.Marshal(closure)
	if err != nil {
		return err
	}
	if err := work.ctx.Err(); err != nil {
		return err
	}
	job.closure, job.wire = closure, append(data, '\n')
	return nil
}

// Independent epochs are flattened once. Caller-only joins preserve each
// validator's exact accepted records and complete ordered proof projection.
func prepareFinalSemanticFixtureClosedProofs(validators []FinalValidatorIdentityEvidence, keys []ed25519.PrivateKey, depth int, artifacts map[string][]byte, work finalSemanticFixtureWorkControl) ([]FinalValidatorPathProofEvidence, *finalSemanticFixtureArtifacts, error) {
	var jobs []finalSemanticFixtureTerminalJob
	spans := make([][2]int, len(validators))
	for validatorIndex, validator := range validators {
		if validator.ValidatorID == 0 || validator.ValidatorID > uint64(len(keys)) || len(keys[validator.ValidatorID-1]) != ed25519.PrivateKeySize {
			return nil, nil, fmt.Errorf("fixture terminal validator %d key is incomplete", validator.ValidatorID)
		}
		spans[validatorIndex][0] = len(jobs)
		for _, cycle := range validator.Cycles {
			data, found := artifacts[cycle.MeasurementArtifact.URI]
			if !found {
				return nil, nil, fmt.Errorf("fixture terminal measurement %s is absent", cycle.MeasurementArtifact.URI)
			}
			jobs = append(jobs, finalSemanticFixtureTerminalJob{validatorID: validator.ValidatorID, data: append([]byte(nil), data...), key: append(ed25519.PrivateKey(nil), keys[validator.ValidatorID-1]...)})
		}
		spans[validatorIndex][1] = len(jobs)
	}
	if err := work.run(finalSemanticFixtureTerminalClosures, len(jobs), 4, func(ctx context.Context, index int) error {
		stageWork := work
		stageWork.ctx = ctx
		return prepareFinalSemanticFixtureTerminal(&jobs[index], stageWork, index)
	}); err != nil {
		return nil, nil, err
	}
	owner := &finalSemanticFixtureArtifacts{}
	var proofs []FinalValidatorPathProofEvidence
	for validatorIndex, validator := range validators {
		records := map[uint64]map[uint64]validatorpkg.AttemptRecord{}
		var closures []FinalCollectedSettlementClosure
		for _, job := range jobs[spans[validatorIndex][0]:spans[validatorIndex][1]] {
			if job.closure == nil {
				continue
			}
			closure := job.closure
			for _, transition := range closure.Transitions {
				noID := transition.Identity.NoID
				if records[noID] == nil {
					records[noID] = map[uint64]validatorpkg.AttemptRecord{}
				}
				if err := mergeFinalAttemptCut(transition.PreFold.AttemptCut, records[noID]); err != nil {
					return nil, nil, err
				}
			}
			locator := owner.artifact("validator-settlement-closure", fmt.Sprintf("settlement-closure-%d-%d.json", validator.ValidatorID, closure.Epoch), job.wire)
			boundary := closure.Transitions[0].FromBoundary
			closures = append(closures, FinalCollectedSettlementClosure{Epoch: closure.Epoch, Boundary: ChainHead{Number: boundary.EVMBlock, Hash: boundary.EVMBlockHash}, Artifact: locator})
		}
		sort.Slice(closures, func(i, j int) bool { return closures[i].Epoch < closures[j].Epoch })
		for noID := uint64(1); noID <= uint64(len(records)); noID++ {
			data, count, err := finalAcceptedAttemptProofBytes(records[noID], 10, 14)
			if err != nil {
				return nil, nil, err
			}
			locator := owner.artifact("validator-path-proofs", fmt.Sprintf("path-proofs-%d-%d.jsonl", validator.ValidatorID, noID), data)
			proofs = append(proofs, FinalValidatorPathProofEvidence{ValidatorID: validator.ValidatorID, NoID: noID, FirstEpoch: 10, LastEpoch: 14, ProofCount: count, TrailDepth: depth, ProofsHash: locator.ContentHash, Artifact: locator, SettlementClosures: append([]FinalCollectedSettlementClosure(nil), closures...)})
		}
	}
	if owner.err != nil {
		return nil, nil, owner.err
	}
	if err := work.ctx.Err(); err != nil {
		return nil, nil, err
	}
	return proofs, owner, nil
}
