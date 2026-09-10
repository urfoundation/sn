package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

// Serve the actual ABI for each pinned request, with controllable current
// finality, historical drift and a separately failing receipt endpoint.
type fleetInstallHistoryCacheRPC struct {
	mu              sync.Mutex
	t               *testing.T
	finalized       uint64
	noncanonical    bool
	badState        bool
	outputs         map[string]string
	finalizedReads  int
	canonicalReads  int
	contractReads   int
	contractBatches int
	receiptReads    int
}

func (f *fleetInstallHistoryCacheRPC) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	defer request.Body.Close()
	wire, err := io.ReadAll(request.Body)
	if err != nil {
		f.t.Errorf("read install fixture request: %v", err)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(strings.TrimSpace(string(wire)), "[") {
		var calls []fleetHistoryBatchRPCRequest
		if err := json.Unmarshal(wire, &calls); err != nil || len(calls) == 0 || len(calls) > maximumEVMRPCBatchCalls {
			f.t.Errorf("invalid install fixture batch: size=%d err=%v", len(calls), err)
			return
		}
		f.contractBatches++
		responses := make([]map[string]any, len(calls))
		for index, call := range calls {
			if call.Method != "eth_call" || len(call.Params) != 2 {
				f.t.Errorf("unexpected install fixture batch method=%s", call.Method)
				return
			}
			var message struct {
				To   common.Address `json:"to"`
				Data hexutil.Bytes  `json:"data"`
			}
			var selector string
			if json.Unmarshal(call.Params[0], &message) != nil || json.Unmarshal(call.Params[1], &selector) != nil {
				f.t.Error("invalid install call arguments")
				return
			}
			if _, err := hexutil.DecodeUint64(selector); err != nil {
				f.t.Errorf("install call was not pinned to a numbered block: %s", selector)
				return
			}
			f.contractReads++
			output := f.outputs[hexutil.Encode(message.Data)]
			if output == "" || f.badState {
				output = "0x00"
			}
			responses[index] = map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": output}
		}
		if err := json.NewEncoder(writer).Encode(responses); err != nil {
			f.t.Errorf("encode install batch: %v", err)
		}
		return
	}
	var call fleetHistoryBatchRPCRequest
	if err := json.Unmarshal(wire, &call); err != nil {
		f.t.Errorf("decode install fixture request: %v", err)
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": call.ID}
	switch call.Method {
	case "eth_getBlockByNumber":
		var selector string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
			f.t.Error("invalid install checkpoint request")
			return
		}
		number := f.finalized
		if selector == "finalized" {
			f.finalizedReads++
		} else {
			f.canonicalReads++
			var err error
			number, err = hexutil.DecodeUint64(selector)
			if err != nil {
				f.t.Errorf("invalid install checkpoint selector: %v", err)
				return
			}
		}
		hash := fleetHistoryBatchBlockHash(number)
		if selector != "finalized" && f.noncanonical {
			hash = "0x" + strings.Repeat("f", 64)
		}
		response["result"] = map[string]any{"number": hexutil.EncodeUint64(number), "hash": hash}
	case "eth_getTransactionReceipt":
		f.receiptReads++
		response["error"] = map[string]any{"code": -32000, "message": "receipt archive temporarily unavailable"}
	default:
		f.t.Errorf("unexpected install fixture method=%s", call.Method)
		return
	}
	if err := json.NewEncoder(writer).Encode(response); err != nil {
		f.t.Errorf("encode install fixture response: %v", err)
	}
}

type fleetInstallHistoryCacheFixture struct {
	executor  *Executor
	rpc       *fleetInstallHistoryCacheRPC
	action    Action
	head      ChainHead
	evidence  FleetInstallBatchEvidence
	snapshots []fleetInstallPinnedSnapshot
}

func newFleetInstallHistoryCacheFixture(t *testing.T) fleetInstallHistoryCacheFixture {
	t.Helper()
	f := fleetInstallHistoryCacheFixture{
		executor: historicalAuditCacheTestExecutor(t),
		rpc:      &fleetInstallHistoryCacheRPC{t: t, finalized: 1_000, outputs: map[string]string{}},
		head:     ChainHead{Number: 100, Hash: fleetHistoryBatchBlockHash(100)},
		action: Action{
			ID: "fleet.install.batch.1", Kind: "evm-transaction",
			Target: "0x1234567890123456789012345678901234567890", IntentHash: "0x" + strings.Repeat("5", 64),
			Parameters: map[string]string{"first_fleet": "1", "last_fleet": "10", "generation": "1"},
		},
		evidence: FleetInstallBatchEvidence{
			Schema: fleetInstallBatchEvidenceSchema, Batch: 1, FirstFleet: 1, LastFleet: 10,
			Generation: 1, TransactionHash: "0x" + strings.Repeat("6", 64),
			BlockNumber: 99, BlockHash: fleetHistoryBatchBlockHash(99),
		},
	}
	f.executor.cfg.Config.Topology.HeadFleets = 10
	f.executor.cfg.Config.Topology.ClientsPerHeadFleet = 4
	server := httptest.NewServer(f.rpc)
	t.Cleanup(server.Close)
	client, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	f.executor.cfg.OperationalEVM = server.URL
	f.executor.oracle = &EvmTxManager{client: ethclient.NewClient(client)}
	coordinatorAddress := common.HexToAddress("0x7777777777777777777777777777777777777777")
	f.executor.payloads = &DeploymentPayloads{Manifest: ContractDeployment{CoordinatorProxy: coordinatorAddress}}
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		t.Fatal(err)
	}
	coordinator := stabi.NewSTCoordinator()
	add := func(call []byte, method string, values ...any) {
		output, err := parsed.Methods[method].Outputs.Pack(values...)
		if err != nil {
			t.Fatalf("pack %s fixture result: %v", method, err)
		}
		f.rpc.outputs[hexutil.Encode(call)] = hexutil.Encode(output)
	}
	for fleet := 1; fleet <= 10; fleet++ {
		snapshot := fleetInstallPinnedSnapshot{
			Fleet: fleet, Hotkey: [32]byte{byte(fleet)}, CommitmentHash: [32]byte{byte(fleet + 20)},
			FinalizedBlockHash: [32]byte{byte(fleet + 40)},
			CommitmentEvidence: &FleetCommitmentEvidence{FinalizedBlock: uint64(50 + fleet), CommitmentBlock: uint64(50 + fleet)},
		}
		add(coordinator.PackMirroredCommitments(snapshot.Hotkey), "mirroredCommitments", snapshot.CommitmentHash, snapshot.FinalizedBlockHash, snapshot.CommitmentEvidence.FinalizedBlock)
		for member := 1; member <= 4; member++ {
			binding := protocol.FleetBinding{
				ChainID: f.executor.cfg.ChainID, Netuid: f.executor.cfg.Netuid, Coordinator: [20]byte(coordinatorAddress),
				FleetID: [32]byte{byte(fleet + 60)}, Hotkey: snapshot.Hotkey,
				ClientID: [16]byte{byte(fleet), byte(member)}, ClientKey: [32]byte{byte(fleet), byte(member), 42},
				Generation: 1, ValidFromEpoch: 2, ValidToEpoch: 33, CommitmentHash: snapshot.CommitmentHash,
			}
			memberEvidence := FleetBindingEvidence{Generation: 1, UID: uint16(4*fleet + member)}
			snapshot.Members = append(snapshot.Members, fleetInstallPinnedMember{Evidence: memberEvidence, Binding: binding})
			add(coordinator.PackBindingVersionCount(binding.ClientID), "bindingVersionCount", big.NewInt(1))
			add(coordinator.PackBindingVersionAt(binding.ClientID, new(big.Int)), "bindingVersionAt", stabi.STCoordinatorBindingRecord{
				FleetId: binding.FleetID, Hotkey: binding.Hotkey, ClientKey: binding.ClientKey,
				CommitmentHash: binding.CommitmentHash, Generation: binding.Generation,
				ValidFromEpoch: binding.ValidFromEpoch, ValidToEpoch: binding.ValidToEpoch, Uid: memberEvidence.UID,
			})
			f.evidence.MemberEvidence = append(f.evidence.MemberEvidence, fmt.Sprintf("fleet-%d-member-%d.binding.json", fleet, member))
		}
		f.evidence.CarriedFleets = append(f.evidence.CarriedFleets, fleet)
		f.snapshots = append(f.snapshots, snapshot)
	}
	return f
}

func (f fleetInstallHistoryCacheFixture) reopenedExecutor() *Executor {
	return &Executor{
		cfg: f.executor.cfg, auditAuthorizedConfig: f.executor.auditAuthorizedConfig,
		plan: f.executor.plan, stateDir: f.executor.stateDir,
		oracle: f.executor.oracle, independentEVM: f.executor.independentEVM, payloads: f.executor.payloads,
	}
}

func (f fleetInstallHistoryCacheFixture) verify(ctx context.Context) error {
	return f.reopenedExecutor().verifyFleetInstallPinnedState(ctx, f.action, f.head, f.evidence, f.snapshots)
}

func TestFleetInstallHistoryCacheRetainsNinetyReadsAfterReceiptFailure(t *testing.T) {
	f := newFleetInstallHistoryCacheFixture(t)
	for attempt := 0; attempt < 2; attempt++ {
		if err := f.verify(context.Background()); err != nil {
			t.Fatal(err)
		}
		// Exercise the unchanged receipt boundary after the production pinned
		// helper. Its failure must not erase the independently completed proof.
		if _, err := f.executor.oracle.client.TransactionReceipt(context.Background(), common.HexToHash(f.evidence.TransactionHash)); err == nil {
			t.Fatal("receipt fixture unexpectedly succeeded")
		}
	}
	f.rpc.mu.Lock()
	defer f.rpc.mu.Unlock()
	if f.rpc.contractReads != 90 || f.rpc.contractBatches != 2 || f.rpc.receiptReads != 2 || f.rpc.finalizedReads != 2 || f.rpc.canonicalReads != 2 {
		t.Fatalf("reopened install reads contracts/batches/receipts/finalized/canonical=%d/%d/%d/%d/%d, want90/2/2/2/2", f.rpc.contractReads, f.rpc.contractBatches, f.rpc.receiptReads, f.rpc.finalizedReads, f.rpc.canonicalReads)
	}
}

func TestFleetInstallHistoryCacheWarmProofStillRequiresFreshCanonicalFinality(t *testing.T) {
	for _, failure := range []string{"canonical", "finalized"} {
		t.Run(failure, func(t *testing.T) {
			f := newFleetInstallHistoryCacheFixture(t)
			if err := f.verify(context.Background()); err != nil {
				t.Fatal(err)
			}
			f.rpc.mu.Lock()
			if failure == "canonical" {
				f.rpc.noncanonical = true
			} else {
				f.rpc.finalized = f.head.Number - 1
			}
			f.rpc.mu.Unlock()
			// Historical actionPostState binds this old head in the context.
			// It must not replace the fresh observer read used by the cache gate.
			ctx, err := withFinalizedEVMHead(context.Background(), f.head)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.verify(ctx); err == nil || !strings.Contains(err.Error(), "checkpoint") {
				t.Fatalf("warm %s failure was accepted: %v", failure, err)
			}
			f.rpc.mu.Lock()
			defer f.rpc.mu.Unlock()
			if f.rpc.contractReads != 90 || f.rpc.finalizedReads != 2 || f.rpc.canonicalReads != 2 {
				t.Fatalf("warm refusal skipped fresh checkpoint or repeated state: %d/%d/%d", f.rpc.contractReads, f.rpc.finalizedReads, f.rpc.canonicalReads)
			}
		})
	}
}

func TestFleetInstallHistoryCacheCurrentCheckpointChangeRequiresNewState(t *testing.T) {
	f := newFleetInstallHistoryCacheFixture(t)
	if err := f.verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.head = ChainHead{Number: 101, Hash: fleetHistoryBatchBlockHash(101)}
	f.rpc.mu.Lock()
	f.rpc.badState = true
	f.rpc.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if err := f.verify(context.Background()); err == nil {
			t.Fatal("new current checkpoint inherited old historical state")
		}
	}
	f.rpc.mu.Lock()
	defer f.rpc.mu.Unlock()
	if f.rpc.contractReads != 270 || f.rpc.contractBatches != 6 {
		t.Fatalf("new failed snapshot was cached: reads/batches=%d/%d", f.rpc.contractReads, f.rpc.contractBatches)
	}
}

func TestFleetInstallHistoryCacheBindsEveryDecoderAndActionInput(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*fleetInstallHistoryCacheFixture)
	}{
		{"member-uid", func(f *fleetInstallHistoryCacheFixture) { f.snapshots[0].Members[0].Evidence.UID++ }},
		{"member-binding", func(f *fleetInstallHistoryCacheFixture) { f.snapshots[0].Members[0].Binding.ValidToEpoch++ }},
		{"member-signature", func(f *fleetInstallHistoryCacheFixture) {
			f.snapshots[0].Members[0].Evidence.ClientSignature = "changed"
		}},
		{"mirror-native-block", func(f *fleetInstallHistoryCacheFixture) { f.snapshots[0].CommitmentEvidence.FinalizedBlock++ }},
		{"mirror-hash", func(f *fleetInstallHistoryCacheFixture) { f.snapshots[0].CommitmentHash[0] ^= 1 }},
		{"mirror-block-hash", func(f *fleetInstallHistoryCacheFixture) { f.snapshots[0].FinalizedBlockHash[0] ^= 1 }},
		{"hotkey-calldata", func(f *fleetInstallHistoryCacheFixture) { f.snapshots[0].Hotkey[0] ^= 1 }},
		{"client-calldata", func(f *fleetInstallHistoryCacheFixture) { f.snapshots[0].Members[0].Binding.ClientID[0] ^= 1 }},
		{"batch-evidence", func(f *fleetInstallHistoryCacheFixture) { f.evidence.CalldataHash = "changed" }},
		{"action-intent", func(f *fleetInstallHistoryCacheFixture) { f.action.IntentHash = "changed" }},
		{"contract-target", func(f *fleetInstallHistoryCacheFixture) { f.executor.payloads.Manifest.CoordinatorProxy[0] ^= 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFleetInstallHistoryCacheFixture(t)
			if err := f.verify(context.Background()); err != nil {
				t.Fatal(err)
			}
			test.change(&f)
			f.rpc.mu.Lock()
			f.rpc.badState = true
			f.rpc.mu.Unlock()
			if err := f.verify(context.Background()); err == nil {
				t.Fatal("changed decoder or action input reused an old proof")
			}
			f.rpc.mu.Lock()
			defer f.rpc.mu.Unlock()
			if f.rpc.contractReads != 180 {
				t.Fatalf("changed input did not replay the pinned batch: %d reads", f.rpc.contractReads)
			}
		})
	}
}

func TestFleetInstallHistoryCacheKeepsIndependentObserverProofSeparate(t *testing.T) {
	f := newFleetInstallHistoryCacheFixture(t)
	second := &fleetInstallHistoryCacheRPC{t: t, finalized: 1_000, outputs: f.rpc.outputs}
	server := httptest.NewServer(second)
	defer server.Close()
	client, err := rpc.DialHTTP(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	f.executor.cfg.Public.Chain.EVMPublicReadEndpoint = server.URL
	f.executor.independentEVM = ethclient.NewClient(client)
	if err := f.verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	independent := f.reopenedExecutor()
	independent.oracle = cloneReadManager(f.executor.oracle, f.executor.independentEVM)
	for attempt := 0; attempt < 2; attempt++ {
		if err := independent.verifyFleetInstallPinnedState(context.Background(), f.action, f.head, f.evidence, f.snapshots); err != nil {
			t.Fatal(err)
		}
	}
	second.mu.Lock()
	defer second.mu.Unlock()
	if second.contractReads != 90 || second.finalizedReads != 2 || second.canonicalReads != 2 {
		t.Fatalf("independent reads/contracts/checkpoints=%d/%d/%d, want90/2/2", second.contractReads, second.finalizedReads, second.canonicalReads)
	}
}

func TestFleetInstallHistoryCacheCannotBypassLocalBatchEvidence(t *testing.T) {
	f := newFleetInstallHistoryCacheFixture(t)
	if err := f.verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.executor.stateDir, "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePublicJSON(filepath.Join(f.executor.stateDir, "public", "fleet-install-batch-1.json"), map[string]any{"schema": "changed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.reopenedExecutor().verifyFleetInstallBatchPostState(context.Background(), f.action, f.head, map[string]any{}); err == nil || !strings.Contains(err.Error(), "evidence identity mismatch") {
		t.Fatalf("warm pinned proof bypassed local batch evidence: %v", err)
	}
	f.rpc.mu.Lock()
	defer f.rpc.mu.Unlock()
	if f.rpc.contractReads != 90 || f.rpc.finalizedReads != 1 || f.rpc.canonicalReads != 1 {
		t.Fatal("invalid local batch evidence reached chain reads")
	}
}

func TestFleetInstallHistoryCacheCanceledProofIsNotPublished(t *testing.T) {
	f := newFleetInstallHistoryCacheFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.verify(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled pinned proof error=%v", err)
	}
	if err := f.verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.rpc.mu.Lock()
	defer f.rpc.mu.Unlock()
	if f.rpc.contractReads != 90 {
		t.Fatalf("canceled proof populated cache: historical reads=%d", f.rpc.contractReads)
	}
}
