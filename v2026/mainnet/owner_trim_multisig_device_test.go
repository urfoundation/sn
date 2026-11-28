// Owner-local multisig signing uses the real durable device journal and, where
// noted, the real Python adapter with only its native SDK boundary replaced.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// The synthetic device derives whichever account a scenario selects, so a
// wrong device key is observable at the exact response boundary.
type ownerTrimMultisigDeviceAdapter struct {
	request   ownerSigningRequest
	signers   ownerTrimMultisigTestSigners
	device    string
	prepares  int
	signs     int
	loseReply bool
}

// The Go side must pass the named signer, the complete set and both seams.
func (self *ownerTrimMultisigDeviceAdapter) invoke(_ context.Context, config ownerSigningDeviceConfig, input ownerSigningAdapterInput) (ownerSigningAdapterResult, error) {
	action := self.request.Config.Action
	result := ownerSigningAdapterResult{Schema: ownerSigningAdapterSchema, Mode: input.Mode, RequestHash: self.request.ContentHash,
		SourceCommit: rootActionV1Source, MetadataDigest: action.MetadataDigest, ProofHash: rootObjectHash("synthetic multisig proof")}
	if input.Owner != action.Multisig.Signatory || input.MultisigAccount != action.Coldkey || input.MultisigThreshold != 2 ||
		!slices.Equal(input.MultisigSignatories, action.Multisig.Signatories) || input.Account != 1 || input.Index != 0 ||
		input.Call != action.Call || !strings.Contains(input.Call, action.Multisig.InnerCall[2:]) ||
		input.Call+input.IncludedExtrinsic[2:]+input.IncludedSignedData[2:] != action.Payload {
		return result, errors.New("adapter input changed the named signer, signer set or payload seams")
	}
	if input.Mode == "prepare" {
		self.prepares++
		return result, nil
	}
	self.signs++
	payload, _ := hex.DecodeString(action.Payload[2:])
	result.PublicKey, result.AppVersion = self.device, config.AppVersion
	result.Response = "0x" + hex.EncodeToString(append([]byte{0}, ed25519.Sign(self.signers.keys[self.device], payload)...))
	encoded, _ := json.Marshal(result)
	file, err := os.OpenFile(input.ResponsePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, err
	}
	_, err = file.Write(encoded)
	err = errors.Join(err, file.Sync(), file.Close())
	if self.loseReply {
		err = errors.Join(err, errors.New("synthetic process exit after durable response"))
	}
	return result, err
}

// A multisig first approval's request on the owner's own computer.
func ownerTrimMultisigTestRequest(t *testing.T) (*ownerTrimTestFixture, ownerTrimMultisigTestSigners, ownerSigningRequest) {
	t.Helper()
	f, signers, _ := newOwnerTrimMultisigTestFixture(t)
	request, err := newOwnerSigningRequest(f.config, f.key, f.metadata, f.ledgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	return f, signers, request
}

// Only the named signatory's device response becomes a reply. A wrong device
// account is retained as unresolved issuance and never prompts a second sign.
func TestOwnerTrimMultisigDeviceSignsOnlyAsNamedSignatory(t *testing.T) {
	for _, scenario := range []string{"named", "other-signatory"} {
		_, signers, request := ownerTrimMultisigTestRequest(t)
		config := ownerSigningTestDeviceConfig(t)
		adapter := &ownerTrimMultisigDeviceAdapter{request: request, signers: signers, device: request.Config.Action.Multisig.Signatory}
		if scenario == "other-signatory" {
			adapter.device = signers.accounts[0]
		}
		for attempt := 0; attempt < 2; attempt++ {
			reply, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke)
			if scenario == "named" {
				if err != nil || reply.Signatory != request.Config.Action.Multisig.Signatory || reply.Owner != signers.owner {
					t.Fatal("named signatory reply lost its signer or owner", err)
				}
				if _, err := reply.validate(request); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "retained owner device response differs") {
				t.Fatal("a device deriving another signatory produced a reply", err)
			}
		}
		if adapter.signs != 1 || adapter.prepares != 1 {
			t.Fatal(scenario, "device issuance repeated", adapter.prepares, adapter.signs)
		}
	}
}

// A crash after the durable device response but before the owner journal
// records the reply recovers the original bytes without touching the device.
func TestOwnerTrimMultisigOwnerJournalSurvivesCrashBeforeImport(t *testing.T) {
	_, signers, request := ownerTrimMultisigTestRequest(t)
	config := ownerSigningTestDeviceConfig(t)
	ctx := ownerLocalDurableTestContext(t, filepath.Dir(config.StatePath))
	store, err := openOwnerSigningDeviceStore(ctx, config, request)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	record.Phase, record.ProofHash = "signing", rootObjectHash("synthetic multisig proof")
	if err := store.save(record); err != nil {
		t.Fatal(err)
	}
	// The device answered durably; the process then stopped before recording it.
	device := &ownerTrimMultisigDeviceAdapter{request: request, signers: signers, device: request.Config.Action.Multisig.Signatory}
	input := ownerSigningAdapterInput{Mode: "sign", ResponsePath: config.StatePath + ".ledger-response"}
	payload, _ := hex.DecodeString(request.Config.Action.Payload[2:])
	response := ownerSigningAdapterResult{Schema: ownerSigningAdapterSchema, Mode: "sign", RequestHash: request.ContentHash, SourceCommit: rootActionV1Source,
		MetadataDigest: request.Config.Action.MetadataDigest, ProofHash: record.ProofHash, PublicKey: device.device, AppVersion: config.AppVersion,
		Response: "0x" + hex.EncodeToString(append([]byte{0}, ed25519.Sign(signers.keys[device.device], payload)...))}
	bootstrapRootTestWrite(t, input.ResponsePath, response)
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	var first ownerSigningReply
	for attempt := 0; attempt < 2; attempt++ {
		reply, err := ownerSigningTestSign(t, t.Context(), config, request, device.invoke)
		if err != nil || device.prepares != 0 || device.signs != 0 {
			t.Fatal("restart did not recover the original response without the device", err, device.prepares, device.signs)
		}
		if attempt == 0 {
			first = reply
		} else if reply != first {
			t.Fatal("recovered reply changed across restarts")
		}
	}
	if _, err := first.validate(request); err != nil || first.Signatory != request.Config.Action.Multisig.Signatory {
		t.Fatal("recovered multisig reply does not verify", err)
	}
	// A different request cannot reuse this owner-local custody.
	f, _, _ := ownerTrimMultisigTestRequest(t)
	other := ownerTrimMultisigTestStep(t, f, "as_multi", signers.accounts[0], treasuryTimepoint{Height: 101, Index: 1})
	otherRequest, err := newOwnerSigningRequest(other, f.key, f.metadata, f.ledgerMetadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ownerSigningTestSign(t, t.Context(), config, otherRequest, device.invoke); err == nil || !strings.Contains(err.Error(), "owner device marker differs") || device.signs != 0 {
		t.Fatal("owner-local custody was retargeted to another step's request", err)
	}
}

// The direct owner's adapter protocol keeps its exact original field set.
func TestOwnerTrimMultisigDirectAdapterInputUnchanged(t *testing.T) {
	_, request, _ := ownerSigningTestRequest(t)
	config := ownerSigningTestDeviceConfig(t)
	var keys []string
	adapter := func(_ context.Context, _ ownerSigningDeviceConfig, input ownerSigningAdapterInput) (ownerSigningAdapterResult, error) {
		raw, _ := json.Marshal(input)
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			return ownerSigningAdapterResult{}, err
		}
		for key := range fields {
			keys = append(keys, key)
		}
		return ownerSigningAdapterResult{}, errors.New("synthetic stop after recording the input")
	}
	if _, err := ownerSigningTestSign(t, t.Context(), config, request, adapter); err == nil {
		t.Fatal("synthetic adapter stop was ignored")
	}
	sort.Strings(keys)
	original := []string{"account", "backend_path", "backend_sha256", "call_scale", "expected_app_version", "included_in_extrinsic", "included_in_signed_data", "index",
		"metadata_digest", "metadata_scale", "mode", "owner_account_id", "proof_sha256", "request_hash", "response_path", "schema", "spec_name", "spec_version"}
	if !slices.Equal(keys, original) {
		t.Fatal("direct owner adapter input changed", keys)
	}
}

// Instrument the unmodified adapter with a fixture backend and device whose
// public key a scenario selects. The native loader is the only replacement.
func ownerTrimMultisigInstrumentedAdapter(t *testing.T, config *ownerSigningDeviceConfig, request ownerSigningRequest, deviceKey string, signature string) string {
	t.Helper()
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 is required for the owner Ledger adapter fixture", err)
	}
	helperSource, err := os.ReadFile("owner_ledger_adapter.py")
	if err != nil || bytes.Count(helperSource, []byte(`if __name__ == "__main__":`)) != 1 {
		t.Fatal("owner adapter main boundary changed", err)
	}
	logPath := filepath.Join(filepath.Dir(config.StatePath), "device-calls.log")
	action := request.Config.Action
	fixture, _ := json.Marshal(map[string]any{"log_path": logPath, "device": deviceKey, "metadata": request.LedgerMetadata, "digest": action.MetadataDigest,
		"payload": action.Payload, "signature": signature})
	instrumentation := fmt.Sprintf(`
fixture = json.loads(%q)
def fixture_log(value):
    with open(fixture["log_path"], "a", encoding="utf-8") as output:
        output.write(value + "\n")
class FixtureDevice:
    def __init__(self):
        fixture_log("open")
    def app_version(self):
        return (100, 0, 26)
    def address(self, account, index, prefix, confirm):
        assert (account, index, prefix, confirm) == (1, 0, 42, True)
        fixture_log("address")
        return bytes.fromhex(fixture["device"][2:]), "synthetic-signatory"
    def sign(self, payload, proof, account, index):
        assert "0x" + payload.hex() == fixture["payload"] and proof == b"synthetic-sdk-proof"
        fixture_log("sign")
        return bytes.fromhex(fixture["signature"][2:])
class FixtureBackend:
    LedgerDevice = FixtureDevice
    @staticmethod
    def metadata_digest(metadata, spec_version, spec_name, prefix, decimals, token):
        assert "0x" + metadata.hex() == fixture["metadata"]
        fixture_log("digest")
        return bytes.fromhex(fixture["digest"][2:])
    @staticmethod
    def generate_extrinsic_proof(call, extra, implicit, *chain):
        assert "0x" + (call + extra + implicit).hex() == fixture["payload"]
        fixture_log("proof")
        return b"synthetic-sdk-proof"
load_backend = lambda path, digest: FixtureBackend
`, string(fixture))
	instrumented := strings.Replace(string(helperSource), `if __name__ == "__main__":`, instrumentation+"\n"+`if __name__ == "__main__":`, 1)
	config.PythonPath = pythonPath
	config.HelperPath = filepath.Join(filepath.Dir(config.StatePath), "owner-adapter-fixture.py")
	if err := os.WriteFile(config.HelperPath, []byte(instrumented), 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(instrumented))
	config.HelperHash, config.AppVersion = "sha256:"+hex.EncodeToString(digest[:]), [3]uint16{100, 0, 26}
	return logPath
}

// The real Python bridge signs only when the device derives the named
// signatory. A signer outside the set, a set not deriving the owner and an
// unsorted set are refused offline, before the SDK or any device is opened.
func TestOwnerTrimMultisigPythonAdapterChecksSignatoryAndDerivation(t *testing.T) {
	_, signers, request := ownerTrimMultisigTestRequest(t)
	action := request.Config.Action
	payload, _ := hex.DecodeString(action.Payload[2:])
	named := action.Multisig.Signatory
	signature := "0x" + hex.EncodeToString(append([]byte{0}, ed25519.Sign(signers.keys[named], payload)...))
	for _, scenario := range []string{"named", "other-signatory"} {
		config := ownerSigningTestDeviceConfig(t)
		device := named
		if scenario == "other-signatory" {
			device = signers.accounts[0]
		}
		logPath := ownerTrimMultisigInstrumentedAdapter(t, &config, request, device, signature)
		reply, err := ownerSigningTestSign(t, t.Context(), config, request, nil)
		log, _ := os.ReadFile(logPath)
		if scenario == "named" {
			if err != nil || reply.Signatory != named || bytes.Count(log, []byte("sign\n")) != 1 {
				t.Fatal("real adapter refused the named signatory", err, string(log))
			}
		} else if err == nil || !strings.Contains(err.Error(), "device-derived AccountId32 is not the named multisig signatory") ||
			!bytes.Contains(log, []byte("address\n")) || bytes.Contains(log, []byte("sign\n")) {
			t.Fatal("real adapter signed with a device that is not the named signatory", err, string(log))
		}
	}
	outsider := "0x" + hex.EncodeToString(ownerTrimMultisigTestKey("outsider").Public().(ed25519.PublicKey))
	for scenario, reason := range map[string]string{"outside-set": "named signatory is not in the multisig signer set",
		"not-deriving-owner": "do not derive the subnet owner account", "unsorted": "unique and sorted", "wrong-threshold": "do not derive the subnet owner account"} {
		config := ownerSigningTestDeviceConfig(t)
		logPath := ownerTrimMultisigInstrumentedAdapter(t, &config, request, named, signature)
		call, _ := hex.DecodeString(action.Call[2:])
		extraLength := 2 + len(rootCompact(uint64(action.Nonce))) + 2
		input := ownerSigningAdapterInput{Schema: ownerSigningAdapterSchema, Mode: "prepare", RequestHash: request.ContentHash, BackendPath: config.BackendPath,
			BackendHash: config.BackendHash, MetadataHex: request.LedgerMetadata, MetadataDigest: action.MetadataDigest, SpecName: action.Runtime.RuntimeVersion.SpecName,
			SpecVersion: action.Runtime.RuntimeVersion.SpecVersion, Owner: named, Account: 1, Index: 0, Call: action.Call,
			IncludedExtrinsic: "0x" + hex.EncodeToString(payload[len(call):len(call)+extraLength]), IncludedSignedData: "0x" + hex.EncodeToString(payload[len(call)+extraLength:]),
			ResponsePath: config.StatePath + ".ledger-response", AppVersion: config.AppVersion,
			MultisigAccount: signers.owner, MultisigThreshold: 2, MultisigSignatories: slices.Clone(signers.accounts)}
		switch scenario {
		case "outside-set":
			input.Owner = outsider
		case "not-deriving-owner":
			input.MultisigSignatories[0] = outsider
			slices.Sort(input.MultisigSignatories)
			if !slices.Contains(input.MultisigSignatories, named) {
				t.Fatal("fixture removed the named signatory")
			}
		case "unsorted":
			slices.Reverse(input.MultisigSignatories)
		case "wrong-threshold":
			input.MultisigThreshold = 3
		}
		if _, err := runOwnerLedgerAdapter(t.Context(), config, input); err == nil || !strings.Contains(err.Error(), reason) {
			t.Fatal(scenario, "real adapter accepted a foreign multisig signer set", err)
		}
		if log, err := os.ReadFile(logPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal(scenario, "refusal reached the SDK or device", string(log), err)
		}
	}
}

// The adapter's offline derivation is the same pallet_multisig rule as Go's,
// including a signer set wide enough for the two-byte compact length.
func TestOwnerTrimMultisigPythonDerivationMatchesGo(t *testing.T) {
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 is required for the owner Ledger adapter fixture", err)
	}
	helperSource, err := os.ReadFile("owner_ledger_adapter.py")
	if err != nil {
		t.Fatal(err)
	}
	signers := newOwnerTrimMultisigTestSigners(t)
	sets := [][]string{signers.accounts}
	wide := []string{}
	for index := 0; index < 70; index++ {
		key := ownerTrimMultisigTestKey(fmt.Sprint("wide ", index))
		wide = append(wide, "0x"+hex.EncodeToString(key.Public().(ed25519.PublicKey)))
	}
	sort.Strings(wide)
	sets = append(sets, wide)
	for _, set := range sets {
		raw := make([][32]byte, len(set))
		for index, account := range set {
			decoded, _ := hex.DecodeString(account[2:])
			copy(raw[index][:], decoded)
		}
		for _, threshold := range []uint16{2, 3} {
			derived, err := crv4.DeriveNativeMultisigAccount(raw, threshold)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(set)
			script := string(helperSource) + fmt.Sprintf("\nprint(multisig_account([bytes.fromhex(v[2:]) for v in json.loads(%q)], %d).hex())\n", string(encoded), threshold)
			script = strings.Replace(script, `if __name__ == "__main__":`, `if False:`, 1)
			output, err := exec.CommandContext(t.Context(), pythonPath, "-I", "-c", script).CombinedOutput()
			if err != nil || strings.TrimSpace(string(output)) != hex.EncodeToString(derived[:]) {
				t.Fatal("adapter multisig derivation differs from Go", len(set), threshold, err, string(output))
			}
		}
	}
}

// The public owner command signs a multisig step only with both independent
// account pins, and verify accepts exactly that signatory's reply file.
func TestOwnerTrimMultisigOwnerCommandSignsAndVerifies(t *testing.T) {
	f, signers, request := ownerTrimMultisigTestRequest(t)
	config := ownerSigningTestDeviceConfig(t)
	requestRef := bootstrapRootTestWrite(t, filepath.Join(ownerSigningTestDirectory(t), "request.json"), request)
	signatory := request.Config.Action.Multisig.Signatory
	args := []string{"sign", "--request", requestRef.Path, "--accept-request-hash", request.ContentHash, "--trim-approval-key", f.key,
		"--owner-account-id", signers.owner, "--expected-genesis", request.Config.Action.Network.GenesisHash, "--owner-state", config.StatePath,
		"--ledger-python", config.PythonPath, "--ledger-helper", config.HelperPath, "--ledger-helper-sha256", config.HelperHash,
		"--ledger-backend", config.BackendPath, "--ledger-backend-sha256", config.BackendHash, "--ledger-app-version", "100.0.5"}
	adapter := &ownerTrimMultisigDeviceAdapter{request: request, signers: signers, device: signatory}
	ctx := ownerLocalDurableTestContext(t, filepath.Dir(config.StatePath))
	var stdout, stderr bytes.Buffer
	if code := runOwnerSigningCommandWithAdapter(ctx, args, &stdout, &stderr, adapter.invoke); code != 3 || adapter.prepares != 0 {
		t.Fatal("a multisig step was signed without its named signatory pin", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runOwnerSigningCommandWithAdapter(ctx, append(args, "--signatory-account-id", signers.accounts[0]), &stdout, &stderr, adapter.invoke); code != 3 || adapter.prepares != 0 {
		t.Fatal("a multisig step was signed for another named signatory", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runOwnerSigningCommandWithAdapter(ctx, append(args, "--signatory-account-id", signatory), &stdout, &stderr, adapter.invoke); code != 0 {
		t.Fatalf("owner multisig sign command: %d %s", code, stderr.String())
	}
	var reply ownerSigningReply
	if err := decodePlanJson(stdout.Bytes(), &reply); err != nil || reply.Signatory != signatory || adapter.signs != 1 {
		t.Fatal("owner multisig sign command reply differs", err)
	}
	replyRef := bootstrapRootTestWrite(t, filepath.Join(ownerSigningTestDirectory(t), "reply.json"), reply)
	trust := ownerSigningTrust{RequestHash: request.ContentHash, ApprovalKey: f.key, Owner: signers.owner, Genesis: request.Config.Action.Network.GenesisHash}
	verified, diagnostic, code := ownerSigningTestCommand(t, "verify", requestRef.Path, trust, "--signatory-account-id", signatory, "--reply", replyRef.Path, "--reply-sha256", replyRef.Sha256)
	var retained ownerSigningReply
	if code != 0 || decodePlanJson(verified, &retained) != nil || retained != reply {
		t.Fatalf("owner multisig verify: %d %s", code, diagnostic)
	}
}
