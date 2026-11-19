// Recipient amounts come from original storage mutations at reviewed causes.
// Separate collateral and liquid calls remain distinct original observations.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const nativeRecipientStorageLayoutSchema = "original-storage-credit-pairs-v1"

type nativeRecipientComponentProvenance struct {
	Purpose      string `json:"purpose"`
	ReadOrdinal  uint64 `json:"read_ordinal"`
	WriteOrdinal uint64 `json:"write_ordinal"`
}

type nativeRecipientInputProvenance struct {
	Schema             string                               `json:"schema"`
	Hotkey             string                               `json:"hotkey"`
	Components         []nativeRecipientComponentProvenance `json:"components"`
	OwnerHotkeyOrdinal *uint64                              `json:"owner_hotkey_ordinal,omitempty"`
	AutoStakeOrdinal   *uint64                              `json:"auto_stake_ordinal,omitempty"`
	OwnerOrdinal       *uint64                              `json:"owner_ordinal,omitempty"`
	ZeroEntitlement    bool                                 `json:"zero_entitlement,omitempty"`
}

type nativeRecipientInput struct {
	record              historicalReplayObservation
	hotkey              string
	coldkey             string
	branch              string
	gross               uint64
	captured            uint64
	liquid              uint64
	recycled            uint64
	destination         string
	ownerHotkey         *string
	ownerHotkeyObserved bool
	autoStake           *string
	autoStakeObserved   bool
	provenance          *nativeRecipientInputProvenance
}

type nativeRecipientStorageLayout struct {
	profile         *historicalReplayObservationProfile
	metadata        *types.Metadata
	netuid          uint16
	poolFallback    []byte
	recycleFallback []byte
}

func nativeRecipientStoragePurpose(purpose string) bool {
	return purpose == "native-miner-capture" || purpose == "native-miner-credit" || purpose == "native-owner-recycle" || purpose == "native-recipient-owner-hotkey" || purpose == "native-recipient-auto-stake" || purpose == "native-recipient-owner"
}

func newNativeRecipientStorageLayout(profile *historicalReplayObservationProfile, metadata *types.Metadata, netuid uint16) (*nativeRecipientStorageLayout, error) {
	if profile.RecipientLayout == nil {
		return nil, nil
	}
	if *profile.RecipientLayout != nativeRecipientStorageLayoutSchema || profile.Schema != historicalNativeProfileSchema {
		return nil, errors.New("native recipient layout is not an explicit original storage-pair join")
	}
	if err := profile.validateRecipientLayout(); err != nil {
		return nil, err
	}
	entries, err := observationStorageProfile(metadata, []rootStorageSpec{
		{name: "TotalHotkeyAlpha", keys: []string{"account", "u16"}, hashers: []string{"blake128concat", "identity"}, value: "u64"},
		{name: "SubnetAlphaOut", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
		{name: "Owner", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "account"},
		{name: "SubnetOwnerHotkey", keys: []string{"u16"}, hashers: []string{"identity"}, value: "account"},
		{name: "AutoStakeDestination", keys: []string{"account", "u16"}, hashers: []string{"blake128concat", "identity"}, value: "account", optional: true},
	})
	if err != nil {
		return nil, err
	}
	pool, recycle := entries["TotalHotkeyAlpha"].Fallback, entries["SubnetAlphaOut"].Fallback
	if !bytes.Equal(pool, make([]byte, 8)) || !bytes.Equal(recycle, make([]byte, 8)) {
		return nil, errors.New("native recipient metadata defaults are not original zero u64 values")
	}
	return &nativeRecipientStorageLayout{profile: profile, metadata: metadata, netuid: netuid, poolFallback: append([]byte(nil), pool...), recycleFallback: append([]byte(nil), recycle...)}, nil
}

func (self *nativeRecipientStorageLayout) key(name string, account string) (string, error) {
	args := [][]byte{}
	if account != "" {
		raw, err := historicalReplayHex(account, 32)
		if err != nil || len(raw) != 32 {
			return "", errors.New("native recipient original account width differs")
		}
		args = append(args, raw)
	}
	if name != "Owner" {
		args = append(args, binary.LittleEndian.AppendUint16(nil, self.netuid))
	}
	key, err := types.CreateStorageKey(self.metadata, "SubtensorModule", name, args...)
	return key.Hex(), err
}

// Context reads may not spill a netuid. Their exact original key, authenticated
// by the same metadata, supplies it without manufacturing a memory field.
func (self *nativeRecipientStorageLayout) netuidFor(record historicalReplayObservation) (uint64, error) {
	if record.Purpose == "native-recipient-owner" {
		if _, err := self.cause(record); err != nil {
			return 0, err
		}
		if _, err := nativeRecipientOption(record); err != nil {
			return 0, err
		}
		hotkey, err := nativeRecipientAccount(record, "hotkey")
		if err != nil {
			return 0, err
		}
		key, err := self.key("Owner", hotkey)
		if err != nil || key != record.KeyHex {
			return 0, errors.New("native ordinary Owner read changed its namespace or hotkey")
		}
		return nativeCaptureUint(record, "netuid", 2)
	}
	if record.Purpose != "native-recipient-auto-stake" && record.Purpose != "native-recipient-owner-hotkey" {
		return nativeCaptureUint(record, "netuid", 2)
	}
	if _, err := self.cause(record); err != nil {
		return 0, err
	}
	if _, err := nativeRecipientOption(record); err != nil {
		return 0, err
	}
	raw, err := historicalReplayHex(record.KeyHex, 82)
	width := 34
	if record.Purpose == "native-recipient-auto-stake" {
		width = 82
	}
	if err != nil || len(raw) != width {
		return 0, errors.New("native recipient context key width differs")
	}
	name := "SubnetOwnerHotkey"
	args := [][]byte{raw[len(raw)-2:]}
	if record.Purpose == "native-recipient-auto-stake" {
		name = "AutoStakeDestination"
		coldkey, err := nativeCapture(record, "coldkey", 32)
		if err != nil {
			return 0, err
		}
		args = [][]byte{coldkey, raw[len(raw)-2:]}
	}
	expected, err := types.CreateStorageKey(self.metadata, "SubtensorModule", name, args...)
	if err != nil || !bytes.Equal(expected, raw) {
		return 0, errors.New("native recipient context changed its metadata namespace or account key")
	}
	return uint64(binary.LittleEndian.Uint16(raw[len(raw)-2:])), nil
}

func (self *nativeRecipientStorageLayout) cause(record historicalReplayObservation) (*historicalReplayHookRule, error) {
	if record.Native == nil || record.Native.ExecutionPhaseHex == nil || *record.Native.ExecutionPhaseHex != "0x02" {
		return nil, errors.New("native recipient storage cause is not original Initialization")
	}
	var selected *historicalReplayHookRule
	for index := range self.profile.Rules {
		rule := &self.profile.Rules[index]
		if rule.Purpose != record.Purpose || rule.StorageCall == nil || rule.StorageCall.Operation != record.Operation {
			continue
		}
		for frame := range record.Stack {
			if historicalRuleMatchesAt(*rule, record.Stack, frame) {
				if selected != nil {
					return nil, errors.New("native recipient storage cause is ambiguous")
				}
				selected = rule
				break
			}
		}
	}
	if selected == nil {
		return nil, errors.New("native recipient storage operation lacks its exact original call path")
	}
	if selected.RecipientOwner != (record.Purpose == "native-owner-recycle") {
		return nil, errors.New("native recipient original Owner read scope differs")
	}
	for _, field := range record.Native.Memory {
		if field.Name == "captured" || field.Name == "subnet-owner-hotkey" || field.Name == "auto-stake-destination" || record.Purpose == "native-owner-recycle" && field.Name == "coldkey" {
			return nil, errors.New("native storage-pair layout cannot invent recipient memory")
		}
	}
	return selected, nil
}

func nativeRecipientAccount(record historicalReplayObservation, name string) (string, error) {
	raw, err := nativeCapture(record, name, 32)
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(raw), nil
}

func nativeRecipientOption(record historicalReplayObservation) (*string, error) {
	value := record.StorageReturn
	if record.Operation != "get" || value == nil || value.Present != (value.ValueHex != nil) || value.Offset != nil || value.OutputLength != nil || record.ValueHex != nil {
		return nil, errors.New("native recipient option omitted its original returned value")
	}
	if !value.Present {
		return nil, nil
	}
	raw, err := historicalReplayHex(*value.ValueHex, 32)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("native recipient option is not an original AccountId32")
	}
	text := "0x" + hex.EncodeToString(raw)
	return &text, nil
}

// Recycle skips the runtime's ordinary Owner get. Its independently selected
// observer read uses the same live execution overlay, never a later snapshot.
func (self *nativeRecipientStorageLayout) recycleOwner(record historicalReplayObservation, hotkey string) (string, error) {
	key, err := self.key("Owner", hotkey)
	if err != nil {
		return "", err
	}
	if record.Native == nil || record.Native.ExecutionState == nil || len(*record.Native.ExecutionState) != 1 {
		return "", errors.New("native recycle omitted its original Owner overlay read")
	}
	value := (*record.Native.ExecutionState)[0]
	if value.KeyHex != key || value.ValueHex == nil {
		return "", errors.New("native recycle Owner key or presence differs")
	}
	raw, err := historicalReplayHex(*value.ValueHex, 32)
	if err != nil || len(raw) != 32 {
		return "", errors.New("native recycle Owner value is not an AccountId32")
	}
	return "0x" + hex.EncodeToString(raw), nil
}

func (self *nativeRecipientStorageLayout) pair(before, after historicalReplayObservation) (nativeRecipientInput, error) {
	result := nativeRecipientInput{record: after, branch: "native-miner-credit"}
	readRule, err := self.cause(before)
	if err != nil {
		return result, err
	}
	writeRule, err := self.cause(after)
	if err != nil {
		return result, err
	}
	if before.Operation != "get" || after.Operation != "set" || before.Purpose != after.Purpose || before.Ordinal == 0 || after.Ordinal <= before.Ordinal || before.KeyHex != after.KeyHex || readRule.FunctionIndex != writeRule.FunctionIndex || readRule.FunctionBodySha256 != writeRule.FunctionBodySha256 || readRule.OffsetStart != writeRule.OffsetStart || readRule.OffsetEnd != writeRule.OffsetEnd {
		return result, errors.New("native recipient get/set pair changed its original cause, key or order")
	}
	for _, record := range []historicalReplayObservation{before, after} {
		netuid, err := nativeCaptureUint(record, "netuid", 2)
		if err != nil || netuid != uint64(self.netuid) {
			return result, errors.New("native recipient pair changed original subnet")
		}
	}
	result.hotkey, err = nativeRecipientAccount(after, "hotkey")
	if err != nil {
		return result, err
	}
	hotkey, err := nativeRecipientAccount(before, "hotkey")
	if err != nil || hotkey != result.hotkey {
		return result, errors.New("native recipient pair changed original hotkey")
	}
	result.gross, err = nativeCaptureUint(after, "gross", 8)
	if err != nil {
		return result, err
	}
	gross, err := nativeCaptureUint(before, "gross", 8)
	if err != nil || gross != result.gross {
		return result, errors.New("native recipient pair changed original gross")
	}
	var key string
	fallback := self.poolFallback
	if after.Purpose == "native-owner-recycle" {
		result.branch = "native-owner-recycle"
		result.coldkey, err = self.recycleOwner(after, result.hotkey)
		if err != nil {
			return result, err
		}
		owner, err := self.recycleOwner(before, result.hotkey)
		if err != nil || owner != result.coldkey {
			return result, errors.New("native recycle changed its original Owner custody")
		}
		key, err = self.key("SubnetAlphaOut", "")
		fallback = self.recycleFallback
	} else {
		result.coldkey, err = nativeRecipientAccount(after, "coldkey")
		if err != nil {
			return result, err
		}
		coldkey, err := nativeRecipientAccount(before, "coldkey")
		if err != nil || coldkey != result.coldkey {
			return result, errors.New("native recipient pair changed original coldkey")
		}
		result.destination, err = nativeRecipientAccount(after, "stake-destination")
		if err != nil {
			return result, err
		}
		destination, err := nativeRecipientAccount(before, "stake-destination")
		if err != nil || destination != result.destination {
			return result, errors.New("native recipient pair changed original stake destination")
		}
		if after.Purpose == "native-miner-capture" && result.destination != result.hotkey {
			return result, errors.New("native collateral was routed away from its original hotkey")
		}
		key, err = self.key("TotalHotkeyAlpha", result.destination)
	}
	if err != nil || before.KeyHex != key {
		return result, errors.New("native recipient storage key differs from its original identity")
	}
	returned := before.StorageReturn
	if returned == nil || returned.Present != (returned.ValueHex != nil) || returned.Offset != nil || returned.OutputLength != nil || before.ValueHex != nil || after.StorageReturn != nil || after.ValueHex == nil {
		return result, errors.New("native recipient pair omitted exact get/set bytes")
	}
	old := fallback
	if returned.Present {
		old, err = historicalReplayHex(*returned.ValueHex, 8)
	}
	if err != nil || len(old) != 8 {
		return result, errors.New("native recipient original pool read is not u64")
	}
	next, err := historicalReplayHex(*after.ValueHex, 8)
	if err != nil || len(next) != 8 {
		return result, errors.New("native recipient original pool write is not u64")
	}
	from, to := binary.LittleEndian.Uint64(old), binary.LittleEndian.Uint64(next)
	if result.branch == "native-owner-recycle" {
		if from < to || from-to != result.gross {
			return result, errors.New("native original recycle delta does not equal gross")
		}
		result.recycled = from - to
	} else {
		pool, err := nativeCaptureUint(after, "pool-after", 8)
		if err != nil || pool != to || to <= from || to-from > result.gross {
			return result, errors.New("native original credit delta contradicts its pool or gross")
		}
		if after.Purpose == "native-miner-capture" {
			result.captured = to - from
		} else {
			result.liquid = to - from
			for _, record := range []historicalReplayObservation{before, after} {
				liquid, err := nativeCaptureUint(record, "liquid", 8)
				if err != nil || liquid != result.liquid {
					return result, errors.New("native liquid credit delta differs from its original amount")
				}
			}
		}
	}
	result.provenance = &nativeRecipientInputProvenance{Schema: nativeRecipientStorageLayoutSchema, Hotkey: result.hotkey, Components: []nativeRecipientComponentProvenance{{Purpose: after.Purpose, ReadOrdinal: before.Ordinal, WriteOrdinal: after.Ordinal}}}
	return result, nil
}

func (self *nativeRecipientStorageLayout) decode(records []historicalReplayObservation) ([]nativeRecipientInput, error) {
	if len(records) > 6*rootCensusLimit+1 {
		return nil, errors.New("native recipient component census exceeds its finite bound")
	}
	result := []nativeRecipientInput{}
	seen := map[string]bool{}
	var ownerRecord, autoRecord *historicalReplayObservation
	var ownerOption, autoOption *string
	var pending *nativeRecipientInput
	var ordinary *nativeRecipientInput
	var previous uint64
	for index := 0; index < len(records); {
		record := records[index]
		if record.Ordinal <= previous {
			return nil, errors.New("native recipient original observations are out of order")
		}
		previous = record.Ordinal
		if record.Purpose == "native-recipient-owner" {
			if _, err := self.cause(record); err != nil {
				return nil, err
			}
			netuid, err := self.netuidFor(record)
			if err != nil || netuid != uint64(self.netuid) {
				return nil, errors.New("native ordinary Owner read changed its subnet")
			}
			coldkey, err := nativeRecipientOption(record)
			if err != nil || coldkey == nil {
				return nil, errors.New("native ordinary recipient omitted its original Owner return")
			}
			hotkey, err := nativeRecipientAccount(record, "hotkey")
			if err != nil {
				return nil, err
			}
			key, err := self.key("Owner", hotkey)
			if err != nil || key != record.KeyHex || pending != nil || autoRecord != nil || ordinary != nil || seen[hotkey] {
				return nil, errors.New("native ordinary Owner read crossed another recipient or changed key")
			}
			gross, err := nativeCaptureUint(record, "gross", 8)
			if err != nil {
				return nil, err
			}
			ordinal := record.Ordinal
			input := nativeRecipientInput{record: record, hotkey: hotkey, coldkey: *coldkey, branch: "native-miner-credit", gross: gross, provenance: &nativeRecipientInputProvenance{Schema: nativeRecipientStorageLayoutSchema, Hotkey: hotkey, Components: []nativeRecipientComponentProvenance{}, OwnerOrdinal: &ordinal, ZeroEntitlement: gross == 0}}
			if gross != 0 {
				ordinary = &input
				index++
				continue
			}
			if ownerRecord != nil {
				input.ownerHotkey, input.ownerHotkeyObserved = ownerOption, true
				ownerOrdinal := ownerRecord.Ordinal
				input.provenance.OwnerHotkeyOrdinal = &ownerOrdinal
			}
			if len(result) >= rootCensusLimit {
				return nil, errors.New("native recipient original census exceeds its UID bound")
			}
			result = append(result, input)
			seen[hotkey] = true
			index++
			continue
		}
		if record.Purpose == "native-recipient-owner-hotkey" || record.Purpose == "native-recipient-auto-stake" {
			if _, err := self.cause(record); err != nil {
				return nil, err
			}
			value, err := nativeRecipientOption(record)
			if err != nil {
				return nil, err
			}
			if record.Purpose == "native-recipient-owner-hotkey" {
				key, err := self.key("SubnetOwnerHotkey", "")
				if err != nil || record.KeyHex != key || ownerRecord != nil || pending != nil || autoRecord != nil || ordinary != nil || len(result) != 0 {
					return nil, errors.New("native owner-hotkey context is duplicated, late or miskeyed")
				}
				ownerRecord, ownerOption = &record, value
			} else {
				coldkey, err := nativeRecipientAccount(record, "coldkey")
				if err != nil {
					return nil, err
				}
				key, err := self.key("AutoStakeDestination", coldkey)
				if err != nil || record.KeyHex != key || autoRecord != nil {
					return nil, errors.New("native auto-stake context is duplicated or miskeyed")
				}
				autoRecord, autoOption = &record, value
			}
			index++
			continue
		}
		if index+1 >= len(records) {
			return nil, errors.New("native recipient original get has no matching set")
		}
		input, err := self.pair(record, records[index+1])
		if err != nil {
			return nil, err
		}
		previous = records[index+1].Ordinal
		index += 2
		if seen[input.hotkey] {
			return nil, errors.New("native recipient original effect is repeated")
		}
		if input.branch == "native-miner-credit" {
			if ordinary == nil || ordinary.hotkey != input.hotkey || ordinary.coldkey != input.coldkey || ordinary.gross != input.gross {
				return nil, errors.New("native credit differs from its original ordinary Owner cause")
			}
			input.provenance.OwnerOrdinal = ordinary.provenance.OwnerOrdinal
		} else if ordinary != nil {
			return nil, errors.New("native recycle crossed an ordinary recipient Owner read")
		}
		if record.Purpose == "native-miner-capture" {
			if pending != nil || autoRecord != nil {
				return nil, errors.New("native collateral component crossed another recipient cause")
			}
			if input.captured < input.gross {
				pending = &input
				continue
			}
		} else if record.Purpose == "native-miner-credit" {
			if autoRecord != nil {
				hotkey, e1 := nativeRecipientAccount(*autoRecord, "hotkey")
				coldkey, e2 := nativeRecipientAccount(*autoRecord, "coldkey")
				gross, e3 := nativeCaptureUint(*autoRecord, "gross", 8)
				liquid, e4 := nativeCaptureUint(*autoRecord, "liquid", 8)
				if errors.Join(e1, e2, e3, e4) != nil || hotkey != input.hotkey || coldkey != input.coldkey || gross != input.gross || liquid != input.liquid {
					return nil, errors.New("native auto-stake read changed original recipient cause")
				}
				input.autoStake, input.autoStakeObserved = autoOption, true
				ordinal := autoRecord.Ordinal
				input.provenance.AutoStakeOrdinal = &ordinal
				autoRecord, autoOption = nil, nil
			}
			if pending != nil {
				if pending.hotkey != input.hotkey || pending.coldkey != input.coldkey || pending.gross != input.gross || pending.captured > input.gross || input.liquid != input.gross-pending.captured {
					return nil, errors.New("native split recipient components changed identity or gross")
				}
				input.captured = pending.captured
				input.provenance.Components = append(pending.provenance.Components, input.provenance.Components...)
				pending = nil
			} else if input.liquid != input.gross {
				return nil, errors.New("native partial liquid credit omitted original collateral")
			}
		} else if record.Purpose != "native-owner-recycle" {
			return nil, errors.New("native recipient storage pair has an unrelated purpose")
		} else if pending != nil || autoRecord != nil {
			return nil, errors.New("native recycle crossed an unfinished original credit")
		}
		if ownerRecord != nil {
			input.ownerHotkey, input.ownerHotkeyObserved = ownerOption, true
			ordinal := ownerRecord.Ordinal
			input.provenance.OwnerHotkeyOrdinal = &ordinal
		}
		seen[input.hotkey] = true
		ordinary = nil
		if len(result) >= rootCensusLimit {
			return nil, errors.New("native recipient original census exceeds its UID bound")
		}
		result = append(result, input)
	}
	if pending != nil || autoRecord != nil || ordinary != nil {
		return nil, errors.New("native recipient original component or context is unfinished")
	}
	return result, nil
}

func decodeNativeLegacyRecipient(record historicalReplayObservation) (nativeRecipientInput, error) {
	result := nativeRecipientInput{record: record, branch: record.Purpose}
	var err error
	result.hotkey, err = nativeRecipientAccount(record, "hotkey")
	if err != nil {
		return result, err
	}
	result.coldkey, err = nativeRecipientAccount(record, "coldkey")
	if err != nil {
		return result, err
	}
	result.gross, err = nativeCaptureUint(record, "gross", 8)
	if err != nil {
		return result, err
	}
	if record.Purpose == "native-owner-recycle" {
		result.recycled, err = nativeCaptureUint(record, "recycled", 8)
	} else {
		result.captured, err = nativeCaptureUint(record, "captured", 8)
		if err == nil {
			result.liquid, err = nativeCaptureUint(record, "liquid", 8)
		}
	}
	return result, err
}

func deriveNativeStorageTreasuryRecipient(authority *nativeTreasuryAuthority, input nativeRecipientInput, effect nativeExecutionEffect) (*nativeTreasuryRecipientEffect, error) {
	if input.provenance == nil {
		return deriveNativeTreasuryRecipient(authority, input.record, effect)
	}
	if authority == nil || !nativeTreasuryRecipient(authority.Policy, effect.Recipient) {
		return nil, nil
	}
	if effect.Branch != "native-miner-credit" || effect.Provider || effect.Recipient.Coldkey != fmt.Sprintf("0x%x", authority.Policy.MultisigAccount) || !input.ownerHotkeyObserved {
		return nil, errors.New("native treasury storage credit omitted its ordinary owner context")
	}
	owner, err := nativeRecipientAccount(input.record, "subnet-owner")
	if err != nil {
		return nil, err
	}
	owners, err := nativeCapture(input.record, "owner-hotkeys", -1)
	if err != nil || len(owners)%32 != 0 || len(owners) > rootCensusLimit*32 {
		return nil, errors.New("native treasury original owner census is incomplete")
	}
	result := &nativeTreasuryRecipientEffect{SubnetOwner: owner, SubnetOwnerHotkey: input.ownerHotkey, OwnerHotkeys: []string{}}
	for offset := 0; offset < len(owners); offset += 32 {
		result.OwnerHotkeys = append(result.OwnerHotkeys, "0x"+hex.EncodeToString(owners[offset:offset+32]))
	}
	if input.liquid != 0 {
		if !input.autoStakeObserved {
			return nil, errors.New("native treasury liquid credit omitted original auto-stake context")
		}
		result.AutoStakeDestination = input.autoStake
		destination := input.destination
		result.StakeDestination = &destination
	}
	return result, result.validate(authority.Policy, effect)
}

// Enrollment admits the complete reader, including branches absent in any one
// epoch. Original return/write observations still prove each actual outcome.
func validateNativeStorageTreasuryProfile(profile *historicalReplayObservationProfile) error {
	epoch, counter := false, false
	seen := map[string]bool{}
	for _, rule := range profile.Rules {
		var required []string
		switch rule.Purpose {
		case "native-epoch":
			epoch = true
			if profile.EpochLayout == nil {
				required = []string{"subnet-epoch"}
			}
		case "native-epoch-index":
			counter = true
		case "native-miner-capture", "native-miner-credit", "native-owner-recycle", "native-recipient-owner-hotkey", "native-recipient-auto-stake", "native-recipient-owner":
			if rule.StorageCall == nil {
				return errors.New("native treasury component has no original storage call")
			}
			seen[rule.Purpose+":"+rule.StorageCall.Operation] = true
			if rule.Purpose == "native-miner-capture" || rule.Purpose == "native-miner-credit" {
				required = []string{"subnet-owner", "owner-hotkeys", "stake-destination"}
			} else if rule.Purpose == "native-recipient-owner" {
				required = []string{"subnet-owner", "owner-hotkeys"}
			}
		}
		for _, name := range required {
			found := false
			for _, capture := range rule.Memory {
				found = found || capture.Name == name
			}
			if !found {
				return errors.New("native treasury reader omitted original custody: " + name)
			}
		}
	}
	if !epoch || profile.EpochLayout != nil && !counter {
		return errors.New("native treasury storage reader omitted its original epoch counter")
	}
	for _, purpose := range []string{"native-miner-capture", "native-miner-credit", "native-owner-recycle"} {
		if !seen[purpose+":get"] || !seen[purpose+":set"] {
			return errors.New("native treasury storage reader omitted complete original component paths")
		}
	}
	for _, purpose := range []string{"native-recipient-owner-hotkey", "native-recipient-auto-stake", "native-recipient-owner"} {
		if !seen[purpose+":get"] {
			return errors.New("native treasury storage reader omitted original custody context")
		}
	}
	return nil
}
