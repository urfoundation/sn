//go:build linux || darwin

package validator

// Streaming statistics replay joins the published scores to every authenticated
// terminal attempt. Memory follows the bounded declared provider/hash census,
// never the number of historical attempts or candidate-selected new identities.

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

// The caller supplies authenticated scoring configuration and independent
// finite census limits. Replay retains its existing row, disk and stream bounds.
// This verifier owns VisitRecord; it cannot publish a partial scoring result.
type AttemptCutV2StatsOptions struct {
	ExpectedConfig  ReleaseStatsConfig
	MaxProviders    uint64
	MaxEgressHashes uint64
	Replay          AttemptCutV2ReplayOptions
}

// Counters and remaining hashes belong to one synchronous replay. Repeated
// egress observations remove the same expected hash idempotently.
type attemptCutV2ProviderRemainder struct {
	assignments    uint64
	confirmations  uint64
	latencyBuckets [statsLatencyBuckets]uint64
	egress         map[[32]byte]bool
}

// Raw statistics are admitted and copied before any public object is read.
// One replay owns these remaining counters and its eventual scored result.
type attemptCutV2StatsProjection struct {
	egressFirstSequence uint64
	verified            VerifiedReleaseStats
	remainingKVs        map[connect.Id]*attemptCutV2ProviderRemainder
}

// Reconstructs scores and proves their complete raw inputs using the actual
// policy-aware record/proof replay. The measurement must contain raw statistics
// only: legacy cuts/transitions cannot be silently discarded as v2 authority.
// Prior EMA lineage, historical activation/eligibility and cross-cut terminal-ID
// continuity remain outer checks; this does not activate a production writer.
func VerifyReleaseStatsMeasurementWithAttemptCutV2(ctx context.Context, measurement ReleaseStatsMeasurement, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2StatsOptions) (result VerifiedReleaseStats, replayed AttemptCutV2ReplayResult, resultErr error) {
	return verifyReleaseStatsMeasurementWithAttemptCutV2AndObserver(ctx, measurement, cut, expected, policy, bounds, options, nil)
}

// The operation-local observer reaches the actual generic admission boundary.
// Full policy replay remains in this workflow; there is no cached verdict.
func verifyReleaseStatsMeasurementWithAttemptCutV2AndObserver(ctx context.Context, measurement ReleaseStatsMeasurement, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2StatsOptions, observeStatsVerifier func()) (result VerifiedReleaseStats, replayed AttemptCutV2ReplayResult, resultErr error) {
	if ctx == nil {
		return result, replayed, errors.New("compact attempt statistics context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result, replayed = VerifiedReleaseStats{}, AttemptCutV2ReplayResult{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, replayed, err
	}
	projection, err := newAttemptCutV2StatsProjection(ctx, measurement, expected, policy, options, observeStatsVerifier)
	if err != nil {
		return result, replayed, err
	}
	replayOptions := options.Replay
	replayOptions.VisitRecord = projection.visitRecord
	replayed, err = ReplayAttemptCutV2WithPolicy(ctx, cut, expected, policy, bounds, replayOptions)
	if err != nil {
		return result, replayed, err
	}
	if err := projection.verifyComplete(ctx); err != nil {
		return result, replayed, err
	}
	return projection.verified, replayed, nil
}

// Both standalone and joint replay use this one admission and scoring path.
// Fixed widths precede generic UUID errors/case normalization, which can copy
// their input; finite collection counts alone do not bound those allocations.
func newAttemptCutV2StatsProjection(ctx context.Context, measurement ReleaseStatsMeasurement, expected AttemptCutV2Context, policy protocol.Policy, options AttemptCutV2StatsOptions, observeStatsVerifier func()) (*attemptCutV2StatsProjection, error) {
	if measurement.AttemptCut != nil || measurement.SettlementTransition != nil || options.Replay.VisitRecord != nil {
		return nil, errors.New("compact attempt statistics contain competing replay authority")
	}
	if options.MaxProviders == 0 || options.MaxEgressHashes == 0 || uint64(len(measurement.Providers)) > options.MaxProviders {
		return nil, errors.New("compact attempt statistics provider census exceeds its bound")
	}
	if measurement.Config != options.ExpectedConfig || options.ExpectedConfig.AMin != policy.Verify.ReliabilityAMin {
		return nil, errors.New("compact attempt statistics differ from the expected scoring configuration")
	}
	var egressCount uint64
	for _, provider := range measurement.Providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if count := uint64(len(provider.EgressIPHashHexes)); count > options.MaxEgressHashes-egressCount {
			return nil, errors.New("compact attempt statistics egress census exceeds its bound")
		} else {
			egressCount += count
		}
		if len(provider.ClientID) != 36 || len(provider.LatencyBuckets) != statsLatencyBuckets {
			return nil, errors.New("compact attempt statistics provider shape is not canonical")
		}
		for _, encoded := range provider.EgressIPHashHexes {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if len(encoded) != 66 {
				return nil, errors.New("compact attempt statistics egress hash width is not canonical")
			}
		}
	}
	if observeStatsVerifier != nil {
		observeStatsVerifier()
	}
	verified, err := VerifyReleaseStatsMeasurement(measurement)
	if err != nil {
		return nil, err
	}
	remainingKVs := make(map[connect.Id]*attemptCutV2ProviderRemainder, len(measurement.Providers))
	for _, provider := range measurement.Providers {
		// The real statistics verifier has already checked canonical identities,
		// all histogram sizes and the complete ordered nonzero hash census.
		clientID, err := connect.ParseId(provider.ClientID)
		if err != nil {
			return nil, err
		}
		remaining := &attemptCutV2ProviderRemainder{
			assignments: provider.Assignments, confirmations: provider.Confirmations,
			egress: make(map[[32]byte]bool, len(provider.EgressIPHashHexes)),
		}
		copy(remaining.latencyBuckets[:], provider.LatencyBuckets)
		for hash := range verified.Providers[clientID].EgressIPHashes {
			remaining.egress[hash] = true
		}
		remainingKVs[clientID] = remaining
	}
	return &attemptCutV2StatsProjection{egressFirstSequence: expected.EgressFirstSequence, verified: verified, remainingKVs: remainingKVs}, nil
}

// Authenticated terminal observations consume exact declared counters/hashes.
// Repeated prefixes are idempotent; unlisted providers can never gain a score.
func (self *attemptCutV2StatsProjection) visitRecord(record AttemptRecord) error {
	if record.Disposition == AttemptDispositionPending {
		return nil
	}
	for _, assignment := range record.Assignments {
		remaining := self.remainingKVs[assignment.NextHop]
		if remaining == nil || remaining.assignments == 0 {
			return fmt.Errorf("provider %s assignments are absent from compact attempt statistics", assignment.NextHop)
		}
		remaining.assignments--
		if assignment.Confirmed {
			if int(assignment.LatencyBucket) >= len(remaining.latencyBuckets) || remaining.confirmations == 0 || remaining.latencyBuckets[assignment.LatencyBucket] == 0 {
				return fmt.Errorf("provider %s confirmations or latency differ from compact attempts", assignment.NextHop)
			}
			remaining.confirmations--
			remaining.latencyBuckets[assignment.LatencyBucket]--
		}
	}
	if record.Sequence < self.egressFirstSequence || record.Disposition != AttemptDispositionComplete || record.Proof == nil {
		return nil
	}
	for index := 1; index < len(record.Proof.Hops); index++ {
		hop := record.Proof.Hops[index]
		if hop.EgressIpHash == ([32]byte{}) {
			continue
		}
		remaining := self.remainingKVs[hop.ClientId]
		if remaining == nil || !self.verified.Providers[hop.ClientId].EgressIPHashes[hop.EgressIpHash] {
			return fmt.Errorf("provider %s egress hash is absent from compact attempt statistics", hop.ClientId)
		}
		delete(remaining.egress, hop.EgressIpHash)
	}
	return nil
}

// No declared input may be left over after complete proof EOF and Close.
// Completion returns no transferable authority or reusable verification token.
func (self *attemptCutV2StatsProjection) verifyComplete(ctx context.Context) error {
	for clientID, remaining := range self.remainingKVs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if remaining.assignments != 0 || remaining.confirmations != 0 || len(remaining.egress) != 0 {
			return fmt.Errorf("provider %s raw statistics are absent from compact attempts", clientID)
		}
		for _, count := range remaining.latencyBuckets {
			if count != 0 {
				return fmt.Errorf("provider %s latency statistics are absent from compact attempts", clientID)
			}
		}
	}
	return nil
}
