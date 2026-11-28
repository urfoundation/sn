// Genuine signed authority and storage readers retain physical I/O failure
// without manufacturing a successful-read policy or identity contradiction.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Every selected read occurs after the retained signature and exact route
// checks. Recovery rereads the complete census using the same approval bytes.
func TestOwnerRecycleAdmissionReadPreservesTransportCause(t *testing.T) {
	for _, selected := range []string{"runtime", "mode", "owner", "mechanisms", "uid-0"} {
		fixture := newRecycleAdmissionFixture(t, nil)
		fixture.retain(t)
		steerer := &ReleaseSteerer{cfg: fixture.cfg, native: fixture.chain}
		baseline, err := steerer.ObserveOwnerRecycleAdmission(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		injected := false
		fixture.before = func(_ context.Context, method string, args []any) error {
			if selected == "runtime" && method == "state_getRuntimeVersion" || method == "state_getStorage" && len(args) == 2 && args[0] == fixture.keys[selected] {
				injected = true
				return context.DeadlineExceeded
			}
			return nil
		}
		observed, err := steerer.ObserveOwnerRecycleAdmission(t.Context())
		if !injected || observed != nil || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) {
			t.Fatalf("%s census read timeout became a runtime/owner/mode contradiction: %+v %v", selected, observed, err)
		}
		fixture.before = nil
		observed, err = steerer.ObserveOwnerRecycleAdmission(t.Context())
		if err != nil || !reflect.DeepEqual(observed, baseline) {
			t.Fatalf("%s recovered reader did not reproduce its independently approved census: %v", selected, err)
		}
	}
}

// The actual measured production stage precedes the test. A successful
// observation's physical transcript identifies the explicit late activation
// header after the validator checks; no admission callback supplies a result.
func TestOwnerRecycleProductionReadPreservesTransportCause(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	stage, _ := fixture.stage(t)
	admission := fixture.operator.measurement.admission
	client := admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	original := client.callContext
	activation := types.Hash(stage.proof.Eligibility.ActivationHash).Hex()
	headerReads := 0
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if method == "chain_getHeader" && len(args) == 1 && args[0] == activation {
			headerReads++
		}
		return original(ctx, target, method, args...)
	}
	baseline, err := observeOwnerRecycleProductionEligibility(t.Context(), fixture.cfg, admission.chain, stage.authority)
	if err != nil || baseline == nil || headerReads == 0 {
		t.Fatalf("production baseline did not reach its activation header: %v", err)
	}
	activationHeaderRead := headerReads
	for _, selected := range []string{"header", "PendingServerEmission", "SubnetEpochIndex"} {
		injected, headers := false, 0
		client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
			if method == "chain_getHeader" && len(args) == 1 && args[0] == activation {
				headers++
				if selected == "header" && headers == activationHeaderRead {
					injected = true
					return context.DeadlineExceeded
				}
			}
			key := fixture.storageNameKVs[selected]
			if selected == "SubnetEpochIndex" {
				key = admission.keys["epoch"]
			}
			if selected != "header" && method == "state_getStorage" && len(args) == 2 && args[0] == key && args[1] == activation {
				injected = true
				return context.DeadlineExceeded
			}
			return original(ctx, target, method, args...)
		}
		observed, err := observeOwnerRecycleProductionEligibility(t.Context(), fixture.cfg, admission.chain, stage.authority)
		if !injected || observed != nil || !errors.Is(err, context.DeadlineExceeded) || !RetryableEvidenceTransportError(err) {
			t.Fatalf("%s production read timeout became an activation contradiction: %+v %v", selected, observed, err)
		}
		client.callContext = original
		observed, err = observeOwnerRecycleProductionEligibility(t.Context(), fixture.cfg, admission.chain, stage.authority)
		if err != nil || !reflect.DeepEqual(observed, baseline) {
			t.Fatalf("%s recovered eligibility differs under unchanged authority: %v", selected, err)
		}
	}
}

// A complete response still must pass its consumed width and semantic checks;
// neither the production stage nor a mixed transport error conceals a defect.
func TestOwnerRecycleReadContradictionsRemainHard(t *testing.T) {
	fixture := newRecycleAdmissionFixture(t, nil)
	fixture.retain(t)
	original := fixture.storage[fixture.keys["mode"]]
	for _, value := range []any{"0x00", "0x", "0x0100", nil} {
		fixture.storage[fixture.keys["mode"]] = value
		observed, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain)
		if observed != nil || err == nil || RetryableEvidenceTransportError(err) {
			t.Fatalf("returned mode %v acquired retry or approval: %+v %v", value, observed, err)
		}
	}
	fixture.storage[fixture.keys["mode"]] = original
	fixture.before = func(_ context.Context, method string, args []any) error {
		if method == "state_getStorage" && len(args) == 2 && args[0] == fixture.keys["owner"] {
			return errors.Join(context.DeadlineExceeded, &json.SyntaxError{Offset: 1})
		}
		return nil
	}
	observed, err := ObserveOwnerRecycleAdmission(t.Context(), fixture.cfg, fixture.chain)
	if observed != nil || err == nil || RetryableEvidenceTransportError(err) {
		t.Fatalf("mixed decoder/transport error acquired retry or approval: %+v %v", observed, err)
	}
}
