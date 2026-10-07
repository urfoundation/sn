//go:build linux || darwin

// Shared bootstrap custody must restore its complete named owner union. These
// controls use actual public plan/apply and the original runtime readers.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

type storageUnionRestoreFixture struct {
	storage         *storageSnapshotRestoreFixture
	owners          []durablevolume.PreparationOwner
	original        map[string]string
	memberName      string
	memberRaw       []byte
	claim           string
	monitorIdentity identityExpectation
	monitorState    *monitorState
}

// Wire construction lets the old strict decoder serve as the causal baseline.
func storageUnionRestoreRequest(t *testing.T, f *storageUnionRestoreFixture, owners []durablevolume.PreparationOwner, omitCoverage bool) {
	t.Helper()
	raw, err := os.ReadFile(f.storage.target.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]json.RawMessage
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	wire := make([]map[string]any, 0, len(owners))
	for _, owner := range owners {
		item := map[string]any{"kind": owner.Kind, "relative_path": owner.RelativePath, "purpose": "restore", "inputs": owner.Inputs}
		if !omitCoverage {
			item["restore_coverage"] = "complete-union-v1"
		}
		wire = append(wire, item)
	}
	request["owners"], err = json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.storage.target.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.storage.target.requestHash = durablefixture.Digest(raw)
}

// Fresh preparation already supports these disjoint real owners. Restoration
// must retain all five snapshot heads plus the signed-member census together.
func newStorageUnionRestoreFixture(t *testing.T, unknown bool) *storageUnionRestoreFixture {
	t.Helper()
	source := newStoragePreparationCommandFixture(t)
	owners := []durablevolume.PreparationOwner{
		storagePreparationSnapshotOwner(t, "mainnet-bootstrap-root", bootstrapRootProgressFile, 16*1024),
		storagePreparationSnapshotOwner(t, "mainnet-bootstrap-chain", "bootstrap-chain.json", rootServiceStoreLimit),
		storagePreparationSnapshotOwner(t, "mainnet-root-service", "root-service.json", rootServiceStoreLimit),
		storagePreparationSnapshotOwner(t, "mainnet-owner-trim", "owner-trim.json", ownerTrimStoreLimit),
		storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "monitor.json", maxRpcReplyBytes),
		storagePreparationDirectoryOwner(t, "mainnet-successor-local-members"),
	}
	storagePreparationOwnerRequest(t, source, "daemon", owners)
	ctx := storagePreparationApplyOwnerCommand(t, source, "storage-prepare")
	identity := identityExpectation{NativeChain: "synthetic-chain", GenesisHash: "0x" + strings.Repeat("41", 32), EvmChainId: mainnetEvmChainId}
	state := &monitorState{lastNumber: 71, lastHash: "0x" + strings.Repeat("42", 32), lastProgressAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), lastSuccessAt: time.Date(2020, 1, 1, 0, 1, 0, 0, time.UTC)}
	monitor, err := openMonitorCheckpoint(filepath.Join(source.root, "monitor.json"), identity, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(monitor.save(state), monitor.close()); err != nil {
		t.Fatal(err)
	}
	root, err := bootstrapSuccessorPhysicalRoot(source.root)
	if err != nil {
		t.Fatal(err)
	}
	claim := safeReleaseHash([]byte("synthetic shared-root original claimant"))
	name := bootstrapSuccessorExecutionPrefix + "synthetic-union.json"
	payload := []byte(`{"nonce":17,"signature":"synthetic immutable original payload"}`)
	member := openStorageMemberRestoreTestOwner(t, ctx, source.root, root, claim, false, nil)
	if err := errors.Join(member.publish(name, "retained", payload), member.close()); err != nil {
		t.Fatal(err)
	}
	if unknown {
		if err := os.WriteFile(filepath.Join(source.root, "unassigned-history.json"), []byte("synthetic unknown custody"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	original := bootstrapSuccessorPreparationTestFiles(t, source.root)
	storage := storageSnapshotRestoreTarget(t, source, ctx, owners[0], false)
	result := &storageUnionRestoreFixture{storage: storage, owners: owners, original: original, memberName: name, memberRaw: payload, claim: claim, monitorIdentity: identity, monitorState: state}
	storageUnionRestoreRequest(t, result, owners, false)
	return result
}

func (self *storageUnionRestoreFixture) inspect(t *testing.T, result durablevolume.PreparationResult, plan durablevolume.PreparationPlan) {
	t.Helper()
	if result.RestartAuthorized || len(plan.Owners) != len(self.owners) || len(plan.Derivations) != 1 {
		t.Fatal("shared restore lost owner/derivation scope or gained activation")
	}
	ctx := durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), self.storage.target.storage.Host)
	current := bootstrapSuccessorPreparationTestFiles(t, self.storage.target.root)
	if len(current) != len(self.original) {
		t.Fatal("shared restore discarded or added an original member")
	}
	for name, raw := range self.original {
		if name != ".successor-local-members.json" && current[name] != raw {
			t.Fatal("shared restore rewrote original payload or marker", name)
		}
	}
	for _, owner := range self.owners[:4] {
		var scope storageSnapshotPreparationScope
		if err := json.Unmarshal(owner.Inputs, &scope); err != nil {
			t.Fatal(err)
		}
		passive, err := openBootstrapUnclaimedSnapshot(ctx, filepath.Join(self.storage.target.root, scope.Name), owner.Kind, int(scope.MaximumBytes))
		if err != nil {
			t.Fatal("restored shared passive bootstrap owner refuses original head", owner.Kind, err)
		}
		if err := passive.close(); err != nil {
			t.Fatal(err)
		}
	}
	monitor, err := openMonitorCheckpoint(filepath.Join(self.storage.target.root, "monitor.json"), self.monitorIdentity, ctx)
	if err != nil {
		t.Fatal("restored shared monitor owner cannot open", err)
	}
	actual, err := monitor.load()
	if closeErr := monitor.close(); err != nil || closeErr != nil || actual == nil || actual.lastNumber != self.monitorState.lastNumber || actual.lastHash != self.monitorState.lastHash || !actual.lastProgressAt.Equal(self.monitorState.lastProgressAt) {
		t.Fatal("shared restore lost actual monitor continuation", err, closeErr)
	}
	root, err := bootstrapSuccessorPhysicalRoot(self.storage.target.root)
	if err != nil {
		t.Fatal(err)
	}
	member := openStorageMemberRestoreTestOwner(t, ctx, self.storage.target.root, root, self.claim, false, nil)
	raw, err := member.read(self.memberName)
	if closeErr := member.close(); err != nil || closeErr != nil || !bytes.Equal(raw, self.memberRaw) {
		t.Fatal("shared restore lost actual member continuation", err, closeErr)
	}
	for _, source := range plan.Sources {
		if source.File.Path != self.memberName && source.File.Path != ".successor-local-members.json" {
			continue
		}
		var stat unix.Stat_t
		if err := unix.Stat(filepath.Join(self.storage.target.root, source.File.Path), &stat); err != nil || stat.Ino != source.Identity.Inode {
			t.Fatal("shared restore failed to transfer the exact member inode", source.File.Path, err)
		}
	}
}

// The actual public command must preserve all co-owned heads and their
// original continuations instead of treating an incomplete subset as restored.
func TestStoragePreparationRestoreCompleteUnionOpensAllOriginalOwners(t *testing.T) {
	f := newStorageUnionRestoreFixture(t, false)
	path, hash := storagePreparationFreezeOwnerPlan(t, f.storage.target, "storage-prepare")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plan durablevolume.PreparationPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	var first durablevolume.PreparationResult
	var retained map[string]string
	for attempt := 0; attempt < 2; attempt++ {
		var output, diagnostic bytes.Buffer
		if code := runMain(f.storage.target.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
			t.Fatal("public complete owner union cannot restore", code, diagnostic.String())
		}
		var result durablevolume.PreparationResult
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		f.inspect(t, result, plan)
		if attempt == 0 {
			first = result
			retained = bootstrapSuccessorPreparationTestFiles(t, f.storage.target.root)
		} else if result != first || !maps.Equal(retained, bootstrapSuccessorPreparationTestFiles(t, f.storage.target.root)) {
			t.Fatal("repeated union restore changed completed original custody")
		}
	}
}

// Omitted/duplicated owners and unknown original files are never filtered out
// to make a partial plan pass. Refusal precedes any target head or member.
func TestStoragePreparationRestoreUnionRefusesIncompleteAndOverlappingCoverage(t *testing.T) {
	for _, mode := range []string{"omitted", "duplicate", "unknown", "legacy"} {
		f := newStorageUnionRestoreFixture(t, mode == "unknown")
		owners := append([]durablevolume.PreparationOwner(nil), f.owners...)
		switch mode {
		case "omitted":
			owners = owners[1:]
		case "duplicate":
			owners = append(owners, owners[0])
		}
		storageUnionRestoreRequest(t, f, owners, mode == "legacy")
		var output, diagnostic bytes.Buffer
		code := runMain(f.storage.target.ctx, []string{"storage-prepare", "plan", "--request", f.storage.target.requestPath, "--request-sha256", f.storage.target.requestHash}, &output, &diagnostic)
		if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "coverage") {
			t.Fatal("incomplete owner union was admitted or reached an unrelated boundary", mode, code, diagnostic.String())
		}
		entries, err := os.ReadDir(f.storage.target.root)
		if err != nil || len(entries) != 0 {
			t.Fatal("refused owner union changed live target", mode, err)
		}
		if !maps.Equal(f.original, bootstrapSuccessorPreparationTestFiles(t, f.storage.heldSource)) || !maps.Equal(f.original, bootstrapSuccessorPreparationTestFiles(t, f.storage.archive)) {
			t.Fatal("refused owner union altered historical source", mode)
		}
	}
}

// Loss of acknowledgement after one real owner head leaves that head intact;
// joined exact-plan application completes the remaining distinct owners.
func TestStoragePreparationRestoreUnionResumesOriginalPartialHeadSet(t *testing.T) {
	f := newStorageUnionRestoreFixture(t, false)
	path, hash := storagePreparationFreezeOwnerPlan(t, f.storage.target, "storage-prepare")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plan durablevolume.PreparationPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.storage.target.ctx)
	defer cancel()
	called := false
	marker := filepath.Join(f.storage.target.root, bootstrapRootProgressFile+".lock")
	attribute := durablehead.Attribute("mainnet-bootstrap-root", bootstrapRootProgressFile)
	host := &storagePreparationObservedHost{Host: f.storage.target.storage.Host, observe: func(*os.File) {
		if called {
			return
		}
		if _, err := unix.Getxattr(marker, attribute, make([]byte, 4096)); err == nil {
			called = true
			cancel()
		}
	}}
	var output, diagnostic bytes.Buffer
	if code := runMain(durablepath.WithHost(ctx, host), []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code == 0 || !called || output.Len() != 0 {
		t.Fatal("union restore did not stop after an actual retained head", code, called, diagnostic.String())
	}
	before := storagePreparationOwnerAttribute(t, marker, attribute)
	output.Reset()
	diagnostic.Reset()
	if code := runMain(f.storage.target.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("joined union restore lost original progress", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, storagePreparationOwnerAttribute(t, marker, attribute)) {
		t.Fatal("union recovery replaced an acknowledged owner head")
	}
	f.inspect(t, result, plan)
}
