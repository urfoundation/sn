// A distinct signed domain prepares one local successor claim without admitting
// Safe signatures, relayer nonces, transaction spending or chain execution.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const bootstrapSuccessorPreparationSchema = "urnetwork-mainnet-successor-preparation-v1"
const bootstrapSuccessorPreparationApprovalSchema = "urnetwork-mainnet-successor-preparation-approval-v1"
const bootstrapSuccessorPreparationEnvelopeSchema = "urnetwork-mainnet-successor-preparation-envelope-v1"
const bootstrapSuccessorPreparationStateSchema = "urnetwork-mainnet-successor-preparation-state-v1"
const bootstrapSuccessorPreparationFile = "contract-successor-preparation.json"
const maximumBootstrapSuccessorPreparationBytes = 256 * 1024

// Physical identity prevents a copied or replaced local directory from acquiring
// the same approval. It is not a cross-host key or distributed custody fence.
type bootstrapSuccessorRootIdentity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

// The original contract approver may sign this separate local preparation only.
// Exact source references and retained proposal seals are rechecked on every use.
type bootstrapSuccessorPreparationPlan struct {
	Schema            string                             `json:"schema"`
	OriginalConfig    planFileReference                  `json:"original_config"`
	Request           planFileReference                  `json:"request"`
	ApprovalPublicKey string                             `json:"approval_public_key_ed25519"`
	Root              bootstrapSuccessorRootIdentity     `json:"physical_root"`
	Proposal          bootstrapContractSuccessorProposal `json:"retained_proposal"`
}

// Signing bytes cover only this preparation domain; original phase signatures,
// Safe approvals and transaction signatures are never interchangeable with it.
type bootstrapSuccessorPreparationApproval struct {
	Schema    string                            `json:"schema"`
	Plan      bootstrapSuccessorPreparationPlan `json:"plan"`
	Signature string                            `json:"signature_ed25519"`
}

// The new record has no mutable send counter or transaction signature field.
// Its sole phase is local preparation; original journals remain authoritative.
type bootstrapSuccessorPreparationRecord struct {
	Schema      string                                `json:"schema"`
	Approval    bootstrapSuccessorPreparationApproval `json:"approval"`
	Phase       string                                `json:"phase"`
	ContentHash string                                `json:"content_hash"`
}

// Inspect a private physical directory without choosing or creating a new one.
func bootstrapSuccessorPhysicalRoot(path string) (bootstrapSuccessorRootIdentity, error) {
	if err := bootstrapRootDirectory(path); err != nil {
		return bootstrapSuccessorRootIdentity{}, err
	}
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return bootstrapSuccessorRootIdentity{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0077 != 0 || stat.Ino == 0 {
		return bootstrapSuccessorRootIdentity{}, errors.New("successor physical root is not a private directory")
	}
	return bootstrapSuccessorRootIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

// Bounded structural admission does not treat retained receipt claims as fresh
// canonical state. The public owner additionally rebuilds the complete proposal.
func (self bootstrapSuccessorPreparationPlan) validate() error {
	p := self.Proposal
	if self.Schema != bootstrapSuccessorPreparationSchema || !bootstrapRootAbsolutePath(self.OriginalConfig.Path) ||
		!planSha256(self.OriginalConfig.Sha256) || !bootstrapRootAbsolutePath(self.Request.Path) || !planSha256(self.Request.Sha256) ||
		!rootCanonicalHash(self.ApprovalPublicKey) || self.Root.Inode == 0 || !bootstrapRootAbsolutePath(p.OriginalRunDirectory) ||
		p.Schema != bootstrapContractSuccessorProposalSchema || p.Status != "unsigned-proposal-prerequisites-unresolved" ||
		p.RequestSha256 != self.Request.Sha256 || p.LocalPreparation == nil || len(p.AdoptedActions) != 8 ||
		len(p.UnfinishedActions) != 1 || p.UnfinishedActions[0] != "evidence-anchor" || p.Budget.UnfinishedActionCount != 1 ||
		p.ApprovalVerified || p.ApprovalSigningPayloadProvided || p.Executable || p.CurrentChainVerified || p.SafeAuthorityVerified ||
		p.NetworkEffects || p.InstallationComplete || p.ActivationReady {
		return errors.New("successor preparation exceeds its exact retained offline scope")
	}
	claimed := p.ContentHash
	p.ContentHash = ""
	if !planSha256(claimed) || rootObjectHash(p) != claimed {
		return errors.New("successor proposal seal differs")
	}
	retainedAttempts := uint16(0)
	for i, action := range p.AdoptedActions {
		if action.CustodyStatus != "retained-complete" || action.ReceiptObservation != "retained" || action.Receipt == nil || action.Receipt.Status != 1 ||
			!planSha256(action.JournalHash) || !planSha256(action.CustodyHash) || !rootCanonicalHash(action.TransactionHash) || action.Attempts == 0 ||
			action.Id != []string{"reserve-create", "vault-create", "coordinator-create", "escrow-register", "proxy-create", "reserve-link", "vault-link", "evidence-create"}[i] {
			return errors.New("successor preparation lacks eight complete retained actions")
		}
		retainedAttempts += uint16(action.Attempts)
	}
	for _, seal := range []string{p.OriginalConfigHash, p.Request.BootstrapPlanHash, p.Request.OriginalContractPlanHash,
		p.LocalPreparation.PreparationHash, p.LocalPreparation.ContractsHash} {
		if !planSha256(seal) {
			return errors.New("successor preparation lacks complete original custody seals")
		}
	}
	if !p.LocalPreparation.validRootSeals() {
		return errors.New("successor preparation lacks complete original root seals")
	}
	budget := p.Budget
	original, originalErr := evmWei(budget.OriginalMaximumWei)
	additional, additionalErr := evmWei(budget.AdditionalMaximumWei)
	lifetime, lifetimeErr := evmWei(budget.ProposedMaximumLifetimeWei)
	completed, completedErr := evmWei(budget.CompletedEnvelopeReservationWei)
	unexecuted, unexecutedErr := evmWei(budget.UnexecutedEnvelopeReservationWei)
	if err := errors.Join(originalErr, additionalErr, lifetimeErr, completedErr, unexecutedErr); err != nil {
		return err
	}
	if budget.OriginalMaximumAttempts == 0 || budget.OriginalMaximumAttempts > 8 || budget.RetainedAttempts > uint16(budget.OriginalMaximumAttempts) ||
		budget.RetainedAttempts != retainedAttempts || budget.AdditionalMaximumAttempts != p.Request.AdditionalMaximumAttempts || budget.AdditionalMaximumWei != p.Request.AdditionalMaximumWei ||
		budget.AdditionalMaximumAttempts == 0 || budget.AdditionalMaximumAttempts > 16 ||
		budget.ProposedMaximumCumulativeAttempts != uint16(budget.OriginalMaximumAttempts)+uint16(budget.AdditionalMaximumAttempts) ||
		budget.ProposedRemainingAttempts != budget.ProposedMaximumCumulativeAttempts-budget.RetainedAttempts ||
		budget.ProposedRemainingAttempts == 0 || budget.RetryMarginAttempts != budget.ProposedRemainingAttempts-1 ||
		completed.Sign() <= 0 || completed.Add(completed, unexecuted).Cmp(original) > 0 ||
		additional.Sign() <= 0 || original.Add(original, additional).BitLen() > 256 || original.Cmp(lifetime) != 0 {
		return errors.New("successor preparation does not conserve original additive budget floors")
	}
	return nil
}

// No approval payload is produced without complete bounded plan admission.
func (self bootstrapSuccessorPreparationPlan) signingBytes() ([]byte, error) {
	if err := self.validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > maximumBootstrapSuccessorPreparationBytes {
		return nil, errors.Join(errors.New("successor preparation exceeds its byte bound"), err)
	}
	return append([]byte(bootstrapSuccessorPreparationApprovalSchema+"\x00"), raw...), nil
}

// Stable content identity excludes the signature but includes the entire domain.
func (self bootstrapSuccessorPreparationPlan) hash() string {
	raw, _ := self.signingBytes()
	return rootObjectHash(struct {
		Domain string `json:"domain"`
		Bytes  []byte `json:"bytes"`
	}{Domain: bootstrapSuccessorPreparationSchema, Bytes: raw})
}

// The expected key and freshly rebuilt plan come from the original accepted
// preparation, never from the successor envelope or retained child journal.
func (self bootstrapSuccessorPreparationApproval) validate(expected bootstrapSuccessorPreparationPlan) error {
	if self.Schema != bootstrapSuccessorPreparationEnvelopeSchema || rootObjectHash(self.Plan) != rootObjectHash(expected) {
		return errors.New("successor approval differs from original custody and physical scope")
	}
	message, err := expected.signingBytes()
	if err != nil {
		return err
	}
	key, keyErr := rootReceiptHex(expected.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if keyErr != nil || signatureErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.New("successor preparation independent domain approval is invalid")
	}
	return nil
}

// Hold the five original shared locks while callers claim or reopen the child.
// In particular, the reserve lock also fences every cooperating action writer.
func loadBootstrapSuccessorPreparation(ctx context.Context, configPath, runDirectory, accepted, requestPath, approvalPath string, additionalPaths ...string) (_ bootstrapSuccessorPreparationPlan, _ *bootstrapChainReadinessState, resultErr error) {
	var plan bootstrapSuccessorPreparationPlan
	preparation, err := loadBootstrapChainPreparation(ctx, configPath)
	if err != nil {
		return plan, nil, err
	}
	if !bootstrapChainHasRootRole(preparation.Plan.Config.Schema) || accepted != preparation.Plan.ContentHash || runDirectory != preparation.Plan.Config.RunDirectory {
		return plan, nil, errors.New("successor preparation requires exact original accepted v3 custody")
	}
	raw, requestHash, err := readBootstrapRootFile(ctx, requestPath, 16*1024)
	var request bootstrapContractSuccessorRequest
	if err == nil {
		err = decodePlanJson(raw, &request)
	}
	if err != nil || request.BootstrapPlanHash != accepted {
		return plan, nil, errors.Join(errors.New("successor request differs from original preparation"), err)
	}
	_, plans, err := prepareBootstrapContractReadiness(ctx, preparation.Contracts, preparation.Plan.Config.Contracts.Path)
	if err == nil {
		err = validateBootstrapSuccessorPreparationPaths(preparation, plans, requestPath, approvalPath, additionalPaths...)
	}
	if err != nil {
		return plan, nil, err
	}
	root, err := bootstrapSuccessorPhysicalRoot(runDirectory)
	if err != nil {
		return plan, nil, err
	}
	retained, err := openBootstrapChainReadinessState(ctx, preparation)
	if err != nil {
		return plan, nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, retained.close())
		}
	}()
	proposal, err := inspectBootstrapContractSuccessor(ctx, preparation.Contracts, preparation.Plan.Config.Contracts.Path, request, requestHash)
	if err != nil {
		return plan, nil, err
	}
	proposal.LocalPreparation = retained
	proposal.ContentHash = rootObjectHash(proposal)
	plan = bootstrapSuccessorPreparationPlan{Schema: bootstrapSuccessorPreparationSchema,
		OriginalConfig: planFileReference{Path: configPath, Sha256: preparation.Plan.ConfigSha256},
		Request:        planFileReference{Path: requestPath, Sha256: requestHash}, ApprovalPublicKey: preparation.Contracts.Config.ApprovalPublicKey,
		Root: root, Proposal: proposal}
	observed, err := bootstrapSuccessorPhysicalRoot(runDirectory)
	if err != nil || observed != root {
		return plan, nil, errors.Join(errors.New("successor physical root changed during original custody inspection"), err)
	}
	return plan, retained, errors.Join(retained.checkpoint(ctx), plan.validate())
}

// The fixed destination and its staged namespace cannot borrow any original
// input or validator path. Admission changes no original plan or journal hash.
func validateBootstrapSuccessorPreparationPaths(preparation bootstrapChainPreparation, plans []evmCreatePlan, requestPath, approvalPath string, additionalPaths ...string) error {
	if err := validateBootstrapContractReadinessPaths(preparation, plans); err != nil {
		return err
	}
	c := preparation.Plan.Config
	paths := append([]string(nil), preparation.protectedPaths()...)
	for i := 1; i < len(plans); i++ {
		paths = append(paths, filepath.Join(c.RunDirectory, bootstrapContractStateFile(i)))
	}
	paths = append(paths, filepath.Join(c.RunDirectory, bootstrapSuccessorPreparationFile))
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] || seen[path+".lock"] {
			return errors.New("successor fixed custody aliases an original owner")
		}
		seen[path], seen[path+".lock"] = true, true
	}
	inputs := []string{preparation.Plan.ConfigPath, c.OwnerTrimPolicy.Path, c.OwnerTrimPlan.Path, c.Contracts.Path, c.Root.Path,
		c.Validators[0].Config.Path, c.Validators[1].Config.Path, preparation.Contracts.Config.Plan.Artifacts.Path,
		preparation.Root.ServiceInput.Path, c.RootValidator.Approval.Path, requestPath}
	if approvalPath != "" {
		inputs = append(inputs, approvalPath)
	}
	inputs = append(inputs, additionalPaths...)
	for _, path := range inputs {
		if !bootstrapRootAbsolutePath(path) || seen[path] {
			return errors.New("successor input aliases original or prepared custody or another input")
		}
		seen[path] = true
	}
	if err := validateBootstrapChainValidatorPaths(preparation.Plan.ValidatorInspections, seen); err != nil {
		return err
	}
	for _, inspection := range preparation.Plan.ValidatorInspections {
		for _, path := range inspection.DeclaredPaths {
			seen[path] = true
		}
	}
	for path := range seen {
		if strings.HasPrefix(path, c.RunDirectory+string(filepath.Separator)+bootstrapSuccessorStagePrefix) {
			return errors.New("successor staged custody namespace overlaps an approved input or validator path")
		}
		if strings.HasPrefix(path, c.RunDirectory+string(filepath.Separator)+bootstrapSuccessorExecutionPrefix) || strings.HasPrefix(path, c.RunDirectory+string(filepath.Separator)+bootstrapSuccessorExecutionStagePrefix) {
			return errors.New("successor execution custody namespace overlaps an approved input or validator path")
		}
	}
	return nil
}

// Record seals retain the complete independently approved preparation, with no
// mutable allowance or chain outcome that a missing journal could reset.
func (self bootstrapSuccessorPreparationRecord) validate(approval bootstrapSuccessorPreparationApproval) error {
	claimed := self.ContentHash
	self.ContentHash = ""
	if self.Schema != bootstrapSuccessorPreparationStateSchema || self.Phase != "prepared-offline" ||
		rootObjectHash(self.Approval) != rootObjectHash(approval) || claimed != rootObjectHash(self) {
		return errors.New("successor preparation record differs from its immutable approval")
	}
	return approval.validate(approval.Plan)
}

// These files are public signed material; their ownership remains private.
func bootstrapSuccessorPrivateRegular(file *os.File) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0077 != 0 || stat.Nlink != 1 {
		return errors.New("successor file is not a private single-link regular file")
	}
	return nil
}
