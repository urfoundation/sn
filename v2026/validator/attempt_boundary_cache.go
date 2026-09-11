package validator

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/stabi"
)

const (
	attemptBoundaryCacheBlocks  = 8
	attemptBoundaryRefreshDelay = 2 * time.Minute
)

type attemptBoundaryRPC interface {
	Snapshot(context.Context) (AttemptBoundary, error)
	Validate(context.Context, AttemptBoundary) error
	Hotkeys(context.Context, AttemptBoundary) (map[[32]byte]uint16, error)
	Binding(context.Context, AttemptBoundary, connect.Id) (stabi.BindingAtOutput, error)
}

type attemptBoundaryBlock struct {
	boundary AttemptBoundary
	hotkeys  map[[32]byte]uint16
	bindings map[connect.Id]AttemptBinding
}

type attemptBoundaryLoad struct {
	done chan struct{}
	err  error
}

type cachedAttemptBoundaryResolver struct {
	rpc          attemptBoundaryRPC
	stateLock    sync.Mutex
	owner        context.Context
	cancel       context.CancelFunc
	prepareLimit time.Duration
	preparations sync.WaitGroup
	closed       bool
	blocks       map[uint64]*attemptBoundaryBlock
	blockOrder   []uint64
	blockLoads   map[uint64]*attemptBoundaryLoad
	bindingLoads map[string]*attemptBoundaryLoad
	now          func() time.Time
	refreshDelay time.Duration
	latest       AttemptBoundary
	refreshAt    time.Time
	snapshotLoad *attemptBoundaryLoad
	snapshotAge  uint64
}

func newCachedAttemptBoundaryResolver(rpc attemptBoundaryRPC) *cachedAttemptBoundaryResolver {
	return newCachedAttemptBoundaryResolverWithClock(rpc, attemptBoundaryRefreshDelay, time.Now)
}

func newCachedAttemptBoundaryResolverWithClock(rpc attemptBoundaryRPC, refreshDelay time.Duration, now func() time.Time) *cachedAttemptBoundaryResolver {
	resolver := newCachedAttemptBoundaryResolverWithLifecycle(context.Background(), rpc, releaseNativeEndpointTimeout(nil))
	resolver.refreshDelay, resolver.now = refreshDelay, now
	return resolver
}

// A trail's step deadline bounds its wait, not the shared immutable census.
// The validator owns and joins preparation, including work whose first waiter
// timed out before the source-wide RPC queue reached its final batch.
func newCachedAttemptBoundaryResolverWithLifecycle(ctx context.Context, rpc attemptBoundaryRPC, prepareLimit time.Duration) *cachedAttemptBoundaryResolver {
	owner, cancel := context.WithCancel(ctx)
	return &cachedAttemptBoundaryResolver{
		rpc: rpc, owner: owner, cancel: cancel, prepareLimit: prepareLimit,
		blocks: map[uint64]*attemptBoundaryBlock{}, blockLoads: map[uint64]*attemptBoundaryLoad{},
		bindingLoads: map[string]*attemptBoundaryLoad{}, now: time.Now, refreshDelay: attemptBoundaryRefreshDelay,
	}
}

func (self *cachedAttemptBoundaryResolver) close() {
	self.stateLock.Lock()
	self.closed = true
	self.cancel()
	self.stateLock.Unlock()
	self.preparations.Wait()
}

func waitAttemptBoundaryLoad(ctx context.Context, load *attemptBoundaryLoad) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-load.done:
		if err := ctx.Err(); err != nil {
			return err
		}
		return load.err
	}
}

func (self *cachedAttemptBoundaryResolver) invalidateLatest() {
	self.stateLock.Lock()
	self.latest = AttemptBoundary{}
	self.refreshAt = time.Time{}
	self.snapshotAge++
	self.stateLock.Unlock()
}

func (self *cachedAttemptBoundaryResolver) block(ctx context.Context, boundary AttemptBoundary, trusted bool) (*attemptBoundaryBlock, error) {
	return self.blockWithOwner(ctx, self.owner, boundary, trusted)
}

func (self *cachedAttemptBoundaryResolver) blockWithOwner(ctx, owner context.Context, boundary AttemptBoundary, trusted bool) (*attemptBoundaryBlock, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		self.stateLock.Lock()
		if self.closed || self.owner.Err() != nil {
			self.stateLock.Unlock()
			return nil, context.Canceled
		}
		if block := self.blocks[boundary.EVMBlock]; block != nil {
			self.stateLock.Unlock()
			if block.boundary != boundary {
				return nil, errors.New("attempt boundary cache observed conflicting finalized block identity")
			}
			return block, nil
		}
		if load := self.blockLoads[boundary.EVMBlock]; load != nil {
			self.stateLock.Unlock()
			if err := waitAttemptBoundaryLoad(ctx, load); err != nil {
				return nil, err
			}
			continue
		}
		load := &attemptBoundaryLoad{done: make(chan struct{})}
		self.blockLoads[boundary.EVMBlock] = load
		self.preparations.Add(1)
		self.stateLock.Unlock()
		go self.prepareBlock(owner, boundary, trusted, load)
		if err := waitAttemptBoundaryLoad(ctx, load); err != nil {
			return nil, err
		}
	}
}

func (self *cachedAttemptBoundaryResolver) prepareBlock(owner context.Context, boundary AttemptBoundary, trusted bool, load *attemptBoundaryLoad) {
	defer self.preparations.Done()
	ctx, cancel := context.WithTimeout(owner, self.prepareLimit)
	defer cancel()
	var err error
	if !trusted {
		err = self.rpc.Validate(ctx, boundary)
	}
	var hotkeys map[[32]byte]uint16
	if err == nil {
		hotkeys, err = self.rpc.Hotkeys(ctx, boundary)
	}
	if err == nil {
		err = ctx.Err()
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	delete(self.blockLoads, boundary.EVMBlock)
	load.err = err
	if err == nil {
		self.blocks[boundary.EVMBlock] = &attemptBoundaryBlock{boundary: boundary, hotkeys: hotkeys, bindings: map[connect.Id]AttemptBinding{}}
		self.blockOrder = append(self.blockOrder, boundary.EVMBlock)
		if len(self.blockOrder) > attemptBoundaryCacheBlocks {
			oldest := self.blockOrder[0]
			self.blockOrder = self.blockOrder[1:]
			delete(self.blocks, oldest)
		}
	}
	close(load.done)
}

func attemptBindingLoadKey(block uint64, clientID connect.Id) string {
	return fmt.Sprintf("%020d:%s", block, clientID)
}

func (self *cachedAttemptBoundaryResolver) latestBoundary(ctx context.Context) (AttemptBoundary, error) {
	for {
		if err := ctx.Err(); err != nil {
			return AttemptBoundary{}, err
		}
		self.stateLock.Lock()
		if self.closed || self.owner.Err() != nil {
			self.stateLock.Unlock()
			return AttemptBoundary{}, context.Canceled
		}
		now := self.now()
		if self.latest != (AttemptBoundary{}) && now.Before(self.refreshAt) {
			boundary := self.latest
			self.stateLock.Unlock()
			return boundary, nil
		}
		if self.snapshotLoad != nil {
			load := self.snapshotLoad
			self.stateLock.Unlock()
			if err := waitAttemptBoundaryLoad(ctx, load); err != nil {
				return AttemptBoundary{}, err
			}
			continue
		}
		load := &attemptBoundaryLoad{done: make(chan struct{})}
		self.snapshotLoad = load
		snapshotAge := self.snapshotAge
		self.preparations.Add(1)
		self.stateLock.Unlock()
		go self.prepareLatest(load, snapshotAge)
		if err := waitAttemptBoundaryLoad(ctx, load); err != nil {
			return AttemptBoundary{}, err
		}
	}
}

func (self *cachedAttemptBoundaryResolver) prepareLatest(load *attemptBoundaryLoad, snapshotAge uint64) {
	defer self.preparations.Done()
	ctx, cancel := context.WithTimeout(self.owner, self.prepareLimit)
	defer cancel()
	boundary, err := self.rpc.Snapshot(ctx)
	if err == nil {
		err = validateAttemptBoundary(boundary)
	}
	self.stateLock.Lock()
	invalidated := snapshotAge != self.snapshotAge
	self.stateLock.Unlock()
	if err == nil && !invalidated {
		_, err = self.blockWithOwner(ctx, ctx, boundary, true)
	}
	if err == nil {
		err = ctx.Err()
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.snapshotLoad = nil
	load.err = err
	if err == nil && snapshotAge == self.snapshotAge {
		self.latest = boundary
		self.refreshAt = self.now().Add(self.refreshDelay)
	}
	close(load.done)
}

func (self *cachedAttemptBoundaryResolver) binding(ctx context.Context, block *attemptBoundaryBlock, clientID connect.Id) (AttemptBinding, error) {
	key := attemptBindingLoadKey(block.boundary.EVMBlock, clientID)
	for {
		self.stateLock.Lock()
		if binding, ok := block.bindings[clientID]; ok {
			self.stateLock.Unlock()
			return binding, nil
		}
		if load := self.bindingLoads[key]; load != nil {
			self.stateLock.Unlock()
			if err := waitAttemptBoundaryLoad(ctx, load); err != nil {
				return AttemptBinding{}, err
			}
			continue
		}
		load := &attemptBoundaryLoad{done: make(chan struct{})}
		self.bindingLoads[key] = load
		self.stateLock.Unlock()

		chainBinding, err := self.rpc.Binding(ctx, block.boundary, clientID)
		observation := AttemptBinding{ClientID: clientID, FleetID: zeroAttemptHash(), Hotkey: zeroAttemptHash()}
		if err == nil && chainBinding.Active {
			if chainBinding.Record.Cleaned || chainBinding.Record.Generation == 0 || chainBinding.Record.ValidFromEpoch > block.boundary.SettlementEpoch || chainBinding.Record.ValidToEpoch < block.boundary.SettlementEpoch {
				err = fmt.Errorf("attempt provider %s has an inconsistent active binding", clientID)
			} else {
				observation.Active = true
				observation.FleetID = attemptHex32(chainBinding.Record.FleetId)
				observation.Hotkey = attemptHex32(chainBinding.Record.Hotkey)
				observation.Generation = chainBinding.Record.Generation
				if uid, found := block.hotkeys[chainBinding.Record.Hotkey]; found && uid == chainBinding.Record.Uid {
					observation.UIDFound = true
					observation.UID = uid
				}
			}
		}
		self.stateLock.Lock()
		delete(self.bindingLoads, key)
		load.err = err
		if err == nil {
			block.bindings[clientID] = observation
		}
		close(load.done)
		self.stateLock.Unlock()
		return observation, err
	}
}

func (self *cachedAttemptBoundaryResolver) Resolve(ctx context.Context, pinned *AttemptBoundary, clientIDs []connect.Id) (AttemptBoundary, []AttemptBinding, error) {
	if ctx == nil || self.rpc == nil {
		return AttemptBoundary{}, nil, errors.New("attempt boundary resolver is unavailable")
	}
	var boundary AttemptBoundary
	trusted := false
	if pinned == nil {
		var err error
		boundary, err = self.latestBoundary(ctx)
		if err != nil {
			return AttemptBoundary{}, nil, err
		}
		trusted = true
	} else {
		boundary = *pinned
	}
	if err := validateAttemptBoundary(boundary); err != nil {
		return AttemptBoundary{}, nil, err
	}
	block, err := self.block(ctx, boundary, trusted)
	if err != nil {
		return AttemptBoundary{}, nil, err
	}
	bindings := make([]AttemptBinding, len(clientIDs))
	for index, clientID := range clientIDs {
		binding, err := self.binding(ctx, block, clientID)
		if err != nil {
			return AttemptBoundary{}, nil, fmt.Errorf("attempt provider %s binding: %w", clientID, err)
		}
		bindings[index] = binding
	}
	return boundary, bindings, nil
}

type chainAttemptBoundaryRPC struct {
	chain  *ChainClient
	netuid uint16
}

func (self *chainAttemptBoundaryRPC) Snapshot(ctx context.Context) (AttemptBoundary, error) {
	block, hash, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return AttemptBoundary{}, fmt.Errorf("attempt finalized EVM head: %w", err)
	}
	epoch, err := chainViewAtHashContext(ctx, self.chain, block, hash, self.chain.coordinator.PackCurrentEpoch(), self.chain.coordinator.UnpackCurrentEpoch)
	if err != nil || epoch == nil || !epoch.IsUint64() {
		return AttemptBoundary{}, fmt.Errorf("attempt finalized EVM epoch: %w", err)
	}
	return AttemptBoundary{SettlementEpoch: epoch.Uint64(), EVMBlock: block, EVMBlockHash: attemptHex32(hash)}, nil
}

func (self *chainAttemptBoundaryRPC) Validate(ctx context.Context, boundary AttemptBoundary) error {
	expectedHash, hashErr := canonicalAttemptHex32("attempt EVM boundary hash", boundary.EVMBlockHash, false)
	if hashErr != nil {
		return hashErr
	}
	blockHash, err := self.chain.BlockHashContext(ctx, boundary.EVMBlock)
	if err != nil || blockHash != expectedHash {
		return errors.New("attempt pinned EVM block hash is no longer canonical")
	}
	epoch, err := chainViewAtHashContext(ctx, self.chain, boundary.EVMBlock, expectedHash, self.chain.coordinator.PackCurrentEpoch(), self.chain.coordinator.UnpackCurrentEpoch)
	if err != nil || epoch == nil || !epoch.IsUint64() || epoch.Uint64() != boundary.SettlementEpoch {
		return errors.New("attempt pinned EVM settlement epoch differs")
	}
	return nil
}

func (self *chainAttemptBoundaryRPC) Hotkeys(ctx context.Context, boundary AttemptBoundary) (map[[32]byte]uint16, error) {
	blockHash, err := canonicalAttemptHex32("attempt EVM boundary hash", boundary.EVMBlockHash, false)
	if err != nil {
		return nil, err
	}
	return self.chain.MetagraphHotkeysAtHashContext(ctx, boundary.EVMBlock, blockHash, self.netuid)
}

func (self *chainAttemptBoundaryRPC) Binding(ctx context.Context, boundary AttemptBoundary, clientID connect.Id) (stabi.BindingAtOutput, error) {
	blockHash, err := canonicalAttemptHex32("attempt EVM boundary hash", boundary.EVMBlockHash, false)
	if err != nil {
		return stabi.BindingAtOutput{}, err
	}
	return chainViewAtHashContext(ctx, self.chain, boundary.EVMBlock, blockHash, self.chain.coordinator.PackBindingAt([16]byte(clientID), new(big.Int).SetUint64(boundary.SettlementEpoch)), self.chain.coordinator.UnpackBindingAt)
}
