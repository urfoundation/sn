// Literal v1 fixtures retain the frozen 00d field order and checksum. The
// public restart controls publish those bytes through the real guarded owner.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func monitorEconomicTestLegacyGolden(t *testing.T) ([]byte, monitorEconomicNativePolicy, monitorEconomicNativeCheckpoint) {
	t.Helper()
	policyRaw, err := os.ReadFile("testdata/native-economic-00d/policy.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/native-economic-00d/checkpoint.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy monitorEconomicNativePolicy
	var record monitorEconomicNativeCheckpoint
	if err := errors.Join(json.Unmarshal(policyRaw, &policy), json.Unmarshal(raw, &record)); err != nil {
		t.Fatal(err)
	}
	return raw, policy, record
}

func monitorEconomicTestPublishLegacy(t *testing.T, fixture *monitorEconomicTestFixture, raw []byte) {
	t.Helper()
	fixture.prepare(t)
	checkpoint, _ := monitorEconomicNativePaths(fixture.services.checkpointPath, fixture.services.metricsPath, fixture.policy.Role)
	expected := identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}
	owner, err := openMonitorCheckpoint(checkpoint, expected, fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = errors.Join(owner.directory.publish(filepath.Base(checkpoint), raw, 0600, owner.syncDirectory), owner.close())
	if err != nil {
		t.Fatal(err)
	}
}

// Substitute only fixed-size synthetic positions and the independent policy
// hash in the literal old wire record, then hash its exact original encoding.
func monitorEconomicTestLegacyForArchive(t *testing.T, fixture *monitorEconomicTestFixture) ([]byte, monitorEconomicNativeCheckpoint) {
	t.Helper()
	raw, _, record := monitorEconomicTestLegacyGolden(t)
	events := fixture.source.storageKVs[fixture.source.chain.byHeight[101]][fixture.source.eventsKey]
	if events == nil {
		t.Fatal("old checkpoint fixture has no real original event bytes")
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(*events, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	replacements := []string{record.PolicyHash, fixture.policy.identityHash(), record.ContentHash, "",
		"0x" + strings.Repeat("1", 64), fixture.source.chain.byHeight[100],
		"0x" + strings.Repeat("2", 64), fixture.source.chain.byHeight[101],
		"0x" + strings.Repeat("3", 64), fixture.source.chain.byHeight[102],
		"0x" + strings.Repeat("4", 64), rootExtrinsicHash(decoded)}
	oldBytes := strings.TrimSpace(strings.NewReplacer(replacements...).Replace(string(raw)))
	digest := sha256.Sum256([]byte(oldBytes))
	oldBytes = strings.Replace(oldBytes, `"content_hash":""`, `"content_hash":"sha256:`+hex.EncodeToString(digest[:])+`"`, 1)
	raw = append([]byte(oldBytes), '\n')
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if record.ContentHash != record.hash() || record.PolicyHash != fixture.policy.identityHash() {
		t.Fatal("literal old-format synthetic substitution changed the checksum format")
	}
	return raw, record
}

func TestMonitorEconomicRuntimeLiteral00dCheckpointKeepsChecksumAndHistory(t *testing.T) {
	raw, policy, original := monitorEconomicTestLegacyGolden(t)
	if original.ContentHash != "sha256:98c3e5bb3021d099f0180c28d397d5ccb087d00450ffc7d2caf6d63aa0787db7" || original.hash() != original.ContentHash || original.PolicyHash != policy.identityHash() {
		t.Fatal("new loader changed the literal 00d checksum or policy encoding")
	}
	encoded, err := json.Marshal(original)
	if err != nil || !bytes.Equal(append(encoded, '\n'), raw) {
		t.Fatal("literal 00d checkpoint did not round trip byte for byte", err)
	}
	fixture := newMonitorEconomicTestFixture(t, false)
	fixture.policy = policy
	monitorEconomicTestPublishLegacy(t, fixture, raw)
	basis := uint64(0)
	policy.ReadBudgetBasisSeconds, policy.ReadBudgetSeconds = &basis, 600
	policy.RuntimeCatalog = []monitorEconomicRuntimeEntry{monitorEconomicTestReview(policy.Observation.Runtime)}
	worker, err := openMonitorEconomicNativeWorker(fixture.ctx, fixture.source.client, policy, identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}, fixture.services.checkpointPath, fixture.services.metricsPath, monitorServiceHooks{})
	if err != nil {
		t.Fatal("reviewed renewal refused the literal prior checkpoint", err)
	}
	defer func() {
		if err := worker.close(monitorServiceHooks{}); err != nil {
			t.Error(err)
		}
	}()
	if rootObjectHash(worker.state) != rootObjectHash(original.State) || worker.runtimeAcknowledgement == nil || worker.runtimeAcknowledgement.ReadBudgetSeconds != 300 || worker.runtimeAcknowledgement.Entries != 0 {
		t.Fatal("loading old bytes relabeled history or acknowledged an uncommitted renewal")
	}
	checkpoint, _ := monitorEconomicNativePaths(fixture.services.checkpointPath, fixture.services.metricsPath, policy.Role)
	loaded, err := os.ReadFile(checkpoint)
	if err != nil || !bytes.Equal(loaded, raw) {
		t.Fatal("read-only old-checkpoint load rewrote persisted authority", err)
	}
}

func TestMonitorEconomicRuntimePublicOldCheckpointUsesLargerOwnedBudget(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	raw, original := monitorEconomicTestLegacyForArchive(t, fixture)
	monitorEconomicTestPublishLegacy(t, fixture, raw)
	basis := uint64(0)
	fixture.policy.ReadBudgetBasisSeconds, fixture.policy.ReadBudgetSeconds = &basis, 600
	fixture.policy.RuntimeCatalog = []monitorEconomicRuntimeEntry{monitorEconomicTestReview(fixture.policy.Observation.Runtime)}
	monitorEconomicTestPolicyWrite(t, fixture)
	fixture.unavailable.Store(true)
	var waits atomic.Int32
	run := fixture.start(t, monitorServiceHooks{rpcWait: func(ctx context.Context, role string, _ time.Duration) error {
		deadline, ok := ctx.Deadline()
		if role != fixture.policy.Role || !ok || time.Until(deadline) > 600*time.Second || time.Until(deadline) < 500*time.Second {
			return errors.New("renewed public read did not own its single 600-second budget")
		}
		waits.Add(1)
		fixture.unavailable.Store(false)
		return nil
	}})
	event := run.next(t)
	if !event.Current || waits.Load() != 1 || event.State.Cursor.Number != 102 || event.State.BatchCount != 2 || monitorEconomicTestFee(event) != "12" || event.State.RuntimeAcknowledgement == nil || event.State.RuntimeAcknowledgement.ReadBudgetSeconds != 600 {
		t.Fatal("literal old checkpoint could not continue under a larger reviewed read budget", event, waits.Load())
	}
	run.stop(t)
	record := fixture.record(t)
	if record.PolicyHash != original.PolicyHash || record.State.RuntimeBoundFrom != 102 || len(record.State.History) != 2 || rootObjectHash(record.State.History[0]) != rootObjectHash(original.State.History[0]) || record.State.History[0].ExecutionRuntime != nil {
		t.Fatal("read-budget renewal rewrote old history, authority or its original interpretation", record)
	}
	fixture.policy.ReadBudgetSeconds = 300
	worker, err := openMonitorEconomicNativeWorker(fixture.ctx, fixture.source.client, fixture.policy, identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}, fixture.services.checkpointPath, fixture.services.metricsPath, monitorServiceHooks{})
	if worker != nil {
		_ = worker.close(monitorServiceHooks{})
	}
	if err == nil || !strings.Contains(err.Error(), "shrank an acknowledged budget") {
		t.Fatal("restart silently shrank an acknowledged read budget", err)
	}
}

func TestMonitorEconomicRuntimeRestartRefusesReviewAndCapacityReplacement(t *testing.T) {
	fixture := newMonitorEconomicTestFixture(t, false)
	fixture.policy.RuntimeCatalog = []monitorEconomicRuntimeEntry{monitorEconomicTestReview(fixture.policy.Observation.Runtime)}
	fixture.policy.RuntimeCapacity = &monitorEconomicRuntimeCapacity{Entries: 16, Bytes: 16 * 1024}
	run := fixture.start(t, monitorServiceHooks{})
	if event := run.next(t); !event.Current {
		t.Fatal(event)
	}
	run.stop(t)
	original := fixture.record(t)
	expected := identityExpectation{NativeChain: "fixture-mainnet", GenesisHash: testGenesisHash, EvmChainId: 964}
	for _, change := range []string{"review", "entry-limit", "byte-limit", "budget-without-basis", "network", "history"} {
		policy := fixture.policy
		policy.RuntimeCatalog = append([]monitorEconomicRuntimeEntry(nil), fixture.policy.RuntimeCatalog...)
		switch change {
		case "review":
			policy.RuntimeCatalog[0].ReviewSha256 = "sha256:" + strings.Repeat("9", 64)
		case "entry-limit":
			policy.RuntimeCapacity = &monitorEconomicRuntimeCapacity{Entries: 8, Bytes: 16 * 1024}
		case "byte-limit":
			policy.RuntimeCapacity = &monitorEconomicRuntimeCapacity{Entries: 16, Bytes: 8 * 1024}
		case "budget-without-basis":
			policy.ReadBudgetSeconds = 600
		case "network":
			policy.Observation.Network.GenesisHash = "0x" + strings.Repeat("b", 64)
		case "history":
			policy.HistoryEntries++
		}
		worker, err := openMonitorEconomicNativeWorker(fixture.ctx, fixture.source.client, policy, expected, fixture.services.checkpointPath, fixture.services.metricsPath, monitorServiceHooks{})
		if worker != nil {
			_ = worker.close(monitorServiceHooks{})
		}
		if err == nil {
			t.Fatal("retained checkpoint accepted a replacement policy", change)
		}
		if record := fixture.record(t); record.ContentHash != original.ContentHash {
			t.Fatal("refused renewal rewrote original checkpoint", change)
		}
	}
}
