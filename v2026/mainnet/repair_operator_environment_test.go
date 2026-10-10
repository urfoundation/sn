package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
)

// Reaching this body also witnesses successful mainnet package initialization.
// Independently signed declarations exercise the actual host admission bound.
func TestRepairOperatorEnvironmentValueBounds(t *testing.T) {
	f := newRepairOperatorFixture(t)
	for _, test := range []struct {
		name  string
		value string
		valid bool
	}{
		{"one byte", "a", true},
		{"regexp repeat ceiling", strings.Repeat("a", 1000), true},
		{"above regexp repeat ceiling", strings.Repeat("a", 1001), true},
		{"below token ceiling", strings.Repeat("a", 2047), true},
		{"token ceiling", strings.Repeat("a", 2048), true},
		{"every allowed character", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:@,+-", true},
		{"above token ceiling", strings.Repeat("a", 2049), false},
		{"empty", "", false},
		{"space", "a b", false},
		{"newline", "a\nb", false},
		{"nul", "a\x00b", false},
		{"non ASCII", "a\u00e9b", false},
		{"variable expansion", "$HOME", false},
		{"systemd expansion", "%n", false},
		{"quote", "a\"b", false},
		{"escape", "a\\b", false},
	} {
		approval := f.envelope.original
		approval.Plan.Environment = append(slices.Clone(approval.Plan.Environment), repairOperatorEnvironment{Name: "ZZ_OPERATOR_BOUNDARY", Value: test.value})
		approval.Plan.Unit.Sha256 = monitorReadDigest(approval.Plan.render())
		raw, err := approval.signingBytes()
		if err != nil {
			t.Fatal(err)
		}
		approval.Signature = hex.EncodeToString(ed25519.Sign(f.originalPrivate, raw))
		if err := approval.validate(f.originalKey); (err == nil) != test.valid {
			t.Errorf("%s: host admission error = %v, want valid = %t", test.name, err, test.valid)
		}
	}
}
