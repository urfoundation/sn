package miner

import (
	"os"
	"testing"
)

// TestMain captures after every dependency and miner init has finished,
// before testing.M.Run intentionally aliases stderr in test2json mode.
var minerLibraryStreamsIndependent bool

// All library initialization has completed here. A package variable's own
// initializer would run too early to observe side effects of miner init.
func TestMain(m *testing.M) {
	minerLibraryStreamsIndependent = os.Stdout != os.Stderr && os.Stdout.Fd() != os.Stderr.Fd()
	os.Exit(m.Run())
}

// The observed boundary is library initialization, not the later test runner.
func TestLibraryInitializationKeepsStdoutAndStderrIndependent(t *testing.T) {
	if !minerLibraryStreamsIndependent {
		t.Fatal("importing the miner library aliased stderr to stdout")
	}
}
