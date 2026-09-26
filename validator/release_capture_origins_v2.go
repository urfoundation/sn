//go:build linux || darwin

// Two diagnostic origin readers share one synchronous durable sink. Workers
// retain only their current bounded body and invocation-local success witnesses;
// all archive accounting and output callbacks stay on the calling goroutine.
package validator

import (
	"context"
	"errors"
	"fmt"
)

// Call-local tests replace transport and pacing without changing signatures,
// descriptor walking, origin identity, response custody or the archive sink.
type releaseCaptureStreamHooksV2 struct {
	newReader func(string, AttemptCutV2Bounds) (*HTTPAttemptStreamV2Reader, error)
	wait      releaseSnapshotRetryWait
}

// The worker lends its immutable body until the coordinator acknowledges it.
// There are exactly two workers and no buffered body queue.
type releaseCaptureEmissionV2 struct {
	source  ReleaseEvidenceV2CaptureSource
	encoded []byte
	done    chan error
}

// The caller has already authenticated every cut header and activation. Each
// origin still traverses complete metadata and proves its own exact bodies.
// One origin's failure does not discard the other's remaining useful sources.
func captureReleaseStreamOriginsV2(ctx context.Context, bounds AttemptCutV2Bounds, options ReleaseEvidenceV2CaptureOptions, cuts []*AttemptCutV2, emit func(ReleaseEvidenceV2CaptureSource, []byte) error, hooks releaseCaptureStreamHooksV2) error {
	if ctx == nil || emit == nil || options.Origins[0] == "" || options.Origins[1] == "" || options.Origins[0] == options.Origins[1] {
		return errors.New("compact stream origin owner is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, cut := range cuts {
		if cut == nil {
			return errors.New("compact stream origin owner has a missing cut")
		}
	}
	run := func(ctx context.Context, capture *releaseCaptureStreamsV2, origin string, index int, cut *AttemptCutV2) error {
		for _, stream := range []struct {
			kind      string
			reference AttemptStreamV2Reference
			limits    AttemptStreamV2Bounds
		}{{kind: AttemptStreamV2Records, reference: cut.Records, limits: bounds.Records}, {kind: AttemptStreamV2Proofs, reference: cut.Proofs, limits: bounds.Proofs}} {
			if err := capture.capture(ctx, origin, stream.kind, stream.reference, stream.limits); err != nil {
				return fmt.Errorf("compact capture cut %d/%d operator %d origin %s %s: %w", index+1, len(cuts), cut.Context.Identity.NoID, origin, stream.kind, err)
			}
		}
		return nil
	}
	if !options.ParallelStreamOrigins {
		capture := newReleaseCaptureStreamsV2(bounds, options, emit)
		capture.hooks = hooks
		for index, cut := range cuts {
			for _, origin := range options.Origins {
				if err := run(ctx, capture, origin, index, cut); err != nil {
					return err
				}
			}
		}
		return ctx.Err()
	}
	ownerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	emissions := make(chan releaseCaptureEmissionV2)
	results := make(chan error, len(options.Origins))
	for _, origin := range options.Origins {
		go func() {
			var resultErr error
			defer func() {
				if failure := recover(); failure != nil {
					resultErr = fmt.Errorf("compact capture origin %s panicked: %v", origin, failure)
				}
				results <- resultErr
			}()
			capture := newReleaseCaptureStreamsV2(bounds, options, func(source ReleaseEvidenceV2CaptureSource, encoded []byte) error {
				request := releaseCaptureEmissionV2{source: source, encoded: encoded, done: make(chan error, 1)}
				select {
				case <-ownerCtx.Done():
					return ownerCtx.Err()
				case emissions <- request:
				}
				select {
				case <-ownerCtx.Done():
					return ownerCtx.Err()
				case err := <-request.done:
					return err
				}
			})
			capture.hooks = hooks
			for index, cut := range cuts {
				if resultErr = run(ownerCtx, capture, origin, index, cut); resultErr != nil {
					return
				}
			}
		}()
	}
	var failures []error
	for remaining := len(options.Origins); remaining != 0; {
		select {
		case request := <-emissions:
			err := ownerCtx.Err()
			if err == nil {
				err = func() (resultErr error) {
					defer func() {
						if failure := recover(); failure != nil {
							resultErr = fmt.Errorf("compact capture sink panicked: %v", failure)
						}
					}()
					return emit(request.source, request.encoded)
				}()
			}
			request.done <- err
			if err != nil {
				failures = append(failures, err)
				cancel()
			}
		case err := <-results:
			failures = append(failures, err)
			remaining--
		}
	}
	return errors.Join(errors.Join(failures...), ctx.Err())
}
