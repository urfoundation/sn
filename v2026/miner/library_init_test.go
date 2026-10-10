package miner

import (
	"os"
	"testing"
)

func TestLibraryInitializationKeepsStdoutAndStderrIndependent(t *testing.T) {
	if os.Stdout == os.Stderr || os.Stdout.Fd() == os.Stderr.Fd() {
		t.Fatal("importing the miner library aliased stderr to stdout")
	}
}
