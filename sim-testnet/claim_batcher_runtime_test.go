package main

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/miner/onchain"
)

// CL-1, CL-5: the claim tools refuse a batcher whose code is not
// onchain.ClaimBatcherRuntime, which is the catalog's exact runtime, the code
// the deployment phase's batcher-create installs (CB-7).
func TestClaimToolsHoldTheCatalogsBatcherRuntime(t *testing.T) {
	hash := crypto.Keccak256Hash(common.FromHex(onchain.ClaimBatcherRuntime)).Hex()
	if onchain.ClaimBatcherRuntime != "0x"+ClaimBatcherRuntimeBytecode || hash != ClaimBatcherRuntimeBytecodeHash {
		t.Fatalf("the claim tools' batcher runtime, hash %s, is not the catalog's, %s", hash, ClaimBatcherRuntimeBytecodeHash)
	}
}
