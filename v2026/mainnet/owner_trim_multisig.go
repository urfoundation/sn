// A native multisig subnet owner trims through ordered signatory steps. Each
// step is one exact outer extrinsic signed on one signatory's own Ledger: the
// first approval opens the operation, the final approval executes the nested
// trim from the owner account, and only the original depositor may cancel.
package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
	"golang.org/x/crypto/blake2b"
)

// Reviewed pallet_multisig indices. Preparation authenticates them in metadata.
const ownerTrimMultisigPallet = 13
const ownerTrimMultisigAsMulti = 1
const ownerTrimMultisigCancel = 3

// Steps are strictly sequential; retries of a later step stay bounded.
const ownerTrimMultisigStepLimit = 16

const ownerTrimMultisigResidual = "A native multisig owner trims only after a second signatory's separate approval. The first approval reserves the depositor's deposit and opens an operation that any other signatory can complete with the same call, outside this tool and at any later time, until it executes or the original depositor cancels it. Per-step admission does not fence that interval; each outcome is established only by canonical receipt and exact-block readback."

// The complete public signer set and threshold derive the owner account. One
// step names its outer signatory and operation and, after opening, the
// original timepoint. Device references stay on each signatory's computer.
type ownerTrimMultisig struct {
	AccountId       string             `json:"account_id"`
	Threshold       uint16             `json:"threshold"`
	Signatories     []string           `json:"signatories"`
	Signatory       string             `json:"signatory_account_id"`
	Operation       string             `json:"operation"`
	Timepoint       *treasuryTimepoint `json:"timepoint"`
	InnerCall       string             `json:"inner_call_scale"`
	CallHash        string             `json:"inner_call_hash"`
	MaxRefTime      uint64             `json:"max_ref_time"`
	MaxProofSize    uint64             `json:"max_proof_size"`
	DepositLimitRao uint64             `json:"deposit_limit_rao"`
}

// Copies never alias the caller's signer slice or timepoint.
func (self ownerTrimMultisig) clone() ownerTrimMultisig {
	self.Signatories = slices.Clone(self.Signatories)
	if self.Timepoint != nil {
		point := *self.Timepoint
		self.Timepoint = &point
	}
	return self
}

// The first approval opens; at threshold two the next approval executes.
func (self ownerTrimMultisig) kind() string {
	switch {
	case self.Operation == "cancel_as_multi":
		return "cancel"
	case self.Timepoint == nil:
		return "first"
	}
	return "final"
}

// Threshold two is the reviewed owner shape: the second approval executes. The
// sorted signer set must derive the subnet owner exactly as pallet_multisig does.
func (self ownerTrimMultisig) validate(owner string) error {
	if self.AccountId != owner || !rootCanonicalHash(owner) || self.Threshold != 2 || len(self.Signatories) < 2 || len(self.Signatories) > 100 {
		return errors.New("owner trim multisig requires the subnet owner account, threshold two and its complete signer set")
	}
	accounts := make([][32]byte, len(self.Signatories))
	named := false
	for index, signer := range self.Signatories {
		if !rootCanonicalHash(signer) {
			return errors.New("owner trim multisig signatories require canonical AccountId32")
		}
		raw, _ := hex.DecodeString(signer[2:])
		copy(accounts[index][:], raw)
		named = named || signer == self.Signatory
	}
	derived, err := crv4.DeriveNativeMultisigAccount(accounts, self.Threshold)
	if err != nil || "0x"+hex.EncodeToString(derived[:]) != self.AccountId {
		return errors.Join(errors.New("owner trim signer set and threshold do not derive the subnet owner multisig"), err)
	}
	if !named {
		return errors.New("owner trim outer signer is not a signatory of the owner multisig")
	}
	switch self.Operation {
	case "as_multi":
		if self.MaxRefTime == 0 || self.MaxProofSize == 0 || (self.Timepoint == nil) != (self.DepositLimitRao != 0) {
			return errors.New("owner trim multisig approval requires an inner weight bound and a deposit limit on the first approval only")
		}
	case "cancel_as_multi":
		if self.Timepoint == nil || self.MaxRefTime != 0 || self.MaxProofSize != 0 || self.DepositLimitRao != 0 {
			return errors.New("owner trim multisig cancellation requires the original timepoint and no weight or deposit")
		}
	default:
		return errors.New("owner trim multisig operation is unsupported")
	}
	if self.Timepoint != nil && self.Timepoint.Height == 0 {
		return errors.New("owner trim multisig timepoint is invalid")
	}
	return nil
}

// Exact pallet_multisig codec: threshold, the sorted other signatories, the
// optional or required timepoint, then the nested call (or its hash for a
// cancellation) and the inner weight bound.
func (self ownerTrimMultisig) outerCall(owner string, inner []byte) ([]byte, error) {
	if err := self.validate(owner); err != nil {
		return nil, err
	}
	if self.InnerCall != "0x"+hex.EncodeToString(inner) || self.CallHash != rootExtrinsicHash(inner) {
		return nil, errors.New("owner trim multisig inner call or call hash differs from the exact trim call")
	}
	index := byte(ownerTrimMultisigAsMulti)
	if self.Operation == "cancel_as_multi" {
		index = ownerTrimMultisigCancel
	}
	call := binary.LittleEndian.AppendUint16([]byte{ownerTrimMultisigPallet, index}, self.Threshold)
	call = append(call, rootCompact(uint64(len(self.Signatories)-1))...)
	for _, signer := range self.Signatories {
		if signer != self.Signatory {
			raw, _ := hex.DecodeString(signer[2:])
			call = append(call, raw...)
		}
	}
	if self.Operation == "as_multi" {
		if self.Timepoint == nil {
			call = append(call, 0)
		} else {
			call = append(call, 1)
		}
	}
	if self.Timepoint != nil {
		call = binary.LittleEndian.AppendUint32(call, self.Timepoint.Height)
		call = binary.LittleEndian.AppendUint32(call, self.Timepoint.Index)
	}
	if self.Operation == "cancel_as_multi" {
		hash := blake2b.Sum256(inner)
		return append(call, hash[:]...), nil
	}
	call = append(call, inner...)
	call = append(call, rootCompact(self.MaxRefTime)...)
	return append(call, rootCompact(self.MaxProofSize)...), nil
}

// Authenticate both multisig calls, the nested RuntimeCall carrying AdminUtils,
// pending storage and deposit constants. The deposit is base plus factor times
// threshold, as pallet_multisig reserves it from the depositor.
func ownerTrimMultisigProfile(metadata *types.Metadata, signatories int, threshold uint16) (uint64, error) {
	if metadata == nil || metadata.Version != 14 {
		return 0, errors.New("owner trim multisig requires authenticated metadata14")
	}
	if err := treasuryCallProfile(metadata, "Multisig", "as_multi", ownerTrimMultisigPallet, ownerTrimMultisigAsMulti,
		[]string{"threshold", "other_signatories", "maybe_timepoint", "call", "max_weight"},
		[]string{"u16", "accounts", "optional-timepoint", "runtime-call", "weight"}); err != nil {
		return 0, err
	}
	if err := treasuryCallProfile(metadata, "Multisig", "cancel_as_multi", ownerTrimMultisigPallet, ownerTrimMultisigCancel,
		[]string{"threshold", "other_signatories", "timepoint", "call_hash"}, []string{"u16", "accounts", "timepoint", "account"}); err != nil {
		return 0, err
	}
	if err := ownerTrimMultisigNestedProfile(metadata); err != nil {
		return 0, err
	}
	entries := 0
	constants := map[string]uint64{}
	for _, pallet := range metadata.AsMetadataV14.Pallets {
		if pallet.Name != "Multisig" {
			continue
		}
		if !pallet.HasStorage || pallet.Storage.Prefix != "Multisig" {
			return 0, errors.New("owner trim multisig storage prefix changed")
		}
		for _, entry := range pallet.Storage.Items {
			if entry.Name != "Multisigs" {
				continue
			}
			entries++
			if !entry.Modifier.IsOptional || entry.Modifier.IsDefault || !entry.Type.IsMap || len(entry.Type.AsMap.Hashers) != 2 || !entry.Type.AsMap.Hashers[0].IsTwox64Concat ||
				!entry.Type.AsMap.Hashers[1].IsBlake2_128Concat || !treasuryType(metadata, entry.Type.AsMap.Value, "pending", 0) {
				return 0, errors.New("owner trim pending multisig storage changed")
			}
			key := metadata.AsMetadataV14.EfficientLookup[entry.Type.AsMap.Key.Int64()]
			if key == nil || !key.Def.IsTuple || len(key.Def.Tuple) != 2 || !rootTypeMatches(metadata, key.Def.Tuple[0], "account", 0) || !rootTypeMatches(metadata, key.Def.Tuple[1], "account", 0) {
				return 0, errors.New("owner trim multisig storage key tuple changed")
			}
		}
		for _, constant := range pallet.Constants {
			name := string(constant.Name)
			if name != "DepositBase" && name != "DepositFactor" && name != "MaxSignatories" {
				continue
			}
			if _, exists := constants[name]; exists {
				return 0, errors.New("owner trim multisig duplicate deposit constant")
			}
			shape, width := "u64", 8
			if name == "MaxSignatories" {
				shape, width = "u32", 4
			}
			if !rootTypeMatches(metadata, constant.Type, shape, 0) || len(constant.Value) != width {
				return 0, errors.New("owner trim multisig deposit constant shape changed")
			}
			if width == 8 {
				constants[name] = binary.LittleEndian.Uint64(constant.Value)
			} else {
				constants[name] = uint64(binary.LittleEndian.Uint32(constant.Value))
			}
		}
	}
	if entries != 1 || len(constants) != 3 || threshold == 0 || signatories < 2 || uint64(signatories) > constants["MaxSignatories"] ||
		constants["DepositFactor"] > (math.MaxUint64-constants["DepositBase"])/uint64(threshold) {
		return 0, errors.New("owner trim multisig deposit, signer bound or pending storage profile is incomplete")
	}
	return constants["DepositBase"] + uint64(threshold)*constants["DepositFactor"], nil
}

// The as_multi call argument is a RuntimeCall whose AdminUtils variant carries
// the AdminUtils call enum at its own pallet index, so nested bytes are exact.
func ownerTrimMultisigNestedProfile(metadata *types.Metadata) error {
	var nested *types.Si1LookupTypeID
	var admin *types.PalletMetadataV14
	for index := range metadata.AsMetadataV14.Pallets {
		pallet := &metadata.AsMetadataV14.Pallets[index]
		switch pallet.Name {
		case "AdminUtils":
			admin = pallet
		case "Multisig":
			entry := metadata.AsMetadataV14.EfficientLookup[pallet.Calls.Type.Int64()]
			if !pallet.HasCalls || entry == nil || !entry.Def.IsVariant {
				return errors.New("owner trim multisig call variants are absent")
			}
			for _, variant := range entry.Def.Variant.Variants {
				if variant.Name == "as_multi" && len(variant.Fields) == 5 {
					call := variant.Fields[3].Type
					nested = &call
				}
			}
		}
	}
	if nested == nil || admin == nil || !admin.HasCalls {
		return errors.New("owner trim multisig nested call or AdminUtils calls are absent")
	}
	entry := metadata.AsMetadataV14.EfficientLookup[nested.Int64()]
	if entry == nil || !entry.Def.IsVariant {
		return errors.New("owner trim multisig nested call is not a RuntimeCall")
	}
	matched := 0
	for _, variant := range entry.Def.Variant.Variants {
		if variant.Name != "AdminUtils" {
			continue
		}
		matched++
		if variant.Index != admin.Index || len(variant.Fields) != 1 || variant.Fields[0].Type.Int64() != admin.Calls.Type.Int64() {
			return errors.New("owner trim nested AdminUtils call variant changed")
		}
	}
	if matched != 1 {
		return errors.New("owner trim nested AdminUtils call variant is absent or duplicated")
	}
	return nil
}

// pallet_multisig::Multisig carries the original timepoint, deposit, depositor
// and sorted approvals, all members of the owner signer set. No trailing bytes.
func decodeOwnerTrimMultisigPending(raw []byte, signatories []string) (*treasuryPending, error) {
	if len(raw) < 49 || len(raw) > 3300 {
		return nil, errors.New("owner trim pending multisig value length invalid")
	}
	pending := &treasuryPending{Timepoint: treasuryTimepoint{Height: binary.LittleEndian.Uint32(raw[:4]), Index: binary.LittleEndian.Uint32(raw[4:8])},
		Deposit: binary.LittleEndian.Uint64(raw[8:16]), Depositor: "0x" + hex.EncodeToString(raw[16:48])}
	reader := rootScaleReader{data: raw, offset: 48}
	count, err := reader.compact()
	if err != nil || count == 0 || count > uint64(len(signatories)) || len(raw)-reader.offset != int(count)*32 {
		return nil, errors.New("owner trim pending multisig approval count invalid")
	}
	previous := ""
	depositor := false
	for index := uint64(0); index < count; index++ {
		value, _ := reader.take(32)
		account := "0x" + hex.EncodeToString(value)
		if !slices.Contains(signatories, account) || account <= previous {
			return nil, errors.New("owner trim pending multisig approvals are foreign or unsorted")
		}
		pending.Approvals = append(pending.Approvals, account)
		previous = account
		depositor = depositor || account == pending.Depositor
	}
	if !depositor || pending.Timepoint.Height == 0 || pending.Deposit == 0 {
		return nil, errors.New("owner trim pending multisig depositor, timepoint or deposit absent")
	}
	return pending, nil
}

// Exact Multisig event of this outer extrinsic, signatory, owner and inner call
// hash. Outer success with an Err inner result is a failed trim, never success.
func decodeOwnerTrimMultisigDispatch(metadata *types.Metadata, eventsRaw []byte, bodyCount int, receipt rootActionReceipt, action ownerTrimAction) (treasuryDispatch, error) {
	var result treasuryDispatch
	step := action.Multisig
	if step == nil {
		return result, errors.New("owner trim multisig dispatch requires a multisig step")
	}
	events, err := nativeReceiptEvents(metadata, false)
	if err != nil {
		return result, err
	}
	found := 0
	err = walkNativeEventRecords(metadata, eventsRaw, bodyCount, events, rootBodyCountLimit, func(record nativeEventRecord) error {
		if record.extrinsicIndex == nil || *record.extrinsicIndex != receipt.ExtrinsicIndex || !strings.HasPrefix(record.event.name, "Multisig.") {
			return nil
		}
		found++
		fields := record.fields
		names := []string{"approving", "timepoint", "multisig", "call_hash"}
		shapes := []string{"account", "timepoint", "account", "account"}
		switch record.event.name {
		case "Multisig.NewMultisig":
			names, shapes = []string{"approving", "multisig", "call_hash"}, []string{"account", "account", "account"}
			result.Kind = "opened"
		case "Multisig.MultisigApproval":
			result.Kind = "approved"
		case "Multisig.MultisigExecuted":
			names, shapes = append(names, "result"), append(shapes, "dispatch-result")
			result.Kind = "executed"
		case "Multisig.MultisigCancelled":
			names[0] = "cancelling"
			result.Kind = "cancelled"
		default:
			return fmt.Errorf("%w: unrelated multisig event inside the owner trim outer call", errRpcIntegrity)
		}
		if len(fields) != len(names) || len(record.event.variant.Fields) != len(names) {
			return errors.New("owner trim multisig event fields changed")
		}
		for index, field := range record.event.variant.Fields {
			if !field.HasName || string(field.Name) != names[index] {
				return errors.New("owner trim multisig event names changed")
			}
			if shapes[index] == "dispatch-result" {
				entry := metadata.AsMetadataV14.EfficientLookup[field.Type.Int64()]
				if entry == nil || !entry.Def.IsVariant || len(entry.Def.Variant.Variants) != 2 {
					return errors.New("owner trim inner dispatch result changed")
				}
				ok, bad := entry.Def.Variant.Variants[0], entry.Def.Variant.Variants[1]
				if ok.Name != "Ok" || ok.Index != 0 || len(ok.Fields) != 1 || !rootSigningType(metadata, ok.Fields[0].Type, "unit", 0) || bad.Name != "Err" || bad.Index != 1 || len(bad.Fields) != 1 {
					return errors.New("owner trim inner dispatch result variants changed")
				}
			} else if !treasuryType(metadata, field.Type, shapes[index], 0) {
				return errors.New("owner trim multisig event wire shape changed")
			}
		}
		if "0x"+hex.EncodeToString(fields[0]) != step.Signatory {
			return errors.New("owner trim multisig event names another signatory")
		}
		position := 1
		if result.Kind != "opened" {
			point := treasuryTimepoint{Height: binary.LittleEndian.Uint32(fields[1][:4]), Index: binary.LittleEndian.Uint32(fields[1][4:])}
			result.Timepoint, position = &point, 2
			if step.Timepoint == nil || point != *step.Timepoint {
				return errors.New("owner trim multisig event changed the original timepoint")
			}
		}
		if "0x"+hex.EncodeToString(fields[position]) != step.AccountId || "0x"+hex.EncodeToString(fields[position+1]) != step.CallHash {
			return errors.New("owner trim multisig event owner account or inner call hash differs")
		}
		switch result.Kind {
		case "opened":
			if step.kind() != "first" || receipt.BlockNumber > math.MaxUint32 {
				return errors.New("owner trim multisig opening contradicts this step")
			}
			result.Timepoint = &treasuryTimepoint{Height: uint32(receipt.BlockNumber), Index: receipt.ExtrinsicIndex}
		case "executed":
			if step.kind() != "final" {
				return errors.New("owner trim multisig execution contradicts this step")
			}
			raw := fields[len(fields)-1]
			if len(raw) == 1 && raw[0] == 0 {
				result.InnerSuccess = true
			} else if len(raw) > 1 && raw[0] == 1 {
				result.InnerError = "scale:0x" + hex.EncodeToString(raw[1:])
			} else {
				return errors.New("owner trim inner dispatch result is malformed")
			}
		}
		if (result.Kind == "cancelled") != (step.kind() == "cancel") {
			return errors.New("owner trim multisig cancellation contradicts this step")
		}
		return nil
	})
	if err != nil {
		return treasuryDispatch{}, err
	}
	if receipt.Success && found != 1 || !receipt.Success && found != 0 {
		return treasuryDispatch{}, errors.New("owner trim outer dispatch and multisig event count disagree")
	}
	return result, nil
}

// Raw inclusion events and exact-block state after the outer extrinsic.
// Facts are re-derived from retained bytes on every journal load.
type ownerTrimMultisigEvidence struct {
	BodyCount     int                        `json:"body_count"`
	Events        string                     `json:"events_scale"`
	Dispatch      treasuryDispatch           `json:"multisig_dispatch"`
	Readback      *ownerTrimMultisigReadback `json:"inclusion_block_readback,omitempty"`
	ReadbackIssue string                     `json:"readback_issue,omitempty"`
}

// One block's pending operation row, subnet owner and capacity values.
type ownerTrimMultisigReadback struct {
	BlockNumber    uint64             `json:"block_number"`
	BlockHash      string             `json:"block_hash"`
	PendingKey     string             `json:"pending_storage_key"`
	PendingStorage *string            `json:"pending_storage"`
	Storage        []rootStorageValue `json:"subnet_storage"`
}

// Decoded only from retained public bytes; never accepted from a JSON field.
type ownerTrimMultisigFacts struct {
	Pending        *treasuryPending
	SubnetOwner    string
	MaxAllowedUids uint16
}

// The owner must be explicitly stored; capacity may use its metadata default.
func (self ownerTrimMultisigReadback) facts(action ownerTrimAction) (ownerTrimMultisigFacts, error) {
	var result ownerTrimMultisigFacts
	if action.Multisig == nil || self.BlockNumber == 0 || !rootCanonicalHash(self.BlockHash) || !strings.HasPrefix(self.PendingKey, "0x") || len(self.Storage) != 2 {
		return result, errors.New("owner trim multisig readback scope is incomplete")
	}
	if self.PendingStorage != nil {
		raw, err := rootReceiptHex(*self.PendingStorage, 4096)
		if err != nil {
			return result, err
		}
		if result.Pending, err = decodeOwnerTrimMultisigPending(raw, action.Multisig.Signatories); err != nil {
			return result, err
		}
	}
	capacity := false
	for _, value := range self.Storage {
		data, err := rootReceiptHex(value.EffectiveScale, 32)
		if err != nil {
			return result, err
		}
		if value.RawStorage != nil {
			stored, err := hex.DecodeString(strings.TrimPrefix(*value.RawStorage, "0x"))
			if err != nil || !strings.HasPrefix(*value.RawStorage, "0x") || hex.EncodeToString(stored) != hex.EncodeToString(data) {
				return result, errors.New("owner trim multisig readback storage differs from its effective value")
			}
		}
		switch {
		case value.Name == "SubnetOwner" && result.SubnetOwner == "" && value.RawStorage != nil && len(data) == 32:
			result.SubnetOwner = value.EffectiveScale
		case value.Name == "MaxAllowedUids" && !capacity && len(data) == 2:
			result.MaxAllowedUids, capacity = binary.LittleEndian.Uint16(data), true
		default:
			return result, errors.New("owner trim multisig readback contains unexpected or absent subnet storage")
		}
	}
	if result.SubnetOwner == "" || !capacity {
		return result, errors.New("owner trim multisig readback lacks the subnet owner or capacity")
	}
	return result, nil
}

// Retained inclusion evidence must match its receipt, operation and block.
func (self ownerTrimActionReconciliation) validateMultisig(action ownerTrimAction) error {
	evidence, receipt := self.Multisig, self.Receipt
	if action.Multisig == nil {
		if evidence != nil {
			return errors.New("owner trim direct action cannot retain multisig evidence")
		}
		return nil
	}
	if action.Multisig.kind() != "final" && self.Census != nil {
		return errors.New("owner trim multisig approval or cancellation cannot claim trim census correspondence")
	}
	if receipt == nil {
		if evidence != nil {
			return errors.New("owner trim multisig evidence lacks its exact inclusion")
		}
		return nil
	}
	if evidence == nil || evidence.BodyCount <= int(receipt.ExtrinsicIndex) || evidence.BodyCount > rootBodyCountLimit {
		return errors.New("owner trim multisig inclusion lacks its original events")
	}
	events, err := rootReceiptHex(evidence.Events, rootBodyBytesLimit)
	if err != nil || rootExtrinsicHash(events) != receipt.EventHash {
		return errors.Join(errors.New("owner trim multisig events differ from the retained receipt"), err)
	}
	dispatch := evidence.Dispatch
	if !receipt.Success {
		if dispatch.Kind != "" || dispatch.Timepoint != nil || dispatch.InnerSuccess || dispatch.InnerError != "" || evidence.Readback != nil || evidence.ReadbackIssue != "" {
			return errors.New("owner trim failed outer dispatch cannot retain multisig outcomes")
		}
		return nil
	}
	expected := action.Multisig.Timepoint
	if dispatch.Kind == "opened" {
		expected = &treasuryTimepoint{Height: uint32(receipt.BlockNumber), Index: receipt.ExtrinsicIndex}
	}
	if expected == nil || dispatch.Timepoint == nil || *dispatch.Timepoint != *expected ||
		dispatch.Kind != "executed" && (dispatch.InnerSuccess || dispatch.InnerError != "") || dispatch.Kind == "executed" && dispatch.InnerSuccess != (dispatch.InnerError == "") ||
		dispatch.Kind != "opened" && dispatch.Kind != "approved" && dispatch.Kind != "executed" && dispatch.Kind != "cancelled" {
		return errors.New("owner trim multisig dispatch contradicts its receipt or original timepoint")
	}
	if (evidence.Readback == nil) == (evidence.ReadbackIssue == "") {
		return errors.New("owner trim multisig inclusion requires readback or an explicit gap")
	}
	if readback := evidence.Readback; readback != nil {
		if readback.BlockNumber != receipt.BlockNumber || readback.BlockHash != receipt.BlockHash {
			return errors.New("owner trim multisig readback is not the exact inclusion block")
		}
		if _, err := readback.facts(action); err != nil {
			return err
		}
	}
	return nil
}

// Native outcomes classify each step. Exceeding the local fee reserve is
// reported separately: an opened approval must stay completable or cancellable.
func ownerTrimMultisigPhase(action ownerTrimAction, evidence ownerTrimActionReconciliation, signed bool) string {
	step := action.Multisig
	if receipt := evidence.Receipt; receipt != nil {
		if receipt.ExecutionRuntimeVersion != action.Runtime.RuntimeVersion || receipt.ExecutionCodeHash != action.Runtime.RuntimeCodeHash || receipt.ExecutionMetadataHash != action.Runtime.RuntimeMetadataHash {
			return "runtime-deviation"
		}
		if !receipt.Success {
			return "outer-dispatch-failed"
		}
		outcome := evidence.Multisig
		if outcome == nil || step == nil {
			return "finalized-state-conflict"
		}
		dispatch := outcome.Dispatch
		switch {
		case step.kind() == "first" && dispatch.Kind == "opened", step.kind() == "cancel" && dispatch.Kind == "cancelled":
		case step.kind() == "final" && dispatch.Kind == "executed":
			if !dispatch.InnerSuccess {
				return "inner-dispatch-failed"
			}
		default:
			return "finalized-state-conflict"
		}
		if outcome.Readback == nil {
			return "finalized-readback-pending"
		}
		facts, err := outcome.Readback.facts(action)
		if err != nil || facts.SubnetOwner != step.AccountId {
			return "finalized-state-conflict"
		}
		switch step.kind() {
		case "first":
			pending := facts.Pending
			if pending == nil || dispatch.Timepoint == nil || pending.Timepoint != *dispatch.Timepoint || pending.Depositor != step.Signatory ||
				!slices.Equal(pending.Approvals, []string{step.Signatory}) || pending.Deposit > step.DepositLimitRao {
				return "finalized-state-conflict"
			}
			return "approval-recorded"
		case "final":
			if facts.Pending != nil || facts.MaxAllowedUids != action.MaximumUids {
				return "finalized-state-conflict"
			}
			return "executed"
		}
		if facts.Pending != nil {
			return "finalized-state-conflict"
		}
		return "cancelled"
	}
	o := evidence.Observation
	if o.FinalizedNumber >= action.BirthBlock+action.Period && o.AccountNonce != nil {
		if *o.AccountNonce == action.Nonce {
			if !signed {
				return "expired-unsigned"
			}
			return "expired"
		}
		if *o.AccountNonce > action.Nonce {
			return "nonce-conflict"
		}
	}
	return ""
}

// After these phases a step can make no further native effect.
func ownerTrimMultisigSettled(phase string) bool {
	switch phase {
	case "approval-recorded", "executed", "inner-dispatch-failed", "cancelled", "outer-dispatch-failed", "finalized-state-conflict",
		"runtime-deviation", "expired", "expired-unsigned", "nonce-conflict":
		return true
	}
	return false
}

// Every outer extrinsic of one operation, in order. Only the last step may be
// unsettled; a later step requires the original open operation and timepoint.
type ownerTrimMultisigRecord struct {
	Steps []ownerTrimRecord `json:"steps"`
}

// Steps share the anchor's approved operation: owner, signer set, inner call and
// hash, preparation, review, runtime, Ledger metadata and route. Only a step's
// signatory, operation, timepoint, envelope and bounds may differ.
func ownerTrimMultisigOperation(config ownerTrimExecutionConfig) string {
	if config.Action.Multisig == nil {
		return ""
	}
	config.Signature = ""
	action := config.Action
	multisig := action.Multisig.clone()
	multisig.Signatory, multisig.Operation, multisig.Timepoint = "", "", nil
	multisig.MaxRefTime, multisig.MaxProofSize, multisig.DepositLimitRao = 0, 0, 0
	action.Multisig = &multisig
	action.Nonce, action.BirthBlock, action.BirthHash, action.Period, action.FeeReserveRao, action.MaxBroadcasts, action.DerivationPath = 0, 0, "", 0, 0, 0, ""
	action.Call, action.Payload, action.RequestHash = "", "", ""
	config.Action = action
	return rootObjectHash(config)
}

// Validated sequence state. A new step may be planned only while the original
// operation is open and every retained step is settled.
type ownerTrimMultisigState struct {
	Timepoint *treasuryTimepoint
	Open      bool
	Settled   bool
	Closed    string
}

// Every step is an independently approved direct record of the same operation.
func ownerTrimMultisigSequence(anchor ownerTrimExecutionConfig, key string, steps []ownerTrimRecord) (ownerTrimMultisigState, error) {
	var state ownerTrimMultisigState
	if anchor.Action.Multisig == nil || anchor.Action.Multisig.kind() != "first" || len(steps) == 0 || len(steps) > ownerTrimMultisigStepLimit {
		return state, errors.New("owner trim multisig journal step sequence is invalid")
	}
	operation := ownerTrimMultisigOperation(anchor)
	depositor := anchor.Action.Multisig.Signatory
	for index, step := range steps {
		if step.Schema != ownerTrimRecordSchema || step.ApprovalKey != key || step.Multisig != nil || step.Phase == "multisig" || step.Phase == "signing" {
			return state, errors.New("owner trim multisig step is not a direct independently approved signatory record")
		}
		if err := step.validate(step.Config, key); err != nil {
			return state, err
		}
		multisig := step.Config.Action.Multisig
		if index == 0 && rootObjectHash(step.Config) != rootObjectHash(anchor) || index != 0 && (!state.Open || !state.Settled ||
			ownerTrimMultisigOperation(step.Config) != operation || multisig.kind() == "first" || multisig.Timepoint == nil || *multisig.Timepoint != *state.Timepoint ||
			(multisig.kind() == "cancel") != (multisig.Signatory == depositor)) {
			return state, errors.New("owner trim multisig step lacks the original operation, open timepoint or signatory role")
		}
		state.Settled = ownerTrimMultisigSettled(step.Phase)
		switch step.Phase {
		case "approval-recorded":
			point := *step.Reconciliation.Multisig.Dispatch.Timepoint
			state.Open, state.Timepoint = true, &point
		case "executed", "inner-dispatch-failed", "cancelled", "finalized-state-conflict", "runtime-deviation":
			state.Open, state.Closed = false, step.Phase
		}
		if index == 0 && state.Settled && step.Phase != "approval-recorded" {
			state.Open, state.Closed = false, step.Phase
		}
	}
	return state, nil
}

// The anchor keeps no direct progress; ordered steps hold every effect.
func (self ownerTrimRecord) validateMultisig(config ownerTrimExecutionConfig, key string) error {
	if config.Action.Multisig == nil || self.Phase != "multisig" || self.Multisig == nil || self.Signature != "" || self.RawExtrinsic != "" || self.ExtrinsicHash != "" ||
		self.Broadcasts != 0 || self.LastFinalized != 0 || self.LastFinalizedHash != "" || self.Reconciliation != nil || self.Submission != nil {
		return errors.New("owner trim multisig journal keeps all progress in its ordered signatory steps")
	}
	_, err := ownerTrimMultisigSequence(config, key, self.Multisig.Steps)
	return err
}

// An unclaimed first step is implicit only in the original reserved anchor.
func ownerTrimMultisigSteps(record ownerTrimRecord) ([]ownerTrimRecord, error) {
	if record.Multisig != nil {
		return slices.Clone(record.Multisig.Steps), nil
	}
	if record.Phase != "reserved" || record.Signature != "" || record.Reconciliation != nil || record.Submission != nil || record.Broadcasts != 0 {
		return nil, errors.New("owner trim multisig anchor has direct progress outside its ordered steps")
	}
	first := ownerTrimRecord{Schema: ownerTrimRecordSchema, Config: record.Config, ApprovalKey: record.ApprovalKey, Phase: "reserved"}
	first.ContentHash = rootObjectHash(first)
	return []ownerTrimRecord{first}, nil
}
