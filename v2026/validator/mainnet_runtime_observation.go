// Mainnet observation authenticates only an explicitly approved exact artifact
// at a canonical finalized block. It does not decode arbitrary runtime storage,
// construct transactions, open state writers or grant a producer capability.
package validator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Value-only evidence identifies the original config, approval and block.
// Finality/canonical ancestry are Rpc assertions, not an independent light proof.
type MainnetRuntimeObservation struct {
	ConfigSha256   string                       `json:"config_sha256"`
	ApprovalSha256 string                       `json:"approval_sha256"`
	Revision       uint64                       `json:"revision"`
	NativeChain    string                       `json:"native_chain"`
	GenesisHash    types.Hash                   `json:"genesis_hash"`
	EvmChainId     uint64                       `json:"evm_chain_id"`
	BlockHash      types.Hash                   `json:"block_hash"`
	BlockNumber    uint64                       `json:"block_number"`
	FinalizedHash  types.Hash                   `json:"finalized_hash"`
	Runtime        crv4.RuntimeArtifactIdentity `json:"runtime"`
}

// The supplied config must come from LoadMainnetRuntimeObservationConfig.
// A zero hash selects the current finalized head; a nonzero hash observes only
// that historical block. Config and connection must remain immutable during
// concurrent calls. No mutable signing view or authenticated proof is exposed.
func ObserveMainnetRuntimeAtContext(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, block types.Hash) (*MainnetRuntimeObservation, error) {
	_, observation, err := authenticateReleaseMainnetRuntimeAtContext(ctx, native, cfg, block)
	return observation, err
}

// Fresh network identity and closing canonical checks surround exact artifact
// authentication, even when immutable metadata is reused from the connection.
func authenticateReleaseMainnetRuntimeAtContext(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, block types.Hash) (crv4.AuthenticatedRuntimeArtifact, *MainnetRuntimeObservation, error) {
	empty := crv4.AuthenticatedRuntimeArtifact{}
	if err := validateReleaseMainnetRuntimeHistory(cfg); err != nil {
		return empty, nil, err
	}
	if ctx == nil || native == nil || native.API == nil || native.API.Client == nil || native.ProvisionalRuntimeCompatibilityEnabled() ||
		!slices.Contains(cfg.Substrate, native.API.Client.URL()) {
		return empty, nil, errors.New("mainnet runtime observation requires an approved non-provisional connection and caller context")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return empty, nil, err
	}
	history := cfg.mainnetRuntimeHistory
	pin := history.approvals[0]
	expectedGenesis, err := types.NewHashFromHexString(pin.GenesisHash)
	if err != nil || native.GenesisHash != expectedGenesis {
		return empty, nil, errors.New("mainnet runtime observation connection genesis differs from independent authority")
	}
	call := native.API.Client.CallContext
	var nativeChain, evmChainId string
	var genesis types.Hash
	if err := call(ctx, &nativeChain, "system_chain"); err != nil {
		return empty, nil, err
	}
	if err := call(ctx, &genesis, "chain_getBlockHash", uint64(0)); err != nil {
		return empty, nil, err
	}
	if err := call(ctx, &evmChainId, "eth_chainId"); err != nil {
		return empty, nil, err
	}
	if nativeChain != pin.NativeChain || genesis != expectedGenesis || evmChainId != "0x3c4" {
		return empty, nil, errors.New("mainnet runtime observation fresh chain name/genesis/EVM964 identity differs")
	}
	finalized, err := crv4.FinalizedHeadContext(ctx, native)
	if err != nil {
		return empty, nil, err
	}
	finalizedHeader, err := native.HeaderAtContext(ctx, finalized)
	if err != nil {
		return empty, nil, err
	}
	if block == (types.Hash{}) {
		block = finalized
	}
	header := finalizedHeader
	if block != finalized {
		header, err = native.HeaderAtContext(ctx, block)
		if err != nil {
			return empty, nil, err
		}
	}
	number := uint64(header.Number)
	if number == 0 || number > uint64(finalizedHeader.Number) {
		return empty, nil, errors.New("mainnet runtime observation block is not finalized")
	}
	var selected *releaseMainnetRuntimeApproval
	for index := range history.approvals {
		approval := &history.approvals[index]
		if approval.ValidFromBlock <= number && number <= approval.ValidThroughBlock {
			selected = approval
			break
		}
	}
	if selected == nil {
		return empty, nil, errors.New("mainnet runtime observation block is outside every approved interval")
	}
	checkCanonical := func(hash types.Hash, height uint64) error {
		var canonical types.Hash
		if err := call(ctx, &canonical, "chain_getBlockHash", height); err != nil {
			return err
		}
		if canonical != hash {
			return errors.New("mainnet runtime observation block is not canonical at its height")
		}
		return ctx.Err()
	}
	if err := checkCanonical(finalized, uint64(finalizedHeader.Number)); err != nil {
		return empty, nil, err
	}
	if err := checkCanonical(block, number); err != nil {
		return empty, nil, err
	}
	artifact, err := crv4.AuthenticateRuntimeArtifactAtContext(ctx, native, block, selected.artifactIdentity())
	if err != nil {
		return empty, nil, fmt.Errorf("mainnet runtime observation approval revision %d at %s: %w", selected.Revision, block.Hex(), err)
	}
	if artifact.CompatibilityProfile != "" {
		return empty, nil, errors.New("mainnet runtime observation cannot inherit testnet provisional authority")
	}
	if err := checkCanonical(block, number); err != nil {
		return empty, nil, err
	}
	artifact.GenesisHash = genesis
	return artifact, &MainnetRuntimeObservation{
		ConfigSha256: attemptHex32(history.configHash), ApprovalSha256: cfg.MainnetRuntimeApprovals[selected.Revision-1].SHA256,
		Revision: selected.Revision, NativeChain: nativeChain, GenesisHash: genesis, EvmChainId: 964,
		BlockHash: block, BlockNumber: number, FinalizedHash: finalized, Runtime: selected.artifactIdentity(),
	}, nil
}
