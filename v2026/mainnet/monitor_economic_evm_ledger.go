// The retained ledger checks event effects against exact before/after contract
// state. A Claimed event is accepted liability, never proof of a token payment.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"slices"
	"time"
)

func (self monitorEconomicEvmSnapshot) conservation(policy monitorEconomicEvmPolicy) error {
	amount := func(name string) *big.Int { value, _ := monitorEconomicInteger(self.Counters[name]); return value }
	if err := self.validate(policy); err != nil {
		return errors.Join(errRpcIntegrity, err)
	}
	if policy.ContractKind == "reserve-sink" {
		if amount("principal").Cmp(amount("liveStake")) > 0 {
			return monitorEvmIntegrity("reserve principal exceeds observed live backing")
		}
	} else if new(big.Int).Add(amount("totalPaid"), amount("escrowAccounted")).Cmp(amount("totalCaptured")) != 0 || new(big.Int).Add(amount("pendingFunding"), amount("outstandingLiability")).Cmp(amount("escrowAccounted")) != 0 || amount("escrowAccounted").Cmp(amount("liveEscrowStake")) > 0 {
		return monitorEvmIntegrity("vault captured, paid, pending, liability or live backing does not conserve")
	}
	selected := new(big.Int)
	for _, value := range self.Pools {
		n, _ := monitorEconomicInteger(value)
		selected.Add(selected, n)
	}
	for _, value := range self.Credits {
		n, _ := monitorEconomicInteger(value)
		selected.Add(selected, n)
	}
	limit := "principal"
	if policy.ContractKind == "settlement-vault" {
		limit = "outstandingLiability"
	}
	if selected.Cmp(amount(limit)) > 0 {
		return monitorEvmIntegrity("independently selected credit/carry/principal exceeds total contract liability")
	}
	return nil
}

// State summaries are intentionally separate from the independent expected
// claim census. A complete contract event page is not complete provider payout.
func monitorEvmCheckLedger(policy monitorEconomicEvmPolicy, before monitorEconomicEvmSnapshot, block monitorEvmBlock) error {
	if err := before.conservation(policy); err != nil {
		return err
	}
	if err := block.Snapshot.conservation(policy); err != nil {
		return err
	}
	next := before.clone()
	adjust := func(values map[string]string, key, amount string, subtract bool) error {
		current, e1 := monitorEconomicInteger(values[key])
		delta, e2 := monitorEconomicInteger(amount)
		if e1 != nil || e2 != nil {
			return monitorEvmIntegrity("economic ledger amount or expected key is absent")
		}
		if subtract {
			current.Sub(current, delta)
		} else {
			current.Add(current, delta)
		}
		if current.Sign() < 0 || current.BitLen() > 256 {
			return monitorEvmIntegrity("economic event underflows or overflows retained contract liability")
		}
		values[key] = current.String()
		return nil
	}
	consumedFunding := map[string]bool{}
	for _, event := range block.Events {
		values := event.Values
		pool, coldkey := values["noId"], values["coldkey"]
		_, poolExpected := next.Pools[pool]
		_, creditExpected := next.Credits[coldkey]
		add := func(name, value string) error { return adjust(next.Counters, name, value, false) }
		sub := func(name, value string) error { return adjust(next.Counters, name, value, true) }
		var err error
		if policy.ContractKind == "reserve-sink" {
			switch event.Name {
			case "RecorderFixed":
			case "ReservePrincipalAdded":
				err = add("principal", values["amount"])
				if err == nil && next.Counters["principal"] != values["totalPrincipal"] {
					err = monitorEvmIntegrity("reserve principal event total differs")
				}
				if err == nil && poolExpected {
					err = adjust(next.Pools, pool, values["amount"], false)
					if err == nil && next.Pools[pool] != values["operatorPrincipal"] {
						err = monitorEvmIntegrity("reserve operator principal differs")
					}
				}
				live, e1 := monitorEconomicInteger(values["liveStake"])
				principal, e2 := monitorEconomicInteger(values["totalPrincipal"])
				if err == nil && (e1 != nil || e2 != nil || live.Cmp(principal) < 0) {
					err = monitorEvmIntegrity("reserve addition lacks asserted backing")
				}
			default:
				err = monitorEvmIntegrity("reserve ledger event is outside the reviewed purpose")
			}
		} else {
			switch event.Name {
			case "CoordinatorFixed", "EscrowRegistered", "PoolRegistered", "PoolActiveSet", "EmissionDeferred", "EmissionDustDeferred":
				// Registration and dust deferral do not create a captured amount.
			case "EmissionCaptured":
				for _, name := range []string{"totalCaptured", "pendingFunding", "escrowAccounted"} {
					if err == nil {
						err = add(name, values["amount"])
					}
				}
			case "EntitlementFinalized", "RootMissed":
				key := values["epoch"] + "/" + pool
				funded, ok := block.Funded[key]
				if !ok || consumedFunding[key] {
					return monitorEvmIntegrity("economic entitlement funding was omitted or counted twice")
				}
				consumedFunding[key] = true
				err = sub("pendingFunding", funded)
				if err == nil {
					err = add("outstandingLiability", funded)
				}
				if event.Name == "RootMissed" {
					if funded != values["carried"] {
						return monitorEvmIntegrity("missed-root carry differs from original funding")
					}
					if err == nil && poolExpected {
						err = adjust(next.Pools, pool, funded, false)
					}
				} else if poolExpected {
					carry, e1 := monitorEconomicInteger(next.Pools[pool])
					amount, e2 := monitorEconomicInteger(funded)
					total, e3 := monitorEconomicInteger(values["total"])
					if e1 != nil || e2 != nil || e3 != nil || new(big.Int).Add(carry, amount).Cmp(total) != 0 {
						return monitorEvmIntegrity("finalized entitlement omitted original pool carry")
					}
					next.Pools[pool] = "0"
				}
			case "Claimed":
				if creditExpected {
					err = adjust(next.Credits, coldkey, values["amount"], false)
				}
			case "ClaimPaid":
				if creditExpected && next.Credits[coldkey] != values["amount"] {
					return monitorEvmIntegrity("aggregate claim payment differs from retained complete credit")
				}
				if creditExpected {
					next.Credits[coldkey] = "0"
				}
				err = add("totalPaid", values["amount"])
				if err == nil {
					err = sub("escrowAccounted", values["amount"])
				}
				if err == nil {
					err = sub("outstandingLiability", values["amount"])
				}
			case "ClaimPaymentDeferred":
				if creditExpected && next.Credits[coldkey] != values["creditAlphaRao"] {
					return monitorEvmIntegrity("deferred aggregate credit differs from retained accepted liability")
				}
			case "EntitlementExpired":
				if poolExpected {
					err = adjust(next.Pools, pool, values["unclaimed"], false)
					if err == nil && next.Pools[pool] != values["operatorCarry"] {
						err = monitorEvmIntegrity("expired entitlement carry differs")
					}
				}
			default:
				err = monitorEvmIntegrity("vault ledger event is outside the reviewed purpose")
			}
		}
		if err != nil {
			return err
		}
	}
	if len(consumedFunding) != len(block.Funded) {
		return monitorEvmIntegrity("economic block has unconsumed funding observations")
	}
	// Live backing can move independently through native emissions. It is
	// checked against liabilities, not falsely inferred from EVM event deltas.
	live := "liveStake"
	if policy.ContractKind == "settlement-vault" {
		live = "liveEscrowStake"
	}
	next.Counters[live] = block.Snapshot.Counters[live]
	if rootObjectHash(next) != rootObjectHash(block.Snapshot) {
		return monitorEvmIntegrity("complete economic event census differs from block-pinned counters, carry or credit")
	}
	return nil
}

// Whole blocks, including quiet blocks, enter the chained digest. History
// never silently evicts an original financial witness to make room for a page.
type monitorEconomicEvmState struct {
	Cursor            economicEmissionBoundary    `json:"cursor"`
	PendingThrough    *economicEmissionBoundary   `json:"pending_through,omitempty"`
	Finalized         *economicEmissionBoundary   `json:"observed_finalized,omitempty"`
	MappingHash       string                      `json:"mapping_hash,omitempty"`
	BatchCount        uint64                      `json:"batch_count"`
	BatchChainHash    string                      `json:"batch_chain_hash"`
	Snapshot          *monitorEconomicEvmSnapshot `json:"snapshot,omitempty"`
	History           []monitorEconomicEvmEvent   `json:"history"`
	Fees              []monitorEconomicEvmFee     `json:"fees"`
	SampleAt          time.Time                   `json:"sample_at"`
	LastReadAt        time.Time                   `json:"last_read_at"`
	LastProgressAt    time.Time                   `json:"last_progress_at"`
	UnavailableSince  time.Time                   `json:"unavailable_since"`
	Status            string                      `json:"status"`
	Incidents         uint64                      `json:"incidents"`
	CapacityRemaining uint64                      `json:"capacity_remaining"`
	Archive           *monitorEconomicEvmArchive  `json:"archive,omitempty"`
	Catalog           *monitorHistoryCatalogState `json:"history_catalog,omitempty"`
	Retained          *monitorEvmRetainedCounts   `json:"retained_observations,omitempty"`
}

func newMonitorEconomicEvmState(policy monitorEconomicEvmPolicy) *monitorEconomicEvmState {
	return &monitorEconomicEvmState{Cursor: policy.From, BatchChainHash: policy.identityHash(), History: []monitorEconomicEvmEvent{}, Fees: []monitorEconomicEvmFee{}, Status: "starting", CapacityRemaining: policy.HistoryEntries}
}

func (self *monitorEconomicEvmState) validate(policy monitorEconomicEvmPolicy) error {
	if self.Catalog != nil && (policy.HistoryCatalog == nil || len(self.Catalog.Revisions) == 0 || len(self.Catalog.Revisions) > maximumMonitorHistoryRevisions) {
		return errors.New("EVM economic catalog lacks its original bounded authority")
	}
	if err := self.Archive.validate(policy, self); err != nil {
		return err
	}
	if self.Retained != nil {
		counts, err := self.retainedCounts()
		if err != nil || *self.Retained != counts {
			return errors.Join(errors.New("EVM checkpoint omitted retained event or fee history"), err)
		}
	}
	if self.Cursor.Number < policy.From.Number || self.Cursor.Number > math.MaxInt64 || !rootCanonicalHash(self.Cursor.Hash) || !planSha256(self.BatchChainHash) || uint64(len(self.History)+len(self.Fees)) > policy.HistoryEntries || self.CapacityRemaining != policy.HistoryEntries-uint64(len(self.History)+len(self.Fees)) {
		return errors.New("EVM economic retained cursor or history exceeds its policy")
	}
	if self.BatchCount == 0 && (self.Archive != nil || self.Cursor != policy.From || len(self.History) != 0 || len(self.Fees) != 0 || self.BatchChainHash != policy.identityHash()) {
		return errors.New("EVM economic original cursor was replaced")
	}
	if self.PendingThrough != nil && (self.PendingThrough.Number <= self.Cursor.Number || self.PendingThrough.Number-self.Cursor.Number > policy.BatchBlocks || !rootCanonicalHash(self.PendingThrough.Hash)) {
		return errors.New("EVM economic pending original high water differs")
	}
	if self.BatchCount != 0 && (self.Snapshot == nil || self.Finalized == nil || self.Finalized.Number < self.Cursor.Number || !rootCanonicalHash(self.Finalized.Hash) || !planSha256(self.MappingHash)) {
		return errors.New("EVM economic acknowledged cursor lacks its state and finality assertion")
	}
	if self.Snapshot != nil {
		if err := self.Snapshot.conservation(policy); err != nil {
			return err
		}
	}
	from := policy.From.Number
	if self.Archive != nil {
		from = self.Archive.Cursor.Number
	}
	previous := from
	var previousIndex uint64
	var previousHash string
	for _, event := range self.History {
		if event.Block.Number <= from || event.Block.Number < previous || event.Block.Number > self.Cursor.Number || !rootCanonicalHash(event.Block.Hash) || !rootCanonicalHash(event.TransactionHash) || !planSha256(event.ReceiptHash) || event.TransactionIndex >= maximumMonitorEvmTransactions || event.LogIndex >= maximumMonitorEvmLogs || len(event.Name) > 64 || len(event.Values) > 16 || event.Block.Number == previous && (event.Block.Hash != previousHash || event.LogIndex <= previousIndex) || event.Block.Number == self.Cursor.Number && event.Block.Hash != self.Cursor.Hash {
			return errors.New("EVM economic original event identity/order changed")
		}
		for key, value := range event.Values {
			if len(key) > 64 || len(value) > 128 {
				return errors.New("EVM economic event value exceeds its bound")
			}
		}
		if err := event.CaptureIdentity.validate(policy, event); err != nil {
			return err
		}
		previous, previousIndex, previousHash = event.Block.Number, event.LogIndex, event.Block.Hash
	}
	previous = from
	previousIndex = 0
	previousHash = ""
	for _, fee := range self.Fees {
		price, e1 := monitorEconomicInteger(fee.EffectiveGasPriceWei)
		cost, e2 := monitorEconomicInteger(fee.FeeWei)
		if e1 != nil || e2 != nil || fee.GasUsed == 0 || new(big.Int).Mul(price, new(big.Int).SetUint64(fee.GasUsed)).Cmp(cost) != 0 || !slices.Contains(policy.FeePayers, fee.Payer) || fee.Block.Number <= from || fee.Block.Number < previous || fee.Block.Number > self.Cursor.Number || !rootCanonicalHash(fee.Block.Hash) || !rootCanonicalHash(fee.TransactionHash) || !planSha256(fee.ReceiptHash) || fee.TransactionIndex >= maximumMonitorEvmTransactions || fee.Block.Number == previous && (fee.Block.Hash != previousHash || fee.TransactionIndex <= previousIndex) || fee.Block.Number == self.Cursor.Number && fee.Block.Hash != self.Cursor.Hash {
			return errors.New("EVM economic fee lost original transaction attribution")
		}
		previous, previousIndex, previousHash = fee.Block.Number, fee.TransactionIndex, fee.Block.Hash
	}
	if _, ok := monitorEconomicEvmStatusCodes[self.Status]; !ok {
		return errors.New("EVM economic retained status is unknown")
	}
	for _, stamp := range []time.Time{self.LastReadAt, self.LastProgressAt, self.UnavailableSince} {
		if !stamp.IsZero() && (self.SampleAt.IsZero() || stamp.After(self.SampleAt)) {
			return errors.New("EVM economic timestamp exceeds its publication")
		}
	}
	raw, err := json.Marshal(self)
	if err != nil {
		return err
	}
	if len(raw) > maximumMonitorEconomicBytes {
		return errMonitorEconomicCapacity
	}
	return nil
}

func (self *monitorEconomicEvmState) append(policy monitorEconomicEvmPolicy, observation *monitorEvmObservation, now time.Time) (*monitorEconomicEvmState, error) {
	if observation.From != self.Cursor || observation.Finalized.Number < self.Cursor.Number || now.IsZero() || now.Before(self.SampleAt) {
		return nil, monitorEvmIntegrity("EVM economic observation changed its original cursor or clock")
	}
	if self.Snapshot != nil && rootObjectHash(*self.Snapshot) != rootObjectHash(observation.Before) {
		return nil, monitorEvmIntegrity("EVM economic retained contract state changed on restart")
	}
	next := *self
	next.History = append([]monitorEconomicEvmEvent{}, self.History...)
	next.Fees = append([]monitorEconomicEvmFee{}, self.Fees...)
	snapshot := observation.Before.clone()
	next.Snapshot = &snapshot
	for _, block := range observation.Blocks {
		if block.Boundary.Number != next.Cursor.Number+1 || block.ParentHash != next.Cursor.Hash {
			return nil, monitorEvmIntegrity("EVM economic page skipped an original block")
		}
		if uint64(len(next.History)+len(next.Fees)+len(block.Events)+len(block.Fees)) > policy.HistoryEntries {
			if next.Cursor == self.Cursor {
				return nil, errMonitorEconomicCapacity
			}
			break
		}
		if err := monitorEvmCheckLedger(policy, *next.Snapshot, block); err != nil {
			return nil, err
		}
		prior := next
		next.History = append(slices.Clone(next.History), block.Events...)
		next.Fees = append(slices.Clone(next.Fees), block.Fees...)
		next.Cursor = block.Boundary
		value := block.Snapshot.clone()
		next.Snapshot = &value
		next.BatchChainHash = rootObjectHash(struct {
			Prior string          `json:"prior"`
			Block monitorEvmBlock `json:"block"`
		}{Prior: next.BatchChainHash, Block: block})
		raw, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		if len(raw) > maximumMonitorEconomicBytes {
			next = prior
			if next.Cursor == self.Cursor {
				return nil, errMonitorEconomicCapacity
			}
			break
		}
	}
	if next.Cursor != self.Cursor {
		if next.BatchCount == math.MaxUint64 {
			return nil, errMonitorEconomicCapacity
		}
		next.BatchCount++
		next.LastProgressAt = now
	}
	next.Finalized = &observation.Finalized
	next.MappingHash = observation.MappingHash
	next.PendingThrough = nil
	if next.Cursor.Number < observation.RequestedThrough.Number {
		pending := observation.RequestedThrough
		next.PendingThrough = &pending
	}
	next.SampleAt = now
	next.LastReadAt = now
	next.UnavailableSince = time.Time{}
	next.Status = "caught-up"
	if next.Cursor.Number < observation.Finalized.Number {
		next.Status = "observed-economic-outcome-unresolved"
	}
	next.CapacityRemaining = policy.HistoryEntries - uint64(len(next.History)+len(next.Fees))
	counts, err := next.retainedCounts()
	if err != nil {
		return nil, err
	}
	next.Retained = &counts
	if err := next.validate(policy); err != nil {
		return nil, err
	}
	return &next, nil
}

// Header pages start at the acknowledged cursor even after a long outage.
// Canonical lookups remain explicit RPC assertions, not invented ancestry proofs.
func observeMonitorEconomicEvm(ctx context.Context, client *rpcClient, policy monitorEconomicEvmPolicy, state *monitorEconomicEvmState) (*monitorEvmObservation, error) {
	ctx, cancel := context.WithTimeout(ctx, client.retryWindow)
	defer cancel()
	expected := identityExpectation{NativeChain: policy.Network.NativeChain, GenesisHash: policy.Network.GenesisHash, EvmChainId: policy.Network.EvmChainId}
	mapping, err := client.readFinalizedMapping(ctx, &expected)
	if err != nil {
		return nil, err
	}
	contract, err := monitorEvmAbi(policy.ContractKind)
	if err != nil {
		return nil, err
	}
	reader := &monitorEvmReader{client: client, policy: policy, contract: contract}
	genesis, err := reader.header(ctx, 0)
	if err != nil {
		return nil, err
	}
	if genesis.Hash().Hex() != policy.EvmGenesisHash {
		return nil, monitorEvmIntegrity("EVM economic genesis differs")
	}
	current, err := reader.header(ctx, state.Cursor.Number)
	if err != nil {
		return nil, err
	}
	if current.Hash().Hex() != state.Cursor.Hash {
		return nil, monitorEvmIntegrity("EVM economic original cursor changed canonically")
	}
	head := economicEmissionBoundary{Number: mapping.EvmHeader.Number, Hash: mapping.EvmHeader.Hash}
	if head.Number < state.Cursor.Number || head.Number == state.Cursor.Number && head.Hash != state.Cursor.Hash {
		return nil, monitorEvmIntegrity("EVM economic finalized head regressed")
	}
	if state.Finalized != nil {
		if head.Number < state.Finalized.Number {
			return nil, monitorEvmIntegrity("EVM economic finalized high water regressed")
		}
		prior, err := reader.header(ctx, state.Finalized.Number)
		if err != nil {
			return nil, err
		}
		if prior.Hash().Hex() != state.Finalized.Hash {
			return nil, monitorEvmIntegrity("EVM economic prior finalized assertion changed")
		}
	}
	through := min(head.Number, state.Cursor.Number+policy.BatchBlocks)
	if state.PendingThrough != nil {
		through = state.PendingThrough.Number
	}
	boundary, err := reader.header(ctx, through)
	if err != nil {
		return nil, err
	}
	requested := economicEmissionBoundary{Number: through, Hash: boundary.Hash().Hex()}
	if state.PendingThrough != nil && requested != *state.PendingThrough {
		return nil, monitorEvmIntegrity("EVM economic retained pending page changed")
	}
	before, err := reader.snapshot(ctx, state.Cursor.Hash)
	if err != nil {
		return nil, err
	}
	observation := &monitorEvmObservation{From: state.Cursor, RequestedThrough: requested, Finalized: head, MappingHash: rootObjectHash(mapping), Before: before, Blocks: []monitorEvmBlock{}}
	parent := state.Cursor.Hash
	prior := before
	for number := state.Cursor.Number + 1; number <= through; number++ {
		header, err := reader.header(ctx, number)
		if err != nil {
			if errors.Is(err, errMonitorEconomicCapacity) && len(observation.Blocks) != 0 {
				break
			}
			return nil, err
		}
		if header.ParentHash.Hex() != parent {
			return nil, monitorEvmIntegrity("EVM economic historical page is discontinuous")
		}
		block, err := reader.block(ctx, header)
		if err != nil {
			if errors.Is(err, errMonitorEconomicCapacity) && len(observation.Blocks) != 0 {
				break
			}
			return nil, err
		}
		if err := monitorEvmCheckLedger(policy, prior, block); err != nil {
			return nil, err
		}
		observation.Blocks = append(observation.Blocks, block)
		prior = block.Snapshot
		parent = block.Boundary.Hash
	}
	// Closing reads have a separate fixed three-header allowance. The data page
	// cannot consume the budget that verifies its selected head and original cursor.
	reader.used = 0
	for _, bound := range []economicEmissionBoundary{state.Cursor, requested, head} {
		header, err := reader.header(ctx, bound.Number)
		if err != nil {
			return nil, err
		}
		if header.Hash().Hex() != bound.Hash {
			return nil, monitorEvmIntegrity("EVM economic canonical binding changed during observation")
		}
	}
	if _, err := client.readFinalizedMappingAtIdentity(ctx, mapping.Identity); err != nil {
		return nil, err
	}
	return observation, ctx.Err()
}
