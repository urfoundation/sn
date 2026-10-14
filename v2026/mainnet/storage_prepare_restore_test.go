//go:build linux || darwin

// Actual public plan/apply restore must retain historical native custody while
// rebinding only the new target's physical metadata. Every payload is synthetic.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Kernel facts include both private synthetic source and target mount views;
// all path, inode, permission, hash, write and sync operations stay real.
type storagePreparationRestoreHost struct {
	durablevolume.Host
	source durablevolume.Host
}

// A backup input may reside on a different approved physical source view.
func (self *storagePreparationRestoreHost) Mounts() ([]durablevolume.Mount, error) {
	target, err := self.Host.Mounts()
	if err != nil {
		return nil, err
	}
	source, err := self.source.Mounts()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, mount := range target {
		seen[mount.Path] = true
	}
	for _, mount := range source {
		if !seen[mount.Path] {
			target = append(target, mount)
			seen[mount.Path] = true
		}
	}
	return target, nil
}

// The fixture retains both old and new roots plus the exact public source
// export. It never directly creates a target owner checkpoint.
type storagePreparationRestoreFixture struct {
	source     *storagePreparationCommandFixture
	target     *storagePreparationCommandFixture
	command    string
	archive    string
	reportPath string
	report     durablevolume.Inventory
	original   storagePreparationNativeImage
	rawName    string
	raw        []byte
	pendingRaw []byte
	ownerLocal bool
}

// Source entries are produced by the real journal; pending states are forced
// by a context barrier after actual payload publication, not fabricated heads.
func newStoragePreparationRestoreFixture(t *testing.T, ownerLocal bool, pending string) *storagePreparationRestoreFixture {
	t.Helper()
	source, target := newStoragePreparationCommandFixture(t), newStoragePreparationCommandFixture(t)
	command, scope := "storage-prepare", "daemon"
	open := chain.OpenDurableJournal
	if ownerLocal {
		command, scope, open = "storage-owner-prepare", "owner-local", chain.OpenOwnerLocalJournal
	}
	storagePreparationOwnerRequest(t, source, scope, []durablevolume.PreparationOwner{storagePreparationNativeOwner()})
	sourceContext := storagePreparationApplyOwnerCommand(t, source, command)
	ref, _ := durablevolume.ReferenceFromContext(sourceContext)
	ctx, cancel := context.WithCancel(sourceContext)
	t.Cleanup(cancel)
	armed, called := false, false
	nextRaw := []byte("synthetic second original raw intent, never sent")
	observed := &storagePreparationObservedHost{Host: source.storage.Host, observe: func(*os.File) {
		if !armed || called {
			return
		}
		data := make([]byte, 4096)
		n, err := unix.Getxattr(source.root, chain.NativeJournalCustodyAttribute, data)
		if err != nil {
			return
		}
		var head struct {
			Committed struct {
				RawCount int `json:"raw_count"`
			} `json:"committed"`
			Pending *struct {
				JournalSize int64 `json:"journal_size"`
				RawCount    int   `json:"raw_count"`
			} `json:"pending"`
		}
		if json.Unmarshal(data[:n], &head) != nil || head.Pending == nil {
			return
		}
		if pending == "append" {
			info, err := os.Stat(filepath.Join(source.root, "native-transactions.jsonl"))
			if err != nil || info.Size() != head.Pending.JournalSize {
				return
			}
		} else if pending == "raw" || pending == "raw-temporary" {
			names, err := os.ReadDir(filepath.Join(source.root, "native-transactions"))
			if err != nil || len(names) != head.Pending.RawCount || head.Pending.RawCount != head.Committed.RawCount+1 {
				return
			}
			temporaries := 0
			for _, name := range names {
				if strings.Contains(name.Name(), ".pending-") {
					temporaries++
					raw, err := os.ReadFile(filepath.Join(source.root, "native-transactions", name.Name()))
					if err != nil || !bytes.Equal(raw, nextRaw) {
						return
					}
				} else if !strings.HasSuffix(name.Name(), ".scale") {
					return
				}
			}
			if pending == "raw" && temporaries != 0 || pending == "raw-temporary" && temporaries != 1 {
				return
			}
		} else {
			return
		}
		called = true
		cancel()
	}}
	journal, err := open(durablepath.WithHost(ctx, observed), source.root)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("synthetic original retained SCALE bytes with unchanged signing intent")
	hash := chain.ExtrinsicHash(raw)
	if err := journal.SaveRaw(hash, raw); err != nil {
		t.Fatal(err)
	}
	entry := chain.JournalEntry{Time: "2020-01-01T00:00:00Z", Command: "synthetic-retained", Nonce: 71, ExtrinsicHash: hash.Hex(), Stage: chain.JournalStageBroadcast}
	if err := journal.Append(entry); err != nil {
		t.Fatal(err)
	}
	entry.Stage = chain.JournalStageFinalized
	if pending == "append" {
		armed = true
	}
	err = journal.Append(entry)
	if pending == "append" {
		if !called || !errors.Is(err, context.Canceled) || !errors.Is(err, chain.ErrJournalUncertain) {
			t.Fatal("actual append barrier did not retain uncertainty", called, err)
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if pending == "raw" || pending == "raw-temporary" {
		armed = true
		err = journal.SaveRaw(chain.ExtrinsicHash(nextRaw), nextRaw)
		if !called || !errors.Is(err, context.Canceled) || !errors.Is(err, chain.ErrJournalUncertain) {
			t.Fatal("actual raw barrier did not retain uncertainty", called, err)
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	original := storagePreparationNativeCustody(t, source)
	fence := storagePreparationExportFence(t, source, ref, ownerLocal)
	var output, diagnostic bytes.Buffer
	exportContext := durablepath.WithHost(durablevolume.WithReference(t.Context(), ref), source.storage.Host)
	if code := runMain(exportContext, []string{command, "export", "--root", source.root, "--former-writer-fence", fence.Path, "--former-writer-fence-sha256", fence.Sha256}, &output, &diagnostic); code != 0 {
		t.Fatal("actual original owner cannot export complete source authority", code, diagnostic.String())
	}
	var report durablevolume.Inventory
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(target.metadata, "original-physical-inventory.json")
	if err := os.WriteFile(reportPath, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(filepath.Dir(source.root), "copied-original-archive")
	if err := os.Mkdir(archive, 0700); err != nil {
		t.Fatal(err)
	}
	for _, member := range report.Entries {
		path := filepath.Join(archive, member.Path)
		if member.Path != "" {
			if member.Kind == "directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				data, err := os.ReadFile(filepath.Join(source.root, member.Path))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, attribute := range member.OwnerAttributes {
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
	info, err := os.Stat(archive)
	if err != nil || os.SameFile(info, original.members[0]) {
		t.Fatal("archive did not use a genuinely different root inode", err)
	}
	owner := storagePreparationNativeOwner()
	owner.Purpose = "restore"
	storagePreparationOwnerRequest(t, target, scope, []durablevolume.PreparationOwner{owner})
	requestRaw, err := os.ReadFile(target.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request map[string]any
	if err := json.Unmarshal(requestRaw, &request); err != nil {
		t.Fatal(err)
	}
	request["purpose"] = "restore"
	request["restore_source"] = map[string]any{"directory": archive, "inventory": durablevolume.Reference{Path: reportPath, Sha256: durablefixture.Digest(output.Bytes())}, "former_writer_fence": fence}
	var rootStat unix.Stat_t
	if err := unix.Stat(target.root, &rootStat); err != nil {
		t.Fatal(err)
	}
	targetFence, err := json.Marshal(map[string]any{"schema": durablevolume.PreparationFenceSchema, "root_path": target.root, "root_inode": rootStat.Ino,
		"purpose": "restore", "former_writers_stopped": true, "no_previous_owner_state": false, "no_previous_target_state": true,
		"evidence": "synthetic new target; original stopped source authority is separate"})
	if err != nil {
		t.Fatal(err)
	}
	fencePath := filepath.Join(target.metadata, "restore-target-fence.json")
	if err := os.WriteFile(fencePath, targetFence, 0600); err != nil {
		t.Fatal(err)
	}
	request["former_writer_fence"] = durablevolume.Reference{Path: fencePath, Sha256: durablefixture.Digest(targetFence)}
	requestRaw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target.requestPath, requestRaw, 0600); err != nil {
		t.Fatal(err)
	}
	target.requestHash = durablefixture.Digest(requestRaw)
	target.ctx = durablepath.WithHost(target.ctx, &storagePreparationRestoreHost{Host: target.storage.Host, source: source.storage.Host})
	return &storagePreparationRestoreFixture{source: source, target: target, command: command, archive: archive, reportPath: reportPath, report: report, original: original,
		rawName: strings.TrimPrefix(hash.Hex(), "0x") + ".scale", raw: raw, pendingRaw: nextRaw, ownerLocal: ownerLocal}
}

// The actual apply dispatcher returns an offline declaration, never restart.
func (self *storagePreparationRestoreFixture) apply(t *testing.T, path, hash string) context.Context {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(self.target.ctx, []string{self.command, "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code != 0 {
		t.Fatal("public restore apply failed", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("restore result grants activation or loses its declaration", err)
	}
	return durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), self.target.storage.Host)
}

// Reopening the original native owner proves compatibility at the public
// runtime boundary, including byte-exact nonce and signed-payload retention.
func (self *storagePreparationRestoreFixture) assertRuntime(t *testing.T, ctx context.Context, pending bool) {
	t.Helper()
	open, reconcile := chain.OpenDurableJournal, chain.ReconcileDurableJournal
	if self.ownerLocal {
		open, reconcile = chain.OpenOwnerLocalJournal, chain.ReconcileOwnerLocalJournal
	}
	journal, err := open(ctx, self.target.root)
	if pending {
		if journal != nil || !errors.Is(err, chain.ErrJournalUncertain) {
			t.Fatal("restore silently completed original pending intent", err)
		}
		journal, err = reconcile(ctx, self.target.root)
	}
	if err != nil {
		t.Fatal("restored actual native owner refused exact retained intent", err)
	}
	entries, err := journal.Entries()
	if closeErr := journal.Close(); err != nil || closeErr != nil || len(entries) != 2 {
		t.Fatal("restored runtime lost original entries", err, closeErr, entries)
	}
	for _, entry := range entries {
		if entry.Nonce != 71 || entry.ExtrinsicHash != chain.ExtrinsicHash(self.raw).Hex() {
			t.Fatal("restore changed signed intent coordinates", entry)
		}
	}
	log, err := os.ReadFile(filepath.Join(self.target.root, "native-transactions.jsonl"))
	if err != nil || !bytes.Equal(log, self.original.log) {
		t.Fatal("restore changed original JSONL bytes", err)
	}
	raw, err := os.ReadFile(filepath.Join(self.target.root, "native-transactions", self.rawName))
	if err != nil || !bytes.Equal(raw, self.raw) {
		t.Fatal("restore changed original SCALE bytes", err)
	}
	storagePreparationAssertNativeCustody(t, self.source, self.original)
}

// A physical archive has different inodes from both original and target; its
// original signed bytes still survive the entire reviewed public workflow.
func TestStoragePreparationRestoreNativeCopiedArchiveRetainsOriginalIntent(t *testing.T) {
	for _, ownerLocal := range []bool{false, true} {
		f := newStoragePreparationRestoreFixture(t, ownerLocal, "")
		path, hash := storagePreparationFreezeOwnerPlan(t, f.target, f.command)
		ctx := f.apply(t, path, hash)
		f.assertRuntime(t, ctx, false)
		before := storagePreparationNativeCustody(t, f.target)
		f.apply(t, path, hash)
		storagePreparationAssertNativeCustody(t, f.target, before)
	}
}

// Full pending append bytes remain pending after restore; only the existing
// explicitly joined native reconciliation acknowledges that original intent.
func TestStoragePreparationRestoreNativePendingAppendKeepsReconciliation(t *testing.T) {
	for _, ownerLocal := range []bool{false, true} {
		f := newStoragePreparationRestoreFixture(t, ownerLocal, "append")
		path, hash := storagePreparationFreezeOwnerPlan(t, f.target, f.command)
		f.assertRuntime(t, f.apply(t, path, hash), true)
	}
}

// Interrupted raw checkpoints without original member-predecessor authority
// never become empty/fresh custody or an implicitly completed restore.
func TestStoragePreparationRestoreRefusesAmbiguousOriginalRawIntent(t *testing.T) {
	f := newStoragePreparationRestoreFixture(t, false, "raw")
	var output, diagnostic bytes.Buffer
	code := runMain(f.target.ctx, []string{f.command, "plan", "--request", f.target.requestPath, "--request-sha256", f.target.requestHash}, &output, &diagnostic)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "original predecessor member authority") {
		t.Fatal("ambiguous original raw intent was reset or misclassified", code, diagnostic.String())
	}
	entries, err := os.ReadDir(f.target.root)
	if err != nil || len(entries) != 0 {
		t.Fatal("refused pending restore changed target", err)
	}
	storagePreparationAssertNativeCustody(t, f.source, f.original)
}

// Completed custody loss remains loss on later process invocation, including
// when the complete original backup is still available beside it.
func TestStoragePreparationRestoreCompletedLossNeverRecreates(t *testing.T) {
	f := newStoragePreparationRestoreFixture(t, false, "")
	path, hash := storagePreparationFreezeOwnerPlan(t, f.target, f.command)
	f.apply(t, path, hash)
	logPath := filepath.Join(f.target.root, "native-transactions.jsonl")
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	if code := runMain(f.target.ctx, []string{f.command, "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic); code == 0 || output.Len() != 0 {
		t.Fatal("completed restored log was silently recreated", code)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatal("missing completed log was reconstructed", err)
	}
	storagePreparationAssertNativeCustody(t, f.source, f.original)
}

// Approved staging is retained source intent; losing its input archive later
// cannot redirect copying to a replacement archive or regenerate signed bytes.
func TestStoragePreparationRestoreAcceptedStageRetainsOriginalSource(t *testing.T) {
	f := newStoragePreparationRestoreFixture(t, false, "")
	path, hash := storagePreparationFreezeOwnerPlan(t, f.target, f.command)
	if err := os.Rename(f.archive, f.archive+".held"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.archive, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.archive, "unreviewed"), []byte("must never be read into restore"), 0600); err != nil {
		t.Fatal(err)
	}
	f.assertRuntime(t, f.apply(t, path, hash), false)
}

// The public command can lose its completion acknowledgement after installing
// the original pending native head. Its next invocation keeps that head intact.
func TestStoragePreparationRestoreResumesOriginalPendingTarget(t *testing.T) {
	for _, ownerLocal := range []bool{false, true} {
		func() {
			f := newStoragePreparationRestoreFixture(t, ownerLocal, "append")
			path, hash := storagePreparationFreezeOwnerPlan(t, f.target, f.command)
			ctx, cancel := context.WithCancel(f.target.ctx)
			defer cancel()
			called := false
			host := &storagePreparationObservedHost{Host: &storagePreparationRestoreHost{Host: f.target.storage.Host, source: f.source.storage.Host}, observe: func(*os.File) {
				if called {
					return
				}
				if _, err := unix.Getxattr(f.target.root, chain.NativeJournalCustodyAttribute, make([]byte, 4096)); err != nil {
					return
				}
				raw, err := os.ReadFile(filepath.Join(f.target.metadata, "preparation.jsonl"))
				if err != nil {
					return
				}
				lines := bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n"))
				var last struct {
					Phase string `json:"phase"`
					Step  struct {
						Path      string `json:"path"`
						Attribute string `json:"attribute"`
					} `json:"step"`
				}
				if json.Unmarshal(lines[len(lines)-1], &last) != nil || last.Phase != "pending" || last.Step.Path != f.target.root || last.Step.Attribute != chain.NativeJournalCustodyAttribute {
					return
				}
				called = true
				cancel()
			}}
			var output, diagnostic bytes.Buffer
			args := []string{f.command, "apply", "--plan", path, "--plan-sha256", hash}
			code := runMain(durablepath.WithHost(ctx, host), args, &output, &diagnostic)
			if !called || code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrPreparationUncertain.Error()) {
				t.Fatal("public restored-head acknowledgement did not retain uncertainty", called, code, diagnostic.String())
			}
			before := storagePreparationNativeCustody(t, f.target)
			if _, err := os.Stat(filepath.Join(f.target.metadata, "durable-volumes.json")); !os.IsNotExist(err) {
				t.Fatal("uncertain restored custody gained runtime admission", err)
			}
			runtimeContext := f.apply(t, path, hash)
			after := storagePreparationNativeCustody(t, f.target)
			if !bytes.Equal(before.anchor, after.anchor) || !bytes.Equal(before.log, after.log) || !bytes.HasPrefix(after.control, before.control) {
				t.Fatal("restore continuation changed original pending authority or progress")
			}
			for index := range before.members {
				if !os.SameFile(before.members[index], after.members[index]) {
					t.Fatal("restore continuation replaced original target member", index)
				}
			}
			f.assertRuntime(t, runtimeContext, true)
		}()
	}
}

// Public negative admission proves that the copied archive, not only its
// metadata report, is authenticated before any new target owner is published.
func TestStoragePreparationRestoreRefusesChangedArchivedBytes(t *testing.T) {
	f := newStoragePreparationRestoreFixture(t, false, "")
	altered := append([]byte(nil), f.raw...)
	altered[0] ^= 1
	if err := os.WriteFile(filepath.Join(f.archive, "native-transactions", f.rawName), altered, 0600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	code := runMain(f.target.ctx, []string{f.command, "plan", "--request", f.target.requestPath, "--request-sha256", f.target.requestHash}, &output, &diagnostic)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "differs from original exported bytes") {
		t.Fatal("changed copied SCALE payload was admitted or refused at an unrelated boundary", code, diagnostic.String())
	}
	names, err := os.ReadDir(f.target.root)
	if err != nil || len(names) != 0 {
		t.Fatal("source corruption created target custody", err)
	}
	if _, err := os.Stat(filepath.Join(f.target.metadata, "preparation.jsonl")); !os.IsNotExist(err) {
		t.Fatal("source corruption created target progress", err)
	}
	storagePreparationAssertNativeCustody(t, f.source, f.original)
}
