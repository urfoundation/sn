// Optional domain files select signing without changing ordinary provider admission.
package miner

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/docopt/docopt-go"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Every domain field is synthetic and independent of API endpoint selection.
func providerCloseDomainFixture() protocol.ClientKeyHistoryDomain {
	return protocol.ClientKeyHistoryDomain{ChainID: 945, GenesisHash: [32]byte{1}, Netuid: 521, Coordinator: common.Address{2}, SettlementVault: common.Address{3}, DeploymentIDHash: [32]byte{4}, PolicyHash: [32]byte{5}, NoID: 6}
}

// Actual CLI spellings and strict file reading preserve a complete explicit domain.
func TestProviderCloseReportDomainActualCliAndOwnedFile(t *testing.T) {
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler, OptionsFirst: false}
	for _, argv := range [][]string{{"provide", "--close-report-domain=/synthetic/domain.json"}, {"auth-provide", "synthetic-code", "--close-report-domain=/synthetic/domain.json"}} {
		opts, err := parser.ParseArgs(mainUsage(), argv, "")
		if err != nil || opts["--close-report-domain"] != "/synthetic/domain.json" {
			t.Fatal("actual provider command lost optional original domain", opts, err)
		}
	}
	domain := providerCloseDomainFixture()
	data, _ := json.Marshal(domain)
	path := filepath.Join(t.TempDir(), "domain.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := readProviderCloseReportDomain(path)
	want, _ := domain.Digest()
	if err != nil || digest != want {
		t.Fatal("original domain file was relabeled", err)
	}
	domain.PolicyHash[0]++
	changed, _ := json.Marshal(domain)
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if digest != want {
		t.Fatal("later domain file mutation changed owned signing identity")
	}
}

// Unavailable/ambiguous optional data returns no guessed namespace; callers
// retain the ordinary provider path and the economic evidence stays unknown.
func TestProviderCloseReportAbsentAndInvalidDomainRemainUnsigned(t *testing.T) {
	if domain, err := readProviderCloseReportDomain(""); err != nil || domain != ([32]byte{}) {
		t.Fatal("legacy provider acquired a domain", domain, err)
	}
	path := filepath.Join(t.TempDir(), "domain.json")
	if domain, err := readProviderCloseReportDomain(path); err == nil || domain != ([32]byte{}) {
		t.Fatal("missing optional file acquired authority", domain, err)
	}
	for _, data := range [][]byte{[]byte(`{"chain_id":945,"chain_id":946}`), []byte(`{}`), bytes.Repeat([]byte{'x'}, 16*1024+1)} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if domain, err := readProviderCloseReportDomain(path); err == nil || domain != ([32]byte{}) {
			t.Fatal("malformed optional domain was admitted", domain, err)
		}
	}
	settings := swarmMemberDeviceSettings(ProviderSwarmMember{CloseReportDomain: &protocol.ClientKeyHistoryDomain{}}, nil, nil)
	if settings.ClientSettings.ContractManagerSettings.CloseReportDomainHash != ([32]byte{}) || !settings.ClientSettings.ClientKeyRegistrationRequired {
		t.Fatal("optional domain changed ordinary swarm key admission")
	}
}

// Every swarm member receives a fresh settings owner and its exact independent domain.
func TestProviderSwarmCloseReportDomainsDoNotCrossMembers(t *testing.T) {
	first, second := providerCloseDomainFixture(), providerCloseDomainFixture()
	second.NoID++
	a := swarmMemberDeviceSettings(ProviderSwarmMember{CloseReportDomain: &first}, nil, nil)
	b := swarmMemberDeviceSettings(ProviderSwarmMember{CloseReportDomain: &second}, nil, nil)
	wantA, _ := first.Digest()
	wantB, _ := second.Digest()
	if a.ClientSettings.ContractManagerSettings.CloseReportDomainHash != wantA || b.ClientSettings.ContractManagerSettings.CloseReportDomainHash != wantB || wantA == wantB {
		t.Fatal("independent swarm members borrowed original domains")
	}
	a.ClientSettings.ContractManagerSettings.CloseReportDomainHash[0]++
	if b.ClientSettings.ContractManagerSettings.CloseReportDomainHash != wantB {
		t.Fatal("member settings shared mutable original domain")
	}
}
