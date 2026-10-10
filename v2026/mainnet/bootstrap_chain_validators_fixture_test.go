// These complete synthetic configs use the public producer approval format.
// Only public config and approval files exist; credentials and operator evidence
// deliberately do not, so offline parsing cannot accidentally start a producer.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
	"gopkg.in/yaml.v3"
)

// Each role's independent synthetic approver signs its entire normalized config.
type bootstrapChainValidatorFixture struct {
	path     string
	config   validator.ReleaseConfig
	approval validator.OwnerRecycleApproval
	private  ed25519.PrivateKey
}

// Parse visibly synthetic account IDs without deriving or opening a hotkey seed.
func bootstrapChainTestAccount(t *testing.T, value string) [32]byte {
	t.Helper()
	raw, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil || len(raw) != 32 {
		t.Fatal("invalid synthetic account", err)
	}
	return [32]byte(raw)
}

// The fixture retains the existing public policy grammar and explicit evidence
// bounds, with independent absent custody namespaces for both validator roles.
func newBootstrapChainValidatorFixture(t *testing.T, chain *bootstrapChainFixture, scope subnetCensusPolicy, index int) *bootstrapChainValidatorFixture {
	t.Helper()
	policy, err := protocol.LoadPolicy(filepath.Join("..", "deploy", "testnet", "policy-v1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	policy.NetworkProfile, policy.ProductionCadence.EpochBlocks = "mainnet", 50_400
	policyHash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(chain.path)
	root := filepath.Join(directory, fmt.Sprintf("validator-%d", index+1))
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(0x70 + index)}, ed25519.SeedSize))
	cfg := validator.ReleaseConfig{SchemaVersion: 3, Production: true, Release: "1.0", DeploymentID: chain.config.DeploymentId, ValidatorID: uint64(index + 1),
		ChainID: scope.EvmChainId, GenesisHash: scope.GenesisHash, RuntimeSpec: scope.RuntimeVersion.SpecVersion, TransactionVersion: scope.RuntimeVersion.TransactionVersion,
		StateVersion: scope.RuntimeVersion.StateVersion, RuntimeCodeHash: scope.RuntimeCodeHash, RuntimeMetadataHash: scope.RuntimeMetadataHash, Netuid: 25,
		Coordinator: "0x" + strings.Repeat("12", 20), SettlementVault: "0x" + strings.Repeat("34", 20), DeployBlock: 10,
		PolicyHash: fmt.Sprintf("0x%x", policyHash), Policy: *policy, RPC: []string{"https://evm.example"}, Substrate: []string{"wss://native.example"},
		StateDir: filepath.Join(root, "state"), HotkeySeedFile: filepath.Join(root, "hotkey.seed"), TrailDepth: policy.Verify.TrailDepth, PollSeconds: 2,
		OwnerRecycleApproval: &validator.ReleaseOwnerRecycleApprovalConfig{Approval: validator.ReleaseEvidenceV2File{Path: filepath.Join(directory, fmt.Sprintf("validator-%d-approval.json", index+1))}, Signer: "0x" + hex.EncodeToString(private.Public().(ed25519.PublicKey))},
	}
	for i := 0; i < 2; i++ {
		state := filepath.Join(root, fmt.Sprintf("operator-%d", i+1))
		cfg.Operators = append(cfg.Operators, validator.OperatorConfig{NoID: uint64(i + 1), APIURL: fmt.Sprintf("https://operator-%d.example", i+1), ConnectURL: fmt.Sprintf("wss://operator-%d.example", i+1),
			ArtifactSigner: "0x" + strings.Repeat(fmt.Sprintf("%02x", 0x50+i), 20), StateDir: state, NetworkJWTFile: filepath.Join(state, "network.jwt"), ClientJWTFile: filepath.Join(state, "client.jwt"), ClientKeySeedFile: filepath.Join(state, "client.key"), Concurrency: 2})
	}
	stream := validator.AttemptStreamV2Bounds{MaxDataBytes: 4 * 1024 * 1024, MaxItems: 128, MaxChunkBytes: 128 * 1024, MaxChunks: 128, MaxPages: 8, MaxPageBytes: 4096, MaxDescriptorsPerPage: 16, MaxManifestBytes: 1024}
	cfg.EvidenceV2 = validator.ReleaseEvidenceV2Config{Schema: validator.ReleaseEvidenceV2ConfigSchema, Bounds: validator.ReleaseEvidenceV2Bounds{
		Disk:         validator.AttemptLedgerDiskLimits{MaxRecordBytes: 64 * 1024, MaxRecordCount: 128, MaxTrailCount: 16, MaxRawRecordBytes: 4 * 1024 * 1024, MaxStorageBytes: 16 * 1024 * 1024, MaxStorageFiles: 256, MaxLegacyBytes: 4 * 1024 * 1024, MaxProofBytes: 1024 * 1024},
		Cut:          validator.AttemptCutV2Bounds{MaxHeaderBytes: max(stream.MaxManifestBytes, stream.MaxPageBytes), Records: stream, Proofs: stream},
		Replay:       validator.AttemptCutV2ReplayBounds{MaxRecordBytes: 128 * 1024, MaxProofBytes: 128 * 1024, MaxTrails: 16, MaxScratchBytes: 16 * 1024 * 1024, MaxScratchFiles: 256},
		Persistence:  validator.AttemptSettlementRuntimeV2PersistenceBounds{MaxSnapshotBytes: 2 * 1024 * 1024, MaxJournalBytes: 16 * 1024 * 1024},
		HeadEMA:      validator.HeadEMAStoreV2Limits{MaxFileBytes: 2 * 1024 * 1024, MaxEntries: 64, MaxControlBytes: 8 * 1024 * 1024},
		MaxProviders: 128, MaxEgressHashes: 128, MaxFleetPrefixes: 128, MaxOperators: 2, MaxHeadEntries: 64, MaxArtifactBytes: 64 * 1024 * 1024, MaxControlBytes: 64 * 1024 * 1024,
		MaxInputJournalBytes: 4 * 1024 * 1024, MaxParticipants: 2, MaxTransitionBytes: 1024 * 1024, MaxClosureBytes: 4 * 1024 * 1024, MaxHistoryBytes: 1024 * 1024,
	}}
	for _, operator := range cfg.Operators {
		input := filepath.Join(root, "evidence", fmt.Sprintf("operator-%d", operator.NoID))
		ref := func(name string, size uint64) validator.ReleaseEvidenceV2File {
			return validator.ReleaseEvidenceV2File{Path: filepath.Join(input, name), Bytes: size, SHA256: "0x" + strings.Repeat("ab", 32)}
		}
		cfg.EvidenceV2.Operators = append(cfg.EvidenceV2.Operators, validator.ReleaseEvidenceV2OperatorConfig{NoID: operator.NoID,
			Activation: ref("activation.payload", uint64(protocol.ValidatorEvidenceActivationPayloadSize)), VPKSignature: ref("vpk.signature", 64), HotkeySignature: ref("hotkey.signature", 64), Context: ref("context.json", 32), History: ref("history.json", 32),
			ReplayScratchRoot: filepath.Join(root, "scratch", fmt.Sprintf("operator-%d", operator.NoID), "replay"), SealScratchRoot: filepath.Join(root, "scratch", fmt.Sprintf("operator-%d", operator.NoID), "seal"),
		})
	}
	return &bootstrapChainValidatorFixture{path: filepath.Join(directory, fmt.Sprintf("validator-%d.yml", index+1)), config: cfg, private: private,
		approval: validator.OwnerRecycleApproval{Schema: "urnetwork-owner-recycle-approval-v2", NativeChain: scope.NativeChain,
			Proposal: validator.OwnerRecycleProposal{Schema: "urnetwork-owner-recycle-proposal-v1", ParentPolicyHash: policyHash, PolicyId: policy.PolicyID + 1, EffectiveEpoch: policy.EffectiveEpoch + 1,
				ProviderShare: protocol.Rational{Numerator: 1, Denominator: 10}, Remainder: "recognized_owner_recycle", OwnerAllocation: "equal_unmasked_registered",
				Runtime: validator.OwnerRecycleRuntimePin{GenesisHash: bootstrapChainTestAccount(t, scope.GenesisHash), Netuid: 25, Version: scope.RuntimeVersion,
					CodeHash: bootstrapChainTestAccount(t, scope.RuntimeCodeHash), MetadataHash: bootstrapChainTestAccount(t, scope.RuntimeMetadataHash), SourceCommit: scope.RuntimeSourceCommit}},
			RuntimeReviewHash: [32]byte{0x91}, ValidatorHotkey: bootstrapChainTestAccount(t, scope.Preserve[index].Hotkey), SubnetOwner: bootstrapChainTestAccount(t, scope.SubnetOwnerColdkey),
			OwnerHotkeys:     [][32]byte{bootstrapChainTestAccount(t, "0x"+strings.Repeat("41", 32)), bootstrapChainTestAccount(t, "0x"+strings.Repeat("42", 32))},
			FirstNativeEpoch: 20, ValidFromNativeBlock: 101, ValidThroughNativeBlock: 200, MaximumSubnetUids: 6, MaximumOwnedHotkeys: 16,
			Production: &validator.OwnerRecycleProductionApproval{Schema: "urnetwork-owner-recycle-production-v1", RuntimeCapability: crv4.ValidatorProducerRuntimeProfile,
				ValidatorHotkeys: [][32]byte{bootstrapChainTestAccount(t, scope.Preserve[0].Hotkey), bootstrapChainTestAccount(t, scope.Preserve[1].Hotkey)}, MaximumLastUpdateAge: 100, ValidThroughNativeEpoch: 30, ActivationNativeHash: [32]byte{0x92}},
		},
	}
}

// Fixtures sign only their generated test-only approval keys, never chain keys.
func (self *bootstrapChainValidatorFixture) publish(t *testing.T) planFileReference {
	t.Helper()
	// The strict loader sees YAML's concrete empty slices, not Go nil defaults.
	encoded, err := yaml.Marshal(self.config)
	if err != nil {
		t.Fatal(err)
	}
	var resolved validator.ReleaseConfig
	if err := yaml.Unmarshal(encoded, &resolved); err != nil {
		t.Fatal(err)
	}
	self.config = resolved
	self.approval.ConfigHash, err = validator.OwnerRecycleConfigHash(&self.config)
	if err != nil {
		t.Fatal(err)
	}
	message, err := self.approval.SigningMessage()
	if err != nil {
		t.Fatal(err)
	}
	envelope := validator.OwnerRecycleApprovalEnvelope{Approval: self.approval, Signature: hex.EncodeToString(ed25519.Sign(self.private, message))}
	self.writeApproval(t, envelope)
	return self.writeConfig(t)
}

// Replacing the exact envelope also updates its config reference, allowing a
// negative test to reach signature checks instead of stopping at a byte hash.
func (self *bootstrapChainValidatorFixture) writeApproval(t *testing.T, envelope validator.OwnerRecycleApprovalEnvelope) {
	t.Helper()
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	selection := self.config.OwnerRecycleApproval
	if self.config.TreasuryApproval != nil {
		selection = self.config.TreasuryApproval
	}
	path := selection.Approval.Path
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	selection.Approval = validator.ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(raw)), SHA256: fmt.Sprintf("0x%x", digest)}
}

// Marshal the already normalized fixture so signature identity stays exact.
func (self *bootstrapChainValidatorFixture) writeConfig(t *testing.T) planFileReference {
	t.Helper()
	raw, err := yaml.Marshal(self.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(self.path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return planFileReference{Path: self.path, Sha256: fmt.Sprintf("sha256:%x", digest)}
}
