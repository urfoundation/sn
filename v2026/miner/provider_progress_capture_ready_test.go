// A capture launch keeps requiring the processed registration of its key.
package miner

import "testing"

// A capture launch is a standalone launch whose devices require the processed
// registration (ProviderWorkCaptureProfile.apply, ProviderContractCaptureProfile
// .apply); the run attaches each member with its device's own requirement, so
// a published key alone leaves the member not ready there too.
func TestProviderProgressCaptureLaunchStillRequiresRegistration(t *testing.T) {
	owner := newProviderProgressTestOwner(t)
	device := newProviderProgressPublishedDevice()
	if _, err := owner.attachWithKeyRegistration(t.Context(), "direct", device, true); err != nil {
		t.Fatal(err)
	}
	value, err := owner.snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if member := value.Members[0]; member.Ready || member.KeyRegistered {
		t.Fatal("a capture member with a published but unregistered key reads ready", member)
	}
	device.registered = true
	device.ready = true
	value, err = owner.snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if member := value.Members[0]; !member.Ready || !member.KeyRegistered {
		t.Fatal("a registered capture member reads not ready", member)
	}
}
