//go:build linux || darwin

// Settlement v2 moves an operator pool's weight destination to vault2's
// immutable pool hotkey from the pool's first v2 epoch (VERSION2-FINAL.md
// §9.1). Live steering, replay, audits and the production row all resolve the
// destination here, so the production row's agreement holds across the switch.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/stabi"
)

// SettlementV2Addresses names the two contracts of the optional
// `settlement_v2` configuration block (§13). The zero value means the block
// is absent, and every epoch keeps today's operatorAt destination.
type SettlementV2Addresses struct {
	Settlement common.Address
	Vault      common.Address
}

func (self SettlementV2Addresses) configured() bool {
	return self != SettlementV2Addresses{}
}

// releaseSettlementV2Addresses is the one place a release configuration's
// `settlement_v2` block reaches steering, replay and the startup identity
// check. The loader admitted both addresses as nonzero lowercase hex.
func releaseSettlementV2Addresses(cfg *ReleaseConfig) SettlementV2Addresses {
	if cfg.SettlementV2 == nil {
		return SettlementV2Addresses{}
	}
	return SettlementV2Addresses{Settlement: common.HexToAddress(cfg.SettlementV2.Settlement), Vault: common.HexToAddress(cfg.SettlementV2.Vault)}
}

// settlementAt's identity version for a pool from its first v2 epoch.
const settlementV2IdentityVersion = 2

// settlementV2ViewAtHashContext reads one fixed-shape v2 view at a canonical
// block. An answer the ABI would not re-encode byte for byte is refused, as
// canonicalReleaseDecisionV2View refuses one from the coordinator.
func (self *ChainClient) settlementV2ViewAtHashContext(ctx context.Context, to common.Address, metadata *bind.MetaData, method string, arguments []any, block uint64, blockHash [32]byte) ([]any, error) {
	contractABI, err := metadata.ParseABI()
	if err != nil {
		return nil, err
	}
	calldata, err := contractABI.Pack(method, arguments...)
	if err != nil {
		return nil, err
	}
	encoded, err := self.ethCallAtHashContext(ctx, to, calldata, block, blockHash)
	if err != nil {
		return nil, err
	}
	values, err := contractABI.Unpack(method, encoded)
	if err != nil {
		return nil, fmt.Errorf("settlement v2 %s view: %w", method, err)
	}
	canonical, err := contractABI.Methods[method].Outputs.Pack(values...)
	if err != nil || !bytes.Equal(encoded, canonical) {
		return nil, errors.Join(fmt.Errorf("settlement v2 %s view is noncanonical", method), err)
	}
	return values, nil
}

// releasePoolHotkeyContext returns the hotkey that receives noID's pool
// weight in epoch, given operatorAt's pool hotkey for that epoch. A pool
// registered in vault2, read at the finalized head, takes vault2's immutable
// hotkey from its first epoch, and STSettlement2's settlementAt at the pinned
// block must name exactly that pool, so an upgrade cannot redirect emission.
// Every other epoch keeps operatorAt: replay never reads a contract that did
// not exist yet, and after the switch a pruned v1 pool hotkey is not consulted.
func (self *ChainClient) releasePoolHotkeyContext(ctx context.Context, v2 SettlementV2Addresses, headBlock uint64, headHash [32]byte, block uint64, blockHash [32]byte, noID, epoch uint64, operatorHotkey [32]byte) ([32]byte, error) {
	if !v2.configured() {
		return operatorHotkey, nil
	}
	id := new(big.Int).SetUint64(noID)
	values, err := self.settlementV2ViewAtHashContext(ctx, v2.Vault, &stabi.STSettlementVault2MetaData, "pools", []any{id}, headBlock, headHash)
	if err != nil {
		return [32]byte{}, fmt.Errorf("no_id %d vault2 pool: %w", noID, err)
	}
	pool := *abi.ConvertType(values[0], new(stabi.ISTSettlementVault2Pool)).(*stabi.ISTSettlementVault2Pool)
	if !pool.Registered || epoch < pool.FirstEpoch {
		return operatorHotkey, nil
	}
	values, err = self.settlementV2ViewAtHashContext(ctx, v2.Settlement, &stabi.STSettlement2MetaData, "settlementAt", []any{id, new(big.Int).SetUint64(epoch)}, block, blockHash)
	if err != nil {
		return [32]byte{}, fmt.Errorf("no_id %d epoch %d settlementAt: %w", noID, epoch, err)
	}
	if values[0].(common.Address) != v2.Vault || values[1].([32]byte) != pool.Hotkey || values[2].(uint8) != settlementV2IdentityVersion {
		return [32]byte{}, fmt.Errorf("no_id %d epoch %d settlementAt differs from vault2's immutable pool", noID, epoch)
	}
	return pool.Hotkey, nil
}

// SettlementV2PoolHotkeyContext is the shared resolver for readers outside
// the release runtime: noID's pool weight destination for epoch, given
// operatorAt's pool hotkey for that epoch, read at the finalized head as live
// steering reads its snapshot.
func (self *ChainClient) SettlementV2PoolHotkeyContext(ctx context.Context, v2 SettlementV2Addresses, noID, epoch uint64, operatorHotkey [32]byte) ([32]byte, error) {
	head, headHash, err := self.FinalizedBlockContext(ctx)
	if err != nil {
		return [32]byte{}, err
	}
	return self.releasePoolHotkeyContext(ctx, v2, head, headHash, head, headHash, noID, epoch, operatorHotkey)
}
