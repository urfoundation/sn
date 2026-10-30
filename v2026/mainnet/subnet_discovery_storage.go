// Discovery stages exact keys in small sequential batches to fit a public
// archive's read quota. Normal policy previews retain their per-key read path.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const subnetDiscoveryStorageBatchKeys = 128

// Only the enumerated contiguous forward keys and their four exact identity
// mappings are fetched. No prefix, latest state or unrelated account is queried.
func (self *rootStorageReader) prepareSubnetRegistrations(ctx context.Context, netuid uint16, count int, forwardKVs map[string]bool) error {
	netuidArg := binary.LittleEndian.AppendUint16(nil, netuid)
	forwardKeys := make([]string, count)
	for index := range forwardKeys {
		key, err := types.CreateStorageKey(self.metadata, "SubtensorModule", "Keys", netuidArg, binary.LittleEndian.AppendUint16(nil, uint16(index)))
		if err != nil || !forwardKVs[key.Hex()] {
			return fmt.Errorf("%w: discovery forward census has a hole or out-of-range UID", errRpcIntegrity)
		}
		forwardKeys[index] = key.Hex()
	}
	forwardValues, err := self.readDiscoveryStorageBatches(ctx, forwardKeys)
	if err != nil {
		return err
	}
	identityKeys := make([]string, 0, 4*count)
	for index, key := range forwardKeys {
		raw := forwardValues[key]
		if raw == nil || !subnetAccountValid(*raw) {
			return fmt.Errorf("%w: discovery forward batch has no recorded nonzero hotkey", errRpcIntegrity)
		}
		hotkey, _ := hex.DecodeString((*raw)[2:])
		for _, request := range []struct {
			name string
			args [][]byte
		}{
			{name: "Uids", args: [][]byte{netuidArg, hotkey}},
			{name: "Owner", args: [][]byte{hotkey}},
			{name: "BlockAtRegistration", args: [][]byte{netuidArg, binary.LittleEndian.AppendUint16(nil, uint16(index))}},
			{name: "IsNetworkMember", args: [][]byte{hotkey, netuidArg}},
		} {
			key, err := types.CreateStorageKey(self.metadata, "SubtensorModule", request.name, request.args...)
			if err != nil {
				return err
			}
			identityKeys = append(identityKeys, key.Hex())
		}
	}
	identityValues, err := self.readDiscoveryStorageBatches(ctx, identityKeys)
	if err != nil {
		return err
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if len(self.preparedKVs) != 0 {
		return fmt.Errorf("%w: discovery left unconsumed prepared storage", errRpcIntegrity)
	}
	self.preparedKVs = forwardValues
	for key, value := range identityValues {
		self.preparedKVs[key] = value
	}
	return nil
}

// Every requested key must occur exactly once in one change set at the retained
// hash. Explicit null remains a recorded absence; an omitted key is an error.
func (self *rootStorageReader) readDiscoveryStorageBatches(ctx context.Context, keys []string) (map[string]*string, error) {
	values := make(map[string]*string, len(keys))
	requestedKVs := make(map[string]bool, len(keys))
	if len(keys) > 4*rootCensusLimit {
		return nil, fmt.Errorf("%w: discovery batch key census exceeds bound", errRpcIntegrity)
	}
	for _, key := range keys {
		if requestedKVs[key] {
			return nil, fmt.Errorf("%w: discovery requested duplicate identity keys", errRpcIntegrity)
		}
		requestedKVs[key] = true
	}
	for start := 0; start < len(keys); start += subnetDiscoveryStorageBatchKeys {
		batch := keys[start:min(start+subnetDiscoveryStorageBatchKeys, len(keys))]
		var reply []struct {
			Block   string              `json:"block"`
			Changes [][]json.RawMessage `json:"changes"`
		}
		if err := self.client.callAdmittedRead(ctx, "state_queryStorageAt", []any{batch, self.block}, &reply, false, maxRpcReplyBytes); err != nil {
			return nil, fmt.Errorf("discovery storage batch: %w", err)
		}
		if len(reply) != 1 || !strings.EqualFold(reply[0].Block, self.block) || len(reply[0].Changes) != len(batch) {
			return nil, fmt.Errorf("%w: discovery batch block or cardinality differs", errRpcIntegrity)
		}
		batchKVs := make(map[string]bool, len(batch))
		for _, key := range batch {
			batchKVs[key] = true
		}
		for _, change := range reply[0].Changes {
			var key string
			var value *string
			if len(change) != 2 || json.Unmarshal(change[0], &key) != nil || json.Unmarshal(change[1], &value) != nil {
				return nil, fmt.Errorf("%w: discovery batch change is malformed", errRpcIntegrity)
			}
			key = strings.ToLower(key)
			if _, exists := values[key]; exists || !batchKVs[key] {
				return nil, fmt.Errorf("%w: discovery batch repeats or substitutes a requested key", errRpcIntegrity)
			}
			values[key] = value
		}
	}
	return values, nil
}
