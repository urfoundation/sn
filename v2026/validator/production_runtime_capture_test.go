//go:build linux || darwin

// Production capture retains the exact successor native evidence used by an
// independently replayed decision, including the earlier drained activation.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Both the decision and signed atomic source batch are produced by the real
// provider proof and cryptographic preparation paths before capture starts.
func TestProductionRuntimeCaptureRetainsSuccessorAndActivationReads(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	measurement := fixture.operator.measurement
	artifact := measurement.provider.artifact
	fixture.head, fixture.epoch = 101, artifact.SubnetEpoch+1
	artifact.SubnetEpoch, artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash = fixture.epoch, 101, fixture.block(101).Hex()
	artifact.PreviousArtifactHash = ReleaseMeasurementContentHash(measurement.encoded)
	for index := range artifact.Inputs {
		artifact.Inputs[index].CutNativeBlock, artifact.Inputs[index].CutNativeBlockHash = 101, fixture.block(101).Hex()
	}
	var err error
	measurement.encoded, _, err = SealReleaseMeasurementArtifactV2(t.Context(), artifact, measurement.provider.options(t))
	if err != nil {
		t.Fatal(err)
	}
	stage, provider := fixture.stage(t)
	intent := fixture.intent(t, stage, provider)
	admission := measurement.admission
	client := admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	original := client.callContext
	client.callContext = func(ctx context.Context, result any, method string, args ...any) error {
		var raw json.RawMessage
		if err := original(ctx, &raw, method, args...); err != nil {
			return err
		}
		return json.Unmarshal(raw, result)
	}
	// A cold separate view proves metadata is retained independently of the
	// preparation cache, while the original signing view remains unchanged.
	native := &crv4.Chain{API: admission.chain.API, GenesisHash: admission.chain.GenesisHash}
	var reads []ReleaseEvidenceV2NativeRead
	if err := CaptureReleaseNativeSourceV2(t.Context(), native, fixture.cfg, intent, measurement.encoded, func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
		reads = append(reads, read)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	activationMetadata := false
	for _, read := range reads {
		var args []string
		if read.Method != "state_getStorage" && read.Method != "state_getMetadata" {
			continue
		}
		if err := json.Unmarshal(read.Parameters, &args); err != nil {
			t.Fatal(err)
		}
		if read.Method == "state_getMetadata" && len(args) == 1 && args[0] == fixture.block(100).Hex() {
			activationMetadata = true
		}
		if read.Method == "state_getStorage" && len(args) == 2 {
			for _, name := range []string{"LastUpdate", "PendingServerEmission"} {
				if args[0] == fixture.storageNameKVs[name] {
					want := fixture.block(101).Hex()
					if name == "PendingServerEmission" {
						want = fixture.block(100).Hex()
					}
					if args[1] != want {
						t.Fatalf("captured %s substituted block %s for %s", name, args[1], want)
					}
					seen[name] = true
				}
			}
		}
	}
	if !activationMetadata || !seen["LastUpdate"] || !seen["PendingServerEmission"] {
		t.Fatal("capture omitted the original activation or actual production eligibility")
	}
	if native.Meta != nil || native.Runtime != nil {
		t.Fatal("capture changed the caller's native signing view")
	}
	canary := errors.New("synthetic production capture sink refusal")
	if err := CaptureReleaseNativeSourceV2(t.Context(), native, fixture.cfg, intent, measurement.encoded, func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
		if read.Method == "state_getStorage" {
			var args []string
			if err := json.Unmarshal(read.Parameters, &args); err != nil {
				return err
			}
			if len(args) == 2 && args[0] == fixture.storageNameKVs["LastUpdate"] {
				return canary
			}
		}
		return nil
	}); !errors.Is(err, canary) {
		t.Fatalf("failed successor evidence retention was hidden: %v", err)
	}
}
