package main

// final_semantic_fleet_generation_events.go turns retained raw coordinator
// and batcher logs into the exact lineage fields they attest. It is shared by
// collection and public replay so a producer cannot use one decoder while a
// reviewer accepts another interpretation of the same receipt bytes.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Carries both the normalized public field projection and the ABI event name
// needed by batch-shape checks. The count remains explicit because it is part
// of FleetRefreshed's non-indexed payload, not derivable from a topic.
type finalFleetGenerationDecodedEvent struct {
	Evidence FinalFleetGenerationEventEvidence
	Name     string
}

// Decodes one retained log only when its contract and ABI event are permitted
// for the approved mutation class. The returned fields use fixed-width
// lower-case wire encodings, matching the source artifact and public RPC
// transcript without relying on map iteration or presentation formatting.
// The explicit batcher address is a historical write identity: predecessor
// install receipts must not be decoded against a later release helper.
func finalFleetGenerationDecodeEventForBatcher(evidence *FinalSemanticEvidence, actionID, batcherAddress string, log finalCanonicalEVMLog) (finalFleetGenerationDecodedEvent, error) {
	if evidence == nil || !common.IsHexAddress(log.Address) || common.HexToAddress(log.Address) == (common.Address{}) || len(log.Topics) == 0 || !finalCanonicalHex(log.Topics[0], common.HashLength) {
		return finalFleetGenerationDecodedEvent{}, errors.New("ordinary fleet generation event identity is incomplete")
	}
	coordinator, batcher, err := finalFleetGenerationABIs()
	if err != nil {
		return finalFleetGenerationDecodedEvent{}, err
	}
	requiresBatcher := strings.HasPrefix(actionID, "fleet.install.batch.") || strings.HasPrefix(actionID, "fleet.refresh.batch.")
	if requiresBatcher && (!common.IsHexAddress(batcherAddress) || common.HexToAddress(batcherAddress) == (common.Address{})) {
		return finalFleetGenerationDecodedEvent{}, errors.New("ordinary fleet generation batcher event has no authenticated batcher address")
	}
	var contract abi.ABI
	contractClass := ""
	switch {
	case strings.EqualFold(log.Address, evidence.Deployment.CoordinatorProxy):
		contract, contractClass = coordinator, "coordinator"
	case requiresBatcher && strings.EqualFold(log.Address, batcherAddress):
		contract, contractClass = batcher, "batcher"
	default:
		return finalFleetGenerationDecodedEvent{}, errors.New("ordinary fleet generation event has an unapproved contract")
	}
	event, found := finalSemanticReceiptABIEvent(contract, log.Topics[0])
	if !found {
		return finalFleetGenerationDecodedEvent{}, errors.New("ordinary fleet generation event has an unknown topic")
	}
	data, err := decodeEvidenceHex(log.Data)
	if err != nil {
		return finalFleetGenerationDecodedEvent{}, err
	}
	args := map[string]any{}
	if err := event.Inputs.NonIndexed().UnpackIntoMap(args, data); err != nil {
		return finalFleetGenerationDecodedEvent{}, fmt.Errorf("decode ordinary fleet generation %s data: %w", event.Name, err)
	}
	topics := make([]common.Hash, len(log.Topics)-1)
	for index := 1; index < len(log.Topics); index++ {
		if !finalCanonicalHex(log.Topics[index], common.HashLength) {
			return finalFleetGenerationDecodedEvent{}, fmt.Errorf("ordinary fleet generation %s topic %d is malformed", event.Name, index)
		}
		topics[index-1] = common.HexToHash(log.Topics[index])
	}
	if err := abi.ParseTopicsIntoMap(args, indexedABIArguments(event.Inputs), topics); err != nil {
		return finalFleetGenerationDecodedEvent{}, fmt.Errorf("decode ordinary fleet generation %s topics: %w", event.Name, err)
	}
	if err := finalFleetGenerationActionEventAllowed(actionID, contractClass, event.Name); err != nil {
		return finalFleetGenerationDecodedEvent{}, err
	}
	decoded := finalFleetGenerationDecodedEvent{
		Evidence: FinalFleetGenerationEventEvidence{
			Contract: strings.ToLower(common.HexToAddress(log.Address).Hex()),
			Kind:     strings.ToLower(event.ID.Hex()),
			Log:      log,
		},
		Name: event.Name,
	}
	switch event.Name {
	case "CommitmentMirrored":
		if decoded.Evidence.Hotkey, err = finalFleetGenerationEventHex32(args, "hotkey"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.CommitmentHash, err = finalFleetGenerationEventHex32(args, "commitmentHash"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.FinalizedBlock, err = finalFleetGenerationEventUint(args, "finalizedBlock"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.FinalizedBlockHash, err = finalFleetGenerationEventHex32(args, "finalizedBlockHash"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
	case "FleetBound":
		if decoded.Evidence.ClientID, err = finalFleetGenerationEventHex16(args, "clientId"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.FleetID, err = finalFleetGenerationEventHex32(args, "fleetId"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.Hotkey, err = finalFleetGenerationEventHex32(args, "hotkey"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.UID, err = finalFleetGenerationEventUID(args, "uid"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.Generation, err = finalFleetGenerationEventUint(args, "generation"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.ValidFromEpoch, err = finalFleetGenerationEventUint(args, "validFromEpoch"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.ValidToEpoch, err = finalFleetGenerationEventUint(args, "validToEpoch"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
	case "FleetBindingRevoked":
		if decoded.Evidence.ClientID, err = finalFleetGenerationEventHex16(args, "clientId"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.Generation, err = finalFleetGenerationEventUint(args, "generation"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.ValidFromEpoch, err = finalFleetGenerationEventUint(args, "effectiveEpoch"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
	case "FleetMemberBound":
		if decoded.Evidence.FleetID, err = finalFleetGenerationEventHex32(args, "fleetId"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.ClientID, err = finalFleetGenerationEventHex16(args, "clientId"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.Generation, err = finalFleetGenerationEventUint(args, "generation"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.UID, err = finalFleetGenerationEventUID(args, "uid"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
	case "FleetInstalled", "FleetRefreshed":
		if decoded.Evidence.Hotkey, err = finalFleetGenerationEventHex32(args, "hotkey"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.CommitmentHash, err = finalFleetGenerationEventHex32(args, "commitmentHash"); err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		if decoded.Evidence.MemberCount, err = finalFleetGenerationEventUint(args, "members"); err != nil || decoded.Evidence.MemberCount == 0 {
			if err == nil {
				err = errors.New("ordinary fleet generation FleetRefreshed members is zero")
			}
			return finalFleetGenerationDecodedEvent{}, err
		}
	default:
		return finalFleetGenerationDecodedEvent{}, fmt.Errorf("ordinary fleet generation event %s is unsupported", event.Name)
	}
	return decoded, nil
}

// Uses the current reviewed batcher root for direct decoder callers. Source
// reconstruction and public replay use the explicit form above, which is
// necessary for historical install transactions that predate the terminal
// release helper.
func finalFleetGenerationDecodeEvent(evidence *FinalSemanticEvidence, actionID string, log finalCanonicalEVMLog) (finalFleetGenerationDecodedEvent, error) {
	batcherAddress := ""
	if strings.HasPrefix(actionID, "fleet.install.batch.") || strings.HasPrefix(actionID, "fleet.refresh.batch.") {
		if evidence == nil {
			return finalFleetGenerationDecodedEvent{}, errors.New("ordinary fleet generation event evidence is unavailable")
		}
		batcher, err := finalReleaseRuntimeRootByName(evidence, "fleet_batcher")
		if err != nil {
			return finalFleetGenerationDecodedEvent{}, err
		}
		batcherAddress = batcher.Address
	}
	return finalFleetGenerationDecodeEventForBatcher(evidence, actionID, batcherAddress, log)
}

// Limits a decoded ABI event to the action that can legitimately emit it.
// Batcher refreshes intentionally retain all coordinator and batcher events;
// an unrepresented one is rejected by exact receipt-log equality upstream.
func finalFleetGenerationActionEventAllowed(actionID, contractClass, eventName string) error {
	switch {
	case strings.HasPrefix(actionID, "fleet.mirror."):
		if contractClass == "coordinator" && eventName == "CommitmentMirrored" {
			return nil
		}
	case strings.HasPrefix(actionID, "fleet.bind."):
		if contractClass == "coordinator" && eventName == "FleetBound" {
			return nil
		}
	case strings.HasPrefix(actionID, "fleet.install.batch."):
		if contractClass == "coordinator" && (eventName == "CommitmentMirrored" || eventName == "FleetBound") {
			return nil
		}
		if contractClass == "batcher" && (eventName == "FleetMemberBound" || eventName == "FleetInstalled") {
			return nil
		}
	case strings.HasPrefix(actionID, "fleet.refresh.batch."):
		if contractClass == "coordinator" && (eventName == "CommitmentMirrored" || eventName == "FleetBindingRevoked" || eventName == "FleetBound") {
			return nil
		}
		if contractClass == "batcher" && (eventName == "FleetMemberBound" || eventName == "FleetRefreshed") {
			return nil
		}
	default:
		return fmt.Errorf("ordinary fleet generation action %q is unsupported", actionID)
	}
	return fmt.Errorf("ordinary fleet generation action %q has unexpected %s event %s", actionID, contractClass, eventName)
}

// Extracts a required bytes32 argument in the canonical evidence spelling.
func finalFleetGenerationEventHex32(args map[string]any, name string) (string, error) {
	value, ok := finalSemanticHex32(args, name)
	if !ok || !finalCanonicalHex(value, common.HashLength) || common.HexToHash(value) == (common.Hash{}) {
		return "", fmt.Errorf("ordinary fleet generation event %s is not a nonzero bytes32", name)
	}
	return strings.ToLower(value), nil
}

// Extracts a required bytes16 client identity without accepting a padded
// bytes32 substitute. Fleet bindings intentionally identify their client at
// this narrower ABI width.
func finalFleetGenerationEventHex16(args map[string]any, name string) (string, error) {
	value, found := args[name]
	if !found {
		return "", fmt.Errorf("ordinary fleet generation event %s is absent", name)
	}
	var bytes [16]byte
	switch typed := value.(type) {
	case [16]byte:
		bytes = typed
	default:
		return "", fmt.Errorf("ordinary fleet generation event %s is not bytes16", name)
	}
	zero := [16]byte{}
	if bytes == zero {
		return "", fmt.Errorf("ordinary fleet generation event %s is zero", name)
	}
	return fmt.Sprintf("0x%x", bytes[:]), nil
}

// Extracts one ABI unsigned integer and rejects an omitted or overflowing
// representation before it can be compared to an evidence coordinate.
func finalFleetGenerationEventUint(args map[string]any, name string) (uint64, error) {
	value, ok := finalSemanticUint(args, name)
	if !ok {
		return 0, fmt.Errorf("ordinary fleet generation event %s is not uint64", name)
	}
	return value, nil
}

// Converts a decoded ABI value into the exact uint16 UID domain. UID zero is
// intentionally valid on Subtensor and is therefore not rejected here.
func finalFleetGenerationEventUID(args map[string]any, name string) (uint16, error) {
	value, err := finalFleetGenerationEventUint(args, name)
	if err != nil || value > uint64(^uint16(0)) {
		if err == nil {
			err = fmt.Errorf("ordinary fleet generation event %s exceeds uint16", name)
		}
		return 0, err
	}
	return uint16(value), nil
}

// Checks every decoded event against the specific signed fleet/member it is
// meant to represent. Exact raw-log equality alone prevents receipt edits;
// this second join prevents a valid event for fleet A from being relabeled as
// evidence for fleet B.
func verifyFinalFleetGenerationEventTopology(evidence *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence) error {
	if evidence == nil || lineage == nil {
		return errors.New("ordinary fleet generation event topology is unavailable")
	}
	for _, batch := range lineage.Batches {
		if batch.Generation == 1 && len(batch.CarriedFleets) != 0 {
			if err := verifyFinalFleetGenerationCarriedEvents(evidence, lineage, batch); err != nil {
				return err
			}
			if len(batch.InstalledFleets) == 0 {
				// An all-carried initial partition has completed its only
				// provenance path. Mixed partitions continue below to verify
				// their one atomic installed-side receipt as well.
				continue
			}
		}
		if batch.Generation == 1 && len(batch.InstalledFleets) != 0 {
			if batch.BatchWrite == nil {
				return fmt.Errorf("ordinary fleet generation install batch %d has no write events", batch.Batch)
			}
			if err := verifyFinalFleetGenerationInstallEvents(evidence, lineage, batch, *batch.BatchWrite); err != nil {
				return err
			}
			continue
		}
		if batch.Generation != 2 {
			return fmt.Errorf("ordinary fleet generation initial batch %d has no provenance events", batch.Batch)
		}
		if batch.BatchWrite == nil {
			return fmt.Errorf("ordinary fleet generation refresh batch %d has no write events", batch.Batch)
		}
		if err := verifyFinalFleetGenerationRefreshEvents(evidence, lineage, batch, *batch.BatchWrite); err != nil {
			return err
		}
	}
	return nil
}

// Requires each predecessor mirror and bind transaction to contain exactly
// its one expected event. These were separate historical transactions, so a
// second release-contract log would be an unexplained mutation rather than
// harmless batch noise.
func verifyFinalFleetGenerationCarriedEvents(evidence *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence, batch FinalFleetGenerationBatchEvidence) error {
	for offset, fleetID := range batch.CarriedFleets {
		version := lineage.SetupFleets[fleetID-1].Initial
		mirrorIndex := offset * int(finalFleetGenerationMembersPerFleet+1)
		mirror, err := finalFleetGenerationSingleDecodedEvent(evidence, batch.CarriedHistory[mirrorIndex])
		if err != nil {
			return fmt.Errorf("ordinary fleet generation carried mirror %d: %w", fleetID, err)
		}
		if mirror.Name != "CommitmentMirrored" || !finalFleetGenerationMirrorMatches(mirror.Evidence, version) {
			return fmt.Errorf("ordinary fleet generation carried mirror %d differs from its signed commitment", fleetID)
		}
		for memberIndex := uint64(0); memberIndex < finalFleetGenerationMembersPerFleet; memberIndex++ {
			write := batch.CarriedHistory[mirrorIndex+1+int(memberIndex)]
			bound, decodeErr := finalFleetGenerationSingleDecodedEvent(evidence, write)
			if decodeErr != nil {
				return fmt.Errorf("ordinary fleet generation carried binding %d/%d: %w", fleetID, memberIndex+1, decodeErr)
			}
			if bound.Name != "FleetBound" || !finalFleetGenerationBoundMatches(bound.Evidence, version.Members[memberIndex]) {
				return fmt.Errorf("ordinary fleet generation carried binding %d/%d differs from its signed member", fleetID, memberIndex+1)
			}
		}
	}
	return nil
}

// Requires one fully ordered ten-log group for each installed initial fleet:
// coordinator mirror, four coordinator binds, four batcher member joins, and
// the batcher's FleetInstalled summary. The raw receipt has ten logs because
// each member has both a coordinator state event and batcher provenance event.
func verifyFinalFleetGenerationInstallEvents(evidence *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence, batch FinalFleetGenerationBatchEvidence, write FinalFleetGenerationWriteEvidence) error {
	wantEvents := len(batch.InstalledFleets) * int(1+finalFleetGenerationMembersPerFleet*2+1)
	if len(write.Events) != wantEvents {
		return fmt.Errorf("ordinary fleet generation install batch %d has %d events, want %d", batch.Batch, len(write.Events), wantEvents)
	}
	position := 0
	for _, fleetID := range batch.InstalledFleets {
		fleet := lineage.SetupFleets[fleetID-1]
		mirror, err := finalFleetGenerationDecodedEventAt(evidence, write, position)
		if err != nil {
			return fmt.Errorf("ordinary fleet generation install batch %d mirror %d: %w", batch.Batch, fleetID, err)
		}
		position++
		if mirror.Name != "CommitmentMirrored" || !finalFleetGenerationMirrorMatches(mirror.Evidence, fleet.Initial) {
			return fmt.Errorf("ordinary fleet generation install batch %d mirror %d differs", batch.Batch, fleetID)
		}
		for memberIndex := range fleet.Initial.Members {
			bound, decodeErr := finalFleetGenerationDecodedEventAt(evidence, write, position)
			if decodeErr != nil {
				return fmt.Errorf("ordinary fleet generation install batch %d binding %d/%d: %w", batch.Batch, fleetID, memberIndex+1, decodeErr)
			}
			position++
			if bound.Name != "FleetBound" || !finalFleetGenerationBoundMatches(bound.Evidence, fleet.Initial.Members[memberIndex]) {
				return fmt.Errorf("ordinary fleet generation install batch %d binding %d/%d differs", batch.Batch, fleetID, memberIndex+1)
			}
			memberBound, memberErr := finalFleetGenerationDecodedEventAt(evidence, write, position)
			if memberErr != nil {
				return fmt.Errorf("ordinary fleet generation install batch %d member event %d/%d: %w", batch.Batch, fleetID, memberIndex+1, memberErr)
			}
			position++
			if memberBound.Name != "FleetMemberBound" || !strings.EqualFold(memberBound.Evidence.ClientID, fleet.Initial.Members[memberIndex].ClientID) || !strings.EqualFold(memberBound.Evidence.FleetID, fleet.Initial.Members[memberIndex].FleetKey) || memberBound.Evidence.Generation != 1 || memberBound.Evidence.UID != fleet.Initial.Members[memberIndex].UID {
				return fmt.Errorf("ordinary fleet generation install batch %d member event %d/%d differs", batch.Batch, fleetID, memberIndex+1)
			}
		}
		installed, installErr := finalFleetGenerationDecodedEventAt(evidence, write, position)
		if installErr != nil {
			return fmt.Errorf("ordinary fleet generation install batch %d summary %d: %w", batch.Batch, fleetID, installErr)
		}
		position++
		if installed.Name != "FleetInstalled" || !strings.EqualFold(installed.Evidence.Hotkey, fleet.Initial.Hotkey) || !strings.EqualFold(installed.Evidence.CommitmentHash, fleet.Initial.CommitmentHash) || installed.Evidence.MemberCount != finalFleetGenerationMembersPerFleet {
			return fmt.Errorf("ordinary fleet generation install batch %d summary %d differs", batch.Batch, fleetID)
		}
	}
	if position != len(write.Events) {
		return errors.New("ordinary fleet generation install has trailing events")
	}
	return nil
}

// Requires one fully ordered fourteen-event group for each fleet refresh:
// mirror, four revoke/bind/member-bound triples, then FleetRefreshed. The
// contract emits this sequence atomically, so accepting an unordered set
// would allow a valid member event to be transplanted across fleet groups.
func verifyFinalFleetGenerationRefreshEvents(evidence *FinalSemanticEvidence, lineage *FinalFleetGenerationLineageEvidence, batch FinalFleetGenerationBatchEvidence, write FinalFleetGenerationWriteEvidence) error {
	wantEvents := int(finalFleetGenerationBatchSize * (1 + finalFleetGenerationMembersPerFleet*3 + 1))
	if len(write.Events) != wantEvents {
		return fmt.Errorf("ordinary fleet generation refresh batch %d has %d events, want %d", batch.Batch, len(write.Events), wantEvents)
	}
	position := 0
	for fleetID := batch.FirstFleet; fleetID <= batch.LastFleet; fleetID++ {
		fleet := lineage.SetupFleets[fleetID-1]
		mirror, err := finalFleetGenerationDecodedEventAt(evidence, write, position)
		if err != nil {
			return fmt.Errorf("ordinary fleet generation refresh batch %d mirror %d: %w", batch.Batch, fleetID, err)
		}
		position++
		if mirror.Name != "CommitmentMirrored" || !finalFleetGenerationMirrorMatches(mirror.Evidence, fleet.Refresh) {
			return fmt.Errorf("ordinary fleet generation refresh batch %d mirror %d differs", batch.Batch, fleetID)
		}
		for memberIndex := range fleet.Refresh.Members {
			before, after := fleet.Initial.Members[memberIndex], fleet.Refresh.Members[memberIndex]
			revoked, decodeErr := finalFleetGenerationDecodedEventAt(evidence, write, position)
			if decodeErr != nil {
				return fmt.Errorf("ordinary fleet generation refresh batch %d revoke %d/%d: %w", batch.Batch, fleetID, memberIndex+1, decodeErr)
			}
			position++
			if revoked.Name != "FleetBindingRevoked" || !strings.EqualFold(revoked.Evidence.ClientID, before.ClientID) || revoked.Evidence.Generation != before.Generation || revoked.Evidence.ValidFromEpoch != after.ValidFromEpoch {
				return fmt.Errorf("ordinary fleet generation refresh batch %d revoke %d/%d differs", batch.Batch, fleetID, memberIndex+1)
			}
			bound, decodeErr := finalFleetGenerationDecodedEventAt(evidence, write, position)
			if decodeErr != nil {
				return fmt.Errorf("ordinary fleet generation refresh batch %d binding %d/%d: %w", batch.Batch, fleetID, memberIndex+1, decodeErr)
			}
			position++
			if bound.Name != "FleetBound" || !finalFleetGenerationBoundMatches(bound.Evidence, after) {
				return fmt.Errorf("ordinary fleet generation refresh batch %d binding %d/%d differs", batch.Batch, fleetID, memberIndex+1)
			}
			memberBound, decodeErr := finalFleetGenerationDecodedEventAt(evidence, write, position)
			if decodeErr != nil {
				return fmt.Errorf("ordinary fleet generation refresh batch %d member event %d/%d: %w", batch.Batch, fleetID, memberIndex+1, decodeErr)
			}
			position++
			if memberBound.Name != "FleetMemberBound" || !strings.EqualFold(memberBound.Evidence.ClientID, after.ClientID) || !strings.EqualFold(memberBound.Evidence.FleetID, after.FleetKey) || memberBound.Evidence.Generation != after.Generation || memberBound.Evidence.UID != after.UID {
				return fmt.Errorf("ordinary fleet generation refresh batch %d member event %d/%d differs", batch.Batch, fleetID, memberIndex+1)
			}
		}
		refreshed, err := finalFleetGenerationDecodedEventAt(evidence, write, position)
		if err != nil {
			return fmt.Errorf("ordinary fleet generation refresh batch %d fleet %d: %w", batch.Batch, fleetID, err)
		}
		position++
		if refreshed.Name != "FleetRefreshed" || !strings.EqualFold(refreshed.Evidence.Hotkey, fleet.Refresh.Hotkey) || !strings.EqualFold(refreshed.Evidence.CommitmentHash, fleet.Refresh.CommitmentHash) || refreshed.Evidence.MemberCount != finalFleetGenerationMembersPerFleet {
			return fmt.Errorf("ordinary fleet generation refresh batch %d fleet %d summary differs", batch.Batch, fleetID)
		}
	}
	if position != len(write.Events) {
		return errors.New("ordinary fleet generation refresh has trailing events")
	}
	return nil
}

// Decodes a separate carried receipt and checks its retained semantic fields
// before the caller joins them to one signed fleet generation.
func finalFleetGenerationSingleDecodedEvent(evidence *FinalSemanticEvidence, write FinalFleetGenerationWriteEvidence) (finalFleetGenerationDecodedEvent, error) {
	if len(write.Events) != 1 {
		return finalFleetGenerationDecodedEvent{}, fmt.Errorf("ordinary fleet generation write has %d events, want 1", len(write.Events))
	}
	return finalFleetGenerationDecodedEventAt(evidence, write, 0)
}

// Re-decodes one evidence row and rejects any stored field that differs from
// the raw ABI log. This intentionally duplicates no source-side trust: both
// collection and public replay use this same total function.
func finalFleetGenerationDecodedEventAt(evidence *FinalSemanticEvidence, write FinalFleetGenerationWriteEvidence, index int) (finalFleetGenerationDecodedEvent, error) {
	if index < 0 || index >= len(write.Events) {
		return finalFleetGenerationDecodedEvent{}, errors.New("ordinary fleet generation event index is outside its receipt")
	}
	decoded, err := finalFleetGenerationDecodeEventForBatcher(evidence, write.Action.ActionID, write.BatcherAddress, write.Events[index].Log)
	if err != nil {
		return finalFleetGenerationDecodedEvent{}, err
	}
	if !finalJSONEqual(decoded.Evidence, write.Events[index]) {
		return finalFleetGenerationDecodedEvent{}, errors.New("ordinary fleet generation event fields differ from its raw log")
	}
	return decoded, nil
}

// Matches the four fields emitted when the coordinator mirrors a native
// commitment. A successor's hash and finality must never be borrowed from its
// predecessor even if the hotkey identity remains stable.
func finalFleetGenerationMirrorMatches(event FinalFleetGenerationEventEvidence, version FinalFleetGenerationVersionEvidence) bool {
	return strings.EqualFold(event.Hotkey, version.Hotkey) && strings.EqualFold(event.CommitmentHash, version.CommitmentHash) && event.FinalizedBlock == version.NativeHead.Number && strings.EqualFold(event.FinalizedBlockHash, version.NativeHead.Hash)
}

// Matches every field the coordinator emits for one member binding. The
// commitment is linked through the preceding mirror and signed binding bytes;
// FleetBound itself intentionally does not repeat it in its Solidity event.
func finalFleetGenerationBoundMatches(event FinalFleetGenerationEventEvidence, member FinalFleetGenerationMemberEvidence) bool {
	return strings.EqualFold(event.ClientID, member.ClientID) && strings.EqualFold(event.FleetID, member.FleetKey) && strings.EqualFold(event.Hotkey, member.Hotkey) && event.UID == member.UID && event.Generation == member.Generation && event.ValidFromEpoch == member.ValidFromEpoch && event.ValidToEpoch == member.ValidToEpoch
}
