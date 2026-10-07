package hotkeywallet

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urnetwork/connect/v2026"
)

// The operator boundary every synthetic challenge is issued at.
var testBoundary = protocol.ClientKeyEffectiveBoundary{Epoch: 50, Block: 500, Hash: [32]byte{12}}

// When every synthetic challenge is issued; it expires 300 seconds later.
const testIssuedAt = 1791244800

var testNetworkId = connect.Id{9}
var testUserId = connect.Id{7}

func testOperatorKey(t testing.TB) (*ecdsa.PrivateKey, common.Address) {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	return key, crypto.PubkeyToAddress(key.PublicKey)
}

func testJwt(t testing.TB, claims gojwt.MapClaims) string {
	t.Helper()
	token, err := gojwt.NewWithClaims(gojwt.SigningMethodNone, claims).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// The synthetic network owner's network JWT.
func testNetworkJwt(t testing.TB) string {
	return testJwt(t, gojwt.MapClaims{"network_id": testNetworkId.String(), "user_id": testUserId.String(), "network_name": "synthetic-network"})
}

func testAddress(key [32]byte) string {
	address, _ := ss58.Encode(key, ss58.BittensorPrefix)
	return address
}

// Serves the hotkey wallet routes as the operator does. It verifies and stores
// each hotkey's chain, issues delegation challenges with a boundary signed by
// a synthetic operator key, and accepts a delegation only with the hotkey's
// signature over a challenge it issued. Requests decode with unknown fields
// refused, so a client that adds, renames or misencodes a field fails here.
// Configuration is set before the server starts; counters are read through
// counts.
type testOperator struct {
	byJwt  string
	signer *ecdsa.PrivateKey
	// new generations one request may add; maximumNewGenerationsPerSubmission when 0
	newGenerations int
	// changes each challenge before the operator signs it, or replaces it
	// with the message it returns
	hostile func(*protocol.HotkeyNetworkDelegationStatement) string
	// acknowledgements name another head
	wrongAcknowledgement bool
	uppercaseHashes      bool
	// listed after the operator's own wallet entries
	extraWallets []map[string]any

	stateLock   sync.Mutex
	consents    map[[32]byte][]protocol.HotkeyWalletMappingConsent
	delegations []protocol.WalletMappingConsent
	issued      map[string]bool
	requests    int
	submissions int
	challenges  int
	accepts     int
}

func newTestOperator(t testing.TB, configure func(*testOperator)) (*testOperator, Operator) {
	t.Helper()
	signer, _ := testOperatorKey(t)
	operator := &testOperator{byJwt: testNetworkJwt(t), signer: signer, consents: map[[32]byte][]protocol.HotkeyWalletMappingConsent{}, issued: map[string]bool{}}
	if configure != nil {
		configure(operator)
	}
	server := httptest.NewServer(operator)
	t.Cleanup(server.Close)
	return operator, Operator{ApiUrl: server.URL, ByJwt: operator.byJwt}
}

func (self *testOperator) counts() (requests int, submissions int, challenges int, accepts int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.requests, self.submissions, self.challenges, self.accepts
}

// The network's delegation head.
func (self *testOperator) delegationHead(t testing.TB) ([32]byte, uint64) {
	t.Helper()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if len(self.delegations) == 0 {
		t.Fatal("the operator has no delegation")
	}
	_, hash, err := protocol.VerifyHotkeyNetworkDelegation(t.Context(), self.delegations[len(self.delegations)-1])
	if err != nil {
		t.Fatal(err)
	}
	return hash, uint64(len(self.delegations))
}

func (self *testOperator) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.requests++
	if !strings.HasSuffix(request.URL.Path, "/history") && request.Header.Get("Authorization") != "Bearer "+self.byJwt {
		http.Error(writer, "401 the network JWT is required", http.StatusUnauthorized)
		return
	}
	switch request.Method + " " + request.URL.Path {
	case "POST /sn/wallet/hotkey-consent":
		self.storeChain(writer, request)
	case "POST /sn/wallet/hotkey-consent/history":
		self.consentHistory(writer, request)
	case "POST /sn/wallet/hotkey-delegation":
		self.delegationChallenge(writer, request)
	case "POST /sn/wallet":
		self.acceptDelegation(writer, request)
	case "POST /sn/wallet/hotkey-delegation/history":
		self.delegationHistory(writer, request)
	case "GET /sn/wallet":
		self.listWallets(writer, request)
	default:
		http.NotFound(writer, request)
	}
}

func testDecode(writer http.ResponseWriter, request *http.Request, body any) bool {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(body); err != nil {
		http.Error(writer, "400 "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func testAnswer(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

// The hash of the stored original at the generation, or zero.
func (self *testOperator) consentHashWithLock(request *http.Request, hotkey [32]byte, generation uint64) [32]byte {
	stored := self.consents[hotkey]
	if generation == 0 || uint64(len(stored)) < generation {
		return [32]byte{}
	}
	_, hash, _ := protocol.VerifyHotkeyWalletMappingConsent(request.Context(), stored[generation-1])
	return hash
}

func (self *testOperator) storeChain(writer http.ResponseWriter, request *http.Request) {
	self.submissions++
	var body struct {
		Originals []protocol.HotkeyWalletMappingConsent `json:"originals"`
	}
	if !testDecode(writer, request, &body) {
		return
	}
	head, headHash, err := protocol.VerifyHotkeyWalletMappingLineage(request.Context(), body.Originals)
	if err != nil || head.Subnet != testDomain.HotkeySubnet() {
		http.Error(writer, "400 the chain does not verify for this operator's subnet", http.StatusBadRequest)
		return
	}
	stored := self.consents[head.Hotkey]
	for index := range min(len(stored), len(body.Originals)) {
		if stored[index] != body.Originals[index] {
			http.Error(writer, "409 another original is stored at this generation", http.StatusConflict)
			return
		}
	}
	// the server's bound on what one request may add
	bound := self.newGenerations
	if bound <= 0 {
		bound = maximumNewGenerationsPerSubmission
	}
	if len(body.Originals)-len(stored) > bound {
		http.Error(writer, "400 a submission may add at most 64 new generations", http.StatusBadRequest)
		return
	}
	if len(stored) < len(body.Originals) {
		self.consents[head.Hotkey] = slices.Clone(body.Originals)
	}
	if self.wrongAcknowledgement {
		headHash[0] ^= 1
	}
	testAnswer(writer, map[string]any{"hotkey_ss58": testAddress(head.Hotkey), "head_hash": headHash, "generation": head.Generation})
}

func (self *testOperator) consentHistory(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		HotkeySs58 string   `json:"hotkey_ss58"`
		HeadHash   [32]byte `json:"head_hash"`
		Generation uint64   `json:"generation"`
	}
	if !testDecode(writer, request, &body) {
		return
	}
	hotkey, err := ss58.DecodeWithPrefix(body.HotkeySs58, ss58.BittensorPrefix)
	if err != nil || body.HeadHash == ([32]byte{}) || self.consentHashWithLock(request, hotkey, body.Generation) != body.HeadHash {
		http.Error(writer, "404 no chain through that head", http.StatusNotFound)
		return
	}
	testAnswer(writer, map[string]any{"originals": self.consents[hotkey][:body.Generation]})
}

func (self *testOperator) delegationChallenge(writer http.ResponseWriter, request *http.Request) {
	self.challenges++
	var body struct {
		HotkeySs58        string   `json:"hotkey_ss58"`
		ConsentHeadHash   [32]byte `json:"consent_head_hash"`
		ConsentGeneration uint64   `json:"consent_generation"`
		FromEpoch         uint64   `json:"from_epoch"`
		ThroughEpoch      uint64   `json:"through_epoch"`
	}
	if !testDecode(writer, request, &body) {
		return
	}
	hotkey, err := ss58.DecodeWithPrefix(body.HotkeySs58, ss58.BittensorPrefix)
	if err != nil || body.ConsentHeadHash == ([32]byte{}) || self.consentHashWithLock(request, hotkey, body.ConsentGeneration) != body.ConsentHeadHash {
		http.Error(writer, "400 the chain through that head is not stored for the hotkey", http.StatusBadRequest)
		return
	}
	statement := protocol.HotkeyNetworkDelegationStatement{
		Domain:            testDomain,
		UserId:            testUserId,
		NetworkId:         testNetworkId,
		Hotkey:            hotkey,
		ConsentHeadHash:   body.ConsentHeadHash,
		ConsentGeneration: body.ConsentGeneration,
		Generation:        uint64(len(self.delegations)) + 1,
		IssuedAt:          testIssuedAt,
		ExpiresAt:         testIssuedAt + 300,
		FromEpoch:         body.FromEpoch,
		ThroughEpoch:      body.ThroughEpoch,
	}
	if len(self.delegations) > 0 {
		previous, previousHash, err := protocol.VerifyHotkeyNetworkDelegation(request.Context(), self.delegations[len(self.delegations)-1])
		if err != nil || body.FromEpoch <= previous.FromEpoch {
			http.Error(writer, "400 the earning epochs must follow the previous delegation's", http.StatusBadRequest)
			return
		}
		statement.PreviousHash = previousHash
	}
	_, _ = rand.Read(statement.Nonce[:])
	message := ""
	if self.hostile != nil {
		message = self.hostile(&statement)
	}
	if message == "" {
		if err := protocol.SignProspectiveHotkeyNetworkDelegation(&statement, testBoundary, self.signer); err != nil {
			http.Error(writer, "400 "+err.Error(), http.StatusBadRequest)
			return
		}
		message, _ = statement.Message()
	}
	self.issued[message] = true
	testAnswer(writer, map[string]any{"message": message})
}

func (self *testOperator) acceptDelegation(writer http.ResponseWriter, request *http.Request) {
	self.accepts++
	var body struct {
		ColdkeySs58 string `json:"coldkey_ss58"`
		Message     string `json:"message"`
		Signature   string `json:"signature"`
	}
	if !testDecode(writer, request, &body) {
		return
	}
	signature, err := hex.DecodeString(strings.TrimPrefix(body.Signature, "0x"))
	if err != nil || len(signature) != 64 {
		http.Error(writer, "400 the signature is not 64 bytes of hex", http.StatusBadRequest)
		return
	}
	original := protocol.WalletMappingConsent{Message: body.Message, Signature: [64]byte(signature)}
	statement, hash, err := protocol.VerifyHotkeyNetworkDelegation(request.Context(), original)
	if err != nil {
		http.Error(writer, "400 "+err.Error(), http.StatusBadRequest)
		return
	}
	switch signer, err := ss58.DecodeWithPrefix(body.ColdkeySs58, ss58.BittensorPrefix); {
	case err != nil || signer != statement.Hotkey:
		http.Error(writer, "400 coldkey_ss58 is not the signing hotkey", http.StatusBadRequest)
	case !self.issued[body.Message]:
		http.Error(writer, "400 the delegation is not an issued challenge", http.StatusBadRequest)
	case statement.Generation != uint64(len(self.delegations))+1:
		http.Error(writer, "409 the delegation does not extend the network's chain", http.StatusConflict)
	default:
		delete(self.issued, body.Message)
		self.delegations = append(self.delegations, original)
		if self.wrongAcknowledgement {
			hash[0] ^= 1
		}
		testAnswer(writer, map[string]any{"mapping_hash": hex.EncodeToString(hash[:]), "mapping_generation": statement.Generation})
	}
}

func (self *testOperator) delegationHistory(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Domain     protocol.ClientKeyHistoryDomain `json:"domain"`
		NetworkId  connect.Id                      `json:"network_id"`
		HeadHash   [32]byte                        `json:"head_hash"`
		Generation uint64                          `json:"generation"`
	}
	if !testDecode(writer, request, &body) {
		return
	}
	if body.Domain != testDomain || body.NetworkId != testNetworkId || body.Generation == 0 || uint64(len(self.delegations)) < body.Generation {
		http.Error(writer, "404 no delegation chain through that head", http.StatusNotFound)
		return
	}
	if _, hash, err := protocol.VerifyHotkeyNetworkDelegation(request.Context(), self.delegations[body.Generation-1]); err != nil || hash != body.HeadHash {
		http.Error(writer, "404 no delegation chain through that head", http.StatusNotFound)
		return
	}
	testAnswer(writer, map[string]any{"originals": self.delegations[:body.Generation]})
}

// A network consent entry, the hotkey entry once a delegation is accepted, and
// a provider entry, in the operator's order.
func (self *testOperator) listWallets(writer http.ResponseWriter, request *http.Request) {
	hexHash := func(hash [32]byte) string {
		if self.uppercaseHashes {
			return "0X" + strings.ToUpper(hex.EncodeToString(hash[:]))
		}
		return "0x" + hex.EncodeToString(hash[:])
	}
	wallets := []map[string]any{
		{"coldkey_ss58": testAddress([32]byte{0x51}), "set_at_millis": 0, "consent_scope": "network", "from_epoch": 1, "through_epoch": 2},
	}
	if len(self.delegations) > 0 {
		statement, hash, err := protocol.VerifyHotkeyNetworkDelegation(request.Context(), self.delegations[len(self.delegations)-1])
		if err != nil {
			http.Error(writer, "500 "+err.Error(), http.StatusInternalServerError)
			return
		}
		var coldkey [32]byte
		if stored := self.consents[statement.Hotkey]; statement.ConsentGeneration <= uint64(len(stored)) {
			if consent, err := protocol.DecodeHotkeyWalletMappingStatement(stored[statement.ConsentGeneration-1].Message); err == nil {
				coldkey = consent.Coldkey
			}
		}
		wallets = append(wallets, map[string]any{
			"coldkey_ss58":       testAddress(coldkey),
			"set_at_millis":      0,
			"consent_scope":      "hotkey",
			"hotkey_ss58":        testAddress(statement.Hotkey),
			"from_epoch":         statement.FromEpoch,
			"through_epoch":      statement.ThroughEpoch,
			"consent_head_hash":  hexHash(statement.ConsentHeadHash),
			"consent_generation": statement.ConsentGeneration,
			"mapping_hash":       hexHash(hash),
			"mapping_generation": statement.Generation,
		})
	}
	wallets = append(wallets, map[string]any{"coldkey_ss58": testAddress([32]byte{0x52}), "client_id": connect.Id{0x53}.String(), "set_at_millis": 0, "consent_scope": "provider", "from_epoch": 1, "through_epoch": 2})
	wallets = append(wallets, self.extraWallets...)
	testAnswer(writer, map[string]any{"wallet": wallets[0], "wallets": wallets})
}

func testPost(t testing.TB, apiUrl string, path string, body any, result any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(apiUrl+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s: %s", path, response.Status)
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		t.Fatal(err)
	}
}

// Resolves a provider of the delegated network at the epoch from the public
// histories, as settlement does, with the network's current delegation head
// pinned.
func testResolve(t testing.TB, operator *testOperator, client Operator, epoch uint64) *protocol.EarningWallet {
	t.Helper()
	headHash, generation := operator.delegationHead(t)
	var delegations struct {
		Originals []protocol.WalletMappingConsent `json:"originals"`
	}
	testPost(t, client.ApiUrl, "/sn/wallet/hotkey-delegation/history", map[string]any{"domain": testDomain, "network_id": testNetworkId, "head_hash": headHash, "generation": generation}, &delegations)
	head, _, err := protocol.VerifyHotkeyNetworkDelegation(t.Context(), delegations.Originals[len(delegations.Originals)-1])
	if err != nil {
		t.Fatal(err)
	}
	var consents struct {
		Originals []protocol.HotkeyWalletMappingConsent `json:"originals"`
	}
	testPost(t, client.ApiUrl, "/sn/wallet/hotkey-consent/history", map[string]any{"hotkey_ss58": testAddress(head.Hotkey), "head_hash": head.ConsentHeadHash, "generation": head.ConsentGeneration}, &consents)
	_, signer := testOperatorKey(t)
	wallet, err := protocol.ResolveHotkeyEarningWallet(t.Context(), [16]byte{0x61}, protocol.HotkeyEarningWalletEvidence{
		Delegations:        delegations.Originals,
		DelegationExpected: protocol.HotkeyNetworkDelegationHistoryExpectation{Domain: testDomain, NetworkId: testNetworkId, HeadHash: headHash, Generation: generation, Epoch: epoch},
		Consents:           consents.Originals,
	}, signer, testBoundary.Block+1, testIssuedAt+300)
	if err != nil {
		t.Fatal(err)
	}
	return wallet
}

func TestSubmitChainStoresTheChainAtTheOperator(t *testing.T) {
	operator, client := newTestOperator(t, nil)
	chain := testChain(t, testKey(t, 1), testKey(t, 2), 2)
	for _, prefix := range [][]protocol.HotkeyWalletMappingConsent{chain[:1], chain, chain} {
		headHash, generation, err := client.SubmitChain(t.Context(), prefix)
		if err != nil {
			t.Fatal(err)
		}
		if headHash != testHash(t, prefix[len(prefix)-1]) || generation != uint64(len(prefix)) {
			t.Fatalf("the submitted head was not returned: %x %d", headHash, generation)
		}
	}
	var stored struct {
		Originals []protocol.HotkeyWalletMappingConsent `json:"originals"`
	}
	testPost(t, client.ApiUrl, "/sn/wallet/hotkey-consent/history", map[string]any{"hotkey_ss58": testKey(t, 2).Address(), "head_hash": testHash(t, chain[1]), "generation": 2}, &stored)
	if !slices.Equal(stored.Originals, chain) {
		t.Fatal("the operator does not serve the submitted chain")
	}
	if _, submissions, _, _ := operator.counts(); submissions != 3 {
		t.Fatalf("expected 3 submissions, got %d", submissions)
	}
}

func TestSubmitChainSendsALongChainInPrefixes(t *testing.T) {
	// a bound of 2 stands in for the operator's 64, so 5 generations take 3 requests
	operator, client := newTestOperator(t, func(operator *testOperator) { operator.newGenerations = 2 })
	client.newGenerations = 2
	chain := testChain(t, testKey(t, 1), testKey(t, 2), 5)
	headHash, generation, err := client.SubmitChain(t.Context(), chain)
	if err != nil {
		t.Fatal(err)
	}
	if headHash != testHash(t, chain[len(chain)-1]) || generation != uint64(len(chain)) {
		t.Fatalf("acknowledged head %x generation %d, want the chain's head", headHash, generation)
	}
	if _, submissions, _, _ := operator.counts(); submissions != 3 {
		t.Fatalf("a %d-generation chain took %d submissions, want 3", len(chain), submissions)
	}
	if _, _, err := client.SubmitChain(t.Context(), chain); err != nil {
		t.Fatalf("replaying the stored chain failed: %v", err)
	}
}

func TestSubmitChainSendsNoUnverifiedChain(t *testing.T) {
	operator, client := newTestOperator(t, nil)
	chain := testChain(t, testKey(t, 1), testKey(t, 2), 2)
	unsigned := slices.Clone(chain)
	unsigned[1].HotkeySignature = unsigned[1].ColdkeySignature
	for _, refused := range [][]protocol.HotkeyWalletMappingConsent{nil, unsigned, chain[1:], {chain[1], chain[0]}} {
		if _, _, err := client.SubmitChain(t.Context(), refused); err == nil {
			t.Errorf("an unverified chain was submitted")
		}
	}
	if requests, _, _, _ := operator.counts(); requests != 0 {
		t.Fatalf("an unverified chain reached the operator in %d requests", requests)
	}
}

func TestSubmitChainReportsARefusalOrAnotherHead(t *testing.T) {
	coldkey, hotkey := testKey(t, 1), testKey(t, 2)
	chain := testChain(t, coldkey, hotkey, 1)
	fork := []protocol.HotkeyWalletMappingConsent{testOriginal(t, nil, coldkey, hotkey, 100, 1100)}
	_, client := newTestOperator(t, nil)
	if _, _, err := client.SubmitChain(t.Context(), chain); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.SubmitChain(t.Context(), fork); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "409") {
		t.Fatalf("a refused fork was not reported: %v", err)
	}
	_, lying := newTestOperator(t, func(operator *testOperator) { operator.wrongAcknowledgement = true })
	if _, _, err := lying.SubmitChain(t.Context(), chain); !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
		t.Fatalf("an acknowledgement of another head was admitted: %v", err)
	}
}

func TestEnsureDelegationSignsOnceAndThenLeavesItAsIs(t *testing.T) {
	coldkey, hotkey := testKey(t, 1), testKey(t, 2)
	operator, client := newTestOperator(t, nil)
	headHash, generation, err := client.SubmitChain(t.Context(), testChain(t, coldkey, hotkey, 1))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := client.EnsureDelegation(t.Context(), hotkey, headHash, generation, 100, 1100); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, challenges, accepts := operator.counts(); challenges != 1 || accepts != 1 {
		t.Fatalf("an adopted head was delegated again: %d challenges, %d accepts", challenges, accepts)
	}
	wallet := testResolve(t, operator, client, 100)
	if wallet.Mode != protocol.EarningWalletModeHotkey || wallet.Coldkey != coldkey.PublicKey() || wallet.Hotkey != hotkey.PublicKey() || wallet.NetworkId != [16]byte(testNetworkId) || wallet.ConsentHeadHash != headHash {
		t.Fatalf("the network does not earn to the coldkey through the hotkey: %+v", wallet)
	}
}

func TestEnsureDelegationAdoptsANewConsentHead(t *testing.T) {
	coldkey, hotkey, replacement := testKey(t, 1), testKey(t, 2), testKey(t, 3)
	operator, client := newTestOperator(t, func(operator *testOperator) { operator.uppercaseHashes = true })
	chain := testChain(t, coldkey, hotkey, 1)
	firstHash, firstGeneration, err := client.SubmitChain(t.Context(), chain)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.EnsureDelegation(t.Context(), hotkey, firstHash, firstGeneration, 100, 1100); err != nil {
		t.Fatal(err)
	}
	chain = append(chain, testOriginal(t, chain, replacement, hotkey, 200, 1200))
	secondHash, secondGeneration, err := client.SubmitChain(t.Context(), chain)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := client.EnsureDelegation(t.Context(), hotkey, secondHash, secondGeneration, 200, 1200); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, challenges, accepts := operator.counts(); challenges != 2 || accepts != 2 {
		t.Fatalf("the new head was not delegated exactly once: %d challenges, %d accepts", challenges, accepts)
	}
	if wallet := testResolve(t, operator, client, 200); wallet.Coldkey != replacement.PublicKey() || wallet.ConsentHeadHash != secondHash || wallet.HeadGeneration != 2 {
		t.Fatalf("the network does not earn to the replacement coldkey: %+v", wallet)
	}
}

func TestEnsureDelegationNeverSignsAHostileChallenge(t *testing.T) {
	coldkey, hotkey, attacker := testKey(t, 1), testKey(t, 2), testKey(t, 4)
	signer, _ := testOperatorKey(t)
	chain := testChain(t, coldkey, hotkey, 1)
	headHash := testHash(t, chain[0])
	// what the hotkey's signature would make valid: the attacker's coldkey as
	// the next global generation, or the hotkey as a provider's or a
	// network's coldkey
	global, err := NextStatement(chain, testDomain.HotkeySubnet(), hotkey.PublicKey(), attacker.PublicKey(), 200, 1200, testNow)
	if err != nil {
		t.Fatal(err)
	}
	globalMessage, err := global.Message()
	if err != nil {
		t.Fatal(err)
	}
	provider := protocol.WalletMappingStatement{Schema: protocol.WalletMappingConsentSchema, Domain: testDomain, UserId: testUserId, ClientId: [16]byte{8}, NetworkId: testNetworkId, Coldkey: hotkey.PublicKey(), Generation: 1, Nonce: [32]byte{10}, IssuedAt: testIssuedAt, ExpiresAt: testIssuedAt + 300, FromEpoch: 100, ThroughEpoch: 1100}
	providerMessage, err := provider.Message()
	if err != nil {
		t.Fatal(err)
	}
	network := protocol.NetworkWalletMappingStatement{Domain: testDomain, UserId: testUserId, NetworkId: testNetworkId, Coldkey: hotkey.PublicKey(), Generation: 1, Nonce: [32]byte{10}, IssuedAt: testIssuedAt, ExpiresAt: testIssuedAt + 300, FromEpoch: 100, ThroughEpoch: 1100}
	if err := protocol.SignProspectiveNetworkWalletMapping(&network, testBoundary, signer); err != nil {
		t.Fatal(err)
	}
	networkMessage, err := network.Message()
	if err != nil {
		t.Fatal(err)
	}
	notDelegation := "is not a hotkey delegation"
	for _, c := range []struct {
		name    string
		hostile func(*protocol.HotkeyNetworkDelegationStatement) string
		// the reason it is refused before the hotkey signs
		refusal string
	}{
		{name: "another hotkey", refusal: "the requested hotkey", hostile: func(statement *protocol.HotkeyNetworkDelegationStatement) string {
			statement.Hotkey = attacker.PublicKey()
			return ""
		}},
		{name: "another consent head", refusal: "the requested consent head", hostile: func(statement *protocol.HotkeyNetworkDelegationStatement) string {
			statement.ConsentHeadHash[0] ^= 1
			return ""
		}},
		{name: "another consent generation", refusal: "the requested consent head", hostile: func(statement *protocol.HotkeyNetworkDelegationStatement) string {
			statement.ConsentGeneration++
			return ""
		}},
		{name: "another network", refusal: "the network of the JWT", hostile: func(statement *protocol.HotkeyNetworkDelegationStatement) string {
			statement.NetworkId = [16]byte{0x66}
			return ""
		}},
		{name: "another user", refusal: "the user of the JWT", hostile: func(statement *protocol.HotkeyNetworkDelegationStatement) string {
			statement.UserId = [16]byte{0x67}
			return ""
		}},
		{name: "another from epoch", refusal: "the requested earning epochs", hostile: func(statement *protocol.HotkeyNetworkDelegationStatement) string {
			statement.FromEpoch++
			return ""
		}},
		{name: "another through epoch", refusal: "the requested earning epochs", hostile: func(statement *protocol.HotkeyNetworkDelegationStatement) string {
			statement.ThroughEpoch++
			return ""
		}},
		{name: "a delegation with trailing text", refusal: notDelegation, hostile: func(statement *protocol.HotkeyNetworkDelegationStatement) string {
			if err := protocol.SignProspectiveHotkeyNetworkDelegation(statement, testBoundary, signer); err != nil {
				return "unsigned"
			}
			message, _ := statement.Message()
			return message + "\n"
		}},
		{name: "a global hotkey consent", refusal: notDelegation, hostile: func(*protocol.HotkeyNetworkDelegationStatement) string { return globalMessage }},
		{name: "a provider consent", refusal: notDelegation, hostile: func(*protocol.HotkeyNetworkDelegationStatement) string { return providerMessage }},
		{name: "a network consent", refusal: notDelegation, hostile: func(*protocol.HotkeyNetworkDelegationStatement) string { return networkMessage }},
		{name: "a sign-in challenge", refusal: notDelegation, hostile: func(*protocol.HotkeyNetworkDelegationStatement) string {
			return "Sign in to URnetwork\nChallenge: AAAAAAAAAAAAAAAAAAAAAAAA\nTimestamp: 1791244800"
		}},
	} {
		operator, client := newTestOperator(t, func(operator *testOperator) { operator.hostile = c.hostile })
		if _, _, err := client.SubmitChain(t.Context(), chain); err != nil {
			t.Fatal(err)
		}
		err := client.EnsureDelegation(t.Context(), hotkey, headHash, 1, 100, 1100)
		if _, _, challenges, accepts := operator.counts(); !errors.Is(err, protocol.ErrWalletMappingIntegrity) || !strings.Contains(err.Error(), c.refusal) || challenges != 1 || accepts != 0 {
			t.Errorf("a challenge naming %s was signed or not refused unsigned: %d challenges, %d accepts: %v", c.name, challenges, accepts, err)
		}
	}
}

func TestEnsureDelegationRefusesAClientOrIncompleteJwt(t *testing.T) {
	hotkey := testKey(t, 2)
	for _, byJwt := range []string{
		testJwt(t, gojwt.MapClaims{"network_id": testNetworkId.String(), "user_id": testUserId.String(), "client_id": connect.Id{0x61}.String()}),
		testJwt(t, gojwt.MapClaims{"user_id": testUserId.String()}),
		testJwt(t, gojwt.MapClaims{"network_id": testNetworkId.String()}),
		testJwt(t, gojwt.MapClaims{"network_id": "synthetic", "user_id": testUserId.String()}),
		testJwt(t, gojwt.MapClaims{"network_id": connect.Id{}.String(), "user_id": testUserId.String()}),
		"",
		"not-a-jwt",
	} {
		operator, client := newTestOperator(t, func(operator *testOperator) { operator.byJwt = byJwt })
		if err := client.EnsureDelegation(t.Context(), hotkey, [32]byte{1}, 1, 100, 1100); err == nil {
			t.Errorf("the credential %q was admitted", byJwt)
		}
		if requests, _, _, _ := operator.counts(); requests != 0 {
			t.Errorf("the credential %q reached the operator", byJwt)
		}
	}
}

func TestEnsureDelegationNeverMovesBackToAnEarlierConsentGeneration(t *testing.T) {
	coldkey, hotkey := testKey(t, 1), testKey(t, 2)
	operator, client := newTestOperator(t, nil)
	chain := testChain(t, coldkey, hotkey, 2)
	headHash, generation, err := client.SubmitChain(t.Context(), chain)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.EnsureDelegation(t.Context(), hotkey, headHash, generation, 200, 1200); err != nil {
		t.Fatal(err)
	}
	if err := client.EnsureDelegation(t.Context(), hotkey, testHash(t, chain[0]), 1, 300, 1300); err == nil {
		t.Fatal("a delegation moved back to an earlier consent generation")
	}
	if _, _, challenges, accepts := operator.counts(); challenges != 1 || accepts != 1 {
		t.Fatalf("the earlier generation was requested: %d challenges, %d accepts", challenges, accepts)
	}
}

func TestEnsureDelegationCountsOnlyTheNetworkEntryOfTheHotkey(t *testing.T) {
	coldkey, hotkey := testKey(t, 1), testKey(t, 2)
	chain := testChain(t, coldkey, hotkey, 1)
	headHash := testHash(t, chain[0])
	entry := func(fields map[string]any) map[string]any {
		wallet := map[string]any{"coldkey_ss58": testAddress(coldkey.PublicKey()), "set_at_millis": 0, "consent_scope": "hotkey", "hotkey_ss58": hotkey.Address(), "from_epoch": 100, "through_epoch": 1100, "consent_head_hash": "0x" + hex.EncodeToString(headHash[:]), "consent_generation": 1, "mapping_hash": "0x" + strings.Repeat("ab", 32), "mapping_generation": 1}
		for key, value := range fields {
			wallet[key] = value
		}
		return wallet
	}
	operator, client := newTestOperator(t, func(operator *testOperator) {
		operator.extraWallets = []map[string]any{
			entry(map[string]any{"client_id": connect.Id{0x61}.String()}),
			entry(map[string]any{"consent_scope": "network"}),
			entry(map[string]any{"hotkey_ss58": testKey(t, 4).Address()}),
			entry(map[string]any{"consent_head_hash": "0x" + strings.Repeat("cd", 32)}),
			entry(map[string]any{"consent_head_hash": hex.EncodeToString(headHash[:31])}),
		}
	})
	if _, _, err := client.SubmitChain(t.Context(), chain); err != nil {
		t.Fatal(err)
	}
	if err := client.EnsureDelegation(t.Context(), hotkey, headHash, 1, 100, 1100); err != nil {
		t.Fatal(err)
	}
	if _, _, challenges, accepts := operator.counts(); challenges != 1 || accepts != 1 {
		t.Fatalf("another entry was taken for the network's delegation: %d challenges, %d accepts", challenges, accepts)
	}
}

func TestEnsureDelegationRefusesAnAcknowledgementOfAnotherDelegation(t *testing.T) {
	coldkey, hotkey := testKey(t, 1), testKey(t, 2)
	chain := testChain(t, coldkey, hotkey, 1)
	operator, client := newTestOperator(t, func(operator *testOperator) {
		operator.consents[hotkey.PublicKey()] = chain
		operator.wrongAcknowledgement = true
	})
	err := client.EnsureDelegation(t.Context(), hotkey, testHash(t, chain[0]), 1, 100, 1100)
	if _, _, _, accepts := operator.counts(); !errors.Is(err, protocol.ErrWalletMappingIntegrity) || accepts != 1 {
		t.Fatalf("an acknowledgement of another delegation was admitted: %d accepts: %v", accepts, err)
	}
}

// Counts every request it is given and answers none.
type testTransport struct {
	requests atomic.Int32
}

func (self *testTransport) RoundTrip(*http.Request) (*http.Response, error) {
	self.requests.Add(1)
	return nil, errors.New("the synthetic transport answers no request")
}

func TestOperatorRefusesUnsafeUrlsAndNeverFollowsARedirect(t *testing.T) {
	chain := testChain(t, testKey(t, 1), testKey(t, 2), 1)
	transport := &testTransport{}
	for _, apiUrl := range []string{
		"http://api.operator.example",
		"http://localhost.operator.example",
		"https://user:synthetic-secret@api.operator.example",
		"https://api.operator.example?operator=1",
		"https://api.operator.example#operator",
		"ftp://api.operator.example",
		"api.operator.example",
		"",
	} {
		_, _, err := Operator{ApiUrl: apiUrl, ByJwt: testNetworkJwt(t), Client: &http.Client{Transport: transport}}.SubmitChain(t.Context(), chain)
		if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
			t.Errorf("the api url %q was admitted or printed its credentials: %v", apiUrl, err)
		}
	}
	if requests := transport.requests.Load(); requests != 0 {
		t.Fatalf("unsafe api urls were requested %d times", requests)
	}

	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL+request.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()
	for _, client := range []*http.Client{nil, {}} {
		_, _, err := Operator{ApiUrl: redirector.URL, ByJwt: testNetworkJwt(t), Client: client}.SubmitChain(t.Context(), chain)
		if !errors.Is(err, ErrRefused) || redirected.Load() != 0 {
			t.Fatalf("a redirect was followed: %v", err)
		}
	}
}

func TestOperatorBoundsAndChecksAnswers(t *testing.T) {
	hotkey := testKey(t, 2)
	chain := testChain(t, testKey(t, 1), hotkey, 1)
	acknowledgement := testJson(t, map[string]any{"hotkey_ss58": hotkey.Address(), "head_hash": testHash(t, chain[0]), "generation": 1})
	for _, c := range []struct {
		answer   string
		admitted bool
	}{
		{answer: acknowledgement, admitted: true},
		// valid JSON that only the bound refuses
		{answer: acknowledgement + strings.Repeat(" ", maximumAnswerBytes)},
		{answer: "not json"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte(c.answer))
		}))
		_, _, err := Operator{ApiUrl: server.URL, ByJwt: testNetworkJwt(t)}.SubmitChain(t.Context(), chain)
		server.Close()
		if (err == nil) != c.admitted {
			t.Errorf("the answer %.48q was admitted %t, want %t: %v", c.answer, err == nil, c.admitted, err)
		}
	}
}

func TestEnsureDelegationNeedsAHotkeyAndAHead(t *testing.T) {
	hotkey := testKey(t, 2)
	operator, client := newTestOperator(t, nil)
	for _, c := range []struct {
		hotkey     *crv4.Keypair
		headHash   [32]byte
		generation uint64
	}{
		{headHash: [32]byte{1}, generation: 1},
		{hotkey: hotkey, generation: 1},
		{hotkey: hotkey, headHash: [32]byte{1}},
	} {
		if err := client.EnsureDelegation(t.Context(), c.hotkey, c.headHash, c.generation, 100, 1100); err == nil {
			t.Errorf("a delegation without a hotkey or head was attempted: %+v", c)
		}
	}
	if requests, _, _, _ := operator.counts(); requests != 0 {
		t.Fatalf("an incomplete delegation reached the operator in %d requests", requests)
	}
}
