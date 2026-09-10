//go:build linux || darwin

// One decision owns successful immutable key-authority reads. This shares
// chain observations, never client signatures, response bytes or head verdicts.
package validator

import (
	"context"
	"errors"
	"math/big"
	"reflect"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// Configuration and native decision identity are immutable for this lifetime;
// a different runtime or decision creates a new owner and fresh real reads.
type releaseClientKeyAuthorityV2Identity struct {
	domain              protocol.ClientKeyHistoryDomain
	request             protocol.ClientKeyObservationRequest
	validatorId         uint64
	selfUid             uint16
	deployBlock         uint64
	runtimeSpec         uint32
	transactionVersion  uint32
	stateVersion        uint8
	runtimeCodeHash     [32]byte
	runtimeMetadataHash [32]byte
}

// Operator and exact canonical boundary remain separate cache dimensions.
type releaseClientKeyAuthorityV2Key struct {
	identity releaseClientKeyAuthorityV2Identity
	domain   protocol.ClientKeyHistoryDomain
	boundary protocol.ClientKeyEffectiveBoundary
}

// Only complete successful reads remain in the map. Failed attempts wake
// existing waiters, then disappear so a later caller must perform a real read.
type releaseClientKeyAuthorityV2Entry struct {
	done   chan struct{}
	signer common.Address
	err    error
}

// Methods are safe for concurrent readers. The enclosing decision must not
// debit the shared budget while readers are in flight. Finish cancels and joins
// every synchronous leader before releasing ownership; no worker is detached.
// Retained file custody keeps its enclosing parent lifetime independently.
type releaseClientKeyAuthorityV2Reads struct {
	stateLock           sync.Mutex
	parent              context.Context
	ctx                 context.Context
	cancel              context.CancelFunc
	chain               *ChainClient
	ownedChain          *ChainClient
	identity            releaseClientKeyAuthorityV2Identity
	operatorNoIds       map[uint64]bool
	authorityKVs        map[releaseClientKeyAuthorityV2Key]*releaseClientKeyAuthorityV2Entry
	budget              *releaseHeadV2Budget
	maximumCaptureFiles uint64
	pending             sync.WaitGroup
	inflight            uint64
	closed              bool
	batched             bool
}

// A private context key prevents a caller from installing a verdict or a
// shared cross-decision cache through any exported API.
type releaseClientKeyAuthorityV2ContextKey struct{}

// The owner takes only already admitted configuration/decision inputs. Native
// metadata, stake and permit remain the actual enclosing decision's obligation.
func newReleaseClientKeyAuthorityV2Reads(ctx context.Context, chain *ChainClient, cfg *ReleaseConfig, artifact *ReleaseMeasurementArtifact, hotkey [32]byte, budget *releaseHeadV2Budget) (context.Context, *releaseClientKeyAuthorityV2Reads, error) {
	if ctx == nil || chain == nil || cfg == nil || artifact == nil || budget == nil || budget.limit == 0 || budget.limit > maxReleaseMeasurementArtifactBytes || len(cfg.Operators) == 0 || len(cfg.Operators) > 65536 || ctx.Value(releaseClientKeyAuthorityV2ContextKey{}) != nil {
		return nil, nil, errors.New("client-key authority decision owner is incomplete or nested")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	maximumCaptureFiles := cfg.EvidenceV2.Bounds.CaptureFileLimit()
	if maximumCaptureFiles >= uint64(^uint(0)>>1) {
		return nil, nil, errors.New("client-key capture count exceeds signed allocation bounds")
	}
	domain, request, err := releaseClientKeyDecisionV2(cfg, cfg.Operators[0].NoID, hotkey, artifact, connect.Id{1})
	if err != nil {
		return nil, nil, err
	}
	request.Nonce = [32]byte{1}
	if err := request.Validate(); err != nil {
		return nil, nil, err
	}
	if cfg.ValidatorID == 0 || cfg.DeployBlock == 0 || cfg.DeployBlock > request.DecisionBoundary.Block || cfg.RuntimeSpec == 0 || cfg.TransactionVersion == 0 || cfg.StateVersion == 0 {
		return nil, nil, errors.New("client-key authority runtime or deployment configuration is incomplete")
	}
	codeHash, codeErr := canonicalAttemptHex32("client-key runtime code", cfg.RuntimeCodeHash, false)
	metadataHash, metadataErr := canonicalAttemptHex32("client-key runtime metadata", cfg.RuntimeMetadataHash, false)
	if err := errors.Join(codeErr, metadataErr); err != nil {
		return nil, nil, err
	}
	if chain.client == nil || chain.coordinator == nil || chain.chainId == nil || !chain.release || !chain.chainId.IsUint64() || chain.chainId.Uint64() != domain.ChainID || chain.contractAddr != domain.Coordinator {
		return nil, nil, errors.New("client-key authority transport differs from its decision")
	}
	// Reserve owned fixed values and each admitted operator before allocating.
	if err := budget.charge(1, uint64(reflect.TypeFor[releaseClientKeyAuthorityV2Reads]().Size())+uint64(reflect.TypeFor[ChainClient]().Size())); err != nil {
		return nil, nil, err
	}
	if err := budget.charge(uint64(len(cfg.Operators)), 8+1); err != nil {
		return nil, nil, err
	}
	operators := make(map[uint64]bool, len(cfg.Operators))
	for _, operator := range cfg.Operators {
		if operator.NoID == 0 || operators[operator.NoID] {
			return nil, nil, errors.New("client-key authority operator census is incomplete or repeated")
		}
		operators[operator.NoID] = true
	}
	domain.NoID = 0
	request.ClientID, request.Nonce = [16]byte{}, [32]byte{}
	ownedCtx, cancel := context.WithCancel(ctx)
	owner := &releaseClientKeyAuthorityV2Reads{
		parent: ctx, ctx: ownedCtx, cancel: cancel, chain: chain,
		ownedChain:    &ChainClient{client: chain.client, coordinator: chain.coordinator, chainId: new(big.Int).Set(chain.chainId), contractAddr: chain.contractAddr, release: true},
		identity:      releaseClientKeyAuthorityV2Identity{domain: domain, request: request, validatorId: cfg.ValidatorID, selfUid: artifact.SelfUID, deployBlock: cfg.DeployBlock, runtimeSpec: cfg.RuntimeSpec, transactionVersion: cfg.TransactionVersion, stateVersion: cfg.StateVersion, runtimeCodeHash: codeHash, runtimeMetadataHash: metadataHash},
		operatorNoIds: operators, authorityKVs: make(map[releaseClientKeyAuthorityV2Key]*releaseClientKeyAuthorityV2Entry), budget: budget,
		maximumCaptureFiles: maximumCaptureFiles,
	}
	// The marker routes Rpc work to the private cancellable owner, but it also
	// travels into retained file custody that outlives this one Rpc operation.
	return context.WithValue(ctx, releaseClientKeyAuthorityV2ContextKey{}, owner), owner, nil
}

// Checks immutable routing before consulting shared state or issuing any read.
func (self *releaseClientKeyAuthorityV2Reads) validate(ctx context.Context, chain *ChainClient, domain protocol.ClientKeyHistoryDomain) error {
	if ctx == nil || self == nil || chain != self.chain || chain == nil || chain.client != self.ownedChain.client || chain.coordinator != self.ownedChain.coordinator || chain.chainId == nil || !chain.release || chain.chainId.Cmp(self.ownedChain.chainId) != 0 || chain.contractAddr != self.ownedChain.contractAddr {
		return errors.New("client-key authority read escaped its concrete transport owner")
	}
	if err := errors.Join(ctx.Err(), self.ctx.Err(), domain.Validate()); err != nil {
		return err
	}
	if !self.operatorNoIds[domain.NoID] {
		return errors.New("client-key authority operator is not in the admitted decision")
	}
	domain.NoID = 0
	if domain != self.identity.domain {
		return errors.New("client-key authority read changed its deployment domain")
	}
	return nil
}

// The response's complete ownership is charged before decoding proportional
// canonical wrappers. Individual signatures remain mandatory on every call.
func (self *releaseClientKeyAuthorityV2Reads) reserveResponse(ctx context.Context, chain *ChainClient, domain protocol.ClientKeyHistoryDomain, request protocol.ClientKeyObservationRequest, size uint64) error {
	if err := self.validate(ctx, chain, domain); err != nil {
		return err
	}
	request.ClientID, request.Nonce = [16]byte{}, [32]byte{}
	if request != self.identity.request {
		return errors.New("client-key authority response escaped its exact native and Evm decision")
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if self.closed {
		return errors.New("client-key authority owner is closed")
	}
	return self.budget.charge(size, 8)
}

// Retained/live reads cannot allocate a new response beyond the same remaining
// control allowance. Unowned strict callers retain their explicit original cap.
func releaseClientKeyCaptureV2Maximum(ctx context.Context, maximum uint64) (uint64, error) {
	if ctx == nil {
		return 0, errors.New("client-key capture bound has no context")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if owner, ok := ctx.Value(releaseClientKeyAuthorityV2ContextKey{}).(*releaseClientKeyAuthorityV2Reads); ok {
		if owner == nil {
			return 0, errors.New("client-key capture bound has no reader owner")
		}
		owner.stateLock.Lock()
		defer owner.stateLock.Unlock()
		if owner.closed || owner.budget.used > owner.budget.limit {
			return 0, errors.New("client-key capture budget owner is closed or exhausted")
		}
		maximum = min(maximum, (owner.budget.limit-owner.budget.used)/8)
	}
	if maximum == 0 {
		return 0, errors.New("client-key capture has exhausted its control allowance")
	}
	return maximum, nil
}

// A cancelled waiter cannot cancel another caller's in-flight real read. A
// cancelled leader cancels its own transport, removes the failed entry and
// wakes waiters without authority. The owner cancellation joins all leaders.
func (self *releaseClientKeyAuthorityV2Reads) read(ctx context.Context, chain *ChainClient, domain protocol.ClientKeyHistoryDomain, boundary protocol.ClientKeyEffectiveBoundary) (result common.Address, resultErr error) {
	if err := self.validate(ctx, chain, domain); err != nil {
		return result, err
	}
	if err := boundary.Validate(); err != nil {
		return result, err
	}
	decision := self.identity.request.DecisionBoundary
	if boundary.Block < self.identity.deployBlock || boundary.Block > decision.Block || boundary.Epoch > decision.Epoch || boundary.Block == decision.Block && boundary != decision {
		return result, errors.New("client-key authority boundary escapes the admitted decision")
	}
	leader := false
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err(), self.ctx.Err())
		if resultErr != nil {
			result = common.Address{}
		}
		if leader {
			func() {
				self.stateLock.Lock()
				defer self.stateLock.Unlock()
				self.inflight--
			}()
			self.pending.Done()
		}
	}()
	key := releaseClientKeyAuthorityV2Key{identity: self.identity, domain: domain, boundary: boundary}
	var entry *releaseClientKeyAuthorityV2Entry
	err := func() error {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.closed {
			return errors.New("client-key authority owner is closed")
		}
		entry = self.authorityKVs[key]
		if entry != nil {
			return nil
		}
		// Include the final witness key slice and pending entry/channel. Failed
		// attempts are not refunded: repeated failed work has a finite budget.
		width := 2*uint64(reflect.TypeFor[releaseClientKeyAuthorityV2Key]().Size()) + uint64(reflect.TypeFor[releaseClientKeyAuthorityV2Entry]().Size()) + uint64(reflect.TypeFor[protocol.ClientKeyEffectiveBoundary]().Size()) + 1 + 128
		if err := self.budget.charge(1, width); err != nil {
			return err
		}
		entry = &releaseClientKeyAuthorityV2Entry{done: make(chan struct{})}
		self.authorityKVs[key] = entry
		self.pending.Add(1)
		self.inflight++
		leader = true
		return nil
	}()
	if err != nil {
		return result, err
	}
	if !leader {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-self.ctx.Done():
			return result, self.ctx.Err()
		case <-entry.done:
			return entry.signer, entry.err
		}
	}
	readCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(self.ctx, cancel)
	signer, readErr := self.ownedChain.readReleaseClientKeyAuthorityUnsharedV2(readCtx, domain, boundary)
	readErr = errors.Join(readErr, readCtx.Err(), self.ctx.Err())
	stop()
	cancel()
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.closed {
			readErr = errors.Join(readErr, errors.New("client-key authority owner closed during read"))
		}
		if readErr != nil {
			signer = common.Address{}
			delete(self.authorityKVs, key)
		}
		entry.signer, entry.err = signer, readErr
		close(entry.done)
	}()
	return signer, readErr
}

// Finalization is single-owner, after all response verifications. A fresh
// canonical-height witness is required even when every lookup hit the cache.
func (self *releaseClientKeyAuthorityV2Reads) finish(resultErr error) error {
	if self == nil {
		return resultErr
	}
	var unfinished bool
	func() {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.closed {
			resultErr = errors.Join(resultErr, errors.New("client-key authority owner was already closed"))
		}
		self.closed = true
		unfinished = self.inflight != 0
	}()
	self.cancel()
	self.pending.Wait()
	if unfinished {
		resultErr = errors.Join(resultErr, errors.New("client-key authority owner closed with unfinished reads"))
	}
	if resultErr = errors.Join(resultErr, self.parent.Err()); resultErr != nil {
		return resultErr
	}
	keys := make([]releaseClientKeyAuthorityV2Key, 0, len(self.authorityKVs))
	for key := range self.authorityKVs {
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	chain := self.ownedChain
	ctx := self.parent
	if self.batched {
		boundaryKVs := make(map[protocol.ClientKeyEffectiveBoundary]bool, len(keys))
		boundaries := make([]protocol.ClientKeyEffectiveBoundary, 0, len(keys))
		for _, key := range keys {
			if !boundaryKVs[key.boundary] {
				boundaryKVs[key.boundary] = true
				boundaries = append(boundaries, key.boundary)
			}
		}
		for start := 0; start < len(boundaries); start += 128 {
			end := min(start+128, len(boundaries))
			limits := stabi.ClientKeyAuthorityRpcLimits{MaximumRequests: protocol.MaxClientKeyObservationBatchRpcRequests, MaximumMethods: protocol.MaxClientKeyObservationBatchRpcMethods, MaximumBytes: self.budget.limit - self.budget.used}
			work, err := stabi.WitnessClientKeyAuthorityBoundariesContext(ctx, chain.client, keys[0].domain, boundaries[start:end], limits)
			if err := errors.Join(err, self.budget.charge(work.Bytes, 1)); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
	callCtx, cancel := context.WithTimeout(ctx, chainCallTimeout)
	actualChainId, err := chain.client.ChainID(callCtx)
	err = errors.Join(err, callCtx.Err())
	cancel()
	if err != nil || actualChainId == nil || actualChainId.Cmp(chain.chainId) != 0 {
		return errors.Join(errors.New("client-key authority final chain identity changed"), err, ctx.Err())
	}
	var genesis *common.Hash
	callCtx, cancel = context.WithTimeout(ctx, chainCallTimeout)
	err = chain.client.Client().CallContext(callCtx, &genesis, "chain_getBlockHash", uint64(0))
	err = errors.Join(err, callCtx.Err())
	cancel()
	if err != nil || genesis == nil || [32]byte(*genesis) != self.identity.domain.GenesisHash {
		return errors.Join(errors.New("client-key authority final native genesis changed"), err, ctx.Err())
	}
	finalized, finalizedHash, err := chain.FinalizedBlockContext(ctx)
	if err != nil {
		return err
	}
	witnessedBoundaryKVs := make(map[protocol.ClientKeyEffectiveBoundary]bool, len(keys))
	for _, key := range keys {
		boundary := key.boundary
		if witnessedBoundaryKVs[boundary] {
			continue
		}
		if finalized < boundary.Block || finalized == boundary.Block && finalizedHash != boundary.Hash {
			return errors.New("client-key authority final boundary is not finalized")
		}
		var header *chainRPCBlock
		callCtx, cancel := context.WithTimeout(ctx, chainCallTimeout)
		err := chain.client.Client().CallContext(callCtx, &header, "eth_getBlockByNumber", hexutil.EncodeUint64(boundary.Block), false)
		err = errors.Join(err, callCtx.Err())
		cancel()
		block, hash, identityErr := header.identity()
		if err != nil || identityErr != nil || block != boundary.Block || hash != boundary.Hash {
			return errors.Join(errors.New("client-key authority final canonical boundary changed"), err, identityErr, ctx.Err())
		}
		witnessedBoundaryKVs[boundary] = true
	}
	return ctx.Err()
}

// Capture retains all exact source bytes first, then authenticates each
// intent's responses under its own original control allowance and decision.
type releaseClientKeyCapturedV2Response struct {
	encoded []byte
	maximum uint64
	domain  protocol.ClientKeyHistoryDomain
	request protocol.ClientKeyObservationRequest
}

// This is only shared source authentication. Native decision and complete
// measurement replay remain mandatory independent collector obligations.
func verifyReleaseClientKeyCapturedResponsesV2(ctx context.Context, chain *ChainClient, cfg *ReleaseConfig, artifact *ReleaseMeasurementArtifact, hotkey [32]byte, responses []releaseClientKeyCapturedV2Response, maximum uint64) (resultErr error) {
	budget := releaseHeadV2Budget{limit: maximum}
	if err := budget.charge(uint64(len(responses)), uint64(reflect.TypeFor[releaseClientKeyCapturedV2Response]().Size())); err != nil {
		return err
	}
	keyCtx, reads, err := newReleaseClientKeyAuthorityV2Reads(ctx, chain, cfg, artifact, hotkey, &budget)
	if err != nil {
		return err
	}
	defer func() { resultErr = reads.finish(resultErr) }()
	queries := []stabi.ClientKeyAuthorityQuery{}
	queryKVs := make(map[stabi.ClientKeyAuthorityQuery]bool)
	for _, response := range responses {
		if err := reads.reserveResponse(keyCtx, chain, response.domain, response.request, uint64(len(response.encoded))); err != nil {
			return err
		}
		members, err := releaseClientKeyBatchV2Queries(keyCtx, response, true)
		if err != nil {
			return err
		}
		for _, member := range members {
			if !queryKVs[member] {
				queryKVs[member] = true
				queries = append(queries, member)
			}
		}
	}
	if err := reads.prefetch(keyCtx, chain, queries); err != nil {
		return err
	}
	for _, response := range responses {
		if _, err := verifyReservedReleaseClientKeyCaptureV2(keyCtx, chain, response.encoded, response.maximum, response.domain, response.request, true); err != nil {
			return err
		}
	}
	return nil
}
