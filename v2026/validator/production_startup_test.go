//go:build linux || darwin

// The public root opens real dormant disk stores, authenticates signed
// activation/history, starts actual operator sessions and reconciles native work.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/stabi"
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
	productionStartupTestInitialDeployment(t, newProductionStartupTestFixture(t))
}

// VA-9: the signed activation authenticates against the evidence journal and
// the initial settlement publication reaches readiness unchanged under a
// production config carrying settlement_v2, after its identity check. The
// census of a closed epoch is TestSettlementV2ClosedCensusContinuesAcrossAdoption.
func TestProductionStartupRunReleaseInitialDeploymentWithSettlementV2(t *testing.T) {
	productionStartupTestInitialDeployment(t, newProductionStartupSettlementV2TestFixture(t))
}

// A fresh deployment up to its first trail seed request.
func productionStartupTestInitialDeployment(t *testing.T, fixture *productionStartupTestFixture) {
	t.Helper()
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

// The startup config also carries settlement_v2. No pool is registered in
// vault2 yet, as before E_s, so every epoch keeps its operatorAt destination.
func newProductionStartupSettlementV2TestFixture(t *testing.T) *productionStartupTestFixture {
	t.Helper()
	block := settlementV2TestBlock
	return newProductionStartupTestFixtureWithConfig(t, false, func(cfg *ReleaseConfig) { cfg.SettlementV2 = &block })
}

// The chain's honest settlement v2 views for cfg: the three identity getters
// and an unregistered vault2 pool for every configured operator.
func settlementV2TestStartupViews(t *testing.T, cfg *ReleaseConfig) map[string][]byte {
	t.Helper()
	views := settlementV2TestAnswers(cfg.Coordinator)
	vaultABI, err := stabi.STSettlementVault2MetaData.ParseABI()
	if err != nil {
		t.Fatal(err)
	}
	unregistered, err := vaultABI.Methods["pools"].Outputs.Pack(stabi.ISTSettlementVault2Pool{})
	if err != nil {
		t.Fatal(err)
	}
	for _, operator := range cfg.Operators {
		calldata := stabi.NewSTSettlementVault2().PackPools(new(big.Int).SetUint64(operator.NoID))
		views[cfg.SettlementV2.Vault+"/"+common.Bytes2Hex(calldata)] = unregistered
	}
	return views
}

// VA-6: when STSettlement2 names another vault, the validator stops at its
// first finalized snapshot, before trail seeding or any native send.
func TestProductionStartupRunReleaseRefusesSettlementV2Mismatch(t *testing.T) {
	fixture := newProductionStartupSettlementV2TestFixture(t)
	fixture.evm.settlementV2[settlementV2TestKey(settlementV2TestBlock.Settlement, "vault()")] = settlementV2TestWord("0x5e77000000000000000000000000000000000003")
	fixture.selectEmptyDeployment(t)
	err := RunRelease(fixture.storageContext(t.Context()), fixture.configPath)
	if err == nil || !strings.Contains(err.Error(), "settlement_v2 STSettlement2.vault() at block") {
		t.Fatalf("startup admitted another vault or stopped for another cause: %v", err)
	}
	for _, api := range fixture.origins {
		select {
		case <-api.seedRead:
			t.Fatal("trail seeding began under a refused settlement v2 identity")
		default:
		}
	}
	fixture.stateLock.Lock()
	writeRequests := fixture.writeRequests
	fixture.stateLock.Unlock()
	if len(fixture.native.broadcasts) != 0 || writeRequests != 0 {
		t.Fatal("refused settlement v2 identity reached a native send")
	}
}
