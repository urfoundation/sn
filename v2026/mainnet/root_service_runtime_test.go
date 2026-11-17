// Runtime tests use synthetic approvals and native signatures, private local
// journals and an owned local HTTP fixture. No production key or route exists.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

type rootServiceRuntimeFixture struct {
	root       *bootstrapRootFixture
	submission *rootSubmissionFixture
	config     rootServiceRuntimeConfig
	input      planFileReference
}

// Independent service approval and original action/route approval use distinct
// keys and domains. The command receives the same inputs as a real embedding.
func newRootServiceRuntimeFixture(t *testing.T) *rootServiceRuntimeFixture {
	t.Helper()
	submission := newRootSubmissionFixture(t)
	root := bootstrapRootFixtureFromOffline(t, submission.offline)
	submission.offline, submission.config.Service = root.offline, root.plan.Service
	submission.config.Approval.ServiceConfigHash = rootObjectHash(root.plan.Service)
	submission.config.Approval.PacketHash = root.offline.packet.ContentHash
	submission.approve(t)
	submission.intent.Packet = root.offline.packet
	submission.intent.ConfigHash = rootObjectHash(root.plan.Service)
	directory := filepath.Dir(root.configPath)
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x5c}, ed25519.SeedSize))
	approval := bootstrapChainRootApproval{Schema: bootstrapChainRootApprovalSchema, DeploymentId: root.plan.DeploymentId,
		RootPlanHash: root.plan.ContentHash, ServiceConfigHash: rootObjectHash(root.plan.Service)}
	message, err := approval.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	approval.Signature = hex.EncodeToString(ed25519.Sign(key, message))
	scope := root.plan.Service.Packet.Action.Scope
	f := &rootServiceRuntimeFixture{root: root, submission: submission, config: rootServiceRuntimeConfig{
		Schema: rootServiceRuntimeConfigSchema, Root: planFileReference{Path: root.configPath, Sha256: root.plan.ConfigSha256},
		Role: bootstrapChainRootValidator{Role: scope.Role, Netuid: new(uint16(0)), Implementation: "sn/mainnet/root-service",
			Hotkey: scope.Hotkey, Coldkey: scope.Coldkey, Seat: new(scope.Seat), Strategy: scope.Strategy,
			ActionApprovalPublicKey: root.plan.Service.CustodyTrust.ApprovalPublicKey, ApprovalPublicKey: "0x" + hex.EncodeToString(key.Public().(ed25519.PublicKey)),
			Approval: bootstrapRootTestWrite(t, filepath.Join(directory, "service-approval.json"), approval)},
		Submission: bootstrapRootTestWrite(t, filepath.Join(directory, "submission-config.json"), submission.config),
	}}
	f.input = bootstrapRootTestWrite(t, filepath.Join(directory, "runtime.json"), f.config)
	return f
}

// Only the last scalar runtime result is returned; step diagnostics have their
// own schema. Buffers are inspected after all bounded exporters have joined.
func (self *rootServiceRuntimeFixture) command(t *testing.T, ctx context.Context, operation string, extra ...string) (int, rootServiceRuntimeResult, string) {
	t.Helper()
	ctx = durablepath.WithHost(durablevolume.WithReference(ctx, self.root.storage.Reference), self.root.storage.Host)
	args := []string{"root-service", operation, "--config", self.input.Path}
	if operation != "plan" {
		args = append(args, "--accept-runtime-sha256", self.input.Sha256)
	}
	var stdout, stderr bytes.Buffer
	code := runMain(ctx, append(args, extra...), &stdout, &stderr)
	var result rootServiceRuntimeResult
	decoder := json.NewDecoder(&stdout)
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if err != io.EOF {
				t.Fatal("malformed runtime output", err)
			}
			break
		}
		var schema struct {
			Schema string `json:"schema"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		if schema.Schema == "urnetwork-mainnet-root-service-runtime-result-v1" {
			if err := decodePlanJson(raw, &result); err != nil {
				t.Fatal(err)
			}
		}
	}
	return code, result, stderr.String()
}

func (self *rootServiceRuntimeFixture) prepare(t *testing.T) {
	t.Helper()
	self.root.result(t, "apply")
	if code, result, detail := self.command(t, t.Context(), "prepare"); code != 0 || result.Observations != 0 || result.Broadcasts != 0 || result.SignatureStatus != "unresolved" {
		t.Fatalf("prepare differs: %d %+v %s", code, result, detail)
	}
}

// Local complete weight storage is independent of custody and submission. A
// census request after issued-signature recovery is separately observable.
func (self *rootServiceRuntimeFixture) weights(t *testing.T) {
	t.Helper()
	fixture := self.submission.receipt
	set := func(name string, value []byte, args ...[]byte) {
		key, err := types.CreateStorageKey(fixture.metadata, "SubtensorModule", name, args...)
		if err != nil {
			t.Fatal(err)
		}
		fixture.storageKVs[key.Hex()] = "0x" + hex.EncodeToString(value)
	}
	for netuid := uint16(0); netuid < 8; netuid++ {
		set("NetworksAdded", []byte{1}, binary.LittleEndian.AppendUint16(nil, netuid))
	}
	set("RootWeightSettingEnabled", []byte{1})
	set("RootWeightsCap", binary.LittleEndian.AppendUint16(nil, 4096), []byte{0, 0})
	set("WeightsSetRateLimit", binary.LittleEndian.AppendUint64(nil, 1), []byte{0, 0})
	fixture.fault = func(method string, params []json.RawMessage, _ int) (any, bool) {
		if method != "state_getKeysPaged" {
			return nil, false
		}
		var prefix, start string
		if len(params) != 4 {
			t.Error("weight census omitted exact-block arguments")
			return []string{}, true
		}
		json.Unmarshal(params[0], &prefix)
		json.Unmarshal(params[2], &start)
		keys := []string{}
		for key := range fixture.storageKVs {
			if strings.HasPrefix(key, prefix) && key > start {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		return keys, true
	}
}

func TestRootServiceRuntimePublicActivationRemainsClosed(t *testing.T) {
	f := newRootServiceRuntimeFixture(t)
	prepared, err := os.ReadDir(f.root.config.RunDirectory)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"plan", "activate"} {
		code, result, detail := f.command(t, t.Context(), operation)
		wanted := 0
		if operation == "activate" {
			wanted = 3
		}
		if code != wanted || result.ActivationReady || result.NativeSigning || result.NetworkEffects || result.CurrentAuthorityVerified || len(result.ActivationBlockers) != 5 {
			t.Fatalf("public %s became mutation authority: %d %+v %s", operation, code, result, detail)
		}
	}
	entries, err := os.ReadDir(f.root.config.RunDirectory)
	if err != nil || len(entries) != len(prepared) || len(f.submission.sent()) != 0 || len(f.submission.receipt.counts) != 0 {
		t.Fatal("planning/activation touched ownership or the chain", err)
	}
	for i := range entries {
		if entries[i].Name() != prepared[i].Name() {
			t.Fatal("planning replaced a prepared custody member")
		}
	}
	f.weights(t)
	f.prepare(t)
	if code, _, detail := f.command(t, t.Context(), "run"); code != 0 {
		t.Fatalf("read-only observation failed: %d %s", code, detail)
	}
	if code, result, _ := f.command(t, t.Context(), "activate"); code != 3 || result.ActivationReady || len(f.submission.sent()) != 0 {
		t.Fatal("read-only success activated mutation")
	}
}

func TestRootServiceRuntimeObservesWithoutSpendingMutationAllowance(t *testing.T) {
	f := newRootServiceRuntimeFixture(t)
	f.weights(t)
	f.prepare(t)
	if code, result, detail := f.command(t, t.Context(), "run"); code != 0 || result.ServicePhase != "active" || result.ActionPhase != "reserved" || result.Observations != 1 || result.Broadcasts != 0 || result.SignatureStatus != "unresolved" {
		t.Fatalf("read-only decision changed mutation allowance: %d %+v %s", code, result, detail)
	}
	if code, result, _ := f.command(t, t.Context(), "run"); code != 3 || result.ActionPhase != "reserved" || result.Observations != 1 || result.Broadcasts != 0 || len(f.submission.sent()) != 0 {
		t.Fatalf("missing capabilities admitted mutation: %d %+v", code, result)
	}
	for _, operation := range []string{"status", "prepare"} {
		if code, result, _ := f.command(t, t.Context(), operation); code != 0 || result.Observations != 1 || result.Broadcasts != 0 || result.SignatureStatus != "unresolved" {
			t.Fatalf("%s renewed an observation or signature allowance: %d %+v", operation, code, result)
		}
	}
}

func TestRootServiceRuntimeRecoversIssuedBeforeUnavailableObserver(t *testing.T) {
	f := newRootServiceRuntimeFixture(t)
	f.prepare(t)
	receipt := f.root.offline.receipt(t)
	reference := bootstrapRootTestWrite(t, filepath.Join(filepath.Dir(f.input.Path), "signature.json"), receipt)
	f.root.result(t, "resume", "--signature-file", reference.Path, "--signature-sha256", reference.Sha256)
	// No NetworksAdded census exists, so a weight observation cannot succeed.
	if code, result, detail := f.command(t, t.Context(), "run"); code != 3 || result.ActionPhase != "signed" || !result.RecoveredSignature || result.Observations != 0 || result.Broadcasts != 0 || result.SignatureStatus != "issued" {
		t.Fatalf("issued signature waited for a decision or current authority: %d %+v %s", code, result, detail)
	}
	if f.submission.receipt.counts["state_getKeysPaged"] != 0 || len(f.submission.sent()) != 0 {
		t.Fatal("issued liability reached the observer or submitter")
	}
	signature, _ := hex.DecodeString(receipt.Signature)
	raw, err := f.root.plan.Service.Packet.Action.signed(signature)
	if err != nil {
		t.Fatal(err)
	}
	f.submission.finalize(t, raw)
	if code, result, detail := f.command(t, t.Context(), "run"); code != 0 || result.ServicePhase != "complete" || result.ActionPhase != "finalized" || result.ExtrinsicHash != rootExtrinsicHash(raw) || result.Broadcasts != 0 || result.Observations != 0 || result.ActivationReady {
		t.Fatalf("old receipt lost after authority removal/restart: %d %+v %s", code, result, detail)
	}
}

func TestRootServiceRuntimeSubmissionOnlyRecoveryPreservesUncertainAttempt(t *testing.T) {
	f := newRootServiceRuntimeFixture(t)
	f.prepare(t)
	f.submission.respond = func(writer http.ResponseWriter, _ *http.Request) {
		connection, _, err := writer.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	}
	owner, store := f.submission.open(t, false)
	if err := owner.submitRoot(t.Context(), f.submission.intent); !errors.Is(err, errRootSubmissionUncertain) {
		t.Fatal("fixture did not create an uncertain original send", err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	if code, result, detail := f.command(t, t.Context(), "run"); code != 3 || result.ActionPhase != "pending" || !result.RecoveredSignature || result.Broadcasts != 1 || result.SubmissionAttempts != 1 || result.Observations != 0 || result.ExtrinsicHash != f.submission.intent.ExtrinsicHash {
		t.Fatalf("submission liability/floor was lost: %d %+v %s", code, result, detail)
	}
	if code, result, _ := f.command(t, t.Context(), "run"); code != 3 || result.Broadcasts != 1 || result.Observations != 0 || len(f.submission.sent()) != 1 {
		t.Fatal("recovery resent original bytes or replenished attempts", code, result)
	}
	custody, custodyStore := f.root.offline.open(t, false)
	signature, err := custody.recoverSignature(t.Context(), f.root.plan.Service.Packet.Action.RequestHash)
	if err != nil {
		t.Fatal("submission-only signature was not recovered to original custody", err)
	}
	raw, err := f.root.plan.Service.Packet.Action.signed(signature)
	if err != nil || rootExtrinsicHash(raw) != f.submission.intent.ExtrinsicHash {
		t.Fatal("recovery substituted native bytes", err)
	}
	custodyStore.close()
}

func TestRootServiceRuntimeOriginalInputsCannotBeRebound(t *testing.T) {
	for _, change := range []string{"approval-missing", "submission-missing", "service-missing", "wrong-service-approver", "wrong-action-approver", "missing-netuid", "alias-state", "changed-runtime-bytes"} {
		f := newRootServiceRuntimeFixture(t)
		f.prepare(t)
		switch change {
		case "approval-missing":
			os.Remove(f.config.Role.Approval.Path)
		case "submission-missing":
			os.Remove(f.config.Submission.Path)
		case "service-missing":
			os.Remove(f.root.config.RootService.Path)
		case "wrong-service-approver":
			f.config.Role.ApprovalPublicKey = "0x" + strings.Repeat("1c", 32)
		case "wrong-action-approver":
			f.config.Role.ActionApprovalPublicKey = "0x" + strings.Repeat("2c", 32)
		case "missing-netuid":
			f.config.Role.Netuid = nil
		case "alias-state":
			f.config.Submission.Path = f.root.plan.Service.Packet.Action.Scope.StatePath
		case "changed-runtime-bytes":
			f.config.Schema = "synthetic-unapproved-schema"
		}
		f.input = bootstrapRootTestWrite(t, f.input.Path, f.config)
		if code, _, _ := f.command(t, t.Context(), "run"); code == 0 || len(f.submission.receipt.counts) != 0 || len(f.submission.sent()) != 0 {
			t.Fatalf("%s admitted a substituted input or reached the route", change)
		}
	}
}

func TestRootServiceRuntimePreparationRecoversDurablePrefixes(t *testing.T) {
	for _, childExists := range []bool{false, true} {
		f := newRootServiceRuntimeFixture(t)
		f.root.result(t, "apply")
		// The first synced prefix claims the exact route before child creation.
		store, err := openRootServiceStore(f.root.plan.Service, false, f.root.storage.Context)
		if err != nil {
			t.Fatal(err)
		}
		record, err := store.load()
		if err != nil {
			t.Fatal(err)
		}
		record.SubmissionConfigHash = rootObjectHash(f.submission.config)
		record.ContentHash = ""
		record.ContentHash = rootObjectHash(record)
		if err := store.save(record); err != nil {
			t.Fatal(err)
		}
		store.close()
		if childExists {
			child, err := openRootSubmissionStore(f.submission.config, true, f.root.storage.Context)
			if err != nil {
				t.Fatal(err)
			}
			child.close()
		}
		if code, _, _ := f.command(t, t.Context(), "run"); code != 3 {
			t.Fatal("incomplete preparation was run", code)
		}
		if code, result, detail := f.command(t, t.Context(), "prepare"); code != 0 || result.Observations != 0 || result.Broadcasts != 0 {
			t.Fatalf("prepare could not resume prefix child=%v: %d %+v %s", childExists, code, result, detail)
		}
		// The completed parent marker forbids recreation even if both files vanish.
		for _, path := range []string{f.submission.config.Approval.StatePath, f.submission.config.Approval.StatePath + ".lock"} {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		if code, _, _ := f.command(t, t.Context(), "prepare"); code != 3 {
			t.Fatal("completed submission disappearance created another allowance", code)
		}
		if _, err := os.Lstat(f.submission.config.Approval.StatePath + ".lock"); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("refused resume recreated the submission marker", err)
		}
	}
}

func TestRootServiceRuntimeMissingOriginalJournalsCannotReplenish(t *testing.T) {
	for _, missing := range []string{"service", "custody", "submission"} {
		f := newRootServiceRuntimeFixture(t)
		f.prepare(t)
		path := f.root.plan.Service.Packet.Action.Scope.StatePath
		if missing == "custody" {
			path = f.root.plan.Service.CustodyTrust.StatePath
		} else if missing == "submission" {
			path = f.submission.config.Approval.StatePath
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		for _, operation := range []string{"prepare", "status", "run"} {
			if code, _, _ := f.command(t, t.Context(), operation); code != 3 || len(f.submission.receipt.counts) != 0 {
				t.Fatalf("missing %s was recreated by %s", missing, operation)
			}
		}
	}
}

func TestRootServiceRuntimeConflictingSignaturesStopBeforeChainReads(t *testing.T) {
	f := newRootServiceRuntimeFixture(t)
	f.prepare(t)
	first, second := f.root.offline.receipt(t), f.root.offline.receipt(t)
	if first.Signature == second.Signature {
		t.Fatal("fixture did not produce distinct valid native signatures")
	}
	custody, custodyStore := f.root.offline.open(t, false)
	if err := custody.importSignature(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	custodyStore.close()
	store, err := openRootServiceStore(f.root.plan.Service, false, f.root.storage.Context)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := newRootServiceOwner(f.root.plan.Service, store, rootServicePorts{})
	if err != nil {
		t.Fatal(err)
	}
	signature, _ := hex.DecodeString(second.Signature)
	if err := owner.retainIssuedSignature(t.Context(), signature, 0); err != nil {
		t.Fatal(err)
	}
	store.close()
	if code, _, _ := f.command(t, t.Context(), "run"); code != 3 || len(f.submission.receipt.counts) != 0 || len(f.submission.sent()) != 0 {
		t.Fatal("conflicting original signatures reached a chain read", code)
	}
}
