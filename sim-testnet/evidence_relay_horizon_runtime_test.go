//go:build linux || darwin

// Real transport cancellation, original activation production and native
// eligibility feed phase admission. No injected verdict can publish readiness.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
)

// The earlier allowance reaches the real finalized observer but cannot
// authorize preparation, native work, request admission or an onchain send.
func TestEvidenceRelayHorizonRefusesBeforePreparationAndSpend(t *testing.T) {
	fixture := newEvidenceRelayLaunchRuntimeTestFixture(t, false, 200)
	journal, err := OpenJournal(fixture.base.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	fixture.executor.journal = journal
	before, err := os.ReadFile(journal.path)
	if err != nil {
		t.Fatal(err)
	}
	failures := make(chan error, 1)
	worker, err := newEvidenceRelayRuntime(t.Context(), fixture.base.cfg, fixture.executor, "release-1.0", false, func(err error) { failures <- err })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	if err := worker.WaitReady(t.Context()); err == nil || !strings.Contains(err.Error(), "insufficient remaining horizon") {
		t.Fatal("insufficient original allowance published funded readiness", err)
	}
	if err := worker.RequirePrepared(t.Context()); err == nil {
		t.Fatal("failed preflight authorized completed preparation")
	}
	if err := worker.Close(); err == nil || !strings.Contains(err.Error(), "insufficient remaining horizon") {
		t.Fatal("join discarded actual funding refusal", err)
	}
	select {
	case failure := <-failures:
		if !strings.Contains(failure.Error(), "insufficient remaining horizon") {
			t.Fatal(failure)
		}
	default:
		t.Fatal("real funding refusal was not reported to the campaign owner")
	}
	select {
	case <-worker.ready:
		t.Fatal("a refused worker published readiness")
	default:
	}
	select {
	case <-fixture.headExited:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	if fixture.requests.Load() != 2 || fixture.forwarded.Load() != 2 || worker.horizon != nil || len(journal.Entries()) != 0 {
		t.Fatal("funding refusal crossed its read-only preflight boundary")
	}
	after, err := os.ReadFile(journal.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("phase refusal changed original journal bytes", err)
	}
	after, err = os.ReadFile(filepath.Join(fixture.base.stateDir, "plan.json"))
	if err != nil || !bytes.Equal(fixture.planBytes, after) {
		t.Fatal("phase refusal rewrote approved allowance", err)
	}
	if _, err := os.Lstat(filepath.Join(fixture.base.stateDir, "evidence-relay")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refused preflight created a transaction request", err)
	}
}

// Readiness cannot outrun a real in-flight finalized request. Client
// cancellation, not server-side cleanup, releases and joins its actual handler.
func TestEvidenceRelayHorizonCancellationCannotPublishReadiness(t *testing.T) {
	fixture := newEvidenceRelayLaunchRuntimeTestFixture(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	failures := make(chan error, 1)
	worker, err := newEvidenceRelayRuntime(ctx, fixture.base.cfg, fixture.executor, "release-1.0", false, func(err error) { failures <- err })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	select {
	case <-fixture.headStarted:
	case failure := <-failures:
		t.Fatal(failure)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	select {
	case <-worker.ready:
		t.Fatal("in-flight transport published phase readiness")
	default:
	}
	cancel()
	if err := worker.WaitReady(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled caller obtained phase admission", err)
	}
	if err := worker.Close(); err != nil {
		t.Fatal("owned cancellation became a source failure", err)
	}
	select {
	case <-fixture.headExited:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	select {
	case <-worker.ready:
		t.Fatal("joined cancellation published readiness")
	default:
	}
	select {
	case failure := <-failures:
		t.Fatal("owned cancellation triggered campaign failure", failure)
	default:
	}
	if worker.horizon != nil || fixture.requests.Load() != 2 || fixture.forwarded.Load() != 2 {
		t.Fatal("cancelled preflight retained authority or started another read")
	}
	if _, err := os.Lstat(filepath.Join(fixture.base.stateDir, "evidence-relay")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled preflight created a transaction request", err)
	}
}

// This method-level positive follows the actual four-consent producer and
// the real metadata-qualified native reader. The fixture's private tiny runtime
// pins are not represented as an approved production plan or finalized send.
func TestEvidenceRelayHorizonReadsOriginalAndCurrentNativeAuthority(t *testing.T) {
	fixture := newRuntimeEvidenceActivationRpcV2TestFixture(t)
	fixture.executor.stateDir = filepath.Join(t.TempDir(), "state")
	journal, err := OpenJournal(fixture.executor.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	fixture.executor.journal = journal
	prepared, original, err := fixture.executor.prepareRuntimeEvidenceActivationsV2(t.Context(), fixture.chain)
	if err != nil || prepared == nil || len(prepared.Members) != 4 {
		t.Fatal("actual original source production failed", err)
	}
	newRuntime := func() *evidenceRelayRuntime {
		value := &evidenceRelayRuntime{ctx: t.Context(), executor: fixture.executor, chain: fixture.chain, phase: "production-soak", prepared: true,
			origins: [2]string{fixture.base.cfg.OperatorAPIOrigins[0], fixture.base.cfg.OperatorAPIOrigins[1]}}
		for _, configured := range fixture.base.cfg.Config.ValidatorEvidenceV2 {
			source := evidenceRelaySource{validatorId: configured.ValidatorID, bounds: configured.Evidence.Bounds,
				stateDir: filepath.Join(fixture.executor.stateDir, "runtime", fmt.Sprintf("validator-%d", configured.ValidatorID), "state")}
			for _, member := range prepared.Members {
				if member.ValidatorId == source.validatorId {
					source.activations = append(source.activations, member.Activation)
				}
			}
			if len(source.activations) != 2 {
				t.Fatal("independent configured source census changed")
			}
			value.sources = append(value.sources, source)
		}
		return value
	}
	func() {
		fixture.stateLock.Lock()
		defer fixture.stateLock.Unlock()
		fixture.finalizedEvmNumber = 210
		fixture.finalizedEvmHash = common.Hash{0x33}
	}()
	beforeCalls := fixture.callCount("state_call")
	beforeJournal, err := os.ReadFile(journal.path)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newRuntime()
	if err := runtime.prepareHorizon(); err != nil {
		t.Fatal("genuine empty pending census failed independent preflight", err)
	}
	remaining, err := runtime.work.remaining("production-soak", true)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.horizon == nil || runtime.horizon.anchorBlock != 200 || runtime.horizon.anchorNativeEpoch != 77 || runtime.horizon.minimumEnd != 210+remaining || len(runtime.horizon.sourceKVs) != 4 || len(runtime.horizon.headerKVs) != 0 || fixture.callCount("state_call") != beforeCalls+3 {
		t.Fatal("preflight substituted Evm height, skipped native eligibility or lost original sources")
	}
	for _, member := range prepared.Members {
		key := evidenceRelayHorizonSource{hotkey: member.Activation.Hotkey, noId: member.NoId}
		if runtime.horizon.sourceKVs[key] != member.Activation {
			t.Fatal("preflight replaced an original activation")
		}
		if err := member.Activation.Verify(member.Activation, member.VpkSignature, member.HotkeySignature); err != nil {
			t.Fatal(err)
		}
	}
	func() { fixture.stateLock.Lock(); defer fixture.stateLock.Unlock(); fixture.permits[1] = false }()
	beforeStorage := fixture.callCount("state_getStorage")
	refused := newRuntime()
	if err := refused.prepareHorizon(); err == nil || !strings.Contains(err.Error(), "independent finalized eligibility/schedule") || refused.horizon != nil || fixture.callCount("state_getStorage") <= beforeStorage {
		t.Fatal("lost actual native permit published a funded horizon", err)
	}
	afterJournal, err := os.ReadFile(journal.path)
	if err != nil || !bytes.Equal(beforeJournal, afterJournal) || len(journal.Entries()) != 0 {
		t.Fatal("native preflight mutated original transaction debits", err)
	}
	after, err := os.ReadFile(filepath.Join(fixture.executor.stateDir, "evidence-v2-setup", "prepared.json"))
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("native refusal replaced original signed source bytes", err)
	}
	for _, source := range runtime.sources {
		if _, err := os.Lstat(source.stateDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("read-only empty discovery created a validator state owner", err)
		}
	}
	if runtime.horizon.sourceKVs[evidenceRelayHorizonSource{hotkey: prepared.Members[0].Activation.Hotkey, noId: prepared.Members[0].NoId}] == (protocol.ValidatorEvidenceActivation{}) {
		t.Fatal("positive horizon was overwritten by independent refusal")
	}
}
