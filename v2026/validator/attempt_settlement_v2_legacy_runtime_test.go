//go:build linux || darwin

package validator

// Adjacent generic APIs may neither fold compact counters nor attach/recover
// compact state without the independent all-operator runtime authority.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// Actual signed pending input is copied into a new ledger namespace at the
// real pre-checkpoint crash boundary. Generic attachment must not append its
// validator-error terminal, write Stats or publish attachment.
func TestAttemptSettlementRuntimeV2LegacyAttachRefusesBeforePendingRecovery(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	participant := fixture.participants[0]
	initial, err := os.ReadFile(filepath.Join(participant.StateDir, "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.trails(t, 0, 1, 0)
	var record AttemptRecord
	if err := participant.Ledger.Walk(t.Context(), 1, 1, func(actual AttemptRecord) error { record = actual; return nil }); err != nil {
		t.Fatal(err)
	}
	if record.Disposition != AttemptDispositionPending || record.M != 8 {
		t.Fatal("real M8 pending prefix prerequisite differs")
	}
	dir := newAttemptSettlementRuntimeV2TestStateDir(t)
	if err := os.WriteFile(filepath.Join(dir, "attempt-ledger.jsonl"), attemptLedgerDiskTestJSONL(t, []AttemptRecord{record}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stats.json"), initial, 0o600); err != nil {
		t.Fatal(err)
	}
	ledger, err := NewDiskAttemptLedger(t.Context(), dir, participant.Ledger.identity, attemptLedgerDiskTestCoordinator, fixture.fixtures[0].key, attemptLedgerDiskTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	stats := NewStatsEngine(participant.Stats.cfg)
	if err := stats.Load(dir); err != nil {
		t.Fatal(err)
	}
	head, err := ledger.Head()
	if err != nil || head.LastSequence != 1 {
		t.Fatalf("actual pending import prerequisite: %v", err)
	}
	if err := stats.AttachAttemptLedgerContext(t.Context(), ledger, dir); err == nil || !strings.Contains(err.Error(), "independently authenticated v2 recovery") {
		t.Fatalf("generic attach bypassed compact authority: %v", err)
	}
	after, err := ledger.Head()
	if err != nil || after != head || stats.attemptLedger != nil || !stats.attemptCutPending {
		t.Fatalf("generic attach appended or published: %v", err)
	}
	disk, err := os.ReadFile(filepath.Join(dir, "stats.json"))
	if err != nil || !bytes.Equal(disk, initial) {
		t.Fatalf("generic attach wrote an unauthenticated snapshot: %v", err)
	}
	pending := 0
	if err := ledger.disk.Pending(t.Context(), func(actual AttemptRecord) error {
		pending++
		if actual.RecordHash != record.RecordHash {
			return errors.New("pending prefix changed")
		}
		return nil
	}); err != nil || pending != 1 {
		t.Fatalf("generic attach recovered actual pending row: %v/%d", err, pending)
	}
}

// The legacy public fold still computes real positive quality under M8/a_min8.
func TestAttemptSettlementRuntimeV2LegacyFoldPreservesRealV1Math(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	fixture.trails(t, 0, 15, 1)
	stats := fixture.participants[0].Stats
	if len(stats.window) == 0 {
		t.Fatal("real legacy window is empty")
	}
	if err := stats.Fold(); err != nil {
		t.Fatal(err)
	}
	if len(stats.window) != 0 || len(stats.emaPPM) == 0 || len(stats.ema) != len(stats.emaPPM) {
		t.Fatal("legacy fold lost real quality or did not clear its raw window")
	}
	for _, value := range stats.emaPPM {
		if value == 0 {
			t.Fatal("guaranteed real M8 legacy quality was not positive")
		}
	}
}

// A v6 legacy fold returns an error and leaves actual counters, epoch, cursor,
// EMA, history and all admission flags untouched.
func TestAttemptSettlementRuntimeV2LegacyFoldCannotMutateRealV6Counters(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if err := fixture.participants[0].Stats.Fold(); err == nil {
		t.Fatal("legacy fold accepted compact statistics")
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) || fixture.participants[0].Stats.attemptCutPending {
		t.Fatal("refused legacy fold changed compact ownership")
	}
}

// A real local RPC endpoint observes no request beyond the initial dial, and
// no native endpoint receives a dial. Both first-tempo and later-tempo generic
// steering paths refuse before folding or mutating their epoch cache.
func TestAttemptSettlementRuntimeV2GenericSteererRefusesBeforeExternalIO(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, true)
	fixture.trails(t, 0, 1, 0)
	var requests, nativeRequests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		var input struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(writer, err.Error(), 400)
			return
		}
		result := "0x3b1"
		if input.Method != "eth_chainId" {
			result = "0x0"
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": input.ID, "result": result})
	}))
	defer endpoint.Close()
	native := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		nativeRequests.Add(1)
		http.Error(writer, "unexpected native submission", 500)
	}))
	defer native.Close()
	chain, err := DialChainContext(t.Context(), []string{endpoint.URL}, common.Address{1})
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	if requests.Load() != 1 {
		t.Fatal("actual dial did not establish its chain ID")
	}
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	for _, seen := range []bool{false, true} {
		steerer := NewSteerer(chain, fixture.participants[0].Stats, nil, SteerConfig{SubstrateUrls: []string{"ws" + strings.TrimPrefix(native.URL, "http")}})
		steerer.epochSeen, steerer.lastFoldedEpoch = seen, 42
		if err := steerer.SubmitOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "authenticated v2 steering") {
			t.Fatalf("generic v6 steering was not refused: %v", err)
		}
		if steerer.epochSeen != seen || steerer.lastFoldedEpoch != 42 || requests.Load() != 1 || nativeRequests.Load() != 0 {
			t.Fatal("generic v6 steering mutated cache or entered external I/O")
		}
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("generic steerer changed compact statistics")
	}
}

// A real old journal cannot be skipped by the new initializer. Its distinct
// v2 journal/snapshots must remain absent and every live legacy byte preserved.
func TestAttemptSettlementRuntimeV2ActivationRefusesPendingLegacyJournal(t *testing.T) {
	fixture := newAttemptSettlementRuntimeV2TestFixture(t, false)
	_, _, journal := legacyAttemptSettlementRuntimeV2TestJournal(t, fixture, fixture.participants)
	publishLegacyAttemptSettlementRuntimeV2TestJournal(t, fixture, journal)
	before := runtimeAttemptSettlementV2TestImages(t, fixture.participants)
	if err := InitializeAttemptSettlementEpochV2(t.Context(), fixture.coordinator, fixture.participants, 42, fixture.options(t).Authority, runtimeAttemptSettlementV2TestPersistence()); err == nil {
		t.Fatal("compact activation skipped a pending v1 transaction")
	}
	if !reflect.DeepEqual(before, runtimeAttemptSettlementV2TestImages(t, fixture.participants)) {
		t.Fatal("pending legacy activation changed live state")
	}
	if _, err := os.Lstat(attemptSettlementTransactionV2Path(fixture.coordinator)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending legacy activation created v2 journal: %v", err)
	}
}
