package main

// These tests exercise the production load census independently from the
// large semantic fixture, then compare it with every nested typed locator.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// Keeps each artifact's content distinct, so one adjacent or shared proof
// cannot accidentally satisfy an omitted receipt by URI or content alias.
type finalHistoricalArtifactCensusFixture struct {
	evidence  FinalSemanticEvidence
	artifacts map[string][]byte
}

// Creates a unique, content-addressed non-secret artifact without requiring
// unrelated semantic domains to be valid for this load-boundary unit test.
func (self *finalHistoricalArtifactCensusFixture) artifact(kind, name string) FinalArtifactLocator {
	uri := "final-derived/census/" + name
	data := []byte("census fixture " + uri)
	self.artifacts[uri] = data
	return FinalArtifactLocator{Kind: kind, URI: uri, ContentHash: bytesSHA256(data), SizeBytes: uint64(len(data))}
}

// Includes target-only historical mutations. None of their captured receipt
// proofs is also referenced by an ordinary epoch, pool or exit-criterion row.
func finalHistoricalArtifactCensusTestFixture() *finalHistoricalArtifactCensusFixture {
	value := &finalHistoricalArtifactCensusFixture{artifacts: make(map[string][]byte)}
	evidence := &value.evidence
	evidence.PlanArtifact = value.artifact("setup-plan", "plan.json")
	evidence.ReleaseLockArtifact = value.artifact("release-lock", "release-lock.json")
	evidence.PolicyArtifact = value.artifact("policy", "policy.json")
	evidence.Adversaries.MatrixArtifact = value.artifact("adversarial-matrix", "matrix.json")
	evidence.Adversaries.Artifact = value.artifact("adversarial-campaign", "adversaries.json")
	evidence.ArchiveRetention.Artifact = value.artifact("archive-retention", "retention.json")
	evidence.Topology.MinerManifest = value.artifact("miner-manifest", "miners.json")
	evidence.Topology.BindingManifest = value.artifact("binding-manifest", "bindings.json")
	evidence.ContractCleanup.SupervisorStateArtifact = value.artifact("supervisor-state", "supervisor.json")
	evidence.Deployment.Artifact = value.artifact("contract-deployment", "deployment.json")
	evidence.Reserve.Artifact = value.artifact("reserve", "reserve.json")
	evidence.ValidatorView.Artifact = value.artifact("validator-view", "validator-view.json")
	evidence.FleetRefreshOracleWindow.Artifact = value.artifact("historical-fleet-refresh-oracle-window", "oracle-window.json")
	evidence.HistoricalCoordinatorTimelineArtifact = value.artifact("historical-coordinator-timeline", "timeline.json")
	for _, actionID := range []string{"fleet.refresh.oracle-activate", "fleet.refresh.oracle-restore", "evm.coordinator-upgrade-activate"} {
		evidence.HistoricalCoordinatorReceipts = append(evidence.HistoricalCoordinatorReceipts, FinalHistoricalCoordinatorReceiptEvidence{
			ActionID:              actionID,
			Receipt:               FinalEVMReceipt{Proof: value.artifact("evm-receipt", actionID+"-captured.json")},
			ReceiptArtifact:       value.artifact("historical-coordinator-receipt", actionID+"-envelope.json"),
			PlanArtifact:          value.artifact("historical-setup-plan", "shared-predecessor-plan.json"),
			JournalArtifact:       value.artifact("historical-journal", "shared-journal.jsonl"),
			PostconditionArtifact: value.artifact("historical-action-postcondition", actionID+"-postcondition.json"),
		})
	}
	return value
}

// Derives an independent census from the typed evidence rather than repeating
// the production list. The full release fixture also calls this check, so a
// future nested locator cannot silently escape source loading and hashing.
func assertFinalSemanticArtifactCensus(t *testing.T, evidence *FinalSemanticEvidence, uses []finalSemanticArtifactUse) {
	t.Helper()
	want := make(map[FinalArtifactLocator][]string)
	var walk func(reflect.Value, string)
	walk = func(value reflect.Value, path string) {
		if !value.IsValid() {
			return
		}
		if value.Type() == reflect.TypeFor[FinalArtifactLocator]() {
			locator := value.Interface().(FinalArtifactLocator)
			if locator != (FinalArtifactLocator{}) {
				want[locator] = append(want[locator], path)
			}
			return
		}
		switch value.Kind() {
		case reflect.Interface, reflect.Pointer:
			if !value.IsNil() {
				walk(value.Elem(), path)
			}
		case reflect.Struct:
			for index := 0; index < value.NumField(); index++ {
				if value.Field(index).CanInterface() {
					walk(value.Field(index), path+"."+value.Type().Field(index).Name)
				}
			}
		case reflect.Slice, reflect.Array:
			if value.Type().Elem().Kind() == reflect.Uint8 {
				return
			}
			for index := 0; index < value.Len(); index++ {
				walk(value.Index(index), fmt.Sprintf("%s[%d]", path, index))
			}
		case reflect.Map:
			for _, key := range value.MapKeys() {
				walk(value.MapIndex(key), fmt.Sprintf("%s[%v]", path, key.Interface()))
			}
		}
	}
	walk(reflect.ValueOf(evidence), "evidence")
	seen := make(map[FinalArtifactLocator]int)
	for index, use := range uses {
		if use.locator == (FinalArtifactLocator{}) {
			t.Fatalf("artifact census selected an empty locator at index %d", index)
		}
		if len(want[use.locator]) == 0 {
			t.Fatalf("artifact census selected unreferenced locator %s", use.locator.URI)
		}
		seen[use.locator]++
	}
	for locator, paths := range want {
		if seen[locator] != len(paths) {
			t.Errorf("artifact census selected %s %d times, want all %d references from %v", locator.URI, seen[locator], len(paths), paths)
		}
	}
}

// Exercises selection, real content hashing and cache population with no
// ordinary reference that could accidentally preload a historical proof.
func TestFinalSemanticHistoricalArtifactCensusLoadsEveryCapturedReceiptProof(t *testing.T) {
	value := finalHistoricalArtifactCensusTestFixture()
	uses, err := finalSemanticArtifactUses(&value.evidence)
	if err != nil {
		t.Fatal(err)
	}
	loads := make(map[string]int)
	cache, err := loadFinalSemanticArtifactUses(context.Background(), uses, func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		loads[locator.URI]++
		return value.artifacts[locator.URI], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range value.evidence.HistoricalCoordinatorReceipts {
		proof := row.Receipt.Proof
		if loads[proof.URI] != 1 || !bytes.Equal(cache[proof.URI], value.artifacts[proof.URI]) {
			t.Fatalf("historical %s captured proof was not independently loaded", row.ActionID)
		}
	}
	for uri, count := range loads {
		if count != 1 {
			t.Fatalf("shared artifact %s was loaded %d times", uri, count)
		}
	}
	assertFinalSemanticArtifactCensus(t, &value.evidence, uses)
}

// A withdrawal-only payment has no epoch claim whose proof could preload it.
func TestFinalSemanticHistoricalArtifactCensusLoadsWithdrawalOnlyPayments(t *testing.T) {
	value := finalHistoricalArtifactCensusTestFixture()
	proof := value.artifact("evm-receipt", "withdraw-claim-credit.json")
	value.evidence.ClaimPayments = []FinalClaimPaymentEvidence{{Receipt: FinalEVMReceipt{Proof: proof}}}
	uses, err := finalSemanticArtifactUses(&value.evidence)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := loadFinalSemanticArtifactUses(context.Background(), uses, func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
		return value.artifacts[locator.URI], nil
	})
	if err != nil || !bytes.Equal(cache[proof.URI], value.artifacts[proof.URI]) {
		t.Fatalf("withdrawal-only payment proof was not loaded: %v", err)
	}
	assertFinalSemanticArtifactCensus(t, &value.evidence, uses)
}

// Missing bytes, changed bytes and a conflicting digest on a reused URI must
// all fail before any partially authenticated cache reaches semantic replay.
func TestFinalSemanticHistoricalArtifactCensusRejectsMissingTamperedAndAliasedProofs(t *testing.T) {
	missing := errors.New("captured historical proof is absent")
	for _, mutation := range []string{"missing", "tampered", "conflicting alias"} {
		value := finalHistoricalArtifactCensusTestFixture()
		proof := value.evidence.HistoricalCoordinatorReceipts[0].Receipt.Proof
		uses, err := finalSemanticArtifactUses(&value.evidence)
		if err != nil {
			t.Fatal(err)
		}
		if mutation == "conflicting alias" {
			alias := proof
			alias.ContentHash = bytesSHA256([]byte("another receipt"))
			uses = append(uses, finalSemanticArtifactUse{locator: alias})
		}
		cache, err := loadFinalSemanticArtifactUses(context.Background(), uses, func(_ context.Context, locator FinalArtifactLocator) ([]byte, error) {
			if locator.URI == proof.URI {
				if mutation == "missing" {
					return nil, missing
				}
				if mutation == "tampered" {
					return []byte("substituted captured receipt"), nil
				}
			}
			return value.artifacts[locator.URI], nil
		})
		if err == nil || cache != nil || (mutation == "missing" && !errors.Is(err, missing)) {
			t.Errorf("%s proof did not fail closed: cache=%t err=%v", mutation, cache != nil, err)
		}
	}
}

// Covers the neighboring carried/new fleet write and challenger branches
// without assuming that an ordinary receipt references any of their proofs.
func TestFinalSemanticHistoricalArtifactCensusIncludesCarriedFleetProofs(t *testing.T) {
	value := finalHistoricalArtifactCensusTestFixture()
	version := func(name string) FinalFleetGenerationVersionEvidence {
		return FinalFleetGenerationVersionEvidence{
			Manifest:                value.artifact("fleet-manifest", name+"-manifest.json"),
			Commitment:              value.artifact("fleet-commitment", name+"-commitment.json"),
			CommitmentPostcondition: value.artifact("action-postcondition", name+"-commitment-postcondition.json"),
		}
	}
	write := func(name string) FinalFleetGenerationWriteEvidence {
		return FinalFleetGenerationWriteEvidence{
			Receipt:       FinalEVMReceipt{Proof: value.artifact("evm-receipt", name+"-receipt.json")},
			Postcondition: value.artifact("action-postcondition", name+"-postcondition.json"),
		}
	}
	batchWrite := write("refresh")
	value.evidence.FleetGeneration = &FinalFleetGenerationLineageEvidence{
		Artifact: value.artifact("fleet-generation-lineage", "fleet-lineage.json"),
		Batches: []FinalFleetGenerationBatchEvidence{
			{CarriedHistory: []FinalFleetGenerationWriteEvidence{write("prior-install"), write("prior-binding")}},
			{BatchWrite: &batchWrite, Postcondition: value.artifact("action-postcondition", "batch-postcondition.json")},
		},
		SetupFleets: []FinalFleetGenerationFleetEvidence{{Initial: version("initial"), Refresh: version("refresh")}},
		ChallengerFleets: []FinalFleetGenerationChallengerEvidence{{
			Initial: version("challenger"), Registration: FinalNativeReceipt{Proof: value.artifact("native-receipt", "challenger-registration.json")},
			Transition: value.artifact("fleet-transition", "challenger-transition.json"),
		}},
	}
	uses, err := finalSemanticArtifactUses(&value.evidence)
	if err != nil {
		t.Fatal(err)
	}
	assertFinalSemanticArtifactCensus(t, &value.evidence, uses)
}
