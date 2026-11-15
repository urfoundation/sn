// Device boundaries are synthetic and deterministic. Tests assert durable
// intent, original-response recovery and exclusion before any hardware effect.
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
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

// Adapter pins are synthetic; an injected test adapter never opens these files.
func ownerSigningTestDeviceConfig(t *testing.T) ownerSigningDeviceConfig {
	t.Helper()
	directory := ownerSigningTestDirectory(t)
	prepareMainnetSnapshotTest(t, filepath.Join(directory, "owner-signing.json"), "mainnet-owner-signing", ownerSigningReplyLimit)
	return ownerSigningDeviceConfig{StatePath: filepath.Join(directory, "owner-signing.json"), PythonPath: "/synthetic-tools/python3",
		HelperPath: "/synthetic-tools/owner_ledger_adapter.py", HelperHash: rootObjectHash("synthetic helper"),
		BackendPath: "/synthetic-tools/bittensor_core.so", BackendHash: rootObjectHash("synthetic SDK artifact"), AppVersion: [3]uint16{100, 0, 5}}
}

// Counters and explicit barriers allow concurrent ownership tests without
// sleeps or scheduling guesses. The signing reply uses a public test-only key.
type ownerSigningDeviceTestAdapter struct {
	request      ownerSigningRequest
	prepares     atomic.Int32
	signs        atomic.Int32
	loseReply    bool
	omitResponse bool
	prepareError bool
	entered      chan struct{}
	release      chan struct{}
}

// The fixture exercises the same public parts and response file as the SDK
// adapter; it does not implement or claim hardware or RFC78 qualification.
func (self *ownerSigningDeviceTestAdapter) invoke(ctx context.Context, config ownerSigningDeviceConfig, input ownerSigningAdapterInput) (ownerSigningAdapterResult, error) {
	action := self.request.Config.Action
	result := ownerSigningAdapterResult{Schema: ownerSigningAdapterSchema, Mode: input.Mode, RequestHash: self.request.ContentHash,
		SourceCommit: rootActionV1Source, MetadataDigest: action.MetadataDigest, ProofHash: rootObjectHash("synthetic proof")}
	if input.MetadataHex != self.request.LedgerMetadata || input.Owner != action.Coldkey || input.Account != 7 || input.Index != 3 || input.AppVersion != config.AppVersion ||
		input.Call+input.IncludedExtrinsic[2:]+input.IncludedSignedData[2:] != action.Payload || input.BackendHash != config.BackendHash || input.BackendPath != config.BackendPath {
		return result, errors.New("adapter changed request or payload seams")
	}
	if input.Mode == "prepare" {
		self.prepares.Add(1)
		if self.prepareError {
			return result, errors.New("synthetic metadata digest mismatch before device")
		}
		return result, nil
	}
	self.signs.Add(1)
	raw, err := os.ReadFile(config.StatePath)
	var record ownerSigningDeviceRecord
	if err != nil || decodePlanJson(raw, &record) != nil || record.Phase != "signing" || record.RequestHash != input.RequestHash || record.ProofHash != input.ProofHash {
		return result, errors.New("device called without original durable signing intent")
	}
	if self.entered != nil {
		close(self.entered)
		select {
		case <-self.release:
		case <-ctx.Done():
			return result, ctx.Err()
		}
	}
	if self.omitResponse {
		return result, errors.New("synthetic device response lost before durable publication")
	}
	result.PublicKey, result.AppVersion = action.Coldkey, config.AppVersion
	result.Response = "0x" + hex.EncodeToString(append([]byte{0}, ed25519.Sign(ownerSigningTestKey(), ownerSigningBytes(action))...))
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

// Lost stdout after a durable adapter response recovers the exact original
// public signature once, including after the device/backend disappears.
func TestOwnerSigningDeviceRecoversDurableResponseWithoutResigning(t *testing.T) {
	_, request, _ := ownerSigningTestRequest(t)
	config := ownerSigningTestDeviceConfig(t)
	adapter := &ownerSigningDeviceTestAdapter{request: request, loseReply: true}
	reply, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke)
	if err != nil || adapter.signs.Load() != 1 || adapter.prepares.Load() != 1 {
		t.Fatal("lost adapter stdout did not recover durable original bytes", err)
	}
	retained, err := os.ReadFile(config.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		got, err := ownerSigningTestSign(t, t.Context(), config, request, nil)
		if err != nil || got != reply {
			t.Fatal("completed owner journal needed device/backend or replaced reply", err)
		}
	}
	after, _ := os.ReadFile(config.StatePath)
	if !bytes.Equal(retained, after) {
		t.Fatal("completed owner recovery rewrote original custody")
	}
	changed := config
	changed.BackendHash = rootObjectHash("replacement native backend")
	if _, err := ownerSigningTestSign(t, t.Context(), changed, request, adapter.invoke); err == nil || adapter.signs.Load() != 1 {
		t.Fatal("changed tool provenance adopted original custody")
	}
}

// No response is not evidence of non-issuance. Restart may retain a later
// recovered original artifact but must never call the device a second time.
func TestOwnerSigningDeviceUnknownIssuanceRequiresOriginalResponse(t *testing.T) {
	_, request, _ := ownerSigningTestRequest(t)
	config := ownerSigningTestDeviceConfig(t)
	adapter := &ownerSigningDeviceTestAdapter{request: request, omitResponse: true}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke); err == nil {
			t.Fatal("unknown original device issuance claimed a reply")
		}
	}
	if adapter.signs.Load() != 1 || adapter.prepares.Load() != 1 {
		t.Fatal("unknown issuance retried the original hardware operation")
	}
	response := ownerSigningAdapterResult{Schema: ownerSigningAdapterSchema, Mode: "sign", RequestHash: request.ContentHash,
		SourceCommit: rootActionV1Source, MetadataDigest: request.Config.Action.MetadataDigest, ProofHash: rootObjectHash("synthetic proof"),
		PublicKey: request.Config.Action.Coldkey, AppVersion: config.AppVersion,
		Response: "0x" + hex.EncodeToString(append([]byte{0}, ed25519.Sign(ownerSigningTestKey(), ownerSigningBytes(request.Config.Action))...))}
	bootstrapRootTestWrite(t, config.StatePath+".ledger-response", response)
	reply, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke)
	if err != nil || reply.Signature == "" || adapter.signs.Load() != 1 || adapter.prepares.Load() != 1 {
		t.Fatal("original response recovery did not preserve the one device attempt", err)
	}
}

// A completed marker makes deleted/partial journals unresolved forever; a new
// request or absent reply cannot replenish hardware signing authority.
func TestOwnerSigningDeviceMissingJournalAndForeignReplyCannotReissue(t *testing.T) {
	_, request, _ := ownerSigningTestRequest(t)
	config := ownerSigningTestDeviceConfig(t)
	adapter := &ownerSigningDeviceTestAdapter{request: request, omitResponse: true}
	if _, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke); err == nil {
		t.Fatal("fixture did not reach ambiguous issuance")
	}
	bootstrapRootTestWrite(t, config.StatePath+".ledger-response", ownerSigningAdapterResult{Schema: ownerSigningAdapterSchema, Mode: "sign", RequestHash: rootObjectHash("foreign request")})
	if _, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke); err == nil || adapter.signs.Load() != 1 {
		t.Fatal("foreign response resolved original issuance or retried device")
	}
	if err := os.Remove(config.StatePath); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke); err == nil || adapter.signs.Load() != 1 {
		t.Fatal("missing established owner journal created a fresh signing allowance")
	}
}

// The second command encounters the held marker while the first is provably
// inside its single device operation. No timing assumptions establish exclusion.
func TestOwnerSigningDeviceExclusiveCustodyAcrossConcurrentCommands(t *testing.T) {
	_, request, _ := ownerSigningTestRequest(t)
	config := ownerSigningTestDeviceConfig(t)
	adapter := &ownerSigningDeviceTestAdapter{request: request, entered: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	completed := make(chan struct{})
	var signErr error
	go func() {
		_, signErr = ownerSigningTestSign(t, ctx, config, request, adapter.invoke)
		close(completed)
	}()
	t.Cleanup(func() {
		cancel()
		<-completed
	})
	select {
	case <-adapter.entered:
	case <-completed:
		t.Fatal("first owner command failed before the device barrier", signErr)
	case <-ctx.Done():
		t.Fatal("first owner command canceled before the device barrier", ctx.Err())
	}
	if _, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke); err == nil || !strings.Contains(err.Error(), "owner device request already has a local owner") || adapter.signs.Load() != 1 {
		t.Error("second local command did not observe the active exclusive owner", err)
	}
	close(adapter.release)
	<-completed
	if signErr != nil {
		t.Fatal(signErr)
	}
	if adapter.signs.Load() != 1 || adapter.prepares.Load() != 1 {
		t.Fatal("exclusive owner allowed duplicate adapter operations")
	}
}

// Cancellation occurs only after the durable intent and device-entry barrier.
// Completion is joined before fixture cleanup; restart cannot issue again.
func TestOwnerSigningDeviceCancellationPreservesUnknownIssuance(t *testing.T) {
	_, request, _ := ownerSigningTestRequest(t)
	config := ownerSigningTestDeviceConfig(t)
	adapter := &ownerSigningDeviceTestAdapter{request: request, entered: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	completed := make(chan struct{})
	var signErr error
	go func() {
		_, signErr = ownerSigningTestSign(t, ctx, config, request, adapter.invoke)
		close(completed)
	}()
	t.Cleanup(func() {
		cancel()
		<-completed
	})
	select {
	case <-adapter.entered:
	case <-completed:
		t.Fatal("first owner command failed before the device barrier", signErr)
	case <-ctx.Done():
		t.Fatal("first owner command canceled before the device barrier", ctx.Err())
	}
	cancel()
	<-completed
	if !errors.Is(signErr, context.Canceled) || adapter.signs.Load() != 1 {
		t.Fatal("device cancellation did not preserve the original issued attempt", signErr)
	}
	if _, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke); err == nil || adapter.signs.Load() != 1 || adapter.prepares.Load() != 1 {
		t.Fatal("canceled device operation was reissued", err)
	}
	if _, err := os.Stat(config.StatePath + ".ledger-response"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled fixture synthesized a device response", err)
	}
}

// Umask is process-global, so each ambient mask runs in an isolated copy of the
// test binary. Real signing custody must work under permissive owner defaults.
func TestOwnerSigningDeviceFixturePermissionsIndependentOfUmask(t *testing.T) {
	const variable = "UR_TEST_OWNER_SIGNING_FIXTURE_UMASK"
	if encoded := os.Getenv(variable); encoded != "" {
		mask, err := strconv.ParseUint(encoded, 8, 9)
		if err != nil {
			t.Fatal(err)
		}
		original := syscall.Umask(int(mask))
		defer syscall.Umask(original)
		_, request, _ := ownerSigningTestRequest(t)
		config := ownerSigningTestDeviceConfig(t)
		info, err := os.Stat(filepath.Dir(config.StatePath))
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("owner fixture inherited ambient directory permissions", encoded, info, err)
		}
		adapter := &ownerSigningDeviceTestAdapter{request: request}
		reply, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke)
		if err != nil || adapter.signs.Load() != 1 || adapter.prepares.Load() != 1 {
			t.Fatal("private owner fixture could not sign under ambient umask", encoded, err)
		}
		if _, err := reply.validate(request); err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{"", ".lock", ".ledger-response"} {
			info, err := os.Stat(config.StatePath + suffix)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("owner custody file permissions changed under ambient umask", encoded, suffix, info, err)
			}
		}
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mask := range []string{"0000", "0002", "0022", "0077"} {
		command := exec.CommandContext(t.Context(), executable, "-test.run=^TestOwnerSigningDeviceFixturePermissionsIndependentOfUmask$", "-test.count=1")
		command.Env = append(os.Environ(), variable+"="+mask)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("owner fixture with umask %s: %v\n%s", mask, err, output)
		}
	}
}

// Metadata/proof refusal remains before issuance; the same reserved request
// can retry offline preparation without treating a device failure that way.
func TestOwnerSigningDeviceMetadataFailurePrecedesIssuance(t *testing.T) {
	_, request, _ := ownerSigningTestRequest(t)
	config := ownerSigningTestDeviceConfig(t)
	adapter := &ownerSigningDeviceTestAdapter{request: request, prepareError: true}
	if _, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke); err == nil || adapter.signs.Load() != 0 {
		t.Fatal("bad metadata reached the signing device")
	}
	adapter.prepareError = false
	if _, err := ownerSigningTestSign(t, t.Context(), config, request, adapter.invoke); err != nil || adapter.signs.Load() != 1 || adapter.prepares.Load() != 2 {
		t.Fatal("offline preparation failure consumed or renewed device issuance", err)
	}
}

// The actual sign parser needs only owner-local files; it returns a reply that
// the ordinary public handoff verifier accepts without exposing key material.
func TestOwnerSigningDevicePublicCommandUsesPortableRequest(t *testing.T) {
	f, request, trust := ownerSigningTestRequest(t)
	if err := os.RemoveAll(filepath.Dir(f.config.Action.StatePath)); err != nil {
		t.Fatal(err)
	}
	config := ownerSigningTestDeviceConfig(t)
	requestPath := filepath.Join(ownerSigningTestDirectory(t), "request.json")
	bootstrapRootTestWrite(t, requestPath, request)
	args := []string{"sign", "--request", requestPath, "--accept-request-hash", trust.RequestHash, "--trim-approval-key", trust.ApprovalKey,
		"--owner-account-id", trust.Owner, "--expected-genesis", trust.Genesis, "--owner-state", config.StatePath,
		"--ledger-python", config.PythonPath, "--ledger-helper", config.HelperPath, "--ledger-helper-sha256", config.HelperHash,
		"--ledger-backend", config.BackendPath, "--ledger-backend-sha256", config.BackendHash, "--ledger-app-version", "100.0.5"}
	adapter := &ownerSigningDeviceTestAdapter{request: request}
	var stdout, stderr bytes.Buffer
	ctx := ownerLocalDurableTestContext(t, filepath.Dir(config.StatePath))
	code := runOwnerSigningCommandWithAdapter(ctx, args, &stdout, &stderr, adapter.invoke)
	var reply ownerSigningReply
	if code != 0 || decodePlanJson(stdout.Bytes(), &reply) != nil {
		t.Fatalf("owner sign command: %d %s", code, stderr.String())
	}
	if _, err := reply.validate(request); err != nil || adapter.signs.Load() != 1 {
		t.Fatal("owner-local sign command changed exact public reply", err)
	}
	if _, err := os.Stat(filepath.Dir(f.config.Action.StatePath)); !os.IsNotExist(err) {
		t.Fatal("owner sign command accessed host custody", err)
	}
}

// The real Python bridge runs with only its native-module loading boundary
// replaced. The synthetic module implements the pinned SDK API, not HID or
// RFC78; the production preparation, key check, proof and durable response path
// all execute unchanged. Python is needed to exercise the actual SDK glue.
func TestOwnerSigningPythonBridgeSdkBoundaryAndRefusals(t *testing.T) {
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python 3 is required for the owner Ledger adapter fixture", err)
	}
	helperSource, err := os.ReadFile("owner_ledger_adapter.py")
	if err != nil || bytes.Count(helperSource, []byte(`if __name__ == "__main__":`)) != 1 {
		t.Fatal("owner adapter main boundary changed", err)
	}
	_, request, _ := ownerSigningTestRequest(t)
	for _, scenario := range []string{"valid", "wrong-key", "wrong-version", "wrong-digest", "changed-proof", "invalid-signature"} {
		config := ownerSigningTestDeviceConfig(t)
		config.PythonPath = pythonPath
		config.HelperPath = filepath.Join(filepath.Dir(config.StatePath), "owner-adapter-fixture.py")
		logPath := filepath.Join(filepath.Dir(config.StatePath), "device-calls.log")
		fixture, _ := json.Marshal(map[string]any{"scenario": scenario, "log_path": logPath, "owner": request.Config.Action.Coldkey,
			"metadata": request.LedgerMetadata, "digest": request.Config.Action.MetadataDigest, "payload": request.Config.Action.Payload,
			"signature": "0x" + hex.EncodeToString(append([]byte{0}, ed25519.Sign(ownerSigningTestKey(), ownerSigningBytes(request.Config.Action))...))})
		instrumentation := fmt.Sprintf(`
fixture = json.loads(%q)
def fixture_log(value):
    with open(fixture["log_path"], "a", encoding="utf-8") as output:
        output.write(value + "\n")
class FixtureDevice:
    def __init__(self):
        fixture_log("open")
    def app_version(self):
        fixture_log("version")
        return (100, 0, 6) if fixture["scenario"] == "wrong-version" else (100, 0, 5)
    def address(self, account, index, prefix, confirm):
        assert (account, index, prefix, confirm) == (7, 3, 42, True)
        fixture_log("address")
        return (bytes(32) if fixture["scenario"] == "wrong-key" else bytes.fromhex(fixture["owner"][2:])), "synthetic-owner"
    def sign(self, payload, proof, account, index):
        assert "0x" + payload.hex() == fixture["payload"]
        assert proof == b"synthetic-sdk-proof" and (account, index) == (7, 3)
        fixture_log("sign")
        return bytes(65) if fixture["scenario"] == "invalid-signature" else bytes.fromhex(fixture["signature"][2:])
class FixtureBackend:
    LedgerDevice = FixtureDevice
    @staticmethod
    def metadata_digest(metadata, spec_version, spec_name, prefix, decimals, token):
        assert "0x" + metadata.hex() == fixture["metadata"]
        assert (prefix, decimals, token) == (42, 9, "TAO")
        fixture_log("digest")
        return bytes(32) if fixture["scenario"] == "wrong-digest" else bytes.fromhex(fixture["digest"][2:])
    @staticmethod
    def generate_extrinsic_proof(call, extra, implicit, *chain):
        assert "0x" + (call + extra + implicit).hex() == fixture["payload"]
        fixture_log("proof")
        if fixture["scenario"] == "changed-proof" and os.path.exists(%q):
            with open(%q, encoding="utf-8") as source:
                if json.load(source)["phase"] == "signing":
                    return b"different-proof"
        return b"synthetic-sdk-proof"
load_backend = lambda path, digest: FixtureBackend
`, string(fixture), config.StatePath, config.StatePath)
		instrumented := strings.Replace(string(helperSource), `if __name__ == "__main__":`, instrumentation+"\n"+`if __name__ == "__main__":`, 1)
		if err := os.WriteFile(config.HelperPath, []byte(instrumented), 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(instrumented))
		config.HelperHash = "sha256:" + hex.EncodeToString(digest[:])
		reply, err := ownerSigningTestSign(t, t.Context(), config, request, nil)
		log, logErr := os.ReadFile(logPath)
		if logErr != nil {
			t.Fatal(scenario, "adapter did not run", logErr, err)
		}
		if scenario == "valid" {
			if err != nil || bytes.Count(log, []byte("sign\n")) != 1 {
				t.Fatal("real Python bridge did not retain one verified fixture response", err, string(log))
			}
			if _, err := reply.validate(request); err != nil {
				t.Fatal(err)
			}
		} else {
			if err == nil {
				t.Fatal(scenario, "bridge admitted incompatible device or metadata")
			}
			if scenario != "invalid-signature" && bytes.Contains(log, []byte("sign\n")) {
				t.Fatal(scenario, "mismatch reached device signing", string(log))
			}
		}
		if scenario == "wrong-digest" || scenario == "changed-proof" {
			if bytes.Contains(log, []byte("open\n")) {
				t.Fatal(scenario, "metadata failure opened the device")
			}
		}
		if scenario != "wrong-digest" {
			_, _ = ownerSigningTestSign(t, t.Context(), config, request, nil)
			after, _ := os.ReadFile(logPath)
			if !bytes.Equal(log, after) {
				t.Fatal(scenario, "restart invoked the device or adapter again")
			}
		}
	}
}
