// The drift between the operators a signed release config pins and the
// published operator list. The list says only where operators serve; it never
// changes what is signed, weighted or kept as evidence. The report makes the
// difference visible: in the log at startup and at each published list, and
// in a small private JSON file beside the list cache. Nothing reads the file
// back.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/operatorlist"
)

const operatorListReportSchema = "urnetwork-validator-operator-list-report-v1"

// Entries per section in the file. The log names every entry.
const operatorListReportEntries = 16

type operatorListReportList struct {
	Url       string `json:"url"`
	Sha256    string `json:"sha256"`
	FetchedAt string `json:"fetched_at"`
	Cached    bool   `json:"cached"`
}

type operatorListReportPinned struct {
	NoId               uint64 `json:"no_id"`
	ApiUrl             string `json:"api_url"`
	Listed             bool   `json:"listed"`
	Domain             string `json:"domain,omitempty"`
	ConnectUrlMismatch bool   `json:"connect_url_mismatch"`
	// Log detail only.
	connectUrl       string
	listedConnectUrl string
}

// The complete drift. encode caps each section for the file.
type operatorListReport struct {
	Schema          string                     `json:"schema"`
	GeneratedAt     string                     `json:"generated_at"`
	List            operatorListReportList     `json:"list"`
	Pinned          []operatorListReportPinned `json:"pinned"`
	PinnedOmitted   int                        `json:"pinned_omitted"`
	Unpinned        []string                   `json:"unpinned"`
	UnpinnedOmitted int                        `json:"unpinned_omitted"`
	listed          int
	production      bool
}

// Pinned operators match the list by normalized api_url, in config order;
// unpinned names the listed operators none of them matches, in list order.
// Flag mode passes no pinned operators.
func newOperatorListReport(pinned []OperatorConfig, snapshot *operatorlist.Snapshot, listUrl string, generatedAt time.Time) operatorListReport {
	report := operatorListReport{
		Schema:      operatorListReportSchema,
		GeneratedAt: generatedAt.UTC().Format(time.RFC3339Nano),
		List: operatorListReportList{
			Url:       listUrl,
			Sha256:    snapshot.Sha256,
			FetchedAt: snapshot.FetchedAt.UTC().Format(time.RFC3339Nano),
			Cached:    snapshot.Cached,
		},
		Pinned:     []operatorListReportPinned{},
		Unpinned:   []string{},
		listed:     len(snapshot.List.Operators),
		production: len(pinned) != 0,
	}
	listed := map[string]operatorlist.Operator{}
	for _, operator := range snapshot.List.Operators {
		key := normalizedOperatorEndpoint(operator.ApiUrl)
		if _, ok := listed[key]; !ok {
			listed[key] = operator
		}
	}
	for _, operator := range pinned {
		entry := operatorListReportPinned{NoId: operator.NoID, ApiUrl: operator.APIURL, connectUrl: operator.ConnectURL}
		if match, ok := listed[normalizedOperatorEndpoint(operator.APIURL)]; ok {
			entry.Listed, entry.Domain, entry.listedConnectUrl = true, match.Domain, match.ConnectUrl
			entry.ConnectUrlMismatch = normalizedOperatorEndpoint(match.ConnectUrl) != normalizedOperatorEndpoint(operator.ConnectURL)
		}
		report.Pinned = append(report.Pinned, entry)
	}
	endpoints := pinnedOperatorEndpoints(pinned)
	for _, operator := range snapshot.List.Operators {
		if !endpoints[normalizedOperatorEndpoint(operator.ApiUrl)] {
			report.Unpinned = append(report.Unpinned, operator.Domain)
		}
	}
	return report
}

// The normalized api_url of every pinned operator. Observation and the report
// share it, so a listed operator is unpinned in both or in neither.
func pinnedOperatorEndpoints(pinned []OperatorConfig) map[string]bool {
	endpoints := map[string]bool{}
	for _, operator := range pinned {
		endpoints[normalizedOperatorEndpoint(operator.APIURL)] = true
	}
	return endpoints
}

// Endpoint identity for matching config URLs to list URLs. Scheme and host
// case, a trailing dot or slash and the scheme's default port don't
// distinguish endpoints. An unparseable value matches only itself.
func normalizedOperatorEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	port := u.Port()
	switch {
	case port == "443" && (scheme == "https" || scheme == "wss"), port == "80" && (scheme == "http" || scheme == "ws"):
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host + strings.TrimSuffix(u.EscapedPath(), "/")
}

// The file form, at most operatorListReportEntries per section.
func (self operatorListReport) encode() ([]byte, error) {
	if len(self.Pinned) > operatorListReportEntries {
		self.Pinned, self.PinnedOmitted = self.Pinned[:operatorListReportEntries], len(self.Pinned)-operatorListReportEntries
	}
	if len(self.Unpinned) > operatorListReportEntries {
		self.Unpinned, self.UnpinnedOmitted = self.Unpinned[:operatorListReportEntries], len(self.Unpinned)-operatorListReportEntries
	}
	raw, err := json.MarshalIndent(self, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// One line for the list, then one per pinned operator and one per unpinned
// listed operator. Flag mode names the listed operators on the first line.
func (self operatorListReport) lines() []string {
	source := "fetched"
	if self.List.Cached {
		source = "cached"
	}
	summary := fmt.Sprintf("operator list %s from %s (%s %s): %d listed", self.List.Sha256, self.List.Url, source, self.List.FetchedAt, self.listed)
	if !self.production {
		return []string{summary + ": " + strings.Join(self.Unpinned, ", ")}
	}
	lines := []string{summary}
	for _, pinned := range self.Pinned {
		switch {
		case !pinned.Listed:
			lines = append(lines, fmt.Sprintf("pinned no_id %d %s is delisted", pinned.NoId, pinned.ApiUrl))
		case pinned.ConnectUrlMismatch:
			lines = append(lines, fmt.Sprintf("pinned no_id %d %s is listed as %s, but its connect_url %s differs from the listed %s", pinned.NoId, pinned.ApiUrl, pinned.Domain, pinned.connectUrl, pinned.listedConnectUrl))
		default:
			lines = append(lines, fmt.Sprintf("pinned no_id %d %s is listed as %s", pinned.NoId, pinned.ApiUrl, pinned.Domain))
		}
	}
	for _, domain := range self.Unpinned {
		lines = append(lines, fmt.Sprintf("listed operator %s is not pinned by the config", domain))
	}
	return lines
}

// Written like the list cache: a private temporary file is synced, renamed
// over the previous report, then the directory is synced.
func writeOperatorListReport(path string, report operatorListReport) (returnErr error) {
	raw, err := report.encode()
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if returnErr != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0600); err != nil {
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	parent, err := os.Open(directory)
	if err != nil {
		return err
	}
	return errors.Join(parent.Sync(), parent.Close())
}

type operatorListReportSettings struct {
	// Nil in flag mode.
	pinned []OperatorConfig
	url    string
	// The report file; empty writes none.
	path string
	now  func() time.Time
	log  func(string)
}

// Reports each published list until closed. A failed write is logged and
// retried at the next list; it never reaches the runners or the release.
type operatorListReporter struct {
	source   operatorListSource
	settings operatorListReportSettings
	cancel   context.CancelFunc
	done     chan struct{}
}

func newOperatorListReporter(ctx context.Context, source operatorListSource, settings operatorListReportSettings) *operatorListReporter {
	if settings.now == nil {
		settings.now = time.Now
	}
	if settings.log == nil {
		settings.log = logOperatorLine
	}
	ctx, cancel := context.WithCancel(ctx)
	self := &operatorListReporter{source: source, settings: settings, cancel: cancel, done: make(chan struct{})}
	go self.run(ctx)
	return self
}

func (self *operatorListReporter) run(ctx context.Context) {
	defer close(self.done)
	for {
		snapshot, update := self.source.Snapshot()
		if snapshot == nil {
			self.settings.log(fmt.Sprintf("operator list from %s is not loaded yet", self.settings.url))
		} else {
			report := newOperatorListReport(self.settings.pinned, snapshot, self.settings.url, self.settings.now())
			// The file precedes the log, so a logged report is already on disk.
			if self.settings.path != "" {
				if err := writeOperatorListReport(self.settings.path, report); err != nil {
					self.settings.log(fmt.Sprintf("operator list report %s was not written: %v", self.settings.path, err))
				}
			}
			for _, line := range report.lines() {
				self.settings.log(line)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-update:
		}
	}
}

func (self *operatorListReporter) Close() {
	self.cancel()
	<-self.done
}

func logOperatorLine(line string) {
	fmt.Fprintln(os.Stderr, "validator: "+line)
}
