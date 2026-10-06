//go:build linux

// Directory-head preparation uses actual public plan/apply boundaries. Unknown
// scope, capacity, pending bytes and missing completed heads never become fresh.
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

	"github.com/urfoundation/sn/v2026/internal/durablehead"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// All independently fixed dimensions and the command scope are part of review.
func TestStoragePreparationDirectoryOwnersRejectWrongProfiles(t *testing.T) {
	cases := []struct {
		kind, field string
		value       any
	}{
		{"mainnet-successor-local-members", "name", "other.json"},
		{"mainnet-successor-local-members", "maximum_bytes", 8388609},
		{"mainnet-successor-local-members", "maximum_members", 4097},
		{"mainnet-successor-local-members", "maximum_member_bytes", 524289},
		{"mainnet-successor-local-members", "maximum_total_bytes", 268435457},
		{"mainnet-successor-local-members", "maximum_namespace_entries", 4104},
		{"mainnet-successor-local-members", "maximum_name_bytes", 256},
		{"mainnet-successor-nonce-members", "scope", "owner-local"},
		{"fleet-recovery", "scope", "daemon"},
		{"fleet-recovery", "maximum_records", 257},
		{"fleet-recovery", "maximum_raw_record_bytes", 65537},
		{"fleet-recovery", "maximum_manifest_bytes", 262145},
		{"fleet-recovery", "name", "../journal.json"},
		{"provider-claim-queue", "maximum_bytes", 16777217},
		{"provider-claim-queue", "scope", "owner-local"},
		{"provider-claim-queue", "private_key", "forbidden"},
	}
	for _, test := range cases {
		f := newStoragePreparationCommandFixture(t)
		owner := storagePreparationDirectoryOwner(t, test.kind)
		scope, command := "daemon", "storage-prepare"
		if test.kind == "fleet-recovery" {
			scope, command = "owner-local", "storage-owner-prepare"
		}
		var input map[string]any
		if err := json.Unmarshal(owner.Inputs, &input); err != nil {
			t.Fatal(err)
		}
		if test.field == "scope" {
			scope = test.value.(string)
			command = "storage-prepare"
			if scope == "owner-local" {
				command = "storage-owner-prepare"
			}
		} else {
			input[test.field] = test.value
		}
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		owner.Inputs = raw
		storagePreparationOwnerRequest(t, f, scope, []durablevolume.PreparationOwner{owner})
		var output, diagnostic bytes.Buffer
		code := runMain(f.ctx, []string{command, "plan", "--request", f.requestPath, "--request-sha256", f.requestHash}, &output, &diagnostic)
		expected := "fixed runtime profile"
		if test.field == "private_key" {
			expected = "unknown field"
		}
		if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), expected) {
			t.Fatal("directory profile missed exact refusal", test.kind, test.field, code, diagnostic.String())
		}
		names, err := os.ReadDir(f.root)
		if err != nil || len(names) != 0 {
			t.Fatal("invalid profile changed target", test, err)
		}
		if _, err := os.Lstat(filepath.Join(f.metadata, "preparation.jsonl")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid profile created apply control", test, err)
		}
	}
}

// Whole-namespace owners cannot claim a shared root. The local successor head
// may coexist with an unrelated bootstrap role's separately locked snapshot.
func TestStoragePreparationDirectoryOwnersRetainNamespaceBoundaries(t *testing.T) {
	for _, kind := range []string{"fleet-recovery", "provider-claim-queue", "mainnet-successor-nonce-members", "mainnet-successor-local-members"} {
		f := newStoragePreparationCommandFixture(t)
		owner := storagePreparationDirectoryOwner(t, kind)
		scope, command := "daemon", "storage-prepare"
		other := storagePreparationSnapshotOwner(t, "mainnet-monitor-checkpoint", "monitor.json", maxRpcReplyBytes)
		if kind == "fleet-recovery" {
			scope, command = "owner-local", "storage-owner-prepare"
			other = storagePreparationSnapshotOwner(t, "mainnet-owner-signing", "signing.json", ownerSigningReplyLimit)
		}
		storagePreparationOwnerRequest(t, f, scope, []durablevolume.PreparationOwner{owner, other})
		if kind != "mainnet-successor-local-members" {
			var output, diagnostic bytes.Buffer
			code := runMain(f.ctx, []string{command, "plan", "--request", f.requestPath, "--request-sha256", f.requestHash}, &output, &diagnostic)
			if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "exclusive root namespace") {
				t.Fatal("exclusive directory owner admitted conflicting peer", kind, code, diagnostic.String())
			}
			names, err := os.ReadDir(f.root)
			if err != nil || len(names) != 0 {
				t.Fatal("conflicting owners changed target", err)
			}
			continue
		}
		ctx := storagePreparationApplyOwnerCommand(t, f, command)
		guard, err := openMainnetDurableDirectory(ctx, f.root, durablevolume.ReadWrite)
		if err != nil {
			t.Fatal(err)
		}
		file := guard.directory.File()
		if err := mainnetDurableFlock(int(file.Fd()), unix.LOCK_EX); err != nil {
			guard.close()
			t.Fatal(err)
		}
		members, err := openBootstrapSuccessorMembers(guard, file, false, false)
		if err != nil {
			guard.close()
			t.Fatal("local census falsely owns unrelated role marker", err)
		}
		if err := errors.Join(members.close(), guard.close()); err != nil {
			t.Fatal(err)
		}
		passive, err := openBootstrapUnclaimedSnapshot(ctx, filepath.Join(f.root, "monitor.json"), other.Kind, maxRpcReplyBytes)
		if err != nil {
			t.Fatal("prepared peer lost independent monitor custody", err)
		}
		if err := passive.close(); err != nil {
			t.Fatal(err)
		}
	}
}

// A real post-xattr observation interrupts before completion acknowledgement;
// the next command can only reconcile the exact retained checkpoint bytes.
func TestStoragePreparationDirectoryOwnersResumeOriginalPendingHeads(t *testing.T) {
	for _, kind := range []string{"fleet-recovery", "provider-claim-queue", "mainnet-successor-local-members", "mainnet-successor-nonce-members"} {
		f := newStoragePreparationCommandFixture(t)
		owner := storagePreparationDirectoryOwner(t, kind)
		scope, command := "daemon", "storage-prepare"
		if kind == "fleet-recovery" {
			scope, command = "owner-local", "storage-owner-prepare"
		}
		var inputs struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(owner.Inputs, &inputs); err != nil {
			t.Fatal(err)
		}
		attribute := durablehead.Attribute(kind, inputs.Name)
		storagePreparationOwnerRequest(t, f, scope, []durablevolume.PreparationOwner{owner})
		path, hash := storagePreparationFreezeOwnerPlan(t, f, command)
		ctx, cancel := context.WithCancel(f.ctx)
		called := false
		host := &storagePreparationObservedHost{Host: f.storage.Host, observe: func(*os.File) {
			if called {
				return
			}
			if _, err := unix.Getxattr(f.root, attribute, make([]byte, 4096)); err != nil {
				return
			}
			raw, err := os.ReadFile(filepath.Join(f.metadata, "preparation.jsonl"))
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
			if json.Unmarshal(lines[len(lines)-1], &last) != nil || last.Phase != "pending" || last.Step.Path != f.root || last.Step.Attribute != attribute {
				return
			}
			called = true
			cancel()
		}}
		args := []string{command, "apply", "--plan", path, "--plan-sha256", hash}
		var output, diagnostic bytes.Buffer
		code := runMain(durablepath.WithHost(ctx, host), args, &output, &diagnostic)
		cancel()
		if !called || code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrPreparationUncertain.Error()) {
			t.Fatal("directory checkpoint uncertainty was not retained", kind, called, code, diagnostic.String())
		}
		before := storagePreparationOwnerAttribute(t, f.root, attribute)
		original, err := os.Stat(f.root)
		if err != nil {
			t.Fatal(err)
		}
		output.Reset()
		diagnostic.Reset()
		if code := runMain(f.ctx, args, &output, &diagnostic); code != 0 {
			t.Fatal("joined original directory-head plan could not resume", kind, code, diagnostic.String())
		}
		retained, err := os.Stat(f.root)
		if err != nil || !os.SameFile(original, retained) || !bytes.Equal(before, storagePreparationOwnerAttribute(t, f.root, attribute)) {
			t.Fatal("resume rewrote original directory head", kind, err)
		}
	}
}

// Completed attributes and auxiliary generations are never implicitly enrolled
// a second time by repeating the same accepted fresh plan.
func TestStoragePreparationDirectoryOwnersRefuseLostCompletedHeads(t *testing.T) {
	for _, kind := range []string{"fleet-recovery", "provider-claim-queue", "mainnet-successor-local-members", "mainnet-successor-nonce-members"} {
		f := newStoragePreparationCommandFixture(t)
		owner := storagePreparationDirectoryOwner(t, kind)
		scope, command := "daemon", "storage-prepare"
		if kind == "fleet-recovery" {
			scope, command = "owner-local", "storage-owner-prepare"
		}
		storagePreparationOwnerRequest(t, f, scope, []durablevolume.PreparationOwner{owner})
		storagePreparationApplyOwnerCommand(t, f, command)
		control := filepath.Join(f.metadata, "preparation.jsonl")
		before, err := os.ReadFile(control)
		if err != nil {
			t.Fatal(err)
		}
		var inputs struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(owner.Inputs, &inputs); err != nil {
			t.Fatal(err)
		}
		attribute := durablehead.Attribute(kind, inputs.Name)
		if err := unix.Removexattr(f.root, attribute); err != nil {
			t.Fatal(err)
		}
		plan := filepath.Join(f.metadata, "fixed-owner-plan.json")
		raw, err := os.ReadFile(plan)
		if err != nil {
			t.Fatal(err)
		}
		var output, diagnostic bytes.Buffer
		code := runMain(f.ctx, []string{command, "apply", "--plan", plan, "--plan-sha256", safeReleaseHash(raw)}, &output, &diagnostic)
		if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrIdentity.Error()) {
			t.Fatal("missing directory head gained replacement enrollment", kind, code, diagnostic.String())
		}
		after, err := os.ReadFile(control)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("lost head rewrote accepted progress", kind, err)
		}
		if _, err := unix.Getxattr(f.root, attribute, make([]byte, 4096)); !errors.Is(err, unix.ENODATA) {
			t.Fatal("missing head was recreated", kind, err)
		}
	}
}
