// Provider launch declarations reuse exact production admission without changing
// the original bootstrap inspection bytes or returning an activation capability.
package validator

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Endpoints select transport only. The complete domain is the same original
// policy namespace used by operator key registration and validator tunnel work.
type ProductionProviderDomain struct {
	NoId       uint64                          `json:"no_id"`
	ApiUrl     string                          `json:"api_url"`
	ConnectUrl string                          `json:"connect_url"`
	Domain     protocol.ClientKeyHistoryDomain `json:"domain"`
}

// Borrows exact pinned config bytes. The original inspector verifies its signed
// approval; the second decode uses those same bytes and never reopens the path.
func InspectProductionProviderDomains(ctx context.Context, path string, raw []byte) ([]ProductionProviderDomain, error) {
	inspection, err := InspectProductionBootstrapConfig(ctx, path, raw)
	if err != nil {
		return nil, err
	}
	cfg, err := decodeReleaseConfigDocument(path, raw)
	if err != nil {
		return nil, err
	}
	result := make([]ProductionProviderDomain, 0, len(cfg.Operators))
	for _, operator := range cfg.Operators {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		domain := protocol.ClientKeyHistoryDomain{ChainID: inspection.EvmChainId, GenesisHash: common.HexToHash(cfg.GenesisHash),
			Netuid: inspection.Netuid, Coordinator: common.HexToAddress(inspection.Coordinator), SettlementVault: common.HexToAddress(inspection.SettlementVault),
			DeploymentIDHash: sha256.Sum256([]byte(inspection.DeploymentId)), PolicyHash: common.HexToHash(inspection.PolicyHash), NoID: operator.NoID}
		if digest, err := domain.Digest(); err != nil || digest != releaseCloseReportDomain(cfg, operator.NoID) {
			return nil, errors.Join(errors.New("provider domain differs from original producer namespace"), err)
		}
		result = append(result, ProductionProviderDomain{NoId: operator.NoID, ApiUrl: operator.APIURL, ConnectUrl: operator.ConnectURL, Domain: domain})
	}
	return result, nil
}
