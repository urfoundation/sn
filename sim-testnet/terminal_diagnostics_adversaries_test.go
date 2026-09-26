//go:build linux || darwin

// An external diagnostic must read the selected run, preserve its exact bytes,
// and report failed campaigns without lending them a passing matrix outcome.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type terminalDiagnosticAdversaryFixture struct {
	cfg       *ResolvedConfig
	result    *ScenarioResult
	collector *terminalDiagnosticCollector
	runDir    string
	raw       []byte
}

func newTerminalDiagnosticAdversaryFixture(t *testing.T, failed bool) *terminalDiagnosticAdversaryFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	campaign, _, _ := finalSemanticAdversarialTestCampaignForConfig(t, cfg)
	if failed {
		campaign.Vectors[0].Status = "fail"
	}
	raw, err := json.MarshalIndent(campaign, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	fixture := &terminalDiagnosticAdversaryFixture{cfg: cfg, runDir: t.TempDir(), raw: raw,
		result:    &ScenarioResult{AdversarialMatrix: campaign.MatrixHash, Adversaries: campaign},
		collector: &terminalDiagnosticCollector{ctx: t.Context(), output: t.TempDir(), report: &terminalDiagnosticReport{ReadOnly: true}},
	}
	if err := os.WriteFile(filepath.Join(fixture.runDir, "adversaries.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (self *terminalDiagnosticAdversaryFixture) verifyRetention(t *testing.T, status string) {
	t.Helper()
	checks := self.collector.report.Checks
	if len(checks) != 2 || checks[0].Id != "adversarial-matrix" || checks[0].Status != "pass" || checks[1].Id != "adversarial-campaign" || checks[1].Status != status || self.collector.report.FinalAcceptance {
		t.Fatalf("campaign read the wrong directory or changed disposition: %+v", checks)
	}
	locator, ok := checks[1].Evidence.(FinalArtifactLocator)
	if !ok || locator.ContentHash != bytesSHA256(self.raw) || locator.SizeBytes != uint64(len(self.raw)) || locator.Kind != "scenario-adversaries" {
		t.Fatalf("campaign exact-byte locator missing: %+v", checks[1].Evidence)
	}
	retained, err := os.ReadFile(filepath.Join(self.collector.output, locator.URI))
	if err != nil || !bytes.Equal(retained, self.raw) {
		t.Fatalf("campaign bytes changed: %v", err)
	}
	original, err := os.ReadFile(filepath.Join(self.runDir, "adversaries.json"))
	if err != nil || !bytes.Equal(original, self.raw) {
		t.Fatalf("original campaign changed: %v", err)
	}
	entries, err := os.ReadDir(self.runDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("diagnostic wrote into original run: %v, entries=%v", err, entries)
	}
	originals := self.collector.report.Originals
	if len(originals) != 1 || originals[0].Path != filepath.Join(self.runDir, "adversaries.json") || originals[0].Sha256 != bytesSHA256(self.raw) {
		t.Fatalf("original run authority was not retained: %+v", originals)
	}
}

func TestTerminalDiagnosticAdversariesReadOriginalRun(t *testing.T) {
	fixture := newTerminalDiagnosticAdversaryFixture(t, false)
	// This plausible output name must never substitute for selected-run input.
	if err := os.WriteFile(filepath.Join(fixture.collector.output, "adversaries.json"), []byte("untrusted output decoy"), 0o600); err != nil {
		t.Fatal(err)
	}
	collectTerminalDiagnosticAdversaries(fixture.collector, fixture.cfg, fixture.runDir, fixture.result)
	fixture.verifyRetention(t, "pass")
}

func TestTerminalDiagnosticAdversariesRetainFailedOriginal(t *testing.T) {
	fixture := newTerminalDiagnosticAdversaryFixture(t, true)
	collectTerminalDiagnosticAdversaries(fixture.collector, fixture.cfg, fixture.runDir, fixture.result)
	fixture.verifyRetention(t, "fail")
	if !strings.Contains(fixture.collector.report.Checks[1].Detail, "not a complete passing record") {
		t.Fatal("original vector failure was concealed")
	}
	_, matrix, _ := finalSemanticAdversarialTestCampaignForConfig(t, fixture.cfg)
	if _, err := captureFinalSemanticAdversaries(fixture.runDir, fixture.result, matrix); err == nil {
		t.Fatal("diagnostic retention waived the strict campaign verifier")
	}
}

func TestTerminalDiagnosticAdversariesRejectSourceSubstitution(t *testing.T) {
	for _, kind := range []string{"missing", "foreign", "unknown-field", "symlink", "matrix", "result-absent"} {
		fixture := newTerminalDiagnosticAdversaryFixture(t, false)
		path := filepath.Join(fixture.runDir, "adversaries.json")
		if err := os.WriteFile(filepath.Join(fixture.collector.output, "adversaries.json"), fixture.raw, 0o600); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "missing":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		case "foreign":
			foreign := cloneFinalSemanticAdversarialCampaign(t, fixture.result.Adversaries)
			foreign.Seed++
			raw, err := json.Marshal(foreign)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
		case "unknown-field":
			raw := append([]byte("{\"foreign\":true,"), bytes.TrimSpace(fixture.raw)[1:]...)
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(fixture.collector.output, "adversaries.json"), path); err != nil {
				t.Fatal(err)
			}
		case "matrix":
			fixture.result.AdversarialMatrix = "0x" + strings.Repeat("42", 32)
		case "result-absent":
			fixture.result = nil
		}
		collectTerminalDiagnosticAdversaries(fixture.collector, fixture.cfg, fixture.runDir, fixture.result)
		checks := fixture.collector.report.Checks
		if len(checks) != 2 || checks[1].Status == "pass" || len(fixture.collector.report.Originals) != 0 || fixture.collector.report.FinalAcceptance {
			t.Fatalf("%s substituted source authority: %+v", kind, checks)
		}
		if kind == "matrix" || kind == "result-absent" {
			if checks[0].Status == "pass" || checks[1].Status != "unavailable" {
				t.Fatalf("%s fabricated a matrix prerequisite: %+v", kind, checks)
			}
		}
		if _, err := os.Stat(filepath.Join(fixture.collector.output, "final-inputs", "adversaries.json")); !os.IsNotExist(err) {
			t.Fatalf("%s retained an unauthenticated campaign: %v", kind, err)
		}
	}
}
