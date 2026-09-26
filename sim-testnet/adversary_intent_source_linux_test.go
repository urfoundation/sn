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
	"reflect"
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
	legacyStateDir := filepath.Join(f.stateDir, "runtime", "validator-2", "state")
	if _, err := os.Stat(filepath.Join(legacyStateDir, "steering-intents.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fixture unexpectedly has a legacy intent", err)
	}
	if err := os.RemoveAll(legacyStateDir); err != nil {
		t.Fatal(err)
	}
	return g, handoff, probe, operators, intent
}

// Exercise the production factory with synthetic provider identities; no
// actor other than the local consensus model is run by these tests.
func consensusGenerationActor(t *testing.T, g *policyRolloverGenerationTestV2, probe *liveScenarioProbe) adversaryActor {
	t.Helper()
	roles := cloneRoleSecrets(g.roles)
	for miner := 1; miner <= probe.cfg.Config.Topology.Miners; miner++ {
		label := fmt.Sprintf("miner-%d", miner)
		role := roles.Clients[label]
		role.ClientIDHex = fmt.Sprintf("%032x", miner)
		roles.Clients[label] = role
	}
	actors, err := newLiveAdversaryActors(probe.cfg, g.fixture.stateDir, roles, g.fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range actors {
		if actor.ID() == "consensus-cabal-emulation" {
			return actor
		}
	}
	t.Fatal("consensus actor missing")
	return nil
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
	actor := consensusGenerationActor(t, g, probe)
	before := validatorNamespaceTreeSnapshot(t, g.fixture.stateDir)
	for _, phase := range []adversarySamplePhase{adversaryControlPhase, adversaryAttackPhase} {
		result := actor.Sample(t.Context(), phase, 1)
		if result.Outcome != adversaryOutcomeSuccess || result.Metrics["finalized_intents"] != 0 || result.Metrics["last_applied_epoch"] != intent.SubnetEpoch {
			t.Fatalf("actor lost the selected signed generation or promoted local receipts: %+v", result)
		}
	}
	if after := validatorNamespaceTreeSnapshot(t, g.fixture.stateDir); !reflect.DeepEqual(before, after) {
		t.Fatal("consensus observation changed the selected source or recreated legacy state")
	}
	if observations[0].LocalRuntimeIntents == nil || observations[0].LocalRuntimeIntents.State != "absent" || observations[0].AppliedIntents != 0 {
		t.Fatal("independent emulation invented the missing affiliated validator intent")
	}
}

// Missing and forged sources cannot seed an emulation, even after a prior
// successful sample. Original byte ownership remains with the selected source.
func TestConsensusAdversaryRejectsMissingAndForgedGenerationSources(t *testing.T) {
	g, handoff, probe, _, intent := consensusGenerationIntentFixture(t)
	actor := consensusGenerationActor(t, g, probe)
	if result := actor.Sample(t.Context(), adversaryAttackPhase, 1); result.Outcome != adversaryOutcomeSuccess {
		t.Fatalf("valid source baseline failed: %+v", result)
	}
	root := handoff.Validators[1].StateDir
	for _, fault := range []struct {
		name   string
		path   string
		remove bool
		detail string
	}{
		{name: "missing intent", path: filepath.Join(root, "steering-intents.json"), remove: true, detail: "no applied intent"},
		{name: "forged intent", path: filepath.Join(root, "steering-intents.json"), detail: "independent validator intent source:"},
		{name: "forged measurement", path: filepath.Join(root, intent.MeasurementArtifactPath), detail: "local measurement content differs"},
		{name: "forged envelope", path: filepath.Join(root, intent.MeasurementEnvelopePath), detail: "local measurement content differs"},
		{name: "forged generation", path: handoff.Validators[1].Evidence.Operators[0].Context.Path, detail: "independent validator intent source:"},
	} {
		original, err := os.ReadFile(fault.path)
		if err != nil {
			t.Fatal(err)
		}
		if fault.remove {
			err = os.Remove(fault.path)
		} else {
			err = os.WriteFile(fault.path, []byte("{}\n"), 0o600)
		}
		if err != nil {
			t.Fatal(err)
		}
		before := validatorNamespaceTreeSnapshot(t, g.fixture.stateDir)
		result := actor.Sample(t.Context(), adversaryAttackPhase, 2)
		if result.Outcome != adversaryOutcomeSkipped || len(result.Metrics) != 0 || !strings.Contains(result.Detail, fault.detail) {
			t.Fatalf("%s seeded emulation or lost the exact source failure: %+v", fault.name, result)
		}
		if after := validatorNamespaceTreeSnapshot(t, g.fixture.stateDir); !reflect.DeepEqual(before, after) {
			t.Fatalf("%s observation modified original state", fault.name)
		}
		if err := os.WriteFile(fault.path, original, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// A transport derivative cannot replace the canonical approval used to select
// the signed generation, and an unapproved route is rejected at construction.
func TestConsensusAdversaryRetainsOriginalSourceAuthority(t *testing.T) {
	g, _, probe, _, _ := consensusGenerationIntentFixture(t)
	actor := consensusGenerationActor(t, g, probe).(*consensusAdversary)
	actor.intentProbe.authorizedCfg = probe.cfg
	result := actor.Sample(t.Context(), adversaryAttackPhase, 1)
	if result.Outcome != adversaryOutcomeSkipped || len(result.Metrics) != 0 || !strings.Contains(result.Detail, "resolved_inputs_hash differs") {
		t.Fatalf("transport config replaced the approved source: %+v", result)
	}
	changed := *probe.cfg
	changed.OperationalEVM = "https://unapproved-rpc.example"
	if _, err := newLiveAdversaryActors(&changed, g.fixture.stateDir, g.roles, g.fixture.cfg); err == nil || !strings.Contains(err.Error(), "adversarial source transport") {
		t.Fatalf("unapproved transport reached actor construction: %v", err)
	}
}
