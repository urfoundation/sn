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
	"github.com/urfoundation/sn/v2026/evmrpc"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Borrows one EVM client's transport for context-bound native reads only.
// Ownership and closure remain with the EVM operation.
type fleetEvmNativeReadClient struct{ client *rpc.Client }

// Forwards bounded reads without taking ownership of the transport.
func (self *fleetEvmNativeReadClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	return evmrpc.CallRuntimeReadContext(ctx, self.client, result, method, args...)
}

// Borrowed adapters preserve the submitting client's actual socket lifetime.
func (self *fleetEvmNativeReadClient) TransportGeneration() uint64 {
	if self == nil {
		return 0
	}
	generation, _ := evmrpc.RuntimeTransportGeneration(self.client)
	return generation
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
	purpose := crv4.FleetFrontierWrite
	if number != nil {
		purpose = crv4.FleetFrontierRead
	}
	return self.admitEvmPurpose(ctx, client, number, purpose)
}

// Status getters require only their read capability. Signing callbacks choose
// the distinct reviewed write purpose before any local consent or EVM send.
func (self *fleetMainnetRuntimeAuthority) admitEvmPurpose(ctx context.Context, client *ethclient.Client, number *big.Int, purpose crv4.FleetRuntimePurpose) error {
	if self == nil || ctx == nil || client == nil {
		return errors.New("mainnet EVM runtime authority is unavailable")
	}
	if generation, tracked := evmrpc.RuntimeTransportGeneration(client.Client()); !tracked || generation == 0 {
		return errors.New("mainnet EVM runtime transport owner is unavailable")
	}
	if number != nil {
		number = new(big.Int).Set(number)
	}
	genesis, err := types.NewHashFromHexString(self.GenesisHash)
	if err != nil {
		return err
	}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: &fleetEvmNativeReadClient{client: client.Client()}}, GenesisHash: genesis}
	var block types.Hash
	_, err = crv4.ReadRuntimeObservationContext(ctx, chain, func(ctx context.Context) (struct{}, error) {
		var chainId hexutil.Big
		if err := chain.API.Client.CallContext(ctx, &chainId, "eth_chainId"); err != nil {
			return struct{}{}, err
		}
		if !(*big.Int)(&chainId).IsUint64() || (*big.Int)(&chainId).Uint64() != self.EvmChainId {
			return struct{}{}, errors.New("mainnet EVM runtime authority chain id differs")
		}
		if block == (types.Hash{}) {
			if number == nil {
				block, err = crv4.FinalizedHeadContext(ctx, chain)
			} else {
				if !number.IsUint64() || number.Sign() <= 0 {
					return struct{}{}, errors.New("mainnet receipt native height is invalid")
				}
				err = chain.API.Client.CallContext(ctx, &block, "chain_getBlockHash", number.Uint64())
			}
			if err != nil {
				return struct{}{}, err
			}
		}
		if _, err := self.authenticateFor(ctx, chain, block, purpose); err != nil {
			return struct{}{}, err
		}
		if number != nil {
			height, _, err := chain.ReceiptHeaderAtContext(ctx, block)
			if err != nil {
				return struct{}{}, err
			}
			if height != number.Uint64() {
				return struct{}{}, errors.New("mainnet receipt native height changed")
			}
		}
		return struct{}{}, ctx.Err()
	})
	return err
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
		client, err := evmrpc.DialContext(ctx, endpoint)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := self.admitEvmPurpose(ctx, client, nil, crv4.FleetFrontierRead); err != nil {
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
	if err := self.admitEvm(ctx, client, nil); err != nil {
		client.Close()
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
	if err := self.admitEvmPurpose(ctx, client, nil, crv4.FleetFrontierRead); err != nil {
		return nil, "", err
	}
	return result, endpoint, nil
}
