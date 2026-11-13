// Production current admission reconstructs exact original execution custody.
// It owns readback only: this adapter never requests a signature or submission.
package main

import (
	"context"
	"errors"
	"reflect"
	"time"
)

// Reconstruct before the first network operation; all already retained approvals
// must match. Importing new runtime or Safe authority is a separate command.
func newValidatorActivationCurrentAuthority(ctx context.Context, activation validatorActivationApproval, activationKey string, reference planFileReference, key string) (_ *validatorActivationCurrentAuthority, resultErr error) {
	raw, hash, err := readPlanFile(ctx, reference.Path, 64*1024)
	var approved validatorActivationCurrentApproval
	if err == nil && hash == reference.Sha256 {
		err = decodePlanJson(raw, &approved)
	} else {
		err = errors.Join(errors.New("validator current approval file pin differs"), err)
	}
	if err != nil {
		return nil, err
	}
	preparation, err := loadValidatorActivationPreparation(ctx, activation)
	if err != nil {
		return nil, err
	}
	if err := approved.validate(activation, activationKey, key, preparation); err != nil {
		return nil, err
	}
	for _, file := range approved.Authorization.references() {
		raw, hash, err := readPlanFile(ctx, file.Path, 1024*1024)
		if err != nil || len(raw) == 0 || hash != file.Sha256 {
			return nil, errors.Join(errors.New("validator current original input pin differs"), err)
		}
	}
	p := approved.Authorization.Installation
	plan, profile, retained, err := loadBootstrapSuccessorExecution(ctx, activation.Plan.Preparation.Path, preparation.Plan.Config.RunDirectory, activation.Plan.PlanHash,
		p.Request.Path, p.SafeRequest.Path, p.ExecutionRequest.Path, p.ExecutionApproval.Path, p.CanonicalApproval.Path, approved.Authorization.CustodyEvidence.Path, reference.Path)
	if err != nil {
		return nil, err
	}
	var owner *bootstrapSuccessorExecutionStore
	var chain *bootstrapSuccessorCanonicalChain
	close := func() error { return errors.Join(chain.close(), owner.close(), retained.close()) }
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, close())
		}
	}()
	if plan.hash() != p.ExecutionPlanHash {
		return nil, errors.New("validator current installation differs from original execution plan")
	}
	raw, hash, err = readBootstrapRootFile(ctx, p.ExecutionApproval.Path, maximumBootstrapSuccessorExecutionBytes)
	var execution bootstrapSuccessorExecutionApproval
	if err == nil && hash == p.ExecutionApproval.Sha256 {
		err = decodePlanJson(raw, &execution)
	} else {
		err = errors.Join(errors.New("validator current execution approval changed"), err)
	}
	if err != nil {
		return nil, err
	}
	owner, err = openBootstrapSuccessorExecutionStore(ctx, plan, execution, profile, false, nil)
	if err != nil {
		return nil, err
	}
	raw, hash, err = readBootstrapRootFile(ctx, p.CanonicalApproval.Path, 16*1024)
	var canonical bootstrapSuccessorCanonicalApproval
	if err == nil && hash == p.CanonicalApproval.Sha256 {
		err = decodePlanJson(raw, &canonical)
	} else {
		err = errors.Join(errors.New("validator current canonical approval changed"), err)
	}
	if err != nil {
		return nil, err
	}
	if owner.canonicalAuthority == nil || owner.canonicalAuthorityHash != rootObjectHash(canonical) || owner.safeCurrentHistory.hash() != p.AcceptedSafePolicy || owner.safeCurrentHistory.pendingHash != "" || owner.runtimeHistory.pendingHash != "" {
		return nil, errors.New("validator current admission requires exact already retained canonical and current-only policy authority")
	}
	if err := retained.checkpoint(ctx); err != nil {
		return nil, err
	}
	chain, err = newBootstrapSuccessorCanonicalChainWithAuthorities(ctx, owner, canonical, nil, bootstrapSuccessorSafeCurrentPublicRoute, nil)
	if err != nil {
		return nil, err
	}
	self := &validatorActivationCurrentAuthority{retained: validatorActivationCurrentRetained{Approval: approved, PublicKey: key, Reference: reference}, close: close}
	self.custody = func(ctx context.Context) error {
		return errors.Join(retained.checkpoint(ctx), chain.checkpoint(ctx, owner.planCopy()))
	}
	self.observe = func(ctx context.Context, store *validatorActivationStore, host *validatorActivationHost, record *validatorActivationRecord, now func() time.Time, pending int) (*validatorActivationReadiness, error) {
		return observeValidatorActivationHealthPending(ctx, store, host, record, now, true, pending)
	}
	self.installation = func(ctx context.Context, readiness *validatorActivationReadiness) (validatorActivationInstallationObservation, error) {
		var result validatorActivationInstallationObservation
		if readiness == nil || readiness.Production == nil || owner.safeCurrentHistory.hash() != p.AcceptedSafePolicy {
			return result, errors.New("validator current installation lacks current original scope")
		}
		declaration, err := loadBootstrapContractRolePlan(ctx, activation.Plan.Preparation.Path)
		if err != nil || declaration.PreparationHash != activation.Plan.PlanHash || declaration.ContractPlanHash != readiness.Production.ContractPlanHash {
			return result, errors.Join(errors.New("validator current producer roles differ from the original installed graph"), err)
		}
		earliest, err := bootstrapContractReceiptScanFloor(declaration.Validators, chain.records)
		if err != nil {
			return result, err
		}
		head, err := chain.chain.client.readIdentityAt(ctx, readiness.FinalizedHash)
		if err != nil || head.FinalizedNumber != readiness.FinalizedNumber {
			return result, errors.Join(errors.New("validator current installation native point differs"), err)
		}
		observed, err := inspectBootstrapContractInstallationAt(ctx, owner, chain, head)
		if err != nil {
			return result, err
		}
		mapping := observed.Snapshot.Mapping
		if !observed.InstallationComplete || observed.ActivationReady || observed.NetworkEffects || observed.InstallationIdentityHash != p.InstallationHash ||
			observed.Identity.BootstrapPlanHash != activation.Plan.PlanHash || observed.Identity.ExecutionPlanHash != p.ExecutionPlanHash || observed.CurrentPolicyRevision != p.AcceptedSafePolicy ||
			observed.Identity.ContractPlanHash != readiness.Production.ContractPlanHash || rootObjectHash(mapping) != readiness.Production.MappingHash ||
			!reflect.DeepEqual(observed.CurrentContracts, readiness.Production.Contracts) || observed.InitialContracts == nil || !observed.InitialContracts.CompleteStorage || observed.InitialSafe == nil || observed.CurrentSafe == nil {
			return result, errors.New("validator current installation anchor, policy, complete storage or current domain differs")
		}
		return validatorActivationInstallationObservation{InstallationHash: observed.InstallationIdentityHash, PreparationHash: observed.Identity.BootstrapPlanHash,
			ContractPlanHash: observed.Identity.ContractPlanHash, ObservationHash: observed.ContentHash, NativeNumber: mapping.Identity.FinalizedNumber, NativeHash: mapping.Identity.FinalizedHash,
			EvmNumber: mapping.EvmHeader.Number, EvmHash: mapping.EvmHeader.Hash, EarliestOriginalEvmBlock: earliest,
			DeclaredScanFloors: [2]uint64{declaration.Validators[0].DeclaredDeployBlock, declaration.Validators[1].DeclaredDeployBlock}}, nil
	}
	return self, self.custody(ctx)
}
