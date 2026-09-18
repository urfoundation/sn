package validator

// Provisional testnet runtime observations are separate from the immutable
// release config and from the authority of retained signed transactions.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

const provisionalRuntimeTestnetGenesis = "0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105"

// This is a distinct permission; deferring closed input never enables it.
func validateReleaseProvisionalRuntimeCompatibility(cfg *ReleaseConfig) error {
	if cfg == nil {
		return errors.New("provisional runtime config is unavailable")
	}
	if cfg.ProvisionalRuntimeCompatibility == "" {
		return nil
	}
	if cfg.ProvisionalRuntimeCompatibility != crv4.ProvisionalRuntimeCompatibilityProfile || cfg.ChainID != 945 || cfg.Policy.NetworkProfile != "testnet" || !strings.EqualFold(cfg.GenesisHash, provisionalRuntimeTestnetGenesis) || !filepath.IsAbs(cfg.StateDir) {
		return errors.New("provisional runtime compatibility requires the consumed profile, testnet chain 945, exact testnet genesis and absolute state directory")
	}
	return nil
}

// Installation precedes sharing the connection or decoding native state.
func enableReleaseProvisionalRuntimeCompatibility(native *crv4.Chain, cfg *ReleaseConfig) error {
	if err := validateReleaseProvisionalRuntimeCompatibility(cfg); err != nil {
		return err
	}
	if cfg.ProvisionalRuntimeCompatibility == "" {
		return nil
	}
	genesis, err := types.NewHashFromHexString(cfg.GenesisHash)
	if err != nil {
		return err
	}
	return native.EnableProvisionalRuntimeCompatibility(genesis, func(artifact crv4.AuthenticatedRuntimeArtifact) error {
		return crv4.WriteProvisionalRuntimeObservation(filepath.Join(cfg.StateDir, "runtime-compatibility"), artifact)
	})
}

// The mutable signing view must contain an already authenticated artifact;
// config authority and signed source versions remain their original values.
func validateReleaseNativeSigningRuntime(native *crv4.Chain, cfg *ReleaseConfig) error {
	if native == nil || cfg == nil || native.Runtime == nil {
		return errors.New("native signing runtime is unavailable")
	}
	if uint32(native.Runtime.SpecVersion) == cfg.RuntimeSpec && uint32(native.Runtime.TransactionVersion) == cfg.TransactionVersion && native.Runtime.SpecName == "node-subtensor" {
		return nil
	}
	if err := validateReleaseProvisionalRuntimeCompatibility(cfg); err != nil {
		return err
	}
	if cfg.ProvisionalRuntimeCompatibility != "" && native.CurrentRuntimeCompatibilityProfile() == cfg.ProvisionalRuntimeCompatibility {
		return nil
	}
	return errors.New("native signing runtime is not bound to the configured artifact or explicitly authenticated provisional profile")
}

// Compatibility permits fresh signing with the actual version. It cannot
// make an old unfinalized signature replayable after a runtime replacement.
func validatePreparedNativeRuntimeContext(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, preparedHash, currentHash types.Hash) error {
	expected := crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: cfg.RuntimeSpec, TransactionVersion: cfg.TransactionVersion, StateVersion: cfg.StateVersion}, CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash}
	prepared, err := crv4.AuthenticateRuntimeArtifactAtContext(ctx, native, preparedHash, expected)
	if err != nil {
		return fmt.Errorf("authenticate pending signing runtime: %w", err)
	}
	current, err := crv4.AuthenticateRuntimeArtifactAtContext(ctx, native, currentHash, expected)
	if err != nil {
		return fmt.Errorf("authenticate current replay runtime: %w", err)
	}
	if prepared.Version != current.Version || prepared.CodeHash != current.CodeHash || prepared.MetadataHash != current.MetadataHash {
		return fmt.Errorf("prepared steering signing runtime differs from current runtime: prepared %s/%d/%d/%d, current %s/%d/%d/%d", prepared.Version.SpecName, prepared.Version.SpecVersion, prepared.Version.TransactionVersion, prepared.Version.StateVersion, current.Version.SpecName, current.Version.SpecVersion, current.Version.TransactionVersion, current.Version.StateVersion)
	}
	return nil
}

// Both fresh intent versions share this last submission boundary. The bool
// distinguishes admission refusal from an attempted submit with uncertain
// finality, preserving the existing pending-error persistence behavior.
func submitPreparedNativeRuntimeContext(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, prepared *crv4.PreparedSubmission) (*crv4.SubmitResult, bool, error) {
	if prepared == nil {
		return nil, false, errors.New("prepared steering submission is unavailable")
	}
	preparedHash, err := types.NewHashFromHexString(prepared.PreparedAtBlockHash)
	if err != nil {
		return nil, false, fmt.Errorf("decode prepared steering runtime hash: %w", err)
	}
	currentHash, err := authenticatePinnedNativeRuntimeContext(ctx, native, cfg)
	if err != nil {
		return nil, false, fmt.Errorf("authenticate native runtime before steering broadcast: %w", err)
	}
	if err := validatePreparedNativeRuntimeContext(ctx, native, cfg, preparedHash, currentHash); err != nil {
		return nil, false, err
	}
	result, err := crv4.SubmitPrepared(ctx, native, prepared)
	return result, true, err
}
