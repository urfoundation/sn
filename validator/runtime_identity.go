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

	"github.com/urfoundation/sn/crv4"
)

const (
	releaseRuntimeSpecVersion            = uint32(459)
	releaseRuntimeTransactionVersion     = uint32(1)
	releaseRuntimeStateVersion           = uint8(1)
	releaseRuntimeCodeHash               = "0x558275958401c026fa4a4159466d49eabd08c761f0c801390593fcba91dee69b"
	releaseRuntimeMetadataHash           = "0xcf97fac54fee756137f42e53deeeca828959a74c6d87274898db2c36a33c4fef"
	releaseHistoricalRuntimeCodeHash     = "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a"
	releaseHistoricalRuntimeMetadataHash = "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc"
)

func validateReleaseNativeRuntimeConfig(cfg *ReleaseConfig) error {
	if cfg == nil ||
		cfg.RuntimeSpec != releaseRuntimeSpecVersion ||
		cfg.TransactionVersion != releaseRuntimeTransactionVersion ||
		cfg.StateVersion != releaseRuntimeStateVersion ||
		!strings.EqualFold(cfg.RuntimeCodeHash, releaseRuntimeCodeHash) ||
		!strings.EqualFold(cfg.RuntimeMetadataHash, releaseRuntimeMetadataHash) {
		return errors.New("release 1.0 native runtime is not the reviewed node-subtensor/459/1/1 artifact")
	}
	return nil
}

// Archive owners retain their original runtime fields. Only the three reviewed
// artifacts belong to this historical reader; current admission stays separate.
func validateReleaseHistoricalNativeRuntimeConfig(cfg *ReleaseConfig) error {
	if cfg != nil && cfg.RuntimeSpec == 455 && cfg.TransactionVersion == 1 && cfg.StateVersion == 1 &&
		strings.EqualFold(cfg.RuntimeCodeHash, releaseHistoricalRuntimeCodeHash) && strings.EqualFold(cfg.RuntimeMetadataHash, releaseHistoricalRuntimeMetadataHash) {
		return nil
	}
	if cfg != nil && cfg.RuntimeSpec == 458 && cfg.TransactionVersion == 1 && cfg.StateVersion == 1 &&
		strings.EqualFold(cfg.RuntimeCodeHash, "0x2fdb28e5c3fe4e79844b25dee09ed960e90004432ea2bd98079aba4c5530c51a") && strings.EqualFold(cfg.RuntimeMetadataHash, "0x040088e73e34ed5561372aa51b07b56e41cf7f390312837b074434f30452593d") {
		return nil
	}
	return validateReleaseNativeRuntimeConfig(cfg)
}

// Only exact reviewed release artifacts inherit their original replay domain.
// Current observations and signing keep a single pin; old458 owners still read
// their455 receipts without adopting459 or rewriting the original source.
func HistoricalReleaseRuntimeArtifacts(current crv4.RuntimeArtifactIdentity) []crv4.RuntimeArtifactIdentity {
	allowed := []crv4.RuntimeArtifactIdentity{current}
	currentRelease := current.Version == (crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: releaseRuntimeSpecVersion, TransactionVersion: releaseRuntimeTransactionVersion, StateVersion: releaseRuntimeStateVersion}) &&
		strings.EqualFold(current.CodeHash, releaseRuntimeCodeHash) && strings.EqualFold(current.MetadataHash, releaseRuntimeMetadataHash)
	previousRelease := current.Version == (crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 458, TransactionVersion: 1, StateVersion: 1}) &&
		strings.EqualFold(current.CodeHash, "0x2fdb28e5c3fe4e79844b25dee09ed960e90004432ea2bd98079aba4c5530c51a") && strings.EqualFold(current.MetadataHash, "0x040088e73e34ed5561372aa51b07b56e41cf7f390312837b074434f30452593d")
	if !currentRelease && !previousRelease {
		return allowed
	}
	allowed = append(allowed, crv4.RuntimeArtifactIdentity{
		Version:  crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1, StateVersion: 1},
		CodeHash: releaseHistoricalRuntimeCodeHash, MetadataHash: releaseHistoricalRuntimeMetadataHash,
	})
	if currentRelease {
		allowed = append(allowed, crv4.RuntimeArtifactIdentity{
			Version:  crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 458, TransactionVersion: 1, StateVersion: 1},
			CodeHash: "0x2fdb28e5c3fe4e79844b25dee09ed960e90004432ea2bd98079aba4c5530c51a", MetadataHash: "0x040088e73e34ed5561372aa51b07b56e41cf7f390312837b074434f30452593d",
		})
	}
	return allowed
}

// authenticatePinnedNativeRuntimeAtContext binds a caller-selected finalized
// block only while its public-RPC reads remain cancellable by that caller.
func authenticatePinnedNativeRuntimeAtContext(ctx context.Context, chain *crv4.Chain, cfg *ReleaseConfig, finalized types.Hash) error {
	return authenticateReleaseNativeRuntimeAtContext(ctx, chain, cfg, finalized, false)
}

// The caller must own a private chain view and independently authenticate the
// original signed source or receipt. This cannot admit an old signing runtime.
func authenticateHistoricalNativeRuntimeAtContext(ctx context.Context, chain *crv4.Chain, cfg *ReleaseConfig, finalized types.Hash) error {
	return authenticateReleaseNativeRuntimeAtContext(ctx, chain, cfg, finalized, true)
}

func authenticateReleaseNativeRuntimeAtContext(ctx context.Context, chain *crv4.Chain, cfg *ReleaseConfig, finalized types.Hash, historical bool) error {
	if ctx == nil || chain == nil || cfg == nil || finalized == (types.Hash{}) {
		return errors.New("native runtime identity context is incomplete")
	}
	validate := validateReleaseNativeRuntimeConfig
	if historical {
		validate = validateReleaseHistoricalNativeRuntimeConfig
	}
	if err := validate(cfg); err != nil {
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
	allowed := []crv4.RuntimeArtifactIdentity{expected}
	if historical {
		allowed = HistoricalReleaseRuntimeArtifacts(expected)
	}
	artifact, err := crv4.AuthenticateRuntimeArtifactAtContext(ctx, chain, finalized, allowed...)
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
