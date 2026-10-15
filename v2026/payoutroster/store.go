// Private immutable custody pins reviewed requests before signing and retains
// signed originals before publication. A directory lock spans the whole workflow.
package payoutroster

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/payoutartifact"
	"golang.org/x/sys/unix"
)

// An existing name is never replaced, even after an interrupted publication.
var (
	ErrStoreIntegrity = errors.New("authority custody integrity failure")
	ErrStoreConflict  = errors.New("authority custody already contains different bytes")
)

// One owner holds the process-independent directory lock until Close. Methods
// are sequential; separate processes and instances serialize before signing.
type Store struct {
	ctx       context.Context
	directory *os.File
	path      string
	retained  map[string][32]byte
}

// The selected directory must already exist, be owned and private, and have no
// symlink path components. Lock admission and crash recovery are cancellable.
func OpenStore(ctx context.Context, directory string) (*Store, error) {
	if ctx == nil || directory == "" {
		return nil, ErrStoreIntegrity
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	directoryFile, err := openRosterDirectory(path, true)
	if err != nil {
		return nil, errors.Join(ErrStoreIntegrity, err)
	}
	fd := int(directoryFile.Fd())
	store := &Store{ctx: ctx, directory: directoryFile, path: path, retained: map[string][32]byte{}}
	defer func() {
		if store != nil {
			_ = store.Close()
		}
	}()
	for {
		if err := store.check(ctx); err != nil {
			return nil, err
		}
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err := store.cleanPending(ctx); err != nil {
		return nil, err
	}
	result := store
	store = nil
	return result, nil
}

// Closing is idempotent and releases the cross-process workflow lock.
func (self *Store) Close() error {
	if self == nil || self.directory == nil {
		return nil
	}
	directory := self.directory
	self.directory = nil
	return errors.Join(unix.Flock(int(directory.Fd()), unix.LOCK_UN), directory.Close())
}

// Original signatures, canonical bytes and selection are checked on every load.
// The caller must additionally verify its independently admitted expected signer.
func (self *Store) Load(ctx context.Context, domain [32]byte, epoch uint64) ([]byte, error) {
	raw, err := self.load(ctx, domain, epoch, "authority.json", payoutartifact.MaxWholeWorkAuthorityBytes)
	if err != nil {
		if errors.Is(err, ErrStoreIntegrity) {
			return nil, ErrStoreIntegrity
		}
		return nil, err
	}
	if err := validateStoredAuthority(ctx, domain, epoch, raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// The signed original is committed without replacement and made durable before
// any publication. Identical retries succeed; competing originals fail closed.
func (self *Store) Retain(ctx context.Context, domain [32]byte, epoch uint64, raw []byte) error {
	if err := validateStoredAuthority(ctx, domain, epoch, raw); err != nil {
		return err
	}
	return self.retain(ctx, domain, epoch, "authority.json", raw, payoutartifact.MaxWholeWorkAuthorityBytes)
}

// Reviewed input is opaque to custody; the producer checks its schema and intent.
func (self *Store) LoadRequest(ctx context.Context, domain [32]byte, epoch uint64) ([]byte, error) {
	return self.load(ctx, domain, epoch, "request.json", MaxRequestBytes)
}

// The reviewed request must be retained before the signing key is invoked.
func (self *Store) RetainRequest(ctx context.Context, domain [32]byte, epoch uint64, raw []byte) error {
	return self.retain(ctx, domain, epoch, "request.json", raw, MaxRequestBytes)
}

// A durable acknowledgement applies only to this exact retained authority hash.
func (self *Store) RetainPublished(ctx context.Context, domain [32]byte, epoch uint64, hash [32]byte) error {
	raw, err := self.Load(ctx, domain, epoch)
	if err != nil {
		return err
	}
	if hash == ([32]byte{}) || sha256.Sum256(raw) != hash {
		return ErrStoreConflict
	}
	return self.retain(ctx, domain, epoch, "published", hash[:], sha256.Size)
}

// An absent acknowledgement permits exact-byte re-publication after a restart.
// Recovery can inspect this pin before recreating a missing signed original.
func (self *Store) PublishedHash(ctx context.Context, domain [32]byte, epoch uint64) ([32]byte, bool, error) {
	raw, err := self.load(ctx, domain, epoch, "published", sha256.Size)
	if errors.Is(err, os.ErrNotExist) && !errors.Is(err, ErrStoreIntegrity) {
		return [32]byte{}, false, nil
	}
	if err != nil {
		return [32]byte{}, false, err
	}
	if len(raw) != sha256.Size {
		return [32]byte{}, false, ErrStoreIntegrity
	}
	hash := [32]byte(raw)
	if hash == ([32]byte{}) {
		return [32]byte{}, false, ErrStoreIntegrity
	}
	return hash, true, nil
}

// Acknowledgement requires both the requested hash and its exact signed original.
func (self *Store) IsPublished(ctx context.Context, domain [32]byte, epoch uint64, hash [32]byte) (bool, error) {
	publishedHash, exists, err := self.PublishedHash(ctx, domain, epoch)
	if err != nil || !exists {
		return false, err
	}
	if hash == ([32]byte{}) || publishedHash != hash {
		return false, ErrStoreConflict
	}
	authority, err := self.Load(ctx, domain, epoch)
	if err != nil {
		return false, errors.Join(ErrStoreIntegrity, err)
	}
	if sha256.Sum256(authority) != hash {
		return false, ErrStoreConflict
	}
	return true, nil
}

// The pathname must still identify the private directory whose lock is held.
func (self *Store) check(ctx context.Context) error {
	if ctx == nil || self == nil || self.ctx == nil || self.directory == nil {
		return errors.Join(ErrStoreIntegrity, os.ErrClosed)
	}
	if err := errors.Join(ctx.Err(), self.ctx.Err()); err != nil {
		return err
	}
	opened, err := self.directory.Stat()
	if err != nil {
		return err
	}
	named, err := os.Lstat(self.path)
	if err != nil {
		return errors.Join(ErrStoreIntegrity, err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(self.directory.Fd()), &stat); err != nil {
		return err
	}
	if !opened.IsDir() || stat.Mode&07777 != 0700 || stat.Uid != uint32(os.Geteuid()) || named.Mode()&os.ModeSymlink != 0 || !os.SameFile(opened, named) {
		return ErrStoreIntegrity
	}
	return nil
}

// A domain and epoch have one fixed leaf for each stage; caller input is never
// interpreted as a path or used to select a different custody directory.
func storeName(domain [32]byte, epoch uint64, kind string) (string, error) {
	if domain == ([32]byte{}) {
		return "", ErrStoreIntegrity
	}
	return hex.EncodeToString(domain[:]) + "-" + strconv.FormatUint(epoch, 10) + "." + kind, nil
}

// Reads reject symlinks, devices, unexpected permissions, oversized files and
// replacement during the read. A retained hash also detects in-session tampering.
func (self *Store) load(ctx context.Context, domain [32]byte, epoch uint64, kind string, maximum int) ([]byte, error) {
	if err := self.check(ctx); err != nil {
		return nil, err
	}
	name, err := storeName(domain, epoch, kind)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(self.directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, exists := self.retained[name]; exists {
				return nil, errors.Join(ErrStoreIntegrity, err)
			}
			return nil, os.ErrNotExist
		}
		return nil, errors.Join(ErrStoreIntegrity, err)
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var before unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return nil, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&07777 != 0600 || before.Uid != uint32(os.Geteuid()) || before.Nlink != 1 || before.Size <= 0 || before.Size > int64(maximum) {
		return nil, ErrStoreIntegrity
	}
	beforeInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return nil, err
	}
	var after, named unix.Stat_t
	if err := errors.Join(unix.Fstat(fd, &after), unix.Fstatat(int(self.directory.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW)); err != nil {
		return nil, errors.Join(ErrStoreIntegrity, err)
	}
	afterInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if after.Dev != before.Dev || after.Ino != before.Ino || after.Mode != before.Mode || after.Nlink != before.Nlink || after.Uid != before.Uid || after.Size != before.Size || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) || after.Dev != named.Dev || after.Ino != named.Ino || after.Mode != named.Mode || named.Nlink != 1 || len(raw) != int(before.Size) {
		return nil, ErrStoreIntegrity
	}
	hash := sha256.Sum256(raw)
	if retained, exists := self.retained[name]; exists && retained != hash {
		return nil, ErrStoreIntegrity
	}
	if err := self.check(ctx); err != nil {
		return nil, err
	}
	self.retained[name] = hash
	return raw, nil
}

// A synced temporary inode is linked without clobbering an existing original.
// The temporary name is removed before the directory is synced and acknowledged.
func (self *Store) retain(ctx context.Context, domain [32]byte, epoch uint64, kind string, raw []byte, maximum int) (resultErr error) {
	if len(raw) == 0 || len(raw) > maximum {
		return ErrStoreIntegrity
	}
	prior, err := self.load(ctx, domain, epoch, kind, maximum)
	if err == nil {
		if !bytes.Equal(raw, prior) {
			return ErrStoreConflict
		}
		return self.directory.Sync()
	}
	if !errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrStoreIntegrity) {
		return err
	}
	name, err := storeName(domain, epoch, kind)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".pending-" + hex.EncodeToString(nonce[:])
	directoryFd := int(self.directory.Fd())
	fd, err := unix.Openat(directoryFd, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), temporary)
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		if err := unix.Unlinkat(directoryFd, temporary, 0); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := self.check(ctx); err != nil {
		return err
	}
	if err := unix.Linkat(directoryFd, temporary, directoryFd, name, 0); err != nil {
		if errors.Is(err, os.ErrExist) {
			prior, readErr := self.load(ctx, domain, epoch, kind, maximum)
			if readErr != nil {
				return readErr
			}
			if !bytes.Equal(raw, prior) {
				return ErrStoreConflict
			}
			return self.directory.Sync()
		}
		return err
	}
	if err := unix.Unlinkat(directoryFd, temporary, 0); err != nil {
		return err
	}
	if err := self.directory.Sync(); err != nil {
		return err
	}
	retained, err := self.load(ctx, domain, epoch, kind, maximum)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, retained) {
		return ErrStoreIntegrity
	}
	return nil
}

// A crash between link and unlink can leave a second name for a fully synced
// original. Removing only our exact temporary names restores single-link custody.
func (self *Store) cleanPending(ctx context.Context) error {
	for {
		if err := self.check(ctx); err != nil {
			return err
		}
		entries, err := self.directory.ReadDir(100)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, ".pending-") {
				continue
			}
			nonce, decodeErr := hex.DecodeString(strings.TrimPrefix(name, ".pending-"))
			if decodeErr != nil || len(nonce) != 16 || ".pending-"+hex.EncodeToString(nonce) != name {
				return ErrStoreIntegrity
			}
			var stat unix.Stat_t
			if err := unix.Fstatat(int(self.directory.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return err
			}
			if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&07777 != 0600 || stat.Uid != uint32(os.Geteuid()) {
				return ErrStoreIntegrity
			}
			if err := unix.Unlinkat(int(self.directory.Fd()), name, 0); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return self.directory.Sync()
		}
	}
}

// Embedded identity authenticates corruption only; independent signer admission
// remains the producer's responsibility and is checked again before publication.
func validateStoredAuthority(ctx context.Context, domain [32]byte, epoch uint64, raw []byte) error {
	if ctx == nil {
		return ErrStoreIntegrity
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(raw) == 0 || len(raw) > payoutartifact.MaxWholeWorkAuthorityBytes {
		return ErrStoreIntegrity
	}
	var authority payoutartifact.WholeWorkAuthority
	if err := json.Unmarshal(raw, &authority); err != nil {
		return errors.Join(ErrStoreIntegrity, err)
	}
	authority, err := payoutartifact.DecodeWholeWorkAuthority(ctx, raw, authority.Signer)
	if err != nil {
		return errors.Join(ErrStoreIntegrity, err)
	}
	actual, err := authority.Domain.Digest()
	if err != nil || actual != domain || authority.Epoch != epoch {
		return fmt.Errorf("%w: authority selection differs", ErrStoreIntegrity)
	}
	return nil
}
