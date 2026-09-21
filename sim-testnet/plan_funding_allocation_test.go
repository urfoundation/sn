//go:build linux || darwin

package main

// Raised hard ceilings must not turn unused headroom into wallet funding. Real
// complete plans exercise the builder and a second recovery after the increase.

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPlanFundingAllocation512CapsPreserveExistingRoleFunding(t *testing.T) {
	cfg := testResolvedConfig(t)
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := buildPlan(cfg, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	originalAllocation := prior.Limits.EVMGasWei
	priorBytes, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaximumEVMGasWei = "512000000000000000000"
	cfg.MaximumTAORao = 512_000_000_000
	current := partialRevisionFacts(t, cfg, 1)
	current.WalletFreeTAORao = 1_000_000_000_000
	revised, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), prior, current, nil, time.Unix(2, 0))
	if err != nil {
		t.Fatal("cap-only increase attempted to allocate or fund the full 512 EVM", err)
	}
	if revised.Limits.EVMGasWei != cfg.MaximumEVMGasWei || revised.Limits.TAORao != cfg.MaximumTAORao || revised.EVMFundingAllocationWei != originalAllocation {
		t.Fatal("revision did not bind separate 512 hard caps and retained allocation", revised.Limits, revised.EVMFundingAllocationWei)
	}
	funds := 0
	for _, old := range prior.Actions {
		if !strings.HasPrefix(old.ID, "evm.fund-") {
			continue
		}
		retained := actionByID(t, revised, old.ID)
		if !reflect.DeepEqual(old, retained) {
			t.Fatalf("unused budget increased or rewrote role funding %s", old.ID)
		}
		funds++
	}
	if funds == 0 || revised.MaximumSpend.EVMGasWei != prior.MaximumSpend.EVMGasWei {
		t.Fatal("cap revision changed actual planned EVM allocation", funds, revised.MaximumSpend, prior.MaximumSpend)
	}
	again, err := buildPlanRevisionFromFacts(cfg, t.TempDir(), revised, current, nil, time.Unix(3, 0))
	if err != nil {
		t.Fatal("second recovery reset explicit allocation to hard cap", err)
	}
	if again.EVMFundingAllocationWei != originalAllocation || again.MaximumSpend.EVMGasWei != revised.MaximumSpend.EVMGasWei {
		t.Fatal("second recovery allocated unused headroom")
	}
	if err := validatePlanBudget(again); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(prior)
	if err != nil || !bytes.Equal(priorBytes, after) {
		t.Fatal("revision mutated its predecessor", err)
	}
	raw, err := json.Marshal(revised)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodePersistedPlanBytes(raw)
	if err != nil || decoded.PlanHash != revised.PlanHash || decoded.EVMFundingAllocationWei != originalAllocation {
		t.Fatal("persisted approval did not authenticate its retained funding allocation", err)
	}
}

func TestPlanFundingAllocationRejectsMalformedOrExcessiveAuthority(t *testing.T) {
	cfg := testResolvedConfig(t)
	cfg.MaximumEVMGasWei = "512000000000000000000"
	for _, allocation := range []DecimalUint{"-1", "not-a-number", "513000000000000000000"} {
		prior := &SetupPlan{Limits: Spend{EVMGasWei: cfg.MaximumEVMGasWei}, EVMFundingAllocationWei: allocation}
		if _, err := planRevisionEVMFundingAllocation(cfg, prior); err == nil {
			t.Fatal("malformed or excessive prior allocation entered the revision", allocation)
		}
		if err := validatePlanBudget(prior); err == nil {
			t.Fatal("strict acceptance accepted malformed allocation", allocation)
		}
	}
	prior := &SetupPlan{Limits: Spend{EVMGasWei: "512000000000000000000"}, EVMFundingAllocationWei: "290000000000000000000"}
	cfg.MaximumEVMGasWei = "280000000000000000000"
	if value, err := planRevisionEVMFundingAllocation(cfg, prior); err != nil || value != cfg.MaximumEVMGasWei {
		t.Fatal("lower configured hard cap was not applied", value, err)
	}
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildPlanWithFundingAllocation(cfg, testSetupFacts(), roles, time.Unix(1, 0), 0, "290000000000000000000"); err == nil {
		t.Fatal("builder permitted allocation above the current hard cap")
	}
}

func TestPlanFundingAllocation512CapsAddOnlyExactRelayExpansion(t *testing.T) {
	fixture, executor := newEvidenceRelayRefreshTest(t)
	original := executor.plan
	fixture.cfg.MaximumEVMGasWei = "512000000000000000000"
	fixture.cfg.MaximumTAORao = 512_000_000_000
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var funded SetupPlan
	if err := json.Unmarshal(raw, &funded); err != nil {
		t.Fatal(err)
	}
	funded.PriorPlanHashes = append(funded.PriorPlanHashes, original.PlanHash)
	funded.Limits = configuredPlanLimits(fixture.cfg)
	funded.EVMFundingAllocationWei, err = planRevisionEVMFundingAllocation(fixture.cfg, original)
	if err != nil {
		t.Fatal(err)
	}
	funded.ResolvedInputsHash, err = resolvedInputsHash(fixture.cfg)
	if err != nil {
		t.Fatal(err)
	}
	funded.PlanHash, err = funded.hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, &funded, fixture.roles); err != nil {
		t.Fatal(err)
	}
	executor.plan = &funded
	current := evidenceRelayExpansionRequestTest(t, executor)
	expanded, err := appendEvidenceRelayContinuationPlan(&funded, current)
	if err != nil {
		t.Fatal(err)
	}
	delta, err := subtractDecimalUint(expanded.MaximumSpend.EVMGasWei, funded.MaximumSpend.EVMGasWei)
	if err != nil || delta != "25600000000000000000" || expanded.MaximumSpend.TAORao != funded.MaximumSpend.TAORao || expanded.Limits != funded.Limits {
		t.Fatal("expansion spent more than its exact extra reserve or changed 512 hard caps", delta, err)
	}
	for _, old := range funded.Actions {
		if old.ID == evidenceRelayReserveId {
			continue
		}
		if retained := actionByID(t, expanded, old.ID); !reflect.DeepEqual(old, retained) {
			t.Fatal("extra relay reserve rewrote unrelated funding or executable action", old.ID)
		}
	}
	if expanded.EVMFundingAllocationWei != funded.EVMFundingAllocationWei {
		t.Fatal("relay reserve expansion increased every role funding allocation")
	}
	if err := validateEvidenceRelayContinuationSource(fixture.stateDir, expanded, executor.journal.Entries()); err != nil {
		t.Fatal(err)
	}
	if err := validatePlanBudget(expanded); err != nil {
		t.Fatal(err)
	}
}
