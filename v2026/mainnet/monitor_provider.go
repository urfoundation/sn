// Expected providers come from reviewed local policy. HTTP process liveness,
// traffic and candidate-selected member counts cannot satisfy that policy.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

const maxMonitorProviders = 4
const maxMonitorProviderMembers = 64

type monitorExpectedProviderMember struct {
	Slot     string `json:"slot"`
	ClientId string `json:"client_id"`
}
type monitorProviderPolicy struct {
	Role                 string                          `json:"role"`
	Endpoint             string                          `json:"endpoint"`
	ExpectedSource       protocol.ProviderProgressSource `json:"expected_source"`
	Members              []monitorExpectedProviderMember `json:"members"`
	FreshnessSeconds     uint64                          `json:"freshness_seconds"`
	ReadBudgetSeconds    uint64                          `json:"read_budget_seconds,omitempty"`
	ReviewHistoryEntries uint64                          `json:"review_history_entries,omitempty"`
	Renewal              *monitorProgressPolicyRenewal   `json:"renewal,omitempty"`
}

func (self monitorProviderPolicy) validate() error {
	endpoint, err := url.Parse(self.Endpoint)
	if err != nil || len(self.Endpoint) > 2048 || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "/provider-progress" || endpoint.RawPath != "" {
		return errors.New("provider endpoint must be bounded canonical progress URL")
	}
	address, addressErr := netip.ParseAddr(endpoint.Hostname())
	if endpoint.Scheme != "https" && (endpoint.Scheme != "http" || addressErr != nil || !address.IsLoopback()) {
		return errors.New("provider endpoint requires HTTPS or literal loopback HTTP")
	}
	if !monitorRolePattern.MatchString(self.Role) || self.ExpectedSource.Validate() != nil || validateMonitorProgressRenewal(self.resources(), self.Renewal, false) != nil || len(self.Members) == 0 || len(self.Members) > maxMonitorProviderMembers {
		return errors.New("provider policy requires bounded role, source, freshness and expected members")
	}
	slots, identities := map[string]bool{}, map[string]bool{}
	for _, member := range self.Members {
		if !protocol.ValidProviderSlot(member.Slot) || !protocol.ValidProviderClientId(member.ClientId) || slots[member.Slot] || identities[member.ClientId] {
			return errors.New("provider expected roster has invalid or repeated identities")
		}
		slots[member.Slot], identities[member.ClientId] = true, true
	}
	return nil
}

func (self monitorProviderPolicy) hash() string {
	raw, _ := json.Marshal(self)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func monitorProviderPaths(checkpoint, metrics, role string) (string, string) {
	return strings.TrimSuffix(checkpoint, ".json") + ".provider-" + role + ".json", strings.TrimSuffix(metrics, ".prom") + ".provider-" + role + ".prom"
}

func newMonitorProviderClient() *http.Client {
	// Connection setup can fail quickly, but a healthy slow response needs a
	// useful read window. The request context clips both phases to its owner.
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, ResponseHeaderTimeout: monitorProgressAttemptBudget, TLSHandshakeTimeout: 5 * time.Second, MaxResponseHeaderBytes: 8 * 1024, MaxConnsPerHost: 1, DisableKeepAlives: true}
	return &http.Client{Transport: transport, Timeout: monitorProgressAttemptBudget, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// Body close is synchronous and joined on every path. A complete identity or
// authentication refusal dominates a simultaneous observation/close failure.
func readMonitorProvider(ctx context.Context, client *http.Client, policy monitorProviderPolicy) (*protocol.ProviderProgress, string) {
	return readMonitorProviderWithBudget(ctx, client, policy, policy.resources().readBudget(), monitorProgressReadClock{})
}

func readMonitorProviderWithBudget(ctx context.Context, client *http.Client, policy monitorProviderPolicy, budget time.Duration, clock monitorProgressReadClock) (*protocol.ProviderProgress, string) {
	return readMonitorProgress(ctx, budget, clock, func(attemptCtx context.Context, attempt *monitorProgressReadAttempt) (*protocol.ProviderProgress, string) {
		return readMonitorProviderAttempt(attemptCtx, client, policy, attempt)
	})
}

func readMonitorProviderAttempt(ctx context.Context, client *http.Client, policy monitorProviderPolicy, attempt *monitorProgressReadAttempt) (*protocol.ProviderProgress, string) {
	if ctx == nil || ctx.Err() != nil {
		return nil, "unavailable"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, policy.Endpoint, nil)
	if err != nil {
		return nil, "invalid"
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := client.Do(request)
	attempt.retryable = monitorProgressRetryTransport(err)
	if response == nil {
		return nil, "unavailable"
	}
	attempt.header = response.Header.Clone()
	attempt.retryable = attempt.retryable || monitorProgressRetryStatus(response.StatusCode)
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		if response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, "authentication"
	}
	if response.Body == nil {
		return nil, "invalid"
	}
	if response.StatusCode != http.StatusOK {
		attempt.observeErrors(err, response.Body.Close(), ctx.Err())
		if response.StatusCode >= 400 && response.StatusCode < 500 && !monitorProgressRetryStatus(response.StatusCode) {
			return nil, "invalid"
		}
		return nil, "unavailable"
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, protocol.MaxProviderProgressBytes+1))
	closeErr := response.Body.Close()
	if len(raw) > protocol.MaxProviderProgressBytes {
		return nil, "invalid"
	}
	value, decodeErr := protocol.DecodeProviderProgress(raw)
	if readErr == nil && decodeErr != nil {
		return nil, "invalid"
	}
	if decodeErr == nil {
		if value.Source != policy.ExpectedSource {
			return value, "identity"
		}
		expected := map[string]string{}
		for _, member := range policy.Members {
			expected[member.Slot] = member.ClientId
		}
		for _, member := range value.Members {
			identity, ok := expected[member.Slot]
			if !ok || member.ClientId != "" && member.ClientId != identity {
				return value, "identity"
			}
		}
	}
	if err != nil || readErr != nil || closeErr != nil || ctx.Err() != nil {
		attempt.observeErrors(err, readErr, closeErr, ctx.Err())
		return nil, "unavailable"
	}
	if decodeErr != nil {
		return nil, "invalid"
	}
	if len(value.Members) != len(policy.Members) {
		return value, "missing"
	}
	return value, "ok"
}

// The last accepted instance/sequence/member generations survive observer
// restart. Current readiness never survives restart or a failed observation.
type monitorProviderState struct {
	SampleAt       time.Time                  `json:"sample_at"`
	HighWaterAt    time.Time                  `json:"high_water_at"`
	LastAcceptedAt time.Time                  `json:"last_accepted_at"`
	OutageSince    time.Time                  `json:"outage_since"`
	LastIssueAt    time.Time                  `json:"last_issue_at"`
	LastIssue      string                     `json:"last_issue"`
	Incidents      uint64                     `json:"incidents"`
	Restarts       uint64                     `json:"restarts"`
	Status         string                     `json:"status"`
	Record         *protocol.ProviderProgress `json:"record,omitempty"`
	current        bool
	ready          int
}

func (self *monitorProviderState) observe(policy monitorProviderPolicy, value *protocol.ProviderProgress, code string, now time.Time) {
	self.current, self.ready = false, 0
	if !self.HighWaterAt.IsZero() && now.Before(self.HighWaterAt) && code != "identity" && code != "authentication" {
		code = "clock"
	}
	self.SampleAt = now
	if now.After(self.HighWaterAt) {
		self.HighWaterAt = now
	}
	if code == "ok" {
		observed, _ := time.Parse(time.RFC3339Nano, value.ObservedAt)
		started, _ := time.Parse(time.RFC3339Nano, value.StartedAt)
		if observed.After(now.Add(5*time.Second)) || now.Sub(observed) > time.Duration(policy.FreshnessSeconds)*time.Second {
			code = "stale"
		}
		if prior := self.Record; prior != nil {
			priorObserved, _ := time.Parse(time.RFC3339Nano, prior.ObservedAt)
			priorStarted, _ := time.Parse(time.RFC3339Nano, prior.StartedAt)
			if value.InstanceId == prior.InstanceId {
				if value.StartedAt != prior.StartedAt {
					code = "identity"
				} else if value.Sequence <= prior.Sequence || observed.Before(priorObserved) {
					code = "stale"
				}
				generations := map[string]uint64{}
				for _, member := range prior.Members {
					generations[member.Slot] = member.Generation
				}
				for _, member := range value.Members {
					if member.Generation < generations[member.Slot] && code != "identity" {
						code = "stale"
					}
				}
			} else if !started.After(priorStarted) || observed.Before(priorObserved) {
				code = "stale"
			}
		}
	}
	if code == "ok" {
		self.current = true
		for _, member := range value.Members {
			if member.Current && member.Ready {
				self.ready++
			}
		}
		if self.Record != nil && self.Record.InstanceId != value.InstanceId && self.Restarts < math.MaxUint64 {
			self.Restarts++
		}
		copyValue := *value
		copyValue.Members = slices.Clone(value.Members)
		self.Record = &copyValue
		self.LastAcceptedAt = now
		if self.ready != len(policy.Members) {
			code = "not_ready"
		}
	}
	if code == "ok" {
		self.OutageSince = time.Time{}
	} else {
		if self.OutageSince.IsZero() {
			self.OutageSince = now
		}
		if code != self.Status && self.Incidents < math.MaxUint64 {
			self.Incidents++
		}
		self.LastIssue, self.LastIssueAt = code, now
	}
	self.Status = code
}

func monitorProviderTerminal(code string) bool { return code == "identity" || code == "authentication" }
