//go:build linux || darwin

package validator

// Actual native transitions force the reader/write-owner races without
// sleeps, fake crypto, synthetic contents or injected acceptance results.

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// A real peerless FIFO replaces the observed predecessor before actual Openat.
// The opened flags and descriptor refusal prove nonblocking custody directly.
func TestHeadEMAStoreV2RuntimePredecessorFIFOOpensNonblocking(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	opened := false
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, file *os.File) error {
			if step == "predecessor-observed" {
				if err := os.Rename(fixture.path, fixture.path+".held"); err != nil {
					return err
				}
				return unix.Mkfifo(fixture.path, 0o600)
			}
			if step == "predecessor-opened" {
				opened = true
				flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
				if err != nil {
					return err
				}
				if flags&unix.O_NONBLOCK == 0 {
					t.Error("actual FIFO descriptor lacks native nonblocking flags")
				}
			}
			return nil
		}})
	if err == nil || out != nil || records != nil || !opened {
		t.Fatalf("FIFO transition escaped actual open/refusal: opened=%t error=%v", opened, err)
	}
	held, readErr := os.ReadFile(fixture.path + ".held")
	if readErr != nil || !bytes.Equal(held, fixture.encoded) {
		t.Fatalf("FIFO transition changed retained bytes: %v", readErr)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimePredecessorFIFOOpensNonblocking")
}

// Initial target observation cannot authorize a later symlink to another file.
func TestHeadEMAStoreV2RuntimePredecessorSymlinkNeverRedirects(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	foreign := fixture.path + ".foreign"
	foreignBytes := []byte("private foreign contents must survive")
	if err := os.WriteFile(foreign, foreignBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
			if step != "predecessor-observed" {
				return nil
			}
			if err := os.Rename(fixture.path, fixture.path+".held"); err != nil {
				return err
			}
			return os.Symlink(foreign, fixture.path)
		}})
	if err == nil || out != nil || records != nil || !errors.Is(err, unix.ELOOP) {
		t.Fatalf("symlink redirect lacks real no-follow refusal: %v", err)
	}
	after, err := os.ReadFile(foreign)
	if err != nil || !bytes.Equal(after, foreignBytes) {
		t.Fatalf("symlink target changed: %v", err)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimePredecessorSymlinkNeverRedirects")
}

// A retained native directory cannot be retargeted to a replacement namespace.
func TestHeadEMAStoreV2RuntimeRejectsParentReplacementBeforeWrite(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	held := fixture.stateDir + ".held"
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
			if step != "directory-opened" {
				return nil
			}
			if err := os.Rename(fixture.stateDir, held); err != nil {
				return err
			}
			if err := os.Mkdir(fixture.stateDir, 0o700); err != nil {
				return err
			}
			return os.WriteFile(fixture.path, fixture.encoded, 0o600)
		}})
	if err == nil || out != nil || records != nil {
		t.Fatalf("replacement parent became authority: %v", err)
	}
	for _, path := range []string{fixture.path, filepath.Join(held, "head-ema.json")} {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, fixture.encoded) {
			t.Fatalf("parent refusal wrote %s: %v", path, err)
		}
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeRejectsParentReplacementBeforeWrite")
}

// This actual check-to-swap race preserves the foreign displaced inode and
// refuses it as the loaded predecessor. It deliberately does not claim rollback
// or automatic recovery of the now-unresolved on-disk head.
func TestHeadEMAStoreV2RuntimeExchangeRetainsConflictingPredecessor(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	foreign := []byte("actual foreign predecessor retained by atomic exchange")
	published := false
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
			if step == "publish-ready" {
				if err := os.Rename(fixture.path, fixture.path+".held"); err != nil {
					return err
				}
				return os.WriteFile(fixture.path, foreign, 0o600)
			}
			if step == "published" {
				published = true
			}
			return nil
		}})
	if err == nil || out != nil || records != nil || !published {
		t.Fatalf("actual exchange conflict was missed: published=%t error=%v", published, err)
	}
	displaced, readErr := os.ReadFile(filepath.Join(fixture.stateDir, headEMAStoreV2Candidate))
	if readErr != nil || !bytes.Equal(displaced, foreign) {
		t.Fatalf("atomic exchange destroyed the conflicting predecessor: %v", readErr)
	}
	held, readErr := os.ReadFile(fixture.path + ".held")
	if readErr != nil || !bytes.Equal(held, fixture.encoded) {
		t.Fatalf("conflict changed original authority bytes: %v", readErr)
	}
	assertHeadEMAStoreV2Fault(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeExchangeRetainsConflictingPredecessor")
}

// Witnessed absence must use actual no-replace publication, not rename-over
// a file which appears after the final prepublication check.
func TestHeadEMAStoreV2RuntimeAbsentHeadUsesNoReplace(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	if err := os.Rename(fixture.path, fixture.path+".held"); err != nil {
		t.Fatal(err)
	}
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	foreign := []byte("new owner appeared after loaded absence")
	published := false
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2FoldEpoch, 10, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
			if step == "publish-ready" {
				return os.WriteFile(fixture.path, foreign, 0o600)
			}
			if step == "published" {
				published = true
			}
			return nil
		}})
	if err == nil || out != nil || records != nil || published || !errors.Is(err, os.ErrExist) {
		t.Fatalf("no-replace did not preserve late existence: %v", err)
	}
	after, readErr := os.ReadFile(fixture.path)
	if readErr != nil || !bytes.Equal(after, foreign) || store.v2.fault == nil || store.lastSubnetEpoch != nil {
		t.Fatalf("late existence was overwritten/adopted: %v", readErr)
	}
	if loaded, err := NewHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits()); err == nil || loaded != nil {
		t.Fatal("unresolved absent-head publication was reloaded")
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeAbsentHeadUsesNoReplace")
}

// A normal empty-store publication and its exact restart use genuine folds.
func TestHeadEMAStoreV2RuntimeCreatesAbsentHeadAndReloads(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	if err := os.Rename(fixture.path, fixture.path+".held"); err != nil {
		t.Fatal(err)
	}
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	if store.lastSubnetEpoch != nil || !store.v2.namespace.missing {
		t.Fatal("empty fixture was not genuinely absent")
	}
	out, records, err := store.FoldForEpochV2(context.Background(), 10, fixture.raw, fixture.alpha)
	if err != nil || len(out) != 2 || len(records) != 2 || out[7].Cmp(big.NewRat(4, 1)) != 0 {
		t.Fatalf("absent-head publication failed: %v", err)
	}
	reloaded := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	if reloaded.v2.namespace.missing || !equalHeadEMAFolds(reloaded.lastFold, records) {
		t.Fatal("fresh publication changed on reload")
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeCreatesAbsentHeadAndReloads")
}

// A real candidate Close precedes this replacement; the replacement FIFO is
// observed without opening and cannot gain publication authority.
func TestHeadEMAStoreV2RuntimeCandidateReplacementAfterRealClose(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	replaced := false
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{afterClose: func(file *os.File) error {
			if filepath.Base(file.Name()) != headEMAStoreV2Candidate || replaced {
				return nil
			}
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Errorf("observer ran before actual candidate Close: %v", err)
			}
			replaced = true
			if err := os.Rename(file.Name(), file.Name()+".held"); err != nil {
				return err
			}
			return unix.Mkfifo(file.Name(), 0o600)
		}})
	if err == nil || out != nil || records != nil || !replaced {
		t.Fatalf("after-close candidate replacement was accepted: %v", err)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	assertHeadEMAStoreV2Fault(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeCandidateReplacementAfterRealClose")
}

// The post-publication file Close is also an owned observable boundary.
func TestHeadEMAStoreV2RuntimePublishedReplacementAfterRealClose(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	published, replaced := false, false
	foreign := []byte("late published-name replacement remains explicit evidence")
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{
			step: func(step string, _ *os.File) error {
				if step == "published" {
					published = true
				}
				return nil
			},
			afterClose: func(file *os.File) error {
				if !published || replaced || filepath.Base(file.Name()) != "head-ema.json" {
					return nil
				}
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Errorf("observer ran before actual published-file Close: %v", err)
				}
				replaced = true
				if err := os.Rename(fixture.path, fixture.path+".published-held"); err != nil {
					return err
				}
				return os.WriteFile(fixture.path, foreign, 0o600)
			},
		})
	if err == nil || out != nil || records != nil || !replaced {
		t.Fatalf("late published-name replacement was accepted: %v", err)
	}
	after, readErr := os.ReadFile(fixture.path)
	if readErr != nil || !bytes.Equal(after, foreign) {
		t.Fatalf("late foreign name was overwritten: %v", readErr)
	}
	assertHeadEMAStoreV2Fault(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimePublishedReplacementAfterRealClose")
}

// The last observer runs after the real write-owner directory Close while its
// marker is still present; cleanup cannot adopt the replacement directory.
func TestHeadEMAStoreV2RuntimeParentReplacementAfterRealClose(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	held, replaced := fixture.stateDir+".held", false
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{afterClose: func(file *os.File) error {
			if file.Name() != fixture.stateDir || replaced {
				return nil
			}
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Errorf("observer ran before actual directory Close: %v", err)
			}
			replaced = true
			if err := os.Rename(fixture.stateDir, held); err != nil {
				return err
			}
			return os.Mkdir(fixture.stateDir, 0o700)
		}})
	if err == nil || out != nil || records != nil || !replaced || store.v2.fault == nil {
		t.Fatalf("late parent replacement was accepted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(held, headEMAStoreV2Marker)); err != nil {
		t.Fatalf("original physical directory lost marker evidence: %v", err)
	}
	if _, err := os.Lstat(fixture.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup wrote replacement physical directory: %v", err)
	}
	if store.lastSubnetEpoch == nil || *store.lastSubnetEpoch != 10 {
		t.Fatal("late parent replacement published speculative memory")
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeParentReplacementAfterRealClose")
}

// This cause is a genuine error from Sync/Close on an actually closed handle,
// not a hook-provided sentinel mislabeled as a filesystem failure.
func TestHeadEMAStoreV2RuntimeRetainsActualClosedDescriptorFailure(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	closed := false
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, file *os.File) error {
			if step == "candidate-written" {
				closed = true
				return file.Close()
			}
			return nil
		}})
	if err == nil || out != nil || records != nil || !closed || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("actual closed-descriptor failure disappeared: %v", err)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	assertHeadEMAStoreV2Fault(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeRetainsActualClosedDescriptorFailure")
}

// Injected after-real-Close error and cancellation are an error-tree contract
// control, not an assertion that a kernel Close manufactured this sentinel.
func TestHeadEMAStoreV2RuntimeJoinsInjectedLateCloseAndCancellation(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	injected := errors.New("injected observer after actual candidate Close")
	observed := false
	out, records, err := store.runHeadEMAStoreV2(ctx, headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{afterClose: func(file *os.File) error {
			if filepath.Base(file.Name()) != headEMAStoreV2Candidate || observed {
				return nil
			}
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Errorf("observer ran before real Close: %v", err)
			}
			observed = true
			cancel()
			return injected
		}})
	if out != nil || records != nil || !observed || !errors.Is(err, injected) || !errors.Is(err, context.Canceled) {
		t.Fatalf("late error/cancel causes were suppressed: %v", err)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	assertHeadEMAStoreV2Fault(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeJoinsInjectedLateCloseAndCancellation")
}

// Real permission mutation after Write must not be adopted by the later fstat
// and serialized as a valid candidate owner.
func TestHeadEMAStoreV2RuntimeRejectsCandidatePermissionMutation(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	out, records, err := store.runHeadEMAStoreV2(context.Background(), headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, file *os.File) error {
			if step == "candidate-written" {
				return file.Chmod(0o644)
			}
			return nil
		}})
	if err == nil || out != nil || records != nil {
		t.Fatalf("candidate permission mutation was adopted: %v", err)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	assertHeadEMAStoreV2Fault(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeRejectsCandidatePermissionMutation")
}

// Cancellation after the durable marker is deliberately unresolved even when
// publication has not happened. Memory and the old head remain unchanged.
func TestHeadEMAStoreV2RuntimeCancellationAfterMarkerRetainsEvidence(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, records, err := store.runHeadEMAStoreV2(ctx, headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
		headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
			if step == "marker-synced" {
				cancel()
			}
			return nil
		}})
	if out != nil || records != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("marker cancellation returned state: %v", err)
	}
	assertHeadEMAStoreV2Unchanged(t, fixture, store)
	assertHeadEMAStoreV2Fault(t, fixture, store)
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeCancellationAfterMarkerRetainsEvidence")
}

// Stable private read-only files and existing hardlinks remain legal inputs.
// Atomic replacement changes the head name, not the prior linked file bytes.
func TestHeadEMAStoreV2RuntimePreservesReadOnlyHardlinkInput(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	held := fixture.path + ".hardlink"
	if err := os.Link(fixture.path, held); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture.path, 0o400); err != nil {
		t.Fatal(err)
	}
	store := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	if _, _, err := store.FoldForEpochV2(context.Background(), 11, fixture.raw, fixture.alpha); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(held)
	if err != nil || !bytes.Equal(original, fixture.encoded) {
		t.Fatalf("atomic commit changed linked prior bytes: %v", err)
	}
	oldStatus, err := os.Stat(held)
	if err != nil {
		t.Fatal(err)
	}
	newStatus, err := os.Stat(fixture.path)
	if err != nil || os.SameFile(oldStatus, newStatus) || newStatus.Mode().Perm() != 0o600 {
		t.Fatalf("atomic replacement did not retain private independent ownership: %v", err)
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimePreservesReadOnlyHardlinkInput")
}

// Two independently loaded owners cannot use the same prior concurrently.
// The real first writer durably holds its marker while the second refuses.
func TestHeadEMAStoreV2RuntimeIndependentLoadedOwnersCannotRaceCommit(t *testing.T) {
	t.Parallel()
	fixture := newHeadEMAStoreV2TestFixture(t)
	first := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	second := loadHeadEMAStoreV2RuntimeTest(t, fixture, headEMAStoreV2TestLimits())
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, _, err := first.runHeadEMAStoreV2(context.Background(), headEMAStoreV2FoldEpoch, 11, fixture.raw, nil, fixture.alpha,
			headEMAStoreV2RuntimeHooks{step: func(step string, _ *os.File) error {
				if step == "marker-synced" {
					close(entered)
					<-release
				}
				return nil
			}})
		done <- err
	}()
	<-entered
	out, records, err := second.FoldForEpochV2(context.Background(), 11, fixture.raw, fixture.alpha)
	close(release)
	firstErr := <-done
	if err == nil || out != nil || records != nil || second.v2.fault == nil || firstErr != nil {
		t.Fatalf("independent writer ownership differs: second=%v first=%v", err, firstErr)
	}
	if first.lastSubnetEpoch == nil || *first.lastSubnetEpoch != 11 || second.lastSubnetEpoch == nil || *second.lastSubnetEpoch != 10 {
		t.Fatal("competing writer published speculative memory")
	}
	if _, _, err := second.PreviewForEpochV2(context.Background(), 10, fixture.raw, fixture.alpha); err == nil {
		t.Fatal("competing owner silently recovered after another commit")
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeIndependentLoadedOwnersCannotRaceCommit")
}

// Existing unresolved names are rejected by native metadata only, regardless
// of marker contents/type. Legacy loader behavior deliberately stays legacy.
func TestHeadEMAStoreV2RuntimeExistingMarkersRefuseBeforeParsing(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"regular", "symlink", "fifo"} {
		fixture := newHeadEMAStoreV2TestFixture(t)
		marker := filepath.Join(fixture.stateDir, headEMAStoreV2Marker)
		switch kind {
		case "regular":
			if err := os.WriteFile(marker, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		case "symlink":
			if err := os.Symlink(marker+".absent", marker); err != nil {
				t.Fatal(err)
			}
		case "fifo":
			if err := unix.Mkfifo(marker, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		parsed := 0
		store, err := newHeadEMAStoreV2(context.Background(), fixture.stateDir, headEMAStoreV2TestLimits(),
			headEMAStoreV2LoadHooks{beforeRational: func() error { parsed++; return nil }})
		if err == nil || store != nil || parsed != 0 {
			t.Fatalf("%s unresolved evidence reached parsing: %d %v", kind, parsed, err)
		}
		legacy, err := NewHeadEMAStore(fixture.stateDir)
		if err != nil || legacy.v2 != nil {
			t.Fatalf("%s marker changed original legacy loader contract: %v", kind, err)
		}
	}
	t.Log("ema_runtime_control_completed: TestHeadEMAStoreV2RuntimeExistingMarkersRefuseBeforeParsing")
}
