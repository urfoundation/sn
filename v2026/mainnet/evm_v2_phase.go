// The settlement v2 contract phase is a separate five-action graph under one
// independent approval, which may add a sixth action creating the claim
// batcher. It reuses the original phase's envelopes, approval, journals,
// owner and owned receipt adapter; completion reads back all three installed
// contracts at the binding's canonical inclusion.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const evmPhase2Schema = "urnetwork-mainnet-settlement-v2-contract-phase-v1"
const evmPhase2ApprovalSchema = "urnetwork-mainnet-settlement-v2-contract-phase-approval-v1"
const evmPhase2ConfigSchema = "urnetwork-mainnet-settlement-v2-contract-phase-config-v1"
const evmPhase2PreviewSchema = "urnetwork-mainnet-settlement-v2-contract-phase-preview-v1"
const contractReleaseV2Schema = "urnetwork-settlement-v2-contract-release-artifacts-v1"

// Each action owns one journal with its own schema and outcome names.
type evmV2Action struct {
	id, file, schema, name, completed, reverted string
}

// The order is the graph: vault2, its escrow registration at the very next
// nonce, the STSettlement2 implementation, the initialized proxy and the
// one-shot coordinator binding. Vault2's creation reveals the escrow hotkey,
// and anyone who registers it first makes registerEscrow fail for good, so the
// registration directly follows the creation (A7).
var evmV2Actions = []evmV2Action{
	{"vault2-create", "vault2-create.json", "urnetwork-mainnet-evm-vault2-state-v1", "vault2", "vault2-created", "create-reverted-nonce-consumed"},
	{"escrow2-register", "escrow2-register.json", "urnetwork-mainnet-evm-escrow2-state-v1", "vault2 escrow registration", "escrow2-registered", "escrow-registration-reverted-nonce-consumed"},
	{"settlement2-create", "settlement2-create.json", "urnetwork-mainnet-evm-settlement2-state-v1", "settlement2 implementation", "settlement2-created", "create-reverted-nonce-consumed"},
	{"proxy2-create", "proxy2-create.json", "urnetwork-mainnet-evm-proxy2-state-v1", "initialized settlement2 proxy", "proxy2-created-initialized", "create-reverted-nonce-consumed"},
	{"vault2-link", "vault2-link.json", "urnetwork-mainnet-evm-vault2-link-state-v1", "vault2 coordinator binding", "vault2-coordinator-bound", "vault-binding-reverted-nonce-consumed"},
}

// An approval may add one optional sixth action after the binding: the
// argument-free CREATE of the claim batcher (VERSION2-FINAL.md §10.2) at the
// deployer's next nonce. Nothing depends on it, and it changes no other
// action.
var evmV2BatcherAction = evmV2Action{"batcher-create", "batcher-create.json", "urnetwork-mainnet-evm-batcher-state-v1", "claim batcher", "batcher-created", "create-reverted-nonce-consumed"}

// The settlement v2 action at a graph index, the claim batcher's included.
func evmV2ActionAt(index int) evmV2Action {
	if index == len(evmV2Actions) {
		return evmV2BatcherAction
	}
	return evmV2Actions[index]
}

// A settlement v2 graph has its five actions, or six with the claim batcher.
func evmV2GraphSize(actions int) bool {
	return actions == len(evmV2Actions) || actions == len(evmV2Actions)+1
}

// The schema alone selects the phase; the original plan never carries it.
func (self evmPhasePlan) settlementV2() bool {
	return self.Schema == evmPhase2Schema
}

// The approved ids in graph order.
func evmV2ActionIds() []string {
	ids := []string{}
	for _, action := range evmV2Actions {
		ids = append(ids, action.id)
	}
	return ids
}

// The graph index of a settlement v2 action id, the claim batcher's
// included, or -1.
func evmV2ActionIndex(id string) int {
	if id == evmV2BatcherAction.id {
		return len(evmV2Actions)
	}
	return slices.IndexFunc(evmV2Actions, func(action evmV2Action) bool { return action.id == id })
}

// Predecessor projections in graph order: vault2, its escrow registration,
// the implementation, the initialized proxy and the binding.
func (self evmCreatePlan) v2Prior() []*evmCreatePlan {
	return []*evmCreatePlan{self.Vault, self.Escrow, self.Coordinator, self.Proxy, self.VaultLink}
}

// One bootstrap deployer sends all of them at consecutive nonces. The
// CREATEs carry no value; both calls are checked by their own builders.
func evmV2GraphEdges(p evmPhasePlan) error {
	first := p.Actions[0]
	if first.Nonce > ^uint64(0)-uint64(len(p.Actions)) {
		return errors.New("settlement v2 nonces overflow")
	}
	for index, action := range p.Actions {
		create := index == 0 || index == 2 || index == 3 || index == 5
		if action.Sender != first.Sender || action.Nonce != first.Nonce+uint64(index) || create && (action.To != nil || action.ValueWei != "0") {
			return errors.New("settlement v2 actions must be one deployer's consecutive nonces with zero-value CREATEs")
		}
	}
	return nil
}

// Every projection derives from the approved graph and the pinned release in
// one pass, so any action's selection validates the complete graph before a
// first send. Predecessors are shared objects, as in the original graph.
func buildEvmV2Graph(ctx context.Context, config evmPhaseConfig, configPath string) ([]*evmCreatePlan, error) {
	p := config.Plan
	if !p.settlementV2() || !evmV2GraphSize(len(p.Actions)) {
		return nil, errors.New("contract config is not the five-action settlement v2 phase")
	}
	for _, action := range append(slices.Clone(evmV2Actions), evmV2BatcherAction) {
		statePath := filepath.Join(p.RunDirectory, action.file)
		for _, input := range []string{configPath, p.Artifacts.Path} {
			if input == statePath || input == statePath+".lock" {
				return nil, errors.New("contract phase input aliases a settlement v2 journal")
			}
		}
	}
	if err := evmV2GraphEdges(p); err != nil {
		return nil, err
	}
	release, err := loadContractReleaseOf(ctx, p.Artifacts, contractReleaseV2Schema, "SettlementVault2", "Settlement2", "ERC1967Proxy", "ClaimBatcher")
	if err != nil {
		return nil, err
	}
	artifacts := map[string]contractReleaseArtifact{}
	for _, artifact := range release.Artifacts {
		artifacts[artifact.Name] = artifact
	}
	first := p.Actions[0]
	vault := evmCreatePlan{Config: config, ActionIndex: 0, Address: crypto.CreateAddress(first.Sender, first.Nonce)}
	constructor, runtime, err := contractVault2Payload(artifacts["SettlementVault2"], p)
	if err != nil {
		return nil, err
	}
	vault.Runtime, vault.Vault2Constructor = runtime, &constructor
	vault.Getters, vault.Storage = contractVault2State(vault, common.Address{}, false)
	registration, err := contractEscrow2Action(p, vault)
	if err != nil {
		return nil, err
	}
	escrow := evmCreatePlan{Config: config, ActionIndex: 1, Vault: &vault, Vault2Constructor: &constructor, EscrowRegistration: &registration, Address: vault.Address, Runtime: append([]byte(nil), vault.Runtime...)}
	escrow.Getters, escrow.Storage = contractVault2State(vault, common.Address{}, true)
	implementation := evmCreatePlan{Config: config, ActionIndex: 2, Vault: &vault, Escrow: &escrow, Vault2Constructor: &constructor, EscrowRegistration: &registration, Address: crypto.CreateAddress(first.Sender, first.Nonce+2)}
	implementation.Runtime, implementation.Getters, implementation.Storage, err = contractSettlement2Payload(artifacts["Settlement2"], p.Actions[2])
	if err != nil {
		return nil, err
	}
	proxy := evmCreatePlan{Config: config, ActionIndex: 3, Vault: &vault, Escrow: &escrow, Coordinator: &implementation, Vault2Constructor: &constructor, EscrowRegistration: &registration, Address: crypto.CreateAddress(first.Sender, first.Nonce+3)}
	initializer, runtime, getters, storage, err := contractSettlement2ProxyPayload(artifacts["ERC1967Proxy"], p, vault, implementation)
	if err != nil {
		return nil, err
	}
	proxy.Runtime, proxy.Getters, proxy.Storage, proxy.Settlement2Initializer = runtime, getters, storage, &initializer
	binding, err := contractVault2LinkAction(p, vault, proxy)
	if err != nil {
		return nil, err
	}
	link := evmCreatePlan{Config: config, ActionIndex: 4, Vault: &vault, Escrow: &escrow, Coordinator: &implementation, Proxy: &proxy, Vault2Constructor: &constructor, EscrowRegistration: &registration, Settlement2Initializer: &initializer, VaultBinding: &binding, Address: vault.Address, Runtime: append([]byte(nil), vault.Runtime...)}
	link.Getters, link.Storage = contractVault2State(vault, proxy.Address, true)
	plans := []*evmCreatePlan{&vault, &escrow, &implementation, &proxy, &link}
	if len(p.Actions) > len(evmV2Actions) {
		runtime, err := contractClaimBatcherPayload(artifacts["ClaimBatcher"], p.Actions[5])
		if err != nil {
			return nil, err
		}
		// Its readback is its exact runtime: it has no immutables and no state.
		batcher := evmCreatePlan{Config: config, ActionIndex: 5, Vault: &vault, Escrow: &escrow, Coordinator: &implementation, Proxy: &proxy, VaultLink: &link, Vault2Constructor: &constructor, EscrowRegistration: &registration, Settlement2Initializer: &initializer, VaultBinding: &binding, Address: crypto.CreateAddress(first.Sender, first.Nonce+5), Runtime: runtime}
		plans = append(plans, &batcher)
	}
	return plans, ctx.Err()
}

// Only the approved ids select a settlement v2 action.
var errEvmV2Action = errors.New("contract action is not a settlement v2 action")

// Selection names one action of the complete, already validated graph.
func selectEvmV2Plan(ctx context.Context, config evmPhaseConfig, actionId, configPath string) (evmCreatePlan, error) {
	index := evmV2ActionIndex(actionId)
	if index < 0 {
		return evmCreatePlan{}, errEvmV2Action
	}
	plans, err := buildEvmV2Graph(ctx, config, configPath)
	if err != nil {
		return evmCreatePlan{}, err
	}
	if index >= len(plans) {
		return evmCreatePlan{}, errors.New("the approved settlement v2 graph has no claim batcher action")
	}
	selected := *plans[index]
	return selected, selected.validateSelection()
}

// A selection binds one index to its exact approved predecessors in graph
// order and carries exactly the review values its prefix has decoded.
func (self evmCreatePlan) validateV2Selection() error {
	if self.ActionIndex < 0 || self.ActionIndex >= len(self.Config.Plan.Actions) || !evmV2GraphSize(len(self.Config.Plan.Actions)) || self.Reserve != nil || self.ReserveLink != nil || self.VaultConstructor != nil || self.ProxyConstructor != nil || self.ReserveBinding != nil || self.EvidenceConstructor != nil {
		return errors.New("settlement v2 selection differs from its five-action graph")
	}
	hash := rootObjectHash(self.Config)
	for index, prior := range self.v2Prior() {
		if (index < self.ActionIndex) != (prior != nil) || prior != nil && (prior.ActionIndex != index || rootObjectHash(prior.Config) != hash) {
			return errors.New("settlement v2 selection lacks its exact approved predecessors")
		}
	}
	if self.Vault2Constructor == nil || (self.ActionIndex >= 1) != (self.EscrowRegistration != nil) || (self.ActionIndex >= 3) != (self.Settlement2Initializer != nil) || (self.ActionIndex >= 4) != (self.VaultBinding != nil) {
		return errors.New("settlement v2 selection review values differ from its action")
	}
	return nil
}

// Every completed predecessor journal stays locked for the selected action's
// lifetime. Each journal seals the exact completed record before it.
func openEvmV2ActionStores(ctx context.Context, plan evmCreatePlan, create bool) ([]*evmActionStore, error) {
	stores := []*evmActionStore{}
	prior := []evmActionRecord{}
	for index := 0; index <= plan.ActionIndex; index++ {
		current := plan
		if index < plan.ActionIndex {
			current = plan.priorPlan(index)
		}
		store, err := openEvmV2ActionStore(current, prior, create && index == plan.ActionIndex, nil, ctx)
		if err != nil {
			return stores, fmt.Errorf("%s custody: %w", evmV2ActionAt(index).name, err)
		}
		stores = append(stores, store)
		if index < plan.ActionIndex {
			record, err := store.load()
			if err != nil {
				return stores, fmt.Errorf("%s prerequisite: %w", evmV2ActionAt(index).name, err)
			}
			prior = append(prior, record)
		}
	}
	return stores, nil
}

// The selected journal binds the completed predecessor prefix, including the
// cumulative attempts that the one original allowance still bounds.
func openEvmV2ActionStore(plan evmCreatePlan, prior []evmActionRecord, create bool, claimHook func(string) error, storageContexts ...context.Context) (*evmActionStore, error) {
	if err := plan.validateSelection(); err != nil {
		return nil, err
	}
	if !plan.Config.Plan.settlementV2() || len(prior) != plan.ActionIndex {
		return nil, errors.New("settlement v2 custody requires its exact predecessor prefix")
	}
	predecessor := ""
	if plan.ActionIndex > 0 {
		plan.Prerequisites = prior
		predecessor = rootObjectHash(prior[len(prior)-1])
		if err := validateEvmCreatePrerequisite(plan, evmActionRecord{PredecessorHash: predecessor}); err != nil {
			return nil, err
		}
	}
	return openEvmSelectedActionStore(plan.Config, plan.ActionIndex, predecessor, create, claimHook, storageContexts...)
}

// Only vault2's signed journal holds the escrow registration's original
// bytes, exactly the approved second envelope, for its back-to-back send (A7).
func (self evmActionRecord) validateEscrowFollow(p evmPhasePlan, actionIndex int) error {
	if self.EscrowSigned == "" && self.EscrowTransactionHash == "" && self.EscrowAttempts == 0 {
		return nil
	}
	if !p.settlementV2() || actionIndex != 0 || self.Signed == "" {
		return errors.New("only vault2's signed custody holds its back-to-back escrow registration")
	}
	raw, err := rootReceiptHex(self.EscrowSigned, 128*1024)
	if err != nil {
		return err
	}
	tx, err := p.Actions[1].signed(raw)
	if err != nil || tx.Hash().Hex() != self.EscrowTransactionHash {
		return errors.Join(errors.New("retained escrow registration signature differs"), err)
	}
	return nil
}

// Vault2's journal retains the escrow registration's exact signed bytes before
// any send; later imports cannot substitute another signature.
func (self *evmCreateOwner) retainEscrowFollow(record *evmActionRecord, raw []byte) error {
	if !self.plan.Config.Plan.settlementV2() || self.plan.ActionIndex != 0 || record.Signed == "" {
		return errors.New("only vault2's signed custody accepts its back-to-back escrow registration")
	}
	tx, err := self.plan.Config.Plan.Actions[1].signed(raw)
	if err != nil {
		return err
	}
	encoded := "0x" + hex.EncodeToString(raw)
	if record.EscrowSigned != "" && record.EscrowSigned != encoded {
		return errors.New("vault2 custody already owns different escrow registration bytes")
	}
	if record.EscrowSigned != "" {
		return nil
	}
	record.EscrowSigned, record.EscrowTransactionHash = encoded, tx.Hash().Hex()
	return self.retain(record)
}

// Right after vault2's own send, the escrow registration's original bytes go
// out at the next nonce, so nobody can register the revealed escrow hotkey
// in between (A7). A durable attempt precedes the single write, within the
// graph's one allowance.
func (self *evmCreateOwner) submitEscrowFollow(ctx context.Context, plan evmCreatePlan, record *evmActionRecord, prior []evmActionRecord, priorAttempts uint16) (string, error) {
	if uint16(record.Attempts)+uint16(record.EscrowAttempts)+priorAttempts >= uint16(plan.Config.Plan.MaximumAttempts) {
		return "escrow-registration-allowance-exhausted", nil
	}
	if err := self.checkpointCustody(*record, prior); err != nil {
		return "", err
	}
	record.EscrowAttempts++
	if err := self.retain(record); err != nil {
		return "", err
	}
	if err := self.checkpointCustody(*record, prior); err != nil {
		return "", err
	}
	if err := self.chain.submitEscrow(ctx, plan, *record); err != nil {
		return "escrow-registration-submission-uncertain", err
	}
	return "submitted-with-escrow-registration-awaiting-canonical-receipts", nil
}

// Results name the phase's own contracts. Installation completes only with
// the binding's successful receipt, which retention admits after readback;
// the claim batcher's action, which requires it, reports it with its own.
// The escrow collateral is reported where a retained receipt read it back.
// Vault2's creation reveals the escrow hotkey, so vault2's results also carry
// the escrow registration's envelope, the approved action 1, which the
// deployer signs before any send and imports beside vault2's own (A7).
func evmV2CreateResult(plan evmCreatePlan, result evmCreateResult) evmCreateResult {
	plans := append(plan.v2Prior(), &plan)
	plans[plan.ActionIndex] = &plan
	result.Address, result.ExecutableAction, result.RemainingActions = "", evmV2ActionAt(plan.ActionIndex).id, []string{}
	for _, action := range plan.Config.Plan.Actions[plan.ActionIndex+1:] {
		result.RemainingActions = append(result.RemainingActions, action.Id)
	}
	if plan.ActionIndex == 0 {
		result.EscrowSigningDigest, result.EscrowUnsignedTransaction = plan.Config.Plan.Actions[1].envelope()
	}
	result.Vault2Address = plans[0].Address.Hex()
	if plan.ActionIndex >= 2 {
		result.Settlement2ImplementationAddress = plans[2].Address.Hex()
	}
	if plan.ActionIndex >= 3 {
		result.Settlement2Address = plans[3].Address.Hex()
	}
	if plan.ActionIndex == 5 {
		result.ClaimBatcherAddress = plan.Address.Hex()
	}
	if plan.actionIs("escrow2-register", "vault2-link") && result.Receipt != nil && result.Receipt.Status == 1 {
		collateral := contractEscrowCollateralExpected
		result.EscrowCollateral = &collateral
	}
	constructor := *plan.Vault2Constructor
	result.Vault2Constructor = &constructor
	if plan.EscrowRegistration != nil {
		registration := *plan.EscrowRegistration
		result.EscrowRegistration = &registration
	}
	if plan.Settlement2Initializer != nil {
		initializer := *plan.Settlement2Initializer
		initializer.Auditors = append([]common.Address(nil), initializer.Auditors...)
		result.Settlement2Initializer = &initializer
	}
	if plan.VaultBinding != nil {
		binding := *plan.VaultBinding
		result.VaultBinding = &binding
	}
	if result.Receipt != nil {
		result.ReceiptObservation = "retained"
	}
	result.InstallationComplete = plan.ActionIndex >= 4 && result.Receipt != nil && result.Receipt.Status == 1
	return result
}

// The journal names that signed-byte inputs must never alias, the claim
// batcher's included.
func evmV2JournalNames() []string {
	names := []string{}
	for _, action := range evmV2Actions {
		names = append(names, action.file)
	}
	return append(names, evmV2BatcherAction.file)
}

// The exact readback expectations of one installed contract at completion.
type evmV2PreviewContract struct {
	Address     string                `json:"address"`
	RuntimeHash string                `json:"runtime_hash"`
	Getters     []contractGetter      `json:"getters"`
	Storage     []contractStorageWord `json:"storage"`
}

// The reviewer sees every predicted identity, decoded value and the complete
// completion readback before signing. Selection changes only the named action.
type evmV2PhasePreview struct {
	Schema                       string                          `json:"schema"`
	Plan                         evmPhasePlan                    `json:"plan"`
	PlanHash                     string                          `json:"plan_hash"`
	ApprovalPublicKey            string                          `json:"approval_public_key_ed25519"`
	ApprovalSigningMessageHex    string                          `json:"approval_signing_message_hex"`
	ApprovalSigningMessageSha256 string                          `json:"approval_signing_message_sha256"`
	ApprovalVerified             bool                            `json:"approval_verified"`
	ExecutableAction             string                          `json:"executable_action"`
	Vault2                       evmV2PreviewContract            `json:"vault2"`
	Settlement2Implementation    evmV2PreviewContract            `json:"settlement2_implementation"`
	Settlement2                  evmV2PreviewContract            `json:"settlement2"`
	ClaimBatcher                 *evmV2PreviewContract           `json:"claim_batcher,omitempty"`
	Vault2Constructor            *contractVault2Constructor      `json:"vault2_constructor"`
	EscrowRegistration           *contractEscrowRegistration     `json:"escrow_registration"`
	Settlement2Initializer       *contractSettlement2Initializer `json:"settlement2_initializer"`
	VaultBinding                 *contractVaultBinding           `json:"vault_binding"`
	InstallationComplete         bool                            `json:"installation_complete"`
}

// Unsigned review reads only the draft and release files: no key, journal,
// lock or route. A supplied signature belongs to plan, not preview.
func loadEvmV2PhasePreview(ctx context.Context, path, actionId string) (evmV2PhasePreview, error) {
	var result evmV2PhasePreview
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
	if evmV2ActionIndex(actionId) < 0 {
		return result, errEvmV2Action
	}
	plans, err := buildEvmV2Graph(ctx, config, path)
	if err != nil {
		return result, err
	}
	if evmV2ActionIndex(actionId) >= len(plans) {
		return result, errors.New("the approved settlement v2 graph has no claim batcher action")
	}
	message, _ := config.Plan.signingBytes()
	digest := sha256.Sum256(message)
	contract := func(plan *evmCreatePlan) evmV2PreviewContract {
		return evmV2PreviewContract{Address: plan.Address.Hex(), RuntimeHash: crypto.Keccak256Hash(plan.Runtime).Hex(), Getters: plan.Getters, Storage: plan.Storage}
	}
	link := plans[4]
	result = evmV2PhasePreview{Schema: evmPhase2PreviewSchema, Plan: config.Plan, PlanHash: config.Plan.hash(), ApprovalPublicKey: config.ApprovalPublicKey, ApprovalSigningMessageHex: hex.EncodeToString(message), ApprovalSigningMessageSha256: "sha256:" + hex.EncodeToString(digest[:]), ExecutableAction: actionId,
		Vault2: contract(link), Settlement2Implementation: contract(plans[2]), Settlement2: contract(plans[3]), Vault2Constructor: link.Vault2Constructor, EscrowRegistration: link.EscrowRegistration, Settlement2Initializer: link.Settlement2Initializer, VaultBinding: link.VaultBinding}
	if len(plans) > len(evmV2Actions) {
		batcher := contract(plans[5])
		result.ClaimBatcher = &batcher
	}
	return result, ctx.Err()
}
