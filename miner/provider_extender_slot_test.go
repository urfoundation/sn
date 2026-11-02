//go:build linux || darwin

// Which slot of the actual provider run asks for the provider extender role.
// The run test stops before each device, since a direct slot's device would
// bind tcp 443 on the test host; the device test builds a proxy slot's.
package miner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
)

// Each slot the run starts reaches its device with the role asked for in the
// direct slot and in no proxy slot. A run with a proxy file starts proxy slots
// alone (readProxySettings), so it asks for no role at all; a run without one
// is the direct slot alone, which keeps the role.
func TestProviderRunAsksForTheExtenderRoleOnlyInTheDirectSlot(t *testing.T) {
	first := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.30:1080"}
	second := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.31:1080"}
	for _, c := range []struct {
		name    string
		members []*connect.ProxySettings
		expect  map[string]bool
	}{
		{
			name:   "no proxy file",
			expect: map[string]bool{"direct": true},
		},
		{
			name:    "a proxy file",
			members: []*connect.ProxySettings{first, second},
			expect: map[string]bool{
				providerRegistrationSlot(first):  false,
				providerRegistrationSlot(second): false,
			},
		},
		{
			name:    "a direct slot beside proxy slots",
			members: []*connect.ProxySettings{nil, first, second},
			expect: map[string]bool{
				"direct":                         true,
				providerRegistrationSlot(first):  false,
				providerRegistrationSlot(second): false,
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			fixture := newProviderRegistrationFixture(t)
			stop := errors.New("synthetic stop before the provider device")
			var stateLock sync.Mutex
			asked := map[string]bool{}
			hooks := providerRegistrationHooks{beforeDevice: func(slot string, settings *sdk.DeviceLocalSettings) error {
				stateLock.Lock()
				defer stateLock.Unlock()
				asked[slot] = settings.ProvideExtenderEnabled
				return stop
			}}
			if err := fixture.run(t.Context(), true, hooks, c.members...); !errors.Is(err, stop) {
				t.Fatal("a provider slot did not reach its device", err)
			}
			if len(asked) != len(c.expect) {
				t.Fatalf("slots that reached a device = %v, expected %v", asked, c.expect)
			}
			for slot, expected := range c.expect {
				actual, ok := asked[slot]
				if !ok {
					t.Fatalf("slot %q did not reach a device", slot)
				}
				if actual != expected {
					t.Fatalf("slot %q asks for the extender role = %t, expected %t", slot, actual, expected)
				}
			}
		})
	}
}

// A real device built from a proxy slot's settings provides, and the sdk holds
// the role off for the slot's switch alone: the user's setting still reads on.
func TestProviderProxySlotDeviceProvidesWithoutTheExtenderRole(t *testing.T) {
	t.Setenv("URNETWORK_STATE_DIR", providerRegistrationPrivateDir(t))
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		// the device's services get a transient answer from this local fixture
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	proxySettings := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.32:1080"}
	dialSettings := &connect.DialContextSettings{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != server.Listener.Addr().String() {
			return nil, errors.New("synthetic proxy slot refuses nonlocal egress")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	strategy := connect.DefaultClientStrategySettings()
	strategy.EnableResilient = false
	strategy.ProxySettings = proxySettings
	strategy.DialContextSettings = dialSettings
	space := sdk.NewNetworkSpaceWithUrls(ctx, server.URL, "ws"+strings.TrimPrefix(server.URL, "http"), strategy)
	defer space.Close()

	extenderKeySeed, err := connect.NewExtenderKeySeed()
	if err != nil {
		t.Fatal(err)
	}
	settings := providerSlotDeviceSettings([32]byte{}, proxySettings)
	settings.KeyMaterial = providerDeviceKeyMaterial(bytes.Repeat([]byte{19}, ed25519.SeedSize), nil, nil, extenderKeySeed)
	settings.ProviderDialContextSettings = dialSettings
	// a loopback platform names its own pump ingress: the sdk default is a
	// production host, which the auto transport would resolve and send to
	settings.DnsPumpHost = server.Listener.Addr().(*net.TCPAddr).IP.String()
	clientId := connect.NewId()
	device, err := newProviderDeviceLocal(
		ctx,
		space,
		strategy,
		providerRegistrationTestToken(t, clientId.String(), "proxy-slot"),
		"synthetic proxy slot",
		settings,
		nil,
		providerRegistrationSlot(proxySettings),
		clientId,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		join, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if err := device.CloseAndWait(join); err != nil {
			t.Error("the proxy slot's device did not join", err)
		}
	}()

	device.SetProvideControlMode(sdk.ProvideControlModeAlways)
	if !device.GetProvideEnabled() {
		t.Fatal("the proxy slot does not provide")
	}
	if !device.GetProvideExtender() {
		t.Fatal("the extender setting reads off, so the slot's switch proves nothing")
	}
	status := device.GetExtenderProvideStatus()
	if status.State != sdk.ExtenderProvideStateNotProviding || status.Enabled || status.Listening {
		t.Fatalf("status = %+v, expected the role held off", status)
	}
}
