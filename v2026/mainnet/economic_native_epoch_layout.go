// This explicit layout joins original storage observations with the original
// epoch vectors. It never invents a spilled total or a contiguous hotkey vector.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const nativeEpochStorageLayoutSchema = "single-mechanism-original-keys-v1"

type nativeEpochInputProvenance struct {
	Schema                  string   `json:"schema"`
	EpochObservationOrdinal uint64   `json:"epoch_observation_ordinal"`
	DrainOrdinals           []uint64 `json:"drain_ordinals"`
	TotalAlpha              string   `json:"saturated_original_tranche_total"`
	UidReadOrdinals         []uint64 `json:"uid_read_ordinals"`
	EpochWriteOrdinal       uint64   `json:"epoch_write_ordinal"`
}

type nativeEpochStorageLayout struct {
	netuid             uint16
	keysPrefix         []byte
	epochPrefix        []byte
	registrationKey    string
	generationKey      string
	generationFallback []byte
	uidRecords         map[uint16]historicalReplayObservation
	epochRecord        *historicalReplayObservation
}

func (self *historicalReplayObservationProfile) validateEpochLayout() error {
	if self.EpochLayout != nil && (*self.EpochLayout != nativeEpochStorageLayoutSchema || self.Schema != historicalNativeProfileSchema) {
		return errors.New("native epoch layout is not an explicit reviewed storage join")
	}
	stateKeys := map[string]bool{}
	for _, rule := range self.Rules {
		if (rule.Purpose == "native-uid-census" || rule.Purpose == "native-epoch-index") && (self.EpochLayout == nil || len(rule.Memory) != 0) {
			return errors.New("native epoch storage census lacks an explicit layout")
		}
		if len(rule.StateReads) != 0 {
			if self.EpochLayout == nil || rule.Purpose != "native-drain" || len(rule.StateReads) != 2 || rule.StateReads[0] == rule.StateReads[1] {
				return errors.New("native observer state reads exceed their explicit two-key drain scope")
			}
			for _, value := range rule.StateReads {
				key, err := historicalReplayHex(value, 64)
				if err != nil || len(key) == 0 || "0x"+hex.EncodeToString(key) != value {
					return errors.New("native observer state key is not canonical bounded hex")
				}
				stateKeys[value] = true
				if len(stateKeys) > 2 {
					return errors.New("native observer state key pair changed between callsites")
				}
			}
		}
		if self.EpochLayout != nil && rule.Purpose == "native-epoch" {
			if rule.HostSnapshot == nil {
				return errors.New("native storage-joined epoch lacks its explicit original allocator boundary")
			}
			for _, field := range rule.Memory {
				switch field.Name {
				case "total-alpha", "uids", "hotkeys", "subnet-epoch":
					return errors.New("native storage-joined epoch cannot substitute legacy memory labels")
				}
			}
		}
	}
	return nil
}

// Metadata is the same independently pinned runtime metadata used to decode
// the event and drains; exact map types/hashers are checked before key creation.
func newNativeEpochStorageLayout(profile *historicalReplayObservationProfile, metadata *types.Metadata, netuid uint16) (*nativeEpochStorageLayout, error) {
	if profile.EpochLayout == nil {
		return nil, nil
	}
	if err := profile.validateEpochLayout(); err != nil {
		return nil, err
	}
	if metadata == nil {
		return nil, errors.New("native epoch storage join lacks original runtime metadata")
	}
	entries, err := observationStorageProfile(metadata, []rootStorageSpec{
		{name: "Keys", keys: []string{"u16", "u16"}, hashers: []string{"identity", "identity"}, value: "account"},
		{name: "SubnetEpochIndex", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
		{name: "NetworkRegisteredAt", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
		{name: "RegisteredSubnetCounter", keys: []string{"u16"}, hashers: []string{"identity"}, value: "u64"},
	})
	if err != nil {
		return nil, err
	}
	arg := binary.LittleEndian.AppendUint16(nil, netuid)
	key, err := types.CreateStorageKey(metadata, "SubtensorModule", "Keys", arg, []byte{0, 0})
	if err != nil || len(key) != 36 {
		return nil, errors.Join(errors.New("native Keys layout is not the admitted Identity pair"), err)
	}
	epoch, err := types.CreateStorageKey(metadata, "SubtensorModule", "SubnetEpochIndex", arg)
	if err != nil || len(epoch) != 34 {
		return nil, errors.Join(errors.New("native epoch index layout is not admitted Identity"), err)
	}
	registration, err := types.CreateStorageKey(metadata, "SubtensorModule", "NetworkRegisteredAt", arg)
	if err != nil {
		return nil, err
	}
	generation, err := types.CreateStorageKey(metadata, "SubtensorModule", "RegisteredSubnetCounter", arg)
	if err != nil {
		return nil, err
	}
	fallback := entries["RegisteredSubnetCounter"].Fallback
	if len(fallback) != 8 {
		return nil, errors.New("native subnet generation metadata default is not u64")
	}
	return &nativeEpochStorageLayout{registrationKey: registration.Hex(), generationKey: generation.Hex(), generationFallback: append([]byte(nil), fallback...), netuid: netuid, keysPrefix: append([]byte(nil), key[:32]...), epochPrefix: append([]byte(nil), epoch[:32]...), uidRecords: map[uint16]historicalReplayObservation{}}, nil
}

func (self *nativeEpochStorageLayout) hasRecords() bool {
	return len(self.uidRecords) != 0 || self.epochRecord != nil
}

func validateHistoricalExecutionState(rule historicalReplayHookRule, record historicalNativeObservation) error {
	if rule.RecipientOwner {
		return validateHistoricalRecipientOwnerState(record)
	}
	if len(rule.StateReads) == 0 {
		if record.ExecutionState != nil {
			return errors.New("native original observation invented execution-state reads")
		}
		return nil
	}
	if record.ExecutionState == nil || len(*record.ExecutionState) != len(rule.StateReads) {
		return errors.New("native original observation omitted explicit execution-state reads")
	}
	for index, value := range *record.ExecutionState {
		if value.KeyHex != rule.StateReads[index] {
			return errors.New("native execution-state read changed its selected key")
		}
		if value.ValueHex != nil {
			if _, err := historicalReplayHex(*value.ValueHex, 8); err != nil {
				return err
			}
		}
	}
	return nil
}

// Exact Identity suffixes choose the subnet for an original pending get. This
// does not require a fictitious netuid local in the nested storage host frame.
func nativeEpochDrainNetuid(record historicalReplayObservation, drains [3]nativeExecutionDrain) (uint64, error) {
	if record.Native == nil || record.Native.ExecutionPhaseHex == nil || *record.Native.ExecutionPhaseHex != "0x02" {
		return 0, errors.New("native original drain lacks Initialization phase")
	}
	key, err := historicalReplayHex(record.KeyHex, 34)
	if err != nil || len(key) != 34 {
		return 0, errors.New("native original drain key is not an exact Identity subnet map")
	}
	for _, selected := range drains {
		expected, err := historicalReplayHex(selected.Key, 34)
		if err != nil || len(expected) != 34 {
			return 0, errors.New("native admitted drain key layout differs")
		}
		if bytes.Equal(key[:32], expected[:32]) {
			return uint64(binary.LittleEndian.Uint16(key[32:])), nil
		}
	}
	return 0, errors.New("native original drain changed its admitted namespace")
}

func (self *nativeEpochStorageLayout) validateGeneration(record historicalReplayObservation, policy economicEmissionPolicy) error {
	if record.Native == nil || record.Native.ExecutionPhaseHex == nil || *record.Native.ExecutionPhaseHex != "0x02" || record.Native.ExecutionState == nil || len(*record.Native.ExecutionState) != 2 {
		return errors.New("native drain omitted its original two-key execution-state custody")
	}
	values := map[string]*string{}
	for _, value := range *record.Native.ExecutionState {
		if _, exists := values[value.KeyHex]; exists {
			return errors.New("native drain repeated execution-state custody key")
		}
		values[value.KeyHex] = value.ValueHex
	}
	registration, ok := values[self.registrationKey]
	if !ok || registration == nil {
		return errors.New("native drain has no observed present subnet registration")
	}
	raw, err := historicalReplayHex(*registration, 8)
	if err != nil || len(raw) != 8 || binary.LittleEndian.Uint64(raw) != *policy.SubnetRegistrationBlock {
		return errors.New("native drain original registration differs")
	}
	generation, ok := values[self.generationKey]
	if !ok {
		return errors.New("native drain omitted generation observation")
	}
	raw = self.generationFallback
	if generation != nil {
		raw, err = historicalReplayHex(*generation, 8)
	}
	if err != nil || len(raw) != 8 || binary.LittleEndian.Uint64(raw) != *policy.SubnetGeneration {
		return errors.New("native drain original generation differs")
	}
	return nil
}

// A foreign subnet is decoded before it is excluded. No caller-supplied UID,
// AccountId or later state reply substitutes for the actual original get/set.
func (self *nativeEpochStorageLayout) collect(record historicalReplayObservation) error {
	if record.Native == nil || record.Native.ExecutionPhaseHex == nil || *record.Native.ExecutionPhaseHex != "0x02" {
		return errors.New("native epoch census lacks original Initialization phase")
	}
	key, err := historicalReplayHex(record.KeyHex, 36)
	if err != nil {
		return err
	}
	switch record.Purpose {
	case "native-uid-census":
		if (len(key) != 34 && len(key) != 36) || !bytes.Equal(key[:32], self.keysPrefix) {
			return errors.New("native UID census changed its original Keys namespace")
		}
		if record.Operation == "next_key" {
			// Existing request-only iteration observations grant no returned-key
			// or absence credit. Completeness comes from the dense get census.
			if record.ValueHex != nil || record.StorageReturn != nil {
				return errors.New("native iteration request invented returned evidence")
			}
			return nil
		}
		if record.Operation != "get" || len(key) != 36 || record.StorageReturn == nil || !record.StorageReturn.Present || record.StorageReturn.ValueHex == nil || record.ValueHex != nil {
			return errors.New("native UID census omits an actual present Keys get")
		}
		value, err := historicalReplayHex(*record.StorageReturn.ValueHex, 32)
		if err != nil || len(value) != 32 {
			return errors.Join(errors.New("native UID get is not an exact AccountId"), err)
		}
		if binary.LittleEndian.Uint16(key[32:34]) != self.netuid {
			return nil
		}
		uid := binary.LittleEndian.Uint16(key[34:])
		if int(uid) >= rootCensusLimit || len(self.uidRecords) >= rootCensusLimit {
			return errors.New("native UID get exceeds the admitted census bound")
		}
		if _, exists := self.uidRecords[uid]; exists {
			return errors.New("native UID get repeats a registration")
		}
		self.uidRecords[uid] = record
	case "native-epoch-index":
		if record.Operation != "set" || len(key) != 34 || !bytes.Equal(key[:32], self.epochPrefix) || record.ValueHex == nil || record.StorageReturn != nil {
			return errors.New("native epoch index is not its exact original write")
		}
		value, err := historicalReplayHex(*record.ValueHex, 8)
		if err != nil || len(value) != 8 {
			return errors.Join(errors.New("native epoch write is not an exact u64"), err)
		}
		if binary.LittleEndian.Uint16(key[32:]) != self.netuid {
			return nil
		}
		if self.epochRecord != nil {
			return errors.New("native epoch counter was written more than once")
		}
		self.epochRecord = &record
	default:
		return errors.New("native epoch census has an unrelated purpose")
	}
	return nil
}

// The same original roster kernel serves the producer's provider selection
// and the independently admitted replay. Its order is UID order, not trie
// iteration order or AccountId/BTreeMap order.
func (self *nativeEpochStorageLayout) roster(epoch historicalReplayObservation, count int, drains []*historicalReplayObservation) ([]byte, []byte, []uint64, error) {
	fail := func(message string) ([]byte, []byte, []uint64, error) { return nil, nil, nil, errors.New(message) }
	if epoch.Operation != "host" || count < 0 || count > rootCensusLimit || len(self.uidRecords) != count || self.epochRecord == nil || self.epochRecord.Ordinal >= epoch.Ordinal || len(drains) != 3 {
		return fail("native epoch storage join is incomplete or out of order")
	}
	for index, drain := range drains {
		if drain == nil || drain.Ordinal >= self.epochRecord.Ordinal || index > 0 && drain.Ordinal <= drains[index-1].Ordinal {
			return fail("native original drain does not precede its epoch write")
		}
	}
	present, err := nativeCaptureUint(epoch, "mechanism-count-present", 1)
	if err != nil || present > 1 {
		return fail("native original mechanism-count option is malformed")
	}
	mechanisms := uint64(1)
	if present == 1 {
		mechanisms, err = nativeCaptureUint(epoch, "mechanism-count", 1)
		if err != nil {
			return fail("native original mechanism count is unavailable")
		}
	}
	if mechanisms != 1 {
		return fail("native original epoch exceeds the admitted single mechanism")
	}
	hotkeys, uids := make([]byte, 0, count*32), make([]byte, 0, count*2)
	ordinals := make([]uint64, count)
	seen := map[string]bool{}
	for index := 0; index < count; index++ {
		record, ok := self.uidRecords[uint16(index)]
		if !ok || record.Ordinal >= epoch.Ordinal || record.Ordinal <= self.epochRecord.Ordinal {
			return fail("native UID census is not complete and ordered before the epoch")
		}
		value, err := historicalReplayHex(*record.StorageReturn.ValueHex, 32)
		if err != nil || len(value) != 32 || seen[string(value)] {
			return fail("native UID census aliases an original hotkey")
		}
		seen[string(value)] = true
		key, err := historicalReplayHex(record.KeyHex, 36)
		if err != nil || len(key) != 36 {
			return fail("native original UID key disappeared")
		}
		uids = append(uids, key[34:]...)
		hotkeys = append(hotkeys, value...)
		ordinals[index] = record.Ordinal
	}
	return hotkeys, uids, ordinals, nil
}

func (self *nativeEpochStorageLayout) derive(epoch historicalReplayObservation, count int, total uint64, drains []*historicalReplayObservation) ([]byte, []byte, uint64, *nativeEpochInputProvenance, error) {
	hotkeys, uids, ordinals, err := self.roster(epoch, count, drains)
	if err != nil {
		return nil, nil, 0, nil, err
	}
	value, err := historicalReplayHex(*self.epochRecord.ValueHex, 8)
	if err != nil || len(value) != 8 {
		return nil, nil, 0, nil, errors.New("native original epoch write disappeared")
	}
	provenance := &nativeEpochInputProvenance{Schema: nativeEpochStorageLayoutSchema, EpochObservationOrdinal: epoch.Ordinal, TotalAlpha: fmt.Sprint(total), EpochWriteOrdinal: self.epochRecord.Ordinal, UidReadOrdinals: ordinals, DrainOrdinals: []uint64{drains[0].Ordinal, drains[1].Ordinal, drains[2].Ordinal}}
	return hotkeys, uids, binary.LittleEndian.Uint64(value), provenance, nil
}
