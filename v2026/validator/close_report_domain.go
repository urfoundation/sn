// Validator-owned tunnel work uses the same admitted client-key policy domain.
// This signs its own reports only; it is not whole consumer-work coverage.
package validator

import (
	"crypto/sha256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// Production configuration has already been admitted. Incomplete standalone
// measurement configuration remains unsigned instead of acquiring guessed authority.
func releaseCloseReportDomain(cfg *ReleaseConfig, noId uint64) [32]byte {
	if cfg == nil || cfg.DeploymentID == "" || !payoutartifact.IsDigest(cfg.GenesisHash, "0x") || !payoutartifact.IsDigest(cfg.PolicyHash, "0x") || !common.IsHexAddress(cfg.Coordinator) || !common.IsHexAddress(cfg.SettlementVault) {
		return [32]byte{}
	}
	domain := protocol.ClientKeyHistoryDomain{ChainID: cfg.ChainID, GenesisHash: common.HexToHash(cfg.GenesisHash), Netuid: cfg.Netuid, Coordinator: common.HexToAddress(cfg.Coordinator), SettlementVault: common.HexToAddress(cfg.SettlementVault), DeploymentIDHash: sha256.Sum256([]byte(cfg.DeploymentID)), PolicyHash: common.HexToHash(cfg.PolicyHash), NoID: noId}
	digest, _ := domain.Digest()
	return digest
}

// Every independently derived tunnel client gets fresh settings but the same
// immutable operator policy domain retained by this transport owner.
func (self *TunnelTransport) closeReportClientSettings() *connect.ClientSettings {
	settings := newTunnelClientSettings()
	settings.ContractManagerSettings.CloseReportDomainHash = self.cfg.CloseReportDomainHash
	return settings
}
