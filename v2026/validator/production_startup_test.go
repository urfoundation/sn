//go:build linux || darwin

// The public root opens real dormant disk stores, authenticates signed
// activation/history, starts actual operator sessions and reconciles native work.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// An unrelated latest metadata/current EVM outage cannot strand an immortal
// original signature. Both receipt and application use actual canonical rows;
// a later preparation request is blocked before it can sign anything new.
func TestProductionStartupRunReleaseReconcilesBeforeCurrentPreparation(t *testing.T) {
	fixture := newProductionStartupTestFixture(t)
	continuation := fixture.continuation
	pending := continuation.beginAndLoseAcknowledgement(t, fixture.native)
	fixture.native.receiptNumber, fixture.native.applied = 103, true
	continuation.production.head = max(uint64(104), pending.Prepared.RevealBlock)
	continuation.production.extrinsicsKVs = map[uint64][]string{103: {pending.Prepared.ExtrinsicHex}}
	continuation.production.epoch++
	continuation.production.operator.blocks[110] = [32]byte{0xa2}
	continuation.production.operator.finalized = 110
	fixture.closePreparation(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
	var result error
	select {
	case result = <-done:
		fixture.stateLock.Lock()
		latestReads := fixture.latestReads
		fixture.stateLock.Unlock()
		if latestReads != 0 {
			t.Fatalf("actual RunRelease queried unrelated latest metadata before retained intent recovery: %v", result)
		}
		t.Fatalf("actual RunRelease stopped before the completed original intent reached current preparation: %v", result)
	case <-fixture.latestRead:
		cancel()
		result = <-done
		t.Fatalf("actual RunRelease queried unrelated latest metadata before retained intent recovery: %v", result)
	case <-fixture.evm.freshRead:
		cancel()
		result = <-done
	case <-t.Context().Done():
		cancel()
		result = <-done
		t.Fatalf("actual RunRelease did not reach its preparation barrier: %v", result)
	}
	if result != nil && !errors.Is(result, context.Canceled) {
		t.Fatalf("public root cancellation invented a terminal integrity failure: %v", result)
	}
	var stored steeringIntentFile
	if err := json.Unmarshal(fixture.storedIntentBytes(t), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Current == nil {
		t.Fatal("public root lost its durable nonempty intent")
	}
	applied := stored.Current
	if applied.Status != "applied" || applied.SubnetEpoch != pending.SubnetEpoch || applied.VectorHash != pending.VectorHash || applied.CreatedAt != pending.CreatedAt || applied.Prepared == nil || applied.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex || applied.FinalizedBlock != 103 || applied.ApplicationBlock == 0 || !slices.Equal(fixture.native.broadcasts, []string{pending.Prepared.ExtrinsicHex}) {
		t.Fatalf("public RunRelease lost original pending receipt/application or signed a replacement: status=%s finalized=%d application=%d sends=%d", applied.Status, applied.FinalizedBlock, applied.ApplicationBlock, len(fixture.native.broadcasts))
	}
	fixture.stateLock.Lock()
	latestReads, writeRequests := fixture.latestReads, fixture.writeRequests
	fixture.stateLock.Unlock()
	if latestReads != 0 || writeRequests != 0 {
		t.Fatal("historical recovery queried latest metadata or sent a replacement native request")
	}
	for _, api := range fixture.origins {
		api.stateLock.Lock()
		sessions := api.sessions
		api.stateLock.Unlock()
		if sessions == 0 {
			t.Fatal("public root did not establish both actual operator session owners")
		}
	}
}

// Empty stores traverse the same actual public root. Trail seeding cannot
// reach either real operator API until current UID/stake and the initial
// settlement publication owner have completed. The test cancels at that I/O
// barrier. The empty measurement census cannot authorize a native signature.
func TestProductionStartupRunReleaseInitialDeploymentBecomesReady(t *testing.T) {
	fixture := newProductionStartupTestFixture(t)
	fixture.selectEmptyDeployment(t)
	cfg := fixture.continuation.production.cfg
	intentPath := filepath.Join(cfg.StateDir, "steering-intents.json")
	if _, err := os.Stat(intentPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("initial deployment already contained an intent: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRelease(fixture.storageContext(ctx), fixture.configPath) }()
	select {
	case err := <-done:
		t.Fatalf("actual fresh RunRelease failed before readiness: %v", err)
	case <-fixture.latestRead:
		cancel()
		err := <-done
		t.Fatalf("fresh startup abandoned explicitly approved initialization: %v", err)
	case <-fixture.origins[0].seedRead:
	case <-fixture.origins[1].seedRead:
	case <-t.Context().Done():
		cancel()
		err := <-done
		t.Fatalf("fresh startup did not reach its actual seed request: %v", err)
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("fresh startup failed while joining actual worker owners: %v", err)
	}
	for _, op := range cfg.Operators {
		raw, err := os.ReadFile(filepath.Join(op.StateDir, "stats.json"))
		if err != nil {
			t.Fatal(err)
		}
		var snapshot statsSnapshot
		if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.SettlementEpoch == nil || *snapshot.SettlementEpoch != 42 {
			t.Fatalf("fresh readiness omitted actual durable initial settlement: %v", err)
		}
	}
	fixture.stateLock.Lock()
	writeRequests := fixture.writeRequests
	fixture.stateLock.Unlock()
	if len(fixture.native.broadcasts) != 0 || writeRequests != 0 {
		t.Fatal("fresh startup invented a native send before the test's trail barrier")
	}
}
