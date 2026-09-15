// Full metadata, publication, private fixtures, prior replay, evidence codecs,
// typed-source boundaries and renewal fixtures own scoped capture budgets.
// Exact exclusions require admitted successors.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Pin the selected families independently of their growing source population.
const releaseGateCaptureSelector = "^Test(FinalArchive|FinalCompositeArchive|ArchivePreflight|FinalClaimQueueCapture|FinalCollected(Bundle|File|Chain)|FinalSemantic(PublicCapture|LaunchFoundation)|FinalContractCleanupCapture|VerifyFinalCollected|FleetLifecycle|CanonicalRPCReceiptLogs|ScenarioProcessLogGate|ReleaseAndProductionScenariosRequireProcessLogGate|ScenarioCompletion|ScenarioRunner(WritesCompleteEvidenceOnlyOnPass|FailureHasNoCompleteMarker)|PublishedScenarioCandidateKeepsFrozenHashWhenClockAdvances|PublishedCompletionCommits|CampaignEvidence|DirectScenarioCompletion|EvidenceFileHashes|ArchiveCurrentDeploymentPublication|VerifyPublishedEvidenceOrigin|ReleaseCandidateCampaign|ProductionCampaignCompletion|ReleaseCampaignGate|ExactReleaseCampaignGate|ScenarioCampaignAttempt|ProductionHandoff|InitialScenarioFailure|ProductionPolicyEvidence|PrepareSignedAttemptStateNamespace|ClassifyValidatorAttemptState|ValidatorStateNamespace|QualificationLauncher|SimulatorAttemptCutV2|ProducerGateStateSelection|ProducerGateCustodySelection|ProducerGateCaptureSelection|FinalCaptureV2|FinalCaptureCapacity|ScenarioNativeWarmupV2|ScenarioNativeObservationV2|StrictHistoryAdoption|FleetRenewal|OwnedRPC|CoordinatorRepairCarry)"

const releaseGateCapturePopulationRoot = "TestCampaignEvidencePopulationV2StreamsPhaseCensusWithBoundedOwners"

const releaseGateCapturePrivatePattern = "TestFinalCaptureV2(ReadsActualRenderedSetupAndRejectsChangedSource|PendingPriorClosesOriginalAuthority|PendingPriorRejectsRehashedSourceAndMissingCensus|PendingPriorRejectsWrongHandoffAndSemanticRelabel|PendingJobIsImmutableAndNeverAccepted|PendingPriorRejectsWrongGateBeforeWrites|PendingPriorArtifactCensusHasNoSemanticOutputs)"
const releaseGateCapturePriorRoot = "TestVerifyFinalCollectedPriorPhaseBytesRejectsReopenedHandoffSubstitution"
const releaseGateCaptureTypedPriorRoot = "TestFinalCaptureCapacityPriorCarrierDecodeV2KeepsTypedLimitsAndIntentSchema"
const releaseGateCaptureLifecycleRoot = "TestFleetLifecycleRenewalDescriptorsKeepLaterWaves"
const releaseGateCaptureEvidencePattern = "TestCampaignEvidence.*"
const releaseGateCaptureRenewalPattern = "Test(FleetRenewal(Budget(AccountsAllSignedAttemptsAndNonceGaps|DoesNotChargeRetiredGasTwice)|CLIRequiresExactImportedApproval|ExactEVMRecoveryDoesNotResignOrRebroadcast|FeeQuoteUsesExactApprovedCeiling|HistoricalScopeExcludesFundingAndUnrelatedActions|Pipeline(JoinsCanceledWorkers|SubmitsExactNoncesBeforeFinality)|PlansExpiredAndLiveMixedGenerations|RejectsChangedPrestateAndPreservesApproval)|FleetLifecycleRenewalAdmitsOnlyApprovedSuccessor)"
const releaseGateCaptureRevisionPattern = "TestFleetRenewalRevision(PreservesApprovedRoundsAndChargesOnce|RefusesCustodyFeeOrLiabilityChanges)"
const releaseGateCaptureEvidenceSkip = " -skip '^TestCampaignEvidence(CapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier|PopulationV2StreamsPhaseCensusWithBoundedOwners)$'"
const releaseGateCaptureOwnerSkip = " -skip '^(" + releaseGateCaptureEvidencePattern + "|" + releaseGateCapturePrivatePattern + "|" + releaseGateCapturePriorRoot + "|" + releaseGateCaptureTypedPriorRoot + "|" + releaseGateCaptureLifecycleRoot + "|" + releaseGateCaptureRenewalPattern + "|" + releaseGateCaptureRevisionPattern + ")$'"

// Inspect the gates' bounded line-oriented registry grammar, after removing
// declarations. Exact calls inside an extra branch are not admitted jobs.
func releaseGateRegistrationConditions(script string, registration string) ([]string, error) {
	definitions := regexp.MustCompile(`(?ms)^[\t ]*[a-zA-Z_][a-zA-Z0-9_]*\(\) \{\n.*?^[\t ]*\}[\t ]*$`)
	registry := definitions.ReplaceAllString(script, "")
	return releaseGateRegistryConditions(registry, registration)
}

// Reuse a declaration-free registry without scanning function bodies again.
// The caller retains the independent whole-script invocation census.
func releaseGateRegistryConditions(registry string, registration string) ([]string, error) {
	var conditions []string
	var registrationConditions []string
	count := 0
	for _, line := range strings.Split(registry, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == registration {
			count++
			registrationConditions = slices.Clone(conditions)
		}
		// Nothing after the first registration can change its ancestry. Count
		// later duplicates without parsing unrelated final-fence shell syntax.
		if count != 0 {
			continue
		}
		switch {
		case strings.HasPrefix(line, "if "):
			if !strings.HasSuffix(line, "; then") || len(conditions) >= 16 {
				return nil, fmt.Errorf("release registration has unsupported conditional syntax")
			}
			conditions = append(conditions, line)
		case line == "else", strings.HasPrefix(line, "elif "):
			if len(conditions) == 0 {
				return nil, fmt.Errorf("release registration has an unowned conditional branch")
			}
			conditions[len(conditions)-1] = line
		case line == "fi":
			if len(conditions) == 0 {
				return nil, fmt.Errorf("release registration has an unowned conditional end")
			}
			conditions = conditions[:len(conditions)-1]
		case line == "(", line == "{", strings.HasPrefix(line, "for "), strings.HasPrefix(line, "while "), strings.HasPrefix(line, "until "), strings.HasPrefix(line, "select "), strings.HasPrefix(line, "case "):
			return nil, fmt.Errorf("release registration has unsupported enclosing control flow")
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("release registration needs exactly one executable call")
	}
	return registrationConditions, nil
}

// Keep the exact body and its source boundary for command and admission checks.
type releaseGateCaptureDefinition struct {
	body string
	end  int
}

// Bind actual phase commands and registry entries, not comments or dead text.
// The ordinary selector remains protected by the complete source-family guards.
func verifyReleaseGateCaptureMetadataIsolation(script string) error {
	return verifyReleaseGateCaptureMetadataIsolationWithScan(script, func(pattern *regexp.Regexp, source string) [][]int {
		return pattern.FindAllStringSubmatchIndex(source, -1)
	})
}

// Index declarations once before checking every owner. The scanner argument
// lets the work regression bound actual whole-script matching without a clock.
func verifyReleaseGateCaptureMetadataIsolationWithScan(script string, scan func(*regexp.Regexp, string) [][]int) error {
	const fullRoot = "TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier"
	// Use the same declaration grammar as registration ancestry. Keeping all
	// names also rejects a selected phase hidden inside an unrelated function.
	definitions := regexp.MustCompile(`(?ms)^[\t ]*([a-zA-Z_][a-zA-Z0-9_]*)\(\) \{\n(.*?)^[\t ]*\}[\t ]*$`)
	definitionKVs := map[string][]releaseGateCaptureDefinition{}
	var registry strings.Builder
	previousEnd := 0
	for _, match := range scan(definitions, script) {
		name := script[match[2]:match[3]]
		definitionKVs[name] = append(definitionKVs[name], releaseGateCaptureDefinition{body: script[match[4]:match[5]], end: match[1]})
		registry.WriteString(script[previousEnd:match[0]])
		previousEnd = match[1]
	}
	registry.WriteString(script[previousEnd:])
	registrySource := registry.String()
	// An indented live call cannot authenticate a different unindented call
	// hidden in a removed definition. Both exact censuses must name one call.
	registryCallCountsKVs := map[string]int{}
	for line := range strings.SplitSeq(registrySource, "\n") {
		if strings.HasPrefix(line, "release_gate_start ") {
			registryCallCountsKVs[strings.TrimRight(line, "\t ")]++
		}
	}
	// Record exact unindented calls, including dead-body duplicates, in one
	// line pass. Registry ancestry below decides whether the sole call is live.
	callOffsetsKVs := map[string][]int{}
	offset := 0
	for line := range strings.SplitAfterSeq(script, "\n") {
		if strings.HasPrefix(line, "release_gate_start ") {
			command := strings.TrimRight(line, "\t \n")
			callOffsetsKVs[command] = append(callOffsetsKVs[command], offset)
		}
		offset += len(line)
	}
	for _, group := range []struct {
		phase       string
		job         string
		variable    string
		selector    string
		skip        string
		raceTimeout string
	}{
		{phase: "capture", job: "capture", variable: "capture_tests", selector: releaseGateCaptureSelector, skip: releaseGateCaptureOwnerSkip, raceTimeout: "10m"},
		{phase: "capture_evidence", job: "capture-evidence", variable: "capture_evidence_tests", selector: "^" + releaseGateCaptureEvidencePattern + "$", skip: releaseGateCaptureEvidenceSkip, raceTimeout: "10m"},
		{phase: "capture_renewal", job: "capture-renewal", variable: "capture_renewal_tests", selector: "^" + releaseGateCaptureRenewalPattern + "$", raceTimeout: "10m"},
		{phase: "capture_revision", job: "capture-revision", variable: "capture_revision_tests", selector: "^" + releaseGateCaptureRevisionPattern + "$", raceTimeout: "10m"},
		{phase: "capture_private", job: "capture-private", variable: "capture_private_tests", selector: "^" + releaseGateCapturePrivatePattern + "$", raceTimeout: "10m"},
		{phase: "capture_prior", job: "capture-prior", variable: "capture_prior_tests", selector: "^" + releaseGateCapturePriorRoot + "$", raceTimeout: "10m"},
		{phase: "capture_typed_prior", job: "capture-typed-prior", variable: "capture_typed_prior_tests", selector: "^" + releaseGateCaptureTypedPriorRoot + "$", raceTimeout: "10m"},
		{phase: "capture_lifecycle", job: "capture-lifecycle", variable: "capture_lifecycle_tests", selector: "^" + releaseGateCaptureLifecycleRoot + "$", raceTimeout: "10m"},
		{phase: "capture_population", job: "capture-population", variable: "capture_population_tests", raceTimeout: "10m"},
		{phase: "capture_metadata", job: "capture-metadata", variable: "capture_metadata_tests", raceTimeout: "45m"},
	} {
		function := "release_phase_" + group.phase
		phases := definitionKVs[function]
		if len(phases) != 1 {
			return fmt.Errorf("capture metadata needs exactly one %s phase", group.phase)
		}
		body := phases[0].body
		selector, err := releaseConnectPolicySelectorAssignment(body, group.variable)
		if err != nil {
			return err
		}
		if group.selector != "" && selector != group.selector {
			return fmt.Errorf("capture changed its exact %s partition", group.phase)
		}
		selectedPattern, err := regexp.Compile(selector)
		if err != nil {
			return err
		}
		for _, stress := range []struct {
			root  string
			phase string
		}{
			{root: fullRoot, phase: "capture_metadata"},
			{root: releaseGateCapturePopulationRoot, phase: "capture_population"},
			{root: releaseGateCaptureLifecycleRoot, phase: "capture_lifecycle"},
			{root: releaseGateCaptureTypedPriorRoot, phase: "capture_typed_prior"},
		} {
			selected := selectedPattern.MatchString(stress.root)
			wantSelected := group.phase == "capture" || group.phase == stress.phase || group.phase == "capture_evidence" && (stress.root == fullRoot || stress.root == releaseGateCapturePopulationRoot)
			if selected != wantSelected || group.phase == stress.phase && selector != "^"+stress.root+"$" {
				return fmt.Errorf("capture metadata changed its exact %s partition", stress.phase)
			}
		}
		var commands []string
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				commands = append(commands, line)
			}
		}
		expected := []string{
			`cd "$sn_repo"`,
			group.variable + "='" + selector + "'",
			`go test ./sim-testnet -run "$` + group.variable + `" -count=1` + group.skip + " -timeout 5m",
			`go test -race ./sim-testnet -run "$` + group.variable + `" -count=1` + group.skip + " -timeout " + group.raceTimeout,
		}
		if !slices.Equal(commands, expected) {
			return fmt.Errorf("capture metadata changed %s execution or its scoped budgets", group.phase)
		}
		start := "release_gate_start " + group.job + " " + function
		calls := callOffsetsKVs[start]
		if len(calls) != 1 || registryCallCountsKVs[start] != 1 || calls[0] < phases[0].end {
			return fmt.Errorf("capture metadata does not independently admit %s", group.phase)
		}
		conditions, err := releaseGateRegistryConditions(registrySource, start)
		if err != nil || len(conditions) != 0 {
			return fmt.Errorf("capture metadata has conditional job admission: %v %v", conditions, err)
		}
	}
	return nil
}

// The full census is neither duplicated inside ordinary capture nor omitted
// from the gate; each owner retains its independently reviewed mode budgets.
func TestProducerGateCaptureSelectionRequiresIndependentMetadata(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseGateCaptureMetadataIsolation(string(raw)); err != nil {
		t.Fatal(err)
	}
	metadataSource, err := os.ReadFile("evidence_metadata_census_v2_test.go")
	if err != nil {
		t.Fatal(err)
	}
	const selector = "^TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier$"
	declarations, err := releaseSelectedTestDeclarations(selector, []string{string(metadataSource)})
	if err != nil || len(declarations) != 1 {
		t.Fatalf("independent metadata phase lost its exact source root: %v", err)
	}
}

// The real full-size typed control owns its clock; the other codec and
// allocation controls retain their ordinary owner and original test bodies.
func TestProducerGateCaptureSelectionRequiresIndependentTypedPrior(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if err := verifyReleaseGateCaptureMetadataIsolation(script); err != nil {
		t.Fatalf("typed prior boundary lacks an independent unchanged budget: %v", err)
	}
	sources := releaseEvidenceV2GateSources(t, []string{"final_semantic_prior_carrier_decode_v2_test.go", "final_semantic_prior_carrier_canonical_v2_test.go"})
	names, err := releaseSelectedTestDeclarations("^TestFinalCaptureCapacityPriorCarrier(Canonical|Decode)V2", sources)
	if err != nil {
		t.Fatal(err)
	}
	typedSelector, err := releaseConnectPolicySelectorAssignment(script, "capture_typed_prior_tests")
	if err != nil {
		t.Fatal(err)
	}
	separate := regexp.MustCompile(typedSelector)
	ordinarySkip := regexp.MustCompile(strings.TrimSuffix(strings.TrimPrefix(releaseGateCaptureOwnerSkip, " -skip '"), "'"))
	for _, name := range names {
		wantSeparate := name == releaseGateCaptureTypedPriorRoot
		if separate.MatchString(name) != wantSeparate || ordinarySkip.MatchString(name) != wantSeparate {
			t.Fatalf("typed prior partition changed adjacent codec ownership for %s", name)
		}
	}
	for _, name := range []string{
		releaseGateCaptureTypedPriorRoot,
		"TestFinalCaptureCapacityPriorCarrierDecodeV2KeepsBoundedVerificationAllocation",
		"TestFinalCaptureCapacityPriorCarrierCanonicalV2KeepsTypedSourceBoundary",
	} {
		if !slices.Contains(names, name) {
			t.Fatalf("typed prior partition lost its adjacent source %s", name)
		}
	}
}

// Recreate the old combined aggregate and adjacent ways an exclusion could
// become an omitted root, broadened deadline, hidden failure or unowned job.
func TestProducerGateCaptureSelectionRejectsMetadataPartitionDrift(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if err := verifyReleaseGateCaptureMetadataIsolation(script); err != nil {
		t.Fatal(err)
	}
	const skip = releaseGateCaptureOwnerSkip
	if err := verifyReleaseGateCaptureMetadataIsolation(strings.ReplaceAll(script, skip, "")); err == nil {
		t.Fatal("capture accepted the old combined serial stress owner")
	}
	for _, pair := range []struct {
		old     string
		changed string
	}{
		{old: skip, changed: " -skip '^TestCampaignEvidence'"},
		{old: skip, changed: skip + " -skip '^Test'"},
		{old: "capture_metadata_tests='^TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier$'", changed: "capture_metadata_tests='^TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier'"},
		{old: "release_gate_start capture-metadata release_phase_capture_metadata", changed: "# release_gate_start capture-metadata release_phase_capture_metadata"},
		{old: "release_gate_start capture-metadata release_phase_capture_metadata", changed: "release_gate_start capture-metadata release_phase_capture"},
		{old: "release_gate_start capture-metadata release_phase_capture_metadata", changed: "release_gate_start capture-metadata release_phase_capture_metadata\nrelease_gate_start capture-metadata release_phase_capture_metadata"},
		{old: "release_gate_start capture-metadata release_phase_capture_metadata", changed: "release_phase_unused() {\nrelease_gate_start capture-metadata release_phase_capture_metadata\n}"},
		{old: "release_gate_start capture-metadata release_phase_capture_metadata", changed: "if false; then\nrelease_gate_start capture-metadata release_phase_capture_metadata\nfi"},
		{old: "release_gate_start capture-metadata release_phase_capture_metadata", changed: "for omitted in; do\nrelease_gate_start capture-metadata release_phase_capture_metadata\ndone"},
		{old: "release_gate_start capture-metadata release_phase_capture_metadata", changed: "{\nrelease_gate_start capture-metadata release_phase_capture_metadata\n}"},
	} {
		if !strings.Contains(script, pair.old) {
			t.Fatal("mutation lost its actual source prerequisite", pair.old)
		}
		if err := verifyReleaseGateCaptureMetadataIsolation(strings.Replace(script, pair.old, pair.changed, 1)); err == nil {
			t.Fatal("capture accepted altered metadata partition", pair.changed)
		}
	}
	for _, variable := range []string{"capture_tests", "capture_evidence_tests", "capture_renewal_tests", "capture_revision_tests", "capture_private_tests", "capture_prior_tests", "capture_typed_prior_tests", "capture_lifecycle_tests", "capture_population_tests", "capture_metadata_tests"} {
		for _, race := range []bool{false, true} {
			command := "go test"
			timeout := "5m"
			if race {
				command += " -race"
				timeout = "10m"
				if variable == "capture_metadata_tests" {
					timeout = "45m"
				}
			}
			command += ` ./sim-testnet -run "$` + variable + `" -count=1`
			if variable == "capture_tests" {
				command += skip
			} else if variable == "capture_evidence_tests" {
				command += releaseGateCaptureEvidenceSkip
			}
			command += " -timeout " + timeout
			for _, replacement := range []string{
				"# " + command, command + " || true", command + " -run '^$'",
				strings.Replace(command, "-timeout "+timeout, "-timeout 0", 1),
				strings.Replace(command, "-count=1", "-count=0", 1),
				strings.Replace(command, "./sim-testnet", "./protocol", 1),
				"if false; then\n  " + command + "\n  fi",
			} {
				if strings.Count(script, command) != 1 {
					t.Fatal("mutation does not identify one original command", command)
				}
				if err := verifyReleaseGateCaptureMetadataIsolation(strings.Replace(script, command, replacement, 1)); err == nil {
					t.Fatal("capture accepted altered metadata execution", replacement)
				}
			}
		}
	}
}

// The actual complete metadata command receives only its measured race owner.
// Restoring the failed ten-minute allowance must fail before any gate body.
func TestProducerGateCaptureSelectionPinsMeasuredMetadataRaceBudget(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if err := verifyReleaseGateCaptureMetadataIsolation(script); err != nil {
		t.Fatalf("full metadata race lacks its measured 45-minute owner: %v", err)
	}
	const command = `go test -race ./sim-testnet -run "$capture_metadata_tests" -count=1 -timeout 45m`
	if strings.Count(script, command) != 1 {
		t.Fatal("full metadata race command is not uniquely executable")
	}
	old := strings.Replace(script, command, strings.Replace(command, "45m", "10m", 1), 1)
	if err := verifyReleaseGateCaptureMetadataIsolation(old); err == nil {
		t.Fatal("full metadata race accepted the previously failed ten-minute owner")
	}
}

// A larger full-metadata race owner must not leak to normal, ordinary capture,
// selector breadth, ignored failures, or the separately admitted full packages.
func TestProducerGateCaptureSelectionRejectsMetadataRaceBudgetLeak(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if err := verifyReleaseGateCaptureMetadataIsolation(script); err != nil {
		t.Fatal(err)
	}
	const metadataRace = `go test -race ./sim-testnet -run "$capture_metadata_tests" -count=1 -timeout 45m`
	const metadataNormal = `go test ./sim-testnet -run "$capture_metadata_tests" -count=1 -timeout 5m`
	const captureNormal = `go test ./sim-testnet -run "$capture_tests" -count=1` + releaseGateCaptureOwnerSkip + ` -timeout 5m`
	const captureRace = `go test -race ./sim-testnet -run "$capture_tests" -count=1` + releaseGateCaptureOwnerSkip + ` -timeout 10m`
	for _, change := range []struct {
		name        string
		original    string
		replacement string
	}{
		{name: "metadata implicit timeout", original: metadataRace, replacement: strings.Replace(metadataRace, " -timeout 45m", "", 1)},
		{name: "metadata unbounded timeout", original: metadataRace, replacement: strings.Replace(metadataRace, "45m", "0", 1)},
		{name: "metadata diagnostic timeout", original: metadataRace, replacement: strings.Replace(metadataRace, "45m", "90m", 1)},
		{name: "metadata normal budget leak", original: metadataNormal, replacement: strings.Replace(metadataNormal, "5m", "45m", 1)},
		{name: "ordinary normal budget leak", original: captureNormal, replacement: strings.Replace(captureNormal, "5m", "45m", 1)},
		{name: "ordinary race budget leak", original: captureRace, replacement: strings.Replace(captureRace, "10m", "45m", 1)},
		{name: "broader metadata selector", original: metadataRace, replacement: strings.Replace(metadataRace, `"$capture_metadata_tests"`, "'^TestCampaignEvidence'", 1)},
		{name: "hidden metadata failure", original: metadataRace, replacement: metadataRace + " || true"},
	} {
		if strings.Count(script, change.original) != 1 {
			t.Fatalf("%s lost its unique command", change.name)
		}
		mutated := strings.Replace(script, change.original, change.replacement, 1)
		if err := verifyReleaseGateCaptureMetadataIsolation(mutated); err == nil {
			t.Fatalf("%s escaped the exact phase budget", change.name)
		}
	}
	localRaw, err := os.ReadFile("../scripts/test-release-1.0-local.sh")
	if err != nil {
		t.Fatal(err)
	}
	localScript := string(localRaw)
	if err := verifyReleaseGateFullValidatorRace(localScript); err != nil {
		t.Fatalf("metadata budget changed an independent full-package owner: %v", err)
	}
	for _, fullRace := range []string{releaseGateSimulatorOrdinaryRaceCommand, releaseGateSimulatorPopulationRaceCommand, releaseGateSimulatorSupplementRaceCommand} {
		if strings.Count(localScript, fullRace) != 1 {
			t.Fatal("full simulator race owner is not uniquely executable")
		}
		changedFull := strings.Replace(localScript, fullRace, strings.Replace(fullRace, "90m", "45m", 1), 1)
		if err := verifyReleaseGateFullValidatorRace(changedFull); err == nil {
			t.Fatal("metadata race budget replaced an independent full-simulator allowance")
		}
	}
}

// Every selected declaration has one owner; open source families may grow,
// while finite cohorts, exact exclusions and execution budgets stay pinned.
func verifyReleaseGateCaptureSourceCensus(script string, sources []string) error {
	if err := verifyReleaseGateCaptureMetadataIsolation(script); err != nil {
		return fmt.Errorf("capture population lacks its independently admitted five/ten-minute owner: %w", err)
	}
	selector, err := releaseConnectPolicySelectorAssignment(script, "capture_tests")
	if err != nil {
		return err
	}
	selected, err := releaseSelectedTestDeclarations(selector, sources)
	if err != nil {
		return err
	}
	evidenceRoots, err := releaseSelectedTestDeclarations("^"+releaseGateCaptureEvidencePattern+"$", sources)
	if err != nil {
		return err
	}
	selectors := map[string]*regexp.Regexp{}
	for _, variable := range []string{"capture_population_tests", "capture_metadata_tests", "capture_private_tests", "capture_prior_tests", "capture_typed_prior_tests", "capture_lifecycle_tests", "capture_evidence_tests", "capture_renewal_tests", "capture_revision_tests"} {
		value, err := releaseConnectPolicySelectorAssignment(script, variable)
		if err != nil {
			return err
		}
		selectors[variable] = regexp.MustCompile(value)
	}
	skip := regexp.MustCompile(strings.TrimSuffix(strings.TrimPrefix(releaseGateCaptureOwnerSkip, " -skip '"), "'"))
	evidenceSkip := regexp.MustCompile(strings.TrimSuffix(strings.TrimPrefix(releaseGateCaptureEvidenceSkip, " -skip '"), "'"))
	ordinaryOwners := map[string]bool{}
	counts := map[string]int{}
	for _, name := range selected {
		owners := 0
		if !skip.MatchString(name) {
			ordinaryOwners[name] = true
			owners++
		}
		for variable, selector := range selectors {
			if selector.MatchString(name) && !(variable == "capture_evidence_tests" && evidenceSkip.MatchString(name)) {
				counts[variable]++
				owners++
			}
		}
		if owners != 1 {
			return fmt.Errorf("capture source %s has %d execution owners, want exactly one", name, owners)
		}
	}
	separateOwners := 0
	for _, count := range counts {
		separateOwners += count
	}
	// The two stress roots have their own owners; every other evidence root
	// remains in the evidence family, including newly added regressions.
	if counts["capture_population_tests"] != 1 || counts["capture_metadata_tests"] != 1 || counts["capture_private_tests"] != len(releaseCapturePrivateFixtureRoots) || counts["capture_prior_tests"] != 1 || counts["capture_typed_prior_tests"] != 1 || counts["capture_lifecycle_tests"] != 1 || counts["capture_evidence_tests"] != len(evidenceRoots)-2 || counts["capture_renewal_tests"] != 11 || counts["capture_revision_tests"] != 2 || len(ordinaryOwners) == 0 || len(ordinaryOwners)+separateOwners != len(selected) {
		return fmt.Errorf("capture partition changed its complete source census: ordinary=%d separate=%v selected=%d", len(ordinaryOwners), counts, len(selected))
	}
	for _, root := range releaseCapturePrivateFixtureRoots {
		if !slices.Contains(selected, root.name) || !selectors["capture_private_tests"].MatchString(root.name) {
			return fmt.Errorf("capture private source %s lost its separate owner", root.name)
		}
	}
	if !slices.Contains(selected, releaseGateCapturePriorRoot) {
		return fmt.Errorf("capture prior source lost its separate owner")
	}
	if !slices.Contains(selected, releaseGateCaptureTypedPriorRoot) {
		return fmt.Errorf("capture typed prior source lost its separate owner")
	}
	if !slices.Contains(selected, releaseGateCaptureLifecycleRoot) {
		return fmt.Errorf("capture lifecycle source lost its separate owner")
	}
	for _, name := range []string{
		"TestCampaignEvidencePopulationV2AdmitsFullConfiguredMetadataCensus",
		"TestCampaignEvidenceCapacityV2MetadataCompletionWriteBindsExactSignedObject",
	} {
		if !slices.Contains(selected, name) || !selectors["capture_evidence_tests"].MatchString(name) || evidenceSkip.MatchString(name) {
			return fmt.Errorf("exact stress partition removed adjacent evidence source %s", name)
		}
	}
	for _, name := range []string{
		"TestFinalCaptureV2PrivateFixtureInputsAreDetached",
		"TestFleetRenewalOriginalOracleAcceptsCompletedRestore",
		"TestFleetRenewalOriginalOracleRejectsChangedRouting",
	} {
		if !ordinaryOwners[name] {
			return fmt.Errorf("capture source %s lost its ordinary owner", name)
		}
	}
	return nil
}

// Keep the complete current source census in exactly one owner per mode.
func TestProducerGateCaptureSelectionRequiresIndependentPopulation(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseGateCaptureSourceCensus(string(raw), releaseEvidenceV2GateSources(t, []string{"*_test.go"})); err != nil {
		t.Fatal(err)
	}
}

// Adding renewal regressions must not invalidate an unchanged owner partition.
func TestProducerGateCaptureSelectionAdmitsAdditionalOrdinarySources(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	sources := releaseEvidenceV2GateSources(t, []string{"*_test.go"})
	sources = append(sources, "func TestFleetRenewalSyntheticCaptureFirst(t *testing.T) {}\nfunc TestFleetRenewalSyntheticCaptureSecond(t *testing.T) {}\n")
	if err := verifyReleaseGateCaptureSourceCensus(string(raw), sources); err != nil {
		t.Fatalf("additional ordinary roots broke complete capture ownership: %v", err)
	}
}

// The evidence family's open prefix must also admit future source roots.
func TestProducerGateCaptureSelectionAdmitsAdditionalEvidenceSources(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	sources := releaseEvidenceV2GateSources(t, []string{"*_test.go"})
	sources = append(sources, "func TestCampaignEvidenceSyntheticCapture(t *testing.T) {}\n")
	if err := verifyReleaseGateCaptureSourceCensus(string(raw), sources); err != nil {
		t.Fatalf("additional evidence root broke complete capture ownership: %v", err)
	}
}

// Dynamic totals cannot hide a narrowed family, absent retained root or duplicate.
func TestProducerGateCaptureSelectionRejectsSourceCensusDrift(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	sources := releaseEvidenceV2GateSources(t, []string{"*_test.go"})
	if err := verifyReleaseGateCaptureSourceCensus(script, sources); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"TestFleetRenewalOriginalOracleAcceptsCompletedRestore",
		"TestFleetRenewalOriginalOracleRejectsChangedRouting",
		releaseGateCapturePopulationRoot,
		releaseGateCaptureTypedPriorRoot,
		"TestFleetRenewalBudgetDoesNotChargeRetiredGasTwice",
	} {
		missingSources := slices.Clone(sources)
		for i, source := range missingSources {
			missingSources[i] = strings.ReplaceAll(source, "func "+name+"(", "func omitted"+name+"(")
		}
		if err := verifyReleaseGateCaptureSourceCensus(script, missingSources); err == nil {
			t.Fatalf("missing retained source %s escaped complete ownership", name)
		}
		duplicateSources := append(slices.Clone(sources), "func "+name+"(t *testing.T) {}\n")
		if err := verifyReleaseGateCaptureSourceCensus(script, duplicateSources); err == nil || !strings.Contains(err.Error(), "duplicated") {
			t.Fatalf("duplicate source %s escaped exact declaration membership: %v", name, err)
		}
	}
	for _, family := range []string{"FleetRenewal", "CampaignEvidence", "FinalArchive"} {
		changed := strings.Replace(script, "capture_tests='"+releaseGateCaptureSelector+"'", "capture_tests='"+strings.Replace(releaseGateCaptureSelector, family+"|", "", 1)+"'", 1)
		if changed == script {
			t.Fatalf("capture family %s lost its mutation prerequisite", family)
		}
		if err := verifyReleaseGateCaptureSourceCensus(changed, sources); err == nil {
			t.Fatalf("omitted capture family %s escaped source census", family)
		}
	}
}

// Restoring the combined owner, hiding its successor, or broadening the skip
// must be rejected without rerunning the known failed five-minute package.
func TestProducerGateCaptureSelectionRejectsPopulationPartitionDrift(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if err := verifyReleaseGateCaptureMetadataIsolation(script); err != nil {
		t.Fatal(err)
	}
	const start = "release_gate_start capture-population release_phase_capture_population"
	const assignment = "capture_population_tests='^" + releaseGateCapturePopulationRoot + "$'"
	const normal = `go test ./sim-testnet -run "$capture_population_tests" -count=1 -timeout 5m`
	const race = `go test -race ./sim-testnet -run "$capture_population_tests" -count=1 -timeout 10m`
	const oldCombinedSkip = " -skip '^(TestCampaignEvidence(CapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier|PopulationV2StreamsPhaseCensusWithBoundedOwners)|TestFinalCaptureV2(ReadsActualRenderedSetupAndRejectsChangedSource|PendingPriorClosesOriginalAuthority|PendingPriorRejectsRehashedSourceAndMissingCensus|PendingPriorRejectsWrongHandoffAndSemanticRelabel|PendingJobIsImmutableAndNeverAccepted|PendingPriorRejectsWrongGateBeforeWrites|PendingPriorArtifactCensusHasNoSemanticOutputs)|TestVerifyFinalCollectedPriorPhaseBytesRejectsReopenedHandoffSubstitution|TestFleetLifecycleRenewalDescriptorsKeepLaterWaves)$'"
	for _, change := range []struct {
		name        string
		original    string
		replacement string
	}{
		{name: "old combined evidence and renewal prefix", original: releaseGateCaptureOwnerSkip, replacement: oldCombinedSkip},
		{name: "typed prior shares ordinary budget", original: releaseGateCaptureOwnerSkip, replacement: strings.Replace(releaseGateCaptureOwnerSkip, "|"+releaseGateCaptureTypedPriorRoot, "", 1)},
		{name: "old combined population", original: releaseGateCaptureOwnerSkip, replacement: " -skip '^TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier$'"},
		{name: "old combined private fixtures", original: releaseGateCaptureOwnerSkip, replacement: " -skip '^TestCampaignEvidence(CapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier|PopulationV2StreamsPhaseCensusWithBoundedOwners)$'"},
		{name: "broader ordinary omission", original: releaseGateCaptureOwnerSkip, replacement: " -skip '^TestCampaignEvidence'"},
		{name: "missing population registration", original: start, replacement: "# " + start},
		{name: "wrong population function", original: start, replacement: "release_gate_start capture-population release_phase_capture"},
		{name: "duplicate population registration", original: start, replacement: start + "\n" + start},
		{name: "hidden population function", original: start, replacement: "release_phase_unused() {\n" + start + "\n}"},
		{name: "conditional population registration", original: start, replacement: "if false; then\n" + start + "\nfi"},
		{name: "looped population registration", original: start, replacement: "for omitted in; do\n" + start + "\ndone"},
		{name: "subshell population registration", original: start, replacement: "(\n" + start + "\n)"},
		{name: "broader population selector", original: assignment, replacement: "capture_population_tests='^TestCampaignEvidencePopulationV2'"},
		{name: "foreign population selector", original: assignment, replacement: "capture_population_tests='^TestCampaignEvidencePopulationV2AdmitsFullConfiguredMetadataCensus$'"},
		{name: "duplicate metadata execution", original: assignment, replacement: "capture_population_tests='^TestCampaignEvidence(CapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier|PopulationV2StreamsPhaseCensusWithBoundedOwners)$'"},
		{name: "hidden normal failure", original: normal, replacement: normal + " || true"},
		{name: "missing race execution", original: race, replacement: "# " + race},
		{name: "normal deadline expansion", original: normal, replacement: strings.Replace(normal, "5m", "10m", 1)},
		{name: "race deadline expansion", original: race, replacement: strings.Replace(race, "10m", "45m", 1)},
	} {
		if !strings.Contains(script, change.original) {
			t.Fatalf("%s lost its original source prerequisite", change.name)
		}
		mutated := strings.Replace(script, change.original, change.replacement, 1)
		if err := verifyReleaseGateCaptureMetadataIsolation(mutated); err == nil {
			t.Fatalf("%s escaped exact population ownership", change.name)
		}
	}
	for _, owner := range []struct {
		phase    string
		job      string
		variable string
		selector string
	}{
		{phase: "capture_private", job: "capture-private", variable: "capture_private_tests", selector: "^" + releaseGateCapturePrivatePattern + "$"},
		{phase: "capture_prior", job: "capture-prior", variable: "capture_prior_tests", selector: "^" + releaseGateCapturePriorRoot + "$"},
		{phase: "capture_typed_prior", job: "capture-typed-prior", variable: "capture_typed_prior_tests", selector: "^" + releaseGateCaptureTypedPriorRoot + "$"},
		{phase: "capture_lifecycle", job: "capture-lifecycle", variable: "capture_lifecycle_tests", selector: "^" + releaseGateCaptureLifecycleRoot + "$"},
		{phase: "capture_evidence", job: "capture-evidence", variable: "capture_evidence_tests", selector: "^" + releaseGateCaptureEvidencePattern + "$"},
		{phase: "capture_renewal", job: "capture-renewal", variable: "capture_renewal_tests", selector: "^" + releaseGateCaptureRenewalPattern + "$"},
		{phase: "capture_revision", job: "capture-revision", variable: "capture_revision_tests", selector: "^" + releaseGateCaptureRevisionPattern + "$"},
	} {
		start := "release_gate_start " + owner.job + " release_phase_" + owner.phase
		assignment := owner.variable + "='" + owner.selector + "'"
		skip := ""
		if owner.variable == "capture_evidence_tests" {
			skip = releaseGateCaptureEvidenceSkip
		}
		normal := `go test ./sim-testnet -run "$` + owner.variable + `" -count=1` + skip + " -timeout 5m"
		race := `go test -race ./sim-testnet -run "$` + owner.variable + `" -count=1` + skip + " -timeout 10m"
		for _, change := range []struct {
			original    string
			replacement string
		}{
			{original: normal, replacement: normal + " || true"},
			{original: race, replacement: "# " + race},
			{original: normal, replacement: strings.Replace(normal, "5m", "10m", 1)},
			{original: race, replacement: strings.Replace(race, "10m", "15m", 1)},
			{original: race, replacement: strings.Replace(race, "-count=1", "-count=2", 1)},
			{original: start, replacement: "# " + start},
			{original: start, replacement: start + "\n" + start},
			{original: start, replacement: "if false; then\n" + start + "\nfi"},
			{original: start, replacement: "release_phase_unused() {\n" + start + "\n}"},
			{original: assignment, replacement: owner.variable + "='^TestFinalCaptureV2'"},
			{original: assignment, replacement: owner.variable + "='^" + releaseGateCapturePopulationRoot + "$'"},
		} {
			if strings.Count(script, change.original) != 1 {
				t.Fatal("private capture mutation lost its unique source", change.original)
			}
			if err := verifyReleaseGateCaptureMetadataIsolation(strings.Replace(script, change.original, change.replacement, 1)); err == nil {
				t.Fatal("capture accepted altered private fixture ownership", change.replacement)
			}
		}
	}
}

// These seven private filesystem/authority fixtures share their exact job's
// parallel admission. Allocation measurements and both stress owners stay serial.
var releaseCapturePrivateFixtureRoots = []struct {
	path    string
	name    string
	fixture string
}{
	{path: "final_semantic_capture_v2_test.go", name: "TestFinalCaptureV2ReadsActualRenderedSetupAndRejectsChangedSource", fixture: "newRuntimeEvidenceProvisionV2TestFixture"},
	{path: "final_semantic_pending_prior_v2_test.go", name: "TestFinalCaptureV2PendingPriorClosesOriginalAuthority", fixture: "newFinalPendingPriorV2TestFixture"},
	{path: "final_semantic_pending_prior_v2_test.go", name: "TestFinalCaptureV2PendingPriorRejectsRehashedSourceAndMissingCensus", fixture: "newFinalPendingPriorV2TestFixture"},
	{path: "final_semantic_pending_prior_v2_test.go", name: "TestFinalCaptureV2PendingPriorRejectsWrongHandoffAndSemanticRelabel", fixture: "newFinalPendingPriorV2TestFixture"},
	{path: "final_semantic_pending_prior_v2_test.go", name: "TestFinalCaptureV2PendingJobIsImmutableAndNeverAccepted", fixture: "newFinalPendingPriorV2TestFixture"},
	{path: "final_semantic_pending_prior_v2_test.go", name: "TestFinalCaptureV2PendingPriorRejectsWrongGateBeforeWrites", fixture: "newFinalPendingPriorV2TestFixture"},
	{path: "final_semantic_pending_prior_v2_test.go", name: "TestFinalCaptureV2PendingPriorArtifactCensusHasNoSemanticOutputs", fixture: "newFinalPendingPriorV2TestFixture"},
}

// Provisioning roots share the scheduling guard without entering the seven-root
// capture selector. Clone the selector before adding these independent owners.
var releaseCapturePrivateSchedulingRoots = append(slices.Clone(releaseCapturePrivateFixtureRoots), []struct {
	path    string
	name    string
	fixture string
}{
	{path: "runtime_evidence_provision_v2_test.go", name: "TestRuntimeEvidenceProvisionV2FixedPlanBoundsAllFourKeeperActions", fixture: "newRuntimeEvidenceProvisionV2TestFixture"},
	{path: "runtime_evidence_provision_v2_test.go", name: "TestRuntimeEvidenceProvisionV2HeaderCapacityMatchesPublicReader", fixture: "newRuntimeEvidenceProvisionV2TestFixture"},
	{path: "runtime_evidence_provision_v2_test.go", name: "TestRuntimeEvidenceProvisionV2RetainsActualFilesAndResolvesSameTemplate", fixture: "newRuntimeEvidenceProvisionV2TestFixture"},
	{path: "runtime_evidence_provision_v2_test.go", name: "TestRuntimeEvidenceProvisionV2InterruptedLastSourceResumesExactConsents", fixture: "newRuntimeEvidenceProvisionV2TestFixture"},
	{path: "runtime_evidence_provision_v2_test.go", name: "TestRuntimeEvidenceProvisionV2ChangedInputCannotBeRehashedIntoAuthority", fixture: "newRuntimeEvidenceProvisionV2TestFixture"},
	{path: "runtime_evidence_provision_v2_test.go", name: "TestRuntimeEvidenceProvisionV2SignedForeignSourceIsRefusedBeforeFiles", fixture: "newRuntimeEvidenceProvisionV2TestFixture"},
	{path: "runtime_evidence_provision_v2_test.go", name: "TestRuntimeEvidenceProvisionV2NoImplicitBudgetOrFabricatedReferences", fixture: "newRuntimeEvidenceProvisionV2TestFixture"},
	{path: "runtime_evidence_provision_v2_test.go", name: "TestRuntimeEvidenceProvisionV2FreshSetupPreservesExistingDiskHistory", fixture: "newRuntimeEvidenceProvisionV2TestFixture"},
}...)

var releaseCaptureSerialOwnerRoots = []struct {
	path string
	name string
}{
	{path: "final_semantic_prior_carrier_decode_v2_test.go", name: "TestFinalCaptureCapacityPriorCarrierDecodeV2KeepsBoundedVerificationAllocation"},
	{path: "evidence_canonical_stream_test.go", name: "TestCampaignEvidenceCanonicalStreamVerificationKeepsAllocationBelowPayload"},
	{path: "evidence_canonical_stream_test.go", name: "TestCampaignEvidenceCanonicalStreamWireComparisonKeepsAllocationBelowPayload"},
	{path: "evidence_encoding_test.go", name: "TestCampaignEvidenceEncodingSigningKeepsAllocationBelowPayload"},
	{path: "evidence_public_file_v2_test.go", name: "TestCampaignEvidenceReadbackV2CanonicalFilesBoundDefaultDecodeAllocation"},
	{path: "evidence_metadata_row_size_v2_test.go", name: "TestCampaignEvidenceCapacityV2MetadataAdmissionKeepsAllocationBelowRows"},
	{path: "evidence_metadata_census_v2_test.go", name: "TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier"},
	{path: "evidence_population_v2_test.go", name: "TestCampaignEvidencePopulationV2StreamsPhaseCensusWithBoundedOwners"},
}

// Read the exact reviewed roots and the real runtime fixture's directory owner.
func releaseCaptureSchedulingSources(t *testing.T) map[string]string {
	t.Helper()
	paths := map[string]bool{"runtime_evidence_provision_v2_test.go": true}
	for _, root := range releaseCapturePrivateSchedulingRoots {
		paths[root.path] = true
	}
	for _, root := range releaseCaptureSerialOwnerRoots {
		paths[root.path] = true
	}
	sources := map[string]string{}
	for path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources[path] = string(raw)
	}
	return sources
}

// Inspect actual root statements and private directory assignments. Moving
// only comments, helpers or an unreachable call cannot repair serial admission.
func verifyReleaseCapturePrivateScheduling(sources map[string]string) error {
	functions := map[string]*ast.FuncDecl{}
	for path, source := range sources {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if err != nil {
			return err
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && function.Body != nil {
				key := path + "/" + function.Name.Name
				if functions[key] != nil {
					return fmt.Errorf("capture fixture function is duplicated: %s", key)
				}
				functions[key] = function
			}
		}
	}
	isTestCall := func(expression ast.Expr, method string) bool {
		call, ok := expression.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return false
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != method {
			return false
		}
		receiver, ok := selector.X.(*ast.Ident)
		return ok && receiver.Name == "t"
	}
	for _, root := range releaseCapturePrivateSchedulingRoots {
		function := functions[root.path+"/"+root.name]
		if function == nil || len(function.Body.List) < 2 {
			return fmt.Errorf("capture private root is missing: %s", root.name)
		}
		first, ok := function.Body.List[0].(*ast.ExprStmt)
		if !ok || !isTestCall(first.X, "Parallel") {
			return fmt.Errorf("capture private fixture remains serial: %s", root.name)
		}
		fixture, ok := function.Body.List[1].(*ast.AssignStmt)
		if !ok || len(fixture.Rhs) != 1 {
			return fmt.Errorf("capture root lost its original private fixture: %s", root.name)
		}
		call, ok := fixture.Rhs[0].(*ast.CallExpr)
		if !ok {
			return fmt.Errorf("capture root bypassed its real fixture: %s", root.name)
		}
		factory, ok := call.Fun.(*ast.Ident)
		if !ok || factory.Name != root.fixture {
			return fmt.Errorf("capture root substituted its original fixture: %s", root.name)
		}
	}
	for _, root := range releaseCaptureSerialOwnerRoots {
		function := functions[root.path+"/"+root.name]
		if function == nil {
			return fmt.Errorf("capture serial owner is missing: %s", root.name)
		}
		parallel := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && isTestCall(call, "Parallel") {
				parallel = true
			}
			return true
		})
		if parallel {
			return fmt.Errorf("capture process-wide or stress owner became parallel: %s", root.name)
		}
	}
	for _, owner := range []struct {
		path, name, variable string
		joined               bool
	}{
		{path: "final_semantic_pending_prior_v2_test.go", name: "newFinalPendingPriorV2TestFixture", variable: "stateRoot"},
		{path: "runtime_evidence_provision_v2_test.go", name: "newRuntimeEvidenceProvisionV2ConfiguredTestFixture", variable: "stateDir", joined: true},
	} {
		function := functions[owner.path+"/"+owner.name]
		if function == nil {
			return fmt.Errorf("capture private factory disappeared: %s", owner.name)
		}
		assignments, private := 0, true
		ast.Inspect(function.Body, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for index, expression := range assignment.Lhs {
				identifier, ok := expression.(*ast.Ident)
				if !ok || identifier.Name != owner.variable {
					continue
				}
				assignments++
				if len(assignment.Rhs) != len(assignment.Lhs) {
					private = false
					continue
				}
				value := assignment.Rhs[index]
				if owner.joined {
					joined, ok := value.(*ast.CallExpr)
					if !ok || len(joined.Args) != 2 {
						private = false
						continue
					}
					selector, ok := joined.Fun.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "Join" {
						private = false
						continue
					}
					receiver, ok := selector.X.(*ast.Ident)
					if !ok || receiver.Name != "filepath" {
						private = false
						continue
					}
					leaf, ok := joined.Args[1].(*ast.BasicLit)
					if !ok || leaf.Kind != token.STRING || leaf.Value != "\"state\"" {
						private = false
						continue
					}
					value = joined.Args[0]
				}
				private = private && isTestCall(value, "TempDir")
			}
			return true
		})
		if assignments != 1 || !private {
			return fmt.Errorf("capture fixture lost its private directory owner: %s", owner.name)
		}
	}
	return nil
}

// Every heavy fixture begins with parallel admission; process-wide allocation
// controls remain serial and the actual directory factories stay private.
func TestProducerGateCaptureSelectionRequiresPrivateFixtureScheduling(t *testing.T) {
	t.Parallel()
	if err := verifyReleaseCapturePrivateScheduling(releaseCaptureSchedulingSources(t)); err != nil {
		t.Fatal(err)
	}
}

// The old first-statement omission, hidden calls, lost fixtures and shared
// directories fail deterministically without a timing-based assertion.
func TestProducerGateCaptureSelectionRejectsPrivateFixtureSchedulingDrift(t *testing.T) {
	t.Parallel()
	sources := releaseCaptureSchedulingSources(t)
	if err := verifyReleaseCapturePrivateScheduling(sources); err != nil {
		t.Fatal(err)
	}
	check := func(path, old, replacement string) {
		t.Helper()
		original := sources[path]
		if strings.Count(original, old) != 1 {
			t.Fatal("scheduling mutation lacks one original source", path, old)
		}
		sources[path] = strings.Replace(original, old, replacement, 1)
		err := verifyReleaseCapturePrivateScheduling(sources)
		sources[path] = original
		if err == nil {
			t.Fatal("capture scheduling accepted altered ownership", path, replacement)
		}
	}
	for _, root := range releaseCapturePrivateSchedulingRoots {
		declaration := "func " + root.name + "(t *testing.T) {\n"
		original := declaration + "\tt.Parallel()\n"
		check(root.path, original, declaration)
		check(root.path, original, declaration+"\tif false { t.Parallel() }\n")
		check(root.path, original, declaration+"\tt.Helper()\n\tt.Parallel()\n")
		check(root.path, original+"\tfixture := "+root.fixture+"(t)", original+"\tfixture := missingPrivateFixture(t)")
	}
	for _, root := range releaseCaptureSerialOwnerRoots {
		declaration := "func " + root.name + "(t *testing.T) {\n"
		check(root.path, declaration, declaration+"\tt.Parallel()\n")
	}
	check("final_semantic_pending_prior_v2_test.go", "stateRoot := t.TempDir()", "stateRoot := \"shared-capture-root\"")
	check("runtime_evidence_provision_v2_test.go", "stateDir := filepath.Join(t.TempDir(), \"state\")", "stateDir := filepath.Join(\"shared-capture-root\", \"state\")")
}
