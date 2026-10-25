//go:build linux || darwin

// Real retained files and deterministic I/O seams expose native custody loss.
package chain

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Declared roots and volume metadata are explicitly provisioned by the fixture.
func nativeJournalFixture(t *testing.T) (*Journal, *durablefixture.Fixture) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "native-root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := durablefixture.New(t, t.Context(), root)
	durablefixture.ProvisionNativeJournal(t, filepath.Join(root, "journal"))
	journal, err := OpenDurableJournal(fixture.Context, filepath.Join(root, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	return journal, fixture
}

// Stable completed bytes remain inspectable while mutation is refused.
func TestDurableNativeJournalReadsUnderWritePressure(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	entry := JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic", Stage: JournalStageFinalized}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	raw := []byte("synthetic-original-signed-bytes")
	hash := ExtrinsicHash(raw)
	if err := journal.SaveRaw(hash, raw); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(journal.Path())
	if err != nil {
		t.Fatal(err)
	}
	fixture.Host.SetReserve(0, 0)
	entries, err := journal.Entries()
	if err != nil || len(entries) != 1 || entries[0] != entry {
		t.Errorf("write pressure blocked retained read: %+v %v", entries, err)
	}
	if err := journal.SaveRaw(hash, raw); err != nil {
		t.Error("write pressure blocked exact retained raw inspection", err)
	}
	if err := journal.Append(entry); !errors.Is(err, durablevolume.ErrUnavailable) {
		t.Error("pressure admitted mutation", err)
	}
	after, err := os.ReadFile(journal.Path())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("pre-admission refusal changed custody", err)
	}
	fixture.Host.SetReserve(1024*1024*1024, 1024*1024)
	if err := journal.Append(entry); err != nil {
		t.Fatal("same owner failed reserve recovery", err)
	}
}

// Same-directory replacement cannot acknowledge bytes written to a detached leaf.
func TestDurableNativeJournalRejectsReplacedOpenedLeaves(t *testing.T) {
	for _, operation := range []string{"append", "entries", "raw"} {
		journal, _ := nativeJournalFixture(t)
		entry := JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic", Stage: JournalStageFinalized}
		if err := journal.Append(entry); err != nil {
			t.Fatal(err)
		}
		raw := []byte("synthetic-retained-raw")
		hash := ExtrinsicHash(raw)
		if err := journal.SaveRaw(hash, raw); err != nil {
			t.Fatal(err)
		}
		path := journal.Path()
		if operation == "raw" {
			path = journal.RawPath(hash)
		}
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		owner := journal.guard.(*guardedNativeJournal)
		owner.checkpoint = func(stage string, _ *os.File) error {
			if stage != operation+"-opened" {
				return nil
			}
			owner.checkpoint = nil
			if err := os.Rename(path, path+".retained"); err != nil {
				return err
			}
			return os.WriteFile(path, original, 0600)
		}
		switch operation {
		case "append":
			err = journal.Append(entry)
		case "entries":
			_, err = journal.Entries()
		case "raw":
			err = journal.SaveRaw(hash, raw)
		}
		if !errors.Is(err, durablevolume.ErrIdentity) {
			t.Errorf("%s acknowledged detached leaf: %v", operation, err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".retained", path); err != nil {
			t.Fatal(err)
		}
		if err := journal.Append(entry); !errors.Is(err, durablevolume.ErrIdentity) {
			t.Errorf("%s resurrected lost leaf generation: %v", operation, err)
		}
	}
}

// A real partial append is retained and cannot be extended into invalid JSONL.
func TestDurableNativeJournalPartialAppendRequiresReconciliation(t *testing.T) {
	journal, _ := nativeJournalFixture(t)
	entry := JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic", Stage: JournalStageFinalized}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	owner := journal.guard.(*guardedNativeJournal)
	owner.writeFile = func(file *os.File, raw []byte) (int, error) {
		n, err := file.Write(raw[:len(raw)/2])
		return n, errors.Join(err, syscall.ENOSPC)
	}
	if err := journal.Append(entry); !errors.Is(err, syscall.ENOSPC) {
		t.Fatal("partial write did not retain cause", err)
	}
	partial, err := os.ReadFile(journal.Path())
	if err != nil {
		t.Fatal(err)
	}
	owner.writeFile = nil
	if err := journal.Append(entry); err == nil {
		t.Error("same owner appended after uncertain partial write")
	}
	after, err := os.ReadFile(journal.Path())
	if err != nil || !bytes.Equal(partial, after) {
		t.Error("uncertain custody was changed", err)
	}
}

// Publication followed by failed durability admission cannot be acknowledged later.
func TestDurableNativeJournalPostRenameFailureCannotAcknowledge(t *testing.T) {
	journal, _ := nativeJournalFixture(t)
	raw := []byte("synthetic-signed-raw")
	hash := ExtrinsicHash(raw)
	owner := journal.guard.(*guardedNativeJournal)
	owner.checkpoint = func(stage string, _ *os.File) error {
		if stage == "raw-renamed" {
			return syscall.EIO
		}
		return nil
	}
	if err := journal.SaveRaw(hash, raw); !errors.Is(err, syscall.EIO) {
		t.Fatal("rename uncertainty did not retain cause", err)
	}
	retained, err := os.ReadFile(journal.RawPath(hash))
	if err != nil || !bytes.Equal(raw, retained) {
		t.Fatal("renamed raw bytes were lost", err)
	}
	owner.checkpoint = nil
	if err := journal.SaveRaw(hash, raw); err == nil {
		t.Fatal("same uncertain owner acknowledged prior publication")
	}
}

// Context ownership continues through later reads and writes, not only opening.
func TestDurableNativeJournalCancellationPrecedesIo(t *testing.T) {
	journal, fixture := nativeJournalFixture(t)
	path := journal.Dir()
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(fixture.Context)
	journal, err := OpenDurableJournal(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	entry := JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic", Stage: JournalStageFinalized}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(journal.Path())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := journal.Entries(); !errors.Is(err, context.Canceled) {
		t.Error("canceled owner read custody", err)
	}
	if err := journal.Append(entry); !errors.Is(err, context.Canceled) {
		t.Error("canceled owner changed custody", err)
	}
	after, err := os.ReadFile(journal.Path())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("canceled owner published bytes", err)
	}
}
