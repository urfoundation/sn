package main

// Launch regressions follow their real package and private-service owner.
// Source-derived coverage cannot be satisfied by an empty or narrowed run.

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Both gates run these actual server sources only after their private profile
// is installed. Package order retains the existing shared-database ownership.
func releaseLaunchEvidenceServerGroup(t *testing.T) releaseEvidenceV2GateGroup {
	t.Helper()
	group := releaseEvidenceV2GateGroup{
		phase: "server_db", job: "server-db", variable: "evidence_source_tests",
		alternatives: "StAttempt|StReserved|StClientKeyHistory|StClientKeyPublication|StClientKeyRegistrationCohort|StClientKeyRegistrationReadiness|SnAttempt|SnReserved|SnClientKey|MigrationCatalog|PublishedMigration|MainVersion650MigratesThroughClientKeyAppend|TransferContractOpenPlanRepairMigrationOrder|ApplyDbMigrations|ContractResultErrorSeparatesReliabilityFromAccountFailures",
		packages:     []string{"./api", "./api/handlers", "./controller", "./model", "."},
		sources: map[string][]string{
			".":     releaseEvidenceV2GateSources(t, []string{"../../server/db_client_key_history_migration_test.go"}),
			"./api": releaseEvidenceV2GateSources(t, []string{"../../server/api/*attempt*route_test.go", "../../server/api/sn_client_key_history_batch_route_test.go"}),
			"./api/handlers": releaseEvidenceV2GateSources(t, []string{
				"../../server/api/handlers/sn_attempt*_test.go", "../../server/api/handlers/sn_reserved*_test.go", "../../server/api/handlers/sn_client_key_history*_test.go",
			}),
			"./controller": releaseEvidenceV2GateSources(t, []string{"../../server/controller/st_attempt_upload_test.go", "../../server/controller/st_client_key_history*_test.go", "../../server/controller/st_client_key_registration_cohort_test.go", "../../server/controller/st_client_key_registration_cohort_control_test.go", "../../server/controller/st_client_key_registration_readiness_test.go"}),
			"./model":      releaseEvidenceV2GateSources(t, []string{"../../server/model/st_attempt_upload_quota_test.go", "../../server/model/st_reserved_attempt_upload_test.go", "../../server/model/st_client_key_history_test.go"}),
		},
	}
	for _, packagePath := range group.packages {
		partition := ""
		if packagePath == "./controller" {
			partition = " -skip '^TestStClientKeyHistoryBatchActualFullPopulation(SharedBoundary|DistinctBoundaries)$' -timeout 10m"
		}
		group.commands = append(group.commands,
			fmt.Sprintf(`go test %s -run "$evidence_source_tests" -count=1`, packagePath)+partition,
			fmt.Sprintf(`go test -race %s -run "$evidence_source_tests" -count=1`, packagePath)+partition)
	}
	return group
}

// The previous gates compiled or omitted these paths without executing their
// real authentication, cancellation, durable rotation or Redis assertions.
func TestProducerGateStateSelectionCoversLaunchServerSourcesInBothGates(t *testing.T) {
	t.Parallel()
	group := releaseLaunchEvidenceServerGroup(t)
	for _, path := range []string{"../scripts/test-release-1.0-producer-gate.sh", "../scripts/test-release-1.0-local.sh"} {
		encoded, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyReleaseEvidenceV2GateGroup(string(encoded), group); err != nil {
			t.Fatalf("%s omits real launch server qualification: %v", path, err)
		}
	}
}

// Removing any family, either executable mode or the actual admitted phase
// deterministically reproduces an incomplete gate without running a service.
func TestProducerGateStateSelectionRejectsLaunchServerCoverageOmissions(t *testing.T) {
	t.Parallel()
	group := releaseLaunchEvidenceServerGroup(t)
	encoded, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(encoded)
	if err := verifyReleaseEvidenceV2GateGroup(script, group); err != nil {
		t.Fatal(err)
	}
	selector, err := releaseConnectPolicySelectorAssignment(script, group.variable)
	if err != nil {
		t.Fatal(err)
	}
	for _, omitted := range strings.Split(group.alternatives, "|") {
		var retained []string
		for _, value := range strings.Split(group.alternatives, "|") {
			if value != omitted {
				retained = append(retained, value)
			}
		}
		changed := strings.Replace(script, group.variable+"='"+selector+"'", group.variable+"='^Test("+strings.Join(retained, "|")+")'", 1)
		if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
			t.Fatalf("removed launch server family %s was accepted", omitted)
		}
	}
	for _, command := range append(append([]string(nil), group.commands...), "release_gate_start server-db release_phase_server_db") {
		changed := strings.Replace(script, command, "# omitted: "+command, 1)
		if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
			t.Fatalf("removed launch server command %s was accepted", command)
		}
	}
}

// The original native source batch gets an independent job, not an implicit
// dependency on a long validator suite or a compile-only package inclusion.
func TestProducerGateStateSelectionCoversNativeSourceCommitmentJob(t *testing.T) {
	t.Parallel()
	encoded, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	group := releaseEvidenceV2GateGroup{
		phase: "evidence_native", job: "evidence-native", variable: "native_evidence_tests", packages: []string{"./crv4"},
		sources:  map[string][]string{"./crv4": releaseEvidenceV2GateSources(t, []string{"../crv4/source_commitment_test.go", "../crv4/source_commitment_runtime455_test.go"})},
		commands: []string{`go test ./crv4 -run "$native_evidence_tests" -count=1`, `go test -race ./crv4 -run "$native_evidence_tests" -count=1`},
	}
	script := string(encoded)
	if err := verifyReleaseEvidenceV2GateGroup(script, group); err != nil {
		t.Fatal(err)
	}
	for _, command := range append(append([]string(nil), group.commands...), "release_gate_start evidence-native release_phase_evidence_native") {
		if err := verifyReleaseEvidenceV2GateGroup(strings.Replace(script, command, "# omitted: "+command, 1), group); err == nil {
			t.Fatalf("native source qualification lost command %s", command)
		}
	}
}

// Cold release-scale fixtures belong to the existing semantic job. Three
// fresh 120-second singleton processes reconstruct the same graph three times;
// selecting them together retains the full census and its established bounds.
func TestProducerGateStateSelectionKeepsColdFixturesInSemanticJob(t *testing.T) {
	t.Parallel()
	encoded, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(encoded)
	roots := []string{
		"TestFinalSemanticArtifactDeploymentAdmissionPrecedesSignedReplay",
		"TestFinalSemanticDeploymentArtifactRejectsIgnoredRuntimeMap",
		"TestFinalSemanticFixtureSnapshotsAreDetached",
		"TestFinalSemanticOriginalClosureRequiresExactCampaignStartMarker",
	}
	sources := releaseEvidenceV2GateSources(t, []string{
		"final_semantic_commit_test.go", "final_semantic_deployment_anchor_test.go",
		"final_semantic_fixture_work_test.go", "final_semantic_evidence_test.go",
	})
	var requiredRoots []string
	for _, name := range roots {
		requiredRoots = append(requiredRoots, regexp.QuoteMeta(name))
	}
	requiredSelector := "^(" + strings.Join(requiredRoots, "|") + ")$"
	verifyActualRoots := func(actualSources []string) error {
		actual, err := releaseSelectedTestDeclarations(requiredSelector, actualSources)
		if err != nil {
			return err
		}
		return verifyReleaseSemanticCensus(roots, actual)
	}
	if err := verifyActualRoots(sources); err != nil {
		t.Fatalf("cold fixture declarations differ from their actual source: %v", err)
	}
	group := releaseEvidenceV2GateGroup{
		phase: "semantic", variable: "semantic_integrity_tests", requiredSelector: requiredSelector,
		packages: []string{"./sim-testnet"}, sources: map[string][]string{"./sim-testnet": sources},
		commands: []string{
			`go test ./sim-testnet -run "$semantic_integrity_tests" -count=1` + releaseGateSemanticOwnerSkip + ` -parallel=4 -timeout 15m`,
			`go test -race ./sim-testnet -run "$semantic_integrity_tests" -count=1` + releaseGateSemanticOwnerSkip + ` -parallel=4 -timeout 25m`,
		},
	}
	if err := verifyReleaseEvidenceV2GateGroup(script, group); err != nil {
		t.Fatal(err)
	}
	selector, err := releaseConnectPolicySelectorAssignment(script, group.variable)
	if err != nil {
		t.Fatal(err)
	}
	for _, omitted := range roots {
		changedSources := append([]string(nil), sources...)
		for index, source := range changedSources {
			changedSources[index] = strings.Replace(source, "func "+omitted+"(", "func TestOmittedColdFixture(", 1)
		}
		if err := verifyActualRoots(changedSources); err == nil {
			t.Fatalf("cold fixture %s survived a missing source declaration", omitted)
		}
		var retained []string
		for _, name := range roots {
			if name != omitted {
				retained = append(retained, regexp.QuoteMeta(name))
			}
		}
		changed := strings.Replace(script, group.variable+"='"+selector+"'", group.variable+"='^("+strings.Join(retained, "|")+")$'", 1)
		if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
			t.Fatalf("cold fixture %s escaped semantic admission", omitted)
		}
	}
	for _, command := range group.commands {
		changed := strings.Replace(script, command, strings.Replace(command, "-timeout ", "-timeout 120s # prior-bound=", 1), 1)
		if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
			t.Fatal("cold fixture accepted the short singleton deadline")
		}
	}
}

// Registration readiness is a processed server result, not carrier delivery.
// Both public client modules exercise the actual request and lifecycle path.
func releaseLaunchRegistrationGroups(t *testing.T) []releaseEvidenceV2GateGroup {
	t.Helper()
	groups := []releaseEvidenceV2GateGroup{
		{
			phase: "connect", variable: "client_key_registration_tests",
			alternatives: "ClientKeyRegistration|ApiOutOfBandControl|ControlSyncOob|ClientKeyManager|VerifyClientKeySignature|CanonicalCertChainBytes|ClientCloseAndWaitJoinsClientKeyPublisher",
			packages:     []string{"."},
			sources: map[string][]string{".": releaseEvidenceV2GateSources(t, []string{
				"../../connect/api_control_result_test.go", "../../connect/transfer_key_registration_test.go",
				"../../connect/transfer_key_test.go", "../../connect/transfer_control_oob_test.go",
				"../../connect/transfer_oob_control_refresh_test.go",
			})},
		},
		{
			phase: "sdk", variable: "provider_registration_tests",
			alternatives: "DeviceLocalProviderRegistration|DeviceLocalProviderConnected",
			packages:     []string{"."},
			sources:      map[string][]string{".": releaseEvidenceV2GateSources(t, []string{"../../sdk/device_local_registration_test.go"})},
		},
	}
	for index := range groups {
		group := &groups[index]
		group.commands = []string{
			fmt.Sprintf("go test . -run \"$%s\" -count=1", group.variable),
			fmt.Sprintf("go test -race . -run \"$%s\" -count=1", group.variable),
		}
	}
	return groups
}

// Compile-only SDK coverage and transport-only client roots cannot establish
// that durable registration gates a real swarm provider in either release gate.
func TestProducerGateStateSelectionCoversRegistrationReadinessInBothGates(t *testing.T) {
	t.Parallel()
	groups := releaseLaunchRegistrationGroups(t)
	for _, path := range []string{"../scripts/test-release-1.0-producer-gate.sh", "../scripts/test-release-1.0-local.sh"} {
		encoded, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, group := range groups {
			if err := verifyReleaseEvidenceV2GateGroup(string(encoded), group); err != nil {
				t.Fatalf("%s omits %s registration readiness: %v", path, group.phase, err)
			}
		}
	}
}

// Every omitted family, executable mode, phase, and escaped source root
// deterministically reproduces incomplete registration qualification.
func TestProducerGateStateSelectionRejectsRegistrationReadinessOmissions(t *testing.T) {
	t.Parallel()
	groups := releaseLaunchRegistrationGroups(t)
	for _, path := range []string{"../scripts/test-release-1.0-producer-gate.sh", "../scripts/test-release-1.0-local.sh"} {
		encoded, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		script := string(encoded)
		for _, group := range groups {
			if err := verifyReleaseEvidenceV2GateGroup(script, group); err != nil {
				t.Fatal(err)
			}
			selector, err := releaseConnectPolicySelectorAssignment(script, group.variable)
			if err != nil {
				t.Fatal(err)
			}
			for _, omitted := range strings.Split(group.alternatives, "|") {
				var retained []string
				for _, value := range strings.Split(group.alternatives, "|") {
					if value != omitted {
						retained = append(retained, value)
					}
				}
				changed := strings.Replace(script, group.variable+"='"+selector+"'", group.variable+"='^Test("+strings.Join(retained, "|")+")'", 1)
				if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
					t.Fatalf("%s accepted removed registration family %s", path, omitted)
				}
			}
			start := "release_gate_start " + group.phase + " release_phase_" + group.phase
			for _, command := range append(append([]string(nil), group.commands...), start) {
				changed := strings.Replace(script, command, "# omitted: "+command, 1)
				if err := verifyReleaseEvidenceV2GateGroup(changed, group); err == nil {
					t.Fatalf("%s accepted removed registration command %s", path, command)
				}
			}
			for index, source := range group.sources["."] {
				declaration := regexp.MustCompile("(?m)^func Test[[:alnum:]_]+\\(").FindString(source)
				if declaration == "" {
					t.Fatal("registration source has no test declaration")
				}
				changed := group
				changed.sources = map[string][]string{".": append([]string(nil), group.sources["."]...)}
				changed.sources["."][index] = strings.Replace(source, declaration, "func TestOmittedRegistrationCoverage(", 1)
				if err := verifyReleaseEvidenceV2GateGroup(script, changed); err == nil {
					t.Fatalf("%s accepted escaped registration root %s", path, declaration)
				}
			}
		}
	}
}
