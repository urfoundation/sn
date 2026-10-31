//go:build linux || darwin

// What the settlement v2 audit reads (VERSION2-FINAL.md §9.2, §9.3): a pool's
// vault2 state at the finalized head, pinned by its hash; head bindings, from
// the v1 coordinator's FleetBound events and archive bindingAt reads at both
// boundaries of each covered epoch; vault2's demand history since the latest
// finalized root; and ledger objects from the operator's public endpoint.
package validator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"slices"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/stabi"
)

// The generated ABIs always parse.
var (
	settlementV2VaultABI, _       = stabi.STSettlementVault2MetaData.ParseABI()
	settlementV2CoordinatorABI, _ = stabi.STCoordinatorMetaData.ParseABI()
	settlementV2Vault             = stabi.NewSTSettlementVault2()
)

// settlementV2AuditChain reads the chain through the validator's release
// client, whose contract is the v1 coordinator.
type settlementV2AuditChain struct {
	chain *ChainClient
	vault common.Address
	// settlementVault is the v1 vault, whose entitlements the recovery
	// sources are checked against.
	settlementVault common.Address
	// deployBlock is the coordinator's deploy block: no event of either
	// contract precedes it.
	deployBlock uint64
	fleet       settlementV2FleetIndex
	// schedule returns a pool's recovery schedule; nil is
	// ledger.ScheduledRecovery.
	schedule func(vault [20]byte, noID uint64) *ledger.RecoverySchedule
}

// poolState reads the pool, and for a registered pool its pending root; with
// a pending root, also vault2's challenge window, the latest finalized root F
// and fundedThrough for the covered epochs p+1 … e, unless Verify refuses
// that range by its length alone.
func (self *settlementV2AuditChain) poolState(ctx context.Context, noID uint64) (*settlementV2PoolState, error) {
	block, blockHash, err := self.chain.FinalizedBlockContext(ctx)
	if err != nil {
		return nil, err
	}
	read := func(method string, arguments ...any) ([]any, error) {
		values, err := self.chain.settlementV2ViewAtHashContext(ctx, self.vault, &stabi.STSettlementVault2MetaData, method, arguments, block, blockHash)
		if err != nil {
			return nil, fmt.Errorf("vault2 %s of pool %d at block %d: %w", method, noID, block, err)
		}
		return values, nil
	}
	id := new(big.Int).SetUint64(noID)
	values, err := read("pools", id)
	if err != nil {
		return nil, err
	}
	pool := *abi.ConvertType(values[0], new(stabi.ISTSettlementVault2Pool)).(*stabi.ISTSettlementVault2Pool)
	state := &settlementV2PoolState{noID: noID, block: block, blockHash: blockHash, registered: pool.Registered, firstEpoch: pool.FirstEpoch, funded: map[uint64]uint256.Int{}}
	if !pool.Registered {
		return state, nil
	}
	if values, err = read("pending", id); err != nil {
		return nil, err
	}
	state.pending = *abi.ConvertType(values[0], new(stabi.ISTSettlementVault2Pending)).(*stabi.ISTSettlementVault2Pending)
	if state.pending.Seq == 0 {
		return state, nil
	}
	if values, err = read("CHALLENGE_BLOCKS"); err != nil {
		return nil, err
	}
	state.challengeBlocks = values[0].(uint64)
	if values, err = read("rootCount", id); err != nil {
		return nil, err
	}
	p := pool.FirstEpoch - 1
	if count := values[0].(*big.Int); count.Sign() > 0 {
		index := new(big.Int).Sub(count, common.Big1)
		if values, err = read("roots", id, index); err != nil {
			return nil, err
		}
		root := *abi.ConvertType(values[0], new(stabi.ISTSettlementVault2Root)).(*stabi.ISTSettlementVault2Root)
		state.previous, state.previousIndex, p = &root, index.Uint64(), root.Epoch
	}
	if e := state.pending.Epoch; e > p && e-p <= ledger.MaxCoveredEpochs {
		for k := p + 1; k <= e; k++ {
			if values, err = read("fundedThrough", id, new(big.Int).SetUint64(k)); err != nil {
				return nil, err
			}
			state.funded[k] = *uint256.MustFromBig(values[0].(*big.Int))
		}
	}
	return state, nil
}

func (self *settlementV2AuditChain) finalizedRoot(ctx context.Context, state *settlementV2PoolState, rootIndex uint64) (stabi.ISTSettlementVault2Root, error) {
	id, index := new(big.Int).SetUint64(state.noID), new(big.Int).SetUint64(rootIndex)
	values, err := self.chain.settlementV2ViewAtHashContext(ctx, self.vault, &stabi.STSettlementVault2MetaData, "roots", []any{id, index}, state.block, state.blockHash)
	if err != nil {
		return stabi.ISTSettlementVault2Root{}, fmt.Errorf("vault2 roots of pool %d at block %d: %w", state.noID, state.block, err)
	}
	return *abi.ConvertType(values[0], new(stabi.ISTSettlementVault2Root)).(*stabi.ISTSettlementVault2Root), nil
}

func (self *settlementV2AuditChain) headOracle(state *settlementV2PoolState) ledger.HeadOracle {
	return &settlementV2HeadOracle{reader: self, block: state.block, blockHash: state.blockHash}
}

// settlementV2HeadOracle answers check 6 for the epochs closed at a pinned
// finalized block.
type settlementV2HeadOracle struct {
	reader    *settlementV2AuditChain
	block     uint64
	blockHash [32]byte
}

// HeadBindings maps the epoch's candidates, the clients with a FleetBound
// event whose validity covers it, to whether bindingAt(client, epoch) is
// active at the epoch's start or end block. bindingAt reads live getUid, so
// both are archive reads at those blocks, pinned by their hashes.
func (self *settlementV2HeadOracle) HeadBindings(ctx context.Context, epoch uint64) (map[[16]byte]bool, error) {
	chain := self.reader.chain
	if err := self.reader.fleet.extend(ctx, chain, self.reader.deployBlock, self.block, self.blockHash); err != nil {
		return nil, err
	}
	candidates := self.reader.fleet.candidates(epoch)
	bound := make(map[[16]byte]bool, len(candidates))
	if len(candidates) == 0 {
		return bound, nil
	}
	e := new(big.Int).SetUint64(epoch)
	start, err := chain.ReleaseEpochStartBlockAtHashContext(ctx, self.block, self.blockHash, e)
	if err != nil {
		return nil, err
	}
	end, err := chain.ReleaseEpochEndBlockAtHashContext(ctx, self.block, self.blockHash, e)
	if err != nil {
		return nil, err
	}
	if start >= end || end > self.block {
		return nil, fmt.Errorf("epoch %d boundaries [%d,%d] are not closed at finalized block %d", epoch, start, end, self.block)
	}
	for _, boundary := range []uint64{start, end} {
		hash, err := chain.BlockHashContext(ctx, boundary)
		if err != nil {
			return nil, err
		}
		bindings, err := chain.ReleaseBindingsAtHashContext(ctx, boundary, hash, candidates, e)
		if err != nil {
			return nil, err
		}
		for i := range bindings {
			bound[candidates[i]] = bound[candidates[i]] || bindings[i].Active
		}
	}
	return bound, nil
}

// settlementV2FleetIndex holds the client and validity of every FleetBound
// event from the deploy block through scannedThrough. Revocations only
// shorten a binding, so the events' validity bounds every active binding.
type settlementV2FleetIndex struct {
	scannedThrough uint64
	scannedHash    [32]byte
	bindings       []settlementV2FleetBinding
}

type settlementV2FleetBinding struct {
	clientID           [16]byte
	validFrom, validTo uint64
}

// extend scans the coordinator's FleetBound events after scannedThrough, or
// from the deploy block, through the pinned block. A binding that covers a
// closed epoch was made before the epoch started, so the index holds it.
func (self *settlementV2FleetIndex) extend(ctx context.Context, chain *ChainClient, deployBlock, block uint64, blockHash [32]byte) error {
	if self.scannedThrough >= block {
		return nil
	}
	from := deployBlock
	if self.scannedThrough != 0 {
		hash, err := chain.BlockHashContext(ctx, self.scannedThrough)
		if err != nil {
			return err
		}
		if hash != self.scannedHash {
			return fmt.Errorf("FleetBound scan checkpoint %d is no longer canonical", self.scannedThrough)
		}
		from = self.scannedThrough + 1
	}
	bindings := slices.Clone(self.bindings)
	topics := [][]common.Hash{{settlementV2CoordinatorABI.Events["FleetBound"].ID}}
	for start := from; start <= block; start += getLogsChunkBlocks {
		logs, err := readSettlementV2Logs(ctx, chain, chain.contractAddr, topics, start, min(start+getLogsChunkBlocks-1, block))
		if err != nil {
			return fmt.Errorf("FleetBound scan: %w", err)
		}
		for i := range logs {
			event, err := chain.coordinator.UnpackFleetBoundEvent(&logs[i])
			if err != nil {
				return fmt.Errorf("FleetBound log %d of block %d: %w", logs[i].Index, logs[i].BlockNumber, err)
			}
			bindings = append(bindings, settlementV2FleetBinding{clientID: event.ClientId, validFrom: event.ValidFromEpoch, validTo: event.ValidToEpoch})
		}
	}
	self.bindings, self.scannedThrough, self.scannedHash = bindings, block, blockHash
	return nil
}

// candidates returns, in first-bound order, the clients with a binding whose
// validity covers epoch.
func (self *settlementV2FleetIndex) candidates(epoch uint64) [][16]byte {
	var out [][16]byte
	for _, binding := range self.bindings {
		if binding.validFrom <= epoch && epoch <= binding.validTo && !slices.Contains(out, binding.clientID) {
			out = append(out, binding.clientID)
		}
	}
	return out
}

// readSettlementV2Logs reads address's logs matching topics over [from, to],
// a span the public endpoint answers.
func readSettlementV2Logs(ctx context.Context, chain *ChainClient, address common.Address, topics [][]common.Hash, from, to uint64) ([]types.Log, error) {
	var logs []types.Log
	err := chain.retryChainRead(ctx, func(callCtx context.Context) error {
		var err error
		logs, err = chain.client.FilterLogs(callCtx, ethereum.FilterQuery{FromBlock: new(big.Int).SetUint64(from), ToBlock: new(big.Int).SetUint64(to), Addresses: []common.Address{address}, Topics: topics})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("eth_getLogs [%d,%d]: %w", from, to, err)
	}
	for i := range logs {
		if logs[i].Removed || logs[i].Address != address || logs[i].BlockNumber < from || logs[i].BlockNumber > to {
			return nil, fmt.Errorf("eth_getLogs [%d,%d] returned log %d outside the query", from, to, i)
		}
	}
	return logs, nil
}

// settlementV2VaultEvent is a vault2 event of the demand history. A demand
// names the finalized root its account is proven against (rootIndex) and
// the account's index; a finalization names the root it appended
// (rootIndex).
type settlementV2VaultEvent struct {
	name             string
	seq, replacedSeq uint64
	rootIndex, index *big.Int
}

func decodeSettlementV2VaultEvent(log *types.Log) (settlementV2VaultEvent, error) {
	var topic common.Hash
	if len(log.Topics) > 0 {
		topic = log.Topics[0]
	}
	var event settlementV2VaultEvent
	var err error
	switch topic {
	case settlementV2VaultABI.Events["DistributionCommitted"].ID:
		var e *stabi.STSettlementVault2DistributionCommitted
		if e, err = settlementV2Vault.UnpackDistributionCommittedEvent(log); err == nil {
			event = settlementV2VaultEvent{name: "DistributionCommitted", seq: e.Seq, replacedSeq: e.ReplacedSeq}
		}
	case settlementV2VaultABI.Events["DistributionDeleted"].ID:
		var e *stabi.STSettlementVault2DistributionDeleted
		if e, err = settlementV2Vault.UnpackDistributionDeletedEvent(log); err == nil {
			event = settlementV2VaultEvent{name: "DistributionDeleted", seq: e.Seq}
		}
	case settlementV2VaultABI.Events["PathDemanded"].ID:
		var e *stabi.STSettlementVault2PathDemanded
		if e, err = settlementV2Vault.UnpackPathDemandedEvent(log); err == nil {
			event = settlementV2VaultEvent{name: "PathDemanded", seq: e.Seq, rootIndex: e.RootIndex, index: e.Index}
		}
	case settlementV2VaultABI.Events["PathAnswered"].ID:
		var e *stabi.STSettlementVault2PathAnswered
		if e, err = settlementV2Vault.UnpackPathAnsweredEvent(log); err == nil {
			event = settlementV2VaultEvent{name: "PathAnswered", seq: e.Seq, rootIndex: e.RootIndex, index: e.Index}
		}
	case settlementV2VaultABI.Events["DistributionFinalized"].ID:
		var e *stabi.STSettlementVault2DistributionFinalized
		if e, err = settlementV2Vault.UnpackDistributionFinalizedEvent(log); err == nil {
			event = settlementV2VaultEvent{name: "DistributionFinalized", seq: e.Seq, rootIndex: e.RootIndex}
		}
	default:
		err = errors.New("not an event of the demand history")
	}
	if err != nil {
		return event, fmt.Errorf("vault2 log %d of block %d: %w", log.Index, log.BlockNumber, err)
	}
	return event, nil
}

// demandHistory scans vault2's events of the pool backwards from the pending
// commit to F's DistributionFinalized, then reads the history after it: each
// earlier pending root over F that was replaced (DistributionCommitted's
// replacedSeq) or deleted (DistributionDeleted) while a PathDemanded of it
// had no PathAnswered of the same root index and index adds one doubling,
// and those indexes are demanded again.
func (self *settlementV2AuditChain) demandHistory(ctx context.Context, state *settlementV2PoolState) (uint, []uint64, error) {
	topics := [][]common.Hash{{
		settlementV2VaultABI.Events["DistributionCommitted"].ID, settlementV2VaultABI.Events["DistributionDeleted"].ID, settlementV2VaultABI.Events["PathDemanded"].ID,
		settlementV2VaultABI.Events["PathAnswered"].ID, settlementV2VaultABI.Events["DistributionFinalized"].ID,
	}, {common.BigToHash(new(big.Int).SetUint64(state.noID))}}
	var events []settlementV2VaultEvent
	for to := state.pending.CommittedBlock; ; {
		from := max(to-min(to, getLogsChunkBlocks-1), self.deployBlock)
		logs, err := readSettlementV2Logs(ctx, self.chain, self.vault, topics, from, to)
		if err != nil {
			return 0, nil, fmt.Errorf("vault2 demand history: %w", err)
		}
		chunk, start := make([]settlementV2VaultEvent, len(logs)), -1
		for i := range logs {
			if chunk[i], err = decodeSettlementV2VaultEvent(&logs[i]); err != nil {
				return 0, nil, err
			}
			if chunk[i].name == "DistributionFinalized" && chunk[i].rootIndex.IsUint64() && chunk[i].rootIndex.Uint64() == state.previousIndex {
				start = i
			}
		}
		events = append(chunk[start+1:], events...)
		if start >= 0 {
			break
		}
		if from <= self.deployBlock {
			return 0, nil, fmt.Errorf("vault2 events up to block %d hold no DistributionFinalized of root %d of pool %d", state.pending.CommittedBlock, state.previousIndex, state.noID)
		}
		to = from - 1
	}
	type demand struct{ rootIndex, index string }
	gone, open := map[uint64]bool{}, map[uint64]map[demand]uint64{}
	for _, event := range events {
		switch event.name {
		case "DistributionCommitted":
			if event.replacedSeq != 0 {
				gone[event.replacedSeq] = true
			}
		case "DistributionDeleted":
			gone[event.seq] = true
		case "PathDemanded":
			if open[event.seq] == nil {
				open[event.seq] = map[demand]uint64{}
			}
			open[event.seq][demand{event.rootIndex.String(), event.index.String()}] = event.index.Uint64()
		case "PathAnswered":
			delete(open[event.seq], demand{event.rootIndex.String(), event.index.String()})
		}
	}
	var doublings uint
	var again []uint64
	for seq := range gone {
		if len(open[seq]) > 0 {
			doublings++
			for _, index := range open[seq] {
				again = append(again, index)
			}
		}
	}
	slices.Sort(again)
	return doublings, slices.Compact(again), nil
}

// recoveries plans the pending root's recovery components (§16 step 6): the
// pool's SurplusAssigned events from its schedule's first block through the
// pending commit, after which no epoch the root covers can receive surplus,
// placed by ledger.PlanRecovery; each placed source's amount must be vault1's
// RootMissed entitlement of it at the pinned block. A pool without a
// schedule reads nothing.
func (self *settlementV2AuditChain) recoveries(ctx context.Context, state *settlementV2PoolState) (map[uint64][]ledger.Recovery, error) {
	lookup := self.schedule
	if lookup == nil {
		lookup = ledger.ScheduledRecovery
	}
	schedule := lookup(self.vault, state.noID)
	if schedule == nil {
		return nil, nil
	}
	p := state.firstEpoch - 1
	if state.previous != nil {
		p = state.previous.Epoch
	}
	surplus := map[uint64]uint256.Int{}
	topics := [][]common.Hash{{settlementV2VaultABI.Events["SurplusAssigned"].ID}, {common.BigToHash(new(big.Int).SetUint64(state.noID))}}
	for from := max(schedule.FromBlock, self.deployBlock); from <= state.pending.CommittedBlock; from += getLogsChunkBlocks {
		logs, err := readSettlementV2Logs(ctx, self.chain, self.vault, topics, from, min(from+getLogsChunkBlocks-1, state.pending.CommittedBlock))
		if err != nil {
			return nil, fmt.Errorf("vault2 surplus: %w", err)
		}
		for i := range logs {
			event, err := settlementV2Vault.UnpackSurplusAssignedEvent(&logs[i])
			if err != nil {
				return nil, fmt.Errorf("vault2 log %d of block %d: %w", logs[i].Index, logs[i].BlockNumber, err)
			}
			if !event.NoId.IsUint64() || event.NoId.Uint64() != state.noID || !event.Epoch.IsUint64() {
				return nil, fmt.Errorf("vault2 SurplusAssigned log %d of block %d names another pool or epoch", logs[i].Index, logs[i].BlockNumber)
			}
			amount, overflow := uint256.FromBig(event.Amount)
			if overflow {
				return nil, fmt.Errorf("vault2 SurplusAssigned log %d of block %d has an amount of 2^256 or more", logs[i].Index, logs[i].BlockNumber)
			}
			total := surplus[event.Epoch.Uint64()]
			surplus[event.Epoch.Uint64()] = *total.Add(&total, amount)
		}
	}
	plan := ledger.PlanRecovery(schedule, surplus, state.firstEpoch, p, state.pending.Epoch)
	for _, components := range plan {
		for _, component := range components {
			values, err := self.chain.settlementV2ViewAtHashContext(ctx, self.settlementVault, &stabi.STSettlementVaultMetaData, "entitlement", []any{new(big.Int).SetUint64(component.Epoch), new(big.Int).SetUint64(state.noID)}, state.block, state.blockHash)
			if err != nil {
				return nil, fmt.Errorf("vault1 entitlement of epoch %d of pool %d at block %d: %w", component.Epoch, state.noID, state.block, err)
			}
			entitlement := *abi.ConvertType(values[0], new(stabi.STSettlementVaultEntitlement)).(*stabi.STSettlementVaultEntitlement)
			funded, overflow := uint256.FromBig(entitlement.Funded)
			if overflow {
				return nil, fmt.Errorf("vault1 entitlement of epoch %d funds 2^256 or more", component.Epoch)
			}
			if err := schedule.CheckSource(component.Epoch, entitlement.Status, funded); err != nil {
				return nil, err
			}
		}
	}
	return plan, nil
}

// settlementV2Objects reads ledger objects from an operator's public
// GET /sn/ledger/object endpoint, beside /sn/artifact on the API host,
// without following redirects.
type settlementV2Objects struct {
	endpoint *url.URL
	client   *http.Client
}

func newSettlementV2Objects(apiURL string) (*settlementV2Objects, error) {
	base, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("settlement v2 object endpoint: %w", err)
	}
	return &settlementV2Objects{endpoint: base.ResolveReference(&url.URL{Path: "/sn/ledger/object"}), client: &http.Client{Timeout: releaseHttpGetAttemptTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}, nil
}

// Fetch opens the object's bytes; Verify checks them against ref.
func (self *settlementV2Objects) Fetch(ctx context.Context, ref ledger.Ref) (io.ReadCloser, error) {
	endpoint := *self.endpoint
	endpoint.RawQuery = url.Values{"hash": {ref.String()}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := self.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("GET %s: status %d", endpoint.String(), response.StatusCode)
	}
	return response.Body, nil
}
