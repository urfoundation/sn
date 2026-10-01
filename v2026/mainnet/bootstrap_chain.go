// Offline composition invokes only the existing custody preparation owners.
// It has no chain adapter, signature import, signing key or service-start port.
package main

import (
	"context"
	"errors"
	"path/filepath"
)

const bootstrapChainResultSchema = "urnetwork-mainnet-bootstrap-chain-result-v3"

// Local preparation is complete only after both real child owners are checked.
// No result claims an executed trim, installation, eligible role or activation.
type bootstrapChainResult struct {
	Schema                      string              `json:"schema"`
	PlanHash                    string              `json:"plan_hash"`
	LocalPreparationComplete    bool                `json:"local_preparation_complete"`
	NetworkEffects              bool                `json:"network_effects"`
	ActivationReady             bool                `json:"activation_ready"`
	OwnerTrimStatus             string              `json:"owner_trim_status"`
	UrValidatorsStatus          string              `json:"ur_validators_status"`
	UrValidatorConfigsVerified  bool                `json:"ur_validator_configs_verified,omitempty"`
	RootValidatorStatus         string              `json:"root_validator_status,omitempty"`
	RootValidatorConfigVerified bool                `json:"root_validator_config_verified,omitempty"`
	PendingChainPhases          []string            `json:"pending_chain_phases"`
	Contracts                   evmCreateResult     `json:"contract_custody"`
	Root                        bootstrapRootResult `json:"root_custody"`
}

// The caller holds the chain store's exclusive ownership through all child
// closes. Hooks expose durable boundaries only; they supply no chain verdict.
func advanceBootstrapChain(ctx context.Context, store *bootstrapChainStore, boundaryHook func(string) error) (bootstrapChainResult, error) {
	var result bootstrapChainResult
	if ctx == nil {
		return result, errors.New("bootstrap chain context is absent")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	record, err := store.load()
	if err != nil {
		return result, err
	}
	preparation := store.preparation
	contracts, err := func() (result evmCreateResult, resultErr error) {
		path := filepath.Join(preparation.Plan.Config.RunDirectory, evmCreateStateFile)
		create, err := bootstrapRootCreateChild(path, record.Phase != "claimed")
		if err != nil {
			return result, err
		}
		child, err := openEvmActionStore(preparation.Contracts.Config, create, nil)
		if err != nil {
			return result, err
		}
		defer func() { resultErr = errors.Join(resultErr, child.close()) }()
		owner, err := newEvmCreateOwner(preparation.Contracts, child, nil)
		if err != nil {
			return result, err
		}
		return owner.advance(ctx, nil, false, false)
	}()
	if err != nil {
		return result, err
	}
	if record.ContractTransactionHash != "" && record.ContractTransactionHash != contracts.TransactionHash {
		return result, errors.New("bootstrap chain contract custody lost or replaced its original signature")
	}
	if boundaryHook != nil {
		if err := boundaryHook("contracts-retained"); err != nil {
			store.failed = err
			return result, err
		}
	}
	if record.Phase == "claimed" {
		record.Phase, record.ContractTransactionHash = "contracts-retained", contracts.TransactionHash
		if err := store.save(record); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	root, err := func() (result bootstrapRootResult, resultErr error) {
		path := filepath.Join(preparation.Root.RunDirectory, bootstrapRootProgressFile)
		create, err := bootstrapRootCreateChild(path, record.Phase == "prepared")
		if err != nil {
			return result, err
		}
		child, err := openBootstrapRootStore(preparation.Root, create)
		if err != nil {
			return result, err
		}
		defer func() { resultErr = errors.Join(resultErr, child.close()) }()
		owner, err := newBootstrapRootOwner(preparation.Root, child)
		if err != nil {
			return result, err
		}
		return owner.advance(ctx, nil)
	}()
	if err != nil {
		return result, err
	}
	if record.RootExtrinsicHash != "" && record.RootExtrinsicHash != root.ExtrinsicHash {
		return result, errors.New("bootstrap chain root custody lost or replaced its original signature")
	}
	if boundaryHook != nil {
		if err := boundaryHook("root-retained"); err != nil {
			store.failed = err
			return result, err
		}
	}
	if record.Phase != "prepared" || record.ContractTransactionHash != contracts.TransactionHash || record.RootExtrinsicHash != root.ExtrinsicHash {
		record.Phase, record.ContractTransactionHash, record.RootExtrinsicHash = "prepared", contracts.TransactionHash, root.ExtrinsicHash
		if err := store.save(record); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result = bootstrapChainResult{Schema: bootstrapChainResultSchema, PlanHash: preparation.Plan.ContentHash, LocalPreparationComplete: true,
		OwnerTrimStatus: "retained-review-execution-blocked", UrValidatorsStatus: "two-protected-role-inputs-pinned-production-admission-pending",
		PendingChainPhases: bootstrapChainPendingPhases(), Contracts: contracts, Root: root}
	if preparation.Plan.Config.Schema != bootstrapChainConfigSchemaV1 {
		result.UrValidatorsStatus = "two-signed-production-configs-verified-live-admission-pending"
		result.UrValidatorConfigsVerified = true
		if bootstrapChainHasRootRole(preparation.Plan.Config.Schema) {
			result.RootValidatorStatus = "signed-root-service-config-verified-live-authority-pending"
			result.RootValidatorConfigVerified = true
			if preparation.Root.PassiveService != nil {
				result.Schema = "urnetwork-mainnet-bootstrap-chain-result-v4"
				result.RootValidatorStatus = "signed-passive-root-service-config-verified-observation-pending"
			}
		} else {
			result.Schema = "urnetwork-mainnet-bootstrap-chain-result-v2"
		}
	} else {
		result.Schema = "urnetwork-mainnet-bootstrap-chain-result-v1"
	}
	return result, nil
}
