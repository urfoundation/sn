//go:build linux || darwin

// Read the actual launch profile and exercise finite campaign admission with
// declared counts. No multi-gigabyte tape, wallet or public transport is opened.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	validatorpkg "github.com/urfoundation/sn/validator"
)

func runtimeEvidenceProvisionalSourceCapacityTest(t *testing.T) *ResolvedConfig {
	t.Helper()
	cfg := runtimeEvidenceLaunchConfigTest(t)
	// The scalar forecast selects the same owned mode as real LAN routing.
	// This documentation address is never passed to a transport or dialer.
	cfg.OperationalRPCMode = rpcModeOwnedNode
	cfg.ownedRPCAuthority = "192.0.2.40:9944"
	cfg.provisionalResume = &provisionalResumeState{Record: &provisionalResumeRecord{
		Schema: "urnetwork-sim-provisional-resume-v1", Provisional: true, FinalAcceptance: false,
		PlanHash: "0x" + strings.Repeat("71", 32), ConfigHash: cfg.ConfigHash, DeploymentID: cfg.Config.Deployment.DeploymentID,
	}}
	return cfg
}

// Keep waiver controls below the computed minimum when launch quotas grow.
func runtimeEvidenceProvisionalSourceShortfallTest(t *testing.T) *ResolvedConfig {
	t.Helper()
	cfg := runtimeEvidenceProvisionalSourceCapacityTest(t)
	minimum, err := requiredRuntimeEvidenceSourceCapacity(cfg, cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds)
	if err != nil || minimum.retryRequestsPerHour <= 1 {
		t.Fatalf("test needs a finite retry forecast: %+v %v", minimum, err)
	}
	for index := range cfg.Config.Artifacts.ReservedAttemptUploads {
		cfg.Config.Artifacts.ReservedAttemptUploads[index].Budget.RetryRequestsPerHour = minimum.retryRequestsPerHour - 1
	}
	return cfg
}

// The old public-only fixture missed the fourfold poll frequency of the
// owned route. Check its independent count and all configured quota axes.
func TestRuntimeEvidenceSourceCapacityForecastUsesActualOwnedPoll(t *testing.T) {
	for _, testCase := range []struct {
		mode                     string
		poll                     int
		objects, bytes, requests uint64
	}{
		{mode: rpcModePublicOverride, poll: 60, objects: 18575, bytes: 15406497792, requests: 2555000},
		{mode: rpcModePrivateAuthority, poll: 15, objects: 18935, bytes: 15430090752, requests: 6012260},
		{mode: rpcModeOwnedNode, poll: 15, objects: 18935, bytes: 15430090752, requests: 6012260},
	} {
		cfg := runtimeEvidenceLaunchConfigTest(t)
		cfg.OperationalRPCMode = testCase.mode
		value, err := requiredRuntimeEvidenceSourceCapacity(cfg, cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds)
		if err != nil || validatorPollSeconds(cfg) != testCase.poll || value.objectsPerHour != testCase.objects || value.bytesPerHour != testCase.bytes || value.retryRequestsPerHour != testCase.requests {
			t.Fatalf("%s actual poll forecast=%+v: %v", testCase.mode, value, err)
		}
		var diagnostic bytes.Buffer
		err = validateRuntimeEvidenceSourceCapacityWithDiagnostics(cfg, &diagnostic)
		if err != nil || diagnostic.Len() != 0 {
			t.Fatalf("%s configured forecast no longer fits: %v %s", testCase.mode, err, &diagnostic)
		}
		if testCase.poll == 60 {
			continue
		}
		// Keep the strict diagnostic regression independent from the launch
		// profile: the profile must fit the owned LAN cadence.
		for index := range cfg.Config.Artifacts.ReservedAttemptUploads {
			cfg.Config.Artifacts.ReservedAttemptUploads[index].Budget.RetryRequestsPerHour = 4_194_304
		}
		diagnostic.Reset()
		err = validateRuntimeEvidenceSourceCapacityWithDiagnostics(cfg, &diagnostic)
		if err == nil || diagnostic.Len() != 0 || strings.Count(err.Error(), "protected publication capacity is below") != 4 {
			t.Fatalf("%s strict refusal omitted an original/destination owner: %v %s", testCase.mode, err, &diagnostic)
		}
		for _, field := range []string{"validator_poll_seconds=15", "objects_per_hour=32768 required_objects_per_hour=18935", "bytes_per_hour=34359738368 required_bytes_per_hour=15430090752", "retry_requests_per_hour=4194304 required_retry_requests_per_hour=6012260"} {
			if strings.Count(err.Error(), field) != 4 {
				t.Errorf("%s strict forecast lost exact %s: %v", testCase.mode, field, err)
			}
		}
	}
}

// Provisional admission preserves the poll, configuration, finite operator
// quotas and full source horizon; it reports no accepted result.
func TestRuntimeEvidenceSourceCapacityProvisionalOwnedForecastPreservesRuntimeLimits(t *testing.T) {
	cfg := runtimeEvidenceProvisionalSourceCapacityTest(t)
	// The checked-in profile fits the owned LAN cadence. This fixture is
	// deliberately below only the retry dimension to verify that a valid
	// provisional approval records the bounded shortfall without mutating it.
	for index := range cfg.Config.Artifacts.ReservedAttemptUploads {
		cfg.Config.Artifacts.ReservedAttemptUploads[index].Budget.RetryRequestsPerHour = 4_194_304
	}
	before, err := canonicalHashHex(cfg.Config)
	if err != nil {
		t.Fatal(err)
	}
	configHash := cfg.ConfigHash
	value, err := requiredRuntimeEvidenceSourceCapacity(cfg, cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds)
	if err != nil || value.span != 10080 || value.objectsPerHour != 18935 || value.bytesPerHour != 15430090752 || value.retryRequestsPerHour != 6012260 {
		t.Fatalf("provisional mode changed source forecast: %+v %v", value, err)
	}
	var diagnostic bytes.Buffer
	if err := validateRuntimeEvidenceSourceCapacityWithDiagnostics(cfg, &diagnostic); err != nil {
		t.Fatalf("non-accepting owned campaign stopped on an hourly forecast: %v", err)
	}
	for _, field := range []string{"forecast_waived=true", "runtime_limits_unchanged=true", "final_acceptance=false", "required_retry_requests_per_hour=6012260", "validator 2 replica 2"} {
		if !strings.Contains(diagnostic.String(), field) {
			t.Errorf("provisional advisory lost %s: %s", field, &diagnostic)
		}
	}
	after, err := canonicalHashHex(cfg.Config)
	if err != nil || before != after || cfg.ConfigHash != configHash || validatorPollSeconds(cfg) != 15 || !ownedRPCOnly(cfg) || cfg.provisionalResume.Record.FinalAcceptance {
		t.Fatalf("advisory changed approved runtime inputs: %v", err)
	}
	if _, err := runtimeAttemptUploadBudget(cfg); err != nil {
		t.Fatalf("adjacent ordinary upload capacity does not fit unchanged: %v", err)
	}
}

// Exercise the actual release and production entrypoint without a live
// executor. Both must pass the forecast and stop at the absent relay owner.
func TestRuntimeEvidenceSourceCapacityProvisionalOwnedReachesRealRelayBoundary(t *testing.T) {
	for _, phase := range []string{"release-1.0", "production-soak"} {
		cfg := runtimeEvidenceProvisionalSourceCapacityTest(t)
		prepared := false
		result, err := runScenarioWithEvidenceRelay(t.Context(), cfg, "", scenarioDefinition{Name: phase}, nil, scenarioRunOptions{Prepare: func(context.Context) error { prepared = true; return nil }}, nil)
		if err == nil || err.Error() != "evidence relay runtime owners are incomplete" || result != nil || prepared {
			t.Fatalf("%s provisional forecast blocked before real relay ownership: %v", phase, err)
		}
	}
}

// Each finite hourly dimension can be a forecast shortfall. Aggregate both
// destinations and both sources instead of failing sequentially at replica 1.
func TestRuntimeEvidenceSourceCapacityProvisionalForecastCoversEachQuotaDimension(t *testing.T) {
	for _, dimension := range []string{"objects", "bytes", "requests", "all"} {
		cfg := runtimeEvidenceProvisionalSourceCapacityTest(t)
		value, err := requiredRuntimeEvidenceSourceCapacity(cfg, cfg.Config.ValidatorEvidenceV2[0].Evidence.Bounds)
		if err != nil {
			t.Fatal(err)
		}
		for index := range cfg.Config.Artifacts.ReservedAttemptUploads {
			budget := &cfg.Config.Artifacts.ReservedAttemptUploads[index].Budget
			budget.ObjectsPerHour, budget.BytesPerHour, budget.RetryRequestsPerHour = value.objectsPerHour, value.bytesPerHour, value.retryRequestsPerHour
			if dimension == "objects" || dimension == "all" {
				budget.ObjectsPerHour--
			}
			if dimension == "bytes" || dimension == "all" {
				budget.BytesPerHour--
			}
			if dimension == "requests" || dimension == "all" {
				budget.RetryRequestsPerHour--
			}
		}
		var diagnostic bytes.Buffer
		if err := validateRuntimeEvidenceSourceCapacityWithDiagnostics(cfg, &diagnostic); err != nil || strings.Count(diagnostic.String(), "protected publication capacity is below") != 4 {
			t.Fatalf("%s provisional quota census: %v %s", dimension, err, &diagnostic)
		}
		cfg.provisionalResume = nil
		diagnostic.Reset()
		if err := validateRuntimeEvidenceSourceCapacityWithDiagnostics(cfg, &diagnostic); err == nil || strings.Count(err.Error(), "protected publication capacity is below") != 4 || diagnostic.Len() != 0 {
			t.Fatalf("%s strict quota census: %v %s", dimension, err, &diagnostic)
		}
	}
}

// An in-memory flag or an accepting/foreign record cannot waive a forecast.
func TestRuntimeEvidenceSourceCapacityForecastRequiresNonAcceptingApproval(t *testing.T) {
	for _, field := range []string{"missing-record", "schema", "provisional", "final-acceptance", "chain", "config", "deployment", "plan"} {
		cfg := runtimeEvidenceProvisionalSourceShortfallTest(t)
		switch field {
		case "missing-record":
			cfg.provisionalResume.Record = nil
		case "schema":
			cfg.provisionalResume.Record.Schema = "different-schema"
		case "provisional":
			cfg.provisionalResume.Record.Provisional = false
		case "final-acceptance":
			cfg.provisionalResume.Record.FinalAcceptance = true
		case "chain":
			cfg.ChainID++
		case "config":
			cfg.provisionalResume.Record.ConfigHash = "0x" + strings.Repeat("72", 32)
		case "deployment":
			cfg.provisionalResume.Record.DeploymentID = "other-test-deployment"
		case "plan":
			cfg.provisionalResume.Record.PlanHash = "invalid-plan"
		}
		var diagnostic bytes.Buffer
		if err := validateRuntimeEvidenceSourceCapacityWithDiagnostics(cfg, &diagnostic); err == nil || diagnostic.Len() != 0 {
			t.Fatalf("%s admitted a forecast waiver: %v %s", field, err, &diagnostic)
		}
	}
}

// Invalid capacities, ownership, arithmetic, history and real source bounds
// remain fatal even after a valid earlier replica has a retry forecast deficit.
func TestRuntimeEvidenceSourceCapacityProvisionalForecastRetainsHardAdmission(t *testing.T) {
	for _, field := range []string{"objects-zero", "bytes-zero", "requests-zero", "owner-product", "owner-census", "history", "record-newline", "record-product", "source-trails", "source-captures", "source-history", "source-operator", "later-source"} {
		cfg := runtimeEvidenceProvisionalSourceShortfallTest(t)
		destination := &cfg.Config.Artifacts.ReservedAttemptUploads[1]
		bounds := &cfg.Config.ValidatorEvidenceV2[1].Evidence.Bounds
		switch field {
		case "objects-zero":
			destination.Budget.ObjectsPerHour = 0
		case "bytes-zero":
			destination.Budget.BytesPerHour = 0
		case "requests-zero":
			destination.Budget.RetryRequestsPerHour = 0
		case "owner-product":
			destination.Budget.RetryRequestsPerHour = 9007199254740991
		case "owner-census":
			destination.Admission.ReplicaNoID = 1
		case "history":
			destination.Admission.MaximumRanges = 1
			destination.Admission.BlocksPerRange = 1
		case "record-newline":
			bounds.Replay.MaxRecordBytes = bounds.Disk.MaxRecordBytes
		case "record-product":
			bounds.Disk.MaxRecordCount = uint64(math.MaxInt64) / 2
			bounds.Cut.Records.MaxItems = bounds.Disk.MaxRecordCount
		case "source-trails":
			bounds.Disk.MaxTrailCount = 40960
		case "source-captures":
			bounds.MaxCaptureFiles = 340010
		case "source-history":
			bounds.MaxHistoryBytes = 1024
		case "source-operator":
			cfg.Config.ValidatorEvidenceV2[1].Evidence.Operators[1].NoID = 1
		case "later-source":
			cfg.Config.ValidatorEvidenceV2[1].ValidatorID = 1
		}
		var diagnostic bytes.Buffer
		if err := validateRuntimeEvidenceSourceCapacityWithDiagnostics(cfg, &diagnostic); err == nil || diagnostic.Len() != 0 {
			t.Fatalf("%s became an advisory after an earlier forecast deficit: %v %s", field, err, &diagnostic)
		}
	}
}

func TestRuntimeEvidenceSourceCapacityForecastRequiresRecordedDiagnostic(t *testing.T) {
	cfg := runtimeEvidenceProvisionalSourceShortfallTest(t)
	if err := validateRuntimeEvidenceSourceCapacityWithDiagnostics(cfg, nil); err == nil || !strings.Contains(err.Error(), "no diagnostic owner") {
		t.Fatalf("missing advisory output was silently admitted: %v", err)
	}
	path := filepath.Join(t.TempDir(), "read-only-diagnostic")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := validateRuntimeEvidenceSourceCapacityWithDiagnostics(cfg, writer); err == nil || !strings.Contains(err.Error(), "record provisional source forecast") {
		t.Fatalf("failed advisory output was silently admitted: %v", err)
	}
	for _, source := range cfg.Config.ValidatorEvidenceV2 {
		for _, destination := range cfg.Config.Artifacts.ReservedAttemptUploads {
			if err := destination.ValidateCapacity(); err != nil {
				t.Fatal(fmt.Errorf("validator %d replica %d runtime capacity: %w", source.ValidatorID, destination.Admission.ReplicaNoID, err))
			}
		}
	}
}

// No waiver is needed when the configured quotas cover the whole forecast.
func TestRuntimeEvidenceSourceCapacityFittingForecastNeedsNoDiagnostic(t *testing.T) {
	cfg := runtimeEvidenceProvisionalSourceCapacityTest(t)
	if err := validateRuntimeEvidenceSourceCapacityWithDiagnostics(cfg, nil); err != nil {
		t.Fatalf("fitting forecast required an unused diagnostic writer: %v", err)
	}
}

// A partial write with no writer error still fails the required receipt.
type runtimeEvidenceShortDiagnosticTest struct{}

// Return a deterministic incomplete receipt without relying on filesystem timing.
func (self runtimeEvidenceShortDiagnosticTest) Write(encoded []byte) (int, error) {
	if len(encoded) == 0 {
		return 0, nil
	}
	return len(encoded) - 1, nil
}

// Advisory admission requires every diagnostic byte, including its census.
func TestRuntimeEvidenceSourceCapacityForecastRejectsShortDiagnostic(t *testing.T) {
	cfg := runtimeEvidenceProvisionalSourceShortfallTest(t)
	err := validateRuntimeEvidenceSourceCapacityWithDiagnostics(cfg, runtimeEvidenceShortDiagnosticTest{})
	if !errors.Is(err, io.ErrShortWrite) || strings.Count(err.Error(), "protected publication capacity is below") != 4 {
		t.Fatalf("partial advisory was silently admitted or lost its shortfall census: %v", err)
	}
}

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
