//go:build linux || darwin

// validator setup with synthetic public terms: the actual flag grammar, wallet
// resolution, key custody, configure and its loaders, with the chain reads
// and endpoint checks replaced. The endpoint checks run against a local
// JSON-RPC server.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	gethrpc "github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/crv4"
)

const (
	setupTestAliceSeedHex = "e5be9a5092b81bca64be81d212e7f2f9eba183bb7a90954f7b76361f6edb5c0a"
	setupTestAlicePubHex  = "d43593c715fdd31c61141abd04a99fd6822c8558854ccde39a5684e7a56da27d"
	setupTestAliceAddress = "5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY"
)

type setupTestFixture struct {
	home       string
	profile    *releaseMainnetProfile
	snapshot   releaseConfigureNativeSnapshot
	reads      int
	preflights []string
	statuses   []string
}

// A private account home with a btcli wallet holding Alice's hotkey as btcli
// writes it, and synthetic public terms from the configure fixtures.
func newSetupTestFixture(t *testing.T) *setupTestFixture {
	t.Helper()
	profile, _, snapshot := configureTestInputs(t)
	self := &setupTestFixture{home: identityTestStateDir(t), profile: profile, snapshot: snapshot}
	t.Setenv("HOME", self.home)
	hotkeys := filepath.Join(self.home, ".bittensor", "wallets", "operator", "hotkeys")
	if err := os.MkdirAll(hotkeys, 0o700); err != nil {
		t.Fatal(err)
	}
	keyfile := `{"accountId": "0x` + setupTestAlicePubHex + `", "publicKey": "0x` + setupTestAlicePubHex + `", "secretSeed": "0x` + setupTestAliceSeedHex + `", "ss58Address": "` + setupTestAliceAddress + `"}`
	if err := os.WriteFile(filepath.Join(hotkeys, "sn25"), []byte(keyfile), 0o600); err != nil {
		t.Fatal(err)
	}
	return self
}

func (self *setupTestFixture) dependencies() setupDependencies {
	return setupDependencies{
		profile: func() (*releaseMainnetProfile, error) { return self.profile, nil },
		preflight: func(_ context.Context, rpc, substrate string, _ *releaseMainnetProfile) error {
			self.preflights = append(self.preflights, rpc+" "+substrate)
			return nil
		},
		snapshot: func(context.Context, *releaseMainnetProfile, string) (releaseConfigureNativeSnapshot, error) {
			self.reads++
			return self.snapshot, nil
		},
		status: func(path string) { self.statuses = append(self.statuses, path) },
	}
}

func (self *setupTestFixture) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runSetupCommandWith(t.Context(), args, &stdout, &stderr, self.dependencies())
	return code, stdout.String(), stderr.String()
}

// The hotkey is the one required input, and each way of leaving it out or
// giving it twice is a usage error that names the flags.
func TestSetupRequiresExactlyOneHotkey(t *testing.T) {
	fixture := newSetupTestFixture(t)
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: nil, want: "--wallet.name=<wallet> --wallet.hotkey=<hotkey>"},
		{args: []string{"--wallet.name=operator"}, want: "needs both --wallet.name and --wallet.hotkey"},
		{args: []string{"--wallet.hotkey=sn25"}, want: "needs both --wallet.name and --wallet.hotkey"},
		{args: []string{"--wallet.name=operator", "--wallet.hotkey=sn25", "--hotkey-file=/tmp/hotkey"}, want: "choose the hotkey once"},
		{args: []string{"--wallet.name=../operator", "--wallet.hotkey=sn25"}, want: "not a wallet entry name"},
		{args: []string{"--wallet.name=operator", "--wallet.hotkey=sn25", "--validator-id=0"}, want: "--validator-id must be positive"},
		{args: []string{"--hotkey-file=/tmp/hotkey", "--unknown"}, want: "flag provided but not defined"},
	} {
		code, stdout, stderr := fixture.run(t, test.args...)
		if code != 2 || !strings.Contains(stderr, test.want) || fixture.reads != 0 || stdout != "" {
			t.Errorf("%v: exit=%d stderr=%q stdout=%q", test.args, code, stderr, stdout)
		}
	}
	if _, err := os.Lstat(filepath.Join(fixture.home, ".urnetwork")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused setup created the home")
	}
}

// A wallet hotkey that does not exist, or cannot be read, is named with its
// path and cause before any home or key is made.
func TestSetupNamesAnUnusableWalletHotkey(t *testing.T) {
	fixture := newSetupTestFixture(t)
	code, _, stderr := fixture.run(t, "--wallet.name=operator", "--wallet.hotkey=missing")
	if want := filepath.Join(fixture.home, ".bittensor", "wallets", "operator", "hotkeys", "missing"); code != 1 || !strings.Contains(stderr, want) || !strings.Contains(stderr, "does not exist") {
		t.Fatalf("missing hotkey: exit=%d %q", code, stderr)
	}
	encrypted := filepath.Join(fixture.home, ".bittensor", "wallets", "operator", "hotkeys", "locked")
	if err := os.WriteFile(encrypted, []byte("$NACL"+strings.Repeat("e", 80)), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = fixture.run(t, "--wallet.name=operator", "--wallet.hotkey=locked")
	if code != 1 || !strings.Contains(stderr, "encrypted Bittensor keyfile") {
		t.Fatalf("encrypted hotkey: exit=%d %q", code, stderr)
	}
	if _, err := os.Lstat(filepath.Join(fixture.home, ".urnetwork")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an unusable hotkey created the home")
	}
}

// The wallet's own keyfile, the bundled terms and the home make the signed
// initial configuration; flags override the bundled endpoints, the ID and the
// window. A second run reuses it, and another hotkey cannot take the home.
func TestSetupWritesTheSignedConfigFromAWalletHotkey(t *testing.T) {
	fixture := newSetupTestFixture(t)
	code, stdout, stderr := fixture.run(t, "--wallet.name=operator", "--wallet.hotkey=sn25")
	if code != 0 {
		t.Fatalf("setup exit=%d: %s", code, stderr)
	}
	home, err := resolveValidatorHome("")
	if err != nil {
		t.Fatal(err)
	}
	if home.Root() != filepath.Join(fixture.home, ".urnetwork", "sn25-validator") {
		t.Fatalf("default home %s", home.Root())
	}
	cfg, err := LoadReleaseProductionConfigPreActivation(home.InitialConfig())
	if err != nil {
		t.Fatalf("setup config fails pending production admission: %v", err)
	}
	approved, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		t.Fatal(err)
	}
	hotkey, err := crv4.KeypairFromSeedHex(setupTestAliceSeedHex)
	if err != nil {
		t.Fatal(err)
	}
	wallet := filepath.Join(fixture.home, ".bittensor", "wallets", "operator", "hotkeys", "sn25")
	window := approved.Approval.ValidThroughNativeBlock - approved.Approval.ValidFromNativeBlock
	if cfg.HotkeySeedFile != wallet || approved.Approval.ValidatorHotkey != hotkey.PublicKey() || cfg.ValidatorID != defaultSetupValidatorID(hotkey.PublicKey()) ||
		cfg.StateDir != home.State() || cfg.Operators[0].ClientKeySeedFile != home.ClientKey() || window != uint64(defaultSetupApprovalValidFor/(12*time.Second)) {
		t.Fatalf("setup config does not name the wallet hotkey and home: hotkey=%s id=%d state=%s window=%d", cfg.HotkeySeedFile, cfg.ValidatorID, cfg.StateDir, window)
	}
	if len(fixture.preflights) != 1 || fixture.preflights[0] != fixture.profile.Config.RPC[0]+" "+fixture.profile.Config.Substrate[0] ||
		len(fixture.statuses) != 1 || fixture.statuses[0] != home.InitialConfig() {
		t.Fatalf("setup skipped its endpoint check or registration report: %v %v", fixture.preflights, fixture.statuses)
	}
	for _, directory := range []string{home.Root(), home.KeysDir(), home.ConfigDir(), home.StorageDir(), home.Runtime()} {
		if info, err := os.Stat(directory); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("home directory %s is not private: %v", directory, err)
		}
	}
	if entries, err := os.ReadDir(home.Runtime()); err != nil || len(entries) != 0 {
		t.Fatalf("setup wrote into the runtime root before storage preparation: %v %v", entries, err)
	}
	if !strings.Contains(stdout, "validator setup storage --validator-uid=<UID> --fresh") || !strings.Contains(stdout, "validator run") {
		t.Fatalf("setup did not print the next steps: %s", stdout)
	}
	code, stdout, stderr = fixture.run(t, "--wallet.name=operator", "--wallet.hotkey=sn25")
	if code != 0 || fixture.reads != 1 || !strings.Contains(stdout, "(existing)") {
		t.Fatalf("a second setup did not reuse the config: exit=%d reads=%d %s %s", code, fixture.reads, stdout, stderr)
	}
	other := filepath.Join(fixture.home, "other.seed")
	if err := os.WriteFile(other, []byte("0x"+strings.Repeat("21", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = fixture.run(t, "--hotkey-file="+other)
	if code != 1 || !strings.Contains(stderr, "is set up for the hotkey in "+wallet) {
		t.Fatalf("another hotkey took over the home: exit=%d %s", code, stderr)
	}
	// Flags win over the bundled defaults, in their own home.
	code, _, stderr = fixture.run(t, "--hotkey-file="+other, "--home="+filepath.Join(fixture.home, "second"), "--validator-id=7",
		"--rpc=https://evm.operator.example", "--substrate=wss://native.operator.example", "--approval-valid-for=48h")
	if code != 0 {
		t.Fatalf("setup with overrides exit=%d: %s", code, stderr)
	}
	second, err := LoadReleaseProductionConfigPreActivation(filepath.Join(fixture.home, "second", "config", "initial.yml"))
	if err != nil {
		t.Fatal(err)
	}
	approved, err = ownerRecycleProductionApproval(second)
	if err != nil {
		t.Fatal(err)
	}
	if second.ValidatorID != 7 || second.RPC[0] != "https://evm.operator.example" || second.Substrate[0] != "wss://native.operator.example" ||
		approved.Approval.ValidThroughNativeBlock-approved.Approval.ValidFromNativeBlock != uint64(48*time.Hour/(12*time.Second)) ||
		fixture.preflights[1] != "https://evm.operator.example wss://native.operator.example" {
		t.Fatalf("flags did not override the bundled defaults: id=%d rpc=%v substrate=%v", second.ValidatorID, second.RPC, second.Substrate)
	}
}

// An endpoint check failure stops setup before it signs a configuration.
func TestSetupStopsOnAnUnusableEndpoint(t *testing.T) {
	fixture := newSetupTestFixture(t)
	dependencies := fixture.dependencies()
	dependencies.preflight = func(context.Context, string, string, *releaseMainnetProfile) error {
		return errors.New("EVM RPC https://public.example refuses eth_getLogs")
	}
	var stdout, stderr bytes.Buffer
	if code := runSetupCommandWith(t.Context(), []string{"--wallet.name=operator", "--wallet.hotkey=sn25"}, &stdout, &stderr, dependencies); code != 1 || !strings.Contains(stderr.String(), "refuses eth_getLogs") {
		t.Fatalf("exit=%d %s", code, stderr.String())
	}
	home, err := resolveValidatorHome("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(home.InitialConfig()); !errors.Is(err, os.ErrNotExist) || fixture.reads != 0 {
		t.Fatalf("a refused endpoint still produced a signed config: %v", err)
	}
}

// Public endpoints refuse log ranges wider than some bound, or keep no
// history; such a node passes setup's own reads and fails the running
// validator. The check reads one log chunk of the validator's own size and a
// week-old state, refuses each failure, and accepts an archive-like node.
func TestSetupEndpointCheckRefusesNodesWithoutLogsOrHistory(t *testing.T) {
	const coordinator = "0xc18925925e2b7bb9059b7d696b8c92762ae86406"
	for _, test := range []struct {
		name string
		logs uint64
		code string
		want string
	}{
		{name: "archive", logs: 1000, code: "0x6080", want: ""},
		{name: "logs refused", logs: 0, code: "0x6080", want: "refuses eth_getLogs over 1000 blocks"},
		{name: "narrow log ranges", logs: 999, code: "0x6080", want: "refuses eth_getLogs over 1000 blocks"},
		{name: "pruned", logs: 1000, code: "0x", want: "serves no state from a week ago"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var call struct {
				ID     json.RawMessage   `json:"id"`
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
				http.Error(w, "bad request", 400)
				return
			}
			reply := map[string]any{"jsonrpc": "2.0", "id": call.ID}
			switch call.Method {
			case "eth_blockNumber":
				reply["result"] = "0x9218de"
			case "eth_getLogs":
				var filter struct {
					Address string `json:"address"`
					From    string `json:"fromBlock"`
					To      string `json:"toBlock"`
				}
				from, to := uint64(0), uint64(0)
				if len(call.Params) == 1 && json.Unmarshal(call.Params[0], &filter) == nil {
					from, _ = hexutil.DecodeUint64(filter.From)
					to, _ = hexutil.DecodeUint64(filter.To)
				}
				if filter.Address != coordinator || to != 0x9218de || to < from || to-from+1 > test.logs {
					reply["error"] = map[string]any{"code": -32601, "message": "Method not allowed"}
				} else {
					reply["result"] = []any{}
				}
			case "eth_getCode":
				var address, block string
				if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &address) != nil || json.Unmarshal(call.Params[1], &block) != nil || address != coordinator || block != "0x9153fe" {
					reply["error"] = map[string]any{"code": -32602, "message": "unexpected eth_getCode " + string(call.Params[1])}
				} else {
					reply["result"] = test.code
				}
			default:
				reply["error"] = map[string]any{"code": -32601, "message": "unexpected " + call.Method}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(reply)
		}))
		client, err := gethrpc.DialContext(t.Context(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		err = preflightSetupEVM(t.Context(), client, server.URL, coordinator)
		client.Close()
		server.Close()
		if test.want == "" && err != nil || test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
			t.Errorf("%s: %v", test.name, err)
		}
	}
}

// The derived ID is positive and the same for the same hotkey.
func TestSetupValidatorIDIsDerivedFromTheHotkey(t *testing.T) {
	first, err := hex.DecodeString(setupTestAlicePubHex)
	if err != nil {
		t.Fatal(err)
	}
	id := defaultSetupValidatorID([32]byte(first))
	if id == 0 || id != defaultSetupValidatorID([32]byte(first)) || id == defaultSetupValidatorID([32]byte{1}) {
		t.Fatalf("derived validator ID %d is not a stable positive hotkey identity", id)
	}
}

// The later steps refuse to run without the earlier ones' files.
func TestSetupLaterStepsNeedTheEarlierOnes(t *testing.T) {
	fixture := newSetupTestFixture(t)
	for _, test := range []struct {
		args []string
		code int
		want string
	}{
		{args: []string{"storage"}, code: 2, want: "needs --validator-uid=<UID>"},
		{args: []string{"storage", "--apply"}, code: 1, want: "no storage plan to apply"},
		{args: []string{"storage", "--apply", "--fresh"}, code: 2, want: "--apply takes only the plan"},
		{args: []string{"activate", "--dry-run"}, code: 2, want: "--relayer-key-file"},
		{args: []string{"activate", "--relayer-key-file=/tmp/relayer.key", "--dry-run"}, code: 1, want: "no prepared storage"},
		{args: []string{"unknown"}, code: 2, want: "unknown setup step"},
	} {
		code, _, stderr := fixture.run(t, test.args...)
		if code != test.code || !strings.Contains(stderr, test.want) {
			t.Errorf("%v: exit=%d %q", test.args, code, stderr)
		}
	}
}
