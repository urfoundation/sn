package main

import (
	"fmt"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// One pure renderer feeds both the reviewable startup request and the actual
// runtime writer, so a new plan cannot approve guessed config bytes.
func runtimeComponentConfigBase(cfg *ResolvedConfig, contracts *ContractDeployment, eventSyncBlock uint64) map[string]any {
	return map[string]any{"schema_version": 1, "production": true, "release": "1.0", "deployment_id": cfg.Config.Deployment.DeploymentID, "chain_id": testnetChainID, "genesis_hash": testnetGenesis, "runtime_spec": cfg.Public.Chain.ExpectedRuntimeSpec, "transaction_version": cfg.Public.Chain.ExpectedTransactionVersion, "state_version": cfg.Public.Chain.ExpectedStateVersion, "runtime_code_hash": cfg.Release.Runtime.CodeHash, "runtime_metadata_hash": cfg.Release.Runtime.MetadataHash, "netuid": cfg.Netuid, "coordinator": contracts.CoordinatorProxy.Hex(), "settlement_vault": contracts.SettlementVault.Hex(), "deploy_block": eventSyncBlock, "policy_hash": cfg.PolicyHash, "rpc": []string{evmHTTP(workloadRPCAuthority())}, "substrate": []string{substrateWS(workloadSubstrateRPCAuthority())}}
}

func marshalRuntimeValidatorConfig(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, base map[string]any, id int) ([]byte, error) {
	v := cloneMap(base)
	v["validator_id"] = id
	v["state_dir"] = filepath.Join(stateDir, "runtime", fmt.Sprintf("validator-%d", id), "state")
	v["hotkey_seed_file"] = filepath.Join(stateDir, "secrets", fmt.Sprintf("validator-%d-hotkey.seed", id))
	v["controlled_no_ids"] = controlledNOIDsForValidator(id)
	v["trail_depth"] = cfg.Policy.Verify.TrailDepth
	v["poll_seconds"] = validatorPollSeconds(cfg)
	v["version_key"] = hyperparameterUint64(cfg.Hyperparameters.OwnerControlled["weights_version_key"])
	v["policy"] = cfg.Policy
	v["operators"] = operatorDirectory(cfg, stateDir, roles, id)
	v["evidence_v2"] = cfg.Config.ValidatorEvidenceV2[id-1].Evidence
	return yaml.Marshal(v)
}
