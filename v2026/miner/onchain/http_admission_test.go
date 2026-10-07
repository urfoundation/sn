// The CLI submit/view constructor must retain the common physical HTTP bound.
package onchain

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"

	"github.com/urfoundation/sn/v2026/evmrpc"
)

func TestEvmHttpSubmitDialRefusesOversizedIdentity(t *testing.T) {
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
	defer server.Close()
	client, chain, err := dialOne(t.Context(), server.URL)
	if client != nil {
		client.Close()
	}
	if client != nil || chain != nil || !errors.Is(err, evmrpc.ErrResponseLimit) || calls.Load() != 1 {
		t.Fatalf("submit dial bypassed response admission: client=%v chain=%v error=%v calls=%d", client, chain, err, calls.Load())
	}
}

// A wrapped physical status retains the existing finality owner's retry
// authority. A status is never a reorg or permission to resend signed work.
func TestEvmHttpFinalityPreservesStatusRetryAuthority(t *testing.T) {
	for _, status := range []int{429, 503, 400} {
		var inclusionReads, finalizedReads, sends atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var call struct {
				Id     json.RawMessage   `json:"id"`
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				t.Error(err)
				return
			}
			if call.Method == "eth_sendRawTransaction" {
				sends.Add(1)
			}
			var selector string
			if call.Method != "eth_getBlockByNumber" || len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
				t.Errorf("unexpected finality method: %s", call.Method)
				return
			}
			if selector == "finalized" {
				finalizedReads.Add(1)
			}
			if selector == "0x5a" && inclusionReads.Add(1) == 1 {
				http.Error(writer, "synthetic status body", status)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": finalityClosureTestBlock(selector, 95)})
		}))
		client, err := evmrpc.DialContext(t.Context(), server.URL)
		if err != nil {
			t.Fatal(err)
		}
		err = waitFinalized(t.Context(), client, finalityClosureTestReceipt())
		client.Close()
		server.Close()
		if status == 400 {
			var statusErr rpc.HTTPError
			if !errors.As(err, &statusErr) || statusErr.StatusCode != status || len(statusErr.Body) != 0 || inclusionReads.Load() != 1 || finalizedReads.Load() != 1 {
				t.Fatalf("hard status acquired retry authority: %v inclusion=%d finalized=%d", err, inclusionReads.Load(), finalizedReads.Load())
			}
		} else if err != nil || inclusionReads.Load() != 3 || finalizedReads.Load() != 3 {
			t.Fatalf("status %d lost closing retry: %v inclusion=%d finalized=%d", status, err, inclusionReads.Load(), finalizedReads.Load())
		}
		if sends.Load() != 0 {
			t.Fatal("finality status triggered a send")
		}
	}
}
