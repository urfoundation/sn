// The production archive adapter performs only bounded reads. Raw witnesses
// are emitted only after complete range and later-head continuity checks pass.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/urnetwork/server/v2026/strecovery"
)

var errSafeHistoryArchiveUnavailable = errors.New("Safe archive census requires unavailable raw archive capabilities")

// The proof boundary is explicit in every successful report, including empty
// Safe projections. Finality is an owned-RPC assertion; only bytes are proven.
type safeHistoryCapture struct {
	Schema                    string                    `json:"schema"`
	CodecSourceCommit         string                    `json:"codec_source_commit"`
	FinalityAuthority         string                    `json:"finality_authority"`
	NativeChain               string                    `json:"native_chain"`
	GenesisHash               string                    `json:"genesis_hash"`
	EvmChainId                uint64                    `json:"evm_chain_id"`
	Scope                     safeHistoryScope          `json:"scope"`
	InitialFinalized          safeHistoryBoundary       `json:"initial_finalized"`
	LaterFinalized            safeHistoryBoundary       `json:"later_finalized"`
	Blocks                    []safeHistoryBlock        `json:"blocks"`
	ByteCommitmentsVerified   bool                      `json:"byte_commitments_verified"`
	RuntimeSourceProven       bool                      `json:"runtime_source_proven"`
	NativeExecutionVerified   bool                      `json:"native_execution_verified"`
	InternalExecutionVerified bool                      `json:"internal_execution_verified"`
	DeploymentHistoryVerified bool                      `json:"deployment_history_verified"`
	CompletePendingVerified   bool                      `json:"complete_pending_verified"`
	SendAuthorized            bool                      `json:"send_authorized"`
	Unresolved                []string                  `json:"unresolved"`
	NativeTrace               *safeHistoryNativeCapture `json:"native_trace,omitempty"`
}

// This is a content digest, not a signature or provenance-policy approval.
type safeHistoryCaptureEnvelope struct {
	Capture     safeHistoryCapture `json:"capture"`
	ContentHash string             `json:"content_hash"`
}

// Raw methods are deliberately isolated from the ordinary RPC whitelist.
// Each invocation owns its retry window under the caller's cancellation.
func (self *rpcClient) callSafeHistoryRead(ctx context.Context, method string, hash string, result any) error {
	limit := 16 * 1024 * 1024
	switch method {
	case "debug_getRawHeader":
		limit = 2*maximumMappingHeaderBytes + 1024
	case "debug_getRawBlock", "debug_getRawReceipts":
	default:
		return errors.New("RPC method is outside the Safe archive read profile")
	}
	if !rootCanonicalHash(hash) {
		return errors.New("Safe archive read requires an exact EVM block hash")
	}
	var raw json.RawMessage
	err := self.callAdmittedRead(ctx, method, []any{map[string]any{"blockHash": hash, "requireCanonical": true}}, &raw, true, limit)
	if err != nil {
		var rpcErr *rpcCallError
		if errors.As(err, &rpcErr) && (rpcErr.code == -32601 || rpcErr.code == -32602) {
			return fmt.Errorf("%w: %s: %v", errSafeHistoryArchiveUnavailable, method, err)
		}
		return err
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("%w: %s has no retained block", errSafeHistoryArchiveUnavailable, method)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("%w: Safe archive %s: %v", errRpcIntegrity, method, err)
	}
	return nil
}

// Header self-hashing and canonical-by-number checks retain the actual finality
// observation. No runtime version or source provenance is invented for it.
func (self *rpcClient) safeHistoryHead(ctx context.Context, expected identityExpectation) (safeHistoryBoundary, error) {
	chain := rootCanonicalChain{client: self, expected: expected}
	if err := chain.network(ctx); err != nil {
		return safeHistoryBoundary{}, err
	}
	var hash string
	if err := self.call(ctx, "chain_getFinalizedHead", []any{}, &hash); err != nil {
		return safeHistoryBoundary{}, err
	}
	_, number, err := chain.header(ctx, hash)
	if err != nil {
		return safeHistoryBoundary{}, err
	}
	boundary := safeHistoryBoundary{Number: number, Hash: hash}
	if err := self.safeHistoryCanonical(ctx, boundary); err != nil {
		return safeHistoryBoundary{}, err
	}
	return boundary, nil
}

// A later finalized head may advance indefinitely at normal cadence. Only the
// retained boundary's canonical identity must remain unchanged.
func (self *rpcClient) safeHistoryCanonical(ctx context.Context, boundary safeHistoryBoundary) error {
	var hash string
	if err := self.call(ctx, "chain_getBlockHash", []any{boundary.Number}, &hash); err != nil {
		return err
	}
	if hash != boundary.Hash {
		return fmt.Errorf("%w: Safe archive retained boundary is no longer canonical", errRpcIntegrity)
	}
	return nil
}

// The interval is bounded by count and a shared encoded witness budget. There
// is no aggregate read deadline: each RPC renews its own bounded retry allowance.
// Native bodies and EVM parents form complete consecutive chains within scope.
func (self *rpcClient) captureSafeHistory(ctx context.Context, expected identityExpectation, scope safeHistoryScope) (result safeHistoryCapture, resultErr error) {
	if ctx == nil || self == nil || self.httpClient == nil || self.retryWindow <= 0 || expected.NativeChain == "" || expected.EvmChainId != mainnetEvmChainId || !rootCanonicalHash(expected.GenesisHash) {
		return safeHistoryCapture{}, errors.New("Safe archive capture requires a context, owned client, and explicit mainnet identity")
	}
	if err := scope.validate(); err != nil {
		return safeHistoryCapture{}, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = safeHistoryCapture{}
		}
	}()
	initial, err := self.safeHistoryHead(ctx, expected)
	if err != nil {
		return result, err
	}
	if initial.Number < scope.Through.Number {
		return result, errors.New("Safe archive scope is beyond the observed finalized head")
	}
	if err := self.safeHistoryCanonical(ctx, scope.Through); err != nil {
		return result, err
	}
	remaining := maximumSafeHistoryEncodedBytes
	consume := func(size int) error {
		if size > remaining {
			return errors.New("Safe archive capture exceeds shared encoded witness byte bound")
		}
		remaining -= size
		return nil
	}
	chain := rootCanonicalChain{client: self}
	boundary := scope.Through
	blocks := []safeHistoryBlock{}
	for {
		header, number, err := chain.header(ctx, boundary.Hash)
		if err != nil || number != boundary.Number {
			return result, errors.Join(errors.New("Safe archive native ancestry number differs"), err)
		}
		body, err := chain.body(ctx, boundary.Hash, header)
		if err != nil {
			return result, err
		}
		headerBytes, err := json.Marshal(header)
		if err != nil {
			return result, err
		}
		if err := consume(len(headerBytes)); err != nil {
			return result, err
		}
		witness := safeHistoryWitness{Native: boundary, NativeHeader: header, Extrinsics: []string{}}
		for _, raw := range body {
			if err := consume(2 + 2*len(raw)); err != nil {
				return result, err
			}
			witness.Extrinsics = append(witness.Extrinsics, "0x"+hex.EncodeToString(raw))
		}
		postLog, err := finalizedFrontierPostLog(header)
		if err != nil {
			return result, err
		}
		if err := self.callSafeHistoryRead(ctx, "debug_getRawHeader", postLog.BlockHash, &witness.EvmHeader); err != nil {
			return result, err
		}
		if err := consume(len(witness.EvmHeader)); err != nil {
			return result, err
		}
		if err := self.callSafeHistoryRead(ctx, "debug_getRawBlock", postLog.BlockHash, &witness.EvmBlock); err != nil {
			return result, err
		}
		if err := consume(len(witness.EvmBlock)); err != nil {
			return result, err
		}
		if err := self.callSafeHistoryRead(ctx, "debug_getRawReceipts", postLog.BlockHash, &witness.EvmReceipts); err != nil {
			return result, err
		}
		if len(witness.EvmReceipts) > maximumMappingTransactionHashes {
			return result, errors.New("Safe archive receipt vector exceeds count bound")
		}
		for _, receipt := range witness.EvmReceipts {
			if err := consume(len(receipt)); err != nil {
				return result, err
			}
		}
		block, err := authenticateSafeHistoryBlock(ctx, scope.Safe, witness)
		if err != nil {
			return result, err
		}
		if len(blocks) > 0 {
			child := blocks[len(blocks)-1]
			if block.Evm.Number == ^uint64(0) || child.Evm.Number != block.Evm.Number+1 || child.EvmParent != block.Evm.Hash {
				return result, errors.New("Safe archive EVM ancestry number or parent differs")
			}
		}
		blocks = append(blocks, block)
		if boundary.Number == scope.From.Number {
			if boundary.Hash != scope.From.Hash {
				return result, errors.New("Safe archive native ancestry has another pinned start")
			}
			break
		}
		boundary = safeHistoryBoundary{Number: boundary.Number - 1, Hash: header.ParentHash}
	}
	later, err := self.safeHistoryHead(ctx, expected)
	if err != nil {
		return result, err
	}
	if later.Number < initial.Number {
		return result, errors.New("Safe archive finalized head regressed")
	}
	for _, retained := range []safeHistoryBoundary{initial, scope.Through} {
		if err := self.safeHistoryCanonical(ctx, retained); err != nil {
			return result, err
		}
	}
	slices.Reverse(blocks)
	return safeHistoryCapture{Schema: safeHistoryCensusSchema, CodecSourceCommit: frontierMappingSourceCommit, FinalityAuthority: "owned-rpc-assertion",
		NativeChain: expected.NativeChain, GenesisHash: expected.GenesisHash, EvmChainId: expected.EvmChainId, Scope: scope,
		InitialFinalized: initial, LaterFinalized: later, Blocks: blocks, ByteCommitmentsVerified: true,
		Unresolved: []string{"runtime source and native hook execution are unproven", "internal calls and reverted delegatecall/storage mutations are not visible in complete receipt vectors",
			"deployment initialization, owner/module mapping history and complete Safe authority remain unproven", "pending overlay, provenance-policy approval and public submission remain unavailable"}}, nil
}

// The size-limited report is sealed only after every commitment and continuity
// check. The domain prevents confusing its digest with a Safe history approval.
func sealSafeHistoryCapture(capture safeHistoryCapture) (safeHistoryCaptureEnvelope, error) {
	if err := capture.Scope.validate(); err != nil {
		return safeHistoryCaptureEnvelope{}, err
	}
	if capture.Schema != safeHistoryCensusSchema && capture.Schema != safeHistoryNativeCaptureSchema || !capture.ByteCommitmentsVerified || capture.FinalityAuthority != "owned-rpc-assertion" ||
		capture.RuntimeSourceProven || capture.NativeExecutionVerified || capture.InternalExecutionVerified || capture.DeploymentHistoryVerified || capture.CompletePendingVerified || capture.SendAuthorized ||
		len(capture.Blocks) != int(capture.Scope.Through.Number-capture.Scope.From.Number+1) {
		return safeHistoryCaptureEnvelope{}, errors.New("Safe archive census cannot seal a history, execution or send authority claim")
	}
	if capture.Schema == safeHistoryCensusSchema && capture.NativeTrace != nil || capture.Schema == safeHistoryNativeCaptureSchema && capture.NativeTrace == nil {
		return safeHistoryCaptureEnvelope{}, errors.New("Safe archive native trace schema differs from retained evidence")
	}
	if native := capture.NativeTrace; native != nil {
		if native.Schema != safeHistoryNativeSchema || native.SdkSource != safeCurrentStorageSdk || native.TraceAuthority != "owned-rpc-replay-assertion" ||
			!native.ParentRuntimeBytesVerified || !native.KeyedTraceScopeMatched || native.CompleteParentStateVerified || native.ClearPrefixCoverageVerified ||
			native.RollbackCoverageVerified || native.StorageRootCoverageVerified || native.InnerEvmCoverageVerified || native.TraceCompletenessVerified || len(native.Blocks) != len(capture.Blocks) {
			return safeHistoryCaptureEnvelope{}, errors.New("Safe archive native trace cannot seal complete storage, execution or history coverage")
		}
		for i, block := range native.Blocks {
			if block.Native != capture.Blocks[i].Witness.Native || block.ParentRuntime.Witness.At != capture.Blocks[i].Witness.NativeHeader.ParentHash ||
				!rootCanonicalHash(block.ParentRuntime.CodeHash) || block.ParentRuntime.Bytes <= 0 || block.ParentRuntime.Bytes > maximumRuntimeSnapshotCodeBytes {
				return safeHistoryCaptureEnvelope{}, errors.New("Safe archive native trace interval or runtime evidence differs")
			}
		}
	}
	raw, err := json.Marshal(capture)
	if err != nil {
		return safeHistoryCaptureEnvelope{}, err
	}
	// Projections may repeat at most the committed EVM inputs/logs once.
	maximum := 2*maximumSafeHistoryEncodedBytes + strecovery.MaximumReceiptBlockCensusEncodedBytes
	if capture.NativeTrace != nil {
		maximum += maximumSafeHistoryNativeEncodedBytes
	}
	if len(raw) > maximum {
		return safeHistoryCaptureEnvelope{}, errors.New("Safe archive report exceeds sealed output bound")
	}
	digest := sha256.Sum256(append([]byte(capture.Schema+"\x00"), raw...))
	return safeHistoryCaptureEnvelope{Capture: capture, ContentHash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}
