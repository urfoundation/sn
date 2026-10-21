//go:build linux || darwin

// Batch prefetch owns actual bounded reads for already signed source boundaries.
// It supplies no client-key verdict; each retained response is replayed again.
package validator

import (
	"context"
	"errors"
	"reflect"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

// Canonical decoding and every source signature precede work proportional to
// proposed historical boundaries. The enclosing caller has charged this body.
func releaseClientKeyBatchV2Queries(ctx context.Context, source releaseClientKeyCapturedV2Response, retained bool) ([]stabi.ClientKeyAuthorityQuery, error) {
	response, err := protocol.DecodeClientKeyHistoryResponse(source.encoded, source.maximum)
	if err != nil {
		return nil, err
	}
	wrapper, err := protocol.DecodeClientKeyEvidence(response.Observation, source.domain, protocol.ClientKeyObservationEvidenceKind)
	if err != nil {
		return nil, err
	}
	observation, err := protocol.DecodeClientKeyObservation(wrapper.Payload)
	if err != nil {
		return nil, err
	}
	request := source.request
	if retained {
		request.Nonce = observation.Request.Nonce
	}
	if err := request.Validate(); err != nil || observation.Request != request || observation.ClientID != request.ClientID || observation.Domain != source.domain || observation.Generation != uint64(len(response.History)) {
		return nil, errors.Join(errors.New("client-key batch source differs from its independently owned request"), err)
	}
	queries := []stabi.ClientKeyAuthorityQuery{{Domain: source.domain, Boundary: request.DecisionBoundary}}
	queryKVs := map[stabi.ClientKeyAuthorityQuery]bool{queries[0]: true}
	var prior *protocol.ClientKeyRegistration
	for _, encoded := range response.History {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		envelope, err := protocol.DecodeClientKeyEvidence(encoded, source.domain, protocol.ClientKeyRegistrationEvidenceKind)
		if err != nil {
			return nil, err
		}
		registration, err := protocol.DecodeClientKeyRegistration(envelope.Payload)
		if err != nil || registration.Domain != source.domain || registration.ClientID != request.ClientID {
			return nil, errors.Join(errors.New("client-key batch historical source identity differs"), err)
		}
		if err := registration.Follows(prior); err != nil {
			return nil, err
		}
		boundary := registration.EffectiveBoundary
		if boundary.Block > request.DecisionBoundary.Block || boundary.Epoch > request.DecisionBoundary.Epoch || boundary.Block == request.DecisionBoundary.Block && boundary != request.DecisionBoundary {
			return nil, errors.New("client-key batch historical source follows its decision")
		}
		query := stabi.ClientKeyAuthorityQuery{Domain: source.domain, Boundary: boundary}
		if !queryKVs[query] {
			queryKVs[query] = true
			queries = append(queries, query)
		}
		owned := registration
		prior = &owned
	}
	return queries, ctx.Err()
}

// One synchronous prefetch is exclusive with other leader reads. Existing
// completed values may be reused, but a failed new census is discarded whole.
func (self *releaseClientKeyAuthorityV2Reads) prefetch(ctx context.Context, chain *ChainClient, queries []stabi.ClientKeyAuthorityQuery) (resultErr error) {
	if ctx == nil || self == nil {
		return errors.New("client-key prefetch owner is absent")
	}
	if len(queries) == 0 {
		return ctx.Err()
	}
	decision := self.identity.request.DecisionBoundary
	for _, query := range queries {
		if err := errors.Join(self.validate(ctx, chain, query.Domain), query.Boundary.Validate()); err != nil {
			return err
		}
		boundary := query.Boundary
		if boundary.Block < self.identity.deployBlock || boundary.Block > decision.Block || boundary.Epoch > decision.Epoch || boundary.Block == decision.Block && boundary != decision {
			return errors.New("client-key batch boundary escaped its independently owned decision")
		}
	}
	var pending []stabi.ClientKeyAuthorityQuery
	var entries []*releaseClientKeyAuthorityV2Entry
	err := func() error {
		self.stateLock.Lock()
		defer self.stateLock.Unlock()
		if self.closed || self.inflight != 0 {
			return errors.New("client-key authority prefetch owner is closed or busy")
		}
		for _, query := range queries {
			key := releaseClientKeyAuthorityV2Key{identity: self.identity, domain: query.Domain, boundary: query.Boundary}
			if self.authorityKVs[key] != nil {
				continue
			}
			width := 2*uint64(reflect.TypeFor[releaseClientKeyAuthorityV2Key]().Size()) + uint64(reflect.TypeFor[releaseClientKeyAuthorityV2Entry]().Size()) + uint64(reflect.TypeFor[protocol.ClientKeyEffectiveBoundary]().Size()) + 256
			if err := self.budget.charge(1, width); err != nil {
				for index, pendingQuery := range pending {
					delete(self.authorityKVs, releaseClientKeyAuthorityV2Key{identity: self.identity, domain: pendingQuery.Domain, boundary: pendingQuery.Boundary})
					entries[index].err = err
					close(entries[index].done)
				}
				pending, entries = nil, nil
				return err
			}
			entry := &releaseClientKeyAuthorityV2Entry{done: make(chan struct{})}
			self.authorityKVs[key] = entry
			pending = append(pending, query)
			entries = append(entries, entry)
		}
		self.pending.Add(1)
		self.inflight++
		self.batched = true
		return nil
	}()
	if err != nil {
		// Admission can fail after creating entries; none may survive as a
		// never-completed authority or strand a concurrent waiter.
		self.stateLock.Lock()
		for index, query := range pending {
			delete(self.authorityKVs, releaseClientKeyAuthorityV2Key{identity: self.identity, domain: query.Domain, boundary: query.Boundary})
			entries[index].err = err
			close(entries[index].done)
		}
		self.stateLock.Unlock()
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err(), self.ctx.Err())
		self.stateLock.Lock()
		if self.closed {
			resultErr = errors.Join(resultErr, errors.New("client-key prefetch closed during its real reads"))
		}
		for index, query := range pending {
			entry := entries[index]
			if resultErr != nil {
				entry.signer = common.Address{}
				entry.err = resultErr
				delete(self.authorityKVs, releaseClientKeyAuthorityV2Key{identity: self.identity, domain: query.Domain, boundary: query.Boundary})
			}
			close(entry.done)
		}
		self.inflight--
		self.stateLock.Unlock()
		self.pending.Done()
	}()
	readCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(self.ctx, cancel)
	defer func() { stop(); cancel() }()
	for start := 0; start < len(pending); {
		var remaining uint64
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			remaining = self.budget.limit - self.budget.used
		}()
		limits := stabi.ClientKeyAuthorityRpcLimits{MaximumRequests: protocol.MaxClientKeyObservationBatchRpcRequests, MaximumMethods: protocol.MaxClientKeyObservationBatchRpcMethods, MaximumBytes: remaining}
		end := len(pending)
		if _, err := stabi.PlanClientKeyAuthorityRpc(pending[start:end], limits); errors.Is(err, stabi.ErrClientKeyAuthorityRpcWork) {
			end = min(start+protocol.ClientKeyObservationFallbackBatchClients, len(pending))
		} else if err != nil {
			return err
		}
		observed, work, err := stabi.ReadClientKeyAuthoritiesContext(readCtx, self.ownedChain.client, pending[start:end], limits)
		chargeErr := func() error {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			return self.budget.charge(work.Bytes, 1)
		}()
		if err := errors.Join(err, chargeErr); err != nil {
			return err
		}
		for index, value := range observed {
			entries[start+index].signer = value.Operator.RootSigner
		}
		start = end
	}
	return readCtx.Err()
}
