// Current admission authenticates exact executable/domain views and fresh
// operator key responses. It leaves global custody and start authority closed.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urfoundation/sn/v2026/validator"
)

const validatorActivationProductionSchema = "urnetwork-mainnet-validator-activation-production-observation-v1"

// Each projection commits the exact approved executable and consumed views.
// It is owned-RPC evidence, not a source-to-bytecode or consensus proof.
type validatorActivationContractObservation struct {
	Address     common.Address `json:"address"`
	RuntimeHash string         `json:"runtime_hash"`
	GettersHash string         `json:"getters_hash"`
	StorageHash string         `json:"storage_hash"`
}

// The same native/EVM boundary is consumed by all five contracts and both
// validator operator censuses. No health boolean can become a start capability.
type validatorActivationProductionReadiness struct {
	Schema           string                                     `json:"schema"`
	EvidenceHash     string                                     `json:"evidence_hash"`
	ContractPlanHash string                                     `json:"contract_plan_hash"`
	MappingHash      string                                     `json:"native_evm_mapping_hash"`
	EvmBlock         uint64                                     `json:"evm_block"`
	EvmHash          string                                     `json:"evm_hash"`
	Contracts        []validatorActivationContractObservation   `json:"contracts"`
	Validators       []validator.ProductionBootstrapObservation `json:"validators"`
}

// Journal validation detects malformed projections; fresh authority must still
// perform real reads. Historical journals can omit this optional projection.
func (self validatorActivationProductionReadiness) validate(plan validatorActivationPlan, readiness validatorActivationReadiness) error {
	hash := self.EvidenceHash
	self.EvidenceHash = ""
	if self.Schema != validatorActivationProductionSchema || !planSha256(hash) || hash != rootObjectHash(self) ||
		!planSha256(self.ContractPlanHash) || !planSha256(self.MappingHash) || self.EvmBlock == 0 || !rootCanonicalHash(self.EvmHash) || len(self.Contracts) != 5 || len(self.Validators) != 2 || readiness.Native == nil {
		return errors.New("validator activation production observation is incomplete")
	}
	addresses := map[common.Address]bool{}
	for _, contract := range self.Contracts {
		if contract.Address == (common.Address{}) || addresses[contract.Address] || !rootCanonicalHash(contract.RuntimeHash) || !planSha256(contract.GettersHash) || !planSha256(contract.StorageHash) {
			return errors.New("validator activation executable census differs")
		}
		addresses[contract.Address] = true
	}
	for i, observed := range self.Validators {
		if observed.ConfigHash != plan.Units[i].Unit.Config.Sha256 || observed.ValidatorId != plan.Units[i].Source.ValidatorId || observed.DeploymentId != plan.Units[i].Source.DeploymentId ||
			observed.EvmBlock != self.EvmBlock || observed.EvmHash != self.EvmHash || observed.Native.Block != readiness.FinalizedNumber || common.Hash(observed.Native.Hash).Hex() != readiness.FinalizedHash || observed.Native.Epoch != readiness.Native.NativeEpoch ||
			common.Hash(observed.Native.Hotkey).Hex() != readiness.Roles[i].Expected.Hotkey || len(observed.Operators) == 0 || len(observed.Operators) > 16 {
			return errors.New("validator activation operator observation differs from original role or clocks")
		}
		operators := map[uint64]bool{}
		for _, operator := range observed.Operators {
			if operator.NoId == 0 || operators[operator.NoId] || operator.ClientId == "" || operator.ClientKeyGeneration == 0 || operator.PublishedBlock == 0 || operator.PublishedBlock > self.EvmBlock ||
				!rootCanonicalHash(operator.ActivationHash) || !rootCanonicalHash(operator.ClientKey) || !rootCanonicalHash(operator.ClientKeyRegistrationHash) || !rootCanonicalHash(operator.ClientKeyResponseHash) || !rootCanonicalHash(operator.ObservationNonce) || !common.IsHexAddress(operator.RootSigner) || common.HexToAddress(operator.RootSigner) == (common.Address{}) {
				return errors.New("validator activation operator observation is malformed")
			}
			operators[operator.NoId] = true
		}
	}
	return nil
}

// Only the original independently approved complete installation graph selects
// deployed addresses or executable bytes. This preflight precedes network I/O.
func validatorActivationContractPlans(ctx context.Context, preparation bootstrapChainPreparation) ([]evmCreatePlan, error) {
	if len(preparation.Contracts.Config.Plan.Actions) < 8 {
		return nil, errors.New("validator production observation requires the complete approved contract profile")
	}
	_, plans, err := prepareBootstrapContractReadiness(ctx, preparation.Contracts, preparation.Plan.Config.Contracts.Path)
	if err != nil || len(plans) != 8 {
		return nil, errors.Join(errors.New("validator production contract profile is unavailable"), err)
	}
	return plans, nil
}

// Constructor counters are not current invariants. Explicitly exclude only
// accounting/census clocks, retain authority and pause checks, and substitute
// the exact one-shot evidence anchor. Current policy/roots are checked by the
// real client-key historical-authority reader at this same EVM boundary.
func validatorActivationContractViews(plan evmCreatePlan, index int, evidence common.Address) ([]contractGetter, []contractStorageWord) {
	coordinator, reserve, vault := stabi.NewSTCoordinator(), stabi.NewSTReserveSink(), stabi.NewSTSettlementVault()
	var mutable [][]byte
	switch index {
	case 4:
		mutable = [][]byte{coordinator.PackCurrentEpoch(), coordinator.PackOperatorCount(), coordinator.PackCampaignReserved()}
	case 5:
		mutable = [][]byte{reserve.PackPrincipal()}
	case 6:
		mutable = [][]byte{vault.PackTotalCaptured(), vault.PackTotalPaid(), vault.PackPendingFunding(), vault.PackOutstandingLiability(), vault.PackEscrowAccounted()}
	}
	getters := slices.DeleteFunc(slices.Clone(plan.Getters), func(getter contractGetter) bool {
		for _, data := range mutable {
			if getter.Data == "0x"+hex.EncodeToString(data) {
				return true
			}
		}
		return false
	})
	for i := range getters {
		if index == 4 && getters[i].Data == "0x"+hex.EncodeToString(coordinator.PackValidatorEvidence()) {
			getters[i].Expected = common.BytesToHash(evidence[:]).Hex()
		}
	}
	storage := slices.Clone(plan.Storage)
	if index == 5 || index == 6 {
		// Reserve recorder and packed vault coordinator/escrow flag are slot
		// zero. Other words are mutable accounting or transient reentrancy.
		storage = slices.DeleteFunc(storage, func(word contractStorageWord) bool { return common.HexToHash(word.Slot) != (common.Hash{}) })
	}
	return getters, storage
}

// One fixed canonical selector follows exact native-header authentication.
// Runtime equality precedes getters, with bounded canonical hex at every read.
func (self *rpcClient) observeValidatorActivationContracts(ctx context.Context, plans []evmCreatePlan, mapping finalizedMapping) ([]validatorActivationContractObservation, error) {
	if len(plans) != 8 || mapping.EvmHeader.Number == 0 || !rootCanonicalHash(mapping.EvmHeader.Hash) {
		return nil, errors.New("validator contract observation lacks its complete mapped scope")
	}
	block := map[string]any{"blockHash": mapping.EvmHeader.Hash, "requireCanonical": true}
	observations := make([]validatorActivationContractObservation, 0, 5)
	for _, index := range []int{2, 4, 5, 6, 7} {
		plan := plans[index]
		var encoded string
		if err := self.callEvmRead(ctx, "eth_getCode", []any{plan.Address.Hex(), block}, &encoded); err != nil {
			return nil, err
		}
		code, err := rootReceiptHex(encoded, 64*1024)
		if err != nil || encoded != "0x"+hex.EncodeToString(code) || len(code) == 0 || !bytes.Equal(code, plan.Runtime) {
			return nil, errors.Join(errors.New("validator deployed executable differs from original approved artifact"), err)
		}
		getters, storage := validatorActivationContractViews(plan, index, plans[7].Address)
		for _, getter := range getters {
			if err := self.callEvmRead(ctx, "eth_call", []any{map[string]any{"to": plan.Address.Hex(), "data": getter.Data}, block}, &encoded); err != nil {
				return nil, err
			}
			if encoded != getter.Expected {
				return nil, errors.New("validator deployed contract domain or authority getter differs")
			}
		}
		for _, word := range storage {
			if err := self.callEvmRead(ctx, "eth_getStorageAt", []any{plan.Address.Hex(), word.Slot, block}, &encoded); err != nil {
				return nil, err
			}
			if encoded != word.Expected {
				return nil, errors.New("validator deployed contract binding or authority storage differs")
			}
		}
		if index == 4 {
			// Initialization rewrites the policy's effective clock, while every
			// economic/cadence field must retain the independently approved bytes.
			binding := stabi.NewSTCoordinator()
			data := "0x" + hex.EncodeToString(binding.PackPolicyByIndex(big.NewInt(0)))
			if err := self.callEvmRead(ctx, "eth_call", []any{map[string]any{"to": plan.Address.Hex(), "data": data}, block}, &encoded); err != nil {
				return nil, err
			}
			raw, err := rootReceiptHex(encoded, 1024)
			if err != nil || encoded != "0x"+hex.EncodeToString(raw) || plan.ProxyConstructor == nil {
				return nil, errors.Join(errors.New("validator initial policy encoding is unavailable"), err)
			}
			observed, err := binding.UnpackPolicyByIndex(raw)
			if err != nil || observed.EffectiveEpoch != 0 || observed.EffectiveBlock == 0 || observed.EffectiveBlock > mapping.EvmHeader.Number {
				return nil, errors.Join(errors.New("validator initial policy effective clock differs"), err)
			}
			expected := plan.ProxyConstructor.ApprovedPolicy.binding()
			expected.EffectiveEpoch, expected.EffectiveBlock = 0, observed.EffectiveBlock
			parsed, err := stabi.STCoordinatorMetaData.ParseABI()
			if err != nil {
				return nil, err
			}
			canonical, err := parsed.Methods["policyByIndex"].Outputs.Pack(expected)
			if err != nil || !bytes.Equal(canonical, raw) {
				return nil, errors.Join(errors.New("validator initial policy differs from original approved fields"), err)
			}
			getters = append(getters, contractGetter{Data: data, Expected: encoded})
		}
		observations = append(observations, validatorActivationContractObservation{Address: plan.Address, RuntimeHash: crypto.Keccak256Hash(code).Hex(), GettersHash: rootObjectHash(getters), StorageHash: rootObjectHash(storage)})
	}
	return observations, ctx.Err()
}

// Endpoint responsiveness, dual-signed activation publication and registered
// client keys are independently authenticated. Prefix replay, historical
// native activation eligibility, worker lifecycle, custody and majority remain
// outside this observation and all public fresh starts remain unavailable.
func (self *rpcClient) observeValidatorActivationProduction(ctx context.Context, preparation bootstrapChainPreparation, plans []evmCreatePlan, readiness bootstrapChainReadiness, result *validatorActivationReadiness) (*validatorActivationProductionReadiness, error) {
	identity := readiness.Census.Observation.Identity
	mapping, err := self.readFinalizedMappingAtIdentity(ctx, identity)
	if err != nil {
		return nil, err
	}
	contracts, err := self.observeValidatorActivationContracts(ctx, plans, mapping)
	if err != nil {
		return nil, err
	}
	production := &validatorActivationProductionReadiness{Schema: validatorActivationProductionSchema, ContractPlanHash: preparation.Plan.ContractPlanHash, MappingHash: rootObjectHash(mapping), EvmBlock: mapping.EvmHeader.Number, EvmHash: mapping.EvmHeader.Hash, Contracts: contracts}
	for i, role := range preparation.Plan.Config.Validators {
		raw, err := readBootstrapChainInput(ctx, role.Config, 2*1024*1024)
		if err != nil {
			return nil, err
		}
		native := validator.ProductionBootstrapNativePoint{Block: identity.FinalizedNumber, Hash: common.HexToHash(identity.FinalizedHash), Epoch: result.Native.NativeEpoch, Hotkey: common.HexToHash(result.Roles[i].Expected.Hotkey)}
		evm := validator.ProductionBootstrapEvmPoint{Block: mapping.EvmHeader.Number, Hash: common.HexToHash(mapping.EvmHeader.Hash)}
		observed, err := validator.ObserveProductionBootstrapOperators(ctx, role.Config.Path, raw, native, evm)
		if err != nil {
			return nil, err
		}
		production.Validators = append(production.Validators, *observed)
	}
	// Both endpoints may have taken time. Recheck the original native and EVM
	// mapping, then the signed activation checkpoint, after the last reply.
	confirmed, err := self.readFinalizedMappingAtIdentity(ctx, identity)
	if err != nil || rootObjectHash(confirmed) != production.MappingHash {
		return nil, errors.Join(errors.New("validator production native/EVM mapping changed during observation"), err)
	}
	var checkpoint string
	if err := self.call(ctx, "chain_getBlockHash", []any{result.Native.ActivationBlock}, &checkpoint); err != nil {
		return nil, err
	}
	if !strings.EqualFold(checkpoint, result.Native.ActivationHash) {
		return nil, fmt.Errorf("%w: validator production activation checkpoint changed", errRpcIntegrity)
	}
	production.EvidenceHash = rootObjectHash(*production)
	return production, ctx.Err()
}
