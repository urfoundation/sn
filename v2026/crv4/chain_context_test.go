package crv4

// chain_context_test.go exercises the context-aware replacement for GSRPC's
// convenience initialization and finalized-head calls using only local RPCs.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

type chainContextRPCRequest struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

// Builds the exact map metadata needed by release schedule, weights, and
// canonical-nonce readers. Identity hashers keep this fixture small while the
// production code still derives each storage key from authenticated metadata.
func releaseContextStorageMetadata() *types.Metadata {
	mapEntry := func(name string, hashers int) types.StorageEntryMetadataV14 {
		entryHashers := make([]types.StorageHasherV10, hashers)
		for index := range entryHashers {
			entryHashers[index] = types.StorageHasherV10{IsIdentity: true}
		}
		return types.StorageEntryMetadataV14{
			Name: types.Text(name),
			Type: types.StorageEntryTypeV14{
				IsMap: true,
				AsMap: types.MapTypeV14{Hashers: entryHashers},
			},
		}
	}
	metadata := types.NewMetadataV14()
	metadata.AsMetadataV14.Pallets = []types.PalletMetadataV14{
		{
			Name:       types.Text(PalletName),
			HasStorage: true,
			Storage: types.StorageMetadataV14{
				Prefix: types.Text(PalletName),
				Items: []types.StorageEntryMetadataV14{
					mapEntry("Weights", 2),
					mapEntry("LastEpochBlock", 1),
					mapEntry("PendingEpochAt", 1),
					mapEntry("SubnetEpochIndex", 1),
					mapEntry("BlocksSinceLastStep", 1),
					mapEntry("Tempo", 1),
				},
			},
		},
		{
			Name:       types.Text("System"),
			HasStorage: true,
			Storage: types.StorageMetadataV14{
				Prefix: types.Text("System"),
				Items:  []types.StorageEntryMetadataV14{mapEntry("Account", 1)},
			},
		},
	}
	return metadata
}

// Writes one minimally valid JSON-RPC success response for the request ID.
func writeChainContextRPCResult(writer http.ResponseWriter, request chainContextRPCRequest, result any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      request.ID,
		"result":  result,
	})
}

// Dial initialization must use exactly three caller-cancellable RPCs rather
// than GSRPC's contextless NewSubstrateAPI/NewRPC metadata helpers.
func TestDialChainContextInitializesWithExactContextRPCs(t *testing.T) {
	metadata, _ := runtimeIdentityTestMetadata(t)
	genesis := types.Hash{8}
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var rpcRequest chainContextRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil {
			t.Errorf("decode dial RPC request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		calls = append(calls, rpcRequest.Method)
		switch rpcRequest.Method {
		case "state_getMetadata":
			if len(rpcRequest.Params) != 0 {
				t.Errorf("metadata params=%s", rpcRequest.Params)
			}
			writeChainContextRPCResult(writer, rpcRequest, metadata)
		case "chain_getBlockHash":
			if len(rpcRequest.Params) != 1 || string(rpcRequest.Params[0]) != "0" {
				t.Errorf("genesis params=%s", rpcRequest.Params)
			}
			writeChainContextRPCResult(writer, rpcRequest, genesis.Hex())
		case "state_getRuntimeVersion":
			if len(rpcRequest.Params) != 0 {
				t.Errorf("runtime version params=%s", rpcRequest.Params)
			}
			writeChainContextRPCResult(writer, rpcRequest, map[string]any{
				"apis":               []any{},
				"authoringVersion":   1,
				"implName":           "test",
				"implVersion":        1,
				"specName":           "node-subtensor",
				"specVersion":        454,
				"transactionVersion": 1,
			})
		default:
			t.Errorf("unexpected dial RPC %s", rpcRequest.Method)
			writer.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	chain, err := DialChainContext(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	if chain.GenesisHash != genesis || chain.Meta == nil || chain.Runtime == nil || chain.Runtime.SpecName != "node-subtensor" || uint32(chain.Runtime.SpecVersion) != 454 {
		t.Fatalf("dialed chain=%+v", chain)
	}
	if len(calls) != 3 || calls[0] != "state_getMetadata" || calls[1] != "chain_getBlockHash" || calls[2] != "state_getRuntimeVersion" {
		t.Fatalf("dial RPC sequence=%v", calls)
	}
}

// A stalled initialization metadata read must leave DialChainContext as soon
// as the caller cancels, without relying on transport-default timeouts.
func TestDialChainContextCancelsStalledInitialization(t *testing.T) {
	metadataStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var rpcRequest chainContextRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&rpcRequest); err != nil || rpcRequest.Method != "state_getMetadata" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		close(metadataStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := DialChainContext(ctx, server.URL)
		done <- err
	}()
	<-metadataStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled dial error=%v", err)
	}
}

// Finalized-head reads are a first-class release boundary, so a canceled
// caller must not be silently replaced with context.Background by GSRPC.
func TestFinalizedHeadContextCancelsPublicRPC(t *testing.T) {
	started := make(chan struct{})
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, _ any, method string, args ...any) error {
		if method != "chain_getFinalizedHead" || len(args) != 0 {
			return fmt.Errorf("unexpected finalized-head RPC %s args=%v", method, args)
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := FinalizedHeadContext(ctx, chain)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled finalized-head error=%v", err)
	}
}

// Native publish allocates its nonce after runtime authentication, so that
// final RPC must retain the same operation cancellation boundary as well.
func TestAccountNonceContextCancelsPublicRPC(t *testing.T) {
	started := make(chan struct{})
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, _ any, method string, args ...any) error {
		if method != "system_accountNextIndex" || len(args) != 1 || args[0] != "5TestHotkey" {
			return fmt.Errorf("unexpected account nonce RPC %s args=%v", method, args)
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := chain.AccountNonceContext(ctx, "5TestHotkey")
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled account nonce error=%v", err)
	}
}

// Every release state helper must carry one caller context and one explicit
// state root. Returning null exercises the legitimate on-chain-default path
// without depending on runtime-specific SCALE fixtures.
func TestReleaseStateReadersUseExactBlockAndCallerContext(t *testing.T) {
	type callerContextKey struct{}
	const callerContextValue = "release-state"
	blockHash := types.Hash{12}
	storageCalls := 0
	headerCalls := 0
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if ctx.Value(callerContextKey{}) != callerContextValue {
			return errors.New("release state reader lost caller context")
		}
		switch method {
		case "chain_getHeader":
			headerCalls++
			if len(args) != 1 || args[0] != blockHash.Hex() {
				return fmt.Errorf("header args=%v, want exact block %s", args, blockHash.Hex())
			}
			*(result.(*types.Header)) = types.Header{Number: types.BlockNumber(42)}
			return nil
		case "state_getStorage":
			storageCalls++
			if len(args) != 2 || args[1] != blockHash.Hex() {
				return fmt.Errorf("storage args=%v, want exact block %s", args, blockHash.Hex())
			}
			return setRuntimeIdentityTestResult(result, nil)
		default:
			return fmt.Errorf("unexpected release state RPC %s", method)
		}
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: releaseContextStorageMetadata()}
	ctx := context.WithValue(context.Background(), callerContextKey{}, callerContextValue)
	header, err := chain.HeaderAtContext(ctx, blockHash)
	if err != nil || header.Number != 42 {
		t.Fatalf("header=%+v err=%v", header, err)
	}
	weights, err := chain.WeightsAtContext(ctx, 7, 3, blockHash)
	if err != nil || len(weights) != 0 {
		t.Fatalf("weights=%v err=%v", weights, err)
	}
	state, err := chain.EpochScheduleStateAtContext(ctx, 7, blockHash)
	if err != nil || state.CurrentBlock != 42 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	nonce, err := chain.AccountNonceAtContext(ctx, [32]byte{9}, blockHash)
	if err != nil || nonce != 0 {
		t.Fatalf("nonce=%d err=%v", nonce, err)
	}
	if headerCalls != 2 || storageCalls != 7 {
		t.Fatalf("header/storage calls=%d/%d, want 2/7", headerCalls, storageCalls)
	}
}

// A public provider can stall at any first boundary. Each release helper must
// therefore leave as soon as its caller cancels instead of entering GSRPC's
// contextless state/header convenience methods.
func TestReleaseStateReadersCancelAtFirstRPC(t *testing.T) {
	blockHash := types.Hash{13}
	for _, test := range []struct {
		name   string
		method string
		call   func(context.Context, *Chain) error
	}{
		{
			name:   "header",
			method: "chain_getHeader",
			call: func(ctx context.Context, chain *Chain) error {
				_, err := chain.HeaderAtContext(ctx, blockHash)
				return err
			},
		},
		{
			name:   "weights",
			method: "state_getStorage",
			call: func(ctx context.Context, chain *Chain) error {
				_, err := chain.WeightsAtContext(ctx, 7, 3, blockHash)
				return err
			},
		},
		{
			name:   "schedule",
			method: "chain_getHeader",
			call: func(ctx context.Context, chain *Chain) error {
				_, err := chain.EpochScheduleStateAtContext(ctx, 7, blockHash)
				return err
			},
		},
		{
			name:   "canonical nonce",
			method: "state_getStorage",
			call: func(ctx context.Context, chain *Chain) error {
				_, err := chain.AccountNonceAtContext(ctx, [32]byte{9}, blockHash)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, _ any, method string, args ...any) error {
				if method != test.method {
					return fmt.Errorf("RPC method=%s, want %s", method, test.method)
				}
				if len(args) == 0 || args[len(args)-1] != blockHash.Hex() {
					return fmt.Errorf("RPC args=%v, want exact block %s", args, blockHash.Hex())
				}
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}}
			chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: releaseContextStorageMetadata()}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- test.call(ctx, chain) }()
			<-started
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled %s error=%v", test.name, err)
			}
		})
	}
}

// This opt-in WebSocket regression exercises the production transport, manual
// context-aware initialization, and complete v454 artifact authentication.
// Local HTTP fixtures above cannot prove the GSRPC WebSocket adapter path.
func TestLiveDialChainContextAuthenticatesRuntime454(t *testing.T) {
	if os.Getenv("CRV4_LIVE_DIAL_CONTEXT") != "1" {
		t.Skip("set CRV4_LIVE_DIAL_CONTEXT=1 to qualify the public WebSocket endpoint")
	}
	endpoint := os.Getenv("CRV4_LIVE_DIAL_ENDPOINT")
	if endpoint == "" {
		endpoint = "wss://test.finney.opentensor.ai:443"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	chain, err := DialChainContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	finalized, err := FinalizedHeadContext(ctx, chain)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := AuthenticateRuntimeArtifactAtContext(ctx, chain, finalized, RuntimeArtifactIdentity{
		Version: RuntimeVersionIdentity{
			SpecName:           "node-subtensor",
			SpecVersion:        454,
			TransactionVersion: 1,
			StateVersion:       1,
		},
		CodeHash:     "0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef",
		MetadataHash: "0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702",
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.BlockHash != finalized {
		t.Fatalf("authenticated block=%s, want finalized=%s", artifact.BlockHash.Hex(), finalized.Hex())
	}
	t.Logf("endpoint=%s finalized_hash=%s tuple=%s/%d/%d/%d code=%s metadata=%s", endpoint, finalized.Hex(), artifact.Version.SpecName, artifact.Version.SpecVersion, artifact.Version.TransactionVersion, artifact.Version.StateVersion, artifact.CodeHash, artifact.MetadataHash)
}
