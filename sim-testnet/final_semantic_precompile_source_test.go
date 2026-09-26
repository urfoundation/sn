// Independent negative controls bind every recovery admission to its original
// signed source, completed step and unique finalized transaction coordinates.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Re-signs only the completion so corrupt approval or action semantics cannot
// be rejected merely because the outer evidence hash was left stale.
func finalPrecompileTestStore(t *testing.T, source *finalHistoricalCoordinatorSource, evidence *PrecompileConformanceEvidence, completion *PrecompileRecoveryCompletion, signer *precompileRecoveryTestFixture, resign bool) {
	t.Helper()
	if resign {
		evidence.EvidenceHash = ""
		var err error
		evidence.EvidenceHash, err = canonicalHashHex(evidence)
		if err != nil {
			t.Fatal(err)
		}
		completion.Record.EvidenceHash = evidence.EvidenceHash
		signer.sign(t, completion.Record, &completion.Hash, &completion.OwnerSignature, &completion.DeployerSignature)
	}
	for name, value := range map[string]any{finalPrecompileConformancePath: evidence, precompileRecoveryCompletionFilename: completion} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		source.archive.files[name] = raw
	}
}

// Valid signatures from another source cannot authorize a relabeled journal;
// even freshly signed completion records must retain valid bounded steps.
func TestFinalPrecompileCaptureRejectsChangedRecoveryAuthority(t *testing.T) {
	for _, fault := range []string{
		"missing evidence", "missing completion", "unsigned owner completion", "unsigned deployer completion",
		"unsigned owner approval", "unsigned deployer approval", "signed approval from another plan",
		"changed current custody", "changed source chain", "changed step", "uncompleted step",
		"changed intent", "changed transaction", "changed block number", "changed block hash", "changed deployment",
		"duplicate finalization", "missing finalization", "missing budget checkpoint", "missing completion checkpoint",
		"unapproved step", "finalized unsigned v1", "static action collision",
	} {
		t.Run(fault, func(t *testing.T) {
			source, evidence, completion, signer := finalPrecompileCaptureFixture(t)
			if err := source.verifyReleaseCaptureCensus(); err != nil {
				t.Fatalf("valid recovery fixture: %v", err)
			}
			finalIndex := -1
			for index, entry := range source.entries {
				if entry.ActionID == evidence.Recovery.Steps[0].Action.ID && entry.Stage == StageFinalized {
					finalIndex = index
					break
				}
			}
			if finalIndex < 0 {
				t.Fatal("fixture has no finalization")
			}
			resign := false
			switch fault {
			case "unsigned owner completion":
				completion.OwnerSignature = completion.DeployerSignature
			case "unsigned deployer completion":
				completion.DeployerSignature = completion.OwnerSignature
			case "unsigned owner approval":
				evidence.Recovery.Authorization.OwnerSignature = evidence.Recovery.Authorization.DeployerSignature
				resign = true
			case "unsigned deployer approval":
				evidence.Recovery.Authorization.DeployerSignature = evidence.Recovery.Authorization.OwnerSignature
				resign = true
			case "signed approval from another plan":
				originalHash := evidence.Recovery.Authorization.Request.PlanHash
				foreign := clonePrecompileProbeSuccessorPlan(t, source.plans[originalHash])
				foreign.PlanHash = finalTestHex(0xda)
				source.current.PriorPlanHashes = []string{foreign.PlanHash}
				source.plans = map[string]*SetupPlan{source.current.PlanHash: source.current, foreign.PlanHash: foreign}
				for index := range source.entries {
					if source.entries[index].PlanHash == originalHash {
						source.entries[index].PlanHash = foreign.PlanHash
					}
				}
			case "changed current custody":
				source.current.Roles.Deployer = common.Address{0xee}.Hex()
			case "changed source chain":
				source.plans[evidence.Recovery.Authorization.Request.PlanHash].ChainID++
			case "changed step":
				evidence.Recovery.Steps[0].Action.Target = common.Address{0xee}.Hex()
				resign = true
			case "uncompleted step":
				evidence.Recovery.Steps[0].Move.TransactionHash = ""
				evidence.Recovery.Steps[0].Move.BlockNumber = 0
				evidence.Recovery.Steps[0].Move.BlockHash = ""
				resign = true
			case "changed intent":
				source.entries[finalIndex].IntentHash = finalTestHex(0xe1)
			case "changed transaction":
				source.entries[finalIndex].TransactionHash = finalTestHex(0xe2)
			case "changed block number":
				source.entries[finalIndex].BlockNumber++
			case "changed block hash":
				source.entries[finalIndex].BlockHash = finalTestHex(0xe3)
			case "changed deployment":
				source.entries[finalIndex].DeploymentID += "-foreign"
			case "duplicate finalization":
				source.entries = append(source.entries, source.entries[finalIndex])
			case "missing finalization":
				source.entries = append(source.entries[:finalIndex], source.entries[finalIndex+1:]...)
			case "missing budget checkpoint", "missing completion checkpoint":
				hash := evidence.Recovery.Authorization.Request.Budget.JournalHash
				if fault == "missing completion checkpoint" {
					hash = completion.Record.JournalHash
				}
				for index := range source.entries {
					if source.entries[index].EntryHash == hash {
						source.entries[index].EntryHash = finalTestHex(0xe4)
					}
				}
			case "unapproved step", "finalized unsigned v1":
				entry := source.entries[finalIndex]
				entry.ActionID = precompileRecoveryActionPrefix + "v2.3"
				if fault == "finalized unsigned v1" {
					entry.ActionID = precompileRecoveryActionPrefix + "1"
					entry.IntentHash = evidence.Recovery.Authorization.Request.GasRevision.Evidence.Recovery.Steps[0].Action.IntentHash
				}
				source.entries = append(source.entries, entry)
			case "static action collision":
				plan := source.plans[evidence.Recovery.Authorization.Request.PlanHash]
				plan.Actions = append(plan.Actions, evidence.Recovery.Steps[0].Action)
			}
			finalPrecompileTestStore(t, source, evidence, completion, signer, resign)
			if fault == "missing evidence" {
				delete(source.archive.files, finalPrecompileConformancePath)
			} else if fault == "missing completion" {
				delete(source.archive.files, precompileRecoveryCompletionFilename)
			}
			if err := source.verifyReleaseCaptureCensus(); err == nil {
				t.Fatal("changed recovery authority was admitted")
			}
			if _, err := source.journalSources(); err == nil {
				t.Fatal("changed recovery authority reached chronology source admission")
			}
		})
	}
}

// The bounded state reader and closed archive rebuild the same two actions;
// neither adds the probe to the release emitter graph or reads arbitrary files.
func TestFinalPrecompileCaptureStateAndArchiveShareExactProof(t *testing.T) {
	source, evidence, _, _ := finalPrecompileCaptureFixture(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "public"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range source.archive.files {
		if err := os.WriteFile(filepath.Join(root, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	live, err := finalHistoricalJournalSourcesFromState(t.Context(), root, source.current, source.plans, source.entries)
	if err != nil {
		t.Fatal(err)
	}
	actions, err := finalHistoricalJournalActions(source.current, source.plans, source.entries, live)
	if err != nil || len(actions) != 2 {
		t.Fatalf("live completed recovery actions: %d, %v", len(actions), err)
	}
	for _, step := range evidence.Recovery.Steps {
		key := evidenceRelayRequestKey{planHash: evidence.Recovery.Authorization.Request.PlanHash, actionId: step.Action.ID}
		if !precompileRecoveryActionEqual(actions[key], step.Action) {
			t.Fatal("live recovery action differs from its signed source")
		}
	}
	census, err := finalCaptureReleaseContractCensusWithSources(source.current, source.deployment, common.HexToAddress(source.chain.FleetBatcher), source.plans, source.entries, live)
	if err != nil {
		t.Fatal(err)
	}
	if finalHistoricalCaptureContains(census.releaseAddresses, common.HexToAddress(evidence.ProbeAddress)) {
		t.Fatal("recovery extended the release emitter graph")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := finalHistoricalJournalSourcesFromState(ctx, root, source.current, source.plans, source.entries); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery capture: %v", err)
	}
}

// The recovery proof remains present in the fleet artifact used by the
// independent timeline verifier, even though these calls target the probe.
func TestFinalPrecompileCaptureTimelineAndArtifactRetainRecoveryProof(t *testing.T) {
	source, conformance, _, _ := finalPrecompileCaptureFixture(t)
	plan := source.plans[conformance.Recovery.Authorization.Request.PlanHash]
	initialAction := testFleetSupersessionAction(t, Action{ID: "evm.coordinator-proxy", Kind: "evm-transaction", Target: plan.Deployment.CoordinatorProxy.Hex()})
	plan.Actions = append(plan.Actions, initialAction)
	initial := JournalEntry{DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ActionID: initialAction.ID, IntentHash: initialAction.IntentHash,
		Stage: StageFinalized, Sequence: 1, TransactionHash: finalTestHex(0xa1), BlockNumber: 100, BlockHash: finalTestHex(0xa2)}
	source.entries = append(source.entries, initial)
	proxy := strings.ToLower(plan.Deployment.CoordinatorProxy.Hex())
	initialHead := ChainHead{Number: initial.BlockNumber, Hash: initial.BlockHash}
	logs := map[string][]finalCanonicalEVMLog{initial.TransactionHash: {finalPayloadTestEvent(t, CoordinatorABI, "Upgraded", proxy, initial.TransactionHash, initialHead, 0, map[string]any{"implementation": plan.Deployment.CoordinatorImplementation})}}
	baselines := []FinalCollectedCoordinatorBaseline{{Proxy: proxy, Head: initialHead,
		Implementation: strings.ToLower(plan.Deployment.CoordinatorImplementation.Hex()), ImplementationRuntimeHash: plan.Deployment.RuntimeHashes[plan.Deployment.CoordinatorImplementation.Hex()],
		ProxyRuntimeHash: plan.Deployment.RuntimeHashes[plan.Deployment.CoordinatorProxy.Hex()]}}
	evidence := &FinalSemanticEvidence{DeploymentID: plan.DeploymentID, PlanHash: source.current.PlanHash, EVMCampaignStartHead: ChainHead{Number: 500, Hash: finalTestHex(0xa3)}}
	sources, err := source.journalSources()
	if err != nil {
		t.Fatal(err)
	}
	targets, err := finalHistoricalCoordinatorJournalActionsWithSources(evidence, source.current, source.plans, source.entries, sources)
	if err != nil || len(targets) != 1 || targets[initial.TransactionHash].action.ID != initialAction.ID {
		t.Fatalf("coordinator action selection lost source admission: %v, %d targets", err, len(targets))
	}
	timeline, err := finalHistoricalCoordinatorBuildTimelineWithSources(evidence, source.current, source.plans, source.entries, logs, baselines, sources)
	if err != nil {
		t.Fatal(err)
	}
	evidence.HistoricalCoordinatorTimeline = timeline.evidence()
	artifact, err := finalHistoricalCoordinatorTimelineArtifactFromSource(timeline, baselines, logs)
	if err != nil {
		t.Fatal(err)
	}
	timelineBytes, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	fleetSource := &finalFleetGenerationSource{archive: source.archive, raw: make(map[string][]byte)}
	if _, err := finalPrecompileRecoveryForJournal(source.current, source.plans, source.entries, fleetSource.record); err != nil {
		t.Fatal(err)
	}
	if len(fleetSource.raw) != 2 {
		t.Fatalf("recovery retained %d proof files", len(fleetSource.raw))
	}
	files := make([]finalFleetGenerationLineageFile, 0, len(fleetSource.raw))
	for name, raw := range fleetSource.raw {
		files = append(files, finalFleetGenerationLineageFile{Path: name, Data: raw, ContentHash: bytesSHA256(raw), SizeBytes: uint64(len(raw))})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	lineage := finalFleetGenerationLineageArtifact{Schema: finalFleetGenerationLineageSchema, DeploymentID: evidence.DeploymentID, PlanHash: evidence.PlanHash, Files: files}
	lineageBytes, err := json.Marshal(lineage)
	if err != nil {
		t.Fatal(err)
	}
	evidence.FleetGeneration = &FinalFleetGenerationLineageEvidence{Artifact: FinalArtifactLocator{URI: "lineage.json"}}
	cache := map[string][]byte{"lineage.json": lineageBytes}
	retained, err := finalHistoricalJournalSourcesFromArtifact(evidence, source.current, source.plans, source.entries, cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorTimelineArtifactWithSources(evidence, source.current, source.plans, source.entries, timelineBytes, retained); err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorTimelineArtifact(evidence, source.current, source.plans, source.entries, timelineBytes); err == nil {
		t.Fatal("timeline accepted a recovery without its retained proof")
	}
	lineage.Files = lineage.Files[:1]
	cache["lineage.json"], err = json.Marshal(lineage)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := finalHistoricalJournalSourcesFromArtifact(evidence, source.current, source.plans, source.entries, cache); err == nil {
		t.Fatal("lineage accepted a missing recovery completion")
	}
}
