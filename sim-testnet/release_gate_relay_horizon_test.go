//go:build linux || darwin

// Exact horizon sources and their real behavioral regressions remain part of
// the executable producer phases. Source edges supplement, never replace,
// authenticated transport, filesystem and funded-runtime tests.
package main

import (
	"strings"
	"testing"
)

// The exact filenames must exist even if a wider glob still finds unrelated
// tests. Removing either executable mode or the new family fails admission.
func TestProducerGateStateSelectionCoversFundedRelayHorizon(t *testing.T) {
	t.Parallel()
	script, groups := releaseEvidenceV2GateFixture(t)
	selected := []releaseEvidenceV2GateGroup{groups[1], groups[0], groups[2]}
	selected[0].packages = []string{"./sim-testnet"}
	selected[0].sources = map[string][]string{"./sim-testnet": releaseEvidenceV2GateSources(t, []string{
		"evidence_relay_horizon_test.go", "evidence_relay_horizon_runtime_test.go",
		"evidence_relay_launch_budget_test.go", "evidence_relay_launch_runtime_test.go",
		"runtime_evidence_launch_config_test.go",
	})}
	selected[1].sources = map[string][]string{"./validator": releaseEvidenceV2GateSources(t, []string{"../validator/release_evidence_publication_discovery_v2_test.go", "../validator/release_evidence_publication_discovery_race_v2_test.go"})}
	selected[2].sources = map[string][]string{"./sim-testnet": releaseEvidenceV2GateSources(t, []string{"release_gate_relay_horizon_test.go"})}
	for _, group := range selected {
		if err := verifyReleaseEvidenceV2GateGroup(script, group); err != nil {
			t.Fatal(err)
		}
		for _, command := range group.commands {
			changed := strings.Replace(script, command, "# omitted: "+command, 1)
			if changed == script || verifyReleaseEvidenceV2GateGroup(changed, group) == nil {
				t.Fatalf("funded horizon coverage survived removed invocation %s", command)
			}
		}
	}
	prior := strings.Replace(script, "|EvidenceRelay|", "|", 1)
	if prior == script || verifyReleaseEvidenceV2GateGroup(prior, selected[0]) == nil {
		t.Fatal("funded relay sources escaped their actual producer phase")
	}
}

// Call ownership cannot drift into a disconnected helper or a fresh source
// anchor. These syntax checks make no claim about execution or call ordering.
func TestProducerGateStateSelectionFundedRelayRetainsAuthenticatedOwners(t *testing.T) {
	t.Parallel()
	for _, edge := range []struct {
		path    string
		caller  string
		callees []string
	}{
		{path: "evidence_relay_campaign.go", caller: "runScenarioWithEvidenceRelay", callees: []string{"validateRuntimeEvidenceSourceCapacity", "newEvidenceRelayRuntime", "WaitReady", "RequirePrepared", "WaitThrough", "WaitAuditPass", "Close", "runScenarioWithProbe"}},
		{path: "evidence_relay_runtime.go", caller: "run", callees: []string{"prepareHorizon", "advance", "advanceDepositAudits", "checkRemaining"}},
		{path: "evidence_relay_runtime.go", caller: "advance", callees: []string{"checkHorizonBlock", "readClosedPublication", "admit", "admitOwnedEvidenceRelayAction"}},
		{path: "evidence_relay_audit.go", caller: "advanceDepositAudits", callees: []string{"checkHorizonBlock", "readAuditPublication", "admit", "admitOwnedEvidenceRelayAction"}},
		{path: "evidence_relay_continuation_budget.go", caller: "admitOwnedEvidenceRelayAction", callees: []string{"readOwnedEvidenceRelayRequest", "admitEvidenceRelayAction"}},
		{path: "evidence_relay_continuation_budget.go", caller: "readOwnedEvidenceRelayRequest", callees: []string{"readValidatorEvidenceHistoricalPlan", "validateEvidenceRelayRequest"}},
		{path: "evidence_relay_horizon_runtime.go", caller: "prepareHorizon", callees: []string{"readHorizonNative", "requireHorizonRemaining", "readAdmittedHorizon", "DiscoverValidatorEvidencePublicationV2Manifests", "DiscoverValidatorEvidenceDepositAuditV2Manifests", "readClosedPublication", "readAuditPublication"}},
		{path: "evidence_relay_horizon_runtime.go", caller: "readHorizonNative", callees: []string{"FinalizedHeadContext", "ReadValidatorScheduleAtContext", "MeetsNonSelfStakeAndPermit"}},
		{path: "evidence_relay_horizon_runtime.go", caller: "readAdmittedHorizon", callees: []string{"Entries", "ReadReleaseEvidenceV2SetupFile", "validateEvidenceRelayRequest", "admit"}},
		{path: "evidence_relay_horizon_runtime.go", caller: "readClosedPublication", callees: []string{"ReleaseEpochStartBlockAtHashContext", "ReleaseEpochEndBlockAtHashContext", "ReadValidatorEvidencePublicationV2"}},
		{path: "evidence_relay_horizon_runtime.go", caller: "readAuditPublication", callees: []string{"ReleaseEpochStartBlockAtHashContext", "ReleaseEpochEndBlockAtHashContext", "ReadValidatorEvidenceDepositAuditV2"}},
		{path: "evidence_relay_horizon_runtime.go", caller: "checkRemaining", callees: []string{"FinalizedBlockContext", "readHorizonNative", "requireHorizonRemaining"}},
		{path: "evidence_relay_horizon_runtime.go", caller: "requireHorizonRemaining", callees: []string{"phaseHorizonRemaining", "requireRemaining"}},
		{path: "evidence_relay_horizon_runtime.go", caller: "phaseHorizonRemaining", callees: []string{"provisionalResumeEnabled", "ceilings"}},
		{path: "evidence_relay_horizon.go", caller: "evidenceRelayConfiguredWork", callees: []string{"scenarioDefinitionFor", "evidenceRelayWatchdogDuration"}},
		{path: "evidence_relay_horizon.go", caller: "evidenceRelayWatchdogDuration", callees: []string{"scenarioTimeout", "fleetLifecycleReleaseScheduleRequired"}},
		{path: "evidence_relay_horizon.go", caller: "ceilings", callees: []string{"extraSubjects", "evidenceRelayMaximumSpan", "checkedAdd"}},
		{path: "../validator/release_evidence_publication_discovery_v2.go", caller: "DiscoverValidatorEvidencePublicationV2Manifests", callees: []string{"discoverValidatorEvidencePublicationV2Manifests"}},
		{path: "../validator/release_evidence_publication_discovery_v2.go", caller: "discoverValidatorEvidencePublicationV2Manifests", callees: []string{"openAttemptPrivateDirectory", "ReadValidatorEvidencePublicationV2Manifest", "stat", "check", "close"}},
		{path: "../validator/release_evidence_deposit_audit_manifest_v2.go", caller: "DiscoverValidatorEvidenceDepositAuditV2Manifests", callees: []string{"discoverValidatorEvidenceDepositAuditV2Manifests"}},
		{path: "../validator/release_evidence_deposit_audit_manifest_v2.go", caller: "discoverValidatorEvidenceDepositAuditV2Manifests", callees: []string{"openAttemptPrivateDirectory", "ReadValidatorEvidenceDepositAuditV2Manifest", "stat", "check", "close"}},
	} {
		calls := releaseClosureFunctionCalls(t, edge.path, edge.caller)
		for _, callee := range edge.callees {
			if !calls[callee] {
				t.Errorf("%s:%s lost required owned call %s", edge.path, edge.caller, callee)
			}
		}
	}
}
