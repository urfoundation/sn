package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Preview the four native-reader configuration changes against canonical live
// paths without writing the deployment tree, stopping processes, or using RPC.
// Every other manifest input is read and authenticated from retained state.
func previewProvisionalRuntimeConfigs(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets) (map[string][]byte, error) {
	if !provisionalResumeEnabled(cfg) || !filepath.IsAbs(stateDir) {
		return nil, errors.New("runtime compatibility preview requires explicit provisional mode and an absolute retained state directory")
	}
	resolved, err := runtimeEvidenceV2ResolvedConfig(cfg, stateDir)
	if err != nil {
		return nil, err
	}
	cfg = resolved
	contracts, err := loadContractDeployment(stateDir)
	if err != nil {
		return nil, err
	}
	eventSyncBlock, err := contractDeploymentEventSyncBlock(contracts)
	if err != nil {
		return nil, err
	}
	uploadBudget, err := runtimeAttemptUploadBudget(cfg)
	if err != nil {
		return nil, err
	}
	reservedUploads, err := runtimeReservedAttemptUploads(cfg, stateDir, contracts)
	if err != nil {
		return nil, err
	}
	publicRPCURL := verificationEVMEndpoint(cfg)
	if strings.TrimSpace(publicRPCURL) == "" || strings.TrimSpace(publicRPCURL) != publicRPCURL {
		return nil, errors.New("runtime compatibility preview RPC is non-canonical")
	}
	preview := map[string][]byte{}
	base := runtimeComponentConfigBase(cfg, contracts, eventSyncBlock)
	for id := 1; id <= cfg.Config.Topology.Validators; id++ {
		wire, err := marshalRuntimeValidatorConfig(cfg, stateDir, roles, base, id)
		if err != nil {
			return nil, err
		}
		preview[fmt.Sprintf("runtime/validator-%d/validator.yml", id)] = wire
	}
	for id := 1; id <= cfg.Config.Topology.Operators; id++ {
		wire, err := marshalRuntimeOperatorStConfig(cfg, roles, contracts, publicRPCURL, eventSyncBlock, uploadBudget, reservedUploads[id-1], id)
		if err != nil {
			return nil, err
		}
		preview[fmt.Sprintf("runtime/operator-%d/vault/st.yml", id)] = wire
	}
	manifest, err := buildRuntimeConfigManifestWithPreview(cfg, stateDir, preview)
	if err != nil {
		return nil, err
	}
	wire, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	preview["runtime-config-manifest.json"] = append(wire, '\n')
	return preview, nil
}
