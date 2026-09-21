//go:build linux || darwin

package main

// A real retained plan and archived review prove the recovery path changes only
// the two authorized ceilings; ordinary release identity remains strict.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestAllowanceOnlyPlan512PreservesActionsAndStrictReleaseGate(t *testing.T) {
	cfg, source, stateDir, options, before := provisionalRuntimePlanFixture(t)
	cfg.MaximumTAORao = 512_000_000_000
	cfg.MaximumEVMGasWei = "512000000000000000000"
	if _, err := loadPersistedPlan(cfg, stateDir); !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatal("fixture did not reproduce strict release/config revision rejection", err)
	}
	reviewed, err := buildAllowanceOnlyPlan(t.Context(), cfg, stateDir, source.PlanHash)
	if err != nil {
		t.Fatal("cap-only review required new chain or clean-release assertions", err)
	}
	if reviewed.Limits.TAORao != 512_000_000_000 || reviewed.Limits.EVMGasWei != "512000000000000000000" || reviewed.EVMFundingAllocationWei != source.Limits.EVMGasWei || reviewed.PlanHash == source.PlanHash {
		t.Fatal("review lost exact 512 caps, retained funding, or distinct approval")
	}
	// Normalize only the explicit cap/digest/ancestry changes. Every remaining
	// byte of the approved economic and deployment structure must stay exact.
	normalized := *reviewed
	normalized.Limits = source.Limits
	normalized.EVMFundingAllocationWei = source.EVMFundingAllocationWei
	normalized.ResolvedInputsHash = source.ResolvedInputsHash
	normalized.PriorPlanHashes = source.PriorPlanHashes
	normalized.PlanHash = source.PlanHash
	original, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(&normalized)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("allowance review changed an action, funding transfer, fact, release, or custody byte", err)
	}
	if len(reviewed.PriorPlanHashes) != len(source.PriorPlanHashes)+1 || reviewed.PriorPlanHashes[len(reviewed.PriorPlanHashes)-1] != source.PlanHash {
		t.Fatal("review did not bind its exact forward predecessor")
	}
	wire, err := json.Marshal(reviewed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadPlanIdentityBytes(cfg, wire, false); !errors.Is(err, errPersistedPlanIdentityMismatch) || !strings.Contains(err.Error(), "release_lock_hash") {
		t.Fatal("cap review granted strict release acceptance across the hotfix", err)
	}
	if _, err := archiveReviewedSetupPlan(stateDir, reviewed); err != nil {
		t.Fatal(err)
	}
	options.PlanHash = reviewed.PlanHash
	admitted, err := loadInvocationPlan(cfg, stateDir, "setup", options)
	if err != nil || admitted.PlanHash != reviewed.PlanHash {
		t.Fatal("existing exact provisional setup path cannot admit reviewed caps", err)
	}
	active, err := os.ReadFile(filepath.Join(stateDir, "plan.json"))
	if err != nil || !bytes.Equal(active, before) {
		t.Fatal("read-only planning or review changed active approval bytes", err)
	}
}

func TestAllowanceOnlyPlanRejectsNonCeilingAndSourceDrift(t *testing.T) {
	cfg, source, stateDir, _, before := provisionalRuntimePlanFixture(t)
	cfg.MaximumTAORao = 512_000_000_000
	cfg.MaximumEVMGasWei = "512000000000000000000"
	if _, err := buildAllowanceOnlyPlan(t.Context(), cfg, stateDir, common.Hash{0xa1}.Hex()); err == nil {
		t.Fatal("review accepted a different active source hash")
	}
	for _, change := range []struct {
		name   string
		mutate func(*ResolvedConfig)
	}{
		{name: "decreased total", mutate: func(c *ResolvedConfig) { c.MaximumTAORao = source.Limits.TAORao - 1 }},
		{name: "decreased gas", mutate: func(c *ResolvedConfig) { c.MaximumEVMGasWei = "1" }},
		{name: "alpha", mutate: func(c *ResolvedConfig) { c.MaximumAlphaRao++ }},
		{name: "configuration", mutate: func(c *ResolvedConfig) { c.ConfigHash = common.Hash{0xa2}.Hex() }},
		{name: "policy", mutate: func(c *ResolvedConfig) { c.PolicyHash = common.Hash{0xa3}.Hex() }},
		{name: "rpc route", mutate: func(c *ResolvedConfig) { c.OperationalSubstrate = "ws://different-rpc.example:9944" }},
		{name: "authority", mutate: func(c *ResolvedConfig) { c.Authority = "different-authority.example" }},
		{name: "wallet", mutate: func(c *ResolvedConfig) { c.WalletPublic = common.Hash{0xa4}.Hex() }},
		{name: "owned route", mutate: func(c *ResolvedConfig) { c.ownedRPCAuthority = "192.0.2.37:9944" }},
	} {
		candidate := *cfg
		change.mutate(&candidate)
		if _, err := buildAllowanceOnlyPlan(t.Context(), &candidate, stateDir, source.PlanHash); err == nil {
			t.Fatal("allowance review admitted non-ceiling change", change.name)
		}
	}
	var tampered SetupPlan
	if err := json.Unmarshal(before, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Actions[0].Target = "different-custody.example"
	raw, err := json.Marshal(&tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "plan.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildAllowanceOnlyPlan(t.Context(), cfg, stateDir, source.PlanHash); err == nil {
		t.Fatal("action mutation inherited authenticated source authority")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := buildAllowanceOnlyPlan(ctx, cfg, stateDir, source.PlanHash); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled review returned an approval", err)
	}
}

func TestAllowanceOnlyPlanCLIRequiresPinnedReadOnlyReview(t *testing.T) {
	hash := common.Hash{0xa5}.Hex()
	command, options, err := parseCLI([]string{"plan", "--allowance-only", "--plan-hash", hash})
	if err != nil || command != "plan" || !options.AllowanceOnly || options.PlanHash != hash || options.Apply {
		t.Fatal("explicit pinned allowance review is unavailable", command, options, err)
	}
	for _, args := range [][]string{
		{"plan", "--allowance-only"},
		{"plan", "--allowance-only", "--plan-hash", hash, "--apply"},
		{"plan", "--allowance-only", "--plan-hash", hash, "--provisional-resume", "--apply"},
		{"resume", "--allowance-only", "--plan-hash", hash},
		{"plan", "--allowance-only", "--plan-hash", hash, "--detach"},
		{"plan", "--allowance-only", "--plan-hash", hash, "--prepare-only"},
		{"plan", "--allowance-only", "--plan-hash", hash, "--maximum-total-tao", "512"},
	} {
		if _, _, err := parseCLI(args); err == nil {
			t.Fatal("allowance review accepted mutation, missing source, or inline monetary override", args)
		}
	}
}
