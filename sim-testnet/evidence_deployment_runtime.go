// Deployment readback uses a single canonical EVM block hash for code, all
// immutable getters and the coordinator link, including crash recovery.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Returns no accepted state on cancellation, RPC failure, malformed tuples or
// foreign code. A zero link is allowed only during the pre-anchor observation.
func readValidatorEvidenceDeploymentAtHead(ctx context.Context, client *ethclient.Client, payloads *validatorEvidenceDeploymentPayloads, head ChainHead, requireAnchor bool) (common.Address, error) {
	if ctx == nil || client == nil || payloads == nil || head.Number == 0 {
		return common.Address{}, errors.New("validator evidence deployment reader is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return common.Address{}, err
	}
	blockHash, err := decodeHex32("validator evidence deployment block hash", head.Hash)
	if err != nil || blockHash == ([32]byte{}) {
		return common.Address{}, errors.New("validator evidence deployment block hash is invalid")
	}
	selector := finalEVMBlockSelector{BlockHash: head.Hash, RequireCanonical: true}
	manifest := payloads.Manifest
	var code hexutil.Bytes
	if err := client.Client().CallContext(ctx, &code, "eth_getCode", manifest.Address, selector); err != nil {
		return common.Address{}, err
	}
	if err := ctx.Err(); err != nil {
		return common.Address{}, err
	}
	if len(code) == 0 || len(code) > 24*1024 || !bytes.Equal(code, payloads.Runtime) || crypto.Keccak256Hash(code) != manifest.RuntimeCodeHash {
		return common.Address{}, errors.New("validator evidence deployment runtime does not match approval")
	}
	getters := validatorEvidenceDeploymentGetters(manifest)
	coordinator := stabi.NewSTCoordinator()
	getters = append(getters,
		struct {
			address  common.Address
			data     []byte
			expected []byte
		}{address: manifest.Coordinator, data: coordinator.PackSettlementVault(), expected: abiWordAddress(manifest.SettlementVault)},
		struct {
			address  common.Address
			data     []byte
			expected []byte
		}{address: manifest.Coordinator, data: coordinator.PackNetuid(), expected: abiWordUint(uint64(manifest.Netuid))},
	)
	values := make([]hexutil.Bytes, len(getters)+1)
	batch := make([]rpc.BatchElem, len(values))
	for index, getter := range getters {
		batch[index] = rpc.BatchElem{Method: "eth_call", Args: []any{map[string]any{"to": getter.address, "data": hexutil.Bytes(getter.data)}, selector}, Result: &values[index]}
	}
	batch[len(getters)] = rpc.BatchElem{Method: "eth_call", Args: []any{map[string]any{"to": manifest.Coordinator, "data": hexutil.Bytes(coordinator.PackValidatorEvidence())}, selector}, Result: &values[len(getters)]}
	if err := client.Client().BatchCallContext(ctx, batch); err != nil {
		return common.Address{}, err
	}
	if err := ctx.Err(); err != nil {
		return common.Address{}, err
	}
	for index, call := range batch {
		if call.Error != nil {
			return common.Address{}, fmt.Errorf("validator evidence deployment getter %d: %w", index, call.Error)
		}
		if index < len(getters) && !bytes.Equal(values[index], getters[index].expected) {
			return common.Address{}, fmt.Errorf("validator evidence deployment getter %d differs from approval", index)
		}
	}
	rawAnchor := values[len(getters)]
	if len(rawAnchor) != 32 || !bytes.Equal(rawAnchor[:12], make([]byte, 12)) {
		return common.Address{}, errors.New("validator evidence coordinator link is not a canonical address")
	}
	anchor := common.BytesToAddress(rawAnchor[12:])
	if anchor != manifest.Address && (requireAnchor || anchor != (common.Address{})) {
		return common.Address{}, errors.New("validator evidence coordinator link differs from approval")
	}
	if err := ctx.Err(); err != nil {
		return common.Address{}, err
	}
	return anchor, nil
}

// The owner can link only the exactly approved immutable journal; an already
// linked foreign address is a hard conflict, not a reason to redeploy/retry.
func (self *Executor) anchorValidatorEvidence(ctx context.Context, action Action) error {
	if self != nil && self.plan != nil && self.plan.ValidatorEvidenceCarry != nil {
		return self.verifyValidatorEvidenceCarryAction(ctx, action)
	}
	if err := self.ensurePayloads(ctx); err != nil {
		return err
	}
	if self.plan == nil || self.owner == nil || self.owner.client == nil || self.owner.key == nil || self.payloads.ValidatorEvidence == nil {
		return errors.New("validator evidence owner deployment context is unavailable")
	}
	payloads := self.payloads.ValidatorEvidence
	if err := validateValidatorEvidenceDeployment(self.plan.ValidatorEvidence, payloads); err != nil {
		return err
	}
	owner := crypto.PubkeyToAddress(self.owner.key.PublicKey)
	if !common.IsHexAddress(self.plan.Roles.Owner) || owner != common.HexToAddress(self.plan.Roles.Owner) {
		return errors.New("validator evidence anchor signer is not the approved owner")
	}
	expected, err := payloads.actionParameters(validatorEvidenceAnchorActionID, owner)
	if err != nil {
		return err
	}
	if action.ID != validatorEvidenceAnchorActionID || action.Target != payloads.Manifest.Coordinator.Hex() {
		return errors.New("validator evidence anchor target is not approved")
	}
	for key, value := range expected {
		if action.Parameters[key] != value {
			return fmt.Errorf("validator evidence anchor does not bind %s", key)
		}
	}
	head, err := finalizedEVMHead(ctx, self.owner.client)
	if err != nil {
		return err
	}
	anchor, err := readValidatorEvidenceDeploymentAtHead(ctx, self.owner.client, payloads, head, false)
	if err != nil || anchor == payloads.Manifest.Address {
		return err
	}
	if _, err := self.owner.Send(ctx, self.plan.PlanHash, action, &payloads.Manifest.Coordinator, new(big.Int), payloads.Anchor); err != nil {
		return err
	}
	head, err = finalizedEVMHead(ctx, self.owner.client)
	if err != nil {
		return err
	}
	_, err = readValidatorEvidenceDeploymentAtHead(ctx, self.owner.client, payloads, head, true)
	return err
}

// Both CREATE and anchor postconditions persist the same manifest identity;
// the executor's existing journal and independent-reader replay bind receipts.
func (self *Executor) verifyValidatorEvidenceDeploymentPostState(ctx context.Context, action Action, head ChainHead, state map[string]any) (map[string]any, error) {
	if err := self.ensurePayloads(ctx); err != nil {
		return nil, err
	}
	if self.deployer == nil || self.plan == nil || self.payloads.ValidatorEvidence == nil {
		return nil, errors.New("validator evidence deployment postcondition is unavailable")
	}
	payloads := self.payloads.ValidatorEvidence
	if err := validateValidatorEvidenceDeployment(self.plan.ValidatorEvidence, payloads); err != nil {
		return nil, err
	}
	anchor, err := readValidatorEvidenceDeploymentAtHead(ctx, self.deployer.client, payloads, head, action.ID == validatorEvidenceAnchorActionID)
	if err != nil {
		return nil, err
	}
	hash, err := canonicalHashHex(payloads.Manifest)
	if err != nil {
		return nil, err
	}
	state["address"] = payloads.Manifest.Address.Hex()
	state["runtime_hash"] = payloads.Manifest.RuntimeCodeHash.Hex()
	state["coordinator"] = payloads.Manifest.Coordinator.Hex()
	state["coordinator_validator_evidence"] = anchor.Hex()
	state[validatorEvidenceManifestParameter] = hash
	return state, nil
}
