// Root daemons publish bounded operational projections. Full observations and
// signed action bytes remain with their preview and durable custody owners.
package main

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/urfoundation/sn/v2026/diagnostics"
)

// Run consumes this exporter and joins it on every exit. Snapshot remains
// readable after close; an absent external metrics consumer cannot attest delivery.
type rootServiceOutput struct {
	exporter *diagnostics.Exporter
}

// Only the shared exporter's explicit bounded sink contracts are admitted.
func newRootServiceOutput(ctx context.Context, writer io.Writer) (*rootServiceOutput, error) {
	exporter, err := diagnostics.New(ctx, writer, []string{"root"})
	if err != nil {
		return nil, err
	}
	return &rootServiceOutput{exporter: exporter}, nil
}

// Scalars cannot retain the step's mutable vectors, signature or custody packet.
type rootServiceDiagnostic struct {
	Schema          string               `json:"schema"`
	Phase           string               `json:"phase"`
	Status          string               `json:"status"`
	Observations    uint32               `json:"observations"`
	ActionPhase     string               `json:"action_phase,omitempty"`
	Failed          bool                 `json:"failed"`
	ActivationReady bool                 `json:"activation_ready"`
	Diagnostics     diagnostics.Snapshot `json:"diagnostics"`
}

// A closed vocabulary refuses arbitrary strings without formatting errors.
func rootDiagnosticPhase(value string) string {
	switch value {
	case "observing", "active", "complete", "reserved", "signing", "signed", "pending", "finalized", "dispatch-failed", "fee-overrun", "expired", "runtime-deviation":
		return value
	default:
		return "unknown"
	}
}

// Decision states describe this step, never independently observed acceptance.
func rootDiagnosticStatus(value string) string {
	switch value {
	case "blocked", "complete", "pending", "observation-unavailable", "intent", "target-observed", "setter-disabled", "rate-limited", "destination-count", "destination-unavailable", "concentration-cap":
		return value
	default:
		return "unknown"
	}
}

// Publication is nonblocking and cannot create, replace or replay an action.
func (self *rootServiceOutput) offer(event rootServiceEvent, failed bool) {
	value := rootServiceDiagnostic{Schema: "urnetwork-mainnet-root-service-diagnostic-v1", Phase: rootDiagnosticPhase(event.Phase),
		Status: rootDiagnosticStatus(event.Status), Observations: event.Observations, Failed: failed, Diagnostics: self.snapshot()}
	if event.Action != nil {
		value.ActionPhase = rootDiagnosticPhase(event.Action.Phase)
	}
	raw, _ := json.Marshal(value) // Fixed scalar fields have no custom marshaler.
	self.exporter.Offer("root", append(raw, '\n'))
}

// Delivery acknowledges a previous write only, not the current step.
func (self *rootServiceOutput) snapshot() diagnostics.Snapshot {
	return self.exporter.Snapshot("root")
}

// Descriptor errors stay joined to the caller's original result.
func (self *rootServiceOutput) close() error { return self.exporter.Close() }

// The default cadence remains bounded and cancellation joins the wait itself.
func waitRootService(ctx context.Context, interval time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(interval):
		return true
	}
}
