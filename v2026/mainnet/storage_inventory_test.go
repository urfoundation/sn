// Command-level controls retain real bytes, exact declarations, and nonblocking
// leases. Only Linux kernel facts are synthetic; no mount or service is started.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// A short successful write is an output refusal, never complete publication.
type storageInspectionShortWriter struct{ bytes.Buffer }

// The output boundary deterministically drops one byte without returning an error.
func (self *storageInspectionShortWriter) Write(raw []byte) (int, error) {
	return self.Buffer.Write(raw[:len(raw)-1])
}

// A truncated JSON report cannot produce a successful command result.
func TestStorageInspectionRejectsShortOutput(t *testing.T) {
	fixture, _, args := storageInspectionTestFixture(t)
	open := func(reference durablevolume.Reference, root string, access durablevolume.Access) (*durablevolume.Owner, error) {
		return durablevolume.OpenWithHost(reference, root, access, fixture.Host)
	}
	var stdout storageInspectionShortWriter
	var stderr bytes.Buffer
	if code := runStorageInspection(fixture.Context, args, &stdout, &stderr, false, open); code != 1 || stdout.Len() == 0 || !strings.Contains(stderr.String(), io.ErrShortWrite.Error()) {
		t.Fatalf("short output reported success: code=%d bytes=%d stderr=%s", code, stdout.Len(), stderr.String())
	}
}

// This assertion remains external to the root and explicitly covers old writers.
func storageInspectionTestFence(t *testing.T, fixture *durablefixture.Fixture, root string) durablevolume.Reference {
	t.Helper()
	config, err := durablevolume.Load(fixture.Reference)
	if err != nil {
		t.Fatal(err)
	}
	var lease string
	for _, declared := range config.Volumes[0].StateRoots {
		if declared.Path == root {
			lease = declared.LeaseSha256
		}
	}
	raw, err := json.Marshal(durablevolume.FormerWriterFence{Schema: durablevolume.FormerWriterFenceSchema, RootPath: root, DeclarationSha256: fixture.Reference.Sha256, LeaseSha256: lease, FormerWritersStopped: true, Evidence: "synthetic writer was canceled and joined"})
	if err != nil {
		t.Fatal(err)
	}
	reference := durablevolume.Reference{Path: filepath.Join(filepath.Dir(fixture.Reference.Path), "former-writer.json"), Sha256: durablefixture.Digest(raw)}
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return reference
}

// The root must be precreated; fixture provisioning belongs only to tests.
func storageInspectionTestFixture(t *testing.T) (*durablefixture.Fixture, string, []string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "custody")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "completed.json"), []byte("synthetic-completed-custody\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := durablefixture.New(t, t.Context(), root)
	fence := storageInspectionTestFence(t, fixture, root)
	return fixture, root, []string{"--root", root, "--former-writer-fence", fence.Path, "--former-writer-fence-sha256", fence.Sha256}
}

// Direct library dispatch cannot fall back to fresh root-filesystem custody.
func TestStorageInspectionRequiresExplicitDeclaration(t *testing.T) {
	_, _, args := storageInspectionTestFixture(t)
	var stdout, stderr bytes.Buffer
	if code := runStorageInventory(context.Background(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
		t.Fatalf("missing declaration code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

// A matching report keeps restart false and binds the exact retained bytes.
func TestStorageInspectionRetainedLocalVerification(t *testing.T) {
	fixture, _, args := storageInspectionTestFixture(t)
	open := func(reference durablevolume.Reference, root string, access durablevolume.Access) (*durablevolume.Owner, error) {
		return durablevolume.OpenWithHost(reference, root, access, fixture.Host)
	}
	var stdout, stderr bytes.Buffer
	if code := runStorageInspection(fixture.Context, args, &stdout, &stderr, false, open); code != 0 {
		t.Fatalf("inventory=%d: %s", code, stderr.String())
	}
	raw := append([]byte(nil), stdout.Bytes()...)
	path := filepath.Join(filepath.Dir(fixture.Reference.Path), "inventory.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	args = append(args, "--inventory", path, "--inventory-sha256", durablefixture.Digest(raw))
	stdout.Reset()
	if code := runStorageInspection(fixture.Context, args, &stdout, &stderr, true, open); code != 0 {
		t.Fatalf("verify=%d: %s", code, stderr.String())
	}
	var report durablevolume.RestoreVerification
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || !report.ExactLocalBytesAndMetadata || !report.SamePhysicalRoot || report.RestartAuthorized || report.Observed.RestartAuthorized {
		t.Fatalf("verification widened authority: %+v, %v", report, err)
	}
}

// Snapshot admission never waits behind the actual active writer lease.
func TestStorageInspectionBusyRootRetainsCustody(t *testing.T) {
	fixture, root, args := storageInspectionTestFixture(t)
	writer, err := durablevolume.OpenWithHost(fixture.Reference, root, durablevolume.ReadWrite, fixture.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	open := func(reference durablevolume.Reference, root string, access durablevolume.Access) (*durablevolume.Owner, error) {
		return durablevolume.OpenWithHost(reference, root, access, fixture.Host)
	}
	var stdout, stderr bytes.Buffer
	if code := runStorageInspection(fixture.Context, args, &stdout, &stderr, false, open); code != 4 || stdout.Len() != 0 {
		t.Fatalf("busy root code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if err := writer.CheckWrite(); err != nil {
		t.Fatal("snapshot disturbed writer", err)
	}
}

// Traversal failures emit no plausible partial inventory or changed state.
func TestStorageInspectionBoundsAndCancellationEmitNoReport(t *testing.T) {
	fixture, root, args := storageInspectionTestFixture(t)
	open := func(reference durablevolume.Reference, root string, access durablevolume.Access) (*durablevolume.Owner, error) {
		return durablevolume.OpenWithHost(reference, root, access, fixture.Host)
	}
	ctx, cancel := context.WithCancel(fixture.Context)
	cancel()
	for _, item := range []struct {
		ctx  context.Context
		args []string
	}{
		{ctx: fixture.Context, args: append(append([]string(nil), args...), "--max-bytes", "1")},
		{ctx: ctx, args: args},
	} {
		var stdout, stderr bytes.Buffer
		if code := runStorageInspection(item.ctx, item.args, &stdout, &stderr, false, open); code == 0 || stdout.Len() != 0 {
			t.Fatalf("partial report code=%d stdout=%s", code, stdout.String())
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "completed.json"))
	if err != nil || string(raw) != "synthetic-completed-custody\n" {
		t.Fatalf("custody changed: %q %v", raw, err)
	}
}

// The actual public dispatcher extracts immutable policy flags, refuses missing
// or canceled admission, and cannot acknowledge truncated report publication.
func TestStorageInspectionPublicDispatchRequiresAndRetainsAuthority(t *testing.T) {
	fixture, _, inspection := storageInspectionTestFixture(t)
	flags := []string{"--durable-volumes", fixture.Reference.Path, "--durable-volumes-sha256", fixture.Reference.Sha256}
	args := append([]string{"storage-inventory"}, inspection...)
	var stdout, stderr bytes.Buffer
	if code := runMain(context.Background(), args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
		t.Fatal("public dispatcher admitted missing declaration", code, stderr.String())
	}
	canceled, cancel := context.WithCancel(fixture.Context)
	cancel()
	if code := runMain(canceled, append(append([]string(nil), args...), flags...), &stdout, &stderr); code != 2 || stdout.Len() != 0 {
		t.Fatal("canceled dispatcher opened storage", code, stderr.String())
	}
	wrong := append(append([]string(nil), args...), "--durable-volumes", fixture.Reference.Path, "--durable-volumes-sha256", "sha256:"+strings.Repeat("0", 64))
	if code := runMain(fixture.Context, wrong, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
		t.Fatal("caller authority was overridden", code, stderr.String())
	}
	if code := runMain(fixture.Context, append(append([]string(nil), args...), flags...), &stdout, &stderr); code != 0 {
		t.Fatal("public inventory refused exact declaration", code, stderr.String())
	}
	raw := append([]byte(nil), stdout.Bytes()...)
	inventory := filepath.Join(filepath.Dir(fixture.Reference.Path), "public-inventory.json")
	if err := os.WriteFile(inventory, raw, 0600); err != nil {
		t.Fatal(err)
	}
	verify := append([]string{"storage-verify"}, inspection...)
	verify = append(verify, "--inventory", inventory, "--inventory-sha256", durablefixture.Digest(raw))
	verify = append(verify, flags...)
	stdout.Reset()
	if code := runMain(fixture.Context, verify, &stdout, &stderr); code != 0 {
		t.Fatal("public verification failed", code, stderr.String())
	}
	var report durablevolume.RestoreVerification
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || !report.ExactLocalBytesAndMetadata || report.RestartAuthorized || report.Observed.RestartAuthorized {
		t.Fatal("dispatcher widened report authority", report, err)
	}
	var short storageInspectionShortWriter
	if code := runMain(fixture.Context, append(append([]string(nil), args...), flags...), &short, &stderr); code != 1 || short.Len() == 0 {
		t.Fatal("public dispatcher acknowledged short report", code, stderr.String())
	}
}
