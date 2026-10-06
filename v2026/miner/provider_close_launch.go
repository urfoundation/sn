// Launch generators and embedded provider owners share the actual device
// settings constructor; the domain also follows actual key publication and
// rotation, without enabling a new processed-registration startup gate.
package miner

import "github.com/urnetwork/sdk/v2026"

// Fresh settings preserve sdk defaults and copy only the already owned domain.
// A zero domain retains legacy unsigned close reports and key publication.
func ProviderDeviceSettings(domainHash [32]byte) *sdk.DeviceLocalSettings {
	settings := sdk.DefaultDeviceLocalSettings()
	settings.ClientSettings.ContractManagerSettings.CloseReportDomainHash = domainHash
	return settings
}

// Reads the optional launch artifact once. Both the real CLI and a launcher
// receive the same bounded descriptor/parser result; errors return zero domain.
func ReadProviderCloseReportDomain(path string) ([32]byte, error) {
	return readProviderCloseReportDomain(path)
}
