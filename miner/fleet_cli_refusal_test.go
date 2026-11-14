// A refused fleet command is an answer: its message on stderr and exit 1, never
// a panic and its stack trace, whatever stderr is attached to. The CLI tests run
// the real Run in a child with stderr on a pipe, as a script, an agent or a remote
// shell runs it: off a terminal, a refusal used to come out as a Go panic. The Rpc
// fixture answers on loopback only; nothing reaches a network.
package miner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/protocol"
)

const fleetCliTestNoDocument = "no reviewed fleet runtime authority document is published for the chain's current runtime"

// Pinned as text: an operator follows this link from the message.
const fleetCliTestReadme = "https://github.com/urfoundation/sn/blob/main/mainnet/fleet-runtime-authority/README.md"

var fleetCliTestClientId = [16]byte{0x59}

// The child: Run with the arguments its parent encoded. Otherwise nothing.
func TestFleetCliChild(t *testing.T) {
	encoded := os.Getenv("SN_TEST_FLEET_CLI_CHILD")
	if encoded == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(encoded), &args); err != nil {
		t.Fatal(err)
	}
	Run(args)
}

// Runs one command line through Run in a child, stderr on a pipe. The child
// keeps the parent's environment, so a fixture's state directory reaches it;
// the switches that ask for a panic's trace instead of a message are cleared.
func runFleetCli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFleetCliChild$", "-test.timeout=50s")
	child.Env = append(os.Environ(), "SN_TEST_FLEET_CLI_CHILD="+string(encoded), "WARP_VERSION=synthetic-test", "HOME="+t.TempDir(), "PROVIDER_DEBUG=", cliSetupStepEnv+"=")
	var stdout, stderr bytes.Buffer
	child.Stdout, child.Stderr = &stdout, &stderr
	if err := child.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("fleet child did not run: %v", err)
		}
	}
	return child.ProcessState.ExitCode(), stdout.String(), stderr.String()
}

// A refusal exits 1 with each wanted text on stderr, and never as a panic.
func requireFleetCliRefusal(t *testing.T, name string, code int, stdout, stderr string, wants ...string) {
	t.Helper()
	if code != 1 || strings.Contains(stderr, "panic") || strings.Contains(stderr, "goroutine ") || !strings.Contains(stderr, "✗ ") {
		t.Errorf("%s: exit %d, want a plain refusal and exit 1\nstdout: %s\nstderr: %s", name, code, stdout, stderr)
		return
	}
	for _, want := range wants {
		if !strings.Contains(stderr, want) {
			t.Errorf("%s: stderr lacks %q\nstderr: %s", name, want, stderr)
		}
	}
}

// A canonical synthetic mainnet manifest, in a private directory.
func writeFleetCliTestManifest(t *testing.T) string {
	t.Helper()
	var clientKey [32]byte
	copy(clientKey[:], ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x57}, ed25519.SeedSize)).Public().(ed25519.PublicKey))
	manifest := &protocol.FleetManifest{Schema: protocol.FleetManifestSchema, ChainID: 964, Netuid: 25, Coordinator: [20]byte{0x55}, FleetID: [32]byte{0x58}, Hotkey: [32]byte(bytes.Repeat([]byte{0x56}, 32)), Generation: 1,
		Members: []protocol.FleetMember{{ClientID: fleetCliTestClientId, ClientKey: clientKey}}}
	raw, err := manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fleet.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Without a document every online mainnet command refuses before keys or
// network, and says that none is published, how to pass one and where to look.
// The seed files named here don't exist: nothing reads them.
func TestFleetCliRefusesMainnetWithoutAuthorityDocument(t *testing.T) {
	t.Setenv("URNETWORK_STATE_DIR", t.TempDir())
	manifest := "--manifest=" + writeFleetCliTestManifest(t)
	clientId := "--client_id=" + hex.EncodeToString(fleetCliTestClientId[:])
	absent := filepath.Join(t.TempDir(), "absent.seed")
	for _, args := range [][]string{
		{"fleet", "status", manifest, clientId, "--substrate=wss://native.example", "--rpc=https://evm.example"},
		{"fleet", "register", manifest, "--hotkey_seed_file=" + absent, "--coldkey_seed_file=" + absent, "--substrate=wss://native.example"},
		{"fleet", "publish", manifest, "--substrate=wss://native.example", "--hotkey_seed_file=" + absent},
		{"fleet", "bind", manifest, clientId, "--client_seed_file=" + absent, "--hotkey_seed_file=" + absent, "--valid_from_epoch=1", "--valid_to_epoch=2", "--rpc=https://evm.example", "--relayer_key_file=" + absent},
		{"fleet", "revoke", manifest, clientId, "--client_seed_file=" + absent, "--effective_epoch=2", "--rpc=https://evm.example", "--relayer_key_file=" + absent},
	} {
		code, stdout, stderr := runFleetCli(t, args...)
		requireFleetCliRefusal(t, args[1], code, stdout, stderr, fleetCliTestNoDocument, "--mainnet-runtime-authority=<absolute path>", fleetCliTestReadme)
	}
}

// A document that can't be used is refused in words: missing, other bytes than
// the given digest, and malformed under its own digest.
func TestFleetCliRefusesUnusableAuthorityDocument(t *testing.T) {
	t.Setenv("URNETWORK_STATE_DIR", t.TempDir())
	manifest := "--manifest=" + writeFleetCliTestManifest(t)
	directory := t.TempDir()
	malformed := filepath.Join(directory, "malformed-authority.json")
	raw := []byte(`{"schema":"urnetwork-mainnet-fleet-runtime-authority-v1","schema":"repeated"}`)
	if err := os.WriteFile(malformed, raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	missing := filepath.Join(directory, "absent-authority.json")
	other := strings.Repeat("1", 64)
	for _, c := range []struct {
		name   string
		path   string
		digest string
		wants  []string
	}{
		{name: "missing", path: missing, digest: other, wants: []string{"the fleet runtime authority document can't be read", missing}},
		{name: "other bytes", path: malformed, digest: other, wants: []string{"differs from --mainnet-runtime-authority-sha256"}},
		{name: "malformed", path: malformed, digest: hex.EncodeToString(digest[:]), wants: []string{"the fleet runtime authority document is malformed"}},
	} {
		code, stdout, stderr := runFleetCli(t, "fleet", "status", manifest, "--client_id="+hex.EncodeToString(fleetCliTestClientId[:]),
			"--substrate=wss://native.example", "--rpc=https://evm.example", "--mainnet-runtime-authority="+c.path, "--mainnet-runtime-authority-sha256="+c.digest)
		requireFleetCliRefusal(t, c.name, code, stdout, stderr, append(c.wants, fleetCliTestReadme)...)
	}
}

// Past the document, a failed runtime version read and a runtime the document
// doesn't name are words through Run too. Only the second says no reviewed
// document covers the chain's runtime: a failed read is not an upgrade.
func TestFleetCliRefusesStatusAfterDial(t *testing.T) {
	for _, c := range []struct {
		name     string
		change   func(*fleetMainnetTestFixture)
		wants    []string
		unwanted string
	}{
		{
			name: "runtime version read fails",
			change: func(fixture *fleetMainnetTestFixture) {
				fixture.rpcFailure = func(method string) error {
					if method == "state_getRuntimeVersion" {
						return errors.New("synthetic runtime version outage")
					}
					return nil
				}
			},
			wants:    []string{"synthetic runtime version outage"},
			unwanted: fleetCliTestNoDocument,
		},
		{
			name:   "runtime upgraded",
			change: func(fixture *fleetMainnetTestFixture) { fixture.version.SpecVersion++ },
			wants:  []string{fleetCliTestNoDocument + ", synthetic-subtensor 8002, or the one given is out of date", fleetCliTestReadme, "unreviewed identity synthetic-subtensor/8002/1/1"},
		},
	} {
		fixture := newFleetMainnetTestFixture(t)
		fixture.stateLock.Lock()
		c.change(fixture)
		fixture.stateLock.Unlock()
		code, stdout, stderr := runFleetCli(t, "fleet", "status", "--manifest="+fleetOpt(fixture.opts, "--manifest"), "--client_id="+fleetOpt(fixture.opts, "--client_id"),
			"--substrate="+fixture.server.URL, "--rpc="+fixture.server.URL,
			"--mainnet-runtime-authority="+fleetOpt(fixture.opts, "--mainnet-runtime-authority"), "--mainnet-runtime-authority-sha256="+fleetOpt(fixture.opts, "--mainnet-runtime-authority-sha256"))
		requireFleetCliRefusal(t, c.name, code, stdout, stderr, c.wants...)
		if c.unwanted != "" && strings.Contains(stderr, c.unwanted) {
			t.Errorf("%s: a failed read was reported as a missing document\nstderr: %s", c.name, stderr)
		}
		if fixture.count("eth_call") != 0 {
			t.Errorf("%s: refused status reached the coordinator", c.name)
		}
	}
}

// Commands that read no authority document took the same panic: a missing
// manifest, fleet init without a member, and fleet storage without its request.
func TestFleetCliRefusesOtherFleetCommandsInWords(t *testing.T) {
	t.Setenv("URNETWORK_STATE_DIR", t.TempDir())
	missing := filepath.Join(t.TempDir(), "absent-fleet.json")
	// linux and darwin name the request; elsewhere storage refuses the host
	storage := "fleet storage preparation requires"
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		storage = "/absent-request.json"
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{args: []string{"fleet", "status", "--manifest=" + missing, "--client_id=" + hex.EncodeToString(fleetCliTestClientId[:]), "--substrate=wss://native.example", "--rpc=https://evm.example"}, want: missing},
		{args: []string{"fleet", "init", "--chain_id=964", "--coordinator=0x" + strings.Repeat("55", 20), "--hotkey=0x" + strings.Repeat("56", 32)}, want: "--member"},
		{args: []string{"fleet", "storage", "plan", "--request=/absent-request.json", "--request-sha256=" + fleetStorageTestDigest}, want: storage},
	} {
		code, stdout, stderr := runFleetCli(t, c.args...)
		requireFleetCliRefusal(t, strings.Join(c.args[:2], " "), code, stdout, stderr, c.want)
	}
}
