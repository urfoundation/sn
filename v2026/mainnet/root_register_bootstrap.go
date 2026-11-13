// A completed registration supplies one exact seat to later bootstrap planning.
// The handoff cannot create passive-service approval or claim deployment readiness.
package main

import (
	"context"
	"errors"
	"flag"
	"io"
)

const rootRegisterBootstrapInputSchema = "urnetwork-mainnet-root-register-bootstrap-input-v1"
const rootRegisterBootstrapHandoffSchema = "urnetwork-mainnet-root-register-bootstrap-handoff-v1"

// Independently pinned input bytes bind the intended deployment, network,
// original registration config and passive root input before custody is opened.
type rootRegisterBootstrapInput struct {
	Schema             string            `json:"schema"`
	DeploymentId       string            `json:"deployment_id"`
	Network            planNetwork       `json:"network"`
	RootInput          planFileReference `json:"root_input"`
	RegistrationConfig planFileReference `json:"registration_config"`
	Operator           string            `json:"operator_account_id"`
	Hotkey             string            `json:"hotkey_account_id"`
}

// The proposed role deliberately omits its independent passive approver and
// approval file. A registration receipt grants no continuing observation service.
type rootRegisterBootstrapHandoff struct {
	Schema                 string                      `json:"schema"`
	Status                 string                      `json:"status"`
	DeploymentId           string                      `json:"deployment_id"`
	Network                planNetwork                 `json:"network"`
	InputSha256            string                      `json:"bootstrap_input_sha256"`
	RootInput              planFileReference           `json:"root_input"`
	RegistrationConfig     planFileReference           `json:"registration_config"`
	RegistrationConfigHash string                      `json:"registration_config_hash"`
	ApprovalPublicKey      string                      `json:"registration_approval_public_key_ed25519"`
	ActionHash             string                      `json:"registration_action_hash"`
	RequestHash            string                      `json:"registration_signing_request_hash"`
	ExtrinsicHash          string                      `json:"registration_extrinsic_hash"`
	Receipt                rootActionReceipt           `json:"original_finalized_receipt"`
	ReconciliationHash     string                      `json:"original_reconciliation_hash"`
	RootValidator          bootstrapChainRootValidator `json:"proposed_root_validator"`
	RequiredSteps          []string                    `json:"required_steps"`
	NetworkEffects         bool                        `json:"network_effects"`
	NativeSigning          bool                        `json:"native_signing"`
	ActivationReady        bool                        `json:"activation_ready"`
	ContentHash            string                      `json:"content_hash"`
}

// This projection accepts only original terminal custody and an agreeing
// event/readback generation. Current membership must still be checked by service.
func prepareRootRegisterBootstrapHandoff(input rootRegisterBootstrapInput, inputHash string, root bootstrapRootConfig, record rootRegisterRecord) (rootRegisterBootstrapHandoff, error) {
	var result rootRegisterBootstrapHandoff
	policy := record.Config.Action.Policy
	network := planNetwork{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}
	if input.Schema != rootRegisterBootstrapInputSchema || !planSha256(inputHash) || !planLabel(input.DeploymentId) ||
		input.Network != network || input.Operator != policy.Operator || input.Hotkey != policy.Hotkey ||
		!bootstrapRootAbsolutePath(input.RootInput.Path) || !planSha256(input.RootInput.Sha256) ||
		!bootstrapRootAbsolutePath(input.RegistrationConfig.Path) || !planSha256(input.RegistrationConfig.Sha256) ||
		input.RootInput.Path == input.RegistrationConfig.Path || root.Schema != bootstrapRootPassiveConfigSchema ||
		root.DeploymentId != input.DeploymentId || root.Network != input.Network || !bootstrapRootAbsolutePath(root.RunDirectory) ||
		!bootstrapRootAbsolutePath(root.RootService.Path) || !planSha256(root.RootService.Sha256) {
		return result, errors.New("root registration bootstrap input differs from the explicit deployment, passive root input or operator role")
	}
	if err := record.validate(record.Config, record.ApprovalKey); err != nil {
		return result, err
	}
	if record.Phase != "finalized" || record.Request == nil || record.Reconciliation == nil || record.Reconciliation.Receipt == nil {
		return result, errors.New("root registration bootstrap handoff requires original finalized registration custody")
	}
	seat := rootRegisterObservedSeat(*record.Request, *record.Reconciliation)
	if seat == nil {
		return result, errors.New("root registration bootstrap handoff has no agreeing original seat event and inclusion readback")
	}
	for _, path := range []string{input.RootInput.Path, input.RegistrationConfig.Path, root.RootService.Path} {
		if path == record.Config.Action.StatePath || path == record.Config.Action.StatePath+".lock" {
			return result, errors.New("root registration bootstrap input overlaps original custody")
		}
	}
	netuid, originalSeat := uint16(0), *seat
	result = rootRegisterBootstrapHandoff{
		Schema: rootRegisterBootstrapHandoffSchema, Status: "passive-service-configuration-and-approval-required", DeploymentId: input.DeploymentId,
		Network: network, InputSha256: inputHash, RootInput: input.RootInput, RegistrationConfig: input.RegistrationConfig,
		RegistrationConfigHash: rootObjectHash(record.Config), ApprovalPublicKey: record.ApprovalKey, ActionHash: record.Config.Action.RequestHash,
		RequestHash: record.Request.ContentHash, ExtrinsicHash: record.ExtrinsicHash,
		Receipt: *record.Reconciliation.Receipt, ReconciliationHash: rootObjectHash(record.Reconciliation),
		RootValidator: bootstrapChainRootValidator{Role: "bittensor-root-validator", Netuid: &netuid, Implementation: "sn/mainnet/root-passive-service",
			Hotkey: policy.Hotkey, Coldkey: policy.Operator, Seat: &originalSeat, Strategy: rootPassiveStrategy},
		RequiredSteps: []string{"prepare-passive-service-with-original-seat-and-independent-runtime-pins", "approve-complete-passive-service-config-and-bootstrap-root-plan", "run-bootstrap-chain-v4-with-separate-ur-validator-roles", "verify-current-root-membership-and-service-readiness"},
	}
	result.ContentHash = rootObjectHash(result)
	return result, nil
}

// Only pinned local inputs and original retained custody are opened. This
// operation has no chain reader, signer, sender or passive-approval constructor.
func rootRegisterBootstrapCommand(ctx context.Context, args []string, stderr io.Writer) (value any, resultErr error) {
	flags := flag.NewFlagSet("root-register bootstrap-handoff", flag.ContinueOnError)
	flags.SetOutput(stderr)
	inputPath := flags.String("input", "", "explicit bootstrap registration input")
	inputHash := flags.String("input-sha256", "", "independently pinned bootstrap input bytes")
	deployment := flags.String("deployment-id", "", "independently selected deployment")
	key := flags.String("approval-key", "", "independent original registration approval key")
	actionHash := flags.String("accept-action-hash", "", "independently accepted registration action hash")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *inputPath == "" || !planSha256(*inputHash) ||
		!planLabel(*deployment) || !rootCanonicalHash(*key) || !planSha256(*actionHash) {
		return nil, errors.New("root registration bootstrap handoff requires explicit input/file hash, deployment, approval key and original action hash")
	}
	raw, actual, err := readBootstrapRootFile(ctx, *inputPath, 32*1024)
	if err != nil || actual != *inputHash {
		return nil, errors.Join(errors.New("root registration bootstrap input differs from its independent byte pin"), err)
	}
	var input rootRegisterBootstrapInput
	if err := decodePlanJson(raw, &input); err != nil {
		return nil, err
	}
	if input.DeploymentId != *deployment || input.Schema != rootRegisterBootstrapInputSchema {
		return nil, errors.New("root registration bootstrap deployment or input domain differs")
	}
	raw, actual, err = readBootstrapRootFile(ctx, input.RegistrationConfig.Path, 128*1024)
	if err != nil || actual != input.RegistrationConfig.Sha256 {
		return nil, errors.Join(errors.New("root registration bootstrap config differs from its original file pin"), err)
	}
	var config rootRegisterConfig
	if err := decodePlanJson(raw, &config); err != nil {
		return nil, err
	}
	if err := config.validate(*key); err != nil {
		return nil, err
	}
	if config.Action.RequestHash != *actionHash || input.Operator != config.Action.Policy.Operator || input.Hotkey != config.Action.Policy.Hotkey {
		return nil, errors.New("root registration bootstrap action, operator or hotkey differs")
	}
	raw, actual, err = readBootstrapRootFile(ctx, input.RootInput.Path, 32*1024)
	if err != nil || actual != input.RootInput.Sha256 {
		return nil, errors.Join(errors.New("root registration bootstrap root input differs from its original file pin"), err)
	}
	var root bootstrapRootConfig
	if err := decodePlanJson(raw, &root); err != nil {
		return nil, err
	}
	for _, path := range []string{*inputPath, input.RegistrationConfig.Path, input.RootInput.Path, root.RootService.Path} {
		if path == config.Action.StatePath || path == config.Action.StatePath+".lock" {
			return nil, errors.New("root registration bootstrap input overlaps original custody")
		}
	}
	store, err := openRootRegisterStore(config, *key, false, ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr == nil {
			_, resultErr = store.load()
		}
		resultErr = errors.Join(resultErr, store.close())
		if resultErr != nil {
			value = nil
		}
	}()
	record, err := store.load()
	if err != nil {
		return nil, err
	}
	return prepareRootRegisterBootstrapHandoff(input, *inputHash, root, record)
}
