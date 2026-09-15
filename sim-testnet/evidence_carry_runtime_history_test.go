// Synthetic source approvals exercise the real archive and restart readers;
// original runtime provenance never substitutes for current launch authority.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Public artifact literals are independent of the historical admission code.
// Deployment identities, receipt hashes and filesystem custody are synthetic.
func validatorEvidenceRuntime455TestLock(t *testing.T) *ReleaseLock {
	t.Helper()
	lock := testReleaseLockFixture(t)
	lock.Runtime = ReleaseRuntimeLock{
		SourceRepository: "https://github.com/RaoFoundation/subtensor",
		SourceRefKind:    "commit", SourceRefName: "67dcf7f791dc495064c293f080a0702cb433e51e",
		SourceCommit:         "67dcf7f791dc495064c293f080a0702cb433e51e",
		CodeHash:             "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a",
		MetadataHash:         "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc",
		CompressedWasmSHA256: "0x232bfc0d65ec2dbe4280b152e23f13879df9692d2286dd08c6ba14483deee00f",
		SpecVersion:          455, TransactionVersion: 1, StateVersion: 1,
		Image: "runtime.example/synthetic@sha256:" + strings.Repeat("17", 32),
	}
	return lock
}

// Both plans own their source maps and canonical hashes. Synthetic receipt
// references exercise persisted admission, not finalized chain authentication.
type validatorEvidenceHistoricalLockTestFixture struct {
	config       *ResolvedConfig
	original     *SetupPlan
	current      *SetupPlan
	originalWire []byte
	currentWire  []byte
	stateDir     string
}

// The ordinary planner creates each approval before the real carry renderer
// retains the original two intents under the current runtime459 release.
func newValidatorEvidenceHistoricalLockTestFixture(t *testing.T) validatorEvidenceHistoricalLockTestFixture {
	t.Helper()
	return newValidatorEvidenceRuntimeLockTestFixture(t, 455)
}

// Original455 and migrated458 approvals retain their own exact source lock.
func newValidatorEvidenceRuntimeLockTestFixture(t *testing.T, spec uint32) validatorEvidenceHistoricalLockTestFixture {
	t.Helper()
	config := testResolvedConfig(t)
	config.Public.Chain.ConfigIdentityRuntimeSpec = 455
	var err error
	config.ConfigHash, err = releaseConfigHash(config.Config, config.Public, config.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := derivePublicRoles(config)
	if err != nil {
		t.Fatal(err)
	}
	originalConfig := *config
	originalPublic := *config.Public
	originalPublic.Chain.ExpectedRuntimeSpec = spec
	originalPublic.Chain.ConfigIdentityRuntimeSpec = 0
	if spec == 458 || spec == 459 {
		originalPublic.Chain.ConfigIdentityRuntimeSpec = 455
	}
	originalConfig.Public = &originalPublic
	originalConfig.ConfigHash, err = releaseConfigHash(originalConfig.Config, originalConfig.Public, originalConfig.Hyperparameters)
	if err != nil {
		t.Fatal(err)
	}
	originalConfig.Release = validatorEvidenceRuntime455TestLock(t)
	// Build the synthetic original with current planner checks, then bind its
	// independently approved458 source before producing any persisted bytes.
	if spec == 458 || spec == 459 {
		originalConfig = *config
	}
	original, err := buildPlan(&originalConfig, testSetupFacts(), roles, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if spec == 458 || spec == 459 {
		lock := testReleaseLockFixture(t)
		image := lock.Runtime.Image
		lock.Runtime = runtime458ReviewedTestLock().Runtime
		if spec == 459 {
			lock.Runtime = runtime459ReviewedTestLock().Runtime
		}
		lock.Runtime.Image = image
		rebindValidatorEvidenceReleaseLockTest(t, original, lock)
		original.PlanHash, err = original.hash()
		if err != nil {
			t.Fatal(err)
		}
	}
	current, err := buildPlan(config, testSetupFacts(), roles, time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	payloads, err := validatorEvidencePayloadsForPlan(original)
	if err != nil {
		t.Fatal(err)
	}
	receipt := func(actionId, transactionByte string, block uint64) ValidatorEvidenceCarryReceipt {
		return ValidatorEvidenceCarryReceipt{
			PlanHash: original.PlanHash, IntentHash: actionByID(t, original, actionId).IntentHash,
			TransactionHash: "0x" + strings.Repeat(transactionByte, 32), BlockNumber: block,
			BlockHash: "0x" + strings.Repeat("28", 32), PostconditionHash: "0x" + strings.Repeat("39", 32),
		}
	}
	observed := &validatorEvidenceCarryObservation{
		sourcePlan: original, payloads: payloads,
		reference: ValidatorEvidenceCarry{
			Schema: "urnetwork-validator-evidence-carry-v1", SourcePlanHash: original.PlanHash,
			SourceReleaseLockHash: original.ReleaseLockHash,
			Creation:              receipt(validatorEvidenceDeployActionID, "41", 100),
			Anchor:                receipt(validatorEvidenceAnchorActionID, "52", 101),
		},
	}
	current.PriorPlanHashes = []string{original.PlanHash}
	if err := carryValidatorEvidencePlan(current, original, observed); err != nil {
		t.Fatal(err)
	}
	originalWire, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	currentWire, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := atomicWrite(filepath.Join(stateDir, "plans", stringsTrim0x(original.PlanHash)+".json"), originalWire, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(stateDir, "plan.json"), currentWire, 0o600); err != nil {
		t.Fatal(err)
	}
	return validatorEvidenceHistoricalLockTestFixture{
		config: config, original: original, current: current,
		originalWire: originalWire, currentWire: currentWire, stateDir: stateDir,
	}
}

// The actual archived-plan reader retains every approved byte and reconstructs
// the original companion payload without requiring runtime455 to be current.
func TestValidatorEvidenceHistoricalLockReopensOriginalApproval(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceHistoricalLockTestFixture(t)
	reopened, err := readValidatorEvidenceHistoricalPlan(fixture.stateDir, fixture.original.PlanHash)
	if err != nil || reopened == nil {
		t.Fatalf("approved runtime455 archive was refused: %v", err)
	}
	if !reopened.validatorEvidenceHistorical || reopened.PlanHash != fixture.original.PlanHash || !finalJSONEqual(reopened.ValidatorEvidenceSource, fixture.original.ValidatorEvidenceSource) || reopened.MaximumSpend != fixture.original.MaximumSpend || reopened.Limits != fixture.original.Limits || !finalJSONEqual(reopened.Actions, fixture.original.Actions) {
		t.Fatal("historical reader changed original approval, actions or budgets")
	}
	before, err := validatorEvidencePayloadsForPlan(fixture.original)
	if err != nil {
		t.Fatal(err)
	}
	after, err := validatorEvidencePayloadsForPlan(reopened)
	if err != nil || after == nil || before.Manifest != after.Manifest || !bytes.Equal(before.Creation, after.Creation) || !bytes.Equal(before.Runtime, after.Runtime) || !bytes.Equal(before.Anchor, after.Anchor) {
		t.Fatalf("historical replay changed original companion bytes: %v", err)
	}
	wire, err := os.ReadFile(filepath.Join(fixture.stateDir, "plans", stringsTrim0x(fixture.original.PlanHash)+".json"))
	if err != nil || !bytes.Equal(wire, fixture.originalWire) {
		t.Fatal("historical replay rewrote its archived source")
	}
}

// Native setup loads a current approval containing an older carried source.
// The active reader must keep those two release identities distinct.
func TestValidatorEvidenceHistoricalLockRestartsCurrentCarry(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceHistoricalLockTestFixture(t)
	restarted, err := loadPersistedPlan(fixture.config, fixture.stateDir)
	if err != nil || restarted == nil {
		t.Fatalf("current approval could not reopen its runtime455 companion: %v", err)
	}
	if restarted.validatorEvidenceHistorical || restarted.ReleaseLockHash == restarted.ValidatorEvidenceCarry.SourceReleaseLockHash || restarted.ValidatorEvidenceSource.ReleaseLock.Runtime.SpecVersion != 455 || !finalJSONEqual(restarted.ValidatorEvidenceCarry, fixture.current.ValidatorEvidenceCarry) {
		t.Fatal("restart replaced current or original source authority")
	}
	for _, actionId := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID} {
		if !finalJSONEqual(actionByID(t, restarted, actionId), actionByID(t, fixture.original, actionId)) {
			t.Fatalf("restart changed original %s intent or spend", actionId)
		}
	}
	if restarted.MaximumSpend != fixture.current.MaximumSpend || restarted.Limits != fixture.current.Limits {
		t.Fatal("restart changed the current approved budget")
	}
	if err := atomicWrite(filepath.Join(fixture.stateDir, "plan.json"), fixture.originalWire, 0o600); err != nil {
		t.Fatal(err)
	}
	if plan, err := loadPersistedPlan(fixture.config, fixture.stateDir); plan != nil || !errors.Is(err, errPersistedPlanIdentityMismatch) {
		t.Fatalf("original455 approval became current460 execution authority: %v", err)
	}
}

// Every existing source-lineage loader uses the same strict historical reader.
// A valid archived object alone never admits an unapproved ancestor.
func TestValidatorEvidenceHistoricalLockKeepsAdjacentLineageAuthority(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceHistoricalLockTestFixture(t)
	for _, item := range []struct {
		name string
		load func(string, *SetupPlan, string) (*SetupPlan, error)
	}{
		{name: "fleet mirror", load: loadFleetMirrorLineagePlan},
		{name: "native funding", load: loadSubstrateFundingLineagePlan},
		{name: "conviction", load: loadVoluntaryConvictionLineagePlan},
	} {
		plan, err := item.load(fixture.stateDir, fixture.current, fixture.original.PlanHash)
		if err != nil || plan == nil || plan.PlanHash != fixture.original.PlanHash || !finalJSONEqual(plan.ValidatorEvidenceSource, fixture.original.ValidatorEvidenceSource) {
			t.Fatalf("%s lost the approved runtime455 source: %v", item.name, err)
		}
		foreign := *fixture.current
		foreign.PriorPlanHashes = nil
		if plan, err := item.load(fixture.stateDir, &foreign, fixture.original.PlanHash); err == nil || plan != nil {
			t.Fatalf("%s accepted an unapproved original source", item.name)
		}
	}
}

// Both outer-plan and inner-source approval hashes precede historical runtime
// admission. Rehashing only the outer object cannot authenticate a new lock.
func TestValidatorEvidenceHistoricalLockRejectsApprovalDrift(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceHistoricalLockTestFixture(t)
	for _, rehashPlan := range []bool{false, true} {
		var changed SetupPlan
		if err := json.Unmarshal(fixture.currentWire, &changed); err != nil {
			t.Fatal(err)
		}
		changed.ValidatorEvidenceSource.ReleaseLock.Runtime.CodeHash = "0x" + strings.Repeat("63", 32)
		if rehashPlan {
			var err error
			changed.PlanHash, err = changed.hash()
			if err != nil {
				t.Fatal(err)
			}
		}
		wire, err := json.Marshal(&changed)
		if err != nil {
			t.Fatal(err)
		}
		want := "persisted setup plan hash mismatch"
		if rehashPlan {
			want = "validator evidence source release lock differs from its approval"
		}
		if got, err := decodePersistedPlanBytes(wire); err == nil || got != nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("outer rehash=%t accepted changed source approval: %v", rehashPlan, err)
		}
	}
}

// Recomputed approval hashes still cannot authorize a foreign or mixed runtime
// tuple. Every accepted provenance field is checked independently.
func TestValidatorEvidenceHistoricalLockRejectsForeignRuntime(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceHistoricalLockTestFixture(t)
	for _, mutate := range []func(*ReleaseRuntimeLock){
		func(runtime *ReleaseRuntimeLock) { runtime.SourceRepository = "https://source.example/subtensor" },
		func(runtime *ReleaseRuntimeLock) { runtime.SourceTag = "v455" },
		func(runtime *ReleaseRuntimeLock) { runtime.SourceRefKind = "head" },
		func(runtime *ReleaseRuntimeLock) { runtime.SourceRefName = "synthetic-branch" },
		func(runtime *ReleaseRuntimeLock) { runtime.SourceCommit = strings.Repeat("74", 20) },
		func(runtime *ReleaseRuntimeLock) { runtime.SpecVersion = 454 },
		func(runtime *ReleaseRuntimeLock) { runtime.SpecVersion = 456 },
		func(runtime *ReleaseRuntimeLock) { runtime.SpecVersion = 458 },
		func(runtime *ReleaseRuntimeLock) { runtime.TransactionVersion = 2 },
		func(runtime *ReleaseRuntimeLock) { runtime.StateVersion = 0 },
		func(runtime *ReleaseRuntimeLock) { runtime.CodeHash = runtime458ReviewedTestLock().Runtime.CodeHash },
		func(runtime *ReleaseRuntimeLock) {
			runtime.MetadataHash = runtime458ReviewedTestLock().Runtime.MetadataHash
		},
		func(runtime *ReleaseRuntimeLock) { runtime.CompressedWasmSHA256 = "0x" + strings.Repeat("85", 32) },
		func(runtime *ReleaseRuntimeLock) { runtime.UpstreamReleaseCallHash = "0x" + strings.Repeat("96", 32) },
		func(runtime *ReleaseRuntimeLock) { runtime.UpstreamReleaseTimepoint = "123:4" },
	} {
		var changed SetupPlan
		if err := json.Unmarshal(fixture.originalWire, &changed); err != nil {
			t.Fatal(err)
		}
		lock := changed.ValidatorEvidenceSource.ReleaseLock
		mutate(&lock.Runtime)
		rebindValidatorEvidenceReleaseLockTest(t, &changed, lock)
		var err error
		changed.PlanHash, err = changed.hash()
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(&changed)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := decodePersistedPlanBytesForHistory(wire, true); err == nil || got != nil || !strings.Contains(err.Error(), "validator evidence original release lock:") {
			t.Fatalf("reapproved foreign runtime reached historical replay: %v", err)
		}
	}
}

// Sharing structural checks preserves every original source constraint even
// when its runtime identity is legitimately older than the current release.
func TestValidatorEvidenceHistoricalLockPreservesStaticControls(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*ReleaseLock){
		func(lock *ReleaseLock) { lock.SchemaVersion = 2 },
		func(lock *ReleaseLock) { lock.Release = "2.0" },
		func(lock *ReleaseLock) { lock.Runtime.Image = "runtime.example/synthetic:latest" },
		func(lock *ReleaseLock) { lock.Dependencies["redis"] = "redis.example/synthetic:latest" },
		func(lock *ReleaseLock) { delete(lock.EVMBuild, "validator_evidence_artifact_hash") },
		func(lock *ReleaseLock) { lock.EVMBuild["solidity"] = "0.8.25" },
		func(lock *ReleaseLock) { lock.Repositories["operator_proxy_commit"] = "synthetic-branch" },
		func(lock *ReleaseLock) { lock.Interfaces["unreviewed"] = "sha256:" + strings.Repeat("17", 32) },
		func(lock *ReleaseLock) { delete(lock.Infrastructure, "gateway_config_hash") },
	} {
		lock := validatorEvidenceRuntime455TestLock(t)
		mutate(lock)
		if err := validateValidatorEvidenceHistoricalReleaseLock(lock); err == nil {
			t.Fatal("historical runtime admission bypassed a static source constraint")
		}
	}
}

// Fresh lock rendering remains current460-only. Closed anchors reproduce
// their original approval; historical runtime bytes do not replace a new lock.
func TestValidatorEvidenceHistoricalLockKeepsCurrentReleaseAuthority(t *testing.T) {
	t.Parallel()
	current, original := testReleaseLockFixture(t), validatorEvidenceRuntime455TestLock(t)
	if err := validateValidatorEvidenceHistoricalReleaseLock(current); err != nil {
		t.Fatalf("exact current460 archive was refused: %v", err)
	}
	if err := validateReleaseLockStatic(current); err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseLockStatic(original); err == nil {
		t.Fatal("historical455 source became current release authority")
	}
	if raw, err := canonicalReleaseLockBytes(original); err == nil || raw != nil {
		t.Fatal("historical455 source became a current lock rendering")
	}
	fixture := newValidatorEvidenceHistoricalLockTestFixture(t)
	if roots, err := finalReleaseRuntimeRootsForPlan(fixture.original, original); err != nil || len(roots) == 0 {
		t.Fatalf("closed original455 anchor lost its exact approved lock: %v", err)
	}
	if roots, err := finalReleaseRuntimeRootsForPlan(fixture.current, current); err != nil || len(roots) == 0 {
		t.Fatalf("current460 anchor lost its exact approved lock: %v", err)
	}
	if roots, err := finalReleaseRuntimeRootsForPlan(fixture.current, original); err == nil || roots != nil || !strings.Contains(err.Error(), "approved plan does not bind the canonical release lock") {
		t.Fatalf("historical455 lock replaced current460 anchor authority: %v", err)
	}
	for _, version := range []uint32{451, 452, 453, 454, 455} {
		if _, ok := reviewedHistoricalRuntimeArtifact(runtimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: version, TransactionVersion: 1, StateVersion: 1}); !ok {
			t.Fatalf("existing runtime%d historical evidence was lost", version)
		}
	}
}
