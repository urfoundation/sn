//go:build linux || darwin

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
)

type evidenceRelayStartupCacheTestFixture struct {
	runtime     *evidenceRelayRuntime
	entry       *evidenceRelayStartupCacheEntry
	proof       evidenceRelayStartupCacheProof
	entries     []JournalEntry
	inventories map[uint64]evidenceRelayStartupSourceInventory
	requestPath string
	receiptPath string
	closedPaths []string
}

func newEvidenceRelayStartupCacheTestFixture(t *testing.T) *evidenceRelayStartupCacheTestFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	stateDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	planHash := common.Hash{0x41}.Hex()
	plan := &SetupPlan{Schema: "relay-startup-cache-test-plan", PlanHash: planHash, DeploymentID: cfg.Config.Deployment.DeploymentID,
		ConfigHash: cfg.ConfigHash, PolicyHash: cfg.PolicyHash, ReleaseLockHash: common.Hash{0x42}.Hex(), ResolvedInputsHash: common.Hash{0x43}.Hex(),
		ChainID: cfg.ChainID, GenesisHash: cfg.Public.Chain.GenesisHash, Netuid: cfg.Netuid, EvidenceRelayContinuation: &EvidenceRelayContinuation{}}
	cfg.provisionalResume = &provisionalResumeState{RecordPath: filepath.Join(stateDir, "provisional-resumes", "test", "provenance.json"),
		Record: &provisionalResumeRecord{Schema: "urnetwork-sim-provisional-resume-v1", Provisional: true, FinalAcceptance: false,
			DeploymentID: plan.DeploymentID, PlanHash: plan.PlanHash, ConfigHash: plan.ConfigHash, ReleaseLockHash: plan.ReleaseLockHash}}
	actionId := evidenceRelayActionPrefix + strings.Repeat("11", 32)
	intentHash := common.Hash{0x44}.Hex()
	entries := []JournalEntry{{DeploymentID: plan.DeploymentID, PlanHash: planHash, ActionID: actionId, IntentHash: intentHash, Stage: StageIntent, EntryHash: common.Hash{0x45}.Hex()}}
	requestPath := filepath.Join(stateDir, "evidence-relay", stringsTrimRelayPrefix(actionId)+".json")
	receiptPath := filepath.Join(stateDir, "evidence-relay", actionId+".receipt.json")
	if err := os.MkdirAll(filepath.Dir(requestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, value := range map[string][]byte{requestPath: []byte("request\n"), receiptPath: []byte("receipt\n")} {
		if err := os.WriteFile(path, value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sourceDir := filepath.Join(stateDir, "runtime", "validator-1", "coordinator-state-v2")
	closedDir := filepath.Join(sourceDir, "evidence-publications")
	if err := os.MkdirAll(closedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	closedPaths := []string{filepath.Join(closedDir, "epoch-00000000000000000007.json"), filepath.Join(closedDir, "epoch-00000000000000000008.json")}
	for index, path := range closedPaths {
		if err := os.WriteFile(path, []byte{byte(index + 1)}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	requestWitness, err := evidenceRelayStartupWitness(stateDir, requestPath)
	if err != nil {
		t.Fatal(err)
	}
	receiptWitness, err := evidenceRelayStartupWitness(stateDir, receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	closed := make([]evidenceRelayStartupManifest, 0, len(closedPaths))
	closedWitnesses := make([]evidenceRelayStartupFileWitness, 0, len(closedPaths))
	for index, path := range closedPaths {
		witness, err := evidenceRelayStartupWitness(stateDir, path)
		if err != nil {
			t.Fatal(err)
		}
		closedWitnesses = append(closedWitnesses, witness)
		closed = append(closed, evidenceRelayStartupManifest{Kind: "closed", Epoch: uint64(index + 7), IdentitySha256: common.Hash{byte(index + 1)}.Hex(), File: witness})
	}
	source := evidenceRelaySource{validatorId: 1, stateDir: sourceDir, nextEpoch: 7,
		bounds:      validatorcomponent.ReleaseEvidenceV2Bounds{MaxClosureBytes: 4 * 1024 * 1024, MaxHistoryBytes: 16 * 1024 * 1024, MaxParticipants: 1},
		activations: []protocol.ValidatorEvidenceActivation{{Domain: protocol.ValidatorEvidenceActivationDomain{Epoch: 7}}}}
	runtime := &evidenceRelayRuntime{executor: &Executor{cfg: cfg, auditAuthorizedConfig: cfg, plan: plan, stateDir: stateDir},
		sources: []evidenceRelaySource{source}, through: map[uint64]uint64{}, completed: map[uint64]bool{1: false}}
	inventories := map[uint64]evidenceRelayStartupSourceInventory{1: {closed: closedWitnesses}}
	session, err := runtime.newEvidenceRelayStartupSession(t.Context(), entries, inventories)
	if err != nil || session == nil || session.hit || session.entry == nil {
		t.Fatal("cold relay startup cache entry was not prepared", err)
	}
	action := evidenceRelayStartupAction{PlanHash: planHash, ActionId: actionId, IntentHash: intentHash,
		RequestSha256: bytesSHA256([]byte("request\n")), ReceiptSha256: bytesSHA256([]byte("receipt\n")), Request: requestWitness, Receipt: receiptWitness}
	closedRoot, err := canonicalHashHex(closed)
	if err != nil {
		t.Fatal(err)
	}
	auditsRoot, err := canonicalHashHex([]evidenceRelayStartupManifest(nil))
	if err != nil {
		t.Fatal(err)
	}
	sources := []evidenceRelayStartupSourceProof{{ValidatorId: 1, NextEpoch: 9, Through: 8, Completed: true, ClosedRoot: closedRoot, AuditRoot: auditsRoot, Closed: closed}}
	actionRoot, receiptRoot, err := relayStartupActionsRoot([]evidenceRelayStartupAction{action})
	if err != nil {
		t.Fatal(err)
	}
	journalRoot, err := canonicalHashHex(entries)
	if err != nil {
		t.Fatal(err)
	}
	sourceRoot, err := canonicalHashHex(sources)
	if err != nil {
		t.Fatal(err)
	}
	proof := session.entry.fixed
	proof.JournalCount, proof.JournalHash, proof.JournalRoot = 1, entries[0].EntryHash, journalRoot
	proof.ActionRoot, proof.ReceiptRoot, proof.SourceRoot = actionRoot, receiptRoot, sourceRoot
	proof.Actions, proof.Sources = []evidenceRelayStartupAction{action}, sources
	return &evidenceRelayStartupCacheTestFixture{runtime: runtime, entry: session.entry, proof: proof, entries: entries,
		inventories: inventories, requestPath: requestPath, receiptPath: receiptPath, closedPaths: closedPaths}
}

func TestEvidenceRelayStartupCacheColdWriteAndEarliestIncompleteResume(t *testing.T) {
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	if !fixture.entry.save(t.Context(), fixture.proof) {
		t.Fatal("cold successful prefix was not durably cached")
	}
	session, err := fixture.runtime.newEvidenceRelayStartupSession(t.Context(), fixture.entries, fixture.inventories)
	if err != nil || session == nil || !session.hit || fixture.runtime.sources[0].nextEpoch != 9 || fixture.runtime.through[1] != 8 || !fixture.runtime.completed[1] {
		t.Fatalf("exact resume did not restore the earliest incomplete epoch: session=%+v error=%v", session, err)
	}
	if !session.revalidate(fixture.runtime) {
		t.Fatal("unchanged prefix did not survive its second witness check")
	}
	fixture.runtime.startupCache = session
	closed, audits, err := fixture.runtime.readEvidenceRelayStartupManifests(t.Context(), &fixture.runtime.sources[0], fixture.inventories[1])
	if err != nil || len(closed) != 0 || len(audits) != 0 {
		t.Fatal("cache hit opened an invalid prefix manifest object", err)
	}
}

func TestEvidenceRelayStartupCacheChecksSuffixWithoutReusingChangedPrefix(t *testing.T) {
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	if !fixture.runtime.validateEvidenceRelayStartupProof(fixture.proof, fixture.entries, fixture.inventories) {
		t.Fatal("valid startup prefix was rejected")
	}
	suffix := JournalEntry{DeploymentID: fixture.runtime.executor.plan.DeploymentID, PlanHash: fixture.runtime.executor.plan.PlanHash,
		ActionID: evidenceRelayActionPrefix + strings.Repeat("22", 32), IntentHash: common.Hash{0x51}.Hex(), EntryHash: common.Hash{0x52}.Hex()}
	if !fixture.runtime.validateEvidenceRelayStartupProof(fixture.proof, append(append([]JournalEntry(nil), fixture.entries...), suffix), fixture.inventories) {
		t.Fatal("a new action suffix invalidated the authenticated prefix")
	}
	suffix.ActionID = fixture.proof.Actions[0].ActionId
	if fixture.runtime.validateEvidenceRelayStartupProof(fixture.proof, append(append([]JournalEntry(nil), fixture.entries...), suffix), fixture.inventories) {
		t.Fatal("a suffix rewrote an already cached action")
	}
	newPath := filepath.Join(filepath.Dir(fixture.closedPaths[0]), "epoch-00000000000000000009.json")
	if err := os.WriteFile(newPath, []byte("suffix"), 0o600); err != nil {
		t.Fatal(err)
	}
	witness, err := evidenceRelayStartupWitness(fixture.runtime.executor.stateDir, newPath)
	if err != nil {
		t.Fatal(err)
	}
	inventory := fixture.inventories[1]
	inventory.closed = append(inventory.closed, witness)
	fixture.inventories[1] = inventory
	if !fixture.runtime.validateEvidenceRelayStartupProof(fixture.proof, fixture.entries, fixture.inventories) {
		t.Fatal("an ordered manifest suffix invalidated the authenticated prefix")
	}
	fixture.runtime.startupCache = &evidenceRelayStartupSession{hit: true, sourceKVs: map[uint64]*evidenceRelayStartupSourceState{
		1: {proof: fixture.proof.Sources[0]},
	}}
	if _, _, err := fixture.runtime.readEvidenceRelayStartupManifests(t.Context(), &fixture.runtime.sources[0], fixture.inventories[1]); err == nil {
		t.Fatal("manifest suffix was not opened by the ordinary authenticated reader")
	}
}

func TestEvidenceRelayStartupCacheInvalidatesMutationTruncationReorderingIdentityAndUnsafeMode(t *testing.T) {
	tests := []struct {
		name   string
		change func(*evidenceRelayStartupCacheTestFixture) error
	}{
		{"mutation", func(fixture *evidenceRelayStartupCacheTestFixture) error {
			return os.WriteFile(fixture.closedPaths[0], []byte("changed"), 0o600)
		}},
		{"truncation", func(fixture *evidenceRelayStartupCacheTestFixture) error {
			inventory := fixture.inventories[1]
			inventory.closed = inventory.closed[:1]
			fixture.inventories[1] = inventory
			return nil
		}},
		{"reordering", func(fixture *evidenceRelayStartupCacheTestFixture) error {
			inventory := fixture.inventories[1]
			inventory.closed[0], inventory.closed[1] = inventory.closed[1], inventory.closed[0]
			fixture.inventories[1] = inventory
			return nil
		}},
		{"identity drift", func(fixture *evidenceRelayStartupCacheTestFixture) error {
			replacement := fixture.closedPaths[0] + ".replacement"
			if err := os.WriteFile(replacement, []byte{1}, 0o600); err != nil {
				return err
			}
			return os.Rename(replacement, fixture.closedPaths[0])
		}},
		{"missing receipt", func(fixture *evidenceRelayStartupCacheTestFixture) error {
			return os.Remove(fixture.receiptPath)
		}},
		{"unsafe mode", func(fixture *evidenceRelayStartupCacheTestFixture) error {
			return os.Chmod(fixture.requestPath, 0o644)
		}},
	}
	for _, test := range tests {
		fixture := newEvidenceRelayStartupCacheTestFixture(t)
		if err := test.change(fixture); err != nil {
			t.Errorf("%s: %v", test.name, err)
			continue
		}
		if fixture.runtime.validateEvidenceRelayStartupProof(fixture.proof, fixture.entries, fixture.inventories) {
			t.Errorf("%s: changed prefix was accepted", test.name)
		}
	}
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	if !fixture.entry.save(t.Context(), fixture.proof) {
		t.Fatal("seed cache write failed")
	}
	changed := *fixture.entry
	changed.fixed.ContextHash = common.Hash{0x99}.Hex()
	if _, hit := changed.read(t.Context()); hit {
		t.Fatal("a different exact-plan/verifier context reused the cache")
	}
	path := filepath.Join(fixture.entry.stateDir, evidenceRelayStartupCacheDirectoryName, fixture.entry.name)
	wire, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope evidenceRelayStartupCacheEnvelope
	if err := json.Unmarshal(wire, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Mac = strings.Repeat("00", sha256.Size)
	wire, err = json.Marshal(envelope)
	if err != nil || os.WriteFile(path, wire, 0o600) != nil {
		t.Fatal("could not corrupt authenticated cache", err)
	}
	if _, hit := fixture.entry.read(t.Context()); hit {
		t.Fatal("failed cache authentication was accepted")
	}
}

func TestEvidenceRelayStartupCacheCancellationAndPartialFailureDoNotWrite(t *testing.T) {
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if fixture.entry.save(canceled, fixture.proof) {
		t.Fatal("canceled construction wrote a cache")
	}
	partial := &evidenceRelayStartupSession{entry: fixture.entry, dirty: true, sourceKVs: map[uint64]*evidenceRelayStartupSourceState{
		1: {proof: fixture.proof.Sources[0], targetNext: fixture.proof.Sources[0].NextEpoch + 1}}, auditTargetKVs: map[evidenceRelayAuditKey][32]byte{}}
	fixture.runtime.sources[0].nextEpoch = fixture.proof.Sources[0].NextEpoch
	if partial.checkpoint(t.Context(), fixture.runtime, map[evidenceRelayAuditKey][32]byte{}) {
		t.Fatal("partial successful prefix wrote a checkpoint")
	}
	path := filepath.Join(fixture.entry.stateDir, evidenceRelayStartupCacheDirectoryName, fixture.entry.name)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed or canceled construction left a reusable checkpoint", err)
	}
}

func TestEvidenceRelayStartupCacheStrictModeIgnoresPersistedOptimization(t *testing.T) {
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	if !fixture.entry.save(t.Context(), fixture.proof) {
		t.Fatal("seed cache write failed")
	}
	strict := fixture.runtime.executor.cfg.provisionalResume
	fixture.runtime.executor.cfg.provisionalResume = nil
	session, err := fixture.runtime.newEvidenceRelayStartupSession(t.Context(), fixture.entries, fixture.inventories)
	fixture.runtime.executor.cfg.provisionalResume = strict
	if err != nil || session != nil || fixture.runtime.sources[0].nextEpoch != 7 {
		t.Fatal("strict final acceptance consulted the provisional optimization", err)
	}
}

func TestEvidenceRelayStartupCacheRejectsMoreThan1024SlotsBeforeObjectRead(t *testing.T) {
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	directory := filepath.Join(fixture.runtime.sources[0].stateDir, "evidence-publications")
	for index := 0; index < int(evidenceRelayStartupCacheMaximumSlots)+1; index++ {
		path := filepath.Join(directory, "unread-"+strconv.Itoa(index))
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fixture.runtime.evidenceRelayStartupInventories(t.Context(), evidenceRelayStartupCacheMaximumSlots); err == nil || !strings.Contains(err.Error(), "slots exceed") {
		t.Fatal("oversized manifest census reached historical object reads", err)
	}
}

func TestEvidenceRelayStartupCacheRejectsConcurrentJournalAppend(t *testing.T) {
	fixture := newEvidenceRelayStartupCacheTestFixture(t)
	journal, err := OpenJournal(fixture.runtime.executor.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	first := fixture.entries[0]
	first.EntryHash = ""
	if err := journal.Append(first); err != nil {
		t.Fatal(err)
	}
	fixture.runtime.executor.journal = journal
	fixture.runtime.sources[0].nextEpoch = fixture.proof.Sources[0].NextEpoch
	session := &evidenceRelayStartupSession{entry: fixture.entry, hit: true, dirty: true,
		actionKVs:      map[string]evidenceRelayStartupAction{fixture.proof.Actions[0].ActionId: fixture.proof.Actions[0]},
		actionDraftKVs: map[string]evidenceRelayStartupActionDraft{}, sourceKVs: map[uint64]*evidenceRelayStartupSourceState{
			1: {proof: fixture.proof.Sources[0], targetNext: fixture.proof.Sources[0].NextEpoch}}, auditTargetKVs: map[evidenceRelayAuditKey][32]byte{}}
	var appendErr error
	session.beforeJournalRecheck = func() {
		appendErr = journal.Append(JournalEntry{DeploymentID: first.DeploymentID, PlanHash: first.PlanHash,
			ActionID: evidenceRelayActionPrefix + strings.Repeat("33", 32), IntentHash: common.Hash{0x61}.Hex(), Stage: StageIntent})
	}
	if session.checkpoint(t.Context(), fixture.runtime, map[evidenceRelayAuditKey][32]byte{}) {
		t.Fatal("concurrent journal append was included in a stale construction")
	}
	if appendErr != nil {
		t.Fatal(appendErr)
	}
	path := filepath.Join(fixture.entry.stateDir, evidenceRelayStartupCacheDirectoryName, fixture.entry.name)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("concurrent journal append published a startup checkpoint", err)
	}
}

func TestEvidenceRelayStartupCacheKeepsNewSendAdmissionSelectors(t *testing.T) {
	raw, err := os.ReadFile("evidence_relay_runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func (self *evidenceRelayRuntime) advance() error")
	end := strings.Index(source, "func (self *evidenceRelayRuntime) retainResult")
	if start < 0 || end <= start {
		t.Fatal("relay runtime send boundary is absent")
	}
	source = source[start:end]
	selectors := []string{"admitOwnedEvidenceRelayAction(self.ctx, expected)", "relayValidatorEvidenceTransaction(self.ctx", "retainOwnedResult(ownerPlanHash, action, result)", "startupCache.rememberAction(ownerPlanHash, action"}
	previous := -1
	for _, selector := range selectors {
		index := strings.Index(source, selector)
		if index < 0 {
			t.Fatalf("relay startup optimization removed normal new-send selector %q", selector)
		}
		if index <= previous {
			t.Fatalf("relay startup checkpoint precedes normal admission/send/durable receipt at %q", selector)
		}
		previous = index
	}
}

func TestEvidenceRelayStartupCacheKeepsFreshHeadAndScheduleSelectors(t *testing.T) {
	raw, err := os.ReadFile("evidence_relay_horizon_runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func (self *evidenceRelayRuntime) prepareHorizon() error")
	end := strings.Index(source, "func (self *evidenceRelayRuntime) readAdmittedHorizon")
	if start < 0 || end <= start {
		t.Fatal("relay startup admission boundary is absent")
	}
	source = source[start:end]
	cache := strings.Index(source, "newEvidenceRelayStartupSession(self.ctx")
	firstEvm := strings.Index(source, "self.chain.FinalizedBlockContext(self.ctx)")
	lastEvm := strings.LastIndex(source, "self.chain.FinalizedBlockContext(self.ctx)")
	firstNative := strings.Index(source, "evidenceRelayNativeCurrentHead)")
	lastNative := strings.LastIndex(source, "evidenceRelayNativeCurrentHead)")
	if cache < 0 || firstEvm < 0 || firstEvm >= cache || lastEvm <= cache || firstNative < 0 || firstNative >= cache || lastNative <= cache {
		t.Fatal("relay startup cache is not bracketed by fresh finalized Evm/native validator-schedule reads")
	}
}
