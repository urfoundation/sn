package main

// Predecessor indexing retains exact approved finality and has bounded lineage
// allocation work even when unrelated journal history grows.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/stabi"
)

func TestFleetRenewalHistoricalPlanDecodeIsReused(t *testing.T) {
	fixture := newCarriedPreparationTest(t, 1)
	first, err := readCachedFleetRenewalHistoricalPlan(fixture.executor.stateDir, fixture.source.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.executor.stateDir, "plans", stringsTrim0x(fixture.source.PlanHash)+".json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	second, err := readCachedFleetRenewalHistoricalPlan(fixture.executor.stateDir, fixture.source.PlanHash)
	if err != nil {
		t.Fatalf("cached immutable renewal plan was reread after admission: %v", err)
	}
	if first != second {
		t.Fatal("cached immutable renewal plan did not retain its admitted decode")
	}
}

// The finalized head is already authenticated, so allocation counts isolate
// local history work without RPC, timing thresholds, or scheduler dependence.
func TestFleetRenewalPredecessorsDoNotRebuildLineageForJournalRows(t *testing.T) {
	base := &SetupPlan{PlanHash: "approved-current"}
	for index := range 128 {
		base.PriorPlanHashes = append(base.PriorPlanHashes, fmt.Sprintf("approved-prior-%d", index))
	}
	ctx, err := withFinalizedEVMHead(t.Context(), ChainHead{Number: 100, Hash: common.Hash{0x64}.Hex()})
	if err != nil {
		t.Fatal(err)
	}
	executor := &Executor{keeper: &EvmTxManager{}, journal: &Journal{}}
	measure := func() float64 {
		var verifyErr error
		allocations := testing.AllocsPerRun(4, func() {
			verifyErr = executor.verifyFleetRenewalPredecessors(ctx, FleetRenewal{}, base)
		})
		if verifyErr != nil {
			t.Fatal(verifyErr)
		}
		return allocations
	}
	baseline := measure()
	for range 4096 {
		executor.journal.entries = append(executor.journal.entries, JournalEntry{PlanHash: base.PlanHash, Stage: StageIncluded})
	}
	withHistory := measure()
	// Only the detached journal slice adds an allocation. Allow a small margin
	// for map growth across Go versions, never an allocation per history row.
	if withHistory > baseline+16 {
		t.Fatalf("lineage allocation grew with journal rows: empty=%g, 4096 rows=%g", baseline, withHistory)
	}
}

// A finalized approved tuple reaches fresh receipt/event verification; each
// single-field near miss must fail before issuing historical RPC calls.
func TestFleetRenewalPredecessorsRequireApprovedFinalizedExactCheckpoint(t *testing.T) {
	base := &SetupPlan{PlanHash: "approved-current", PriorPlanHashes: []string{"approved-prior"}}
	prior := FleetBindingEvidence{
		ClientID: fleetLifecycleHex16([16]byte{0x31}), FleetID: fleetLifecycleHex([32]byte{0x32}),
		Hotkey: fleetLifecycleHex([32]byte{0x33}), UID: 7, Generation: 2, ValidFromEpoch: 10, ValidToEpoch: 20,
		TransactionHash: common.Hash{0x41}.Hex(), BlockNumber: 50, BlockHash: common.Hash{0x42}.Hex(),
	}
	coordinator := common.Address{0x51}
	contractAbi, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	event := contractAbi.Events["FleetBound"]
	eventData, err := event.Inputs.NonIndexed().Pack(prior.UID, prior.Generation, prior.ValidFromEpoch, prior.ValidToEpoch)
	if err != nil {
		t.Fatal(err)
	}
	receipt := &ethTypes.Receipt{
		Status: ethTypes.ReceiptStatusSuccessful, TxHash: common.HexToHash(prior.TransactionHash),
		BlockNumber: new(big.Int).SetUint64(prior.BlockNumber), BlockHash: common.HexToHash(prior.BlockHash),
		Logs: []*ethTypes.Log{{Address: coordinator, Topics: []common.Hash{event.ID, {0x31}, {0x32}, {0x33}}, Data: eventData}},
	}
	var readMethods []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		defer request.Body.Close()
		var call struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			return nil, err
		}
		readMethods = append(readMethods, call.Method)
		var result any
		switch call.Method {
		case "eth_getBlockByNumber":
			if len(call.Params) != 2 || string(call.Params[0]) != `"0x32"` {
				return nil, fmt.Errorf("unexpected block request: %s", call.Params)
			}
			result = map[string]any{"number": "0x32", "hash": prior.BlockHash}
		case "eth_getTransactionReceipt":
			if len(call.Params) != 1 || string(call.Params[0]) != `"`+prior.TransactionHash+`"` {
				return nil, fmt.Errorf("unexpected receipt request: %s", call.Params)
			}
			result = receipt
		default:
			return nil, fmt.Errorf("unexpected predecessor method: %s", call.Method)
		}
		wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result})
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(wire))}, nil
	})
	rpcClient, err := rpc.DialOptions(t.Context(), "http://predecessor.example", rpc.WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rpcClient.Close)
	executor := &Executor{keeper: &EvmTxManager{client: ethclient.NewClient(rpcClient)}, plan: &SetupPlan{}, journal: &Journal{}}
	executor.plan.Deployment.CoordinatorProxy = coordinator
	renewal := FleetRenewal{Fleets: []FleetRenewalFleet{{Fleet: 1, Members: []FleetRenewalMember{{Prior: prior}}}}}
	ctx, err := withFinalizedEVMHead(t.Context(), ChainHead{Number: 100, Hash: common.Hash{0x64}.Hex()})
	if err != nil {
		t.Fatal(err)
	}
	entry := JournalEntry{PlanHash: base.PlanHash, Stage: StageFinalized, TransactionHash: prior.TransactionHash, BlockNumber: prior.BlockNumber, BlockHash: prior.BlockHash}
	for _, planHash := range []string{base.PlanHash, base.PriorPlanHashes[0]} {
		approved := entry
		approved.PlanHash = planHash
		executor.journal.entries = []JournalEntry{approved, approved}
		readMethods = nil
		if err := executor.verifyFleetRenewalPredecessors(ctx, renewal, base); err != nil {
			t.Fatalf("approved lineage %s rejected: %v", planHash, err)
		}
		if strings.Join(readMethods, ",") != "eth_getBlockByNumber,eth_getTransactionReceipt" {
			t.Fatalf("approved predecessor reads=%v", readMethods)
		}
	}
	for _, change := range []struct {
		name   string
		mutate func(*JournalEntry)
	}{
		{name: "unapproved lineage", mutate: func(entry *JournalEntry) { entry.PlanHash = "unapproved" }},
		{name: "included only", mutate: func(entry *JournalEntry) { entry.Stage = StageIncluded }},
		{name: "verified only", mutate: func(entry *JournalEntry) { entry.Stage = StageVerified }},
		{name: "transaction", mutate: func(entry *JournalEntry) { entry.TransactionHash = common.Hash{0x61}.Hex() }},
		{name: "block number", mutate: func(entry *JournalEntry) { entry.BlockNumber++ }},
		{name: "block hash", mutate: func(entry *JournalEntry) { entry.BlockHash = common.Hash{0x62}.Hex() }},
	} {
		changed := entry
		change.mutate(&changed)
		executor.journal.entries = []JournalEntry{changed}
		readMethods = nil
		err := executor.verifyFleetRenewalPredecessors(ctx, renewal, base)
		if err == nil || !strings.Contains(err.Error(), "no finalized approved journal lineage") || len(readMethods) != 0 {
			t.Errorf("%s: error=%v reads=%v", change.name, err, readMethods)
		}
	}
}
