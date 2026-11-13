// Command solidity-coverage checks the settlement v2 contracts against the
// coverage bar of activation gate 1 (VERSION2-FINAL.md §14, §15): in the lcov
// report of the pinned command
//
//	forge coverage --ir-minimum --report lcov \
//	  --skip STValidatorEvidence.sol --skip ValidatorEvidenceStorage.t.sol --skip ValidatorEvidenceActivation.t.sol
//
// every branch of each contract below is taken, and every line is executed
// except the reviewed instrumentation artifacts. From the repository root:
//
//	go run ./scripts/solidity-coverage evm path/to/lcov.info
package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The contracts held to the bar, named as the report names them: relative to
// the Foundry project.
var contracts = []string{"src/LedgerTree.sol", "src/STClaimBatcher.sol", "src/STSettlement2.sol", "src/STSettlementVault2.sol"}

// An artifact is a source line that the report shows unexecuted although the
// tests run it. The report must show exactly count unexecuted lines of the
// file whose trimmed text is text; any other count is a coverage change for
// review here.
type artifact struct {
	file  string
	text  string
	count int
}

// The reviewed artifacts of --ir-minimum: call statements that get no hit of
// their own, while the report shows their function, their callee and the next
// line of their function executed. vault2 has none: its backing checks take
// an argument, and those lines get hits.
var reviewed = []artifact{
	// The only statement of the implementation's constructor (S-1: its
	// initializers are disabled).
	{file: "src/STSettlement2.sol", text: "_disableInitializers();", count: 1},
}

// coverage is one source file's entries in the report.
type coverage struct {
	// lines maps each instrumented line to its hits.
	lines map[int]uint64
	// branches maps each branch, "line,block,branch", to whether it was taken.
	branches map[string]bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: solidity-coverage EVM_DIR LCOV_FILE")
		return 2
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	report, err := parse(data)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", args[1], err)
		return 1
	}
	summary, err := check(args[0], report)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprint(stdout, strings.Join(summary, "\n")+"\n")
	return 0
}

// parse reads the SF, DA, BRDA and end_of_record entries of an lcov report and
// ignores the rest. Records of the same file are merged.
func parse(data []byte) (map[string]*coverage, error) {
	report := map[string]*coverage{}
	var current *coverage
	for n, line := range strings.Split(string(data), "\n") {
		field, value, _ := strings.Cut(line, ":")
		switch field {
		case "SF":
			if current != nil {
				return nil, fmt.Errorf("line %d: a record starts inside another", n+1)
			}
			if report[value] == nil {
				report[value] = &coverage{lines: map[int]uint64{}, branches: map[string]bool{}}
			}
			current = report[value]
		case "end_of_record":
			if current == nil {
				return nil, fmt.Errorf("line %d: a record ends outside one", n+1)
			}
			current = nil
		case "DA", "BRDA":
			if current == nil {
				return nil, fmt.Errorf("line %d: %s outside a record", n+1, field)
			}
			if !current.add(field, strings.Split(value, ",")) {
				return nil, fmt.Errorf("line %d: malformed %s", n+1, line)
			}
		}
	}
	if current != nil {
		return nil, errors.New("the last record does not end")
	}
	return report, nil
}

// add records a DA entry (line, hits) or a BRDA entry (line, block, branch,
// taken, where "-" is not taken), and reports whether it was well formed.
func (c *coverage) add(field string, parts []string) bool {
	if field == "DA" && len(parts) == 2 {
		line, err := strconv.Atoi(parts[0])
		hits, err2 := strconv.ParseUint(parts[1], 10, 64)
		if err != nil || err2 != nil {
			return false
		}
		c.lines[line] += hits
		return true
	}
	if field == "BRDA" && len(parts) == 4 {
		taken := uint64(0)
		_, err := strconv.Atoi(parts[0])
		if parts[3] != "-" && err == nil {
			taken, err = strconv.ParseUint(parts[3], 10, 64)
		}
		if err != nil {
			return false
		}
		key := strings.Join(parts[:3], ",")
		c.branches[key] = c.branches[key] || taken > 0
		return true
	}
	return false
}

// check holds each contract to the bar, reading its source under root. It
// returns one summary line per contract, or an error listing every shortfall.
func check(root string, report map[string]*coverage) ([]string, error) {
	var summary, problems []string
	for _, file := range contracts {
		c := report[file]
		if c == nil || len(c.lines) == 0 {
			problems = append(problems, file+": no lines in the report")
			continue
		}
		source, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		text := strings.Split(string(source), "\n")
		executed, unexecuted := 0, map[string]int{}
		for _, line := range slices.Sorted(maps.Keys(c.lines)) {
			switch {
			case c.lines[line] > 0:
				executed++
			case line < 1 || line > len(text):
				problems = append(problems, fmt.Sprintf("%s:%d: unexecuted line outside the source", file, line))
			default:
				statement := strings.TrimSpace(text[line-1])
				unexecuted[statement]++
				if !slices.ContainsFunc(reviewed, func(a artifact) bool { return a.file == file && a.text == statement }) {
					problems = append(problems, fmt.Sprintf("%s:%d: unexecuted: %s", file, line, statement))
				}
			}
		}
		artifacts := 0
		for _, a := range reviewed {
			if a.file != file {
				continue
			}
			artifacts += unexecuted[a.text]
			if unexecuted[a.text] != a.count {
				problems = append(problems, fmt.Sprintf("%s: %d unexecuted %q lines, %d reviewed", file, unexecuted[a.text], a.text, a.count))
			}
		}
		taken := 0
		for _, branch := range slices.Sorted(maps.Keys(c.branches)) {
			if c.branches[branch] {
				taken++
				continue
			}
			line, rest, _ := strings.Cut(branch, ",")
			problems = append(problems, fmt.Sprintf("%s:%s: branch %s not taken", file, line, rest))
		}
		summary = append(summary, fmt.Sprintf("%s: lines %d/%d executed (%d reviewed artifacts), branches %d/%d taken",
			file, executed, len(c.lines), artifacts, taken, len(c.branches)))
	}
	if len(problems) != 0 {
		return nil, errors.New("settlement v2 coverage is below the bar:\n" + strings.Join(problems, "\n"))
	}
	return summary, nil
}
