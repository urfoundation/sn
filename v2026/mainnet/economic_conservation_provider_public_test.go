// The real combined command acquires optional provider originals inside its
// existing isolated entitlement worker. Missing work never erases money facts.
package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/validator"
)

// The missing-work controls stop at the first actual companion read. A pinned
// later authority file exists, but its contents cannot authorize missing work.
func economicProviderUnavailableFixture(t *testing.T) *economicEntitlementFixture {
	t.Helper()
	f := newEconomicEntitlementFixture(t)
	raw := []byte("{}\n")
	path := filepath.Join(filepath.Dir(f.source.path), "synthetic-provider-attempt-authority.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	source := &f.source.policy.EntitlementSources.Sources[0]
	source.ProviderMeasurements = &economicProviderMeasurementPolicy{Schema: economicProviderMeasurementPolicySchema, WholeWorkAuthoritySigner: "0x0000000000000000000000000000000000007890", WalletEndpoint: source.Endpoint, AttemptAuthority: validator.ReleaseEvidenceV2File{Path: path, Bytes: uint64(len(raw)), SHA256: monitorReadDigest(raw)}, MaximumOriginalBytes: 256 * 1024}
	f.source.writePolicy(t)
	return f
}

// The missing raw witness is retryable across a cold public reopen. Actual
// native/vault/Claim observations and original paid-source identity progress.
func TestEconomicProviderPublicMissingWorkKeepsIndependentMoneyProgress(t *testing.T) {
	f := economicProviderUnavailableFixture(t)
	var workReads atomic.Uint64
	f.artifactHook = func(w http.ResponseWriter, request *http.Request) bool {
		if request.URL.Path != "/provider-work/v1/windows" {
			return false
		}
		workReads.Add(1)
		http.NotFound(w, request)
		return true
	}
	economicEntitlementFirstPage(t, f)
	before := f.source.state(t)
	claims := f.source.claimReads.Load()
	for page := uint64(1); page <= 2; page++ {
		summary, code, issue := f.source.run(t, monitorServiceHooks{})
		record := economicEntitlementRecord(t, f.source)
		state := f.source.state(t)
		if code != 3 || !summary.NativeCurrent || !summary.VaultCurrent || record.Census != nil || record.CensusHeld || record.CensusIssue == "" || workReads.Load() != page || f.source.claimReads.Load() <= claims || len(state.Payments) != 1 || len(state.Lots) != len(before.Lots) || state.EntitlementReadAfter["1"] != record.Id {
			t.Fatal("missing provider work held a healthy domain or changed original funding", page, code, issue, record, summary)
		}
		if summary.OriginalEntitlements == nil || summary.OriginalEntitlements.ProviderMeasurementsAuthenticated || summary.OriginalEntitlements.ProviderMeasurementRoots != 0 || summary.TargetMet != nil {
			t.Fatal("missing companion became a complete provider measurement", summary)
		}
		claims = f.source.claimReads.Load()
	}
}

// Cancellation occurs in the actual provider GET after the artifact and its
// on-chain authorization were read; no detached projection is published.
func TestEconomicProviderPublicPendingWorkJoinsOwnerCancellation(t *testing.T) {
	f := economicProviderUnavailableFixture(t)
	economicEntitlementFirstPage(t, f)
	before, err := os.ReadFile(f.source.checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	joined := make(chan struct{})
	var entered atomic.Uint64
	f.artifactHook = func(_ http.ResponseWriter, request *http.Request) bool {
		if request.URL.Path != "/provider-work/v1/windows" {
			return false
		}
		entered.Add(1)
		defer close(joined)
		cancel()
		<-request.Context().Done()
		return true
	}
	var output, diagnostic bytes.Buffer
	code := runMonitorStorageTestWithHooks(t, ctx, f.source.args(t), &output, &diagnostic, func() time.Time { return f.source.now }, monitorServiceHooks{})
	select {
	case <-joined:
	case <-time.After(10 * time.Second):
		t.Fatal("actual provider request did not join owner cancellation")
	}
	after, err := os.ReadFile(f.source.checkpoint)
	if err != nil || code != 0 || entered.Load() != 1 || output.Len() != 0 || !bytes.Equal(before, after) || strings.Contains(diagnostic.String(), "integrity") {
		t.Fatal("canceled provider work published partial original authority", code, diagnostic.String(), err)
	}
}
