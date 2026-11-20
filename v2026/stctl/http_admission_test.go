// The operator CLI's direct EVM session shares the same physical response cap.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/evmrpc"
)

func TestEvmHttpSessionRefusesOversizedIdentity(t *testing.T) {
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
	session, err := dialSession(&Config{RpcUrls: []string{server.URL}, ChainId: 945})
	if session != nil {
		session.close()
	}
	if session != nil || !errors.Is(err, evmrpc.ErrResponseLimit) || calls.Load() != 1 {
		t.Fatalf("operator CLI session bypassed response admission: session=%v error=%v calls=%d", session, err, calls.Load())
	}
}
