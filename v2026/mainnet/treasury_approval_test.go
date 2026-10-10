// Treasury approval drafting and assembly are checked against the validator's
// own production loader with synthetic keys. The fixed vector lets an external
// signer check its digest and Ed25519 signature byte for byte.
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
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
	"gopkg.in/yaml.v3"
)

// RFC 8032 section 7.1 TEST 1: a published test key that signs no deployment.
const treasuryApprovalVectorSeed = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
const treasuryApprovalVectorPublicKey = "0xd75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"

// The approval JSON an external signer hashes; its approval file holds these
// bytes and one line feed. The digest is SHA-256 of the 48 bytes
// "urnetwork-native-treasury-approval-signature-v1\n" followed by this JSON;
// the signature is the vector key's Ed25519 signature of the 32 digest bytes;
// the envelope is {"approval":JSON,"signature":"SIGNATURE"} and a line feed.
const treasuryApprovalVectorJSON = `{"schema":"urnetwork-native-treasury-approval-v1","config_hash":[193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193,193],"proposal":{"Schema":"urnetwork-native-treasury-proposal-v1","ParentPolicyHash":[178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178,178],"PolicyId":2,"EffectiveEpoch":1,"ProviderShare":{"numerator":1,"denominator":10},"Remainder":"ordinary_treasury_credit","OwnerAllocation":"equal_exact_registered","Runtime":{"GenesisHash":[163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163,163],"Netuid":25,"Version":{"specName":"node-subtensor","specVersion":473,"transactionVersion":1,"stateVersion":1},"CodeHash":[164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164,164],"MetadataHash":[165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165,165],"SourceCommit":"f87cada631f81d11683e715a9f059f693992e64a"},"treasury":{"schema":"urnetwork-native-treasury-receive-policy-v1","multisig_account":[119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119,119],"threshold":0,"signatories":null,"recipients":[{"uid":4,"hotkey":[69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69,69],"registration_block":44},{"uid":5,"hotkey":[70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70,70],"registration_block":45}],"provider_share":{"numerator":1,"denominator":10},"treasury_share":{"numerator":9,"denominator":10},"max_weight_limit_u16":32768}},"native_chain":"synthetic-test-vector-chain","runtime_review_hash":[145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145,145],"validator_hotkey":[90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90],"subnet_owner":[97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97,97],"owner_hotkeys":[[65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65,65]],"first_native_epoch":20,"valid_from_native_block":101,"valid_through_native_block":200,"maximum_subnet_uids":16,"maximum_owned_hotkeys":16,"production":{"schema":"urnetwork-native-treasury-production-v1","runtime_capability":"urnetwork-validator-producer-interface-v1","epoch_schedule_profile":"urnetwork-subtensor-tempo-drift-v1","validator_hotkeys":[[90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90,90]],"maximum_last_update_age":100,"valid_through_native_epoch":30,"activation_native_hash":[146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146,146]}}`
const treasuryApprovalVectorDigest = "0x00e72b3985fc9db6219f4250a3272c8e4ee498a6e846e15aacff6ede663a7f0e"
const treasuryApprovalVectorSignature = "c0c1879a679716f967f2eec0d9c8518f12bef762afbe5cfbf01a15335c0d200d75bb615b14c6bccbb1ae6eec10ff304d5b02e36fdb8b20a4af8818a4b6a74f02"
const treasuryApprovalVectorEnvelopeSha256 = "0xac5fe50b0ccee79830aeec26e142be505ea9b2e2f60e2b90ae742b91a45832c7"

// The vector's meaning; it must marshal to exactly treasuryApprovalVectorJSON.
func treasuryApprovalVectorBody() validator.TreasuryApproval {
	repeat := func(value byte) [32]byte { return [32]byte(bytes.Repeat([]byte{value}, 32)) }
	return validator.TreasuryApproval{Schema: validator.TreasuryApprovalSchema, ConfigHash: repeat(0xc1),
		Proposal: validator.TreasuryProposal{Schema: validator.TreasuryProposalSchema, ParentPolicyHash: repeat(0xb2), PolicyId: 2, EffectiveEpoch: 1,
			ProviderShare: protocol.Rational{Numerator: 1, Denominator: 10}, Remainder: "ordinary_treasury_credit", OwnerAllocation: "equal_exact_registered",
			Runtime: validator.OwnerRecycleRuntimePin{GenesisHash: repeat(0xa3), Netuid: 25, Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 473, TransactionVersion: 1, StateVersion: 1},
				CodeHash: repeat(0xa4), MetadataHash: repeat(0xa5), SourceCommit: crv4.NativeOwnerSource473},
			Treasury: &validator.TreasuryPolicy{Schema: validator.TreasuryReceivePolicySchema, MultisigAccount: repeat(0x77),
				Recipients:    []validator.TreasuryRecipient{{Uid: 4, Hotkey: repeat(0x45), RegistrationBlock: 44}, {Uid: 5, Hotkey: repeat(0x46), RegistrationBlock: 45}},
				ProviderShare: protocol.Rational{Numerator: 1, Denominator: 10}, TreasuryShare: protocol.Rational{Numerator: 9, Denominator: 10}, MaxWeightLimitU16: 32768}},
		NativeChain: "synthetic-test-vector-chain", RuntimeReviewHash: repeat(0x91), ValidatorHotkey: repeat(0x5a), SubnetOwner: repeat(0x61),
		OwnerHotkeys: [][32]byte{repeat(0x41)}, FirstNativeEpoch: 20, ValidFromNativeBlock: 101, ValidThroughNativeBlock: 200, MaximumSubnetUids: 16, MaximumOwnedHotkeys: 16,
		Production: &validator.TreasuryProductionApproval{Schema: validator.TreasuryProductionScope, RuntimeCapability: crv4.ValidatorProducerRuntimeProfile,
			EpochScheduleProfile: crv4.TempoDriftEpochScheduleProfile, ValidatorHotkeys: [][32]byte{repeat(0x5a)}, MaximumLastUpdateAge: 100,
			ValidThroughNativeEpoch: 30, ActivationNativeHash: repeat(0x92)}}
}

// Runs the real dispatcher, including its durable-volume admission.
func treasuryApprovalTestRun(t *testing.T, args ...string) (int, []byte, string) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	code := runMain(t.Context(), append([]string{"treasury"}, args...), &output, &diagnostic)
	return code, output.Bytes(), diagnostic.String()
}

// Assembles through the public command and decodes only a successful result.
func treasuryApprovalTestEnvelope(t *testing.T, approvalPath, signature, key, out string) (int, treasuryApprovalEnvelopeResult, string) {
	t.Helper()
	code, output, diagnostic := treasuryApprovalTestRun(t, "approval-envelope", "--approval", approvalPath, "--signature", signature, "--approval-key", key, "--out", out)
	var result treasuryApprovalEnvelopeResult
	if code == 0 {
		if err := decodePlanJson(output, &result); err != nil {
			t.Fatal(err)
		}
	} else if len(output) != 0 {
		t.Fatal("refused approval envelope wrote a result", string(output))
	}
	return code, result, diagnostic
}

// A changed signature digit stays lowercase hex, so only verification refuses it.
func treasuryApprovalTestOtherDigit(digit byte) byte {
	if digit == '0' {
		return '1'
	}
	return '0'
}

// One operator and one validator under the approved mainnet policy. The
// contract addresses keep checksum case, which the producer loader lowercases
// before hashing, and the treasury policy is real receive-only plan output.
type treasuryApprovalTestFixture struct {
	directory  string
	config     validator.ReleaseConfig
	configPath string
	private    ed25519.PrivateKey
	key        string
	input      treasuryApprovalInput
	planPath   string
	planHash   string
}

func newTreasuryApprovalTestFixture(t *testing.T) *treasuryApprovalTestFixture {
	t.Helper()
	// The test directory is owner-private whatever umask the test runner uses.
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	destination, _, _ := treasuryDestinationTestFixture(t)
	observePath, _ := treasuryTestJson(t, directory, "receive-observe.json", destination)
	code, observation, diagnostic := treasuryApprovalTestRun(t, "observe", "--input", observePath)
	if code != 0 || decodePlanJson(observation, &destination.Observation) != nil {
		t.Fatal("receive-only observation failed", code, diagnostic)
	}
	destinationPath, _ := treasuryTestJson(t, directory, "policy-plan-input.json", destination)
	code, plan, diagnostic := treasuryApprovalTestRun(t, "policy-plan", "--input", destinationPath)
	if code != 0 {
		t.Fatal("receive-only policy plan failed", code, diagnostic)
	}
	planPath, planHash := ownerRecycleTestFile(t, directory, "policy-plan.json", plan)
	policy, err := protocol.LoadPolicy(filepath.Join("..", "deploy", "mainnet", "policy-v1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	policyHash, err := policy.Hash()
	if err != nil {
		t.Fatal(err)
	}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x7a}, ed25519.SeedSize))
	key := "0x" + hex.EncodeToString(private.Public().(ed25519.PublicKey))
	network := destination.Policy
	root := filepath.Join(directory, "validator")
	operator := filepath.Join(root, "operator-1")
	cfg := validator.ReleaseConfig{SchemaVersion: 3, Production: true, Release: "1.0", DeploymentID: "synthetic-treasury-approval", ValidatorID: 1,
		ChainID: mainnetEvmChainId, GenesisHash: network.GenesisHash, RuntimeSpec: network.RuntimeVersion.SpecVersion, TransactionVersion: network.RuntimeVersion.TransactionVersion,
		StateVersion: network.RuntimeVersion.StateVersion, RuntimeCodeHash: network.RuntimeCodeHash, RuntimeMetadataHash: network.RuntimeMetadataHash, Netuid: 25,
		Coordinator: common.HexToAddress("0x" + strings.Repeat("ab", 20)).Hex(), SettlementVault: common.HexToAddress("0x" + strings.Repeat("cd", 20)).Hex(), DeployBlock: 10,
		PolicyHash: fmt.Sprintf("0x%x", policyHash), Policy: *policy, RPC: []string{"https://evm.example"}, Substrate: []string{"wss://native.example"},
		StateDir: filepath.Join(root, "state"), HotkeySeedFile: filepath.Join(root, "hotkey.seed"), TrailDepth: policy.Verify.TrailDepth, PollSeconds: 2,
		Operators: []validator.OperatorConfig{{NoID: 1, APIURL: "https://operator-1.example", ConnectURL: "wss://operator-1.example", ArtifactSigner: "0x" + strings.Repeat("50", 20),
			StateDir: operator, NetworkJWTFile: filepath.Join(operator, "network.jwt"), ClientJWTFile: filepath.Join(operator, "client.jwt"), ClientKeySeedFile: filepath.Join(operator, "client.key"), Concurrency: 2}},
		TreasuryApproval: &validator.ReleaseTreasuryApprovalConfig{Approval: validator.ReleaseEvidenceV2File{Path: filepath.Join(directory, "treasury-approval-envelope.json")}, Signer: key}}
	if cfg.Coordinator == strings.ToLower(cfg.Coordinator) || cfg.SettlementVault == strings.ToLower(cfg.SettlementVault) {
		t.Fatal("fixture lost its checksum-case contract addresses")
	}
	stream := validator.AttemptStreamV2Bounds{MaxDataBytes: 4 * 1024 * 1024, MaxItems: 128, MaxChunkBytes: 128 * 1024, MaxChunks: 128, MaxPages: 8, MaxPageBytes: 4096, MaxDescriptorsPerPage: 16, MaxManifestBytes: 1024}
	cfg.EvidenceV2 = validator.ReleaseEvidenceV2Config{Schema: validator.ReleaseEvidenceV2ConfigSchema, Bounds: validator.ReleaseEvidenceV2Bounds{
		Disk:         validator.AttemptLedgerDiskLimits{MaxRecordBytes: 64 * 1024, MaxRecordCount: 128, MaxTrailCount: 16, MaxRawRecordBytes: 4 * 1024 * 1024, MaxStorageBytes: 16 * 1024 * 1024, MaxStorageFiles: 256, MaxLegacyBytes: 4 * 1024 * 1024, MaxProofBytes: 1024 * 1024},
		Cut:          validator.AttemptCutV2Bounds{MaxHeaderBytes: max(stream.MaxManifestBytes, stream.MaxPageBytes), Records: stream, Proofs: stream},
		Replay:       validator.AttemptCutV2ReplayBounds{MaxRecordBytes: 128 * 1024, MaxProofBytes: 128 * 1024, MaxTrails: 16, MaxScratchBytes: 16 * 1024 * 1024, MaxScratchFiles: 256},
		Persistence:  validator.AttemptSettlementRuntimeV2PersistenceBounds{MaxSnapshotBytes: 2 * 1024 * 1024, MaxJournalBytes: 16 * 1024 * 1024},
		HeadEMA:      validator.HeadEMAStoreV2Limits{MaxFileBytes: 2 * 1024 * 1024, MaxEntries: 64, MaxControlBytes: 8 * 1024 * 1024},
		MaxProviders: 128, MaxEgressHashes: 128, MaxFleetPrefixes: 128, MaxOperators: 1, MaxHeadEntries: 64, MaxArtifactBytes: 64 * 1024 * 1024, MaxControlBytes: 64 * 1024 * 1024,
		MaxInputJournalBytes: 4 * 1024 * 1024, MaxParticipants: 1, MaxTransitionBytes: 1024 * 1024, MaxClosureBytes: 4 * 1024 * 1024, MaxHistoryBytes: 1024 * 1024,
	}}
	evidence := filepath.Join(root, "evidence", "operator-1")
	reference := func(name string, size uint64) validator.ReleaseEvidenceV2File {
		return validator.ReleaseEvidenceV2File{Path: filepath.Join(evidence, name), Bytes: size, SHA256: "0x" + strings.Repeat("ab", 32)}
	}
	cfg.EvidenceV2.Operators = []validator.ReleaseEvidenceV2OperatorConfig{{NoID: 1,
		Activation: reference("activation.payload", uint64(protocol.ValidatorEvidenceActivationPayloadSize)), VPKSignature: reference("vpk.signature", 64),
		HotkeySignature: reference("hotkey.signature", 64), Context: reference("context.json", 32), History: reference("history.json", 32),
		ReplayScratchRoot: filepath.Join(root, "scratch", "replay"), SealScratchRoot: filepath.Join(root, "scratch", "seal")}}
	hex32 := func(value byte) string { return "0x" + strings.Repeat(fmt.Sprintf("%02x", value), 32) }
	profile := crv4.TempoDriftEpochScheduleProfile
	input := treasuryApprovalInput{Schema: treasuryApprovalInputSchema, NativeChain: network.NativeChain, RuntimeReviewHash: hex32(0x91),
		ValidatorHotkey: hex32(0x5a), SubnetOwner: hex32(0x47), OwnerHotkeys: []string{hex32(0x48)}, FirstNativeEpoch: 20,
		ValidFromNativeBlock: 101, ValidThroughNativeBlock: 200, MaximumSubnetUids: 16, MaximumOwnedHotkeys: 16,
		Proposal: treasuryApprovalProposalInput{ParentPolicyHash: cfg.PolicyHash, PolicyId: policy.PolicyID + 1, EffectiveEpoch: policy.EffectiveEpoch + 1,
			ProviderShare: protocol.Rational{Numerator: 1, Denominator: 10}, Remainder: "ordinary_treasury_credit", OwnerAllocation: "equal_exact_registered",
			Runtime: treasuryApprovalRuntimeInput{GenesisHash: network.GenesisHash, Netuid: 25, Version: network.RuntimeVersion,
				CodeHash: network.RuntimeCodeHash, MetadataHash: network.RuntimeMetadataHash, SourceCommit: network.RuntimeSourceCommit}},
		Production: treasuryApprovalProductionInput{RuntimeCapability: crv4.ValidatorProducerRuntimeProfile, EpochScheduleProfile: &profile,
			ValidatorHotkeys: []string{hex32(0x5a)}, MaximumLastUpdateAge: 100, ValidThroughNativeEpoch: 30, ActivationNativeHash: hex32(0x92)}}
	return &treasuryApprovalTestFixture{directory: directory, config: cfg, configPath: filepath.Join(directory, "validator.yml"),
		private: private, key: key, input: input, planPath: planPath, planHash: planHash}
}

// The operator edits one YAML config in place; every write keeps its path.
func (self *treasuryApprovalTestFixture) writeConfig(t *testing.T) string {
	t.Helper()
	raw, err := yaml.Marshal(self.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(self.configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return monitorReadDigest(raw)
}

// The complete approval-plan command line for the fixture's current inputs.
func (self *treasuryApprovalTestFixture) planArgs(t *testing.T, out string) []string {
	t.Helper()
	inputPath, _ := treasuryTestJson(t, self.directory, "approval-input.json", self.input)
	return []string{"approval-plan", "--input", inputPath, "--config", self.configPath, "--config-sha256", self.writeConfig(t),
		"--policy-plan", self.planPath, "--policy-plan-sha256", self.planHash, "--approval-key", self.key, "--out", out}
}

// Runs approval-plan and decodes only a successful result.
func (self *treasuryApprovalTestFixture) plan(t *testing.T, out string) (int, treasuryApprovalPlan, string) {
	t.Helper()
	code, output, diagnostic := treasuryApprovalTestRun(t, self.planArgs(t, out)...)
	var result treasuryApprovalPlan
	if code == 0 {
		if err := decodePlanJson(output, &result); err != nil {
			t.Fatal(err)
		}
	} else if len(output) != 0 {
		t.Fatal("refused approval plan wrote a result", string(output))
	}
	return code, result, diagnostic
}

// Acts as the external signer: the digest is recomputed from the approval file
// alone, using the documented domain line, and the 32 bytes are signed.
func (self *treasuryApprovalTestFixture) sign(t *testing.T, approvalPath string) ([32]byte, string) {
	t.Helper()
	raw, err := os.ReadFile(approvalPath)
	if err != nil || len(raw) < 2 || raw[len(raw)-1] != '\n' || raw[len(raw)-2] == '\n' {
		t.Fatal("approval file is not JSON with one final line feed", err)
	}
	digest := sha256.Sum256(append([]byte("urnetwork-native-treasury-approval-signature-v1\n"), raw[:len(raw)-1]...))
	return digest, hex.EncodeToString(ed25519.Sign(self.private, digest[:]))
}

// The fixed vector matches its stated meaning, the plain derivation, the
// validator's own message, an RFC 8032 signature and the public command's
// exact envelope bytes.
func TestTreasuryApprovalSigningVector(t *testing.T) {
	encoded, err := json.Marshal(treasuryApprovalVectorBody())
	if err != nil || string(encoded) != treasuryApprovalVectorJSON {
		t.Fatal("vector approval JSON differs from its stated body", err)
	}
	domain := validator.TreasuryApprovalSignatureDomain
	if domain != "urnetwork-native-treasury-approval-signature-v1\n" || len(domain) != 48 || !strings.Contains(treasuryApprovalDerivation, "the 47 ASCII bytes") {
		t.Fatal("signature domain differs from its plain description")
	}
	digest := sha256.Sum256(append([]byte(domain), treasuryApprovalVectorJSON...))
	draft, err := validator.DecodeTreasuryApprovalDraft([]byte(treasuryApprovalVectorJSON + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	message, err := draft.Approval.SigningMessage()
	if err != nil || "0x"+hex.EncodeToString(digest[:]) != treasuryApprovalVectorDigest || draft.Digest != digest || !bytes.Equal(message, digest[:]) {
		t.Fatal("vector digest differs from the validator's signing message", err)
	}
	seed, err := hex.DecodeString(treasuryApprovalVectorSeed)
	if err != nil {
		t.Fatal(err)
	}
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	signature := ed25519.Sign(private, digest[:])
	if "0x"+hex.EncodeToString(public) != treasuryApprovalVectorPublicKey || hex.EncodeToString(signature) != treasuryApprovalVectorSignature || !ed25519.Verify(public, digest[:], signature) {
		t.Fatal("vector key or signature differs")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	approvalPath, _ := ownerRecycleTestFile(t, directory, "approval.json", []byte(treasuryApprovalVectorJSON+"\n"))
	envelopePath := filepath.Join(directory, "envelope.json")
	code, result, diagnostic := treasuryApprovalTestEnvelope(t, approvalPath, treasuryApprovalVectorSignature, treasuryApprovalVectorPublicKey, envelopePath)
	if code != 0 {
		t.Fatal("vector envelope refused", code, diagnostic)
	}
	envelope, err := os.ReadFile(envelopePath)
	info, statErr := os.Lstat(envelopePath)
	if err != nil || statErr != nil || info.Mode().Perm() != 0600 ||
		string(envelope) != `{"approval":`+treasuryApprovalVectorJSON+`,"signature":"`+treasuryApprovalVectorSignature+`"}`+"\n" {
		t.Fatal("vector envelope is not the exact private loader file", err, statErr)
	}
	want := validator.ReleaseEvidenceV2File{Path: envelopePath, Bytes: uint64(len(envelope)), SHA256: treasuryApprovalVectorEnvelopeSha256}
	if result.Schema != treasuryApprovalEnvelopeSchema || result.TreasuryApproval.Approval != want || result.TreasuryApproval.Signer != treasuryApprovalVectorPublicKey ||
		result.SigningDigest != treasuryApprovalVectorDigest || result.ConfigHash != "0x"+strings.Repeat("c1", 32) {
		t.Fatal("vector envelope reference differs", result)
	}
	if _, err := validator.VerifyTreasuryApproval(envelope, [32]byte(public), treasuryApprovalVectorEnvelopeSha256); err != nil {
		t.Fatal("validator verifier refused the vector envelope", err)
	}
}

// The full public path: real policy-plan output, approval-plan, an external
// signature over the digest recomputed from the approval file,
// approval-envelope, then the validator's own production loader. Every
// approval byte and signature digit is load-bearing, including when a config
// re-pins the changed envelope.
func TestTreasuryApprovalPlanEnvelopeLoaderRoundTrip(t *testing.T) {
	f := newTreasuryApprovalTestFixture(t)
	approvalPath := filepath.Join(f.directory, "treasury-approval.json")
	code, plan, diagnostic := f.plan(t, approvalPath)
	if code != 0 {
		t.Fatal("approval plan refused the fixture", code, diagnostic)
	}
	approval, err := os.ReadFile(approvalPath)
	info, statErr := os.Lstat(approvalPath)
	if err != nil || statErr != nil || info.Mode().Perm() != 0600 {
		t.Fatal("approval file is not private", err, statErr)
	}
	approvalDigest := sha256.Sum256(approval)
	planRaw, err := os.ReadFile(f.planPath)
	var policyPlan treasuryPolicyPlanOutput
	if err != nil || decodePlanJson(planRaw, &policyPlan) != nil {
		t.Fatal("policy plan fixture is unreadable", err)
	}
	planDigest := sha256.Sum256(planRaw)
	if plan.Schema != treasuryApprovalPlanSchema || plan.Approved || plan.ApprovalKey != f.key || plan.SigningDomain != validator.TreasuryApprovalSignatureDomain ||
		plan.SigningDerivation != treasuryApprovalDerivation || plan.PolicyPlan.Sha256 != "sha256:"+hex.EncodeToString(planDigest[:]) ||
		plan.TreasuryPolicyHash != policyPlan.Hash || plan.ObservationHash != policyPlan.Observation ||
		plan.ApprovalFile != (validator.ReleaseEvidenceV2File{Path: approvalPath, Bytes: uint64(len(approval)), SHA256: "0x" + hex.EncodeToString(approvalDigest[:])}) {
		t.Fatal("approval plan lost its exact inputs, bindings or file reference", plan)
	}
	digest, signature := f.sign(t, approvalPath)
	if plan.SigningDigest != "0x"+hex.EncodeToString(digest[:]) {
		t.Fatal("printed digest differs from the one recomputed from the approval file")
	}

	// Drafting is deterministic and never replaces a reviewed approval file.
	again := filepath.Join(f.directory, "treasury-approval-again.json")
	code, replay, _ := f.plan(t, again)
	repeated, err := os.ReadFile(again)
	if code != 0 || err != nil || replay.SigningDigest != plan.SigningDigest || replay.ConfigHash != plan.ConfigHash || !bytes.Equal(repeated, approval) {
		t.Fatal("approval plan is not deterministic", code, err)
	}
	if code, _, diagnostic := f.plan(t, approvalPath); code == 0 || !strings.Contains(diagnostic, "never replaced") {
		t.Fatal("approval plan replaced an existing approval file", code, diagnostic)
	}
	if current, err := os.ReadFile(approvalPath); err != nil || !bytes.Equal(current, approval) {
		t.Fatal("refused approval plan changed the existing file", err)
	}

	// The command assembles the envelope at the path the config nominated.
	envelopePath := f.config.TreasuryApproval.Approval.Path
	code, result, diagnostic := treasuryApprovalTestEnvelope(t, approvalPath, signature, f.key, envelopePath)
	if code != 0 {
		t.Fatal("approval envelope refused a valid signature", code, diagnostic)
	}
	envelope, err := os.ReadFile(envelopePath)
	if err != nil {
		t.Fatal(err)
	}
	envelopeDigest := sha256.Sum256(envelope)
	body := approval[:len(approval)-1]
	if result.TreasuryApproval.Approval != (validator.ReleaseEvidenceV2File{Path: envelopePath, Bytes: uint64(len(envelope)), SHA256: "0x" + hex.EncodeToString(envelopeDigest[:])}) ||
		result.TreasuryApproval.Signer != f.key || result.ConfigHash != plan.ConfigHash || result.SigningDigest != plan.SigningDigest ||
		!bytes.Equal(envelope, fmt.Appendf(nil, "{\"approval\":%s,\"signature\":%q}\n", body, signature)) {
		t.Fatal("envelope is not the approval file's JSON with its signature", result)
	}
	if code, _, diagnostic := treasuryApprovalTestEnvelope(t, approvalPath, signature, f.key, envelopePath); code == 0 || !strings.Contains(diagnostic, "never replaced") {
		t.Fatal("approval envelope replaced an existing envelope", code, diagnostic)
	}

	// Only the envelope reference changes in the operator's config.
	f.config.TreasuryApproval = &result.TreasuryApproval
	f.writeConfig(t)
	loaded, err := validator.LoadReleaseConfig(f.configPath)
	if err != nil {
		t.Fatal("validator loader refused the assembled treasury approval", err)
	}
	hash, err := validator.OwnerRecycleConfigHash(loaded)
	if err != nil || "0x"+hex.EncodeToString(hash[:]) != plan.ConfigHash {
		t.Fatal("draft config hash differs from the loader's", err)
	}
	admitted, err := validator.DecodeTreasuryApproval(loaded, envelope)
	draft, draftErr := validator.DecodeTreasuryApprovalDraft(approval)
	if err != nil || draftErr != nil || !reflect.DeepEqual(admitted.Approval, draft.Approval) || admitted.Approval.Proposal.Treasury.Recipients[0].Hotkey != policyPlan.Policy.Recipients[0].Hotkey {
		t.Fatal("loaded approval differs from the drafted body", err, draftErr)
	}

	// Each changed approval byte is refused; value changes reach the signature.
	prefix := len(`{"approval":`)
	signatureRefusals := 0
	for index := range body {
		tampered := bytes.Clone(envelope)
		tampered[prefix+index] ^= 0x01
		_, err := validator.DecodeTreasuryApproval(loaded, tampered)
		if err == nil {
			t.Fatal("loader admitted a changed approval byte", index)
		}
		if strings.Contains(err.Error(), "signature differs") {
			signatureRefusals++
		}
	}
	if signatureRefusals == 0 {
		t.Fatal("no changed approval value reached the signature check")
	}
	start := len(envelope) - len(signature) - len("\"}\n")
	for index := range len(signature) {
		tampered := bytes.Clone(envelope)
		tampered[start+index] = treasuryApprovalTestOtherDigit(tampered[start+index])
		if _, err := validator.DecodeTreasuryApproval(loaded, tampered); err == nil || !strings.Contains(err.Error(), "signature differs") {
			t.Fatal("loader admitted a changed signature digit", index, err)
		}
	}
	upper := bytes.Clone(envelope)
	letter := bytes.IndexAny(upper[start:start+len(signature)], "abcdef")
	if letter < 0 {
		t.Fatal("synthetic signature has no hex letter")
	}
	upper[start+letter] -= 'a' - 'A'
	if _, err := validator.DecodeTreasuryApproval(loaded, upper); err == nil {
		t.Fatal("loader admitted an uppercase signature digit")
	}
	signer := treasuryApprovalKey(f.key)
	for index := range approval {
		tampered := bytes.Clone(approval)
		tampered[index] ^= 0x01
		if _, _, err := validator.AssembleTreasuryApprovalEnvelope(tampered, signer, signature); err == nil {
			t.Fatal("assembly admitted a changed approval file byte", index)
		}
	}

	// A config that re-pins changed bytes is still refused by the loader.
	generation := bytes.Index(envelope, []byte(`"registration_block":20}`))
	if generation < 0 {
		t.Fatal("fixture recipient generation is absent from the envelope")
	}
	for _, fault := range []string{"approval", "signature", "unpinned"} {
		tampered := bytes.Clone(envelope)
		if fault == "signature" {
			tampered[start] = treasuryApprovalTestOtherDigit(tampered[start])
		} else {
			tampered[generation+len(`"registration_block":2`)] = '1'
		}
		path := filepath.Join(f.directory, "tampered-"+fault+"-envelope.json")
		if err := os.WriteFile(path, tampered, 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(tampered)
		selection := validator.ReleaseTreasuryApprovalConfig{Signer: f.key,
			Approval: validator.ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(tampered)), SHA256: "0x" + hex.EncodeToString(digest[:])}}
		if fault == "unpinned" {
			selection.Approval.SHA256 = result.TreasuryApproval.Approval.SHA256
		}
		f.config.TreasuryApproval = &selection
		f.writeConfig(t)
		_, err := validator.LoadReleaseConfig(f.configPath)
		if err == nil || fault != "unpinned" && !strings.Contains(err.Error(), "signature differs") {
			t.Fatal("validator loader admitted a changed treasury approval", fault, err)
		}
	}

	// The public command refuses a changed file, another key or a bad signature.
	tamperedPath, _ := ownerRecycleTestFile(t, f.directory, "tampered-approval.json", bytes.Replace(approval, []byte(`"first_native_epoch":20`), []byte(`"first_native_epoch":21`), 1))
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x7b}, ed25519.SeedSize))
	otherKey := "0x" + hex.EncodeToString(other.Public().(ed25519.PublicKey))
	for name, args := range map[string][4]string{
		"approval":  {tamperedPath, signature, f.key, "refused-approval.json"},
		"key":       {approvalPath, signature, otherKey, "refused-key.json"},
		"signer":    {approvalPath, hex.EncodeToString(ed25519.Sign(other, digest[:])), f.key, "refused-signer.json"},
		"uppercase": {approvalPath, strings.ToUpper(signature), f.key, "refused-uppercase.json"},
		"prefixed":  {approvalPath, "0x" + signature, f.key, "refused-prefixed.json"},
	} {
		out := filepath.Join(f.directory, args[3])
		if code, _, _ := treasuryApprovalTestEnvelope(t, args[0], args[1], args[2], out); code == 0 {
			t.Fatal("approval envelope admitted a refused input", name)
		}
		if _, err := os.Lstat(out); !os.IsNotExist(err) {
			t.Fatal("refused approval envelope created its output", name, err)
		}
	}
}

// approval-plan writes nothing when its pinned inputs, explicit input grammar
// or the validator's own admission would refuse the approval.
func TestTreasuryApprovalPlanRefusesInadmissibleDrafts(t *testing.T) {
	f := newTreasuryApprovalTestFixture(t)
	if code, _, diagnostic := f.plan(t, filepath.Join(f.directory, "baseline.json")); code != 0 {
		t.Fatal("negative fixture never admitted its unchanged baseline", code, diagnostic)
	}
	original, originalConfig := f.input, f.config
	hex32 := func(value byte) string { return "0x" + strings.Repeat(fmt.Sprintf("%02x", value), 32) }
	planVariant := func(change func(*treasuryPolicyPlanOutput)) func() {
		return func() {
			raw, err := os.ReadFile(f.planPath)
			var output treasuryPolicyPlanOutput
			if err != nil || decodePlanJson(raw, &output) != nil {
				t.Fatal("policy plan fixture is unreadable", err)
			}
			change(&output)
			f.planPath, f.planHash = treasuryTestJson(t, f.directory, "changed-policy-plan.json", output)
		}
	}
	originalPlanPath, originalPlanHash := f.planPath, f.planHash
	for _, fault := range []struct {
		name, expected string
		change         func()
		args           func([]string)
	}{
		{name: "config-pin", expected: "config differs from its independent pin", args: func(args []string) { args[6] = "sha256:" + strings.Repeat("11", 32) }},
		{name: "policy-plan-pin", expected: "policy plan differs from its independent pin", args: func(args []string) { args[10] = "sha256:" + strings.Repeat("11", 32) }},
		{name: "approval-key", expected: "pins another signer", args: func(args []string) { args[12] = hex32(0x7b) }},
		{name: "config-signer", expected: "pins another signer", change: func() {
			f.config.TreasuryApproval = &validator.ReleaseTreasuryApprovalConfig{Approval: originalConfig.TreasuryApproval.Approval, Signer: hex32(0x7b)}
		}},
		{name: "owner-recycle-selector", expected: "only its treasury_approval selector", change: func() {
			f.config.TreasuryApproval, f.config.OwnerRecycleApproval = nil, originalConfig.TreasuryApproval
		}},
		{name: "relative-path", expected: "depends on where the config file sits", change: func() {
			operator := originalConfig.Operators[0]
			operator.RequestPreparation = &validator.ReleaseEvidenceV2File{Path: "request-preparation.json", Bytes: 64, SHA256: "0x" + strings.Repeat("ab", 32)}
			f.config.Operators = []validator.OperatorConfig{operator}
		}},
		{name: "policy-hash", expected: "hash matches its policy", change: planVariant(func(output *treasuryPolicyPlanOutput) { output.Hash = hex32(0x11) })},
		{name: "approved-plan", expected: "hash matches its policy", change: planVariant(func(output *treasuryPolicyPlanOutput) { output.Approved = true })},
		{name: "owner-order", expected: "must be ordered, unique, nonzero", change: func() { f.input.OwnerHotkeys = []string{hex32(0x49), hex32(0x48)} }},
		{name: "recipient-generation", expected: "outside the signed census or activation", change: func() { f.input.ValidFromNativeBlock = 20 }},
		{name: "runtime", expected: "runtime and configured mainnet tuple differ", change: func() { f.input.Proposal.Runtime.CodeHash = hex32(0x27) }},
		{name: "provider-share", expected: "exactly one tenth provider allocation", change: func() { f.input.Proposal.ProviderShare = protocol.Rational{Numerator: 1, Denominator: 9} }},
		{name: "validator-census", expected: "validator census", change: func() { f.input.Production.ValidatorHotkeys = []string{} }},
		{name: "omitted-owner-hotkeys", expected: "explicit owner_hotkeys list", change: func() { f.input.OwnerHotkeys = nil }},
		{name: "omitted-schedule", expected: "explicit epoch_schedule_profile", change: func() { f.input.Production.EpochScheduleProfile = nil }},
		{name: "noncanonical-hex", expected: "must be canonical nonzero lowercase", change: func() { f.input.ValidatorHotkey = strings.ToUpper(hex32(0x5a)) }},
	} {
		f.input, f.config, f.planPath, f.planHash = original, originalConfig, originalPlanPath, originalPlanHash
		if fault.change != nil {
			fault.change()
		}
		out := filepath.Join(f.directory, "refused-"+fault.name+".json")
		args := f.planArgs(t, out)
		if fault.args != nil {
			fault.args(args)
		}
		code, output, diagnostic := treasuryApprovalTestRun(t, args...)
		if code == 0 || len(output) != 0 || !strings.Contains(diagnostic, fault.expected) {
			t.Fatal("approval plan admitted or misattributed an inadmissible draft", fault.name, code, diagnostic)
		}
		if _, err := os.Lstat(out); !os.IsNotExist(err) {
			t.Fatal("refused approval plan created its output", fault.name, err)
		}
	}
}

// The owner-validator form: the validator runs on the subnet owner hotkey, so
// the signed owner census holds the validator itself. The public path drafts,
// assembles and loads it; another approved validator still cannot be an owner.
func TestTreasuryApprovalPlanAdmitsOwnerValidator(t *testing.T) {
	f := newTreasuryApprovalTestFixture(t)
	hex32 := func(value byte) string { return "0x" + strings.Repeat(fmt.Sprintf("%02x", value), 32) }
	owner := hex32(0x48)
	f.input.ValidatorHotkey, f.input.OwnerHotkeys, f.input.Production.ValidatorHotkeys = owner, []string{owner}, []string{owner}
	approvalPath := filepath.Join(f.directory, "owner-validator-approval.json")
	code, plan, diagnostic := f.plan(t, approvalPath)
	if code != 0 {
		t.Fatal("approval plan refused the owner-validator", code, diagnostic)
	}
	digest, signature := f.sign(t, approvalPath)
	if plan.SigningDigest != "0x"+hex.EncodeToString(digest[:]) {
		t.Fatal("printed digest differs from the one recomputed from the approval file")
	}
	code, result, diagnostic := treasuryApprovalTestEnvelope(t, approvalPath, signature, f.key, f.config.TreasuryApproval.Approval.Path)
	if code != 0 {
		t.Fatal("approval envelope refused the owner-validator", code, diagnostic)
	}
	f.config.TreasuryApproval = &result.TreasuryApproval
	f.writeConfig(t)
	loaded, err := validator.LoadReleaseConfig(f.configPath)
	if err != nil {
		t.Fatal("validator loader refused the owner-validator approval", err)
	}
	envelope, err := os.ReadFile(result.TreasuryApproval.Approval.Path)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := validator.DecodeTreasuryApproval(loaded, envelope)
	key := [][32]byte{treasuryApprovalKey(owner)}
	if err != nil || admitted.Approval.ValidatorHotkey != key[0] || !reflect.DeepEqual(admitted.Approval.OwnerHotkeys, key) ||
		!reflect.DeepEqual(admitted.Approval.Production.ValidatorHotkeys, key) || admitted.Approval.Proposal.Treasury == nil {
		t.Fatal("loaded owner-validator approval differs from its draft", err)
	}

	other := newTreasuryApprovalTestFixture(t)
	other.input.OwnerHotkeys, other.input.Production.ValidatorHotkeys = []string{owner}, []string{owner, hex32(0x5a)}
	out := filepath.Join(other.directory, "refused-owner-census-validator.json")
	if code, _, diagnostic := other.plan(t, out); code == 0 || !strings.Contains(diagnostic, "cannot be an owner recipient") {
		t.Fatal("approval plan admitted another approved validator as an owner", code, diagnostic)
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatal("refused approval plan created its output", err)
	}
}
