// Real signed runtime windows and complete native headers exercise closing
// finality through the public observation and actual production read owners.
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

// The fixture alters raw RPC responses only after the first admitted runtime
// read. Every successful result still comes from the real artifact verifier.
type runtimeFinalityFault struct {
	rpc      *mainnetRuntimeTestFixture
	fault    string
	atHead   int
	heads    int
	injected bool
	original func(context.Context, any, string, ...any) error
}

func installRuntimeFinalityFault(rpc *mainnetRuntimeTestFixture, fault string, atHead int) *runtimeFinalityFault {
	client := rpc.native.API.Client.(*validatorRuntimeIdentityTestClient)
	self := &runtimeFinalityFault{rpc: rpc, fault: fault, atHead: atHead, original: client.callContext}
	client.callContext = self.call
	return self
}

func (self *runtimeFinalityFault) call(ctx context.Context, target any, method string, args ...any) error {
	if method == "chain_getFinalizedHead" {
		self.heads++
	}
	if self.heads >= self.atHead {
		switch self.fault {
		case "advanced":
			self.rpc.head = 151
		case "lower", "lower with changed opening", "lower with changed block":
			self.rpc.head = 149
		case "orphan":
			self.rpc.head = 151
			self.rpc.canonicalHashKVs[151] = types.Hash{0x99}
		}
		if self.fault == "lower with changed opening" {
			self.rpc.canonicalHashKVs[150] = types.Hash{0x98}
		}
		if self.fault == "lower with changed block" {
			self.rpc.canonicalHashKVs[100] = types.Hash{0x97}
		}
		if method == "chain_getFinalizedHead" {
			switch self.fault {
			case "head timeout":
				self.injected = true
				return context.DeadlineExceeded
			case "mixed timeout":
				self.injected = true
				return errors.Join(context.DeadlineExceeded, errors.New("independent response integrity failure"))
			case "missing head":
				self.injected = true
				return setReleaseHistoricalTestResult(target, nil)
			case "short head":
				self.injected = true
				return setReleaseHistoricalTestResult(target, "0x01")
			}
		}
		if method == "chain_getHeader" && self.fault == "header timeout" {
			self.injected = true
			return context.DeadlineExceeded
		}
		if method == "chain_getBlockHash" && args[0] != uint64(0) {
			switch self.fault {
			case "canonical timeout":
				self.injected = true
				return context.DeadlineExceeded
			case "missing canonical":
				self.injected = true
				return setReleaseHistoricalTestResult(target, nil)
			case "zero canonical":
				self.injected = true
				return setReleaseHistoricalTestResult(target, (types.Hash{}).Hex())
			}
		}
	}
	return self.original(ctx, target, method, args...)
}

// An unchanged canonical chain does not establish current finality after a
// lagging closing head. Actual returned contradictions remain hard even then.
func TestMainnetRuntimeFinalityClosesPublicObservation(t *testing.T) {
	for _, historical := range []bool{false, true} {
		for _, fault := range []string{"unchanged", "advanced", "lower", "orphan", "lower with changed opening", "lower with changed block"} {
			fixture := newMainnetRuntimeTestFixture(t)
			cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			requested, number := types.Hash{}, uint64(150)
			if historical {
				requested, number = mainnetRuntimeTestBlock(100), 100
			}
			if !historical && fault == "lower with changed block" {
				continue
			}
			wire := installRuntimeFinalityFault(fixture, fault, 2)
			metadata, runtime := fixture.native.Meta, fixture.native.Runtime
			got, err := ObserveMainnetRuntimeAtContext(t.Context(), fixture.native, cfg, requested)
			wantSuccess := fault == "unchanged" || fault == "advanced"
			if wantSuccess {
				if err != nil || got == nil || got.BlockHash != mainnetRuntimeTestBlock(number) || got.BlockNumber != number {
					t.Fatalf("historical=%t %s changed the original runtime block: %+v %v", historical, fault, got, err)
				}
			} else {
				var unavailable *crv4.ReceiptEvidenceUnavailableError
				if got != nil || err == nil || errors.As(err, &unavailable) != (fault == "lower") || retryableProductionSteeringRead(err) != (fault == "lower") {
					t.Fatalf("historical=%t %s finality result lost its authority class: %+v %v", historical, fault, got, err)
				}
			}
			if !wantSuccess && wire.heads < 2 || fixture.callKVs["state_getStorageHash"] == 0 || fixture.native.Meta != metadata || fixture.native.Runtime != runtime {
				t.Fatalf("historical=%t %s bypassed real runtime closure or rebound its view", historical, fault)
			}
		}
	}
}

// Timeouts and genuinely missing wire values retain pending read semantics;
// a malformed returned hash or a joined integrity error never acquires them.
func TestMainnetRuntimeFinalityClosingReadCauses(t *testing.T) {
	for _, fault := range []string{"head timeout", "header timeout", "canonical timeout", "missing head", "missing canonical", "short head", "zero canonical", "mixed timeout"} {
		fixture := newMainnetRuntimeTestFixture(t)
		cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		wire := installRuntimeFinalityFault(fixture, fault, 2)
		got, err := ObserveMainnetRuntimeAtContext(t.Context(), fixture.native, cfg, mainnetRuntimeTestBlock(100))
		wantRetry := fault != "short head" && fault != "zero canonical" && fault != "mixed timeout"
		if !wire.injected || got != nil || err == nil || retryableProductionSteeringRead(err) != wantRetry {
			t.Fatalf("%s closing read changed its actual cause: %+v %v", fault, got, err)
		}
		if (fault == "head timeout" || fault == "header timeout" || fault == "canonical timeout") && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s became a finality contradiction: %v", fault, err)
		}
	}
}

// A transport replay at a historical target must retain the higher finalized
// witness it already consumed, even when the replacement still covers target100.
func TestMainnetRuntimeFinalityReconnectRetainsOriginalWitness(t *testing.T) {
	fixture := newMainnetRuntimeTestFixture(t)
	cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	original := fixture.native.API.Client.(*validatorRuntimeIdentityTestClient)
	client := &productionTransportTestClient{Client: original}
	client.generation.Store(1)
	fixture.native.API.Client = client
	ctx, recorder, cancel := newRuntimeReadBudgetRecorder(t, 45*time.Second)
	defer cancel()
	faulted := false
	original.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		recorder.observe(ctx)
		err := fixture.callContext(ctx, target, method, args...)
		if err == nil && method == "state_getStorageHash" && !faulted {
			faulted = true
			fixture.head = 149
			client.generation.Add(1)
		}
		return err
	}
	got, err := ObserveMainnetRuntimeAtContext(ctx, fixture.native, cfg, mainnetRuntimeTestBlock(100))
	var unavailable *crv4.ReceiptEvidenceUnavailableError
	if !faulted || got != nil || !errors.As(err, &unavailable) || unavailable.BlockHash != mainnetRuntimeTestBlock(150) ||
		!retryableProductionSteeringRead(err) || fixture.callKVs["system_chain"] != 2 || recorder.calls == 0 {
		t.Fatalf("reconnect discarded original finality or changed its deadline: %+v %v calls=%v", got, err, fixture.callKVs)
	}
}

// The actual production-purpose binding closes after its own dependent reads,
// while historical artifact admission keeps the original approved interval.
func TestProductionRuntimeFinalityPreservesOriginalView(t *testing.T) {
	for _, historical := range []bool{false, true} {
		for _, fault := range []string{"lower", "head timeout", "orphan"} {
			fixture := newProductionRuntimeTestFixture(t, historical)
			atHead, number := 3, uint64(150)
			if historical {
				atHead, number = 2, 100
			}
			wire := installRuntimeFinalityFault(fixture.rpc, fault, atHead)
			metadata, runtime := fixture.rpc.native.Meta, fixture.rpc.native.Runtime
			var err error
			if historical {
				err = authenticateHistoricalNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(number))
			} else {
				err = authenticatePinnedNativeRuntimeAtContext(t.Context(), fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(number))
			}
			if err == nil || retryableProductionSteeringRead(err) != (fault != "orphan") || wire.heads < atHead ||
				fixture.rpc.native.Meta != metadata || fixture.rpc.native.Runtime != runtime {
				t.Fatalf("historical=%t %s finality published a production view: %v heads=%d", historical, fault, err, wire.heads)
			}
		}
	}
}

// A lagging closing node retries through the existing owner. Its original
// deadline and exact historical block survive recovery to an advanced head.
func TestProductionRuntimeFinalityRetryKeepsOriginalBlockAndDeadline(t *testing.T) {
	for _, fault := range []string{"lower", "head timeout", "missing head"} {
		fixture := newProductionRuntimeTestFixture(t, true)
		wire := installRuntimeFinalityFault(fixture.rpc, fault, 2)
		ctx, recorder, cancel := newRuntimeReadBudgetRecorder(t, 45*time.Second)
		defer cancel()
		client := fixture.rpc.native.API.Client.(*validatorRuntimeIdentityTestClient)
		call := client.callContext
		client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			recorder.observe(ctx)
			if method == "state_getRuntimeVersion" || method == "state_getStorageHash" || method == "state_getMetadata" {
				if args[len(args)-1] != mainnetRuntimeTestBlock(100).Hex() {
					t.Fatalf("%s retry drifted to another runtime block: %v", fault, args)
				}
			}
			return call(ctx, target, method, args...)
		}
		owner := &ReleaseSteerer{cfg: fixture.cfg}
		waits, attempts := 0, 0
		owner.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
			waits++
			wire.fault = "advanced"
			return ctx.Err()
		}
		err := owner.productionRead(ctx, productionReadReceipt, nil, func(ctx context.Context) error {
			attempts++
			artifact, number, err := authenticateOwnerRecycleProductionArtifactAtContext(ctx, fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(100), true)
			if err == nil && (artifact.BlockHash != mainnetRuntimeTestBlock(100) || number != 100) {
				t.Fatalf("%s recovery changed original historical authority", fault)
			}
			return err
		})
		recorder.completed(err)
		if attempts != 2 || waits != 1 {
			t.Fatalf("%s did not use the bounded original owner: attempts=%d waits=%d error=%v", fault, attempts, waits, err)
		}
	}
}

// A changed opening hash is still a hard result when the same closing node
// reports a lower head. The normal owner must not turn it into a read wait.
func TestProductionRuntimeFinalityContradictionDoesNotRetry(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, true)
	installRuntimeFinalityFault(fixture.rpc, "lower with changed opening", 2)
	owner := &ReleaseSteerer{cfg: fixture.cfg}
	attempts, waits := 0, 0
	owner.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		return ctx.Err()
	}
	err := owner.productionRead(t.Context(), productionReadReceipt, nil, func(ctx context.Context) error {
		attempts++
		_, _, err := authenticateOwnerRecycleProductionArtifactAtContext(ctx, fixture.rpc.native, fixture.cfg, mainnetRuntimeTestBlock(100), true)
		return err
	})
	var pending *productionSteeringReadWait
	if err == nil || retryableProductionSteeringRead(err) || errors.As(err, &pending) || attempts != 1 || waits != 0 {
		t.Fatalf("canonical contradiction acquired retry authority: attempts=%d waits=%d error=%v", attempts, waits, err)
	}
}

// The physical RPC can finish returning bytes at the same boundary at which
// its owner expires. Err and Done agree without a wall-clock sleep.
type runtimeFinalityLateContext struct {
	context.Context
	done  chan struct{}
	cause error
}

func (self *runtimeFinalityLateContext) Done() <-chan struct{} { return self.done }

func (self *runtimeFinalityLateContext) Err() error {
	select {
	case <-self.done:
		return self.cause
	default:
		return nil
	}
}

// Complete wrong canonical bytes remain a hard contradiction when deadline or
// cancellation arrives before CallContext returns. Matching bytes still cannot
// publish success, and an RPC error without bytes retains the earlier controls.
func TestMainnetRuntimeFinalityCompletedHashRetainsLateCancellation(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, changed := range []bool{false, true} {
			fixture := newMainnetRuntimeTestFixture(t)
			cfg, err := LoadMainnetRuntimeObservationConfig(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			late := &runtimeFinalityLateContext{Context: context.Background(), done: make(chan struct{}), cause: cause}
			faulted := false
			defer func() {
				if !faulted {
					close(late.done)
				}
			}()
			client := fixture.native.API.Client.(*validatorRuntimeIdentityTestClient)
			call := client.callContext
			heads := 0
			client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
				if method == "chain_getFinalizedHead" {
					heads++
				}
				if heads >= 2 && method == "chain_getBlockHash" && !faulted {
					hash := mainnetRuntimeTestBlock(150)
					if changed {
						hash = types.Hash{0xf1}
					}
					if err := setReleaseHistoricalTestResult(target, hash.Hex()); err != nil {
						return err
					}
					faulted = true
					close(late.done)
					<-ctx.Done()
					return nil
				}
				return call(ctx, target, method, args...)
			}
			got, err := ObserveMainnetRuntimeAtContext(late, fixture.native, cfg, types.Hash{})
			if !faulted || got != nil || !errors.Is(err, cause) || strings.Contains(err.Error(), "not canonical") != changed ||
				retryableProductionSteeringRead(err) != (!changed && cause == context.DeadlineExceeded) {
				t.Fatalf("changed=%t cause=%v completed canonical evidence lost a cause: %+v %v", changed, cause, got, err)
			}
		}
	}
}
