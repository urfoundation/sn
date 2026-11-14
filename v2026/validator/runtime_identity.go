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
	releaseRuntimeSpecVersion            = crv4.ReviewedRuntimeSpecVersion
	releaseRuntimeTransactionVersion     = uint32(1)
	releaseRuntimeStateVersion           = uint8(1)
	releaseRuntimeCodeHash               = crv4.ReviewedRuntimeCodeHash
	releaseRuntimeMetadataHash           = crv4.ReviewedRuntimeMetadataHash
	releaseHistoricalRuntimeCodeHash     = "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a"
	releaseHistoricalRuntimeMetadataHash = "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc"
)

func validateReleaseNativeRuntimeConfig(cfg *ReleaseConfig) error {
	if isOwnerRecycleProductionConfig(cfg) {
		return validateReleaseProductionRuntimeHistory(cfg)
	}
	if cfg != nil && (cfg.SchemaVersion == releaseMainnetRuntimeObservationSchemaVersion || len(cfg.MainnetRuntimeApprovals) != 0 || cfg.mainnetRuntimeHistory != nil) {
		return validateReleaseMainnetRuntimeHistory(cfg)
	}
	if cfg == nil ||
		cfg.RuntimeSpec != releaseRuntimeSpecVersion ||
		cfg.TransactionVersion != releaseRuntimeTransactionVersion ||
		cfg.StateVersion != releaseRuntimeStateVersion ||
		!strings.EqualFold(cfg.RuntimeCodeHash, releaseRuntimeCodeHash) ||
		!strings.EqualFold(cfg.RuntimeMetadataHash, releaseRuntimeMetadataHash) {
		return errors.New("release 1.0 native runtime is not the reviewed node-subtensor/461/1/1 artifact")
	}
	return nil
}

// Archive owners retain their exact original artifact. Companion sources began
// at455; catalog entries before that boundary never gain companion authority.
func validateReleaseHistoricalNativeRuntimeConfig(cfg *ReleaseConfig) error {
	if isOwnerRecycleProductionConfig(cfg) {
		return validateReleaseProductionRuntimeHistory(cfg)
	}
	if cfg != nil && (cfg.SchemaVersion == releaseMainnetRuntimeObservationSchemaVersion || len(cfg.MainnetRuntimeApprovals) != 0 || cfg.mainnetRuntimeHistory != nil) {
		return validateReleaseMainnetRuntimeHistory(cfg)
	}
	if cfg != nil && cfg.RuntimeSpec >= 455 {
		artifact, ok := crv4.ReviewedRuntimeArtifact(crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion})
		if ok && strings.EqualFold(cfg.RuntimeCodeHash, artifact.CodeHash) && strings.EqualFold(cfg.RuntimeMetadataHash, artifact.MetadataHash) {
			return nil
		}
	}
	return errors.New("historical native runtime is not an exact reviewed companion artifact")
}

// Only an exact reviewed owner inherits earlier companion artifacts. Filtering
// by that owner preserves455,458,459 replay domains after each current upgrade.
func HistoricalReleaseRuntimeArtifacts(current crv4.RuntimeArtifactIdentity) []crv4.RuntimeArtifactIdentity {
	allowed := []crv4.RuntimeArtifactIdentity{current}
	owner, ok := crv4.ReviewedRuntimeArtifact(current.Version)
	if !ok || current.Version.SpecVersion < 455 || !strings.EqualFold(current.CodeHash, owner.CodeHash) || !strings.EqualFold(current.MetadataHash, owner.MetadataHash) {
		return allowed
	}
	for _, artifact := range crv4.ReviewedRuntimeArtifacts() {
		if artifact.Version.SpecVersion >= 455 && artifact.Version.SpecVersion < current.Version.SpecVersion {
			allowed = append(allowed, artifact)
		}
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
	if isOwnerRecycleProductionConfig(cfg) {
		if !historical {
			return authenticateOwnerRecycleProductionRuntimeAtContext(ctx, chain, cfg, finalized)
		}
		artifact, _, err := authenticateOwnerRecycleProductionArtifactAtContext(ctx, chain, cfg, finalized, true)
		if err != nil {
			return err
		}
		return chain.BindRuntimeArtifact(artifact)
	}
	if cfg.SchemaVersion == releaseMainnetRuntimeObservationSchemaVersion || len(cfg.MainnetRuntimeApprovals) != 0 || cfg.mainnetRuntimeHistory != nil {
		artifact, _, err := authenticateReleaseMainnetRuntimeAtContext(ctx, chain, cfg, finalized)
		if err != nil {
			return err
		}
		return chain.BindRuntimeArtifact(artifact)
	}
	if err := validateReleaseProvisionalRuntimeCompatibility(cfg); err != nil {
		return err
	}
	validate := validateReleaseNativeRuntimeConfig
	if historical {
		validate = validateReleaseHistoricalNativeRuntimeConfig
	}
	if err := validate(cfg); err != nil {
		// The explicit testnet provisional profile authenticates a successor
		// artifact on this connection from the reviewed configured anchor.
		// Historical replay remains exact-pinned and cannot use this path.
		if historical || cfg.ProvisionalRuntimeCompatibility == "" {
			return err
		}
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
	} else if cfg.ProvisionalRuntimeCompatibility != "" {
		// A retained testnet config may name an exact reviewed predecessor.
		// Its explicit profile admits only the current exact reviewed artifact
		// as a successor; a later unknown version remains subject to the
		// connection-bound provisional metadata checks.
		current, ok := crv4.ReviewedRuntimeArtifact(crv4.RuntimeVersionIdentity{
			SpecName: "node-subtensor", SpecVersion: crv4.ReviewedRuntimeSpecVersion,
			TransactionVersion: releaseRuntimeTransactionVersion, StateVersion: releaseRuntimeStateVersion,
		})
		if !ok {
			return errors.New("reviewed current runtime artifact is unavailable")
		}
		if current != expected {
			allowed = append(allowed, current)
		}
	}
	artifact, err := crv4.ReadRuntimeArtifactAtContext(ctx, chain, finalized, allowed...)
	if err != nil {
		return fmt.Errorf("native runtime at %s is not the configured node-subtensor/%d/%d/%d artifact: %w", finalized.Hex(), cfg.RuntimeSpec, cfg.TransactionVersion, cfg.StateVersion, err)
	}
	if artifact.CompatibilityProfile != "" && (cfg.ProvisionalRuntimeCompatibility != artifact.CompatibilityProfile || !chain.RuntimeArtifactCompatible(artifact)) {
		return errors.New("native runtime compatibility lacks explicit validator authority")
	}
	return chain.BindRuntimeArtifact(artifact)
}

// authenticatePinnedNativeRuntimeContext selects the canonical finalized head
// and binds its complete reviewed artifact through caller-cancellable RPCs.
func authenticatePinnedNativeRuntimeContext(ctx context.Context, chain *crv4.Chain, cfg *ReleaseConfig) (types.Hash, error) {
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil {
		return types.Hash{}, errors.New("native runtime chain is unavailable")
	}
	if isOwnerRecycleProductionConfig(cfg) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, productionSteeringReadTimeout)
		defer cancel()
		ctx = withRuntimeFinalityOwner(ctx)
	}
	var finalized types.Hash
	var err error
	if isOwnerRecycleProductionConfig(cfg) {
		finalized, err = readRuntimeWitnessHash(ctx, chain, "finalized head", "chain_getFinalizedHead")
	} else {
		finalized, err = crv4.FinalizedHeadContext(ctx, chain)
	}
	if err != nil {
		return types.Hash{}, err
	}
	if isOwnerRecycleProductionConfig(cfg) {
		finalized, err = crv4.SelectFinalityReadBlockContext(ctx, chain, types.Hash{}, finalized)
		if err != nil {
			return types.Hash{}, err
		}
	}
	if err := authenticatePinnedNativeRuntimeAtContext(ctx, chain, cfg, finalized); err != nil {
		return types.Hash{}, err
	}
	return finalized, nil
}
