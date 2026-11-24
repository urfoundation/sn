package miner

// The exit DNS check, in provider doctor's Exit DNS row and in the running
// miner. A client resolves the names it visits through its provider with DNS
// over HTTPS to well-known public servers (connect.DefaultDnsResolverSettings'
// RemoteDohUrlsIpv4), and so does the network's quality check: its URL loads
// resolve in the tunnel, within a 5-second DNS budget, and a provider whose
// exit doesn't answer fails them at dial_dns and ranks low. A network that
// blocks or slows those servers, as some countries' and ISPs' do, makes every
// lookup through the provider fail, and nothing in the provider's own logs
// said so. The check asks each server for one name straight from this host,
// the way a client's lookup leaves this device's exit, with the same budget,
// and fails only when none answers: clients race the servers, so one that
// answers is enough.

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/urnetwork/connect"
)

// The name doctor asks each server for: benign and answered everywhere, the
// name connect warms its servers with.
const minerExitDnsName = "example.com"

// The largest DNS message, which bounds an answer's read.
const minerExitDnsMaxMessageBytes = 64 * 1024

// Asks the DNS over HTTPS server at dohUrl for the
// addresses of name, in the RFC 8484 wire format clients send, over TLS
// verified as connect's DNS over HTTPS client verifies it (its resolver
// settings' TLS configuration, else the system's roots), and reports whether
// an answer with an address came within ctx. A variable so tests stay off
// the network.
var minerExitDnsQuery = func(ctx context.Context, dohUrl string, name string) bool {
	question, err := dnsmessage.NewName(strings.TrimSuffix(name, ".") + ".")
	if err != nil {
		return false
	}
	// id 0, as RFC 8484 asks of a DoH query
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{RecursionDesired: true})
	if builder.StartQuestions() != nil || builder.Question(dnsmessage.Question{Name: question, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}) != nil {
		return false
	}
	query, err := builder.Finish()
	if err != nil {
		return false
	}
	tlsConfig := &tls.Config{}
	if resolverTlsConfig := connect.DefaultDnsResolverSettings().TlsConfig; resolverTlsConfig != nil {
		tlsConfig = resolverTlsConfig.Clone()
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true}}
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, dohUrl+"?dns="+base64.RawURLEncoding.EncodeToString(query), nil)
	if err != nil {
		return false
	}
	request.Header.Set("Accept", "application/dns-message")
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	answer, err := io.ReadAll(io.LimitReader(response.Body, minerExitDnsMaxMessageBytes))
	if err != nil {
		return false
	}
	var parser dnsmessage.Parser
	header, err := parser.Start(answer)
	if err != nil || header.RCode != dnsmessage.RCodeSuccess || parser.SkipAllQuestions() != nil {
		return false
	}
	_, err = parser.AnswerHeader()
	return err == nil
}

// What the servers answered.
type minerExitDnsResult struct {
	// the servers' hosts, in their order
	hosts []string
	// the hosts that answered in time
	answered map[string]bool
	// each server's budget
	timeout time.Duration
}

// Asks every server at once, each within timeout.
func checkMinerExitDns(ctx context.Context, timeout time.Duration, dohUrls []string) minerExitDnsResult {
	result := minerExitDnsResult{answered: map[string]bool{}, timeout: timeout}
	var stateLock sync.Mutex
	var wg sync.WaitGroup
	for _, dohUrl := range dohUrls {
		host := dohUrl
		if parsed, err := url.Parse(dohUrl); err == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
		}
		result.hosts = append(result.hosts, host)
		wg.Add(1)
		go func() {
			defer wg.Done()
			queryCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			if minerExitDnsQuery(queryCtx, dohUrl, minerExitDnsName) {
				func() {
					stateLock.Lock()
					defer stateLock.Unlock()
					result.answered[host] = true
				}()
			}
		}()
	}
	wg.Wait()
	return result
}

// One answer is enough: clients race the servers.
func (self minerExitDnsResult) ok() bool {
	return 0 < len(self.answered) || len(self.hosts) == 0
}

// The row's detail.
func (self minerExitDnsResult) detail() string {
	switch {
	case len(self.hosts) == 0:
		return "no encrypted DNS servers to check"
	case len(self.answered) == len(self.hosts):
		return fmt.Sprintf("all %d encrypted DNS servers clients resolve names with through this device answer from here", len(self.hosts))
	case self.ok():
		silent := []string{}
		for _, host := range self.hosts {
			if !self.answered[host] {
				silent = append(silent, host)
			}
		}
		return fmt.Sprintf("%d of the %d encrypted DNS servers clients resolve names with answer from here; clients use those (no answer from %s)", len(self.answered), len(self.hosts), strings.Join(silent, ", "))
	}
	return fmt.Sprintf("none of the %d encrypted DNS servers clients resolve names with through this device answers from here within %s", len(self.hosts), cliDuration(self.timeout))
}

// Why a failed check matters.
func (self minerExitDnsResult) summary() string {
	return "Every name a client looks up through this device fails, and so do the network quality check's test sites, at DNS, so the network ranks this device low and offers it less. This network blocks or slows DNS over HTTPS (TCP 443 to " + strings.Join(self.hosts, ", ") + ")."
}

// The fix of a failed check; proxied says this miner has proxy slots, whose
// exits leave through their proxies, not this host's network.
func (self minerExitDnsResult) fix(proxied bool) string {
	fix := "Nothing on this device changes how its network treats DNS over HTTPS. Run the miner on a network that allows it,\n" +
		"or give it a proxy slot through a network that does: provider proxy add <host:port>"
	if proxied {
		fix += "\nThis checked the host's own network; the proxy slots' exits leave through their proxies."
	}
	return fix
}

// The finding of a failed check, nil for one that passed.
func (self minerExitDnsResult) finding(now time.Time) *minerFinding {
	if self.ok() {
		return nil
	}
	return &minerFinding{
		Code:    minerFindingExitDnsBlocked,
		Title:   "Clients' DNS lookups through this device fail",
		Summary: strings.ToUpper(self.detail()[:1]) + self.detail()[1:] + ". " + self.summary(),
		Fix:     self.fix(false),
		FoundAt: now.UTC(),
	}
}

// The running miner's exit DNS check: like doctor's row, a while after start
// and then now and again, in a run with a direct slot (a proxy slot's exit
// leaves through its proxy, which this host can't check).
type minerExitDnsCheckSettings struct {
	// FirstCheckDelay is the wait after start before the first check: 3
	// minutes, once the run's own connections have settled. Non-positive
	// checks at once.
	FirstCheckDelay time.Duration
	// CheckInterval is the wait between checks: 6 hours. Non-positive checks
	// only once.
	CheckInterval time.Duration
	// QueryTimeout bounds each server's answer: 5 seconds, the network
	// quality check's DNS budget through a provider (dial_dns).
	QueryTimeout time.Duration
}

func defaultMinerExitDnsCheckSettings() *minerExitDnsCheckSettings {
	return &minerExitDnsCheckSettings{
		FirstCheckDelay: 3 * time.Minute,
		CheckInterval:   6 * time.Hour,
		QueryTimeout:    5 * time.Second,
	}
}

// One run's exit DNS check.
type minerExitDnsCheck struct {
	settings *minerExitDnsCheckSettings
	dohUrls  []string
	// set while no server answers, cleared once one does
	findings *minerFindings
	// tells the operator of a finding
	report func(minerFinding)
}

// Asks the servers once. No answer sets the finding and reports it; an
// answer clears it.
func (self *minerExitDnsCheck) check(ctx context.Context) {
	result := checkMinerExitDns(ctx, self.settings.QueryTimeout, self.dohUrls)
	if ctx.Err() != nil {
		// a stopped run's unanswered queries say nothing of the network
		return
	}
	finding := result.finding(time.Now())
	if finding == nil {
		self.findings.clear(minerFindingExitDnsBlocked)
		return
	}
	self.findings.set(*finding)
	if self.report != nil {
		self.report(*finding)
	}
}

// Checks FirstCheckDelay after it is called and then every
// CheckInterval, until ctx ends or stop is called. stop joins the loop.
func (self *minerExitDnsCheck) start(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		wait := self.settings.FirstCheckDelay
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			self.check(ctx)
			if self.settings.CheckInterval <= 0 {
				return
			}
			wait = self.settings.CheckInterval
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
