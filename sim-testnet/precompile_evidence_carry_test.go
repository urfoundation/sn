// Approved allowance revisions retain partial evidence and original battery
// receipts without granting a pass to unfinished value-conservation phases.
package main

import (
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Builds a real archived successor approval, then raises only gas allowances.
// The seed has saved its input but has never submitted a transaction.
type precompileEvidenceCarryFixture struct {
	owner          *Executor
	source         *SetupPlan
	evidence       *PrecompileConformanceEvidence
	battery        *PrecompileConformanceEvidence
	batteryEntry   JournalEntry
	batteryRecord  *ActionPostcondition
	evidenceBefore []byte
}

func newPrecompileEvidenceCarryFixture(t *testing.T) *precompileEvidenceCarryFixture {
	t.Helper()
	prior := newArchivedPrecompileProbeSuccessorFixture(t)
	source := prior.plan
	persistFleetCommitmentRecoveryTestPlan(t, prior.stateDir, source)
	plan := clonePrecompileProbeSuccessorPlan(t, source)
	plan.ConfigHash = common.Hash{111}.Hex()
	plan.PriorPlanHashes = append(slices.Clone(source.PriorPlanHashes), source.PlanHash)
	for index := range plan.Actions {
		action := &plan.Actions[index]
		if !strings.HasPrefix(action.ID, "precompile.") || action.Kind != "evm-transaction" || action.ID == "precompile.probe-deploy" {
			continue
		}
		gas, err := strconv.ParseUint(action.Parameters[evmMaximumGasUnitsParameter], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		action.Parameters[evmMaximumGasUnitsParameter] = strconv.FormatUint(gas+1, 10)
		amount, ok := new(big.Int).SetString(string(action.Spend.EVMGasWei), 10)
		if !ok {
			t.Fatal("fixture gas spend is absent")
		}
		action.Spend.EVMGasWei = DecimalUint(amount.Add(amount, big.NewInt(1)).String())
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			t.Fatal(err)
		}
	}
	var err error
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	cfg := *prior.cfg
	cfg.ConfigHash = plan.ConfigHash
	evidence := source.PrecompileProbeSuccessor.Evidence
	evidence.ProbeAddress = source.PrecompileProbeSuccessor.Probe
	coldkey := ss58Mirror(approvedPrecompileProbe(source))
	evidence.ProbeColdkey = hexBytesValue(coldkey[:])
	evidence.CommitmentSource = precompileProbeCommitmentSource(source.PrecompileProbeSuccessor)
	evidence.Battery = completePrecompileEvidence().Battery
	evidence.Battery.FinalizedHead = ChainHead{Number: 210, Hash: common.Hash{112}.Hex()}
	if err := writePrecompileEvidence(prior.stateDir, &evidence); err != nil {
		t.Fatal(err)
	}
	battery := evidence
	action := actionByID(t, source, "precompile.read-battery")
	entry := JournalEntry{Sequence: 100, DeploymentID: source.DeploymentID, PlanHash: source.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified}
	observed := map[string]any{"kind": action.Kind, "target": action.Target, "probe": battery.ProbeAddress, "evidence_hash": battery.EvidenceHash, "complete": false, "canonical_chain_evidence": true}
	record := testFleetSupersessionPostcondition(prior.cfg, action, entry, 210, observed)
	reader := &Executor{cfg: prior.cfg, stateDir: prior.stateDir, plan: source}
	entry.PostconditionPath, entry.PostconditionHash, err = reader.persistActionPostcondition(record)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Seed.TAORao = plan.LiveFacts.ProbeTAORao
	evidence.Seed.ValueWei = new(big.Int).Mul(new(big.Int).SetUint64(evidence.Seed.TAORao), big.NewInt(1_000_000_000)).String()
	if err := writePrecompileEvidence(prior.stateDir, &evidence); err != nil {
		t.Fatal(err)
	}
	evidenceBefore, err := os.ReadFile(precompileEvidencePath(prior.stateDir))
	if err != nil {
		t.Fatal(err)
	}
	entries := append(slices.Clone(prior.entries), entry)
	owner := &Executor{cfg: &cfg, stateDir: prior.stateDir, plan: plan, payloads: prior.payloads, journal: &Journal{entries: entries}}
	return &precompileEvidenceCarryFixture{owner: owner, source: source, evidence: &evidence, battery: &battery, batteryEntry: entry, batteryRecord: record, evidenceBefore: evidenceBefore}
}

// Config-only and gas-ceiling changes preserve partial inputs and actual
// completion remains controlled by all original conservation requirements.
func TestPrecompileEvidenceCarryKeepsIncompleteProgress(t *testing.T) {
	fixture := newPrecompileEvidenceCarryFixture(t)
	owner, evidence := fixture.owner, fixture.evidence
	probe := approvedPrecompileProbe(owner.plan)
	if err := validatePrecompileEvidenceIdentity(owner.cfg, probe, evidence); err == nil {
		t.Fatal("fixture did not reproduce the original config identity mismatch")
	}
	if err := owner.validatePrecompileEvidence(probe, evidence); err != nil {
		t.Fatalf("approved allowance revision lost partial conformance: %v", err)
	}
	observer := &liveScenarioProbe{cfg: owner.cfg, stateDir: owner.stateDir, precompilePlan: owner.plan, precompileJournal: owner.journal}
	if err := observer.validatePrecompileEvidence(probe, evidence); err != nil {
		t.Fatalf("scenario observation lost the same original approval: %v", err)
	}
	if observer.precompileSourcePlan == nil || observer.precompileSourcePlan.PlanHash != fixture.source.PlanHash || precompileEvidenceComplete(evidence) {
		t.Fatal("partial evidence was relabeled, completed, or not bound to its original source")
	}
	// A cached identity is not a completion cache or authority for edited roles.
	changed := *evidence
	changed.Complete = true
	if err := observer.validatePrecompileEvidence(probe, &changed); err != nil || precompileEvidenceComplete(&changed) {
		t.Fatalf("an incomplete drill became valid from its complete flag: %v", err)
	}
	changed.Owner = common.Address{115}.Hex()
	if err := observer.validatePrecompileEvidence(probe, &changed); err == nil {
		t.Fatal("cached source accepted a changed owner")
	}
	after, err := os.ReadFile(precompileEvidencePath(owner.stateDir))
	if err != nil || string(after) != string(fixture.evidenceBefore) {
		t.Fatalf("identity validation rewrote durable partial evidence: %v", err)
	}
	if evidence.ConfigHash != fixture.source.ConfigHash || evidence.Seed.TransactionHash != "" || evidence.Seed.TAORao == 0 {
		t.Fatal("the original config label or unsubmitted seed checkpoint changed")
	}
}

// Every mismatch outside the permitted config/gas envelope is rejected before
// historical evidence can be reused by an executor or a live observation.
func TestPrecompileEvidenceCarryRejectsForeignScope(t *testing.T) {
	fixture := newPrecompileEvidenceCarryFixture(t)
	for _, name := range []string{"probe", "coldkey", "policy", "deployment", "chain", "netuid", "owner", "sample", "sample uid", "absent", "move", "recovery", "native proof", "source config", "source lineage", "roles", "action target", "action fee", "action input", "action dependency", "action non-gas spend", "missing action", "new action"} {
		t.Run(name, func(t *testing.T) {
			plan := clonePrecompileProbeSuccessorPlan(t, fixture.owner.plan)
			cfg := *fixture.owner.cfg
			evidence := *fixture.evidence
			switch name {
			case "probe":
				evidence.ProbeAddress = common.Address{116}.Hex()
			case "coldkey":
				evidence.ProbeColdkey = common.Hash{116}.Hex()
			case "policy":
				evidence.PolicyHash = common.Hash{116}.Hex()
			case "deployment":
				evidence.DeploymentID += "-foreign"
			case "chain":
				evidence.ChainID++
			case "netuid":
				evidence.Netuid++
			case "owner":
				evidence.Owner = common.Address{116}.Hex()
			case "sample":
				evidence.SampleHotkey = common.Hash{116}.Hex()
			case "sample uid":
				evidence.SampleUID++
			case "absent":
				evidence.AbsentHotkey = common.Hash{116}.Hex()
			case "move":
				evidence.MoveHotkey = common.Hash{116}.Hex()
			case "recovery":
				evidence.RecoveryColdkey = common.Hash{116}.Hex()
			case "native proof":
				evidence.Commitment.WriteTransactionHash = common.Hash{116}.Hex()
			case "source config":
				evidence.ConfigHash = common.Hash{116}.Hex()
			case "source lineage":
				plan.PriorPlanHashes = nil
			case "roles":
				plan.Roles.Owner = common.Address{116}.Hex()
			case "missing action":
				plan.Actions = slices.DeleteFunc(plan.Actions, func(action Action) bool { return action.ID == "precompile.dividend" })
			case "new action":
				plan.Actions = append(plan.Actions, Action{ID: "precompile.foreign"})
			default:
				for index := range plan.Actions {
					action := &plan.Actions[index]
					if action.ID != "precompile.seed" {
						continue
					}
					switch name {
					case "action target":
						action.Target = "validator:99"
					case "action fee":
						action.Parameters["maximum_fee_per_gas_wei"] = "1"
					case "action input":
						action.Parameters["maximum_tao_rao"] = "1"
					case "action dependency":
						action.DependsOn = nil
					case "action non-gas spend":
						action.Spend.AlphaRao++
					}
					var err error
					action.IntentHash, err = actionIntentHash(*action)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			owner := &Executor{cfg: &cfg, stateDir: fixture.owner.stateDir, plan: plan, journal: fixture.owner.journal}
			if err := owner.validatePrecompileEvidence(approvedPrecompileProbe(plan), &evidence); err == nil {
				t.Fatalf("foreign %s was accepted", name)
			}
		})
	}
}

// Hashing a local evidence JSON does not make a forged historical approval
// valid. The production archive reader must reject changed source bytes.
func TestPrecompileEvidenceCarryAuthenticatesSourceArchive(t *testing.T) {
	fixture := newPrecompileEvidenceCarryFixture(t)
	source := clonePrecompileProbeSuccessorPlan(t, fixture.source)
	source.Roles.Owner = common.Address{117}.Hex()
	persistFleetCommitmentRecoveryTestPlan(t, fixture.owner.stateDir, source)
	if err := fixture.owner.validatePrecompileEvidence(approvedPrecompileProbe(fixture.owner.plan), fixture.evidence); err == nil {
		t.Fatal("a source plan edited without its hash was accepted")
	}
}

// Reproduces the live path: a battery finalized on the successor, followed by
// failed seed estimation and another approval. Replay must keep that probe.
func TestPrecompileBatterySuccessorReceiptRetainsCurrentProbe(t *testing.T) {
	fixture := newPrecompileEvidenceCarryFixture(t)
	owner := fixture.owner
	action := actionByID(t, owner.plan, "precompile.read-battery")
	recordPath := filepath.Join(owner.stateDir, filepath.FromSlash(fixture.batteryEntry.PostconditionPath))
	recordBefore, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	source, handled, err := owner.precompileProbeHistoricalReadSource(action, fixture.batteryEntry, fixture.batteryRecord)
	if err != nil || !handled || source == nil {
		t.Fatalf("successor battery receipt could not be replayed: handled=%t error=%v", handled, err)
	}
	if source.payloads.PrecompileProbeAddress != approvedPrecompileProbe(owner.plan) || !reflect.DeepEqual(source.precompileHistoryEvidence, fixture.battery) || source.cfg.ConfigHash != fixture.evidence.ConfigHash {
		t.Fatal("successor battery was routed to retired-probe evidence or lost its exact phase hash")
	}
	if source.precompileHistoryEvidence.Seed != (PrecompileValueStep{}) || owner.precompileHistoryEvidence != nil || fixture.evidence.Seed.TAORao == 0 || precompileEvidenceComplete(source.precompileHistoryEvidence) {
		t.Fatal("historical projection overwrote live progress or completed the missing drill")
	}
	recordAfter, err := os.ReadFile(recordPath)
	if err != nil || string(recordBefore) != string(recordAfter) {
		t.Fatalf("replay rewrote the authenticated original battery receipt: %v", err)
	}
	evidenceAfter, err := os.ReadFile(precompileEvidencePath(owner.stateDir))
	if err != nil || string(evidenceAfter) != string(fixture.evidenceBefore) {
		t.Fatalf("replay rewrote current partial evidence: %v", err)
	}
	changed := *fixture.batteryRecord
	changed.Observed = maps.Clone(changed.Observed)
	changed.Observed["evidence_hash"] = common.Hash{118}.Hex()
	if _, handled, err := owner.precompileProbeHistoricalReadSource(action, fixture.batteryEntry, &changed); !handled || err == nil {
		t.Fatal("altered original battery observation was accepted")
	}
}

// A later appended phase is removable, but an edited original observation is
// not. The battery phase digest is compared through the same production path.
func TestPrecompileBatteryHistoricalEvidenceRejectsChangedBattery(t *testing.T) {
	fixture := newPrecompileEvidenceCarryFixture(t)
	changed := *fixture.evidence
	changed.Battery.UIDCount++
	if _, err := precompileBatteryHistoricalEvidence(actionByID(t, fixture.source, "precompile.read-battery"), &changed, fixture.batteryRecord); err == nil {
		t.Fatal("changed battery contents were accepted after projecting the seed")
	}
}

// Original probe generations also retain config labels. With no successor
// descriptor, native roles are checked against the original derivation domain.
func TestPrecompileEvidenceCarryOriginalProbeChecksNativeRoles(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	persistFleetCommitmentRecoveryTestPlan(t, stateDir, source)
	plan := clonePrecompileProbeSuccessorPlan(t, source)
	plan.PriorPlanHashes = []string{source.PlanHash}
	plan.ConfigHash = common.Hash{120}.Hex()
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	currentCfg := *cfg
	currentCfg.ConfigHash = plan.ConfigHash
	probe := approvedPrecompileProbe(plan)
	coldkey := ss58Mirror(probe)
	absent := derive32(cfg, "precompile/absent-hotkey")
	evidence := &PrecompileConformanceEvidence{
		Schema: "urnetwork-precompile-conformance-v1", DeploymentID: plan.DeploymentID,
		ConfigHash: source.ConfigHash, PolicyHash: source.PolicyHash, ChainID: source.ChainID,
		GenesisHash: source.GenesisHash, Netuid: source.Netuid, Owner: roles.Deployer,
		ProbeAddress: probe.Hex(), ProbeColdkey: hexBytesValue(coldkey[:]), AbsentHotkey: hexBytesValue(absent[:]),
	}
	for label, field := range map[string]*string{validatorHotkeyLabel(1): &evidence.SampleHotkey, validatorHotkeyLabel(2): &evidence.MoveHotkey, fleetColdkeyLabel(1): &evidence.RecoveryColdkey} {
		keypair, err := crv4.KeypairFromSeed(derive32(cfg, "substrate/"+label))
		if err != nil {
			t.Fatal(err)
		}
		key := keypair.PublicKey()
		*field = hexBytesValue(key[:])
	}
	action := actionByID(t, source, "precompile.read-battery")
	owner := &Executor{cfg: &currentCfg, stateDir: stateDir, plan: plan, journal: &Journal{entries: []JournalEntry{{DeploymentID: plan.DeploymentID, PlanHash: source.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFailed}}}}
	if err := owner.validatePrecompileEvidence(probe, evidence); err != nil || precompileEvidenceComplete(evidence) {
		t.Fatalf("original partial probe could not retain its source configuration: %v", err)
	}
	evidence.MoveHotkey = common.Hash{121}.Hex()
	if err := owner.validatePrecompileEvidence(probe, evidence); err == nil {
		t.Fatal("original probe accepted a foreign native role")
	}
}
