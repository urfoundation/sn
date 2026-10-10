// The operator adapter joins its actual installed taskworker, original complete
// signature census and retained resource owners to one signed process repair.
package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"time"

	"github.com/urnetwork/server/v2026/strecovery"
)

// Private transport fields permit original-layer deterministic fixtures. Public
// loading always selects PostgreSQL census and the kernel's real /proc owner.
type repairOperatorEnvelope struct {
	approval repairOperatorApproval
	original repairOperatorHostApproval
	history  []*repairOperatorEnvelope
	reader   strecovery.SnapshotReader
	procRoot string
}

// The public host leaves this nil. Deterministic fixtures replace only the
// physical database/proc transports while all signed source admission runs.
type repairOperatorTransports struct {
	reader   strecovery.SnapshotReader
	procRoot string
}

// Signature and source pin validation precede all journal and service mutation.
func loadRepairOperatorEnvelope(ctx context.Context, raw []byte, key string, host *repairValidatorHost) (*repairOperatorEnvelope, error) {
	return loadRepairOperatorEnvelopeDepth(ctx, raw, key, host, 0, map[string]bool{})
}

// Every signed predecessor stays in the protected closure. A bounded acyclic
// history avoids manufacturing ancestry from a lone acknowledged journal.
func loadRepairOperatorEnvelopeDepth(ctx context.Context, raw []byte, key string, host *repairValidatorHost, depth int, paths map[string]bool) (*repairOperatorEnvelope, error) {
	if ctx == nil || host == nil || depth >= 64 {
		return nil, errors.New("operator repair owner or bounded history is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var approval repairOperatorApproval
	if err := decodePlanJson(raw, &approval); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if err := approval.validate(key); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	p := approval.Plan
	if paths[p.StatePath] {
		return nil, errors.Join(errRpcIntegrity, errors.New("operator repair predecessor history repeats a journal"))
	}
	paths[p.StatePath] = true
	originalRaw, err := readRepairProcessOriginal(ctx, host, p.OriginalApproval, 64*1024)
	if err != nil {
		return nil, err
	}
	var original repairOperatorHostApproval
	if err := decodePlanJson(originalRaw, &original); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	self := &repairOperatorEnvelope{approval: approval, original: original, reader: strecovery.PostgresReader{ReadTimeout: time.Duration(p.timeoutSeconds()) * time.Second}, procRoot: "/proc"}
	if host.operator != nil {
		if host.operator.reader != nil {
			self.reader = host.operator.reader
		}
		if host.operator.procRoot != "" {
			self.procRoot = host.operator.procRoot
		}
	}
	if p.Predecessor != nil {
		prior := p.Predecessor
		priorRaw, err := readRepairProcessOriginal(ctx, host, prior.Approval, 64*1024)
		if err != nil {
			return nil, err
		}
		previous, err := loadRepairOperatorEnvelopeDepth(ctx, priorRaw, prior.PublicKey, host, depth+1, paths)
		if err != nil {
			return nil, err
		}
		old := previous.approval.Plan
		if old.OriginalApproval != p.OriginalApproval || old.OriginalPublicKey != p.OriginalPublicKey || old.StatePath != prior.Journal.Path || !old.IncidentAt.Before(p.IncidentAt) || old.Previous == p.Previous {
			return nil, errors.Join(errRpcIntegrity, errors.New("operator repair predecessor changes original host or incident order"))
		}
		self.history = append([]*repairOperatorEnvelope{previous}, previous.history...)
	}
	if err := self.validate(key); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	return self, nil
}

// Shared fields describe only the physical process; validator source and
// monitor authority are deliberately absent from this separately signed role.
func (self *repairOperatorEnvelope) profile() repairValidatorPlan {
	p, original := self.approval.Plan, self.original.Plan
	return repairValidatorPlan{Role: "operator-" + original.Role, MachineId: original.MachineId, BootId: original.BootId, Unit: repairValidatorUnit{DurableVolumes: &original.DurableVolumes, Name: original.unitName(), File: original.Unit, Binary: original.Binary, Config: p.OriginalApproval, StateDirectory: original.StateDirectory, Uid: original.Uid, Gid: original.Gid}, Systemctl: original.Systemctl, RequiredMounts: original.RequiredMounts, Previous: p.Previous, IncidentId: p.IncidentId, StatePath: p.StatePath, ValidFrom: p.ValidFrom, ExpiresAt: p.ExpiresAt, MaximumStarts: p.MaximumStarts, MaximumObservations: p.MaximumObservations, CommandTimeoutSeconds: p.timeoutSeconds(), MaximumSampleAgeSeconds: p.MaximumSampleAgeSeconds}
}

// The permanent process marker retains this distinct operator authority domain.
func (self *repairOperatorEnvelope) schema() string { return repairOperatorSchema }

// The complete original signed incident identifies every consumed allowance.
func (self *repairOperatorEnvelope) approvalHash() string { return rootObjectHash(self.approval) }

// Expected journals and real local blob quota owners cannot be omitted from an
// incident. A new effect path cannot overlap any original resource or history.
func (self *repairOperatorEnvelope) validate(key string) error {
	if err := self.approval.validate(key); err != nil {
		return err
	}
	p, original := self.approval.Plan, self.original.Plan
	if err := self.original.validate(p.OriginalPublicKey); err != nil {
		return err
	}
	if p.IncidentAt.Before(original.ActivatedAt) || p.Predecessor == nil && p.Previous != original.Generation {
		return errors.New("operator repair differs from original acknowledged activation")
	}
	expected := slices.Clone(original.JournalPaths)
	quota := map[string]bool{}
	for _, root := range original.WritableRoots {
		if root.Blob {
			path := filepath.Join(root.Path, ".capacity-owner.partial")
			expected = append(expected, path)
			quota[path] = true
		}
	}
	slices.Sort(expected)
	if len(expected) != len(p.OriginalFiles) {
		return errors.New("operator incident omits original journals or quota owners")
	}
	for index, path := range expected {
		if p.OriginalFiles[index].File.Path != path || p.OriginalFiles[index].Quota != quota[path] || index > 0 && path == expected[index-1] {
			return errors.New("operator original journal and quota path census differs")
		}
	}
	for _, effect := range []string{p.StatePath, p.StatePath + ".lock", repairProcessClaimPath(self.profile()), repairProcessClaimPath(self.profile()) + ".lock"} {
		for _, path := range self.protectedPaths() {
			if rootPassiveHostPathContains(effect, path) || rootPassiveHostPathContains(path, effect) || effect == path+".lock" {
				return errors.New("operator repair effect overlaps retained original custody")
			}
		}
	}
	return nil
}

// Controller outputs cannot overwrite a secret, original signature, local
// custody tree or any predecessor journal through an ancestor or .lock alias.
func (self *repairOperatorEnvelope) protectedPaths() []string {
	p, original := self.approval.Plan, self.original.Plan
	paths := []string{p.OriginalApproval.Path, p.OriginalCensus.Path, original.Unit.Path, original.Unit.Path + ".sn-control.lock", original.Binary.Path, original.Inspector.Path, original.Systemctl.Path, original.DurableVolumes.Path}
	for _, tree := range original.Resources {
		paths = append(paths, tree.Path)
	}
	for _, root := range original.WritableRoots {
		paths = append(paths, root.Path)
	}
	for _, source := range original.Census.Databases {
		paths = append(paths, source.Connection.Path)
	}
	for _, source := range original.Census.Stores {
		paths = append(paths, source.Directory)
	}
	current := self
	for _, previous := range self.history {
		prior := current.approval.Plan.Predecessor
		if prior != nil {
			paths = append(paths, prior.Approval.Path, prior.Journal.Path, prior.Journal.Path+".lock", repairProcessClaimPath(previous.profile()), repairProcessClaimPath(previous.profile())+".lock", previous.approval.Plan.OriginalCensus.Path)
		}
		current = previous
	}
	return paths
}

// Readiness owns no allowance; every borrowed source is joined before return.
func (self *repairOperatorEnvelope) ready(ctx context.Context, host *repairValidatorHost, now time.Time) (resultErr error) {
	if ctx == nil || host == nil {
		return errors.New("operator repair readiness owner is absent")
	}
	owner, cancel := context.WithTimeout(ctx, time.Duration(self.approval.Plan.timeoutSeconds())*time.Second)
	defer cancel()
	custody, err := self.open(owner, host)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, custody.close()) }()
	return repairProcessReady(owner, self, host, custody, now)
}

// A claim records only this incident's permanent one-start generation custody.
func (self *repairOperatorEnvelope) claim(ctx context.Context, key string, host *repairValidatorHost, now func() time.Time) error {
	return claimRepairProcess(ctx, self, key, host, now)
}

// Reopening an interrupted effect retains its consumed allowance and original
// uncertainty. No caller infers successful start from a later healthy snapshot.
func (self *repairOperatorEnvelope) resume(ctx context.Context, key string, host *repairValidatorHost, now func() time.Time) (status string, completed bool, resultErr error) {
	if ctx == nil || host == nil || now == nil {
		return "", false, errors.New("operator repair resume owner is absent")
	}
	store, err := openRepairProcessStore(ctx, self, key, false, now())
	if err != nil {
		return "", false, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.close()) }()
	return resumeRepairProcess(ctx, store, host, now)
}

// Previous effect journals are retained under their original physical owners.
type repairOperatorCustody struct {
	envelope     *repairOperatorEnvelope
	host         *repairValidatorHost
	original     *strecovery.Archive
	predecessors []*repairProcessStore
}

// The complete signed archive is replayed from raw attempts, never from a
// pending-only monitor census or an operator-provided aggregate.
func (self *repairOperatorEnvelope) open(ctx context.Context, host *repairValidatorHost) (result repairProcessCustody, resultErr error) {
	p := self.approval.Plan
	raw, _, err := readRepairOperatorFile(ctx, host, p.OriginalCensus.Path, host.rootUid, strecovery.MaximumArchiveBytes)
	if err != nil {
		return nil, err
	}
	if monitorReadDigest(raw) != p.OriginalCensus.Sha256 {
		return nil, errors.Join(errRpcIntegrity, errors.New("operator original signed-attempt census bytes differ"))
	}
	var archive strecovery.Archive
	if err := decodePlanJson(raw, &archive); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if err := archive.Validate(ctx); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if !repairOperatorExtendsSelection(self.original.Plan.Census, archive.Selection) {
		return nil, errors.Join(errRpcIntegrity, errors.New("operator incident census changes original database, signer or resource scope"))
	}
	custody := &repairOperatorCustody{envelope: self, host: host, original: &archive}
	transferred := false
	defer func() {
		if !transferred {
			resultErr = errors.Join(resultErr, custody.close())
		}
	}()
	current := self
	for _, previous := range self.history {
		prior := current.approval.Plan.Predecessor
		if prior == nil {
			return nil, errors.Join(errRpcIntegrity, errors.New("operator original repair history is incomplete"))
		}
		if _, err := readRepairProcessOriginal(ctx, host, prior.Journal, 64*1024); err != nil {
			return nil, err
		}
		store, err := openRepairProcessStore(ctx, previous, prior.PublicKey, false, p.ValidFrom)
		if err != nil {
			return nil, err
		}
		custody.predecessors = append(custody.predecessors, store)
		if err := repairProcessClaim(ctx, host, previous, prior.PublicKey, false); err != nil {
			return nil, err
		}
		record, err := store.load(ctx)
		if err != nil {
			return nil, err
		}
		if record.Generation == nil || record.StartAt.IsZero() || *record.Generation != current.approval.Plan.Previous || record.StartAt.After(current.approval.Plan.IncidentAt) {
			return nil, errors.Join(errRpcIntegrity, errors.New("operator repair predecessor did not acknowledge this exact generation"))
		}
		// Every earlier signed archive and quota inode remains original custody.
		// A fresh signature cannot reset an old journal's unresolved attempts or
		// authorize recreation of its physical local quota owner.
		priorRaw, _, err := readRepairOperatorFile(ctx, host, previous.approval.Plan.OriginalCensus.Path, host.rootUid, strecovery.MaximumArchiveBytes)
		if err != nil {
			return nil, err
		}
		if monitorReadDigest(priorRaw) != previous.approval.Plan.OriginalCensus.Sha256 {
			return nil, errors.Join(errRpcIntegrity, errors.New("operator predecessor archive bytes differ"))
		}
		var priorArchive strecovery.Archive
		if err := decodePlanJson(priorRaw, &priorArchive); err != nil {
			return nil, errors.Join(errRpcIntegrity, err)
		}
		if err := priorArchive.Validate(ctx); err != nil {
			return nil, errors.Join(errRpcIntegrity, err)
		}
		if err := retainRepairOperatorCensus(&priorArchive, &archive); err != nil {
			return nil, errors.Join(errRpcIntegrity, err)
		}
		for index, retained := range previous.approval.Plan.OriginalFiles {
			if retained.Quota && retained != p.OriginalFiles[index] {
				return nil, errors.Join(errRpcIntegrity, errors.New("operator successor resets an original physical quota owner"))
			}
		}
		current = previous
	}
	if current.approval.Plan.Predecessor != nil || current.approval.Plan.Previous != self.original.Plan.Generation {
		return nil, errors.Join(errRpcIntegrity, errors.New("operator repair history does not reach original activation"))
	}
	if err := custody.check(ctx); err != nil {
		return nil, err
	}
	transferred = true
	return custody, nil
}

// This checks local immutable authority and existing owners only. Complete
// database reads occur at the stopped precondition and continued postcondition.
func (self *repairOperatorCustody) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e, p := self.envelope, self.envelope.original.Plan
	if _, err := readRepairProcessOriginal(ctx, self.host, e.approval.Plan.OriginalApproval, 64*1024); err != nil {
		return err
	}
	raw, _, err := readRepairOperatorFile(ctx, self.host, e.approval.Plan.OriginalCensus.Path, self.host.rootUid, strecovery.MaximumArchiveBytes)
	if err != nil {
		return err
	}
	if monitorReadDigest(raw) != e.approval.Plan.OriginalCensus.Sha256 {
		return errors.Join(errRpcIntegrity, errors.New("operator original archive custody changed"))
	}
	for _, tree := range p.Resources {
		hash, err := inspectRepairOperatorTree(ctx, self.host, tree, p)
		if err != nil {
			return err
		}
		if hash != tree.Sha256 {
			return errors.Join(errRpcIntegrity, errors.New("operator complete WARP resource closure changed"))
		}
	}
	if err := inspectRepairOperatorRoots(ctx, self.host, p); err != nil {
		return err
	}
	if err := self.source(ctx); err != nil {
		return err
	}
	for _, store := range self.predecessors {
		if err := store.validateOwner(); err != nil {
			return err
		}
	}
	return nil
}

// Taskworker uses its actual argument grammar and exact signed environment.
func (self *repairOperatorCustody) inspect(ctx context.Context) (repairValidatorManager, error) {
	p := self.envelope.original.Plan
	return self.host.inspectCommandEnvironment(ctx, self.envelope.profile(), p.arguments(), nil, p.environment())
}

// No database or filesystem repair is attempted. A changed census remains a
// separate reconciliation task with every original byte retained.
func (self *repairOperatorCustody) beforeStart(ctx context.Context, now time.Time) error {
	p, original := self.envelope.approval.Plan, self.envelope.original.Plan
	if now.Before(p.IncidentAt) {
		return errors.Join(errRpcIntegrity, errors.New("operator incident clock moved backwards"))
	}
	if err := inspectRepairOperatorRetained(ctx, self.host, original, p.OriginalFiles, true); err != nil {
		return err
	}
	current, err := strecovery.Collect(ctx, self.original.Selection, self.envelope.reader)
	if err != nil {
		return repairOperatorCensusError(err)
	}
	if current.CensusHash != self.original.CensusHash {
		return errors.Join(errRpcIntegrity, errors.New("operator complete original signed-attempt census changed before start"))
	}
	return self.inspectStorage(ctx)
}

// The application resumes its own unchanged journals and policies. This fixed
// effect cannot select init-tasks, SQL, a signer, another binary or a new quota.
func (self *repairOperatorCustody) start(ctx context.Context, dispatchCheck func() error) error {
	if dispatchCheck == nil {
		return errors.New("operator start dispatch authority check is absent")
	}
	if err := self.inspectStorage(ctx); err != nil {
		return err
	}
	if err := dispatchCheck(); err != nil {
		return err
	}
	_, err := self.host.command(ctx, self.envelope.profile(), "--system", "--no-pager", "--no-ask-password", "--job-mode=fail", "start", "--", self.envelope.original.Plan.unitName())
	return err
}

// Original borrowed journals are closed in reverse acquisition order; closing
// never deletes a marker or replenishes an uncertain effect allowance.
func (self *repairOperatorCustody) close() error {
	var result error
	for index := len(self.predecessors) - 1; index >= 0; index-- {
		result = errors.Join(result, self.predecessors[index].close())
	}
	self.predecessors = nil
	return result
}
