//go:build linux || darwin

package main

// Real signed setup and successor plans qualify retained runtime identity.
import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	validatorcomponent "github.com/urfoundation/sn/validator"
	"gopkg.in/yaml.v3"
)

// The original render survives exact V6 adoption, while strict rendering still
// requires the new identity and children receive only the approved capacity.
func TestEvidenceRelaySourceExpansionRetainedManifestAndValidatorHandoff(t *testing.T) {
	fixture, executor := newEvidenceRelayExpansionTest(t)
	retainEvidenceRelaySourceExpansionSetupTest(t, fixture, executor)
	cfg, stateDir := fixture.cfg, fixture.stateDir
	cfg.Repos.Vault = t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg.Repos.Vault, "local"), 0o700); err != nil {
		t.Fatal(err)
	}
	original, err := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := expectedRuntimeConfigFiles(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for relative, mode := range expected {
		path := filepath.Join(stateDir, filepath.FromSlash(relative))
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			if err := atomicWrite(path, []byte("synthetic retained input\n"), mode); err != nil {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	var specs []ProcessSpec
	for index := range original.Config.ValidatorEvidenceV2 {
		id := fmt.Sprintf("validator-%d", index+1)
		wire, err := marshalRuntimeValidatorConfig(original, stateDir, fixture.roles, map[string]any{}, index+1)
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(stateDir, "runtime", id, "validator.yml"), wire, 0o600); err != nil {
			t.Fatal(err)
		}
		specs = append(specs, ProcessSpec{ID: id, Role: "validator"})
	}
	if err := writeRuntimeConfigManifest(cfg, stateDir); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(runtimeConfigManifestPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	current := evidenceRelaySourceExpansionRequestTest(t, fixture, executor)
	plan, err := appendEvidenceRelayContinuationPlan(executor.plan, current)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(cfg, stateDir, plan, fixture.roles); err != nil {
		t.Fatal(err)
	}
	cfg.provisionalResume = &provisionalResumeState{
		Record: &provisionalResumeRecord{Command: "resume", PlanHash: plan.PlanHash, Provisional: true},
		RecordPath: filepath.Join(stateDir, "provisional-resumes", "synthetic", "provenance.json"),
	}
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	if _, err := verifyRuntimeConfigManifest(cfg, stateDir); err == nil || !strings.Contains(err.Error(), "manifest identity") {
		t.Fatal("strict render verification admitted original capacity", err)
	}
	if _, err := verifyRetainedProvisionalRuntimeConfigManifest(cfg, stateDir, plan); err != nil {
		t.Fatal("approved successor stranded its original retained manifest", err)
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimeBlobConfigManifest(resolved, stateDir); err != nil {
		t.Fatal("provisional store observation lost the original manifest", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) {
		t.Fatal("retained manifest verification changed original state")
	}
	if err := attachProvisionalActivationSetup(cfg, stateDir, plan, fixture.roles, specs); err != nil {
		t.Fatal(err)
	}
	for index, spec := range specs {
		path := strings.TrimPrefix(spec.Args[0], "--provisional-activation-setup=")
		raw, err := os.ReadFile(path)
		if err != nil || spec.Args[1] != "--provisional-activation-setup-sha256="+bytesSHA256(raw) {
			t.Fatal("handoff lost its exact supervisor pin", err)
		}
		var handoff validatorcomponent.ProvisionalActivationSetupV2
		if err := json.Unmarshal(raw, &handoff); err != nil {
			t.Fatal(err)
		}
		want := validatorcomponent.ReleaseEvidenceV2SourceBounds{Original: current.SourceBounds[index].Original, Approved: current.SourceBounds[index].Approved}
		if handoff.SourceBounds == nil || !reflect.DeepEqual(*handoff.SourceBounds, want) || handoff.ApprovedPlanHash != plan.PlanHash || !handoff.Provisional || handoff.FinalAcceptance {
			t.Fatal("validator handoff lost the exact non-accepting capacity approval")
		}
		configBytes, err := os.ReadFile(filepath.Join(stateDir, "runtime", spec.ID, "validator.yml"))
		if err != nil || bytesSHA256(configBytes) != handoff.ConfigSHA256 {
			t.Fatal("handoff changed the original config pin", err)
		}
		var rendered struct { Evidence validatorcomponent.ReleaseEvidenceV2Config `yaml:"evidence_v2"` }
		if err := yaml.Unmarshal(configBytes, &rendered); err != nil || !reflect.DeepEqual(rendered.Evidence.Bounds, want.Original) {
			t.Fatal("retained YAML was rewritten with successor bounds", err)
		}
	}
	for _, field := range []string{"config", "policy", "evidence", "upload"} {
		rewriteRuntimeConfigManifest(t, stateDir, func(manifest *RuntimeConfigManifest) {
			hash := "0x" + strings.Repeat("91", 32)
			switch field {
			case "config": manifest.ConfigHash = hash
			case "policy": manifest.PolicyHash = hash
			case "evidence": manifest.EvidenceV2Hash = hash
			case "upload": manifest.AttemptUploadHash = hash
			}
		})
		if _, err := verifyRetainedProvisionalRuntimeConfigManifest(cfg, stateDir, plan); err == nil {
			t.Fatal("retained identity admitted changed manifest field", field)
		}
		if err := atomicWrite(runtimeConfigManifestPath(stateDir), manifestBytes, 0o600); err != nil { t.Fatal(err) }
	}
	path := filepath.Join(stateDir, "runtime", "validator-1", "validator.yml")
	wire, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	if err := atomicWrite(path, append(bytes.Clone(wire), []byte("# changed\n")...), 0o600); err != nil { t.Fatal(err) }
	if _, err := verifyRetainedProvisionalRuntimeConfigManifest(cfg, stateDir, plan); err == nil {
		t.Fatal("retained startup accepted changed config bytes")
	}
	if err := atomicWrite(path, wire, 0o600); err != nil { t.Fatal(err) }
	cfg.provisionalResume.Record.FinalAcceptance = true
	if _, err := verifyRetainedProvisionalRuntimeConfigManifest(cfg, stateDir, plan); err == nil { t.Fatal("retained identity became final acceptance") }
	if err := verifyRuntimeBlobConfigManifest(resolved, stateDir); err == nil { t.Fatal("retained store identity became final acceptance") }
}
