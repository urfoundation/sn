//go:build linux || darwin

package validator

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"math/big"
	"reflect"
	"slices"
	"time"

	"github.com/urfoundation/sn/crv4"
)

// This private owner exists only inside one submitOnceV2 call. Its independent
// authority and authenticated reader closures are detached before the first
// replay and never exposed to later callers. It cannot be used by intent or
// lineage verification, a retry, or another process.
type releaseSubmissionReplayV2 struct {
	ctx       context.Context
	options   ReleaseMeasurementV2Options
	authority ReleaseMeasurementV2Options
	bytes     []byte
	hash      string
	verified  VerifiedReleaseMeasurementV2
}

func (self *releaseRuntimeV2) sealSubmissionArtifactV2(ctx context.Context, artifact *ReleaseMeasurementArtifact, options ReleaseMeasurementV2Options) (encoded []byte, hash string, reuse *releaseSubmissionReplayV2, resultErr error) {
	if self == nil || self.history == nil || !self.history.retainedStartup {
		encoded, hash, resultErr = SealReleaseMeasurementArtifactV2(ctx, artifact, options)
		return
	}
	defer func() {
		if ctx != nil {
			resultErr = errors.Join(resultErr, ctx.Err())
		}
		if resultErr != nil {
			encoded, hash, reuse = nil, "", nil
		}
	}()
	owned, authority, err := ownReleaseMeasurementV2(ctx, artifact, options)
	if err != nil {
		return nil, "", nil, err
	}
	// Retain a separate value-owned authority pin, including every key and
	// bound. Reader functions stay private to this submission rather than being
	// compared by code address (which cannot identify a Go closure's receiver).
	_, replayAuthority, err := ownReleaseMeasurementV2(ctx, owned, authority)
	if err != nil {
		return nil, "", nil, err
	}
	// The result is retained only after every ordinary/terminal stream and its
	// resources completed successfully. No lineage checkpoint is waived.
	verified, err := verifyOwnedReleaseMeasurementV2(ctx, owned, replayAuthority, nil, nil)
	if err != nil {
		return nil, "", nil, err
	}
	encoded, err = canonicalReleaseMeasurementBytes(owned)
	if err != nil {
		return nil, "", nil, err
	}
	hash = ReleaseMeasurementContentHash(encoded)
	reuse = &releaseSubmissionReplayV2{ctx: ctx, options: replayAuthority, authority: releaseSubmissionAuthorityV2(authority), bytes: bytes.Clone(encoded), hash: hash, verified: verified}
	return encoded, hash, reuse, nil
}

func (self *releaseSubmissionReplayV2) close() {
	if self != nil {
		*self = releaseSubmissionReplayV2{}
	}
}

// Every reuse retains canonical decoding and complete structural/authority
// admission. The full canonical bytes include all signed cuts, stream hashes,
// statistics, head inputs and chain/HTTP observations. The independent options
// (including keys, bounds and reader owners) remain the same private snapshot.
func (self *releaseSubmissionReplayV2) verify(ctx context.Context, measurement []byte) (result VerifiedReleaseMeasurementV2, resultErr error) {
	if self == nil || ctx == nil || self.ctx != ctx || self.verified.Decision == nil {
		return result, errors.New("provisional submission replay owner is absent or differs")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = VerifiedReleaseMeasurementV2{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	artifact, err := decodeReleaseMeasurementV2Bytes(ctx, measurement, self.options.MaxArtifactBytes, self.options.MaxOperators)
	if err != nil {
		return result, err
	}
	_, authority, err := ownReleaseMeasurementV2(ctx, artifact, self.options)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(releaseSubmissionAuthorityV2(authority), self.authority) {
		return result, errors.New("provisional submission replay independent authority differs")
	}
	if ReleaseMeasurementContentHash(measurement) != self.hash || !bytes.Equal(measurement, self.bytes) {
		return result, errors.New("provisional submission replay measurement differs from completed replay")
	}
	return cloneReleaseSubmissionResultV2(self.verified), nil
}

func releaseSubmissionAuthorityV2(options ReleaseMeasurementV2Options) ReleaseMeasurementV2Options {
	options.Operators = maps.Clone(options.Operators)
	for id, operator := range options.Operators {
		operator.Measurement.Replay.ReadMetadata = nil
		operator.Measurement.Replay.OpenData = nil
		options.Operators[id] = operator
	}
	if options.Settlement != nil {
		settlement := *options.Settlement
		settlement.Operators = maps.Clone(settlement.Operators)
		for id, operator := range settlement.Operators {
			operator.Measurement.Replay.ReadMetadata = nil
			operator.Measurement.Replay.OpenData = nil
			settlement.Operators[id] = operator
		}
		options.Settlement = &settlement
	}
	return options
}

func (self *releaseSubmissionReplayV2) sealEnvelope(ctx context.Context, measurement []byte, uid uint16, hotkey *crv4.Keypair, preparedHash string, signedAt time.Time) ([]byte, string, *ReleaseMeasurementEnvelope, error) {
	if self == nil || self.ctx != ctx {
		return nil, "", nil, errors.New("provisional submission envelope owner differs")
	}
	return sealReleaseMeasurementEnvelopeV2(ctx, measurement, uid, hotkey, preparedHash, signedAt, self.options, self)
}

func cloneReleaseSubmissionResultV2(value VerifiedReleaseMeasurementV2) VerifiedReleaseMeasurementV2 {
	cloneRat := func(value *big.Rat) *big.Rat {
		if value == nil {
			return nil
		}
		return new(big.Rat).Set(value)
	}
	cloneWeights := func(values []ExactWeightInput) []ExactWeightInput {
		values = slices.Clone(values)
		for i := range values {
			values[i].Score = cloneRat(values[i].Score)
		}
		return values
	}
	decision := *value.Decision
	decision.EligibleHead = cloneWeights(decision.EligibleHead)
	decision.SelectedHead = cloneWeights(decision.SelectedHead)
	decision.RejectedHead = cloneWeights(decision.RejectedHead)
	decision.StaleBindings = slices.Clone(decision.StaleBindings)
	decision.Pools = slices.Clone(decision.Pools)
	for i := range decision.Pools {
		decision.Pools[i].Score = cloneRat(decision.Pools[i].Score)
	}
	decision.MaskedUIDs = slices.Clone(decision.MaskedUIDs)
	decision.UIDs = slices.Clone(decision.UIDs)
	decision.Scores = slices.Clone(decision.Scores)
	for i := range decision.Scores {
		decision.Scores[i] = cloneRat(decision.Scores[i])
	}
	decision.BoundProviders = maps.Clone(decision.BoundProviders)
	for noID, providers := range decision.BoundProviders {
		decision.BoundProviders[noID] = maps.Clone(providers)
	}
	decision.StatsByNO = maps.Clone(decision.StatsByNO)
	for noID, stats := range decision.StatsByNO {
		stats.Providers = maps.Clone(stats.Providers)
		for id, provider := range stats.Providers {
			provider.EgressIPHashes = maps.Clone(provider.EgressIPHashes)
			stats.Providers[id] = provider
		}
		decision.StatsByNO[noID] = stats
	}
	return VerifiedReleaseMeasurementV2{Decision: &decision, ReplayByNO: maps.Clone(value.ReplayByNO), SettlementReplayByNO: maps.Clone(value.SettlementReplayByNO)}
}
