package main

// Release-scale fixtures use the same signed attempt records for ordinary
// measurements and terminal proofs. No standalone unrelated proof census can
// accidentally satisfy the public closure verifier.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
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
	if measurement == nil || measurement.SettlementEpoch < 9 || len(measurement.Inputs) == 0 {
		t.Fatal("fixture terminal transition measurement is incomplete")
	}
	epoch := measurement.SettlementEpoch
	boundary := validatorpkg.AttemptBoundary{SettlementEpoch: epoch, EVMBlock: 100 + (epoch-9)*finalReleaseEpochBlocks - 1, EVMBlockHash: finalTestHex(byte(0xe0 + epoch))}
	transitions := make([]*validatorpkg.AttemptSettlementTransition, 0, len(measurement.Inputs))
	for _, input := range measurement.Inputs {
		preFold := input.Stats
		preFold.SettlementTransition = nil
		if preFold.AttemptCut == nil || input.SettlementEpoch != epoch {
			t.Fatalf("fixture terminal transition operator %d cut is incomplete", input.NoID)
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
			t.Fatal(err)
		}
		cut.Signature = ed25519.Sign(key, append([]byte(finalAttemptLedgerCutSignDomain), data...))
		preFold.AttemptCut = &cut
		verified, err := validatorpkg.VerifyReleaseStatsMeasurement(preFold)
		if err != nil {
			t.Fatal(err)
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
		batch[index] = validatorpkg.AttemptSettlementMember{NoID: transition.Identity.NoID, Digest: finalAttemptSettlementDigest(t, transition)}
	}
	for _, transition := range transitions {
		transition.Batch = append([]validatorpkg.AttemptSettlementMember(nil), batch...)
		transition.Signature = ed25519.Sign(key, finalAttemptSettlementMessage(t, transition))
	}
	return transitions
}

// Publishes every accepted terminal transaction and its exact proof projection;
// no proof is introduced independently of the signed measurement record chain.
func finalSemanticFixtureClosedProofs(t *testing.T, validators []FinalValidatorIdentityEvidence, keys []ed25519.PrivateKey, depth int, artifacts map[string][]byte, artifact func(string, string, []byte) FinalArtifactLocator) []FinalValidatorPathProofEvidence {
	t.Helper()
	var proofs []FinalValidatorPathProofEvidence
	for _, validator := range validators {
		records := map[uint64]map[uint64]validatorpkg.AttemptRecord{}
		var closures []FinalCollectedSettlementClosure
		var next *validatorpkg.ReleaseMeasurementArtifact
		for cycleIndex, cycle := range validator.Cycles {
			measurement := next
			if measurement == nil {
				measurement = &validatorpkg.ReleaseMeasurementArtifact{}
				if err := json.Unmarshal(artifacts[cycle.MeasurementArtifact.URI], measurement); err != nil {
					t.Fatal(err)
				}
			}
			next = nil
			if cycleIndex+1 < len(validator.Cycles) {
				next = &validatorpkg.ReleaseMeasurementArtifact{}
				if err := json.Unmarshal(artifacts[validator.Cycles[cycleIndex+1].MeasurementArtifact.URI], next); err != nil {
					t.Fatal(err)
				}
			}
			epoch := measurement.SettlementEpoch
			if epoch < 10 || epoch > 14 {
				continue
			}
			var transitions []*validatorpkg.AttemptSettlementTransition
			if next != nil {
				var err error
				transitions, err = finalSemanticFixtureRetainedTransitions(measurement, next)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if validator.ValidatorID == 0 || validator.ValidatorID > uint64(len(keys)) || len(keys[validator.ValidatorID-1]) != ed25519.PrivateKeySize {
					t.Fatal("fixture terminal epoch without a successor requires its signing key")
				}
				transitions = finalSemanticFixtureTerminalTransitions(t, measurement, keys[validator.ValidatorID-1])
			}
			closure := &validatorpkg.AttemptSettlementClosure{Schema: validatorpkg.AttemptSettlementClosureSchema, Epoch: epoch, Transitions: transitions}
			for _, transition := range closure.Transitions {
				noID := transition.Identity.NoID
				if records[noID] == nil {
					records[noID] = map[uint64]validatorpkg.AttemptRecord{}
				}
				if err := mergeFinalAttemptCut(transition.PreFold.AttemptCut, records[noID]); err != nil {
					t.Fatal(err)
				}
			}
			data, err := json.Marshal(closure)
			if err != nil {
				t.Fatal(err)
			}
			locator := artifact("validator-settlement-closure", fmt.Sprintf("settlement-closure-%d-%d.json", validator.ValidatorID, epoch), append(data, '\n'))
			boundary := closure.Transitions[0].FromBoundary
			closures = append(closures, FinalCollectedSettlementClosure{Epoch: epoch, Boundary: ChainHead{Number: boundary.EVMBlock, Hash: boundary.EVMBlockHash}, Artifact: locator})
		}
		sort.Slice(closures, func(i, j int) bool { return closures[i].Epoch < closures[j].Epoch })
		for noID := uint64(1); noID <= uint64(len(records)); noID++ {
			data, count, err := finalAcceptedAttemptProofBytes(records[noID], 10, 14)
			if err != nil {
				t.Fatal(err)
			}
			locator := artifact("validator-path-proofs", fmt.Sprintf("path-proofs-%d-%d.jsonl", validator.ValidatorID, noID), data)
			proofs = append(proofs, FinalValidatorPathProofEvidence{ValidatorID: validator.ValidatorID, NoID: noID, FirstEpoch: 10, LastEpoch: 14, ProofCount: count, TrailDepth: depth, ProofsHash: locator.ContentHash, Artifact: locator, SettlementClosures: append([]FinalCollectedSettlementClosure(nil), closures...)})
		}
	}
	return proofs
}

// Reuses the exact transaction already sealed into the successor, rather than
// signing and replaying every accepted epoch a second time during construction.
// These are builder-owned decoded values, not a replacement for the production
// closure verifier: every exported byte still goes through that verifier.
func finalSemanticFixtureRetainedTransitions(measurement, successor *validatorpkg.ReleaseMeasurementArtifact) ([]*validatorpkg.AttemptSettlementTransition, error) {
	if measurement == nil || successor == nil || measurement.SettlementEpoch < 9 || measurement.SettlementEpoch == ^uint64(0) || successor.SettlementEpoch != measurement.SettlementEpoch+1 || len(measurement.Inputs) == 0 || len(measurement.Inputs) != len(successor.Inputs) {
		return nil, errors.New("fixture retained terminal epoch or participant census differs")
	}
	if measurement.DeploymentID != successor.DeploymentID || measurement.ChainID != successor.ChainID || measurement.GenesisHash != successor.GenesisHash || measurement.Netuid != successor.Netuid || measurement.ValidatorID != successor.ValidatorID || measurement.SelfUID != successor.SelfUID || measurement.Coordinator != successor.Coordinator || measurement.SettlementVault != successor.SettlementVault {
		return nil, errors.New("fixture retained terminal validator domain differs")
	}
	epoch := measurement.SettlementEpoch
	boundary := validatorpkg.AttemptBoundary{SettlementEpoch: epoch, EVMBlock: 100 + (epoch-9)*finalReleaseEpochBlocks - 1, EVMBlockHash: finalTestHex(byte(0xe0 + epoch))}
	transitions := make([]*validatorpkg.AttemptSettlementTransition, len(measurement.Inputs))
	for index, input := range measurement.Inputs {
		next := successor.Inputs[index]
		transition := next.Stats.SettlementTransition
		if input.NoID == 0 || index > 0 && input.NoID <= measurement.Inputs[index-1].NoID || next.NoID != input.NoID || input.SettlementEpoch != epoch || next.SettlementEpoch != epoch+1 || input.Stats.AttemptCut == nil || next.Stats.AttemptCut == nil || transition == nil || transition.PreFold.AttemptCut == nil {
			return nil, errors.New("fixture retained terminal operator or cut is missing or reordered")
		}
		if transition.Identity != input.Stats.AttemptCut.Identity || transition.Identity != next.Stats.AttemptCut.Identity || transition.FromBoundary != boundary || transition.ToEpoch != epoch+1 || transition.Identity.NoID != input.NoID || transition.Schema != finalAttemptSettlementSchema || len(transition.Signature) != ed25519.SignatureSize {
			return nil, errors.New("fixture retained terminal transaction belongs to a different cut")
		}
		preFold := input.Stats
		preFold.SettlementTransition = nil
		cut := *input.Stats.AttemptCut
		cut.Boundary = boundary
		cut.Signature = transition.PreFold.AttemptCut.Signature
		preFold.AttemptCut = &cut
		if !finalJSONEqual(preFold, transition.PreFold) {
			return nil, errors.New("fixture retained terminal transaction changed the measured statistics or signed records")
		}
		transitions[index] = transition
	}
	return transitions, nil
}

// A cached successor cannot substitute a different owner, epoch, operator,
// measured statistic or signed record, and malformed retention never falls
// back to generating replacement evidence.
func TestFinalSemanticFixtureRetainedTransitionsRejectChangedSource(t *testing.T) {
	first, successor, _ := finalSemanticFixtureClosureReusePair(t)
	encoded, err := json.Marshal(successor)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"epoch", "validator", "genesis", "netuid", "uid", "deployment", "coordinator", "vault", "missing-operator", "reordered-operator", "missing-transition", "missing-cut", "transition-identity", "terminal-boundary", "next-epoch", "statistics", "record-root", "record-signature"} {
		var changed validatorpkg.ReleaseMeasurementArtifact
		if err := json.Unmarshal(encoded, &changed); err != nil {
			t.Fatal(err)
		}
		switch invalid {
		case "epoch":
			changed.SettlementEpoch++
		case "validator":
			changed.ValidatorID++
		case "genesis":
			changed.GenesisHash = finalTestHex(99)
		case "netuid":
			changed.Netuid++
		case "uid":
			changed.SelfUID++
		case "deployment":
			changed.DeploymentID += "-foreign"
		case "coordinator":
			changed.Coordinator = finalTestHex(99)
		case "vault":
			changed.SettlementVault = finalTestHex(99)
		case "missing-operator":
			changed.Inputs = changed.Inputs[:1]
		case "reordered-operator":
			changed.Inputs[0], changed.Inputs[1] = changed.Inputs[1], changed.Inputs[0]
		case "missing-transition":
			changed.Inputs[0].Stats.SettlementTransition = nil
		case "missing-cut":
			changed.Inputs[0].Stats.SettlementTransition.PreFold.AttemptCut = nil
		case "transition-identity":
			changed.Inputs[0].Stats.SettlementTransition.Identity.NoID++
		case "terminal-boundary":
			changed.Inputs[0].Stats.SettlementTransition.FromBoundary.EVMBlock++
		case "next-epoch":
			changed.Inputs[0].Stats.SettlementTransition.ToEpoch++
		case "statistics":
			changed.Inputs[0].Stats.SettlementTransition.PreFold.Providers[0].Assignments++
		case "record-root":
			changed.Inputs[0].Stats.SettlementTransition.PreFold.AttemptCut.Root = finalTestHex(99)
		case "record-signature":
			changed.Inputs[0].Stats.SettlementTransition.PreFold.AttemptCut.Records[0].Signature[0] ^= 1
		}
		if _, err := finalSemanticFixtureRetainedTransitions(first, &changed); err == nil {
			t.Fatalf("%s retained transaction was accepted", invalid)
		}
	}
	transitions, err := finalSemanticFixtureRetainedTransitions(first, successor)
	if err != nil || len(transitions) != 2 || transitions[0] != successor.Inputs[0].Stats.SettlementTransition || transitions[1] != successor.Inputs[1].Stats.SettlementTransition {
		t.Fatalf("exact retained transaction was not reused: %v", err)
	}
}
