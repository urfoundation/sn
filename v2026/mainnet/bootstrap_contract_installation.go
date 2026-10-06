// Installation readback joins original CREATE custody and counted anchor
// recovery to complete finalized storage. Reports never authorize service start.
package main

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

const bootstrapContractInstallationSchema = "urnetwork-mainnet-bootstrap-contract-installation-v1"

// This stable identity binds original authority and the terminal anchor journal.
// Later observation heads cannot renew a nonce, budget or approved policy.
type bootstrapContractInstallationIdentity struct {
	BootstrapPlanHash       string                             `json:"bootstrap_plan_hash"`
	ContractPlanHash        string                             `json:"contract_plan_hash"`
	ExecutionPlanHash       string                             `json:"execution_plan_hash"`
	ExecutionApprovalHash   string                             `json:"execution_approval_hash"`
	CanonicalAuthorityHash  string                             `json:"canonical_authority_hash"`
	RuntimeRevisionHash     string                             `json:"runtime_revision_hash,omitempty"`
	SafeCurrentRevisionHash string                             `json:"safe_current_revision_hash,omitempty"`
	OriginalCustodyHashes   []string                           `json:"original_custody_hashes"`
	AnchorEventHash         string                             `json:"anchor_event_hash"`
	AnchorReceipt           bootstrapSuccessorExecutionReceipt `json:"anchor_receipt"`
}

// The complete historical storage proof describes initial installation. Current
// account views allow normal service accounting, while the Safe proof excludes
// hidden authority. Current-only policy never becomes a history attestation.
type bootstrapContractInstallation struct {
	Schema                   string                                   `json:"schema"`
	Identity                 bootstrapContractInstallationIdentity    `json:"identity"`
	InstallationIdentityHash string                                   `json:"installation_identity_hash"`
	Snapshot                 finalizedMappingEnvelope                 `json:"snapshot"`
	InitialContracts         *bootstrapContractInstallationStorage    `json:"initial_contracts"`
	InitialSafe              *safeCurrentStorageObservation           `json:"initial_safe"`
	CurrentSafe              *safeCurrentStorageObservation           `json:"current_safe"`
	CurrentSafeNonce         string                                   `json:"current_safe_nonce"`
	CurrentContracts         []validatorActivationContractObservation `json:"current_contracts"`
	AuthorityPolicy          string                                   `json:"authority_policy"`
	CurrentRuntimeRevision   string                                   `json:"current_runtime_revision_hash,omitempty"`
	CurrentPolicyRevision    string                                   `json:"current_policy_revision_hash,omitempty"`
	CheckedThroughNativeHash string                                   `json:"checked_through_native_hash"`
	CheckedThroughNative     uint64                                   `json:"checked_through_native_number"`
	CompleteSafeHistory      bool                                     `json:"complete_safe_history_verified"`
	CompleteContractHistory  bool                                     `json:"complete_contract_history_verified"`
	CompletePendingState     bool                                     `json:"complete_pending_state_verified"`
	InstallationComplete     bool                                     `json:"installation_complete"`
	ActivationReady          bool                                     `json:"activation_ready"`
	NetworkEffects           bool                                     `json:"network_effects"`
	ContentHash              string                                   `json:"content_hash"`
}

// The convenience command chooses a current finalized identity. Service
// consumers use At to join their own independently checked observation boundary.
func inspectBootstrapContractInstallation(ctx context.Context, owner *bootstrapSuccessorExecutionStore, chain *bootstrapSuccessorCanonicalChain) (bootstrapContractInstallation, error) {
	if ctx == nil || owner == nil || chain == nil || chain.owner != owner {
		return bootstrapContractInstallation{}, errors.New("installation readback requires retained canonical ownership")
	}
	if err := chain.checkpoint(ctx, owner.planCopy()); err != nil {
		return bootstrapContractInstallation{}, err
	}
	head, err := chain.identity(ctx, owner.planCopy())
	if err != nil {
		return bootstrapContractInstallation{}, err
	}
	return inspectBootstrapContractInstallationAt(ctx, owner, chain, head)
}

// Every invocation reauthenticates receipts, exact local journal bytes and the
// selected policy. Recovery may finish its original terminal event, never send.
func inspectBootstrapContractInstallationAt(ctx context.Context, owner *bootstrapSuccessorExecutionStore, chain *bootstrapSuccessorCanonicalChain, head chainIdentity) (bootstrapContractInstallation, error) {
	var result bootstrapContractInstallation
	if ctx == nil || owner == nil || chain == nil || chain.owner != owner {
		return result, errors.New("installation readback lacks its exact retained owner")
	}
	plan := owner.planCopy()
	if err := chain.checkpoint(ctx, plan); err != nil {
		return result, err
	}
	if chain.currentPolicy == nil && chain.provenance == nil {
		return result, errBootstrapSuccessorSafeProvenanceUnavailable
	}
	if chain.currentPolicy != nil {
		if err := chain.currentPolicyReady(ctx, plan, chain.currentRevisionHash); err != nil {
			return result, err
		}
	}
	completion, err := advanceBootstrapSuccessorExecution(ctx, owner, chain, false)
	if err != nil || !completion.InstallationComplete || owner.last.Phase != "installed" || owner.last.Receipt == nil ||
		owner.last.CanonicalAuthorityHash != owner.canonicalAuthorityHash || owner.runtimeHistory.pendingHash != "" || owner.safeCurrentHistory.pendingHash != "" {
		return result, errors.Join(errors.New("installation readback lacks a counted canonical evidence anchor"), err)
	}
	anchor := *owner.last.Receipt
	anchorProfiles := []rootReceiptProfile{chain.approval.Authorization.CurrentRuntime}
	if owner.last.RuntimeRevisionHash != "" {
		found := false
		for _, revision := range owner.runtimeHistory.approvals {
			anchorProfiles = append(anchorProfiles, revision.Authorization.Runtime)
			if rootObjectHash(revision) == owner.last.RuntimeRevisionHash {
				found = true
				break
			}
		}
		if !found {
			return result, errors.New("installation anchor lost its original runtime authority prefix")
		}
	}
	if head.FinalizedNumber < anchor.NativeNumber {
		return result, errors.New("installation readback snapshot precedes its anchor")
	}
	if err := chain.currentRuntime(ctx, head); err != nil {
		return result, err
	}
	mapping, err := chain.chain.client.readFinalizedMappingAtIdentity(ctx, head)
	if err != nil || mapping.EvmHeader.Number < anchor.Receipt.BlockNumber {
		return result, errors.Join(errors.New("installation readback mapping precedes its anchor"), err)
	}
	anchorHead, err := chain.chain.client.readIdentityAt(ctx, anchor.NativeHash.Hex())
	if err != nil || anchorHead.FinalizedNumber != anchor.NativeNumber {
		return result, errors.Join(errors.New("installation readback anchor identity differs"), err)
	}
	accounts, err := bootstrapContractInstallationAccounts(ctx, chain.plans, chain.records)
	if err != nil {
		return result, err
	}
	// The first inclusion's proof must use its independently retained runtime,
	// including after a later approved upgrade. Current config cannot backdate it.
	profileAt := func(identity chainIdentity, profiles []rootReceiptProfile) (rootReceiptProfile, error) {
		runtime, err := chain.chain.client.readRuntimeSnapshotAtIdentity(ctx, identity)
		if err != nil {
			return rootReceiptProfile{}, err
		}
		for _, profile := range profiles {
			if profile.RuntimeVersion == runtime.Version && profile.RuntimeCodeHash == runtime.CodeHash && profile.RuntimeMetadataHash == runtime.MetadataHash {
				return profile, nil
			}
		}
		return rootReceiptProfile{}, errors.New("installation proof has no retained runtime authority")
	}
	anchorRuntime, err := profileAt(anchorHead, anchorProfiles)
	if err != nil {
		return result, err
	}
	witness := safeCurrentStorageWitness{At: anchorHead.FinalizedHash}
	if err := chain.chain.client.call(ctx, "chain_getHeader", []any{anchorHead.FinalizedHash}, &witness.Header); err != nil {
		return result, err
	}
	var proof struct {
		At    string   `json:"at"`
		Proof []string `json:"proof"`
	}
	if err := chain.chain.client.callAdmittedRead(ctx, "state_getReadProof", []any{bootstrapContractInstallationStorageKeys(accounts), anchorHead.FinalizedHash}, &proof, false, 2*maximumSafeCurrentProofBytes+maxRpcReplyBytes); err != nil {
		return result, err
	}
	witness.At, witness.Nodes = proof.At, proof.Proof
	initialContracts, err := verifyBootstrapContractInstallationStorage(ctx, accounts, anchorRuntime, anchorHead, witness)
	if err != nil {
		return result, err
	}
	nonce := new(big.Int).Add(plan.transaction().Nonce, big.NewInt(1))
	nonce.Mod(nonce, new(big.Int).Lsh(big.NewInt(1), 256))
	scope := safeCurrentStorageScope{Safe: plan.Review.Transaction.Safe, Singleton: plan.Request.Singleton, Owners: plan.Request.Owners, Nonce: nonce.String(),
		Version: plan.Review.Request.Version, Variant: plan.Review.Request.Variant, Runtime: anchorRuntime}
	pin, err := loadSafeReleasePin(scope.Version, scope.Variant)
	if err != nil {
		return result, err
	}
	for _, artifact := range pin.Artifacts {
		if artifact.Name == "SafeProxy" {
			scope.SafeProxyRuntimeHash = common.HexToHash(artifact.RuntimeKeccak256)
		}
		if artifact.Name == scope.Variant {
			scope.SingletonRuntimeHash = common.HexToHash(artifact.RuntimeKeccak256)
		}
	}
	initialSafe, err := chain.chain.client.readSafeCurrentStorage(ctx, scope, anchorHead)
	if err != nil {
		return result, err
	}
	block := map[string]any{"blockHash": mapping.EvmHeader.Hash, "requireCanonical": true}
	safe, err := chain.safeState(ctx, plan, block)
	if err != nil {
		return result, err
	}
	currentNonce, err := evmWei(safe.SafeNonce)
	if err != nil || currentNonce.Cmp(nonce) < 0 {
		return result, errors.Join(errors.New("installation current Safe nonce precedes the included anchor"), err)
	}
	// A later observed nonce is readback, not authority for another operation.
	// Every other permitted word stays exact; history remains explicitly unknown.
	scope.Nonce, scope.Runtime = safe.SafeNonce, rootReceiptProfile{}
	scope.Runtime, err = profileAt(head, chain.runtimeProfiles)
	if err != nil {
		return result, err
	}
	if chain.currentPolicy != nil && scope.Runtime != chain.currentPolicy.scope.Runtime {
		return result, errors.New("installation current proof differs from selected policy runtime")
	}
	currentSafe, err := chain.chain.client.readSafeCurrentStorage(ctx, scope, head)
	if err != nil {
		return result, err
	}
	currentContracts, err := chain.chain.client.observeBootstrapContractInstallationContracts(ctx, chain.plans, chain.records, mapping)
	if err != nil {
		return result, err
	}
	policy := bootstrapSuccessorSafeProvenancePolicy
	if chain.currentPolicy != nil {
		policy = owner.safeCurrentHistory.approvals[len(owner.safeCurrentHistory.approvals)-1].Authorization.Policy
	} else if err := chain.authenticateProvenance(ctx, plan, head); err != nil {
		return result, err
	}
	check, err := chain.chain.client.readFinalizedMappingAtIdentity(ctx, head)
	if err != nil || rootObjectHash(check) != rootObjectHash(mapping) {
		return result, errors.Join(errors.New("installation current mapping changed"), err)
	}
	latest, err := chain.identity(ctx, plan)
	if err != nil {
		return result, err
	}
	for _, point := range []evmActionRecord{{ScanNumber: head.FinalizedNumber, ScanHash: head.FinalizedHash}, {ScanNumber: anchor.NativeNumber, ScanHash: anchor.NativeHash.Hex()}} {
		if err := chain.chain.continuity(ctx, chain.plans[0].Config.Plan, point, latest); err != nil {
			return result, err
		}
	}
	if err := chain.checkpoint(ctx, plan); err != nil {
		return result, err
	}
	snapshot, err := sealFinalizedMapping(mapping)
	if err != nil {
		return result, err
	}
	p := plan.Review.Preparation.Approval.Plan
	identity := bootstrapContractInstallationIdentity{BootstrapPlanHash: p.Proposal.Request.BootstrapPlanHash, ContractPlanHash: chain.plans[0].Config.Plan.hash(),
		ExecutionPlanHash: plan.hash(), ExecutionApprovalHash: rootObjectHash(owner.approval), CanonicalAuthorityHash: owner.last.CanonicalAuthorityHash,
		RuntimeRevisionHash: owner.last.RuntimeRevisionHash, SafeCurrentRevisionHash: owner.last.SafeCurrentRevisionHash, AnchorEventHash: owner.last.ContentHash, AnchorReceipt: anchor}
	for _, record := range chain.records {
		identity.OriginalCustodyHashes = append(identity.OriginalCustodyHashes, rootObjectHash(record))
	}
	result = bootstrapContractInstallation{Schema: bootstrapContractInstallationSchema, Identity: identity, InstallationIdentityHash: rootObjectHash(identity), Snapshot: snapshot,
		InitialContracts: initialContracts, InitialSafe: initialSafe, CurrentSafe: currentSafe, CurrentSafeNonce: safe.SafeNonce, CurrentContracts: currentContracts,
		AuthorityPolicy: policy, CurrentRuntimeRevision: owner.runtimeHistory.hash(), CurrentPolicyRevision: owner.safeCurrentHistory.hash(),
		CheckedThroughNativeHash: latest.FinalizedHash, CheckedThroughNative: latest.FinalizedNumber, CompleteSafeHistory: chain.currentPolicy == nil,
		InstallationComplete: true}
	result.ContentHash = rootObjectHash(result)
	return result, nil
}

// Only the reauthenticated original proxy receipt fixes epoch zero. Neither the
// constructor's placeholder, anchor block nor a later observed clock can renew it.
func (self *rpcClient) observeBootstrapContractInstallationContracts(ctx context.Context, plans []evmCreatePlan, records []evmActionRecord, mapping finalizedMapping) ([]validatorActivationContractObservation, error) {
	if len(plans) != 8 || len(records) != 8 || records[4].Receipt == nil || records[4].Receipt.BlockNumber == 0 ||
		records[4].Receipt.ContractAddress != plans[4].Address.Hex() || records[4].Receipt.BlockNumber > mapping.EvmHeader.Number {
		return nil, errors.New("installation current policy lacks its original proxy inclusion")
	}
	return self.observeValidatorActivationContractsWithPolicyBlock(ctx, plans, mapping, records[4].Receipt.BlockNumber)
}
