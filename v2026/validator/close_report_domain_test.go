// Validator tunnels sign only their own original work under the admitted namespace.
package validator

import (
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Exact domain bytes match the independently used key-history authority grammar.
func TestValidatorCloseReportDomainMatchesIndependentKeyHistory(t *testing.T) {
	cfg := &ReleaseConfig{ChainID: 945, GenesisHash: "0x" + strings.Repeat("12", 32), Netuid: 521, Coordinator: common.Address{2}.Hex(), SettlementVault: common.Address{3}.Hex(), DeploymentID: "synthetic-validator-original", PolicyHash: "0x" + strings.Repeat("34", 32)}
	want, err := (protocol.ClientKeyHistoryDomain{ChainID: cfg.ChainID, GenesisHash: [32]byte(common.HexToHash(cfg.GenesisHash)), Netuid: cfg.Netuid, Coordinator: common.HexToAddress(cfg.Coordinator), SettlementVault: common.HexToAddress(cfg.SettlementVault), DeploymentIDHash: sha256.Sum256([]byte(cfg.DeploymentID)), PolicyHash: [32]byte(common.HexToHash(cfg.PolicyHash)), NoID: 7}).Digest()
	if err != nil || releaseCloseReportDomain(cfg, 7) != want || releaseCloseReportDomain(cfg, 8) == want {
		t.Fatal("validator report domain differs from original policy namespace", err)
	}
	owner := &TunnelTransport{cfg: TunnelTransportConfig{CloseReportDomainHash: want}}
	first, second := owner.closeReportClientSettings(), owner.closeReportClientSettings()
	first.ContractManagerSettings.CloseReportDomainHash[0]++
	if second.ContractManagerSettings.CloseReportDomainHash != want || !second.ClientKeyRegistrationRequired {
		t.Fatal("derived tunnel clients shared or lost original authority")
	}
	cfg.GenesisHash = "0x12"
	if releaseCloseReportDomain(cfg, 7) != ([32]byte{}) || releaseCloseReportDomain(nil, 7) != ([32]byte{}) {
		t.Fatal("incomplete standalone configuration acquired signing authority")
	}
}
