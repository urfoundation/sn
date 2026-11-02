//go:build linux || darwin

// Fresh operators use the public commands without copying another validator's
// credentials or converting an approved production document into legacy input.
package validator

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urfoundation/sn/clientauth"
)

// Init may provision keys from an approved activation-pending production file;
// the producer still refuses that file until activation renders its evidence.
func TestOnboardingInitAcceptsSignedProductionConfig(t *testing.T) {
	f := newProductionPendingTestFixture(t, false)
	opts, err := parseValidatorArgsForTest(t, []string{"init", "--config=" + f.path})
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(t.TempDir(), "init-output-")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := runInit(opts, output); err != nil {
		t.Fatal("signed production setup did not reach key provisioning", err)
	}
	for _, operator := range f.cfg.Operators {
		if _, err := loadClientSeed(operator.ClientKeySeedFile); err != nil {
			t.Fatal("configured operator key was not provisioned", err)
		}
	}
	if _, err := LoadReleaseConfig(f.path); err == nil {
		t.Fatal("setup made an activation-pending profile runnable")
	}
}

// The release's exact operator credential path is distinct from both the
// global login and the observation-only operator-list directory.
func onboardingAuthTestProfile(t *testing.T) (*productionPendingTestFixture, string, string, *operatorTestSignIn) {
	t.Helper()
	signIn, server := newOperatorTestSignIn(t)
	f := newProductionPendingTestFixture(t, false)
	seedPath := filepath.Join(identityTestStateDir(t), "hotkey.seed")
	writeOperatorTestHotkeySeed(t, seedPath, 13)
	hotkey := operatorTestHotkey(t, 13).PublicKey()
	path := f.variant(t, func(cfg *ReleaseConfig, approval *OwnerRecycleApproval) {
		cfg.HotkeySeedFile = seedPath
		cfg.Operators[0].APIURL = server.URL
		approval.ValidatorHotkey = hotkey
		approval.Production.ValidatorHotkeys = append(approval.Production.ValidatorHotkeys, hotkey)
		slices.SortFunc(approval.Production.ValidatorHotkeys, func(left, right [32]byte) int { return bytes.Compare(left[:], right[:]) })
	})
	return f, path, seedPath, signIn
}

// The public auth command selects the same path the runtime consumes.
func TestOnboardingAuthTargetsConfiguredOperator(t *testing.T) {
	f, path, _, signIn := onboardingAuthTestProfile(t)
	opts, err := parseValidatorArgsForTest(t, []string{"auth", "--config=" + path, "--no_id=1", "-f"})
	if err != nil {
		t.Fatal("configured production authentication is unavailable", err)
	}
	output, err := runOperatorAuthTest(t, opts, "")
	if err != nil {
		t.Fatal(err)
	}
	jwtPath := f.cfg.Operators[0].NetworkJWTFile
	if token, err := clientauth.ReadToken(jwtPath); err != nil || token == "" || signIn.networkCount() != 1 || !strings.Contains(output, jwtPath) {
		t.Fatal("configured credential was not durably provisioned", err, output)
	}
	if _, err := os.Stat(f.cfg.Operators[1].NetworkJWTFile); !os.IsNotExist(err) {
		t.Fatal("authentication touched another operator's credential", err)
	}
}

// A wrong key, an unknown NO and explicit empty config must fail before any
// sign-in or credential publication, never falling back to global auth.
func TestOnboardingAuthRefusesWrongIdentityOrOperator(t *testing.T) {
	f, path, seedPath, signIn := onboardingAuthTestProfile(t)
	for _, args := range [][]string{
		{"auth", "--config=" + path, "--no_id=77", "-f"},
		{"auth", "--config=", "--no_id=1", "-f"},
		{"auth", "--config=" + path, "--no_id=0", "-f"},
	} {
		opts, err := parseValidatorArgsForTest(t, args)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runOperatorAuthTest(t, opts, ""); err == nil {
			t.Fatal("invalid configured auth selected a target", args)
		}
	}
	writeOperatorTestHotkeySeed(t, seedPath, 14)
	opts, err := parseValidatorArgsForTest(t, []string{"auth", "--config=" + path, "--no_id=1", "-f"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runOperatorAuthTest(t, opts, ""); err == nil || !strings.Contains(err.Error(), "differs from the approved") {
		t.Fatal("another hotkey authenticated the production profile", err)
	}
	if signIn.networkCount() != 0 {
		t.Fatal("refused authentication reached the API")
	}
	for _, operator := range f.cfg.Operators {
		if _, err := os.Stat(operator.NetworkJWTFile); !os.IsNotExist(err) {
			t.Fatal("refused authentication wrote credentials", err)
		}
	}
}

// The approval key is a named private file; hand-converting its signature into
// a CLI argument must not be required for fresh production activation.
func TestOnboardingActivationAcceptsApprovalKeyOption(t *testing.T) {
	opts, err := parseValidatorArgsForTest(t, []string{"activate", "--config=/synthetic/release.yml", "--rendered-config=/synthetic/rendered.yml", "--successor-approval=/synthetic/approval.json", "--original-authority=/synthetic/original.json", "--approval-key-file=/synthetic/approval.key", "--apply"})
	if err != nil {
		t.Fatal("activation cannot select its approval key", err)
	}
	if options := releaseProductionSuccessorOptionsFromOpts(opts); options == nil {
		t.Fatal("approval key did not select production successor options")
	}
}

// A raw private approval seed signs the unchanged successor message. The
// ordinary publisher and strict loader still verify the resulting lineage.
func TestOnboardingActivationKeyPublishesStrictSuccessor(t *testing.T) {
	f := newProductionPendingTestFixture(t, false)
	keyPath := filepath.Join(identityTestStateDir(t), "approval.key")
	if err := os.WriteFile(keyPath, f.private.Seed(), 0o600); err != nil {
		t.Fatal(err)
	}
	options := productionPendingTestOptions(t)
	options.ApprovalKeyFile = keyPath
	successor, err := buildReleaseProductionSuccessor(t.Context(), f.cfg, f.path, productionPendingTestRender(t, f.cfg), options, nil)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := successor.approvalSignature()
	if err != nil {
		t.Fatal(err)
	}
	rawSignature, err := hex.DecodeString(signature)
	if err != nil || !ed25519.Verify(f.private.Public().(ed25519.PublicKey), successor.message, rawSignature) {
		t.Fatal("approval key signed a different message", err)
	}
	if _, err := successor.publish(t.Context(), signature); err != nil {
		t.Fatal("key-signed successor failed ordinary publication", err)
	}
	if _, err := LoadReleaseConfig(options.RenderedConfigPath); err != nil {
		t.Fatal("key-signed successor failed the strict producer loader", err)
	}
	raw, err := os.ReadFile(options.RenderedConfigPath)
	if err != nil || bytes.Contains(raw, []byte(keyPath)) || bytes.Contains(raw, []byte(hex.EncodeToString(f.private.Seed()))) {
		t.Fatal("rendered configuration retained the offline approval key", err)
	}
}

// A caller cannot substitute a new approval root, use both signature sources,
// or silently treat an explicitly empty key argument as omission.
func TestOnboardingActivationRefusesWrongOrAmbiguousKey(t *testing.T) {
	f := newProductionPendingTestFixture(t, false)
	options := productionPendingTestOptions(t)
	options.ApprovalKeyFile = filepath.Join(identityTestStateDir(t), "wrong.key")
	if err := os.WriteFile(options.ApprovalKeyFile, bytes.Repeat([]byte{0x19}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	successor, err := buildReleaseProductionSuccessor(t.Context(), f.cfg, f.path, productionPendingTestRender(t, f.cfg), options, nil)
	if err != nil {
		t.Fatal(err)
	}
	if signature, err := successor.approvalSignature(); err == nil || signature != "" {
		t.Fatal("a different approval key signed the successor")
	}
	for _, path := range []string{options.RenderedConfigPath, options.SuccessorApprovalPath, options.OriginalAuthorityPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("wrong approval key published successor material", err)
		}
	}
	options.ApprovalSignature = strings.Repeat("ab", 64)
	if err := options.validate(); err == nil {
		t.Fatal("two approval signature sources were accepted")
	}
	opts, err := parseValidatorArgsForTest(t, []string{"activate", "--config=/synthetic/release.yml", "--rendered-config=/synthetic/rendered.yml", "--successor-approval=/synthetic/approval.json", "--original-authority=/synthetic/original.json", "--approval-key-file=", "--apply"})
	if err != nil {
		t.Fatal(err)
	}
	if err := releaseProductionSuccessorOptionsFromOpts(opts).validate(); err == nil {
		t.Fatal("an empty approval-key argument became unsigned activation")
	}
}

// Changing the signed production document cannot reach init merely because the
// setup entrypoint admits an unrendered evidence census.
func TestOnboardingBootstrapKeepsSignedProductionAuthority(t *testing.T) {
	f := newProductionPendingTestFixture(t, false)
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(raw, []byte(fmt.Sprintf("poll_seconds: %d", f.cfg.PollSeconds)), []byte(fmt.Sprintf("poll_seconds: %d", f.cfg.PollSeconds+1)), 1)
	if bytes.Equal(raw, changed) {
		t.Fatal("synthetic mutation did not change the signed document")
	}
	if err := os.WriteFile(f.path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := loadReleaseBootstrapCommandConfig(f.path); err == nil || cfg != nil {
		t.Fatal("changed production configuration reached bootstrap")
	}
	current := newProductionRuntimeTestFixture(t, false)
	if cfg, err := loadReleaseBootstrapCommandConfig(current.path); err != nil || cfg.ValidatorID != current.cfg.ValidatorID {
		t.Fatal("a rendered production configuration failed bootstrap", err)
	}
}

// Key preparation is independent of an empty future runtime root. Repeating it
// must reuse all identities and print only the approval key's public signer.
func TestOnboardingInitCreatesAndReusesApprovalKey(t *testing.T) {
	root := identityTestStateDir(t)
	state := filepath.Join(root, "keys")
	approvalPath := filepath.Join(root, "approval.key")
	opts, err := parseValidatorArgsForTest(t, []string{"init", "--state_dir=" + state, "--no_id=1", "--approval-key-file=" + approvalPath})
	if err != nil {
		t.Fatal("init cannot prepare the offline approval key", err)
	}
	output, err := os.CreateTemp(root, "init-output-")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := runInit(opts, output); err != nil {
		t.Fatal(err)
	}
	paths := []string{approvalPath, filepath.Join(state, "hotkey.seed"), filepath.Join(state, "no-1", "client.key")}
	originals := make([][]byte, len(paths))
	for index, path := range paths {
		originals[index], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	private, err := loadApprovalKeyFile(approvalPath)
	if err != nil || len(originals[0]) != 32 {
		t.Fatal("init did not create a private approval seed", err)
	}
	if err := runInit(opts, output); err != nil {
		t.Fatal(err)
	}
	for index, path := range paths {
		if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, originals[index]) {
			t.Fatal("init replaced a provisioned identity", path, err)
		}
	}
	raw, err := os.ReadFile(output.Name())
	if err != nil || !bytes.Contains(raw, []byte(hex.EncodeToString(private.Public().(ed25519.PublicKey)))) || bytes.Contains(raw, []byte(hex.EncodeToString(private.Seed()))) {
		t.Fatal("init omitted its public signer or exposed private seed bytes", err)
	}
}

// Explicitly unsafe or empty approval-key selections fail without replacing
// an existing key; they never silently select another generated identity.
func TestOnboardingInitRefusesSharedOrEmptyApprovalKey(t *testing.T) {
	root := identityTestStateDir(t)
	path := filepath.Join(root, "approval.key")
	before := bytes.Repeat([]byte{0x29}, 32)
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(root, "init-output-")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	for _, keyPath := range []string{path, ""} {
		opts, err := parseValidatorArgsForTest(t, []string{"init", "--state_dir=" + filepath.Join(root, "keys"), "--no_id=1", "--approval-key-file=" + keyPath})
		if err != nil {
			t.Fatal(err)
		}
		if err := runInit(opts, output); err == nil {
			t.Fatal("invalid approval key selection was admitted")
		}
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
		t.Fatal("refused approval key changed", err)
	}
}

// A caller may already hold the documented hex encoding; init must reuse it
// rather than replacing it or demanding a manual private-key conversion.
func TestOnboardingInitReusesHexApprovalKey(t *testing.T) {
	root := identityTestStateDir(t)
	path := filepath.Join(root, "approval.key")
	before := []byte(strings.Repeat("71", 32) + "\n")
	if err := os.WriteFile(path, before, 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(root, "init-output-")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := initApprovalKeySeed(path, output); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
		t.Fatal("init replaced an existing hex approval seed", err)
	}
}

// Status may inspect an approved pending profile before keys or runtime
// storage exist. Missing identity stays a reported observation, not a panic.
func TestOnboardingStatusReadsPendingProductionBeforeKeyProvisioning(t *testing.T) {
	f := newProductionPendingTestFixture(t, false)
	seedPath := filepath.Join(identityTestStateDir(t), "not-provisioned.seed")
	path := f.variant(t, func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) { cfg.HotkeySeedFile = seedPath })
	statusRelease(path)
	if _, err := os.Stat(seedPath); !os.IsNotExist(err) {
		t.Fatal("status provisioned a missing signing identity", err)
	}
}
