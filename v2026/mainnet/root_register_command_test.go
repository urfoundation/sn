// Public root-registration commands retain exact public bytes across the
// operator handoff. All accounts, signatures and files are synthetic fixtures.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// External operator hardware can return either native signature scheme without
// Ledger metadata, derivation or executable adapter configuration.
func TestRootRegisterPortableHardwarePublicHandoff(t *testing.T) {
	for _, scheme := range []string{"sr25519", "ed25519"} {
		f := newRootRegisterTestFixture(t, scheme == "ed25519")
		if scheme == "ed25519" {
			f.input.Action.SigningProfile = rootRegisterPortableHardware
			f.input.Action.MetadataDigest, f.input.Action.LedgerMetadataHash, f.input.Action.DerivationPath = "", "", ""
			f.input.LedgerMetadata = ""
			var err error
			f.config, err = prepareRootRegisterPlan(f.input)
			if err != nil {
				t.Fatal(err)
			}
			f.approve()
		}
		directory := filepath.Dir(f.config.Action.StatePath)
		configRaw, _ := json.Marshal(f.config)
		configPath, _ := ownerRecycleTestFile(t, directory, "portable-approved.json", configRaw)
		metadataPath, _ := ownerRecycleTestFile(t, directory, "portable-metadata.hex", []byte(f.input.Metadata))
		common := []string{"--config", configPath, "--approval-key", f.key, "--accept-action-hash", f.config.Action.RequestHash}
		invoke := func(mode string, extra ...string) []byte {
			t.Helper()
			args := append([]string{"root-register", mode}, common...)
			args = append(args, extra...)
			var output, diagnostic bytes.Buffer
			if code := runMain(f.storage.Context, args, &output, &diagnostic); code != 0 {
				t.Fatal("portable hardware command failed", scheme, mode, code, diagnostic.String())
			}
			return bytes.Clone(output.Bytes())
		}
		invoke("reserve")
		var request rootRegisterSigningRequest
		if err := decodePlanJson(invoke("export", "--metadata", metadataPath), &request); err != nil || request.Config.Action.SigningProfile != rootRegisterPortableHardware || request.Config.Action.SignatureScheme != scheme || request.LedgerMetadata != "" {
			t.Fatal("portable export acquired Ledger configuration", scheme, err)
		}
		trust := ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: f.key, Owner: f.config.Action.Policy.Operator, Genesis: f.config.Action.Policy.GenesisHash}
		adapterCalls := 0
		adapter := ownerSigningAdapter(func(context.Context, ownerSigningDeviceConfig, ownerSigningAdapterInput) (ownerSigningAdapterResult, error) {
			adapterCalls++
			return ownerSigningAdapterResult{}, nil
		})
		if _, err := signRootRegisterRequest(t.Context(), ownerSigningDeviceConfig{}, request, trust, "", adapter); err == nil || !strings.Contains(err.Error(), "portable operator hardware uses export/import") || adapterCalls != 0 {
			t.Fatal("portable profile borrowed a Ledger device", scheme, adapterCalls, err)
		}
		signaturePath, signatureHash := ownerRecycleTestFile(t, directory, "portable-public-signature.hex", []byte(hex.EncodeToString(f.signature(t))))
		var result rootRegisterResult
		if err := decodePlanJson(invoke("import", "--signature", signaturePath, "--signature-file-sha256", signatureHash, "--accept-request-hash", request.ContentHash), &result); err != nil || result.Phase != "signed" || result.ActivationReady || result.RootSeatObserved {
			t.Fatal("portable operator signature did not retain one original signed action", scheme, result, err)
		}
		if scheme == "ed25519" {
			response := append([]byte{0}, f.signature(t)...)
			responsePath, responseHash := ownerRecycleTestFile(t, directory, "portable-ed25519-framed.hex", []byte(hex.EncodeToString(response)))
			args := append([]string{"root-register", "import"}, common...)
			args = append(args, "--signature", responsePath, "--signature-file-sha256", responseHash, "--accept-request-hash", request.ContentHash, "--ledger-response")
			var output, diagnostic bytes.Buffer
			if code := runMain(f.storage.Context, args, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "MultiSignature") {
				t.Fatal("portable Ed25519 profile accepted a Ledger-specific import", code, diagnostic.String())
			}
		}
	}
}

// Public command output can be lost after export/import while the original
// journal remains authoritative; request verification needs no embedded paths.
func TestRootRegisterPublicCliRoundTrip(t *testing.T) {
	f := newRootRegisterTestFixture(t, true)
	directory := filepath.Dir(f.config.Action.StatePath)
	inputRaw, _ := json.Marshal(f.input)
	inputPath, _ := ownerRecycleTestFile(t, directory, "input.json", inputRaw)
	var out, errOut bytes.Buffer
	if code := runMain(t.Context(), []string{"root-register", "plan", "--input", inputPath}, &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	var unsigned rootRegisterConfig
	if err := decodePlanJson(out.Bytes(), &unsigned); err != nil || unsigned.Signature != "" || unsigned.Action.RequestHash != f.config.Action.RequestHash {
		t.Fatal("plan inferred approval or changed action", err)
	}
	firstPlan := bytes.Clone(out.Bytes())
	out.Reset()
	errOut.Reset()
	if code := runMain(t.Context(), []string{"bootstrap-chain", "root-registration", "plan", "--input", inputPath}, &out, &errOut); code != 0 || !bytes.Equal(firstPlan, out.Bytes()) {
		t.Fatal("bootstrap precursor changed the exact registration planning domain", code, errOut.String())
	}
	configRaw, _ := json.Marshal(f.config)
	configPath, _ := ownerRecycleTestFile(t, directory, "approved.json", configRaw)
	metadataPath, _ := ownerRecycleTestFile(t, directory, "metadata.hex", []byte(f.input.Metadata))
	ledgerPath, _ := ownerRecycleTestFile(t, directory, "metadata15.hex", []byte(f.input.LedgerMetadata))
	common := []string{"--config", configPath, "--approval-key", f.key, "--accept-action-hash", f.config.Action.RequestHash}
	run := func(mode string, extra ...string) []byte {
		t.Helper()
		out.Reset()
		errOut.Reset()
		args := append([]string{mode}, common...)
		args = append(args, extra...)
		if code := runRootRegisterCommand(f.storage.Context, args, &out, &errOut); code != 0 {
			t.Fatal(mode, code, errOut.String())
		}
		return bytes.Clone(out.Bytes())
	}
	run("reserve")
	exported := run("export", "--metadata", metadataPath, "--ledger-metadata", ledgerPath)
	var request rootRegisterSigningRequest
	if err := decodePlanJson(exported, &request); err != nil {
		t.Fatal(err)
	}
	requestPath, _ := ownerRecycleTestFile(t, directory, "portable.json", exported)
	trust := []string{"--request", requestPath, "--accept-request-hash", request.ContentHash, "--approval-key", f.key, "--operator-account-id", f.config.Action.Policy.Operator, "--expected-genesis", f.config.Action.Policy.GenesisHash}
	out.Reset()
	errOut.Reset()
	if code := runMain(t.Context(), append([]string{"root-register", "inspect-request"}, trust...), &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	proofPath, proofHash := ownerRecycleTestFile(t, directory, "synthetic-proof.bin", bytes.Repeat([]byte{29}, 330))
	out.Reset()
	errOut.Reset()
	args := append([]string{"bootstrap-chain", "root-registration", "ledger-plan"}, trust...)
	args = append(args, "--metadata-proof", proofPath, "--metadata-proof-sha256", proofHash)
	if code := runMain(t.Context(), args, &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	var transcript ownerLedgerTranscript
	if err := decodePlanJson(out.Bytes(), &transcript); err != nil || transcript.Signing || transcript.NetworkEffects || transcript.DeviceQualified {
		t.Fatal("offline transcript claimed execution", err)
	}
	signature := f.signature(t)
	response := append([]byte{0}, signature...)
	signaturePath, signatureHash := ownerRecycleTestFile(t, directory, "public-ledger-response.hex", []byte(hex.EncodeToString(response)+"\n"))
	importArgs := []string{"--signature", signaturePath, "--signature-file-sha256", signatureHash, "--accept-request-hash", request.ContentHash, "--ledger-response"}
	run("import", importArgs...)
	run("import", importArgs...)
	var record rootRegisterRecord
	if err := decodePlanJson(run("status"), &record); err != nil || record.Phase != "signed" {
		t.Fatal("public signature not retained", err)
	}
	want, err := f.config.Action.signed(signature)
	if err != nil || record.RawExtrinsic != "0x"+hex.EncodeToString(want) {
		t.Fatal("CLI rewrote signed extrinsic", err)
	}
	// A portable request may be inspected after moving to an unrelated private
	// directory; only independently named inputs are opened on that computer.
	other := t.TempDir()
	os.Chmod(other, 0700)
	moved, _ := ownerRecycleTestFile(t, other, "request.json", exported)
	trust[1] = moved
	out.Reset()
	errOut.Reset()
	if code := runRootRegisterCommand(f.storage.Context, append([]string{"inspect-request"}, trust...), &out, &errOut); code != 0 {
		t.Fatal("portable inspection depended on host custody", errOut.String())
	}
}

// Pure portable review precedes provisioning, but every host/device state mode
// requires a durable declaration through both public dispatcher paths.
func TestRootRegisterEffectsRequireCustodyDeclaration(t *testing.T) {
	for _, prefix := range [][]string{{"root-register"}, {"bootstrap-chain", "root-registration"}} {
		for _, mode := range []string{"sign", "reserve", "export", "import", "import-reply", "status", "reconcile", "submit-plan", "submit", "bootstrap-handoff"} {
			var output, diagnostic bytes.Buffer
			args := append(append([]string{}, prefix...), mode)
			if code := runMain(t.Context(), args, &output, &diagnostic); code == 0 || output.Len() != 0 || !strings.Contains(diagnostic.String(), "durable custody declaration") {
				t.Fatal("registration effect bypassed public durable declaration", args, code, diagnostic.String())
			}
		}
	}
}

// A scripted stale tag occurs inside the census while the retained height/hash
// stays canonical. Coverage recovers without rescanning or replacing that cursor.
func TestRootRegisterObserveRecoversStaleFinalizedTag(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	chain, native, _, _ := rootRegisterTestChain(t, f, 3, false)
	selected := f.config.Action.BirthBlock + 2
	selectedHash := native.byHeight[selected]
	native.finalized = selectedHash
	baseFault := native.fault
	moved := false
	storageReadsAtStale := 0
	native.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if method == "state_getStorage" && !moved {
			native.finalized = native.byHeight[selected-1]
			moved = true
		}
		if method == "chain_getFinalizedHead" && count == 2 && moved {
			stale := native.finalized
			native.finalized = selectedHash
			storageReadsAtStale = native.counts["state_getStorage"]
			return stale, true
		}
		if baseFault != nil {
			return baseFault(method, params, count)
		}
		return nil, false
	}
	policyRaw, _ := json.Marshal(f.config.Action.Policy)
	path, _ := ownerRecycleTestFile(t, filepath.Dir(f.config.Action.StatePath), "observe-policy.json", policyRaw)
	args := []string{"root-register", "observe", "--policy", path, "--operator-account-id", f.config.Action.Policy.Operator, "--rpc", chain.client.url}
	var output, diagnostic bytes.Buffer
	code := runMain(t.Context(), args, &output, &diagnostic)
	native.stateLock.Lock()
	changed, posts, heads, storageReads := moved, native.counts["author_submitExtrinsic"], native.counts["chain_getFinalizedHead"], native.counts["state_getStorage"]
	native.stateLock.Unlock()
	if code != 0 || !changed || posts != 0 || heads != 3 || storageReadsAtStale == 0 || storageReadsAtStale != storageReads {
		t.Fatal("stale finality did not recover inside the original census", code, diagnostic.String(), changed, posts, heads, storageReadsAtStale, storageReads)
	}
	var observation rootRegisterObservation
	if err := decodePlanJson(output.Bytes(), &observation); err != nil || observation.FinalizedNumber != selected || observation.FinalizedHash != selectedHash {
		t.Fatal("recovered finality changed the selected observation", err)
	}
}

// Normal forward finality leaves the original fully pinned storage snapshot
// intact; portable observation needs no durable journal and never posts.
func TestRootRegisterObserveAllowsFinalizedAdvance(t *testing.T) {
	f := newRootRegisterTestFixture(t, false)
	chain, native, _, _ := rootRegisterTestChain(t, f, 3, false)
	selected := f.config.Action.BirthBlock + 2
	selectedHash := native.byHeight[selected]
	native.finalized = selectedHash
	baseFault := native.fault
	moved := false
	native.fault = func(method string, params []json.RawMessage, count int) (any, bool) {
		if method == "state_getStorage" && !moved {
			native.finalized = native.byHeight[selected+1]
			moved = true
		}
		if baseFault != nil {
			return baseFault(method, params, count)
		}
		return nil, false
	}
	policyRaw, _ := json.Marshal(f.config.Action.Policy)
	path, _ := ownerRecycleTestFile(t, filepath.Dir(f.config.Action.StatePath), "observe-policy.json", policyRaw)
	args := []string{"bootstrap-chain", "root-registration", "observe", "--policy", path, "--operator-account-id", f.config.Action.Policy.Operator, "--rpc", chain.client.url}
	var output, diagnostic bytes.Buffer
	if code := runMain(t.Context(), args, &output, &diagnostic); code != 0 {
		t.Fatal("advancing finality invalidated the pinned original observation", code, diagnostic.String())
	}
	var observation rootRegisterObservation
	if err := decodePlanJson(output.Bytes(), &observation); err != nil || observation.FinalizedNumber != selected || observation.FinalizedHash != selectedHash {
		t.Fatal("closing head replaced the selected storage snapshot", err)
	}
	native.stateLock.Lock()
	changed, posts := moved, native.counts["author_submitExtrinsic"]
	native.stateLock.Unlock()
	if !changed || posts != 0 {
		t.Fatal("observation did not exercise advancing finality or posted native bytes", changed, posts)
	}
}

// Runtime473 advertises the separate registration command only. A catalogue
// never grants current eligibility, operator custody or native submission.
func TestRootRegisterCapabilitiesRemainSeparateFromAuthority(t *testing.T) {
	_, metadata, digest, fileHash := rootCurrentTestMetadata(t)
	report, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, crv4.NativeOwnerSource473)
	if err != nil {
		t.Fatal(err)
	}
	registration := rootCurrentTestCall(t, report, "root_register")
	if !strings.Contains(registration.MutationAdapter, "root-register-v1") || !strings.Contains(report.MutationExecution, "separate-473-root-register-domain") ||
		report.NativeSigning || report.NetworkEffects || report.ActivationReady || report.CurrentRuntimeVerified || !report.PlanningOnly {
		t.Fatal("registration catalogue did not retain the separate execution boundary", registration, report)
	}
	legacy, err := inspectRootCurrentCapabilities(metadata, digest, fileHash, rootPassiveSource)
	if err != nil || rootCurrentTestCall(t, legacy, "root_register").MutationAdapter != "not-implemented-for-current-root" {
		t.Fatal("runtime470 catalogue acquired473 registration authority", err)
	}
}

// Invalid Ledger framing is rejected without guessing away variant/status bytes.
func TestRootRegisterPublicImportRejectsLedgerFraming(t *testing.T) {
	f := newRootRegisterTestFixture(t, true)
	store, err := openRootRegisterStore(f.config, f.key, true, f.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	custody := rootRegisterCustody{config: f.config, key: f.key, store: store}
	request, err := custody.export(f.input.Metadata, f.input.LedgerMetadata)
	store.close()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(f.config.Action.StatePath)
	configRaw, _ := json.Marshal(f.config)
	configPath, _ := ownerRecycleTestFile(t, directory, "config.json", configRaw)
	signature := f.signature(t)
	for _, response := range [][]byte{signature, append([]byte{1}, signature...), append(append([]byte{0}, signature...), 0x90, 0)} {
		path, digest := ownerRecycleTestFile(t, directory, "response.hex", []byte(hex.EncodeToString(response)))
		args := []string{"import", "--config", configPath, "--approval-key", f.key, "--accept-action-hash", f.config.Action.RequestHash, "--accept-request-hash", request.ContentHash, "--signature", path, "--signature-file-sha256", digest, "--ledger-response"}
		var out, errOut bytes.Buffer
		if code := runRootRegisterCommand(f.storage.Context, args, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "MultiSignature") {
			t.Fatal("ambiguous Ledger response accepted", code, errOut.String())
		}
	}
}
