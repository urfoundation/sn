package miner

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
)

// The router port mapping of the miner's extender (connect/EXTENDER.md S10): the
// extender asks the router, by UPnP, NAT-PMP or PCP, to forward its own ports --
// TCP 443, UDP 443 and UDP 4053 -- to this device, so a miner behind a home
// router can be an extender. The miner turns it on by default and the apps
// leave it off (owner, 2026-11-11). URNETWORK_PORT_MAPPING=0 turns it off for a
// run and for the service it installs, and provide's --no-port-mapping for one
// run. A device with a public address maps nothing either way.

const minerPortMappingEnv = "URNETWORK_PORT_MAPPING"

// Whether this run asks the router to forward the extender's ports: on unless
// URNETWORK_PORT_MAPPING says off.
func minerPortMappingEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(minerPortMappingEnv))) {
	case "0", "false", "off", "no":
		return false
	}
	return true
}

// The port mapping as the extender last reported it, which the running miner
// writes to its status file for provider status and provider doctor. The state
// is the sdk's closed set (sdk.ExtenderPortMapping*), the method one of pcp,
// nat-pmp and upnp, and the ports the external ones the router forwards to the
// extender's TCP 443, UDP 443 and UDP 4053. Never the router's words, which can
// name the gateway's or the router's own address (extender_standing.go keeps
// the status file to a closed vocabulary for the same reason); the miner's log
// has them.
type minerPortMappingStatus struct {
	Enabled bool   `json:"enabled"`
	State   string `json:"state,omitempty"`
	Method  string `json:"method,omitempty"`
	TcpPort int    `json:"tcp_port,omitempty"`
	UdpPort int    `json:"udp_port,omitempty"`
	DnsPort int    `json:"dns_port,omitempty"`
}

// The run's port mapping, from the extender slot's status. Safe for concurrent
// use, and nil-safe for a run with no extender slot.
type minerPortMappingHolder struct {
	stateLock sync.Mutex
	status    minerPortMappingStatus
}

func newMinerPortMappingHolder(enabled bool) *minerPortMappingHolder {
	return &minerPortMappingHolder{status: minerPortMappingStatus{Enabled: enabled}}
}

func (self *minerPortMappingHolder) observe(status *sdk.ExtenderProvideStatus) {
	if self == nil || status == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.status.State = status.PortMapping
	self.status.Method = status.PortMappingMethod
	self.status.TcpPort = status.PortMappingTcpPort
	self.status.UdpPort = status.PortMappingUdpPort
	self.status.DnsPort = status.PortMappingDnsPort
}

// A copy, nil for a run with no holder.
func (self *minerPortMappingHolder) snapshot() *minerPortMappingStatus {
	if self == nil {
		return nil
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	status := self.status
	return &status
}

// The Port mapping line of provider status and provider doctor: ok is true for
// ✓ and nil for –, and warn asks for the ! of a warning, which is never counted
// as a problem: the miner provides without the mapping.
func minerPortMappingRow(status *minerPortMappingStatus) (ok any, warn bool, detail string) {
	if !status.Enabled {
		return nil, false, "off (" + minerPortMappingEnv + "=0 or --no-port-mapping)"
	}
	switch status.State {
	case "":
		return nil, false, "on, waiting for the extender"
	case sdk.ExtenderPortMappingNotNeeded:
		return true, false, "not needed: this device has a public IPv4 address"
	case sdk.ExtenderPortMappingMapping:
		return nil, false, "asking the router"
	case sdk.ExtenderPortMappingMapped:
		parts := []string{}
		for _, port := range []struct {
			label    string
			internal int
			external int
		}{
			{label: "TCP", internal: connect.ExtenderTcpPort, external: status.TcpPort},
			{label: "UDP", internal: connect.ExtenderQuicPort, external: status.UdpPort},
			{label: "UDP", internal: connect.ExtenderDnsPort, external: status.DnsPort},
		} {
			switch {
			case port.external == 0:
			case port.external == port.internal:
				parts = append(parts, fmt.Sprintf("%s %d", port.label, port.internal))
			default:
				parts = append(parts, fmt.Sprintf("%s %d to %d", port.label, port.external, port.internal))
			}
		}
		detail = "the router forwards " + strings.Join(parts, ", ")
		if status.Method != "" {
			detail += " (" + status.Method + ")"
		}
		return true, false, detail
	case sdk.ExtenderPortMappingFailed:
		// the reason is in the log ("port mapping failed"), not here
		return nil, true, "the router didn't open the extender's ports by UPnP, NAT-PMP or PCP; the miner's log says why"
	default:
		return nil, false, "off"
	}
}
