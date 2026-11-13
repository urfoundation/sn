//go:build linux || darwin || windows

// A regular-file stdout (nohup, systemd file:, CI capture, Windows redirect)
// must receive records; it used to be refused, leaving startup refusals silent.
package diagnostics

import (
	"os"
	"path/filepath"
	"testing"
)

// The record lands in the file and the caller's handle survives exporter close.
func TestExporterDeliversToRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stdout.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	owner, err := New(t.Context(), file, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Offer("alpha", []byte("bounded record\n")) {
		t.Fatal("regular file refused", owner.Snapshot("alpha"))
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if state := owner.Snapshot("alpha"); state.Delivered != 1 || state.Dropped != 0 {
		t.Fatal("regular file record not delivered", state)
	}
	if _, err := file.WriteString("caller line\n"); err != nil {
		t.Fatal("caller handle closed by exporter", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "bounded record\ncaller line\n" {
		t.Fatalf("unexpected file contents %q", raw)
	}
}
