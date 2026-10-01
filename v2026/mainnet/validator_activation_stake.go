// Native stake admission bounds the two original roles at one canonical block.
// Runtime API floors, projected activity and applied consensus are distinct.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	native "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
)

const validatorActivationStakeSchema = "urnetwork-mainnet-validator-stake-capacity-v1"
const validatorStakeQ32One = uint64(1) << 32

// All quantities refer to the same runtime census. Q32 bounds divide by 2^32;
// the floor in rao is not an exact fixed-point stake or raw alpha balance.
type validatorActivationStakeRole struct {
	Role                    string             `json:"role"`
	Registration            subnetRegistration `json:"registration"`
	WeightedStakeFloorRao   uint64             `json:"weighted_stake_floor_rao"`
	ValidatorPermit         bool               `json:"validator_permit"`
	RegisteredSubnetOwner   bool               `json:"registered_subnet_owner"`
	NonSelfStakeAndPermit   bool               `json:"non_self_stake_and_permit"`
	LastUpdate              uint64             `json:"last_update"`
	RuntimeActivityEligible bool               `json:"runtime_activity_eligible"`
	FirstNormalizedLowerQ32 uint64             `json:"first_normalized_lower_q32"`
	CapacityShareLowerQ32   uint64             `json:"capacity_share_lower_q32"`
	ActiveShareLowerQ32     uint64             `json:"active_share_lower_q32"`
}

// Capacity admits fresh starts' stake prerequisite only. Even a positive
// active bound does not authenticate applied weights, clipping or emissions.
type validatorActivationStakeReadiness struct {
	Schema                        string                          `json:"schema"`
	ContentHash                   string                          `json:"content_hash"`
	EvidenceHash                  string                          `json:"evidence_hash"`
	PlanHash                      string                          `json:"bootstrap_plan_hash"`
	FinalizedNumber               uint64                          `json:"finalized_number"`
	FinalizedHash                 string                          `json:"finalized_hash"`
	CensusSize                    uint16                          `json:"census_size"`
	StakeThresholdRao             uint64                          `json:"stake_threshold_rao"`
	FilteredStakeFloorSumRao      uint64                          `json:"filtered_stake_floor_sum_rao"`
	FilteredStakeUpperSumRao      uint64                          `json:"filtered_stake_upper_sum_rao"`
	ActivityCutoffFactorMilli     uint32                          `json:"activity_cutoff_factor_milli"`
	Tempo                         uint16                          `json:"tempo"`
	ActivityCutoffBlocks          uint64                          `json:"activity_cutoff_blocks"`
	Kappa                         uint16                          `json:"kappa"`
	RequiredMajorityShareQ32      uint64                          `json:"required_majority_share_q32"`
	CapacityAdmitted              bool                            `json:"capacity_admitted"`
	ActiveMajorityBoundObserved   bool                            `json:"active_majority_bound_observed"`
	AppliedWeightsInfluenceProven bool                            `json:"applied_weights_influence_proven"`
	Roles                         [2]validatorActivationStakeRole `json:"roles"`
}

// Structural journal checks cannot turn retained observations into authority.
// Actual admission always re-reads the original approval and complete census.
func (self validatorActivationStakeReadiness) validate(readiness validatorActivationReadiness) error {
	hash := self.ContentHash
	self.ContentHash = ""
	if self.Schema != validatorActivationStakeSchema || !planSha256(hash) || hash != rootObjectHash(self) || !planSha256(self.EvidenceHash) ||
		self.PlanHash != readiness.PlanHash || self.FinalizedNumber != readiness.FinalizedNumber || self.FinalizedHash != readiness.FinalizedHash || readiness.Native == nil ||
		self.CensusSize < 2 || self.CensusSize > 4096 || self.FilteredStakeFloorSumRao == 0 || self.FilteredStakeUpperSumRao <= self.FilteredStakeFloorSumRao ||
		self.FilteredStakeUpperSumRao > math.MaxInt64 || self.ActivityCutoffBlocks != max(uint64(1), uint64(self.ActivityCutoffFactorMilli)*uint64(self.Tempo)/1000) ||
		self.RequiredMajorityShareQ32 != validatorStakeRequiredShare(self.Kappa) || !self.CapacityAdmitted || self.AppliedWeightsInfluenceProven || len(readiness.Roles) != 2 {
		return errors.New("validator stake projection lacks its exact bounded native scope")
	}
	for i, role := range self.Roles {
		original := readiness.Roles[i]
		if original.Observed == nil || role.Role != original.Role || role.Registration != *original.Observed || int(role.Registration.Uid) >= int(self.CensusSize) ||
			original.ValidatorPermit == nil || role.ValidatorPermit != *original.ValidatorPermit || role.LastUpdate != readiness.Native.Roles[i].LastUpdate || role.LastUpdate > self.FinalizedNumber ||
			role.RuntimeActivityEligible != (self.FinalizedNumber-role.LastUpdate <= self.ActivityCutoffBlocks) ||
			role.NonSelfStakeAndPermit != (role.RegisteredSubnetOwner || role.ValidatorPermit && role.WeightedStakeFloorRao >= self.StakeThresholdRao) || !role.NonSelfStakeAndPermit ||
			role.FirstNormalizedLowerQ32 == 0 || role.FirstNormalizedLowerQ32 > validatorStakeQ32One || role.CapacityShareLowerQ32 == 0 || role.CapacityShareLowerQ32 > validatorStakeQ32One ||
			role.ActiveShareLowerQ32 > validatorStakeQ32One || !role.RuntimeActivityEligible && role.ActiveShareLowerQ32 != 0 {
			return errors.New("validator stake role or activity differs from the original native observation")
		}
	}
	if self.Roles[0].Role != "majority" || self.Roles[1].Role != "secondary" || self.Roles[0].CapacityShareLowerQ32 <= self.RequiredMajorityShareQ32 ||
		self.ActiveMajorityBoundObserved != (self.Roles[0].ActiveShareLowerQ32 > self.RequiredMajorityShareQ32) {
		return errors.New("validator stake projection overstates its conservative majority bound")
	}
	return nil
}

// Both tails of the reviewed weighted median must be strictly dominated.
// This is at least a strict half; endpoint kappa values cannot pass this bound.
func validatorStakeRequiredShare(kappa uint16) uint64 {
	value := uint64(kappa) * validatorStakeQ32One / math.MaxUint16
	return max(validatorStakeQ32One/2, value, validatorStakeQ32One-value)
}

// Each actual stake lies in [floor,floor+1). Threshold filtering precedes
// normalization and preserves the registered-owner exception. The first sum
// includes inactive/non-permitted seats: those can quantize a small role to zero
// before masks are applied. All arithmetic is bounded integer arithmetic.
// Each returned role has first-normalization, capacity and active-share bounds.
func validatorActivationStakeBounds(entries []crv4.ValidatorStakeCensusEntry, threshold uint64, owner *uint16, active []bool, uids [2]uint16) ([2][3]uint64, uint64, uint64, error) {
	var result [2][3]uint64
	if len(entries) < 2 || len(entries) > 4096 || len(active) != len(entries) || uids[0] == uids[1] || int(uids[0]) >= len(entries) || int(uids[1]) >= len(entries) || owner != nil && int(*owner) >= len(entries) {
		return result, 0, 0, errors.New("validator stake bound has an incomplete census")
	}
	var lowerSum, upperSum uint64
	filtered, permitted := make([]bool, len(entries)), make([]bool, len(entries))
	for i, entry := range entries {
		isOwner := owner != nil && int(*owner) == i
		filtered[i] = isOwner || entry.TotalStakeFloorRao >= threshold
		permitted[i] = filtered[i] && (isOwner || entry.ValidatorPermit)
		if entry.TotalStakeFloorRao > math.MaxInt64 {
			return result, 0, 0, errors.New("validator stake floor exceeds the runtime fixed-point range")
		}
		if filtered[i] {
			// Refuse when the unknown fractions could overflow the runtime sum.
			if entry.TotalStakeFloorRao >= math.MaxInt64-upperSum {
				return result, 0, 0, errors.New("validator stake fractional upper sum exceeds the runtime range")
			}
			lowerSum += entry.TotalStakeFloorRao
			upperSum += entry.TotalStakeFloorRao + 1
		}
	}
	if lowerSum == 0 {
		return result, 0, 0, errors.New("validator stake has no positive integer lower bound")
	}
	divide := func(numerator, denominator uint64) uint64 {
		value := new(big.Int).Mul(new(big.Int).SetUint64(numerator), new(big.Int).SetUint64(validatorStakeQ32One))
		value.Quo(value, new(big.Int).SetUint64(denominator))
		if value.Cmp(new(big.Int).SetUint64(validatorStakeQ32One)) > 0 {
			return validatorStakeQ32One
		}
		return value.Uint64()
	}
	upper := make([]uint64, len(entries))
	for i, entry := range entries {
		if filtered[i] {
			upper[i] = divide(entry.TotalStakeFloorRao+1, lowerSum)
		}
	}
	for role, uid := range uids {
		if !permitted[uid] {
			continue
		}
		lower := divide(entries[uid].TotalStakeFloorRao, upperSum)
		result[role][0] = lower
		if lower == 0 {
			continue
		}
		capacitySum, activeSum := lower, lower
		for peer := range entries {
			if peer != int(uid) && permitted[peer] {
				capacitySum += upper[peer]
				if active[peer] {
					activeSum += upper[peer]
				}
			}
		}
		result[role][1] = divide(lower, capacitySum)
		if active[uid] {
			result[role][2] = divide(lower, activeSum)
		}
	}
	return result, lowerSum, upperSum, nil
}

// Runtime execution is restricted to the exact selective-metagraph arguments
// and block of this census. The generic native client gains no state_call path.
type validatorActivationStakeReadClient struct {
	*evmNativeReadClient
	input string
	block string
}

// All ordinary reads borrow the existing route; no write or subscription is
// admitted and the runtime reply retains the one-MiB transport ceiling.
func (self *validatorActivationStakeReadClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	if method != "state_call" {
		return self.evmNativeReadClient.CallContext(ctx, result, method, args...)
	}
	if len(args) != 3 || args[0] != "SubnetInfoRuntimeApi_get_selective_metagraph" || args[1] != self.input || args[2] != self.block {
		return errors.New("validator stake runtime call differs from its fixed census scope")
	}
	return self.client.callAdmittedRead(ctx, method, args, result, false, maxRpcReplyBytes)
}

// The pinned production epoch derives activity from factor and tempo, not the
// deprecated ActivityCutoff value or the prior epoch's stored Active vector.
var validatorActivationStakeStorageSpecs = []rootStorageSpec{
	{name: "ActivityCutoffFactorMilli", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u32"},
	{name: "Tempo", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "Kappa", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "LastUpdate", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64s"},
}

// Original native admission precedes the new capacity read. A single caller
// budget and exact signed route cover both; old native-only admit is unchanged.
func observeValidatorActivationWithStake(ctx context.Context, approval validatorActivationApproval, now func() time.Time) (bootstrapChainPreparation, bootstrapChainReadiness, *validatorActivationReadiness, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(approval.Plan.Route.ReadRetrySeconds)*time.Second)
	defer cancel()
	preparation, readiness, result, err := observeValidatorActivation(ctx, approval, now)
	if err != nil {
		return preparation, readiness, nil, err
	}
	client, err := newOwnedSubmissionClient(approval.Plan.Route)
	if err != nil {
		return preparation, readiness, nil, err
	}
	defer client.httpClient.CloseIdleConnections()
	result.Stake, err = client.observeValidatorActivationStake(ctx, preparation, readiness, result.Native)
	if err != nil {
		return preparation, readiness, nil, err
	}
	if err := errors.Join(result.validate(approval.Plan), ctx.Err()); err != nil {
		return preparation, readiness, nil, err
	}
	return preparation, readiness, result, nil
}

// The complete current census is authenticated against the original artifact
// and every current hotkey/permit. Closing reads preserve both native anchors.
func (self *rpcClient) observeValidatorActivationStake(ctx context.Context, preparation bootstrapChainPreparation, readiness bootstrapChainReadiness, prior *validatorActivationNativeReadiness) (*validatorActivationStakeReadiness, error) {
	if ctx == nil || self == nil || prior == nil || readiness.Census == nil || !readiness.ObservationComplete || readiness.PlanHash != preparation.Plan.ContentHash || len(readiness.UrValidators) != 2 || len(preparation.Plan.ValidatorInspections) != 2 {
		return nil, errors.New("validator stake observation requires the original complete native scope")
	}
	approval := preparation.Plan.ValidatorInspections[0].Approval
	pin := approval.Proposal.Runtime
	if !mainnetRuntimeCodecSource(pin.SourceCommit) {
		return nil, errors.New("validator stake normalization source profile is unreviewed")
	}
	census := readiness.Census.Observation
	identity, metadata, err := self.readApprovedRuntimeAt(ctx, identityExpectation{NativeChain: approval.NativeChain, GenesisHash: fmt.Sprintf("0x%x", pin.GenesisHash), EvmChainId: preparation.Plan.Config.Network.EvmChainId}, pin.Version, fmt.Sprintf("0x%x", pin.CodeHash), fmt.Sprintf("0x%x", pin.MetadataHash), census.Identity.FinalizedHash)
	if err != nil {
		return nil, err
	}
	if identity.FinalizedNumber != census.Identity.FinalizedNumber || len(census.Seats) < 2 || len(census.Seats) > 4096 {
		return nil, errors.New("validator stake snapshot or complete census differs")
	}
	entries, err := observationStorageProfile(metadata, validatorActivationStakeStorageSpecs)
	if err != nil {
		return nil, err
	}
	var uids [2]uint16
	for i, role := range readiness.UrValidators {
		if role.Observed == nil || *role.Observed != prior.Roles[i].Registration || len(role.ObservationBlockers) != 0 || role.Role != []string{"majority", "secondary"}[i] {
			return nil, errors.New("validator stake role differs from the original admitted registration")
		}
		uids[i] = role.Observed.Uid
	}
	genesis, _ := native.NewHashFromHexString(identity.GenesisHash)
	block, _ := native.NewHashFromHexString(identity.FinalizedHash)
	input := binary.LittleEndian.AppendUint16(nil, preparation.Plan.Config.Netuid)
	input = append(input, 5*4)
	for _, index := range []uint16{0, 30, 52, 57, 69} {
		input = binary.LittleEndian.AppendUint16(input, index)
	}
	bridge := &validatorActivationStakeReadClient{evmNativeReadClient: &evmNativeReadClient{client: self}, input: "0x" + hex.EncodeToString(input), block: block.Hex()}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: bridge}, GenesisHash: genesis}
	maximum := min(uint32(4096), approval.MaximumSubnetUids, preparation.Plan.ValidatorInspections[1].Approval.MaximumSubnetUids)
	observed, err := crv4.ReadValidatorStakeCensusAtContext(ctx, chain, crv4.ValidatorIdentityQuery{GenesisHash: genesis, BlockHash: block, BlockNumber: identity.FinalizedNumber, Netuid: preparation.Plan.Config.Netuid, UID: uids[0], MaximumSubnetUIDs: maximum}, crv4.RuntimeArtifactIdentity{Version: pin.Version, CodeHash: fmt.Sprintf("0x%x", pin.CodeHash), MetadataHash: fmt.Sprintf("0x%x", pin.MetadataHash)})
	if err != nil {
		return nil, err
	}
	selected := observed.Selected
	if len(observed.Entries) != len(census.Seats) || common.Hash(selected.Identity.Hotkey).Hex() != readiness.UrValidators[0].Expected.Hotkey || common.Hash(selected.Identity.Coldkey).Hex() != readiness.UrValidators[0].Expected.Coldkey ||
		selected.SubnetOwnerPresent != (census.SubnetOwnerHotkey != nil) || selected.SubnetOwnerPresent && common.Hash(selected.SubnetOwnerHotkey).Hex() != *census.SubnetOwnerHotkey {
		return nil, errors.New("validator weighted stake source differs from the original current census")
	}
	for i, entry := range observed.Entries {
		if census.Seats[i].Uid != uint16(i) || common.Hash(entry.Hotkey).Hex() != census.Seats[i].Hotkey || entry.ValidatorPermit != census.Seats[i].ValidatorPermit {
			return nil, errors.New("validator weighted stake peer identity or permit differs from the complete census")
		}
	}
	reader := &rootStorageReader{client: self, metadata: metadata, entries: entries, specs: validatorActivationStakeStorageSpecs, block: identity.FinalizedHash, valueKVs: map[string]rootStorageValue{}}
	values := map[string]rootStorageValue{}
	for _, spec := range validatorActivationStakeStorageSpecs {
		value, err := reader.read(ctx, spec.name, binary.LittleEndian.AppendUint16(nil, preparation.Plan.Config.Netuid))
		if err != nil {
			return nil, err
		}
		values[spec.name] = value
	}
	factor, tempo := binary.LittleEndian.Uint32(values["ActivityCutoffFactorMilli"].data), binary.LittleEndian.Uint16(values["Tempo"].data)
	cutoff := max(uint64(1), uint64(factor)*uint64(tempo)/1000)
	updates, count, err := rootVector(values["LastUpdate"].data, 8, 0)
	if err != nil || count != len(observed.Entries) {
		return nil, errors.New("validator stake activity vector does not cover the complete census")
	}
	active := make([]bool, count)
	for i := range active {
		last := binary.LittleEndian.Uint64(updates[i*8:])
		if last > identity.FinalizedNumber {
			return nil, errors.New("validator stake activity is in the future")
		}
		active[i] = identity.FinalizedNumber-last <= cutoff
	}
	var owner *uint16
	if selected.SubnetOwnerRegistered {
		owner = &selected.SubnetOwnerUID
	}
	bounds, lowerSum, upperSum, err := validatorActivationStakeBounds(observed.Entries, selected.StakeThresholdRao, owner, active, uids)
	if err != nil {
		return nil, err
	}
	result := &validatorActivationStakeReadiness{Schema: validatorActivationStakeSchema, PlanHash: readiness.PlanHash, FinalizedHash: identity.FinalizedHash, FinalizedNumber: identity.FinalizedNumber, CensusSize: uint16(count), StakeThresholdRao: selected.StakeThresholdRao,
		FilteredStakeFloorSumRao: lowerSum, FilteredStakeUpperSumRao: upperSum, ActivityCutoffFactorMilli: factor, Tempo: tempo, ActivityCutoffBlocks: cutoff, Kappa: binary.LittleEndian.Uint16(values["Kappa"].data)}
	result.RequiredMajorityShareQ32 = validatorStakeRequiredShare(result.Kappa)
	for i, uid := range uids {
		entry, role := observed.Entries[uid], readiness.UrValidators[i]
		isOwner := owner != nil && *owner == uid
		result.Roles[i] = validatorActivationStakeRole{Role: role.Role, Registration: *role.Observed, WeightedStakeFloorRao: entry.TotalStakeFloorRao, ValidatorPermit: entry.ValidatorPermit, RegisteredSubnetOwner: isOwner,
			NonSelfStakeAndPermit: isOwner || entry.ValidatorPermit && entry.TotalStakeFloorRao >= selected.StakeThresholdRao, LastUpdate: binary.LittleEndian.Uint64(updates[int(uid)*8:]), RuntimeActivityEligible: active[uid],
			FirstNormalizedLowerQ32: bounds[i][0], CapacityShareLowerQ32: bounds[i][1], ActiveShareLowerQ32: bounds[i][2]}
		if result.Roles[i].LastUpdate != prior.Roles[i].LastUpdate || !result.Roles[i].NonSelfStakeAndPermit || bounds[i][0] == 0 || bounds[i][1] == 0 {
			return nil, errors.New("validator role has changed activity, insufficient weighted stake/permit or unproven nonzero fixed-point capacity")
		}
	}
	if result.Roles[0].CapacityShareLowerQ32 <= result.RequiredMajorityShareQ32 {
		return nil, errors.New("validator majority is not proven after fractional uncertainty and both fixed-point normalizations")
	}
	result.CapacityAdmitted = true
	result.ActiveMajorityBoundObserved = result.Roles[0].ActiveShareLowerQ32 > result.RequiredMajorityShareQ32
	for _, anchor := range []struct {
		number uint64
		hash   string
	}{{number: prior.ActivationBlock, hash: prior.ActivationHash}, {number: identity.FinalizedNumber, hash: identity.FinalizedHash}} {
		var confirmed string
		if err := self.call(ctx, "chain_getBlockHash", []any{anchor.number}, &confirmed); err != nil {
			return nil, err
		}
		if !strings.EqualFold(confirmed, anchor.hash) {
			return nil, errors.New("validator stake native anchor changed during observation")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.EvidenceHash = rootObjectHash(struct {
		PlanHash string
		Census   crv4.ValidatorStakeCensusObservation
		Storage  []rootStorageValue
	}{PlanHash: readiness.PlanHash, Census: observed, Storage: reader.evidence()})
	result.ContentHash = rootObjectHash(*result)
	return result, nil
}
