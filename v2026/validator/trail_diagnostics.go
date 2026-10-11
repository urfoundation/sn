// Trail diagnostics borrow the release instance's existing bounded exporter.
// They report local work only, never native progress or independent acceptance.
package validator

import (
	"context"
	"fmt"

	"github.com/urfoundation/sn/v2026/diagnostics"
	"github.com/urnetwork/connect/v2026"
)

// An operator binding is numeric and scoped to this worker's context.
type trailDiagnosticOperatorKey struct{}

// The zero value means no independently supplied operator identity is known.
type trailDiagnosticOperator struct {
	id    uint64
	known bool
}

// The actual release worker binds its admitted operator before starting work.
func withTrailDiagnosticOperator(ctx context.Context, operatorId uint64) context.Context {
	return context.WithValue(ctx, trailDiagnosticOperatorKey{}, trailDiagnosticOperator{id: operatorId, known: true})
}

// A precreated authentication child keeps its own cancellation and deadline.
// Only diagnostic routing and scalar identity are copied, never authority.
func copyTrailDiagnosticContext(child context.Context, source context.Context) context.Context {
	operator, _ := source.Value(trailDiagnosticOperatorKey{}).(trailDiagnosticOperator)
	child = context.WithValue(child, trailDiagnosticOperatorKey{}, operator)
	return context.WithValue(child, releaseDiagnosticContextKey{}, releaseDiagnosticOwner(source))
}

// These closed events cannot inject peer data, signatures or raw error text.
type trailDiagnosticCode uint8

const (
	trailDiagnosticFailed trailDiagnosticCode = iota
	trailDiagnosticComplete
	trailDiagnosticFatal
	trailDiagnosticSignatureVariant
)

// The real trail owner supplies its concrete classification. Diagnostic code
// never searches arbitrary wrappers or calls their optional methods.
func trailDiagnosticClassification(err error) (string, string) {
	kind, cause := "unknown", "unknown"
	if trailErr, ok := err.(*TrailError); ok && trailErr != nil {
		switch trailErr.Kind {
		case TrailErrorSeed:
			kind = "seed"
		case TrailErrorHop:
			kind = "hop"
		case TrailErrorProtocol:
			kind = "protocol"
		case TrailErrorUnknownOutcome:
			kind = "unknown_final"
		}
		err = trailErr.Err
	}
	switch diagnostics.ClassifyCause(err) {
	case diagnostics.CauseCanceled:
		cause = "canceled"
	case diagnostics.CauseTimeout:
		cause = "timeout"
	case diagnostics.CauseTransport:
		cause = "transport"
	}
	return kind, cause
}

// Offering a fixed scalar record never waits for output. All trail workers in
// one release instance share the existing finite runtime domain; congestion is
// represented by that domain's prior delivery/drop state in the unchanged wire.
func observeTrailDiagnostic(ctx context.Context, event trailDiagnosticCode, err error, record *ProofRecord, epochKnown bool) {
	owner := releaseDiagnosticOwner(ctx)
	if owner == nil {
		return
	}
	operator, _ := ctx.Value(trailDiagnosticOperatorKey{}).(trailDiagnosticOperator)
	if !operator.known {
		operator.id = 0
	}
	code, kind, cause := "diagnostic_unknown", "unknown", "unknown"
	depth, epoch := 0, uint64(0)
	switch event {
	case trailDiagnosticFailed:
		code = "trail_failed"
		kind, cause = trailDiagnosticClassification(err)
	case trailDiagnosticComplete:
		code = "trail_complete"
		if record != nil && connect.VerifyMMin <= record.M && record.M <= connect.VerifyMMax {
			depth = record.M
			if epochKnown {
				epoch = record.Epoch
			}
		} else {
			epochKnown = false
		}
	case trailDiagnosticFatal:
		code, cause = "trail_custody_failed", "hard_error"
	case trailDiagnosticSignatureVariant:
		code = "trail_verifier_signature_variant"
	}
	if event != trailDiagnosticComplete {
		epochKnown = false
	}
	raw := []byte(fmt.Sprintf("validator diagnostic: domain=runtime code=%s operator_id=%d operator_known=%t trail_kind=%s cause=%s depth=%d settlement_epoch=%d settlement_epoch_known=%t\n", code, operator.id, operator.known, kind, cause, depth, epoch, epochKnown))
	owner.exporter.Offer("runtime", raw)
}
