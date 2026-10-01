// Directory and nonce fences are exercised across distinct physical roots.
// Recovery refuses corruption, substitution and ambiguous partial outcomes.
package main

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A competing original root cannot evade either global nonce domain by changing
// the other nonce. Its failed claim remains evidence and never gains a send.
func TestBootstrapSuccessorExecutionSeparatelyFencesGlobalNonceDomains(t *testing.T) {
	for _, pair := range []struct {
		safe  string
		outer uint64
	}{{safe: "17", outer: 43}, {safe: "18", outer: 42}} {
		first := newBootstrapSuccessorExecutionFixture(t)
		owner := first.open(true, nil)
		owner.close()
		second := newBootstrapSuccessorExecutionNonceFixture(t, pair.safe, pair.outer)
		second.approval.Plan.Request.RegistryDirectory = first.approval.Plan.Request.RegistryDirectory
		second.approval.Plan.Registry = first.approval.Plan.Registry
		second.approval = bootstrapSuccessorExecutionTestSign(t, second.approval.Plan, second.key, second.profile)
		before := bootstrapSuccessorPreparationTestFiles(t, first.approval.Plan.Request.RegistryDirectory)
		competing, err := openBootstrapSuccessorExecutionStore(t.Context(), second.approval.Plan, second.approval, second.profile, true, nil)
		if competing != nil {
			competing.close()
		}
		if err == nil || !strings.Contains(err.Error(), "retained publication differs") {
			t.Fatal("another root acquired a consumed inner or outer nonce", pair, err)
		}
		after := bootstrapSuccessorPreparationTestFiles(t, first.approval.Plan.Request.RegistryDirectory)
		for name, raw := range before {
			if after[name] != raw {
				t.Fatal("competing nonce claim overwrote original custody")
			}
		}
		owner = first.open(false, nil)
		if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, first, true); err != nil || len(first.writes) != 1 {
			t.Fatal("competing claim invalidated original owner", err)
		}
	}
}

// Holding one owner deterministically excludes another before any signatures
// are usable, even if its original root differs but registry is the same.
func TestBootstrapSuccessorExecutionSerializesRootAndRegistryOwners(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	contender, err := openBootstrapSuccessorExecutionStore(t.Context(), f.approval.Plan, f.approval, f.profile, false, nil)
	if contender != nil {
		contender.close()
	}
	if err == nil || !strings.Contains(err.Error(), "active local owner") {
		t.Fatal("execution escaped physical root ownership", err)
	}
	second := newBootstrapSuccessorExecutionNonceFixture(t, "99", 199)
	second.approval.Plan.Request.RegistryDirectory, second.approval.Plan.Registry = f.approval.Plan.Request.RegistryDirectory, f.approval.Plan.Registry
	second.approval = bootstrapSuccessorExecutionTestSign(t, second.approval.Plan, second.key, second.profile)
	contender, err = openBootstrapSuccessorExecutionStore(t.Context(), second.approval.Plan, second.approval, second.profile, true, nil)
	if contender != nil {
		contender.close()
	}
	if err == nil || !strings.Contains(err.Error(), "registry already has an owner") {
		t.Fatal("execution escaped global registry ownership", err)
	}
	owner.close()
	contender = second.open(true, nil)
	contender.close()
}

// Completed adoption and nonce claims cannot be recreated after deletion; links,
// writable permissions and event sequence gaps also fail without broadcasting.
func TestBootstrapSuccessorExecutionRefusesMissingReboundAndUnsafeCustody(t *testing.T) {
	for _, fault := range []string{"missing-claim", "changed-claim", "missing-nonce", "changed-nonce", "missing-adoption", "missing-intent", "record-mode", "record-hardlink", "event-gap", "foreign-stage"} {
		f := newBootstrapSuccessorExecutionFixture(t)
		owner := f.open(true, nil)
		owner.close()
		directory := f.approval.Plan.Review.Preparation.Approval.Plan.Proposal.OriginalRunDirectory
		path := filepath.Join(directory, bootstrapSuccessorExecutionEventName(0)+".json")
		switch fault {
		case "missing-claim", "changed-claim":
			path = filepath.Join(directory, bootstrapSuccessorExecutionPrefix+".claim")
		case "missing-nonce", "changed-nonce":
			path = filepath.Join(f.approval.Plan.Request.RegistryDirectory, f.approval.Plan.nonceNames()[0])
		case "missing-intent":
			path = filepath.Join(directory, bootstrapSuccessorExecutionEventName(0)+".intent")
		}
		var err error
		switch {
		case strings.HasPrefix(fault, "missing-"):
			err = os.Remove(path)
		case strings.HasPrefix(fault, "changed-"):
			err = os.WriteFile(path, []byte("synthetic changed custody"), 0600)
		case fault == "record-mode":
			err = os.Chmod(path, 0644)
		case fault == "record-hardlink":
			err = os.Link(path, filepath.Join(t.TempDir(), "synthetic-custody-link"))
		case fault == "event-gap":
			err = os.WriteFile(filepath.Join(directory, bootstrapSuccessorExecutionEventName(2)+".intent"), []byte("{}"), 0600)
		case fault == "foreign-stage":
			err = os.WriteFile(filepath.Join(directory, bootstrapSuccessorExecutionStagePrefix+"synthetic-foreign"), nil, 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		before := bootstrapSuccessorPreparationTestFiles(t, directory)
		owner, err = openBootstrapSuccessorExecutionStore(t.Context(), f.approval.Plan, f.approval, f.profile, false, nil)
		if owner != nil {
			owner.close()
		}
		if err == nil || len(f.writes) != 0 || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, directory)) {
			t.Fatal("execution repaired or accepted missing/unsafe custody", fault, err)
		}
	}
}

// Empty terminal stages block attempts until their exact result is reconciled.
// The same partial bytes cannot be relabeled as another result or reservation.
func TestBootstrapSuccessorExecutionPendingOutcomeCannotBecomeAnotherSend(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err != nil {
		t.Fatal(err)
	}
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
	owner.local.hook = func(stage string) error {
		if stage == bootstrapSuccessorExecutionEventName(2)+".intent:name-synced" {
			return errors.New("synthetic empty terminal stage")
		}
		return nil
	}
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err == nil {
		t.Fatal("completion stage did not interrupt")
	}
	owner = f.open(false, nil)
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "absent"}
	if _, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true); err == nil || len(f.writes) != 1 || owner.last.CumulativeAttempts != 9 {
		t.Fatal("partial terminal stage became a fresh send", err)
	}
}

// Copying the registry changes its signed physical identity, including a clone
// with every original nonce file. Ordinary same-inode reopen remains supported.
func TestBootstrapSuccessorExecutionFencesRegistryReplacement(t *testing.T) {
	f := newBootstrapSuccessorExecutionFixture(t)
	owner := f.open(true, nil)
	owner.close()
	path := f.approval.Plan.Request.RegistryDirectory
	before := bootstrapSuccessorPreparationTestFiles(t, path)
	moved := path + "-synthetic-moved"
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(moved) })
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range before {
		if err := os.WriteFile(filepath.Join(path, name), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := openBootstrapSuccessorExecutionStore(t.Context(), f.approval.Plan, f.approval, f.profile, false, nil)
	if owner != nil {
		owner.close()
	}
	if err == nil || !strings.Contains(err.Error(), "physical directory changed") || !maps.Equal(before, bootstrapSuccessorPreparationTestFiles(t, path)) {
		t.Fatal("copied registry acquired the original signed custody", err)
	}
}
