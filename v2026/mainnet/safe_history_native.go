// Native traces retain only the keyed events exposed by the pinned SDK. Parent
// runtime proofs authenticate bytes, never trace completeness or Safe authority.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/crypto/blake2b"
)

const safeHistoryNativeSchema = "urnetwork-mainnet-safe-archive-native-trace-v1"
const safeHistoryNativeCaptureSchema = "urnetwork-mainnet-safe-archive-census-native-trace-v1"
const maximumSafeHistoryNativeReplyBytes = 16 * 1024 * 1024
const maximumSafeHistoryNativeEncodedBytes = 64 * 1024 * 1024
const maximumSafeHistoryNativeEvents = 65536
const maximumSafeHistoryNativeSpans = 16384

// These fields mirror sp_rpc::tracing at the pinned SDK revision. In
// particular block hashes and storage keys use unprefixed lowercase hex.
type safeHistoryNativeSpan struct {
	Id       uint64  `json:"id"`
	ParentId *uint64 `json:"parentId"`
	Name     string  `json:"name"`
	Target   string  `json:"target"`
	Wasm     bool    `json:"wasm"`
}

// Values are the SDK's recorded strings, including its debug value encoding.
// Their ordering does not supply omitted transaction or rollback boundaries.
type safeHistoryNativeEvent struct {
	Target string `json:"target"`
	Data   struct {
		StringValues map[string]string `json:"stringValues"`
	} `json:"data"`
	ParentId *uint64 `json:"parentId"`
}

// Arrays must be explicit even when empty. Empty filtered output makes no
// claim about native hooks, prefix removals, or internal EVM effects.
type safeHistoryNativeTrace struct {
	BlockHash      string                   `json:"blockHash"`
	ParentHash     string                   `json:"parentHash"`
	TracingTargets string                   `json:"tracingTargets"`
	StorageKeys    string                   `json:"storageKeys"`
	Methods        string                   `json:"methods"`
	Spans          []safeHistoryNativeSpan  `json:"spans"`
	Events         []safeHistoryNativeEvent `json:"events"`
}

// The header supplies the root. The code hash is derived from the complete
// :code value, which remains in the retained raw StorageProof nodes.
type safeHistoryParentRuntime struct {
	Witness  safeCurrentStorageWitness `json:"witness"`
	CodeHash string                    `json:"code_blake2_256"`
	Bytes    int                       `json:"code_bytes"`
}

// The trace is an owned-node replay assertion at this exact block and parent.
// Its parent runtime proof does not authenticate the trace's execution.
type safeHistoryNativeBlock struct {
	Native        safeHistoryBoundary      `json:"native"`
	ParentRuntime safeHistoryParentRuntime `json:"parent_runtime"`
	Trace         safeHistoryNativeTrace   `json:"trace"`
}

// Separate false coverage fields prevent a successful trace or code proof
// from becoming the full-history capability required by successor submission.
type safeHistoryNativeCapture struct {
	Schema                      string                   `json:"schema"`
	SdkSource                   string                   `json:"sdk_source"`
	TraceAuthority              string                   `json:"trace_authority"`
	Blocks                      []safeHistoryNativeBlock `json:"blocks"`
	ParentRuntimeBytesVerified  bool                     `json:"parent_runtime_bytes_verified"`
	KeyedTraceScopeMatched      bool                     `json:"keyed_trace_scope_matched"`
	CompleteParentStateVerified bool                     `json:"complete_parent_state_verified"`
	ClearPrefixCoverageVerified bool                     `json:"clear_prefix_coverage_verified"`
	RollbackCoverageVerified    bool                     `json:"rollback_coverage_verified"`
	StorageRootCoverageVerified bool                     `json:"storage_root_coverage_verified"`
	InnerEvmCoverageVerified    bool                     `json:"inner_evm_coverage_verified"`
	TraceCompletenessVerified   bool                     `json:"trace_completeness_verified"`
}

// Prefixes bound the response to this Safe's native code/storage plus runtime
// changes and extrinsic-index observations. No filter can reveal keyless events.
func safeHistoryNativeKeys(safe string) string {
	keys := []string{runtimeCodeStorageKey[2:], hex.EncodeToString([]byte(":extrinsic_index"))}
	for _, item := range []string{"AccountCodes", "AccountCodesMetadata", "AccountStorages"} {
		keys = append(keys, hex.EncodeToString(safeCurrentNativeAccountKey(item, common.HexToAddress(safe))))
	}
	return strings.Join(keys, ",")
}

// Byte bounds precede JSON allocation; explicit fields exclude null/default
// substitutions. Unknown SDK response shapes remain unavailable evidence.
func decodeSafeHistoryNativeTrace(ctx context.Context, raw []byte, safe string, block safeHistoryBlock) (safeHistoryNativeTrace, error) {
	var result safeHistoryNativeTrace
	if ctx == nil || len(raw) == 0 || len(raw) > maximumSafeHistoryNativeReplyBytes {
		return result, errors.New("Safe native trace context or reply bound differs")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var envelope map[string]json.RawMessage
	if err := decodePlanJson(raw, &envelope); err != nil || len(envelope) != 1 {
		return result, errors.Join(errors.New("Safe native trace response is not one explicit SDK variant"), err)
	}
	if failure, exists := envelope["traceError"]; exists {
		var reason struct {
			Error string `json:"error"`
		}
		if err := decodePlanJson(failure, &reason); err != nil || reason.Error == "" {
			return result, errors.New("Safe native trace error response is malformed")
		}
		return result, fmt.Errorf("%w: state_traceBlock returned a trace error", errSafeHistoryArchiveUnavailable)
	}
	var required map[string]json.RawMessage
	if err := decodePlanJson(envelope["blockTrace"], &required); err != nil {
		return result, err
	}
	for _, name := range []string{"blockHash", "parentHash", "tracingTargets", "storageKeys", "methods", "spans", "events"} {
		if value, exists := required[name]; !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return result, errors.New("Safe native trace omits an explicit SDK field: " + name)
		}
	}
	if err := decodePlanJson(envelope["blockTrace"], &result); err != nil {
		return safeHistoryNativeTrace{}, err
	}
	if err := validateSafeHistoryNativeTrace(ctx, result, safe, block); err != nil {
		return safeHistoryNativeTrace{}, err
	}
	return result, nil
}

// Matching hashes/filter echoes verifies the request scope, not the truth or
// completeness of replay. Missing contextual parents are preserved, not inferred.
func validateSafeHistoryNativeTrace(ctx context.Context, trace safeHistoryNativeTrace, safe string, block safeHistoryBlock) error {
	if ctx == nil || !rootCanonicalHash(block.Witness.Native.Hash) || !rootCanonicalHash(block.Witness.NativeHeader.ParentHash) ||
		trace.BlockHash != block.Witness.Native.Hash[2:] || trace.ParentHash != block.Witness.NativeHeader.ParentHash[2:] ||
		trace.TracingTargets != "state" || trace.StorageKeys != safeHistoryNativeKeys(safe) || trace.Methods != "" ||
		trace.Spans == nil || trace.Events == nil || len(trace.Spans) > maximumSafeHistoryNativeSpans || len(trace.Events) > maximumSafeHistoryNativeEvents {
		return errors.New("Safe native trace block, parent, filters or vector bounds differ")
	}
	ids := map[uint64]bool{}
	for _, span := range trace.Spans {
		if err := ctx.Err(); err != nil {
			return err
		}
		if span.Id == 0 || ids[span.Id] || span.ParentId != nil && (*span.ParentId == 0 || *span.ParentId == span.Id) ||
			span.Name == "" || len(span.Name) > 1024 || !strings.HasPrefix(span.Target, "state") || len(span.Target) > 1024 {
			return errors.New("Safe native trace span identity or target differs")
		}
		ids[span.Id] = true
	}
	prefixes := strings.Split(trace.StorageKeys, ",")
	for _, event := range trace.Events {
		if err := ctx.Err(); err != nil {
			return err
		}
		values := event.Data.StringValues
		key, method := values["key"], values["method"]
		if !strings.HasPrefix(event.Target, "state") || len(event.Target) > 1024 || event.ParentId != nil && *event.ParentId == 0 ||
			len(values) == 0 || len(values) > 32 || key == "" || len(key) > 2*maximumSafeCurrentStorageKeyBytes || len(key)%2 != 0 ||
			method == "" || len(method) > 128 {
			return errors.New("Safe native trace keyed event shape differs")
		}
		for _, value := range key {
			if !(value >= '0' && value <= '9') && !(value >= 'a' && value <= 'f') {
				return errors.New("Safe native trace storage key is not canonical SDK hex")
			}
		}
		if !slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(key, prefix) }) {
			return errors.New("Safe native trace event is outside the exact requested storage prefixes")
		}
		for name, value := range values {
			if name == "" || len(name) > 128 || len(value) > maximumSafeHistoryNativeReplyBytes {
				return errors.New("Safe native trace event value exceeds bound")
			}
		}
	}
	return ctx.Err()
}

// A parent :code point proof says nothing about other state, runtime build
// provenance, overrides used by the node, or whether the trace executed it.
func verifySafeHistoryParentRuntime(ctx context.Context, block safeHistoryBlock, witness safeCurrentStorageWitness) (safeHistoryParentRuntime, error) {
	var result safeHistoryParentRuntime
	if ctx == nil || block.Witness.Native.Number == 0 || witness.At != block.Witness.NativeHeader.ParentHash {
		return result, errors.New("Safe native trace runtime proof selects another parent")
	}
	number, err := witness.Header.authenticate(witness.At)
	if err != nil || number != block.Witness.Native.Number-1 {
		return result, errors.Join(errors.New("Safe native trace runtime parent header differs"), err)
	}
	trie, err := newSafeCurrentStorageTrie(ctx, witness.Nodes)
	if err != nil {
		return result, err
	}
	code, present, err := trie.read(ctx, [32]byte(common.HexToHash(witness.Header.StateRoot)), []byte(":code"))
	if err != nil || !present || len(code) == 0 || len(code) > maximumRuntimeSnapshotCodeBytes {
		return result, errors.Join(errors.New("Safe native trace parent runtime bytes are unproven"), err)
	}
	return safeHistoryParentRuntime{Witness: witness, CodeHash: common.Hash(blake2b.Sum256(code)).Hex(), Bytes: len(code)}, ctx.Err()
}
