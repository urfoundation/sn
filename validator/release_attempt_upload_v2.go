// Existing authenticated operator sessions own typed upload lifetimes. Public
// readback and validator signatures remain separate evidence authorities.
package validator

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"
)

// One origin is many API instances behind its load balancer, and each instance
// keeps its own reserved upload lane and authority cache. An instance whose lane
// did not start or whose refresh failed can refuse an upload its siblings accept.
// A new request can reach a sibling. The bound keeps a refusal from every
// instance what it was: a hard failure after a few seconds, not a wait.
const (
	releaseReservedUploadRefusalAttempts = 4
	releaseReservedUploadRefusalDelay    = time.Second
)

// An upload a lossy or briefly unavailable path cut short is sent again: its
// own deadline, a transport failure, or an answer of 408, 425, 429 or 5xx from
// the api or its load balancer. Objects are content-addressed and the api
// stores them put-if-absent, so a repeat is safe. Before repeating, the public
// reader is asked whether the exact bytes already landed, which a lost
// acknowledgement leaves behind. Waits grow exponentially with jitter, honor a
// Retry-After up to the cap, and end with the caller. Any other answer keeps
// its verdict at once. On 2026-11-16 each cut-off upload instead failed its
// whole seal, and the steering loop started the seal over.
const (
	releaseAttemptUploadAttempts    = 6
	releaseAttemptUploadBackoffBase = 2 * time.Second
	releaseAttemptUploadBackoffCap  = time.Minute
)

// Immutable routing and limits are safe for concurrent operations. The session
// owner cancels on logout, failed token persistence or runtime shutdown; each
// operation also joins its caller's cancellation before returning.
type releaseAttemptUploadV2 struct {
	ctx                context.Context
	cancel             context.CancelFunc
	noID               uint64
	origin             string
	bounds             AttemptCutV2Bounds
	maxTransitionBytes uint64
	writer             *HTTPAttemptStreamV2Writer
	// the wait between retries of a refused or cut-short upload; nil is the real one
	refusalWait releaseSnapshotRetryWait
	// a uniform draw in [0, n); nil is math/rand/v2's
	jitter func(n int64) int64
}

// Admission does not fetch a credential or perform network I/O. The getter is
// the actual API session's live getter, never a copied bootstrap credential.
func newReleaseAttemptUploadV2(ctx context.Context, operator OperatorConfig, bounds ReleaseEvidenceV2Bounds, byJwt func() string) (*releaseAttemptUploadV2, error) {
	if ctx == nil || operator.NoID == 0 || byJwt == nil {
		return nil, errors.New("release attempt upload session is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateReleaseMeasurementInputV2Limit(bounds.MaxTransitionBytes); err != nil {
		return nil, err
	}
	ownerCtx, cancel := context.WithCancel(ctx)
	writer, err := newHttpAttemptStreamV2Writer(operator.APIURL, bounds.Cut, max(attemptStreamV2MetadataBytes(bounds.Cut), bounds.MaxTransitionBytes), func() string {
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
	if bounds.Cut.MaxHeaderBytes == 0 || bounds.Cut.MaxHeaderBytes > attemptStreamV2MetadataBytes(bounds.Cut) {
		cancel()
		return nil, errors.New("release attempt upload header exceeds its public metadata bound")
	}
	if err := ownerCtx.Err(); err != nil {
		cancel()
		return nil, err
	}
	return &releaseAttemptUploadV2{ctx: ownerCtx, cancel: cancel, noID: operator.NoID, origin: operator.APIURL, bounds: bounds.Cut, maxTransitionBytes: bounds.MaxTransitionBytes, writer: writer}, nil
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
	wait := self.refusalWait
	if wait == nil {
		wait = waitReleaseSnapshotRetry
	}
	refusals, failures := uint64(0), uint64(0)
	for {
		err := self.writer.write(operationCtx, kind, hash, raw, signer)
		if err == nil || operationCtx.Err() != nil {
			return err
		}
		var delay time.Duration
		switch {
		case signer != nil && releaseUploadForbidden(err):
			refusals++
			if refusals >= releaseReservedUploadRefusalAttempts {
				return err
			}
			releaseDiagnostic(operationCtx, "operator", "publication_unavailable_retrying", 0, false, refusals, releaseDiagnosticFacts{cause: releaseDiagnosticUnavailable, operatorId: self.noID, operatorKnown: true})
			delay = releaseReservedUploadRefusalDelay
		case releaseAttemptUploadTransient(err):
			failures++
			if failures >= releaseAttemptUploadAttempts {
				return err
			}
			cause := releaseDiagnosticUnavailable
			if releaseUploadStatus(err) == nil {
				cause = releaseDiagnosticReadCause(err)
			}
			releaseDiagnostic(operationCtx, "operator", "publication_unavailable_retrying", 0, false, failures, releaseDiagnosticFacts{cause: cause, operatorId: self.noID, operatorKnown: true})
			// A lost acknowledgement leaves the stored object behind.
			if self.landed(operationCtx, kind, hash, len(raw)) {
				return nil
			}
			delay = self.backoff(failures, err)
		default:
			return err
		}
		// An interrupted retry has no verdict yet, so the refusal is not its answer.
		if err := wait(operationCtx, delay); err != nil {
			return err
		}
	}
}

// A ceiling that doubles from the base up to the cap, and a wait drawn
// uniformly from its upper half: spaced enough for a path to recover, spread
// so validators that lost the same path do not return at once. A longer
// Retry-After the answer carried is honored, up to the cap.
func (self *releaseAttemptUploadV2) backoff(failures uint64, err error) time.Duration {
	jitter := self.jitter
	if jitter == nil {
		jitter = rand.Int64N
	}
	ceiling := min(releaseAttemptUploadBackoffBase<<min(failures-1, 8), releaseAttemptUploadBackoffCap)
	delay := ceiling/2 + time.Duration(jitter(int64(ceiling-ceiling/2)))
	if status := releaseUploadStatus(err); status != nil {
		delay = max(delay, status.retryAfter)
	}
	return min(delay, releaseAttemptUploadBackoffCap)
}

// Whether the object is already stored at this origin: the public reader
// answers its content address with the typed content and exact length. The
// publisher reads every object back in full once its write returns, so this
// takes only the answer's head, within one stall window. Absence or any
// failure says nothing, and the upload is sent again.
func (self *releaseAttemptUploadV2) landed(ctx context.Context, kind, hash string, size int) bool {
	writer := self.writer
	limit := writer.metadataBytes
	switch kind {
	case AttemptStreamV2Records:
		limit = writer.recordBytes
	case AttemptStreamV2Proofs:
		limit = writer.proofBytes
	}
	probe, cancel := context.WithTimeout(ctx, attemptStreamV2HttpWriteIoTimeout)
	defer cancel()
	reader := &HTTPAttemptStreamV2Reader{endpoint: writer.endpoint, metadataBytes: writer.metadataBytes, recordBytes: writer.recordBytes, proofBytes: writer.proofBytes, client: writer.client}
	body, err := reader.open(probe, kind, hash, uint64(size), limit)
	if err != nil {
		return false
	}
	// The body is left unread on purpose; its incomplete close is no answer.
	_ = body.Close()
	return true
}

// The answers of a path that lost the upload rather than refused it: the
// owner's own deadline, a transport failure, or 408, 425, 429 or 5xx. A 400,
// 401, 403, 413 or a malformed acknowledgement keeps its verdict.
func releaseAttemptUploadTransient(err error) bool {
	if status := releaseUploadStatus(err); status != nil {
		return status.status == http.StatusRequestTimeout || status.status == http.StatusTooEarly || status.status == http.StatusTooManyRequests || status.status >= 500 && status.status <= 599
	}
	return RetryableEvidenceTransportError(err)
}

// One upload's status answer, alone or joined only with nothing else.
func releaseUploadStatus(err error) *attemptStreamHttpStatusError {
	for range 16 {
		switch cause := err.(type) {
		case *attemptStreamHttpStatusError:
			if cause.upload {
				return cause
			}
			return nil
		case interface{ Unwrap() []error }:
			causes := cause.Unwrap()
			if len(causes) != 1 {
				return nil
			}
			err = causes[0]
		default:
			return nil
		}
	}
	return nil
}

// One upload's plain 403 and nothing else. The status alone decides, never the
// answer's text, and a joined close failure or cancellation keeps its own verdict.
func releaseUploadForbidden(err error) bool {
	for range 16 {
		switch cause := err.(type) {
		case *attemptStreamHttpStatusError:
			return cause.upload && cause.status == http.StatusForbidden
		case interface{ Unwrap() []error }:
			causes := cause.Unwrap()
			if len(causes) != 1 {
				return false
			}
			err = causes[0]
		default:
			return false
		}
	}
	return false
}
