package main

import (
	"fmt"
	"github.com/urnetwork/server/controller"
	"github.com/urnetwork/server/model"
	"gopkg.in/yaml.v3"
)

// Use one pure encoder for reviewed preview bytes and actual startup inputs.
func marshalRuntimeOperatorStConfig(cfg *ResolvedConfig, roles *RoleSecrets, contracts *ContractDeployment, publicRPCURL string, eventSyncBlock uint64, uploadBudget model.StAttemptUploadBudget, reservedUpload controller.StReservedAttemptUploadConfig, i int) ([]byte, error) {
	deposit := roles.EVM[fmt.Sprintf("operator-%d-deposit", i)].PrivateKeyHex
	rootKey := roles.EVM[fmt.Sprintf("operator-%d-root", i)].PrivateKeyHex
	artifactKey := roles.EVM[fmt.Sprintf("operator-%d-artifact", i)].PrivateKeyHex
	depositHotkey := "0x" + roles.Substrate[fmt.Sprintf("operator-%d-deposit-hotkey", i)].PublicKeyHex
	// Testnet services are intentionally rendered with only testnet-prefixed
	// values. The server loader must never be able to fall through to a
	// mainnet signer or address when URNETWORK_ST_PROFILE=testnet.
	st := map[string]any{
		"profile":                         "testnet",
		"testnet-enabled":                 true,
		"testnet-attempt-upload":          uploadBudget,
		"testnet-reserved-attempt-upload": reservedUpload,
		// Swarms sign with their existing payout roles. Retain unsigned
		// compatibility only for explicitly admitted provisional runs.
		"testnet-wallet-allow-unsigned":              provisionalResumeEnabled(cfg),
		"testnet-public-rpc-url":                     publicRPCURL,
		"testnet-authority":                          workloadRPCAuthority(),
		"testnet-rpc-urls":                           []string{evmHTTP(workloadRPCAuthority())},
		"testnet-chain-id":                           testnetChainID,
		"testnet-genesis-hash":                       testnetGenesis,
		"testnet-deployment-id":                      cfg.Config.Deployment.DeploymentID,
		"testnet-policy-hash":                        cfg.PolicyHash,
		"testnet-coordinator-address":                contracts.CoordinatorProxy.Hex(),
		"testnet-settlement-vault-address":           contracts.SettlementVault.Hex(),
		"testnet-reserve-sink-address":               contracts.ReserveSink.Hex(),
		"testnet-deploy-block":                       eventSyncBlock,
		"testnet-netuid":                             cfg.Netuid,
		"testnet-no-id":                              i,
		"testnet-treasury-hotkey":                    "0x" + roles.Substrate[operatorPoolHotkeyLabelForGeneration(i, contracts.RegistrationRoleGeneration)].PublicKeyHex,
		"testnet-deposit-hotkey":                     depositHotkey,
		"testnet-deposit-key":                        deposit,
		"testnet-root-key":                           rootKey,
		"testnet-artifact-key":                       artifactKey,
		"testnet-deposit-rate-numerator-rao-per-gib": cfg.Policy.Deposit.Tiers[0].RateNumeratorRaoPerGiB,
		"testnet-deposit-rate-denominator":           cfg.Policy.Deposit.Tiers[0].RateDenominator,
		"testnet-deposit-tiers":                      cfg.Policy.Deposit.Tiers,
		"testnet-deposit-epoch-cap-rao":              cfg.Policy.Deposit.EpochCapRaoPerOperator,
		"testnet-reliability-a-min":                  cfg.Policy.Verify.ReliabilityAMin,
		"testnet-block-seconds":                      cfg.Public.Chain.ExpectedBlockSeconds,
	}
	return yaml.Marshal(st)
}
