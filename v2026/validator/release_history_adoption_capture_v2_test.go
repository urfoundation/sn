//go:build linux || darwin

package validator

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseHistoryAdoptionV2CapturePreservesActualAbsence(t *testing.T) {
	t.Parallel()
	request, path := historyAdoptionRequestTest(t)
	configBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := CaptureReleaseHistoryAdoptionV2(t.Context(), path, configBytes, request.ApprovedPlanHash, request.SourcePlanHash, request.FirstNativeEpoch)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, encodeHistoryAdoptionRequestTest(t, request)) {
		t.Fatal("capture changed the exact requested config or empty source")
	}
	if err := CheckReleaseHistoryAdoptionV2Source(t.Context(), path, configBytes, raw, ReleaseMeasurementContentHash(raw)); err != nil {
		t.Fatal(err)
	}
	intentPath := filepath.Join(request.CoordinatorStateDir, "steering-intents.json")
	if _, err := os.Lstat(intentPath); !os.IsNotExist(err) {
		t.Fatalf("read-only capture created an intent file: %v", err)
	}
	if err := os.WriteFile(intentPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckReleaseHistoryAdoptionV2Source(t.Context(), path, configBytes, raw, ReleaseMeasurementContentHash(raw)); err == nil {
		t.Fatal("occupied source acquired actual-absence authority")
	}
	if _, err := CaptureReleaseHistoryAdoptionV2(t.Context(), path, configBytes, request.ApprovedPlanHash, request.SourcePlanHash, request.FirstNativeEpoch); err == nil {
		t.Fatal("noncanonical occupied source was recaptured as empty")
	}
}
