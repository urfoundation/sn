//go:build linux || darwin

// Bootstrap inspects the signed pending config as another account than the
// validator. Once the validator provisions its private custody directories,
// nothing below them is observable to that account, and inspection must not
// depend on it. The validator's own loaders still check every path physically.
package validator

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// Takes every permission from directory for the rest of the test. A lookup
// below it then fails with EACCES, exactly as below a 0700 directory another
// account owns, without root or a second account. A process that bypasses
// search permission (root) cannot observe that refusal, so the test skips.
// The original mode is restored before the test's directories are removed.
func custodyTestUntraversable(t *testing.T, directory string) {
	t.Helper()
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() {
		t.Fatal("untraversable control needs an existing directory", directory, err)
	}
	if err := os.Chmod(directory, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(directory, info.Mode().Perm()); err != nil {
			t.Error("untraversable test directory was not restored", directory, err)
		}
	})
	probe := filepath.Join(directory, "probe")
	if _, err := os.Lstat(probe); !errors.Is(err, syscall.EACCES) {
		t.Skipf("this process can search a mode 0000 directory (running as root?), so another account's private directory cannot be simulated: lstat %s: %v", probe, err)
	}
}

// The inspection reads only its pinned config bytes and the approval held in
// bootstrap custody. Declared paths are admitted by spelling, distinctness and
// overlap, so neither a directory private to another account nor an
// untraversable parent changes its result, and both rules still refuse.
func TestProductionBootstrapPreActivationIgnoresUnobservableDeclaredPaths(t *testing.T) {
	f := newProductionPendingTestFixture(t, true)
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := InspectProductionBootstrapConfigPreActivation(t.Context(), f.path, raw)
	if err != nil || want == nil || !want.EvidenceActivationPending {
		t.Fatal("control: the clean pending config was not inspected", err)
	}
	// Signed declarations refused by shape and by overlap. Signing normalizes
	// with the physical walk, so they are prepared while paths can be walked.
	refusals := map[string]string{
		f.variant(t, func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.EvidenceV2.Operators[0].SealScratchRoot += string(filepath.Separator)
		}): "canonical absolute non-root",
		f.variant(t, func(cfg *ReleaseConfig, _ *OwnerRecycleApproval) {
			cfg.EvidenceV2.Operators[1].ReplayScratchRoot = filepath.Join(cfg.Operators[0].StateDir, "replay")
		}): "overlaps protected state",
	}
	// The validator account provisions its private custody directories, all
	// below the fixture's one private root.
	root := filepath.Dir(f.cfg.StateDir)
	directories := []string{f.cfg.StateDir}
	for _, operator := range f.cfg.Operators {
		directories = append(directories, operator.StateDir)
	}
	for _, operator := range f.cfg.EvidenceV2.Operators {
		directories = append(directories, filepath.Dir(operator.Activation.Path), operator.ReplayScratchRoot, operator.SealScratchRoot)
	}
	for _, directory := range directories {
		if !strings.HasPrefix(directory, root+string(filepath.Separator)) {
			t.Fatal("fixture declares custody outside its private root", directory)
		}
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	inspect := func(stage string) {
		t.Helper()
		got, err := InspectProductionBootstrapConfigPreActivation(t.Context(), f.path, raw)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: declared custody changed the pre-activation inspection: %v", stage, err)
		}
	}
	inspect("provisioned")
	// Another account's directory is never owner-private to this one; group
	// bits stand in for that foreign owner here.
	if err := os.Chmod(f.cfg.StateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReleaseProductionConfigPreActivation(f.path); err == nil || !strings.Contains(err.Error(), "directory is not owner-private") {
		t.Fatal("control: the validator loader admitted a state directory that is not owner-private", err)
	}
	inspect("foreign state directory")
	if err := os.Chmod(f.cfg.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Below an untraversable root no declared path can even be stat'd.
	custodyTestUntraversable(t, root)
	if _, err := LoadReleaseProductionConfigPreActivation(f.path); !errors.Is(err, syscall.EACCES) {
		t.Fatal("control: the validator loader did not walk its declared paths", err)
	}
	inspect("untraversable root")
	for path, diagnostic := range refusals {
		variant, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := InspectProductionBootstrapConfigPreActivation(t.Context(), path, variant); got != nil || err == nil || !strings.Contains(err.Error(), diagnostic) {
			t.Errorf("declared-only inspection admitted a bad declaration or refused it for another cause (want %q): %v", diagnostic, err)
		}
	}
}
