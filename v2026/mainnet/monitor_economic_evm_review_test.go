// Resource review history stays attached to the actual retained economic
// checkpoint. These references are configuration evidence, not review signatures.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMonitorEconomicEvmPublicResourceReviewsRemainAppendOnly(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	first := fixture.start(t, monitorServiceHooks{})
	if event := first.next(t); !event.Current || event.State.ResourceReviewHistory.Entries != 1 {
		t.Fatal("initial resource basis was not acknowledged", event)
	}
	first.stop(t)
	original := fixture.record(t)
	if original.ResourceHistory == nil || len(original.ResourceHistory.Entries) != 1 {
		t.Fatal("initial resource history missing", original)
	}
	fixture.policy.ResourceRevision = &monitorEvmResourceRevision{Original: fixture.policy.resources(), ReviewSha256: "sha256:" + strings.Repeat("a", 64)}
	fixture.policy.ReadBudgetSeconds = 600
	fixture.services.policy.EvmEconomics = []monitorEconomicEvmPolicy{fixture.policy}
	fixture.services.writePolicy(t)
	second := fixture.start(t, monitorServiceHooks{})
	if event := second.next(t); !event.Current || event.State.ResourceReviewHistory.Entries != 2 {
		t.Fatal("first resource review did not retain its predecessor", event)
	}
	second.stop(t)
	prior := fixture.record(t)
	fixture.policy.ResourceRevision.ReviewSha256 = "sha256:" + strings.Repeat("b", 64)
	fixture.policy.HistoryEntries = 128
	fixture.services.policy.EvmEconomics = []monitorEconomicEvmPolicy{fixture.policy}
	fixture.services.writePolicy(t)
	third := fixture.start(t, monitorServiceHooks{})
	event := third.next(t)
	third.stop(t)
	current := fixture.record(t)
	if !event.Current || event.State.ResourceReviewHistory.Entries != 3 || event.State.ResourceReviewHistory.LegacyLatestOnly || event.State.ResourceReviewHistory.Authority != "local-config-review-reference" || current.ResourceHistory == nil || len(current.ResourceHistory.Entries) != 3 || !reflect.DeepEqual(current.ResourceHistory.Entries[:2], prior.ResourceHistory.Entries) || current.ResourceHistory.Entries[0] != original.ResourceHistory.Entries[0] {
		t.Fatal("resource growth discarded an acknowledged review", event, current)
	}
	if !reflect.DeepEqual(current.State.History, original.State.History) || !reflect.DeepEqual(current.State.Fees, original.State.Fees) || current.State.BatchChainHash != original.State.BatchChainHash {
		t.Fatal("resource review altered original contract or fee evidence", current)
	}
	path, _ := monitorEconomicEvmPaths(fixture.services.checkpointPath, fixture.services.metricsPath, fixture.policy.Role)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixture.policy.ResourceRevision.ReviewSha256 = "sha256:" + strings.Repeat("c", 64)
	owner, err := openMonitorCheckpoint(path, identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}, fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker := &monitorEconomicEvmWorker{policy: fixture.policy, checkpoint: owner}
	_, loadErr := worker.load(fixture.ctx)
	closeErr := owner.close()
	after, readErr := os.ReadFile(path)
	if loadErr == nil || closeErr != nil || readErr != nil || string(before) != string(after) {
		t.Fatal("same resources silently relabelled an acknowledged review", loadErr, closeErr, readErr)
	}
}

func TestMonitorEconomicEvmLegacyCheckpointKeepsOriginalReviewProvenance(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	fixture.policy.ResourceRevision = &monitorEvmResourceRevision{Original: fixture.policy.resources(), ReviewSha256: "sha256:" + strings.Repeat("a", 64)}
	fixture.policy.ReadBudgetSeconds = 600
	first := fixture.start(t, monitorServiceHooks{})
	if event := first.next(t); !event.Current {
		t.Fatal(event)
	}
	first.stop(t)
	current := fixture.record(t)
	// Exact field order and hash grammar from frozen 0fb's checkpoint writer;
	// this synthetic legacy record has no knowledge of the new history type.
	type legacyCheckpoint struct {
		Schema               string                  `json:"schema"`
		PolicyHash           string                  `json:"policy_hash"`
		State                monitorEconomicEvmState `json:"state"`
		ContentHash          string                  `json:"content_hash"`
		Resources            *monitorEvmResources    `json:"resources,omitempty"`
		ResourceReviewSha256 string                  `json:"resource_review_sha256,omitempty"`
	}
	legacy := legacyCheckpoint{Schema: current.Schema, PolicyHash: current.PolicyHash, State: current.State, Resources: current.Resources, ResourceReviewSha256: current.ResourceReviewSha256}
	legacy.ContentHash = rootObjectHash(legacy)
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var decoded monitorEconomicEvmCheckpoint
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.hash() != legacy.ContentHash || decoded.ResourceHistory != nil {
		t.Fatal("new reader changed original0fb checksum grammar", err, decoded)
	}
	path, _ := monitorEconomicEvmPaths(fixture.services.checkpointPath, fixture.services.metricsPath, fixture.policy.Role)
	owner, err := openMonitorCheckpoint(path, identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}, fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	publishErr := owner.directory.publish(filepath.Base(path), append(raw, '\n'), 0600, owner.syncDirectory)
	closeErr := owner.close()
	if publishErr != nil || closeErr != nil {
		t.Fatal(publishErr, closeErr)
	}
	fixture.policy.ReadBudgetSeconds = 700
	fixture.policy.ResourceRevision.ReviewSha256 = "sha256:" + strings.Repeat("b", 64)
	fixture.services.policy.EvmEconomics = []monitorEconomicEvmPolicy{fixture.policy}
	fixture.services.writePolicy(t)
	second := fixture.start(t, monitorServiceHooks{})
	event := second.next(t)
	second.stop(t)
	record := fixture.record(t)
	if !event.Current || !event.State.ResourceReviewHistory.LegacyLatestOnly || record.ResourceHistory == nil || record.ResourceHistory.LegacyCheckpointSha256 != legacy.ContentHash || len(record.ResourceHistory.Entries) != 2 || record.ResourceHistory.Entries[0].Resources != *legacy.Resources || record.ResourceHistory.Entries[0].ReviewSha256 != legacy.ResourceReviewSha256 || record.ResourceHistory.Entries[1].ReviewSha256 != fixture.policy.ResourceRevision.ReviewSha256 || record.State.BatchChainHash != legacy.State.BatchChainHash || !reflect.DeepEqual(record.State.History, legacy.State.History) || !reflect.DeepEqual(record.State.Fees, legacy.State.Fees) {
		t.Fatal("legacy checkpoint lost its actual latest review or invented older provenance", event, record)
	}
}

func TestMonitorEconomicEvmReviewCapacityGrowthRetainsEveryAcknowledgment(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	policy := fixture.policy
	var history *monitorEvmResourceHistory
	history, err := history.advance(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.ResourceRevision = &monitorEvmResourceRevision{Original: policy.resources()}
	for index := 1; index < defaultMonitorEvmReviewHistoryEntries; index++ {
		policy.StallSeconds++
		policy.ResourceRevision.ReviewSha256 = fmt.Sprintf("sha256:%064x", index)
		history, err = history.advance(policy)
		if err != nil {
			t.Fatal("bounded reviewed growth failed", index, err)
		}
	}
	prior := append([]monitorEvmResourceAcknowledgment(nil), history.Entries...)
	summary := history.summary(policy)
	if !summary.CapacityWarning || summary.Remaining != 0 || summary.Entries != defaultMonitorEvmReviewHistoryEntries {
		t.Fatal("review capacity warning missing", summary)
	}
	policy.StallSeconds++
	policy.ResourceRevision.ReviewSha256 = fmt.Sprintf("sha256:%064x", defaultMonitorEvmReviewHistoryEntries)
	if _, err := history.advance(policy); !errors.Is(err, errMonitorEconomicCapacity) || !reflect.DeepEqual(prior, history.Entries) {
		t.Fatal("full review history silently evicted acknowledged provenance", err)
	}
	policy.ResourceRevision.ReviewHistoryEntries = 64
	grown, err := history.advance(policy)
	if err != nil || grown == nil || len(grown.Entries) != 33 || !reflect.DeepEqual(grown.Entries[:32], prior) || grown.summary(policy).CapacityWarning {
		t.Fatal("reviewed capacity growth did not retain all prior acknowledgments", err, grown)
	}
	policy.ResourceRevision.ReviewHistoryEntries = 32
	if _, err := grown.advance(policy); err == nil {
		t.Fatal("review history capacity shrink admitted")
	}
}

func TestMonitorEconomicEvmReviewHistoryRejectsChangedPrefix(t *testing.T) {
	fixture := newMonitorEvmFixture(t, "settlement-vault", false)
	policy := fixture.policy
	var history *monitorEvmResourceHistory
	history, err := history.advance(policy)
	if err != nil {
		t.Fatal(err)
	}
	policy.ResourceRevision = &monitorEvmResourceRevision{Original: policy.resources(), ReviewSha256: "sha256:" + strings.Repeat("a", 64)}
	policy.ReadBudgetSeconds = 600
	history, err = history.advance(policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"settings", "review", "drop", "order", "legacy-origin", "reused-review"} {
		changed := *history
		changed.Entries = append([]monitorEvmResourceAcknowledgment(nil), history.Entries...)
		switch mode {
		case "settings":
			changed.Entries[0].Resources.StallSeconds++
		case "review":
			changed.Entries[0].ReviewSha256 = "sha256:" + strings.Repeat("b", 64)
		case "drop":
			changed.Entries = changed.Entries[1:]
		case "order":
			changed.Entries[0], changed.Entries[1] = changed.Entries[1], changed.Entries[0]
		case "legacy-origin":
			changed.LegacyCheckpointSha256 = "sha256:" + strings.Repeat("c", 64)
		case "reused-review":
			changed.Entries[1].ReviewSha256 = changed.Entries[0].ReviewSha256
			changed.Entries[1].ContentHash = changed.Entries[1].hash()
		}
		if err := changed.validate(policy); err == nil {
			t.Fatal("changed acknowledged resource prefix admitted", mode)
		}
	}
}
