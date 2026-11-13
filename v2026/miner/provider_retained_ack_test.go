package miner

import (
	"testing"

	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

// A provider's socket-owned TCP returns are retained past the ordinary
// acknowledgement lifetime, and connect bounds that retention with
// SendBufferSettings.RetainedAckTimeout. Its zero value keeps the unbounded
// retention that rewrote a gone destination's whole window every
// MaxResendInterval until the process restarted, and a swarm host multiplies
// that by every provider it runs. Every provider this command launches, the
// standalone one and each swarm member, must keep the bound.
func TestProviderDeviceSettingsKeepRetainedAckBound(t *testing.T) {
	bound := connect.DefaultSendBufferSettings().RetainedAckTimeout
	if bound <= 0 {
		t.Fatal("connect sets no default retained acknowledgement bound, so this proves nothing")
	}
	for _, c := range []struct {
		name     string
		settings *sdk.DeviceLocalSettings
	}{
		{name: "standalone provider", settings: ProviderDeviceSettings([32]byte{})},
		{name: "standalone provider with a close report domain", settings: ProviderDeviceSettings([32]byte{71})},
		{name: "swarm member", settings: swarmMemberDeviceSettings(ProviderSwarmMember{ID: "miner-1"}, nil, nil)},
	} {
		if retained := c.settings.ClientSettings.SendBufferSettings.RetainedAckTimeout; retained != bound {
			t.Errorf("%s retained acknowledgement bound = %s, want connect's %s", c.name, retained, bound)
		}
	}
}
