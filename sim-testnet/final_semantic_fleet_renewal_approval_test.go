package main

// final_semantic_fleet_renewal_approval_test.go keeps renewal approval recovery
// independent of both predecessor and current operational fingerprints.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestFinalFleetRenewalApprovalRetainsRuntimeFingerprintsAcrossLaterRevision
// reproduces a renewal authored after a release update and archived before a
// second update. Both ordinary and lifecycle proofs must name its exact bytes.
func TestFinalFleetRenewalApprovalRetainsRuntimeFingerprintsAcrossLaterRevision(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	approved, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	fixture.cfg.ConfigHash = finalFleetGenerationTestHash(7301)
	fixture.cfg.ObjectStoreHost = "renewal.objects.example"
	approved, err = bindFleetRenewalRuntimeIdentity(fixture.cfg, approved)
	if err != nil {
		t.Fatal(err)
	}
	if approved.ConfigHash == fixture.base.ConfigHash || approved.ResolvedInputsHash == fixture.base.ResolvedInputsHash {
		t.Fatal("fixture did not reproduce both changed operational fingerprints")
	}
	current := *approved
	current.PriorPlanHashes = append(append([]string(nil), approved.PriorPlanHashes...), approved.PlanHash)
	currentPlan, err := rebindFleetRenewalRuntimePlan(&current, finalFleetGenerationTestHash(7302), finalFleetGenerationTestHash(7303))
	if err != nil {
		t.Fatal(err)
	}
	current = *currentPlan
	source := finalFleetRenewalApprovalTestSource(t, &current, fixture.base, approved)
	baseBytes := append([]byte(nil), source.archive.files[source.planPaths[fixture.base.PlanHash]]...)
	approvedBytes := append([]byte(nil), source.archive.files[source.planPaths[approved.PlanHash]]...)
	if _, err := decodePersistedPlanBytes(approvedBytes); err != nil {
		t.Fatalf("live renewal import rejected its producer's runtime identities: %v", err)
	}
	if _, err := decodeFinalHistoricalPlanBytes(source.archive.files["launch-foundation/plan.json"]); err != nil {
		t.Fatalf("later revision is not a valid archived plan: %v", err)
	}
	round, retained, err := source.renewalApproval(fixture.renewal)
	if err != nil || retained == nil || retained.PlanHash != approved.PlanHash || round.ApprovedPlanHash != approved.PlanHash || round.Approval.ContentHash != bytesSHA256(approvedBytes) {
		t.Fatalf("renewal lost original rebound approval: %v", err)
	}
	retained, err = finalFleetLifecycleRenewalApproval(&current, source.archive.files)
	if err != nil || retained == nil || retained.PlanHash != approved.PlanHash {
		t.Fatalf("lifecycle lost original rebound approval: %v", err)
	}
	if after, err := json.Marshal(fixture.base); err != nil || !bytes.Equal(after, baseBytes) {
		t.Fatalf("reconstruction mutated predecessor bytes: %v", err)
	}
	if after, err := json.Marshal(approved); err != nil || !bytes.Equal(after, approvedBytes) {
		t.Fatalf("reconstruction mutated approved bytes: %v", err)
	}
	delete(source.archive.files, source.planPaths[approved.PlanHash])
	if _, _, err := source.renewalApproval(fixture.renewal); err == nil {
		t.Fatal("generation proof accepted unavailable historical approval bytes")
	}
	if _, err := finalFleetLifecycleRenewalApproval(&current, source.archive.files); err == nil {
		t.Fatal("lifecycle proof accepted unavailable historical approval bytes")
	}
}

// TestFinalFleetRenewalApprovalFingerprintCannotMaskPlanMutation verifies that
// operational recovery preserves every other part of the exact append hash.
func TestFinalFleetRenewalApprovalFingerprintCannotMaskPlanMutation(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	expected, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*SetupPlan)
	}{
		{name: "policy", mutate: func(plan *SetupPlan) { plan.PolicyHash = finalFleetGenerationTestHash(7310) }},
		{name: "rpc authority", mutate: func(plan *SetupPlan) { plan.OwnedRPCAuthority = "unapproved.rpc.example" }},
		{name: "custody", mutate: func(plan *SetupPlan) { plan.Owner = "unapproved-owner" }},
		{name: "allowance", mutate: func(plan *SetupPlan) { plan.Limits.AlphaRao++ }},
		{name: "action", mutate: func(plan *SetupPlan) { plan.Actions[len(plan.Actions)-1].Target = "unapproved-target" }},
		{name: "local action dependencies", mutate: func(plan *SetupPlan) {
			for index := range plan.Actions {
				if plan.Actions[index].ID == "config.render" {
					plan.Actions[index].DependsOn = nil
				}
			}
		}},
		{name: "renewal terms", mutate: func(plan *SetupPlan) { plan.FleetRenewals[0].MaximumFeePerGasWei++ }},
		{name: "lineage", mutate: func(plan *SetupPlan) {
			plan.PriorPlanHashes = append(plan.PriorPlanHashes, finalFleetGenerationTestHash(7311))
		}},
	} {
		var changed SetupPlan
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		changed.ConfigHash, changed.ResolvedInputsHash = finalFleetGenerationTestHash(7312), finalFleetGenerationTestHash(7313)
		test.mutate(&changed)
		changed.PlanHash, err = changed.hash()
		if err != nil {
			t.Fatal(err)
		}
		if matches, err := finalFleetRenewalApprovalMatches(expected, &changed); err != nil || matches {
			t.Fatalf("shared approval matcher accepted changed %s: %v", test.name, err)
		}
	}
}

// TestFinalFleetRenewalApprovalRejectsAmbiguousFingerprintBranches prevents
// archive map order from selecting between two different exact append approvals.
func TestFinalFleetRenewalApprovalRejectsAmbiguousFingerprintBranches(t *testing.T) {
	fixture := newFleetRenewalTestFixture(t)
	first, err := appendFleetRenewalPlan(fixture.base, fixture.renewal)
	if err != nil {
		t.Fatal(err)
	}
	second, err := rebindFleetRenewalRuntimePlan(first, finalFleetGenerationTestHash(7320), first.ResolvedInputsHash)
	if err != nil {
		t.Fatal(err)
	}
	current := *second
	current.PriorPlanHashes = append(append([]string(nil), second.PriorPlanHashes...), first.PlanHash, second.PlanHash)
	current.PlanHash, err = current.hash()
	if err != nil {
		t.Fatal(err)
	}
	source := finalFleetRenewalApprovalTestSource(t, &current, fixture.base, first, second)
	if _, _, err := source.renewalApproval(fixture.renewal); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("generation proof selected an ambiguous append: %v", err)
	}
	if _, err := finalFleetLifecycleRenewalApproval(&current, source.archive.files); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("lifecycle proof selected an ambiguous append: %v", err)
	}
}

// finalFleetRenewalApprovalTestSource seals synthetic plan bytes for both
// finalization readers without introducing live state or an external service.
func finalFleetRenewalApprovalTestSource(t *testing.T, current *SetupPlan, ancestors ...*SetupPlan) *finalFleetGenerationSource {
	t.Helper()
	archive := &finalSemanticArchive{files: map[string][]byte{}, artifactDeriver: func(kind, uri string, raw []byte) (FinalArtifactLocator, error) {
		return FinalArtifactLocator{Kind: kind, URI: uri, ContentHash: bytesSHA256(raw), SizeBytes: uint64(len(raw))}, nil
	}}
	source := &finalFleetGenerationSource{archive: archive, current: current, plans: map[string]*SetupPlan{}, planPaths: map[string]string{}, raw: map[string][]byte{}}
	for _, plan := range append(ancestors, current) {
		path := "plan-history/" + stringsTrim0x(plan.PlanHash) + ".json"
		if plan.PlanHash == current.PlanHash {
			path = "launch-foundation/plan.json"
		}
		raw, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		archive.files[path] = raw
		source.plans[plan.PlanHash], source.planPaths[plan.PlanHash] = plan, path
	}
	return source
}
