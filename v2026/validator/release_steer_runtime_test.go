package validator

import (
	"testing"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/stabi"
)

func TestReleaseRuntimeHeadClaimDoesNotCrossBindingGeneration(t *testing.T) {
	clientID := connect.NewId()
	fleetID, hotkey := [32]byte{1}, [32]byte{2}
	claim := AttemptEgressClaim{
		Binding: AttemptBinding{
			ClientID: clientID, Active: true, UIDFound: true, UID: 7,
			FleetID: attemptHex32(fleetID), Hotkey: attemptHex32(hotkey), Generation: 1,
		},
		EgressIPHash: attemptHex32([32]byte{3}),
	}
	binding := stabi.STCoordinatorBindingRecord{FleetId: fleetID, Hotkey: hotkey, Generation: 1, Uid: 7}
	if !releaseAttemptClaimMatchesBinding(claim, binding, 7) {
		t.Fatal("same-generation attempt claim did not reach runtime head scoring")
	}
	binding.Generation = 2
	if releaseAttemptClaimMatchesBinding(claim, binding, 7) {
		t.Fatal("prior-generation attempt claim transferred to a rebound provider")
	}
}
