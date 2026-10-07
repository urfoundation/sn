// Root registration observations retain one finalized census and the operator's
// raw account state. Eligibility and burn are snapshot checks, never execution
// predicates or an on-chain spending ceiling.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

const rootRegisterObservationSchema = "urnetwork-mainnet-root-register-observation-v1"
const rootRegisterReadbackLimit = 16 * 1024 * 1024

// The two enumerations retain complete forward and reverse root census claims.
// Their checksum binds the RPC observation; it is not a storage/finality proof.
type rootRegisterObservation struct {
	Schema          string             `json:"schema"`
	PolicyHash      string             `json:"policy_hash"`
	FinalizedNumber uint64             `json:"finalized_number"`
	FinalizedHash   string             `json:"finalized_hash"`
	Storage         []rootStorageValue `json:"storage"`
	Keys            []string           `json:"root_keys"`
	UidKeys         []string           `json:"root_uid_keys"`
	ContentHash     string             `json:"content_hash"`
}

// A full root network chooses stake, then older registration, then lower UID.
type rootRegisterCandidate struct {
	Uid               uint16 `json:"uid"`
	Hotkey            string `json:"hotkey_account_id"`
	RegistrationBlock uint64 `json:"registration_block"`
	StakeRao          uint64 `json:"root_stake_rao"`
}

// Only raw metadata-authenticated rows supply these facts. The conservative
// balance quote keeps both frozen funds and the existential deposit; later fees,
// incoming funds and state changes can alter the execution-time exposure.
type rootRegisterEligibility struct {
	Nonce                    uint32
	FreeRao                  uint64
	FrozenRao                uint64
	ConservativeReducibleRao uint64
	BurnRao                  uint64
	AccountPresent           bool
	OwnerPresent             bool
	ExistingOwner            string
	ExistingSeat             *rootSeatExpectation
	MaximumSeats             uint16
	SeatCount                uint16
	ApplicantStakeRao        uint64
	RegistrationsThisBlock   uint16
	MaxRegistrationsPerBlock uint16
	RegistrationsInInterval  uint16
	TargetInInterval         uint16
	Candidate                *rootRegisterCandidate
	DelegatePresent          bool
	DelegateTake             uint16
	AutoParentEnabled        bool
	OwnedHotkeys             []string
	StakingHotkeys           []string
	Eligible                 bool
	Reason                   string
}

// This adapter admits only the reviewed direct coldkey root_register call.
// The historical hotkey root-weights action supplies no registration authority.
func rootRegisterCall(metadata *types.Metadata) ([2]byte, error) {
	var result [2]byte
	if metadata == nil || metadata.Version != 14 {
		return result, errors.New("root registration requires metadata14")
	}
	pallets, calls := 0, 0
	seenPallets := map[byte]bool{}
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if seenPallets[byte(pallet.Index)] {
			return result, errors.New("root registration metadata repeats a pallet index")
		}
		seenPallets[byte(pallet.Index)] = true
		if pallet.Name != "SubtensorModule" {
			continue
		}
		pallets++
		entry := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
		if pallet.Index != 7 || !pallet.HasCalls || entry == nil || !entry.Def.IsVariant {
			return result, errors.New("root registration call pallet changed")
		}
		seenCalls := map[byte]bool{}
		for _, variant := range entry.Def.Variant.Variants {
			if seenCalls[byte(variant.Index)] {
				return result, errors.New("root registration metadata repeats a call index")
			}
			seenCalls[byte(variant.Index)] = true
			if variant.Name != "root_register" {
				continue
			}
			calls++
			if variant.Index != 62 || len(variant.Fields) != 1 || !variant.Fields[0].HasName || variant.Fields[0].Name != "hotkey" || !rootTypeMatches(metadata, variant.Fields[0].Type, "account", 0) {
				return result, errors.New("root registration hotkey call encoding changed")
			}
			result = [2]byte{7, 62}
		}
	}
	if pallets != 1 || calls != 1 {
		return result, errors.New("root registration call missing or duplicated")
	}
	return result, nil
}

// The profile includes pre-burn ownership mutations and post-registration
// delegation settings, without interpreting an absent Owner as its default key.
func rootRegisterStorageSpecs() []rootStorageSpec {
	return []rootStorageSpec{
		{name: "NetworksAdded", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bool"},
		{name: "SubnetOwner", keys: []string{"u16"}, hashers: []string{"identity"}, value: "account"},
		{name: "SubnetworkN", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
		{name: "MaxAllowedUids", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
		{name: "ImmunityPeriod", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
		{name: "Burn", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
		{name: "RegistrationsThisBlock", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
		{name: "MaxRegistrationsPerBlock", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
		{name: "RegistrationsThisInterval", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
		{name: "TargetRegistrationsPerInterval", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
		{name: "Keys", keys: []string{"u16", "u16"}, hashers: []string{"identity", "identity"}, value: "account"},
		{name: "Uids", keys: []string{"u16", "account"}, hashers: []string{"identity", "blake128concat"}, value: "u16", optional: true},
		{name: "Owner", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "account"},
		{name: "BlockAtRegistration", keys: []string{"u16", "u16"}, hashers: []string{"identity", "identity"}, value: "u64"},
		{name: "TotalHotkeyAlpha", keys: []string{"account", "u16"}, hashers: []string{"blake128concat", "identity"}, value: "u64"},
		{name: "Delegates", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "u16"},
		{name: "AutoParentDelegationEnabled", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "bool"},
		{name: "OwnedHotkeys", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "accounts"},
		{name: "StakingHotkeys", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "accounts"},
	}
}

// Source473 uses a u64 existential deposit of 500 rao. Authenticate its constant
// before applying a conservative quote; no frozen/reserved reduction is guessed.
func rootRegisterExistentialDeposit(metadata *types.Metadata) (uint64, error) {
	if metadata == nil || metadata.Version != 14 {
		return 0, errors.New("root registration balance constants unavailable")
	}
	pallets, constants := 0, 0
	var deposit uint64
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "Balances" {
			continue
		}
		pallets++
		for _, constant := range pallet.Constants {
			if constant.Name != "ExistentialDeposit" {
				continue
			}
			constants++
			if !rootTypeMatches(metadata, constant.Type, "u64", 0) || len(constant.Value) != 8 {
				return 0, errors.New("root registration existential deposit encoding changed")
			}
			deposit = binary.LittleEndian.Uint64(constant.Value)
		}
	}
	if pallets != 1 || constants != 1 || deposit != 500 {
		return 0, errors.New("root registration existential deposit differs from source473")
	}
	return deposit, nil
}

// Public observation bytes are re-decoded on every approval/import/reopen. A
// recomputed JSON checksum cannot turn a foreign owner or incomplete census into
// an eligible registration, and an existing seat remains an observation only.
func (self rootRegisterObservation) eligibility(policy rootRegisterPolicy, metadata *types.Metadata) (rootRegisterEligibility, error) {
	var result rootRegisterEligibility
	encoded, err := json.Marshal(self)
	if err != nil || len(encoded) > rootRegisterReadbackLimit {
		return result, errors.New("root registration observation exceeds its retained byte bound")
	}
	if err := policy.validate(); err != nil {
		return result, err
	}
	claimed := self.ContentHash
	self.ContentHash = ""
	if policy.RuntimeSourceCommit != crv4.NativeOwnerSource473 || self.Schema != rootRegisterObservationSchema || self.PolicyHash != rootObjectHash(policy) || claimed != rootObjectHash(self) || self.FinalizedNumber == 0 || self.FinalizedNumber > uint64(^uint32(0)) || !rootCanonicalHash(self.FinalizedHash) {
		return result, errors.New("root registration observation source, domain or seal changed")
	}
	if _, err := rootRegisterCall(metadata); err != nil {
		return result, err
	}
	specs := rootRegisterStorageSpecs()
	entries, err := observationStorageProfile(metadata, specs)
	if err != nil {
		return result, err
	}
	if err := rootAccountProfile(metadata); err != nil {
		return result, err
	}
	deposit, err := rootRegisterExistentialDeposit(metadata)
	if err != nil {
		return result, err
	}
	accountEntry, err := rootSystemEntry(metadata, "Account")
	if err != nil || len(accountEntry.Fallback) != 56 {
		return result, errors.New("root registration account default changed")
	}
	operator, _ := hex.DecodeString(policy.Operator[2:])
	hotkey, _ := hex.DecodeString(policy.Hotkey[2:])
	accountKey, err := types.CreateStorageKey(metadata, "System", "Account", operator)
	if err != nil {
		return result, err
	}
	if len(self.Storage) > 20+5*rootCensusLimit || len(self.Keys) > rootCensusLimit || len(self.UidKeys) > rootCensusLimit {
		return result, errors.New("root registration storage census exceeds bound")
	}
	shapeKVs := map[string]string{}
	for _, spec := range specs {
		shapeKVs[spec.name] = spec.value
	}
	rowKVs := map[string]rootStorageValue{}
	for _, row := range self.Storage {
		if _, duplicate := rowKVs[row.Key]; duplicate {
			return result, errors.New("root registration repeats a storage key")
		}
		entry, known := entries[row.Name]
		shape := shapeKVs[row.Name]
		if row.Name == "System.Account" {
			entry, known, shape = accountEntry, row.Key == accountKey.Hex(), "native-account"
		}
		if !known || shape == "" {
			return result, errors.New("root registration observation contains unreviewed storage")
		}
		data, source := []byte(nil), "absent-optional"
		if row.RawStorage != nil {
			data, err = rootReceiptHex(*row.RawStorage, 4+rootCensusLimit*32)
			source = "finalized-storage"
		} else if entry.Modifier.IsDefault {
			data, source = append([]byte(nil), entry.Fallback...), "authenticated-metadata-fallback"
		}
		if err != nil || row.ValueSource != source {
			return result, errors.New("root registration raw storage provenance differs")
		}
		effective := ""
		if row.RawStorage != nil || entry.Modifier.IsDefault {
			if shape == "native-account" {
				if len(data) != 56 {
					return result, errors.New("root registration account is not the reviewed 56-byte row")
				}
			} else if err := rootValidateScale(data, shape); err != nil {
				return result, err
			}
			effective = "0x" + hex.EncodeToString(data)
		}
		if row.EffectiveScale != effective {
			return result, errors.New("root registration effective storage differs from raw bytes")
		}
		row.data = data
		rowKVs[row.Key] = row
	}
	used := map[string]bool{}
	read := func(name string, args ...[]byte) (rootStorageValue, error) {
		pallet, item := "SubtensorModule", name
		if name == "System.Account" {
			pallet, item = "System", "Account"
		}
		key, err := types.CreateStorageKey(metadata, pallet, item, args...)
		if err != nil {
			return rootStorageValue{}, err
		}
		row, found := rowKVs[key.Hex()]
		if !found || row.Name != name {
			return rootStorageValue{}, fmt.Errorf("root registration observation lacks exact %s key", name)
		}
		used[key.Hex()] = true
		return row, nil
	}
	root := []byte{0, 0}
	subnetOwner, err := read("SubnetOwner", []byte{25, 0})
	if err != nil || subnetOwner.RawStorage == nil || subnetOwner.EffectiveScale != policy.SubnetOwner {
		return result, errors.Join(errors.New("root registration independently pinned subnet owner differs from current storage"), err)
	}
	subnetExists, err := read("NetworksAdded", []byte{25, 0})
	if err != nil || subnetExists.data[0] != 1 {
		return result, errors.Join(errors.New("root registration subnet25 owner exclusion lacks an existing subnet"), err)
	}
	base := map[string]rootStorageValue{}
	for _, name := range []string{"NetworksAdded", "SubnetworkN", "MaxAllowedUids", "ImmunityPeriod", "Burn", "RegistrationsThisBlock", "MaxRegistrationsPerBlock", "RegistrationsThisInterval", "TargetRegistrationsPerInterval"} {
		base[name], err = read(name, root)
		if err != nil {
			return result, err
		}
	}
	result.SeatCount = binary.LittleEndian.Uint16(base["SubnetworkN"].data)
	result.MaximumSeats = binary.LittleEndian.Uint16(base["MaxAllowedUids"].data)
	result.BurnRao = binary.LittleEndian.Uint64(base["Burn"].data)
	result.RegistrationsThisBlock = binary.LittleEndian.Uint16(base["RegistrationsThisBlock"].data)
	result.MaxRegistrationsPerBlock = binary.LittleEndian.Uint16(base["MaxRegistrationsPerBlock"].data)
	result.RegistrationsInInterval = binary.LittleEndian.Uint16(base["RegistrationsThisInterval"].data)
	result.TargetInInterval = binary.LittleEndian.Uint16(base["TargetRegistrationsPerInterval"].data)
	immunity := uint64(binary.LittleEndian.Uint16(base["ImmunityPeriod"].data))
	if int(result.SeatCount) > rootCensusLimit || len(self.Keys) != int(result.SeatCount) || len(self.UidKeys) != int(result.SeatCount) {
		return result, errors.New("root registration census cardinality differs from root capacity")
	}
	wantKeys, wantUids := make([]string, 0, result.SeatCount), make([]string, 0, result.SeatCount)
	for uid := uint16(0); uid < result.SeatCount; uid++ {
		uidArg := binary.LittleEndian.AppendUint16(nil, uid)
		key, err := read("Keys", root, uidArg)
		if err != nil || key.RawStorage == nil || !rootCanonicalHash(key.EffectiveScale) {
			return result, errors.Join(errors.New("root registration census has a missing hotkey"), err)
		}
		reverse, err := read("Uids", root, key.data)
		if err != nil || reverse.RawStorage == nil || binary.LittleEndian.Uint16(reverse.data) != uid {
			return result, errors.Join(errors.New("root registration forward/reverse census disagrees"), err)
		}
		owner, err := read("Owner", key.data)
		if err != nil || owner.RawStorage == nil || !rootCanonicalHash(owner.EffectiveScale) {
			return result, errors.Join(errors.New("root registration census owner is absent"), err)
		}
		registration, err := read("BlockAtRegistration", root, uidArg)
		if err != nil || registration.RawStorage == nil {
			return result, errors.Join(errors.New("root registration census generation is absent"), err)
		}
		registered := binary.LittleEndian.Uint64(registration.data)
		if registered > self.FinalizedNumber {
			return result, errors.New("root registration census generation is in the future")
		}
		stake, err := read("TotalHotkeyAlpha", key.data, root)
		if err != nil {
			return result, err
		}
		candidate := rootRegisterCandidate{Uid: uid, Hotkey: key.EffectiveScale, RegistrationBlock: registered, StakeRao: binary.LittleEndian.Uint64(stake.data)}
		prior := result.Candidate
		if self.FinalizedNumber-registered >= immunity && (prior == nil || candidate.StakeRao < prior.StakeRao || candidate.StakeRao == prior.StakeRao && (registered < prior.RegistrationBlock || registered == prior.RegistrationBlock && uid < prior.Uid)) {
			result.Candidate = &candidate
		}
		if key.EffectiveScale == policy.Hotkey {
			result.ExistingSeat = &rootSeatExpectation{Uid: uid, RegistrationBlock: registered}
		}
		wantKeys, wantUids = append(wantKeys, key.Key), append(wantUids, reverse.Key)
	}
	sort.Strings(wantKeys)
	sort.Strings(wantUids)
	if !slices.Equal(self.Keys, wantKeys) || !slices.Equal(self.UidKeys, wantUids) {
		return result, errors.New("root registration census includes a foreign or missing key")
	}
	selected, err := read("Uids", root, hotkey)
	if err != nil {
		return result, err
	}
	if (selected.RawStorage != nil) != (result.ExistingSeat != nil) || selected.RawStorage != nil && binary.LittleEndian.Uint16(selected.data) != result.ExistingSeat.Uid {
		return result, errors.New("root registration selected hotkey contradicts full census")
	}
	owner, err := read("Owner", hotkey)
	if err != nil {
		return result, err
	}
	result.OwnerPresent = owner.RawStorage != nil
	if result.OwnerPresent {
		result.ExistingOwner = owner.EffectiveScale
	}
	stake, err := read("TotalHotkeyAlpha", hotkey, root)
	if err != nil {
		return result, err
	}
	result.ApplicantStakeRao = binary.LittleEndian.Uint64(stake.data)
	delegate, err := read("Delegates", hotkey)
	if err != nil {
		return result, err
	}
	result.DelegatePresent, result.DelegateTake = delegate.RawStorage != nil, binary.LittleEndian.Uint16(delegate.data)
	auto, err := read("AutoParentDelegationEnabled", hotkey)
	if err != nil {
		return result, err
	}
	result.AutoParentEnabled = auto.data[0] == 1
	for _, name := range []string{"OwnedHotkeys", "StakingHotkeys"} {
		row, err := read(name, operator)
		if err != nil {
			return result, err
		}
		data, count, err := rootVector(row.data, 32, 0)
		if err != nil {
			return result, err
		}
		accounts := make([]string, 0, count)
		for offset := 0; offset < len(data); offset += 32 {
			accounts = append(accounts, "0x"+hex.EncodeToString(data[offset:offset+32]))
		}
		if name == "OwnedHotkeys" {
			result.OwnedHotkeys = accounts
		} else {
			result.StakingHotkeys = accounts
		}
	}
	account, err := read("System.Account", operator)
	if err != nil {
		return result, err
	}
	result.AccountPresent = account.RawStorage != nil
	result.Nonce = binary.LittleEndian.Uint32(account.data[:4])
	result.FreeRao = binary.LittleEndian.Uint64(account.data[16:24])
	result.FrozenRao = binary.LittleEndian.Uint64(account.data[32:40])
	retained := max(result.FrozenRao, deposit)
	if result.FreeRao > retained {
		result.ConservativeReducibleRao = result.FreeRao - retained
	}
	if len(used) != len(rowKVs) {
		return result, errors.New("root registration observation contains unrelated storage rows")
	}
	// Source473 saturates the u16 interval multiplier. Counters are current-block
	// observations and can reset or change before the mortal call is included.
	intervalLimit := min(uint32(result.TargetInInterval)*3, uint32(^uint16(0)))
	switch {
	case base["NetworksAdded"].data[0] != 1:
		result.Reason = "root-network-missing"
	case result.ExistingSeat != nil:
		result.Reason = "root-seat-already-present"
	case rootRegisterReservedHotkey(policy.Hotkey):
		result.Reason = "reserved-subnet-system-hotkey"
	case result.OwnerPresent && result.ExistingOwner != policy.Operator:
		result.Reason = "hotkey-owned-by-another-coldkey"
	case result.RegistrationsThisBlock >= result.MaxRegistrationsPerBlock:
		result.Reason = "root-block-registration-limit"
	case uint32(result.RegistrationsInInterval) >= intervalLimit:
		result.Reason = "root-interval-registration-limit"
	case result.SeatCount >= result.MaximumSeats && result.Candidate == nil:
		result.Reason = "root-full-without-nonimmune-seat"
	case result.SeatCount >= result.MaximumSeats && result.ApplicantStakeRao < result.Candidate.StakeRao:
		result.Reason = "root-stake-below-pruning-candidate"
	case !result.AccountPresent:
		result.Reason = "operator-account-not-funded"
	case result.BurnRao > result.ConservativeReducibleRao:
		result.Reason = "quoted-burn-exceeds-conservative-balance"
	default:
		result.Eligible, result.Reason = true, "snapshot-eligible-execution-state-can-change"
	}
	return result, nil
}

// Source473 uses PalletId("subtensr").try_from_sub_account::<NetUid>.
// Its pinned SDK decodes "modl", the eight-byte pallet ID and a u16, then
// requires every remaining AccountId32 byte to be zero, even for absent subnets.
func rootRegisterReservedHotkey(account string) bool {
	raw, err := rootReceiptHex(account, 32)
	return err == nil && len(raw) == 32 && bytes.Equal(raw[:12], []byte("modlsubtensr")) && bytes.Equal(raw[14:], make([]byte, 18))
}

// A bounded exact-hash read retains all rows needed to reconstruct eligibility.
// The caller authenticates the finalized header, network and mapping fence.
func (self *rootCanonicalChain) rootRegisterObservationAt(ctx context.Context, policy rootRegisterPolicy, hash string, number uint64) (rootRegisterObservation, error) {
	result := rootRegisterObservation{Schema: rootRegisterObservationSchema, PolicyHash: rootObjectHash(policy), FinalizedNumber: number, FinalizedHash: hash}
	if err := policy.validate(); err != nil {
		return rootRegisterObservation{}, err
	}
	runtime, err := self.nativeRuntimeAt(ctx, hash)
	if err != nil {
		return rootRegisterObservation{}, err
	}
	if runtime.profile != policy.runtime() {
		return rootRegisterObservation{}, errors.New("root registration runtime differs from approved policy")
	}
	if _, err := rootRegisterCall(runtime.metadata); err != nil {
		return rootRegisterObservation{}, err
	}
	if _, err := rootRegisterExistentialDeposit(runtime.metadata); err != nil {
		return rootRegisterObservation{}, err
	}
	specs := rootRegisterStorageSpecs()
	entries, err := observationStorageProfile(runtime.metadata, specs)
	if err != nil {
		return rootRegisterObservation{}, err
	}
	reader := rootStorageReader{client: self.client, metadata: runtime.metadata, entries: entries, specs: specs, block: hash, valueKVs: map[string]rootStorageValue{}}
	root := []byte{0, 0}
	for _, name := range []string{"SubnetOwner", "NetworksAdded"} {
		if _, err := reader.read(ctx, name, []byte{25, 0}); err != nil {
			return rootRegisterObservation{}, err
		}
	}
	for _, name := range []string{"NetworksAdded", "SubnetworkN", "MaxAllowedUids", "ImmunityPeriod", "Burn", "RegistrationsThisBlock", "MaxRegistrationsPerBlock", "RegistrationsThisInterval", "TargetRegistrationsPerInterval"} {
		if _, err := reader.read(ctx, name, root); err != nil {
			return rootRegisterObservation{}, err
		}
	}
	countKey, _ := types.CreateStorageKey(runtime.metadata, "SubtensorModule", "SubnetworkN", root)
	count := int(binary.LittleEndian.Uint16(reader.valueKVs[countKey.Hex()].data))
	if count > rootCensusLimit {
		return rootRegisterObservation{}, errors.New("root registration seat count exceeds local bound")
	}
	keyProbe, err := types.CreateStorageKey(runtime.metadata, "SubtensorModule", "Keys", root, root)
	if err != nil || len(keyProbe) != 36 {
		return rootRegisterObservation{}, errors.New("root registration forward census prefix changed")
	}
	result.Keys, err = reader.keys(ctx, keyProbe[:34])
	if err != nil || len(result.Keys) != count {
		return rootRegisterObservation{}, errors.Join(errors.New("root registration forward census cardinality differs"), err)
	}
	reverseProbe, err := types.CreateStorageKey(runtime.metadata, "SubtensorModule", "Uids", root, make([]byte, 32))
	if err != nil || len(reverseProbe) != observationCensusKeyBytes {
		return rootRegisterObservation{}, errors.New("root registration reverse census prefix changed")
	}
	result.UidKeys, err = reader.keys(ctx, reverseProbe[:34])
	if err != nil || len(result.UidKeys) != count {
		return rootRegisterObservation{}, errors.Join(errors.New("root registration reverse census cardinality differs"), err)
	}
	err = rootReadParallel(ctx, count, func(ctx context.Context, index int) error {
		uid := binary.LittleEndian.AppendUint16(nil, uint16(index))
		key, err := reader.read(ctx, "Keys", root, uid)
		if err != nil || key.RawStorage == nil {
			return errors.Join(errors.New("root registration root census has a hole"), err)
		}
		for _, read := range []struct {
			name string
			args [][]byte
		}{
			{name: "Uids", args: [][]byte{root, key.data}},
			{name: "Owner", args: [][]byte{key.data}},
			{name: "BlockAtRegistration", args: [][]byte{root, uid}},
			{name: "TotalHotkeyAlpha", args: [][]byte{key.data, root}},
		} {
			if _, err := reader.read(ctx, read.name, read.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return rootRegisterObservation{}, err
	}
	hotkey, _ := hex.DecodeString(policy.Hotkey[2:])
	operator, _ := hex.DecodeString(policy.Operator[2:])
	for _, read := range []struct {
		name string
		args [][]byte
	}{
		{name: "Uids", args: [][]byte{root, hotkey}}, {name: "Owner", args: [][]byte{hotkey}},
		{name: "TotalHotkeyAlpha", args: [][]byte{hotkey, root}}, {name: "Delegates", args: [][]byte{hotkey}},
		{name: "AutoParentDelegationEnabled", args: [][]byte{hotkey}},
		{name: "OwnedHotkeys", args: [][]byte{operator}}, {name: "StakingHotkeys", args: [][]byte{operator}},
	} {
		if _, err := reader.read(ctx, read.name, read.args...); err != nil {
			return rootRegisterObservation{}, err
		}
	}
	result.Storage = reader.evidence()
	accountEntry, err := rootSystemEntry(runtime.metadata, "Account")
	if err != nil {
		return rootRegisterObservation{}, err
	}
	accountKey, err := types.CreateStorageKey(runtime.metadata, "System", "Account", operator)
	if err != nil {
		return rootRegisterObservation{}, err
	}
	var raw *string
	if err := self.client.callWithStorageAbsence(ctx, "state_getStorage", []any{accountKey.Hex(), hash}, &raw, true); err != nil {
		return rootRegisterObservation{}, err
	}
	data, source := []byte(accountEntry.Fallback), "authenticated-metadata-fallback"
	if raw != nil {
		data, err = rootReceiptHex(*raw, 56)
		if err != nil {
			return rootRegisterObservation{}, err
		}
		source = "finalized-storage"
	}
	result.Storage = append(result.Storage, rootStorageValue{Name: "System.Account", Key: accountKey.Hex(), RawStorage: raw, EffectiveScale: "0x" + hex.EncodeToString(data), ValueSource: source})
	sort.Slice(result.Storage, func(i, j int) bool { return result.Storage[i].Key < result.Storage[j].Key })
	result.ContentHash = rootObjectHash(result)
	if _, err := result.eligibility(policy, runtime.metadata); err != nil {
		return rootRegisterObservation{}, err
	}
	return result, nil
}

// Equality does not infer ownership from account-list membership: Owner is the
// runtime authority and the lists merely retain possible account-creation effects.
func rootRegisterSameAccount(raw []byte, account string) bool {
	expected, err := rootReceiptHex(account, 32)
	return err == nil && len(expected) == 32 && bytes.Equal(raw, expected)
}
