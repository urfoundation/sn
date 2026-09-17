// Finality compares the canonical RPC identity to the receipt, including
// synthetic block hashes that cannot be reconstructed from header fields.
package onchain

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Supplies complete synthetic headers through the actual RPC decode path.
type blockIdentityTestAPI struct {
	block func(string) any
}

// Returns the explicit fixture identity selected by the caller.
func (self *blockIdentityTestAPI) GetBlockByNumber(_ context.Context, selector string, transactions bool) any {
	if transactions {
		return nil
	}
	return self.block(selector)
}

// Uses in-process RPC to exercise ethclient without transport timing.
func blockIdentityTestClient(t *testing.T, block func(string) any) *ethclient.Client {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("eth", &blockIdentityTestAPI{block: block}); err != nil {
		t.Fatal(err)
	}
	client := ethclient.NewClient(rpc.DialInProc(server))
	t.Cleanup(client.Close)
	t.Cleanup(server.Stop)
	return client
}

// Produces an otherwise valid Ethereum header whose RPC hash is deliberately
// different from Header.Hash, as observed on public Subtensor testnet.
func syntheticBlockIdentityFixture(t *testing.T, number uint64, hash common.Hash) map[string]any {
	t.Helper()
	header := &types.Header{Number: new(big.Int).SetUint64(number), Difficulty: big.NewInt(0), Extra: []byte("synthetic")}
	if header.Hash() == hash {
		t.Fatal("synthetic fixture does not distinguish the RPC and recomputed hash")
	}
	encoded, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	fields["hash"] = hash.Hex()
	return fields
}

// A canonical synthetic hash must complete finality without a false reorg.
func TestWaitFinalizedChecksCanonicalReceiptBlock(t *testing.T) {
	hash := common.HexToHash("0xaad46c25ee81b4f9f636677c1b9197a146733e8f16d57114269030ddf26790e2")
	canonical := syntheticBlockIdentityFixture(t, 10, hash)
	finalized := syntheticBlockIdentityFixture(t, 12, common.HexToHash("0x12"))
	var selectors []string
	client := blockIdentityTestClient(t, func(selector string) any {
		selectors = append(selectors, selector)
		if selector == "finalized" {
			return finalized
		}
		return canonical
	})
	receipt := &types.Receipt{BlockNumber: big.NewInt(10), BlockHash: hash, TxHash: common.HexToHash("0x1")}
	if err := waitFinalized(context.Background(), client, receipt); err != nil {
		t.Fatal(err)
	}
	if len(selectors) != 2 || selectors[0] != "finalized" || selectors[1] != "0xa" {
		t.Fatalf("finality selectors = %v", selectors)
	}
}

// A changed RPC hash is a reorg even when every local header field is equal.
func TestWaitFinalizedRejectsSyntheticHashReorg(t *testing.T) {
	hash := common.HexToHash("0x10")
	replacement := syntheticBlockIdentityFixture(t, 10, common.HexToHash("0x20"))
	finalized := syntheticBlockIdentityFixture(t, 12, common.HexToHash("0x12"))
	client := blockIdentityTestClient(t, func(selector string) any {
		if selector == "finalized" {
			return finalized
		}
		return replacement
	})
	receipt := &types.Receipt{BlockNumber: big.NewInt(10), BlockHash: hash, TxHash: common.HexToHash("0x1")}
	if err := waitFinalized(context.Background(), client, receipt); err == nil || !strings.Contains(err.Error(), "reorged") {
		t.Fatalf("reorg error = %v", err)
	}
}

// Inclusion lookups reject missing or substituted heights before success.
func TestWaitFinalizedRejectsMalformedCanonicalBlock(t *testing.T) {
	hash := common.HexToHash("0x10")
	finalized := syntheticBlockIdentityFixture(t, 12, common.HexToHash("0x12"))
	for _, canonical := range []any{nil, map[string]any{"number": "0xb", "hash": hash.Hex()}, map[string]any{"number": "0xa", "hash": common.Hash{}.Hex()}} {
		client := blockIdentityTestClient(t, func(selector string) any {
			if selector == "finalized" {
				return finalized
			}
			return canonical
		})
		receipt := &types.Receipt{BlockNumber: big.NewInt(10), BlockHash: hash, TxHash: common.HexToHash("0x1")}
		if err := waitFinalized(context.Background(), client, receipt); err == nil || !strings.Contains(err.Error(), "canonical inclusion block") {
			t.Errorf("canonical block %v: error = %v", canonical, err)
		}
	}
}

// Invalid receipt heights and hashes cannot reach the finality poller.
func TestWaitFinalizedRejectsIncompleteReceipt(t *testing.T) {
	hash := common.HexToHash("0x10")
	for _, receipt := range []*types.Receipt{
		nil, {}, {BlockNumber: big.NewInt(0), BlockHash: hash},
		{BlockNumber: big.NewInt(-1), BlockHash: hash},
		{BlockNumber: new(big.Int).Lsh(big.NewInt(1), 64), BlockHash: hash},
		{BlockNumber: big.NewInt(10)},
	} {
		if err := waitFinalized(context.Background(), nil, receipt); err == nil || !strings.Contains(err.Error(), "incomplete receipt") {
			t.Errorf("invalid receipt %+v: error = %v", receipt, err)
		}
	}
}

// RPC identity decoding rejects malformed quantities, missing fields, and
// zero/malformed hashes without inventing an identity from other fields.
func TestEVMBlockIdentityRejectsMalformedRPC(t *testing.T) {
	hash := common.HexToHash("0x10").Hex()
	cases := []any{
		nil,
		map[string]any{"number": "0xa"},
		map[string]any{"hash": hash},
		map[string]any{"number": nil, "hash": hash},
		map[string]any{"number": "0xa", "hash": nil},
		map[string]any{"number": "0xa", "hash": common.Hash{}.Hex()},
		map[string]any{"number": "0xa", "hash": "0x10"},
		map[string]any{"number": "0xa", "hash": "0x" + strings.Repeat("zz", 32)},
		map[string]any{"number": "0xb", "hash": hash},
		map[string]any{"number": "a", "hash": hash},
		map[string]any{"number": "0x0a", "hash": hash},
		map[string]any{"number": "0x10000000000000000", "hash": hash},
		map[string]any{"number": "-0xa", "hash": hash},
	}
	for index, block := range cases {
		client := blockIdentityTestClient(t, func(string) any { return block })
		if identity, err := ReadEVMBlockIdentity(context.Background(), client, big.NewInt(10)); err == nil {
			t.Errorf("case %d accepted malformed RPC identity %+v", index, identity)
		}
	}
}

// Only exact unsigned heights and the finalized tag are admitted selectors.
func TestEVMBlockIdentityRejectsInvalidSelector(t *testing.T) {
	client := blockIdentityTestClient(t, func(selector string) any {
		t.Errorf("invalid selector reached RPC: %s", selector)
		return nil
	})
	for _, selector := range []*big.Int{nil, big.NewInt(-1), big.NewInt(-2), new(big.Int).Lsh(big.NewInt(1), 64)} {
		if _, err := ReadEVMBlockIdentity(context.Background(), client, selector); err == nil {
			t.Errorf("accepted invalid selector %v", selector)
		}
	}
}
