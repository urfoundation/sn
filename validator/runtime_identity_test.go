package validator

// runtime_identity_test.go deterministically proves the release validator
// refuses an unreviewed native artifact before replacing its signing metadata.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/crv4"
)

// Supplies only the raw RPC calls made by the validator runtime gate.
type validatorRuntimeIdentityTestClient struct {
	callContext func(context.Context, any, string, ...any) error
}

// Keeps unexpected contextless reads visible to the test fixture.
func (self *validatorRuntimeIdentityTestClient) Call(result any, method string, args ...any) error {
	return self.callContext(context.Background(), result, method, args...)
}

// Propagates exact-block authentication context to the scripted provider.
func (self *validatorRuntimeIdentityTestClient) CallContext(ctx context.Context, result any, method string, args ...any) error {
	return self.callContext(ctx, result, method, args...)
}

// Runtime authentication has no subscription surface.
func (self *validatorRuntimeIdentityTestClient) Subscribe(context.Context, string, string, string, string, any, ...any) (*gsrpcgeth.ClientSubscription, error) {
	return nil, errors.New("unexpected validator runtime subscription")
}

// Gives test diagnostics a stable endpoint identity.
func (self *validatorRuntimeIdentityTestClient) URL() string { return "wss://validator-runtime.test" }

// The fixture owns no network connection.
func (self *validatorRuntimeIdentityTestClient) Close() {}

// Produces valid but non-release metadata so its exact content hash differs.
func validatorRuntimeIdentityTestMetadata(t *testing.T) string {
	t.Helper()
	metadata := types.NewMetadataV14()
	metadata.MagicNumber = types.MagicNumber
	encoded, err := codec.EncodeToHex(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := crv4.DecodeRuntimeMetadata(encoded); err != nil {
		t.Fatal(err)
	}
	return encoded
}

// Assigns the two concrete raw RPC result types exercised by this boundary.
func setValidatorRuntimeIdentityTestResult(result any, value any) error {
	switch target := result.(type) {
	case *json.RawMessage:
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		*target = encoded
		return nil
	case *string:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("test RPC string result has type %T", value)
		}
		*target = text
		return nil
	default:
		return fmt.Errorf("unexpected validator runtime RPC result type %T", result)
	}
}

// Covers version, transaction, state, code, and metadata drift in the actual
// release-validator adapter, including the no-partial-binding invariant.
func TestAuthenticatePinnedNativeRuntimeRejectsAdjacentArtifactDrift(t *testing.T) {
	metadata := validatorRuntimeIdentityTestMetadata(t)
	cfg := &ReleaseConfig{
		RuntimeSpec:         releaseRuntimeSpecVersion,
		TransactionVersion:  releaseRuntimeTransactionVersion,
		StateVersion:        releaseRuntimeStateVersion,
		RuntimeCodeHash:     releaseRuntimeCodeHash,
		RuntimeMetadataHash: releaseRuntimeMetadataHash,
	}
	cases := []struct {
		name              string
		version           crv4.RuntimeVersionIdentity
		codeHash          string
		metadata          string
		wantCodeCalls     int
		wantMetadataCalls int
	}{
		{name: "preceding runtime spec", version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 453, TransactionVersion: 1, StateVersion: 1}, codeHash: cfg.RuntimeCodeHash, metadata: metadata},
		{name: "transaction version drift", version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 454, TransactionVersion: 2, StateVersion: 1}, codeHash: cfg.RuntimeCodeHash, metadata: metadata},
		{name: "state version drift", version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 454, TransactionVersion: 1, StateVersion: 2}, codeHash: cfg.RuntimeCodeHash, metadata: metadata},
		{name: "code hash drift", version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 454, TransactionVersion: 1, StateVersion: 1}, codeHash: "0x825e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef", metadata: metadata, wantCodeCalls: 1},
		{name: "metadata hash drift", version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 454, TransactionVersion: 1, StateVersion: 1}, codeHash: cfg.RuntimeCodeHash, metadata: metadata, wantCodeCalls: 1, wantMetadataCalls: 1},
	}
	for index, testCase := range cases {
		codeCalls, metadataCalls := 0, 0
		blockHash := types.Hash{byte(index + 1)}
		client := &validatorRuntimeIdentityTestClient{callContext: func(_ context.Context, result any, method string, args ...any) error {
			switch method {
			case "state_getRuntimeVersion":
				if len(args) != 1 || args[0] != blockHash.Hex() {
					return fmt.Errorf("runtime version args=%v, want %s", args, blockHash.Hex())
				}
				return setValidatorRuntimeIdentityTestResult(result, testCase.version)
			case "state_getStorageHash":
				codeCalls++
				if len(args) != 2 || args[0] != "0x3a636f6465" || args[1] != blockHash.Hex() {
					return fmt.Errorf("code hash args=%v, want System.Code at %s", args, blockHash.Hex())
				}
				return setValidatorRuntimeIdentityTestResult(result, testCase.codeHash)
			case "state_getMetadata":
				metadataCalls++
				if len(args) != 1 || args[0] != blockHash.Hex() {
					return fmt.Errorf("metadata args=%v, want %s", args, blockHash.Hex())
				}
				return setValidatorRuntimeIdentityTestResult(result, testCase.metadata)
			default:
				return fmt.Errorf("unexpected RPC method %s", method)
			}
		}}
		oldMetadata := types.NewMetadataV14()
		oldRuntime := &types.RuntimeVersion{SpecName: "old-runtime", SpecVersion: 7, TransactionVersion: 8}
		chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: oldMetadata, Runtime: oldRuntime}
		if err := authenticatePinnedNativeRuntimeAtContext(context.Background(), chain, cfg, blockHash); err == nil {
			t.Errorf("%s was accepted", testCase.name)
		}
		if codeCalls != testCase.wantCodeCalls || metadataCalls != testCase.wantMetadataCalls {
			t.Errorf("%s calls code/metadata=%d/%d, want %d/%d", testCase.name, codeCalls, metadataCalls, testCase.wantCodeCalls, testCase.wantMetadataCalls)
		}
		if chain.Meta != oldMetadata || chain.Runtime != oldRuntime {
			t.Errorf("%s changed the prior chain binding", testCase.name)
		}
	}
}

// A canceled release steering iteration must interrupt the first exact-block
// runtime RPC; the adapter must never substitute context.Background.
func TestAuthenticatePinnedNativeRuntimeAtContextCancelsPublicRPC(t *testing.T) {
	blockHash := types.Hash{9}
	runtimeReadStarted := make(chan struct{})
	client := &validatorRuntimeIdentityTestClient{callContext: func(ctx context.Context, _ any, method string, args ...any) error {
		if method != "state_getRuntimeVersion" || len(args) != 1 || args[0] != blockHash.Hex() {
			return fmt.Errorf("unexpected canceled validator runtime RPC %s args=%v", method, args)
		}
		close(runtimeReadStarted)
		<-ctx.Done()
		return ctx.Err()
	}}
	cfg := &ReleaseConfig{
		RuntimeSpec:         releaseRuntimeSpecVersion,
		TransactionVersion:  releaseRuntimeTransactionVersion,
		StateVersion:        releaseRuntimeStateVersion,
		RuntimeCodeHash:     releaseRuntimeCodeHash,
		RuntimeMetadataHash: releaseRuntimeMetadataHash,
	}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: types.NewMetadataV14(), Runtime: &types.RuntimeVersion{SpecName: "old-runtime"}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- authenticatePinnedNativeRuntimeAtContext(ctx, chain, cfg, blockHash)
	}()
	<-runtimeReadStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled validator runtime authentication error=%v", err)
	}
}
