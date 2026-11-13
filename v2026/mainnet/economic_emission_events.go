// Miner incentive evidence is decoded from the exact execution metadata. The
// reviewed event reports UID amounts, not the pre-withholding miner tranche.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Validate the outer event record as well as pallet variants; otherwise a
// compatible-looking inner event could be read through a changed envelope.
func economicEmissionEventProfile(metadata *types.Metadata) (map[[2]byte]rootReceiptEvent, error) {
	events, err := nativeReceiptEvents(metadata, false)
	if err != nil {
		return nil, err
	}
	entry, err := rootSystemEntry(metadata, "Events")
	if err != nil || !entry.Type.IsPlainType || !entry.Modifier.IsDefault || entry.Modifier.IsOptional || !bytes.Equal(entry.Fallback, []byte{0}) {
		return nil, errors.New("native incentive System.Events query/default changed")
	}
	sequence := metadata.AsMetadataV14.EfficientLookup[entry.Type.AsPlainType.Int64()]
	if sequence == nil || !sequence.Def.IsSequence {
		return nil, errors.New("native incentive Events is not a sequence")
	}
	record := metadata.AsMetadataV14.EfficientLookup[sequence.Def.Sequence.Type.Int64()]
	if record == nil || !record.Def.IsComposite || len(record.Def.Composite.Fields) != 3 {
		return nil, errors.New("native incentive event record changed")
	}
	fields := record.Def.Composite.Fields
	if fields[0].Name != "phase" || fields[1].Name != "event" || fields[2].Name != "topics" || !rootTypeMatches(metadata, fields[2].Type, "accounts", 0) {
		return nil, errors.New("native incentive event record fields/topics changed")
	}
	phase := metadata.AsMetadataV14.EfficientLookup[fields[0].Type.Int64()]
	if phase == nil || !phase.Def.IsVariant || len(phase.Def.Variant.Variants) != 3 {
		return nil, errors.New("native incentive event phase changed")
	}
	seenPhases := map[byte]bool{}
	for _, variant := range phase.Def.Variant.Variants {
		index := byte(variant.Index)
		valid := index == 0 && variant.Name == "ApplyExtrinsic" && len(variant.Fields) == 1 && rootTypeMatches(metadata, variant.Fields[0].Type, "u32", 0) ||
			index == 1 && variant.Name == "Finalization" && len(variant.Fields) == 0 || index == 2 && variant.Name == "Initialization" && len(variant.Fields) == 0
		if seenPhases[index] || !valid {
			return nil, errors.New("native incentive event phase encoding changed")
		}
		seenPhases[index] = true
	}
	outer := metadata.AsMetadataV14.EfficientLookup[fields[1].Type.Int64()]
	if outer == nil || !outer.Def.IsVariant {
		return nil, errors.New("native incentive RuntimeEvent is absent")
	}
	palletKVs := map[string]types.PalletMetadataV14{}
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if _, exists := palletKVs[string(pallet.Name)]; exists {
			return nil, errors.New("native incentive pallet name is duplicated")
		}
		palletKVs[string(pallet.Name)] = pallet
	}
	seenOuter := map[byte]bool{}
	for _, variant := range outer.Def.Variant.Variants {
		pallet, exists := palletKVs[string(variant.Name)]
		if !exists || !pallet.HasEvents || byte(pallet.Index) != byte(variant.Index) || seenOuter[byte(variant.Index)] || len(variant.Fields) != 1 || variant.Fields[0].Type.Int64() != pallet.Events.Type.Int64() {
			return nil, errors.New("native incentive RuntimeEvent differs from pallet metadata")
		}
		seenOuter[byte(variant.Index)] = true
	}
	for _, pallet := range palletKVs {
		if pallet.HasEvents && !seenOuter[byte(pallet.Index)] {
			return nil, errors.New("native incentive RuntimeEvent omits a pallet")
		}
	}
	required := map[string][]string{
		"SubtensorModule.IncentiveAlphaEmittedToMiners": {"netuid:u16", "emissions:u64s"},
		"SubtensorModule.EpochDeferred":                 {"netuid:u16", "from_block:u64", "to_block:u64"},
		"SubtensorModule.EpochSkipped":                  {"netuid:u16", "block:u64"},
		"SubtensorModule.TempoSet":                      {":u16", ":u16"},
		"SubtensorModule.SubnetOwnerChanged":            {"netuid:u16", "old_coldkey:account", "new_coldkey:account"},
	}
	seenNames := map[string]bool{}
	for _, event := range events {
		if seenNames[event.name] {
			return nil, errors.New("native incentive event name is duplicated")
		}
		seenNames[event.name] = true
		shapes, selected := required[event.name]
		if !selected {
			continue
		}
		if len(event.variant.Fields) != len(shapes) {
			return nil, fmt.Errorf("native incentive %s field count changed", event.name)
		}
		for index, shape := range shapes {
			field := event.variant.Fields[index]
			colon := bytes.IndexByte([]byte(shape), ':')
			if field.HasName != (colon != 0) || string(field.Name) != shape[:colon] || !rootTypeMatches(metadata, field.Type, shape[colon+1:], 0) {
				return nil, fmt.Errorf("native incentive %s field encoding changed", event.name)
			}
		}
	}
	for name := range required {
		if !seenNames[name] {
			return nil, fmt.Errorf("native incentive required event %s absent", name)
		}
	}
	return events, nil
}

// Fully traverse all event fields/topics, including other subnets, before any
// target amount is returned. No event index or mechanism is silently skipped.
func decodeEconomicEmissionEvents(metadata *types.Metadata, raw []byte, bodyCount int, netuid, maximumUids uint16, block uint64) ([]economicEmissionEvent, []economicEmissionContextEvent, error) {
	if len(raw) == 0 || len(raw) > economicEmissionEventBytesLimit || bodyCount < 0 || bodyCount > rootBodyCountLimit || netuid >= economicEmissionMechanismStride || maximumUids == 0 || maximumUids > rootCensusLimit {
		return nil, nil, errors.New("native incentive event bundle exceeds observation bounds")
	}
	eventKVs, err := economicEmissionEventProfile(metadata)
	if err != nil {
		return nil, nil, err
	}
	selected := []economicEmissionEvent{}
	contextEvents := []economicEmissionContextEvent{}
	err = walkNativeEventRecords(metadata, raw, bodyCount, eventKVs, economicEmissionEventLimit, func(record nativeEventRecord) error {
		event, fields, eventIndex, extrinsicIndex := record.event, record.fields, record.index, record.extrinsicIndex
		if event.name == "SubtensorModule.TempoSet" || event.name == "SubtensorModule.SubnetOwnerChanged" {
			if binary.LittleEndian.Uint16(fields[0]) != netuid {
				return nil
			}
			if record.phase == 1 || event.name == "SubtensorModule.SubnetOwnerChanged" && record.phase != 2 {
				return errors.New("native incentive execution context has an unreviewed phase")
			}
			context := economicEmissionContextEvent{EventIndex: eventIndex, Kind: event.name, Netuid: netuid, Phase: "Initialization", ExtrinsicIndex: extrinsicIndex}
			if record.phase == 0 {
				context.Phase = "ApplyExtrinsic"
			}
			if event.name == "SubtensorModule.TempoSet" {
				value := binary.LittleEndian.Uint16(fields[1])
				context.Tempo = &value
			} else {
				context.OldOwnerColdkey, context.NewOwnerColdkey = "0x"+hex.EncodeToString(fields[1]), "0x"+hex.EncodeToString(fields[2])
				if context.OldOwnerColdkey == context.NewOwnerColdkey {
					return errors.New("native incentive takeover did not change the owner")
				}
			}
			contextEvents = append(contextEvents, context)
			return nil
		}
		if event.name != "SubtensorModule.IncentiveAlphaEmittedToMiners" && event.name != "SubtensorModule.EpochDeferred" && event.name != "SubtensorModule.EpochSkipped" {
			return nil
		}
		index := binary.LittleEndian.Uint16(fields[0])
		baseNetuid, mechanism := index, uint8(0)
		if event.name == "SubtensorModule.IncentiveAlphaEmittedToMiners" {
			baseNetuid, mechanism = index%economicEmissionMechanismStride, uint8(index/economicEmissionMechanismStride)
		}
		if baseNetuid != netuid {
			return nil
		}
		if record.phase != 2 || mechanism != 0 {
			return errors.New("native incentive target event is not initialization of reviewed mechanism zero")
		}
		// Exactly one terminal epoch event per target block in this profile.
		if len(selected) != 0 {
			return errors.New("native incentive target epoch event is duplicated or contradictory")
		}
		observed := economicEmissionEvent{EventIndex: eventIndex, Kind: event.name, Netuid: baseNetuid, Mechanism: mechanism}
		switch event.name {
		case "SubtensorModule.IncentiveAlphaEmittedToMiners":
			values, count, err := rootVector(fields[1], 8, 0)
			if err != nil || count > int(maximumUids) {
				return errors.New("native incentive UID vector exceeds policy bound")
			}
			observed.AlphaByUid = make([]string, count)
			total := new(big.Int)
			for uid := 0; uid < count; uid++ {
				value := binary.LittleEndian.Uint64(values[uid*8:])
				observed.AlphaByUid[uid] = strconv.FormatUint(value, 10)
				total.Add(total, new(big.Int).SetUint64(value))
			}
			observed.TotalAlpha = total.String()
		case "SubtensorModule.EpochDeferred":
			observed.FromBlock, observed.ToBlock = binary.LittleEndian.Uint64(fields[1]), binary.LittleEndian.Uint64(fields[2])
			if observed.FromBlock != block || observed.ToBlock != block+1 {
				return errors.New("native incentive deferred epoch block differs from event block")
			}
		case "SubtensorModule.EpochSkipped":
			observed.FromBlock = binary.LittleEndian.Uint64(fields[1])
			if observed.FromBlock != block {
				return errors.New("native incentive skipped epoch block differs from event block")
			}
		}
		selected = append(selected, observed)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return selected, contextEvents, nil
}
