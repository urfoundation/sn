//go:build linux || darwin

// Checked full-profile arithmetic and actual original signed admissions bind
// capacity independently of arrival order. No transaction finality is claimed.
package main

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Original generated source identities own four slots per epoch. The native
// epoch here is an arithmetic input; separate real-Rpc tests authenticate it.
func newEvidenceRelayHorizonTestFixture(t *testing.T) (*runtimeEvidenceProvisionV2TestFixture, *evidenceRelayHorizon) {
	t.Helper()
	fixture := newRuntimeEvidenceProvisionV2ConfiguredTestFixture(t, func(cfg *ResolvedConfig) { cfg.Config.ValidatorEvidenceRelay.MaxSlots = 256 })
	work, err := evidenceRelayConfiguredWork(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	anchor := fixture.prepared.Members[0].Activation
	horizon := &evidenceRelayHorizon{work: work, maximum: fixture.cfg.Config.ValidatorEvidenceRelay.MaxSlots, anchorBlock: anchor.EVMBlock, anchorEpoch: anchor.Domain.Epoch, anchorNativeEpoch: 77,
		sourceKVs: map[evidenceRelayHorizonSource]protocol.ValidatorEvidenceActivation{}, headerKVs: map[[32]byte]protocol.ValidatorEvidenceHeader{}}
	for _, member := range fixture.prepared.Members {
		horizon.sourceKVs[evidenceRelayHorizonSource{hotkey: member.Activation.Hotkey, noId: member.NoId}] = member.Activation
	}
	return fixture, horizon
}

// Later observations under one native subject remain valid distinct slots.
// Signing uses the same real configured keys as original admission tests.
func evidenceRelayHorizonAuditTest(t *testing.T, fixture *runtimeEvidenceProvisionV2TestFixture, member runtimeEvidenceActivationMemberV2, observationOffset uint64) validatorcomponent.ValidatorEvidenceTransactionV2Expected {
	t.Helper()
	request := evidenceRelayLaunchRequestTest(t, fixture, member, member.Activation.Domain.Epoch, true, 77)
	request.Evidence.Header.Subject.ObservationEpoch = member.Activation.Domain.Epoch + observationOffset
	request.Evidence.Header.BoundaryBlock = request.Window.EndBlock + 300*(observationOffset-1)
	request.Window.Subject = request.Evidence.Header.Subject
	request.Window.FinalizedBlock = request.Evidence.Header.BoundaryBlock
	hotkey, key, err := runtimeEvidenceActivationKeysV2(fixture.roles, member.ValidatorId, member.NoId)
	if err != nil {
		t.Fatal(err)
	}
	request.Evidence.VPKSignature, err = request.Evidence.Header.SignVPK(key)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := request.Evidence.Header.Digest()
	if err != nil {
		t.Fatal(err)
	}
	request.Evidence.HotkeySignature, err = hotkey.Sign(digest[:])
	if err != nil {
		t.Fatal(err)
	}
	domain, err := member.Activation.EvidenceDomain()
	if err != nil {
		t.Fatal(err)
	}
	if err := request.Evidence.Header.Verify(domain, request.Window, request.Evidence.VPKSignature, request.Evidence.HotkeySignature); err != nil {
		t.Fatal(err)
	}
	return request
}

// The former nominal14-hour estimate did not include the actual watchdogs
// and preparation waits. Both phases, terminal slack and all four owners count.
func TestEvidenceRelayHorizonUsesActualFullPopulationPhaseClocks(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	if cfg.Config.Topology.Miners != 1000 || cfg.Config.Topology.HeadSlots != 200 || cfg.Config.Topology.Validators != 2 || cfg.Config.Topology.Operators != 2 {
		t.Fatal("full launch population changed")
	}
	work, err := evidenceRelayConfiguredWork(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if work.releasePreparation != 1338 || work.releaseObservation != 2780 || work.productionPreparation != 1169 || work.productionObservation != 1640 || work.releaseWarmup != 1530 || work.productionWarmup != 1620 || work.settlementCadence != 300 || work.nativeCadence != 360 {
		t.Fatalf("actual configured phase/preparation geometry changed: %+v", work)
	}
	remaining, err := work.remaining("release-1.0", false)
	if err != nil || remaining != 10077 {
		t.Fatal("release entry omitted remaining production/preparation work", remaining, err)
	}
	for _, entry := range []struct {
		phase    string
		prepared bool
		want     uint64
	}{
		{phase: "release-1.0", prepared: true, want: 8739}, {phase: "production-soak", prepared: false, want: 4429}, {phase: "production-soak", prepared: true, want: 3260},
	} {
		value, err := work.remaining(entry.phase, entry.prepared)
		if err != nil || value != entry.want {
			t.Fatalf("remaining work %+v: %d %v", entry, value, err)
		}
	}
	required, closed, native, err := evidenceRelayForecast(work, 4, remaining)
	if err != nil || required != 256 || closed != 35 || native != 29 {
		t.Fatal("required original-source forecast differs", required, closed, native, err)
	}
	span, closed, native, err := evidenceRelayConfiguredHorizon(cfg)
	if err != nil || span != 10080 || closed != 35 || native != 29 || cfg.Config.ValidatorEvidenceRelay.MaxSlots-required != 0 {
		t.Fatal("finite delay/extra-subject headroom differs", span, closed, native, err)
	}
	if required > cfg.Config.ValidatorEvidenceRelay.MaxSlots {
		t.Fatal("launch allowance cannot fund its own minimum work")
	}
	t.Logf("actual geometry: 1000 miners/200 heads/four sources; minimum=%d blocks/%d source slots; finite no-extra horizon=%d blocks; shared delay/extra slots=%d", remaining, required, span, cfg.Config.ValidatorEvidenceRelay.MaxSlots-required)
}

// The anchor cannot reset on restart or production entry. Exact remaining
// work fits at the boundary; one additional elapsed block fails without state.
func TestEvidenceRelayHorizonRefusesOldAllowanceAndNonresettableAge(t *testing.T) {
	_, horizon := newEvidenceRelayHorizonTestFixture(t)
	remaining, err := horizon.work.remaining("release-1.0", false)
	if err != nil {
		t.Fatal(err)
	}
	horizon.maximum = 128
	if err := horizon.requireRemaining(horizon.anchorBlock, 77, remaining); err == nil || horizon.minimumEnd != 0 {
		t.Fatal("original128 slots admitted the actual full phase clocks", err)
	}
	horizon.maximum = 256
	maximum, _, _, err := horizon.ceilings(nil)
	if err != nil {
		t.Fatal(err)
	}
	latest := maximum - remaining
	if err := horizon.requireRemaining(latest, 77, remaining); err != nil || horizon.minimumEnd != maximum {
		t.Fatal("exact remaining-work boundary refused", err)
	}
	if err := horizon.requireRemaining(latest+1, 77, remaining); err == nil || horizon.minimumEnd != maximum {
		t.Fatal("one-over age bought a new activation horizon", err)
	}
	restarted := *horizon
	restarted.minimumEnd = 0
	production, err := restarted.work.remaining("production-soak", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.requireRemaining(maximum-production, 77, production); err != nil {
		t.Fatal("authenticated completed preparation was charged twice", err)
	}
	if err := restarted.requireRemaining(maximum-production+1, 77, production); err == nil {
		t.Fatal("production restart reset the original horizon")
	}
}

// All valid extra subjects consume finite capacity regardless of delivery
// order. No native-epoch collapse or new monotonicity rejection is introduced.
func TestEvidenceRelayHorizonCountsOutOfOrderAdditionalAuditSubjects(t *testing.T) {
	fixture, horizon := newEvidenceRelayHorizonTestFixture(t)
	remaining, err := horizon.work.remaining("release-1.0", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := horizon.requireRemaining(horizon.anchorBlock, 77, remaining); err != nil {
		t.Fatal(err)
	}
	var headers []protocol.ValidatorEvidenceHeader
	for _, member := range fixture.prepared.Members {
		for offset := uint64(1); offset <= 19; offset++ {
			headers = append(headers, evidenceRelayHorizonAuditTest(t, fixture, member, offset).Evidence.Header)
		}
	}
	for index := len(headers) - 1; index >= 0; index-- {
		if err := horizon.admit(headers[index], 6000); err != nil {
			t.Fatalf("valid reverse-delivered subject %d refused: %v", index, err)
		}
	}
	extra, err := horizon.extraSubjects(nil)
	if err != nil || extra != 72 || len(horizon.headerKVs) != 76 {
		t.Fatal("same-native later observations were collapsed or uncharged", extra, err)
	}
	for _, header := range headers {
		if err := horizon.admit(header, 6000); err != nil {
			t.Fatal("exact retry changed original debit", err)
		}
	}
	if len(horizon.headerKVs) != 76 {
		t.Fatal("retry or delivery order minted extra funded slots")
	}
	before := horizon.minimumEnd
	oneOver := evidenceRelayHorizonAuditTest(t, fixture, fixture.prepared.Members[0], 20)
	if err := horizon.admit(oneOver.Evidence.Header, 6300); err == nil || len(horizon.headerKVs) != 76 || horizon.minimumEnd != before {
		t.Fatal("seventy-third extra subject consumed required remaining phase work", err)
	}
	for _, header := range headers {
		slot, err := header.SlotKey()
		if err != nil || horizon.headerKVs[slot] != header {
			t.Fatal("funding refusal discarded an original source", err)
		}
	}
}

// Failed original transactions still consume their exact debit on reopen.
// Changed/absent records cannot refund it or reach any new request admission.
func TestEvidenceRelayHorizonAuthenticatesFailedOriginalDebitsOnReopen(t *testing.T) {
	fixture, horizon := newEvidenceRelayHorizonTestFixture(t)
	journal, err := OpenJournal(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir, journal: journal}
	t.Cleanup(func() {
		if executor.journal != nil {
			if err := executor.journal.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	requests := []validatorcomponent.ValidatorEvidenceTransactionV2Expected{
		evidenceRelayHorizonAuditTest(t, fixture, fixture.prepared.Members[0], 2),
		evidenceRelayHorizonAuditTest(t, fixture, fixture.prepared.Members[0], 1),
		evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[1], horizon.anchorEpoch, false, 0),
	}
	var first Action
	for index, request := range requests {
		action, err := executor.admitEvidenceRelayAction(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = action
			if err := journal.Append(JournalEntry{DeploymentID: fixture.plan.DeploymentID, PlanHash: fixture.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFailed}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	executor.journal, err = OpenJournal(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &evidenceRelayRuntime{executor: executor}
	before, err := os.ReadFile(filepath.Join(fixture.stateDir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.readAdmittedHorizon(t.Context(), horizon, 1000); err != nil {
		t.Fatal("original failed/prior-phase journal set failed authentication", err)
	}
	extra, err := horizon.extraSubjects(nil)
	if err != nil || extra != 1 || len(horizon.headerKVs) != 3 {
		t.Fatal("failed or delayed original debit was omitted", extra, err)
	}
	if err := runtime.readAdmittedHorizon(t.Context(), horizon, 1000); err != nil || len(horizon.headerKVs) != 3 {
		t.Fatal("reopened exact retry minted another slot", err)
	}
	path := filepath.Join(fixture.stateDir, "evidence-relay", strings.TrimPrefix(first.ID, evidenceRelayActionPrefix)+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(append([]byte(nil), original...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runtime.readAdmittedHorizon(t.Context(), horizon, 1000); err == nil {
		t.Fatal("changed original request refunded or escaped its debit")
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".preserved"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.readAdmittedHorizon(t.Context(), horizon, 1000); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing failed original became an uncharged slot", err)
	}
	if err := os.Rename(path+".preserved", path); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runtime.readAdmittedHorizon(cancelled, horizon, 1000); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled inventory was acknowledged", err)
	}
	after, err := os.ReadFile(filepath.Join(fixture.stateDir, "journal.jsonl"))
	if err != nil || !bytes.Equal(before, after) || len(horizon.headerKVs) != 3 {
		t.Fatal("read-only refusal mutated original debits", err)
	}
}

// Integer extremes and changed source/retry identities fail before mutation.
func TestEvidenceRelayHorizonRejectsOverflowAndChangedOriginalHeader(t *testing.T) {
	fixture, horizon := newEvidenceRelayHorizonTestFixture(t)
	request := evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], horizon.anchorEpoch, false, 0)
	if err := horizon.admit(request.Evidence.Header, 600); err != nil {
		t.Fatal(err)
	}
	before := maps.Clone(horizon.headerKVs)
	changed := request.Evidence.Header
	changed.PayloadHash[0] ^= 1
	if err := horizon.admit(changed, 600); err == nil || len(horizon.headerKVs) != 1 {
		t.Fatal("same-slot changed payload replaced its original", err)
	}
	changed = request.Evidence.Header
	changed.NoID = 99
	if err := horizon.admit(changed, 600); err == nil || len(horizon.headerKVs) != 1 {
		t.Fatal("unconfigured source spent the original allowance", err)
	}
	for _, fault := range []string{"slots", "anchor", "native", "remaining"} {
		candidate := *horizon
		var err error
		switch fault {
		case "slots":
			candidate.maximum = ^uint64(0)
			_, _, _, err = candidate.ceilings(nil)
		case "anchor":
			candidate.anchorBlock = ^uint64(0) - 1
			_, _, _, err = candidate.ceilings(nil)
		case "native":
			candidate.anchorNativeEpoch = ^uint64(0)
			_, _, _, err = candidate.ceilings(nil)
		case "remaining":
			err = candidate.requireRemaining(horizon.anchorBlock, 77, ^uint64(0))
		}
		if err == nil || !reflect.DeepEqual(candidate.headerKVs, before) {
			t.Fatalf("%s arithmetic overflow changed approval: %v", fault, err)
		}
	}
	cfg := runtimeEvidenceLaunchConfigTest(t)
	cfg.Public.Chain.ExpectedBlockSeconds = ^uint64(0)
	if _, err := evidenceRelayConfiguredWork(cfg); err == nil {
		t.Fatal("overflowed actual scenario duration became positive funded work")
	}
	if _, err := horizon.work.remaining("unknown", false); err == nil {
		t.Fatal("unknown campaign phase borrowed release work")
	}
}

// Native progress is independently observed, not derived from Evm height.
// Preparation must leave its later work funded before the next spend window.
func TestEvidenceRelayHorizonReservesNativeAndInFlightPreparationWork(t *testing.T) {
	fixture, horizon := newEvidenceRelayHorizonTestFixture(t)
	remaining, err := horizon.work.remaining("release-1.0", true)
	if err != nil {
		t.Fatal(err)
	}
	block, _, native, err := horizon.ceilings(nil)
	if err != nil {
		t.Fatal(err)
	}
	nativeRemaining := remaining / horizon.work.nativeCadence
	if remaining%horizon.work.nativeCadence != 0 {
		nativeRemaining++
	}
	lastNative := native - nativeRemaining
	if err := horizon.requireRemaining(horizon.anchorBlock, lastNative, remaining); err != nil {
		t.Fatal("exact observed native room was refused", err)
	}
	before := horizon.minimumEnd
	beforeNative := horizon.minimumNativeEnd
	if err := horizon.requireRemaining(horizon.anchorBlock, lastNative+1, remaining); err == nil || horizon.minimumEnd != before || horizon.minimumNativeEnd != beforeNative {
		t.Fatal("one extra native epoch consumed unfunded remaining work", err)
	}
	first := evidenceRelayHorizonAuditTest(t, fixture, fixture.prepared.Members[0], 1)
	if err := horizon.admit(first.Evidence.Header, first.Evidence.Header.BoundaryBlock); err != nil {
		t.Fatal("original delayed native subject was refused", err)
	}
	// Four-source forecasts move in four-slot steps. The first four extras
	// shorten only the block ceiling; the fifth would remove a native epoch.
	for offset := uint64(2); offset <= 5; offset++ {
		extra := evidenceRelayHorizonAuditTest(t, fixture, fixture.prepared.Members[0], offset)
		if err := horizon.admit(extra.Evidence.Header, extra.Evidence.Header.BoundaryBlock); err != nil {
			t.Fatal("funded extra subject was refused before its real native boundary", err)
		}
	}
	extra := evidenceRelayHorizonAuditTest(t, fixture, fixture.prepared.Members[0], 6)
	if err := horizon.admit(extra.Evidence.Header, extra.Evidence.Header.BoundaryBlock); err == nil || len(horizon.headerKVs) != 5 || horizon.minimumNativeEnd != beforeNative {
		t.Fatal("extra subject consumed independently reserved native room", err)
	}
	block, _, _, err = horizon.ceilings(nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &evidenceRelayRuntime{phase: "release-1.0", work: horizon.work, horizon: horizon}
	if err := runtime.checkHorizonBlock(block - remaining); err != nil || horizon.minimumEnd != block {
		t.Fatal("exact in-flight preparation floor failed", err)
	}
	if err := runtime.checkHorizonBlock(block - remaining + 1); err == nil || horizon.minimumEnd != block {
		t.Fatal("ongoing preparation spent beyond its remaining observation floor", err)
	}
	runtime.prepared = true
	if err := runtime.checkHorizonBlock(block); err != nil {
		t.Fatal("completed preparation was incorrectly charged throughout observation", err)
	}
	if err := runtime.checkHorizonBlock(block + 1); err == nil {
		t.Fatal("completed preparation erased the original absolute horizon")
	}
}
