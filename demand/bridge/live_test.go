package bridge

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/demand/solana"
)

// TestLiveSendSimulates compiles a send for a real holder of TAO and simulates
// it on mainnet, which signs nothing and moves nothing. It runs only with
// DEMAND_LIVE_SOLANA_RPC and DEMAND_LIVE_TAO_HOLDER (a system-owned wallet that
// holds TAO and SOL) set.
func TestLiveSendSimulates(t *testing.T) {
	endpoint, holder := os.Getenv("DEMAND_LIVE_SOLANA_RPC"), os.Getenv("DEMAND_LIVE_TAO_HOLDER")
	if endpoint == "" || holder == "" {
		t.Skip("set DEMAND_LIVE_SOLANA_RPC and DEMAND_LIVE_TAO_HOLDER")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	client := solana.NewClient(endpoint)
	sender := solana.MustPublicKey(holder)
	source, err := solana.AssociatedTokenAddress(sender, TaoMint)
	if err != nil {
		t.Fatal(err)
	}
	before, err := client.TokenAccount(ctx, source)
	if err != nil || before == nil {
		t.Fatal("holder's TAO account", err)
	}
	lamports, err := client.Balance(ctx, sender)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := client.Account(ctx, PeerConfig)
	if err != nil || peer == nil {
		t.Fatal("peer config", err)
	}
	if err := CheckPeerConfig(peer.Data); err != nil {
		t.Fatal(err)
	}
	tableAccount, err := client.Account(ctx, LookupTable)
	if err != nil || tableAccount == nil {
		t.Fatal("lookup table", err)
	}
	table, err := solana.ParseLookupTable(LookupTable, tableAccount.Data)
	if err != nil {
		t.Fatal(err)
	}
	blockhash, err := client.LatestBlockhash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	amount := min(before.Amount/UnitRao*UnitRao, uint64(100_000_000))
	const maxFee = 50_000_000
	send := Send{Sender: sender, To: common.HexToAddress("0x1111111111111111111111111111111111111111"), AmountRao: amount, MaxFeeLamports: maxFee}
	tx, err := send.Transaction(blockhash.Hash, table)
	if err != nil {
		t.Fatal(err)
	}
	raw := tx.Serialize()
	t.Logf("compiled %d bytes (limit %d), %d static keys", len(raw), solana.MaxTransactionBytes, len(tx.StaticKeys))
	sim, err := client.Simulate(ctx, raw, []solana.PublicKey{source, sender})
	if err != nil {
		t.Fatal(err)
	}
	if sim.Err != nil {
		for _, line := range sim.Logs {
			t.Log(line)
		}
		t.Fatalf("simulation failed: %s", sim.Err)
	}
	after, wallet := sim.Accounts[0].Token, sim.Accounts[1]
	if after == nil || after.Amount != before.Amount-amount {
		t.Fatalf("simulation moved the wrong TAO: before %d after %+v amount %d", before.Amount, after, amount)
	}
	spent := lamports - wallet.Lamports
	t.Logf("amount %d rao; TAO %d -> %d; SOL spent %d lamports against a cap of %d; %d compute units", amount, before.Amount, after.Amount, spent, uint64(maxFee), sim.UnitsConsumed)
	if spent >= maxFee {
		t.Fatalf("the fee cap was charged in full (%d): it should be a cap, not the fee", spent)
	}
}
