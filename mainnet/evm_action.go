// The CREATE owner imports public signed bytes and never owns a private key.
// One caller may advance at a time; canceled waiters cannot mutate custody.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// A complete canonical observation is retained independently of CLI output.
type evmCreateReceipt struct {
	TransactionHash   string `json:"transaction_hash"`
	BlockHash         string `json:"evm_block_hash"`
	BlockNumber       uint64 `json:"evm_block_number"`
	NativeHash        string `json:"native_block_hash"`
	NativeNumber      uint64 `json:"native_block_number"`
	Status            uint64 `json:"status"`
	GasUsed           uint64 `json:"gas_used"`
	EffectiveGasPrice string `json:"effective_gas_price_wei"`
	ContractAddress   string `json:"contract_address"`
	RuntimeHash       string `json:"runtime_hash,omitempty"`
	GetterHash        string `json:"getter_hash,omitempty"`
}

// Read results never grant another nonce or a replacement signature.
type evmActionObservation struct {
	Status     string
	SendReady  bool
	ScanNumber uint64
	ScanHash   string
	Receipt    *evmCreateReceipt
}

// The same owned adapter performs reconciliation, admission and exactly one
// HTTP write. Deterministic tests also exercise its real local HTTP route.
type evmActionChain interface {
	reconcile(context.Context, evmCreatePlan, evmActionRecord) (evmActionObservation, error)
	submit(context.Context, evmCreatePlan, evmActionRecord) error
}

// The result explicitly separates one contract from the full installation.
type evmCreateResult struct {
	PlanHash             string            `json:"plan_hash"`
	Status               string            `json:"status"`
	Address              string            `json:"reserve_address"`
	VaultAddress         string            `json:"vault_address,omitempty"`
	ExecutableAction     string            `json:"executable_action,omitempty"`
	SigningDigest        string            `json:"signing_digest"`
	UnsignedTransaction  string            `json:"unsigned_transaction"`
	TransactionHash      string            `json:"transaction_hash,omitempty"`
	Attempts             uint8             `json:"attempts"`
	Receipt              *evmCreateReceipt `json:"receipt,omitempty"`
	ReceiptObservation   string            `json:"receipt_observation,omitempty"`
	InstallationComplete bool              `json:"installation_complete"`
	ActivationReady      bool              `json:"activation_ready"`
	RemainingActions     []string          `json:"remaining_actions"`
}

// All mutable projections are copied on construction. Storage failure poisons
// this instance; a new owner must reload actual durable state before proceeding.
type evmCreateOwner struct {
	plan         evmCreatePlan
	store        evmActionStorage
	chain        evmActionChain
	reserveStore evmActionStorage
	gate         chan struct{}
	failed       error
}

// JSON copying preserves the exact public approval while severing caller slices.
func copyEvmPhaseConfig(config evmPhaseConfig) evmPhaseConfig {
	raw, _ := json.Marshal(config)
	var copied evmPhaseConfig
	_ = json.Unmarshal(raw, &copied)
	return copied
}

// No network operation or signature request happens during construction.
func newEvmCreateOwner(plan evmCreatePlan, store evmActionStorage, chain evmActionChain) (*evmCreateOwner, error) {
	return newEvmSelectedCreateOwner(plan, store, nil, chain)
}

// Vault custody borrows an already locked reserve store for the owner's entire
// lifetime. No signature or attempt may bypass that completed prerequisite.
func newEvmVaultCreateOwner(plan evmCreatePlan, store, reserveStore evmActionStorage, chain evmActionChain) (*evmCreateOwner, error) {
	if plan.ActionIndex != 1 || reserveStore == nil {
		return nil, errors.New("vault owner lacks reserve custody")
	}
	return newEvmSelectedCreateOwner(plan, store, reserveStore, chain)
}

// Copy both projections; caller changes cannot alter an admitted prerequisite.
func newEvmSelectedCreateOwner(plan evmCreatePlan, store, reserveStore evmActionStorage, chain evmActionChain) (*evmCreateOwner, error) {
	if store == nil {
		return nil, errors.New("EVM custody storage is unavailable")
	}
	if err := plan.Config.validate(); err != nil {
		return nil, err
	}
	if err := plan.validateSelection(); err != nil {
		return nil, err
	}
	if (plan.ActionIndex == 1) != (reserveStore != nil) {
		return nil, errors.New("EVM owner prerequisite differs from action selection")
	}
	plan.Config = copyEvmPhaseConfig(plan.Config)
	plan.Prerequisite = nil
	plan.Runtime = append([]byte(nil), plan.Runtime...)
	plan.Getters = append([]contractGetter(nil), plan.Getters...)
	if plan.Reserve != nil {
		reserve := *plan.Reserve
		reserve.Config = copyEvmPhaseConfig(reserve.Config)
		reserve.Runtime = append([]byte(nil), reserve.Runtime...)
		reserve.Getters = append([]contractGetter(nil), reserve.Getters...)
		plan.Reserve = &reserve
	}
	if plan.VaultConstructor != nil {
		constructor := *plan.VaultConstructor
		plan.VaultConstructor = &constructor
	}
	record, err := store.load()
	if err != nil {
		return nil, err
	}
	if err := record.validateForAction(plan.Config, plan.ActionIndex); err != nil {
		return nil, err
	}
	owner := &evmCreateOwner{plan: plan, store: store, reserveStore: reserveStore, chain: chain, gate: make(chan struct{}, 1)}
	if _, err := owner.prerequisite(context.Background(), record, false); err != nil {
		return nil, err
	}
	return owner, nil
}

// Status one alone is insufficient to open the next signed-intent reservation.
// Offline evidence must include the exact runtime and constructor getter hashes.
func validateEvmReservePrerequisite(plan evmCreatePlan, record evmActionRecord) error {
	if err := record.validate(plan.Config); err != nil {
		return err
	}
	r := record.Receipt
	if r == nil || r.Status != 1 || r.ContractAddress != plan.Address.Hex() || r.RuntimeHash != crypto.Keccak256Hash(plan.Runtime).Hex() || r.GetterHash != rootObjectHash(plan.Getters) || r.NativeNumber != record.ScanNumber || r.NativeHash != record.ScanHash {
		return errors.New("vault requires the exact retained successful reserve postconditions")
	}
	action := plan.Config.Plan.Actions[0]
	fee, err := evmWei(r.EffectiveGasPrice)
	maximum, maximumErr := evmWei(action.FeeCapWei)
	if err != nil || maximumErr != nil || fee.Cmp(maximum) > 0 || r.BlockNumber == 0 || r.GasUsed == 0 || r.GasUsed > action.Gas {
		return errors.New("reserve prerequisite financial receipt is incomplete")
	}
	return nil
}

// Re-audit the original reserve at its historical inclusion before any online
// vault progress. The reserve file itself remains unchanged under its held lock.
func (self *evmCreateOwner) prerequisite(ctx context.Context, record evmActionRecord, online bool) (evmActionRecord, error) {
	if self.plan.ActionIndex == 0 {
		return evmActionRecord{}, nil
	}
	reserve, err := self.reserveStore.load()
	if err != nil {
		return reserve, err
	}
	plan := self.plan
	plan.Prerequisite = &reserve
	if err := validateEvmCreatePrerequisite(plan, record); err != nil {
		return reserve, err
	}
	if online {
		if self.chain == nil {
			return reserve, errors.New("vault prerequisite audit requires the owned adapter")
		}
		observation, err := self.chain.reconcile(ctx, *self.plan.Reserve, reserve)
		if err != nil {
			return reserve, err
		}
		if observation.Receipt == nil || *observation.Receipt != *reserve.Receipt || observation.ScanNumber != reserve.ScanNumber || observation.ScanHash != reserve.ScanHash || observation.SendReady {
			return reserve, errors.New("vault prerequisite canonical reserve receipt changed")
		}
	}
	return reserve, ctx.Err()
}

// The transport receives the exact prerequisite loaded under its held lock,
// not a checkpoint learned from the RPC response or from an unsigned plan.
func validateEvmCreatePrerequisite(plan evmCreatePlan, record evmActionRecord) error {
	if plan.ActionIndex == 0 {
		return nil
	}
	if plan.Prerequisite == nil {
		return errors.New("vault transport lacks retained reserve custody")
	}
	reserve := *plan.Prerequisite
	if err := validateEvmReservePrerequisite(*plan.Reserve, reserve); err != nil {
		return err
	}
	if rootObjectHash(reserve) != record.PredecessorHash || uint16(record.Attempts)+uint16(reserve.Attempts) > uint16(plan.Config.Plan.MaximumAttempts) {
		return errors.New("vault prerequisite custody or original graph attempt allowance changed")
	}
	return nil
}

// A failed publication must never be followed by a network effect in this owner.
func (self *evmCreateOwner) retain(record *evmActionRecord) error {
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(*record)
	if err := self.store.save(*record); err != nil {
		self.failed = err
		return err
	}
	return nil
}

// Offline mode only retains public bytes. Online mode reconciles first; submit
// additionally requires exact current admission and an unused finite attempt.
func (self *evmCreateOwner) advance(ctx context.Context, signed []byte, online, submit bool) (evmCreateResult, error) {
	var result evmCreateResult
	if ctx == nil || submit && !online {
		return result, errors.New("EVM operation context or online mode is invalid")
	}
	select {
	case self.gate <- struct{}{}:
	case <-ctx.Done():
		return result, ctx.Err()
	}
	defer func() { <-self.gate }()
	if err := errors.Join(ctx.Err(), self.failed); err != nil {
		return result, err
	}
	record, err := self.store.load()
	if err != nil {
		return result, err
	}
	if err := record.validateForAction(self.plan.Config, self.plan.ActionIndex); err != nil {
		return result, err
	}
	prior, err := self.prerequisite(ctx, record, online)
	if err != nil {
		return result, err
	}
	plan := self.plan
	if plan.ActionIndex == 1 {
		plan.Prerequisite = &prior
	}
	p := self.plan.Config.Plan
	action := p.Actions[self.plan.ActionIndex]
	if len(signed) != 0 {
		tx, err := action.signed(signed)
		if err != nil {
			return result, err
		}
		encoded := "0x" + hex.EncodeToString(signed)
		if record.Signed != "" && record.Signed != encoded {
			return result, errors.New("EVM custody already owns different original signed bytes")
		}
		if record.Signed == "" {
			record.Signed, record.TransactionHash = encoded, tx.Hash().Hex()
			if err := self.retain(&record); err != nil {
				return result, err
			}
		}
	}
	status := "signature-awaiting-import"
	if record.Signed != "" {
		status = "signed-custody-complete"
	}
	if record.Receipt != nil {
		status = self.plan.completedStatus()
		if record.Receipt.Status == 0 {
			status = "create-reverted-nonce-consumed"
		}
	}
	if online {
		if self.chain == nil || record.Signed == "" {
			return result, errors.New("EVM online reconciliation requires retained signed bytes and an owned adapter")
		}
		observation, err := self.chain.reconcile(ctx, plan, record)
		if err != nil {
			return result, err
		}
		if observation.ScanNumber < record.ScanNumber || observation.ScanNumber == record.ScanNumber && observation.ScanHash != record.ScanHash {
			return result, errors.New("EVM reconciliation regressed retained ancestry")
		}
		if record.Receipt != nil && (observation.Receipt == nil || *record.Receipt != *observation.Receipt) {
			return result, errors.New("EVM canonical receipt changed after retention")
		}
		record.ScanNumber, record.ScanHash = observation.ScanNumber, observation.ScanHash
		record.Receipt = observation.Receipt
		if err := self.retain(&record); err != nil {
			return result, err
		}
		status = observation.Status
		if submit && observation.SendReady {
			if record.Receipt != nil || uint16(record.Attempts)+uint16(prior.Attempts) >= uint16(p.MaximumAttempts) {
				status = "attempt-allowance-exhausted"
			} else {
				if err := ctx.Err(); err != nil {
					return result, err
				}
				record.Attempts++
				if err := self.retain(&record); err != nil {
					return result, err
				}
				// The attempt is uncertain before entering the transport, even if
				// cancellation or a malformed reply prevents acknowledgement.
				if err := self.chain.submit(ctx, plan, record); err != nil {
					return self.result(record, "submission-uncertain"), err
				}
				status = "submitted-awaiting-canonical-receipt"
			}
		}
	}
	result = self.result(record, status)
	if online && result.Receipt != nil {
		result.ReceiptObservation = "revalidated-online"
	}
	return result, ctx.Err()
}

// Public signing material contains the exact envelope; it cannot sign itself.
func (self *evmCreateOwner) result(record evmActionRecord, status string) evmCreateResult {
	tx, _ := self.plan.Config.Plan.Actions[self.plan.ActionIndex].unsigned()
	raw, _ := tx.MarshalBinary()
	signer := types.LatestSignerForChainID(big.NewInt(mainnetEvmChainId))
	result := evmCreateResult{PlanHash: self.plan.Config.Plan.hash(), Status: status, Address: self.plan.Address.Hex(), SigningDigest: signer.Hash(tx).Hex(), UnsignedTransaction: "0x" + hex.EncodeToString(raw), TransactionHash: record.TransactionHash, Attempts: record.Attempts, Receipt: record.Receipt, RemainingActions: []string{"vault-create", "coordinator-create", "escrow-register", "proxy-create", "reserve-link", "vault-link", "evidence-create", "evidence-anchor"}}
	if self.plan.ActionIndex == 1 {
		result.Address = self.plan.Reserve.Address.Hex()
		result.VaultAddress = self.plan.Address.Hex()
		result.ExecutableAction = "vault-create"
		result.RemainingActions = result.RemainingActions[1:]
	}
	if record.Receipt != nil {
		result.ReceiptObservation = "retained"
	}
	return result
}
