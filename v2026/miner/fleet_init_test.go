package miner

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/protocol"
)

func fleetInitParse(t *testing.T, args ...string) docopt.Opts {
	t.Helper()
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler}
	opts, err := parser.ParseArgs(mainUsage(), append([]string{"fleet", "init"}, args...), "test")
	if err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return opts
}

const (
	fleetInitCoordinator = "0x00000000000000000000000000000000000000c0"
	fleetInitHotkeyHex   = "0x1111111111111111111111111111111111111111111111111111111111111111"
	fleetInitMemberA     = "0x0000000000000000000000000000000a:0x2222222222222222222222222222222222222222222222222222222222222222"
	fleetInitMemberB     = "00000000-0000-0000-0000-00000000000b:0x3333333333333333333333333333333333333333333333333333333333333333"
)

// Support inbox 7992: a claimant could not produce a manifest; fleet init
// must print one that the --manifest commands accept unchanged. That is all of
// stdout, as a redirect saves it, not its first line; the commitment and member
// count fleet manifest prints after the manifest go to stderr.
func TestFleetInitPrintsCanonicalManifest(t *testing.T) {
	opts := fleetInitParse(t, "--chain_id=964", "--coordinator="+fleetInitCoordinator, "--hotkey="+fleetInitHotkeyHex,
		"--member="+fleetInitMemberB, "--member="+fleetInitMemberA)
	random := bytes.NewReader(bytes.Repeat([]byte{0x44}, 32))
	var stdout, stderr bytes.Buffer
	if err := fleetInit(opts, &stdout, &stderr, random); err != nil {
		t.Fatal(err)
	}
	manifest, err := protocol.ParseFleetManifest(stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not a manifest file: %v\n%s", err, stdout.String())
	}
	if manifest.Netuid != 25 || manifest.Generation != 1 || manifest.ChainID != 964 {
		t.Fatalf("defaults: netuid %d generation %d chain %d", manifest.Netuid, manifest.Generation, manifest.ChainID)
	}
	if manifest.FleetID != [32]byte(bytes.Repeat([]byte{0x44}, 32)) {
		t.Fatalf("fleet_id not from injected randomness: %x", manifest.FleetID)
	}
	if manifest.Members[0].ClientID[15] != 0x0a || manifest.Members[1].ClientID[15] != 0x0b {
		t.Fatal("members not sorted by client_id")
	}
	// fleet manifest prints both parts on stdout, in this order
	var want bytes.Buffer
	if err := printFleetManifest(&want, &want, manifest); err != nil {
		t.Fatal(err)
	}
	if stdout.String()+stderr.String() != want.String() || !strings.HasPrefix(stderr.String(), "commitment_sha256: 0x") || !strings.HasSuffix(stderr.String(), "\nmembers: 2\n") {
		t.Fatalf("output differs from fleet manifest:\nstdout %q\nstderr %q\nwant %q", stdout.String(), stderr.String(), want.String())
	}
}

func TestFleetInitClientSeedAndSs58Hotkey(t *testing.T) {
	seed := bytes.Repeat([]byte{0x55}, 32)
	seedPath := filepath.Join(t.TempDir(), "client.seed")
	if err := os.WriteFile(seedPath, []byte(hex.EncodeToString(seed)), 0o600); err != nil {
		t.Fatal(err)
	}
	var hotkey [32]byte
	copy(hotkey[:], bytes.Repeat([]byte{0x11}, 32))
	opts := fleetInitParse(t, "--chain_id=964", "--coordinator="+fleetInitCoordinator, "--hotkey="+testSs58(hotkey),
		"--netuid=521", "--generation=7", "--fleet_id=0x"+strings.Repeat("66", 32),
		"--client_seed_file="+seedPath, "--client_id=0x0000000000000000000000000000000c")
	var out, info bytes.Buffer
	if err := fleetInit(opts, &out, &info, bytes.NewReader(nil)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String()+info.String(), hex.EncodeToString(seed)) {
		t.Fatal("seed printed")
	}
	manifest, err := protocol.ParseFleetManifest(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	public := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if manifest.Hotkey != hotkey || manifest.Netuid != 521 || manifest.Generation != 7 || len(manifest.Members) != 1 ||
		!bytes.Equal(manifest.Members[0].ClientKey[:], public) || manifest.Members[0].ClientID[15] != 0x0c {
		t.Fatalf("unexpected manifest: %s", out.String())
	}
}

func TestFleetInitRefusals(t *testing.T) {
	seedPath := filepath.Join(t.TempDir(), "client.seed")
	if err := os.WriteFile(seedPath, bytes.Repeat([]byte{0x77}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	base := []string{"--chain_id=964", "--coordinator=" + fleetInitCoordinator, "--hotkey=" + fleetInitHotkeyHex}
	for name, args := range map[string][]string{
		"no members":        nil,
		"duplicate member":  {"--member=" + fleetInitMemberA, "--member=" + fleetInitMemberA},
		"bad member":        {"--member=0x0a"},
		"seed no client id": {"--client_seed_file=" + seedPath},
		"client id no seed": {"--client_id=0x0000000000000000000000000000000c"},
	} {
		opts := fleetInitParse(t, append(append([]string(nil), base...), args...)...)
		var out bytes.Buffer
		err := fleetInit(opts, &out, &out, bytes.NewReader(bytes.Repeat([]byte{0x44}, 32)))
		if err == nil {
			t.Fatalf("%s: accepted: %s", name, out.String())
		}
		if strings.Contains(err.Error(), strings.Repeat("77", 32)) {
			t.Fatalf("%s: seed in error", name)
		}
	}
}
