// Runtime-shape regressions protect the testnet setup path from generic
// Substrate assumptions and similarly named storage items.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	gsrpcblock "github.com/centrifuge/go-substrate-rpc-client/v4/types/block"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/crv4"
)

func TestFinalizedCheckpointWaitsForReadSurfaceAfterSubscription(t *testing.T) {
	target := ChainHead{Number: 100, Hash: "0x" + strings.Repeat("11", 32)}
	calls := 0
	err := waitForFinalizedCheckpoint(context.Background(), target, time.Millisecond, func() (ChainHead, string, error) {
		calls++
		switch calls {
		case 1:
			return ChainHead{Number: 99, Hash: "0x" + strings.Repeat("22", 32)}, "", nil
		case 2:
			return ChainHead{}, "", errors.New("temporary RPC front lag")
		default:
			return ChainHead{Number: 101, Hash: "0x" + strings.Repeat("33", 32)}, target.Hash, nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("checkpoint observations = %d, want 3", calls)
	}
}

func TestFinalizedCheckpointRejectsCanonicalMismatch(t *testing.T) {
	target := ChainHead{Number: 100, Hash: "0x" + strings.Repeat("11", 32)}
	err := waitForFinalizedCheckpoint(context.Background(), target, time.Millisecond, func() (ChainHead, string, error) {
		return ChainHead{Number: 100, Hash: target.Hash}, "0x" + strings.Repeat("22", 32), nil
	})
	if err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("canonical mismatch was accepted: %v", err)
	}
}

func TestFinalizedCheckpointWaitHonorsContext(t *testing.T) {
	target := ChainHead{Number: 100, Hash: "0x" + strings.Repeat("11", 32)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	err := waitForFinalizedCheckpoint(ctx, target, time.Millisecond, func() (ChainHead, string, error) {
		return ChainHead{Number: 99, Hash: target.Hash}, "", nil
	})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("checkpoint wait ignored its deadline: %v", err)
	}
}

// Keeps cancellation attached to the initial authenticated-head read before a
// recovered transaction scan or rebroadcast can begin.
func TestWatchRawPropagatesCallerContextToFinalizedHead(t *testing.T) {
	type callerContextKey struct{}
	const callerContextValue = "watch-raw"
	var reachedOnce sync.Once
	reached := make(chan struct{})
	client := &releaseRuntimeTestClient{callContext: func(ctx context.Context, _ any, method string, _ ...any) error {
		if method != "chain_getFinalizedHead" {
			return errors.New("unexpected watch RPC")
		}
		reachedOnce.Do(func() { close(reached) })
		if ctx.Value(callerContextKey{}) != callerContextValue {
			return errors.New("watch lost caller context")
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	manager := &SubstrateManager{
		chain: &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}},
		cfg:   testResolvedConfig(t),
	}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), callerContextKey{}, callerContextValue))
	done := make(chan error, 1)
	go func() {
		_, _, err := manager.watchRaw(ctx, "plan", Action{ID: "action", IntentHash: "intent"}, nil, types.Hash{1}, 1, types.Hash{2}.Hex(), true)
		done <- err
	}()
	<-reached
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "lost caller context") {
		t.Fatalf("canceled watch error=%v", err)
	}
}

// Keeps recovered-receipt authentication inside the watcher's cancellation
// boundary before any durable finalized entry can be appended.
func TestAppendRecoveredFinalityPropagatesCallerContext(t *testing.T) {
	type callerContextKey struct{}
	const callerContextValue = "recovered-finality"
	var reachedOnce sync.Once
	reached := make(chan struct{})
	client := &releaseRuntimeTestClient{callContext: func(ctx context.Context, _ any, method string, _ ...any) error {
		if method != "state_getRuntimeVersion" {
			return errors.New("unexpected recovered-finality RPC")
		}
		reachedOnce.Do(func() { close(reached) })
		if ctx.Value(callerContextKey{}) != callerContextValue {
			return errors.New("recovered finality lost caller context")
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	manager := &SubstrateManager{
		chain:   &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}},
		cfg:     testResolvedConfig(t),
		journal: &Journal{},
	}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), callerContextKey{}, callerContextValue))
	done := make(chan error, 1)
	go func() {
		_, _, err := manager.appendRecoveredFinality(
			ctx,
			"plan",
			Action{ID: "action", IntentHash: "intent"},
			types.Hash{1},
			&crv4.FinalizedExtrinsic{BlockHash: types.Hash{2}, BlockNumber: 9},
			8,
			types.Hash{3}.Hex(),
		)
		done <- err
	}()
	<-reached
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "lost caller context") {
		t.Fatalf("canceled recovered finality error=%v", err)
	}
}

// Uses the exact authenticated metadata on a private chain copy, leaving the
// shared signing metadata untouched while proving the receipt.
func TestAuthenticatedFinalizedExtrinsicBindsExactMetadataAndContext(t *testing.T) {
	type callerContextKey struct{}
	const callerContextValue = "finalized-receipt"
	metadata, encodedEventsJSON := finalSemanticSuccessfulDispatchFixture(t, 0)
	var encodedEvents string
	if err := json.Unmarshal(encodedEventsJSON, &encodedEvents); err != nil {
		t.Fatal(err)
	}
	raw := []byte{1, 2, 3, 4}
	digest := blake2b.Sum256(raw)
	extrinsicHash := types.Hash(digest)
	blockHash := types.Hash{9}
	calls := 0
	client := &releaseRuntimeTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if ctx.Value(callerContextKey{}) != callerContextValue {
			return errors.New("finalized receipt lost caller context")
		}
		if len(args) == 0 || args[len(args)-1] != blockHash.Hex() {
			return errors.New("finalized receipt changed block hash")
		}
		calls++
		switch method {
		case "chain_getBlock":
			*(result.(*gsrpcblock.SignedBlock)) = gsrpcblock.SignedBlock{Block: gsrpcblock.Block{Extrinsics: []string{codec.HexEncodeToString(raw)}}}
		case "state_getStorage":
			*(result.(*string)) = encodedEvents
		default:
			return errors.New("unexpected finalized receipt RPC")
		}
		return nil
	}}
	sharedMetadata := types.NewMetadataV14()
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: sharedMetadata}
	authenticated := authenticatedRuntimeMetadata{
		FinalizedHash: blockHash,
		Version:       crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1},
		CodeHash:      "0x" + strings.Repeat("11", 32),
		MetadataHash:  "0x" + strings.Repeat("22", 32),
		Metadata:      metadata,
	}
	ctx := context.WithValue(context.Background(), callerContextKey{}, callerContextValue)
	if err := verifyAuthenticatedFinalizedExtrinsicContext(ctx, chain, authenticated, blockHash, extrinsicHash); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("finalized receipt RPC calls=%d, want 2", calls)
	}
	if chain.Meta != sharedMetadata {
		t.Fatal("receipt verification mutated shared signing metadata")
	}
}

// Rejects a receipt/runtime association unless every carried identity field is
// present and the authenticated block is the exact receipt block.
func TestAuthenticatedFinalizedExtrinsicRejectsMismatchedAndIncompleteIdentity(t *testing.T) {
	blockHash := types.Hash{9}
	extrinsicHash := types.Hash{8}
	testCases := []struct {
		name        string
		mutate      func(*authenticatedRuntimeMetadata)
		errorDetail string
	}{
		{
			name: "different finalized hash",
			mutate: func(authenticated *authenticatedRuntimeMetadata) {
				authenticated.FinalizedHash = types.Hash{7}
			},
			errorDetail: "authenticated finalized hash",
		},
		{
			name: "missing spec name",
			mutate: func(authenticated *authenticatedRuntimeMetadata) {
				authenticated.Version.SpecName = ""
			},
			errorDetail: "runtime version is incomplete",
		},
		{
			name: "missing spec version",
			mutate: func(authenticated *authenticatedRuntimeMetadata) {
				authenticated.Version.SpecVersion = 0
			},
			errorDetail: "runtime version is incomplete",
		},
		{
			name: "missing transaction version",
			mutate: func(authenticated *authenticatedRuntimeMetadata) {
				authenticated.Version.TransactionVersion = 0
			},
			errorDetail: "runtime version is incomplete",
		},
		{
			name: "missing state version",
			mutate: func(authenticated *authenticatedRuntimeMetadata) {
				authenticated.Version.StateVersion = 0
			},
			errorDetail: "runtime version is incomplete",
		},
		{
			name: "missing code hash",
			mutate: func(authenticated *authenticatedRuntimeMetadata) {
				authenticated.CodeHash = ""
			},
			errorDetail: "code identity",
		},
		{
			name: "malformed metadata hash",
			mutate: func(authenticated *authenticatedRuntimeMetadata) {
				authenticated.MetadataHash = "0x1234"
			},
			errorDetail: "metadata identity",
		},
		{
			name: "missing decoded metadata",
			mutate: func(authenticated *authenticatedRuntimeMetadata) {
				authenticated.Metadata = nil
			},
			errorDetail: "context is unavailable",
		},
	}
	client := &releaseRuntimeTestClient{callContext: func(context.Context, any, string, ...any) error {
		return errors.New("receipt RPC reached")
	}}
	chain := &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: client}}
	for _, testCase := range testCases {
		authenticated := authenticatedRuntimeMetadata{
			FinalizedHash: blockHash,
			Version:       crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1},
			CodeHash:      "0x" + strings.Repeat("11", 32),
			MetadataHash:  "0x" + strings.Repeat("22", 32),
			Metadata:      types.NewMetadataV14(),
		}
		testCase.mutate(&authenticated)
		err := verifyAuthenticatedFinalizedExtrinsicContext(context.Background(), chain, authenticated, blockHash, extrinsicHash)
		if err == nil || !strings.Contains(err.Error(), testCase.errorDetail) {
			t.Errorf("%s error=%v, want detail %q", testCase.name, err, testCase.errorDetail)
		}
		if err != nil && strings.Contains(err.Error(), "receipt RPC reached") {
			t.Errorf("%s reached receipt RPC before identity rejection", testCase.name)
		}
	}
}

func TestSubtensorAccountInfoDecodesRuntime453U64Balances(t *testing.T) {
	raw := make([]byte, 56)
	binary.LittleEndian.PutUint32(raw[0:4], 7)
	binary.LittleEndian.PutUint32(raw[4:8], 2)
	binary.LittleEndian.PutUint32(raw[8:12], 1)
	binary.LittleEndian.PutUint32(raw[12:16], 0)
	binary.LittleEndian.PutUint64(raw[16:24], 12_345_678_901)
	binary.LittleEndian.PutUint64(raw[24:32], 22)
	binary.LittleEndian.PutUint64(raw[32:40], 33)
	raw[40] = 44 // little-endian u128 account flags

	var got subtensorAccountInfo
	if err := codec.Decode(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Nonce != 7 || got.Consumers != 2 || got.Providers != 1 || got.Sufficients != 0 {
		t.Fatalf("account references decoded incorrectly: %+v", got)
	}
	if got.Data.Free != 12_345_678_901 || got.Data.Reserved != 22 || got.Data.Frozen != 33 || got.Data.Flags.Int == nil || got.Data.Flags.Uint64() != 44 {
		t.Fatalf("account data decoded incorrectly: %+v", got.Data)
	}
}

func TestRuntimeExistentialDepositDecodesExactU64Rao(t *testing.T) {
	raw := make([]byte, 8)
	binary.LittleEndian.PutUint64(raw, 500)
	got, err := decodeRuntimeExistentialDepositRao(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != 500 {
		t.Fatalf("existential deposit = %d, want 500 rao", got)
	}
}

func TestRuntimeExistentialDepositRejectsShapeAndZeroDrift(t *testing.T) {
	valid := make([]byte, 8)
	binary.LittleEndian.PutUint64(valid, 500)
	for name, raw := range map[string][]byte{
		"empty":     nil,
		"truncated": valid[:7],
		"trailing":  append(append([]byte(nil), valid...), 0),
		"zero":      make([]byte, 8),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeRuntimeExistentialDepositRao(raw); err == nil {
				t.Fatalf("%s existential-deposit encoding was accepted", name)
			}
		})
	}
}

func TestRuntimeInitialMinStakeDecodesExactU64Rao(t *testing.T) {
	raw := make([]byte, 8)
	binary.LittleEndian.PutUint64(raw, 2_000_000)
	got, err := decodeRuntimeInitialMinStakeRao(raw)
	if err != nil || got != 2_000_000 {
		t.Fatalf("InitialMinStake=%d error=%v", got, err)
	}
	for _, malformed := range [][]byte{nil, raw[:7], append(append([]byte(nil), raw...), 0), make([]byte, 8)} {
		if _, err := decodeRuntimeInitialMinStakeRao(malformed); err == nil {
			t.Fatalf("malformed InitialMinStake %x was accepted", malformed)
		}
	}
}

func TestRuntimeInitialMinTransferDecodesExactU64Rao(t *testing.T) {
	raw := make([]byte, 8)
	binary.LittleEndian.PutUint64(raw, 100_000)
	got, err := decodeRuntimeInitialMinTransferRao(raw)
	if err != nil || got != 100_000 {
		t.Fatalf("InitialMinTransfer=%d error=%v", got, err)
	}
	for _, malformed := range [][]byte{
		nil,
		raw[:7],
		append(append([]byte(nil), raw...), 0),
		make([]byte, 8),
	} {
		if _, err := decodeRuntimeInitialMinTransferRao(malformed); err == nil {
			t.Fatalf("malformed InitialMinTransfer %x was accepted", malformed)
		}
	}
}

type recordingMetadataConstantReader struct {
	module, constant string
	raw              []byte
	err              error
}

func (r *recordingMetadataConstantReader) FindConstantValue(module, constant string) ([]byte, error) {
	r.module, r.constant = module, constant
	return r.raw, r.err
}

func TestRuntimeDefaultMinTransferUsesInitialMetadataConstant(t *testing.T) {
	reader := &recordingMetadataConstantReader{raw: make([]byte, 8)}
	binary.LittleEndian.PutUint64(reader.raw, 100_000)
	raw, err := runtimeDefaultMinTransferMetadataValue(reader)
	if err != nil || len(raw) != 8 {
		t.Fatalf("metadata value length=%d error=%v", len(raw), err)
	}
	if reader.module != "SubtensorModule" || reader.constant != "InitialMinTransfer" {
		t.Fatalf("looked up %s.%s, want SubtensorModule.InitialMinTransfer", reader.module, reader.constant)
	}
	reader.err = errors.New("missing constant")
	if _, err := runtimeDefaultMinTransferMetadataValue(reader); err == nil || !strings.Contains(err.Error(), "InitialMinTransfer") {
		t.Fatalf("missing InitialMinTransfer metadata was accepted: %v", err)
	}
}

func TestRuntimeDefaultMinTransferBindingRejectsRuntimeAndManifestDrift(t *testing.T) {
	raw := make([]byte, 8)
	binary.LittleEndian.PutUint64(raw, 100_000)
	wantHash := "0x" + strings.Repeat("11", 32)
	got, err := validateRuntimeDefaultMinTransferBinding(raw, wantHash, wantHash, 100_000)
	if err != nil || got != 100_000 {
		t.Fatalf("valid binding=%d error=%v", got, err)
	}
	tests := map[string]struct {
		raw                []byte
		observed, expected string
		floor              uint64
		message            string
	}{
		"runtime-code": {raw, "0x" + strings.Repeat("22", 32), wantHash, 100_000, "runtime code hash"},
		"manifest":     {raw, wantHash, wantHash, 100_001, "reviewed manifest"},
		"zero":         {raw, wantHash, wantHash, 0, "zero"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := validateRuntimeDefaultMinTransferBinding(test.raw, test.observed, test.expected, test.floor); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("drift was accepted or reported unclearly: %v", err)
			}
		})
	}
}

func TestAlphaPrecompilePriceDecodesOnlyExactQ9Units(t *testing.T) {
	got, err := decodeAlphaPriceQ9(big.NewInt(568_309_000_000_000))
	if err != nil || got != 568_309 {
		t.Fatalf("alpha price=%d error=%v", got, err)
	}
	for _, malformed := range []*big.Int{nil, new(big.Int), big.NewInt(-1), big.NewInt(568_309_000_000_001), new(big.Int).Lsh(big.NewInt(1), 200)} {
		if _, err := decodeAlphaPriceQ9(malformed); err == nil {
			t.Fatalf("malformed alpha price %v was accepted", malformed)
		}
	}
}

func TestFinalizedStorageBatchRejectsPartialForeignAndDuplicateResponses(t *testing.T) {
	key1, key2 := types.NewStorageKey([]byte{1}), types.NewStorageKey([]byte{2})
	block := types.NewHash([]byte{3})
	valid := []types.StorageChangeSet{{Block: block, Changes: []types.KeyValueOption{
		{StorageKey: key1, HasStorageData: true, StorageData: types.StorageDataRaw{9}},
		{StorageKey: key2, HasStorageData: false},
	}}}
	values, err := validateStorageQueryChanges([]types.StorageKey{key1, key2}, block, valid)
	if err != nil || values[key1.Hex()] == nil || values[key2.Hex()] != nil {
		t.Fatalf("valid storage batch decoded as values=%v error=%v", values, err)
	}
	tests := map[string][]types.StorageChangeSet{
		"missing":     {{Block: block, Changes: valid[0].Changes[:1]}},
		"wrong-block": {{Block: types.NewHash([]byte{4}), Changes: valid[0].Changes}},
		"duplicate":   {{Block: block, Changes: append(append([]types.KeyValueOption(nil), valid[0].Changes...), valid[0].Changes[0])}},
		"foreign":     {{Block: block, Changes: []types.KeyValueOption{{StorageKey: types.NewStorageKey([]byte{7})}, valid[0].Changes[1]}}},
		"multiple":    {valid[0], valid[0]},
	}
	for name, sets := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := validateStorageQueryChanges([]types.StorageKey{key1, key2}, block, sets); err == nil {
				t.Fatal("malformed finalized storage batch was accepted")
			}
		})
	}
}

func TestValueQueryU64AcceptsBothAbsentRPCShapesAndRejectsMalformedData(t *testing.T) {
	key := types.NewStorageKey([]byte{1})
	empty := types.StorageDataRaw{}
	for name, values := range map[string]map[string]*types.StorageDataRaw{
		"null":  {key.Hex(): nil},
		"empty": {key.Hex(): &empty},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := decodeValueQueryU64(values, key, "value"); err != nil || got != 0 {
				t.Fatalf("fallback=%d error=%v", got, err)
			}
		})
	}
	raw := make([]byte, 8)
	binary.LittleEndian.PutUint64(raw, 99)
	data := types.StorageDataRaw(raw)
	if got, err := decodeValueQueryU64(map[string]*types.StorageDataRaw{key.Hex(): &data}, key, "value"); err != nil || got != 99 {
		t.Fatalf("value=%d error=%v", got, err)
	}
	malformed := types.StorageDataRaw{1}
	if _, err := decodeValueQueryU64(map[string]*types.StorageDataRaw{key.Hex(): &malformed}, key, "value"); err == nil {
		t.Fatal("malformed ValueQuery u64 was accepted")
	}
	if _, err := decodeValueQueryU64(map[string]*types.StorageDataRaw{}, key, "value"); err == nil {
		t.Fatal("missing response key was accepted as a runtime fallback")
	}
}

func TestOptionalRuntimeStructLeadingU64ChecksPresenceAndPinnedWidth(t *testing.T) {
	key := types.NewStorageKey([]byte{1})
	if got, err := decodeOptionalLeadingU64(map[string]*types.StorageDataRaw{key.Hex(): nil}, key, "lock", 32); err != nil || got != 0 {
		t.Fatalf("absent optional row=%d error=%v", got, err)
	}
	raw := make([]byte, 32)
	binary.LittleEndian.PutUint64(raw, 123)
	data := types.StorageDataRaw(raw)
	if got, err := decodeOptionalLeadingU64(map[string]*types.StorageDataRaw{key.Hex(): &data}, key, "lock", 32); err != nil || got != 123 {
		t.Fatalf("leading alpha=%d error=%v", got, err)
	}
	malformed := types.StorageDataRaw(raw[:31])
	if _, err := decodeOptionalLeadingU64(map[string]*types.StorageDataRaw{key.Hex(): &malformed}, key, "lock", 32); err == nil {
		t.Fatal("changed runtime struct width was accepted")
	}
	if _, err := decodeOptionalLeadingU64(map[string]*types.StorageDataRaw{}, key, "lock", 32); err == nil {
		t.Fatal("missing batch response key was accepted as an absent optional row")
	}
}

func TestTransferHyperparameterUsesAtomicTransferStorage(t *testing.T) {
	shape, ok := hyperShapes["transfer_enabled"]
	if !ok {
		t.Fatal("transfer_enabled hyperparameter is missing")
	}
	if shape.Storage != "TransferToggle" || shape.Call != "sudo_set_toggle_transfer" || shape.Kind != "bool" {
		t.Fatalf("transfer_enabled shape = %+v", shape)
	}
	if shape.Storage == "SubtokenEnabled" {
		t.Fatal("atomic transfer setting was confused with one-time subnet activation")
	}
}

func TestRuntimeStorageFallbackSuppliesAbsentValueQuery(t *testing.T) {
	entry := types.StorageEntryMetadataV14{
		Modifier: types.StorageFunctionModifierV0{IsDefault: true},
		Fallback: types.Bytes{0x33, 0xb3, 0x66, 0xe6},
	}
	var value struct {
		Low  types.U16
		High types.U16
	}
	present, err := decodeStorageFallback(entry, &value)
	if err != nil {
		t.Fatal(err)
	}
	if !present || value.Low != 45_875 || value.High != 58_982 {
		t.Fatalf("runtime fallback decoded as present=%t value=%+v", present, value)
	}
}

func TestRuntimeStorageFallbackKeepsAbsentOptionalQueryAbsent(t *testing.T) {
	entry := types.StorageEntryMetadataV14{
		Modifier: types.StorageFunctionModifierV0{IsOptional: true},
		Fallback: types.Bytes{0x01, 0x00},
	}
	value := types.U16(77)
	present, err := decodeStorageFallback(entry, &value)
	if err != nil {
		t.Fatal(err)
	}
	if present || value != 77 {
		t.Fatalf("optional storage was invented as present=%t value=%d", present, value)
	}
}

func TestHotkeyOwnerMustMatchExpectedColdkey(t *testing.T) {
	expected := [32]byte{1, 2, 3}
	if err := validateHotkeyOwner("validator", expected, expected); err != nil {
		t.Fatalf("matching coldkey rejected: %v", err)
	}
	actual := expected
	actual[31] = 4
	if err := validateHotkeyOwner("validator", actual, expected); err == nil {
		t.Fatal("mismatched registration coldkey was accepted")
	}
}
