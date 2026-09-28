// Evidence CREATE retains the approved immutable domain and seven predecessor
// journals. Creation does not anchor the journal or authenticate published proof.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/stabi"
)

// The six immutable values derive from the original graph and network domain.
// DeploymentIdHash uses the reviewed evidence deployment builder's SHA-256 rule.
type contractEvidenceConstructor struct {
	Coordinator      common.Address `json:"coordinator"`
	SettlementVault  common.Address `json:"settlement_vault"`
	ChainId          uint64         `json:"chain_id"`
	Netuid           uint16         `json:"netuid"`
	GenesisHash      common.Hash    `json:"genesis_hash"`
	DeploymentIdHash common.Hash    `json:"deployment_id_hash"`
}

// The eighth graph reservation is the bootstrap's next zero-value CREATE after
// vault binding. Simulator-only upgrade and fleet nonces are not imported here.
func contractEvidenceDomain(plan evmPhasePlan, vault, proxy evmCreatePlan) (contractEvidenceConstructor, error) {
	var result contractEvidenceConstructor
	if len(plan.Actions) < 8 || proxy.ProxyConstructor == nil || plan.Network.EvmChainId != mainnetEvmChainId || plan.Netuid != 25 || !rootCanonicalHash(plan.Network.GenesisHash) || !planLabel(plan.DeploymentId) {
		return result, errors.New("evidence CREATE lacks the approved deployment domain")
	}
	action, prior := plan.Actions[7], plan.Actions[6]
	if prior.Nonce == ^uint64(0) || action.Sender != prior.Sender || action.Nonce != prior.Nonce+1 || action.To != nil || action.ValueWei != "0" || vault.Address != crypto.CreateAddress(plan.Actions[1].Sender, plan.Actions[1].Nonce) || proxy.Address != crypto.CreateAddress(plan.Actions[4].Sender, plan.Actions[4].Nonce) {
		return result, errors.New("evidence must be the same deployer's next zero-value CREATE after vault binding")
	}
	genesis := common.HexToHash(plan.Network.GenesisHash)
	deployment := sha256.Sum256([]byte(plan.DeploymentId))
	if genesis == (common.Hash{}) || deployment == ([32]byte{}) {
		return result, errors.New("evidence CREATE domain hash is zero")
	}
	return contractEvidenceConstructor{Coordinator: proxy.Address, SettlementVault: vault.Address, ChainId: plan.Network.EvmChainId, Netuid: plan.Netuid, GenesisHash: genesis, DeploymentIdHash: deployment}, nil
}

// Constructor bytes repack through both reviewed artifact ABI and generated
// binding. Six immutable references and exact getters retain the same domain.
func contractEvidencePayload(artifact contractReleaseArtifact, plan evmPhasePlan, vault, proxy evmCreatePlan) (contractEvidenceConstructor, []byte, []byte, []contractGetter, []contractStorageWord, error) {
	domain, err := contractEvidenceDomain(plan, vault, proxy)
	if err != nil || artifact.Name != "ValidatorEvidence" {
		return domain, nil, nil, nil, nil, errors.Join(errors.New("evidence release or constructor domain differs"), err)
	}
	creation, err := contractCode(artifact.Creation, 48*1024)
	if err != nil {
		return domain, nil, nil, nil, nil, err
	}
	parsed, err := abi.JSON(strings.NewReader(artifact.Abi))
	if err != nil {
		return domain, nil, nil, nil, nil, err
	}
	contract := stabi.NewSTValidatorEvidence()
	arguments, err := parsed.Pack("", domain.Coordinator, [32]byte(domain.GenesisHash), [32]byte(domain.DeploymentIdHash))
	if err != nil || len(parsed.Constructor.Inputs) != 3 || len(arguments) != 96 || !bytes.Equal(arguments, contract.PackConstructor(domain.Coordinator, domain.GenesisHash, domain.DeploymentIdHash)) {
		return domain, nil, nil, nil, nil, errors.Join(errors.New("evidence artifact constructor differs from generated binding"), err)
	}
	coordinator, settlementVault := common.BytesToHash(domain.Coordinator[:]), common.BytesToHash(domain.SettlementVault[:])
	chain, subnet := common.BigToHash(new(big.Int).SetUint64(domain.ChainId)), common.BigToHash(new(big.Int).SetUint64(uint64(domain.Netuid)))
	runtime, err := artifact.withImmutables(map[string][]byte{"coordinator": coordinator[:], "settlementVault": settlementVault[:], "chainId": chain[:], "netuid": subnet[:], "genesisHash": domain.GenesisHash[:], "deploymentIdHash": domain.DeploymentIdHash[:]})
	if err != nil {
		return domain, nil, nil, nil, nil, err
	}
	getters := []contractGetter{}
	for _, getter := range []struct {
		data []byte
		word common.Hash
	}{{data: contract.PackCoordinator(), word: coordinator}, {data: contract.PackSettlementVault(), word: settlementVault}, {data: contract.PackChainId(), word: chain}, {data: contract.PackNetuid(), word: subnet}, {data: contract.PackGenesisHash(), word: domain.GenesisHash}, {data: contract.PackDeploymentIdHash(), word: domain.DeploymentIdHash}} {
		getters = append(getters, contractGetter{Data: "0x" + hex.EncodeToString(getter.data), Expected: getter.word.Hex()})
	}
	// Slots zero and one are mapping roots, not a census of their hashed entries.
	// The reviewed constructor performs only immutable assignments and static reads.
	storage := []contractStorageWord{{Slot: (common.Hash{}).Hex(), Expected: (common.Hash{}).Hex()}, {Slot: common.HexToHash("0x01").Hex(), Expected: (common.Hash{}).Hex()}}
	return domain, append(creation, arguments...), runtime, getters, storage, nil
}

// The constructor's external reads are static and emit no events. Explicit
// CREATE identity and an empty log list are required for either receipt status.
func evmEvidenceCreateReceipt(fields map[string]json.RawMessage, plan evmCreatePlan) error {
	var from string
	var logs []json.RawMessage
	raw, exists := fields["logs"]
	if json.Unmarshal(fields["from"], &from) != nil || !common.IsHexAddress(from) || common.HexToAddress(from) != plan.Config.Plan.Actions[7].Sender || !bytes.Equal(fields["to"], []byte("null")) {
		return errors.New("evidence receipt sender or CREATE target differs")
	}
	if !exists || bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &logs) != nil || len(logs) != 0 {
		return errors.New("evidence constructor receipt requires explicit empty logs")
	}
	return nil
}

// Reobserve the bound vault, bound reserve and still-unanchored initialized
// proxy at one selector. Original proxy policy height remains historical.
func (self *evmOwnedChain) authenticateEvidenceDependencies(ctx context.Context, plan evmCreatePlan, block any) error {
	if plan.VaultLink == nil || len(plan.Prerequisites) != 7 {
		return errors.New("evidence CREATE lacks completed vault-binding custody")
	}
	vault := *plan.VaultLink
	var code string
	if err := self.read(ctx, "eth_getCode", []any{vault.Address.Hex(), block}, &code); err != nil {
		return err
	}
	if code != "0x"+hex.EncodeToString(vault.Runtime) {
		return errors.New("evidence bound vault runtime differs")
	}
	for _, getter := range vault.Getters {
		var output string
		if err := self.read(ctx, "eth_call", []any{map[string]any{"to": vault.Address.Hex(), "data": getter.Data}, block}, &output); err != nil {
			return err
		}
		if output != getter.Expected {
			return errors.New("evidence bound vault getter differs")
		}
	}
	for _, word := range vault.Storage {
		var output string
		if err := self.read(ctx, "eth_getStorageAt", []any{vault.Address.Hex(), word.Slot, block}, &output); err != nil {
			return err
		}
		if output != word.Expected {
			return errors.New("evidence bound vault storage differs")
		}
	}
	vault.Prerequisites = plan.Prerequisites[:6]
	return self.authenticateVaultCoordinator(ctx, vault, block)
}

// Current dependencies are separate from all seven historical inclusions.
// Both canonical and pending state must retain the original bound identities.
func (self *evmOwnedChain) admitEvidenceCreate(ctx context.Context, plan evmCreatePlan, block map[string]any) error {
	var code string
	if err := self.read(ctx, "eth_getCode", []any{plan.Address.Hex(), block}, &code); err != nil {
		return err
	}
	if code != "0x" {
		return errors.New("evidence CREATE address already has canonical code")
	}
	for _, selector := range []any{block, "pending"} {
		if err := self.authenticateEvidenceDependencies(ctx, plan, selector); err != nil {
			return err
		}
	}
	return nil
}
