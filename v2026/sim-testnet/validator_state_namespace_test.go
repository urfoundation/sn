package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeValidatorStateTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareSignedAttemptStateNamespaceQuarantinesLegacyUnsignedState(t *testing.T) {
	stateDir := t.TempDir()
	state := filepath.Join(stateDir, "runtime", "validator-1", "state")
	writeValidatorStateTestFile(t, filepath.Join(state, "head-ema.json"), `{"legacy":true}`)
	writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-1", "stats.json"), `{"assignments":1}`)
	writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-1", "client.key"), "private-seed")
	writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-2", "proofs.jsonl"), "{}\n")

	if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(state))
	if err != nil {
		t.Fatal(err)
	}
	archive := ""
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "state-legacy-unsigned-") {
			archive = filepath.Join(filepath.Dir(state), entry.Name())
		}
	}
	if archive == "" {
		t.Fatal("legacy state archive is absent")
	}
	if _, err := os.Stat(filepath.Join(archive, "operators", "no-1", "client.key")); err != nil {
		t.Fatalf("legacy private state was not retained: %v", err)
	}
	if current, err := os.ReadDir(state); err != nil || len(current) != 0 {
		t.Fatalf("fresh signed namespace = %v, %v", current, err)
	}
	receipts, err := os.ReadDir(filepath.Join(stateDir, "receipts", "validator-state-namespaces"))
	if err != nil || len(receipts) != 1 {
		t.Fatalf("state reset receipts = %v, %v", receipts, err)
	}
	var receipt validatorAttemptStateReset
	wire, err := os.ReadFile(filepath.Join(stateDir, "receipts", "validator-state-namespaces", receipts[0].Name()))
	if err != nil || json.Unmarshal(wire, &receipt) != nil || receipt.Schema != validatorAttemptStateResetSchema || !receipt.Complete || receipt.ValidatorID != 1 || receipt.SourceHash == "" || receipt.FileCount != 4 {
		t.Fatalf("state reset receipt = %+v, %v", receipt, err)
	}
	if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err != nil {
		t.Fatalf("fresh namespace retry: %v", err)
	}
}

func TestPrepareSignedAttemptStateNamespacePreservesSignedState(t *testing.T) {
	stateDir := t.TempDir()
	state := filepath.Join(stateDir, "runtime", "validator-1", "state")
	for noID := 1; noID <= 2; noID++ {
		root := filepath.Join(state, "operators", "no-"+string(rune('0'+noID)))
		writeValidatorStateTestFile(t, filepath.Join(root, "attempt-ledger.jsonl"), "signed\n")
		writeValidatorStateTestFile(t, filepath.Join(root, "stats.json"), `{"signed":true}`)
	}
	writeValidatorStateTestFile(t, filepath.Join(state, "steering-intents.json"), `{"signed":true}`)
	if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "operators", "no-1", "attempt-ledger.jsonl")); err != nil {
		t.Fatalf("signed state was moved: %v", err)
	}
}

func TestPrepareSignedAttemptStateNamespaceRejectsMixedOrLiveState(t *testing.T) {
	t.Run("mixed signed and unsigned", func(t *testing.T) {
		stateDir := t.TempDir()
		state := filepath.Join(stateDir, "runtime", "validator-1", "state")
		writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-1", "attempt-ledger.jsonl"), "signed\n")
		writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-2", "stats.json"), `{"legacy":true}`)
		if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err == nil || !strings.Contains(err.Error(), "mixes signed and legacy") {
			t.Fatalf("mixed state = %v", err)
		}
		if _, err := os.Stat(filepath.Join(state, "operators", "no-2", "stats.json")); err != nil {
			t.Fatalf("mixed state was modified: %v", err)
		}
	})

	t.Run("live supervisor", func(t *testing.T) {
		stateDir := t.TempDir()
		state := filepath.Join(stateDir, "runtime", "validator-1", "state")
		writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-1", "stats.json"), `{"legacy":true}`)
		supervisor := SupervisorState{Schema: "urnetwork-sim-supervisor-state-v1", Processes: []ProcessState{{ID: "validator-1", Role: "validator", PID: 123, Healthy: true}}}
		wire, err := json.Marshal(&supervisor)
		if err != nil {
			t.Fatal(err)
		}
		writeValidatorStateTestFile(t, filepath.Join(stateDir, "supervisor.state.json"), string(wire))
		if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err == nil || !strings.Contains(err.Error(), "while supervisor records pid") {
			t.Fatalf("live state = %v", err)
		}
	})
}

func TestPrepareSignedAttemptStateNamespaceRecoversEveryResetCrashPoint(t *testing.T) {
	for _, stage := range []string{
		validatorStateResetAfterJournal,
		validatorStateResetAfterRename,
		validatorStateResetAfterMkdir,
		validatorStateResetAfterReceipt,
	} {
		t.Run(stage, func(t *testing.T) {
			stateDir := t.TempDir()
			state := filepath.Join(stateDir, "runtime", "validator-1", "state")
			writeValidatorStateTestFile(t, filepath.Join(state, "unknown-measurement", "raw.json"), `{"legacy":true}`)
			interrupted := false
			err := prepareSignedAttemptStateNamespaceWithHook("deployment", stateDir, 1, 2, func(current string) error {
				if current == stage && !interrupted {
					interrupted = true
					return errors.New("simulated crash")
				}
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "simulated crash") {
				t.Fatalf("interrupted reset = %v", err)
			}
			if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err != nil {
				t.Fatalf("reconcile reset: %v", err)
			}
			journalPath := filepath.Join(stateDir, "runtime", "validator-1", validatorAttemptStateResetJournalName)
			var journal validatorAttemptStateResetJournal
			if err := decodeStrictJSONFile(journalPath, &journal); err != nil || journal.Status != "complete" {
				t.Fatalf("completed journal = %+v, %v", journal, err)
			}
			archive := filepath.Join(stateDir, "runtime", "validator-1", journal.ArchiveName)
			if err := verifyValidatorLegacyArchive(archive, journal.SourceHash, journal.FileCount); err != nil {
				t.Fatalf("recovered archive: %v", err)
			}
			receiptPath := validatorAttemptStateResetReceiptPath(stateDir, 1, journal.SourceHash)
			var receipt validatorAttemptStateReset
			if err := decodeStrictJSONFile(receiptPath, &receipt); err != nil || !receipt.Complete || receipt.SourceHash != journal.SourceHash {
				t.Fatalf("recovered receipt = %+v, %v", receipt, err)
			}
			entries, err := os.ReadDir(state)
			if err != nil || len(entries) != 0 {
				t.Fatalf("fresh namespace = %v, %v", entries, err)
			}
		})
	}
}

func TestPrepareSignedAttemptStateNamespaceRejectsTamperedRecoveryArchive(t *testing.T) {
	stateDir := t.TempDir()
	state := filepath.Join(stateDir, "runtime", "validator-1", "state")
	writeValidatorStateTestFile(t, filepath.Join(state, "unknown-measurement.json"), `{"legacy":true}`)
	err := prepareSignedAttemptStateNamespaceWithHook("deployment", stateDir, 1, 2, func(stage string) error {
		if stage == validatorStateResetAfterRename {
			return errors.New("simulated crash")
		}
		return nil
	})
	if err == nil {
		t.Fatal("reset unexpectedly completed")
	}
	journalPath := filepath.Join(stateDir, "runtime", "validator-1", validatorAttemptStateResetJournalName)
	var journal validatorAttemptStateResetJournal
	if err := decodeStrictJSONFile(journalPath, &journal); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(stateDir, "runtime", "validator-1", journal.ArchiveName)
	writeValidatorStateTestFile(t, filepath.Join(archive, "tampered.json"), `{}`)
	if err := prepareSignedAttemptStateNamespace("deployment", stateDir, 1, 2); err == nil || !strings.Contains(err.Error(), "archive") {
		t.Fatalf("tampered recovery = %v", err)
	}
	if _, err := os.Stat(validatorAttemptStateResetReceiptPath(stateDir, 1, journal.SourceHash)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tampered archive issued receipt: %v", err)
	}
}

func TestClassifyValidatorAttemptStateRejectsEveryUnsignedMeasurementTree(t *testing.T) {
	t.Run("unknown nested state", func(t *testing.T) {
		state := t.TempDir()
		writeValidatorStateTestFile(t, filepath.Join(state, "future-format", "opaque.bin"), "measurement")
		legacy, signed, err := classifyValidatorAttemptState(state, 2)
		if err != nil || !legacy || signed {
			t.Fatalf("classification = legacy %v, signed %v, %v", legacy, signed, err)
		}
	})

	t.Run("static credentials only", func(t *testing.T) {
		state := t.TempDir()
		writeValidatorStateTestFile(t, filepath.Join(state, ".validator.key"), "seed")
		writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-1", "client.key"), "seed")
		legacy, signed, err := classifyValidatorAttemptState(state, 2)
		if err != nil || legacy || signed {
			t.Fatalf("classification = legacy %v, signed %v, %v", legacy, signed, err)
		}
	})

	t.Run("exact complete operator ledgers", func(t *testing.T) {
		state := t.TempDir()
		for noID := 1; noID <= 2; noID++ {
			writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-"+string(rune('0'+noID)), "attempt-ledger.jsonl"), "signed\n")
		}
		writeValidatorStateTestFile(t, filepath.Join(state, "unknown-future-measurement.json"), `{}`)
		legacy, signed, err := classifyValidatorAttemptState(state, 2)
		if err != nil || legacy || !signed {
			t.Fatalf("classification = legacy %v, signed %v, %v", legacy, signed, err)
		}
	})

	t.Run("extra operator ledger", func(t *testing.T) {
		state := t.TempDir()
		for noID := 1; noID <= 3; noID++ {
			writeValidatorStateTestFile(t, filepath.Join(state, "operators", "no-"+string(rune('0'+noID)), "attempt-ledger.jsonl"), "signed\n")
		}
		legacy, signed, err := classifyValidatorAttemptState(state, 2)
		if err != nil || !legacy || !signed {
			t.Fatalf("classification = legacy %v, signed %v, %v", legacy, signed, err)
		}
	})
}
