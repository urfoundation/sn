//go:build linux || darwin

// Ordinary signed payout evidence and lifecycle qualification are independent
// diagnostic outputs; neither can manufacture a strict acceptance result.
package main

import (
	"context"
	"time"
)

// The existing strict collector still authenticates each requested signed
// artifact. The test seam supplies transport without replacing that verifier.
type terminalDiagnosticPayoutReader func(context.Context, *ResolvedConfig, string, *ScenarioObservation, *ScenarioAcceptanceWindow, *finalPublicIdentities) ([]FinalCollectedPayoutArtifact, []FinalCollectedPayoutArtifact, error)

// An unavailable lifecycle index must not suppress ordinary acceptance-window
// evidence. Only this non-accepting diagnostic projects the two scopes apart.
func collectTerminalDiagnosticPayouts(collector *terminalDiagnosticCollector, cfg *ResolvedConfig, terminal *ScenarioObservation, identities *finalPublicIdentities, prerequisite string, read terminalDiagnosticPayoutReader) {
	if !collector.report.ReadOnly || collector.report.FinalAcceptance || read == nil {
		prerequisite = "read-only non-accepting payout diagnostic authority unavailable"
	}
	if prerequisite == "" && (terminal == nil || collector.report.Window == nil || identities == nil) {
		prerequisite = "terminal observation, signed window or payout authority unavailable"
	}
	collector.check("signed-payout-artifacts", prerequisite, 10*time.Minute, func(ctx context.Context) (any, error) {
		ordinary := *terminal
		ordinary.FleetLifecycle = nil
		payouts, _, err := read(ctx, cfg, collector.output, &ordinary, collector.report.Window, identities)
		return map[string]any{"acceptance": payouts, "scope": "signed-acceptance-window", "final_acceptance": false}, err
	})
	lifecyclePrerequisite := prerequisite
	if lifecyclePrerequisite == "" && (terminal == nil || terminal.FleetLifecycle == nil || len(terminal.FleetLifecycle.Payouts) == 0) {
		lifecyclePrerequisite = "terminal fleet lifecycle payout artifact index unavailable; no lifecycle qualification is claimed"
	}
	collector.check("lifecycle-payout-artifacts", lifecyclePrerequisite, 10*time.Minute, func(ctx context.Context) (any, error) {
		_, payouts, err := read(ctx, cfg, collector.output, terminal, collector.report.Window, identities)
		return map[string]any{"lifecycle": payouts, "scope": "original-lifecycle-index", "final_acceptance": false}, err
	})
}
