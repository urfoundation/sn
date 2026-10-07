// Treasury admission keeps exact hash-addressed native storage. The public RPC
// remains the approved observation source, not an independent finality oracle.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const treasuryObservationSchema = "urnetwork-native-treasury-observation-v1"
const treasuryRowLimit = 256 * 1024

// One exact-key reader serves native capture and retained observation replay.
type treasuryStorageReader func(context.Context, string, []byte) ([]byte, bool, error)

// Each row is an original raw value, including explicit native absence.
type treasuryStorageRow struct {
	Key string  `json:"key"`
	Raw *string `json:"raw"`
}

// A checksum is review evidence, never an authorization signature.
type treasuryObservation struct {
	Schema          string               `json:"schema"`
	ScopeHash       string               `json:"scope_hash"`
	FinalizedNumber uint64               `json:"finalized_number"`
	FinalizedHash   string               `json:"finalized_hash"`
	Rows            []treasuryStorageRow `json:"rows"`
	ContentHash     string               `json:"content_hash"`
}

// Pending state retains the original depositor/timepoint and exact approvals.
type treasuryPending struct {
	Timepoint treasuryTimepoint `json:"timepoint"`
	Deposit   uint64            `json:"deposit_rao"`
	Depositor string            `json:"depositor"`
	Approvals []string          `json:"approvals"`
}

// These values are always re-derived from the retained original rows.
type treasuryFacts struct {
	Nonce                uint32
	SignatorySpendable   uint64
	TreasurySpendable    uint64
	Deposit              uint64
	SubnetOwner          string
	RegistrationBlock    uint64
	SubnetGeneration     uint64
	Count                uint16
	Capacity             uint16
	RegistrationAllowed  bool
	Burn                 uint64
	Recipients           []subnetRegistration
	Missing              int
	Pending              *treasuryPending
	AutoStakeDestination string
	LiquidationAvailable *uint64
}

// Only public identities/runtime/inner bytes determine this observation scope.
func treasuryObservationScope(a treasuryAction) string {
	inner, _ := a.innerCall()
	return rootObjectHash(struct {
		Policy     treasuryChainPolicy
		Descriptor treasuryDescriptor
		Owner      string
		Inner      string
		Operation  string
	}{Policy: a.Policy, Descriptor: a.Descriptor, Owner: a.Owner, Inner: "0x" + hex.EncodeToString(inner), Operation: a.Operation})
}

// Decode fixed source fields, including SCALE compact count and no trailing data.
func decodeTreasuryPending(raw []byte, a treasuryAction) (*treasuryPending, error) {
	if len(raw) < 49 || len(raw) > 3300 {
		return nil, errors.New("treasury pending value length invalid")
	}
	p := &treasuryPending{Timepoint: treasuryTimepoint{Height: binary.LittleEndian.Uint32(raw[:4]), Index: binary.LittleEndian.Uint32(raw[4:8])}, Deposit: binary.LittleEndian.Uint64(raw[8:16]), Depositor: "0x" + hex.EncodeToString(raw[16:48])}
	r := rootScaleReader{data: raw, offset: 48}
	n, err := r.compact()
	if err != nil || n == 0 || n > uint64(len(a.Descriptor.Multisig.Signatories)) || len(raw)-r.offset != int(n)*32 {
		return nil, errors.New("treasury pending approval count invalid")
	}
	allowed := map[string]bool{}
	for _, s := range a.Descriptor.Multisig.Signatories {
		allowed[s.AccountId] = true
	}
	previous := ""
	depositor := false
	for i := uint64(0); i < n; i++ {
		v, _ := r.take(32)
		account := "0x" + hex.EncodeToString(v)
		if !allowed[account] || account <= previous {
			return nil, errors.New("treasury pending approvals are foreign or unsorted")
		}
		p.Approvals = append(p.Approvals, account)
		previous = account
		depositor = depositor || account == p.Depositor
	}
	if !depositor || p.Timepoint.Height == 0 || p.Deposit == 0 {
		return nil, errors.New("treasury pending depositor/timepoint/deposit absent")
	}
	return p, nil
}

// An injected read primitive is used both for live storage and exact replay.
// No derived JSON field supplies an owner, generation, nonce or deposit limit.
func treasuryReadFacts(ctx context.Context, a treasuryAction, metadata *types.Metadata, number uint64, read func(context.Context, string, []byte) ([]byte, bool, error)) (treasuryFacts, error) {
	var f treasuryFacts
	if ctx == nil || read == nil {
		return f, errors.New("treasury native observation requires an owned context and reader")
	}
	if err := errors.Join(a.Policy.validate(), a.Descriptor.validate()); err != nil {
		return f, err
	}
	if a.Policy.GenesisHash != a.Descriptor.GenesisHash || !rootCanonicalHash(a.Owner) {
		return f, errors.New("treasury observation differs from custody network or signatory")
	}
	deposit, err := treasuryMetadataProfile(metadata, a)
	if err != nil {
		return f, err
	}
	f.Deposit = deposit
	if a.Operation == "cancel_as_multi" {
		return treasuryCancellationFacts(ctx, a, metadata, number, read)
	}
	f, err = treasuryReadRecipientFacts(ctx, a.Descriptor.destination(), metadata, number, read, true)
	if err != nil {
		return f, err
	}
	f.Deposit = deposit
	if err := rootAccountProfile(metadata); err != nil {
		return f, err
	}
	for i, account := range []string{a.Owner, a.Descriptor.Multisig.AccountId} {
		raw, _ := hex.DecodeString(account[2:])
		key, err := types.CreateStorageKey(metadata, "System", "Account", raw)
		if err != nil {
			return f, err
		}
		data, present, err := read(ctx, key.Hex(), make([]byte, 56))
		if err != nil {
			return f, err
		}
		if len(data) != 56 {
			return f, errors.New("treasury account storage width changed")
		}
		free, frozen := binary.LittleEndian.Uint64(data[16:24]), binary.LittleEndian.Uint64(data[32:40])
		spendable := uint64(0)
		if free > frozen {
			spendable = free - frozen
		}
		if i == 0 {
			f.Nonce = binary.LittleEndian.Uint32(data[:4])
			f.SignatorySpendable = spendable
			if !present {
				return f, errors.New("treasury signatory account is unfunded")
			}
		} else {
			f.TreasurySpendable = spendable
		}
	}
	inner, err := a.innerCall()
	if err != nil {
		return f, err
	}
	account, _ := hex.DecodeString(a.Descriptor.Multisig.AccountId[2:])
	callHash, _ := hex.DecodeString(rootExtrinsicHash(inner)[2:])
	key, err := types.CreateStorageKey(metadata, "Multisig", "Multisigs", account, callHash)
	if err != nil {
		return f, err
	}
	raw, present, err := read(ctx, key.Hex(), nil)
	if err != nil {
		return f, err
	}
	if present {
		f.Pending, err = decodeTreasuryPending(raw, a)
		if err != nil {
			return f, err
		}
		if uint64(f.Pending.Timepoint.Height) > number {
			return f, fmt.Errorf("%w: treasury operation timepoint is in the future", errRpcIntegrity)
		}
	}
	if a.Inner.Kind == "remove_stake_limit" {
		f.LiquidationAvailable, err = treasuryLiquidationAvailable(ctx, a, metadata, read)
		if err != nil {
			return f, err
		}
	}
	sort.Slice(f.Recipients, func(i, j int) bool { return f.Recipients[i].Uid < f.Recipients[j].Uid })
	return f, nil
}

// Receiving observes ownership and generations; registration additionally reads
// its burn/availability fields without weakening the shared recipient checks.
func treasuryReadRecipientFacts(ctx context.Context, destination treasuryDestination, metadata *types.Metadata, number uint64, read treasuryStorageReader, registration bool) (treasuryFacts, error) {
	var f treasuryFacts
	if ctx == nil || read == nil || len(destination.RecipientHotkeys) < 2 {
		return f, errors.New("treasury routing requires an owned reader and at least two declared recipients")
	}
	if err := destination.validate(); err != nil {
		return f, err
	}
	specs := []rootStorageSpec{
		{name: "NetworksAdded", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bool"},
		{name: "SubnetworkN", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
		{name: "MaxAllowedUids", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u16"},
		{name: "NetworkRegisteredAt", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
		{name: "RegisteredSubnetCounter", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
		{name: "NetworkRegistrationAllowed", keys: []string{"u16"}, hashers: []string{"identity"}, value: "bool"},
		{name: "SubnetOwner", keys: []string{"u16"}, hashers: []string{"identity"}, value: "account"},
		{name: "SubnetOwnerHotkey", keys: []string{"u16"}, hashers: []string{"identity"}, value: "account"},
		{name: "Burn", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
		{name: "Owner", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "account"},
		{name: "OwnedHotkeys", keys: []string{"account"}, hashers: []string{"blake128concat"}, value: "accounts"},
		{name: "Uids", keys: []string{"u16", "account"}, hashers: []string{"identity", "blake128concat"}, value: "u16", optional: true},
		{name: "Keys", keys: []string{"u16", "u16"}, hashers: []string{"identity", "identity"}, value: "account"},
		{name: "BlockAtRegistration", keys: []string{"u16", "u16"}, hashers: []string{"identity", "identity"}, value: "u64"},
		{name: "IsNetworkMember", keys: []string{"account", "u16"}, hashers: []string{"blake128concat", "identity"}, value: "bool"},
		{name: "AutoStakeDestination", keys: []string{"account", "u16"}, hashers: []string{"blake128concat", "identity"}, value: "account", optional: true},
	}
	if !registration {
		filtered := make([]rootStorageSpec, 0, len(specs)-2)
		for _, spec := range specs {
			if spec.name != "NetworkRegistrationAllowed" && spec.name != "Burn" {
				filtered = append(filtered, spec)
			}
		}
		specs = filtered
	}
	entries, err := observationStorageProfile(metadata, specs)
	if err != nil {
		return f, err
	}
	get := func(name string, args ...[]byte) ([]byte, bool, error) {
		key, err := types.CreateStorageKey(metadata, "SubtensorModule", name, args...)
		if err != nil {
			return nil, false, err
		}
		fallback := entries[name].Fallback
		if entries[name].Modifier.IsOptional {
			fallback = nil
		}
		data, present, err := read(ctx, key.Hex(), fallback)
		if err != nil {
			return nil, false, err
		}
		shape := ""
		for _, s := range specs {
			if s.name == name {
				shape = s.value
			}
		}
		width := map[string]int{"bool": 1, "u16": 2, "u64": 8, "account": 32}[shape]
		if !present && entries[name].Modifier.IsOptional {
			return nil, false, nil
		}
		if shape == "accounts" {
			r := rootScaleReader{data: data}
			n, err := r.compact()
			if err != nil || n > rootCensusLimit || len(data)-r.offset != int(n)*32 {
				return nil, false, errors.New("treasury owner hotkey census exceeds bound or is truncated")
			}
			return data, present, nil
		}
		if len(data) != width || shape == "bool" && data[0] > 1 {
			return nil, false, errors.New("treasury storage field width/value changed")
		}
		return data, present, nil
	}
	net := []byte{25, 0}
	values := map[string][]byte{}
	names := []string{"NetworksAdded", "SubnetworkN", "MaxAllowedUids", "NetworkRegisteredAt", "RegisteredSubnetCounter", "SubnetOwner", "SubnetOwnerHotkey"}
	if registration {
		names = append(names, "NetworkRegistrationAllowed", "Burn")
	}
	for _, name := range names {
		raw, present, err := get(name, net)
		if err != nil {
			return f, err
		}
		if (name == "SubnetOwner" || name == "NetworkRegisteredAt") && !present {
			return f, errors.New("treasury subnet identity is missing")
		}
		values[name] = raw
	}
	if values["NetworksAdded"][0] != 1 {
		return f, errors.New("treasury subnet does not exist")
	}
	f.SubnetOwner = "0x" + hex.EncodeToString(values["SubnetOwner"])
	ownerHotkey := "0x" + hex.EncodeToString(values["SubnetOwnerHotkey"])
	if !rootCanonicalHash(f.SubnetOwner) || f.SubnetOwner == destination.AccountId {
		return f, fmt.Errorf("%w: treasury receiving account cannot be subnet owner", errRpcIntegrity)
	}
	ownerAccount, _ := hex.DecodeString(f.SubnetOwner[2:])
	ownedRaw, _, err := get("OwnedHotkeys", ownerAccount)
	if err != nil {
		return f, err
	}
	ownedReader := rootScaleReader{data: ownedRaw}
	ownedCount, _ := ownedReader.compact()
	ownerHotkeys := map[string]bool{}
	for i := uint64(0); i < ownedCount; i++ {
		raw, _ := ownedReader.take(32)
		hotkey := "0x" + hex.EncodeToString(raw)
		if !rootCanonicalHash(hotkey) || ownerHotkeys[hotkey] {
			return f, fmt.Errorf("%w: treasury owner hotkey census repeats an invalid identity", errRpcIntegrity)
		}
		ownerHotkeys[hotkey] = true
	}
	f.Count = binary.LittleEndian.Uint16(values["SubnetworkN"])
	f.Capacity = binary.LittleEndian.Uint16(values["MaxAllowedUids"])
	f.RegistrationBlock = binary.LittleEndian.Uint64(values["NetworkRegisteredAt"])
	f.SubnetGeneration = binary.LittleEndian.Uint64(values["RegisteredSubnetCounter"])
	if registration {
		f.RegistrationAllowed = values["NetworkRegistrationAllowed"][0] == 1
		f.Burn = binary.LittleEndian.Uint64(values["Burn"])
	}
	if f.Capacity == 0 || f.Count > f.Capacity || f.Count > rootCensusLimit || f.RegistrationBlock > number {
		return f, fmt.Errorf("%w: treasury subnet capacity/generation contradiction", errRpcIntegrity)
	}
	uids := map[uint16]bool{}
	for _, recipient := range destination.RecipientHotkeys {
		hotkey, _ := hex.DecodeString(recipient[2:])
		owner, owned, err := get("Owner", hotkey)
		if err != nil {
			return f, err
		}
		coldkey := "0x" + hex.EncodeToString(owner)
		if recipient == ownerHotkey || ownerHotkeys[recipient] || owned && coldkey != destination.AccountId {
			return f, fmt.Errorf("%w: treasury hotkey is an owner or belongs to another coldkey", errRpcIntegrity)
		}
		uid, registered, err := get("Uids", net, hotkey)
		if err != nil {
			return f, err
		}
		member, _, err := get("IsNetworkMember", hotkey, net)
		if err != nil {
			return f, err
		}
		if !registered {
			if member[0] != 0 {
				return f, fmt.Errorf("%w: absent treasury UID has membership", errRpcIntegrity)
			}
			f.Missing++
			continue
		}
		id := binary.LittleEndian.Uint16(uid)
		if !owned || member[0] != 1 || id >= f.Count || uids[id] {
			return f, fmt.Errorf("%w: treasury UID/owner/membership contradiction", errRpcIntegrity)
		}
		uids[id] = true
		forward, present, err := get("Keys", net, uid)
		if err != nil {
			return f, err
		}
		if !present || !bytes.Equal(forward, hotkey) {
			return f, fmt.Errorf("%w: treasury forward/reverse registration mismatch", errRpcIntegrity)
		}
		generation, present, err := get("BlockAtRegistration", net, uid)
		if err != nil {
			return f, err
		}
		block := uint64(0)
		if present {
			block = binary.LittleEndian.Uint64(generation)
		}
		if !present || block > number || block < f.RegistrationBlock {
			return f, fmt.Errorf("%w: treasury registration generation invalid", errRpcIntegrity)
		}
		f.Recipients = append(f.Recipients, subnetRegistration{Uid: id, Hotkey: recipient, Coldkey: coldkey, RegistrationBlock: block})
	}
	multisig, _ := hex.DecodeString(destination.AccountId[2:])
	autoStakeRaw, present, err := get("AutoStakeDestination", multisig, net)
	if err != nil {
		return f, err
	}
	if present {
		f.AutoStakeDestination = "0x" + hex.EncodeToString(autoStakeRaw)
		if !rootCanonicalHash(f.AutoStakeDestination) {
			return f, errors.New("treasury auto-stake destination is invalid")
		}
	}
	sort.Slice(f.Recipients, func(i, j int) bool { return f.Recipients[i].Uid < f.Recipients[j].Uid })
	return f, nil
}

// Replayed observation values are complete, unique and exact-key addressed.
func (self treasuryObservation) facts(ctx context.Context, a treasuryAction, metadata *types.Metadata) (treasuryFacts, error) {
	return self.replay(ctx, treasuryObservationSchema, treasuryObservationScope(a), func(read treasuryStorageReader) (treasuryFacts, error) {
		return treasuryReadFacts(ctx, a, metadata, self.FinalizedNumber, read)
	})
}

// Scope and exact consumed rows cannot cross receiving and execution domains.
func (self treasuryObservation) replay(ctx context.Context, schema, scope string, readFacts func(treasuryStorageReader) (treasuryFacts, error)) (treasuryFacts, error) {
	claimed := self.ContentHash
	self.ContentHash = ""
	if self.Schema != schema || self.ScopeHash != scope || claimed != rootObjectHash(self) || !rootCanonicalHash(self.FinalizedHash) || self.FinalizedNumber == 0 || len(self.Rows) > 1024 {
		return treasuryFacts{}, errors.New("treasury observation scope or bound changed")
	}
	rows := map[string]*string{}
	for _, row := range self.Rows {
		if _, exists := rows[row.Key]; exists {
			return treasuryFacts{}, errors.New("duplicate treasury storage row")
		}
		rows[row.Key] = row.Raw
	}
	used := map[string]bool{}
	f, err := readFacts(func(ctx context.Context, key string, fallback []byte) ([]byte, bool, error) {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		raw, exists := rows[key]
		if !exists {
			return nil, false, errors.New("treasury observation is incomplete")
		}
		used[key] = true
		if raw == nil {
			return fallback, false, nil
		}
		data, err := rootReceiptHex(*raw, treasuryRowLimit)
		return data, true, err
	})
	if err == nil && len(used) != len(rows) {
		err = errors.New("treasury observation contains unrelated storage")
	}
	return f, err
}

// Admission leaves native fees and multisig deposits independently bounded.
func (f treasuryFacts) admits(a treasuryAction, initial bool) error {
	if a.Operation == "cancel_as_multi" {
		if a.Timepoint == nil || f.Pending == nil || f.Pending.Timepoint != *a.Timepoint || f.Pending.Depositor != a.Owner {
			return errors.New("treasury cancellation requires the exact original pending timepoint and depositor")
		}
		if f.Nonce != a.Nonce || f.SignatorySpendable < a.FeeReserveRao {
			return errors.New("treasury cancellation lacks original nonce or fee reserve")
		}
		return nil
	}
	if a.Treasury != nil {
		projection, err := treasuryPublicPolicy(a.Descriptor, f)
		if err != nil {
			return err
		}
		hash, err := projection.Hash()
		if err != nil || a.TreasuryPolicyHash != "0x"+hex.EncodeToString(hash[:]) {
			return errors.Join(errors.New("treasury current ownership, generation or auto-stake destination differs from approved public policy"), err)
		}
	}
	if f.RegistrationBlock != a.SubnetRegistrationBlock || f.SubnetGeneration != a.SubnetGeneration {
		return fmt.Errorf("%w: treasury subnet generation changed", errRpcIntegrity)
	}
	if f.Nonce != a.Nonce {
		return errors.New("treasury signatory nonce differs from original action")
	}
	deposit := uint64(0)
	if a.Timepoint == nil {
		if f.Pending != nil {
			return errors.New("treasury operation already exists; retain its original timepoint")
		}
		deposit = f.Deposit
		if deposit > a.DepositLimitRao {
			return errors.New("treasury deposit exceeds approved cap")
		}
	} else {
		if f.Pending == nil || f.Pending.Timepoint != *a.Timepoint {
			return errors.New("treasury original pending operation absent or timepoint changed")
		}
		if a.Operation == "cancel_as_multi" && f.Pending.Depositor != a.Owner {
			return errors.New("only the original multisig depositor can cancel")
		}
		approved := false
		for _, account := range f.Pending.Approvals {
			approved = approved || account == a.Owner
		}
		if a.Operation == "approve_as_multi" && approved {
			return errors.New("treasury signatory already approved this operation")
		}
	}
	if deposit > math.MaxUint64-a.FeeReserveRao || f.SignatorySpendable < deposit+a.FeeReserveRao {
		return errors.New("treasury signatory lacks admitted fee/deposit reserve")
	}
	if a.Operation != "cancel_as_multi" && a.Inner.Kind == "register_limit" {
		for _, r := range f.Recipients {
			if r.Hotkey == a.Inner.Hotkey {
				return errors.New("treasury recipient is already registered; no repeat registration")
			}
		}
		if !f.RegistrationAllowed || f.Burn > a.Inner.LimitPrice || int(f.Capacity)-int(f.Count) < f.Missing || f.TreasurySpendable < a.Inner.LimitPrice {
			return errors.New("treasury registration lacks burn funding, open registration or unpruned capacity for every recipient")
		}
	}
	if a.Operation != "cancel_as_multi" && a.Inner.Kind == "transfer_keep_alive" && f.TreasurySpendable <= a.Inner.Amount {
		return errors.New("treasury transfer lacks liquid balance plus account survival reserve")
	}
	if a.Operation != "cancel_as_multi" && a.Inner.Kind == "remove_stake_limit" && (f.LiquidationAvailable == nil || *f.LiquidationAvailable < a.Inner.Amount) {
		return errors.New("treasury liquidation exceeds the selected position or coldkey-wide available stake")
	}
	if initial && a.BirthBlock < f.RegistrationBlock {
		return errors.New("treasury birth precedes subnet generation")
	}
	return nil
}

// Every RPC row is retained once at an authenticated finalized hash.
func (self *rootCanonicalChain) treasuryObservationAt(ctx context.Context, a treasuryAction, hash string, number uint64) (treasuryObservation, error) {
	return self.captureTreasuryObservation(ctx, treasuryObservationSchema, treasuryObservationScope(a), hash, number, self.nativeRuntimeAt, func(metadata *types.Metadata, read treasuryStorageReader) (treasuryFacts, error) {
		return treasuryReadFacts(ctx, a, metadata, number, read)
	})
}

// Each observation authenticates its selected runtime and retains exact raw rows.
func (self *rootCanonicalChain) captureTreasuryObservation(ctx context.Context, schema, scope, hash string, number uint64, loadRuntime func(context.Context, string) (rootReceiptRuntime, error), readFacts func(*types.Metadata, treasuryStorageReader) (treasuryFacts, error)) (treasuryObservation, error) {
	result := treasuryObservation{Schema: schema, ScopeHash: scope, FinalizedNumber: number, FinalizedHash: hash}
	runtime, err := loadRuntime(ctx, hash)
	if err != nil {
		return result, err
	}
	seen := map[string]treasuryStorageRow{}
	_, err = readFacts(runtime.metadata, func(ctx context.Context, key string, fallback []byte) ([]byte, bool, error) {
		row, exists := seen[key]
		if !exists {
			var raw *string
			if strings.HasPrefix(key, "runtime-api:") {
				parts := strings.SplitN(key, ":", 3)
				var encoded string
				if err := self.client.call(ctx, "state_call", []any{parts[1], parts[2], hash}, &encoded); err != nil {
					return nil, false, err
				}
				raw = &encoded
			} else if err := self.client.callWithStorageAbsence(ctx, "state_getStorage", []any{key, hash}, &raw, true); err != nil {
				return nil, false, err
			}
			row = treasuryStorageRow{Key: key, Raw: raw}
			seen[key] = row
		}
		if row.Raw == nil {
			return fallback, false, nil
		}
		data, err := rootReceiptHex(*row.Raw, treasuryRowLimit)
		return data, true, err
	})
	if err != nil {
		return treasuryObservation{}, err
	}
	for _, row := range seen {
		result.Rows = append(result.Rows, row)
	}
	sort.Slice(result.Rows, func(i, j int) bool { return result.Rows[i].Key < result.Rows[j].Key })
	result.ContentHash = rootObjectHash(result)
	return result, nil
}
