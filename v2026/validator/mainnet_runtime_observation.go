// Mainnet observation authenticates only an explicitly approved exact artifact
// at a canonical finalized block. It does not decode arbitrary runtime storage,
// construct transactions, open state writers or grant a producer capability.
package validator

import (
	"context"
	"errors"
	"fmt"
	"slices"

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
	if ctx == nil {
		return crv4.AuthenticatedRuntimeArtifact{}, nil, errors.New("mainnet runtime observation context is absent")
	}
	ctx, cancel := context.WithTimeout(ctx, productionSteeringReadTimeout)
	defer cancel()
	ctx = withRuntimeFinalityOwner(ctx)
	type result struct {
		artifact    crv4.AuthenticatedRuntimeArtifact
		observation *MainnetRuntimeObservation
	}
	var finality runtimeFinalityObservation
	value, err := crv4.ReadRuntimeObservationContext(ctx, native, func(ctx context.Context) (result, error) {
		artifact, observation, err := authenticateReleaseMainnetRuntimeAttempt(ctx, native, cfg, &block, &finality)
		return result{artifact: artifact, observation: observation}, err
	})
	return value.artifact, value.observation, err
}

// The first selected finalized block remains pinned if its transport expires;
// fresh network identity and every canonical check still repeat together.
func authenticateReleaseMainnetRuntimeAttempt(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, selectedBlock *types.Hash, finality *runtimeFinalityObservation) (crv4.AuthenticatedRuntimeArtifact, *MainnetRuntimeObservation, error) {
	empty := crv4.AuthenticatedRuntimeArtifact{}
	if err := validateReleaseMainnetRuntimeHistory(cfg); err != nil {
		return empty, nil, err
	}
	if ctx == nil || native == nil || native.API == nil || native.API.Client == nil || native.ProvisionalRuntimeCompatibilityEnabled() ||
		!slices.Contains(cfg.Substrate, native.API.Client.URL()) {
		return empty, nil, errors.New("mainnet runtime observation requires an approved non-provisional connection and caller context")
	}
	if err := ctx.Err(); err != nil {
		return empty, nil, err
	}
	history := cfg.mainnetRuntimeHistory
	pin := history.approvals[0]
	expectedGenesis, err := types.NewHashFromHexString(pin.GenesisHash)
	if err != nil || native.GenesisHash != expectedGenesis {
		return empty, nil, errors.New("mainnet runtime observation connection genesis differs from independent authority")
	}
	genesis, err := readRuntimeNetworkIdentity(ctx, native, pin.NativeChain, expectedGenesis)
	if err != nil {
		return empty, nil, err
	}
	finalized, err := readRuntimeFinalityWitness(ctx, native)
	if err != nil {
		return empty, nil, err
	}
	if *selectedBlock == (types.Hash{}) {
		*selectedBlock, err = crv4.SelectFinalityReadBlockContext(ctx, native, *selectedBlock, finalized.hash)
		if err != nil {
			return empty, nil, err
		}
	}
	block := *selectedBlock
	number := finalized.number
	if block != finalized.hash {
		number, _, err = native.ReceiptHeaderAtContext(ctx, block)
		if err != nil {
			return empty, nil, err
		}
	}
	if number == 0 {
		return empty, nil, errors.New("mainnet runtime observation block is genesis")
	}
	selectedBlockWitness := runtimeFinalityWitness{hash: block, number: number}
	if err := finality.check(ctx, native, finalized, selectedBlockWitness); err != nil {
		return empty, nil, err
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
	artifact, err := crv4.ReadRuntimeArtifactAtContext(ctx, native, block, selected.artifactIdentity())
	if err != nil {
		return empty, nil, fmt.Errorf("mainnet runtime observation approval revision %d at %s: %w", selected.Revision, block.Hex(), err)
	}
	if artifact.CompatibilityProfile != "" {
		return empty, nil, errors.New("mainnet runtime observation cannot inherit testnet provisional authority")
	}
	if err := finality.close(ctx, native, selectedBlockWitness); err != nil {
		return empty, nil, err
	}
	artifact.GenesisHash = genesis
	return artifact, &MainnetRuntimeObservation{
		ConfigSha256: attemptHex32(history.configHash), ApprovalSha256: cfg.MainnetRuntimeApprovals[selected.Revision-1].SHA256,
		Revision: selected.Revision, NativeChain: pin.NativeChain, GenesisHash: genesis, EvmChainId: 964,
		BlockHash: block, BlockNumber: number, FinalizedHash: finalized.hash, Runtime: selected.artifactIdentity(),
	}, nil
}
