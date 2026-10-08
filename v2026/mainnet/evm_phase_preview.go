// Unsigned review exports exact public approval material without acquiring
// custody, loading a signing key, opening a journal or creating an RPC adapter.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/ethereum/go-ethereum/crypto"
)

const evmPhasePreviewSchema = "urnetwork-mainnet-contract-phase-preview-v1"

// The reviewable typed plan and the exact domain-separated bytes are exported
// together. Hashes identify content; only the independent signature can approve.
type evmPhasePreview struct {
	Schema                         string                       `json:"schema"`
	Plan                           evmPhasePlan                 `json:"plan"`
	PlanHash                       string                       `json:"plan_hash"`
	ApprovalPublicKey              string                       `json:"approval_public_key_ed25519"`
	ApprovalSigningMessageHex      string                       `json:"approval_signing_message_hex"`
	ApprovalSigningMessageSha256   string                       `json:"approval_signing_message_sha256"`
	ApprovalVerified               bool                         `json:"approval_verified"`
	ExecutableAction               string                       `json:"executable_action"`
	ReserveAddress                 string                       `json:"reserve_address"`
	ExpectedReserveRuntimeHash     string                       `json:"expected_reserve_runtime_hash"`
	VaultAddress                   string                       `json:"vault_address,omitempty"`
	ExpectedVaultRuntimeHash       string                       `json:"expected_vault_runtime_hash,omitempty"`
	VaultConstructor               *contractVaultConstructor    `json:"vault_constructor,omitempty"`
	CoordinatorAddress             string                       `json:"coordinator_implementation_address,omitempty"`
	ExpectedCoordinatorRuntimeHash string                       `json:"expected_coordinator_runtime_hash,omitempty"`
	CoordinatorStorage             []contractStorageWord        `json:"coordinator_storage,omitempty"`
	EscrowRegistration             *contractEscrowRegistration  `json:"escrow_registration,omitempty"`
	ProxyAddress                   string                       `json:"coordinator_proxy_address,omitempty"`
	ExpectedProxyRuntimeHash       string                       `json:"expected_proxy_runtime_hash,omitempty"`
	ProxyConstructor               *contractProxyConstructor    `json:"proxy_constructor,omitempty"`
	ProxyStorage                   []contractStorageWord        `json:"proxy_storage,omitempty"`
	ReserveBinding                 *contractReserveBinding      `json:"reserve_binding,omitempty"`
	ReserveBindingStorage          []contractStorageWord        `json:"reserve_binding_storage,omitempty"`
	VaultBinding                   *contractVaultBinding        `json:"vault_binding,omitempty"`
	VaultBindingStorage            []contractStorageWord        `json:"vault_binding_storage,omitempty"`
	EvidenceAddress                string                       `json:"validator_evidence_address,omitempty"`
	ExpectedEvidenceRuntimeHash    string                       `json:"expected_evidence_runtime_hash,omitempty"`
	EvidenceConstructor            *contractEvidenceConstructor `json:"evidence_constructor,omitempty"`
	EvidenceStorage                []contractStorageWord        `json:"evidence_storage,omitempty"`
	InstallationComplete           bool                         `json:"installation_complete"`
}

// An unsigned config must explicitly lack a signature; already signed or
// malformed approval material cannot be silently treated as an unsigned draft.
func loadEvmPhasePreview(ctx context.Context, path string) (evmPhasePreview, error) {
	return loadEvmPhasePreviewAction(ctx, path, "reserve-create")
}

// Selecting a prepared vault still reads only draft and release files. Both
// constructor projections are checked without opening future custody paths.
func loadEvmPhasePreviewAction(ctx context.Context, path, actionId string) (evmPhasePreview, error) {
	var result evmPhasePreview
	config, err := readEvmPhaseConfig(ctx, path)
	if err != nil {
		return result, err
	}
	if config.Signature != "" {
		return result, errors.New("contract preview requires an unsigned config; use plan to verify an approval")
	}
	if err := config.validateStructure(); err != nil {
		return result, err
	}
	plan, err := buildEvmCreatePlan(ctx, config, path)
	if err != nil {
		return result, err
	}
	selected, err := selectEvmCreatePlan(ctx, plan, actionId, path)
	if err != nil {
		return result, err
	}
	message, err := config.Plan.signingBytes()
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(message)
	result = evmPhasePreview{Schema: evmPhasePreviewSchema, Plan: config.Plan, PlanHash: config.Plan.hash(), ApprovalPublicKey: config.ApprovalPublicKey, ApprovalSigningMessageHex: hex.EncodeToString(message), ApprovalSigningMessageSha256: "sha256:" + hex.EncodeToString(digest[:]), ExecutableAction: "reserve-create", ReserveAddress: plan.Address.Hex(), ExpectedReserveRuntimeHash: crypto.Keccak256Hash(plan.Runtime).Hex()}
	if selected.ActionIndex > 0 {
		vault := selected
		if selected.ActionIndex >= 2 {
			vault = *selected.Vault
		}
		result.ExecutableAction = actionId
		result.VaultAddress = vault.Address.Hex()
		result.ExpectedVaultRuntimeHash = crypto.Keccak256Hash(vault.Runtime).Hex()
		result.VaultConstructor = vault.VaultConstructor
	}
	if selected.ActionIndex >= 2 {
		coordinator := selected
		if selected.ActionIndex >= 3 {
			coordinator = *selected.Coordinator
		}
		result.CoordinatorAddress = coordinator.Address.Hex()
		result.ExpectedCoordinatorRuntimeHash = crypto.Keccak256Hash(coordinator.Runtime).Hex()
		result.CoordinatorStorage = coordinator.Storage
		result.EscrowRegistration = selected.EscrowRegistration
	}
	if selected.ActionIndex >= 4 {
		proxy := selected
		if selected.ActionIndex >= 5 {
			proxy = *selected.Proxy
		}
		result.ProxyAddress = proxy.Address.Hex()
		result.ExpectedProxyRuntimeHash = crypto.Keccak256Hash(proxy.Runtime).Hex()
		result.ProxyConstructor, result.ProxyStorage = proxy.ProxyConstructor, proxy.Storage
	}
	if selected.ActionIndex >= 5 {
		result.ReserveBinding, result.ReserveBindingStorage = selected.ReserveBinding, selected.Storage
		if selected.ActionIndex >= 6 {
			result.ReserveBindingStorage = selected.ReserveLink.Storage
			result.VaultBinding, result.VaultBindingStorage = selected.VaultBinding, selected.Storage
			if selected.ActionIndex == 7 {
				result.VaultBindingStorage = selected.VaultLink.Storage
			}
		}
	}
	if selected.ActionIndex == 7 {
		result.EvidenceAddress, result.ExpectedEvidenceRuntimeHash = selected.Address.Hex(), crypto.Keccak256Hash(selected.Runtime).Hex()
		result.EvidenceConstructor, result.EvidenceStorage = selected.EvidenceConstructor, selected.Storage
	}
	return result, ctx.Err()
}
