// Launch generators and embedded provider owners share the actual device
// settings constructor; the domain also follows actual key publication and
// rotation, without enabling a new processed-registration startup gate.
package miner

import (
	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
)

// Fresh settings preserve sdk defaults and copy only the already owned domain.
// A zero domain retains legacy unsigned close reports and key publication.
// The miner, unlike the apps, also tries udp 53 for its extender's dns
// carrier beside udp 4053 (connect/EXTENDER.md L2): a refused 53 leaves the
// carrier on 4053, and tcp 443 still decides whether the extender runs. And
// unlike the apps it asks the router to forward the extender's ports unless
// URNETWORK_PORT_MAPPING turns that off (S10, port_mapping.go).
func ProviderDeviceSettings(domainHash [32]byte) *sdk.DeviceLocalSettings {
	settings := sdk.DefaultDeviceLocalSettings()
	settings.ClientSettings.ContractManagerSettings.CloseReportDomainHash = domainHash
	settings.ProvideExtenderDnsPrivilegedPort = true
	settings.DefaultProvideExtenderPortMapping = minerPortMappingEnabled()
	return settings
}

// The device settings of one slot of a provider run. At most one slot runs the
// provider extender role (connect/EXTENDER.md G1, G2): the slot providerExtenderSlot
// names, when it is the direct slot. A host binds the carrier ports once, and
// only that slot's space runs a gossip node and has an extender identity
// (connect#227): the other slots' spaces read its extender stack. A proxy slot
// never runs the role, since the role binds and forwards on this host, outside
// the proxy every other egress of the slot takes (owner 2026-11-18, "route
// through the proxy"); the sdk holds the role off in a proxied space whatever
// this asks. A slot otherwise provides the same with or without the role.
func providerSlotDeviceSettings(domainHash [32]byte, extenderRole bool) *sdk.DeviceLocalSettings {
	settings := ProviderDeviceSettings(domainHash)
	settings.ProvideExtenderEnabled = extenderRole
	return settings
}

// The slot of a provider run that hosts the run's extender stack -- the
// directory, the network client that samples it and the gossip node, which run
// through the slot's own egress (connect#227) -- and that holds the provider
// extender role when it is the direct slot: the direct slot when the run has
// one, otherwise the proxy slot whose name sorts first. A run with a proxy file
// has only proxy slots, so it hosts the stack in a proxy slot and runs no role
// (owner 2026-11-18). The name comes from the proxy's network and address alone,
// so restarts, the file's order and rotated credentials leave the stack where it
// is, and only removing that proxy or adding one that sorts first moves it.
func providerExtenderSlot(providers []*connect.ProxySettings) string {
	extenderSlot := ""
	for _, proxySettings := range providers {
		slot := providerRegistrationSlot(proxySettings)
		if proxySettings == nil {
			return slot
		}
		if extenderSlot == "" || slot < extenderSlot {
			extenderSlot = slot
		}
	}
	return extenderSlot
}

// Reads the optional launch artifact once. Both the real CLI and a launcher
// receive the same bounded descriptor/parser result; errors return zero domain.
func ReadProviderCloseReportDomain(path string) ([32]byte, error) {
	return readProviderCloseReportDomain(path)
}
