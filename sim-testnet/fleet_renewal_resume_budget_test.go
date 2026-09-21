// Finalized renewal fees are already spent, so replay cannot require the same
// funds again. Pending exact signed transactions retain their full fee ceiling.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// A sequential balance transport prohibits writes and returns exact test funds.
type fleetRenewalBalanceTransport struct{ balance *big.Int }

// Only the remaining signer balance is a permitted network observation.
func (self *fleetRenewalBalanceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	defer request.Body.Close()
	var call struct {
		Id     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		return nil, err
	}
	if call.Method != "eth_getBalance" {
		return nil, fmt.Errorf("unexpected balance Rpc %s", call.Method)
	}
	wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": hexutil.EncodeBig(self.balance)})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(wire))}, nil
}

// Reproduce a mid-renewal resume whose balance covers only its unfinished work.
func TestFleetRenewalResumeBalanceExcludesFinalizedFees(t *testing.T) {
	transport := &fleetRenewalBalanceTransport{balance: big.NewInt(200)}
	client, err := rpc.DialOptions(t.Context(), "http://renewal-balance.example", rpc.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	address := common.Address{0x61}.Hex()
	actions := []Action{
		{ID: "fleet.renew.1.1.bind.1", IntentHash: "first", Kind: "evm-transaction", Parameters: map[string]string{"renewal_expected_signer": address}, Spend: Spend{EVMGasWei: "100"}},
		{ID: "fleet.renew.1.1.bind.2", IntentHash: "second", Kind: "evm-transaction", Parameters: map[string]string{"renewal_expected_signer": address}, Spend: Spend{EVMGasWei: "200"}},
	}
	executor := &Executor{plan: &SetupPlan{PlanHash: "approved"}, keeper: &EvmTxManager{client: ethclient.NewClient(client)}, journal: &Journal{entries: []JournalEntry{
		{PlanHash: "approved", ActionID: actions[0].ID, IntentHash: actions[0].IntentHash, Stage: StageFinalized, TransactionHash: common.Hash{0x62}.Hex()},
		{PlanHash: "approved", ActionID: actions[1].ID, IntentHash: actions[1].IntentHash, Stage: StageBroadcast, TransactionHash: common.Hash{0x63}.Hex()},
	}}}
	if err := executor.verifyFleetRenewalSignerBalances(t.Context(), actions); err != nil {
		t.Fatalf("paid fee was charged twice on resume: %v", err)
	}
	transport.balance.SetUint64(199)
	if err := executor.verifyFleetRenewalSignerBalances(t.Context(), actions); err == nil {
		t.Fatal("pending fee liability was discarded")
	}
	transport.balance.SetUint64(200)
	executor.journal.entries[0].PlanHash = "foreign-plan"
	if err := executor.verifyFleetRenewalSignerBalances(t.Context(), actions); err == nil {
		t.Fatal("foreign finalized receipt reduced required signer funds")
	}
	executor.journal.entries[0].PlanHash = "approved"
	executor.journal.entries[0].IntentHash = "foreign-intent"
	if err := executor.verifyFleetRenewalSignerBalances(t.Context(), actions); err == nil {
		t.Fatal("foreign finalized intent reduced required signer funds")
	}
	executor.journal.entries[0].IntentHash = actions[0].IntentHash
	executor.journal.entries[1].Stage = StageFinalized
	executor.keeper = nil
	if err := executor.verifyFleetRenewalSignerBalances(t.Context(), actions); err != nil {
		t.Fatalf("fully finalized replay required fresh signer funding: %v", err)
	}
}
