// Synthetic approvals exercise the failed-probe boundary and original native
// receipt routing independently of any live deployment or transaction writer.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Represents the already authenticated source consumed by the pure admission
// layer. Receipt files are persisted and decoded through the production reader.
type precompileProbeSuccessorFixture struct {
	cfg                        *ResolvedConfig
	source, plan               *SetupPlan
	payloads                   *DeploymentPayloads
	entries                    []JournalEntry
	stateDir                   string
	writeRecord, restoreRecord *ActionPostcondition
}

// Uses current generated payloads with a distinct synthetic prior probe hash.
func newPrecompileProbeSuccessorFixture(t *testing.T) *precompileProbeSuccessorFixture {
	t.Helper()
	cfg, payloads, retained, baseline, _ := replacementPrecompileProbeFixture(t)
	oldUpgrade := payloads.CoordinatorUpgrade
	oldPayloads := *payloads
	if err := configureCoordinatorUpgradeNonce(payloads, oldUpgrade.DeployerNonce+3); err != nil {
		t.Fatal(err)
	}
	payloads.FleetBatcherAddress, payloads.FleetBatcherNonce = oldPayloads.FleetBatcherAddress, oldPayloads.FleetBatcherNonce
	payloads.FleetBatcher, payloads.FleetBatcherRuntime, payloads.ValidatorEvidence = oldPayloads.FleetBatcher, oldPayloads.FleetBatcherRuntime, oldPayloads.ValidatorEvidence
	baseline.ReplacementPrecompileProbeHash = crypto.Keccak256Hash([]byte{0x60, 0x77}).Hex()
	baseline.PrecompileProbeExecutableHash = common.Hash{8}.Hex()
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := &SetupPlan{
		Schema: currentSetupPlanSchema, DeploymentID: cfg.Config.Deployment.DeploymentID,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ChainID: testnetChainID, GenesisHash: testnetGenesis, Netuid: cfg.Netuid,
		PlanHash: common.Hash{1}.Hex(), Roles: roles, Deployment: retained, CoordinatorUpgrade: payloads.CoordinatorUpgrade, CoordinatorUpgradeBaseline: baseline,
		CoordinatorRepairCarry: &CoordinatorRepairCarry{Request: signedCoordinatorRepairRequest{Request: coordinatorRepairRequest{OldUpgrade: oldUpgrade, Upgrade: payloads.CoordinatorUpgrade}}},
		Limits:                 Spend{TAORao: 200, AlphaRao: 300, EVMGasWei: "40"}, MaximumSpend: Spend{EVMGasWei: "40"},
	}
	for _, actionId := range []string{"precompile.probe-deploy", "precompile.commitment-write", "precompile.commitment-restore", "precompile.read-battery", "precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.dividend", "precompile.transfer-out", "fleet.refresh.deploy-batcher", "coordinator.repair-carry"} {
		action := Action{ID: actionId, Kind: "evm-read", Target: "synthetic-phase", Parameters: map[string]string{}}
		if slices.Contains(precompileProbeSuccessorTransactionIds(), actionId) {
			action.Kind = "evm-transaction"
		}
		if strings.HasPrefix(actionId, "precompile.") {
			action.Parameters[precompileProbeAddressParameter] = baseline.ReplacementPrecompileProbe
			action.Parameters[precompileProbeRuntimeParameter] = baseline.ReplacementPrecompileProbeHash
		}
		if precompileNativeAction(actionId) {
			action.Kind, action.Target = "substrate-extrinsic", "head-fleet:1"
			action.Parameters["canonical_generation"] = "2"
			action.Parameters[fleetCommitmentStorageParameter] = fleetCommitmentStorageV2
		}
		if actionId == "precompile.probe-deploy" {
			action.Kind = "evm-transaction"
			action.Spend.EVMGasWei = "7"
		}
		source.Actions = append(source.Actions, testFleetSupersessionAction(t, action))
	}
	source.Actions = append(source.Actions, testFleetSupersessionAction(t, Action{ID: "campaign.evm-gas-reserve", Kind: "budget-reserve", Target: "synthetic-campaign", Spend: Spend{EVMGasWei: "33"}}))
	complete := completePrecompileEvidence()
	complete.Commitment.WriteTransactionHash = common.Hash{21}.Hex()
	complete.Commitment.RestoreTransactionHash = common.Hash{22}.Hex()
	complete.Commitment.WriteFinalizedHead = ChainHead{Number: 90, Hash: common.Hash{23}.Hex()}
	complete.Commitment.RestoreFinalizedHead = ChainHead{Number: 100, Hash: common.Hash{24}.Hex()}
	complete.Commitment.WriteCommitmentBlock, complete.Commitment.RestoreCommitmentBlock = 90, 100
	coldkey := ss58Mirror(common.HexToAddress(baseline.ReplacementPrecompileProbe))
	evidence := PrecompileConformanceEvidence{
		Schema: "urnetwork-precompile-conformance-v1", DeploymentID: source.DeploymentID, ConfigHash: source.ConfigHash, PolicyHash: source.PolicyHash,
		ChainID: source.ChainID, GenesisHash: source.GenesisHash, Netuid: source.Netuid, ProbeAddress: baseline.ReplacementPrecompileProbe,
		ProbeColdkey: hexBytesValue(coldkey[:]), Owner: source.Roles.Deployer, SampleHotkey: common.Hash{4}.Hex(), SampleUID: 7,
		AbsentHotkey: common.Hash{5}.Hex(), MoveHotkey: common.Hash{6}.Hex(), RecoveryColdkey: common.Hash{7}.Hex(), Commitment: complete.Commitment,
	}
	evidence.EvidenceHash, err = canonicalHashHex(&evidence)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	owner := &Executor{cfg: cfg, stateDir: stateDir, plan: source}
	probe := actionByID(t, source, "precompile.probe-deploy")
	entries := []JournalEntry{{Sequence: 1, DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: probe.ID, IntentHash: probe.IntentHash, Stage: StageVerified}}
	records := []*ActionPostcondition{}
	for index, actionId := range []string{"precompile.commitment-write", "precompile.commitment-restore"} {
		action := actionByID(t, source, actionId)
		snapshot := evidence
		if index == 0 {
			snapshot.Commitment = PrecompileCommitmentEvidence{
				ProbeHash: evidence.Commitment.ProbeHash, CanonicalHash: evidence.Commitment.CanonicalHash, CanonicalGeneration: evidence.Commitment.CanonicalGeneration,
				EncodedProbeBytes: evidence.Commitment.EncodedProbeBytes, WriteTransactionHash: evidence.Commitment.WriteTransactionHash,
				WriteFinalizedHead: evidence.Commitment.WriteFinalizedHead, WriteCommitmentBlock: evidence.Commitment.WriteCommitmentBlock,
			}
			snapshot.EvidenceHash = ""
			snapshot.EvidenceHash, err = canonicalHashHex(&snapshot)
			if err != nil {
				t.Fatal(err)
			}
		}
		entry := JournalEntry{Sequence: uint64(index + 2), DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: actionId, IntentHash: action.IntentHash, Stage: StageVerified}
		observed := map[string]any{"kind": action.Kind, "target": action.Target, "probe": snapshot.ProbeAddress, "evidence_hash": snapshot.EvidenceHash, "complete": false, "canonical_chain_evidence": true}
		record := testFleetSupersessionPostcondition(cfg, action, entry, 100, observed)
		entry.PostconditionPath, entry.PostconditionHash, err = owner.persistActionPostcondition(record)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
		records = append(records, record)
	}
	battery := actionByID(t, source, "precompile.read-battery")
	entries = append(entries, JournalEntry{Sequence: 4, DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: battery.ID, IntentHash: battery.IntentHash, Stage: StageFailed, Error: "synthetic mapping precompile mismatch", EntryHash: common.Hash{9}.Hex()})
	if err := configurePrecompileProbeNonce(payloads, source.CoordinatorUpgrade.DeployerNonce+1); err != nil {
		t.Fatal(err)
	}
	successor := &PrecompileProbeSuccessor{
		Schema: "urnetwork-precompile-probe-successor-v1", SourcePlanHash: source.PlanHash, RetiredProbe: baseline.ReplacementPrecompileProbe, RetiredRuntimeHash: baseline.ReplacementPrecompileProbeHash,
		Probe: payloads.PrecompileProbeAddress.Hex(), DeployerNonce: payloads.PrecompileProbeNonce, RuntimeHash: crypto.Keccak256Hash(payloads.ExpectedRuntime[payloads.PrecompileProbeAddress]).Hex(), CreationHash: crypto.Keccak256Hash(payloads.PrecompileProbe).Hex(),
		FinalizedHead: ChainHead{Number: 200, Hash: common.Hash{10}.Hex()}, JournalSequence: 4, JournalHash: entries[3].EntryHash, Write: entries[1], Restore: entries[2], Evidence: evidence,
	}
	plan := *source
	plan.PlanHash = common.Hash{2}.Hex()
	plan.PriorPlanHashes = []string{source.PlanHash}
	plan.Actions = slices.Clone(source.Actions)
	if err := rebindPrecompileProbeSuccessor(&plan, source, payloads, successor); err != nil {
		t.Fatal(err)
	}
	return &precompileProbeSuccessorFixture{cfg: cfg, source: source, plan: &plan, payloads: payloads, entries: entries, stateDir: stateDir, writeRecord: records[0], restoreRecord: records[1]}
}

// Produces independent descriptor and action maps for negative controls.
func clonePrecompileProbeSuccessorPlan(t *testing.T, plan *SetupPlan) *SetupPlan {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var copy SetupPlan
	if err := json.Unmarshal(raw, &copy); err != nil {
		t.Fatal(err)
	}
	return &copy
}

// Rebinding changes each unstarted probe phase while retaining exact native work.
func TestPrecompileProbeSuccessorRebindsOnlyUnstartedPhases(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	for _, original := range fixture.source.Actions {
		current := actionByID(t, fixture.plan, original.ID)
		if precompileNativeAction(original.ID) || !strings.HasPrefix(original.ID, "precompile.") {
			if !reflect.DeepEqual(original, current) {
				t.Fatalf("completed or unrelated action %s changed", original.ID)
			}
		} else if current.IntentHash == original.IntentHash || current.Parameters[precompileProbeAddressParameter] != fixture.plan.PrecompileProbeSuccessor.Probe {
			t.Fatalf("unstarted phase %s retained its retired probe", original.ID)
		}
	}
	if fixture.plan.MaximumSpend != fixture.source.MaximumSpend || fixture.plan.Limits != fixture.source.Limits || !reflect.DeepEqual(fixture.plan.Deployment, fixture.source.Deployment) {
		t.Fatal("probe replacement changed spending or core deployment")
	}
}

// Original success plus a failed read is admissible; the unused-probe guard
// remains stricter and cannot be used to discard this completed native work.
func TestPrecompileProbeSuccessorRequiresFailedUnfundedHistory(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	if _, _, err := failedPrecompileProbeNativeEntries(fixture.source, fixture.entries); err != nil {
		t.Fatalf("completed native drill plus failed battery was refused: %v", err)
	}
	if err := validateUnusedPrecompileProbeGeneration(fixture.stateDir, fixture.source, fixture.entries); err == nil {
		t.Fatal("ordinary unused-probe guard accepted completed native work")
	}
	for _, actionId := range []string{"precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.dividend", "precompile.transfer-out"} {
		action := actionByID(t, fixture.source, actionId)
		entries := append(slices.Clone(fixture.entries), JournalEntry{Sequence: 5, PlanHash: fixture.source.PlanHash, ActionID: actionId, IntentHash: action.IntentHash, Stage: StageIntent})
		if _, _, err := failedPrecompileProbeNativeEntries(fixture.source, entries); err == nil {
			t.Fatalf("attempted value phase %s was abandoned", actionId)
		}
	}
	for _, change := range []string{"successful battery", "broadcast battery", "missing restore", "foreign native intent", "missing failure"} {
		entries := slices.Clone(fixture.entries)
		switch change {
		case "successful battery":
			entries[3].Stage = StageVerified
		case "broadcast battery":
			entries[3].TransactionHash = common.Hash{11}.Hex()
		case "missing restore":
			entries[2].Stage = StageFailed
		case "foreign native intent":
			entries[1].IntentHash = common.Hash{11}.Hex()
		case "missing failure":
			entries[3].Stage = StageIntent
		}
		if _, _, err := failedPrecompileProbeNativeEntries(fixture.source, entries); err == nil {
			t.Fatalf("changed %s was admitted", change)
		}
	}
}

// Descriptor changes cannot manufacture a different native source or CREATE.
func TestPrecompileProbeSuccessorRejectsDescriptorDrift(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	for _, change := range []string{"source", "repair", "nonce", "address", "retired probe", "retired hash", "head", "journal", "write", "restore", "funding", "battery", "native proof", "identity"} {
		plan := clonePrecompileProbeSuccessorPlan(t, fixture.plan)
		successor := plan.PrecompileProbeSuccessor
		switch change {
		case "source":
			successor.SourcePlanHash = common.Hash{12}.Hex()
		case "repair":
			plan.CoordinatorRepairCarry = nil
		case "nonce":
			successor.DeployerNonce++
		case "address":
			successor.Probe = common.Address{12}.Hex()
		case "retired probe":
			successor.RetiredProbe = common.Address{12}.Hex()
		case "retired hash":
			successor.RetiredRuntimeHash = common.Hash{12}.Hex()
		case "head":
			successor.FinalizedHead.Number = 0
		case "journal":
			successor.JournalHash = "changed"
		case "write":
			successor.Write.Sequence = successor.Restore.Sequence
		case "restore":
			successor.Restore.Stage = StageFailed
		case "funding":
			successor.Evidence.Seed.TAORao = 1
		case "battery":
			successor.Evidence.Battery.FinalizedHead.Number = 1
		case "native proof":
			successor.Evidence.Commitment.RestoreTransactionHash = common.Hash{12}.Hex()
		case "identity":
			successor.Evidence.ProbeAddress = successor.Probe
		}
		if err := validatePrecompileProbeSuccessor(plan); err == nil {
			t.Fatalf("changed %s descriptor was admitted", change)
		}
	}
}

// Action intents may not redirect native writes or silently reuse old reads.
func TestPrecompileProbeSuccessorRejectsActionDrift(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	for _, actionId := range []string{"precompile.probe-deploy", "precompile.commitment-write", "precompile.commitment-restore", "precompile.read-battery", "precompile.seed", "precompile.move-forward", "precompile.move-back", "precompile.snapshot", "precompile.dividend", "precompile.transfer-out"} {
		plan := clonePrecompileProbeSuccessorPlan(t, fixture.plan)
		for index := range plan.Actions {
			if plan.Actions[index].ID == actionId {
				plan.Actions[index].Parameters[precompileProbeAddressParameter] = common.Address{13}.Hex()
				plan.Actions[index] = testFleetSupersessionAction(t, plan.Actions[index])
			}
		}
		if err := validatePrecompileProbeSuccessorActions(plan); err == nil {
			t.Fatalf("changed probe for %s was admitted", actionId)
		}
	}
}

// The completed native source survives replacement and later battery evidence.
func TestPrecompileProbeSuccessorPreservesNativeEvidenceIdentity(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	successor := fixture.plan.PrecompileProbeSuccessor
	original := successor.Evidence
	identity := original
	identity.ProbeAddress, identity.Commitment = successor.Probe, PrecompileCommitmentEvidence{}
	coldkey := ss58Mirror(common.HexToAddress(identity.ProbeAddress))
	identity.ProbeColdkey, identity.EvidenceHash = hexBytesValue(coldkey[:]), ""
	fresh, err := precompileProbeSuccessorEvidence(fixture.plan, &identity, &original)
	if err != nil || fresh.ProbeAddress != successor.Probe || fresh.Commitment != original.Commitment || !reflect.DeepEqual(fresh.CommitmentSource, precompileProbeCommitmentSource(successor)) || fresh.EvidenceHash != "" || fresh.Battery.Passed {
		t.Fatalf("replacement lost original native identity or reused old battery: %v", err)
	}
	fresh.Battery.Passed = true
	if reused, err := precompileProbeSuccessorEvidence(fixture.plan, &identity, fresh); err != nil || reused != fresh {
		t.Fatalf("replacement's later battery lost original native source: %v", err)
	}
	if !reflect.DeepEqual(original, successor.Evidence) || original.CommitmentSource != nil {
		t.Fatal("replacement rewrote the retired evidence")
	}
}

// Neither a forged old snapshot nor relabeled new evidence can enter execution.
func TestPrecompileProbeSuccessorRejectsEvidenceSubstitution(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	successor := fixture.plan.PrecompileProbeSuccessor
	identity := successor.Evidence
	identity.ProbeAddress = successor.Probe
	for _, change := range []string{"old write", "old owner", "new write", "new source", "new probe"} {
		evidence := successor.Evidence
		if strings.HasPrefix(change, "new") {
			evidence.ProbeAddress = successor.Probe
			evidence.CommitmentSource = precompileProbeCommitmentSource(successor)
		}
		switch change {
		case "old write", "new write":
			evidence.Commitment.WriteTransactionHash = common.Hash{14}.Hex()
		case "old owner":
			evidence.Owner = common.Address{14}.Hex()
		case "new source":
			evidence.CommitmentSource.Probe = successor.Probe
		case "new probe":
			evidence.ProbeAddress = common.Address{14}.Hex()
		}
		if _, err := precompileProbeSuccessorEvidence(fixture.plan, &identity, &evidence); err == nil {
			t.Fatalf("changed %s was admitted", change)
		}
	}
}

// The actual executor refuses both old native writes before opening any owner.
func TestPrecompileProbeSuccessorNeverResendsNativeDrill(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	owner := &Executor{plan: fixture.plan}
	for _, actionId := range []string{"precompile.commitment-write", "precompile.commitment-restore"} {
		if err := owner.executePrecompileConformance(context.Background(), actionByID(t, fixture.plan, actionId)); err == nil || !strings.Contains(err.Error(), "cannot resend a completed native drill") {
			t.Fatalf("completed %s reached a transaction owner: %v", actionId, err)
		}
	}
}

// A later restore cannot change the exact original write phase's stored hash.
func TestPrecompileProbeSuccessorReplaysOriginalWritePhase(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	action := actionByID(t, fixture.source, "precompile.commitment-write")
	state, err := precompileNativeHistoricalObservation(action, &fixture.plan.PrecompileProbeSuccessor.Evidence, fixture.writeRecord)
	if err != nil || !reflect.DeepEqual(state, fixture.writeRecord.Observed) {
		t.Fatalf("original write receipt was lost after restore: %v", err)
	}
	changed := fixture.plan.PrecompileProbeSuccessor.Evidence
	changed.Commitment.WriteCommitmentBlock++
	if _, err := precompileNativeHistoricalObservation(action, &changed, fixture.writeRecord); err == nil {
		t.Fatal("changed original native write was accepted")
	}
}

// An authenticated original probe hash is separate from the new artifact;
// retained core checks still reject even one executable-byte change.
func TestPrecompileProbeSuccessorKeepsCoreExecutableChecks(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	baseline := fixture.plan.CoordinatorUpgradeBaseline
	payloads := coordinatorRepairBaselinePayloads(fixture.payloads, fixture.plan.CoordinatorRepairCarry)
	if err := validateCoordinatorUpgradePayloadBaseline(baseline, fixture.plan.Deployment, payloads); err == nil {
		t.Fatal("ordinary baseline accepted a changed probe")
	}
	if err := validateCoordinatorUpgradePayloadBaselineWithProbe(baseline, fixture.plan.Deployment, payloads, fixture.plan.PrecompileProbeSuccessor); err != nil {
		t.Fatalf("explicit probe successor could not retain exact core: %v", err)
	}
	for _, address := range []common.Address{payloads.Manifest.ReserveSink, payloads.Manifest.SettlementVault, payloads.Manifest.CoordinatorProxy} {
		original := payloads.ExpectedRuntime[address]
		changed := slices.Clone(original)
		changed[0] ^= 1
		payloads.ExpectedRuntime[address] = changed
		if err := validateCoordinatorUpgradePayloadBaselineWithProbe(baseline, fixture.plan.Deployment, payloads, fixture.plan.PrecompileProbeSuccessor); err == nil {
			t.Fatalf("probe successor changed retained core %s", address)
		}
		payloads.ExpectedRuntime[address] = original
	}
}

// Only a finalized exact next CREATE can explain the successor nonce.
func TestPrecompileProbeSuccessorPinsSingleCreateBoundary(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	nonce := fixture.plan.PrecompileProbeSuccessor.DeployerNonce
	if ok, err := precompileProbeSuccessorNonce(fixture.plan, fixture.entries, nonce); err != nil || !ok {
		t.Fatalf("empty approved boundary was refused: %v", err)
	}
	action := actionByID(t, fixture.plan, "precompile.probe-deploy")
	final := JournalEntry{Sequence: 5, DeploymentID: fixture.plan.DeploymentID, PlanHash: fixture.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: common.Hash{71}.Hex(), BlockNumber: 210, BlockHash: common.Hash{72}.Hex()}
	for _, change := range []string{"exact", "no receipt", "foreign plan", "foreign intent", "not finalized", "before admission", "extra nonce"} {
		entries := slices.Clone(fixture.entries)
		entry, observedNonce := final, nonce+1
		switch change {
		case "foreign plan":
			entry.PlanHash = common.Hash{15}.Hex()
		case "foreign intent":
			entry.IntentHash = common.Hash{15}.Hex()
		case "not finalized":
			entry.Stage = StageBroadcast
		case "before admission":
			entry.Sequence = 3
		case "extra nonce":
			observedNonce++
		}
		if change != "no receipt" {
			entries = append(entries, entry)
		}
		ok, err := precompileProbeSuccessorNonce(fixture.plan, entries, observedNonce)
		if change == "exact" && (err != nil || !ok) || change != "exact" && (err == nil || ok) {
			t.Fatalf("%s nonce boundary accepted=%t error=%v", change, ok, err)
		}
	}
}

// The filesystem receipt reader verifies both original observer maps and their
// hashes; this runs after the production archive reader authenticates the plan.
func TestPrecompileProbeSuccessorAuthenticatesOriginalReceiptFiles(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	if err := validatePrecompileProbeSuccessorSource(fixture.cfg, fixture.stateDir, fixture.plan, fixture.source, fixture.entries); err != nil {
		t.Fatalf("exact original receipt files were refused: %v", err)
	}
	for _, entry := range []JournalEntry{fixture.plan.PrecompileProbeSuccessor.Write, fixture.plan.PrecompileProbeSuccessor.Restore} {
		path := filepath.Join(fixture.stateDir, filepath.FromSlash(entry.PostconditionPath))
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := validatePrecompileProbeSuccessorSource(fixture.cfg, fixture.stateDir, fixture.plan, fixture.source, fixture.entries); err == nil {
			t.Fatalf("changed original %s receipt was admitted", entry.ActionID)
		}
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	changedEntries := slices.Clone(fixture.entries)
	changedEntries[len(changedEntries)-1].EntryHash = common.Hash{16}.Hex()
	if err := validatePrecompileProbeSuccessorSource(fixture.cfg, fixture.stateDir, fixture.plan, fixture.source, changedEntries); err == nil {
		t.Fatal("changed source journal prefix was admitted")
	}
}

// The old CREATE ceiling is charged once while the replacement keeps its own
// nonzero ceiling inside the original aggregate campaign allowance.
func TestPrecompileProbeSuccessorRetiresCreateGasOnce(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	retired, err := addRetiredVerifiedEVMGas(fixture.source, fixture.plan, fixture.entries, Spend{})
	if err != nil || retired.EVMGasWei != "7" {
		t.Fatalf("old CREATE ceiling was not retained exactly once: %s %v", retired.EVMGasWei, err)
	}
	if err := applySupersededSpend(fixture.plan, retired); err != nil {
		t.Fatal(err)
	}
	if err := trimLiveCampaignEVMReserveToLimit(fixture.plan); err != nil {
		t.Fatal(err)
	}
	if actionByID(t, fixture.plan, "precompile.probe-deploy").Spend.EVMGasWei != "7" || actionByID(t, fixture.plan, "campaign.evm-gas-reserve").Spend.EVMGasWei != "26" || fixture.plan.MaximumSpend.EVMGasWei != "33" || fixture.plan.SupersededSpend.EVMGasWei != "7" || fixture.plan.Limits != fixture.source.Limits {
		t.Fatal("new CREATE or retired ceiling escaped the original campaign allowance")
	}
	next := clonePrecompileProbeSuccessorPlan(t, fixture.plan)
	next.PlanHash = common.Hash{30}.Hex()
	next.PriorPlanHashes = append(next.PriorPlanHashes, fixture.plan.PlanHash)
	retiredAgain, err := addRetiredVerifiedEVMGas(fixture.plan, next, fixture.entries, retired)
	if err != nil || retiredAgain != retired {
		t.Fatalf("ordinary rerender charged the original CREATE again: %+v %v", retiredAgain, err)
	}
	if total, err := addDecimalUint(next.MaximumSpend.EVMGasWei, retiredAgain.EVMGasWei); err != nil || total != fixture.source.MaximumSpend.EVMGasWei {
		t.Fatalf("active plus retired ceilings changed across rerender: %s %v", total, err)
	}
}

// Adds a complete approved graph and real deterministic owner signatures so
// the production archive reader, rather than a stub, admits the source plan.
func newArchivedPrecompileProbeSuccessorFixture(t *testing.T) *precompileProbeSuccessorFixture {
	t.Helper()
	fixture := newPrecompileProbeSuccessorFixture(t)
	roles, err := BuildRoleSecrets(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := buildDeploymentPayloads(fixture.cfg, roles, fixture.source.Deployment.InitialNonce)
	if err != nil {
		t.Fatal(err)
	}
	oldUpgrade := fixture.source.CoordinatorRepairCarry.Request.Request.OldUpgrade
	if err := configureCoordinatorUpgradeNonce(payloads, oldUpgrade.DeployerNonce); err != nil {
		t.Fatal(err)
	}
	if err := configurePrecompileProbeNonce(payloads, fixture.source.CoordinatorUpgradeBaseline.ReplacementPrecompileProbeNonce); err != nil {
		t.Fatal(err)
	}
	payloads.ExpectedRuntime[payloads.PrecompileProbeAddress] = []byte{0x60, 0x77}
	facts := *testSetupFacts()
	facts.DeployerNonce = fixture.source.Deployment.InitialNonce
	source, err := buildPlan(fixture.cfg, &facts, fixture.source.Roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := rebindPlanDeployment(source, fixture.source.Deployment); err != nil {
		t.Fatal(err)
	}
	source.CoordinatorUpgradeBaseline = fixture.source.CoordinatorUpgradeBaseline
	if err := rebindPlanCoordinatorUpgrade(source, payloads); err != nil {
		t.Fatal(err)
	}
	source.PriorPlanHashes = []string{common.Hash{40}.Hex()}
	source.PlanHash, err = source.hash()
	if err != nil {
		t.Fatal(err)
	}
	persistFleetCommitmentRecoveryTestPlan(t, fixture.stateDir, source)
	beforeRepair := source.PlanHash
	request := coordinatorRepairRequest{
		Schema: "urnetwork-provisional-coordinator-repair-v1", Provisional: true, PlanHash: beforeRepair,
		ConfigHash: source.ConfigHash, DeploymentID: source.DeploymentID, ArtifactSHA256: strings.Repeat("41", 32), BudgetSHA256: strings.Repeat("42", 32), IdentityHash: common.Hash{43}.Hex(),
		Proxy: source.Deployment.CoordinatorProxy, Vault: source.Deployment.SettlementVault, Reserve: source.Deployment.ReserveSink,
		Owner: common.HexToAddress(source.Roles.Owner), Deployer: common.HexToAddress(source.Roles.Deployer), OldUpgrade: oldUpgrade, Upgrade: fixture.source.CoordinatorUpgrade,
		Deploy:   testFleetSupersessionAction(t, Action{ID: "repair.coordinator-rounding.deploy", Kind: "evm-transaction", Target: fixture.source.CoordinatorUpgrade.Implementation.Hex()}),
		Activate: testFleetSupersessionAction(t, Action{ID: "repair.coordinator-rounding.activate", Kind: "evm-transaction", Target: source.Deployment.CoordinatorProxy.Hex()}),
	}
	ownerRole, err := roles.EVMKey("testnet-owner")
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, err := crypto.HexToECDSA(ownerRole.PrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	carry := &CoordinatorRepairCarry{Schema: "urnetwork-coordinator-repair-carry-v1", Request: signedCoordinatorRepairRequest{Request: request}}
	carry.Request.Hash, carry.Request.Signature, err = coordinatorRepairSignature(request, ownerKey)
	if err != nil {
		t.Fatal(err)
	}
	finals := []JournalEntry{}
	for index, action := range []Action{request.Deploy, request.Activate} {
		finals = append(finals, JournalEntry{Sequence: uint64(index + 1), DeploymentID: source.DeploymentID, PlanHash: beforeRepair, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFinalized, TransactionHash: common.Hash{byte(44 + index)}.Hex(), BlockNumber: uint64(20 + index), BlockHash: common.Hash{byte(46 + index)}.Hex()})
	}
	carry.Result.Result = coordinatorRepairResult{Schema: "urnetwork-provisional-coordinator-repair-result-v1", Provisional: true, RequestHash: carry.Request.Hash, IdentityHash: request.IdentityHash, Deploy: finals[0], Activate: finals[1], ObservedHead: ChainHead{Number: 22, Hash: common.Hash{48}.Hex()}}
	carry.Result.Hash, carry.Result.Signature, err = coordinatorRepairSignature(carry.Result.Result, ownerKey)
	if err != nil {
		t.Fatal(err)
	}
	source.ValidatorEvidenceCarry = &ValidatorEvidenceCarry{
		Schema: "urnetwork-validator-evidence-carry-v1", SourcePlanHash: beforeRepair, SourceReleaseLockHash: source.ReleaseLockHash,
		Creation: ValidatorEvidenceCarryReceipt{PlanHash: beforeRepair, IntentHash: actionByID(t, source, validatorEvidenceDeployActionID).IntentHash, TransactionHash: common.Hash{60}.Hex(), BlockNumber: 10, BlockHash: common.Hash{61}.Hex(), PostconditionHash: common.Hash{62}.Hex()},
		Anchor:   ValidatorEvidenceCarryReceipt{PlanHash: beforeRepair, IntentHash: actionByID(t, source, validatorEvidenceAnchorActionID).IntentHash, TransactionHash: common.Hash{63}.Hex(), BlockNumber: 11, BlockHash: common.Hash{64}.Hex(), PostconditionHash: common.Hash{65}.Hex()},
	}
	source.CoordinatorRepairCarry, source.CoordinatorUpgrade = carry, request.Upgrade
	source.PriorPlanHashes = append(source.PriorPlanHashes, beforeRepair)
	source.PlanHash, err = source.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(source); err != nil {
		t.Fatalf("complete archived source prerequisite: %v", err)
	}
	persistFleetCommitmentRecoveryTestPlan(t, fixture.stateDir, source)
	// Resume consumes persisted approvals, including canonical DecimalUint zeros.
	source, err = readValidatorEvidenceHistoricalPlan(fixture.stateDir, source.PlanHash)
	if err != nil {
		t.Fatal(err)
	}
	owner := &Executor{cfg: fixture.cfg, stateDir: fixture.stateDir, plan: source}
	entries := slices.Clone(fixture.entries)
	for index := range entries {
		action := actionByID(t, source, entries[index].ActionID)
		entries[index].Sequence += 2
		entries[index].PlanHash, entries[index].IntentHash = source.PlanHash, action.IntentHash
		if index == 1 || index == 2 {
			record := []*ActionPostcondition{fixture.writeRecord, fixture.restoreRecord}[index-1]
			record.PlanHash, record.IntentHash = source.PlanHash, action.IntentHash
			entries[index].PostconditionPath, entries[index].PostconditionHash, err = owner.persistActionPostcondition(record)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	successor := *fixture.plan.PrecompileProbeSuccessor
	successor.SourcePlanHash, successor.Write, successor.Restore = source.PlanHash, entries[1], entries[2]
	successor.JournalSequence = entries[len(entries)-1].Sequence
	plan := *source
	plan.validatorEvidenceHistorical = false
	plan.PlanHash = common.Hash{49}.Hex()
	plan.PriorPlanHashes = append(slices.Clone(source.PriorPlanHashes), source.PlanHash)
	plan.Actions = slices.Clone(source.Actions)
	if err := rebindPrecompileProbeSuccessor(&plan, source, fixture.payloads, &successor); err != nil {
		t.Fatal(err)
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	fixture.source, fixture.plan, fixture.entries = source, &plan, append(finals, entries...)
	return fixture
}

// Canonical zero amounts retain signed identity across the actual archive reader.
func TestPrecompileProbeSuccessorRetainsCanonicalArchivedAmounts(t *testing.T) {
	fixture := newArchivedPrecompileProbeSuccessorFixture(t)
	for _, action := range []Action{
		fixture.source.CoordinatorRepairCarry.Request.Request.Deploy,
		fixture.source.CoordinatorRepairCarry.Request.Request.Activate,
		actionByID(t, fixture.plan, "precompile.commitment-write"),
		actionByID(t, fixture.plan, "precompile.commitment-restore"),
	} {
		if action.Spend.EVMGasWei != "0" {
			t.Fatalf("archived action %s did not retain canonical zero wei", action.ID)
		}
		intent, err := actionIntentHash(action)
		if err != nil || intent != action.IntentHash {
			t.Fatalf("archived zero amount changed %s intent: %v", action.ID, err)
		}
	}
	for _, changedPart := range []string{"signed repair", "native action"} {
		changed := clonePrecompileProbeSuccessorPlan(t, fixture.plan)
		if changedPart == "signed repair" {
			changed.CoordinatorRepairCarry.Request.Request.Deploy.Spend.EVMGasWei = "1"
		} else {
			for index := range changed.Actions {
				if changed.Actions[index].ID == "precompile.commitment-write" {
					changed.Actions[index].Spend.EVMGasWei = "1"
				}
			}
		}
		if _, err := readPrecompileProbeSuccessorSource(fixture.cfg, fixture.stateDir, changed, fixture.entries); err == nil {
			t.Fatalf("changed %s amount escaped exact source identity", changedPart)
		}
	}
}

// The actual constructor loads the complete archived approval and persisted
// receipt, then keeps the native source and original probe independent of today.
func TestPrecompileProbeSuccessorConstructsOriginalNativeReplay(t *testing.T) {
	fixture := newArchivedPrecompileProbeSuccessorFixture(t)
	owner := &Executor{cfg: fixture.cfg, stateDir: fixture.stateDir, plan: fixture.plan, payloads: fixture.payloads, journal: &Journal{entries: fixture.entries}}
	action := actionByID(t, fixture.plan, "precompile.commitment-write")
	source, handled, err := owner.precompileProbeNativeSource(action, fixture.plan.PrecompileProbeSuccessor.Write, fixture.writeRecord)
	if err != nil || !handled || source == nil || source.plan.PlanHash != fixture.source.PlanHash || source.payloads.PrecompileProbeAddress.Hex() != fixture.plan.PrecompileProbeSuccessor.RetiredProbe || !reflect.DeepEqual(source.precompileHistoryEvidence, &fixture.plan.PrecompileProbeSuccessor.Evidence) {
		t.Fatalf("constructor lost original native receipt/probe identity: handled=%t error=%v", handled, err)
	}
	if owner.precompileHistoryEvidence != nil || owner.payloads.PrecompileProbeAddress != common.HexToAddress(fixture.plan.PrecompileProbeSuccessor.Probe) {
		t.Fatal("historical constructor changed current execution identity")
	}
	changed := fixture.plan.PrecompileProbeSuccessor.Write
	changed.PostconditionHash = common.Hash{50}.Hex()
	if _, handled, err := owner.precompileProbeNativeSource(action, changed, fixture.writeRecord); !handled || err == nil {
		t.Fatal("historical constructor accepted a substituted original receipt")
	}
	restore := actionByID(t, fixture.plan, "precompile.commitment-restore")
	if _, handled, err := owner.precompileProbeNativeSource(restore, fixture.plan.PrecompileProbeSuccessor.Restore, fixture.restoreRecord); !handled || err == nil || !strings.Contains(err.Error(), "requires authenticated completed renewal") {
		t.Fatalf("restore constructor bypassed completed renewal: handled=%t error=%v", handled, err)
	}
}

func TestPrecompileProbeSuccessorConstructsOriginalBatteryReplay(t *testing.T) {
	fixture := newArchivedPrecompileProbeSuccessorFixture(t)
	owner := &Executor{cfg: fixture.cfg, stateDir: fixture.stateDir, plan: fixture.plan, payloads: fixture.payloads, journal: &Journal{entries: fixture.entries}}
	action := actionByID(t, fixture.plan, "precompile.read-battery")
	sourceAction := actionByID(t, fixture.source, action.ID)
	verified := JournalEntry{PlanHash: fixture.source.PlanHash, ActionID: action.ID, IntentHash: sourceAction.IntentHash}
	record := *fixture.writeRecord
	record.PlanHash, record.ActionID, record.IntentHash = verified.PlanHash, verified.ActionID, verified.IntentHash
	source, handled, err := owner.precompileProbeHistoricalReadSource(action, verified, &record)
	if err != nil || !handled || source == nil || source.plan.PlanHash != fixture.source.PlanHash || source.payloads.PrecompileProbeAddress.Hex() != fixture.plan.PrecompileProbeSuccessor.RetiredProbe || !reflect.DeepEqual(source.precompileHistoryEvidence, &fixture.plan.PrecompileProbeSuccessor.Evidence) {
		t.Fatalf("battery replay lost original source: handled=%t error=%v", handled, err)
	}
	replayedAction, err := precompileBatteryHistoricalSourceAction(source, action)
	if err != nil || replayedAction.IntentHash != sourceAction.IntentHash || !reflect.DeepEqual(replayedAction.Parameters, sourceAction.Parameters) {
		t.Fatalf("battery replay used successor action instead of retained source: action=%+v error=%v", replayedAction, err)
	}
	if reflect.DeepEqual(replayedAction.Parameters, action.Parameters) {
		t.Fatal("battery replay test requires successor probe parameters to differ")
	}
	changed := record
	changed.IntentHash = common.Hash{50}.Hex()
	if _, handled, err := owner.precompileProbeHistoricalReadSource(action, verified, &changed); !handled || err == nil {
		t.Fatal("battery replay accepted a substituted source record")
	}
}

// The public generation validator authenticates the signed corrective upgrade
// while retaining the earlier v4 probe baseline and explicit successor source.
func TestPrecompileProbeSuccessorPublishesSignedBaselineIdentity(t *testing.T) {
	fixture := newArchivedPrecompileProbeSuccessorFixture(t)
	identities, err := json.Marshal(finalPublicIdentities{Schema: "urnetwork-sim-public-identities-v1", DeploymentID: fixture.plan.DeploymentID, EVM: map[string]string{"deployer": fixture.plan.Roles.Deployer, "testnet-owner": fixture.plan.Roles.Owner}})
	if err != nil {
		t.Fatal(err)
	}
	public := &PublicDeploymentManifest{PlanHash: fixture.plan.PlanHash, DeploymentID: fixture.plan.DeploymentID, ConfigHash: fixture.plan.ConfigHash, PolicyHash: fixture.plan.PolicyHash, ChainID: fixture.plan.ChainID, GenesisHash: fixture.plan.GenesisHash, Netuid: fixture.plan.Netuid, Contracts: &fixture.plan.Deployment, Identities: identities, CoordinatorUpgrade: fixture.plan.CoordinatorUpgrade, CoordinatorUpgradeBaseline: &fixture.plan.CoordinatorUpgradeBaseline, CoordinatorRepairCarry: fixture.plan.CoordinatorRepairCarry, PrecompileProbeSuccessor: fixture.plan.PrecompileProbeSuccessor}
	if err := validatePublicPrecompileProbeGeneration(public); err != nil {
		t.Fatalf("published successor lost its signed original baseline: %v", err)
	}
	public.CoordinatorRepairCarry = nil
	if err := validatePublicPrecompileProbeGeneration(public); err == nil {
		t.Fatal("public successor silently treated the corrected upgrade as the original adjacent CREATE")
	}
	copy := *fixture.plan.CoordinatorRepairCarry
	copy.Request.Signature = "changed"
	public.CoordinatorRepairCarry = &copy
	if err := validatePublicPrecompileProbeGeneration(public); err == nil {
		t.Fatal("public successor accepted a changed original owner signature")
	}
}

// Status and scenario readers select the same new probe as execution and keep
// the v4 baseline available as the historical native proof's separate identity.
func TestPrecompileProbeSuccessorRoutesCurrentContractView(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	view := &ContractView{Deployment: &fixture.plan.Deployment, CoordinatorUpgrade: fixture.plan.CoordinatorUpgrade, CoordinatorUpgradeBaseline: &fixture.plan.CoordinatorUpgradeBaseline, PrecompileProbeSuccessor: fixture.plan.PrecompileProbeSuccessor}
	public := &PublicDeploymentManifest{Contracts: view.Deployment, CoordinatorUpgrade: view.CoordinatorUpgrade, CoordinatorUpgradeBaseline: view.CoordinatorUpgradeBaseline, PrecompileProbeSuccessor: view.PrecompileProbeSuccessor, CoordinatorRepairCarry: fixture.plan.CoordinatorRepairCarry}
	wire, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	var loaded PublicDeploymentManifest
	if err := json.Unmarshal(wire, &loaded); err != nil {
		t.Fatal(err)
	}
	view.PrecompileProbeSuccessor = loaded.PrecompileProbeSuccessor
	want := common.HexToAddress(fixture.plan.PrecompileProbeSuccessor.Probe)
	if approvedPrecompileProbe(fixture.plan) != want || contractViewPrecompileProbe(view) != want || loaded.PrecompileProbeSuccessor == nil || !reflect.DeepEqual(loaded.PrecompileProbeSuccessor, fixture.plan.PrecompileProbeSuccessor) {
		t.Fatal("public/status probe generation differs from executor or lost original proof")
	}
	view.PrecompileProbeSuccessor = nil
	if contractViewPrecompileProbe(view) != common.HexToAddress(fixture.plan.PrecompileProbeSuccessor.RetiredProbe) {
		t.Fatal("ordinary v4 observation changed its historical probe identity")
	}
}

// The pure revision reducer consumes the observer's actual post-CREATE nonce,
// while a detached or stale observation cannot adopt that continuation.
func TestPrecompileProbeSuccessorRerendersObservedCreateNonce(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	actualNonce := fixture.plan.PrecompileProbeSuccessor.DeployerNonce + 1
	observed := &coordinatorRepairCarryObservation{reference: *fixture.plan.CoordinatorRepairCarry, deployerNonce: actualNonce}
	fixture.plan.coordinatorRepairObserved = observed
	migration := &coordinatorUpgradeMigration{Deployment: fixture.plan.Deployment, Baseline: fixture.plan.CoordinatorUpgradeBaseline, Upgrade: fixture.plan.CoordinatorUpgrade, Repair: observed, ProbeSuccessor: fixture.plan.PrecompileProbeSuccessor}
	if ok, err := coordinatorUpgradeMigrationNonceMatches(fixture.plan, migration, fixture.payloads, actualNonce, fixture.entries); err != nil || !ok {
		t.Fatalf("actual authenticated successor nonce was dropped by reducer: %v", err)
	}
	for _, change := range []string{"old nonce", "extra nonce", "detached observation"} {
		nonce := actualNonce
		prior := *fixture.plan
		switch change {
		case "old nonce":
			nonce--
		case "extra nonce":
			nonce++
		case "detached observation":
			copy := *observed
			prior.coordinatorRepairObserved = &copy
		}
		if ok, _ := coordinatorUpgradeMigrationNonceMatches(&prior, migration, fixture.payloads, nonce, fixture.entries); ok {
			t.Fatalf("%s was admitted as a completed observation", change)
		}
	}
	before, err := canonicalHashHex(fixture.plan.PrecompileProbeSuccessor)
	if err != nil {
		t.Fatal(err)
	}
	next := clonePrecompileProbeSuccessorPlan(t, fixture.plan)
	next.PlanHash = common.Hash{31}.Hex()
	next.PriorPlanHashes = append(next.PriorPlanHashes, fixture.plan.PlanHash)
	if err := rebindPrecompileProbeSuccessor(next, fixture.plan, fixture.payloads, fixture.plan.PrecompileProbeSuccessor); err != nil {
		t.Fatalf("ordinary rerender lost approved successor: %v", err)
	}
	after, err := canonicalHashHex(next.PrecompileProbeSuccessor)
	if err != nil || after != before {
		t.Fatal("ordinary rerender replaced original admission or native receipt source")
	}
}

func TestPrecompileProbeSuccessorRetainsHistoricalEvidenceAcrossConfigRevision(t *testing.T) {
	fixture := newPrecompileProbeSuccessorFixture(t)
	revised := *fixture.plan
	revised.ConfigHash = common.Hash{0xa1}.Hex()
	revised.PolicyHash = common.Hash{0xa2}.Hex()
	if err := validatePrecompileProbeSuccessor(&revised); err != nil {
		t.Fatalf("successor rejected its immutable source evidence after a configuration revision: %v", err)
	}
	changed := revised
	changed.PrecompileProbeSuccessor = new(PrecompileProbeSuccessor)
	*changed.PrecompileProbeSuccessor = *revised.PrecompileProbeSuccessor
	changed.PrecompileProbeSuccessor.Evidence.ConfigHash = revised.ConfigHash
	if err := validatePrecompileProbeSuccessor(&changed); err == nil {
		t.Fatal("successor accepted evidence relabeled with the revised configuration hash")
	}
}
