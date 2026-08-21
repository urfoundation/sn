package onchain

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

type finalityStub struct {
	finalized *types.Header
	canonical *types.Header
}

func (s finalityStub) HeaderByNumber(_ context.Context, number *big.Int) (*types.Header, error) {
	if number.Int64() == int64(rpc.FinalizedBlockNumber) {
		return s.finalized, nil
	}
	return s.canonical, nil
}

func TestWaitFinalizedChecksCanonicalReceiptBlock(t *testing.T) {
	canonical := &types.Header{Number: big.NewInt(10), Extra: []byte("canonical")}
	receipt := &types.Receipt{BlockNumber: big.NewInt(10), BlockHash: canonical.Hash(), TxHash: common.HexToHash("0x1")}
	stub := finalityStub{finalized: &types.Header{Number: big.NewInt(12)}, canonical: canonical}
	if err := waitFinalized(context.Background(), stub, receipt); err != nil {
		t.Fatal(err)
	}

	reorged := &types.Header{Number: big.NewInt(10), Extra: []byte("replacement")}
	if err := waitFinalized(context.Background(), finalityStub{finalized: stub.finalized, canonical: reorged}, receipt); err == nil || !strings.Contains(err.Error(), "reorged") {
		t.Fatalf("reorg error = %v", err)
	}
}

func TestWaitFinalizedRejectsIncompleteReceipt(t *testing.T) {
	if err := waitFinalized(context.Background(), finalityStub{}, &types.Receipt{}); err == nil {
		t.Fatal("incomplete receipt accepted")
	}
}
