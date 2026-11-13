// Native receipts retain raw event evidence and require exactly one dispatch
// result and payer/fee event for the exact extrinsic phase. Events from another
// transaction cannot make an unconfirmed action appear successful or free.
package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// All event variants are selected by their authenticated pallet/variant index.
type rootReceiptEvent struct {
	name    string
	variant types.Si1Variant
}

// Rejects duplicate indices and validates every receipt-bearing field shape.
func rootReceiptEvents(metadata *types.Metadata) (map[[2]byte]rootReceiptEvent, error) {
	return nativeReceiptEvents(metadata, true)
}

// Common dispatch/fee evidence does not require a root-weight event for a trim.
func nativeReceiptEvents(metadata *types.Metadata, rootWeights bool) (map[[2]byte]rootReceiptEvent, error) {
	if metadata == nil || metadata.Version != 14 {
		return nil, errors.New("root receipts require reviewed metadata14")
	}
	result := map[[2]byte]rootReceiptEvent{}
	seenPallets := map[byte]bool{}
	known := map[string]int{}
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if seenPallets[byte(pallet.Index)] {
			return nil, errors.New("root receipt pallet index is duplicated")
		}
		seenPallets[byte(pallet.Index)] = true
		if !pallet.HasEvents {
			continue
		}
		entry := metadata.AsMetadataV14.EfficientLookup[pallet.Events.Type.Int64()]
		if entry == nil || !entry.Def.IsVariant {
			return nil, errors.New("root receipt event metadata is absent")
		}
		for _, variant := range entry.Def.Variant.Variants {
			key := [2]byte{byte(pallet.Index), byte(variant.Index)}
			if _, exists := result[key]; exists {
				return nil, errors.New("root receipt event index is duplicated")
			}
			name := string(pallet.Name) + "." + string(variant.Name)
			result[key] = rootReceiptEvent{name: name, variant: variant}
			known[name]++
			switch name {
			case "TransactionPayment.TransactionFeePaid":
				if len(variant.Fields) != 3 || variant.Fields[0].Name != "who" || variant.Fields[1].Name != "actual_fee" || variant.Fields[2].Name != "tip" || !rootTypeMatches(metadata, variant.Fields[0].Type, "account", 0) || !rootTypeMatches(metadata, variant.Fields[1].Type, "u64", 0) || !rootTypeMatches(metadata, variant.Fields[2].Type, "u64", 0) {
					return nil, errors.New("root receipt payer or u64 native fee shape changed")
				}
			case "System.ExtrinsicSuccess":
				if len(variant.Fields) != 1 || variant.Fields[0].Name != "dispatch_info" {
					return nil, errors.New("root receipt dispatch success shape changed")
				}
			case "System.ExtrinsicFailed":
				if len(variant.Fields) != 2 || variant.Fields[0].Name != "dispatch_error" || variant.Fields[1].Name != "dispatch_info" {
					return nil, errors.New("root receipt dispatch failure shape changed")
				}
			case "SubtensorModule.RootWeightsSet":
				if len(variant.Fields) != 1 || !rootTypeMatches(metadata, variant.Fields[0].Type, "u16", 0) {
					return nil, errors.New("root receipt root-weight seat shape changed")
				}
			}
		}
	}
	required := []string{"TransactionPayment.TransactionFeePaid", "System.ExtrinsicSuccess", "System.ExtrinsicFailed"}
	if rootWeights {
		required = append(required, "SubtensorModule.RootWeightsSet")
	}
	for _, name := range required {
		if known[name] != 1 {
			return nil, fmt.Errorf("root receipt required event %s absent or duplicated", name)
		}
	}
	return result, nil
}

// Raw events are hashed as one canonical SCALE storage bundle. A successful
// basket call additionally requires its exact seat's RootWeightsSet event;
// dispatch failure may not borrow a root-weight event from another phase.
func rootDecodeReceiptEvents(metadata *types.Metadata, raw []byte, extrinsicIndex uint32, bodyCount int, action rootAction) (rootActionReceipt, error) {
	return nativeDecodeReceiptEvents(metadata, raw, extrinsicIndex, bodyCount, action.Scope.Hotkey, &action.Scope.Seat.Uid)
}

// The optional root seat selects only the root protocol's extra event. A trim
// has no per-removal event; its census correspondence must remain separate.
func nativeDecodeReceiptEvents(metadata *types.Metadata, raw []byte, extrinsicIndex uint32, bodyCount int, payer string, rootSeat *uint16) (rootActionReceipt, error) {
	receipt := rootActionReceipt{EventHash: rootExtrinsicHash(raw)}
	if len(raw) == 0 || len(raw) > rootBodyBytesLimit || int(extrinsicIndex) >= bodyCount {
		return receipt, errors.New("root receipt event bundle or inclusion index is invalid")
	}
	events, err := nativeReceiptEvents(metadata, rootSeat != nil)
	if err != nil {
		return receipt, err
	}
	terminalCount, feeCount, rootWeightCount := 0, 0, 0
	err = walkNativeEventRecords(metadata, raw, bodyCount, events, rootBodyCountLimit, func(record nativeEventRecord) error {
		if record.extrinsicIndex == nil || *record.extrinsicIndex != extrinsicIndex {
			return nil
		}
		switch record.event.name {
		case "System.ExtrinsicSuccess":
			terminalCount++
			receipt.Success = true
		case "System.ExtrinsicFailed":
			terminalCount++
			receipt.DispatchError = "scale:0x" + hex.EncodeToString(record.fields[0])
		case "TransactionPayment.TransactionFeePaid":
			feeCount++
			if "0x"+hex.EncodeToString(record.fields[0]) != payer || binary.LittleEndian.Uint64(record.fields[2]) != 0 {
				return errors.New("root receipt actual payer or tip differs from direct zero-tip action")
			}
			receipt.ActualFeeRao = binary.LittleEndian.Uint64(record.fields[1])
		case "SubtensorModule.RootWeightsSet":
			rootWeightCount++
			if rootSeat == nil || binary.LittleEndian.Uint16(record.fields[0]) != *rootSeat {
				return errors.New("root receipt weight event has another root seat")
			}
		}
		return nil
	})
	if err != nil {
		return receipt, err
	}

	wantedWeights := 0
	if rootSeat != nil && receipt.Success {
		wantedWeights = 1
	}
	if terminalCount != 1 || feeCount != 1 || rootWeightCount != wantedWeights {
		return receipt, errors.New("root receipt has trailing, missing, duplicated or contradictory outcome evidence")
	}
	return receipt, nil
}
