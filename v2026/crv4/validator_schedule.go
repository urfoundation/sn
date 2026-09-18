// Historical ordinary cuts identify the signing hotkey, not its earlier
// activation UID. This reader resolves the actual registration and subnet
// epoch at one finalized native block using only that block's metadata.
package crv4

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// No UID is supplied: an old ledger UID must not select current registration.
// The independent block and runtime pins retain the identity reader's bounds.
type ValidatorScheduleQuery struct {
	GenesisHash       types.Hash
	BlockHash         types.Hash
	BlockNumber       uint64
	Netuid            uint16
	Hotkey            [32]byte
	MaximumSubnetUIDs uint32
}

// All fields are value-owned observations from one native block. Eligibility
// and a caller's expected subnet epoch remain explicit outer comparisons.
type ValidatorScheduleObservation struct {
	Stake            ValidatorStakeObservation
	SubnetEpochIndex uint64
}

// Resolves Uids through authenticated historical metadata, then executes the
// real bidirectional registration, finality and calculated-stake reader. No
// dial-time metadata, EVM block number or activation UID participates.
func ReadValidatorScheduleAtContext(ctx context.Context, chain *Chain, query ValidatorScheduleQuery, allowed ...RuntimeArtifactIdentity) (result ValidatorScheduleObservation, resultErr error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil {
		return result, errors.New("validator schedule context is unavailable")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = ValidatorScheduleObservation{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if query.GenesisHash == (types.Hash{}) || chain.GenesisHash != query.GenesisHash || query.BlockHash == (types.Hash{}) || query.BlockNumber == 0 || query.BlockNumber > math.MaxUint32 || query.Netuid == 0 || query.Hotkey == ([32]byte{}) || query.MaximumSubnetUIDs == 0 || query.MaximumSubnetUIDs > math.MaxUint16 {
		return result, errors.New("validator schedule chain, block, hotkey or census bounds are invalid")
	}
	if len(allowed) == 0 || len(allowed) > maximumRuntimeMetadataArtifactsPerChain {
		return result, errors.New("validator schedule runtime allowlist is invalid")
	}
	// RPC callbacks cannot change the policy used by the later real readers.
	allowed = slices.Clone(allowed)
	versions := map[RuntimeVersionIdentity]bool{}
	for _, identity := range allowed {
		if _, err := canonicalRuntimeArtifactIdentity(identity); err != nil {
			return result, err
		}
		if versions[identity.Version] {
			return result, errors.New("validator schedule runtime allowlist duplicates a version")
		}
		versions[identity.Version] = true
	}
	artifact, err := AuthenticateRuntimeArtifactAtContext(ctx, chain, query.BlockHash, allowed...)
	if err != nil {
		return result, err
	}
	read := func(name string, maximum int, args ...[]byte) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key, err := types.CreateStorageKey(artifact.Metadata, PalletName, name, args...)
		if err != nil {
			return nil, fmt.Errorf("validator schedule %s key: %w", name, err)
		}
		raw := json.RawMessage("null")
		if err := chain.API.Client.CallContext(ctx, &raw, "state_getStorage", key.Hex(), query.BlockHash.Hex()); err != nil {
			return nil, fmt.Errorf("validator schedule %s read: %w", name, err)
		}
		value, err := decodeValidatorIdentityHexResult(raw, maximum, false)
		if err != nil || len(value) != maximum {
			return nil, errors.Join(fmt.Errorf("validator schedule %s is not an exact %d-byte value", name, maximum), err)
		}
		return value, nil
	}
	netuid := encodeNetuid(query.Netuid)
	uid, err := read("Uids", 2, netuid, query.Hotkey[:])
	if err != nil {
		return result, err
	}
	resolvedUID := binary.LittleEndian.Uint16(uid)
	if uint32(resolvedUID) >= query.MaximumSubnetUIDs {
		return result, errors.New("validator schedule resolved UID exceeds the independent census bound")
	}
	stake, err := ReadValidatorStakeAtContext(ctx, chain, ValidatorIdentityQuery{
		GenesisHash: query.GenesisHash, BlockHash: query.BlockHash, BlockNumber: query.BlockNumber,
		Netuid: query.Netuid, UID: resolvedUID, MaximumSubnetUIDs: query.MaximumSubnetUIDs,
	}, allowed...)
	if err != nil {
		return result, err
	}
	if stake.Identity.Hotkey != query.Hotkey || stake.Identity.Runtime != (RuntimeArtifactIdentity{Version: artifact.Version, CodeHash: artifact.CodeHash, MetadataHash: artifact.MetadataHash}) {
		return result, errors.New("validator schedule hotkey or runtime changed during identity observation")
	}
	epoch, err := read("SubnetEpochIndex", 8, netuid)
	if err != nil {
		return result, err
	}
	// A changed reverse mapping or canonical hash cannot publish a mixed view.
	recheckedUID, err := read("Uids", 2, netuid, query.Hotkey[:])
	if err != nil || binary.LittleEndian.Uint16(recheckedUID) != resolvedUID {
		return result, errors.Join(errors.New("validator schedule registration changed during observation"), err)
	}
	canonical, err := validatorIdentityBlockHashAtContext(ctx, chain, query.BlockNumber)
	if err != nil || canonical != query.BlockHash {
		return result, errors.Join(errors.New("validator schedule canonical block changed during observation"), err)
	}
	return ValidatorScheduleObservation{Stake: stake, SubnetEpochIndex: binary.LittleEndian.Uint64(epoch)}, ctx.Err()
}
