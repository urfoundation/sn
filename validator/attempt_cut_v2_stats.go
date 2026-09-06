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

// Reconstructs scores and proves their complete raw inputs using the actual
// policy-aware record/proof replay. The measurement must contain raw statistics
// only: legacy cuts/transitions cannot be silently discarded as v2 authority.
// Prior EMA lineage, historical activation/eligibility and cross-cut terminal-ID
// continuity remain outer checks; this does not activate a production writer.
func VerifyReleaseStatsMeasurementWithAttemptCutV2(ctx context.Context, measurement ReleaseStatsMeasurement, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2StatsOptions) (result VerifiedReleaseStats, replayed AttemptCutV2ReplayResult, resultErr error) {
	return verifyReleaseStatsMeasurementWithAttemptCutV2AndObserver(ctx, measurement, cut, expected, policy, bounds, options, nil)
}

// The operation-local observer marks the real generic verification boundary;
// production callers supply nil and retain the exact public replay path.
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
	if measurement.AttemptCut != nil || measurement.SettlementTransition != nil || options.Replay.VisitRecord != nil {
		return result, replayed, errors.New("compact attempt statistics contain competing replay authority")
	}
	if options.MaxProviders == 0 || options.MaxEgressHashes == 0 || uint64(len(measurement.Providers)) > options.MaxProviders {
		return result, replayed, errors.New("compact attempt statistics provider census exceeds its bound")
	}
	if measurement.Config != options.ExpectedConfig || options.ExpectedConfig.AMin != policy.Verify.ReliabilityAMin {
		return result, replayed, errors.New("compact attempt statistics differ from the expected scoring configuration")
	}
	var egressCount uint64
	for _, provider := range measurement.Providers {
		if err := ctx.Err(); err != nil {
			return result, replayed, err
		}
		if count := uint64(len(provider.EgressIPHashHexes)); count > options.MaxEgressHashes-egressCount {
			return result, replayed, errors.New("compact attempt statistics egress census exceeds its bound")
		} else {
			egressCount += count
		}
		// Generic UUID errors and case normalization can copy their input.
		// Canonical fixed widths bound that work before entering the verifier.
		if len(provider.ClientID) != 36 || len(provider.LatencyBuckets) != statsLatencyBuckets {
			return result, replayed, errors.New("compact attempt statistics provider shape is not canonical")
		}
		for _, encoded := range provider.EgressIPHashHexes {
			if err := ctx.Err(); err != nil {
				return result, replayed, err
			}
			if len(encoded) != 66 {
				return result, replayed, errors.New("compact attempt statistics egress hash width is not canonical")
			}
		}
	}
	if observeStatsVerifier != nil {
		observeStatsVerifier()
	}
	verified, err := VerifyReleaseStatsMeasurement(measurement)
	if err != nil {
		return result, replayed, err
	}
	remainingKVs := make(map[connect.Id]*attemptCutV2ProviderRemainder, len(measurement.Providers))
	for _, provider := range measurement.Providers {
		// The real statistics verifier has already checked canonical identities,
		// all histogram sizes and the complete ordered nonzero hash census.
		clientID, err := connect.ParseId(provider.ClientID)
		if err != nil {
			return result, replayed, err
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
	replayOptions := options.Replay
	replayOptions.VisitRecord = func(record AttemptRecord) error {
		if record.Disposition == AttemptDispositionPending {
			return nil
		}
		for _, assignment := range record.Assignments {
			remaining := remainingKVs[assignment.NextHop]
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
		if record.Sequence < expected.EgressFirstSequence || record.Disposition != AttemptDispositionComplete || record.Proof == nil {
			return nil
		}
		for index := 1; index < len(record.Proof.Hops); index++ {
			hop := record.Proof.Hops[index]
			if hop.EgressIpHash == ([32]byte{}) {
				continue
			}
			remaining := remainingKVs[hop.ClientId]
			if remaining == nil || !verified.Providers[hop.ClientId].EgressIPHashes[hop.EgressIpHash] {
				return fmt.Errorf("provider %s egress hash is absent from compact attempt statistics", hop.ClientId)
			}
			delete(remaining.egress, hop.EgressIpHash)
		}
		return nil
	}
	replayed, err = ReplayAttemptCutV2WithPolicy(ctx, cut, expected, policy, bounds, replayOptions)
	if err != nil {
		return result, replayed, err
	}
	for clientID, remaining := range remainingKVs {
		if remaining.assignments != 0 || remaining.confirmations != 0 || len(remaining.egress) != 0 {
			return result, replayed, fmt.Errorf("provider %s raw statistics are absent from compact attempts", clientID)
		}
		for _, count := range remaining.latencyBuckets {
			if count != 0 {
				return result, replayed, fmt.Errorf("provider %s latency statistics are absent from compact attempts", clientID)
			}
		}
	}
	return verified, replayed, nil
}
