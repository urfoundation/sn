package validator

// Release validators authenticate the complete native runtime at the exact
// finalized hash used for every steering snapshot. Runtime spec alone cannot
// bind state encoding, signed transactions, Wasm, or metadata.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/crv4"
)

const (
	releaseRuntimeSpecVersion        = uint32(453)
	releaseRuntimeTransactionVersion = uint32(1)
	releaseRuntimeStateVersion       = uint8(1)
	releaseRuntimeCodeHash           = "0xabe169cc148e2a63068772788c191fa6566f02aa2ea9afb80cdeb28217bab4d4"
	releaseRuntimeMetadataHash       = "0xb00e7e0188d537136a973df4d5c5f2c86ef903ffff49c1cf8d129dabc98b07ce"
)

func validateReleaseNativeRuntimeConfig(cfg *ReleaseConfig) error {
	if cfg == nil ||
		cfg.RuntimeSpec != releaseRuntimeSpecVersion ||
		cfg.TransactionVersion != releaseRuntimeTransactionVersion ||
		cfg.StateVersion != releaseRuntimeStateVersion ||
		!strings.EqualFold(cfg.RuntimeCodeHash, releaseRuntimeCodeHash) ||
		!strings.EqualFold(cfg.RuntimeMetadataHash, releaseRuntimeMetadataHash) {
		return errors.New("release 1.0 native runtime is not the reviewed node-subtensor/453/1/1 artifact")
	}
	return nil
}

func validatePinnedRuntimeHash(name, observed, expected string) error {
	observedHash, err := parseHash32("observed "+name, observed)
	if err != nil {
		return err
	}
	expectedHash, err := parseHash32("configured "+name, expected)
	if err != nil {
		return err
	}
	if observedHash != expectedHash {
		return fmt.Errorf("native %s %s, configured %s", name, observed, expected)
	}
	return nil
}

func authenticatePinnedNativeRuntimeAt(chain *crv4.Chain, cfg *ReleaseConfig, finalized types.Hash) error {
	if chain == nil || cfg == nil || finalized == (types.Hash{}) {
		return errors.New("native runtime identity context is incomplete")
	}
	if err := validateReleaseNativeRuntimeConfig(cfg); err != nil {
		return err
	}
	version, err := crv4.RuntimeVersionAt(chain, finalized)
	if err != nil {
		return err
	}
	if version.SpecName != "node-subtensor" ||
		version.SpecVersion != cfg.RuntimeSpec ||
		version.TransactionVersion != cfg.TransactionVersion ||
		version.StateVersion != cfg.StateVersion {
		return fmt.Errorf(
			"native runtime at %s is %s/%d/%d/%d, configured node-subtensor/%d/%d/%d",
			finalized.Hex(), version.SpecName, version.SpecVersion, version.TransactionVersion, version.StateVersion,
			cfg.RuntimeSpec, cfg.TransactionVersion, cfg.StateVersion,
		)
	}
	codeHash, err := crv4.RuntimeCodeHashAt(chain, finalized)
	if err != nil {
		return fmt.Errorf("native runtime code hash at %s: %w", finalized.Hex(), err)
	}
	if err := validatePinnedRuntimeHash("code hash", codeHash, cfg.RuntimeCodeHash); err != nil {
		return err
	}
	metadata, metadataHash, err := crv4.RuntimeMetadataAt(chain, finalized)
	if err != nil {
		return fmt.Errorf("native runtime metadata at %s: %w", finalized.Hex(), err)
	}
	if err := validatePinnedRuntimeHash("metadata hash", metadataHash, cfg.RuntimeMetadataHash); err != nil {
		return err
	}
	chain.Meta = metadata
	chain.Runtime = &types.RuntimeVersion{
		SpecName:           version.SpecName,
		SpecVersion:        types.U32(version.SpecVersion),
		TransactionVersion: types.U32(version.TransactionVersion),
	}
	return nil
}

func authenticatePinnedNativeRuntime(chain *crv4.Chain, cfg *ReleaseConfig) (types.Hash, error) {
	if chain == nil || chain.API == nil {
		return types.Hash{}, errors.New("native runtime chain is unavailable")
	}
	finalized, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return types.Hash{}, err
	}
	if err := authenticatePinnedNativeRuntimeAt(chain, cfg, finalized); err != nil {
		return types.Hash{}, err
	}
	return finalized, nil
}
