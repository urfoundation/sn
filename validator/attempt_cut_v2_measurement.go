//go:build linux || darwin

package validator

// One fully authenticated stream traversal reconstructs both operator quality
// and generation-attributed miner head evidence. Neither result can outlive a
// failed/canceled replay or escape before the other projection has completed.

import (
	"context"
	"errors"

	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

// A single caller-owned transport/scratch namespace serves both projections.
// Configuration and current bindings are independently authenticated inputs;
// this API does not derive their authority from the candidate cut or install
// production defaults. Inputs must be stable during synchronous admission.
type AttemptCutV2MeasurementOptions struct {
	ExpectedConfig    ReleaseStatsConfig
	CurrentBindingKVs map[connect.Id]FleetScoreKey
	MaxProviders      uint64
	MaxEgressHashes   uint64
	MaxFleetPrefixes  uint64
	Replay            AttemptCutV2ReplayOptions
}

// These owned maps are published together after full stream verification.
// Outer release verification still proves historical/current eligibility,
// prior EMA lineage, cross-cut continuity and complete operator membership.
type VerifiedAttemptCutV2Measurement struct {
	Stats        VerifiedReleaseStats
	HeadPrefixes map[FleetScoreKey]map[[32]byte]bool
	Replay       AttemptCutV2ReplayResult
}

// Uses the same admission and per-record primitives as standalone statistics
// and head replay. The private visitor invokes both against every authenticated
// record; no external callback, cached verdict or second history scan is used.
func VerifyReleaseStatsAndHeadWithAttemptCutV2(ctx context.Context, measurement ReleaseStatsMeasurement, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2MeasurementOptions) (VerifiedAttemptCutV2Measurement, error) {
	return verifyReleaseStatsAndHeadWithAttemptCutV2(ctx, measurement, cut, expected, policy, bounds, options, nil)
}

// Only an operation-owned release/lineage join may add this private observer.
// The public Replay.VisitRecord option remains rejected by both projections.
func verifyReleaseStatsAndHeadWithAttemptCutV2(ctx context.Context, measurement ReleaseStatsMeasurement, cut AttemptCutV2, expected AttemptCutV2Context, policy protocol.Policy, bounds AttemptCutV2Bounds, options AttemptCutV2MeasurementOptions, visitRecord func(AttemptRecord) error) (result VerifiedAttemptCutV2Measurement, resultErr error) {
	if ctx == nil {
		return result, errors.New("compact measurement context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = VerifiedAttemptCutV2Measurement{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	head, err := newAttemptCutV2HeadProjection(ctx, expected.EgressFirstSequence, AttemptCutV2HeadOptions{
		CurrentBindingKVs: options.CurrentBindingKVs, MaxProviders: options.MaxProviders,
		MaxFleetPrefixes: options.MaxFleetPrefixes, Replay: options.Replay,
	})
	if err != nil {
		return result, err
	}
	stats, err := newAttemptCutV2StatsProjection(ctx, measurement, expected, policy, AttemptCutV2StatsOptions{
		ExpectedConfig: options.ExpectedConfig, MaxProviders: options.MaxProviders,
		MaxEgressHashes: options.MaxEgressHashes, Replay: options.Replay,
	}, nil)
	if err != nil {
		return result, err
	}
	replayOptions := options.Replay
	replayOptions.VisitRecord = func(record AttemptRecord) error {
		if err := stats.visitRecord(record); err != nil {
			return err
		}
		if err := head.visitRecord(record); err != nil {
			return err
		}
		if visitRecord != nil {
			return visitRecord(record)
		}
		return nil
	}
	replayed, err := ReplayAttemptCutV2WithPolicy(ctx, cut, expected, policy, bounds, replayOptions)
	if err != nil {
		return result, err
	}
	if err := stats.verifyComplete(ctx); err != nil {
		return result, err
	}
	return VerifiedAttemptCutV2Measurement{Stats: stats.verified, HeadPrefixes: head.fleetsKVs, Replay: replayed}, nil
}
