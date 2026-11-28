// Submission tests use synthetic independent approvals/native keys and local
// http fixtures. Explicit barriers force uncertain-send and durability ordering.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

// One fixture owns a complete canonical reader and a distinct mutation route.
// Server state is either configured before use or guarded by stateLock.
type rootSubmissionFixture struct {
	config    rootSubmissionConfig
	intent    rootServiceSubmission
	offline   rootOfflineFixture
	receipt   *rootReceiptFixture
	authority *rootAuthorityFixture
	stateLock sync.Mutex
	writes    []string
	respond   func(http.ResponseWriter, *http.Request)
	inspect   func()
}

// Route approval is separate from the already signed native-action approval.
func (self *rootSubmissionFixture) approve(t *testing.T) {
	t.Helper()
	message, err := self.config.Approval.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	self.config.Approval.Signature = hex.EncodeToString(ed25519.Sign(self.offline.approvalKey, message))
}

// A locked copy provides a deterministic observation of handler-owned writes.
func (self *rootSubmissionFixture) sent() []string {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]string(nil), self.writes...)
}

// The local server delegates all reads to the real canonical receipt fixture.
func newRootSubmissionFixture(t *testing.T) *rootSubmissionFixture {
	t.Helper()
	_, receipt := newRootReceiptFixture(t, 2, false)
	offline := newRootOfflineFixture(t)
	packet := rootOfflineApprove(t, offline.trust, receipt.action, offline.approvalKey)
	service := rootServiceConfig{Schema: rootServiceConfigSchema, CustodyTrust: offline.trust, Packet: packet, MaximumObservations: 3}
	fixture := &rootSubmissionFixture{receipt: receipt, offline: offline, authority: &rootAuthorityFixture{}}
	fixture.offline.packet = packet
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(io.LimitReader(request.Body, 128*1024))
		if err != nil {
			http.Error(writer, "synthetic request failure", 400)
			return
		}
		request.Body.Close()
		request.Body = io.NopCloser(bytes.NewReader(body))
		var call struct {
			Method string   `json:"method"`
			Params []string `json:"params"`
		}
		// Reads include numeric parameters; only the mutation envelope needs strings.
		json.Unmarshal(body, &call)
		if call.Method != "author_submitExtrinsic" {
			receipt.serve(writer, request)
			return
		}
		if len(call.Params) != 1 {
			http.Error(writer, "synthetic wrong parameters", 400)
			return
		}
		var respond func(http.ResponseWriter, *http.Request)
		var inspect func()
		func() {
			fixture.stateLock.Lock()
			defer fixture.stateLock.Unlock()
			fixture.writes = append(fixture.writes, call.Params[0])
			respond, inspect = fixture.respond, fixture.inspect
		}()
		if inspect != nil {
			inspect()
		}
		if respond != nil {
			respond(writer, request)
			return
		}
		raw, _ := hex.DecodeString(call.Params[0][2:])
		json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": rootExtrinsicHash(raw)})
	}))
	t.Cleanup(server.Close)
	fixture.config = rootSubmissionConfig{Schema: rootSubmissionConfigSchema, Service: service, Approval: rootSubmissionApproval{
		Schema: rootSubmissionApprovalSchema, ServiceConfigHash: rootObjectHash(service), PacketHash: packet.ContentHash, RpcUrl: server.URL,
		StatePath: filepath.Join(filepath.Dir(offline.trust.StatePath), "submission.json"), ReadRetrySeconds: 60, SendTimeoutSeconds: 10,
	}}
	fixture.approve(t)
	fixture.intent = rootServiceSubmission{Packet: packet, ConfigHash: rootObjectHash(service), Attempt: 1, RawExtrinsic: "0x" + hex.EncodeToString(receipt.signed), ExtrinsicHash: rootExtrinsicHash(receipt.signed)}
	prepareMainnetSnapshotTest(t, fixture.config.Approval.StatePath, "mainnet-root-submission", rootSubmissionStoreLimit)
	return fixture
}

// Every opened fixture is closed after all operations and handlers are joined.
func (self *rootSubmissionFixture) open(t *testing.T, create bool) (*rootOwnedSubmission, *rootSubmissionStore) {
	t.Helper()
	store, err := openRootSubmissionStore(self.config, create, self.offline.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.close() })
	owner, err := newRootOwnedSubmission(self.config, store, self.authority)
	if err != nil {
		t.Fatal(err)
	}
	return owner, store
}

// Produce a canonical successor containing the exact submitted bytes and fee.
func (self *rootSubmissionFixture) finalize(t *testing.T, raw []byte) {
	t.Helper()
	receipt := self.receipt
	header, hash := rootReceiptHeaderFixture(t, receipt.finalized, receipt.action.BirthBlock+3, [][]byte{{8, 4, 0}, raw}, false)
	key, err := types.CreateStorageKey(receipt.metadata, "System", "Account", mustRootAccountBytes(t, receipt.action.Scope.Hotkey))
	if err != nil {
		t.Fatal(err)
	}
	account := make([]byte, 56)
	binary.LittleEndian.PutUint32(account, receipt.action.Nonce+1)
	receipt.stateLock.Lock()
	defer receipt.stateLock.Unlock()
	receipt.headers[hash], receipt.byHeight[receipt.action.BirthBlock+3], receipt.finalized = header, hash, hash
	receipt.bodies[hash] = []string{"0x080400", "0x" + hex.EncodeToString(raw)}
	receipt.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(account)
}

// Decode only synthetic canonical account identifiers used by the fixtures.
func mustRootAccountBytes(t *testing.T, value string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(value[2:])
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Actual http writes cannot occur before exact raw bytes and the uncertain
// attempt are synced. An acknowledgement alone remains nonterminal.
func TestRootSubmissionDurableAttemptBeforeHttp(t *testing.T) {
	fixture := newRootSubmissionFixture(t)
	owner, store := fixture.open(t, true)
	fixture.inspect = func() {
		record, err := store.load()
		if err != nil || record.RawExtrinsic != fixture.intent.RawExtrinsic || len(record.Attempts) != 1 || record.Attempts[0].Phase != "uncertain" || record.Reconciliation == nil {
			t.Errorf("http reached server before durable exact intent: %+v %v", record, err)
		}
	}
	if err := owner.submitRoot(context.Background(), fixture.intent); err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil || len(fixture.sent()) != 1 || record.Attempts[0].Phase != "acknowledged" || record.Attempts[0].ReturnedHash != fixture.intent.ExtrinsicHash || rootTerminalPhase(fixture.intent.Packet.Action, *record.Reconciliation) != "" {
		t.Fatal("acknowledgement became terminal or lost exact attempt", err)
	}
	fixture.stateLock.Lock()
	fixture.inspect = nil
	fixture.stateLock.Unlock()
	if err := owner.submitRoot(context.Background(), fixture.intent); err != nil || len(fixture.sent()) != 1 {
		t.Fatal("same numbered acknowledgement was sent twice", err)
	}
}

// A lost response can mean accepted. Reopening cannot resend that attempt;
// a separately counted next attempt reuses only the original signed bytes.
func TestRootSubmissionLostResponseAndBoundedReplay(t *testing.T) {
	fixture := newRootSubmissionFixture(t)
	fixture.respond = func(writer http.ResponseWriter, _ *http.Request) {
		connection, _, err := writer.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	}
	owner, store := fixture.open(t, true)
	if err := owner.submitRoot(context.Background(), fixture.intent); !errors.Is(err, errRootSubmissionUncertain) || len(fixture.sent()) != 1 {
		t.Fatal("dropped reply lost uncertainty", err)
	}
	store.close()
	owner, store = fixture.open(t, false)
	if err := owner.submitRoot(context.Background(), fixture.intent); !errors.Is(err, errRootSubmissionUncertain) || len(fixture.sent()) != 1 {
		t.Fatal("restart replayed an uncertain numbered attempt", err)
	}
	fixture.stateLock.Lock()
	fixture.respond = nil
	fixture.stateLock.Unlock()
	fixture.intent.Attempt = 2
	if err := owner.submitRoot(context.Background(), fixture.intent); err != nil || len(fixture.sent()) != 2 || fixture.sent()[0] != fixture.sent()[1] {
		t.Fatal("fresh numbered attempt did not retain identical signed bytes", err)
	}
	fixture.intent.Attempt = 3
	if err := owner.submitRoot(context.Background(), fixture.intent); err == nil || len(fixture.sent()) != 2 {
		t.Fatal("submission exceeded its original approval")
	}
	fixture.finalize(t, fixture.receipt.signed)
	owner.authority = nil
	result, err := owner.reconcile(context.Background(), fixture.config.Service.Packet.Action, fixture.receipt.signed)
	if err != nil || result.Receipt == nil || result.Receipt.ActualFeeRao != 12 {
		t.Fatalf("old receipt was not retained independently of authority: %+v %v", result, err)
	}
	store.close()
	owner, _ = fixture.open(t, false)
	fixture.receipt.stateLock.Lock()
	fixture.receipt.evmHex = "0x3b1"
	fixture.receipt.stateLock.Unlock()
	result, err = owner.reconcile(context.Background(), fixture.config.Service.Packet.Action, fixture.receipt.signed)
	if err != nil || result.Receipt == nil || len(fixture.sent()) != 2 {
		t.Fatal("terminal receipt disappeared after route changed", err)
	}
}

// Bad acknowledgements and overload never trigger a transport retry or release
// the attempt, even when the node explicitly reports a pool-level error.
func TestRootSubmissionReplyFailuresNeverRetry(t *testing.T) {
	for _, failure := range []string{"502", "redirect", "rpc-error", "wrong-hash", "duplicate", "folded-duplicate", "wrong-id", "oversize", "null"} {
		fixture := newRootSubmissionFixture(t)
		fixture.respond = func(writer http.ResponseWriter, _ *http.Request) {
			switch failure {
			case "502":
				writer.WriteHeader(502)
			case "redirect":
				writer.Header().Set("Location", fixture.config.Approval.RpcUrl+"/unapproved")
				writer.WriteHeader(307)
			case "rpc-error":
				io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"error":{"code":1013,"message":"synthetic already imported"}}`)
			case "wrong-hash":
				json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x" + strings.Repeat("ab", 32)})
			case "duplicate":
				io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"id":1,"result":"`+fixture.intent.ExtrinsicHash+`"}`)
			case "folded-duplicate":
				io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"ID":1,"result":"`+fixture.intent.ExtrinsicHash+`"}`)
			case "wrong-id":
				json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "result": fixture.intent.ExtrinsicHash})
			case "oversize":
				io.WriteString(writer, strings.Repeat("x", 64*1024+1))
			case "null":
				io.WriteString(writer, `{"jsonrpc":"2.0","id":1,"result":null}`)
			}
		}
		owner, store := fixture.open(t, true)
		if err := owner.submitRoot(context.Background(), fixture.intent); !errors.Is(err, errRootSubmissionUncertain) || len(fixture.sent()) != 1 {
			t.Fatalf("%s lost uncertainty or retried: %v", failure, err)
		}
		record, err := store.load()
		if err != nil || record.Attempts[0].Phase != "uncertain" {
			t.Fatalf("%s changed durable outcome: %v", failure, err)
		}
	}
}

// Independent action approval cannot authorize a new route, timeout, journal,
// service or attempt. The native signed call is also checked before any read.
func TestRootSubmissionExactIndependentAdmission(t *testing.T) {
	fixture := newRootSubmissionFixture(t)
	for _, change := range []func(*rootSubmissionConfig){
		func(config *rootSubmissionConfig) { config.Approval.RpcUrl += "/changed" },
		func(config *rootSubmissionConfig) { config.Approval.SendTimeoutSeconds++ },
		func(config *rootSubmissionConfig) { config.Approval.StatePath += ".changed" },
		func(config *rootSubmissionConfig) {
			config.Approval.Signature = config.Service.Packet.Approval.Signature
		},
		func(config *rootSubmissionConfig) { config.Service.MaximumObservations++ },
		func(config *rootSubmissionConfig) { config.Service.CustodyTrust.EvmChainId = 945 },
	} {
		config := copyRootSubmissionConfig(fixture.config)
		change(&config)
		if config.validate() == nil {
			t.Fatal("changed independent submission approval passed")
		}
	}
	owner, _ := fixture.open(t, true)
	for _, change := range []func(*rootServiceSubmission){
		func(intent *rootServiceSubmission) { intent.Attempt = 0 },
		func(intent *rootServiceSubmission) { intent.ConfigHash = rootObjectHash("another service") },
		func(intent *rootServiceSubmission) { intent.ExtrinsicHash = "0x" + strings.Repeat("ab", 32) },
		func(intent *rootServiceSubmission) {
			intent.RawExtrinsic = "0x00"
			intent.ExtrinsicHash = rootExtrinsicHash([]byte{0})
		},
		func(intent *rootServiceSubmission) { intent.Packet.Action.Nonce++ },
	} {
		intent := fixture.intent
		intent.Packet.Action = copyRootAction(intent.Packet.Action)
		change(&intent)
		if err := owner.submitRoot(context.Background(), intent); err == nil {
			t.Fatal("changed submission intent passed")
		}
	}
	if len(fixture.sent()) != 0 || fixture.receipt.counts["chain_getFinalizedHead"] != 0 {
		t.Fatal("invalid action admission reached transport")
	}
}

// A retained original may not be replaced by another valid sr25519 signature
// of the same approved payload. Skipped service attempts cannot later be refilled.
func TestRootSubmissionOriginalBytesAndSkippedAttempts(t *testing.T) {
	fixture := newRootSubmissionFixture(t)
	fixture.intent.Attempt = 2
	owner, _ := fixture.open(t, true)
	if err := owner.submitRoot(context.Background(), fixture.intent); err != nil {
		t.Fatal(err)
	}
	other := fixture.offline.receipt(t)
	signature, _ := hex.DecodeString(other.Signature)
	raw, err := fixture.config.Service.Packet.Action.signed(signature)
	if err != nil || bytes.Equal(raw, fixture.receipt.signed) {
		t.Fatal("synthetic alternate native signature was not distinct", err)
	}
	changed := fixture.intent
	changed.RawExtrinsic, changed.ExtrinsicHash = "0x"+hex.EncodeToString(raw), rootExtrinsicHash(raw)
	if err := owner.submitRoot(context.Background(), changed); err == nil {
		t.Fatal("valid alternate signature replaced original bytes")
	}
	fixture.intent.Attempt = 1
	if err := owner.submitRoot(context.Background(), fixture.intent); err == nil || len(fixture.sent()) != 1 {
		t.Fatal("skipped numbered attempt replenished allowance", err)
	}
}

// IP route construction refuses credentials, redirection, implicit IP ports
// and TLS trust shortcuts. Public DNS routes separately require pinned HTTPS.
func TestRootSubmissionRouteAndTlsBoundary(t *testing.T) {
	fixture := newRootSubmissionFixture(t)
	for _, route := range []string{"https://owned-node.example:443", "http://192.0.2.10", "http://192.0.2.10:0", "http://user@192.0.2.10:9944", "http://192.0.2.10:9944?secret=x", "http://192.0.2.10:9944#fragment", "http://0.0.0.0:9944", "http://192.0.2.10:09944", "http://192.0.2.10:9944/%2fother"} {
		approval := fixture.config.Approval
		approval.RpcUrl = route
		if rootSubmissionRoute(approval) == nil {
			t.Fatal("unbound route accepted", route)
		}
	}
	approval := fixture.config.Approval
	approval.RpcUrl = "https://192.0.2.10:443"
	approval.TlsSpkiHash = rootObjectHash("synthetic certificate pin")
	client, err := newRootSubmissionClient(approval)
	if err != nil {
		t.Fatal(err)
	}
	transport := client.httpClient.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.TLSClientConfig.InsecureSkipVerify || !transport.DisableKeepAlives || transport.TLSClientConfig.VerifyConnection(tls.ConnectionState{}) == nil ||
		transport.TLSClientConfig.VerifyConnection(tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}, PeerCertificates: []*x509.Certificate{{RawSubjectPublicKeyInfo: []byte("synthetic other certificate")}}}) == nil {
		t.Fatal("transport weakens approved route or tls checks")
	}
	if _, err := transport.DialContext(context.Background(), "tcp", "192.0.2.11:443"); err == nil {
		t.Fatal("transport dialed another endpoint")
	}
	var result string
	if err := client.call(context.Background(), "author_submitExtrinsic", []any{fixture.intent.RawExtrinsic}, &result); err == nil {
		t.Fatal("read-only whitelist acquired mutation capability")
	}
}

// Durability faults can occur before or after an attempt/acknowledgement commit.
// The open owner must stop; reopening never repeats a persisted numbered write.
type rootSubmissionFaultStore struct {
	store  rootSubmissionStorage
	writes int
	failAt int
	after  bool
}

// Reads preserve the actual durable journal across the injected fault.
func (self *rootSubmissionFaultStore) load() (rootSubmissionRecord, error) {
	return self.store.load()
}

// The chosen write is either omitted or committed before returning ambiguity.
func (self *rootSubmissionFaultStore) save(record rootSubmissionRecord) error {
	self.writes++
	if self.writes == self.failAt {
		if self.after {
			if err := self.store.save(record); err != nil {
				return err
			}
		}
		return errors.New("synthetic ambiguous submission journal write")
	}
	return self.store.save(record)
}

// Binding bytes, canonical read, attempt reservation and response persistence
// are separate durable boundaries; no response loss creates another write.
func TestRootSubmissionAmbiguousJournalWrites(t *testing.T) {
	for _, failAt := range []int{3, 4} {
		for _, after := range []bool{false, true} {
			fixture := newRootSubmissionFixture(t)
			_, store := fixture.open(t, true)
			fault := &rootSubmissionFaultStore{store: store, failAt: failAt, after: after}
			owner, err := newRootOwnedSubmission(fixture.config, fault, fixture.authority)
			if err != nil {
				t.Fatal(err)
			}
			if err := owner.submitRoot(context.Background(), fixture.intent); err == nil || !owner.poisoned {
				t.Fatal("ambiguous write did not poison owner", failAt, after, err)
			}
			before := len(fixture.sent())
			if err := owner.submitRoot(context.Background(), fixture.intent); err == nil || len(fixture.sent()) != before {
				t.Fatal("poisoned submission owner continued")
			}
			store.close()
			owner, _ = fixture.open(t, false)
			err = owner.submitRoot(context.Background(), fixture.intent)
			want := before
			if failAt == 3 && !after {
				want++
			}
			if len(fixture.sent()) != want || failAt == 3 && !after && err != nil {
				t.Fatal("reopen duplicated a persisted attempt", failAt, after, err)
			}
		}
	}
}

// Cancellation after the server receives the body joins the client request and
// retains uncertainty. Concurrent duplicate callers cannot write the same id.
func TestRootSubmissionCancellationAndConcurrentOwnership(t *testing.T) {
	fixture := newRootSubmissionFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	fixture.respond = func(writer http.ResponseWriter, request *http.Request) {
		close(entered)
		<-release
		writer.WriteHeader(502)
	}
	owner, store := fixture.open(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- owner.submitRoot(ctx, fixture.intent) }()
	<-entered
	waitCtx, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	if err := owner.submitRoot(waitCtx, fixture.intent); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled waiter entered submission", err)
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal("active request did not join cancellation", err)
	}
	close(release)
	record, err := store.load()
	if err != nil || record.Attempts[0].Phase != "uncertain" {
		t.Fatal("canceled send lost durable uncertainty", err)
	}
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			if err := owner.submitRoot(context.Background(), fixture.intent); !errors.Is(err, errRootSubmissionUncertain) {
				t.Error("concurrent duplicate changed uncertainty", err)
			}
		})
	}
	workers.Wait()
	if len(fixture.sent()) != 1 {
		t.Fatal("concurrent duplicate calls repeated transport")
	}
}

// Loss, duplicate keys, rehashing a changed approval and unsafe files cannot
// reset submission ownership. The marker remains a lifetime local obligation.
func TestRootSubmissionStoreRejectsReplacementAndUnsafeFiles(t *testing.T) {
	fixture := newRootSubmissionFixture(t)
	_, store := fixture.open(t, true)
	if _, err := openRootSubmissionStore(fixture.config, false, fixture.offline.storage.Context); err == nil {
		t.Fatal("concurrent journal owner admitted")
	}
	store.close()
	path := fixture.config.Approval.StatePath
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record rootSubmissionRecord
	json.Unmarshal(raw, &record)
	record.Config.Approval.RpcUrl += "/another"
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	changed, _ := json.Marshal(record)
	for _, invalid := range [][]byte{nil, changed, append([]byte(`{"SCHEMA":"duplicate",`), raw[1:]...), append(raw, []byte(` {}`)...)} {
		if err := os.WriteFile(path, invalid, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootSubmissionStore(fixture.config, false, fixture.offline.storage.Context); err == nil {
			t.Fatal("invalid submission journal reopened")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := openRootSubmissionStore(fixture.config, true, fixture.offline.storage.Context); err == nil {
		t.Fatal("missing journal reset retained allowance")
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", ".lock"} {
		file := path + suffix
		original, _ := os.ReadFile(file)
		if err := os.Chmod(file, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootSubmissionStore(fixture.config, false, fixture.offline.storage.Context); err == nil {
			t.Fatal("public file admitted")
		}
		os.Remove(file)
		if err := syscall.Mkfifo(file, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootSubmissionStore(fixture.config, false, fixture.offline.storage.Context); err == nil {
			t.Fatal("special file admitted")
		}
		os.Remove(file)
		if err := os.Symlink("synthetic-missing-state", file); err != nil {
			t.Fatal(err)
		}
		if _, err := openRootSubmissionStore(fixture.config, false, fixture.offline.storage.Context); err == nil {
			t.Fatal("symlink admitted")
		}
		os.Remove(file)
		if err := os.WriteFile(file, original, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// An injected current-authority hook changes route identity at a precise point.
type rootSubmissionAuthorityFunc func(context.Context, rootAction, rootActionObservation) error

// The hook represents independent admission, never observation-derived authority.
func (self rootSubmissionAuthorityFunc) authorize(ctx context.Context, action rootAction, observation rootActionObservation) error {
	return self(ctx, action, observation)
}

// Current network/runtime/seat/nonce and authority are rechecked independently
// before every new send; no old approval or local acknowledgement bypasses them.
func TestRootSubmissionCurrentIdentityAndAuthority(t *testing.T) {
	for _, failure := range []string{"network", "runtime", "generation", "nonce", "absent-authority", "revoked-authority", "final-network", "canceled-authority"} {
		fixture := newRootSubmissionFixture(t)
		owner, store := fixture.open(t, true)
		ctx, cancel := context.WithCancel(context.Background())
		switch failure {
		case "network":
			fixture.receipt.evmHex = "0x3b1"
		case "runtime":
			fixture.receipt.profile.RuntimeCodeHash = "0x" + strings.Repeat("ab", 32)
		case "generation":
			uid := binary.LittleEndian.AppendUint16(nil, fixture.intent.Packet.Action.Scope.Seat.Uid)
			key, err := types.CreateStorageKey(fixture.receipt.metadata, "SubtensorModule", "BlockAtRegistration", []byte{0, 0}, uid)
			if err != nil {
				t.Fatal(err)
			}
			fixture.receipt.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(binary.LittleEndian.AppendUint64(nil, 12))
		case "nonce":
			key, err := types.CreateStorageKey(fixture.receipt.metadata, "System", "Account", mustRootAccountBytes(t, fixture.intent.Packet.Action.Scope.Hotkey))
			if err != nil {
				t.Fatal(err)
			}
			account := make([]byte, 56)
			binary.LittleEndian.PutUint32(account, fixture.intent.Packet.Action.Nonce+1)
			fixture.receipt.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(account)
		case "absent-authority":
			owner.authority = nil
		case "revoked-authority":
			fixture.authority.err = errors.New("synthetic current fence revoked")
		case "final-network":
			owner.authority = rootSubmissionAuthorityFunc(func(context.Context, rootAction, rootActionObservation) error {
				fixture.receipt.stateLock.Lock()
				fixture.receipt.evmHex = "0x3b1"
				fixture.receipt.stateLock.Unlock()
				return nil
			})
		case "canceled-authority":
			owner.authority = rootSubmissionAuthorityFunc(func(context.Context, rootAction, rootActionObservation) error { cancel(); return nil })
		}
		if err := owner.submitRoot(ctx, fixture.intent); err == nil {
			t.Fatal("changed current submission admission passed", failure)
		}
		cancel()
		record, err := store.load()
		if err != nil || len(record.Attempts) != 0 || len(fixture.sent()) != 0 {
			t.Fatal("failed current admission consumed a write attempt", failure, err)
		}
	}
}

// The actual offline custody receipt, service journal, canonical reader and
// owned submitter compose without a native secret or network outside fixtures.
func TestRootSubmissionOfflineCustodyServiceComposition(t *testing.T) {
	fixture := newRootSubmissionFixture(t)
	submitter, submissionStore := fixture.open(t, true)
	custody, _ := fixture.offline.open(t, true)
	receipt := fixture.offline.receipt(t)
	if err := custody.importSignature(context.Background(), receipt); err != nil {
		t.Fatal(err)
	}
	action := fixture.config.Service.Packet.Action
	position, err := submitter.reconcile(context.Background(), action, nil)
	if err != nil {
		t.Fatal(err)
	}
	view := rootWeightObservation{Schema: rootWeightObservationSchema, Position: position.Observation, Enabled: true, ActiveNetworks: []uint16{0, 1, 2, 3, 4, 5, 6, 7}, StoredWeights: []rootStoredWeight{}, ConcentrationCap: 4096, StorageHash: rootObjectHash("synthetic complete root observation")}
	serviceStorage := durablefixture.New(t, t.Context(), filepath.Dir(action.Scope.StatePath))
	prepareMainnetSnapshotTest(t, action.Scope.StatePath, "mainnet-root-service", rootServiceStoreLimit)
	serviceStore, err := openRootServiceStore(fixture.config.Service, true, serviceStorage.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer serviceStore.close()
	owner, err := newRootServiceOwner(fixture.config.Service, serviceStore, rootServicePorts{Observer: &rootServiceObserverFixture{view: view}, Reconciler: submitter, Authority: fixture.authority, Signer: custody, Submitter: submitter})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := owner.step(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(fixture.sent()) != 1 {
		t.Fatal("composed service did not issue exactly one approved local request")
	}
	stored, err := submissionStore.load()
	if err != nil || stored.RawExtrinsic != fixture.sent()[0] {
		t.Fatal("service and submission journals disagree", err)
	}
	raw, _ := hex.DecodeString(stored.RawExtrinsic[2:])
	fixture.finalize(t, raw)
	owner.ports.Authority, owner.ports.Signer, owner.ports.Submitter, submitter.authority = nil, nil, nil, nil
	result, err := owner.step(context.Background())
	if err != nil || result.Phase != "complete" || result.Action.Phase != "finalized" || result.ActivationReady || len(fixture.sent()) != 1 {
		t.Fatalf("composed service lost old receipt or created activation authority: %+v %v", result, err)
	}
	stored, err = submissionStore.load()
	if err != nil || stored.Reconciliation == nil || stored.Reconciliation.Receipt == nil {
		t.Fatal("submission journal did not retain canonical terminal evidence", err)
	}
}

// Even fully authenticated individual snapshots cannot move this owner's
// retained finalized position backward or onto another same-height branch.
func TestRootSubmissionFinalizedContinuity(t *testing.T) {
	for _, fork := range []bool{false, true} {
		fixture := newRootSubmissionFixture(t)
		owner, store := fixture.open(t, true)
		if err := owner.submitRoot(context.Background(), fixture.intent); err != nil {
			t.Fatal(err)
		}
		receipt := fixture.receipt
		if fork {
			header, hash := rootReceiptHeaderFixture(t, receipt.byHeight[receipt.action.BirthBlock+1], receipt.action.BirthBlock+2, [][]byte{{8, 4, 9}}, false)
			receipt.stateLock.Lock()
			receipt.headers[hash], receipt.byHeight[receipt.action.BirthBlock+2], receipt.finalized = header, hash, hash
			receipt.bodies[hash] = []string{"0x080409"}
			receipt.stateLock.Unlock()
		} else {
			receipt.stateLock.Lock()
			receipt.finalized = receipt.byHeight[receipt.action.BirthBlock+1]
			receipt.stateLock.Unlock()
		}
		fixture.intent.Attempt = 2
		if err := owner.submitRoot(context.Background(), fixture.intent); err == nil {
			t.Fatal("changed finalized continuity admitted another send", fork)
		}
		record, err := store.load()
		if err != nil || len(record.Attempts) != 1 || len(fixture.sent()) != 1 {
			t.Fatal("finalized drift changed durable attempt count", err)
		}
	}
}
