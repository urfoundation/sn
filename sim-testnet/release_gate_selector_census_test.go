package main

// Every -run selector of sn's scripts that run go test, both release gates
// among them, selects tests: each of its alternatives, nested ones included,
// matches a test that a package of its command declares, read from the
// package's test sources without a build, the way Go matches it, so
// ^Test(…)$ is an exact name and an alternative without $ a prefix. A renamed
// or never-declared test fails here instead of leaving its alternative
// selecting nothing, as the local gate's server-unit StHeadBoundCkeysFromEvents$
// did: no gate ran either HF-1 double-pay regression test.

import (
	"errors"
	"fmt"
	"go/build"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The -run values, as written, that the census leaves out, each with its
// reason; one held only by the command given must be that command.
var releaseGateUncensusedRuns = []struct{ run, command, reason string }{
	{run: `'^$'`, reason: "compile only: it builds the packages' tests and selects none"},
	{run: `"$selector"`, command: releaseGateShardCommand, reason: "computed at run time: each shard's CRC-32 share of its package's go test -list, which release_gate_shard_selector refuses when it is empty or skips a name that is no test"},
}

// The flags of the scripts' go test commands that take a value, and those
// that take none, and the same of their test binaries' runs; the census
// refuses any other.
var (
	releaseGateGoTestValueFlags     = []string{"-run", "-skip", "-timeout", "-tags", "-p", "-parallel", "-count", "-o"}
	releaseGateGoTestBoolFlags      = []string{"-race", "-v", "-c"}
	releaseGateTestBinaryValueFlags = []string{"-test.run", "-test.skip", "-test.timeout", "-test.count", "-test.parallel"}
	releaseGateTestBinaryBoolFlags  = []string{"-test.v"}
)

// The variables a -run command may not set before go test, since they change
// the build or the selection that the census reads.
var releaseGateGoTestBuildVariables = []string{"GOFLAGS", "GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT"}

// One command of a script that selects tests, as written: its line, the
// directory its packages are in, beside sim-testnet, its packages, the build
// tags of its mode and -tags, and its -run value. A go test -c command that
// builds a test binary instead names the binary it writes, and the binary's
// run selects from the packages of that build.
type releaseGateRunCommand struct {
	line                         int
	text, directory, run, binary string
	packages, tags               []string
}

// The names a package in the directory, beside sim-testnet, declares for -run
// when built with the tags.
type releaseGateTestCensus func(directory string, tags []string) ([]string, error)

// The census of the checked-out sources, which every test shares.
var releaseGateCheckoutTestCensus = releaseGateSourceTestCensus()

// A census of the checked-out sources that reads each package once per set
// of tags, for any number of concurrent tests.
func releaseGateSourceTestCensus() releaseGateTestCensus {
	var stateLock sync.Mutex
	packageTestKVs := map[string][]string{}
	return func(directory string, tags []string) ([]string, error) {
		stateLock.Lock()
		defer stateLock.Unlock()
		key := directory + " " + strings.Join(tags, ",")
		if names, ok := packageTestKVs[key]; ok {
			return names, nil
		}
		context := build.Default
		context.BuildTags = append(slices.Clone(context.BuildTags), tags...)
		names, err := releaseGateDeclaredTests(directory, context)
		if err != nil {
			return nil, err
		}
		packageTestKVs[key] = names
		return names, nil
	}
}

// A script line that selects tests, which the census must read: go test
// with -run, or a test binary's run with -test.run.
func releaseGateRunLine(line string) bool {
	line = strings.TrimSpace(line)
	return !strings.HasPrefix(line, "#") && (strings.Contains(line, "go test") && strings.Contains(line, "-run") || strings.Contains(line, "-test.run"))
}

// A script line that builds a test binary with go test -c.
func releaseGateTestBinaryLine(line string) bool {
	line = strings.TrimSpace(line)
	return !strings.HasPrefix(line, "#") && strings.Contains(line, "go test") && slices.Contains(strings.Fields(line), "-c")
}

// The scripts under root that run go test with -run, in lexical order.
func releaseGateSelectorScripts(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".sh") {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(strings.Split(string(raw), "\n"), releaseGateRunLine) {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

// The directory, beside sim-testnet, that a script's cd argument enters: sn's
// checkout for $sn_repo and the one that holds its siblings for $workspace.
// Any other argument leaves it unknown.
func releaseGateCdDirectory(argument string) string {
	target, quoted := strings.CutPrefix(argument, `"`)
	target, closed := strings.CutSuffix(target, `"`)
	for variable, directory := range map[string]string{"$sn_repo": "..", "$workspace": "../.."} {
		if rest, ok := strings.CutPrefix(target, variable); quoted && closed && ok && (rest == "" || strings.HasPrefix(rest, "/")) && !strings.ContainsAny(rest, "$\"' ") {
			return filepath.Join(directory, rest)
		}
	}
	return ""
}

// One go test line's packages, build tags, -run value and built binary as
// written, or one run of a test binary that binaryKVs names by the path the
// script built it at, after the line's environment assignments and up to its
// first control operator. A go test command's packages are in directory.
func releaseGateParseRunCommand(line, directory string, binaryKVs map[string]releaseGateRunCommand) (releaseGateRunCommand, error) {
	command := releaseGateRunCommand{text: line, directory: directory}
	fields := strings.Fields(line)
	for index, field := range fields {
		if field == "||" || field == "&&" || field == "|" || field == ";" {
			fields = fields[:index]
			break
		}
	}
	assignment := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=`)
	for len(fields) > 0 && assignment.MatchString(fields[0]) {
		if variable := assignment.FindStringSubmatch(fields[0])[1]; slices.Contains(releaseGateGoTestBuildVariables, variable) {
			return command, fmt.Errorf("its %s changes the build that the census reads", variable)
		}
		fields = fields[1:]
	}
	valueFlags, boolFlags, build := releaseGateGoTestValueFlags, releaseGateGoTestBoolFlags, false
	switch {
	case strings.HasSuffix(line, `\`):
		return command, errors.New("the census cannot read this -run command")
	case len(fields) >= 2 && fields[0] == "go" && fields[1] == "test":
		fields = fields[2:]
	case len(fields) >= 1 && binaryKVs[fields[0]].binary != "":
		binary := binaryKVs[fields[0]]
		command.directory, command.packages, command.tags = binary.directory, binary.packages, binary.tags
		valueFlags, boolFlags, fields = releaseGateTestBinaryValueFlags, releaseGateTestBinaryBoolFlags, fields[1:]
	default:
		return command, errors.New("the census cannot read this -run command")
	}
	binaryRun := len(command.packages) != 0
	for index := 0; index < len(fields); index++ {
		name, value, inline := strings.Cut(fields[index], "=")
		switch {
		case !strings.HasPrefix(name, "-") && binaryRun:
			return command, fmt.Errorf("its test binary takes the argument %s", fields[index])
		case !strings.HasPrefix(name, "-"):
			command.packages = append(command.packages, fields[index])
		case slices.Contains(boolFlags, name) && !inline:
			if name == "-race" {
				command.tags = append(command.tags, "race")
			}
			build = build || name == "-c"
		case slices.Contains(valueFlags, name):
			if !inline {
				if index++; index == len(fields) {
					return command, fmt.Errorf("its %s has no value", name)
				}
				value = fields[index]
			}
			if (name == "-run" || name == "-test.run") && command.run != "" {
				return command, fmt.Errorf("it repeats %s", name)
			}
			switch name {
			case "-run", "-test.run":
				command.run = value
			case "-tags":
				command.tags = append(command.tags, strings.Split(value, ",")...)
			case "-o":
				command.binary = value
			}
		default:
			return command, fmt.Errorf("it has the unreviewed go test flag %s", fields[index])
		}
	}
	slices.Sort(command.tags)
	switch {
	case build && command.binary == "":
		return command, errors.New("it builds a test binary without -o")
	case build && command.run != "":
		return command, errors.New("it builds a test binary, which runs no test, with -run")
	case build && len(command.packages) != 1:
		return command, fmt.Errorf("it builds one test binary of %d packages", len(command.packages))
	case !build && command.run == "":
		return command, errors.New("it has no -run value")
	}
	if !build {
		command.binary = ""
	}
	return command, nil
}

// The commands of a script that select tests, each with the directory that
// the last cd of its own function, group or subshell enters; a function's is
// its caller's, so it starts unknown. A test binary's run selects from the
// packages of the go test -c that built it, earlier in the script. Every line
// that selects tests or builds a test binary is read first, so a line the
// census cannot read is refused rather than left out.
func releaseGateRunCommands(script string) ([]releaseGateRunCommand, error) {
	function := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\(\) \{$`)
	directories := []string{""}
	var commands []releaseGateRunCommand
	binaryKVs := map[string]releaseGateRunCommand{}
	for index, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case releaseGateTestBinaryLine(line) || releaseGateRunLine(line):
			command, err := releaseGateParseRunCommand(line, directories[len(directories)-1], binaryKVs)
			if err != nil {
				return nil, fmt.Errorf("line %d: %s: %w", index+1, line, err)
			}
			command.line = index + 1
			if command.binary != "" {
				binaryKVs[command.binary] = command
			} else {
				commands = append(commands, command)
			}
		case function.MatchString(line):
			directories = append(directories, "")
		case line == "(" || line == "{":
			directories = append(directories, directories[len(directories)-1])
		case strings.HasPrefix(line, ")") || strings.HasPrefix(line, "}"):
			if len(directories) == 1 {
				return nil, fmt.Errorf("line %d closes no function, group or subshell", index+1)
			}
			directories = directories[:len(directories)-1]
		case strings.HasPrefix(line, "cd "):
			directories[len(directories)-1] = releaseGateCdDirectory(strings.TrimPrefix(line, "cd "))
		}
	}
	if len(directories) != 1 {
		return nil, errors.New("the script leaves a function, group or subshell open")
	}
	return commands, nil
}

// The values a command's -run takes in the gate's two dependency modes: its
// quoted literal, or its variable's one assignment or reviewed mode pair.
func releaseGateRunSelectorValues(script, run string) ([]string, error) {
	if literal, ok := strings.CutPrefix(run, "'"); ok {
		if value, ok := strings.CutSuffix(literal, "'"); ok && !strings.Contains(value, "'") {
			return []string{value}, nil
		}
	}
	variable, quoted := strings.CutPrefix(run, `"$`)
	variable, closed := strings.CutSuffix(variable, `"`)
	if !quoted || !closed || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(variable) {
		return nil, fmt.Errorf("the census cannot read -run %s", run)
	}
	var values []string
	for _, mode := range []string{"pinned", "local"} {
		value, err := releaseGateModeSelector(script, variable, mode)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(values, value) {
			values = append(values, value)
		}
	}
	return values, nil
}

// The leaves of a -run selector: one regular expression for each way of
// taking one branch of every alternation in it, nested ones included, so each
// alternative as written selects tests on its own. A group with flags or a
// quantifier, whose branches cannot stand alone, is refused.
func releaseGateSelectorLeaves(selector string) ([]string, error) {
	leaves, rest, err := releaseGateSelectorAlternation(selector)
	if err == nil && rest != "" {
		err = errors.New("an unmatched )")
	}
	if err != nil {
		return nil, fmt.Errorf("selector %q has %w", selector, err)
	}
	return leaves, nil
}

// The leaves of the branches from the start of text to its first unmatched )
// or its end, and the text from there.
func releaseGateSelectorAlternation(text string) ([]string, string, error) {
	var leaves []string
	for {
		branch, rest, err := releaseGateSelectorBranch(text)
		if err != nil {
			return nil, "", err
		}
		leaves = append(leaves, branch...)
		if !strings.HasPrefix(rest, "|") {
			return leaves, rest, nil
		}
		text = rest[1:]
	}
}

// The leaves of one branch, up to its | or ), and the text from there.
func releaseGateSelectorBranch(text string) ([]string, string, error) {
	leaves := []string{""}
	for text != "" && text[0] != '|' && text[0] != ')' {
		atoms, rest, err := releaseGateSelectorAtom(text)
		if err != nil {
			return nil, "", err
		}
		var joined []string
		for _, leaf := range leaves {
			for _, atom := range atoms {
				joined = append(joined, leaf+atom)
			}
		}
		leaves, text = joined, rest
	}
	return leaves, text, nil
}

// The leaves of the atom that starts text, an escape, a class, a group's
// branches or one byte, and the text after it.
func releaseGateSelectorAtom(text string) ([]string, string, error) {
	switch text[0] {
	case '\\':
		if len(text) < 2 {
			return nil, "", errors.New("a trailing \\")
		}
		return []string{text[:2]}, text[2:], nil
	case '[':
		end := 1
		if strings.HasPrefix(text[end:], "^") {
			end++
		}
		if strings.HasPrefix(text[end:], "]") {
			end++
		}
		for end < len(text) && text[end] != ']' {
			if text[end] == '\\' {
				end++
			} else if strings.HasPrefix(text[end:], "[:") {
				if named := strings.Index(text[end:], ":]"); named > 0 {
					end += named + 1
				}
			}
			end++
		}
		if end >= len(text) {
			return nil, "", errors.New("an unmatched [")
		}
		return []string{text[:end+1]}, text[end+1:], nil
	case '(':
		inner := strings.TrimPrefix(text[1:], "?:")
		if strings.HasPrefix(inner, "?") {
			return nil, "", errors.New("a group with flags")
		}
		branches, rest, err := releaseGateSelectorAlternation(inner)
		if err != nil {
			return nil, "", err
		}
		if !strings.HasPrefix(rest, ")") {
			return nil, "", errors.New("an unmatched (")
		}
		if rest = rest[1:]; rest != "" && strings.ContainsRune("*+?{", rune(rest[0])) {
			return nil, "", errors.New("a quantified group")
		}
		return branches, rest, nil
	}
	return []string{text[:1]}, text[1:], nil
}

// The tests that the packages of a command declare, built with its tags, and
// the packages' directories.
func releaseGateRunCommandTests(command releaseGateRunCommand, census releaseGateTestCensus) ([]string, []string, error) {
	if command.directory == "" {
		return nil, nil, errors.New("no cd of its own function or subshell enters a checkout it names")
	}
	if len(command.packages) == 0 {
		return nil, nil, errors.New("it names no package")
	}
	var names, directories []string
	for _, packagePath := range command.packages {
		if packagePath != "." && (!strings.HasPrefix(packagePath, "./") || strings.Contains(packagePath, "...") || strings.ContainsAny(packagePath, "$\"'")) {
			return nil, nil, fmt.Errorf("its package %s is not one directory of its module", packagePath)
		}
		directory := filepath.Join(command.directory, packagePath)
		declared, err := census(directory, command.tags)
		if err != nil {
			return nil, nil, err
		}
		names, directories = append(names, declared...), append(directories, directory)
	}
	return names, directories, nil
}

// One -run value's census: each leaf of each value it takes selects a test
// that a package of one of its commands declares. A selector that several
// commands share, each on its own packages, as evidence_source_tests is
// across the server's, selects from all of them.
func verifyReleaseGateRunSelector(script, run string, commands []releaseGateRunCommand, census releaseGateTestCensus) error {
	var names, directories, lines []string
	for _, command := range commands {
		declared, packages, err := releaseGateRunCommandTests(command, census)
		if err != nil {
			return fmt.Errorf("line %d: %s: %w", command.line, command.text, err)
		}
		names, lines = append(names, declared...), append(lines, strconv.Itoa(command.line))
		for _, directory := range packages {
			if !slices.Contains(directories, directory) {
				directories = append(directories, directory)
			}
		}
	}
	at := "line " + lines[0]
	if len(lines) > 1 {
		at = "lines " + strings.Join(lines, ", ")
	}
	values, err := releaseGateRunSelectorValues(script, run)
	if err != nil {
		return fmt.Errorf("%s: %w", at, err)
	}
	var stale []error
	for _, value := range values {
		leaves, err := releaseGateSelectorLeaves(value)
		if err != nil {
			return fmt.Errorf("%s: %w", at, err)
		}
		for _, leaf := range leaves {
			compiled, err := regexp.Compile(leaf)
			if err != nil {
				return fmt.Errorf("%s: %w", at, err)
			}
			if !slices.ContainsFunc(names, compiled.MatchString) {
				stale = append(stale, fmt.Errorf("%s: -run %s: its alternative %s selects no test of %s", at, run, leaf, strings.Join(directories, " ")))
			}
		}
	}
	return errors.Join(stale...)
}

// The census of a script's -run commands: the number it read, the number it
// left out by their -run value, and an error for each alternative that
// selects no test and each command it cannot read.
func verifyReleaseGateRunSelectors(script string, census releaseGateTestCensus) (int, map[string]int, error) {
	commands, err := releaseGateRunCommands(script)
	if err != nil {
		return 0, nil, err
	}
	var runs []string
	runCommandKVs, uncensusedKVs := map[string][]releaseGateRunCommand{}, map[string]int{}
	var failures []error
commands:
	for _, command := range commands {
		for _, uncensused := range releaseGateUncensusedRuns {
			if command.run != uncensused.run {
				continue
			}
			if uncensused.command != "" && command.text != uncensused.command {
				failures = append(failures, fmt.Errorf("line %d: %s: only %s has -run %s", command.line, command.text, uncensused.command, command.run))
			}
			uncensusedKVs[command.run]++
			continue commands
		}
		if runCommandKVs[command.run] == nil {
			runs = append(runs, command.run)
		}
		runCommandKVs[command.run] = append(runCommandKVs[command.run], command)
	}
	read := 0
	for _, run := range runs {
		read += len(runCommandKVs[run])
		if err := verifyReleaseGateRunSelector(script, run, runCommandKVs[run], census); err != nil {
			failures = append(failures, err)
		}
	}
	return read, uncensusedKVs, errors.Join(failures...)
}

// HF-1's drift cannot recur: at the checked-out sn and siblings, every
// alternative of every -run selector of every script under scripts that runs
// go test, both gates and the mainnet exports builder among them, selects a
// test that its command's packages declare. Only the reviewed selectors that
// no script states are left out, each used and for its reason.
func TestReleaseGateSelectorsSelectDeclaredTests(t *testing.T) {
	t.Parallel()
	scripts, err := releaseGateSelectorScripts("../scripts")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"../scripts/build-mainnet-exports.sh", "../scripts/test-release-1.0-local.sh", "../scripts/test-release-1.0-producer-gate.sh"} {
		if !slices.Contains(scripts, required) {
			t.Fatalf("the census does not read %s: %q", required, scripts)
		}
	}
	uncensusedKVs := map[string]int{}
	for _, path := range scripts {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		read, uncensused, err := verifyReleaseGateRunSelectors(string(raw), releaseGateCheckoutTestCensus)
		if err != nil {
			t.Errorf("%s:\n%v", path, err)
		}
		if read == 0 {
			t.Errorf("the census read no -run command of %s", path)
		}
		for run, count := range uncensused {
			uncensusedKVs[run] += count
		}
		t.Logf("%s: %d -run commands read", path, read)
	}
	for _, uncensused := range releaseGateUncensusedRuns {
		if uncensusedKVs[uncensused.run] == 0 {
			t.Errorf("no script command has the uncensused -run %s", uncensused.run)
		}
		t.Logf("%d commands with -run %s left out: %s", uncensusedKVs[uncensused.run], uncensused.run, uncensused.reason)
	}
}

// The root cause of HF-1's drift fails the census: the server-unit phase's
// ./model selector before e5908acf6, whose anchored StHeadBoundCkeysFromEvents$
// named no server test, while the selector that replaced it passes.
func TestReleaseGateSelectorCensusRefusesThePreFixHeadBoundSelector(t *testing.T) {
	t.Parallel()
	const fixed = `go test ./model -run '^Test(VerifyEgressExactIndexAndPrefixScoreAreIndependent|StTransactionAdvisoryLockKeyUsesEthereumNonceScope|StHeadBoundCkeysFromEvents(WindowOverlap|UnbindDodge)|ParseHeadEventCkey)$' -count=1`
	const preFix = `go test ./model -run '^Test(VerifyEgressExactIndexAndPrefixScoreAreIndependent|StTransactionAdvisoryLockKeyUsesEthereumNonceScope|StHeadBoundCkeysFromEvents|ParseHeadEventCkey)$' -count=1`
	script := readReleaseGateLocalScript(t)
	if strings.Count(script, "\n  "+fixed+"\n") != 1 {
		t.Fatal("the server-unit phase no longer runs the fixed ./model selector")
	}
	if _, _, err := verifyReleaseGateRunSelectors(script, releaseGateCheckoutTestCensus); err != nil {
		t.Fatalf("the fixed gate fails the census: %v", err)
	}
	_, _, err := verifyReleaseGateRunSelectors(strings.Replace(script, fixed, preFix, 1), releaseGateCheckoutTestCensus)
	if err == nil || !strings.Contains(err.Error(), "its alternative ^TestStHeadBoundCkeysFromEvents$ selects no test of ../../server/model") {
		t.Fatalf("the census accepted the pre-fix selector: %v", err)
	}
}

// The companion evidence selector that copied the simulator's families fails
// the census on each of the eight that ./protocol, ./stabi and
// ./sim-testnet/gencontracts never declared, while its two live families pass.
func TestReleaseGateSelectorCensusRefusesThePreFixCompanionEvidenceSelector(t *testing.T) {
	t.Parallel()
	const fixed = "validator_evidence_tests='^Test(ValidatorEvidence|ClientKeyHistory)'"
	const preFix = "validator_evidence_tests='^Test(ValidatorEvidence|RuntimeEvidenceV2|RuntimeEvidence|EvidenceRelay|EvmTxManager|ClientKeyHistory|RuntimeProvisional|ProvisionalRelay|RelayContinuation|EvidencePolicy)'"
	raw, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	if strings.Count(script, "\n  "+fixed+"\n") != 1 {
		t.Fatal("the solidity phase no longer assigns the fixed companion selector")
	}
	if _, _, err := verifyReleaseGateRunSelectors(script, releaseGateCheckoutTestCensus); err != nil {
		t.Fatalf("the fixed gate fails the census: %v", err)
	}
	_, _, err = verifyReleaseGateRunSelectors(strings.Replace(script, fixed, preFix, 1), releaseGateCheckoutTestCensus)
	for _, family := range []string{"RuntimeEvidenceV2", "RuntimeEvidence", "EvidenceRelay", "EvmTxManager", "RuntimeProvisional", "ProvisionalRelay", "RelayContinuation", "EvidencePolicy"} {
		if err == nil || !strings.Contains(err.Error(), "its alternative ^Test"+family+" selects no test of ../protocol ../stabi ../sim-testnet/gencontracts") {
			t.Fatalf("the census accepted the pre-fix companion family %s: %v", family, err)
		}
	}
}

// A renamed regression fails the census through its own alternative, nested,
// held by a variable or passed to a test binary: one of the two names of the
// local gate's HF-1 group, a family of a producer gate variable, and the
// exports builder's native-fee export.
func TestReleaseGateSelectorCensusRefusesARenamedAlternative(t *testing.T) {
	t.Parallel()
	local := readReleaseGateLocalScript(t)
	producer, err := os.ReadFile("../scripts/test-release-1.0-producer-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	builder, err := os.ReadFile("../scripts/build-mainnet-exports.sh")
	if err != nil {
		t.Fatal(err)
	}
	producerSelector, err := releaseConnectPolicySelectorAssignment(string(producer), "producer_tests")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(producerSelector, "^Test(") || !strings.HasSuffix(producerSelector, ")") {
		t.Fatalf("producer_tests is missing its anchored test group: %q", producerSelector)
	}
	alternatives := strings.Split(strings.TrimSuffix(strings.TrimPrefix(producerSelector, "^Test("), ")"), "|")
	productionLoader := slices.Index(alternatives, "ProductionLoader")
	if productionLoader < 0 || slices.Contains(alternatives[productionLoader+1:], "ProductionLoader") {
		t.Fatal("producer_tests must contain one ProductionLoader alternative")
	}
	alternatives[productionLoader] = "ProductionLoaderRenamed"
	producerAssignment := "producer_tests='" + producerSelector + "'"
	renamedProducerAssignment := "producer_tests='^Test(" + strings.Join(alternatives, "|") + ")'"
	for _, change := range []struct{ script, old, new, stale string }{
		{script: local, old: "StHeadBoundCkeysFromEvents(WindowOverlap|UnbindDodge)", new: "StHeadBoundCkeysFromEvents(WindowOverlap|UnbindDodgeRenamed)", stale: "^TestStHeadBoundCkeysFromEventsUnbindDodgeRenamed$ selects no test of ../../server/model"},
		{script: string(producer), old: producerAssignment, new: renamedProducerAssignment, stale: "^TestProductionLoaderRenamed selects no test of ../validator"},
		{script: string(builder), old: "'^TestNativeFeeOutcomeExportSettlementFixtures$'", new: "'^TestNativeFeeOutcomeExportSettlementFixturesRenamed$'", stale: "^TestNativeFeeOutcomeExportSettlementFixturesRenamed$ selects no test of ../mainnet"},
	} {
		if strings.Count(change.script, change.old) != 1 {
			t.Fatalf("the mutation names no unique text: %q", change.old)
		}
		_, _, err := verifyReleaseGateRunSelectors(strings.Replace(change.script, change.old, change.new, 1), releaseGateCheckoutTestCensus)
		if err == nil || !strings.Contains(err.Error(), "its alternative "+change.stale) {
			t.Fatalf("the census accepted %s: %v", change.new, err)
		}
	}
}

// A census of synthetic declarations, by directory, beside sim-testnet, and
// build tags, each written after its directory.
func releaseGateFixtureTestCensus(declaredTestKVs map[string][]string) releaseGateTestCensus {
	return func(directory string, tags []string) ([]string, error) {
		names, ok := declaredTestKVs[strings.Join(append([]string{directory}, tags...), " ")]
		if !ok {
			return nil, fmt.Errorf("test census: 0 files in %s %q", directory, tags)
		}
		return names, nil
	}
}

// A synthetic gate whose one phase enters the server's checkout.
func releaseGateCensusFixtureScript(lines ...string) string {
	return "release_phase_fixture() {\n  cd \"$workspace/server\"\n  " + strings.Join(lines, "\n  ") + "\n}\nrelease_gate_start fixture release_phase_fixture\n"
}

// The synthetic declarations of the command censuses.
var releaseGateFixtureDeclaredTestKVs = map[string][]string{
	"../ledger":                  {"TestLedgerRoot"},
	"../../server/model":         {"TestModelRoot"},
	"../../server/model race":    {"TestModelRoot", "TestModelRaceRoot"},
	"../../server/model scale":   {"TestModelRoot", "TestStV2ScaleRoot"},
	"../../server/controller":    {"TestControllerRoot"},
	"../../sdk/build/cmd/export": {"TestExportRoot"},
}

// ^Test(…)$ selects only a test of that exact name, an alternative without $
// any test it begins, and an unanchored one any test containing it.
func TestReleaseGateSelectorCensusHonoursAnchoring(t *testing.T) {
	t.Parallel()
	census := releaseGateFixtureTestCensus(map[string][]string{"../../server/model": {"TestFooBar", "TestQux"}})
	for _, testCase := range []struct{ run, stale string }{
		{run: `'^Test(FooBar|Qux)$'`},
		{run: `'^Test(Foo|Qux)'`},
		{run: `'Test.*(Bar|Qux)'`},
		{run: `'^Test(Foo|Qux)$'`, stale: "^TestFoo$"},
		{run: `'^Test(FooBar|Quux)'`, stale: "^TestQuux"},
		{run: `'^TestFoo(Bar|Baz)$'`, stale: "^TestFooBaz$"},
		{run: `'Test.*(Bar|Baz)'`, stale: "Test.*Baz"},
	} {
		_, _, err := verifyReleaseGateRunSelectors(releaseGateCensusFixtureScript("go test ./model -run "+testCase.run+" -count=1"), census)
		if testCase.stale == "" && err != nil || testCase.stale != "" && (err == nil || !strings.Contains(err.Error(), "its alternative "+testCase.stale+" selects no test")) {
			t.Fatalf("-run %s: %v, want stale %q", testCase.run, err, testCase.stale)
		}
	}
}

// Each alternative as written stands alone, nested ones included.
func TestReleaseGateSelectorCensusReadsEveryAlternative(t *testing.T) {
	t.Parallel()
	for selector, want := range map[string][]string{
		"^Test(A(B|C)|D)$":            {"^TestAB$", "^TestAC$", "^TestD$"},
		"^TestX(Y|Z)":                 {"^TestXY", "^TestXZ"},
		"^(TestA|TestB)$|^(TestC)$":   {"^TestA$", "^TestB$", "^TestC$"},
		`^Test(V[12].*|H[13]X|Y\(Z)$`: {"^TestV[12].*$", "^TestH[13]X$", `^TestY\(Z$`},
		"^Test(?:A|B)[]|(:alpha:]$":   {"^TestA[]|(:alpha:]$", "^TestB[]|(:alpha:]$"},
		"^Test([[:alpha:]|]|C)":       {"^Test[[:alpha:]|]", "^TestC"},
		"TestTrie":                    {"TestTrie"},
	} {
		leaves, err := releaseGateSelectorLeaves(selector)
		if err != nil || !slices.Equal(leaves, want) {
			t.Fatalf("the leaves of %q are %q, want %q: %v", selector, leaves, want, err)
		}
	}
}

// A selector whose alternatives cannot stand alone, or that does not parse,
// is refused.
func TestReleaseGateSelectorCensusRefusesAnAlternativeThatCannotStandAlone(t *testing.T) {
	t.Parallel()
	for selector, refusal := range map[string]string{
		"^Test(A|B)*":    "a quantified group",
		"^Test(A|B)?":    "a quantified group",
		"^Test(A|B)+":    "a quantified group",
		"^Test(A|B){2}":  "a quantified group",
		"^Test(A|B":      "an unmatched (",
		"^Test(A|B))":    "an unmatched )",
		"^Test(?i:A|B)":  "a group with flags",
		"^Test[AB":       "an unmatched [",
		`^Test\`:         "a trailing \\",
		"^Test(A|B)$|(C": "an unmatched (",
	} {
		if leaves, err := releaseGateSelectorLeaves(selector); err == nil || !strings.Contains(err.Error(), refusal) {
			t.Fatalf("the census read %q as %q: %v, want %q", selector, leaves, err, refusal)
		}
	}
}

// Each command reads its own phase's checkout and the union of its packages,
// built with the tags of its mode and -tags, after any environment it sets;
// a selector that several commands share reads all their packages, a test
// binary's run reads the package of the go test -c that built it, and only
// the reviewed runtime selectors are left out.
func TestReleaseGateSelectorCensusResolvesEachCommand(t *testing.T) {
	t.Parallel()
	other := "release_phase_other() {\n  cd \"$workspace/sdk/build\"\n  go test ./cmd/export -run '^TestExportRoot$' -count=1\n}\n"
	binary := "release_phase_binary() {\n  cd \"$sn_repo\"\n  go test -c -o \"$output/ledger.test\" ./ledger\n  cd \"$sn_repo/ledger\"\n  FIXTURE_DIRECTORY=\"$output/fixture\" \"$output/ledger.test\" -test.run '^TestLedgerRoot$' -test.timeout 10m\n}\n"
	read, uncensused, err := verifyReleaseGateRunSelectors(releaseGateCensusFixtureScript(
		`go test ./model ./controller -run '^Test(ModelRoot|ControllerRoot)$' -count=1`,
		`shared_tests='^Test(ModelRoot|ControllerRoot)$'`,
		`go test ./model -run "$shared_tests" -count=1`,
		`go test ./controller -run "$shared_tests" -count=1`,
		`fixture_tests='^Test(ModelRaceRoot)$'`,
		`go test -race ./model -run "$fixture_tests" -count=1 -parallel=4 -timeout 10m`,
		`go test -tags scale ./model -run '^TestStV2Scale' -count=1 -p 1 -v`,
		`URNETWORK_FIXTURE_DIR="$output/fixture" GOMAXPROCS=4 go test ./controller -run '^TestControllerRoot$' -count=1`,
		"(",
		`  cd "$sn_repo"`,
		`  go test ./ledger -run='^TestLedgerRoot$' -count=1 || status=1`,
		") || status=1",
		`go test ./model -run '^TestModelRoot$' -count=1`,
		`go test ./... -run '^$'`,
	)+other+binary, releaseGateFixtureTestCensus(releaseGateFixtureDeclaredTestKVs))
	if err != nil || read != 10 || uncensused[`'^$'`] != 1 || len(uncensused) != 1 {
		t.Fatalf("the census read %d and left out %v: %v", read, uncensused, err)
	}
}

// A command the census cannot place or read is refused rather than passed,
// as is an alternative that selects no test of the packages it runs.
func TestReleaseGateSelectorCensusRefusesAnUnreadableCommand(t *testing.T) {
	t.Parallel()
	census := releaseGateFixtureTestCensus(releaseGateFixtureDeclaredTestKVs)
	other := "release_phase_other() {\n  cd \"$workspace/sdk/build\"\n  go test ./cmd/export -run '^TestExportRoot$' -count=1\n}\n"
	for _, testCase := range []struct{ script, refusal string }{
		{script: releaseGateCensusFixtureScript(`go test ./model -run '^Test(ModelRoot|ControllerRoot)$' -count=1`), refusal: "its alternative ^TestControllerRoot$ selects no test of ../../server/model"},
		{script: releaseGateCensusFixtureScript(`shared_tests='^Test(ModelRoot|ControllerRoot|LedgerRoot)$'`, `go test ./model -run "$shared_tests" -count=1`, `go test ./controller -run "$shared_tests" -count=1`), refusal: "its alternative ^TestLedgerRoot$ selects no test of ../../server/model ../../server/controller"},
		{script: releaseGateCensusFixtureScript(`go test ./model -run '^TestModelRaceRoot$' -count=1`), refusal: "its alternative ^TestModelRaceRoot$ selects no test"},
		{script: releaseGateCensusFixtureScript(`go test ./model -run '^TestStV2Scale' -count=1`), refusal: "its alternative ^TestStV2Scale selects no test"},
		{script: releaseGateCensusFixtureScript("(", `  cd "$sn_repo"`, ")", `go test ./ledger -run '^TestLedgerRoot$' -count=1`), refusal: "0 files in ../../server/ledger"},
		{script: "release_phase_fixture() {\n  go test ./model -run '^TestModelRoot$' -count=1\n}\n", refusal: "no cd of its own function or subshell enters a checkout"},
		{script: other + "release_phase_fixture() {\n  go test ./cmd/export -run '^TestExportRoot$' -count=1\n}\n", refusal: "no cd of its own function or subshell enters a checkout"},
		{script: "release_phase_fixture() {\n  cd \"$HOME/server\"\n  go test ./model -run '^TestModelRoot$' -count=1\n}\n", refusal: "no cd of its own function or subshell enters a checkout"},
		{script: releaseGateCensusFixtureScript(`go test ./... -run '^TestModelRoot$' -count=1`), refusal: "its package ./... is not one directory of its module"},
		{script: releaseGateCensusFixtureScript(`go test "$package" -run '^TestModelRoot$' -count=1`), refusal: `its package "$package" is not one directory of its module`},
		{script: releaseGateCensusFixtureScript(`go test ./missing -run '^TestModelRoot$' -count=1`), refusal: "0 files in ../../server/missing"},
		{script: releaseGateCensusFixtureScript(`go test -run '^TestModelRoot$' -count=1`), refusal: "it names no package"},
		{script: releaseGateCensusFixtureScript(`go test -cover ./model -run '^TestModelRoot$' -count=1`), refusal: "the unreviewed go test flag -cover"},
		{script: releaseGateCensusFixtureScript(`go test ./model -run '^TestModelRoot$' -count=1 \`, "  -timeout 10m"), refusal: "the census cannot read this -run command"},
		{script: releaseGateCensusFixtureScript(`env URNETWORK_FIXTURE_DIR=fixture go test ./model -run '^TestModelRoot$' -count=1`), refusal: "the census cannot read this -run command"},
		{script: releaseGateCensusFixtureScript(`cd "$sn_repo" && go test ./ledger -run '^TestLedgerRoot$' -count=1`), refusal: "the census cannot read this -run command"},
		{script: releaseGateCensusFixtureScript(`GOFLAGS=-tags=scale go test ./model -run '^TestStV2Scale' -count=1`), refusal: "its GOFLAGS changes the build that the census reads"},
		{script: releaseGateCensusFixtureScript(`GOOS=windows go test ./model -run '^TestModelRoot$' -count=1`), refusal: "its GOOS changes the build that the census reads"},
		{script: releaseGateCensusFixtureScript(`go test ./model -run '^TestModelRoot$' -run '^TestModelRoot$' -count=1`), refusal: "it repeats -run"},
		{script: releaseGateCensusFixtureScript(`go test ./model -count=1 -run`), refusal: "its -run has no value"},
		{script: releaseGateCensusFixtureScript(`go test ./model -runx '^TestModelRoot$' -count=1`), refusal: "the unreviewed go test flag -runx"},
		{script: releaseGateCensusFixtureScript(`go test ./model -run TestModelRoot -count=1`), refusal: "the census cannot read -run TestModelRoot"},
		{script: releaseGateCensusFixtureScript(`go test ./model -run "${fixture_tests}" -count=1`), refusal: `the census cannot read -run "${fixture_tests}"`},
		{script: releaseGateCensusFixtureScript(`go test ./model -run "$missing_tests" -count=1`), refusal: "missing_tests has 0 assignments"},
		{script: releaseGateCensusFixtureScript(`go test ./model -run '^Test(ModelRoot|Other)*$' -count=1`), refusal: "a quantified group"},
		{script: releaseGateCensusFixtureScript(`go test ./model -run "$selector" -count=1`), refusal: "only " + releaseGateShardCommand + ` has -run "$selector"`},
		{script: releaseGateCensusFixtureScript("}", `go test ./model -run '^TestModelRoot$' -count=1`), refusal: "closes no function, group or subshell"},
		{script: "release_phase_fixture() {\n  cd \"$workspace/server\"\n", refusal: "leaves a function, group or subshell open"},
		{script: releaseGateCensusFixtureScript(`go test -c -o "$output/model.test" ./model`, `"$output/model.test" -test.run '^TestModelRenamed$' -test.timeout 10m`), refusal: "its alternative ^TestModelRenamed$ selects no test of ../../server/model"},
		{script: releaseGateCensusFixtureScript(`"$output/model.test" -test.run '^TestModelRoot$'`), refusal: "the census cannot read this -run command"},
		{script: releaseGateCensusFixtureScript(`go test -c ./model`), refusal: "it builds a test binary without -o"},
		{script: releaseGateCensusFixtureScript(`go test -c -o "$output/model.test" ./model -run '^TestModelRoot$'`), refusal: "it builds a test binary, which runs no test, with -run"},
		{script: releaseGateCensusFixtureScript(`go test -c -o "$output/two.test" ./model ./controller`), refusal: "it builds one test binary of 2 packages"},
		{script: releaseGateCensusFixtureScript(`go test -c -o "$output/model.test" ./model`, `"$output/model.test" extra -test.run '^TestModelRoot$'`), refusal: "its test binary takes the argument extra"},
		{script: releaseGateCensusFixtureScript(`go test -c -o "$output/model.test" ./model`, `"$output/model.test" -test.run '^TestModelRoot$' -test.cpu 2`), refusal: "the unreviewed go test flag -test.cpu"},
		{script: releaseGateCensusFixtureScript(`go test -c -o "$output/model.test" ./model`, `"$output/model.test" -test.run '^TestModelRoot$' -test.run '^TestModelRoot$'`), refusal: "it repeats -test.run"},
		{script: "release_phase_fixture() {\n  go test -c -o \"$output/model.test\" ./model\n  cd \"$workspace/server\"\n  \"$output/model.test\" -test.run '^TestModelRoot$'\n}\n", refusal: "no cd of its own function or subshell enters a checkout"},
	} {
		if _, _, err := verifyReleaseGateRunSelectors(testCase.script, census); err == nil || !strings.Contains(err.Error(), testCase.refusal) {
			t.Fatalf("the census accepted %q: %v, want %q", testCase.script, err, testCase.refusal)
		}
	}
}

// The census reads every script under its root whose code runs go test with
// -run, in nested directories too, and no other file.
func TestReleaseGateSelectorCensusReadsEveryScriptThatSelectsTests(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, source := range map[string]string{
		"selects.sh":        "go test ./fixture -run '^TestFixtureRoot$' -count=1\n",
		"nested/selects.sh": "  ENVIRONMENT=fixture go test ./fixture -run \"$fixture_tests\" -count=1\n",
		"whole.sh":          "go test ./fixture -count=1\n",
		"commented.sh":      "# go test ./fixture -run '^TestFixtureRoot$' -count=1\n",
		"selects.txt":       "go test ./fixture -run '^TestFixtureRoot$' -count=1\n",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	scripts, err := releaseGateSelectorScripts(root)
	if want := []string{filepath.Join(root, "nested", "selects.sh"), filepath.Join(root, "selects.sh")}; err != nil || !slices.Equal(scripts, want) {
		t.Fatalf("the census reads %q, want %q: %v", scripts, want, err)
	}
}
