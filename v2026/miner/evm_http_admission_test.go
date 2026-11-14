// All miner dial owners must use the same physical response admission. The
// actual claim queue additionally retains a signed intent across a bad reply.
package miner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/evmrpc"
)

func TestEvmHttpMinerDialPathsRefuseOversizedIdentity(t *testing.T) {
	for _, owner := range []string{"fleet-current", "fleet-recovery", "claim-state", "claim-replay", "claim-receipt"} {
		cfg, claim, entry, _, _ := signedClaimFixture(t, 70, 23)
		tx, _, from, err := authenticateSignedClaim(cfg, entry)
		if err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			calls.Add(1)
			var call struct {
				Id json.RawMessage `json:"id"`
			}
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				t.Error(err)
				return
			}
			writer.Header().Set("Content-Length", fmt.Sprint(40*1024*1024))
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": "0x3b1"})
		}))
		cfg.RPC = []string{server.URL}
		authority := &fleetMainnetRuntimeAuthority{EvmChainId: 945, GenesisHash: "0x" + strings.Repeat("a1", 32)}
		switch owner {
		case "fleet-current":
			client, _, callErr := authority.dialEvm(t.Context(), cfg.RPC)
			err = callErr
			if client != nil {
				client.Close()
			}
		case "fleet-recovery":
			client, _, callErr := fleetRecoveryDialEvm(t.Context(), cfg.RPC, authority)
			err = callErr
			if client != nil {
				client.Close()
			}
		case "claim-state":
			_, err = queryClaimedFinalized(t.Context(), cfg, claim)
		case "claim-replay":
			_, err = rebroadcastSignedClaimTest(t, t.Context(), cfg, tx, from)
		case "claim-receipt":
			_, err = finalizedClaimReceipt(t.Context(), cfg, entry.TxHash, tx.ChainId())
		}
		server.Close()
		if err == nil || calls.Load() != 1 {
			t.Errorf("%s advanced beyond an oversized chain identity: calls=%d error=%v", owner, calls.Load(), err)
		}
		// Replay's existing identity diagnostic intentionally hides its cause;
		// the other paths retain the bound either by wrapping or formatting it.
		if owner != "claim-replay" && !strings.Contains(err.Error(), evmrpc.ErrResponseLimit.Error()) {
			t.Errorf("%s bypassed physical response admission: %v", owner, err)
		}
	}
}

// Forward only synthetic fixture traffic, replacing one selected response.
// The proxy joins and closes its upstream body before replying.
func claimHttpTestProxy(t *testing.T, upstream string, intercept func(http.ResponseWriter, string, json.RawMessage) bool) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		encoded, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var call struct {
			Method string          `json:"method"`
			Id     json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(encoded, &call); err != nil {
			t.Error(err)
			return
		}
		if intercept(writer, call.Method, call.Id) {
			return
		}
		forward, err := http.NewRequestWithContext(request.Context(), http.MethodPost, upstream, bytes.NewReader(encoded))
		if err != nil {
			t.Error(err)
			return
		}
		response, err := http.DefaultClient.Do(forward)
		if err != nil {
			t.Error(err)
			return
		}
		defer response.Body.Close()
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(response.StatusCode)
		_, _ = io.Copy(writer, response.Body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestEvmHttpClaimRecoveryRetainsSignedReceiptAcrossAdmissionFailure(t *testing.T) {
	fixture := newClaimClockTestRPC(t, func(fixture *claimClockTestRPC) { fixture.finalized = 95 })
	originalEndpoint := fixture.cfg.RPC[0]
	var refuse atomic.Bool
	refuse.Store(true)
	fixture.cfg.RPC = []string{claimHttpTestProxy(t, originalEndpoint, func(writer http.ResponseWriter, method string, id json.RawMessage) bool {
		if method != "eth_getTransactionReceipt" || !refuse.Load() {
			return false
		}
		writer.Header().Set("Content-Length", fmt.Sprint(40*1024*1024))
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": fixture.receipt})
		return true
	})}
	store := newClaimQueueTestStore(t, fixture.cfg.StateDir)
	queue := &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: 70, Entries: map[string]*ClaimQueueEntry{"70": fixture.entry}}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	before := *fixture.entry
	status, err := reconcileSignedClaim(t.Context(), fixture.cfg, fixture.entry, store)
	if err == nil || status == "finalized" || !errors.Is(err, evmrpc.ErrResponseLimit) || fixture.entry.RawTxHex != before.RawTxHex || fixture.entry.TxHash != before.TxHash || fixture.entry.FinalizedBlock != 0 {
		t.Fatalf("bad HTTP receipt altered signed custody: status=%s error=%v entry=%+v", status, err, fixture.entry)
	}
	if err := store.save(queue); err != nil {
		t.Fatal(err)
	}
	retained, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	refuse.Store(false)
	status, err = reconcileSignedClaim(t.Context(), fixture.cfg, retained.Entries["70"], store)
	_, _, _, sends := fixture.evidence()
	if err != nil || status != "finalized" || retained.Entries["70"].FinalizedBlock != 90 || retained.Entries["70"].RawTxHex != before.RawTxHex || retained.Entries["70"].TxHash != before.TxHash || len(sends) != 1 || sends[0] != before.RawTxHex {
		t.Fatalf("restart replaced the authorized exact replay or sent again: status=%s error=%v sends=%v", status, err, sends)
	}
}

func TestEvmHttpClaimReplayStatusCannotMasqueradeAsKnownTransaction(t *testing.T) {
	fixture := newClaimClockTestRPC(t, nil)
	var sendCalls atomic.Int32
	fixture.cfg.RPC = []string{claimHttpTestProxy(t, fixture.cfg.RPC[0], func(writer http.ResponseWriter, method string, _ json.RawMessage) bool {
		if method != "eth_sendRawTransaction" {
			return false
		}
		sendCalls.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(writer, "already known synthetic status-body diagnostic")
		return true
	})}
	tx, _, from, err := authenticateSignedClaim(fixture.cfg, fixture.entry)
	if err != nil {
		t.Fatal(err)
	}
	before := *fixture.entry
	consumed, err := rebroadcastSignedClaimTest(t, t.Context(), fixture.cfg, tx, from)
	if err == nil || consumed || knownClaimTransaction(err) || sendCalls.Load() != 1 || *fixture.entry != before {
		t.Fatalf("status body acknowledged or retried signed send: consumed=%t error=%v calls=%d", consumed, err, sendCalls.Load())
	}
}
