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
func ReadEVMCheckpointAtContext(ctx context.Context, chain *Chain, query EVMCheckpointQuery, allowed ...RuntimeArtifactIdentity) (result EVMCheckpointObservation, resultErr error) {
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
	if err != nil || genesis != query.GenesisHash {
		return empty, errors.Join(errors.New("EVM/native checkpoint genesis differs"), err)
	}
	canonical, err := validatorIdentityBlockHashAtContext(ctx, chain, query.NativeNumber)
	if err != nil || canonical != query.NativeHash {
		return empty, errors.Join(errors.New("EVM/native checkpoint is not canonical"), err)
	}
	header, err := chain.HeaderAtContext(ctx, query.NativeHash)
	if err != nil || uint64(header.Number) != query.NativeNumber || header.ParentHash == (types.Hash{}) {
		return empty, errors.Join(errors.New("EVM/native checkpoint header differs"), err)
	}
	parent, err := validatorIdentityBlockHashAtContext(ctx, chain, query.NativeNumber-1)
	if err != nil || parent != header.ParentHash {
		return empty, errors.Join(errors.New("EVM/native checkpoint parent differs"), err)
	}
	parentHeader, err := chain.HeaderAtContext(ctx, parent)
	if err != nil || uint64(parentHeader.Number) != query.NativeNumber-1 {
		return empty, errors.Join(errors.New("EVM/native checkpoint parent height differs"), err)
	}
	finalized, err := FinalizedHeadContext(ctx, chain)
	if err != nil {
		return empty, err
	}
	finalizedHeader, err := chain.HeaderAtContext(ctx, finalized)
	if err != nil || uint64(finalizedHeader.Number) < query.NativeNumber {
		return empty, errors.Join(errors.New("EVM/native checkpoint is not finalized"), err)
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
	return EVMCheckpointObservation{Query: query, NativeParentHash: parent, Runtime: currentRuntime, ParentRuntime: parentRuntime}, ctx.Err()
}
