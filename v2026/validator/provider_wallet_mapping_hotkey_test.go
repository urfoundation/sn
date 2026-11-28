// The hotkey delegation history and the global hotkey consent chain are read
// through their pinned heads from their own endpoints, with the bounds of the
// network consent reader. Signatures are real; identities are synthetic.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
)

var hotkeyHistoryHttpDomain = protocol.ClientKeyHistoryDomain{ChainID: 964, GenesisHash: [32]byte{1}, Netuid: 25, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 6}

// The substrate-context sr25519 signature of a synthetic seed over message.
func hotkeyHistoryHttpSign(t testing.TB, seed byte, message string) [64]byte {
	t.Helper()
	key, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	signature, err := key.ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(message)))
	if err != nil {
		t.Fatal(err)
	}
	return signature.Encode()
}

// The public key of a synthetic seed.
func hotkeyHistoryHttpPublic(t testing.TB, seed byte) [32]byte {
	t.Helper()
	key, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	return key.Public().Encode()
}

// A global consent chain of the hotkey with seed 43, signed by it and the
// coldkey with seed 41. Generation g is effective from epoch 51+10(g-1)
// through 151. Returns the originals and their hashes.
func hotkeyConsentHttpChain(t testing.TB, generations int) ([]protocol.HotkeyWalletMappingConsent, [][32]byte) {
	t.Helper()
	var originals []protocol.HotkeyWalletMappingConsent
	var hashes [][32]byte
	var previous [32]byte
	for index := 0; index < generations; index++ {
		statement := protocol.HotkeyWalletMappingStatement{Schema: protocol.HotkeyWalletMappingConsentSchema, Scope: protocol.HotkeyWalletMappingScope, Subnet: hotkeyHistoryHttpDomain.HotkeySubnet(), Hotkey: hotkeyHistoryHttpPublic(t, 43), Coldkey: hotkeyHistoryHttpPublic(t, 41), Generation: uint64(index + 1), PreviousHash: previous, Nonce: [32]byte{byte(60 + index)}, IssuedAt: 1000, FromEpoch: uint64(51 + 10*index), ThroughEpoch: 151}
		message, err := statement.Message()
		if err != nil {
			t.Fatal(err)
		}
		original := protocol.HotkeyWalletMappingConsent{Message: message, ColdkeySignature: hotkeyHistoryHttpSign(t, 41, message), HotkeySignature: hotkeyHistoryHttpSign(t, 43, message)}
		_, hash, err := protocol.VerifyHotkeyWalletMappingConsent(t.Context(), original)
		if err != nil {
			t.Fatal(err)
		}
		originals, hashes, previous = append(originals, original), append(hashes, hash), hash
	}
	return originals, hashes
}

// A delegation chain of network 9 to the hotkey with seed 43, each generation
// naming consentHead. Generation g is effective from epoch 51+10(g-1) through
// 151, issued after the operator's boundary just before it.
func hotkeyDelegationHttpChain(t testing.TB, generations int, consentHead [32]byte) ([]protocol.WalletMappingConsent, [][32]byte) {
	t.Helper()
	operator, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	var originals []protocol.WalletMappingConsent
	var hashes [][32]byte
	var previous [32]byte
	for index := 0; index < generations; index++ {
		fromEpoch := uint64(51 + 10*index)
		statement := protocol.HotkeyNetworkDelegationStatement{Domain: hotkeyHistoryHttpDomain, UserId: [16]byte{7}, NetworkId: [16]byte{9}, Hotkey: hotkeyHistoryHttpPublic(t, 43), ConsentHeadHash: consentHead, ConsentGeneration: 1, Generation: uint64(index + 1), PreviousHash: previous, Nonce: [32]byte{byte(70 + index)}, IssuedAt: 1000, ExpiresAt: 1300, FromEpoch: fromEpoch, ThroughEpoch: 151}
		if err := protocol.SignProspectiveHotkeyNetworkDelegation(&statement, protocol.ClientKeyEffectiveBoundary{Epoch: fromEpoch - 1, Block: 10 * (fromEpoch - 1), Hash: [32]byte{12}}, operator); err != nil {
			t.Fatal(err)
		}
		message, err := statement.Message()
		if err != nil {
			t.Fatal(err)
		}
		original := protocol.WalletMappingConsent{Message: message, Signature: hotkeyHistoryHttpSign(t, 43, message)}
		_, hash, err := protocol.VerifyHotkeyNetworkDelegation(t.Context(), original)
		if err != nil {
			t.Fatal(err)
		}
		originals, hashes, previous = append(originals, original), append(hashes, hash), hash
	}
	return originals, hashes
}

// The expectation for a delegation chain's head at epoch.
func hotkeyDelegationHttpExpected(head [32]byte, generation uint64, epoch uint64) protocol.HotkeyNetworkDelegationHistoryExpectation {
	return protocol.HotkeyNetworkDelegationHistoryExpectation{Domain: hotkeyHistoryHttpDomain, NetworkId: [16]byte{9}, HeadHash: head, Generation: generation, Epoch: epoch}
}

// The expectation for a global consent chain's head at epoch.
func hotkeyConsentHttpExpected(t testing.TB, head [32]byte, generation uint64, epoch uint64) protocol.HotkeyWalletMappingHistoryExpectation {
	return protocol.HotkeyWalletMappingHistoryExpectation{Subnet: hotkeyHistoryHttpDomain.HotkeySubnet(), Hotkey: hotkeyHistoryHttpPublic(t, 43), HeadHash: head, Generation: generation, Epoch: epoch}
}

// The JSON response body carrying originals.
func hotkeyHistoryHttpBody[T any](t testing.TB, originals []T) []byte {
	t.Helper()
	raw, err := json.Marshal(struct {
		Originals []T `json:"originals"`
	}{Originals: originals})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A server for one history route only. It records every request body and
// counts requests, then answers with respond.
func hotkeyHistoryHttpServer(t testing.TB, path string, bodies chan<- string, calls *atomic.Int32, respond func(w http.ResponseWriter, call int32)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8193))
		if err != nil || r.URL.Path != path || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "synthetic request mismatch", http.StatusBadRequest)
			return
		}
		if bodies != nil {
			bodies <- string(raw)
		}
		respond(w, call)
	}))
}

// A reader of endpoint whose retry wait is refused, so any retry is visible.
func hotkeyHistoryHttpReader(t testing.TB, endpoint *httptest.Server) *HttpWalletMappingReader {
	t.Helper()
	reader, err := NewHttpWalletMappingReader(endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	reader.wait = func(context.Context, time.Duration) error {
		return errors.New("unexpected wallet mapping retry")
	}
	return reader
}

func TestHotkeyDelegationHttpReadsPinnedDelegationHead(t *testing.T) {
	_, consentHashes := hotkeyConsentHttpChain(t, 1)
	delegations, hashes := hotkeyDelegationHttpChain(t, 1, consentHashes[0])
	bodies := make(chan string, 1)
	var calls atomic.Int32
	endpoint := hotkeyHistoryHttpServer(t, "/sn/wallet/hotkey-delegation/history", bodies, &calls, func(w http.ResponseWriter, _ int32) {
		_, _ = w.Write(hotkeyHistoryHttpBody(t, delegations))
	})
	defer endpoint.Close()
	reader := hotkeyHistoryHttpReader(t, endpoint)
	defer reader.CloseIdleConnections()
	expected := hotkeyDelegationHttpExpected(hashes[0], 1, 51)
	originals, verified, err := reader.ReadHotkeyDelegationBounded(t.Context(), expected, 64*1024)
	if err != nil || verified == nil || len(originals) != 1 || originals[0] != delegations[0] || verified.HeadHash != hashes[0] || verified.Statement.ConsentHeadHash != consentHashes[0] || verified.Statement.Hotkey != hotkeyHistoryHttpPublic(t, 43) {
		t.Fatal("delegation history was not read", verified, err)
	}
	decoder := json.NewDecoder(strings.NewReader(<-bodies))
	decoder.DisallowUnknownFields()
	var request struct {
		Domain     protocol.ClientKeyHistoryDomain `json:"domain"`
		NetworkId  string                          `json:"network_id"`
		HeadHash   [32]byte                        `json:"head_hash"`
		Generation uint64                          `json:"generation"`
	}
	if err := decoder.Decode(&request); err != nil || request.Domain != hotkeyHistoryHttpDomain || request.NetworkId != "09000000-0000-0000-0000-000000000000" || request.HeadHash != hashes[0] || request.Generation != 1 {
		t.Fatal("delegation history request differs", request, err)
	}
}

// The chain through a later head still selects the generation effective at
// the epoch, and the request names the hotkey by its bittensor address only.
func TestHotkeyConsentHttpReadsPinnedConsentHead(t *testing.T) {
	consents, hashes := hotkeyConsentHttpChain(t, 2)
	bodies := make(chan string, 2)
	var calls atomic.Int32
	endpoint := hotkeyHistoryHttpServer(t, "/sn/wallet/hotkey-consent/history", bodies, &calls, func(w http.ResponseWriter, _ int32) {
		_, _ = w.Write(hotkeyHistoryHttpBody(t, consents))
	})
	defer endpoint.Close()
	reader := hotkeyHistoryHttpReader(t, endpoint)
	defer reader.CloseIdleConnections()
	for _, c := range []struct {
		epoch      uint64
		generation uint64
	}{{epoch: 61, generation: 2}, {epoch: 55, generation: 1}} {
		originals, verified, err := reader.ReadHotkeyConsentBounded(t.Context(), hotkeyConsentHttpExpected(t, hashes[1], 2, c.epoch), 64*1024)
		if err != nil || verified == nil || len(originals) != 2 || verified.HeadHash != hashes[1] || verified.Generation != 2 || verified.Statement.Generation != c.generation || verified.OriginalHash != hashes[c.generation-1] || verified.Statement.Coldkey != hotkeyHistoryHttpPublic(t, 41) {
			t.Fatal("global consent chain was not read", c.epoch, verified, err)
		}
	}
	address, err := ss58.Encode(hotkeyHistoryHttpPublic(t, 43), ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(<-bodies))
	decoder.DisallowUnknownFields()
	var request struct {
		HotkeySs58 string   `json:"hotkey_ss58"`
		HeadHash   [32]byte `json:"head_hash"`
		Generation uint64   `json:"generation"`
	}
	if err := decoder.Decode(&request); err != nil || request.HotkeySs58 != address || request.HeadHash != hashes[1] || request.Generation != 2 {
		t.Fatal("global consent request differs", request, err)
	}
}

// A verified chain with no consent or delegation at the epoch returns its
// originals with the not-effective outcome, never a mapping.
func TestHotkeyHistoryHttpReturnsNotEffectiveEvidence(t *testing.T) {
	consents, consentHashes := hotkeyConsentHttpChain(t, 1)
	delegations, delegationHashes := hotkeyDelegationHttpChain(t, 1, consentHashes[0])
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/sn/wallet/hotkey-delegation/history":
			_, _ = w.Write(hotkeyHistoryHttpBody(t, delegations))
		case "/sn/wallet/hotkey-consent/history":
			_, _ = w.Write(hotkeyHistoryHttpBody(t, consents))
		default:
			http.Error(w, "synthetic request mismatch", http.StatusBadRequest)
		}
	}))
	defer endpoint.Close()
	reader := hotkeyHistoryHttpReader(t, endpoint)
	defer reader.CloseIdleConnections()
	for _, epoch := range []uint64{50, 152} {
		originals, verified, err := reader.ReadHotkeyDelegationBounded(t.Context(), hotkeyDelegationHttpExpected(delegationHashes[0], 1, epoch), 64*1024)
		if verified != nil || len(originals) != 1 || !errors.Is(err, protocol.ErrWalletMappingNotEffective) || !errors.Is(err, protocol.ErrWalletMappingUnavailable) {
			t.Error("an ineffective delegation was not reported with its evidence", epoch, verified, err)
		}
		consentOriginals, consent, err := reader.ReadHotkeyConsentBounded(t.Context(), hotkeyConsentHttpExpected(t, consentHashes[0], 1, epoch), 64*1024)
		if consent != nil || len(consentOriginals) != 1 || !errors.Is(err, protocol.ErrWalletMappingNotEffective) || !errors.Is(err, protocol.ErrWalletMappingUnavailable) {
			t.Error("an ineffective global consent was not reported with its evidence", epoch, consent, err)
		}
	}
	if calls.Load() != 4 {
		t.Fatal("not-effective evidence was retried", calls.Load())
	}
}

// A missing or malformed selector stays unknown before any request, and an
// exhausted owner performs no external work.
func TestHotkeyHistoryHttpRefusesMissingSelectorsWithoutRequest(t *testing.T) {
	var calls atomic.Int32
	endpoint := hotkeyHistoryHttpServer(t, "/unused", nil, &calls, func(w http.ResponseWriter, _ int32) {})
	defer endpoint.Close()
	reader := hotkeyHistoryHttpReader(t, endpoint)
	defer reader.CloseIdleConnections()
	head := [32]byte{13}
	for index, expected := range []protocol.HotkeyNetworkDelegationHistoryExpectation{
		hotkeyDelegationHttpExpected([32]byte{}, 1, 51),
		hotkeyDelegationHttpExpected(head, 0, 51),
		hotkeyDelegationHttpExpected(head, protocol.MaxWalletMappingHistory+1, 51),
		{Domain: hotkeyHistoryHttpDomain, HeadHash: head, Generation: 1, Epoch: 51},
		{NetworkId: [16]byte{9}, HeadHash: head, Generation: 1, Epoch: 51},
	} {
		if originals, verified, err := reader.ReadHotkeyDelegationBounded(t.Context(), expected, 64*1024); originals != nil || verified != nil || err == nil {
			t.Error("a missing delegation selector was read", index, err)
		}
	}
	otherHotkey := hotkeyConsentHttpExpected(t, head, 1, 51)
	otherHotkey.Hotkey = [32]byte{}
	otherSubnet := hotkeyConsentHttpExpected(t, head, 1, 51)
	otherSubnet.Subnet.Netuid = 0
	for index, expected := range []protocol.HotkeyWalletMappingHistoryExpectation{
		hotkeyConsentHttpExpected(t, [32]byte{}, 1, 51),
		hotkeyConsentHttpExpected(t, head, 0, 51),
		hotkeyConsentHttpExpected(t, head, protocol.MaxWalletMappingHistory+1, 51),
		otherHotkey,
		otherSubnet,
	} {
		if originals, verified, err := reader.ReadHotkeyConsentBounded(t.Context(), expected, 64*1024); originals != nil || verified != nil || err == nil {
			t.Error("a missing global consent selector was read", index, err)
		}
	}
	if originals, verified, err := reader.ReadHotkeyDelegationBounded(t.Context(), hotkeyDelegationHttpExpected(head, 1, 51), 0); originals != nil || verified != nil || !errors.Is(err, protocol.ErrWalletMappingCapacity) {
		t.Fatal("an exhausted owner read a delegation", err)
	}
	if originals, verified, err := reader.ReadHotkeyConsentBounded(t.Context(), hotkeyConsentHttpExpected(t, head, 1, 51), 0); originals != nil || verified != nil || !errors.Is(err, protocol.ErrWalletMappingCapacity) {
		t.Fatal("an exhausted owner read a global consent", err)
	}
	if calls.Load() != 0 {
		t.Fatal("a refused selector performed external work", calls.Load())
	}
}

// The caller's remaining allowance is reserved exactly, with and without a
// declared length, and the response reserve is the network reader's: one
// maximal original per pinned generation plus framing.
func TestHotkeyHistoryHttpReservesNetworkWalletBounds(t *testing.T) {
	consents, consentHashes := hotkeyConsentHttpChain(t, 1)
	delegations, delegationHashes := hotkeyDelegationHttpChain(t, 1, consentHashes[0])
	reserve := protocol.MaxWalletMappingConsentBytes + 1024
	read := func(reader *HttpWalletMappingReader, path string, maximumBytes uint64) error {
		if path == "/sn/wallet/hotkey-delegation/history" {
			originals, verified, err := reader.ReadHotkeyDelegationBounded(t.Context(), hotkeyDelegationHttpExpected(delegationHashes[0], 1, 51), maximumBytes)
			if err == nil && (len(originals) != 1 || verified == nil) || err != nil && (originals != nil || verified != nil) {
				t.Error("a delegation read returned partial evidence", err)
			}
			return err
		}
		originals, verified, err := reader.ReadHotkeyConsentBounded(t.Context(), hotkeyConsentHttpExpected(t, consentHashes[0], 1, 51), maximumBytes)
		if err == nil && (len(originals) != 1 || verified == nil) || err != nil && (originals != nil || verified != nil) {
			t.Error("a global consent read returned partial evidence", err)
		}
		return err
	}
	for _, c := range []struct {
		path string
		raw  []byte
	}{
		{path: "/sn/wallet/hotkey-delegation/history", raw: hotkeyHistoryHttpBody(t, delegations)},
		{path: "/sn/wallet/hotkey-consent/history", raw: hotkeyHistoryHttpBody(t, consents)},
	} {
		for _, chunked := range []bool{false, true} {
			// padding is trailing whitespace, which the strict decoder accepts
			var body atomic.Pointer[[]byte]
			body.Store(&c.raw)
			var calls atomic.Int32
			endpoint := hotkeyHistoryHttpServer(t, c.path, nil, &calls, func(w http.ResponseWriter, _ int32) {
				if chunked {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				_, _ = w.Write(*body.Load())
			})
			reader := hotkeyHistoryHttpReader(t, endpoint)
			if err := read(reader, c.path, uint64(len(c.raw))); err != nil {
				t.Error("an exact original frame was not admitted", c.path, chunked, err)
			}
			if err := read(reader, c.path, uint64(len(c.raw)-1)); !errors.Is(err, protocol.ErrWalletMappingCapacity) {
				t.Error("a remote frame enlarged the remaining allowance", c.path, chunked, err)
			}
			padded := append(bytes.Clone(c.raw), bytes.Repeat([]byte(" "), reserve-len(c.raw))...)
			body.Store(&padded)
			if err := read(reader, c.path, math.MaxUint64); err != nil {
				t.Error("a frame at the per-generation reserve was refused", c.path, chunked, err)
			}
			padded = append(padded, ' ')
			body.Store(&padded)
			if err := read(reader, c.path, math.MaxUint64); !errors.Is(err, protocol.ErrWalletMappingCapacity) {
				t.Error("a frame beyond the per-generation reserve was read", c.path, chunked, err)
			}
			reader.CloseIdleConnections()
			endpoint.Close()
			if calls.Load() != 4 {
				t.Error("a bounded read was retried", c.path, chunked, calls.Load())
			}
		}
	}
}

// A served chain that ends at another head, or that belongs to another
// network, hotkey or subnet, is a contradiction and is not retried.
func TestHotkeyHistoryHttpRefusesMismatchedHeads(t *testing.T) {
	consents, consentHashes := hotkeyConsentHttpChain(t, 1)
	delegations, delegationHashes := hotkeyDelegationHttpChain(t, 1, consentHashes[0])
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/sn/wallet/hotkey-delegation/history" {
			_, _ = w.Write(hotkeyHistoryHttpBody(t, delegations))
			return
		}
		_, _ = w.Write(hotkeyHistoryHttpBody(t, consents))
	}))
	defer endpoint.Close()
	reader := hotkeyHistoryHttpReader(t, endpoint)
	defer reader.CloseIdleConnections()
	otherHead := hotkeyDelegationHttpExpected(delegationHashes[0], 1, 51)
	otherHead.HeadHash[0]++
	otherNetwork := hotkeyDelegationHttpExpected(delegationHashes[0], 1, 51)
	otherNetwork.NetworkId[0]++
	otherDomain := hotkeyDelegationHttpExpected(delegationHashes[0], 1, 51)
	otherDomain.Domain.NoID++
	for index, expected := range []protocol.HotkeyNetworkDelegationHistoryExpectation{otherHead, otherNetwork, otherDomain} {
		if originals, verified, err := reader.ReadHotkeyDelegationBounded(t.Context(), expected, 64*1024); originals != nil || verified != nil || !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
			t.Error("a contradictory delegation chain was admitted", index, err)
		}
	}
	otherConsentHead := hotkeyConsentHttpExpected(t, consentHashes[0], 1, 51)
	otherConsentHead.HeadHash[0]++
	otherHotkey := hotkeyConsentHttpExpected(t, consentHashes[0], 1, 51)
	otherHotkey.Hotkey = hotkeyHistoryHttpPublic(t, 44)
	otherSubnet := hotkeyConsentHttpExpected(t, consentHashes[0], 1, 51)
	otherSubnet.Subnet.Netuid++
	for index, expected := range []protocol.HotkeyWalletMappingHistoryExpectation{otherConsentHead, otherHotkey, otherSubnet} {
		if originals, verified, err := reader.ReadHotkeyConsentBounded(t.Context(), expected, 64*1024); originals != nil || verified != nil || !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
			t.Error("a contradictory global consent chain was admitted", index, err)
		}
	}
	if calls.Load() != 6 {
		t.Fatal("a contradiction was retried", calls.Load())
	}
}

// A chain served without its first generations is unavailable rather than a
// contradiction or an ineffective chain; one longer than its head is refused.
func TestHotkeyHistoryHttpRefusesTruncatedChains(t *testing.T) {
	consents, consentHashes := hotkeyConsentHttpChain(t, 2)
	delegations, delegationHashes := hotkeyDelegationHttpChain(t, 2, consentHashes[0])
	var served atomic.Pointer[[]byte]
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write(*served.Load())
	}))
	defer endpoint.Close()
	reader := hotkeyHistoryHttpReader(t, endpoint)
	defer reader.CloseIdleConnections()
	serve := func(raw []byte) {
		served.Store(&raw)
	}
	for index, delegationOriginals := range [][]protocol.WalletMappingConsent{nil, delegations[:1], delegations[1:]} {
		serve(hotkeyHistoryHttpBody(t, delegationOriginals))
		if originals, verified, err := reader.ReadHotkeyDelegationBounded(t.Context(), hotkeyDelegationHttpExpected(delegationHashes[1], 2, 61), 64*1024); originals != nil || verified != nil || !errors.Is(err, protocol.ErrWalletMappingUnavailable) || errors.Is(err, protocol.ErrWalletMappingNotEffective) || errors.Is(err, protocol.ErrWalletMappingIntegrity) {
			t.Error("a truncated delegation chain was not unavailable", index, err)
		}
	}
	for index, consentOriginals := range [][]protocol.HotkeyWalletMappingConsent{nil, consents[:1], consents[1:]} {
		serve(hotkeyHistoryHttpBody(t, consentOriginals))
		if originals, verified, err := reader.ReadHotkeyConsentBounded(t.Context(), hotkeyConsentHttpExpected(t, consentHashes[1], 2, 61), 64*1024); originals != nil || verified != nil || !errors.Is(err, protocol.ErrWalletMappingUnavailable) || errors.Is(err, protocol.ErrWalletMappingNotEffective) || errors.Is(err, protocol.ErrWalletMappingIntegrity) {
			t.Error("a truncated global consent chain was not unavailable", index, err)
		}
	}
	serve(hotkeyHistoryHttpBody(t, delegations))
	if originals, verified, err := reader.ReadHotkeyDelegationBounded(t.Context(), hotkeyDelegationHttpExpected(delegationHashes[0], 1, 51), 64*1024); originals != nil || verified != nil || !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
		t.Error("a delegation chain beyond its pinned head was admitted", err)
	}
	serve(hotkeyHistoryHttpBody(t, consents))
	if originals, verified, err := reader.ReadHotkeyConsentBounded(t.Context(), hotkeyConsentHttpExpected(t, consentHashes[0], 1, 51), 64*1024); originals != nil || verified != nil || !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
		t.Error("a global consent chain beyond its pinned head was admitted", err)
	}
	if calls.Load() != 8 {
		t.Fatal("a truncated chain was retried", calls.Load())
	}
}

// Each route serves only its own kind in its exact response shape: another
// kind's originals, unknown members and duplicate members are refused.
func TestHotkeyHistoryHttpRefusesOtherKindsAndShapes(t *testing.T) {
	consents, consentHashes := hotkeyConsentHttpChain(t, 1)
	delegations, delegationHashes := hotkeyDelegationHttpChain(t, 1, consentHashes[0])
	network, networkExpected := networkWalletMappingHttpFixture(t)
	var served atomic.Pointer[[]byte]
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(*served.Load())
	}))
	defer endpoint.Close()
	reader := hotkeyHistoryHttpReader(t, endpoint)
	defer reader.CloseIdleConnections()
	serve := func(raw []byte) {
		served.Store(&raw)
	}
	// a network consent where a delegation belongs, and the reverse
	serve(hotkeyHistoryHttpBody(t, []protocol.WalletMappingConsent{network}))
	networkAsDelegation := hotkeyDelegationHttpExpected(networkExpected.HeadHash, 1, 51)
	if _, verified, err := reader.ReadHotkeyDelegationBounded(t.Context(), networkAsDelegation, 64*1024); verified != nil || !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
		t.Error("a network consent was read as a delegation", err)
	}
	serve(hotkeyHistoryHttpBody(t, delegations))
	delegationAsNetwork := networkExpected
	delegationAsNetwork.HeadHash = delegationHashes[0]
	if _, verified, err := reader.ReadNetworkBounded(t.Context(), delegationAsNetwork, 64*1024); verified != nil || !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
		t.Error("a delegation was read as a network consent", err)
	}
	// a delegation where a global consent belongs: its single signature has no
	// place in the global consent's shape
	if _, verified, err := reader.ReadHotkeyConsentBounded(t.Context(), hotkeyConsentHttpExpected(t, delegationHashes[0], 1, 51), 64*1024); verified != nil || !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
		t.Error("a delegation was read as a global consent", err)
	}
	raw := hotkeyHistoryHttpBody(t, consents)
	for index, changed := range [][]byte{
		append([]byte(`{"head":1,`), raw[1:]...),
		append([]byte(`{"originals":[],`), raw[1:]...),
		append(bytes.Clone(raw), []byte(`{}`)...),
	} {
		serve(changed)
		if originals, verified, err := reader.ReadHotkeyConsentBounded(t.Context(), hotkeyConsentHttpExpected(t, consentHashes[0], 1, 51), 64*1024); originals != nil || verified != nil || !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
			t.Error("a changed response shape was admitted", index, err)
		}
	}
}

// A failed attempt retries the identical request under the same owner, with
// the shared pacing.
func TestHotkeyConsentHttpRetriesIdenticalRequest(t *testing.T) {
	consents, hashes := hotkeyConsentHttpChain(t, 1)
	bodies := make(chan string, 2)
	var calls atomic.Int32
	endpoint := hotkeyHistoryHttpServer(t, "/sn/wallet/hotkey-consent/history", bodies, &calls, func(w http.ResponseWriter, call int32) {
		if call == 1 {
			http.Error(w, "synthetic original history unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(hotkeyHistoryHttpBody(t, consents))
	})
	defer endpoint.Close()
	reader := hotkeyHistoryHttpReader(t, endpoint)
	defer reader.CloseIdleConnections()
	waits := 0
	reader.wait = func(ctx context.Context, delay time.Duration) error {
		waits++
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 300*time.Second || delay != 5*time.Second {
			return errors.New("global consent read budget changed")
		}
		return nil
	}
	originals, verified, err := reader.ReadHotkeyConsentBounded(t.Context(), hotkeyConsentHttpExpected(t, hashes[0], 1, 51), 64*1024)
	if err != nil || verified == nil || len(originals) != 1 || waits != 1 || calls.Load() != 2 {
		t.Fatal("global consent read did not recover under its owner", verified, waits, err)
	}
	if first, second := <-bodies, <-bodies; first != second {
		t.Fatal("a retry changed its independently pinned head")
	}
}
