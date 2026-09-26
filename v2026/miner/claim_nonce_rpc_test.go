package miner

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/urnetwork/sdk/v2026"
)

// This fixture uses real ethclient HTTP calls. The first acknowledged boundary
// is either a blocked send or a blocked finality read, selected by the test.
func claimNonceRpcFixture(t *testing.T, blockFinality bool) (string, <-chan *types.Transaction, <-chan struct{}) {
	t.Helper()
	sent := make(chan *types.Transaction, 2)
	blocked := make(chan struct{}, 1)
	var stateLock sync.Mutex
	var latest *types.Transaction
	var firstSend atomic.Bool
	receipt, canonical := claimReceiptIdentityFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var call struct {
			Id     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch call.Method {
		case "eth_chainId":
			result = "0x3b1"
		case "eth_call":
			result = "0x"
		case "eth_estimateGas":
			result = "0x186a0"
		case "eth_getTransactionCount":
			result = "0x17" // deliberately stale after preparation
		case "eth_gasPrice":
			result = "0x1"
		case "eth_sendRawTransaction":
			var encoded string
			if err := json.Unmarshal(call.Params[0], &encoded); err != nil {
				t.Error(err)
				return
			}
			raw, err := hex.DecodeString(strings.TrimPrefix(encoded, "0x"))
			if err != nil {
				t.Error(err)
				return
			}
			tx := new(types.Transaction)
			if err := tx.UnmarshalBinary(raw); err != nil {
				t.Error(err)
				return
			}
			stateLock.Lock()
			latest = tx
			stateLock.Unlock()
			sent <- tx
			if !blockFinality && firstSend.CompareAndSwap(false, true) {
				blocked <- struct{}{}
				<-request.Context().Done()
				return
			}
			if !blockFinality {
				_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "error": map[string]any{"code": -32000, "message": "synthetic broadcast transport failure"}})
				return
			}
			result = tx.Hash().Hex()
		case "eth_getTransactionReceipt":
			stateLock.Lock()
			copyReceipt := *receipt
			copyReceipt.TxHash = latest.Hash()
			stateLock.Unlock()
			result = &copyReceipt
		case "eth_getBlockByNumber":
			if string(call.Params[0]) == `"finalized"` {
				blocked <- struct{}{}
				<-request.Context().Done()
				return
			}
			result = canonical
		default:
			t.Errorf("unexpected nonce RPC %s", call.Method)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": result})
	}))
	t.Cleanup(server.Close)
	return server.URL, sent, blocked
}

func claimNoncePollFixture(t *testing.T, cfg *ClaimDaemonConfig, claim *sdk.SnPoolClaimResult, admission *claimAdmission, owner string) (*ClaimQueue, claimQueuePollHooks) {
	t.Helper()
	store, err := newClaimQueueStore(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: claim.Epoch, Entries: map[string]*ClaimQueueEntry{fmt.Sprint(claim.Epoch): {Epoch: claim.Epoch, Status: "pending"}}}
	hooks := claimPollTestHooks(t, time.Now())
	hooks.latestEpoch = admission.observeEpoch
	hooks.begin = func(ctx context.Context, candidates []claimPollCandidate) (int, context.Context, func(), error) {
		return beginClaimOperation(ctx, admission, owner, candidates)
	}
	hooks.save = store.save
	hooks.reconcile = func(context.Context, *ClaimQueueEntry) (string, error) { return "", nil }
	hooks.submit = func(ctx context.Context, entry *ClaimQueueEntry) error {
		return submitClaimDirect(ctx, cfg, fakeClaimAPI{result: claim}, entry, store, queue, admission)
	}
	return queue, hooks
}

// A durably prepared nonce survives an unacknowledged send. A second member
// signs N+1 despite an RPC still reporting N, and restart reconstructs the floor
// from both files without deleting either signed liability.
func TestClaimNonceFloorPreventsReuseAfterUnacknowledgedSend(t *testing.T) {
	cfg, claim, _, _, _ := signedClaimFixture(t, 70, 23)
	endpoint, sent, blocked := claimNonceRpcFixture(t, false)
	cfg.RPC = []string{endpoint}
	admission := &claimAdmission{}
	first, firstHooks := claimNoncePollFixture(t, cfg, claim, admission, "first")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- pollClaimQueue(ctx, first, firstHooks) }()
	firstTx := <-sent
	<-blocked
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	firstEntry := first.Entries["70"]
	if firstTx.Nonce() != 23 || firstEntry.Status != "uncertain" || firstEntry.TxHash != strings.ToLower(firstTx.Hash().Hex()) || firstEntry.RawTxHex == "" || admission.nonceMinimum() != 24 {
		t.Fatalf("first timeout lost custody: %+v floor=%d", firstEntry, admission.nonceMinimum())
	}
	secondCfg, secondClaim := *cfg, *claim
	secondCfg.StateDir = filepath.Join(t.TempDir(), "claims")
	secondClaim.Epoch = 71
	second, secondHooks := claimNoncePollFixture(t, &secondCfg, &secondClaim, admission, "second")
	if err := pollClaimQueue(context.Background(), second, secondHooks); err != nil {
		t.Fatal(err)
	}
	secondTx := <-sent
	if secondTx.Nonce() != 24 || second.Entries["71"].Status != "uncertain" || admission.nonceMinimum() != 25 {
		t.Fatalf("stale RPC reused nonce: tx=%d entry=%+v floor=%d", secondTx.Nonce(), second.Entries["71"], admission.nonceMinimum())
	}
	restarted := &claimAdmission{}
	if err := restarted.seedMember(cfg); err != nil {
		t.Fatal(err)
	}
	if err := restarted.seedMember(&secondCfg); err != nil {
		t.Fatal(err)
	}
	if restarted.nonceMinimum() != 25 {
		t.Fatalf("restart lost a member's signed nonce: %d", restarted.nonceMinimum())
	}
}

// Real finality RPC cancellation joins the operation and persists exact bytes
// before a different owner acquires. Cancellation never pretends finality.
func TestClaimAdmissionCancelsInFlightFinalityAndRetainsSignedOutcome(t *testing.T) {
	cfg, claim, _, _, _ := signedClaimFixture(t, 70, 23)
	endpoint, sent, blocked := claimNonceRpcFixture(t, true)
	cfg.RPC = []string{endpoint}
	admission := &claimAdmission{}
	queue, hooks := claimNoncePollFixture(t, cfg, claim, admission, "owner")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- pollClaimQueue(ctx, queue, hooks) }()
	tx := <-sent
	<-blocked
	claimTestAcquire(t, admission, "waiting", false, 70)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	entry := queue.Entries["70"]
	if entry.Status != "uncertain" || entry.TxHash != strings.ToLower(tx.Hash().Hex()) || entry.RawTxHex == "" || entry.FinalizedBlock != 0 {
		t.Fatalf("cancelled finality lost signed outcome: %+v", entry)
	}
	claimTestAcquire(t, admission, "waiting", true, 70)
	admission.release("waiting")
	if err := admission.checkChain(big.NewInt(946)); err == nil {
		t.Fatal("different chain reused one nonce domain")
	}
}

func TestClaimQueueRestartPreservesPartialSignedIdentity(t *testing.T) {
	store, err := newClaimQueueStore(filepath.Join(t.TempDir(), "claims"))
	if err != nil {
		t.Fatal(err)
	}
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 3, Entries: map[string]*ClaimQueueEntry{
		"1": {Epoch: 1, Status: "submitting"},
		"2": {Epoch: 2, Status: "submitting", TxHash: common.HexToHash("0x22").Hex()},
		"3": {Epoch: 3, Status: "submitting", RawTxHex: "synthetic retained partial bytes"},
	}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Entries["1"].Status != "retry" || loaded.Entries["2"].Status != "uncertain" || loaded.Entries["3"].Status != "uncertain" {
		t.Fatalf("restart discarded partial signed custody: %+v", loaded.Entries)
	}
}

func TestClaimNonceFloorAdvancesOnlyAfterPreparedCheckpoint(t *testing.T) {
	cfg, claim, _, _, _ := signedClaimFixture(t, 70, 23)
	endpoint, _, _ := claimNonceRpcFixture(t, false)
	cfg.RPC = []string{endpoint}
	admission := &claimAdmission{}
	queue := &ClaimQueue{LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": {Epoch: 70, Status: "submitting"}}}
	// A missing directory forces the actual prepared fsync to fail before send.
	store := &claimQueueStore{path: filepath.Join(t.TempDir(), "missing", "queue.json")}
	err := submitClaimDirect(context.Background(), cfg, fakeClaimAPI{result: claim}, queue.Entries["70"], store, queue, admission)
	if err == nil || admission.nonceMinimum() != 0 || queue.Entries["70"].TxHash != "" || queue.Entries["70"].RawTxHex != "" {
		t.Fatalf("failed checkpoint reserved or leaked signed state: floor=%d err=%v entry=%+v", admission.nonceMinimum(), err, queue.Entries["70"])
	}
	var custody *claimNonceSafetyError
	if errors.As(err, &custody) {
		t.Fatal("ordinary persistence failure was misclassified as identity corruption")
	}
}
