// Explicit journal and latest-head transitions exercise admission and recovery
// without sleeps, external services, or wall-clock-dependent outcomes.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Small identities make exact plan and intent ownership visible in assertions.
func fleetRenewalDeadlineActions(count int) []Action {
	actions := make([]Action, 0, count)
	for index := range count {
		actions = append(actions, Action{ID: fmt.Sprintf("fleet.renew.1.1.bind.%d", index+1), IntentHash: fmt.Sprintf("intent-%d", index), Parameters: map[string]string{"operation": "bind"}})
	}
	return actions
}

// A merely future epoch can still be too close for the actual joined workload.
func TestFleetRenewalDeadlineBudgetsAllPipelineWaves(t *testing.T) {
	actions := fleetRenewalDeadlineActions(2020)
	waves, blocks, err := fleetRenewalRemainingWindow("approved", actions, nil)
	if err != nil || waves != 202 || blocks != 824 {
		t.Fatalf("renewal work = %d waves/%d blocks: %v", waves, blocks, err)
	}
	if err := validateFleetRenewalDeadline(7, 1000, 1600, waves, blocks); err == nil {
		t.Fatal("future activation admitted despite insufficient remaining pipeline time")
	}
	if err := validateFleetRenewalDeadline(8, 1000, 1900, waves, blocks); err != nil {
		t.Fatal(err)
	}
	for _, activation := range []uint64{999, 1000, 1000 + blocks} {
		if err := validateFleetRenewalDeadline(7, 1000, activation, waves, blocks); err == nil {
			t.Fatalf("closed or margin-exhausted activation %d accepted", activation)
		}
	}
	if err := validateFleetRenewalDeadline(7, math.MaxUint64-10, math.MaxUint64, 1, 16); err == nil {
		t.Fatal("near-overflow block numbers bypassed the deadline")
	}
}

// Operation barriers cost a wave even when a preceding phase was underfull.
func TestFleetRenewalDeadlineUsesExecutionWaveBoundaries(t *testing.T) {
	actions := fleetRenewalDeadlineActions(12)
	for index := range 6 {
		actions[index].Parameters["operation"] = "mirror"
	}
	waves, blocks, err := fleetRenewalRemainingWindow("approved", actions, nil)
	if err != nil || waves != 2 || blocks != 24 {
		t.Fatalf("phase-boundary work = %d/%d: %v", waves, blocks, err)
	}
}

// Resume discounts exact completed work, while intent markers cannot erase
// signed transaction recovery or let a foreign receipt manufacture progress.
func TestFleetRenewalDeadlineResumeRetainsCompletedAndSignedWork(t *testing.T) {
	actions := fleetRenewalDeadlineActions(31)
	entries := make([]JournalEntry, 0, len(actions))
	for _, action := range actions[:20] {
		entries = append(entries, JournalEntry{PlanHash: "approved", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: common.Hash{0x41}.Hex()})
	}
	waves, blocks, err := fleetRenewalRemainingWindow("approved", actions, entries)
	if err != nil || waves != 2 || blocks != 24 {
		t.Fatalf("resumed work = %d/%d: %v", waves, blocks, err)
	}
	for _, action := range actions[20:] {
		entries = append(entries, JournalEntry{PlanHash: "approved", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageBroadcast, TransactionHash: common.Hash{0x42}.Hex()})
		entries = append(entries, JournalEntry{PlanHash: "approved", ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent})
	}
	if err := verifyFleetRenewalDeadline(context.Background(), nil, common.Address{}, FleetRenewal{ValidFromEpoch: 1}, "approved", actions, entries); err != nil {
		t.Fatalf("replay-only resume was subject to a fresh signing deadline: %v", err)
	}
	waves, blocks, err = fleetRenewalRemainingWindow("foreign-plan", actions, entries)
	if err != nil || waves != 4 || blocks != 32 {
		t.Fatalf("foreign approval discounted work: %d/%d: %v", waves, blocks, err)
	}
	for index := range entries {
		entries[index].IntentHash = "foreign-intent"
	}
	waves, blocks, err = fleetRenewalRemainingWindow("approved", actions, entries)
	if err != nil || waves != 4 || blocks != 32 {
		t.Fatalf("foreign intent discounted work: %d/%d: %v", waves, blocks, err)
	}
}

// A transport-only fixture avoids opening any listening socket. Mutations are
// explicit and occur between sequential deadline checks.
type fleetRenewalDeadlineTransport struct {
	head       uint64
	activation *big.Int
}

// Every contract read must be pinned to the most recently sampled latest head.
func (self *fleetRenewalDeadlineTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	defer request.Body.Close()
	var call struct {
		Id     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		return nil, err
	}
	var result string
	switch call.Method {
	case "eth_blockNumber":
		result = hexutil.EncodeUint64(self.head)
	case "eth_call":
		if len(call.Params) != 2 || string(call.Params[1]) != fmt.Sprintf("%q", hexutil.EncodeUint64(self.head)) {
			return nil, fmt.Errorf("deadline queried stale or unpinned contract state: %s", call.Params)
		}
		result = hexutil.Encode(common.LeftPadBytes(self.activation.Bytes(), 32))
	default:
		return nil, fmt.Errorf("unexpected deadline Rpc %s", call.Method)
	}
	wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(wire))}, nil
}

// Latest can cross activation while a cached finalized head still looks safe.
func TestFleetRenewalDeadlineRejectsLatestCrossingFinalizedBoundary(t *testing.T) {
	transport := &fleetRenewalDeadlineTransport{head: 80, activation: big.NewInt(120)}
	client, err := rpc.DialOptions(t.Context(), "http://renewal.example", rpc.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	manager := &EvmTxManager{client: ethclient.NewClient(client)}
	ctx, err := withFinalizedEVMHead(t.Context(), ChainHead{Number: 70, Hash: common.Hash{0x70}.Hex()})
	if err != nil {
		t.Fatal(err)
	}
	actions := fleetRenewalDeadlineActions(2)
	renewal := FleetRenewal{ValidFromEpoch: 7}
	if err := verifyFleetRenewalDeadline(ctx, manager, common.Address{0x50}, renewal, "approved", actions, nil); err != nil {
		t.Fatal(err)
	}
	transport.head = 120
	if err := verifyFleetRenewalDeadline(ctx, manager, common.Address{0x50}, renewal, "approved", actions, nil); err == nil || !strings.Contains(err.Error(), "latest block 120") {
		t.Fatalf("latest activation crossing bypassed deadline: %v", err)
	}
	transport.activation = new(big.Int).Lsh(big.NewInt(1), 64)
	if _, _, err := readFleetRenewalDeadline(ctx, manager, common.Address{0x50}, renewal.ValidFromEpoch); err == nil {
		t.Fatal("overflowing on-chain activation block accepted")
	}
}
