// Public host commands drive real six-journal custody for a synthetic native
// multisig owner. Signatory replies come through the owner-signing command;
// the local canonical census route records every post without broadcasting.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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
type ownerTrimMultisigHostFixture struct {
	chain        *bootstrapChainFixture
	action       *ownerTrimTestFixture
	signers      ownerTrimMultisigTestSigners
	guard        *ownerTrimGuardFixture
	configPath   string
	metadataPath string
	approvalKey  ed25519.PrivateKey
	stateLock    sync.Mutex
	posts        []string
	loseReply    bool
}

// The anchor is the first approval by signatory three, born at the given block.
// The reviewed census is block 100; admission reads canonical block 101.
func newOwnerTrimMultisigHostFixture(t *testing.T, birth uint64) *ownerTrimMultisigHostFixture {
	t.Helper()
	signers := newOwnerTrimMultisigTestSigners(t)
	owner, _ := hex.DecodeString(signers.owner[2:])
	chain, action := ownerTrimPreparedTestFixtureWithCensus(t, nil, owner)
	chain.census.set(t, "ValidatorPermit", subnetTestVector([]byte{0, 0, 1, 0, 0, 0}, 1), []byte{25, 0})
	after := &rootRpcFixture{metadata: chain.census.metadata, metadataHex: chain.census.metadataHex, policy: chain.census.policy,
		storageKVs: maps.Clone(chain.census.storageKVs), evmChainHex: chain.census.evmChainHex, version: chain.census.version, methodCounts: map[string]int{}}
	after.set(t, "NetworkImmunityPeriod", binary.LittleEndian.AppendUint64(nil, 200))
	header, hash := rootReceiptHeaderFixture(t, testFinalizedHash, 101, nil, false)
	seed := sha256.Sum256([]byte("synthetic multisig submission risk approval only"))
	self := &ownerTrimMultisigHostFixture{chain: chain, action: action, signers: signers, approvalKey: ed25519.NewKeyFromSeed(seed[:]),
		guard: &ownerTrimGuardFixture{before: chain.census, after: after, afterHeader: header, afterHash: hash, afterNumber: 101, methodCounts: map[string]int{}}}
	server := httptest.NewServer(http.HandlerFunc(self.serve))
	t.Cleanup(server.Close)
	action.config.Route.RpcUrl = server.URL
	action.config.Action.BirthBlock, action.config.Action.BirthHash = birth, testFinalizedHash
	if birth == 101 {
		action.config.Action.BirthHash = hash
	}
	ownerTrimMultisigTestConfig(t, action, signers, signers.accounts[2])
	self.account(t, signers.accounts[2], action.config.Action.Nonce, 1_000_000_000)
	self.configPath = filepath.Join(filepath.Dir(chain.path), "synthetic-multisig-action.json")
	self.metadataPath = filepath.Join(filepath.Dir(chain.path), "synthetic-multisig-metadata.hex")
	if err := os.WriteFile(self.metadataPath, []byte(action.metadata), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(self.metadataPath+".v15", []byte(action.ledgerMetadata), 0600); err != nil {
		t.Fatal(err)
	}
	unsigned := action.config
	unsigned.Signature = ""
	bootstrapRootTestWrite(t, self.configPath, unsigned)
	planned, code, diagnostic := self.command(t, "trim-plan", "--metadata", self.metadataPath, "--ledger-metadata", self.metadataPath+".v15")
	var reviewed ownerTrimExecutionConfig
	if code != 0 || decodePlanJson(planned, &reviewed) != nil || !reflect.DeepEqual(reviewed, unsigned) {
		t.Fatal("first multisig approval planning changed reviewed scope", code, diagnostic)
	}
	bootstrapRootTestWrite(t, self.configPath, action.config)
	if _, code, diagnostic := self.command(t, "trim-apply"); code != 0 {
		t.Fatal("multisig anchor claim", code, diagnostic)
	}
	return self
}

// Signer nonce and free balance at canonical block 101.
func (self *ownerTrimMultisigHostFixture) account(t *testing.T, account string, nonce uint32, free uint64) {
	t.Helper()
	raw, _ := hex.DecodeString(account[2:])
	key, err := types.CreateStorageKey(self.guard.after.metadata, "System", "Account", raw)
	if err != nil {
		t.Fatal(err)
	}
	value := make([]byte, 56)
	binary.LittleEndian.PutUint32(value, nonce)
	binary.LittleEndian.PutUint64(value[16:], free)
	self.guard.after.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(value)
}

// The pending operation row at canonical block 101, or its absence.
func (self *ownerTrimMultisigHostFixture) pending(t *testing.T, value []byte) {
	t.Helper()
	owner, _ := hex.DecodeString(self.signers.owner[2:])
	callHash, _ := hex.DecodeString(self.action.config.Action.Multisig.CallHash[2:])
	key, err := types.CreateStorageKey(self.guard.after.metadata, "Multisig", "Multisigs", owner, callHash)
	if err != nil {
		t.Fatal(err)
	}
	if value == nil {
		delete(self.guard.after.storageKVs, key.Hex())
		return
	}
	self.guard.after.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(value)
}

// Only the owned native-post method is added to the two-block census route.
func (self *ownerTrimMultisigHostFixture) serve(writer http.ResponseWriter, request *http.Request) {
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
		decoded, _ := hex.DecodeString(strings.TrimPrefix(signed, "0x"))
		json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": rootExtrinsicHash(decoded)})
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

// Counted transport observations are read only after each synchronous command.
func (self *ownerTrimMultisigHostFixture) sent() []string {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return append([]string(nil), self.posts...)
}

// The host dispatcher owns all paths and independent trust inputs as in use.
func (self *ownerTrimMultisigHostFixture) command(t *testing.T, mode string, extra ...string) ([]byte, int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args := append([]string{"--trim-config", self.configPath, "--trim-approval-key", self.action.key}, extra...)
	code := self.chain.command(t.Context(), mode, &stdout, &stderr, args...)
	return stdout.Bytes(), code, stderr.String()
}

// The retained status and record after a status-printing host command.
func (self *ownerTrimMultisigHostFixture) status(t *testing.T, raw []byte) (ownerTrimMultisigStatus, ownerTrimRecord) {
	t.Helper()
	var result struct {
		Multisig ownerTrimMultisigStatus `json:"multisig"`
		Record   ownerTrimRecord         `json:"retained_action"`
	}
	if err := decodePlanJson(raw, &result); err != nil {
		t.Fatal("multisig status output", err, string(raw))
	}
	return result.Multisig, result.Record
}

// Export the latest step, have its named signatory answer through the
// owner-signing command on another computer, and import the exact reply.
func (self *ownerTrimMultisigHostFixture) signStep(t *testing.T, step string) (ownerSigningRequest, planFileReference, planFileReference) {
	t.Helper()
	exported, code, diagnostic := self.command(t, "trim-export", "--multisig-step", step, "--metadata", self.metadataPath, "--ledger-metadata", self.metadataPath+".v15")
	var request ownerSigningRequest
	if code != 0 || decodePlanJson(exported, &request) != nil {
		t.Fatal("multisig step export", step, code, diagnostic)
	}
	action := request.Config.Action
	trust := ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: self.action.key, Owner: self.signers.owner, Genesis: action.Network.GenesisHash, Signatory: action.Multisig.Signatory}
	ownerDirectory := ownerSigningTestDirectory(t)
	requestRef := bootstrapRootTestWrite(t, filepath.Join(ownerDirectory, "request.json"), request)
	inspected, diagnostic, code := ownerSigningTestCommand(t, "inspect", requestRef.Path, trust, "--signatory-account-id", trust.Signatory)
	if code != 0 || !bytes.Contains(inspected, []byte("Multisig.")) || !bytes.Contains(inspected, []byte(action.Multisig.InnerCall)) {
		t.Fatal("owner inspection of the multisig step", code, diagnostic)
	}
	if _, diagnostic, code := ownerSigningTestCommand(t, "inspect", requestRef.Path, trust); code != 3 {
		t.Fatal("a multisig request was inspected without its named signatory pin", code, diagnostic)
	}
	payload, _ := hex.DecodeString(action.Payload[2:])
	response := append([]byte{0}, ed25519.Sign(self.signers.keys[action.Multisig.Signatory], payload)...)
	responsePath := filepath.Join(ownerDirectory, "ledger-response.hex")
	if err := os.WriteFile(responsePath, []byte(hex.EncodeToString(response)), 0600); err != nil {
		t.Fatal(err)
	}
	raw, diagnostic, code := ownerSigningTestCommand(t, "reply", requestRef.Path, trust, "--signatory-account-id", trust.Signatory, "--ledger-response", responsePath)
	var reply ownerSigningReply
	if code != 0 || decodePlanJson(raw, &reply) != nil || reply.Signatory != action.Multisig.Signatory {
		t.Fatal("owner multisig reply", code, diagnostic)
	}
	replyRef := bootstrapRootTestWrite(t, filepath.Join(ownerDirectory, "reply.json"), reply)
	if _, code, diagnostic := self.command(t, "trim-import-reply", "--multisig-step", step, "--request", requestRef.Path, "--accept-request-hash", request.ContentHash,
		"--reply", replyRef.Path, "--reply-sha256", replyRef.Sha256); code != 0 {
		t.Fatal("multisig reply import", step, code, diagnostic)
	}
	return request, requestRef, replyRef
}

// A separately signed submission policy for the latest signed step.
func (self *ownerTrimMultisigHostFixture) policy(t *testing.T, step string) planFileReference {
	t.Helper()
	draft, code, diagnostic := self.command(t, "trim-submit-plan", "--multisig-step", step)
	var plan struct {
		Approval     ownerTrimBestEffortApproval `json:"approval_template"`
		SigningBytes string                      `json:"signing_bytes"`
	}
	if code != 0 || decodePlanJson(draft, &plan) != nil || plan.SigningBytes != "0x"+hex.EncodeToString(plan.Approval.signingBytes()) ||
		plan.Approval.MultisigStep == "" || plan.Approval.ResidualRisks[len(plan.Approval.ResidualRisks)-1] != ownerTrimMultisigResidual {
		t.Fatal("multisig step submission policy draft", code, diagnostic)
	}
	plan.Approval.Signature = hex.EncodeToString(ed25519.Sign(self.approvalKey, plan.Approval.signingBytes()))
	return bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(self.chain.path), "synthetic-multisig-policy-"+step+".json"), plan.Approval)
}

// One guarded post of the latest step under its separately signed policy.
func (self *ownerTrimMultisigHostFixture) submit(t *testing.T, step string, policy planFileReference) ([]byte, int, string) {
	t.Helper()
	return self.command(t, "trim-submit", "--multisig-step", step, "--submission-policy", policy.Path, "--submission-policy-sha256", policy.Sha256,
		"--submission-approval-key", "0x"+hex.EncodeToString(self.approvalKey.Public().(ed25519.PublicKey)))
}

// Retain the first approval as canonically recorded at block 101, index 1, the
// state the receipt adapter produces after NewMultisig and pending readback.
func (self *ownerTrimMultisigHostFixture) recordFirstApproval(t *testing.T) treasuryTimepoint {
	t.Helper()
	store, err := openOwnerTrimStore(self.action.storage.Context, self.chain.preparation, self.action.config, self.action.key, false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	owner := &ownerTrimMultisigOwner{store: store, config: self.action.config, key: self.action.key}
	record, steps, err := owner.load()
	if err != nil {
		t.Fatal(err)
	}
	step := steps[0]
	action := step.Config.Action
	metadata := self.guard.after.metadata
	signer, _ := hex.DecodeString(action.Multisig.Signatory[2:])
	account, _ := hex.DecodeString(self.signers.owner[2:])
	callHash, _ := hex.DecodeString(action.Multisig.CallHash[2:])
	events := append([]byte{12}, rootReceiptEventFixture(t, metadata, "TransactionPayment.TransactionFeePaid", 1, signer, binary.LittleEndian.AppendUint64(nil, 17), make([]byte, 8))...)
	events = append(events, rootReceiptEventFixture(t, metadata, "System.ExtrinsicSuccess", 1)...)
	events = append(events, rootReceiptEventFixture(t, metadata, "Multisig.NewMultisig", 1, signer, account, callHash)...)
	timepoint := treasuryTimepoint{Height: 101, Index: 1}
	pending := "0x" + hex.EncodeToString(ownerTrimMultisigTestPending(timepoint, 196_000_000, action.Multisig.Signatory))
	pendingKey, _ := types.CreateStorageKey(metadata, "Multisig", "Multisigs", account, callHash)
	readback := &ownerTrimMultisigReadback{BlockNumber: 101, BlockHash: self.guard.afterHash, PendingKey: pendingKey.Hex(), PendingStorage: &pending}
	for _, item := range []struct {
		name  string
		value []byte
	}{{name: "MaxAllowedUids", value: binary.LittleEndian.AppendUint16(nil, 10)}, {name: "SubnetOwner", value: account}} {
		key, _ := types.CreateStorageKey(metadata, "SubtensorModule", item.name, []byte{25, 0})
		encoded := "0x" + hex.EncodeToString(item.value)
		readback.Storage = append(readback.Storage, rootStorageValue{Name: item.name, Key: key.Hex(), RawStorage: &encoded, EffectiveScale: encoded, ValueSource: "finalized-storage"})
	}
	nonce := action.Nonce + 1
	receipt := rootActionReceipt{BlockNumber: 101, BlockHash: self.guard.afterHash, ExtrinsicIndex: 1, RawExtrinsic: step.RawExtrinsic, EventHash: rootExtrinsicHash(events),
		Success: true, ActualFeeRao: 17, ExecutionRuntimeVersion: action.Runtime.RuntimeVersion, ExecutionCodeHash: action.Runtime.RuntimeCodeHash, ExecutionMetadataHash: action.Runtime.RuntimeMetadataHash}
	evidence := ownerTrimActionReconciliation{Observation: ownerTrimObservation{FinalizedNumber: 101, FinalizedHash: self.guard.afterHash, AccountNonce: &nonce},
		AnchorHash: action.BirthHash, CheckedFrom: action.BirthBlock + 1, CheckedThrough: 101, Receipt: &receipt,
		Multisig: &ownerTrimMultisigEvidence{BodyCount: 2, Events: "0x" + hex.EncodeToString(events), Dispatch: treasuryDispatch{Kind: "opened", Timepoint: &timepoint}, Readback: readback}}
	step.Phase, step.Reconciliation, step.LastFinalized, step.LastFinalizedHash = "approval-recorded", &evidence, 101, self.guard.afterHash
	if err := owner.persist(record, steps, 0, step); err != nil {
		t.Fatal("retained first approval evidence refused", err)
	}
	self.pending(t, ownerTrimMultisigTestPending(timepoint, 196_000_000, action.Multisig.Signatory))
	self.account(t, action.Multisig.Signatory, nonce, 1_000_000_000)
	return timepoint
}

// Plan, independently approve and apply one later step born at block 101.
func (self *ownerTrimMultisigHostFixture) laterStep(t *testing.T, operation, signatory string, nonce uint32) (ownerTrimExecutionConfig, int, string) {
	t.Helper()
	template := ownerTrimMultisigStepTemplate{Operation: operation, Signatory: signatory, DerivationPath: "m/44'/354'/1'/0'/0'", Nonce: nonce,
		BirthBlock: 101, BirthHash: self.guard.afterHash, Period: 16, FeeReserveRao: 1_000_000, MaxBroadcasts: 2}
	if operation == "as_multi" {
		template.MaxRefTime, template.MaxProofSize = 1_000_000_000, 65_536
	}
	templateRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(self.chain.path), "synthetic-multisig-template.json"), template)
	planned, code, diagnostic := self.command(t, "trim-multisig-plan", "--multisig-template", templateRef.Path, "--metadata", self.metadataPath)
	if code != 0 {
		return ownerTrimExecutionConfig{}, code, diagnostic
	}
	var plan struct {
		Config       ownerTrimExecutionConfig         `json:"step_config"`
		SigningBytes string                           `json:"approval_signing_bytes"`
		Observed     ownerTrimMultisigPlanObservation `json:"observed_pending_operation"`
	}
	if err := decodePlanJson(planned, &plan); err != nil || plan.SigningBytes != "0x"+hex.EncodeToString(plan.Config.signingBytes()) || plan.Observed.FinalizedNumber != 101 {
		t.Fatal("later multisig step plan output", err)
	}
	config := plan.Config
	config.Signature = hex.EncodeToString(ed25519.Sign(self.action.approval, config.signingBytes()))
	configRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(self.chain.path), "synthetic-multisig-step.json"), config)
	_, code, diagnostic = self.command(t, "trim-multisig-apply", "--multisig-step-config", configRef.Path)
	return config, code, diagnostic
}

// The first approval is exported, signed by its named signatory and imported
// once. Reimport is idempotent, and no other step or signature can replace it.
func TestOwnerTrimMultisigHostFirstApprovalCustodyAndReplay(t *testing.T) {
	f := newOwnerTrimMultisigHostFixture(t, 101)
	original := f.chain.journals(t)
	request, requestRef, replyRef := f.signStep(t, "0")
	for _, args := range [][]string{
		{"trim-import-reply", "--multisig-step", "0", "--request", requestRef.Path, "--accept-request-hash", request.ContentHash, "--reply", replyRef.Path, "--reply-sha256", replyRef.Sha256},
		{"trim-resume"},
	} {
		raw, code, diagnostic := f.command(t, args[0], args[1:]...)
		status, record := f.status(t, raw)
		if code != 0 || len(status.Steps) != 1 || status.Steps[0].Phase != "signed" || status.OperationOpen || record.Phase != "multisig" || record.Signature != "" {
			t.Fatal("reimport or restart changed the signed first approval", args[0], code, diagnostic)
		}
	}
	if _, code, diagnostic := f.command(t, "trim-export", "--multisig-step", "0", "--metadata", f.metadataPath, "--ledger-metadata", f.metadataPath+".v15"); code != 3 ||
		!strings.Contains(diagnostic, "not in original reserved custody") {
		t.Fatal("a signed first approval was exported again", code, diagnostic)
	}
	if _, code, _ := f.command(t, "trim-import", "--signature", replyRef.Path); code != 2 {
		t.Fatal("a raw signature import reached multisig custody", code)
	}
	if _, code, diagnostic := f.command(t, "trim-export", "--multisig-step", "1", "--metadata", f.metadataPath, "--ledger-metadata", f.metadataPath+".v15"); code != 3 ||
		!strings.Contains(diagnostic, "must name the latest retained step") {
		t.Fatal("an absent step was selected", code, diagnostic)
	}
	if _, code, _ := f.command(t, "trim-reconcile"); code != 2 {
		t.Fatal("a multisig effect ran without an explicit step selector", code)
	}
	if !reflect.DeepEqual(original, f.chain.journals(t)) || len(f.sent()) != 0 {
		t.Fatal("offline multisig handoff changed original preparation journals or posted")
	}
}

// The first approval posts once per durable reservation under its separate
// policy and fee/deposit admission. Lost acknowledgements consume attempts.
func TestOwnerTrimMultisigHostFirstApprovalSubmissionRetainsAllowance(t *testing.T) {
	f := newOwnerTrimMultisigHostFixture(t, 101)
	f.signStep(t, "0")
	policy := f.policy(t, "0")
	f.pending(t, ownerTrimMultisigTestPending(treasuryTimepoint{Height: 99, Index: 3}, 196_000_000, f.signers.accounts[2]))
	if _, code, diagnostic := f.submit(t, "0", policy); code != 1 || len(f.sent()) != 0 || !strings.Contains(diagnostic, "already pending") {
		t.Fatal("first approval was posted over an existing pending operation", code, diagnostic)
	}
	f.pending(t, nil)
	f.account(t, f.signers.accounts[2], f.action.config.Action.Nonce, 196_000_000)
	if _, code, diagnostic := f.submit(t, "0", policy); code != 1 || len(f.sent()) != 0 || !strings.Contains(diagnostic, "fee and deposit reserve") {
		t.Fatal("first approval was posted without its fee reserve above the deposit", code, diagnostic)
	}
	f.account(t, f.signers.accounts[2], f.action.config.Action.Nonce, 1_000_000_000)
	f.loseReply = true
	for attempt := 1; attempt <= 2; attempt++ {
		raw, code, diagnostic := f.submit(t, "0", policy)
		status, record := f.status(t, raw)
		step := record.Multisig.Steps[0]
		if code != 1 || step.Broadcasts != uint8(attempt) || step.Submission == nil || step.Submission.SubmittedBroadcasts != uint8(attempt) ||
			status.Steps[0].Phase != "pending" || len(f.sent()) != attempt || f.sent()[attempt-1] != step.RawExtrinsic {
			t.Fatal("uncertain first approval post changed custody", attempt, code, diagnostic)
		}
	}
	if _, code, _ := f.submit(t, "0", policy); code != 1 || len(f.sent()) != 2 {
		t.Fatal("restart replenished the first approval allowance", code, f.sent())
	}
}

// After the recorded first approval, another signatory's final approval binds
// the original timepoint read back from chain and posts under its own policy.
func TestOwnerTrimMultisigHostFinalApprovalBindsReadBackTimepoint(t *testing.T) {
	f := newOwnerTrimMultisigHostFixture(t, 100)
	f.signStep(t, "0")
	timepoint := f.recordFirstApproval(t)
	final := f.signers.accounts[0]
	f.account(t, final, 5, 50_000_000)
	// A pending row with another timepoint refuses planning before approval.
	f.pending(t, ownerTrimMultisigTestPending(treasuryTimepoint{Height: 100, Index: 9}, 196_000_000, f.signers.accounts[2]))
	if _, code, diagnostic := f.laterStep(t, "as_multi", final, 5); code != 3 || !strings.Contains(diagnostic, "lacks the original pending timepoint") {
		t.Fatal("a pending operation with another timepoint was planned", code, diagnostic)
	}
	f.pending(t, ownerTrimMultisigTestPending(timepoint, 196_000_000, f.signers.accounts[2]))
	if _, code, diagnostic := f.laterStep(t, "as_multi", f.signers.accounts[2], 5); code != 2 {
		t.Fatal("the depositor was planned as its own final approval", code, diagnostic)
	}
	// An independently approved step naming another timepoint cannot be applied.
	wrong, err := ownerTrimMultisigStepConfig(f.action.config, ownerTrimMultisigStepTemplate{Operation: "as_multi", Signatory: final, DerivationPath: "m/44'/354'/1'/0'/0'",
		Nonce: 5, BirthBlock: 101, BirthHash: f.guard.afterHash, Period: 16, FeeReserveRao: 1_000_000, MaxBroadcasts: 2, MaxRefTime: 1_000_000_000, MaxProofSize: 65_536},
		treasuryTimepoint{Height: 100, Index: 9})
	if err == nil {
		wrong.Action, err = prepareOwnerTrimAction(wrong.Action, f.action.metadata)
	}
	if err != nil {
		t.Fatal(err)
	}
	wrong.Signature = hex.EncodeToString(ed25519.Sign(f.action.approval, wrong.signingBytes()))
	wrongRef := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.chain.path), "synthetic-wrong-step.json"), wrong)
	if _, code, diagnostic := f.command(t, "trim-multisig-apply", "--multisig-step-config", wrongRef.Path); code != 3 || !strings.Contains(diagnostic, "its exact timepoint") {
		t.Fatal("a step with another timepoint was applied", code, diagnostic)
	}
	config, code, diagnostic := f.laterStep(t, "as_multi", final, 5)
	if code != 0 || *config.Action.Multisig.Timepoint != timepoint || config.Action.Multisig.Signatory != final {
		t.Fatal("final approval planning or apply", code, diagnostic)
	}
	if _, code, _ := f.command(t, "trim-multisig-apply", "--multisig-step-config", filepath.Join(filepath.Dir(f.chain.path), "synthetic-multisig-step.json")); code != 0 {
		t.Fatal("reapplying the identical approved step was not idempotent", code)
	}
	f.signStep(t, "1")
	policy := f.policy(t, "1")
	// The final approval needs a remaining approval slot on the original row.
	f.pending(t, ownerTrimMultisigTestPending(timepoint, 196_000_000, f.signers.accounts[2], final))
	if _, code, diagnostic := f.submit(t, "1", policy); code != 1 || len(f.sent()) != 0 || !strings.Contains(diagnostic, "a remaining approval") {
		t.Fatal("final approval was posted over an operation it already approved", code, diagnostic)
	}
	f.pending(t, ownerTrimMultisigTestPending(timepoint, 196_000_000, f.signers.accounts[2]))
	raw, code, diagnostic := f.submit(t, "1", policy)
	status, record := f.status(t, raw)
	if code != 0 || len(f.sent()) != 1 || f.sent()[0] != record.Multisig.Steps[1].RawExtrinsic || status.Steps[1].Phase != "pending" || !status.OperationOpen {
		t.Fatal("final approval was not posted once under its policy", code, diagnostic)
	}
	if _, code, _ := f.command(t, "trim-multisig-plan", "--multisig-template", filepath.Join(filepath.Dir(f.chain.path), "synthetic-multisig-template.json"),
		"--metadata", f.metadataPath); code != 3 {
		t.Fatal("another step was planned while the final approval is unsettled", code)
	}
}

// Only the original depositor cancels a stuck first approval; cancellation
// has no census admission but still binds the original pending row and fee.
func TestOwnerTrimMultisigHostDepositorCancelsStuckApproval(t *testing.T) {
	f := newOwnerTrimMultisigHostFixture(t, 100)
	f.signStep(t, "0")
	f.recordFirstApproval(t)
	depositor := f.signers.accounts[2]
	if _, code, diagnostic := f.laterStep(t, "cancel_as_multi", f.signers.accounts[1], 5); code != 2 {
		t.Fatal("a non-depositor cancellation was planned", code, diagnostic)
	}
	config, code, diagnostic := f.laterStep(t, "cancel_as_multi", depositor, f.action.config.Action.Nonce+1)
	if code != 0 || config.Action.Multisig.kind() != "cancel" || config.Action.Multisig.Signatory != depositor {
		t.Fatal("depositor cancellation planning or apply", code, diagnostic)
	}
	f.signStep(t, "1")
	policy := f.policy(t, "1")
	// Census drift cannot block the original depositor's cancellation.
	f.guard.after.set(t, "BlockAtRegistration", binary.LittleEndian.AppendUint64(nil, 99), []byte{25, 0}, []byte{2, 0})
	raw, code, diagnostic := f.submit(t, "1", policy)
	status, _ := f.status(t, raw)
	if code != 0 || len(f.sent()) != 1 || status.Steps[1].Kind != "cancel" || status.Steps[1].Phase != "pending" {
		t.Fatal("depositor cancellation was not posted once", code, diagnostic)
	}
}

// Retain the final approval's canonical outcome at block 102 as the receipt
// adapter records it: MultisigExecuted with the given inner result, the exact
// readback and, for a successful trim, matching whole-block correspondence.
func (self *ownerTrimMultisigHostFixture) recordFinalOutcome(t *testing.T, timepoint treasuryTimepoint, innerOk bool) {
	t.Helper()
	store, err := openOwnerTrimStore(self.action.storage.Context, self.chain.preparation, self.action.config, self.action.key, false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	owner := &ownerTrimMultisigOwner{store: store, config: self.action.config, key: self.action.key}
	record, steps, err := owner.load()
	if err != nil {
		t.Fatal(err)
	}
	index := len(steps) - 1
	step := steps[index]
	action := step.Config.Action
	metadata := self.guard.after.metadata
	signer, _ := hex.DecodeString(action.Multisig.Signatory[2:])
	account, _ := hex.DecodeString(self.signers.owner[2:])
	callHash, _ := hex.DecodeString(action.Multisig.CallHash[2:])
	point := binary.LittleEndian.AppendUint32(nil, timepoint.Height)
	point = binary.LittleEndian.AppendUint32(point, timepoint.Index)
	result, dispatch := []byte{0}, treasuryDispatch{Kind: "executed", Timepoint: &timepoint, InnerSuccess: true}
	if !innerOk {
		result, dispatch = []byte{1, 0}, treasuryDispatch{Kind: "executed", Timepoint: &timepoint, InnerError: "scale:0x00"}
	}
	events := append([]byte{12}, rootReceiptEventFixture(t, metadata, "TransactionPayment.TransactionFeePaid", 0, signer, binary.LittleEndian.AppendUint64(nil, 19), make([]byte, 8))...)
	events = append(events, rootReceiptEventFixture(t, metadata, "System.ExtrinsicSuccess", 0)...)
	events = append(events, rootReceiptEventFixture(t, metadata, "Multisig.MultisigExecuted", 0, signer, point, account, callHash, result)...)
	blockHash, finalizedHash := "0x"+strings.Repeat("a2", 32), "0x"+strings.Repeat("a3", 32)
	pendingKey, _ := types.CreateStorageKey(metadata, "Multisig", "Multisigs", account, callHash)
	readback := &ownerTrimMultisigReadback{BlockNumber: 102, BlockHash: blockHash, PendingKey: pendingKey.Hex()}
	for _, item := range []struct {
		name  string
		value []byte
	}{{name: "MaxAllowedUids", value: binary.LittleEndian.AppendUint16(nil, action.MaximumUids)}, {name: "SubnetOwner", value: account}} {
		key, _ := types.CreateStorageKey(metadata, "SubtensorModule", item.name, []byte{25, 0})
		encoded := "0x" + hex.EncodeToString(item.value)
		readback.Storage = append(readback.Storage, rootStorageValue{Name: item.name, Key: key.Hex(), RawStorage: &encoded, EffectiveScale: encoded, ValueSource: "finalized-storage"})
	}
	nonce := action.Nonce + 1
	receipt := rootActionReceipt{BlockNumber: 102, BlockHash: blockHash, ExtrinsicIndex: 0, RawExtrinsic: step.RawExtrinsic, EventHash: rootExtrinsicHash(events),
		Success: true, ActualFeeRao: 19, ExecutionRuntimeVersion: action.Runtime.RuntimeVersion, ExecutionCodeHash: action.Runtime.RuntimeCodeHash, ExecutionMetadataHash: action.Runtime.RuntimeMetadataHash}
	evidence := ownerTrimActionReconciliation{Observation: ownerTrimObservation{FinalizedNumber: 103, FinalizedHash: finalizedHash, AccountNonce: &nonce},
		AnchorHash: action.BirthHash, CheckedFrom: action.BirthBlock + 1, CheckedThrough: 103, Receipt: &receipt,
		Multisig: &ownerTrimMultisigEvidence{BodyCount: 1, Events: "0x" + hex.EncodeToString(events), Dispatch: dispatch, Readback: readback}}
	if innerOk {
		before, _ := sealSubnetPreview(subnetPreview{CensusComplete: true, Identity: chainIdentity{FinalizedNumber: 101, FinalizedHash: self.guard.afterHash}})
		after, _ := sealSubnetPreview(subnetPreview{CensusComplete: true, Identity: chainIdentity{FinalizedNumber: 102, FinalizedHash: blockHash}})
		evidence.Census = &ownerTrimReceiptCensus{Before: &before, After: &after, Correspondence: &ownerTrimActualSubset{Matches: true, ResidualOld: []subnetRegistration{{Uid: 4}}, Blockers: []string{}}}
	}
	step.Phase, step.Reconciliation, step.LastFinalized, step.LastFinalizedHash = "inner-dispatch-failed", &evidence, 103, finalizedHash
	if innerOk {
		step.Phase = "executed"
	}
	if err := owner.persist(record, steps, index, step); err != nil {
		t.Fatal("retained final outcome refused", err)
	}
}

// MultisigExecuted with an Err inner result is a failed trim that closes the
// operation. Success additionally needs the capacity readback and matching
// whole-block correspondence, and it never claims a full reset or activation.
func TestOwnerTrimMultisigHostFinalOutcomeRequiresOkInnerResult(t *testing.T) {
	for _, innerOk := range []bool{false, true} {
		f := newOwnerTrimMultisigHostFixture(t, 100)
		f.signStep(t, "0")
		timepoint := f.recordFirstApproval(t)
		final := f.signers.accounts[0]
		f.account(t, final, 5, 50_000_000)
		if _, code, diagnostic := f.laterStep(t, "as_multi", final, 5); code != 0 {
			t.Fatal("final approval planning or apply", code, diagnostic)
		}
		f.signStep(t, "1")
		f.recordFinalOutcome(t, timepoint, innerOk)
		raw, code, diagnostic := f.command(t, "trim-resume")
		status, _ := f.status(t, raw)
		view := status.Steps[1]
		if code != 0 || status.OperationOpen || status.NextStepPlannable || status.FullResetCompleted || status.ActivationReady {
			t.Fatal("final outcome left the operation open or claimed completion", innerOk, code, diagnostic)
		}
		if innerOk && (view.Phase != "executed" || view.Status != "multisig-executed-partial-trim-correspondence-observed" || !view.GenerationReconciled ||
			view.ResidualOldCount == nil || *view.ResidualOldCount != 1 || status.ClosedBy != "executed") {
			t.Fatalf("successful final approval status differs: %+v", status)
		}
		if !innerOk && (view.Phase != "inner-dispatch-failed" || view.Status != "multisig-executed-inner-trim-failed" || view.InnerError == "" ||
			view.GenerationReconciled || status.ClosedBy != "inner-dispatch-failed") {
			t.Fatalf("Err inner trim result was not a failure: %+v", status)
		}
		templatePath := filepath.Join(filepath.Dir(f.chain.path), "synthetic-multisig-template.json")
		if _, code, diagnostic := f.command(t, "trim-multisig-plan", "--multisig-template", templatePath, "--metadata", f.metadataPath); code != 3 ||
			!strings.Contains(diagnostic, "original open operation") {
			t.Fatal("a later step was planned after the operation closed", innerOk, code, diagnostic)
		}
	}
}
