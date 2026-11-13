//go:build linux || darwin

// Actual launch configuration, runtime admission and process construction
// retain full-population logical charges without allocating their byte budget.
package main

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/protocol"
	validatorcomponent "github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/server/v2026/model"
)

// Independent arithmetic includes every miner, not just the 404 currently
// active clients per operator. Both Rpc selections retain the same capacity.
func TestRuntimeEvidenceV2UploadCapacityFitsActualFullPopulation(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	want := model.StAttemptUploadBudget{RequestsPerHour: 201568, BytesPerHour: 2141846503424, AccountRequestsPerHour: 74096, AccountBytesPerHour: 587336777728}
	for _, mode := range []string{rpcModePublicOverride, rpcModePrivateAuthority} {
		cfg.OperationalRPCMode = mode
		minimum, err := requiredRuntimeClientKeyUploadBudget(cfg)
		if err != nil || minimum != want {
			t.Fatalf("%s full-population minimum=%+v want=%+v: %v", mode, minimum, want, err)
		}
		actual, err := runtimeAttemptUploadBudget(cfg)
		if err != nil || actual != *cfg.Config.Artifacts.AttemptUpload {
			t.Fatalf("%s checked-in capacity cannot launch unchanged: %v", mode, err)
		}
	}
	if cfg.Config.Topology.Miners != 1000 || cfg.Config.Topology.fleetCandidateMiners() != 808 || cfg.Config.Topology.Validators != 2 || protocol.MaxClientKeyObservationReservationAttempts != 2 {
		t.Fatal("quota observation changed its actual population or fallback census")
	}
	t.Logf("miners=1000 active=808 validators=2 logical_observations_per_account_hour=70000 minimum=%+v configured=%+v; reserved counters are not stored or resident bytes", want, *cfg.Config.Artifacts.AttemptUpload)
}

// Exact equality passes while every one-unit shortfall refuses. The original
// tiny account quota must not acquire an otherwise valid full-launch owner.
func TestRuntimeEvidenceV2UploadCapacityRejectsEachExactShortfall(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	minimum, err := requiredRuntimeClientKeyUploadBudget(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Config.Artifacts.AttemptUpload = &minimum
	if actual, err := runtimeAttemptUploadBudget(cfg); err != nil || actual != minimum {
		t.Fatalf("exact quota boundary did not pass: %v", err)
	}
	for _, field := range []string{"requests_per_hour", "bytes_per_hour", "account_requests_per_hour", "account_bytes_per_hour"} {
		changed := minimum
		switch field {
		case "requests_per_hour":
			changed.RequestsPerHour--
		case "bytes_per_hour":
			changed.BytesPerHour--
		case "account_requests_per_hour":
			changed.AccountRequestsPerHour--
		case "account_bytes_per_hour":
			changed.AccountBytesPerHour--
		}
		cfg.Config.Artifacts.AttemptUpload = &changed
		actual, err := runtimeAttemptUploadBudget(cfg)
		if err == nil || actual != (model.StAttemptUploadBudget{}) || !strings.Contains(err.Error(), "artifacts.attempt_upload."+field+"=") {
			t.Fatalf("%s shortfall acquired a quota owner: %+v %v", field, actual, err)
		}
	}
}

// Configuration admission catches the observed 128 MiB account budget before
// later runtime identity, setup files, network clients or signing are involved.
func TestRuntimeEvidenceV2UploadCapacityRefusesOriginalQuotaAtStartup(t *testing.T) {
	cfg := runtimeEvidenceLaunchConfigTest(t)
	original := runtimeOrdinaryUploadAllowance()
	cfg.Config.Artifacts.AttemptUpload = &original
	if original.AccountBytesPerHour/uint64(protocol.MaxClientKeyHistoryResponseBytes) != 16 {
		t.Fatal("original quota no longer reproduces the observed 16-response limit")
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "full-population client-key workload minimum") {
		t.Fatalf("underprovisioned full-launch configuration passed startup: %v", err)
	}
	stateDir := t.TempDir()
	before := validatorNamespaceTreeSnapshot(t, stateDir)
	bins := map[string]string{"sim-testnet": "test-owned-unexecuted-binary"}
	if err := LaunchDeployment(t.Context(), cfg, stateDir, nil, nil, nil, bins, false); err == nil || !strings.Contains(err.Error(), "full-population client-key workload minimum") {
		t.Fatalf("insufficient quota reached supervisor, account or secret work: %v", err)
	}
	if after := validatorNamespaceTreeSnapshot(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatal("quota refusal mutated the retained launch namespace")
	}
}

// Shape, scalar and multiplication faults cannot wrap a huge workload into a
// small admissible counter; no mutable cfg owner is returned on these paths.
func TestRuntimeEvidenceV2UploadCapacityRejectsInvalidWorkloadAndOverflow(t *testing.T) {
	for _, fault := range []string{"operators", "miners", "validators", "assignment", "indivisible", "tempo", "fractional-tempo", "period-overflow", "product-overflow", "population-overflow", "attack-rate"} {
		cfg := testResolvedConfig(t)
		switch fault {
		case "operators":
			cfg.Config.Topology.Operators = 0
		case "miners":
			cfg.Config.Topology.Miners = -1
		case "validators":
			cfg.Config.Topology.Validators = 0
		case "assignment":
			cfg.Config.Topology.OperatorAssignment = "unknown"
		case "indivisible":
			cfg.Config.Topology.Miners++
		case "tempo":
			cfg.Hyperparameters.OwnerControlled["tempo"] = uint64(0)
		case "fractional-tempo":
			cfg.Hyperparameters.OwnerControlled["tempo"] = 360.5
		case "period-overflow":
			cfg.Hyperparameters.OwnerControlled["tempo"] = uint64(math.MaxUint64)
		case "product-overflow":
			cfg.Public.Chain.ExpectedBlockSeconds = math.MaxUint64
		case "population-overflow":
			cfg.Config.Topology.Miners = math.MaxInt - 1
		case "attack-rate":
			cfg.Config.Scenarios.Adversaries.MaximumOperatorRequestsPerSec = 21
		}
		if actual, err := requiredRuntimeClientKeyUploadBudget(cfg); err == nil || actual != (model.StAttemptUploadBudget{}) {
			t.Fatalf("%s workload acquired finite-looking counters: %+v %v", fault, actual, err)
		}
	}
	for _, cfg := range []*ResolvedConfig{nil, {}, {Config: &HarnessConfig{}}} {
		if _, err := requiredRuntimeClientKeyUploadBudget(cfg); err == nil {
			t.Fatal("absent workload acquired quota capacity")
		}
	}
}

// Native cadence and adversarial work change independently. Faster decisions
// add per-account charges; extra hostile traffic consumes only shared slack.
func TestRuntimeEvidenceV2UploadCapacityTracksCadenceAndAdversarialWork(t *testing.T) {
	cfg := testResolvedConfig(t)
	base, err := requiredRuntimeClientKeyUploadBudget(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Hyperparameters.OwnerControlled["tempo"] = uint64(59)
	faster, err := requiredRuntimeClientKeyUploadBudget(cfg)
	if err != nil || faster.AccountRequestsPerHour <= base.AccountRequestsPerHour || faster.AccountBytesPerHour <= base.AccountBytesPerHour {
		t.Fatalf("faster native cadence did not increase required quota: %+v %v", faster, err)
	}
	cfg = testResolvedConfig(t)
	cfg.Config.Scenarios.Adversaries.MaximumOperatorRequestsPerSec++
	hostile, err := requiredRuntimeClientKeyUploadBudget(cfg)
	if err != nil || hostile.AccountRequestsPerHour != base.AccountRequestsPerHour || hostile.AccountBytesPerHour != base.AccountBytesPerHour || hostile.RequestsPerHour-base.RequestsPerHour != 3600 || hostile.BytesPerHour-base.BytesPerHour != 3600*model.StAttemptUploadMaximumObjectBytes {
		t.Fatalf("hostile work changed the wrong quota namespace: %+v %v", hostile, err)
	}
}

// Process builders, not a comment or unrelated constant, own the actual
// restart budget used by the hourly full-census arithmetic.
func TestRuntimeEvidenceV2UploadCapacityUsesActualValidatorRestartLimit(t *testing.T) {
	if validatorProcessRestartLimit != 5 {
		t.Fatalf("approved validator restart profile changed: %d", validatorProcessRestartLimit)
	}
	cfg := testResolvedConfig(t)
	specs := buildClientSpecs(cfg, t.TempDir(), map[string]string{"sim-testnet": "test-owned-unexecuted-binary"}, &RoleSecrets{})
	validators := 0
	for _, spec := range specs {
		if spec.Role == "validator" {
			validators++
			if spec.RestartLimit != validatorProcessRestartLimit {
				t.Fatalf("%s process restart budget differs: %d", spec.ID, spec.RestartLimit)
			}
		}
	}
	if validators != cfg.Config.Topology.Validators || validatorcomponent.ReleaseSteeringFailureLimit != 10 {
		t.Fatal("quota census omitted the actual validator or failed-attempt limits")
	}
}
