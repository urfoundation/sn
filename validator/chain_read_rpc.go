package validator

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/stabi"
)

// NewReleaseChainReadRPCContext binds the ordinary release reader to a caller-
// owned RPC transport. Public evidence uses a read-only recording facade over
// its already admitted endpoint, preserving its existing quota and batches.
// The endpoint's real chain ID is read here; every historical authority check
// remains in the ordinary methods. Closing this reader closes this RPC client.
func NewReleaseChainReadRPCContext(ctx context.Context, transport *rpc.Client, endpoint string, coordinator common.Address) (*ChainClient, error) {
	if ctx == nil || transport == nil || endpoint == "" || coordinator == (common.Address{}) {
		return nil, errors.New("release read RPC owner is incomplete")
	}
	client := ethclient.NewClient(transport)
	probe, cancel := context.WithTimeout(ctx, chainDialTimeout)
	defer cancel()
	chainID, err := client.ChainID(probe)
	if err := errors.Join(err, probe.Err(), ctx.Err()); err != nil {
		return nil, err
	}
	if chainID == nil || chainID.Sign() <= 0 { return nil, errors.New("release read RPC chain identity is absent") }
	result := &ChainClient{client: client, rpcUrl: endpoint, chainId: chainID, coordinator: stabi.NewSTCoordinator(), contractAddr: coordinator, release: true}
	result.contract = result.coordinator.Instance(client, coordinator)
	return result, nil
}
