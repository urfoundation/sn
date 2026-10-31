//go:build linux || darwin

// Public setup composes with the real preparation publisher and activation's
// empty-ledger admission. Mount facts and chain snapshots are synthetic;
// signed documents, files, custody checkpoints and databases are real.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/crv4"
	"github.com/urnetwork/connect/durablevolume"
)

// Signed synthetic configuration has private keys outside the empty runtime
// root and the complete configured ledger census, including valid native UID0.
type validatorStorageCommandTestFixture struct {
	ctx             context.Context
	root            string
	metadata        string
	configPath      string
	cfg             *ReleaseConfig
	approvalKeyPath string
	request         string
	args            []string
	keys            map[uint64]ed25519.PrivateKey
}

func newValidatorStorageCommandTestFixture(t *testing.T) *validatorStorageCommandTestFixture {
	t.Helper()
	ctx, reference, root, _, _ := validatorRuntimePreparationFixture(t)
	raw, err := os.ReadFile(reference.Path)
	if err != nil {
		t.Fatal(err)
	}
	var prior durablevolume.PreparationRequest
	if err := json.Unmarshal(raw, &prior); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Dir(reference.Path)
	profile, options, snapshot := configureTestInputs(t)
	for _, path := range []string{options.HotkeySeedFile, options.ApprovalKeyFile, options.ClientKeySeedFile} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	keyState := filepath.Join(filepath.Dir(options.HotkeySeedFile), "keys")
	options.ClientKeySeedFile = filepath.Join(keyState, "no-1", "client.key")
	options.StateDir, options.EvidenceDir = filepath.Join(root, "state"), filepath.Join(root, "evidence")
	options.OutputPath = filepath.Join(metadata, "configured.yml")
	initOptions, err := parseValidatorArgsForTest(t, []string{"init", "--state_dir=" + keyState, "--hotkey_seed_file=" + options.HotkeySeedFile,
		"--no_id=1", "--approval-key-file=" + options.ApprovalKeyFile})
	if err != nil {
		t.Fatal(err)
	}
	initOutput, err := os.OpenFile(filepath.Join(metadata, "init-output"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(runInit(initOptions, initOutput), initOutput.Close()); err != nil {
		t.Fatal("documented key initialization failed", err)
	}
	var output, diagnostic bytes.Buffer
	readSnapshot := func(context.Context, *releaseMainnetProfile, string) (releaseConfigureNativeSnapshot, error) {
		return snapshot, nil
	}
	if code := runConfigureCommandWithProfile(t.Context(), configureTestArgs(options), &output, &diagnostic, profile, readSnapshot); code != 0 {
		t.Fatal("actual init keys cannot configure the signed pending profile", diagnostic.String())
	}
	cfg, err := LoadReleaseProductionConfigPreActivation(options.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	keyKVs := map[uint64]ed25519.PrivateKey{}
	for _, operator := range cfg.Operators {
		seed, err := loadClientSeed(operator.ClientKeySeedFile)
		if err != nil {
			t.Fatal("actual init key cannot open the storage identity", err)
		}
		keyKVs[operator.NoID] = ed25519.NewKeyFromSeed(seed)
	}
	request := filepath.Join(metadata, "command-request.json")
	args := []string{"--config=" + options.OutputPath, "--root-dir=" + root, "--validator-uid=0", "--mount-path=" + prior.MountPath,
		"--filesystem-uuid=" + prior.FilesystemUuid, "--filesystem-type=" + prior.FilesystemType, "--fresh", "--out=" + request,
		"--min-available-bytes=" + strconv.FormatUint(prior.MinAvailableBytes, 10), "--min-available-inodes=" + strconv.FormatUint(prior.MinAvailableInodes, 10)}
	return &validatorStorageCommandTestFixture{ctx: ctx, root: root, metadata: metadata, configPath: options.OutputPath, cfg: cfg,
		approvalKeyPath: options.ApprovalKeyFile, request: request, args: args, keys: keyKVs}
}

// A directory-sync failure cannot return a usable request reference, even
// after both complete files have been written and their own fsync succeeded.
func TestValidatorStorageRequestDirectorySyncFailureReturnsNoReference(t *testing.T) {
	f := newValidatorStorageCommandTestFixture(t)
	canary := errors.New("synthetic metadata directory sync failure")
	calls := 0
	ctx := context.WithValue(f.ctx, validatorStorageRequestSyncKey{}, func(directory *os.File) error {
		calls++
		if directory.Name() != f.metadata {
			t.Fatal("request synced another directory", directory.Name())
		}
		for _, path := range []string{f.request, f.request + ".fresh.json"} {
			if raw, err := os.ReadFile(path); err != nil || !json.Valid(raw) {
				t.Fatal("directory sync preceded complete request files", err)
			}
		}
		return errors.Join(directory.Sync(), canary)
	})
	var output, diagnostic bytes.Buffer
	code := runValidatorStoragePrepare(ctx, append([]string{"request"}, f.args...), &output, &diagnostic)
	if code != 1 || calls != 1 || output.Len() != 0 || !strings.Contains(diagnostic.String(), canary.Error()) {
		t.Fatalf("directory sync failure returned request success: code=%d calls=%d output=%s diagnostic=%s", code, calls, output.String(), diagnostic.String())
	}
}

// Signed input and the complete operator census flow through every public
// command. Activation must admit each actually published target checkpoint.
func TestValidatorStoragePublicRequestPlanApplyReachesFreshActivation(t *testing.T) {
	f := newValidatorStorageCommandTestFixture(t)
	var output, diagnostic bytes.Buffer
	if code := runValidatorStoragePrepare(f.ctx, append([]string{"request"}, f.args...), &output, &diagnostic); code != 0 {
		t.Fatalf("public request failed: %s", diagnostic.String())
	}
	var request durablevolume.Reference
	if err := json.Unmarshal(output.Bytes(), &request); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if code := runValidatorStoragePrepare(f.ctx, []string{"plan", "--request=" + request.Path, "--request-sha256=" + request.Sha256}, &output, &diagnostic); code != 0 {
		t.Fatalf("public plan failed: %s", diagnostic.String())
	}
	plan := bytes.Clone(output.Bytes())
	planPath := filepath.Join(f.metadata, "command-plan.json")
	if err := os.WriteFile(planPath, plan, 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if code := runValidatorStoragePrepare(f.ctx, []string{"apply", "--plan=" + planPath, "--plan-sha256=" + validatorPreparationDigest(plan)}, &output, &diagnostic); code != 0 {
		t.Fatalf("public apply failed: %s", diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized || result.Declaration.Path == "" {
		t.Fatal("public preparation result changed", err, output.String())
	}
	ctx := durablevolume.WithReference(f.ctx, result.Declaration)
	parent, err := openValidatorDurableDirectory(ctx, f.cfg.StateDir, durablevolume.ReadWrite, false)
	if err != nil {
		t.Fatal("prepared parent cannot be retained by production", err)
	}
	if err := parent.Close(); err != nil {
		t.Fatal(err)
	}
	members := []ReleaseActivationMemberV2{}
	noIds := []uint64{}
	for _, operator := range f.cfg.Operators {
		member := ReleaseActivationMemberV2{NoID: operator.NoID, ValidatorUID: 0}
		copy(member.Activation.VPK[:], f.keys[operator.NoID].Public().(ed25519.PublicKey))
		members = append(members, member)
		noIds = append(noIds, operator.NoID)
	}
	admitted, err := requireFreshReleaseOperatorState(ctx, f.cfg, members)
	if err != nil || !slices.Equal(admitted, noIds) {
		t.Fatal("prepared complete census refused fresh activation", admitted, err)
	}
	changed := slices.Clone(members)
	changed[0].ValidatorUID = 1
	if _, err := requireFreshReleaseOperatorState(ctx, f.cfg, changed); err == nil {
		t.Fatal("activation accepted a substituted native UID")
	}
	changed = slices.Clone(members)
	changed[0].Activation.VPK = [32]byte{0x72}
	if _, err := requireFreshReleaseOperatorState(ctx, f.cfg, changed); err == nil {
		t.Fatal("activation accepted a substituted measurement key")
	}
	if _, err := requireFreshReleaseOperatorState(ctx, f.cfg, nil); err == nil {
		t.Fatal("activation accepted an omitted member")
	}
	// Initial inputs were signed outside the fresh root. Activation's new
	// public files belong inside its prepared declaration, outside the state
	// and evidence roles whose approved paths they cannot overlap.
	hotkey, err := loadReleaseHotkey(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := parseHash32("synthetic genesis", f.cfg.GenesisHash)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := parseHash32("synthetic policy", f.cfg.PolicyHash)
	if err != nil {
		t.Fatal(err)
	}
	deployment := ReleaseActivationDeploymentV2{DeploymentID: f.cfg.DeploymentID, ChainID: f.cfg.ChainID, GenesisHash: genesis,
		Netuid: f.cfg.Netuid, Coordinator: [20]byte(common.HexToAddress(f.cfg.Coordinator)),
		SettlementVault: [20]byte(common.HexToAddress(f.cfg.SettlementVault)), PolicyHash: policy}
	snapshot := ReleaseActivationSnapshotV2{Epoch: 9, NativeBlock: 101, NativeHash: [32]byte{0x68}, EVMBlock: 200, EVMHash: [32]byte{6}}
	for index := range members {
		member := &members[index]
		member.Activation, err = BuildFreshReleaseActivationV2(deployment, snapshot, hotkey.PublicKey(), member.NoID, member.Activation.VPK)
		if err != nil {
			t.Fatal(err)
		}
		member.VPKSignature, member.HotkeySignature, err = SignReleaseActivationV2(member.Activation, hotkey, f.keys[member.NoID])
		if err != nil {
			t.Fatal(err)
		}
		if member.Activation.FirstSequence != 1 || member.Activation.PriorRoot != ([32]byte{}) {
			t.Fatal("fresh activation invented predecessor history")
		}
	}
	prepared := ReleaseActivationSetupPreparedV2{Schema: ReleaseActivationSetupPreparedSchemaV2, DeploymentID: f.cfg.DeploymentID,
		ValidatorID: f.cfg.ValidatorID, Netuid: f.cfg.Netuid, PolicyHash: f.cfg.PolicyHash, Epoch: snapshot.Epoch,
		Journal: common.Address{0x33}.Hex(), RuntimeHash: attemptHex32([32]byte{8}),
		Native: ReleaseActivationSetupHeadV2{Number: snapshot.NativeBlock, Hash: attemptHex32(snapshot.NativeHash)},
		EVM:    ReleaseActivationSetupHeadV2{Number: snapshot.EVMBlock, Hash: attemptHex32(snapshot.EVMHash)}, Members: members}
	preparedPath, _ := ReleaseActivationSetupPaths(f.cfg)
	encoded, err := writeReleaseActivationSetupV2(ctx, preparedPath, &prepared, f.cfg.EvidenceV2.Bounds.MaxControlBytes)
	if err != nil {
		t.Fatal("prepared declaration cannot retain activation journal", err)
	}
	completed := ReleaseActivationSetupCompletedV2{Schema: ReleaseActivationSetupCompletedSchemaV2,
		PreparedHash: attemptHex32(sha256.Sum256(encoded)), Boundary: ReleaseActivationSetupHeadV2{Number: 260, Hash: attemptHex32([32]byte{7})}}
	setup := &releaseActivationSetup{cfg: f.cfg, output: &output, limit: f.cfg.EvidenceV2.Bounds.MaxControlBytes}
	rendered, err := setup.renderInputs(ctx, &prepared, &completed)
	if err != nil {
		t.Fatal("prepared declaration cannot publish activation inputs", err)
	}
	activationDirectory := filepath.Join(f.root, "activation")
	options := ReleaseProductionSuccessorOptions{RenderedConfigPath: filepath.Join(activationDirectory, "validator.yml"),
		SuccessorApprovalPath: filepath.Join(activationDirectory, "approval.json"), OriginalAuthorityPath: filepath.Join(activationDirectory, "initial-authority.json"),
		ApprovalKeyFile: f.approvalKeyPath}
	successor, err := buildReleaseProductionSuccessor(ctx, f.cfg, f.configPath, rendered, options, nil)
	if err != nil {
		t.Fatal("generated pending authority cannot render its own successor", err)
	}
	signature, err := successor.approvalSignature()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := successor.publish(ctx, signature); err != nil {
		t.Fatal("prepared declaration cannot publish approved successor", err)
	}
	runtimeConfig, err := LoadReleaseConfig(options.RenderedConfigPath)
	if err != nil || runtimeConfig.ValidatorID != f.cfg.ValidatorID || len(runtimeConfig.ProductionAuthorityHistory) != 1 {
		t.Fatal("rendered generated profile cannot enter the producer", err)
	}
	if _, err := crv4.LoadSeedFile(runtimeConfig.HotkeySeedFile); err != nil {
		t.Fatal("rendered producer lost init hotkey format", err)
	}
	for index, operator := range f.cfg.Operators {
		ledger, err := NewDiskAttemptLedger(ctx, operator.StateDir, releaseActivationLedgerIdentity(f.cfg, members[index]), strings.ToLower(f.cfg.Coordinator), f.keys[operator.NoID], f.cfg.EvidenceV2.Bounds.Disk)
		if err != nil {
			t.Fatal("prepared operator cannot open its actual ledger", err)
		}
		if err := ledger.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Explicit native registration and fresh-root assertions are mandatory. None
// of these refusals may publish a preparation request or create runtime state.
func TestValidatorStoragePublicRequestRefusesAmbiguousInputs(t *testing.T) {
	f := newValidatorStorageCommandTestFixture(t)
	for _, option := range []string{"--fresh", "--validator-uid=0"} {
		args := slices.DeleteFunc(slices.Clone(f.args), func(value string) bool { return value == option })
		var output, diagnostic bytes.Buffer
		if code := runValidatorStoragePrepare(f.ctx, append([]string{"request"}, args...), &output, &diagnostic); code != 1 || output.Len() != 0 {
			t.Fatal("missing explicit request input was accepted", option, code)
		}
	}
	args := slices.Clone(f.args)
	for index, value := range args {
		if value == "--validator-uid=0" {
			args[index] = "--validator-uid=65536"
		}
	}
	var output, diagnostic bytes.Buffer
	if code := runValidatorStoragePrepare(f.ctx, append([]string{"request"}, args...), &output, &diagnostic); code != 1 {
		t.Fatal("out-of-range native UID was accepted")
	}
	if err := os.WriteFile(filepath.Join(f.root, "retained-state"), []byte("synthetic retained state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runValidatorStoragePrepare(f.ctx, append([]string{"request"}, f.args...), &output, &diagnostic); code != 1 {
		t.Fatal("fresh preparation accepted retained runtime state")
	}
	if _, err := os.Stat(f.request); !os.IsNotExist(err) {
		t.Fatal("refused request published metadata", err)
	}
}
