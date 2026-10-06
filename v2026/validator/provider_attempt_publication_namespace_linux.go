//go:build linux

// A prepared directory is retained through the caller's real read/write owner.
// The namespace guard creates no directory, file, attribute or restart grant.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"syscall"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// The explicit fresh-empty preparation fence belongs to the offline caller.
// This pure helper supplies its canonical physical attribute without publishing.
func FreshProviderAttemptPublicationNamespaceAttribute(preparation ProviderAttemptPublicationPreparation, directory os.FileInfo) ([]byte, error) {
	digest, err := preparation.Digest()
	if err != nil {
		return nil, err
	}
	if directory == nil {
		return nil, errors.New("provider publication fresh directory observation is absent")
	}
	stat, ok := directory.Sys().(*syscall.Stat_t)
	if !ok || !directory.IsDir() || directory.Mode().Perm() != 0700 || directory.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || stat.Uid != uint32(os.Geteuid()) || stat.Ino == 0 {
		return nil, errors.Join(durablevolume.ErrIdentity, errors.New("provider publication namespace is not private physical custody"))
	}
	return json.Marshal(ProviderAttemptPublicationNamespaceCheckpoint{Schema: ProviderAttemptPublicationNamespaceSchema, ProfileSha256: digest, DirectoryDevice: uint64(stat.Dev), DirectoryInode: uint64(stat.Ino)})
}

// Callers keep this guard until their actual publication owner has joined.
// It owns the retained volume/directory descriptor, never the borrowed file.
type ProviderAttemptPublicationNamespace struct {
	stateLock sync.Mutex
	directory *durablepath.Directory
	profile   ProviderAttemptPublicationPreparation
	raw       []byte
	failure   error
}

// Open only original prepared custody; an absent namespace is never created.
func OpenProviderAttemptPublicationNamespace(ctx context.Context, preparation ProviderAttemptPublicationPreparation) (_ *ProviderAttemptPublicationNamespace, resultErr error) {
	if ctx == nil {
		return nil, errors.New("provider publication namespace has no owner context")
	}
	if err := preparation.Validate(); err != nil {
		return nil, err
	}
	preparation.Operators = append([]ProviderAttemptPublicationOperator(nil), preparation.Operators...)
	directory, err := durablepath.Open(ctx, preparation.Directory(), durablevolume.ReadOnly, false)
	if err != nil {
		return nil, err
	}
	self := &ProviderAttemptPublicationNamespace{directory: directory, profile: preparation}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.Close())
		}
	}()
	info, err := directory.File().Stat()
	if err != nil {
		return nil, err
	}
	self.raw, err = FreshProviderAttemptPublicationNamespaceAttribute(preparation, info)
	if err != nil {
		return nil, err
	}
	if err := self.Check(ctx, directory.File()); err != nil {
		return nil, err
	}
	return self, nil
}

// The actual input owner must borrow this same prepared physical namespace.
// Observation errors are returned before any equality or identity inference.
func (self *ProviderAttemptPublicationNamespace) Check(ctx context.Context, borrowed *os.File) (resultErr error) {
	if self == nil || ctx == nil || borrowed == nil {
		return errors.New("provider publication namespace or borrowed owner is absent")
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.failure != nil {
		return self.failure
	}
	if self.directory == nil {
		return errors.New("provider publication namespace is closed")
	}
	defer func() {
		if errors.Is(resultErr, durablevolume.ErrIdentity) {
			self.failure = resultErr
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := self.directory.CheckRead(); err != nil {
		return err
	}
	info, err := borrowed.Stat()
	if err != nil {
		return errors.Join(&durablevolume.UnavailableError{Reason: "cannot observe provider publication borrowed namespace"}, err)
	}
	expected, err := FreshProviderAttemptPublicationNamespaceAttribute(self.profile, info)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, self.raw) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider publication owner changed its prepared directory"))
	}
	raw := make([]byte, 4097)
	n, err := unix.Fgetxattr(int(borrowed.Fd()), ProviderAttemptPublicationNamespaceAttribute, raw)
	if err != nil {
		if errors.Is(err, unix.ENODATA) {
			return errors.Join(durablevolume.ErrIdentity, errors.New("provider publication original birth is absent"), err)
		}
		return errors.Join(&durablevolume.UnavailableError{Reason: "cannot read provider publication original birth"}, err)
	}
	if n == 0 || n > 4096 || !bytes.Equal(raw[:n], self.raw) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider publication original birth differs"))
	}
	return errors.Join(ctx.Err(), self.directory.CheckRead())
}

// Close releases only retained custody after the caller joins all borrowers.
func (self *ProviderAttemptPublicationNamespace) Close() error {
	if self == nil {
		return nil
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.directory == nil {
		return nil
	}
	directory := self.directory
	self.directory = nil
	return directory.Close()
}
