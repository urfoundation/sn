//go:build linux || darwin

package main

// A separate root-only fixture executes this same test image as the service
// UID. It uses real kernel mount/UUID facts and never starts a system service.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durableinspect"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// The pinned fixture executable accepts the exact production argv. Its sole
// child path invokes the real read-only command, without a supplied Host seam.
func init() {
	if len(os.Args) > 1 && os.Args[1] == "storage-inspect" {
		os.Exit(durableinspect.Run(context.Background(), os.Args[2:], os.Stdout, os.Stderr))
	}
}

func TestServiceStorageCredentialInspectorRealHost(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("SN_STORAGE_INSPECTOR_REAL_FIXTURE") != "approved-test-only" {
		t.Skip("requires separately selected root-only synthetic fixture on /mnt/data; no live service effects")
	}
	uuid := os.Getenv("SN_STORAGE_INSPECTOR_DATA_UUID")
	if uuid == "" {
		t.Fatal("fixture must pin the observed /mnt/data filesystem uuid")
	}
	base, err := os.MkdirTemp("/mnt/data", "sn-service-inspector-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Error(err)
		}
	})
	if err := os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "service")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(root, 1000, 1000); err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, durablevolume.RootGenerationBytes)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	if err := unix.Setxattr(root, durablevolume.RootGenerationAttribute, nonce, unix.XATTR_CREATE); err != nil {
		t.Fatal(err)
	}
	var state unix.Stat_t
	if err := unix.Stat(root, &state); err != nil {
		t.Fatal(err)
	}
	write := func(name string, raw []byte, mode os.FileMode) planFileReference {
		t.Helper()
		path := filepath.Join(base, name)
		if err := os.WriteFile(path, raw, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return planFileReference{Path: path, Sha256: monitorReadDigest(raw)}
	}
	marker := write("marker", []byte("synthetic inspection volume\n"), 0644)
	lease := write("lease", []byte("synthetic inspection root lease\n"), 0644)
	config := durablevolume.Config{Schema: durablevolume.Schema, Volumes: []durablevolume.VolumeSpec{{MountPath: "/mnt/data", FilesystemUuid: uuid, FilesystemType: "ext4", MarkerPath: marker.Path, MarkerSha256: marker.Sha256,
		MinAvailableBytes: 1024, MinAvailableInodes: 8, StateRoots: []durablevolume.StateRootSpec{{Path: root, RootInode: state.Ino, GenerationSha256: monitorReadDigest(nonce), LeasePath: lease.Path, LeaseSha256: lease.Sha256}}}}}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	policy := write("volumes.json", raw, 0644)
	reference := durablevolume.Reference{Path: policy.Path, Sha256: policy.Sha256}
	// Ordinary root admission still rejects another UID's private role root.
	if owner, err := durablevolume.Open(reference, root, durablevolume.ReadOnly); owner != nil || !errors.Is(err, durablevolume.ErrIdentity) {
		t.Fatal("root inspection waived role ownership", owner, err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(base, "approved-inspector")
	target, err := os.OpenFile(binary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(target, source)
	if err := errors.Join(copyErr, source.Close(), target.Sync(), target.Close(), os.Chmod(binary, 0755)); err != nil {
		t.Fatal(err)
	}
	digest, err := sourceLockFileHash(binary)
	if err != nil {
		t.Fatal(err)
	}
	manager := write("manager", []byte("synthetic manager image; the test owns its transport\n"), 0755)
	unit := repairValidatorUnit{Name: rootPassiveHostUnitName, StateDirectory: root, Binary: planFileReference{Path: binary, Sha256: "sha256:" + strings.TrimPrefix(digest, "0x")}, DurableVolumes: &reference, Uid: 1000, Gid: 1000}
	host := newRepairValidatorHost()
	managerCalls := 0
	host.execute = func(context.Context, string, []string) ([]byte, error) {
		managerCalls++
		return nil, nil
	}
	ctx := durablevolume.WithReference(t.Context(), reference)
	plan := repairValidatorPlan{Unit: unit, Systemctl: manager, CommandTimeoutSeconds: 30}
	if err := host.start(ctx, plan, func() error { return nil }); err != nil || managerCalls != 1 {
		t.Fatal("actual service credential inspection failed", managerCalls, err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatal("read-only inspector created state", entries, err)
	}
	if err := os.Rename(root, root+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(root, 1000, 1000); err != nil {
		t.Fatal(err)
	}
	if err := host.start(ctx, plan, func() error { return nil }); !errors.Is(err, durablevolume.ErrIdentity) || managerCalls != 1 {
		t.Fatal("replaced role root reached the manager", managerCalls, err)
	}
	if err := errors.Join(os.Remove(root), os.Rename(root+"-original", root)); err != nil {
		t.Fatal(err)
	}
	plan.Unit.Binary.Sha256 = monitorReadDigest([]byte("another executable"))
	if err := host.start(ctx, plan, func() error { return nil }); err == nil || managerCalls != 1 {
		t.Fatal("different executable pin reached the manager", managerCalls, err)
	}
}
