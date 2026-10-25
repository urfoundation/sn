//go:build linux || darwin

package validator

// Canonical restart decoding and consecutive-window joins retain the same
// complete batch verifier. No filesystem retry cache or trusted-member flag
// may replace replay, exact post-fold priors, or authenticated chain context.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// The decoder has no destination for legacy cuts or recursive transitions.
// A competing authority field is unknown even before canonical comparison.
type attemptSettlementV2RawStats struct {
	Config    ReleaseStatsConfig           `json:"config"`
	Providers []ReleaseProviderMeasurement `json:"providers"`
}

// Separate raw input prevents a v1 history hidden in PreFold from allocating
// a complete legacy tree before the compact-only admission can reject it.
type attemptSettlementV2RawTransition struct {
	Schema       string                      `json:"schema"`
	Identity     AttemptLedgerIdentity       `json:"identity"`
	FromBoundary AttemptBoundary             `json:"from_boundary"`
	ToEpoch      uint64                      `json:"to_epoch"`
	PreFold      attemptSettlementV2RawStats `json:"pre_fold"`
	Cut          AttemptCutV2                `json:"cut"`
	PostFold     []AttemptSettlementQuality  `json:"post_fold"`
	Batch        []AttemptSettlementMember   `json:"batch"`
	Signature    []byte                      `json:"signature"`
}

// Strict, bounded public decoding includes full policy replay on every call.
// Reopening the same immutable bytes is not a cached authentication verdict;
// the caller supplies a fresh replay namespace for each required operation.
func DecodeAttemptSettlementClosureV2(ctx context.Context, raw []byte, options AttemptSettlementV2Options) (closure *AttemptSettlementClosureV2, result VerifiedAttemptSettlementV2, resultErr error) {
	if ctx == nil {
		return nil, result, errors.New("compact settlement context is nil")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			closure, result = nil, VerifiedAttemptSettlementV2{}
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, result, err
	}
	closure, err := decodeAttemptSettlementClosureV2Bytes(ctx, raw, options.MaxClosureBytes, options.MaxParticipants)
	if err != nil {
		return nil, result, err
	}
	operation, err := admitAttemptSettlementV2(ctx, closure, options, true)
	if err != nil {
		return nil, result, err
	}
	result, err = operation.replay(ctx, nil, false)
	if err != nil {
		return nil, VerifiedAttemptSettlementV2{}, err
	}
	return operation.closure, result, nil
}

// Containing measurement operations decode the same strict compact-only wire,
// then admit all authorities before their first replay. This helper proves
// canonical bytes only; no result can replace the complete batch verifier.
func decodeAttemptSettlementClosureV2Bytes(ctx context.Context, raw []byte, maxBytes, maxParticipants uint64) (*AttemptSettlementClosureV2, error) {
	if ctx == nil {
		return nil, errors.New("compact settlement context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxBytes == 0 || maxParticipants == 0 || uint64(len(raw)) > maxBytes {
		return nil, errors.New("compact settlement encoded closure exceeds its byte or participant bound")
	}
	var wire struct {
		Schema      string                              `json:"schema"`
		Epoch       uint64                              `json:"epoch"`
		Transitions []*attemptSettlementV2RawTransition `json:"transitions"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("compact settlement closure has trailing data")
	}
	if uint64(len(wire.Transitions)) > maxParticipants {
		return nil, errors.New("compact settlement encoded participant census exceeds its bound")
	}
	closure := &AttemptSettlementClosureV2{Schema: wire.Schema, Epoch: wire.Epoch, Transitions: make([]*AttemptSettlementTransitionV2, len(wire.Transitions))}
	for index, transition := range wire.Transitions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if transition == nil {
			return nil, errors.New("compact settlement closure contains nil")
		}
		closure.Transitions[index] = &AttemptSettlementTransitionV2{
			Schema: transition.Schema, Identity: transition.Identity, FromBoundary: transition.FromBoundary,
			ToEpoch: transition.ToEpoch, PreFold: ReleaseStatsMeasurement{Config: transition.PreFold.Config, Providers: transition.PreFold.Providers},
			Cut: transition.Cut, PostFold: transition.PostFold, Batch: transition.Batch, Signature: transition.Signature,
		}
	}
	encoded, err := json.Marshal(closure)
	if err != nil || !bytes.Equal(raw, append(encoded, '\n')) {
		return nil, errors.New("compact settlement closure is not canonical")
	}
	return closure, ctx.Err()
}

// Used only by containing operations that perform both complete verifications.
// Immediate successor cuts consume the one settlement rotation. Later native
// windows keep its settlement prefix but may have rotated egress again.
func verifyAttemptSettlementV2Successor(transition *AttemptSettlementTransitionV2, measurement ReleaseStatsMeasurement, cut AttemptCutV2, immediate bool) error {
	if transition == nil || measurement.AttemptCut != nil || measurement.SettlementTransition != nil || measurement.Config != transition.PreFold.Config {
		return errors.New("compact settlement successor statistics authority differs")
	}
	prior := transition.Cut
	if prior.LastSequence == ^uint64(0) || prior.Context.EgressGeneration == ^uint64(0) || cut.Context.Identity != transition.Identity || cut.Context.Activation != prior.Context.Activation || cut.Context.Boundary.SettlementEpoch != transition.ToEpoch || cut.Context.FirstSequence != prior.LastSequence+1 || cut.Context.PriorRoot != prior.Root || cut.Context.Boundary.EVMBlock <= transition.FromBoundary.EVMBlock || cut.Context.EgressGeneration <= prior.Context.EgressGeneration {
		return errors.New("compact settlement successor epoch, boundary, activation or cursor differs")
	}
	if immediate && (cut.Context.EgressFirstSequence != cut.Context.FirstSequence || cut.Context.EgressGeneration != prior.Context.EgressGeneration+1) {
		return errors.New("compact settlement immediate successor lost its exact egress rotation")
	}
	wantKVs := make(map[string]uint32, len(transition.PostFold))
	for _, quality := range transition.PostFold {
		wantKVs[quality.ClientID] = quality.QualityPPM
	}
	seenKVs := map[string]bool{}
	for _, provider := range measurement.Providers {
		want, exists := wantKVs[provider.ClientID]
		if provider.HasPriorQuality != exists || exists && provider.PriorQualityPPM != want || !exists && provider.PriorQualityPPM != 0 {
			return fmt.Errorf("compact settlement successor provider %s prior EMA differs", provider.ClientID)
		}
		if exists {
			seenKVs[provider.ClientID] = true
		}
	}
	if len(seenKVs) != len(wantKVs) {
		return errors.New("compact settlement successor prior EMA census is incomplete")
	}
	return nil
}

// An independent restart/history reader verifies every member of both windows
// once, then joins their exact fold/cursor boundary. Current is a later terminal
// cut, so native egress rotations within that settlement are allowed. This does
// not authenticate chain ancestry or the first window's external prior EMA.
func VerifyAttemptSettlementClosureV2Lineage(ctx context.Context, prior, current *AttemptSettlementClosureV2, priorOptions, currentOptions AttemptSettlementV2Options) (result VerifiedAttemptSettlementV2, resultErr error) {
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
	previous, err := admitAttemptSettlementV2(ctx, prior, priorOptions, true)
	if err != nil {
		return result, err
	}
	next, err := admitAttemptSettlementV2(ctx, current, currentOptions, true)
	if err != nil {
		return result, err
	}
	if previous.closure.Epoch == ^uint64(0) || next.closure.Epoch != previous.closure.Epoch+1 || len(previous.closure.Transitions) != len(next.closure.Transitions) {
		return result, errors.New("compact settlement closure lineage skips an epoch or participant")
	}
	// Both admitted traversals need disjoint owners before the first window
	// reads or creates its scratch, including aliases across operator roles.
	scratchKVs := map[string]bool{}
	for _, operation := range []*attemptSettlementV2Operation{previous, next} {
		for _, operator := range operation.options.Operators {
			path := operator.Measurement.Replay.ScratchDirectory
			if scratchKVs[path] {
				return result, errors.New("compact settlement closure lineage reuses a replay namespace")
			}
			scratchKVs[path] = true
		}
	}
	for index, transition := range next.closure.Transitions {
		old := previous.closure.Transitions[index]
		if old.Identity.NoID != transition.Identity.NoID {
			return result, errors.New("compact settlement closure lineage changes a participant")
		}
		if err := verifyAttemptSettlementV2Successor(old, transition.PreFold, transition.Cut, false); err != nil {
			return result, err
		}
	}
	if _, err := previous.replay(ctx, nil, false); err != nil {
		return result, err
	}
	return next.replay(ctx, nil, false)
}
