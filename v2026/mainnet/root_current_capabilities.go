// Current root participation is a registered, staked basket fund. This offline
// metadata report describes reviewed call shapes and source rules; it grants no
// signing, current-state, economic-policy or runtime-authority capability.
package main

import (
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

const rootCurrentCapabilitiesSchema = "urnetwork-mainnet-root-current-capabilities-v1"

// Public protocol metadata, independently executed from the official v470
// artifact in runtime-470-audit.md. Equality is not live chain or source proof.
const rootCurrentAuditedMetadataHash = "0x8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34"

// The signed origin comes from reviewed source, not from SCALE metadata. Every
// listed operation still needs its own custody, policy and execution admission.
type rootCurrentCallCapability struct {
	Call                 string `json:"call"`
	PalletIndex          uint8  `json:"pallet_index"`
	ExpectedCallIndex    uint8  `json:"expected_call_index"`
	ObservedCallIndex    *uint8 `json:"observed_call_index"`
	MetadataStatus       string `json:"metadata_status"`
	ReviewedSignedOrigin string `json:"reviewed_signed_origin"`
	Purpose              string `json:"purpose"`
	MutationAdapter      string `json:"current_root_mutation_adapter"`
}

// Runtime capacity is read from storage. No metadata catalogue or source
// default can establish a current seat, pruning candidate or stake amount.
type rootCurrentRegistrationCapability struct {
	FreeCapacityStakeRule string  `json:"free_capacity_stake_rule"`
	FullCapacityStakeRule string  `json:"full_capacity_stake_rule"`
	BurnProtection        string  `json:"native_registration_burn_protection"`
	CurrentEligibility    string  `json:"current_eligibility"`
	CurrentMaximumSeats   *uint16 `json:"current_maximum_seats"`
	CurrentApplicantStake *string `json:"current_applicant_root_stake_rao"`
	CurrentCandidateStake *string `json:"current_pruning_candidate_root_stake_rao"`
}

// The status separates current on-chain participation from an observer process
// and from a native signing service. Metadata alone proves none of those states.
type rootCurrentParticipantStatus struct {
	Membership              string `json:"current_membership"`
	RootStake               string `json:"current_root_stake"`
	BasketAccrual           string `json:"current_basket_accrual"`
	Custody                 string `json:"current_custody"`
	ObservationService      string `json:"continuous_observation_service"`
	PeriodicHotkeyCall      string `json:"periodic_root_hotkey_call"`
	RootHotkeyDevice        string `json:"root_hotkey_device"`
	RootColdkeyDevice       string `json:"root_coldkey_device"`
	StakeClaimColdkeyDevice string `json:"stake_claim_coldkey_device"`
}

// A report is an offline planning result, never a bootstrap approval or a
// successful current-root outcome. Unknowns remain explicit JSON nulls/statuses.
type rootCurrentCapabilities struct {
	Schema                      string                            `json:"schema"`
	Status                      string                            `json:"status"`
	RuntimeSourceCommit         string                            `json:"reviewed_source_commit"`
	MetadataHash                string                            `json:"metadata_blake2b_256"`
	MetadataFileHash            string                            `json:"metadata_file_sha256"`
	AuditedMetadataMatched      bool                              `json:"audited_metadata_matched"`
	MetadataInterfaceCompatible bool                              `json:"metadata_interface_compatible"`
	RuntimeSourceProven         bool                              `json:"runtime_source_proven"`
	CurrentRuntimeVerified      bool                              `json:"current_runtime_verified"`
	RootWeightCallStatus        string                            `json:"set_root_weights_status"`
	RetiredClaimControlStatus   string                            `json:"retired_claim_control_status"`
	RootWeightsProxy            string                            `json:"reviewed_root_weights_proxy"`
	Calls                       []rootCurrentCallCapability       `json:"calls"`
	Registration                rootCurrentRegistrationCapability `json:"registration"`
	Participant                 rootCurrentParticipantStatus      `json:"participant"`
	UrValidatorRequirement      string                            `json:"ur_validator_requirement"`
	MutationExecution           string                            `json:"current_root_mutation_execution"`
	MutationCustodyRequirement  string                            `json:"current_root_mutation_custody_requirement"`
	PlanningOnly                bool                              `json:"planning_only"`
	NativeSigning               bool                              `json:"native_signing"`
	NetworkEffects              bool                              `json:"network_effects"`
	ActivationReady             bool                              `json:"activation_ready"`
	Blockers                    []string                          `json:"blockers"`
}

// Exact field order, widths and indices distinguish supported interfaces from
// same-named incompatible calls. Source behavior is pinned separately above.
type rootCurrentCallSpec struct {
	name       string
	index      uint8
	fieldNames []string
	shapes     []string
	origin     string
	purpose    string
}

// Each invocation owns its catalogue; no caller can alter later reports.
func rootCurrentCallSpecs() []rootCurrentCallSpec {
	return []rootCurrentCallSpec{
		{name: "root_register", index: 62, fieldNames: []string{"hotkey"}, shapes: []string{"account"}, origin: "root-owning-coldkey", purpose: "register-root-seat"},
		{name: "add_stake", index: 2, fieldNames: []string{"hotkey", "netuid", "amount_staked"}, shapes: []string{"account", "u16", "u64"}, origin: "staker-coldkey", purpose: "add-root-principal-with-netuid-zero"},
		{name: "remove_stake", index: 3, fieldNames: []string{"hotkey", "netuid", "amount_unstaked"}, shapes: []string{"account", "u16", "u64"}, origin: "staker-coldkey", purpose: "withdraw-root-principal-subject-to-unlock"},
		{name: "claim_root", index: 121, fieldNames: []string{"subnets"}, shapes: []string{"u16s"}, origin: "staker-coldkey-or-explicit-root-claim-proxy", purpose: "claim-entitlements-across-validator-funds-subnets-argument-ignored"},
		{name: "claim_root_with_hotkey", index: 148, fieldNames: []string{"hotkey"}, shapes: []string{"account"}, origin: "staker-coldkey-or-explicit-root-claim-proxy", purpose: "claim-one-fund-entitlement-into-root-stake"},
		{name: "stake_into_basket", index: 147, fieldNames: []string{"hotkey", "amount_staked"}, shapes: []string{"account", "u64"}, origin: "depositor-coldkey", purpose: "optional-purchase-of-basket-entitlement-not-root-principal"},
		{name: "swap_basket", index: 150, fieldNames: []string{"hotkey", "origin_netuid", "destination_netuid", "amount", "min_amount_out"}, shapes: []string{"account", "u16", "u16", "u64", "u64"}, origin: "root-owning-coldkey-or-explicit-basket-trading-proxy", purpose: "optional-approved-basket-trade"},
		{name: "swap_basket_many", index: 151, fieldNames: []string{"hotkey", "legs"}, shapes: []string{"account", "basket-legs"}, origin: "root-owning-coldkey-or-explicit-basket-trading-proxy", purpose: "optional-approved-basket-trades"},
		{name: "set_auto_parent_delegation_enabled", index: 135, fieldNames: []string{"hotkey", "enabled"}, shapes: []string{"account", "bool"}, origin: "root-owning-coldkey", purpose: "configure-future-automatic-parent-delegation"},
		{name: "decrease_take", index: 65, fieldNames: []string{"hotkey", "take"}, shapes: []string{"account", "u16"}, origin: "root-owning-coldkey", purpose: "separately-approved-delegate-take-change"},
		{name: "increase_take", index: 66, fieldNames: []string{"hotkey", "take"}, shapes: []string{"account", "u16"}, origin: "root-owning-coldkey", purpose: "separately-approved-rate-limited-delegate-take-change"},
		{name: "set_children", index: 67, fieldNames: []string{"hotkey", "netuid", "children"}, shapes: []string{"account", "u16", "links"}, origin: "root-owning-coldkey", purpose: "separately-approved-delegation-on-nonroot-subnets"},
	}
}

// BoundedVec is represented as a sequence, possibly through one-field wrappers.
// Its maximum is a source rule, so metadata shape does not invent a trade limit.
func rootCurrentCallType(metadata *types.Metadata, id types.Si1LookupTypeID, shape string, depth int) bool {
	if shape != "basket-legs" && shape != "basket-leg" {
		return rootSigningType(metadata, id, shape, depth)
	}
	if depth > 16 {
		return false
	}
	entry := metadata.AsMetadataV14.EfficientLookup[id.Int64()]
	if entry == nil {
		return false
	}
	def := entry.Def
	if def.IsComposite && len(def.Composite.Fields) == 1 {
		return rootCurrentCallType(metadata, def.Composite.Fields[0].Type, shape, depth+1)
	}
	if shape == "basket-legs" {
		return def.IsSequence && rootCurrentCallType(metadata, def.Sequence.Type, "basket-leg", depth+1)
	}
	if !def.IsTuple || len(def.Tuple) != 4 {
		return false
	}
	for index, part := range []string{"u16", "u16", "u64", "u64"} {
		if !rootCurrentCallType(metadata, def.Tuple[index], part, depth+1) {
			return false
		}
	}
	return true
}

// Only authenticated metadata enters this projection. A compatible interface
// still cannot establish source-to-Wasm equality, current storage or custody.
func inspectRootCurrentCapabilities(metadata *types.Metadata, metadataHash, fileHash, source string) (rootCurrentCapabilities, error) {
	if metadata == nil || metadata.Version != 14 || !crv4.ReviewedNativeOwnerSource(source) || !rootCanonicalHash(metadataHash) || !planSha256(fileHash) {
		return rootCurrentCapabilities{}, errors.New("root capabilities require pinned metadata14 and the exact reviewed current-root source")
	}
	var variants []types.Si1Variant
	pallets := 0
	palletIndexKVs := map[uint8]bool{}
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if palletIndexKVs[uint8(pallet.Index)] {
			return rootCurrentCapabilities{}, errors.New("root capability pallet indices are duplicated")
		}
		palletIndexKVs[uint8(pallet.Index)] = true
		if pallet.Name != "SubtensorModule" {
			continue
		}
		pallets++
		if pallet.Index != rootCallPallet || !pallet.HasCalls {
			return rootCurrentCapabilities{}, errors.New("root capability pallet index or call registry differs")
		}
		entry := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
		if entry == nil || !entry.Def.IsVariant {
			return rootCurrentCapabilities{}, errors.New("root capability pallet index or call registry differs")
		}
		variants = entry.Def.Variant.Variants
	}
	if pallets != 1 {
		return rootCurrentCapabilities{}, errors.New("root capability pallet is absent or duplicated")
	}
	callKVs := map[string]types.Si1Variant{}
	indexKVs := map[uint8]bool{}
	for _, variant := range variants {
		name := string(variant.Name)
		if _, exists := callKVs[name]; exists || indexKVs[uint8(variant.Index)] {
			return rootCurrentCapabilities{}, errors.New("root capability call names or indices are duplicated")
		}
		callKVs[name], indexKVs[uint8(variant.Index)] = variant, true
	}
	result := rootCurrentCapabilities{
		Schema: rootCurrentCapabilitiesSchema, Status: "metadata-compatible-current-state-unknown", RuntimeSourceCommit: source,
		MetadataHash: metadataHash, MetadataFileHash: fileHash, AuditedMetadataMatched: metadataHash == rootCurrentAuditedMetadataHash,
		MetadataInterfaceCompatible: true, RootWeightCallStatus: "absent-retired-in-reviewed-source", RetiredClaimControlStatus: "absent-retired-in-reviewed-source",
		RootWeightsProxy: "denies-all-calls-in-reviewed-source", Calls: []rootCurrentCallCapability{}, PlanningOnly: true,
		Registration:               rootCurrentRegistrationCapability{FreeCapacityStakeRule: "no-displacement-stake-comparison", FullCapacityStakeRule: "applicant-root-stake-at-least-lowest-nonimmune-member;no-nonimmune-member-refuses-registration", BurnProtection: "root_register-has-no-maximum-burn-argument", CurrentEligibility: "unknown"},
		Participant:                rootCurrentParticipantStatus{Membership: "unknown", RootStake: "unknown", BasketAccrual: "unknown", Custody: "unknown", ObservationService: "unverified", PeriodicHotkeyCall: "not-required-for-unchanged-accumulation-under-reviewed-source", RootHotkeyDevice: "unspecified-separate-hardware", RootColdkeyDevice: "unspecified-independent-custody", StakeClaimColdkeyDevice: "unspecified-independent-custody"},
		UrValidatorRequirement:     "separate-standard-sn-validator-majority-and-secondary-admissions;root-cannot-count-as-ur-validator",
		MutationExecution:          "not-implemented;required-only-for-separately-approved-mutations",
		MutationCustodyRequirement: "separately-approved-mutations-only;no-native-device-required-for-observation-or-unchanged-participation-under-reviewed-source",
		Blockers:                   []string{"CURRENT_RUNTIME_AND_FINALITY_UNVERIFIED", "ROOT_MEMBERSHIP_GENERATION_AND_CAPACITY_UNKNOWN", "ROOT_PRINCIPAL_STAKE_AND_RETENTION_UNKNOWN", "ROOT_BASKET_ACCRUAL_AND_COMPLETE_ENTITLEMENTS_UNKNOWN", "CONTINUOUS_OBSERVATION_AND_REPAIR_UNVERIFIED"},
	}
	if _, found := callKVs["set_root_weights"]; found {
		result.RootWeightCallStatus, result.MetadataInterfaceCompatible = "unexpected-present", false
		result.Blockers = append(result.Blockers, "RETIRED_ROOT_WEIGHT_CALL_PRESENT")
	}
	for _, name := range []string{"set_root_claim_type", "sudo_set_num_root_claims"} {
		if _, found := callKVs[name]; found {
			result.RetiredClaimControlStatus, result.MetadataInterfaceCompatible = "unexpected-present", false
			result.Blockers = append(result.Blockers, "RETIRED_ROOT_CLAIM_CONTROL_PRESENT:"+name)
		}
	}
	for _, spec := range rootCurrentCallSpecs() {
		call := rootCurrentCallCapability{Call: spec.name, PalletIndex: rootCallPallet, ExpectedCallIndex: spec.index, MetadataStatus: "absent", ReviewedSignedOrigin: spec.origin, Purpose: spec.purpose, MutationAdapter: "not-implemented-for-current-root"}
		if variant, found := callKVs[spec.name]; found {
			index := uint8(variant.Index)
			call.ObservedCallIndex, call.MetadataStatus = &index, "schema-mismatch"
			matches := index == spec.index && len(variant.Fields) == len(spec.fieldNames)
			if matches {
				for fieldIndex, field := range variant.Fields {
					if !field.HasName || string(field.Name) != spec.fieldNames[fieldIndex] || !rootCurrentCallType(metadata, field.Type, spec.shapes[fieldIndex], 0) {
						matches = false
						break
					}
				}
			}
			if matches {
				call.MetadataStatus = "shape-supported"
			}
		}
		if call.MetadataStatus != "shape-supported" {
			result.MetadataInterfaceCompatible = false
			result.Blockers = append(result.Blockers, fmt.Sprintf("ROOT_CALL_INTERFACE_UNAVAILABLE:%s", spec.name))
		}
		if version, reviewed := rootRegisterReviewedSpec(source); spec.name == "root_register" && reviewed && call.MetadataStatus == "shape-supported" {
			call.MutationAdapter = fmt.Sprintf("root-register-v1;requires-separate-%d-policy-operator-custody-and-whole-reducible-balance-exposure-approval", version)
			result.MutationExecution = fmt.Sprintf("root_register-only:separate-%d-root-register-domain;other-current-root-mutations-not-implemented", version)
		}
		result.Calls = append(result.Calls, call)
	}
	if !result.MetadataInterfaceCompatible {
		result.Status = "metadata-interface-unavailable"
		result.Participant.PeriodicHotkeyCall = "unknown-for-supplied-metadata"
	}
	return result, nil
}
