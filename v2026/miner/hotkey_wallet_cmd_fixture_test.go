package miner

// A synthetic operator of the hotkey wallet routes (section 6.5), mirroring
// hotkeywallet's test operator: it verifies and stores each hotkey's chain,
// issues network consent and delegation challenges with a boundary its
// synthetic key signs, accepts a delegation only with the hotkey's signature
// over a challenge it issued, and lists the network's hotkey entry. Requests
// decode with unknown fields refused. Keys, identities and domains are
// visibly synthetic.

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/hotkeywallet"
	"github.com/urfoundation/sn/v2026/operatorlist"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
)

// The finalized boundary every synthetic challenge is issued at.
var testWalletBoundary = protocol.ClientKeyEffectiveBoundary{Epoch: 50, Block: 500, Hash: [32]byte{12}}

// Every synthetic operator's current epoch, just after its boundary.
const testWalletEpoch = 51

// When every synthetic challenge is issued; it expires 300 seconds later.
const testWalletIssuedAt = 1791244800

// Every synthetic operator serves this subnet unless a test moves one.
var testWalletDomain = protocol.ClientKeyHistoryDomain{
	ChainID:          7,
	GenesisHash:      [32]byte{3},
	Netuid:           25,
	Coordinator:      common.Address{4},
	SettlementVault:  common.Address{5},
	DeploymentIDHash: [32]byte{6},
	PolicyHash:       [32]byte{7},
	NoID:             8,
}

// Configuration is set before the server starts, except unavailable.
type testHotkeyWalletOperator struct {
	server    *httptest.Server
	domain    protocol.ClientKeyHistoryDomain
	networkId connect.Id
	userId    connect.Id
	byJwt     string
	signer    *ecdsa.PrivateKey
	// a network consent the network already holds, from this epoch
	networkConsentFrom uint64

	stateLock   sync.Mutex
	unavailable bool
	consents    map[[32]byte][]protocol.HotkeyWalletMappingConsent
	delegations []protocol.WalletMappingConsent
	issued      map[string]bool
	submissions int
	accepts     int
}

// Each operator has its own network, user and operator domain number.
func newTestHotkeyWalletOperator(t *testing.T, number byte, configure func(*testHotkeyWalletOperator)) *testHotkeyWalletOperator {
	t.Helper()
	signer, err := crypto.HexToECDSA(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	self := &testHotkeyWalletOperator{
		domain:    testWalletDomain,
		networkId: connect.Id{number, 9},
		userId:    connect.Id{number, 7},
		signer:    signer,
		consents:  map[[32]byte][]protocol.HotkeyWalletMappingConsent{},
		issued:    map[string]bool{},
	}
	self.domain.NoID = uint64(number)
	self.byJwt, err = gojwt.NewWithClaims(gojwt.SigningMethodNone, gojwt.MapClaims{"network_id": self.networkId.String(), "user_id": self.userId.String()}).SignedString(gojwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if configure != nil {
		configure(self)
	}
	self.server = httptest.NewServer(self)
	t.Cleanup(self.server.Close)
	return self
}

// The operator as a list entry; plaintext urls are listed only for loopback.
func (self *testHotkeyWalletOperator) listed(domain string) operatorlist.Operator {
	return operatorlist.Operator{Domain: domain, ApiUrl: self.server.URL, ConnectUrl: "ws" + strings.TrimPrefix(self.server.URL, "http")}
}

// Writes the operator's network jwt as provider auth --operator would.
func (self *testHotkeyWalletOperator) authenticate(t *testing.T, base string, domain string) {
	t.Helper()
	if err := clientauth.WriteToken(operatorJwtPath(base, domain), self.byJwt); err != nil {
		t.Fatal(err)
	}
}

func (self *testHotkeyWalletOperator) setUnavailable(unavailable bool) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.unavailable = unavailable
}

func (self *testHotkeyWalletOperator) counts() (submissions int, accepts int) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.submissions, self.accepts
}

// The network's delegation head, or nil when there is none.
func (self *testHotkeyWalletOperator) delegationHead(t *testing.T) *protocol.HotkeyNetworkDelegationStatement {
	t.Helper()
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if len(self.delegations) == 0 {
		return nil
	}
	statement, _, err := protocol.VerifyHotkeyNetworkDelegation(t.Context(), self.delegations[len(self.delegations)-1])
	if err != nil {
		t.Fatal(err)
	}
	return statement
}

func (self *testHotkeyWalletOperator) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.unavailable {
		http.Error(writer, "503 synthetic outage", http.StatusServiceUnavailable)
		return
	}
	if request.URL.Path != "/sn/epoch" && request.Header.Get("Authorization") != "Bearer "+self.byJwt {
		http.Error(writer, "401 the network JWT is required", http.StatusUnauthorized)
		return
	}
	switch request.Method + " " + request.URL.Path {
	case "GET /sn/epoch":
		testWalletAnswer(writer, map[string]any{"epoch": testWalletEpoch, "chain_id": self.domain.ChainID})
	case "POST /sn/wallet/network-consent":
		self.networkChallenge(writer, request)
	case "POST /sn/wallet/hotkey-consent":
		self.storeChain(writer, request)
	case "POST /sn/wallet/hotkey-delegation":
		self.delegationChallenge(writer, request)
	case "POST /sn/wallet":
		self.acceptDelegation(writer, request)
	case "GET /sn/wallet":
		self.listWallets(writer, request)
	default:
		http.NotFound(writer, request)
	}
}

func testWalletDecode(writer http.ResponseWriter, request *http.Request, body any) bool {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(body); err != nil {
		http.Error(writer, "400 "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func testWalletAnswer(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

func testSs58(key [32]byte) string {
	address, _ := ss58.Encode(key, ss58.BittensorPrefix)
	return address
}

// Issued only to be read here: the network consent itself is never accepted.
func (self *testHotkeyWalletOperator) networkChallenge(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		ColdkeySs58  string `json:"coldkey_ss58"`
		FromEpoch    uint64 `json:"from_epoch"`
		ThroughEpoch uint64 `json:"through_epoch"`
	}
	if !testWalletDecode(writer, request, &body) {
		return
	}
	coldkey, err := ss58.DecodeWithPrefix(body.ColdkeySs58, ss58.BittensorPrefix)
	if err != nil || 0 < self.networkConsentFrom && body.FromEpoch <= self.networkConsentFrom {
		http.Error(writer, "400 the earning epochs must follow the network's consent", http.StatusBadRequest)
		return
	}
	statement := protocol.NetworkWalletMappingStatement{
		Domain:       self.domain,
		UserId:       self.userId,
		NetworkId:    self.networkId,
		Coldkey:      coldkey,
		Generation:   1,
		IssuedAt:     testWalletIssuedAt,
		ExpiresAt:    testWalletIssuedAt + 300,
		FromEpoch:    body.FromEpoch,
		ThroughEpoch: body.ThroughEpoch,
	}
	_, _ = rand.Read(statement.Nonce[:])
	if err := protocol.SignProspectiveNetworkWalletMapping(&statement, testWalletBoundary, self.signer); err != nil {
		http.Error(writer, "400 "+err.Error(), http.StatusBadRequest)
		return
	}
	message, _ := statement.Message()
	testWalletAnswer(writer, map[string]any{"message": message})
}

// The hash of the stored original at the generation, or zero.
func (self *testHotkeyWalletOperator) consentHashWithLock(request *http.Request, hotkey [32]byte, generation uint64) [32]byte {
	stored := self.consents[hotkey]
	if generation == 0 || uint64(len(stored)) < generation {
		return [32]byte{}
	}
	_, hash, _ := protocol.VerifyHotkeyWalletMappingConsent(request.Context(), stored[generation-1])
	return hash
}

func (self *testHotkeyWalletOperator) storeChain(writer http.ResponseWriter, request *http.Request) {
	self.submissions++
	var body struct {
		Originals []protocol.HotkeyWalletMappingConsent `json:"originals"`
	}
	if !testWalletDecode(writer, request, &body) {
		return
	}
	head, headHash, err := protocol.VerifyHotkeyWalletMappingLineage(request.Context(), body.Originals)
	if err != nil || head.Subnet != self.domain.HotkeySubnet() {
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
	if len(stored) < len(body.Originals) {
		self.consents[head.Hotkey] = slices.Clone(body.Originals)
	}
	testWalletAnswer(writer, map[string]any{"hotkey_ss58": testSs58(head.Hotkey), "head_hash": headHash, "generation": head.Generation})
}

func (self *testHotkeyWalletOperator) delegationChallenge(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		HotkeySs58        string   `json:"hotkey_ss58"`
		ConsentHeadHash   [32]byte `json:"consent_head_hash"`
		ConsentGeneration uint64   `json:"consent_generation"`
		FromEpoch         uint64   `json:"from_epoch"`
		ThroughEpoch      uint64   `json:"through_epoch"`
	}
	if !testWalletDecode(writer, request, &body) {
		return
	}
	hotkey, err := ss58.DecodeWithPrefix(body.HotkeySs58, ss58.BittensorPrefix)
	if err != nil || body.ConsentHeadHash == ([32]byte{}) || self.consentHashWithLock(request, hotkey, body.ConsentGeneration) != body.ConsentHeadHash {
		http.Error(writer, "400 the chain through that head is not stored for the hotkey", http.StatusBadRequest)
		return
	}
	statement := protocol.HotkeyNetworkDelegationStatement{
		Domain:            self.domain,
		UserId:            self.userId,
		NetworkId:         self.networkId,
		Hotkey:            hotkey,
		ConsentHeadHash:   body.ConsentHeadHash,
		ConsentGeneration: body.ConsentGeneration,
		Generation:        uint64(len(self.delegations)) + 1,
		IssuedAt:          testWalletIssuedAt,
		ExpiresAt:         testWalletIssuedAt + 300,
		FromEpoch:         body.FromEpoch,
		ThroughEpoch:      body.ThroughEpoch,
	}
	if 0 < len(self.delegations) {
		previous, previousHash, err := protocol.VerifyHotkeyNetworkDelegation(request.Context(), self.delegations[len(self.delegations)-1])
		if err != nil || body.FromEpoch <= previous.FromEpoch {
			http.Error(writer, "400 the earning epochs must follow the previous delegation's", http.StatusBadRequest)
			return
		}
		statement.PreviousHash = previousHash
	}
	_, _ = rand.Read(statement.Nonce[:])
	// refuses a from epoch at or before the boundary
	if err := protocol.SignProspectiveHotkeyNetworkDelegation(&statement, testWalletBoundary, self.signer); err != nil {
		http.Error(writer, "400 "+err.Error(), http.StatusBadRequest)
		return
	}
	message, _ := statement.Message()
	self.issued[message] = true
	testWalletAnswer(writer, map[string]any{"message": message})
}

func (self *testHotkeyWalletOperator) acceptDelegation(writer http.ResponseWriter, request *http.Request) {
	self.accepts++
	var body struct {
		ColdkeySs58 string `json:"coldkey_ss58"`
		Message     string `json:"message"`
		Signature   string `json:"signature"`
	}
	if !testWalletDecode(writer, request, &body) {
		return
	}
	signature, err := hex.DecodeString(strings.TrimPrefix(body.Signature, "0x"))
	if err != nil || len(signature) != 64 {
		http.Error(writer, "400 the signature is not 64 bytes of hex", http.StatusBadRequest)
		return
	}
	original := protocol.WalletMappingConsent{Message: body.Message, Signature: [64]byte(signature)}
	statement, hash, err := protocol.VerifyHotkeyNetworkDelegation(request.Context(), original)
	switch {
	case err != nil:
		http.Error(writer, "400 "+err.Error(), http.StatusBadRequest)
	case body.ColdkeySs58 != testSs58(statement.Hotkey):
		http.Error(writer, "400 coldkey_ss58 is not the signing hotkey", http.StatusBadRequest)
	case !self.issued[body.Message]:
		http.Error(writer, "400 the delegation is not an issued challenge", http.StatusBadRequest)
	case statement.Generation != uint64(len(self.delegations))+1:
		http.Error(writer, "409 the delegation does not extend the network's chain", http.StatusConflict)
	default:
		delete(self.issued, body.Message)
		self.delegations = append(self.delegations, original)
		testWalletAnswer(writer, map[string]any{"mapping_hash": hex.EncodeToString(hash[:]), "mapping_generation": statement.Generation})
	}
}

// The network consent entry when configured, the hotkey entry once a
// delegation is accepted, and a provider entry, in the operator's order.
func (self *testHotkeyWalletOperator) listWallets(writer http.ResponseWriter, request *http.Request) {
	var wallets []map[string]any
	if 0 < self.networkConsentFrom {
		wallets = append(wallets, map[string]any{"coldkey_ss58": testSs58([32]byte{0x51}), "set_at_millis": 0, "consent_scope": "network", "from_epoch": self.networkConsentFrom, "through_epoch": self.networkConsentFrom + 10})
	}
	if 0 < len(self.delegations) {
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
			"coldkey_ss58":       testSs58(coldkey),
			"set_at_millis":      0,
			"consent_scope":      "hotkey",
			"hotkey_ss58":        testSs58(statement.Hotkey),
			"from_epoch":         statement.FromEpoch,
			"through_epoch":      statement.ThroughEpoch,
			"consent_head_hash":  "0x" + hex.EncodeToString(statement.ConsentHeadHash[:]),
			"consent_generation": statement.ConsentGeneration,
			"mapping_hash":       "0x" + hex.EncodeToString(hash[:]),
			"mapping_generation": statement.Generation,
		})
	}
	wallets = append(wallets, map[string]any{"coldkey_ss58": testSs58([32]byte{0x52}), "client_id": connect.Id{0x53}.String(), "set_at_millis": 0, "consent_scope": "provider", "from_epoch": 1, "through_epoch": 2})
	testWalletAnswer(writer, map[string]any{"wallet": wallets[0], "wallets": wallets})
}

// Serves the operators as the published list and returns its url.
func testServeOperatorList(t *testing.T, operators ...operatorlist.Operator) string {
	t.Helper()
	raw := testOperatorListYaml(t, operators...)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/yaml")
		_, _ = writer.Write([]byte(raw))
	}))
	t.Cleanup(server.Close)
	return server.URL + "/operators.yml"
}

// Appends generation 1 of the coldkey's consent for the hotkey, earning
// epochs 0 through 65535, as wallet hotkey set does.
func testHotkeyWalletChain(t *testing.T, base string, coldkey *crv4.Keypair, hotkey *crv4.Keypair) []protocol.HotkeyWalletMappingConsent {
	t.Helper()
	statement, err := hotkeywallet.NextStatement(nil, testWalletDomain.HotkeySubnet(), hotkey.PublicKey(), coldkey.PublicKey(), 0, 65535, time.Unix(testWalletIssuedAt, 0))
	if err != nil {
		t.Fatal(err)
	}
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	original := protocol.HotkeyWalletMappingConsent{Message: message}
	if original.ColdkeySignature, err = hotkeywallet.Sign(coldkey, message); err != nil {
		t.Fatal(err)
	}
	if original.HotkeySignature, err = hotkeywallet.Sign(hotkey, message); err != nil {
		t.Fatal(err)
	}
	store := hotkeyWalletStore(base)
	if err := store.Append(original); err != nil {
		t.Fatal(err)
	}
	chain, err := store.Chain()
	if err != nil {
		t.Fatal(err)
	}
	return chain
}

// The 64 hex characters of a seed of one repeated byte.
func testSeedHex(fill byte) string {
	return strings.Repeat(fmt.Sprintf("%02x", fill), 32)
}
