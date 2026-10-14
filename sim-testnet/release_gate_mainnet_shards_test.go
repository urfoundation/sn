package main

// ./mainnet runs in its own shards: sn-all-normal leaves it out by package,
// and each shard runs the tests of the package's own list whose name's CRC-32
// mod the shard count is its own, with the aggregate skip, in its job's own
// private temporaries on the storage root.

import (
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

const releaseGateMainnetShardCount = 8

// The shard owner's one test command.
const releaseGateMainnetShardCommand = `go test ./mainnet -run "$selector" -count=1 -timeout ` + releaseGateMainnetShardTimeout + ` -skip "$release_gate_aggregate_skip"`

// Each shard's package deadline: about twice the 255 minutes of measured
// serial test time of the heaviest shard.
const releaseGateMainnetShardTimeout = "510m"

// Go's rule for a test, fuzz or benchmark name: the prefix, then nothing or a
// rune that is not lower case.
func releaseGateGoTestName(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	next, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return len(name) == len(prefix) || !unicode.IsLower(next)
}

// The names `go test -list .` prints for ./mainnet's default build, except
// benchmarks, which no -run executes: its tests, fuzz targets and examples
// with output.
func releaseGateMainnetTestList(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("../mainnet/*_test.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("mainnet test census: %d files, %v", len(paths), err)
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
		if matched, err := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path)); err != nil || !matched {
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

// Run the shard owner's own definitions in bash after the prelude, which
// stands in for the gate's job environment, then the given commands.
func runReleaseGateMainnetShardOwner(t *testing.T, prelude, commands string, arguments ...string) (string, error) {
	t.Helper()
	fragment := releaseGateLocalFragment(t, "release_gate_mainnet_shards=", "release_phase_sn_mainnet_1() {")
	command := exec.Command("bash", append([]string{"-c", "set -euo pipefail\n" + prelude + "\n" + fragment + "\n" + commands, "bash"}, arguments...)...)
	output, err := command.CombinedOutput()
	return string(output), err
}

// A job environment whose go records each call: -list answers with the list
// file, and a test run records its arguments and passes.
type releaseGateMainnetShardFixture struct {
	root, sn string
}

func newReleaseGateMainnetShardFixture(t *testing.T, names []string) releaseGateMainnetShardFixture {
	t.Helper()
	root := t.TempDir()
	fixture := releaseGateMainnetShardFixture{root: root, sn: filepath.Join(root, "sn")}
	if err := os.Mkdir(fixture.sn, 0o700); err != nil {
		t.Fatal(err)
	}
	// go test merges the test binary's stderr into its list output.
	list := "[env]glog not configured from command line. Using default configuration.\n" + strings.Join(names, "\n") + "\nok  \tgithub.com/urfoundation/sn/mainnet\t0.104s\n"
	if err := os.WriteFile(filepath.Join(root, "list"), []byte(list), 0o600); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (self releaseGateMainnetShardFixture) prelude(skip string) string {
	return `fixture=` + strconv.Quote(self.root) + `
sn_repo=` + strconv.Quote(self.sn) + `
release_gate_aggregate_skip='` + skip + `'
go() {
  if [[ "$1 $2" == "test -list" ]]; then
    printf 'list %s\n' "$*" >> "$fixture/calls"
    cat "$fixture/list"
    return
  fi
  printf '%s\n' "$@" > "$fixture/arguments"
}
`
}

// The arguments of the shard's test run, by flag; a flag=value argument is
// its own key.
func (self releaseGateMainnetShardFixture) runFlags(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(self.root, "arguments"))
	if err != nil {
		t.Fatal(err)
	}
	arguments := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(arguments) < 3 || arguments[0] != "test" || arguments[1] != "./mainnet" {
		t.Fatalf("the shard ran go %q", arguments)
	}
	flags := map[string]string{}
	for index := 2; index < len(arguments); index++ {
		flag := arguments[index]
		if _, repeated := flags[flag]; repeated || !strings.HasPrefix(flag, "-") {
			t.Fatalf("the shard ran go %q", arguments)
		}
		if strings.Contains(flag, "=") {
			flags[flag] = ""
			continue
		}
		if index+1 == len(arguments) {
			t.Fatalf("the shard ran go %q", arguments)
		}
		flags[flag] = arguments[index+1]
		index++
	}
	return flags
}

// The names of one printed selector, which must be exactly ^(a|b|...)$.
func releaseGateMainnetSelectorNames(selector string) ([]string, error) {
	if !strings.HasPrefix(selector, "^(") || !strings.HasSuffix(selector, ")$") {
		return nil, fmt.Errorf("selector %q is not one anchored alternation", selector)
	}
	return strings.Split(selector[2:len(selector)-2], "|"), nil
}

// Every name of the list is in exactly one shard's selector, as Go matches it,
// and each shard's selector names only its own tests, at least one.
func verifyReleaseGateMainnetPartition(t *testing.T, names []string, selectors []string) error {
	t.Helper()
	owners := map[string]int{}
	for index, selector := range selectors {
		members, err := releaseGateMainnetSelectorNames(selector)
		if err != nil {
			return err
		}
		if len(members) == 0 || members[0] == "" {
			return fmt.Errorf("shard %d selects no test", index+1)
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

// The selector each shard prints for the list, from the gate's own function.
func releaseGateMainnetSelectors(t *testing.T, names []string, shards int, skips ...string) ([]string, error) {
	t.Helper()
	fixture := newReleaseGateMainnetShardFixture(t, names)
	var selectors []string
	for shard := 1; shard <= shards; shard++ {
		output, err := runReleaseGateMainnetShardOwner(t, "", "release_gate_mainnet_skip_names="+strconv.Quote(strings.Join(skips, " "))+"\n"+`release_gate_mainnet_selector "$1" "$2" < "$3"`, strconv.Itoa(shard), strconv.Itoa(shards), filepath.Join(fixture.root, "list"))
		if err != nil {
			return nil, fmt.Errorf("shard %d of %d: %v: %s", shard, shards, err, output)
		}
		selectors = append(selectors, output)
	}
	return selectors, nil
}

// The shard partition is complete and disjoint for ./mainnet's current list:
// every test, fuzz target and example runs in exactly one of the eight shards,
// none is empty, and go test's other output lines select nothing.
func TestReleaseGateMainnetShardsPartitionTheTestList(t *testing.T) {
	t.Parallel()
	names := releaseGateMainnetTestList(t)
	if len(names) < 1000 {
		t.Fatalf("mainnet lists only %d tests", len(names))
	}
	selectors, err := releaseGateMainnetSelectors(t, names, releaseGateMainnetShardCount)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyReleaseGateMainnetPartition(t, names, selectors); err != nil {
		t.Fatal(err)
	}
	var sizes []int
	for _, selector := range selectors {
		members, _ := releaseGateMainnetSelectorNames(selector)
		sizes = append(sizes, len(members))
	}
	t.Logf("%d tests in shards of %v", len(names), sizes)
	// A selector that loses its last shard or invents a name fails.
	if verifyReleaseGateMainnetPartition(t, names, selectors[:len(selectors)-1]) == nil || verifyReleaseGateMainnetPartition(t, names, append(slices.Clone(selectors[1:]), "^("+names[0]+"|TestInvented)$")) == nil {
		t.Fatal("the partition check accepted a lost shard or an invented name")
	}
	// Names Go would list but that no -run could match by an ASCII
	// alternation, and an empty list, are refused, not silently dropped.
	for _, list := range [][]string{append(slices.Clone(names), "TestÜbernahme"), append(slices.Clone(names), "Test-dash"), {}} {
		if _, err := releaseGateMainnetSelectors(t, list, releaseGateMainnetShardCount); err == nil {
			t.Fatalf("the shard selector accepted the list ending %q", list[max(len(list)-1, 0):])
		}
	}
}

// sn-all-normal runs every sn package but ./mainnet, by package: its list
// drops exactly that import path, keeping its siblings and subpackages.
func TestReleaseGateMainnetLeavesTheAggregateJob(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	commands, _, err := releaseGatePhaseCommands(script, "sn_all_normal")
	if err != nil || !slices.Equal(commands, releaseGateAggregateNormalBody) {
		t.Fatalf("sn-all-normal runs %q, %v; want %q", commands, err, releaseGateAggregateNormalBody)
	}
	root := t.TempDir()
	packages := []string{"github.com/urfoundation/sn/v2026/crv4", "github.com/urfoundation/sn/v2026/mainnet", "github.com/urfoundation/sn/v2026/mainnet/sub", "github.com/urfoundation/sn/v2026/mainnetx", "github.com/urfoundation/sn/v2026/sim-testnet"}
	body := strings.Join(commands[1:], "\n")
	command := exec.Command("bash", "-c", `set -euo pipefail
release_gate_aggregate_skip=skip
go() {
  if [[ "$1" == list ]]; then printf '%s\n' `+strings.Join(packages, " ")+`; return; fi
  printf '%s\n' "$@" > "$0/arguments"
}
`+body, root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("sn-all-normal's body failed: %v\n%s", err, output)
	}
	raw, err := os.ReadFile(filepath.Join(root, "arguments"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"test", "-parallel=4", "-timeout", "90m"}
	want = append(want, slices.DeleteFunc(slices.Clone(packages), func(path string) bool { return path == "github.com/urfoundation/sn/v2026/mainnet" })...)
	want = append(want, "-count=1", "-skip", "skip")
	if got := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n"); !slices.Equal(got, want) {
		t.Fatalf("sn-all-normal ran go %q, want %q", got, want)
	}
	// ./mainnet runs whole only in its shards; sn-e2e runs selected roots.
	var mainnet []string
	for _, line := range strings.Split(script, "\n") {
		if line = strings.TrimSpace(line); !strings.HasPrefix(line, "#") && strings.Contains(line, "go test") && strings.Contains(line, "./mainnet") {
			mainnet = append(mainnet, line)
		}
	}
	if want := []string{
		`selector="$(go test -list . ./mainnet | release_gate_mainnet_selector "$shard" "$release_gate_mainnet_shards")"`,
		releaseGateMainnetShardCommand,
		`go test ./mainnet -run "$settlement_v2_e2e_tests" -count=1 -timeout 30m`,
		`go test -race ./mainnet -run "$settlement_v2_e2e_tests" -count=1 -timeout 30m`,
		`go test ./mainnet -run "$stmonitor_fixture_tests" -count=1`,
		`go test -race ./mainnet -run "$stmonitor_fixture_tests" -count=1`,
	}; !slices.Equal(mainnet, want) {
		t.Fatalf("the gate runs ./mainnet in %q, want %q", mainnet, want)
	}
	census, err := releaseGateAggregateSkipCensus("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ old, new string }{
		{releaseGateAggregateNormalCommand, `go test -parallel=4 -timeout 90m ./... -count=1 -skip "$release_gate_aggregate_skip"`},
		{`grep -Evx 'github[.]com/urfoundation/sn/mainnet'`, `grep -Ev 'mainnet'`},
		{`  [[ -n "$sn_package_list" ]]` + "\n", ""},
	} {
		if strings.Count(script, change.old) != 1 {
			t.Fatalf("the mutation names no unique text: %s", change.old)
		}
		if err := verifyReleaseGateAggregateE2ESkip(strings.Replace(script, change.old, change.new, 1), census); err == nil {
			t.Fatalf("sn-all-normal accepted %q", change.new)
		}
	}
}

// The shards are exactly sn-mainnet-1 to sn-mainnet-8, each admitted once,
// unconditionally, running the one shard owner with its own index, whose test
// run has the aggregate skip of the gate's mode, the same for every shard.
func TestReleaseGateMainnetShardsRunTheSameSkips(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	if strings.Count(script, "\nrelease_gate_mainnet_shards="+strconv.Itoa(releaseGateMainnetShardCount)+"\n") != 1 || strings.Count(script, "release_gate_mainnet_shards=") != 1 {
		t.Fatalf("the gate does not run exactly %d ./mainnet shards", releaseGateMainnetShardCount)
	}
	for shard := 1; shard <= releaseGateMainnetShardCount; shard++ {
		index := strconv.Itoa(shard)
		if err := verifyReleaseGateExactPhase(script, "sn-mainnet-"+index, "sn_mainnet_"+index, []string{"release_gate_mainnet_shard " + index}); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(script, "release_phase_sn_mainnet_"+strconv.Itoa(releaseGateMainnetShardCount+1)) || strings.Count(script, "release_gate_mainnet_shard ") != releaseGateMainnetShardCount {
		t.Fatal("the gate runs a ./mainnet shard outside sn-mainnet-1 to sn-mainnet-8")
	}
	owner := regexp.MustCompile(`(?ms)^release_gate_mainnet_shard\(\) \{\n(.*?)^\}$`).FindAllStringSubmatch(script, -1)
	if len(owner) != 1 || strings.Count(owner[0][1], "go test ") != 2 || strings.Count(owner[0][1], "\n  "+releaseGateMainnetShardCommand+"\n") != 1 {
		t.Fatal("the shard owner does not run exactly its one reviewed test command")
	}
	names := releaseGateMainnetTestList(t)
	selectors, err := releaseGateMainnetSelectors(t, names, releaseGateMainnetShardCount)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"pinned", "local"} {
		skip := releaseGateAggregateSkipFor(t, mode)
		for _, shard := range []int{1, releaseGateMainnetShardCount} {
			fixture := newReleaseGateMainnetShardFixture(t, names)
			if output, err := runReleaseGateMainnetShardOwner(t, fixture.prelude(skip), `release_gate_mainnet_shard "$1"`, strconv.Itoa(shard)); err != nil {
				t.Fatalf("%s shard %d failed: %v\n%s", mode, shard, err, output)
			}
			flags := fixture.runFlags(t)
			_, fresh := flags["-count=1"]
			if flags["-skip"] != skip || flags["-run"] != selectors[shard-1] || !fresh || flags["-timeout"] != releaseGateMainnetShardTimeout || len(flags) != 4 {
				t.Fatalf("%s shard %d ran with %q", mode, shard, flags)
			}
		}
	}
	for _, change := range []struct{ old, new string }{
		{"release_phase_sn_mainnet_3() {\n  release_gate_mainnet_shard 3\n", "release_phase_sn_mainnet_3() {\n  release_gate_mainnet_shard 2\n"},
		{"release_gate_start sn-mainnet-5 release_phase_sn_mainnet_5\n", ""},
		{"release_gate_start sn-mainnet-6 release_phase_sn_mainnet_6\n", "if [[ \"$release_gate_dependency_mode\" == pinned ]]; then\nrelease_gate_start sn-mainnet-6 release_phase_sn_mainnet_6\nfi\n"},
	} {
		changed := strings.Replace(script, change.old, change.new, 1)
		if changed == script {
			t.Fatalf("the mutation names no text: %q", change.old)
		}
		failed := false
		for shard := 1; shard <= releaseGateMainnetShardCount; shard++ {
			index := strconv.Itoa(shard)
			failed = failed || verifyReleaseGateExactPhase(changed, "sn-mainnet-"+index, "sn_mainnet_"+index, []string{"release_gate_mainnet_shard " + index}) != nil
		}
		if !failed {
			t.Fatalf("the shard census accepted %q", change.new)
		}
	}
}

// An unknown or mistyped shard, or shard count, is refused before any test
// is listed or run.
func TestReleaseGateMainnetShardRefusesAnUnknownShard(t *testing.T) {
	t.Parallel()
	names := releaseGateMainnetTestList(t)
	cases := []struct{ count, shard string }{
		{"8", "0"}, {"8", "9"}, {"8", "08"}, {"8", "1x"}, {"8", ""}, {"8", "-1"}, {"8", "+1"}, {"8", " 1"},
		{"8x", "1"}, {"0", "1"}, {"", "1"}, {"08", "1"}, {"100", "1"}, {"-8", "1"},
	}
	for _, change := range cases {
		fixture := newReleaseGateMainnetShardFixture(t, names)
		output, err := runReleaseGateMainnetShardOwner(t, fixture.prelude("skip"), "release_gate_mainnet_shards="+strconv.Quote(change.count)+"\nrelease_gate_mainnet_shard \"$1\"", change.shard)
		if err == nil || !strings.Contains(output, "unknown ./mainnet shard") {
			t.Fatalf("shard %q of %q ran: %v\n%s", change.shard, change.count, err, output)
		}
		if _, err := os.Stat(filepath.Join(fixture.root, "calls")); !os.IsNotExist(err) {
			t.Fatalf("shard %q of %q listed tests", change.shard, change.count)
		}
	}
}

// The shards' named skip list is empty, and a name it gains leaves every
// shard while the rest stay complete and disjoint; a name that is no test of
// the package's list is refused, so a renamed test cannot vanish silently.
func TestReleaseGateMainnetShardsSkipOnlyNamedTests(t *testing.T) {
	t.Parallel()
	script := readReleaseGateLocalScript(t)
	if strings.Count(script, "\nrelease_gate_mainnet_skip_names=''\n") != 1 || strings.Count(script, "release_gate_mainnet_skip_names=") != 1 {
		t.Fatal("the shards' named skip list is not the one reviewed empty assignment")
	}
	names := releaseGateMainnetTestList(t)
	skipped := names[len(names)/2]
	selectors, err := releaseGateMainnetSelectors(t, names, releaseGateMainnetShardCount, skipped)
	if err != nil {
		t.Fatal(err)
	}
	rest := slices.DeleteFunc(slices.Clone(names), func(name string) bool { return name == skipped })
	if err := verifyReleaseGateMainnetPartition(t, rest, selectors); err != nil {
		t.Fatal(err)
	}
	for _, selector := range selectors {
		if regexp.MustCompile(selector).MatchString(skipped) {
			t.Fatalf("a shard still runs the named skip %s", skipped)
		}
	}
	if _, err := releaseGateMainnetSelectors(t, names, releaseGateMainnetShardCount, skipped+"Renamed"); err == nil {
		t.Fatal("the shards accepted a named skip that is no test")
	}
	fixture := newReleaseGateMainnetShardFixture(t, names)
	output, err := runReleaseGateMainnetShardOwner(t, fixture.prelude("skip"), "release_gate_mainnet_skip_names="+strconv.Quote(skipped)+"\nrelease_gate_mainnet_shard 1")
	if err != nil || !strings.Contains(output, "[release-1.0] ./mainnet shards skip by name: "+skipped+"\n") {
		t.Fatalf("a shard with a named skip did not print it: %v\n%s", err, output)
	}
}
