// Real command spelling and the shared device constructor preserve optional
// evidence without changing ordinary admission or sharing mutable role settings.
package miner

import (
	"testing"

	"github.com/docopt/docopt-go"
)

// This is the exact argument vector emitted by bootstrap provider-role-config.
func TestProviderLaunchArgumentsSelectActualDomainOption(t *testing.T) {
	args := []string{"provide", "--api_url=https://operator.example", "--connect_url=wss://operator.example", "--close-report-domain=/synthetic/provider-a.close-domain.json"}
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler, OptionsFirst: false}
	options, err := parser.ParseArgs(mainUsage(), args, "")
	if err != nil || options["--api_url"] != "https://operator.example" || options["--connect_url"] != "wss://operator.example" || options["--close-report-domain"] != "/synthetic/provider-a.close-domain.json" {
		t.Fatal("bootstrap launch arguments do not reach the actual provider parser", options, err)
	}
}

// One launch receives a fresh default settings tree. Changing its domain must
// not relabel another provider, and a missing optional domain remains unsigned.
func TestProviderLaunchDeviceSettingsKeepSeparateDomainOwners(t *testing.T) {
	first, second := ProviderDeviceSettings([32]byte{71}), ProviderDeviceSettings([32]byte{72})
	first.ClientSettings.ContractManagerSettings.CloseReportDomainHash[0] = 99
	if second.ClientSettings.ContractManagerSettings.CloseReportDomainHash != ([32]byte{72}) || first.ClientSettings.ContractManagerSettings == second.ClientSettings.ContractManagerSettings {
		t.Fatal("provider launch shared mutable domain settings")
	}
	legacy := ProviderDeviceSettings([32]byte{})
	if legacy.ClientSettings.ContractManagerSettings.CloseReportDomainHash != ([32]byte{}) {
		t.Fatal("legacy provider acquired guessed signing authority")
	}
}
