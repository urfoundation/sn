// Public command controls retain original custody and use real HTTP, ABI and
// trie decoding. Retry clocks are injected only after the actual transient read.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

func TestMonitorEconomicEvmPublicRestartRetainsCreditCarryAndAggregatePayment(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	fixture.policy.BatchBlocks = 1
	first := fixture.start(t, monitorServiceHooks{})
	event := first.next(t)
	if !event.Current || event.State.Cursor.Number != 11 || event.State.ContractState == nil || event.State.ContractState.Credits[fixture.policy.Coldkeys[0]] != "12" || event.State.ContractState.Pools["1"] != "50" || event.State.ContractState.Counters["totalPaid"] != "0" {
		t.Fatal("accepted/deferred claim was lost or relabelled as payment", event)
	}
	first.stop(t)
	second := fixture.start(t, monitorServiceHooks{})
	event = second.next(t)
	if !event.Current || event.State.Cursor.Number != 12 || event.State.BatchCount != 2 || event.State.ContractState == nil || event.State.ContractState.Credits[fixture.policy.Coldkeys[0]] != "0" || event.State.ContractState.Pools["1"] != "0" || event.State.ContractState.Counters["totalPaid"] != "15" || event.State.ObservedFeeCostWei == nil || *event.State.ObservedFeeCostWei != "84000" {
		t.Fatal("restart lost original carry, aggregate credit or exact transaction costs", event)
	}
	if event.State.NativeFeeDebitRao != nil || event.State.NativeFeeRefundRao != nil || event.State.NativeMinerAllocationAlpha != nil || event.State.CompleteProviderEntitlementAlpha != nil || event.State.NativeFeeExecutionVerified || event.State.IndependentFinalityVerified || event.State.Authority != "owned-rpc-assertion" {
		t.Fatal("owned observations were promoted to independent economic authority", event)
	}
	second.stop(t)
	third := fixture.start(t, monitorServiceHooks{})
	event = third.next(t)
	if !event.Current || event.State.Cursor.Number != 13 || event.State.HistoryEntries != 9 || event.State.BatchCount != 3 {
		t.Fatal("quiet block skipped or duplicated original events", event)
	}
	third.resume <- struct{}{}
	again := third.next(t)
	if !again.Current || again.State.BatchCount != event.State.BatchCount || again.State.BatchChainHash != event.State.BatchChainHash || again.State.HistoryEntries != event.State.HistoryEntries {
		t.Fatal("same finalized head replayed a financial event", again)
	}
	third.stop(t)
	if first.exit != 0 || second.exit != 0 || third.exit != 0 {
		t.Fatal("public EVM restart did not join", first.exit, second.exit, third.exit, first.diagnostic.String(), second.diagnostic.String(), third.diagnostic.String())
	}
}

func TestMonitorEconomicEvmPublicPartialReceiptOutageKeepsOriginalCursor(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	fixture.unavailable.Store(true)
	var waits, closed atomic.Int32
	run := fixture.start(t, monitorServiceHooks{rpcWait: func(ctx context.Context, role string, _ time.Duration) error {
		deadline, ok := ctx.Deadline()
		if role != fixture.policy.Role || !ok || time.Until(deadline) < time.Minute || time.Until(deadline) > 300*time.Second {
			return errors.New("EVM logical operation lost its one 300-second deadline")
		}
		waits.Add(1)
		return context.DeadlineExceeded
	}, afterClose: func(role, kind string, _ *os.File) error {
		if role == fixture.policy.Role && kind == "checkpoint" {
			closed.Add(1)
		}
		return nil
	}})
	first := run.next(t)
	if first.Current || first.Status != "unavailable" || first.State.Cursor != fixture.policy.From || first.State.HistoryEntries != 0 || first.State.ContractState != nil || waits.Load() != 1 {
		t.Fatal("partial receipt page advanced or fabricated economic progress", first, waits.Load())
	}
	if record := fixture.record(t); record.State.Cursor != fixture.policy.From || len(record.State.History) != 0 {
		t.Fatal("partial receipt page escaped into durable checkpoint", record)
	}
	fixture.unavailable.Store(false)
	run.resume <- struct{}{}
	second := run.next(t)
	if !second.Current || second.State.Cursor.Number != 13 || second.State.HistoryEntries != 9 || closed.Load() != 0 || second.State.Incidents != 1 {
		t.Fatal("transient EVM outage replaced custody or lost same-page recovery", second, closed.Load())
	}
	run.stop(t)
	if run.exit != 0 || closed.Load() != 1 {
		t.Fatal("EVM cancellation failed to join original owner", run.exit, closed.Load())
	}
}

func TestMonitorEconomicEvmPublicLostCheckpointAckDoesNotRepeatPayment(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	var failed atomic.Bool
	joined := make(chan struct{}, 2)
	run := fixture.start(t, monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == fixture.policy.Role && kind == "checkpoint" && failed.CompareAndSwap(false, true) {
			return errors.Join(err, syscall.EIO)
		}
		return err
	}, afterClose: func(role, kind string, file *os.File) error {
		if role == fixture.policy.Role && kind == "checkpoint" {
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				return errors.New("uncertain EVM owner remained open")
			}
			joined <- struct{}{}
		}
		return nil
	}})
	first := run.next(t)
	if first.Current || first.CheckpointCurrent || first.State.Cursor != fixture.policy.From {
		t.Fatal("lost checkpoint acknowledgment advanced public EVM cursor", first)
	}
	select {
	case <-joined:
	case <-time.After(10 * time.Second):
		t.Fatal("uncertain EVM owner did not join")
	}
	record := fixture.record(t)
	if record.State.Cursor.Number != 13 || record.State.Snapshot == nil || record.State.Snapshot.Counters["totalPaid"] != "15" {
		t.Fatal("actual retained write lost completed aggregate payment", record)
	}
	run.resume <- struct{}{}
	second := run.next(t)
	if !second.Current || second.State.Cursor.Number != 13 || second.State.BatchCount != 1 || second.State.HistoryEntries != 9 {
		t.Fatal("lost acknowledgment replayed or discarded EVM events", second)
	}
	run.stop(t)
}

func TestMonitorEconomicEvmPublicReceiptContradictionStopsOnlyAffectedRole(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", true)
	fixture.setFault(func(method string, _ []any, result any) (any, bool) {
		if method != "eth_getTransactionReceipt" {
			return nil, false
		}
		original := result.(map[string]any)
		changed := map[string]any{}
		for key, value := range original {
			changed[key] = value
		}
		changed["gasUsed"] = "0x1"
		return changed, true
	})
	stopped := make(chan int, 1)
	run := fixture.start(t, monitorServiceHooks{afterWorker: func(role string, exit int) {
		if role == fixture.policy.Role {
			stopped <- exit
		}
	}})
	event := run.next(t)
	if event.Current || event.Status != "identity-conflict" || event.State.Cursor != fixture.policy.From {
		t.Fatal("receipt contradiction advanced affected EVM role", event)
	}
	select {
	case exit := <-stopped:
		if exit != 3 {
			t.Fatal("EVM contradiction was not terminal", exit)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("affected EVM role did not join")
	}
	select {
	case peer := <-run.sink.peers:
		if peer.Role != "validator-a" || peer.Publication != "published" || peer.State == nil || peer.State.ReadStatus != "ok" || peer.State.Record == nil || peer.State.LastReadSuccessAt.IsZero() {
			t.Fatal("healthy validator did not publish its actual first read", peer)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("healthy validator never observed")
	}
	fixture.services.clock.seconds.Add(1)
	fresh := monitorServicesTestRecord(fixture.services.clock.now(), 1)
	monitorServicesTestWrite(t, fixture.services.policy.Validators[0].ProgressFile, fresh)
	run.peerResume <- struct{}{}
	select {
	case peer := <-run.sink.peers:
		if peer.Role != "validator-a" || peer.Publication != "published" || peer.State == nil || peer.State.ReadStatus != "ok" || peer.State.Record == nil || !peer.State.LastReadSuccessAt.Equal(fixture.services.clock.now()) || peer.State.Record.HeartbeatAt != fresh.HeartbeatAt {
			t.Fatal("healthy peer lost progress after EVM contradiction", peer)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("EVM contradiction stopped unrelated validator")
	}
	run.stop(t)
	if run.exit != 3 {
		t.Fatal("public role failure was hidden", run.exit)
	}
}

func TestMonitorEconomicEvmPublicReserveReadsActualPrincipalAndBacking(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "reserve-sink", false)
	run := fixture.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 13 || event.State.ContractState == nil || event.State.ContractState.Counters["principal"] != "120" || event.State.ContractState.Pools["1"] != "60" || event.State.ContractState.Counters["liveStake"] != "140" || event.State.HistoryEntries != 2 {
		t.Fatal("reserve role observed only recorder installation or lost actual principal", event)
	}
	run.stop(t)
}

func TestMonitorEconomicEvmReaderRequiresCompleteReceiptAndTransactionTries(t *testing.T) {
	for _, mode := range []string{"missing-transaction", "missing-log", "duplicate-log", "wrong-cumulative", "wrong-block", "price-above-signed"} {
		fixture := newMonitorEvmFixture(t, "settlement-vault", false)
		fixture.setFault(func(method string, params []any, result any) (any, bool) {
			if mode == "missing-transaction" && method == "eth_getBlockByHash" && params[1] == true {
				original := result.(map[string]any)
				changed := map[string]any{}
				for key, value := range original {
					changed[key] = value
				}
				changed["transactions"] = []any{}
				return changed, true
			}
			if method != "eth_getTransactionReceipt" {
				return nil, false
			}
			original := result.(map[string]any)
			changed := map[string]any{}
			for key, value := range original {
				changed[key] = value
			}
			switch mode {
			case "missing-log":
				changed["logs"] = []any{}
			case "duplicate-log":
				logs := original["logs"].([]any)
				changed["logs"] = append(append([]any{}, logs...), logs[0])
			case "wrong-cumulative":
				changed["cumulativeGasUsed"] = "0x1"
			case "wrong-block":
				changed["blockHash"] = "0x" + strings.Repeat("55", 32)
			case "price-above-signed":
				changed["effectiveGasPrice"] = "0x3"
			default:
				return nil, false
			}
			return changed, true
		})
		observation, err := observeMonitorEconomicEvm(t.Context(), monitorEvmFixtureClient(t, fixture), fixture.policy, newMonitorEconomicEvmState(fixture.policy))
		if !errors.Is(err, errRpcIntegrity) || observation != nil {
			t.Fatal("incomplete/contradictory EVM trie became financial evidence", mode, observation, err)
		}
	}
}

func TestMonitorEconomicEvmReaderRefusesChangedCursorAndCode(t *testing.T) {
	for _, mode := range []string{"cursor", "code", "counter", "genesis", "final-canonical"} {
		fixture := newMonitorEvmFixture(t, "settlement-vault", false)
		var cursorReads atomic.Int32
		fixture.setFault(func(method string, params []any, result any) (any, bool) {
			if method == "eth_getCode" && mode == "code" {
				return "0x6001", true
			}
			if method == "eth_call" && mode == "counter" {
				call := params[0].(map[string]any)
				data, _ := hexutil.Decode(call["data"].(string))
				selector, _ := fixture.contract.MethodById(data[:4])
				if selector.Name == "totalPaid" {
					return "0x" + strings.Repeat("0", 63) + "1", true
				}
			}
			if method == "eth_getBlockByNumber" && (mode == "cursor" && params[0] == "0xa" || mode == "genesis" && params[0] == "0x0" || mode == "final-canonical" && params[0] == "0xa" && cursorReads.Add(1) > 1) {
				original := result.(map[string]any)
				changed := map[string]any{}
				for key, value := range original {
					changed[key] = value
				}
				changed["hash"] = fixture.blocks[11].header.Hash().Hex()
				return changed, true
			}
			return nil, false
		})
		observation, err := observeMonitorEconomicEvm(t.Context(), monitorEvmFixtureClient(t, fixture), fixture.policy, newMonitorEconomicEvmState(fixture.policy))
		if !errors.Is(err, errRpcIntegrity) || observation != nil {
			t.Fatal("changed EVM identity/custody became a current sample", mode, observation, err)
		}
	}
}

func TestMonitorEconomicEvmLedgerSeparatesDeferredCreditAndAggregatePayment(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	observation, err := observeMonitorEconomicEvm(t.Context(), monitorEvmFixtureClient(t, fixture), fixture.policy, newMonitorEconomicEvmState(fixture.policy))
	if err != nil || observation == nil || len(observation.Blocks) != 3 {
		t.Fatal("actual EVM ledger fixture did not close", err)
	}
	first := observation.Blocks[0]
	if first.Snapshot.Counters["totalPaid"] != "0" || first.Snapshot.Credits[fixture.policy.Coldkeys[0]] != "12" {
		t.Fatal("accepted claim was treated as payment", first)
	}
	for _, mode := range []string{"accepted-as-paid", "small-aggregate", "lost-carry", "funding-as-total"} {
		encoded, _ := json.Marshal(observation.Blocks[1])
		var block monitorEvmBlock
		if err := json.Unmarshal(encoded, &block); err != nil {
			t.Fatal(err)
		}
		switch mode {
		case "accepted-as-paid":
			block.Events[1].Name = "ClaimPaid"
		case "small-aggregate":
			block.Events[2].Values["amount"] = "3"
		case "lost-carry":
			block.Events[0].Values["total"] = "0"
		case "funding-as-total":
			block.Funded["3/1"] = "50"
		}
		if err := monitorEvmCheckLedger(fixture.policy, first.Snapshot, block); !errors.Is(err, errRpcIntegrity) {
			t.Fatal("economic ledger accepted a payment/carry authority substitution", mode, err)
		}
	}
}

func TestMonitorEconomicEvmPublicCapacityKeepsOriginalPendingRange(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	fixture.policy.HistoryEntries = 5
	run := fixture.start(t, monitorServiceHooks{})
	first := run.next(t)
	if !first.Current || first.State.Cursor.Number != 11 || first.State.PendingThrough == nil || first.State.PendingThrough.Number != 13 || first.State.HistoryEntries != 5 {
		t.Fatal("bounded history skipped first complete subpage or lost high water", first)
	}
	run.resume <- struct{}{}
	second := run.next(t)
	if second.Current || second.Status != "capacity-held" || second.State.Cursor != first.State.Cursor || second.State.BatchChainHash != first.State.BatchChainHash || second.State.PendingThrough == nil || *second.State.PendingThrough != *first.State.PendingThrough {
		t.Fatal("history exhaustion erased or skipped unresolved original page", second)
	}
	run.stop(t)
}

func TestMonitorEconomicEvmPublicCancellationJoinsInFlightRead(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	fixture.unavailable.Store(true)
	waiting := make(chan struct{})
	var once atomic.Bool
	run := fixture.start(t, monitorServiceHooks{rpcWait: func(ctx context.Context, _ string, _ time.Duration) error {
		if once.CompareAndSwap(false, true) {
			close(waiting)
		}
		<-ctx.Done()
		return ctx.Err()
	}})
	select {
	case <-waiting:
	case <-time.After(10 * time.Second):
		t.Fatal("actual transient receipt read did not reach owner wait")
	}
	run.stop(t)
	if run.exit != 0 {
		t.Fatal("caller cancellation became a terminal financial contradiction", run.exit, run.diagnostic.String())
	}
	select {
	case event := <-run.sink.events:
		t.Fatal("canceled partial read published fabricated economic progress", event)
	default:
	}
}

func TestMonitorEconomicEvmPolicyDoesNotBorrowDiscoveredContractIdentity(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	expected := identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}
	if err := fixture.policy.validate(expected); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"purpose", "duplicate-pool", "unbounded-history", "short-budget", "zero-contract", "native-identity"} {
		policy := fixture.policy
		switch mode {
		case "purpose":
			policy.ContractKind = "discovered"
		case "duplicate-pool":
			policy.PoolIds = []string{"1", "1"}
		case "unbounded-history":
			policy.HistoryEntries = 2049
		case "short-budget":
			policy.ReadBudgetSeconds = 59
		case "zero-contract":
			policy.Address = "0x0000000000000000000000000000000000000000"
		case "native-identity":
			policy.Network.GenesisHash = "0x" + strings.Repeat("51", 32)
		}
		if err := policy.validate(expected); err == nil {
			t.Fatal("unreviewed economic expectation was admitted", mode)
		}
	}
}

// Failed execution still consumes gas. The signed transaction and receipt root
// bind that cost observation; a successful contract event is not its prerequisite.
func TestMonitorEconomicEvmReceiptFeeIncludesFailedExecution(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	block := fixture.blocks[11]
	var receipt types.Receipt
	raw, _ := json.Marshal(block.receipts[0])
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	delete(fixture.byHash, block.header.Hash().Hex())
	receipt.Status = 0
	receipt.Logs = []*types.Log{}
	receipt.Bloom = types.CreateBloom(&receipt)
	block.header.ReceiptHash = types.DeriveSha(types.Receipts{&receipt}, trie.NewStackTrie(nil))
	block.header.Bloom = receipt.Bloom
	receipt.BlockHash = block.header.Hash()
	block.snapshot = fixture.blocks[10].snapshot.clone()
	block.funded = map[string]string{}
	raw, err := json.Marshal(&receipt)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["from"], fields["to"], fields["contractAddress"] = fixture.policy.FeePayers[0], fixture.policy.Address, nil
	block.receipts = []map[string]any{fields}
	fixture.byTransaction[receipt.TxHash.Hex()] = fields
	fixture.byHash[receipt.BlockHash.Hex()] = block
	fixture.mapping.evmHeader = block.header
	fixture.mapping.evmHash = block.header.Hash().Hex()
	encoded, err := rlp.EncodeToBytes(block.header)
	if err != nil {
		t.Fatal(err)
	}
	fixture.mapping.rawEvmHeader = hexutil.Encode(encoded)
	fixture.mapping.replaceNativeLogs(t, []string{mappingTestDigest(t, 3, fixture.mapping.evmHash, nil)})
	run := fixture.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 11 || event.State.HistoryEntries != 1 || event.State.ObservedFeeCostWei == nil || *event.State.ObservedFeeCostWei != "42000" || event.State.ContractState == nil || event.State.ContractState.Counters["totalPaid"] != "0" {
		t.Fatal("failed included transaction lost its gas observation or minted a contract payment", event)
	}
	run.stop(t)
	record := fixture.record(t)
	if len(record.State.Fees) != 1 || record.State.Fees[0].Success || record.State.Fees[0].TransactionHash != receipt.TxHash.Hex() || len(record.State.History) != 0 {
		t.Fatal("failed receipt lost original payer/transaction position", record)
	}
}

func TestMonitorEconomicEvmPublicReviewedResourceGrowthKeepsOriginalHistory(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	fixture.policy.HistoryEntries = 5
	fixture.policy.BatchBlocks = 1
	first := fixture.start(t, monitorServiceHooks{})
	old := first.next(t)
	first.stop(t)
	if !old.Current || old.State.Cursor.Number != 11 {
		t.Fatal("original EVM policy did not retain first page", old)
	}
	original := fixture.policy.resources()
	fixture.policy.ResourceRevision = &monitorEvmResourceRevision{Original: original, ReviewSha256: "sha256:" + strings.Repeat("6", 64)}
	fixture.policy.ReadBudgetSeconds = 600
	fixture.policy.HistoryEntries = 64
	fixture.policy.BatchBlocks = 3
	fixture.policy.StallSeconds = 120
	fixture.services.policy.EvmEconomics = []monitorEconomicEvmPolicy{fixture.policy}
	fixture.services.writePolicy(t)
	second := fixture.start(t, monitorServiceHooks{})
	next := second.next(t)
	second.stop(t)
	if !next.Current || next.State.Cursor.Number != 13 || next.State.BatchCount != 2 || next.State.HistoryEntries != 9 || next.State.ObservedFeeCostWei == nil || *next.State.ObservedFeeCostWei != "84000" || next.State.AcknowledgedResources == nil || next.State.AcknowledgedResources.ReadBudgetSeconds != 600 || next.State.AcknowledgedResources.HistoryEntries != 64 {
		t.Fatal("reviewed resource increase reset history or failed to acknowledge retained continuation", next)
	}
	record := fixture.record(t)
	if record.PolicyHash != fixture.policy.identityHash() || record.State.History[0].Block.Number != 11 || record.Resources == nil || record.Resources.ReadBudgetSeconds != 600 {
		t.Fatal("resource review replaced original event or identity", record)
	}
	if record.State.BatchChainHash == old.State.BatchChainHash {
		t.Fatal("continued complete blocks did not advance chained evidence")
	}
}

func TestMonitorEconomicEvmResourceShrinkAndContractReplacementRefuseRetainedCheckpoint(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	fixture.policy.BatchBlocks = 1
	first := fixture.start(t, monitorServiceHooks{})
	if event := first.next(t); !event.Current {
		t.Fatal(event)
	}
	first.stop(t)
	original := fixture.policy.resources()
	fixture.policy.ResourceRevision = &monitorEvmResourceRevision{Original: original, ReviewSha256: "sha256:" + strings.Repeat("7", 64)}
	fixture.policy.ReadBudgetSeconds = 600
	fixture.policy.HistoryEntries = 128
	fixture.services.policy.EvmEconomics = []monitorEconomicEvmPolicy{fixture.policy}
	fixture.services.writePolicy(t)
	second := fixture.start(t, monitorServiceHooks{})
	if event := second.next(t); !event.Current {
		t.Fatal(event)
	}
	second.stop(t)
	path, _ := monitorEconomicEvmPaths(fixture.services.checkpointPath, fixture.services.metricsPath, fixture.policy.Role)
	expected := identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}
	for _, mode := range []string{"budget", "capacity", "contract", "pool", "payer", "original-basis"} {
		policy := fixture.policy
		revision := *policy.ResourceRevision
		policy.ResourceRevision = &revision
		switch mode {
		case "budget":
			policy.ReadBudgetSeconds = 300
		case "capacity":
			policy.HistoryEntries = 64
		case "contract":
			policy.CodeHash = "0x" + strings.Repeat("5", 64)
		case "pool":
			policy.PoolIds = []string{"2"}
		case "payer":
			policy.FeePayers = nil
		case "original-basis":
			policy.ResourceRevision.Original.ReadBudgetSeconds = 60
		}
		owner, err := openMonitorCheckpoint(path, expected, fixture.ctx)
		if err != nil {
			t.Fatal(err)
		}
		worker := &monitorEconomicEvmWorker{policy: policy, checkpoint: owner}
		_, loadErr := worker.load(fixture.ctx)
		closeErr := owner.close()
		if loadErr == nil || closeErr != nil {
			t.Fatal("retained EVM checkpoint accepted resource shrink or identity replacement", mode, loadErr, closeErr)
		}
	}
}

func TestMonitorEconomicEvmResourceAcknowledgmentWaitsForDurablePublication(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	fixture.policy.BatchBlocks = 1
	first := fixture.start(t, monitorServiceHooks{})
	old := first.next(t)
	first.stop(t)
	if !old.Current {
		t.Fatal(old)
	}
	fixture.policy.ResourceRevision = &monitorEvmResourceRevision{Original: fixture.policy.resources(), ReviewSha256: "sha256:" + strings.Repeat("8", 64)}
	fixture.policy.ReadBudgetSeconds = 600
	fixture.services.policy.EvmEconomics = []monitorEconomicEvmPolicy{fixture.policy}
	fixture.services.writePolicy(t)
	var failed atomic.Bool
	run := fixture.start(t, monitorServiceHooks{syncDirectory: func(role, kind string, file *os.File) error {
		err := file.Sync()
		if role == fixture.policy.Role && kind == "checkpoint" && failed.CompareAndSwap(false, true) {
			return errors.Join(err, syscall.EIO)
		}
		return err
	}})
	event := run.next(t)
	if event.Current || event.CheckpointCurrent || event.State.ConfiguredResources.ReadBudgetSeconds != 600 || event.State.AcknowledgedResources == nil || event.State.AcknowledgedResources.ReadBudgetSeconds != 0 || event.State.Cursor != old.State.Cursor || event.State.ResourceReviewHistory.Entries != 1 {
		t.Fatal("unacknowledged resource renewal advertised a completed checkpoint", event)
	}
	run.resume <- struct{}{}
	recovered := run.next(t)
	if !recovered.Current || recovered.State.AcknowledgedResources == nil || recovered.State.AcknowledgedResources.ReadBudgetSeconds != 600 || recovered.State.HistoryEntries != 9 || recovered.State.ResourceReviewHistory.Entries != 2 {
		t.Fatal("lost resource acknowledgment did not recover original same history", recovered)
	}
	run.stop(t)
}

func TestMonitorEconomicEvmPublicLargePageCommitsCompletePrefixThenContinues(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	fixture.policy.BatchBlocks = 32
	parent := fixture.blocks[13]
	for number := uint64(14); number <= 30; number++ {
		header := types.CopyHeader(parent.header)
		header.ParentHash = parent.header.Hash()
		header.Number = new(big.Int).SetUint64(number)
		header.Time += 12000
		block := &monitorEvmFixtureBlock{header: header, transactions: types.Transactions{}, receipts: []map[string]any{}, snapshot: parent.snapshot.clone(), funded: map[string]string{}}
		fixture.blocks[number], fixture.byHash[header.Hash().Hex()] = block, block
		parent = block
	}
	fixture.setFinalized(t, 30)
	padding := strings.Repeat("p", 600*1024)
	fixture.setFault(func(method string, params []any, result any) (any, bool) {
		if method != "eth_getBlockByHash" || params[1] != true {
			return nil, false
		}
		original := result.(map[string]any)
		changed := map[string]any{}
		for key, value := range original {
			changed[key] = value
		}
		changed["syntheticNodeAnnotation"] = padding
		return changed, true
	})
	run := fixture.start(t, monitorServiceHooks{})
	first := run.next(t)
	if !first.Current || first.State.Cursor.Number <= 13 || first.State.Cursor.Number >= 30 || first.State.PendingThrough == nil || first.State.PendingThrough.Number != 30 || first.State.HistoryEntries != 9 {
		t.Fatal("bounded EVM byte page stalled or lost complete original prefix", first)
	}
	run.resume <- struct{}{}
	second := run.next(t)
	if !second.Current || second.State.Cursor.Number != 30 || second.State.PendingThrough != nil || second.State.HistoryEntries != 9 || second.State.BatchCount != 2 || second.State.BatchChainHash == first.State.BatchChainHash {
		t.Fatal("EVM capacity continuation repeated or skipped original evidence", second)
	}
	run.stop(t)
}

func TestMonitorEconomicEvmPublicHistoricalBacklogDoesNotResetOrRequireFullAncestry(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	header := types.CopyHeader(fixture.blocks[13].header)
	header.Number = big.NewInt(10000)
	block := &monitorEvmFixtureBlock{header: header, transactions: types.Transactions{}, receipts: []map[string]any{}, snapshot: fixture.blocks[13].snapshot.clone(), funded: map[string]string{}}
	fixture.blocks[10000], fixture.byHash[header.Hash().Hex()] = block, block
	fixture.setFinalized(t, 10000)
	run := fixture.start(t, monitorServiceHooks{})
	event := run.next(t)
	if !event.Current || event.State.Cursor.Number != 13 || event.State.Finalized == nil || event.State.Finalized.Number != 10000 || event.State.HistoryEntries != 9 || event.State.IndependentFinalityVerified {
		t.Fatal("long EVM outage abandoned original bounded cursor or invented independent ancestry", event)
	}
	run.stop(t)
}
