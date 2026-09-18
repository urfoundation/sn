package crv4

// commitment_context_test.go proves fleet commitment reads retain one exact
// block hash and caller cancellation across every raw storage/header RPC.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// Builds only the two map entries needed to derive the exact commitment keys.
func commitmentContextTestMetadata() *types.Metadata {
	mapEntry := func(name string) types.StorageEntryMetadataV14 {
		return types.StorageEntryMetadataV14{
			Name: types.Text(name),
			Type: types.StorageEntryTypeV14{
				IsMap: true,
				AsMap: types.MapTypeV14{Hashers: []types.StorageHasherV10{
					{IsIdentity: true},
					{IsIdentity: true},
				}},
			},
		}
	}
	metadata := types.NewMetadataV14()
	metadata.AsMetadataV14.Pallets = []types.PalletMetadataV14{{
		Name:       types.Text(CommitmentsPalletName),
		HasStorage: true,
		Storage: types.StorageMetadataV14{
			Prefix: types.Text(CommitmentsPalletName),
			Items:  []types.StorageEntryMetadataV14{mapEntry("CommitmentOf"), mapEntry("LastCommitment")},
		},
	}}
	return metadata
}

// Provides canonical fixed-width storage bytes for one exact registration.
func commitmentContextTestStorage(t *testing.T, hash [32]byte, block uint32) (string, string) {
	t.Helper()
	registration := make([]byte, registrationFixedU32PrefixBytes)
	binary.LittleEndian.PutUint64(registration[:8], 1)
	binary.LittleEndian.PutUint32(registration[8:], block)
	info, err := EncodeFleetCommitmentInfo(hash)
	if err != nil {
		t.Fatal(err)
	}
	registration = append(registration, info...)
	last := make([]byte, 4)
	binary.LittleEndian.PutUint32(last, block)
	return codec.HexEncodeToString(registration), codec.HexEncodeToString(last)
}

// Proves the same caller context and same explicit hash reach both raw reads
// and the header, preventing a mixed-head commitment proof.
func TestFleetCommitmentAtContextUsesExactBlockForEveryRPC(t *testing.T) {
	type callerContextKey struct{}
	const callerContextValue = "fleet-commitment"
	blockHash := types.Hash{9}
	hotkey := [32]byte{7}
	commitmentHash := [32]byte{5}
	registration, last := commitmentContextTestStorage(t, commitmentHash, 42)
	storageCalls := 0
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if ctx.Value(callerContextKey{}) != callerContextValue {
			return errors.New("fleet commitment lost caller context")
		}
		switch method {
		case "state_getStorage":
			if len(args) != 2 || args[1] != blockHash.Hex() {
				return fmt.Errorf("fleet storage args=%v, want exact block %s", args, blockHash.Hex())
			}
			storageCalls++
			if storageCalls == 1 {
				return setRuntimeIdentityTestResult(result, registration)
			}
			if storageCalls == 2 {
				return setRuntimeIdentityTestResult(result, last)
			}
			return errors.New("fleet commitment made extra storage read")
		case "chain_getHeader":
			if len(args) != 1 || args[0] != blockHash.Hex() {
				return fmt.Errorf("fleet header args=%v, want exact block %s", args, blockHash.Hex())
			}
			*(result.(*types.Header)) = types.Header{Number: types.BlockNumber(42)}
			return nil
		default:
			return fmt.Errorf("unexpected fleet commitment RPC %s", method)
		}
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: commitmentContextTestMetadata()}
	ctx := context.WithValue(context.Background(), callerContextKey{}, callerContextValue)
	observed, err := chain.FleetCommitmentAtContext(ctx, 7, hotkey, blockHash)
	if err != nil {
		t.Fatal(err)
	}
	if storageCalls != 2 || observed.Hash != commitmentHash || observed.CommitmentBlock != 42 || observed.FinalizedAt != 42 || observed.FinalizedHash != blockHash {
		t.Fatalf("fleet commitment=%+v storage_calls=%d", observed, storageCalls)
	}
}

// A stalled provider must release the fleet status/publish operation as soon
// as its caller cancels, before a header or second storage read is attempted.
func TestFleetCommitmentAtContextCancelsStorageRead(t *testing.T) {
	blockHash := types.Hash{3}
	storageStarted := make(chan struct{})
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, _ any, method string, args ...any) error {
		if method != "state_getStorage" || len(args) != 2 || args[1] != blockHash.Hex() {
			return fmt.Errorf("unexpected canceled fleet commitment RPC %s args=%v", method, args)
		}
		close(storageStarted)
		<-ctx.Done()
		return ctx.Err()
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: commitmentContextTestMetadata()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := chain.FleetCommitmentAtContext(ctx, 7, [32]byte{4}, blockHash)
		done <- err
	}()
	<-storageStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled fleet commitment error=%v", err)
	}
}
