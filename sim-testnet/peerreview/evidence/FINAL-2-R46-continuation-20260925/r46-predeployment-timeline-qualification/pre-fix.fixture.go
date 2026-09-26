package main

// Predeployment ancestors retain authenticated planning history without adding
// a proxy, initialization, receipt, or archive query to the coordinator graph.

import (
	"encoding/json"
	"testing"
)

// Authenticates a small historical wire image through the production decoder
// before its hash is retained by the synthetic deployed successor.
func finalHistoricalPredeploymentTestPlan(t *testing.T, current *SetupPlan) *SetupPlan {
	t.Helper()
	action := Action{ID: "subnet.verify-owner", Kind: "substrate-read", Target: "netuid:521"}
	var err error
	action.IntentHash, err = actionIntentHash(action)
	if err != nil {
		t.Fatal(err)
	}
	plan := &SetupPlan{
		Schema: "urnetwork-sim-plan-v1", DeploymentID: current.DeploymentID,
		ChainID: current.ChainID, GenesisHash: current.GenesisHash, Netuid: current.Netuid,
		Actions: []Action{action},
	}
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash, err = persistedSetupPlanHash(data, plan.Schema)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeFinalHistoricalPlanBytes(data)
	if err != nil {
		t.Fatalf("authenticate predeployment fixture: %v", err)
	}
	return decoded
}

// Adding an explicitly retained, unexecuted ancestor must preserve the exact
// two-proxy transition graph and its independent artifact reconstruction.
func TestFinalSemanticHistoricalCoordinatorTimelineAcceptsAuthenticatedPredeployment(t *testing.T) {
	evidence, current, plans, entries, logs, baselines, _ := finalHistoricalCoordinatorTimelineTestFixture(t)
	want, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil {
		t.Fatal(err)
	}
	prior := finalHistoricalPredeploymentTestPlan(t, current)
	current.PriorPlanHashes = append(current.PriorPlanHashes, prior.PlanHash)
	plans[prior.PlanHash] = prior
	got, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil {
		t.Fatalf("authenticated predeployment ancestor was refused: %v", err)
	}
	if !finalHistoricalCoordinatorTimelinesEqual(got.evidence(), want.evidence()) {
		t.Fatal("predeployment ancestor changed the deployed proxy timeline")
	}
	artifact, err := finalHistoricalCoordinatorTimelineArtifactFromSource(want, baselines, logs)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	evidence.HistoricalCoordinatorTimeline = want.evidence()
	if err := verifyFinalHistoricalCoordinatorTimelineArtifact(evidence, current, plans, entries, data); err != nil {
		t.Fatalf("artifact reconstruction refused predeployment ancestor: %v", err)
	}
}
