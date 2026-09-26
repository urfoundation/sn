//go:build linux

// The consensus actor must use the same signed generation as the scenario
// observer. These local sources never acquire strict native receipt authority.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
)

// Build an applied local intent in the real test-owned validator generation,
// including the signed measurement and the prepared lifecycle bytes.
func consensusGenerationIntentFixture(t *testing.T) (*policyRolloverGenerationTestV2, *policyRolloverHandoffV2, *liveScenarioProbe, []OperatorObservation, validatorpkg.SteeringIntent) {
	t.Helper()
	g, handoff, probe, operators := scenarioValidatorAuthorityFixture(t)
	f := g.fixture
	selected := handoff.Validators[1]
	source := scenarioNativeSourceV2TestFixture()
	intent, artifact := source.References[0].Intent, source.References[0].Artifact
	intent.Schema, intent.ValidatorID, intent.Netuid, intent.PolicyHash = validatorpkg.SteeringIntentSchema, 2, f.cfg.Netuid, f.cfg.PolicyHash
	intent.MaskedUIDs = []uint16{intent.SelfUID}
	intent.CreatedAt, intent.UpdatedAt = "2025-01-02T03:04:05Z", "2025-01-02T03:05:05Z"
	pair, _, err := runtimeEvidenceActivationKeysV2(g.roles, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	intent.Prepared = finalTestPreparedSubmission(t, intent.UIDs, intent.Values, FinalCRv4Cycle{
		SubnetEpoch: intent.SubnetEpoch, NativeSnapshot: ChainHead{Number: intent.NativeSnapshotBlock, Hash: intent.NativeSnapshotHash},
		Reveal: FinalNativeReceipt{Block: ChainHead{Number: intent.RevealBlock}},
	}, pair.PublicKey())
	intent.Prepared.Netuid = intent.Netuid
	intent.ExtrinsicHash = intent.Prepared.ExtrinsicHash
	artifact.Schema, artifact.DeploymentID, artifact.ChainID, artifact.GenesisHash = validatorpkg.ReleaseMeasurementSchemaV2, f.cfg.Config.Deployment.DeploymentID, f.cfg.ChainID, f.cfg.Public.Chain.GenesisHash
	artifact.Coordinator, artifact.SettlementVault = "0x"+strings.Repeat("11", 20), "0x"+strings.Repeat("22", 20)
	artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash, artifact.EVMSnapshotBlock, artifact.EVMSnapshotHash = intent.NativeSnapshotBlock, intent.NativeSnapshotHash, intent.EVMSnapshotBlock, intent.EVMSnapshotHash
	artifact.ValidatorID, artifact.Netuid, artifact.PolicyHash = intent.ValidatorID, intent.Netuid, intent.PolicyHash
	artifact.SubnetEpoch, artifact.SettlementEpoch, artifact.SelfUID = intent.SubnetEpoch, intent.SettlementEpoch, intent.SelfUID
	writeContent := func(value any, directory string) (string, string, uint64) {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		encoded = append(encoded, '\n')
		hash := bytesSHA256(encoded)
		path := directory + "/" + strings.TrimPrefix(hash, "sha256:") + ".json"
		if err := os.MkdirAll(filepath.Join(selected.StateDir, directory), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(selected.StateDir, path), encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		return path, hash, uint64(len(encoded))
	}
	intent.MeasurementArtifactPath, intent.MeasurementArtifactHash, intent.MeasurementArtifactSize = writeContent(artifact, "measurements")
	envelope := validatorpkg.ReleaseMeasurementEnvelope{
		Schema: validatorpkg.ReleaseMeasurementEnvelopeSchemaV2, MeasurementSchema: artifact.Schema,
		DeploymentID: artifact.DeploymentID, ChainID: artifact.ChainID, GenesisHash: artifact.GenesisHash,
		Coordinator: artifact.Coordinator, SettlementVault: artifact.SettlementVault, ValidatorID: artifact.ValidatorID,
		ValidatorHotkey: intent.Prepared.HotkeyHex, ValidatorUID: intent.SelfUID, Netuid: artifact.Netuid,
		SubnetEpoch: artifact.SubnetEpoch, SettlementEpoch: artifact.SettlementEpoch, PolicyHash: artifact.PolicyHash,
		NativeSnapshotBlock: artifact.NativeSnapshotBlock, NativeSnapshotHash: artifact.NativeSnapshotHash,
		EVMSnapshotBlock: artifact.EVMSnapshotBlock, EVMSnapshotHash: artifact.EVMSnapshotHash,
		MeasurementArtifactHash: intent.MeasurementArtifactHash, MeasurementArtifactSize: intent.MeasurementArtifactSize,
		PreparedExtrinsicHash: intent.Prepared.ExtrinsicHash, SignedAt: intent.CreatedAt, SignatureScheme: "sr25519",
	}
	unsigned, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(append([]byte(validatorpkg.ReleaseMeasurementEnvelopeSigningDomainV2+"\x00"), unsigned...))
	signature, err := pair.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	envelope.SigningHash, envelope.Signature = "sha256:"+hex.EncodeToString(digest[:]), "0x"+hex.EncodeToString(signature)
	intent.MeasurementEnvelopePath, intent.MeasurementEnvelopeHash, intent.MeasurementEnvelopeSize = writeContent(envelope, "measurements/envelopes")
	intent.VectorHash, err = intent.ReconstructedVectorHash()
	if err != nil {
		t.Fatal(err)
	}
	store := struct {
		Schema  string                        `json:"schema"`
		Current *validatorpkg.SteeringIntent  `json:"current,omitempty"`
		History []validatorpkg.SteeringIntent `json:"history"`
	}{Schema: validatorpkg.SteeringIntentSchema, Current: &intent, History: []validatorpkg.SteeringIntent{}}
	encoded, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(selected.StateDir, "steering-intents.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.stateDir, "runtime", "validator-2", "state", "steering-intents.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fixture unexpectedly has a legacy intent", err)
	}
	return g, handoff, probe, operators, intent
}

// The existing source-aware probe must see a real signed local vector before
// the consensus actor is allowed to exercise the same bounded emulation.
func TestConsensusAdversaryReadsAppliedGenerationWithoutLegacyIntent(t *testing.T) {
	g, _, probe, operators, intent := consensusGenerationIntentFixture(t)
	observations, err := probe.inspectValidators(t.Context(), operators)
	if err != nil || len(observations) != 2 {
		t.Fatal("source-aware probe failed", err)
	}
	independent := observations[1]
	if independent.Error != "" || independent.VectorHash != intent.VectorHash || len(independent.AppliedWeights) != 2 || independent.LocalRuntimeIntents == nil || independent.LocalRuntimeIntents.State != "observed" || independent.FinalizedIntents != 0 || independent.AppliedIntents != 0 {
		t.Fatalf("signed local source was not observed without promotion: %+v", independent)
	}
	roles := cloneRoleSecrets(g.roles)
	for miner := 1; miner <= probe.cfg.Config.Topology.Miners; miner++ {
		label := fmt.Sprintf("miner-%d", miner)
		role := roles.Clients[label]
		role.ClientIDHex = fmt.Sprintf("%032x", miner)
		roles.Clients[label] = role
	}
	actors, err := newLiveAdversaryActors(probe.cfg, g.fixture.stateDir, roles)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range actors {
		if actor.ID() == "consensus-cabal-emulation" {
			result := actor.Sample(t.Context(), adversaryAttackPhase, 1)
			if result.Outcome != adversaryOutcomeSuccess || result.Metrics["finalized_intents"] != 0 {
				t.Fatalf("actor lost the selected signed generation or promoted local receipts: %+v", result)
			}
			return
		}
	}
	t.Fatal("consensus actor missing")
}
