// Mainnet setup selects one public, finalized and drained native boundary.
// This read grants no permit, signs no extrinsic and opens no validator state.
package validator

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

// A fresh connection owns every runtime and canonical read; the existing
// bounded observation loop redoes the whole read if its transport changes.
func readReleaseConfigureNativeSnapshot(ctx context.Context, profile *releaseMainnetProfile, endpoint string) (releaseConfigureNativeSnapshot, error) {
	native, err := crv4.DialChainContext(ctx, endpoint)
	if err != nil {
		return releaseConfigureNativeSnapshot{}, err
	}
	defer native.API.Client.Close()
	return readReleaseConfigureNativeSnapshotOn(ctx, native, profile, os.Stderr)
}

// The read on an owned connection. A profile that opts into successor
// admission installs it first, as the generated config's own connections do,
// so setup works on the runtime the chain runs now, not only the anchor.
func readReleaseConfigureNativeSnapshotOn(ctx context.Context, native *crv4.Chain, profile *releaseMainnetProfile, log io.Writer) (releaseConfigureNativeSnapshot, error) {
	if err := enableReleaseConfigureRuntimeSuccession(native, profile, log); err != nil {
		return releaseConfigureNativeSnapshot{}, err
	}
	return crv4.ReadRuntimeObservationContext(ctx, native, func(ctx context.Context) (releaseConfigureNativeSnapshot, error) {
		return readReleaseConfigureNativeBoundary(ctx, native, profile)
	})
}

// The profile's approved artifact is the only anchor. A successor is admitted
// only when the producer interface and the owner and treasury storage the
// generated config decodes are unchanged, exactly as enableReleaseRuntimeSuccession
// admits one for that config. Without the opt-in the connection stays exact.
func enableReleaseConfigureRuntimeSuccession(native *crv4.Chain, profile *releaseMainnetProfile, log io.Writer) error {
	if native == nil || profile == nil {
		return errors.New("setup runtime successor admission is incomplete")
	}
	if profile.Config.RuntimeSuccessorProfile == "" {
		return nil
	}
	if profile.Config.RuntimeSuccessorProfile != crv4.ValidatorProducerRuntimeProfile {
		return fmt.Errorf("bundled mainnet profile names runtime successor profile %q, not %s", profile.Config.RuntimeSuccessorProfile, crv4.ValidatorProducerRuntimeProfile)
	}
	anchor := releaseNativeRuntimeIdentity(&profile.Config)
	return native.EnableRuntimeSuccession(types.Hash(profile.Proposal.Runtime.GenesisHash), crv4.ValidatorProducerRuntimeProfile, []crv4.RuntimeArtifactIdentity{anchor}, func(successor crv4.RuntimeSuccessor) error {
		if err := validateReleaseRuntimeSuccessorStorage(successor.Metadata, true); err != nil {
			return err
		}
		if log != nil {
			fmt.Fprintf(log, "validator: admitted runtime %s/%d code=%s metadata=%s at %s as a %s successor of approved %d\n",
				successor.Artifact.Version.SpecName, successor.Artifact.Version.SpecVersion, successor.Artifact.CodeHash, successor.Artifact.MetadataHash,
				successor.BlockHash.Hex(), crv4.ValidatorProducerRuntimeProfile, successor.Approved.Version.SpecVersion)
		}
		return nil
	})
}

// Reads use the shipped exact artifact and the same producer interface and
// drained-emission storage rules as activation. No validator registration is
// required: the generated config is also used to register and obtain a permit.
func readReleaseConfigureNativeBoundary(ctx context.Context, native *crv4.Chain, profile *releaseMainnetProfile) (releaseConfigureNativeSnapshot, error) {
	empty := releaseConfigureNativeSnapshot{}
	genesis := types.Hash(profile.Proposal.Runtime.GenesisHash)
	if _, err := readRuntimeNetworkIdentity(ctx, native, profile.NativeChain, genesis); err != nil {
		return empty, err
	}
	head, err := crv4.FinalizedHeadContext(ctx, native)
	if err != nil {
		return empty, err
	}
	expected := releaseNativeRuntimeIdentity(&profile.Config)
	artifact, err := crv4.ReadRuntimeArtifactAtContext(ctx, native, head, expected)
	if err != nil {
		return empty, err
	}
	if err := native.BindValidatorProducerRuntimeArtifactContext(ctx, artifact); err != nil {
		return empty, err
	}
	state, err := native.EpochScheduleStateAtContext(ctx, profile.Config.Netuid, head)
	if err != nil {
		return empty, err
	}
	if state.LastEpochBlock == 0 || state.LastEpochBlock > state.CurrentBlock || state.SubnetEpochIndex == 0 || state.Tempo == 0 {
		return empty, errors.New("native schedule has no current drained epoch boundary")
	}
	var encoded string
	if err := native.API.Client.CallContext(ctx, &encoded, "chain_getBlockHash", state.LastEpochBlock); err != nil {
		return empty, err
	}
	hash, err := parseHash32("drained native boundary", encoded)
	if err != nil {
		return empty, err
	}
	block := types.Hash(hash)
	artifact, err = crv4.ReadRuntimeArtifactAtContext(ctx, native, block, expected)
	if err != nil {
		return empty, err
	}
	entries, err := ownerRecycleProductionStorageProfile(artifact.Metadata)
	if err != nil {
		return empty, err
	}
	boundary, err := native.EpochScheduleStateAtContext(ctx, profile.Config.Netuid, block)
	if err != nil {
		return empty, err
	}
	key, err := types.CreateStorageKey(artifact.Metadata, crv4.PalletName, "PendingServerEmission", binary.LittleEndian.AppendUint16(nil, profile.Config.Netuid))
	if err != nil {
		return empty, err
	}
	value := ownerRecycleStorageValue{limit: 8}
	if err := native.API.Client.CallContext(ctx, &value, "state_getStorage", key.Hex(), block.Hex()); err != nil {
		return empty, err
	}
	raw := value.raw
	if !value.present {
		raw = entries["PendingServerEmission"].Fallback
	}
	if boundary.CurrentBlock != state.LastEpochBlock || boundary.SubnetEpochIndex != state.SubnetEpochIndex || len(raw) != 8 || binary.LittleEndian.Uint64(raw) != 0 {
		return empty, fmt.Errorf("native epoch %d at block %d has not drained its miner emission", state.SubnetEpochIndex, state.LastEpochBlock)
	}
	if err := native.CheckCanonicalBlockAtContext(ctx, block, boundary.CurrentBlock); err != nil {
		return empty, err
	}
	if err := native.CheckCanonicalBlockAtContext(ctx, head, state.CurrentBlock); err != nil {
		return empty, err
	}
	return releaseConfigureNativeSnapshot{Boundary: productionSuccessorBoundary{Epoch: state.SubnetEpochIndex, Block: boundary.CurrentBlock, Hash: hash}, Tempo: state.Tempo}, ctx.Err()
}
