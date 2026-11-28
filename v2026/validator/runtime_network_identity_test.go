// Public runtime consumers retain the first completed identity contradiction,
// while missing replies recover through the original bounded read owner.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// Each case retains the public reader's real signed fixture and finality checks.
// The original mock owns reply instrumentation even when the installed client
// wraps its route; no retry or authority verdict is substituted.
type runtimeNetworkIdentityFixture struct {
	client    *validatorRuntimeIdentityTestClient
	native    *crv4.Chain
	read      func(context.Context) error
	succeeded int
}

// The five public consumer paths use independent synthetic approvals and the
// original selected block. Every error must discard its entire public result.
func newRuntimeNetworkIdentityFixture(t *testing.T, reader string) *runtimeNetworkIdentityFixture {
	t.Helper()
	self := &runtimeNetworkIdentityFixture{}
	switch reader {
	case "mainnet observation":
		fixture := newMainnetRuntimeTestFixture(t)
		cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		self.native = fixture.native
		self.client = fixture.client
		self.read = func(ctx context.Context) error {
			result, err := ObserveMainnetRuntimeAtContext(ctx, fixture.native, cfg, mainnetRuntimeTestBlock(100))
			if err != nil {
				if result != nil {
					t.Fatal("failed mainnet identity published an observation")
				}
				return err
			}
			if result == nil || result.BlockHash != mainnetRuntimeTestBlock(100) || result.GenesisHash != fixture.native.GenesisHash {
				t.Fatal("mainnet identity changed its original block or genesis")
			}
			self.succeeded++
			return nil
		}
	case "production artifact":
		fixture := newProductionRuntimeTestFixture(t, true)
		self.native = fixture.rpc.native
		self.client = fixture.rpc.client
		self.read = func(ctx context.Context) error {
			result, number, err := authenticateOwnerRecycleProductionArtifactAtContext(ctx, fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(100), true)
			if err != nil {
				if result.BlockHash != (types.Hash{}) || number != 0 {
					t.Fatal("failed production identity published an artifact")
				}
				return err
			}
			if result.BlockHash != mainnetRuntimeTestBlock(100) || number != 100 || result.GenesisHash != fixture.rpc.native.GenesisHash {
				t.Fatal("production identity changed its historical artifact")
			}
			self.succeeded++
			return nil
		}
	case "runtime continuity":
		fixture := newProductionContinuityPolicyTestFixture(t)
		self.native = fixture.owner.rpc.native
		self.client = fixture.owner.rpc.client
		self.read = func(ctx context.Context) error {
			result, err := InspectProductionRuntimeContinuityContext(ctx, fixture.owner.rpc.native, fixture.owner.cfg, fixture.hashes[150], fixture.policyRaw, fixture.certificateRaw)
			if err != nil {
				if result != nil {
					t.Fatal("failed continuity identity published an inspection")
				}
				return err
			}
			if result == nil || result.NativeHash != fixture.hashes[150] || result.ProductionSelectionInstalled || result.SigningAuthority {
				t.Fatal("continuity identity changed its block or granted authority")
			}
			self.succeeded++
			return nil
		}
	case "recycle census":
		fixture := newRecycleAdmissionFixture(t, nil)
		fixture.retain(t)
		self.native = fixture.chain
		self.client = fixture.client
		self.read = func(ctx context.Context) error {
			result, err := ObserveOwnerRecycleAdmissionAt(ctx, fixture.cfg, fixture.chain, [32]byte(fixture.finalized))
			if err != nil {
				if result != nil {
					t.Fatal("failed recycle identity published a census")
				}
				return err
			}
			if result == nil || result.Snapshot.FinalizedHash != [32]byte(fixture.finalized) || result.ActivationReady || result.NativeOutcomeVerified {
				t.Fatal("recycle identity changed its census or granted activation")
			}
			self.succeeded++
			return nil
		}
	case "upload observer":
		fixture := newValidatorUploadProductionTestFixture(t)
		owner := fixture.owner(t)
		self.native = fixture.production.rpc.native
		self.client = fixture.production.rpc.client
		self.read = func(ctx context.Context) error {
			result, err := ValidatorUploadNativeObserverContext(ctx, self.native, owner.config.Deployment)
			if err != nil {
				if result != (ValidatorUploadNativeObserver{}) {
					t.Fatal("failed upload identity published a native observer")
				}
				return err
			}
			if result.Hash != mainnetRuntimeTestBlock(fixture.production.rpc.head) || result.Number != fixture.production.rpc.head || result.TimestampMillis != fixture.upload.currentMillis {
				t.Fatal("upload identity changed its original finalized observer")
			}
			self.succeeded++
			return nil
		}
	default:
		t.Fatal("unknown runtime identity reader", reader)
	}
	return self
}

// Fault injection reaches the installed transport without replacing its route
// wrapper or guessing the concrete type exposed through the client interface.
func TestRuntimeNetworkIdentityFixtureRetainsInstalledClientInstrumentation(t *testing.T) {
	for _, reader := range []string{"mainnet observation", "production artifact", "runtime continuity", "recycle census", "upload observer"} {
		fixture := newRuntimeNetworkIdentityFixture(t, reader)
		installed := fixture.native.API.Client
		route := installed.URL()
		failure := errors.New("synthetic fixture instrumentation")
		calls := 0
		fixture.client.callContext = func(_ context.Context, _ any, method string, _ ...any) error {
			calls++
			if method != "synthetic_fixture_probe" {
				t.Fatalf("%s changed the instrumented method: %s", reader, method)
			}
			return failure
		}
		err := installed.CallContext(t.Context(), nil, "synthetic_fixture_probe")
		if err != failure || calls != 1 || fixture.native.API.Client != installed || installed.URL() != route {
			t.Fatalf("%s lost its installed client or instrumentation: calls=%d route=%s error=%v", reader, calls, installed.URL(), err)
		}
	}
}

// Independent recycle fixtures retain their own mock while the approved route
// remains visible to the actual public reader through each installed wrapper.
func TestRuntimeNetworkIdentityFixtureKeepsIndependentWrappedClients(t *testing.T) {
	first := newRuntimeNetworkIdentityFixture(t, "recycle census")
	second := newRuntimeNetworkIdentityFixture(t, "recycle census")
	if first.client == second.client || first.native.API.Client == first.client || second.native.API.Client == second.client {
		t.Fatal("recycle fixtures shared a mock or discarded their route wrapper")
	}
	failure := errors.New("synthetic first fixture failure")
	first.client.callContext = func(context.Context, any, string, ...any) error { return failure }
	var chain string
	if err := first.native.API.Client.CallContext(t.Context(), &chain, "system_chain"); err != failure {
		t.Fatalf("first fixture lost its injected error: %v", err)
	}
	if err := second.native.API.Client.CallContext(t.Context(), &chain, "system_chain"); err != nil || chain == "" {
		t.Fatalf("first fixture changed its independent peer: chain=%q error=%v", chain, err)
	}
	for _, fixture := range []*runtimeNetworkIdentityFixture{first, second} {
		if fixture.native.API.Client.URL() != "wss://recycle.example" {
			t.Fatalf("instrumentation changed the approved recycle route: %s", fixture.native.API.Client.URL())
		}
	}
}

// The genesis method is also used for closing canonical witnesses. Only the
// original height-zero identity reply belongs to these faults.
func runtimeNetworkIdentityMethod(method string, args []any, selected string) bool {
	return method == selected && (method != "chain_getBlockHash" || len(args) == 1 && args[0] == uint64(0))
}

// All cases enter the actual production read owner. A later read is armed to
// time out, but a completed wrong identity must stop before that read or retry.
func TestRuntimeNetworkIdentityStopsAtFirstCompletedContradiction(t *testing.T) {
	ownerConfig := newProductionRuntimeTestFixture(t, false).cfg
	for _, reader := range []string{"mainnet observation", "production artifact", "runtime continuity", "recycle census", "upload observer"} {
		for _, fault := range []struct {
			method string
			value  any
		}{
			{method: "system_chain", value: "Different Synthetic Chain"},
			{method: "chain_getBlockHash", value: types.Hash{0xe7}.Hex()},
			{method: "eth_chainId", value: "0x1"},
		} {
			fixture := newRuntimeNetworkIdentityFixture(t, reader)
			original := fixture.client.callContext
			faulted, later, waits := false, 0, 0
			fixture.client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
				if faulted {
					later++
					return context.DeadlineExceeded
				}
				if runtimeNetworkIdentityMethod(method, args, fault.method) {
					faulted = true
					return setReleaseHistoricalTestResult(target, fault.value)
				}
				return original(ctx, target, method, args...)
			}
			owner := &ReleaseSteerer{cfg: ownerConfig}
			owner.productionReadHooks.wait = func(context.Context, time.Duration) error {
				waits++
				return errors.New("synthetic unexpected identity retry")
			}
			err := owner.productionRead(t.Context(), productionReadPreparation, nil, fixture.read)
			var pending *productionSteeringReadWait
			if !faulted || later != 0 || waits != 0 || err == nil || retryableProductionSteeringRead(err) || errors.As(err, &pending) ||
				!strings.Contains(err.Error(), "runtime network identity") || fixture.succeeded != 0 {
				t.Fatalf("%s %s lost its first completed contradiction: later=%d waits=%d error=%v", reader, fault.method, later, waits, err)
			}
		}
	}
}

// A null or empty RPC value cannot establish a conflicting approved identity.
// It also cannot authorize any artifact, census, or continuity result.
func TestRuntimeNetworkIdentityMissingReplyRemainsUnavailable(t *testing.T) {
	for _, reader := range []string{"mainnet observation", "production artifact", "runtime continuity", "recycle census", "upload observer"} {
		for _, method := range []string{"system_chain", "chain_getBlockHash", "eth_chainId"} {
			for _, raw := range []string{"null", `""`} {
				fixture := newRuntimeNetworkIdentityFixture(t, reader)
				original := fixture.client.callContext
				faults := 0
				fixture.client.callContext = func(ctx context.Context, target any, called string, args ...any) error {
					if runtimeNetworkIdentityMethod(called, args, method) {
						faults++
						return json.Unmarshal([]byte(raw), target)
					}
					return original(ctx, target, called, args...)
				}
				err := fixture.read(t.Context())
				var unavailable *crv4.ReceiptEvidenceUnavailableError
				if err == nil || !errors.As(err, &unavailable) || !retryableProductionSteeringRead(err) || faults != 1 || fixture.succeeded != 0 {
					t.Fatalf("%s %s %s invented identity evidence: faults=%d error=%v", reader, method, raw, faults, err)
				}
			}
		}
	}
}

// A present zero/short/non-hex hash or non-string wire value stays hard. This
// differs from a validly decoded absent reply, despite sharing an RPC method.
func TestRuntimeNetworkIdentityMalformedReplyRemainsHard(t *testing.T) {
	for _, reader := range []string{"mainnet observation", "production artifact", "runtime continuity", "recycle census", "upload observer"} {
		for _, fault := range []struct {
			method string
			raw    string
		}{
			{method: "system_chain", raw: "964"},
			{method: "chain_getBlockHash", raw: "964"},
			{method: "chain_getBlockHash", raw: `"0x01"`},
			{method: "chain_getBlockHash", raw: `"` + types.Hash{}.Hex() + `"`},
			{method: "chain_getBlockHash", raw: `"0x` + strings.Repeat("zz", 32) + `"`},
			{method: "eth_chainId", raw: "964"},
			{method: "eth_chainId", raw: `"not-a-chain-id"`},
		} {
			fixture := newRuntimeNetworkIdentityFixture(t, reader)
			original := fixture.client.callContext
			faults := 0
			fixture.client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
				if runtimeNetworkIdentityMethod(method, args, fault.method) {
					faults++
					return json.Unmarshal([]byte(fault.raw), target)
				}
				return original(ctx, target, method, args...)
			}
			err := fixture.read(t.Context())
			var unavailable *crv4.ReceiptEvidenceUnavailableError
			if err == nil || errors.As(err, &unavailable) || retryableProductionSteeringRead(err) || faults != 1 || fixture.succeeded != 0 {
				t.Fatalf("%s %s malformed %s acquired availability: faults=%d error=%v", reader, fault.method, fault.raw, faults, err)
			}
		}
	}
}

// Cancellation is triggered after a complete decoded reply and joined at the
// actual child context. No sleep or scheduler timing chooses the ordering.
func TestRuntimeNetworkIdentityCompletedReplyPreservesLateContext(t *testing.T) {
	for _, reader := range []string{"mainnet observation", "production artifact", "runtime continuity", "recycle census", "upload observer"} {
		for _, method := range []string{"system_chain", "chain_getBlockHash", "eth_chainId"} {
			for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
				for _, wrong := range []bool{false, true} {
					fixture := newRuntimeNetworkIdentityFixture(t, reader)
					parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
					late := &runtimeFinalityLateContext{Context: parent, done: make(chan struct{}), cause: cause}
					original := fixture.client.callContext
					faulted, later := false, 0
					fixture.client.callContext = func(ctx context.Context, target any, called string, args ...any) error {
						if faulted {
							later++
							return ctx.Err()
						}
						if !runtimeNetworkIdentityMethod(called, args, method) {
							return original(ctx, target, called, args...)
						}
						var err error
						if wrong {
							value := "Different Synthetic Chain"
							if method == "chain_getBlockHash" {
								value = types.Hash{0xe7}.Hex()
							} else if method == "eth_chainId" {
								value = "0x1"
							}
							err = setReleaseHistoricalTestResult(target, value)
						} else {
							err = original(ctx, target, called, args...)
						}
						if err != nil {
							return err
						}
						faulted = true
						close(late.done)
						<-ctx.Done()
						return nil
					}
					err := fixture.read(late)
					if !faulted {
						close(late.done)
					}
					cancel()
					if !faulted || later != 0 || !errors.Is(err, cause) || fixture.succeeded != 0 {
						t.Fatalf("%s %s wrong=%t lost late %v: later=%d error=%v", reader, method, wrong, cause, later, err)
					}
					conflict := strings.Contains(err.Error(), "runtime network identity")
					if conflict != wrong || wrong && retryableProductionSteeringRead(err) {
						t.Fatalf("%s %s wrong=%t changed completed identity at %v: %v", reader, method, wrong, cause, err)
					}
				}
			}
		}
	}
}

// A physical timeout does not erase a simultaneous independent local failure.
// The actual public result remains empty and the original error stays reachable.
func TestRuntimeNetworkIdentityPreservesMixedOriginalReadCause(t *testing.T) {
	for _, reader := range []string{"mainnet observation", "production artifact", "runtime continuity", "recycle census", "upload observer"} {
		fixture := newRuntimeNetworkIdentityFixture(t, reader)
		hard := &os.LinkError{Op: "synthetic custody", Old: "original", New: "replacement", Err: context.DeadlineExceeded}
		cause := errors.Join(context.DeadlineExceeded, hard)
		calls := 0
		fixture.client.callContext = func(context.Context, any, string, ...any) error {
			calls++
			return cause
		}
		err := fixture.read(t.Context())
		if err == nil || !errors.Is(err, hard) || !errors.Is(err, context.DeadlineExceeded) || retryableProductionSteeringRead(err) || calls != 1 || fixture.succeeded != 0 {
			t.Fatalf("%s replaced a joined original cause: calls=%d error=%v", reader, calls, err)
		}
	}
}

// The existing production owner retries an absent genesis once and resumes
// the same public reader. All RPCs borrow the original shorter caller deadline.
func TestRuntimeNetworkIdentityProductionOwnerRecoversAbsentGenesis(t *testing.T) {
	ownerConfig := newProductionRuntimeTestFixture(t, false).cfg
	for _, reader := range []string{"mainnet observation", "production artifact", "runtime continuity", "recycle census", "upload observer"} {
		fixture := newRuntimeNetworkIdentityFixture(t, reader)
		ctx, recorder, cancel := newRuntimeReadBudgetRecorder(t, 45*time.Second)
		original := fixture.client.callContext
		missing, faults := true, 0
		fixture.client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			recorder.observe(ctx)
			if missing && runtimeNetworkIdentityMethod(method, args, "chain_getBlockHash") {
				faults++
				return json.Unmarshal([]byte("null"), target)
			}
			return original(ctx, target, method, args...)
		}
		owner := &ReleaseSteerer{cfg: ownerConfig}
		attempts, waits := 0, 0
		owner.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
			waits++
			if waits != 1 {
				return errors.New("synthetic unexpected repeated identity wait")
			}
			missing = false
			return ctx.Err()
		}
		err := owner.productionRead(ctx, productionReadPreparation, nil, func(ctx context.Context) error {
			attempts++
			return fixture.read(ctx)
		})
		cancel()
		recorder.completed(err)
		if faults != 1 || waits != 1 || attempts != 2 || fixture.succeeded != 1 {
			t.Fatalf("%s did not recover the original identity read: faults=%d waits=%d attempts=%d success=%d error=%v", reader, faults, waits, attempts, fixture.succeeded, err)
		}
	}
}

// Correct complete replies still reach all artifact, finality and purpose
// checks under the default300-second owner or the caller's original45 seconds.
func TestRuntimeNetworkIdentityApprovedReadsRetainOriginalBudget(t *testing.T) {
	for _, reader := range []string{"mainnet observation", "production artifact", "runtime continuity", "recycle census", "upload observer"} {
		for _, parentLimit := range []time.Duration{0, 45 * time.Second} {
			fixture := newRuntimeNetworkIdentityFixture(t, reader)
			ctx, recorder, cancel := newRuntimeReadBudgetRecorder(t, parentLimit)
			original := fixture.client.callContext
			identityCalls := map[string]int{}
			fixture.client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
				recorder.observe(ctx)
				for _, selected := range []string{"system_chain", "chain_getBlockHash", "eth_chainId"} {
					if runtimeNetworkIdentityMethod(method, args, selected) {
						identityCalls[selected]++
					}
				}
				return original(ctx, target, method, args...)
			}
			err := fixture.read(ctx)
			cancel()
			recorder.completed(err)
			if fixture.succeeded != 1 || identityCalls["system_chain"] != 1 || identityCalls["chain_getBlockHash"] != 1 || identityCalls["eth_chainId"] != 1 {
				t.Fatalf("%s changed the complete original identity read: calls=%v success=%d error=%v", reader, identityCalls, fixture.succeeded, err)
			}
		}
	}
}
