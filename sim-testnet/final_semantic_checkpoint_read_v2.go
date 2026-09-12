//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/urfoundation/sn/crv4"
)

// The reviewed runtime uses the native height as an Ethereum-number search
// hint. Only exact first insertion of the independently canonical Ethereum
// hash proves the mapping; a coincident number never satisfies the reader.
func readFinalNativeCheckpointV2(ctx context.Context, native *crv4.Chain, evm ChainHead, netuid, uid uint16, hotkey [32]byte, runtime crv4.RuntimeArtifactIdentity) (result FinalNativeCheckpointV2, resultErr error) {
	if ctx == nil || native == nil || native.API == nil || native.API.Client == nil || evm.Number == 0 || evm.Number > math.MaxUint32 {
		return result, errors.New("native coverage checkpoint owner is incomplete")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = FinalNativeCheckpointV2{}
		}
	}()
	evmHash, err := gsrpctypes.NewHashFromHexString(evm.Hash)
	if err != nil {
		return result, err
	}
	var nativeHash gsrpctypes.Hash
	if err := native.API.Client.CallContext(ctx, &nativeHash, "chain_getBlockHash", evm.Number); err != nil {
		return result, err
	}
	result.Mapping, err = crv4.ReadEVMCheckpointAtContext(ctx, native, crv4.EVMCheckpointQuery{GenesisHash: native.GenesisHash, NativeHash: nativeHash, NativeNumber: evm.Number, EVMHash: evmHash, EVMNumber: evm.Number}, runtime)
	if err != nil {
		return result, err
	}
	result.Identity, err = crv4.ReadValidatorScheduleAtContext(ctx, native, crv4.ValidatorScheduleQuery{GenesisHash: native.GenesisHash, BlockHash: nativeHash, BlockNumber: result.Mapping.Query.NativeNumber, Netuid: netuid, Hotkey: hotkey, MaximumSubnetUIDs: math.MaxUint16}, runtime)
	if err != nil {
		return result, err
	}
	if result.Identity.Stake.Identity.UID != uid {
		return result, errors.New("native coverage checkpoint UID differs from original validator")
	}
	artifact, err := crv4.AuthenticateRuntimeArtifactAtContext(ctx, native, nativeHash, runtime)
	if err != nil {
		return result, err
	}
	own := *native
	own.Meta = artifact.Metadata
	state, err := own.EpochScheduleStateAtContext(ctx, netuid, nativeHash)
	if err != nil {
		return result, err
	}
	result.Schedule = *state
	result.RevealPeriodEpochs, err = own.RevealPeriodEpochsAtContext(ctx, netuid, nativeHash)
	if err != nil {
		return result, err
	}
	row, err := own.WeightsAtContext(ctx, netuid, uid, nativeHash)
	if err != nil {
		return result, err
	}
	if len(row) == 0 || len(row) > math.MaxUint16 {
		return result, errors.New("native coverage applied row is absent or unbounded")
	}
	key, err := gsrpctypes.CreateStorageKey(artifact.Metadata, crv4.PalletName, "LastUpdate", netuidArg(netuid))
	if err != nil {
		return result, err
	}
	var raw json.RawMessage
	if err := own.API.Client.CallContext(ctx, &raw, "state_getStorage", key.Hex(), nativeHash.Hex()); err != nil {
		return result, err
	}
	if len(raw) > 4+2*(4+8*math.MaxUint16) {
		return result, errors.New("native coverage LastUpdate exceeds runtime UID bound")
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return result, err
	}
	var updates []gsrpctypes.U64
	if err := codec.DecodeFromHex(encoded, &updates); err != nil {
		return result, err
	}
	canonicalUpdates, err := codec.EncodeToHex(updates)
	if err != nil || canonicalUpdates != encoded || len(updates) > math.MaxUint16 {
		return result, errors.New("native coverage LastUpdate is not canonical bounded SCALE")
	}
	if int(uid) >= len(updates) || updates[uid] == 0 {
		return result, errors.New("native coverage LastUpdate is absent")
	}
	result.Weights = FinalNativeWeightState{ValidatorUID: uid, ValidatorHotkey: fmt.Sprintf("0x%x", hotkey), LastUpdate: uint64(updates[uid]), Block: ChainHead{Number: result.Mapping.Query.NativeNumber, Hash: nativeHash.Hex()}}
	for _, pair := range row {
		result.Weights.UIDs = append(result.Weights.UIDs, uint16(pair.UID))
		result.Weights.Values = append(result.Weights.Values, uint16(pair.Value))
	}
	return result, nil
}
