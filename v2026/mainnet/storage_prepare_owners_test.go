//go:build linux || darwin

// The actual offline dispatcher must create only the fixed fresh formats that
// their unchanged runtime owners can admit. No fixture enrolls the target.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/chain"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Replacing the public fresh request is separate from accepted plan mutation.
func storagePreparationOwnerRequest(t *testing.T, f *storagePreparationCommandFixture, scope string, owners []durablevolume.PreparationOwner) {
	t.Helper()
	raw, err := os.ReadFile(f.requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request durablevolume.PreparationRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	request.Scope, request.Owners = scope, owners
	request.Limits.MaxOwnerAttributes, request.Limits.MaxOwnerAttributeBytes = 16, 16*4096
	raw, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.requestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f.requestHash = durablefixture.Digest(raw)
}

// A reviewed request grants only private empty custody, never runtime approval.
func storagePreparationNativeOwner() durablevolume.PreparationOwner {
	return durablevolume.PreparationOwner{Kind: "native-journal", RelativePath: ".", Purpose: "fresh",
		Inputs: json.RawMessage(`{"schema":"urnetwork-native-journal-preparation-v1","maximum_journal_bytes":16777216,"maximum_raw_bytes":67108864,"maximum_raw_members":10000,"maximum_raw_record_bytes":1048576}`)}
}

// The command independently selects scope before inspecting request contents.
func storagePreparationApplyOwnerCommand(t *testing.T, f *storagePreparationCommandFixture, command string) context.Context {
	t.Helper()
	var plan, diagnostic bytes.Buffer
	if code := runMain(f.ctx, []string{command, "plan", "--request", f.requestPath, "--request-sha256", f.requestHash}, &plan, &diagnostic); code != 0 {
		t.Fatalf("fixed owner public plan failed: %d %s", code, diagnostic.String())
	}
	entries, err := os.ReadDir(f.root)
	if err != nil || len(entries) != 0 {
		t.Fatal("fixed owner plan mutated target", err)
	}
	path := filepath.Join(f.metadata, "fixed-owner-plan.json")
	if err := os.WriteFile(path, plan.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	diagnostic.Reset()
	if code := runMain(f.ctx, []string{command, "apply", "--plan", path, "--plan-sha256", durablefixture.Digest(plan.Bytes())}, &output, &diagnostic); code != 0 {
		t.Fatalf("fixed owner accepted plan failed: %d %s", code, diagnostic.String())
	}
	var result durablevolume.PreparationResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.RestartAuthorized {
		t.Fatal("fixed owner preparation granted activation or lost its result", err)
	}
	return durablepath.WithHost(durablevolume.WithReference(t.Context(), result.Declaration), f.storage.Host)
}

// An empty daemon journal must be produced by the public command and accepted
// by the existing mandatory log/raw/anchor constructor without a signing key.
func TestStoragePreparationCommandInitializesNativeJournal(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	storagePreparationOwnerRequest(t, f, "daemon", []durablevolume.PreparationOwner{storagePreparationNativeOwner()})
	ctx := storagePreparationApplyOwnerCommand(t, f, "storage-prepare")
	journal, err := chain.OpenDurableJournal(ctx, f.root)
	if err != nil {
		t.Fatal("public preparation cannot open the actual native journal", err)
	}
	entries, err := journal.Entries()
	if closeErr := journal.Close(); err != nil || closeErr != nil || len(entries) != 0 {
		t.Fatal("fresh native journal gained entries or lost custody", err, closeErr)
	}
}

// Owner-local native custody is explicit; its declaration cannot enter the
// daemon constructor even on the same approved non-root filesystem.
func TestStoragePreparationOwnerCommandInitializesNativeJournal(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	storagePreparationOwnerRequest(t, f, "owner-local", []durablevolume.PreparationOwner{storagePreparationNativeOwner()})
	ctx := storagePreparationApplyOwnerCommand(t, f, "storage-owner-prepare")
	if journal, err := chain.OpenDurableJournal(ctx, f.root); err == nil {
		journal.Close()
		t.Fatal("owner-local declaration entered daemon native constructor")
	}
	journal, err := chain.OpenOwnerLocalJournal(ctx, f.root)
	if err != nil {
		t.Fatal("public owner-local preparation cannot open original native format", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
}

// Every daemon snapshot uses its original capacity and marker grammar. All
// heads share one root but remain on distinct marker inodes.
func TestStoragePreparationCommandInitializesFixedSnapshotHeads(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	type snapshot struct {
		kind, name string
		maximum    int
	}
	snapshots := []snapshot{
		{"mainnet-root-action", "root-action.json", rootActionStoreLimit},
		{"mainnet-root-service", "root-service.json", rootServiceStoreLimit},
		{"mainnet-root-submission", "root-submission.json", rootSubmissionStoreLimit},
		{"mainnet-root-offline", "root-offline.json", rootOfflineStoreLimit},
		{"mainnet-owner-recycle", "owner-recycle.json", ownerRecycleStoreLimit},
		{kind: "mainnet-root-register", name: rootRegisterStateFile, maximum: rootRegisterStoreLimit},
		{"mainnet-owner-trim", "owner-trim.json", ownerTrimStoreLimit},
		{"mainnet-evm-action", "evm-action.json", 512 * 1024},
		{"mainnet-bootstrap-root", bootstrapRootProgressFile, 16 * 1024},
		{"mainnet-bootstrap-chain", "bootstrap-chain.json", rootServiceStoreLimit},
		{"mainnet-validator-activation", "validator-activation.json", 128 * 1024},
		{"mainnet-host-action", "host-action.json", 64 * 1024},
		{"mainnet-monitor-checkpoint", "chain-monitor.json", maxRpcReplyBytes},
	}
	owners := make([]durablevolume.PreparationOwner, 0, len(snapshots))
	for _, spec := range snapshots {
		inputs, err := json.Marshal(map[string]any{"schema": "urnetwork-snapshot-preparation-v1", "name": spec.name, "maximum_bytes": spec.maximum})
		if err != nil {
			t.Fatal(err)
		}
		owners = append(owners, durablevolume.PreparationOwner{Kind: spec.kind, RelativePath: ".", Purpose: "fresh", Inputs: inputs})
	}
	storagePreparationOwnerRequest(t, f, "daemon", owners)
	ctx := storagePreparationApplyOwnerCommand(t, f, "storage-prepare")
	for _, spec := range snapshots {
		owner, err := openBootstrapUnclaimedSnapshot(ctx, filepath.Join(f.root, spec.name), spec.kind, spec.maximum)
		if err != nil {
			t.Fatal("prepared snapshot cannot pass actual passive bootstrap custody", spec.kind, err)
		}
		if err := owner.close(); err != nil {
			t.Fatal(err)
		}
	}
}

// The explicit local signing-owner head opens only its request store; preparing
// it does not invoke a hardware adapter, derive a key, or issue a signature.
func TestStoragePreparationOwnerCommandInitializesSigningCustody(t *testing.T) {
	f := newStoragePreparationCommandFixture(t)
	inputs, err := json.Marshal(map[string]any{"schema": "urnetwork-snapshot-preparation-v1", "name": "signing.json", "maximum_bytes": ownerSigningReplyLimit})
	if err != nil {
		t.Fatal(err)
	}
	storagePreparationOwnerRequest(t, f, "owner-local", []durablevolume.PreparationOwner{{Kind: "mainnet-owner-signing", RelativePath: ".", Purpose: "fresh", Inputs: inputs}})
	ctx := storagePreparationApplyOwnerCommand(t, f, "storage-owner-prepare")
	config := ownerSigningDeviceConfig{StatePath: filepath.Join(f.root, "signing.json"), PythonPath: "/usr/bin/python3", HelperPath: "/public/helper.py", BackendPath: "/public/backend.py",
		HelperHash: "sha256:" + strings.Repeat("21", 32), BackendHash: "sha256:" + strings.Repeat("22", 32), AppVersion: [3]uint16{100, 0, 5}}
	request := ownerSigningRequest{ContentHash: "sha256:" + strings.Repeat("23", 32)}
	request.Config.Action.StatePath = filepath.Join(f.metadata, "separate-action.json")
	owner, err := openOwnerSigningDeviceStore(ctx, config, request)
	if err != nil {
		t.Fatal("public local preparation cannot claim actual request custody", err)
	}
	record, err := owner.load()
	if closeErr := owner.close(); err != nil || closeErr != nil || record.Phase != "reserved" || record.Reply != nil {
		t.Fatal("preparation gained signing authority or lost original reservation", err, closeErr)
	}
}
