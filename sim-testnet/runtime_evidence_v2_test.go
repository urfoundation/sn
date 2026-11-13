package main

// Fixtures pin independent bytes without asserting historical chain authority.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/protocol"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Explicit small fixture policy, never a fallback for the live testnet file.
func configureRuntimeEvidenceV2Test(t *testing.T, cfg *ResolvedConfig, stateDir string) {
	t.Helper()
	cfg.Config.ValidatorEvidenceRelay = evidenceRelayConfig{MaxSlots: 128, GasUnits: 1_000_000}
	stream := validatorpkg.AttemptStreamV2Bounds{MaxDataBytes: 4 * 1024 * 1024, MaxItems: 128, MaxChunkBytes: 128 * 1024, MaxChunks: 128, MaxPages: 8, MaxPageBytes: 4096, MaxDescriptorsPerPage: 16, MaxManifestBytes: 1024}
	bounds := validatorpkg.ReleaseEvidenceV2Bounds{
		Disk:         validatorpkg.AttemptLedgerDiskLimits{MaxRecordBytes: 64 * 1024, MaxRecordCount: 128, MaxTrailCount: 16, MaxRawRecordBytes: 4 * 1024 * 1024, MaxStorageBytes: 16 * 1024 * 1024, MaxStorageFiles: 256, MaxLegacyBytes: 4 * 1024 * 1024, MaxProofBytes: 1024 * 1024},
		Cut:          validatorpkg.AttemptCutV2Bounds{MaxHeaderBytes: max(stream.MaxManifestBytes, stream.MaxPageBytes), Records: stream, Proofs: stream},
		Replay:       validatorpkg.AttemptCutV2ReplayBounds{MaxRecordBytes: 128 * 1024, MaxProofBytes: 128 * 1024, MaxTrails: 16, MaxScratchBytes: 16 * 1024 * 1024, MaxScratchFiles: 256},
		Persistence:  validatorpkg.AttemptSettlementRuntimeV2PersistenceBounds{MaxSnapshotBytes: 2 * 1024 * 1024, MaxJournalBytes: 16 * 1024 * 1024},
		HeadEMA:      validatorpkg.HeadEMAStoreV2Limits{MaxFileBytes: 2 * 1024 * 1024, MaxEntries: 64, MaxControlBytes: 8 * 1024 * 1024},
		MaxProviders: 128, MaxEgressHashes: 128, MaxFleetPrefixes: 128, MaxOperators: uint64(cfg.Config.Topology.Operators), MaxHeadEntries: 64, MaxArtifactBytes: 64 * 1024 * 1024, MaxControlBytes: 64 * 1024 * 1024,
		MaxInputJournalBytes: 4 * 1024 * 1024,
		MaxParticipants:      uint64(cfg.Config.Topology.Operators), MaxTransitionBytes: 1024 * 1024, MaxClosureBytes: 4 * 1024 * 1024, MaxHistoryBytes: 1024 * 1024,
	}
	cfg.Config.ValidatorEvidenceV2 = nil
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		evidence := validatorpkg.ReleaseEvidenceV2Config{Schema: validatorpkg.ReleaseEvidenceV2ConfigSchema, Bounds: bounds}
		for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
			paths, replay, seal := runtimeEvidenceV2Paths(stateDir, validatorID, uint64(noID))
			activation := protocol.ValidatorEvidenceActivation{Domain: protocol.ValidatorEvidenceActivationDomain{ChainID: 945, GenesisHash: [32]byte{1}, Netuid: 7, Coordinator: [20]byte{2}, SettlementVault: [20]byte{3}, DeploymentIDHash: sha256.Sum256([]byte(cfg.Config.Deployment.DeploymentID)), PolicyHash: [32]byte{4}}, Hotkey: [32]byte{byte(validatorID)}, NoID: uint64(noID), VPK: [32]byte{byte(validatorID), byte(noID)}, FirstSequence: 1, NativeBlock: 10, NativeHash: [32]byte{5}, EVMBlock: 20, EVMHash: [32]byte{6}}
			payload, err := activation.Payload()
			if err != nil {
				t.Fatal(err)
			}
			// Signature, context and history bytes make no authentication claim:
			// this fixture qualifies exact references, not their future readers.
			wires := [][]byte{payload, bytes.Repeat([]byte{byte(noID)}, 64), bytes.Repeat([]byte{byte(validatorID)}, 64), []byte(fmt.Sprintf("{\"validator\":%d,\"no_id\":%d,\"role\":\"context\"}\n", validatorID, noID)), []byte(fmt.Sprintf("{\"validator\":%d,\"no_id\":%d,\"role\":\"history\"}\n", validatorID, noID))}
			refs := make([]validatorpkg.ReleaseEvidenceV2File, len(paths))
			for index, path := range paths {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, wires[index], 0o600); err != nil {
					t.Fatal(err)
				}
				digest := sha256.Sum256(wires[index])
				refs[index] = validatorpkg.ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(wires[index])), SHA256: "0x" + hex.EncodeToString(digest[:])}
			}
			evidence.Operators = append(evidence.Operators, validatorpkg.ReleaseEvidenceV2OperatorConfig{NoID: uint64(noID), Activation: refs[0], VPKSignature: refs[1], HotkeySignature: refs[2], Context: refs[3], History: refs[4], ReplayScratchRoot: replay, SealScratchRoot: seal})
		}
		cfg.Config.ValidatorEvidenceV2 = append(cfg.Config.ValidatorEvidenceV2, validatorpkg.ReleaseValidatorEvidenceV2Config{ValidatorID: uint64(validatorID), Evidence: evidence})
	}
	var err error
	cfg.ConfigHash, err = releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
}

// The actual renderer must refuse even a later-pair failure before mutation.
func TestRuntimeEvidenceV2RenderAdmissionPrecedesEveryMutation(t *testing.T) {
	for _, mutate := range []func(*testing.T, *ResolvedConfig){
		func(t *testing.T, cfg *ResolvedConfig) { cfg.Config.ValidatorEvidenceV2 = nil },
		func(t *testing.T, cfg *ResolvedConfig) {
			cfg.Config.ValidatorEvidenceV2[1].Evidence.Bounds.Persistence.MaxJournalBytes = 0
		},
		func(t *testing.T, cfg *ResolvedConfig) {
			cfg.Config.ValidatorEvidenceV2[1].Evidence.Operators[1].NoID = 1
		},
		func(t *testing.T, cfg *ResolvedConfig) {
			cfg.Config.ValidatorEvidenceV2[1].Evidence.Operators[1].Activation.SHA256 = ""
		},
		func(t *testing.T, cfg *ResolvedConfig) {
			if err := os.Remove(cfg.Config.ValidatorEvidenceV2[1].Evidence.Operators[1].Context.Path); err != nil {
				t.Fatal(err)
			}
		},
		func(t *testing.T, cfg *ResolvedConfig) {
			cfg.Config.ValidatorEvidenceV2[1].Evidence.Operators[1].SealScratchRoot = cfg.Config.ValidatorEvidenceV2[0].Evidence.Operators[0].SealScratchRoot
		},
	} {
		cfg := testResolvedConfig(t)
		cfg.Public.Chain.EVMPublicReadEndpoint = "https://test.chain.opentensor.ai"
		stateDir := t.TempDir()
		configureRuntimeEvidenceV2Test(t, cfg, stateDir)
		deployment := ContractDeployment{Schema: "urnetwork-contract-deployment-v1", DeploymentID: cfg.Config.Deployment.DeploymentID, CoordinatorProxy: common.HexToAddress("0x4000000000000000000000000000000000000004"), SettlementVault: common.HexToAddress("0x2000000000000000000000000000000000000002"), DeployBlock: 123, DeployBlockHash: "0x" + strings.Repeat("ab", 32), CoordinatorEventStartBlock: 100, CoordinatorEventStartBlockHash: "0x" + strings.Repeat("cd", 32)}
		if err := saveContractDeployment(stateDir, deployment); err != nil {
			t.Fatal(err)
		}
		mutate(t, cfg)
		before := validatorNamespaceTreeSnapshot(t, stateDir)
		if err := RenderRuntimeConfigs(cfg, stateDir, nil); err == nil || !strings.Contains(err.Error(), "evidence") {
			t.Fatalf("renderer did not refuse incomplete evidence: %v", err)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
			t.Fatal("rendering mutated state before complete V2 admission")
		}
	}
}

func TestRuntimeEvidenceV2CompletePairsHaveIndependentOwners(t *testing.T) {
	cfg := testResolvedConfig(t)
	stateDir := t.TempDir()
	configureRuntimeEvidenceV2Test(t, cfg, stateDir)
	if err := preflightRuntimeEvidenceV2(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, validator := range cfg.Config.ValidatorEvidenceV2 {
		for _, operator := range validator.Evidence.Operators {
			paths := []string{operator.ReplayScratchRoot, operator.SealScratchRoot}
			for _, file := range operator.Files() {
				paths = append(paths, file.Path)
			}
			for _, path := range paths {
				if seen[path] {
					t.Fatalf("shared pair owner %s", path)
				}
				seen[path] = true
			}
		}
	}
	if len(seen) != cfg.Config.Topology.Validators*cfg.Config.Topology.Operators*7 {
		t.Fatal("incomplete pair resource census")
	}
	cfg.Config.ValidatorEvidenceV2[1].Evidence.Operators[0].Context.Path = cfg.Config.ValidatorEvidenceV2[0].Evidence.Operators[0].Context.Path
	if err := preflightRuntimeEvidenceV2(cfg, stateDir); err == nil {
		t.Fatal("healthy other-validator reference substituted")
	}
}

func TestRuntimeEvidenceV2ManifestBindsConfigurationIdentity(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixture(t)
	configureRuntimeEvidenceV2Test(t, cfg, stateDir)
	if err := writeRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	before, err := releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds.MaxHistoryBytes++
	after, err := releaseConfigHash(cfg.Config, cfg.Public, cfg.Hyperparameters)
	if err != nil || before == after {
		t.Fatal("source configuration identity omitted V2 bounds")
	}
	// Deliberately keep stale ConfigHash to prove the separate runtime identity.
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("stale V2 runtime identity accepted: %v", err)
	}
}

func TestRuntimeEvidenceV2ManifestCannotRehashAlteredReference(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixture(t)
	configureRuntimeEvidenceV2Test(t, cfg, stateDir)
	if err := writeRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	reference := cfg.Config.ValidatorEvidenceV2[0].Evidence.Operators[0].Context
	wire, err := os.ReadFile(reference.Path)
	if err != nil {
		t.Fatal(err)
	}
	wire[0] ^= 1
	if err := os.WriteFile(reference.Path, wire, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(wire)
	relative, err := filepath.Rel(stateDir, reference.Path)
	if err != nil {
		t.Fatal(err)
	}
	rewriteRuntimeConfigManifest(t, stateDir, func(manifest *RuntimeConfigManifest) {
		for index := range manifest.Files {
			if manifest.Files[index].Path == filepath.ToSlash(relative) {
				manifest.Files[index].SHA256 = "sha256:" + hex.EncodeToString(digest[:])
			}
		}
	})
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "configured identity") {
		t.Fatalf("rehashing authorized changed context bytes: %v", err)
	}
	if err := writeRuntimeConfigManifest(cfg, stateDir); err == nil {
		t.Fatal("manifest rebuild silently adopted changed context")
	}
}

func TestRuntimeEvidenceV2ManifestRetainsReferenceModeAndInventory(t *testing.T) {
	cfg, stateDir := runtimeConfigManifestFixture(t)
	configureRuntimeEvidenceV2Test(t, cfg, stateDir)
	if err := writeRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	reference := cfg.Config.ValidatorEvidenceV2[0].Evidence.Operators[0].History
	if err := os.Chmod(reference.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil {
		t.Fatal("reference mode weakened")
	}
	if err := os.Chmod(reference.Path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(reference.Path), "unconfigured.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "unexpected static") {
		t.Fatalf("unconfigured reference accepted: %v", err)
	}
}

func TestRuntimeEvidenceV2HarnessStrictRoutingAndLosslessBounds(t *testing.T) {
	cfg := testResolvedConfig(t)
	configureRuntimeEvidenceV2Test(t, cfg, t.TempDir())
	cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds.MaxHistoryBytes = 9007199254740993
	wire, err := yaml.Marshal(cfg.Config)
	if err != nil {
		t.Fatal(err)
	}
	var decoded HarnessConfig
	if err := yaml.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.ValidatorEvidenceV2, cfg.Config.ValidatorEvidenceV2) {
		t.Fatal("simulator lost integer or reference identity")
	}
	for _, value := range []string{"\"1\"", "1.0", "0x1", "18446744073709551616"} {
		mutated := strings.Replace(string(wire), "validator_id: 1", "validator_id: "+value, 1)
		if mutated == string(wire) {
			t.Fatal("routing mutation missed its field")
		}
		if err := yaml.Unmarshal([]byte(mutated), &decoded); err == nil {
			t.Errorf("non-canonical validator routing key %q accepted", value)
		}
	}
	index := strings.Index(string(wire), "\nvalidator_evidence_v2:")
	if index < 0 {
		t.Fatal("original-node fixture missed the simulator evidence field")
	}
	path := filepath.Join(t.TempDir(), "harness.yml")
	if err := os.WriteFile(path, []byte(string(wire[:index])+"\nvalidator_evidence_v2: null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := strictYAML(path, &decoded); err == nil || !strings.Contains(err.Error(), "requires a sequence") {
		t.Fatalf("harness file loader skipped original null admission: %v", err)
	}
}
