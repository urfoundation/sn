// The network consent history is read through the pinned network head from
// its own endpoint, with real operator and coldkey signatures over synthetic
// identities.
package validator

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/protocol"
)

// A network consent effective for epochs 51 through 151 and its expectation.
func networkWalletMappingHttpFixture(t testing.TB) (protocol.WalletMappingConsent, protocol.NetworkWalletMappingHistoryExpectation) {
	t.Helper()
	coldkey, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{40})
	if err != nil {
		t.Fatal(err)
	}
	operator, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	statement := protocol.NetworkWalletMappingStatement{Domain: protocol.ClientKeyHistoryDomain{ChainID: 964, GenesisHash: [32]byte{1}, Netuid: 25, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 6}, UserId: [16]byte{7}, NetworkId: [16]byte{9}, Coldkey: coldkey.Public().Encode(), Generation: 1, Nonce: [32]byte{10}, IssuedAt: 1000, ExpiresAt: 1300, FromEpoch: 51, ThroughEpoch: 151}
	if err := protocol.SignProspectiveNetworkWalletMapping(&statement, protocol.ClientKeyEffectiveBoundary{Epoch: 50, Block: 500, Hash: [32]byte{12}}, operator); err != nil {
		t.Fatal(err)
	}
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := coldkey.ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(message)))
	if err != nil {
		t.Fatal(err)
	}
	original := protocol.WalletMappingConsent{Message: message, Signature: signature.Encode()}
	_, hash, err := protocol.VerifyNetworkWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, protocol.NetworkWalletMappingHistoryExpectation{Domain: statement.Domain, NetworkId: statement.NetworkId, HeadHash: hash, Generation: 1, Epoch: 51}
}

// A server for the network history endpoint only, recording request bodies.
func networkWalletMappingHttpServer(t testing.TB, original protocol.WalletMappingConsent, bodies chan<- string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8193))
		if err != nil || r.URL.Path != "/sn/wallet/network-consent/history" || r.Method != http.MethodPost {
			http.Error(w, "synthetic request mismatch", http.StatusBadRequest)
			return
		}
		bodies <- string(raw)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Originals []protocol.WalletMappingConsent `json:"originals"`
		}{Originals: []protocol.WalletMappingConsent{original}})
	}))
}

func TestNetworkWalletMappingHttpReadsPinnedNetworkHead(t *testing.T) {
	original, expected := networkWalletMappingHttpFixture(t)
	bodies := make(chan string, 1)
	endpoint := networkWalletMappingHttpServer(t, original, bodies)
	defer endpoint.Close()
	reader, err := NewHttpWalletMappingReader(endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	originals, verified, err := reader.ReadNetworkBounded(t.Context(), expected, 64*1024)
	if err != nil || verified == nil || len(originals) != 1 || verified.HeadHash != expected.HeadHash {
		t.Fatal("network history was not read", verified, err)
	}
	var request struct {
		NetworkId  string   `json:"network_id"`
		HeadHash   [32]byte `json:"head_hash"`
		Generation uint64   `json:"generation"`
	}
	if err := json.Unmarshal([]byte(<-bodies), &request); err != nil || request.NetworkId != "09000000-0000-0000-0000-000000000000" || request.HeadHash != expected.HeadHash || request.Generation != 1 {
		t.Fatal("network history request differs", request, err)
	}
}

// A verified network chain with no consent at the epoch returns its originals
// with the not-effective outcome, never a mapping.
func TestNetworkWalletMappingHttpReturnsNotEffectiveEvidence(t *testing.T) {
	original, expected := networkWalletMappingHttpFixture(t)
	bodies := make(chan string, 1)
	endpoint := networkWalletMappingHttpServer(t, original, bodies)
	defer endpoint.Close()
	reader, err := NewHttpWalletMappingReader(endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	expected.Epoch = 152
	originals, verified, err := reader.ReadNetworkBounded(t.Context(), expected, 64*1024)
	if verified != nil || len(originals) != 1 || !errors.Is(err, protocol.ErrWalletMappingNotEffective) {
		t.Fatal("ineffective network consent was not reported with its evidence", verified, err)
	}
	expected.NetworkId = [16]byte{}
	if _, _, err := reader.ReadNetworkBounded(t.Context(), expected, 64*1024); !errors.Is(err, protocol.ErrWalletMappingUnavailable) || errors.Is(err, protocol.ErrWalletMappingNotEffective) {
		t.Fatal("a missing network identity was read", err)
	}
}
