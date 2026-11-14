// A signed mainnet production config can opt in to admitting an authenticated
// successor of its approved runtime when every interface the validator consumes
// is unchanged. Genesis, route, finality and transport authentication are the
// exact production rules; only the identity selected at a block can advance.
package validator

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The opt-in names the same producer interface profile that the approval's
// production.runtime_capability names. It is meaningful only for a signed
// schema-3 mainnet production config and never joins testnet provisional
// compatibility.
func validateReleaseRuntimeSuccessorProfile(cfg *ReleaseConfig) error {
	if cfg == nil {
		return errors.New("runtime successor configuration is unavailable")
	}
	if cfg.RuntimeSuccessorProfile == "" {
		return nil
	}
	if cfg.RuntimeSuccessorProfile != crv4.ValidatorProducerRuntimeProfile || cfg.SchemaVersion != ReleaseMainnetProductionSchemaVersion ||
		cfg.ChainID != 964 || cfg.Policy.NetworkProfile != "mainnet" || cfg.ProvisionalRuntimeCompatibility != "" || productionEconomicSelection(cfg) == nil {
		return fmt.Errorf("runtime_successor_profile requires %s on a signed schema 3 mainnet production config", crv4.ValidatorProducerRuntimeProfile)
	}
	return nil
}

// Each signed config that opted in contributes its own exact approved artifact.
// An original config keeps its anchor so decisions it made under an admitted
// successor still verify after renewal; other windows stay exact.
func releaseRuntimeSuccessionAnchors(cfg *ReleaseConfig) ([]crv4.RuntimeArtifactIdentity, error) {
	if err := validateReleaseProductionAuthorityHistory(cfg); err != nil {
		return nil, err
	}
	var anchors []crv4.RuntimeArtifactIdentity
	add := func(source *ReleaseConfig) error {
		if source.RuntimeSuccessorProfile == "" {
			return nil
		}
		if err := validateReleaseRuntimeSuccessorProfile(source); err != nil {
			return err
		}
		if identity := releaseNativeRuntimeIdentity(source); !slices.Contains(anchors, identity) {
			anchors = append(anchors, identity)
		}
		return nil
	}
	if err := add(cfg); err != nil {
		return nil, err
	}
	if history := cfg.productionAuthorityHistory; history != nil {
		for _, entry := range history.entries {
			if err := add(entry.config); err != nil {
				return nil, err
			}
		}
	}
	return anchors, nil
}

// Beyond the crv4 producer profile, production also decodes the owner census,
// eligibility and (for treasury authority) owner/auto-stake storage. A changed
// shape refuses the successor before any reader decodes its state.
func validateReleaseRuntimeSuccessorStorage(metadata *types.Metadata, treasury bool) error {
	entries, err := ownerRecycleProductionStorageProfile(metadata)
	if err != nil {
		return err
	}
	if treasury {
		return treasuryStorageProfile(metadata, entries)
	}
	return nil
}

// Installation precedes sharing the connection or decoding native state. A
// config without the opt-in, in its current or original authority, leaves the
// connection exact. Admission is reported once per artifact and connection.
func enableReleaseRuntimeSuccession(native *crv4.Chain, cfg *ReleaseConfig) error {
	if native == nil || !isOwnerRecycleProductionConfig(cfg) {
		return errors.New("runtime successor admission requires a production native connection")
	}
	anchors, err := releaseRuntimeSuccessionAnchors(cfg)
	if err != nil || len(anchors) == 0 {
		return err
	}
	genesis, err := types.NewHashFromHexString(cfg.GenesisHash)
	if err != nil {
		return err
	}
	treasury := cfg.TreasuryApproval != nil
	return native.EnableRuntimeSuccession(genesis, crv4.ValidatorProducerRuntimeProfile, anchors, func(successor crv4.RuntimeSuccessor) error {
		if err := validateReleaseRuntimeSuccessorStorage(successor.Metadata, treasury); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "validator: admitted runtime %s/%d code=%s metadata=%s at %s as a %s successor of approved %d\n",
			successor.Artifact.Version.SpecName, successor.Artifact.Version.SpecVersion, successor.Artifact.CodeHash, successor.Artifact.MetadataHash,
			successor.BlockHash.Hex(), crv4.ValidatorProducerRuntimeProfile, successor.Approved.Version.SpecVersion)
		return nil
	})
}
