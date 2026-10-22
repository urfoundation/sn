package validator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/operatorlist"
)

func operatorReportTestSnapshot(operators ...operatorlist.Operator) *operatorlist.Snapshot {
	return &operatorlist.Snapshot{
		List:      operatorlist.List{Schema: operatorlist.Schema, Operators: operators},
		Sha256:    "sha256:" + strings.Repeat("cd", 32),
		FetchedAt: time.Date(2026, 10, 6, 11, 0, 0, 0, time.UTC),
		Cached:    true,
	}
}

func TestOperatorListReportMatchesPinnedOperatorsByNormalizedApiUrl(t *testing.T) {
	one, two, three := operatorTestOperator("one"), operatorTestOperator("two"), operatorTestOperator("three")
	pinned := []OperatorConfig{
		{NoID: 1, APIURL: "HTTPS://API.One.Example:443/", ConnectURL: "wss://connect.one.example:443"},
		{NoID: 2, APIURL: "https://api.two.example", ConnectURL: "wss://connect-old.two.example"},
		{NoID: 3, APIURL: "https://api.gone.example", ConnectURL: "wss://connect.gone.example"},
	}
	report := newOperatorListReport(pinned, operatorReportTestSnapshot(one, two, three), "https://list.example/operators.yml", time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC))
	raw, err := report.encode()
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "schema": "urnetwork-validator-operator-list-report-v1",
  "generated_at": "2026-10-06T12:00:00Z",
  "list": {
    "url": "https://list.example/operators.yml",
    "sha256": "sha256:` + strings.Repeat("cd", 32) + `",
    "fetched_at": "2026-10-06T11:00:00Z",
    "cached": true
  },
  "pinned": [
    {
      "no_id": 1,
      "api_url": "HTTPS://API.One.Example:443/",
      "listed": true,
      "domain": "one.example",
      "connect_url_mismatch": false
    },
    {
      "no_id": 2,
      "api_url": "https://api.two.example",
      "listed": true,
      "domain": "two.example",
      "connect_url_mismatch": true
    },
    {
      "no_id": 3,
      "api_url": "https://api.gone.example",
      "listed": false,
      "connect_url_mismatch": false
    }
  ],
  "pinned_omitted": 0,
  "unpinned": [
    "three.example"
  ],
  "unpinned_omitted": 0
}
`
	if string(raw) != want {
		t.Fatalf("report file:\n%s\nwant:\n%s", raw, want)
	}
	lines := report.lines()
	wantLines := []string{
		"operator list sha256:" + strings.Repeat("cd", 32) + " from https://list.example/operators.yml (cached 2026-10-06T11:00:00Z): 3 listed",
		"pinned no_id 1 HTTPS://API.One.Example:443/ is listed as one.example",
		"pinned no_id 2 https://api.two.example is listed as two.example, but its connect_url wss://connect-old.two.example differs from the listed wss://connect.two.example",
		"pinned no_id 3 https://api.gone.example is delisted",
		"listed operator three.example is not pinned by the config",
	}
	if strings.Join(lines, "\n") != strings.Join(wantLines, "\n") {
		t.Fatalf("report lines:\n%s", strings.Join(lines, "\n"))
	}
}

func TestOperatorListReportCapsEachSectionInTheFile(t *testing.T) {
	var pinned []OperatorConfig
	var operators []operatorlist.Operator
	for i := 1; i <= 20; i++ {
		pinned = append(pinned, OperatorConfig{NoID: uint64(i), APIURL: fmt.Sprintf("https://api.pinned-%d.example", i), ConnectURL: fmt.Sprintf("wss://connect.pinned-%d.example", i)})
		operators = append(operators, operatorTestOperator(fmt.Sprintf("listed-%d", i)))
	}
	report := newOperatorListReport(pinned, operatorReportTestSnapshot(operators...), operatorlist.DefaultUrl, time.Now())
	path := filepath.Join(t.TempDir(), "operators-report.json")
	if err := writeOperatorListReport(path, report); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"no_id": 16,`, `"pinned_omitted": 4,`, `"listed-16.example"`, `"unpinned_omitted": 4`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("capped report lacks %s:\n%s", want, raw)
		}
	}
	for _, unwanted := range []string{`"no_id": 17,`, `"listed-17.example"`} {
		if strings.Contains(string(raw), unwanted) {
			t.Fatalf("capped report includes %s", unwanted)
		}
	}
	if lines := report.lines(); len(lines) != 41 || !strings.Contains(lines[40], "listed-20.example") {
		t.Fatalf("the log omitted entries: %d lines", len(lines))
	}
}

func TestOperatorListReportFileIsPrivateAndReplacedWhole(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "operators-report.json")
	first := newOperatorListReport(nil, operatorReportTestSnapshot(operatorTestOperator("one")), operatorlist.DefaultUrl, time.Now())
	second := newOperatorListReport(nil, operatorReportTestSnapshot(operatorTestOperator("one"), operatorTestOperator("two")), operatorlist.DefaultUrl, time.Now())
	for _, report := range []operatorListReport{first, second} {
		if err := writeOperatorListReport(path, report); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	want, encodeErr := second.encode()
	if err != nil || encodeErr != nil || string(raw) != string(want) {
		t.Fatal("report was not replaced by the latest list", err, encodeErr)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("report is not private", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("report left temporary files: %v %v", entries, err)
	}
	if lines := second.lines(); len(lines) != 1 || !strings.HasSuffix(lines[0], ": 2 listed: one.example, two.example") {
		t.Fatalf("flag-mode report lines %q", lines)
	}
}

func TestNormalizedOperatorEndpointIgnoresOnlySpellingDifferences(t *testing.T) {
	for _, same := range [][2]string{
		{"HTTPS://API.Example.COM/", "https://api.example.com"},
		{"https://api.example.com:443", "https://api.example.com"},
		{"wss://connect.example.com:443/", "wss://connect.example.com"},
		{"http://127.0.0.1:80", "http://127.0.0.1"},
		{"ws://localhost:80", "ws://localhost"},
		{"https://api.example.com./v1/", "https://api.example.com/v1"},
		{"https://[2001:db8::1]:443", "https://[2001:db8::1]"},
	} {
		if normalizedOperatorEndpoint(same[0]) != normalizedOperatorEndpoint(same[1]) {
			t.Errorf("%s and %s should match", same[0], same[1])
		}
	}
	for _, different := range [][2]string{
		{"https://api.example.com:8443", "https://api.example.com"},
		{"http://api.example.com", "https://api.example.com"},
		{"https://api.example.com/v1", "https://api.example.com/V1"},
		{"https://api.example.com/v1", "https://api.example.com"},
		{"https://api.example.com:80", "https://api.example.com"},
		{"not a url", "https://not"},
	} {
		if normalizedOperatorEndpoint(different[0]) == normalizedOperatorEndpoint(different[1]) {
			t.Errorf("%s and %s should differ", different[0], different[1])
		}
	}
}

func TestOperatorListReporterReportsEachPublishedList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operators-report.json")
	source, log := newOperatorTestSource(), newOperatorTestLog()
	pinned := []OperatorConfig{{NoID: 1, APIURL: "https://api.one.example", ConnectURL: "wss://connect.one.example"}}
	reporter := newOperatorListReporter(t.Context(), source, operatorListReportSettings{pinned: pinned, url: "https://list.example/operators.yml", path: path, log: log.log})
	defer reporter.Close()
	log.wait(t, "operator list from https://list.example/operators.yml is not loaded yet")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a report was written before any list", err)
	}
	source.publish(operatorTestOperator("one"))
	log.wait(t, "pinned no_id 1 https://api.one.example is listed as one.example")
	source.publish(operatorTestOperator("two"))
	log.wait(t, "pinned no_id 1 https://api.one.example is delisted")
	log.wait(t, "listed operator two.example is not pinned by the config")
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), `"listed": false`) || !strings.Contains(string(raw), `"two.example"`) {
		t.Fatalf("report file does not follow the latest list: %s %v", raw, err)
	}
}
