// Reads the runtime-recognized owner census through an independently approved
// exact artifact. No caller-declared snapshot can substitute for these reads.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Reports only authenticated approval and owner/mode preconditions. Validators,
// providers, economics and the successor's source/intent history remain gates.
type OwnerRecycleAdmissionObservation struct {
	ApprovalHash              string                     `json:"approval_hash"`
	ProposalHash              [32]byte                   `json:"proposal_hash"`
	ConfigHash                [32]byte                   `json:"config_hash"`
	NativeEpoch               uint64                     `json:"native_epoch"`
	FirstNativeEpoch          uint64                     `json:"first_native_epoch"`
	Snapshot                  OwnerRecycleSnapshot       `json:"snapshot"`
	RecognizedOwners          []OwnerRecycleRegistration `json:"recognized_owners"`
	TreasuryRecipients        []TreasuryRecipient        `json:"treasury_recipients,omitempty"`
	MinimumAllowedWeights     uint16                     `json:"minimum_allowed_weights"`
	StoredMaximumWeightLimit  uint16                     `json:"stored_maximum_weight_limit"`
	RuntimeMaximumWeightLimit uint16                     `json:"runtime_maximum_weight_limit"`
	SignedMaximumWeightLimit  uint16                     `json:"signed_maximum_weight_limit"`
	ApprovalAuthenticated     bool                       `json:"approval_authenticated"`
	OwnerCensusAuthenticated  bool                       `json:"owner_census_authenticated"`
	RecycleModeAuthenticated  bool                       `json:"recycle_mode_authenticated"`
	ActivationReady           bool                       `json:"activation_ready"`
	NativeOutcomeVerified     bool                       `json:"native_outcome_verified"`
	Blockers                  []string                   `json:"activation_blockers"`
}

// The actual steerer exposes the same read-only admission owner as tooling.
// It neither uses the hotkey signer nor advances measurement/intent state.
func (self *ReleaseSteerer) ObserveOwnerRecycleAdmission(ctx context.Context) (*OwnerRecycleAdmissionObservation, error) {
	if self == nil {
		return nil, errors.New("owner-recycle steerer is absent")
	}
	return ObserveOwnerRecycleAdmission(ctx, self.cfg, self.native)
}

// Requires previously retained signed approval, current mainnet identity and
// one finalized artifact. All storage queries and the closing canonical check
// use that exact hash, under a finite operation deadline. RPC finality is a
// provider assertion; this read is not an independent state proof or payout.
// Configuration and connection are borrowed and must remain immutable during
// the call. The reader never rebinds the connection's signing metadata/runtime.
func ObserveOwnerRecycleAdmission(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain) (*OwnerRecycleAdmissionObservation, error) {
	return observeOwnerRecycleAdmissionAt(ctx, cfg, native, types.Hash{})
}

// Replays the original census at an explicit canonical block at or below the
// current finalized head. Neither a later head nor a changed runtime relabels
// the retained approval. Finality/ancestry still rely on the approved RPC.
func ObserveOwnerRecycleAdmissionAt(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain, blockHash [32]byte) (*OwnerRecycleAdmissionObservation, error) {
	if blockHash == ([32]byte{}) {
		return nil, errors.New("owner-recycle historical census requires an exact block hash")
	}
	return observeOwnerRecycleAdmissionAt(ctx, cfg, native, types.Hash(blockHash))
}

// Current admission and exact historical replay share the same bounded reader.
func observeOwnerRecycleAdmissionAt(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain, requested types.Hash) (*OwnerRecycleAdmissionObservation, error) {
	if ctx == nil || native == nil || native.API == nil || native.API.Client == nil || native.ProvisionalRuntimeCompatibilityEnabled() {
		return nil, errors.New("owner-recycle observation needs a non-provisional native connection and caller context")
	}
	envelope, err := readRetainedOwnerRecycleApproval(ctx, cfg)
	if err != nil {
		return nil, err
	}
	routeApproved := false
	for _, endpoint := range cfg.Substrate {
		if native.API.Client.URL() == endpoint {
			routeApproved = true
			break
		}
	}
	if !routeApproved {
		return nil, errors.New("owner-recycle connection is not an explicitly approved native route")
	}
	approval := envelope.Approval
	pin := approval.Proposal.Runtime
	operationCtx, cancel := context.WithTimeout(ctx, productionSteeringReadTimeout)
	defer cancel()
	operationCtx = withRuntimeFinalityOwner(operationCtx)
	var finality runtimeFinalityObservation
	return crv4.ReadRuntimeObservationContext(operationCtx, native, func(ctx context.Context) (*OwnerRecycleAdmissionObservation, error) {
		call := native.API.Client.CallContext
		if _, err := readRuntimeNetworkIdentity(ctx, native, approval.NativeChain, types.Hash(pin.GenesisHash)); err != nil {
			return nil, err
		}
		head, err := readRuntimeFinalityWitness(ctx, native)
		if err != nil {
			return nil, err
		}
		if requested == (types.Hash{}) {
			requested, err = crv4.SelectFinalityReadBlockContext(ctx, native, requested, head.hash)
			if err != nil {
				return nil, err
			}
		}
		finalized := requested
		number, _, err := native.ReceiptHeaderAtContext(ctx, finalized)
		if err != nil {
			return nil, err
		}
		if finalized == (types.Hash{}) || number < approval.ValidFromNativeBlock || number > approval.ValidThroughNativeBlock || number > math.MaxUint32 {
			return nil, errors.New("owner-recycle finalized head is outside the signed observation window")
		}
		selectedBlock := runtimeFinalityWitness{hash: finalized, number: number}
		if err := finality.check(ctx, native, head, selectedBlock); err != nil {
			return nil, err
		}
		expected := crv4.RuntimeArtifactIdentity{Version: pin.Version, CodeHash: releaseHex32(pin.CodeHash), MetadataHash: releaseHex32(pin.MetadataHash)}
		artifact, err := crv4.ReadRuntimeArtifactAtContext(ctx, native, finalized, expected)
		if err != nil {
			return nil, fmt.Errorf("read owner-recycle finalized runtime: %w", err)
		}
		if artifact.CompatibilityProfile != "" {
			return nil, errors.New("owner-recycle finalized runtime lacks exact independent authority")
		}
		entries, err := ownerRecycleStorageProfile(artifact.Metadata)
		if err != nil {
			return nil, err
		}
		if approval.Proposal.Treasury != nil {
			if err := treasuryStorageProfile(artifact.Metadata, entries); err != nil {
				return nil, err
			}
		}
		read := func(name string, limit int, required bool, args ...[]byte) ([]byte, error) {
			key, err := types.CreateStorageKey(artifact.Metadata, crv4.PalletName, name, args...)
			if err != nil {
				return nil, err
			}
			value := ownerRecycleStorageValue{limit: limit}
			if err := call(ctx, &value, "state_getStorage", key.Hex(), finalized.Hex()); err != nil {
				return nil, fmt.Errorf("owner-recycle %s: %w", name, err)
			}
			if !value.present {
				if required {
					return nil, fmt.Errorf("owner-recycle %s requires explicit finalized storage", name)
				}
				return nil, nil
			}
			return value.raw, nil
		}
		readFixed := func(name string, size int, fallback bool, args ...[]byte) ([]byte, error) {
			raw, err := read(name, size, !fallback, args...)
			if err != nil {
				return nil, err
			}
			if raw == nil && fallback {
				raw = entries[name].Fallback
			}
			if len(raw) != size {
				return nil, fmt.Errorf("owner-recycle %s has an incompatible value length", name)
			}
			return raw, nil
		}
		netuid := binary.LittleEndian.AppendUint16(nil, pin.Netuid)
		mode, err := readFixed("RecycleOrBurn", 1, approval.Proposal.Treasury != nil, netuid)
		if err != nil {
			return nil, err
		}
		if mode[0] > 1 || approval.Proposal.Treasury == nil && mode[0] != 1 {
			return nil, errors.New("owner-recycle needs explicit finalized Recycle; absent or Burn mode cannot pass")
		}
		owner, err := readFixed("SubnetOwner", 32, false, netuid)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(owner, approval.SubnetOwner[:]) {
			return nil, errors.New("owner-recycle subnet owner differs from approval")
		}
		mechanisms, err := readFixed("MechanismCountCurrent", 1, true, netuid)
		if err != nil {
			return nil, err
		}
		if mechanisms[0] != 1 {
			return nil, errors.New("owner-recycle requires exactly one native mechanism")
		}
		nativeEpoch, err := readFixed("SubnetEpochIndex", 8, true, netuid)
		if err != nil {
			return nil, err
		}
		epoch := binary.LittleEndian.Uint64(nativeEpoch)
		if approval.Production == nil && epoch > approval.FirstNativeEpoch || approval.Production != nil && !ownerRecycleDecisionEpochApproved(&approval, epoch) {
			return nil, errors.New("owner-recycle signed first native epoch has passed; no late activation is inferred")
		}
		countRaw, err := readFixed("SubnetworkN", 2, false, netuid)
		if err != nil {
			return nil, err
		}
		count := uint32(binary.LittleEndian.Uint16(countRaw))
		if count == 0 || count > approval.MaximumSubnetUids {
			return nil, errors.New("owner-recycle subnet census exceeds the approved nonzero bound")
		}
		snapshot := OwnerRecycleSnapshot{Runtime: pin, FinalizedHash: [32]byte(finalized), FinalizedNumber: number, MechanismCount: 1, RecycleModeScale: append([]byte(nil), mode...), SubnetOwner: approval.SubnetOwner}
		byHotkey := make(map[[32]byte]OwnerRecycleRegistration, count)
		for uid := uint32(0); uid < count; uid++ {
			uidRaw := binary.LittleEndian.AppendUint16(nil, uint16(uid))
			hotkeyRaw, err := readFixed("Keys", 32, false, netuid, uidRaw)
			if err != nil {
				return nil, err
			}
			hotkey := [32]byte(hotkeyRaw)
			if _, exists := byHotkey[hotkey]; hotkey == ([32]byte{}) || exists {
				return nil, errors.New("owner-recycle registration census has a zero or duplicate hotkey")
			}
			reverse, err := readFixed("Uids", 2, false, netuid, hotkeyRaw)
			if err != nil {
				return nil, err
			}
			if binary.LittleEndian.Uint16(reverse) != uint16(uid) {
				return nil, errors.New("owner-recycle forward and reverse registrations disagree")
			}
			registered, err := readFixed("BlockAtRegistration", 8, false, netuid, uidRaw)
			if err != nil {
				return nil, err
			}
			registration := OwnerRecycleRegistration{Uid: uint16(uid), Hotkey: hotkey, RegistrationBlock: binary.LittleEndian.Uint64(registered)}
			if registration.RegistrationBlock > number {
				return nil, errors.New("owner-recycle registration is newer than its finalized census")
			}
			byHotkey[hotkey] = registration
			snapshot.Registrations = append(snapshot.Registrations, registration)
		}
		if _, exists := byHotkey[approval.ValidatorHotkey]; !exists {
			return nil, errors.New("owner-recycle approved validator hotkey is not registered")
		}
		ownedRaw, err := read("OwnedHotkeys", 4+32*int(approval.MaximumOwnedHotkeys), false, owner)
		if err != nil {
			return nil, err
		}
		if ownedRaw == nil {
			ownedRaw = entries["OwnedHotkeys"].Fallback
		}
		snapshot.OwnedHotkeys, err = decodeOwnerRecycleHotkeys(ownedRaw, approval.MaximumOwnedHotkeys)
		if err != nil {
			return nil, err
		}
		ownerSet := map[[32]byte]bool{}
		var recognized []OwnerRecycleRegistration
		for _, hotkey := range snapshot.OwnedHotkeys {
			ownerSet[hotkey] = true
			if registration, exists := byHotkey[hotkey]; exists {
				recognized = append(recognized, registration)
			}
		}
		sort.Slice(recognized, func(i, j int) bool {
			if recognized[i].RegistrationBlock != recognized[j].RegistrationBlock {
				return recognized[i].RegistrationBlock > recognized[j].RegistrationBlock
			}
			return recognized[i].Uid < recognized[j].Uid
		})
		explicit, err := read("SubnetOwnerHotkey", 32, false, netuid)
		if err != nil {
			return nil, err
		}
		if explicit != nil {
			if len(explicit) != 32 || [32]byte(explicit) == ([32]byte{}) {
				return nil, errors.New("owner-recycle explicit owner hotkey is malformed")
			}
			hotkey := [32]byte(explicit)
			snapshot.SubnetOwnerHotkey = &hotkey
			if registration, exists := byHotkey[hotkey]; exists && !ownerSet[hotkey] {
				recognized = append([]OwnerRecycleRegistration{registration}, recognized...)
			}
		}
		actualOwners := make([][32]byte, len(recognized))
		for index, registration := range recognized {
			actualOwners[index] = registration.Hotkey
		}
		sort.Slice(actualOwners, func(i, j int) bool { return bytes.Compare(actualOwners[i][:], actualOwners[j][:]) < 0 })
		if len(actualOwners) != len(approval.OwnerHotkeys) {
			return nil, errors.New("owner-recycle recognized owner census differs from the exact approved recipients")
		}
		for index := range actualOwners {
			if actualOwners[index] != approval.OwnerHotkeys[index] {
				return nil, errors.New("owner-recycle recognized owner identities changed")
			}
		}
		var treasuryRecipients []TreasuryRecipient
		if policy := approval.Proposal.Treasury; policy != nil {
			if policy.MultisigAccount == snapshot.SubnetOwner {
				return nil, errors.New("treasury custody became the subnet owner")
			}
			for _, recipient := range policy.Recipients {
				registration, exists := byHotkey[recipient.Hotkey]
				if !exists || registration.Uid != recipient.Uid || registration.RegistrationBlock != recipient.RegistrationBlock || ownerSet[recipient.Hotkey] ||
					snapshot.SubnetOwnerHotkey != nil && *snapshot.SubnetOwnerHotkey == recipient.Hotkey {
					return nil, errors.New("treasury exact recipient is absent, re-registered or recognized as a subnet owner")
				}
				coldkey, err := readFixed("Owner", 32, false, recipient.Hotkey[:])
				if err != nil {
					return nil, err
				}
				if !bytes.Equal(coldkey, policy.MultisigAccount[:]) {
					return nil, errors.New("treasury recipient Owner differs from the approved native receiving account")
				}
				treasuryRecipients = append(treasuryRecipients, recipient)
			}
			destination, err := read("AutoStakeDestination", 32, false, policy.MultisigAccount[:], netuid)
			if err != nil {
				return nil, err
			}
			if policy.AutoStakeDestination == nil && destination != nil || policy.AutoStakeDestination != nil && !bytes.Equal(destination, policy.AutoStakeDestination[:]) {
				return nil, errors.New("treasury auto-stake destination differs from the explicit signed policy")
			}
		}
		minimum, err := readFixed("MinAllowedWeights", 2, true, netuid)
		if err != nil {
			return nil, err
		}
		storedCap, err := readFixed("MaxWeightsLimit", 2, true, netuid)
		if err != nil {
			return nil, err
		}
		if err := finality.close(ctx, native, selectedBlock); err != nil {
			return nil, err
		}
		proposalHash, err := approval.Proposal.Hash(cfg.Policy)
		if err != nil {
			return nil, err
		}
		observation := &OwnerRecycleAdmissionObservation{
			ApprovalHash: productionEconomicSelection(cfg).Approval.SHA256, ProposalHash: proposalHash, ConfigHash: approval.ConfigHash,
			NativeEpoch: epoch, FirstNativeEpoch: approval.FirstNativeEpoch, Snapshot: snapshot, RecognizedOwners: recognized,
			TreasuryRecipients:    treasuryRecipients,
			MinimumAllowedWeights: binary.LittleEndian.Uint16(minimum), StoredMaximumWeightLimit: binary.LittleEndian.Uint16(storedCap),
			RuntimeMaximumWeightLimit: 65535, SignedMaximumWeightLimit: cfg.Policy.Steering.MaxWeightLimitU16,
			ApprovalAuthenticated: true, OwnerCensusAuthenticated: true, RecycleModeAuthenticated: true,
			Blockers: []string{
				"production runtime/configuration authority must be migrated without testnet inheritance",
				"successor measurement/envelope/intent/archive and drained activation boundary are not implemented",
				"provider artifacts, self/controlled masks and independent active validator admission need exact decision-time proof",
				"final Yuma/native miner allocation, recycled incentive and runtime-derived rounding tolerance are unobserved",
			},
		}
		if approval.Production != nil {
			observation.Blockers = []string{
				"census alone does not authenticate the complete production decision, eligibility or source transaction",
				"final Yuma/native miner allocation, recycled incentive and runtime-derived rounding tolerance are unobserved",
			}
		}
		if approval.Proposal.Treasury != nil {
			observation.Blockers = []string{
				"census alone does not authenticate the complete production decision, eligibility or source transaction",
				"execution-time owner exclusion, collateral, auto-stake credit and final native 10/90 outcome require independent observation",
			}
		}
		return observation, nil
	})
}

// Bounds hex decoding before copying the storage payload. The transport still
// owns its JSON-RPC envelope limit; this adapter bounds retained/decoded data.
type ownerRecycleStorageValue struct {
	limit   int
	present bool
	raw     []byte
}

// Rejects malformed present values instead of manufacturing storage absence.
func (self *ownerRecycleStorageValue) UnmarshalJSON(encoded []byte) error {
	if bytes.Equal(encoded, []byte("null")) {
		return nil
	}
	if self.limit <= 0 || len(encoded) < 4 || len(encoded) > 4+2*self.limit || encoded[0] != '"' || encoded[len(encoded)-1] != '"' || !bytes.HasPrefix(encoded[1:], []byte("0x")) {
		return errors.New("owner-recycle storage is not bounded canonical hex or null")
	}
	raw := encoded[3 : len(encoded)-1]
	if len(raw)%2 != 0 {
		return errors.New("owner-recycle storage hex has odd length")
	}
	self.raw = make([]byte, len(raw)/2)
	if _, err := hex.Decode(self.raw, raw); err != nil {
		return err
	}
	self.present = true
	return nil
}

// Decodes only bounded, minimally encoded compact lengths before allocation.
func decodeOwnerRecycleHotkeys(raw []byte, maximum uint32) ([][32]byte, error) {
	if len(raw) == 0 || maximum == 0 || maximum > maximumOwnerRecycleCensusEntries {
		return nil, errors.New("owner-recycle owned-hotkey vector is absent or unbounded")
	}
	var count uint32
	var prefix int
	switch raw[0] & 3 {
	case 0:
		count, prefix = uint32(raw[0]>>2), 1
	case 1:
		if len(raw) < 2 {
			return nil, errors.New("owner-recycle owned-hotkey compact length is truncated")
		}
		count, prefix = uint32(binary.LittleEndian.Uint16(raw[:2])>>2), 2
		if count < 64 {
			return nil, errors.New("owner-recycle owned-hotkey compact length is not minimal")
		}
	case 2:
		if len(raw) < 4 {
			return nil, errors.New("owner-recycle owned-hotkey compact length is truncated")
		}
		count, prefix = binary.LittleEndian.Uint32(raw[:4])>>2, 4
		if count < 16384 {
			return nil, errors.New("owner-recycle owned-hotkey compact length is not minimal")
		}
	default:
		return nil, errors.New("owner-recycle owned-hotkey compact length exceeds supported bound")
	}
	if count > maximum || len(raw)-prefix != int(count)*32 {
		return nil, errors.New("owner-recycle owned-hotkey vector exceeds bound or has trailing/truncated bytes")
	}
	result := make([][32]byte, count)
	seen := make(map[[32]byte]bool, count)
	for index := range result {
		copy(result[index][:], raw[prefix+32*index:prefix+32*(index+1)])
		if result[index] == ([32]byte{}) || seen[result[index]] {
			return nil, errors.New("owner-recycle owned-hotkey vector has zero or duplicate identities")
		}
		seen[result[index]] = true
	}
	return result, nil
}

// Keeps the custom storage result tied to the decoder contract used by RPCs.
var _ json.Unmarshaler = (*ownerRecycleStorageValue)(nil)
