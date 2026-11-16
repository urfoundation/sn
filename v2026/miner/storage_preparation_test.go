//go:build linux || darwin

// These fixtures run the same fixed adapters as the public command, then the
// actual fleet/claim constructors. No target is enrolled by a test helper.
package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The test owns one explicitly fresh namespace and records exact apply input.
type minerPreparationFixture struct {
	ctx        context.Context
	root       string
	plan       durablevolume.Reference
	adapter    durablevolume.PreparationAdapter
	host       durablevolume.Host
	ownerLocal bool
}

// Synthetic facts are separate from real inode, xattr and publication work.
func newMinerPreparationFixture(t *testing.T, ownerLocal bool) *minerPreparationFixture {
	t.Helper()
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "claim-queue")
	kind := "provider-claim-queue"
	scope := minerStoragePreparationScope{Schema: "urnetwork-miner-snapshot-preparation-v1", Name: "claim-queue.json", MaximumBytes: maximumClaimQueueBytes}
	if ownerLocal {
		root = filepath.Join(parent, "fleet-mainnet-recovery")
		kind = "fleet-recovery"
		scope = minerStoragePreparationScope{Schema: "urnetwork-miner-snapshot-preparation-v1", Name: "journal.json", MaximumBytes: fleetRecoveryMaxBytes, MaximumRecords: fleetRecoveryMaxRecords, MaximumRawRecordBytes: fleetRecoveryMaxRaw, MaximumManifestBytes: 256 * 1024}
		t.Setenv("URNETWORK_STATE_DIR", parent)
	}
	metadata, staging, observed := filepath.Join(parent, "metadata"), filepath.Join(parent, "staging"), filepath.Join(parent, "observed")
	for _, path := range []string{root, metadata, staging, observed} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	physical := durablefixture.New(t, t.Context(), observed)
	config, err := durablevolume.Load(physical.Reference)
	if err != nil {
		t.Fatal(err)
	}
	volume := config.Volumes[0]
	var stat unix.Stat_t
	if err := unix.Stat(root, &stat); err != nil {
		t.Fatal(err)
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
	fence := write("former-writer.json", durablevolume.PreparationFence{Schema: durablevolume.PreparationFenceSchema, RootPath: root, RootInode: stat.Ino, Purpose: "fresh", FormerWritersStopped: true, NoPreviousOwnerState: true, Evidence: "synthetic fresh owner; no service or signing"})
	raw, err := json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	request := durablevolume.PreparationRequest{Schema: durablevolume.PreparationRequestSchema, Purpose: "fresh", Scope: "daemon", MountPath: volume.MountPath, FilesystemUuid: volume.FilesystemUuid, FilesystemType: volume.FilesystemType, MinAvailableBytes: 1024 * 1024, MinAvailableInodes: 64,
		RootPath: root, MarkerPath: filepath.Join(metadata, "identity"), LeasePath: filepath.Join(metadata, "lease"), DeclarationPath: filepath.Join(metadata, "declaration.json"), ControlPath: filepath.Join(metadata, "control.jsonl"), StagingDirectory: staging, FormerWriterFence: fence,
		Limits: durablevolume.PreparationLimits{MaxEntries: 16, MaxBytes: 1024 * 1024, MaxDepth: 4, MaxOwnerAttributes: 4, MaxOwnerAttributeBytes: 16 * 1024, MaxPlanBytes: 1024 * 1024},
		Owners: []durablevolume.PreparationOwner{{Kind: kind, RelativePath: ".", Purpose: "fresh", Inputs: raw}}}
	if ownerLocal {
		request.Scope = "owner-local"
	}
	requestRef := write("request.json", request)
	adapter := durablevolume.PreparationAdapter{Build: func(ctx context.Context, parent *os.File, name string, owner durablevolume.PreparationOwner) (durablevolume.PreparationOwnerPlan, error) {
		return BuildFreshStoragePreparation(ctx, parent, name, owner, ownerLocal)
	},
		Inspect: func(ctx context.Context, root *os.File, owner durablevolume.PreparationOwnerPlan) ([]durablevolume.PreparedAttribute, error) {
			return InspectFreshStoragePreparation(ctx, root, owner, ownerLocal)
		}}
	planFn, applyFn := durablevolume.PlanPreparationWithHost, durablevolume.ApplyPreparationWithHost
	if ownerLocal {
		planFn, applyFn = durablevolume.PlanOwnerLocalPreparationWithHost, durablevolume.ApplyOwnerLocalPreparationWithHost
	}
	plan, err := planFn(t.Context(), requestRef, adapter, physical.Host)
	if err != nil {
		t.Fatal("fixed miner profile cannot plan", err)
	}
	planRef := write("plan.json", plan)
	result, err := applyFn(t.Context(), planRef, adapter, physical.Host)
	if err != nil || result.RestartAuthorized {
		t.Fatal("fixed miner profile cannot apply or grants restart", err)
	}
	return &minerPreparationFixture{ctx: durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), physical.Host), root: root, plan: planRef, adapter: adapter, host: physical.Host, ownerLocal: ownerLocal}
}

// Actual owner startup writes its original empty journal and retains the same
// marker. Reopening does not invent a signature or replacement generation.
func TestMinerPreparationFleetActualConstructorKeepsFreshCustody(t *testing.T) {
	f := newMinerPreparationFixture(t, true)
	marker := filepath.Join(f.root, "initialized")
	before, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := openFleetRecoveryStore(f.ctx)
	if err != nil {
		t.Fatal("prepared fleet cannot open real owner", err)
	}
	if len(owner.records) != 0 {
		t.Fatal("prepared fleet acquired intent")
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root, "journal.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var journal fleetRecoveryJournal
	if err := json.Unmarshal(original, &journal); err != nil || journal.Schema != fleetRecoverySchema || len(journal.Records) != 0 || len(journal.Signature) != 0 {
		t.Fatal("fresh owner fabricated signed history", err)
	}
	again, err := openFleetRecoveryStore(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.close(); err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(path)
	after, statErr := os.Stat(marker)
	markerRaw, readErr := os.ReadFile(marker)
	if err != nil || statErr != nil || readErr != nil || !bytes.Equal(original, retained) || !os.SameFile(before, after) || len(markerRaw) != 0 {
		t.Fatal("fleet reopen rewrote original custody", err, statErr, readErr)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if lost, err := openFleetRecoveryStore(f.ctx); err == nil {
		lost.close()
		t.Fatal("missing completed fleet journal recreated")
	}
	if _, err := durablevolume.ApplyOwnerLocalPreparationWithHost(t.Context(), f.plan, f.adapter, f.host); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("original fresh plan reset completed fleet custody", err)
	}
}

// Discovery progress is written by the real queue after preparation; deleting
// that acknowledged file cannot cause startup to seed a fresh queue again.
func TestMinerPreparationClaimActualConstructorRetainsProgress(t *testing.T) {
	f := newMinerPreparationFixture(t, false)
	owner, err := newClaimQueueStore(f.root, f.ctx)
	if err != nil {
		t.Fatal("prepared claim queue cannot open real owner", err)
	}
	queue, err := owner.load()
	if err != nil || queue.LastDiscovered != -1 || len(queue.Entries) != 0 {
		t.Fatal("fresh queue gained progress", err)
	}
	queue.LastDiscovered = 7
	queue.Entries["7"] = &ClaimQueueEntry{Epoch: 7, Status: "pending"}
	if err := owner.save(queue); err != nil {
		t.Fatal(err)
	}
	if err := owner.close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.root, "claim-queue.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := newClaimQueueStore(f.root, f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	queue, err = again.load()
	if err != nil || queue.LastDiscovered != 7 || len(queue.Entries) != 1 || queue.Entries["7"].Status != "pending" {
		t.Fatal("reopen lost acknowledged discovery", err)
	}
	if err := again.close(); err != nil {
		t.Fatal(err)
	}
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("queue reopen rewrote original bytes", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if lost, err := newClaimQueueStore(f.root, f.ctx); err == nil {
		lost.close()
		t.Fatal("missing completed queue recreated")
	}
	if _, err := durablevolume.ApplyPreparationWithHost(t.Context(), f.plan, f.adapter, f.host); !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("original fresh plan reset completed claim progress", err)
	}
}

// A replacement auxiliary must not acquire the original fleet enrollment.
func TestMinerPreparationFleetRefusesReplacedMarkerBeforeStartup(t *testing.T) {
	f := newMinerPreparationFixture(t, true)
	path := filepath.Join(f.root, "initialized")
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if owner, err := openFleetRecoveryStore(f.ctx); !errors.Is(err, durablevolume.ErrIdentity) {
		if owner != nil {
			owner.close()
		}
		t.Fatal("byte-identical new marker gained original head", err)
	}
	if _, err := os.Lstat(filepath.Join(f.root, "journal.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused marker caused first journal publication", err)
	}
}
