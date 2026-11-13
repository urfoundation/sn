// Subnet census policy binds requested identities to registration generations.
// It is an independently reviewed input, not an approval derived from an RPC.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/urfoundation/sn/v2026/crv4"
)

const subnetPolicySchema = "urnetwork-mainnet-subnet-census-policy-v1"
const subnetPreviewSchema = "urnetwork-mainnet-subnet-census-preview-v1"
const subnetStorageProfileName = "subtensor-subnet-census-67dcf7f-v1"

// A numeric slot alone never identifies a removal or protected registration.
type subnetIdentityExpectation struct {
	Hotkey            string  `json:"hotkey_account_id"`
	Coldkey           string  `json:"coldkey_account_id"`
	RegistrationBlock *uint64 `json:"registration_block"`
}

// Declared roles protect identities even when the current validator permit is off.
type subnetProtectedIdentity struct {
	subnetIdentityExpectation
	Roles []string `json:"roles"`
}

// Runtime numbers provide no authority. The source/profile is pinned while
// exact version/code/metadata and subnet generation must be approved separately.
type subnetCensusPolicy struct {
	Schema                  string                      `json:"schema"`
	Netuid                  uint16                      `json:"netuid"`
	NativeChain             string                      `json:"native_chain"`
	GenesisHash             string                      `json:"genesis_hash"`
	EvmChainId              uint64                      `json:"evm_chain_id"`
	StorageProfile          string                      `json:"storage_profile"`
	RuntimeSourceCommit     string                      `json:"runtime_source_commit"`
	RuntimeVersion          crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash         string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash     string                      `json:"runtime_metadata_hash"`
	SubnetOwnerColdkey      string                      `json:"subnet_owner_coldkey_account_id"`
	SubnetRegistrationBlock *uint64                     `json:"subnet_registration_block"`
	SubnetGeneration        *uint64                     `json:"subnet_generation"`
	TrimMaximumUids         *uint16                     `json:"trim_maximum_uids"`
	Remove                  []subnetIdentityExpectation `json:"remove"`
	Preserve                []subnetProtectedIdentity   `json:"preserve"`
}

// Requires an explicit bounded identity scope before making any network read.
func (self subnetCensusPolicy) validate() error {
	if self.Schema != subnetPolicySchema || self.Netuid != 25 || self.EvmChainId != mainnetEvmChainId || strings.TrimSpace(self.NativeChain) == "" {
		return errors.New("subnet census requires its exact schema, netuid 25, native chain and mainnet EVM chain ID 964")
	}
	for _, value := range []string{self.GenesisHash, self.RuntimeCodeHash, self.RuntimeMetadataHash, self.SubnetOwnerColdkey} {
		if !subnetAccountValid(value) {
			return errors.New("subnet census requires independently approved nonzero genesis, runtime artifacts and owner AccountId32")
		}
	}
	if self.StorageProfile != subnetStorageProfileName || !mainnetRuntimeCodecSource(self.RuntimeSourceCommit) {
		return errors.New("subnet census source/storage profile is not reviewed")
	}
	if strings.TrimSpace(self.RuntimeVersion.SpecName) == "" || self.RuntimeVersion.SpecVersion == 0 || self.RuntimeVersion.TransactionVersion == 0 || self.RuntimeVersion.StateVersion == 0 {
		return errors.New("subnet census requires a complete approved runtime version")
	}
	if self.SubnetRegistrationBlock == nil || self.SubnetGeneration == nil || self.TrimMaximumUids == nil {
		return errors.New("subnet census requires explicit subnet generation and proposed trim capacity")
	}
	if len(self.Remove) > rootCensusLimit || len(self.Preserve) > rootCensusLimit || len(self.Remove)+len(self.Preserve) > rootCensusLimit {
		return errors.New("subnet identity scope exceeds the 4096 identity observation bound")
	}
	seen := map[string]bool{}
	check := func(identity subnetIdentityExpectation) error {
		if !subnetAccountValid(identity.Hotkey) || !subnetAccountValid(identity.Coldkey) || identity.RegistrationBlock == nil {
			return errors.New("each scoped identity requires nonzero hotkey, coldkey and explicit registration block")
		}
		hotkey := strings.ToLower(identity.Hotkey)
		if seen[hotkey] {
			return errors.New("duplicate scoped hotkey or removal/protection conflict")
		}
		seen[hotkey] = true
		return nil
	}
	for _, identity := range self.Remove {
		if err := check(identity); err != nil {
			return err
		}
	}
	allowedRoleKVs := map[string]bool{"owner": true, "ur-validator": true, "third-party-validator": true, "reserve": true, "pool": true, "escrow": true, "custody": true, "other": true}
	for _, identity := range self.Preserve {
		if err := check(identity.subnetIdentityExpectation); err != nil {
			return err
		}
		if len(identity.Roles) == 0 || len(identity.Roles) > len(allowedRoleKVs) {
			return errors.New("protected identities require a bounded explicit role list")
		}
		roles := map[string]bool{}
		for _, role := range identity.Roles {
			if !allowedRoleKVs[role] || roles[role] {
				return fmt.Errorf("unrecognized or duplicate protected role %q", role)
			}
			roles[role] = true
		}
	}
	return nil
}

// Account and artifact representations share an exact nonzero 32-byte boundary.
func subnetAccountValid(value string) bool {
	return validHash(value) && value != "0x"+strings.Repeat("0", 64)
}

// Census rows retain the owner and generation independently of the numeric UID.
type subnetRegistration struct {
	Uid               uint16 `json:"uid"`
	Hotkey            string `json:"hotkey_account_id"`
	Coldkey           string `json:"coldkey_account_id"`
	RegistrationBlock uint64 `json:"registration_block"`
}

// Runtime and declared protection remain visible when they conflict with removal.
type subnetSeat struct {
	subnetRegistration
	Active                  bool     `json:"active"`
	ValidatorPermit         bool     `json:"validator_permit"`
	EmissionRao             string   `json:"emission_rao"`
	TemporarilyImmune       bool     `json:"temporarily_immune"`
	ImmunityExpiresAt       uint64   `json:"immunity_expires_at"`
	ImmunityExpirySaturated bool     `json:"immunity_expiry_saturated"`
	OwnerImmune             bool     `json:"owner_immune"`
	OwnerRecognized         bool     `json:"owner_recognized"`
	RequestedRemoval        bool     `json:"requested_removal"`
	Disposition             string   `json:"disposition"`
	ProtectionReasons       []string `json:"protection_reasons"`
	emission                uint64
}

// This is a metadata call descriptor, never a payload or signing authorization.
type subnetTrimCall struct {
	Pallet       string `json:"pallet"`
	Call         string `json:"call"`
	PalletIndex  uint8  `json:"pallet_index"`
	CallIndex    uint8  `json:"call_index"`
	SourceOrigin string `json:"reviewed_source_origin_rule"`
}

// Surviving registrations keep their generation when the runtime compresses UIDs.
type subnetUidMapping struct {
	subnetRegistration
	NewUid uint16 `json:"new_uid"`
}

// Matching one block's selected set does not bind a later destructive execution.
type subnetTrimPreview struct {
	MaximumUids                uint16               `json:"proposed_maximum_uids"`
	Call                       *subnetTrimCall      `json:"verified_call_schema"`
	MaximumImmunePercentage    *uint8               `json:"maximum_immune_percentage"`
	ImmunePercentage           *uint8               `json:"candidate_immune_percentage"`
	OwnerLastTrimBlock         uint64               `json:"owner_last_trim_block"`
	OwnerRateLimitStatus       string               `json:"owner_rate_limit_status"`
	AdminWindowOpen            bool                 `json:"admin_window_open"`
	CandidateComplete          bool                 `json:"candidate_complete"`
	MatchesRequestedRemovalSet bool                 `json:"matches_requested_removal_set_at_observed_block"`
	Removed                    []subnetRegistration `json:"runtime_selected_removals"`
	Survivors                  []subnetUidMapping   `json:"survivor_uid_mapping"`
	Blockers                   []string             `json:"blockers"`
}

// A complete census can be useful while reset remains explicitly blocked.
type subnetPreview struct {
	Schema                  string                      `json:"schema"`
	PolicyHash              string                      `json:"policy_hash"`
	Status                  string                      `json:"status"`
	CensusComplete          bool                        `json:"census_complete"`
	ResetReady              bool                        `json:"reset_ready"`
	Identity                chainIdentity               `json:"identity"`
	Netuid                  uint16                      `json:"netuid"`
	RuntimeSourceCommit     string                      `json:"runtime_source_commit"`
	RuntimeVersion          crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash         string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash     string                      `json:"runtime_metadata_hash"`
	SubnetRegistrationBlock uint64                      `json:"subnet_registration_block"`
	SubnetGeneration        uint64                      `json:"subnet_generation"`
	SubnetOwnerColdkey      string                      `json:"subnet_owner_coldkey_account_id"`
	SubnetOwnerHotkey       *string                     `json:"subnet_owner_hotkey_account_id"`
	MinimumUids             uint16                      `json:"minimum_uids"`
	MaximumUids             uint16                      `json:"maximum_uids"`
	ImmunityBlocks          uint16                      `json:"immunity_blocks"`
	ImmuneOwnerUidsLimit    uint16                      `json:"immune_owner_uids_limit"`
	RegistrationAllowed     bool                        `json:"registration_allowed"`
	PowRegistrationAllowed  bool                        `json:"pow_registration_allowed"`
	Seats                   []subnetSeat                `json:"seats"`
	RootRegistrations       []subnetRegistration        `json:"excluded_root_registrations"`
	Remove                  []subnetRegistration        `json:"remove"`
	Preserve                []subnetRegistration        `json:"preserve"`
	Unresolved              []subnetRegistration        `json:"unresolved"`
	Trim                    subnetTrimPreview           `json:"owner_trim_preview"`
	Storage                 []rootStorageValue          `json:"storage"`
	Blockers                []string                    `json:"scope_blockers"`
	ResetBlockers           []string                    `json:"reset_blockers"`
}

// The hash binds all raw absence, fallback, identity and feasibility evidence.
type subnetPreviewEnvelope struct {
	Observation subnetPreview `json:"observation"`
	ContentHash string        `json:"content_hash"`
}

// Sealing observation bytes confers no signer, runtime or policy authority.
func sealSubnetPreview(preview subnetPreview) (subnetPreviewEnvelope, error) {
	raw, err := json.Marshal(preview)
	if err != nil {
		return subnetPreviewEnvelope{}, err
	}
	digest := sha256.Sum256(raw)
	return subnetPreviewEnvelope{Observation: preview, ContentHash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}
