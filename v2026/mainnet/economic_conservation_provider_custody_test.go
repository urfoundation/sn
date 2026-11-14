// Even missing provider evidence cannot bypass the retained containing owner.
package main

import (
	"context"
	"errors"
	"os"
	"testing"
)

// Owner refusal precedes raw evidence access, so missing remote components do
// not hide a closed original view or a canceled private admission transaction.
func TestEconomicProviderOriginalCustodyPrecedesMissingSource(t *testing.T) {
	policy := economicConservationPolicy{EntitlementSources: &economicConservationEntitlementPolicy{Sources: []economicConservationEntitlementSource{{PoolId: "1", ProviderMeasurements: &economicProviderMeasurementPolicy{Schema: economicProviderMeasurementPolicySchema}}}}}
	record := economicConservationEntitlement{PoolId: "1", Census: &economicConservationEntitlementCensus{}}
	state := &economicConservationState{}
	closed := &economicConservationArchiveView{closed: true}
	if value, err := closed.verifyProviderOriginals(t.Context(), policy, state, record); value != nil || !errors.Is(err, os.ErrClosed) {
		t.Fatal("missing provider source hid closed original custody", value, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	pending := &economicConservationArchiveView{admission: ctx}
	if value, err := pending.verifyProviderOriginals(ctx, policy, state, record); value != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("missing provider source hid canceled original admission", value, err)
	}
}
