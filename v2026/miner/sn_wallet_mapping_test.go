// Actual miner and swarm HTTP producers preserve one signed consent across
// restart and independently verify every current server acknowledgement.
package miner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// Every authority field is synthetic; none comes from local deployment config.
func financialWalletMappingTestDomain() protocol.ClientKeyHistoryDomain {
	return protocol.ClientKeyHistoryDomain{ChainID: 945, GenesisHash: [32]byte{1}, Netuid: 521, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: sha256.Sum256([]byte("synthetic-wallet-consent")), PolicyHash: [32]byte{4}, NoID: 1}
}

// A deterministic test root signs each canonical prospective statement. The
// real wallet producer must verify these bytes before invoking its coldkey.
func financialWalletMappingTestSign(t testing.TB, statement protocol.WalletMappingStatement) string {
	t.Helper()
	root, err := crypto.HexToECDSA(strings.Repeat("47", 32))
	if err != nil {
		t.Fatal(err)
	}
	boundary := statement.Prospective.Boundary
	if boundary == (protocol.ClientKeyEffectiveBoundary{}) {
		boundary = protocol.ClientKeyEffectiveBoundary{Epoch: statement.FromEpoch - 1, Block: 100, Hash: [32]byte{5}}
	}
	if err := protocol.SignProspectiveWalletMapping(&statement, boundary, root); err != nil {
		t.Fatal(err)
	}
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	return message
}

// Shared startup fixtures use the same complete token identities and actual
// prospective signature as the wallet producer's dedicated HTTP regressions.
func financialWalletMappingTestMessage(t testing.TB, token string, coldkey [32]byte, from, through int64, nonce byte) string {
	t.Helper()
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(token, claims); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	statement := protocol.WalletMappingStatement{Domain: financialWalletMappingTestDomain(), Coldkey: coldkey, Generation: 1, Nonce: [32]byte{nonce}, IssuedAt: now, ExpiresAt: now + 300, FromEpoch: uint64(from), ThroughEpoch: uint64(through)}
	for name, destination := range map[string]*[16]byte{"client_id": &statement.ClientId, "network_id": &statement.NetworkId, "user_id": &statement.UserId} {
		raw, ok := claims[name].(string)
		id, err := connect.ParseId(raw)
		if !ok || err != nil || id == (connect.Id{}) {
			t.Fatalf("synthetic mapping token lacks %s: %v", name, err)
		}
		*destination = [16]byte(id)
	}
	return financialWalletMappingTestSign(t, statement)
}

// The HTTP peer verifies the actual coldkey signature and selected client,
// then computes the accepted checkpoint from the exact original bytes.
func financialWalletMappingTestReceipt(ctx context.Context, args *sdk.SnSetWalletArgs) (protocol.WalletMappingConsent, *sdk.SnSetWalletResult, error) {
	encoded, err := hex.DecodeString(strings.TrimPrefix(args.Signature, "0x"))
	if err != nil || len(encoded) != 64 {
		return protocol.WalletMappingConsent{}, nil, errors.New("wallet signature has invalid encoding")
	}
	original := protocol.WalletMappingConsent{Message: args.Message, Signature: [64]byte(encoded)}
	statement, head, err := protocol.VerifyWalletMappingConsent(ctx, original)
	if err != nil {
		return protocol.WalletMappingConsent{}, nil, err
	}
	if args.ClientId == nil || args.ClientId.String() != connect.Id(statement.ClientId).String() {
		return protocol.WalletMappingConsent{}, nil, errors.New("wallet consent client differs from request")
	}
	coldkey, err := ss58.DecodeWithPrefix(args.ColdkeySs58, ss58.BittensorPrefix)
	if err != nil || coldkey != statement.Coldkey {
		return protocol.WalletMappingConsent{}, nil, errors.New("wallet consent coldkey differs from request")
	}
	return original, &sdk.SnSetWalletResult{MappingHash: hex.EncodeToString(head[:]), MappingGeneration: int64(statement.Generation)}, nil
}

// State is synchronized because HTTP handlers run independently of assertions.
// Faults change concrete message fields, receipts or the current server domain.
type financialWalletMappingHttp struct {
	t                 *testing.T
	endpoint          *httptest.Server
	token             string
	address           string
	coldkey           [32]byte
	clientId          string
	stateLock         sync.Mutex
	epoch             int64
	fromEpoch         int64
	throughEpoch      int64
	domain            protocol.ClientKeyHistoryDomain
	challengeFault    string
	receiptFault      string
	beforePost        func(protocol.WalletMappingConsent)
	loseAck           context.CancelFunc
	epochRequests     int
	challengeRequests []sdk.SnWalletMappingChallengeArgs
	messages          []string
	walletBodies      [][]byte
	lastHash          [32]byte
	lastGeneration    uint64
}

// Real commands select their own SDK/one-shot transport; only the remote peer
// is replaced by this local authenticated protocol implementation.
func newFinancialWalletMappingHttp(t *testing.T, token, address string, coldkey [32]byte) *financialWalletMappingHttp {
	t.Helper()
	claims := gojwt.MapClaims{}
	if _, _, err := gojwt.NewParser().ParseUnverified(token, claims); err != nil {
		t.Fatal(err)
	}
	clientId, ok := claims["client_id"].(string)
	if !ok {
		t.Fatal("mapping HTTP fixture requires a provider token")
	}
	f := &financialWalletMappingHttp{t: t, token: token, address: address, coldkey: coldkey, clientId: clientId, epoch: 2, fromEpoch: 3, throughEpoch: 65538, domain: financialWalletMappingTestDomain()}
	f.endpoint = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.endpoint.Close)
	return f
}

// The only unauthenticated traffic permitted is strategy discovery. Neither a
// bootstrap token nor a generic wallet challenge can satisfy the provider flow.
func (self *financialWalletMappingHttp) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/hello" {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer "+self.token {
		self.t.Error("mapping request lost the exact provider credential", r.URL.Path)
		http.Error(w, "provider credential mismatch", http.StatusForbidden)
		return
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /sn/epoch":
		self.epochRequests++
		_ = json.NewEncoder(w).Encode(sdk.SnEpochResult{Epoch: self.epoch})
	case "POST /sn/wallet/consent":
		var args sdk.SnWalletMappingChallengeArgs
		if err := json.NewDecoder(io.LimitReader(r.Body, protocol.MaxWalletMappingMessageBytes)).Decode(&args); err != nil || args.ClientId == nil || args.ClientId.String() != self.clientId || args.ColdkeySs58 != self.address || args.FromEpoch != self.fromEpoch || args.ThroughEpoch != self.throughEpoch {
			self.t.Errorf("mapping challenge changed identity or interval: %+v, %v", args, err)
			http.Error(w, "mapping selection mismatch", http.StatusBadRequest)
			return
		}
		self.challengeRequests = append(self.challengeRequests, args)
		if self.challengeFault == "refused" {
			http.Error(w, "wallet authorization refused", http.StatusForbidden)
			return
		}
		message := financialWalletMappingTestMessage(self.t, self.token, self.coldkey, args.FromEpoch, args.ThroughEpoch, byte(len(self.challengeRequests)))
		statement, err := protocol.DecodeWalletMappingStatement(message)
		if err != nil {
			self.t.Error(err)
			http.Error(w, "fixture message", http.StatusInternalServerError)
			return
		}
		statement.Generation, statement.PreviousHash = self.lastGeneration+1, self.lastHash
		switch self.challengeFault {
		case "client":
			statement.ClientId[0] ^= 1
		case "network":
			statement.NetworkId[0] ^= 1
		case "user":
			statement.UserId[0] ^= 1
		case "coldkey":
			statement.Coldkey[0] ^= 1
		case "from":
			statement.FromEpoch++
		case "through":
			statement.ThroughEpoch--
		}
		message = financialWalletMappingTestSign(self.t, *statement)
		if self.challengeFault == "prospective-signature" {
			statement, _ = protocol.DecodeWalletMappingStatement(message)
			statement.Prospective.Signature[0] ^= 1
			raw, err := json.Marshal(statement)
			if err != nil {
				self.t.Error(err)
				return
			}
			message = protocol.WalletMappingConsentPrefix + string(raw)
		}
		self.messages = append(self.messages, message)
		_ = json.NewEncoder(w).Encode(sdk.SnWalletMappingChallengeResult{Message: message})
	case "POST /sn/wallet":
		raw, err := io.ReadAll(io.LimitReader(r.Body, protocol.MaxWalletMappingConsentBytes+1))
		if err != nil {
			self.t.Error(err)
			return
		}
		self.walletBodies = append(self.walletBodies, bytes.Clone(raw))
		var args sdk.SnSetWalletArgs
		if err := json.Unmarshal(raw, &args); err != nil || args.ColdkeySs58 != self.address || args.ClientId == nil || args.ClientId.String() != self.clientId {
			self.t.Error("wallet POST changed provider or coldkey", err)
			http.Error(w, "wallet identity", http.StatusBadRequest)
			return
		}
		original, receipt, err := financialWalletMappingTestReceipt(r.Context(), &args)
		if err != nil {
			self.t.Error("wallet POST lacks a valid original", err)
			http.Error(w, "wallet original", http.StatusBadRequest)
			return
		}
		statement, head, err := protocol.VerifyWalletMappingConsent(r.Context(), original)
		if err != nil || statement.Domain != self.domain {
			http.Error(w, "wallet consent deployment domain changed", http.StatusConflict)
			return
		}
		if self.beforePost != nil {
			self.beforePost(original)
		}
		self.lastHash, self.lastGeneration = head, statement.Generation
		if self.loseAck != nil {
			connection, _, err := w.(http.Hijacker).Hijack()
			self.loseAck()
			if err != nil {
				self.t.Error(err)
				return
			}
			_ = connection.Close()
			return
		}
		switch self.receiptFault {
		case "hash":
			receipt.MappingHash = strings.Repeat("a7", 32)
		case "generation":
			receipt.MappingGeneration++
		case "refused":
			receipt = &sdk.SnSetWalletResult{Error: &sdk.SnSetWalletError{Message: "wallet authorization refused"}}
		}
		_ = json.NewEncoder(w).Encode(receipt)
	default:
		self.t.Error("provider mapping reached an unexpected route", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

// Each invocation owns a new SDK and strategy, matching a process restart.
func financialWalletMappingTestSet(t *testing.T, ctx context.Context, endpoint, address string, proof *snWalletProof) error {
	t.Helper()
	settings := swarmWalletTestSettings()
	strategy := connect.NewClientStrategy(ctx, settings)
	defer strategy.Close()
	return snSetWallet(ctx, strategy, endpoint, address, proof)
}

// A seed and complete provider token are private files, independent of the
// network bootstrap token deliberately retained alongside them.
func financialWalletMappingTestProvider(t *testing.T) (string, *crv4.Keypair, string) {
	t.Helper()
	token := setTestProviderJwt(t)
	seed := [32]byte{153}
	key, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.Getenv("URNETWORK_STATE_DIR"), "synthetic-wallet.seed")
	if err := crv4.EnsureSeedFile(path, seed); err != nil {
		t.Fatal(err)
	}
	return token, key, path
}

// Read-only inspection is safe while the HTTP owner retains the file lock;
// decoding still checks the complete canonical signed journal and API binding.
func financialWalletMappingTestJournal(ctx context.Context, tokenPath, apiUrl string) (*snWalletConsentJournal, error) {
	directory := tokenPath + ".wallet-consent"
	for path, expected := range map[string]os.FileMode{directory: 0700, filepath.Join(directory, "history.json"): 0600} {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("wallet custody lacks private original file %s: %w", path, err)
		}
		if info.Mode().Perm() != expected || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("wallet custody file %s has mode %s", path, info.Mode())
		}
	}
	raw, err := os.ReadFile(filepath.Join(directory, "history.json"))
	if err != nil {
		return nil, err
	}
	journal, _, _, err := decodeSnWalletConsent(ctx, raw, apiUrl)
	return journal, err
}

// Retention precedes the first actual POST. Cancellation after remote receipt
// loses its acknowledgement; a new command reuses the exact randomized signature.
func TestSnWalletMappingPersistsBeforePostAndReplaysLostAcknowledgement(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	token, key, seedPath := financialWalletMappingTestProvider(t)
	f := newFinancialWalletMappingHttp(t, token, key.Address(), key.PublicKey())
	tokenPath := filepath.Join(os.Getenv("URNETWORK_STATE_DIR"), ".provider.jwt")
	firstCtx, loseAck := context.WithCancel(ctx)
	defer loseAck()
	var retainedBeforePost bool
	f.beforePost = func(original protocol.WalletMappingConsent) {
		journal, err := financialWalletMappingTestJournal(ctx, tokenPath, f.endpoint.URL)
		if err != nil || len(journal.Records) != 1 || journal.Records[0].Original != original || journal.Records[0].Acknowledged {
			t.Error("first POST preceded durable private retention of its exact original", err)
			return
		}
		retainedBeforePost = true
	}
	f.loseAck = loseAck
	proof := &snWalletProof{SeedFile: seedPath}
	if err := financialWalletMappingTestSet(t, firstCtx, f.endpoint.URL, key.Address(), proof); err == nil {
		t.Fatal("lost wallet acknowledgement became success")
	}
	f.stateLock.Lock()
	retained, challenges, posts := retainedBeforePost, len(f.challengeRequests), len(f.walletBodies)
	f.beforePost, f.loseAck = nil, nil
	f.stateLock.Unlock()
	if !retained || challenges != 1 || posts != 1 {
		t.Fatal("lost acknowledgement did not follow one retained signed POST", retained, challenges, posts)
	}
	owner, err := openSnWalletConsent(ctx, tokenPath, f.endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	original, acknowledged, loadErr := owner.load(ctx)
	closeErr := owner.close()
	if loadErr != nil || closeErr != nil || original == nil || acknowledged {
		t.Fatal("restart lost its pending original", acknowledged, loadErr, closeErr)
	}
	// A fresh signature is impossible on restart; the private original must
	// suffice without reopening the removed seed or acquiring another challenge.
	if err := os.Remove(seedPath); err != nil {
		t.Fatal(err)
	}
	if err := financialWalletMappingTestSet(t, ctx, f.endpoint.URL, key.Address(), proof); err != nil {
		t.Fatal("retained consent did not survive restart", err)
	}
	f.stateLock.Lock()
	challenges, posts = len(f.challengeRequests), len(f.walletBodies)
	sameBody := posts == 2 && bytes.Equal(f.walletBodies[0], f.walletBodies[1])
	f.stateLock.Unlock()
	if challenges != 1 || posts != 2 || !sameBody {
		t.Fatal("restart changed nonce, signature, POST bytes or fetched another challenge", challenges, posts, sameBody)
	}
	journal, err := financialWalletMappingTestJournal(ctx, tokenPath, f.endpoint.URL)
	if err != nil || len(journal.Records) != 1 || !journal.Records[0].Acknowledged || journal.Records[0].Original != *original {
		t.Fatal("exact accepted replay did not acknowledge the retained original", err)
	}
}

// A prior local acknowledgement never authenticates today's endpoint. Actual
// replay must reject altered receipt fields and an independently changed domain.
func TestSnWalletMappingAcknowledgedReplayReconcilesCurrentServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for _, fault := range []string{"hash", "generation", "domain"} {
		token, key, seedPath := financialWalletMappingTestProvider(t)
		f := newFinancialWalletMappingHttp(t, token, key.Address(), key.PublicKey())
		proof := &snWalletProof{SeedFile: seedPath}
		if err := financialWalletMappingTestSet(t, ctx, f.endpoint.URL, key.Address(), proof); err != nil {
			t.Fatal(fault, "initial consent failed", err)
		}
		f.stateLock.Lock()
		if fault == "domain" {
			f.domain.DeploymentIDHash[0] ^= 1
		} else {
			f.receiptFault = fault
		}
		f.stateLock.Unlock()
		if err := financialWalletMappingTestSet(t, ctx, f.endpoint.URL, key.Address(), proof); err == nil {
			t.Fatal(fault, "cached acknowledgement bypassed the current server")
		}
		f.stateLock.Lock()
		challenges, posts := len(f.challengeRequests), len(f.walletBodies)
		sameBody := posts == 2 && bytes.Equal(f.walletBodies[0], f.walletBodies[1])
		f.receiptFault, f.domain = "", financialWalletMappingTestDomain()
		f.stateLock.Unlock()
		if challenges != 1 || posts != 2 || !sameBody {
			t.Fatal(fault, "acknowledged retry did not reconcile the exact original", challenges, posts, sameBody)
		}
		if err := financialWalletMappingTestSet(t, ctx, f.endpoint.URL, key.Address(), proof); err != nil {
			t.Fatal(fault, "current original could not be acknowledged after the refusal", err)
		}
		f.stateLock.Lock()
		challenges, posts = len(f.challengeRequests), len(f.walletBodies)
		sameBody = posts == 3 && bytes.Equal(f.walletBodies[0], f.walletBodies[2])
		f.stateLock.Unlock()
		if challenges != 1 || posts != 3 || !sameBody {
			t.Fatal(fault, "receipt refusal replaced the original", challenges, posts, sameBody)
		}
	}
}

// Every identity and interval fault remains canonical under a real test root;
// only the malformed-signature case breaks prospective cryptographic integrity.
func TestSnWalletMappingRefusesChangedChallengeBeforePost(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for _, fault := range []string{"client", "network", "user", "coldkey", "from", "through", "prospective-signature"} {
		token, key, seedPath := financialWalletMappingTestProvider(t)
		f := newFinancialWalletMappingHttp(t, token, key.Address(), key.PublicKey())
		f.challengeFault = fault
		if err := financialWalletMappingTestSet(t, ctx, f.endpoint.URL, key.Address(), &snWalletProof{SeedFile: seedPath}); err == nil {
			t.Fatal(fault, "changed prospective statement acquired a wallet signature")
		}
		f.stateLock.Lock()
		challenges, posts := len(f.challengeRequests), len(f.walletBodies)
		f.stateLock.Unlock()
		if challenges != 1 || posts != 0 {
			t.Fatal(fault, "changed prospective challenge reached a signed POST", challenges, posts)
		}
		journal, err := financialWalletMappingTestJournal(ctx, filepath.Join(os.Getenv("URNETWORK_STATE_DIR"), ".provider.jwt"), f.endpoint.URL)
		if err != nil || len(journal.Records) != 0 {
			t.Fatal(fault, "changed challenge retained a signed original", err)
		}
	}
}

// Explicit earning bounds bypass epoch discovery and survive the exact signed
// producer request, original retention and current acknowledgement unchanged.
func TestSnWalletMappingSignsExplicitEarningInterval(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	token, key, seedPath := financialWalletMappingTestProvider(t)
	f := newFinancialWalletMappingHttp(t, token, key.Address(), key.PublicKey())
	from, through := int64(11), int64(13)
	f.fromEpoch, f.throughEpoch = from, through
	if err := financialWalletMappingTestSet(t, ctx, f.endpoint.URL, key.Address(), &snWalletProof{SeedFile: seedPath, fromEpoch: &from, throughEpoch: &through}); err != nil {
		t.Fatal(err)
	}
	f.stateLock.Lock()
	epochs, challenges, posts := f.epochRequests, len(f.challengeRequests), len(f.walletBodies)
	f.stateLock.Unlock()
	if epochs != 0 || challenges != 1 || posts != 1 {
		t.Fatal("explicit earning interval was replaced by epoch discovery", epochs, challenges, posts)
	}
	journal, err := financialWalletMappingTestJournal(ctx, filepath.Join(os.Getenv("URNETWORK_STATE_DIR"), ".provider.jwt"), f.endpoint.URL)
	if err != nil || len(journal.Records) != 1 || !journal.Records[0].Acknowledged {
		t.Fatal("explicit interval lost its acknowledged original", err)
	}
	statement, _, err := protocol.VerifyWalletMappingConsent(ctx, journal.Records[0].Original)
	if err != nil || statement.FromEpoch != uint64(from) || statement.ThroughEpoch != uint64(through) {
		t.Fatal("explicit earning bounds changed in original custody", statement, err)
	}
}

// Externally signed canonical consent uses its exact original message and
// signature without requesting another challenge or signing through a seed.
func TestSnWalletMappingPreservesExternallySignedOriginal(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	token, key, _ := financialWalletMappingTestProvider(t)
	f := newFinancialWalletMappingHttp(t, token, key.Address(), key.PublicKey())
	message := financialWalletMappingTestMessage(t, token, key.PublicKey(), 3, 20, 9)
	signature, err := snSignWalletChallenge(key, message)
	if err != nil {
		t.Fatal(err)
	}
	if err := financialWalletMappingTestSet(t, ctx, f.endpoint.URL, key.Address(), &snWalletProof{Message: snEscapeMessage(message), Signature: signature}); err != nil {
		t.Fatal(err)
	}
	f.stateLock.Lock()
	epochs, challenges, posts := f.epochRequests, len(f.challengeRequests), len(f.walletBodies)
	var received sdk.SnSetWalletArgs
	if posts == 1 {
		err = json.Unmarshal(f.walletBodies[0], &received)
	}
	f.stateLock.Unlock()
	if err != nil || epochs != 0 || challenges != 0 || posts != 1 || received.Message != message || received.Signature != signature {
		t.Fatal("external original was normalized, replaced or signed again", epochs, challenges, posts, err)
	}
}

// Default provider commands never fall back to generic login or unsigned
// network-wallet behavior, even when the supplied login signature is valid.
func TestSnWalletMappingDefaultProviderRefusesGenericAndUnsignedProof(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	token, key, _ := financialWalletMappingTestProvider(t)
	f := newFinancialWalletMappingHttp(t, token, key.Address(), key.PublicKey())
	signature, err := snSignWalletChallenge(key, testChallengeMessage)
	if err != nil {
		t.Fatal(err)
	}
	rawSignature, err := hex.DecodeString(strings.TrimPrefix(signature, "0x"))
	if err != nil || !key.Verify(snWrapBytes(testChallengeMessage), rawSignature) {
		t.Fatal("generic-proof refusal fixture lacks a valid coldkey signature", err)
	}
	for _, proof := range []*snWalletProof{nil, {Message: testChallengeMessage, Signature: signature}} {
		if err := financialWalletMappingTestSet(t, ctx, f.endpoint.URL, key.Address(), proof); err == nil {
			t.Fatal("default provider accepted a generic or unsigned wallet proof")
		}
	}
	f.stateLock.Lock()
	epochs, challenges, posts := f.epochRequests, len(f.challengeRequests), len(f.walletBodies)
	f.stateLock.Unlock()
	if epochs != 0 || challenges != 0 || posts != 0 {
		t.Fatal("default provider sent a generic or unsigned proof", epochs, challenges, posts)
	}
}

// The swarm's source-pinned one-shot transport performs the same durable
// handoff and sends identical bytes on the next actual member invocation.
func TestSetSwarmMemberWalletReplaysLostAcknowledgementActualHttp(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	config := validProviderSwarmConfig(t)
	member := config.Members[0]
	tokenPath := filepath.Join(member.StateDir, ".provider.jwt")
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := swarmMemberWalletKey(member)
	if err != nil {
		t.Fatal(err)
	}
	f := newFinancialWalletMappingHttp(t, string(token), member.Wallet, key.PublicKey())
	member.APIURL = f.endpoint.URL
	firstCtx, loseAck := context.WithCancel(ctx)
	defer loseAck()
	var retainedBeforePost bool
	f.beforePost = func(original protocol.WalletMappingConsent) {
		journal, err := financialWalletMappingTestJournal(ctx, tokenPath, member.APIURL)
		if err != nil || len(journal.Records) != 1 || journal.Records[0].Original != original || journal.Records[0].Acknowledged {
			t.Error("swarm POST preceded exact original retention", err)
			return
		}
		retainedBeforePost = true
	}
	f.loseAck = loseAck
	if err := setSwarmMemberWallet(firstCtx, member, swarmWalletTestSettings()); err == nil {
		t.Fatal("lost swarm acknowledgement became success")
	}
	f.stateLock.Lock()
	retained, challenges, posts := retainedBeforePost, len(f.challengeRequests), len(f.walletBodies)
	f.beforePost, f.loseAck = nil, nil
	f.stateLock.Unlock()
	if !retained || challenges != 1 || posts != 1 {
		t.Fatal("swarm lost acknowledgement did not follow exactly one retained POST", retained, challenges, posts)
	}
	if err := os.Remove(member.WalletSeedFile); err != nil {
		t.Fatal(err)
	}
	config.Members[0] = member
	if err := config.Validate(); err != nil {
		t.Fatal("swarm configuration blocked retained consent recovery without a seed", err)
	}
	if err := setSwarmMemberWallet(ctx, member, swarmWalletTestSettings()); err != nil {
		t.Fatal("swarm did not replay its retained original", err)
	}
	f.stateLock.Lock()
	challenges, posts = len(f.challengeRequests), len(f.walletBodies)
	sameBody := posts == 2 && bytes.Equal(f.walletBodies[0], f.walletBodies[1])
	f.stateLock.Unlock()
	if challenges != 1 || posts != 2 || !sameBody {
		t.Fatal("swarm replay changed nonce, signature or request bytes", challenges, posts, sameBody)
	}
}
