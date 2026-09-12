//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	validatorpkg "github.com/urfoundation/sn/validator"
)

type finalV2PublicRPCFixture struct {
	reader *PublicFinalSemanticChainReader
	evmHead, nativeHead ChainHead
	calls, batches atomic.Uint64
	answer atomic.Uint64
	payloadBytes atomic.Uint64
}

func newFinalV2PublicRPCFixture(t *testing.T) *finalV2PublicRPCFixture {
	t.Helper()
	f := &finalV2PublicRPCFixture{evmHead: ChainHead{Number: 100, Hash: common.Hash{0x11}.Hex()}, nativeHead: ChainHead{Number: 71, Hash: common.Hash{0x22}.Hex()}}
	f.answer.Store(55)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var raw json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&raw); err != nil { t.Error(err); return }
		var calls []finalV2RPCRequest
		batch := len(raw) != 0 && raw[0] == '['
		if batch { f.batches.Add(1); if err := json.Unmarshal(raw, &calls); err != nil { t.Error(err); return } } else {
			calls = make([]finalV2RPCRequest, 1)
			if err := json.Unmarshal(raw, &calls[0]); err != nil { t.Error(err); return }
		}
		var replies []string
		for _, call := range calls {
			f.calls.Add(1)
			var result string
			switch call.Method {
			case "eth_chainId": result = fmt.Sprintf(`"%s"`, hexutil.EncodeUint64(testnetChainID))
			case "eth_getBlockByHash", "eth_getBlockByNumber": result = fmt.Sprintf(`{ "number" : "0x64", "hash" : %q }`, f.evmHead.Hash)
			case "eth_call":
				result = fmt.Sprintf(`"0x%064x"`, f.answer.Load())
				if n := f.payloadBytes.Load(); n != 0 { result = `"0x` + strings.Repeat("00", int(n)) + `"` }
			case "eth_getCode": result = `"0x6000"`
			default: t.Errorf("unexpected source method %s", call.Method); http.Error(writer, "unadmitted call", 400); return
			}
			replies = append(replies, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, call.ID, result))
		}
		writer.Header().Set("Content-Type", "application/json")
		if batch { fmt.Fprintf(writer, "[%s]", strings.Join(replies, ",")) } else { fmt.Fprint(writer, replies[0]) }
	}))
	t.Cleanup(server.Close)
	client, err := rpc.DialContext(t.Context(), server.URL)
	if err != nil { t.Fatal(err) }
	t.Cleanup(client.Close)
	policy := defaultFinalSemanticRPCRetryPolicy()
	policy.maximumAttempts = 1
	f.reader = &PublicFinalSemanticChainReader{evm: client, evmRetry: policy, canonicalEVMRPC: "https://public.example/evm", evidence: &FinalSemanticEvidence{ChainID: testnetChainID, GenesisHash: testnetGenesis, EVMTerminalHead: f.evmHead, NativeTerminalHead: f.nativeHead}}
	return f
}

func finalV2PublicReadFacade(t *testing.T, ctx context.Context, recorder *finalV2RPCRecorder) *rpc.Client {
	t.Helper()
	client, err := rpc.DialOptions(ctx, "http://validator-evidence-read.invalid", rpc.WithHTTPClient(&http.Client{Transport: &finalV2EVMReadTransport{owner: recorder}}))
	if err != nil { t.Fatal(err) }
	t.Cleanup(client.Close)
	return client
}

func TestFinalPublicValidatorSourcesV2KeepsDetachedEvidenceAuthority(t *testing.T) {
	t.Parallel()
	f := newFinalV2PublicRPCFixture(t)
	f.reader.evidence.PlanHash = common.Hash{0x31}.Hex()
	f.reader.evidence.ConfigHash = common.Hash{0x32}.Hex()
	f.reader.evidence.Window = ScenarioAcceptanceWindow{FirstEpoch: 300, EpochCount: 5}
	f.reader.evidence.ExitCriteria = []FinalExitCriterionEvidence{{ID: "source", Assertions: []FinalMetricAssertion{{Metric: "count", Expected: 4, Observed: 4}}}}
	target, err := finalSemanticEvidenceDetachedCopy(f.reader.evidence)
	if err != nil { t.Fatal(err) }
	target.EvidenceHash = common.Hash{0x33}.Hex()
	target.PublicVerification = &FinalPublicChainVerification{Schema: finalPublicChainVerificationSchema}
	owned, err := f.reader.finalV2InvocationReader(target)
	if err != nil || owned == nil || owned.evidence == target || owned.evidence == f.reader.evidence || owned.evm != f.reader.evm {
		t.Fatalf("detached producer/verifier source was refused or aliased: %v", err)
	}
	for _, fault := range []string{"plan", "config", "window", "terminal", "nested-source"} {
		t.Run(fault, func(t *testing.T) {
			changed, err := finalSemanticEvidenceDetachedCopy(target)
			if err != nil { t.Fatal(err) }
			switch fault {
			case "plan": changed.PlanHash = common.Hash{0x44}.Hex()
			case "config": changed.ConfigHash = common.Hash{0x44}.Hex()
			case "window": changed.Window.FirstEpoch++
			case "terminal": changed.NativeTerminalHead.Number++
			case "nested-source": changed.ExitCriteria[0].Assertions[0].Observed++
			}
			if got, err := f.reader.finalV2InvocationReader(changed); err == nil || got != nil {
				t.Fatal("different immutable evidence acquired reader authority")
			}
		})
	}
	f.reader.evidence.ExitCriteria[0].Assertions[0].Observed = 90
	target.ExitCriteria[0].Assertions[0].Observed = 91
	if owned.evidence.ExitCriteria[0].Assertions[0].Observed != 4 || target.EvidenceHash != common.Hash{0x33}.Hex() || target.PublicVerification == nil {
		t.Fatal("factory/caller mutation changed the invocation or admission rewrote sealing fields")
	}
	recorder := newFinalV2RPCRecorder(t.Context(), owned, 64*1024, 1024*1024)
	facade := finalV2PublicReadFacade(t, t.Context(), recorder)
	var chainID string
	if err := facade.CallContext(t.Context(), &chainID, "eth_chainId"); err != nil || chainID != hexutil.EncodeUint64(testnetChainID) {
		t.Fatalf("detached owner did not perform the real admitted read: %v", err)
	}
	if _, err := recorder.finish(); err != nil { t.Fatal(err) }
}

func TestFinalPublicValidatorSourcesV2RetainsRealBatchesAndCanonicalHeads(t *testing.T) {
	t.Parallel()
	f := newFinalV2PublicRPCFixture(t)
	recorder := newFinalV2RPCRecorder(t.Context(), f.reader, 64*1024, 1024*1024)
	facade := finalV2PublicReadFacade(t, t.Context(), recorder)
	chain, err := validatorpkg.NewReleaseChainReadRPCContext(t.Context(), facade, f.reader.canonicalEVMRPC, common.Address{1})
	if err != nil { t.Fatal(err) }
	netuid, err := chain.ReleaseNetuidAtHashContext(t.Context(), f.evmHead.Number, common.HexToHash(f.evmHead.Hash))
	if err != nil || netuid != 55 { t.Fatalf("actual typed canonical reader: netuid=%d err=%v", netuid, err) }
	var results [2]hexutil.Bytes
	selector := finalEVMBlockSelector{BlockHash: f.evmHead.Hash, RequireCanonical: true}
	batch := []rpc.BatchElem{
		{Method: "eth_call", Args: []any{map[string]string{"to": common.Address{1}.Hex(), "input": "0x0102"}, selector}, Result: &results[0]},
		{Method: "eth_call", Args: []any{map[string]string{"to": common.Address{1}.Hex(), "input": "0x0304"}, selector}, Result: &results[1]},
	}
	before := f.batches.Load()
	if err := facade.BatchCallContext(t.Context(), batch); err != nil { t.Fatal(err) }
	if f.batches.Load() != before+1 || batch[0].Error != nil || batch[1].Error != nil || new(big.Int).SetBytes(results[0]).Uint64() != 55 || new(big.Int).SetBytes(results[1]).Uint64() != 55 {
		t.Fatal("the actual two-call source batch was split or lost a result")
	}
	exchanges, err := recorder.finish()
	if err != nil { t.Fatal(err) }
	var originalHeader bool
	for _, exchange := range exchanges {
		if exchange.Chain != "evm" || exchange.PinnedHead != f.evmHead { t.Fatal("EVM result lost its exact EVM checkpoint") }
		requestHash, responseHash, err := finalRPCExchangeHashes(exchange)
		if err != nil || exchange.RequestHash != requestHash || exchange.ResponseHash != responseHash { t.Fatalf("transcript hashes changed: %v", err) }
		if exchange.Method == "eth_getBlockByHash" && bytes.Contains(exchange.Result, []byte(`{ "number" :`)) { originalHeader = true }
	}
	if !originalHeader { t.Fatal("original RPC result bytes were normalized before custody") }
	first := bytes.Clone(exchanges[0].Result)
	exchanges[0].Result[0] ^= 1
	again, err := recorder.finish()
	if err != nil || !bytes.Equal(again[0].Result, first) { t.Fatal("returned transcript aliases the completed audit") }
	// An independent invocation must still perform its own real source reads.
	independent := newFinalV2RPCRecorder(t.Context(), f.reader, 64*1024, 1024*1024)
	other := finalV2PublicReadFacade(t, t.Context(), independent)
	beforeCalls := f.calls.Load()
	var answer hexutil.Bytes
	if err := other.CallContext(t.Context(), &answer, batch[0].Method, batch[0].Args...); err != nil { t.Fatal(err) }
	if f.calls.Load() <= beforeCalls { t.Fatal("source transcript scope leaked across invocations") }
}

func TestFinalPublicValidatorSourcesV2RefusesWritesUnpinnedAndConflictingReads(t *testing.T) {
	t.Parallel()
	f := newFinalV2PublicRPCFixture(t)
	for _, call := range []struct { method string; args []any }{
		{"eth_sendRawTransaction", []any{"0x01"}},
		{"eth_call", []any{map[string]string{"to": common.Address{1}.Hex(), "input": "0x01"}, "latest"}},
		{"eth_call", []any{map[string]string{"to": common.Address{1}.Hex(), "input": "0x01"}, finalEVMBlockSelector{BlockHash: f.evmHead.Hash}}},
		{"eth_getLogs", []any{map[string]string{"fromBlock": "0x1", "toBlock": "latest"}}},
	} {
		recorder := newFinalV2RPCRecorder(t.Context(), f.reader, 64*1024, 1024*1024)
		facade := finalV2PublicReadFacade(t, t.Context(), recorder)
		before := f.calls.Load()
		var result json.RawMessage
		if err := facade.CallContext(t.Context(), &result, call.method, call.args...); err == nil || f.calls.Load() != before { t.Fatalf("unadmitted method reached real transport: %s", call.method) }
	}
	recorder := newFinalV2RPCRecorder(t.Context(), f.reader, 64*1024, 1024*1024)
	facade := finalV2PublicReadFacade(t, t.Context(), recorder)
	args := []any{map[string]string{"to": common.Address{1}.Hex(), "input": "0x01"}, finalEVMBlockSelector{BlockHash: f.evmHead.Hash, RequireCanonical: true}}
	var result hexutil.Bytes
	if err := facade.CallContext(t.Context(), &result, "eth_call", args...); err != nil { t.Fatal(err) }
	f.answer.Store(56)
	if err := facade.CallContext(t.Context(), &result, "eth_call", args...); err == nil { t.Fatal("changed immutable source answer was admitted") }
	if got, err := recorder.finish(); err == nil || got != nil { t.Fatal("conflicting source produced a partial successful transcript") }
	other := newFinalV2RPCRecorder(t.Context(), f.reader, 64*1024, 1024*1024)
	foreign := finalV2PublicReadFacade(t, t.Context(), other)
	args[1] = finalEVMBlockSelector{BlockHash: common.Hash{0x99}.Hex(), RequireCanonical: true}
	if err := foreign.CallContext(t.Context(), &result, "eth_call", args...); err == nil { t.Fatal("foreign hash was assigned the real canonical height") }
}

type finalV2NativeReadTestClient struct { *gsrpcgeth.Client; endpoint string }
func (self *finalV2NativeReadTestClient) URL() string { return self.endpoint }

func TestFinalPublicValidatorSourcesV2KeepsNativeClockAndNullStorage(t *testing.T) {
	t.Parallel()
	f := newFinalV2PublicRPCFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call finalV2RPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil { t.Error(err); return }
		var result string
		switch call.Method {
		case "chain_getHeader": result = `{"number":"0x47"}`
		case "chain_getBlockHash": result = fmt.Sprintf("%q", f.nativeHead.Hash)
		case "chain_getFinalizedHead": result = fmt.Sprintf("%q", common.Hash{0x33}.Hex())
		case "state_getStorage": result = "null"
		default: t.Errorf("unexpected native method %s", call.Method); http.Error(writer, "unexpected", 400); return
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%s,"result":%s}`, call.ID, result)
	}))
	t.Cleanup(server.Close)
	client, err := gsrpcgeth.DialContext(t.Context(), server.URL)
	if err != nil { t.Fatal(err) }
	t.Cleanup(client.Close)
	f.reader.native = &gsrpc.SubstrateAPI{Client: &finalV2NativeReadTestClient{Client: client, endpoint: server.URL}}
	recorder := newFinalV2RPCRecorder(t.Context(), f.reader, 64*1024, 1024*1024)
	params, _ := json.Marshal([]any{"0x0102", f.nativeHead.Hash})
	// GSRPC's nullable storage decoding is exercised by the production capture
	// adapter's adjacent controls; this retains that exact actual public result.
	var raw *json.RawMessage
	if err := client.CallContext(t.Context(), &raw, "state_getStorage", "0x0102", f.nativeHead.Hash); err != nil { t.Fatal(err) }
	if raw != nil { t.Fatal("actual absent storage was not JSON null") }
	if err := recorder.retainNative(t.Context(), validatorpkg.ReleaseEvidenceV2NativeRead{Schema: "urnetwork-validator-native-read-v2", Method: "state_getStorage", Parameters: params, Result: []byte("null")}); err != nil { t.Fatal(err) }
	exchanges, err := recorder.finish()
	if err != nil { t.Fatal(err) }
	var absent bool
	for _, exchange := range exchanges {
		if exchange.Chain != "substrate" || exchange.PinnedHead != f.nativeHead || exchange.PinnedHead.Number == f.evmHead.Number { t.Fatal("native read inherited the EVM numeric clock") }
		if exchange.Method == "state_getStorage" && bytes.Equal(exchange.Result, []byte("null")) { absent = true }
	}
	if !absent { t.Fatal("valid absent native storage was dropped") }
	before := len(exchanges)
	finalized, _ := json.Marshal(common.Hash{0x33}.Hex())
	if err := recorder.retainNative(t.Context(), validatorpkg.ReleaseEvidenceV2NativeRead{Schema: "urnetwork-validator-native-read-v2", Method: "chain_getFinalizedHead", Parameters: []byte("[]"), Result: finalized}); err != nil { t.Fatal(err) }
	if got, err := recorder.finish(); err != nil || len(got) != before { t.Fatal("moving finality was mislabeled as immutable historical state") }
	if err := recorder.retainNative(t.Context(), validatorpkg.ReleaseEvidenceV2NativeRead{Schema: "urnetwork-validator-native-read-v2", Method: "state_getStorage", Parameters: []byte(`["0x0102"]`), Result: []byte("null")}); err == nil { t.Fatal("unpinned native storage gained historical authority") }
}

func TestFinalPublicValidatorSourcesV2CancelsAndJoinsActiveTransport(t *testing.T) {
	t.Parallel()
	entered, joined := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		close(entered)
		<-request.Context().Done()
		close(joined)
	}))
	t.Cleanup(server.Close)
	client, err := rpc.DialContext(t.Context(), server.URL)
	if err != nil { t.Fatal(err) }
	t.Cleanup(client.Close)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	policy := defaultFinalSemanticRPCRetryPolicy(); policy.maximumAttempts = 1
	reader := &PublicFinalSemanticChainReader{evm: client, evmRetry: policy}
	recorder := newFinalV2RPCRecorder(ctx, reader, 4096, 8192)
	facade := finalV2PublicReadFacade(t, t.Context(), recorder)
	returned := make(chan error, 1)
	go func() { var result string; returned <- facade.CallContext(t.Context(), &result, "eth_chainId") }()
	select { case <-entered: case <-time.After(2*time.Second): t.Fatal("actual RPC body never entered") }
	cancel()
	select { case err := <-returned: if err == nil { t.Fatal("cancelled source succeeded") }; case <-time.After(2*time.Second): t.Fatal("cancelled facade did not return") }
	select { case <-joined: case <-time.After(2*time.Second): t.Fatal("actual source request was not joined") }
	if got, err := recorder.finish(); err == nil || got != nil { t.Fatal("cancelled source yielded a transcript") }
}

func TestFinalPublicValidatorSourcesV2BoundsRejectPartialReadResults(t *testing.T) {
	t.Parallel()
	f := newFinalV2PublicRPCFixture(t)
	f.payloadBytes.Store(1024)
	recorder := newFinalV2RPCRecorder(t.Context(), f.reader, 512, 1024*1024)
	facade := finalV2PublicReadFacade(t, t.Context(), recorder)
	var output string = "unchanged"
	if err := facade.CallContext(t.Context(), &output, "eth_call", map[string]string{"to": common.Address{1}.Hex(), "input": "0x01"}, finalEVMBlockSelector{BlockHash: f.evmHead.Hash, RequireCanonical: true}); err == nil || output != "unchanged" || f.calls.Load() == 0 {
		t.Fatal("oversized actual RPC response bypassed its decode bound or changed output")
	}
	if got, err := recorder.finish(); err == nil || got != nil { t.Fatal("oversized source response left a successful partial transcript") }
	bounded := newFinalV2RPCRecorder(t.Context(), f.reader, 64*1024, 1)
	limited := finalV2PublicReadFacade(t, t.Context(), bounded)
	if err := limited.CallContext(t.Context(), &output, "eth_chainId"); err == nil || output != "unchanged" { t.Fatal("aggregate byte refusal exposed an uncaptured source fact") }
	if got, err := bounded.finish(); err == nil || got != nil { t.Fatal("exhausted transcript allowance produced a partial success") }
}
