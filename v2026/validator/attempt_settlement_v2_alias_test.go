//go:build linux || darwin

package validator

// Legacy recovery alone preserves stable ancestor aliases. New v2 admission
// requires the physical absolute namespace, including on Darwin /var and /tmp.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The alias is a parent component; the selected final state directory itself
// is a real directory. Test provisioning precedes every authority handoff.
func attemptSettlementRuntimeV2TestAncestorAlias(t *testing.T, physical string) (string, string) {
	t.Helper()
	link := filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "ancestor")
	if err := os.Symlink(filepath.Dir(physical), link); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(link, filepath.Base(physical)), link
}

// The legacy wire/output bytes and completed journal removal stay unchanged
// when both coordinator and operator paths use stable historical aliases.
func TestAttemptSettlementRuntimeV2LegacyRecoveryPreservesStableAncestorAliases(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	fixture.trails(t, 0, 1, 0)
	fixture.coordinator, _ = attemptSettlementRuntimeV2TestAncestorAlias(t, fixture.coordinator)
	participants := append([]AttemptSettlementRuntimeV2Participant(nil), fixture.participants...)
	for index := range participants {
		participants[index].StateDir, _ = attemptSettlementRuntimeV2TestAncestorAlias(t, participants[index].StateDir)
	}
	transaction, legacy, journal := legacyAttemptSettlementRuntimeV2TestJournal(t, fixture, participants)
	publishLegacyAttemptSettlementRuntimeV2TestJournal(t, fixture, journal)
	if err := RecoverAttemptSettlementEpoch(fixture.coordinator, legacy); err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range transaction.Snapshots {
		actual, err := os.ReadFile(snapshot.StatsPath)
		if err != nil || !bytes.Equal(actual, snapshot.StatsJSON) {
			t.Fatalf("stable alias changed legacy bytes: %v", err)
		}
	}
	if _, err := os.Lstat(attemptSettlementTransactionPath(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stable alias journal was not removed: %v", err)
	}
}

// Resolving an old alias for read-only admission cannot hide a real v6 target.
func TestAttemptSettlementRuntimeV2LegacyAliasToV6TargetIsRejected(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	fixture.trails(t, 0, 1, 0)
	participants := append([]AttemptSettlementRuntimeV2Participant(nil), fixture.participants...)
	participants[1].StateDir, _ = attemptSettlementRuntimeV2TestAncestorAlias(t, participants[1].StateDir)
	transaction, legacy, journal := legacyAttemptSettlementRuntimeV2TestJournal(t, fixture, participants)
	activateAttemptSettlementRuntimeV2TestSecond(t, fixture)
	publishLegacyAttemptSettlementRuntimeV2TestJournal(t, fixture, journal)
	before := make([][]byte, len(participants))
	for index, snapshot := range transaction.Snapshots {
		var err error
		before[index], err = os.ReadFile(snapshot.StatsPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := RecoverAttemptSettlementEpoch(fixture.coordinator, legacy); err == nil || !strings.Contains(err.Error(), "cannot replace compact") {
		t.Fatalf("ancestor alias concealed v6 target: %v", err)
	}
	assertLegacyAttemptSettlementRuntimeV2TestUnchanged(t, fixture.coordinator, transaction, journal, before)
}

// Operator and coordinator retargeting are separately forced at the actual
// all-target admission/first-write boundary. Neither namespace is written.
func TestAttemptSettlementRuntimeV2LegacyAliasRetargetRefusesBeforeFirstWrite(t *testing.T) {
	for _, variant := range []string{"operator", "coordinator"} {
		fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
		fixture.trails(t, 0, 1, 0)
		physicalCoordinator := fixture.coordinator
		participants := append([]AttemptSettlementRuntimeV2Participant(nil), fixture.participants...)
		var link string
		if variant == "operator" {
			participants[0].StateDir, link = attemptSettlementRuntimeV2TestAncestorAlias(t, participants[0].StateDir)
		} else {
			fixture.coordinator, link = attemptSettlementRuntimeV2TestAncestorAlias(t, fixture.coordinator)
		}
		transaction, legacy, journal := legacyAttemptSettlementRuntimeV2TestJournal(t, fixture, participants)
		publishLegacyAttemptSettlementRuntimeV2TestJournal(t, fixture, journal)
		before := make([][]byte, len(fixture.participants))
		for index, participant := range fixture.participants {
			var err error
			before[index], err = os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
			if err != nil {
				t.Fatal(err)
			}
		}
		if bytes.Equal(before[0], transaction.Snapshots[0].StatsJSON) {
			t.Fatal("first target would not detect a premature write")
		}
		other := newAttemptSettlementRuntimeV2TestStateDir(t)
		marker := []byte("unrelated namespace must remain untouched\n")
		if err := os.WriteFile(filepath.Join(other, "stats.json"), marker, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(other, "settlement-transaction.json"), marker, 0o600); err != nil {
			t.Fatal(err)
		}
		steps := 0
		err := recoverAttemptSettlementEpochOwned(fixture.coordinator, legacy, nil, func(step string) error {
			if step != "before-first-write" {
				return nil
			}
			steps++
			if err := os.Rename(link, link+".preserved"); err != nil {
				return err
			}
			return os.Symlink(filepath.Dir(other), link)
		})
		if err == nil || steps != 1 {
			t.Fatalf("%s alias retarget was not refused: %v/%d", variant, err, steps)
		}
		for index, participant := range fixture.participants {
			actual, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
			if err != nil || !bytes.Equal(actual, before[index]) {
				t.Fatalf("%s retarget wrote original target: %v", variant, err)
			}
		}
		actual, err := os.ReadFile(attemptSettlementTransactionPath(physicalCoordinator))
		if err != nil || !bytes.Equal(actual, journal) {
			t.Fatalf("%s retarget changed original journal: %v", variant, err)
		}
		for _, name := range []string{"stats.json", "settlement-transaction.json"} {
			actual, err := os.ReadFile(filepath.Join(other, name))
			if err != nil || !bytes.Equal(actual, marker) {
				t.Fatalf("%s retarget wrote unrelated %s: %v", variant, name, err)
			}
		}
	}
}

// New v2 does not inherit the legacy alias fallback. Physical success and
// ancestor-alias refusal use the same real initialized engine state.
func TestAttemptSettlementRuntimeV2PhysicalPathRequiredBeforePersistence(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatalf("physical canonical namespace refused: %v", err)
	}
	alias, _ := attemptSettlementRuntimeV2TestAncestorAlias(t, fixture.coordinator)
	physical := attemptSettlementV2PhysicalIO()
	writes := 0
	physical.writeJournal = func(*attemptPrivateDirectory, string, []byte) error {
		writes++
		return errors.New("unexpected alias journal")
	}
	if err := initializeAttemptSettlementEpochV2(t.Context(), alias, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical); err == nil || writes != 0 {
		t.Fatalf("v2 normalized an alias candidate: %v/%d", err, writes)
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("alias refusal changed actual v6 state")
	}
}
