package crv4

// Seed custody is separate from replaceable state snapshots. A bounded private
// seed is either loaded through checked descriptors or fully synced in a fresh
// temporary file and published with no-replace semantics. All methods own their
// descriptors; independent callers may race without changing each other's key.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maximumSeedFileBytes = 4096
const maximumSeedPathComponents = 256

// Only initial leaf absence permits creation; disappearance after observing
// an occupied file is an acquisition failure, never a regeneration request.
var errSeedFileMissing = errors.New("seed leaf is absent at initial observation")

// Private deterministic seams observe acquisition and durability boundaries.
// No seed bytes are passed to hooks or errors, and no state lock is held.
type seedFileHooks struct {
	step   func(operation, name string) error
	random io.Reader
	write  func(*os.File, []byte) (int, error)
}

// Each path component is anchored to its actual opened directory, not merely
// to a prior pathname check. Retaining ancestry detects replacement on recheck.
type seedFileDirectory struct {
	roots []*os.Root
	files []*os.File
	infos []os.FileInfo
	names []string
	hooks seedFileHooks
}

// Runs one optional boundary observer without exposing key material.
func (self *seedFileDirectory) step(operation, name string) error {
	if self.hooks.step != nil {
		return self.hooks.step(operation, name)
	}
	return nil
}

// Every failure closes already acquired descriptors, including partial opens.
func (self *seedFileDirectory) close() error {
	var err error
	for index := len(self.files) - 1; index >= 0; index-- {
		err = errors.Join(err, self.files[index].Close())
	}
	for index := len(self.roots) - 1; index >= 0; index-- {
		err = errors.Join(err, self.roots[index].Close())
	}
	return err
}

// Relative entries must still name the opened directory, without a symlink or
// changed mode. Directory timestamps are deliberately not content ownership.
func (self *seedFileDirectory) check() error {
	for index, file := range self.files {
		info, err := file.Stat()
		if err != nil || !info.IsDir() || !os.SameFile(info, self.infos[index]) || info.Mode() != self.infos[index].Mode() {
			return errors.Join(errors.New("seed directory descriptor changed"), err)
		}
		if index != 0 {
			entry, err := self.roots[index-1].Lstat(self.names[index-1])
			if err != nil || !entry.IsDir() || !os.SameFile(entry, info) || entry.Mode() != info.Mode() {
				return errors.Join(errors.New("seed directory namespace changed"), err)
			}
		}
	}
	return nil
}

// Traversal rejects every symlink component. Only missing directories are
// created, with 0700; existing ancestry is never chmodded or removed.
func openSeedFileDirectory(path string, create bool, hooks seedFileHooks) (directory *seedFileDirectory, resultErr error) {
	if err := seedFilesSupported(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil || path == "" || filepath.Base(abs) == "." || filepath.Dir(abs) == abs {
		return nil, errors.Join(errors.New("seed path is incomplete"), err)
	}
	parent := filepath.Dir(abs)
	componentTs := strings.Split(strings.TrimPrefix(parent, string(filepath.Separator)), string(filepath.Separator))
	if parent == string(filepath.Separator) {
		componentTs = nil
	}
	if len(componentTs) > maximumSeedPathComponents {
		return nil, errors.New("seed path exceeds its directory-component bound")
	}
	self := &seedFileDirectory{hooks: hooks}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	root, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, err
	}
	self.roots = append(self.roots, root)
	file, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	self.files = append(self.files, file)
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	self.infos = append(self.infos, info)
	for index, component := range componentTs {
		if err := self.check(); err != nil {
			return nil, err
		}
		parentRoot, parentFile := self.roots[len(self.roots)-1], self.files[len(self.files)-1]
		info, err := parentRoot.Lstat(component)
		created := false
		if errors.Is(err, os.ErrNotExist) && create {
			if err := parentRoot.Mkdir(component, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return nil, err
			} else if err == nil {
				created = true
			}
			info, err = parentRoot.Lstat(component)
		}
		if err != nil || !info.IsDir() {
			return nil, errors.Join(errors.New("seed parent component is not a real directory"), err)
		}
		if created && (!seedFileOwnedParent(info) || info.Mode().Perm() != 0o700) {
			return nil, errors.New("new seed parent is not private and owned")
		}
		if index == len(componentTs)-1 {
			if !seedFileOwnedParent(info) {
				return nil, errors.New("seed parent must be owned and not writable by group or others")
			}
			if err := self.step("parent-observed", component); err != nil {
				return nil, err
			}
		}
		child, err := parentRoot.OpenRoot(component)
		if err != nil {
			return nil, err
		}
		self.roots = append(self.roots, child)
		opened, err := child.Open(".")
		if err != nil {
			return nil, err
		}
		self.files = append(self.files, opened)
		actual, err := opened.Stat()
		if err != nil || !actual.IsDir() || !os.SameFile(info, actual) || info.Mode() != actual.Mode() {
			return nil, errors.Join(errors.New("seed parent changed during descriptor acquisition"), err)
		}
		self.infos = append(self.infos, actual)
		self.names = append(self.names, component)
		if err := self.check(); err != nil {
			return nil, err
		}
		if index == len(componentTs)-1 {
			if err := self.step("parent-opened", component); err != nil {
				return nil, err
			}
			if err := self.check(); err != nil {
				return nil, err
			}
		}
		if created {
			if err := self.step("directory-sync", component); err != nil {
				return nil, err
			}
			if err := self.check(); err != nil {
				return nil, err
			}
			if err := errors.Join(opened.Sync(), parentFile.Sync()); err != nil {
				return nil, err
			}
		}
	}
	if !seedFileOwnedParent(self.infos[len(self.infos)-1]) {
		return nil, errors.New("seed parent must be owned and not writable by group or others")
	}
	return self, nil
}

// Reads at most one bounded seed from a checked no-follow descriptor. Syncing
// a concurrent winner's file and parent establishes durability for this caller.
func (self *seedFileDirectory) read(name string, rawOnly bool) (seed [32]byte, observed os.FileInfo, resultErr error) {
	if err := self.check(); err != nil {
		return seed, nil, err
	}
	root := self.roots[len(self.roots)-1]
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return seed, nil, errors.Join(errSeedFileMissing, err)
	}
	if err != nil {
		return seed, nil, err
	}
	if !seedFilePrivate(info) || info.Size() > maximumSeedFileBytes {
		return seed, nil, errors.New("seed must be a bounded private owned single-link regular file")
	}
	if err := self.step("leaf-observed", name); err != nil {
		return seed, nil, err
	}
	if err := self.check(); err != nil {
		return seed, nil, err
	}
	file, err := root.OpenFile(name, os.O_RDONLY|seedFileOpenFlags(), 0)
	if err != nil {
		return seed, nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		if resultErr != nil {
			seed, observed = [32]byte{}, nil
		}
	}()
	opened, err := file.Stat()
	if err != nil || !seedFilePrivate(opened) || !seedFileSameWriteState(info, opened) {
		return seed, nil, errors.Join(errors.New("seed changed during descriptor acquisition"), err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumSeedFileBytes+1))
	if err != nil {
		return seed, nil, err
	}
	if err := self.step("seed-read", name); err != nil {
		return seed, nil, err
	}
	if rawOnly {
		if len(raw) != len(seed) {
			return seed, nil, errors.New("seed must contain exactly 32 raw bytes")
		}
		copy(seed[:], raw)
	} else if len(raw) > maximumSeedFileBytes {
		return seed, nil, errors.New("seed exceeds its file-byte bound")
	} else if seed, err = parseSeedFile(raw); err != nil {
		return seed, nil, err
	}
	if err := self.step("load-sync", name); err != nil {
		return seed, nil, err
	}
	if err := self.check(); err != nil {
		return seed, nil, err
	}
	if err := errors.Join(file.Sync(), self.files[len(self.files)-1].Sync()); err != nil {
		return seed, nil, err
	}
	after, err := file.Stat()
	entry, entryErr := root.Lstat(name)
	if err != nil || entryErr != nil || !seedFilePrivate(after) || !seedFilePrivate(entry) || !seedFileSameWriteState(opened, after) || !seedFileSameWriteState(after, entry) || int64(len(raw)) != after.Size() {
		return seed, nil, errors.Join(errors.New("seed changed while reading or syncing"), err, entryErr)
	}
	if err := self.check(); err != nil {
		return seed, nil, err
	}
	return seed, after, nil
}

// A failed operation never returns a usable seed. An already-published file
// is retained on durability failure; a later verified load may acknowledge it.
func loadSeedFileOwned(path string, create, rawOnly bool, hooks seedFileHooks) (seed [32]byte, created bool, resultErr error) {
	directory, err := openSeedFileDirectory(path, create, hooks)
	if err != nil {
		return seed, false, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, directory.close())
		if resultErr != nil {
			seed, created = [32]byte{}, false
		}
	}()
	name := filepath.Base(path)
	seed, _, err = directory.read(name, rawOnly)
	if err == nil || !errors.Is(err, errSeedFileMissing) || !create {
		return seed, false, err
	}
	if err := directory.step("seed-missing", name); err != nil {
		return seed, false, err
	}
	if err := directory.check(); err != nil {
		return seed, false, err
	}
	reader := hooks.random
	if reader == nil {
		reader = rand.Reader
	}
	if _, err := io.ReadFull(reader, seed[:]); err != nil {
		return seed, false, err
	}
	raw := seed[:]
	if !rawOnly {
		raw = []byte("0x" + hex.EncodeToString(seed[:]) + "\n")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return seed, false, err
	}
	temporaryName := ".seed-pending-" + hex.EncodeToString(nonce[:])
	root := directory.roots[len(directory.roots)-1]
	file, err := root.OpenFile(temporaryName, os.O_RDWR|os.O_CREATE|os.O_EXCL|seedFileOpenFlags(), 0o600)
	if err != nil {
		return seed, false, err
	}
	info, statErr := file.Stat()
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		entry, err := root.Lstat(temporaryName)
		if err == nil {
			if info != nil && seedFilePrivate(entry) && os.SameFile(info, entry) {
				resultErr = errors.Join(resultErr, root.Remove(temporaryName))
			} else {
				resultErr = errors.Join(resultErr, errors.New("seed temporary entry changed; preserved for inspection"))
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if statErr != nil || !seedFilePrivate(info) {
		return seed, false, errors.Join(errors.New("seed temporary file is not private and owned"), statErr)
	}
	write := hooks.write
	if write == nil {
		write = func(file *os.File, data []byte) (int, error) { return file.Write(data) }
	}
	if err := directory.check(); err != nil {
		return seed, false, err
	}
	n, err := write(file, raw)
	if err != nil || n != len(raw) {
		return seed, false, errors.Join(io.ErrShortWrite, err)
	}
	if err := directory.step("seed-sync", name); err != nil {
		return seed, false, err
	}
	if err := directory.check(); err != nil {
		return seed, false, err
	}
	if err := file.Sync(); err != nil {
		return seed, false, err
	}
	synced, err := file.Stat()
	if err != nil || !seedFilePrivate(synced) || !os.SameFile(info, synced) || synced.Size() != int64(len(raw)) {
		return seed, false, errors.Join(errors.New("seed temporary file changed before publication"), err)
	}
	if err := directory.step("publish", name); err != nil {
		return seed, false, err
	}
	if err := directory.check(); err != nil {
		return seed, false, err
	}
	entry, err := root.Lstat(temporaryName)
	if err != nil || !seedFilePrivate(entry) || !seedFileSameWriteState(synced, entry) {
		return seed, false, errors.Join(errors.New("seed temporary namespace changed before publication"), err)
	}
	if err := seedFilePublish(directory.files[len(directory.files)-1], temporaryName, name); errors.Is(err, os.ErrExist) {
		winner, _, readErr := directory.read(name, rawOnly)
		return winner, false, readErr
	} else if err != nil {
		return seed, false, err
	}
	if err := directory.step("parent-sync", name); err != nil {
		return seed, false, err
	}
	if err := directory.check(); err != nil {
		return seed, false, err
	}
	if err := directory.files[len(directory.files)-1].Sync(); err != nil {
		return seed, false, err
	}
	loaded, published, err := directory.read(name, rawOnly)
	if err != nil || published == nil || !os.SameFile(info, published) || loaded != seed {
		return seed, false, errors.Join(errors.New("published seed identity changed before acknowledgement"), err)
	}
	return seed, true, nil
}
