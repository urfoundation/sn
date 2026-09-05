// Require exact EOF at both harness YAML entry points without rejecting
// permitted comments, document terminators, or authenticated legacy policy.
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/protocol"
	"gopkg.in/yaml.v3"
)

// The shared runtime/configuration decoder must not ignore a malformed suffix.
func TestStrictYAMLRejectsMalformedTrailingYAML(t *testing.T) {
	type sample struct {
		Name string `yaml:"name"`
	}
	for _, testCase := range []struct {
		name, suffix, wantError string
	}{
		{name: "valid"},
		{name: "comment", suffix: "\n# permitted trailing comment\n"},
		{name: "explicit document end", suffix: "\n...\n"},
		{name: "second document", suffix: "\n---\n{}\n", wantError: "multiple YAML documents"},
		{name: "incomplete mapping", suffix: "\n---\n{\n", wantError: "trailing YAML"},
		{name: "incomplete sequence", suffix: "\n---\n[unterminated\n", wantError: "trailing YAML"},
		{name: "invalid escape", suffix: "\n---\n\"\\q\"\n", wantError: "unknown escape"},
	} {
		path := filepath.Join(t.TempDir(), "config.yml")
		if err := os.WriteFile(path, []byte("name: expected\n"+testCase.suffix), 0o600); err != nil {
			t.Fatal(err)
		}
		var got sample
		err := strictYAML(path, &got)
		if testCase.wantError != "" {
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Errorf("%s: config error=%v, want %q", testCase.name, err, testCase.wantError)
			}
		} else if err != nil || got.Name != "expected" {
			t.Errorf("%s: valid config changed: name=%q error=%v", testCase.name, got.Name, err)
		}
	}
}

// Historic policy extraction retains its old validation rules, but must still
// reject a malformed document after the authenticated policy/hash mapping.
func TestRenderedValidatorPolicyRejectsMalformedTrailingYAML(t *testing.T) {
	currentPolicy, err := protocol.LoadPolicy(filepath.Join("..", "deploy", "testnet", "policy-v1.yml"))
	if err != nil {
		t.Fatal(err)
	}
	policy, declaredHash := previousAcceleratedPolicy(t, &ResolvedConfig{Policy: currentPolicy})
	fixture, err := yaml.Marshal(struct {
		Policy     *protocol.Policy `yaml:"policy"`
		PolicyHash string           `yaml:"policy_hash"`
	}{Policy: policy, PolicyHash: declaredHash})
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name, suffix, wantError string
	}{
		{name: "valid legacy policy"},
		{name: "comment", suffix: "\n# permitted trailing comment\n"},
		{name: "explicit document end", suffix: "\n...\n"},
		{name: "second document", suffix: "\n---\n{}\n", wantError: "multiple YAML documents"},
		{name: "incomplete mapping", suffix: "\n---\n{\n", wantError: "trailing YAML"},
		{name: "incomplete sequence", suffix: "\n---\n[unterminated\n", wantError: "trailing YAML"},
		{name: "invalid escape", suffix: "\n---\n\"\\q\"\n", wantError: "unknown escape"},
	} {
		path := filepath.Join(t.TempDir(), "validator.yml")
		wire := append(append([]byte(nil), fixture...), testCase.suffix...)
		if err := os.WriteFile(path, wire, 0o600); err != nil {
			t.Fatal(err)
		}
		got, gotHash, err := readRenderedValidatorPolicy(path)
		if testCase.wantError != "" {
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) || got != nil || gotHash != "" {
				t.Errorf("%s: policy present=%t hash=%q error=%v, want %q", testCase.name, got != nil, gotHash, err, testCase.wantError)
			}
		} else if err != nil || gotHash != declaredHash || !reflect.DeepEqual(got, policy) {
			t.Errorf("%s: legacy policy changed: hash=%q error=%v", testCase.name, gotHash, err)
		}
	}
}
