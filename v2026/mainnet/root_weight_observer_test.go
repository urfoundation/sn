// Local read-only rpc fixtures exercise the production root weight observer's
// canonical block binding, complete census, defaults and interrupted rechecks.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// State is configured before starting reads; the underlying fixture serializes
// every fault hook and joins its http handlers at cleanup.
func newRootWeightReaderFixture(t *testing.T) (*rootCanonicalChain, *rootReceiptFixture) {
	t.Helper()
	chain, fixture := newRootReceiptFixture(t, 2, false)
	set := func(name string, value []byte, args ...[]byte) {
		key, err := types.CreateStorageKey(fixture.metadata, "SubtensorModule", name, args...)
		if err != nil {
			t.Fatal(err)
		}
		fixture.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(value)
	}
	for netuid := uint16(0); netuid < 8; netuid++ {
		set("NetworksAdded", []byte{1}, binary.LittleEndian.AppendUint16(nil, netuid))
	}
	set("NetworksAdded", []byte{0}, []byte{8, 0})
	set("RootWeightSettingEnabled", []byte{1})
	set("RootWeightsCap", binary.LittleEndian.AppendUint16(nil, 4096), []byte{0, 0})
	set("WeightsSetRateLimit", binary.LittleEndian.AppendUint64(nil, 1), []byte{0, 0})
	fixture.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if method != "state_getKeysPaged" {
			return nil, false
		}
		if len(params) != 4 {
			t.Errorf("network census omitted exact-block arguments: %s", params)
			return []string{}, true
		}
		var prefix, start, block string
		json.Unmarshal(params[0], &prefix)
		json.Unmarshal(params[2], &start)
		json.Unmarshal(params[3], &block)
		if block != fixture.finalized {
			t.Errorf("network census read another block: %s", block)
		}
		keys := []string{}
		for key := range fixture.storageKVs {
			if strings.HasPrefix(key, prefix) && key > start {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		return keys, true
	}
	return chain, fixture
}

// Only true network entries count toward runtime minimum/cap rules. Every
// storage request uses the same authenticated finalized block, never latest.
func TestRootServiceCanonicalWeightObservation(t *testing.T) {
	chain, fixture := newRootWeightReaderFixture(t)
	baseFault := fixture.fault
	fixture.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if method == "state_getStorage" {
			var block string
			if len(params) > 1 {
				json.Unmarshal(params[1], &block)
			}
			if block != fixture.finalized {
				t.Errorf("storage was not pinned to the finalized observation: %s", block)
			}
		}
		return baseFault(method, params, count)
	}
	view, err := chain.observeRootWeights(context.Background(), fixture.action)
	if err != nil || len(view.ActiveNetworks) != 8 || view.ActiveNetworks[7] != 7 || !view.Enabled || view.ConcentrationCap != 4096 || view.RateLimitBlocks != 1 ||
		view.LastUpdate != fixture.action.BirthBlock+1 || len(view.StoredWeights) != 1 || view.StoredWeights[0] != (rootStoredWeight{Netuid: 1, Weight: 65535}) || view.Position.FinalizedHash != fixture.finalized || view.validate(fixture.action) != nil {
		t.Fatalf("incomplete canonical weight observation: %+v %v", view, err)
	}
	if fixture.counts["state_getKeysPaged"] != 2 {
		t.Fatal("network census did not read its terminating empty page")
	}
}

// The runtime uses metadata defaults for absent weights and zero for a missing
// last-update slot. Those authenticated defaults must not invent a prior action.
func TestRootServiceCanonicalWeightDefaults(t *testing.T) {
	for _, shortVector := range []bool{false, true} {
		chain, fixture := newRootWeightReaderFixture(t)
		uid := binary.LittleEndian.AppendUint16(nil, fixture.action.Scope.Seat.Uid)
		weights, _ := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "Weights", []byte{0, 0}, uid)
		last, _ := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "LastUpdate", []byte{0, 0})
		delete(fixture.storageKVs, weights.Hex())
		delete(fixture.storageKVs, last.Hex())
		if shortVector {
			fixture.storageKVs[last.Hex()] = "0x00"
		}
		view, err := chain.observeRootWeights(context.Background(), fixture.action)
		if err != nil || view.StoredWeights == nil || len(view.StoredWeights) != 0 || view.LastUpdate != 0 {
			t.Fatalf("defaulted weights or last update differ: %+v %v", view, err)
		}
	}
}

// A sampled network, runtime or canonical-hash change cannot publish a partial
// weight view after the initial ancestry and existing-seat checks succeeded.
func TestRootServiceCanonicalWeightRechecks(t *testing.T) {
	for _, failure := range []string{"evm-before", "evm-after", "runtime", "code", "metadata", "finalized", "cancel"} {
		chain, fixture := newRootWeightReaderFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		baseFault := fixture.fault
		finalizedLookups := 0
		fixture.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
			if method == "eth_chainId" && (failure == "evm-before" || failure == "evm-after" && count == 3) {
				return "0x3b1", true
			}
			if method == "state_getRuntimeVersion" && count == 2 && failure == "runtime" {
				version := fixture.profile.RuntimeVersion
				version.SpecVersion++
				return version, true
			}
			if method == "state_getStorageHash" && count == 2 && failure == "code" {
				return "0x" + strings.Repeat("ad", 32), true
			}
			if method == "state_getMetadata" && failure == "metadata" {
				return fixture.metadataHex + "00", true
			}
			if method == "chain_getBlockHash" {
				var height uint64
				json.Unmarshal(params[0], &height)
				if height == fixture.action.BirthBlock+2 {
					finalizedLookups++
					if finalizedLookups == 2 && failure == "finalized" {
						return "0x" + strings.Repeat("ad", 32), true
					}
				}
			}
			if method == "eth_chainId" && count == 3 && failure == "cancel" {
				cancel()
			}
			return baseFault(method, params, count)
		}
		view, err := chain.observeRootWeights(ctx, fixture.action)
		cancel()
		if err == nil || !reflect.DeepEqual(view, rootWeightObservation{}) {
			t.Fatalf("%s published a changed/partial weight observation: %+v %v", failure, view, err)
		}
	}
}

// A paginated key cannot silently disappear, use a default bool, or carry an
// unvalidated suffix; malformed storage is refused as a whole observation.
func TestRootServiceCanonicalWeightCensusFailures(t *testing.T) {
	for _, failure := range []string{"missing", "malformed-bool", "wrong-key-width", "duplicate-page"} {
		chain, fixture := newRootWeightReaderFixture(t)
		key, _ := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "NetworksAdded", []byte{3, 0})
		baseFault := fixture.fault
		fixture.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
			if method == "state_getStorage" {
				var current string
				json.Unmarshal(params[0], &current)
				if current == key.Hex() {
					if failure == "missing" {
						return nil, true
					}
					if failure == "malformed-bool" {
						return "0x0100", true
					}
				}
			}
			if method == "state_getKeysPaged" && failure == "wrong-key-width" {
				return []string{key.Hex() + "00"}, true
			}
			if method == "state_getKeysPaged" && failure == "duplicate-page" {
				return []string{key.Hex(), key.Hex()}, true
			}
			return baseFault(method, params, count)
		}
		view, err := chain.observeRootWeights(context.Background(), fixture.action)
		if err == nil || !reflect.DeepEqual(view, rootWeightObservation{}) {
			t.Fatalf("%s census failure was published: %+v %v", failure, view, err)
		}
	}
}
