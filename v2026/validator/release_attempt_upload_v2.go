// Existing authenticated operator sessions own typed upload lifetimes. Public
// readback and validator signatures remain separate evidence authorities.
package validator

import (
	"context"
	"errors"
)

// Immutable routing and limits are safe for concurrent operations. The session
// owner cancels on logout, failed token persistence or runtime shutdown; each
// operation also joins its caller's cancellation before returning.
type releaseAttemptUploadV2 struct {
	ctx    context.Context
	cancel context.CancelFunc
	noID   uint64
	origin string
	bounds AttemptCutV2Bounds
	writer *HTTPAttemptStreamV2Writer
}

// Admission does not fetch a credential or perform network I/O. The getter is
// the actual API session's live getter, never a copied bootstrap credential.
func newReleaseAttemptUploadV2(ctx context.Context, operator OperatorConfig, bounds AttemptCutV2Bounds, byJwt func() string) (*releaseAttemptUploadV2, error) {
	if ctx == nil || operator.NoID == 0 || byJwt == nil {
		return nil, errors.New("release attempt upload session is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ownerCtx, cancel := context.WithCancel(ctx)
	writer, err := NewHTTPAttemptStreamV2Writer(operator.APIURL, bounds, func() string {
		if ownerCtx.Err() != nil {
			return ""
		}
		credential := byJwt()
		if ownerCtx.Err() != nil {
			return ""
		}
		return credential
	})
	if err != nil {
		cancel()
		return nil, err
	}
	if bounds.MaxHeaderBytes == 0 || bounds.MaxHeaderBytes > writer.metadataBytes {
		cancel()
		return nil, errors.New("release attempt upload header exceeds its public metadata bound")
	}
	if err := ownerCtx.Err(); err != nil {
		cancel()
		return nil, err
	}
	return &releaseAttemptUploadV2{ctx: ownerCtx, cancel: cancel, noID: operator.NoID, origin: operator.APIURL, bounds: bounds, writer: writer}, nil
}

// Cancellation requests are nonjoining and safe inside API callbacks. Every
// admitted publication still owns and joins its complete upload/readback work.
func (self *releaseAttemptUploadV2) close() {
	if self != nil && self.cancel != nil {
		self.cancel()
	}
}

// A publication is bounded by both the caller and the original session owner.
// The small cancellation callback is joined even when it races completion.
func (self *releaseAttemptUploadV2) write(ctx context.Context, kind, hash string, raw []byte) (resultErr error) {
	return self.writeWithSigner(ctx, kind, hash, raw, nil)
}

// The source activation signer is independent of this destination's live API
// credential. Both ordinary and protected writes retain the same joined owner.
func (self *releaseAttemptUploadV2) writeWithSigner(ctx context.Context, kind, hash string, raw []byte, signer *validatorAttemptUploadSigner) (resultErr error) {
	if ctx == nil || self == nil || self.ctx == nil || self.writer == nil {
		return errors.New("release attempt upload operation is incomplete")
	}
	if err := errors.Join(ctx.Err(), self.ctx.Err()); err != nil {
		return err
	}
	operationCtx, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	stop := context.AfterFunc(self.ctx, func() {
		defer close(finished)
		cancel()
	})
	defer func() {
		if !stop() {
			<-finished
		}
		resultErr = errors.Join(resultErr, ctx.Err(), self.ctx.Err())
		cancel()
	}()
	return self.writer.write(operationCtx, kind, hash, raw, signer)
}
