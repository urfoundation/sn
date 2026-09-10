// Staging admission reopens actual approved setup files. Counterexamples use
// the real journal writer so its hash chain cannot substitute for plan intent.
package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// The fixture sends the genuine generated constructor, then retains the real
// canonical-hash code/getter postcondition through the ordinary executor.
func runtimeReservedCreationFixtureTest(t *testing.T) (*ResolvedConfig, string, *runtimeReservedRenderTestFixture) {
	t.Helper()
	cfg := testResolvedConfig(t)
	cfg.OperationalRPCMode = rpcModePublicOverride
	cfg.Public.Chain.EVMPublicReadEndpoint = "https://test.chain.opentensor.ai"
	budget, err := requiredRuntimeClientKeyUploadBudget(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Config.Artifacts.AttemptUpload = &budget
	stateDir := t.TempDir()
	configureRuntimeEvidenceV2Test(t, cfg, stateDir)
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, stateDir, prepareRuntimeReservedRenderTest(t, cfg, stateDir, roles)
}

// Independent owned copies retain original source bytes; only the requested
// journal rows are rebuilt with the production sequence/hash-chain writer.
func runtimeReservedCreationStateTest(t *testing.T, source string, entries []JournalEntry) string {
	t.Helper()
	destination := t.TempDir()
	if err := os.Chmod(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDir(destination); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "journal.jsonl" || relative == "deployment.lock" {
			return nil
		}
		if !entry.Type().IsRegular() {
			t.Fatalf("original fixture has a non-regular retained source: %s", relative)
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return atomicWrite(filepath.Join(destination, relative), value, 0o600)
	}); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(destination)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err := journal.Append(entry); err != nil {
			t.Fatalf("counterexample must first pass actual journal admission: %v", err)
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if retained, err := readJournalEntries(destination); err != nil || len(retained) != len(entries) {
		t.Fatalf("counterexample did not survive actual journal reopening: %v", err)
	}
	return destination
}

// Reopening the actual completed source is read-only and never sends or asks
// a current provider to invent a substitute historical creation observation.
func TestRuntimeEvidenceV2ReservedCreationRetainsOriginalSourceOnRestart(t *testing.T) {
	t.Parallel()
	cfg, stateDir, fixture := runtimeReservedCreationFixtureTest(t)
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	calls := fixture.transport.rpcCalls.Load()
	for range 2 {
		values, err := runtimeReservedAttemptUploads(cfg, stateDir, &fixture.deployment)
		if err != nil || !reflect.DeepEqual(values, cfg.Config.Artifacts.ReservedAttemptUploads) {
			t.Fatalf("original approved CREATE did not survive staging restart: %v", err)
		}
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, stateDir)) || fixture.transport.rpcCalls.Load() != calls || fixture.transport.sendCalls.Load() != 1 {
		t.Fatal("staging restart rewrote original evidence or performed new network work")
	}
}

// A valid journal chain can be authored for a foreign intent or deployment.
// Neither becomes an approved CREATE merely by naming its action and height.
func TestRuntimeEvidenceV2ReservedCreationRejectsForgedIntentAndForeignDeployment(t *testing.T) {
	t.Parallel()
	cfg, stateDir, fixture := runtimeReservedCreationFixtureTest(t)
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"intent", "deployment"} {
		candidate := slices.Clone(entries)
		for index := range candidate {
			if fault == "intent" {
				candidate[index].IntentHash = common.Hash{0xd1}.Hex()
			} else {
				candidate[index].DeploymentID += "-foreign"
			}
		}
		other := runtimeReservedCreationStateTest(t, stateDir, candidate)
		before := validatorNamespaceTreeSnapshot(t, other)
		if _, err := runtimeReservedAttemptUploads(cfg, other, &fixture.deployment); err == nil || !strings.Contains(err.Error(), "competing approved action history") {
			t.Fatalf("%s journal chain became approved companion authority: %v", fault, err)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, other)) {
			t.Fatalf("%s refusal rewrote the original evidence", fault)
		}
	}
}

// Every current source is required: a finalized stage is not a signature, a
// verified label is not its retained postcondition, and bytes are hash-bound.
func TestRuntimeEvidenceV2ReservedCreationRejectsMissingOrChangedSources(t *testing.T) {
	t.Parallel()
	cfg, stateDir, fixture := runtimeReservedCreationFixtureTest(t)
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"broadcast", "broadcast-signer", "broadcast-nonce", "verified", "transaction", "postcondition", "immutable-source"} {
		candidate := slices.Clone(entries)
		candidate = slices.DeleteFunc(candidate, func(entry JournalEntry) bool {
			return fault == "broadcast" && entry.Stage == StageBroadcast || fault == "verified" && entry.Stage == StageVerified
		})
		for index := range candidate {
			if candidate[index].Stage == StageBroadcast {
				if fault == "broadcast-signer" {
					candidate[index].Signer = common.Address{0xd6}.Hex()
				} else if fault == "broadcast-nonce" {
					candidate[index].Nonce = "0"
				}
			}
		}
		other := runtimeReservedCreationStateTest(t, stateDir, candidate)
		switch fault {
		case "transaction":
			path := filepath.Join(other, "transactions", stringsTrim0x(fixture.creation.TransactionHash)+".rlp")
			if err := atomicWrite(path, []byte{0x80}, 0o600); err != nil {
				t.Fatal(err)
			}
		case "postcondition":
			for _, entry := range candidate {
				if entry.Stage == StageVerified {
					if err := os.Rename(filepath.Join(other, entry.PostconditionPath), filepath.Join(other, entry.PostconditionPath)+".withheld"); err != nil {
						t.Fatal(err)
					}
				}
			}
		case "immutable-source":
			for index, entry := range candidate {
				if entry.Stage != StageVerified {
					continue
				}
				record, err := readValidatorEvidenceSourcePostcondition(other, cfg, fixture.plan, entry)
				if err != nil {
					t.Fatal(err)
				}
				record.Observed["runtime_hash"], record.IndependentObserved["runtime_hash"] = common.Hash{0xd2}.Hex(), common.Hash{0xd2}.Hex()
				writer := &Executor{stateDir: other}
				path, hash, err := writer.persistActionPostcondition(record)
				if err != nil {
					t.Fatal(err)
				}
				candidate[index].PostconditionPath, candidate[index].PostconditionHash = path, hash
			}
			other = runtimeReservedCreationStateTest(t, other, candidate)
		}
		before := validatorNamespaceTreeSnapshot(t, other)
		if _, err := runtimeReservedAttemptUploads(cfg, other, &fixture.deployment); err == nil {
			t.Fatalf("%s missing or changed source self-authorized staging", fault)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, other)) {
			t.Fatalf("%s refusal changed retained state", fault)
		}
	}
}

// Conflicting final observations fail even at the same height. An earlier
// nonfinal inclusion may move during a reorg without changing the final owner.
func TestRuntimeEvidenceV2ReservedCreationSeparatesReorgFromFinalConflict(t *testing.T) {
	t.Parallel()
	cfg, stateDir, fixture := runtimeReservedCreationFixtureTest(t)
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"earlier-inclusion", "duplicate-finalized", "finalized-hash", "finalized-height"} {
		candidate := make([]JournalEntry, 0, len(entries)+1)
		for _, entry := range entries {
			if fault == "earlier-inclusion" && entry.Stage == StageIncluded {
				entry.BlockHash = common.Hash{0xd3}.Hex()
			}
			if fault == "finalized-hash" && entry.Stage == StageFinalized {
				entry.BlockHash = common.Hash{0xd4}.Hex()
			}
			if fault == "finalized-height" && entry.Stage == StageFinalized {
				entry.BlockNumber++
			}
			candidate = append(candidate, entry)
			if fault == "duplicate-finalized" && entry.Stage == StageFinalized {
				entry.BlockHash = common.Hash{0xd5}.Hex()
				candidate = append(candidate, entry)
			}
		}
		other := runtimeReservedCreationStateTest(t, stateDir, candidate)
		_, err := runtimeReservedAttemptUploads(cfg, other, &fixture.deployment)
		if fault == "earlier-inclusion" && err != nil || fault != "earlier-inclusion" && err == nil {
			t.Fatalf("%s changed the original canonical finalization rule: %v", fault, err)
		}
	}
}

// The carry is built by actual historical code/receipt/immutable reads. Its
// hash-approved shape alone cannot replace a missing or changed original.
func TestRuntimeEvidenceV2ReservedCreationRequiresOriginalPredecessorCarry(t *testing.T) {
	t.Parallel()
	fixture := newValidatorEvidenceCarryTestFixture(t, true)
	current := validatorEvidenceHistoricalSuccessorTest(t, fixture)
	executor := fixture.executor
	before := validatorNamespaceTreeSnapshot(t, executor.stateDir)
	calls := fixture.rpc.calls.Load()
	creation, err := runtimeReservedAttemptUploadCreation(executor.cfg, executor.stateDir, current)
	if err != nil || creation != current.ValidatorEvidenceCarry.Creation {
		t.Fatalf("actual authenticated predecessor source was not retained: %v", err)
	}
	if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, executor.stateDir)) || fixture.rpc.calls.Load() != calls || fixture.rpc.sends.Load() != 0 {
		t.Fatal("retained carry admission rewrote or replaced original history")
	}
	for _, fault := range []string{"missing-plan", "changed-plan", "missing-journal", "changed-reference", "unapproved-predecessor"} {
		candidate, carry := *current, *current.ValidatorEvidenceCarry
		candidate.ValidatorEvidenceCarry = &carry
		entries := executor.journal.Entries()
		if fault == "missing-journal" {
			entries = nil
		}
		other := runtimeReservedCreationStateTest(t, executor.stateDir, entries)
		switch fault {
		case "missing-plan":
			path := filepath.Join(other, "plans", stringsTrim0x(carry.SourcePlanHash)+".json")
			if err := os.Rename(path, path+".withheld"); err != nil {
				t.Fatal(err)
			}
		case "changed-plan":
			path := filepath.Join(other, "plans", stringsTrim0x(carry.SourcePlanHash)+".json")
			if err := atomicWrite(path, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		case "changed-reference":
			carry.Creation.BlockNumber++
		case "unapproved-predecessor":
			candidate.ValidatorEvidenceCarry = nil
		}
		before := validatorNamespaceTreeSnapshot(t, other)
		if _, err := runtimeReservedAttemptUploadCreation(executor.cfg, other, &candidate); err == nil {
			t.Fatalf("%s carry reference became original source authority", fault)
		}
		if !reflect.DeepEqual(before, validatorNamespaceTreeSnapshot(t, other)) {
			t.Fatalf("%s carry refusal changed retained originals", fault)
		}
	}
}

// Fixture setup owns its mode explicitly. The actual journal still refuses
// exposed caller-owned state and must not silently repair that authority.
func TestRuntimeEvidenceV2ReservedFixtureStateModeDoesNotRelaxJournalAdmission(t *testing.T) {
	stateDir := t.TempDir()
	for _, mode := range []os.FileMode{0o750, 0o775} {
		if err := os.Chmod(stateDir, mode); err != nil {
			t.Fatal(err)
		}
		if journal, err := OpenJournal(stateDir); err == nil {
			_ = journal.Close()
			t.Fatalf("exposed state mode %o reached journal ownership", mode)
		}
		if info, err := os.Stat(stateDir); err != nil || info.Mode().Perm() != mode {
			t.Fatal("journal admission silently changed caller-owned permissions")
		}
		if _, err := os.Lstat(filepath.Join(stateDir, "journal.jsonl")); !os.IsNotExist(err) {
			t.Fatal("refused exposed state acquired a journal")
		}
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
}
