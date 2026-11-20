//go:build linux || darwin

// Test-only consumers provision explicit synthetic declarations around their
// owned fixture roots. Host injection changes kernel facts, never real I/O.
package durablefixture

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"

	"github.com/urnetwork/connect/v2026/durablesys"

	"golang.org/x/sys/unix"
)

// Each fixture owns immutable declarations and its own mutable kernel facts.
type Fixture struct {
	Context   context.Context
	Reference durablevolume.Reference
	Host      *Host
	Roots     []string
}

// Tests select finite fact changes with no global environment override.
type Host struct {
	stateLock  sync.Mutex
	mounts     []durablevolume.Mount
	device     durablevolume.Device
	filesystem durablevolume.Filesystem
}

// Returns a copied census, safe against a concurrent causal transition.
func (self *Host) Mounts() ([]durablevolume.Mount, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]durablevolume.Mount(nil), self.mounts...), nil
}

// Re-enumeration is modeled at the uuid/device boundary only.
func (self *Host) DeviceUuid(string) (durablevolume.Device, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.device, nil
}

// All actual reads, writes, syncs, permissions and leases remain on real files.
func (self *Host) Filesystem(*os.File) (durablevolume.Filesystem, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.filesystem, nil
}

// A capacity transition does not modify retained filesystem identity.
func (self *Host) SetReserve(bytes, inodes uint64) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.filesystem.AvailableBytes, self.filesystem.AvailableInodes = bytes, inodes
}

// Read-only observations refuse writers while permitting retained inspection.
func (self *Host) SetReadOnly(value bool) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.filesystem.ReadOnly = value
	self.mounts[1].ReadOnly = value
}

// An observed mount generation replacement invalidates the old owner.
func (self *Host) ReplaceMount() {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.mounts[1].Id++
}

// The declaration and marker use canonical digest bytes.
func Digest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// Caller roots must already exist and belong to the test. Metadata is created
// outside them; New never repairs missing roots or hides their permissions.
func New(t testing.TB, ctx context.Context, roots ...string) *Fixture {
	return newFixture(t, ctx, durablevolume.Schema, roots...)
}

// Signing fixtures explicitly select the separately validated local schema.
func NewOwnerLocal(t testing.TB, ctx context.Context, roots ...string) *Fixture {
	return newFixture(t, ctx, durablevolume.OwnerLocalSchema, roots...)
}

// Both scopes retain the same real descriptor and precreated-root requirements.
func newFixture(t testing.TB, ctx context.Context, schema string, roots ...string) *Fixture {
	t.Helper()
	if ctx == nil || len(roots) == 0 {
		t.Fatal("synthetic storage context and roots are required")
	}
	var selected []string
	for _, root := range roots {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root {
			t.Fatal("fixture root is not canonical")
		}
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() {
			t.Fatalf("fixture root: %v", err)
		}
		duplicate := false
		for _, prior := range roots {
			if prior != root && strings.HasPrefix(root, prior+string(filepath.Separator)) {
				duplicate = true
			}
		}
		for _, prior := range selected {
			if prior == root {
				duplicate = true
			}
		}
		if !duplicate {
			selected = append(selected, root)
		}
	}
	if len(selected) == 0 {
		t.Fatal("no distinct fixture roots")
	}
	mount := filepath.Dir(selected[0])
	for _, root := range selected[1:] {
		for !strings.HasPrefix(root, mount+string(filepath.Separator)) {
			mount = filepath.Dir(mount)
			if mount == "/" {
				t.Fatal("fixture roots need one private synthetic mount ancestor")
			}
		}
	}
	metadata, err := os.MkdirTemp(mount, ".synthetic-durable-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(metadata); err != nil {
			t.Error(err)
		}
	})
	marker := filepath.Join(metadata, "identity")
	markerBytes := []byte("synthetic-fixture-volume-generation\n")
	if err := os.WriteFile(marker, markerBytes, 0600); err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Stat(mount, &stat); err != nil {
		t.Fatal(err)
	}
	device := durablevolume.Device{Major: unix.Major(rawDevice(&stat)), Minor: unix.Minor(rawDevice(&stat))}
	host := &Host{device: device, filesystem: durablevolume.Filesystem{Id: [2]int32{37, 41}, Type: filesystemMagic, AvailableBytes: 1024 * 1024 * 1024, AvailableInodes: 1024 * 1024}, mounts: []durablevolume.Mount{
		{Id: 1, ParentId: 1, Root: "/", Path: "/", Device: durablevolume.Device{Major: device.Major ^ 1, Minor: device.Minor}, FilesystemType: filesystemType},
		{Id: 7, ParentId: 1, Root: "/", Path: mount, Device: device, FilesystemType: filesystemType},
	}}
	spec := durablevolume.VolumeSpec{MountPath: mount, FilesystemUuid: "1234-abcd", FilesystemType: filesystemType, MarkerPath: marker, MarkerSha256: Digest(markerBytes), MinAvailableBytes: 1024, MinAvailableInodes: 8}
	for index, root := range selected {
		lease := filepath.Join(metadata, fmt.Sprintf("root-%d-lease", index))
		raw := []byte("synthetic-root-lease:" + root + "\n")
		if err := os.WriteFile(lease, raw, 0600); err != nil {
			t.Fatal(err)
		}
		// Fixture-only enrollment never overwrites a prior root generation.
		nonce := make([]byte, durablevolume.RootGenerationBytes)
		count, err := unix.Getxattr(root, durablevolume.RootGenerationAttribute, nonce)
		if errors.Is(err, durablesys.ErrNoAttribute) {
			if _, err := rand.Read(nonce); err != nil {
				t.Fatal(err)
			}
			if err := unix.Setxattr(root, durablevolume.RootGenerationAttribute, nonce, unix.XATTR_CREATE); err != nil {
				t.Fatal(err)
			}
		} else if err != nil || count != len(nonce) {
			t.Fatalf("synthetic root generation: size=%d err=%v", count, err)
		}
		var rootStat unix.Stat_t
		if err := unix.Stat(root, &rootStat); err != nil {
			t.Fatal(err)
		}
		spec.StateRoots = append(spec.StateRoots, durablevolume.StateRootSpec{Path: root, LeasePath: lease, LeaseSha256: Digest(raw), RootInode: rootStat.Ino, GenerationSha256: Digest(nonce)})
	}
	raw, err := json.Marshal(durablevolume.Config{Schema: schema, Volumes: []durablevolume.VolumeSpec{spec}})
	if err != nil {
		t.Fatal(err)
	}
	reference := durablevolume.Reference{Path: filepath.Join(metadata, "volumes.json"), Sha256: Digest(raw)}
	if err := os.WriteFile(reference.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return &Fixture{Context: durablepath.WithHost(durablevolume.WithReference(ctx, reference), host), Reference: reference, Host: host, Roots: selected}
}
