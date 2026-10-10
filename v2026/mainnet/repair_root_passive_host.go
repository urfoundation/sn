// The original passive preparation and host journal remain borrowed through
// each repair operation. The monitor keeps its own continuing checkpoint.
package main

import (
	"context"
	"errors"
	"path/filepath"
	"time"
)

// Original and predecessor owners are read-only borrowers, never new claims.
type repairRootPassiveCustody struct {
	envelope    *repairRootPassiveEnvelope
	host        *rootPassiveHost
	preparation bootstrapChainPreparation
	original    *rootPassiveHostStore
	predecessor *repairProcessStore
	expected    identityExpectation
}

// All preparatory child journals must still be the original physical owners.
// Later repair history must acknowledge this exact prior process, never a pid
// inferred from an uncertain command or a newly copied state file.
func (self *repairRootPassiveEnvelope) open(ctx context.Context, host *repairValidatorHost) (result repairProcessCustody, resultErr error) {
	p := self.approval.Plan
	preparation, err := loadRootPassiveHostPreparation(ctx, self.original)
	if err != nil {
		return nil, err
	}
	verified := *self
	verified.preparation = &preparation
	if err := verified.validatePaths(); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if preparation.Root.PassiveService.CheckpointPath != p.OriginalCheckpoint.Path {
		return nil, errors.Join(errRpcIntegrity, errors.New("root repair checkpoint differs from original runtime"))
	}
	policy := preparation.Root.PassiveService.Policy
	expected := identityExpectation{NativeChain: policy.NativeChain, GenesisHash: policy.GenesisHash, EvmChainId: policy.EvmChainId}
	if err := (&monitorCheckpointStore{expected: expected}).validate(p.Checkpoint); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	rootHost := &rootPassiveHost{files: &validatorActivationHost{host: host, unitDirectory: filepath.Dir(self.original.Plan.Unit.Path)}, gid: self.gid}
	if err := errors.Join(rootHost.authority(ctx, self.original.Plan, preparation), ctx.Err()); err != nil {
		return nil, err
	}
	prepared, err := openBootstrapChainReadinessState(ctx, preparation)
	if err != nil {
		return nil, err
	}
	original, err := openRootPassiveHostStore(ctx, self.original, p.OriginalPublicKey, false, p.ValidFrom, prepared, rootObjectHash(policy))
	if err != nil {
		return nil, errors.Join(err, prepared.close())
	}
	custody := &repairRootPassiveCustody{envelope: self, host: rootHost, preparation: preparation, original: original, expected: expected}
	transferred := false
	defer func() {
		if !transferred {
			resultErr = errors.Join(resultErr, custody.close())
		}
	}()
	if _, err := readRepairProcessOriginal(ctx, host, p.OriginalJournal, 64*1024); err != nil {
		return nil, err
	}
	record, err := original.load(ctx)
	if err != nil {
		return nil, err
	}
	if record.Generation == nil || record.StartAt.IsZero() || !record.Installed || p.IncidentAt.Before(record.StartAt) {
		return nil, errors.Join(errRpcIntegrity, errors.New("root repair original start is not durably acknowledged"))
	}
	previous := record.Generation
	if p.Predecessor != nil {
		prior := p.Predecessor
		raw, err := readRepairProcessOriginal(ctx, host, prior.Approval, 64*1024)
		if err != nil {
			return nil, err
		}
		envelope, err := loadRepairRootPassiveEnvelope(ctx, raw, prior.PublicKey, host)
		if err != nil {
			return nil, err
		}
		envelope.uid, envelope.gid = self.uid, self.gid
		old := envelope.approval.Plan
		if old.OriginalApproval != p.OriginalApproval || old.OriginalJournal != p.OriginalJournal || old.OriginalPublicKey != p.OriginalPublicKey || prior.Journal.Path != old.StatePath || old.IncidentAt.After(p.IncidentAt) || old.Previous == p.Previous {
			return nil, errors.Join(errRpcIntegrity, errors.New("root repair predecessor belongs to another original authority"))
		}
		if _, err := readRepairProcessOriginal(ctx, host, prior.Journal, 64*1024); err != nil {
			return nil, err
		}
		store, err := openRepairProcessStore(ctx, envelope, prior.PublicKey, false, p.ValidFrom)
		if err != nil {
			return nil, err
		}
		custody.predecessor = store
		if err := repairProcessClaim(ctx, host, envelope, prior.PublicKey, false); err != nil {
			return nil, err
		}
		priorRecord, err := store.load(ctx)
		if err != nil {
			return nil, err
		}
		if priorRecord.Generation == nil || priorRecord.StartAt.IsZero() || p.IncidentAt.Before(priorRecord.StartAt) {
			return nil, errors.Join(errRpcIntegrity, errors.New("root repair cannot adopt an uncertain predecessor"))
		}
		previous = priorRecord.Generation
	}
	if *previous != p.Previous {
		return nil, errors.Join(errRpcIntegrity, errors.New("root repair previous generation differs from original acknowledgment"))
	}
	if err := custody.check(ctx); err != nil {
		return nil, err
	}
	transferred = true
	return custody, nil
}

// Repeated reads do not write the original activation or predecessor journals.
func (self *repairRootPassiveCustody) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := self.envelope.approval.Plan
	if _, err := readRepairProcessOriginal(ctx, self.host.files.host, p.OriginalApproval, 64*1024); err != nil {
		return err
	}
	if err := errors.Join(self.original.custody.checkpoint(ctx), self.original.validateOwner(), self.host.authority(ctx, self.envelope.original.Plan, self.preparation), ctx.Err()); err != nil {
		return err
	}
	if self.predecessor != nil {
		return self.predecessor.validateOwner()
	}
	return nil
}

// The existing exact sandbox and fixed signer-free argv remain authoritative.
func (self *repairRootPassiveCustody) inspect(ctx context.Context) (repairValidatorManager, error) {
	p := self.envelope.original.Plan
	return self.host.files.host.inspectCommand(ctx, self.envelope.profile(), p.arguments(), p.sandbox())
}

// A retained checkpoint and its lifetime marker are required before restart.
// Opening the existing owner proves no other monitor still owns this state.
func (self *repairRootPassiveCustody) beforeStart(ctx context.Context, now time.Time) error {
	p := self.envelope.approval.Plan
	raw, err := self.host.files.host.readRootCheckpoint(ctx, p.OriginalCheckpoint.Path)
	if err != nil {
		return repairValidatorObservationError("cannot read original root checkpoint", err, true)
	}
	if monitorReadDigest(raw) != p.OriginalCheckpoint.Sha256 {
		return errors.Join(errRpcIntegrity, errors.New("root checkpoint differs from original byte pin"))
	}
	var record monitorCheckpointRecord
	if err := decodePlanJson(raw, &record); err != nil {
		return errors.Join(errRpcIntegrity, err)
	}
	if rootObjectHash(record) != rootObjectHash(p.Checkpoint) {
		return errors.Join(errRpcIntegrity, errors.New("root repair signed checkpoint record differs from original bytes"))
	}
	for _, value := range []string{record.LastProgressAt, record.LastSuccessAt, record.UnavailableSince} {
		if value != "" {
			stamp, err := time.Parse(time.RFC3339Nano, value)
			if err != nil || stamp.After(now) {
				return errors.Join(errRpcIntegrity, errors.New("root repair original checkpoint clock differs"), err)
			}
		}
	}
	store, err := openMonitorCheckpoint(p.OriginalCheckpoint.Path, self.expected, ctx)
	if err != nil {
		return err
	}
	_, err = store.load()
	return errors.Join(err, store.close(), ctx.Err())
}

// Root continues through its original executable and storage-inspection gate.
func (self *repairRootPassiveCustody) start(ctx context.Context, dispatchCheck func() error) error {
	return self.host.files.host.start(ctx, self.envelope.profile(), dispatchCheck)
}

// Fresh local checkpoint progress is attributed only while the acknowledged
// generation stays running. Finality remains the observer's original authority.
func (self *repairRootPassiveCustody) progress(ctx context.Context, startedAt, now time.Time) (string, error) {
	p := self.envelope.approval.Plan
	raw, err := self.host.files.host.readRootCheckpoint(ctx, p.OriginalCheckpoint.Path)
	if err != nil {
		return "", repairValidatorObservationError("cannot read continued root checkpoint", err, true)
	}
	var record monitorCheckpointRecord
	if err := decodePlanJson(raw, &record); err != nil {
		return "", errors.Join(errRpcIntegrity, err)
	}
	if err := (&monitorCheckpointStore{expected: self.expected}).validate(record); err != nil {
		return "", errors.Join(errRpcIntegrity, err)
	}
	if record.FinalizedAt < p.Checkpoint.FinalizedAt || record.FinalizedAt == p.Checkpoint.FinalizedAt && record.FinalizedHash != p.Checkpoint.FinalizedHash {
		return "", errors.Join(errRpcIntegrity, errors.New("continued root checkpoint contradicts original finality"))
	}
	for _, value := range []string{record.LastProgressAt, record.LastSuccessAt, record.UnavailableSince} {
		if value != "" {
			stamp, err := time.Parse(time.RFC3339Nano, value)
			if err != nil || stamp.After(now) {
				return "", errors.Join(errRpcIntegrity, errors.New("continued root checkpoint clock differs"), err)
			}
		}
	}
	successAt, err := time.Parse(time.RFC3339Nano, record.LastSuccessAt)
	if err != nil || successAt.Before(startedAt) || now.Sub(successAt) > time.Duration(p.MaximumSampleAgeSeconds)*time.Second || record.UnavailableSince != "" {
		return "", errRepairProcessPending
	}
	return monitorReadDigest(raw), nil
}

// Closing borrowed owners preserves every original marker and consumed start.
func (self *repairRootPassiveCustody) close() error {
	var err error
	if self.predecessor != nil {
		err = self.predecessor.close()
		self.predecessor = nil
	}
	if self.original != nil {
		err = errors.Join(err, self.original.close())
		self.original = nil
	}
	return err
}
