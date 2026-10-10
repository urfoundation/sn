// The netuid-0 role is a separate signer-free observation service. It cannot
// supply UR miner scoring, native administrative authority or transaction caps.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/v2026/crv4"
)

const rootPolicySchema = "urnetwork-mainnet-root-observer-policy-v1"
const rootPreviewSchema = "urnetwork-mainnet-root-preview-v1"
const rootStorageProfileName = "subtensor-root-read-only-v1"
const rootProfileSource = "67dcf7f791dc495064c293f080a0702cb433e51e"
const rootPassivePolicySchema = "urnetwork-mainnet-root-observer-policy-v2"
const rootPassiveStorageProfile = "subtensor-root-passive-read-only-v2"
const rootPassiveSource = "923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d"

// Adopting a UID requires the exact hotkey registration generation, not a slot.
type rootSeatExpectation struct {
	Uid               uint16 `json:"uid"`
	RegistrationBlock uint64 `json:"registration_block"`
}

// Independently reviewed inputs have no endpoint-derived identity defaults.
// The source-to-Wasm mapping must be reviewed externally; a supplied digest is
// an expectation, not proof that this source built the running Wasm.
type rootValidatorPolicy struct {
	Schema               string                      `json:"schema"`
	Role                 string                      `json:"role"`
	Netuid               uint16                      `json:"netuid"`
	NativeChain          string                      `json:"native_chain"`
	GenesisHash          string                      `json:"genesis_hash"`
	EvmChainId           uint64                      `json:"evm_chain_id"`
	StorageProfile       string                      `json:"storage_profile"`
	RuntimeSourceCommit  string                      `json:"runtime_source_commit"`
	RuntimeVersion       crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash      string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash  string                      `json:"runtime_metadata_hash"`
	Hotkey               string                      `json:"hotkey_account_id"`
	Coldkey              string                      `json:"coldkey_account_id"`
	ExpectedSeat         *rootSeatExpectation        `json:"expected_seat"`
	MinimumStakeRao      string                      `json:"minimum_stake_rao"`
	ExpectedDelegateTake *uint16                     `json:"expected_delegate_take_u16"`
	BasketStrategy       string                      `json:"basket_strategy"`
	DelegationStrategy   string                      `json:"delegation_strategy"`
	ValidFromBlock       uint64                      `json:"valid_from_native_block,omitempty"`
	ValidThroughBlock    uint64                      `json:"valid_through_native_block,omitempty"`
}

// Rejects missing role, policy or runtime authority before any RPC request.
func (self rootValidatorPolicy) validate() error {
	passive := self.Schema == rootPassivePolicySchema
	if self.Schema != rootPolicySchema && !passive || self.Role != "bittensor-root-validator" || self.Netuid != 0 || self.EvmChainId != mainnetEvmChainId || strings.TrimSpace(self.NativeChain) == "" {
		return errors.New("root policy requires its exact schema, separate root role, netuid 0 and mainnet EVM chain ID 964")
	}
	for _, digest := range []string{self.GenesisHash, self.RuntimeCodeHash, self.RuntimeMetadataHash, self.Hotkey, self.Coldkey} {
		if !validHash(digest) || digest == "0x"+strings.Repeat("0", 64) {
			return errors.New("root policy requires approved nonzero genesis, runtime artifacts and AccountId32 identities")
		}
	}
	if !passive && (self.StorageProfile != rootStorageProfileName || self.RuntimeSourceCommit != rootProfileSource) ||
		passive && (self.StorageProfile != rootPassiveStorageProfile || !crv4.ReviewedNativeOwnerSource(self.RuntimeSourceCommit) || self.ExpectedSeat == nil || self.ExpectedSeat.RegistrationBlock == 0) {
		return errors.New("root observation source/storage profile is unreviewed; add and qualify a profile before interpreting another runtime source")
	}
	if strings.TrimSpace(self.RuntimeVersion.SpecName) == "" || self.RuntimeVersion.SpecVersion == 0 || self.RuntimeVersion.TransactionVersion == 0 || self.RuntimeVersion.StateVersion == 0 {
		return errors.New("root policy requires the complete approved runtime version")
	}
	minimum, err := strconv.ParseUint(self.MinimumStakeRao, 10, 64)
	if err != nil || minimum == 0 || strconv.FormatUint(minimum, 10) != self.MinimumStakeRao || self.ExpectedDelegateTake == nil {
		return errors.New("root policy requires canonical positive minimum stake and explicit delegate take")
	}
	if self.BasketStrategy != "accumulate_in_place" || !passive && self.DelegationStrategy != "none" || passive && self.DelegationStrategy != "observe_existing" {
		return errors.New("root observation requires accumulate_in_place with its schema's exact delegation policy")
	}
	if passive && (self.ValidFromBlock == 0 || self.ValidThroughBlock < self.ValidFromBlock) || !passive && (self.ValidFromBlock != 0 || self.ValidThroughBlock != 0) {
		return errors.New("root observation window does not match its approved strategy")
	}
	return nil
}

// Every seat is reconciled in both mapping directions at the same block.
type rootSeat struct {
	Uid               uint16 `json:"uid"`
	Hotkey            string `json:"hotkey_account_id"`
	Coldkey           string `json:"coldkey_account_id"`
	RegistrationBlock uint64 `json:"registration_block"`
	StakeRao          string `json:"stake_rao"`
	Immune            bool   `json:"immune"`
	stake             uint64
}

// A complete sample can be ready for read-only operation while every mutating
// action remains blocked. Eligibility is deliberately separated by operation.
type rootPreview struct {
	Schema                   string                      `json:"schema"`
	Role                     string                      `json:"role"`
	PolicyHash               string                      `json:"policy_hash"`
	Status                   string                      `json:"status"`
	ReadOnlyReady            bool                        `json:"read_only_ready"`
	ActivationReady          bool                        `json:"activation_ready"`
	Identity                 chainIdentity               `json:"identity"`
	RuntimeSourceCommit      string                      `json:"runtime_source_commit"`
	RuntimeVersion           crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash          string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash      string                      `json:"runtime_metadata_hash"`
	Netuid                   uint16                      `json:"netuid"`
	Seats                    []rootSeat                  `json:"seats"`
	SelectedSeat             *rootSeat                   `json:"selected_seat"`
	MaximumSeats             uint16                      `json:"maximum_seats"`
	ImmunityBlocks           uint16                      `json:"immunity_blocks"`
	LowestNonImmuneUid       *uint16                     `json:"lowest_nonimmune_uid"`
	StakeRank                int                         `json:"stake_rank"`
	RetentionMarginRao       string                      `json:"retention_margin_rao,omitempty"`
	RootWeightSettingEnabled bool                        `json:"root_weight_setting_enabled"`
	StakeThresholdRao        string                      `json:"stake_threshold_rao"`
	TaoWeightU64             string                      `json:"tao_weight_u64"`
	RootWeightsCap           uint16                      `json:"root_weights_cap_u16"`
	WeightsRateLimitBlocks   uint64                      `json:"weights_rate_limit_blocks"`
	RootStakeUnlockBlocks    uint64                      `json:"root_stake_unlock_blocks"`
	WeightEligibility        string                      `json:"custom_weight_eligibility"`
	BasketStrategy           string                      `json:"basket_strategy"`
	DelegationStrategy       string                      `json:"delegation_strategy"`
	NetworkIds               []uint16                    `json:"observed_network_ids"`
	Storage                  []rootStorageValue          `json:"storage"`
	Blockers                 []string                    `json:"read_only_blockers"`
	ActivationBlockers       []string                    `json:"activation_blockers"`
}

// Retains the complete observation with a content hash, not an approval claim.
type rootPreviewEnvelope struct {
	Observation rootPreview `json:"observation"`
	ContentHash string      `json:"content_hash"`
}

// Exact runtime bytes are authenticated before interpreting any root storage.
func (self *rpcClient) rootRuntime(ctx context.Context, policy rootValidatorPolicy) (chainIdentity, *types.Metadata, error) {
	return self.readApprovedRuntime(ctx, identityExpectation{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}, policy.RuntimeVersion, policy.RuntimeCodeHash, policy.RuntimeMetadataHash)
}

// A storage observer must authenticate its complete runtime before deriving keys.
func (self *rpcClient) readApprovedRuntime(ctx context.Context, expected identityExpectation, expectedVersion crv4.RuntimeVersionIdentity, expectedCodeHash, expectedMetadataHash string) (chainIdentity, *types.Metadata, error) {
	return self.readApprovedRuntimeAt(ctx, expected, expectedVersion, expectedCodeHash, expectedMetadataHash, "")
}

// Historical reads use the same complete runtime pin checks as current reads.
func (self *rpcClient) readApprovedRuntimeAt(ctx context.Context, expected identityExpectation, expectedVersion crv4.RuntimeVersionIdentity, expectedCodeHash, expectedMetadataHash, blockHash string) (chainIdentity, *types.Metadata, error) {
	identity, err := self.readIdentityAt(ctx, blockHash)
	if err != nil {
		return identity, nil, err
	}
	if err := expected.match(identity); err != nil {
		return identity, nil, fmt.Errorf("%w: %v", errRpcIntegrity, err)
	}
	var rawVersion json.RawMessage
	if err := self.call(ctx, "state_getRuntimeVersion", []any{identity.FinalizedHash}, &rawVersion); err != nil {
		return identity, nil, err
	}
	version, err := crv4.DecodeRuntimeVersionIdentity(rawVersion)
	if err != nil || version != expectedVersion || uint64(version.SpecVersion) != identity.RuntimeSpec || uint64(version.TransactionVersion) != identity.RuntimeTx {
		return identity, nil, fmt.Errorf("%w: root runtime version differs from approved policy: %v", errRpcIntegrity, err)
	}
	var codeHash, metadataHex string
	if err := self.call(ctx, "state_getStorageHash", []any{"0x3a636f6465", identity.FinalizedHash}, &codeHash); err != nil {
		return identity, nil, err
	}
	if !validHash(codeHash) || !strings.EqualFold(codeHash, expectedCodeHash) {
		return identity, nil, fmt.Errorf("%w: root runtime code differs from approved policy", errRpcIntegrity)
	}
	if err := self.call(ctx, "state_getMetadata", []any{identity.FinalizedHash}, &metadataHex); err != nil {
		return identity, nil, err
	}
	if !strings.HasPrefix(metadataHex, "0x") || len(metadataHex) <= 2 {
		return identity, nil, fmt.Errorf("%w: malformed root metadata hex", errRpcIntegrity)
	}
	raw, err := hex.DecodeString(metadataHex[2:])
	if err != nil {
		return identity, nil, fmt.Errorf("%w: malformed root metadata hex", errRpcIntegrity)
	}
	digest := blake2b.Sum256(raw)
	if !strings.EqualFold("0x"+hex.EncodeToString(digest[:]), expectedMetadataHash) {
		return identity, nil, fmt.Errorf("%w: root runtime metadata differs from approved policy", errRpcIntegrity)
	}
	metadata, _, err := crv4.DecodeRuntimeMetadata(metadataHex)
	if err != nil {
		return identity, nil, fmt.Errorf("%w: root metadata decode: %v", errRpcIntegrity, err)
	}
	if err := self.closeSnapshotFinality(ctx, identity); err != nil {
		return chainIdentity{}, nil, err
	}
	return identity, metadata, nil
}

// All independent reads share one deadline and bounded workers. A sample is
// published only after complete census and canonical-block rechecks succeed.
func (self *rpcClient) readRootPreview(ctx context.Context, policy rootValidatorPolicy, policyHash string) (rootPreview, error) {
	return self.readRootPreviewAt(ctx, policy, policyHash, "")
}

// Composed readiness uses the already selected finalized hash for both roles.
// The empty hash retains independent monitor sampling at its current head.
func (self *rpcClient) readRootPreviewAt(ctx context.Context, policy rootValidatorPolicy, policyHash, blockHash string) (rootPreview, error) {
	preview := rootPreview{}
	if err := policy.validate(); err != nil {
		return preview, err
	}
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	identity, metadata, err := self.readApprovedRuntimeAt(sampleCtx, identityExpectation{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}, policy.RuntimeVersion, policy.RuntimeCodeHash, policy.RuntimeMetadataHash, blockHash)
	if err != nil {
		return preview, err
	}
	passive := policy.Schema == rootPassivePolicySchema
	profile := rootStorageProfile
	if passive {
		profile = rootPassiveStorageMetadata
	}
	entries, err := profile(metadata)
	if err != nil {
		return preview, fmt.Errorf("%w: %v", errRpcIntegrity, err)
	}
	reader := &rootStorageReader{client: self, metadata: metadata, entries: entries, block: identity.FinalizedHash, valueKVs: map[string]rootStorageValue{}}
	rootArg := []byte{0, 0}
	read := func(name string, args ...[]byte) (rootStorageValue, error) {
		return reader.read(sampleCtx, name, args...)
	}
	preview = rootPreview{
		Schema: rootPreviewSchema, Role: policy.Role, PolicyHash: policyHash, Status: "blocked", Identity: identity,
		RuntimeSourceCommit: policy.RuntimeSourceCommit, RuntimeVersion: policy.RuntimeVersion, RuntimeCodeHash: policy.RuntimeCodeHash, RuntimeMetadataHash: policy.RuntimeMetadataHash,
		Seats: []rootSeat{}, NetworkIds: []uint16{}, Blockers: []string{}, WeightEligibility: "not-qualified-for-custom-weight-submission",
		BasketStrategy: policy.BasketStrategy, DelegationStrategy: policy.DelegationStrategy,
		ActivationBlockers: []string{"ROOT_AUTHORITY_CUSTODY_SUBMISSION_ADAPTERS_NOT_IMPLEMENTED", "ROOT_REGISTRATION_CAPABILITY_BLOCKED_UNLESS_APPROVED_EXISTING_SEAT", "ROOT_STAKING_CLAIM_AND_TAKE_ACTION_CAPS_NOT_IMPLEMENTED", "ROOT_CUSTOM_WEIGHT_ELIGIBILITY_AND_RUNTIME_PROFILE_NOT_QUALIFIED_FOR_MAINNET", "ROOT_BASKET_NAV_AND_ALL_STAKER_RIGHTS_NOT_AUDITED", "ROOT_RETIRED_NETWORK_DELEGATION_HISTORY_NOT_AUDITED", "SOURCE_TO_WASM_PROVENANCE_REQUIRES_INDEPENDENT_REVIEW", "UR_VALIDATOR_READINESS_IS_A_SEPARATE_ROLE"},
	}
	if passive {
		preview.Schema = "urnetwork-mainnet-root-preview-v2"
		preview.WeightEligibility = "retired-no-root-weight-call"
		preview.ActivationBlockers = []string{"PASSIVE_ROOT_OBSERVATION_HAS_NO_TRANSACTION_AUTHORITY", "ROOT_BASKET_NAV_AND_ALL_STAKER_RIGHTS_NOT_AUDITED", "ROOT_RETIRED_NETWORK_DELEGATION_HISTORY_NOT_AUDITED", "SOURCE_TO_WASM_PROVENANCE_REQUIRES_INDEPENDENT_REVIEW", "UR_VALIDATOR_READINESS_IS_A_SEPARATE_ROLE"}
		if identity.FinalizedNumber < policy.ValidFromBlock || identity.FinalizedNumber > policy.ValidThroughBlock {
			preview.Blockers = append(preview.Blockers, "SIGNED_ROOT_OBSERVATION_WINDOW_CLOSED")
		}
	}
	rootExists, err := read("NetworksAdded", rootArg)
	if err != nil {
		return rootPreview{}, err
	}
	if rootExists.data[0] != 1 {
		preview.Blockers = append(preview.Blockers, "ROOT_NETWORK_MISSING")
	}
	countValue, err := read("SubnetworkN", rootArg)
	if err != nil {
		return rootPreview{}, err
	}
	count := int(binary.LittleEndian.Uint16(countValue.data))
	if count > rootCensusLimit {
		return rootPreview{}, fmt.Errorf("%w: root seat census exceeds local bound", errRpcIntegrity)
	}
	maxValue, err := read("MaxAllowedUids", rootArg)
	if err != nil {
		return rootPreview{}, err
	}
	preview.MaximumSeats = binary.LittleEndian.Uint16(maxValue.data)
	if count > int(preview.MaximumSeats) || preview.MaximumSeats == 0 {
		return rootPreview{}, fmt.Errorf("%w: root census exceeds runtime capacity", errRpcIntegrity)
	}
	immunityValue, err := read("ImmunityPeriod", rootArg)
	if err != nil {
		return rootPreview{}, err
	}
	preview.ImmunityBlocks = binary.LittleEndian.Uint16(immunityValue.data)
	keyProbe, _ := types.CreateStorageKey(metadata, "SubtensorModule", "Keys", rootArg, rootArg)
	keys, err := reader.keys(sampleCtx, keyProbe[:34])
	if err != nil {
		return rootPreview{}, err
	}
	if len(keys) != count {
		return rootPreview{}, fmt.Errorf("%w: root Keys cardinality differs from SubnetworkN", errRpcIntegrity)
	}
	keySet := map[string]bool{}
	for _, key := range keys {
		keySet[key] = true
	}
	seats := make([]rootSeat, count)
	err = rootReadParallel(sampleCtx, count, func(ctx context.Context, index int) error {
		uidArg := binary.LittleEndian.AppendUint16(nil, uint16(index))
		key, _ := types.CreateStorageKey(metadata, "SubtensorModule", "Keys", rootArg, uidArg)
		if !keySet[key.Hex()] {
			return fmt.Errorf("%w: root Keys census has a hole or out-of-range UID", errRpcIntegrity)
		}
		hotkey, err := reader.read(ctx, "Keys", rootArg, uidArg)
		if err != nil {
			return err
		}
		if hotkey.RawStorage == nil {
			return fmt.Errorf("%w: missing root hotkey", errRpcIntegrity)
		}
		uid, err := reader.read(ctx, "Uids", rootArg, hotkey.data)
		if err != nil {
			return err
		}
		if uid.RawStorage == nil || binary.LittleEndian.Uint16(uid.data) != uint16(index) {
			return fmt.Errorf("%w: root UID/hotkey mapping disagrees", errRpcIntegrity)
		}
		coldkey, err := reader.read(ctx, "Owner", hotkey.data)
		if err != nil {
			return err
		}
		if coldkey.RawStorage == nil {
			return fmt.Errorf("%w: root seat has no recorded owner", errRpcIntegrity)
		}
		registration, err := reader.read(ctx, "BlockAtRegistration", rootArg, uidArg)
		if err != nil {
			return err
		}
		if registration.RawStorage == nil {
			return fmt.Errorf("%w: root seat has no recorded registration generation", errRpcIntegrity)
		}
		registeredAt := binary.LittleEndian.Uint64(registration.data)
		if registeredAt > identity.FinalizedNumber {
			return fmt.Errorf("%w: root registration is in the future", errRpcIntegrity)
		}
		stakeValue, err := reader.read(ctx, "TotalHotkeyAlpha", hotkey.data, rootArg)
		if err != nil {
			return err
		}
		stake := binary.LittleEndian.Uint64(stakeValue.data)
		seats[index] = rootSeat{Uid: uint16(index), Hotkey: hotkey.EffectiveScale, Coldkey: coldkey.EffectiveScale, RegistrationBlock: registeredAt, StakeRao: strconv.FormatUint(stake, 10), Immune: identity.FinalizedNumber-registeredAt < uint64(preview.ImmunityBlocks), stake: stake}
		return nil
	})
	if err != nil {
		return rootPreview{}, err
	}
	preview.Seats = seats
	zeroAccount := make([]byte, 32)
	reverseProbe, _ := types.CreateStorageKey(metadata, "SubtensorModule", "Uids", rootArg, zeroAccount)
	reverseKeys, err := reader.keys(sampleCtx, reverseProbe[:34])
	if err != nil {
		return rootPreview{}, err
	}
	if len(reverseKeys) != count {
		return rootPreview{}, fmt.Errorf("%w: root Uids cardinality differs from SubnetworkN", errRpcIntegrity)
	}
	expectedReverseKeys := map[string]bool{}
	for _, seat := range seats {
		hotkey, _ := hex.DecodeString(seat.Hotkey[2:])
		key, _ := types.CreateStorageKey(metadata, "SubtensorModule", "Uids", rootArg, hotkey)
		expectedReverseKeys[key.Hex()] = true
	}
	for _, key := range reverseKeys {
		if !expectedReverseKeys[key] {
			return rootPreview{}, fmt.Errorf("%w: root reverse census has a foreign seat", errRpcIntegrity)
		}
	}
	hotkey, _ := hex.DecodeString(policy.Hotkey[2:])
	coldkey, _ := hex.DecodeString(policy.Coldkey[2:])
	selectedUid, err := read("Uids", rootArg, hotkey)
	if err != nil {
		return rootPreview{}, err
	}
	if selectedUid.RawStorage == nil {
		preview.Blockers = append(preview.Blockers, "ROOT_SEAT_NOT_REGISTERED")
	} else {
		uid := binary.LittleEndian.Uint16(selectedUid.data)
		if int(uid) >= count || !strings.EqualFold(seats[uid].Hotkey, policy.Hotkey) {
			return rootPreview{}, fmt.Errorf("%w: selected root UID contradicts full census", errRpcIntegrity)
		}
		selected := seats[uid]
		preview.SelectedSeat = &selected
		if !strings.EqualFold(selected.Coldkey, policy.Coldkey) {
			preview.Blockers = append(preview.Blockers, "ROOT_OWNER_MISMATCH")
		}
		if policy.ExpectedSeat == nil || *policy.ExpectedSeat != (rootSeatExpectation{Uid: uid, RegistrationBlock: selected.RegistrationBlock}) {
			preview.Blockers = append(preview.Blockers, "ROOT_REGISTRATION_GENERATION_NOT_APPROVED")
		}
		minimum, _ := strconv.ParseUint(policy.MinimumStakeRao, 10, 64)
		if selected.stake < minimum {
			preview.Blockers = append(preview.Blockers, "ROOT_STAKE_BELOW_POLICY_MINIMUM")
		}
		if !passive {
			weights, err := read("Weights", rootArg, binary.LittleEndian.AppendUint16(nil, uid))
			if err != nil {
				return rootPreview{}, err
			}
			_, weightsCount, _ := rootVector(weights.data, 4, 0)
			if weightsCount != 0 {
				preview.Blockers = append(preview.Blockers, "ROOT_EXISTING_CUSTOM_WEIGHTS_REQUIRE_EXPLICIT_TRANSITION")
			}
		}
	}
	var lowest *rootSeat
	for index := range seats {
		seat := &seats[index]
		if !seat.Immune && (lowest == nil || seat.stake < lowest.stake || seat.stake == lowest.stake && (seat.RegistrationBlock < lowest.RegistrationBlock || seat.RegistrationBlock == lowest.RegistrationBlock && seat.Uid < lowest.Uid)) {
			lowest = seat
		}
		if preview.SelectedSeat != nil && seat.stake > preview.SelectedSeat.stake {
			preview.StakeRank++
		}
	}
	if lowest != nil {
		uid := lowest.Uid
		preview.LowestNonImmuneUid = &uid
		if preview.SelectedSeat != nil {
			if preview.SelectedSeat.stake >= lowest.stake {
				preview.RetentionMarginRao = strconv.FormatUint(preview.SelectedSeat.stake-lowest.stake, 10)
			} else {
				preview.RetentionMarginRao = "-" + strconv.FormatUint(lowest.stake-preview.SelectedSeat.stake, 10)
			}
			if count == int(preview.MaximumSeats) && uid == preview.SelectedSeat.Uid {
				preview.Blockers = append(preview.Blockers, "ROOT_SEAT_IS_CURRENT_PRUNING_CANDIDATE")
			}
		}
	}
	if preview.SelectedSeat != nil {
		preview.StakeRank++
	}
	delegate, err := read("Delegates", hotkey)
	if err != nil {
		return rootPreview{}, err
	}
	if delegate.RawStorage == nil || binary.LittleEndian.Uint16(delegate.data) != *policy.ExpectedDelegateTake {
		preview.Blockers = append(preview.Blockers, "ROOT_DELEGATE_TAKE_NOT_APPROVED")
	}
	auto, err := read("AutoParentDelegationEnabled", hotkey)
	if err != nil {
		return rootPreview{}, err
	}
	if auto.data[0] != 0 && !passive {
		preview.Blockers = append(preview.Blockers, "ROOT_AUTOMATIC_DELEGATION_ENABLED")
	}
	for _, scalar := range []struct {
		name string
		args [][]byte
	}{
		{name: "RootWeightSettingEnabled"}, {name: "RootWeightsCap", args: [][]byte{rootArg}}, {name: "StakeThreshold"}, {name: "TaoWeight"},
		{name: "WeightsSetRateLimit", args: [][]byte{rootArg}}, {name: "RootStakeUnlockInterval"}, {name: "BasketShares", args: [][]byte{hotkey}},
		{name: "BasketRate", args: [][]byte{hotkey}}, {name: "BasketClaimed", args: [][]byte{hotkey, coldkey}}, {name: "LastUpdate", args: [][]byte{rootArg}},
	} {
		if passive && rootRetiredWeightStorage(scalar.name) {
			continue
		}
		value, err := read(scalar.name, scalar.args...)
		if err != nil {
			return rootPreview{}, err
		}
		switch scalar.name {
		case "RootWeightSettingEnabled":
			preview.RootWeightSettingEnabled = value.data[0] == 1
		case "RootWeightsCap":
			preview.RootWeightsCap = binary.LittleEndian.Uint16(value.data)
		case "StakeThreshold":
			preview.StakeThresholdRao = strconv.FormatUint(binary.LittleEndian.Uint64(value.data), 10)
		case "TaoWeight":
			preview.TaoWeightU64 = strconv.FormatUint(binary.LittleEndian.Uint64(value.data), 10)
		case "WeightsSetRateLimit":
			preview.WeightsRateLimitBlocks = binary.LittleEndian.Uint64(value.data)
		case "RootStakeUnlockInterval":
			preview.RootStakeUnlockBlocks = binary.LittleEndian.Uint64(value.data)
		case "LastUpdate":
			body, n, _ := rootVector(value.data, 8, 0)
			if n != count {
				return rootPreview{}, fmt.Errorf("%w: root LastUpdate census differs from SubnetworkN", errRpcIntegrity)
			}
			for i := 0; i < n; i++ {
				if binary.LittleEndian.Uint64(body[i*8:]) > identity.FinalizedNumber {
					return rootPreview{}, fmt.Errorf("%w: root weight update is in the future", errRpcIntegrity)
				}
			}
		}
	}
	networkProbe, _ := types.CreateStorageKey(metadata, "SubtensorModule", "NetworksAdded", rootArg)
	networkKeys, err := reader.keys(sampleCtx, networkProbe[:32])
	if err != nil {
		return rootPreview{}, err
	}
	rootSeen := false
	for _, key := range networkKeys {
		raw, _ := hex.DecodeString(key[2:])
		if len(raw) != 34 {
			return rootPreview{}, fmt.Errorf("%w: malformed network census key", errRpcIntegrity)
		}
		netuid := binary.LittleEndian.Uint16(raw[32:])
		rootSeen = rootSeen || netuid == 0
		preview.NetworkIds = append(preview.NetworkIds, netuid)
	}
	if rootExists.data[0] == 1 && !rootSeen {
		return rootPreview{}, fmt.Errorf("%w: root missing from network key census", errRpcIntegrity)
	}
	delegationPresent := make([]bool, len(preview.NetworkIds))
	err = rootReadParallel(sampleCtx, len(preview.NetworkIds), func(ctx context.Context, index int) error {
		netuidArg := binary.LittleEndian.AppendUint16(nil, preview.NetworkIds[index])
		if _, err := reader.read(ctx, "NetworksAdded", netuidArg); err != nil {
			return err
		}
		for _, query := range []struct {
			name   string
			args   [][]byte
			suffix int
		}{
			{name: "ChildKeys", args: [][]byte{hotkey, netuidArg}}, {name: "ParentKeys", args: [][]byte{hotkey, netuidArg}}, {name: "PendingChildKeys", args: [][]byte{netuidArg, hotkey}, suffix: 8},
		} {
			value, err := reader.read(ctx, query.name, query.args...)
			if err != nil {
				return err
			}
			_, n, _ := rootVector(value.data, 40, query.suffix)
			delegationPresent[index] = delegationPresent[index] || n != 0
		}
		return nil
	})
	if err != nil {
		return rootPreview{}, err
	}
	for _, present := range delegationPresent {
		if present && !passive {
			preview.Blockers = append(preview.Blockers, "ROOT_EXISTING_OR_PENDING_DELEGATION_REQUIRES_EXPLICIT_TRANSITION")
			break
		}
	}
	var confirmedHash string
	if err := self.call(sampleCtx, "chain_getBlockHash", []any{identity.FinalizedNumber}, &confirmedHash); err != nil {
		return rootPreview{}, err
	}
	if !validHash(confirmedHash) || !strings.EqualFold(confirmedHash, identity.FinalizedHash) {
		return rootPreview{}, fmt.Errorf("%w: root finalized block changed during census", errRpcIntegrity)
	}
	if err := self.closeSnapshotFinality(sampleCtx, identity); err != nil {
		return rootPreview{}, err
	}
	if err := sampleCtx.Err(); err != nil {
		return rootPreview{}, err
	}
	preview.Storage = reader.evidence()
	preview.ReadOnlyReady = len(preview.Blockers) == 0
	if preview.ReadOnlyReady {
		preview.Status = "ready"
	}
	return preview, nil
}

// The entire observation, including explicit absence and blockers, is sealed.
func sealRootPreview(preview rootPreview) (rootPreviewEnvelope, error) {
	raw, err := json.Marshal(preview)
	if err != nil {
		return rootPreviewEnvelope{}, err
	}
	digest := sha256.Sum256(raw)
	return rootPreviewEnvelope{Observation: preview, ContentHash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}
