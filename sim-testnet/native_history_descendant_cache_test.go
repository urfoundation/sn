// Exercise real cache authentication and production native replay. The
// synthetic archive deliberately refuses uncached history, making redundant
// recovery reads observable without timing or a live node.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/crv4"
)

// Give the native fixture a complete archived approval for descendant checks.
func newNativeHistoryDescendantFixture(t *testing.T) *nativeHistoryCacheFixture {
	t.Helper()
	fixture := newNativeHistoryCacheFixture(t)
	cfg := fixture.executor.cfg
	plan := fixture.executor.plan
	plan.Schema, plan.Release = "urnetwork-sim-plan-v1", "1.0"
	plan.Netuid, plan.Owner, plan.PolicyHash = cfg.Netuid, cfg.WalletPublic, cfg.PolicyHash
	plan.Limits = configuredPlanLimits(cfg)
	var err error
	plan.ReleaseLockHash, err = canonicalHashHex(cfg.Release)
	if err != nil {
		t.Fatal(err)
	}
	plan.ResolvedInputsHash, err = resolvedInputsHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanHash, err = plan.hash()
	if err != nil {
		t.Fatal(err)
	}
	historicalAuditDescendantArchive(t, fixture.executor)
	return fixture
}

// Preserve the authorized transport identity while changing only the reviewed
// runner source and its approved descendant plan.
func nativeHistoryDescendantRelease(t *testing.T, fixture *nativeHistoryCacheFixture) {
	t.Helper()
	fixture.executor = historicalAuditDescendantRelease(t, fixture.executor, true)
	fixture.executor.auditAuthorizedConfig = fixture.executor.cfg
}

// A later failure must not force a compatible build to repeat completed
// immutable native work; canonical/finalized checks remain fresh each time.
func TestNativeHistoryDescendantRetainsCompletedProof(t *testing.T) {
	fixture := newNativeHistoryDescendantFixture(t)
	fixture.retain(t)
	nativeHistoryDescendantRelease(t, fixture)
	for index := 0; index < 2; index++ {
		if err := fixture.executor.verifySubstrateTransactionEvidenceAtHead(t.Context(), fixture.recorded, fixture.transaction, fixture.finalized); err != nil {
			t.Fatalf("compatible recovery repeated immutable native proof: %v", err)
		}
	}
	if fixture.canonicalCalls.Load() != 2 || fixture.historyCalls.Load() != 0 {
		t.Fatalf("recovery reads canonical=%d historical=%d", fixture.canonicalCalls.Load(), fixture.historyCalls.Load())
	}
	fixture.canonical = "0x" + strings.Repeat("ab", 32)
	if err := fixture.executor.verifySubstrateTransactionEvidenceAtHead(t.Context(), fixture.recorded, fixture.transaction, fixture.finalized); err == nil {
		t.Fatal("descendant proof bypassed a changed canonical block")
	}
}

// Immutable successes are action-granular. Cancellation leaves the unfinished
// transaction cold while completed siblings remain independently reusable.
func TestNativeHistoryDescendantRetainsOnlyCompletedTransactions(t *testing.T) {
	fixture := newNativeHistoryDescendantFixture(t)
	fixture.retain(t)
	incompleteTransaction := "0x" + strings.Repeat("ef", 32)
	input, err := nativeHistoryCacheInput(fixture.executor.cfg, fixture.recorded, incompleteTransaction, fixture.client.URL())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	_, err = fixture.executor.withHistoricalAuditCache(ctx, historicalNativeExtrinsicCacheKind, input, func(context.Context) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled proof result=%v", err)
	}
	nativeHistoryDescendantRelease(t, fixture)
	if err := fixture.executor.verifySubstrateTransactionEvidenceAtHead(t.Context(), fixture.recorded, fixture.transaction, fixture.finalized); err != nil {
		t.Fatal(err)
	}
	if err := fixture.executor.verifySubstrateTransactionEvidenceAtHead(t.Context(), fixture.recorded, incompleteTransaction, fixture.finalized); err == nil || fixture.historyCalls.Load() != 1 {
		t.Fatalf("unfinished sibling inherited success: err=%v historical=%d", err, fixture.historyCalls.Load())
	}
}

// A compatible build may not broaden either the observer, approval domain or
// reviewed runtime identity. Each mismatch must reach the rejecting archive.
func TestNativeHistoryDescendantRejectsChangedAuthority(t *testing.T) {
	for _, mutation := range []string{"observer", "transaction", "runtime", "runtime-hash", "policy", "owner", "foreign-plan", "missing-plan", "tampered-plan"} {
		fixture := newNativeHistoryDescendantFixture(t)
		fixture.retain(t)
		source := fixture.executor.plan
		nativeHistoryDescendantRelease(t, fixture)
		switch mutation {
		case "observer":
			fixture.client.endpoint = "wss://different-native-observer.example"
		case "transaction":
			fixture.transaction = "0x" + strings.Repeat("ab", 32)
		case "runtime":
			fixture.executor.cfg.Release.Runtime.SpecVersion++
		case "runtime-hash":
			fixture.executor.cfg.Release.Runtime.MetadataHash = "0x" + strings.Repeat("ab", 32)
		case "policy":
			fixture.executor.cfg.PolicyHash = "0x" + strings.Repeat("ab", 32)
		case "owner":
			fixture.executor.cfg.WalletPublic += "synthetic-change"
		case "foreign-plan":
			fixture.executor.plan.PriorPlanHashes = nil
		case "missing-plan":
			if err := os.Remove(filepath.Join(fixture.executor.stateDir, "plans", stringsTrim0x(source.PlanHash)+".json")); err != nil {
				t.Fatal(err)
			}
		case "tampered-plan":
			path := filepath.Join(fixture.executor.stateDir, "plans", stringsTrim0x(source.PlanHash)+".json")
			if err := os.WriteFile(path, []byte(`{"plan_hash":"tampered"}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := fixture.executor.verifySubstrateTransactionEvidenceAtHead(t.Context(), fixture.recorded, fixture.transaction, fixture.finalized); err == nil {
			t.Fatalf("changed %s reused strict native success", mutation)
		}
	}
}

// Provisional verification cannot read or write the strict cache, even if the
// same configured approval and runtime pins remain in the public manifests.
func TestNativeHistoryCacheProvisionalCannotSeedStrictSuccess(t *testing.T) {
	fixture := newNativeHistoryDescendantFixture(t)
	input, err := nativeHistoryCacheInput(fixture.executor.cfg, fixture.recorded, fixture.transaction, fixture.client.URL())
	if err != nil {
		t.Fatal(err)
	}
	fixture.executor.cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{Provisional: true}}
	if !provisionalResumeEnabled(fixture.executor.cfg) {
		t.Fatal("provisional boundary was not exercised")
	}
	if hit, err := fixture.executor.withHistoricalAuditCache(t.Context(), historicalNativeExtrinsicCacheKind, input, func(context.Context) error { return nil }); hit || err != nil {
		t.Fatalf("provisional read behavior: hit=%t err=%v", hit, err)
	}
	fixture.executor.cfg.provisionalResume = nil
	if err := fixture.executor.verifySubstrateTransactionEvidenceAtHead(t.Context(), fixture.recorded, fixture.transaction, fixture.finalized); err == nil || fixture.historyCalls.Load() != 1 {
		t.Fatalf("provisional proof supplied strict authority: err=%v historical=%d", err, fixture.historyCalls.Load())
	}
}

// Neither untyped old proof input nor a changed catalogue binding may acquire
// descendant metadata through a generic cache call.
func TestNativeHistoryCacheRejectsOpaqueOrChangedCatalogueInput(t *testing.T) {
	fixture := newNativeHistoryDescendantFixture(t)
	input, err := nativeHistoryCacheInput(fixture.executor.cfg, fixture.recorded, fixture.transaction, fixture.client.URL())
	if err != nil {
		t.Fatal(err)
	}
	changed := input
	changed.RuntimeArtifactsHash = "0x" + strings.Repeat("ab", 32)
	for _, candidate := range []any{map[string]any{"recorded": fixture.recorded, "transaction": fixture.transaction, "observer": fixture.client.URL()}, changed} {
		if entry, hit := fixture.executor.lookupHistoricalAuditCache(t.Context(), historicalNativeExtrinsicCacheKind, candidate); entry != nil || hit {
			t.Fatal("opaque or changed runtime proof gained reusable authority")
		}
	}
}

// Reusing a connection after provisional recovery cannot preserve that
// connection's broader runtime admission inside a strict evidence verifier.
func TestNativeHistoryStrictReadRejectsPriorProvisionalConnection(t *testing.T) {
	cfg := provisionalRuntimeConfigTest(t)
	chain := provisionalRuntimeChainTest(t, cfg)
	if _, err := readReleaseHistoryRuntimeMetadataAtContext(t.Context(), chain, cfg, types.Hash{2}); err != nil {
		t.Fatalf("prime actual provisional reader: %v", err)
	}
	cfg.provisionalResume = nil
	for _, read := range []func(context.Context, *crv4.Chain, *ResolvedConfig, types.Hash) (authenticatedRuntimeMetadata, error){readAuthenticatedRuntimeMetadataAtContext, readReleaseHistoryRuntimeMetadataAtContext} {
		if _, err := read(t.Context(), chain, cfg, types.Hash{2}); err == nil || !strings.Contains(err.Error(), "provisional runtime artifact cannot authorize") {
			t.Fatalf("shared connection retained provisional runtime authority: %v", err)
		}
	}
}
