// The optional native reader captures the pinned SDK's real trace response and
// parent :code proof. Each RPC has its own finite retry budget and cancellation.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Neither this specialized read nor the proof method joins the general RPC
// allowlist. Null, disabled, unsupported and SDK trace-error results fail closed.
func (self *rpcClient) safeHistoryNativeRead(ctx context.Context, method string, params []any, limit int, result any) error {
	if ctx == nil || self == nil || self.httpClient == nil || self.retryWindow <= 0 ||
		method != "state_traceBlock" && method != "state_getReadProof" || limit <= 0 || limit > 2*maximumSafeCurrentProofBytes+maxRpcReplyBytes {
		return errors.New("Safe native trace read is outside its bounded profile")
	}
	var raw json.RawMessage
	if err := self.callAdmittedRead(ctx, method, params, &raw, true, limit); err != nil {
		var rpcErr *rpcCallError
		if errors.As(err, &rpcErr) && (rpcErr.code == -32601 || rpcErr.code == -32602) {
			return fmt.Errorf("%w: %s: %v", errSafeHistoryArchiveUnavailable, method, err)
		}
		return err
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%w: %s returned no retained evidence", errSafeHistoryArchiveUnavailable, method)
	}
	if err := decodePlanJson(raw, result); err != nil {
		return fmt.Errorf("%w: Safe native %s: %v", errRpcIntegrity, method, err)
	}
	return ctx.Err()
}

// Trace capture extends a completed census without changing its interval. It
// proves the runtime bytes stored at every parent, not the node's replay of them.
// No partial trace interval or proof is returned after failure.
func (self *rpcClient) captureSafeHistoryNative(ctx context.Context, expected identityExpectation, scope safeHistoryScope) (result safeHistoryCapture, resultErr error) {
	if ctx == nil {
		return result, errors.New("Safe native trace requires a caller context")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = safeHistoryCapture{}
		}
	}()
	census, err := self.captureSafeHistory(ctx, expected, scope)
	if err != nil {
		return result, err
	}
	native := &safeHistoryNativeCapture{Schema: safeHistoryNativeSchema, SdkSource: safeCurrentStorageSdk, TraceAuthority: "owned-rpc-replay-assertion",
		Blocks: []safeHistoryNativeBlock{}, ParentRuntimeBytesVerified: true, KeyedTraceScopeMatched: true}
	remaining := maximumSafeHistoryNativeEncodedBytes
	consume := func(size int) error {
		if size > remaining {
			return errors.New("Safe native trace capture exceeds shared encoded witness byte bound")
		}
		remaining -= size
		return nil
	}
	for _, block := range census.Blocks {
		parent := safeCurrentStorageWitness{At: block.Witness.NativeHeader.ParentHash}
		if err := self.call(ctx, "chain_getHeader", []any{parent.At}, &parent.Header); err != nil {
			return result, err
		}
		if number, err := parent.Header.authenticate(parent.At); err != nil || number != block.Witness.Native.Number-1 {
			return result, errors.Join(errors.New("Safe native trace parent selection differs"), err)
		}
		var proof struct {
			At    string   `json:"at"`
			Proof []string `json:"proof"`
		}
		if err := self.safeHistoryNativeRead(ctx, "state_getReadProof", []any{[]string{runtimeCodeStorageKey}, parent.At},
			2*maximumSafeCurrentProofBytes+maxRpcReplyBytes, &proof); err != nil {
			return result, err
		}
		parent.At, parent.Nodes = proof.At, proof.Proof
		encoded, err := json.Marshal(parent)
		if err != nil {
			return result, err
		}
		if err := consume(len(encoded)); err != nil {
			return result, err
		}
		runtime, err := verifySafeHistoryParentRuntime(ctx, block, parent)
		if err != nil {
			return result, err
		}
		var raw json.RawMessage
		if err := self.safeHistoryNativeRead(ctx, "state_traceBlock", []any{block.Witness.Native.Hash, "state", safeHistoryNativeKeys(scope.Safe), ""},
			maximumSafeHistoryNativeReplyBytes, &raw); err != nil {
			return result, err
		}
		if err := consume(len(raw)); err != nil {
			return result, err
		}
		trace, err := decodeSafeHistoryNativeTrace(ctx, raw, scope.Safe, block)
		if err != nil {
			return result, err
		}
		native.Blocks = append(native.Blocks, safeHistoryNativeBlock{Native: block.Witness.Native, ParentRuntime: runtime, Trace: trace})
	}
	latest, err := self.safeHistoryHead(ctx, expected)
	if err != nil {
		return result, err
	}
	if latest.Number < census.LaterFinalized.Number {
		return result, errors.New("Safe native trace finalized head regressed")
	}
	// Canonical descendants commit the proved parent headers. Rechecking each
	// witness also catches an inconsistent owned route without relabeling proofs.
	for _, boundary := range []safeHistoryBoundary{census.InitialFinalized, census.LaterFinalized, scope.Through} {
		if err := self.safeHistoryCanonical(ctx, boundary); err != nil {
			return result, err
		}
	}
	for _, block := range census.Blocks {
		if err := self.safeHistoryCanonical(ctx, block.Witness.Native); err != nil {
			return result, err
		}
	}
	census.Schema, census.LaterFinalized, census.NativeTrace = safeHistoryNativeCaptureSchema, latest, native
	census.Unresolved = []string{
		"parent :code bytes are proven, but runtime source/build provenance, complete parent state and the node's execution are unproven",
		"state_traceBlock omits keyless ClearPrefix and storage-root events; native transaction commit/rollback boundaries are not traced",
		"keyed native traces do not prove complete internal EVM calls or reverted delegatecall/storage mutations",
		"deployment initialization, owner/module mapping history and complete Safe authority remain unproven",
		"pending overlay, provenance-policy approval and public submission remain unavailable; complete history requires a qualified node trace extension or independent full replay",
	}
	return census, nil
}
