// Synthetic owner signatures, real six-journal custody and canonical local RPC
// exercise the public best-effort handoff without a device or external network.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// All mutable transport controls and posted bytes are serialized by stateLock.
type ownerTrimBestEffortTestFixture struct {
	chain       *bootstrapChainFixture
	action      *ownerTrimTestFixture
	guard       *ownerTrimGuardFixture
	configPath  string
	approval    ownerTrimBestEffortApproval
	approvalKey ed25519.PrivateKey
	policyRef   planFileReference
	raw         []byte
	stateLock   sync.Mutex
	posts       []string
	loseReply   bool
	beforeReply func(string)
}

// Offline public commands claim, export and import the exact new-domain action.
// The original reviewed census remains at 100; admission is at canonical 101.
func newOwnerTrimBestEffortTestFixture(t *testing.T, registrationOpen ...bool) *ownerTrimBestEffortTestFixture {
	t.Helper()
	configure := func(census *rootRpcFixture, _ *subnetCensusPolicy) {
		if len(registrationOpen) != 0 && registrationOpen[0] {
			census.set(t, "NetworkRegistrationAllowed", []byte{1}, []byte{25, 0})
			census.set(t, "NetworkPowRegistrationAllowed", []byte{1}, []byte{25, 0})
		}
	}
	chain, action := ownerTrimPreparedTestFixtureWithCensus(t, configure, ownerSigningTestKey().Public().(ed25519.PublicKey))
	ownerSigningTestLedgerConfig(t, action)
	chain.census.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 1, 0, 0, 0}, 1), []byte{25, 0})
	after := &rootRpcFixture{metadata: chain.census.metadata, metadataHex: chain.census.metadataHex, policy: chain.census.policy,
		storageKVs: maps.Clone(chain.census.storageKVs), evmChainHex: chain.census.evmChainHex, version: chain.census.version, methodCounts: map[string]int{}}
	after.set(t, "NetworkImmunityPeriod", binary.LittleEndian.AppendUint64(nil, 200))
	owner, _ := hex.DecodeString(action.config.Action.Coldkey[2:])
	account, err := types.CreateStorageKey(after.metadata, "System", "Account", owner)
	if err != nil {
		t.Fatal(err)
	}
	accountValue := make([]byte, 56)
	binary.LittleEndian.PutUint32(accountValue, action.config.Action.Nonce)
	after.storageKVs[account.Hex()] = "0x" + hex.EncodeToString(accountValue)
	header, hash := rootReceiptHeaderFixture(t, testFinalizedHash, 101, nil, false)
	seed := sha256.Sum256([]byte("synthetic separate best-effort risk approval only"))
	self := &ownerTrimBestEffortTestFixture{chain: chain, action: action, approvalKey: ed25519.NewKeyFromSeed(seed[:]),
		guard: &ownerTrimGuardFixture{before: chain.census, after: after, afterHeader: header, afterHash: hash, afterNumber: 101, methodCounts: map[string]int{}}}
	server := httptest.NewServer(http.HandlerFunc(self.serve))
	t.Cleanup(server.Close)
	action.config.Schema = ownerTrimBestEffortExecutionSchema
	action.config.Action.Schema, action.config.Action.SelectionRule = ownerTrimBestEffortActionSchema, ownerTrimBestEffortSelection
	action.config.Action.BirthBlock, action.config.Action.BirthHash = 101, hash
	action.config.Action, err = prepareOwnerTrimAction(action.config.Action, action.metadata)
	if err != nil {
		t.Fatal(err)
	}
	action.config.Route.RpcUrl = server.URL
	action.approve()
	self.configPath = filepath.Join(filepath.Dir(chain.path), "synthetic-best-effort-action.json")
	metadataPath := filepath.Join(filepath.Dir(chain.path), "synthetic-best-effort-metadata.hex")
	if err := os.WriteFile(metadataPath, []byte(action.metadata), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath+".v15", []byte(action.ledgerMetadata), 0600); err != nil {
		t.Fatal(err)
	}
	unsigned := action.config
	unsigned.Signature = ""
	bootstrapRootTestWrite(t, self.configPath, unsigned)
	planned, code, diagnostic := self.command(t, "trim-plan", "--metadata", metadataPath, "--ledger-metadata", metadataPath+".v15")
	var reviewed ownerTrimExecutionConfig
	if code != 0 || decodePlanJson(planned, &reviewed) != nil || !reflect.DeepEqual(reviewed, unsigned) {
		t.Fatal("fresh best-effort action planning changed reviewed scope", code, diagnostic)
	}
	bootstrapRootTestWrite(t, self.configPath, action.config)
	if _, code, diagnostic := self.command(t, "trim-apply"); code != 0 {
		t.Fatal("new best-effort claim", code, diagnostic)
	}
	exported, code, diagnostic := self.command(t, "trim-export", "--metadata", metadataPath, "--ledger-metadata", metadataPath+".v15")
	var request ownerSigningRequest
	if code != 0 || decodePlanJson(exported, &request) != nil {
		t.Fatal("new-domain public export", code, diagnostic)
	}
	trust := ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: action.key, Owner: action.config.Action.Coldkey, Genesis: action.config.Action.Network.GenesisHash}
	if err := request.validate(trust); err != nil {
		t.Fatal("portable new-domain owner review", err)
	}
	signature, err := ownerLedgerResponse(request, ownerSigningTestLedgerResponse(t, request))
	if err != nil {
		t.Fatal("synthetic public Ledger response", err)
	}
	reply, err := newOwnerSigningReply(request, signature)
	if err != nil {
		t.Fatal(err)
	}
	self.raw, _ = hex.DecodeString(reply.RawExtrinsic[2:])
	requestRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(chain.path), "synthetic-best-effort-request.json"), request)
	replyRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(chain.path), "synthetic-best-effort-reply.json"), reply)
	if _, code, diagnostic := self.command(t, "trim-import-reply", "--request", requestRef.Path, "--accept-request-hash", request.ContentHash,
		"--reply", replyRef.Path, "--reply-sha256", replyRef.Sha256); code != 0 {
		t.Fatal("new-domain original reply import", code, diagnostic)
	}
	draft, code, diagnostic := self.command(t, "trim-submit-plan")
	var policyRequest struct {
		Approval     ownerTrimBestEffortApproval `json:"approval_template"`
		SigningBytes string                      `json:"signing_bytes"`
	}
	if code != 0 || decodePlanJson(draft, &policyRequest) != nil || policyRequest.SigningBytes != "0x"+hex.EncodeToString(policyRequest.Approval.signingBytes()) {
		t.Fatal("local unsigned submission policy", code, diagnostic)
	}
	self.approval = policyRequest.Approval
	self.approval.Signature = hex.EncodeToString(ed25519.Sign(self.approvalKey, self.approval.signingBytes()))
	self.policyRef = bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(chain.path), "synthetic-best-effort-policy.json"), self.approval)
	return self
}

// Only the owned native-post method is added to the existing two-block census
// transport. Failed acknowledgements still record the actual received bytes.
func (self *ownerTrimBestEffortTestFixture) serve(writer http.ResponseWriter, request *http.Request) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	raw, err := io.ReadAll(request.Body)
	var call struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err != nil || json.Unmarshal(raw, &call) != nil {
		http.Error(writer, "invalid synthetic request", 400)
		return
	}
	if self.beforeReply != nil {
		self.beforeReply(call.Method)
	}
	if call.Method == "author_submitExtrinsic" {
		var signed string
		if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &signed) != nil {
			http.Error(writer, "invalid synthetic post", 400)
			return
		}
		self.posts = append(self.posts, signed)
		if self.loseReply {
			http.Error(writer, "synthetic lost acknowledgement", 503)
			return
		}
		json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": rootExtrinsicHash(self.raw)})
		return
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	response, err := self.guard.roundTrip(request)
	if err != nil {
		http.Error(writer, err.Error(), 400)
		return
	}
	defer response.Body.Close()
	writer.WriteHeader(response.StatusCode)
	io.Copy(writer, response.Body)
}

// The dispatcher owns all paths and independent trust inputs exactly as in use.
func (self *ownerTrimBestEffortTestFixture) command(t *testing.T, mode string, extra ...string) ([]byte, int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args := []string{"--trim-config", self.configPath, "--trim-approval-key", self.action.key}
	if mode == "trim-submit" {
		args = append(args, "--submission-policy", self.policyRef.Path, "--submission-policy-sha256", self.policyRef.Sha256,
			"--submission-approval-key", "0x"+hex.EncodeToString(self.approvalKey.Public().(ed25519.PublicKey)))
	}
	code := self.chain.command(t.Context(), mode, &stdout, &stderr, append(args, extra...)...)
	return stdout.Bytes(), code, stderr.String()
}

// Adapter tests use the same public-imported record; no signer is installed.
func (self *ownerTrimBestEffortTestFixture) open(t *testing.T) (*ownerTrimStore, *ownerTrimBestEffortChain, *ownerTrimExecutor) {
	t.Helper()
	store, err := openOwnerTrimStore(self.action.storage.Context, self.chain.preparation, self.action.config, self.action.key, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.close() })
	adapter, err := newOwnerTrimBestEffortChain(store, self.approval, "0x"+hex.EncodeToString(self.approvalKey.Public().(ed25519.PublicKey)))
	if err != nil {
		t.Fatal(err)
	}
	owner := &ownerTrimExecutor{config: self.action.config, key: self.action.key, store: store, chain: adapter, authority: adapter}
	return store, adapter, owner
}

// Counted transport observations are read only after each synchronous operation.
func (self *ownerTrimBestEffortTestFixture) sent() []string {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]string(nil), self.posts...)
}

// Public submit/restart retains original nonce, era, signature and budget after
// acknowledgement loss. Acknowledgement never claims finality or launch readiness.
func TestOwnerTrimBestEffortPublicRetainedSubmission(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t)
	original := f.chain.journals(t)
	f.loseReply = true
	for attempt := 1; attempt <= 2; attempt++ {
		raw, code, diagnostic := f.command(t, "trim-submit")
		var result struct {
			Step   ownerTrimStepResult `json:"step"`
			Record ownerTrimRecord     `json:"retained_action"`
		}
		if code != 1 || decodePlanJson(raw, &result) != nil || result.Record.Submission == nil || result.Record.Broadcasts != uint8(attempt) ||
			result.Record.Submission.SubmittedBroadcasts != uint8(attempt) || result.Record.RawExtrinsic != "0x"+hex.EncodeToString(f.raw) ||
			!reflect.DeepEqual(result.Record.Config, f.action.config) || result.Step.TransactionFinalized || result.Step.FullResetCompleted || result.Step.ActivationReady {
			t.Fatalf("original uncertain post %d changed custody or claimed completion: code=%d result=%+v diagnostic=%s", attempt, code, result, diagnostic)
		}
		if len(f.sent()) != attempt {
			t.Fatal("a single numbered attempt retried transport", f.sent())
		}
	}
	if _, code, _ := f.command(t, "trim-submit"); code != 1 || len(f.sent()) != 2 {
		t.Fatal("restart replenished the original allowance", code, f.sent())
	}
	if _, code, _ := f.command(t, "trim-reconcile"); code != 1 || len(f.sent()) != 2 {
		t.Fatal("recovery command submitted retained bytes", code, f.sent())
	}
	if !reflect.DeepEqual(original, f.chain.journals(t)) {
		t.Fatal("best-effort owner changed original preparation journals")
	}
}

// Even a direct adapter caller cannot repeat a consumed numbered post or swap
// its original policy. A new invocation must go through canonical reconciliation.
func TestOwnerTrimBestEffortPostReservationAndPolicyStayOriginal(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t)
	store, adapter, owner := f.open(t)
	if _, err := owner.step(t.Context()); err != nil {
		t.Fatal("original guarded post", err)
	}
	if err := adapter.submit(t.Context(), f.action.config, f.raw); err == nil || len(f.sent()) != 1 {
		t.Fatal("direct caller reused consumed post reservation", err, f.sent())
	}
	changed := f.approval
	changed.ValidThroughBlock--
	changed.Signature = hex.EncodeToString(ed25519.Sign(f.approvalKey, changed.signingBytes()))
	if _, err := newOwnerTrimBestEffortChain(store, changed, adapter.approvalKey); err == nil {
		t.Fatal("a valid new risk signature replaced original pending policy")
	}
	if _, err := newOwnerTrimBestEffortChain(store, f.approval, f.action.key); err == nil {
		t.Fatal("retained risk policy supplied its own independent trust")
	}
	if record, err := store.load(); err != nil || record.Broadcasts != 1 || record.Submission.SubmittedBroadcasts != 1 || record.RawExtrinsic != "0x"+hex.EncodeToString(f.raw) {
		t.Fatal("refused policy replacement altered original attempt", err)
	}
}

// Explicit approvals bind the entire fresh domain, signed bytes, scope and
// closed residual-risk list. Valid signatures cannot widen the original bounds.
func TestOwnerTrimBestEffortApprovalCannotWeakenStrictDomains(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t)
	store, adapter, _ := f.open(t)
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"strict-v1", "strict-v2", "risk", "runtime", "census", "protected", "signature", "bytes", "expiry", "budget"} {
		approval := ownerTrimTestCopy(t, f.approval)
		config := f.action.config
		switch fault {
		case "strict-v1":
			config.Schema, config.Action.Schema = ownerTrimExecutionSchema, ownerTrimActionSchema
			approval.ConfigHash = rootObjectHash(config)
		case "strict-v2":
			config.Schema, config.Action.Schema = ownerTrimLedgerExecutionSchema, ownerTrimLedgerActionSchema
			approval.ConfigHash = rootObjectHash(config)
		case "risk":
			approval.ResidualRisks = approval.ResidualRisks[:len(approval.ResidualRisks)-1]
		case "runtime":
			approval.Runtime.RuntimeVersion.SpecVersion++
		case "census":
			approval.BaselineCensusHash = rootObjectHash("another census")
		case "protected":
			approval.ProtectedScopeHash = rootObjectHash("another protected generation")
		case "bytes":
			approval.ExtrinsicHash = "0x" + strings.Repeat("ca", 32)
		case "expiry":
			approval.ValidThroughBlock++
		case "budget":
			approval.InitialBroadcasts = config.Action.MaxBroadcasts
		}
		approval.Signature = hex.EncodeToString(ed25519.Sign(f.approvalKey, approval.signingBytes()))
		if fault == "signature" {
			approval.Signature = strings.Repeat("00", 64)
		}
		err := approval.validate(config, adapter.approvalKey, record.ExtrinsicHash)
		if err == nil && (fault == "census" || fault == "protected") {
			err = store.validateBestEffortApproval(approval, adapter.approvalKey, record)
		}
		if err == nil {
			t.Fatal("best-effort approval admitted", fault)
		}
	}
	if len(f.sent()) != 0 {
		t.Fatal("approval checking contacted a submission endpoint")
	}
}

// The preexisting strict action can never be relabeled after claiming custody,
// even with a new independent signature over otherwise identical native bytes.
func TestOwnerTrimBestEffortCannotAdoptClaimedStrictAction(t *testing.T) {
	chain, f := ownerTrimPreparedTestFixture(t, ownerSigningTestKey().Public().(ed25519.PublicKey))
	ownerSigningTestLedgerConfig(t, f)
	store, err := openOwnerTrimStore(f.storage.Context, chain.preparation, f.config, f.key, true)
	if err != nil {
		t.Fatal(err)
	}
	store.close()
	original, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	f.config.Schema, f.config.Action.Schema = ownerTrimBestEffortExecutionSchema, ownerTrimBestEffortActionSchema
	f.config.Action.SelectionRule = ownerTrimBestEffortSelection
	f.config.Action, err = prepareOwnerTrimAction(f.config.Action, f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	f.approve()
	if changed, err := openOwnerTrimStore(f.storage.Context, chain.preparation, f.config, f.key, false); err == nil {
		changed.close()
		t.Fatal("new domain adopted an already claimed strict journal")
	}
	retained, err := os.ReadFile(f.config.Action.StatePath)
	if err != nil || !bytes.Equal(original, retained) {
		t.Fatal("refused best-effort migration changed original bytes", err)
	}
}

// The exact post-reservation directory-sync boundary forces replacement before
// the final transport check. Restoring the original marker cannot heal this owner.
func TestOwnerTrimBestEffortLostCustodyAfterPostReservationRefusesSend(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t)
	store, _, owner := f.open(t)
	var restore func()
	store.syncDirectory = func(directory *os.File) error {
		raw, err := os.ReadFile(store.config.Action.StatePath)
		var record ownerTrimRecord
		if err == nil {
			err = decodePlanJson(raw, &record)
		}
		if err != nil {
			return err
		}
		if record.Submission.SubmittedBroadcasts == 1 {
			restore = bootstrapReadinessTestReplace(t, store.config.Action.StatePath+".lock")
		}
		return directory.Sync()
	}
	_, err := owner.step(t.Context())
	if restore == nil {
		t.Fatal("post reservation boundary was not reached", err)
	}
	restore()
	if !errors.Is(err, errRpcIntegrity) || len(f.sent()) != 0 {
		t.Fatal("lost exclusive marker admitted actual native post", err, f.sent())
	}
	if _, err := store.load(); !errors.Is(err, errRpcIntegrity) {
		t.Fatal("restored marker renewed failed submission owner", err)
	}
}

// A head change after the durable reservation consumes the attempt but refuses
// transport. The fault fires at the real publication boundary, without timing.
func TestOwnerTrimBestEffortHeadChangeAfterPostReservationRefusesSend(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t)
	store, _, owner := f.open(t)
	fired := false
	store.syncDirectory = func(directory *os.File) error {
		raw, err := os.ReadFile(store.config.Action.StatePath)
		var record ownerTrimRecord
		if err == nil {
			err = decodePlanJson(raw, &record)
		}
		if err != nil {
			return err
		}
		if record.Submission.SubmittedBroadcasts == 1 {
			fired = true
			f.stateLock.Lock()
			f.guard.faultMethod, f.guard.fault = "chain_getFinalizedHead", "changed-head"
			f.stateLock.Unlock()
		}
		return directory.Sync()
	}
	_, err := owner.step(t.Context())
	record, loadErr := store.load()
	if !fired || err == nil || loadErr != nil || record.Broadcasts != 1 || record.Submission.SubmittedBroadcasts != 1 || len(f.sent()) != 0 {
		t.Fatal("moving head after reservation posted or restored allowance", fired, err, loadErr, f.sent())
	}
}

// A marker can be lost during the last RPC after all durable publications have
// succeeded. The final physical check must still precede actual network effects.
func TestOwnerTrimBestEffortMarkerReplacementDuringFinalRpcRefusesSend(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t)
	store, _, owner := f.open(t)
	markerPath := store.config.Action.StatePath + ".lock"
	original, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	var replacementErr error
	store.syncDirectory = func(directory *os.File) error {
		raw, err := os.ReadFile(store.config.Action.StatePath)
		var record ownerTrimRecord
		if err == nil {
			err = decodePlanJson(raw, &record)
		}
		if err != nil {
			return err
		}
		if record.Submission.SubmittedBroadcasts == 1 {
			f.stateLock.Lock()
			f.beforeReply = func(method string) {
				if method == "chain_getFinalizedHead" && !fired {
					fired = true
					replacementErr = os.Rename(markerPath, markerPath+".original")
					if replacementErr == nil {
						replacementErr = os.WriteFile(markerPath, original, 0600)
					}
				}
			}
			f.stateLock.Unlock()
		}
		return directory.Sync()
	}
	_, err = owner.step(t.Context())
	f.stateLock.Lock()
	completed, faultErr := fired, replacementErr
	f.stateLock.Unlock()
	if !completed || faultErr != nil || !errors.Is(err, errRpcIntegrity) || len(f.sent()) != 0 {
		t.Fatal("last native read escaped physical custody", completed, faultErr, err, f.sent())
	}
}

// Protected-generation drift is observed in the current block only; the old
// baseline remains reconstructible. An unchanged capacity cannot authorize it.
func TestOwnerTrimBestEffortCurrentProtectedGenerationRefusesSend(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t)
	f.guard.after.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 99), []byte{25, 0}, []byte{2, 0})
	store, _, owner := f.open(t)
	_, err := owner.step(t.Context())
	record, loadErr := store.load()
	if err == nil || loadErr != nil || record.Broadcasts != 0 || record.Submission.SubmittedBroadcasts != 0 || len(f.sent()) != 0 {
		t.Fatal("changed protected generation inherited old admission", err, loadErr, f.sent())
	}
}

// A valid narrower policy does not authorize a head outside its explicit
// interval. Only receipt reconciliation remains available without a new action.
func TestOwnerTrimBestEffortSubmissionRequiresApprovedCurrentWindow(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t)
	f.approval.ValidFromBlock++
	f.approval.Signature = hex.EncodeToString(ed25519.Sign(f.approvalKey, f.approval.signingBytes()))
	store, _, owner := f.open(t)
	_, err := owner.step(t.Context())
	record, loadErr := store.load()
	if err == nil || loadErr != nil || record.Broadcasts != 0 || len(f.sent()) != 0 {
		t.Fatal("not-yet-valid submission window admitted a post", err, loadErr, f.sent())
	}
}

// A direct caller cannot replace a retained later head with an older otherwise
// canonical observation. No fresh census can erase that continuity contradiction.
func TestOwnerTrimBestEffortAdmissionPreservesFinalizedContinuity(t *testing.T) {
	f := newOwnerTrimBestEffortTestFixture(t)
	store, adapter, owner := f.open(t)
	evidence, err := adapter.reconcile(t.Context(), f.action.config.Action, f.raw)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	record.LastFinalized, record.LastFinalizedHash = evidence.Observation.FinalizedNumber+1, "0x"+strings.Repeat("ce", 32)
	if err := owner.persist(record); err != nil {
		t.Fatal(err)
	}
	if err := adapter.authorize(t.Context(), f.action.config, evidence); err == nil || len(f.sent()) != 0 {
		t.Fatal("best-effort admission forgot a later retained finalized head", err)
	}
}
