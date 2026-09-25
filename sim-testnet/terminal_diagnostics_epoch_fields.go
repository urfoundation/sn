// Additive epoch projections can be absent from an older signed producer.
// Attribute that gap without rewriting its bytes or borrowing today's queues.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// This inventories field availability, not payout correctness. Actual missing,
// pending or bad accepted epochs still reach the unchanged strict assertions.
func collectTerminalDiagnosticEpochObservationFields(collector *terminalDiagnosticCollector, terminal *ScenarioObservation, prerequisite string) {
	if prerequisite == "" {
		var missing []string
		if terminal == nil || len(terminal.Claims) == 0 || len(terminal.Operators) == 0 {
			missing = append(missing, "claim or operator observation census is absent")
		} else {
			for _, claim := range terminal.Claims {
				if len(claim.EpochOutcomes) == 0 {
					missing = append(missing, fmt.Sprintf("miner %d omits epoch_outcomes; its default last_discovered value is not observed discovery evidence", claim.MinerID))
				}
			}
			for _, operator := range terminal.Operators {
				if len(operator.PayoutTierArtifacts) == 0 {
					missing = append(missing, fmt.Sprintf("operator %d omits payout_tier_artifacts; latest epoch %d alone does not prove the signed window", operator.NoID, operator.LatestArtifactEpoch))
				}
			}
		}
		if len(missing) != 0 {
			prerequisite = "signed producer epoch fields unavailable: " + strings.Join(missing, "; ") + "; current local files cannot replace historical observations"
		}
	}
	collector.check("terminal-epoch-observation-fields", prerequisite, time.Minute, func(context.Context) (any, error) {
		return map[string]any{"observation_hash": terminal.ObservationHash, "claim_projections": len(terminal.Claims), "operator_projections": len(terminal.Operators)}, nil
	})
}
