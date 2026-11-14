// Historical initialization selects raw metadata/version at one exact block.
// Observation alone cannot acquire a producer capability or signing approval.
package crv4

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// An unavailable latest interface cannot block reopening original metadata.
// Every physical constructor call is inspected for its exact original pin.
func TestDialChainAtContextRetainsHistoricalInitialization(t *testing.T) {
	metadata, _ := runtimeIdentityTestMetadata(t)
	genesis, block := types.Hash{0x71}, types.Hash{0x72}
	var stateLock sync.Mutex
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call chainContextRPCRequest
		if json.NewDecoder(request.Body).Decode(&call) != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		stateLock.Lock()
		calls = append(calls, call.Method)
		stateLock.Unlock()
		var result any
		switch call.Method {
		case "chain_getBlockHash":
			if len(call.Params) != 1 || string(call.Params[0]) != "0" {
				t.Error("historical constructor changed the genesis query")
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			result = genesis.Hex()
		case "state_getMetadata", "state_getRuntimeVersion":
			var selected string
			if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &selected) != nil || selected != block.Hex() {
				t.Error("historical constructor probed unrelated current metadata or version")
				_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "error": map[string]any{"code": -32602, "message": "only the synthetic historical interface exists"}})
				return
			}
			result = metadata
			if call.Method == "state_getRuntimeVersion" {
				result = map[string]any{"specName": "synthetic-historical", "specVersion": 9_001, "transactionVersion": 1, "stateVersion": 1}
			}
		default:
			t.Errorf("historical constructor requested %s", call.Method)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writeChainContextRPCResult(writer, call, result)
	}))
	t.Cleanup(server.Close)
	chain, err := DialChainAtContext(t.Context(), server.URL, block)
	if err != nil {
		t.Fatalf("approved historical initialization depended on the latest interface: %v", err)
	}
	t.Cleanup(chain.API.Client.Close)
	stateLock.Lock()
	actual := slices.Clone(calls)
	stateLock.Unlock()
	if chain.GenesisHash != genesis || chain.Runtime.SpecVersion != 9_001 || !slices.Equal(actual, []string{"state_getMetadata", "chain_getBlockHash", "state_getRuntimeVersion"}) {
		t.Fatalf("historical constructor changed its exact read census: %v", actual)
	}
	if err := chain.ValidateValidatorProducerRuntime(RuntimeArtifactIdentity{}); err == nil {
		t.Fatal("historical observation constructor granted producer capability")
	}
	if result, err := DialChainAtContext(t.Context(), server.URL, types.Hash{}); result != nil || err == nil {
		t.Fatal("historical constructor accepted an absent block")
	}
	stateLock.Lock()
	defer stateLock.Unlock()
	if !slices.Equal(actual, calls) {
		t.Fatal("invalid historical identity reached the physical transport")
	}
}
