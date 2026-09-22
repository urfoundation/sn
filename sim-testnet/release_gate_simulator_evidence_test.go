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

const releaseGateSimulatorEvidenceSelector = "^Test(ValidatorEvidence|RuntimeEvidenceV2|RuntimeEvidence|EvidenceRelay|EvmTxManager|ClientKeyHistory|RuntimeProvisional|ProvisionalRelay|RelayContinuation)"
const releaseGateSimulatorEvidenceSlowSelector = "^(TestRuntimeEvidenceLaunchV2TemplateReachesGeneratedSetupAndRender|TestValidatorEvidenceCarryPublicOverrideRequiresCompleteClonedHeads)$"
const releaseGateSimulatorEvidenceRenderSelector = "^TestRuntimeEvidenceOwnedReservedStagingRerenderReplacesProvisionalConfig$"
const releaseGateSimulatorEvidenceProvisionalRenderSelector = "^TestRuntimeProvisionalStagingRenderBindsRetainedAuthority$"
const releaseGateSimulatorEvidenceProvisionalRejectSelector = "^TestRuntimeProvisionalStagingRejectsChangedSourceAndAuthority$"
const releaseGateSimulatorEvidenceAllRenderSelector = releaseGateSimulatorEvidenceRenderSelector + "|" + releaseGateSimulatorEvidenceProvisionalRenderSelector + "|" + releaseGateSimulatorEvidenceProvisionalRejectSelector
const releaseGateSimulatorEvidenceOwnerSkip = " -skip '^(TestRuntimeEvidenceLaunchV2TemplateReachesGeneratedSetupAndRender|TestValidatorEvidenceCarryPublicOverrideRequiresCompleteClonedHeads|TestRuntimeEvidenceOwnedReservedStagingRerenderReplacesProvisionalConfig|TestRuntimeProvisionalStagingRenderBindsRetainedAuthority|TestRuntimeProvisionalStagingRejectsChangedSourceAndAuthority)$'"

// Follow direct same-package identifier calls through test helpers so a new
// full-launch wrapper cannot rejoin ordinary work. Dynamic calls are outside this classifier.
func releaseGateSimulatorEvidenceRenderRoots(files []*ast.File) []string {
	callsKVs := map[string][]string{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || function.Body == nil {
				continue
			}
			callsKVs[function.Name.Name] = nil
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if name, ok := call.Fun.(*ast.Ident); ok {
					callsKVs[function.Name.Name] = append(callsKVs[function.Name.Name], name.Name)
				}
				return true
			})
		}
	}
	rendersKVs := map[string]bool{"testRuntimeEvidenceLaunchTemplateRender": true}
	for pass := 0; pass < len(callsKVs); pass++ {
		changed := false
		for name, callees := range callsKVs {
			if rendersKVs[name] {
				continue
			}
			for _, callee := range callees {
				if rendersKVs[callee] {
					rendersKVs[name], changed = true, true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
	var roots []string
	for name := range callsKVs {
		if strings.HasPrefix(name, "Test") && rendersKVs[name] {
			roots = append(roots, name)
		}
	}
	slices.Sort(roots)
	return roots
}

// The original prefix census is independent of the new owners. Parse real
// top-level declarations; fixture strings and another platform's files cannot
// invent roots that the package would not compile on this gate's host.
func releaseGateSimulatorEvidenceInventory(t *testing.T) ([]string, []string, []string) {
	t.Helper()
	paths, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var roots []string
	var parallelRoots []string
	var files []*ast.File
	original := regexp.MustCompile(releaseGateSimulatorEvidenceSelector)
	slow := regexp.MustCompile(releaseGateSimulatorEvidenceSlowSelector)
	render := regexp.MustCompile(releaseGateSimulatorEvidenceAllRenderSelector)
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
		files = append(files, file)
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, "Test") {
				roots = append(roots, function.Name.Name)
				if original.MatchString(function.Name.Name) && !slow.MatchString(function.Name.Name) && !render.MatchString(function.Name.Name) && function.Body != nil && len(function.Body.List) > 0 {
					statement, ok := function.Body.List[0].(*ast.ExprStmt)
					if !ok {
						continue
					}
					call, ok := statement.X.(*ast.CallExpr)
					if !ok {
						continue
					}
					selector, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || selector.Sel.Name != "Parallel" {
						continue
					}
					owner, ok := selector.X.(*ast.Ident)
					if ok && owner.Name == "t" && len(call.Args) == 0 {
						parallelRoots = append(parallelRoots, function.Name.Name)
					}
				}
			}
		}
	}
	slices.Sort(roots)
	slices.Sort(parallelRoots)
	return roots, releaseGateSimulatorEvidenceRenderRoots(files), parallelRoots
}

// Every original root has exactly one owner in each mode. Exact commands and
// unconditional admissions also keep the existing Forge/generator dependency,
// contract package coverage and ten-minute simulator package bounds intact.
func verifyReleaseGateSimulatorEvidencePartition(script string, roots, renderRoots, parallelRoots []string) error {
	original := regexp.MustCompile(releaseGateSimulatorEvidenceSelector)
	slow := regexp.MustCompile(releaseGateSimulatorEvidenceSlowSelector)
	render := regexp.MustCompile(releaseGateSimulatorEvidenceAllRenderSelector)
	parallelSelector := "^(" + strings.Join(parallelRoots, "|") + ")$"
	serialSkip := strings.TrimSuffix(strings.TrimPrefix(releaseGateSimulatorEvidenceOwnerSkip, " -skip '"), ")$'") + "|" + strings.Join(parallelRoots, "|") + ")$"
	parallelKVs := map[string]bool{}
	for _, root := range parallelRoots {
		if !original.MatchString(root) || slow.MatchString(root) || render.MatchString(root) || parallelKVs[root] {
			return fmt.Errorf("simulator evidence parallel source census differs: %s", root)
		}
		parallelKVs[root] = true
	}
	if len(parallelRoots) == 0 {
		return fmt.Errorf("simulator evidence parallel source census is empty")
	}
	owners := map[string]int{}
	ordinaryRootsKVs := map[string]bool{}
	for _, group := range []struct {
		phase, job, variable, selector, packages, skip string
		count                                          int
	}{
		{phase: "solidity", job: "solidity", variable: "validator_evidence_tests", selector: releaseGateSimulatorEvidenceSelector, packages: "./protocol ./stabi ./sim-testnet/gencontracts"},
		{phase: "evidence_simulator", job: "evidence-simulator", variable: "simulator_evidence_tests", selector: releaseGateSimulatorEvidenceSelector, packages: "./sim-testnet", skip: ` -skip "$simulator_evidence_serial_skip_tests"`},
		{phase: "evidence_simulator_parallel", job: "evidence-simulator-parallel", variable: "simulator_evidence_parallel_tests", selector: parallelSelector, packages: "./sim-testnet", skip: ""},
		{phase: "evidence_simulator_slow", job: "evidence-simulator-slow", variable: "simulator_evidence_slow_tests", selector: releaseGateSimulatorEvidenceSlowSelector, packages: "./sim-testnet", count: 2},
		{phase: "evidence_simulator_render", job: "evidence-simulator-render", variable: "simulator_evidence_render_tests", selector: releaseGateSimulatorEvidenceRenderSelector, packages: "./sim-testnet", count: 1},
		{phase: "evidence_simulator_provisional_render", job: "evidence-simulator-provisional-render", variable: "simulator_evidence_provisional_render_tests", selector: releaseGateSimulatorEvidenceProvisionalRenderSelector, packages: "./sim-testnet", count: 1},
		{phase: "evidence_simulator_provisional_reject", job: "evidence-simulator-provisional-reject", variable: "simulator_evidence_provisional_reject_tests", selector: releaseGateSimulatorEvidenceProvisionalRejectSelector, packages: "./sim-testnet", count: 1},
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
		if group.phase == "evidence_simulator" {
			expected = append(expected, "simulator_evidence_serial_skip_tests='"+serialSkip+"'")
			actual, err := releaseConnectPolicySelectorAssignment(script, "simulator_evidence_serial_skip_tests")
			if err != nil || actual != serialSkip {
				return fmt.Errorf("simulator evidence serial skip lost its exact parallel complement: %v", err)
			}
		}
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
			if selected.MatchString(root) && (group.skip == "" || !slow.MatchString(root) && !render.MatchString(root) && !parallelKVs[root]) {
				if !original.MatchString(root) {
					return fmt.Errorf("simulator evidence owner selected foreign root %s", root)
				}
				owners[root]++
				if group.phase == "evidence_simulator" || group.phase == "evidence_simulator_parallel" {
					ordinaryRootsKVs[root] = true
				}
				count++
			}
		}
		if count == 0 || group.count != 0 && count != group.count {
			return fmt.Errorf("simulator evidence %s has an incomplete root census", group.phase)
		}
	}
	for _, root := range roots {
		if original.MatchString(root) && owners[root] != 1 {
			return fmt.Errorf("simulator evidence root %s has %d owners per mode", root, owners[root])
		}
	}
	if len(renderRoots) == 0 {
		return fmt.Errorf("simulator evidence full-launch render source census is empty")
	}
	for _, root := range renderRoots {
		if owners[root] != 1 || ordinaryRootsKVs[root] {
			return fmt.Errorf("full-launch renderer %s lacks a dedicated evidence owner", root)
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
	roots, renderRoots, parallelRoots := releaseGateSimulatorEvidenceInventory(t)
	if err := verifyReleaseGateSimulatorEvidencePartition(string(script), roots, renderRoots, parallelRoots); err != nil {
		t.Fatal(err)
	}
	count := 0
	selected := regexp.MustCompile(releaseGateSimulatorEvidenceSelector)
	for _, root := range roots {
		if selected.MatchString(root) {
			count++
		}
	}
	t.Logf("simulator census: %d roots; parallel: %d; one owner per root in normal and race", count, len(parallelRoots))
	// An adjacent root, including a suffix of a slow root, remains ordinary.
	roots = append(roots, "TestEvidenceRelayFutureBoundary", "TestRuntimeEvidenceLaunchV2TemplateReachesGeneratedSetupAndRenderAdjacent")
	if err := verifyReleaseGateSimulatorEvidencePartition(string(script), roots, renderRoots, parallelRoots); err != nil {
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
	roots, renderRoots, parallelRoots := releaseGateSimulatorEvidenceInventory(t)
	if err := verifyReleaseGateSimulatorEvidencePartition(script, roots, renderRoots, parallelRoots); err != nil {
		t.Fatal(err)
	}
	const serialSkipArgument = ` -skip "$simulator_evidence_serial_skip_tests"`
	const normal = `go test ./sim-testnet -run "$simulator_evidence_tests" -count=1` + serialSkipArgument + ` -timeout 10m`
	const race = `go test -race ./sim-testnet -run "$simulator_evidence_slow_tests" -count=1 -timeout 10m`
	const start = "release_gate_start evidence-simulator-slow release_phase_evidence_simulator_slow"
	const renderRace = `go test -race ./sim-testnet -run "$simulator_evidence_render_tests" -count=1 -timeout 10m`
	const renderStart = "release_gate_start evidence-simulator-render release_phase_evidence_simulator_render"
	const parallelRace = `go test -race ./sim-testnet -run "$simulator_evidence_parallel_tests" -count=1 -timeout 10m`
	const parallelStart = "release_gate_start evidence-simulator-parallel release_phase_evidence_simulator_parallel"
	const provisionalRenderRace = `go test -race ./sim-testnet -run "$simulator_evidence_provisional_render_tests" -count=1 -timeout 10m`
	const provisionalRenderStart = "release_gate_start evidence-simulator-provisional-render release_phase_evidence_simulator_provisional_render"
	const provisionalRejectRace = `go test -race ./sim-testnet -run "$simulator_evidence_provisional_reject_tests" -count=1 -timeout 10m`
	const provisionalRejectStart = "release_gate_start evidence-simulator-provisional-reject release_phase_evidence_simulator_provisional_reject"
	parallelAssignment := "simulator_evidence_parallel_tests='^(" + strings.Join(parallelRoots, "|") + ")$'"
	for _, mutation := range []struct{ old, replacement string }{
		{normal, strings.Replace(normal, serialSkipArgument, "", 1)},
		{old: normal, replacement: strings.Replace(normal, serialSkipArgument, releaseGateSimulatorEvidenceOwnerSkip, 1)},
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
		{old: renderRace, replacement: "# " + renderRace},
		{old: renderRace, replacement: strings.Replace(renderRace, "-race ", "", 1)},
		{old: renderRace, replacement: strings.Replace(renderRace, "10m", "30m", 1)},
		{old: renderRace, replacement: renderRace + " || true"},
		{old: renderStart, replacement: "# " + renderStart},
		{old: renderStart, replacement: renderStart + "\n" + renderStart},
		{old: renderStart, replacement: "if false; then\n" + renderStart + "\nfi"},
		{old: "simulator_evidence_render_tests='" + releaseGateSimulatorEvidenceRenderSelector + "'", replacement: "simulator_evidence_render_tests='^TestRuntimeEvidence'"},
		{old: parallelRace, replacement: "# " + parallelRace},
		{old: parallelRace, replacement: strings.Replace(parallelRace, "-race ", "", 1)},
		{old: parallelRace, replacement: strings.Replace(parallelRace, "10m", "30m", 1)},
		{old: parallelRace, replacement: parallelRace + " -skip '^TestRuntimeEvidence'"},
		{old: parallelStart, replacement: "# " + parallelStart},
		{old: parallelStart, replacement: parallelStart + "\n" + parallelStart},
		{old: parallelStart, replacement: "if false; then\n" + parallelStart + "\nfi"},
		{old: parallelAssignment, replacement: "simulator_evidence_parallel_tests='^(" + strings.Join(parallelRoots[1:], "|") + ")$'"},
		{old: parallelAssignment, replacement: "simulator_evidence_parallel_tests='^(" + strings.Join(append(slices.Clone(parallelRoots), parallelRoots[0]), "|") + ")$'"},
		{old: provisionalRenderRace, replacement: "# " + provisionalRenderRace},
		{old: provisionalRenderRace, replacement: strings.Replace(provisionalRenderRace, "10m", "30m", 1)},
		{old: provisionalRenderStart, replacement: "# " + provisionalRenderStart},
		{old: provisionalRenderStart, replacement: "if false; then\n" + provisionalRenderStart + "\nfi"},
		{old: provisionalRejectRace, replacement: "# " + provisionalRejectRace},
		{old: provisionalRejectRace, replacement: strings.Replace(provisionalRejectRace, "-race ", "", 1)},
		{old: provisionalRejectStart, replacement: "# " + provisionalRejectStart},
		{old: provisionalRejectStart, replacement: provisionalRejectStart + "\n" + provisionalRejectStart},
	} {
		if strings.Count(script, mutation.old) != 1 {
			t.Fatalf("mutation does not identify one boundary: %s", mutation.old)
		}
		changed := strings.Replace(script, mutation.old, mutation.replacement, 1)
		if err := verifyReleaseGateSimulatorEvidencePartition(changed, roots, renderRoots, parallelRoots); err == nil {
			t.Fatalf("changed evidence execution was accepted: %s", mutation.replacement)
		}
	}
}

// Strings and unrelated recursion are not render calls; indirect wrappers in
// a different source file still inherit the full-population workload.
func TestProducerGateStateSelectionClassifiesFullLaunchRenderHelpers(t *testing.T) {
	t.Parallel()
	var files []*ast.File
	for _, source := range []string{
		`package fixture
func TestRuntimeEvidenceSyntheticDirect() { testRuntimeEvidenceLaunchTemplateRender() }
func TestRuntimeEvidenceSyntheticIndirect() { syntheticRenderWrapper() }
func TestRuntimeEvidenceSyntheticOrdinary() { syntheticCycle(); syntheticExternal() }
func TestRuntimeEvidenceSyntheticLiteral() { _ = "testRuntimeEvidenceLaunchTemplateRender()" }
`,
		`package fixture
func syntheticRenderWrapper() { testRuntimeEvidenceLaunchTemplateRender() }
func syntheticCycle() { syntheticCycle() }
func syntheticExternal()
`,
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "synthetic.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	want := []string{"TestRuntimeEvidenceSyntheticDirect", "TestRuntimeEvidenceSyntheticIndirect"}
	if got := releaseGateSimulatorEvidenceRenderRoots(files); !slices.Equal(got, want) {
		t.Fatalf("full-launch render classification=%v, want %v", got, want)
	}
}

// A new evidence wrapper may keep exactly one ordinary owner yet recreate the
// timeout. Its source-derived workload must require an independently reviewed owner.
func TestProducerGateStateSelectionRejectsFutureFullLaunchRenderOwner(t *testing.T) {
	t.Parallel()
	encoded, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	roots, renderRoots, parallelRoots := releaseGateSimulatorEvidenceInventory(t)
	if err := verifyReleaseGateSimulatorEvidencePartition(string(encoded), roots, renderRoots, parallelRoots); err != nil {
		t.Fatal(err)
	}
	const future = "TestRuntimeEvidenceSyntheticFutureRender"
	roots = append(roots, future)
	if err := verifyReleaseGateSimulatorEvidencePartition(string(encoded), roots, renderRoots, parallelRoots); err != nil {
		t.Fatalf("ordinary future evidence root lost coverage: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "synthetic.go", `package fixture
func TestRuntimeEvidenceSyntheticFutureRender() { syntheticRenderWrapper() }
func syntheticRenderWrapper() { testRuntimeEvidenceLaunchTemplateRender() }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	futureRenderRoots := append(slices.Clone(renderRoots), releaseGateSimulatorEvidenceRenderRoots([]*ast.File{file})...)
	if err := verifyReleaseGateSimulatorEvidencePartition(string(encoded), roots, futureRenderRoots, parallelRoots); err == nil || !strings.Contains(err.Error(), future+" lacks a dedicated evidence owner") {
		t.Fatalf("future full-launch wrapper regained an ordinary owner: %v", err)
	}
	// Declaring the wrapper parallel cannot make its full render workload
	// ordinary, even when both scheduling selectors are updated exactly.
	futureParallelRoots := append(slices.Clone(parallelRoots), future)
	slices.Sort(futureParallelRoots)
	oldAlternatives := strings.Join(parallelRoots, "|")
	if strings.Count(string(encoded), oldAlternatives) != 2 {
		t.Fatal("future parallel control lost its two exact selector operands")
	}
	parallelScript := strings.ReplaceAll(string(encoded), oldAlternatives, strings.Join(futureParallelRoots, "|"))
	if err := verifyReleaseGateSimulatorEvidencePartition(parallelScript, roots, renderRoots, futureParallelRoots); err != nil {
		t.Fatalf("ordinary future parallel root lost coverage: %v", err)
	}
	if err := verifyReleaseGateSimulatorEvidencePartition(parallelScript, roots, futureRenderRoots, futureParallelRoots); err == nil || !strings.Contains(err.Error(), future+" lacks a dedicated evidence owner") {
		t.Fatalf("future full-launch wrapper regained a parallel owner: %v", err)
	}
}
