// A containing multi-provider owner reserves bytes before each actual HTTP
// read. Neither Content-Length nor chunked framing can enlarge that reserve.
package validator

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Exact capacity succeeds through real decoding/signature verification, while
// one-byte shortage returns no original or partially verified mapping.
func TestWalletMappingPublicReadReservesExactRemainingBytes(t *testing.T) {
	original, expected := walletMappingHttpFixture(t)
	raw, err := json.Marshal(struct {
		Originals []protocol.WalletMappingConsent `json:"originals"`
	}{Originals: []protocol.WalletMappingConsent{original}})
	if err != nil {
		t.Fatal(err)
	}
	for _, chunked := range []bool{false, true} {
		var calls atomic.Int32
		endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			if chunked {
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
			}
			_, _ = w.Write(raw)
		}))
		reader, err := NewHttpWalletMappingReader(endpoint.URL)
		if err != nil {
			endpoint.Close()
			t.Fatal(err)
		}
		originals, verified, err := reader.ReadBounded(t.Context(), expected, uint64(len(raw)))
		if err != nil || len(originals) != 1 || originals[0] != original || verified == nil || verified.HeadHash != expected.HeadHash || calls.Load() != 1 {
			reader.CloseIdleConnections()
			endpoint.Close()
			t.Fatal("exact original frame was not admitted", chunked, verified, err)
		}
		originals, verified, err = reader.ReadBounded(t.Context(), expected, uint64(len(raw)-1))
		reader.CloseIdleConnections()
		endpoint.Close()
		if originals != nil || verified != nil || !errors.Is(err, protocol.ErrWalletMappingCapacity) || calls.Load() != 2 {
			t.Fatal("remote frame enlarged the original byte reserve", chunked, verified, err)
		}
	}
}

// Exhausted capacity refuses before any network operation; a sibling with its
// own actual allowance still reads the same original without a global hold.
func TestWalletMappingExhaustedOwnerCannotConsumeSiblingCapacity(t *testing.T) {
	original, expected := walletMappingHttpFixture(t)
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(struct {
			Originals []protocol.WalletMappingConsent `json:"originals"`
		}{Originals: []protocol.WalletMappingConsent{original}})
	}))
	defer endpoint.Close()
	reader, err := NewHttpWalletMappingReader(endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.CloseIdleConnections()
	if originals, verified, err := reader.ReadBounded(t.Context(), expected, 0); originals != nil || verified != nil || !errors.Is(err, protocol.ErrWalletMappingCapacity) || calls.Load() != 0 {
		t.Fatal("exhausted original owner performed external work", verified, err)
	}
	if originals, verified, err := reader.Read(t.Context(), expected); err != nil || len(originals) != 1 || verified == nil || calls.Load() != 1 {
		t.Fatal("independent original allowance was held by its sibling", verified, err)
	}
}
