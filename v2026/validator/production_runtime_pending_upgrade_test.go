//go:build linux || darwin

// Actual durable pending custody crosses a compatible upgrade. Original bytes
// are retained; no receipt, nonce or replacement verdict is supplied.
package validator

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/urfoundation/sn/v2026/crv4"
)

// Bytes signed under the approved runtime cannot be included once the scanned
// boundary runs a later spec. With no receipt and an unspent nonce, the opted-in
// pending owner records that terminal outcome instead of retrying a hard error
// until the steering failure budget stops the validator. It never rebroadcasts.
// Without the opt-in the upgraded boundary is not admitted at all.
func TestProductionContinuationEndsPendingBytesReplacedByCompatibleUpgrade(t *testing.T) {
	for _, optIn := range []bool{true, false} {
		t.Run(fmt.Sprintf("opt-in=%t", optIn), func(t *testing.T) {
			fixture := newProductionContinuationTestFixtureWithStartup(t, nil, func(production *ownerRecycleProductionTestFixture, _ *productionContinuationTestFixture) {
				if optIn {
					production.cfg.RuntimeSuccessorProfile = crv4.ValidatorProducerRuntimeProfile
				}
			})
			if err := enableReleaseRuntimeSuccession(fixture.steerer.native, fixture.steerer.cfg); err != nil {
				t.Fatal(err)
			}
			production := fixture.production
			installProductionUpgradeTest(t, production, fixture.steerer.cfg.RuntimeSpec+1, releaseHex32([32]byte{0x47, 0x03}), productionUpgradeTestMetadata(t, production.operator.measurement.admission.metadata, nil))
			native := installProductionContinuationNative(t, fixture)
			pending := fixture.beginAndLoseAcknowledgement(t, native)
			production.head = 102
			err := fixture.steerer.submitOnceV2(t.Context())
			retained := fixture.restart(t)
			if len(native.broadcasts) != 1 || retained.Prepared.ExtrinsicHex != pending.Prepared.ExtrinsicHex {
				t.Fatal("pending reconciliation replaced or rebroadcast the original signed bytes")
			}
			if !optIn {
				if err == nil || !strings.Contains(err.Error(), "unreviewed identity") || retained.Status != "pending" {
					t.Fatalf("an unapproved upgrade changed pending liability: status=%s err=%v", retained.Status, err)
				}
				return
			}
			var transition *productionSteeringTransition
			if !errors.As(err, &transition) || retained.Status != "failed" || !strings.Contains(retained.Error, "rejects their signed spec version") {
				t.Fatalf("replaced signing runtime did not end the pending liability: status=%s cause=%q err=%v", retained.Status, retained.Error, err)
			}
		})
	}
}
