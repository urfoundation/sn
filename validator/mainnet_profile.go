// Public mainnet terms are shared; validator keys, approval authority and all
// mutable paths are supplied locally by configure. No existing validator's
// configuration, signatures, runtime history or state is inherited.
package validator

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/urfoundation/sn/crv4"
)

// These exact public profile bytes are the initial approval's review hash.
// They describe the shipped terms, not an independent source-to-Wasm proof.
//
//go:embed mainnet-profile.json
var releaseMainnetProfileBytes []byte

// A public deployment and economic policy without validator-specific authority.
// Config is deliberately incomplete until configure supplies identity and paths.
type releaseMainnetProfile struct {
	Schema               string               `json:"schema"`
	ReviewScope          string               `json:"review_scope"`
	Config               ReleaseConfig        `json:"config"`
	NativeChain          string               `json:"native_chain"`
	Proposal             OwnerRecycleProposal `json:"proposal"`
	SubnetOwner          [32]byte             `json:"subnet_owner"`
	OwnerHotkeys         [][32]byte           `json:"owner_hotkeys"`
	MaximumSubnetUids    uint32               `json:"maximum_subnet_uids"`
	MaximumOwnedHotkeys  uint32               `json:"maximum_owned_hotkeys"`
	MaximumLastUpdateAge uint64               `json:"maximum_last_update_age"`
	reviewHash           [32]byte
}

// Decode a fresh copy so a caller cannot mutate a later configuration's terms.
func readReleaseMainnetProfile() (*releaseMainnetProfile, error) {
	var profile releaseMainnetProfile
	decoder := json.NewDecoder(bytes.NewReader(releaseMainnetProfileBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		return nil, err
	}
	profile.reviewHash = sha256.Sum256(releaseMainnetProfileBytes)
	if profile.Schema != "urnetwork-validator-mainnet-profile-v1" || profile.ReviewScope == "" ||
		profile.Config.SchemaVersion != ReleaseMainnetProductionSchemaVersion || !profile.Config.Production ||
		profile.Config.ValidatorID != 0 || profile.Config.StateDir != "" || profile.Config.HotkeySeedFile != "" ||
		productionEconomicSelection(&profile.Config) != nil || len(profile.Config.ProductionAuthorityHistory) != 0 ||
		len(profile.Config.ProductionRuntimeApprovals) != 0 || len(profile.Config.EvidenceV2.Operators) != 0 ||
		len(profile.Config.RPC) == 0 || len(profile.Config.Substrate) == 0 || len(profile.Config.Operators) != 1 {
		return nil, errors.New("bundled mainnet profile contains validator-specific authority or lacks its public schema")
	}
	if err := profile.Proposal.Validate(profile.Config.Policy); err != nil {
		return nil, err
	}
	if err := profile.Config.EvidenceV2.Bounds.Validate(uint64(len(profile.Config.Operators))); err != nil {
		return nil, err
	}
	if spec, ok := crv4.ReviewedNativeOwnerSourceSpec(profile.Proposal.Runtime.SourceCommit); !ok || spec != profile.Config.RuntimeSpec {
		return nil, errors.New("bundled mainnet profile runtime differs from its reviewed producer source")
	}
	return &profile, nil
}
