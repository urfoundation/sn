package main

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const releaseGateSimulatorEvidenceSelector = "^Test(ValidatorEvidence|RuntimeEvidenceV2|RuntimeEvidence|EvidenceRelay|EvmTxManager|ClientKeyHistory)"
const releaseGateSimulatorEvidenceSlowSelector = "^(TestRuntimeEvidenceLaunchV2TemplateReachesGeneratedSetupAndRender|TestValidatorEvidenceCarryPublicOverrideRequiresCompleteClonedHeads)$"
const releaseGateSimulatorEvidenceOwnerSkip = " -skip '" + releaseGateSimulatorEvidenceSlowSelector + "'"

// The original prefix census is independent of the new owners. Parse real
// top-level declarations; fixture strings and another platform's files cannot
// invent roots that the package would not compile on this gate's host.
func releaseGateSimulatorEvidenceInventory(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var roots []string
	for _, path := range paths {
		matched, err := build.Default.MatchFile(".", path)
		if err != nil {
			t.Fatal(err)
		}
		if !matched {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, "Test") {
				roots = append(roots, function.Name.Name)
			}
		}
	}
	slices.Sort(roots)
	return roots
}

// Every original root has exactly one owner in each mode. Exact commands and
// unconditional admissions also keep the existing Forge/generator dependency,
// contract package coverage and ten-minute simulator package bounds intact.
func verifyReleaseGateSimulatorEvidencePartition(script string, roots []string) error {
	original := regexp.MustCompile(releaseGateSimulatorEvidenceSelector)
	slow := regexp.MustCompile(releaseGateSimulatorEvidenceSlowSelector)
	owners := map[string]int{}
	for _, group := range []struct {
		phase, job, variable, selector, packages, skip string
	}{
		{"solidity", "solidity", "validator_evidence_tests", releaseGateSimulatorEvidenceSelector, "./protocol ./stabi ./sim-testnet/gencontracts", ""},
		{"evidence_simulator", "evidence-simulator", "simulator_evidence_tests", releaseGateSimulatorEvidenceSelector, "./sim-testnet", releaseGateSimulatorEvidenceOwnerSkip},
		{"evidence_simulator_slow", "evidence-simulator-slow", "simulator_evidence_slow_tests", releaseGateSimulatorEvidenceSlowSelector, "./sim-testnet", ""},
	} {
		function := "release_phase_" + group.phase
		pattern := regexp.MustCompile("(?ms)^" + function + "\\(\\) \\{\\n(.*?)^\\}[\\t ]*$")
		phases := pattern.FindAllStringSubmatch(script, -1)
		if len(phases) != 1 {
			return fmt.Errorf("simulator evidence needs one %s definition", function)
		}
		selector, err := releaseConnectPolicySelectorAssignment(script, group.variable)
		if err != nil || selector != group.selector {
			return fmt.Errorf("simulator evidence changed %s selection: %v", group.phase, err)
		}
		var commands []string
		for _, line := range strings.Split(phases[0][1], "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				commands = append(commands, line)
			}
		}
		expected := []string{`cd "$sn_repo"`}
		if group.phase == "solidity" {
			expected = []string{
				`cd "$sn_repo/evm"`, "forge fmt --check", "forge build --deny warnings --sizes", "forge test --summary",
				`cd "$sn_repo"`, `go run ./sim-testnet/gencontracts --check "$FOUNDRY_OUT" sim-testnet/contracts_gen.go`,
				`./stabi/generate.sh --check --artifacts "$FOUNDRY_OUT"`,
			}
		}
		expected = append(expected, group.variable+"='"+group.selector+"'")
		for _, mode := range []string{"", "-race "} {
			command := "go test " + mode + group.packages + ` -run "$` + group.variable + `" -count=1` + group.skip
			if group.phase != "solidity" {
				command += " -timeout 10m"
			}
			expected = append(expected, command)
			invocations := regexp.MustCompile("(?m)^[\\t ]*" + regexp.QuoteMeta(command) + "[\\t ]*$")
			if len(invocations.FindAllString(script, -1)) != 1 {
				return fmt.Errorf("simulator evidence mode has no unique command: %s", command)
			}
		}
		if !slices.Equal(commands, expected) {
			return fmt.Errorf("simulator evidence changed %s commands, modes or bounds", group.phase)
		}
		start := "release_gate_start " + group.job + " " + function
		conditions, err := releaseGateRegistrationConditions(script, start)
		invocation := regexp.MustCompile("(?m)^" + regexp.QuoteMeta(start) + "[\\t ]*$")
		calls := invocation.FindAllStringIndex(script, -1)
		definition := pattern.FindStringIndex(script)
		if err != nil || len(conditions) != 0 || len(calls) != 1 || calls[0][0] < definition[1] {
			return fmt.Errorf("simulator evidence lacks independent %s admission: %v", group.phase, err)
		}
		if group.phase == "solidity" {
			continue
		}
		selected := regexp.MustCompile(group.selector)
		count := 0
		for _, root := range roots {
			if selected.MatchString(root) && (group.skip == "" || !slow.MatchString(root)) {
				if !original.MatchString(root) {
					return fmt.Errorf("simulator evidence owner selected foreign root %s", root)
				}
				owners[root]++
				count++
			}
		}
		if count == 0 || group.phase == "evidence_simulator_slow" && count != 2 {
			return fmt.Errorf("simulator evidence %s has an incomplete root census", group.phase)
		}
	}
	for _, root := range roots {
		if original.MatchString(root) && owners[root] != 1 {
			return fmt.Errorf("simulator evidence root %s has %d owners per mode", root, owners[root])
		}
	}
	return nil
}

func TestProducerGateStateSelectionPartitionsSimulatorEvidenceExactly(t *testing.T) {
	t.Parallel()
	script, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	roots := releaseGateSimulatorEvidenceInventory(t)
	if err := verifyReleaseGateSimulatorEvidencePartition(string(script), roots); err != nil {
		t.Fatal(err)
	}
	count := 0
	selected := regexp.MustCompile(releaseGateSimulatorEvidenceSelector)
	for _, root := range roots {
		if selected.MatchString(root) {
			count++
		}
	}
	t.Logf("original simulator census: %d roots; ordinary: %d; slow: 2; one owner per root in normal and race", count, count-2)
	// An adjacent root, including a suffix of a slow root, remains ordinary.
	roots = append(roots, "TestEvidenceRelayFutureBoundary", "TestRuntimeEvidenceLaunchV2TemplateReachesGeneratedSetupAndRenderAdjacent")
	if err := verifyReleaseGateSimulatorEvidencePartition(string(script), roots); err != nil {
		t.Fatal(err)
	}
}

func TestProducerGateStateSelectionRejectsSimulatorEvidencePartitionDrift(t *testing.T) {
	t.Parallel()
	encoded, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(encoded)
	roots := releaseGateSimulatorEvidenceInventory(t)
	if err := verifyReleaseGateSimulatorEvidencePartition(script, roots); err != nil {
		t.Fatal(err)
	}
	const normal = `go test ./sim-testnet -run "$simulator_evidence_tests" -count=1` + releaseGateSimulatorEvidenceOwnerSkip + ` -timeout 10m`
	const race = `go test -race ./sim-testnet -run "$simulator_evidence_slow_tests" -count=1 -timeout 10m`
	const start = "release_gate_start evidence-simulator-slow release_phase_evidence_simulator_slow"
	for _, mutation := range []struct{ old, replacement string }{
		{normal, strings.Replace(normal, releaseGateSimulatorEvidenceOwnerSkip, "", 1)},
		{normal, normal + " -skip '^TestEvidenceRelay'"},
		{normal, normal + "\n  " + normal},
		{race, "# " + race},
		{race, strings.Replace(race, "-race ", "", 1)},
		{race, strings.Replace(race, "10m", "30m", 1)},
		{race, strings.Replace(race, "-count=1", "-count=0", 1)},
		{race, race + " || true"},
		{"simulator_evidence_slow_tests='" + releaseGateSimulatorEvidenceSlowSelector + "'", "simulator_evidence_slow_tests='^TestRuntimeEvidence'"},
		{`go test ./protocol ./stabi ./sim-testnet/gencontracts -run "$validator_evidence_tests" -count=1`, `go test ./protocol ./stabi ./sim-testnet/gencontracts ./sim-testnet -run "$validator_evidence_tests" -count=1`},
		{start, "# " + start},
		{start, start + "\n" + start},
		{start, "if false; then\n" + start + "\nfi"},
		{start, "release_phase_unused() {\n" + start + "\n}"},
	} {
		if strings.Count(script, mutation.old) != 1 {
			t.Fatalf("mutation does not identify one boundary: %s", mutation.old)
		}
		changed := strings.Replace(script, mutation.old, mutation.replacement, 1)
		if err := verifyReleaseGateSimulatorEvidencePartition(changed, roots); err == nil {
			t.Fatalf("changed evidence execution was accepted: %s", mutation.replacement)
		}
	}
}
