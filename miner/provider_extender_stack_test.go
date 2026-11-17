//go:build linux || darwin

// One extender stack per provider run, and one gossip identity per node
// (connect#227). A run of many slots used to build a whole extender stack per
// slot -- directory, network client, gossip node and reports -- and every slot
// read and wrote the one `.provider.extender.key`, so every node presented the
// same identity. Now the extender slot's space hosts the run's one stack, the
// other slots read it, and only the node that runs has a key, in a file of its
// own client id.
//
// The runs here use the registration fixture's local server under a dotted api
// host, so the spaces derive an extender network (connect/EXTENDER.md F1) and
// build what they build for real, with the network client off, so nothing
// leaves the process. Each slot stops before its device.
package miner

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfoundation/sn/clientauth"
	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
)

// The fixture's run settings under a dotted api host, which derives the
// extender network `stack.example`, dialed at the fixture's local server. The
// platform url's first label is neither `connect` nor `api`, so no alt url
// derives and no alt carrier dials.
func providerExtenderStackSettings(t *testing.T, fixture *providerRegistrationFixture, members ...*connect.ProxySettings) providerRunSettings {
	t.Helper()
	address := fixture.server.Listener.Addr().String()
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	settings := fixture.settings(true, members...)
	settings.apiUrl = "http://api.stack.example:" + port
	fixture.apiUrl = settings.apiUrl
	settings.connectUrl = "ws://platform.stack.example:" + port
	settings.testEgressDialer = &connect.DialContextSettings{DialContext: func(ctx context.Context, network, dialAddress string) (net.Conn, error) {
		host, dialPort, err := net.SplitHostPort(dialAddress)
		if err != nil || dialPort != port || !strings.HasSuffix(host, ".stack.example") {
			return nil, errors.New("synthetic stack fixture refuses nonlocal egress")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	// the network client would resolve the extender dns name and dial
	// hello; nothing else of the stack leaves the process
	previous := sdk.SetExtenderNetworkClientEnabled(false)
	t.Cleanup(func() {
		sdk.SetExtenderNetworkClientEnabled(previous)
	})
	return settings
}

// Runs `settings` until every slot reached its device, with `atDevice` called
// once, by the last slot to arrive, while every slot's space is built.
func runProviderExtenderStack(
	t *testing.T,
	settings providerRunSettings,
	slotCount int,
	beforeDevice func(slot string, settings *sdk.DeviceLocalSettings),
	atDevice func(),
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	stop := errors.New("synthetic stop before the provider device")
	var stateLock sync.Mutex
	arrived := 0
	release := make(chan struct{})
	hooks := providerRegistrationHooks{beforeDevice: func(slot string, deviceSettings *sdk.DeviceLocalSettings) error {
		if beforeDevice != nil {
			beforeDevice(slot, deviceSettings)
		}
		last := func() bool {
			stateLock.Lock()
			defer stateLock.Unlock()
			arrived += 1
			return arrived == slotCount
		}()
		if last {
			if atDevice != nil {
				atDevice()
			}
			close(release)
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return stop
	}}
	err := settings.run(context.WithValue(ctx, providerRegistrationHooksKey{}, hooks), &providerRefusedWriter{})
	if !errors.Is(err, stop) {
		t.Fatal("a provider slot did not reach its device", err)
	}
	if arrived != slotCount {
		t.Fatalf("%d slots reached a device, expected %d", arrived, slotCount)
	}
}

// The goroutines whose stack runs through `function`, a qualified function
// name. Each extender directory, the root of a stack, runs one save loop from
// its constructor until its close joins it, so these count stacks rather than
// time anything.
func countProviderGoroutinesIn(function string) int {
	stacks := make([]byte, 64*1024*1024)
	stacks = stacks[:runtime.Stack(stacks, true)]
	count := 0
	for _, goroutine := range strings.Split(string(stacks), "\n\n") {
		if strings.Contains(goroutine, function) {
			count += 1
		}
	}
	return count
}

const providerTestDirectoryLoop = "github.com/urnetwork/connect.(*ExtenderDirectory).run("

// Root cause of connect#227's CPU: a run built one extender stack per slot,
// so a miner with 1000 proxies ran 1000 directories, network clients, gossip
// nodes and reports on one host. A run of several slots builds exactly one.
func TestProviderRunBuildsOneExtenderStackForAllItsSlots(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	members := []*connect.ProxySettings{
		{Network: "tcp", Address: "192.0.2.60:1080"},
		{Network: "tcp", Address: "192.0.2.61:1080"},
		{Network: "tcp", Address: "192.0.2.62:1080"},
	}
	settings := providerExtenderStackSettings(t, fixture, members...)
	directoryLoops := countProviderGoroutinesIn(providerTestDirectoryLoop)
	runProviderExtenderStack(t, settings, len(members), nil, func() {
		if count := countProviderGoroutinesIn(providerTestDirectoryLoop) - directoryLoops; count != 1 {
			t.Errorf("%d slots run %d extender stacks, expected one for the run", len(members), count)
		}
	})
	// the run closed what it built
	if count := countProviderGoroutinesIn(providerTestDirectoryLoop) - directoryLoops; count != 0 {
		t.Fatalf("%d extender directories outlived the run", count)
	}
}

// The client id a slot of the fixture authenticated as.
func providerExtenderStackClientId(t *testing.T, proxySettings *connect.ProxySettings) connect.Id {
	t.Helper()
	path, err := providerClientJwtPath(proxySettings)
	if err != nil {
		t.Fatal(err)
	}
	token, err := clientauth.ReadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	clientId, err := clientauth.ClientIdFromJwt(token)
	if err != nil {
		t.Fatal(err)
	}
	return clientId
}

// Writes an install's shared extender key, as every run before per-node keys
// left it. The install's client key custody comes first, from a run of its
// slots that stops before their devices, since a provider file without it is
// history the runner refuses to create a key over.
func writeLegacyProviderExtenderKey(t *testing.T, settings providerRunSettings, slotCount int, dir string) []byte {
	t.Helper()
	runProviderExtenderStack(t, settings, slotCount, nil, nil)
	seed, err := connect.NewExtenderKeySeed()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".provider.extender.key"), seed, 0600); err != nil {
		t.Fatal(err)
	}
	return seed
}

// The extender identity each slot's device is built with.
func runProviderExtenderStackIdentities(t *testing.T, settings providerRunSettings, slotCount int) map[string][]byte {
	t.Helper()
	var stateLock sync.Mutex
	slotSeeds := map[string][]byte{}
	runProviderExtenderStack(t, settings, slotCount, func(slot string, deviceSettings *sdk.DeviceLocalSettings) {
		stateLock.Lock()
		defer stateLock.Unlock()
		var seed []byte
		if deviceSettings.KeyMaterial != nil {
			seed = deviceSettings.KeyMaterial.GetExtenderKeySeed()
		}
		slotSeeds[slot] = seed
	}, nil)
	return slotSeeds
}

// Root cause of the shared mesh identity: every slot read the one
// `.provider.extender.key`, so every gossip node presented one peer id and
// the mesh kept a stream to one of them at a time. Only the extender slot,
// whose space runs the run's node, gets an identity: the install's existing
// key, which keeps the peer id and the extender record it had, moved to the
// file of that slot's own client id, so no later node can take it.
func TestProviderRunGivesOnlyTheNodeAnExtenderIdentity(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	first := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.63:1080"}
	second := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.64:1080"}
	holder, other := first, second
	if providerRegistrationSlot(second) < providerRegistrationSlot(first) {
		holder, other = second, first
	}
	settings := providerExtenderStackSettings(t, fixture, first, second)
	legacySeed := writeLegacyProviderExtenderKey(t, settings, 2, fixture.dir)
	slotSeeds := runProviderExtenderStackIdentities(t, settings, 2)

	if seed := slotSeeds[providerRegistrationSlot(other)]; seed != nil {
		t.Fatal("a slot that runs no gossip node carries an extender identity")
	}
	if seed := slotSeeds[providerRegistrationSlot(holder)]; !bytes.Equal(seed, legacySeed) {
		t.Fatal("the extender slot did not keep the install's extender identity")
	}
	if _, err := os.Stat(filepath.Join(fixture.dir, ".provider.extender.key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the shared extender key is still there for another node to take", err)
	}
	holderClientId := providerExtenderStackClientId(t, holder)
	keySeed, err := os.ReadFile(filepath.Join(fixture.dir, ".provider.extender."+holderClientId.String()+".key"))
	if err != nil || !bytes.Equal(keySeed, legacySeed) {
		t.Fatal("the install's key is not kept under the node's own client id", err)
	}
	otherClientId := providerExtenderStackClientId(t, other)
	if _, err := os.Stat(filepath.Join(fixture.dir, ".provider.extender."+otherClientId.String()+".key")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a slot that runs no gossip node has an extender key file", err)
	}
}

// A node never presents another node's key: when the extender role moves to
// another slot -- its proxy left the file -- that slot's node does not take
// the key the install's first node adopted, and starts on a key of its own.
func TestProviderRunNeverHandsAnInstallsKeyToASecondNode(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	first := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.65:1080"}
	second := &connect.ProxySettings{Network: "tcp", Address: "192.0.2.66:1080"}
	holder, other := first, second
	if providerRegistrationSlot(second) < providerRegistrationSlot(first) {
		holder, other = second, first
	}
	settings := providerExtenderStackSettings(t, fixture, first, second)
	legacySeed := writeLegacyProviderExtenderKey(t, settings, 2, fixture.dir)
	slotSeeds := runProviderExtenderStackIdentities(t, settings, 2)
	if !bytes.Equal(slotSeeds[providerRegistrationSlot(holder)], legacySeed) {
		t.Fatal("the first node did not adopt the install's key")
	}

	// the holder's proxy leaves the file, so the other slot holds the role
	slotSeeds = runProviderExtenderStackIdentities(t, providerExtenderStackSettings(t, fixture, other), 1)
	if seed := slotSeeds[providerRegistrationSlot(other)]; bytes.Equal(seed, legacySeed) {
		t.Fatal("a second node took the key the first node presents")
	}
}

// An install that ran one node keeps its key across the move to per-node
// files and every run after it, so its peer id and extender record do not
// churn.
func TestProviderRunKeepsASingleNodeInstallsKey(t *testing.T) {
	fixture := newProviderRegistrationFixture(t)
	settings := providerExtenderStackSettings(t, fixture)
	legacySeed := writeLegacyProviderExtenderKey(t, settings, 1, fixture.dir)
	for run := range 2 {
		slotSeeds := runProviderExtenderStackIdentities(t, settings, 1)
		if seed := slotSeeds["direct"]; !bytes.Equal(seed, legacySeed) {
			t.Fatalf("run %d: the direct slot's node did not keep the install's key", run)
		}
	}
	clientId := providerExtenderStackClientId(t, nil)
	keySeed, err := os.ReadFile(filepath.Join(fixture.dir, ".provider.extender."+clientId.String()+".key"))
	if err != nil || !bytes.Equal(keySeed, legacySeed) {
		t.Fatal("the install's key is not kept under the node's own client id", err)
	}
}
