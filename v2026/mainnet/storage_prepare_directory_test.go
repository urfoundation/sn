//go:build linux

// The public offline dispatcher admits fixed directory-head formats without
// manufacturing a first runtime payload or any transaction authority.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Every runtime dimension is explicit; none is inferred from accounting slots.
func storagePreparationDirectoryOwner(t *testing.T, kind string) durablevolume.PreparationOwner {
	t.Helper()
	var inputs map[string]any
	switch kind {
	case "mainnet-successor-local-members", "mainnet-successor-nonce-members":
		spec := bootstrapSuccessorMemberSpec(kind == "mainnet-successor-nonce-members")
		inputs = map[string]any{"schema": "urnetwork-successor-members-preparation-v1", "name": spec.Name, "maximum_bytes": spec.MaximumBytes,
			"maximum_members": 4096, "maximum_member_bytes": 524288, "maximum_total_bytes": 268435456, "maximum_namespace_entries": 4103, "maximum_name_bytes": 255}
	case "fleet-recovery":
		inputs = map[string]any{"schema": "urnetwork-miner-snapshot-preparation-v1", "name": "journal.json", "maximum_bytes": 16777216,
			"maximum_records": 256, "maximum_raw_record_bytes": 65536, "maximum_manifest_bytes": 262144}
	case "provider-claim-queue":
		inputs = map[string]any{"schema": "urnetwork-miner-snapshot-preparation-v1", "name": "claim-queue.json", "maximum_bytes": 16777216}
	default:
		t.Fatal("invalid test fixed kind", kind)
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	return durablevolume.PreparationOwner{Kind: kind, RelativePath: ".", Purpose: "fresh", Inputs: raw}
}

// Both existing successor constructors require the declared absent directory
// head. Preparation leaves the census payload absent until actual admission.
func TestStoragePreparationCommandInitializesSuccessorMemberHeads(t *testing.T) {
	for _, registry := range []bool{false, true} {
		func() {
			f := newStoragePreparationCommandFixture(t)
			spec := bootstrapSuccessorMemberSpec(registry)
			storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{storagePreparationDirectoryOwner(t, spec.Kind)})
			ctx := storagePreparationApplyOwnerCommand(t, f, "storage-prepare")
			storage, err := openMainnetDurableDirectory(ctx, f.root, durablevolume.ReadWrite)
			if err != nil {
				t.Fatal(err)
			}
			defer storage.close()
			file := storage.directory.File()
			if err := mainnetDurableFlock(int(file.Fd()), unix.LOCK_EX); err != nil {
				t.Fatal(err)
			}
			owner, err := openBootstrapSuccessorMembers(storage, file, registry, false)
			if err != nil {
				t.Fatal("public preparation cannot open actual successor member custody", registry, err)
			}
			defer owner.close()
			if len(owner.census.Members) != 0 || owner.census.Pending != nil {
				t.Fatal("fresh successor gained history or a pending intent")
			}
			if _, present, err := owner.head.Read(); err != nil || present {
				t.Fatal("preparation fabricated a runtime census", registry, err)
			}
		}()
	}
}

// A daemon claim queue uses no fabricated lock file or empty JSON payload.
func TestStoragePreparationCommandInitializesClaimQueueHead(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{storagePreparationDirectoryOwner(t, "provider-claim-queue")})
	ctx := storagePreparationApplyOwnerCommand(t, f, "storage-prepare")
	directory, err := durablepath.Open(ctx, f.root, durablevolume.ReadWrite, false)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := mainnetDurableFlock(int(directory.File().Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	head, err := durablehead.Open(ctx, directory, directory.File(), durablehead.Spec{Kind: "provider-claim-queue", Name: "claim-queue.json", MaximumBytes: 16 * 1024 * 1024})
	if err != nil {
		t.Fatal("prepared claim head cannot be admitted", err)
	}
	defer head.Close()
	if _, present, err := head.Read(); err != nil || present {
		t.Fatal("preparation fabricated claim queue progress", err)
	}
	names, err := os.ReadDir(f.root)
	if err != nil || len(names) != 0 {
		t.Fatal("claim-only head gained a payload or marker", err)
	}
}

// Owner-local fleet custody retains an empty original marker but grants no
// signed inventory. The daemon command cannot substitute for this entrypoint.
func TestStoragePreparationOwnerCommandInitializesFleetHead(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	storagePreparationOwnerRequest(t, f, "owner-local", []durablevolume.PreparationOwner{storagePreparationDirectoryOwner(t, "fleet-recovery")})
	ctx := storagePreparationApplyOwnerCommand(t, f, "storage-owner-prepare")
	directory, err := durablepath.OpenOwnerLocal(ctx, f.root, durablevolume.ReadWrite, false)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := mainnetDurableFlock(int(directory.File().Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	head, err := durablehead.Open(ctx, directory, directory.File(), durablehead.Spec{Kind: "fleet-recovery", Name: "journal.json", MaximumBytes: 16 * 1024 * 1024, AuxiliaryNames: []string{"initialized"}})
	if err != nil {
		t.Fatal("prepared fleet head cannot be admitted", err)
	}
	defer head.Close()
	if _, present, err := head.Read(); err != nil || present {
		t.Fatal("preparation fabricated signed fleet history", err)
	}
	marker, err := os.ReadFile(filepath.Join(f.root, "initialized"))
	if err != nil || len(marker) != 0 {
		t.Fatal("preparation claimed a signed fleet inventory", err)
	}
}
