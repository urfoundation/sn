//go:build linux || darwin

// Strict staging refresh admits actual anchored, eligible hotkeys. An explicit
// provisional testnet mode instead grants the retained private context allowance.
// Upload requests perform no RPC.
// Ordinary accounts and duplicate reads cannot take a fresh owner's slot.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

// Ordinary deployment-provisioned references seed discovery; the separate
// provisional retained mode explicitly grants these exact contexts authority.
// The bounded complete event scan also discovers later external validators.
// Operators must provision enough finite scan capacity for their journal;
// exceeding it refuses refresh rather than silently installing an allowlist.
type ValidatorUploadAdmissionConfig struct {
	Deployment              ValidatorUploadDeployment `json:"deployment" yaml:"deployment"`
	ReplicaNoID             uint64                    `json:"replica_no_id" yaml:"replica_no_id"`
	ActivationContexts      []ReleaseEvidenceV2File   `json:"activation_contexts" yaml:"activation_contexts"`
	ProductionRuntimeConfig ReleaseEvidenceV2File     `json:"production_runtime_config,omitzero" yaml:"production_runtime_config,omitempty"`
	// Provisional testnet continuity discovers only these exact references;
	// their anchored authentication and current eligibility remain mandatory.
	ProvisionalSeededDiscoveryOnly bool `json:"provisional_seeded_discovery_only,omitempty" yaml:"provisional_seeded_discovery_only,omitempty"`
	// A separately authorized testnet staging allowance uses exact private
	// context pins without asserting historical or current chain eligibility.
	ProvisionalRetainedContextAuthority bool   `json:"provisional_retained_context_authority,omitempty" yaml:"provisional_retained_context_authority,omitempty"`
	ProvisionalRuntimeCompatibility     string `json:"provisional_runtime_compatibility,omitempty" yaml:"provisional_runtime_compatibility,omitempty"`
	RuntimeObservationDir               string `json:"runtime_observation_dir,omitempty" yaml:"runtime_observation_dir,omitempty"`
	MaximumContextBytes                 uint64 `json:"maximum_context_bytes" yaml:"maximum_context_bytes"`
	MaximumOwners                       uint64 `json:"maximum_owners" yaml:"maximum_owners"`
	BlocksPerRange                      uint64 `json:"blocks_per_range" yaml:"blocks_per_range"`
	MaximumRanges                       uint64 `json:"maximum_ranges" yaml:"maximum_ranges"`
	MaximumEventsPerRange               uint64 `json:"maximum_events_per_range" yaml:"maximum_events_per_range"`
	RefreshSeconds                      uint64 `json:"refresh_seconds" yaml:"refresh_seconds"`
	MaximumRefreshSeconds               uint64 `json:"maximum_refresh_seconds" yaml:"maximum_refresh_seconds"`
	MaximumHeadAgeSeconds               uint64 `json:"maximum_head_age_seconds" yaml:"maximum_head_age_seconds"`
	MaximumIntentSeconds                uint64 `json:"maximum_intent_seconds" yaml:"maximum_intent_seconds"`
	FreshActivePerOwner                 uint64 `json:"fresh_active_per_owner" yaml:"fresh_active_per_owner"`
	RetryActivePerOwner                 uint64 `json:"retry_active_per_owner" yaml:"retry_active_per_owner"`
}

// Capacity products are checked before allocation or file/RPC work. These
// transport ceilings are not mainnet population or per-hour budget defaults.
func (self ValidatorUploadAdmissionConfig) Validate() error {
	if err := self.Deployment.Validate(); err != nil {
		return err
	}
	if self.Deployment.ChainID == 964 {
		if err := self.ProductionRuntimeConfig.Validate(maximumReleaseConfigBytes); err != nil {
			return fmt.Errorf("mainnet staging requires an independently pinned production runtime config: %w", err)
		}
	} else if self.ProductionRuntimeConfig != (ReleaseEvidenceV2File{}) {
		return errors.New("production staging runtime authority requires mainnet chain 964")
	}
	if self.ProvisionalRuntimeCompatibility != "" || self.RuntimeObservationDir != "" {
		if self.ProvisionalRuntimeCompatibility != crv4.ProvisionalRuntimeCompatibilityProfile || self.Deployment.ChainID != 945 || types.Hash(self.Deployment.GenesisHash).Hex() != provisionalRuntimeTestnetGenesis || !filepath.IsAbs(self.RuntimeObservationDir) {
			return errors.New("provisional staging runtime requires the consumed profile, testnet chain 945, exact testnet genesis and absolute observation directory")
		}
	}
	if self.ProvisionalSeededDiscoveryOnly && (self.Deployment.ChainID != 945 || len(self.ActivationContexts) == 0) {
		return errors.New("provisional seeded discovery requires testnet chain 945 and explicit activation contexts")
	}
	if self.ProvisionalRetainedContextAuthority && (!self.ProvisionalSeededDiscoveryOnly || self.Deployment.ChainID != 945 || len(self.ActivationContexts) != provisionalRetainedValidatorUploadContexts || self.MaximumOwners != provisionalRetainedValidatorUploadContexts) {
		return errors.New("provisional retained staging requires testnet chain 945, seeded discovery and exactly four pinned contexts and owners")
	}
	return self.ValidateCapacity()
}

// Capacity-only preflight cannot authorize a deployment or an upload. Fresh
// setup templates reuse the same finite checks before generated pins exist.
func (self ValidatorUploadAdmissionConfig) ValidateCapacity() error {
	if self.ReplicaNoID == 0 || self.MaximumOwners == 0 || self.MaximumOwners > 65536 ||
		self.MaximumContextBytes == 0 || self.MaximumContextBytes > 1024*1024 || uint64(len(self.ActivationContexts)) > self.MaximumOwners ||
		self.BlocksPerRange == 0 || self.BlocksPerRange > math.MaxInt64 || self.MaximumRanges == 0 || self.MaximumRanges > 65536 || self.BlocksPerRange > math.MaxInt64/self.MaximumRanges ||
		self.MaximumEventsPerRange == 0 || self.MaximumEventsPerRange > 65536 || self.MaximumEventsPerRange > 65536/self.MaximumRanges ||
		self.FreshActivePerOwner == 0 || self.FreshActivePerOwner > 8 || self.RetryActivePerOwner == 0 || self.RetryActivePerOwner > 8 {
		return errors.New("validator staging census, discovery or active-slot bounds are invalid")
	}
	for _, value := range []uint64{self.RefreshSeconds, self.MaximumRefreshSeconds, self.MaximumHeadAgeSeconds, self.MaximumIntentSeconds} {
		if value == 0 || value > 3600 {
			return errors.New("validator staging freshness or operation bound is invalid")
		}
	}
	if self.RefreshSeconds >= self.MaximumHeadAgeSeconds || self.MaximumRefreshSeconds >= self.MaximumHeadAgeSeconds {
		return errors.New("validator staging refresh cannot fit its freshness window")
	}
	seen := make(map[string]bool, len(self.ActivationContexts))
	for _, reference := range self.ActivationContexts {
		if err := reference.Validate(self.MaximumContextBytes); err != nil {
			return err
		}
		if seen[reference.Path] {
			return errors.New("validator staging discovery reference is duplicated")
		}
		seen[reference.Path] = true
	}
	return nil
}

// A distinct actually eligible native hotkey is a distinct bounded owner.
// Mutable UID/JWT and activation/VPK rotation of this hotkey are not owners.
type ValidatorUploadOwner struct {
	Hotkey       [32]byte
	OperatorNoID uint64
}

// Slot channels survive a same-hotkey activation rotation. Historical keys
// may not create a second active pool while old request owners are joining.
type validatorUploadOwnerSlots struct {
	fresh chan struct{}
	retry chan struct{}
}

// An immutable record has a cancellable admission generation. A refresh error
// or a newer valid activation cancels every lease for a displaced generation.
// The entry expires with the freshness of its own last complete verification,
// so a retained entry never outlives the observation that admitted it.
type validatorUploadAdmissionEntry struct {
	record     protocol.ValidatorEvidenceActivation
	published  uint64
	validUntil time.Time
	ctx        context.Context
	cancel     context.CancelFunc
	slots      *validatorUploadOwnerSlots
}

// One completed or failed refresh, reported once by the owned loop. Counts
// and causes are copies; the report retains no admission state.
type validatorUploadRefreshReport struct {
	started            time.Time
	finished           time.Time
	evmHeadAge         time.Duration
	nativeHeadAge      time.Duration
	ranges             uint64
	events             uint64
	admitted           int
	retained           int
	validUntil         time.Time
	activationFailures map[[32]byte]error
	err                error
}

// Safe for concurrent requests. The constructor owns its refresh goroutine;
// Close cancels refresh and all leases and joins both before returning. Chain
// clients are borrowed, and their external owner closes them only after Close.
type ValidatorUploadAdmission struct {
	ctx              context.Context
	cancel           context.CancelFunc
	done             chan struct{}
	ready            chan struct{}
	chain            *ChainClient
	native           *crv4.Chain
	config           ValidatorUploadAdmissionConfig
	discoveryDigests [][32]byte
	// The publication clock and report sink are fixed before the loop starts.
	clock         func() time.Time
	refreshReport func(validatorUploadRefreshReport)
	// Only the owned refresh loop accesses verified immutable histories and the
	// last refresh duration. Request-side diagnostics use activationFailures
	// under stateLock.
	verifiedHistory     map[[32]byte]VerifiedReleaseActivationV2
	lastRefreshDuration time.Duration
	activationFailures  map[[32]byte]error
	stateLock           sync.Mutex
	entries             map[[32]byte]*validatorUploadAdmissionEntry
	ownerSlots          map[ValidatorUploadOwner]*validatorUploadOwnerSlots
	validUntil          time.Time
	lastError           error
	closed              bool
	leases              sync.WaitGroup
}

// File custody and canonical context syntax precede the first RPC. The API
// can start while activation publication is still pending; its reserved route
// refuses until the first complete real refresh, while ordinary routes remain.
func NewValidatorUploadAdmission(ctx context.Context, chain *ChainClient, native *crv4.Chain, config ValidatorUploadAdmissionConfig) (*ValidatorUploadAdmission, error) {
	self, err := newValidatorUploadAdmissionState(ctx, chain, native, config)
	if err != nil {
		return nil, err
	}
	go self.run()
	return self, nil
}

// Separates finite construction from the one owned loop; package tests use
// the same real refresh directly to force exact freshness transitions.
func newValidatorUploadAdmissionState(ctx context.Context, chain *ChainClient, native *crv4.Chain, config ValidatorUploadAdmissionConfig) (*ValidatorUploadAdmission, error) {
	if ctx == nil || chain == nil || chain.client == nil || native == nil || native.API == nil || native.API.Client == nil {
		return nil, errors.New("validator staging refresh owners are unavailable")
	}
	if err := errors.Join(ctx.Err(), config.Validate()); err != nil {
		return nil, err
	}
	config.ActivationContexts = slices.Clone(config.ActivationContexts)
	deployment, err := loadValidatorUploadRuntimeContext(ctx, config.Deployment, config.ProductionRuntimeConfig)
	if err != nil {
		return nil, err
	}
	config.Deployment = deployment
	digests := make([][32]byte, 0, len(config.ActivationContexts))
	seen := make(map[[32]byte]bool, len(config.ActivationContexts))
	var retainedContexts []ReleaseEvidenceV2ActivationContext
	for _, reference := range config.ActivationContexts {
		encoded, err := ReadReleaseEvidenceV2File(ctx, reference, config.MaximumContextBytes)
		if err != nil {
			return nil, err
		}
		parsed, err := decodeReleaseEvidenceV2ActivationContext(encoded, config.MaximumContextBytes)
		if err != nil {
			return nil, err
		}
		if parsed.Journal != config.Deployment.Journal || parsed.RuntimeHash != config.Deployment.RuntimeHash {
			return nil, errors.New("validator staging discovery context differs from independently approved journal")
		}
		digest, err := parsed.Activation.Digest()
		if err != nil {
			return nil, err
		}
		if seen[digest] {
			return nil, errors.New("validator staging discovery digest is duplicated")
		}
		seen[digest] = true
		digests = append(digests, digest)
		if config.ProvisionalRetainedContextAuthority {
			retainedContexts = append(retainedContexts, parsed)
		}
	}
	// The signed mainnet producer config's opt-in also lets staging follow its
	// validator across a compatible upgrade. Staging decodes only producer-
	// profile interfaces, which crv4 checks for every successor. A different
	// route genesis keeps the exact refusal that the refresh reports.
	if owner := config.Deployment.productionRuntime; owner != nil && len(owner.successors) != 0 && native.GenesisHash == types.Hash(config.Deployment.GenesisHash) {
		if err := native.EnableRuntimeSuccession(types.Hash(config.Deployment.GenesisHash), crv4.ValidatorProducerRuntimeProfile, owner.successors, func(successor crv4.RuntimeSuccessor) error {
			fmt.Fprintf(os.Stderr, "validator staging: admitted runtime %s/%d code=%s metadata=%s at %s as a %s successor of approved %d\n",
				successor.Artifact.Version.SpecName, successor.Artifact.Version.SpecVersion, successor.Artifact.CodeHash, successor.Artifact.MetadataHash,
				successor.BlockHash.Hex(), crv4.ValidatorProducerRuntimeProfile, successor.Approved.Version.SpecVersion)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if config.ProvisionalRuntimeCompatibility != "" {
		if err := native.EnableProvisionalRuntimeCompatibility(types.Hash(config.Deployment.GenesisHash), func(artifact crv4.AuthenticatedRuntimeArtifact) error {
			return crv4.WriteProvisionalRuntimeObservation(config.RuntimeObservationDir, artifact)
		}); err != nil {
			return nil, err
		}
	}
	ownerCtx, cancel := context.WithCancel(ctx)
	owner := &ValidatorUploadAdmission{ctx: ownerCtx, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}), chain: chain, native: native, config: config,
		discoveryDigests: digests, clock: time.Now, refreshReport: printValidatorUploadRefreshReport,
		verifiedHistory: make(map[[32]byte]VerifiedReleaseActivationV2, len(digests)), activationFailures: make(map[[32]byte]error, len(digests)),
		entries: make(map[[32]byte]*validatorUploadAdmissionEntry), ownerSlots: make(map[ValidatorUploadOwner]*validatorUploadOwnerSlots), lastError: errors.New("validator staging admission has not refreshed")}
	if config.ProvisionalRetainedContextAuthority {
		if err := owner.installProvisionalRetainedContextAuthority(retainedContexts); err != nil {
			cancel()
			return nil, err
		}
	}
	return owner, nil
}

// Never overlap refreshes or retry inside an observation. Each next attempt
// starts after the interval, or earlier so that it can publish before the
// current state expires, and has its own finite cancellation owner.
func (self *ValidatorUploadAdmission) run() {
	defer close(self.done)
	defer func() {
		if value := recover(); value != nil {
			self.invalidate(errors.New("validator staging refresh panicked"))
		} else {
			self.invalidate(errors.New("validator staging refresh stopped"))
		}
	}()
	if self.config.ProvisionalRetainedContextAuthority {
		self.logProvisionalRetainedContextAuthority("startup")
		close(self.ready)
		<-self.ctx.Done()
		return
	}
	first := true
	for {
		operationCtx, cancel := context.WithTimeout(self.ctx, time.Duration(self.config.MaximumRefreshSeconds)*time.Second)
		started := self.clock()
		err := self.refresh(operationCtx, started)
		cancel()
		self.lastRefreshDuration = self.clock().Sub(started)
		if err != nil {
			self.invalidate(err)
		}
		if first {
			close(self.ready)
			first = false
		}
		select {
		case <-self.ctx.Done():
			return
		case <-time.After(self.nextRefreshDelay()):
		}
	}
}

// Ordinary readiness reports the actual first refresh. Explicit retained
// staging readiness reports only its local allowance, with chain checks unrun.
func (self *ValidatorUploadAdmission) WaitReady(ctx context.Context) error {
	if self == nil || ctx == nil {
		return errors.New("validator staging readiness owner is unavailable")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-self.done:
		return errors.New("validator staging admission stopped")
	case <-self.ready:
	}
	if self.config.ProvisionalRetainedContextAuthority {
		self.logProvisionalRetainedContextAuthority("local-readiness")
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return errors.Join(self.lastError, self.seededReadinessErrorLocked(), self.ctx.Err(), ctx.Err())
}

// A successful subset remains usable, but never reports complete seeded
// readiness. Incomplete seeded refreshes retry promptly without redoing the
// immutable history of already authenticated candidates.
func (self *ValidatorUploadAdmission) seededReadinessErrorLocked() error {
	if !self.config.ProvisionalSeededDiscoveryOnly {
		return nil
	}
	for _, digest := range self.discoveryDigests {
		if err := self.activationFailures[digest]; err != nil {
			return fmt.Errorf("provisional seeded admission incomplete for activation %x: %w", digest[:8], err)
		}
		if entry := self.entries[digest]; entry == nil || entry.ctx.Err() != nil {
			return fmt.Errorf("provisional seeded admission incomplete for activation %x", digest[:8])
		}
	}
	return nil
}

// The fixed interval is shortened when the published state would otherwise
// expire before the next refresh, measured by the last refresh's duration,
// can publish. One second bounds the loop's rate when no window remains.
func (self *ValidatorUploadAdmission) nextRefreshDelay() time.Duration {
	delay := time.Duration(self.config.RefreshSeconds) * time.Second
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.config.ProvisionalSeededDiscoveryOnly && (self.lastError != nil || self.seededReadinessErrorLocked() != nil) {
		return min(delay, 30*time.Second)
	}
	if self.lastError == nil && !self.validUntil.IsZero() {
		lead := self.lastRefreshDuration + max(time.Second, self.lastRefreshDuration/4)
		delay = max(time.Second, min(delay, self.validUntil.Sub(self.clock())-lead))
	}
	return delay
}

// Causes are request diagnostics, never authority. A success clears its own.
func (self *ValidatorUploadAdmission) recordActivationFailure(digest [32]byte, err error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if err == nil {
		delete(self.activationFailures, digest)
	} else {
		self.activationFailures[digest] = err
	}
}

// A verified anchor without its original canonical hash is a contradiction,
// never an unavailable read that may keep an earlier admission.
var errValidatorUploadHistoryChanged = errors.New("verified historical canonical block changed")

// Cache only successful exact configured activation proofs for this owner
// lifecycle. A finalized anchor's original runtime, record and historical
// eligibility cannot change, so later refreshes recheck only that both chains
// still cover the original canonical anchors; current native eligibility is
// separately read on every refresh below. A cache hit needs no archive state.
func (self *ValidatorUploadAdmission) authenticateRefreshCandidate(ctx context.Context, digest [32]byte, observer ValidatorUploadObserver, nativeObserver ValidatorUploadNativeObserver) (VerifiedReleaseActivationV2, error) {
	if cached, ok := self.verifiedHistory[digest]; ok {
		var err error
		changed := false
		if observer.Number < cached.ObservedEVMBlock || nativeObserver.Number < cached.Publication.Record.NativeBlock {
			err = errors.New("verified history is ahead of the current finalized observers")
		} else {
			var canonical [32]byte
			canonical, err = self.chain.BlockHashContext(ctx, cached.ObservedEVMBlock)
			if err == nil && canonical != cached.ObservedEVMHash {
				changed = true
				err = fmt.Errorf("%w: EVM block %d", errValidatorUploadHistoryChanged, cached.ObservedEVMBlock)
			}
		}
		if err == nil {
			var canonical string
			err = self.native.API.Client.CallContext(ctx, &canonical, "chain_getBlockHash", cached.Publication.Record.NativeBlock)
			if err == nil && canonical != types.Hash(cached.Publication.Record.NativeHash).Hex() {
				changed = true
				err = fmt.Errorf("%w: native block %d", errValidatorUploadHistoryChanged, cached.Publication.Record.NativeBlock)
			}
		}
		if err = errors.Join(err, ctx.Err()); err != nil {
			// Unavailability denies this refresh without erasing successful
			// immutable work. Only an observed different hash invalidates it.
			if changed {
				delete(self.verifiedHistory, digest)
			}
			return VerifiedReleaseActivationV2{}, err
		}
		return cached, nil
	}
	verified, err := self.chain.AuthenticateValidatorUploadActivationContext(ctx, self.native, self.config.Deployment, digest, observer)
	if err = errors.Join(err, ctx.Err()); err != nil {
		return VerifiedReleaseActivationV2{}, err
	}
	self.verifiedHistory[digest] = verified
	return verified, nil
}

// Retain no accepted entry after a failed observer or incomplete scan. Cancel
// outside the state lock, so request cleanup never runs under shared state.
func (self *ValidatorUploadAdmission) invalidate(err error) {
	var cancel []context.CancelFunc
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		self.lastError, self.validUntil = err, time.Time{}
		for _, entry := range self.entries {
			cancel = append(cancel, entry.cancel)
		}
		self.entries = make(map[[32]byte]*validatorUploadAdmissionEntry)
	}()
	for _, stop := range cancel {
		stop()
	}
}

// Refresh scans the finite complete anchor history, retaining at most one
// latest authenticated activation per stable owner. Failed candidate reads
// grant nothing new, record their cause for the refused request and are
// revisited next refresh; an ineligible account's event does not revoke an
// independently verified legitimate owner's admission. A previously admitted
// activation is checked again even when discovery omits it. If its reads fail,
// rather than observe ineligibility, deregistration, a later publication of
// its owner or a changed anchor, it keeps only the expiry of its last complete
// verification. A refresh that finishes after its own freshness window
// publishes nothing and fails with the measured cause.
func (self *ValidatorUploadAdmission) refresh(ctx context.Context, now time.Time) (resultErr error) {
	report := validatorUploadRefreshReport{started: now}
	defer func() {
		report.finished, report.err = self.clock(), resultErr
		if self.refreshReport != nil {
			self.refreshReport(report)
		}
	}()
	if err := errors.Join(ctx.Err(), self.ctx.Err()); err != nil {
		return err
	}
	observer, err := self.chain.ValidatorUploadObserverContext(ctx)
	if err != nil {
		return err
	}
	nativeObserver, err := ValidatorUploadNativeObserverContext(ctx, self.native, self.config.Deployment)
	if err != nil {
		return err
	}
	report.evmHeadAge = now.Sub(time.Unix(int64(observer.Timestamp), 0))
	report.nativeHeadAge = now.Sub(time.UnixMilli(int64(nativeObserver.TimestampMillis)))
	currentRuntime, err := self.config.Deployment.runtimeArtifactsAt(nativeObserver.Number, false)
	if err != nil {
		return err
	}
	if now.Unix() <= 0 || observer.Timestamp > uint64(now.Unix()) || uint64(now.Unix())-observer.Timestamp >= self.config.MaximumHeadAgeSeconds ||
		nativeObserver.TimestampMillis > uint64(now.UnixMilli()) || uint64(now.UnixMilli())-nativeObserver.TimestampMillis >= self.config.MaximumHeadAgeSeconds*1000 {
		return errors.New("validator staging chain observer is stale or from the future")
	}
	start := self.config.Deployment.DeploymentBlock
	if observer.Number < start || !self.config.ProvisionalSeededDiscoveryOnly && observer.Number-start >= self.config.BlocksPerRange*self.config.MaximumRanges {
		return errors.New("validator staging complete discovery exceeds its explicit range budget")
	}
	next := make(map[ValidatorUploadOwner]VerifiedReleaseActivationV2)
	seen := make(map[[32]byte]bool)
	// Digests whose reads failed without observing ineligibility or a changed
	// anchor. Only an entry already admitted by an earlier refresh survives.
	unobserved := make(map[[32]byte]bool)
	// Each owner's latest publication from discovery or an authenticated
	// record, admitted or not. An older entry of that owner is superseded and
	// is never retained through its own failed reads.
	latestPublishedOwnerBlocks := make(map[ValidatorUploadOwner]uint64)
	// Canonical event order also distinguishes rotations within one block.
	latestPublishedOwnerEvents := make(map[ValidatorUploadOwner]ValidatorUploadActivationEvent)
	superseded := func(owner ValidatorUploadOwner, digest [32]byte, published uint64) bool {
		if latestPublishedOwnerBlocks[owner] > published {
			return true
		}
		event, found := latestPublishedOwnerEvents[owner]
		return found && event.Block == published && event.Digest != digest
	}
	// Current eligibility is read once per stable hotkey in this captured
	// refresh. Its metadata and resolved UID come from the actual current hash.
	current := make(map[[32]byte]bool)
	currentErrors := make(map[[32]byte]error)
	admit := func(digest [32]byte, event *ValidatorUploadActivationEvent) error {
		if seen[digest] {
			return nil
		}
		seen[digest] = true
		verified, err := self.authenticateRefreshCandidate(ctx, digest, observer, nativeObserver)
		if err != nil {
			self.recordActivationFailure(digest, fmt.Errorf("historical activation authentication: %w", err))
			if ctx.Err() != nil {
				return ctx.Err()
			}
			unobserved[digest] = !errors.Is(err, errValidatorUploadHistoryChanged)
			return nil // No new candidate authority survives an RPC/eligibility error.
		}
		record := verified.Publication.Record
		if event != nil && (event.Hotkey != record.Hotkey || event.NoID != record.NoID || event.Epoch != record.Domain.Epoch || event.Block != verified.Publication.PublishedBlock) {
			return errors.New("validator staging event conflicts with the canonical anchored record")
		}
		owner := ValidatorUploadOwner{Hotkey: record.Hotkey, OperatorNoID: record.NoID}
		latestPublishedOwnerBlocks[owner] = max(latestPublishedOwnerBlocks[owner], verified.Publication.PublishedBlock)
		eligible, checked := current[record.Hotkey]
		if !checked {
			observation, err := crv4.ReadValidatorScheduleAtContext(ctx, self.native, crv4.ValidatorScheduleQuery{GenesisHash: types.Hash(self.config.Deployment.GenesisHash), BlockHash: nativeObserver.Hash,
				BlockNumber: nativeObserver.Number, Netuid: self.config.Deployment.Netuid, Hotkey: record.Hotkey, MaximumSubnetUIDs: self.config.Deployment.MaximumSubnetUIDs}, currentRuntime...)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			eligible = err == nil && observation.Stake.MeetsNonSelfStakeAndPermit()
			currentErrors[record.Hotkey] = err
			if uint64(len(current)) >= self.config.MaximumOwners {
				return errors.New("validator staging current native owner census exceeds its bound")
			}
			current[record.Hotkey] = eligible
		}
		if !eligible {
			switch err := currentErrors[record.Hotkey]; {
			case err == nil:
				self.recordActivationFailure(digest, errors.New("current native hotkey lacks authenticated stake and permit"))
			case errors.Is(err, crv4.ErrValidatorScheduleHotkeyUnregistered):
				// A deregistered hotkey is observed ineligibility, not a failed read.
				self.recordActivationFailure(digest, fmt.Errorf("current native eligibility: %w", err))
			default:
				self.recordActivationFailure(digest, fmt.Errorf("current native eligibility read: %w", err))
				unobserved[digest] = true
			}
			return nil
		}
		self.recordActivationFailure(digest, nil)
		prior, exists := next[owner]
		if !exists && uint64(len(next)) >= self.config.MaximumOwners {
			return errors.New("validator staging owner census exceeds its bound")
		}
		if exists && prior.Publication.PublishedBlock > verified.Publication.PublishedBlock {
			return nil
		}
		if exists && prior.Publication.PublishedBlock == verified.Publication.PublishedBlock && prior.Publication.Record != record && event == nil {
			return errors.New("validator staging initial references compete at one publication height")
		}
		next[owner] = verified
		return nil
	}
	discoveryRanges := self.config.MaximumRanges
	if self.config.ProvisionalSeededDiscoveryOnly {
		discoveryRanges = 0
	}
	for range discoveryRanges {
		end := observer.Number
		if observer.Number-start >= self.config.BlocksPerRange {
			end = start + self.config.BlocksPerRange - 1
		}
		events, err := self.chain.ValidatorUploadActivationEventsContext(ctx, self.config.Deployment, start, end, self.config.BlocksPerRange, self.config.MaximumEventsPerRange, observer)
		if err != nil {
			return err
		}
		report.ranges++
		report.events += uint64(len(events))
		for index := range events {
			owner := ValidatorUploadOwner{Hotkey: events[index].Hotkey, OperatorNoID: events[index].NoID}
			latestPublishedOwnerBlocks[owner] = max(latestPublishedOwnerBlocks[owner], events[index].Block)
			latestPublishedOwnerEvents[owner] = events[index]
			if err := admit(events[index].Digest, &events[index]); err != nil {
				return err
			}
		}
		if end == observer.Number {
			break
		}
		start = end + 1
	}
	// Event order resolves multiple real rotations in one block. Reference
	// seeds are considered afterward and cannot replace a later publication.
	for _, digest := range self.discoveryDigests {
		if err := admit(digest, nil); err != nil {
			return err
		}
	}
	// A provider that serves recent logs but returns no rows for old ranges
	// cannot silently drop an activation this owner already authenticated.
	var previousDigests [][32]byte
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		for digest := range self.entries {
			previousDigests = append(previousDigests, digest)
		}
	}()
	slices.SortFunc(previousDigests, func(a [32]byte, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	for _, digest := range previousDigests {
		if err := admit(digest, nil); err != nil {
			return err
		}
	}
	// A failed successor proof cannot resurrect the older generation even
	// when that older proof and its current eligibility still read correctly.
	for owner, verified := range next {
		digest, err := verified.Publication.Record.Digest()
		if err != nil {
			return err
		}
		if superseded(owner, digest, verified.Publication.PublishedBlock) {
			delete(next, owner)
			self.recordActivationFailure(digest, errors.New("validator staging activation was superseded by a later publication"))
		}
	}
	if err := errors.Join(ctx.Err(), self.ctx.Err()); err != nil {
		return err
	}
	// Each current schedule rechecks its native canonical hash. Recheck the
	// shared EVM observer after the final candidate, before any cache publish.
	if err := self.chain.recheckValidatorUploadBlockContext(ctx, observer.Number, observer.Hash); err != nil {
		return err
	}
	validUntil := time.Unix(int64(observer.Timestamp+self.config.MaximumHeadAgeSeconds), 0)
	nativeValidUntil := time.UnixMilli(int64(nativeObserver.TimestampMillis + self.config.MaximumHeadAgeSeconds*1000))
	if nativeValidUntil.Before(validUntil) {
		validUntil = nativeValidUntil
	}
	report.validUntil = validUntil
	// The observers were captured at the start, so a slow refresh can finish
	// after its own window. Publishing that state would refuse every request
	// with no recorded cause while it looked like a successful refresh.
	if published := self.clock(); !published.Before(validUntil) {
		return fmt.Errorf("validator staging refresh finished after its freshness window: EVM head age %s and native head age %s at start, refresh took %s, maximum head age %ds",
			report.evmHeadAge.Round(time.Millisecond), report.nativeHeadAge.Round(time.Millisecond), published.Sub(now).Round(time.Millisecond), self.config.MaximumHeadAgeSeconds)
	}
	var retired []context.CancelFunc
	err = func() error {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.closed || self.ctx.Err() != nil {
			return errors.New("validator staging cache is closed")
		}
		// An earlier entry whose reads failed keeps its own expiry unless the
		// same owner's equal or later activation was admitted now, or a later
		// one was discovered or authenticated. It replaces an older activation
		// of its owner that was admitted in its place.
		published := self.clock()
		retainedOwnerDigests := make(map[ValidatorUploadOwner][32]byte)
		for digest, retain := range unobserved {
			entry := self.entries[digest]
			if !retain || entry == nil || !published.Before(entry.validUntil) {
				continue
			}
			owner := ValidatorUploadOwner{Hotkey: entry.record.Hotkey, OperatorNoID: entry.record.NoID}
			if superseded(owner, digest, entry.published) {
				continue
			}
			if admitted, exists := next[owner]; exists && admitted.Publication.PublishedBlock >= entry.published {
				continue
			}
			retainedOwnerDigests[owner] = digest
		}
		// Owner slots never multiply under rotation or an overlapping request.
		missing := uint64(0)
		for owner := range next {
			if self.ownerSlots[owner] == nil {
				missing++
			}
		}
		if uint64(len(self.ownerSlots))+missing > self.config.MaximumOwners {
			return errors.New("validator staging retained owner-slot census exceeds its bound")
		}
		entries := make(map[[32]byte]*validatorUploadAdmissionEntry, len(next)+len(retainedOwnerDigests))
		for owner, value := range next {
			if _, retained := retainedOwnerDigests[owner]; retained {
				continue
			}
			digest, err := value.Publication.Record.Digest()
			if err != nil {
				return err
			}
			entry := self.entries[digest]
			if entry == nil {
				slots := self.ownerSlots[owner]
				if slots == nil {
					slots = &validatorUploadOwnerSlots{fresh: make(chan struct{}, int(self.config.FreshActivePerOwner)), retry: make(chan struct{}, int(self.config.RetryActivePerOwner))}
					self.ownerSlots[owner] = slots
				}
				entryCtx, cancel := context.WithCancel(self.ctx)
				entry = &validatorUploadAdmissionEntry{record: value.Publication.Record, published: value.Publication.PublishedBlock, ctx: entryCtx, cancel: cancel, slots: slots}
			}
			entry.validUntil = validUntil
			entries[digest] = entry
		}
		for _, digest := range retainedOwnerDigests {
			entries[digest] = self.entries[digest]
		}
		for digest, entry := range self.entries {
			if entries[digest] == nil {
				retired = append(retired, entry.cancel)
			}
		}
		// Discovered causes stay bounded by this refresh's candidates.
		if !self.config.ProvisionalSeededDiscoveryOnly {
			for digest := range self.activationFailures {
				if !seen[digest] {
					delete(self.activationFailures, digest)
				}
			}
		}
		report.admitted, report.retained = len(entries)-len(retainedOwnerDigests), len(retainedOwnerDigests)
		report.activationFailures = maps.Clone(self.activationFailures)
		self.entries, self.validUntil, self.lastError = entries, validUntil, nil
		return nil
	}()
	for _, cancel := range retired {
		cancel()
	}
	// Verified histories stay bounded by this refresh's candidates.
	if err == nil && !self.config.ProvisionalSeededDiscoveryOnly {
		for digest := range self.verifiedHistory {
			if !seen[digest] {
				delete(self.verifiedHistory, digest)
			}
		}
	}
	return err
}

// One line per refresh: its outcome, duration, head ages and every candidate
// activation that was not admitted, so neither a silently dropped activation
// nor a state published past its window can hide behind a generic refusal.
func printValidatorUploadRefreshReport(report validatorUploadRefreshReport) {
	digests := slices.SortedFunc(maps.Keys(report.activationFailures), func(a [32]byte, b [32]byte) int { return bytes.Compare(a[:], b[:]) })
	causes := make([]string, 0, len(digests))
	for _, digest := range digests {
		causes = append(causes, fmt.Sprintf("%x: %v", digest[:8], report.activationFailures[digest]))
	}
	outcome := "published"
	if report.err != nil {
		outcome = "failed: " + report.err.Error()
	}
	line := fmt.Sprintf("validator staging refresh %s; took %s, EVM head age %s, native head age %s, %d ranges, %d events, %d admitted, %d retained, valid until %s; not admitted [%s]",
		outcome, report.finished.Sub(report.started).Round(time.Millisecond), report.evmHeadAge.Round(time.Millisecond), report.nativeHeadAge.Round(time.Millisecond),
		report.ranges, report.events, report.admitted, report.retained, report.validUntil.UTC().Format(time.RFC3339), strings.Join(causes, "; "))
	fmt.Fprintln(os.Stderr, strings.ReplaceAll(line, "\n", " | "))
}

// A lease owns its joined cancellation hooks and at most one nonblocking
// active slot. The handler must Close after body Close and storage/readback.
type ValidatorUploadLease struct {
	owner     *ValidatorUploadAdmission
	entry     *validatorUploadAdmissionEntry
	intent    protocol.ValidatorAttemptUploadIntent
	ctx       context.Context
	cancel    context.CancelFunc
	stop      func() bool
	stopped   chan struct{}
	stateLock sync.Mutex
	slot      chan struct{}
	started   bool
	closed    bool
	closeOnce sync.Once
}

// Actual JWT authentication is the HTTP caller's responsibility. This method
// binds its exact accepted bearer hash, destination, type, digest and size to
// real VPK consent and the independently refreshed cache before quota work.
func (self *ValidatorUploadAdmission) Begin(ctx context.Context, header string, sessionHash [32]byte, kind byte, contentHash [32]byte, size uint64) (*ValidatorUploadLease, error) {
	return self.beginAt(ctx, header, sessionHash, kind, contentHash, size, time.Now())
}

// The explicit private clock makes expired/refresh transitions deterministic;
// no production caller can supply an older clock to extend eligibility.
func (self *ValidatorUploadAdmission) beginAt(ctx context.Context, header string, sessionHash [32]byte, kind byte, contentHash [32]byte, size uint64, now time.Time) (*ValidatorUploadLease, error) {
	if self == nil || ctx == nil {
		return nil, errors.New("validator staging request owner is unavailable")
	}
	if err := errors.Join(ctx.Err(), self.ctx.Err()); err != nil {
		return nil, err
	}
	intent, err := protocol.VerifyValidatorAttemptUploadHeader(header)
	if err != nil {
		return nil, err
	}
	if now.Unix() <= 0 || intent.SessionHash != sessionHash || intent.ReplicaNoID != self.config.ReplicaNoID || intent.Kind != kind || intent.ContentHash != contentHash || intent.Size != size ||
		intent.NotAfter <= uint64(now.Unix()) || intent.NotAfter-uint64(now.Unix()) > self.config.MaximumIntentSeconds {
		return nil, errors.New("validator staging consent differs from current request, replica or time")
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	entry := self.entries[intent.ActivationHash]
	if self.lastError != nil {
		return nil, fmt.Errorf("validator staging activation is absent, stale or superseded: %w", self.lastError)
	}
	// A retained entry keeps the expiry of its own last complete verification.
	validUntil := self.validUntil
	if entry != nil && entry.validUntil.Before(validUntil) {
		validUntil = entry.validUntil
	}
	expired := !self.config.ProvisionalRetainedContextAuthority && !now.Before(validUntil)
	if entry == nil || expired {
		if err := self.activationFailures[intent.ActivationHash]; err != nil {
			return nil, fmt.Errorf("validator staging activation %x is unavailable: %w", intent.ActivationHash[:8], err)
		}
	}
	switch {
	case self.closed:
		return nil, errors.New("validator staging activation is absent, stale or superseded: admission is closed")
	case entry == nil:
		return nil, fmt.Errorf("validator staging activation is absent, stale or superseded: activation %x is not admitted", intent.ActivationHash[:8])
	case expired:
		return nil, fmt.Errorf("validator staging activation is absent, stale or superseded: admission expired at %s", validUntil.UTC().Format(time.RFC3339))
	case entry.record.VPK != intent.VPK || entry.ctx.Err() != nil:
		return nil, fmt.Errorf("validator staging activation is absent, stale or superseded: activation %x names another key or was superseded", intent.ActivationHash[:8])
	}
	deadline := time.Unix(int64(intent.NotAfter), 0)
	if !self.config.ProvisionalRetainedContextAuthority && validUntil.Before(deadline) {
		deadline = validUntil
	}
	leaseCtx, cancel := context.WithDeadline(ctx, deadline)
	lease := &ValidatorUploadLease{owner: self, entry: entry, intent: intent, ctx: leaseCtx, cancel: cancel, stopped: make(chan struct{})}
	lease.stop = context.AfterFunc(entry.ctx, func() { defer close(lease.stopped); cancel() })
	self.leases.Add(1)
	return lease, nil
}

// These are value copies of authenticated routing, never account authority.
func (self *ValidatorUploadLease) Owner() ValidatorUploadOwner {
	return ValidatorUploadOwner{Hotkey: self.entry.record.Hotkey, OperatorNoID: self.entry.record.NoID}
}
func (self *ValidatorUploadLease) Intent() protocol.ValidatorAttemptUploadIntent { return self.intent }
func (self *ValidatorUploadLease) Context() context.Context                      { return self.ctx }

// Redis, not a request hint, classifies the exact bounded object reservation.
// Duplicate traffic has a separate active pool and cannot occupy fresh slots.
func (self *ValidatorUploadLease) Start(fresh bool) error {
	if self == nil {
		return errors.New("validator staging lease is absent")
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.closed || self.started {
		return errors.New("validator staging lease was already used")
	}
	if err := self.ctx.Err(); err != nil {
		return err
	}
	self.started = true
	slots := self.entry.slots.retry
	if fresh {
		slots = self.entry.slots.fresh
	}
	select {
	case slots <- struct{}{}:
		self.slot = slots
		return nil
	default:
		return errors.New("429 Validator staging owner is busy.")
	}
}

// Close is idempotent and joins the cancellation callback before ownership
// is released to the cache's shutdown waiter. No request goroutine escapes.
func (self *ValidatorUploadLease) Close() {
	if self == nil {
		return
	}
	self.closeOnce.Do(func() {
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.closed = true
			if self.slot != nil {
				<-self.slot
				self.slot = nil
			}
		}()
		self.cancel()
		if !self.stop() {
			<-self.stopped
		}
		self.owner.leases.Done()
	})
}

// A real lease from another deployment/cache cannot spend this owner's quota.
func (self *ValidatorUploadAdmission) ValidateLease(lease *ValidatorUploadLease) error {
	if self == nil || lease == nil || lease.owner != self {
		return errors.New("validator staging lease belongs to another admission owner")
	}
	return errors.Join(self.ctx.Err(), lease.ctx.Err())
}

// Stop new admission before joining the loop and leases. The outer API owner
// drains handlers before Close, and cancellation also interrupts stalled I/O.
func (self *ValidatorUploadAdmission) Close() {
	if self == nil {
		return
	}
	func() { self.stateLock.Lock(); defer self.stateLock.Unlock(); self.closed = true }()
	self.cancel()
	<-self.done
	self.leases.Wait()
}

// The lifetime used by the Redis clock must be the same independently
// configured limit already checked by the VPK consent/cache owner.
func (self *ValidatorUploadAdmission) MaximumIntentSeconds() uint64 {
	return self.config.MaximumIntentSeconds
}

// Stable deployment identifiers make accidental replica/config substitution
// visible to the real HTTP/controller owner before any quota is spent.
func (self *ValidatorUploadAdmission) Deployment() ValidatorUploadDeployment {
	return self.config.Deployment
}
func (self *ValidatorUploadAdmission) ReplicaNoID() uint64 { return self.config.ReplicaNoID }

// Rendering may pin configuration without enabling an unverified entry.
func (self ValidatorUploadAdmissionConfig) String() string {
	return fmt.Sprintf("reserved staging replica %d, at most %d native owners", self.ReplicaNoID, self.MaximumOwners)
}
