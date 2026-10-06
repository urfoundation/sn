//go:build linux

package main

// Declared future custody must fit the same fixed complete inventory used by
// real export and restore. Small current contents cannot excuse a larger scope.
import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Each refusal enters the actual public fresh command with an empty target.
// The generic request itself remains within Core's limits, isolating the
// previously unbounded application profile as the causal admission failure.
func TestStoragePreparationPublicationRejectsUnexportableDeclaredCapacity(t *testing.T) {
	for _, dimension := range []string{"history", "window", "files"} {
		func() {
			f := newStoragePublicationPreparationFixture(t)
			switch dimension {
			case "history":
				f.profile.MaxHistoryBytes = validator.ProviderAttemptPublicationMaximumHistoryBytes + 1
			case "window":
				f.profile.MaxHistoryBytes = validator.ProviderAttemptPublicationMaximumHistoryBytes
				f.profile.MaxWindowBytes = validator.ProviderAttemptPublicationMaximumHistoryBytes + 1
			default:
				f.profile.MaxFiles = durablevolume.MaximumPhysicalInventoryEntries
			}
			owner := storagePublicationProfileOwner(t, f.source, f.profile)
			storagePreparationOwnerRequest(t, f.source, "daemon", []durablevolume.PreparationOwner{owner})
			var output, diagnostic bytes.Buffer
			if code := runMain(f.source.ctx, []string{"storage-prepare", "plan", "--request", f.source.requestPath, "--request-sha256", f.source.requestHash}, &output, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "provider publication allowance exceeds complete physical inventory capacity") {
				t.Fatal("public preparation did not refuse the oversized original profile", dimension, code, diagnostic.String())
			}
			if _, err := f.profile.InventoryLimits(); !errors.Is(err, protocol.ErrProviderAttemptsCapacity) {
				t.Fatal("unexportable declaration acquired a complete inventory forecast", dimension, err)
			}
			if entries, err := os.ReadDir(f.source.root); err != nil || len(entries) != 0 {
				t.Fatal("oversized original scope changed the empty target", dimension, err)
			}
			if _, err := unix.Getxattr(f.source.root, validator.ProviderAttemptPublicationNamespaceAttribute, make([]byte, 4096)); !errors.Is(err, unix.ENODATA) {
				t.Fatal("oversized original scope published custody", dimension, err)
			}
		}()
	}
}

// Only the limits are large. Actual files remain tiny, so this proves the real
// Core admission boundary without allocating, creating or copying large data.
func TestStoragePreparationPublicationBoundaryForecastEntersActualPhysicalExport(t *testing.T) {
	f := newStoragePublicationPreparationFixture(t)
	f.profile.MaxHistoryBytes = validator.ProviderAttemptPublicationMaximumHistoryBytes
	f.profile.MaxFiles = durablevolume.MaximumPhysicalInventoryEntries - 1
	limits, err := f.profile.InventoryLimits()
	if err != nil || limits.MaxBytes != 1024*1024*1024*1024 || limits.MaxEntries != durablevolume.MaximumPhysicalInventoryEntries || limits.MaxOwnerAttributes != 2 || limits.MaxOwnerAttributeBytes != 8192 {
		t.Fatal("original complete namespace forecast changed Core's physical envelope", limits, err)
	}
	f.owner = storagePublicationProfileOwner(t, f.source, f.profile)
	storagePreparationOwnerRequest(t, f.source, "daemon", []durablevolume.PreparationOwner{f.owner})
	f.ctx = storagePreparationApplyOwnerCommand(t, f.source, "storage-prepare")
	// Both empty and partial interrupted writes are real retained namespace
	// entries. Neither is promoted to a published request window.
	for index, raw := range [][]byte{nil, []byte(`{"header":`)} {
		path := filepath.Join(f.source.root, fmt.Sprintf(".compact-input-%032x", index+1))
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	reference, ok := durablevolume.ReferenceFromContext(f.ctx)
	if !ok {
		t.Fatal("public preparation lost its actual declaration")
	}
	fence := storagePreparationExportFence(t, f.source, reference, false)
	args := []string{"storage-prepare", "export", "--root", f.source.root, "--former-writer-fence", fence.Path, "--former-writer-fence-sha256", fence.Sha256,
		"--max-entries", fmt.Sprint(limits.MaxEntries), "--max-bytes", fmt.Sprint(limits.MaxBytes), "--max-depth", fmt.Sprint(limits.MaxDepth),
		"--max-owner-attributes", fmt.Sprint(limits.MaxOwnerAttributes), "--max-owner-attribute-bytes", fmt.Sprint(limits.MaxOwnerAttributeBytes)}
	var output, diagnostic bytes.Buffer
	if code := runMain(f.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("valid declared boundary cannot enter actual complete physical export", code, diagnostic.String())
	}
	var report durablevolume.Inventory
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || report.Schema != durablevolume.PhysicalInventorySchema || report.Limits != limits || len(report.Entries) != 3 || report.TotalOwnerAttributes != 2 || report.RestartAuthorized {
		t.Fatal("physical export omitted original root/temps or changed its admitted envelope", err)
	}
	for _, entry := range report.Entries[1:] {
		_, _, temporary, err := storageProviderPublicationName(entry.Path)
		if err != nil || !temporary {
			t.Fatal("complete physical census changed an interrupted original name", err)
		}
	}
}

// An independently proposed capacity increase is not authority to rewrite old
// birth. Runtime and public restore both keep that explicit migration gate.
func TestStoragePreparationPublicationCapacityChangeCannotAdoptOriginalBirth(t *testing.T) {
	f := newStoragePublicationFixture(t)
	path := f.retain(t, f.originals(t, 1)[0])
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	marker := make([]byte, 4096)
	n, err := unix.Getxattr(f.source.root, validator.ProviderAttemptPublicationNamespaceAttribute, marker)
	if err != nil {
		t.Fatal(err)
	}
	marker = marker[:n]
	changed := f.profile
	changed.MaxHistoryBytes++
	if err := changed.Validate(); err != nil {
		t.Fatal("synthetic proposed capacity migration is not otherwise valid", err)
	}
	if guard, err := validator.OpenProviderAttemptPublicationNamespace(f.ctx, changed); !errors.Is(err, durablevolume.ErrIdentity) || guard != nil {
		if guard != nil {
			guard.Close()
		}
		t.Fatal("current capacity auto-adopted a different original publication birth", err)
	}
	guard, err := validator.OpenProviderAttemptPublicationNamespace(f.ctx, f.profile)
	if err != nil {
		t.Fatal("unchanged original owner lost custody after proposed capacity change", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	storage := storageSnapshotRestoreTarget(t, f.source, f.ctx, f.owner, false)
	owner := storagePublicationProfileOwner(t, storage.target, changed)
	owner.Purpose = "restore"
	storagePreparationOwnerRequest(t, storage.target, "daemon", []durablevolume.PreparationOwner{owner})
	var output, diagnostic bytes.Buffer
	if code := runMain(storage.target.ctx, []string{storage.command, "plan", "--request", storage.target.requestPath, "--request-sha256", storage.target.requestHash}, &output, &diagnostic); code == 0 {
		t.Fatal("capacity upgrade reinterpreted original custody without reviewed migration")
	}
	for _, root := range []string{storage.heldSource, storage.archive} {
		retained, err := os.ReadFile(filepath.Join(root, filepath.Base(path)))
		if err != nil || !bytes.Equal(retained, original) {
			t.Fatal("refused capacity migration changed original signed window", err)
		}
		observed := make([]byte, 4096)
		n, err := unix.Getxattr(root, validator.ProviderAttemptPublicationNamespaceAttribute, observed)
		if err != nil || !bytes.Equal(observed[:n], marker) {
			t.Fatal("refused capacity migration rewrote original birth", err)
		}
	}
	if entries, err := os.ReadDir(f.source.root); err != nil || len(entries) != 0 {
		t.Fatal("refused capacity migration changed empty replacement target", err)
	}
}
