//go:build linux || darwin

package validator

// Terminal acceptance is a complete operation, not a per-member verdict.
// Admission owns every member and authority input before external reads; one
// policy replay per cut reconstructs both projections before any result escapes.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	"github.com/urfoundation/sn/protocol"
	"github.com/urnetwork/connect"
)

// Expected contexts and policy come from existing authenticated deployment,
// operator, activation and chain views, never from the candidate's signature.
// Transports honor cancellation. Callers keep inputs stable during admission.
type AttemptSettlementV2OperatorOptions struct {
	Expected    AttemptCutV2Context
	Policy      protocol.Policy
	Bounds      AttemptCutV2Bounds
	Measurement AttemptCutV2MeasurementOptions
}

// The map is the complete configured participant census, not a discovered
// subset. All limits are explicit; metadata budgets do not waive stream caps.
type AttemptSettlementV2Options struct {
	Operators          map[uint64]AttemptSettlementV2OperatorOptions
	MaxParticipants    uint64
	MaxTransitionBytes uint64
	MaxClosureBytes    uint64
}

// No map is published after a missing member, late read/close error or cancel.
// These values are local results, not transferable authorization/cache tokens.
type VerifiedAttemptSettlementV2 struct {
	Operators map[uint64]VerifiedAttemptCutV2Measurement
}

// Raw pre-fold evidence is supplied independently of the private signing keys.
// The real cut already commits every terminal attempt and exact proof stream.
type AttemptSettlementV2Input struct {
	PreFold ReleaseStatsMeasurement
	Cut     AttemptCutV2
}

// Owned transport closures remain callable, while every mutable byte/map input
// and candidate field is detached before the first operator can read anything.
type attemptSettlementV2Operation struct {
	closure *AttemptSettlementClosureV2
	options AttemptSettlementV2Options
}

// Validate and detach the complete independent authority before candidate I/O.
func ownAttemptSettlementV2Options(ctx context.Context, options AttemptSettlementV2Options) (AttemptSettlementV2Options, error) {
	if options.MaxParticipants == 0 || options.MaxTransitionBytes == 0 || options.MaxClosureBytes == 0 || options.MaxTransitionBytes > options.MaxClosureBytes || options.MaxClosureBytes > uint64(int(^uint(0)>>1))-1 || len(options.Operators) == 0 || uint64(len(options.Operators)) > options.MaxParticipants {
		return AttemptSettlementV2Options{}, errors.New("compact settlement authority or metadata bounds are incomplete")
	}
	owned := options
	owned.Operators = make(map[uint64]AttemptSettlementV2OperatorOptions, len(options.Operators))
	scratchKVs := map[string]bool{}
	var first *AttemptSettlementV2OperatorOptions
	for noID, operator := range options.Operators {
		if err := ctx.Err(); err != nil {
			return AttemptSettlementV2Options{}, err
		}
		if noID == 0 || operator.Expected.Identity.NoID != noID || uint64(len(operator.Expected.Identity.DeploymentID)) > operator.Bounds.MaxHeaderBytes {
			return AttemptSettlementV2Options{}, errors.New("compact settlement expected operator identity differs")
		}
		if _, err := attemptCutV2PolicyDepth(operator.Expected, operator.Policy); err != nil {
			return AttemptSettlementV2Options{}, err
		}
		if operator.Expected.Boundary.SettlementEpoch == ^uint64(0) || operator.Expected.EgressGeneration == ^uint64(0) {
			return AttemptSettlementV2Options{}, errors.New("compact settlement successor epoch or generation overflows")
		}
		if err := operator.Measurement.Replay.Bounds.validate(operator.Bounds); err != nil {
			return AttemptSettlementV2Options{}, err
		}
		if _, err := newAttemptCutV2HeadProjection(ctx, operator.Expected.EgressFirstSequence, AttemptCutV2HeadOptions{
			CurrentBindingKVs: operator.Measurement.CurrentBindingKVs, MaxProviders: operator.Measurement.MaxProviders,
			MaxFleetPrefixes: operator.Measurement.MaxFleetPrefixes, Replay: operator.Measurement.Replay,
		}); err != nil {
			return AttemptSettlementV2Options{}, err
		}
		if operator.Measurement.MaxEgressHashes == 0 || operator.Measurement.ExpectedConfig.AMin != operator.Policy.Verify.ReliabilityAMin {
			return AttemptSettlementV2Options{}, errors.New("compact settlement scoring authority differs")
		}
		scratch := operator.Measurement.Replay.ScratchDirectory
		if !filepath.IsAbs(scratch) || filepath.Clean(scratch) != scratch || filepath.Dir(scratch) == scratch || scratchKVs[scratch] || operator.Measurement.Replay.ReadMetadata == nil || operator.Measurement.Replay.OpenData == nil {
			return AttemptSettlementV2Options{}, errors.New("compact settlement requires distinct owned replay namespaces and readers")
		}
		scratchKVs[scratch] = true
		operator.Policy.Deposit.Tiers = slices.Clone(operator.Policy.Deposit.Tiers)
		bindings := make(map[connect.Id]FleetScoreKey, len(operator.Measurement.CurrentBindingKVs))
		for id, binding := range operator.Measurement.CurrentBindingKVs {
			bindings[id] = binding
		}
		operator.Measurement.CurrentBindingKVs = bindings
		keys := make(map[byte]ed25519.PublicKey, len(operator.Measurement.Replay.ServerKeys))
		for id, key := range operator.Measurement.Replay.ServerKeys {
			if len(key) != ed25519.PublicKeySize {
				return AttemptSettlementV2Options{}, errors.New("compact settlement server-key history has an invalid key")
			}
			keys[id] = slices.Clone(key)
		}
		operator.Measurement.Replay.ServerKeys = keys
		if first == nil {
			copy := operator
			first = &copy
		} else if !equalAttemptSettlementIdentity(first.Expected.Identity, operator.Expected.Identity) || !equalAttemptCutV2CommonDomain(first.Expected.Activation.Domain, operator.Expected.Activation.Domain) || first.Expected.Activation.Hotkey != operator.Expected.Activation.Hotkey || first.Expected.Boundary != operator.Expected.Boundary || first.Measurement.ExpectedConfig != operator.Measurement.ExpectedConfig {
			return AttemptSettlementV2Options{}, errors.New("compact settlement authority mixes validator, activation, boundary or scoring domains")
		}
		owned.Operators[noID] = operator
	}
	return owned, nil
}

// Bound variable-width fields before cloning/hashing. Metadata memory is
// O(explicit provider/hash/member and byte limits), never historical attempts.
// Marshal's bounded temporary is additional to the owned candidate and replay.
func ownAttemptSettlementV2Transition(ctx context.Context, transition *AttemptSettlementTransitionV2, operator AttemptSettlementV2OperatorOptions, options AttemptSettlementV2Options, signed bool) (*AttemptSettlementTransitionV2, error) {
	if transition == nil || transition.Schema != AttemptSettlementTransitionV2Schema || transition.Identity != operator.Expected.Identity || transition.FromBoundary != operator.Expected.Boundary || transition.ToEpoch == 0 || transition.ToEpoch != transition.FromBoundary.SettlementEpoch+1 {
		return nil, errors.New("compact settlement transition identity or epoch differs")
	}
	if transition.PreFold.AttemptCut != nil || transition.PreFold.SettlementTransition != nil {
		return nil, errors.New("compact settlement pre-fold input contains legacy authority")
	}
	if uint64(len(transition.PreFold.Providers)) > options.MaxTransitionBytes || uint64(len(transition.PostFold)) > operator.Measurement.MaxProviders || uint64(len(transition.PostFold)) > options.MaxTransitionBytes || uint64(len(transition.Batch)) > options.MaxParticipants || uint64(len(transition.Batch)) > options.MaxTransitionBytes || signed && len(transition.Signature) != ed25519.SignatureSize || !signed && len(transition.Signature) != 0 {
		return nil, errors.New("compact settlement transition shape exceeds its bounds")
	}
	if err := transition.Cut.VerifyHeader(operator.Expected, operator.Bounds); err != nil {
		return nil, err
	}
	var hashes uint64
	for _, provider := range transition.PreFold.Providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if uint64(len(provider.EgressIPHashHexes)) > options.MaxTransitionBytes-hashes {
			return nil, errors.New("compact settlement hash metadata exceeds its byte bound")
		}
		hashes += uint64(len(provider.EgressIPHashHexes))
	}
	if _, err := newAttemptCutV2StatsProjection(ctx, transition.PreFold, operator.Expected, operator.Policy, AttemptCutV2StatsOptions{
		ExpectedConfig: operator.Measurement.ExpectedConfig, MaxProviders: operator.Measurement.MaxProviders,
		MaxEgressHashes: operator.Measurement.MaxEgressHashes, Replay: operator.Measurement.Replay,
	}, nil); err != nil {
		return nil, err
	}
	for index, quality := range transition.PostFold {
		if len(quality.ClientID) != 36 || !quality.HasQuality || quality.QualityPPM > 1_000_000 || index > 0 && quality.ClientID <= transition.PostFold[index-1].ClientID {
			return nil, errors.New("compact settlement post-fold census is not canonical")
		}
	}
	for index, member := range transition.Batch {
		if member.NoID == 0 || index > 0 && member.NoID <= transition.Batch[index-1].NoID {
			return nil, errors.New("compact settlement member census is not canonical")
		}
		if _, err := canonicalAttemptHex32("compact settlement member digest", member.Digest, false); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(transition)
	if err != nil || uint64(len(raw))+1 > options.MaxTransitionBytes {
		return nil, errors.Join(errors.New("compact settlement transition exceeds its byte bound"), err)
	}
	owned := *transition
	owned.Cut.Signature = slices.Clone(transition.Cut.Signature)
	owned.Signature = slices.Clone(transition.Signature)
	owned.PostFold = slices.Clone(transition.PostFold)
	owned.Batch = slices.Clone(transition.Batch)
	owned.PreFold.Providers = slices.Clone(transition.PreFold.Providers)
	for index := range owned.PreFold.Providers {
		provider := &owned.PreFold.Providers[index]
		provider.LatencyBuckets = slices.Clone(provider.LatencyBuckets)
		provider.EgressIPHashHexes = slices.Clone(provider.EgressIPHashHexes)
	}
	return &owned, ctx.Err()
}

// Both admission paths require exactly the configured sorted all-operator set.
// A signed core digest excludes only membership/signature; the VPK signature
// binds the complete recomputed census and cannot authenticate a partial batch.
func admitAttemptSettlementV2(ctx context.Context, closure *AttemptSettlementClosureV2, options AttemptSettlementV2Options, signed bool) (*attemptSettlementV2Operation, error) {
	ownedOptions, err := ownAttemptSettlementV2Options(ctx, options)
	if err != nil {
		return nil, err
	}
	if closure == nil || closure.Schema != AttemptSettlementClosureV2Schema || len(closure.Transitions) != len(ownedOptions.Operators) {
		return nil, errors.New("compact settlement closure omits or adds a configured participant")
	}
	owned := &AttemptSettlementClosureV2{Schema: closure.Schema, Epoch: closure.Epoch, Transitions: make([]*AttemptSettlementTransitionV2, len(closure.Transitions))}
	for index, transition := range closure.Transitions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if transition == nil || index > 0 && transition.Identity.NoID <= closure.Transitions[index-1].Identity.NoID {
			return nil, errors.New("compact settlement closure participant order is not canonical")
		}
		operator, ok := ownedOptions.Operators[transition.Identity.NoID]
		if !ok || operator.Expected.Boundary.SettlementEpoch != closure.Epoch {
			return nil, errors.New("compact settlement closure participant or epoch differs")
		}
		owned.Transitions[index], err = ownAttemptSettlementV2Transition(ctx, transition, operator, ownedOptions, signed)
		if err != nil {
			return nil, fmt.Errorf("compact settlement no_id %d: %w", transition.Identity.NoID, err)
		}
	}
	if err := boundAttemptSettlementV2Closure(owned, ownedOptions); err != nil {
		return nil, err
	}
	if signed {
		members, err := attemptSettlementV2Members(owned)
		if err != nil {
			return nil, err
		}
		for _, transition := range owned.Transitions {
			if !slices.Equal(members, transition.Batch) {
				return nil, errors.New("compact settlement complete member digest census differs")
			}
			message, err := attemptSettlementTransitionMessageV2(transition)
			if err != nil {
				return nil, err
			}
			vpk, err := canonicalAttemptHex32("compact settlement expected vpk", transition.Identity.ValidatorVPK, false)
			if err != nil || !ed25519.Verify(vpk[:], message, transition.Signature) {
				return nil, errors.New("compact settlement member signature is invalid")
			}
		}
	}
	return &attemptSettlementV2Operation{closure: owned, options: ownedOptions}, ctx.Err()
}

// Hash all cores before signing any member; this construction has no hash cycle.
func attemptSettlementV2Members(closure *AttemptSettlementClosureV2) ([]AttemptSettlementMember, error) {
	members := make([]AttemptSettlementMember, len(closure.Transitions))
	for index, transition := range closure.Transitions {
		digest, err := attemptSettlementTransitionDigestV2(transition)
		if err != nil {
			return nil, err
		}
		members[index] = AttemptSettlementMember{NoID: transition.Identity.NoID, Digest: attemptHex32(digest)}
	}
	return members, nil
}

// Byte limits cover signatures and the complete repeated participant census.
func boundAttemptSettlementV2Closure(closure *AttemptSettlementClosureV2, options AttemptSettlementV2Options) error {
	var memberBytes uint64
	for _, transition := range closure.Transitions {
		raw, err := json.Marshal(transition)
		if err != nil || uint64(len(raw))+1 > options.MaxTransitionBytes {
			return errors.Join(errors.New("compact settlement transition exceeds its byte bound"), err)
		}
		if uint64(len(raw)) > options.MaxClosureBytes-memberBytes {
			return errors.New("compact settlement closure exceeds its byte bound")
		}
		memberBytes += uint64(len(raw))
	}
	raw, err := json.Marshal(closure)
	if err != nil || uint64(len(raw))+1 > options.MaxClosureBytes {
		return errors.Join(errors.New("compact settlement closure exceeds its byte bound"), err)
	}
	return nil
}

// Retain valid zero quality and idle priors; omitted unscored providers are not
// fabricated as zero-valued EMA entries. Ordering matches legacy exact math.
func attemptSettlementV2Qualities(stats VerifiedReleaseStats) []AttemptSettlementQuality {
	qualities := make([]AttemptSettlementQuality, 0, len(stats.Providers))
	for id, provider := range stats.Providers {
		if provider.HasQuality {
			qualities = append(qualities, AttemptSettlementQuality{ClientID: id.String(), HasQuality: true, QualityPPM: provider.QualityPPM})
		}
	}
	sort.Slice(qualities, func(i, j int) bool { return qualities[i].ClientID < qualities[j].ClientID })
	return qualities
}

// The private observer belongs to one ordinary/terminal composition. It never
// replaces either projection, policy checks, proof EOF or joined reader Close.
func (self *attemptSettlementV2Operation) replay(ctx context.Context, visitRecord func(uint64, AttemptRecord) error, derive bool) (VerifiedAttemptSettlementV2, error) {
	result := VerifiedAttemptSettlementV2{Operators: make(map[uint64]VerifiedAttemptCutV2Measurement, len(self.closure.Transitions))}
	for _, transition := range self.closure.Transitions {
		if err := ctx.Err(); err != nil {
			return VerifiedAttemptSettlementV2{}, err
		}
		noID := transition.Identity.NoID
		operator := self.options.Operators[noID]
		var visit func(AttemptRecord) error
		if visitRecord != nil {
			visit = func(record AttemptRecord) error { return visitRecord(noID, record) }
		}
		verified, err := verifyReleaseStatsAndHeadWithAttemptCutV2(ctx, transition.PreFold, transition.Cut, operator.Expected, operator.Policy, operator.Bounds, operator.Measurement, visit)
		if err != nil {
			return VerifiedAttemptSettlementV2{}, fmt.Errorf("compact settlement no_id %d replay: %w", noID, err)
		}
		qualities := attemptSettlementV2Qualities(verified.Stats)
		if derive {
			transition.PostFold = qualities
		} else if !slices.Equal(qualities, transition.PostFold) {
			return VerifiedAttemptSettlementV2{}, errors.New("compact settlement post-fold EMA differs from the complete replay")
		}
		result.Operators[noID] = verified
	}
	if err := ctx.Err(); err != nil {
		return VerifiedAttemptSettlementV2{}, err
	}
	return result, nil
}

// Standalone acceptance always authenticates the full expected batch and every
// cut. Runtime journal promotion and independently pinned prior EMA are outside
// this function; neither a candidate signature nor this result invents them.
func VerifyAttemptSettlementClosureV2(ctx context.Context, closure *AttemptSettlementClosureV2, options AttemptSettlementV2Options) (VerifiedAttemptSettlementV2, error) {
	return verifyAttemptSettlementClosureV2(ctx, closure, options, nil)
}

// Shared full verification for a containing operation's private prefix join.
func verifyAttemptSettlementClosureV2(ctx context.Context, closure *AttemptSettlementClosureV2, options AttemptSettlementV2Options, visitRecord func(uint64, AttemptRecord) error) (result VerifiedAttemptSettlementV2, resultErr error) {
	if ctx == nil {
		return result, errors.New("compact settlement context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = VerifiedAttemptSettlementV2{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	operation, err := admitAttemptSettlementV2(ctx, closure, options, true)
	if err != nil {
		return result, err
	}
	return operation.replay(ctx, visitRecord, false)
}

// Signs only after every real terminal stream has completed full policy replay.
// No ledger cursor, stats snapshot, public object or transaction journal is
// mutated here. The caller must publish this owned closure before promoting its
// atomically reserved all-operator snapshots and removing the durable journal.
func SealAttemptSettlementBatchV2(ctx context.Context, inputs []AttemptSettlementV2Input, privateKeys map[uint64]ed25519.PrivateKey, options AttemptSettlementV2Options) (result *AttemptSettlementClosureV2, resultErr error) {
	if ctx == nil {
		return nil, errors.New("compact settlement context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(inputs) == 0 || len(inputs) != len(options.Operators) || len(privateKeys) != len(inputs) || uint64(len(inputs)) > options.MaxParticipants {
		return nil, errors.New("compact settlement producer participant coverage differs")
	}
	closure := &AttemptSettlementClosureV2{Schema: AttemptSettlementClosureV2Schema, Epoch: inputs[0].Cut.Context.Boundary.SettlementEpoch, Transitions: make([]*AttemptSettlementTransitionV2, len(inputs))}
	for index, input := range inputs {
		boundary := input.Cut.Context.Boundary
		closure.Transitions[index] = &AttemptSettlementTransitionV2{Schema: AttemptSettlementTransitionV2Schema, Identity: input.Cut.Context.Identity, FromBoundary: boundary, ToEpoch: boundary.SettlementEpoch + 1, PreFold: input.PreFold, Cut: input.Cut}
	}
	sort.Slice(closure.Transitions, func(i, j int) bool {
		return closure.Transitions[i].Identity.NoID < closure.Transitions[j].Identity.NoID
	})
	operation, err := admitAttemptSettlementV2(ctx, closure, options, false)
	if err != nil {
		return nil, err
	}
	ownedKeys := make(map[uint64]ed25519.PrivateKey, len(privateKeys))
	for _, transition := range operation.closure.Transitions {
		noID := transition.Identity.NoID
		vpk, err := canonicalAttemptHex32("compact settlement signing vpk", transition.Identity.ValidatorVPK, false)
		if err != nil {
			return nil, err
		}
		key := privateKeys[noID]
		if err := attemptCutV2PrivateKey(key, vpk[:]); err != nil {
			return nil, err
		}
		ownedKeys[noID] = slices.Clone(key)
	}
	if _, err := operation.replay(ctx, nil, true); err != nil {
		return nil, err
	}
	members, err := attemptSettlementV2Members(operation.closure)
	if err != nil {
		return nil, err
	}
	for _, transition := range operation.closure.Transitions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		transition.Batch = slices.Clone(members)
		message, err := attemptSettlementTransitionMessageV2(transition)
		if err != nil {
			return nil, err
		}
		transition.Signature = ed25519.Sign(ownedKeys[transition.Identity.NoID], message)
	}
	if err := boundAttemptSettlementV2Closure(operation.closure, operation.options); err != nil {
		return nil, err
	}
	return operation.closure, nil
}
