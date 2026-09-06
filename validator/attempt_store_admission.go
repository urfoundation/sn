//go:build linux || darwin

package validator

// Inspection owns the actual private directory inode without creating LOCK or
// syncing a directory. Promotion retains that owner; a pathname is never a
// substitute for the descriptors whose namespace was inspected.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Opens an existing parent read-only; only an admitted missing final directory
// may be created. Shared legacy wrappers explicitly promote without a schema
// inspector, while aggregate Open supplies its complete authority inspection.
func openAttemptRecordStoreInspection(ctx context.Context, path string, bounds attemptRecordStoreBounds, hooks attemptRecordStoreHooks, fault func(error)) (result *attemptRecordStoreStorage, resultErr error) {
	if ctx == nil || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return nil, errors.New("attempt record store inspection context or path is invalid")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parentPath, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	parent, err := os.OpenRoot(parentPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, parent.Close())
		if resultErr != nil && result != nil {
			resultErr = errors.Join(resultErr, result.Close())
			result = nil
		}
	}()
	return acquireAttemptRecordStoreStorage(ctx, parent, filepath.Join(parentPath, filepath.Base(path)), nil, bounds, hooks, fault)
}

// Acquires a directory-only gate before any durable operation. The retained
// parent is a separate descriptor, not a second flock on an already-owned parent.
func acquireAttemptRecordStoreStorage(ctx context.Context, parent *os.Root, path string, expected os.FileInfo, bounds attemptRecordStoreBounds, hooks attemptRecordStoreHooks, fault func(error)) (result *attemptRecordStoreStorage, resultErr error) {
	if ctx == nil || parent == nil || expected != nil && !attemptStorePrivateDirectory(expected) || bounds.MaxStorageBytes <= attemptStoreMetadataReserve || bounds.MaxStorageFiles == 0 {
		return nil, errors.New("attempt record store directory authority is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	anchor, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		if expected != nil {
			return nil, errors.New("attempt record store owned directory is missing")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// EEXIST is a refused competing creator, never implicit adoption.
		if err := parent.Mkdir(name, 0o700); err != nil {
			return nil, err
		}
		anchor, err = parent.Lstat(name)
	}
	if err != nil || !attemptStorePrivateDirectory(anchor) || expected != nil && !os.SameFile(expected, anchor) {
		return nil, errors.New("attempt record store owned directory changed before open")
	}
	self := &attemptRecordStoreStorage{path: path, anchor: anchor, bounds: bounds, hooks: hooks, fault: fault, sizes: map[string]uint64{}, used: attemptStoreMetadataReserve, openingCtx: ctx, inspection: true, observed: map[string]os.FileInfo{}}
	complete := false
	defer func() {
		if !complete {
			resultErr = errors.Join(resultErr, self.Close(), ctx.Err())
			result = nil
		}
	}()
	if err := self.step("after-directory-check", ""); err != nil {
		return nil, err
	}
	self.parent, err = parent.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	self.root, err = parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	self.directory, err = self.root.Open(".")
	if err != nil {
		return nil, err
	}
	opened, err := self.directory.Stat()
	if err != nil || !attemptStorePrivateDirectory(opened) || !os.SameFile(anchor, opened) {
		return nil, errors.New("attempt record store directory changed during open")
	}
	if err := attemptStoreLockFile(self.directory); err != nil {
		return nil, err
	}
	if err := self.censusInspection(true); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	complete = true
	return self, nil
}

// The fixed file census retains only native identity/write state. It does not
// inspect atime, read record histories into a map, or repair permissions.
func (self *attemptRecordStoreStorage) censusInspection(capture bool) (resultErr error) {
	if err := self.step("admission-census", ""); err != nil {
		return err
	}
	current, err := self.parent.Lstat(filepath.Base(self.path))
	opened, openErr := self.directory.Stat()
	if err != nil || openErr != nil || !attemptStorePrivateDirectory(current) || !attemptStorePrivateDirectory(opened) || !os.SameFile(self.anchor, current) || !os.SameFile(self.anchor, opened) {
		return errors.New("attempt record store inspection directory changed")
	}
	directory, err := self.root.Open(".")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	var count, metadataBytes uint64
	used := uint64(attemptStoreMetadataReserve)
	for {
		if err := self.step("admission-census-read", ""); err != nil {
			return err
		}
		entries, readErr := directory.ReadDir(128)
		for _, entry := range entries {
			count++
			info, infoErr := entry.Info()
			if infoErr != nil || !attemptStorePrivateFile(info) || info.Size() < 0 || count > self.bounds.MaxStorageFiles {
				return errors.New("attempt record store contains nonregular or excessive files")
			}
			if !capture && !attemptLedgerSameFileState(self.observed[entry.Name()], info) {
				return errors.New("attempt record store inspected file changed before promotion")
			}
			size := uint64(info.Size())
			if attemptStoreMetadataName(entry.Name()) {
				if size > attemptStoreMetadataReserve-metadataBytes {
					return errors.New("attempt record store metadata files exceed their reservation")
				}
				metadataBytes += size
			} else {
				if size > self.bounds.MaxStorageBytes-used {
					return errAttemptRecordStoreLimit
				}
				used += size
			}
			if capture {
				self.sizes[entry.Name()] = size
				self.observed[entry.Name()] = info
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if !capture && count != uint64(len(self.observed)) {
		return errors.New("attempt record store inspection census changed")
	}
	if capture {
		self.used = used
	}
	return self.openingContextError()
}

// No database worker exists until this transition. Every existing descriptor
// must still match the inspected census before the first Sync or LOCK creation.
func (self *attemptRecordStoreStorage) promoteInspection() error {
	if err := self.step("admission-before-promote", ""); err != nil {
		return err
	}
	if err := self.censusInspection(false); err != nil {
		return err
	}
	self.stateLock.Lock()
	self.inspection = false
	self.stateLock.Unlock()
	parentFile, err := self.parent.Open(".")
	if err != nil {
		return err
	}
	err = func() (resultErr error) {
		defer func() { resultErr = errors.Join(resultErr, parentFile.Close()) }()
		return self.syncDirectoryFile(parentFile, "parent")
	}()
	if err != nil {
		return err
	}
	if _, exists := self.sizes["LOCK"]; !exists && uint64(len(self.sizes)) >= self.bounds.MaxStorageFiles {
		return errAttemptRecordStoreLimit
	}
	self.ownerFile, err = self.openFile("LOCK", os.O_RDWR|os.O_CREATE, true)
	if err != nil {
		return err
	}
	if _, exists := self.sizes["LOCK"]; !exists {
		self.sizes["LOCK"] = 0
	}
	return self.syncDirectoryFile(self.directory, "owner")
}

// Opening cancellation is not the lifetime context of a successfully returned
// owner. Publication clears it only after all admitted recovery succeeds.
func (self *attemptRecordStoreStorage) finishOpening() error {
	if err := self.openingContextError(); err != nil {
		return err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.openingCtx = nil
	self.observed = nil
	return nil
}

// The final census checkpoint has no external callback after validation.
func (self *attemptRecordStoreStorage) openingContextError() error {
	self.stateLock.Lock()
	ctx := self.openingCtx
	self.stateLock.Unlock()
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			self.stateLock.Lock()
			stillOpening := self.openingCtx != nil
			self.stateLock.Unlock()
			if stillOpening {
				return err
			}
		}
	}
	return nil
}

// Inspection capability cannot accidentally expose a writable Storage method.
func (self *attemptRecordStoreStorage) requireWritable() error {
	self.stateLock.Lock()
	inspection := self.inspection
	self.stateLock.Unlock()
	if inspection {
		return errors.New("attempt record store inspection is read-only")
	}
	return self.openingContextError()
}

// A single read owner checks the actual descriptor before and after every
// bounded read, and joins its late close/cancellation outcome explicitly.
type attemptStoreAdmissionFile struct {
	file *os.File
	disk *attemptRecordStoreStorage
	name string
	info os.FileInfo
}

// Only files from the captured bounded census are admitted for parsing.
func (self *attemptRecordStoreStorage) inspectFile(name string) (result *attemptStoreAdmissionFile, resultErr error) {
	info := self.observed[name]
	if !attemptStorePrivateFile(info) {
		return nil, errors.New("attempt record store inspection file is absent")
	}
	file, err := self.openFile(name, os.O_RDONLY, false)
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			resultErr = errors.Join(resultErr, file.Close())
			result = nil
		}
	}()
	result = &attemptStoreAdmissionFile{file: file, disk: self, name: name, info: info}
	if err := result.check(); err != nil {
		return nil, err
	}
	complete = true
	return result, nil
}

// Native write state includes ctime; restoring mtime/size is not a match.
func (self *attemptStoreAdmissionFile) check() error {
	if err := self.disk.step("admission-read-check", self.name); err != nil {
		return err
	}
	opened, err := self.file.Stat()
	current, currentErr := self.disk.root.Lstat(self.name)
	if err != nil || currentErr != nil || !attemptLedgerSameFileState(self.info, opened) || !attemptLedgerSameFileState(self.info, current) {
		return errors.New("attempt record store file changed during inspection")
	}
	return nil
}

// Section readers bound offsets independently from the bytes they request.
func (self *attemptStoreAdmissionFile) ReadAt(data []byte, offset int64) (int, error) {
	if err := self.check(); err != nil {
		return 0, err
	}
	if offset < 0 || offset > self.info.Size() || int64(len(data)) > self.info.Size()-offset {
		return 0, io.ErrUnexpectedEOF
	}
	n, err := self.file.ReadAt(data, offset)
	return n, errors.Join(err, self.check())
}

// Hooks cannot suppress the actual close or its result.
func (self *attemptStoreAdmissionFile) Close() (resultErr error) {
	defer func() {
		err := self.file.Close()
		resultErr = errors.Join(resultErr, err, self.disk.step("admission-after-close", self.name))
	}()
	return self.disk.step("admission-before-close", self.name)
}
