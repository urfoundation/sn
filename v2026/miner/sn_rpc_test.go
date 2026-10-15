// Exercise the fleet/claim read transport's actual HTTP response boundary.
package miner

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A chain identity is usable only when its complete response names the exact
// request and protocol. Valid result bytes cannot repair a foreign envelope.
func TestEthRpcRejectsForeignResponseEnvelope(t *testing.T) {
	for _, raw := range []string{
		`{"jsonrpc":"2.0","id":2,"result":"0x3b1"}`,
		`{"jsonrpc":"1.0","id":1,"result":"0x3b1"}`,
		`{"id":1,"result":"0x3b1"}`,
		`{"jsonrpc":"2.0","result":"0x3b1"}`,
		`{"jsonrpc":"2.0","id":"1","result":"0x3b1"}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte(raw)) }))
		value, err := ethRpcHexResult(t.Context(), server.URL, "eth_chainId", []any{})
		server.Close()
		if err == nil || value != "" {
			t.Fatalf("foreign response envelope admitted: response=%s value=%q error=%v", raw, value, err)
		}
	}
}

// Limiting a read to exactly the ceiling hides bytes after a valid JSON
// object. The original reader accepted this oversized, otherwise valid reply.
func TestEthRpcRejectsOversizedCompleteReply(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			if chunked {
				writer.WriteHeader(http.StatusOK)
				writer.(http.Flusher).Flush()
			}
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`))
			_, _ = writer.Write(bytes.Repeat([]byte(" "), 1024*1024))
		}))
		value, err := ethRpcHexResult(t.Context(), server.URL, "eth_chainId", []any{})
		server.Close()
		if err == nil || value != "" {
			t.Fatalf("oversized complete reply admitted: chunked=%t value=%q error=%v", chunked, value, err)
		}
	}
}

// Missing prefixes and quantity padding cannot stand in for canonical RPC
// values. Empty data is valid; an empty quantity is not.
func TestEthRpcHexParsersRequireCanonicalWireForms(t *testing.T) {
	for _, raw := range []string{"", "1", "0X1", "0x", "0x01", "0x+1", "0x10000000000000000"} {
		if _, err := parseEthHexQuantity(raw); err == nil {
			t.Fatalf("malformed quantity accepted: %q", raw)
		}
	}
	for _, raw := range []string{"", "01", "0X01", "0x1", "0xgg"} {
		if _, err := parseEthHexBytes(raw); err == nil {
			t.Fatalf("malformed data accepted: %q", raw)
		}
	}
	if value, err := parseEthHexQuantity("0x3b1"); err != nil || value != 945 {
		t.Fatalf("valid quantity refused: %d %v", value, err)
	}
	if value, err := parseEthHexQuantity("0x0"); err != nil || value != 0 {
		t.Fatalf("zero quantity refused: %d %v", value, err)
	}
	if value, err := parseEthHexBytes("0x"); err != nil || len(value) != 0 {
		t.Fatalf("empty data refused: %x %v", value, err)
	}
	if value, err := parseEthHexBytes("0x01aB"); err != nil || !bytes.Equal(value, []byte{1, 0xab}) {
		t.Fatalf("valid data refused: %x %v", value, err)
	}
}

// Complete valid responses remain usable at the exact byte ceiling.
func TestEthRpcAdmitsExactLimit(t *testing.T) {
	raw := `{"jsonrpc":"2.0","id":1,"result":"0x3b1"}`
	raw += strings.Repeat(" ", (1024*1024)-len(raw))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte(raw)) }))
	defer server.Close()
	if value, err := ethRpcHexResult(t.Context(), server.URL, "eth_chainId", []any{}); err != nil || value != "0x3b1" {
		t.Fatalf("valid exact-limit response refused: value=%q error=%v", value, err)
	}
}
