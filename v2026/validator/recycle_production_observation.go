// Each production decision observes actual validator eligibility and the
// independently pinned drained activation block. Economic outcome is recorded
// later; it is not an impossible prerequisite to sending the first weight row.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// All fields are observations, not caller-supplied approval switches.
type OwnerRecycleProductionEligibility struct {
	ActivationBlock       uint64                            `json:"activation_block"`
	ActivationHash        [32]byte                          `json:"activation_hash"`
	ActivationNativeEpoch uint64                            `json:"activation_native_epoch"`
	PendingServerEmission uint64                            `json:"pending_server_emission"`
	Validators            []OwnerRecycleProductionValidator `json:"validators"`
}

// Registration freshness bootstraps a new validator without requiring it to
// have submitted the very row being authorized. Later freshness uses LastUpdate.
type OwnerRecycleProductionValidator struct {
	Uid                  uint16   `json:"uid"`
	Hotkey               [32]byte `json:"hotkey"`
	Coldkey              [32]byte `json:"coldkey"`
	StakeAlphaRao        uint64   `json:"stake_alpha_rao"`
	TotalStakeRao        uint64   `json:"total_stake_rao"`
	StakeThresholdRao    uint64   `json:"stake_threshold_rao"`
	ValidatorPermit      bool     `json:"validator_permit"`
	SubnetOwnerException bool     `json:"subnet_owner_exception"`
	LastUpdate           uint64   `json:"last_update"`
	RegistrationBlock    uint64   `json:"registration_block"`
}

// The complete current and historical runtime identities are authenticated
// before any storage is decoded; no spec-version-only fallback is selected.
func observeOwnerRecycleProductionEligibility(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain, authority *OwnerRecycleMeasurementAuthority) (*OwnerRecycleProductionEligibility, error) {
	return crv4.ReadRuntimeObservationContext(ctx, native, func(ctx context.Context) (*OwnerRecycleProductionEligibility, error) {
		return observeOwnerRecycleProductionEligibilityAttempt(ctx, cfg, native, authority)
	})
}

// Both the decision and activation census must belong to one complete read;
// a late reconnect cannot retain an earlier validator or emission projection.
func observeOwnerRecycleProductionEligibilityAttempt(ctx context.Context, cfg *ReleaseConfig, native *crv4.Chain, authority *OwnerRecycleMeasurementAuthority) (*OwnerRecycleProductionEligibility, error) {
	envelope, err := ownerRecycleProductionApproval(cfg)
	if err != nil || authority == nil || authority.expected.NativeSnapshotBlock == 0 {
		return nil, errors.Join(errors.New("owner-recycle production eligibility lacks approved decision authority"), err)
	}
	approval := envelope.Approval
	production := approval.Production
	block := types.Hash(authority.observation.Snapshot.FinalizedHash)
	view := *native
	if err := authenticateHistoricalNativeRuntimeAtContext(ctx, &view, cfg, block); err != nil {
		return nil, err
	}
	artifact, err := crv4.ReadRuntimeArtifactAtContext(ctx, &view, block, releaseRuntimeIdentityV2(cfg))
	if err != nil {
		return nil, err
	}
	entries, err := ownerRecycleProductionStorageProfile(artifact.Metadata)
	if err != nil {
		return nil, err
	}
	netuid := binary.LittleEndian.AppendUint16(nil, cfg.Netuid)
	read := func(metadata *types.Metadata, hash types.Hash, name string, limit int, fallback []byte) ([]byte, error) {
		key, err := types.CreateStorageKey(metadata, crv4.PalletName, name, netuid)
		if err != nil {
			return nil, err
		}
		value := ownerRecycleStorageValue{limit: limit}
		if err := view.API.Client.CallContext(ctx, &value, "state_getStorage", key.Hex(), hash.Hex()); err != nil {
			return nil, err
		}
		if !value.present {
			return bytes.Clone(fallback), nil
		}
		return value.raw, nil
	}
	lastRaw, err := read(artifact.Metadata, block, "LastUpdate", 5+8*len(authority.observation.Snapshot.Registrations), entries["LastUpdate"].Fallback)
	if err != nil {
		return nil, err
	}
	lastUpdates, err := ownerRecycleLastUpdates(lastRaw, len(authority.observation.Snapshot.Registrations))
	if err != nil {
		return nil, err
	}
	result := &OwnerRecycleProductionEligibility{ActivationBlock: ownerRecycleActivationBlock(&approval), ActivationHash: production.ActivationNativeHash}
	coldkeysKVs := map[[32]byte]bool{}
	for _, hotkey := range production.ValidatorHotkeys {
		var registration *OwnerRecycleRegistration
		for index := range authority.observation.Snapshot.Registrations {
			candidate := &authority.observation.Snapshot.Registrations[index]
			if candidate.Hotkey == hotkey {
				registration = candidate
				break
			}
		}
		if registration == nil {
			return nil, errors.New("owner-recycle production validator is absent from the exact native census")
		}
		stake, err := crv4.ReadValidatorStakeAtContext(ctx, &view, crv4.ValidatorIdentityQuery{
			GenesisHash: view.GenesisHash, BlockHash: block, BlockNumber: authority.expected.NativeSnapshotBlock,
			Netuid: cfg.Netuid, UID: registration.Uid, MaximumSubnetUIDs: approval.MaximumSubnetUids,
		}, releaseRuntimeIdentityV2(cfg))
		if err != nil {
			return nil, err
		}
		last := lastUpdates[registration.Uid]
		fresh := max(last, registration.RegistrationBlock)
		if stake.Identity.Hotkey != hotkey || !stake.MeetsNonSelfStakeAndPermit() || stake.Identity.Coldkey == ([32]byte{}) ||
			coldkeysKVs[stake.Identity.Coldkey] || fresh == 0 || fresh > authority.expected.NativeSnapshotBlock ||
			authority.expected.NativeSnapshotBlock-fresh > production.MaximumLastUpdateAge ||
			approval.Proposal.Treasury != nil && stake.Identity.Coldkey == approval.Proposal.Treasury.MultisigAccount {
			return nil, errors.New("owner-recycle production validators lack distinct ownership, stake/permit or bounded native activity")
		}
		// The runtime lets the subnet owner hotkey submit without a permit or
		// threshold stake. An approved owner-validator gets no such exception:
		// it is the registered owner hotkey under the subnet owner coldkey and
		// still holds a real validator permit and threshold stake.
		if slices.Contains(approval.OwnerHotkeys, hotkey) && (stake.Identity.Coldkey != approval.SubnetOwner || !stake.SubnetOwnerRegistered ||
			stake.SubnetOwnerUID != registration.Uid || !stake.Identity.ValidatorPermit || stake.TotalStakeRao < stake.StakeThresholdRao) {
			return nil, errors.New("owner-validator lacks the subnet owner identity, a validator permit or threshold stake")
		}
		coldkeysKVs[stake.Identity.Coldkey] = true
		// The advancing finality witness is deliberately not a decision fact;
		// retaining it here would invalidate an unchanged historical replay.
		result.Validators = append(result.Validators, OwnerRecycleProductionValidator{
			Uid: registration.Uid, Hotkey: stake.Identity.Hotkey, Coldkey: stake.Identity.Coldkey,
			StakeAlphaRao: stake.Identity.StakeAlphaRao, TotalStakeRao: stake.TotalStakeRao, StakeThresholdRao: stake.StakeThresholdRao,
			ValidatorPermit: stake.Identity.ValidatorPermit, SubnetOwnerException: stake.SubnetOwnerPresent && stake.SubnetOwnerRegistered && stake.SubnetOwnerUID == registration.Uid,
			LastUpdate: last, RegistrationBlock: registration.RegistrationBlock})
	}
	activationHash := types.Hash(production.ActivationNativeHash)
	if err := authenticateHistoricalNativeRuntimeAtContext(ctx, &view, cfg, activationHash); err != nil {
		return nil, err
	}
	activationNumber, _, err := view.CanonicalHeaderAtContext(ctx, activationHash)
	if err != nil {
		return nil, fmt.Errorf("read owner-recycle activation header: %w", err)
	}
	if activationNumber != result.ActivationBlock || result.ActivationBlock > authority.expected.NativeSnapshotBlock {
		return nil, errors.New("owner-recycle activation is not its signed earlier finalized block")
	}
	allowed, err := releaseHistoricalRuntimeArtifactsAt(cfg, result.ActivationBlock)
	if err != nil {
		return nil, err
	}
	activation, err := crv4.ReadRuntimeArtifactAtContext(ctx, &view, activationHash, allowed...)
	if err != nil {
		return nil, err
	}
	activationEntries, err := ownerRecycleProductionStorageProfile(activation.Metadata)
	if err != nil {
		return nil, err
	}
	for _, field := range []struct {
		name   string
		target *uint64
	}{
		{name: "PendingServerEmission", target: &result.PendingServerEmission},
		{name: "SubnetEpochIndex", target: &result.ActivationNativeEpoch},
	} {
		raw, err := read(activation.Metadata, activationHash, field.name, 8, activationEntries[field.name].Fallback)
		if err != nil {
			return nil, fmt.Errorf("read owner-recycle activation %s: %w", field.name, err)
		}
		if len(raw) != 8 {
			return nil, fmt.Errorf("owner-recycle activation %s is not an exact u64", field.name)
		}
		*field.target = binary.LittleEndian.Uint64(raw)
	}
	if result.PendingServerEmission != 0 || result.ActivationNativeEpoch != approval.FirstNativeEpoch {
		return nil, errors.New("owner-recycle activation has pre-activation pending miner emissions or a different first native epoch")
	}
	if err := view.CheckCanonicalBlockAtContext(ctx, activationHash, activationNumber); err != nil {
		return nil, err
	}
	if err := view.CheckCanonicalBlockAtContext(ctx, block, authority.expected.NativeSnapshotBlock); err != nil {
		return nil, err
	}
	return result, ctx.Err()
}

// The facts each decision checks at a signed activation, read at one candidate
// block before an approval names it: its canonical height, native epoch index
// and pending miner emission, under that block's approved historical runtime.
func readOwnerRecycleDrainedBoundaryAt(ctx context.Context, native *crv4.Chain, cfg *ReleaseConfig, block types.Hash) (productionDrainedBoundaryFacts, error) {
	return crv4.ReadRuntimeObservationContext(ctx, native, func(ctx context.Context) (productionDrainedBoundaryFacts, error) {
		view := *native
		if err := authenticateHistoricalNativeRuntimeAtContext(ctx, &view, cfg, block); err != nil {
			return productionDrainedBoundaryFacts{}, err
		}
		number, _, err := view.CanonicalHeaderAtContext(ctx, block)
		if err != nil {
			return productionDrainedBoundaryFacts{}, fmt.Errorf("read drained native boundary header: %w", err)
		}
		allowed, err := releaseHistoricalRuntimeArtifactsAt(cfg, number)
		if err != nil {
			return productionDrainedBoundaryFacts{}, err
		}
		artifact, err := crv4.ReadRuntimeArtifactAtContext(ctx, &view, block, allowed...)
		if err != nil {
			return productionDrainedBoundaryFacts{}, err
		}
		entries, err := ownerRecycleProductionStorageProfile(artifact.Metadata)
		if err != nil {
			return productionDrainedBoundaryFacts{}, err
		}
		facts := productionDrainedBoundaryFacts{Block: number}
		for _, field := range []struct {
			name   string
			target *uint64
		}{
			{name: "PendingServerEmission", target: &facts.PendingServerEmission},
			{name: "SubnetEpochIndex", target: &facts.Epoch},
		} {
			key, err := types.CreateStorageKey(artifact.Metadata, crv4.PalletName, field.name, binary.LittleEndian.AppendUint16(nil, cfg.Netuid))
			if err != nil {
				return productionDrainedBoundaryFacts{}, err
			}
			value := ownerRecycleStorageValue{limit: 8}
			if err := view.API.Client.CallContext(ctx, &value, "state_getStorage", key.Hex(), block.Hex()); err != nil {
				return productionDrainedBoundaryFacts{}, fmt.Errorf("read drained native boundary %s: %w", field.name, err)
			}
			raw := value.raw
			if !value.present {
				raw = entries[field.name].Fallback
			}
			if len(raw) != 8 {
				return productionDrainedBoundaryFacts{}, fmt.Errorf("drained native boundary %s is not an exact u64", field.name)
			}
			*field.target = binary.LittleEndian.Uint64(raw)
		}
		if err := view.CheckCanonicalBlockAtContext(ctx, block, number); err != nil {
			return productionDrainedBoundaryFacts{}, err
		}
		return facts, ctx.Err()
	})
}

// Observations only; an approval decides separately whether to name them.
type productionDrainedBoundaryFacts struct {
	Block                 uint64
	Epoch                 uint64
	PendingServerEmission uint64
}

// Additional consumed fields have the same exact metadata discipline as the
// owner census. Zero defaults are accepted only when their type defines them.
func ownerRecycleProductionStorageProfile(metadata *types.Metadata) (map[string]types.StorageEntryMetadataV14, error) {
	entries, err := ownerRecycleStorageProfile(metadata)
	if err != nil {
		return nil, err
	}
	for _, field := range []struct {
		name, kind string
		fallback   []byte
	}{
		{name: "PendingServerEmission", kind: "u64", fallback: make([]byte, 8)},
		{name: "LastUpdate", kind: "u64s", fallback: []byte{0}},
	} {
		entry, found := entries[field.name]
		if !found || !entry.Type.IsMap || !entry.Modifier.IsDefault || entry.Modifier.IsOptional ||
			!reflect.DeepEqual(entry.Type.AsMap.Hashers, []types.StorageHasherV10{{IsIdentity: true}}) ||
			!ownerRecycleStorageType(metadata, entry.Type.AsMap.Key, "u16", 0) || !ownerRecycleStorageType(metadata, entry.Type.AsMap.Value, field.kind, 0) ||
			!bytes.Equal(entry.Fallback, field.fallback) {
			return nil, fmt.Errorf("owner-recycle production storage %s changed its consumed interface", field.name)
		}
	}
	return entries, nil
}

// Decode the bounded census before allocation and reject noncanonical compact
// lengths, surplus rows and truncated u64s rather than accepting a prefix.
func ownerRecycleLastUpdates(raw []byte, count int) ([]uint64, error) {
	if len(raw) == 0 || count < 1 || count > 65535 {
		return nil, errors.New("owner-recycle last-update census is absent or unbounded")
	}
	width, length := 1, uint32(raw[0]>>2)
	switch raw[0] & 3 {
	case 1:
		if len(raw) < 2 {
			return nil, errors.New("owner-recycle last-update length is truncated")
		}
		width, length = 2, uint32(binary.LittleEndian.Uint16(raw[:2])>>2)
		if length < 64 {
			return nil, errors.New("owner-recycle last-update length is noncanonical")
		}
	case 2:
		if len(raw) < 4 {
			return nil, errors.New("owner-recycle last-update length is truncated")
		}
		width, length = 4, binary.LittleEndian.Uint32(raw[:4])>>2
		if length < 16384 {
			return nil, errors.New("owner-recycle last-update length is noncanonical")
		}
	case 3:
		return nil, errors.New("owner-recycle last-update length exceeds its finite census")
	}
	if length != uint32(count) || len(raw)-width != 8*count {
		return nil, errors.New("owner-recycle last-update vector differs from the complete registered census")
	}
	result := make([]uint64, count)
	for index := range result {
		result[index] = binary.LittleEndian.Uint64(raw[width+8*index:])
	}
	return result, nil
}
