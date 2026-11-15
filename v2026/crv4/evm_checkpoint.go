package crv4

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Both heads are independently selected and canonicalized. Equal numbers are
// allowed but never establish the mapping. The caller must separately verify
// EVMHash as the canonical EVM header at EVMNumber.
type EVMCheckpointQuery struct {
	GenesisHash  types.Hash `json:"genesis_hash"`
	NativeHash   types.Hash `json:"native_hash"`
	NativeNumber uint64     `json:"native_number"`
	EVMHash      types.Hash `json:"evm_hash"`
	EVMNumber    uint64     `json:"evm_number"`
}

type EVMCheckpointObservation struct {
	Query            EVMCheckpointQuery      `json:"query"`
	NativeParentHash types.Hash              `json:"native_parent_hash"`
	Runtime          RuntimeArtifactIdentity `json:"runtime"`
	ParentRuntime    RuntimeArtifactIdentity `json:"parent_runtime"`
}

// The reviewed runtime source 67dcf7f791dc495064c293f080a0702cb433e51e,
// vendor/frontier/frame/ethereum/src/lib.rs:236-245,368,490-495, writes the
// current Ethereum header and its U256-number/H256-hash mapping together during
// native on_finalize. First insertion therefore identifies execution at this
// native head, not a delayed history-map update. Authenticate both runtime
// artifacts before constructing keys; dial-time metadata has no authority.
func ReadEVMCheckpointAtContext(ctx context.Context, chain *Chain, query EVMCheckpointQuery, allowed ...RuntimeArtifactIdentity) (EVMCheckpointObservation, error) {
	if ctx == nil {
		return EVMCheckpointObservation{}, errors.New("EVM/native checkpoint caller context is absent")
	}
	if len(allowed) > maximumRuntimeMetadataArtifactsPerChain {
		return EVMCheckpointObservation{}, errors.New("EVM/native checkpoint runtime allowlist exceeds its bound")
	}
	allowed = append([]RuntimeArtifactIdentity(nil), allowed...)
	return readRuntimeObservation(ctx, chain, func(ctx context.Context) (EVMCheckpointObservation, error) {
		return readEvmCheckpointAttempt(ctx, chain, query, allowed...)
	})
}

// One attempt preserves the original query and rejects any partial result.
func readEvmCheckpointAttempt(ctx context.Context, chain *Chain, query EVMCheckpointQuery, allowed ...RuntimeArtifactIdentity) (result EVMCheckpointObservation, resultErr error) {
	empty := EVMCheckpointObservation{}
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil || query.GenesisHash == (types.Hash{}) || chain.GenesisHash != query.GenesisHash || query.NativeHash == (types.Hash{}) || query.EVMHash == (types.Hash{}) || query.NativeNumber == 0 || query.NativeNumber > math.MaxUint32 || query.EVMNumber == 0 || len(allowed) == 0 || len(allowed) > maximumRuntimeMetadataArtifactsPerChain {
		return empty, errors.New("EVM/native checkpoint authority is incomplete")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = EVMCheckpointObservation{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	versions := map[RuntimeVersionIdentity]bool{}
	for _, identity := range allowed {
		if _, err := canonicalRuntimeArtifactIdentity(identity); err != nil {
			return empty, err
		}
		if versions[identity.Version] {
			return empty, errors.New("EVM/native checkpoint runtime allowlist repeats a version")
		}
		versions[identity.Version] = true
	}
	genesis, err := validatorIdentityBlockHashAtContext(ctx, chain, 0)
	if err != nil {
		return empty, err
	}
	if genesis != query.GenesisHash {
		return empty, errors.New("EVM/native checkpoint genesis differs")
	}
	canonical, err := validatorIdentityBlockHashAtContext(ctx, chain, query.NativeNumber)
	if err != nil {
		return empty, err
	}
	if canonical != query.NativeHash {
		return empty, errors.New("EVM/native checkpoint is not canonical")
	}
	number, headerParent, err := chain.ReceiptHeaderAtContext(ctx, query.NativeHash)
	if err != nil {
		return empty, err
	}
	if number != query.NativeNumber || headerParent == (types.Hash{}) {
		return empty, errors.New("EVM/native checkpoint header differs")
	}
	parent, err := validatorIdentityBlockHashAtContext(ctx, chain, query.NativeNumber-1)
	if err != nil {
		return empty, err
	}
	if parent != headerParent {
		return empty, errors.New("EVM/native checkpoint parent differs")
	}
	parentNumber, _, err := chain.ReceiptHeaderAtContext(ctx, parent)
	if err != nil {
		return empty, err
	}
	if parentNumber != query.NativeNumber-1 {
		return empty, errors.New("EVM/native checkpoint parent height differs")
	}
	finalized, finalizedNumber, err := readFinalityReadWitnessContext(ctx, chain, query.NativeHash)
	if err != nil {
		return empty, err
	}
	if err := RetainFinalityReadWitnessContext(ctx, chain, query.NativeHash, finalized, finalizedNumber); err != nil {
		return empty, err
	}
	if finalizedNumber < query.NativeNumber {
		return empty, &ReceiptEvidenceUnavailableError{BlockHash: query.NativeHash, Field: "finalized head through retained EVM/native checkpoint"}
	}
	read := func(head types.Hash, absent bool) (RuntimeArtifactIdentity, error) {
		artifact, err := AuthenticateRuntimeArtifactAtContext(ctx, chain, head, allowed...)
		if err != nil {
			return RuntimeArtifactIdentity{}, err
		}
		// SCALE U256 is exactly 32 little-endian bytes. No candidate key or
		// candidate RPC method enters this storage lookup.
		arg := make([]byte, 32)
		binary.LittleEndian.PutUint64(arg, query.EVMNumber)
		key, err := types.CreateStorageKey(artifact.Metadata, "Ethereum", "BlockHash", arg)
		if err != nil {
			return RuntimeArtifactIdentity{}, err
		}
		raw := json.RawMessage("null")
		if err := chain.API.Client.CallContext(ctx, &raw, "state_getStorage", key.Hex(), head.Hex()); err != nil {
			return RuntimeArtifactIdentity{}, err
		}
		value, err := decodeValidatorIdentityHexResult(raw, 32, absent)
		if err != nil {
			return RuntimeArtifactIdentity{}, err
		}
		if absent {
			if len(value) != 0 && (len(value) != 32 || !bytes.Equal(value, make([]byte, 32))) {
				return RuntimeArtifactIdentity{}, errors.New("EVM block mapping already existed at the native parent")
			}
		} else if len(value) != 32 || !bytes.Equal(value, query.EVMHash[:]) {
			return RuntimeArtifactIdentity{}, errors.New("EVM block hash does not match its first native storage insertion")
		}
		return RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}, ctx.Err()
	}
	currentRuntime, err := read(query.NativeHash, false)
	if err != nil {
		return empty, fmt.Errorf("EVM/native checkpoint current: %w", err)
	}
	parentRuntime, err := read(parent, true)
	if err != nil {
		return empty, fmt.Errorf("EVM/native checkpoint parent: %w", err)
	}
	// Canonical hashes cannot establish that the opening finality witness is
	// still finalized after the dependent storage reads.
	closing, closingNumber, err := readFinalityReadWitnessContext(ctx, chain, query.NativeHash)
	if err != nil {
		return empty, err
	}
	for _, boundary := range []struct {
		hash   types.Hash
		number uint64
	}{{hash: query.NativeHash, number: number}, {hash: parent, number: parentNumber}, {hash: finalized, number: finalizedNumber}} {
		if err := chain.CheckCanonicalBlockAtContext(ctx, boundary.hash, boundary.number); err != nil {
			return empty, err
		}
	}
	if err := RetainFinalityReadWitnessContext(ctx, chain, query.NativeHash, closing, closingNumber); err != nil {
		return empty, err
	}
	if closingNumber < finalizedNumber {
		return empty, &ReceiptEvidenceUnavailableError{BlockHash: finalized, Field: "finalized head through original EVM/native checkpoint witness"}
	}
	return EVMCheckpointObservation{Query: query, NativeParentHash: parent, Runtime: currentRuntime, ParentRuntime: parentRuntime}, ctx.Err()
}
