// Fleet EVM operations authenticate the native runtime on the actual EVM
// connection; a separately healthy native endpoint cannot authorize another
// network that merely advertises the same EVM chain id.
package miner

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Borrows one EVM client's transport for context-bound native reads only.
// Ownership and closure remain with the EVM operation.
type fleetEvmNativeReadClient struct{ client *rpc.Client }

// Forwards bounded reads without taking ownership of the transport.
func (self *fleetEvmNativeReadClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	return self.client.CallContext(ctx, result, method, args...)
}

// Admission has no background-context fallback.
func (self *fleetEvmNativeReadClient) Call(result any, method string, args ...any) error {
	return errors.New("fleet native admission requires a caller context")
}

// The borrowed adapter cannot acquire a subscription lifecycle.
func (self *fleetEvmNativeReadClient) Subscribe(context.Context, string, string, string, string, any, ...any) (*gsrpcgeth.ClientSubscription, error) {
	return nil, errors.New("fleet native admission cannot subscribe")
}

// Diagnostics identify the owner without retaining endpoint credentials.
func (self *fleetEvmNativeReadClient) URL() string { return "fleet-evm-runtime-admission" }

// The submitting operation closes its own connection.
func (self *fleetEvmNativeReadClient) Close() {}

// Frontier heights and native heights must retain the reviewed one-to-one
// mapping. Inclusion is checked at its exact height, not at the later head.
func (self *fleetMainnetRuntimeAuthority) admitEvm(ctx context.Context, client *ethclient.Client, number *big.Int) error {
	if self == nil || ctx == nil || client == nil {
		return errors.New("mainnet EVM runtime authority is unavailable")
	}
	chainId, err := client.ChainID(ctx)
	if err != nil {
		return err
	}
	if !chainId.IsUint64() || chainId.Uint64() != self.EvmChainId {
		return errors.New("mainnet EVM runtime authority chain id differs")
	}
	genesis, err := types.NewHashFromHexString(self.GenesisHash)
	if err != nil {
		return err
	}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: &fleetEvmNativeReadClient{client: client.Client()}}, GenesisHash: genesis}
	var block types.Hash
	if number == nil {
		block, err = crv4.FinalizedHeadContext(ctx, chain)
	} else {
		if !number.IsUint64() || number.Sign() <= 0 {
			return errors.New("mainnet receipt native height is invalid")
		}
		err = chain.API.Client.CallContext(ctx, &block, "chain_getBlockHash", number.Uint64())
	}
	if err != nil {
		return err
	}
	if _, err := self.authenticateAt(ctx, chain, block); err != nil {
		return err
	}
	if number != nil {
		header, err := chain.HeaderAtContext(ctx, block)
		if err != nil {
			return err
		}
		if uint64(header.Number) != number.Uint64() {
			return errors.New("mainnet receipt native height changed")
		}
	}
	return ctx.Err()
}

// Nil preserves the established testnet submission path without creating any
// production exception. The callback always uses the submitter's connection.
func (self *fleetMainnetRuntimeAuthority) evmAdmission() func(context.Context, *ethclient.Client, *big.Int) error {
	if self == nil {
		return nil
	}
	return self.admitEvm
}

// Endpoint failover is limited to failed transport construction. A reached
// wrong chain or artifact is a hard refusal rather than approval by fallback.
func (self *fleetMainnetRuntimeAuthority) dialEvm(ctx context.Context, endpoints []string) (*ethclient.Client, string, error) {
	var errs []error
	for _, endpoint := range endpoints {
		client, err := ethclient.DialContext(ctx, endpoint)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := self.admitEvm(ctx, client, nil); err != nil {
			client.Close()
			return nil, "", err
		}
		return client, endpoint, nil
	}
	return nil, "", fmt.Errorf("no mainnet fleet EVM endpoint: %w", errors.Join(errs...))
}

// Client/hotkey consent is signed only after the target network is admitted.
// Submission rechecks that selected endpoint on its own actual connection.
func (self *fleetMainnetRuntimeAuthority) prepareEvm(ctx context.Context, endpoints []string) ([]string, error) {
	if self == nil {
		return endpoints, nil
	}
	client, endpoint, err := self.dialEvm(ctx, endpoints)
	if err != nil {
		return nil, err
	}
	client.Close()
	return []string{endpoint}, nil
}

// Contract views and their runtime admission use the same EVM connection.
func (self *fleetMainnetRuntimeAuthority) coordinatorCall(ctx context.Context, manifest *protocol.FleetManifest, endpoints []string, calldata []byte) ([]byte, string, error) {
	if self == nil {
		return finalizedCoordinatorCall(ctx, manifest, endpoints, calldata)
	}
	client, endpoint, err := self.dialEvm(ctx, endpoints)
	if err != nil {
		return nil, "", err
	}
	defer client.Close()
	var result hexutil.Bytes
	if err := client.Client().CallContext(ctx, &result, "eth_call", map[string]any{"to": common.Address(manifest.Coordinator).Hex(), "data": hexutil.Encode(calldata)}, "finalized"); err != nil {
		return nil, "", err
	}
	// Recheck before a remote digest is eligible for local signing.
	if err := self.admitEvm(ctx, client, nil); err != nil {
		return nil, "", err
	}
	return result, endpoint, nil
}
