// The grammar and help of `provider fleet storage`, on every platform.
package miner

import (
	"strings"
	"testing"
)

const fleetStorageTestDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func fleetStorageTestRequestArgs() []string {
	return []string{"fleet", "storage", "request", "--mount_path=/", "--filesystem_uuid=1234-abcd", "--filesystem_type=ext4", "--former-writers-stopped", "--no-previous-owner-state", "--evidence=a new fleet"}
}

// Each step takes exactly its file and digest, so nothing can run on bytes the
// operator didn't pin, and the request can't be written without their statements.
func TestFleetStorageCommandGrammar(t *testing.T) {
	for _, args := range [][]string{
		fleetStorageTestRequestArgs(),
		{"fleet", "storage", "plan", "--request=/r.json", "--request-sha256=" + fleetStorageTestDigest},
		{"fleet", "storage", "apply", "--plan=/p.json", "--plan-sha256=" + fleetStorageTestDigest},
	} {
		opts, err := parseArgsForTestErr(args)
		if err != nil {
			t.Fatalf("%v does not parse: %v", args, err)
		}
		if !mustBoolOpt(opts, "fleet") || !mustBoolOpt(opts, "storage") || !mustBoolOpt(opts, args[2]) {
			t.Fatalf("%v did not choose fleet storage %s: %v", args, args[2], opts)
		}
		if got := selectedFrontCommand(opts); got != "" {
			t.Fatalf("%v chose the front command %q", args, got)
		}
	}
	for _, args := range [][]string{
		{"fleet", "storage"},
		{"fleet", "storage", "plan"},
		{"fleet", "storage", "plan", "--request=/r.json"},
		{"fleet", "storage", "plan", "--request-sha256=" + fleetStorageTestDigest},
		{"fleet", "storage", "apply"},
		{"fleet", "storage", "apply", "--plan=/p.json"},
		{"fleet", "storage", "apply", "--plan-sha256=" + fleetStorageTestDigest},
		{"fleet", "storage", "request"},
		{"fleet", "storage", "request", "--mount_path=/", "--filesystem_uuid=1234-abcd", "--filesystem_type=ext4", "--no-previous-owner-state", "--evidence=a new fleet"},
		{"fleet", "storage", "request", "--mount_path=/", "--filesystem_uuid=1234-abcd", "--filesystem_type=ext4", "--former-writers-stopped", "--evidence=a new fleet"},
		{"fleet", "storage", "request", "--mount_path=/", "--filesystem_uuid=1234-abcd", "--filesystem_type=ext4", "--former-writers-stopped", "--no-previous-owner-state"},
		// Storage is prepared from nothing: no fleet, key or declaration goes in.
		{"fleet", "storage", "plan", "--request=/r.json", "--request-sha256=" + fleetStorageTestDigest, "--durable-volumes=/d.json", "--durable-volumes-sha256=" + fleetStorageTestDigest},
		{"fleet", "storage", "apply", "--plan=/p.json", "--plan-sha256=" + fleetStorageTestDigest, "--hotkey_seed_file=/h.seed"},
		{"fleet", "storage", "plan", "--request=/r.json", "--request-sha256=" + fleetStorageTestDigest, "--manifest=/m.json"},
	} {
		if _, err := parseArgsForTestErr(args); err == nil {
			t.Fatalf("%v parsed", args)
		}
	}
}

// `provider fleet --help` leads to it, and its own help says what it is for.
func TestFleetStorageHelp(t *testing.T) {
	fleet := cliHelpFor([]string{"fleet", "--help"})
	storage := cliHelpFor([]string{"fleet", "storage", "--help"})
	for _, want := range []string{"provider fleet storage request", "provider fleet storage plan", "provider fleet storage apply", "--filesystem_uuid=<uuid>", "--request-sha256=<hash>", "--plan-sha256=<hash>"} {
		for name, help := range map[string]string{"fleet": fleet, "fleet storage": storage} {
			if !strings.Contains(help, want) {
				t.Errorf("%s help does not show %q:\n%s", name, want, help)
			}
		}
	}
	for _, want := range []string{"--durable-volumes", "fleet register, publish, bind and revoke"} {
		if !strings.Contains(storage, want) {
			t.Errorf("fleet storage help does not say what it is for: %q:\n%s", want, storage)
		}
	}
	if !strings.Contains(fleet, "fleet storage") || strings.Contains(storage, "--manifest") {
		t.Errorf("fleet help is not pointed at storage, or storage help shows the fleet flags:\n%s\n%s", fleet, storage)
	}
	for _, help := range []string{fleet, storage} {
		for _, line := range strings.Split(help, "\n") {
			if cliHelpWidth < len(line) {
				t.Errorf("%d-character line %q", len(line), line)
			}
		}
	}
	if durable := cliHelpFor([]string{"fleet", "publish", "--help"}); !strings.Contains(durable, "provider fleet storage") {
		t.Errorf("fleet publish does not say where its declaration comes from:\n%s", durable)
	}
}
