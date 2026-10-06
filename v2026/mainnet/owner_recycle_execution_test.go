// Public recycle execution uses synthetic accounts, a local canonical RPC and
// a deterministic device boundary. No test needs physical hardware or a chain.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The actual adapter protocol checks durable intent before creating a public
// response. Counters and explicit callbacks expose issuance boundaries.
type ownerRecycleDeviceFixture struct {
	f          *ownerRecycleTestFixture
	request    ownerRecycleSigningRequest
	prepares   atomic.Int32
	signs      atomic.Int32
	omit       bool
	lost       bool
	beforeSign func()
}

// Only the hardware boundary is replaced; all payload seams and custody remain.
func (self *ownerRecycleDeviceFixture) invoke(ctx context.Context, config ownerSigningDeviceConfig, input ownerSigningAdapterInput) (ownerSigningAdapterResult, error) {
	action := self.request.Config.Action
	result := ownerSigningAdapterResult{Schema: ownerSigningAdapterSchema, Mode: input.Mode, RequestHash: self.request.ContentHash,
		SourceCommit: rootActionV1Source, MetadataDigest: action.MetadataDigest, ProofHash: rootObjectHash("synthetic recycle proof")}
	if input.RequestHash != self.request.ContentHash || input.MetadataHex != self.request.LedgerMetadata || input.Owner != action.Owner || input.Account != 0 || input.Index != 0 ||
		input.AppVersion != config.AppVersion || input.BackendPath != config.BackendPath || input.BackendHash != config.BackendHash ||
		len(input.IncludedExtrinsic) < 2 || len(input.IncludedSignedData) < 2 ||
		input.Call+input.IncludedExtrinsic[2:]+input.IncludedSignedData[2:] != action.Payload {
		return result, errors.New("recycle adapter changed exact original owner or payload seams")
	}
	if input.Mode == "prepare" {
		self.prepares.Add(1)
		return result, nil
	}
	self.signs.Add(1)
	raw, err := os.ReadFile(config.StatePath)
	var record ownerSigningDeviceRecord
	if err != nil || decodePlanJson(raw, &record) != nil || record.Schema != ownerRecycleDeviceStateSchema || record.Phase != "signing" || record.RequestHash != input.RequestHash || record.ProofHash != input.ProofHash {
		return result, errors.New("recycle device reached before durable original signing intent")
	}
	if self.beforeSign != nil {
		self.beforeSign()
	}
	if self.omit {
		return result, errors.New("synthetic recycle device response unavailable")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	payload, _ := hex.DecodeString(action.Payload[2:])
	result.PublicKey, result.AppVersion = action.Owner, config.AppVersion
	result.Response = "0x" + hex.EncodeToString(append([]byte{0}, ed25519.Sign(self.f.edKey, payload)...))
	encoded, _ := json.Marshal(result)
	file, err := os.OpenFile(input.ResponsePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, err
	}
	_, err = file.Write(encoded)
	err = errors.Join(err, file.Sync(), file.Close())
	if self.lost {
		err = errors.Join(err, errors.New("synthetic stdout lost after durable device response"))
	}
	return result, err
}

// Portable command inputs live apart from host custody and use an owner-local
// durable declaration. The fixture never opens a deployment path while signing.
func ownerRecycleDeviceCommandFixture(t *testing.T) (*ownerRecycleDeviceFixture, ownerSigningDeviceConfig, context.Context, []string) {
	t.Helper()
	f := newOwnerRecycleTestFixture(t, true)
	request, err := newOwnerRecycleSigningRequest(f.config, f.key, f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	config := ownerSigningTestDeviceConfig(t)
	ctx := ownerLocalDurableTestContext(t, filepath.Dir(config.StatePath))
	raw, _ := json.Marshal(request)
	requestPath, _ := ownerRecycleTestFile(t, filepath.Dir(config.StatePath), "portable-recycle.json", raw)
	args := []string{"sign", "--request", requestPath, "--accept-request-hash", request.ContentHash, "--approval-key", f.key,
		"--owner-account-id", f.config.Action.Owner, "--expected-genesis", f.config.Action.Policy.GenesisHash,
		"--owner-state", config.StatePath, "--ledger-python", config.PythonPath, "--ledger-helper", config.HelperPath,
		"--ledger-helper-sha256", config.HelperHash, "--ledger-backend", config.BackendPath,
		"--ledger-backend-sha256", config.BackendHash, "--ledger-app-version", "100.0.5"}
	return &ownerRecycleDeviceFixture{f: f, request: request}, config, ctx, args
}

// This was impossible with the trim-only signer. Lost stdout now recovers one
// exact reply and import-reply preserves the original exported host request.
func TestOwnerRecyclePublicLedgerReplyRoundTripAndLostOutput(t *testing.T) {
	f, config, ctx, args := ownerRecycleDeviceCommandFixture(t)
	f.lost = true
	var out, diagnostic bytes.Buffer
	if code := runOwnerRecycleCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke); code != 0 {
		t.Fatal(code, diagnostic.String())
	}
	var reply ownerSigningReply
	if err := decodePlanJson(out.Bytes(), &reply); err != nil || reply.Schema != ownerRecycleReplySchema || f.signs.Load() != 1 || f.prepares.Load() != 1 {
		t.Fatal("original recycle device reply missing", err)
	}
	original, _ := os.ReadFile(config.StatePath)
	out.Reset()
	if code := runOwnerRecycleCommandWithAdapter(ctx, args, &out, &diagnostic, nil); code != 0 {
		t.Fatal("retained reply reopened hardware", diagnostic.String())
	}
	current, _ := os.ReadFile(config.StatePath)
	if !bytes.Equal(original, current) {
		t.Fatal("retained device recovery rewrote original custody")
	}
	store, err := openOwnerRecycleStore(f.f.config, f.f.key, true, f.f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody := ownerRecycleCustody{config: f.f.config, key: f.f.key, store: store}
	if _, err := custody.export(f.f.input.Metadata, f.f.input.LedgerMetadata); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(f.f.config.Action.StatePath)
	raw, _ := json.Marshal(f.f.config)
	configPath, _ := ownerRecycleTestFile(t, directory, "original-config.json", raw)
	raw, _ = json.Marshal(reply)
	replyPath, replyHash := ownerRecycleTestFile(t, directory, "owner-reply.json", raw)
	importArgs := []string{"owner-recycle", "import-reply", "--config", configPath, "--approval-key", f.f.key,
		"--accept-action-hash", f.f.config.Action.RequestHash, "--accept-request-hash", f.request.ContentHash,
		"--reply", replyPath, "--reply-sha256", replyHash}
	for attempt := 0; attempt < 2; attempt++ {
		out.Reset()
		diagnostic.Reset()
		if code := runMain(f.f.storage.Context, importArgs, &out, &diagnostic); code != 0 {
			t.Fatal("public reply import failed", code, diagnostic.String())
		}
	}
	raw, err = os.ReadFile(f.f.config.Action.StatePath)
	var retained ownerRecycleRecord
	if err != nil || decodePlanJson(raw, &retained) != nil || retained.RawExtrinsic != reply.RawExtrinsic || retained.Request.ContentHash != f.request.ContentHash {
		t.Fatal("host import changed original recycle request or bytes", err)
	}
}

// An uncertain issuance must never return to reserved or call hardware again.
func TestOwnerRecycleLedgerUnknownIssuanceNeverReissues(t *testing.T) {
	f, _, ctx, args := ownerRecycleDeviceCommandFixture(t)
	f.omit = true
	for attempt := 0; attempt < 2; attempt++ {
		var out, diagnostic bytes.Buffer
		if code := runOwnerRecycleCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke); code == 0 || out.Len() != 0 {
			t.Fatal("unresolved issuance produced a public reply")
		}
	}
	if f.signs.Load() != 1 || f.prepares.Load() != 1 {
		t.Fatal("unknown original signing was repeated")
	}
}

// Independent pins and domain-separated replies cannot be borrowed from trim
// or from another valid recycle request, even with a correct native signature.
func TestOwnerRecycleLedgerIndependentPinsAndReplyDomain(t *testing.T) {
	for _, flag := range []string{"--owner-account-id", "--expected-genesis", "--approval-key", "--accept-request-hash"} {
		f, config, ctx, args := ownerRecycleDeviceCommandFixture(t)
		for i := range args {
			if args[i] == flag {
				args[i+1] = "0x" + strings.Repeat("ab", 32)
				if flag == "--accept-request-hash" {
					args[i+1] = rootObjectHash("foreign recycle request")
				}
			}
		}
		var out, diagnostic bytes.Buffer
		if runOwnerRecycleCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke) == 0 || f.signs.Load() != 0 || f.prepares.Load() != 0 {
			t.Fatal("independent pin mismatch reached hardware", flag)
		}
		if _, err := os.Stat(config.StatePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid portable request claimed device custody", err)
		}
	}
	f := newOwnerRecycleTestFixture(t, true)
	request, _ := newOwnerRecycleSigningRequest(f.config, f.key, f.input.Metadata, f.input.LedgerMetadata)
	reply, _ := newOwnerRecycleSigningReply(request, f.signature(t))
	reply.Schema = ownerSigningReplySchema
	if _, err := validateOwnerRecycleSigningReply(request, reply); err == nil {
		t.Fatal("trim reply domain acquired recycle authority")
	}
}

// A second command sees the first owner's permanent physical lock while the
// explicit adapter barrier proves that issuance is in progress.
func TestOwnerRecycleLedgerConcurrentOwner(t *testing.T) {
	f, _, ctx, args := ownerRecycleDeviceCommandFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.beforeSign = func() { close(entered); <-release }
	finished := make(chan int, 1)
	go func() {
		var out, diagnostic bytes.Buffer
		finished <- runOwnerRecycleCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke)
	}()
	<-entered
	var out, diagnostic bytes.Buffer
	code := runOwnerRecycleCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke)
	close(release)
	first := <-finished
	if code == 0 || first != 0 || f.signs.Load() != 1 {
		t.Fatal("concurrent recycle device custody was not exclusive", code, first)
	}
}

// Cancellation after durable issuance intent remains unknown on restart and
// cannot trigger another device operation merely because no response exists.
func TestOwnerRecycleLedgerCancellationPreservesUnknownIssuance(t *testing.T) {
	f, _, parent, args := ownerRecycleDeviceCommandFixture(t)
	ctx, cancel := context.WithCancel(parent)
	f.beforeSign = cancel
	var out, diagnostic bytes.Buffer
	if runOwnerRecycleCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke) == 0 || out.Len() != 0 {
		t.Fatal("canceled device issuance returned a reply")
	}
	if runOwnerRecycleCommandWithAdapter(parent, args, &out, &diagnostic, f.invoke) == 0 || f.signs.Load() != 1 || f.prepares.Load() != 1 {
		t.Fatal("canceled issuance reopened a new device attempt")
	}
}

// A local canonical fixture retains the real dispatcher, archive reads,
// signatures, durable host owner and HTTP post implementation.
type ownerRecycleSubmissionFixture struct {
	f          *ownerRecycleTestFixture
	native     *rootReceiptFixture
	common     []string
	approval   ownerRecycleSubmissionApproval
	policyPath string
	policyHash string
	signed     []byte
}

// All fixture approvals are synthetic and independently signed after export.
func newOwnerRecycleSubmissionFixture(t *testing.T) *ownerRecycleSubmissionFixture {
	t.Helper()
	f := newOwnerRecycleTestFixture(t, true)
	chain, native, _, signed := ownerRecycleTestChain(t, f, 2, false)
	f.config.Route.RpcUrl = chain.client.url
	f.approve()
	directory := filepath.Dir(f.config.Action.StatePath)
	raw, _ := json.Marshal(f.config)
	configPath, _ := ownerRecycleTestFile(t, directory, "config.json", raw)
	self := &ownerRecycleSubmissionFixture{f: f, native: native, signed: signed,
		common: []string{"--config", configPath, "--approval-key", f.key, "--accept-action-hash", f.config.Action.RequestHash}}
	store, err := openOwnerRecycleStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody := ownerRecycleCustody{config: f.config, key: f.key, store: store}
	request, err := custody.export(f.input.Metadata, f.input.LedgerMetadata)
	if err == nil {
		_, err = custody.importSignature(request.ContentHash, f.signature(t))
	}
	if err := errors.Join(err, store.close()); err != nil {
		t.Fatal(err)
	}
	output, code, diagnostic := self.command("submit-plan", "--production-authority-hash", rootObjectHash("synthetic live authority"))
	var template struct {
		Approval     ownerRecycleSubmissionApproval `json:"approval_template"`
		SigningBytes string                         `json:"signing_bytes"`
	}
	if code != 0 || decodePlanJson(output, &template) != nil || template.SigningBytes != "0x"+hex.EncodeToString(template.Approval.signingBytes()) {
		t.Fatal("public submission template failed", code, diagnostic)
	}
	self.approval = template.Approval
	self.approve(t)
	native.fault = func(method string, params []json.RawMessage, _ int) (any, bool) {
		if method != "author_submitExtrinsic" {
			return nil, false
		}
		var posted string
		json.Unmarshal(params[0], &posted)
		record, err := self.readRecord()
		if err != nil {
			t.Error("post has no readable retained intent", err)
			return nil, true
		}
		if posted != "0x"+hex.EncodeToString(self.signed) || record.Submission == nil || record.Submission.Attempts != 1 {
			t.Error("post escaped original bytes or its durable numbered reservation")
		}
		return record.ExtrinsicHash, true
	}
	return self
}

// File pins change with a new candidate, while retained policy cannot change.
func (self *ownerRecycleSubmissionFixture) approve(t *testing.T) {
	t.Helper()
	self.approval.Signature = hex.EncodeToString(ed25519.Sign(self.f.approval, self.approval.signingBytes()))
	raw, _ := json.Marshal(self.approval)
	self.policyPath, self.policyHash = ownerRecycleTestFile(t, filepath.Dir(self.f.config.Action.StatePath), "submission.json", raw)
}

// Commands execute through the real main dispatcher with admitted test storage.
func (self *ownerRecycleSubmissionFixture) command(mode string, extra ...string) ([]byte, int, string) {
	args := append([]string{"owner-recycle", mode}, self.common...)
	args = append(args, extra...)
	var out, diagnostic bytes.Buffer
	code := runMain(self.f.storage.Context, args, &out, &diagnostic)
	return out.Bytes(), code, diagnostic.String()
}

// Independent trust inputs must be supplied again on every resumed submit.
func (self *ownerRecycleSubmissionFixture) submit() ([]byte, int, string) {
	return self.command("submit", "--submission-policy", self.policyPath, "--submission-policy-sha256", self.policyHash,
		"--submission-approval-key", self.f.key, "--production-authority-hash", self.approval.AuthorityHash)
}

// Read raw output independently of the production store to inspect failures.
func (self *ownerRecycleSubmissionFixture) record(t *testing.T) ownerRecycleRecord {
	t.Helper()
	record, err := self.readRecord()
	if err != nil {
		t.Fatal("retained recycle record unreadable", err)
	}
	return record
}

// Handler boundaries return errors instead of exiting their HTTP goroutine.
func (self *ownerRecycleSubmissionFixture) readRecord() (ownerRecycleRecord, error) {
	var record ownerRecycleRecord
	raw, err := os.ReadFile(self.f.config.Action.StatePath)
	if err == nil {
		err = decodePlanJson(raw, &record)
	}
	return record, err
}

// The fake peer counts actual posts, not a caller-side requested effect flag.
func (self *ownerRecycleSubmissionFixture) posts() int {
	self.native.stateLock.Lock()
	defer self.native.stateLock.Unlock()
	return self.native.counts["author_submitExtrinsic"]
}

// Actual canonical inclusion is installed after one post; an exhausted budget
// cannot prevent recovery of that original successful receipt and mode readback.
func (self *ownerRecycleSubmissionFixture) include(t *testing.T) {
	t.Helper()
	self.native.stateLock.Lock()
	defer self.native.stateLock.Unlock()
	action := self.f.config.Action
	height := action.BirthBlock + 3
	header, hash := rootReceiptHeaderFixture(t, self.native.finalized, height, [][]byte{{8, 4, 0}, self.signed}, false)
	self.native.headers[hash], self.native.byHeight[height], self.native.finalized = header, hash, hash
	self.native.bodies[hash] = []string{"0x080400", "0x" + hex.EncodeToString(self.signed)}
	entries, err := ownerRecycleEntries(self.f.metadata, action.Owner)
	if err != nil {
		t.Fatal(err)
	}
	self.native.storageKVs[entries["RecycleOrBurn"].key] = "0x01"
	self.native.storageKVs[entries["LastRateLimitedBlock"].key] = "0x" + hex.EncodeToString(binary.LittleEndian.AppendUint64(nil, height))
	account, _ := hex.DecodeString(self.native.storageKVs[entries["System.Account"].key][2:])
	binary.LittleEndian.PutUint32(account, action.Nonce+1)
	self.native.storageKVs[entries["System.Account"].key] = "0x" + hex.EncodeToString(account)
}

// This public path previously had no submit command. A post acknowledgment is
// still signed/pending; only original canonical inclusion qualifies mode.
func TestOwnerRecyclePublicSubmitAndExhaustedReceiptRecovery(t *testing.T) {
	f := newOwnerRecycleSubmissionFixture(t)
	out, code, diagnostic := f.submit()
	var result ownerRecycleResult
	if code != 0 || decodePlanJson(out, &result) != nil || result.TransactionFinalized || result.ActivationReady || f.posts() != 1 {
		t.Fatal("approved original post failed or claimed inclusion", code, diagnostic)
	}
	if _, code, _ := f.submit(); code == 0 || f.posts() != 1 || f.record(t).Submission.Attempts != 1 {
		t.Fatal("bounded original submission allowance was replenished")
	}
	f.include(t)
	out, code, diagnostic = f.submit()
	if code != 0 || decodePlanJson(out, &result) != nil || !result.TransactionFinalized || !result.RecycleModeObserved || result.ActivationReady || f.posts() != 1 {
		t.Fatal("exhausted original action did not recover exact inclusion", code, diagnostic)
	}
}

// An uncertain acknowledgment consumes exactly one original allowance. Neither
// a new valid signature over more posts nor another invocation can replace it.
func TestOwnerRecyclePublicUncertainPostCannotRenewApproval(t *testing.T) {
	f := newOwnerRecycleSubmissionFixture(t)
	f.native.fault = func(method string, _ []json.RawMessage, _ int) (any, bool) {
		return "0x" + strings.Repeat("ba", 32), method == "author_submitExtrinsic"
	}
	if _, code, _ := f.submit(); code == 0 || f.posts() != 1 || f.record(t).Submission.Attempts != 1 {
		t.Fatal("uncertain acknowledgment was not retained as one consumed post")
	}
	f.approval.MaximumAttempts = 2
	f.approve(t)
	if _, code, _ := f.submit(); code == 0 || f.posts() != 1 || f.record(t).Submission.Approval.MaximumAttempts != 1 {
		t.Fatal("renewed approval replaced original cumulative liability")
	}
}

// A preapproved retry is another numbered post of identical bytes, not a
// transport retry or renewed budget. Each invocation still posts at most once.
func TestOwnerRecyclePublicApprovedRetryKeepsOriginalBytes(t *testing.T) {
	f := newOwnerRecycleSubmissionFixture(t)
	f.approval.MaximumAttempts = 2
	f.approve(t)
	f.native.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if method != "author_submitExtrinsic" {
			return nil, false
		}
		var raw string
		json.Unmarshal(params[0], &raw)
		record, err := f.readRecord()
		if err != nil || record.Submission == nil || int(record.Submission.Attempts) != count || raw != "0x"+hex.EncodeToString(f.signed) {
			t.Error("approved retry changed its original bytes or unconsumed reservation", err)
			return nil, true
		}
		if count == 1 {
			return "0x" + strings.Repeat("ba", 32), true
		}
		return record.ExtrinsicHash, true
	}
	if _, code, _ := f.submit(); code == 0 || f.posts() != 1 {
		t.Fatal("first uncertain invocation automatically retried")
	}
	if _, code, diagnostic := f.submit(); code != 0 || f.posts() != 2 {
		t.Fatal("separately invoked approved retry failed", code, diagnostic)
	}
	if _, code, _ := f.submit(); code == 0 || f.posts() != 2 {
		t.Fatal("original retry allowance was renewed")
	}
}

// Real RPC admission must distinguish every changed current predicate while
// preserving original signed bytes and leaving the post allowance unused.
func TestOwnerRecyclePublicSubmissionRejectsCurrentStateDrift(t *testing.T) {
	for _, change := range []string{"owner", "generation", "nonce", "mode", "funds", "pending-epoch", "rate", "runtime", "birth"} {
		f := newOwnerRecycleSubmissionFixture(t)
		entries, err := ownerRecycleEntries(f.f.metadata, f.f.config.Action.Owner)
		if err != nil {
			t.Fatal(err)
		}
		f.native.fault = func(method string, params []json.RawMessage, _ int) (any, bool) {
			if method == "state_getRuntimeVersion" && change == "runtime" {
				version := f.f.config.Action.Policy.RuntimeVersion
				version.SpecVersion++
				return version, true
			}
			if method != "state_getStorage" {
				return nil, false
			}
			var key, block string
			json.Unmarshal(params[0], &key)
			json.Unmarshal(params[1], &block)
			if change == "birth" && block == f.f.config.Action.BirthHash && key == entries["LastEpochBlock"].key {
				return "0x" + hex.EncodeToString(binary.LittleEndian.AppendUint64(nil, 95)), true
			}
			if block != f.native.finalized {
				return nil, false
			}
			name := map[string]string{"owner": "SubnetOwner", "generation": "NetworkRegisteredAt", "nonce": "System.Account", "mode": "RecycleOrBurn", "funds": "System.Account", "pending-epoch": "PendingEpochAt", "rate": "LastRateLimitedBlock"}[change]
			if name == "" || key != entries[name].key {
				return nil, false
			}
			var raw []byte
			switch change {
			case "owner":
				raw = bytes.Repeat([]byte{0xaa}, 32)
			case "generation":
				raw = binary.LittleEndian.AppendUint64(nil, 11)
			case "mode":
				raw = []byte{1}
			case "pending-epoch":
				raw = binary.LittleEndian.AppendUint64(nil, 106)
			case "rate":
				raw = binary.LittleEndian.AppendUint64(nil, 101)
			case "nonce", "funds":
				raw, _ = hex.DecodeString(f.native.storageKVs[key][2:])
				if change == "nonce" {
					binary.LittleEndian.PutUint32(raw, f.f.config.Action.Nonce+1)
				} else {
					binary.LittleEndian.PutUint64(raw[16:], 0)
				}
			}
			return "0x" + hex.EncodeToString(raw), true
		}
		before := f.record(t).RawExtrinsic
		_, code, diagnostic := f.submit()
		record := f.record(t)
		// A consumed foreign nonce is a reconciled conflict, not a retryable
		// send error. Both outcomes must leave the same zero post count.
		if change != "nonce" && code == 0 || f.posts() != 0 || record.Submission == nil || record.Submission.Attempts != 0 || record.RawExtrinsic != before {
			t.Fatal("current drift escaped submission admission", change, code, diagnostic)
		}
	}
}

// Replacing the physical marker or changing finality exactly after reservation
// refuses the post while retaining the already consumed numbered attempt.
func TestOwnerRecyclePublicSubmissionFinalFenceConsumesWithoutSending(t *testing.T) {
	for _, change := range []string{"head", "marker"} {
		f := newOwnerRecycleSubmissionFixture(t)
		fired := false
		f.native.fault = func(method string, _ []json.RawMessage, _ int) (any, bool) {
			if method != "chain_getFinalizedHead" || fired {
				return nil, false
			}
			record, err := f.readRecord()
			if err != nil {
				t.Error("final fence lost its retained intent", err)
				return nil, true
			}
			if record.Submission == nil || record.Submission.Attempts == 0 {
				return nil, false
			}
			fired = true
			if change == "marker" {
				path := f.f.config.Action.StatePath + ".lock"
				raw, err := os.ReadFile(path)
				if err == nil {
					err = os.Rename(path, path+".detached")
				}
				if err == nil {
					err = os.WriteFile(path, raw, 0600)
				}
				if err != nil {
					t.Error("synthetic marker replacement failed", err)
				}
				return nil, false
			}
			return "0x" + strings.Repeat("bc", 32), true
		}
		if _, code, _ := f.submit(); code == 0 || !fired || f.posts() != 0 || f.record(t).Submission.Attempts != 1 {
			t.Fatal("late finality or original custody loss reached a post", change, code)
		}
	}
}

// Action approval is not submission approval; each signature, authority pin,
// mandatory residual and finite ceiling is checked before any route request.
func TestOwnerRecyclePublicSubmissionRequiresIndependentAuthority(t *testing.T) {
	for _, change := range []string{"signature", "authority", "residual", "attempts", "era"} {
		f := newOwnerRecycleSubmissionFixture(t)
		switch change {
		case "signature":
			f.approval.Signature = f.f.config.Signature
		case "authority":
			f.approval.AuthorityHash = ""
		case "residual":
			f.approval.ResidualRisks = f.approval.ResidualRisks[:1]
		case "attempts":
			f.approval.MaximumAttempts = 9
		case "era":
			f.approval.ValidThroughBlock = f.f.config.Action.BirthBlock + f.f.config.Action.Period
		}
		if change != "signature" {
			f.approve(t)
		} else {
			raw, _ := json.Marshal(f.approval)
			f.policyPath, f.policyHash = ownerRecycleTestFile(t, filepath.Dir(f.f.config.Action.StatePath), "submission.json", raw)
		}
		if _, code, _ := f.submit(); code == 0 || f.posts() != 0 || f.record(t).Submission != nil {
			t.Fatal("invalid independent authority acquired submission custody", change)
		}
	}
}
