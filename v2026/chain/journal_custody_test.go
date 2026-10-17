//go:build linux || darwin

// Real files, descriptor joins and explicit barriers prove checkpoint custody.
package chain

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/urnetwork/connect/v2026/durablesys"
	"golang.org/x/sys/unix"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A missing authority is never re-enrolled, even when all data still exists.
func TestNativeJournalCustodyAnchorIsMandatory(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	path := journal.Dir()
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Removexattr(path, NativeJournalCustodyAttribute); err != nil {
		t.Fatal(err)
	}
	for _, open := range []func(context.Context, string) (*Journal, error){OpenDurableJournal, ReconcileDurableJournal} {
		owner, err := open(fixture.Context, path)
		if owner != nil {
			owner.Close()
		}
		if !errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal("missing anchor was admitted", err)
		}
		if _, err := unix.Getxattr(path, NativeJournalCustodyAttribute, nil); !errors.Is(err, durablesys.ErrNoAttribute) {
			t.Fatal("runtime enrolled a missing anchor", err)
		}
	}
}

// No-runtime-enrollment also covers an otherwise approved empty directory.
func TestNativeJournalCustodyOpenerDoesNotCreateLog(t *testing.T) {
	root := filepath.Join(t.TempDir(), "synthetic-approved-root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := durablefixture.New(t, t.Context(), root)
	if owner, err := OpenDurableJournal(fixture.Context, root); err == nil {
		owner.Close()
		t.Fatal("empty approved root was silently enrolled")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("admission created native custody", entries, err)
	}
}

// A named observation outage refuses this operation without inventing loss.
func TestNativeJournalCustodyObservationFailureRetriesSameOwner(t *testing.T) {
	journal, _ := nativeJournalFixture(t)
	owner := journal.guard.(*guardedNativeJournal)
	owner.checkpoint = func(stage string, _ *os.File) error {
		if stage == "custody-observe" {
			return syscall.EIO
		}
		return nil
	}
	if err := journal.CheckWrite(); !errors.Is(err, durablevolume.ErrUnavailable) || !errors.Is(err, syscall.EIO) || errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("unavailable anchor observation changed identity", err)
	}
	owner.checkpoint = nil
	if err := journal.Append(JournalEntry{Command: "synthetic", Stage: JournalStageFinalized}); err != nil {
		t.Fatal("same owner could not resume", err)
	}
}

// Actual checkpoint/data uncertainty stops the old owner; a joined, exact
// recovery resumes only that owner without resigning or rewriting its bytes.
func TestNativeJournalCustodyExactLostAcknowledgementRecovers(t *testing.T) {
	for _, stage := range []string{"custody-pending-synced", "append-directory-synced", "custody-committed-synced", "raw-written", "raw-synced", "raw-renamed"} {
		journal, fixture := nativeJournalFixture(t)
		owner := journal.guard.(*guardedNativeJournal)
		owner.checkpoint = func(point string, _ *os.File) error {
			if point == stage {
				return syscall.EIO
			}
			return nil
		}
		entry := JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic", Stage: JournalStageFinalized}
		raw := []byte("synthetic-fully-retained-native-publication")
		var err error
		if strings.HasPrefix(stage, "raw-") {
			err = journal.SaveRaw(ExtrinsicHash(raw), raw)
		} else {
			err = journal.Append(entry)
		}
		if !errors.Is(err, ErrJournalUncertain) || !errors.Is(err, syscall.EIO) {
			t.Fatal(stage, "actual publication uncertainty was acknowledged", err)
		}
		if err := journal.CheckWrite(); !errors.Is(err, ErrJournalUncertain) {
			t.Fatal(stage, "old owner resumed after uncertain publication", err)
		}
		if live, err := ReconcileDurableJournal(fixture.Context, journal.Dir()); !errors.Is(err, durablevolume.ErrBusy) {
			if live != nil {
				live.Close()
			}
			t.Fatal(stage, "recovery bypassed an unjoined writer", err)
		}
		before, err := os.ReadFile(journal.Path())
		if err != nil {
			t.Fatal(err)
		}
		if err := journal.Close(); err != nil {
			t.Fatal(err)
		}
		recovered, err := ReconcileDurableJournal(fixture.Context, journal.Dir())
		if err != nil {
			t.Fatal(stage, "exact retained checkpoint did not reconcile", err)
		}
		after, err := os.ReadFile(journal.Path())
		if err != nil || !bytes.Equal(before, after) {
			recovered.Close()
			t.Fatal(stage, "reconciliation changed journal bytes", err)
		}
		if strings.HasPrefix(stage, "raw-") {
			retained, err := os.ReadFile(journal.RawPath(ExtrinsicHash(raw)))
			if err != nil || !bytes.Equal(raw, retained) {
				recovered.Close()
				t.Fatal(stage, "exact raw bytes changed", err)
			}
		}
		if err := recovered.Append(entry); err != nil {
			recovered.Close()
			t.Fatal(stage, "reconciled owner failed to continue", err)
		}
		if err := recovered.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Unknown partial content stays intact instead of being truncated to an anchor.
func TestNativeJournalCustodyRecoveryRefusesPartialBytes(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	owner := journal.guard.(*guardedNativeJournal)
	owner.writeFile = func(file *os.File, raw []byte) (int, error) {
		n, err := file.Write(raw[:len(raw)/2])
		return n, errors.Join(err, syscall.ENOSPC)
	}
	if err := journal.Append(JournalEntry{Command: "synthetic", Stage: JournalStageFinalized}); !errors.Is(err, ErrJournalUncertain) {
		t.Fatal(err)
	}
	before, err := os.ReadFile(journal.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := ReconcileDurableJournal(fixture.Context, journal.Dir())
	if recovered != nil {
		recovered.Close()
	}
	if !errors.Is(err, ErrJournalUncertain) {
		t.Fatal("partial bytes were automatically reconciled", err)
	}
	after, err := os.ReadFile(journal.Path())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed recovery rewrote partial bytes", err)
	}
}

// A retained root's loss is observed during readback; unrelated owners continue.
func TestNativeJournalCustodyConcurrentRootLossIsScoped(t *testing.T) {
	parent := t.TempDir()
	rootA, rootB := filepath.Join(parent, "owner-a"), filepath.Join(parent, "owner-b")
	for _, root := range []string{rootA, rootB} {
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		durablefixture.ProvisionNativeJournal(t, filepath.Join(root, "journal"))
	}
	fixture := durablefixture.New(t, t.Context(), rootA, rootB)
	a, err := OpenDurableJournal(fixture.Context, filepath.Join(rootA, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := OpenDurableJournal(fixture.Context, filepath.Join(rootB, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	entry := JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic", Stage: JournalStageFinalized}
	if err := a.Append(entry); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	a.guard.(*guardedNativeJournal).checkpoint = func(stage string, _ *os.File) error {
		if stage == "entries-read" {
			close(entered)
			<-release
		}
		return nil
	}
	result := make(chan error, 1)
	go func() {
		entries, err := a.Entries()
		if len(entries) != 0 {
			err = errors.Join(err, errors.New("lost-root read returned entries"))
		}
		result <- err
	}()
	<-entered
	if err := os.Rename(rootA, rootA+".retained"); err != nil {
		close(release)
		<-result
		t.Fatal(err)
	}
	if err := os.Mkdir(rootA, 0700); err != nil {
		close(release)
		<-result
		t.Fatal(err)
	}
	close(release)
	if err := <-result; !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("root loss was not retained", err)
	}
	if err := b.Append(entry); err != nil {
		t.Fatal("unrelated owner was stopped", err)
	}
	if err := os.Remove(rootA); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(rootA+".retained", rootA); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckWrite(); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("old owner resurrected restored pathname", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := OpenDurableJournal(fixture.Context, filepath.Join(rootA, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if entries, err := recovered.Entries(); err != nil || len(entries) != 1 || entries[0] != entry {
		t.Fatal("same approved root lost completed bytes", entries, err)
	}
}

// Handoff authenticates acknowledged bytes even after their descriptor closed.
func TestNativeJournalCustodyInPlaceMutationStopsHandoff(t *testing.T) {
	for _, kind := range []string{"journal", "raw"} {
		journal, _ := nativeJournalFixture(t)
		if err := journal.Append(JournalEntry{Command: "synthetic", Stage: JournalStageFinalized}); err != nil {
			t.Fatal(err)
		}
		raw := []byte("synthetic-immutable-native-custody")
		if err := journal.SaveRaw(ExtrinsicHash(raw), raw); err != nil {
			t.Fatal(err)
		}
		path := journal.Path()
		if kind == "raw" {
			path = journal.RawPath(ExtrinsicHash(raw))
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte("changed")); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if err := journal.CheckWrite(); !errors.Is(err, durablevolume.ErrIdentity) {
			t.Fatal(kind, "changed retained bytes crossed handoff", err)
		}
	}
}

// The child exits after real data/parent sync, before the pending checkpoint clears.
func TestNativeJournalCustodyCrashChild(t *testing.T) {
	root := os.Getenv("SN_SYNTHETIC_NATIVE_CRASH_ROOT")
	if root == "" {
		return
	}
	fixture := durablefixture.New(t, t.Context(), root)
	ctx := durablevolume.WithReference(fixture.Context, durablevolume.Reference{Path: os.Getenv("SN_SYNTHETIC_NATIVE_CRASH_REFERENCE"), Sha256: os.Getenv("SN_SYNTHETIC_NATIVE_CRASH_SHA256")})
	journal, err := OpenDurableJournal(ctx, filepath.Join(root, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	selected := os.Getenv("SN_SYNTHETIC_NATIVE_CRASH_STAGE")
	journal.guard.(*guardedNativeJournal).checkpoint = func(stage string, _ *os.File) error {
		if stage == selected {
			os.Exit(86)
		}
		return nil
	}
	if strings.HasPrefix(selected, "raw-") {
		raw := []byte("synthetic-forced-crash-raw-publication")
		err = journal.SaveRaw(ExtrinsicHash(raw), raw)
	} else {
		err = journal.Append(JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic-crash", Stage: JournalStageFinalized})
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("forced crash checkpoint was not reached")
}

// Joined process death releases ownership; exact recovery retains the written row.
func TestNativeJournalCustodyCrashRecoversExactSyncedBytes(t *testing.T) {
	for _, stage := range []string{"append-directory-synced", "raw-created", "raw-written", "raw-synced", "raw-renamed"} {
		journal, fixture := nativeJournalFixture(t)
		if err := journal.Close(); err != nil {
			t.Fatal(err)
		}
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestNativeJournalCustodyCrashChild$", "-test.timeout=30s")
		cmd.Env = append(os.Environ(), "SN_SYNTHETIC_NATIVE_CRASH_ROOT="+filepath.Dir(journal.Dir()), "SN_SYNTHETIC_NATIVE_CRASH_REFERENCE="+fixture.Reference.Path, "SN_SYNTHETIC_NATIVE_CRASH_SHA256="+fixture.Reference.Sha256, "SN_SYNTHETIC_NATIVE_CRASH_STAGE="+stage)
		output, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 86 {
			t.Fatalf("unexpected child result: %v %s", err, output)
		}
		before, err := os.ReadFile(journal.Path())
		if err != nil {
			t.Fatal(err)
		}
		recovered, err := ReconcileDurableJournal(fixture.Context, journal.Dir())
		if stage == "raw-created" {
			if recovered != nil {
				recovered.Close()
			}
			if !errors.Is(err, ErrJournalUncertain) {
				t.Fatal("unanchored temporary was enrolled", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(journal.Path())
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("crash recovery rewrote original bytes", err)
		}
		entries, err := recovered.Entries()
		if err != nil || stage == "append-directory-synced" && (len(entries) != 1 || entries[0].Command != "synthetic-crash") || strings.HasPrefix(stage, "raw-") && len(entries) != 0 {
			t.Fatal("synced crash row was lost", entries, err)
		}
		if strings.HasPrefix(stage, "raw-") {
			raw := []byte("synthetic-forced-crash-raw-publication")
			retained, err := os.ReadFile(journal.RawPath(ExtrinsicHash(raw)))
			if err != nil || !bytes.Equal(raw, retained) {
				t.Fatal(stage, "recovery lost exact signed raw bytes", err)
			}
		}
		if err := recovered.Append(JournalEntry{Command: "synthetic-next", Stage: JournalStageFinalized}); err != nil {
			t.Fatal("recovered owner could not continue", err)
		}
		if err := recovered.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// An exact pending member is unique; collisions, unknown bytes and duplicates
// are retained for separate reconciliation rather than guessed or overwritten.
func TestNativeJournalCustodyPendingRawRejectsAmbiguity(t *testing.T) {
	for _, kind := range []string{"two-pending", "target-collision", "hash-mismatch", "partial"} {
		journal, fixture := nativeJournalFixture(t)
		owner := journal.guard.(*guardedNativeJournal)
		owner.checkpoint = func(stage string, _ *os.File) error {
			if stage == "raw-synced" {
				return syscall.EIO
			}
			return nil
		}
		raw := []byte("synthetic-pending-exact-raw-member")
		if err := journal.SaveRaw(ExtrinsicHash(raw), raw); !errors.Is(err, ErrJournalUncertain) {
			t.Fatal(err)
		}
		if err := journal.Close(); err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(journal.Dir(), journalRawDir)
		entries, err := os.ReadDir(directory)
		if err != nil || len(entries) != 1 {
			t.Fatal(entries, err)
		}
		path := filepath.Join(directory, entries[0].Name())
		switch kind {
		case "two-pending":
			last := "0"
			if strings.HasSuffix(path, last) {
				last = "1"
			}
			if err := os.WriteFile(path[:len(path)-1]+last, raw, 0600); err != nil {
				t.Fatal(err)
			}
		case "target-collision":
			if err := os.WriteFile(journal.RawPath(ExtrinsicHash(raw)), raw, 0600); err != nil {
				t.Fatal(err)
			}
		case "hash-mismatch":
			if err := os.WriteFile(path, bytes.Repeat([]byte{7}, len(raw)), 0600); err != nil {
				t.Fatal(err)
			}
		case "partial":
			if err := os.WriteFile(path, raw[:len(raw)/2], 0600); err != nil {
				t.Fatal(err)
			}
		}
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		recovered, err := ReconcileDurableJournal(fixture.Context, journal.Dir())
		if recovered != nil {
			recovered.Close()
		}
		if err == nil {
			t.Fatal(kind, "ambiguous pending custody was accepted")
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal(kind, "failed reconciliation changed original bytes", err)
		}
	}
}
