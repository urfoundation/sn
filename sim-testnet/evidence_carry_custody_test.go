// Historical file custody is established by the native opener before reads.
// Public override receipts retain the same complete v4 checkpoint contract.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// A held peer makes the old blocking syscall return deterministically. Fd
// exposes its caller-visible native mode, including Go's restoration of any
// nonblocking bit added only after Open. After that causal assertion passes,
// the same production reader must acquire and refuse the FIFO without a peer.
func TestValidatorEvidenceCarryHistoricalFilesUseNativeNonblockingOpen(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"plan.json", "plans/source.json", "transactions/source.rlp", "receipts/postconditions/source.json"} {
		func() {
			directory := filepath.Join(t.TempDir(), "owner")
			path := filepath.Join(directory, filepath.FromSlash(name))
			original := []byte("1234")
			if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
				t.Fatal(err)
			}
			if err := atomicWrite(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if raw, err := readValidatorEvidenceHistoricalFile(directory, name, int64(len(original))); err != nil || !bytes.Equal(raw, original) {
				t.Fatalf("%s exact regular-file prerequisite: %v", name, err)
			}
			if err := os.Rename(path, path+".original"); err != nil {
				t.Fatal(err)
			}
			if err := unix.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			keeper, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			keeperClosed := false
			defer func() {
				if !keeperClosed {
					if err := unix.Close(keeper); err != nil {
						t.Error(err)
					}
				}
			}()
			refusedBlocking := errors.New("historical reader exposed a blocking native descriptor")
			opened, nativeFlags := 0, 0
			raw, err := readValidatorEvidenceHistoricalFileObserved(directory, name, int64(len(original)), func(file *os.File) error {
				opened++
				info, err := file.Stat()
				if err != nil {
					return err
				}
				if info.Mode()&os.ModeNamedPipe == 0 {
					return errors.New("historical fixture did not acquire its actual FIFO")
				}
				nativeFlags, err = unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
				if err != nil {
					return err
				}
				if nativeFlags&unix.O_NONBLOCK == 0 {
					return refusedBlocking
				}
				return nil
			})
			if opened != 1 || nativeFlags&unix.O_NONBLOCK == 0 || errors.Is(err, refusedBlocking) || err == nil || raw != nil {
				t.Fatalf("%s historical FIFO acquisition is not natively nonblocking: opened=%d flags=%d error=%v", name, opened, nativeFlags, err)
			}
			if err := unix.Close(keeper); err != nil {
				t.Fatal(err)
			}
			keeperClosed = true
			if raw, err := readValidatorEvidenceHistoricalFile(directory, name, int64(len(original))); err == nil || raw != nil {
				t.Fatalf("%s writer-free FIFO produced historical bytes: %v", name, err)
			}
			preserved, err := os.ReadFile(path + ".original")
			if err != nil || !bytes.Equal(preserved, original) {
				t.Fatalf("%s FIFO refusal changed the original regular file: %v", name, err)
			}
		}()
	}
}

// Root, parent and leaf aliases back to the exact same inode must not turn
// namespace indirection into approved historical source authority.
func TestValidatorEvidenceCarryHistoricalFilesRejectSameOwnerAliases(t *testing.T) {
	t.Parallel()
	for _, location := range []string{"root", "parent", "leaf"} {
		directory := filepath.Join(t.TempDir(), "owner")
		name := "plans/source.json"
		path := filepath.Join(directory, filepath.FromSlash(name))
		original := []byte("1234")
		if err := ensurePrivateDir(filepath.Dir(path)); err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if raw, err := readValidatorEvidenceHistoricalFile(directory, name, int64(len(original))); err != nil || !bytes.Equal(raw, original) {
			t.Fatalf("%s exact source prerequisite: %v", location, err)
		}
		switch location {
		case "root":
			alias := directory + "-alias"
			if err := os.Symlink(directory, alias); err != nil {
				t.Fatal(err)
			}
			directory = alias
		case "parent":
			parent := filepath.Dir(path)
			if err := os.Rename(parent, parent+"-original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Base(parent)+"-original", parent); err != nil {
				t.Fatal(err)
			}
		case "leaf":
			if err := os.Rename(path, path+".original"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Base(path)+".original", path); err != nil {
				t.Fatal(err)
			}
		}
		aliasedPath := filepath.Join(directory, filepath.FromSlash(name))
		after, err := os.Stat(aliasedPath)
		if err != nil || !os.SameFile(before, after) {
			t.Fatalf("%s alias lost its exact same-inode prerequisite: %v", location, err)
		}
		if raw, err := readValidatorEvidenceHistoricalFile(directory, name, int64(len(original))); err == nil || raw != nil {
			t.Fatalf("%s historical alias acquired source authority: %v", location, err)
		}
		preserved, err := os.ReadFile(aliasedPath)
		if err != nil || !bytes.Equal(preserved, original) {
			t.Fatalf("%s alias refusal mutated source bytes: %v", location, err)
		}
	}
}

// Directory-only admission must reject special root/parent components before
// opening them as streams; leaf directories cannot become historical bytes.
func TestValidatorEvidenceCarryHistoricalFilesRejectSpecialComponents(t *testing.T) {
	t.Parallel()
	for _, location := range []string{"root-fifo", "parent-fifo", "leaf-directory"} {
		directory := filepath.Join(t.TempDir(), "owner")
		if err := ensurePrivateDir(directory); err != nil {
			t.Fatal(err)
		}
		name := "source.json"
		switch location {
		case "root-fifo":
			fifo := filepath.Join(directory, "root")
			if err := unix.Mkfifo(fifo, 0o600); err != nil {
				t.Fatal(err)
			}
			directory = fifo
		case "parent-fifo":
			if err := unix.Mkfifo(filepath.Join(directory, "parent"), 0o600); err != nil {
				t.Fatal(err)
			}
			name = "parent/source.json"
		case "leaf-directory":
			if err := ensurePrivateDir(filepath.Join(directory, name)); err != nil {
				t.Fatal(err)
			}
		}
		if raw, err := readValidatorEvidenceHistoricalFile(directory, name, 4); err == nil || raw != nil {
			t.Fatalf("%s special component became historical bytes: %v", location, err)
		}
	}
}

// Nonrequired means one honestly labelled provider, not absent v4 evidence.
// The real persisted public receipt and signed journal succeed with nil
// independent client; replacing a cloned head must fail even with a new hash.
func TestValidatorEvidenceCarryPublicOverrideRequiresCompleteClonedHeads(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, false)
	fixture.authenticate(t)
	executor := fixture.executor
	if independentRPCRequired(executor.cfg) || executor.independentEVM != nil {
		t.Fatal("public-override fixture unexpectedly requires a second provider")
	}
	action := actionByID(t, executor.plan, validatorEvidenceDeployActionID)
	_, _, verified, err := validatorEvidenceHistoryReceipt(executor.plan, executor.journal.Entries(), action)
	if err != nil {
		t.Fatal(err)
	}
	record, err := readValidatorEvidenceSourcePostcondition(executor.stateDir, executor.cfg, executor.plan, verified)
	if err != nil {
		t.Fatalf("actual public source receipt prerequisite: %v", err)
	}
	if record.IndependentRPC || record.EVMFinalized != record.IndependentEVMFinalized || record.SubstrateFinalized != record.IndependentSubstrateFinalized || !finalJSONEqual(record.Observed, record.IndependentObserved) {
		t.Fatal("actual public v4 receipt does not contain complete shared-provider clones")
	}
	path := filepath.Join(executor.stateDir, filepath.FromSlash(verified.PostconditionPath))
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"zero-independent-evm", "zero-independent-native", "independent-before-inclusion", "different-shared-hash", "false-independence"} {
		changed, err := decodeActionPostcondition(original)
		if err != nil {
			t.Fatal(err)
		}
		switch fault {
		case "zero-independent-evm":
			changed.IndependentEVMFinalized = ChainHead{}
		case "zero-independent-native":
			changed.IndependentSubstrateFinalized = ChainHead{}
		case "independent-before-inclusion":
			changed.IndependentEVMFinalized.Number--
		case "different-shared-hash":
			changed.IndependentEVMFinalized.Hash = testEVMHead(changed.EVMFinalized.Number, 0x8f).Hash
		case "false-independence":
			changed.IndependentRPC = true
		}
		hash, err := canonicalHashHex(changed)
		if err != nil {
			t.Fatal(err)
		}
		if hash == verified.PostconditionHash {
			t.Fatalf("%s did not alter the real receipt", fault)
		}
		raw, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := atomicWrite(path, append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		entries := executor.journal.Entries()
		changedEntries := 0
		for index := range entries {
			if entries[index].Sequence == verified.Sequence {
				entries[index].PostconditionHash = hash
				changedEntries++
			}
		}
		if changedEntries != 1 {
			t.Fatalf("%s has %d original receipt rows", fault, changedEntries)
		}
		beforeCalls := fixture.rpc.calls.Load()
		observed, err := authenticateValidatorEvidenceCarry(t.Context(), executor.cfg, executor.stateDir, executor.plan, entries, executor.deployer.client, nil)
		if err == nil || observed != nil || fixture.rpc.calls.Load() != beforeCalls {
			t.Fatalf("%s malformed public receipt reached historical authority: %v", fault, err)
		}
		if err := atomicWrite(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fixture.authenticate(t)
}
