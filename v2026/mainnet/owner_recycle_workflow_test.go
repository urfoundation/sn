//go:build linux

// Public preparation and dispatcher continuations retain one original owner
// request across separate computers. Only the device boundary is synthetic.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Review must work before either computer provisions custody. The top-level
// dispatcher may not impose deployment storage on a portable public request.
func TestOwnerRecyclePortableCommandsPrecedeCustody(t *testing.T) {
	f := newOwnerRecycleTestFixture(t, true)
	chain, native, _, _ := ownerRecycleTestChain(t, f, 2, false)
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	invoke := func(args ...string) []byte {
		t.Helper()
		var output, diagnostic bytes.Buffer
		if code := runMain(t.Context(), append([]string{"owner-recycle"}, args...), &output, &diagnostic); code != 0 {
			t.Fatalf("portable %s required custody: %d %s", args[0], code, diagnostic.String())
		}
		return bytes.Clone(output.Bytes())
	}
	input, _ := json.Marshal(f.input)
	inputPath, _ := ownerRecycleTestFile(t, directory, "unsigned-input.json", input)
	var unsigned ownerRecycleConfig
	if err := decodePlanJson(invoke("plan", "--input", inputPath), &unsigned); err != nil || unsigned.Signature != "" || unsigned.Action.RequestHash != f.config.Action.RequestHash {
		t.Fatal("portable planning changed scope or inferred approval", err)
	}
	raw, _ := json.Marshal(f.config.Action.Policy)
	policyPath, _ := ownerRecycleTestFile(t, directory, "observation-policy.json", raw)
	var observation ownerRecycleObservation
	if err := decodePlanJson(invoke("observe", "--policy", policyPath, "--owner-account-id", f.config.Action.Owner, "--rpc", chain.client.url), &observation); err != nil || observation.Owner != f.config.Action.Owner || observation.FinalizedNumber != f.config.Action.BirthBlock+2 {
		t.Fatal("public observation required retained custody or changed its owner", err)
	}
	request, err := newOwnerRecycleSigningRequest(f.config, f.key, f.input.Metadata, f.input.LedgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(request)
	requestPath, _ := ownerRecycleTestFile(t, directory, "portable-request.json", raw)
	trust := []string{"--request", requestPath, "--accept-request-hash", request.ContentHash, "--approval-key", f.key,
		"--owner-account-id", f.config.Action.Owner, "--expected-genesis", f.config.Action.Policy.GenesisHash}
	var inspected ownerRecycleSigningRequest
	if err := decodePlanJson(invoke(append([]string{"inspect-request"}, trust...)...), &inspected); err != nil || inspected.ContentHash != request.ContentHash {
		t.Fatal("portable request inspection changed original scope", err)
	}
	proofPath, proofHash := ownerRecycleTestFile(t, directory, "synthetic-proof.bin", bytes.Repeat([]byte{29}, 330))
	args := append([]string{"ledger-plan"}, trust...)
	args = append(args, "--metadata-proof", proofPath, "--metadata-proof-sha256", proofHash)
	var transcript ownerLedgerTranscript
	if err := decodePlanJson(invoke(args...), &transcript); err != nil || transcript.Signing || transcript.NetworkEffects || transcript.DeviceQualified {
		t.Fatal("portable transcript gained device or execution authority", err)
	}
	if _, err := os.Stat(f.config.Action.StatePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("portable review claimed host action custody", err)
	}
	native.stateLock.Lock()
	posts := native.counts["author_submitExtrinsic"]
	native.stateLock.Unlock()
	if posts != 0 {
		t.Fatal("portable review submitted an extrinsic")
	}
}

// Every actual owner or host operation must still be stopped by the public
// declaration gate before parsing its independent action or device inputs.
func TestOwnerRecycleEffectsStillRequireCustodyDeclaration(t *testing.T) {
	for _, mode := range []string{"sign", "reserve", "export", "import", "import-reply", "status", "reconcile", "submit-plan", "submit"} {
		var output, diagnostic bytes.Buffer
		if code := runMain(t.Context(), []string{"owner-recycle", mode}, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "durable custody declaration") {
			t.Fatal("owner operation bypassed the public durable declaration", mode, code, diagnostic.String())
		}
	}
}

// Fresh host and owner namespaces come from real accepted preparation plans,
// not fixture enrollment. Reply recovery uses the ordinary public sign route.
func TestOwnerRecyclePreparedOwnerAndHostWorkflow(t *testing.T) {
	hostPreparation := newStoragePreparationCommandFixture(t)
	hostInputs, _ := json.Marshal(storageSnapshotPreparationScope{Schema: "urnetwork-snapshot-preparation-v1", Name: ownerRecycleStateFile, MaximumBytes: ownerRecycleStoreLimit})
	storagePreparationOwnerRequest(t, hostPreparation, "daemon", []durablevolume.PreparationOwner{{Kind: "mainnet-owner-recycle", RelativePath: ".", Purpose: "fresh", Inputs: hostInputs}})
	hostContext := storagePreparationApplyOwnerCommand(t, hostPreparation, "storage-prepare")
	hostReference, present := durablevolume.ReferenceFromContext(hostContext)
	if !present {
		t.Fatal("public host preparation omitted its declaration")
	}
	ownerPreparation := newStoragePreparationCommandFixture(t)
	ownerInputs, _ := json.Marshal(storageSnapshotPreparationScope{Schema: "urnetwork-snapshot-preparation-v1", Name: "recycle-device.json", MaximumBytes: ownerSigningReplyLimit})
	storagePreparationOwnerRequest(t, ownerPreparation, "owner-local", []durablevolume.PreparationOwner{{Kind: "mainnet-owner-signing", RelativePath: ".", Purpose: "fresh", Inputs: ownerInputs}})
	ownerContext := storagePreparationApplyOwnerCommand(t, ownerPreparation, "storage-owner-prepare")
	ownerReference, present := durablevolume.ReferenceFromContext(ownerContext)
	if !present {
		t.Fatal("public owner preparation omitted its declaration")
	}
	f := newOwnerRecycleTestFixture(t, true)
	f.input.Action.StatePath = filepath.Join(hostPreparation.root, ownerRecycleStateFile)
	chain, native, _, signed := ownerRecycleTestChain(t, f, 2, false)
	f.config.Route.RpcUrl = chain.client.url
	f.approve()
	raw, _ := json.Marshal(f.config)
	configPath, _ := ownerRecycleTestFile(t, hostPreparation.metadata, "approved-action.json", raw)
	metadataPath, _ := ownerRecycleTestFile(t, hostPreparation.metadata, "metadata14.hex", []byte(f.input.Metadata))
	ledgerPath, _ := ownerRecycleTestFile(t, hostPreparation.metadata, "metadata15.hex", []byte(f.input.LedgerMetadata))
	common := []string{"--config", configPath, "--approval-key", f.key, "--accept-action-hash", f.config.Action.RequestHash,
		"--durable-volumes", hostReference.Path, "--durable-volumes-sha256", hostReference.Sha256}
	invokeHost := func(mode string, extra ...string) []byte {
		t.Helper()
		args := append([]string{"owner-recycle", mode}, common...)
		args = append(args, extra...)
		var output, diagnostic bytes.Buffer
		ctx := durablepath.WithHost(t.Context(), hostPreparation.storage.Host)
		if code := runMain(ctx, args, &output, &diagnostic); code != 0 {
			t.Fatalf("prepared host %s failed: %d %s", mode, code, diagnostic.String())
		}
		return bytes.Clone(output.Bytes())
	}
	invokeHost("reserve")
	raw = invokeHost("export", "--metadata", metadataPath, "--ledger-metadata", ledgerPath)
	var request ownerRecycleSigningRequest
	if err := decodePlanJson(raw, &request); err != nil {
		t.Fatal(err)
	}
	requestPath, _ := ownerRecycleTestFile(t, ownerPreparation.metadata, "portable-request.json", raw)
	device := ownerSigningDeviceConfig{StatePath: filepath.Join(ownerPreparation.root, "recycle-device.json"), PythonPath: "/reviewed/python3",
		HelperPath: filepath.Join(ownerPreparation.metadata, "helper.py"), HelperHash: rootObjectHash("synthetic reviewed helper"),
		BackendPath: filepath.Join(ownerPreparation.metadata, "backend.so"), BackendHash: rootObjectHash("synthetic reviewed backend"), AppVersion: [3]uint16{100, 0, 5}}
	signArgs := []string{"sign", "--request", requestPath, "--accept-request-hash", request.ContentHash, "--approval-key", f.key,
		"--owner-account-id", f.config.Action.Owner, "--expected-genesis", f.config.Action.Policy.GenesisHash,
		"--owner-state", device.StatePath, "--ledger-python", device.PythonPath, "--ledger-helper", device.HelperPath,
		"--ledger-helper-sha256", device.HelperHash, "--ledger-backend", device.BackendPath, "--ledger-backend-sha256", device.BackendHash,
		"--ledger-app-version", "100.0.5"}
	// The operator journal is unavailable while the owner uses the portable
	// request. Restoring its original directory preserves the original inode.
	detached := hostPreparation.root + "-offline"
	if err := os.Rename(hostPreparation.root, detached); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := os.Stat(detached); err == nil {
			if err := os.Rename(detached, hostPreparation.root); err != nil {
				t.Error("cannot restore synthetic original host custody", err)
			}
		}
	})
	adapter := &ownerRecycleDeviceFixture{f: f, request: request, lost: true}
	var replyOutput, diagnostic bytes.Buffer
	if code := runOwnerRecycleCommandWithAdapter(ownerContext, signArgs, &replyOutput, &diagnostic, adapter.invoke); code != 0 {
		t.Fatal("prepared owner cannot sign portable original request", code, diagnostic.String())
	}
	var reply ownerSigningReply
	if err := decodePlanJson(replyOutput.Bytes(), &reply); err != nil || reply.RequestHash != request.ContentHash {
		t.Fatal("prepared owner lost original public reply", err)
	}
	replayArgs := append([]string{"owner-recycle"}, signArgs...)
	replayArgs = append(replayArgs, "--durable-volumes", ownerReference.Path, "--durable-volumes-sha256", ownerReference.Sha256)
	var replay bytes.Buffer
	diagnostic.Reset()
	if code := runMain(durablepath.WithHost(t.Context(), ownerPreparation.storage.Host), replayArgs, &replay, &diagnostic); code != 0 || !bytes.Equal(replay.Bytes(), replyOutput.Bytes()) || adapter.signs.Load() != 1 || adapter.prepares.Load() != 1 {
		t.Fatal("public sign restart required a device or changed original reply", code, diagnostic.String())
	}
	if _, err := os.Stat(hostPreparation.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owner command created or opened operator custody", err)
	}
	if err := os.Rename(detached, hostPreparation.root); err != nil {
		t.Fatal(err)
	}
	replyPath, replyHash := ownerRecycleTestFile(t, hostPreparation.metadata, "owner-reply.json", replyOutput.Bytes())
	invokeHost("import-reply", "--accept-request-hash", request.ContentHash, "--reply", replyPath, "--reply-sha256", replyHash)
	authority := rootObjectHash("synthetic separately reviewed production authority")
	var template struct {
		Approval     ownerRecycleSubmissionApproval `json:"approval_template"`
		SigningBytes string                         `json:"signing_bytes"`
	}
	if err := decodePlanJson(invokeHost("submit-plan", "--production-authority-hash", authority), &template); err != nil || template.Approval.MaximumAttempts != 1 || template.SigningBytes != "0x"+hex.EncodeToString(template.Approval.signingBytes()) {
		t.Fatal("public submission template lost exact bytes or one-post default", err)
	}
	submissionKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{77}, ed25519.SeedSize))
	template.Approval.Signature = hex.EncodeToString(ed25519.Sign(submissionKey, template.Approval.signingBytes()))
	raw, _ = json.Marshal(template.Approval)
	approvalPath, approvalHash := ownerRecycleTestFile(t, hostPreparation.metadata, "separate-submission-approval.json", raw)
	retained := &ownerRecycleSubmissionFixture{f: f, native: native, signed: signed}
	native.fault = func(method string, params []json.RawMessage, _ int) (any, bool) {
		if method != "author_submitExtrinsic" {
			return nil, false
		}
		var posted string
		if len(params) != 1 || json.Unmarshal(params[0], &posted) != nil {
			t.Error("public post changed its single original argument")
			return nil, true
		}
		record, err := retained.readRecord()
		if err != nil || record.Submission == nil || record.Submission.Attempts != 1 || posted != reply.RawExtrinsic || record.ExtrinsicHash != reply.ExtrinsicHash {
			t.Error("public post escaped original reply or its durable single reservation", err)
			return nil, true
		}
		return reply.ExtrinsicHash, true
	}
	var result ownerRecycleResult
	raw = invokeHost("submit", "--submission-policy", approvalPath, "--submission-policy-sha256", approvalHash,
		"--submission-approval-key", "0x"+hex.EncodeToString(submissionKey.Public().(ed25519.PublicKey)), "--production-authority-hash", authority)
	if err := decodePlanJson(raw, &result); err != nil || result.TransactionFinalized || result.ActivationReady || retained.posts() != 1 {
		t.Fatal("one approved public post gained finality or activation", err)
	}
	retained.include(t)
	if err := decodePlanJson(invokeHost("reconcile"), &result); err != nil || !result.TransactionFinalized || !result.RecycleModeObserved || result.ActivationReady || retained.posts() != 1 {
		t.Fatal("retained reconciliation lost original finality or resubmitted", err)
	}
	record := retained.record(t)
	if record.Request.ContentHash != request.ContentHash || record.RawExtrinsic != reply.RawExtrinsic || record.Submission.Attempts != 1 || record.Submission.ApprovalKey == f.key {
		t.Fatal("completed workflow replaced original request, signature or independent approval")
	}
}
