// Synthetic local receipts prove that startup never waits for archive capacity.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
)

func TestProvisionalHistoryDeferralStartsWithoutArchiveAndCannotAccept(t *testing.T) {
	self, firstAction, firstEntry := provisionalVerifiedExecutor(t, "evidence.activate.1", false)
	var firstRecord ActionPostcondition
	if err := readJSONFile(filepath.Join(self.stateDir, firstEntry.PostconditionPath), &firstRecord); err != nil {
		t.Fatal(err)
	}
	second := firstAction
	second.ID, second.IntentHash = "evidence.activate.2", "another-synthetic-intent"
	second.Target = "synthetic-distinct-target"
	second.Parameters = map[string]string{"calldata": "synthetic-distinct-calldata"}
	self.plan.Actions = append(self.plan.Actions, second)
	secondRecord := firstRecord
	secondRecord.ActionID, secondRecord.IntentHash = second.ID, second.IntentHash
	path, hash, err := self.persistActionPostcondition(&secondRecord)
	if err != nil {
		t.Fatal(err)
	}
	secondEntry := firstEntry
	secondEntry.ActionID, secondEntry.IntentHash = second.ID, second.IntentHash
	secondEntry.PostconditionPath, secondEntry.PostconditionHash = path, hash
	if err := self.journal.Append(secondEntry); err != nil {
		t.Fatal(err)
	}
	var reads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		reads.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	manager := &EvmTxManager{client: client}
	self.deployer, self.owner, self.oracle, self.keeper = manager, manager, manager, manager
	before := self.journal.Entries()
	deferredPath := filepath.Join(filepath.Dir(self.cfg.provisionalResume.RecordPath), "historical-audit-deferred.json")
	if _, err := os.Stat(deferredPath); !os.IsNotExist(err) {
		t.Fatal("cold deferral fixture already has its output leaf")
	}
	if err := self.collectCarriedActionHistory(t.Context()); err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(deferredPath)
	if err != nil {
		t.Fatal(err)
	}
	var record provisionalHistoricalAuditDeferral
	if err := json.Unmarshal(wire, &record); err != nil {
		t.Fatal(err)
	}
	if !record.Provisional || record.FinalAcceptance || record.PlanHash != self.plan.PlanHash || record.ProvenanceHash != self.cfg.provisionalResume.RecordHash || record.JournalRoot != before[len(before)-1].EntryHash || len(record.Actions) != 2 {
		t.Fatalf("deferred history lost exact non-acceptance provenance: %+v", record)
	}
	for index, action := range self.plan.Actions {
		want, err := canonicalHashHex(action)
		if err != nil || record.Actions[index].ActionHash != want || record.Actions[index].SourceReceipt != before[index] || !strings.Contains(record.Actions[index].Detail, "historical chain replay deferred") {
			t.Fatalf("distinct action/receipt was coalesced or claimed historical proof: %+v %v", record.Actions[index], err)
		}
	}
	if record.Actions[0].ActionHash == record.Actions[1].ActionHash {
		t.Fatal("distinct targets/calldata share a deferral identity")
	}
	if err := self.collectCarriedActionHistory(t.Context()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(deferredPath)
	if err != nil || !bytes.Equal(wire, after) || reads.Load() != 0 || !reflect.DeepEqual(before, self.journal.Entries()) {
		t.Fatalf("provisional continuation repeated archive work or changed source receipts: reads=%d error=%v", reads.Load(), err)
	}
	result := &ScenarioResult{Result: "pass"}
	applyProvisionalScenarioProvenance(self.cfg, result)
	if result.FinalAcceptance == nil || *result.FinalAcceptance {
		t.Fatal("deferred history claimed final acceptance")
	}
	if err := validateScenarioFinalSemanticSource(nil, nil, result, nil); err == nil || !strings.Contains(err.Error(), "provisional") {
		t.Fatalf("strict final acceptance admitted deferred history: %v", err)
	}
}

func TestProvisionalHistoryDeferralRefusesChangedInvocationAndCancellation(t *testing.T) {
	self, _, _ := provisionalVerifiedExecutor(t, "evidence.activate.1", false)
	path := filepath.Join(filepath.Dir(self.cfg.provisionalResume.RecordPath), "historical-audit-deferred.json")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := self.recordProvisionalHistoryDeferral(ctx, self.journal.Entries()); err == nil {
		t.Fatal("canceled deferral publication succeeded")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("canceled deferral publication wrote a record")
	}
	if err := os.WriteFile(self.cfg.provisionalResume.RecordPath, []byte("changed synthetic provenance"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := self.collectCarriedActionHistory(t.Context()); err == nil || !strings.Contains(err.Error(), "provenance changed") {
		t.Fatalf("changed invocation retained provisional authority: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("changed invocation published deferred authority")
	}
}

// A missing output is creatable; an existing unsafe output is never followed
// or overwritten. The unrelated target keeps its exact bytes in both cases.
func TestProvisionalHistoryDeferralRefusesUnsafeExistingDestination(t *testing.T) {
	for _, linked := range []bool{false, true} {
		self, _, _ := provisionalVerifiedExecutor(t, "evidence.activate.1", false)
		path := filepath.Join(filepath.Dir(self.cfg.provisionalResume.RecordPath), "historical-audit-deferred.json")
		target := filepath.Join(self.stateDir, "synthetic-untouched-target")
		original := []byte("synthetic retained bytes")
		if err := os.WriteFile(target, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if linked {
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Fatal(err)
		}
		if !linked {
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := self.collectCarriedActionHistory(t.Context()); err == nil || !strings.Contains(err.Error(), "not a private regular file") {
			t.Fatalf("unsafe existing destination accepted: symlink=%t error=%v", linked, err)
		}
		if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, original) {
			t.Fatalf("destination check changed another file: symlink=%t error=%v", linked, err)
		}
	}
}
