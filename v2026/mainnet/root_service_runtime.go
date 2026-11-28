// The executable root runtime composes original scoped approvals and durable
// owners. Its public capability set admits observation and receipt recovery;
// it contains no current-authority, native-device or submission capability.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"path/filepath"
)

const rootServiceRuntimeConfigSchema = "urnetwork-mainnet-root-service-runtime-config-v1"

// The root's own inputs suffice. Unrelated validators, contracts and historical
// work are not prerequisites for reading this original action's liabilities.
type rootServiceRuntimeConfig struct {
	Schema     string                      `json:"schema"`
	Root       planFileReference           `json:"root_config"`
	Role       bootstrapChainRootValidator `json:"root_validator"`
	Submission planFileReference           `json:"submission_config"`
}

// These values come from exact independent files, never a retained journal.
type rootServiceRuntimePreparation struct {
	Input      planFileReference
	Config     rootServiceRuntimeConfig
	Root       bootstrapRootPlan
	Approval   bootstrapChainRootApproval
	Submission rootSubmissionConfig
}

// The original service-config signature remains a distinct approval from both
// the native action and its owned route. No read-only-ready bit is consumed.
func (self rootServiceRuntimePreparation) validate() error {
	if self.Config.Schema != rootServiceRuntimeConfigSchema || !planSha256(self.Input.Sha256) ||
		!planSha256(self.Config.Submission.Sha256) || self.Root.ConfigPath != self.Config.Root.Path || self.Root.ConfigSha256 != self.Config.Root.Sha256 {
		return errors.New("root runtime requires exact independently provisioned root and submission inputs")
	}
	if err := errors.Join(self.Config.Role.validate(), self.Root.validate(), self.Submission.validate()); err != nil {
		return err
	}
	role, approval, service := self.Config.Role, self.Approval, self.Root.Service
	scope := service.Packet.Action.Scope
	if role.Role != scope.Role || *role.Netuid != scope.Netuid || role.Hotkey != scope.Hotkey || role.Coldkey != scope.Coldkey ||
		*role.Seat != scope.Seat || role.Strategy != scope.Strategy || role.ActionApprovalPublicKey != service.CustodyTrust.ApprovalPublicKey ||
		rootObjectHash(self.Submission.Service) != rootObjectHash(service) || approval.Schema != bootstrapChainRootApprovalSchema ||
		approval.DeploymentId != self.Root.DeploymentId || approval.RootPlanHash != self.Root.ContentHash || approval.ServiceConfigHash != rootObjectHash(service) {
		return errors.New("root runtime action, role, full-service approval or submission differs from its original scope")
	}
	key, _ := hex.DecodeString(role.ApprovalPublicKey[2:])
	signature, err := rootOfflineSignatureBytes(approval.Signature)
	message, messageErr := approval.signingBytes()
	if err != nil || messageErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.New("root runtime full-service approval signature differs from the independent service approver")
	}
	seen := map[string]bool{}
	for _, path := range []string{scope.StatePath, service.CustodyTrust.StatePath, self.Submission.Approval.StatePath, filepath.Join(self.Root.RunDirectory, bootstrapRootProgressFile)} {
		if !bootstrapRootAbsolutePath(path) || seen[path] || seen[path+".lock"] {
			return errors.New("root runtime journals or ownership markers overlap")
		}
		seen[path], seen[path+".lock"] = true, true
	}
	for _, path := range []string{self.Input.Path, self.Config.Root.Path, self.Root.ServiceInput.Path, role.Approval.Path, self.Config.Submission.Path} {
		if !bootstrapRootAbsolutePath(path) || seen[path] || seen[path+".lock"] {
			return errors.New("root runtime input is noncanonical or overlaps another input or journal")
		}
		seen[path], seen[path+".lock"] = true, true
	}
	return nil
}

// Loading authenticates bounded files only. It never opens a journal or route.
func loadRootServiceRuntime(ctx context.Context, path string) (rootServiceRuntimePreparation, error) {
	var result rootServiceRuntimePreparation
	raw, digest, err := readBootstrapRootFile(ctx, path, rootServiceStoreLimit)
	if err != nil {
		return result, err
	}
	result.Input = planFileReference{Path: path, Sha256: digest}
	if err := decodePlanJson(raw, &result.Config); err != nil {
		return result, err
	}
	read := func(reference planFileReference, maximum int) ([]byte, error) {
		if !planSha256(reference.Sha256) {
			return nil, errors.New("root runtime input lacks an exact byte pin")
		}
		raw, digest, err := readBootstrapRootFile(ctx, reference.Path, maximum)
		if err != nil || digest != reference.Sha256 {
			return nil, errors.Join(errors.New("root runtime original input differs from its byte pin"), err)
		}
		return raw, nil
	}
	raw, err = read(result.Config.Root, rootServiceStoreLimit)
	if err != nil {
		return result, err
	}
	result.Root, err = decodeBootstrapRootPlan(ctx, result.Config.Root.Path, raw, result.Config.Root.Sha256)
	if err != nil {
		return result, err
	}
	raw, err = read(result.Config.Role.Approval, maximumBootstrapChainRootApprovalBytes)
	if err != nil {
		return result, err
	}
	if err := decodePlanJson(raw, &result.Approval); err != nil {
		return result, err
	}
	raw, err = read(result.Config.Submission, rootSubmissionStoreLimit)
	if err != nil {
		return result, err
	}
	if err := decodePlanJson(raw, &result.Submission); err != nil {
		return result, err
	}
	return result, errors.Join(result.validate(), ctx.Err())
}

// The caller joins Run before close. No worker, secret loader or device command
// is started by construction, and every child journal remains after close.
type rootServiceRuntime struct {
	service         *rootServiceOwner
	custody         *rootOfflineCustody
	submission      *rootOwnedSubmission
	serviceStore    *rootServiceStore
	custodyStore    *rootOfflineCustodyStore
	submissionStore *rootSubmissionStore
}

// Only prepare may explicitly create the submission journal. Service and
// custody must already exist, including on every interrupted/restarted run.
func openRootServiceRuntime(ctx context.Context, preparation rootServiceRuntimePreparation, createSubmission bool) (_ *rootServiceRuntime, resultErr error) {
	if ctx == nil {
		return nil, errors.New("root runtime requires a context")
	}
	if err := errors.Join(preparation.validate(), ctx.Err()); err != nil {
		return nil, err
	}
	self := &rootServiceRuntime{}
	success := false
	defer func() {
		if !success {
			resultErr = errors.Join(resultErr, self.close())
		}
	}()
	service := preparation.Root.Service
	var err error
	self.custodyStore, err = openRootOfflineCustodyStore(service.CustodyTrust, nil, ctx)
	if err != nil {
		return nil, err
	}
	self.custody, err = newRootOfflineCustody(service.CustodyTrust, self.custodyStore)
	if err != nil {
		return nil, err
	}
	packet, err := self.custody.packet(ctx)
	if err != nil || packet.ContentHash != service.Packet.ContentHash {
		return nil, errors.Join(errors.New("root runtime custody retains a different packet"), err)
	}
	self.serviceStore, err = openRootServiceStore(service, false, ctx)
	if err != nil {
		return nil, err
	}
	// Authenticate both old child states before claiming any new path.
	serviceRecord, err := self.serviceStore.load()
	if err != nil {
		return nil, err
	}
	custodyRecord, err := self.custodyStore.load()
	if err != nil || serviceRecord.Action.Signature != "" && custodyRecord.Signature != "" && serviceRecord.Action.Signature != custodyRecord.Signature {
		return nil, errors.Join(errors.New("root runtime original custody/service signatures disagree"), err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	submissionHash := rootObjectHash(preparation.Submission)
	persistPreparation := func() error {
		serviceRecord.ContentHash = ""
		serviceRecord.ContentHash = rootObjectHash(serviceRecord)
		return self.serviceStore.save(serviceRecord)
	}
	if serviceRecord.SubmissionConfigHash != "" && serviceRecord.SubmissionConfigHash != submissionHash {
		return nil, errors.New("root runtime cannot replace its original submission route or journal")
	}
	if createSubmission && serviceRecord.SubmissionConfigHash == "" {
		// This claim precedes child creation. Once completion is recorded below,
		// even removal of both child files cannot replenish the spent allowance.
		serviceRecord.SubmissionConfigHash = submissionHash
		if err := persistPreparation(); err != nil {
			return nil, err
		}
	}
	if !createSubmission && (!serviceRecord.SubmissionPrepared || serviceRecord.SubmissionConfigHash != submissionHash) {
		return nil, errors.New("root runtime original submission preparation is incomplete; resume prepare")
	}
	createChild, err := bootstrapRootCreateChild(preparation.Submission.Approval.StatePath, serviceRecord.SubmissionPrepared)
	if err != nil {
		return nil, err
	}
	self.submissionStore, err = openRootSubmissionStore(preparation.Submission, createChild, ctx)
	if err != nil {
		return nil, err
	}
	self.submission, err = newRootOwnedSubmission(preparation.Submission, self.submissionStore, nil)
	if err != nil {
		return nil, err
	}
	if !serviceRecord.SubmissionPrepared {
		serviceRecord.SubmissionPrepared = true
		if err := persistPreparation(); err != nil {
			return nil, err
		}
	}
	self.service, err = newRootServiceOwner(service, self.serviceStore, rootServicePorts{
		Observer: self.submission.chain, Reconciler: self.submission, Signer: self.custody,
	})
	if err != nil {
		return nil, err
	}
	if _, _, err := self.issuedSignature(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	success = true
	return self, nil
}

// Closing joins no detached work; the externally driven operation has already
// returned. All close failures survive alongside the original operation error.
func (self *rootServiceRuntime) close() error {
	if self == nil {
		return nil
	}
	if self.submission != nil {
		self.submission.chain.client.httpClient.CloseIdleConnections()
	}
	return errors.Join(self.submissionStore.close(), self.serviceStore.close(), self.custodyStore.close())
}

// Any retained valid signature wins over an absent local copy. Contradictory
// valid signatures are refused before mutation; absence is always unresolved.
// This function requires sole runtime ownership, outside a running service step.
func (self *rootServiceRuntime) issuedSignature() ([]byte, uint8, error) {
	service, err := self.serviceStore.load()
	if err != nil {
		return nil, 0, err
	}
	custody, err := self.custodyStore.load()
	if err != nil {
		return nil, 0, err
	}
	submission, err := self.submissionStore.load()
	if err != nil {
		return nil, 0, err
	}
	signatures := []string{service.Action.Signature, custody.Signature}
	if submission.RawExtrinsic != "" {
		raw, err := rootReceiptHex(submission.RawExtrinsic, 64*1024)
		if err != nil || rootReceiptSignedAction(service.Action.Action, raw) != nil {
			return nil, 0, errors.New("root runtime submission signature differs from the approved action")
		}
		// The original codec has already checked the length, address, signature
		// variant and every signed field. Extract only its verified public bytes.
		reader := rootScaleReader{data: raw}
		if _, err := reader.compact(); err != nil {
			return nil, 0, err
		}
		signatures = append(signatures, hex.EncodeToString(raw[reader.offset+35:reader.offset+99]))
	}
	var signature string
	for _, retained := range signatures {
		if retained == "" {
			continue
		}
		if signature != "" && retained != signature {
			return nil, 0, errors.New("root runtime original journals retain conflicting native signatures")
		}
		signature = retained
	}
	broadcasts := service.Action.Broadcasts
	for _, attempt := range submission.Attempts {
		broadcasts = max(broadcasts, attempt.Number)
	}
	if signature == "" {
		return nil, broadcasts, nil
	}
	raw, err := rootOfflineSignatureBytes(signature)
	return raw, broadcasts, err
}

// This runs before any weight decision or chain read. A signature in any one
// original journal is enough to retain and reconcile the issued liability,
// including after current authority, eligibility or the mortal window changes.
func (self *rootServiceRuntime) recoverIssued(ctx context.Context) error {
	if ctx == nil {
		return errors.New("root runtime recovery requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	signature, broadcasts, err := self.issuedSignature()
	if err != nil || len(signature) == 0 {
		return err
	}
	if err := self.service.retainIssuedSignature(ctx, signature, broadcasts); err != nil {
		return err
	}
	// Retain the same public receipt in custody too. If either local write is
	// interrupted, the next run resumes from the surviving original bytes.
	return self.custody.importSignature(ctx, rootOfflineSignature{Schema: rootOfflineSignatureSchema,
		PacketHash: self.service.config.Packet.ContentHash, Signature: hex.EncodeToString(signature)})
}
