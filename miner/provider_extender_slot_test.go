//go:build linux || darwin

// Which slot of the actual provider run asks for the provider extender role.
// The run test stops before each device, since the slot with the role would
// bind tcp 443 on the test host; the device test builds a proxy slot without it.
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

	"golang.org/x/net/proxy"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
)

// Each slot the run starts reaches its device, and exactly one asks for the role.
// A run with a proxy file starts proxy slots alone (readProxySettings), and one
// of them holds the role, so the host still has its extender. The file's order
// doesn't choose which, since readProxySettings reads a map.
func TestProviderRunAsksForTheExtenderRoleInOneSlot(t *testing.T) {
	first := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.30:1080"}
	second := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.31:1080"}
	holder, other := first, second
	if providerRegistrationSlot(second) < providerRegistrationSlot(first) {
		holder, other = second, first
	}
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
				providerRegistrationSlot(holder): true,
				providerRegistrationSlot(other):  false,
			},
		},
		{
			name:    "a proxy file in the other order",
			members: []*connect.ProxySettings{second, first},
			expect: map[string]bool{
				providerRegistrationSlot(holder): true,
				providerRegistrationSlot(other):  false,
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
	settings := providerSlotDeviceSettings([32]byte{}, false)
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

// The role stays with one slot across restarts and the file's order, since the
// operator's row for the host's key follows whichever client last activated it.
// Credentials aren't part of a slot's name, so rotating them doesn't move it.
func TestProviderExtenderSlotHoldsAcrossOrderAndCredentials(t *testing.T) {
	a := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.40:1080"}
	b := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.41:1080"}
	c := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.42:1080"}
	chosen := providerExtenderSlot([]*connect.ProxySettings{a, b, c})
	if chosen == "" || chosen == "direct" {
		t.Fatalf("a run of proxy slots chose %q", chosen)
	}
	holders := 0
	for _, member := range []*connect.ProxySettings{a, b, c} {
		if providerRegistrationSlot(member) == chosen {
			holders += 1
		}
	}
	if holders != 1 {
		t.Fatalf("%d slots hold the role, expected 1", holders)
	}
	for _, members := range [][]*connect.ProxySettings{
		{a, c, b}, {b, a, c}, {b, c, a}, {c, a, b}, {c, b, a},
	} {
		if actual := providerExtenderSlot(members); actual != chosen {
			t.Fatalf("the file's order moved the role to %q from %q", actual, chosen)
		}
	}

	rotate := func(member *connect.ProxySettings) *connect.ProxySettings {
		return &connect.ProxySettings{
			Network: member.Network,
			Address: member.Address,
			Auth:    &proxy.Auth{User: "rotated", Password: "rotated"},
		}
	}
	if actual := providerExtenderSlot([]*connect.ProxySettings{rotate(a), rotate(b), rotate(c)}); actual != chosen {
		t.Fatalf("rotated credentials moved the role to %q from %q", actual, chosen)
	}

	// a slot that leaves the file only moves the role if it held it
	for _, member := range []*connect.ProxySettings{a, b, c} {
		var rest []*connect.ProxySettings
		for _, other := range []*connect.ProxySettings{a, b, c} {
			if other != member {
				rest = append(rest, other)
			}
		}
		actual := providerExtenderSlot(rest)
		if providerRegistrationSlot(member) == chosen {
			if actual == chosen {
				t.Fatal("the role stayed with a slot that left the file")
			}
		} else if actual != chosen {
			t.Fatalf("a slot without the role left, and the role moved to %q", actual)
		}
	}

	if actual := providerExtenderSlot([]*connect.ProxySettings{a, nil, b}); actual != "direct" {
		t.Fatalf("a run with a direct slot chose %q", actual)
	}
	if actual := providerExtenderSlot(nil); actual != "" {
		t.Fatalf("a run with no slots chose %q", actual)
	}
}
