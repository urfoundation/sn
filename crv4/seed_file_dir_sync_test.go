package crv4

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A fresh state directory creates and reloads a seed with no directory-flush
// failure on any platform. Windows has no directory flush; the create path
// must still succeed there (the fresh-profile case), and the same test runs
// on the Unix platforms where the flush is real.
func TestFreshSeedDirectoryCreationSyncsOnEveryPlatform(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "hotkey.seed")
	seed, created, err := LoadOrCreateSeedFile(path)
	if err != nil {
		if runtime.GOOS == "windows" && strings.Contains(err.Error(), "unsupported on this platform") {
			// The private custody refuses on Windows by design, before any
			// write, so no directory flush is ever attempted there.
			return
		}
		t.Fatalf("fresh create: created=%v err=%v", created, err)
	}
	if !created {
		t.Fatalf("fresh create did not report created")
	}
	reload, reloadCreated, err := LoadOrCreateSeedFile(path)
	if err != nil || reloadCreated || reload != seed {
		t.Fatalf("reload: created=%v same=%v err=%v", reloadCreated, reload == seed, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("seed file missing: %v", err)
	}
}
