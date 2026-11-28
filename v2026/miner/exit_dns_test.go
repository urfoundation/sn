package miner

import (
	"bytes"
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/connect/v2026"
)

// Synthetic DNS over HTTPS servers, at documentation addresses.
var minerTestDohUrls = []string{
	"https://192.0.2.1/dns-query",
	"https://198.51.100.2/dns-query",
	"https://203.0.113.3/dns-query",
}

// minerTestExitDns makes the servers whose hosts answering names answer, and
// records what each was asked.
func minerTestExitDns(t *testing.T, answering map[string]bool) *[]string {
	t.Helper()
	asked := &[]string{}
	var stateLock sync.Mutex
	previous := minerExitDnsQuery
	minerExitDnsQuery = func(ctx context.Context, dohUrl string, name string) bool {
		func() {
			stateLock.Lock()
			defer stateLock.Unlock()
			*asked = append(*asked, dohUrl+" "+name)
		}()
		if _, ok := ctx.Deadline(); !ok {
			t.Error("an exit DNS query without the budget's deadline")
		}
		for host, answers := range answering {
			if answers && strings.Contains(dohUrl, "//"+host+"/") {
				return true
			}
		}
		return false
	}
	t.Cleanup(func() { minerExitDnsQuery = previous })
	return asked
}

// A network that blocks DNS over HTTPS fails every lookup through the
// provider at DNS, as the quality check's dial_dns does for providers in
// some countries; doctor says so, with what does and doesn't fix it. One
// server that answers is enough, since clients race them.
func TestDoctorExitDnsFailsOnlyWhenNoServerAnswers(t *testing.T) {
	timeout := 5 * time.Second
	asked := minerTestExitDns(t, map[string]bool{})
	result := checkMinerExitDns(t.Context(), timeout, minerTestDohUrls)
	if result.ok() || len(*asked) != len(minerTestDohUrls) {
		t.Fatalf("no server answered: ok=%v asked=%v", result.ok(), *asked)
	}
	for _, query := range *asked {
		if !strings.HasSuffix(query, " "+minerExitDnsName) {
			t.Fatalf("asked %q", query)
		}
	}
	if detail := result.detail(); !strings.Contains(detail, "none of the 3 encrypted DNS servers") || !strings.Contains(detail, "within 5s") {
		t.Fatalf("detail %q", detail)
	}
	if summary := result.summary(); !strings.Contains(summary, "at DNS") || !strings.Contains(summary, "192.0.2.1, 198.51.100.2, 203.0.113.3") {
		t.Fatalf("summary %q", summary)
	}
	if fix := result.fix(false); !strings.Contains(fix, "provider proxy add <host:port>") || strings.Contains(fix, "proxy slots' exits") {
		t.Fatalf("fix %q", fix)
	}
	if fix := result.fix(true); !strings.Contains(fix, "proxy slots' exits leave through their proxies") {
		t.Fatalf("fix with proxy slots %q", fix)
	}

	finding := result.finding(time.Now())
	if finding == nil || finding.Code != minerFindingExitDnsBlocked || !strings.HasPrefix(finding.Summary, "None of the 3") || !strings.Contains(finding.Fix, "provider proxy add") {
		t.Fatalf("finding %+v", finding)
	}

	minerTestExitDns(t, map[string]bool{"198.51.100.2": true})
	result = checkMinerExitDns(t.Context(), timeout, minerTestDohUrls)
	if !result.ok() || !strings.Contains(result.detail(), "1 of the 3") || !strings.Contains(result.detail(), "no answer from 192.0.2.1, 203.0.113.3") || result.finding(time.Now()) != nil {
		t.Fatalf("one server answered: %q", result.detail())
	}
	minerTestExitDns(t, map[string]bool{"192.0.2.1": true, "198.51.100.2": true, "203.0.113.3": true})
	if result := checkMinerExitDns(t.Context(), timeout, minerTestDohUrls); !result.ok() || !strings.HasPrefix(result.detail(), "all 3 encrypted DNS servers") {
		t.Fatalf("every server answered: %q", result.detail())
	}
}

// doctor shows the row, and counts it a problem, at its real entry point.
func TestDoctorShowsExitDns(t *testing.T) {
	dir := phase2State(t)
	updaterTestServices(t)
	writeState(t, dir, "jwt", fakeJwt(map[string]any{"network_name": "synthetic-network"}))
	previousNetwork, previousRelease := doctorNetwork, doctorRelease
	t.Cleanup(func() { doctorNetwork, doctorRelease = previousNetwork, previousRelease })
	doctorNetwork = func(string) []doctorCheck { return []doctorCheck{{true, "Network", "skipped in tests"}} }
	doctorRelease = func(context.Context) (*minerRelease, error) { return nil, context.DeadlineExceeded }

	minerTestExitDns(t, map[string]bool{})
	var out bytes.Buffer
	code := doctorCmd(docopt.Opts{}, &out)
	got := out.String()
	for _, want := range []string{"✗ Exit DNS     none of the", "DNS over HTTPS", "provider proxy add <host:port>", "1 problem found."} {
		if !strings.Contains(got, want) {
			t.Errorf("doctor lacks %q:\n%s", want, got)
		}
	}
	if code != 1 {
		t.Fatalf("doctor exit %d", code)
	}

	// the first of the servers clients use
	first, err := url.Parse(connect.DefaultDnsResolverSettings().RemoteDohUrlsIpv4[0])
	if err != nil {
		t.Fatal(err)
	}
	minerTestExitDns(t, map[string]bool{first.Hostname(): true})
	out.Reset()
	if code := doctorCmd(docopt.Opts{}, &out); code != 0 || !strings.Contains(out.String(), "✓ Exit DNS") {
		t.Fatalf("doctor exit %d:\n%s", code, out.String())
	}
}

// The running miner's check sets its finding when no server answers and
// reports it, clears it once one does, and changes nothing for a stopped run.
func TestMinerExitDnsCheckSetsAndClearsItsFinding(t *testing.T) {
	reports := []minerFinding{}
	check := &minerExitDnsCheck{
		settings: &minerExitDnsCheckSettings{QueryTimeout: 5 * time.Second},
		dohUrls:  minerTestDohUrls,
		findings: newMinerFindings(),
		report:   func(finding minerFinding) { reports = append(reports, finding) },
	}
	minerTestExitDns(t, map[string]bool{})
	check.check(t.Context())
	if current := check.findings.current(); len(current) != 1 || current[0].Code != minerFindingExitDnsBlocked || len(reports) != 1 || !strings.Contains(reports[0].Title, "DNS lookups through this device fail") {
		t.Fatalf("no server answered: findings %+v, reports %+v", current, reports)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	minerTestExitDns(t, map[string]bool{"203.0.113.3": true})
	check.check(canceled)
	if len(check.findings.current()) != 1 {
		t.Fatal("a stopped run's check changed the finding")
	}
	check.check(t.Context())
	if current := check.findings.current(); len(current) != 0 || len(reports) != 1 {
		t.Fatalf("a server answered: findings %+v, reports %d", current, len(reports))
	}
}

// The loop checks FirstCheckDelay after it starts, and its stop joins it.
func TestMinerExitDnsCheckLoopChecksAndJoins(t *testing.T) {
	minerTestExitDns(t, map[string]bool{})
	reported := make(chan minerFinding, 1)
	check := &minerExitDnsCheck{
		settings: &minerExitDnsCheckSettings{FirstCheckDelay: 0, CheckInterval: time.Hour, QueryTimeout: 5 * time.Second},
		dohUrls:  minerTestDohUrls,
		findings: newMinerFindings(),
		report:   func(finding minerFinding) { reported <- finding },
	}
	stop := check.start(t.Context())
	select {
	case finding := <-reported:
		if finding.Code != minerFindingExitDnsBlocked {
			t.Fatalf("finding %+v", finding)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the exit DNS check never checked")
	}
	stop()
}
