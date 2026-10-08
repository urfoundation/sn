// The actual v4 preparation selects observer approval without a native key.
// Both UR signing roles remain distinct and participant outcomes remain open.
package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Existing-seat observation cannot accidentally inherit legacy action custody
// or relax the separate standard UR validators' signing prerequisites.
func TestRootCurrentPassiveLaunchSeparatesNativeCustody(t *testing.T) {
	fixture := newBootstrapRootPassiveFixture(t)
	preparation := fixture.preparation
	role := *preparation.Plan.Config.RootValidator
	if err := role.validate(); err != nil || role.ActionApprovalPublicKey != "" || role.Implementation != "sn/mainnet/root-passive-service" || role.Strategy != rootPassiveStrategy {
		t.Fatalf("current root role requires legacy action authority: %+v, %v", role, err)
	}
	if preparation.Root.PassiveService == nil || !reflect.DeepEqual(preparation.Root.Service, rootServiceConfig{}) || len(preparation.Root.custodyChildPaths()) != 0 {
		t.Fatal("v4 observer acquired native custody children or a legacy action")
	}
	readiness := newBootstrapChainReadiness(preparation)
	policy := preparation.Root.PassiveService.Policy
	if readiness.RootValidator.ApprovedFromBlock != policy.ValidFromBlock || readiness.RootValidator.ApprovedThroughBlock != policy.ValidThroughBlock || readiness.RootValidator.ApprovedCheckpointHash != "" {
		t.Fatal("passive observation acquired a native action mortality checkpoint")
	}
	for _, blocker := range readiness.RootValidator.ActivationBlockers {
		if strings.Contains(blocker, "WEIGHT") || strings.Contains(blocker, "NONCE") || strings.Contains(blocker, "SIGNING_DEVICE") || strings.Contains(blocker, "GLOBAL_CUSTODY") {
			t.Fatalf("v4 observer acquired a legacy signer gate: %s", blocker)
		}
	}
	if len(readiness.UrValidators) != 2 || readiness.RootValidator.Observed != nil || readiness.ObservationComplete || readiness.ActivationReady || readiness.NativeSigning || readiness.NetworkEffects {
		t.Fatal("observer configuration fabricated participation or replaced a UR validator")
	}
	for _, urRole := range readiness.UrValidators {
		if !slices.Contains(urRole.ActivationBlockers, "SIGNING_DEVICE_AND_GLOBAL_CUSTODY_FENCE_UNVERIFIED") || urRole.Expected.Hotkey == role.Hotkey {
			t.Fatal("passive root exception weakened separate UR signing admission")
		}
	}
	role.ActionApprovalPublicKey = role.ApprovalPublicKey
	if err := role.validate(); err == nil {
		t.Fatal("passive role accepted a native-action approval key")
	}
}
