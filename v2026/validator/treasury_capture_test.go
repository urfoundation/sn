//go:build linux || darwin

// Distinct activation, decision and successor blocks expose capture routing
// that a same-block fixture would conceal behind its metadata cache.
package validator

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// Capture keeps the original activation100 and decision101 even when the
// independently signed treasury successor activates at102 and is unavailable.
func TestTreasuryHistoricalCaptureRetainsOriginalActivationMetadata(t *testing.T) {
	fixture := newOwnerRecycleProductionTestFixture(t)
	measurement := fixture.operator.measurement
	artifact := measurement.provider.artifact
	fixture.head, fixture.epoch = 101, artifact.SubnetEpoch+1
	artifact.PreviousArtifactHash = ReleaseMeasurementContentHash(measurement.encoded)
	artifact.SubnetEpoch, artifact.NativeSnapshotBlock, artifact.NativeSnapshotHash = fixture.epoch, 101, fixture.block(101).Hex()
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
	originalCfg := *fixture.cfg
	originalMetadata, originalVersion, originalStorage := admission.metadata, admission.version, maps.Clone(admission.storage)
	originalEpoch := fixture.epoch
	originalMeasurement, originalProof := bytes.Clone(measurement.encoded), bytes.Clone(stage.encoded)
	bundle, err := BuildOwnerRecycleProductionAuthority(t.Context(), &originalCfg)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := WriteReleaseEvidenceV2File(t.Context(), filepath.Join(identityTestStateDir(t), "capture-original-owner.json"), bundle, maximumProductionAuthorityBundleBytes)
	if err != nil {
		t.Fatal(err)
	}
	configureTreasuryProductionTest(t, fixture)
	cfg := fixture.cfg
	cfg.ProductionAuthorityHistory = []ReleaseEvidenceV2File{reference}
	admission.approval.Proposal.PolicyId++
	admission.approval.Proposal.EffectiveEpoch++
	admission.approval.FirstNativeEpoch = originalEpoch + 1
	admission.approval.ValidFromNativeBlock, admission.approval.Production.ActivationNativeBlock = 102, 102
	admission.approval.Production.ActivationNativeHash = [32]byte(fixture.block(102))
	fixture.head, fixture.epoch = 102, originalEpoch+1
	admission.approval.ConfigHash, err = TreasuryConfigHash(cfg)
	if err != nil {
		t.Fatal(err)
	}
	admission.sign(t)
	if err := loadOwnerRecycleProductionConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionRuntimeHistory(cfg); err != nil {
		t.Fatal(err)
	}
	if err := loadReleaseProductionAuthorityHistory(cfg); err != nil {
		t.Fatal(err)
	}
	client := admission.chain.API.Client.(*recycleAdmissionRouteClient).validatorRuntimeIdentityTestClient
	previousCall := client.callContext
	client.callContext = func(ctx context.Context, target any, method string, args ...any) error {
		if len(args) != 0 {
			hash := args[len(args)-1]
			if (method == "state_getMetadata" || method == "state_getRuntimeVersion") && hash == fixture.block(102).Hex() {
				return errors.New("synthetic new treasury artifact is unavailable")
			}
			if hash == fixture.block(100).Hex() || hash == fixture.block(101).Hex() {
				var value any
				handled := true
				switch method {
				case "state_getMetadata":
					value = originalMetadata
				case "state_getRuntimeVersion":
					value = map[string]any{"specName": originalVersion.SpecName, "specVersion": originalVersion.SpecVersion,
						"transactionVersion": originalVersion.TransactionVersion, "stateVersion": originalVersion.StateVersion, "apis": []any{[]any{"0x8375104b299b74c5", 2}}}
				case "state_getStorage":
					value, handled = originalStorage[args[0].(string)]
					if args[0] == admission.keys["epoch"] && hash == fixture.block(101).Hex() {
						value, handled = codec.HexEncodeToString(binary.LittleEndian.AppendUint64(nil, originalEpoch)), true
					}
				default:
					handled = false
				}
				if handled {
					raw, err := json.Marshal(value)
					if err != nil {
						return err
					}
					return json.Unmarshal(raw, target)
				}
			}
		}
		var raw json.RawMessage
		if err := previousCall(ctx, &raw, method, args...); err != nil {
			return err
		}
		return json.Unmarshal(raw, target)
	}
	metadataKVs := map[string]bool{}
	if err := CaptureReleaseNativeSourceV2(t.Context(), admission.chain, cfg, intent, originalMeasurement, func(_ context.Context, read ReleaseEvidenceV2NativeRead) error {
		if read.Method == "state_getMetadata" {
			var hashes []string
			if err := json.Unmarshal(read.Parameters, &hashes); err != nil || len(hashes) != 1 {
				return errors.New("captured metadata lost its exact native block")
			}
			metadataKVs[hashes[0]] = true
		}
		return nil
	}); err != nil {
		t.Fatalf("treasury renewal stranded original native capture: %v", err)
	}
	if !metadataKVs[fixture.block(100).Hex()] || !metadataKVs[fixture.block(101).Hex()] || metadataKVs[fixture.block(102).Hex()] || !bytes.Equal(stage.encoded, originalProof) {
		t.Fatal("capture omitted original activation metadata or substituted treasury activation")
	}
}
