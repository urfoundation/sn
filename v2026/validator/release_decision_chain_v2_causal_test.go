//go:build linux || darwin

// Real compact M8 head collection must reject noncanonical binding RPC bytes
// before opening a proof stream or publishing an EMA preview. These controls
// retain the old production entrypoint and compile under its exact preimage.
package validator

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urnetwork/connect/v2026"
)

// The real existing RPC fixture handles the request first. Only its serialized
// binding ABI result is extended; geth still performs actual batch ID matching,
// hash selection and body decoding. Header responses are left untouched.
func releaseDecisionV2TestBindingSuffix(t *testing.T, fixture *releaseHeadV2TestFixture, suffix string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response := httptest.NewRecorder()
		fixture.rpc.ServeHTTP(response, request)
		var value any
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		appendSuffix := func(item map[string]any) {
			if result, ok := item["result"].(string); ok && strings.HasPrefix(result, "0x") {
				item["result"] = result + suffix
			}
		}
		switch item := value.(type) {
		case []any:
			for _, entry := range item {
				appendSuffix(entry.(map[string]any))
			}
		case map[string]any:
			appendSuffix(item)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(response.Code)
		_ = json.NewEncoder(writer).Encode(value)
	}))
	client, err := gethrpc.DialContext(t.Context(), server.URL)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	prior := fixture.steerer.chain
	fixture.steerer.chain = &ChainClient{client: ethclient.NewClient(client), coordinator: prior.coordinator, chainId: new(big.Int).Set(prior.chainId), contractAddr: prior.contractAddr, release: true}
}

// An active binding's accepted ABI prefix cannot hide trailing wire data.
func TestReleaseEvidenceV2DecisionLiveHeadRejectsActiveBindingSuffix(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	releaseDecisionV2TestBindingSuffix(t, fixture, strings.Repeat("00", 32))
	options := fixture.options(t)
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	if err == nil {
		t.Fatal("live head accepted active binding ABI suffix")
	}
	if !strings.Contains(err.Error(), "noncanonical") || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("noncanonical active binding reached genuine proof replay or retained a result: %v", err)
	}
	fixture.assertNoEMACommit(t)
}

// Inactivity does not exempt a real binding response from exact wire identity.
func TestReleaseEvidenceV2DecisionLiveHeadRejectsInactiveBindingSuffix(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	for id, binding := range fixture.rpc.bindingKVs {
		binding.Active = false
		fixture.rpc.bindingKVs[id] = binding
	}
	releaseDecisionV2TestBindingSuffix(t, fixture, strings.Repeat("00", 64))
	options := fixture.options(t)
	reads := observeReleaseMeasurementV2SettlementTest(&options)
	result, err := fixture.gather(t.Context(), options)
	if err == nil {
		t.Fatal("live head accepted inactive binding ABI suffix")
	}
	if !strings.Contains(err.Error(), "noncanonical") || *reads != 0 || !reflect.DeepEqual(result, releaseHeadResult{}) {
		t.Fatalf("noncanonical inactive binding reached genuine proof replay or retained a result: %v", err)
	}
	fixture.assertNoEMACommit(t)
}

// A genuine first-operator key callback cannot retarget the second operator's
// RPC through the retained mutable ChainClient object.
func TestReleaseEvidenceV2DecisionLiveHeadOwnsRoutingAcrossClientKeyCallback(t *testing.T) {
	t.Parallel()
	fixture := newReleaseHeadV2TestFixture(t, 2)
	options := fixture.options(t)
	wantBindings := slices.Clone(fixture.measurement.artifact.Bindings)
	lookup := fixture.steerer.contexts[9].ClientKey
	mutated := false
	fixture.steerer.contexts[9].ClientKey = func(clientID connect.Id) ([32]byte, bool, error) {
		if !mutated {
			mutated = true
			fixture.steerer.chain.contractAddr[0] ^= 1
		}
		return lookup(clientID)
	}
	result, err := fixture.gather(t.Context(), options)
	if err != nil {
		t.Fatalf("live head client-key callback retargeted retained chain routing: %v", err)
	}
	_, batches := fixture.rpc.bindingCounts()
	if !mutated || batches != 2 || !reflect.DeepEqual(releaseClientKeyTestChainBindings(result.Bindings), wantBindings) {
		t.Fatalf("real callback did not preserve the exact complete live binding census: mutated=%v batches=%d", mutated, batches)
	}
	for _, binding := range result.Bindings {
		if binding.Active && binding.ClientKeyObservationHash == "" {
			t.Fatal("owned live routing lost its actual client-key observation identity")
		}
	}
	fixture.assertNoEMACommit(t)
}
