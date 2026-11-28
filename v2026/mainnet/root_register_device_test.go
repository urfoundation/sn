// Public root registration signing uses synthetic accounts and a deterministic
// device boundary. No test needs physical hardware or a chain.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
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
type rootRegisterDeviceFixture struct {
	f               *rootRegisterTestFixture
	request         rootRegisterSigningRequest
	prepares        atomic.Int32
	signs           atomic.Int32
	omit            bool
	lost            bool
	responseAccount string
	beforeSign      func()
}

// Only the hardware boundary is replaced; all payload seams and custody remain.
func (self *rootRegisterDeviceFixture) invoke(ctx context.Context, config ownerSigningDeviceConfig, input ownerSigningAdapterInput) (ownerSigningAdapterResult, error) {
	action := self.request.Config.Action
	result := ownerSigningAdapterResult{Schema: ownerSigningAdapterSchema, Mode: input.Mode, RequestHash: self.request.ContentHash,
		SourceCommit: rootActionV1Source, MetadataDigest: action.MetadataDigest, ProofHash: rootObjectHash("synthetic root registration proof")}
	if input.RequestHash != self.request.ContentHash || input.MetadataHex != self.request.LedgerMetadata || input.Owner != action.Policy.Operator || input.Account != 7 || input.Index != 1 ||
		input.AppVersion != config.AppVersion || input.BackendPath != config.BackendPath || input.BackendHash != config.BackendHash ||
		len(input.IncludedExtrinsic) < 2 || len(input.IncludedSignedData) < 2 ||
		input.Call+input.IncludedExtrinsic[2:]+input.IncludedSignedData[2:] != action.Payload {
		return result, errors.New("root registration adapter changed exact original operator or payload seams")
	}
	if input.Mode == "prepare" {
		self.prepares.Add(1)
		return result, nil
	}
	self.signs.Add(1)
	raw, err := os.ReadFile(config.StatePath)
	var record ownerSigningDeviceRecord
	if err != nil || decodePlanJson(raw, &record) != nil || record.Schema != rootRegisterDeviceStateSchema || record.Phase != "signing" || record.RequestHash != input.RequestHash || record.ProofHash != input.ProofHash {
		return result, errors.New("root registration device reached before durable original signing intent")
	}
	if self.beforeSign != nil {
		self.beforeSign()
	}
	if self.omit {
		return result, errors.New("synthetic root registration device response unavailable")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	payload, _ := hex.DecodeString(action.Payload[2:])
	result.PublicKey, result.AppVersion = action.Policy.Operator, config.AppVersion
	if self.responseAccount != "" {
		result.PublicKey = self.responseAccount
	}
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

// Portable command inputs live apart from host custody and use an operator-local
// durable declaration. The fixture never opens a deployment path while signing.
func rootRegisterDeviceCommandFixture(t *testing.T) (*rootRegisterDeviceFixture, ownerSigningDeviceConfig, context.Context, []string) {
	t.Helper()
	f := newRootRegisterTestFixture(t, true)
	request, err := newRootRegisterSigningRequest(f.config, f.key, f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	config := ownerSigningTestDeviceConfig(t)
	ctx := ownerLocalDurableTestContext(t, filepath.Dir(config.StatePath))
	raw, _ := json.Marshal(request)
	requestPath, _ := ownerRecycleTestFile(t, filepath.Dir(config.StatePath), "portable-root-registration.json", raw)
	args := []string{"sign", "--request", requestPath, "--accept-request-hash", request.ContentHash, "--approval-key", f.key,
		"--operator-account-id", f.config.Action.Policy.Operator, "--expected-genesis", f.config.Action.Policy.GenesisHash,
		"--operator-derivation-path", f.config.Action.DerivationPath,
		"--operator-state", config.StatePath, "--ledger-python", config.PythonPath, "--ledger-helper", config.HelperPath,
		"--ledger-helper-sha256", config.HelperHash, "--ledger-backend", config.BackendPath,
		"--ledger-backend-sha256", config.BackendHash, "--ledger-app-version", "100.0.5"}
	return &rootRegisterDeviceFixture{f: f, request: request}, config, ctx, args
}

// Lost stdout recovers one exact reply while import-reply preserves the
// original exported host request and separate registration reply domain.
func TestRootRegisterPublicLedgerReplyRoundTripAndLostOutput(t *testing.T) {
	f, config, ctx, args := rootRegisterDeviceCommandFixture(t)
	f.lost = true
	var out, diagnostic bytes.Buffer
	if code := runRootRegisterCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke); code != 0 {
		t.Fatal(code, diagnostic.String())
	}
	var reply ownerSigningReply
	if err := decodePlanJson(out.Bytes(), &reply); err != nil || reply.Schema != rootRegisterReplySchema || f.signs.Load() != 1 || f.prepares.Load() != 1 {
		t.Fatal("original root registration device reply missing", err)
	}
	original, _ := os.ReadFile(config.StatePath)
	out.Reset()
	if code := runRootRegisterCommandWithAdapter(ctx, args, &out, &diagnostic, nil); code != 0 {
		t.Fatal("retained reply reopened hardware", diagnostic.String())
	}
	current, _ := os.ReadFile(config.StatePath)
	if !bytes.Equal(original, current) {
		t.Fatal("retained device recovery rewrote original custody")
	}
	store, err := openRootRegisterStore(f.f.config, f.f.key, true, f.f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody := rootRegisterCustody{config: f.f.config, key: f.f.key, store: store}
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
	replyPath, replyHash := ownerRecycleTestFile(t, directory, "operator-reply.json", raw)
	importArgs := []string{"root-register", "import-reply", "--config", configPath, "--approval-key", f.f.key,
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
	var retained rootRegisterRecord
	if err != nil || decodePlanJson(raw, &retained) != nil || retained.RawExtrinsic != reply.RawExtrinsic || retained.Request.ContentHash != f.request.ContentHash {
		t.Fatal("host import changed original root registration request or bytes", err)
	}
}

// An uncertain issuance must never return to reserved or call hardware again.
func TestRootRegisterLedgerUnknownIssuanceNeverReissues(t *testing.T) {
	f, _, ctx, args := rootRegisterDeviceCommandFixture(t)
	f.omit = true
	for attempt := 0; attempt < 2; attempt++ {
		var out, diagnostic bytes.Buffer
		if code := runRootRegisterCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke); code == 0 || out.Len() != 0 {
			t.Fatal("unresolved issuance produced a public reply")
		}
	}
	if f.signs.Load() != 1 || f.prepares.Load() != 1 {
		t.Fatal("unknown original signing was repeated")
	}
}

// A connected device returning the subnet owner or reserve identity cannot
// substitute for the explicitly selected operator, including during recovery.
func TestRootRegisterLedgerRefusesExcludedAccountResponse(t *testing.T) {
	for _, account := range []string{"subnet-owner", "reserve"} {
		f, _, ctx, args := rootRegisterDeviceCommandFixture(t)
		f.responseAccount = f.f.config.Action.Policy.SubnetOwner
		if account == "reserve" {
			f.responseAccount = f.f.config.Action.Policy.Reserve
		}
		for attempt := 0; attempt < 2; attempt++ {
			var output, diagnostic bytes.Buffer
			if code := runRootRegisterCommandWithAdapter(ctx, args, &output, &diagnostic, f.invoke); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "operator") {
				t.Fatal("excluded device account became the root operator", account, code, diagnostic.String())
			}
		}
		if f.signs.Load() != 1 || f.prepares.Load() != 1 {
			t.Fatal("excluded response triggered another device issue", account)
		}
	}
}

// Independent pins and domain-separated replies cannot be borrowed from trim
// or from another valid root registration request, even with a correct native signature.
func TestRootRegisterLedgerIndependentPinsAndReplyDomain(t *testing.T) {
	for _, flag := range []string{"--operator-account-id", "--expected-genesis", "--approval-key", "--accept-request-hash", "--operator-derivation-path"} {
		f, config, ctx, args := rootRegisterDeviceCommandFixture(t)
		for i := range args {
			if args[i] == flag {
				args[i+1] = "0x" + strings.Repeat("ab", 32)
				if flag == "--accept-request-hash" {
					args[i+1] = rootObjectHash("foreign root registration request")
				}
				if flag == "--operator-derivation-path" {
					args[i+1] = "m/44'/354'/1'/0'/0'"
				}
			}
		}
		var out, diagnostic bytes.Buffer
		if runRootRegisterCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke) == 0 || f.signs.Load() != 0 || f.prepares.Load() != 0 {
			t.Fatal("independent pin mismatch reached hardware", flag)
		}
		if _, err := os.Stat(config.StatePath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid portable request claimed device custody", err)
		}
	}
	f := newRootRegisterTestFixture(t, true)
	request, err := newRootRegisterSigningRequest(f.config, f.key, f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := newRootRegisterSigningReply(request, f.signature(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateRootRegisterSigningReply(request, reply); err != nil {
		t.Fatal("original registration reply is invalid", err)
	}
	for _, schema := range []string{ownerSigningReplySchema, ownerRecycleReplySchema, treasuryReplySchema} {
		reply.Schema = schema
		if _, err := validateRootRegisterSigningReply(request, reply); err == nil {
			t.Fatal("another reply domain acquired root registration authority", schema)
		}
	}
}

// A second command sees the first owner's permanent physical lock while the
// explicit adapter barrier proves that issuance is in progress.
func TestRootRegisterLedgerConcurrentOwner(t *testing.T) {
	f, _, ctx, args := rootRegisterDeviceCommandFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.beforeSign = func() { close(entered); <-release }
	finished := make(chan int, 1)
	go func() {
		var out, diagnostic bytes.Buffer
		finished <- runRootRegisterCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke)
	}()
	select {
	case <-entered:
	case code := <-finished:
		t.Fatal("first operator stopped before the signing barrier", code)
	}
	var out, diagnostic bytes.Buffer
	code := runRootRegisterCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke)
	close(release)
	first := <-finished
	if code == 0 || first != 0 || f.signs.Load() != 1 {
		t.Fatal("concurrent root registration device custody was not exclusive", code, first)
	}
}

// Cancellation after durable issuance intent remains unknown on restart and
// cannot trigger another device operation merely because no response exists.
func TestRootRegisterLedgerCancellationPreservesUnknownIssuance(t *testing.T) {
	f, _, parent, args := rootRegisterDeviceCommandFixture(t)
	ctx, cancel := context.WithCancel(parent)
	f.beforeSign = cancel
	var out, diagnostic bytes.Buffer
	if runRootRegisterCommandWithAdapter(ctx, args, &out, &diagnostic, f.invoke) == 0 || out.Len() != 0 {
		t.Fatal("canceled device issuance returned a reply")
	}
	if runRootRegisterCommandWithAdapter(parent, args, &out, &diagnostic, f.invoke) == 0 || f.signs.Load() != 1 || f.prepares.Load() != 1 {
		t.Fatal("canceled issuance reopened a new device attempt")
	}
}
