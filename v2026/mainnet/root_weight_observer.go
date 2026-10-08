// The production weight observer extends the approved read-only canonical
// chain port. It cannot supply live eligibility, signer custody or submission.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// One total bounded context covers canonical ancestry, storage census and final
// network/hash rechecks. Read caches remain under the chain's existing owner.
func (self *rootCanonicalChain) observeRootWeights(ctx context.Context, action rootAction) (rootWeightObservation, error) {
	if ctx == nil {
		return rootWeightObservation{}, errors.New("root weight observation requires a context")
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	reconciliation, err := self.reconcile(operationCtx, action, nil)
	if err != nil {
		return rootWeightObservation{}, err
	}
	if err := reconciliation.Observation.matches(action, true); err != nil {
		return rootWeightObservation{}, err
	}
	select {
	case self.reconcileCh <- struct{}{}:
		defer func() { <-self.reconcileCh }()
	case <-operationCtx.Done():
		return rootWeightObservation{}, operationCtx.Err()
	}
	if err := operationCtx.Err(); err != nil {
		return rootWeightObservation{}, err
	}
	position := reconciliation.Observation
	runtime, err := self.runtimeAt(operationCtx, position.FinalizedHash)
	if err != nil {
		return rootWeightObservation{}, err
	}
	if runtime.profile.RuntimeVersion != action.Scope.RuntimeVersion || runtime.profile.RuntimeCodeHash != action.Scope.RuntimeCodeHash || runtime.profile.RuntimeMetadataHash != action.Scope.RuntimeMetadataHash {
		return rootWeightObservation{}, errors.New("root weight runtime changed at the finalized observation hash")
	}
	var specs []rootStorageSpec
	for _, spec := range rootStorageSpecs {
		switch spec.name {
		case "NetworksAdded", "Weights", "LastUpdate", "RootWeightSettingEnabled", "RootWeightsCap", "WeightsSetRateLimit":
			specs = append(specs, spec)
		}
	}
	entries, err := observationStorageProfile(runtime.metadata, specs)
	if err != nil {
		return rootWeightObservation{}, err
	}
	reader := &rootStorageReader{client: self.client, metadata: runtime.metadata, entries: entries, specs: specs, block: position.FinalizedHash, valueKVs: map[string]rootStorageValue{}}
	rootArg := []byte{0, 0}
	probe, err := types.CreateStorageKey(runtime.metadata, "SubtensorModule", "NetworksAdded", rootArg)
	if err != nil || len(probe) != 34 {
		return rootWeightObservation{}, errors.New("root weight network key profile differs")
	}
	keys, err := reader.keys(operationCtx, probe[:32])
	if err != nil {
		return rootWeightObservation{}, err
	}
	networkIds, active := make([]uint16, len(keys)), make([]bool, len(keys))
	err = rootReadParallel(operationCtx, len(keys), func(readCtx context.Context, index int) error {
		key, err := hex.DecodeString(keys[index][2:])
		if err != nil || len(key) != 34 {
			return errors.New("root weight network census key has the wrong width")
		}
		networkIds[index] = binary.LittleEndian.Uint16(key[32:])
		value, err := reader.read(readCtx, "NetworksAdded", key[32:])
		if err != nil {
			return err
		}
		if value.RawStorage == nil || value.Key != keys[index] {
			return errors.New("root weight enumerated network entry is absent or mismatched")
		}
		active[index] = value.data[0] == 1
		return nil
	})
	if err != nil {
		return rootWeightObservation{}, err
	}
	result := rootWeightObservation{Schema: rootWeightObservationSchema, Position: position, ActiveNetworks: []uint16{}, StoredWeights: []rootStoredWeight{}}
	for index, netuid := range networkIds {
		if active[index] {
			result.ActiveNetworks = append(result.ActiveNetworks, netuid)
		}
	}
	sort.Slice(result.ActiveNetworks, func(i, j int) bool { return result.ActiveNetworks[i] < result.ActiveNetworks[j] })
	weights, err := reader.read(operationCtx, "Weights", rootArg, binary.LittleEndian.AppendUint16(nil, action.Scope.Seat.Uid))
	if err != nil {
		return rootWeightObservation{}, err
	}
	body, count, err := rootVector(weights.data, 4, 0)
	if err != nil {
		return rootWeightObservation{}, err
	}
	for index := range count {
		result.StoredWeights = append(result.StoredWeights, rootStoredWeight{Netuid: binary.LittleEndian.Uint16(body[index*4:]), Weight: binary.LittleEndian.Uint16(body[index*4+2:])})
	}
	last, err := reader.read(operationCtx, "LastUpdate", rootArg)
	if err != nil {
		return rootWeightObservation{}, err
	}
	body, count, err = rootVector(last.data, 8, 0)
	if err != nil {
		return rootWeightObservation{}, err
	}
	if int(action.Scope.Seat.Uid) < count {
		result.LastUpdate = binary.LittleEndian.Uint64(body[int(action.Scope.Seat.Uid)*8:])
	}
	enabled, err := reader.read(operationCtx, "RootWeightSettingEnabled")
	if err != nil {
		return rootWeightObservation{}, err
	}
	result.Enabled = enabled.data[0] == 1
	cap, err := reader.read(operationCtx, "RootWeightsCap", rootArg)
	if err != nil {
		return rootWeightObservation{}, err
	}
	result.ConcentrationCap = binary.LittleEndian.Uint16(cap.data)
	rate, err := reader.read(operationCtx, "WeightsSetRateLimit", rootArg)
	if err != nil {
		return rootWeightObservation{}, err
	}
	result.RateLimitBlocks = binary.LittleEndian.Uint64(rate.data)
	result.StorageHash = rootObjectHash(reader.evidence())
	var canonical string
	if err := self.client.call(operationCtx, "chain_getBlockHash", []any{position.FinalizedNumber}, &canonical); err != nil {
		return rootWeightObservation{}, err
	}
	if canonical != position.FinalizedHash {
		return rootWeightObservation{}, errors.New("root weight finalized snapshot changed during storage reads")
	}
	if err := self.network(operationCtx); err != nil {
		return rootWeightObservation{}, err
	}
	if err := errors.Join(operationCtx.Err(), result.validate(action)); err != nil {
		return rootWeightObservation{}, err
	}
	return result, nil
}
