// Public inspection commands retain opaque owner metadata and explicit policy
// scope. Only kernel facts are synthetic; file bytes and xattrs are real.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// The exact external former-writer fence is separate from root contents and
// works for either explicitly chosen policy schema without weakening either.
func storageCustodyTestFence(t *testing.T, fixture *durablefixture.Fixture, root string) []string {
	t.Helper()
	raw, err := os.ReadFile(fixture.Reference.Path)
	if err != nil {
		t.Fatal(err)
	}
	var config durablevolume.Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(durablevolume.FormerWriterFence{Schema: durablevolume.FormerWriterFenceSchema, RootPath: root, DeclarationSha256: fixture.Reference.Sha256, LeaseSha256: config.Volumes[0].StateRoots[0].LeaseSha256, FormerWritersStopped: true, Evidence: "synthetic owner was stopped and joined"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(fixture.Reference.Path), "fence-"+strings.TrimPrefix(durablefixture.Digest(raw), "sha256:")+".json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return []string{"--root", root, "--former-writer-fence", path, "--former-writer-fence-sha256", durablefixture.Digest(raw), "--durable-volumes", fixture.Reference.Path, "--durable-volumes-sha256", fixture.Reference.Sha256}
}

// Defaults and explicit bounds reach the real v3 traversal; losing a retained
// owner anchor cannot be hidden by unchanged file contents during verification.
func TestStorageInspectionPublicInventoryPreservesOwnerAttributesAndBounds(t *testing.T) {
	fixture, root, _ := storageInspectionTestFixture(t)
	anchor := []byte(`{"synthetic":"completed-owner-head"}`)
	attribute := "user.urnetwork.native-journal-custody"
	if err := unix.Setxattr(root, attribute, anchor, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	args := storageCustodyTestFence(t, fixture, root)
	var stdout, stderr bytes.Buffer
	if code := runMain(fixture.Context, append([]string{"storage-inventory"}, args...), &stdout, &stderr); code != 0 {
		t.Fatal("public inventory did not admit bounded owner metadata", code, stderr.String())
	}
	var report durablevolume.Inventory
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || report.Schema != durablevolume.InventorySchema || report.TotalOwnerAttributes != 1 || report.TotalOwnerAttributeBytes != uint64(len(anchor)) || report.RestartAuthorized {
		t.Fatal("owner metadata report", report, err)
	}
	if len(report.Entries[0].OwnerAttributes) != 1 || report.Entries[0].OwnerAttributes[0].Name != attribute || !bytes.Equal(report.Entries[0].OwnerAttributes[0].Value, anchor) {
		t.Fatal("original root owner bytes were omitted", report.Entries)
	}
	raw := append([]byte(nil), stdout.Bytes()...)
	inventoryPath := filepath.Join(filepath.Dir(fixture.Reference.Path), "owner-inventory.json")
	if err := os.WriteFile(inventoryPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, bounds := range [][]string{{"--max-owner-attributes", "0"}, {"--max-owner-attribute-bytes", "1"}, {"--max-owner-attributes", "10001"}, {"--max-owner-attribute-bytes", "16777217"}} {
		stdout.Reset()
		stderr.Reset()
		if code := runMain(fixture.Context, append(append([]string{"storage-inventory"}, args...), bounds...), &stdout, &stderr); code == 0 || stdout.Len() != 0 {
			t.Fatal("bounded traversal emitted a report", bounds, code, stdout.String())
		}
	}
	stdout.Reset()
	if code := runMain(fixture.Context, append(append([]string{"storage-inventory"}, args...), "--max-owner-attributes", "1", "--max-owner-attribute-bytes", "4096"), &stdout, &stderr); code != 0 {
		t.Fatal("finite explicit bounds were not usable after refusal", code, stderr.String())
	}
	if err := unix.Removexattr(root, attribute); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	verify := append(append([]string{"storage-verify"}, args...), "--inventory", inventoryPath, "--inventory-sha256", durablefixture.Digest(raw))
	if code := runMain(fixture.Context, verify, &stdout, &stderr); code == 0 || stdout.Len() != 0 {
		t.Fatal("unchanged files concealed lost custody metadata", code, stdout.String())
	}
}

// A signing device must deliberately select owner-local commands and policy;
// neither schema becomes a fallback accepted by the other command family.
func TestStorageInspectionOwnerLocalCommandsRemainSeparate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "owner-custody")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := durablefixture.NewOwnerLocal(t, t.Context(), root)
	args := storageCustodyTestFence(t, fixture, root)
	var stdout, stderr bytes.Buffer
	if code := runMain(fixture.Context, append([]string{"storage-inventory"}, args...), &stdout, &stderr); code == 0 || stdout.Len() != 0 {
		t.Fatal("daemon command accepted owner-local policy", code, stdout.String())
	}
	if code := runMain(fixture.Context, append([]string{"storage-owner-inventory"}, args...), &stdout, &stderr); code != 0 {
		t.Fatal("explicit owner-local inspection is not runnable", code, stderr.String())
	}
	raw := append([]byte(nil), stdout.Bytes()...)
	inventoryPath := filepath.Join(filepath.Dir(fixture.Reference.Path), "owner-local-inventory.json")
	if err := os.WriteFile(inventoryPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	verify := append(append([]string{"storage-owner-verify"}, args...), "--inventory", inventoryPath, "--inventory-sha256", durablefixture.Digest(raw))
	if code := runMain(fixture.Context, verify, &stdout, &stderr); code != 0 {
		t.Fatal("owner-local verification is not runnable", code, stderr.String())
	}
	var report durablevolume.RestoreVerification
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || !report.ExactLocalBytesAndMetadata || report.RestartAuthorized || report.Observed.RestartAuthorized {
		t.Fatal("owner-local report widened authority", report, err)
	}
	for _, ctx := range []context.Context{context.Background(), nil} {
		stdout.Reset()
		if code := runMain(ctx, append([]string{"storage-owner-inventory"}, args[:6]...), &stdout, &stderr); code == 0 || stdout.Len() != 0 {
			t.Fatal("owner-local command lost context authority", code)
		}
	}
	daemon, daemonRoot, _ := storageInspectionTestFixture(t)
	stdout.Reset()
	if code := runMain(daemon.Context, append([]string{"storage-owner-inventory"}, storageCustodyTestFence(t, daemon, daemonRoot)...), &stdout, &stderr); code == 0 || stdout.Len() != 0 {
		t.Fatal("owner-local command accepted daemon policy", code, stdout.String())
	}
}

// A copied root needs an independently supplied target declaration and a
// deliberately selected comparison; neither exact bytes nor copied anchors
// authorize restart or rewrite embedded physical owner bindings.
func TestStorageInspectionReboundComparisonRequiresExplicitOption(t *testing.T) {
	fixture, root, _ := storageInspectionTestFixture(t)
	args := storageCustodyTestFence(t, fixture, root)
	anchor := []byte(`{"synthetic":"retained-original-physical-owner"}`)
	attribute := "user.urnetwork.native-journal-custody"
	if err := unix.Setxattr(root, attribute, anchor, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runMain(fixture.Context, append([]string{"storage-inventory"}, args...), &stdout, &stderr); code != 0 {
		t.Fatal("initial inventory", code, stderr.String())
	}
	expected := append([]byte(nil), stdout.Bytes()...)
	inventoryPath := filepath.Join(filepath.Dir(fixture.Reference.Path), "prior-root-inventory.json")
	if err := os.WriteFile(inventoryPath, expected, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root+"-retained", "completed.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "completed.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{37}, durablevolume.RootGenerationBytes)
	if err := unix.Setxattr(root, durablevolume.RootGenerationAttribute, nonce, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(root, attribute, anchor, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	config, err := durablevolume.Load(fixture.Reference)
	if err != nil {
		t.Fatal(err)
	}
	var stat syscall.Stat_t
	if err := syscall.Stat(root, &stat); err != nil {
		t.Fatal(err)
	}
	config.Volumes[0].StateRoots[0].RootInode = stat.Ino
	config.Volumes[0].StateRoots[0].GenerationSha256 = durablefixture.Digest(nonce)
	raw, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	reference := durablevolume.Reference{Path: filepath.Join(filepath.Dir(fixture.Reference.Path), "reviewed-target.json"), Sha256: durablefixture.Digest(raw)}
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	target := &durablefixture.Fixture{Reference: reference, Host: fixture.Host, Context: durablepath.WithHost(durablevolume.WithReference(t.Context(), reference), fixture.Host)}
	verify := append(append([]string{"storage-verify"}, storageCustodyTestFence(t, target, root)...), "--inventory", inventoryPath, "--inventory-sha256", durablefixture.Digest(expected))
	stdout.Reset()
	if code := runMain(target.Context, verify, &stdout, &stderr); code == 0 || stdout.Len() != 0 {
		t.Fatal("ordinary verification silently rebound custody", code)
	}
	if code := runMain(target.Context, append(verify, "--compare-reviewed-rebound"), &stdout, &stderr); code != 0 {
		t.Fatal("explicit reviewed target comparison failed", code, stderr.String())
	}
	var report durablevolume.RestoreVerification
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || !report.ExactLocalBytesAndMetadata || report.SamePhysicalRoot || report.SameDeclaration || report.SameRootGeneration || report.RestartAuthorized || report.Observed.RestartAuthorized {
		t.Fatal("rebound report widened authority", report, err)
	}
}
