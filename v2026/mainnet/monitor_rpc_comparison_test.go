// Deterministic raw RPC fixtures exercise independently admitted routes,
// original-boundary reads, complete close checks and production event wiring.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"golang.org/x/crypto/blake2b"
)

// Both synthetic authorities sign the whole pair; these keys never leave tests.
func signMonitorRpcComparisonFixture(t *testing.T, policy *monitorRpcComparisonPolicy) {
	t.Helper()
	keys := []ed25519.PrivateKey{ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, ed25519.SeedSize)), ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x32}, ed25519.SeedSize))}
	policy.Primary.ApprovalPublicKey = "0x" + hex.EncodeToString(keys[0].Public().(ed25519.PublicKey))
	policy.Secondary.ApprovalPublicKey = "0x" + hex.EncodeToString(keys[1].Public().(ed25519.PublicKey))
	message, err := policy.signingBytes()
	if err != nil {
		t.Fatal(err)
	}
	policy.Primary.Signature = hex.EncodeToString(ed25519.Sign(keys[0], message))
	policy.Secondary.Signature = hex.EncodeToString(ed25519.Sign(keys[1], message))
}

// Fixture admission uses the production private-file loader before replacing
// only HTTP transports with deterministic raw-response peers.
func newMonitorRpcComparisonFixture(t *testing.T) (*monitorRpcComparison, *identityFinalityFixture, *identityFinalityFixture) {
	t.Helper()
	primary, secondary := newIdentityFinalityFixture(t), newIdentityFinalityFixture(t)
	codeHash, metadataHash := blake2b.Sum256(primary.mapping.runtimeCode), blake2b.Sum256(primary.mapping.runtimeMetadata)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	policy := monitorRpcComparisonPolicy{
		Schema: monitorRpcComparisonPolicySchema, Purpose: monitorRpcComparisonPurpose,
		NotBefore: now.Add(-time.Hour).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano),
		Network:  planNetwork{NativeChain: "synthetic-chain", GenesisHash: testGenesisHash, EvmChainId: mainnetEvmChainId},
		Runtime:  crv4.RuntimeVersionIdentity{SpecName: "synthetic-runtime", SpecVersion: 991, TransactionVersion: 1, StateVersion: 1},
		CodeHash: "0x" + hex.EncodeToString(codeHash[:]), MetadataHash: "0x" + hex.EncodeToString(metadataHash[:]), StorageKeys: []string{"0x0102"},
		Primary:   monitorRpcComparisonRoute{OperatorId: "owned-operator", ProviderId: "owned-provider", RouteId: "owned-route", Url: "https://owned-rpc.example"},
		Secondary: monitorRpcComparisonRoute{OperatorId: "independent-operator", ProviderId: "independent-provider", RouteId: "independent-route", Url: "https://independent-rpc.example"},
	}
	signMonitorRpcComparisonFixture(t, &policy)
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "comparison.json")
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	expected := identityExpectation{NativeChain: policy.Network.NativeChain, GenesisHash: policy.Network.GenesisHash, EvmChainId: policy.Network.EvmChainId}
	owner, err := loadMonitorRpcComparison(t.Context(), path, "sha256:"+hex.EncodeToString(digest[:]), policy.Primary.Url, expected, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.close)
	primary.client, secondary.client = owner.primary, owner.secondary
	for _, fixture := range []*identityFinalityFixture{primary, secondary} {
		fixture.client.httpClient.Transport = roundTripFunc(fixture.roundTrip)
		fixture.reply = func(method string, params []any) (any, bool) {
			if method == "state_getStorage" && len(params) == 2 && params[0] == "0x0102" {
				if params[1] != fixture.mapping.nativeHash {
					t.Fatal("storage comparison retargeted its original block")
				}
				return "0xaabb", true
			}
			return nil, false
		}
	}
	return owner, primary, secondary
}

// Unequal later heads do not change the original native100/EVM37 boundary.
func TestMonitorRpcComparisonAuthenticatesOriginalBoundaryAndAdvance(t *testing.T) {
	owner, primary, secondary := newMonitorRpcComparisonFixture(t)
	secondary.head = 180
	primary.after = func(method string, _ []any) {
		if method == "state_getMetadata" {
			primary.head = 180
		}
	}
	result := owner.compare(t.Context(), primary.mapping.nativeHash)
	if result.Status != "agreement" || !result.IndependentRpc || result.ActivationAuthority || result.Primary == nil || result.Secondary == nil || result.Primary.NativeNumber != 100 || result.Secondary.NativeNumber != 100 || result.Primary.EvmHeader.Number != 37 || result.Secondary.EvmHeader.Number != 37 || result.NativeHash != primary.mapping.nativeHash {
		t.Fatalf("independent fixed-boundary result: %+v", result)
	}
	for _, fixture := range []*identityFinalityFixture{primary, secondary} {
		if fixture.counts["state_getMetadata"] < 2 || fixture.counts["chain_getFinalizedHead"] < 4 || len(fixture.mapping.rawRequests) != 2 || fixture.mapping.rawRequests[0] != fixture.mapping.evmHash || fixture.mapping.rawRequests[1] != fixture.mapping.evmHash {
			t.Fatalf("complete route or closing scope was omitted: counts=%v raw=%v", fixture.counts, fixture.mapping.rawRequests)
		}
	}
}

// Signed approval must not treat different aliases, keys or route names as
// evidence that the same declared provider became an independent source.
func TestMonitorRpcComparisonRejectsAliasedOrUnsignedAuthority(t *testing.T) {
	owner, _, _ := newMonitorRpcComparisonFixture(t)
	expected := identityExpectation{NativeChain: owner.policy.Network.NativeChain, GenesisHash: owner.policy.Network.GenesisHash, EvmChainId: owner.policy.Network.EvmChainId}
	for _, mutate := range []func(*monitorRpcComparisonPolicy){
		func(p *monitorRpcComparisonPolicy) { p.Secondary.ProviderId = p.Primary.ProviderId },
		func(p *monitorRpcComparisonPolicy) { p.Secondary.OperatorId = p.Primary.OperatorId },
		func(p *monitorRpcComparisonPolicy) { p.Secondary.Url = "http://OWNED-RPC.EXAMPLE.:1234/alias" },
		func(p *monitorRpcComparisonPolicy) { p.Purpose = "activate-validator" },
		func(p *monitorRpcComparisonPolicy) { p.Runtime.TransactionVersion++ },
		func(p *monitorRpcComparisonPolicy) { p.Secondary.Signature = p.Primary.Signature },
		func(p *monitorRpcComparisonPolicy) { p.StorageKeys = []string{"0x0102", "0x0102"} },
	} {
		policy := owner.policy
		mutate(&policy)
		if err := policy.validate(policy.Primary.Url, expected, owner.now()); err == nil {
			t.Fatalf("changed route authority was accepted: %+v", policy)
		}
	}
	policy := owner.policy
	policy.Secondary.ProviderId = policy.Primary.ProviderId
	signMonitorRpcComparisonFixture(t, &policy)
	if err := policy.validate(policy.Primary.Url, expected, owner.now()); err == nil {
		t.Fatal("two valid signatures over one provider manufactured independence")
	}
}

// Null is a checked storage absence; it differs from a nonempty storage value.
func TestMonitorRpcComparisonReportsStorageDisagreement(t *testing.T) {
	owner, primary, secondary := newMonitorRpcComparisonFixture(t)
	secondary.reply = func(method string, params []any) (any, bool) {
		return nil, method == "state_getStorage" && len(params) == 2 && params[0] == "0x0102"
	}
	result := owner.compare(t.Context(), primary.mapping.nativeHash)
	if result.Status != "disagreement" || !result.IndependentRpc || result.ActivationAuthority || result.Secondary == nil || result.Secondary.Storage[0].Value != nil || result.Primary.Storage[0].Value == nil {
		t.Fatalf("different storage facts were not retained: %+v", result)
	}
}

// Identical version numbers do not admit altered artifact bytes or metadata.
func TestMonitorRpcComparisonRefusesChangedExactRuntimeAndNetwork(t *testing.T) {
	for _, mutate := range []func(*identityFinalityFixture){
		func(f *identityFinalityFixture) { f.mapping.runtimeMetadata = []byte("synthetic altered metadata") },
		func(f *identityFinalityFixture) { f.mapping.runtimeCode = []byte("synthetic altered code") },
		func(f *identityFinalityFixture) { f.canonicalKVs[0] = "0x" + strings.Repeat("ab", 32) },
		func(f *identityFinalityFixture) {
			f.reply = func(method string, _ []any) (any, bool) { return "foreign-synthetic-chain", method == "system_chain" }
		},
	} {
		owner, primary, secondary := newMonitorRpcComparisonFixture(t)
		mutate(secondary)
		result := owner.compare(t.Context(), primary.mapping.nativeHash)
		if result.Status != "disagreement" || result.IndependentRpc || result.Primary != nil || result.Secondary != nil || result.ActivationAuthority {
			t.Fatalf("incomplete or foreign evidence became agreement: %+v", result)
		}
	}
}

// Losing the primary opening witness during the secondary view invalidates
// the whole pair even while both original100 and new180 remain canonical.
func TestMonitorRpcComparisonClosesOriginalWitnessAfterOtherRoute(t *testing.T) {
	owner, primary, secondary := newMonitorRpcComparisonFixture(t)
	secondary.after = func(method string, _ []any) {
		if method == "state_getMetadata" {
			primary.head = 180
			primary.canonicalKVs[150] = testGenesisHash
		}
	}
	result := owner.compare(t.Context(), primary.mapping.nativeHash)
	if result.Status != "disagreement" || result.IndependentRpc || result.Primary != nil {
		t.Fatalf("changed original witness survived other-route reads: %+v", result)
	}
}

// Missing configuration, a lagging second route and an expired read approval
// remain input unknown; none can be converted into a positive agreement.
func TestMonitorRpcComparisonMissingLaggingAndExpiredAreUnknown(t *testing.T) {
	owner, primary, secondary := newMonitorRpcComparisonFixture(t)
	var missing *monitorRpcComparison
	if result := missing.compare(t.Context(), primary.mapping.nativeHash); result.Status != "input-unknown" || result.IndependentRpc || result.ActivationAuthority {
		t.Fatalf("missing route is not unknown: %+v", result)
	}
	secondary.head = 90
	if result := owner.compare(t.Context(), primary.mapping.nativeHash); result.Status != "input-unknown" || result.IndependentRpc {
		t.Fatalf("lag was treated as agreement/conflict: %+v", result)
	}
	before := primary.counts["system_chain"]
	owner.now = func() time.Time { value, _ := time.Parse(time.RFC3339Nano, owner.policy.ExpiresAt); return value }
	if result := owner.compare(t.Context(), primary.mapping.nativeHash); result.Status != "input-unknown" || result.IndependentRpc || primary.counts["system_chain"] != before {
		t.Fatalf("expired approval read or claimed agreement: %+v", result)
	}
}

// The transport sees one 300s owner with 60s attempts, and cancellation joins
// synchronously without leaking a partially completed primary observation.
func TestMonitorRpcComparisonOwnsBudgetsAndCancellation(t *testing.T) {
	owner, primary, secondary := newMonitorRpcComparisonFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	called := false
	secondary.client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		called = true
		deadline, ok := request.Context().Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining <= 0 || remaining > 60*time.Second || owner.primary.retryWindow != 300*time.Second || owner.secondary.retryWindow != 300*time.Second {
			t.Fatalf("comparison/attempt budgets differ: %s", remaining)
		}
		cancel()
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	result := owner.compare(ctx, primary.mapping.nativeHash)
	if !called || !errors.Is(ctx.Err(), context.Canceled) || result.Status != "input-unknown" || result.IndependentRpc || result.Primary != nil || result.Secondary != nil {
		t.Fatalf("canceled pair published partial authority: %+v", result)
	}
}

// Real chain-worker wiring publishes comparison evidence and chain-scoped
// metrics while preserving independent operator-journal limitations.
func TestMonitorRpcComparisonChainWorkerPublishesResultAndMetrics(t *testing.T) {
	owner, primary, secondary := newMonitorRpcComparisonFixture(t)
	primary.head, secondary.head = 100, 150
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx = context.WithValue(ctx, monitorRpcComparisonContextKey{}, owner)
	var output, stderr bytes.Buffer
	expected := identityExpectation{NativeChain: owner.policy.Network.NativeChain, GenesisHash: owner.policy.Network.GenesisHash, EvmChainId: owner.policy.Network.EvmChainId}
	code := runChainMonitor(ctx, primary.client, expected, "", "", time.Minute, time.Hour, &output, &stderr, owner.now, monitorServiceHooks{afterEvent: func(context.Context, string) { cancel() }})
	if code != 0 {
		t.Fatalf("chain worker: %d %s", code, stderr.String())
	}
	var event monitorEvent
	if err := json.Unmarshal(output.Bytes(), &event); err != nil || event.RpcComparison == nil || event.RpcComparison.Status != "agreement" || !event.RpcComparison.IndependentRpc || event.RpcComparison.ActivationAuthority {
		t.Fatalf("chain comparison event missing: %s err=%v", output.String(), err)
	}
	raw, err := renderMonitorMetrics(event, &monitorState{})
	if err != nil || !bytes.Contains(raw, []byte("sn_mainnet_monitor_rpc_comparison_status 1\n")) || !bytes.Contains(raw, []byte("sn_mainnet_monitor_independent_rpc 1\n")) {
		t.Fatalf("comparison telemetry: %s err=%v", raw, err)
	}
}

// Invalid/missing pins and use outside monitor are refused before any network
// request; the optional capability cannot be silently activated by one flag.
func TestMonitorRpcComparisonCommandRequiresPairedPinnedPolicy(t *testing.T) {
	for _, args := range [][]string{
		{"monitor", "--rpc", "https://owned-rpc.example", "--expected-chain", "synthetic-chain", "--expected-genesis", testGenesisHash, "--expected-evm-chain-id", "964", "--rpc-comparison-policy", "/synthetic/missing.json"},
		{"inspect", "--rpc", "https://owned-rpc.example", "--rpc-comparison-policy", "/synthetic/missing.json", "--rpc-comparison-policy-sha256", "sha256:" + strings.Repeat("a", 64)},
	} {
		var stderr bytes.Buffer
		if code := runMain(t.Context(), args, io.Discard, &stderr); code != 2 {
			t.Fatalf("unpaired policy accepted: code=%d detail=%s", code, stderr.String())
		}
	}
}

// The shared exporter cannot retain an oversized record. Keep it explicitly
// unknown instead of publishing positive metrics for silently dropped facts.
func TestMonitorRpcComparisonOversizedEvidenceCannotClaimAgreement(t *testing.T) {
	owner, primary, secondary := newMonitorRpcComparisonFixture(t)
	for _, fixture := range []*identityFinalityFixture{primary, secondary} {
		fixture.reply = func(method string, params []any) (any, bool) {
			return "0x" + strings.Repeat("ab", maximumMonitorRpcComparisonEventBytes), method == "state_getStorage" && len(params) == 2 && params[0] == "0x0102"
		}
	}
	result := owner.compare(t.Context(), primary.mapping.nativeHash)
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > maximumMonitorRpcComparisonEventBytes || result.Status != "input-unknown" || result.IndependentRpc || result.Primary != nil || result.Secondary != nil {
		t.Fatalf("oversized comparison claimed retained agreement: size=%d result=%+v err=%v", len(raw), result, err)
	}
}

// File authentication is independent of valid inner signatures; changing the
// original bytes or using an unreviewed file pin must fail before route reads.
func TestMonitorRpcComparisonRequiresExactProtectedPolicyBytes(t *testing.T) {
	owner, _, _ := newMonitorRpcComparisonFixture(t)
	raw, err := json.Marshal(owner.policy)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "policy.json")
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	expected := identityExpectation{NativeChain: owner.policy.Network.NativeChain, GenesisHash: owner.policy.Network.GenesisHash, EvmChainId: owner.policy.Network.EvmChainId}
	if changed, err := loadMonitorRpcComparison(t.Context(), path, "sha256:"+hex.EncodeToString(digest[:]), owner.policy.Primary.Url, expected, owner.now); err == nil || changed != nil {
		t.Fatal("changed original policy bytes borrowed approval")
	}
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if changed, err := loadMonitorRpcComparison(t.Context(), path, "sha256:"+hex.EncodeToString(digest[:]), owner.policy.Primary.Url, expected, owner.now); err == nil || changed != nil {
		t.Fatal("unprotected policy bytes admitted routes")
	}
}

// An EVM-only canonical-route change during the other peer's read must also
// invalidate the pair; native finality by itself cannot close that read view.
func TestMonitorRpcComparisonClosesEvmMappingAfterOtherRoute(t *testing.T) {
	owner, primary, secondary := newMonitorRpcComparisonFixture(t)
	secondary.after = func(method string, _ []any) {
		if method == "state_getMetadata" {
			primary.mapping.fault = func(method string, _ []any, _ int) (any, bool) {
				return map[string]any{"hash": testGenesisHash, "number": "0x25", "transactions": []string{}, "timestamp": "0x6553f100", "baseFeePerGas": "0x1"}, method == "eth_getBlockByNumber"
			}
		}
	}
	result := owner.compare(t.Context(), primary.mapping.nativeHash)
	if result.Status != "disagreement" || result.IndependentRpc || result.Primary != nil || result.Secondary != nil {
		t.Fatalf("late EVM route replacement survived comparison: %+v", result)
	}
}

// An unavailable optional policy is comparison-local: the actual chain worker
// still emits its authenticated observation and can be stopped normally.
func TestMonitorRpcComparisonUnavailablePolicyKeepsChainWorkerAlive(t *testing.T) {
	owner, primary, _ := newMonitorRpcComparisonFixture(t)
	primary.head = 100
	expected := identityExpectation{NativeChain: owner.policy.Network.NativeChain, GenesisHash: owner.policy.Network.GenesisHash, EvmChainId: owner.policy.Network.EvmChainId}
	request := &monitorRpcComparisonRequest{path: filepath.Join(t.TempDir(), "missing.json"), pin: owner.policyPin, primaryUrl: owner.policy.Primary.Url, expected: expected, now: owner.now}
	defer request.close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx = context.WithValue(ctx, monitorRpcComparisonContextKey{}, request)
	var output, stderr bytes.Buffer
	code := runChainMonitor(ctx, primary.client, expected, "", "", time.Minute, time.Hour, &output, &stderr, owner.now, monitorServiceHooks{afterEvent: func(context.Context, string) { cancel() }})
	var event monitorEvent
	err := json.Unmarshal(output.Bytes(), &event)
	if code != 0 || err != nil || event.Status != "ok" || event.Snapshot == nil || event.RpcComparison == nil || event.RpcComparison.Status != "input-unknown" || event.RpcComparison.IndependentRpc || request.owner != nil {
		t.Fatalf("missing optional comparison policy stopped or approved chain: code=%d output=%s stderr=%s err=%v", code, output.String(), stderr.String(), err)
	}
}

// Approval expiry clips actual read attempts, rather than merely rejecting a
// result after spending the remainder of a longer generic read budget.
func TestMonitorRpcComparisonApprovalExpiryBoundsActualAttempt(t *testing.T) {
	owner, primary, secondary := newMonitorRpcComparisonFixture(t)
	expiresAt, err := time.Parse(time.RFC3339Nano, owner.policy.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	owner.now = func() time.Time { return expiresAt.Add(-30 * time.Second) }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	called := false
	secondary.client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		called = true
		deadline, ok := request.Context().Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining <= 0 || remaining > 30*time.Second {
			t.Fatalf("signed approval did not bound actual attempt: %s", remaining)
		}
		cancel()
		return nil, request.Context().Err()
	})
	result := owner.compare(ctx, primary.mapping.nativeHash)
	if !called || result.Status != "input-unknown" || result.IndependentRpc || result.ActivationAuthority {
		t.Fatalf("expiry-bounded canceled comparison claimed authority: %+v", result)
	}
}
