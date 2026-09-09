//go:build linux || darwin

package validator

// JSON replaces invalid UTF-8; authority paths must instead be refused without
// touching that namespace. Valid multibyte bytes survive real journal/restart.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// Move only an actually empty v5 ledger, preserving its exact identity/prefix.
// Then open a real disk ledger and reattach by the existing compatible API.
func runtimeAttemptSettlementV2MoveEmptyOperator(t *testing.T, fixture *attemptSettlementRuntimeV2TestFixture, index int, path string) {
	t.Helper()
	participant, operator := fixture.participants[index], fixture.fixtures[index]
	head, err := participant.Ledger.Head()
	if err != nil || head.LastSequence != 0 || head.Root != zeroAttemptHash() || participant.Stats.attemptV2 != nil {
		t.Fatalf("path fixture requires an actual empty legacy prefix: %+v/%v", head, err)
	}
	if err := participant.Ledger.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(participant.StateDir, path); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewDiskAttemptLedger(t.Context(), path, operator.expected.Identity, attemptLedgerDiskTestCoordinator, operator.key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	reopened, err := ledger.Head()
	if err != nil || reopened != head || ledger.identity != participant.Ledger.identity {
		t.Fatalf("path fixture changed its real identity or prefix: %v", err)
	}
	stats := NewStatsEngine(participant.Stats.cfg)
	if err := stats.Load(path); err != nil {
		t.Fatal(err)
	}
	if err := stats.AttachAttemptLedger(ledger, path); err != nil {
		t.Fatal(err)
	}
	store, err := NewProofStore(path)
	if err != nil {
		t.Fatal(err)
	}
	config := operator.engine.cfg
	config.AttemptLedger = ledger
	operator.engine = NewTrailEngine(operator.engine.clientId, operator.key, operator.server, NewStaticServerKeyRing(operator.server.serverPublicKeys()), operator.engine.pickSeed, stats, store, operator.engine.epochFn, config)
	operator.ledger = ledger
	fixture.participants[index] = AttemptSettlementRuntimeV2Participant{NoID: participant.NoID, StateDir: path, Stats: stats, Ledger: ledger}
}

// Each actual public runtime role is exercised with an occupied invalid-byte
// coordinator. The bad bytes are neither cleaned away nor replaced with U+FFFD.
func TestAttemptSettlementRuntimeV2PathEncodingCoordinatorBeforeIO(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"initialize", "advance", "recover", "closure"} {
		fixture := newAttemptSettlementRuntimeV2TestFixture(t, role != "initialize")
		if role == "advance" || role == "closure" {
			fixture.trails(t, 0, 2, 1)
		}
		if role == "closure" {
			if _, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t)); err != nil {
				t.Fatal(err)
			}
		}
		path := filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "coordinator-\xff")
		if utf8.ValidString(path) {
			t.Fatal("invalid-byte coordinator fixture became UTF-8")
		}
		if err := os.Rename(fixture.coordinator, path); err != nil {
			t.Fatal(err)
		}
		fixture.coordinator = path
		participants := fixture.participants
		if role == "recover" {
			participants = fixture.reopen(t)
		}
		physical, events := attemptSettlementV2PhysicalIO(), 0
		physical.step = func(string) error { events++; return nil }
		physical.closeFile = func(file *os.File) error { events++; return file.Close() }
		physical.closeRoot = func(root *attemptPrivateDirectory) error { events++; return root.close() }
		options := fixture.options(t)
		var err error
		switch role {
		case "initialize":
			err = initializeAttemptSettlementEpochV2(t.Context(), path, participants, 42, options.Authority, options.Persistence, physical)
		case "advance":
			_, err = advanceAttemptSettlementEpochV2(t.Context(), path, participants, 43, fixture.fixtures[0].expected.Boundary, options, physical, true)
		case "recover":
			err = recoverAttemptSettlementEpochV2(t.Context(), path, participants, options.Authority, options.Persistence, physical)
		case "closure":
			closure, verified, readErr := readAttemptSettlementClosureV2(t.Context(), path, 42, options.Authority, physical)
			err = readErr
			if closure != nil || !reflect.DeepEqual(verified, VerifiedAttemptSettlementV2{}) {
				t.Fatal("invalid-byte closure coordinator returned a result")
			}
		}
		if err == nil || !strings.Contains(err.Error(), "lossless UTF-8") || events != 0 {
			t.Fatalf("invalid-byte coordinator reached IO or was normalized: role=%s events=%d err=%v", role, events, err)
		}
	}
}

// An occupied operator namespace must not be lossy in its durable StatsPath.
func TestAttemptSettlementRuntimeV2PathEncodingOperatorBeforeIO(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	path := filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "operator-\xfe")
	if utf8.ValidString(path) {
		t.Fatal("invalid-byte operator fixture became UTF-8")
	}
	runtimeAttemptSettlementV2MoveEmptyOperator(t, fixture, 1, path)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	physical, events := attemptSettlementV2PhysicalIO(), 0
	physical.step = func(string) error { events++; return nil }
	physical.closeRoot = func(root *attemptPrivateDirectory) error { events++; return root.close() }
	err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical)
	if err == nil || !strings.Contains(err.Error(), "lossless UTF-8") || events != 0 {
		t.Fatalf("invalid-byte operator reached IO or lossy journal encoding: events=%d err=%v", events, err)
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("invalid-byte operator refusal mutated live state")
	}
	if _, err := os.Lstat(attemptSettlementTransactionV2Path(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid-byte operator wrote a journal: %v", err)
	}
}

// Full real activation, M8 terminal, immutable read and fresh recovery prove
// exact non-ASCII paths remain supported without any candidate normalization.
func TestAttemptSettlementRuntimeV2PathEncodingRealUnicodeRestart(t *testing.T) {
	t.Parallel()
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	path := filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "协调者-é<&>")
	if err := os.Rename(fixture.coordinator, path); err != nil {
		t.Fatal(err)
	}
	fixture.coordinator = path
	for index := range fixture.participants {
		operatorPath := filepath.Join(newAttemptSettlementRuntimeV2TestStateDir(t), "оператор-终端-é<&>")
		runtimeAttemptSettlementV2MoveEmptyOperator(t, fixture, index, operatorPath)
	}
	physical := attemptSettlementV2PhysicalIO()
	var journal []byte
	physical.writeJournal = func(root *attemptPrivateDirectory, name string, data []byte) error {
		journal = append([]byte(nil), data...)
		return writeAttemptSettlementV2OwnedState(root, name, data)
	}
	if err := initializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence(), physical); err != nil {
		t.Fatal(err)
	}
	var transaction attemptSettlementTransactionV2
	if err := json.Unmarshal(journal, &transaction); err != nil {
		t.Fatal(err)
	}
	if len(transaction.Snapshots) != 2 || !bytes.Contains(journal, []byte(`\u003c`)) {
		t.Fatal("real Unicode journal lost its path/HTML-escape prerequisite")
	}
	for index, entry := range transaction.Snapshots {
		if entry.StatsPath != filepath.Join(fixture.participants[index].StateDir, "stats.json") {
			t.Fatal("real Unicode journal changed path bytes")
		}
	}
	fixture.trails(t, 0, 2, 1)
	fixture.trails(t, 1, 1, 0)
	closure, err := AdvanceAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 43, fixture.fixtures[0].expected.Boundary, fixture.options(t))
	if err != nil || closure == nil {
		t.Fatalf("real Unicode terminal failed: %v", err)
	}
	if _, _, err := ReadAttemptSettlementClosureV2(t.Context(), fixture.coordinator, 42, fixture.options(t).Authority); err != nil {
		t.Fatal(err)
	}
	images := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	participants := fixture.reopen(t)
	if err := RecoverAttemptSettlementEpochV2(t.Context(), fixture.coordinator, participants, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(images, runtimeAttemptSettlementV2TestImages(t, participants)) {
		t.Fatal("real Unicode restart changed signed postimages")
	}
}
