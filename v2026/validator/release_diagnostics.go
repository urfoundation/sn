// Validator diagnostics have one explicit lifecycle owner. Closed scalar
// events never format raw errors, custody records, signatures or credentials.
package validator

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/diagnostics"
	"github.com/urfoundation/sn/v2026/protocol"
)

// A private context key routes only this RunRelease instance's exporter.
type releaseDiagnosticContextKey struct{}

// Tests may observe the actual owner after close or supply an explicit sink.
// Neither callback replaces configuration, admission or lifecycle decisions.
type releaseDiagnosticHooks struct {
	writer      io.Writer
	afterCreate func(context.Context, *releaseDiagnostics)
	afterClose  func(*releaseDiagnostics, error)
}

// This key is distinct from the running exporter and carries no protocol state.
type releaseDiagnosticHooksKey struct{}

// The protocol/status worker and core workers borrow this owner; only the
// enclosing RunRelease lifecycle closes and joins its actual destination.
type releaseDiagnostics struct{ exporter *diagnostics.Exporter }

// Even when no progress path is configured, production diagnostics have an
// owned bounded path to the existing journal/socket/pipe log collection.
func newReleaseDiagnostics(ctx context.Context, writer io.Writer, now func() time.Time) (context.Context, *releaseDiagnostics, error) {
	if hooks, ok := ctx.Value(releaseDiagnosticHooksKey{}).(releaseDiagnosticHooks); ok && hooks.writer != nil {
		writer = hooks.writer
	}
	owner, err := diagnostics.NewWithClock(ctx, writer, []string{"startup", "steering", "progress", "operator", "runtime"}, now)
	if err != nil {
		return ctx, nil, err
	}
	self := &releaseDiagnostics{exporter: owner}
	return context.WithValue(ctx, releaseDiagnosticContextKey{}, self), self, nil
}

// Cause and phase are observation-only classifications from existing typed
// retry branches. None of these values authorizes another operation.
type releaseDiagnosticCause uint8

const (
	releaseDiagnosticUnknown releaseDiagnosticCause = iota
	releaseDiagnosticTimeout
	releaseDiagnosticTransport
	releaseDiagnosticUnavailable
	releaseDiagnosticHardError
)

// Additional facts have fixed cardinality or numeric bounds, never raw text.
type releaseDiagnosticFacts struct {
	phase         productionSteeringReadPhase
	cause         releaseDiagnosticCause
	operatorId    uint64
	operatorKnown bool
	cacheStage    uint8 // 0 unknown, 1 read, 2 write
}

// Retry authority has already been decided by the operation's owner. Optional
// output reads only concrete tags/fields; opaque joins and foreign wrappers
// stay unknown without calling retry predicates, Is, As or Unwrap again.
func releaseDiagnosticReadCause(err error) releaseDiagnosticCause {
	if err == nil {
		return releaseDiagnosticUnknown
	}
	value := reflect.ValueOf(err)
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return releaseDiagnosticUnknown
	}
	if _, unavailable := err.(*crv4.ReceiptEvidenceUnavailableError); unavailable {
		return releaseDiagnosticUnavailable
	}
	// This reviewed helper performs a concrete type switch only. It does not
	// call transport methods or decide whether the original error is retryable.
	if crv4.IsSubstrateReadTransportCause(err) {
		return releaseDiagnosticTransport
	}
	switch diagnostics.ClassifyCause(err) {
	case diagnostics.CauseTimeout:
		return releaseDiagnosticTimeout
	case diagnostics.CauseTransport:
		return releaseDiagnosticTransport
	}
	return releaseDiagnosticUnknown
}

// No output error can change validation or reconciliation authority. The
// exporter retains unavailable counters; shutdown joins all descriptor effects.
func (self *releaseDiagnostics) close() error {
	if self == nil {
		return nil
	}
	return self.exporter.Close()
}

// A missing optional owner is unknown, never permission to use blocking stdout.
func releaseDiagnosticOwner(ctx context.Context) *releaseDiagnostics {
	if ctx == nil {
		return nil
	}
	self, _ := ctx.Value(releaseDiagnosticContextKey{}).(*releaseDiagnostics)
	return self
}

// Fixed messages keep Error/String implementations off the core logging path.
// Offered records are bounded before formatting and never wait for their sink.
func releaseDiagnostic(ctx context.Context, domain, code string, epoch uint64, known bool, attempt uint64, details ...releaseDiagnosticFacts) {
	self := releaseDiagnosticOwner(ctx)
	if self == nil {
		return
	}
	switch code {
	case "publisher_disabled_configuration", "close_error", "publisher_disabled_ownership", "publication_unavailable_retrying", "publication_recovered",
		"startup_evm_identity_unavailable", "startup_native_identity_unavailable", "startup_activation_history_unavailable", "startup_server_key_history_unavailable", "startup_disk_intent_history_unavailable", "startup_native_owner_unavailable", "startup_unavailable",
		"preparation_unavailable", "read_wait", "receipt_transport_wait", "preparation_pending", "receipt_pending", "hard_error", "runtime_active", "jwt_save_failed", "receipt_cache_disabled",
		"authentication_pending", "authentication_unavailable", "authentication_recovery_required", "authentication_ready", "authentication_revoked", "jwt_rejection_save_failed", "network_sign_in_rejected":
	default:
		code = "diagnostic_unknown"
	}
	switch domain {
	case "startup", "steering", "progress", "operator", "runtime":
	default:
		domain = "runtime"
	}
	if !known {
		epoch = 0
	}
	facts := releaseDiagnosticFacts{}
	if len(details) == 1 {
		facts = details[0]
	}
	phase, cause, cache := "unknown", "unknown", "unknown"
	switch facts.phase {
	case productionReadIntent:
		phase = "intent"
	case productionReadReceipt:
		phase = "receipt"
	case productionReadPreparation:
		phase = "preparation"
	case productionReadApplication:
		phase = "application"
	}
	switch facts.cause {
	case releaseDiagnosticTimeout:
		cause = "timeout"
	case releaseDiagnosticTransport:
		cause = "transport"
	case releaseDiagnosticUnavailable:
		cause = "unavailable"
	case releaseDiagnosticHardError:
		cause = "hard_error"
	}
	switch facts.cacheStage {
	case 1:
		cache = "read"
	case 2:
		cache = "write"
	}
	if !facts.operatorKnown {
		facts.operatorId = 0
	}
	raw := []byte(fmt.Sprintf("validator diagnostic: domain=%s code=%s native_epoch=%d epoch_known=%t attempt=%d phase=%s cause=%s operator_id=%d operator_known=%t cache_stage=%s\n", domain, code, epoch, known, attempt, phase, cause, facts.operatorId, facts.operatorKnown, cache))
	self.exporter.Offer(domain, raw)
}

// Each status snapshot copies actual exporter acknowledgments; it cannot claim
// that its own file publication or queued diagnostic has completed.
func (self *releaseDiagnostics) snapshot(now time.Time) *protocol.ValidatorDiagnosticObservation {
	if self == nil {
		return nil
	}
	state := func(name string) protocol.ValidatorDiagnosticState {
		value := self.exporter.Snapshot(name)
		return protocol.ValidatorDiagnosticState{Outcome: value.Outcome, Delivered: value.Delivered, Dropped: value.Dropped, DroppedBytes: value.DroppedBytes, Unavailable: value.Unavailable, LastSuccessAt: value.LastSuccessAt}
	}
	return &protocol.ValidatorDiagnosticObservation{ObservedAt: now.UTC().Format(time.RFC3339Nano), Startup: state("startup"), Steering: state("steering"), Progress: state("progress"), Operator: state("operator"), Runtime: state("runtime")}
}
