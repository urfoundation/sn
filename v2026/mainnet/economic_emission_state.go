// Pending budgets and epoch counters are source-pinned inputs to a future
// denominator proof. Post-block storage alone cannot reconstruct intra-block
// accrual, fixed-point rounding, zero-incentive fallback or recycling transfers.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strconv"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Every selected field has an independently checked wire shape and query
// default. Mechanism count uses Twox64Concat, unlike the subnet Identity maps.
var economicEmissionStorageSpecs = []rootStorageSpec{
	{name: "NetworksAdded", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bool"},
	{name: "NetworkRegisteredAt", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "RegisteredSubnetCounter", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "SubnetworkN", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "MechanismCountCurrent", keys: []string{"u16"}, hashers: []string{"twox64concat"}, value: "u8"},
	{name: "SubnetEpochIndex", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "LastEpochBlock", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "Tempo", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "PendingEpochAt", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "PendingServerEmission", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "PendingValidatorEmission", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "PendingRootAlphaDivs", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "PendingOwnerCut", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "SubnetAlphaOutEmission", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "OwnerCutEnabled", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bool"},
	{name: "SubnetOwnerCut", value: "u16"},
}

// The caller retains partial raw storage when a later key or invariant fails.
func readEconomicEmissionState(ctx context.Context, client *rpcClient, metadata *types.Metadata, policy economicEmissionPolicy, boundary economicEmissionBoundary) (state economicEmissionState, resultErr error) {
	state.Boundary = boundary
	entries, err := observationStorageProfile(metadata, economicEmissionStorageSpecs)
	if err != nil {
		return state, err
	}
	modeKey, modeFallback, err := recycleModeStorage(metadata, policy.Netuid)
	if err != nil {
		return state, err
	}
	reader := rootStorageReader{client: client, metadata: metadata, entries: entries, specs: economicEmissionStorageSpecs, block: boundary.Hash, valueKVs: map[string]rootStorageValue{}}
	defer func() { state.Storage = reader.evidence() }()
	values := make([]rootStorageValue, len(economicEmissionStorageSpecs))
	netuidArg := binary.LittleEndian.AppendUint16(nil, policy.Netuid)
	if err := rootReadParallel(ctx, len(values), func(ctx context.Context, index int) error {
		spec := economicEmissionStorageSpecs[index]
		var args [][]byte
		if len(spec.keys) != 0 {
			args = [][]byte{netuidArg}
		}
		value, err := reader.read(ctx, spec.name, args...)
		values[index] = value
		return err
	}); err != nil {
		return state, err
	}
	valueKVs := map[string]rootStorageValue{}
	for _, value := range values {
		valueKVs[value.Name] = value
	}
	u64 := func(name string) uint64 { return binary.LittleEndian.Uint64(valueKVs[name].data) }
	state.SubnetRegistration, state.SubnetGeneration = u64("NetworkRegisteredAt"), u64("RegisteredSubnetCounter")
	state.SubnetUids = binary.LittleEndian.Uint16(valueKVs["SubnetworkN"].data)
	state.MechanismCount = valueKVs["MechanismCountCurrent"].data[0]
	state.Epoch, state.LastEpochBlock, state.PendingEpochAt = u64("SubnetEpochIndex"), u64("LastEpochBlock"), u64("PendingEpochAt")
	state.Tempo = binary.LittleEndian.Uint16(valueKVs["Tempo"].data)
	state.PendingServerAlpha, state.PendingValidatorAlpha = strconv.FormatUint(u64("PendingServerEmission"), 10), strconv.FormatUint(u64("PendingValidatorEmission"), 10)
	state.PendingRootAlpha, state.PendingOwnerAlpha = strconv.FormatUint(u64("PendingRootAlphaDivs"), 10), strconv.FormatUint(u64("PendingOwnerCut"), 10)
	state.BlockAlphaOut = strconv.FormatUint(u64("SubnetAlphaOutEmission"), 10)
	state.OwnerCutEnabled, state.OwnerCutParts = valueKVs["OwnerCutEnabled"].data[0] == 1, binary.LittleEndian.Uint16(valueKVs["SubnetOwnerCut"].data)
	state.ModeStorageKey = modeKey.Hex()
	if err := client.callWithStorageAbsence(ctx, "state_getStorage", []any{modeKey.Hex(), boundary.Hash}, &state.ModeRawStorage, true); err != nil {
		return state, err
	}
	mode := modeFallback
	if state.ModeRawStorage != nil {
		mode, err = rootReceiptHex(*state.ModeRawStorage, 1)
	}
	if err != nil || len(mode) != 1 || mode[0] > 1 {
		return state, errors.New("native incentive recycle mode is not the reviewed exact enum")
	}
	state.RecycleMode = "Burn"
	if mode[0] == 1 {
		state.RecycleMode = "Recycle"
	}
	if valueKVs["NetworksAdded"].data[0] != 1 || valueKVs["NetworkRegisteredAt"].RawStorage == nil ||
		state.SubnetRegistration != *policy.SubnetRegistrationBlock || state.SubnetGeneration != *policy.SubnetGeneration || state.SubnetRegistration > boundary.Number {
		return state, errors.New("native incentive subnet existence or retained generation changed")
	}
	if state.MechanismCount != 1 || state.SubnetUids > policy.MaximumUids || state.LastEpochBlock > boundary.Number {
		return state, errors.New("native incentive state exceeds reviewed one-mechanism, UID or epoch bounds")
	}
	return state, nil
}

// Event evidence cannot resolve M or Q: this block's newly accrued tranche was
// drained before post-state; normalized incentive/dividend terms and each
// fixed-point truncation are not reconstructed by summing the emitted vector.
func economicEmissionDenominatorEvidence(before, after economicEmissionState, events []economicEmissionEvent, contextEvents []economicEmissionContextEvent) (economicEmissionDenominator, error) {
	result := economicEmissionDenominator{
		Status: "unresolved-no-incentive-event", ObservedIncentiveAlpha: "0", PriorPendingServerAlpha: before.PendingServerAlpha, AfterPendingServerAlpha: after.PendingServerAlpha,
		Blockers: []string{
			"pre-withholding native miner tranche requires complete intra-block accrual and drain inputs; pending snapshots and stored alpha-out are insufficient",
			"runtime I32F32 normalization, I96F32 multiplication and per-UID u64 truncation are not reconstructed; quantization tolerance remains unknown",
			"zero-incentive fallback may redirect the miner tranche to dividends; emitted incentive zero does not establish a zero denominator",
			"UID event slots do not authenticate recipient hotkey generations, provider entitlement, collateral capture or an actual owner recycling transfer",
		},
	}
	if before.RecycleMode != "Recycle" || after.RecycleMode != "Recycle" {
		result.Blockers = append(result.Blockers, "observed Burn mode does not satisfy the owner-recycle mode precondition")
	}
	if before.SubnetUids != after.SubnetUids {
		result.Blockers = append(result.Blockers, "registration count changed across the block; event UID slots require execution-time generation reconciliation")
	}
	var lastTempo *uint16
	var ownerChange *economicEmissionContextEvent
	for index := range contextEvents {
		context := &contextEvents[index]
		switch context.Kind {
		case "SubtensorModule.TempoSet":
			lastTempo = context.Tempo
		case "SubtensorModule.SubnetOwnerChanged":
			if ownerChange != nil || context.Phase != "Initialization" || len(events) != 1 || events[0].Kind != "SubtensorModule.IncentiveAlphaEmittedToMiners" || context.EventIndex >= events[0].EventIndex {
				return result, errors.New("native incentive owner change is not the single pre-incentive initialization takeover")
			}
			ownerChange = context
		default:
			return result, errors.New("native incentive execution context is unknown")
		}
	}
	if lastTempo != nil && *lastTempo != after.Tempo || lastTempo == nil && before.Tempo != after.Tempo {
		return result, errors.New("native incentive tempo event and storage disagree")
	}
	if ownerChange != nil {
		result.Blockers = append(result.Blockers, "conviction takeover may append or replace an owner neuron before incentives; recipient hotkey generations and actual owner recycling remain unresolved")
	}
	if len(events) == 0 {
		anchorReset := after.LastEpochBlock != before.LastEpochBlock && after.LastEpochBlock == after.Boundary.Number && lastTempo != nil
		if after.Epoch != before.Epoch || after.LastEpochBlock != before.LastEpochBlock && (!anchorReset || before.Epoch == math.MaxUint64) {
			return result, errors.New("native incentive epoch advanced without its reviewed terminal event")
		}
		if anchorReset {
			result.Status = "unresolved-schedule-reset-observed"
		}
		return result, nil
	}
	if len(events) != 1 {
		return result, errors.New("native incentive epoch evidence is ambiguous")
	}
	event := events[0]
	expectedEpoch := before.Epoch
	if expectedEpoch < math.MaxUint64 {
		expectedEpoch++
	}
	switch event.Kind {
	case "SubtensorModule.IncentiveAlphaEmittedToMiners":
		result.Status, result.ObservedIncentiveAlpha = "unresolved-incentive-observed", event.TotalAlpha
		result.ZeroIncentiveFallback = event.TotalAlpha == "0"
		// A takeover calls register_neuron at most once before Yuma. Its event
		// permits that one append, never an arbitrary post-block census length.
		uidCountMatches := len(event.AlphaByUid) == int(before.SubnetUids) || ownerChange != nil && len(event.AlphaByUid) == int(before.SubnetUids)+1
		if !uidCountMatches || after.Epoch != expectedEpoch || after.LastEpochBlock != after.Boundary.Number || after.PendingServerAlpha != "0" {
			return result, errors.New("native incentive event and epoch/drain snapshots disagree")
		}
	case "SubtensorModule.EpochSkipped":
		result.Status = "unresolved-epoch-skipped"
		if after.Epoch != expectedEpoch || after.LastEpochBlock != after.Boundary.Number {
			return result, errors.New("native incentive skipped event and consumed epoch disagree")
		}
	case "SubtensorModule.EpochDeferred":
		result.Status = "unresolved-epoch-deferred"
		if after.Epoch != before.Epoch || after.LastEpochBlock != before.LastEpochBlock || after.PendingEpochAt != event.ToBlock {
			return result, errors.New("native incentive deferred event and pending epoch disagree")
		}
	default:
		return result, errors.New("native incentive denominator received an unknown event")
	}
	return result, nil
}
