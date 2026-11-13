package validator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/urfoundation/sn/v2026/crv4"
)

func TestProductionRuntimeContinuityLowerHeadsKeepCanonicalContradictions(t *testing.T) {
	for _, closing := range []bool{false, true} {
		for _, changed := range []bool{false, true} {
			fixture := newProductionContinuityPolicyTestFixture(t)
			fixture.head = 149
			if closing {
				fixture.head = 151
			}
			fixture.fault = func(_ context.Context, target any, method string, args ...any) (bool, error) {
				if method == "state_getMetadata" {
					fixture.head = 149
				}
				if changed && fixture.head == 149 && method == "chain_getBlockHash" && args[0] == uint64(150) {
					return true, setReleaseHistoricalTestResult(target, types.Hash{99}.Hex())
				}
				return false, nil
			}
			got, err := InspectProductionRuntimeContinuityContext(t.Context(), fixture.owner.rpc.native, fixture.owner.cfg, fixture.hashes[150], fixture.policyRaw, fixture.certificateRaw)
			var unavailable *crv4.ReceiptEvidenceUnavailableError
			if got != nil || err == nil || errors.As(err, &unavailable) == changed || retryableProductionSteeringRead(err) == changed {
				t.Fatalf("closing=%t changed=%t lost continuity finality cause: %+v %v", closing, changed, got, err)
			}
		}
	}
}

func TestProductionRuntimeContinuityOwnerRetainsSustainedLowerHead(t *testing.T) {
	fixture := newProductionContinuityPolicyTestFixture(t)
	fixture.head = 151
	fault := true
	fixture.fault = func(_ context.Context, _ any, method string, _ ...any) (bool, error) {
		if fault && method == "state_getMetadata" {
			fixture.head = 150
		}
		return false, nil
	}
	owner := &ReleaseSteerer{cfg: fixture.owner.cfg}
	attempts, waits := 0, 0
	owner.productionReadHooks.wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits > 2 {
			return errors.New("unexpected extra continuity retry")
		}
		if waits == 2 {
			fault, fixture.head = false, 151
		}
		return ctx.Err()
	}
	var got *ProductionRuntimeContinuityInspection
	err := owner.productionRead(t.Context(), productionReadPreparation, nil, func(ctx context.Context) error {
		attempts++
		var err error
		got, err = InspectProductionRuntimeContinuityContext(ctx, fixture.owner.rpc.native, fixture.owner.cfg, fixture.hashes[150], fixture.policyRaw, fixture.certificateRaw)
		if attempts <= 2 && (got != nil || err == nil || !retryableProductionSteeringRead(err)) {
			t.Fatalf("continuity attempt%d discarded its original higher witness: %+v %v", attempts, got, err)
		}
		return err
	})
	if err != nil || got == nil || got.NativeHash != fixture.hashes[150] || attempts != 3 || waits != 2 {
		t.Fatalf("continuity did not recover its original selected block: %+v %v", got, err)
	}
}

func TestOwnerRecycleAdmissionClosesFinalityAndKeepsCanonicalContradictions(t *testing.T) {
	for _, closing := range []bool{false, true} {
		for _, changed := range []bool{false, true} {
			fixture := newRecycleAdmissionFixture(t, nil)
			fixture.retain(t)
			_, lower := releaseReceiptTestHeader(t, types.Hash(recycleTestId(1200)), 99)
			lag := func() {
				fixture.headHash, fixture.headNumber = lower, 99
				if changed {
					fixture.canonical = types.Hash{99}
				}
			}
			if !closing {
				lag()
			}
			storage := 0
			fixture.before = func(_ context.Context, method string, args []any) error {
				if method == "state_getStorage" {
					storage++
					if args[0] == fixture.keys["cap"] {
						lag()
					}
				}
				return nil
			}
			got, err := ObserveOwnerRecycleAdmissionAt(t.Context(), fixture.cfg, fixture.chain, [32]byte(fixture.finalized))
			var unavailable *crv4.ReceiptEvidenceUnavailableError
			if got != nil || err == nil || errors.As(err, &unavailable) == changed || retryableProductionSteeringRead(err) == changed || closing != (storage > 0) {
				t.Fatalf("closing=%t changed=%t lost census finality cause: storage=%d result=%+v error=%v", closing, changed, storage, got, err)
			}
			fixture.before, fixture.headHash, fixture.canonical = nil, types.Hash{}, types.Hash{}
			if got, err := ObserveOwnerRecycleAdmissionAt(t.Context(), fixture.cfg, fixture.chain, [32]byte(fixture.finalized)); err != nil || got == nil {
				t.Fatalf("original census failed after current finality recovered: %v", err)
			}
		}
	}
}
