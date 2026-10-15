package main

// Packages too large for one job's deadline run in their own shards:
// sn-all-normal leaves each of them out by package, and each shard runs the
// tests of the package's own list, in the shard's build mode, whose name's
// CRC-32 mod the shard count is its own, with the aggregate skip, in its job's
// own private temporaries on the storage root.

import (
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// One sharded package in one build mode, normal or race. Its jobs are job1 to
// jobN, running release_phase_phase1 to release_phase_phaseN, which call its
// owner with their index; the owner runs its preamble's lines, enters its
// module's directory, and runs the shard owner, after the environment prefix,
// with the package's count and skip list variables and its test run's flags,
// which follow -count=1. The package's sources are in source, beside
// sim-testnet.
type releaseGateShards struct {
	path, mode, job, phase, owner, countVariable, skipVariable, directory, environment, source string
	count                                                                                      int
	flags, preamble                                                                            []string
}

// The modules whose packages run in shards, as their owners enter them.
const (
	releaseGateSnDirectory      = `"$sn_repo"`
	releaseGateConnectDirectory = `"$workspace/connect"`
)

// Each ./mainnet shard's deadline is about twice the 255 minutes of measured
// serial test time of the heaviest shard. Its owner first requires the exports
// job's exit zero and reads that job's environment.
var releaseGateMainnetShards = releaseGateShards{
	path: "./mainnet", mode: "normal", job: "sn-mainnet-", phase: "sn_mainnet_", owner: "release_gate_mainnet_shard",
	countVariable: "release_gate_mainnet_shards", skipVariable: "release_gate_mainnet_skip_names",
	directory: releaseGateSnDirectory, source: "../mainnet",
	count: 8, flags: []string{"-timeout", "510m"},
	preamble: []string{
		`if [[ "${release_gate_mainnet_exports_exit:-}" != 0 ]]; then`,
		`  printf 'release gate: ./mainnet shard %s has no qualification exports: sn-mainnet-exports exit %s\n' "$1" "${release_gate_mainnet_exports_exit:-unknown}" >&2`,
		`  return 1`,
		`fi`,
		`source "$release_gate_mainnet_exports/environment.sh"`,
	},
}

// ./validator's normal and race shards share one skip list; each mode has its
// own count and deadline, about twice its heaviest shard's measured time.
var releaseGateValidatorNormalShards = releaseGateShards{
	path: "./validator", mode: "normal", job: "sn-validator-normal-", phase: "sn_validator_normal_", owner: "release_gate_validator_normal_shard",
	countVariable: "release_gate_validator_normal_shards", skipVariable: "release_gate_validator_skip_names",
	directory: releaseGateSnDirectory, source: "../validator",
	count: 2, flags: []string{"-parallel=4", "-timeout", "110m"},
}

var releaseGateValidatorRaceShards = releaseGateShards{
	path: "./validator", mode: "race", job: "sn-validator-race-", phase: "sn_validator_race_", owner: "release_gate_validator_race_shard",
	countVariable: "release_gate_validator_race_shards", skipVariable: "release_gate_validator_skip_names",
	directory: releaseGateSnDirectory, source: "../validator",
	count: 12, flags: []string{"-parallel=4", "-timeout", "110m"},
}

// ./sim-testnet's normal and race shards share one skip list, like
// ./validator's.
var releaseGateSimulatorNormalShards = releaseGateShards{
	path: "./sim-testnet", mode: "normal", job: "sn-simulator-normal-", phase: "sn_simulator_normal_", owner: "release_gate_simulator_normal_shard",
	countVariable: "release_gate_simulator_normal_shards", skipVariable: "release_gate_simulator_skip_names",
	directory: releaseGateSnDirectory, source: ".",
	count: 5, flags: []string{"-parallel=4", "-timeout", "80m"},
}

var releaseGateSimulatorRaceShards = releaseGateShards{
	path: "./sim-testnet", mode: "race", job: "sn-simulator-race-", phase: "sn_simulator_race_", owner: "release_gate_simulator_race_shard",
	countVariable: "release_gate_simulator_race_shards", skipVariable: "release_gate_simulator_skip_names",
	directory: releaseGateSnDirectory, source: ".",
	count: 17, flags: []string{"-parallel=4", "-timeout", "210m"},
}

// connect's race shards and their shuffled twins share one count, so the same
// partition, and one skip list; each has GOMAXPROCS 4, as the whole package's
// race runs had.
var releaseGateConnectRaceShards = releaseGateShards{
	path: "github.com/urnetwork/connect/v2026", mode: "race", job: "connect-race-", phase: "connect_race_", owner: "release_gate_connect_race_shard",
	countVariable: "release_gate_connect_race_shards", skipVariable: "release_gate_connect_skip_names",
	directory: releaseGateConnectDirectory, environment: "GOMAXPROCS=4 ", source: "../../connect",
	count: 15, flags: []string{"-timeout", "340m"},
}

var releaseGateConnectShuffleShards = releaseGateShards{
	path: "github.com/urnetwork/connect/v2026", mode: "race", job: "connect-shuffle-", phase: "connect_shuffle_", owner: "release_gate_connect_shuffle_shard",
	countVariable: "release_gate_connect_race_shards", skipVariable: "release_gate_connect_skip_names",
	directory: releaseGateConnectDirectory, environment: "GOMAXPROCS=4 ", source: "../../connect",
	count: 15, flags: []string{"-timeout", "340m", "-shuffle=4535211000"},
}

// Every sharded package and mode.
var releaseGateShardedPackages = []releaseGateShards{releaseGateMainnetShards, releaseGateValidatorNormalShards, releaseGateValidatorRaceShards, releaseGateSimulatorNormalShards, releaseGateSimulatorRaceShards, releaseGateConnectRaceShards, releaseGateConnectShuffleShards}

// The shard owner's two commands: the package's list in the shard's build
// mode, then the run of the shard's own tests with the aggregate skip.
const releaseGateShardListCommand = `selector="$(go test "${build[@]}" -list . "$package" | release_gate_shard_selector "$package" "$shard" "$shards" "$skips")" || return 1`

const releaseGateShardCommand = `go test "${build[@]}" "$package" -run "$selector" -count=1 "$@" -skip "$release_gate_aggregate_skip"`

// Linux's limit on one argument, its terminating NUL included
// (MAX_ARG_STRLEN). The test binary gets each selector in one argument.
const releaseGateArgumentLimit = 32 * 4096

// The package's owner's command of the shard owner.
func (self releaseGateShards) ownerCommand() string {
	return self.environment + "release_gate_shard " + self.path + " " + self.mode + ` "$1" "$` + self.countVariable + `" "$` + self.skipVariable + `" ` + strings.Join(self.flags, " ")
}

// The package's owner's whole body: its preamble, its module, the shard owner.
func (self releaseGateShards) ownerBody() string {
	body := ""
	for _, line := range append(append(slices.Clone(self.preamble), "cd "+self.directory), self.ownerCommand()) {
		body += "  " + line + "\n"
	}
	return body
}

// The arguments with which the fixture's go records the package's list.
func (self releaseGateShards) listCall() string {
	if self.mode == "race" {
		return "list test -race -list . " + self.path
	}
	return "list test -list . " + self.path
}

// The test run of a shard with this selector and aggregate skip.
func (self releaseGateShards) runArguments(selector, skip string) []string {
	arguments := []string{"test"}
	if self.mode == "race" {
		arguments = append(arguments, "-race")
	}
	arguments = append(arguments, self.path, "-run", selector, "-count=1")
	arguments = append(arguments, self.flags...)
	return append(arguments, "-skip", skip)
}

// Go's rule for a test, fuzz or benchmark name: the prefix, then nothing or a
// rune that is not lower case.
func releaseGateGoTestName(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	next, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return len(name) == len(prefix) || !unicode.IsLower(next)
}

// The names `go test -list .` prints for the package's build in the shards'
// mode, except benchmarks, which no -run executes: its tests, fuzz targets and
// examples with output.
func releaseGatePackageTestList(t *testing.T, shards releaseGateShards) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(shards.source, "*_test.go"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("%s test census: %d files, %v", shards.path, len(paths), err)
	}
	context := build.Default
	if shards.mode == "race" {
		context.BuildTags = append(slices.Clone(context.BuildTags), "race")
	}
	pointerTo := func(field *ast.Field, name string) bool {
		pointer, ok := field.Type.(*ast.StarExpr)
		if !ok {
			return false
		}
		selector, ok := pointer.X.(*ast.SelectorExpr)
		return ok && selector.Sel.Name == name
	}
	var names []string
	for _, path := range paths {
		if matched, err := context.MatchFile(filepath.Dir(path), filepath.Base(path)); err != nil || !matched {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || function.Type.Params == nil || function.Type.Results != nil && len(function.Type.Results.List) != 0 {
				continue
			}
			parameters, name := function.Type.Params.List, function.Name.Name
			if len(parameters) == 1 && len(parameters[0].Names) <= 1 && (releaseGateGoTestName(name, "Test") && name != "TestMain" && pointerTo(parameters[0], "T") || releaseGateGoTestName(name, "Fuzz") && pointerTo(parameters[0], "F")) {
				names = append(names, name)
			}
		}
		for _, example := range doc.Examples(file) {
			if example.Output != "" || example.EmptyOutput {
				names = append(names, "Example"+example.Name)
			}
		}
	}
	slices.Sort(names)
	return names
}

// Run the shard owners' own definitions in bash after the prelude, which
// stands in for the gate's job environment, then the given commands.
func runReleaseGateShardOwner(t *testing.T, prelude, commands string, arguments ...string) (string, error) {
	t.Helper()
	fragment := releaseGateLocalFragment(t, "release_gate_shard_selector() {", "release_phase_sn_mainnet_1() {")
	command := exec.Command("bash", append([]string{"-c", "set -euo pipefail\n" + prelude + "\n" + fragment + "\n" + commands, "bash"}, arguments...)...)
	output, err := command.CombinedOutput()
	return string(output), err
}

// A job environment, with GOMAXPROCS 2, whose go records each call: a -list
// answers with the list file, and a test run records its working directory,
// GOMAXPROCS, arguments and URNETWORK_ environment and passes. The ./mainnet
// exports job passed, and its environment names a path of the fixture for
// each export variable.
type releaseGateShardFixture struct {
	root, sn, workspace string
}

// The path the fixture's exports environment gives the variable.
func (self releaseGateShardFixture) export(variable string) string {
	return filepath.Join(self.root, "exports", strings.ToLower(variable))
}

func newReleaseGateShardFixture(t *testing.T, path string, names []string) releaseGateShardFixture {
	t.Helper()
	root := t.TempDir()
	fixture := releaseGateShardFixture{root: root, sn: filepath.Join(root, "sn"), workspace: filepath.Join(root, "workspace")}
	for _, directory := range []string{fixture.sn, filepath.Join(fixture.workspace, "connect")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// go test merges the test binary's stderr into its list output.
	list := "[env]glog not configured from command line. Using default configuration.\n" + strings.Join(names, "\n") + "\nok  \t" + path + "\t0.104s\n"
	if err := os.WriteFile(filepath.Join(root, "list"), []byte(list), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "exports"), 0o700); err != nil {
		t.Fatal(err)
	}
	var environment strings.Builder
	for _, variable := range releaseGateMainnetExportVariables(t) {
		environment.WriteString("export " + variable + "=" + strconv.Quote(fixture.export(variable)) + "\n")
	}
	if err := os.WriteFile(filepath.Join(root, "exports", "environment.sh"), []byte(environment.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (self releaseGateShardFixture) prelude(skip string) string {
	return `fixture=` + strconv.Quote(self.root) + `
sn_repo=` + strconv.Quote(self.sn) + `
workspace=` + strconv.Quote(self.workspace) + `
export GOMAXPROCS=2
release_gate_aggregate_skip='` + skip + `'
release_gate_mainnet_exports="$fixture/exports"
release_gate_mainnet_exports_exit=0
go() {
  if [[ " $* " == *" -list "* ]]; then
    printf 'list %s\n' "$*" >> "$fixture/calls"
    cat "$fixture/list"
    return
  fi
  printf '%s %s\n' "$PWD" "$GOMAXPROCS" > "$fixture/directory"
  printf '%s\n' "$@" > "$fixture/arguments"
  { env | grep '^URNETWORK_' || true; } | LC_ALL=C sort > "$fixture/environment"
}
`
}

// The shard's go calls: its lists, then its test run's arguments, which must
// run in the shards' module with their GOMAXPROCS.
func (self releaseGateShardFixture) calls(t *testing.T, shards releaseGateShards) ([]string, []string) {
	t.Helper()
	lists, err := os.ReadFile(filepath.Join(self.root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	run, err := os.ReadFile(filepath.Join(self.root, "arguments"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{releaseGateSnDirectory: self.sn, releaseGateConnectDirectory: filepath.Join(self.workspace, "connect")}[shards.directory] + " 2\n"
	if shards.environment == "GOMAXPROCS=4 " {
		want = strings.Replace(want, " 2\n", " 4\n", 1)
	} else if shards.environment != "" {
		t.Fatalf("unreviewed shard environment %q", shards.environment)
	}
	if directory, err := os.ReadFile(filepath.Join(self.root, "directory")); err != nil || string(directory) != want {
		t.Fatalf("the shard ran go in %q, want %q: %v", directory, want, err)
	}
	return strings.Split(strings.TrimSuffix(string(lists), "\n"), "\n"), strings.Split(strings.TrimSuffix(string(run), "\n"), "\n")
}

// The names of one printed selector, which must be exactly ^(a|b|...)$.
func releaseGateSelectorNames(selector string) ([]string, error) {
	if !strings.HasPrefix(selector, "^(") || !strings.HasSuffix(selector, ")$") {
		return nil, fmt.Errorf("selector %q is not one anchored alternation", selector)
	}
	return strings.Split(selector[2:len(selector)-2], "|"), nil
}

// Every name of the list is in exactly one shard's selector, as Go matches it,
// and each shard's selector names only its own tests, at least one, in one
// argument of the test binary.
func verifyReleaseGateShardPartition(names []string, selectors []string) error {
	owners := map[string]int{}
	for index, selector := range selectors {
		members, err := releaseGateSelectorNames(selector)
		if err != nil {
			return err
		}
		if len(members) == 0 || members[0] == "" {
			return fmt.Errorf("shard %d selects no test", index+1)
		}
		if len("-test.run=")+len(selector) >= releaseGateArgumentLimit {
			return fmt.Errorf("shard %d's selector of %d bytes exceeds one argument", index+1, len(selector))
		}
		compiled, err := regexp.Compile(selector)
		if err != nil {
			return err
		}
		for _, member := range members {
			if !slices.Contains(names, member) {
				return fmt.Errorf("shard %d selects %s, which the list does not name", index+1, member)
			}
		}
		for _, name := range names {
			if compiled.MatchString(name) {
				owners[name]++
			}
		}
	}
	for _, name := range names {
		if owners[name] != 1 {
			return fmt.Errorf("%s runs in %d shards, want exactly one", name, owners[name])
		}
	}
	return nil
}

// The selector each shard of the package prints for the list, from the gate's
// own function.
func releaseGateShardSelectors(t *testing.T, path string, names []string, shards int, skips ...string) ([]string, error) {
	t.Helper()
	fixture := newReleaseGateShardFixture(t, path, names)
	var selectors []string
	for shard := 1; shard <= shards; shard++ {
		output, err := runReleaseGateShardOwner(t, "", `release_gate_shard_selector "$1" "$2" "$3" "$4" < "$5"`, path, strconv.Itoa(shard), strconv.Itoa(shards), strings.Join(skips, " "), filepath.Join(fixture.root, "list"))
		if err != nil {
			return nil, fmt.Errorf("%s shard %d of %d: %v: %s", path, shard, shards, err, output)
		}
		selectors = append(selectors, output)
	}
	return selectors, nil
}

// The package's list for the shards' mode, with at least the given number of
// tests, partitioned completely and disjointly by their selectors.
func verifyReleaseGateShardsPartition(t *testing.T, shards releaseGateShards, least int) ([]string, []string) {
	t.Helper()
	names := releaseGatePackageTestList(t, shards)
	if len(names) < least {
		t.Fatalf("%s lists only %d tests", shards.path, len(names))
	}
	selectors, err := releaseGateShardSelectors(t, shards.path, names, shards.count)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseGateShardPartition(names, selectors); err != nil {
		t.Fatalf("%s %s: %v", shards.mode, shards.path, err)
	}
	var sizes []int
	for _, selector := range selectors {
		members, _ := releaseGateSelectorNames(selector)
		sizes = append(sizes, len(members))
	}
	t.Logf("%s %s: %d tests in shards of %v", shards.mode, shards.path, len(names), sizes)
	return names, selectors
}

// The shard owner ends in exactly its two reviewed go test commands, which are
// its only ones, and leaves the directory to its caller.
func verifyReleaseGateShardOwner(script string) error {
	owner := regexp.MustCompile(`(?ms)^release_gate_shard\(\) \{\n(.*?)^\}$`).FindAllStringSubmatch(script, -1)
	tail := "\n  " + releaseGateShardListCommand + "\n  " + releaseGateShardCommand + "\n"
	if len(owner) != 1 || strings.Count(owner[0][1], "go test ") != 2 || strings.Contains(owner[0][1], "cd ") || !strings.HasSuffix(owner[0][1], tail) {
		return errors.New("the shard owner does not end in exactly its two reviewed go test commands")
	}
	return nil
}

// The shards are exactly job1 to jobN, each admitted once, unconditionally,
// running the package's owner with its own index, and the owner runs only the
// shard owner, with the package's mode, count, skip list and flags, the
// deadline among them.
func verifyReleaseGateShardJobs(script string, shards releaseGateShards) error {
	if strings.Count(script, "\n"+shards.countVariable+"="+strconv.Itoa(shards.count)+"\n") != 1 || strings.Count(script, shards.countVariable+"=") != 1 {
		return fmt.Errorf("the gate does not run exactly %d %s %s shards", shards.count, shards.mode, shards.path)
	}
	for shard := 1; shard <= shards.count; shard++ {
		index := strconv.Itoa(shard)
		if err := verifyReleaseGateExactPhase(script, shards.job+index, shards.phase+index, []string{shards.owner + " " + index}); err != nil {
			return err
		}
	}
	calls := regexp.MustCompile(`(?m)^[\t ]*`+regexp.QuoteMeta(shards.owner)+`[\t ]`).FindAllString(script, -1)
	if strings.Contains(script, "release_phase_"+shards.phase+strconv.Itoa(shards.count+1)) || len(calls) != shards.count {
		return fmt.Errorf("the gate runs a %s %s shard outside %s1 to %s%d", shards.mode, shards.path, shards.job, shards.job, shards.count)
	}
	owner := regexp.MustCompile(`(?ms)^`+regexp.QuoteMeta(shards.owner)+`\(\) \{\n(.*?)^\}$`).FindAllStringSubmatch(script, -1)
	if len(owner) != 1 || owner[0][1] != shards.ownerBody() {
		return fmt.Errorf("%s does not run exactly its reviewed shard owner", shards.owner)
	}
	return verifyReleaseGateShardOwner(script)
}

// Outside the shards, every go test command of the shards' mode that names the
// package selects its tests with -run, so only the shards run it whole.
func verifyReleaseGateShardsRunTheWholePackage(script string, shards releaseGateShards) error {
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "go test ") {
			continue
		}
		fields := strings.Fields(line)
		if slices.Contains(fields, shards.path) && slices.Contains(fields, "-race") == (shards.mode == "race") && !slices.Contains(fields, "-run") {
			return fmt.Errorf("%s runs %s whole outside its %s shards", line, shards.path, shards.mode)
		}
	}
	return nil
}

// Each mode's first and last shard list the package in the shards' build mode
// and run their own selector of its list with that mode's aggregate skip and
// the shards' flags, the deadline among them, and nothing else.
func verifyReleaseGateShardRuns(t *testing.T, shards releaseGateShards) {
	t.Helper()
	names := releaseGatePackageTestList(t, shards)
	selectors, err := releaseGateShardSelectors(t, shards.path, names, shards.count)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"pinned", "local"} {
		skip := releaseGateAggregateSkipFor(t, mode)
		for _, shard := range []int{1, shards.count} {
			fixture := newReleaseGateShardFixture(t, shards.path, names)
			if output, err := runReleaseGateShardOwner(t, fixture.prelude(skip), shards.owner+` "$1"`, strconv.Itoa(shard)); err != nil {
				t.Fatalf("%s %s %s shard %d failed: %v\n%s", mode, shards.mode, shards.path, shard, err, output)
			}
			lists, run := fixture.calls(t, shards)
			if want := shards.runArguments(selectors[shard-1], skip); !slices.Equal(lists, []string{shards.listCall()}) || !slices.Equal(run, want) {
				t.Fatalf("%s %s %s shard %d listed with %q and ran go %q, want %q", mode, shards.mode, shards.path, shard, lists, run, want)
			}
		}
	}
}

// The census refuses a shard running another's index, an omitted or
// conditional admission, another count, and an owner or shard owner whose
// deadline, mode, selection or fresh execution changed.
func verifyReleaseGateShardJobsRejectDrift(t *testing.T, script string, shards releaseGateShards) {
	t.Helper()
	last := strconv.Itoa(shards.count)
	command := shards.ownerCommand()
	deadline := slices.Index(shards.flags, "-timeout")
	if deadline < 0 || deadline+1 == len(shards.flags) {
		t.Fatalf("%s %s shards have no deadline", shards.mode, shards.path)
	}
	timeout := "-timeout " + shards.flags[deadline+1]
	for _, change := range []struct{ old, new string }{
		{"release_phase_" + shards.phase + last + "() {\n  " + shards.owner + " " + last + "\n", "release_phase_" + shards.phase + last + "() {\n  " + shards.owner + " 1\n"},
		{"release_gate_start " + shards.job + "1 release_phase_" + shards.phase + "1\n", ""},
		{"release_gate_start " + shards.job + last + " release_phase_" + shards.phase + last + "\n", "if [[ \"$release_gate_dependency_mode\" == pinned ]]; then\nrelease_gate_start " + shards.job + last + " release_phase_" + shards.phase + last + "\nfi\n"},
		{"\n" + shards.countVariable + "=" + last + "\n", "\n" + shards.countVariable + "=" + strconv.Itoa(shards.count+1) + "\n"},
		{command, strings.Replace(command, " "+timeout, "", 1)},
		{command, strings.Replace(command, timeout, "-timeout 10m", 1)},
		{command, strings.Replace(command, timeout, "-timeout 0", 1)},
		{command, command + " -run '^TestNone$'"},
		{command, strings.Replace(command, " "+shards.mode+" ", " fast ", 1)},
		{"  cd " + shards.directory + "\n  " + command + "\n", "  cd \"$workspace/server\"\n  " + command + "\n"},
		{"  cd " + shards.directory + "\n  " + command + "\n", "  " + command + "\n"},
		{releaseGateShardCommand, strings.Replace(releaseGateShardCommand, " -count=1", "", 1)},
		{releaseGateShardCommand, releaseGateShardCommand + " || true"},
		{releaseGateShardListCommand, strings.Replace(releaseGateShardListCommand, `"${build[@]}" `, "", 1)},
	} {
		changed := strings.Replace(script, change.old, change.new, 1)
		if changed == script {
			t.Fatalf("the mutation names no text: %q", change.old)
		}
		if verifyReleaseGateShardJobs(changed, shards) == nil {
			t.Fatalf("the %s %s shard census accepted %q", shards.mode, shards.path, change.new)
		}
	}
	if shards.environment != "" && verifyReleaseGateShardJobs(strings.Replace(script, command, strings.TrimPrefix(command, shards.environment), 1), shards) == nil {
		t.Fatalf("the %s %s shard census accepted its owner without %q", shards.mode, shards.path, shards.environment)
	}
}

// The named skip list is the package's one reviewed empty assignment, and a
// name it gains leaves every shard while the rest stay complete and disjoint;
// a name that is no test of the package's list is refused, so a renamed test
// cannot vanish silently, and the shards print the names they skip.
func verifyReleaseGateShardsSkipOnlyNamedTests(t *testing.T, script string, shards releaseGateShards) {
	t.Helper()
	if strings.Count(script, "\n"+shards.skipVariable+"=''\n") != 1 || strings.Count(script, shards.skipVariable+"=") != 1 {
		t.Fatalf("%s is not the one reviewed empty assignment", shards.skipVariable)
	}
	names := releaseGatePackageTestList(t, shards)
	skipped := names[len(names)/2]
	selectors, err := releaseGateShardSelectors(t, shards.path, names, shards.count, skipped)
	if err != nil {
		t.Fatal(err)
	}
	rest := slices.DeleteFunc(slices.Clone(names), func(name string) bool { return name == skipped })
	if err := verifyReleaseGateShardPartition(rest, selectors); err != nil {
		t.Fatal(err)
	}
	for _, selector := range selectors {
		if regexp.MustCompile(selector).MatchString(skipped) {
			t.Fatalf("a %s %s shard still runs the named skip %s", shards.mode, shards.path, skipped)
		}
	}
	if _, err := releaseGateShardSelectors(t, shards.path, names, shards.count, skipped+"Renamed"); err == nil {
		t.Fatalf("the %s %s shards accepted a named skip that is no test", shards.mode, shards.path)
	}
	fixture := newReleaseGateShardFixture(t, shards.path, names)
	output, err := runReleaseGateShardOwner(t, fixture.prelude("skip"), shards.skipVariable+"="+strconv.Quote(skipped)+"\n"+shards.owner+" 1")
	if err != nil || !strings.Contains(output, "[release-1.0] "+shards.path+" shards skip by name: "+skipped+"\n") {
		t.Fatalf("a %s %s shard with a named skip did not print it: %v\n%s", shards.mode, shards.path, err, output)
	}
}

// The go test arguments of sn-all-normal's body for go list's packages.
func runReleaseGateAggregateBody(t *testing.T, script string, packages []string) []string {
	t.Helper()
	commands, _, err := releaseGatePhaseCommands(script, "sn_all_normal")
	if err != nil || !slices.Equal(commands, releaseGateAggregateNormalBody) {
		t.Fatalf("sn-all-normal runs %q, %v; want %q", commands, err, releaseGateAggregateNormalBody)
	}
	root := t.TempDir()
	command := exec.Command("bash", "-c", `set -euo pipefail
release_gate_aggregate_skip=skip
go() {
  if [[ "$1" == list ]]; then printf '%s\n' `+strings.Join(packages, " ")+`; return; fi
  printf '%s\n' "$@" > "$0/arguments"
}
`+strings.Join(commands[1:], "\n"), root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sn-all-normal's body failed: %v\n%s", err, output)
	}
	raw, err := os.ReadFile(filepath.Join(root, "arguments"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

// sn-all-normal runs every sn package but the sharded ones, by exact import
// path, keeping their siblings and subpackages, and refuses to lose the
// package's exclusion.
func verifyReleaseGateShardedPackageLeavesTheAggregateJob(t *testing.T, path string) {
	t.Helper()
	script := readReleaseGateLocalScript(t)
	sharded := []string{"github.com/urfoundation/sn/v2026/mainnet", "github.com/urfoundation/sn/v2026/validator", "github.com/urfoundation/sn/v2026/sim-testnet"}
	packages := []string{"github.com/urfoundation/sn/v2026/crv4"}
	for _, excluded := range sharded {
		packages = append(packages, excluded, excluded+"/sub", excluded+"x")
	}
	want := []string{"test", "-parallel=4", "-timeout", "90m"}
	want = append(want, slices.DeleteFunc(slices.Clone(packages), func(path string) bool { return slices.Contains(sharded, path) })...)
	want = append(want, "-count=1", "-skip", "skip")
	if got := runReleaseGateAggregateBody(t, script, packages); !slices.Equal(got, want) {
		t.Fatalf("sn-all-normal ran go %q, want %q", got, want)
	}
	name := strings.TrimPrefix(path, "./")
	if !slices.Contains(sharded, "github.com/urfoundation/sn/v2026/"+name) {
		t.Fatalf("%s is no sharded package", path)
	}
	census, err := releaseGateAggregateSkipCensus("..")
	if err != nil {
		t.Fatal(err)
	}
	const exclusion = "(mainnet|validator|sim-testnet)"
	kept := strings.Join(slices.DeleteFunc([]string{"mainnet", "validator", "sim-testnet"}, func(other string) bool { return other == name }), "|")
	for _, change := range []struct{ old, new string }{
		{exclusion, "(" + kept + ")"},
		{exclusion, strings.Replace(exclusion, name, name+".*", 1)},
		{`grep -Evx`, `grep -Ev`},
		{`  [[ -n "$sn_package_list" ]]` + "\n", ""},
		{releaseGateAggregateNormalCommand, `go test -parallel=4 -timeout 90m ./... -count=1 -skip "$release_gate_aggregate_skip"`},
	} {
		if strings.Count(script, change.old) != 1 || change.old == change.new {
			t.Fatalf("the mutation names no unique text: %s", change.old)
		}
		if err := verifyReleaseGateAggregateE2ESkip(strings.Replace(script, change.old, change.new, 1), census); err == nil {
			t.Fatalf("sn-all-normal accepted %q", change.new)
		}
	}
}

// The ./mainnet partition is complete and disjoint for its current list:
// every test, fuzz target and example runs in exactly one of the eight shards,
// none is empty, and go test's other output lines select nothing.
func TestReleaseGateMainnetShardsPartitionTheTestList(t *testing.T) {
	t.Parallel()
	names, selectors := verifyReleaseGateShardsPartition(t, releaseGateMainnetShards, 1000)
	// A selector that loses its last shard or invents a name fails.
	if verifyReleaseGateShardPartition(names, selectors[:len(selectors)-1]) == nil || verifyReleaseGateShardPartition(names, append(slices.Clone(selectors[1:]), "^("+names[0]+"|TestInvented)$")) == nil {
		t.Fatal("the partition check accepted a lost shard or an invented name")
	}
	// So does a selector too long for one argument of the test binary.
	long := strings.TrimSuffix(selectors[0], ")$") + strings.Repeat("|"+names[0], releaseGateArgumentLimit/len(names[0])) + ")$"
	if verifyReleaseGateShardPartition(names, append(slices.Clone(selectors[1:]), long)) == nil {
		t.Fatal("the partition check accepted a selector longer than one argument")
	}
	// Names Go would list but that no -run could match by an ASCII
	// alternation, and an empty list, are refused, not silently dropped.
	for _, list := range [][]string{append(slices.Clone(names), "TestÜbernahme"), append(slices.Clone(names), "Test-dash"), {}} {
		if _, err := releaseGateShardSelectors(t, "./mainnet", list, releaseGateMainnetShards.count); err == nil {
			t.Fatalf("the shard selector accepted the list ending %q", list[max(len(list)-1, 0):])
		}
	}
}

// ./validator's partitions are complete and disjoint in both modes: each of
// its tests runs in exactly one normal and exactly one race shard.
func TestReleaseGateValidatorShardsPartitionTheTestList(t *testing.T) {
	t.Parallel()
	verifyReleaseGateShardsPartition(t, releaseGateValidatorNormalShards, 1000)
	verifyReleaseGateShardsPartition(t, releaseGateValidatorRaceShards, 1000)
}

// ./sim-testnet's partitions are complete and disjoint in both modes: each of
// its tests runs in exactly one normal and exactly one race shard.
func TestReleaseGateSimulatorShardsPartitionTheTestList(t *testing.T) {
	t.Parallel()
	verifyReleaseGateShardsPartition(t, releaseGateSimulatorNormalShards, 1000)
	verifyReleaseGateShardsPartition(t, releaseGateSimulatorRaceShards, 1000)
}

// sn-all-normal runs every sn package but ./mainnet and the other sharded
// packages, by package: its list drops exactly their import paths, keeping
// their siblings and subpackages. ./mainnet runs whole only in its shards,
// whose owner alone names it beside its refusal without the exports; sn-e2e
// runs selected roots.
func TestReleaseGateMainnetLeavesTheAggregateJob(t *testing.T) {
	t.Parallel()
	verifyReleaseGateShardedPackageLeavesTheAggregateJob(t, "./mainnet")
	script := readReleaseGateLocalScript(t)
	var mainnet []string
	for _, line := range strings.Split(script, "\n") {
		if line = strings.TrimSpace(line); !strings.HasPrefix(line, "#") && strings.Contains(line, "./mainnet") {
			mainnet = append(mainnet, line)
		}
	}
	if want := []string{
		strings.TrimSpace(releaseGateMainnetShards.preamble[1]),
		releaseGateMainnetShards.ownerCommand(),
		`go test ./mainnet -run "$settlement_v2_e2e_tests" -count=1 -timeout 30m`,
		`go test -race ./mainnet -run "$settlement_v2_e2e_tests" -count=1 -timeout 30m`,
		`go test ./mainnet -run "$stmonitor_fixture_tests" -count=1`,
		`go test -race ./mainnet -run "$stmonitor_fixture_tests" -count=1`,
	}; !slices.Equal(mainnet, want) {
		t.Fatalf("the gate runs ./mainnet in %q, want %q", mainnet, want)
	}
	if err := verifyReleaseGateShardsRunTheWholePackage(script, releaseGateMainnetShards); err != nil {
		t.Fatal(err)
	}
}

// sn-all-normal leaves ./validator to its shards, and no other job runs the
// whole package in either mode.
func TestReleaseGateValidatorLeavesTheAggregateJob(t *testing.T) {
	t.Parallel()
	verifyReleaseGateShardedPackageLeavesTheAggregateJob(t, "./validator")
	script := readReleaseGateLocalScript(t)
	for _, shards := range []releaseGateShards{releaseGateValidatorNormalShards, releaseGateValidatorRaceShards} {
		if err := verifyReleaseGateShardsRunTheWholePackage(script, shards); err != nil {
			t.Fatal(err)
		}
		race := map[string]string{"normal": "", "race": "-race "}[shards.mode]
		if verifyReleaseGateShardsRunTheWholePackage(script+"\ngo test "+race+"./validator -count=1\n", shards) == nil {
			t.Fatalf("the gate accepted another whole %s run of ./validator", shards.mode)
		}
	}
}

// sn-all-normal leaves ./sim-testnet to its shards, and no other job runs the
// whole package in either mode.
func TestReleaseGateSimulatorLeavesTheAggregateJob(t *testing.T) {
	t.Parallel()
	verifyReleaseGateShardedPackageLeavesTheAggregateJob(t, "./sim-testnet")
	script := readReleaseGateLocalScript(t)
	for _, shards := range []releaseGateShards{releaseGateSimulatorNormalShards, releaseGateSimulatorRaceShards} {
		if err := verifyReleaseGateShardsRunTheWholePackage(script, shards); err != nil {
			t.Fatal(err)
		}
		race := map[string]string{"normal": "", "race": "-race "}[shards.mode]
		if verifyReleaseGateShardsRunTheWholePackage(script+"\ngo test "+race+"./sim-testnet -count=1 -skip '^TestNone$'\n", shards) == nil {
			t.Fatalf("the gate accepted another whole %s run of ./sim-testnet", shards.mode)
		}
	}
}

// connect's race partition is complete and disjoint: each test of its race
// build runs in exactly one race shard, and in the same shard of the shuffled
// ordering. The aggregate skip, which every shard runs, names no connect test
// in either mode.
func TestReleaseGateConnectShardsPartitionTheTestList(t *testing.T) {
	t.Parallel()
	names, selectors := verifyReleaseGateShardsPartition(t, releaseGateConnectRaceShards, 1000)
	shuffled, err := releaseGateShardSelectors(t, releaseGateConnectShuffleShards.path, releaseGatePackageTestList(t, releaseGateConnectShuffleShards), releaseGateConnectShuffleShards.count)
	if err != nil || !slices.Equal(shuffled, selectors) {
		t.Fatalf("the shuffled shards select %d other tests: %v", len(shuffled), err)
	}
	for _, mode := range []string{"pinned", "local"} {
		skip := regexp.MustCompile(releaseGateAggregateSkipFor(t, mode))
		for _, name := range append(names, releaseGatePackageTestList(t, releaseGateShards{source: releaseGateConnectRaceShards.source, mode: "normal", path: releaseGateConnectRaceShards.path})...) {
			if skip.MatchString(name) {
				t.Fatalf("the %s aggregate skip names connect's %s", mode, name)
			}
		}
	}
}

// Outside its shards, no job runs connect's whole package under race: every
// race command of a package in the current directory selects its tests with
// -run, and connect's one unsharded run is its normal one.
func verifyReleaseGateConnectRacesOnlyInShards(script string) error {
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "GOMAXPROCS=4 "))
		if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, "go test ") {
			continue
		}
		if fields := strings.Fields(line); slices.Contains(fields, "-race") && slices.Contains(fields, ".") && !slices.Contains(fields, "-run") {
			return fmt.Errorf("%s runs a whole package under race outside its shards", line)
		}
	}
	commands, _, err := releaseGatePhaseCommands(script, "connect_all_normal")
	if err != nil || !slices.Equal(commands, []string{"cd " + releaseGateConnectDirectory, releaseGateConnectPackageJobs[0].command}) {
		return fmt.Errorf("connect-all-normal runs %q, %v", commands, err)
	}
	return nil
}

// The connect race shards are exactly connect-race-1 to -N and their shuffled
// twins connect-shuffle-1 to -N, each admitted once, unconditionally; each
// lists connect's race build and runs its own tests under race, with
// GOMAXPROCS 4, the aggregate skip and the shards' deadline, and the shuffled
// twins with the fixed seed.
func TestReleaseGateConnectShardsRunTheSameSkips(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	for _, shards := range []releaseGateShards{releaseGateConnectRaceShards, releaseGateConnectShuffleShards} {
		if err := verifyReleaseGateShardJobs(script, shards); err != nil {
			t.Fatal(err)
		}
		verifyReleaseGateShardRuns(t, shards)
		verifyReleaseGateShardJobsRejectDrift(t, script, shards)
	}
	if err := verifyReleaseGateConnectRacesOnlyInShards(script); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"GOMAXPROCS=4 go test -race -count=1 -timeout 30m .", "GOMAXPROCS=4 go test -race -count=1 -timeout 30m -shuffle=4535211000 ."} {
		if verifyReleaseGateConnectRacesOnlyInShards(script+"\n"+command+"\n") == nil {
			t.Fatalf("the gate accepted connect's whole race run %q", command)
		}
	}
	shuffle := releaseGateConnectShuffleShards.ownerCommand()
	if verifyReleaseGateShardJobs(strings.Replace(script, shuffle, strings.Replace(shuffle, " -shuffle=4535211000", " -shuffle=on", 1), 1), releaseGateConnectShuffleShards) == nil {
		t.Fatal("the shuffled shards accepted another seed")
	}
}

// connect's shards' one named skip list is empty, and takes only test names.
func TestReleaseGateConnectShardsSkipOnlyNamedTests(t *testing.T) {
	t.Parallel()
	verifyReleaseGateShardsSkipOnlyNamedTests(t, readReleaseGateLocalScript(t), releaseGateConnectRaceShards)
}

// The ./mainnet shards are exactly sn-mainnet-1 to sn-mainnet-8, each admitted
// once, unconditionally, running the package's owner with its own index, whose
// test run has the aggregate skip of the gate's mode, the same for every
// shard, and the shards' deadline.
func TestReleaseGateMainnetShardsRunTheSameSkips(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	if err := verifyReleaseGateShardJobs(script, releaseGateMainnetShards); err != nil {
		t.Fatal(err)
	}
	verifyReleaseGateShardRuns(t, releaseGateMainnetShards)
	verifyReleaseGateShardJobsRejectDrift(t, script, releaseGateMainnetShards)
}

// The ./validator shards are exactly sn-validator-normal-1 to -N and
// sn-validator-race-1 to -M, each admitted once, unconditionally; each lists
// the package in its own build mode and runs its own tests in that mode with
// the aggregate skip of the gate's mode and its mode's deadline.
func TestReleaseGateValidatorShardsRunTheSameSkips(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	for _, shards := range []releaseGateShards{releaseGateValidatorNormalShards, releaseGateValidatorRaceShards} {
		if err := verifyReleaseGateShardJobs(script, shards); err != nil {
			t.Fatal(err)
		}
		verifyReleaseGateShardRuns(t, shards)
		verifyReleaseGateShardJobsRejectDrift(t, script, shards)
	}
}

// The ./sim-testnet shards are exactly sn-simulator-normal-1 to -N and
// sn-simulator-race-1 to -M, each admitted once, unconditionally; each lists
// the package in its own build mode and runs its own tests in that mode with
// the aggregate skip of the gate's mode, which in local mode skips its two
// named tests in every shard, and its mode's deadline.
func TestReleaseGateSimulatorShardsRunTheSameSkips(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	for _, shards := range []releaseGateShards{releaseGateSimulatorNormalShards, releaseGateSimulatorRaceShards} {
		if err := verifyReleaseGateShardJobs(script, shards); err != nil {
			t.Fatal(err)
		}
		verifyReleaseGateShardRuns(t, shards)
		verifyReleaseGateShardJobsRejectDrift(t, script, shards)
	}
}

// An unknown or mistyped shard or shard count of any sharded package, and an
// unknown build mode, are refused before any test is listed or run.
func TestReleaseGateMainnetShardRefusesAnUnknownShard(t *testing.T) {
	t.Parallel()
	cases := []struct{ count, shard string }{
		{"8", "0"}, {"8", "9"}, {"8", "08"}, {"8", "1x"}, {"8", ""}, {"8", "-1"}, {"8", "+1"}, {"8", " 1"},
		{"8x", "1"}, {"0", "1"}, {"", "1"}, {"08", "1"}, {"100", "1"}, {"-8", "1"},
	}
	for _, shards := range releaseGateShardedPackages {
		names := releaseGatePackageTestList(t, shards)
		for _, change := range cases {
			fixture := newReleaseGateShardFixture(t, shards.path, names)
			output, err := runReleaseGateShardOwner(t, fixture.prelude("skip"), shards.countVariable+"="+strconv.Quote(change.count)+"\n"+shards.owner+` "$1"`, change.shard)
			if err == nil || !strings.Contains(output, "unknown "+shards.path+" shard ") {
				t.Fatalf("%s shard %q of %q ran: %v\n%s", shards.path, change.shard, change.count, err, output)
			}
			if _, err := os.Stat(filepath.Join(fixture.root, "calls")); !os.IsNotExist(err) {
				t.Fatalf("%s shard %q of %q listed tests", shards.path, change.shard, change.count)
			}
		}
		for _, mode := range []string{"", "Race", "race ", "fast"} {
			fixture := newReleaseGateShardFixture(t, shards.path, names)
			output, err := runReleaseGateShardOwner(t, fixture.prelude("skip"), `release_gate_shard "$1" "$2" 1 "$3" ''`, shards.path, mode, strconv.Itoa(shards.count))
			if err == nil || !strings.Contains(output, "unknown "+shards.path+" shard mode") {
				t.Fatalf("%s shards ran in mode %q: %v\n%s", shards.path, mode, err, output)
			}
			if _, err := os.Stat(filepath.Join(fixture.root, "calls")); !os.IsNotExist(err) {
				t.Fatalf("%s shards in mode %q listed tests", shards.path, mode)
			}
		}
	}
}

// A shard whose list fails, or whose selection refuses its list, returns an
// error before any test runs, even where its caller's errexit does not reach
// it.
func TestReleaseGateShardRunsNoTestAfterAFailedList(t *testing.T) {
	t.Parallel()
	for _, shards := range releaseGateShardedPackages {
		names := releaseGatePackageTestList(t, shards)
		for _, failure := range []string{
			shards.skipVariable + "=" + strconv.Quote(names[0]+"Renamed"),
			`go() { if [[ " $* " == *" -list "* ]]; then return 2; fi; printf '%s\n' "$@" > "$fixture/arguments"; }`,
			`go() { if [[ " $* " == *" -list "* ]]; then return 0; fi; printf '%s\n' "$@" > "$fixture/arguments"; }`,
		} {
			fixture := newReleaseGateShardFixture(t, shards.path, names)
			output, err := runReleaseGateShardOwner(t, fixture.prelude("skip"), failure+"\nstatus=0\n"+shards.owner+" 1 || status=$?\n(( status != 0 ))")
			if err != nil {
				t.Fatalf("%s %s shard 1 passed after %q: %v\n%s", shards.mode, shards.path, failure, err, output)
			}
			if _, err := os.Stat(filepath.Join(fixture.root, "arguments")); !os.IsNotExist(err) {
				t.Fatalf("%s %s shard 1 ran tests after %q", shards.mode, shards.path, failure)
			}
		}
	}
}

// The ./mainnet shards' named skip list is empty, and takes only test names.
func TestReleaseGateMainnetShardsSkipOnlyNamedTests(t *testing.T) {
	t.Parallel()
	verifyReleaseGateShardsSkipOnlyNamedTests(t, readReleaseGateLocalScript(t), releaseGateMainnetShards)
}

// The ./validator shards' one named skip list, for both modes, is empty, and
// takes only test names.
func TestReleaseGateValidatorShardsSkipOnlyNamedTests(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	verifyReleaseGateShardsSkipOnlyNamedTests(t, script, releaseGateValidatorNormalShards)
	verifyReleaseGateShardsSkipOnlyNamedTests(t, script, releaseGateValidatorRaceShards)
}

// The ./sim-testnet shards' one named skip list, for both modes, is empty, and
// takes only test names.
func TestReleaseGateSimulatorShardsSkipOnlyNamedTests(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	verifyReleaseGateShardsSkipOnlyNamedTests(t, script, releaseGateSimulatorNormalShards)
	verifyReleaseGateShardsSkipOnlyNamedTests(t, script, releaseGateSimulatorRaceShards)
}
