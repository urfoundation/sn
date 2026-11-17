// Original bootstrap actions keep their physical marker and retained journal
// through network reads, counted publication and result admission.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// A synchronous boundary follows the real owned route's full reconciliation.
// No scheduler timing is needed to lose custody immediately before admission.
type evmActionCustodyChain struct {
	evmActionChain
	afterReconcile func(evmCreatePlan)
}

// Synthetic filesystem faults happen only after successful chain observation.
func (self *evmActionCustodyChain) reconcile(ctx context.Context, plan evmCreatePlan, record evmActionRecord) (evmActionObservation, error) {
	observation, err := self.evmActionChain.reconcile(ctx, plan, record)
	if err == nil && self.afterReconcile != nil {
		self.afterReconcile(plan)
	}
	return observation, err
}

// Replacing a live marker with identical bytes can admit another flock owner.
// The original owner must detect its lost physical fence before counting/sending.
func TestEvmCreateLostLiveMarkerRefusesSend(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	store, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.client.httpClient.CloseIdleConnections)
	replaced := false
	controlled := &evmActionCustodyChain{evmActionChain: chain, afterReconcile: func(evmCreatePlan) {
		markerPath := store.path + ".lock"
		marker, err := os.ReadFile(markerPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(markerPath, markerPath+".retained"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(markerPath, marker, 0600); err != nil {
			t.Fatal(err)
		}
		replacement, err := os.OpenFile(markerPath, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Flock(int(replacement.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			t.Fatalf("different physical inode did not admit a second flock: %v", err)
		}
		if err := replacement.Close(); err != nil {
			t.Fatal(err)
		}
		other, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
		if other != nil {
			_ = other.close()
		}
		if !errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatalf("replacement silently became production custody: %v", err)
		}
		replaced = true
	}}
	owner, err := newEvmCreateOwner(f.plan, store, controlled)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.advance(t.Context(), nil, true, true)
	after, readErr := os.ReadFile(store.path)
	if !replaced || err == nil || len(f.writes) != 0 || readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("lost live marker reached a counted send: status=%s error=%v writes=%d unchanged=%v read=%v", result.Status, err, len(f.writes), bytes.Equal(before, after), readErr)
	}
}

// A completed signed journal lost during reconciliation is missing custody,
// even when the live owner still has a valid copy in memory.
func TestEvmCreateMissingLiveJournalRefusesRecreation(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	store, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.client.httpClient.CloseIdleConnections)
	controlled := &evmActionCustodyChain{evmActionChain: chain, afterReconcile: func(evmCreatePlan) {
		if err := os.Remove(store.path); err != nil {
			t.Fatal(err)
		}
	}}
	owner, err := newEvmCreateOwner(f.plan, store, controlled)
	if err != nil {
		t.Fatal(err)
	}
	result, err := owner.advance(t.Context(), nil, true, true)
	_, statErr := os.Lstat(store.path)
	if err == nil || len(f.writes) != 0 || !os.IsNotExist(statErr) {
		t.Fatalf("missing live journal was recreated or sent: status=%s error=%v writes=%d journal=%v", result.Status, err, len(f.writes), statErr)
	}
}

// A durability callback runs after the counted record has been renamed.
// Losing that record before send must retain the attempt loss without a write.
func TestEvmCreateMissingCountedJournalRefusesSend(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	store, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.close() })
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.client.httpClient.CloseIdleConnections)
	owner, err := newEvmCreateOwner(f.plan, store, chain)
	if err != nil {
		t.Fatal(err)
	}
	publications := 0
	store.syncDirectory = func(directory *os.File) error {
		publications++
		if publications == 2 {
			if err := os.Rename(store.path, filepath.Join(f.config.Plan.RunDirectory, "retained-counted.json")); err != nil {
				t.Fatal(err)
			}
		}
		return directory.Sync()
	}
	result, err := owner.advance(t.Context(), nil, true, true)
	if publications != 2 || err == nil || len(f.writes) != 0 {
		t.Fatalf("missing counted journal reached transport: publications=%d status=%s error=%v writes=%d", publications, result.Status, err, len(f.writes))
	}
	var retained evmActionRecord
	raw, err := os.ReadFile(filepath.Join(f.config.Plan.RunDirectory, "retained-counted.json"))
	if err != nil || decodePlanJson(raw, &retained) != nil || retained.Attempts != 1 || retained.TransactionHash != f.tx.Hash().Hex() {
		t.Fatalf("fault did not retain the original counted liability: record=%+v error=%v", retained, err)
	}
}

// Every action schema shares the same live physical and retained-file checks.
// These are storage faults, separate from the owner's canonical receipt tests.
func TestEvmActionLiveCustodyRejectsPhysicalAndRecordChanges(t *testing.T) {
	for actionIndex := 0; actionIndex < 8; actionIndex++ {
		for _, fault := range []string{"marker-missing", "marker-bytes", "marker-permissions", "marker-link", "journal-missing", "journal-bytes", "journal-permissions", "journal-link", "directory-replaced", "directory-symlink"} {
			f := newEvmEvidenceFixture(t)
			predecessor := ""
			if actionIndex != 0 {
				predecessor = "sha256:" + strings.Repeat("1", 64)
			}
			store, err := openEvmSelectedActionStore(f.config, actionIndex, predecessor, true, nil, f.storage.Context)
			if err != nil {
				t.Fatal(err)
			}
			record, err := store.load()
			if err != nil {
				t.Fatal(err)
			}
			path := store.path
			if strings.HasPrefix(fault, "marker-") {
				path += ".lock"
			}
			switch fault {
			case "marker-missing", "journal-missing":
				err = os.Remove(path)
			case "marker-bytes", "journal-bytes":
				err = os.WriteFile(path, []byte("changed retained custody\n"), 0600)
			case "marker-permissions", "journal-permissions":
				err = os.Chmod(path, 0644)
			case "marker-link", "journal-link":
				err = os.Link(path, path+".alias")
			case "directory-replaced", "directory-symlink":
				directory := f.config.Plan.RunDirectory
				err = os.Rename(directory, directory+".retained")
				if err == nil && fault == "directory-symlink" {
					err = os.Symlink(directory+".retained", directory)
				} else if err == nil {
					err = os.Mkdir(directory, 0700)
					for _, suffix := range []string{"", ".lock"} {
						raw, readErr := os.ReadFile(filepath.Join(directory+".retained", filepath.Base(path)+suffix))
						if readErr != nil {
							t.Fatal(readErr)
						}
						err = errors.Join(err, os.WriteFile(path+suffix, raw, 0600))
					}
				}
			}
			if err != nil {
				t.Fatalf("fault %s: %v", fault, err)
			}
			_, loadErr := store.load()
			saveErr := store.save(record)
			if loadErr == nil || saveErr == nil {
				t.Errorf("action %d accepted %s: load=%v save=%v", actionIndex, fault, loadErr, saveErr)
			}
			if err := store.close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Restoring an older valid signed journal does not renew an in-process budget.
// An identical current record remains readable and normal reopening still works.
func TestEvmCreateLiveCustodyRejectsValidJournalRollback(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	f.mine = false
	path := filepath.Join(f.config.Plan.RunDirectory, evmCreateStateFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	store, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	retained, err := store.load()
	if err != nil || retained.Attempts != 1 {
		t.Fatalf("original counted journal unavailable: attempts=%d error=%v", retained.Attempts, err)
	}
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.load(); err == nil {
		t.Error("valid earlier signed journal reset live custody")
	}
	if err := store.save(retained); err == nil {
		t.Error("live owner silently repaired a rolled-back journal")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rollback refusal rewrote custody: %v", err)
	}
}

// A receipt published just before its journal disappears cannot become a
// successful created-contract report from the owner's cached record.
func TestEvmCreateMissingTerminalJournalRefusesResult(t *testing.T) {
	f := newEvmCreateFixture(t)
	f.prepareSigned()
	if _, code, diagnostic := f.command("resume", "--online", "--submit"); code != 0 {
		t.Fatal(diagnostic)
	}
	store, err := openEvmActionStore(f.config, false, nil, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	chain, err := newEvmOwnedChain(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.client.httpClient.CloseIdleConnections()
	owner, err := newEvmCreateOwner(f.plan, store, chain)
	if err != nil {
		t.Fatal(err)
	}
	terminal := false
	store.syncDirectory = func(directory *os.File) error {
		raw, err := os.ReadFile(store.path)
		var record evmActionRecord
		if err != nil || json.Unmarshal(raw, &record) != nil || record.Receipt == nil || record.Attempts != 1 {
			t.Fatal("fault did not follow the original terminal receipt publication")
		}
		terminal = true
		return errors.Join(os.Remove(store.path), directory.Sync())
	}
	result, err := owner.advance(t.Context(), nil, true, true)
	if !terminal || err == nil || result.Receipt != nil || len(f.writes) != 1 {
		t.Fatalf("lost terminal custody became a result: status=%s receipt=%v error=%v writes=%d", result.Status, result.Receipt != nil, err, len(f.writes))
	}
}

// The eighth action must retain every earlier physical claim after its network
// audit and again after its last counted publication before the actual write.
func TestEvmEvidenceCreateLostPredecessorCustodyRefusesSend(t *testing.T) {
	for _, boundary := range []string{"reconciliation", "counted-publication"} {
		f := newEvmEvidenceFixture(t)
		f.prepareEvidenceSigned()
		stores, records := f.openEvidenceAncestors()
		store, err := openEvmEvidenceActionStore(f.plan, records[0], records[1], records[2], records[3], records[4], records[5], records[6], false, nil, f.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.close() })
		chain, err := newEvmOwnedChain(f.config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(chain.client.httpClient.CloseIdleConnections)
		changed := false
		change := func() {
			for _, prior := range stores {
				if err := os.Remove(prior.path + ".lock"); err != nil {
					t.Fatal(err)
				}
			}
			changed = true
		}
		controlled := &evmActionCustodyChain{evmActionChain: chain, afterReconcile: func(plan evmCreatePlan) {
			if boundary == "reconciliation" && plan.ActionIndex == 7 {
				change()
			}
		}}
		publications := 0
		store.syncDirectory = func(directory *os.File) error {
			publications++
			if boundary == "counted-publication" && publications == 2 {
				change()
			}
			return directory.Sync()
		}
		owner, err := newEvmEvidenceCreateOwner(f.plan, store, stores[0], stores[1], stores[2], stores[3], stores[4], stores[5], stores[6], controlled)
		if err != nil {
			t.Fatal(err)
		}
		result, err := owner.advance(t.Context(), nil, true, true)
		if !changed || err == nil || len(f.writes) != 7 {
			t.Fatalf("lost predecessor custody at %s reached evidence send: status=%s error=%v writes=%d", boundary, result.Status, err, len(f.writes))
		}
		retained, err := store.load()
		wantAttempts := uint8(0)
		if boundary == "counted-publication" {
			wantAttempts = 1
		}
		if err != nil || retained.Attempts != wantAttempts || retained.TransactionHash != f.tx.Hash().Hex() {
			t.Fatalf("predecessor refusal changed original allowance: attempts=%d want=%d error=%v", retained.Attempts, wantAttempts, err)
		}
	}
}
