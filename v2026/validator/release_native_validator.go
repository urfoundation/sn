// Startup authenticates the actual signing hotkey's native registration,
// inherited weighted stake and non-self permit before acquiring durable state.
// This current-state prerequisite is separate from earlier activation evidence.
package validator

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/v2026/crv4"
)

// The bound is the reviewed native SubnetworkN wire type, not an operator-
// supplied census. Even its maximum bounds the selective response below3MiB.
const releaseNativeValidatorMaximumUIDs = uint32(math.MaxUint16)

// Uses the same caller-clamped native observation window as startup runtime
// authentication. No default endpoint, historical block or signing key is made
// up here; the release config's exact artifact must pass before any RPC.
func authenticateReleaseValidatorStakeContext(ctx context.Context, chain *crv4.Chain, cfg *ReleaseConfig, hotkey [32]byte, uid uint16) (crv4.ValidatorStakeObservation, error) {
	if ctx == nil || cfg == nil {
		return crv4.ValidatorStakeObservation{}, errors.New("native validator startup context is incomplete")
	}
	if err := validateReleaseNativeRuntimeConfig(cfg); err != nil {
		return crv4.ValidatorStakeObservation{}, err
	}
	genesis, err := parseHash32("genesis_hash", cfg.GenesisHash)
	if err != nil {
		return crv4.ValidatorStakeObservation{}, err
	}
	expected := crv4.RuntimeArtifactIdentity{
		Version:  crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion},
		CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash,
	}
	observationCtx, cancel := context.WithTimeout(ctx, releaseNativeEndpointTimeout(cfg))
	defer cancel()
	return readReleaseNativeValidatorAtContext(observationCtx, chain, types.Hash(genesis), cfg.Netuid, hotkey, uid, expected)
}

// Reads through the actual CRV4 adapter. Keeping the independent expected
// artifact explicit lets deterministic RPC fixtures exercise the complete
// reader without substituting an already-approved eligibility verdict.
func readReleaseNativeValidatorAtContext(ctx context.Context, chain *crv4.Chain, genesis types.Hash, netuid uint16, hotkey [32]byte, uid uint16, expected crv4.RuntimeArtifactIdentity) (crv4.ValidatorStakeObservation, error) {
	empty := crv4.ValidatorStakeObservation{}
	if ctx == nil || chain == nil || chain.API == nil || chain.API.Client == nil ||
		genesis == (types.Hash{}) || chain.GenesisHash != genesis || netuid == 0 ||
		hotkey == ([32]byte{}) || uint32(uid) >= releaseNativeValidatorMaximumUIDs {
		return empty, errors.New("native validator startup identity is incomplete or outside the native UID bound")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	finalized, err := crv4.FinalizedHeadContext(ctx, chain)
	if err != nil {
		return empty, err
	}
	header, err := chain.HeaderAtContext(ctx, finalized)
	if err != nil {
		return empty, err
	}
	observation, err := crv4.ReadValidatorStakeAtContext(ctx, chain, crv4.ValidatorIdentityQuery{
		GenesisHash: genesis, BlockHash: finalized, BlockNumber: uint64(header.Number),
		Netuid: netuid, UID: uid, MaximumSubnetUIDs: releaseNativeValidatorMaximumUIDs,
	}, expected)
	if err != nil {
		return empty, fmt.Errorf("native validator startup observation: %w", err)
	}
	if observation.Identity.Hotkey != hotkey {
		return empty, errors.New("native validator startup hotkey/UID differs from the signing identity")
	}
	if !observation.MeetsNonSelfStakeAndPermit() {
		return empty, fmt.Errorf("native validator UID %d lacks non-self stake/permit authority at block %d: weighted_stake_rao=%d threshold_rao=%d permit=%t", uid, observation.Identity.BlockNumber, observation.TotalStakeRao, observation.StakeThresholdRao, observation.Identity.ValidatorPermit)
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return observation, nil
}
