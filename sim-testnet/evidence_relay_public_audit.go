//go:build linux || darwin

// Provisional campaigns authenticate local ownership before starting, while a
// separate read-only worker verifies the fixed public census. Its private
// horizon cannot authorize transactions, and completion remains a final gate.
package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"

	validatorcomponent "github.com/urfoundation/sn/validator"
)

// Locators are parsed and bound to their immutable file witnesses before the
// interval starts. Appending later publications does not invalidate this work.
type evidenceRelayPublicCensusSource struct {
	closed []validatorcomponent.ValidatorEvidencePublicationV2Manifest
	audits []validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest
}

// Only one verification worker owns these snapshots. The shared chain client
// permits concurrent reads and is closed only after this worker has joined.
// The executor is consulted for immutable plan/configuration fields only.
type evidenceRelayPublicCensus struct {
	reader           *evidenceRelayRuntime
	horizon          *evidenceRelayHorizon
	sources          []evidenceRelayPublicCensusSource
	witnesses        []evidenceRelayStartupFileWitness
	block            uint64
	hash             [32]byte
	retainedVerified bool
}

// Authenticate bounded local locators, retained liabilities and original
// approvals synchronously. Only exact provisional continuations defer public
// payload authentication; strict admission retains its complete inline audit.
func (self *evidenceRelayRuntime) preparePublicCensus(horizon *evidenceRelayHorizon, block uint64, hash [32]byte, inventories map[uint64]evidenceRelayStartupSourceInventory) error {
	if horizon == nil {
		return errors.New("relay public census has no authenticated horizon")
	}
	deferred := horizon.continuation != nil && horizon.forecastAdvisory && !self.executor.cfg.readOnlyAudit && self.retainedPublications == nil
	reader := &evidenceRelayRuntime{executor: self.executor, chain: self.chain, origins: self.origins, work: self.work, phase: self.phase,
		retainedPublications: self.retainedPublications, sources: slices.Clone(self.sources)}
	for index := range reader.sources {
		reader.sources[index].activations = slices.Clone(self.sources[index].activations)
	}
	census := &evidenceRelayPublicCensus{reader: reader, horizon: horizon, block: block, hash: hash,
		retainedVerified: self.startupCache != nil && self.startupCache.hit}
	for index := range self.sources {
		if provisionalResumeEnabled(self.executor.cfg) && horizon.continuation == nil {
			fmt.Fprintln(os.Stderr, "sim-testnet: provisional evidence relay pending_public_census_preview_waived=true; actual publication authentication and slot admission remain required; final_acceptance=false")
			break
		}
		source := &self.sources[index]
		var closed []validatorcomponent.ValidatorEvidencePublicationV2Manifest
		var audits []validatorcomponent.ValidatorEvidenceDepositAuditV2Manifest
		var err error
		if inventory, found := inventories[source.validatorId]; found {
			closed, audits, err = self.readEvidenceRelayStartupManifests(self.ctx, source, inventory)
		} else {
			closed, err = discoverEvidenceRelayClosedGenerations(self.ctx, source)
			if err == nil {
				audits, err = discoverEvidenceRelayAuditGenerations(self.ctx, source)
			}
		}
		if err != nil {
			return err
		}
		if err := self.startupCache.observeSource(source, closed, audits); err != nil {
			return err
		}
		census.sources = append(census.sources, evidenceRelayPublicCensusSource{closed: closed, audits: audits})
		if deferred {
			inventory := inventories[source.validatorId]
			census.witnesses = append(census.witnesses, inventory.closed...)
			census.witnesses = append(census.witnesses, inventory.audits...)
		}
	}
	if self.startupCache != nil && self.startupCache.hit && !self.startupCache.revalidate(self) {
		return errors.New("relay startup cached prefix changed during admission")
	}
	if !deferred {
		return census.verify(self.ctx)
	}
	if self.startupCache != nil && self.startupCache.hit {
		for _, action := range self.startupCache.actionKVs {
			census.witnesses = append(census.witnesses, action.Request, action.Receipt)
		}
	}
	// Approved signed subjects reserve their original slots before live work.
	// They do not claim current replica availability or transaction completion.
	for _, request := range horizon.continuation.Retained {
		if err := horizon.admit(request.Evidence.Header, block); err != nil {
			return err
		}
	}
	privateHorizon := *horizon
	privateHorizon.sourceKVs = maps.Clone(horizon.sourceKVs)
	privateHorizon.headerKVs = maps.Clone(horizon.headerKVs)
	census.horizon = &privateHorizon
	self.pendingPublicCensus = census
	fmt.Fprintf(os.Stderr, "sim-testnet: provisional evidence relay pending_public_census=true parallel_read_only_audit=true sources=%d final_audit_required=true final_acceptance=false\n", len(census.sources))
	return self.ctx.Err()
}

// Verify the fixed startup inventory using the same complete two-origin
// reader and durable per-publication cache as the former inline path. Every
// actual relay keeps its separate uncached authentication immediately before
// admission and send. No journal, transaction or source cursor is changed here.
func (self *evidenceRelayPublicCensus) verify(ctx context.Context) error {
	if self == nil || self.reader == nil || ctx == nil {
		return errors.New("relay public census owner is absent")
	}
	coldCensus, err := self.reader.newEvidenceRelayColdCensusSession(ctx)
	if err != nil {
		return err
	}
	var observed []validatorcomponent.ValidatorEvidenceTransactionV2Expected
	for index, inventory := range self.sources {
		source := &self.reader.sources[index]
		census := coldCensus.beginSource(source, uint64(len(inventory.closed)+len(inventory.audits)))
		for index := range inventory.closed {
			requests, err := self.reader.readClosedPublication(ctx, source, &inventory.closed[index], self.block, self.hash, census)
			if err != nil {
				return err
			}
			for _, request := range requests {
				observed = append(observed, request)
				if err := self.horizon.admit(request.Evidence.Header, self.block); err != nil {
					return err
				}
			}
		}
		for index := range inventory.audits {
			requests, err := self.reader.readAuditPublication(ctx, source, &inventory.audits[index], self.block, self.hash, census)
			if err != nil {
				return err
			}
			for _, request := range requests {
				observed = append(observed, request)
				if err := self.horizon.admit(request.Evidence.Header, self.block); err != nil {
					return err
				}
			}
		}
	}
	if self.horizon.continuation != nil && !self.retainedVerified {
		if err := validateEvidenceRelayContinuationRetained(self.horizon.continuation, observed); err != nil {
			return err
		}
	}
	for _, witness := range self.witnesses {
		if !self.reader.matchEvidenceRelayStartupWitness(witness) {
			return errors.New("relay public census locator changed during audit")
		}
	}
	return errors.Join(coldCensus.revalidate(), ctx.Err())
}

// Construction starts its single read-only worker. Outcome is immutable after
// done closes, and all methods are safe for concurrent use. Source failures
// are retained for the final gate rather than canceling provisional runtime.
type evidenceRelayPublicAudit struct {
	cancel context.CancelFunc
	done   chan struct{}
	result error
	census *evidenceRelayPublicCensus
	ctx    context.Context
}

// The campaign context owns cancellation, but audit failure owns no runtime
// cancellation authority. Close joins the worker before shared readers close.
func newEvidenceRelayPublicAudit(ctx context.Context, census *evidenceRelayPublicCensus) *evidenceRelayPublicAudit {
	auditCtx, cancel := context.WithCancel(ctx)
	self := &evidenceRelayPublicAudit{ctx: auditCtx, cancel: cancel, done: make(chan struct{}), census: census}
	go self.run()
	return self
}

// A terminal outcome never turns an incomplete or failed public audit into a
// pass, and the durable checkpoints remain available to later recovery.
func (self *evidenceRelayPublicAudit) run() {
	if self.census == nil || self.census.reader == nil {
		self.result = errors.New("relay public census owner is absent")
	} else {
		self.result = self.census.reader.retryStep(self.ctx, "public-census", func() error { return self.census.verify(self.ctx) })
	}
	if self.result != nil {
		fmt.Fprintf(os.Stderr, "sim-testnet: provisional evidence relay public census audit incomplete; final_audit_required=true final_acceptance=false: %v\n", self.result)
	} else {
		fmt.Fprintln(os.Stderr, "sim-testnet: provisional evidence relay pending_public_census=false public_census_audit_passed=true final_acceptance=false")
	}
	close(self.done)
}

// Waiters retain their own deadline; an expired observation does not cancel
// or restart the read-only worker.
func (self *evidenceRelayPublicAudit) Wait(ctx context.Context) error {
	if self == nil || ctx == nil {
		return errors.New("relay public census audit is absent")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-self.done:
		return errors.Join(self.result, ctx.Err())
	}
}

// Owned cancellation releases in-flight Http reads while preserving every
// independent validation or I/O failure alongside shutdown.
func (self *evidenceRelayPublicAudit) Close() error {
	if self == nil {
		return nil
	}
	self.cancel()
	<-self.done
	return evidenceRelayNonCancellationError(self.result)
}

// The final scenario gate must observe the actual parallel audit result.
// A staged-but-unstarted audit cannot be mistaken for an omitted obligation.
func (self *evidenceRelayRuntime) WaitPublicAudit(ctx context.Context) error {
	if self == nil || ctx == nil {
		return errors.New("relay public census completion owner is absent")
	}
	if self.pendingPublicCensus == nil {
		return ctx.Err()
	}
	return self.publicAudit.Wait(ctx)
}

// A queued phase transition takes precedence at every publication boundary,
// including a long retained audit pass. Only the relay worker calls this,
// keeping horizon changes and transaction admission under one owner.
func (self *evidenceRelayRuntime) serviceRemainingRequests() error {
	if err := self.ctx.Err(); err != nil {
		return err
	}
	select {
	case request := <-self.remainingRequests:
		return self.completeRemainingRequest(request)
	default:
		return nil
	}
}

// Release preparation only installs owner-controlled bindings; it does not
// consume relay receipts. Let its explicit clock check run before the first
// retained payload read. Production preparation may await deposit audits and
// retains cooperative admission between individual relay publications.
func (self *evidenceRelayRuntime) awaitInitialReleasePreparation() error {
	if self.pendingPublicCensus == nil || self.phase != "release-1.0" {
		return nil
	}
	for {
		select {
		case <-self.ctx.Done():
			return self.ctx.Err()
		case request := <-self.remainingRequests:
			if err := self.completeRemainingRequest(request); err != nil {
				return err
			}
			if request.ctx.Err() == nil && self.prepared {
				return nil
			}
		}
	}
}
