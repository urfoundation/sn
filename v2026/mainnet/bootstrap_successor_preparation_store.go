// One directory-locked, descriptor-relative claim prepares a local successor.
// Published files are immutable; interrupted staging retains its approval id.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const bootstrapSuccessorStagePrefix = ".contract-successor-preparation-"

// A caller holds the original five preparation locks for this owner's lifetime.
// The directory lock serializes cooperating preparations, not other machines
// or a privileged operator. An error closes the owner before any further use.
type bootstrapSuccessorPreparationStore struct {
	ctx       context.Context
	approval  bootstrapSuccessorPreparationApproval
	directory *os.File
	hook      func(string) error
}

// Fresh prepare claims once. Resume requires an existing exact claim or its
// hash-named stage; absence never becomes a new local budget or approval.
func openBootstrapSuccessorPreparationStore(ctx context.Context, expected bootstrapSuccessorPreparationPlan, approval bootstrapSuccessorPreparationApproval, create bool, hook func(string) error) (_ *bootstrapSuccessorPreparationStore, resultErr error) {
	if ctx == nil {
		return nil, errors.New("successor preparation context is absent")
	}
	if err := errors.Join(ctx.Err(), approval.validate(expected)); err != nil {
		return nil, err
	}
	// Copy all slice/pointer fields before retaining independently verified input.
	raw, err := json.Marshal(approval)
	if err != nil || len(raw) > maximumBootstrapSuccessorPreparationBytes {
		return nil, errors.Join(errors.New("successor approval exceeds its byte bound"), err)
	}
	var copied bootstrapSuccessorPreparationApproval
	if err := decodePlanJson(raw, &copied); err != nil {
		return nil, err
	}
	record := bootstrapSuccessorPreparationRecord{Schema: bootstrapSuccessorPreparationStateSchema, Approval: copied, Phase: "prepared-offline"}
	record.ContentHash = rootObjectHash(record)
	recordBytes, err := json.Marshal(record)
	if err != nil || len(recordBytes) > maximumBootstrapSuccessorPreparationBytes {
		return nil, errors.Join(errors.New("successor record exceeds its byte bound"), err)
	}
	path := expected.Proposal.OriginalRunDirectory
	physical, err := bootstrapSuccessorPhysicalRoot(path)
	if err != nil || physical != expected.Root {
		return nil, errors.Join(errors.New("successor physical root differs from the signed preparation"), err)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	self := &bootstrapSuccessorPreparationStore{ctx: ctx, approval: copied, directory: os.NewFile(uintptr(fd), path), hook: hook}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(errors.New("successor preparation already has a local owner"), err)
	}
	if err := self.checkpoint("owner-acquired"); err != nil {
		return nil, err
	}
	entries, err := self.directory.Readdirnames(513)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) > 512 {
		return nil, errors.Join(errors.New("successor custody directory exceeds its bounded census"), err)
	}
	claimStage, recordStage := self.stageName("claim"), self.stageName("record")
	markerName := bootstrapSuccessorPreparationFile + ".lock"
	present := map[string]bool{}
	for _, name := range entries {
		if name == bootstrapSuccessorPreparationFile || name == markerName || strings.HasPrefix(name, bootstrapSuccessorStagePrefix) {
			if name != bootstrapSuccessorPreparationFile && name != markerName && name != claimStage && name != recordStage {
				return nil, errors.New("successor custody retains another or unknown staged claimant")
			}
			present[name] = true
		}
	}
	if create && len(present) != 0 {
		return nil, errors.New("successor prepare requires unused fixed custody; retain its existing claim for resume")
	}
	if !create && !present[markerName] && !present[claimStage] {
		return nil, errors.New("successor resume requires its retained claim; missing custody cannot renew preparation")
	}
	if !present[markerName] && (present[bootstrapSuccessorPreparationFile] || present[recordStage]) || present[markerName] && present[claimStage] || present[bootstrapSuccessorPreparationFile] && present[recordStage] {
		return nil, errors.New("successor custody contains inconsistent publication stages")
	}
	marker := []byte(rootObjectHash(copied) + "\n")
	if !present[markerName] {
		if err := self.publish("claim", markerName, marker); err != nil {
			return nil, err
		}
	}
	retainedMarker, err := self.read(markerName, len(marker)+len(bootstrapRootClaimComplete))
	if err != nil || !bytes.HasPrefix(retainedMarker, marker) || !bytes.HasPrefix([]byte(bootstrapRootClaimComplete), retainedMarker[len(marker):]) {
		return nil, errors.Join(errors.New("successor retained claim is incomplete or differs from its approval"), err)
	}
	complete := len(retainedMarker) == len(marker)+len(bootstrapRootClaimComplete)
	retainedRecord, err := self.read(bootstrapSuccessorPreparationFile, maximumBootstrapSuccessorPreparationBytes)
	if errors.Is(err, os.ErrNotExist) && !complete && len(retainedMarker) == len(marker) {
		if err := self.publish("record", bootstrapSuccessorPreparationFile, recordBytes); err != nil {
			return nil, err
		}
	} else if err != nil || !bytes.Equal(retainedRecord, recordBytes) {
		return nil, errors.Join(errors.New("successor retained record is missing, malformed or rebound; preserve custody"), err)
	}
	if err := self.checkpoint("record-retained"); err != nil {
		return nil, err
	}
	// Even an observed complete marker is synced on reopen: an earlier process
	// might have failed after the completion write but before its fsync.
	markerFd, err := unix.Openat(fd, markerName, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	markerFile := os.NewFile(uintptr(markerFd), markerName)
	defer markerFile.Close()
	if err := bootstrapSuccessorPrivateRegular(markerFile); err != nil {
		return nil, err
	}
	if !complete {
		written, err := markerFile.WriteAt([]byte(bootstrapRootClaimComplete), int64(len(marker)))
		if err != nil || written != len(bootstrapRootClaimComplete) {
			return nil, errors.Join(io.ErrShortWrite, err)
		}
		if err := self.checkpoint("complete-written"); err != nil {
			return nil, err
		}
	}
	if err := errors.Join(markerFile.Sync(), self.directory.Sync()); err != nil {
		return nil, err
	}
	if err := self.checkpoint("complete-synced"); err != nil {
		return nil, err
	}
	return self, nil
}

// Physical identity is rechecked at every publication boundary. A renamed or
// restored path must not silently become this approval's new custody root.
func (self *bootstrapSuccessorPreparationStore) checkpoint(stage string) error {
	if self.directory == nil {
		return errors.New("successor preparation owner is closed")
	}
	check := func() error {
		var stat unix.Stat_t
		if err := unix.Fstat(int(self.directory.Fd()), &stat); err != nil {
			return err
		}
		physical, err := bootstrapSuccessorPhysicalRoot(self.approval.Plan.Proposal.OriginalRunDirectory)
		if err != nil || physical != self.approval.Plan.Root || physical.Device != uint64(stat.Dev) || physical.Inode != stat.Ino || stat.Mode&0077 != 0 {
			return errors.Join(errors.New("successor physical root changed during local preparation"), err)
		}
		return self.ctx.Err()
	}
	if err := check(); err != nil {
		return err
	}
	if self.hook != nil {
		if err := self.hook(stage); err != nil {
			return err
		}
	}
	return check()
}

// Approval-hash names reserve a claimant even before its stage bytes exist.
func (self *bootstrapSuccessorPreparationStore) stageName(kind string) string {
	return bootstrapSuccessorStagePrefix + strings.TrimPrefix(rootObjectHash(self.approval), "sha256:") + "." + kind
}

// Descriptor-relative reads reject links, special files, shared permissions and
// extra hardlinks. Empty files remain distinguishable from missing custody.
func (self *bootstrapSuccessorPreparationStore) read(name string, maximum int) ([]byte, error) {
	fd, err := unix.Openat(int(self.directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	if err := bootstrapSuccessorPrivateRegular(file); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil || len(raw) > maximum {
		return nil, errors.Join(errors.New("successor retained file exceeds its bound"), err)
	}
	return raw, nil
}

// Only an exact prefix of this immutable stage can be resumed. A no-replace
// rename publishes the fully synced file; no retained fixed name is overwritten.
func (self *bootstrapSuccessorPreparationStore) publish(kind, name string, raw []byte) error {
	if err := self.checkpoint(kind + "-begin"); err != nil {
		return err
	}
	fd := int(self.directory.Fd())
	stage := self.stageName(kind)
	stageFd, err := unix.Openat(fd, stage, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CREAT|unix.O_EXCL, 0600)
	if errors.Is(err, unix.EEXIST) {
		stageFd, err = unix.Openat(fd, stage, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	}
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(stageFd), stage)
	defer file.Close()
	if err := bootstrapSuccessorPrivateRegular(file); err != nil {
		return err
	}
	retained, err := io.ReadAll(io.LimitReader(file, int64(len(raw))+1))
	if err != nil || !bytes.HasPrefix(raw, retained) {
		return errors.Join(errors.New("successor stage differs from the exact immutable preparation prefix"), err)
	}
	if err := self.checkpoint(kind + "-name-created"); err != nil {
		return err
	}
	if err := self.directory.Sync(); err != nil {
		return err
	}
	if err := self.checkpoint(kind + "-name-synced"); err != nil {
		return err
	}
	written, err := file.WriteAt(raw, 0)
	if err != nil || written != len(raw) {
		return errors.Join(io.ErrShortWrite, err)
	}
	if err := self.checkpoint(kind + "-stage-written"); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := self.checkpoint(kind + "-stage-synced"); err != nil {
		return err
	}
	if err := unix.Renameat2(fd, stage, fd, name, unix.RENAME_NOREPLACE); err != nil {
		return errors.Join(errors.New("successor publication refused an existing fixed name"), err)
	}
	if err := self.checkpoint(kind + "-published"); err != nil {
		return err
	}
	if err := self.directory.Sync(); err != nil {
		return err
	}
	return self.checkpoint(kind + "-published-synced")
}

// Close releases this local directory owner without deleting any custody.
func (self *bootstrapSuccessorPreparationStore) close() error {
	if self == nil || self.directory == nil {
		return nil
	}
	err := self.directory.Close()
	self.directory = nil
	return err
}
