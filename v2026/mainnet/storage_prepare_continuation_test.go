//go:build linux || darwin

// The public dispatcher resumes exact plan-owned ledger bytes after an actual
// interrupted apply; synthetic Host facts cannot approve changed descriptors.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Observations can cancel a caller but cannot replace descriptor or hash checks.
type storagePreparationObservedHost struct {
	durablevolume.Host
	observe func(*os.File)
}

// This callback follows the underlying facts-only host observation synchronously.
func (self *storagePreparationObservedHost) Filesystem(file *os.File) (durablevolume.Filesystem, error) {
	result, err := self.Host.Filesystem(file)
	if err == nil && self.observe != nil {
		self.observe(file)
	}
	return result, err
}

// Actual dispatcher cancellation follows a complete pending file, before its ack.
func TestStoragePreparationCommandResumesOriginalPendingLedger(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	path, hash := f.plan(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plan durablevolume.PreparationPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	var selected durablevolume.PreparationSource
	for _, source := range plan.Sources {
		if source.File.Kind == "file" {
			selected = source
			break
		}
	}
	target := filepath.Join(f.root, selected.File.Path)
	control := filepath.Join(f.metadata, "preparation.jsonl")
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	called := false
	host := &storagePreparationObservedHost{Host: f.storage.Host, observe: func(*os.File) {
		if called {
			return
		}
		data, err := os.ReadFile(target)
		if err != nil || uint64(len(data)) != selected.File.Bytes {
			return
		}
		journal, err := os.ReadFile(control)
		if err != nil {
			return
		}
		lines := bytes.Split(bytes.TrimSuffix(journal, []byte("\n")), []byte("\n"))
		var last struct {
			Phase string `json:"phase"`
			Step  struct {
				Path string `json:"path"`
			} `json:"step"`
		}
		if json.Unmarshal(lines[len(lines)-1], &last) != nil || last.Phase != "pending" || last.Step.Path != target {
			return
		}
		called = true
		cancel()
	}}
	var output, diagnostic bytes.Buffer
	code := runMain(durablepath.WithHost(ctx, host), []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic)
	if !called || code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrPreparationUncertain.Error()) {
		t.Fatal("public continuation lost pending publication uncertainty", called, code, diagnostic.String())
	}
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(f.metadata, "durable-volumes.json")); !os.IsNotExist(err) {
		t.Fatal("interrupted apply published completion", err)
	}
	output.Reset()
	diagnostic.Reset()
	code = runMain(f.ctx, []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}, &output, &diagnostic)
	if code != 0 {
		t.Fatal("exact original plan did not resume", code, diagnostic.String())
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := os.Stat(target)
	if err != nil || !os.SameFile(original, retained) || !bytes.Equal(before, after) {
		t.Fatal("joined resume replaced original pending ledger bytes", err)
	}
}

// Losing completed runtime state never changes the accepted fresh plan's meaning.
func TestStoragePreparationCommandCannotRecreateCompletedLedger(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	path, hash := f.plan(t)
	var output, diagnostic bytes.Buffer
	args := []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}
	if code := runMain(f.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal(code, diagnostic.String())
	}
	before, err := os.ReadFile(filepath.Join(f.metadata, "preparation.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var plan durablevolume.PreparationPlan
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	var member string
	for _, source := range plan.Sources {
		if source.File.Kind == "file" {
			member = filepath.Join(f.root, source.File.Path)
			break
		}
	}
	if err := os.Rename(member, member+".retained"); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	diagnostic.Reset()
	if code := runMain(f.ctx, args, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), durablevolume.ErrIdentity.Error()) {
		t.Fatal("public apply reenrolled lost completed ledger", code, diagnostic.String())
	}
	if _, err := os.Lstat(member); !os.IsNotExist(err) {
		t.Fatal("completed ledger member was reconstructed", err)
	}
	after, err := os.ReadFile(filepath.Join(f.metadata, "preparation.jsonl"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("lost completed journal was reset", err)
	}
}

// Refusals must reach their intended public schema/scope boundary before effects.
func TestStoragePreparationCommandRefusesPrivateKeysAndWrongScope(t *testing.T) {
	for _, mode := range []string{"private-key", "owner-local", "unknown-kind"} {
		func() {
			f := newStoragePreparationCommandFixture(t)
			raw, err := os.ReadFile(f.requestPath)
			if err != nil {
				t.Fatal(err)
			}
			var request map[string]any
			if err := json.Unmarshal(raw, &request); err != nil {
				t.Fatal(err)
			}
			owner := request["owners"].([]any)[0].(map[string]any)
			switch mode {
			case "private-key":
				owner["inputs"].(map[string]any)["private_key"] = "forbidden"
			case "owner-local":
				request["scope"] = "owner-local"
			case "unknown-kind":
				owner["kind"] = "arbitrary-enrollment"
			}
			raw, err = json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.requestPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			var output, diagnostic bytes.Buffer
			code := runMain(f.ctx, []string{"storage-prepare", "plan", "--request", f.requestPath, "--request-sha256", durablefixture.Digest(raw)}, &output, &diagnostic)
			if code == 0 || output.Len() != 0 {
				t.Fatal("private key or wrong fixed scope was admitted", mode, code)
			}
			want := "not in the implemented fixed registry"
			if mode == "private-key" {
				want = "unknown field"
			}
			if mode == "owner-local" {
				want = "explicitly selected scope"
			}
			if !strings.Contains(diagnostic.String(), want) {
				t.Fatal("owner refusal occurred before its intended boundary", mode, diagnostic.String())
			}
			entries, err := os.ReadDir(f.root)
			if err != nil || len(entries) != 0 {
				t.Fatal("refused owner input changed target", err)
			}
		}()
	}
}

// The daemon ledger's actual constructor cannot consume owner-local authority.
func TestStoragePreparationCommandCannotEnrollDaemonLedgerAsOwnerLocal(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	raw, err := os.ReadFile(f.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	request.Scope = "owner-local"
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	code := runMain(f.ctx, []string{"storage-owner-prepare", "plan", "--request", f.requestPath, "--request-sha256", durablefixture.Digest(raw)}, &output, &diagnostic)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "validator ledger requires daemon scope") {
		t.Fatal("owner-local command produced unusable daemon ledger authority", code, diagnostic.String())
	}
	for _, path := range []string{f.root, request.StagingDirectory} {
		entries, err := os.ReadDir(path)
		if err != nil || len(entries) != 0 {
			t.Fatal("unsupported scope created target or staging state", path, err)
		}
	}
}

// A short output is a lost report, not permission to restart the completed plan.
type storagePreparationShortOutput struct{}

// Deliberately returns a short write without concealing it behind a test error.
func (storagePreparationShortOutput) Write(raw []byte) (int, error) { return len(raw) / 2, nil }

// Repeating the exact public apply after lost stdout preserves all completed bytes.
func TestStoragePreparationCommandRecoversLostCompletionReport(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	path, hash := f.plan(t)
	args := []string{"storage-prepare", "apply", "--plan", path, "--plan-sha256", hash}
	var diagnostic bytes.Buffer
	if code := runMain(f.ctx, args, storagePreparationShortOutput{}, &diagnostic); code == 0 || !strings.Contains(diagnostic.String(), "report was not fully delivered") {
		t.Fatal("public command hid a short completion report", code, diagnostic.String())
	}
	control := filepath.Join(f.metadata, "preparation.jsonl")
	before, err := os.ReadFile(control)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(control)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	diagnostic.Reset()
	if code := runMain(f.ctx, args, &output, &diagnostic); code != 0 {
		t.Fatal("retained completion could not be reported", code, diagnostic.String())
	}
	after, err := os.ReadFile(control)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := os.Stat(control)
	if err != nil || !os.SameFile(original, retained) || !bytes.Equal(before, after) {
		t.Fatal("report retry changed completed custody", err)
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized || result.Plan.Path != path || result.Plan.Sha256 != hash {
		t.Fatal("repeat report changed authority", err)
	}
}
