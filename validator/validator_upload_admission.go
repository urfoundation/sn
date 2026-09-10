//go:build linux || darwin

// A bounded, refresh-owned staging cache contains only actual anchored and
// historically/currently eligible hotkeys. Upload requests perform no RPC.
// Ordinary accounts and duplicate reads cannot take a fresh owner's slot.
package validator

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
)

// Explicit deployment-provisioned references seed discovery, never authority.
// The bounded complete event scan also discovers later external validators.
// Operators must provision enough finite scan capacity for their journal;
// exceeding it refuses refresh rather than silently installing an allowlist.
type ValidatorUploadAdmissionConfig struct {
	Deployment            ValidatorUploadDeployment `json:"deployment" yaml:"deployment"`
	ReplicaNoID           uint64                    `json:"replica_no_id" yaml:"replica_no_id"`
	ActivationContexts    []ReleaseEvidenceV2File   `json:"activation_contexts" yaml:"activation_contexts"`
	MaximumContextBytes   uint64                    `json:"maximum_context_bytes" yaml:"maximum_context_bytes"`
	MaximumOwners         uint64                    `json:"maximum_owners" yaml:"maximum_owners"`
	BlocksPerRange        uint64                    `json:"blocks_per_range" yaml:"blocks_per_range"`
	MaximumRanges         uint64                    `json:"maximum_ranges" yaml:"maximum_ranges"`
	MaximumEventsPerRange uint64                    `json:"maximum_events_per_range" yaml:"maximum_events_per_range"`
	RefreshSeconds        uint64                    `json:"refresh_seconds" yaml:"refresh_seconds"`
	MaximumRefreshSeconds uint64                    `json:"maximum_refresh_seconds" yaml:"maximum_refresh_seconds"`
	MaximumHeadAgeSeconds uint64                    `json:"maximum_head_age_seconds" yaml:"maximum_head_age_seconds"`
	MaximumIntentSeconds  uint64                    `json:"maximum_intent_seconds" yaml:"maximum_intent_seconds"`
	FreshActivePerOwner   uint64                    `json:"fresh_active_per_owner" yaml:"fresh_active_per_owner"`
	RetryActivePerOwner   uint64                    `json:"retry_active_per_owner" yaml:"retry_active_per_owner"`
}

// Capacity products are checked before allocation or file/RPC work. These
// transport ceilings are not mainnet population or per-hour budget defaults.
func (self ValidatorUploadAdmissionConfig) Validate() error {
	if err := self.Deployment.Validate(); err != nil {
		return err
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
type validatorUploadAdmissionEntry struct {
	record    protocol.ValidatorEvidenceActivation
	published uint64
	ctx       context.Context
	cancel    context.CancelFunc
	slots     *validatorUploadOwnerSlots
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
	stateLock        sync.Mutex
	entries          map[[32]byte]*validatorUploadAdmissionEntry
	ownerSlots       map[ValidatorUploadOwner]*validatorUploadOwnerSlots
	validUntil       time.Time
	lastError        error
	closed           bool
	leases           sync.WaitGroup
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
	digests := make([][32]byte, 0, len(config.ActivationContexts))
	seen := make(map[[32]byte]bool, len(config.ActivationContexts))
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
	}
	ownerCtx, cancel := context.WithCancel(ctx)
	return &ValidatorUploadAdmission{ctx: ownerCtx, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}), chain: chain, native: native, config: config,
		discoveryDigests: digests, entries: make(map[[32]byte]*validatorUploadAdmissionEntry), ownerSlots: make(map[ValidatorUploadOwner]*validatorUploadOwnerSlots), lastError: errors.New("validator staging admission has not refreshed")}, nil
}

// Never overlap refreshes or retry inside an observation. Each next attempt
// starts after the fixed interval and has its own finite cancellation owner.
func (self *ValidatorUploadAdmission) run() {
	defer close(self.done)
	defer func() {
		if value := recover(); value != nil {
			self.invalidate(errors.New("validator staging refresh panicked"))
		} else {
			self.invalidate(errors.New("validator staging refresh stopped"))
		}
	}()
	first := true
	for {
		operationCtx, cancel := context.WithTimeout(self.ctx, time.Duration(self.config.MaximumRefreshSeconds)*time.Second)
		err := self.refresh(operationCtx, time.Now())
		cancel()
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
		case <-time.After(time.Duration(self.config.RefreshSeconds) * time.Second):
		}
	}
}

// Readiness reports the actual first refresh, never the act of starting it.
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
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return errors.Join(self.lastError, self.ctx.Err(), ctx.Err())
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
// grant nothing and are revisited next refresh; an ineligible account's event
// does not revoke an independently verified legitimate owner's admission.
func (self *ValidatorUploadAdmission) refresh(ctx context.Context, now time.Time) error {
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
	if now.Unix() <= 0 || observer.Timestamp > uint64(now.Unix()) || uint64(now.Unix())-observer.Timestamp >= self.config.MaximumHeadAgeSeconds ||
		nativeObserver.TimestampMillis > uint64(now.UnixMilli()) || uint64(now.UnixMilli())-nativeObserver.TimestampMillis >= self.config.MaximumHeadAgeSeconds*1000 {
		return errors.New("validator staging chain observer is stale or from the future")
	}
	start := self.config.Deployment.DeploymentBlock
	if observer.Number < start || observer.Number-start >= self.config.BlocksPerRange*self.config.MaximumRanges {
		return errors.New("validator staging complete discovery exceeds its explicit range budget")
	}
	next := make(map[ValidatorUploadOwner]VerifiedReleaseActivationV2)
	seen := make(map[[32]byte]bool)
	// Current eligibility is read once per stable hotkey in this captured
	// refresh. Its metadata and resolved UID come from the actual current hash.
	current := make(map[[32]byte]bool)
	admit := func(digest [32]byte, event *ValidatorUploadActivationEvent) error {
		if seen[digest] {
			return nil
		}
		seen[digest] = true
		verified, err := self.chain.AuthenticateValidatorUploadActivationContext(ctx, self.native, self.config.Deployment, digest, observer)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil // No candidate authority survives an RPC/eligibility error.
		}
		record := verified.Publication.Record
		if event != nil && (event.Hotkey != record.Hotkey || event.NoID != record.NoID || event.Epoch != record.Domain.Epoch || event.Block != verified.Publication.PublishedBlock) {
			return errors.New("validator staging event conflicts with the canonical anchored record")
		}
		eligible, checked := current[record.Hotkey]
		if !checked {
			observation, err := crv4.ReadValidatorScheduleAtContext(ctx, self.native, crv4.ValidatorScheduleQuery{GenesisHash: types.Hash(self.config.Deployment.GenesisHash), BlockHash: nativeObserver.Hash,
				BlockNumber: nativeObserver.Number, Netuid: self.config.Deployment.Netuid, Hotkey: record.Hotkey, MaximumSubnetUIDs: self.config.Deployment.MaximumSubnetUIDs}, self.config.Deployment.NativeRuntime)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			eligible = err == nil && observation.Stake.MeetsNonSelfStakeAndPermit()
			if uint64(len(current)) >= self.config.MaximumOwners {
				return errors.New("validator staging current native owner census exceeds its bound")
			}
			current[record.Hotkey] = eligible
		}
		if !eligible {
			return nil
		}
		owner := ValidatorUploadOwner{Hotkey: record.Hotkey, OperatorNoID: record.NoID}
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
	for range self.config.MaximumRanges {
		end := observer.Number
		if observer.Number-start >= self.config.BlocksPerRange {
			end = start + self.config.BlocksPerRange - 1
		}
		events, err := self.chain.ValidatorUploadActivationEventsContext(ctx, self.config.Deployment, start, end, self.config.BlocksPerRange, self.config.MaximumEventsPerRange, observer)
		if err != nil {
			return err
		}
		for index := range events {
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
	var retired []context.CancelFunc
	err = func() error {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.closed || self.ctx.Err() != nil {
			return errors.New("validator staging cache is closed")
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
		entries := make(map[[32]byte]*validatorUploadAdmissionEntry, len(next))
		for owner, value := range next {
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
			entries[digest] = entry
		}
		for digest, entry := range self.entries {
			if entries[digest] == nil {
				retired = append(retired, entry.cancel)
			}
		}
		self.entries, self.validUntil, self.lastError = entries, validUntil, nil
		return nil
	}()
	for _, cancel := range retired {
		cancel()
	}
	return err
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
	if self.closed || self.lastError != nil || !now.Before(self.validUntil) || entry == nil || entry.record.VPK != intent.VPK || entry.ctx.Err() != nil {
		return nil, errors.New("validator staging activation is absent, stale or superseded")
	}
	deadline := time.Unix(int64(intent.NotAfter), 0)
	if self.validUntil.Before(deadline) {
		deadline = self.validUntil
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
