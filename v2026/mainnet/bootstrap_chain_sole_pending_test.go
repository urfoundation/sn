// The sole launch config is signed before its coordinator and operator exist,
// so it can only pre-declare its activation inputs. V5 admits exactly that
// census; every other signed fact and every two-role schema stays strict.
package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/validator"
)

// Every activation input keeps its declared path and scratch roots; only the
// lengths and digests that rendering will pin are absent.
func bootstrapChainSolePendingEvidence(v *bootstrapChainValidatorFixture) {
	for index := range v.config.EvidenceV2.Operators {
		operator := &v.config.EvidenceV2.Operators[index]
		for _, file := range []*validator.ReleaseEvidenceV2File{&operator.Activation, &operator.VPKSignature, &operator.HotkeySignature, &operator.Context, &operator.History} {
			file.Bytes, file.SHA256 = 0, ""
		}
	}
}

// Re-sign the pending config with its approval key and reload the preparation.
func bootstrapChainSolePending(t *testing.T, f *bootstrapChainFixture) {
	t.Helper()
	bootstrapChainSolePendingEvidence(f.validators[0])
	f.config.Validators[0].Config = f.validators[0].publish(t)
	bootstrapChainSoleLoad(t, f)
}

// The sealed plan records the pending census and reserves every declared input
// path; the signed approval, identities and offline apply are unchanged.
func TestBootstrapChainSoleAdmitsActivationPendingConfig(t *testing.T) {
	f := newBootstrapChainSoleFixture(t, true, bootstrapChainTreasuryApproval([32]byte(bytes.Repeat([]byte{0x77}, 32))))
	bootstrapChainSolePending(t, f)
	plan, v := f.preparation.Plan, f.validators[0]
	inspection := plan.ValidatorInspections[0]
	if plan.Schema != bootstrapChainPlanSchemaV5 || len(plan.ValidatorInspections) != 1 || !inspection.EvidenceActivationPending ||
		!reflect.DeepEqual(inspection.Approval, v.approval) || inspection.Approval.Proposal.Treasury == nil || inspection.ApprovalReference != v.config.TreasuryApproval.Approval ||
		inspection.ApprovalSigner != f.config.Validators[0].ApprovalPublicKey || inspection.ValidatorId != f.config.Validators[0].ValidatorId {
		t.Fatalf("pending sole config lost its signed facts or pending record: %+v", inspection)
	}
	for _, operator := range v.config.EvidenceV2.Operators {
		if !operator.Unrendered() {
			t.Fatal("fixture census is not pending")
		}
		for _, file := range operator.Files() {
			if !slices.Contains(inspection.DeclaredPaths, file.Path) {
				t.Fatal("pending input path is not reserved as custody", file.Path)
			}
		}
		if !slices.Contains(inspection.DeclaredPaths, operator.ReplayScratchRoot) || !slices.Contains(inspection.DeclaredPaths, operator.SealScratchRoot) {
			t.Fatal("pending scratch roots are not reserved as custody")
		}
	}
	unsealed := plan
	unsealed.ContentHash = ""
	if raw, err := json.Marshal(unsealed); err != nil || !bytes.Contains(raw, []byte(`"evidence_activation_pending":true`)) || plan.ContentHash != bootstrapChainPlanHash(plan) {
		t.Fatal("plan seal does not cover the pending census", err)
	}
	var stdout, stderr bytes.Buffer
	if code := f.command(t.Context(), "plan", &stdout, &stderr); code != 0 {
		t.Fatalf("plan exit %d: %s", code, stderr.String())
	}
	var decoded bootstrapChainPlan
	if err := decodePlanJson(stdout.Bytes(), &decoded); err != nil || !reflect.DeepEqual(decoded, plan) {
		t.Fatal("public plan differs from the accepted pending preparation", err)
	}
	result := f.result(t, "apply")
	if result.Schema != "urnetwork-mainnet-bootstrap-chain-result-v5" || result.PlanHash != plan.ContentHash || !result.LocalPreparationComplete ||
		!result.UrValidatorConfigsVerified || result.NetworkEffects || result.ActivationReady ||
		result.CurrentEconomicAcceptance != "native-10-percent-provider-allocation-and-90-percent-native-treasury-acceptance" {
		t.Fatalf("pending sole preparation changed its offline result: %+v", result)
	}
	if err := validateBootstrapChainValidatorInspections(plan.Config, plan.ValidatorInspections); err != nil {
		t.Fatal("retained v5 pending inspection refused", err)
	}
}

// A pending config stays refused by the two-role schemas, and v5 refuses an
// incomplete or stale pending config before any custody claim.
func TestBootstrapChainPendingConfigRefusals(t *testing.T) {
	for _, item := range []struct {
		name       string
		build      func(*testing.T) *bootstrapChainFixture
		diagnostic string
	}{
		{name: "v4-pending", diagnostic: "activation inputs are not rendered", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapRootPassiveFixture(t)
			bootstrapChainSolePendingEvidence(f.validators[0])
			f.config.Validators[0].Config = f.validators[0].publish(t)
			return f
		}},
		{name: "v5-undeclared-path", diagnostic: "pre-declare every input path", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			bootstrapChainSolePendingEvidence(f.validators[0])
			f.validators[0].config.EvidenceV2.Operators[1].Context.Path = ""
			f.config.Validators[0].Config = f.validators[0].publish(t)
			return f
		}},
		{name: "v5-mixed-census", diagnostic: "mixes rendered and activation-pending", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			rendered := f.validators[0].config.EvidenceV2.Operators[0]
			bootstrapChainSolePendingEvidence(f.validators[0])
			f.validators[0].config.EvidenceV2.Operators[0] = rendered
			f.config.Validators[0].Config = f.validators[0].publish(t)
			return f
		}},
		{name: "v5-pending-overlap", diagnostic: "overlaps protected state", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			bootstrapChainSolePendingEvidence(f.validators[0])
			f.validators[0].config.EvidenceV2.Operators[0].History.Path = f.validators[0].config.HotkeySeedFile
			f.config.Validators[0].Config = f.validators[0].publish(t)
			return f
		}},
		{name: "v5-stale-approval", diagnostic: "different complete configuration", build: func(t *testing.T) *bootstrapChainFixture {
			f := newBootstrapChainSoleFixture(t, true, nil)
			bootstrapChainSolePending(t, f)
			f.validators[0].config.PollSeconds++
			f.config.Validators[0].Config = f.validators[0].writeConfig(t)
			return f
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			item.build(t).rejectValidatorPlan(t, item.diagnostic)
		})
	}
	// A retained two-role plan cannot acquire the pending record either.
	f := newBootstrapRootPassiveFixture(t)
	inspections := slices.Clone(f.preparation.Plan.ValidatorInspections)
	inspections[0].EvidenceActivationPending = true
	if err := validateBootstrapChainValidatorInspections(f.preparation.Plan.Config, inspections); err == nil || !strings.Contains(err.Error(), "only under v5") {
		t.Fatal("a two-role plan admitted an activation-pending inspection", err)
	}
}

// Contract-role admission binds the pending sole config to the approved
// proxy, vault and policy exactly as it binds a rendered one.
func TestBootstrapContractRolePlanBindsPendingSoleValidator(t *testing.T) {
	f := newBootstrapSuccessorCommandFixture(t)
	bootstrapRootPassiveConvert(t, f, nil, nil)
	bootstrapChainSoleConvert(t, f)
	bootstrapChainSoleShareRoot(t, f)
	bootstrapChainSoleLoad(t, f)
	bootstrapChainSolePendingEvidence(f.validators[0])
	evidence := bootstrapContractRoleBind(t, f)
	inspection := f.preparation.Plan.ValidatorInspections[0]
	if !inspection.EvidenceActivationPending {
		t.Fatal("contract-bound sole config is not pending")
	}
	var stdout, stderr bytes.Buffer
	if code := runMain(f.storageContext(t.Context()), []string{"bootstrap-chain", "contract-role-plan", "--config", f.path}, &stdout, &stderr); code != 0 {
		t.Fatal("pending sole contract-role plan refused", code, stderr.String())
	}
	var result bootstrapContractRolePlan
	if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	proxy, vault := evidence.priorPlan(4), evidence.priorPlan(1)
	if result.PreparationHash != f.preparation.Plan.ContentHash || result.CoordinatorProxy != proxy.Address || result.SettlementVault != vault.Address ||
		result.InitialPolicyHash != common.HexToHash(inspection.PolicyHash) || len(result.Validators) != 1 || result.Validators[0].Role != "sole" ||
		result.Validators[0].ApprovalHash != rootObjectHash(inspection.Approval) || result.ActivationReady || result.NetworkEffects {
		t.Fatal("pending sole contract-role output lost its binding or claimed authority", result)
	}
	f.validators[0].config.Coordinator = strings.ToLower(evidence.priorPlan(2).Address.Hex())
	bootstrapContractRolePublish(t, f)
	if !f.preparation.Plan.ValidatorInspections[0].EvidenceActivationPending {
		t.Fatal("substitution control lost the pending census")
	}
	bootstrapContractRoleReject(t, f, "coordinator differs from the approved proxy", "pending sole config bypassed the coordinator binding")
}
