//go:build linux || darwin

// Each cut of a settlement epoch writes the whole epoch's streams again, though
// they are content addressed and a later cut's stream starts with the rows of the
// one before it: all but the tail of its chunks are objects the origin already
// acknowledged. Writing them again costs minutes per cut at the end of an epoch,
// and every extra write is one more chance for a single refusing API instance to
// fail the whole seal. On 11-01 that is how one instance stopped steering.
//
// This owner remembers, for the life of the process, the objects every replica
// acknowledged. A seal skips those writes and still reads the complete cut back
// from the public origin before it signs, so a skipped object that is gone fails
// that seal, and that failure forgets everything so the next one writes it all.
package validator

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
)

// A settlement epoch's objects fit well inside this. Past it the owner forgets
// everything and the next seal writes its objects again, which is only slower.
const releaseAttemptUploadMemoObjects = 1 << 16

type releaseAttemptUploadMemo struct {
	stateLock    sync.Mutex
	acknowledged map[[2]string]struct{}
}

func newReleaseAttemptUploadMemo() *releaseAttemptUploadMemo {
	return &releaseAttemptUploadMemo{acknowledged: make(map[[2]string]struct{})}
}

func (self *releaseAttemptUploadMemo) has(kind, contentHash string) bool {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	_, found := self.acknowledged[[2]string{kind, contentHash}]
	return found
}

func (self *releaseAttemptUploadMemo) remember(kind, contentHash string) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if len(self.acknowledged) >= releaseAttemptUploadMemoObjects {
		clear(self.acknowledged)
	}
	self.acknowledged[[2]string{kind, contentHash}] = struct{}{}
}

func (self *releaseAttemptUploadMemo) forget() {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	clear(self.acknowledged)
}

// Only a write that returned success is remembered, and that means every
// replica acknowledged the exact bytes of this content address.
func (self *releaseAttemptUploadMemo) writer(kind string, write AttemptStreamV2ObjectWriter) AttemptStreamV2ObjectWriter {
	if self == nil {
		return write
	}
	return func(ctx context.Context, contentHash string, raw []byte) error {
		if ctx == nil {
			return errors.New("attempt upload memo context is missing")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if self.has(kind, contentHash) {
			return nil
		}
		if err := write(ctx, contentHash, raw); err != nil {
			return err
		}
		self.remember(kind, contentHash)
		return nil
	}
}

// The seal's read-back is what proves a skipped object is still there. An origin
// that says it is gone gets everything written again by the next seal.
func (self *releaseAttemptUploadMemo) noticeGone(err error) {
	var status *attemptStreamHttpStatusError
	if self != nil && errors.As(err, &status) && status.status == http.StatusNotFound {
		self.forget()
	}
}

func (self *releaseAttemptUploadMemo) metadataReader(read AttemptStreamV2MetadataReader) AttemptStreamV2MetadataReader {
	if self == nil {
		return read
	}
	return func(ctx context.Context, contentHash string, size uint64) ([]byte, error) {
		raw, err := read(ctx, contentHash, size)
		self.noticeGone(err)
		return raw, err
	}
}

func (self *releaseAttemptUploadMemo) dataOpener(open AttemptStreamV2DataOpener) AttemptStreamV2DataOpener {
	if self == nil {
		return open
	}
	return func(ctx context.Context, kind, contentHash string, size uint64) (io.ReadCloser, error) {
		reader, err := open(ctx, kind, contentHash, size)
		self.noticeGone(err)
		return reader, err
	}
}
