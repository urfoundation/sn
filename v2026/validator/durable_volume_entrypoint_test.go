// Public lifecycle variants share physical admission before any retained
// setup adoption, process progress, network authentication, or runtime owner.
package validator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Canonical handoffs pass their byte decoders, then meet the same mainnet
// storage requirement as the ordinary and progress-enabled public lifecycle.
func TestProductionPublicLifecyclesRequirePhysicalCustody(t *testing.T) {
	fixture := newProductionRuntimeTestFixture(t, false)
	config, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	provisional, err := json.MarshalIndent(ProvisionalActivationSetupV2{}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	provisional = append(provisional, '\n')
	adoption, err := json.MarshalIndent(ReleaseHistoryAdoptionV2{Schema: ReleaseHistoryAdoptionV2Schema,
		DeploymentID: "synthetic-retained", ValidatorID: 1, ApprovedPlanHash: "0x" + strings.Repeat("12", 32), SourcePlanHash: "0x" + strings.Repeat("34", 32),
		ConfigSHA256: ReleaseMeasurementContentHash(config), IntentPrefixSHA256: ReleaseMeasurementContentHash(nil), FirstNativeEpoch: 1}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	adoption = append(adoption, '\n')
	if _, err := DecodeReleaseHistoryAdoptionV2(adoption, provisionalActivationSetupSHA256(adoption)); err != nil {
		t.Fatal("fixture did not reach public lifecycle admission", err)
	}
	progress := filepath.Join(filepath.Dir(fixture.path), "unadmitted-progress.json")
	before, err := os.ReadDir(fixture.cfg.StateDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	for _, run := range []func(context.Context) error{
		func(ctx context.Context) error { return RunRelease(ctx, fixture.path) },
		func(ctx context.Context) error { return RunReleaseWithProgress(ctx, fixture.path, progress) },
		func(ctx context.Context) error {
			return RunReleaseWithProvisionalActivationSetup(ctx, fixture.path, provisional, provisionalActivationSetupSHA256(provisional))
		},
		func(ctx context.Context) error {
			return RunReleaseWithHistoryAdoptionV2(ctx, fixture.path, adoption, provisionalActivationSetupSHA256(adoption))
		},
		func(ctx context.Context) error {
			return RewriteReleaseConfigEvidenceV2Operators(fixture.path, nil, ctx)
		},
	} {
		if err := run(t.Context()); err == nil || !strings.Contains(err.Error(), "durable-volume") {
			t.Fatalf("public lifecycle reached implicit storage: %v", err)
		}
	}
	bad := durablevolume.WithReference(t.Context(), durablevolume.Reference{Path: filepath.Join(filepath.Dir(fixture.path), "absent-volumes.json"), Sha256: ReleaseMeasurementContentHash(nil)})
	if err := RunReleaseWithProgress(bad, fixture.path, progress); err == nil {
		t.Fatal("public lifecycle admitted a declaration without physical custody")
	}
	if after, err := os.ReadFile(fixture.path); err != nil || !bytes.Equal(after, config) {
		t.Fatalf("refusal changed original approved config: %v", err)
	}
	if _, err := os.Stat(progress); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refusal published process progress: %v", err)
	}
	after, err := os.ReadDir(fixture.cfg.StateDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatal("unadmitted public lifecycle created state")
	}
}
