//go:build linux || darwin

// Independent physical readers retain separate error ownership when activation
// joins them. A native verdict remains opaque to generic EVM retry classification.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Both real activation branches fail independently at their physical HTTP
// readers. The caller supplies the supported sixty-second minimum, rather
// than reducing the production budget or injecting an admission verdict.
func TestProductionStartupActivationJoinsIndependentReadCauses(t *testing.T) {
	fixture := newProductionStartupTestFixture(t)
	initial := fixture.contextKVs[9]
	production := fixture.continuation.production
	admission := production.operator.measurement.admission
	var nativeRefusals, evmRefusals atomic.Uint64
	nativeServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.NewDecoder(request.Body).Decode(&call) != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var result any
		switch call.Method {
		case "state_getMetadata":
			result = admission.metadata
		case "state_getRuntimeVersion":
			result = map[string]any{"specName": admission.version.SpecName, "specVersion": admission.version.SpecVersion, "transactionVersion": 1, "stateVersion": 1, "apis": []any{[]any{"0x8375104b299b74c5", 2}}}
		case "chain_getBlockHash":
			if len(call.Params) == 1 && string(call.Params[0]) == "0" {
				result = production.cfg.GenesisHash
				break
			}
			fallthrough
		default:
			nativeRefusals.Add(1)
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result})
	}))
	t.Cleanup(nativeServer.Close)
	native, err := crv4.DialChainAtContext(t.Context(), nativeServer.URL, types.Hash(initial.Activation.NativeHash))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(native.API.Client.Close)
	evmServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Id     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.NewDecoder(request.Body).Decode(&call) != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if call.Method != "eth_chainId" {
			evmRefusals.Add(1)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": "0x3c4"})
	}))
	t.Cleanup(evmServer.Close)
	evm, err := DialReleaseChainContext(t.Context(), []string{evmServer.URL}, common.Address(initial.Activation.Domain.Coordinator))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(evm.Close)
	physical := fixture.continuation.inputKVs[9]
	vpk, err := initial.Activation.SignVPK(physical.source.key)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := initial.Activation.Digest()
	hotkey, err := production.hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	_, result := evm.AuthenticateReleaseActivationV2Context(ctx, native, ReleaseActivationV2Authority{
		Expected: initial.Activation, Journal: common.Address(initial.Journal), RuntimeHash: initial.RuntimeHash, ValidatorUID: initial.ValidatorUID,
		NativeRuntime: releaseNativeRuntimeIdentity(production.cfg), productionRuntimeConfig: production.cfg,
	}, initial.Activation, vpk, hotkey, initial.ObservedEVMBlock, initial.ObservedEVMHash)
	var nativeStatus *crv4.SubstrateReadHttpStatusError
	var evmStatus gethrpc.HTTPError
	if result == nil || nativeRefusals.Load() == 0 || evmRefusals.Load() == 0 || !errors.As(result, &nativeStatus) || nativeStatus.StatusCode() != 502 || !errors.As(result, &evmStatus) || evmStatus.StatusCode != 500 || !retryableProductionSteeringRead(result) {
		t.Fatalf("actual parallel activation transient readers became a hard result: native=%d EVM=%d error=%v", nativeRefusals.Load(), evmRefusals.Load(), result)
	}
	missing := &crv4.ReceiptEvidenceUnavailableError{BlockHash: types.Hash(initial.Activation.NativeHash), Field: "body"}
	if !retryableProductionSteeringRead(errors.Join(result, missing)) {
		t.Fatal("independent missing receipt evidence erased a physical native read cause")
	}
	for _, hard := range []error{errors.New("synthetic canonical commitment contradiction"), context.Canceled, &os.PathError{Op: "close", Path: "synthetic-private-custody", Err: context.DeadlineExceeded}} {
		if retryableProductionSteeringRead(errors.Join(result, hard)) {
			t.Fatalf("independent transient readers masked a hard or canceled owner: %v", hard)
		}
	}
}
