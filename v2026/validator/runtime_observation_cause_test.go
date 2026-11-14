// Canonical observations may be unavailable without invalidating a certificate
// or the retained selection. Only returned numbers establish window conflicts.
package validator

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProductionRuntimeContinuityUnavailableCanonicalReadRecovers(t *testing.T) {
	for _, phase := range []string{"selected", "latest"} {
		f := newProductionContinuityPolicyTestFixture(t)
		f.head = 151
		original := bytes.Clone(f.owner.cfg.ownerRecycleProduction.encoded)
		policy, certificate := bytes.Clone(f.policyRaw), bytes.Clone(f.certificateRaw)
		originalMetadata, originalRuntime := f.owner.rpc.native.Meta, f.owner.rpc.native.Runtime
		faulted, headReads := false, 0
		f.fault = func(_ context.Context, _ any, method string, args ...any) (bool, error) {
			if method == "chain_getHeader" && len(args) == 1 {
				if args[0] == f.hashes[151].Hex() {
					headReads++
				}
				selected := phase == "selected" && args[0] == f.hashes[150].Hex() || phase == "latest" && args[0] == f.hashes[151].Hex() && headReads == 2
				if selected && !faulted {
					faulted = true
					return true, context.DeadlineExceeded
				}
			}
			return false, nil
		}
		owner := &ReleaseSteerer{cfg: f.owner.cfg}
		waits, reads := 0, 0
		owner.productionReadHooks.wait = func(context.Context, time.Duration) error {
			waits++
			if waits != 1 {
				return errors.New("unexpected repeated canonical failure")
			}
			return nil
		}
		var result *ProductionRuntimeContinuityInspection
		err := owner.productionRead(t.Context(), productionReadPreparation, nil, func(ctx context.Context) error {
			reads++
			var err error
			result, err = InspectProductionRuntimeContinuityContext(ctx, f.owner.rpc.native, f.owner.cfg, f.hashes[150], f.policyRaw, f.certificateRaw)
			if reads == 1 && (result != nil || !errors.Is(err, context.DeadlineExceeded) || !retryableProductionSteeringRead(err)) {
				t.Fatalf("%s unavailable canonical read fabricated a certificate/finality conflict: %v", phase, err)
			}
			return err
		})
		if err != nil || result == nil || !faulted || waits != 1 || reads != 2 || result.NativeHash != f.hashes[150] ||
			!bytes.Equal(original, f.owner.cfg.ownerRecycleProduction.encoded) || !bytes.Equal(policy, f.policyRaw) || !bytes.Equal(certificate, f.certificateRaw) ||
			f.owner.rpc.native.Meta != originalMetadata || f.owner.rpc.native.Runtime != originalRuntime {
			t.Fatalf("%s canonical read did not recover with original custody: reads=%d waits=%d result=%+v err=%v", phase, reads, waits, result, err)
		}
	}
}

func TestProductionRuntimeContinuitySeparatesWindowConflictFromLaggingFinality(t *testing.T) {
	for _, phase := range []string{"window", "regression"} {
		f := newProductionContinuityPolicyTestFixture(t)
		f.head = 151
		selected, expected := f.hashes[149], "outside its finalized certificate window"
		if phase == "regression" {
			selected, expected = f.hashes[150], "finalized head through"
			f.fault = func(_ context.Context, _ any, method string, _ ...any) (bool, error) {
				if method == "state_getMetadata" {
					f.head = 149
				}
				return false, nil
			}
		}
		result, err := InspectProductionRuntimeContinuityContext(t.Context(), f.owner.rpc.native, f.owner.cfg, selected, f.policyRaw, f.certificateRaw)
		if err == nil || result != nil || retryableProductionSteeringRead(err) != (phase == "regression") || !strings.Contains(err.Error(), expected) {
			t.Fatalf("%s lost its window/finality cause: result=%+v err=%v", phase, result, err)
		}
	}
}
