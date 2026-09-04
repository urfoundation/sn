package crv4

import (
	"context"
	"errors"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcrpc "github.com/centrifuge/go-substrate-rpc-client/v4/rpc"
	gsrpcchain "github.com/centrifuge/go-substrate-rpc-client/v4/rpc/chain"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	gsrpcblock "github.com/centrifuge/go-substrate-rpc-client/v4/types/block"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
)

func TestExtrinsicIndexUsesScaleBlake2Hash(t *testing.T) {
	raw := []byte{0x10, 0x84, 0x01, 0x02, 0x03}
	digest := blake2b.Sum256(raw)
	want := types.Hash(digest)
	index, found, err := extrinsicIndex([]string{"0x00", codec.HexEncodeToString(raw)}, want)
	if err != nil {
		t.Fatal(err)
	}
	if !found || index != 1 {
		t.Fatalf("index=%d found=%t", index, found)
	}
	if _, found, err := extrinsicIndex([]string{"0x00"}, want); err != nil || found {
		t.Fatalf("absent hash: found=%t err=%v", found, err)
	}
	if _, _, err := extrinsicIndex([]string{"not-hex"}, want); err == nil {
		t.Fatal("malformed block extrinsic was accepted")
	}
}

// Propagates cancellation into a stalled historical block-body read before
// any event decoding can begin.
func TestVerifyFinalizedExtrinsicContextCancelsBlockRead(t *testing.T) {
	blockRead := make(chan struct{})
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, _ any, method string, _ ...any) error {
		if method != "chain_getBlock" {
			return errors.New("unexpected finalized receipt RPC")
		}
		close(blockRead)
		<-ctx.Done()
		return ctx.Err()
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: client}, Meta: types.NewMetadataV14()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- chain.VerifyFinalizedExtrinsicContext(ctx, types.Hash{1}, types.Hash{2})
	}()
	<-blockRead
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled finalized receipt error=%v", err)
	}
}

// Carries the caller's context through every RPC in the finalized-block scan;
// GSRPC's convenience methods otherwise replace it with a background context.
func TestLocateFinalizedExtrinsicPreservesContextAcrossScan(t *testing.T) {
	type callerContextKey struct{}
	const callerContextValue = "receipt-scan"
	raw := []byte{1, 2, 3, 4}
	digest := blake2b.Sum256(raw)
	extrinsicHash := types.Hash(digest)
	finalizedHash := types.Hash{5}
	calls := 0
	client := &runtimeIdentityTestClient{callContext: func(ctx context.Context, result any, method string, args ...any) error {
		if ctx.Value(callerContextKey{}) != callerContextValue {
			return errors.New("finalized scan lost caller context")
		}
		calls++
		switch method {
		case "chain_getFinalizedHead":
			*(result.(*string)) = finalizedHash.Hex()
		case "chain_getHeader":
			if len(args) != 1 || args[0] != finalizedHash.Hex() {
				return errors.New("finalized header hash changed")
			}
			*(result.(*types.Header)) = types.Header{Number: types.BlockNumber(5)}
		case "chain_getBlockHash":
			if len(args) != 1 || args[0] != uint64(5) {
				return errors.New("finalized block number changed")
			}
			*(result.(*string)) = finalizedHash.Hex()
		case "chain_getBlock":
			if len(args) != 1 || args[0] != finalizedHash.Hex() {
				return errors.New("finalized block hash changed")
			}
			*(result.(*gsrpcblock.SignedBlock)) = gsrpcblock.SignedBlock{Block: gsrpcblock.Block{Extrinsics: []string{codec.HexEncodeToString(raw)}}}
		default:
			return errors.New("unexpected finalized scan RPC")
		}
		return nil
	}}
	chain := &Chain{API: &gsrpc.SubstrateAPI{
		Client: client,
		RPC:    &gsrpcrpc.RPC{Chain: gsrpcchain.NewChain(client)},
	}}
	ctx := context.WithValue(context.Background(), callerContextKey{}, callerContextValue)
	receipt, found, err := chain.LocateFinalizedExtrinsic(ctx, extrinsicHash, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !found || receipt == nil || receipt.BlockHash != finalizedHash || receipt.BlockNumber != 5 {
		t.Fatalf("finalized receipt = %+v found=%t", receipt, found)
	}
	if calls != 4 {
		t.Fatalf("finalized scan RPC calls=%d, want 4", calls)
	}
}
