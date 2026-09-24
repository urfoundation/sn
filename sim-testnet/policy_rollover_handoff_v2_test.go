//go:build linux || darwin

package main

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Shared with integration tests for the API/relay selectors: all activation
// signatures, immutable file references and journal receipts are real; chain
// finality itself is owned by the narrowly scoped publication test transport.
func newPolicyRolloverHandoffTestV2(t *testing.T, configure ...func(*policyRolloverGenerationTestV2)) (*policyRolloverGenerationTestV2, *policyRolloverPlanV2, *policyRolloverHandoffV2, *Journal) {
	t.Helper()
	g := newPolicyRolloverGenerationTestV2(t)
	for _, apply := range configure {
		apply(g)
	}
	f := g.fixture
	g.provision(t)
	validators, err := g.stage(t)
	if err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	if err := j.Append(JournalEntry{DeploymentID: f.plan.DeploymentID, PlanHash: f.plan.PlanHash, ActionID: "source.checkpoint", IntentHash: common.Hash{1}.Hex(), Stage: StageIntent}); err != nil {
		t.Fatal(err)
	}
	p := &policyRolloverPlanV2{Schema: policyRolloverPlanV2Schema, SourcePlanHash: f.plan.PlanHash, SourceJournalHash: j.Entries()[0].EntryHash, DeploymentID: f.plan.DeploymentID, StateDir: f.stateDir, ConfigHash: f.cfg.ConfigHash, PolicyHash: f.cfg.PolicyHash,
		Generation: g.generation, Epoch: g.members[0].Activation.Domain.Epoch, Native: f.prepared.Native, EVM: f.prepared.Evm, Journal: f.plan.ValidatorEvidence.Address, JournalRuntimeHash: f.plan.ValidatorEvidence.RuntimeCodeHash, Keeper: common.HexToAddress(f.roles.EVM["keeper"].Address),
		MaximumGasUnits: f.cfg.Config.ValidatorEvidenceActivationGasUnits, MaximumFeePerGasWei: f.plan.MaximumEVMFeePerGasWei, MaximumAttempts: 3, AttemptTimeoutSeconds: 1, Members: g.members}
	limit, err := runtimeEvidenceProvisionLimit(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		id := uint64(index + 1)
		original := filepath.Join(f.stateDir, "runtime", fmt.Sprintf("validator-%d", id), "validator.yml")
		path := filepath.Join(policyRolloverPlanRootV2(f.stateDir, p.Generation, p.Epoch), "source", fmt.Sprintf("validator-%d.yml", id))
		raw := g.original[original]
		if _, err := validatorcomponent.WriteReleaseEvidenceV2File(t.Context(), path, raw, limit); err != nil {
			t.Fatal(err)
		}
		checkpoint := policyRolloverValidatorCheckpointV2{ValidatorID: id, Config: policyRolloverFile(path, raw), StateDir: validators[index].PreviousStateDir}
		for k := 0; k < 2; k++ {
			checkpoint.PreviousActivations = append(checkpoint.PreviousActivations, f.prepared.Members[index*2+k].Activation)
		}
		p.Validators = append(p.Validators, checkpoint)
	}
	p.Actions, err = policyRolloverActionsV2(p)
	if err != nil {
		t.Fatal(err)
	}
	maximum := new(big.Int).Mul(new(big.Int).SetUint64(p.MaximumGasUnits), new(big.Int).SetUint64(p.MaximumFeePerGasWei))
	p.MaximumGasWei = DecimalUint(maximum.Mul(maximum, big.NewInt(4)).String())
	p.PlanHash, err = p.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePolicyRolloverPlanV2(f.cfg, f.plan, f.stateDir, p); err != nil {
		t.Fatal(err)
	}
	if _, err := writeRuntimeEvidenceSetupV2(t.Context(), policyRolloverPlanPathV2(f.stateDir, p.Generation, p.Epoch), p, limit); err != nil {
		t.Fatal(err)
	}
	if _, err := publishPolicyRolloverV2(t.Context(), p, j, limit, policyRolloverPublicationIOV2{Observe: func(context.Context, runtimeEvidenceActivationMemberV2) (uint64, error) {
		return f.completed.Boundary.Number, nil
	}, Send: func(context.Context, Action, runtimeEvidenceActivationMemberV2) error {
		t.Fatal("existing publication sent")
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	h := &policyRolloverHandoffV2{Schema: policyRolloverHandoffV2Schema, PlanHash: p.PlanHash, SourcePlanHash: p.SourcePlanHash, DeploymentID: p.DeploymentID, Activated: true, Generation: p.Generation, CutoffEpoch: p.Epoch, FirstFullEpoch: p.Epoch + 1, Native: p.Native, EVM: p.EVM, Boundary: f.completed.Boundary, Members: p.Members, Validators: validators, Identities: validators[0].Identities}
	return g, p, h, j
}

func activatePolicyRolloverHandoffTestV2(t *testing.T, g *policyRolloverGenerationTestV2, p *policyRolloverPlanV2, h *policyRolloverHandoffV2, j *Journal) {
	t.Helper()
	limit, err := runtimeEvidenceProvisionLimit(g.fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	action, err := policyRolloverHandoffActionV2(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := persistPolicyRolloverPostconditionV2(t.Context(), p, j, action, h, limit); err != nil {
		t.Fatal(err)
	}
	if _, err := writeRuntimeEvidenceSetupV2(t.Context(), filepath.Join(policyRolloverRoot(p.StateDir), "handoff.json"), h, limit); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyRolloverHandoffSelectsCompleteGenerationAndRejectsChangedContext(t *testing.T) {
	g, p, h, j := newPolicyRolloverHandoffTestV2(t)
	f := g.fixture
	if got, err := readPolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan); err != nil || got != nil {
		t.Fatalf("staging selected new inputs: %v", err)
	}
	activatePolicyRolloverHandoffTestV2(t, g, p, h, j)
	got, err := readPolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil || got == nil {
		t.Fatalf("active generation unreadable: %v", err)
	}
	specs := buildClientSpecs(f.cfg, f.stateDir, map[string]string{"sim-testnet": "test-binary"}, f.roles)
	for index := range specs {
		if specs[index].Role == "validator" {
			specs[index].Args = append(specs[index].Args, "--provisional-activation-setup=old", "--provisional-activation-setup-sha256=old")
		}
	}
	if err := attachPolicyRolloverProcessConfigsV2(t.Context(), f.cfg, f.stateDir, f.plan, specs); err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		if spec.Role == "validator" {
			joined := strings.Join(spec.Args, " ")
			if strings.Contains(joined, "provisional-activation-setup") || !strings.Contains(joined, "evidence-generations/generation-") || !strings.Contains(spec.Env["URNETWORK_STATE_DIR"], "evidence-generations/generation-") {
				t.Fatalf("old process source survived: %v", spec)
			}
		}
	}
	path := h.Validators[0].Evidence.Operators[0].Context.Path
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPolicyRolloverHandoffV2(t.Context(), f.cfg, f.stateDir, f.plan); err == nil {
		t.Fatal("changed active context fell back to old generation")
	}
}

func TestRetainedScenarioSelectsActiveRolloverProcessInputs(t *testing.T) {
	g, p, h, j := newPolicyRolloverHandoffTestV2(t)
	f := g.fixture
	activatePolicyRolloverHandoffTestV2(t, g, p, h, j)
	specs := buildClientSpecs(f.cfg, f.stateDir, map[string]string{"sim-testnet": "test-binary"}, f.roles)
	for index := range specs {
		if specs[index].Role == "validator" {
			specs[index].Args = append(specs[index].Args, "--provisional-activation-setup=old", "--provisional-activation-setup-sha256=old")
		}
	}
	if err := attachRetainedProvisionalProcessHandoff(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, specs); err != nil {
		t.Fatal(err)
	}
	for _, validator := range h.Validators {
		var found bool
		for _, spec := range specs {
			if spec.ID != fmt.Sprintf("validator-%d", validator.ValidatorID) {
				continue
			}
			found = true
			if spec.Env["URNETWORK_STATE_DIR"] != validator.ClientStateDir || !strings.Contains(strings.Join(spec.Args, " "), "--config="+validator.Config.Path) || strings.Contains(strings.Join(spec.Args, " "), "provisional-activation-setup") {
				t.Fatalf("retained validator did not select fresh handoff: %+v", spec)
			}
		}
		if !found {
			t.Fatalf("missing retained validator %d", validator.ValidatorID)
		}
	}
	path := h.Validators[0].Evidence.Operators[0].Context.Path
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := attachRetainedProvisionalProcessHandoff(t.Context(), f.cfg, f.stateDir, f.plan, f.roles, specs); err == nil {
		t.Fatal("tampered fresh handoff fell back to old process inputs")
	}
}

func TestPolicyRolloverAPIContextsSelectEveryFreshMember(t *testing.T) {
	g, p, h, j := newPolicyRolloverHandoffTestV2(t)
	f := g.fixture
	activatePolicyRolloverHandoffTestV2(t, g, p, h, j)
	contexts, err := runtimeReservedAttemptUploadContexts(t.Context(), f.cfg, f.stateDir, f.plan)
	if err != nil || len(contexts) != 4 {
		t.Fatalf("fresh API context census: count=%d err=%v", len(contexts), err)
	}
	for index, context := range contexts {
		want := h.Validators[index/2].Evidence.Operators[index%2].Context
		if context != want || !strings.Contains(context.Path, "evidence-generations/generation-") {
			t.Fatalf("API context %d selected another generation: got=%+v want=%+v", index, context, want)
		}
	}
	if err := os.WriteFile(contexts[3].Path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeReservedAttemptUploadContexts(t.Context(), f.cfg, f.stateDir, f.plan); err == nil {
		t.Fatal("changed fresh API context fell back to an old source")
	}
}

func TestPolicyRolloverHandoffUsesApprovedSourceBoundsFromEitherConfigView(t *testing.T) {
	g, p, h, j := newPolicyRolloverHandoffTestV2(t, func(g *policyRolloverGenerationTestV2) {
		f := g.fixture
		continuation := &EvidenceRelayContinuation{Schema: evidenceRelayContinuationSourceExpansionSchema}
		for _, configured := range f.cfg.Config.ValidatorEvidenceV2 {
			bounds, err := doubledEvidenceRelaySourceBounds(configured.Evidence.Bounds)
			if err != nil {
				t.Fatal(err)
			}
			continuation.SourceBounds = append(continuation.SourceBounds, evidenceRelaySourceBounds{ValidatorId: configured.ValidatorID, Original: configured.Evidence.Bounds, Approved: bounds})
		}
		f.plan.EvidenceRelayContinuation = continuation
		// A resolved old-source view can carry historical policy. The fresh
		// generation must still render only its independent current policy.
		f.cfg.previousPolicy = f.cfg.Policy
	})
	f := g.fixture
	activatePolicyRolloverHandoffTestV2(t, g, p, h, j)
	resolved, err := evidenceRelaySourceCapacityConfig(f.cfg, f.plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []*ResolvedConfig{f.cfg, resolved} {
		got, err := readPolicyRolloverHandoffV2(t.Context(), cfg, f.stateDir, f.plan)
		if err != nil || got == nil {
			t.Fatalf("approved source bounds rejected: %v", err)
		}
		for index, validator := range got.Validators {
			if !reflect.DeepEqual(validator.Evidence.Bounds, f.plan.EvidenceRelayContinuation.SourceBounds[index].Approved) {
				t.Fatal("generation lost the approved source bounds")
			}
		}
	}
	if reflect.DeepEqual(f.cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds, resolved.Config.ValidatorEvidenceV2[0].Evidence.Bounds) {
		t.Fatal("approved projection mutated the original template")
	}
}

func TestPolicyRolloverCLIRequiresBothSourceAndSubplanApproval(t *testing.T) {
	hash := common.Hash{1}.Hex()
	for _, args := range [][]string{{"policy-rollover", "--plan-hash", hash, "--rollover-generation", "1", "--rollover-epoch", "15"}, {"policy-rollover", "--apply", "--plan-hash", hash, "--rollover-plan", "/tmp/rollover.json", "--rollover-plan-hash", hash, "--provisional-resume", "--owned-rpc-authority", "10.0.0.1:9944"}} {
		if _, _, err := parseCLI(args); err != nil {
			t.Fatalf("valid command: %v", err)
		}
	}
	for _, args := range [][]string{{"policy-rollover", "--apply", "--plan-hash", hash, "--rollover-plan", "/tmp/rollover.json"}, {"policy-rollover", "--plan-hash", hash, "--rollover-generation", "1"}, {"resume", "--rollover-epoch", "15"}} {
		if _, _, err := parseCLI(args); err == nil {
			t.Fatalf("invalid command accepted: %v", args)
		}
	}
}
