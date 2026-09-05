package main

// Replays every ordinary terminal fleet at each signed validator decision.
// Bounded concurrent RPC work still yields one deterministic sealed transcript.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/urfoundation/sn/ss58"
)

const (
	finalFleetAuditDefaultWorkers = 4
	finalFleetAuditMaximumWorkers = 8
	finalPublicFleetAuditSchema   = "urnetwork-final-public-fleet-audit-v1"
)

// Seals an exact ordinary-fleet replay scope independently of raw exchanges.
// Offline verification therefore rejects absent, truncated, or substituted scope.
type FinalPublicFleetAudit struct {
	Schema                   string `json:"schema"`
	OrdinaryFleetGenerations uint64 `json:"ordinary_fleet_generations"`
	CycleSnapshots           uint64 `json:"cycle_snapshots"`
	ReplayJobs               uint64 `json:"replay_jobs"`
	MemberBindings           uint64 `json:"member_bindings"`
	ProjectionHash           string `json:"projection_hash"`
}

// Limits the hashed scope to canonical binding fields queried by every replay.
// Full values remain in the signed object and its artifacts instead of the summary.
type finalPublicFleetAuditProjection struct {
	Schema string                                 `json:"schema"`
	Netuid uint16                                 `json:"netuid"`
	Fleets []finalPublicFleetAuditFleetProjection `json:"fleets"`
	Cycles []finalPublicFleetAuditCycleProjection `json:"cycles"`
}

// Captures one ordinary generation's immutable source fields for the sealed
// replay-scope hash.
type finalPublicFleetAuditFleetProjection struct {
	FleetID        uint64                         `json:"fleet_id"`
	UID            uint16                         `json:"uid"`
	NativeHotkey   string                         `json:"native_hotkey"`
	Hotkey         string                         `json:"hotkey"`
	Coldkey        string                         `json:"coldkey"`
	FleetKey       string                         `json:"fleet_key"`
	CommitmentHash string                         `json:"commitment_hash"`
	Generation     uint64                         `json:"generation"`
	Members        []FinalHeadFleetMemberEvidence `json:"members"`
}

// Carries one signed decision point whose paired heads anchor replay.
type finalPublicFleetAuditCycleProjection struct {
	SettlementEpoch uint64    `json:"settlement_epoch"`
	NativeHead      ChainHead `json:"native_head"`
	EVMHead         ChainHead `json:"evm_head"`
}

// Multiplies audit dimensions while rejecting an unrepresentable scope.
func finalFleetAuditProduct(left, right uint64) (uint64, error) {
	if left != 0 && right > ^uint64(0)/left {
		return 0, errors.New("ordinary fleet audit count overflows uint64")
	}
	return left * right, nil
}

// Adds audit dimensions while rejecting an unrepresentable scope.
func finalFleetAuditSum(left, right uint64) (uint64, error) {
	if right > ^uint64(0)-left {
		return 0, errors.New("ordinary fleet audit count overflows uint64")
	}
	return left + right, nil
}

// Reconstructs complete replay scope from artifact-bound semantic evidence.
// Both sealing and offline verification use it, rejecting producer count-only claims.
func finalPublicFleetAuditForEvidence(evidence *FinalSemanticEvidence) (FinalPublicFleetAudit, error) {
	fleets, err := finalFleetAuditFleets(evidence)
	if err != nil {
		return FinalPublicFleetAudit{}, err
	}
	cycles, err := finalFleetAuditCycles(evidence)
	if err != nil {
		return FinalPublicFleetAudit{}, err
	}
	fleetCount, cycleCount := uint64(len(fleets)), uint64(len(cycles))
	jobs, err := finalFleetAuditProduct(fleetCount, cycleCount)
	if err != nil || jobs == 0 {
		if err == nil {
			err = errors.New("ordinary fleet audit has zero jobs")
		}
		return FinalPublicFleetAudit{}, err
	}
	var membersPerCycle uint64
	projection := finalPublicFleetAuditProjection{
		Schema: finalPublicFleetAuditSchema,
		Netuid: evidence.Netuid,
		Fleets: make([]finalPublicFleetAuditFleetProjection, 0, len(fleets)),
		Cycles: make([]finalPublicFleetAuditCycleProjection, 0, len(cycles)),
	}
	for _, fleet := range fleets {
		members := append([]FinalHeadFleetMemberEvidence(nil), fleet.Members...)
		membersPerCycle, err = finalFleetAuditSum(membersPerCycle, uint64(len(members)))
		if err != nil {
			return FinalPublicFleetAudit{}, err
		}
		projection.Fleets = append(projection.Fleets, finalPublicFleetAuditFleetProjection{
			FleetID: fleet.FleetID, UID: fleet.UID, NativeHotkey: fleet.NativeHotkey,
			Hotkey: fleet.Hotkey, Coldkey: fleet.Coldkey, FleetKey: fleet.FleetKey,
			CommitmentHash: fleet.CommitmentHash, Generation: fleet.Generation, Members: members,
		})
	}
	for _, cycle := range cycles {
		projection.Cycles = append(projection.Cycles, finalPublicFleetAuditCycleProjection{
			SettlementEpoch: cycle.SettlementEpoch, NativeHead: cycle.NativeHead, EVMHead: cycle.EVMHead,
		})
	}
	memberBindings, err := finalFleetAuditProduct(membersPerCycle, cycleCount)
	if err != nil || memberBindings == 0 {
		if err == nil {
			err = errors.New("ordinary fleet audit has zero member bindings")
		}
		return FinalPublicFleetAudit{}, err
	}
	projectionHash, err := canonicalHashHex(projection)
	if err != nil {
		return FinalPublicFleetAudit{}, err
	}
	return FinalPublicFleetAudit{
		Schema: finalPublicFleetAuditSchema, OrdinaryFleetGenerations: fleetCount,
		CycleSnapshots: cycleCount, ReplayJobs: jobs, MemberBindings: memberBindings,
		ProjectionHash: projectionHash,
	}, nil
}

// Rejects incomplete or internally inconsistent sealed summary fields before
// their source projection is recomputed.
func verifyFinalPublicFleetAuditShape(audit FinalPublicFleetAudit) error {
	if audit.Schema != finalPublicFleetAuditSchema || audit.OrdinaryFleetGenerations == 0 || audit.CycleSnapshots == 0 || audit.ReplayJobs == 0 || audit.MemberBindings == 0 {
		return errors.New("public ordinary fleet audit summary is incomplete")
	}
	jobs, err := finalFleetAuditProduct(audit.OrdinaryFleetGenerations, audit.CycleSnapshots)
	if err != nil || jobs != audit.ReplayJobs || audit.MemberBindings%audit.ReplayJobs != 0 {
		return errors.New("public ordinary fleet audit counts are inconsistent")
	}
	if err := requireFinalHex32("public ordinary fleet audit projection hash", audit.ProjectionHash); err != nil {
		return err
	}
	return nil
}

// Rejects a sealed v5 transcript unless its replay scope exactly matches the
// artifact-bound source projection, including stale same-version transcripts.
func verifyFinalPublicFleetAudit(evidence *FinalSemanticEvidence, got FinalPublicFleetAudit) error {
	if err := verifyFinalPublicFleetAuditShape(got); err != nil {
		return err
	}
	want, err := finalPublicFleetAuditForEvidence(evidence)
	if err != nil {
		return fmt.Errorf("public ordinary fleet audit projection: %w", err)
	}
	if got != want {
		return errors.New("public ordinary fleet audit summary differs from sealed projection")
	}
	return nil
}

// Optionally narrows the conservative public-RPC worker bound. Out-of-range
// values are clamped so release reporting cannot overwhelm an archive provider.
type FinalSemanticFleetAuditConcurrencyReader interface {
	FleetAuditConcurrency() int
}

// Combines lifecycle queries with native UID lookup without broadening the
// lifecycle surface for callers that do not request ordinary-fleet replay.
type finalFleetAuditChainReader interface {
	FinalSemanticLifecycleChainReader
	NativeUID(context.Context, uint16, uint16, ChainHead) (FinalNativeUIDState, []FinalRPCExchange, error)
}

// Represents one unique paired native/EVM decision snapshot. Different heads
// within one settlement epoch intentionally remain distinct audits.
type finalFleetAuditCycle struct {
	SettlementEpoch uint64
	NativeHead      ChainHead
	EVMHead         ChainHead
}

// Holds validated evidence for one replay job. Lifecycle generations stay in
// their separate verifier at their own historical heads.
type finalFleetAuditFleet struct {
	FleetID        uint64
	UID            uint16
	NativeHotkey   string
	Hotkey         string
	Coldkey        string
	FleetKey       string
	CommitmentHash string
	Generation     uint64
	Members        []FinalHeadFleetMemberEvidence
	NativeTerminal ChainHead
}

// Assigns a stable key and index before workers begin. The index deterministically
// owns each deduplicated RPC read.
type finalFleetAuditJob struct {
	Index int
	Key   string
	Fleet finalFleetAuditFleet
	Cycle finalFleetAuditCycle
}

// Holds exchanges before public-transcript admission. Callers append only after
// every concurrent read succeeds.
type finalFleetAuditExchangeGroup struct {
	Chain     string
	Head      ChainHead
	Exchanges []FinalRPCExchange
}

// Carries immutable response values and exchanges owned by one canonical index.
type finalFleetAuditJobResult struct {
	Done       bool
	Commitment FinalNativeFleetCommitmentState
	Mirror     FinalFleetMirrorChainState
	UID        FinalNativeUIDState
	Bindings   []FinalFleetBindingChainState
	Groups     []finalFleetAuditExchangeGroup
	Err        error
}

// Shares exact head/key queries across jobs. It is safe for concurrent callers;
// external RPC work occurs outside stateLock and completion is immutable.
type finalFleetAuditReadCache struct {
	stateLock sync.Mutex
	entries   map[string]*finalFleetAuditReadCacheEntry
}

// Holds one immutable response until every waiter observes its completion.
type finalFleetAuditReadCacheEntry struct {
	done      chan struct{}
	value     any
	exchanges []FinalRPCExchange
	err       error
}

// Queries each key once, then returns the same state and transcript to waiters.
// The deterministic owner table, rather than cache arrival, controls emission.
func (self *finalFleetAuditReadCache) read(ctx context.Context, key string, query func(context.Context) (any, []FinalRPCExchange, error)) (any, []FinalRPCExchange, error) {
	if self == nil || key == "" || query == nil {
		return nil, nil, errors.New("fleet audit cache read is incomplete")
	}
	self.stateLock.Lock()
	if self.entries == nil {
		self.entries = map[string]*finalFleetAuditReadCacheEntry{}
	}
	entry, exists := self.entries[key]
	if !exists {
		entry = &finalFleetAuditReadCacheEntry{done: make(chan struct{})}
		self.entries[key] = entry
	}
	self.stateLock.Unlock()
	if !exists {
		value, exchanges, err := query(ctx)
		self.stateLock.Lock()
		entry.value = value
		entry.exchanges = append([]FinalRPCExchange(nil), exchanges...)
		entry.err = err
		close(entry.done)
		self.stateLock.Unlock()
	}
	select {
	case <-entry.done:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	if entry.err != nil {
		return nil, nil, entry.err
	}
	return entry.value, append([]FinalRPCExchange(nil), entry.exchanges...), nil
}

// Binds a cache entry to one exact semantic request.
func finalFleetAuditReadKey(kind string, head ChainHead, values ...string) string {
	return fmt.Sprintf("%s/%020d/%s/%s", kind, head.Number, head.Hash, strings.Join(values, "/"))
}

// Caps deliberately small public-RPC fanout. Readers may lower it for a
// constrained archive endpoint but cannot exceed the audited maximum.
func finalFleetAuditWorkers(reader FinalSemanticChainReader, jobCount int) int {
	if jobCount <= 0 {
		return 0
	}
	workers := finalFleetAuditDefaultWorkers
	if configured, ok := reader.(FinalSemanticFleetAuditConcurrencyReader); ok {
		workers = configured.FleetAuditConcurrency()
	}
	if workers < 1 {
		workers = 1
	}
	if workers > finalFleetAuditMaximumWorkers {
		workers = finalFleetAuditMaximumWorkers
	}
	if workers > jobCount {
		workers = jobCount
	}
	return workers
}

// Lists every distinct signed decision snapshot across validators and
// dishonest-deposit control/recovery campaigns.
func finalFleetAuditCycles(evidence *FinalSemanticEvidence) ([]finalFleetAuditCycle, error) {
	if evidence == nil {
		return nil, errors.New("fleet audit evidence is unavailable")
	}
	cycles := make(map[string]finalFleetAuditCycle)
	add := func(cycle FinalCRv4Cycle) error {
		if cycle.SettlementEpoch == 0 {
			return errors.New("fleet audit cycle has zero settlement epoch")
		}
		if err := verifyFinalHead("fleet audit native snapshot", cycle.NativeSnapshot); err != nil {
			return err
		}
		if err := verifyFinalHead("fleet audit EVM snapshot", cycle.EVMSnapshot); err != nil {
			return err
		}
		row := finalFleetAuditCycle{SettlementEpoch: cycle.SettlementEpoch, NativeHead: cycle.NativeSnapshot, EVMHead: cycle.EVMSnapshot}
		key := fmt.Sprintf("%020d/%020d/%s/%020d/%s", row.SettlementEpoch, row.NativeHead.Number, row.NativeHead.Hash, row.EVMHead.Number, row.EVMHead.Hash)
		if prior, exists := cycles[key]; exists && prior != row {
			return errors.New("fleet audit cycle key is ambiguous")
		}
		cycles[key] = row
		return nil
	}
	for _, validator := range evidence.Validators {
		for _, cycle := range validator.Cycles {
			if err := add(cycle); err != nil {
				return nil, err
			}
		}
	}
	if evidence.DishonestDeposit != nil {
		for _, decisions := range [][]FinalDishonestDepositDecision{evidence.DishonestDeposit.Penalties, evidence.DishonestDeposit.Recoveries} {
			for _, decision := range decisions {
				if err := add(decision.Cycle); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(cycles) == 0 {
		return nil, errors.New("fleet audit has no signed validator decision snapshots")
	}
	result := make([]finalFleetAuditCycle, 0, len(cycles))
	for _, cycle := range cycles {
		result = append(result, cycle)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SettlementEpoch != result[j].SettlementEpoch {
			return result[i].SettlementEpoch < result[j].SettlementEpoch
		}
		if result[i].NativeHead.Number != result[j].NativeHead.Number {
			return result[i].NativeHead.Number < result[j].NativeHead.Number
		}
		if result[i].NativeHead.Hash != result[j].NativeHead.Hash {
			return result[i].NativeHead.Hash < result[j].NativeHead.Hash
		}
		if result[i].EVMHead.Number != result[j].EVMHead.Number {
			return result[i].EVMHead.Number < result[j].EVMHead.Number
		}
		return result[i].EVMHead.Hash < result[j].EVMHead.Hash
	})
	return result, nil
}

// Validates and selects ordinary terminal generations. Lifecycle generations
// retain their own replay proof and historical validity boundary.
func finalFleetAuditFleets(evidence *FinalSemanticEvidence) ([]finalFleetAuditFleet, error) {
	if evidence == nil {
		return nil, errors.New("fleet audit evidence is unavailable")
	}
	lifecycleFleets := map[uint64]bool{}
	if evidence.FleetLifecycle != nil {
		lifecycleFleets[uint64(fleetLifecycleTargetFleet)] = true
		lifecycleFleets[uint64(fleetLifecycleCompanionFleet)] = true
	}
	seenFleetKeys := map[string]bool{}
	seenUIDs := map[uint16]bool{}
	seenHotkeys := map[string]bool{}
	seenClientIDs := map[string]bool{}
	result := make([]finalFleetAuditFleet, 0, len(evidence.HeadFleets))
	for _, fleet := range evidence.HeadFleets {
		if lifecycleFleets[fleet.FleetID] {
			continue
		}
		if fleet.FleetID == 0 || fleet.Generation == 0 || fleet.MemberCount != 4 || len(fleet.Members) != fleet.MemberCount || !fleet.Registered {
			return nil, fmt.Errorf("ordinary fleet %d projection is incomplete", fleet.FleetID)
		}
		if err := requireFinalHex32(fmt.Sprintf("ordinary fleet %d key", fleet.FleetID), fleet.FleetKey); err != nil {
			return nil, err
		}
		if err := requireFinalHex32(fmt.Sprintf("ordinary fleet %d commitment", fleet.FleetID), fleet.CommitmentHash); err != nil {
			return nil, err
		}
		hotkeyBytes, hotkeyPrefix, err := ss58.Decode(fleet.Hotkey)
		if err != nil || hotkeyPrefix != ss58.BittensorPrefix {
			return nil, stateMismatchError(err, "ordinary fleet %d hotkey is not canonical Bittensor SS58", fleet.FleetID)
		}
		if _, coldkeyPrefix, coldkeyErr := ss58.Decode(fleet.Coldkey); coldkeyErr != nil || coldkeyPrefix != ss58.BittensorPrefix {
			return nil, stateMismatchError(coldkeyErr, "ordinary fleet %d coldkey is not canonical Bittensor SS58", fleet.FleetID)
		}
		hotkey := "0x" + fmt.Sprintf("%x", hotkeyBytes[:])
		if seenFleetKeys[fleet.FleetKey] || seenUIDs[fleet.UID] || seenHotkeys[hotkey] {
			return nil, fmt.Errorf("ordinary fleet %d reuses fleet, UID, or hotkey identity", fleet.FleetID)
		}
		seenFleetKeys[fleet.FleetKey], seenUIDs[fleet.UID], seenHotkeys[hotkey] = true, true, true
		members := append([]FinalHeadFleetMemberEvidence(nil), fleet.Members...)
		for index, member := range members {
			if index != 0 && members[index-1].ClientID >= member.ClientID {
				return nil, fmt.Errorf("ordinary fleet %d members are not canonical", fleet.FleetID)
			}
			if _, ok := evidenceFixedHex(member.ClientID, 16); !ok || requireFinalHex32(fmt.Sprintf("ordinary fleet %d member key", fleet.FleetID), member.ClientKey) != nil || member.ValidFromEpoch == 0 || member.ValidToEpoch < member.ValidFromEpoch {
				return nil, fmt.Errorf("ordinary fleet %d member %d projection is invalid", fleet.FleetID, index+1)
			}
			if seenClientIDs[member.ClientID] {
				return nil, fmt.Errorf("ordinary fleet %d reuses member client %s", fleet.FleetID, member.ClientID)
			}
			seenClientIDs[member.ClientID] = true
		}
		result = append(result, finalFleetAuditFleet{FleetID: fleet.FleetID, UID: fleet.UID, NativeHotkey: fleet.Hotkey, Hotkey: hotkey, Coldkey: fleet.Coldkey, FleetKey: fleet.FleetKey, CommitmentHash: fleet.CommitmentHash, Generation: fleet.Generation, Members: members, NativeTerminal: fleet.Snapshot})
	}
	if len(result) == 0 {
		return nil, errors.New("fleet audit has no ordinary terminal generations")
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].FleetID != result[j].FleetID {
			return result[i].FleetID < result[j].FleetID
		}
		return result[i].Generation < result[j].Generation
	})
	return result, nil
}

// Orders the full cross-product before workers begin.
func finalFleetAuditJobs(fleets []finalFleetAuditFleet, cycles []finalFleetAuditCycle) []finalFleetAuditJob {
	jobs := make([]finalFleetAuditJob, 0, len(fleets)*len(cycles))
	for _, cycle := range cycles {
		for _, fleet := range fleets {
			job := finalFleetAuditJob{Fleet: fleet, Cycle: cycle}
			job.Key = fmt.Sprintf("%020d/%020d/%s/%020d/%020d/%s/%020d/%s", fleet.FleetID, fleet.Generation, fleet.FleetKey, cycle.SettlementEpoch, cycle.NativeHead.Number, cycle.NativeHead.Hash, cycle.EVMHead.Number, cycle.EVMHead.Hash)
			jobs = append(jobs, job)
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Key < jobs[j].Key })
	for index := range jobs {
		jobs[index].Index = index
	}
	return jobs
}

// Assigns duplicate reads to the lowest canonical index, independent of which
// worker first reaches the cache.
func finalFleetAuditReadOwners(jobs []finalFleetAuditJob) map[string]int {
	owners := make(map[string]int, len(jobs)*7)
	for _, job := range jobs {
		keys := []string{
			finalFleetAuditReadKey("native-commitment", job.Cycle.NativeHead, job.Fleet.Hotkey),
			finalFleetAuditReadKey("mirror", job.Cycle.EVMHead, job.Fleet.Hotkey),
			finalFleetAuditReadKey("native-uid", job.Cycle.NativeHead, fmt.Sprintf("%d", job.Fleet.UID)),
		}
		for _, member := range job.Fleet.Members {
			keys = append(keys, finalFleetAuditReadKey("binding-at", job.Cycle.EVMHead, fmt.Sprintf("%d", job.Cycle.SettlementEpoch), member.ClientID))
		}
		for _, key := range keys {
			if prior, exists := owners[key]; !exists || job.Index < prior {
				owners[key] = job.Index
			}
		}
	}
	return owners
}

// Collects one native/EVM generation projection at one cycle. Cached external
// calls emit groups only through their stable owner.
func finalFleetAuditState(ctx context.Context, reader finalFleetAuditChainReader, netuid uint16, job finalFleetAuditJob, cache *finalFleetAuditReadCache, owners map[string]int) finalFleetAuditJobResult {
	result := finalFleetAuditJobResult{Done: true, Bindings: make([]FinalFleetBindingChainState, 0, len(job.Fleet.Members))}
	appendOwned := func(key, chain string, head ChainHead, exchanges []FinalRPCExchange) {
		if owner, exists := owners[key]; exists && owner == job.Index {
			result.Groups = append(result.Groups, finalFleetAuditExchangeGroup{Chain: chain, Head: head, Exchanges: exchanges})
		}
	}
	commitmentKey := finalFleetAuditReadKey("native-commitment", job.Cycle.NativeHead, job.Fleet.Hotkey)
	value, exchanges, err := cache.read(ctx, commitmentKey, func(ctx context.Context) (any, []FinalRPCExchange, error) {
		state, exchanges, err := reader.NativeFleetCommitment(ctx, netuid, job.Fleet.Hotkey, job.Cycle.NativeHead)
		return state, exchanges, err
	})
	if err != nil {
		result.Err = fmt.Errorf("ordinary fleet %d cycle %d native commitment: %w", job.Fleet.FleetID, job.Cycle.SettlementEpoch, err)
		return result
	}
	var ok bool
	if result.Commitment, ok = value.(FinalNativeFleetCommitmentState); !ok {
		result.Err = errors.New("fleet audit cache returned wrong native commitment type")
		return result
	}
	appendOwned(commitmentKey, "substrate", job.Cycle.NativeHead, exchanges)

	mirrorKey := finalFleetAuditReadKey("mirror", job.Cycle.EVMHead, job.Fleet.Hotkey)
	value, exchanges, err = cache.read(ctx, mirrorKey, func(ctx context.Context) (any, []FinalRPCExchange, error) {
		state, exchanges, err := reader.FleetMirror(ctx, job.Fleet.Hotkey, job.Cycle.EVMHead)
		return state, exchanges, err
	})
	if err != nil {
		result.Err = fmt.Errorf("ordinary fleet %d cycle %d coordinator mirror: %w", job.Fleet.FleetID, job.Cycle.SettlementEpoch, err)
		return result
	}
	if result.Mirror, ok = value.(FinalFleetMirrorChainState); !ok {
		result.Err = errors.New("fleet audit cache returned wrong mirror type")
		return result
	}
	appendOwned(mirrorKey, "evm", job.Cycle.EVMHead, exchanges)

	uidKey := finalFleetAuditReadKey("native-uid", job.Cycle.NativeHead, fmt.Sprintf("%d", job.Fleet.UID))
	value, exchanges, err = cache.read(ctx, uidKey, func(ctx context.Context) (any, []FinalRPCExchange, error) {
		state, exchanges, err := reader.NativeUID(ctx, netuid, job.Fleet.UID, job.Cycle.NativeHead)
		return state, exchanges, err
	})
	if err != nil {
		result.Err = fmt.Errorf("ordinary fleet %d cycle %d native UID: %w", job.Fleet.FleetID, job.Cycle.SettlementEpoch, err)
		return result
	}
	if result.UID, ok = value.(FinalNativeUIDState); !ok {
		result.Err = errors.New("fleet audit cache returned wrong native UID type")
		return result
	}
	appendOwned(uidKey, "substrate", job.Cycle.NativeHead, exchanges)

	for _, member := range job.Fleet.Members {
		member := member
		bindingKey := finalFleetAuditReadKey("binding-at", job.Cycle.EVMHead, fmt.Sprintf("%d", job.Cycle.SettlementEpoch), member.ClientID)
		value, exchanges, err = cache.read(ctx, bindingKey, func(ctx context.Context) (any, []FinalRPCExchange, error) {
			state, exchanges, err := reader.FleetBinding(ctx, member.ClientID, job.Cycle.SettlementEpoch, job.Cycle.EVMHead)
			return state, exchanges, err
		})
		if err != nil {
			result.Err = fmt.Errorf("ordinary fleet %d cycle %d member %s binding: %w", job.Fleet.FleetID, job.Cycle.SettlementEpoch, member.ClientID, err)
			return result
		}
		binding, typeOK := value.(FinalFleetBindingChainState)
		if !typeOK {
			result.Err = errors.New("fleet audit cache returned wrong binding type")
			return result
		}
		result.Bindings = append(result.Bindings, binding)
		appendOwned(bindingKey, "evm", job.Cycle.EVMHead, exchanges)
	}
	return result
}

// Compares every state field, including terminal membership and applicability
// at the historical cycle.
func verifyFinalFleetAuditJobState(job finalFleetAuditJob, result finalFleetAuditJobResult) (ChainHead, error) {
	if result.Commitment.Hotkey != job.Fleet.Hotkey || result.Commitment.CommitmentHash != job.Fleet.CommitmentHash || result.Commitment.Block != job.Cycle.NativeHead || result.Commitment.CommitmentBlock == 0 || result.Commitment.CommitmentBlock > job.Cycle.NativeHead.Number {
		return ChainHead{}, fmt.Errorf("ordinary fleet %d cycle %d native commitment differs", job.Fleet.FleetID, job.Cycle.SettlementEpoch)
	}
	if result.Mirror.Hotkey != job.Fleet.Hotkey || result.Mirror.CommitmentHash != job.Fleet.CommitmentHash || result.Mirror.Block != job.Cycle.EVMHead || result.Mirror.FinalizedBlock == 0 || result.Mirror.FinalizedBlock != result.Commitment.CommitmentBlock || result.Mirror.FinalizedBlock > job.Cycle.NativeHead.Number || requireFinalHex32("ordinary fleet mirror finalized hash", result.Mirror.FinalizedBlockHash) != nil {
		return ChainHead{}, fmt.Errorf("ordinary fleet %d cycle %d coordinator mirror differs", job.Fleet.FleetID, job.Cycle.SettlementEpoch)
	}
	if result.UID.UID != job.Fleet.UID || result.UID.Hotkey != job.Fleet.NativeHotkey || result.UID.Coldkey != job.Fleet.Coldkey || !result.UID.Registered {
		return ChainHead{}, fmt.Errorf("ordinary fleet %d cycle %d native UID ownership differs", job.Fleet.FleetID, job.Cycle.SettlementEpoch)
	}
	if len(result.Bindings) != len(job.Fleet.Members) {
		return ChainHead{}, fmt.Errorf("ordinary fleet %d cycle %d binding census differs", job.Fleet.FleetID, job.Cycle.SettlementEpoch)
	}
	for index, member := range job.Fleet.Members {
		binding := result.Bindings[index]
		if !binding.Active || binding.Cleaned || binding.CleanedAtEpoch != 0 || binding.ClientID != member.ClientID || binding.FleetID != job.Fleet.FleetKey || binding.Hotkey != job.Fleet.Hotkey || binding.ClientKey != member.ClientKey || binding.CommitmentHash != job.Fleet.CommitmentHash || binding.Generation != job.Fleet.Generation || binding.ValidFromEpoch != member.ValidFromEpoch || binding.ValidToEpoch != member.ValidToEpoch || binding.UID != job.Fleet.UID || binding.Block != job.Cycle.EVMHead || job.Cycle.SettlementEpoch < binding.ValidFromEpoch || job.Cycle.SettlementEpoch > binding.ValidToEpoch {
			return ChainHead{}, fmt.Errorf("ordinary fleet %d cycle %d member %s binding differs", job.Fleet.FleetID, job.Cycle.SettlementEpoch, member.ClientID)
		}
	}
	return ChainHead{Number: result.Mirror.FinalizedBlock, Hash: result.Mirror.FinalizedBlockHash}, nil
}

// Selects a stable root cause after workers join. Cancellation fallout cannot
// mask a concrete response error or missing worker result.
func finalFleetAuditResultsError(ctx context.Context, results []finalFleetAuditJobResult) error {
	for index, result := range results {
		if !result.Done {
			return fmt.Errorf("ordinary fleet audit job %d has no result", index)
		}
		if result.Err != nil && !errors.Is(result.Err, context.Canceled) {
			return fmt.Errorf("ordinary fleet audit job %d: %w", index, result.Err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for index, result := range results {
		if result.Err != nil {
			return fmt.Errorf("ordinary fleet audit job %d: %w", index, result.Err)
		}
	}
	return nil
}

// Runs complete ordinary-generation replay in stable job/read order. Failed
// concurrent work never emits a partial, tamperable transcript.
func executeFinalSemanticFleetAudit(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader) ([]finalFleetAuditExchangeGroup, error) {
	if ctx == nil || evidence == nil || reader == nil {
		return nil, errors.New("public ordinary fleet audit is unavailable")
	}
	lifecycleReader, ok := reader.(finalFleetAuditChainReader)
	if !ok {
		return nil, errors.New("public semantic reader does not expose ordinary fleet replay")
	}
	cycles, err := finalFleetAuditCycles(evidence)
	if err != nil {
		return nil, err
	}
	fleets, err := finalFleetAuditFleets(evidence)
	if err != nil {
		return nil, err
	}
	jobs := finalFleetAuditJobs(fleets, cycles)
	if len(jobs) == 0 {
		return nil, errors.New("ordinary fleet audit has no jobs")
	}
	owners := finalFleetAuditReadOwners(jobs)
	cache := &finalFleetAuditReadCache{}
	auditCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]finalFleetAuditJobResult, len(jobs))
	jobIndices := make(chan int)
	workers := finalFleetAuditWorkers(reader, len(jobs))
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer wait.Done()
			for index := range jobIndices {
				if err := auditCtx.Err(); err != nil {
					results[index] = finalFleetAuditJobResult{Done: true, Err: err}
					continue
				}
				result := finalFleetAuditState(auditCtx, lifecycleReader, evidence.Netuid, jobs[index], cache, owners)
				results[index] = result
				if result.Err != nil {
					cancel()
				}
			}
		}()
	}
	for index := range jobs {
		if auditCtx.Err() != nil {
			results[index] = finalFleetAuditJobResult{Done: true, Err: auditCtx.Err()}
			continue
		}
		jobIndices <- index
	}
	close(jobIndices)
	wait.Wait()
	if err := finalFleetAuditResultsError(ctx, results); err != nil {
		return nil, err
	}

	mirrorHeads := map[uint64]ChainHead{}
	groups := make([]finalFleetAuditExchangeGroup, 0, len(jobs)*7)
	for index, job := range jobs {
		mirrorHead, verifyErr := verifyFinalFleetAuditJobState(job, results[index])
		if verifyErr != nil {
			return nil, verifyErr
		}
		if prior, exists := mirrorHeads[mirrorHead.Number]; exists && prior != mirrorHead {
			return nil, fmt.Errorf("ordinary fleet audit mirror block %d has conflicting hashes", mirrorHead.Number)
		}
		mirrorHeads[mirrorHead.Number] = mirrorHead
		groups = append(groups, results[index].Groups...)
	}
	mirrorNumbers := make([]uint64, 0, len(mirrorHeads))
	for number := range mirrorHeads {
		mirrorNumbers = append(mirrorNumbers, number)
	}
	sort.Slice(mirrorNumbers, func(i, j int) bool { return mirrorNumbers[i] < mirrorNumbers[j] })
	for _, number := range mirrorNumbers {
		head := mirrorHeads[number]
		exchanges, canonicalErr := reader.CanonicalSubstrateHead(ctx, head)
		if canonicalErr != nil {
			return nil, fmt.Errorf("ordinary fleet audit mirrored native block %d: %w", head.Number, canonicalErr)
		}
		groups = append(groups, finalFleetAuditExchangeGroup{Chain: "substrate", Head: head, Exchanges: exchanges})
	}
	return groups, nil
}

// Appends completed deterministic work only after every bounded RPC job and
// state comparison passes.
func verifyFinalSemanticFleetAudit(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if appendExchanges == nil {
		return errors.New("ordinary fleet audit transcript appender is unavailable")
	}
	groups, err := executeFinalSemanticFleetAudit(ctx, evidence, reader)
	if err != nil {
		return err
	}
	for _, group := range groups {
		if err := appendExchanges(group.Chain, group.Head, group.Exchanges); err != nil {
			return err
		}
	}
	return nil
}
