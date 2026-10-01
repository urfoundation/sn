// Reviews one proposed native incentive row without authorizing a submission.
// The source profile describes a pinned implementation, not a live runtime.
package validator

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

const ownerRecycleProposalSchema = "urnetwork-owner-recycle-proposal-v1"
const ownerRecycleSourceCommit = "67dcf7f791dc495064c293f080a0702cb433e51e"
const ownerRecycleSourceCommit470 = "923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d"

// Bounds preview work to the whole u16 uid domain. OwnedHotkeys can span more
// than one subnet; this is a planner resource limit, not a runtime storage cap.
const maximumOwnerRecycleCensusEntries = 65536

// Independently reviewed pins must eventually be matched by authenticated reads.
// A runtime number alone never selects or approves this source profile.
type OwnerRecycleRuntimePin struct {
	GenesisHash  [32]byte
	Netuid       uint16
	Version      crv4.RuntimeVersionIdentity
	CodeHash     [32]byte
	MetadataHash [32]byte
	SourceCommit string
}

// A separate draft binds the whole existing policy without changing its hash or
// reinterpreting theta. The existing release configuration cannot activate it.
type OwnerRecycleProposal struct {
	Schema           string
	ParentPolicyHash [32]byte
	PolicyId         uint64
	EffectiveEpoch   uint64
	ProviderShare    protocol.Rational
	Remainder        string
	OwnerAllocation  string
	Runtime          OwnerRecycleRuntimePin
}

// Validates the proposed successor without weakening any parent-policy field.
func (self OwnerRecycleProposal) Validate(parent protocol.Policy) error {
	parentHash, err := parent.Hash()
	if err != nil {
		return err
	}
	if self.Schema != ownerRecycleProposalSchema || parent.NetworkProfile != "mainnet" || self.ParentPolicyHash != parentHash {
		return errors.New("owner-recycle requires its exact schema and unchanged mainnet parent policy hash")
	}
	if self.PolicyId <= parent.PolicyID || self.EffectiveEpoch <= parent.EffectiveEpoch {
		return errors.New("owner-recycle successor policy and epoch must advance")
	}
	if self.ProviderShare != (protocol.Rational{Numerator: 1, Denominator: 10}) || self.Remainder != "recognized_owner_recycle" || self.OwnerAllocation != "equal_unmasked_registered" {
		return errors.New("owner-recycle supports exactly the reviewed 10/90 proposal and equal owner allocation")
	}
	if self.Runtime.GenesisHash == ([32]byte{}) || self.Runtime.CodeHash == ([32]byte{}) || self.Runtime.MetadataHash == ([32]byte{}) || self.Runtime.Netuid != 25 ||
		self.Runtime.SourceCommit != ownerRecycleSourceCommit && self.Runtime.SourceCommit != ownerRecycleSourceCommit470 {
		return errors.New("owner-recycle requires nonzero network/artifact pins, netuid 25 and the exact reviewed source profile")
	}
	version := self.Runtime.Version
	if strings.TrimSpace(version.SpecName) == "" || version.SpecVersion == 0 || version.TransactionVersion == 0 || version.StateVersion == 0 {
		return errors.New("owner-recycle requires a complete runtime identity")
	}
	return nil
}

// The digest identifies draft bytes; it is neither a signature nor an approval.
func (self OwnerRecycleProposal) Hash(parent protocol.Policy) ([32]byte, error) {
	if err := self.Validate(parent); err != nil {
		return [32]byte{}, err
	}
	encoded, err := json.Marshal(self)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

// Registration and reverse hotkey lookup must agree at the same finalized hash.
// Preview input carries their claimed result; it does not authenticate storage.
type OwnerRecycleRegistration struct {
	Uid               uint16
	Hotkey            [32]byte
	RegistrationBlock uint64
}

// One declared finalized census follows the pinned source's owner recognition:
// registered OwnedHotkeys[SubnetOwner] plus registered SubnetOwnerHotkey.
// Live validators and healthy operators are claims, not independence proofs.
type OwnerRecycleSnapshot struct {
	Runtime           OwnerRecycleRuntimePin
	FinalizedHash     [32]byte
	FinalizedNumber   uint64
	MechanismCount    uint16
	RecycleModeScale  []byte
	SubnetOwner       [32]byte
	OwnedHotkeys      [][32]byte
	SubnetOwnerHotkey *[32]byte
	Registrations     []OwnerRecycleRegistration
	LiveValidatorUids []uint16
	HealthyOperators  [][32]byte
}

// Explicit hotkeys prevent a provider score from being silently reassigned by
// a stale or ambiguous uid mapping. Head and tail recipients must be disjoint.
type OwnerRecycleProvider struct {
	Uid    uint16
	Hotkey [32]byte
	Score  *big.Rat
}

// All inputs are borrowed for the synchronous call and remain unchanged.
// Self masking is mandatory even when the runtime allows an owner exception.
type OwnerRecyclePreviewInput struct {
	ParentPolicy protocol.Policy
	Proposal     OwnerRecycleProposal
	Snapshot     OwnerRecycleSnapshot
	SelfUid      uint16
	MaskedUids   map[uint16]bool
	Pools        []OwnerRecycleProvider
	Head         []OwnerRecycleProvider
}

// Owns its exact and quantized rows. WireProviderShare is the fraction of this
// validator's integer row, before runtime masks, Yuma or emission accounting.
// It is not a share of final miner allocation, a cap or a deferred entitlement.
type OwnerRecyclePreview struct {
	ProposalHash       [32]byte
	FinalizedHash      [32]byte
	Uids               []uint16
	Scores             []*big.Rat
	WireValues         []uint16
	OwnerUids          []uint16
	MaskedOwnerUids    []uint16
	WireProviderShare  *big.Rat
	ProviderShareError *big.Rat
}

// No preview, including a manually constructed zero value, can authorize a
// commit. A future authenticated adapter and signed successor need review.
func (self *OwnerRecyclePreview) AdmissionError() error {
	return errors.New("owner-recycle preview only: missing authenticated finalized owner/uid census and Recycle mode; matching runtime source-to-code review; signed successor activation and measured independent-validator admission; final Yuma/native-emission reconciliation")
}

// Builds the 10/90 rational row and production max-upscaled u16 candidate.
// Cap clipping or integer repair could change channel budgets, so this preview
// refuses either requirement instead of redistributing between recipients.
func PreviewOwnerRecycle(input OwnerRecyclePreviewInput) (*OwnerRecyclePreview, error) {
	proposalHash, err := input.Proposal.Hash(input.ParentPolicy)
	if err != nil {
		return nil, err
	}
	snapshot := input.Snapshot
	if snapshot.Runtime != input.Proposal.Runtime || snapshot.FinalizedHash == ([32]byte{}) || snapshot.FinalizedNumber == 0 {
		return nil, errors.New("owner-recycle snapshot does not match the declared finalized runtime pins")
	}
	if len(snapshot.Registrations) > maximumOwnerRecycleCensusEntries || len(snapshot.OwnedHotkeys) > maximumOwnerRecycleCensusEntries || len(snapshot.LiveValidatorUids) > maximumOwnerRecycleCensusEntries || len(snapshot.HealthyOperators) > maximumOwnerRecycleCensusEntries || len(input.Pools) > maximumOwnerRecycleCensusEntries || len(input.Head) > maximumOwnerRecycleCensusEntries-len(input.Pools) {
		return nil, errors.New("owner-recycle census exceeds the supported u16-domain planning bound")
	}
	if snapshot.MechanismCount != 1 || len(input.Head) > int(input.ParentPolicy.Steering.MaximumHeadFleets) {
		return nil, errors.New("owner-recycle retains one mechanism and the parent maximum head-fleet count")
	}
	if len(snapshot.RecycleModeScale) != 1 || snapshot.RecycleModeScale[0] != 1 {
		return nil, errors.New("owner-recycle requires explicit Recycle; absent storage defaults to Burn")
	}
	if snapshot.SubnetOwner == ([32]byte{}) || len(snapshot.Registrations) == 0 {
		return nil, errors.New("owner-recycle requires an owner and a bounded registration census")
	}
	registrationUidKVs := map[uint16]OwnerRecycleRegistration{}
	registrationHotkeyKVs := map[[32]byte]OwnerRecycleRegistration{}
	for _, registration := range snapshot.Registrations {
		_, duplicateUid := registrationUidKVs[registration.Uid]
		_, duplicateHotkey := registrationHotkeyKVs[registration.Hotkey]
		if duplicateUid || duplicateHotkey || registration.Hotkey == ([32]byte{}) || registration.RegistrationBlock > snapshot.FinalizedNumber {
			return nil, errors.New("owner-recycle registration census is ambiguous or newer than its finalized hash")
		}
		registrationUidKVs[registration.Uid] = registration
		registrationHotkeyKVs[registration.Hotkey] = registration
	}
	if _, ok := registrationUidKVs[input.SelfUid]; !ok {
		return nil, errors.New("owner-recycle validator is not registered")
	}
	liveValidatorUidKVs := map[uint16]bool{}
	for _, uid := range snapshot.LiveValidatorUids {
		if _, ok := registrationUidKVs[uid]; !ok || liveValidatorUidKVs[uid] {
			return nil, errors.New("owner-recycle live validator census is missing or duplicated")
		}
		liveValidatorUidKVs[uid] = true
	}
	if !liveValidatorUidKVs[input.SelfUid] || len(liveValidatorUidKVs) < int(input.ParentPolicy.Safety.MinimumLiveValidatorCount) {
		return nil, errors.New("owner-recycle cannot weaken the parent live-validator minimum")
	}
	healthyOperatorKVs := map[[32]byte]bool{}
	for _, operator := range snapshot.HealthyOperators {
		if operator == ([32]byte{}) || healthyOperatorKVs[operator] {
			return nil, errors.New("owner-recycle healthy operator census is missing or duplicated")
		}
		healthyOperatorKVs[operator] = true
	}
	if len(healthyOperatorKVs) < int(input.ParentPolicy.Safety.MinimumHealthyNOCount) {
		return nil, errors.New("owner-recycle cannot weaken the parent healthy-operator minimum")
	}

	ownerHotkeyKVs := map[[32]byte]bool{}
	ownerRegistrations := []OwnerRecycleRegistration{}
	for _, hotkey := range snapshot.OwnedHotkeys {
		if hotkey == ([32]byte{}) || ownerHotkeyKVs[hotkey] {
			return nil, errors.New("owner-recycle owned hotkey census is zero or duplicated")
		}
		ownerHotkeyKVs[hotkey] = true
		if registration, ok := registrationHotkeyKVs[hotkey]; ok {
			ownerRegistrations = append(ownerRegistrations, registration)
		}
	}
	sort.Slice(ownerRegistrations, func(i, j int) bool {
		if ownerRegistrations[i].RegistrationBlock != ownerRegistrations[j].RegistrationBlock {
			return ownerRegistrations[i].RegistrationBlock > ownerRegistrations[j].RegistrationBlock
		}
		return ownerRegistrations[i].Uid < ownerRegistrations[j].Uid
	})
	if snapshot.SubnetOwnerHotkey != nil {
		hotkey := *snapshot.SubnetOwnerHotkey
		if hotkey == ([32]byte{}) {
			return nil, errors.New("owner-recycle subnet owner hotkey is zero")
		}
		if registration, ok := registrationHotkeyKVs[hotkey]; ok && !ownerHotkeyKVs[hotkey] {
			ownerRegistrations = append([]OwnerRecycleRegistration{registration}, ownerRegistrations...)
		}
	}
	ownerUidKVs := map[uint16]bool{}
	maskedUidKVs := map[uint16]bool{input.SelfUid: true}
	for uid, masked := range input.MaskedUids {
		if masked {
			maskedUidKVs[uid] = true
		}
	}
	preview := &OwnerRecyclePreview{ProposalHash: proposalHash, FinalizedHash: snapshot.FinalizedHash}
	for _, registration := range ownerRegistrations {
		ownerUidKVs[registration.Uid] = true
		if maskedUidKVs[registration.Uid] {
			preview.MaskedOwnerUids = append(preview.MaskedOwnerUids, registration.Uid)
		} else {
			preview.OwnerUids = append(preview.OwnerUids, registration.Uid)
		}
	}
	if len(preview.OwnerUids) == 0 {
		return nil, errors.New("owner-recycle has no unmasked runtime-recognized registered owner uid")
	}

	providerUidKVs := map[uint16]bool{}
	convert := func(providers []OwnerRecycleProvider) ([]ExactWeightInput, error) {
		converted := make([]ExactWeightInput, 0, len(providers))
		for _, provider := range providers {
			registration, ok := registrationUidKVs[provider.Uid]
			if !ok || registration.Hotkey != provider.Hotkey || ownerUidKVs[provider.Uid] || providerUidKVs[provider.Uid] {
				return nil, errors.New("owner-recycle provider is unregistered, stale, duplicated or overlaps an owner")
			}
			providerUidKVs[provider.Uid] = true
			converted = append(converted, ExactWeightInput{UID: provider.Uid, Score: provider.Score})
		}
		return converted, nil
	}
	pools, err := convert(input.Pools)
	if err != nil {
		return nil, err
	}
	head, err := convert(input.Head)
	if err != nil {
		return nil, err
	}
	uids, scores, err := BuildWeightVectorExact(pools, head, input.ParentPolicy.Steering.Theta, maskedUidKVs)
	if err != nil {
		return nil, fmt.Errorf("owner-recycle provider allocation unavailable; no owner-only or zero-incentive fallback: %w", err)
	}
	if err := completeOwnerRecycleRow(preview, input.ParentPolicy, uids, scores, ownerUidKVs); err != nil {
		return nil, err
	}
	return preview, nil
}

// Shared arithmetic consumes an already reconstructed normalized provider row.
// Callers separately authenticate owner recognition, masks and provider identity;
// this helper grants no validator eligibility or activation authority.
func completeOwnerRecycleRow(preview *OwnerRecyclePreview, parent protocol.Policy, uids []uint16, scores []*big.Rat, ownerUidKVs map[uint16]bool) error {
	if preview == nil || len(preview.OwnerUids) == 0 || len(uids) == 0 || len(uids) != len(scores) {
		return errors.New("owner-recycle row lacks providers or usable owner destinations")
	}
	total := new(big.Rat)
	for index, uid := range uids {
		if scores[index] == nil || scores[index].Sign() <= 0 || index > 0 && uid <= uids[index-1] || ownerUidKVs[uid] {
			return errors.New("owner-recycle provider row is not positive, ordered and separate from owners")
		}
		total.Add(total, scores[index])
	}
	if total.Cmp(big.NewRat(1, 1)) != 0 {
		return errors.New("owner-recycle provider row is not exactly normalized")
	}
	scoreUidKVs := map[uint16]*big.Rat{}
	for i, uid := range uids {
		scoreUidKVs[uid] = new(big.Rat).Mul(scores[i], big.NewRat(1, 10))
	}
	ownerShare := big.NewRat(9, 10*int64(len(preview.OwnerUids)))
	for _, uid := range preview.OwnerUids {
		scoreUidKVs[uid] = new(big.Rat).Set(ownerShare)
	}
	for uid := range scoreUidKVs {
		preview.Uids = append(preview.Uids, uid)
	}
	sort.Slice(preview.Uids, func(i, j int) bool { return preview.Uids[i] < preview.Uids[j] })
	capShare := big.NewRat(int64(parent.Steering.MaxWeightLimitU16), crv4.U16Max)
	for _, uid := range preview.Uids {
		score := scoreUidKVs[uid]
		if score.Cmp(capShare) > 0 {
			return fmt.Errorf("owner-recycle uid %d exceeds the unchanged signed weight cap; clipping would change the proposal", uid)
		}
		preview.Scores = append(preview.Scores, score)
	}
	wireUids, wireValues, err := crv4.NormalizeRationalToU16(preview.Uids, preview.Scores)
	if err != nil {
		return err
	}
	if len(wireUids) != len(preview.Uids) {
		return errors.New("owner-recycle quantization loses a positive recipient")
	}
	var wireSum, providerSum uint64
	for i, uid := range wireUids {
		wireSum += uint64(wireValues[i])
		if !ownerUidKVs[uid] {
			providerSum += uint64(wireValues[i])
		}
	}
	for _, value := range wireValues {
		if uint64(value)*crv4.U16Max > wireSum*uint64(parent.Steering.MaxWeightLimitU16) {
			return errors.New("owner-recycle quantized row exceeds signed cap; integer repair would change the proposal")
		}
	}
	preview.WireValues = wireValues
	preview.WireProviderShare = new(big.Rat).SetFrac(new(big.Int).SetUint64(providerSum), new(big.Int).SetUint64(wireSum))
	preview.ProviderShareError = new(big.Rat).Sub(preview.WireProviderShare, big.NewRat(1, 10))
	return nil
}
