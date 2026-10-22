// Diagnostic labels cannot promote an operation-owned cancellation to financial evidence conflict.
package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// The reader's exact diagnostic prefix is retained while classification uses actual causes.
func TestEconomicClosedWorkHttpCancellationDiagnosticDoesNotHoldEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	original := fmt.Errorf("artifact integrity: %w", ctx.Err())
	got := economicEntitlementEvidenceError(original)
	if !errors.Is(got, context.Canceled) || errors.Is(got, errRpcIntegrity) || !monitorOnlyCancellationCauses(got, 0) {
		t.Fatal("HTTP cancellation diagnostic manufactured an integrity hold", got)
	}
}

// A simultaneous real contradiction remains held even when the operation is also canceled.
func TestEconomicClosedWorkHttpCancellationDoesNotEraseRealConflict(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got := economicEntitlementEvidenceError(errors.Join(fmt.Errorf("artifact integrity: %w", ctx.Err()), errRpcIntegrity))
	if !errors.Is(got, context.Canceled) || !errors.Is(got, errRpcIntegrity) || monitorOnlyCancellationCauses(got, 0) {
		t.Fatal("HTTP cancellation erased an actual original contradiction", got)
	}
}
