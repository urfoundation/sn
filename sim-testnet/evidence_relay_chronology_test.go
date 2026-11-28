//go:build linux || darwin

// Real admission bytes and journal records join the existing coordinator
// transition, foundation, and fleet-lineage decoders without verdict doubles.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// Uses the real approved plan and signed admission writer, plus exact raw
// constructor logs for that plan's generated coordinator implementation.
type evidenceRelayChronologyTestFixture struct {
	executor  *Executor
	action    Action
	original  []byte
	evidence  *FinalSemanticEvidence
	initial   JournalEntry
	logs      map[string][]finalCanonicalEVMLog
	baselines []FinalCollectedCoordinatorBaseline
}

// Admission follows the constructor and precedes the finalized relay row.
func newEvidenceRelayChronologyTestFixture(t *testing.T) *evidenceRelayChronologyTestFixture {
	t.Helper()
	executor, expected := evidenceRelayAdmissionTestFixture(t)
	constructor, err := exactPlanActionByID(executor.plan, "evm.coordinator-proxy")
	if err != nil {
		t.Fatal(err)
	}
	initial := JournalEntry{DeploymentID: executor.plan.DeploymentID, PlanHash: executor.plan.PlanHash, ActionID: constructor.ID, IntentHash: constructor.IntentHash,
		Stage: StageFinalized, TransactionHash: common.Hash{0xa1}.Hex(), BlockNumber: 10, BlockHash: common.Hash{0xa2}.Hex()}
	if err := executor.journal.Append(initial); err != nil {
		t.Fatal(err)
	}
	initial = executor.journal.Entries()[0]
	action, err := executor.admitEvidenceRelayAction(t.Context(), expected)
	if err != nil {
		t.Fatal(err)
	}
	finalized := JournalEntry{DeploymentID: executor.plan.DeploymentID, PlanHash: executor.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash,
		Stage: StageFinalized, TransactionHash: common.Hash{0xa3}.Hex(), BlockNumber: 450, BlockHash: common.Hash{0xa4}.Hex()}
	if err := executor.journal.Append(finalized); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(executor.stateDir, "evidence-relay", strings.TrimPrefix(action.ID, evidenceRelayActionPrefix)+".json")
	original, err := validatorpkg.ReadReleaseEvidenceV2SetupFile(t.Context(), path, evidenceRelayActionBytes)
	if err != nil {
		t.Fatal(err)
	}
	post, err := finalHistoricalCoordinatorTransitionPostIdentity(executor.plan, true)
	if err != nil {
		t.Fatal(err)
	}
	proxy := strings.ToLower(executor.plan.Deployment.CoordinatorProxy.Hex())
	log := finalCanonicalEVMLog{Address: proxy, Topics: []string{strings.ToLower(crypto.Keccak256Hash([]byte("Upgraded(address)")).Hex()), common.BytesToHash(common.HexToAddress(post.Implementation).Bytes()).Hex()}, Data: "0x",
		TransactionHash: initial.TransactionHash, BlockNumber: initial.BlockNumber, BlockHash: initial.BlockHash}
	baseline := FinalCollectedCoordinatorBaseline{Proxy: proxy, Head: ChainHead{Number: initial.BlockNumber, Hash: initial.BlockHash}, Implementation: post.Implementation,
		ImplementationRuntimeHash: post.RuntimeHash, ProxyRuntimeHash: strings.ToLower(executor.plan.Deployment.RuntimeHashes[executor.plan.Deployment.CoordinatorProxy.Hex()])}
	if err := finalVerifyHistoricalCoordinatorBaseline(baseline); err != nil {
		t.Fatal(err)
	}
	return &evidenceRelayChronologyTestFixture{executor: executor, action: action, original: original, initial: initial,
		evidence: &FinalSemanticEvidence{DeploymentID: executor.plan.DeploymentID, PlanHash: executor.plan.PlanHash, ChainID: executor.plan.ChainID, Netuid: executor.plan.Netuid, EVMCampaignStartHead: ChainHead{Number: 500, Hash: common.Hash{0xa5}.Hex()}},
		logs:     map[string][]finalCanonicalEVMLog{initial.TransactionHash: {log}}, baselines: []FinalCollectedCoordinatorBaseline{baseline}}
}

// A low native height must neither enter nor poison actual coordinator history.
func TestEvidenceRelayChronologyNativeHeightKeepsActualTransitions(t *testing.T) {
	evidence, current, plans, entries, logs, baselines, _ := finalHistoricalCoordinatorTimelineTestFixture(t)
	before, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := finalHistoricalCoordinatorJournalActions(evidence, current, plans, entries)
	if err != nil {
		t.Fatal(err)
	}
	old := plans[current.PriorPlanHashes[0]]
	native := Action{ID: "native.actual-commitment", Kind: "substrate-extrinsic", Target: old.Deployment.CoordinatorProxy.Hex()}
	native.IntentHash, err = actionIntentHash(native)
	if err != nil {
		t.Fatal(err)
	}
	old.Actions = append(old.Actions, native)
	entries = append(entries, JournalEntry{DeploymentID: old.DeploymentID, PlanHash: old.PlanHash, ActionID: native.ID, IntentHash: native.IntentHash,
		Stage: StageFinalized, TransactionHash: finalTestHex(0xb1), BlockNumber: 1, BlockHash: finalTestHex(0xb2)})
	after, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, entries, logs, baselines)
	if err != nil || !finalHistoricalCoordinatorTimelinesEqual(before.evidence(), after.evidence()) {
		t.Fatal("native height changed real coordinator transitions", err)
	}
	actualTargets, err := finalHistoricalCoordinatorJournalActions(evidence, current, plans, entries)
	if err != nil || !reflect.DeepEqual(targets, actualTargets) {
		t.Fatal("native height entered coordinator target census", err)
	}
}

// Invalid source identity fails even when a kind or numeric height would make
// an unauthenticated row look irrelevant to this chain's transition stream.
func TestEvidenceRelayChronologyRejectsNativeIntentBeforeHeightClassification(t *testing.T) {
	evidence, current, plans, entries, logs, baselines, _ := finalHistoricalCoordinatorTimelineTestFixture(t)
	native := Action{ID: "native.actual-commitment", Kind: "substrate-extrinsic", Target: "native-hotkey"}
	var err error
	native.IntentHash, err = actionIntentHash(native)
	if err != nil {
		t.Fatal(err)
	}
	current.Actions = append(current.Actions, native)
	for _, height := range []uint64{0, 1, evidence.EVMCampaignStartHead.Number, evidence.EVMCampaignStartHead.Number + 1} {
		invalid := JournalEntry{DeploymentID: current.DeploymentID, PlanHash: current.PlanHash, ActionID: native.ID, IntentHash: finalTestHex(0xb3), Stage: StageFinalized, BlockNumber: height}
		candidate := append(append([]JournalEntry(nil), entries...), invalid)
		if _, err := finalHistoricalCoordinatorJournalActions(evidence, current, plans, candidate); err == nil {
			t.Fatalf("invalid native source survived target classification at %d", height)
		}
		if _, err := finalHistoricalCoordinatorBuildTimeline(evidence, current, plans, candidate, logs, baselines); err == nil {
			t.Fatalf("invalid native source survived timeline classification at %d", height)
		}
	}
}

// The original dual-signed relay source is admitted but does not invent a
// coordinator transition; both legacy wrappers stay strict without its bytes.
func TestEvidenceRelayChronologyOriginalRequestPreservesConstructorAndOfflineTimeline(t *testing.T) {
	fixture := newEvidenceRelayChronologyTestFixture(t)
	plan := fixture.executor.plan
	plans := map[string]*SetupPlan{plan.PlanHash: plan}
	entries := fixture.executor.journal.Entries()
	key := evidenceRelayRequestKey{planHash: plan.PlanHash, actionId: fixture.action.ID}
	requests := map[evidenceRelayRequestKey][]byte{key: fixture.original}
	before, err := finalHistoricalCoordinatorBuildTimeline(fixture.evidence, plan, plans, []JournalEntry{fixture.initial}, fixture.logs, fixture.baselines)
	if err != nil {
		t.Fatal(err)
	}
	after, err := finalHistoricalCoordinatorBuildTimelineWithRelayRequests(fixture.evidence, plan, plans, entries, fixture.logs, fixture.baselines, requests)
	if err != nil || !finalHistoricalCoordinatorTimelinesEqual(before.evidence(), after.evidence()) {
		t.Fatal("original relay request changed constructor chronology", err)
	}
	wantTargets, err := finalHistoricalCoordinatorJournalActions(fixture.evidence, plan, plans, []JournalEntry{fixture.initial})
	if err != nil {
		t.Fatal(err)
	}
	targets, err := finalHistoricalCoordinatorJournalActionsWithRelayRequests(fixture.evidence, plan, plans, entries, requests)
	if err != nil || !reflect.DeepEqual(wantTargets, targets) {
		t.Fatal("relay created a coordinator target", err)
	}
	if _, err := finalHistoricalCoordinatorJournalActions(fixture.evidence, plan, plans, entries); err == nil {
		t.Fatal("legacy target wrapper guessed a dynamic action")
	}
	if _, err := finalHistoricalCoordinatorBuildTimeline(fixture.evidence, plan, plans, entries, fixture.logs, fixture.baselines); err == nil {
		t.Fatal("legacy timeline wrapper guessed a dynamic action")
	}
	fixture.evidence.HistoricalCoordinatorTimeline = after.evidence()
	artifact, err := finalHistoricalCoordinatorTimelineArtifactFromSource(after, fixture.baselines, fixture.logs)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyFinalHistoricalCoordinatorTimelineArtifactWithRelayRequests(fixture.evidence, plan, plans, entries, raw, requests); err != nil {
		t.Fatal("independent raw-log timeline rejected original relay bytes", err)
	}
	if err := verifyFinalHistoricalCoordinatorTimelineArtifact(fixture.evidence, plan, plans, entries, raw); err == nil {
		t.Fatal("legacy artifact wrapper guessed a dynamic action")
	}
	changedLog := fixture.logs[fixture.initial.TransactionHash][0]
	changedLog.Topics = append([]string(nil), changedLog.Topics...)
	changedLog.Topics[1] = common.BytesToHash(common.Address{0xb4}.Bytes()).Hex()
	changedLogs := map[string][]finalCanonicalEVMLog{fixture.initial.TransactionHash: {changedLog}}
	if _, err := finalHistoricalCoordinatorBuildTimelineWithRelayRequests(fixture.evidence, plan, plans, entries, changedLogs, fixture.baselines, requests); err == nil {
		t.Fatal("relay admission waived the exact coordinator implementation transition")
	}
}

// Uses the actual retained current/predecessor plan bytes and journal. The
// production fleet constructor must retain original requests before any write.
func TestEvidenceRelayChronologyPredecessorRequestsRemainInFleetArtifact(t *testing.T) {
	fixture := newEvidenceRelayChronologyTestFixture(t)
	old := fixture.executor.plan
	current := *old
	current.PriorPlanHashes = []string{old.PlanHash}
	var err error
	current.PlanHash, err = current.hash()
	if err != nil {
		t.Fatal(err)
	}
	fixture.evidence.PlanHash = current.PlanHash
	oldBytes, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	currentBytes, err := json.Marshal(&current)
	if err != nil {
		t.Fatal(err)
	}
	journalBytes, err := os.ReadFile(filepath.Join(fixture.executor.stateDir, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	key := evidenceRelayRequestKey{planHash: old.PlanHash, actionId: fixture.action.ID}
	name, err := finalRelayRequestFoundationPath(key)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"launch-foundation/plan.json": currentBytes, "launch-foundation/journal.jsonl": journalBytes,
		"plan-history/" + stringsTrim0x(old.PlanHash) + ".json": oldBytes, name: fixture.original}
	batcher, _, err := finalPlanFleetBatcher(&current)
	if err != nil {
		t.Fatal(err)
	}
	archive := &finalSemanticArchive{files: files}
	chain := &FinalCollectedChainSnapshot{FleetBatcher: strings.ToLower(batcher.Hex())}
	source, err := newFinalFleetGenerationSource(archive, fixture.evidence, chain, &finalSemanticEventIndex{})
	if err != nil || !bytes.Equal(source.raw[name], fixture.original) {
		t.Fatal("fleet lineage lost original predecessor relay bytes", err)
	}
	plans := map[string]*SetupPlan{current.PlanHash: &current, old.PlanHash: old}
	entries := fixture.executor.journal.Entries()
	lineage := finalFleetGenerationLineageArtifact{Schema: finalFleetGenerationLineageSchema, DeploymentID: current.DeploymentID, PlanHash: current.PlanHash}
	names := make([]string, 0, len(source.raw))
	for path := range source.raw {
		names = append(names, path)
	}
	sort.Strings(names)
	for _, path := range names {
		raw := source.raw[path]
		lineage.Files = append(lineage.Files, finalFleetGenerationLineageFile{Path: path, ContentHash: bytesSHA256(raw), SizeBytes: uint64(len(raw)), Data: bytes.Clone(raw)})
	}
	raw, err := json.Marshal(lineage)
	if err != nil {
		t.Fatal(err)
	}
	fixture.evidence.FleetGeneration = &FinalFleetGenerationLineageEvidence{Artifact: FinalArtifactLocator{Kind: "fleet-generation-lineage", URI: "fleet-generation-lineage.json", ContentHash: bytesSHA256(raw), SizeBytes: uint64(len(raw))}}
	requests, err := finalRelayRequestsFromArtifact(fixture.evidence, plans, entries, map[string][]byte{"fleet-generation-lineage.json": raw})
	if err != nil || !bytes.Equal(requests[key], fixture.original) {
		t.Fatal("independent lineage decoder lost predecessor request", err)
	}
	for _, changed := range []map[string][]byte{
		{"launch-foundation/plan.json": currentBytes, "launch-foundation/journal.jsonl": journalBytes, "plan-history/" + stringsTrim0x(old.PlanHash) + ".json": oldBytes},
		{"launch-foundation/plan.json": currentBytes, "launch-foundation/journal.jsonl": journalBytes, "plan-history/" + stringsTrim0x(old.PlanHash) + ".json": oldBytes, name: append(bytes.Clone(fixture.original), '\n')},
	} {
		if _, err := newFinalFleetGenerationSource(&finalSemanticArchive{files: changed}, fixture.evidence, chain, &finalSemanticEventIndex{}); err == nil {
			t.Fatal("fleet source accepted missing or changed predecessor request")
		}
	}
	for _, omit := range []bool{true, false} {
		changed := lineage
		changed.Files = nil
		for _, item := range lineage.Files {
			if item.Path == name {
				if omit {
					continue
				}
				item.Data = append(bytes.Clone(item.Data), '\n')
				item.ContentHash = bytesSHA256(item.Data)
				item.SizeBytes = uint64(len(item.Data))
			}
			changed.Files = append(changed.Files, item)
		}
		encoded, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := finalRelayRequestsFromArtifact(fixture.evidence, plans, entries, map[string][]byte{"fleet-generation-lineage.json": encoded}); err == nil {
			t.Fatal("independent lineage accepted a rehashed missing or changed original request")
		}
	}
}

// Archive source census consumes original requests rather than the mutable
// state directory; a missing, changed or unreferenced namespace fails closed.
func TestEvidenceRelayChronologyArchiveCensusRequiresExactRequestNamespace(t *testing.T) {
	fixture := newEvidenceRelayChronologyTestFixture(t)
	plan := fixture.executor.plan
	plans := map[string]*SetupPlan{plan.PlanHash: plan}
	entries := fixture.executor.journal.Entries()
	key := evidenceRelayRequestKey{planHash: plan.PlanHash, actionId: fixture.action.ID}
	name, err := finalRelayRequestFoundationPath(key)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{name: bytes.Clone(fixture.original)}
	deployment := plan.Deployment
	deployment.DeployBlock = 100
	batcher, _, err := finalPlanFleetBatcher(plan)
	if err != nil {
		t.Fatal(err)
	}
	census, err := finalCaptureReleaseContractCensusWithRelayRequests(plan, &deployment, batcher, plans, entries, map[evidenceRelayRequestKey][]byte{key: fixture.original})
	if err != nil {
		t.Fatal(err)
	}
	source := &finalHistoricalCoordinatorSource{archive: &finalSemanticArchive{files: files}, current: plan, deployment: &deployment, plans: plans, entries: entries,
		chain: &FinalCollectedChainSnapshot{FleetBatcher: strings.ToLower(batcher.Hex()), EVMFromBlock: census.fromBlock, CurrentReleaseFromBlock: deployment.DeployBlock, CurrentReleaseAddresses: census.currentAddresses, ReleaseContractAddresses: census.releaseAddresses}}
	if err := source.verifyReleaseCaptureCensus(); err != nil {
		t.Fatal("archive could not replay original capture census", err)
	}
	for _, changed := range []map[string][]byte{nil, {name: append(bytes.Clone(fixture.original), '\n')}, {name: fixture.original, "launch-foundation/evidence-relay/unreferenced.json": fixture.original}, {name: fixture.original, "launch-foundation/evidence-relay": fixture.original}} {
		source.archive.files = changed
		if err := source.verifyReleaseCaptureCensus(); err == nil {
			t.Fatal("archive accepted missing, changed or unreferenced request source")
		}
	}
}

// The actual original writer and journal drive foundation capture, including
// a failed request that has no successful publication or receipt to select it.
func TestEvidenceRelayChronologyFoundationRetainsFailedOriginalAndCancellation(t *testing.T) {
	executor, action, original := evidenceRelayRequestTestFixture(t)
	if err := executor.journal.Append(JournalEntry{DeploymentID: executor.plan.DeploymentID, PlanHash: executor.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageFailed, Error: "retained failure"}); err != nil {
		t.Fatal(err)
	}
	foundation, err := finalCollectedNamedEntries(executor.stateDir, []string{"plan.json", "journal.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	retained, err := captureFinalRelayFoundationEntries(t.Context(), executor.stateDir, foundation)
	if err != nil || len(retained) != 1 || !bytes.Equal(retained[0].Data, original) {
		t.Fatal("failed admission lost its original foundation bytes", err)
	}
	if retained[0].Path != "evidence-relay/"+strings.TrimPrefix(action.ID, evidenceRelayActionPrefix)+".json" {
		t.Fatal("foundation request changed namespaces")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := captureFinalRelayFoundationEntries(ctx, filepath.Join(executor.stateDir, "absent"), foundation); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled original capture reached state reads", err)
	}
	runRoot := filepath.Join(t.TempDir(), "not-published")
	if _, _, _, _, err := captureFinalSemanticClosedInputsContext(ctx, "absent", runRoot, nil, nil, nil, 1, 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled collector did not preserve cancellation", err)
	}
	if _, err := os.Stat(runRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled collector published a partial source", err)
	}
}
