package miner

// Fleet commands require their own explicit testnet profile and durable
// observation directory. Other provider commands inherit no runtime exception.

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/docopt/docopt-go"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

const fleetProvisionalTestnetGenesis = "0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105"

// Validate before dialing or reading a signing seed. Chain id alone is never
// sufficient; the native genesis is independently checked at installation.
func validateFleetRuntimeCompatibility(manifest *protocol.FleetManifest, profile, directory string) error {
	if profile == "" && directory == "" {
		return nil
	}
	if manifest == nil || manifest.ChainID != 945 || profile != crv4.ProvisionalRuntimeCompatibilityProfile || !filepath.IsAbs(directory) {
		return errors.New("provisional fleet runtime requires the consumed profile, testnet manifest chain 945 and absolute runtime observation directory")
	}
	return nil
}

// The owning command installs the immutable policy before sharing its chain.
func enableFleetProvisionalRuntimeCompatibility(chain *crv4.Chain, manifest *protocol.FleetManifest, profile, directory string) error {
	if err := validateFleetRuntimeCompatibility(manifest, profile, directory); err != nil {
		return err
	}
	if profile == "" {
		return nil
	}
	genesis, err := types.NewHashFromHexString(fleetProvisionalTestnetGenesis)
	if err != nil {
		return err
	}
	return chain.EnableProvisionalRuntimeCompatibility(genesis, func(artifact crv4.AuthenticatedRuntimeArtifact) error {
		return crv4.WriteProvisionalRuntimeObservation(directory, artifact)
	})
}

// Ordered endpoint ownership and cancellation remain the normal fleet path.
func dialFleetNativeOptionsContext(ctx context.Context, opts docopt.Opts, manifest *protocol.FleetManifest) (*crv4.Chain, string, error) {
	profile := fleetOpt(opts, "--provisional-runtime-compatibility")
	directory := fleetOpt(opts, "--runtime-observation-dir")
	if err := validateFleetRuntimeCompatibility(manifest, profile, directory); err != nil {
		return nil, "", err
	}
	if profile == "" {
		return dialFleetNativeContext(ctx, fleetOpts(opts, "--substrate"))
	}
	return dialFleetNativeWithEndpointContext(ctx, fleetOpts(opts, "--substrate"), fleetNativeEndpointTimeout, context.WithTimeout, crv4.DialChainContext, func(endpointCtx context.Context, chain *crv4.Chain) error {
		if err := enableFleetProvisionalRuntimeCompatibility(chain, manifest, profile, directory); err != nil {
			return err
		}
		_, err := authenticateAndBindFleetRuntimeFinalizedContext(endpointCtx, chain)
		return err
	})
}
