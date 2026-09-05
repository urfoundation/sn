package main

// This file reconstructs the exact v453 native epoch payout from public,
// block-pinned Substrate state. It deliberately uses the CRv4 reveal block:
// v453 reveals matured weights before coinbase in the same on_initialize hook.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	gsrpcregistry "github.com/centrifuge/go-substrate-rpc-client/v4/registry"
	gsrpcparser "github.com/centrifuge/go-substrate-rpc-client/v4/registry/parser"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	gsrpccodec "github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/crv4"
)

const (
	finalNativeEpochEmissionEvent             = "SubtensorModule.IncentiveAlphaEmittedToMiners"
	finalNativeStakeAddedEvent                = "SubtensorModule.StakeAdded"
	finalNativeStakeRemovedEvent              = "SubtensorModule.StakeRemoved"
	finalNativeStakeMovedEvent                = "SubtensorModule.StakeMoved"
	finalNativeStakeTransferredEvent          = "SubtensorModule.StakeTransferred"
	finalNativeStakeSwappedEvent              = "SubtensorModule.StakeSwapped"
	finalNativeStakeAndHotkeyTransferredEvent = "SubtensorModule.StakeAndHotkeyTransferred"
	finalNativeAllBalanceTransferredEvent     = "SubtensorModule.AllBalanceUnstakedAndTransferredToNewColdkey"
	finalNativeAlphaRecycledEvent             = "SubtensorModule.AlphaRecycled"
	finalNativeAlphaBurnedEvent               = "SubtensorModule.AlphaBurned"
	finalNativeAutoStakeAddedEvent            = "SubtensorModule.AutoStakeAdded"
	finalNativeHotkeySwappedEvent             = "SubtensorModule.HotkeySwapped"
	finalNativeHotkeySwappedOnSubnetEvent     = "SubtensorModule.HotkeySwappedOnSubnet"
	finalNativeColdkeySwappedEvent            = "SubtensorModule.ColdkeySwapped"
	finalNativeBasketDepositedEvent           = "SubtensorModule.BasketDeposited"
	finalNativeBasketStakedInEvent            = "SubtensorModule.BasketStakedIn"
	finalNativeBasketClaimedEvent             = "SubtensorModule.BasketClaimed"
	finalNativeBasketHoldingConvertedEvent    = "SubtensorModule.BasketHoldingConverted"
	finalNativeAlphaFeeEvent                  = "SubtensorModule.TransactionFeePaidWithAlpha"
)

// Separates parent and reveal-block exchanges so the caller can append each
// group under its exact transcript head.
type finalNativeEpochPayoutRead struct {
	State           FinalNativeEpochPayoutState
	ParentExchanges []FinalRPCExchange
	BlockExchanges  []FinalRPCExchange
}

// Defines the historical reader surface mandatory for evidence carrying native
// rewards. Without it, a broad stake delta cannot be tied to the corresponding
// runtime epoch.
type finalSemanticNativeEpochPayoutChainReader interface {
	NativeEpochPayout(context.Context, uint64, ChainHead) (finalNativeEpochPayoutRead, error)
}

// Selects and UID-sorts the exact signed subjects for one settlement epoch
// before any public query is made.
func finalNativeEpochRewardRows(evidence *FinalSemanticEvidence, epoch uint64) ([]FinalNativeRewardDelta, error) {
	if evidence == nil {
		return nil, errors.New("native epoch reward evidence is unavailable")
	}
	rows := make([]FinalNativeRewardDelta, 0)
	for _, reward := range evidence.NativeRewards {
		if reward.Epoch == epoch {
			rows = append(rows, reward)
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("native payout epoch %d has no signed reward rows", epoch)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].UID < rows[j].UID })
	seenUIDs := make(map[uint16]bool, len(rows))
	seenHotkeys := make(map[string]bool, len(rows))
	for _, row := range rows {
		hotkey, err := finalNativeAccountHex(row.Hotkey)
		if err != nil {
			return nil, fmt.Errorf("native payout epoch %d UID %d hotkey: %w", epoch, row.UID, err)
		}
		if seenUIDs[row.UID] || seenHotkeys[hotkey] {
			return nil, fmt.Errorf("native payout epoch %d repeats UID or hotkey %d/%s", epoch, row.UID, hotkey)
		}
		seenUIDs[row.UID], seenHotkeys[hotkey] = true, true
	}
	return rows, nil
}

// Flattens the metadata-decoded Vec<AlphaBalance> without accepting non-u64
// leaves or an empty event payload.
func finalNativeEpochEventVector(value any) ([]uint64, error) {
	var leaves []finalNativeDecodedLeaf
	if err := finalNativeDecodedLeaves(value, "", &leaves); err != nil {
		return nil, err
	}
	if len(leaves) == 0 {
		return nil, errors.New("native epoch emission event has an empty vector")
	}
	result := make([]uint64, len(leaves))
	for index, leaf := range leaves {
		value, err := finalNativeUnsigned(leaf.value, 64)
		if err != nil {
			return nil, fmt.Errorf("native epoch emission UID %d: %w", index, err)
		}
		result[index] = value
	}
	return result, nil
}

// Decodes one metadata-derived AccountId32 field.
func finalNativeEventAccount(value any) ([32]byte, error) {
	decoded, err := finalNativeDecodedBytes(value, 32)
	if err != nil {
		return [32]byte{}, err
	}
	var result [32]byte
	copy(result[:], decoded)
	return result, nil
}

// Identifies an extrinsic or redirected auto-stake that can change a managed
// payout subject's aggregate stake in the reveal block. Unrelated
// shared-testnet staking remains permissible.
func finalNativeRelevantStakeMutation(event *gsrpcparser.Event, netuid uint16, hotkeys, coldkeys map[[32]byte]bool) (bool, error) {
	if event == nil || event.Phase == nil {
		return false, errors.New("native epoch event has no phase")
	}
	name := finalNativeEventName(event.Name)
	account := func(index int) ([32]byte, error) {
		if index >= len(event.Fields) || event.Fields[index] == nil {
			return [32]byte{}, errors.New("native stake event omits an account")
		}
		return finalNativeEventAccount(event.Fields[index].Value)
	}
	network := func(index int) (uint16, error) {
		if index >= len(event.Fields) || event.Fields[index] == nil {
			return 0, errors.New("native stake event omits a netuid")
		}
		value, err := finalNativeUnsigned(event.Fields[index].Value, 16)
		return uint16(value), err
	}
	contains := func(set map[[32]byte]bool, values ...[32]byte) bool {
		for _, value := range values {
			if set[value] {
				return true
			}
		}
		return false
	}

	switch name {
	case finalNativeAutoStakeAddedEvent:
		if !event.Phase.IsInitialization || len(event.Fields) != 5 {
			return false, errors.New("native AutoStakeAdded event has an invalid phase or shape")
		}
		eventNetuid, err := network(0)
		if err != nil || eventNetuid != netuid {
			return false, err
		}
		destination, err := account(1)
		if err != nil {
			return false, err
		}
		hotkey, err := account(2)
		if err != nil {
			return false, err
		}
		return hotkeys[hotkey] && destination != hotkey, nil
	case finalNativeStakeAddedEvent, finalNativeStakeRemovedEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 6 {
			return false, errors.New("native stake add/remove event has an invalid shape")
		}
		coldkey, err := account(0)
		if err != nil {
			return false, err
		}
		hotkey, err := account(1)
		if err != nil {
			return false, err
		}
		eventNetuid, err := network(4)
		return eventNetuid == netuid && (hotkeys[hotkey] || coldkeys[coldkey]), err
	case finalNativeStakeMovedEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 6 {
			return false, errors.New("native StakeMoved event has an invalid shape")
		}
		coldkey, err := account(0)
		if err != nil {
			return false, err
		}
		origin, err := account(1)
		if err != nil {
			return false, err
		}
		originNetuid, err := network(2)
		if err != nil {
			return false, err
		}
		destination, err := account(3)
		if err != nil {
			return false, err
		}
		destinationNetuid, err := network(4)
		if err != nil {
			return false, err
		}
		return originNetuid == netuid && (hotkeys[origin] || coldkeys[coldkey]) || destinationNetuid == netuid && (hotkeys[destination] || coldkeys[coldkey]), nil
	case finalNativeStakeTransferredEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 6 {
			return false, errors.New("native StakeTransferred event has an invalid shape")
		}
		originColdkey, err := account(0)
		if err != nil {
			return false, err
		}
		destinationColdkey, err := account(1)
		if err != nil {
			return false, err
		}
		hotkey, err := account(2)
		if err != nil {
			return false, err
		}
		originNetuid, err := network(3)
		if err != nil {
			return false, err
		}
		destinationNetuid, err := network(4)
		if err != nil {
			return false, err
		}
		relevantIdentity := hotkeys[hotkey] || contains(coldkeys, originColdkey, destinationColdkey)
		return relevantIdentity && (originNetuid == netuid || destinationNetuid == netuid), nil
	case finalNativeStakeSwappedEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 5 {
			return false, errors.New("native StakeSwapped event has an invalid shape")
		}
		coldkey, err := account(0)
		if err != nil {
			return false, err
		}
		hotkey, err := account(1)
		if err != nil {
			return false, err
		}
		originNetuid, err := network(2)
		if err != nil {
			return false, err
		}
		destinationNetuid, err := network(3)
		if err != nil {
			return false, err
		}
		return (hotkeys[hotkey] || coldkeys[coldkey]) && (originNetuid == netuid || destinationNetuid == netuid), nil
	case finalNativeStakeAndHotkeyTransferredEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 7 {
			return false, errors.New("native StakeAndHotkeyTransferred event has an invalid shape")
		}
		originColdkey, err := account(0)
		if err != nil {
			return false, err
		}
		destinationColdkey, err := account(1)
		if err != nil {
			return false, err
		}
		originHotkey, err := account(2)
		if err != nil {
			return false, err
		}
		destinationHotkey, err := account(3)
		if err != nil {
			return false, err
		}
		originNetuid, err := network(4)
		if err != nil {
			return false, err
		}
		destinationNetuid, err := network(5)
		if err != nil {
			return false, err
		}
		originRelevant := hotkeys[originHotkey] || coldkeys[originColdkey]
		destinationRelevant := hotkeys[destinationHotkey] || coldkeys[destinationColdkey]
		return originNetuid == netuid && originRelevant || destinationNetuid == netuid && destinationRelevant, nil
	case finalNativeAllBalanceTransferredEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 3 {
			return false, errors.New("native all-balance transfer event has an invalid shape")
		}
		origin, err := account(0)
		if err != nil {
			return false, err
		}
		destination, err := account(1)
		if err != nil {
			return false, err
		}
		return contains(coldkeys, origin, destination), nil
	case finalNativeAlphaRecycledEvent, finalNativeAlphaBurnedEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 4 {
			return false, errors.New("native alpha recycle/burn event has an invalid shape")
		}
		coldkey, err := account(0)
		if err != nil {
			return false, err
		}
		hotkey, err := account(1)
		if err != nil {
			return false, err
		}
		eventNetuid, err := network(3)
		return eventNetuid == netuid && (hotkeys[hotkey] || coldkeys[coldkey]), err
	case finalNativeHotkeySwappedEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 3 {
			return false, errors.New("native HotkeySwapped event has an invalid shape")
		}
		coldkey, err := account(0)
		if err != nil {
			return false, err
		}
		oldHotkey, err := account(1)
		if err != nil {
			return false, err
		}
		newHotkey, err := account(2)
		if err != nil {
			return false, err
		}
		return coldkeys[coldkey] || contains(hotkeys, oldHotkey, newHotkey), nil
	case finalNativeHotkeySwappedOnSubnetEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 4 {
			return false, errors.New("native HotkeySwappedOnSubnet event has an invalid shape")
		}
		coldkey, err := account(0)
		if err != nil {
			return false, err
		}
		oldHotkey, err := account(1)
		if err != nil {
			return false, err
		}
		newHotkey, err := account(2)
		if err != nil {
			return false, err
		}
		eventNetuid, err := network(3)
		return eventNetuid == netuid && (coldkeys[coldkey] || contains(hotkeys, oldHotkey, newHotkey)), err
	case finalNativeColdkeySwappedEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 2 {
			return false, errors.New("native ColdkeySwapped event has an invalid shape")
		}
		oldColdkey, err := account(0)
		if err != nil {
			return false, err
		}
		newColdkey, err := account(1)
		if err != nil {
			return false, err
		}
		return contains(coldkeys, oldColdkey, newColdkey), nil
	case finalNativeBasketDepositedEvent:
		if len(event.Fields) != 3 {
			return false, errors.New("native BasketDeposited event has an invalid shape")
		}
		hotkey, err := account(0)
		return hotkeys[hotkey], err
	case finalNativeBasketStakedInEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 5 {
			return false, errors.New("native BasketStakedIn event has an invalid shape")
		}
		hotkey, err := account(0)
		if err != nil {
			return false, err
		}
		coldkey, err := account(1)
		return hotkeys[hotkey] || coldkeys[coldkey], err
	case finalNativeBasketClaimedEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 3 {
			return false, errors.New("native BasketClaimed event has an invalid shape")
		}
		hotkey, err := account(0)
		if err != nil {
			return false, err
		}
		coldkey, err := account(1)
		return hotkeys[hotkey] || coldkeys[coldkey], err
	case finalNativeBasketHoldingConvertedEvent:
		if len(event.Fields) != 3 {
			return false, errors.New("native BasketHoldingConverted event has an invalid shape")
		}
		hotkey, err := account(0)
		if err != nil {
			return false, err
		}
		eventNetuid, err := network(1)
		return eventNetuid == netuid && hotkeys[hotkey], err
	case finalNativeAlphaFeeEvent:
		if !event.Phase.IsApplyExtrinsic {
			return false, nil
		}
		if len(event.Fields) != 4 {
			return false, errors.New("native alpha-fee event has an invalid shape")
		}
		payer, err := account(0)
		if err != nil {
			return false, err
		}
		eventNetuid, err := network(1)
		return eventNetuid == netuid && (hotkeys[payer] || coldkeys[payer]), err
	default:
		return false, nil
	}
}

// Extracts the unique server-emission vector and counts every stake mutation
// relevant to a managed payout identity.
func finalNativeEpochEvents(events []*gsrpcparser.Event, netuid uint16, hotkeys, coldkeys map[[32]byte]bool) ([]uint64, uint64, error) {
	var emissions []uint64
	var mutations uint64
	for _, event := range events {
		if event == nil || event.Phase == nil {
			return nil, 0, errors.New("native epoch contains a malformed event")
		}
		if finalNativeEventName(event.Name) == finalNativeEpochEmissionEvent {
			if !event.Phase.IsInitialization || len(event.Fields) != 2 || event.Fields[0] == nil || event.Fields[1] == nil {
				return nil, 0, errors.New("native epoch emission event has an invalid phase or shape")
			}
			eventNetuid, err := finalNativeUnsigned(event.Fields[0].Value, 16)
			if err != nil {
				return nil, 0, err
			}
			if uint16(eventNetuid) == netuid {
				if emissions != nil {
					return nil, 0, fmt.Errorf("native epoch has multiple emission events for netuid %d", netuid)
				}
				emissions, err = finalNativeEpochEventVector(event.Fields[1].Value)
				if err != nil {
					return nil, 0, err
				}
			}
		}
		relevant, err := finalNativeRelevantStakeMutation(event, netuid, hotkeys, coldkeys)
		if err != nil {
			return nil, 0, err
		}
		if relevant {
			mutations++
		}
	}
	if emissions == nil {
		return nil, 0, fmt.Errorf("native epoch has no emission event for netuid %d", netuid)
	}
	return emissions, mutations, nil
}

// Authenticates and parses the exact block-pinned System.Events value with its
// historical metadata.
func finalNativeParseEpochEvents(metadata *gsrpctypes.Metadata, raw json.RawMessage, netuid uint16, hotkeys, coldkeys map[[32]byte]bool) ([]uint64, uint64, error) {
	if metadata == nil {
		return nil, 0, errors.New("native epoch metadata is unavailable")
	}
	encoded, err := finalDecodeRPCString("System.Events", raw)
	if err != nil {
		return nil, 0, err
	}
	decoded, err := gsrpccodec.HexDecodeString(encoded)
	if err != nil {
		return nil, 0, fmt.Errorf("decode native epoch System.Events: %w", err)
	}
	registry, err := gsrpcregistry.NewFactory().CreateEventRegistry(metadata)
	if err != nil {
		return nil, 0, fmt.Errorf("initialize native epoch event decoder: %w", err)
	}
	storage := gsrpctypes.StorageDataRaw(decoded)
	events, err := gsrpcparser.NewEventParser().ParseEvents(registry, &storage)
	if err != nil {
		return nil, 0, fmt.Errorf("decode native epoch events: %w", err)
	}
	return finalNativeEpochEvents(events, netuid, hotkeys, coldkeys)
}

// Obtains the canonical parent identity from the reveal block's authenticated
// header rather than inferring a hash from history.
func finalNativeParentHead(raw json.RawMessage, block ChainHead) (ChainHead, error) {
	var header struct {
		Number     string `json:"number"`
		ParentHash string `json:"parentHash"`
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &header) != nil || header.Number == "" || header.ParentHash == "" {
		return ChainHead{}, errors.New("native payout block header is invalid")
	}
	number, err := finalDecodeBlockNumber("native payout", header.Number)
	if err != nil || number != block.Number || number == 0 || requireFinalHex32("native payout parent hash", strings.ToLower(header.ParentHash)) != nil {
		return ChainHead{}, stateMismatchError(err, "native payout header does not match reveal block")
	}
	return ChainHead{Number: number - 1, Hash: strings.ToLower(header.ParentHash)}, nil
}

// Reads the event, epoch channels, UID identities, and exact parent-to-reveal
// aggregate stake transition with authenticated metadata.
func (self *PublicFinalSemanticChainReader) NativeEpochPayout(ctx context.Context, epoch uint64, block ChainHead) (finalNativeEpochPayoutRead, error) {
	if self == nil || self.evidence == nil {
		return finalNativeEpochPayoutRead{}, errors.New("public native payout reader is unavailable")
	}
	reveal, subnetEpoch, err := finalNativeEpochReveal(self.evidence, epoch)
	if err != nil || reveal != block {
		return finalNativeEpochPayoutRead{}, stateMismatchError(err, "public native payout epoch %d has another reveal block", epoch)
	}
	rows, err := finalNativeEpochRewardRows(self.evidence, epoch)
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	blockMetadata, blockExchanges, err := self.substrateMetadata(ctx, block)
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	headerRaw, headerExchange, err := self.substrateRaw(ctx, block, "chain_getHeader", block.Hash)
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	blockExchanges = append(blockExchanges, headerExchange)
	parent, err := finalNativeParentHead(headerRaw, block)
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	parentMetadata, parentExchanges, err := self.substrateMetadata(ctx, parent)
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}

	type payoutKeys struct {
		row         FinalNativeRewardDelta
		hotkey      [32]byte
		uidKey      gsrpctypes.StorageKey
		beforeStake gsrpctypes.StorageKey
		afterStake  gsrpctypes.StorageKey
	}
	keys := make([]payoutKeys, len(rows))
	blockKeys := make([]gsrpctypes.StorageKey, 0, 2*len(rows)+3)
	parentKeys := make([]gsrpctypes.StorageKey, 0, len(rows))
	hotkeys, coldkeys := make(map[[32]byte]bool, len(rows)), make(map[[32]byte]bool, len(rows)*2)
	for index, row := range rows {
		hotkey, err := finalNativeAccountPublicKey(row.Hotkey)
		if err != nil {
			return finalNativeEpochPayoutRead{}, err
		}
		keys[index].row, keys[index].hotkey = row, hotkey
		keys[index].uidKey, err = gsrpctypes.CreateStorageKey(blockMetadata, crv4.PalletName, "Keys", netuidArg(self.evidence.Netuid), netuidArg(row.UID))
		if err != nil {
			return finalNativeEpochPayoutRead{}, err
		}
		keys[index].afterStake, err = gsrpctypes.CreateStorageKey(blockMetadata, crv4.PalletName, "TotalHotkeyAlpha", hotkey[:], netuidArg(self.evidence.Netuid))
		if err != nil {
			return finalNativeEpochPayoutRead{}, err
		}
		keys[index].beforeStake, err = gsrpctypes.CreateStorageKey(parentMetadata, crv4.PalletName, "TotalHotkeyAlpha", hotkey[:], netuidArg(self.evidence.Netuid))
		if err != nil {
			return finalNativeEpochPayoutRead{}, err
		}
		blockKeys = append(blockKeys, keys[index].uidKey, keys[index].afterStake)
		parentKeys = append(parentKeys, keys[index].beforeStake)
		hotkeys[hotkey] = true
		for _, encoded := range []string{row.OwnerColdkey, row.ReserveColdkey} {
			if encoded == "" {
				continue
			}
			key, keyErr := finalNativeAccountPublicKey(encoded)
			if keyErr != nil {
				return finalNativeEpochPayoutRead{}, keyErr
			}
			coldkeys[key] = true
		}
	}
	emissionKey, err := gsrpctypes.CreateStorageKey(blockMetadata, crv4.PalletName, "Emission", netuidArg(self.evidence.Netuid))
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	incentiveKey, err := gsrpctypes.CreateStorageKey(blockMetadata, crv4.PalletName, "Incentive", netuidArg(self.evidence.Netuid))
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	dividendsKey, err := gsrpctypes.CreateStorageKey(blockMetadata, crv4.PalletName, "Dividends", netuidArg(self.evidence.Netuid))
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	blockKeys = append(blockKeys, emissionKey, incentiveKey, dividendsKey)

	blockValues, blockStorageExchange, err := self.substrateQueryStorageExact(ctx, block, blockKeys)
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	blockExchanges = append(blockExchanges, blockStorageExchange)
	parentValues, parentStorageExchange, err := self.substrateQueryStorageExact(ctx, parent, parentKeys)
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	parentExchanges = append(parentExchanges, parentStorageExchange)
	var combined []gsrpctypes.U64
	var incentives, dividends []gsrpctypes.U16
	if err := decodeFinalSubstrateQueryValue(blockValues, emissionKey, crv4.PalletName+".Emission", &combined); err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	if err := decodeFinalSubstrateQueryValue(blockValues, incentiveKey, crv4.PalletName+".Incentive", &incentives); err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	if err := decodeFinalSubstrateQueryValue(blockValues, dividendsKey, crv4.PalletName+".Dividends", &dividends); err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	if len(combined) == 0 || len(combined) != len(incentives) || len(combined) != len(dividends) {
		return finalNativeEpochPayoutRead{}, errors.New("native payout storage vectors have inconsistent lengths")
	}
	eventsKey, err := gsrpctypes.CreateStorageKey(blockMetadata, "System", "Events")
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	eventsRaw, eventsExchange, err := self.substrateRaw(ctx, block, "state_getStorage", eventsKey.Hex(), block.Hash)
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	blockExchanges = append(blockExchanges, eventsExchange)
	server, mutations, err := finalNativeParseEpochEvents(blockMetadata, eventsRaw, self.evidence.Netuid, hotkeys, coldkeys)
	if err != nil {
		return finalNativeEpochPayoutRead{}, err
	}
	if len(server) != len(combined) {
		return finalNativeEpochPayoutRead{}, fmt.Errorf("native payout event vector=%d, storage vector=%d", len(server), len(combined))
	}

	state := FinalNativeEpochPayoutState{SettlementEpoch: epoch, SubnetEpoch: subnetEpoch, Netuid: self.evidence.Netuid, Parent: parent, Block: block, ManualStakeMutations: mutations, UIDs: make([]FinalNativeEpochPayoutUIDState, 0, len(rows))}
	for _, item := range keys {
		if int(item.row.UID) >= len(combined) {
			return finalNativeEpochPayoutRead{}, fmt.Errorf("native payout vectors omit managed UID %d", item.row.UID)
		}
		var observedHotkey gsrpctypes.AccountID
		if err := decodeFinalSubstrateQueryValue(blockValues, item.uidKey, crv4.PalletName+".Keys", &observedHotkey); err != nil {
			return finalNativeEpochPayoutRead{}, err
		}
		if !bytes.Equal(observedHotkey[:], item.hotkey[:]) {
			return finalNativeEpochPayoutRead{}, fmt.Errorf("native payout UID %d has another hotkey", item.row.UID)
		}
		var before, after gsrpctypes.U64
		if err := decodeFinalSubstrateQueryValue(parentValues, item.beforeStake, crv4.PalletName+".TotalHotkeyAlpha(parent)", &before); err != nil {
			return finalNativeEpochPayoutRead{}, err
		}
		if err := decodeFinalSubstrateQueryValue(blockValues, item.afterStake, crv4.PalletName+".TotalHotkeyAlpha(reveal)", &after); err != nil {
			return finalNativeEpochPayoutRead{}, err
		}
		state.UIDs = append(state.UIDs, FinalNativeEpochPayoutUIDState{
			UID: item.row.UID, Hotkey: "0x" + hex.EncodeToString(item.hotkey[:]), CombinedEmissionRao: fmt.Sprint(uint64(combined[item.row.UID])),
			ServerEmissionRao: fmt.Sprint(server[item.row.UID]), StakeBeforeRao: fmt.Sprint(uint64(before)), StakeAfterRao: fmt.Sprint(uint64(after)),
			IncentiveU16: uint16(incentives[item.row.UID]), DividendsU16: uint16(dividends[item.row.UID]),
		})
	}
	return finalNativeEpochPayoutRead{State: state, ParentExchanges: parentExchanges, BlockExchanges: blockExchanges}, nil
}

// Performs the public replay, retains the exact decoded observations, and
// appends parent/reveal exchanges only after the state passes.
func verifyFinalSemanticNativeEpochPayouts(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) ([]FinalNativeEpochPayoutState, error) {
	if evidence == nil || len(evidence.NativeRewards) == 0 {
		return nil, errors.New("native payout evidence is unavailable")
	}
	payoutReader, ok := reader.(finalSemanticNativeEpochPayoutChainReader)
	if !ok {
		return nil, errors.New("public semantic reader does not expose exact native epoch payouts")
	}
	seenParents := make(map[ChainHead]bool)
	states := make([]FinalNativeEpochPayoutState, 0, evidence.Window.EpochCount)
	for epoch := evidence.Window.FirstEpoch; epoch < evidence.Window.FirstEpoch+evidence.Window.EpochCount; epoch++ {
		reveal, _, err := finalNativeEpochReveal(evidence, epoch)
		if err != nil {
			return nil, err
		}
		read, err := payoutReader.NativeEpochPayout(ctx, epoch, reveal)
		if err != nil {
			return nil, fmt.Errorf("public native payout epoch %d: %w", epoch, err)
		}
		if err := verifyFinalNativeEpochPayout(evidence, read.State); err != nil {
			return nil, err
		}
		if !seenParents[read.State.Parent] {
			canonical, err := reader.CanonicalSubstrateHead(ctx, read.State.Parent)
			if err != nil {
				return nil, fmt.Errorf("public native payout parent epoch %d: %w", epoch, err)
			}
			if err := appendExchanges("substrate", read.State.Parent, canonical); err != nil {
				return nil, err
			}
			seenParents[read.State.Parent] = true
		}
		if err := appendExchanges("substrate", read.State.Parent, read.ParentExchanges); err != nil {
			return nil, err
		}
		if err := appendExchanges("substrate", read.State.Block, read.BlockExchanges); err != nil {
			return nil, err
		}
		states = append(states, read.State)
	}
	return states, nil
}
