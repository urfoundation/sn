// Read-only observations own reconnect recovery across all of their RPC calls.
// A changed transport discards the entire view without repeating a write.
package crv4

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Only timeout and pacing are replaceable; the real reader and exact selected
// block are retained by the public operation. Callers configure before sharing.
type runtimeObservationReadHooks struct {
	withTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	wait        func(context.Context, time.Duration) error
}

type runtimeObservationReadOwnerKey struct{}

// Nested schedule/stake/artifact reads share the first owner's total deadline.
type runtimeObservationReadOwner struct {
	api    *gsrpc.SubstrateAPI
	parent context.Context
}

// The callback performs one complete read-only observation. Reconnect repeats
// its identity, artifact and closing reads under one original deadline; nested
// runtime readers share that owner. Callers retain their original block and
// policy and publish the returned value only after this function succeeds.
// Signing, submission and durable mutation do not belong in this callback.
func ReadRuntimeObservationContext[T any](ctx context.Context, chain *Chain, read func(context.Context) (T, error)) (T, error) {
	return readRuntimeObservation(ctx, chain, read)
}

// Admission is deliberately one attempt. This read-only wrapper consumes a
// lost transport observation under a finite deadline, preserving exact pins.
func ReadRuntimeArtifactAtContext(ctx context.Context, chain *Chain, blockHash types.Hash, allowed ...RuntimeArtifactIdentity) (AuthenticatedRuntimeArtifact, error) {
	if len(allowed) > maximumRuntimeMetadataArtifactsPerChain {
		return AuthenticatedRuntimeArtifact{}, errors.New("runtime observation allowlist exceeds its bound")
	}
	allowed = slices.Clone(allowed)
	return readRuntimeObservation(ctx, chain, func(ctx context.Context) (AuthenticatedRuntimeArtifact, error) {
		return AuthenticateRuntimeArtifactAtContext(ctx, chain, blockHash, allowed...)
	})
}

// Only this private observation-expiry type permits whole-view replay. Other
// transport errors retain their existing per-RPC/outer-owner policy. A hard,
// nil, cyclic or over-budget cause graph never acquires retry authority.
func runtimeObservationMayRepeat(err error) bool {
	remaining := 128
	var visit func(error, int) bool
	visit = func(err error, depth int) bool {
		remaining--
		if err == nil || remaining < 0 || depth > 32 {
			return false
		}
		value := reflect.ValueOf(err)
		switch value.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
			if value.IsNil() {
				return false
			}
		}
		if _, ok := err.(*runtimeTransportObservationError); ok {
			return true
		}
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			causes := joined.Unwrap()
			if len(causes) == 0 || len(causes) > remaining {
				return false
			}
			for _, cause := range causes {
				if !visit(cause, depth+1) {
					return false
				}
			}
			return true
		}
		if wrapped, ok := err.(interface{ Unwrap() error }); ok {
			return visit(wrapped.Unwrap(), depth+1)
		}
		return false
	}
	return visit(err, 0)
}

// Every attempt snapshots transport around the complete read, including late
// storage and canonical checks after artifact authentication. Nested calls do
// one attempt so only the outermost view can decide to repeat its earlier work.
func readRuntimeObservation[T any](ctx context.Context, chain *Chain, read func(context.Context) (T, error)) (T, error) {
	var empty T
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || read == nil {
		return empty, errors.New("runtime observation read owner is incomplete")
	}
	attempt := func(ctx context.Context) (T, error) {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		transport, err := observeRuntimeTransport(chain)
		if err != nil {
			return empty, err
		}
		result, err := read(ctx)
		if err != nil {
			return empty, errors.Join(err, ctx.Err())
		}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if !transport.matches(chain) {
			return empty, &runtimeTransportObservationError{}
		}
		return result, nil
	}
	if owner, ok := ctx.Value(runtimeObservationReadOwnerKey{}).(runtimeObservationReadOwner); ok && owner.api == chain.API {
		return attempt(ctx)
	}
	withTimeout, wait := chain.runtimeObservationRead.withTimeout, chain.runtimeObservationRead.wait
	if withTimeout == nil {
		withTimeout = context.WithTimeout
	}
	if wait == nil {
		wait = func(ctx context.Context, duration time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(duration):
				return nil
			}
		}
	}
	// Individual production RPC attempts already have their own 60s slice.
	// A complete view shares this 300s total, clipped by any enclosing owner.
	operation, cancel := withTimeout(ctx, substrateRpcReadRetryTimeout)
	defer cancel()
	operation = WithFinalityReadOwnerContext(operation)
	operation = context.WithValue(operation, runtimeObservationReadOwnerKey{}, runtimeObservationReadOwner{api: chain.API, parent: ctx})
	delay := time.Second
	for {
		result, err := attempt(operation)
		if err == nil {
			return result, nil
		}
		if !runtimeObservationMayRepeat(err) || operation.Err() != nil {
			return empty, errors.Join(err, operation.Err())
		}
		if waitErr := wait(operation, delay); waitErr != nil {
			return empty, errors.Join(err, waitErr, operation.Err())
		}
		if delay < 5*time.Second {
			delay = min(delay*2, 5*time.Second)
		}
	}
}
