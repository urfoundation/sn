// Exact runtime expiry reaches the actual read owner without replacing retained
// authority, extending its budget, or replaying a signed operation.
package validator

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	gsrpcclient "github.com/centrifuge/go-substrate-rpc-client/v4/client"
)

// The real transport's atomic reconnect contract is independently exercised by
// gethrpc's socket tests. This barrier forces the public caller's exact read cut.
type productionTransportTestClient struct {
	gsrpcclient.Client
	generation atomic.Uint64
}

func (self *productionTransportTestClient) TransportGeneration() uint64 {
	return self.generation.Load()
}

// A complete candidate read is invalidated before admission. The production
// read owner observes the same selected block again; a sibling keeps its own
// independent authority throughout the reconnect.
func TestProductionRuntimeTransportExpiryReobservesWithinOriginalBudget(t *testing.T) {
	f := newProductionContinuityPolicyTestFixture(t)
	native := f.owner.rpc.native
	client := &productionTransportTestClient{Client: native.API.Client}
	client.generation.Store(1)
	native.API.Client = client
	original := bytes.Clone(f.owner.cfg.ownerRecycleProduction.encoded)
	originalMetadata, originalRuntime := native.Meta, native.Runtime
	faulted, versionReads := false, 0
	f.fault = func(_ context.Context, _ any, method string, _ ...any) (bool, error) {
		if method == "state_getRuntimeVersion" {
			versionReads++
		}
		if method == "state_getRuntimeVersion" && versionReads == 2 && !faulted {
			faulted = true
			client.generation.Add(1)
		}
		return false, nil
	}
	peer := newProductionContinuityPolicyTestFixture(t)
	owner := &ReleaseSteerer{cfg: f.owner.cfg}
	var durations []time.Duration
	owner.productionReadHooks.withTimeout = func(ctx context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
		durations = append(durations, duration)
		return context.WithTimeout(ctx, duration)
	}
	waits, attempts := 0, 0
	owner.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		return errors.New("whole runtime observation escaped its original read attempt")
	}
	var report *ProductionRuntimeContinuityInspection
	err := owner.productionRead(t.Context(), productionReadPreparation, nil, func(ctx context.Context) error {
		attempts++
		var err error
		report, err = InspectProductionRuntimeContinuityContext(ctx, native, f.owner.cfg, f.hashes[150], f.policyRaw, f.certificateRaw)
		return err
	})
	if err != nil || report == nil || !faulted || attempts != 1 || waits != 0 || versionReads != 4 || f.owner.rpc.callKVs["system_chain"] != 2 || report.NativeHash != f.hashes[150] ||
		!slices.Equal(durations, []time.Duration{300 * time.Second, 60 * time.Second}) ||
		!bytes.Equal(original, f.owner.cfg.ownerRecycleProduction.encoded) || native.Meta != originalMetadata || native.Runtime != originalRuntime || report.SigningAuthority {
		t.Fatalf("same-block runtime read did not recover within original owner: attempts=%d waits=%d budgets=%v report=%+v err=%v", attempts, waits, durations, report, err)
	}
	result, err := InspectProductionRuntimeContinuityContext(t.Context(), peer.owner.rpc.native, peer.owner.cfg, peer.hashes[150], peer.policyRaw, peer.certificateRaw)
	if err != nil || result == nil || result.NativeHash != peer.hashes[150] {
		t.Fatalf("independent runtime peer lost its original authority: %v", err)
	}
}

// Returned incompatible runtime bytes still dominate a concurrent disconnect;
// typed expiry is not a blanket retry or a substitute for independent approval.
func TestProductionRuntimeTransportChangeDoesNotMaskObservedIdentity(t *testing.T) {
	f := newProductionContinuityPolicyTestFixture(t)
	native := f.owner.rpc.native
	client := &productionTransportTestClient{Client: native.API.Client}
	client.generation.Store(1)
	native.API.Client = client
	f.fault = func(_ context.Context, target any, method string, _ ...any) (bool, error) {
		if method == "state_getStorageHash" {
			client.generation.Add(1)
			return true, setReleaseHistoricalTestResult(target, mainnetRuntimeTestBlock(999).Hex())
		}
		return false, nil
	}
	owner := &ReleaseSteerer{cfg: f.owner.cfg}
	waits := 0
	owner.productionReadHooks.wait = func(context.Context, time.Duration) error { waits++; return errors.New("unexpected identity retry") }
	err := owner.productionRead(t.Context(), productionReadPreparation, nil, func(ctx context.Context) error {
		_, err := InspectProductionRuntimeContinuityContext(ctx, native, f.owner.cfg, f.hashes[150], f.policyRaw, f.certificateRaw)
		return err
	})
	if err == nil || retryableProductionSteeringRead(err) || waits != 0 {
		t.Fatalf("observed wrong runtime became retryable transport expiry: waits=%d err=%v", waits, err)
	}
}
