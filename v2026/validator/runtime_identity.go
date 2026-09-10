package validator

// Release validators authenticate the complete native runtime at the exact
// finalized hash used for every steering snapshot. Runtime spec alone cannot
// bind state encoding, signed transactions, Wasm, or metadata.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/v2026/crv4"
)

const (
	releaseRuntimeSpecVersion        = uint32(455)
	releaseRuntimeTransactionVersion = uint32(1)
	releaseRuntimeStateVersion       = uint8(1)
	releaseRuntimeCodeHash           = "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a"
	releaseRuntimeMetadataHash       = "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc"
)

func validateReleaseNativeRuntimeConfig(cfg *ReleaseConfig) error {
	if cfg == nil ||
		cfg.RuntimeSpec != releaseRuntimeSpecVersion ||
		cfg.TransactionVersion != releaseRuntimeTransactionVersion ||
		cfg.StateVersion != releaseRuntimeStateVersion ||
		!strings.EqualFold(cfg.RuntimeCodeHash, releaseRuntimeCodeHash) ||
		!strings.EqualFold(cfg.RuntimeMetadataHash, releaseRuntimeMetadataHash) {
		return errors.New("release 1.0 native runtime is not the reviewed node-subtensor/455/1/1 artifact")
	}
	return nil
}

// authenticatePinnedNativeRuntimeAtContext binds a caller-selected finalized
// block only while its public-RPC reads remain cancellable by that caller.
func authenticatePinnedNativeRuntimeAtContext(ctx context.Context, chain *crv4.Chain, cfg *ReleaseConfig, finalized types.Hash) error {
	if ctx == nil || chain == nil || cfg == nil || finalized == (types.Hash{}) {
		return errors.New("native runtime identity context is incomplete")
	}
	if err := validateReleaseNativeRuntimeConfig(cfg); err != nil {
		return err
	}
	expected := crv4.RuntimeArtifactIdentity{
		Version: crv4.RuntimeVersionIdentity{
			SpecName:           "node-subtensor",
			SpecVersion:        cfg.RuntimeSpec,
			TransactionVersion: cfg.TransactionVersion,
			StateVersion:       cfg.StateVersion,
		},
		CodeHash:     cfg.RuntimeCodeHash,
		MetadataHash: cfg.RuntimeMetadataHash,
	}
	artifact, err := crv4.AuthenticateRuntimeArtifactAtContext(ctx, chain, finalized, expected)
	if err != nil {
		return fmt.Errorf("native runtime at %s is not the configured node-subtensor/%d/%d/%d artifact: %w", finalized.Hex(), cfg.RuntimeSpec, cfg.TransactionVersion, cfg.StateVersion, err)
	}
	chain.Meta = artifact.Metadata
	chain.Runtime = &types.RuntimeVersion{
		SpecName:           artifact.Version.SpecName,
		SpecVersion:        types.U32(artifact.Version.SpecVersion),
		TransactionVersion: types.U32(artifact.Version.TransactionVersion),
	}
	return nil
}

// authenticatePinnedNativeRuntimeContext selects the canonical finalized head
// and binds its complete reviewed artifact through caller-cancellable RPCs.
func authenticatePinnedNativeRuntimeContext(ctx context.Context, chain *crv4.Chain, cfg *ReleaseConfig) (types.Hash, error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil {
		return types.Hash{}, errors.New("native runtime chain is unavailable")
	}
	finalized, err := crv4.FinalizedHeadContext(ctx, chain)
	if err != nil {
		return types.Hash{}, err
	}
	if err := authenticatePinnedNativeRuntimeAtContext(ctx, chain, cfg, finalized); err != nil {
		return types.Hash{}, err
	}
	return finalized, nil
}
