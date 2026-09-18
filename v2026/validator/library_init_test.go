package validator

import (
	"os"
	"testing"
)

// TestMain captures after every dependency and validator init has finished,
// before testing.M.Run intentionally aliases stderr in test2json mode.
var validatorLibraryStreamsIndependent bool

// All library initialization has completed here. A package variable's own
// initializer would run too early to observe side effects of validator init.
func TestMain(m *testing.M) {
	validatorLibraryStreamsIndependent = independentLibraryStreams(os.Stdout, os.Stderr)
	os.Exit(m.Run())
}

// Separate nonnil file owners and descriptors are required. This predicate
// never mutates process streams and also rejects two wrappers of one descriptor.
func independentLibraryStreams(stdout, stderr *os.File) bool {
	return stdout != nil && stderr != nil && stdout != stderr && stdout.Fd() != stderr.Fd()
}

// The observed boundary is library initialization, not the later test runner.
func TestLibraryInitializationKeepsStdoutAndStderrIndependent(t *testing.T) {
	if !validatorLibraryStreamsIndependent {
		t.Fatal("importing the validator library aliased stderr to stdout")
	}
}

// Distinct private files pass while a shared owner and missing owners fail,
// without changing either process-global stream or depending on test order.
func TestLibraryStandardStreamsRejectAliasedAndMissingOwners(t *testing.T) {
	first, err := os.CreateTemp(t.TempDir(), "first-")
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.CreateTemp(t.TempDir(), "second-")
	if err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Error(err)
		}
		if err := second.Close(); err != nil {
			t.Error(err)
		}
	})
	if !independentLibraryStreams(first, second) {
		t.Fatal("distinct file owners were aliased")
	}
	if independentLibraryStreams(first, first) || independentLibraryStreams(nil, second) || independentLibraryStreams(first, nil) {
		t.Fatal("aliased or missing file owner was independent")
	}
}
