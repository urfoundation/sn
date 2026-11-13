// Fixture provisioning is explicit; real runtime custody remains strict after
// the original directory has been admitted and a checkpoint has been written.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEconomicConservationFixtureProtectsFreshRootWithoutWeakeningCustody(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0770); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	parentBefore, err := os.Stat(filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	protectFreshEconomicConservationTestRoot(t, root)
	after, err := os.Stat(root)
	if err != nil || after.Mode().Perm() != 0700 || !os.SameFile(before, after) {
		t.Fatal("fresh conservation fixture root did not explicitly acquire private custody", err)
	}
	parentAfter, err := os.Stat(filepath.Dir(root))
	if err != nil || !os.SameFile(parentBefore, parentAfter) || parentBefore.Mode() != parentAfter.Mode() {
		t.Fatal("fixture provisioning altered its unrelated ancestor", err)
	}
	f := newEconomicConservationFixture(t, false)
	f.path, f.checkpoint = filepath.Join(root, "policy.json"), filepath.Join(root, "checkpoint.json")
	f.writePolicy(t)
	positive, code, issue := f.run(t, monitorServiceHooks{})
	if code != 0 || positive.NativeCursor.Number == f.policy.Native.Observation.From.Number || positive.VaultCursor.Number == f.policy.Vault.From.Number {
		t.Fatal("explicitly protected fixture did not reach actual public sources", code, issue)
	}
	if err := os.Chmod(root, 0770); err != nil {
		t.Fatal(err)
	}
	reads := f.claimReads.Load()
	_, code, issue = f.run(t, monitorServiceHooks{})
	after, err = os.Stat(root)
	if err != nil || code != 3 || reads != f.claimReads.Load() || !strings.Contains(issue, "not a protected physical object") || after.Mode().Perm() != 0770 {
		t.Fatal("runtime accepted or repaired later unprotected root custody", code, issue, err)
	}
}
