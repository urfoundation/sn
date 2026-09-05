// This unit reads per-operator EVM state that receipts alone cannot establish.
// Every call uses the public verifier's block-hash selector, preventing a
// provider from substituting current state for a historical decision.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/stabi"
)

// Reads one coordinator conviction ledger entry at an immutable validator
// snapshot.
func (self *PublicFinalSemanticChainReader) CoordinatorConviction(ctx context.Context, noID uint64, head ChainHead) (FinalCoordinatorConvictionChainState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil || noID == 0 {
		return FinalCoordinatorConvictionChainState{}, nil, errors.New("historical coordinator conviction query is incomplete")
	}
	coordinator := stabi.NewSTCoordinator()
	out, exchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.CoordinatorProxy, coordinator.PackCumulativeConviction(new(big.Int).SetUint64(noID)))
	if err != nil {
		return FinalCoordinatorConvictionChainState{}, nil, err
	}
	amount, err := coordinator.UnpackCumulativeConviction(out)
	if err != nil || amount == nil || amount.Sign() < 0 {
		return FinalCoordinatorConvictionChainState{}, nil, stateMismatchError(err, "decode historical coordinator conviction")
	}
	return FinalCoordinatorConvictionChainState{NoID: noID, ConvictionRao: amount.String(), Block: head}, exchanges, nil
}

// Reads the settlement-epoch increment excluded from signed pre-epoch
// conviction. Epoch zero is valid, while a zero operator id is not.
func (self *PublicFinalSemanticChainReader) EpochConvictionAdded(ctx context.Context, epoch, noID uint64, head ChainHead) (FinalEpochConvictionAddedChainState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil || noID == 0 {
		return FinalEpochConvictionAddedChainState{}, nil, errors.New("historical epoch conviction increment query is incomplete")
	}
	coordinator := stabi.NewSTCoordinator()
	out, exchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.CoordinatorProxy, coordinator.PackEpochConvictionAdded(new(big.Int).SetUint64(epoch), new(big.Int).SetUint64(noID)))
	if err != nil {
		return FinalEpochConvictionAddedChainState{}, nil, err
	}
	amount, err := coordinator.UnpackEpochConvictionAdded(out)
	if err != nil || amount == nil || amount.Sign() < 0 {
		return FinalEpochConvictionAddedChainState{}, nil, stateMismatchError(err, "decode historical epoch conviction increment")
	}
	return FinalEpochConvictionAddedChainState{Epoch: epoch, NoID: noID, AmountRao: amount.String(), Block: head}, exchanges, nil
}

// Reads the reserve-side principal ledger at the exact validator snapshot used
// for the corresponding conviction lookup.
func (self *PublicFinalSemanticChainReader) ReserveOperatorPrincipal(ctx context.Context, noID uint64, head ChainHead) (FinalReserveOperatorPrincipalChainState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil || noID == 0 {
		return FinalReserveOperatorPrincipalChainState{}, nil, errors.New("historical reserve principal query is incomplete")
	}
	reserve := stabi.NewSTReserveSink()
	out, exchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.ReserveSink, reserve.PackOperatorPrincipal(new(big.Int).SetUint64(noID)))
	if err != nil {
		return FinalReserveOperatorPrincipalChainState{}, nil, err
	}
	amount, err := reserve.UnpackOperatorPrincipal(out)
	if err != nil || amount == nil || amount.Sign() < 0 {
		return FinalReserveOperatorPrincipalChainState{}, nil, stateMismatchError(err, "decode historical reserve operator principal")
	}
	return FinalReserveOperatorPrincipalChainState{NoID: noID, PrincipalRao: amount.String(), Block: head}, exchanges, nil
}

// Reads one per-operator carry ledger at an immutable boundary.
func (self *PublicFinalSemanticChainReader) VaultCarry(ctx context.Context, noID uint64, head ChainHead) (FinalVaultCarryChainState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil || noID == 0 {
		return FinalVaultCarryChainState{}, nil, errors.New("historical vault carry query is incomplete")
	}
	vault := stabi.NewSTSettlementVault()
	out, exchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.SettlementVault, vault.PackCarry(new(big.Int).SetUint64(noID)))
	if err != nil {
		return FinalVaultCarryChainState{}, nil, err
	}
	amount, err := vault.UnpackCarry(out)
	if err != nil || amount == nil || amount.Sign() < 0 {
		return FinalVaultCarryChainState{}, nil, stateMismatchError(err, "decode historical vault carry")
	}
	return FinalVaultCarryChainState{NoID: noID, CarryRao: amount.String(), Block: head}, exchanges, nil
}

// Reads the canonical payout leaf and distinct claim-once key in one pinned
// transcript group. Epoch zero remains a valid contract epoch.
func (self *PublicFinalSemanticChainReader) VaultClaim(ctx context.Context, epoch, noID uint64, coldkey string, shareBPS uint64, head ChainHead) (FinalVaultClaimChainState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil || noID == 0 || shareBPS == 0 || shareBPS > 10_000 {
		return FinalVaultClaimChainState{}, nil, errors.New("historical vault claim query is incomplete")
	}
	if err := requireFinalHex32("claim coldkey", coldkey); err != nil {
		return FinalVaultClaimChainState{}, nil, err
	}
	coldkeyHash := common.HexToHash(coldkey)
	var coldkeyBytes [32]byte
	copy(coldkeyBytes[:], coldkeyHash[:])
	vault := stabi.NewSTSettlementVault()
	leafOut, leafExchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.SettlementVault, vault.PackPayoutLeaf(coldkeyBytes, new(big.Int).SetUint64(shareBPS)))
	if err != nil {
		return FinalVaultClaimChainState{}, nil, err
	}
	claimKey, err := vault.UnpackPayoutLeaf(leafOut)
	if err != nil || claimKey == ([32]byte{}) {
		return FinalVaultClaimChainState{}, nil, stateMismatchError(err, "decode canonical payout leaf")
	}
	var noIDWord [common.HashLength]byte
	new(big.Int).SetUint64(noID).FillBytes(noIDWord[:])
	claimKeyHash := crypto.Keccak256Hash(append(noIDWord[:], coldkeyBytes[:]...))
	var claimKeyBytes [common.HashLength]byte
	copy(claimKeyBytes[:], claimKeyHash[:])
	leafOut, leafClaimedExchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.SettlementVault, vault.PackLeafClaimed(new(big.Int).SetUint64(epoch), claimKeyBytes))
	if err != nil {
		return FinalVaultClaimChainState{}, nil, err
	}
	leafClaimed, err := vault.UnpackLeafClaimed(leafOut)
	if err != nil {
		return FinalVaultClaimChainState{}, nil, stateMismatchError(err, "decode represented payout leaf")
	}
	return FinalVaultClaimChainState{
		Epoch: epoch, NoID: noID, Coldkey: strings.ToLower(coldkey), ShareBPS: shareBPS,
		PayoutLeaf: "0x" + fmt.Sprintf("%x", claimKey[:]), ClaimKey: strings.ToLower(claimKeyHash.Hex()), LeafClaimed: leafClaimed, Block: head,
	}, append(leafExchanges, leafClaimedExchanges...), nil
}

// Reads a coldkey's cumulative claim credit at one pinned boundary. A single
// payment event cannot establish it because payment can settle prior credit.
func (self *PublicFinalSemanticChainReader) VaultClaimCredit(ctx context.Context, coldkey string, head ChainHead) (FinalVaultClaimCreditChainState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil {
		return FinalVaultClaimCreditChainState{}, nil, errors.New("historical vault claim credit query is incomplete")
	}
	if err := requireFinalHex32("claim credit coldkey", coldkey); err != nil {
		return FinalVaultClaimCreditChainState{}, nil, err
	}
	coldkeyHash := common.HexToHash(coldkey)
	var coldkeyBytes [32]byte
	copy(coldkeyBytes[:], coldkeyHash[:])
	vault := stabi.NewSTSettlementVault()
	out, exchanges, err := self.evmCall(ctx, head, self.evidence.Deployment.SettlementVault, vault.PackClaimCredit(coldkeyBytes))
	if err != nil {
		return FinalVaultClaimCreditChainState{}, nil, err
	}
	credit, err := vault.UnpackClaimCredit(out)
	if err != nil || credit == nil || credit.Sign() < 0 {
		return FinalVaultClaimCreditChainState{}, nil, stateMismatchError(err, "decode historical vault claim credit")
	}
	return FinalVaultClaimCreditChainState{Coldkey: strings.ToLower(coldkey), CreditRao: credit.String(), Block: head}, exchanges, nil
}

// Reads the ERC1967 slot before selecting the implementation whose runtime is
// hashed, preventing a declared address from masking another dispatch target.
func (self *PublicFinalSemanticChainReader) CoordinatorRuntime(ctx context.Context, head ChainHead) (FinalCoordinatorRuntimeChainState, []FinalRPCExchange, error) {
	if self == nil || self.evidence == nil {
		return FinalCoordinatorRuntimeChainState{}, nil, errors.New("historical coordinator runtime query is unavailable")
	}
	deployment := self.evidence.Deployment
	if err := verifyFinalReleaseRuntimeRootsShape(deployment); err != nil {
		return FinalCoordinatorRuntimeChainState{}, nil, err
	}
	if err := requireFinalEVMAddress("coordinator proxy", deployment.CoordinatorProxy); err != nil {
		return FinalCoordinatorRuntimeChainState{}, nil, err
	}
	if err := requireFinalHex32("ERC1967 implementation slot", deployment.ERC1967ImplementationSlot); err != nil {
		return FinalCoordinatorRuntimeChainState{}, nil, err
	}
	slotRaw, slotExchange, err := self.evmRaw(ctx, head, "eth_getStorageAt", deployment.CoordinatorProxy, deployment.ERC1967ImplementationSlot, finalEVMBlockSelector{BlockHash: head.Hash, RequireCanonical: true})
	if err != nil {
		return FinalCoordinatorRuntimeChainState{}, nil, err
	}
	slot, err := finalDecodeRPCString("eth_getStorageAt", slotRaw)
	if err != nil {
		return FinalCoordinatorRuntimeChainState{}, nil, err
	}
	slotBytes, err := hexutil.Decode(slot)
	if err != nil || len(slotBytes) != common.HashLength {
		return FinalCoordinatorRuntimeChainState{}, nil, stateMismatchError(err, "decode historical ERC1967 implementation slot")
	}
	implementation := common.BytesToAddress(slotBytes[common.HashLength-common.AddressLength:])
	if implementation == (common.Address{}) {
		return FinalCoordinatorRuntimeChainState{}, nil, errors.New("historical ERC1967 implementation is zero")
	}
	runtimeRoots := append([]FinalReleaseRuntimeRoot(nil), deployment.RuntimeRoots...)
	exchanges := []FinalRPCExchange{slotExchange}
	proxyCodeHash := ""
	implementationCodeHash := ""
	for index := range runtimeRoots {
		root := &runtimeRoots[index]
		code, codeExchanges, codeErr := self.evmCode(ctx, head, root.Address)
		if codeErr != nil {
			return FinalCoordinatorRuntimeChainState{}, nil, codeErr
		}
		exchanges = append(exchanges, codeExchanges...)
		root.RuntimeCodeHash = strings.ToLower(crypto.Keccak256Hash(code).Hex())
		address := common.HexToAddress(root.Address)
		if address == common.HexToAddress(deployment.CoordinatorProxy) {
			proxyCodeHash = root.RuntimeCodeHash
		}
		if address == implementation {
			implementationCodeHash = root.RuntimeCodeHash
		}
	}
	if proxyCodeHash == "" || implementationCodeHash == "" {
		return FinalCoordinatorRuntimeChainState{}, nil, errors.New("historical runtime root census omits the proxy or active implementation")
	}
	return FinalCoordinatorRuntimeChainState{
		CoordinatorProxy: deployment.CoordinatorProxy, CoordinatorImplementation: strings.ToLower(implementation.Hex()),
		ObservedImplementationSlot: strings.ToLower(slot), ProxyCodeHash: proxyCodeHash,
		ImplementationCodeHash: implementationCodeHash, RuntimeRoots: runtimeRoots, Block: head,
	}, exchanges, nil
}
