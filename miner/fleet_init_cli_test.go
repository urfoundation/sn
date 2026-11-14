// `provider fleet init ... > fleet.json` is how an operator makes the
// --manifest file. The real Run in a child writes stdout to that file as a
// shell redirect does, and the next commands take it: fleet manifest checks it
// and fleet sign signs a binding from it. Everything is local and synthetic.
package miner

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/protocol"
)

// Stdout is the canonical manifest alone, and its commitment and member count
// are on stderr. Saved from stdout, the manifest feeds fleet manifest and
// fleet sign unchanged; the first line, which the Top 200 page's command keeps
// with head -n 1, is the same manifest.
func TestFleetCliInitStdoutIsTheManifestFile(t *testing.T) {
	t.Setenv("URNETWORK_STATE_DIR", t.TempDir())
	directory := t.TempDir()
	seed := bytes.Repeat([]byte{0x57}, ed25519.SeedSize)
	seedPath := filepath.Join(directory, "client.seed")
	if err := os.WriteFile(seedPath, seed, 0600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runFleetCli(t, "fleet", "init", "--chain_id=964", "--coordinator=0x"+strings.Repeat("55", 20),
		"--hotkey=0x"+strings.Repeat("56", 32), "--fleet_id=0x"+strings.Repeat("58", 32),
		"--client_seed_file="+seedPath, "--client_id="+hex.EncodeToString(fleetCliTestClientId[:]))
	if code != 0 {
		t.Fatalf("fleet init: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	var clientKey [32]byte
	copy(clientKey[:], ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	want := &protocol.FleetManifest{Schema: protocol.FleetManifestSchema, ChainID: 964, Netuid: 25, Coordinator: [20]byte(bytes.Repeat([]byte{0x55}, 20)),
		FleetID: [32]byte(bytes.Repeat([]byte{0x58}, 32)), Hotkey: [32]byte(bytes.Repeat([]byte{0x56}, 32)), Generation: 1,
		Members: []protocol.FleetMember{{ClientID: fleetCliTestClientId, ClientKey: clientKey}}}
	canonical, err := want.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := want.CommitmentHash()
	if err != nil {
		t.Fatal(err)
	}
	if stdout != string(canonical)+"\n" {
		t.Errorf("fleet init stdout is not the manifest alone:\n%q\nwant %q", stdout, string(canonical)+"\n")
	}
	if !strings.Contains(stderr, fmt.Sprintf("commitment_sha256: 0x%x\nmembers: 1\n", hash)) {
		t.Errorf("fleet init stderr lacks the commitment and member count:\n%s", stderr)
	}
	path := filepath.Join(directory, "fleet.json")
	if err := os.WriteFile(path, []byte(stdout), 0600); err != nil {
		t.Fatal(err)
	}
	code, checked, stderr := runFleetCli(t, "fleet", "manifest", "--manifest="+path)
	if code != 0 || !strings.HasPrefix(checked, string(canonical)+"\n") {
		t.Fatalf("fleet manifest refused the saved fleet init output: exit %d\nstdout: %s\nstderr: %s", code, checked, stderr)
	}
	code, signed, stderr := runFleetCli(t, "fleet", "sign", "--manifest="+path, "--client_seed_file="+seedPath, "--valid_from_epoch=10", "--valid_to_epoch=20")
	if code != 0 || !strings.Contains(signed, `"client_signature"`) {
		t.Fatalf("fleet sign refused the saved fleet init output: exit %d\nstdout: %s\nstderr: %s", code, signed, stderr)
	}
}
