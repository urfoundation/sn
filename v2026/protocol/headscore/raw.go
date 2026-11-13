package headscore

import (
	"context"
	"errors"
	"math/big"
)

// ErrClaimCensusIncomplete reports a prefix that a fleet showed but that has
// no claim in the census supplied with it.
var ErrClaimCensusIncomplete = errors.New("head prefix claim census is incomplete")

// Claims counts, for every prefix, the fleets of one tempo's census that
// showed it. A fleet's prefix set is its map's keys; the values are ignored.
func Claims[K comparable](fleets map[K]map[[32]byte]bool) map[[32]byte]uint64 {
	claims := map[[32]byte]uint64{}
	for _, prefixes := range fleets {
		for prefix := range prefixes {
			claims[prefix]++
		}
	}
	return claims
}

// RawScores is each fleet's raw head score for one tempo: Σ 1/claim(h) over
// the distinct prefixes h it showed, where claim(h) is the number of the
// census's fleets that showed h. A fleet that showed no prefix scores zero
// and stays live. The census is keyed by fleet identity, as the validator
// scores it, but any comparable key works.
func RawScores[K comparable](fleets map[K]map[[32]byte]bool) map[K]*big.Rat {
	claims := Claims(fleets)
	raw := make(map[K]*big.Rat, len(fleets))
	for key, prefixes := range fleets {
		score := new(big.Rat)
		for prefix := range prefixes {
			score.Add(score, share(claims[prefix]))
		}
		raw[key] = score
	}
	return raw
}

// RawScoresWithClaims is RawScores over a claim census the caller already
// counted, as a caller that budgets the census does. It observes ctx before
// every fleet and every term, and refuses a prefix the census does not claim.
func RawScoresWithClaims[K comparable](ctx context.Context, fleets map[K]map[[32]byte]bool, claims map[[32]byte]uint64) (map[K]*big.Rat, error) {
	raw := make(map[K]*big.Rat, len(fleets))
	for key, prefixes := range fleets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		score := new(big.Rat)
		for prefix := range prefixes {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if claims[prefix] == 0 {
				return nil, ErrClaimCensusIncomplete
			}
			score.Add(score, share(claims[prefix]))
		}
		raw[key] = score
	}
	return raw, nil
}

// share is one prefix's term, 1/claim, for a positive claim.
func share(claim uint64) *big.Rat {
	return new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).SetUint64(claim))
}
