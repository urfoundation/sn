// Local restoration needs a separate original-approver receipt. It preserves
// signed preparation/execution bytes while admitting one reviewed physical root.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

const bootstrapSuccessorLocalRebindSchema = "urnetwork-mainnet-successor-local-rebind-v1"
const bootstrapSuccessorLocalRebindEnvelopeSchema = "urnetwork-mainnet-successor-local-rebind-envelope-v1"
const bootstrapSuccessorLocalRebindFile = bootstrapSuccessorExecutionPrefix + "-local-rebind.json"

// The original signature domain, logical path and every nonce remain fixed.
// Only this separately signed receipt authorizes the new physical coordinate.
type bootstrapSuccessorLocalRebindPlan struct {
	Schema                   string                         `json:"schema"`
	ExecutionApprovalHash    string                         `json:"execution_approval_hash"`
	LocalDirectory           string                         `json:"local_directory"`
	OriginalLocal            bootstrapSuccessorRootIdentity `json:"original_local"`
	RestoredLocal            bootstrapSuccessorRootIdentity `json:"restored_local"`
	RestoredGeneration       string                         `json:"restored_generation_sha256"`
	RuntimeDeclaration       durablevolume.Reference        `json:"runtime_declaration"`
	RestorePlan              durablevolume.Reference        `json:"restore_plan"`
	OriginalInventory        durablevolume.Reference        `json:"original_inventory"`
	OriginalFormerWriter     durablevolume.Reference        `json:"original_former_writer_fence"`
	OriginalMemberCensusHash string                         `json:"original_member_census_sha256"`
	DerivedMemberCensusHash  string                         `json:"derived_member_census_sha256"`
	PendingOutcomeSha256     string                         `json:"pending_outcome_sha256,omitempty"`
}

type bootstrapSuccessorLocalRebindApproval struct {
	Schema    string                            `json:"schema"`
	Plan      bootstrapSuccessorLocalRebindPlan `json:"plan"`
	Signature string                            `json:"signature_ed25519"`
}

// An unsigned preview can borrow only a passive reader. Writer admission is
// constructed separately after the original approver's signature is verified.
type bootstrapSuccessorLocalInspection struct {
	preparationHash string
	physical        bootstrapSuccessorRootIdentity
	writer          bool
	restoredView    *bootstrapSuccessorRestoredMemberView
}

func (self bootstrapSuccessorLocalRebindPlan) signingBytes(original bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile) ([]byte, error) {
	if err := original.validate(original.Plan, profile); err != nil {
		return nil, err
	}
	preparation := original.Plan.Review.Preparation.Approval.Plan
	if self.Schema != bootstrapSuccessorLocalRebindSchema || self.ExecutionApprovalHash != rootObjectHash(original) ||
		self.LocalDirectory != preparation.Proposal.OriginalRunDirectory || self.OriginalLocal != preparation.Root ||
		self.RestoredLocal.Inode == 0 || self.RestoredLocal == self.OriginalLocal || !planSha256(self.RestoredGeneration) ||
		!planSha256(self.OriginalMemberCensusHash) || !planSha256(self.DerivedMemberCensusHash) {
		return nil, errors.New("successor local rebind changes original scope or lacks exact restored custody")
	}
	if self.PendingOutcomeSha256 != "" && !planSha256(self.PendingOutcomeSha256) {
		return nil, errors.New("successor local rebind lacks an exact pending outcome digest")
	}
	for _, reference := range []durablevolume.Reference{self.RuntimeDeclaration, self.RestorePlan, self.OriginalInventory, self.OriginalFormerWriter} {
		if !bootstrapRootAbsolutePath(reference.Path) || !planSha256(reference.Sha256) || reference.Path == self.LocalDirectory || strings.HasPrefix(reference.Path, self.LocalDirectory+"/") {
			return nil, errors.New("successor local rebind reference is absent, unpinned or overlaps original custody")
		}
	}
	raw, err := json.Marshal(self)
	if err != nil || len(raw) > maximumBootstrapSuccessorRegistryRebindBytes {
		return nil, errors.Join(errors.New("successor local rebind exceeds its finite approval bound"), err)
	}
	return append([]byte(bootstrapSuccessorLocalRebindSchema+"\x00"), raw...), nil
}

// This profile covers the original local member census, any exact retained
// next image and every co-owned fixed snapshot. Preview never repairs heads.
func buildBootstrapSuccessorLocalRebind(ctx context.Context, original bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile, reference durablevolume.Reference) (result bootstrapSuccessorLocalRebindPlan, resultErr error) {
	return buildBootstrapSuccessorLocalRebindWithView(ctx, original, profile, reference, nil)
}

// A private passive-read capability accompanies the exact already validated
// pair; it adds no serialized authority and cannot be used by a writer.
func buildBootstrapSuccessorLocalRebindWithView(ctx context.Context, original bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile, reference durablevolume.Reference, retained **bootstrapSuccessorRestoredMemberView) (result bootstrapSuccessorLocalRebindPlan, resultErr error) {
	if retained != nil {
		*retained = nil
	}
	if ctx == nil || ctx.Err() != nil {
		return result, errors.New("successor local rebind requires an active inspection context")
	}
	if err := original.validate(original.Plan, profile); err != nil {
		return result, err
	}
	declarationReference, found := durablevolume.ReferenceFromContext(ctx)
	if !found {
		return result, errors.New("successor local rebind requires its explicit runtime declaration")
	}
	raw, hash, err := readBootstrapRootFile(ctx, reference.Path, 32*1024*1024)
	if err != nil || hash != reference.Sha256 {
		return result, errors.Join(errors.New("successor local restore plan pin differs"), err)
	}
	var plan durablevolume.PreparationPlan
	if err := decodePlanJson(raw, &plan); err != nil {
		return result, err
	}
	var request durablevolume.PreparationRequest
	if err := decodePlanJson(plan.RequestBytes, &request); err != nil {
		return result, err
	}
	p := original.Plan.Review.Preparation.Approval.Plan
	if plan.Schema != durablevolume.PreparationPlanSchema || plan.RestartAuthorized || request.Purpose != "restore" || request.Scope != "daemon" || request.RestoreSource == nil ||
		plan.RequestSha256 != plan.Request.Sha256 || safeReleaseHash(plan.RequestBytes) != plan.RequestSha256 || request.RootPath != p.Proposal.OriginalRunDirectory ||
		len(plan.Owners) < 2 || len(plan.Owners) > 32 || len(plan.Owners) != len(request.Owners) || len(plan.Derivations) < 1 || len(plan.Derivations) > 2 || len(plan.Generation) != durablevolume.RootGenerationBytes || len(plan.Lease) != 32 {
		return result, errors.New("successor local rebind requires the complete original shared-owner restore")
	}
	inventory, err := durablevolume.LoadPhysicalInventory(ctx, request.RestoreSource.Inventory)
	if err != nil {
		return result, err
	}
	if inventory.PhysicalRoot.Inode != p.Root.Inode || unix.Mkdev(inventory.PhysicalRoot.Device.Major, inventory.PhysicalRoot.Device.Minor) != p.Root.Device || inventory.StateRoot.Path != request.RootPath || inventory.FormerWriterFence != request.RestoreSource.FormerWriterFence {
		return result, errors.New("successor local restore source is not its original approved generation")
	}
	fenceRaw, fenceHash, err := readBootstrapRootFile(ctx, inventory.FormerWriterFence.Path, 16*1024)
	var fence durablevolume.FormerWriterFence
	if err == nil {
		err = decodePlanJson(fenceRaw, &fence)
	}
	if err != nil || fenceHash != inventory.FormerWriterFence.Sha256 || fence.Schema != durablevolume.FormerWriterFenceSchema || !fence.FormerWritersStopped || strings.TrimSpace(fence.Evidence) == "" || fence.RootPath != inventory.StateRoot.Path || fence.DeclarationSha256 != inventory.Declaration.Sha256 || fence.LeaseSha256 != inventory.StateRoot.LeaseSha256 {
		return result, errors.Join(errors.New("successor local original former-writer assertion differs"), err)
	}
	memberIndex := -1
	var census bootstrapSuccessorRebindCensus
	originalOwners := make([]durablevolume.PreparationOwnerPlan, 0, len(plan.Owners))
	for index, owner := range plan.Owners {
		if !reflect.DeepEqual(owner.Owner, request.Owners[index]) || owner.Owner.RestoreCoverage != durablevolume.PreparationCompleteUnion {
			return result, errors.New("successor local restore omitted explicit complete owner coverage")
		}
		if owner.Owner.Kind == "mainnet-successor-local-members" {
			if memberIndex != -1 {
				return result, errors.New("successor local restore repeats its member owner")
			}
			memberIndex = index
		} else if _, _, err := storagePreparationSnapshotSpec(false, owner.Owner); err != nil {
			return result, errors.Join(errors.New("successor local restore contains an unsupported co-owner"), err)
		}
		expected, err := planStoragePreparationRestore(ctx, owner.StagingName, owner.Owner, inventory, false)
		if err != nil {
			return result, err
		}
		originalOwners = append(originalOwners, expected)
		if owner.Owner.Kind == "mainnet-successor-local-members" {
			census, err = buildBootstrapSuccessorRebindCensus(ctx, plan, index, expected, inventory)
			if err != nil {
				return result, err
			}
		} else if !reflect.DeepEqual(expected, owner) {
			return result, errors.New("successor local restore changed original owner bytes or capacity")
		}
	}
	if memberIndex == -1 {
		return result, errors.New("successor local restore lacks its immutable member census")
	}
	if err := validateBootstrapSuccessorLocalCoverage(inventory, originalOwners); err != nil {
		return result, err
	}
	memberOwner := plan.Owners[memberIndex]
	pendingOutcome := ""
	if pending := census.Census.Pending; pending != nil {
		// Only an original terminal event can defer the physical receipt.
		// Its full payload remains in the authenticated restored member head;
		// the execution owner still requires exact canonical reconciliation.
		raw, err := base64.StdEncoding.Strict().DecodeString(pending.Payload)
		var event bootstrapSuccessorExecutionEvent
		if err == nil {
			err = decodePlanJson(raw, &event)
		}
		claimed := event.ContentHash
		event.ContentHash = ""
		name := bootstrapSuccessorExecutionEventName(event.Sequence)
		directory := bootstrapSuccessorExecutionDirectory{claim: rootObjectHash(original)}
		if err != nil || pending.Append || event.Sequence == 0 || event.Sequence >= 32 || event.Phase != "installed" && event.Phase != "outer-reverted" ||
			event.ApprovalHash != rootObjectHash(original) || claimed != rootObjectHash(event) || pending.Sha256 != safeReleaseHash(raw) || pending.Size != int64(len(raw)) ||
			pending.Name != name+".intent" && pending.Name != name+".json" || pending.Stage != directory.stageName(pending.Name, event.Phase) {
			return result, errors.Join(errors.New("successor local rebind pending publication is not its original exact terminal outcome"), err)
		}
		pendingOutcome = pending.Sha256
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
					return result, errors.New("successor local runtime declaration differs from exact restored custody")
				}
				found = true
			}
		}
	}
	physical, err := bootstrapSuccessorPhysicalRoot(request.RootPath)
	if err != nil || !found || physical != (bootstrapSuccessorRootIdentity{Device: plan.Root.Device, Inode: plan.Root.Inode}) {
		return result, errors.Join(errors.New("successor local restored physical generation differs"), err)
	}
	if err := inspectBootstrapSuccessorRestoredCensus(ctx, request.RootPath, census, memberOwner, inventory, false); err != nil {
		return result, err
	}
	if err := inspectBootstrapSuccessorLocalSnapshots(ctx, request.RootPath, plan.Owners, inventory); err != nil {
		return result, err
	}
	result = bootstrapSuccessorLocalRebindPlan{Schema: bootstrapSuccessorLocalRebindSchema, ExecutionApprovalHash: rootObjectHash(original), LocalDirectory: request.RootPath,
		OriginalLocal: p.Root, RestoredLocal: physical, RestoredGeneration: safeReleaseHash(plan.Generation), RuntimeDeclaration: declarationReference,
		RestorePlan: reference, OriginalInventory: request.RestoreSource.Inventory, OriginalFormerWriter: request.RestoreSource.FormerWriterFence,
		OriginalMemberCensusHash: census.Derivation.Original.File.Sha256, DerivedMemberCensusHash: census.Derivation.Derived.Sha256}
	result.PendingOutcomeSha256 = pendingOutcome
	_, err = result.signingBytes(original, profile)
	if err == nil && ctx.Err() == nil && retained != nil && census.OuterPending {
		*retained = &bootstrapSuccessorRestoredMemberView{owner: memberOwner, inventory: inventory, census: census.Census}
	}
	return result, errors.Join(err, ctx.Err())
}

// Original complete-union coverage is checked before any physical derivation;
// map deletion rejects overlaps instead of silently collapsing duplicate owners.
func validateBootstrapSuccessorLocalCoverage(report durablevolume.Inventory, owners []durablevolume.PreparationOwnerPlan) error {
	files := map[string]durablevolume.PreparationFile{}
	attributes := map[durablevolume.PreparationAttributeSpec]bool{}
	for _, entry := range report.Entries {
		if entry.Path != "" {
			if _, found := files[entry.Path]; found {
				return errors.New("successor local coverage repeats original member")
			}
			files[entry.Path] = durablevolume.PreparationFile{Path: entry.Path, Kind: entry.Kind, Mode: entry.Mode, Bytes: entry.Size, Sha256: entry.Sha256}
		}
		for _, attribute := range entry.OwnerAttributes {
			if entry.Path == "" && attribute.Name == durablevolume.PreparationAttribute {
				continue
			}
			path := entry.Path
			if path == "" {
				path = "."
			}
			key := durablevolume.PreparationAttributeSpec{Path: path, Name: attribute.Name}
			if attributes[key] {
				return errors.New("successor local coverage repeats original checkpoint")
			}
			attributes[key] = true
		}
	}
	for _, owner := range owners {
		if owner.ExclusiveRoot || owner.Owner.RelativePath != "." || owner.Owner.Purpose != "restore" || owner.Owner.RestoreCoverage != durablevolume.PreparationCompleteUnion {
			return errors.New("successor local coverage changed its original shared scope")
		}
		for _, file := range owner.Files {
			if expected, ok := files[file.Path]; !ok || expected != file {
				return errors.New("successor local coverage overlaps or changes original bytes")
			}
			delete(files, file.Path)
		}
		for _, attribute := range owner.Attributes {
			if !attributes[attribute] {
				return errors.New("successor local coverage overlaps or invents original checkpoint")
			}
			delete(attributes, attribute)
		}
	}
	if len(files) != 0 || len(attributes) != 0 {
		return errors.New("successor local coverage omits original members or checkpoints")
	}
	return nil
}

// Every co-owner must still have its original bytes and exact derived physical
// head. Shared marker locks prevent cooperating snapshot writers during review.
func inspectBootstrapSuccessorLocalSnapshots(ctx context.Context, path string, owners []durablevolume.PreparationOwnerPlan, inventory durablevolume.Inventory) (resultErr error) {
	storage, err := openMainnetDurableDirectory(ctx, path, durablevolume.ReadOnly)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, storage.close()) }()
	root := storage.directory.File()
	if err := mainnetDurableFlock(int(root.Fd()), unix.LOCK_SH); err != nil {
		return err
	}
	for _, owner := range owners {
		if owner.Owner.Kind == "mainnet-successor-local-members" {
			continue
		}
		spec, _, err := storagePreparationSnapshotSpec(false, owner.Owner)
		if err != nil || spec.LockName == "" {
			return errors.Join(errors.New("successor local co-owner lacks an independent marker"), err)
		}
		if err := func() (resultErr error) {
			fd, err := unix.Openat(int(root.Fd()), spec.LockName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if err != nil {
				return err
			}
			file := os.NewFile(uintptr(fd), filepath.Join(path, spec.LockName))
			defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
			if err := mainnetDurableFlock(fd, unix.LOCK_SH|unix.LOCK_NB); err != nil {
				return err
			}
			attributes, err := inspectStoragePreparationRestore(ctx, root, owner, inventory, false)
			if err != nil || len(attributes) != 1 || attributes[0].Spec.Path != spec.LockName {
				return errors.Join(errors.New("successor local co-owner physical head differs"), err)
			}
			raw := make([]byte, 4097)
			n, err := unix.Fgetxattr(fd, attributes[0].Spec.Name, raw)
			if err != nil || n < 0 || n > 4096 || !bytes.Equal(raw[:n], attributes[0].Raw) {
				return errors.Join(errors.New("successor local co-owner head was lost or changed"), err)
			}
			var opened, named unix.Stat_t
			if err := unix.Fstat(fd, &opened); err != nil {
				return err
			}
			if err := unix.Fstatat(int(root.Fd()), spec.LockName, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				return err
			}
			if opened.Dev != named.Dev || opened.Ino != named.Ino {
				return errors.New("successor local co-owner marker changed during inspection")
			}
			return storage.check(root)
		}(); err != nil {
			return err
		}
	}
	return storage.check(root)
}

// A valid independent signature grants only the separately checked new root.
func (self bootstrapSuccessorLocalRebindApproval) validate(ctx context.Context, original bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile) error {
	return self.validateWithView(ctx, original, profile, nil)
}

// The loader alone requests the read-only view after verifying the same
// signature and complete restore lineage as ordinary owner admission.
func (self bootstrapSuccessorLocalRebindApproval) validateWithView(ctx context.Context, original bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile, retained **bootstrapSuccessorRestoredMemberView) (resultErr error) {
	if retained != nil {
		*retained = nil
		defer func() {
			if resultErr != nil {
				*retained = nil
			}
		}()
	}
	message, err := self.Plan.signingBytes(original, profile)
	key, keyErr := rootReceiptHex(original.Plan.Review.Preparation.Approval.Plan.ApprovalPublicKey, ed25519.PublicKeySize)
	signature, signatureErr := rootOfflineSignatureBytes(self.Signature)
	if self.Schema != bootstrapSuccessorLocalRebindEnvelopeSchema || err != nil || keyErr != nil || signatureErr != nil || !ed25519.Verify(key, message, signature) {
		return errors.Join(errors.New("successor local independent rebind approval is invalid"), err, keyErr, signatureErr)
	}
	expected, err := buildBootstrapSuccessorLocalRebindWithView(ctx, original, profile, self.Plan.RestorePlan, retained)
	if err != nil || expected != self.Plan {
		return errors.Join(errors.New("successor local approval differs from exact restored lineage"), err)
	}
	return nil
}

// The constructor calls this only after original claim, nonce and history checks.
func (self *bootstrapSuccessorExecutionStore) retainLocalRebind() error {
	if self.localRebind == nil || self.deferredLocalRebind {
		return nil
	}
	raw, err := json.Marshal(self.localRebind)
	if err != nil {
		return err
	}
	return self.local.publish(bootstrapSuccessorLocalRebindFile, "local-rebind", raw)
}

// Deferral admits only the explicitly bound original terminal member. It never
// frees the shared publication slot for a new attempt, runtime or policy file.
func (self *bootstrapSuccessorExecutionStore) checkDeferredLocalRebind() error {
	if !self.deferredLocalRebind {
		return nil
	}
	pending := self.local.members.census.Pending
	if self.localRebind == nil || !planSha256(self.localRebind.Plan.PendingOutcomeSha256) || pending == nil || pending.Sha256 != self.localRebind.Plan.PendingOutcomeSha256 ||
		self.pending != "installed" && self.pending != "outer-reverted" {
		return errors.New("successor deferred local adoption lost its original terminal publication")
	}
	return nil
}

// The original terminal publication has joined before its physical receipt is
// admitted. Failure closes this owner; a later opener keeps the exact bytes.
func (self *bootstrapSuccessorExecutionStore) completeDeferredLocalRebind() error {
	if !self.deferredLocalRebind {
		return nil
	}
	if self.local.members.census.Pending != nil || self.pending != "" || self.last.Phase != "installed" && self.last.Phase != "outer-reverted" {
		return errors.New("successor local adoption cannot precede original outcome completion")
	}
	self.deferredLocalRebind = false
	if err := self.retainRegistryRebind(); err != nil {
		return errors.Join(err, self.close())
	}
	if err := self.retainLocalRebind(); err != nil {
		return errors.Join(err, self.close())
	}
	return nil
}

// Review output contains no inferred runtime start, transaction or signer grant.
func (self bootstrapSuccessorLocalRebindPlan) preview(original bootstrapSuccessorExecutionApproval, profile *safeExecutionProfile) (any, error) {
	message, err := self.signingBytes(original, profile)
	if err != nil {
		return nil, err
	}
	return struct {
		Schema       string                            `json:"schema"`
		Plan         bootstrapSuccessorLocalRebindPlan `json:"plan"`
		SigningBytes string                            `json:"signing_bytes"`
	}{Schema: "urnetwork-mainnet-successor-local-rebind-preview-v1", Plan: self, SigningBytes: "0x" + hex.EncodeToString(message)}, nil
}
