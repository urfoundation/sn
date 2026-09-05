package main

// These tests reproduce the exact single-metavariable rewrite incident and
// adjacent partial-repair shapes without modifying repository sources.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Writes one isolated source input for the parser-only integrity checks.
func writeSourceGuardFixture(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.go")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A conventional self receiver is valid and must not be confused with the
// destructive whole-file rewrite that prompted this guard.
func TestValidateGoSourceFileAcceptsSelfReceiver(t *testing.T) {
	path := writeSourceGuardFixture(t, "package main\ntype fixture struct{}\nfunc (self fixture) value() int { return 1 }\n")
	if err := validateGoSourceFile(path); err != nil {
		t.Fatal(err)
	}
}

// The exact gofmt metavariable failure changed the package identifier along
// with every other expression and must be rejected before compilation.
func TestValidateGoSourceFileRejectsRewrittenPackage(t *testing.T) {
	path := writeSourceGuardFixture(t, "package self\ntype fixture struct{}\n")
	err := validateGoSourceFile(path)
	if err == nil || !strings.Contains(err.Error(), `declares package "self"`) {
		t.Fatalf("validateGoSourceFile() error = %v, want rewritten-package rejection", err)
	}
}

// A partially repaired package with a rewritten top-level function remains
// corrupt and must not pass the adjacent integrity check.
func TestValidateGoSourceFileRejectsRewrittenFunction(t *testing.T) {
	path := writeSourceGuardFixture(t, "package main\nfunc self() {}\n")
	err := validateGoSourceFile(path)
	if err == nil || !strings.Contains(err.Error(), `top-level function "self"`) {
		t.Fatalf("validateGoSourceFile() error = %v, want rewritten-function rejection", err)
	}
}

// A partially repaired package with rewritten declarations is rejected for
// both type and value variants adjacent to the observed function corruption.
func TestValidateGoSourceFileRejectsRewrittenDeclarations(t *testing.T) {
	typePath := writeSourceGuardFixture(t, "package main\ntype self struct{}\n")
	typeErr := validateGoSourceFile(typePath)
	if typeErr == nil || !strings.Contains(typeErr.Error(), `top-level type "self"`) {
		t.Fatalf("validateGoSourceFile(type) error = %v, want rewritten-type rejection", typeErr)
	}

	valuePath := writeSourceGuardFixture(t, "package main\nvar self = 1\n")
	valueErr := validateGoSourceFile(valuePath)
	if valueErr == nil || !strings.Contains(valueErr.Error(), `top-level value "self"`) {
		t.Fatalf("validateGoSourceFile(value) error = %v, want rewritten-value rejection", valueErr)
	}
}

// Tree validation surfaces malformed adjacent files instead of accepting the
// first valid source file as representative of the directory.
func TestValidateGoSourceTreeChecksEveryFile(t *testing.T) {
	root := t.TempDir()
	validPath := filepath.Join(root, "valid.go")
	if err := os.WriteFile(validPath, []byte("package main\nfunc valid() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	corruptPath := filepath.Join(root, "corrupt.go")
	if err := os.WriteFile(corruptPath, []byte("package self\nfunc corrupt() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateGoSourceTree(root); err == nil {
		t.Fatal("validateGoSourceTree() accepted an adjacent corrupt source file")
	}
}
