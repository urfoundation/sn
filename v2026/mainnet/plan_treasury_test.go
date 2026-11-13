// Current review commands describe the receive-only reserve target while
// retaining historical input semantics and every independent activation gate.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The public account is synthetic and has no supplied signing authority.
func newTreasuryPlanTestFixture(t *testing.T) *planTestFixture {
	t.Helper()
	fixture := newPlanTestFixture(t)
	fixture.config.Schema = bootstrapTreasuryPlanConfigSchema
	fixture.release.Schema = bootstrapTreasuryReleaseInputSchema
	fixture.config.TreasuryDestination = &treasuryDestination{Schema: treasuryDestinationSchema, Profile: "mainnet", Netuid: 25,
		GenesisHash: fixture.config.Network.GenesisHash, AccountId: "0x" + strings.Repeat("61", 32), RecipientHotkeys: []string{}}
	return fixture
}

// Default CLI output cannot direct a new launch back to the retired recycling
// target, and public account input does not manufacture registered recipients.
func TestBootstrapPlanCurrentCommandUsesReceiveOnlyTreasury(t *testing.T) {
	fixture := newTreasuryPlanTestFixture(t)
	path := fixture.rebind(t)
	for _, mode := range [][]string{{"plan", "--outline"}, {"plan", "--config", path}} {
		var first, second, diagnostic bytes.Buffer
		if code := runMain(t.Context(), mode, &first, &diagnostic); code != 0 {
			t.Fatal("current treasury review refused", code, diagnostic.String())
		}
		if code := runMain(t.Context(), mode, &second, &diagnostic); code != 0 || !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatal("current treasury review changed on identical inputs", code, diagnostic.String())
		}
		var plan bootstrapPlan
		if err := json.Unmarshal(first.Bytes(), &plan); err != nil {
			t.Fatal(err)
		}
		if plan.Schema != bootstrapTreasuryPlanSchema || plan.Economics.Remainder != "ordinary-native-treasury" ||
			plan.Economics.ProviderNumerator != 1 || plan.Economics.RemainderNumerator != 9 || plan.Economics.FractionDenominator != 10 ||
			!plan.Economics.ReserveCredit || plan.Economics.Assurance != "observed-native-target" || plan.ApplyAuthority || plan.ActivationReady {
			t.Fatal("current review lost receiving economics or invented authority", plan)
		}
		if err := validateBootstrapDependencies(plan.Requirements, plan.Actions); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, requirement := range plan.Requirements {
			if requirement.Id == "recycle-policy" || requirement.Status != "missing" {
				t.Fatal("new review inherited a recycling gate or manufactured evidence", requirement)
			}
			found = found || requirement.Id == "treasury-policy"
		}
		if !found || len(plan.Requirements) != len(bootstrapRequirements()) || len(plan.Actions) != len(bootstrapActions()) {
			t.Fatal("current review omitted a launch requirement or phase")
		}
		for _, action := range plan.Actions {
			if action.Executable || action.Status != "blocked" || strings.Contains(action.Description, "Recycle mode") {
				t.Fatal("receiving review authorized an action or required owner recycling", action)
			}
		}
		if mode[1] == "--outline" {
			if plan.Status != "unbound_outline" || plan.ContentHash != "" || plan.TreasuryDestination != nil {
				t.Fatal("unbound treasury outline invented an account or content authority")
			}
			continue
		}
		if plan.Status != "blocked" || !reflect.DeepEqual(plan.TreasuryDestination, fixture.config.TreasuryDestination) {
			t.Fatal("bound review lost the exact supplied public destination")
		}
		original := plan.ContentHash
		plan.ContentHash = ""
		raw, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(append([]byte(bootstrapTreasuryPlanSchema+"\x00"), raw...))
		if original != "sha256:"+hex.EncodeToString(digest[:]) {
			t.Fatal("receive-only plan hash does not use its own reproducible domain")
		}
		fixture.config.TreasuryDestination.AccountId = "0x" + strings.Repeat("62", 32)
		changed, err := loadBootstrapPlan(t.Context(), fixture.rebind(t))
		if err != nil || changed.ContentHash == original || changed.TreasuryDestination.AccountId == plan.TreasuryDestination.AccountId {
			t.Fatal("review did not bind the exact native receiving account", err)
		}
	}
}

// Input versions cannot relabel historical intent, and a public receiving
// account never imports spend metadata through the review manifest.
func TestBootstrapPlanTreasuryRejectsSchemaAndDestinationSubstitution(t *testing.T) {
	for _, fault := range []string{"missing-destination", "legacy-config", "legacy-release", "unknown-schema", "destination-schema", "destination-genesis", "destination-netuid", "zero-account", "evm-account", "duplicate-hotkeys", "recycle-requirement", "signatory-field", "device-field"} {
		fixture := newTreasuryPlanTestFixture(t)
		switch fault {
		case "missing-destination":
			fixture.config.TreasuryDestination = nil
		case "legacy-config":
			fixture.config.Schema, fixture.release.Schema = bootstrapPlanConfigSchema, bootstrapReleaseInputSchema
		case "legacy-release":
			fixture.release.Schema = bootstrapReleaseInputSchema
		case "unknown-schema":
			fixture.config.Schema = "urnetwork-mainnet-plan-config-v999"
		case "destination-schema":
			fixture.config.TreasuryDestination.Schema = treasuryCustodySchema
		case "destination-genesis":
			fixture.config.TreasuryDestination.GenesisHash = "0x" + strings.Repeat("63", 32)
		case "destination-netuid":
			fixture.config.TreasuryDestination.Netuid++
		case "zero-account":
			fixture.config.TreasuryDestination.AccountId = "0x" + strings.Repeat("00", 32)
		case "evm-account":
			fixture.config.TreasuryDestination.AccountId = "0x" + strings.Repeat("61", 20)
		case "duplicate-hotkeys":
			fixture.config.TreasuryDestination.RecipientHotkeys = []string{"0x" + strings.Repeat("64", 32), "0x" + strings.Repeat("64", 32)}
		case "recycle-requirement":
			reference := planTestWrite(t, fixture.dir, "recycle.json", map[string]bool{"approved": true})
			fixture.release.ReviewInputs = []planReviewInput{{Requirement: "recycle-policy", planFileReference: reference}}
		}
		path := fixture.rebind(t)
		if fault == "signatory-field" || fault == "device-field" {
			raw, err := json.Marshal(fixture.config)
			if err != nil {
				t.Fatal(fault, err)
			}
			var config map[string]any
			if err := json.Unmarshal(raw, &config); err != nil {
				t.Fatal(fault, err)
			}
			destination := config["treasury_destination"].(map[string]any)
			if fault == "signatory-field" {
				destination["signatories"] = []string{}
			} else {
				destination["device_config"] = map[string]any{}
			}
			planTestWrite(t, fixture.dir, "config.json", config)
		}
		var out, diagnostic bytes.Buffer
		if code := runMain(t.Context(), []string{"plan", "--config", path}, &out, &diagnostic); code == 0 || out.Len() != 0 || diagnostic.Len() == 0 {
			t.Fatal("invalid current review emitted partial authority", fault, code, out.String(), diagnostic.String())
		}
	}
}

// Every supplied review remains unvalidated, including real-looking roster and
// approval claims. Historical inputs still select only their original economy.
func TestBootstrapPlanTreasuryReviewCannotReplaceNativeProofOrLegacyScope(t *testing.T) {
	fixture := newTreasuryPlanTestFixture(t)
	for _, requirement := range bootstrapTreasuryRequirements() {
		reference := planTestWrite(t, fixture.dir, requirement.Id+".json", map[string]bool{"approved": true, "registered": true, "activation_ready": true})
		fixture.release.ReviewInputs = append(fixture.release.ReviewInputs, planReviewInput{Requirement: requirement.Id, planFileReference: reference})
	}
	plan, err := loadBootstrapPlan(t.Context(), fixture.rebind(t))
	if err != nil {
		t.Fatal(err)
	}
	if plan.ApplyAuthority || plan.ActivationReady || plan.Status != "blocked" {
		t.Fatal("opaque treasury review authorized launch")
	}
	for _, requirement := range plan.Requirements {
		if requirement.Status != "supplied_unvalidated" {
			t.Fatal("treasury review input became native proof", requirement)
		}
	}
	for _, action := range plan.Actions {
		if action.Executable || action.Status != "blocked" {
			t.Fatal("treasury review enabled a launch action", action)
		}
	}
	legacy := newPlanTestFixture(t)
	original, err := loadBootstrapPlan(t.Context(), legacy.writeConfig(t))
	if err != nil || original.Schema != bootstrapPlanSchema || original.Economics.Remainder != "owner-recycle" || original.Economics.ReserveCredit || original.TreasuryDestination != nil {
		t.Fatal("historical plan was reinterpreted as a treasury successor", err)
	}
	legacy.release.Schema = bootstrapTreasuryReleaseInputSchema
	if _, err := loadBootstrapPlan(t.Context(), legacy.rebind(t)); err == nil {
		t.Fatal("historical config accepted a receive-only release manifest")
	}
}
