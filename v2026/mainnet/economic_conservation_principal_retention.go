// Cold retention bounds resident principal evidence without resolving unknown
// causes. Exact original checkpoints remain under their existing snapshot owners;
// one authenticated segment may be hydrated at a time for delayed capture joins.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"reflect"
	"sort"
)

const economicPrincipalRetentionSchema = "urnetwork-original-principal-retention-v1"

// CompleteThrough stops at the first incomplete original, even if later
// residuals cancel. After is the latest original API result, not a cause proof.
type economicConservationPrincipalRetained struct {
	Schema           string                           `json:"schema"`
	From             economicEmissionBoundary         `json:"from"`
	Through          economicEmissionBoundary         `json:"through"`
	CompleteThrough  economicEmissionBoundary         `json:"complete_through"`
	Blocks           uint64                           `json:"original_blocks"`
	UnresolvedBlocks uint64                           `json:"unresolved_original_blocks"`
	ChainHash        string                           `json:"original_execution_chain_hash"`
	Before           []historicalPrincipalObservation `json:"first_original_parent_queries"`
	After            []historicalPrincipalObservation `json:"last_original_stake_queries"`
	Pools            []nativePrincipalPoolEffects     `json:"partial_original_pool_effects"`
}

// Each descriptor costs one bounded index entry. It never retains a second
// per-block payload/hash map alongside the cold originals.
type economicConservationPrincipalSegment struct {
	Reference monitorHistoryReference
	From      economicEmissionBoundary
	Through   economicEmissionBoundary
	Count     uint64
	ChainHash string
}

// This cache belongs to one archive view and is discarded on replacement or
// owner closure. Values are admitted originals, never a new causal certificate.
type economicConservationPrincipalCache struct {
	Reference monitorHistoryReference
	Values    []economicConservationPrincipalExecution
}

// A pure archive candidate temporarily borrows its already-validated original
// interval. The enclosing plan/apply operation retains the original checkpoint
// owner; this token also binds its context, predecessor view and exact segment.
// It never serializes and live checkpoint publication explicitly refuses it.
type economicConservationPrincipalProvisional struct {
	ctx          context.Context
	view         *economicConservationArchiveView
	reference    monitorHistoryReference
	originalHash string
	valuesHash   string
	values       []economicConservationPrincipalExecution
}

// Validate once per bounded capture walk. Later lookups use these immutable
// private originals while still observing cancellation and retained custody.
func (self *economicConservationPrincipalProvisional) validate(archive *economicConservationArchive) error {
	if self == nil {
		return nil
	}
	if self.ctx == nil || !planSha256(self.originalHash) || archive == nil || !archive.retainsPrincipal(self.reference) || len(self.values) == 0 || self.valuesHash != rootObjectHash(self.values) {
		return errors.New("principal candidate lost its admitted original interval")
	}
	return errors.Join(self.ctx.Err(), self.view.check())
}

// Partial amounts add only within the same original query. An absent stock or
// residual remains absent; signed net changes never enter unsigned income sums.
func mergeEconomicPrincipalPools(prior, next nativePrincipalPoolEffects) (nativePrincipalPoolEffects, error) {
	if prior.Query != next.Query || !reflect.DeepEqual(prior.After, next.Before) || (prior.CapturedEarnings == nil) != (next.CapturedEarnings == nil) {
		return nativePrincipalPoolEffects{}, errors.New("principal retained effects changed original position lineage")
	}
	result := next
	result.Before, result.Complete = prior.Before, prior.Complete && next.Complete
	for _, item := range []struct {
		target *string
		prior  string
	}{{target: &result.Deposits, prior: prior.Deposits}, {target: &result.Withdrawals, prior: prior.Withdrawals}, {target: &result.Refunds, prior: prior.Refunds}, {target: &result.VaultCaptures, prior: prior.VaultCaptures}, {target: &result.NativeEarnings, prior: prior.NativeEarnings}} {
		value, err := economicConservationSum(*item.target, item.prior)
		if err != nil {
			return nativePrincipalPoolEffects{}, err
		}
		*item.target = value
	}
	if result.CapturedEarnings != nil {
		value, err := economicConservationSum(*prior.CapturedEarnings, *next.CapturedEarnings)
		if err != nil {
			return nativePrincipalPoolEffects{}, err
		}
		result.CapturedEarnings = &value
	}
	result.Residual, result.StockChange = nil, nil
	if prior.Residual != nil && next.Residual != nil {
		value := *prior.Residual
		if err := nativeExecutionAdd(&value, *next.Residual, true); err != nil {
			return nativePrincipalPoolEffects{}, err
		}
		result.Residual = &value
	}
	if result.Before != nil && result.After != nil {
		before, e1 := monitorEconomicInteger(*result.Before)
		after, e2 := monitorEconomicInteger(*result.After)
		if err := errors.Join(e1, e2); err != nil {
			return nativePrincipalPoolEffects{}, err
		}
		value := new(big.Int).Sub(after, before).String()
		result.StockChange = &value
	}
	return result, nil
}

// Chain every complete original value, including unclassified mutations. A
// summary that happens to net to zero cannot substitute for the original chain.
func economicPrincipalRetentionChain(previous string, value economicConservationPrincipalExecution) string {
	return rootObjectHash(struct {
		Previous string `json:"previous"`
		Original string `json:"original"`
	}{Previous: previous, Original: rootObjectHash(value)})
}

// The aggregate is copy-on-write, so a failed candidate cannot alter the prior
// checkpoint's summary or a borrowed original API result.
func mergeEconomicPrincipalRetained(prior *economicConservationPrincipalRetained, value economicConservationPrincipalExecution) (*economicConservationPrincipalRetained, error) {
	result, err := value.Projection.reconcile(value.Outcome)
	if err != nil {
		return nil, err
	}
	next := &economicConservationPrincipalRetained{Schema: economicPrincipalRetentionSchema, From: value.Projection.Parent, Through: value.Projection.Boundary, CompleteThrough: value.Projection.Parent, Blocks: 1, Before: value.Projection.Before, After: value.Projection.After, Pools: result.Pools}
	if next.Through.Number <= next.From.Number || next.Through.Number-next.From.Number != 1 {
		return nil, errors.New("principal retention skipped an original block")
	}
	previousHash := rootObjectHash(next.From)
	if prior != nil {
		if prior.Through != next.From || !reflect.DeepEqual(prior.After, next.Before) || len(prior.Pools) != len(next.Pools) {
			return nil, errors.New("principal retention changed original predecessor or query census")
		}
		next.From, next.Before, next.Blocks = prior.From, prior.Before, prior.Blocks+1
		next.CompleteThrough, next.UnresolvedBlocks = prior.CompleteThrough, prior.UnresolvedBlocks
		previousHash = prior.ChainHash
		for index := range next.Pools {
			next.Pools[index], err = mergeEconomicPrincipalPools(prior.Pools[index], next.Pools[index])
			if err != nil {
				return nil, err
			}
		}
	}
	if result.Status != "original-stake-cause-census-reconciled" {
		next.UnresolvedBlocks++
	}
	if next.UnresolvedBlocks == 0 {
		next.CompleteThrough = next.Through
	}
	next.ChainHash = economicPrincipalRetentionChain(previousHash, value)
	return next, nil
}

// This structural check does not confer provenance: archive admission also
// reconstructs this exact head from every retained original checkpoint.
func (self *economicConservationPrincipalRetained) validate(policy economicConservationPolicy, from economicEmissionBoundary, before []historicalPrincipalObservation, cursor economicEmissionBoundary) error {
	authority := policy.Native.Observation.Execution.Principal
	if self.Schema != economicPrincipalRetentionSchema || self.From != from || self.Through.Number <= from.Number || self.Blocks != self.Through.Number-from.Number || self.Through.Number > cursor.Number || self.Through.Number == cursor.Number && self.Through.Hash != cursor.Hash || !rootCanonicalHash(self.Through.Hash) || !planSha256(self.ChainHash) || self.UnresolvedBlocks > self.Blocks || self.CompleteThrough.Number < from.Number || self.CompleteThrough.Number > self.Through.Number || !rootCanonicalHash(self.CompleteThrough.Hash) || self.UnresolvedBlocks == 0 && self.CompleteThrough != self.Through || self.UnresolvedBlocks != 0 && self.CompleteThrough.Number >= self.Through.Number || self.CompleteThrough.Number == from.Number && self.CompleteThrough != from || before != nil && !reflect.DeepEqual(before, self.Before) || len(self.Pools) != len(authority.Queries) {
		return errors.New("principal retained head changed original interval or completeness")
	}
	if err := errors.Join(validateHistoricalPrincipalReport(authority.Queries, self.Before), validateHistoricalPrincipalReport(authority.Queries, self.After)); err != nil {
		return err
	}
	for index, pool := range self.Pools {
		if pool.Query != authority.Queries[index] || !reflect.DeepEqual(pool.Before, self.Before[index].OpeningStakeAlpha) || !reflect.DeepEqual(pool.After, self.After[index].OpeningStakeAlpha) || nativeTreasuryPrincipalQuery(policy.Native.Observation.Execution.Treasury, pool.Query, policy.Native.Observation.Netuid) != (pool.CapturedEarnings != nil) || self.UnresolvedBlocks == 0 && (!pool.Complete || pool.Residual == nil || *pool.Residual != "0") {
			return errors.New("principal retained head changed original query or treasury role")
		}
		for _, amount := range []string{pool.Deposits, pool.Withdrawals, pool.Refunds, pool.VaultCaptures, pool.NativeEarnings} {
			if _, err := monitorEconomicInteger(amount); err != nil {
				return err
			}
		}
		if pool.CapturedEarnings != nil {
			if _, err := monitorEconomicInteger(*pool.CapturedEarnings); err != nil {
				return err
			}
		}
		for _, amount := range []*string{pool.Residual, pool.StockChange} {
			if amount != nil {
				value := "0"
				if err := nativeExecutionAdd(&value, *amount, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Selection belongs to the original operation over this exact segment. Nil
// selection retains the pre-existing complete-only compaction byte grammar.
func (self *economicConservationState) retainPrincipalOriginals(reference monitorHistoryReference, selected bool) error {
	if !selected || len(self.PrincipalExecutions) == 0 {
		return self.retirePrincipalEffects()
	}
	if self.Archive == nil {
		return errors.New("principal retention requires an original checkpoint segment")
	}
	head := self.Archive.PrincipalRetained
	for _, value := range self.PrincipalExecutions {
		var err error
		head, err = mergeEconomicPrincipalRetained(head, value)
		if err != nil {
			return err
		}
	}
	self.Archive.PrincipalRetained = head
	self.Archive.PrincipalRetentions = append(self.Archive.PrincipalRetentions, reference)
	self.PrincipalExecutions = nil
	return nil
}

// References must be a non-repeating subset of the exact combined catalog.
func (self *economicConservationArchive) validatePrincipalRetentions(segments map[string]monitorHistoryReference) error {
	if (self.PrincipalRetained == nil) != (len(self.PrincipalRetentions) == 0) {
		return errors.New("principal retained head lost its original segments")
	}
	seen := map[string]bool{}
	for _, reference := range self.PrincipalRetentions {
		if seen[reference.Path] || segments[reference.Path] != reference {
			return errors.New("principal retention repeats or substitutes an original segment")
		}
		seen[reference.Path] = true
	}
	return nil
}

// Old operations never acquire the new retention semantics during replay.
func (self *economicConservationArchive) retainsPrincipal(reference monitorHistoryReference) bool {
	if self != nil {
		for _, retained := range self.PrincipalRetentions {
			if retained == reference {
				return true
			}
		}
	}
	return false
}

// Build the bounded descriptor from admitted originals, reserving room for one
// full segment decode. Raw executions and their per-block hashes stay cold.
func (self *economicConservationArchiveView) indexPrincipalRetentions(original, compacted *economicConservationState) error {
	if compacted.Archive == nil {
		return nil
	}
	references := compacted.Archive.Segments
	if len(references) == 0 {
		return errors.New("principal retention omitted original segment catalog")
	}
	reference := references[len(references)-1]
	if compacted.Archive.retainsPrincipal(reference) {
		values := original.PrincipalExecutions
		if len(values) == 0 {
			return errors.New("principal retention selected an empty original interval")
		}
		head := self.principalRetained
		chain := rootObjectHash(values[0].Projection.Parent)
		for _, value := range values {
			var err error
			head, err = mergeEconomicPrincipalRetained(head, value)
			if err != nil {
				return err
			}
			chain = economicPrincipalRetentionChain(chain, value)
		}
		if !reflect.DeepEqual(head, compacted.Archive.PrincipalRetained) {
			return errors.New("principal retained summary differs from original executions")
		}
		descriptor := economicConservationPrincipalSegment{Reference: reference, From: values[0].Projection.Parent, Through: values[len(values)-1].Projection.Boundary, Count: uint64(len(values)), ChainHash: chain}
		if len(self.principalSegments) != 0 && self.principalSegments[len(self.principalSegments)-1].Through != descriptor.From {
			return errors.New("principal retained segments omitted an original interval")
		}
		raw, err := json.Marshal(head)
		if err != nil {
			return err
		}
		// The index already uses a two-times accounting margin. Reserve the
		// raw checkpoint, one decoded checkpoint and the cumulative head.
		reserved := max(self.principalReservedBytes, 2*reference.Bytes+uint64(len(raw))+256)
		if reserved > self.resources.IndexBytes/2 {
			return errMonitorEconomicCapacity
		}
		self.principalCache = nil
		self.principalReservedBytes = reserved
		if err := self.charge(descriptor); err != nil {
			return err
		}
		self.principalSegments = append(self.principalSegments, descriptor)
		self.principalRetained = head
	}
	if !reflect.DeepEqual(self.principalRetained, compacted.Archive.PrincipalRetained) {
		return errors.New("principal retained summary changed outside its original operation")
	}
	return nil
}

// A sequential capture walk crosses segment boundaries through this bounded
// lookup. Lost custody or corruption is an error, never a missing-block result.
func (self *economicConservationArchiveView) principalExecution(ctx context.Context, number uint64) (economicConservationPrincipalExecution, bool, error) {
	var empty economicConservationPrincipalExecution
	if err := errors.Join(ctx.Err(), self.check()); err != nil {
		return empty, false, err
	}
	if self == nil {
		return empty, false, nil
	}
	if value, found := self.principalExecutions[number]; found {
		return value, true, nil
	}
	index := sort.Search(len(self.principalSegments), func(index int) bool { return self.principalSegments[index].Through.Number >= number })
	if index == len(self.principalSegments) || number <= self.principalSegments[index].From.Number {
		return empty, false, nil
	}
	segment := self.principalSegments[index]
	if self.principalCache == nil || self.principalCache.Reference != segment.Reference {
		self.principalCache = nil
		var raw []byte
		var err error
		if self.copiedSourceOnly {
			raw, err = self.copiedPrincipalRead(ctx, segment.Reference)
		} else {
			var owner *monitorHistorySnapshot
			for _, candidate := range self.owners {
				if candidate.path == segment.Reference.Path {
					owner = candidate
					break
				}
			}
			if owner == nil {
				return empty, false, errors.New("principal segment has no retained original snapshot owner")
			}
			var present bool
			raw, present, err = owner.read()
			if err == nil && !present {
				return empty, false, errors.New("principal segment lost original checkpoint bytes")
			}
		}
		if err != nil {
			return empty, false, err
		}
		if err := errors.Join(ctx.Err(), self.check()); err != nil {
			return empty, false, err
		}
		if uint64(len(raw)) != segment.Reference.Bytes || monitorReadDigest(raw) != segment.Reference.Sha256 {
			return empty, false, errors.New("principal segment changed original checkpoint bytes")
		}
		var original economicConservationState
		if err := decodeMonitorHistoryInput(raw, &original); err != nil {
			return empty, false, err
		}
		values := original.PrincipalExecutions
		if uint64(len(values)) != segment.Count || len(values) == 0 || values[0].Projection.Parent != segment.From || values[len(values)-1].Projection.Boundary != segment.Through || original.ContentHash != original.hash() {
			return empty, false, errors.New("principal segment changed its admitted original interval")
		}
		chain, previous := rootObjectHash(segment.From), segment.From
		for _, value := range values {
			if err := ctx.Err(); err != nil {
				return empty, false, err
			}
			if value.Projection.Parent != previous || value.Projection.Boundary.Number != previous.Number+1 {
				return empty, false, errors.New("principal segment omitted original block lineage")
			}
			chain, previous = economicPrincipalRetentionChain(chain, value), value.Projection.Boundary
		}
		if chain != segment.ChainHash {
			return empty, false, errors.New("principal segment changed original execution chain")
		}
		if err := errors.Join(ctx.Err(), self.check()); err != nil {
			return empty, false, err
		}
		self.principalCache = &economicConservationPrincipalCache{Reference: segment.Reference, Values: values}
	}
	return self.principalCache.Values[number-segment.From.Number-1], true, nil
}

// Active records are bounded by MaximumFacts; cold records are looked up from
// their held snapshot rather than copied into an unbounded combined map.
func (self economicConservationState) captureExecutionLookup(ctx context.Context) (func(uint64) (economicConservationPrincipalExecution, bool, error), error) {
	if err := self.principalProvisional.validate(self.Archive); err != nil {
		return nil, err
	}
	values := make(map[uint64]economicConservationPrincipalExecution, len(self.PrincipalExecutions))
	for _, value := range self.PrincipalExecutions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		number := value.Projection.Boundary.Number
		_, duplicate := values[number]
		if duplicate || self.Archive != nil && self.Archive.PrincipalRetained != nil && number <= self.Archive.PrincipalRetained.Through.Number {
			return nil, errors.New("economic capture repeated original principal execution")
		}
		if self.archiveView != nil {
			if _, found := self.archiveView.principalExecutions[number]; found {
				return nil, errors.New("economic capture repeated archived principal execution")
			}
		}
		values[number] = value
	}
	return func(number uint64) (economicConservationPrincipalExecution, bool, error) {
		if err := errors.Join(ctx.Err(), self.archiveView.check()); err != nil {
			return economicConservationPrincipalExecution{}, false, err
		}
		if value, found := values[number]; found {
			return value, true, nil
		}
		if provisional := self.principalProvisional; provisional != nil {
			if err := errors.Join(provisional.ctx.Err(), provisional.view.check()); err != nil {
				return economicConservationPrincipalExecution{}, false, err
			}
			first := provisional.values[0].Projection.Boundary.Number
			if number >= first && number-first < uint64(len(provisional.values)) {
				value := provisional.values[number-first]
				if value.Projection.Boundary.Number != number {
					return economicConservationPrincipalExecution{}, false, errors.New("principal candidate changed original block order")
				}
				return value, true, nil
			}
		}
		return self.archiveView.principalExecution(ctx, number)
	}, nil
}
