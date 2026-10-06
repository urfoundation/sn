//go:build linux

// The retained preparation export must carry enough original physical evidence
// to verify native owner census after copying files to different inodes.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Stopped-writer authority binds the completed public declaration and lease.
func storagePreparationExportFence(t *testing.T, f *storagePreparationCommandFixture, ref durablevolume.Reference, ownerLocal bool) durablevolume.Reference {
	t.Helper()
	load := durablevolume.Load
	if ownerLocal {
		load = durablevolume.LoadOwnerLocal
	}
	declaration, err := load(ref)
	if err != nil {
		t.Fatal(err)
	}
	lease := ""
	for _, volume := range declaration.Volumes {
		for _, root := range volume.StateRoots {
			if root.Path == f.root {
				if lease != "" {
					t.Fatal("fixture repeats its exact export root")
				}
				lease = root.LeaseSha256
			}
		}
	}
	if lease == "" {
		t.Fatal("fixture declaration omits its exact export root")
	}
	fence := durablevolume.FormerWriterFence{Schema: durablevolume.FormerWriterFenceSchema, RootPath: f.root, DeclarationSha256: ref.Sha256, LeaseSha256: lease, FormerWritersStopped: true, Evidence: "synthetic original writer closed and joined; no live account or service"}
	raw, err := json.Marshal(fence)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.metadata, "retained-export-fence.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return durablevolume.Reference{Path: path, Sha256: durablefixture.Digest(raw)}
}

// The exact actual native raw digest cannot be reconstructed from v3 inventory
// alone because it intentionally hashes each original file inode as well.
func TestStoragePreparationExportRetainsNativePhysicalHistory(t *testing.T) {
	for _, ownerLocal := range []bool{false, true} {
		func() {
			f := newStoragePreparationCommandFixture(t)
			scope, command := "daemon", "storage-prepare"
			open := chain.OpenDurableJournal
			if ownerLocal {
				scope, command, open = "owner-local", "storage-owner-prepare", chain.OpenOwnerLocalJournal
			}
			storagePreparationOwnerRequest(t, f, scope, []durablevolume.PreparationOwner{storagePreparationNativeOwner()})
			ctx := storagePreparationApplyOwnerCommand(t, f, command)
			journal, err := open(ctx, f.root)
			if err != nil {
				t.Fatal(err)
			}
			raw := []byte("synthetic retained original SCALE bytes; no live signature")
			hash := chain.ExtrinsicHash(raw)
			if err := journal.SaveRaw(hash, raw); err != nil {
				t.Fatal(err)
			}
			for _, stage := range []string{chain.JournalStageBroadcast, chain.JournalStageFinalized} {
				entry := chain.JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic", Nonce: 71, ExtrinsicHash: hash.Hex(), Stage: stage}
				if err := journal.Append(entry); err != nil {
					t.Fatal(err)
				}
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			before := storagePreparationNativeCustody(t, f)
			ref, _ := durablevolume.ReferenceFromContext(ctx)
			fence := storagePreparationExportFence(t, f, ref, ownerLocal)
			var output, diagnostic bytes.Buffer
			args := []string{command, "export", "--root", f.root, "--former-writer-fence", fence.Path, "--former-writer-fence-sha256", fence.Sha256}
			if code := runMain(ctx, args, &output, &diagnostic); code != 0 {
				t.Fatal("public retained physical export is unavailable", code, diagnostic.String())
			}
			var report struct {
				Schema  string `json:"schema"`
				Entries []struct {
					Path            string                             `json:"path"`
					Kind            string                             `json:"kind"`
					Size            uint64                             `json:"size"`
					Sha256          string                             `json:"sha256"`
					Physical        *durablevolume.PhysicalRoot        `json:"physical"`
					OwnerAttributes []durablevolume.InventoryAttribute `json:"owner_attributes"`
				} `json:"entries"`
				RestartAuthorized bool `json:"restart_authorized"`
			}
			if err := json.Unmarshal(output.Bytes(), &report); err != nil || report.Schema != "urnetwork-durable-volume-physical-inventory-v1" || report.RestartAuthorized {
				t.Fatal("retained export lost original scope", err)
			}
			var anchor struct {
				Committed struct {
					RawCount  int    `json:"raw_count"`
					RawSha256 string `json:"raw_sha256"`
				} `json:"committed"`
			}
			hasher := sha256.New()
			encoder := json.NewEncoder(hasher)
			count := 0
			retainedAnchor := false
			for _, entry := range report.Entries {
				if entry.Physical == nil || entry.Physical.Inode == 0 {
					t.Fatal("retained export omitted original member generation", entry.Path)
				}
				if entry.Path == "" {
					for _, attribute := range entry.OwnerAttributes {
						if attribute.Name == chain.NativeJournalCustodyAttribute {
							if err := json.Unmarshal(attribute.Value, &anchor); err != nil {
								t.Fatal(err)
							}
							if !bytes.Equal(before.anchor, attribute.Value) {
								t.Fatal("export changed original native anchor")
							}
							retainedAnchor = true
						}
					}
				}
				if entry.Kind == "file" && strings.HasPrefix(entry.Path, "native-transactions/") {
					member := struct {
						Name   string `json:"name"`
						Inode  uint64 `json:"inode"`
						Size   int64  `json:"size"`
						Sha256 string `json:"sha256"`
					}{filepath.Base(entry.Path), entry.Physical.Inode, int64(entry.Size), strings.TrimPrefix(entry.Sha256, "sha256:")}
					if err := encoder.Encode(member); err != nil {
						t.Fatal(err)
					}
					count++
				}
			}
			if !retainedAnchor || count != 1 || anchor.Committed.RawCount != count || hex.EncodeToString(hasher.Sum(nil)) != anchor.Committed.RawSha256 {
				t.Fatal("export cannot authenticate original native raw census", count, anchor)
			}
			storagePreparationAssertNativeCustody(t, f, before)
			retained, err := os.ReadFile(filepath.Join(f.root, "native-transactions", strings.TrimPrefix(hash.Hex(), "0x")+".scale"))
			if err != nil || !bytes.Equal(retained, raw) {
				t.Fatal("export changed original retained raw bytes", err)
			}
		}()
	}
}

// Read-only export requires explicit custody, propagates cancellation and
// reports short stdout writes as failure without changing original bytes.
func TestStoragePreparationExportRefusesMissingCanceledAndShortOutput(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{storagePreparationNativeOwner()})
	ctx := storagePreparationApplyOwnerCommand(t, f, "storage-prepare")
	ref, _ := durablevolume.ReferenceFromContext(ctx)
	fence := storagePreparationExportFence(t, f, ref, false)
	args := []string{"storage-prepare", "export", "--root", f.root, "--former-writer-fence", fence.Path, "--former-writer-fence-sha256", fence.Sha256}
	before := storagePreparationNativeCustody(t, f)
	var output, diagnostic bytes.Buffer
	if code := runMain(context.Background(), args, &output, &diagnostic); code == 0 || output.Len() != 0 {
		t.Fatal("missing declaration admitted physical authority", code)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	output.Reset()
	diagnostic.Reset()
	if code := runMain(canceled, args, &output, &diagnostic); code == 0 || output.Len() != 0 {
		t.Fatal("canceled export emitted physical authority", code)
	}
	var short storageInspectionShortWriter
	diagnostic.Reset()
	if code := runMain(ctx, args, &short, &diagnostic); code != 1 || !strings.Contains(diagnostic.String(), io.ErrShortWrite.Error()) {
		t.Fatal("short physical report was acknowledged", code, diagnostic.String())
	}
	storagePreparationAssertNativeCustody(t, f, before)
}
