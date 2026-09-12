//go:build linux || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcclient "github.com/centrifuge/go-substrate-rpc-client/v4/client"
	gsrpcrpc "github.com/centrifuge/go-substrate-rpc-client/v4/rpc"
	gsrpcstate "github.com/centrifuge/go-substrate-rpc-client/v4/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

// The existing UID batch and ValueQuery readers use GSRPC's contextless
// convenience methods. This private facade binds those calls to this one
// capture and preserves the established finite decode bound. It owns no dial
// or upstream connection, and never changes the caller's metadata/RPC graph.
type finalNativeRewardReadClientV2 struct {
	gsrpcclient.Client
	ctx    context.Context
	budget finalV2RPCDecodeBudget
}

func (self *finalNativeRewardReadClientV2) Call(result any, method string, args ...any) error {
	if self == nil {
		return errors.New("historical native reward read owner is absent")
	}
	return self.CallContext(self.ctx, result, method, args...)
}

// GSRPC's CallWithBlockHash invokes CallContext with context.Background, so
// overriding Call alone does not bind its UID batch to the capture owner.
// Every invocation must stop for either caller, and its cancellation callback
// must join before this facade returns without closing the shared connection.
func (self *finalNativeRewardReadClientV2) CallContext(caller context.Context, result any, method string, args ...any) error {
	if self == nil || self.Client == nil || self.ctx == nil {
		return errors.New("historical native reward read owner is absent")
	}
	if caller == nil {
		return errors.New("historical native reward RPC caller is absent")
	}
	if err := errors.Join(self.ctx.Err(), caller.Err()); err != nil {
		return err
	}
	switch method {
	case "chain_getBlockHash", "chain_getHeader", "chain_getFinalizedHead", "state_getRuntimeVersion", "state_getStorageHash", "state_getMetadata", "state_getStorage", "state_queryStorageAt":
	default:
		return errors.New("historical native reward reader requested another method")
	}
	ctx, cancel := context.WithCancel(caller)
	joined := make(chan struct{})
	stop := context.AfterFunc(self.ctx, func() { defer close(joined); cancel() })
	defer func() {
		if !stop() {
			<-joined
		}
		cancel()
	}()
	raw := &finalV2RPCResult{maximum: uint64(maximumCampaignEvidenceRawFileBytes), budget: &self.budget}
	if err := self.Client.CallContext(ctx, &raw, method, args...); err != nil {
		return err
	}
	if raw == nil {
		raw = &finalV2RPCResult{maximum: uint64(maximumCampaignEvidenceRawFileBytes), budget: &self.budget}
		if err := raw.UnmarshalJSON([]byte("null")); err != nil {
			return err
		}
	}
	if err := errors.Join(ctx.Err(), self.ctx.Err()); err != nil {
		return err
	}
	return json.Unmarshal(raw.raw, result)
}

// Read the existing four UID-indexed reward surfaces at an exact canonical
// historical native state. This is an observation only: payout-event, stake
// mutation and repeated-native-boundary checks remain in the final verifier.
func readFinalNativeRewardAtV2(ctx context.Context, native *crv4.Chain, at ChainHead, netuid uint16, runtime crv4.RuntimeArtifactIdentity) (result *NativeRewardObservation, resultErr error) {
	if ctx == nil || native == nil || native.API == nil || native.API.Client == nil || native.GenesisHash == (types.Hash{}) || at.Number == 0 || at.Number > math.MaxUint32 {
		return nil, errors.New("historical native reward authority is incomplete")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash, err := types.NewHashFromHexString(at.Hash)
	if err != nil || hash == (types.Hash{}) {
		return nil, errors.Join(errors.New("historical native reward hash is invalid"), err)
	}
	client := &finalNativeRewardReadClientV2{Client: native.API.Client, ctx: ctx, budget: finalV2RPCDecodeBudget{remaining: uint64(maximumCampaignEvidenceRawFileBytes)}}
	own := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client, RPC: &gsrpcrpc.RPC{State: gsrpcstate.NewState(client)}}, GenesisHash: native.GenesisHash}
	var genesis, canonical types.Hash
	if err := own.API.Client.CallContext(ctx, &genesis, "chain_getBlockHash", uint64(0)); err != nil {
		return nil, err
	}
	if genesis != own.GenesisHash {
		return nil, errors.New("historical native reward genesis differs")
	}
	if err := own.API.Client.CallContext(ctx, &canonical, "chain_getBlockHash", at.Number); err != nil {
		return nil, err
	}
	if canonical != hash {
		return nil, errors.New("historical native reward head is not canonical")
	}
	header, err := own.HeaderAtContext(ctx, hash)
	if err != nil || header == nil || uint64(header.Number) != at.Number {
		return nil, errors.Join(errors.New("historical native reward header differs"), err)
	}
	finalized, err := crv4.FinalizedHeadContext(ctx, own)
	if err != nil {
		return nil, err
	}
	finalizedHeader, err := own.HeaderAtContext(ctx, finalized)
	if err != nil || finalizedHeader == nil || uint64(finalizedHeader.Number) < at.Number {
		return nil, errors.Join(errors.New("historical native reward head is not finalized"), err)
	}
	artifact, err := crv4.AuthenticateRuntimeArtifactAtContext(ctx, own, hash, runtime)
	if err != nil {
		return nil, err
	}
	own.Meta = artifact.Metadata
	read := func(name string, value any) error {
		key, err := types.CreateStorageKey(own.Meta, crv4.PalletName, name, netuidArg(netuid))
		if err != nil {
			return err
		}
		return readRequiredStorageAt(own, key, crv4.PalletName, name, value, hash)
	}
	var count types.U16
	if err := read("SubnetworkN", &count); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, errors.New("historical native reward UID census is empty")
	}
	var emission []types.U64
	var incentive, dividends []types.U16
	if err := read("Emission", &emission); err != nil {
		return nil, err
	}
	if err := read("Incentive", &incentive); err != nil {
		return nil, err
	}
	if err := read("Dividends", &dividends); err != nil {
		return nil, err
	}
	if len(emission) != int(count) || len(incentive) != int(count) || len(dividends) != int(count) {
		return nil, errors.New("historical native reward vectors differ from the exact UID census")
	}
	facts, err := readExistingUIDFactsAt(own, netuid, hash, SubnetTopologyFacts{UIDCount: uint16(count)})
	if err != nil {
		return nil, err
	}
	result, err = nativeRewardObservationFromFinalizedState(ChainHead{Number: at.Number, Hash: hash.Hex()}, emission, incentive, dividends, facts)
	if err != nil {
		return nil, err
	}
	if err := own.API.Client.CallContext(ctx, &canonical, "chain_getBlockHash", at.Number); err != nil {
		return nil, err
	}
	if canonical != hash {
		return nil, errors.New("historical native reward canonical head changed during capture")
	}
	return result, nil
}
