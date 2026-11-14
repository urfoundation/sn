package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Four contracts that meet the bar with exactly the reviewed artifacts: an
// untracked file's unexecuted line does not count.
var fixtureSources = map[string]string{
	"src/LedgerTree.sol":         "library LedgerTree {\n    function f() internal {\n        g();\n    }\n}\n",
	"src/STClaimBatcher.sol":     "contract STClaimBatcher {\n    function c() external {\n        uint256 i;\n        if (i == 0) i++;\n    }\n}\n",
	"src/STSettlement2.sol":      "contract STSettlement2 {\n    constructor() {\n        _disableInitializers();\n    }\n}\n",
	"src/STSettlementVault2.sol": "contract STSettlementVault2 {\n    function a() external {\n        emit E();\n    }\n}\n",
}

const fixtureReport = "TN:\nSF:src/LedgerTree.sol\nFN:2,LedgerTree.f\nFNDA:5,LedgerTree.f\nDA:2,5\nDA:3,5\nBRDA:3,0,0,5\nBRDA:3,0,1,2\nLF:2\nLH:2\nend_of_record\n" +
	"TN:\nSF:src/STClaimBatcher.sol\nDA:2,3\nDA:4,3\nBRDA:4,0,0,3\nBRDA:4,0,1,3\nend_of_record\n" +
	"TN:\nSF:src/STSettlement2.sol\nDA:2,1\nDA:3,0\nend_of_record\n" +
	"TN:\nSF:src/STSettlementVault2.sol\nDA:2,1\nDA:3,1\nBRDA:3,0,0,1\nend_of_record\n" +
	"TN:\nSF:src/Other.sol\nDA:1,0\nBRDA:1,0,0,-\nend_of_record\n"

// fixture writes the sources into a new Foundry project and returns it.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for file, source := range fixtureSources {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, file), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runReport(t *testing.T, root, report string) (int, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lcov.info")
	if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	status := run([]string{root, path}, &stdout, &stderr)
	return status, stdout.String(), stderr.String()
}

// §15 coverage (gate 1): the bar holds with exactly the reviewed artifacts,
// and each contract's counts are printed.
func TestRunAcceptsTheBarWithTheReviewedArtifacts(t *testing.T) {
	status, stdout, stderr := runReport(t, fixture(t), fixtureReport)
	want := "src/LedgerTree.sol: lines 2/2 executed (0 reviewed artifacts), branches 2/2 taken\n" +
		"src/STClaimBatcher.sol: lines 2/2 executed (0 reviewed artifacts), branches 2/2 taken\n" +
		"src/STSettlement2.sol: lines 1/2 executed (1 reviewed artifacts), branches 0/0 taken\n" +
		"src/STSettlementVault2.sol: lines 2/2 executed (0 reviewed artifacts), branches 1/1 taken\n"
	if status != 0 || stdout != want || stderr != "" {
		t.Fatalf("status %d\nstdout %q\nstderr %q", status, stdout, stderr)
	}
}

// §15 coverage (gate 1): a call without exactly the evm directory and the
// report is a usage error.
func TestRunRefusesOtherArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"evm"}, {"evm", "lcov.info", "extra"}} {
		var stdout, stderr bytes.Buffer
		if status := run(args, &stdout, &stderr); status != 2 || stderr.String() != "usage: solidity-coverage EVM_DIR LCOV_FILE\n" {
			t.Fatalf("%q: status %d, stderr %q", args, status, stderr.String())
		}
	}
}

// §15 coverage (gate 1): a missing report fails.
func TestRunRefusesAMissingReport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "lcov.info")
	if status := run([]string{fixture(t), missing}, &stdout, &stderr); status != 1 || !strings.Contains(stderr.String(), "no such file") {
		t.Fatalf("status %d, stderr %q", status, stderr.String())
	}
}

// §15 coverage (gate 1): a malformed report fails, naming the report.
func TestRunRefusesAMalformedReport(t *testing.T) {
	status, _, stderr := runReport(t, fixture(t), "DA:1,1\n")
	if status != 1 || !strings.HasSuffix(stderr, "lcov.info: line 1: DA outside a record\n") {
		t.Fatalf("status %d, stderr %q", status, stderr)
	}
}

// §15 coverage (gate 1): each malformed entry or record structure is refused.
func TestParseRefusesMalformedEntries(t *testing.T) {
	for _, test := range []struct{ report, want string }{
		{"DA:1,1\n", "line 1: DA outside a record"},
		{"BRDA:1,0,0,1\n", "line 1: BRDA outside a record"},
		{"SF:a.sol\nSF:b.sol\n", "line 2: a record starts inside another"},
		{"end_of_record\n", "line 1: a record ends outside one"},
		{"SF:a.sol\nDA:1,1\n", "the last record does not end"},
		{"SF:a.sol\nDA:1\n", "line 2: malformed DA:1"},
		{"SF:a.sol\nDA:1,1,checksum\n", "line 2: malformed DA:1,1,checksum"},
		{"SF:a.sol\nDA:x,1\n", "line 2: malformed DA:x,1"},
		{"SF:a.sol\nDA:1,-1\n", "line 2: malformed DA:1,-1"},
		{"SF:a.sol\nBRDA:1,0,1\n", "line 2: malformed BRDA:1,0,1"},
		{"SF:a.sol\nBRDA:x,0,0,1\n", "line 2: malformed BRDA:x,0,0,1"},
		{"SF:a.sol\nBRDA:x,0,0,-\n", "line 2: malformed BRDA:x,0,0,-"},
		{"SF:a.sol\nBRDA:1,0,0,y\n", "line 2: malformed BRDA:1,0,0,y"},
	} {
		if _, err := parse([]byte(test.report)); err == nil || err.Error() != test.want {
			t.Fatalf("%q: %v, want %q", test.report, err, test.want)
		}
	}
}

// §15 coverage (gate 1): records of one file are merged; a line's hits add up
// and a branch taken in either record is taken.
func TestParseMergesRecordsOfOneFile(t *testing.T) {
	report, err := parse([]byte("SF:a.sol\nDA:1,0\nDA:2,3\nBRDA:2,0,0,-\nBRDA:2,0,1,0\nend_of_record\nSF:a.sol\nDA:1,2\nDA:2,1\nBRDA:2,0,0,4\nBRDA:2,0,1,0\nend_of_record\n"))
	if err != nil {
		t.Fatal(err)
	}
	c := report["a.sol"]
	if len(report) != 1 || len(c.lines) != 2 || c.lines[1] != 2 || c.lines[2] != 4 ||
		len(c.branches) != 2 || !c.branches["2,0,0"] || c.branches["2,0,1"] {
		t.Fatalf("merged %+v", c)
	}
}

// §15 coverage (gate 1): each shortfall is refused with its file and line, and
// every shortfall is listed.
func TestCheckRefusesEachShortfall(t *testing.T) {
	for _, test := range []struct {
		name, old, new string
		edit           func(root string) error
		want           []string
	}{
		{name: "an unexecuted line", old: "DA:3,5\n", new: "DA:3,0\n",
			want: []string{"src/LedgerTree.sol:3: unexecuted: g();"}},
		{name: "a branch taken 0 times", old: "BRDA:3,0,1,2\n", new: "BRDA:3,0,1,0\n",
			want: []string{"src/LedgerTree.sol:3: branch 0,1 not taken"}},
		{name: "a branch never evaluated", old: "BRDA:3,0,0,1\n", new: "BRDA:3,0,0,-\n",
			want: []string{"src/STSettlementVault2.sol:3: branch 0,0 not taken"}},
		{name: "a claim batcher branch not taken", old: "BRDA:4,0,1,3\n", new: "BRDA:4,0,1,0\n",
			want: []string{"src/STClaimBatcher.sol:4: branch 0,1 not taken"}},
		{name: "a contract missing from the report", old: "TN:\nSF:src/STSettlement2.sol\nDA:2,1\nDA:3,0\nend_of_record\n", new: "",
			want: []string{"src/STSettlement2.sol: no lines in the report"}},
		{name: "a contract without lines", old: "DA:2,1\nDA:3,0\nend_of_record", new: "end_of_record",
			want: []string{"src/STSettlement2.sol: no lines in the report"}},
		{name: "a reviewed artifact executed", old: "DA:2,1\nDA:3,0\n", new: "DA:2,1\nDA:3,1\n",
			want: []string{`src/STSettlement2.sol: 0 unexecuted "_disableInitializers();" lines, 1 reviewed`}},
		{name: "an unreviewed artifact", old: "DA:2,1\nDA:3,0\n", new: "DA:2,1\nDA:3,0\nDA:4,0\n", edit: func(root string) error {
			return os.WriteFile(filepath.Join(root, "src/STSettlement2.sol"), []byte(strings.Replace(fixtureSources["src/STSettlement2.sol"], "_disableInitializers();", "_disableInitializers();\n        _disableInitializers();", 1)), 0o644)
		}, want: []string{`src/STSettlement2.sol: 2 unexecuted "_disableInitializers();" lines, 1 reviewed`}},
		{name: "an artifact of another contract", old: "DA:3,1\nBRDA:3,0,0,1\n", new: "DA:3,0\nBRDA:3,0,0,1\n", edit: func(root string) error {
			return os.WriteFile(filepath.Join(root, "src/STSettlementVault2.sol"), []byte(strings.Replace(fixtureSources["src/STSettlementVault2.sol"], "emit E();", "_disableInitializers();", 1)), 0o644)
		}, want: []string{"src/STSettlementVault2.sol:3: unexecuted: _disableInitializers();"}},
		{name: "an unexecuted line outside the source", old: "DA:3,0\nend_of_record\nTN:\nSF:src/STSettlementVault2.sol", new: "DA:3,0\nDA:99,0\nend_of_record\nTN:\nSF:src/STSettlementVault2.sol",
			want: []string{"src/STSettlement2.sol:99: unexecuted line outside the source"}},
		{name: "an unreadable source", edit: func(root string) error {
			return os.Remove(filepath.Join(root, "src/LedgerTree.sol"))
		}, want: []string{"src/LedgerTree.sol: no such file or directory"}},
		{name: "every shortfall", old: "DA:2,5\nDA:3,5\nBRDA:3,0,0,5\n", new: "DA:2,0\nDA:3,5\nBRDA:3,0,0,0\n",
			want: []string{"src/LedgerTree.sol:2: unexecuted: function f() internal {", "src/LedgerTree.sol:3: branch 0,0 not taken"}},
	} {
		root := fixture(t)
		if test.edit != nil {
			if err := test.edit(root); err != nil {
				t.Fatal(err)
			}
		}
		report := strings.Replace(fixtureReport, test.old, test.new, 1)
		if report == fixtureReport && test.old != "" {
			t.Fatalf("%s: the mutation changes nothing", test.name)
		}
		status, stdout, stderr := runReport(t, root, report)
		if status != 1 || stdout != "" || !strings.HasPrefix(stderr, "settlement v2 coverage is below the bar:\n") {
			t.Fatalf("%s: status %d, stdout %q, stderr %q", test.name, status, stdout, stderr)
		}
		for _, want := range test.want {
			if !strings.Contains(stderr, want) {
				t.Fatalf("%s: stderr %q lacks %q", test.name, stderr, want)
			}
		}
		if lines := strings.Count(stderr, "\n"); lines != len(test.want)+1 {
			t.Fatalf("%s: %d problems in %q, want %d", test.name, lines-1, stderr, len(test.want))
		}
	}
}

// §15 coverage (gate 1): each reviewed artifact names exactly that many lines
// of today's contract, so a moved or added statement needs a new review.
func TestReviewedArtifactsAreTheContractsLines(t *testing.T) {
	for _, a := range reviewed {
		source, err := os.ReadFile(filepath.Join("..", "..", "evm", a.file))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, line := range strings.Split(string(source), "\n") {
			if strings.TrimSpace(line) == a.text {
				count++
			}
		}
		if count != a.count {
			t.Fatalf("%s has %d %q lines, %d reviewed", a.file, count, a.text, a.count)
		}
	}
}
