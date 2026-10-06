//go:build linux

// A restore nomination is an unsigned proof candidate, never replacement
// signing authority. Actual public preparation and original reconciliation
// must agree on the same retained raw bytes and acknowledged predecessor.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Each request revision remains explicit and hash-bound before public plan;
// the original source checkpoint and transaction bytes are never rewritten.
func storagePreparationRawRequest(t *testing.T, f *storagePreparationCommandFixture, mutate func(*durablevolume.PreparationRequest)) {
	t.Helper()
	raw, err := os.ReadFile(f.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	mutate(&request)
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.requestHash = durablefixture.Digest(raw)
}

// Adding the optional field does not replace the original fixed capacity
// vector. Wrong fields are left intact for the real strict decoder to refuse.
func storagePreparationNominateRaw(t *testing.T, f *storagePreparationCommandFixture, name string) {
	t.Helper()
	storagePreparationRawRequest(t, f, func(request *durablevolume.PreparationRequest) {
		var scope map[string]any
		if err := json.Unmarshal(request.Owners[0].Inputs, &scope); err != nil {
			t.Fatal(err)
		}
		scope["pending_raw_member"] = name
		raw, err := json.Marshal(scope)
		if err != nil {
			t.Fatal(err)
		}
		request.Owners[0].Inputs = raw
	})
}

// A rejected plan must fail at its intended boundary before target custody or
// progress exists. The original source remains byte-identical after refusal.
func storagePreparationRefuseRawPlan(t *testing.T, f *storagePreparationRestoreFixture, reason string) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	code := runMain(f.target.ctx, []string{f.command, "plan", "--request", f.target.requestPath, "--request-sha256", f.target.requestHash}, &output, &diagnostic)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), reason) {
		t.Fatal("raw restore was admitted or failed at an unrelated boundary", reason, code, diagnostic.String())
	}
	names, err := os.ReadDir(f.target.root)
	if err != nil || len(names) != 0 {
		t.Fatal("refused raw restore changed target namespace", err)
	}
	if _, err := os.Lstat(filepath.Join(f.target.metadata, "preparation.jsonl")); !os.IsNotExist(err) {
		t.Fatal("refused raw restore created target progress", err)
	}
	staged, err := os.ReadDir(filepath.Join(filepath.Dir(f.target.root), "staging"))
	if err != nil || len(staged) != 0 {
		t.Fatal("refused raw restore changed staged custody", err)
	}
	storagePreparationAssertNativeCustody(t, f.source, f.original)
	for _, member := range f.report.Entries {
		if member.Kind == "file" {
			raw, err := os.ReadFile(filepath.Join(f.source.root, member.Path))
			if err != nil || durablefixture.Digest(raw) != member.Sha256 {
				t.Fatal("refused restore rewrote original source member", member.Path, err)
			}
		}
	}
}

// Negative reports are explicitly corrupted reviewed inputs. The copied
// archive and original source are distinct; no test regenerates source heads.
func storagePreparationRawReport(t *testing.T, f *storagePreparationRestoreFixture, mutate func(*durablevolume.Inventory)) {
	t.Helper()
	raw, err := json.Marshal(f.report)
	if err != nil {
		t.Fatal(err)
	}
	var report durablevolume.Inventory
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	mutate(&report)
	sort.Slice(report.Entries, func(a, b int) bool { return report.Entries[a].Path < report.Entries[b].Path })
	// Match the deliberately reviewed copied metadata so semantic negatives
	// reach the owner adapter, rather than fail an earlier archive comparison.
	for _, attribute := range report.Entries[0].OwnerAttributes {
		if err := unix.Setxattr(f.archive, attribute.Name, attribute.Value, 0); err != nil {
			t.Fatal(err)
		}
	}
	report.TotalBytes, report.TotalOwnerAttributes, report.TotalOwnerAttributeBytes = 0, 0, 0
	for _, entry := range report.Entries {
		if entry.Kind == "file" {
			report.TotalBytes += entry.Size
		}
		for _, attribute := range entry.OwnerAttributes {
			report.TotalOwnerAttributes++
			report.TotalOwnerAttributeBytes += uint64(len(attribute.Value))
		}
	}
	raw, err = json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.reportPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	storagePreparationRawRequest(t, f.target, func(request *durablevolume.PreparationRequest) {
		request.RestoreSource.Inventory.Sha256 = durablefixture.Digest(raw)
	})
}

// Only inode-bearing fields may differ before runtime reconciliation. This
// checks every other original checkpoint field instead of excluding the head.
func storagePreparationAssertRawPending(t *testing.T, f *storagePreparationRestoreFixture) {
	t.Helper()
	original := map[string]any{}
	if err := json.Unmarshal(f.original.anchor, &original); err != nil {
		t.Fatal(err)
	}
	restored := map[string]any{}
	if err := json.Unmarshal(storagePreparationOwnerAttribute(t, f.target.root, chain.NativeJournalCustodyAttribute), &restored); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ field, name string }{
		{field: "directory_inode", name: "."}, {field: "raw_directory_inode", name: "native-transactions"},
	} {
		var stat unix.Stat_t
		if err := unix.Stat(filepath.Join(f.target.root, item.name), &stat); err != nil || restored[item.field] != float64(stat.Ino) || restored[item.field] == original[item.field] {
			t.Fatal("restored raw directory does not bind the new physical generation", item, err)
		}
		delete(original, item.field)
		delete(restored, item.field)
	}
	var journal unix.Stat_t
	if err := unix.Stat(filepath.Join(f.target.root, "native-transactions.jsonl"), &journal); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"committed", "pending"} {
		old, ok := original[phase].(map[string]any)
		next, nextOk := restored[phase].(map[string]any)
		if !ok || !nextOk || next["journal_inode"] != float64(journal.Ino) || next["journal_inode"] == old["journal_inode"] || next["raw_sha256"] == old["raw_sha256"] {
			t.Fatal("restore cleared original raw phase or failed to rebind physical custody", phase)
		}
		delete(old, "journal_inode")
		delete(next, "journal_inode")
		delete(old, "raw_sha256")
		delete(next, "raw_sha256")
	}
	if !reflect.DeepEqual(original, restored) {
		t.Fatal("restore changed original log hash, size, count, schema or pending phase", original, restored)
	}
	for _, member := range f.report.Entries {
		if member.Kind != "file" {
			continue
		}
		before, err := os.ReadFile(filepath.Join(f.source.root, member.Path))
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(f.target.root, member.Path))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("restore changed original member bytes or premature pending filename", member.Path, err)
		}
	}
}

// Reconciliation consumes only the original pending bytes; a second explicit
// reconciliation cannot append, rename or regenerate that already proven head.
func storagePreparationAssertRawRuntime(t *testing.T, f *storagePreparationRestoreFixture, ctx context.Context) {
	t.Helper()
	f.assertRuntime(t, ctx, true)
	name := strings.TrimPrefix(chain.ExtrinsicHash(f.pendingRaw).Hex(), "0x") + ".scale"
	path := filepath.Join(f.target.root, "native-transactions", name)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, f.pendingRaw) {
		t.Fatal("reconciliation lost original nominated raw bytes", err)
	}
	image := storagePreparationNativeCustody(t, f.target)
	reconcile := chain.ReconcileDurableJournal
	if f.ownerLocal {
		reconcile = chain.ReconcileOwnerLocalJournal
	}
	journal, err := reconcile(ctx, f.target.root)
	if err != nil {
		t.Fatal("repeated original reconciliation cannot reopen", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	storagePreparationAssertNativeCustody(t, f.target, image)
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("repeated reconciliation replaced completed raw member", err)
	}
	raw, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, f.pendingRaw) {
		t.Fatal("repeated reconciliation changed retained raw bytes", err)
	}
}

// An acknowledged rename retains no temporary filename. The explicit witness
// must prove both aggregate hashes without any new signing or nonce selection.
func TestStoragePreparationRestoreNamedRawKeepsOriginalCheckpoint(t *testing.T) {
	for _, ownerLocal := range []bool{false, true} {
		f := newStoragePreparationRestoreFixture(t, ownerLocal, "raw")
		name := strings.TrimPrefix(chain.ExtrinsicHash(f.pendingRaw).Hex(), "0x") + ".scale"
		storagePreparationNominateRaw(t, f.target, name)
		path, hash := storagePreparationFreezeOwnerPlan(t, f.target, f.command)
		ctx := f.apply(t, path, hash)
		storagePreparationAssertRawPending(t, f)
		storagePreparationAssertRawRuntime(t, f, ctx)
	}
}

// A complete temporary already nominates its one canonical member. Offline
// restore retains its actual name until explicit original runtime recovery.
func TestStoragePreparationRestoreTemporaryRawKeepsOriginalCheckpoint(t *testing.T) {
	for _, ownerLocal := range []bool{false, true} {
		f := newStoragePreparationRestoreFixture(t, ownerLocal, "raw-temporary")
		path, hash := storagePreparationFreezeOwnerPlan(t, f.target, f.command)
		ctx := f.apply(t, path, hash)
		storagePreparationAssertRawPending(t, f)
		storagePreparationAssertRawRuntime(t, f, ctx)
	}
}

// A canonical existing member is still the wrong witness if removing it
// fails to reproduce the acknowledged predecessor. Aliases are never guessed.
func TestStoragePreparationRestoreRawNominationRequiresExactDelta(t *testing.T) {
	f := newStoragePreparationRestoreFixture(t, false, "raw")
	name := strings.TrimPrefix(chain.ExtrinsicHash(f.pendingRaw).Hex(), "0x") + ".scale"
	for _, c := range []struct{ name, reason string }{
		{name: f.rawName, reason: "does not reproduce the original predecessor census"},
		{name: strings.Repeat("0", 64) + ".scale", reason: "do not match the exact intended checkpoint"},
		{name: strings.ToUpper(name), reason: "nomination must be one canonical final raw member"},
		{name: "./" + name, reason: "nomination must be one canonical final raw member"},
		{name: name + ".pending-" + strings.Repeat("0", 32), reason: "nomination must be one canonical final raw member"},
		{name: "", reason: "original predecessor member authority"},
	} {
		storagePreparationNominateRaw(t, f.target, c.name)
		storagePreparationRefuseRawPlan(t, f, c.reason)
	}
	storagePreparationNominateRaw(t, f.target, f.rawName)
	storagePreparationRawRequest(t, f.target, func(request *durablevolume.PreparationRequest) {
		witness, err := json.Marshal(name)
		if err != nil {
			t.Fatal(err)
		}
		raw := bytes.TrimSuffix(request.Owners[0].Inputs, []byte("}"))
		request.Owners[0].Inputs = append(append(raw, []byte(`,"pending_raw_member":`)...), append(witness, '}')...)
	})
	storagePreparationRefuseRawPlan(t, f, "ambiguous repeated raw nomination")
}

// Malformed reviewed metadata cannot rename, alias or multiply the nominated
// member. These negatives leave the authentic original source untouched.
func TestStoragePreparationRestoreRawNamesRejectAliasesAndCollisions(t *testing.T) {
	for _, mode := range []string{"alias", "collision", "two-temporaries", "case"} {
		f := newStoragePreparationRestoreFixture(t, false, "raw-temporary")
		reason := ""
		storagePreparationRawReport(t, f, func(report *durablevolume.Inventory) {
			var temporary, original *durablevolume.InventoryEntry
			for index := range report.Entries {
				entry := &report.Entries[index]
				if strings.Contains(entry.Path, ".pending-") {
					temporary = entry
				} else if entry.Path == "native-transactions/"+f.rawName {
					original = entry
				}
			}
			if temporary == nil || original == nil {
				t.Fatal("actual raw interruption did not retain both members")
			}
			switch mode {
			case "alias":
				temporary.Physical.Inode = original.Physical.Inode
				reason = "aliased or unordered original member generations"
			case "collision":
				oldPath := original.Path
				original.Path = strings.Split(temporary.Path, ".pending-")[0]
				if err := os.Rename(filepath.Join(f.archive, oldPath), filepath.Join(f.archive, original.Path)); err != nil {
					t.Fatal(err)
				}
				reason = "raw census is duplicated"
			case "two-temporaries":
				oldPath := original.Path
				original.Path += ".pending-" + strings.Repeat("0", 32)
				if err := os.Rename(filepath.Join(f.archive, oldPath), filepath.Join(f.archive, original.Path)); err != nil {
					t.Fatal(err)
				}
				reason = "ambiguous pending raw members"
			case "case":
				oldPath := temporary.Path
				temporary.Path = filepath.Join("native-transactions", strings.ToUpper(filepath.Base(temporary.Path)))
				if err := os.Rename(filepath.Join(f.archive, oldPath), filepath.Join(f.archive, temporary.Path)); err != nil {
					t.Fatal(err)
				}
				reason = "raw name is not canonical"
			}
		})
		storagePreparationRefuseRawPlan(t, f, reason)
	}
}

// Even a correctly named temporary cannot authorize absent or partial bytes;
// updating the external report cannot change the original checkpoint hashes.
func TestStoragePreparationRestorePartialRawRemainsStopped(t *testing.T) {
	f := newStoragePreparationRestoreFixture(t, false, "raw-temporary")
	var pendingPath string
	for _, entry := range f.report.Entries {
		if strings.Contains(entry.Path, ".pending-") {
			pendingPath = entry.Path
		}
	}
	if pendingPath == "" {
		t.Fatal("raw interruption lacks its actual pending file")
	}
	partial := f.pendingRaw[:len(f.pendingRaw)-1]
	if err := os.WriteFile(filepath.Join(f.archive, pendingPath), partial, 0600); err != nil {
		t.Fatal(err)
	}
	storagePreparationRawReport(t, f, func(report *durablevolume.Inventory) {
		for index := range report.Entries {
			if report.Entries[index].Path == pendingPath {
				report.Entries[index].Size = uint64(len(partial))
				report.Entries[index].Sha256 = durablefixture.Digest(partial)
			}
		}
	})
	storagePreparationRefuseRawPlan(t, f, "do not match the exact intended checkpoint")
	if err := os.Remove(filepath.Join(f.archive, pendingPath)); err != nil {
		t.Fatal(err)
	}
	storagePreparationRawReport(t, f, func(report *durablevolume.Inventory) {
		for index := range report.Entries {
			if report.Entries[index].Path == pendingPath {
				report.Entries = append(report.Entries[:index], report.Entries[index+1:]...)
				break
			}
		}
	})
	storagePreparationRefuseRawPlan(t, f, "original predecessor member authority")
}

// A nomination cannot invent a pending transition in clean custody or change
// an independently retained log append into a different raw publication.
func TestStoragePreparationRestoreRawNominationCannotCreateIntent(t *testing.T) {
	for _, pending := range []string{"", "append"} {
		f := newStoragePreparationRestoreFixture(t, false, pending)
		storagePreparationNominateRaw(t, f.target, f.rawName)
		reason := "raw nomination has no original pending checkpoint"
		if pending == "append" {
			reason = "not an exact log append or one-member raw delta"
		}
		storagePreparationRefuseRawPlan(t, f, reason)
	}
}

// Original checkpoint bounds are checked before semantic slicing; malformed
// reviewed metadata must produce a refusal, never a panic or target effect.
func TestStoragePreparationRestoreRawRejectsMalformedCheckpointBounds(t *testing.T) {
	f := newStoragePreparationRestoreFixture(t, false, "raw")
	storagePreparationNominateRaw(t, f.target, strings.TrimPrefix(chain.ExtrinsicHash(f.pendingRaw).Hex(), "0x")+".scale")
	for _, phase := range []string{"committed", "pending"} {
		for _, size := range []int64{-1, 16*1024*1024 + 1} {
			storagePreparationRawReport(t, f, func(report *durablevolume.Inventory) {
				for index := range report.Entries[0].OwnerAttributes {
					attribute := &report.Entries[0].OwnerAttributes[index]
					if attribute.Name != chain.NativeJournalCustodyAttribute {
						continue
					}
					var checkpoint map[string]any
					if err := json.Unmarshal(attribute.Value, &checkpoint); err != nil {
						t.Fatal(err)
					}
					checkpoint[phase].(map[string]any)["journal_size"] = size
					raw, err := json.Marshal(checkpoint)
					if err != nil {
						t.Fatal(err)
					}
					attribute.Value, attribute.Sha256 = raw, durablefixture.Digest(raw)
				}
			})
			storagePreparationRefuseRawPlan(t, f, "original checkpoint bounds are invalid")
		}
	}
}

// The optional restore field remains invalid in the fresh public preparation
// profile, which grants no retained intent.
func TestStoragePreparationRawWitnessIsRestoreOnly(t *testing.T) {
	owner := storagePreparationNativeOwner()
	f := newStoragePreparationCommandFixture(t)
	storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{owner})
	storagePreparationNominateRaw(t, f, strings.Repeat("0", 64)+".scale")
	var output, diagnostic bytes.Buffer
	code := runMain(f.ctx, []string{"storage-prepare", "plan", "--request", f.requestPath, "--request-sha256", f.requestHash}, &output, &diagnostic)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), `unknown field "pending_raw_member"`) {
		t.Fatal("fresh preparation accepted restore intent", code, diagnostic.String())
	}
	entries, err := os.ReadDir(f.root)
	if err != nil || len(entries) != 0 {
		t.Fatal("fresh refusal changed target", err)
	}
}
