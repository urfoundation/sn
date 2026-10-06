//go:build linux || darwin

// The real miner owners reopen restored custody after offline physical
// rebinding. Every signature and transaction in these tests is synthetic.
package miner

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Known history is copied under a separately reviewed restore purpose. This
// helper never installs a target checkpoint; only the production adapter does.
func restoreMinerPreparationFixture(t *testing.T, source *minerPreparationFixture) context.Context {
	t.Helper()
	raw, err := os.ReadFile(source.plan.Path)
	if err != nil {
		t.Fatal(err)
	}
	var prior durablevolume.PreparationPlan
	if err := json.Unmarshal(raw, &prior); err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	if err := json.Unmarshal(prior.RequestBytes, &request); err != nil {
		t.Fatal(err)
	}
	reference, present := durablevolume.ReferenceFromContext(source.ctx)
	if !present {
		t.Fatal("miner source lacks physical declaration")
	}
	load, open, planFn, applyFn := durablevolume.Load, durablevolume.OpenWithHost, durablevolume.PlanPreparationWithHost, durablevolume.ApplyPreparationWithHost
	if source.ownerLocal {
		load, open, planFn, applyFn = durablevolume.LoadOwnerLocal, durablevolume.OpenOwnerLocalWithHost, durablevolume.PlanOwnerLocalPreparationWithHost, durablevolume.ApplyOwnerLocalPreparationWithHost
	}
	config, err := load(reference)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(source.root)
	metadata, staging, archive := filepath.Join(parent, "restore-metadata"), filepath.Join(parent, "restore-staging"), filepath.Join(parent, "restore-archive")
	for _, path := range []string{metadata, staging, archive} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name string, value any) durablevolume.Reference {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(metadata, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return durablevolume.Reference{Path: path, Sha256: durablefixture.Digest(raw)}
	}
	oldFence := write("original-fence.json", durablevolume.FormerWriterFence{Schema: durablevolume.FormerWriterFenceSchema, RootPath: source.root, DeclarationSha256: reference.Sha256, LeaseSha256: config.Volumes[0].StateRoots[0].LeaseSha256, FormerWritersStopped: true, Evidence: "synthetic miner owner explicitly closed and joined"})
	volume, err := open(reference, source.root, durablevolume.Snapshot, source.host)
	if err != nil {
		t.Fatal(err)
	}
	report, err := volume.InventoryPhysical(t.Context(), oldFence, durablevolume.InventoryLimits{MaxEntries: 80, MaxBytes: 32 * 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 8, MaxOwnerAttributeBytes: 32 * 1024})
	if closeErr := volume.Close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	reportRef := write("original-inventory.json", report)
	for _, entry := range report.Entries {
		path := filepath.Join(archive, entry.Path)
		if entry.Path != "" {
			if entry.Kind != "file" {
				t.Fatal("miner snapshot contains unexpected descendant", entry.Path)
			}
			raw, err := os.ReadFile(filepath.Join(source.root, entry.Path))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, attribute := range entry.OwnerAttributes {
			if err := unix.Setxattr(path, attribute.Name, attribute.Value, unix.XATTR_CREATE); err != nil {
				t.Fatal(err)
			}
		}
	}
	nonce, err := hex.DecodeString(report.RootGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(archive, durablevolume.RootGenerationAttribute, nonce, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source.root, source.root+".original-held"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(source.root, 0700); err != nil {
		t.Fatal(err)
	}
	var root unix.Stat_t
	if err := unix.Stat(source.root, &root); err != nil {
		t.Fatal(err)
	}
	request.Purpose, request.Owners[0].Purpose = "restore", "restore"
	request.MarkerPath, request.LeasePath = filepath.Join(metadata, "identity"), filepath.Join(metadata, "lease")
	request.DeclarationPath, request.ControlPath = filepath.Join(metadata, "declaration.json"), filepath.Join(metadata, "control.jsonl")
	request.StagingDirectory = staging
	request.FormerWriterFence = write("target-fence.json", durablevolume.PreparationFence{Schema: durablevolume.PreparationFenceSchema, RootPath: source.root, RootInode: root.Ino, Purpose: "restore", FormerWritersStopped: true, NoPreviousTargetState: true, Evidence: "synthetic new physical custody at original logical path"})
	request.RestoreSource = &durablevolume.PreparationRestoreSource{Directory: archive, Inventory: reportRef, FormerWriterFence: oldFence}
	adapter := durablevolume.PreparationAdapter{Restore: func(ctx context.Context, name string, owner durablevolume.PreparationOwner, report durablevolume.Inventory) (durablevolume.PreparationOwnerPlan, error) {
		return PlanStorageRestore(ctx, name, owner, report, source.ownerLocal)
	}, InspectRestore: func(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan, report durablevolume.Inventory) ([]durablevolume.PreparedAttribute, error) {
		return InspectStorageRestore(ctx, root, owner, report, source.ownerLocal)
	}}
	plan, err := planFn(t.Context(), write("request.json", request), adapter, source.host)
	if err != nil {
		t.Fatal("miner restore planning refused original history", err)
	}
	result, err := applyFn(t.Context(), write("plan.json", plan), adapter, source.host)
	if err != nil || result.RestartAuthorized {
		t.Fatal("miner restore failed or granted activation", err)
	}
	for _, entry := range report.Entries {
		if entry.Kind != "file" {
			continue
		}
		before, err := os.ReadFile(filepath.Join(archive, entry.Path))
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(source.root, entry.Path))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("miner restore changed original payload", entry.Path, err)
		}
	}
	return durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), source.host)
}

// Restart classification may change the in-memory status, but the original
// signed transaction and nonce remain the sole outstanding send authority.
func TestMinerStorageRestoreClaimRetainsOriginalPendingTransaction(t *testing.T) {
	f := newMinerPreparationFixture(t, false)
	store, err := newClaimQueueStore(f.root, f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	queue, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	queue.LastDiscovered = 7
	key, err := crypto.HexToECDSA(strings.Repeat("11", 32))
	if err != nil {
		t.Fatal(err)
	}
	destination := common.HexToAddress("0x" + strings.Repeat("12", 20))
	tx, err := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: 71, To: &destination, Gas: 21000, GasPrice: big.NewInt(1), Value: big.NewInt(0)}), types.NewEIP155Signer(big.NewInt(964)), key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	queue.Entries["7"] = &ClaimQueueEntry{Epoch: 7, Status: "submitting", RawTxHex: "0x" + hex.EncodeToString(raw), TxHash: tx.Hash().Hex()}
	if err := errors.Join(store.save(queue), store.close()); err != nil {
		t.Fatal(err)
	}
	ctx := restoreMinerPreparationFixture(t, f)
	store, err = newClaimQueueStore(f.root, ctx)
	if err != nil {
		t.Fatal("restored actual claim owner refused", err)
	}
	retained, err := store.load()
	if closeErr := store.close(); err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	entry := retained.Entries["7"]
	if retained.LastDiscovered != 7 || entry == nil || entry.Status != "uncertain" || entry.RawTxHex != queue.Entries["7"].RawTxHex || entry.TxHash != queue.Entries["7"].TxHash {
		t.Fatal("restored claim owner discarded original transaction", entry)
	}
	retainedRaw, err := hex.DecodeString(strings.TrimPrefix(entry.RawTxHex, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	var retainedTx types.Transaction
	if err := retainedTx.UnmarshalBinary(retainedRaw); err != nil || retainedTx.Nonce() != 71 || retainedTx.Hash() != tx.Hash() {
		t.Fatal("restored claim changed exact original signed nonce", err)
	}
	before, err := os.ReadFile(filepath.Join(f.root+".original-held", "claim-queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(f.root, "claim-queue.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("opening restored claim rewrote original transaction bytes", err)
	}
}

// The original inventory signature still verifies after physical restoration;
// its prepared transaction cannot turn into a newly signed or empty record.
func TestMinerStorageRestoreFleetKeepsOriginalSignedInventory(t *testing.T) {
	network := newFleetMainnetTestFixture(t)
	record, signer := fleetRecoveryTestPrepared(t, network)
	f := newMinerPreparationFixture(t, true)
	store, err := openFleetRecoveryStore(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(store.put(record, signer), store.close()); err != nil {
		t.Fatal(err)
	}
	ctx := restoreMinerPreparationFixture(t, f)
	store, err = openFleetRecoveryStore(ctx)
	if err != nil {
		t.Fatal("restored fleet rejects exact original signed inventory", err)
	}
	if len(store.records) != 1 {
		t.Fatal("restored fleet lost original outstanding record", len(store.records))
	}
	retained := store.records[0]
	if retained.TxHash != record.TxHash || retained.Stage != record.Stage || !bytes.Equal(retained.Raw, record.Raw) || !bytes.Equal(retained.Signature, record.Signature) {
		t.Fatal("restored fleet changed original signed intent")
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(f.root+".original-held", "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(f.root, "journal.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("restored fleet rewrote original signed inventory", err)
	}
}
