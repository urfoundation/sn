package miner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/operatorlist"
)

// A miner run with provide --all-operators keeps each operator's network jwt
// and provider identity in operatorlist.DomainStateDir, never at the top of
// the state directory. provider status still reports it signed in and
// registered (support inbox 8031: "Signed in: no" on a hotkey miner).
func TestMinerStatusReadsAllOperatorsSignIn(t *testing.T) {
	dir := phase2State(t)
	operatorDir := operatorlist.DomainStateDir(dir, "bringyour.com")
	if err := os.MkdirAll(operatorDir, 0700); err != nil {
		t.Fatal(err)
	}
	writeState(t, operatorDir, "jwt", fakeJwt(map[string]any{"network_name": "tao-c47c049837ff6bc584e93683"}))
	writeState(t, operatorDir, ".provider.key", strings.Repeat("k", 32))
	writeState(t, operatorDir, ".provider.jwt", fakeJwt(map[string]any{"client_id": "01a15cc0-0000-0000-0000-00000000c21f"}))

	report, _ := buildMinerStatusReport(time.Now())
	if !report.SignedIn || report.Network != "tao-c47c049837ff6bc584e93683" {
		t.Fatalf("all-operators sign-in not reported: %+v", report)
	}
	if !report.Registered || report.ClientId != "01a15cc0-0000-0000-0000-00000000c21f" {
		t.Fatalf("all-operators device not reported: %+v", report)
	}
	if report.State == "needs_setup" {
		t.Fatalf("all-operators miner reported as needing setup: %+v", report)
	}

	// the single-operator files at the top keep precedence
	writeState(t, dir, "jwt", fakeJwt(map[string]any{"network_name": "kitchen-pi"}))
	if report, _ := buildMinerStatusReport(time.Now()); report.Network != "kitchen-pi" {
		t.Fatalf("top-level sign-in lost precedence: %+v", report)
	}
}

// An operator directory without a jwt is awaiting auth: no sign-in.
func TestMinerStatusOperatorAwaitingAuth(t *testing.T) {
	dir := phase2State(t)
	if err := os.MkdirAll(filepath.Join(operatorlist.DomainStateDir(dir, "bringyour.com")), 0700); err != nil {
		t.Fatal(err)
	}
	if report, _ := buildMinerStatusReport(time.Now()); report.SignedIn || report.Registered {
		t.Fatalf("awaiting operator reported as set up: %+v", report)
	}
}
