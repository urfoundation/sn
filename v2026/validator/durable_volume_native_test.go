//go:build linux || darwin

package validator

// Actual production runtime authority comes from the existing signed synthetic
// fixture. These tests never load a coldkey, sign, dial, or submit a transaction.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	snchain "github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

func TestProductionNativeJournalRequiresOwnerCustody(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	path := filepath.Join(fixture.cfg.StateDir, "native")
	if journal, err := openReleaseNativeJournal(fixture.cfg, t.Context()); journal != nil || err == nil {
		t.Fatal("mainnet native constructor selected implicit local custody", journal, err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing declaration created a native journal", err)
	}
	if err := os.MkdirAll(fixture.cfg.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	storage := durablefixture.NewOwnerLocal(t, t.Context(), fixture.cfg.StateDir)
	if journal, err := openReleaseNativeJournal(fixture.cfg, storage.Context); journal != nil || err == nil {
		t.Fatal("declared volume silently enrolled missing native custody", journal, err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("native owner created missing preprovisioned members", err)
	}
	if len(fixture.rpc.callKVs) != 0 {
		t.Fatal("storage refusal made chain calls", fixture.rpc.callKVs)
	}
}

func TestProductionNativeJournalRetainsOriginalAfterClose(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	if err := os.MkdirAll(fixture.cfg.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.cfg.StateDir, "native")
	durablefixture.ProvisionNativeJournal(t, path)
	storage := durablefixture.NewOwnerLocal(t, t.Context(), fixture.cfg.StateDir)
	journal, err := openReleaseNativeJournal(fixture.cfg, storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	entry := snchain.JournalEntry{Time: "2026-01-01T00:00:00Z", Command: "synthetic retained native intent", Netuid: 25, Nonce: 17, Stage: snchain.JournalStageFailed, Detail: "synthetic original outcome"}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(journal.Path())
	if err != nil {
		t.Fatal(err)
	}
	storage.Host.SetReserve(0, 0)
	if entries, err := journal.Entries(); err != nil || len(entries) != 1 || entries[0] != entry {
		t.Fatal("full-volume inspection lost the original record", entries, err)
	}
	if err := journal.Append(entry); !errors.Is(err, durablevolume.ErrUnavailable) {
		t.Fatal("full-volume native mutation was admitted", err)
	}
	storage.Host.SetReserve(1024*1024*1024, 1024*1024)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = openReleaseNativeJournal(fixture.cfg, storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := journal.Entries(); err != nil || len(entries) != 1 || entries[0] != entry {
		t.Fatal("reopen changed original nonce or outcome", entries, err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	retained := filepath.Join(filepath.Dir(path), "retained-original-native.jsonl")
	if err := os.Rename(journal.Path(), retained); err != nil {
		t.Fatal(err)
	}
	if reopened, err := openReleaseNativeJournal(fixture.cfg, storage.Context); reopened != nil || !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("missing acknowledged journal reopened as empty", reopened, err)
	}
	if after, err := os.ReadFile(retained); err != nil || !bytes.Equal(after, original) {
		t.Fatal("refused reopen changed retained original bytes", err)
	}
}

func TestProductionNativeJournalRejectsDaemonPolicyAndCancellation(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	if err := os.MkdirAll(fixture.cfg.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	durablefixture.ProvisionNativeJournal(t, filepath.Join(fixture.cfg.StateDir, "native"))
	daemon := durablefixture.New(t, t.Context(), fixture.cfg.StateDir)
	if journal, err := openReleaseNativeJournal(fixture.cfg, daemon.Context); journal != nil || err == nil {
		t.Fatal("owner-key journal accepted a daemon declaration", journal, err)
	}
	owner := durablefixture.NewOwnerLocal(t, t.Context(), fixture.cfg.StateDir)
	ctx, cancel := context.WithCancel(owner.Context)
	cancel()
	if journal, err := openReleaseNativeJournal(fixture.cfg, ctx); journal != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled owner opened custody", journal, err)
	}
	journal, err := openReleaseNativeJournal(fixture.cfg, owner.Context)
	if err != nil {
		t.Fatal("failed opener leaked an owner", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
}
