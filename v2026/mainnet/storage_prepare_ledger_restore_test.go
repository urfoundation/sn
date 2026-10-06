//go:build linux

// These controls reach the real public restore dispatcher and the unchanged
// guarded validator constructor. No exported test-only preparation API exists.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// A complete freshly initialized validator is still historical custody once
// its runtime has opened. Restore must consume its original physical head.
func storageLedgerRestoreCommandFixture(t *testing.T, removeHead bool) *storageSnapshotRestoreFixture {
	t.Helper()
	source := newStoragePreparationCommandFixture(t)
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	ledger, err := validator.NewDiskAttemptLedger(ctx, source.root, source.identity, "0x"+strings.Repeat("23", 20), source.key, source.limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	if removeHead {
		if err := unix.Removexattr(source.root, "user.urnetwork.attempt-ledger-custody"); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(source.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	result := storageSnapshotRestoreTarget(t, source, ctx, request.Owners[0], false)
	result.target.key = source.key
	return result
}

// The public command preserves every backend byte before the actual runtime
// opens; only the original import/head's physical coordinates may differ.
func TestStoragePreparationRestoreValidatorLedgerKeepsActualOwner(t *testing.T) {
	f := storageLedgerRestoreCommandFixture(t, false)
	path, hash := storagePreparationFreezeOwnerPlan(t, f.target, "storage-prepare")
	var output, diagnostic bytes.Buffer
	if code := runMain(f.target.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("public ledger restore refused exact original custody", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("public ledger restore granted activation", err)
	}
	var scope validator.AttemptLedgerPreparationScope
	if err := json.Unmarshal(f.owner.Inputs, &scope); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(f.archive, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(f.archive, path)
		if err != nil {
			return err
		}
		before, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		after, err := os.ReadFile(filepath.Join(f.target.root, relative))
		if err != nil {
			return err
		}
		if !bytes.Equal(before, after) {
			return errors.New("public ledger restore rewrote original member " + relative)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx := durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), f.target.storage.Host)
	// The source fixture's synthetic key is independently retained outside all
	// request/plan bytes; the restore command never accepts or invokes it.
	ledger, err := validator.NewDiskAttemptLedger(ctx, f.target.root, scope.Identity, scope.Coordinator, f.target.key, scope.Limits)
	if err != nil {
		t.Fatal("actual validator could not open restored original ledger", err)
	}
	head, headErr := ledger.Head()
	if err := errors.Join(headErr, ledger.Close()); err != nil || head != scope.ExpectedHead {
		t.Fatal("restored validator changed its original acknowledged head", head, err)
	}
}

// Export is a physical record, not a substitute for missing application
// authority. Plan must reject it before creating any target ledger member.
func TestStoragePreparationRestoreValidatorRefusesLostOriginalHead(t *testing.T) {
	f := storageLedgerRestoreCommandFixture(t, true)
	var output, diagnostic bytes.Buffer
	if code := runMain(f.target.ctx, []string{"storage-prepare", "plan", "--request", f.target.requestPath, "--request-sha256", f.target.requestHash}, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "original checkpoint is missing") {
		t.Fatal("missing historical ledger head became restore authority", code, diagnostic.String())
	}
	entries, err := os.ReadDir(f.target.root)
	if err != nil || len(entries) != 0 {
		t.Fatal("refused historical custody created target bytes", err)
	}
}
