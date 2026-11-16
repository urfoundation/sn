// Launch generators and embedded provider owners share the actual device
// settings constructor; the domain also follows actual key publication and
// rotation, without enabling a new processed-registration startup gate.
package miner

import (
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
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

// The device settings of one slot of a provider run. One slot runs the provider
// extender role (connect/EXTENDER.md G1, G2), the one providerExtenderSlot names.
// A host holds one extender identity and binds the carrier ports once, and every
// slot has the same .provider.extender.key under a client of its own, so each
// slot that activated the key would take the operator's row for it from the one
// before. A slot otherwise provides the same with or without the role.
func providerSlotDeviceSettings(domainHash [32]byte, extenderSlot bool) *sdk.DeviceLocalSettings {
	settings := ProviderDeviceSettings(domainHash)
	settings.ProvideExtenderEnabled = extenderSlot
	return settings
}

// The slot of a provider run that holds the extender role: the direct slot when
// the run has one, otherwise the proxy slot whose name sorts first. A run with a
// proxy file has only proxy slots. The name comes from the proxy's network and
// address alone, so restarts, the file's order and rotated credentials leave the
// role where it is, and only removing that proxy or adding one that sorts first
// moves it.
//
// The role runs from the host whichever slot holds it: its activations and its
// forward dials leave out the slot's proxy, so the operator dials back to the host
// that holds the ports.
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
