// Claim expectations are supplied independently of the queue and HTTP source.
// This observer retains assertions; it grants no finality or payment authority.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/protocol"
)

const maxMonitorClaims = 4
const maxMonitorClaimEpochs = 16

type monitorClaimEpochPolicy struct {
	Epoch        int64  `json:"epoch"`
	ShareBps     uint64 `json:"share_bps"`
	AcceptBy     string `json:"accept_by"`
	PayoutRoot   string `json:"payout_root,omitempty"`
	ArtifactHash string `json:"artifact_hash,omitempty"`
}

type monitorClaimPolicy struct {
	Role                 string                        `json:"role"`
	Endpoint             string                        `json:"endpoint"`
	ExpectedMember       string                        `json:"expected_member"`
	ExpectedPool         protocol.ClaimProgressPool    `json:"expected_pool"`
	FreshnessSeconds     uint64                        `json:"freshness_seconds"`
	Epochs               []monitorClaimEpochPolicy     `json:"epochs"`
	ReadBudgetSeconds    uint64                        `json:"read_budget_seconds,omitempty"`
	EpochCapacity        uint64                        `json:"epoch_capacity,omitempty"`
	ReviewHistoryEntries uint64                        `json:"review_history_entries,omitempty"`
	Renewal              *monitorProgressPolicyRenewal `json:"renewal,omitempty"`
	HistoryCatalog       *monitorHistoryCatalogPolicy  `json:"history_catalog,omitempty"`
	Window               *monitorClaimWindowPolicy     `json:"window,omitempty"`
	// An owner-local diagnostic observes actual work; it supplies no evidence,
	// verdict, cache entry or serialized policy field.
	work func(stage string, units uint64)
}

func (self monitorClaimPolicy) observeWork(stage string, units uint64) {
	if self.work != nil {
		self.work(stage, units)
	}
}

func (self monitorClaimPolicy) validate(expected identityExpectation) error {
	endpoint, err := url.Parse(self.Endpoint)
	if err != nil || len(self.Endpoint) > 2048 || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || endpoint.Path != "/claim-progress" || endpoint.RawPath != "" {
		return errors.New("claim endpoint must be a bounded canonical progress URL")
	}
	address, addressErr := netip.ParseAddr(endpoint.Hostname())
	if endpoint.Scheme != "https" && (endpoint.Scheme != "http" || addressErr != nil || !address.IsLoopback()) {
		return errors.New("claim endpoint requires HTTPS or literal loopback HTTP")
	}
	if !monitorRolePattern.MatchString(self.Role) || !protocol.ValidProviderSlot(self.ExpectedMember) || self.ExpectedPool.Validate() != nil || self.ExpectedPool.ChainId != expected.EvmChainId || validateMonitorProgressRenewal(self.resources(), self.Renewal, true) != nil {
		return errors.New("claim policy requires independent bounded member, pool, freshness and epochs")
	}
	seen := map[int64]bool{}
	for _, epoch := range self.Epochs {
		self.observeWork("policy-epoch", 1)
		deadline, err := time.Parse(time.RFC3339Nano, epoch.AcceptBy)
		if epoch.Epoch < 0 || seen[epoch.Epoch] || epoch.ShareBps == 0 || epoch.ShareBps > 10000 || err != nil || deadline.IsZero() {
			return errors.New("claim expectation needs a unique epoch, exact share and absolute acceptance deadline")
		}
		seen[epoch.Epoch] = true
		for _, hash := range []string{epoch.PayoutRoot, epoch.ArtifactHash} {
			if hash == "" {
				continue
			}
			if len(hash) != 66 || !strings.HasPrefix(hash, "0x") || strings.ToLower(hash) != hash {
				return errors.New("claim expected artifact must be an exact canonical hash")
			}
			if _, err := hex.DecodeString(hash[2:]); err != nil {
				return err
			}
		}
	}
	return errors.Join(self.HistoryCatalog.validate(), self.Window.validate())
}

func (self monitorClaimPolicy) hash() string {
	raw, _ := json.Marshal(self)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func monitorClaimPaths(checkpoint, metrics, role string) (string, string) {
	return strings.TrimSuffix(checkpoint, ".json") + ".claim-" + role + ".json", strings.TrimSuffix(metrics, ".prom") + ".claim-" + role + ".prom"
}

// Complete identity contradictions dominate a simultaneous body-close outage.
// An API no-claim assertion has no observed chain domain to compare.
func monitorClaimIdentity(policy monitorClaimPolicy, value *protocol.ClaimProgress) string {
	if value.Member != policy.ExpectedMember || value.DeclaredPool != nil && *value.DeclaredPool != policy.ExpectedPool {
		return "identity"
	}
	for _, entry := range value.Entries {
		observation := entry.Observation
		if observation == nil || observation.EvidenceKind == "api-no-claim" {
			continue
		}
		if observation.Pool != policy.ExpectedPool {
			return "identity"
		}
		for _, expected := range policy.Epochs {
			policy.observeWork("identity-epoch-comparison", 1)
			if entry.Epoch != expected.Epoch {
				continue
			}
			if observation.ShareBps != expected.ShareBps || observation.EvidenceKind == "finalized-leaf" && (expected.PayoutRoot != "" && observation.PayoutRoot != expected.PayoutRoot || expected.ArtifactHash != "" && observation.ArtifactHash != expected.ArtifactHash) {
				return "identity"
			}
		}
	}
	if value.DeclaredPool == nil {
		return "unknown"
	}
	return "ok"
}

func readMonitorClaim(ctx context.Context, client *http.Client, policy monitorClaimPolicy) (*protocol.ClaimProgress, string) {
	return readMonitorClaimWithBudget(ctx, client, policy, policy.resources().readBudget(), monitorProgressReadClock{})
}

func readMonitorClaimWithBudget(ctx context.Context, client *http.Client, policy monitorClaimPolicy, budget time.Duration, clock monitorProgressReadClock) (*protocol.ClaimProgress, string) {
	return readMonitorProgress(ctx, budget, clock, func(attemptCtx context.Context, attempt *monitorProgressReadAttempt) (*protocol.ClaimProgress, string) {
		return readMonitorClaimAttempt(attemptCtx, client, policy, attempt)
	})
}

func readMonitorClaimAttempt(ctx context.Context, client *http.Client, policy monitorClaimPolicy, attempt *monitorProgressReadAttempt) (*protocol.ClaimProgress, string) {
	if ctx == nil || ctx.Err() != nil {
		return nil, "unavailable"
	}
	endpoint, err := url.Parse(policy.Endpoint)
	if err != nil {
		return nil, "invalid"
	}
	endpoint.RawQuery = url.Values{"id": []string{policy.ExpectedMember}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, "invalid"
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-cache")
	response, requestErr := client.Do(request)
	attempt.retryable = monitorProgressRetryTransport(requestErr)
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
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusServiceUnavailable {
		attempt.observeErrors(requestErr, response.Body.Close(), ctx.Err())
		if response.StatusCode >= 400 && response.StatusCode < 500 && !monitorProgressRetryStatus(response.StatusCode) {
			return nil, "invalid"
		}
		return nil, "unavailable"
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, protocol.MaxClaimProgressBytes+1))
	closeErr := response.Body.Close()
	if len(raw) > protocol.MaxClaimProgressBytes {
		return nil, "invalid"
	}
	value, decodeErr := protocol.DecodeClaimProgress(raw)
	if readErr == nil && decodeErr != nil && response.StatusCode == http.StatusOK {
		return nil, "invalid"
	}
	if decodeErr == nil && monitorClaimIdentity(policy, value) == "identity" {
		return value, "identity"
	}
	if requestErr != nil || readErr != nil || closeErr != nil || ctx.Err() != nil {
		attempt.observeErrors(requestErr, readErr, closeErr, ctx.Err())
		return nil, "unavailable"
	}
	if decodeErr != nil {
		if response.StatusCode == http.StatusServiceUnavailable {
			return nil, "unavailable"
		}
		return nil, "invalid"
	}
	if response.StatusCode == http.StatusServiceUnavailable || value.Status == "unavailable" || value.Status == "closed" {
		attempt.retryable = true
		return value, "unavailable"
	}
	if value.Status == "unknown" || value.Sequence == 0 {
		return value, "unknown"
	}
	return value, monitorClaimIdentity(policy, value)
}
