// A bounded qualifier obtains its entire predicate state from the approved RPC
// at one authenticated block. Imported snapshots and caller proof flags are absent.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Separate storage avoids silently changing the retained census/plan schema.
var ownerTrimWindowStorageSpecs = []rootStorageSpec{
	{name: "Tempo", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
	{name: "LastEpochBlock", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "PendingEpochAt", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "BlocksSinceLastStep", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	{name: "AdminFreezeWindow", value: "u16"},
	{name: "LastHotkeySwapOnNetuid", keys: []string{"u16", "account"}, hashers: []string{"identity", "blake128concat"}, value: "u64"},
	{name: "ColdkeySwapAnnouncementDelay", value: "u32"},
	{name: "ColdkeySwapAnnouncements", keys: []string{"account"}, hashers: []string{"twox64concat"}, value: "coldkey-announcement", optional: true},
	{name: "ColdkeySwapDisputes", keys: []string{"account"}, hashers: []string{"twox64concat"}, value: "u32", optional: true},
	{name: "SubnetUidToLeaseId", keys: []string{"u16"}, hashers: []string{"twox64concat"}, value: "u32", optional: true},
}

// Recognize the exact mortality extension and authenticated build-selected
// cooldown. Runtime provenance is still a separate execution gate.
func ownerTrimWindowMetadata(metadata *types.Metadata) (uint64, error) {
	if metadata == nil || metadata.Version != 14 || metadata.AsMetadataV14.Extrinsic.Version != 4 {
		return 0, errors.New("owner trim window requires the reviewed metadata14/extrinsic4 profile")
	}
	mortalityCount := 0
	for _, extension := range metadata.AsMetadataV14.Extrinsic.SignedExtensions {
		if extension.Identifier == "CheckMortality" {
			mortalityCount++
			if !rootSigningType(metadata, extension.Type, "era", 0) || !rootSigningType(metadata, extension.AdditionalSigned, "account", 0) {
				return 0, errors.New("owner trim mortality extension changed")
			}
		}
	}
	if mortalityCount != 1 {
		return 0, errors.New("owner trim mortality extension is missing or duplicated")
	}
	palletCount, constantCount := 0, 0
	var interval uint64
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "SubtensorModule" {
			continue
		}
		palletCount++
		for _, constant := range pallet.Constants {
			if constant.Name == "HotkeySwapOnSubnetInterval" {
				constantCount++
				if !rootTypeMatches(metadata, constant.Type, "u64", 0) || len(constant.Value) != 8 {
					return 0, errors.New("owner trim hotkey swap interval encoding changed")
				}
				interval = binary.LittleEndian.Uint64(constant.Value)
			}
		}
	}
	if palletCount != 1 || constantCount != 1 {
		return 0, errors.New("owner trim hotkey swap interval is missing or duplicated")
	}
	return interval, nil
}

// A single deadline covers census, additional evidence and final canonical/head
// rechecks. Interruption, stale input or changing runtime yields no partial result.
func (self *rpcClient) readOwnerTrimBoundedQualification(ctx context.Context, policy subnetCensusPolicy, policyHash string, window ownerTrimWindow, windowHash string) (ownerTrimBoundedQualification, error) {
	if err := policy.validate(); err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	return self.readOwnerTrimBoundedTarget(ctx, policy, policyHash, window, windowHash, *policy.TrimMaximumUids)
}

// The execution owner supplies its separately approved target; observation-only
// callers continue to use the exact original policy target above.
func (self *rpcClient) readOwnerTrimBoundedTarget(ctx context.Context, policy subnetCensusPolicy, policyHash string, window ownerTrimWindow, windowHash string, maximum uint16) (ownerTrimBoundedQualification, error) {
	if ctx == nil {
		return ownerTrimBoundedQualification{}, errors.New("owner trim qualification requires a context")
	}
	if err := errors.Join(ctx.Err(), policy.validate(), window.validate(policyHash)); err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	if !planSha256(windowHash) {
		return ownerTrimBoundedQualification{}, errors.New("owner trim qualification requires the exact window file hash")
	}
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	census, err := self.readSubnetPreview(sampleCtx, policy, policyHash)
	if err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	if census.Identity.FinalizedNumber != window.FinalizedNumber || census.Identity.FinalizedHash != window.FinalizedHash {
		return ownerTrimBoundedQualification{}, fmt.Errorf("%w: owner trim window anchor is not the observed finalized head", errRpcIntegrity)
	}
	_, metadata, err := self.readApprovedRuntimeAt(sampleCtx, identityExpectation{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}, policy.RuntimeVersion, policy.RuntimeCodeHash, policy.RuntimeMetadataHash, window.FinalizedHash)
	if err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	interval, err := ownerTrimWindowMetadata(metadata)
	if err != nil {
		return ownerTrimBoundedQualification{}, fmt.Errorf("%w: %v", errRpcIntegrity, err)
	}
	entries, err := observationStorageProfile(metadata, ownerTrimWindowStorageSpecs)
	if err != nil {
		return ownerTrimBoundedQualification{}, fmt.Errorf("%w: owner trim window storage: %v", errRpcIntegrity, err)
	}
	reader := &rootStorageReader{client: self, metadata: metadata, entries: entries, specs: ownerTrimWindowStorageSpecs, block: window.FinalizedHash, valueKVs: map[string]rootStorageValue{}}
	// Seed overlapping evidence so even same-hash inconsistent replies are refused.
	for _, value := range census.Storage {
		reader.valueKVs[value.Key] = value
	}
	netuidArg := binary.LittleEndian.AppendUint16(nil, policy.Netuid)
	valueKVs := map[string]rootStorageValue{}
	for _, name := range []string{"Tempo", "LastEpochBlock", "PendingEpochAt", "BlocksSinceLastStep", "SubnetUidToLeaseId", "AdminFreezeWindow", "ColdkeySwapAnnouncementDelay"} {
		args := [][]byte{netuidArg}
		if name == "AdminFreezeWindow" || name == "ColdkeySwapAnnouncementDelay" {
			args = nil
		}
		value, err := reader.read(sampleCtx, name, args...)
		if err != nil {
			return ownerTrimBoundedQualification{}, err
		}
		valueKVs[name] = value
	}
	state := ownerTrimWindowState{
		Tempo: binary.LittleEndian.Uint16(valueKVs["Tempo"].data), LastEpochBlock: binary.LittleEndian.Uint64(valueKVs["LastEpochBlock"].data),
		PendingEpochAt: binary.LittleEndian.Uint64(valueKVs["PendingEpochAt"].data), BlocksSinceLastStep: binary.LittleEndian.Uint64(valueKVs["BlocksSinceLastStep"].data),
		AdminFreezeWindow: binary.LittleEndian.Uint16(valueKVs["AdminFreezeWindow"].data), HotkeySwapInterval: interval,
		ColdkeySwapDelay: binary.LittleEndian.Uint32(valueKVs["ColdkeySwapAnnouncementDelay"].data), Coldkeys: []ownerTrimColdkeyWindow{},
	}
	if value := valueKVs["SubnetUidToLeaseId"]; value.RawStorage != nil {
		id := binary.LittleEndian.Uint32(value.data)
		state.SubnetLeaseId = &id
	}
	coldkeyKVs := map[string]bool{census.SubnetOwnerColdkey: true}
	for _, seat := range census.Seats {
		coldkeyKVs[seat.Coldkey] = true
	}
	coldkeys := make([]string, 0, len(coldkeyKVs))
	for coldkey := range coldkeyKVs {
		coldkeys = append(coldkeys, coldkey)
	}
	sort.Strings(coldkeys)
	state.Coldkeys = make([]ownerTrimColdkeyWindow, len(coldkeys))
	err = rootReadParallel(sampleCtx, len(coldkeys), func(readCtx context.Context, index int) error {
		coldkey := coldkeys[index]
		account, _ := hex.DecodeString(coldkey[2:])
		lastSwap, err := reader.read(readCtx, "LastHotkeySwapOnNetuid", netuidArg, account)
		if err != nil {
			return err
		}
		announcement, err := reader.read(readCtx, "ColdkeySwapAnnouncements", account)
		if err != nil {
			return err
		}
		dispute, err := reader.read(readCtx, "ColdkeySwapDisputes", account)
		if err != nil {
			return err
		}
		observation := ownerTrimColdkeyWindow{Coldkey: coldkey, LastHotkeySwapBlock: binary.LittleEndian.Uint64(lastSwap.data)}
		if announcement.RawStorage != nil {
			when := uint64(binary.LittleEndian.Uint32(announcement.data))
			observation.ColdkeySwapAvailableAt = &when
		}
		if dispute.RawStorage != nil {
			when := uint64(binary.LittleEndian.Uint32(dispute.data))
			observation.ColdkeySwapDisputedAt = &when
		}
		state.Coldkeys[index] = observation
		return nil
	})
	if err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	state.Storage = reader.evidence()
	result, err := qualifyOwnerTrimWindowTarget(sampleCtx, policy, policyHash, window, windowHash, census, state, maximum)
	if err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	var canonical, latest string
	if err := self.call(sampleCtx, "chain_getBlockHash", []any{window.FinalizedNumber}, &canonical); err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	if !validHash(canonical) || !strings.EqualFold(canonical, window.FinalizedHash) {
		return ownerTrimBoundedQualification{}, fmt.Errorf("%w: owner trim window anchor is no longer canonical", errRpcIntegrity)
	}
	if err := self.call(sampleCtx, "chain_getFinalizedHead", []any{}, &latest); err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	if !validHash(latest) || !strings.EqualFold(latest, window.FinalizedHash) {
		return ownerTrimBoundedQualification{}, fmt.Errorf("%w: owner trim window head became stale", errRpcIntegrity)
	}
	if err := sampleCtx.Err(); err != nil {
		return ownerTrimBoundedQualification{}, err
	}
	return result, nil
}
