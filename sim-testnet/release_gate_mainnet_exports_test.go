package main

// The qualification exports: sn-mainnet-exports builds every export that
// ./mainnet's tests and the server's public native-fee tests consume, offline,
// from the working checkouts, into the gate's private storage, and every
// ./mainnet shard and server-db's native-fee owner waits for it and runs with
// its variables, or fails without them.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The exports job's whole body.
const releaseGateMainnetExportsCommand = `"$sn_repo/scripts/build-mainnet-exports.sh" "$release_gate_mainnet_exports"`

// The gate's join of the exports job, before the first shard's admission.
var releaseGateMainnetExportsAwait = []string{
	"release_gate_await sn-mainnet-exports",
	`release_gate_mainnet_exports_exit="$RELEASE_GATE_AWAITED_EXIT"`,
	"export release_gate_mainnet_exports_exit",
}

// The URNETWORK_ names of ./mainnet's sources that name no export: a test's
// own child-process configuration, an optional output directory, an expectation that
// a fixture derives from an export, and production configuration.
var releaseGateMainnetNonExports = []string{
	"URNETWORK_MONITOR_FIXTURE_FAILURE_CHILD", "URNETWORK_MONITOR_WORKER_STDERR_CHILD", "URNETWORK_TEST_SUCCESSOR_MEMBER_CRASH",
	"URNETWORK_E2E_AMENDMENT_SIGNER",
	"URNETWORK_NATIVE_FEE_EXPORT_DIRECTORY",
	"URNETWORK_NATIVE_YUMA_POPULATED_EXPECTATION",
	"URNETWORK_STATE_DIR", "URNETWORK_ST_PROFILE",
}

// The Go exporters the builder runs: the checkout and package, the builder's
// whole command, the test, and the variables the test reads.
var releaseGateMainnetGoExporters = []struct {
	checkout, directory, command, test string
	variables                          []string
}{
	{checkout: "sn", directory: "validator", command: `URNETWORK_PROVIDER_ATTEMPT_COMPLETED_EXPORT_DIR="$output/provider-attempt" go test ./validator -run '^TestProviderAttemptSourceExportsActualCompletedFailedIdleOriginals$' -count=1`, test: "TestProviderAttemptSourceExportsActualCompletedFailedIdleOriginals", variables: []string{"URNETWORK_PROVIDER_ATTEMPT_COMPLETED_EXPORT_DIR"}},
	{checkout: "sn", directory: "payoutartifact", command: `URNETWORK_PROVIDER_WORK_ATTEMPT_ARTIFACT="$output/provider-attempt/artifact.json" URNETWORK_PROVIDER_WORK_EXPORT_DIR="$output/provider-work" go test ./payoutartifact -run '^TestWholeWorkExportsCompleteOriginalsForActualTrialProviderUniverse$' -count=1`, test: "TestWholeWorkExportsCompleteOriginalsForActualTrialProviderUniverse", variables: []string{"URNETWORK_PROVIDER_WORK_ATTEMPT_ARTIFACT", "URNETWORK_PROVIDER_WORK_EXPORT_DIR"}},
	{checkout: "server", directory: "strecovery", command: `URNETWORK_ECONOMIC_FEE_EXPORT_DIR="$output/economic-fee" go test ./strecovery -run '^TestExportEconomicConservationFeeInputs$' -count=1`, test: "TestExportEconomicConservationFeeInputs", variables: []string{"URNETWORK_ECONOMIC_FEE_EXPORT_DIR"}},
}

// The builder's native-fee export: the mainnet.test image it builds where the
// image stays, the image's run that exports the server's settlement fixtures,
// the test, and the variable it reads.
var releaseGateMainnetNativeFeeExporter = struct {
	assignment, build, run, test, variable string
}{
	assignment: `verifier="$output/native-fee-verifier/mainnet.test"`,
	build:      `go test -c -o "$verifier" ./mainnet`,
	run:        `URNETWORK_NATIVE_FEE_EXPORT_DIRECTORY="$output/native-fee" "$verifier" -test.run '^TestNativeFeeOutcomeExportSettlementFixtures$' -test.timeout 10m`,
	test:       "TestNativeFeeOutcomeExportSettlementFixtures",
	variable:   "URNETWORK_NATIVE_FEE_EXPORT_DIRECTORY",
}

// The sources whose URNETWORK_ names are the exports' variables: ./mainnet's,
// and the server's public native-fee tests, which server-db runs with the
// exports' environment.
var releaseGateMainnetExportConsumers = []string{"../mainnet/*.go", "../../server/controller/st_native_fee_settlement_public_test.go", "../../server/model/st_native_fee_settlement_public_test.go"}

// The builder's Cargo commands, each in tools/runtime-metadata-probe.
const releaseGateMainnetCargoBuild = "build --offline --locked --bin runtime-historical-capture --bin runtime-historical-proof"
const releaseGateMainnetCargoTest = "test --offline --locked --lib -- --exact"

func readReleaseGateMainnetExportsBuilder(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../scripts/build-mainnet-exports.sh")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The free KiB the builder requires, from its one assignment.
func releaseGateMainnetExportsRequiredKiB(t *testing.T) int {
	t.Helper()
	builder := readReleaseGateMainnetExportsBuilder(t)
	match := regexp.MustCompile(`(?m)^required_kib=\$\(\(([1-9][0-9]?) \* 1024 \* 1024\)\)$`).FindAllStringSubmatch(builder, -1)
	if len(match) != 1 {
		t.Fatal("the builder has no one required free space")
	}
	gibibytes, err := strconv.Atoi(match[0][1])
	if err != nil {
		t.Fatal(err)
	}
	return gibibytes * 1024 * 1024
}

// The exports' variables, read once for every test that needs them.
var releaseGateMainnetExportCensus = sync.OnceValues(func() ([]string, error) {
	var paths []string
	for _, pattern := range releaseGateMainnetExportConsumers {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			return nil, fmt.Errorf("export consumer census %s: %d files, %v", pattern, len(matches), err)
		}
		paths = append(paths, matches...)
	}
	name := regexp.MustCompile(`^URNETWORK_[A-Z0-9_]+$`)
	seen := map[string]bool{}
	for _, path := range paths {
		if matched, err := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path)); err != nil || !matched {
			if err != nil {
				return nil, err
			}
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
				if text, err := strconv.Unquote(literal.Value); err == nil && name.MatchString(text) && !slices.Contains(releaseGateMainnetNonExports, text) {
					seen[text] = true
				}
			}
			return true
		})
	}
	variables := make([]string, 0, len(seen))
	for variable := range seen {
		variables = append(variables, variable)
	}
	slices.Sort(variables)
	return variables, nil
})

// Every URNETWORK_ name that ./mainnet's default build and the server's
// public native-fee tests spell, but the named non-exports, sorted.
func releaseGateMainnetExportVariables(t *testing.T) []string {
	t.Helper()
	variables, err := releaseGateMainnetExportCensus()
	if err != nil {
		t.Fatal(err)
	}
	return variables
}

// The rows of one single-quoted table of the builder, each of three fields.
func releaseGateMainnetBuilderTable(builder, name string) ([][3]string, error) {
	start := strings.Index(builder, "\n"+name+"='")
	if start < 0 || strings.Count(builder, "\n"+name+"='") != 1 {
		return nil, fmt.Errorf("the builder has no one %s table", name)
	}
	body := builder[start+len(name)+3:]
	end := strings.Index(body, "'")
	if end < 0 {
		return nil, fmt.Errorf("the builder's %s table is unterminated", name)
	}
	var rows [][3]string
	for _, line := range strings.Split(body[:end], "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("the builder's %s table has the row %q", name, line)
		}
		rows = append(rows, [3]string(fields))
	}
	return rows, nil
}

// The names of the gate's one unset of inherited exports, which may continue
// over lines.
func releaseGateMainnetExportUnset(script string) ([]string, error) {
	start := strings.Index(script, "\nunset URNETWORK_")
	if start < 0 || strings.Count(script, "\nunset URNETWORK_") != 1 {
		return nil, errors.New("the gate has no one unset of the exports")
	}
	var names []string
	for _, line := range strings.Split(script[start+1:], "\n") {
		fields := strings.Fields(strings.TrimSuffix(line, `\`))
		if len(names) == 0 {
			fields = fields[1:]
		}
		names = append(names, fields...)
		if !strings.HasSuffix(line, `\`) {
			break
		}
	}
	return names, nil
}

// Every export variable is one row of the builder's table of exports, from sn
// or the server, with its own relative path, or of its table of digests, of a
// file of an export, and one name of the gate's unset.
func verifyReleaseGateMainnetExportCoverage(script, builder string, variables []string) error {
	rows, err := releaseGateMainnetBuilderTable(builder, "exports")
	if err != nil {
		return err
	}
	var built, paths []string
	for _, row := range rows {
		top := strings.Split(row[1], "/")[0]
		if filepath.IsAbs(row[1]) || filepath.Clean(row[1]) != row[1] || strings.HasPrefix(row[1], ".") || slices.Contains(paths, top) || row[2] != "sn" && row[2] != "server" {
			return fmt.Errorf("the builder's export row %q", row)
		}
		built, paths = append(built, row[0]), append(paths, top)
	}
	digests, err := releaseGateMainnetBuilderTable(builder, "digests")
	if err != nil {
		return err
	}
	for _, row := range digests {
		if !slices.Contains(rows, [3]string{strings.TrimSuffix(row[0], "_SHA256"), row[1], row[2]}) {
			return fmt.Errorf("the builder's digest row %q digests no export of its file", row)
		}
		built = append(built, row[0])
	}
	slices.Sort(built)
	if !slices.Equal(built, variables) {
		return fmt.Errorf("the builder builds %q, ./mainnet reads %q", built, variables)
	}
	unset, err := releaseGateMainnetExportUnset(script)
	if err != nil {
		return err
	}
	slices.Sort(unset)
	if !slices.Equal(unset, variables) {
		return fmt.Errorf("the gate drops %q, ./mainnet reads %q", unset, variables)
	}
	return nil
}

// The body of each Rust test function of a probe source, by name.
func releaseGateMainnetRustTests(source string) map[string]string {
	tests := map[string]string{}
	for _, match := range regexp.MustCompile(`(?ms)^#\[test\]\nfn ([a-z0-9_]+)\(\) \{\n(.*?)^\}$`).FindAllStringSubmatch(source, -1) {
		tests[match[1]] = match[2]
	}
	return tests
}

// Each Rust export's root is a test of its module that writes the directory
// named by the variable the builder gives it, which is the first path of an
// export of sn.
func verifyReleaseGateMainnetRustExports(builder string, sources map[string]string) error {
	rows, err := releaseGateMainnetBuilderTable(builder, "rust_exports")
	if err != nil {
		return err
	}
	exports, err := releaseGateMainnetBuilderTable(builder, "exports")
	if err != nil {
		return err
	}
	modules := map[string]string{"": "historical_native_execution_tests.rs"}
	for _, match := range regexp.MustCompile(`(?m)^#\[path = "([a-z_]+\.rs)"\]\nmod ([a-z_]+);$`).FindAllStringSubmatch(sources["historical_native_execution_tests.rs"], -1) {
		modules[match[2]] = match[1]
	}
	for _, row := range rows {
		module, name, nested := strings.Cut(row[2], "::")
		if !nested {
			module, name = "", row[2]
		}
		body, found := releaseGateMainnetRustTests(sources[modules[module]])[name]
		if !found {
			return fmt.Errorf("%s is no test of historical::tests::native_execution_tests", row[2])
		}
		if !strings.Contains(body, strconv.Quote(row[0])) {
			return fmt.Errorf("%s does not write %s", row[2], row[0])
		}
		if !slices.ContainsFunc(exports, func(export [3]string) bool {
			return export[2] == "sn" && strings.Split(export[1], "/")[0] == row[1]
		}) {
			return fmt.Errorf("%s writes %s, which is no export", row[2], row[1])
		}
	}
	return nil
}

// The text of each top-level function of a Go package directory's test files,
// by name.
func releaseGateGoTestFunctions(t *testing.T, directory string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(directory, "*_test.go"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("%s has %d test files: %v", directory, len(paths), err)
	}
	functions := map[string]string{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, raw, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil {
				functions[function.Name.Name] = string(raw[fileSet.Position(function.Pos()).Offset:fileSet.Position(function.End()).Offset])
			}
		}
	}
	return functions
}

// Every export variable that ./mainnet's tests and the server's public
// native-fee tests read is an export the exports job builds, from sn or the
// server, and a name the gate drops from its caller's environment: no test
// lacks an export, nor reads a stale one. Each Rust root and Go exporter the
// builder runs is a real test that writes what the builder gives it, and the
// native-fee export runs from the image that the verifier export names.
func TestReleaseGateMainnetExportsCoverEveryExportVariable(t *testing.T) {
	t.Parallel()
	script, builder := readReleaseGateLocalScript(t), readReleaseGateMainnetExportsBuilder(t)
	variables := releaseGateMainnetExportVariables(t)
	if len(variables) < 21 || !slices.Contains(variables, "URNETWORK_NATIVE_CAPTURE_ENGINE") || !slices.Contains(variables, "URNETWORK_ECONOMIC_FEE_FIXTURE_DIR") || !slices.Contains(variables, "URNETWORK_NATIVE_FEE_SETTLEMENT_VERIFIER_SHA256") {
		t.Fatalf("the consumers read only the exports %q", variables)
	}
	if err := verifyReleaseGateMainnetExportCoverage(script, builder, variables); err != nil {
		t.Fatal(err)
	}
	// A new export a test reads, a row the builder loses and a name the gate
	// keeps are each refused.
	unsetName := "URNETWORK_NATIVE_YUMA_FIXTURE_DIR URNETWORK_NATIVE_YUMA_CAPACITY_FIXTURE_DIR"
	for name, change := range map[string]func() error{
		"a new export": func() error {
			return verifyReleaseGateMainnetExportCoverage(script, builder, append(slices.Clone(variables), "URNETWORK_NATIVE_INVENTED_FIXTURE"))
		},
		"a lost row": func() error {
			return verifyReleaseGateMainnetExportCoverage(script, strings.Replace(builder, "URNETWORK_NATIVE_YUMA_FIXTURE_DIR yuma sn\n", "", 1), variables)
		},
		"a kept name": func() error {
			return verifyReleaseGateMainnetExportCoverage(strings.Replace(script, unsetName, "URNETWORK_NATIVE_YUMA_CAPACITY_FIXTURE_DIR", 1), builder, variables)
		},
		"a shared path": func() error {
			return verifyReleaseGateMainnetExportCoverage(script, strings.Replace(builder, "URNETWORK_NATIVE_YUMA_FIXTURE_DIR yuma sn", "URNETWORK_NATIVE_YUMA_FIXTURE_DIR yuma-capacity sn", 1), variables)
		},
		"a lost digest": func() error {
			return verifyReleaseGateMainnetExportCoverage(script, strings.Replace(builder, "digests='URNETWORK_NATIVE_FEE_SETTLEMENT_VERIFIER_SHA256 ", "digests='URNETWORK_NATIVE_FEE_SETTLEMENT_VERIFIER_DIGEST ", 1), variables)
		},
		"a digest of another file": func() error {
			return verifyReleaseGateMainnetExportCoverage(script, strings.Replace(builder, "URNETWORK_NATIVE_FEE_SETTLEMENT_VERIFIER_SHA256 native-fee-verifier/mainnet.test sn", "URNETWORK_NATIVE_FEE_SETTLEMENT_VERIFIER_SHA256 native-fee/pair.json sn", 1), variables)
		},
	} {
		if err := change(); err == nil {
			t.Fatalf("the export census accepted %s", name)
		}
	}
	probe := map[string]string{}
	paths, err := filepath.Glob("../tools/runtime-metadata-probe/src/*.rs")
	if err != nil || len(paths) == 0 {
		t.Fatalf("probe sources: %d, %v", len(paths), err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		probe[filepath.Base(path)] = string(raw)
	}
	if err := verifyReleaseGateMainnetRustExports(builder, probe); err != nil {
		t.Fatal(err)
	}
	rows, err := releaseGateMainnetBuilderTable(builder, "rust_exports")
	if err != nil || len(rows) != 13 {
		t.Fatalf("the builder runs %d Rust exports: %v", len(rows), err)
	}
	for _, change := range []struct{ old, new string }{
		{"historical_native_yuma_exports_original_complete_branches", "historical_native_yuma_exports_renamed_branches"},
		{"URNETWORK_NATIVE_YUMA_FIXTURE_OUT yuma ", "URNETWORK_NATIVE_YUMA_CAPACITY_FIXTURE_OUT yuma "},
		{"URNETWORK_NATIVE_YUMA_FIXTURE_OUT yuma ", "URNETWORK_NATIVE_YUMA_FIXTURE_OUT yuma-original "},
		{"capture_join_tests::historical_native_vault_capture_exports_original_transaction_causes", "yuma_tests::historical_native_vault_capture_exports_original_transaction_causes"},
	} {
		if strings.Count(builder, change.old) != 1 {
			t.Fatalf("the mutation names no unique text: %q", change.old)
		}
		if verifyReleaseGateMainnetRustExports(strings.Replace(builder, change.old, change.new, 1), probe) == nil {
			t.Fatalf("the Rust exports accepted %q", change.new)
		}
	}
	for _, exporter := range releaseGateMainnetGoExporters {
		if strings.Count(builder, "\n  "+exporter.command+"\n") != 1 {
			t.Fatalf("the builder does not run %s once: %s", exporter.test, exporter.command)
		}
		directory := filepath.Join("..", exporter.directory)
		if exporter.checkout == "server" {
			directory = filepath.Join("..", "..", "server", exporter.directory)
		}
		body, found := releaseGateGoTestFunctions(t, directory)[exporter.test]
		if !found {
			t.Fatalf("%s is no test of %s", exporter.test, directory)
		}
		for _, variable := range exporter.variables {
			if !strings.Contains(body, strconv.Quote(variable)) {
				t.Fatalf("%s does not read %s", exporter.test, variable)
			}
		}
	}
	exports, err := releaseGateMainnetBuilderTable(builder, "exports")
	if err != nil {
		t.Fatal(err)
	}
	native := releaseGateMainnetNativeFeeExporter
	if !slices.Contains(exports, [3]string{"URNETWORK_NATIVE_FEE_SETTLEMENT_VERIFIER", strings.TrimSuffix(strings.TrimPrefix(native.assignment, `verifier="$output/`), `"`), "sn"}) {
		t.Fatalf("the verifier export is not the image %s", native.assignment)
	}
	for _, line := range []string{native.assignment, "  " + native.build, "  " + native.run} {
		if strings.Count(builder, "\n"+line+"\n") != 1 {
			t.Fatalf("the builder does not run %s once: %s", native.test, line)
		}
	}
	if body, found := releaseGateGoTestFunctions(t, "../mainnet")[native.test]; !found || !strings.Contains(body, strconv.Quote(native.variable)) {
		t.Fatalf("%s is no test of ../mainnet that reads %s", native.test, native.variable)
	}
}

// sn-mainnet-exports runs the builder alone, into the gate's private storage,
// as the gate's first phase, admitted unconditionally: the job exists in both
// dependency modes, and the builder, which never reads the mode, builds the
// same exports in each. Starting first, it keeps the shards' wait short.
func TestReleaseGateMainnetExportsJobRunsInBothModes(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	verify := func(script string) error {
		if err := verifyReleaseGateExactPhase(script, "sn-mainnet-exports", "sn_mainnet_exports", []string{releaseGateMainnetExportsCommand}); err != nil {
			return err
		}
		if first := regexp.MustCompile(`(?m)^release_gate_start [a-z0-9-]+ release_phase_`).FindString(releaseGateRegistryLines(script)); first != "release_gate_start sn-mainnet-exports release_phase_" {
			return fmt.Errorf("the gate's first phase is %q, not sn-mainnet-exports", first)
		}
		return nil
	}
	if err := verify(script); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ old, new string }{
		{"release_gate_start sn-mainnet-exports release_phase_sn_mainnet_exports\n", "if [[ \"$release_gate_dependency_mode\" == pinned ]]; then\nrelease_gate_start sn-mainnet-exports release_phase_sn_mainnet_exports\nfi\n"},
		{"release_gate_start sn-mainnet-exports release_phase_sn_mainnet_exports\n", ""},
		{"\n  " + releaseGateMainnetExportsCommand + "\n", "\n  " + releaseGateMainnetExportsCommand + " || true\n"},
		{"release_gate_start sn-mainnet-exports release_phase_sn_mainnet_exports\n", "release_gate_start isolation-regressions release_phase_isolation_regressions\nrelease_gate_start sn-mainnet-exports release_phase_sn_mainnet_exports\n"},
	} {
		if strings.Count(script, change.old) != 1 {
			t.Fatalf("the mutation names no unique text: %q", change.old)
		}
		if verify(strings.Replace(script, change.old, change.new, 1)) == nil {
			t.Fatalf("the exports job accepted %q", change.new)
		}
	}
	builder := readReleaseGateMainnetExportsBuilder(t)
	if strings.Contains(strings.ToLower(builder), "dependency_mode") || strings.Contains(builder, "RELEASE_GATE") {
		t.Fatal("the builder reads the gate's mode or state")
	}
}

// Every ./mainnet shard is admitted after the gate joins sn-mainnet-exports
// and exports that job's exit, unconditionally; the shards depend on the job
// through that exit.
func TestReleaseGateMainnetShardsAwaitTheExports(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	verify := func(script string) error {
		registry := releaseGateRegistryLines(script)
		block := "\n" + strings.Join(releaseGateMainnetExportsAwait, "\n") + "\n"
		if strings.Count(registry, block) != 1 || strings.Count(script, "release_gate_await ") != 1 || strings.Count(script, "release_gate_mainnet_exports_exit=") != 1 {
			return errors.New("the gate does not join the exports job once, exporting its exit")
		}
		await := strings.Index(registry, block)
		if job := strings.Index(registry, "\nrelease_gate_start sn-mainnet-exports release_phase_sn_mainnet_exports\n"); job < 0 || job > await {
			return errors.New("the gate joins the exports job before admitting it")
		}
		for shard := 1; shard <= releaseGateMainnetShards.count; shard++ {
			index := strconv.Itoa(shard)
			if start := strings.Index(registry, "\nrelease_gate_start sn-mainnet-"+index+" release_phase_sn_mainnet_"+index+"\n"); start < await {
				return fmt.Errorf("shard %d is admitted before the exports job joins", shard)
			}
		}
		for _, line := range releaseGateMainnetExportsAwait {
			conditions, err := releaseGateRegistrationConditions(script, line)
			if err != nil || len(conditions) != 0 {
				return fmt.Errorf("the gate joins the exports job conditionally: %q %v", conditions, err)
			}
		}
		return nil
	}
	if err := verify(script); err != nil {
		t.Fatal(err)
	}
	await := strings.Join(releaseGateMainnetExportsAwait, "\n") + "\n"
	for _, change := range []struct{ old, new string }{
		{await, ""},
		{await, strings.Replace(await, "sn-mainnet-exports", "sn-all-normal", 1)},
		{"export release_gate_mainnet_exports_exit\n", ""},
		{await + "release_gate_start sn-mainnet-1 release_phase_sn_mainnet_1\n", "release_gate_start sn-mainnet-1 release_phase_sn_mainnet_1\n" + await},
		{await, "if [[ \"$release_gate_dependency_mode\" == pinned ]]; then\n" + await + "fi\n"},
	} {
		if strings.Count(script, change.old) != 1 {
			t.Fatalf("the mutation names no unique text: %q", change.old)
		}
		if verify(strings.Replace(script, change.old, change.new, 1)) == nil {
			t.Fatalf("the shards' dependency accepted %q", change.new)
		}
	}
}

// The gate drops every export variable it inherits before any job exists,
// only the shard owner and server-db's native-fee owner read the exports job's
// environment, and a shard's test run sees exactly that environment's values.
func TestReleaseGateMainnetShardsTakeTheVariablesOnlyFromTheExports(t *testing.T) {
	t.Parallel()
	variables := releaseGateMainnetExportVariables(t)
	prelude := releaseGateLocalFragment(t, "# Release qualification cannot hide", "\nsn_repo=")
	command := exec.Command("bash", "-c", "set -euo pipefail\n"+prelude+"\nenv")
	command.Env = os.Environ()
	for _, variable := range variables {
		command.Env = append(command.Env, variable+"=/stale/"+variable)
	}
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if variable, _, found := strings.Cut(line, "="); found && slices.Contains(variables, variable) {
			t.Fatalf("the gate kept the inherited %s", line)
		}
	}
	script := readReleaseGateLocalScript(t)
	raw, err := os.ReadFile("../scripts/release-gate-jobs.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, variable := range variables {
		name := regexp.MustCompile(`\b` + regexp.QuoteMeta(variable) + `\b`)
		if len(name.FindAllString(script, -1)) != 1 || name.MatchString(string(raw)) {
			t.Fatalf("the gate or its runner names %s beside dropping it", variable)
		}
	}
	owner := regexp.MustCompile(`(?ms)^release_gate_mainnet_shard\(\) \{\n(.*?)^\}$`).FindAllStringSubmatch(script, -1)
	nativeFee := regexp.MustCompile(`(?ms)^  release_gate_server_native_fee\(\) \{\n(.*?)^  \}$`).FindAllStringSubmatch(script, -1)
	if strings.Count(script, `"$release_gate_mainnet_exports/environment.sh"`) != 2 || len(owner) != 1 || strings.Count(owner[0][1], "\n  source \"$release_gate_mainnet_exports/environment.sh\"\n") != 1 ||
		len(nativeFee) != 1 || strings.Count(nativeFee[0][1], "\n    source \"$release_gate_mainnet_exports/environment.sh\"\n") != 1 {
		t.Fatal("the shard owner and the native-fee owner are not the two readers of the exports' environment")
	}
	names := releaseGatePackageTestList(t, releaseGateMainnetShards)
	fixture := newReleaseGateShardFixture(t, "./mainnet", names)
	stale := ""
	for _, variable := range variables {
		stale += "export " + variable + "=/stale\n"
	}
	if output, err := runReleaseGateShardOwner(t, fixture.prelude("skip")+stale, `release_gate_mainnet_shard 1`); err != nil {
		t.Fatalf("the shard failed: %v\n%s", err, output)
	}
	seen, err := os.ReadFile(filepath.Join(fixture.root, "environment"))
	if err != nil {
		t.Fatal(err)
	}
	var got, want []string
	for _, line := range strings.Split(string(seen), "\n") {
		if variable, _, _ := strings.Cut(line, "="); slices.Contains(variables, variable) {
			got = append(got, line)
		}
	}
	for _, variable := range variables {
		want = append(want, variable+"="+fixture.export(variable))
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the shard's tests saw %q, want %q", got, want)
	}
}

// A shard without the exports job's exit zero fails, naming that exit, before
// it lists or runs a test; on the real queue, a failed exports job fails all
// eight shards, each joined with its own failure, and the gate.
func TestReleaseGateMainnetShardsFailWhenTheExportsFail(t *testing.T) {
	t.Parallel()
	names := releaseGatePackageTestList(t, releaseGateMainnetShards)
	for _, exit := range []string{"release_gate_mainnet_exports_exit=1", "release_gate_mainnet_exports_exit=", "unset release_gate_mainnet_exports_exit"} {
		fixture := newReleaseGateShardFixture(t, "./mainnet", names)
		output, err := runReleaseGateShardOwner(t, fixture.prelude("skip")+exit+"\n", `release_gate_mainnet_shard 3`)
		want := map[string]string{"release_gate_mainnet_exports_exit=1": "1", "release_gate_mainnet_exports_exit=": "unknown", "unset release_gate_mainnet_exports_exit": "unknown"}[exit]
		if err == nil || output != "release gate: ./mainnet shard 3 has no qualification exports: sn-mainnet-exports exit "+want+"\n" {
			t.Fatalf("%s: the shard ran: %v\n%s", exit, err, output)
		}
		for _, name := range []string{"calls", "arguments"} {
			if _, err := os.Stat(filepath.Join(fixture.root, name)); !os.IsNotExist(err) {
				t.Fatalf("%s: the shard listed or ran tests without the exports", exit)
			}
		}
	}
	fragment := releaseGateLocalFragment(t, "release_gate_shard_selector() {", "release_gate_start sn-core-race")
	queue := newReleaseGateJobsFixture(t, `
release_phase_sn_mainnet_exports() { return 9; }
release_phase_sn_all_normal() { true; }
go() { printf '%s\n' "$*" >> "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/go-calls"; }
export -f go
release_gate_start sn-mainnet-exports release_phase_sn_mainnet_exports
`+fragment+`
status=0; release_gate_complete || status=$?
exit "$status"
`)
	queue.join(t, 9)
	output := queue.output.String()
	logs := releaseGateQueueLogs(t, queue.root)
	for shard := 1; shard <= releaseGateMainnetShards.count; shard++ {
		label := "sn-mainnet-" + strconv.Itoa(shard)
		raw, err := os.ReadFile(filepath.Join(logs, label+".log"))
		if !strings.Contains(output, "[release gate] joined "+label+" (exit 1)") || err != nil || !strings.Contains(string(raw), "release gate: ./mainnet shard "+strconv.Itoa(shard)+" has no qualification exports: sn-mainnet-exports exit 9\n") {
			t.Fatalf("%s was not failed by the failed exports: %v\n%s\n%s", label, err, raw, output)
		}
	}
	if _, err := os.Stat(filepath.Join(queue.root, "go-calls")); !os.IsNotExist(err) {
		t.Fatal("a shard listed or ran tests after the exports failed")
	}
}

// On the real queue, a passing exports job's environment reaches every shard's
// test run, and the exports are removed once every shard has joined.
func TestReleaseGateMainnetShardsRunWithThePassingExports(t *testing.T) {
	t.Parallel()
	names := releaseGatePackageTestList(t, releaseGateMainnetShards)
	list := filepath.Join(t.TempDir(), "list")
	if err := os.WriteFile(list, []byte(strings.Join(names, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fragment := releaseGateLocalFragment(t, "release_gate_shard_selector() {", "release_gate_start sn-core-race")
	queue := newReleaseGateJobsFixture(t, `
release_phase_sn_mainnet_exports() {
  mkdir -m 700 "$release_gate_mainnet_exports"
  printf 'export URNETWORK_NATIVE_CAPTURE_ENGINE=%s\n' "$release_gate_mainnet_exports/engine" > "$release_gate_mainnet_exports/environment.sh"
}
release_phase_sn_all_normal() { true; }
release_gate_aggregate_skip=skip
export release_gate_aggregate_skip
go() {
  if [[ "$1 $2" == "test -list" ]]; then cat '`+list+`'; return; fi
  printf '%s %s\n' "$RELEASE_GATE_WORKER_PID" "$URNETWORK_NATIVE_CAPTURE_ENGINE" >> "$RELEASE_GATE_QUEUE_FIXTURE_ROOT/go-runs"
}
export -f go
release_gate_start sn-mainnet-exports release_phase_sn_mainnet_exports
`+fragment+`
release_gate_complete
[[ ! -e "$release_gate_mainnet_exports" ]]
`)
	queue.join(t, 0)
	raw, err := os.ReadFile(filepath.Join(queue.root, "go-runs"))
	if err != nil {
		t.Fatal(err)
	}
	runs := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	root := filepath.Dir(releaseGateQueueLogs(t, queue.root))
	for _, run := range runs {
		if _, engine, _ := strings.Cut(run, " "); engine != filepath.Join(root, "mainnet-exports", "engine") {
			t.Fatalf("a shard's tests ran with %q", run)
		}
	}
	if len(runs) != releaseGateMainnetShards.count {
		t.Fatalf("%d shards ran their tests, want %d\n%s", len(runs), releaseGateMainnetShards.count, queue.output.String())
	}
}

// The logs directory of the one gate root under a queue fixture.
func releaseGateQueueLogs(t *testing.T, root string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "urnetwork-release-gate.*", "logs"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("queue roots %q: %v", matches, err)
	}
	return matches[0]
}

// A copy of the builder in a workspace whose sn and server are Git checkouts,
// with cargo, go and df replaced on PATH by recorders: cargo build writes both
// engines, cargo test writes into each _OUT directory it is given, each go
// test writes what its exporter would, and go test -c writes a recording image
// whose run writes the native-fee settlement fixtures.
type releaseGateMainnetExportsBuild struct {
	root, workspace, calls string
}

// The fake cargo's test command pipes its _OUT variables into its loop, and
// bash waits for every process of a pipeline. Fed from a process substitution,
// which bash never waits for, the loop's last read ends once the substitution's
// shell closes the pipe in its exit, and the script could then exit before that
// shell does: the release gate's subreaper would adopt it. That window lies
// inside the substitution's own exit, so no test can force it.
const releaseGateMainnetFakeCargo = `#!/usr/bin/env bash
printf 'cargo %s|%s|%s|%s|%s\n' "$*" "$PWD" "$CARGO_TARGET_DIR" "${RUSTUP_TOOLCHAIN-unset}" "${RUSTUP_AUTO_INSTALL-unset}" >> "$FAKE_CALLS"
if [[ "$1" == build ]]; then
  mkdir -p "$CARGO_TARGET_DIR/debug"
  for engine in runtime-historical-capture runtime-historical-proof; do printf '%s\n' "$engine" > "$CARGO_TARGET_DIR/debug/$engine"; done
  exit 0
fi
env | grep '^URNETWORK_[A-Z_]*_FIXTURE_OUT=' | LC_ALL=C sort | while IFS='=' read -r variable directory; do
  [[ "$variable" == "${FAKE_SKIP:-}" ]] && continue
  case "$variable" in
    URNETWORK_NATIVE_EXECUTION_FIXTURE_OUT) printf '{"job":1}' > "$directory/native-job.json" ;;
    URNETWORK_NATIVE_TREASURY_FIXTURE_OUT) printf '{"credit":1}' > "$directory/treasury-credit.json"; printf '{}' > "$directory/treasury-missing-original.json" ;;
    *) printf '{"%s":1}' "$variable" > "$directory/job.json" ;;
  esac
done
`

const releaseGateMainnetFakeGo = `#!/usr/bin/env bash
printf 'go %s|%s|%s\n' "$*" "$PWD" "$(env | grep '^URNETWORK_' | LC_ALL=C sort | tr '\n' ' ')" >> "$FAKE_CALLS"
[[ "$2" != "${FAKE_FAIL:-}" ]] || exit 1
case "$2" in
  ./validator) for name in authority artifact original; do printf '{"%s":1}' "$name" > "$URNETWORK_PROVIDER_ATTEMPT_COMPLETED_EXPORT_DIR/$name.json"; done; mkdir -m 700 "$URNETWORK_PROVIDER_ATTEMPT_COMPLETED_EXPORT_DIR/scratch" ;;
  ./payoutartifact) [[ -f "$URNETWORK_PROVIDER_WORK_ATTEMPT_ARTIFACT" ]] && printf '{"work":1}' > "$URNETWORK_PROVIDER_WORK_EXPORT_DIR/inventory.json" ;;
  ./strecovery) printf '{"fee":1}' > "$URNETWORK_ECONOMIC_FEE_EXPORT_DIR/success.json" ;;
  -c) cp -- "$(dirname -- "$0")/mainnet.test" "$4" ;;
esac
`

// The image the fake go test -c writes.
const releaseGateMainnetFakeImage = `#!/usr/bin/env bash
printf 'mainnet.test %s|%s|%s\n' "$*" "$PWD" "$(env | grep '^URNETWORK_' | LC_ALL=C sort | tr '\n' ' ')" >> "$FAKE_CALLS"
[[ "${FAKE_SKIP:-}" != URNETWORK_NATIVE_FEE_EXPORT_DIRECTORY ]] || exit 0
for mode in pair missing fractional-conflict; do printf '{"%s":1}' "$mode" > "$URNETWORK_NATIVE_FEE_EXPORT_DIRECTORY/$mode.json"; done
`

const releaseGateMainnetFakeDf = `#!/usr/bin/env bash
printf 'df %s\n' "$*" >> "$FAKE_CALLS"
printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/fake 100 0 %s 0%% /\n' "$FAKE_AVAILABLE"
`

func newReleaseGateMainnetExportsBuild(t *testing.T) *releaseGateMainnetExportsBuild {
	t.Helper()
	root := t.TempDir()
	self := &releaseGateMainnetExportsBuild{root: root, workspace: filepath.Join(root, "workspace"), calls: filepath.Join(root, "calls")}
	builder := readReleaseGateMainnetExportsBuilder(t)
	for _, directory := range []string{"sn/scripts", "sn/tools/runtime-metadata-probe", "sn/mainnet", "server", "bin"} {
		path := filepath.Join(self.workspace, directory)
		if directory == "bin" {
			path = filepath.Join(root, "bin")
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(self.workspace, "sn", "scripts", "build-mainnet-exports.sh"), []byte(builder), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"cargo": releaseGateMainnetFakeCargo, "go": releaseGateMainnetFakeGo, "df": releaseGateMainnetFakeDf, "mainnet.test": releaseGateMainnetFakeImage} {
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, repository := range []string{"sn", "server"} {
		checkout := filepath.Join(self.workspace, repository)
		runTestGit(t, checkout, "init", "-q")
		if err := os.WriteFile(filepath.Join(checkout, "README.md"), []byte(repository+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runTestGit(t, checkout, "add", "-A")
		runTestGit(t, checkout, "commit", "-qm", "reviewed "+repository)
	}
	return self
}

// Run the builder into output, with the given free KiB and extra environment
// and no inherited URNETWORK_ variable.
func (self *releaseGateMainnetExportsBuild) run(t *testing.T, output, available string, extra ...string) (string, error) {
	t.Helper()
	command := exec.Command("bash", filepath.Join(self.workspace, "sn", "scripts", "build-mainnet-exports.sh"), output)
	command.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool { return strings.HasPrefix(entry, "URNETWORK_") })
	command.Env = append(command.Env, "PATH="+filepath.Join(self.root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_CALLS="+self.calls, "FAKE_AVAILABLE="+available, "RUSTUP_TOOLCHAIN=stable", "RUSTUP_AUTO_INSTALL=1")
	command.Env = append(command.Env, extra...)
	raw, err := command.CombinedOutput()
	return string(raw), err
}

func (self *releaseGateMainnetExportsBuild) recorded(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(self.calls)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

func (self *releaseGateMainnetExportsBuild) commit(t *testing.T, repository string) string {
	t.Helper()
	return strings.TrimSpace(string(testGitOutput(t, filepath.Join(self.workspace, repository), "rev-parse", "HEAD")))
}

// The builder checks the free space beside its new private directory, then
// runs the reviewed offline Cargo builds from the pinned toolchain alone, the
// three Go exporters, and the native-fee export from the image it builds in
// the directory, removes the Cargo target, records each export file's source
// commit and SHA-256, and writes an environment naming every export and the
// image's digest last. It reads the checkouts that other jobs share without
// writing their index, even when its stat information is stale.
func TestReleaseGateMainnetExportsBuildEveryExportOffline(t *testing.T) {
	t.Parallel()
	self := newReleaseGateMainnetExportsBuild(t)
	if err := os.WriteFile(filepath.Join(self.workspace, "server", "untracked.go"), []byte("package server\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(self.workspace, "sn", "README.md"), later, later); err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(self.workspace, "sn", ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	// The builder names its exports by the directory's path with no symbolic
	// link, which its native-fee image sees as its own.
	root, err := filepath.EvalSymlinks(self.root)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "exports")
	log, err := self.run(t, filepath.Join(self.root, "exports"), strconv.Itoa(releaseGateMainnetExportsRequiredKiB(t)))
	if err != nil {
		t.Fatalf("the builder failed: %v\n%s", err, log)
	}
	info, err := os.Lstat(output)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("the exports are not a private directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(output, "cargo-target")); !os.IsNotExist(err) {
		t.Fatalf("the builder kept its Cargo target: %v", err)
	}
	builder := readReleaseGateMainnetExportsBuilder(t)
	rust, err := releaseGateMainnetBuilderTable(builder, "rust_exports")
	if err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(self.workspace, "sn", "tools", "runtime-metadata-probe")
	target := filepath.Join(output, "cargo-target")
	var roots []string
	for _, row := range rust {
		roots = append(roots, "historical::tests::native_execution_tests::"+row[2])
	}
	var exporters []string
	for _, exporter := range releaseGateMainnetGoExporters {
		directory := filepath.Join(self.workspace, exporter.checkout)
		environment := ""
		for _, assignment := range strings.Fields(exporter.command) {
			if variable, value, found := strings.Cut(assignment, "="); found && strings.HasPrefix(variable, "URNETWORK_") {
				environment += variable + "=" + strings.ReplaceAll(strings.Trim(value, `"`), "$output", output) + " "
			}
		}
		_, arguments, _ := strings.Cut(exporter.command, " go ")
		exporters = append(exporters, "go "+strings.ReplaceAll(arguments, "'", "")+"|"+directory+"|"+environment)
	}
	native := releaseGateMainnetNativeFeeExporter
	image := filepath.Join(output, "native-fee-verifier", "mainnet.test")
	_, arguments, _ := strings.Cut(native.run, `"$verifier" `)
	exporters = append(exporters,
		strings.ReplaceAll(native.build, `"$verifier"`, image)+"|"+filepath.Join(self.workspace, "sn")+"|",
		"mainnet.test "+strings.ReplaceAll(arguments, "'", "")+"|"+filepath.Join(self.workspace, "sn", "mainnet")+"|"+native.variable+"="+filepath.Join(output, "native-fee")+" ")
	want := append([]string{
		"df -Pk -- " + self.root,
		"cargo " + releaseGateMainnetCargoBuild + "|" + probe + "|" + target + "|unset|0",
		"cargo " + releaseGateMainnetCargoTest + " " + strings.Join(roots, " ") + "|" + probe + "|" + target + "|unset|0",
	}, exporters...)
	if got := self.recorded(t); !slices.Equal(got, want) {
		t.Fatalf("the builder ran\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	exports, err := releaseGateMainnetBuilderTable(builder, "exports")
	if err != nil {
		t.Fatal(err)
	}
	environment, err := os.ReadFile(filepath.Join(output, "environment.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var wantEnvironment, wantManifest []string
	commits := map[string]string{"sn": self.commit(t, "sn"), "server": self.commit(t, "server") + "+modified"}
	for _, row := range exports {
		wantEnvironment = append(wantEnvironment, "export "+row[0]+"="+filepath.Join(output, row[1]))
		var files []string
		err := filepath.WalkDir(filepath.Join(output, strings.Split(row[1], "/")[0]), func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.Type().IsRegular() {
				files = append(files, path)
			}
			return err
		})
		if err != nil || len(files) == 0 {
			t.Fatalf("%s has no files: %v", row[0], err)
		}
		slices.Sort(files)
		for _, file := range files {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			relative, _ := filepath.Rel(output, file)
			wantManifest = append(wantManifest, row[0]+" "+row[2]+"@"+commits[row[2]]+" sha256:"+hex.EncodeToString(digest[:])+" "+relative)
		}
	}
	digests, err := releaseGateMainnetBuilderTable(builder, "digests")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range digests {
		raw, err := os.ReadFile(filepath.Join(output, row[1]))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		wantEnvironment = append(wantEnvironment, "export "+row[0]+"=sha256:"+hex.EncodeToString(digest[:]))
	}
	if info, err := os.Stat(image); err != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("the native-fee image is not read-only: %v", err)
	}
	if got := strings.Split(strings.TrimSuffix(string(environment), "\n"), "\n"); !slices.Equal(got, wantEnvironment) {
		t.Fatalf("the environment is\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(wantEnvironment, "\n"))
	}
	manifest, err := os.ReadFile(filepath.Join(output, "manifest"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(strings.TrimSuffix(string(manifest), "\n"), "\n"); !slices.Equal(got, wantManifest) || !strings.Contains(log, string(manifest)) {
		t.Fatalf("the manifest is\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(wantManifest, "\n"))
	}
	command := exec.Command("bash", "-c", `set -euo pipefail; source "$1"; for path in "${@:2}"; do [[ -e "${!path}" ]]; done`, "bash", filepath.Join(output, "environment.sh"))
	for _, row := range exports {
		command.Args = append(command.Args, row[0])
	}
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("the environment names an absent export: %v\n%s", err, raw)
	}
	if after, err := os.ReadFile(filepath.Join(self.workspace, "sn", ".git", "index")); err != nil || !slices.Equal(after, index) {
		t.Fatalf("the builder rewrote a shared checkout's index: %v", err)
	}
}

// With less free space than the build needs, the builder refuses before it
// creates the directory or runs a tool.
func TestReleaseGateMainnetExportsRefuseTooLittleFreeSpace(t *testing.T) {
	t.Parallel()
	self := newReleaseGateMainnetExportsBuild(t)
	output := filepath.Join(self.root, "exports")
	required := releaseGateMainnetExportsRequiredKiB(t)
	if required < 6*1024*1024 {
		t.Fatalf("the builder requires only %d KiB, below its measured 3.3 GiB peak twice", required)
	}
	for _, available := range []string{strconv.Itoa(required - 1), "0", "unknown"} {
		log, err := self.run(t, output, available)
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || log != "mainnet exports: "+available+" KiB free for "+output+", need "+strconv.Itoa(required)+" KiB; not building\n" {
			t.Fatalf("%s KiB free: %v\n%s", available, err, log)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("%s KiB free created the exports: %v", available, err)
		}
		if got := self.recorded(t); len(got) != 0 && slices.ContainsFunc(got, func(call string) bool { return !strings.HasPrefix(call, "df ") }) {
			t.Fatalf("%s KiB free ran %q", available, got)
		}
	}
}

// The builder takes exactly one new absolute directory.
func TestReleaseGateMainnetExportsRefuseAnExistingOrRelativeDirectory(t *testing.T) {
	t.Parallel()
	self := newReleaseGateMainnetExportsBuild(t)
	existing := filepath.Join(self.root, "existing")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{{}, {existing}, {"exports"}, {filepath.Join(self.root, "a"), filepath.Join(self.root, "b")}} {
		command := exec.Command("bash", append([]string{filepath.Join(self.workspace, "sn", "scripts", "build-mainnet-exports.sh")}, arguments...)...)
		command.Env = append(os.Environ(), "PATH="+filepath.Join(self.root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_CALLS="+self.calls, "FAKE_AVAILABLE=99999999999")
		raw, err := command.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 64 || string(raw) != "usage: build-mainnet-exports.sh NEW-ABSOLUTE-DIRECTORY\n" {
			t.Fatalf("%q: %v\n%s", arguments, err, raw)
		}
	}
	if got := self.recorded(t); len(got) != 0 {
		t.Fatalf("a refused directory ran %q", got)
	}
}

// A Rust root or the native-fee image that writes nothing, a failed Go
// exporter and a failed image build each fail the build, which then writes no
// environment.
func TestReleaseGateMainnetExportsRefuseAMissingOrFailedExport(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ extra, report string }{
		{"FAKE_SKIP=URNETWORK_NATIVE_YUMA_FIXTURE_OUT", "mainnet exports: URNETWORK_NATIVE_YUMA_FIXTURE_DIR is absent or empty: "},
		{"FAKE_SKIP=URNETWORK_NATIVE_EXECUTION_FIXTURE_OUT", "mainnet exports: URNETWORK_NATIVE_EXECUTION_FIXTURE is absent or empty: "},
		{"FAKE_FAIL=./payoutartifact", ""},
		{"FAKE_SKIP=URNETWORK_NATIVE_FEE_EXPORT_DIRECTORY", "mainnet exports: URNETWORK_NATIVE_FEE_SETTLEMENT_FIXTURE_DIRECTORY is absent or empty: "},
		{"FAKE_FAIL=-c", ""},
	} {
		self := newReleaseGateMainnetExportsBuild(t)
		output := filepath.Join(self.root, "exports")
		log, err := self.run(t, output, "99999999999", test.extra)
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(log, test.report) {
			t.Fatalf("%s: %v\n%s", test.extra, err, log)
		}
		for _, name := range []string{"environment.sh", "environment.sh.pending", "manifest"} {
			if _, err := os.Stat(filepath.Join(output, name)); !os.IsNotExist(err) {
				t.Fatalf("%s: the failed build wrote %s", test.extra, name)
			}
		}
	}
}
