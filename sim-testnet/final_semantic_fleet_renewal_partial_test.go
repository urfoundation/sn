// Partial rounds report actual writes and lease cutoffs, then close only with
// a complete successor. These tests use synthetic receipts and public identities.
package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// One interrupted revocation wave is followed by all complete fleet bindings.
func finalFleetRenewalPartialLineageFixture(t *testing.T) (*FinalSemanticEvidence, *FinalFleetGenerationLineageEvidence) {
	t.Helper()
	evidence, lineage := finalFleetRenewalLineageFixture(t)
	partial := &lineage.Renewals[0].Fleets[4]
	partial.Incomplete = true
	for index := range partial.Members {
		partial.Members[index].Binding = FinalFleetGenerationWriteEvidence{}
		if index >= 2 {
			partial.Members[index].Revocation = nil
		}
	}
	round := FinalFleetRenewalRoundEvidence{Round: 2, SourcePlanHash: lineage.Renewals[0].ApprovedPlanHash, ApprovedPlanHash: finalFleetGenerationTestHash(999101), MetadataHash: finalFleetGenerationTestHash(999102), Approval: finalFleetGenerationTestArtifact("fleet-renewal-approval", "renewal-2"), ValidFromEpoch: 400, ValidToEpoch: 431}
	serial := uint64(2000)
	for _, original := range lineage.Renewals[0].Fleets {
		before := original.Version
		if original.Incomplete {
			var err error
			before, err = verifyFinalIncompleteFleetRenewal(evidence, lineage.Renewals[0], original, *original.Previous)
			if err != nil {
				t.Fatal(err)
			}
		}
		version := finalFleetGenerationTestVersion(original.FleetID, before.Generation+1, 0)
		version.Hotkey = before.Hotkey
		version.CommitmentAction.ActionID = fleetRenewalActionID(round.Round, int(original.FleetID), "commitment", 0)
		version.CommitmentAction.PlanHash = round.ApprovedPlanHash
		version.NativeHead = finalFleetGenerationTestHead(before.NativeHead.Number + 200)
		for index := range version.Members {
			prior := before.Members[index]
			member := &version.Members[index]
			member.ClientID, member.ClientKey, member.FleetKey, member.Hotkey, member.UID = prior.ClientID, prior.ClientKey, prior.FleetKey, version.Hotkey, prior.UID
			member.ValidFromEpoch, member.ValidToEpoch = round.ValidFromEpoch, round.ValidToEpoch
		}
		fleet := FinalFleetRenewalFleetEvidence{FleetID: original.FleetID, Version: version}
		serial++
		fleet.Mirror = finalFleetRenewalABIWrite(t, evidence, fleetRenewalActionID(round.Round, int(fleet.FleetID), "mirror", 0), round.ApprovedPlanHash, "CommitmentMirrored", version, version.Members[0], serial)
		for index, prior := range before.Members {
			serial++
			write := finalFleetRenewalABIWrite(t, evidence, fleetRenewalActionID(round.Round, int(fleet.FleetID), "bind", index+1), round.ApprovedPlanHash, "FleetBound", version, version.Members[index], serial)
			fleet.Members = append(fleet.Members, FinalFleetRenewalMemberEvidence{Member: uint64(index + 1), Prior: prior, Binding: write})
		}
		round.Fleets = append(round.Fleets, fleet)
	}
	lineage.Renewals = append(lineage.Renewals, round)
	return evidence, lineage
}

// The first round has 201 completed fleet versions and two genuine revokes;
// the successor contributes 202 more. Missing writes are never counted as paid.
func TestFinalFleetRenewalPartialRoundPreservesCutoffAndCompletesSuccessor(t *testing.T) {
	t.Parallel()
	evidence, lineage := finalFleetRenewalPartialLineageFixture(t)
	if err := verifyFinalFleetGenerationLineage(evidence, lineage); err != nil {
		t.Fatal(err)
	}
	evidence.FleetGeneration = lineage
	audit, err := finalPublicFleetGenerationAuditForEvidence(evidence)
	if err != nil || audit.RenewalRounds != 2 || audit.RenewedFleetVersions != 403 || audit.IncompleteFleetVersions != 1 || audit.RenewalWrites != 2032 {
		t.Fatalf("partial-round audit %+v: %v", audit, err)
	}
	if err := verifyFinalPublicFleetGenerationAuditShape(audit); err != nil {
		t.Fatal(err)
	}
	for index, member := range lineage.Renewals[1].Fleets[4].Members {
		want := uint64(325)
		if index < 2 {
			want = 317
		}
		if member.Prior.ValidToEpoch != want {
			t.Fatalf("member %d cutoff=%d, want %d", index, member.Prior.ValidToEpoch, want)
		}
	}
}

// Incomplete markers cannot hide an existing binding, erase prior consent, or
// excuse missing terminal renewal work.
func TestFinalFleetRenewalPartialRoundRejectsFabricatedClosure(t *testing.T) {
	t.Parallel()
	evidence, original := finalFleetRenewalPartialLineageFixture(t)
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"terminal", "hidden-binding", "erased-cutoff", "missing-native", "foreign-revoke", "changed-membership", "stale-successor"} {
		var lineage FinalFleetGenerationLineageEvidence
		if err := json.Unmarshal(raw, &lineage); err != nil {
			t.Fatal(err)
		}
		partial := &lineage.Renewals[0].Fleets[4]
		switch fault {
		case "terminal":
			lineage.Renewals = lineage.Renewals[:1]
		case "hidden-binding":
			partial.Members[0].Binding = lineage.Renewals[1].Fleets[4].Members[0].Binding
		case "erased-cutoff":
			partial.Members[0].Revocation = nil
		case "missing-native":
			partial.Version = FinalFleetGenerationVersionEvidence{}
		case "foreign-revoke":
			partial.Members[0].Revocation.Action.PlanHash = "0x" + strings.Repeat("ab", 32)
		case "changed-membership":
			partial.Version.Members[0].UID++
		case "stale-successor":
			lineage.Renewals[1].Fleets[4].Version.NativeHead = partial.Version.NativeHead
		}
		if err := verifyFinalFleetGenerationLineage(evidence, &lineage); err == nil {
			t.Fatalf("partial lineage accepted %s", fault)
		}
	}
}
