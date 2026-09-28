// The CREATE owner imports public signed bytes and never owns a private key.
// One caller may advance at a time; canceled waiters cannot mutate custody.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	StorageHash       string `json:"storage_hash,omitempty"`
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
	CoordinatorAddress   string            `json:"coordinator_implementation_address,omitempty"`
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
	plan        evmCreatePlan
	store       evmActionStorage
	chain       evmActionChain
	priorStores []evmActionStorage
	gate        chan struct{}
	failed      error
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
	return newEvmSelectedCreateOwner(plan, store, []evmActionStorage{reserveStore}, chain)
}

// Both predecessor locks remain held while coordinator custody is open.
func newEvmCoordinatorCreateOwner(plan evmCreatePlan, store, reserveStore, vaultStore evmActionStorage, chain evmActionChain) (*evmCreateOwner, error) {
	if plan.ActionIndex != 2 || reserveStore == nil || vaultStore == nil {
		return nil, errors.New("coordinator owner lacks reserve and vault custody")
	}
	return newEvmSelectedCreateOwner(plan, store, []evmActionStorage{reserveStore, vaultStore}, chain)
}

// Runtime projections own every nested slice; invocation-specific predecessor
// records are always loaded afresh under the held locks rather than copied in.
func copyEvmCreatePlan(plan evmCreatePlan) evmCreatePlan {
	plan.Config = copyEvmPhaseConfig(plan.Config)
	plan.Prerequisites = nil
	plan.Runtime = append([]byte(nil), plan.Runtime...)
	plan.Getters = append([]contractGetter(nil), plan.Getters...)
	plan.Storage = append([]contractStorageWord(nil), plan.Storage...)
	if plan.Reserve != nil {
		reserve := copyEvmCreatePlan(*plan.Reserve)
		plan.Reserve = &reserve
	}
	if plan.Vault != nil {
		vault := copyEvmCreatePlan(*plan.Vault)
		plan.Vault = &vault
	}
	if plan.VaultConstructor != nil {
		constructor := *plan.VaultConstructor
		plan.VaultConstructor = &constructor
	}
	return plan
}

// Copy every projection; caller changes cannot alter an admitted prerequisite.
func newEvmSelectedCreateOwner(plan evmCreatePlan, store evmActionStorage, priorStores []evmActionStorage, chain evmActionChain) (*evmCreateOwner, error) {
	if store == nil {
		return nil, errors.New("EVM custody storage is unavailable")
	}
	if err := plan.Config.validate(); err != nil {
		return nil, err
	}
	if err := plan.validateSelection(); err != nil {
		return nil, err
	}
	if plan.ActionIndex != len(priorStores) {
		return nil, errors.New("EVM owner prerequisite differs from action selection")
	}
	plan = copyEvmCreatePlan(plan)
	record, err := store.load()
	if err != nil {
		return nil, err
	}
	if err := record.validateForAction(plan.Config, plan.ActionIndex); err != nil {
		return nil, err
	}
	if plan.ActionIndex == 2 && record.Receipt != nil && record.Receipt.Status == 1 {
		if err := validateEvmCreateCompletion(plan, record); err != nil {
			return nil, err
		}
	}
	owner := &evmCreateOwner{plan: plan, store: store, priorStores: append([]evmActionStorage(nil), priorStores...), chain: chain, gate: make(chan struct{}, 1)}
	if _, err := owner.prerequisite(context.Background(), record, false); err != nil {
		return nil, err
	}
	return owner, nil
}

// Status one alone is insufficient to open the next signed-intent reservation.
// Offline evidence must include the exact runtime and constructor getter hashes.
func validateEvmReservePrerequisite(plan evmCreatePlan, record evmActionRecord) error {
	return validateEvmCreateCompletion(plan, record)
}

// Every completed predecessor carries exact semantic hashes, including storage
// when the implementation constructor has a namespaced initialization guard.
func validateEvmCreateCompletion(plan evmCreatePlan, record evmActionRecord) error {
	if err := record.validateForAction(plan.Config, plan.ActionIndex); err != nil {
		return err
	}
	r := record.Receipt
	if r == nil || r.Status != 1 || r.ContractAddress != plan.Address.Hex() || r.RuntimeHash != crypto.Keccak256Hash(plan.Runtime).Hex() || r.GetterHash != rootObjectHash(plan.Getters) || r.NativeNumber != record.ScanNumber || r.NativeHash != record.ScanHash {
		name := []string{"reserve", "vault", "coordinator implementation"}[plan.ActionIndex]
		return fmt.Errorf("action requires the exact retained successful %s postconditions", name)
	}
	if len(plan.Storage) != 0 && r.StorageHash != rootObjectHash(plan.Storage) {
		return errors.New("retained constructor storage postconditions differ")
	}
	action := plan.Config.Plan.Actions[plan.ActionIndex]
	fee, err := evmWei(r.EffectiveGasPrice)
	maximum, maximumErr := evmWei(action.FeeCapWei)
	if err != nil || maximumErr != nil || fee.Cmp(maximum) > 0 || r.BlockNumber == 0 || r.GasUsed == 0 || r.GasUsed > action.Gas {
		return errors.New("completed CREATE financial receipt is incomplete")
	}
	return nil
}

// Re-audit each predecessor at its historical inclusion before online progress.
// Predecessor files remain unchanged under their held locks.
func (self *evmCreateOwner) prerequisite(ctx context.Context, record evmActionRecord, online bool) ([]evmActionRecord, error) {
	priorRecords := []evmActionRecord{}
	for _, store := range self.priorStores {
		prior, err := store.load()
		if err != nil {
			return nil, err
		}
		priorRecords = append(priorRecords, prior)
	}
	plan := self.plan
	plan.Prerequisites = priorRecords
	if err := validateEvmCreatePrerequisite(plan, record); err != nil {
		return nil, err
	}
	if online && len(priorRecords) != 0 {
		if self.chain == nil {
			return nil, errors.New("prerequisite audit requires the owned adapter")
		}
		for i, prior := range priorRecords {
			priorPlan := plan.priorPlan(i)
			priorPlan.Prerequisites = priorRecords[:i]
			observation, err := self.chain.reconcile(ctx, priorPlan, prior)
			if err != nil {
				return nil, err
			}
			if observation.Receipt == nil || *observation.Receipt != *prior.Receipt || observation.ScanNumber != prior.ScanNumber || observation.ScanHash != prior.ScanHash || observation.SendReady {
				return nil, errors.New("prerequisite canonical receipt changed")
			}
		}
	}
	return priorRecords, ctx.Err()
}

// The transport receives the exact prerequisite loaded under its held lock,
// not a checkpoint learned from the RPC response or from an unsigned plan.
func validateEvmCreatePrerequisite(plan evmCreatePlan, record evmActionRecord) error {
	if plan.ActionIndex == 0 {
		return nil
	}
	if len(plan.Prerequisites) != plan.ActionIndex {
		return errors.New("transport lacks the exact retained predecessor prefix")
	}
	attempts := uint16(record.Attempts)
	for i, prior := range plan.Prerequisites {
		if err := validateEvmCreateCompletion(plan.priorPlan(i), prior); err != nil {
			return err
		}
		if i > 0 {
			previous := plan.Prerequisites[i-1]
			if prior.PredecessorHash != rootObjectHash(previous) || prior.Receipt.NativeNumber < previous.Receipt.NativeNumber || prior.Receipt.BlockNumber < previous.Receipt.BlockNumber {
				return errors.New("retained predecessor chain or inclusion order differs")
			}
		}
		attempts += uint16(prior.Attempts)
	}
	if rootObjectHash(plan.Prerequisites[len(plan.Prerequisites)-1]) != record.PredecessorHash || attempts > uint16(plan.Config.Plan.MaximumAttempts) {
		return errors.New("prerequisite custody or original graph attempt allowance changed")
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
	if self.plan.ActionIndex == 2 && record.Receipt != nil && record.Receipt.Status == 1 {
		if err := validateEvmCreateCompletion(self.plan, record); err != nil {
			return result, err
		}
	}
	prior, err := self.prerequisite(ctx, record, online)
	if err != nil {
		return result, err
	}
	plan := self.plan
	plan.Prerequisites = prior
	priorAttempts := uint16(0)
	for _, record := range prior {
		priorAttempts += uint16(record.Attempts)
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
			if record.Receipt != nil || uint16(record.Attempts)+priorAttempts >= uint16(p.MaximumAttempts) {
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
	if self.plan.ActionIndex >= 1 {
		result.Address = self.plan.Reserve.Address.Hex()
		result.VaultAddress = self.plan.Address.Hex()
		result.ExecutableAction = "vault-create"
		result.RemainingActions = result.RemainingActions[1:]
	}
	if self.plan.ActionIndex == 2 {
		result.VaultAddress = self.plan.Vault.Address.Hex()
		result.CoordinatorAddress = self.plan.Address.Hex()
		result.ExecutableAction = "coordinator-create"
		result.RemainingActions = result.RemainingActions[1:]
	}
	if record.Receipt != nil {
		result.ReceiptObservation = "retained"
	}
	return result
}
