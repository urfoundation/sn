// Fresh identity replies are compared before any later read can obscure a
// completed contradiction. Missing replies supply no identity authority.
package validator

import (
	"context"
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// A null or empty identity has no observed value. Other invalid JSON remains
// a decoding failure; a completed nonempty string must match independent pins.
func readRuntimeIdentityString(ctx context.Context, native *crv4.Chain, field, method string) (string, error) {
	var value *string
	if err := native.API.Client.CallContext(ctx, &value, method); err != nil {
		return "", err
	}
	if value == nil || *value == "" {
		return "", &crv4.ReceiptEvidenceUnavailableError{Field: field}
	}
	return *value, nil
}

// All callers have independently admitted this exact route and authority.
// Keep the caller's original deadline and compare completed bytes before late
// cancellation. These reads grant neither a mutable runtime nor write authority.
func readRuntimeNetworkIdentity(ctx context.Context, native *crv4.Chain, expectedName string, expectedGenesis types.Hash) (types.Hash, error) {
	if ctx == nil || native == nil || native.API == nil || native.API.Client == nil || expectedName == "" || expectedGenesis == (types.Hash{}) {
		return types.Hash{}, errors.New("runtime network identity owner or independent authority is absent")
	}
	if native.GenesisHash != expectedGenesis {
		return types.Hash{}, errors.Join(errors.New("runtime network identity connection genesis differs from independent authority"), ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return types.Hash{}, err
	}
	name, err := readRuntimeIdentityString(ctx, native, "native chain name", "system_chain")
	if err != nil {
		return types.Hash{}, errors.Join(err, ctx.Err())
	}
	if name != expectedName {
		return types.Hash{}, errors.Join(errors.New("runtime network identity native chain name differs from independent authority"), ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return types.Hash{}, err
	}
	genesis, err := readRuntimeWitnessHash(ctx, native, "genesis hash", "chain_getBlockHash", uint64(0))
	if err != nil {
		return types.Hash{}, errors.Join(err, ctx.Err())
	}
	if genesis != expectedGenesis {
		return types.Hash{}, errors.Join(errors.New("runtime network identity fresh genesis differs from independent authority"), ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return types.Hash{}, err
	}
	evmChainId, err := readRuntimeIdentityString(ctx, native, "EVM chain id", "eth_chainId")
	if err != nil {
		return types.Hash{}, errors.Join(err, ctx.Err())
	}
	if evmChainId != "0x3c4" {
		return types.Hash{}, errors.Join(errors.New("runtime network identity EVM chain id differs from independently approved chain 964"), ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return types.Hash{}, err
	}
	return genesis, nil
}
