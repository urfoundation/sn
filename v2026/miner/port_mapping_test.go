package miner

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"
	"github.com/urnetwork/sdk/v2026"
)

// The miner's router port mapping (connect/EXTENDER.md S10): on by default,
// unlike the apps, off by URNETWORK_PORT_MAPPING or --no-port-mapping, and
// reported by provider status and provider doctor.

// The miner asks the router by default; the sdk's default, every app's, does
// not (owner, 2026-11-11).
func TestProviderDeviceSettingsAskTheRouterByDefault(t *testing.T) {
	t.Setenv(minerPortMappingEnv, "")
	if sdk.DefaultDeviceLocalSettings().DefaultProvideExtenderPortMapping {
		t.Fatal("the sdk default maps the router's ports, so the miner setting proves nothing")
	}
	for _, domainHash := range [][32]byte{{}, {71}} {
		if !ProviderDeviceSettings(domainHash).DefaultProvideExtenderPortMapping {
			t.Fatalf("domain %x: the miner's extender does not ask the router", domainHash[:1])
		}
		for _, extenderSlot := range []bool{true, false} {
			if !providerSlotDeviceSettings(domainHash, extenderSlot).DefaultProvideExtenderPortMapping {
				t.Fatalf("domain %x slot %t: the slot lost the miner's default", domainHash[:1], extenderSlot)
			}
		}
	}
}

func TestProviderDeviceSettingsPortMappingOffByEnvironment(t *testing.T) {
	for _, c := range []struct {
		value   string
		enabled bool
	}{
		{value: "0", enabled: false},
		{value: "off", enabled: false},
		{value: " OFF ", enabled: false},
		{value: "false", enabled: false},
		{value: "no", enabled: false},
		{value: "1", enabled: true},
		{value: "on", enabled: true},
		{value: "", enabled: true},
	} {
		t.Setenv(minerPortMappingEnv, c.value)
		if enabled := ProviderDeviceSettings([32]byte{}).DefaultProvideExtenderPortMapping; enabled != c.enabled {
			t.Errorf("%s=%q: port mapping = %t, expected %t", minerPortMappingEnv, c.value, enabled, c.enabled)
		}
	}
}

// --no-port-mapping parses on every form of provide and on auth-provide.
func TestNoPortMappingParsesOnEveryProvide(t *testing.T) {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler, SkipHelpFlags: true}
	for _, args := range [][]string{
		{"provide", "--no-port-mapping"},
		{"provide", "--all-operators", "--no-port-mapping"},
		{"auth-provide", "code", "--no-port-mapping"},
	} {
		opts, err := parser.ParseArgs(mainUsage(), args, "")
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if noPortMapping, _ := opts.Bool("--no-port-mapping"); !noPortMapping {
			t.Fatalf("%v: --no-port-mapping = false", args)
		}
	}
	if !strings.Contains(mainUsage(), "--no-port-mapping                    Don't ask the router") {
		t.Fatal("the options do not describe --no-port-mapping")
	}
}

// A service installed with URNETWORK_PORT_MAPPING=0 keeps it off.
func TestMinerServiceEnvironmentCarriesPortMapping(t *testing.T) {
	t.Setenv(minerPortMappingEnv, "0")
	if !slices.Contains(minerServiceEnvironment(), minerPortMappingEnv+"=0") {
		t.Fatalf("service environment = %v, expected %s=0", minerServiceEnvironment(), minerPortMappingEnv)
	}
}

// The extender slot's status reaches the status file; a slot with no holder
// feeds nothing.
func TestMinerPortMappingHolderObservesTheExtender(t *testing.T) {
	holder := newMinerPortMappingHolder(true)
	listener := newProviderExtenderStatusListener(nil, 0)
	listener.portMapping = holder
	listener.ExtenderProvideStatusChanged(&sdk.ExtenderProvideStatus{
		Enabled:            true,
		PortMapping:        sdk.ExtenderPortMappingMapped,
		PortMappingMethod:  "upnp",
		PortMappingTcpPort: 40443,
		PortMappingUdpPort: 443,
		PortMappingDnsPort: 4053,
		PortMappingError:   "udp 4053: the router's own address 198.51.100.9 is not public",
	})
	status := holder.snapshot()
	if !status.Enabled || status.State != sdk.ExtenderPortMappingMapped || status.Method != "upnp" ||
		status.TcpPort != 40443 || status.UdpPort != 443 || status.DnsPort != 4053 {
		t.Fatalf("status = %+v, expected the extender's mapping", status)
	}
	raw, err := json.Marshal(&minerLocalStatus{Schema: minerStatusSchema, PortMapping: status})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"port_mapping":{"enabled":true,"state":"mapped","method":"upnp","tcp_port":40443`)) {
		t.Fatalf("status file = %s", raw)
	}
	// the router's words can name an address, and stay out of the status file
	if bytes.Contains(raw, []byte("198.51.100.9")) {
		t.Fatalf("status file = %s, expected no router text", raw)
	}
	var holderless *minerPortMappingHolder
	holderless.observe(&sdk.ExtenderProvideStatus{PortMapping: sdk.ExtenderPortMappingFailed})
	if holderless.snapshot() != nil {
		t.Fatal("a slot with no holder reported a mapping")
	}
}

func TestMinerPortMappingRows(t *testing.T) {
	for _, c := range []struct {
		status minerPortMappingStatus
		ok     any
		warn   bool
		detail string
	}{
		{status: minerPortMappingStatus{Enabled: false}, ok: nil, detail: "off (URNETWORK_PORT_MAPPING=0 or --no-port-mapping)"},
		{status: minerPortMappingStatus{Enabled: true}, ok: nil, detail: "on, waiting for the extender"},
		{status: minerPortMappingStatus{Enabled: true, State: sdk.ExtenderPortMappingNotNeeded}, ok: true, detail: "not needed: this device has a public IPv4 address"},
		{status: minerPortMappingStatus{Enabled: true, State: sdk.ExtenderPortMappingMapping}, ok: nil, detail: "asking the router"},
		{
			status: minerPortMappingStatus{Enabled: true, State: sdk.ExtenderPortMappingMapped, Method: "pcp", TcpPort: 443, UdpPort: 443, DnsPort: 4053},
			ok:     true,
			detail: "the router forwards TCP 443, UDP 443, UDP 4053 (pcp)",
		},
		{
			status: minerPortMappingStatus{Enabled: true, State: sdk.ExtenderPortMappingMapped, Method: "upnp", TcpPort: 40443, DnsPort: 4053},
			ok:     true,
			detail: "the router forwards TCP 40443 to 443, UDP 4053 (upnp)",
		},
		{
			status: minerPortMappingStatus{Enabled: true, State: sdk.ExtenderPortMappingFailed},
			ok:     nil,
			warn:   true,
			detail: "the router didn't open the extender's ports by UPnP, NAT-PMP or PCP; the miner's log says why",
		},
	} {
		ok, warn, detail := minerPortMappingRow(&c.status)
		if ok != c.ok || warn != c.warn || detail != c.detail {
			t.Errorf("%+v = %v %t %q, expected %v %t %q", c.status, ok, warn, detail, c.ok, c.warn, c.detail)
		}
	}
}

// provider status prints the line while the running miner reports it, and a
// failed mapping is a warning, not a problem.
func TestStatusWriterPrintsThePortMapping(t *testing.T) {
	var out bytes.Buffer
	w := statusWriter{out: &out}
	w.portMapping(nil)
	if out.Len() != 0 {
		t.Fatalf("a miner that reports no mapping printed %q", out.String())
	}
	w.portMapping(&minerPortMappingStatus{Enabled: true, State: sdk.ExtenderPortMappingFailed})
	if line := out.String(); !strings.Contains(line, "! Port mapping") || !strings.Contains(line, "the miner's log says why") {
		t.Fatalf("line = %q, expected the warning pointing at the log", line)
	}
}
