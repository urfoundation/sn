// When the chain moves to a runtime this build does not admit, every steering
// attempt refuses the same way until a release that admits it is installed: a
// retry or a restart cannot change the answer. Steering holds instead of
// spending its failure budget and ending the process: it signs nothing, reads
// as a failure on every health signal and says why once a minute, while
// trails, settlement and the audit keep running. A new binary resumes it, and
// the missed native epochs are recorded as an ordinary gap edge.
package validator

import (
	"context"
	"fmt"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Poll cycles between looks while held: one a minute at the three-second
// production poll, as for the native gap hold.
const productionRuntimeHoldPolls = 20

// The first refusal when every cause in the graph is the chain's runtime being
// unreviewed or a refused successor of the approved one; nil when any other
// cause is present, which keeps its ordinary failure budget.
func productionSteeringRuntimeHoldOf(err error) error {
	if err == nil {
		return nil
	}
	var first error
	remaining := 512
	complete := releaseErrorGraph(err, func(cause error) bool {
		switch cause.(type) {
		case *crv4.UnreviewedRuntimeError, *crv4.RuntimeSuccessorRefusedError:
			if first == nil {
				first = cause
			}
			return true
		}
		return false
	}, 0, &remaining)
	if !complete {
		return nil
	}
	return first
}

// The hold reads as the hard error it is, so steering alerts stay on, and
// offers the operator one line naming the runtime and what resumes steering.
func observeProductionRuntimeHold(ctx context.Context, progress *releaseProgress, refusal error) {
	progress.observeSteering(0, false, "hard_error", false)
	releaseDiagnostic(ctx, "steering", "hard_error", 0, false, 1, releaseDiagnosticFacts{cause: releaseDiagnosticHardError})
	if self := releaseDiagnosticOwner(ctx); self != nil {
		self.exporter.Offer("steering", []byte(fmt.Sprintf("validator steering hold: %v; this build does not admit the chain's runtime, so no weights are signed; trails, settlement and the audit keep running; install a validator release that admits the runtime and restart\n", refusal)))
	}
}
