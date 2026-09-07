package main

// adversary_live_contract.go contains bounded read-only EVM identity probes
// for adversarial evidence. It never broadcasts a transaction.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// Reads the deployed coordinator implementation at an explicit finalized EVM
// block and compares its complete bytecode hash with the persisted deployment
// manifest. A missing deployment is retryable during setup; a deployed but
// mismatched implementation is a hard adversarial failure.
func adversaryLiveCoordinatorImplementationCodeHash(ctx context.Context, cfg *ResolvedConfig, stateDir string, client *adversaryHTTP, sequence uint64) (bool, uint64, error) {
	if cfg == nil || cfg.Config == nil || client == nil || stateDir == "" {
		return false, 0, errors.New("live coordinator implementation probe is incomplete")
	}
	deployment, err := loadContractDeployment(stateDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, 0, nil // setup has not persisted the deployment yet.
		}
		return false, 0, fmt.Errorf("load deployment identity: %w", err)
	}
	if deployment.CoordinatorImplementation == (common.Address{}) {
		return false, 0, errors.New("deployed coordinator implementation is zero")
	}
	hashes, err := normalizedDeploymentRuntimeHashes(*deployment)
	if err != nil {
		return false, 0, fmt.Errorf("deployment runtime hashes: %w", err)
	}
	want := hashes[deployment.CoordinatorImplementation]
	if want == "" {
		return false, 0, errors.New("deployment omits coordinator implementation runtime hash")
	}
	rpc := &rpcAdversary{http: client}
	blockResponse, err := rpc.call(ctx, cfg.OperationalEVM, "eth_getBlockByNumber", []any{"finalized", false}, sequence*2+1)
	if err != nil {
		return false, 1, err
	}
	_, block, err := decodeRPCBlock(blockResponse)
	if err != nil {
		return false, 1, err
	}
	tag := fmt.Sprintf("0x%x", block)
	codeResponse, err := rpc.call(ctx, cfg.OperationalEVM, "eth_getCode", []any{deployment.CoordinatorImplementation.Hex(), tag}, sequence*2+2)
	if err != nil {
		return false, 2, err
	}
	code, err := decodeRPCHexBytes(codeResponse)
	if err != nil {
		return false, 2, err
	}
	if len(code) == 0 || !strings.EqualFold(ethcrypto.Keccak256Hash(code).Hex(), want) {
		return false, 2, fmt.Errorf("finalized coordinator implementation code hash=%s, want %s", ethcrypto.Keccak256Hash(code).Hex(), want)
	}
	return true, 2, nil
}
