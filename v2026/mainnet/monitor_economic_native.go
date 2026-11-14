// A native economic role advances one retained contiguous cursor. Its reviewed
// initial window anchors continuous reads; no read authorizes a transaction.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strings"
	"time"
)

const maximumMonitorEconomicRoles = 4
const maximumMonitorEconomicEvents = 2048
const maximumMonitorEconomicBytes = 768 * 1024

// Observation supplies an independently reviewed chain/runtime/generation and
// first window. Later windows start at the retained cursor and remain bounded.
type monitorEconomicNativePolicy struct {
	archiveReferenceBytes  uint64
	runtimeAdmission       *nativeProducerRuntimeAdmission
	Role                   string                          `json:"role"`
	HistoryCatalog         *monitorHistoryCatalogPolicy    `json:"history_catalog,omitempty"`
	RuntimeCatalog         []monitorEconomicRuntimeEntry   `json:"runtime_catalog,omitempty"`
	RuntimeCapacity        *monitorEconomicRuntimeCapacity `json:"runtime_capacity,omitempty"`
	Observation            economicEmissionPolicy          `json:"observation"`
	BatchBlocks            uint64                          `json:"batch_blocks"`
	HistoryEntries         uint64                          `json:"history_entries"`
	StallSeconds           uint64                          `json:"stall_seconds"`
	ReadBudgetSeconds      uint64                          `json:"read_budget_seconds,omitempty"`
	ReadBudgetBasisSeconds *uint64                         `json:"read_budget_basis_seconds,omitempty"`
	HistoricalFinality     string                          `json:"historical_finality"`
}

func (self monitorEconomicNativePolicy) validate(expected identityExpectation) error {
	if !monitorRolePattern.MatchString(self.Role) || self.BatchBlocks == 0 || self.BatchBlocks > economicEmissionBlockLimit || self.HistoryEntries == 0 || self.HistoryEntries > maximumMonitorEconomicEvents || self.StallSeconds < 60 || self.StallSeconds > 3600 {
		return errors.New("native economic role requires bounded batch, retained history and stall policy")
	}
	if err := self.Observation.validate(); err != nil {
		return err
	}
	if self.HistoricalFinality != "owned-rpc-assertion" {
		return errors.New("continuous native economics requires an explicit bounded historical-page RPC assertion policy")
	}
	if self.Observation.Through.Number-self.Observation.From.Number > self.BatchBlocks || self.ReadBudgetSeconds != 0 && (self.ReadBudgetSeconds < 60 || self.ReadBudgetSeconds > 900) {
		return errors.New("native economic first window must fit its batch; read budget must be 60..900 seconds")
	}
	network := self.Observation.Network
	if network.NativeChain != expected.NativeChain || network.GenesisHash != expected.GenesisHash || network.EvmChainId != expected.EvmChainId {
		return errors.New("native economic role differs from independently configured monitor network")
	}
	return errors.Join(self.validateCatalog(), self.HistoryCatalog.validate())
}

func monitorEconomicNativePaths(checkpoint, metrics, role string) (string, string) {
	return strings.TrimSuffix(checkpoint, ".json") + ".native-economics-" + role + ".json", strings.TrimSuffix(metrics, ".prom") + ".native-economics-" + role + ".prom"
}

// Every retained event keeps its actual block/event identity and source bundle
// hash. Cursor-only blocks contribute to the chained batch digest, not an
// invented financial event. This is a bounded observation record, not finality.
type monitorEconomicNativeEvent struct {
	ExecutionRuntime *rootReceiptProfile           `json:"execution_runtime,omitempty"`
	PostStateRuntime *rootReceiptProfile           `json:"post_state_runtime,omitempty"`
	Block            economicEmissionBoundary      `json:"block"`
	EventsHash       string                        `json:"events_hash"`
	Incentive        *economicEmissionEvent        `json:"incentive,omitempty"`
	Fee              *economicNativeFee            `json:"fee,omitempty"`
	Context          *economicEmissionContextEvent `json:"context,omitempty"`
}

func (self monitorEconomicNativeEvent) index() uint64 {
	if self.Incentive != nil {
		return self.Incentive.EventIndex
	}
	if self.Fee != nil {
		return self.Fee.EventIndex
	}
	if self.Context != nil {
		return self.Context.EventIndex
	}
	return 0
}

type monitorEconomicNativeState struct {
	runtimeAdmission       *nativeProducerRuntimeAdmission
	ExecutionAccounting    *nativeExecutionWindow        `json:"execution_accounting,omitempty"`
	ExecutionProducer      *nativeExecutionProducerState `json:"execution_producer,omitempty"`
	Archive                *monitorEconomicNativeArchive `json:"archive,omitempty"`
	Catalog                *monitorHistoryCatalogState   `json:"history_catalog,omitempty"`
	RuntimeBoundFrom       uint64                        `json:"runtime_bound_from,omitempty"`
	LastExecutionRuntime   *rootReceiptProfile           `json:"last_execution_runtime,omitempty"`
	LastPostStateRuntime   *rootReceiptProfile           `json:"last_post_state_runtime,omitempty"`
	Cursor                 economicEmissionBoundary      `json:"cursor"`
	PendingThrough         *economicEmissionBoundary     `json:"pending_through,omitempty"`
	Finalized              *economicEmissionBoundary     `json:"observed_finalized,omitempty"`
	BatchCount             uint64                        `json:"batch_count"`
	BatchChainHash         string                        `json:"batch_chain_hash"`
	ObservedAlpha          string                        `json:"observed_alpha"`
	ObservedFeesRao        string                        `json:"observed_fees_rao"`
	History                []monitorEconomicNativeEvent  `json:"history"`
	SampleAt               time.Time                     `json:"sample_at"`
	LastReadAt             time.Time                     `json:"last_read_at"`
	LastProgressAt         time.Time                     `json:"last_progress_at"`
	UnavailableSince       time.Time                     `json:"unavailable_since"`
	Status                 string                        `json:"status"`
	Incidents              uint64                        `json:"incidents"`
	CapacityRemaining      uint64                        `json:"capacity_remaining"`
	CapacityBytesRemaining uint64                        `json:"capacity_bytes_remaining"`
}

func newMonitorEconomicNativeState(policy monitorEconomicNativePolicy) *monitorEconomicNativeState {
	return &monitorEconomicNativeState{Cursor: policy.Observation.From, BatchChainHash: policy.identityHash(), ObservedAlpha: "0", ObservedFeesRao: "0", History: []monitorEconomicNativeEvent{}, Status: "starting", CapacityRemaining: policy.HistoryEntries, CapacityBytesRemaining: maximumMonitorEconomicBytes - 2}
}

func monitorEconomicInteger(value string) (*big.Int, error) {
	n, ok := new(big.Int).SetString(value, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 256 || n.String() != value {
		return nil, errors.New("economic retained amount is not a canonical bounded integer")
	}
	return n, nil
}

func (self *monitorEconomicNativeState) validate(policy monitorEconomicNativePolicy) error {
	if err := self.ExecutionProducer.validate(policy.Observation, self.Cursor); err != nil {
		return err
	}
	var err error
	policy, err = self.runtimeReadPolicy(policy)
	if err != nil {
		return err
	}
	if self.ExecutionAccounting != nil {
		if policy.Observation.Execution == nil || self.ExecutionAccounting.From != policy.Observation.From || self.ExecutionAccounting.Through != self.Cursor {
			return errors.New("native accounting lost its original policy or retained cursor")
		}
		if err := self.ExecutionAccounting.validate(); err != nil {
			return err
		}
		if !sameNativeTreasuryAuthority(self.ExecutionAccounting.Treasury, newNativeTreasuryAmounts(policy.Observation.Execution.Treasury)) {
			return errors.New("native monitor retained another treasury authority")
		}
	} else if policy.Observation.Execution != nil && self.BatchCount != 0 {
		return errors.New("native execution policy lost retained accounting")
	}
	if self.Catalog != nil && (policy.HistoryCatalog == nil || len(self.Catalog.Revisions) == 0 || len(self.Catalog.Revisions) > maximumMonitorHistoryRevisions) {
		return errors.New("native economic catalog cannot acquire unapproved history capacity")
	}
	if err := self.Archive.validate(policy, self); err != nil {
		return err
	}
	if self.Cursor.Number < policy.Observation.From.Number || self.Cursor.Number > math.MaxUint32 || !rootCanonicalHash(self.Cursor.Hash) || !planSha256(self.BatchChainHash) || len(self.History) > int(policy.HistoryEntries) || self.CapacityRemaining != policy.HistoryEntries-uint64(len(self.History)) {
		return errors.New("native economic cursor or history exceeds its retained policy")
	}
	if self.BatchCount == 0 && (self.Cursor != policy.Observation.From || self.BatchChainHash != policy.identityHash() || len(self.History) != 0 || self.ObservedAlpha != "0" || self.ObservedFeesRao != "0") {
		return errors.New("native economic initial cursor was replaced")
	}
	if self.PendingThrough != nil && (self.PendingThrough.Number <= self.Cursor.Number || self.PendingThrough.Number-self.Cursor.Number > policy.BatchBlocks || !rootCanonicalHash(self.PendingThrough.Hash)) {
		return errors.New("native economic pending range changed or exceeds its bound")
	}
	if self.BatchCount != 0 && (self.Finalized == nil || self.Finalized.Number < self.Cursor.Number || !rootCanonicalHash(self.Finalized.Hash)) {
		return errors.New("native economic cursor has no retained finalized assertion")
	}
	for _, amount := range []string{self.ObservedAlpha, self.ObservedFeesRao} {
		if _, err := monitorEconomicInteger(amount); err != nil {
			return err
		}
	}
	if self.RuntimeBoundFrom != 0 {
		if self.RuntimeBoundFrom <= policy.Observation.From.Number || self.RuntimeBoundFrom > self.Cursor.Number {
			return errors.New("native economic retained runtime context changed")
		}
		if err := self.validateRuntimeContext(policy, self.Cursor, self.LastExecutionRuntime, self.LastPostStateRuntime); err != nil {
			return err
		}
	} else if self.LastExecutionRuntime != nil || self.LastPostStateRuntime != nil {
		return errors.New("native economic retained runtime context has no original boundary")
	}
	seen := map[string]bool{}
	alpha, fees := new(big.Int), new(big.Int)
	previous := policy.Observation.From.Number
	historyFrom := previous
	if self.Archive != nil {
		alpha, _ = monitorEconomicInteger(self.Archive.ObservedAlpha)
		fees, _ = monitorEconomicInteger(self.Archive.ObservedFeesRao)
		previous, historyFrom = self.Archive.Cursor.Number, self.Archive.Cursor.Number
	}
	var previousHash string
	var previousIndex uint64
	for _, item := range self.History {
		kinds := 0
		for _, present := range []bool{item.Incentive != nil, item.Fee != nil, item.Context != nil} {
			if present {
				kinds++
			}
		}
		if item.Block.Number <= historyFrom || item.Block.Number < previous || item.Block.Number > self.Cursor.Number || !rootCanonicalHash(item.Block.Hash) || !rootCanonicalHash(item.EventsHash) || kinds != 1 {
			return errors.New("native economic retained event has no original position")
		}
		if self.RuntimeBoundFrom != 0 && item.Block.Number >= self.RuntimeBoundFrom {
			if err := self.validateRuntimeContext(policy, item.Block, item.ExecutionRuntime, item.PostStateRuntime); err != nil {
				return err
			}
		} else if item.ExecutionRuntime != nil || item.PostStateRuntime != nil {
			return errors.New("native economic legacy event was relabeled with a new artifact")
		}
		index := item.index()
		if item.Block.Number == self.Cursor.Number && item.Block.Hash != self.Cursor.Hash {
			return errors.New("native economic last event differs from retained cursor")
		}
		if index >= economicEmissionEventLimit || item.Block.Number == previous && (item.Block.Hash != previousHash || index <= previousIndex) {
			return errors.New("native economic retained event ordering or block changed")
		}
		if item.Incentive != nil {
			event := item.Incentive
			if event.Netuid != policy.Observation.Netuid || event.Mechanism != 0 {
				return errors.New("native incentive retained identity changed")
			}
			switch event.Kind {
			case "SubtensorModule.IncentiveAlphaEmittedToMiners":
				if len(event.AlphaByUid) > int(policy.Observation.MaximumUids) {
					return errors.New("native incentive retained UID bound changed")
				}
				sum := new(big.Int)
				for _, value := range event.AlphaByUid {
					amount, err := monitorEconomicInteger(value)
					if err != nil || amount.BitLen() > 64 {
						return errors.New("native incentive retained UID amount changed")
					}
					sum.Add(sum, amount)
				}
				if sum.String() != event.TotalAlpha {
					return errors.New("native incentive retained sum changed")
				}
				alpha.Add(alpha, sum)
			case "SubtensorModule.EpochDeferred", "SubtensorModule.EpochSkipped":
				if event.TotalAlpha != "" || len(event.AlphaByUid) != 0 || event.FromBlock != item.Block.Number || event.Kind == "SubtensorModule.EpochDeferred" && event.ToBlock != item.Block.Number+1 || event.Kind == "SubtensorModule.EpochSkipped" && event.ToBlock != 0 {
					return errors.New("native epoch event retained amount or position changed")
				}
			default:
				return errors.New("native incentive retained event is unknown")
			}
		} else if item.Fee != nil {
			if item.Fee.Phase != "ApplyExtrinsic" || !rootCanonicalHash(item.Fee.ExtrinsicHash) || !slices.Contains(policy.Observation.FeePayers, item.Fee.Payer) || item.Fee.ExtrinsicIndex >= rootBodyCountLimit || item.Fee.DispatchSuccess == (item.Fee.DispatchError != "") {
				return errors.New("native fee lost exact extrinsic attribution")
			}
			amount, err := monitorEconomicInteger(item.Fee.ActualFeeRao)
			if err != nil || amount.BitLen() > 64 {
				return errors.New("native retained fee is not u64")
			}
			tip, err := monitorEconomicInteger(item.Fee.TipRao)
			if err != nil || tip.BitLen() > 64 {
				return errors.New("native retained tip is not u64")
			}
			fees.Add(fees, amount)
		} else if item.Context.Netuid != policy.Observation.Netuid || item.Context.Kind != "SubtensorModule.TempoSet" && item.Context.Kind != "SubtensorModule.SubnetOwnerChanged" {
			return errors.New("native retained execution context changed identity")
		}
		key := fmt.Sprintf("%s/%d", item.Block.Hash, index)
		if seen[key] {
			return errors.New("native economic event is duplicated")
		}
		seen[key], previous = true, item.Block.Number
		previousHash, previousIndex = item.Block.Hash, index
	}
	if self.ObservedAlpha != alpha.String() || self.ObservedFeesRao != fees.String() {
		return errors.New("native economic retained aggregate differs from original events")
	}
	raw, err := json.Marshal(self.History)
	if err != nil || len(raw) > maximumMonitorEconomicBytes || self.CapacityBytesRemaining != uint64(maximumMonitorEconomicBytes-len(raw)) {
		return errMonitorEconomicCapacity
	}
	if _, valid := monitorEconomicNativeStatusCodes[self.Status]; !valid {
		return errors.New("native economic retained status is unknown")
	}
	for _, stamp := range []time.Time{self.LastReadAt, self.LastProgressAt, self.UnavailableSince} {
		if !stamp.IsZero() && (self.SampleAt.IsZero() || stamp.After(self.SampleAt)) {
			return errors.New("native economic retained timestamp exceeds its publication")
		}
	}
	return nil
}

// The complete existing archive reader owns retries and rechecks both ends.
// A failed or partial read never returns a candidate cursor to publish.
func observeMonitorEconomicNative(ctx context.Context, client *rpcClient, policy monitorEconomicNativePolicy, state *monitorEconomicNativeState) (*economicEmissionObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, client.retryWindow)
	defer cancel()
	if err := state.admitRuntime(ctx, policy); err != nil {
		return nil, err
	}
	var err error
	policy, err = state.runtimeReadPolicy(policy)
	if err != nil {
		return nil, err
	}
	chain, err := newRootCanonicalChainBounded(client, identityExpectation{NativeChain: policy.Observation.Network.NativeChain, GenesisHash: policy.Observation.Network.GenesisHash, EvmChainId: policy.Observation.Network.EvmChainId}, policy.profiles(), 64, 2)
	if err != nil {
		return nil, err
	}
	if err := chain.network(ctx); err != nil {
		return nil, err
	}
	retained := []nativeFinalityPoint{{Number: state.Cursor.Number, Hash: state.Cursor.Hash}}
	if state.Finalized != nil {
		retained = append(retained, nativeFinalityPoint{Number: state.Finalized.Number, Hash: state.Finalized.Hash})
	}
	if state.BatchCount == 0 {
		retained = append(retained, nativeFinalityPoint{Number: policy.Observation.Through.Number, Hash: policy.Observation.Through.Hash})
	}
	if state.PendingThrough != nil {
		retained = append(retained, nativeFinalityPoint{Number: state.PendingThrough.Number, Hash: state.PendingThrough.Hash})
	}
	point, err := client.readNativeFinalityCovering(ctx, retained...)
	if err != nil {
		return nil, err
	}
	head := point.Number
	if head == state.Cursor.Number {
		return nil, nil
	}
	through := min(head, state.Cursor.Number+policy.BatchBlocks)
	if state.BatchCount == 0 {
		through = policy.Observation.Through.Number
	} else if state.PendingThrough != nil {
		through = state.PendingThrough.Number
	}
	var hash string
	if err := client.call(ctx, "chain_getBlockHash", []any{through}, &hash); err != nil {
		return nil, err
	}
	if state.BatchCount == 0 && hash != policy.Observation.Through.Hash {
		return nil, errors.Join(errRpcIntegrity, errors.New("native economic reviewed initial window changed canonically"))
	}
	requested := economicEmissionBoundary{Number: through, Hash: hash}
	if state.PendingThrough != nil && requested != *state.PendingThrough {
		return nil, errors.Join(errRpcIntegrity, errors.New("native economic pending range changed canonically"))
	}
	width := through - state.Cursor.Number
	for {
		windowContext := context.WithValue(ctx, nativeProducerStateKey{}, state.ExecutionProducer)
		window := policy.Observation
		window.From, window.Through = state.Cursor, economicEmissionBoundary{Number: state.Cursor.Number + width, Hash: hash}
		if window.Through.Number != requested.Number {
			if err := client.call(ctx, "chain_getBlockHash", []any{window.Through.Number}, &window.Through.Hash); err != nil {
				return nil, err
			}
		}
		observation, err := observeEconomicEmissionCatalog(windowContext, client, window, rootObjectHash(window), true, policy.RuntimeCatalog, true)
		if err == nil {
			if !observation.Complete || observation.Policy.From != state.Cursor || observation.Policy.Through != window.Through || uint64(len(observation.Blocks)) != width {
				return nil, errors.Join(errRpcIntegrity, errors.New("native economic batch is incomplete or changed its requested range"))
			}
			observation.RequestedThrough = &requested
			observation.ContentHash = ""
			observation.ContentHash = rootObjectHash(observation)
			// A full declared page may exceed the remaining retained-byte bound.
			// Try a smaller complete original subpage under this same deadline.
			_, err = state.append(policy, &observation, state.SampleAt, ctx)
			if err == nil {
				return &observation, nil
			}
		}
		if !errors.Is(err, errEconomicEmissionEvidenceCapacity) && !errors.Is(err, errMonitorEconomicCapacity) {
			return nil, err
		}
		if width == 1 {
			return nil, errors.Join(errMonitorEconomicCapacity, err)
		}
		width = max(uint64(1), width/2)
	}
}

// A next state is detached from the acknowledged state until durable publish.
// The declared capacity holds this role before history loss; no silent pruning.
func (self *monitorEconomicNativeState) append(policy monitorEconomicNativePolicy, observation *economicEmissionObservation, now time.Time, contexts ...context.Context) (*monitorEconomicNativeState, error) {
	if observation == nil || !observation.Complete || observation.Policy.From != self.Cursor || self.BatchCount == math.MaxUint64 {
		return nil, errors.New("native economic batch cannot advance original custody")
	}
	next := *self
	if policy.Observation.Execution != nil {
		accounting, err := appendNativeExecution(self.ExecutionAccounting, *observation, policy.Observation.From)
		if err != nil {
			return nil, err
		}
		next.ExecutionAccounting = accounting
		if policy.Observation.Execution.Producer != nil {
			producer := observation.ExecutionProducer
			if err := producer.validate(policy.Observation, observation.Policy.Through); err != nil {
				return nil, err
			}
			var previous uint64
			if self.ExecutionProducer != nil {
				previous = self.ExecutionProducer.Completed
			}
			if producer == nil || producer.Completed != previous+uint64(len(observation.Blocks)) {
				return nil, errors.New("native producer skipped or repeated original completed jobs")
			}
			next.ExecutionProducer = producer
			next.runtimeAdmission = observation.runtimeAdmission
		} else if observation.ExecutionProducer != nil {
			return nil, errors.New("legacy native owner cannot enroll an execution producer")
		}
	} else if observation.ExecutionWindow != nil {
		return nil, errors.New("legacy native role received unconfigured execution authority")
	}
	if err := next.admitRuntime(nativeRuntimeAdmissionContext(contexts), policy); err != nil {
		return nil, err
	}
	next.History = append([]monitorEconomicNativeEvent{}, self.History...)
	alpha, err := monitorEconomicInteger(self.ObservedAlpha)
	if err != nil {
		return nil, err
	}
	fees, err := monitorEconomicInteger(self.ObservedFeesRao)
	if err != nil {
		return nil, err
	}
	cursor := self.Cursor
	for _, block := range observation.Blocks {
		if block.Boundary.Number != cursor.Number+1 || block.Header.ParentHash != cursor.Hash {
			return nil, errors.Join(errRpcIntegrity, errors.New("native economic batch skipped its original predecessor"))
		}
		if block.ExecutionRuntime == nil || block.PostStateRuntime == nil {
			return nil, errors.New("native economic completed block lacks runtime read context")
		}
		if next.RuntimeBoundFrom == 0 {
			next.RuntimeBoundFrom = block.Boundary.Number
		}
		next.LastExecutionRuntime, next.LastPostStateRuntime = block.ExecutionRuntime, block.PostStateRuntime
		items := make([]monitorEconomicNativeEvent, 0, len(block.Events)+len(block.Fees)+len(block.ContextEvents))
		for _, event := range block.Events {
			items = append(items, monitorEconomicNativeEvent{Block: block.Boundary, EventsHash: block.EventsHash, ExecutionRuntime: block.ExecutionRuntime, PostStateRuntime: block.PostStateRuntime, Incentive: &event})
			if event.Kind == "SubtensorModule.IncentiveAlphaEmittedToMiners" {
				amount, err := monitorEconomicInteger(event.TotalAlpha)
				if err != nil {
					return nil, err
				}
				alpha.Add(alpha, amount)
			}
		}
		for _, fee := range block.Fees {
			items = append(items, monitorEconomicNativeEvent{Block: block.Boundary, EventsHash: block.EventsHash, ExecutionRuntime: block.ExecutionRuntime, PostStateRuntime: block.PostStateRuntime, Fee: &fee})
			amount, err := monitorEconomicInteger(fee.ActualFeeRao)
			if err != nil {
				return nil, err
			}
			fees.Add(fees, amount)
		}
		for _, event := range block.ContextEvents {
			items = append(items, monitorEconomicNativeEvent{Block: block.Boundary, EventsHash: block.EventsHash, ExecutionRuntime: block.ExecutionRuntime, PostStateRuntime: block.PostStateRuntime, Context: &event})
		}
		slices.SortFunc(items, func(a, b monitorEconomicNativeEvent) int {
			if a.index() < b.index() {
				return -1
			}
			if a.index() > b.index() {
				return 1
			}
			return 0
		})
		next.History = append(next.History, items...)
		if len(next.History) > int(policy.HistoryEntries) {
			return nil, errMonitorEconomicCapacity
		}
		cursor = block.Boundary
	}
	if cursor != observation.Policy.Through {
		return nil, errors.Join(errRpcIntegrity, errors.New("native economic batch end differs"))
	}
	next.Cursor, next.BatchCount = cursor, self.BatchCount+1
	finalized := observation.ClosingFinalized
	next.Finalized = &finalized
	next.PendingThrough = nil
	if observation.RequestedThrough != nil {
		if observation.RequestedThrough.Number < cursor.Number || observation.RequestedThrough.Number-cursor.Number > policy.BatchBlocks || !rootCanonicalHash(observation.RequestedThrough.Hash) {
			return nil, errors.New("native economic requested high-water changed")
		}
		if observation.RequestedThrough.Number > cursor.Number {
			value := *observation.RequestedThrough
			next.PendingThrough = &value
		}
	}
	next.BatchChainHash = rootObjectHash(struct{ Previous, Observation string }{Previous: self.BatchChainHash, Observation: observation.ContentHash})
	next.ObservedAlpha, next.ObservedFeesRao = alpha.String(), fees.String()
	next.SampleAt, next.LastReadAt, next.LastProgressAt = now, now, now
	next.UnavailableSince, next.Status = time.Time{}, "observed-economic-outcome-unresolved"
	next.CapacityRemaining = policy.HistoryEntries - uint64(len(next.History))
	raw, err := json.Marshal(next.History)
	if err != nil || len(raw) > maximumMonitorEconomicBytes {
		return nil, errMonitorEconomicCapacity
	}
	next.CapacityBytesRemaining = uint64(maximumMonitorEconomicBytes - len(raw))
	return &next, next.validate(policy)
}

var errMonitorEconomicCapacity = errors.New("native economic retained history capacity requires reviewed archive continuation; original cursor held")
