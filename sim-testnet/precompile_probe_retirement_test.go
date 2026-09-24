// Synthetic deployed generations reproduce a changed locked artifact after a
// seed estimate failure, without using live chain identities or sending writes.
package main

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Includes real archived approvals and hashed receipt files. The first probe's
// synthetic old bytes deliberately differ from the current generated artifact.
type precompileProbeRetirementFixture struct {
	*precompileProbeSuccessorFixture
	evidence      *PrecompileConformanceEvidence
	batteryRecord *ActionPostcondition
	current       *DeploymentPayloads
}

// Installs exact CREATE/battery receipts and only an unbroadcast seed attempt.
func newPrecompileProbeRetirementFixture(t *testing.T) *precompileProbeRetirementFixture {
	t.Helper()
	fixture := newArchivedPrecompileProbeSuccessorFixture(t)
	current := *fixture.payloads
	current.ExpectedRuntime = make(map[common.Address][]byte, len(fixture.payloads.ExpectedRuntime))
	for address, runtime := range fixture.payloads.ExpectedRuntime {
		current.ExpectedRuntime[address] = slices.Clone(runtime)
	}
	fixture.payloads.PrecompileProbe = []byte{0x60, 0x12, 0x60, 0x00}
	fixture.payloads.ExpectedRuntime[fixture.payloads.PrecompileProbeAddress] = []byte{0x60, 0x12}
	old := *fixture.plan.PrecompileProbeSuccessor
	old.CreationHash = crypto.Keccak256Hash(fixture.payloads.PrecompileProbe).Hex()
	old.RuntimeHash = crypto.Keccak256Hash(fixture.payloads.ExpectedRuntime[fixture.payloads.PrecompileProbeAddress]).Hex()
	if err := rebindPrecompileProbeSuccessor(fixture.plan, fixture.source, fixture.payloads, &old); err != nil {
		t.Fatal(err)
	}
	var err error
	fixture.plan.PlanHash, err = fixture.plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	persistFleetCommitmentRecoveryTestPlan(t, fixture.stateDir, fixture.plan)
	evidence := old.Evidence
	evidence.ProbeAddress = old.Probe
	coldkey := ss58Mirror(common.HexToAddress(old.Probe))
	evidence.ProbeColdkey = hexBytesValue(coldkey[:])
	evidence.CommitmentSource = precompileProbeCommitmentSource(&old)
	evidence.Battery = completePrecompileEvidence().Battery
	evidence.Battery.FinalizedHead = ChainHead{Number: 250, Hash: common.Hash{81}.Hex()}
	evidence.Battery.SelfColdkey, evidence.Battery.SampleSelfStake = evidence.ProbeColdkey, "0"
	if err := writePrecompileEvidence(fixture.stateDir, &evidence); err != nil {
		t.Fatal(err)
	}
	owner := &Executor{cfg: fixture.cfg, stateDir: fixture.stateDir, plan: fixture.plan}
	next := fixture.entries[len(fixture.entries)-1].Sequence + 1
	create := actionByID(t, fixture.plan, "precompile.probe-deploy")
	finalized := JournalEntry{Sequence: next, PlanHash: fixture.plan.PlanHash, DeploymentID: fixture.plan.DeploymentID, ActionID: create.ID, IntentHash: create.IntentHash, Stage: StageFinalized, TransactionHash: common.Hash{82}.Hex(), BlockNumber: 240, BlockHash: common.Hash{83}.Hex(), EntryHash: common.Hash{84}.Hex()}
	fixture.entries = append(fixture.entries, finalized)
	var batteryRecord *ActionPostcondition
	for index, id := range []string{"precompile.probe-deploy", "precompile.read-battery"} {
		action := actionByID(t, fixture.plan, id)
		entry := JournalEntry{Sequence: next + uint64(index) + 1, DeploymentID: fixture.plan.DeploymentID, PlanHash: fixture.plan.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageVerified, EntryHash: common.Hash{byte(85 + index)}.Hex()}
		observed := map[string]any{"kind": action.Kind, "target": action.Target, "address": old.Probe, "runtime_hash": old.RuntimeHash}
		if index == 1 {
			observed = map[string]any{"kind": action.Kind, "target": action.Target, "probe": old.Probe, "evidence_hash": evidence.EvidenceHash, "complete": false, "canonical_chain_evidence": true}
		}
		record := testFleetSupersessionPostcondition(fixture.cfg, action, entry, evidence.Battery.FinalizedHead.Number, observed)
		entry.PostconditionPath, entry.PostconditionHash, err = owner.persistActionPostcondition(record)
		if err != nil {
			t.Fatal(err)
		}
		fixture.entries = append(fixture.entries, entry)
		if index == 1 {
			batteryRecord = record
		}
	}
	seed := actionByID(t, fixture.plan, "precompile.seed")
	fixture.entries = append(fixture.entries, JournalEntry{Sequence: next + 3, DeploymentID: fixture.plan.DeploymentID, PlanHash: fixture.plan.PlanHash, ActionID: seed.ID, IntentHash: seed.IntentHash, Stage: StageIntent, EntryHash: common.Hash{87}.Hex()}, JournalEntry{Sequence: next + 4, DeploymentID: fixture.plan.DeploymentID, PlanHash: fixture.plan.PlanHash, ActionID: seed.ID, IntentHash: seed.IntentHash, Stage: StageFailed, Error: "synthetic seed estimate reverted", EntryHash: common.Hash{88}.Hex()})
	evidence.Seed = PrecompileValueStep{TAORao: fixture.plan.LiveFacts.ProbeTAORao, ValueWei: new(big.Int).Mul(new(big.Int).SetUint64(fixture.plan.LiveFacts.ProbeTAORao), big.NewInt(1_000_000_000)).String()}
	if err := writePrecompileEvidence(fixture.stateDir, &evidence); err != nil {
		t.Fatal(err)
	}
	return &precompileProbeRetirementFixture{precompileProbeSuccessorFixture: fixture, evidence: &evidence, batteryRecord: batteryRecord, current: &current}
}

// Executes the production constructor and binder, retaining a hash-authenticated
// old approval so both immutable generations can be audited after restart.
func (self *precompileProbeRetirementFixture) successor(t *testing.T) *SetupPlan {
	t.Helper()
	nonce := self.plan.PrecompileProbeSuccessor.DeployerNonce + 1
	successor, err := newPrecompileProbeSeedSuccessor(self.cfg, self.stateDir, self.plan, self.current, self.entries, ChainHead{Number: 300, Hash: common.Hash{90}.Hex()}, nonce)
	if err != nil {
		t.Fatal(err)
	}
	plan := clonePrecompileProbeSuccessorPlan(t, self.plan)
	plan.PlanHash = ""
	plan.PriorPlanHashes = append(plan.PriorPlanHashes, self.plan.PlanHash)
	if err := rebindPrecompileProbeSuccessor(plan, self.plan, self.current, successor); err != nil {
		t.Fatal(err)
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

// The old envelope remains invalid for changed source; a separately approved
// new address succeeds while preserving the exact deployed v1 and native proof.
func TestPrecompileProbeRetirementAdmitsNewLockedArtifact(t *testing.T) {
	fixture := newPrecompileProbeRetirementFixture(t)
	if err := bindPrecompileProbeSuccessorPayloads(fixture.current, fixture.plan.PrecompileProbeSuccessor); err == nil {
		t.Fatal("new source was silently rebound to an already deployed probe")
	}
	plan := fixture.successor(t)
	if plan.PrecompileProbeSuccessor.Schema != "urnetwork-precompile-probe-successor-v2" || approvedPrecompileProbe(plan) == approvedPrecompileProbe(fixture.plan) || !reflect.DeepEqual(plan.PrecompileProbeSuccessor.Retirement.Predecessor, fixture.plan.PrecompileProbeSuccessor) {
		t.Fatal("new generation lost the exact deployed predecessor")
	}
	if _, err := readPrecompileProbeSuccessorSource(fixture.cfg, fixture.stateDir, plan, fixture.entries); err != nil {
		t.Fatal(err)
	}
	if err := bindPrecompileProbeSuccessorPayloads(fixture.current, plan.PrecompileProbeSuccessor); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"precompile.commitment-write", "precompile.commitment-restore"} {
		if !reflect.DeepEqual(actionByID(t, plan, id), actionByID(t, fixture.plan, id)) {
			t.Fatalf("native action %s was rewritten", id)
		}
	}
	if approved, err := precompileProbeSuccessorNonce(plan, fixture.entries, plan.PrecompileProbeSuccessor.DeployerNonce); err != nil || !approved {
		t.Fatalf("retired CREATE contaminated new nonce prefix: %v", err)
	}
}

// A later rate amendment cannot relabel the retired proof, but its exact
// approved ancestor remains replayable with the original policy document.
func TestPrecompileProbeRetirementRetainsRateAmendmentAncestor(t *testing.T) {
	fixture := newPrecompileProbeRetirementFixture(t)
	prior := fixture.successor(t)
	plan := clonePrecompileProbeSuccessorPlan(t, prior)
	plan.PriorPlanHashes = append(plan.PriorPlanHashes, prior.PlanHash)
	cfg := *fixture.cfg
	cfg.Policy = rateAmendmentTestPolicy(t, fixture.cfg.Policy)
	var err error
	cfg.PolicyHash, err = cfg.Policy.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	plan.PolicyHash = cfg.PolicyHash
	plan.PolicyRateAmendment = &PolicyRateAmendment{Schema: policyRateAmendmentSchema, PriorPlanHash: prior.PlanHash, Previous: *fixture.cfg.Policy, Next: *cfg.Policy}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readPrecompileProbeSuccessorSource(&cfg, fixture.stateDir, plan, fixture.entries); err != nil {
		t.Fatalf("exact historical retirement rejected after rate amendment: %v", err)
	}
	if plan.PrecompileProbeSuccessor.Retirement.Evidence.PolicyHash != fixture.cfg.PolicyHash {
		t.Fatal("historical proof was relabelled with the successor policy")
	}
	for _, mutation := range []string{"missing amendment", "unapproved ancestor", "foreign policy", "relabelled evidence"} {
		t.Run(mutation, func(t *testing.T) {
			changed := clonePrecompileProbeSuccessorPlan(t, plan)
			switch mutation {
			case "missing amendment":
				changed.PolicyRateAmendment = nil
			case "unapproved ancestor":
				changed.PriorPlanHashes = []string{prior.PlanHash}
			case "foreign policy":
				changed.PolicyRateAmendment.Previous.PolicyID++
			case "relabelled evidence":
				evidence := &changed.PrecompileProbeSuccessor.Retirement.Evidence
				evidence.PolicyHash = changed.PolicyHash
				evidence.EvidenceHash = ""
				evidence.EvidenceHash, err = canonicalHashHex(evidence)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := readPrecompileProbeSuccessorSource(&cfg, fixture.stateDir, changed, fixture.entries); err == nil {
				t.Fatal("changed historical policy lineage was accepted")
			}
		})
	}
}

// Tampering with current bytes, predecessor bytes or any exact phase witness
// cannot reuse approval, even if the caller reconstructs the descriptor hash.
func TestPrecompileProbeRetirementRejectsTamperedLineage(t *testing.T) {
	fixture := newPrecompileProbeRetirementFixture(t)
	plan := fixture.successor(t)
	for _, change := range []string{"predecessor runtime", "predecessor creation", "predecessor nonce", "predecessor source", "source", "create", "battery", "seed", "journal", "anchor", "funded", "extra generation"} {
		changed := clonePrecompileProbeSuccessorPlan(t, plan)
		successor := changed.PrecompileProbeSuccessor
		retirement := successor.Retirement
		switch change {
		case "predecessor runtime":
			retirement.Predecessor.RuntimeHash = common.Hash{99}.Hex()
		case "predecessor creation":
			retirement.Predecessor.CreationHash = common.Hash{99}.Hex()
		case "predecessor nonce":
			retirement.Predecessor.DeployerNonce++
		case "predecessor source":
			retirement.Predecessor.SourcePlanHash = common.Hash{99}.Hex()
		case "source":
			retirement.SourcePlanHash = fixture.source.PlanHash
		case "create":
			retirement.Create.PostconditionHash = common.Hash{99}.Hex()
		case "battery":
			retirement.Battery.PostconditionHash = common.Hash{99}.Hex()
		case "seed":
			retirement.SeedFailure.TransactionHash = common.Hash{99}.Hex()
		case "journal":
			retirement.JournalHash = common.Hash{99}.Hex()
		case "anchor":
			successor.FinalizedHead.Number++
		case "funded":
			retirement.Evidence.Seed.BeforeRao++
		case "extra generation":
			retirement.Predecessor.Retirement = &PrecompileProbeRetirement{}
		}
		if _, err := readPrecompileProbeSuccessorSource(fixture.cfg, fixture.stateDir, changed, fixture.entries); err == nil {
			t.Fatalf("%s escaped retirement authentication", change)
		}
	}
	for _, change := range []string{"creation", "runtime", "address"} {
		payloads := *fixture.current
		payloads.ExpectedRuntime = make(map[common.Address][]byte, len(fixture.current.ExpectedRuntime))
		for address, code := range fixture.current.ExpectedRuntime {
			payloads.ExpectedRuntime[address] = slices.Clone(code)
		}
		successor := *plan.PrecompileProbeSuccessor
		switch change {
		case "creation":
			payloads.PrecompileProbe = []byte{0x60, 0x99}
		case "runtime":
			payloads.ExpectedRuntime[payloads.PrecompileProbeAddress] = []byte{0x60, 0x99}
		case "address":
			successor.Probe = common.Address{99}.Hex()
		}
		if err := bindPrecompileProbeSuccessorPayloads(&payloads, &successor); err == nil {
			t.Fatalf("changed %s reused the approved new envelope", change)
		}
	}
}

// Any broadcast, included, finalized or verified seed, even one later labelled
// failed, must be reconciled on the original contract before replacement.
func TestPrecompileProbeRetirementRejectsPendingAndValueProgress(t *testing.T) {
	fixture := newPrecompileProbeRetirementFixture(t)
	for _, change := range []string{"broadcast", "failed with hash", "included", "finalized", "verified", "move", "foreign", "wrong intent", "missing failure", "new unfinished attempt", "nonce"} {
		entries := slices.Clone(fixture.entries)
		entry := &entries[len(entries)-1]
		nonce := fixture.plan.PrecompileProbeSuccessor.DeployerNonce + 1
		switch change {
		case "broadcast":
			entry.Stage = StageBroadcast
		case "failed with hash":
			entry.TransactionHash = common.Hash{99}.Hex()
		case "included":
			entry.Stage = StageIncluded
		case "finalized":
			entry.Stage = StageFinalized
		case "verified":
			entry.Stage = StageVerified
		case "move":
			entry.ActionID = "precompile.move-forward"
			entry.IntentHash = actionByID(t, fixture.plan, entry.ActionID).IntentHash
		case "foreign":
			entry.PlanHash = common.Hash{99}.Hex()
		case "wrong intent":
			entry.IntentHash = common.Hash{99}.Hex()
		case "missing failure":
			entries = entries[:len(entries)-1]
		case "new unfinished attempt":
			later := *entry
			later.Sequence++
			later.Stage = StageIntent
			entries = append(entries, later)
		case "nonce":
			nonce++
		}
		if _, err := newPrecompileProbeSeedSuccessor(fixture.cfg, fixture.stateDir, fixture.plan, fixture.current, entries, ChainHead{Number: 300, Hash: common.Hash{90}.Hex()}, nonce); err == nil {
			t.Fatalf("%s was abandoned by replacement", change)
		}
	}
}

// A stored receipt must still match its immutable original file on every read.
func TestPrecompileProbeRetirementRejectsChangedReceiptFiles(t *testing.T) {
	fixture := newPrecompileProbeRetirementFixture(t)
	plan := fixture.successor(t)
	for _, entry := range []JournalEntry{plan.PrecompileProbeSuccessor.Retirement.Create, plan.PrecompileProbeSuccessor.Retirement.Battery} {
		path := filepath.Join(fixture.stateDir, entry.PostconditionPath)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readPrecompileProbeSuccessorSource(fixture.cfg, fixture.stateDir, plan, fixture.entries); err == nil {
			t.Fatalf("changed %s receipt file reused approval", entry.ActionID)
		}
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// Evidence starts fresh on the new contract while native proof remains exact;
// the old successful battery is retained only as historical predecessor proof.
func TestPrecompileProbeRetirementStartsFreshValueGeneration(t *testing.T) {
	fixture := newPrecompileProbeRetirementFixture(t)
	plan := fixture.successor(t)
	identity := plan.PrecompileProbeSuccessor.Evidence
	identity.ProbeAddress = plan.PrecompileProbeSuccessor.Probe
	coldkey := ss58Mirror(common.HexToAddress(identity.ProbeAddress))
	identity.ProbeColdkey = hexBytesValue(coldkey[:])
	fresh, err := precompileProbeSuccessorEvidence(plan, &identity, fixture.evidence)
	if err != nil || fresh == nil || fresh.Battery != (PrecompileBatteryEvidence{}) || fresh.Seed != (PrecompileValueStep{}) || fresh.Complete || fresh.Commitment != identity.Commitment || !reflect.DeepEqual(fresh.CommitmentSource, fixture.evidence.CommitmentSource) {
		t.Fatalf("fresh generation reused retired battery/value evidence: %+v %v", fresh, err)
	}
	if _, err := precompileProbeSuccessorEvidence(plan, &identity, &plan.PrecompileProbeSuccessor.Evidence); err == nil {
		t.Fatal("v2 migration bypassed its immediate predecessor evidence")
	}
	owner := &Executor{cfg: fixture.cfg, stateDir: fixture.stateDir, plan: plan, payloads: fixture.current, journal: &Journal{entries: fixture.entries}}
	entry := plan.PrecompileProbeSuccessor.Retirement.Battery
	source, handled, err := owner.precompileProbeHistoricalReadSource(actionByID(t, plan, entry.ActionID), entry, fixture.batteryRecord)
	if err != nil || !handled || source == nil || source.payloads.PrecompileProbeAddress != approvedPrecompileProbe(fixture.plan) || source.precompileHistoryEvidence.Seed != (PrecompileValueStep{}) {
		t.Fatalf("historical first-successor battery was routed to the new probe: %v", err)
	}
}

// Both coldkeys are checked at the stable admission point and current finalized
// head. Exact post-CREATE recovery reuses that immutable historical boundary.
func TestPrecompileProbeRetirementRechecksBothCustodyBoundaries(t *testing.T) {
	fixture := newPrecompileProbeRetirementFixture(t)
	plan := fixture.successor(t)
	successor := plan.PrecompileProbeSuccessor
	current := ChainHead{Number: 300, Hash: common.Hash{90}.Hex()}
	for _, fundedAddress := range []common.Address{common.HexToAddress(successor.Probe), common.HexToAddress(successor.Retirement.Predecessor.Probe)} {
		for _, funding := range []string{"balance", "sample", "move"} {
			balanceAt := func(_ context.Context, address common.Address, block *big.Int) (*big.Int, error) {
				if funding == "balance" && address == fundedAddress && block.Uint64() == current.Number {
					return big.NewInt(1), nil
				}
				return new(big.Int), nil
			}
			stakeAt := func(_ context.Context, block uint64, hotkey, coldkey [32]byte) (uint64, error) {
				key := successor.Evidence.SampleHotkey
				if funding == "move" {
					key = successor.Evidence.MoveHotkey
				}
				if funding != "balance" && coldkey == ss58Mirror(fundedAddress) && hotkey == [32]byte(common.HexToHash(key)) && block == current.Number {
					return 1, nil
				}
				return 0, nil
			}
			if err := verifyPrecompileProbeRetirementCustody(context.Background(), successor, current, successor.DeployerNonce, balanceAt, stakeAt); err == nil {
				t.Fatalf("new %s funding at %s escaped admission", funding, fundedAddress)
			}
			if err := verifyPrecompileProbeRetirementCustody(context.Background(), successor, current, successor.DeployerNonce+1, balanceAt, stakeAt); err != nil {
				t.Fatalf("post-CREATE recovery lost immutable custody boundary: %v", err)
			}
		}
	}
}

// Repeated planning preserves the exact candidate; replacing v1 retains its
// spent CREATE ceiling once without retiring an unbroadcast seed allowance.
func TestPrecompileProbeRetirementKeepsApprovalAndSpendStable(t *testing.T) {
	fixture := newPrecompileProbeRetirementFixture(t)
	plan := fixture.successor(t)
	for _, head := range []ChainHead{{Number: 300, Hash: common.Hash{90}.Hex()}, {Number: 301, Hash: common.Hash{91}.Hex()}} {
		entries := append(slices.Clone(fixture.entries), JournalEntry{Sequence: head.Number, EntryHash: head.Hash, ActionID: "scenario.observation"})
		successor, err := newPrecompileProbeSeedSuccessor(fixture.cfg, fixture.stateDir, fixture.plan, fixture.current, entries, head, plan.PrecompileProbeSuccessor.DeployerNonce)
		if err != nil || !reflect.DeepEqual(successor, plan.PrecompileProbeSuccessor) {
			t.Fatalf("advancing finalized head changed approved candidate: %v", err)
		}
	}
	retired, err := addRetiredVerifiedEVMGas(fixture.plan, plan, fixture.entries, fixture.plan.SupersededSpend)
	want, sumErr := addDecimalUint(fixture.plan.SupersededSpend.EVMGasWei, actionByID(t, fixture.plan, "precompile.probe-deploy").Spend.EVMGasWei)
	if err != nil || sumErr != nil || retired.EVMGasWei != want {
		t.Fatalf("retirement did not charge exactly one deployed CREATE: %+v %v", retired, err)
	}
	next := clonePrecompileProbeSuccessorPlan(t, plan)
	if repeated, err := addRetiredVerifiedEVMGas(plan, next, fixture.entries, retired); err != nil || repeated != retired {
		t.Fatalf("ordinary v2 rerender double-counted retirement: %+v %v", repeated, err)
	}
}

// A same-probe allowance alias cannot survive changing its target contract.
func TestPrecompileProbeRetirementDropsOldGenerationIntentAliases(t *testing.T) {
	fixture := newPrecompileProbeRetirementFixture(t)
	plan := fixture.successor(t)
	for index, action := range plan.Actions {
		if action.ID == "precompile.seed" {
			plan.Actions[index] = actionByID(t, fixture.plan, action.ID)
			plan.Actions[index].AcceptedPriorIntentHashes = []string{common.Hash{99}.Hex()}
		}
	}
	if err := rebindPrecompileProbeSuccessor(plan, fixture.plan, fixture.current, plan.PrecompileProbeSuccessor); err != nil {
		t.Fatal(err)
	}
	seed := actionByID(t, plan, "precompile.seed")
	if len(seed.AcceptedPriorIntentHashes) != 0 || actionAcceptsIntent(seed, actionByID(t, fixture.plan, seed.ID).IntentHash) {
		t.Fatal("new probe accepted an old probe's intent alias")
	}
}
