// A completed local preparation can be borrowed without running its recovery
// writer. Partial claims remain the preparation owner's responsibility.
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

// Callers retain the five original shared preparation locks until this reader
// closes. A shared directory lock excludes preparation publication and recovery.
func openBootstrapSuccessorPreparationReader(ctx context.Context, expected bootstrapSuccessorPreparationPlan, hook func(string) error) (_ *bootstrapSuccessorPreparationStore, _ bootstrapSuccessorPreparationRecord, resultErr error) {
	return openBootstrapSuccessorPreparationReaderMode(ctx, expected, false, hook)
}

// Execution ownership acquires the same physical directory exclusively before
// borrowing preparation bytes. It never upgrades a shared lock in place.
func openBootstrapSuccessorPreparationReaderMode(ctx context.Context, expected bootstrapSuccessorPreparationPlan, exclusive bool, hook func(string) error) (_ *bootstrapSuccessorPreparationStore, _ bootstrapSuccessorPreparationRecord, resultErr error) {
	var record bootstrapSuccessorPreparationRecord
	if ctx == nil {
		return nil, record, errors.New("successor preparation reader context is absent")
	}
	if err := errors.Join(ctx.Err(), expected.validate()); err != nil {
		return nil, record, err
	}
	raw, err := json.Marshal(expected)
	if err != nil || len(raw) > maximumBootstrapSuccessorPreparationBytes {
		return nil, record, errors.Join(errors.New("successor preparation reader plan exceeds its byte bound"), err)
	}
	var copied bootstrapSuccessorPreparationPlan
	if err := decodePlanJson(raw, &copied); err != nil {
		return nil, record, err
	}
	path := copied.Proposal.OriginalRunDirectory
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, record, err
	}
	self := &bootstrapSuccessorPreparationStore{ctx: ctx, approval: bootstrapSuccessorPreparationApproval{Plan: copied}, directory: os.NewFile(uintptr(fd), path), hook: hook}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	mode := unix.LOCK_SH
	if exclusive {
		mode = unix.LOCK_EX
	}
	if err := unix.Flock(fd, mode|unix.LOCK_NB); err != nil {
		return nil, record, errors.Join(errors.New("successor preparation has an active local owner"), err)
	}
	if err := self.checkpoint("reader-acquired"); err != nil {
		return nil, record, err
	}
	entries, err := self.directory.Readdirnames(513)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) > 512 {
		return nil, record, errors.Join(errors.New("successor custody directory exceeds its bounded census"), err)
	}
	for _, name := range entries {
		if strings.HasPrefix(name, bootstrapSuccessorStagePrefix) {
			return nil, record, errors.New("successor preparation retains a staged claimant; resume its original owner")
		}
	}
	raw, err = self.read(bootstrapSuccessorPreparationFile, maximumBootstrapSuccessorPreparationBytes)
	if err == nil {
		err = decodePlanJson(raw, &record)
	}
	if err == nil {
		err = record.Approval.validate(copied)
	}
	if err == nil {
		err = record.validate(record.Approval)
	}
	canonical, encodeErr := json.Marshal(record)
	if err != nil || encodeErr != nil || !bytes.Equal(raw, canonical) {
		return nil, record, errors.Join(errors.New("successor preparation completed record differs from original custody"), err, encodeErr)
	}
	marker := []byte(rootObjectHash(record.Approval) + "\n" + bootstrapRootClaimComplete)
	retainedMarker, err := self.read(bootstrapSuccessorPreparationFile+".lock", len(marker))
	if err != nil || !bytes.Equal(retainedMarker, marker) {
		return nil, record, errors.Join(errors.New("successor preparation completion is absent or incomplete; resume its original owner"), err)
	}
	self.approval = record.Approval
	if err := self.checkpoint("reader-validated"); err != nil {
		return nil, record, err
	}
	return self, record, nil
}
