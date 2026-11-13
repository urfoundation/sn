// Live native-fee probes are read-only incident tools for exact persisted
// SCALE transactions. They are skipped during ordinary deterministic suites.
package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

func TestLiveNativeTransactionFeeQuote(t *testing.T) {
	endpoint := os.Getenv("SIM_TESTNET_FEE_RPC")
	path := os.Getenv("SIM_TESTNET_FEE_EXTRINSIC_PATH")
	if endpoint == "" || path == "" {
		t.Skip("set SIM_TESTNET_FEE_RPC and SIM_TESTNET_FEE_EXTRINSIC_PATH")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := crv4.DialChain(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.API.Client.Close()
	cfg := &ResolvedConfig{Config: &HarnessConfig{Budgets: BudgetConfig{MaximumNativeTransactionFeeRao: 3_000_000}}}
	manager := &SubstrateManager{chain: chain, cfg: cfg}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	estimated, limit, err := manager.approveNativeTransactionFee(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("estimated_fee_rao=%d limit_rao=%d", estimated, limit)
}
