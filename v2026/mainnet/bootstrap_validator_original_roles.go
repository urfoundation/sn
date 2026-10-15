// Original validator request configuration is an unsigned review/adoption
// export. It never mutates or re-signs the admitted ReleaseConfig originals.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/validator"
)

const bootstrapValidatorOriginalRoleSchema = "urnetwork-mainnet-validator-original-request-role-v1"

// This exact separately selected file introduces request source authority. The
// old bootstrap approval cannot select new request keys or a Server profile.
type bootstrapValidatorOriginalRoleRequest struct {
	Schema          string                                    `json:"schema"`
	PreparationHash string                                    `json:"preparation_hash"`
	ServerProfile   string                                    `json:"server_profile"`
	Validators      []bootstrapValidatorOriginalRoleSelection `json:"validators"`
}

type bootstrapValidatorOriginalRoleSelection struct {
	ValidatorId uint64                                         `json:"validator_id"`
	Operators   []validator.ProductionOriginalRequestSelection `json:"operators"`
}

// A copied runtime OperatorConfig is useful for authoritative owner adoption,
// but the original config reference remains unchanged and no signature follows
// the proposed fields into this output.
type bootstrapValidatorOriginalRolePatch struct {
	ValidatorId    uint64                                        `json:"validator_id"`
	OriginalConfig planFileReference                             `json:"original_config"`
	RoleConfig     validator.ProductionOriginalRequestRoleConfig `json:"role_config"`
}

type bootstrapValidatorOriginalRoleConfig struct {
	Schema                           string                                `json:"schema"`
	PreparationHash                  string                                `json:"preparation_hash"`
	ContractRolePlanHash             string                                `json:"contract_role_plan_hash"`
	Request                          planFileReference                     `json:"request"`
	ServerProfile                    string                                `json:"server_profile"`
	Validators                       []bootstrapValidatorOriginalRolePatch `json:"validators"`
	RequiresOwnerSigningAndAdoption  bool                                  `json:"requires_owner_signing_and_adoption"`
	RequiresStoppedWriterPreparation bool                                  `json:"requires_stopped_writer_preparation"`
	ActivationReady                  bool                                  `json:"activation_ready"`
	NetworkEffects                   bool                                  `json:"network_effects"`
	ContentHash                      string                                `json:"content_hash"`
}

// Complete immutable inputs precede output. Scope comes from original signed
// deployment inputs plus the explicit profile, never from retained receipts.
func loadBootstrapValidatorOriginalRoleConfig(ctx context.Context, configPath, requestPath, expectedSha256 string) (bootstrapValidatorOriginalRoleConfig, error) {
	var result bootstrapValidatorOriginalRoleConfig
	if !planSha256(expectedSha256) {
		return result, errors.New("validator source role requires an independently pinned request file")
	}
	raw, requestHash, err := readBootstrapRootFile(ctx, requestPath, 256*1024)
	if err != nil || requestHash != expectedSha256 {
		return result, errors.Join(errors.New("validator original source request differs from approved bytes"), err)
	}
	var request bootstrapValidatorOriginalRoleRequest
	if err := decodePlanJson(raw, &request); err != nil {
		return result, err
	}
	if request.Schema != bootstrapValidatorOriginalRoleSchema || request.ServerProfile != "testnet" && request.ServerProfile != "mainnet" || len(request.Validators) == 0 || len(request.Validators) > 64 {
		return result, errors.New("validator source role requires explicit Server profile and a bounded complete validator census")
	}
	preparation, err := loadBootstrapChainPreparation(ctx, configPath)
	if err != nil {
		return result, err
	}
	roles, err := loadBootstrapContractRolePlan(ctx, configPath)
	if err != nil || roles.PreparationHash != preparation.Plan.ContentHash || request.PreparationHash != preparation.Plan.ContentHash {
		return result, errors.Join(errors.New("validator source role differs from original preparation or contract graph"), err)
	}
	if len(request.Validators) != len(preparation.Plan.Config.Validators) || preparation.Plan.Config.Network.EvmChainId == 0 || roles.CoordinatorProxy == (common.Address{}) {
		return result, errors.New("validator source role omits original validators or deployment key")
	}
	selections := map[uint64][]validator.ProductionOriginalRequestSelection{}
	for _, selection := range request.Validators {
		if _, exists := selections[selection.ValidatorId]; exists {
			return result, errors.New("validator source role repeats an original validator")
		}
		selections[selection.ValidatorId] = selection.Operators
	}
	for _, original := range preparation.Plan.Config.Validators {
		selected, exists := selections[original.ValidatorId]
		if !exists {
			return result, errors.New("validator source role omits an independently admitted validator")
		}
		configRaw, err := readBootstrapChainInput(ctx, original.Config, 2*1024*1024)
		if err != nil {
			return result, err
		}
		role, err := validator.InspectProductionOriginalRequestRoles(ctx, original.Config.Path, configRaw, request.ServerProfile, selected)
		if err != nil {
			return result, err
		}
		for _, operator := range role.Operators {
			scope := operator.RequestReceiptScope
			if scope == nil || scope.Profile != request.ServerProfile || scope.GenesisHash != common.HexToHash(preparation.Plan.Config.Network.GenesisHash) || scope.DeploymentId != preparation.Plan.Config.DeploymentId ||
				scope.DeploymentKey != fmt.Sprintf("%d:%s", preparation.Plan.Config.Network.EvmChainId, strings.ToLower(roles.CoordinatorProxy.Hex())) || scope.PolicyHash != roles.InitialPolicyHash || scope.Netuid != uint64(preparation.Plan.Config.Netuid) || scope.NoId != operator.NoID {
				return result, errors.New("validator receipt scope differs from independent original deployment inputs")
			}
		}
		result.Validators = append(result.Validators, bootstrapValidatorOriginalRolePatch{ValidatorId: original.ValidatorId, OriginalConfig: original.Config, RoleConfig: role})
	}
	result.Schema, result.PreparationHash, result.ContractRolePlanHash = bootstrapValidatorOriginalRoleSchema, preparation.Plan.ContentHash, roles.ContentHash
	result.Request = planFileReference{Path: requestPath, Sha256: requestHash}
	result.ServerProfile = request.ServerProfile
	result.RequiresOwnerSigningAndAdoption, result.RequiresStoppedWriterPreparation = true, true
	result.ContentHash = rootObjectHash(result)
	return result, nil
}

// Stdout is the deterministic patch artifact. There is no write or signing
// option for the admitted original ReleaseConfig and no runtime startup here.
func runBootstrapValidatorOriginalRoleCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("bootstrap-chain validator-source-role-config", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "original canonical chain preparation config")
	requestPath := flags.String("sources", "", "independently reviewed original request source selections")
	requestSha256 := flags.String("sources-sha256", "", "independently approved sha256: digest of source selections")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *configPath == "" || *requestPath == "" || !planSha256(*requestSha256) {
		fmt.Fprintln(stderr, "validator-source-role-config requires --config, --sources and --sources-sha256")
		return 2
	}
	result, err := loadBootstrapValidatorOriginalRoleConfig(ctx, *configPath, *requestPath, *requestSha256)
	if err != nil {
		fmt.Fprintln(stderr, "validator original source role:", err)
		return 2
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		fmt.Fprintln(stderr, "validator original source role output:", err)
		return 1
	}
	return 0
}
