// Native activation prerequisites come from the original signed producer scope
// and exact owned-RPC reads. They provide no operator, contract or signer fence.
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/urfoundation/sn/v2026/crv4"
)

const validatorActivationNativeSchema = "urnetwork-mainnet-validator-activation-native-v1"

// The signed maximum age admits new registrations before their first weights.
// It does not prove weighted stake, economic majority or operator health.
type validatorActivationNativeRole struct {
	Role                 string             `json:"role"`
	Registration         subnetRegistration `json:"registration"`
	LastUpdate           uint64             `json:"last_update"`
	MaximumLastUpdateAge uint64             `json:"maximum_last_update_age"`
}

// This bounded projection preserves the exact consumed block and the digest of
// its raw evidence. Finality/storage remain owned-RPC assertions, not proofs.
type validatorActivationNativeReadiness struct {
	Schema                       string                           `json:"schema"`
	EvidenceHash                 string                           `json:"evidence_hash"`
	RuntimeVersion               crv4.RuntimeVersionIdentity      `json:"runtime_version"`
	RuntimeCodeHash              string                           `json:"runtime_code_hash"`
	RuntimeMetadataHash          string                           `json:"runtime_metadata_hash"`
	NativeEpoch                  uint64                           `json:"native_epoch"`
	ApprovedFirstNativeEpoch     uint64                           `json:"approved_first_native_epoch"`
	ApprovedThroughNativeEpoch   uint64                           `json:"approved_through_native_epoch"`
	ActivationBlock              uint64                           `json:"activation_block"`
	ActivationHash               string                           `json:"activation_hash"`
	ActivationEpoch              uint64                           `json:"activation_epoch"`
	ActivationPendingServerAlpha uint64                           `json:"activation_pending_server_alpha"`
	MechanismCount               uint8                            `json:"mechanism_count"`
	RecycleMode                  string                           `json:"recycle_mode"`
	Roles                        [2]validatorActivationNativeRole `json:"roles"`
}

// Old journals may omit this new projection and still reconcile their original
// consumed starts. A new observation always constructs and validates it.
func (self validatorActivationNativeReadiness) validate(readiness validatorActivationReadiness) error {
	if self.Schema != validatorActivationNativeSchema || !planSha256(self.EvidenceHash) ||
		self.RuntimeVersion.SpecName == "" || self.RuntimeVersion.SpecVersion == 0 || !rootCanonicalHash(self.RuntimeCodeHash) || !rootCanonicalHash(self.RuntimeMetadataHash) ||
		self.ApprovedFirstNativeEpoch == 0 || self.ApprovedThroughNativeEpoch < self.ApprovedFirstNativeEpoch ||
		self.NativeEpoch < self.ApprovedFirstNativeEpoch || self.NativeEpoch > self.ApprovedThroughNativeEpoch ||
		self.ActivationBlock == 0 || self.ActivationBlock > readiness.FinalizedNumber || !rootCanonicalHash(self.ActivationHash) ||
		self.ActivationEpoch != self.ApprovedFirstNativeEpoch || self.ActivationPendingServerAlpha != 0 || self.MechanismCount != 1 || self.RecycleMode != "Recycle" || len(readiness.Roles) != 2 {
		return fmt.Errorf("%w: validator activation native scope, epoch or checkpoint differs", errRpcIntegrity)
	}
	for i, role := range self.Roles {
		observed := readiness.Roles[i]
		fresh := max(role.LastUpdate, role.Registration.RegistrationBlock)
		if role.Role != observed.Role || observed.Observed == nil || role.Registration != *observed.Observed ||
			observed.ApprovedCheckpointHash != self.ActivationHash || observed.ObservedCheckpointHash != self.ActivationHash ||
			role.MaximumLastUpdateAge == 0 || fresh == 0 || fresh > readiness.FinalizedNumber || readiness.FinalizedNumber-fresh > role.MaximumLastUpdateAge {
			return fmt.Errorf("%w: validator activation native role or activity differs", errRpcIntegrity)
		}
	}
	return nil
}

// This is the complete narrow wire profile, independent of unrelated economic
// history or post-start emission measurements. Drain and activity defaults must
// retain the producer's reviewed zero/empty encodings.
var validatorActivationNativeStorageSpecs = []rootStorageSpec{
	{name: "NetworksAdded", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bool"},
	{name: "NetworkRegisteredAt", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "RegisteredSubnetCounter", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "SubnetOwner", keys: []string{"u16"}, hashers: []string{"identity"}, value: "account"},
	{name: "SubnetworkN", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "MechanismCountCurrent", keys: []string{"u16"}, hashers: []string{"twox64concat"}, value: "u8"},
	{name: "SubnetEpochIndex", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "PendingServerEmission", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "LastUpdate", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64s"},
}

// One read owns the evidence used for the retained digest; large census vectors
// are not copied into every per-unit journal entry.
type validatorActivationNativeSample struct {
	Identity chainIdentity      `json:"identity"`
	Storage  []rootStorageValue `json:"storage"`
	Mode     rootStorageValue   `json:"recycle_mode"`
	values   map[string]rootStorageValue
}

// Both boundaries use the exact producer artifact independently of the older
// owner-trim census artifact. An approved hash never authorizes another codec.
func (self *rpcClient) readValidatorActivationNativeSample(ctx context.Context, preparation bootstrapChainPreparation, readiness bootstrapChainReadiness, hash string, checkpoint bool) (validatorActivationNativeSample, error) {
	empty := validatorActivationNativeSample{}
	approval := preparation.Plan.ValidatorInspections[0].Approval
	pin := approval.Proposal.Runtime
	identity, metadata, err := self.readApprovedRuntimeAt(ctx, identityExpectation{NativeChain: approval.NativeChain,
		GenesisHash: fmt.Sprintf("0x%x", pin.GenesisHash), EvmChainId: preparation.Plan.Config.Network.EvmChainId},
		pin.Version, fmt.Sprintf("0x%x", pin.CodeHash), fmt.Sprintf("0x%x", pin.MetadataHash), hash)
	if err != nil {
		return empty, err
	}
	entries, err := observationStorageProfile(metadata, validatorActivationNativeStorageSpecs)
	if err != nil {
		return empty, fmt.Errorf("%w: validator activation native storage: %v", errRpcIntegrity, err)
	}
	for _, spec := range validatorActivationNativeStorageSpecs {
		if spec.name != "PendingServerEmission" && spec.name != "LastUpdate" {
			continue
		}
		for _, value := range entries[spec.name].Fallback {
			if value != 0 {
				return empty, fmt.Errorf("%w: validator activation native %s default changed", errRpcIntegrity, spec.name)
			}
		}
	}
	modeKey, _, err := recycleModeStorage(metadata, preparation.Plan.Config.Netuid)
	if err != nil {
		return empty, fmt.Errorf("%w: validator activation recycle codec: %v", errRpcIntegrity, err)
	}
	reader := &rootStorageReader{client: self, metadata: metadata, entries: entries, specs: validatorActivationNativeStorageSpecs,
		block: hash, valueKVs: map[string]rootStorageValue{}}
	sample := validatorActivationNativeSample{Identity: identity, values: map[string]rootStorageValue{}}
	arg := binary.LittleEndian.AppendUint16(nil, preparation.Plan.Config.Netuid)
	for _, spec := range validatorActivationNativeStorageSpecs {
		// Activity belongs to the current complete census; the drain belongs
		// only to the signed economic checkpoint, never a later block.
		if checkpoint && (spec.name == "LastUpdate" || spec.name == "SubnetworkN") || !checkpoint && spec.name == "PendingServerEmission" {
			continue
		}
		value, err := reader.read(ctx, spec.name, arg)
		if err != nil {
			return empty, err
		}
		sample.values[spec.name] = value
	}
	census := readiness.Census.Observation
	if sample.values["NetworksAdded"].data[0] != 1 || sample.values["NetworkRegisteredAt"].RawStorage == nil ||
		binary.LittleEndian.Uint64(sample.values["NetworkRegisteredAt"].data) != census.SubnetRegistrationBlock ||
		binary.LittleEndian.Uint64(sample.values["RegisteredSubnetCounter"].data) != census.SubnetGeneration ||
		census.SubnetRegistrationBlock > identity.FinalizedNumber || sample.values["SubnetOwner"].RawStorage == nil ||
		sample.values["SubnetOwner"].EffectiveScale != census.SubnetOwnerColdkey || sample.values["MechanismCountCurrent"].data[0] != 1 {
		return empty, fmt.Errorf("%w: validator activation native subnet generation, owner or mechanism differs", errRpcIntegrity)
	}
	sample.Mode = rootStorageValue{Name: "RecycleOrBurn", Key: modeKey.Hex(), ValueSource: "finalized-storage"}
	if err := self.callWithStorageAbsence(ctx, "state_getStorage", []any{modeKey.Hex(), hash}, &sample.Mode.RawStorage, true); err != nil {
		return empty, err
	}
	if sample.Mode.RawStorage == nil || *sample.Mode.RawStorage != "0x01" {
		return empty, fmt.Errorf("%w: validator activation requires explicit finalized Recycle", errRpcIntegrity)
	}
	sample.Mode.EffectiveScale = *sample.Mode.RawStorage
	sample.Storage = reader.evidence()
	return sample, ctx.Err()
}

// This reader requires an already complete current census. It never loads a
// key, producer state, approval substitute or unrelated historical audit.
func (self *rpcClient) observeValidatorActivationNative(ctx context.Context, preparation bootstrapChainPreparation, readiness bootstrapChainReadiness) (*validatorActivationNativeReadiness, error) {
	if ctx == nil || self == nil || readiness.Census == nil || !readiness.ObservationComplete || len(readiness.UrValidators) != 2 ||
		readiness.PlanHash != preparation.Plan.ContentHash || len(preparation.Plan.ValidatorInspections) != 2 {
		return nil, fmt.Errorf("%w: validator activation native observation has no complete original scope", errRpcIntegrity)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	approval := preparation.Plan.ValidatorInspections[0].Approval
	production := approval.Production
	if production == nil {
		return nil, fmt.Errorf("%w: validator activation native production approval is absent", errRpcIntegrity)
	}
	block := production.ActivationNativeBlock
	if block == 0 {
		block = approval.ValidFromNativeBlock
	}
	identity := readiness.Census.Observation.Identity
	if block == 0 || block > identity.FinalizedNumber {
		return nil, fmt.Errorf("%w: validator activation signed checkpoint is not finalized", errRpcIntegrity)
	}
	current, err := self.readValidatorActivationNativeSample(ctx, preparation, readiness, identity.FinalizedHash, false)
	if err != nil {
		return nil, err
	}
	if current.Identity.FinalizedNumber != identity.FinalizedNumber || int(binary.LittleEndian.Uint16(current.values["SubnetworkN"].data)) != len(readiness.Census.Observation.Seats) {
		return nil, fmt.Errorf("%w: validator activation native census changed at one hash", errRpcIntegrity)
	}
	result := &validatorActivationNativeReadiness{Schema: validatorActivationNativeSchema,
		RuntimeVersion: approval.Proposal.Runtime.Version, RuntimeCodeHash: fmt.Sprintf("0x%x", approval.Proposal.Runtime.CodeHash), RuntimeMetadataHash: fmt.Sprintf("0x%x", approval.Proposal.Runtime.MetadataHash),
		NativeEpoch: binary.LittleEndian.Uint64(current.values["SubnetEpochIndex"].data), ApprovedFirstNativeEpoch: approval.FirstNativeEpoch,
		ApprovedThroughNativeEpoch: production.ValidThroughNativeEpoch, ActivationBlock: block, ActivationHash: fmt.Sprintf("0x%x", production.ActivationNativeHash), MechanismCount: 1, RecycleMode: "Recycle"}
	if result.NativeEpoch < result.ApprovedFirstNativeEpoch || result.NativeEpoch > result.ApprovedThroughNativeEpoch {
		return nil, fmt.Errorf("%w: validator activation signed native epoch window is closed", errRpcIntegrity)
	}
	updates, count, err := rootVector(current.values["LastUpdate"].data, 8, 0)
	if err != nil || count != len(readiness.Census.Observation.Seats) {
		return nil, fmt.Errorf("%w: validator activation activity vector does not cover the exact census", errRpcIntegrity)
	}
	for i, role := range readiness.UrValidators {
		if role.Observed == nil || int(role.Observed.Uid) >= count {
			return nil, fmt.Errorf("%w: validator activation native role is absent", errRpcIntegrity)
		}
		last := binary.LittleEndian.Uint64(updates[8*int(role.Observed.Uid):])
		fresh := max(last, role.Observed.RegistrationBlock)
		if fresh == 0 || fresh > identity.FinalizedNumber || identity.FinalizedNumber-fresh > production.MaximumLastUpdateAge {
			return nil, fmt.Errorf("%w: validator activation %s native activity is stale or in the future", errRpcIntegrity, role.Role)
		}
		result.Roles[i] = validatorActivationNativeRole{Role: role.Role, Registration: *role.Observed, LastUpdate: last, MaximumLastUpdateAge: production.MaximumLastUpdateAge}
	}
	checkpoint, err := self.readValidatorActivationNativeSample(ctx, preparation, readiness, result.ActivationHash, true)
	if err != nil {
		return nil, err
	}
	result.ActivationEpoch = binary.LittleEndian.Uint64(checkpoint.values["SubnetEpochIndex"].data)
	result.ActivationPendingServerAlpha = binary.LittleEndian.Uint64(checkpoint.values["PendingServerEmission"].data)
	if checkpoint.Identity.FinalizedNumber != block || result.ActivationEpoch != result.ApprovedFirstNativeEpoch || result.ActivationPendingServerAlpha != 0 {
		return nil, fmt.Errorf("%w: validator activation signed checkpoint is not the approved drained first epoch", errRpcIntegrity)
	}
	if checkpoint.Identity.FinalizedHash == current.Identity.FinalizedHash {
		for name, value := range checkpoint.values {
			if earlier, exists := current.values[name]; exists && (earlier.EffectiveScale != value.EffectiveScale || (earlier.RawStorage == nil) != (value.RawStorage == nil)) {
				return nil, fmt.Errorf("%w: validator activation %s changed at one native hash", errRpcIntegrity, name)
			}
		}
	}
	// Recheck both anchors after all storage, so an earlier successful census
	// or checkpoint cannot survive a later canonical-branch contradiction.
	for _, anchor := range []chainIdentity{checkpoint.Identity, current.Identity} {
		var confirmed string
		if err := self.call(ctx, "chain_getBlockHash", []any{anchor.FinalizedNumber}, &confirmed); err != nil {
			return nil, err
		}
		if !strings.EqualFold(confirmed, anchor.FinalizedHash) {
			return nil, fmt.Errorf("%w: validator activation native anchor changed during observation", errRpcIntegrity)
		}
	}
	result.EvidenceHash = rootObjectHash(struct {
		PlanHash   string                          `json:"bootstrap_plan_hash"`
		Current    validatorActivationNativeSample `json:"current"`
		Checkpoint validatorActivationNativeSample `json:"checkpoint"`
	}{PlanHash: preparation.Plan.ContentHash, Current: current, Checkpoint: checkpoint})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
