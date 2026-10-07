// Package durablepath joins an explicit durable-volume declaration to one
// application-owned directory. It never enrolls roots or carries protocol
// authority, and it owns no process-global registry or filesystem fallback.
package durablepath

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Directory retains both the checked volume and the descriptor used by the
// application. The caller joins its own users before closing this owner.
type Directory struct {
	volume   *durablevolume.Owner
	file     *os.File
	relative string
}

type hostKey struct{}

// WithHost supplies instance-owned kernel observations to deterministic owner
// fixtures. CLI inputs never select a Host; all descriptor, marker and hash
// validation still executes in durablevolume.OpenWithHost.
func WithHost(ctx context.Context, host durablevolume.Host) context.Context {
	return context.WithValue(ctx, hostKey{}, host)
}

// Require refuses a missing operational declaration without changing any
// signed configuration, retained journal, or existing filesystem entry.
func Require(ctx context.Context) error {
	if ctx == nil {
		return errors.New("durable storage context is absent")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	reference, present := durablevolume.ReferenceFromContext(ctx)
	if !present || reference.Path == "" || reference.Sha256 == "" {
		return errors.New("explicit durable-volume declaration and hash are required")
	}
	return nil
}

// OpenVolume admits one exact declared daemon state root for a bounded root
// operation such as snapshot inventory. It never enrolls or infers a root;
// daemon schema and exact root matching remain in the shared volume admission.
func OpenVolume(ctx context.Context, root string, access durablevolume.Access) (*durablevolume.Owner, error) {
	return openVolume(ctx, root, access, false)
}

// Owner-device inspection is separately selected by its caller. A declaration
// filename or filesystem location never changes the strict daemon entry point.
func OpenOwnerLocalVolume(ctx context.Context, root string, access durablevolume.Access) (*durablevolume.Owner, error) {
	return openVolume(ctx, root, access, true)
}

// Scope selection is immutable for this opening; admission and close still
// retain the caller's context and the same actual descriptor checks.
func openVolume(ctx context.Context, root string, access durablevolume.Access, ownerLocal bool) (*durablevolume.Owner, error) {
	if err := Require(ctx); err != nil {
		return nil, err
	}
	reference, _ := durablevolume.ReferenceFromContext(ctx)
	open := durablevolume.Open
	openWithHost := durablevolume.OpenWithHost
	if ownerLocal {
		open = durablevolume.OpenOwnerLocal
		openWithHost = durablevolume.OpenOwnerLocalWithHost
	}
	var owner *durablevolume.Owner
	var err error
	if host, present := ctx.Value(hostKey{}).(durablevolume.Host); present {
		owner, err = openWithHost(reference, root, access, host)
	} else {
		owner, err = open(reference, root, access)
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, owner.Close())
	}
	return owner, nil
}

// Open selects only a root explicitly listed in the authenticated declaration.
// It never derives a root from the filesystem or creates an absent root.
// Descendant creation, when requested, stays relative to the pinned root.
func Open(ctx context.Context, path string, access durablevolume.Access, create bool) (_ *Directory, resultErr error) {
	return open(ctx, path, access, create, false)
}

// OpenOwnerLocal requires the independent owner-device policy. No daemon
// caller gains this scope by selecting a differently named declaration file.
func OpenOwnerLocal(ctx context.Context, path string, access durablevolume.Access, create bool) (_ *Directory, resultErr error) {
	return open(ctx, path, access, create, true)
}

// The selected scope changes only policy admission; owned I/O is identical.
func open(ctx context.Context, path string, access durablevolume.Access, create, ownerLocal bool) (_ *Directory, resultErr error) {
	if err := Require(ctx); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("durable directory path is not canonical absolute")
	}
	reference, _ := durablevolume.ReferenceFromContext(ctx)
	load := durablevolume.Load
	openVolume := durablevolume.Open
	openVolumeWithHost := durablevolume.OpenWithHost
	if ownerLocal {
		load = durablevolume.LoadOwnerLocal
		openVolume = durablevolume.OpenOwnerLocal
		openVolumeWithHost = durablevolume.OpenOwnerLocalWithHost
	}
	config, err := load(reference)
	if err != nil {
		return nil, err
	}
	root, relative := "", ""
	for _, specification := range config.Volumes {
		for _, candidate := range specification.StateRoots {
			part, err := filepath.Rel(candidate.Path, path)
			if err != nil || part == ".." || strings.HasPrefix(part, ".."+string(filepath.Separator)) || filepath.IsAbs(part) {
				continue
			}
			if root != "" {
				return nil, errors.New("durable directory matches more than one declared root")
			}
			root, relative = candidate.Path, part
		}
	}
	if root == "" {
		return nil, errors.New("durable directory is outside the declared roots")
	}
	if relative == "." {
		relative = ""
	}
	var volume *durablevolume.Owner
	if host, present := ctx.Value(hostKey{}).(durablevolume.Host); present {
		volume, err = openVolumeWithHost(reference, root, access, host)
	} else {
		volume, err = openVolume(reference, root, access)
	}
	if err != nil {
		return nil, err
	}
	self := &Directory{volume: volume, relative: relative}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.Close())
		}
	}()
	self.file, err = volume.OpenDirectory(relative, create)
	if err != nil {
		return nil, err
	}
	if err := errors.Join(ctx.Err(), self.Check()); err != nil {
		return nil, err
	}
	return self, nil
}

// File is borrowed until Close. Applications perform actual I/O relative to
// this descriptor; a pathname-only preflight is not a publication fence.
func (self *Directory) File() *os.File {
	if self == nil {
		return nil
	}
	return self.file
}

// Check authenticates the current path, descriptor and filesystem generation.
func (self *Directory) Check() error {
	if self == nil || self.volume == nil || self.file == nil {
		return errors.New("durable directory owner is closed")
	}
	return self.volume.CheckDirectory(self.relative, self.file)
}

// Read admission retains identity and protection without requiring write reserve.
func (self *Directory) CheckRead() error {
	if self == nil || self.volume == nil || self.file == nil {
		return errors.New("durable directory owner is closed")
	}
	return self.volume.CheckReadDirectory(self.relative, self.file)
}

// CheckWrite also retains the configured writable byte/inode reserve. It is
// required before publication and before any irreversible external handoff.
func (self *Directory) CheckWrite() error {
	if err := self.Check(); err != nil {
		return err
	}
	return self.volume.CheckWrite()
}

// Close releases only this owner. It never removes or recreates custody.
func (self *Directory) Close() error {
	if self == nil {
		return nil
	}
	var fileErr, volumeErr error
	if self.file != nil {
		fileErr = self.file.Close()
		self.file = nil
	}
	if self.volume != nil {
		volumeErr = self.volume.Close()
		self.volume = nil
	}
	return errors.Join(fileErr, volumeErr)
}
