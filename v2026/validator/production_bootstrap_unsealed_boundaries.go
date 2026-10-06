//go:build linux || darwin

// Tail boundary proofs join signed local ancestry to real canonical EVM views.
// They do not prove steering intents, provider bindings or applied weights.
package validator

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

const ProductionBootstrapUnsealedBoundarySchema = "urnetwork-production-bootstrap-unsealed-boundaries-v1"
const productionBootstrapUnsealedMaximumBoundaries = 256

// Counts cover every record strictly after the authenticated committed cut.
// Repeated records at one boundary share its read, never another operator's.
type ProductionBootstrapUnsealedBoundary struct {
	Boundary AttemptBoundary `json:"boundary"`
	Records  uint64          `json:"records"`
}

// An optional addition preserves earlier inventory hashes and their narrower
// scope. Only complete canonical observations set HistoricalSources true.
type ProductionBootstrapUnsealedBoundaryProof struct {
	Schema            string                                `json:"schema"`
	HistoricalSources bool                                  `json:"historical_sources_authenticated"`
	Boundaries        []ProductionBootstrapUnsealedBoundary `json:"boundaries"`
}

// Reopening checks the bounded census and exact tail coverage independently
// of its outer hash; this shape check never creates historical authority.
func (self ProductionBootstrapUnsealedBoundaryProof) validate(prefix ProductionBootstrapOperatorPrefix, records uint64) error {
	if self.Schema != ProductionBootstrapUnsealedBoundarySchema || !self.HistoricalSources || len(self.Boundaries) > productionBootstrapUnsealedMaximumBoundaries {
		return errors.New("unsealed tail boundary proof lacks its bounded historical scope")
	}
	remaining := records
	for i, member := range self.Boundaries {
		if err := validateAttemptBoundary(member.Boundary); err != nil {
			return err
		}
		if member.Boundary.EVMBlock == 0 || member.Boundary.SettlementEpoch != prefix.Epoch || member.Records == 0 || member.Records > remaining ||
			i > 0 && member.Boundary.EVMBlock <= self.Boundaries[i-1].Boundary.EVMBlock {
			return errors.New("unsealed tail boundary proof changes its complete ordered census")
		}
		remaining -= member.Records
	}
	if remaining != 0 {
		return errors.New("unsealed tail boundary proof omits signed records")
	}
	return nil
}

// This finite collector is populated only after actual record verification.
// A conflicting hash at the same height cannot become a second valid view.
type productionBootstrapUnsealedBoundaryCensus struct {
	byBlock map[uint64]ProductionBootstrapUnsealedBoundary
}

// New distinct boundaries consume the fixed allowance before allocation.
func (self *productionBootstrapUnsealedBoundaryCensus) add(boundary AttemptBoundary) error {
	if self.byBlock == nil {
		self.byBlock = map[uint64]ProductionBootstrapUnsealedBoundary{}
	}
	member, present := self.byBlock[boundary.EVMBlock]
	if present && member.Boundary != boundary {
		return errors.New("unsealed signed records conflict at one historical boundary")
	}
	if !present && len(self.byBlock) >= productionBootstrapUnsealedMaximumBoundaries {
		return errors.New("unsealed distinct tail boundaries exceed their finite bound")
	}
	member.Boundary, member.Records = boundary, member.Records+1
	self.byBlock[boundary.EVMBlock] = member
	return nil
}

// A stable numerical census keeps proof hashes independent of record ordering.
func (self *productionBootstrapUnsealedBoundaryCensus) sorted() []ProductionBootstrapUnsealedBoundary {
	members := make([]ProductionBootstrapUnsealedBoundary, 0, len(self.byBlock))
	for _, member := range self.byBlock {
		members = append(members, member)
	}
	slices.SortFunc(members, func(a, b ProductionBootstrapUnsealedBoundary) int {
		if a.Boundary.EVMBlock < b.Boundary.EVMBlock {
			return -1
		}
		if a.Boundary.EVMBlock > b.Boundary.EVMBlock {
			return 1
		}
		return 0
	})
	return members
}

// Detached replay retains its complete projection beside the derived census.
// A caller cannot substitute another head while reusing these historical reads.
type productionBootstrapUnsealedTail struct {
	prefix     ProductionBootstrapOperatorPrefix
	ledger     ProductionBootstrapUnsealedLedger
	boundaries []ProductionBootstrapUnsealedBoundary
}

// Borrows the live source owners through every real Rpc. The returned copy is
// all-or-nothing across both operators, including empty tails. Local custody
// failures join transport errors before the enclosing retry owner classifies
// them, so a timeout never hides a source change or refreshes protocol clocks.
func (self *productionBootstrapUnsealedOwner) authenticateTailBoundaries(ctx context.Context, archive *ReleaseEvidenceV2Archive, inventory *ProductionBootstrapUnsealedObservation, chain *ChainClient) (result *ProductionBootstrapUnsealedObservation, resultErr error) {
	if ctx == nil || self == nil || archive == nil || archive.closed || archive.owner == nil || archive.history == nil || inventory == nil || len(self.tails) != 2 || len(inventory.Ledgers) != 2 || chain == nil {
		return nil, errors.New("unsealed tail boundary authentication lacks its replay owner")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err(), self.check(), archive.owner.check(ctx))
		if resultErr != nil {
			result = nil
		}
	}()
	if err := errors.Join(ctx.Err(), self.check(), archive.owner.check(ctx)); err != nil {
		return nil, err
	}
	if self.inventoryHash == "" || self.inventoryHash != productionBootstrapPrefixHash(*inventory) {
		return nil, errors.New("unsealed tail boundary source census differs from its actual capture")
	}
	observed := *inventory
	observed.Ledgers = slices.Clone(inventory.Ledgers)
	prefixes := make([]ProductionBootstrapOperatorPrefix, 0, len(self.tails))
	for i, tail := range self.tails {
		if inventory.Ledgers[i] != tail.ledger {
			return nil, errors.New("unsealed tail boundary inventory differs from its actual replay")
		}
		initial, found := archive.history.initial[tail.ledger.NoId]
		if !found || initial.InitialCut.Identity.NoID != tail.prefix.NoId {
			return nil, errors.New("unsealed tail boundary changed its original operator domain")
		}
		proof := &ProductionBootstrapUnsealedBoundaryProof{Schema: ProductionBootstrapUnsealedBoundarySchema, HistoricalSources: true, Boundaries: slices.Clone(tail.boundaries)}
		if err := proof.validate(tail.prefix, tail.ledger.UnsealedRecords); err != nil {
			return nil, err
		}
		for _, member := range proof.Boundaries {
			if err := chain.authenticateReleaseStartupBoundaryV2WithPolicy(ctx, initial.InitialCut.Activation.Domain, tail.ledger.NoId, member.Boundary, false, false, &archive.owner.cfg, ""); err != nil {
				return nil, fmt.Errorf("unsealed operator %d tail boundary %d: %w", tail.ledger.NoId, member.Boundary.EVMBlock, err)
			}
		}
		observed.Ledgers[i].TailBoundaryProof = proof
		prefixes = append(prefixes, tail.prefix)
	}
	return &observed, observed.Validate(prefixes)
}
