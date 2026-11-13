// Real HTTP responses and real coldkey signatures test acquisition ownership.
// Only the retry wait is accelerated; no callback supplies a trusted mapping.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The selected expected digest is computed from original signed bytes before
// the HTTP source exists; it never comes from a response-selected newest row.
func walletMappingHttpFixture(t testing.TB) (protocol.WalletMappingConsent, protocol.WalletMappingHistoryExpectation) {
	t.Helper()
	key, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{40})
	if err != nil {
		t.Fatal(err)
	}
	statement := protocol.WalletMappingStatement{Schema: protocol.WalletMappingConsentSchema, Domain: protocol.ClientKeyHistoryDomain{ChainID: 964, GenesisHash: [32]byte{1}, Netuid: 25, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 6}, UserId: [16]byte{7}, ClientId: [16]byte{8}, NetworkId: [16]byte{9}, Coldkey: key.Public().Encode(), Generation: 1, Nonce: [32]byte{10}, IssuedAt: 1000, ExpiresAt: 1300, FromEpoch: 0, ThroughEpoch: 100}
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := key.ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(message)))
	if err != nil {
		t.Fatal(err)
	}
	original := protocol.WalletMappingConsent{Message: message, Signature: signature.Encode()}
	_, hash, err := protocol.VerifyWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, protocol.WalletMappingHistoryExpectation{Domain: statement.Domain, ClientId: statement.ClientId, HeadHash: hash, Generation: 1, Epoch: 0}
}

// A failed first response retries the same original request inside a single
// default300s owner with per-attempt60s and no authority learned from transport.
func TestWalletMappingHttpRecoveryKeepsOriginalHeadAndBudget(t *testing.T) {
	original, expected := walletMappingHttpFixture(t)
	var calls atomic.Int32
	requests := make(chan string, 2)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8193))
		if err != nil || r.URL.Path != "/sn/wallet/consent/history" || r.Method != http.MethodPost {
			http.Error(w, "synthetic request mismatch", http.StatusBadRequest)
			return
		}
		requests <- string(raw)
		if calls.Add(1) == 1 {
			http.Error(w, "synthetic original history unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Originals []protocol.WalletMappingConsent `json:"originals"`
		}{Originals: []protocol.WalletMappingConsent{original}})
	}))
	defer endpoint.Close()
	reader, err := NewHttpWalletMappingReader(endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	waits := 0
	reader.wait = func(ctx context.Context, delay time.Duration) error {
		waits++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 300*time.Second || delay != 5*time.Second || reader.client.Timeout != 60*time.Second {
			return errors.New("original wallet mapping read budget changed")
		}
		return nil
	}
	originals, value, err := reader.Read(t.Context(), expected)
	if err != nil || value == nil || value.HeadHash != expected.HeadHash || len(originals) != 1 || originals[0] != original || waits != 1 || calls.Load() != 2 {
		t.Fatal("original wallet history did not recover under its read owner", value, waits, err)
	}
	if first, second := <-requests, <-requests; first != second {
		t.Fatal("read retry changed its independent original head")
	}
}

// A missing expected head creates no request. A complete contradictory source
// is a hard refusal, and cancellation joins the same owner without partial data.
func TestWalletMappingHttpMissingContradictoryAndCanceledRemainDistinct(t *testing.T) {
	original, expected := walletMappingHttpFixture(t)
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Originals []protocol.WalletMappingConsent `json:"originals"`
		}{Originals: []protocol.WalletMappingConsent{original}})
	}))
	defer endpoint.Close()
	reader, err := NewHttpWalletMappingReader(endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	absent := expected
	absent.HeadHash = [32]byte{}
	if raws, value, err := reader.Read(t.Context(), absent); raws != nil || value != nil || !errors.Is(err, protocol.ErrWalletMappingUnavailable) || calls.Load() != 0 {
		t.Fatal("missing original head gained transport authority", value, err)
	}
	foreign := expected
	foreign.HeadHash[0]++
	if raws, value, err := reader.Read(t.Context(), foreign); raws != nil || value != nil || !errors.Is(err, protocol.ErrWalletMappingIntegrity) || calls.Load() != 1 {
		t.Fatal("contradictory original head was retried or accepted", value, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if raws, value, err := reader.Read(ctx, expected); raws != nil || value != nil || !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatal("canceled owner acquired fresh original evidence", value, err)
	}
}
