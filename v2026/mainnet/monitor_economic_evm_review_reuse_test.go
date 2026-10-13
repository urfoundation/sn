// A digest references one acknowledged resource revision. Revisiting an older
// digest cannot authorize different settings merely because another review intervened.
package main

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMonitorEconomicEvmRetainedCheckpointRefusesNonAdjacentReviewReuse(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	first := fixture.start(t, monitorServiceHooks{})
	if event := first.next(t); !event.Current {
		t.Fatal(event)
	}
	first.stop(t)
	fixture.policy.ResourceRevision = &monitorEvmResourceRevision{Original: fixture.policy.resources()}
	for index, review := range []string{"a", "b"} {
		fixture.policy.ReadBudgetSeconds = uint64(400 + index*100)
		fixture.policy.ResourceRevision.ReviewSha256 = "sha256:" + strings.Repeat(review, 64)
		fixture.services.policy.EvmEconomics = []monitorEconomicEvmPolicy{fixture.policy}
		fixture.services.writePolicy(t)
		run := fixture.start(t, monitorServiceHooks{})
		if event := run.next(t); !event.Current || event.State.ResourceReviewHistory.Entries != uint64(index+2) {
			t.Fatal("distinct reviewed growth did not retain history", event)
		}
		run.stop(t)
	}
	beforeRecord := fixture.record(t)
	path, _ := monitorEconomicEvmPaths(fixture.services.checkpointPath, fixture.services.metricsPath, fixture.policy.Role)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.policy.ReadBudgetSeconds = 600
	fixture.policy.ResourceRevision.ReviewSha256 = "sha256:" + strings.Repeat("a", 64)
	owner, err := openMonitorCheckpoint(path, monitorTestExpectation(), fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker := &monitorEconomicEvmWorker{policy: fixture.policy, checkpoint: owner}
	_, loadErr := worker.load(fixture.ctx)
	closeErr := owner.close()
	after, readErr := os.ReadFile(path)
	if loadErr == nil || !strings.Contains(loadErr.Error(), "already acknowledged") || closeErr != nil || readErr != nil || !bytes.Equal(before, after) {
		t.Fatal("non-adjacent review reuse replaced retained resources", loadErr, closeErr, readErr)
	}
	fixture.policy.ResourceRevision.ReviewSha256 = "sha256:" + strings.Repeat("c", 64)
	fixture.services.policy.EvmEconomics = []monitorEconomicEvmPolicy{fixture.policy}
	fixture.services.writePolicy(t)
	last := fixture.start(t, monitorServiceHooks{})
	event := last.next(t)
	last.stop(t)
	afterRecord := fixture.record(t)
	if !event.Current || afterRecord.ResourceHistory == nil || len(afterRecord.ResourceHistory.Entries) != 4 || beforeRecord.ResourceHistory == nil || !reflect.DeepEqual(afterRecord.ResourceHistory.Entries[:3], beforeRecord.ResourceHistory.Entries) || !reflect.DeepEqual(afterRecord.State.History, beforeRecord.State.History) || !reflect.DeepEqual(afterRecord.State.Fees, beforeRecord.State.Fees) || afterRecord.State.BatchChainHash != beforeRecord.State.BatchChainHash {
		t.Fatal("new distinct review did not preserve original cursor/history", event, afterRecord)
	}
}

func TestMonitorEconomicEvmChecksEveryRetainedReviewReference(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	policy := fixture.policy
	var history *monitorEvmResourceHistory
	var err error
	history, err = history.advance(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.ResourceRevision = &monitorEvmResourceRevision{Original: policy.resources()}
	for index, review := range []string{"a", "b", "c"} {
		policy.ReadBudgetSeconds = uint64(400 + index*100)
		policy.ResourceRevision.ReviewSha256 = "sha256:" + strings.Repeat(review, 64)
		history, err = history.advance(policy)
		if err != nil {
			t.Fatal(err)
		}
	}
	changed := *history
	changed.Entries = append([]monitorEvmResourceAcknowledgment(nil), history.Entries...)
	changed.Entries[3].ReviewSha256 = changed.Entries[1].ReviewSha256
	changed.Entries[3].ContentHash = changed.Entries[3].hash()
	policy.ResourceRevision.ReviewSha256 = changed.Entries[3].ReviewSha256
	if err := changed.validate(policy); err == nil {
		t.Fatal("internally hashed A-B-A review chain admitted")
	}
	if history.Entries[3].ReviewSha256 != "sha256:"+strings.Repeat("c", 64) {
		t.Fatal("validation modified acknowledged provenance")
	}
}
