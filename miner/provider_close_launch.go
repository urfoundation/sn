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
// carrier on 4053, and tcp 443 still decides whether the extender runs.
func ProviderDeviceSettings(domainHash [32]byte) *sdk.DeviceLocalSettings {
	settings := sdk.DefaultDeviceLocalSettings()
	settings.ClientSettings.ContractManagerSettings.CloseReportDomainHash = domainHash
	settings.ProvideExtenderDnsPrivilegedPort = true
	return settings
}

// The device settings of one slot of a provider run. Only the direct slot runs
// the provider extender role (connect/EXTENDER.md G1, G2). A host holds one
// extender identity and binds the carrier ports once, and every slot has the
// same .provider.extender.key under a client of its own, so each slot that
// activated the key would take the operator's row for it from the one before.
// A proxy slot otherwise provides exactly as the direct slot does.
func providerSlotDeviceSettings(domainHash [32]byte, proxySettings *connect.ProxySettings) *sdk.DeviceLocalSettings {
	settings := ProviderDeviceSettings(domainHash)
	settings.ProvideExtenderEnabled = proxySettings == nil
	return settings
}

// Reads the optional launch artifact once. Both the real CLI and a launcher
// receive the same bounded descriptor/parser result; errors return zero domain.
func ReadProviderCloseReportDomain(path string) ([32]byte, error) {
	return readProviderCloseReportDomain(path)
}
