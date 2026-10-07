// Miner daemon diagnostics borrow one bounded exporter. Callback output never
// waits on stdout and never retains credentials, identities or raw errors.
package miner

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/urfoundation/sn/v2026/diagnostics"
)

const providerDiagnosticSchema = "urnetwork-provider-diagnostics-v1"

// Fixed events select their own domain; provider indices are numeric facts,
// never dynamic queue names or labels. These facts grant no serving authority.
type providerDiagnosticEvent uint8

const (
	providerAuthenticationWait providerDiagnosticEvent = iota
	providerJwtSaveFailed
	providerRejectionSaveFailed
	providerAuthenticationRejected
	providerClientKeySaveFailed
	providerTlsKeySaveFailed
	providerExtenderKeySaveFailed
	providerExtenderIdentityInvalid
	providerIdentityReady
	providerExtenderObserved
	providerStarted
	providerWorkerFailed
	providerStatusStarted
	providerStatusFailed
	providerStatusNotice
	providerStartupRecoveryRequired
	providerWorkCaptureRequired
	providerContractCaptureRequired
)

// Immutable scalar status can be compared without formatting SDK error strings.
type providerExtenderObservation struct {
	Known            bool `json:"known"`
	Enabled          bool `json:"enabled"`
	Listening        bool `json:"listening"`
	ListenFailed     bool `json:"listen_failed"`
	ActivatedV4      bool `json:"activated_v4"`
	ActivatedV6      bool `json:"activated_v6"`
	ActivationFailed bool `json:"activation_failed"`
	Revoked          bool `json:"revoked"`
}

// Optional operational delivery evidence is independent of status=ok, which
// retains its existing process-liveness meaning. Counters describe one run.
type providerDiagnosticStatus struct {
	Schema         string               `json:"schema"`
	Authentication diagnostics.Snapshot `json:"authentication"`
	Keys           diagnostics.Snapshot `json:"keys"`
	Extender       diagnostics.Snapshot `json:"extender"`
	Runtime        diagnostics.Snapshot `json:"runtime"`
}

// Tests observe actual construction and joined closure, without replacing the
// authentication, file or lifecycle operations that own these events.
type providerDiagnosticHooks struct {
	afterCreate func(*providerDiagnostics)
	afterClose  func(*providerDiagnostics, error)
}

// A private key avoids collisions with protocol and authentication contexts.
type providerDiagnosticHooksKey struct{}

// Concurrent producers share only this explicit instance's four fixed queues.
type providerDiagnostics struct{ exporter *diagnostics.Exporter }

// Refused sinks remain visible through snapshots; no synchronous fallback is
// attempted. The owning run must close this object on every subsequent exit.
func newProviderDiagnostics(ctx context.Context, writer io.Writer) (*providerDiagnostics, error) {
	exporter, err := diagnostics.New(ctx, writer, []string{"authentication", "keys", "extender", "runtime"})
	if err != nil {
		return nil, err
	}
	return &providerDiagnostics{exporter: exporter}, nil
}

// The original sink remains caller-owned; exporter descriptors and worker join.
func (self *providerDiagnostics) close() error {
	if self == nil {
		return nil
	}
	return self.exporter.Close()
}

// Missing publication is omitted, never synthesized as successful delivery.
func (self *providerDiagnostics) snapshot() *providerDiagnosticStatus {
	if self == nil {
		return nil
	}
	return &providerDiagnosticStatus{Schema: providerDiagnosticSchema, Authentication: self.exporter.Snapshot("authentication"), Keys: self.exporter.Snapshot("keys"), Extender: self.exporter.Snapshot("extender"), Runtime: self.exporter.Snapshot("runtime")}
}

// Preserve the existing wire vocabulary without invoking foreign methods.
// Opaque joins/custom wrappers remain unknown; original custody keeps the error.
func providerDiagnosticCause(err error) string {
	switch diagnostics.ClassifyCause(err) {
	case diagnostics.CauseNone:
		return "none"
	case diagnostics.CauseCanceled:
		return "canceled"
	case diagnostics.CauseTimeout:
		return "timeout"
	case diagnostics.CausePermission:
		return "permission"
	case diagnostics.CauseUnavailable:
		return "unavailable"
	}
	return "unknown" // No new miner wire cause is introduced for transport.
}

// Serialization has fixed field/count bounds before queue admission. An accepted
// offer means queued; only exporter snapshots report completed sink writes.
func (self *providerDiagnostics) observe(event providerDiagnosticEvent, provider uint64, known bool, err error, retry time.Duration, extender *providerExtenderObservation) {
	if self == nil {
		return
	}
	domain, code := "runtime", "unknown"
	switch event {
	case providerAuthenticationWait:
		domain, code = "authentication", "retry_wait"
	case providerJwtSaveFailed:
		domain, code = "authentication", "jwt_save_failed"
	case providerRejectionSaveFailed:
		domain, code = "authentication", "rejection_save_failed"
	case providerAuthenticationRejected:
		domain, code = "authentication", "rejected"
	case providerClientKeySaveFailed:
		domain, code = "keys", "client_key_save_failed"
	case providerTlsKeySaveFailed:
		domain, code = "keys", "tls_key_save_failed"
	case providerExtenderKeySaveFailed:
		domain, code = "keys", "extender_key_save_failed"
	case providerExtenderIdentityInvalid:
		domain, code = "keys", "extender_identity_invalid"
	case providerIdentityReady:
		domain, code = "keys", "identity_available"
	case providerExtenderObserved:
		domain, code = "extender", "observed"
	case providerStarted:
		code = "provider_started"
	case providerWorkerFailed:
		code = "provider_worker_failed"
	case providerStatusStarted:
		code = "status_started"
	case providerStatusFailed:
		code = "status_failed"
	case providerStatusNotice:
		code = "http_notice"
	case providerStartupRecoveryRequired:
		domain, code = "authentication", "startup_recovery_required"
	case providerWorkCaptureRequired:
		code = "whole_work_capture_required"
	case providerContractCaptureRequired:
		code = "original_contract_capture_required"
	}
	if event != providerExtenderObserved {
		extender = nil
	}
	if retry < 0 {
		retry = 0
	}
	guidance := ""
	if event == providerStartupRecoveryRequired {
		guidance = "Check proxy slots and restore original provider custody. Known new work: --allow-client-registration. Verified original legacy key/JWT: --adopt-legacy-provider-key. See miner/PROVIDER-REGISTRATION.md."
	}
	if event == providerWorkCaptureRequired {
		guidance = "Restore the reviewed whole-work profile, original provider identity and private outbox. Supply --whole-work-capture and --whole-work-capture-sha256. See miner/PROVIDER-WHOLE-WORK.md."
	}
	if event == providerContractCaptureRequired {
		guidance = "Restore the approved original contract profile, provider key and prepared source custody. Supply --original-contract-capture and --original-contract-capture-sha256. See miner/PROVIDER-WHOLE-WORK.md."
	}
	record := struct {
		Schema        string                       `json:"schema"`
		Domain        string                       `json:"domain"`
		Event         string                       `json:"event"`
		Provider      uint64                       `json:"provider"`
		ProviderKnown bool                         `json:"provider_known"`
		Cause         string                       `json:"cause"`
		RetryMs       uint64                       `json:"retry_ms"`
		Extender      *providerExtenderObservation `json:"extender,omitempty"`
		Guidance      string                       `json:"guidance,omitempty"`
	}{Schema: providerDiagnosticSchema, Domain: domain, Event: code, Provider: provider, ProviderKnown: known, Cause: providerDiagnosticCause(err), RetryMs: uint64(retry / time.Millisecond), Extender: extender, Guidance: guidance}
	raw, encodeErr := json.Marshal(record)
	if encodeErr != nil {
		return // All fields above are scalar; no arbitrary marshaler is invoked.
	}
	self.exporter.Offer(domain, append(raw, '\n'))
}
