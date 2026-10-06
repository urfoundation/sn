// Reconcile the actual surviving generation set independently of a historical
// emission ordering. Whole-block correspondence never proves sole causation.
package main

import (
	"errors"
	"strings"
)

// All originally requested miners remain explicit, including retained miners
// and hotkeys reused for a different registration generation.
type ownerTrimActualSubset struct {
	Requested        []ownerTrimGenerationObservation `json:"requested_generations"`
	AbsentOld        []subnetRegistration             `json:"absent_old_generations"`
	ResidualOld      []subnetRegistration             `json:"residual_old_generations"`
	Survivors        []subnetUidMapping               `json:"survivor_uid_mappings"`
	MissingProtected []subnetRegistration             `json:"missing_protected_generations"`
	Unexpected       []subnetRegistration             `json:"unexpected_generations"`
	Blockers         []string                         `json:"blockers"`
	Matches          bool                             `json:"correspondence_matches"`
	AttributedToTrim bool                             `json:"absence_attributed_to_trim"`
	FullReset        bool                             `json:"full_reset_completed"`
}

// Every survivor must preserve its coldkey/birth and the runtime's ascending
// old-uid compression. UID equality alone cannot recognize a protected role.
func reconcileOwnerTrimActualSubset(action ownerTrimAction, policy subnetCensusPolicy, before, after subnetPreview) (ownerTrimActualSubset, error) {
	result := ownerTrimActualSubset{Requested: []ownerTrimGenerationObservation{}, AbsentOld: []subnetRegistration{}, ResidualOld: []subnetRegistration{},
		Survivors: []subnetUidMapping{}, MissingProtected: []subnetRegistration{}, Unexpected: []subnetRegistration{}, Blockers: []string{}}
	if err := errors.Join(action.validate(), policy.validate()); err != nil {
		return result, err
	}
	if !before.CensusComplete || !after.CensusComplete || before.PolicyHash != action.PolicyHash || after.PolicyHash != action.PolicyHash ||
		before.Netuid != action.Netuid || after.Netuid != action.Netuid || len(before.Seats) > rootCensusLimit || len(after.Seats) > rootCensusLimit ||
		before.Identity.FinalizedNumber+1 != after.Identity.FinalizedNumber {
		return result, errors.New("owner trim correspondence requires complete adjacent before/after censuses")
	}
	for _, census := range []subnetPreview{before, after} {
		if census.Identity.NativeChain != action.Network.NativeChain || census.Identity.GenesisHash != action.Network.GenesisHash || census.Identity.EvmChainId != action.Network.EvmChainId ||
			census.RuntimeVersion != action.Runtime.RuntimeVersion || census.RuntimeCodeHash != action.Runtime.RuntimeCodeHash || census.RuntimeMetadataHash != action.Runtime.RuntimeMetadataHash ||
			census.SubnetOwnerColdkey != action.Coldkey || census.SubnetRegistrationBlock != action.SubnetRegistrationBlock || census.SubnetGeneration != action.SubnetGeneration {
			result.Blockers = append(result.Blockers, "OWNER_TRIM_APPROVED_DOMAIN_OR_SUBNET_GENERATION_CHANGED")
		}
	}
	for _, blocker := range ownerTrimScopeChanges(before, after) {
		// Best-effort correspondence describes already observed generations;
		// open flags do not erase that evidence or supply activation authority.
		if action.Schema == ownerTrimBestEffortActionSchema && blocker == "OWNER_TRIM_COMPETING_REGISTRATION_OR_REENTRY_NOT_FENCED" {
			continue
		}
		result.Blockers = append(result.Blockers, blocker)
	}
	oldKVs, newKVs := map[string]subnetSeat{}, map[string]subnetSeat{}
	for _, item := range []struct {
		seats []subnetSeat
		kvs   map[string]subnetSeat
	}{{seats: before.Seats, kvs: oldKVs}, {seats: after.Seats, kvs: newKVs}} {
		for i, seat := range item.seats {
			key := strings.ToLower(seat.Hotkey)
			if seat.Uid != uint16(i) || item.kvs[key].Hotkey != "" {
				return result, errors.New("owner trim correspondence census is not unique and contiguous")
			}
			item.kvs[key] = seat
		}
	}
	allowedKVs, protectedKVs := map[string]bool{}, map[string]bool{}
	for _, expected := range policy.Preserve {
		key := strings.ToLower(expected.Hotkey)
		protectedKVs[key] = true
		old, exists := oldKVs[key]
		if !exists || !strings.EqualFold(old.Coldkey, expected.Coldkey) || old.RegistrationBlock != *expected.RegistrationBlock {
			result.Blockers = append(result.Blockers, "OWNER_TRIM_PROTECTED_GENERATION_NOT_PRESENT_BEFORE_DISPATCH")
		}
	}
	for _, expected := range policy.Remove {
		key := strings.ToLower(expected.Hotkey)
		old, exists := oldKVs[key]
		if !exists || !strings.EqualFold(old.Coldkey, expected.Coldkey) || old.RegistrationBlock != *expected.RegistrationBlock {
			result.Blockers = append(result.Blockers, "OWNER_TRIM_REQUESTED_GENERATION_NOT_PRESENT_BEFORE_DISPATCH")
			continue
		}
		allowedKVs[key] = true
		row := ownerTrimGenerationObservation{subnetRegistration: old.subnetRegistration, ExpectedDisposition: "approved-subset-or-explicit-residual"}
		current, exists := newKVs[key]
		switch {
		case !exists:
			row.ObservedDisposition = "old-generation-absent"
			result.AbsentOld = append(result.AbsentOld, old.subnetRegistration)
		case ownerTrimSameGeneration(old.subnetRegistration, current.subnetRegistration):
			row.ObservedDisposition = "old-generation-retained"
			result.ResidualOld = append(result.ResidualOld, old.subnetRegistration)
		default:
			row.ObservedDisposition = "different-generation-present"
			result.Blockers = append(result.Blockers, "OWNER_TRIM_REQUESTED_HOTKEY_HAS_NEW_GENERATION")
		}
		if exists {
			registration := current.subnetRegistration
			row.CurrentRegistration = &registration
		}
		result.Requested = append(result.Requested, row)
	}
	for _, old := range before.Seats {
		key := strings.ToLower(old.Hotkey)
		current, exists := newKVs[key]
		if !exists || !ownerTrimSameGeneration(old.subnetRegistration, current.subnetRegistration) {
			if !allowedKVs[key] || protectedKVs[key] || old.OwnerRecognized || old.OwnerImmune || old.ValidatorPermit || old.Disposition != "remove" || len(old.ProtectionReasons) != 0 {
				result.MissingProtected = append(result.MissingProtected, old.subnetRegistration)
				result.Blockers = append(result.Blockers, "OWNER_TRIM_PROTECTED_OR_UNAPPROVED_GENERATION_MISSING")
			}
			continue
		}
		wantedUid := uint16(len(result.Survivors))
		result.Survivors = append(result.Survivors, subnetUidMapping{subnetRegistration: old.subnetRegistration, NewUid: current.Uid})
		if current.Uid != wantedUid {
			result.Blockers = append(result.Blockers, "OWNER_TRIM_SURVIVOR_UID_COMPRESSION_DIFFERS")
		}
	}
	for _, current := range after.Seats {
		old, exists := oldKVs[strings.ToLower(current.Hotkey)]
		if !exists || !ownerTrimSameGeneration(old.subnetRegistration, current.subnetRegistration) {
			result.Unexpected = append(result.Unexpected, current.subnetRegistration)
			result.Blockers = append(result.Blockers, "OWNER_TRIM_UNEXPECTED_POST_DISPATCH_GENERATION")
		}
	}
	if after.MaximumUids != action.MaximumUids || len(after.Seats) != int(action.MaximumUids) ||
		len(result.AbsentOld) != len(before.Seats)-int(action.MaximumUids) || len(result.AbsentOld) == 0 || len(result.Requested) != len(policy.Remove) {
		result.Blockers = append(result.Blockers, "OWNER_TRIM_CAPACITY_REMOVAL_OR_RESIDUAL_COUNT_DIFFERS")
	}
	result.Blockers = ownerTrimUniqueBlockers(result.Blockers)
	result.Matches = len(result.Blockers) == 0
	return result, nil
}
