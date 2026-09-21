//go:build linux || darwin

package main

// The actual approval builder, original signed requests and durable journal
// exercise expansion; directory fixtures preserve the two-member slot cost.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/protocol"
)

// Start from a real authenticated 1024-slot predecessor with a failed original
// 100-gwei liability. The expansion must charge that liability at its old price.
func evidenceRelayExpansionRequestTest(t *testing.T, executor *Executor) EvidenceRelayContinuation {
	t.Helper()
	current := evidenceRelayRefreshRequestTest(t, executor)
	current.Schema = evidenceRelayContinuationExpansionSchema
	var err error
	current.NewSlots, current.HistoricalLiabilityWei, err = current.remainingSlots()
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func TestEvidenceRelayExpansionBinds2048SlotsAndCumulativeMoney(t *testing.T) {
	fixture, executor := newEvidenceRelayRefreshTest(t)
	before, err := json.Marshal(executor.plan)
	if err != nil {
		t.Fatal(err)
	}
	current := evidenceRelayExpansionRequestTest(t, executor)
	plan, err := appendEvidenceRelayContinuationPlan(executor.plan, current)
	if err != nil {
		t.Fatal(err)
	}
	reserve, err := exactPlanActionByID(plan, evidenceRelayReserveId)
	if err != nil {
		t.Fatal(err)
	}
	gas, fee, slots, err := evidenceRelayPlanAllowance(plan, reserve)
	if err != nil || gas != 1_000_000 || fee != 25_000_000_000 || slots != 2048 || reserve.Spend.EVMGasWei != "51200000000000000000" || current.NewSlots != 2044 || current.HistoricalLiabilityWei != "100000000000000000" {
		t.Fatalf("expanded exact monetary terms differ: gas=%d fee=%d slots=%d reserve=%s remaining=%d liability=%s error=%v", gas, fee, slots, reserve.Spend.EVMGasWei, current.NewSlots, current.HistoricalLiabilityWei, err)
	}
	if !reflect.DeepEqual(current.OriginalReserve, executor.plan.EvidenceRelayContinuation.OriginalReserve) {
		t.Fatal("expansion rewrote the original monetary approval")
	}
	if match, err := equalSpend(plan.Limits, executor.plan.Limits); err != nil || !match {
		t.Fatal("expansion silently raised configured external limits", err)
	}
	oldCampaign, _ := exactPlanActionByID(executor.plan, "campaign.evm-gas-reserve")
	newCampaign, _ := exactPlanActionByID(plan, "campaign.evm-gas-reserve")
	delta, err := subtractDecimalUint(oldCampaign.Spend.EVMGasWei, newCampaign.Spend.EVMGasWei)
	if err != nil || delta != "25600000000000000000" {
		t.Fatalf("fungible campaign allocation did not fund the exact extra reserve: delta=%s error=%v", delta, err)
	}
	after, err := json.Marshal(executor.plan)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("pure expansion changed the predecessor", err)
	}
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, plan, fixture.roles); err != nil {
		t.Fatal(err)
	}
	if err := validateEvidenceRelayContinuationSource(fixture.stateDir, plan, executor.journal.Entries()); err != nil {
		t.Fatal("expanded source lost exact original ancestry", err)
	}
	executor.plan = plan
	request := evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], fixture.prepared.Epoch+1, false, 0)
	action, owner, err := executor.admitOwnedEvidenceRelayAction(t.Context(), request)
	if err != nil || owner != plan.PlanHash || action.Spend.EVMGasWei != "25000000000000000" {
		t.Fatal("approved expansion did not admit at its unchanged per-call ceiling", err)
	}
	_, maximum, err := executor.evidenceRelayAdmissionEntries()
	if err != nil || maximum != 2044 {
		t.Fatal("admission refunded the old higher-fee liability", maximum, err)
	}
	if err := validatePlanBudget(plan); err != nil {
		t.Fatal("strict acceptance rejected the fully bound expanded plan", err)
	}
}

func TestEvidenceRelayExpansionRefreshCannotDoubleAgainOrRefundDebits(t *testing.T) {
	fixture, executor := newEvidenceRelayRefreshTest(t)
	current := evidenceRelayExpansionRequestTest(t, executor)
	plan, err := appendEvidenceRelayContinuationPlan(executor.plan, current)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRunInputs(fixture.cfg, fixture.stateDir, plan, fixture.roles); err != nil {
		t.Fatal(err)
	}
	executor.plan = plan
	request := evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], fixture.prepared.Epoch+1, false, 0)
	if _, _, err := executor.admitOwnedEvidenceRelayAction(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	next := evidenceRelayExpansionRequestTest(t, executor)
	refreshed, err := appendEvidenceRelayContinuationPlan(plan, next)
	if err != nil || next.NewSlots != 2043 || len(next.Debits) != 2 || next.HistoricalLiabilityWei != "125000000000000000" {
		t.Fatal("refresh lost cumulative liabilities or repeated the expansion", next.NewSlots, next.HistoricalLiabilityWei, err)
	}
	if refreshed.EvidenceRelayContinuation.OriginalReserve.Spend.EVMGasWei != "25600000000000000000" {
		t.Fatal("refresh replaced immutable original reserve")
	}
	for _, mutate := range []func(*EvidenceRelayContinuation){
		func(c *EvidenceRelayContinuation) { c.NewSlots++ },
		func(c *EvidenceRelayContinuation) { c.Debits = c.Debits[1:] },
		func(c *EvidenceRelayContinuation) { c.Schema = evidenceRelayContinuationRefreshSchema },
	} {
		candidate := next
		mutate(&candidate)
		if _, err := appendEvidenceRelayContinuationPlan(plan, candidate); err == nil {
			t.Fatal("refresh accepted restored money, missing debit, or capacity downgrade")
		}
	}
}

func TestEvidenceRelayExpansionStrictValidationRejectsForgedTerms(t *testing.T) {
	_, executor := newEvidenceRelayRefreshTest(t)
	current := evidenceRelayExpansionRequestTest(t, executor)
	plan, err := appendEvidenceRelayContinuationPlan(executor.plan, current)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*SetupPlan){
		func(p *SetupPlan) { p.EvidenceRelayContinuation.NewSlots = 2048 },
		func(p *SetupPlan) {
			p.EvidenceRelayContinuation.OriginalReserve.Spend.EVMGasWei = "51200000000000000000"
		},
		func(p *SetupPlan) { p.EvidenceRelayContinuation.Schema = evidenceRelayContinuationRefreshSchema },
		func(p *SetupPlan) { p.Limits.EVMGasWei = "51200000000000000000" },
		func(p *SetupPlan) {
			for i := range p.Actions {
				if p.Actions[i].ID == evidenceRelayReserveId {
					p.Actions[i].Parameters["maximum_slots"] = "2049"
					p.Actions[i].IntentHash, _ = actionIntentHash(p.Actions[i])
				}
			}
		},
		func(p *SetupPlan) {
			for i := range p.Actions {
				if p.Actions[i].ID == evidenceRelayReserveId {
					p.Actions[i].Spend.EVMGasWei = "25600000000000000000"
					p.Actions[i].IntentHash, _ = actionIntentHash(p.Actions[i])
				}
			}
		},
	} {
		var candidate SetupPlan
		if err := json.Unmarshal(raw, &candidate); err != nil {
			t.Fatal(err)
		}
		mutate(&candidate)
		candidate.PlanHash, err = candidate.hash()
		if err != nil {
			t.Fatal(err)
		}
		if err := validatePlanBudget(&candidate); err == nil {
			t.Fatal("strict validation accepted forged allowance, aggregate spend, or original identity")
		}
	}
}

func TestEvidenceRelayExpansionAdmissionRetainsOriginalConfigAcrossBudgetRevision(t *testing.T) {
	fixture, executor := newEvidenceRelayRefreshTest(t)
	current := evidenceRelayExpansionRequestTest(t, executor)
	plan, err := appendEvidenceRelayContinuationPlan(executor.plan, current)
	if err != nil {
		t.Fatal(err)
	}
	oldConfig := current.ConfigHash
	// A separate approved revision has a new configuration identity while the
	// original relay dimensions and immutable continuation retain their owner.
	plan.PriorPlanHashes = append(plan.PriorPlanHashes, plan.PlanHash)
	fixture.cfg.ConfigHash = common.Hash{0xf1}.Hex()
	plan.ConfigHash = fixture.cfg.ConfigHash
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	executor.plan = plan
	next := evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], fixture.prepared.Epoch+1, false, 0)
	if _, _, err := executor.admitOwnedEvidenceRelayAction(t.Context(), next); err != nil {
		t.Fatal("budget-only config revision stranded authenticated relay funding", err)
	}
	if plan.EvidenceRelayContinuation.ConfigHash != oldConfig {
		t.Fatal("current admission rewrote historical configuration identity")
	}
	fixture.cfg.Config.ValidatorEvidenceRelay.MaxSlots++
	next = evidenceRelayLaunchRequestTest(t, fixture, fixture.prepared.Members[0], fixture.prepared.Epoch+2, false, 0)
	if _, _, err := executor.admitOwnedEvidenceRelayAction(t.Context(), next); err == nil {
		t.Fatal("changed configured reserve gained current monetary authority")
	}
}

func TestEvidenceRelayExpansionCaptureRequiresExplicit2048Approval(t *testing.T) {
	base := &SetupPlan{EvidenceRelayContinuation: &EvidenceRelayContinuation{Schema: evidenceRelayContinuationRefreshSchema}}
	for _, check := range []struct {
		slots uint64
		pin   *EvidenceRelayContinuation
		want  string
	}{
		{slots: 0, want: evidenceRelayContinuationRefreshSchema},
		{slots: 2048, want: evidenceRelayContinuationExpansionSchema},
		{pin: &EvidenceRelayContinuation{Schema: evidenceRelayContinuationExpansionSchema}, want: evidenceRelayContinuationExpansionSchema},
		{slots: 1024}, {slots: 2049}, {slots: ^uint64(0)},
		{slots: 2048, pin: &EvidenceRelayContinuation{Schema: evidenceRelayContinuationExpansionSchema}},
	} {
		got, err := evidenceRelayContinuationCaptureSchema(base, check.slots, check.pin)
		if check.want == "" && err == nil || check.want != "" && (err != nil || got != check.want) {
			t.Fatalf("capture slots=%d pin=%+v schema=%s error=%v", check.slots, check.pin, got, err)
		}
	}
	if _, err := evidenceRelayContinuationCaptureSchema(&SetupPlan{}, 2048, nil); err == nil {
		t.Fatal("unrelated plan acquired expanded original-custody authority")
	}
	base.EvidenceRelayContinuation.Schema = evidenceRelayContinuationExpansionSchema
	if got, err := evidenceRelayContinuationCaptureSchema(base, 0, nil); err != nil || got != evidenceRelayContinuationExpansionSchema {
		t.Fatal("ordinary recovery reset approved expanded capacity", got, err)
	}
	for _, options := range []cliOptions{
		{RelaySlots: 2048, RelayEndBlock: 100},
		{RelaySlots: 2049, RelayEndBlock: 100},
		{RelaySlots: 2048, RelayEndBlock: 100, ProvisionalResume: true},
		{RelaySlots: 2048, RelayContinuationPlan: "/approval.example.json"},
	} {
		err := validateEvidenceRelayContinuationOptions("relay-continuation", options)
		valid := options.RelaySlots == 2048 && !options.ProvisionalResume && options.RelayContinuationPlan == ""
		if valid != (err == nil) {
			t.Fatalf("incorrect option admission: %+v error=%v", options, err)
		}
	}
	if err := validateEvidenceRelayContinuationOptions("resume", cliOptions{RelaySlots: 2048}); err == nil {
		t.Fatal("ordinary resume implicitly expanded allowance")
	}
}

// Locator files deliberately contain non-JSON bytes: inventory must be counted
// before payload/history calls. Each source has two configured member slots.
func evidenceRelayExpandedInventoryTest(t *testing.T) *evidenceRelayRuntime {
	t.Helper()
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	runtime := fixture.runtime
	for _, path := range fixture.closedPaths {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	template := runtime.sources[0]
	runtime.sources = nil
	for id := uint64(1); id <= 2; id++ {
		source := template
		source.validatorId = id
		source.stateDir = filepath.Join(runtime.executor.stateDir, "runtime", fmt.Sprintf("validator-%d", id), "coordinator-state-v2")
		source.activations = []protocol.ValidatorEvidenceActivation{{NoID: 1}, {NoID: 2}}
		for _, kind := range []string{"evidence-publications", "evidence-deposit-audits"} {
			dir := filepath.Join(source.stateDir, kind)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			count := 271
			if kind == "evidence-deposit-audits" {
				count = 0
				if id == 2 {
					count = 3
				}
			}
			for index := 0; index < count; index++ {
				path := filepath.Join(dir, fmt.Sprintf("unread-%04d.json", index))
				if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		runtime.sources = append(runtime.sources, source)
	}
	return runtime
}

func TestEvidenceRelayExpansionInventories1090MemberSlotsWithin2048(t *testing.T) {
	runtime := evidenceRelayExpandedInventoryTest(t)
	inventory, err := runtime.evidenceRelayStartupInventories(t.Context(), 2048)
	if err != nil {
		t.Fatal("expanded approval rejected retained bounded inventory", err)
	}
	var files, slots int
	for _, source := range runtime.sources {
		items := inventory[source.validatorId]
		count := len(items.closed) + len(items.audits)
		files += count
		slots += count * len(source.activations)
	}
	if files != 545 || slots != 1090 || evidenceRelayStartupDirectoryPageEntries >= 271 {
		t.Fatalf("inventory lost receipts or failed to page: files=%d slots=%d page=%d", files, slots, evidenceRelayStartupDirectoryPageEntries)
	}
	if _, err := runtime.evidenceRelayStartupInventories(t.Context(), 1024); err == nil || !strings.Contains(err.Error(), "slots exceed") {
		t.Fatal("old approval gained expanded monetary authority", err)
	}
	if _, err := runtime.evidenceRelayStartupInventories(t.Context(), 2049); err == nil {
		t.Fatal("process safety bound became unbounded")
	}
	runtime.sources[1].bounds.MaxHistoryBytes = 270
	if _, err := runtime.evidenceRelayStartupInventories(t.Context(), 2048); err == nil || !strings.Contains(err.Error(), "bounded private file") {
		t.Fatal("slot expansion also widened byte bounds", err)
	}
}

func TestEvidenceRelayExpansionInventoryRejectsActual2049thSlot(t *testing.T) {
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	directory := filepath.Dir(fixture.closedPaths[0])
	for index := 2; index < 2049; index++ {
		if err := os.WriteFile(filepath.Join(directory, fmt.Sprintf("unread-%04d.json", index)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.runtime.evidenceRelayStartupInventories(t.Context(), 2048); err == nil || !strings.Contains(err.Error(), "slots exceed") {
		t.Fatal("expanded directory census reached out-of-budget payload reads", err)
	}
}
