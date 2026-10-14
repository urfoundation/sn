// Original acquisition authenticates scope before exposing unresolved prior
// references. It does not turn a raw companion into complete economic work.
package validator

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
)

// A correctly signed roster may be acquired before its absent original cuts;
// the ordinary full reader must still refuse to publish a verified result.
func TestWholeWorkOriginalReadDoesNotAuthorizeMissingCuts(t *testing.T) {
	artifact, inventory, expected := wholeWorkReaderFixture(t)
	authority, err := payoutartifact.DecodeWholeWorkAuthority(t.Context(), inventory.Authority, expected.AuthoritySigner)
	if err != nil {
		t.Fatal(err)
	}
	authority.Owners = []payoutartifact.WholeWorkOwner{{ClientId: [16]byte{1}, NetworkId: [16]byte{2}, Generation: [16]byte{3}, PublicKey: [32]byte{4}}}
	key, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := payoutartifact.SignWholeWorkAuthority(t.Context(), authority, key)
	if err != nil {
		t.Fatal(err)
	}
	inventory.Authority, err = signed.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(inventory)
	}))
	defer endpoint.Close()
	reader, err := NewHttpWholeWorkInventoryReader(endpoint.URL, artifact.DeploymentID, artifact.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.CloseIdleConnections()
	before, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	original, err := reader.ReadOriginal(t.Context(), artifact, expected)
	if err != nil || original == nil || len(original.Owners) != 0 || calls.Load() != 1 {
		t.Fatal("independent original scope could not be acquired", original, err)
	}
	if raw, verified, err := reader.Read(t.Context(), artifact, expected); raw != nil || verified != nil || !errors.Is(err, payoutartifact.ErrClosedWorkUnavailable) || calls.Load() != 2 {
		t.Fatal("raw acquisition authorized missing original cuts", raw, verified, err)
	}
	after, err := json.Marshal(artifact)
	if err != nil || string(before) != string(after) {
		t.Fatal("original acquisition changed signed payout bytes", err)
	}
}

// Even the raw phase checks the independent signer and exact artifact window;
// a caller cannot follow a foreign authority's proposed history references.
func TestWholeWorkOriginalReadRefusesForeignSignedWindow(t *testing.T) {
	artifact, inventory, expected := wholeWorkReaderFixture(t)
	authority, err := payoutartifact.DecodeWholeWorkAuthority(t.Context(), inventory.Authority, expected.AuthoritySigner)
	if err != nil {
		t.Fatal(err)
	}
	authority.Epoch++
	key, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := payoutartifact.SignWholeWorkAuthority(t.Context(), authority, key)
	if err != nil {
		t.Fatal(err)
	}
	inventory.Authority, err = signed.Bytes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(inventory) }))
	defer endpoint.Close()
	reader, err := NewHttpWholeWorkInventoryReader(endpoint.URL, artifact.DeploymentID, artifact.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.CloseIdleConnections()
	if original, err := reader.ReadOriginal(t.Context(), artifact, expected); original != nil || !errors.Is(err, payoutartifact.ErrClosedWorkIntegrity) {
		t.Fatal("foreign signed window escaped original acquisition", original, err)
	}
}
