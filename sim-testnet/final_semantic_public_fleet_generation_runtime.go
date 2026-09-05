package main

// final_semantic_public_fleet_generation_runtime.go supplies the public RPC
// half of per-write predecessor-runtime replay. Unlike the terminal census,
// it reads only the proxy, its historical dispatch target, and the optional
// current batcher at the exact mutation boundary.

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// Reads the historical ERC1967 slot and executable code streams for one
// ordinary-fleet mutation. Every RPC selector includes the sealed block hash
// so a provider cannot answer an old write query using present-day code.
func (self *PublicFinalSemanticChainReader) FleetGenerationRuntime(ctx context.Context, write FinalFleetGenerationWriteEvidence) (FinalFleetGenerationRuntimeState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil {
		return FinalFleetGenerationRuntimeState{}, nil, errors.New("public ordinary fleet generation runtime reader is unavailable")
	}
	if err := verifyFinalFleetGenerationRuntimeRequest(write); err != nil {
		return FinalFleetGenerationRuntimeState{}, nil, err
	}
	if err := requireFinalHex32("ERC1967 implementation slot", self.evidence.Deployment.ERC1967ImplementationSlot); err != nil {
		return FinalFleetGenerationRuntimeState{}, nil, err
	}
	slotRaw, slotExchange, err := self.evmRaw(ctx, write.EVMHead, "eth_getStorageAt", write.CoordinatorProxy, self.evidence.Deployment.ERC1967ImplementationSlot, finalEVMBlockSelector{BlockHash: write.EVMHead.Hash, RequireCanonical: true})
	if err != nil {
		return FinalFleetGenerationRuntimeState{}, nil, err
	}
	slot, err := finalDecodeRPCString("eth_getStorageAt", slotRaw)
	if err != nil {
		return FinalFleetGenerationRuntimeState{}, nil, err
	}
	slotBytes, err := hexutil.Decode(slot)
	if err != nil || len(slotBytes) != common.HashLength {
		return FinalFleetGenerationRuntimeState{}, nil, stateMismatchError(err, "decode ordinary fleet generation ERC1967 implementation slot")
	}
	implementation := common.BytesToAddress(slotBytes[common.HashLength-common.AddressLength:])
	if implementation == (common.Address{}) || !strings.EqualFold(implementation.Hex(), write.CoordinatorImplementation) {
		return FinalFleetGenerationRuntimeState{}, nil, errors.New("ordinary fleet generation ERC1967 slot has another implementation")
	}
	proxyCode, proxyExchanges, err := self.evmCode(ctx, write.EVMHead, write.CoordinatorProxy)
	if err != nil {
		return FinalFleetGenerationRuntimeState{}, nil, err
	}
	implementationCode, implementationExchanges, err := self.evmCode(ctx, write.EVMHead, write.CoordinatorImplementation)
	if err != nil {
		return FinalFleetGenerationRuntimeState{}, nil, err
	}
	exchanges := make([]FinalRPCExchange, 0, 4)
	exchanges = append(exchanges, slotExchange)
	exchanges = append(exchanges, proxyExchanges...)
	exchanges = append(exchanges, implementationExchanges...)
	result := FinalFleetGenerationRuntimeState{
		CoordinatorProxy: strings.ToLower(common.HexToAddress(write.CoordinatorProxy).Hex()), CoordinatorImplementation: strings.ToLower(implementation.Hex()),
		CoordinatorImplementationSlot: strings.ToLower(slot), CoordinatorProxyRuntimeHash: strings.ToLower(crypto.Keccak256Hash(proxyCode).Hex()),
		CoordinatorRuntimeHash: strings.ToLower(crypto.Keccak256Hash(implementationCode).Hex()), Block: write.EVMHead,
	}
	if write.BatcherRuntimeHash == "" {
		return result, exchanges, nil
	}
	if !common.IsHexAddress(write.BatcherAddress) || common.HexToAddress(write.BatcherAddress) == (common.Address{}) {
		return FinalFleetGenerationRuntimeState{}, nil, errors.New("ordinary fleet generation batcher address is invalid")
	}
	batcherCode, batcherExchanges, err := self.evmCode(ctx, write.EVMHead, write.BatcherAddress)
	if err != nil {
		return FinalFleetGenerationRuntimeState{}, nil, err
	}
	exchanges = append(exchanges, batcherExchanges...)
	result.BatcherAddress, result.BatcherRuntimeHash = strings.ToLower(common.HexToAddress(write.BatcherAddress).Hex()), strings.ToLower(crypto.Keccak256Hash(batcherCode).Hex())
	if !bytes.Equal(slotBytes[common.HashLength-common.AddressLength:], implementation.Bytes()) {
		return FinalFleetGenerationRuntimeState{}, nil, errors.New("ordinary fleet generation implementation slot has noncanonical address bytes")
	}
	return result, exchanges, nil
}

var _ finalFleetGenerationRuntimeReader = (*PublicFinalSemanticChainReader)(nil)
