// What makes a provider member ready. A standalone launch requires no
// processed registration of its key (ProviderDeviceSettings), so its devices
// never read registered; it is ready once connected with its key published
// (owner, 2026-11-17). Swarm and capture launches require the processed
// registration and keep requiring it.
package miner

import (
	"testing"

	"github.com/urnetwork/sdk/v2026"
)

// The device a launch that requires no registration gets: connected, its key
// acknowledged by the platform, and never registered, so the sdk's strict
// readiness reads false too.
func newProviderProgressPublishedDevice() *providerProgressTestDevice {
	device := newProviderProgressTestDevice()
	device.registered = false
	device.ready = false
	return device
}

// Root cause: the snapshot read ready as the sdk's strict readiness and the
// processed registration, which a standalone launch never has, so
// miner-status.json and `provider status` read every standalone member not
// ready however long it served.
func TestProviderProgressStandaloneLaunchIsReadyOnceConnectedWithItsKeyPublished(t *testing.T) {
	owner := newProviderProgressTestOwner(t)
	device := newProviderProgressPublishedDevice()
	if _, err := owner.attach(t.Context(), "direct", device); err != nil {
		t.Fatal(err)
	}
	value, err := owner.snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	member := value.Members[0]
	if !member.Current || !member.Connected {
		t.Fatal("the connected standalone member is not current and connected", member)
	}
	if !member.Ready {
		t.Fatal("a standalone member connected with its key published reads not ready", member)
	}
	if !member.KeyRegistered {
		t.Fatal("a standalone member's published key does not read held by the platform", member)
	}
}

// A standalone member is ready only with both facts: a published key with no
// carrier is not ready, and neither is a carrier whose key the platform has
// not acknowledged yet.
func TestProviderProgressStandaloneLaunchWaitsForCarrierAndPublication(t *testing.T) {
	for _, c := range []struct {
		connected bool
		published bool
	}{
		{connected: false, published: true},
		{connected: true, published: false},
		{connected: false, published: false},
	} {
		owner := newProviderProgressTestOwner(t)
		device := newProviderProgressPublishedDevice()
		device.connected = c.connected
		device.published = c.published
		if _, err := owner.attach(t.Context(), "direct", device); err != nil {
			t.Fatal(err)
		}
		value, err := owner.snapshot(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		member := value.Members[0]
		if member.Ready {
			t.Errorf("connected %t, published %t reads ready: %+v", c.connected, c.published, member)
		}
		if member.KeyRegistered != c.published {
			t.Errorf("published %t reads the key held %t", c.published, member.KeyRegistered)
		}
	}
}

// A swarm member requires its key's processed registration: a published key
// alone leaves it not ready, and the processed registration makes it ready.
func TestProviderProgressSwarmMemberStillRequiresRegistration(t *testing.T) {
	owner, err := newProviderProgressOwner("swarm", []providerProgressConfigMember{{Slot: "member-a", ApiUrl: "https://synthetic.invalid", ConnectUrl: "wss://synthetic.invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	device := newProviderProgressPublishedDevice()
	if _, err := owner.attach(t.Context(), "member-a", device); err != nil {
		t.Fatal(err)
	}
	value, err := owner.snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if member := value.Members[0]; member.Ready || member.KeyRegistered {
		t.Fatal("a swarm member with a published but unregistered key reads ready", member)
	}
	device.registered = true
	device.ready = true
	value, err = owner.snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if member := value.Members[0]; !member.Ready || !member.KeyRegistered {
		t.Fatal("a registered swarm member reads not ready", member)
	}
}

// The real device answers every fact the snapshot reads.
var _ providerProgressDevice = (*sdk.DeviceLocal)(nil)
