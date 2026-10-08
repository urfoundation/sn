// Passive root repair is a new incident signature over the original signed
// runtime and consumed launch history. It cannot acquire a native signer.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"
)

const repairRootPassiveSchema = "urnetwork-mainnet-root-passive-repair-v1"
const repairRootPassiveDomain = "urnetwork-mainnet-root-passive-repair-approval-v1"

// Later incidents name the previous actual signed repair and its acknowledged
// journal. An unacknowledged effect cannot become a new approved generation.
type repairProcessPredecessor struct {
	Approval  planFileReference `json:"approval"`
	PublicKey string            `json:"independent_public_key"`
	Journal   planFileReference `json:"journal"`
}

// Every input is an original retained file. The controller neither writes a
// runtime nor copies a bootstrap journal to construct a new allowance.
type repairRootPassivePlan struct {
	OriginalApproval        planFileReference         `json:"original_host_approval"`
	OriginalPublicKey       string                    `json:"original_host_public_key"`
	OriginalJournal         planFileReference         `json:"original_host_journal"`
	OriginalCheckpoint      planFileReference         `json:"original_monitor_checkpoint"`
	Checkpoint              monitorCheckpointRecord   `json:"original_checkpoint_record"`
	Predecessor             *repairProcessPredecessor `json:"previous_repair,omitempty"`
	Previous                repairValidatorGeneration `json:"previous"`
	IncidentAt              time.Time                 `json:"incident_observed_at"`
	IncidentId              string                    `json:"incident_id"`
	StatePath               string                    `json:"state_path"`
	ValidFrom               time.Time                 `json:"valid_from"`
	ExpiresAt               time.Time                 `json:"expires_at"`
	MaximumStarts           uint8                     `json:"maximum_starts"`
	MaximumObservations     uint32                    `json:"maximum_observations"`
	ReadTimeoutSeconds      uint32                    `json:"read_timeout_seconds"`
	MaximumSampleAgeSeconds uint32                    `json:"maximum_sample_age_seconds"`
}

// Independent key input verifies this role domain separately from activation.
type repairRootPassiveApproval struct {
	Schema    string                `json:"schema"`
	Plan      repairRootPassivePlan `json:"plan"`
	Signature string                `json:"signature_ed25519"`
}

// Loaded exact activation bytes are retained separately from the incident.
type repairRootPassiveEnvelope struct {
	approval    repairRootPassiveApproval
	original    rootPassiveHostApproval
	preparation *bootstrapChainPreparation
	// Only private fixtures vary the real host's root uid and gid.
	uid uint32
	gid uint32
}

// A verified incident without its complete original graph cannot establish
// shared output separation. The cause retains unavailable or hard precedence.
type repairRootClosureError struct {
	cause error
}

// The diagnostic adds no new integrity claim or retry authority.
func (self *repairRootClosureError) Error() string {
	return "original passive root custody closure is unavailable: " + self.cause.Error()
}

// Typed original read and integrity causes remain visible to the controller.
func (self *repairRootClosureError) Unwrap() error { return self.cause }

// This digest identifies the observed generation and its original continuity.
// It excludes the new state path, which cannot create another generation claim.
func (self repairRootPassivePlan) incidentId() string {
	return rootObjectHash(struct {
		Schema      string                    `json:"schema"`
		Host        planFileReference         `json:"host"`
		Checkpoint  planFileReference         `json:"checkpoint"`
		Predecessor *repairProcessPredecessor `json:"previous_repair,omitempty"`
		Previous    repairValidatorGeneration `json:"previous"`
		ObservedAt  time.Time                 `json:"observed_at"`
	}{Schema: repairRootPassiveSchema, Host: self.OriginalJournal, Checkpoint: self.OriginalCheckpoint, Predecessor: self.Predecessor, Previous: self.Previous, ObservedAt: self.IncidentAt})
}

// Zero selects the normal five-minute total owner, never an unbounded read.
func (self repairRootPassivePlan) timeoutSeconds() uint32 {
	if self.ReadTimeoutSeconds == 0 {
		return 300
	}
	return self.ReadTimeoutSeconds
}

// Signing does not include its own signature or another role's domain.
func (self repairRootPassiveApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	return append([]byte(repairRootPassiveDomain+"\x00"), raw...), err
}

// The incident consumes exactly one start and a bounded number of observations.
func (self repairRootPassiveApproval) validate(key string) error {
	p := self.Plan
	if self.Schema != repairRootPassiveSchema || !rootCanonicalHash(key) || !rootCanonicalHash(p.OriginalPublicKey) || p.MaximumStarts != 1 || p.MaximumObservations < 1 || p.MaximumObservations > 1024 || p.timeoutSeconds() < 60 || p.timeoutSeconds() > 900 || p.MaximumSampleAgeSeconds < 1 || p.MaximumSampleAgeSeconds > 600 || p.IncidentAt.IsZero() || p.ValidFrom.IsZero() || p.IncidentAt.After(p.ValidFrom) || !p.ExpiresAt.After(p.ValidFrom) || p.ExpiresAt.Sub(p.ValidFrom) > 24*time.Hour || !repairValidatorHex(p.Previous.InvocationId, 16) || p.Previous.Pid <= 1 || p.Previous.StartedUsec == 0 || p.IncidentId != p.incidentId() {
		return errors.New("passive root repair requires one exact incident, generation and bounded allowance")
	}
	refs := []planFileReference{p.OriginalApproval, p.OriginalJournal, p.OriginalCheckpoint}
	if p.Predecessor != nil {
		if !rootCanonicalHash(p.Predecessor.PublicKey) {
			return errors.New("passive root repair predecessor key is absent")
		}
		refs = append(refs, p.Predecessor.Approval, p.Predecessor.Journal)
	}
	paths := map[string]bool{}
	for _, ref := range refs {
		if !repairValidatorPath(ref.Path) || !planSha256(ref.Sha256) || paths[ref.Path] {
			return errors.New("passive root repair original references are incomplete or overlap")
		}
		paths[ref.Path], paths[ref.Path+".lock"] = true, true
	}
	if !repairValidatorPath(p.StatePath) || paths[p.StatePath] || paths[p.StatePath+".lock"] {
		return errors.New("passive root repair state overlaps an original input")
	}
	raw, err := self.signingBytes()
	public, _ := hex.DecodeString(key[2:])
	signature, decodeErr := hex.DecodeString(self.Signature)
	if err != nil || decodeErr != nil || len(signature) != ed25519.SignatureSize || hex.EncodeToString(signature) != self.Signature || !ed25519.Verify(public, raw, signature) {
		return errors.New("passive root repair independent signature differs")
	}
	return nil
}

// Protected reads retain their original error. Only returned mismatching bytes
// establish an integrity contradiction.
func readRepairProcessOriginal(ctx context.Context, host *repairValidatorHost, reference planFileReference, maximum int64) ([]byte, error) {
	raw, err := host.read(ctx, reference.Path, host.rootUid, maximum, true)
	if err != nil {
		return nil, repairValidatorObservationError("cannot read original process authority", err, true)
	}
	if monitorReadDigest(raw) != reference.Sha256 {
		return nil, errors.Join(errRpcIntegrity, errors.New("original process authority bytes differ"))
	}
	return raw, nil
}

// The original host signature is independently checked from its actual file.
// A root repair envelope cannot manufacture a new passive runtime declaration.
func loadRepairRootPassiveEnvelope(ctx context.Context, raw []byte, key string, host *repairValidatorHost) (_ *repairRootPassiveEnvelope, resultErr error) {
	if ctx == nil || host == nil {
		return nil, errors.New("passive root repair load owner is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var approval repairRootPassiveApproval
	if err := decodePlanJson(raw, &approval); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if err := approval.validate(key); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	closureLoaded := false
	defer func() {
		if resultErr != nil && !closureLoaded {
			if rootMonitorStartupPending(resultErr) {
				resultErr = mainnetDurableUnavailable("cannot observe original root custody closure", resultErr)
			}
			resultErr = &repairRootClosureError{cause: resultErr}
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(approval.Plan.timeoutSeconds())*time.Second)
	defer cancel()
	originalRaw, err := readRepairProcessOriginal(ctx, host, approval.Plan.OriginalApproval, 64*1024)
	if err != nil {
		return nil, err
	}
	var original rootPassiveHostApproval
	if err := decodePlanJson(originalRaw, &original); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	self := &repairRootPassiveEnvelope{approval: approval, original: original, uid: host.rootUid, gid: host.rootGid}
	if err := self.validate(key); err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	preparation, err := loadRootPassiveHostPreparation(ctx, original)
	if err != nil {
		return self, err
	}
	self.preparation = &preparation
	closureLoaded = true
	if err := self.validatePaths(); err != nil {
		return self, errors.Join(errRpcIntegrity, err)
	}
	return self, nil
}

// No source fields from the validator protocol are populated or authorized.
func (self *repairRootPassiveEnvelope) profile() repairValidatorPlan {
	p, original := self.approval.Plan, self.original.Plan
	return repairValidatorPlan{Role: "root-passive", MachineId: original.MachineId, BootId: original.BootId, Unit: repairValidatorUnit{DurableVolumes: original.DurableVolumes, Name: rootPassiveHostUnitName, File: original.Unit, Binary: original.Binary, Config: original.Runtime, StateDirectory: original.CheckpointDirectory, Uid: self.uid, Gid: self.gid}, Systemctl: original.Systemctl, RequiredMounts: original.RequiredMounts, Previous: p.Previous, IncidentId: p.IncidentId, StatePath: p.StatePath, ValidFrom: p.ValidFrom, ExpiresAt: p.ExpiresAt, MaximumStarts: p.MaximumStarts, MaximumObservations: p.MaximumObservations, CommandTimeoutSeconds: p.timeoutSeconds(), MaximumSampleAgeSeconds: p.MaximumSampleAgeSeconds}
}

// This domain is retained in the permanent marker and every journal record.
func (self *repairRootPassiveEnvelope) schema() string { return repairRootPassiveSchema }

// The complete original signed incident remains the durable authority key.
func (self *repairRootPassiveEnvelope) approvalHash() string { return rootObjectHash(self.approval) }

// New effect paths cannot enter the observer's sole writable state directory.
func (self *repairRootPassiveEnvelope) validate(key string) error {
	if err := self.approval.validate(key); err != nil {
		return err
	}
	p, original := self.approval.Plan, self.original.Plan
	if err := self.original.validate(p.OriginalPublicKey); err != nil {
		return err
	}
	if p.OriginalJournal.Path != original.StatePath || filepath.Dir(p.OriginalCheckpoint.Path) != original.CheckpointDirectory || original.DurableVolumes == nil {
		return errors.New("passive root repair differs from original process custody")
	}
	return self.validatePaths()
}

// The retained bootstrap graph excludes repair journals and generation markers
// from every nested authority path before any original owner can be opened.
func (self *repairRootPassiveEnvelope) validatePaths() error {
	p := self.approval.Plan
	protected := self.protectedPaths()
	for _, effect := range []string{p.StatePath, p.StatePath + ".lock", repairProcessClaimPath(self.profile()), repairProcessClaimPath(self.profile()) + ".lock"} {
		for _, path := range protected {
			for _, retained := range []string{path, path + ".lock"} {
				if rootPassiveHostPathContains(effect, retained) || rootPassiveHostPathContains(retained, effect) {
					return errors.New("passive root repair effect overlaps retained authority")
				}
			}
		}
	}
	return nil
}

// Controller output validation borrows this actual original closure. The
// checkpoint directory also protects all of its existing lifetime markers.
func (self *repairRootPassiveEnvelope) protectedPaths() []string {
	p, original := self.approval.Plan, self.original.Plan
	paths := []string{p.OriginalApproval.Path, p.OriginalJournal.Path, p.OriginalCheckpoint.Path, original.Preparation.Path, original.Runtime.Path, original.Unit.Path, original.Unit.Path + ".sn-control.lock", original.Binary.Path, original.Systemctl.Path, original.CheckpointDirectory, original.DurableVolumes.Path}
	if p.Predecessor != nil {
		paths = append(paths, p.Predecessor.Approval.Path, p.Predecessor.Journal.Path)
	}
	if self.preparation != nil {
		preparation := self.preparation
		config := preparation.Plan.Config
		paths = append(paths, preparation.Root.ConfigPath, preparation.Root.ServiceInput.Path, config.RootValidator.Approval.Path, config.OwnerTrimPolicy.Path, config.OwnerTrimPlan.Path, config.Contracts.Path, preparation.Contracts.Config.Plan.Artifacts.Path)
		paths = append(paths, preparation.childPaths()...)
		for _, role := range config.Validators {
			paths = append(paths, role.Config.Path)
		}
		for _, inspection := range preparation.Plan.ValidatorInspections {
			paths = append(paths, inspection.DeclaredPaths...)
			paths = append(paths, inspection.ApprovalReference.Path)
		}
	}
	return paths
}

// A readiness read owns no allowance and joins every borrowed original owner.
func (self *repairRootPassiveEnvelope) ready(ctx context.Context, host *repairValidatorHost, now time.Time) (resultErr error) {
	if ctx == nil || host == nil {
		return errors.New("passive root repair read owner is absent")
	}
	owner, cancel := context.WithTimeout(ctx, time.Duration(self.profile().CommandTimeoutSeconds)*time.Second)
	defer cancel()
	custody, err := self.open(owner, host)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, custody.close()) }()
	return repairProcessReady(owner, self, host, custody, now)
}

// This only retains the new explicit incident allowance; it performs no start.
func (self *repairRootPassiveEnvelope) claim(ctx context.Context, key string, host *repairValidatorHost, now func() time.Time) error {
	return claimRepairProcess(ctx, self, key, host, now)
}

// Reopen always selects the existing journal, including uncertain consumption.
func (self *repairRootPassiveEnvelope) resume(ctx context.Context, key string, host *repairValidatorHost, now func() time.Time) (status string, completed bool, resultErr error) {
	if ctx == nil || host == nil || now == nil {
		return "", false, errors.New("passive root repair resume owner is absent")
	}
	store, err := openRepairProcessStore(ctx, self, key, false, now())
	if err != nil {
		return "", false, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.close()) }()
	return resumeRepairProcess(ctx, store, host, now)
}
