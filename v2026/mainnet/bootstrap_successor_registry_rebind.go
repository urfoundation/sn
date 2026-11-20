// A separate approval may bind an already claimed execution's original nonce
// registry to one reviewed restored physical generation. It never changes the
// original execution approval, nonce claims, signed transaction or allowance.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

const bootstrapSuccessorRegistryRebindSchema = "urnetwork-mainnet-successor-registry-rebind-v1"
const bootstrapSuccessorRegistryRebindEnvelopeSchema = "urnetwork-mainnet-successor-registry-rebind-envelope-v1"
const bootstrapSuccessorRegistryRebindFile = bootstrapSuccessorExecutionPrefix + "-registry-rebind.json"
const maximumBootstrapSuccessorRegistryRebindBytes = 16 * 1024

// All authority comes from the independently pinned original approver. The
// target declaration is separately explicit and includes the unchanged local
// preparation root; the single-root restore output cannot silently replace it.
type bootstrapSuccessorRegistryRebindPlan struct {
	Schema                   string                         `json:"schema"`
	ExecutionApprovalHash    string                         `json:"execution_approval_hash"`
	RegistryDirectory        string                         `json:"registry_directory"`
	OriginalRegistry         bootstrapSuccessorRootIdentity `json:"original_registry"`
	RestoredRegistry         bootstrapSuccessorRootIdentity `json:"restored_registry"`
	RestoredGeneration       string                         `json:"restored_generation_sha256"`
	RuntimeDeclaration       durablevolume.Reference        `json:"runtime_declaration"`
	RestorePlan              durablevolume.Reference        `json:"restore_plan"`
	OriginalInventory        durablevolume.Reference        `json:"original_inventory"`
	OriginalFormerWriter     durablevolume.Reference        `json:"original_former_writer_fence"`
	OriginalMemberCensusHash string                         `json:"original_member_census_sha256"`
	DerivedMemberCensusHash  string                         `json:"derived_member_census_sha256"`
}

type bootstrapSuccessorRegistryRebindApproval struct {
	Schema    string                               `json:"schema"`
	Plan      bootstrapSuccessorRegistryRebindPlan `json:"plan"`
	Signature string                               `json:"signature_ed25519"`
}

// The domain has no transaction, signer or allowance fields to increase.
func (self bootstrapSuccessorRegistryRebindPlan) signingBytes(original bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile) ([]byte, error) {
	if err := original.validate(original.Plan, profile); err != nil {
		return nil, err
	}
	if self.Schema != bootstrapSuccessorRegistryRebindSchema || self.ExecutionApprovalHash != rootObjectHash(original) ||
		self.RegistryDirectory != original.Plan.Request.RegistryDirectory || self.OriginalRegistry != original.Plan.Registry ||
		self.RestoredRegistry.Inode == 0 || self.RestoredRegistry == self.OriginalRegistry || !planSha256(self.RestoredGeneration) ||
		!planSha256(self.OriginalMemberCensusHash) || !planSha256(self.DerivedMemberCensusHash) {
		return nil, errors.New("successor registry rebind changes original scope or lacks exact restored custody")
	}
	for _, reference := range []durablevolume.Reference{self.RuntimeDeclaration, self.RestorePlan, self.OriginalInventory, self.OriginalFormerWriter} {
		if !bootstrapRootAbsolutePath(reference.Path) || !planSha256(reference.Sha256) || reference.Path == self.RegistryDirectory || strings.HasPrefix(reference.Path, self.RegistryDirectory+"/") {
			return nil, errors.New("successor registry rebind reference is absent, unpinned or overlaps nonce custody")
		}
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > maximumBootstrapSuccessorRegistryRebindBytes {
		return nil, errors.Join(errors.New("successor registry rebind exceeds its finite approval bound"), err)
	}
	return append([]byte(bootstrapSuccessorRegistryRebindSchema+"\x00"), raw...), nil
}

// Build from reviewed restore lineage and actual declaration; no approval is
// inferred from successful copying and no target member is changed here.
func buildBootstrapSuccessorRegistryRebind(ctx context.Context, original bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile, reference durablevolume.Reference) (bootstrapSuccessorRegistryRebindPlan, error) {
	var result bootstrapSuccessorRegistryRebindPlan
	if ctx == nil || ctx.Err() != nil {
		return result, errors.New("successor registry rebind requires an active inspection context")
	}
	if err := original.validate(original.Plan, profile); err != nil {
		return result, err
	}
	declarationReference, found := durablevolume.ReferenceFromContext(ctx)
	if !found {
		return result, errors.New("successor registry rebind requires its explicit runtime declaration")
	}
	raw, hash, err := readBootstrapRootFile(ctx, reference.Path, 32*1024*1024)
	if err != nil || hash != reference.Sha256 {
		return result, errors.Join(errors.New("successor registry restore plan pin differs"), err)
	}
	var plan durablevolume.PreparationPlan
	if err := decodePlanJson(raw, &plan); err != nil {
		return result, err
	}
	var request durablevolume.PreparationRequest
	if err := decodePlanJson(plan.RequestBytes, &request); err != nil {
		return result, err
	}
	if plan.Schema != durablevolume.PreparationPlanSchema || plan.RestartAuthorized || request.Purpose != "restore" || request.Scope != "daemon" || request.RestoreSource == nil ||
		plan.RequestSha256 != plan.Request.Sha256 || safeReleaseHash(plan.RequestBytes) != plan.RequestSha256 ||
		request.RootPath != original.Plan.Request.RegistryDirectory || len(plan.Owners) != 1 || len(request.Owners) != 1 || !reflect.DeepEqual(request.Owners[0], plan.Owners[0].Owner) ||
		plan.Owners[0].Owner.Kind != "mainnet-successor-nonce-members" || len(plan.Derivations) < 1 || len(plan.Derivations) > 2 || len(plan.Generation) != durablevolume.RootGenerationBytes || len(plan.Lease) != 32 {
		return result, errors.New("successor registry rebind requires one exact restored original nonce registry")
	}
	inventory, err := durablevolume.LoadPhysicalInventory(ctx, request.RestoreSource.Inventory)
	if err != nil {
		return result, err
	}
	if inventory.PhysicalRoot.Inode != original.Plan.Registry.Inode || unix.Mkdev(inventory.PhysicalRoot.Device.Major, inventory.PhysicalRoot.Device.Minor) != original.Plan.Registry.Device || inventory.StateRoot.Path != request.RootPath || inventory.FormerWriterFence != request.RestoreSource.FormerWriterFence {
		return result, errors.New("successor registry restore source is not its original approved generation")
	}
	fenceRaw, fenceHash, err := readBootstrapRootFile(ctx, inventory.FormerWriterFence.Path, 16*1024)
	var fence durablevolume.FormerWriterFence
	if err == nil {
		err = decodePlanJson(fenceRaw, &fence)
	}
	if err != nil || fenceHash != inventory.FormerWriterFence.Sha256 || fence.Schema != durablevolume.FormerWriterFenceSchema || !fence.FormerWritersStopped || strings.TrimSpace(fence.Evidence) == "" || fence.RootPath != inventory.StateRoot.Path || fence.DeclarationSha256 != inventory.Declaration.Sha256 || fence.LeaseSha256 != inventory.StateRoot.LeaseSha256 {
		return result, errors.Join(errors.New("successor registry original former-writer assertion differs"), err)
	}
	expected, err := planStoragePreparationMembersRestore(ctx, plan.Owners[0].StagingName, request.Owners[0], inventory, false)
	if err != nil {
		return result, err
	}
	census, err := buildBootstrapSuccessorRebindCensus(ctx, plan, 0, expected, inventory)
	if err != nil {
		return result, err
	}
	declaration, err := durablevolume.Load(declarationReference)
	if err != nil {
		return result, err
	}
	found = false
	for _, volume := range declaration.Volumes {
		for _, root := range volume.StateRoots {
			if root.Path == request.RootPath {
				if found || volume.FilesystemUuid != request.FilesystemUuid || volume.FilesystemType != request.FilesystemType || root.RootInode != plan.Root.Inode || root.GenerationSha256 != safeReleaseHash(plan.Generation) || root.LeasePath != request.LeasePath || root.LeaseSha256 != safeReleaseHash(plan.Lease) {
					return result, errors.New("successor registry runtime declaration differs from exact restored custody")
				}
				found = true
			}
		}
	}
	if !found {
		return result, errors.New("successor registry runtime declaration omits the restored owner")
	}
	physical, err := bootstrapSuccessorPhysicalRoot(request.RootPath)
	if err != nil || physical != (bootstrapSuccessorRootIdentity{Device: plan.Root.Device, Inode: plan.Root.Inode}) {
		return result, errors.Join(errors.New("successor registry restored physical generation differs"), err)
	}
	if err := inspectBootstrapSuccessorRestoredCensus(ctx, request.RootPath, census, plan.Owners[0], inventory, true); err != nil {
		return result, err
	}
	result = bootstrapSuccessorRegistryRebindPlan{Schema: bootstrapSuccessorRegistryRebindSchema, ExecutionApprovalHash: rootObjectHash(original), RegistryDirectory: request.RootPath,
		OriginalRegistry: original.Plan.Registry, RestoredRegistry: physical, RestoredGeneration: safeReleaseHash(plan.Generation), RuntimeDeclaration: declarationReference,
		RestorePlan: reference, OriginalInventory: request.RestoreSource.Inventory, OriginalFormerWriter: request.RestoreSource.FormerWriterFence,
		OriginalMemberCensusHash: census.Derivation.Original.File.Sha256, DerivedMemberCensusHash: census.Derivation.Derived.Sha256}
	_, err = result.signingBytes(original, profile)
	return result, errors.Join(err, ctx.Err())
}

// Passive admission checks the real retained root/head/member union and never
// repairs a pending outer head. Its shared lock joins before execution opens.
func inspectBootstrapSuccessorRegistryRebindTarget(ctx context.Context, path string, expected bootstrapSuccessorMemberCensus) (resultErr error) {
	return inspectBootstrapSuccessorRebindTarget(ctx, path, expected, true)
}

// Both physical adoptions keep the exact original member set. Additional
// authenticated history may accumulate only through its existing owner.
func inspectBootstrapSuccessorRebindTarget(ctx context.Context, path string, expected bootstrapSuccessorMemberCensus, global bool) (resultErr error) {
	storage, err := openMainnetDurableDirectory(ctx, path, durablevolume.ReadOnly)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, storage.close()) }()
	file := storage.directory.File()
	if err := mainnetDurableFlock(int(file.Fd()), unix.LOCK_SH); err != nil {
		return err
	}
	members, err := openBootstrapSuccessorMembers(storage, file, global, true)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, members.close()) }()
	current := make(map[string]bootstrapSuccessorMember, len(members.census.Members))
	for _, member := range members.census.Members {
		current[member.Name] = member
	}
	for _, original := range expected.Members {
		if current[original.Name] != original {
			return errors.New("successor registry lost or changed a reviewed restored nonce member")
		}
	}
	if pending := expected.Pending; pending != nil && !reflect.DeepEqual(pending, members.census.Pending) {
		if current := members.census.Pending; current != nil && pending.StageInode == 0 && current.StageInode != 0 {
			// A joined runtime may have materialized the exact originally
			// reserved stage. All other payload and name fields remain fixed.
			copy := *current
			copy.StageInode = 0
			if reflect.DeepEqual(pending, &copy) {
				return members.check()
			}
		}
		// Another joined owner may have finished the original retained nonce.
		// The original exact payload and any acknowledged stage inode survive.
		completed, found := current[pending.Name]
		if !found || pending.Append || completed.Size != pending.Size || completed.Sha256 != pending.Sha256 || pending.StageInode != 0 && completed.Inode != pending.StageInode {
			return errors.New("successor registry changed its reviewed pending nonce authority")
		}
	}
	return members.check()
}

// Independent approval reuses only the original approver key and its own
// domain. Validation runs once per reopened owner, outside any state mutex.
func (self bootstrapSuccessorRegistryRebindApproval) validate(ctx context.Context, original bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile) error {
	message, err := self.Plan.signingBytes(original, profile)
	key, keyErr := rootReceiptHex(original.Plan.Review.Preparation.Approval.Plan.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if self.Schema != bootstrapSuccessorRegistryRebindEnvelopeSchema || err != nil || keyErr != nil || signatureErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.Join(errors.New("successor registry independent rebind approval is invalid"), err, keyErr, signatureErr)
	}
	expected, err := buildBootstrapSuccessorRegistryRebind(ctx, original, profile, self.Plan.RestorePlan)
	if err != nil || expected != self.Plan {
		return errors.Join(errors.New("successor registry approval differs from exact restored lineage"), err)
	}
	return nil
}

// Both replay and live checkpoints use this exact authenticated receipt
// inventory. A filename prefix never grants authority. Only initial replay may
// accept absence before publication; the member head still refuses lost custody.
func (self *bootstrapSuccessorExecutionStore) includePhysicalRebindReceipts(allowed map[string]bool, allowUnpublished bool) error {
	if err := self.checkDeferredLocalRebind(); err != nil {
		return err
	}
	receipts := map[string]any{}
	if self.registryRebind != nil {
		receipts[bootstrapSuccessorRegistryRebindFile] = self.registryRebind
	}
	if self.localRebind != nil {
		receipts[bootstrapSuccessorLocalRebindFile] = self.localRebind
	}
	for name, receipt := range receipts {
		raw, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		retained, err := self.local.read(name)
		if (allowUnpublished || self.deferredLocalRebind) && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !bytes.Equal(raw, retained) {
			return errors.Join(errors.New("successor execution lost its original physical rebind receipt"), err)
		}
		allowed[name] = true
	}
	return nil
}

// Original completed nonce/claim bytes and event history are inspected before
// first publication. A joined retry may finish only this exact reserved receipt.
func (self *bootstrapSuccessorExecutionStore) retainRegistryRebind() error {
	if self.registryRebind == nil || self.deferredLocalRebind {
		return nil
	}
	raw, err := json.Marshal(self.registryRebind)
	if err != nil {
		return err
	}
	return self.local.publish(bootstrapSuccessorRegistryRebindFile, "registry-rebind", raw)
}

// Preview exposes public bytes for the original independent approver only.
func (self bootstrapSuccessorRegistryRebindPlan) preview(original bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile) (any, error) {
	message, err := self.signingBytes(original, profile)
	if err != nil {
		return nil, err
	}
	return struct {
		Schema       string                               `json:"schema"`
		Plan         bootstrapSuccessorRegistryRebindPlan `json:"plan"`
		SigningBytes string                               `json:"signing_bytes"`
	}{Schema: "urnetwork-mainnet-successor-registry-rebind-preview-v1", Plan: self, SigningBytes: "0x" + hex.EncodeToString(message)}, nil
}
