package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Exercise the same composed binder as BuildPlanRevision: an original signed
// companion CREATE/anchor plus a separately signed and finalized correction.
// The correction must not move either retained predecessor or consume the next
// free nonce, and its original actions must keep their receipt identities.
func TestCoordinatorRepairCarryBindsOriginalCompanionThroughRepeatedUpgrade(t *testing.T) {
	var companion *validatorEvidenceCarryObservation
	fixture := newCoordinatorRepairCarryPreparedFixture(t, func(original validatorEvidenceCarryTestFixture) {
		companion = original.authenticate(t)
		// These three predecessor markers are the reducer's durable source
		// prerequisite. Companion and corrective transaction authority below
		// still use their actual signed bytes and independent HTTP receipts.
		for index, id := range []string{"evm.coordinator-upgrade-implementation", "evm.coordinator-upgrade-activate", "fleet.refresh.deploy-batcher"} {
			action := actionByID(t, original.executor.plan, id)
			path, err := postconditionRelativePath(original.executor.plan.PlanHash, id)
			if err != nil {
				t.Fatal(err)
			}
			if err := original.executor.journal.Append(JournalEntry{DeploymentID: original.executor.plan.DeploymentID, PlanHash: original.executor.plan.PlanHash, ActionID: id, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionHash: common.Hash{byte(index + 1)}.Hex(), PostconditionPath: path}); err != nil {
				t.Fatal(err)
			}
		}
	})
	e := fixture.executor
	entries := e.journal.Entries()
	retained := map[string][]byte{}
	for _, name := range []string{"journal.jsonl", "plans/" + stringsTrim0x(e.plan.PlanHash) + ".json", coordinatorRepairDirectory + "/request.json", coordinatorRepairDirectory + "/result.json"} {
		raw, err := os.ReadFile(filepath.Join(e.stateDir, name))
		if err != nil {
			t.Fatal(err)
		}
		retained[name] = raw
	}
	observed, err := authenticateCoordinatorRepairCarry(t.Context(), e.cfg, e.stateDir, e.plan, entries, e.deployer.client, e.independentEVM)
	if err != nil {
		t.Fatal(err)
	}
	e.plan.coordinatorRepairObserved = observed
	e.plan.validatorEvidenceObserved = companion
	payloads, err := buildDeploymentPayloadsWithRegistrationGeneration(e.cfg, e.roles, e.plan.Deployment.InitialNonce, e.plan.Deployment.RegistrationRoleGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if err := bindValidatorEvidenceCarryPayloads(payloads, companion); err != nil {
		t.Fatal(err)
	}
	request := observed.reference.Request.Request
	if err := configureCoordinatorUpgradeNonce(payloads, request.Upgrade.DeployerNonce); err != nil {
		t.Fatal(err)
	}
	if err := bindCoordinatorRepairCarryPayloads(payloads, observed); err != nil {
		t.Fatal(err)
	}
	roles, err := derivePublicRoles(e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	revised, err := buildPlan(e.cfg, &e.plan.LiveFacts, roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	revised.PriorPlanHashes = append(append([]string(nil), e.plan.PriorPlanHashes...), e.plan.PlanHash)
	if err := carryValidatorEvidencePlan(revised, e.plan, companion); err != nil {
		t.Fatal(err)
	}
	clone := func(plan *SetupPlan) *SetupPlan {
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
	// The unadopted path keeps refusing: merely observing an occupied
	// companion cannot authorize a nonadjacent predecessor.
	if err := rebindPlanCoordinatorUpgrade(clone(revised), payloads); err == nil || !strings.Contains(err.Error(), "different predecessor CREATE") {
		t.Fatalf("unadopted correction bypassed predecessor admission: %v", err)
	}
	filesOnly, err := readCoordinatorRepairCarry(e.stateDir, e.plan, entries)
	if err != nil {
		t.Fatal(err)
	}
	filesPrior := *e.plan
	filesPrior.coordinatorRepairObserved = filesOnly
	if err := rebindPlanCoordinatorUpgradeWithCarry(clone(revised), &filesPrior, payloads, filesOnly, entries); err == nil {
		t.Fatal("signed files without completed chain authentication entered revision")
	}
	changedEntries := append([]JournalEntry(nil), entries...)
	changedEntries[0].Error = "changed"
	if err := rebindPlanCoordinatorUpgradeWithCarry(clone(revised), e.plan, payloads, observed, changedEntries); err == nil {
		t.Fatal("changed source journal reused completed authentication")
	}
	if err := rebindPlanCoordinatorUpgradeWithCarry(revised, e.plan, payloads, observed, entries); err != nil {
		t.Fatalf("original companion plus completed correction was rejected: %v", err)
	}
	batcher := actionByID(t, revised, "fleet.refresh.deploy-batcher")
	batcherNonce := request.OldUpgrade.DeployerNonce + 1
	if revised.CoordinatorUpgrade != request.Upgrade || *revised.ValidatorEvidence != *e.plan.ValidatorEvidence || batcher.Target != crypto.CreateAddress(payloads.Deployer, batcherNonce).Hex() || batcher.Parameters["expected_nonce"] != strconv.FormatUint(batcherNonce, 10) || revised.ValidatorEvidence.DeployerNonce != batcherNonce+1 || request.Upgrade.DeployerNonce != batcherNonce+2 || observed.deployerNonce != request.Upgrade.DeployerNonce+1 {
		t.Fatal("original, corrected and next CREATE identities were conflated")
	}
	for _, id := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID, "evm.coordinator-upgrade-implementation", "evm.coordinator-upgrade-activate", "fleet.refresh.deploy-batcher"} {
		if !reflect.DeepEqual(actionByID(t, revised, id), actionByID(t, e.plan, id)) {
			t.Fatalf("original executed action %s changed", id)
		}
	}
	if !reflect.DeepEqual(*revised.CoordinatorRepairCarry, fixture.reference) || !reflect.DeepEqual(*revised.ValidatorEvidenceCarry, companion.reference) {
		t.Fatal("revision changed original source or signed receipt authority")
	}
	if equal, err := equalSpend(revised.MaximumSpend, e.plan.MaximumSpend); err != nil || !equal || revised.Limits != e.plan.Limits {
		t.Fatalf("completed correction changed the campaign allowance: %v", err)
	}
	for _, test := range []struct {
		name string
		change func(*SetupPlan)
	}{
		{"old-upgrade", func(plan *SetupPlan) { plan.CoordinatorRepairCarry.Request.Request.OldUpgrade.DeployerNonce++ }},
		{"result-signature", func(plan *SetupPlan) { plan.CoordinatorRepairCarry.Result.Signature = "changed" }},
		{"companion-source", func(plan *SetupPlan) { plan.ValidatorEvidenceCarry.SourcePlanHash = common.Hash{7}.Hex() }},
		{"next-batcher", func(plan *SetupPlan) {
			for index := range plan.Actions {
				if plan.Actions[index].ID == "fleet.refresh.deploy-batcher" {
					plan.Actions[index].Target = crypto.CreateAddress(payloads.Deployer, observed.deployerNonce).Hex()
					plan.Actions[index].Parameters["expected_nonce"] = strconv.FormatUint(observed.deployerNonce, 10)
				}
			}
		}},
		{"batcher-signer", func(plan *SetupPlan) {
			for index := range plan.Actions {
				if plan.Actions[index].ID == "fleet.refresh.deploy-batcher" {
					plan.Actions[index].Parameters["expected_signer"] = common.Address{8}.Hex()
				}
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := clone(revised)
			test.change(changed)
			if err := validateValidatorEvidencePlan(changed); err == nil {
				t.Fatal("changed predecessor or carry authority was admitted")
			}
		})
	}
	for name, before := range retained {
		after, err := os.ReadFile(filepath.Join(e.stateDir, name))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("revision changed original %s: %v", name, err)
		}
	}
	if !reflect.DeepEqual(entries, e.journal.Entries()) || fixture.reader.sends.Load() != 0 || fixture.independent.sends.Load() != 0 {
		t.Fatal("read-only composition changed durable history or sent a transaction")
	}
}
