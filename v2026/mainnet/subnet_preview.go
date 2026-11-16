// A subnet preview authenticates one finalized census and compares a requested
// identity set with the pinned source's trim selection. It cannot authorize reset.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// The complete sample shares one deadline; an error publishes no partial census.
func (self *rpcClient) readSubnetPreview(ctx context.Context, policy subnetCensusPolicy, policyHash string) (subnetPreview, error) {
	return self.readSubnetPreviewAt(ctx, policy, policyHash, "")
}

// Retained plans are rebuilt from their canonical historical census, never
// trusted because an imported file carries a self-consistent content hash.
func (self *rpcClient) readSubnetPreviewAt(ctx context.Context, policy subnetCensusPolicy, policyHash, blockHash string) (subnetPreview, error) {
	if err := policy.validate(); err != nil {
		return subnetPreview{}, err
	}
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	identity, metadata, err := self.readApprovedRuntimeAt(sampleCtx, identityExpectation{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}, policy.RuntimeVersion, policy.RuntimeCodeHash, policy.RuntimeMetadataHash, blockHash)
	if err != nil {
		return subnetPreview{}, err
	}
	entries, err := observationStorageProfile(metadata, subnetStorageSpecs)
	if err != nil {
		return subnetPreview{}, fmt.Errorf("%w: subnet storage profile: %v", errRpcIntegrity, err)
	}
	reader := &rootStorageReader{client: self, metadata: metadata, entries: entries, specs: subnetStorageSpecs, block: identity.FinalizedHash, valueKVs: map[string]rootStorageValue{}}
	registrations, maximum, err := reader.subnetRegistrations(sampleCtx, policy.Netuid, identity.FinalizedNumber)
	if err != nil {
		return subnetPreview{}, err
	}
	rootRegistrations, _, err := reader.subnetRegistrations(sampleCtx, 0, identity.FinalizedNumber)
	if err != nil {
		return subnetPreview{}, err
	}
	netuidArg := binary.LittleEndian.AppendUint16(nil, policy.Netuid)
	valueKVs := map[string]rootStorageValue{}
	for _, name := range []string{"SubnetOwner", "SubnetOwnerHotkey", "NetworkRegisteredAt", "RegisteredSubnetCounter", "MinAllowedUids", "ImmunityPeriod", "ImmuneOwnerUidsLimit", "NetworkRegistrationAllowed", "NetworkPowRegistrationAllowed", "Active", "ValidatorPermit", "Emission", "Tempo", "LastEpochBlock", "PendingEpochAt"} {
		value, err := reader.read(sampleCtx, name, netuidArg)
		if err != nil {
			return subnetPreview{}, err
		}
		valueKVs[name] = value
	}
	owner := valueKVs["SubnetOwner"]
	if owner.RawStorage == nil || !subnetAccountValid(owner.EffectiveScale) || valueKVs["NetworkRegisteredAt"].RawStorage == nil {
		return subnetPreview{}, fmt.Errorf("%w: subnet owner or registration generation is not recorded", errRpcIntegrity)
	}
	registeredAt := binary.LittleEndian.Uint64(valueKVs["NetworkRegisteredAt"].data)
	if registeredAt > identity.FinalizedNumber {
		return subnetPreview{}, fmt.Errorf("%w: subnet registration lies in the future", errRpcIntegrity)
	}
	preview := subnetPreview{
		Schema: subnetPreviewSchema, PolicyHash: policyHash, Status: "census-complete-reset-blocked", Identity: identity, Netuid: policy.Netuid,
		RuntimeSourceCommit: policy.RuntimeSourceCommit, RuntimeVersion: policy.RuntimeVersion, RuntimeCodeHash: policy.RuntimeCodeHash, RuntimeMetadataHash: policy.RuntimeMetadataHash,
		SubnetRegistrationBlock: registeredAt, SubnetGeneration: binary.LittleEndian.Uint64(valueKVs["RegisteredSubnetCounter"].data), SubnetOwnerColdkey: owner.EffectiveScale,
		MinimumUids: binary.LittleEndian.Uint16(valueKVs["MinAllowedUids"].data), MaximumUids: maximum,
		ImmunityBlocks: binary.LittleEndian.Uint16(valueKVs["ImmunityPeriod"].data), ImmuneOwnerUidsLimit: binary.LittleEndian.Uint16(valueKVs["ImmuneOwnerUidsLimit"].data),
		RegistrationAllowed: valueKVs["NetworkRegistrationAllowed"].data[0] == 1, PowRegistrationAllowed: valueKVs["NetworkPowRegistrationAllowed"].data[0] == 1,
		Seats: []subnetSeat{}, RootRegistrations: rootRegistrations, Remove: []subnetRegistration{}, Preserve: []subnetRegistration{}, Unresolved: []subnetRegistration{}, Blockers: []string{},
		ResetBlockers: []string{"RESET_CAPABILITY_BLOCKED", "SOURCE_TO_WASM_PROVENANCE_REQUIRES_INDEPENDENT_REVIEW", "EXECUTION_TIME_SELECTION_GUARD_NOT_PROVEN", "CUSTODY_COLLATERAL_STAKE_CLAIM_AND_HISTORY_AUDIT_NOT_COMPLETE", "NO_ARBITRARY_OWNER_UID_REMOVAL_CAPABILITY_VERIFIED", "RESET_SIGNING_AND_EXECUTION_NOT_IMPLEMENTED", "ROOT_POSTCONDITION_REQUIRES_POST_RESET_CENSUS"},
	}
	if !strings.EqualFold(owner.EffectiveScale, policy.SubnetOwnerColdkey) {
		preview.Blockers = append(preview.Blockers, "SUBNET_OWNER_DIFFERS_FROM_APPROVED_POLICY")
	}
	if preview.SubnetRegistrationBlock != *policy.SubnetRegistrationBlock || preview.SubnetGeneration != *policy.SubnetGeneration {
		preview.Blockers = append(preview.Blockers, "SUBNET_GENERATION_DIFFERS_FROM_APPROVED_POLICY")
	}
	if preview.MinimumUids > preview.MaximumUids {
		return subnetPreview{}, fmt.Errorf("%w: subnet minimum exceeds maximum capacity", errRpcIntegrity)
	}
	ownerHotkey := valueKVs["SubnetOwnerHotkey"]
	if ownerHotkey.RawStorage != nil {
		if !subnetAccountValid(ownerHotkey.EffectiveScale) {
			return subnetPreview{}, fmt.Errorf("%w: recorded owner hotkey is zero", errRpcIntegrity)
		}
		preview.SubnetOwnerHotkey = &ownerHotkey.EffectiveScale
	}
	owned, err := reader.read(sampleCtx, "OwnedHotkeys", owner.data)
	if err != nil {
		return subnetPreview{}, err
	}
	ownedBody, ownedCount, _ := rootVector(owned.data, 32, 0)
	ownedHotkeys := make([][]byte, ownedCount)
	ownedHotkeyKVs := map[string]bool{}
	for index := range ownedHotkeys {
		hotkey := ownedBody[index*32 : (index+1)*32]
		name := "0x" + hex.EncodeToString(hotkey)
		if !subnetAccountValid(name) || ownedHotkeyKVs[name] {
			return subnetPreview{}, fmt.Errorf("%w: duplicate or zero owned hotkey", errRpcIntegrity)
		}
		ownedHotkeyKVs[name] = true
		ownedHotkeys[index] = hotkey
	}
	err = rootReadParallel(sampleCtx, len(ownedHotkeys), func(readCtx context.Context, index int) error {
		value, err := reader.read(readCtx, "Owner", ownedHotkeys[index])
		if err != nil {
			return err
		}
		if value.RawStorage == nil || value.EffectiveScale != owner.EffectiveScale {
			return fmt.Errorf("%w: OwnedHotkeys contradicts hotkey ownership", errRpcIntegrity)
		}
		return nil
	})
	if err != nil {
		return subnetPreview{}, err
	}
	active, activeCount, _ := rootVector(valueKVs["Active"].data, 1, 0)
	permits, permitCount, _ := rootVector(valueKVs["ValidatorPermit"].data, 1, 0)
	emissions, emissionCount, _ := rootVector(valueKVs["Emission"].data, 8, 0)
	if activeCount != len(registrations) || permitCount != len(registrations) || emissionCount != len(registrations) {
		return subnetPreview{}, fmt.Errorf("%w: activity, permit or emission vector does not cover the complete subnet census", errRpcIntegrity)
	}
	for index, registration := range registrations {
		if registration.RegistrationBlock < registeredAt {
			return subnetPreview{}, fmt.Errorf("%w: UID registration predates the current subnet generation", errRpcIntegrity)
		}
		if (registration.Coldkey == owner.EffectiveScale) != ownedHotkeyKVs[registration.Hotkey] {
			return subnetPreview{}, fmt.Errorf("%w: owner hotkey census is incomplete or contradictory", errRpcIntegrity)
		}
		expiresAt := registration.RegistrationBlock + uint64(preview.ImmunityBlocks)
		expirySaturated := expiresAt < registration.RegistrationBlock
		if expirySaturated {
			expiresAt = math.MaxUint64
		}
		emission := binary.LittleEndian.Uint64(emissions[index*8:])
		preview.Seats = append(preview.Seats, subnetSeat{
			subnetRegistration: registration, Active: active[index] == 1, ValidatorPermit: permits[index] == 1,
			EmissionRao: strconv.FormatUint(emission, 10), emission: emission,
			TemporarilyImmune: identity.FinalizedNumber-registration.RegistrationBlock < uint64(preview.ImmunityBlocks), ImmunityExpiresAt: expiresAt,
			ImmunityExpirySaturated: expirySaturated,
			OwnerRecognized:         ownedHotkeyKVs[registration.Hotkey] || preview.SubnetOwnerHotkey != nil && registration.Hotkey == *preview.SubnetOwnerHotkey,
			ProtectionReasons:       []string{},
		})
	}
	setSubnetOwnerImmunity(preview.Seats, ownedHotkeyKVs, preview.SubnetOwnerHotkey, preview.ImmuneOwnerUidsLimit)
	classifySubnetScope(&preview, policy)
	freezeWindow, err := reader.read(sampleCtx, "AdminFreezeWindow")
	if err != nil {
		return subnetPreview{}, err
	}
	lastTrim, err := reader.read(sampleCtx, "TransactionKeyLastBlock", owner.data, netuidArg, []byte{9, 0})
	if err != nil {
		return subnetPreview{}, err
	}
	lastTrimBlock := binary.LittleEndian.Uint64(lastTrim.data)
	lastEpoch := binary.LittleEndian.Uint64(valueKVs["LastEpochBlock"].data)
	if lastTrimBlock > identity.FinalizedNumber || lastEpoch > identity.FinalizedNumber {
		return subnetPreview{}, fmt.Errorf("%w: trim or epoch history is in the future", errRpcIntegrity)
	}
	trim := subnetTrimPreview{MaximumUids: *policy.TrimMaximumUids, OwnerLastTrimBlock: lastTrimBlock, OwnerRateLimitStatus: "first-owner-trim-no-prior-rate-limit", Removed: []subnetRegistration{}, Survivors: []subnetUidMapping{}, Blockers: []string{}}
	trim.Call, err = subnetOwnerTrimCall(metadata)
	if err != nil {
		trim.Blockers = append(trim.Blockers, "RESET_OWNER_TRIM_CALL_NOT_VERIFIED")
	}
	threshold, err := subnetMaximumImmunePercentage(metadata)
	if err != nil {
		trim.Blockers = append(trim.Blockers, "RESET_IMMUNITY_THRESHOLD_NOT_VERIFIED")
	} else {
		trim.MaximumImmunePercentage = &threshold
	}
	if lastTrimBlock != 0 {
		trim.OwnerRateLimitStatus = "blocked-build-selected-interval-not-authenticated"
		trim.Blockers = append(trim.Blockers, "RESET_OWNER_TRIM_RATE_LIMIT_BUILD_VALUE_REQUIRES_REVIEW")
	}
	tempo := uint64(binary.LittleEndian.Uint16(valueKVs["Tempo"].data))
	nextEpoch := lastEpoch + tempo
	if nextEpoch < lastEpoch {
		nextEpoch = math.MaxUint64
	}
	remaining := uint64(0)
	if nextEpoch > identity.FinalizedNumber {
		remaining = nextEpoch - identity.FinalizedNumber
	}
	pending := binary.LittleEndian.Uint64(valueKVs["PendingEpochAt"].data)
	trim.AdminWindowOpen = tempo == 0 || !(pending > identity.FinalizedNumber || remaining < uint64(binary.LittleEndian.Uint16(freezeWindow.data)))
	if !trim.AdminWindowOpen {
		trim.Blockers = append(trim.Blockers, "RESET_ADMIN_WINDOW_CLOSED")
	}
	preview.Trim = previewSubnetTrim(preview.Seats, policy, preview.MinimumUids, preview.MaximumUids, len(preview.Blockers) == 0, trim)
	if preview.RegistrationAllowed || preview.PowRegistrationAllowed {
		preview.ResetBlockers = append(preview.ResetBlockers, "COMPETING_REGISTRATION_AND_REENTRY_POLICY_NOT_ENFORCED")
	}
	var confirmedHash string
	if err := self.call(sampleCtx, "chain_getBlockHash", []any{identity.FinalizedNumber}, &confirmedHash); err != nil {
		return subnetPreview{}, err
	}
	if !validHash(confirmedHash) || !strings.EqualFold(confirmedHash, identity.FinalizedHash) {
		return subnetPreview{}, fmt.Errorf("%w: finalized block changed during subnet census", errRpcIntegrity)
	}
	if err := self.closeSnapshotFinality(sampleCtx, identity); err != nil {
		return subnetPreview{}, err
	}
	if err := sampleCtx.Err(); err != nil {
		return subnetPreview{}, err
	}
	preview.Storage = reader.evidence()
	preview.CensusComplete = true
	return preview, nil
}

// The source sorts owned registrations oldest-first, then lowest UID, truncates,
// and inserts the explicit owner hotkey first with another truncation.
func setSubnetOwnerImmunity(seats []subnetSeat, ownedHotkeyKVs map[string]bool, ownerHotkey *string, limit uint16) {
	indices := []int{}
	explicitIndex := -1
	for index, seat := range seats {
		seats[index].OwnerImmune = false
		if ownedHotkeyKVs[seat.Hotkey] {
			indices = append(indices, index)
		}
		if ownerHotkey != nil && seat.Hotkey == *ownerHotkey {
			explicitIndex = index
		}
	}
	sort.Slice(indices, func(i, j int) bool {
		left, right := seats[indices[i]], seats[indices[j]]
		if left.RegistrationBlock != right.RegistrationBlock {
			return left.RegistrationBlock < right.RegistrationBlock
		}
		return left.Uid < right.Uid
	})
	indices = indices[:min(len(indices), int(limit))]
	found := false
	for _, index := range indices {
		found = found || index == explicitIndex
	}
	if explicitIndex >= 0 && !found {
		indices = append([]int{explicitIndex}, indices...)
		indices = indices[:min(len(indices), int(limit))]
	}
	for _, index := range indices {
		seats[index].OwnerImmune = true
	}
}

// No permit bit can silently turn an undeclared validator into a removable miner.
func classifySubnetScope(preview *subnetPreview, policy subnetCensusPolicy) {
	removeKVs := map[string]subnetIdentityExpectation{}
	preserveKVs := map[string]subnetProtectedIdentity{}
	for _, value := range policy.Remove {
		removeKVs[strings.ToLower(value.Hotkey)] = value
	}
	for _, value := range policy.Preserve {
		preserveKVs[strings.ToLower(value.Hotkey)] = value
	}
	seen := map[string]bool{}
	for index := range preview.Seats {
		seat := &preview.Seats[index]
		seen[seat.Hotkey] = true
		remove, requested := removeKVs[seat.Hotkey]
		preserve, protected := preserveKVs[seat.Hotkey]
		seat.RequestedRemoval = requested
		if protected {
			seat.ProtectionReasons = append(seat.ProtectionReasons, preserve.Roles...)
		}
		if seat.OwnerRecognized {
			seat.ProtectionReasons = append(seat.ProtectionReasons, "runtime-owner-identity")
		}
		if seat.ValidatorPermit {
			seat.ProtectionReasons = append(seat.ProtectionReasons, "observed-validator-permit")
		}
		matches := func(expected subnetIdentityExpectation) bool {
			return strings.EqualFold(expected.Coldkey, seat.Coldkey) && expected.RegistrationBlock != nil && *expected.RegistrationBlock == seat.RegistrationBlock
		}
		seat.Disposition = "unresolved"
		switch {
		case requested && !matches(remove), protected && !matches(preserve.subnetIdentityExpectation):
			preview.Blockers = append(preview.Blockers, "SCOPED_IDENTITY_OWNER_OR_REGISTRATION_GENERATION_CHANGED:"+seat.Hotkey)
		case requested && len(seat.ProtectionReasons) != 0:
			preview.Blockers = append(preview.Blockers, "REMOVAL_CONFLICTS_WITH_PROTECTED_IDENTITY:"+seat.Hotkey)
		case requested:
			seat.Disposition = "remove"
		case len(seat.ProtectionReasons) != 0:
			seat.Disposition = "preserve"
		default:
			preview.Blockers = append(preview.Blockers, "IDENTITY_ROLE_UNRESOLVED:"+seat.Hotkey)
		}
		switch seat.Disposition {
		case "remove":
			preview.Remove = append(preview.Remove, seat.subnetRegistration)
		case "preserve":
			preview.Preserve = append(preview.Preserve, seat.subnetRegistration)
		default:
			preview.Unresolved = append(preview.Unresolved, seat.subnetRegistration)
		}
	}
	for _, expected := range policy.Remove {
		if !seen[strings.ToLower(expected.Hotkey)] {
			preview.Blockers = append(preview.Blockers, "REQUESTED_REMOVAL_GENERATION_NOT_REGISTERED:"+strings.ToLower(expected.Hotkey))
		}
	}
	for _, expected := range policy.Preserve {
		if !seen[strings.ToLower(expected.Hotkey)] {
			preview.Blockers = append(preview.Blockers, "PROTECTED_GENERATION_NOT_REGISTERED:"+strings.ToLower(expected.Hotkey))
		}
	}
	if len(policy.Remove) == 0 {
		preview.Blockers = append(preview.Blockers, "NO_EXPLICIT_REMOVAL_SCOPE")
	}
}

// Percent uses floor-to-whole-percent and saturation in the pinned SDK. Stable
// descending emission order is visited backward, so a tie removes higher UIDs.
func previewSubnetTrim(seats []subnetSeat, policy subnetCensusPolicy, minimum, maximum uint16, scopeComplete bool, trim subnetTrimPreview) subnetTrimPreview {
	if trim.Call == nil {
		return trim
	}
	if trim.MaximumUids < minimum || trim.MaximumUids > maximum {
		trim.Blockers = append(trim.Blockers, "RESET_TRIM_CAPACITY_OUTSIDE_RUNTIME_LIMITS")
		return trim
	}
	needed := max(0, len(seats)-int(trim.MaximumUids))
	if needed > 0 {
		if trim.MaximumImmunePercentage == nil {
			return trim
		}
		immune := 0
		for _, seat := range seats {
			if seat.OwnerImmune || seat.TemporarilyImmune {
				immune++
			}
		}
		percentage := uint8(100)
		if trim.MaximumUids != 0 {
			percentage = uint8(min(100, 100*immune/int(trim.MaximumUids)))
		}
		trim.ImmunePercentage = &percentage
		if percentage >= *trim.MaximumImmunePercentage {
			trim.Blockers = append(trim.Blockers, "RESET_TRIM_IMMUNITY_THRESHOLD_REACHED")
			return trim
		}
	}
	indices := make([]int, len(seats))
	for index := range indices {
		indices[index] = index
	}
	sort.Slice(indices, func(i, j int) bool {
		left, right := seats[indices[i]], seats[indices[j]]
		if left.emission != right.emission {
			return left.emission > right.emission
		}
		return left.Uid < right.Uid
	})
	removed := map[uint16]bool{}
	for index := len(indices) - 1; index >= 0 && len(removed) < needed; index-- {
		seat := seats[indices[index]]
		if seat.OwnerImmune || seat.TemporarilyImmune {
			continue
		}
		removed[seat.Uid] = true
	}
	if len(removed) != needed {
		trim.Blockers = append(trim.Blockers, "RESET_TRIM_CANNOT_REACH_REQUESTED_CAPACITY")
		return trim
	}
	for _, seat := range seats {
		if removed[seat.Uid] {
			trim.Removed = append(trim.Removed, seat.subnetRegistration)
			if seat.Disposition != "remove" {
				trim.Blockers = append(trim.Blockers, "RESET_TRIM_WOULD_REMOVE_PROTECTED_OR_UNRESOLVED_IDENTITY:"+seat.Hotkey)
			}
		} else {
			trim.Survivors = append(trim.Survivors, subnetUidMapping{subnetRegistration: seat.subnetRegistration, NewUid: uint16(len(trim.Survivors))})
		}
	}
	trim.CandidateComplete = true
	trim.MatchesRequestedRemovalSet = scopeComplete && len(trim.Removed) == len(policy.Remove)
	removedIdentityKVs := map[string]subnetRegistration{}
	for _, actual := range trim.Removed {
		removedIdentityKVs[actual.Hotkey] = actual
	}
	for _, expected := range policy.Remove {
		actual, exists := removedIdentityKVs[strings.ToLower(expected.Hotkey)]
		found := exists && strings.EqualFold(expected.Coldkey, actual.Coldkey) && expected.RegistrationBlock != nil && *expected.RegistrationBlock == actual.RegistrationBlock
		trim.MatchesRequestedRemovalSet = trim.MatchesRequestedRemovalSet && found
	}
	if !trim.MatchesRequestedRemovalSet {
		trim.Blockers = append(trim.Blockers, "RESET_TRIM_SELECTED_SET_DIFFERS_FROM_APPROVED_REMOVAL_SCOPE")
	}
	return trim
}
