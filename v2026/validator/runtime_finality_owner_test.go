// Complete public reads retain finality through the actual production owner,
// without replacing its deadline, selected block, approved history or scope.
package validator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

// The second public call must not reset150 to149 merely because149 still
// covers historical target100. The real parent deadline then returns pending.
func TestRuntimeFinalityOwnerRetainsSustainedLowerHead(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, true)
	installRuntimeFinalityFault(fixture.rpc, "lower", 2)
	parent, recorder, cancel := newRuntimeReadBudgetRecorder(t, 45*time.Second)
	defer cancel()
	late := &runtimeFinalityLateContext{Context: parent, done: make(chan struct{}), cause: context.DeadlineExceeded}
	expired := false
	defer func() {
		if !expired {
			close(late.done)
		}
	}()
	client := fixture.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient)
	call := client.callContext
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		recorder.observe(ctx)
		return call(ctx, target, method, args...)
	}
	owner := &ReleaseSteerer{cfg: fixture.cfg}
	attempts, waits := 0, 0
	owner.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits > 2 {
			return errors.New("unexpected extra finality retry")
		}
		if waits == 2 {
			expired = true
			close(late.done)
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	err := owner.productionRead(late, productionReadReceipt, nil, func(ctx context.Context) error {
		attempts++
		artifact, number, err := authenticateOwnerRecycleProductionArtifactAtContext(ctx, fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(100), true)
		if artifact.BlockHash != (types.Hash{}) || number != 0 || err == nil {
			t.Fatalf("sustained lower head published an original runtime view on attempt%d", attempts)
		}
		return err
	})
	var pending *productionSteeringReadWait
	var unavailable *crv4.ReceiptEvidenceUnavailableError
	if !errors.As(err, &pending) || !errors.As(err, &unavailable) || unavailable.BlockHash != mainnetRuntimeTestBlock(150) ||
		!errors.Is(err, context.DeadlineExceeded) || attempts != 2 || waits != 2 || recorder.calls == 0 {
		t.Fatalf("sustained lower head lost original owner: attempts=%d waits=%d error=%v", attempts, waits, err)
	}
	// A separate owner can independently observe149. Retention does not become
	// a process cache that imposes the ended operation's higher witness.
	client.callContext = call
	artifact, number, err := authenticateOwnerRecycleProductionArtifactAtContext(t.Context(), fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(100), true)
	if err != nil || number != 100 || artifact.BlockHash != mainnetRuntimeTestBlock(100) {
		t.Fatalf("ended owner leaked finality state into an independent read: %v", err)
	}
}

// The real preparation/pending entrypoint selects a finalized hash before its
// artifact reader. That implicit selection must survive the parent retry too.
func TestRuntimeFinalityOwnerRetainsImplicitProductionSelection(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	wire := installRuntimeFinalityFault(fixture.rpc, "lower", 3)
	ctx, recorder, cancel := newRuntimeReadBudgetRecorder(t, 45*time.Second)
	defer cancel()
	client := fixture.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient)
	call := client.callContext
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		recorder.observe(ctx)
		if method == "state_getRuntimeVersion" || method == "state_getStorageHash" || method == "state_getMetadata" {
			if args[len(args)-1] != mainnetRuntimeTestBlock(150).Hex() {
				t.Fatalf("production retry replaced its original implicit block: %v", args)
			}
		}
		return call(ctx, target, method, args...)
	}
	owner := &ReleaseSteerer{cfg: fixture.cfg}
	attempts, waits := 0, 0
	owner.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits > 2 {
			return errors.New("unexpected extra finality retry")
		}
		if waits == 2 {
			wire.fault = "advanced"
		}
		return ctx.Err()
	}
	var selected types.Hash
	err := owner.productionRead(ctx, productionReadPreparation, nil, func(ctx context.Context) error {
		attempts++
		var err error
		selected, err = authenticatePinnedNativeRuntimeContext(ctx, fixture.rpc.native, fixture.cfg)
		if attempts <= 2 && (err == nil || selected != (types.Hash{})) {
			t.Fatalf("production lower head replaced the original selection on attempt%d: %s %v", attempts, selected.Hex(), err)
		}
		return err
	})
	recorder.completed(err)
	if attempts != 3 || waits != 2 || selected != mainnetRuntimeTestBlock(150) {
		t.Fatalf("implicit production selection changed across owner: attempts=%d waits=%d selected=%s error=%v", attempts, waits, selected.Hex(), err)
	}
}

func TestImplicitProductionSelectionSeparatesMissingAndMalformedHeads(t *testing.T) {
	for _, response := range []any{nil, "", "0x01", types.Hash{}.Hex()} {
		fixture := newProductionRuntimeTestFixture(t, false)
		client := fixture.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient)
		call := client.callContext
		client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			if method == "chain_getFinalizedHead" {
				return setReleaseHistoricalTestResult(target, response)
			}
			return call(ctx, target, method, args...)
		}
		selected, err := authenticatePinnedNativeRuntimeContext(t.Context(), fixture.rpc.native, fixture.cfg)
		wantPending := response == nil || response == ""
		var unavailable *crv4.ReceiptEvidenceUnavailableError
		if selected != (types.Hash{}) || err == nil || errors.As(err, &unavailable) != wantPending || retryableProductionSteeringRead(err) != wantPending {
			t.Fatalf("implicit production head %v changed evidence cause: %s %v", response, selected.Hex(), err)
		}
	}
}

// The public read-only zero selector shares the first candidate under a parent
// owner, while all independent network/artifact checks repeat after a timeout.
func TestRuntimeFinalityOwnerRetainsPublicZeroSelection(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	wire := installRuntimeFinalityFault(fixture, "head timeout", 2)
	production := newProductionRuntimeTestFixture(t, false)
	owner := &ReleaseSteerer{cfg: production.cfg}
	attempts, waits := 0, 0
	owner.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits > 1 {
			return errors.New("unexpected extra finality retry")
		}
		wire.fault = "advanced"
		return ctx.Err()
	}
	var observed *MainnetRuntimeObservation
	err = owner.productionRead(t.Context(), productionReadPreparation, nil, func(ctx context.Context) error {
		attempts++
		var err error
		observed, err = ObserveMainnetRuntimeAtContext(ctx, fixture.native, cfg, types.Hash{})
		return err
	})
	if err != nil || observed == nil || observed.BlockHash != mainnetRuntimeTestBlock(150) || attempts != 2 || waits != 1 {
		t.Fatalf("public zero selector drifted across owner: attempts=%d waits=%d observed=%+v error=%v", attempts, waits, observed, err)
	}
}

// A later admitted151 must survive just as the first150 does. The next call
// cannot complete at150 merely because that was the original opening height.
func TestRuntimeFinalityOwnerRetainsHighestWitness(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, true)
	wire := installRuntimeFinalityFault(fixture.rpc, "advanced", 2)
	owner := &ReleaseSteerer{cfg: fixture.cfg}
	attempts, waits := 0, 0
	owner.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits > 2 {
			return errors.New("unexpected extra finality retry")
		}
		wire.fault = "unchanged"
		fixture.rpc.head = 150
		if waits >= 2 {
			fixture.rpc.head = 152
		}
		return ctx.Err()
	}
	err := owner.productionRead(t.Context(), productionReadReceipt, nil, func(ctx context.Context) error {
		attempts++
		artifact, number, err := authenticateOwnerRecycleProductionArtifactAtContext(ctx, fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(100), true)
		if attempts == 1 {
			if err != nil || number != 100 || artifact.BlockHash != mainnetRuntimeTestBlock(100) {
				t.Fatal("first completed witness did not authenticate", err)
			}
			return context.DeadlineExceeded
		}
		if attempts == 2 && (err == nil || !retryableProductionSteeringRead(err)) {
			t.Fatalf("second call forgot the higher admitted witness: %v", err)
		}
		return err
	})
	if err != nil || attempts != 3 || waits != 2 {
		t.Fatalf("highest owner witness changed: attempts=%d waits=%d error=%v", attempts, waits, err)
	}
}

// The RPC is prepared to match each different finalized hash at the same
// height on separate calls. Those completed witnesses already contradict.
func TestRuntimeFinalityOwnerRejectsSameHeightForkAcrossPublicCalls(t *testing.T) {
	for _, test := range []struct {
		name string
		late error
	}{{name: "completed"}, {name: "late cancellation", late: context.Canceled}, {name: "late deadline", late: context.DeadlineExceeded}} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMainnetRuntimeTestFixture(t)
			cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			parent, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			cause := test.late
			if cause == nil {
				cause = context.Canceled
			}
			late := &runtimeFinalityLateContext{Context: parent, done: make(chan struct{}), cause: cause}
			expired := false
			defer func() {
				if !expired {
					close(late.done)
				}
			}()
			ctx := withRuntimeFinalityOwner(late)
			if _, err := ObserveMainnetRuntimeAtContext(ctx, fixture.native, cfg, mainnetRuntimeTestBlock(100)); err != nil {
				t.Fatal(err)
			}
			header, replacement := releaseReceiptTestHeader(t, types.Hash{0xe1}, 150)
			client := fixture.native.API.Client.(*validatorRuntimeIdentityTestClient)
			call := client.callContext
			canonicalReads := 0
			client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				switch method {
				case "chain_getFinalizedHead":
					return setReleaseHistoricalTestResult(target, replacement.Hex())
				case "chain_getHeader":
					if args[0] == replacement.Hex() {
						return setReleaseHistoricalTestResult(target, releaseReceiptTestHeaderWire(header))
					}
				case "chain_getBlockHash":
					if args[0] == uint64(150) {
						canonicalReads++
						// Each current canonical lookup gets B; each later
						// retained lookup gets A. Without tuple comparison,
						// both the opening and closing could appear valid.
						value := mainnetRuntimeTestBlock(150)
						if canonicalReads%2 == 1 {
							value = replacement
						}
						if err := setReleaseHistoricalTestResult(target, value.Hex()); err != nil {
							return err
						}
						if canonicalReads == 1 && test.late != nil {
							expired = true
							close(late.done)
							<-ctx.Done()
						}
						return nil
					}
				}
				return call(ctx, target, method, args...)
			}
			got, err := ObserveMainnetRuntimeAtContext(ctx, fixture.native, cfg, mainnetRuntimeTestBlock(100))
			if got != nil || err == nil || retryableProductionSteeringRead(err) || canonicalReads != 1 || !strings.Contains(err.Error(), "same height") ||
				(test.late != nil && !errors.Is(err, test.late)) {
				t.Fatalf("different completed finality at the same height acquired authority: reads=%d result=%+v error=%v", canonicalReads, got, err)
			}
		})
	}
}
