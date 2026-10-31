//go:build linux || darwin

// Synthetic identities exercise the actual setup parser, key reader, treasury
// signature domain and strict pending/producer loaders without live RPC.
package validator

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/crv4"
)

// Shares existing synthetic policy/metadata fixtures, never the public profile's
// live identities. Output, signing keys and absent mutable roots are disjoint.
func configureTestInputs(t *testing.T) (*releaseMainnetProfile, releaseConfigureOptions, releaseConfigureNativeSnapshot) {
	t.Helper()
	fixture := newProductionPendingTestFixture(t, true)
	profile := &releaseMainnetProfile{Schema: "urnetwork-validator-mainnet-profile-v1", ReviewScope: "synthetic profile",
		Config: productionPendingTestClone(t, *fixture.cfg), NativeChain: fixture.approval.NativeChain,
		Proposal: fixture.approval.Proposal, SubnetOwner: fixture.approval.SubnetOwner, OwnerHotkeys: fixture.approval.OwnerHotkeys,
		MaximumSubnetUids: fixture.approval.MaximumSubnetUids, MaximumOwnedHotkeys: fixture.approval.MaximumOwnedHotkeys,
		MaximumLastUpdateAge: fixture.approval.Production.MaximumLastUpdateAge, reviewHash: sha256.Sum256([]byte("synthetic profile bytes"))}
	profile.Config.TreasuryApproval, profile.Config.OwnerRecycleApproval = nil, nil
	profile.Config.ProductionAuthorityHistory, profile.Config.ProductionRuntimeApprovals = nil, nil
	profile.Config.ValidatorID, profile.Config.StateDir, profile.Config.HotkeySeedFile = 0, "", ""
	profile.Config.Operators = profile.Config.Operators[:1]
	profile.Config.EvidenceV2.Operators = nil
	profile.Config.Policy.Safety.MinimumLiveValidatorCount = 1
	profile.Config.Policy.Safety.MinimumHealthyNOCount = 1
	profile.Config.PolicyHash, _ = profile.Config.Policy.HashHex()
	profile.Proposal.ParentPolicyHash, _ = profile.Config.Policy.Hash()
	root := identityTestStateDir(t)
	options := releaseConfigureOptions{StateDir: filepath.Join(root, "runtime"), EvidenceDir: filepath.Join(root, "evidence"),
		HotkeySeedFile: filepath.Join(root, "hotkey.seed"), ClientKeySeedFile: filepath.Join(root, "client.seed"),
		ApprovalKeyFile: filepath.Join(root, "approval.seed"), OutputPath: filepath.Join(root, "validator.yml"), ValidatorId: 73,
		Rpc: "https://evm.example", Substrate: "wss://native.example", ApprovalValidFor: 24 * time.Hour}
	for index, path := range []string{options.HotkeySeedFile, options.ApprovalKeyFile, options.ClientKeySeedFile} {
		if err := os.WriteFile(path, bytes.Repeat([]byte{byte(0xb1 + index)}, 32), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := releaseConfigureNativeSnapshot{Boundary: productionSuccessorBoundary{Epoch: 20, Block: 101, Hash: [32]byte{0x68}}, Tempo: 12}
	return profile, options, snapshot
}

// These are the exact user-facing flags; parser regressions cannot hide behind
// direct options construction in the positive composed test.
func configureTestArgs(options releaseConfigureOptions) []string {
	return []string{"--state-dir=" + options.StateDir, "--evidence-dir=" + options.EvidenceDir,
		"--hotkey-seed-file=" + options.HotkeySeedFile, "--client-key-seed-file=" + options.ClientKeySeedFile,
		"--approval-key-file=" + options.ApprovalKeyFile, "--out=" + options.OutputPath, "--validator-id=73",
		"--rpc=" + options.Rpc, "--substrate=" + options.Substrate, "--approval-valid-for=" + options.ApprovalValidFor.String()}
}

// Loading shared defaults verifies the policy/capacity model and current source
// selection without carrying a production identity into synthetic fixtures.
func TestConfigureBundledMainnetProfileHasNoValidatorAuthority(t *testing.T) {
	profile, err := readReleaseMainnetProfile()
	if err != nil {
		t.Fatal(err)
	}
	if profile.Config.SettlementV2 == nil || profile.Proposal.Treasury == nil || profile.reviewHash != sha256.Sum256(releaseMainnetProfileBytes) ||
		profile.Config.HotkeySeedFile != "" || profile.Config.StateDir != "" || profile.Config.ValidatorID != 0 || productionEconomicSelection(&profile.Config) != nil {
		t.Fatal("public profile inherited a validator identity or lost its shared v2/economic terms")
	}
	profile.Config.Operators[0].APIURL = "https://changed.example"
	again, err := readReleaseMainnetProfile()
	if err != nil || again.Config.Operators[0].APIURL == profile.Config.Operators[0].APIURL {
		t.Fatal("a caller mutated the embedded defaults", err)
	}
}

// Loader admission permits zero for historical planning. The shipped runnable
// profile must instead open the actual authenticated runtime upload owner.
func TestConfigureBundledProfileOpensRuntimeUploadSource(t *testing.T) {
	profile, err := readReleaseMainnetProfile()
	if err != nil {
		t.Fatal(err)
	}
	fixture := newReleaseStartupV2TestFixture(t, false)
	fixture.cfg.EvidenceV2.UploadIntentSeconds = profile.Config.EvidenceV2.UploadIntentSeconds
	source, err := newReleaseAttemptUploadSourceV2(&fixture.cfg, fixture.inputs[0])
	if err != nil || source == nil {
		t.Fatal("bundled profile cannot start authenticated protected uploads", err)
	}
}

// Two independent signatures are not inherited from an existing validator:
// this command's approval key binds this hotkey, ID, paths and public policy.
func TestConfigureMainnetWritesOwnSignedActivationPendingConfig(t *testing.T) {
	profile, options, snapshot := configureTestInputs(t)
	before := productionPendingTestClone(t, *profile)
	var output, errorOutput bytes.Buffer
	reads := 0
	readSnapshot := func(ctx context.Context, selected *releaseMainnetProfile, endpoint string) (releaseConfigureNativeSnapshot, error) {
		reads++
		if _, finite := ctx.Deadline(); !finite || selected != profile || endpoint != options.Substrate {
			t.Fatal("native read lost its finite caller or selected public profile/endpoint")
		}
		return snapshot, nil
	}
	if code := runConfigureCommandWithProfile(t.Context(), configureTestArgs(options), &output, &errorOutput, profile, readSnapshot); code != 0 {
		t.Fatalf("configure exit %d: %s", code, errorOutput.String())
	}
	if reads != 1 || !reflect.DeepEqual(before.Config, profile.Config) || !strings.Contains(output.String(), "activation pending") {
		t.Fatal("setup reread native state, mutated shared defaults or concealed pending activation")
	}
	cfg, err := LoadReleaseProductionConfigPreActivation(options.OutputPath)
	if err != nil {
		t.Fatal("generated config failed actual pending production admission", err)
	}
	approved, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crv4.KeypairFromSeed([32]byte(bytes.Repeat([]byte{0xb1}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	approvalKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0xb2}, 32))
	if cfg.ValidatorID != options.ValidatorId || cfg.TreasuryApproval.Signer != attemptHex32([32]byte(approvalKey.Public().(ed25519.PublicKey))) ||
		approved.Approval.ValidatorHotkey != key.PublicKey() || !slices.Equal(approved.Approval.Production.ValidatorHotkeys, [][32]byte{key.PublicKey()}) ||
		!cfg.Operators[0].AllowClientRegistration || cfg.Operators[0].ClientKeySeedFile != options.ClientKeySeedFile ||
		len(cfg.ProductionAuthorityHistory) != 0 || len(cfg.ProductionRuntimeApprovals) != 0 {
		t.Fatal("generated approval inherited another identity/history or lost independent client onboarding")
	}
	if !reflect.DeepEqual(cfg.Policy, profile.Config.Policy) || !reflect.DeepEqual(approved.Approval.Proposal, profile.Proposal) ||
		approved.Approval.RuntimeReviewHash != profile.reviewHash || approved.Approval.FirstNativeEpoch != snapshot.Boundary.Epoch ||
		approved.Approval.ValidFromNativeBlock != snapshot.Boundary.Block || approved.Approval.Production.ActivationNativeHash != snapshot.Boundary.Hash {
		t.Fatal("generated authority changed economic terms or invented an initial native boundary")
	}
	if _, err := LoadReleaseConfig(options.OutputPath); !errors.Is(err, ErrReleaseProductionActivationPending) {
		t.Fatal("pending setup config gained producer admission", err)
	}
	for _, path := range []string{options.StateDir, options.EvidenceDir} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("configure created mutable runtime state", path, err)
		}
	}
	for _, path := range []string{options.OutputPath, options.OutputPath + ".treasury-approval.json"} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatal("generated signed input is not a private regular file", err)
		}
	}
}

// Invalid CLI values are rejected before key reads or network calls, including
// explicit empties that must not silently choose a bundled default.
func TestConfigureRejectsInvalidAndEmptyOptionsBeforeNativeRead(t *testing.T) {
	profile, options, _ := configureTestInputs(t)
	for _, changed := range []string{"--validator-id=0", "--validator-id=-1", "--approval-valid-for=", "--approval-valid-for=0s", "--approval-valid-for=-1m", "--state-dir=relative", "--evidence-dir=", "--hotkey-seed-file=", "--client-key-seed-file=", "--approval-key-file=", "--rpc=", "--rpc=wss://wrong.example", "--substrate=", "--substrate=https://wrong.example", "--out="} {
		var output, errorOutput bytes.Buffer
		called := false
		args := append(configureTestArgs(options), changed)
		code := runConfigureCommandWithProfile(t.Context(), args, &output, &errorOutput, profile, func(context.Context, *releaseMainnetProfile, string) (releaseConfigureNativeSnapshot, error) {
			called = true
			return releaseConfigureNativeSnapshot{}, errors.New("unexpected native read")
		})
		if code != 2 || called || output.Len() != 0 {
			t.Errorf("%s: exit=%d called=%t output=%s error=%s", changed, code, called, output.String(), errorOutput.String())
		}
	}
}

// Existing output and key-role aliases are rejected before native reads, so a
// failed setup command cannot replace an earlier signed config or reuse a key.
func TestConfigureRefusesOverwriteAndKeyRoleAliasing(t *testing.T) {
	profile, options, _ := configureTestInputs(t)
	if err := os.WriteFile(options.OutputPath, []byte("prior signed config"), 0o600); err != nil {
		t.Fatal(err)
	}
	priorPath := options.OutputPath
	options.OutputPath = filepath.Join(filepath.Dir(priorPath), "fresh.yml")
	for _, changed := range []string{"--out=" + priorPath, "--client-key-seed-file=" + options.ApprovalKeyFile, "--evidence-dir=" + options.StateDir, "--hotkey-seed-file=" + options.ApprovalKeyFile} {
		var output, errorOutput bytes.Buffer
		if code := runConfigureCommandWithProfile(t.Context(), append(configureTestArgs(options), changed), &output, &errorOutput, profile,
			func(context.Context, *releaseMainnetProfile, string) (releaseConfigureNativeSnapshot, error) {
				t.Fatal("invalid output or key alias reached native RPC")
				return releaseConfigureNativeSnapshot{}, nil
			}); code != 2 {
			t.Errorf("%s exit=%d: %s", changed, code, errorOutput.String())
		}
	}
	raw, err := os.ReadFile(priorPath)
	if err != nil || string(raw) != "prior signed config" {
		t.Fatal("setup replaced an earlier output", err)
	}
}

// A failed/canceled observation leaves both mutable state and public outputs
// untouched. Cancellation still belongs to the caller, not a detached setup.
func TestConfigureNativeFailureCreatesNoOutputs(t *testing.T) {
	profile, options, _ := configureTestInputs(t)
	var output, errorOutput bytes.Buffer
	if code := runConfigureCommandWithProfile(t.Context(), configureTestArgs(options), &output, &errorOutput, profile,
		func(context.Context, *releaseMainnetProfile, string) (releaseConfigureNativeSnapshot, error) {
			return releaseConfigureNativeSnapshot{}, context.DeadlineExceeded
		}); code != 1 || !strings.Contains(errorOutput.String(), context.DeadlineExceeded.Error()) {
		t.Fatal("failed native observation became a signed config", code, errorOutput.String())
	}
	for _, path := range []string{options.OutputPath, options.OutputPath + ".treasury-approval.json", options.StateDir, options.EvidenceDir} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed setup created output/state", path, err)
		}
	}
}

// Finite block authority cannot wrap, even if a caller requests a very long
// duration. Epoch count is a conservative ceiling; the block ceiling still ends.
func TestConfigureFiniteApprovalWindowAndOverflow(t *testing.T) {
	profile, options, snapshot := configureTestInputs(t)
	settings := defaultReleaseConfigureSettings()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0xb2}, 32))
	cfg, raw, err := buildConfiguredRelease(options, settings, profile, snapshot, [32]byte{0xd4}, private)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := VerifyTreasuryApproval(raw, [32]byte(private.Public().(ed25519.PublicKey)), attemptHex32(sha256.Sum256(raw)))
	if err != nil || cfg.TreasuryApproval == nil {
		t.Fatal(err)
	}
	blocks := uint64(options.ApprovalValidFor / settings.NativeBlockTime)
	if approved.Approval.ValidThroughNativeBlock != snapshot.Boundary.Block+blocks || approved.Approval.Production.ValidThroughNativeEpoch != snapshot.Boundary.Epoch+blocks {
		t.Fatal("approval omitted or extended its explicit finite window")
	}
	snapshot.Boundary.Block = uint64(^uint32(0))
	if _, _, err := buildConfiguredRelease(options, settings, profile, snapshot, [32]byte{0xd4}, private); err == nil {
		t.Fatal("native approval block ceiling wrapped")
	}
}
