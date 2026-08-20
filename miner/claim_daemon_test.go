package miner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/urnetwork/sdk"
	"gopkg.in/yaml.v3"
)

func TestClaimDaemonConfigStrictAndPortable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "relay.key"), []byte("01"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := ClaimDaemonConfig{SchemaVersion: 1, Release: "1.0", APIURL: "http://operator", RPC: []string{"http://rpc"}, KeyFile: "relay.key", StateDir: "state", PollSeconds: 5, LookbackEpochs: 3}
	b, _ := yaml.Marshal(cfg)
	path := filepath.Join(dir, "claim.yml")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadClaimDaemonConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.KeyFile != filepath.Join(dir, "relay.key") || got.StateDir != filepath.Join(dir, "state") {
		t.Fatalf("relative paths were not anchored to config: %+v", got)
	}
	b = append(b, []byte("unknown: true\n")...)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClaimDaemonConfig(path); err == nil {
		t.Fatal("unknown claim daemon config field accepted")
	}
}

func TestClaimQueueDiscoveryAndCrashRecoveryBoundary(t *testing.T) {
	store, err := newClaimQueueStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	queue, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	discoverClaims(queue, 10, 2)
	if queue.LastDiscovered != 9 || queue.Entries["8"] == nil || queue.Entries["9"] == nil || queue.Entries["7"] != nil {
		t.Fatalf("lookback discovery = %+v", queue)
	}
	queue.Entries["8"].Status = "submitting"
	queue.Entries["9"].Status = "submitting"
	queue.Entries["9"].TxHash = "0x" + fmt.Sprintf("%064x", 9)
	queue.Entries["9"].RawTxHex = "0x01"
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	restarted, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Entries["8"].Status != "retry" || restarted.Entries["9"].Status != "uncertain" {
		t.Fatalf("restart statuses = %s, %s", restarted.Entries["8"].Status, restarted.Entries["9"].Status)
	}
	discoverClaims(restarted, 12, 2)
	if restarted.Entries["10"] == nil || restarted.Entries["11"] == nil {
		t.Fatal("new epochs were not discovered incrementally")
	}
}

func TestClaimTxHashOnlyAcceptsCompleteHash(t *testing.T) {
	hash := "0x" + fmt.Sprintf("%064x", 123)
	if got := claimTxHash("sent: tx " + hash + " (nonce 1)"); got != hash {
		t.Fatalf("hash = %q", got)
	}
	if got := claimTxHash("sent: tx 0x" + string(make([]byte, 64))); got != "" {
		t.Fatalf("non-hex tx hash accepted: %q", got)
	}
	if got := claimTxHash("sent: tx 0x1234\n"); got != "" {
		t.Fatalf("short tx hash accepted: %q", got)
	}
}

func TestBoundedClaimOutputPersistsHashAcrossWrites(t *testing.T) {
	hash := "0x" + fmt.Sprintf("%064x", 456)
	var got string
	w := &boundedClaimOutput{onTxHash: func(value string) { got = value }}
	_, _ = w.Write([]byte("sent: "))
	_, _ = w.Write([]byte("tx " + hash[:30]))
	_, _ = w.Write([]byte(hash[30:] + "\n"))
	if got != hash {
		t.Fatalf("persisted hash = %q, want %q", got, hash)
	}
}

func TestPreparedClaimRecordContainsExactRLP(t *testing.T) {
	to := common.HexToAddress("0x1234")
	tx := ethTypes.NewTx(&ethTypes.LegacyTx{Nonce: 3, GasPrice: big.NewInt(1), Gas: 21_000, To: &to, Value: big.NewInt(0)})
	raw, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf("prepared: tx %s raw 0x%x\n", tx.Hash(), raw)
	hash, encoded := claimPreparedTx(line)
	if hash != strings.ToLower(tx.Hash().Hex()) || encoded != "0x"+fmt.Sprintf("%x", raw) {
		t.Fatalf("prepared record = %q %q", hash, encoded)
	}
	writer := &boundedClaimOutput{onPrepared: func(string, string) error { return errors.New("fsync failed") }}
	if _, err := writer.Write([]byte(line)); err == nil {
		t.Fatal("prepared transaction writer ignored durable callback failure")
	}
}

type fakeClaimAPI struct {
	result *sdk.SnPoolClaimResult
	err    error
}

func (f fakeClaimAPI) SnPoolClaimSyncWithContext(context.Context, *sdk.SnPoolClaimArgs) (*sdk.SnPoolClaimResult, error) {
	return f.result, f.err
}

func TestReconcileClaimEntryUsesFinalizedLeafClaimed(t *testing.T) {
	claimed := true
	ethCalls := 0
	root := bytes.Repeat([]byte{0x11}, 32)
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch request.Method {
		case "eth_chainId":
			result = "0x3b1"
		case "chain_getFinalizedHead":
			result = "0x" + fmt.Sprintf("%064x", 9)
		case "chain_getHeader":
			result = map[string]any{"number": "0x10"}
		case "eth_call":
			ethCalls++
			if ethCalls%2 == 1 {
				result = "0x" + fmt.Sprintf("%x", root) + strings.Repeat(fmt.Sprintf("%064x", 0), 5) + fmt.Sprintf("%064x", 2)
			} else {
				word := 0
				if claimed {
					word = 1
				}
				result = "0x" + fmt.Sprintf("%064x", word)
			}
		default:
			t.Errorf("unexpected rpc method %s", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer rpc.Close()
	claim := &sdk.SnPoolClaimResult{Epoch: 7, NoId: []byte{1}, Coldkey: make([]byte, 32), PayoutRoot: root, ChainId: 945, ContractAddress: "0x0000000000000000000000000000000000001234", SettlementVaultAddress: "0x0000000000000000000000000000000000001234"}
	cfg := &ClaimDaemonConfig{RPC: []string{rpc.URL}}
	entry := &ClaimQueueEntry{Epoch: 7, Status: "pending"}
	status, err := reconcileClaimEntry(context.Background(), cfg, fakeClaimAPI{result: claim}, entry)
	if err != nil || status != "finalized" {
		t.Fatalf("claimed reconciliation = %q, %v", status, err)
	}
	claimed = false
	status, err = reconcileClaimEntry(context.Background(), cfg, fakeClaimAPI{result: claim}, entry)
	if err != nil || status != "" {
		t.Fatalf("unclaimed reconciliation = %q, %v", status, err)
	}
	status, err = reconcileClaimEntry(context.Background(), cfg, fakeClaimAPI{result: &sdk.SnPoolClaimResult{Epoch: 7}}, entry)
	if err != nil || status != "no-claim" {
		t.Fatalf("zero payout reconciliation = %q, %v", status, err)
	}
}
