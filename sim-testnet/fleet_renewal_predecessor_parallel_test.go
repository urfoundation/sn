package main

// Synthetic RPC barriers exercise bounded read concurrency, exact receipt
// reuse, deterministic failures, and cancellation without external services.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/stabi"
)

// Configuration is fixed before verification starts. Only request accounting
// changes concurrently; all barriers execute outside its lock.
type fleetPredecessorReadFixture struct {
	ctx             context.Context
	base            *SetupPlan
	executor        *Executor
	renewal         FleetRenewal
	receiptKVs      map[string]*ethTypes.Receipt
	blockHashKVs    map[uint64]string
	onReceipt       func(context.Context, string) error
	stateLock       sync.Mutex
	receiptCallKVs  map[string]int
	activeReceipts  int
	maximumReceipts int
	blockCalls      int
}

// Build minimal approved proofs, each at its own canonical historical block.
func newFleetPredecessorReadFixture(t *testing.T, count int) *fleetPredecessorReadFixture {
	t.Helper()
	self := &fleetPredecessorReadFixture{
		base:       &SetupPlan{PlanHash: "approved-test-plan"},
		receiptKVs: make(map[string]*ethTypes.Receipt), blockHashKVs: make(map[uint64]string), receiptCallKVs: make(map[string]int),
		renewal: FleetRenewal{Fleets: []FleetRenewalFleet{{Fleet: 1}}},
	}
	var err error
	self.ctx, err = withFinalizedEVMHead(t.Context(), ChainHead{Number: 1_000, Hash: common.Hash{0x7f}.Hex()})
	if err != nil {
		t.Fatal(err)
	}
	self.executor = &Executor{plan: &SetupPlan{}, journal: &Journal{}}
	self.executor.plan.Deployment.CoordinatorProxy = common.Address{0x51}
	contractAbi, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	event := contractAbi.Events["FleetBound"]
	for index := range count {
		clientId := [16]byte{byte(index + 1)}
		prior := FleetBindingEvidence{
			ClientID: fleetLifecycleHex16(clientId), FleetID: fleetLifecycleHex([32]byte{0x32}), Hotkey: fleetLifecycleHex([32]byte{0x33}),
			UID: 7, Generation: 2, ValidFromEpoch: 10, ValidToEpoch: 20,
			TransactionHash: common.BigToHash(big.NewInt(int64(index + 1))).Hex(), BlockNumber: uint64(index + 1),
			BlockHash: common.BigToHash(big.NewInt(int64(index + 10_000))).Hex(),
		}
		data, err := event.Inputs.NonIndexed().Pack(prior.UID, prior.Generation, prior.ValidFromEpoch, prior.ValidToEpoch)
		if err != nil {
			t.Fatal(err)
		}
		self.receiptKVs[prior.TransactionHash] = &ethTypes.Receipt{
			Status: ethTypes.ReceiptStatusSuccessful, TxHash: common.HexToHash(prior.TransactionHash),
			BlockNumber: new(big.Int).SetUint64(prior.BlockNumber), BlockHash: common.HexToHash(prior.BlockHash),
			Logs: []*ethTypes.Log{{Address: self.executor.plan.Deployment.CoordinatorProxy,
				Topics: []common.Hash{event.ID, {byte(index + 1)}, {0x32}, {0x33}}, Data: data}},
		}
		self.blockHashKVs[prior.BlockNumber] = prior.BlockHash
		self.executor.journal.entries = append(self.executor.journal.entries, JournalEntry{
			PlanHash: self.base.PlanHash, Stage: StageFinalized, TransactionHash: prior.TransactionHash, BlockNumber: prior.BlockNumber, BlockHash: prior.BlockHash,
		})
		self.renewal.Fleets[0].Members = append(self.renewal.Fleets[0].Members, FleetRenewalMember{Prior: prior})
	}
	client, err := rpc.DialOptions(t.Context(), "http://predecessor.example", rpc.WithHTTPClient(&http.Client{Transport: self}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	self.executor.keeper = &EvmTxManager{client: ethclient.NewClient(client)}
	return self
}

// Serve only canonical block and receipt reads; a write cannot escape a test.
func (self *fleetPredecessorReadFixture) RoundTrip(request *http.Request) (*http.Response, error) {
	defer request.Body.Close()
	var call struct {
		Id     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		return nil, err
	}
	var result any
	switch call.Method {
	case "eth_getBlockByNumber":
		var selector string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
			return nil, errors.New("invalid historical block request")
		}
		block, err := hexutil.DecodeUint64(selector)
		if err != nil {
			return nil, err
		}
		hash, ok := self.blockHashKVs[block]
		if !ok {
			return nil, errors.New("unexpected historical block")
		}
		self.stateLock.Lock()
		self.blockCalls++
		self.stateLock.Unlock()
		result = map[string]any{"number": selector, "hash": hash}
	case "eth_getTransactionReceipt":
		var transactionHash string
		if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &transactionHash) != nil {
			return nil, errors.New("invalid historical receipt request")
		}
		receipt, ok := self.receiptKVs[transactionHash]
		if !ok {
			return nil, errors.New("unexpected historical transaction")
		}
		self.stateLock.Lock()
		self.receiptCallKVs[transactionHash]++
		self.activeReceipts++
		self.maximumReceipts = max(self.maximumReceipts, self.activeReceipts)
		self.stateLock.Unlock()
		defer func() {
			self.stateLock.Lock()
			self.activeReceipts--
			self.stateLock.Unlock()
		}()
		if self.onReceipt != nil {
			if err := self.onReceipt(request.Context(), transactionHash); err != nil {
				return nil, err
			}
		}
		result = receipt
	default:
		return nil, fmt.Errorf("unexpected RPC method %s", call.Method)
	}
	wire, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(wire))}, nil
}

// The watchdog only bounds a broken test; channel barriers force the ordering.
// Cleanup always cancels and joins the verifier, including assertion failures.
func (self *fleetPredecessorReadFixture) start(t *testing.T) (context.Context, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(self.ctx, 20*time.Second)
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- self.executor.verifyFleetRenewalPredecessors(ctx, self.renewal, self.base)
	}()
	t.Cleanup(func() { cancel(); <-finished })
	return ctx, cancel, done
}

// A held first wave proves actual parallel reads. Every duplicate still has
// its event checked, while the canonical block/receipt is fetched once.
func TestFleetRenewalPredecessorsBoundParallelReadsAndDeduplicate(t *testing.T) {
	count := 2*fleetRenewalPredecessorReadConcurrency + 1
	fixture := newFleetPredecessorReadFixture(t, count)
	fixture.renewal.Fleets[0].Members = append(fixture.renewal.Fleets[0].Members, fixture.renewal.Fleets[0].Members...)
	entered := make(chan string, count)
	release := make(chan struct{})
	fixture.onReceipt = func(ctx context.Context, hash string) error {
		entered <- hash
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ctx, _, done := fixture.start(t)
	for range fleetRenewalPredecessorReadConcurrency {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("independent receipts did not fill the worker wave", ctx.Err())
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if fixture.maximumReceipts != fleetRenewalPredecessorReadConcurrency || fixture.activeReceipts != 0 || fixture.blockCalls != count {
		t.Fatalf("read work: peak=%d active=%d blocks=%d", fixture.maximumReceipts, fixture.activeReceipts, fixture.blockCalls)
	}
	for hash, calls := range fixture.receiptCallKVs {
		if calls != 1 {
			t.Errorf("receipt %s fetched %d times", hash, calls)
		}
	}
	if len(fixture.receiptCallKVs) != count {
		t.Fatalf("unique receipts=%d, want %d", len(fixture.receiptCallKVs), count)
	}
}

// Cancellation releases every occupied worker and cannot dispatch another
// physical receipt read after the barrier has observed the canceled context.
func TestFleetRenewalPredecessorsCancelAndJoinReadWorkers(t *testing.T) {
	fixture := newFleetPredecessorReadFixture(t, 2*fleetRenewalPredecessorReadConcurrency)
	entered := make(chan struct{}, len(fixture.receiptKVs))
	fixture.onReceipt = func(ctx context.Context, _ string) error {
		entered <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel, done := fixture.start(t)
	for range fleetRenewalPredecessorReadConcurrency {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("receipt workers did not reach the cancellation barrier", ctx.Err())
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled proof returned %v", err)
	}
	fixture.stateLock.Lock()
	defer fixture.stateLock.Unlock()
	if fixture.activeReceipts != 0 || len(fixture.receiptCallKVs) != fleetRenewalPredecessorReadConcurrency {
		t.Fatalf("workers escaped cancellation: active=%d receipts=%d", fixture.activeReceipts, len(fixture.receiptCallKVs))
	}
}

// An already-canceled invocation must not begin even its first chain read.
func TestFleetRenewalPredecessorsRejectAlreadyCanceledContext(t *testing.T) {
	fixture := newFleetPredecessorReadFixture(t, 1)
	ctx, cancel := context.WithCancel(fixture.ctx)
	cancel()
	if err := fixture.executor.verifyFleetRenewalPredecessors(ctx, fixture.renewal, fixture.base); !errors.Is(err, context.Canceled) {
		t.Fatalf("already-canceled proof returned %v", err)
	}
	if fixture.blockCalls != 0 || len(fixture.receiptCallKVs) != 0 {
		t.Fatal("already-canceled proof issued RPC reads")
	}
}

// Later proofs may finish first, but the reported defect remains the earliest
// approved proof, independent of request scheduling.
func TestFleetRenewalPredecessorsSelectErrorsInPlanOrder(t *testing.T) {
	fixture := newFleetPredecessorReadFixture(t, 2)
	first := fixture.renewal.Fleets[0].Members[0].Prior.TransactionHash
	secondEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	fixture.onReceipt = func(ctx context.Context, hash string) error {
		if hash != first {
			close(secondEntered)
			return errors.New("second proof failed")
		}
		select {
		case <-releaseFirst:
			return errors.New("first proof failed")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ctx, _, done := fixture.start(t)
	select {
	case <-secondEntered:
	case <-ctx.Done():
		t.Fatal("second proof did not pass the held first proof", ctx.Err())
	}
	close(releaseFirst)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "first proof failed") || strings.Contains(err.Error(), "second proof failed") {
		t.Fatalf("completion order changed selected error: %v", err)
	}
}

// Sharing a receipt never shares the per-member FleetBound verdict.
func TestFleetRenewalPredecessorsRecheckEveryMemberOfSharedReceipt(t *testing.T) {
	fixture := newFleetPredecessorReadFixture(t, 1)
	changed := fixture.renewal.Fleets[0].Members[0]
	changed.Prior.UID++
	fixture.renewal.Fleets[0].Members = append(fixture.renewal.Fleets[0].Members, changed)
	err := fixture.executor.verifyFleetRenewalPredecessors(fixture.ctx, fixture.renewal, fixture.base)
	if err == nil || !strings.Contains(err.Error(), "exactly one matching FleetBound event") {
		t.Fatalf("shared receipt concealed changed member: %v", err)
	}
	if fixture.receiptCallKVs[changed.Prior.TransactionHash] != 1 {
		t.Fatal("shared exact receipt was not deduplicated")
	}
}

// A transaction-only cache used to accept a second contradictory checkpoint
// without rechecking the receipt's canonical block identity.
func TestFleetRenewalPredecessorsDoNotReuseConflictingCheckpoint(t *testing.T) {
	fixture := newFleetPredecessorReadFixture(t, 1)
	changed := fixture.renewal.Fleets[0].Members[0]
	changed.Prior.BlockNumber++
	changed.Prior.BlockHash = common.Hash{0x66}.Hex()
	fixture.renewal.Fleets[0].Members = append(fixture.renewal.Fleets[0].Members, changed)
	entry := fixture.executor.journal.entries[0]
	entry.BlockNumber, entry.BlockHash = changed.Prior.BlockNumber, changed.Prior.BlockHash
	fixture.executor.journal.entries = append(fixture.executor.journal.entries, entry)
	fixture.blockHashKVs[entry.BlockNumber] = entry.BlockHash
	err := fixture.executor.verifyFleetRenewalPredecessors(fixture.ctx, fixture.renewal, fixture.base)
	if err == nil || !strings.Contains(err.Error(), "receipt does not match its canonical finalized evidence") {
		t.Fatalf("transaction-only reuse concealed conflicting checkpoint: %v", err)
	}
	if fixture.receiptCallKVs[changed.Prior.TransactionHash] != 2 {
		t.Fatal("different checkpoints incorrectly shared one authenticated receipt")
	}
}
