//go:build linux || darwin

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Real configured activations, source signatures and journal admissions drive
// the plan/fee tests. The small capacity observations below are syntax inputs;
// the separate real-store roots exercise signed lifetime and index replay.
func evidenceRelayContinuationTest(t *testing.T) (*runtimeEvidenceProvisionV2TestFixture, *Executor, EvidenceRelayContinuation) {
	t.Helper()
	fixture, horizon := newEvidenceRelayHorizonTestFixture(t)
	if fixture.cfg.Config.Budgets.MaximumEVMFeePerGasWei != 2*evidenceRelayContinuationFee || fixture.cfg.Config.ValidatorEvidenceRelay.GasUnits != evidenceRelayContinuationGas {
		t.Fatal("original relay fixture lost its100gwei/one-million-gas approval")
	}
	journal, err := OpenJournal(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
	})
	executor := &Executor{cfg: fixture.cfg, plan: fixture.plan, roles: fixture.roles, stateDir: fixture.stateDir, journal: journal}
	old := evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], fixture.prepared.Epoch, false, 0)
	action, err := executor.admitEvidenceRelayAction(t.Context(), old)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(JournalEntry{DeploymentID: fixture.plan.DeploymentID, PlanHash: fixture.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFailed, Error: "retained failed attempt"}); err != nil {
		t.Fatal(err)
	}
	reserve, err := exactPlanActionByID(fixture.plan, evidenceRelayReserveId)
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := horizon.work.remaining("release-1.0", false)
	if err != nil {
		t.Fatal(err)
	}
	span := remaining + 720
	c := EvidenceRelayContinuation{Schema: evidenceRelayContinuationSchema, SourcePlanHash: fixture.plan.PlanHash, ConfigHash: fixture.cfg.ConfigHash, ActivationPlanHash: fixture.plan.PlanHash, PreparedSHA256: bytesSHA256(fixture.preparedBytes), CompletedSHA256: bytesSHA256([]byte("retained completed fixture")), JournalHash: journal.Entries()[len(journal.Entries())-1].EntryHash, OriginalReserve: reserve, EVMHead: ChainHead{Number: horizon.anchorBlock + 11000, Hash: common.Hash{0x51}.Hex()}, NativeHead: ChainHead{Number: 12000, Hash: common.Hash{0x52}.Hex()}, SettlementEpoch: horizon.anchorEpoch + 37, NativeEpoch: 110, EndBlock: horizon.anchorBlock + 11000 + span, RequiredWorkBlocks: remaining, TransactionsSHA256: bytesSHA256([]byte("[]"))}
	closed, err := evidenceRelayContinuationCeil(span, horizon.work.settlementCadence)
	if err != nil {
		t.Fatal(err)
	}
	native, err := evidenceRelayContinuationCeil(span, horizon.work.nativeCadence)
	if err != nil {
		t.Fatal(err)
	}
	c.EndSettlementEpoch, c.EndNativeEpoch = c.SettlementEpoch+closed, c.NativeEpoch+native
	for _, member := range fixture.prepared.Members {
		identity := validatorcomponent.AttemptLedgerIdentity{DeploymentID: fixture.plan.DeploymentID, ChainID: fixture.plan.ChainID, GenesisHash: fixture.plan.GenesisHash, Netuid: fixture.plan.Netuid, ValidatorID: member.ValidatorId, ValidatorUID: member.ValidatorUid, NoID: member.NoId, ValidatorVPK: fmt.Sprintf("0x%x", member.Activation.VPK)}
		c.Sources = append(c.Sources, EvidenceRelayContinuationSource{ValidatorID: member.ValidatorId, NoID: member.NoId, CoordinatorStateDir: filepath.Join(fixture.stateDir, "runtime", fmt.Sprintf("validator-%d", member.ValidatorId), "coordinator-state-v2"), IntentPrefixSHA256: bytesSHA256(nil), Activation: member.Activation, Capacity: validatorcomponent.StoppedAttemptLedgerCapacity{Identity: identity, Coordinator: fixture.plan.Deployment.CoordinatorProxy.Hex(), Head: validatorcomponent.AttemptLedgerHead{Root: common.Hash{}.Hex()}}})
		c.Retained = append(c.Retained, evidenceRelayLaunchRequestTest(t, fixture, member, member.Activation.Domain.Epoch, false, 0))
	}
	c.Debits, _, err = readEvidenceRelayContinuationDebits(t.Context(), fixture.stateDir, fixture.plan, journal.Entries())
	if err != nil {
		t.Fatal(err)
	}
	c.Retained, err = canonicalEvidenceRelayContinuationRequests(c.Retained)
	if err != nil {
		t.Fatal(err)
	}
	c.NewSlots, c.HistoricalLiabilityWei, err = evidenceRelayContinuationNewSlots(reserve.Spend.EVMGasWei, c.Debits)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.validateClocks(horizon.work); err != nil {
		t.Fatal(err)
	}
	return fixture, executor, c
}

func TestEvidenceRelayContinuationPreservesHigherFeeRetryAndRevisionOwnership(t *testing.T) {
	fixture, executor, c := evidenceRelayContinuationTest(t)
	before := executor.plan
	plan, err := appendEvidenceRelayContinuationPlan(before, c)
	if err != nil {
		t.Fatal(err)
	}
	if plan.MaximumSpend != before.MaximumSpend || plan.SupersededSpend != before.SupersededSpend || plan.Limits != before.Limits || c.NewSlots != 510 || c.HistoricalLiabilityWei != "100000000000000000" || fixture.cfg.Config.ValidatorEvidenceRelay.MaxSlots != 256 {
		t.Fatal("continuation refunded the old failed debit or raised monetary/source allowances")
	}
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, plan, fixture.roles); err != nil {
		t.Fatal(err)
	}
	executor.plan = plan
	if err := validateEvidenceRelayContinuationSource(fixture.stateDir, plan, executor.journal.Entries()); err != nil {
		t.Fatal(err)
	}
	old := evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], fixture.prepared.Epoch, false, 0)
	path := filepath.Join(fixture.stateDir, "evidence-relay", stringsTrimRelayPrefix(c.Debits[0].ActionID)+".json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count := len(executor.journal.Entries())
	action, owner, err := executor.admitOwnedEvidenceRelayAction(t.Context(), old)
	if err != nil || owner != before.PlanHash || action.Spend.EVMGasWei != c.Debits[0].AllowanceWei || len(executor.journal.Entries()) != count {
		t.Fatal("continuation rewrote or re-debited original retry", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("original signed request bytes changed", err)
	}
	next := evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], fixture.prepared.Epoch+1, false, 0)
	newAction, newOwner, err := executor.admitOwnedEvidenceRelayAction(t.Context(), next)
	if err != nil || newOwner != plan.PlanHash || newAction.Spend.EVMGasWei != "50000000000000000" {
		t.Fatal("new subject did not use the bounded50gwei allowance", err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var revised SetupPlan
	if err := json.Unmarshal(raw, &revised); err != nil {
		t.Fatal(err)
	}
	revised.PriorPlanHashes = append(revised.PriorPlanHashes, plan.PlanHash)
	revised.GeneratedAt = "2026-09-12T00:00:00Z"
	revised.PlanHash = ""
	revised.PlanHash, err = revised.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, &revised, fixture.roles); err != nil {
		t.Fatal(err)
	}
	executor.plan = &revised
	count = len(executor.journal.Entries())
	retained, owner, err := executor.admitOwnedEvidenceRelayAction(t.Context(), next)
	if err != nil || owner != plan.PlanHash || retained.IntentHash != newAction.IntentHash || len(executor.journal.Entries()) != count {
		t.Fatal("later plan revision reset a continued nonce/request owner", err)
	}
	entries, maximum, err := executor.evidenceRelayAdmissionEntries()
	if err != nil || maximum != 510 || len(entries) != 1 || entries[0].PlanHash != revised.PlanHash {
		t.Fatal("later revision lost aggregate new-subject allocation", err)
	}
	if _, err := appendEvidenceRelayContinuationPlan(&revised, c); err == nil {
		t.Fatal("a second continuation moved the original fixed end")
	}
}

func TestEvidenceRelayContinuationKeepsOldCatchupAndFiniteFutureSubjects(t *testing.T) {
	fixture, _, c := evidenceRelayContinuationTest(t)
	work, err := evidenceRelayConfiguredWork(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	anchor := fixture.prepared.Members[0].Activation
	horizon := &evidenceRelayHorizon{work: work, maximum: c.NewSlots + uint64(len(c.Debits)), anchorBlock: anchor.EVMBlock, anchorEpoch: anchor.Domain.Epoch, anchorNativeEpoch: 77, continuation: &c, sourceKVs: map[evidenceRelayHorizonSource]protocol.ValidatorEvidenceActivation{}, headerKVs: map[[32]byte]protocol.ValidatorEvidenceHeader{}}
	for _, source := range c.Sources {
		horizon.sourceKVs[evidenceRelayHorizonSource{source.Activation.Hotkey, source.NoID}] = source.Activation
	}
	if err := horizon.requireRemaining(c.EVMHead.Number, c.NativeEpoch, c.RequiredWorkBlocks); err != nil {
		t.Fatal("approved past-age continuation failed", err)
	}
	base, err := c.requiredSubjects(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := uint64(4) * (c.EndSettlementEpoch - anchor.Domain.Epoch + 1 + c.EndNativeEpoch - c.NativeEpoch + 1)
	if base != want {
		t.Fatal("missing historical closed census slots were not reserved", base, want)
	}
	for _, request := range c.Retained {
		if err := horizon.admit(request.Evidence.Header, c.EVMHead.Number); err != nil {
			t.Fatal("original delayed closed request refused", err)
		}
	}
	horizon.maximum = base + 1
	first := evidenceRelayHorizonAuditTest(t, fixture, fixture.prepared.Members[0], 1)
	second := evidenceRelayHorizonAuditTest(t, fixture, fixture.prepared.Members[0], 2)
	if err := horizon.admit(first.Evidence.Header, c.EVMHead.Number); err != nil {
		t.Fatal(err)
	}
	count := len(horizon.headerKVs)
	if err := horizon.admit(first.Evidence.Header, c.EVMHead.Number); err != nil || len(horizon.headerKVs) != count {
		t.Fatal("exact original audit retry changed allocation", err)
	}
	if err := horizon.admit(second.Evidence.Header, c.EVMHead.Number); err == nil || len(horizon.headerKVs) != count {
		t.Fatal("extra old same-native subject was forgiven")
	}
	if err := horizon.requireRemaining(c.EndBlock-c.RequiredWorkBlocks+1, c.NativeEpoch, c.RequiredWorkBlocks); err == nil {
		t.Fatal("restart shifted the approved remaining-work deadline")
	}
	restart := *horizon
	restart.minimumEnd = 0
	restart.minimumNativeEnd = 0
	if err := restart.admit(first.Evidence.Header, c.EndBlock+1); err == nil {
		t.Fatal("another phase reset the fixed continuation end")
	}
}

func TestEvidenceRelayContinuationRejectsChangedSourceAndMissingRetainedPublicInput(t *testing.T) {
	fixture, executor, c := evidenceRelayContinuationTest(t)
	plan, err := appendEvidenceRelayContinuationPlan(fixture.plan, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, plan, fixture.roles); err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRelayContinuationRetained(&c, c.Retained); err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRelayContinuationRetained(&c, c.Retained[1:]); err == nil {
		t.Fatal("omitted approved subject acquired continuation authority")
	}
	for _, mutate := range []func(*EvidenceRelayContinuation){
		func(v *EvidenceRelayContinuation) { v.SourcePlanHash = common.Hash{0x99}.Hex() },
		func(v *EvidenceRelayContinuation) { v.EndBlock++ },
		func(v *EvidenceRelayContinuation) { v.NewSlots++ },
		func(v *EvidenceRelayContinuation) { v.Debits = nil; v.NewSlots = 512; v.HistoricalLiabilityWei = "0" },
		func(v *EvidenceRelayContinuation) {
			v.Sources[0].CoordinatorStateDir = filepath.Join(fixture.stateDir, "other", "coordinator-state-v2")
		},
		func(v *EvidenceRelayContinuation) { v.Retained[0].Evidence.HotkeySignature[0] ^= 1 },
	} {
		raw, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		var changed SetupPlan
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		mutate(changed.EvidenceRelayContinuation)
		if err := validateEvidenceRelayContinuationSource(fixture.stateDir, &changed, executor.journal.Entries()); err == nil {
			t.Fatal("changed continuation reused an existing approval")
		}
	}
	if c.OriginalReserve.Spend.EVMGasWei != "25600000000000000000" {
		t.Fatal("fixture lost its actual original monetary reservation")
	}
}

func TestEvidenceRelayContinuationCapacityRetainsFullMeasuredLifetimeAndTail(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	bounds := cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds
	observed := validatorcomponent.StoppedAttemptLedgerCapacity{Head: validatorcomponent.AttemptLedgerHead{LastSequence: 134673, TrailCount: 16958, RecordBytes: 698568804}, StorageBytes: 283301268, StorageFiles: 100}
	if err := validateEvidenceRelayContinuationCapacity(cfg, bounds, 6927, observed); err != nil {
		t.Fatal("existing measured lifetime plus unchanged complete watchdog work failed", err)
	}
	for _, mutate := range []func(*validatorcomponent.StoppedAttemptLedgerCapacity){
		func(v *validatorcomponent.StoppedAttemptLedgerCapacity) {
			v.Head.LastSequence = bounds.Disk.MaxRecordCount
		},
		func(v *validatorcomponent.StoppedAttemptLedgerCapacity) {
			v.Head.TrailCount = bounds.Disk.MaxTrailCount
		},
		func(v *validatorcomponent.StoppedAttemptLedgerCapacity) {
			v.Head.RecordBytes = bounds.Disk.MaxRawRecordBytes
		},
		func(v *validatorcomponent.StoppedAttemptLedgerCapacity) {
			v.StorageBytes = bounds.Disk.MaxStorageBytes + 1
		},
	} {
		changed := observed
		mutate(&changed)
		if err := validateEvidenceRelayContinuationCapacity(cfg, bounds, 6927, changed); err == nil {
			t.Fatal("retained source use was treated as fresh capacity")
		}
	}
	if cfg.Config.ValidatorEvidenceRelay.MaxSlots != 256 {
		t.Fatal("relay partition silently enlarged source storage admission")
	}
}
