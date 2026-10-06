//go:build linux

// Fixed owner continuation retains actual private inodes and checkpoint bytes
// across public-command interruption; no test helper enrolls target custody.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// The synthetic owner selects an existing fixed runtime profile only.
func storagePreparationSnapshotOwner(t *testing.T, kind, name string, maximum int) durablevolume.PreparationOwner {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"schema": "urnetwork-snapshot-preparation-v1", "name": name, "maximum_bytes": maximum})
	if err != nil {
		t.Fatal(err)
	}
	return durablevolume.PreparationOwner{Kind: kind, RelativePath: ".", Purpose: "fresh", Inputs: raw}
}

// New request inputs cannot smuggle private keys, unsupported capacities,
// traversal or the other command's authority into a fixed owner profile.
func TestStoragePreparationFixedOwnersRefuseUnreviewedInputs(t *testing.T) {
	cases := []string{"native-journal-bytes", "native-raw-bytes", "native-raw-members", "native-record-bytes", "native-private-key", "snapshot-capacity", "snapshot-private-key", "snapshot-traversal", "snapshot-reserved", "snapshot-bootstrap-name", "daemon-signing", "local-monitor", "retained", "unknown-kind"}
	for _, mode := range cases {
		func() {
			f := newStoragePreparationCommandFixture(t)
			owner := storagePreparationNativeOwner()
			if !strings.HasPrefix(mode, "native-") {
				owner = storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "monitor.json", maxRpcReplyBytes)
			}
			var inputs map[string]any
			if err := json.Unmarshal(owner.Inputs, &inputs); err != nil {
				t.Fatal(err)
			}
			scope, command, want := "daemon", "storage-prepare", "fixed runtime profile"
			switch mode {
			case "native-journal-bytes":
				inputs["maximum_journal_bytes"] = 16*1024*1024 + 1
			case "native-raw-bytes":
				inputs["maximum_raw_bytes"] = 64*1024*1024 + 1
			case "native-raw-members":
				inputs["maximum_raw_members"] = 10001
			case "native-record-bytes":
				inputs["maximum_raw_record_bytes"] = 1024*1024 + 1
			case "native-private-key", "snapshot-private-key":
				inputs["private_key"], want = "forbidden", "unknown field"
			case "snapshot-capacity":
				inputs["maximum_bytes"] = maxRpcReplyBytes + 1
			case "snapshot-traversal":
				inputs["name"] = "../outside.json"
			case "snapshot-reserved":
				inputs["name"] = ".durable-head-pending"
			case "snapshot-bootstrap-name":
				owner.Kind, inputs["maximum_bytes"] = "mainnet-bootstrap-root", 16*1024
			case "daemon-signing":
				owner.Kind, inputs["maximum_bytes"] = "mainnet-owner-signing", ownerSigningReplyLimit
			case "local-monitor":
				scope, command = "owner-local", "storage-owner-prepare"
			case "retained":
				owner.Purpose, want = "retained", "fixed fresh kind"
			case "unknown-kind":
				owner.Kind, want = "unreviewed-snapshot-kind", "not in the implemented fixed registry"
			}
			raw, err := json.Marshal(inputs)
			if err != nil {
				t.Fatal(err)
			}
			owner.Inputs = raw
			storagePreparationOwnerRequest(t, f, scope, []durablevolume.PreparationOwner{owner})
			var output, diagnostic bytes.Buffer
			code := runMain(f.ctx, []string{command, "plan", "--request", f.requestPath, "--request-sha256", f.requestHash}, &output, &diagnostic)
			if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), want) {
				t.Fatal("fixed owner did not reach the intended refusal", mode, code, diagnostic.String())
			}
			entries, err := os.ReadDir(f.root)
			if err != nil || len(entries) != 0 {
				t.Fatal("refused owner inputs changed target namespace", mode, err)
			}
			if _, err := os.Lstat(filepath.Join(f.metadata, "preparation.jsonl")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("refused owner created apply custody", mode, err)
			}
		}()
	}
}

// Freeze a public plan before an interruption-specific apply. The helper does
// not write target state or provision a test-only owner checkpoint.
func storagePreparationFreezeOwnerPlan(t *testing.T, f *storagePreparationCommandFixture, command string) (string, string) {
	t.Helper()
	var output, diagnostic bytes.Buffer
	if code := runMain(f.ctx, []string{command, "plan", "--request", f.requestPath, "--request-sha256", f.requestHash}, &output, &diagnostic); code != 0 {
		t.Fatal("fixed owner public planning failed", code, diagnostic.String())
	}
	path := filepath.Join(f.metadata, "continuation-plan.json")
	if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path, durablefixture.Digest(output.Bytes())
}

// Original attribute bytes are read in a fixed buffer; no path write or xattr
// enrollment occurs in this assertion helper.
func storagePreparationOwnerAttribute(t *testing.T, path, attribute string) []byte {
	t.Helper()
	raw := make([]byte, 4096)
	n, err := unix.Getxattr(path, attribute, raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw[:n]
}

// An attribute may be fully installed while its completion acknowledgement is
// lost. Joined exact-plan readback preserves its inode and checkpoint bytes.
func TestStoragePreparationFixedOwnersResumePendingAttribute(t *testing.T) {
	for _, mode := range []string{"native", "owner-signing"} {
		func() {
			f := newStoragePreparationCommandFixture(t)
			owner, scope, command := storagePreparationNativeOwner(), "daemon", "storage-prepare"
			target, attribute := f.root, chain.NativeJournalCustodyAttribute
			if mode == "owner-signing" {
				owner = storagePreparationSnapshotOwner(t, "mainnet-owner-signing", "signing.json", ownerSigningReplyLimit)
				scope, command = "owner-local", "storage-owner-prepare"
				target, attribute = filepath.Join(f.root, "signing.json.lock"), durablehead.Attribute(owner.Kind, "signing.json")
			}
			storagePreparationOwnerRequest(t, f, scope, []durablevolume.PreparationOwner{owner})
			path, hash := storagePreparationFreezeOwnerPlan(t, f, command)
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			called := false
			host := &storagePreparationObservedHost{Host: f.storage.Host, observe: func(*os.File) {
				if called {
					return
				}
				if _, err := unix.Getxattr(target, attribute, make([]byte, 4096)); err != nil {
					return
				}
				journal, err := os.ReadFile(filepath.Join(f.metadata, "preparation.jsonl"))
				if err != nil {
					return
				}
				lines := bytes.Split(bytes.TrimSuffix(journal, []byte("\n")), []byte("\n"))
				var last struct {
					Phase string `json:"phase"`
					Step  struct {
						Path      string `json:"path"`
						Attribute string `json:"attribute"`
					} `json:"step"`
				}
				if json.Unmarshal(lines[len(lines)-1], &last) != nil || last.Phase != "pending" || last.Step.Path != target || last.Step.Attribute != attribute {
					return
				}
				called = true
				cancel()
			}}
			args := []string{command, "apply", "--plan", path, "--plan-sha256", hash}
			var output, diagnostic bytes.Buffer
			code := runMain(durablepath.WithHost(ctx, host), args, &output, &diagnostic)
			if !called || code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrPreparationUncertain.Error()) {
				t.Fatal("public pending attribute lost its uncertainty class", mode, called, code, diagnostic.String())
			}
			before := storagePreparationOwnerAttribute(t, target, attribute)
			original, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(filepath.Join(f.metadata, "durable-volumes.json")); !os.IsNotExist(err) {
				t.Fatal("pending attribute gained a completed declaration", mode, err)
			}
			output.Reset()
			diagnostic.Reset()
			if code := runMain(f.ctx, args, &output, &diagnostic); code != 0 {
				t.Fatal("joined original attribute could not resume", mode, code, diagnostic.String())
			}
			after := storagePreparationOwnerAttribute(t, target, attribute)
			retained, err := os.Stat(target)
			if err != nil || !os.SameFile(original, retained) || !bytes.Equal(before, after) {
				t.Fatal("resume changed the original checkpoint generation", mode, err)
			}
		}()
	}
}

// After acknowledgement, even empty original members cannot be replaced or
// reconstructed by repeating the same accepted fresh plan.
func TestStoragePreparationFixedOwnersRefuseLostCompletedMembers(t *testing.T) {
	for _, mode := range []string{"native-log", "native-raw", "snapshot-marker", "snapshot-head"} {
		func() {
			f := newStoragePreparationCommandFixture(t)
			owner := storagePreparationNativeOwner()
			if strings.HasPrefix(mode, "snapshot-") {
				owner = storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "monitor.json", maxRpcReplyBytes)
			}
			storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{owner})
			path, hash := storagePreparationFreezeOwnerPlan(t, f, "storage-prepare")
			args := []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}
			var output, diagnostic bytes.Buffer
			if code := runMain(f.ctx, args, &output, &diagnostic); code != 0 {
				t.Fatal(code, diagnostic.String())
			}
			control := filepath.Join(f.metadata, "preparation.jsonl")
			before, err := os.ReadFile(control)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(f.root, "native-transactions.jsonl")
			if mode == "native-raw" {
				target = filepath.Join(f.root, "native-transactions")
			}
			if strings.HasPrefix(mode, "snapshot-") {
				target = filepath.Join(f.root, "monitor.json.lock")
			}
			if mode == "snapshot-head" {
				if err := unix.Removexattr(target, durablehead.Attribute(owner.Kind, "monitor.json")); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Rename(target, filepath.Join(f.metadata, "retained-original")); err != nil {
				t.Fatal(err)
			}
			output.Reset()
			diagnostic.Reset()
			if code := runMain(f.ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrIdentity.Error()) {
				t.Fatal("fresh plan reenrolled missing completed custody", mode, code, diagnostic.String())
			}
			after, err := os.ReadFile(control)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("completed preparation was reset", mode, err)
			}
			if mode != "snapshot-head" {
				if _, err := os.Lstat(target); !os.IsNotExist(err) {
					t.Fatal("missing completed member was reconstructed", mode, err)
				}
			}
		}()
	}
}
