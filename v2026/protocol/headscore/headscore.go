// Package headscore is the SN25 validator's exact head arithmetic, the
// policy's integer_rational_v1:
//
//   - the per-tempo raw score of a fleet identity, Σ 1/claim(h) over the
//     distinct egress prefix hashes h it showed, where claim(h) counts the
//     tempo's fleet identities that showed h ([RawScores]);
//   - the head score EMA, next = α·raw + (1 − α)·prior, where a new identity
//     starts at its raw score, a new binding generation is a new identity and
//     inherits nothing, and an identity with no hops, live or not, is
//     multiplied by 1 − α ([Fold], [Next]);
//   - the per-UID head score, the sum of the positive EMA values of the UID's
//     live identities ([Scores]);
//   - the deterministic rank order, score descending then UID ascending
//     ([Compare]), and the top-N head selection ([Select]);
//   - the canonical exact encoding of those rationals ([Rational]) and a float
//     view for display ([Float64], [Decimal]).
//
// The validator computes its head with this package, so an operator that
// imports it computes a claimed fleet's head score bit for bit as the
// validator does. Every value is an exact big.Rat; nothing rounds.
//
// The package depends only on the Go standard library. It has no validator
// runtime, chain, file or network access: callers supply the tempo's prefix
// census and the prior EMA state, and persist the state that [Fold] returns.
// [Fold] applies exactly one tempo. The validator folds once per native epoch
// it steers and never manufactures a fold for an epoch it skipped.
package headscore

import (
	"math/big"
)

// TempoResult is one tempo's complete head computation.
type TempoResult struct {
	// Raw is each live identity's raw score for the tempo.
	Raw map[FleetKey]*big.Rat
	// Records is the fold of every identity in the prior state or the
	// census, in canonical key order.
	Records []FoldRecord
	// State is the EMA state to persist for the next tempo.
	State map[FleetKey]*big.Rat
	// Scores is each eligible UID's head score.
	Scores map[uint16]*big.Rat
	// Selection is the ranked head: the selected seats, then the rest.
	Selection Selection
}

// Step computes one tempo exactly as the validator does: the raw scores of
// the tempo's census, the EMA fold from the prior state, the per-UID scores
// and the selection of at most maximum head seats. The census lists every
// live fleet identity with the set of distinct prefixes it showed on non-seed
// hops of complete trails; a live identity with no hops has an empty set.
// The prior state is the State of the previous Step (empty at the start).
func Step(prior map[FleetKey]*big.Rat, census map[FleetKey]map[[32]byte]bool, alpha Ratio, maximum uint16) (TempoResult, error) {
	raw := RawScores(census)
	records, err := Fold(prior, raw, alpha)
	if err != nil {
		return TempoResult{}, err
	}
	scores := Scores(records)
	inputs := make([]UIDScore, 0, len(scores))
	for uid, score := range scores {
		inputs = append(inputs, UIDScore{UID: uid, Score: score})
	}
	selection, err := Select(inputs, maximum)
	if err != nil {
		return TempoResult{}, err
	}
	return TempoResult{Raw: raw, Records: records, State: State(records), Scores: scores, Selection: selection}, nil
}
