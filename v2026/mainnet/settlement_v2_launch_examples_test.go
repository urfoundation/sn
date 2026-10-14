// The settlement v2 launch examples (VERSION2-LAUNCH.md steps 8.2, 8.3 and
// 10), their placeholders filled with synthetic values, through the real
// commands. Nothing here signs a transaction or opens a route.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A placeholder is one quoted "<...>" string, a number's included.
var launchExamplePlaceholder = regexp.MustCompile(`"<[^"<>]+>"`)

// The runbook keeps the v2 launch files under /secure/ur-mainnet; the tests
// move that layout into a temporary directory.
const launchExampleSecure = "/secure/ur-mainnet"

// launchExample reads an example and requires its placeholders to be exactly
// the keys of values. It replaces each but skip by the JSON of its value and
// moves the runbook's layout under root.
func launchExample(t *testing.T, name, root string, values map[string]any, skip string) []byte {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, token := range launchExamplePlaceholder.FindAll(raw, -1) {
		found[string(token[2:len(token)-2])] = true
	}
	for key := range values {
		if !found[key] {
			t.Fatalf("%s has no placeholder <%s>", name, key)
		}
	}
	if len(found) != len(values) {
		t.Fatalf("%s has placeholders %v; the test fills %d", name, found, len(values))
	}
	filled := launchExamplePlaceholder.ReplaceAllFunc(raw, func(token []byte) []byte {
		key := string(token[2 : len(token)-2])
		if key == skip {
			return token
		}
		encoded, err := json.Marshal(values[key])
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	})
	return bytes.ReplaceAll(filled, []byte(launchExampleSecure), []byte(root))
}

// One private input of a filled example.
func writeLaunchFile(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// The phase example's synthetic owner inputs: one deployer from nonce 12, the
// escrow hotkey and burn cap, the decided demand fee, the Safe, the v1
// coordinator, one auditor at quorum one and the claim batcher (O11). The
// derived fields are built as SETTLEMENT-V2-EXAMPLES.md describes.
type launchPhase struct {
	root               string
	approval           ed25519.PrivateKey
	deployer           common.Address
	nonce, burnCap     uint64
	hotkey             [32]byte
	fee                *big.Int
	owner, v1, auditor common.Address
	values             map[string]any
}

func newLaunchPhase(t *testing.T, root string) *launchPhase {
	t.Helper()
	release := evmV2TestRelease(t)
	raw, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	writeLaunchFile(t, filepath.Join(root, "settlement-v2-contracts.json"), raw)
	seed := sha256.Sum256([]byte("settlement v2 launch example approval"))
	p := &launchPhase{root: root, approval: ed25519.NewKeyFromSeed(seed[:]), deployer: common.Address{0xd4}, nonce: 12, burnCap: 100_000_000_000, hotkey: [32]byte{0xe5},
		fee: big.NewInt(750_000_000_000_000), owner: common.Address{0x08}, v1: common.Address{0x09}, auditor: common.Address{0x10}}
	vault, implementation, proxy := crypto.CreateAddress(p.deployer, p.nonce), crypto.CreateAddress(p.deployer, p.nonce+2), crypto.CreateAddress(p.deployer, p.nonce+3)
	initializer := stabi.NewSTSettlement2().PackInitialize(p.owner, p.v1, vault, []common.Address{p.auditor}, big.NewInt(1))
	funding := new(big.Int).Mul(new(big.Int).SetUint64(p.burnCap), big.NewInt(1_000_000_000))
	feeCap := big.NewInt(30_000_000_000)
	total := new(big.Int).Add(funding, new(big.Int).Mul(big.NewInt(4_000_000+2_500_000+3_000_000+1_000_000+150_000+400_000), feeCap))
	hash := func(fill byte) string { return "0x" + strings.Repeat(hex.EncodeToString([]byte{fill}), 32) }
	pin := func(fill byte) string { return "sha256:" + strings.Repeat(hex.EncodeToString([]byte{fill}), 32) }
	digest := sha256.Sum256(raw)
	p.values = map[string]any{
		"approval-public-key-ed25519": "0x" + hex.EncodeToString(p.approval.Public().(ed25519.PublicKey)),
		"O2-genesis-hash":             hash(0x61),
		"O2-evm-rpc-url":              "http://127.0.0.1:9944",
		"O3-runtime-source-commit":    crv4.NativeOwnerSource475,
		"O3-spec-version":             475,
		"O3-runtime-code-hash":        hash(0x62),
		"O3-runtime-metadata-hash":    hash(0x63),
		"release-file-sha256":         "sha256:" + hex.EncodeToString(digest[:]),
		"O7-source-lock-hash":         pin(0x71),
		"O7-custody-id":               "launch-example-deployer",
		"O7-custody-fence-hash":       pin(0x72),
		"O7-cutover-evidence-hash":    pin(0x73),
		"O7-start-native-number":      9_500_000,
		"O7-start-native-hash":        hash(0x74),
		"O7-valid-through-native":     9_507_200,
		"O7-maximum-total-wei":        total.String(),
		"O7-fee-cap-wei":              feeCap.String(),
		"O7-tip-cap-wei":              "1000000000",
		"O4-deployer-address":         strings.ToLower(p.deployer.Hex()),
		"O4-nonce":                    p.nonce,
		"O4-nonce+1":                  p.nonce + 1,
		"O4-nonce+2":                  p.nonce + 2,
		"O4-nonce+3":                  p.nonce + 3,
		"O4-nonce+4":                  p.nonce + 4,
		"O4-nonce+5":                  p.nonce + 5,
		"O6-escrow-value-wei":         funding.String(),
		"vault2-address":              strings.ToLower(vault.Hex()),
		"vault2-create-data":          evmV2TestVaultData(t, release, 25, p.hotkey, ss58.EvmMirrorPubkey(vault), p.deployer, p.fee),
		"escrow2-register-data":       "0x" + hex.EncodeToString(stabi.NewSTSettlementVault2().PackRegisterEscrow(p.burnCap)),
		"settlement2-create-data":     "0x" + evmV2TestArtifactNamed(t, release, "Settlement2").Creation,
		"proxy2-create-data":          evmV2TestProxyData(t, release, implementation, initializer),
		"vault2-link-data":            "0x" + hex.EncodeToString(stabi.NewSTSettlementVault2().PackSetCoordinatorOnce(proxy)),
		"batcher-create-data":         "0x" + evmV2TestArtifactNamed(t, release, "ClaimBatcher").Creation,
	}
	return p
}

// The unsigned draft with every placeholder but skip filled.
func (self *launchPhase) write(t *testing.T, skip string) string {
	t.Helper()
	path := filepath.Join(self.root, "settlement-v2.unsigned.json")
	writeLaunchFile(t, path, launchExample(t, "settlement-v2.unsigned.example.json", self.root, self.values, skip))
	return path
}

// The filled draft with the synthetic approval key's signature, saved where
// step 8.3 saves the signed config, and its plan hash.
func (self *launchPhase) sign(t *testing.T) (string, string) {
	t.Helper()
	var config evmPhaseConfig
	if err := decodePlanJson(launchExample(t, "settlement-v2.unsigned.example.json", self.root, self.values, ""), &config); err != nil {
		t.Fatal(err)
	}
	message, err := config.Plan.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	config.Signature = hex.EncodeToString(ed25519.Sign(self.approval, message))
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(self.root, "settlement-v2.json")
	writeLaunchFile(t, path, raw)
	return path, config.Plan.hash()
}

// SV-8, step 8.3: the unsigned v2 phase example, filled, passes preview with
// exactly the owner inputs as its review values, and once signed passes plan
// with the same plan hash. Preview refuses the unfilled example and every
// single placeholder left in it, so none can reach the approval, and it
// refuses a draft that is not private to its owner (umask 077).
func TestSettlementV2LaunchPhaseConfigExample(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	p := newLaunchPhase(t, root)
	command := func(args ...string) (int, []byte, string) {
		var stdout, stderr bytes.Buffer
		code := runMain(t.Context(), append([]string{"bootstrap-contracts"}, args...), &stdout, &stderr)
		return code, stdout.Bytes(), stderr.String()
	}
	raw, err := os.ReadFile("settlement-v2.unsigned.example.json")
	if err != nil {
		t.Fatal(err)
	}
	unfilled := filepath.Join(root, "unfilled.json")
	writeLaunchFile(t, unfilled, bytes.ReplaceAll(raw, []byte(launchExampleSecure), []byte(root)))
	if code, _, diagnostic := command("preview", "--action", "vault2-create", "--config", unfilled); code != 2 || !strings.HasPrefix(diagnostic, "contract phase unsigned preview: ") {
		t.Fatalf("unfilled example previewed: %d %s", code, diagnostic)
	}
	code, output, diagnostic := command("preview", "--action", "vault2-create", "--config", p.write(t, ""))
	var preview evmV2PhasePreview
	if code != 0 || json.Unmarshal(output, &preview) != nil {
		t.Fatalf("filled example refused: %d %s", code, diagnostic)
	}
	vault, implementation, proxy := crypto.CreateAddress(p.deployer, p.nonce), crypto.CreateAddress(p.deployer, p.nonce+2), crypto.CreateAddress(p.deployer, p.nonce+3)
	initializer := contractSettlement2Initializer{Owner: p.owner, V1Coordinator: p.v1, Vault: vault, Auditors: []common.Address{p.auditor}, Quorum: 1}
	if preview.Vault2Constructor == nil || preview.EscrowRegistration == nil || preview.Settlement2Initializer == nil || preview.VaultBinding == nil ||
		preview.Vault2.Address != vault.Hex() || preview.Settlement2Implementation.Address != implementation.Hex() || preview.Settlement2.Address != proxy.Hex() || preview.ClaimBatcher == nil || preview.ClaimBatcher.Address != crypto.CreateAddress(p.deployer, p.nonce+5).Hex() ||
		*preview.Vault2Constructor != (contractVault2Constructor{EscrowHotkey: common.Hash(p.hotkey).Hex(), DemandFeeWei: "750000000000000"}) ||
		preview.EscrowRegistration.MaximumBurnRao != p.burnCap || preview.EscrowRegistration.FundingWei != p.values["O6-escrow-value-wei"] ||
		!reflect.DeepEqual(*preview.Settlement2Initializer, initializer) || *preview.VaultBinding != (contractVaultBinding{Vault: vault, Coordinator: proxy}) {
		t.Fatalf("preview review values differ from the owner inputs: %+v", preview)
	}
	for key := range p.values {
		if code, _, diagnostic := command("preview", "--action", "vault2-create", "--config", p.write(t, key)); code != 2 || !strings.HasPrefix(diagnostic, "contract phase unsigned preview: ") {
			t.Errorf("preview admitted the example with <%s> unfilled: %d %s", key, code, diagnostic)
		}
	}
	draft := p.write(t, "")
	if err := os.Chmod(draft, 0640); err != nil {
		t.Fatal(err)
	}
	if code, _, diagnostic := command("preview", "--action", "vault2-create", "--config", draft); code != 2 || !strings.Contains(diagnostic, "bootstrap root input is not a bounded private regular file") {
		t.Fatalf("preview read a draft that its group can read: %d %s", code, diagnostic)
	}
	if err := os.Mkdir(filepath.Join(root, "run-v2"), 0700); err != nil {
		t.Fatal(err)
	}
	signed, hash := p.sign(t)
	code, output, diagnostic = command("plan", "--action", "vault2-create", "--config", signed)
	var plan struct {
		PlanHash string `json:"plan_hash"`
	}
	if code != 0 || json.Unmarshal(output, &plan) != nil || plan.PlanHash != hash || preview.PlanHash != hash {
		t.Fatalf("signed example refused by plan: %d %s %s", code, output, diagnostic)
	}
}

// SV-8, step 8.2: the daemon-scope custody request and its fence, filled with
// a synthetic mount, pass storage-prepare plan, which stages one empty
// mainnet-evm-action snapshot for each v2 journal in the phase example's run
// directory. Applied, its declaration lets offline bootstrap-contracts apply
// open vault2's journal from the signed phase example. Plan refuses a staging
// directory that is not private, an ancestor its group can write (umask 077
// gives neither) and a request digest without its sha256: prefix.
func TestSettlementV2LaunchCustodyRequestExample(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"observation", "run-v2", "run-v2-custody", "run-v2-staging"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	physical := durablefixture.New(t, t.Context(), filepath.Join(parent, "observation"))
	declaration, err := durablevolume.Load(physical.Reference)
	if err != nil {
		t.Fatal(err)
	}
	var root syscall.Stat_t
	if err := syscall.Stat(filepath.Join(parent, "run-v2"), &root); err != nil {
		t.Fatal(err)
	}
	fence := launchExample(t, "settlement-v2-custody-fence.example.json", parent, map[string]any{"root-inode": root.Ino, "fence-evidence": "synthetic directory created empty by this test; no writer ever had it"}, "")
	writeLaunchFile(t, filepath.Join(parent, "run-v2-custody", "former-writer.json"), fence)
	volume := declaration.Volumes[0]
	values := map[string]any{"mount-path": volume.MountPath, "filesystem-uuid": volume.FilesystemUuid, "filesystem-type": volume.FilesystemType, "fence-file-sha256": durablefixture.Digest(fence)}
	request := launchExample(t, "settlement-v2-custody-request.example.json", parent, values, "")
	requestPath := filepath.Join(parent, "run-v2-preparation-request.json")
	writeLaunchFile(t, requestPath, request)
	prepare := func(args ...string) []byte {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := runMain(physical.Context, append([]string{"storage-prepare"}, args...), &stdout, &stderr); code != 0 {
			t.Fatalf("storage-prepare %s refused the example: %d %s", args[0], code, stderr.String())
		}
		return stdout.Bytes()
	}
	for _, refusal := range []struct {
		path, want, digest string
		mode               os.FileMode
	}{
		{path: filepath.Join(parent, "run-v2-staging"), mode: 0750, digest: durablefixture.Digest(request), want: "preparation path must be private and owned by the applying identity"},
		{path: parent, mode: 0770, digest: durablefixture.Digest(request), want: "is not a protected physical object"},
		{path: parent, mode: 0700, digest: strings.TrimPrefix(durablefixture.Digest(request), "sha256:"), want: "request path and exact hash are required"},
	} {
		if err := os.Chmod(refusal.path, refusal.mode); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if code := runMain(physical.Context, []string{"storage-prepare", "plan", "--request", requestPath, "--request-sha256", refusal.digest}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), refusal.want) {
			t.Fatalf("storage-prepare plan with %s at %o: %d %s", refusal.path, refusal.mode, code, stderr.String())
		}
		if err := os.Chmod(refusal.path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	planned := prepare("plan", "--request", requestPath, "--request-sha256", durablefixture.Digest(request))
	var plan durablevolume.PreparationPlan
	if err := json.Unmarshal(planned, &plan); err != nil || plan.RestartAuthorized {
		t.Fatalf("plan: %v", err)
	}
	names := []string{}
	for _, owner := range plan.Owners {
		var scope storageSnapshotPreparationScope
		if err := decodePlanJson(owner.Owner.Inputs, &scope); err != nil || owner.Owner.Kind != "mainnet-evm-action" || scope.MaximumBytes != 512*1024 {
			t.Fatalf("owner %+v: %v", owner.Owner, err)
		}
		names = append(names, scope.Name)
	}
	p := newLaunchPhase(t, parent)
	var phase evmPhaseConfig
	if err := json.Unmarshal(launchExample(t, "settlement-v2.unsigned.example.json", parent, p.values, ""), &phase); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, evmV2JournalNames()) || phase.Plan.RunDirectory != filepath.Join(parent, "run-v2") {
		t.Fatalf("custody prepares %v; the phase example runs in %s", names, phase.Plan.RunDirectory)
	}
	planPath := filepath.Join(parent, "run-v2-preparation-plan.json")
	writeLaunchFile(t, planPath, planned)
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(prepare("apply", "--plan", planPath, "--plan-sha256", durablefixture.Digest(planned)), &result); err != nil || result.RestartAuthorized {
		t.Fatalf("apply: %v", err)
	}
	config, hash := p.sign(t)
	var stdout, stderr bytes.Buffer
	code := runMain(durablepath.WithHost(t.Context(), physical.Host), []string{"bootstrap-contracts", "apply", "--action", "vault2-create", "--config", config, "--run-dir", filepath.Join(parent, "run-v2"), "--accept-plan-hash", hash,
		"--durable-volumes", result.Declaration.Path, "--durable-volumes-sha256", result.Declaration.Sha256}, &stdout, &stderr)
	var opened evmCreateResult
	if code != 0 || json.Unmarshal(stdout.Bytes(), &opened) != nil || opened.Status != "signature-awaiting-import" || opened.Vault2Address != crypto.CreateAddress(p.deployer, p.nonce).Hex() {
		t.Fatalf("prepared custody did not open vault2's journal: %d %+v %s", code, opened, stderr.String())
	}
}

// VA-3, step 10: the auditor drop-in clears the unit's ExecStart and sets the
// command line the repository renders for the validator unit, naming the
// adopted config, plus exactly --ledger_auditor_key_file.
func TestSettlementV2LaunchAuditorDropInExample(t *testing.T) {
	raw, err := os.ReadFile("validator-ledger-auditor.example.conf")
	if err != nil {
		t.Fatal(err)
	}
	var sections, starts []string
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, _ := strings.Cut(line, "=")
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "["):
			sections = append(sections, line)
		case key == "ExecStart":
			starts = append(starts, value)
		default:
			t.Fatalf("drop-in sets %s", line)
		}
	}
	unit := repairValidatorUnit{Binary: planFileReference{Path: "<validator-binary>"}, Config: planFileReference{Path: "<adopted-config>"}, ProgressFile: "<progress-file>",
		DurableVolumes: &durablevolume.Reference{Path: "<validator-declaration>", Sha256: "<validator-declaration-sha256>"}}
	rendered := ""
	for _, line := range strings.Split(string(unit.render()), "\n") {
		if value, found := strings.CutPrefix(line, "ExecStart="); found {
			rendered = value
		}
	}
	if !slices.Equal(sections, []string{"[Service]"}) || !slices.Equal(starts, []string{"", rendered + " --ledger_auditor_key_file=<O10-auditor-key-file>"}) {
		t.Fatalf("drop-in %q %q differs from the rendered %q plus the auditor flag", sections, starts, rendered)
	}
}
