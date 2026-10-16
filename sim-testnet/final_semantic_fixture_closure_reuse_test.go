package main

// The fixture producer must export an existing signed terminal transaction
// without requiring another signing key or hiding invalid public proof bytes.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"testing"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Uses the real signed fixture producer at the actual policy depth. Epoch 15
// carries epoch 14's transaction but is outside the accepted proof interval.
func finalSemanticFixtureClosureReusePair(t *testing.T) (*validatorpkg.ReleaseMeasurementArtifact, *validatorpkg.ReleaseMeasurementArtifact, map[uint64]map[byte]ed25519.PublicKey) {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x41}, ed25519.SeedSize))
	serverKeys := []ed25519.PrivateKey{
		ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x51}, ed25519.SeedSize)),
		ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x52}, ed25519.SeedSize)),
	}
	create := func(epoch uint64) *validatorpkg.ReleaseMeasurementArtifact {
		measurement := &validatorpkg.ReleaseMeasurementArtifact{DeploymentID: "fixture-reuse", ChainID: 945, GenesisHash: finalTestHex(5), Netuid: 521, ValidatorID: 1, SelfUID: 12, SettlementEpoch: epoch}
		for noID := uint64(1); noID <= 2; noID++ {
			input := validatorpkg.ReleaseMeasurementInput{
				NoID: noID, SettlementEpoch: epoch, CutEVMSnapshotBlock: 105 + (epoch-10)*finalReleaseEpochBlocks, CutEVMSnapshotHash: finalTestHex(byte(epoch)),
				Stats: validatorpkg.ReleaseStatsMeasurement{Config: validatorpkg.ReleaseStatsConfig{AMin: 8, AlphaNumerator: 1, AlphaDenominator: 10, LatRefMillis: 4000}},
			}
			for index := uint64(1); index <= uint64(finalSemanticFixtureMaximumAttemptM); index++ {
				clientID := finalAttemptFixtureID(noID*100 + index).String()
				input.Stats.Providers = append(input.Stats.Providers, validatorpkg.ReleaseProviderMeasurement{ClientID: clientID, LatencyBuckets: make([]uint64, 31), HasPriorQuality: true, PriorQualityPPM: 500_000, EgressIPHashHexes: []string{finalTestHex(byte(noID*100 + index))}})
				measurement.Bindings = append(measurement.Bindings, validatorpkg.ReleaseBindingMeasurement{NoID: noID, ClientID: clientID})
			}
			measurement.Inputs = append(measurement.Inputs, input)
		}
		return measurement
	}
	first, successor := create(14), create(15)
	ledgers := map[uint64]*finalAttemptFixtureLedger{}
	attachFinalAttemptCuts(t, first, key, serverKeys, ledgers, nil)
	attachFinalAttemptCuts(t, successor, key, serverKeys, ledgers, first)
	return first, successor, map[uint64]map[byte]ed25519.PublicKey{1: {1: serverKeys[0].Public().(ed25519.PublicKey)}, 2: {1: serverKeys[1].Public().(ed25519.PublicKey)}}
}

// Exercises the actual exporter with no signing key available. The old path
// always indexes the key and reconstructs the full signed transaction again.
func TestFinalSemanticFixtureClosedProofsReuseExactSuccessorWithoutResigning(t *testing.T) {
	first, successor, serverKeys := finalSemanticFixtureClosureReusePair(t)
	firstBytes, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	successorBytes, err := json.Marshal(successor)
	if err != nil {
		t.Fatal(err)
	}
	artifacts := map[string][]byte{"measurement-14.json": firstBytes, "measurement-15.json": successorBytes}
	validators := []FinalValidatorIdentityEvidence{{ValidatorID: 1, Cycles: []FinalCRv4Cycle{
		{SettlementEpoch: 14, MeasurementArtifact: FinalArtifactLocator{URI: "measurement-14.json"}},
		{SettlementEpoch: 15, MeasurementArtifact: FinalArtifactLocator{URI: "measurement-15.json"}},
	}}}
	proofs := finalSemanticFixtureClosedProofs(t, validators, nil, finalSemanticFixtureMaximumAttemptM, artifacts, func(kind, name string, data []byte) FinalArtifactLocator {
		artifacts[name] = append([]byte(nil), data...)
		return FinalArtifactLocator{Kind: kind, URI: name, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
	})
	if len(proofs) != 2 || proofs[0].ProofCount == 0 || proofs[1].ProofCount == 0 || len(proofs[0].SettlementClosures) != 1 || len(proofs[1].SettlementClosures) != 1 {
		t.Fatalf("retained transaction lost an operator, proof or epoch: %+v", proofs)
	}
	if !bytes.Equal(artifacts["measurement-14.json"], firstBytes) || !bytes.Equal(artifacts["measurement-15.json"], successorBytes) {
		t.Fatal("export changed a retained measurement")
	}
	closureBytes := artifacts[proofs[0].SettlementClosures[0].Artifact.URI]
	closure, err := validatorpkg.DecodeAttemptSettlementClosureWithServerKeys(closureBytes, serverKeys)
	if err != nil {
		t.Fatalf("real server and validator signature verification: %v", err)
	}
	if closure.Epoch != 14 || len(closure.Transitions) != len(successor.Inputs) {
		t.Fatal("closure changed the exact retained transaction census")
	}
	for index, transition := range closure.Transitions {
		if !finalJSONEqual(transition, successor.Inputs[index].Stats.SettlementTransition) {
			t.Fatalf("operator %d exported a reconstructed or changed transaction", transition.Identity.NoID)
		}
	}
	// A changed signature is still rejected by the actual public verifier; the
	// construction optimization is not a verification cache or acceptance bypass.
	closure.Transitions[0].Signature[0] ^= 1
	badBytes, err := json.Marshal(closure)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validatorpkg.DecodeAttemptSettlementClosureWithServerKeys(append(badBytes, '\n'), serverKeys); err == nil {
		t.Fatal("public verification accepted a corrupted retained signature")
	}
}
