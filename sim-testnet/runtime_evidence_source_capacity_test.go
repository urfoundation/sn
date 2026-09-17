//go:build linux || darwin

// Read the actual launch profile and exercise finite campaign admission with
// declared counts. No multi-gigabyte tape, wallet or public transport is opened.
package main

import (
	"context"
	"math"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/v2026/validator"
)

// These are the shared funded horizon and independently rounded source
// owners, not an estimate using only the accepted 5+3 observation epochs.
func TestRuntimeEvidenceSourceCapacityUsesActualCompleteLaunchProfile(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	if err := validateRuntimeEvidenceSourceCapacity(cfg); err != nil {
		t.Fatal(err)
	}
	for _, source := range cfg.Config.ValidatorEvidenceV2 {
		value, err := requiredRuntimeEvidenceSourceCapacity(cfg, source.Evidence.Bounds)
		if err != nil {
			t.Fatal(err)
		}
		if value.span != 10080 || value.closed != 35 || value.native != 29 || value.trails != 81072 || value.records != 648576 || value.decisionAttempts != 340 || value.captureFiles != 680010 {
			t.Fatalf("complete per-source and combined-operator count differs: %+v", value)
		}
		if value.historyBytes != 15775825920 || value.storageBytes != 22603104256 || value.storageFiles != 8780 || value.recordChunks != 3085 || value.recordPages != 25 || value.proofChunks != 129 || value.proofPages != 2 {
			t.Fatalf("accepted wire/history/whole-row packing differs: %+v", value)
		}
		if value.objectsPerHour != 18575 || value.bytesPerHour != 15406497792 || value.retryRequestsPerHour != 2555000 {
			t.Fatalf("whole catch-up and actual poll/native retry allowance differs: %+v", value)
		}
		if source.Evidence.Bounds.CaptureFileLimit() != 700000 || source.Evidence.Bounds.MaxHistoryBytes != 16*1024*1024*1024 {
			t.Fatal("explicit count/history margin changed without its capacity receipt")
		}
	}
}

// Independent scalar minima refuse one-below even when all other owners
// retain their larger approved allowances. Each fault names the real field.
func TestRuntimeEvidenceSourceCapacityRejectsEachInsufficientOwner(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		change func(*validatorpkg.ReleaseEvidenceV2Bounds, runtimeEvidenceSourceCapacity)
	}{
		{name: "disk.maxtrailcount", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Disk.MaxTrailCount = value.trails - 1
		}},
		{name: "disk.maxrecordcount", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Disk.MaxRecordCount = value.records - 1
		}},
		{name: "disk.maxrawrecordbytes", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Disk.MaxRawRecordBytes = value.recordBytes - 1
		}},
		{name: "disk.maxproofbytes", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Disk.MaxProofBytes = value.proofBytes - 1
		}},
		{name: "disk.maxstoragebytes", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Disk.MaxStorageBytes = value.storageBytes - 1
		}},
		{name: "disk.maxstoragefiles", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Disk.MaxStorageFiles = value.storageFiles - 1
		}},
		{name: "cut.records.maxchunks", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Cut.Records.MaxChunks = value.recordChunks - 1
		}},
		{name: "cut.records.maxpages", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Cut.Records.MaxPages = value.recordPages - 1
		}},
		{name: "cut.proofs.maxchunks", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Cut.Proofs.MaxChunks = value.proofChunks - 1
		}},
		{name: "cut.proofs.maxpages", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Cut.Proofs.MaxPages = value.proofPages - 1
		}},
		{name: "replay.maxscratchbytes", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Replay.MaxScratchBytes = value.storageBytes - 1
		}},
		{name: "replay.maxscratchfiles", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.Replay.MaxScratchFiles = value.storageFiles - 1
		}},
		{name: "max_capture_files", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.MaxCaptureFiles = value.captureFiles - 1
		}},
		{name: "max_history_bytes", change: func(bounds *validatorpkg.ReleaseEvidenceV2Bounds, value runtimeEvidenceSourceCapacity) {
			bounds.MaxHistoryBytes = value.historyBytes - 1
		}},
	} {
		cfg := runtimeEvidenceLaunchConfigTest(t)
		bounds := &cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds
		value, err := requiredRuntimeEvidenceSourceCapacity(cfg, *bounds)
		if err != nil {
			t.Fatal(err)
		}
		testCase.change(bounds, value)
		if err := validateRuntimeEvidenceSourceCapacity(cfg); err == nil || !strings.Contains(err.Error(), testCase.name) {
			t.Errorf("one-below %s was not its own admission refusal: %v", testCase.name, err)
		}
	}
}

// MaxProviders is per operator. A second operator may not borrow the first
// one's identical capture slots; no capacity is inferred from archive bytes.
func TestRuntimeEvidenceSourceCapacityCombinesEveryOperatorAndPreservesDefaults(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	for index := range cfg.Config.ValidatorEvidenceV2 {
		bounds := &cfg.Config.ValidatorEvidenceV2[index].Evidence.Bounds
		value, err := requiredRuntimeEvidenceSourceCapacity(cfg, *bounds)
		if err != nil {
			t.Fatal(err)
		}
		if value.captureFiles != value.decisionAttempts*2*bounds.MaxProviders+2*uint64(validatorProcessRestartLimit) {
			t.Fatal("combined operator count lost its real dimensions")
		}
		bounds.MaxCaptureFiles = value.captureFiles
	}
	if err := validateRuntimeEvidenceSourceCapacity(cfg); err != nil {
		t.Fatalf("exact combined count: %v", err)
	}
	cfg.Config.ValidatorEvidenceV2[1].Evidence.Bounds.MaxCaptureFiles = 0
	if err := validateRuntimeEvidenceSourceCapacity(cfg); err == nil || !strings.Contains(err.Error(), "max_capture_files") {
		t.Fatal("omitted compatibility count became a launch-sized grant")
	}
	cfg.Config.ValidatorEvidenceV2[1].Evidence.Bounds.MaxCaptureFiles = 340000 + 10
	if err := validateRuntimeEvidenceSourceCapacity(cfg); err == nil || !strings.Contains(err.Error(), "max_capture_files") {
		t.Fatal("one operator's capacity admitted both operators")
	}
}

// Catch-up may deliver every retained original within one UTC-hour bucket.
// The caller must fund repeated requests as well as new immutable content.
func TestRuntimeEvidenceSourceCapacityChargesWholeCatchupAndEachQuotaDimension(t *testing.T) {
	for _, dimension := range []string{"objects", "bytes", "requests"} {
		cfg := runtimeEvidenceLaunchConfigTest(t)
		value, err := requiredRuntimeEvidenceSourceCapacity(cfg, cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds)
		if err != nil {
			t.Fatal(err)
		}
		for index := range cfg.Config.Artifacts.ReservedAttemptUploads {
			budget := &cfg.Config.Artifacts.ReservedAttemptUploads[index].Budget
			budget.ObjectsPerHour, budget.BytesPerHour, budget.RetryRequestsPerHour = value.objectsPerHour, value.bytesPerHour, value.retryRequestsPerHour
		}
		if err := validateRuntimeEvidenceSourceCapacity(cfg); err != nil {
			t.Fatalf("exact whole-catchup quota: %v", err)
		}
		budget := &cfg.Config.Artifacts.ReservedAttemptUploads[1].Budget
		switch dimension {
		case "objects":
			budget.ObjectsPerHour--
		case "bytes":
			budget.BytesPerHour--
		case "requests":
			budget.RetryRequestsPerHour--
		}
		if err := validateRuntimeEvidenceSourceCapacity(cfg); err == nil || !strings.Contains(err.Error(), "protected publication capacity") {
			t.Fatalf("one-below %s quota accepted: %v", dimension, err)
		}
	}
}

// The actual full-phase entrypoint must refuse before constructing a relay,
// invoking Prepare, touching its absent state path or issuing any chain call.
func TestRuntimeEvidenceSourceCapacityPrecedesActualCampaignPrepare(t *testing.T) {
	for _, phase := range []string{"release-1.0", "production-soak"} {
		cfg := runtimeEvidenceLaunchConfigTest(t)
		cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds.Disk.MaxTrailCount = 40960
		prepared := false
		result, err := runScenarioWithEvidenceRelay(t.Context(), cfg, "", scenarioDefinition{Name: phase}, nil, scenarioRunOptions{Prepare: func(context.Context) error { prepared = true; return nil }}, nil)
		if err == nil || result != nil || prepared || !strings.Contains(err.Error(), "disk.maxtrailcount") {
			t.Fatalf("%s reached execution through its old source ceiling: %v", phase, err)
		}
	}
}

// Checked arithmetic and the real writer's page-byte admission both precede
// any source allocation. A nominal descriptor count cannot overrule its wire.
func TestRuntimeEvidenceSourceCapacityRejectsOverflowAndCanonicalPackingGaps(t *testing.T) {
	for _, dimension := range []string{"clock", "raw-product", "count", "newline", "page-bytes"} {
		cfg := runtimeEvidenceLaunchConfigTest(t)
		bounds := &cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds
		switch dimension {
		case "clock":
			cfg.Public.Chain.ExpectedBlockSeconds = math.MaxUint64
		case "raw-product":
			bounds.Disk.MaxRecordCount = uint64(math.MaxInt64) / 2
			bounds.Cut.Records.MaxItems = bounds.Disk.MaxRecordCount
		case "count":
			bounds.MaxCaptureFiles = math.MaxUint64
		case "newline":
			bounds.Replay.MaxRecordBytes = bounds.Disk.MaxRecordBytes
		case "page-bytes":
			bounds.Cut.Records.MaxPageBytes = 512
			if err := bounds.Validate(2); err != nil {
				t.Fatalf("bounded one-descriptor page fixture is malformed: %v", err)
			}
			capacity, err := bounds.Cut.Records.WriterPageCapacity(validatorpkg.AttemptStreamV2Records, bounds.Replay.MaxRecordBytes)
			if err != nil || capacity >= bounds.Cut.Records.MaxDescriptorsPerPage {
				t.Fatal("actual page bytes did not narrow the nominal descriptor census")
			}
		}
		if err := validateRuntimeEvidenceSourceCapacity(cfg); err == nil {
			t.Errorf("%s capacity acquired unchecked work", dimension)
		}
	}
}
