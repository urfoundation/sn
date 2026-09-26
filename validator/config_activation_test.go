//go:build linux || darwin

package validator

// An unrendered evidence_v2 census is a distinct, actionable state: the strict
// loader names it, the pre-activation loader admits it, and the rewrite that
// pins rendered references keeps every other node of the operator's file.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func unrenderedReleaseConfig(t *testing.T) ReleaseConfig {
	t.Helper()
	cfg := validReleaseConfig(t)
	for index := range cfg.EvidenceV2.Operators {
		cfg.EvidenceV2.Operators[index] = ReleaseEvidenceV2OperatorConfig{NoID: cfg.EvidenceV2.Operators[index].NoID}
	}
	return cfg
}

func TestStrictLoaderNamesPendingActivationAndPreActivationLoaderAdmitsIt(t *testing.T) {
	path := writeReleaseConfig(t, unrenderedReleaseConfig(t))
	if _, err := LoadReleaseConfig(path); !errors.Is(err, ErrReleaseEvidenceV2ActivationPending) {
		t.Fatalf("strict loader error = %v, want the activation-pending sentinel", err)
	}
	cfg, err := LoadReleaseConfigPreActivation(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, operator := range cfg.EvidenceV2.Operators {
		if !operator.Unrendered() {
			t.Fatalf("operator %d reported rendered: %+v", operator.NoID, operator)
		}
	}
	// A rendered configuration is admitted by both loaders identically.
	renderedPath := writeReleaseConfig(t, validReleaseConfig(t))
	strict, err := LoadReleaseConfig(renderedPath)
	if err != nil {
		t.Fatal(err)
	}
	pre, err := LoadReleaseConfigPreActivation(renderedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(strict.EvidenceV2, pre.EvidenceV2) {
		t.Fatal("pre-activation loader changed a rendered census")
	}
}

func TestPreActivationLoaderKeepsEveryOtherRule(t *testing.T) {
	// A partially rendered entry is neither unrendered nor complete.
	partial := unrenderedReleaseConfig(t)
	partial.EvidenceV2.Operators[0].Activation.Bytes = 1
	if _, err := LoadReleaseConfigPreActivation(writeReleaseConfig(t, partial)); err == nil || !strings.Contains(err.Error(), "wrong exact width") {
		t.Fatalf("partial reference admitted: %v", err)
	}
	// A pre-declared path overlapping protected state is still refused.
	overlapping := unrenderedReleaseConfig(t)
	overlapping.EvidenceV2.Operators[0].Context.Path = filepath.Join(overlapping.StateDir, "context.json")
	if _, err := LoadReleaseConfigPreActivation(writeReleaseConfig(t, overlapping)); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("overlapping pre-declared path admitted: %v", err)
	}
	// The census must still be exact and sorted.
	unsorted := unrenderedReleaseConfig(t)
	unsorted.EvidenceV2.Operators[0], unsorted.EvidenceV2.Operators[1] = unsorted.EvidenceV2.Operators[1], unsorted.EvidenceV2.Operators[0]
	if _, err := LoadReleaseConfigPreActivation(writeReleaseConfig(t, unsorted)); err == nil || !strings.Contains(err.Error(), "sorted") {
		t.Fatalf("unsorted census admitted: %v", err)
	}
	// Production remains mandatory.
	unproduction := unrenderedReleaseConfig(t)
	unproduction.Production = false
	if _, err := LoadReleaseConfigPreActivation(writeReleaseConfig(t, unproduction)); err == nil || !strings.Contains(err.Error(), "production") {
		t.Fatalf("non-production admitted: %v", err)
	}
}

func TestRewriteReleaseConfigEvidenceV2OperatorsPinsReferencesAndKeepsTheDocument(t *testing.T) {
	cfg := unrenderedReleaseConfig(t)
	rendered := validReleaseConfig(t)
	path := writeReleaseConfig(t, cfg)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A comment and the key order are the operator's document, not ours.
	commented := append([]byte("# operator comment\n"), original...)
	if err := os.WriteFile(path, commented, 0o600); err != nil {
		t.Fatal(err)
	}
	// Rendered references must belong to this configuration's own layout.
	operators := rendered.EvidenceV2.Operators
	for index := range operators {
		operators[index] = releaseEvidenceV2TestConfig(filepath.Dir(cfg.StateDir), cfg.Operators).Operators[index]
	}
	if err := RewriteReleaseConfigEvidenceV2Operators(path, operators); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReleaseConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.EvidenceV2.Operators, operators) {
		t.Fatalf("pinned operators = %+v", loaded.EvidenceV2.Operators)
	}
	if loaded.ValidatorID != cfg.ValidatorID || loaded.PolicyHash != cfg.PolicyHash || len(loaded.Operators) != len(cfg.Operators) || loaded.EvidenceV2.Bounds != cfg.EvidenceV2.Bounds {
		t.Fatal("rewrite changed a node outside evidence_v2.operators")
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(written, []byte("# operator comment\n")) {
		t.Fatalf("rewrite dropped the leading comment:\n%s", written[:80])
	}
	var document yaml.Node
	if err := yaml.Unmarshal(written, &document); err != nil {
		t.Fatal(err)
	}
	firstKey := document.Content[0].Content[0].Value
	if firstKey != "schema_version" {
		t.Fatalf("rewrite reordered keys; first key %q", firstKey)
	}
	backup, err := os.ReadFile(path + ".pre-activation")
	if err != nil || !bytes.Equal(backup, commented) {
		t.Fatalf("pre-activation backup differs: %v", err)
	}
	// A rewrite that would not load strictly never replaces the file.
	broken := operators
	broken[0].Activation.Bytes = 1
	if err := RewriteReleaseConfigEvidenceV2Operators(path, broken); err == nil || !strings.Contains(err.Error(), "strict loader") {
		t.Fatalf("broken rewrite accepted: %v", err)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, written) {
		t.Fatal("a refused rewrite changed the configuration")
	}
}
