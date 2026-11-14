// Exact-key batch faults use synthetic protocol state and deterministic reply
// rewrites. No live endpoint, identity or timing assumption enters these tests.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// The same production metadata profile backs both batch and ordinary readers.
func subnetDiscoveryTestReader(t *testing.T, client *rpcClient, fixture *rootRpcFixture, batch bool) *rootStorageReader {
	t.Helper()
	entries, err := observationStorageProfile(fixture.metadata, subnetStorageSpecs)
	if err != nil {
		t.Fatal(err)
	}
	return &rootStorageReader{client: client, metadata: fixture.metadata, entries: entries, specs: subnetStorageSpecs, block: testFinalizedHash,
		valueKVs: map[string]rootStorageValue{}, batchRegistrations: batch}
}

// Additional generated identities cross the exact 128-key boundary without
// sharing accounts, changing the finalized hash or depending on external state.
func subnetDiscoveryTestLargeMembership(t *testing.T, fixture *rootRpcFixture) {
	t.Helper()
	arg := []byte{25, 0}
	fixture.set(t, "SubnetworkN", []byte{130, 0}, arg)
	fixture.set(t, "MaxAllowedUids", []byte{130, 0}, arg)
	for index := 6; index < 130; index++ {
		hotkey, coldkey := bytes.Repeat([]byte{0x71}, 32), bytes.Repeat([]byte{0x72}, 32)
		binary.LittleEndian.PutUint32(hotkey[28:], uint32(index))
		binary.LittleEndian.PutUint32(coldkey[28:], uint32(index))
		uid := binary.LittleEndian.AppendUint16(nil, uint16(index))
		fixture.set(t, "Keys", hotkey, arg, uid)
		fixture.set(t, "Uids", uid, arg, hotkey)
		fixture.set(t, "Owner", coldkey, hotkey)
		fixture.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 40), arg, uid)
		fixture.set(t, "IsNetworkMember", []byte{1}, hotkey, arg)
	}
}

// Batching changes transport volume only. Identity order and sorted retained
// raw/default evidence match the original per-key implementation exactly.
func TestSubnetDiscoveryBatchMatchesPerKeyEvidenceAcrossBoundary(t *testing.T) {
	client, fixture, _, _ := newSubnetDiscoveryFixture(t)
	subnetDiscoveryTestLargeMembership(t, fixture)
	ordinary := subnetDiscoveryTestReader(t, client, fixture, false)
	want, maximum, err := ordinary.subnetRegistrations(t.Context(), 25, 100)
	if err != nil {
		t.Fatal(err)
	}
	priorReads := fixture.count("state_getStorage")
	batch := subnetDiscoveryTestReader(t, client, fixture, true)
	got, batchMaximum, err := batch.subnetRegistrations(t.Context(), 25, 100)
	if err != nil || maximum != batchMaximum || !reflect.DeepEqual(want, got) || !reflect.DeepEqual(ordinary.evidence(), batch.evidence()) {
		t.Fatalf("batch changed exact identities or evidence: maximum=%d/%d err=%v", maximum, batchMaximum, err)
	}
	if len(got) != 130 || fixture.count("state_queryStorageAt") != 7 || fixture.count("state_getStorage")-priorReads != 3 || len(batch.preparedKVs) != 0 {
		t.Fatalf("batch failed bounds or single consumption: queries=%d per-key=%d unconsumed=%d", fixture.count("state_queryStorageAt"), fixture.count("state_getStorage")-priorReads, len(batch.preparedKVs))
	}
	values := batch.evidence()
	for index := 1; index < len(values); index++ {
		if values[index-1].Key >= values[index].Key {
			t.Fatal("batch evidence order depends on reply or worker order")
		}
	}
}

// Corrupt one batch at the real HTTP response boundary. Exact cardinality,
// block scope and duplicate detection apply before any partial result escapes.
func TestSubnetDiscoveryBatchRejectsIncompleteOrForeignReplies(t *testing.T) {
	for _, fault := range []string{"missing", "duplicate", "extra", "foreign", "wrong-block", "multiple-blocks", "malformed-pair", "null-result", "null-hotkey"} {
		client, fixture, _, snapshot := newSubnetDiscoveryFixture(t)
		client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
			raw, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			request.Body = io.NopCloser(bytes.NewReader(raw))
			var call struct{ Method string }
			if err := json.Unmarshal(raw, &call); err != nil {
				return nil, err
			}
			response, err := fixture.roundTrip(request)
			if err != nil || call.Method != "state_queryStorageAt" {
				return response, err
			}
			var reply map[string]any
			if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
				return nil, err
			}
			response.Body.Close()
			results := reply["result"].([]any)
			result := results[0].(map[string]any)
			changes := result["changes"].([]any)
			switch fault {
			case "missing":
				result["changes"] = changes[1:]
			case "duplicate":
				changes[1] = changes[0]
			case "extra":
				result["changes"] = append(changes, changes[0])
			case "foreign":
				changes[0].([]any)[0] = "0x" + strings.Repeat("ba", 36)
			case "wrong-block":
				result["block"] = testGenesisHash
			case "multiple-blocks":
				reply["result"] = append(results, result)
			case "malformed-pair":
				changes[0] = append(changes[0].([]any), "extra")
			case "null-result":
				reply["result"] = nil
			case "null-hotkey":
				changes[0].([]any)[1] = nil
			}
			raw, err = json.Marshal(reply)
			response.Body = io.NopCloser(bytes.NewReader(raw))
			return response, err
		})
		result, err := client.readSubnetDiscovery(t.Context(), snapshot, "sha256:synthetic-snapshot")
		if !errors.Is(err, errRpcIntegrity) || result.Schema != "" || result.MembershipComplete || len(result.Storage) != 0 || fixture.count("state_queryStorageAt") != 1 {
			t.Fatalf("%s batch escaped the closed census: %+v %v", fault, result, err)
		}
	}
}

// A successful first chunk cannot imply completeness when the last chunk is
// truncated. Staging remains unpublished until the entire requested set arrives.
func TestSubnetDiscoveryBatchRejectsPartialLaterChunk(t *testing.T) {
	client, fixture, _, snapshot := newSubnetDiscoveryFixture(t)
	subnetDiscoveryTestLargeMembership(t, fixture)
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, err := fixture.roundTrip(request)
		if err == nil && fixture.count("state_queryStorageAt") == 2 {
			response.Body.Close()
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": []any{map[string]any{"block": testFinalizedHash, "changes": []any{}}}})
			response.Body = io.NopCloser(bytes.NewReader(body))
		}
		return response, err
	})
	result, err := client.readSubnetDiscovery(t.Context(), snapshot, "sha256:synthetic-snapshot")
	if !errors.Is(err, errRpcIntegrity) || !strings.Contains(err.Error(), "cardinality") || result.Schema != "" || fixture.count("state_queryStorageAt") != 2 || fixture.count("state_getStorage") != 3 {
		t.Fatalf("partial later chunk reached row reads or output: %+v %v", result, err)
	}
}

// A null storage value survives as absence and uses only the authenticated
// metadata default; it is never collapsed into a recorded zero or empty hex.
func TestSubnetDiscoveryBatchPreservesNullDefaultAndOneUse(t *testing.T) {
	client, fixture, _, _ := newSubnetDiscoveryFixture(t)
	key, err := types.CreateStorageKey(fixture.metadata, "SubtensorModule", "NetworkRegistrationAllowed", []byte{25, 0})
	if err != nil {
		t.Fatal(err)
	}
	delete(fixture.storageKVs, key.Hex())
	reader := subnetDiscoveryTestReader(t, client, fixture, true)
	reader.preparedKVs, err = reader.readDiscoveryStorageBatches(t.Context(), []string{key.Hex()})
	if err != nil {
		t.Fatal(err)
	}
	value, err := reader.read(t.Context(), "NetworkRegistrationAllowed", []byte{25, 0})
	if err != nil || value.RawStorage != nil || value.ValueSource != "authenticated-metadata-fallback" || len(value.data) != 1 || len(reader.preparedKVs) != 0 || fixture.count("state_getStorage") != 0 {
		t.Fatalf("explicit null lost its metadata/default distinction: %+v %v", value, err)
	}
	fixture.set(t, "NetworkRegistrationAllowed", bytes.Clone(value.data), []byte{25, 0})
	if _, err := reader.read(t.Context(), "NetworkRegistrationAllowed", []byte{25, 0}); !errors.Is(err, errRpcIntegrity) || !strings.Contains(err.Error(), "storage changed") || fixture.count("state_getStorage") != 1 {
		t.Fatalf("prepared value was reused or absence became a recorded zero: %v", err)
	}
}

// Batches must use the ordinary SCALE checker after exact key matching; a
// well-formed JSON string cannot supply a wrong-width owner or UID value.
func TestSubnetDiscoveryBatchRejectsMalformedScale(t *testing.T) {
	for _, name := range []string{"Owner", "Uids", "BlockAtRegistration", "IsNetworkMember"} {
		client, fixture, _, snapshot := newSubnetDiscoveryFixture(t)
		hotkey := bytes.Repeat([]byte{0x41}, 32)
		args := [][]byte{hotkey}
		switch name {
		case "Uids":
			args = [][]byte{{25, 0}, hotkey}
		case "BlockAtRegistration":
			args = [][]byte{{25, 0}, {0, 0}}
		case "IsNetworkMember":
			args = [][]byte{hotkey, {25, 0}}
		}
		fixture.set(t, name, nil, args...)
		result, err := client.readSubnetDiscovery(t.Context(), snapshot, "sha256:synthetic-snapshot")
		if !errors.Is(err, errRpcIntegrity) || result.Schema != "" || fixture.count("state_queryStorageAt") != 2 {
			t.Fatalf("%s malformed SCALE survived batch transport: %+v %v", name, result, err)
		}
	}
}
