//go:build linux

// Public recovery retains the reviewed root/control pair. Missing completed
// custody or replaced parents never authorize creating another namespace.
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

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Both command scopes encounter real root move completion before cancellation.
func TestStoragePreparationPrivateRootResumesOriginalMovedInode(t *testing.T) {
	for _, ownerLocal := range []bool{false, true} {
		f := newStoragePreparationCommandFixture(t)
		scope, command := "daemon", "storage-prepare"
		if ownerLocal {
			scope, command = "owner-local", "storage-owner-prepare"
		}
		storagePreparationOwnerRequest(t, f, scope, []durablevolume.PreparationOwner{storagePreparationNativeOwner()})
		storagePreparationPrivateRootRequest(t, f)
		path, hash, original := storagePreparationPrivateRootPlan(t, f, command)
		ctx, cancel := context.WithCancel(f.ctx)
		called := false
		host := &storagePreparationObservedHost{Host: f.storage.Host, observe: func(*os.File) {
			if called {
				return
			}
			target, err := os.Stat(f.root)
			if err != nil || !os.SameFile(original, target) {
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
					Kind string `json:"kind"`
					Path string `json:"path"`
				} `json:"step"`
			}
			if json.Unmarshal(lines[len(lines)-1], &last) != nil || last.Phase != "pending" || last.Step.Kind != "root-directory" || last.Step.Path != f.root {
				return
			}
			called = true
			cancel()
		}}
		var output, diagnostic bytes.Buffer
		code := runMain(durablepath.WithHost(ctx, host), []string{command, "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic)
		cancel()
		if !called || code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrPreparationUncertain.Error()) {
			t.Fatal("moved root cancellation lost its exact pending intent", ownerLocal, called, code, diagnostic.String())
		}
		before, err := os.ReadFile(filepath.Join(f.metadata, "preparation.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		storagePreparationApplyPrivateRoot(t, f, command, path, hash, original)
		after, err := os.ReadFile(filepath.Join(f.metadata, "preparation.jsonl"))
		if err != nil || !bytes.HasPrefix(after, before) {
			t.Fatal("joined command discarded original pending move", err)
		}
	}
}

// These public refusals occur before any target control or runtime member.
func TestStoragePreparationPrivateRootRejectsChangedParentsAndSources(t *testing.T) {
	for _, mode := range []string{"parent", "source", "competing-target"} {
		f := newStoragePreparationCommandFixture(t)
		storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{storagePreparationNativeOwner()})
		storagePreparationPrivateRootRequest(t, f)
		path, hash, _ := storagePreparationPrivateRootPlan(t, f, "storage-prepare")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var plan struct {
			RootSource string `json:"root_source"`
		}
		if err := json.Unmarshal(raw, &plan); err != nil {
			t.Fatal(err)
		}
		switch mode {
		case "parent":
			parent := filepath.Dir(f.root)
			if err := os.Rename(parent, parent+"-retained"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
		case "source":
			if err := os.Rename(plan.RootSource, plan.RootSource+"-retained"); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(plan.RootSource, 0700); err != nil {
				t.Fatal(err)
			}
		case "competing-target":
			if err := os.Mkdir(f.root, 0700); err != nil {
				t.Fatal(err)
			}
		}
		var output, diagnostic bytes.Buffer
		code := runMain(f.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic)
		if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrIdentity.Error()) {
			t.Fatal("public apply admitted changed root custody", mode, code, diagnostic.String())
		}
		if _, err := os.Lstat(filepath.Join(f.metadata, "preparation.jsonl")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("changed root began publication", mode, err)
		}
	}
}

// A lost completed root cannot select staged/fresh fallback. Reinstalling the
// exact original inode is explicit and can only return the prior plan result.
func TestStoragePreparationPrivateRootNeverRecreatesCompletedNamespace(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{storagePreparationNativeOwner()})
	storagePreparationPrivateRootRequest(t, f)
	path, hash, original := storagePreparationPrivateRootPlan(t, f, "storage-prepare")
	storagePreparationApplyPrivateRoot(t, f, "storage-prepare", path, hash, original)
	controlPath := filepath.Join(f.metadata, "preparation.jsonl")
	before, err := os.ReadFile(controlPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f.root, f.root+"-retained"); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	code := runMain(f.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrIdentity.Error()) {
		t.Fatal("lost completed namespace gained fresh creation", code, diagnostic.String())
	}
	if _, err := os.Lstat(f.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused completed root was recreated", err)
	}
	retained, err := os.ReadFile(controlPath)
	if err != nil || !bytes.Equal(before, retained) {
		t.Fatal("missing root rewrote original control", err)
	}
	if err := os.Rename(f.root+"-retained", f.root); err != nil {
		t.Fatal(err)
	}
	storagePreparationApplyPrivateRoot(t, f, "storage-prepare", path, hash, original)
	retained, err = os.ReadFile(controlPath)
	if err != nil || !bytes.Equal(before, retained) {
		t.Fatal("original custody readback rewrote completed journal", err)
	}
}
