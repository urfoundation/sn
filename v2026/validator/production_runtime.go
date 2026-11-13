// Mainnet production authenticates fresh chain identity and one exact finalized
// runtime window before binding its private signing or historical read view.
package validator

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// A caller must own this Chain view. The original binding remains unchanged
// until the complete production-purpose and closing canonical checks succeed.
func authenticateOwnerRecycleProductionRuntimeAtContext(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, block types.Hash) error {
	if ctx == nil {
		return errors.New("production runtime caller context is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, productionSteeringReadTimeout)
	defer cancel()
	ctx = withRuntimeFinalityOwner(ctx)
	var finality runtimeFinalityObservation
	view, err := crv4.ReadRuntimeObservationContext(ctx, native, func(ctx context.Context) (*crv4.Chain, error) {
		artifact, number, err := authenticateOwnerRecycleProductionArtifactAttempt(ctx, native, cfg, block, false, &finality)
		if err != nil {
			return nil, err
		}
		view := *native
		if err := view.BindValidatorProducerRuntimeArtifactContext(ctx, artifact); err != nil {
			return nil, err
		}
		if err := finality.close(ctx, native, runtimeFinalityWitness{hash: block, number: number}); err != nil {
			return nil, err
		}
		return &view, ctx.Err()
	})
	if err != nil {
		return err
	}
	*native = *view
	return nil
}

// Complete committed headers select signed runtime windows for both current
// and historical reads. Only the caller selects the authenticated bind purpose;
// an earlier approved artifact never becomes current signing authority.
func authenticateOwnerRecycleProductionArtifactAtContext(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, block types.Hash, historical bool) (crv4.AuthenticatedRuntimeArtifact, uint64, error) {
	if ctx == nil {
		return crv4.AuthenticatedRuntimeArtifact{}, 0, errors.New("production runtime caller context is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, productionSteeringReadTimeout)
	defer cancel()
	ctx = withRuntimeFinalityOwner(ctx)
	type result struct {
		artifact crv4.AuthenticatedRuntimeArtifact
		number   uint64
	}
	var finality runtimeFinalityObservation
	value, err := crv4.ReadRuntimeObservationContext(ctx, native, func(ctx context.Context) (result, error) {
		artifact, number, err := authenticateOwnerRecycleProductionArtifactAttempt(ctx, native, cfg, block, historical, &finality)
		return result{artifact: artifact, number: number}, err
	})
	return value.artifact, value.number, err
}

// No production view can join a fresh artifact to an earlier transport's
// network identity or finalized census. The enclosing read owns all retries.
func authenticateOwnerRecycleProductionArtifactAttempt(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, block types.Hash, historical bool, finality *runtimeFinalityObservation) (crv4.AuthenticatedRuntimeArtifact, uint64, error) {
	empty := crv4.AuthenticatedRuntimeArtifact{}
	if err := validateReleaseProductionRuntimeHistory(cfg); err != nil {
		return empty, 0, err
	}
	if err := validateReleaseProductionAuthorityHistory(cfg); err != nil {
		return empty, 0, err
	}
	if !historical && cfg.ownerRecycleProduction.historicalOnly {
		return empty, 0, errors.New("original production authority permits historical reads only")
	}
	if ctx == nil || native == nil || native.API == nil || native.API.Client == nil || block == (types.Hash{}) ||
		native.ProvisionalRuntimeCompatibilityEnabled() || !slices.Contains(cfg.Substrate, native.API.Client.URL()) {
		return empty, 0, errors.New("production runtime requires an approved non-provisional connection and exact block")
	}
	if err := ctx.Err(); err != nil {
		return empty, 0, err
	}
	approved, err := ownerRecycleProductionApproval(cfg)
	if err != nil {
		return empty, 0, err
	}
	expectedGenesis, err := types.NewHashFromHexString(cfg.GenesisHash)
	if err != nil || native.GenesisHash != expectedGenesis {
		return empty, 0, errors.New("production runtime connection genesis differs from signed authority")
	}
	genesis, err := readRuntimeNetworkIdentity(ctx, native, approved.Approval.NativeChain, expectedGenesis)
	if err != nil {
		return empty, 0, err
	}
	finalized, err := readRuntimeFinalityWitness(ctx, native)
	if err != nil {
		return empty, 0, err
	}
	number := finalized.number
	if block != finalized.hash {
		number, _, err = native.ReceiptHeaderAtContext(ctx, block)
		if err != nil {
			return empty, 0, err
		}
	}
	if number == 0 {
		return empty, 0, errors.New("production runtime block is genesis")
	}
	selectedBlock := runtimeFinalityWitness{hash: block, number: number}
	if err := finality.check(ctx, native, finalized, selectedBlock); err != nil {
		return empty, 0, err
	}
	expected, err := releaseProductionRuntimeAt(cfg, number, historical)
	if err != nil {
		return empty, 0, err
	}
	artifact, err := crv4.ReadRuntimeArtifactAtContext(ctx, native, block, expected)
	if err != nil {
		return empty, 0, fmt.Errorf("production runtime at %s: %w", block.Hex(), err)
	}
	if err := crv4.ValidateValidatorProducerRuntimeArtifactContext(ctx, native, artifact); err != nil {
		return empty, 0, err
	}
	if err := finality.close(ctx, native, selectedBlock); err != nil {
		return empty, 0, err
	}
	artifact.GenesisHash = genesis
	return artifact, number, nil
}
