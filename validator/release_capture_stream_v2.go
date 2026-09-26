//go:build linux || darwin

// Repeated signed cuts can share immutable data. Diagnostic capture keeps only
// bounded success witnesses; the existing sink owns the original byte archive.
package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

type releaseCaptureStreamKeyV2 struct {
	origin      string
	kind        string
	contentHash string
	size        uint64
}

// One synchronous capture owns this map. No bytes or success survive a new
// invocation, and each origin and typed data route must independently succeed.
type releaseCaptureStreamsV2 struct {
	bounds         AttemptCutV2Bounds
	maximumObjects uint64
	retained       map[releaseCaptureStreamKeyV2]struct{}
	retryReads     bool
	hooks          releaseCaptureStreamHooksV2
	emit           func(ReleaseEvidenceV2CaptureSource, []byte) error
}

func newReleaseCaptureStreamsV2(bounds AttemptCutV2Bounds, options ReleaseEvidenceV2CaptureOptions, emit func(ReleaseEvidenceV2CaptureSource, []byte) error) *releaseCaptureStreamsV2 {
	self := &releaseCaptureStreamsV2{bounds: bounds, maximumObjects: options.MaximumObjects, retryReads: options.RetryStreamReads, emit: emit}
	if options.ReuseCapturedStreams {
		self.retained = map[releaseCaptureStreamKeyV2]struct{}{}
	}
	return self
}

// Every reference still traverses and authenticates its complete metadata
// census. Only a chunk whose exact body reached authenticated EOF, closed, and
// was durably accepted by the sink can avoid another identical network read.
func (self *releaseCaptureStreamsV2) capture(ctx context.Context, origin, kind string, reference AttemptStreamV2Reference, limits AttemptStreamV2Bounds) error {
	if self == nil || self.emit == nil || self.maximumObjects == 0 || ctx == nil {
		return errors.New("compact stream capture owner is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	newReader := self.hooks.newReader
	if newReader == nil {
		newReader = NewHTTPAttemptStreamV2Reader
	}
	reader, err := newReader(origin, self.bounds)
	if err != nil {
		return err
	}
	_, err = WalkAttemptStreamV2Descriptors(ctx, kind, reference, limits, func(ctx context.Context, hash string, size uint64) ([]byte, error) {
		encoded, err := self.read(ctx, func(ctx context.Context) ([]byte, error) { return reader.ReadMetadata(ctx, hash, size) })
		if err != nil {
			return nil, fmt.Errorf("metadata %s (%d bytes): %w", hash, size, err)
		}
		if err := self.emit(ReleaseEvidenceV2CaptureSource{Kind: "metadata", Name: hash, Origin: origin}, encoded); err != nil {
			return nil, fmt.Errorf("retain metadata %s: %w", hash, err)
		}
		return encoded, nil
	}, func(chunk AttemptStreamV2Chunk) (resultErr error) {
		defer func() {
			if resultErr != nil {
				resultErr = fmt.Errorf("chunk %d %s (%d bytes): %w", chunk.Index, chunk.ContentHash, chunk.DataBytes, resultErr)
			}
		}()
		if err := ctx.Err(); err != nil {
			return err
		}
		key := releaseCaptureStreamKeyV2{origin: origin, kind: kind, contentHash: chunk.ContentHash, size: chunk.DataBytes}
		if _, found := self.retained[key]; found {
			return nil
		}
		if self.retained != nil && uint64(len(self.retained)) >= self.maximumObjects {
			return errors.New("compact stream capture exceeds its approved witness count")
		}
		encoded, err := self.read(ctx, func(ctx context.Context) ([]byte, error) {
			body, err := reader.OpenData(ctx, kind, chunk.ContentHash, chunk.DataBytes)
			if err != nil {
				return nil, err
			}
			encoded, readErr := io.ReadAll(body)
			return encoded, errors.Join(readErr, body.Close(), ctx.Err())
		})
		if err != nil {
			return err
		}
		if err := self.emit(ReleaseEvidenceV2CaptureSource{Kind: kind, Name: chunk.ContentHash, Origin: origin}, encoded); err != nil {
			return fmt.Errorf("retain original body: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if self.retained != nil {
			self.retained[key] = struct{}{}
		}
		return nil
	})
	return err
}

// A failed attempt owns no bytes. Only diagnostic readers receive a five-minute
// operation with fresh one-minute attempts; every exact body and close still
// passes the original transport before the sink can observe a successful read.
func (self *releaseCaptureStreamsV2) read(ctx context.Context, read func(context.Context) ([]byte, error)) ([]byte, error) {
	if !self.retryReads {
		return read(ctx)
	}
	operationCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	wait := self.hooks.wait
	if wait == nil {
		wait = waitReleaseSnapshotRetry
	}
	var lastErr error
	for {
		if err := errors.Join(ctx.Err(), operationCtx.Err()); err != nil {
			return nil, errors.Join(lastErr, err)
		}
		attemptCtx, attemptCancel := context.WithTimeout(operationCtx, 60*time.Second)
		encoded, err := read(attemptCtx)
		lastErr = errors.Join(err, attemptCtx.Err(), operationCtx.Err(), ctx.Err())
		attemptCancel()
		if lastErr == nil {
			return encoded, nil
		}
		if !RetryableEvidenceTransportError(lastErr) {
			return nil, lastErr
		}
		if err := wait(operationCtx, releaseSnapshotRetryDelayForError(lastErr)); err != nil {
			return nil, errors.Join(lastErr, err, ctx.Err())
		}
	}
}
