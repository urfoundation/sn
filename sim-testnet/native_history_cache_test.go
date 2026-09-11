package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	"github.com/urfoundation/sn/v2026/crv4"
)

type nativeHistoryCacheTestClient struct {
	releaseRuntimeTestClient
	endpoint string
}

func (c *nativeHistoryCacheTestClient) URL() string { return c.endpoint }

type nativeHistoryCacheFixture struct {
	executor       *Executor
	client         *nativeHistoryCacheTestClient
	recorded       ChainHead
	finalized      ChainHead
	transaction    string
	canonical      string
	canonicalCalls atomic.Int64
	historyCalls   atomic.Int64
}

func newNativeHistoryCacheFixture(t *testing.T) *nativeHistoryCacheFixture {
	t.Helper()
	cfg := testResolvedConfig(t)
	f := &nativeHistoryCacheFixture{
		recorded:    ChainHead{Number: 10, Hash: "0x" + strings.Repeat("12", 32)},
		finalized:   ChainHead{Number: 20, Hash: "0x" + strings.Repeat("34", 32)},
		transaction: "0x" + strings.Repeat("56", 32),
	}
	f.canonical = f.recorded.Hash
	f.client = &nativeHistoryCacheTestClient{endpoint: "wss://native-history-cache.test"}
	f.client.callContext = func(_ context.Context, result any, method string, args ...any) error {
		if method == "chain_getBlockHash" {
			f.canonicalCalls.Add(1)
			if len(args) != 1 || fmt.Sprint(args[0]) != "10" {
				return fmt.Errorf("unexpected canonical block request: %v", args)
			}
			return setReleaseRuntimeTestResult(result, f.canonical)
		}
		f.historyCalls.Add(1)
		return errors.New("state already discarded")
	}
	f.executor = &Executor{
		cfg: cfg, auditAuthorizedConfig: cfg, stateDir: t.TempDir(),
		plan: &SetupPlan{
			PlanHash: "0x" + strings.Repeat("78", 32), ReleaseLockHash: "0x" + strings.Repeat("90", 32),
			DeploymentID: cfg.Config.Deployment.DeploymentID, ChainID: cfg.ChainID,
			GenesisHash: cfg.Public.Chain.GenesisHash, ConfigHash: cfg.ConfigHash,
		},
		substrate: &SubstrateManager{chain: &crv4.Chain{API: &gsrpc.SubstrateAPI{Client: f.client}}},
	}
	return f
}

// Model a previously successful expensive proof through the real authenticated
// cache API, then exercise the production caller with a freshly built executor.
func (f *nativeHistoryCacheFixture) retain(t *testing.T) {
	t.Helper()
	hit, err := f.executor.withHistoricalAuditCache(context.Background(), "finalized-native-extrinsic", struct {
		Recorded    ChainHead `json:"recorded"`
		Transaction string    `json:"transaction"`
		Observer    string    `json:"observer"`
	}{f.recorded, f.transaction, f.client.URL()}, func(context.Context) error { return nil })
	if err != nil || hit {
		t.Fatalf("cold successful proof: hit=%t err=%v", hit, err)
	}
	f.executor = &Executor{
		cfg: f.executor.cfg, auditAuthorizedConfig: f.executor.auditAuthorizedConfig,
		stateDir: f.executor.stateDir, plan: f.executor.plan, substrate: f.executor.substrate,
	}
}

func TestNativeHistoryCacheReopenStillChecksCanonicalBlock(t *testing.T) {
	f := newNativeHistoryCacheFixture(t)
	f.retain(t)
	for i := 0; i < 2; i++ {
		if err := f.executor.verifySubstrateTransactionEvidenceAtHead(context.Background(), f.recorded, f.transaction, f.finalized); err != nil {
			t.Fatal(err)
		}
	}
	if f.canonicalCalls.Load() != 2 || f.historyCalls.Load() != 0 {
		t.Fatalf("warm reads: canonical=%d historical=%d, want 2/0", f.canonicalCalls.Load(), f.historyCalls.Load())
	}
}

func TestNativeHistoryCacheRejectsChangedOrUnfinalizedBlock(t *testing.T) {
	for _, changed := range []string{"canonical", "finalized"} {
		t.Run(changed, func(t *testing.T) {
			f := newNativeHistoryCacheFixture(t)
			f.retain(t)
			if changed == "canonical" {
				f.canonical = "0x" + strings.Repeat("ab", 32)
			} else {
				f.finalized.Number = f.recorded.Number - 1
			}
			if err := f.executor.verifySubstrateTransactionEvidenceAtHead(context.Background(), f.recorded, f.transaction, f.finalized); err == nil {
				t.Fatal("cached success bypassed fresh finality/canonical verification")
			}
			if f.canonicalCalls.Load() != 1 || f.historyCalls.Load() != 0 {
				t.Fatalf("fresh refusal reads: canonical=%d historical=%d", f.canonicalCalls.Load(), f.historyCalls.Load())
			}
		})
	}
}

func TestNativeHistoryCacheDoesNotShareObserversOrTransactions(t *testing.T) {
	for _, changed := range []string{"observer", "transaction"} {
		t.Run(changed, func(t *testing.T) {
			f := newNativeHistoryCacheFixture(t)
			f.retain(t)
			if changed == "observer" {
				f.client.endpoint = "wss://independent-native-history-cache.test"
			} else {
				f.transaction = "0x" + strings.Repeat("cd", 32)
			}
			for i := 0; i < 2; i++ {
				if err := f.executor.verifySubstrateTransactionEvidenceAtHead(context.Background(), f.recorded, f.transaction, f.finalized); err == nil {
					t.Fatal("different observer/transaction reused a prior proof")
				}
			}
			if f.canonicalCalls.Load() != 2 || f.historyCalls.Load() != 2 {
				t.Fatalf("new failed proof must retry next invocation: canonical=%d historical=%d, want 2/2", f.canonicalCalls.Load(), f.historyCalls.Load())
			}
		})
	}
}
