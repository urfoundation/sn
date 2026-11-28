package headscore

import (
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
)

// Ratio is a policy rational such as head_score_ema, which is 1/4 on
// mainnet. It need not be reduced.
type Ratio struct {
	Numerator   uint64
	Denominator uint64
}

var (
	errInvalidAlpha = errors.New("invalid head EMA policy")
	errInvalidRaw   = errors.New("head EMA raw score is nil or negative")
	errInvalidPrior = errors.New("head EMA prior score is nil or not positive")
	errFoldOrder    = errors.New("head EMA records are not strictly ordered")
)

// Validate accepts an EMA weight in [0, 1] with a nonzero denominator.
func (self Ratio) Validate() error {
	if self.Denominator == 0 || self.Numerator > self.Denominator {
		return errInvalidAlpha
	}
	return nil
}

// emaWeights holds α and 1 − α for one fold.
type emaWeights struct {
	alpha    *big.Rat
	oneMinus *big.Rat
}

func (self Ratio) weights() (emaWeights, error) {
	if err := self.Validate(); err != nil {
		return emaWeights{}, err
	}
	alpha := new(big.Rat).SetFrac(new(big.Int).SetUint64(self.Numerator), new(big.Int).SetUint64(self.Denominator))
	return emaWeights{alpha: alpha, oneMinus: new(big.Rat).Sub(big.NewRat(1, 1), alpha)}, nil
}

// next is raw for an identity with no prior, else α·raw + (1 − α)·prior.
func (self emaWeights) next(raw, prior *big.Rat, hasPrior bool) *big.Rat {
	next := new(big.Rat)
	if !hasPrior {
		return next.Set(raw)
	}
	return next.Add(new(big.Rat).Mul(self.alpha, raw), new(big.Rat).Mul(self.oneMinus, prior))
}

// Next is one identity's EMA step. An identity with no prior starts at its
// raw score; otherwise next = α·raw + (1 − α)·prior. An identity that is not
// live this tempo is folded with a raw score of zero.
func Next(raw, prior *big.Rat, hasPrior bool, alpha Ratio) (*big.Rat, error) {
	weights, err := alpha.weights()
	if err != nil {
		return nil, err
	}
	if raw == nil || raw.Sign() < 0 {
		return nil, errInvalidRaw
	}
	if hasPrior && (prior == nil || prior.Sign() <= 0) {
		return nil, errInvalidPrior
	}
	return weights.next(raw, prior, hasPrior), nil
}

// FoldRecord is one identity's step in a tempo's fold. Raw and Prior are zero
// when absent.
type FoldRecord struct {
	Key FleetKey
	// HasRaw: the identity is live this tempo and Raw is its raw score.
	HasRaw bool
	Raw    *big.Rat
	// HasPrior: the identity had positive EMA state before this tempo.
	HasPrior bool
	Prior    *big.Rat
	Next     *big.Rat
}

// Fold applies one tempo's raw scores to the prior EMA state. It returns a
// record for every identity in either, in canonical key order:
//
//   - a live identity with a prior moves to α·raw + (1 − α)·prior;
//   - a live identity without one starts at its raw score;
//   - an identity that is not live decays to (1 − α)·prior.
//
// Prior values must be positive, as State returns them; raw values must not
// be negative. Fold does not change its arguments.
func Fold(prior, raw map[FleetKey]*big.Rat, alpha Ratio) ([]FoldRecord, error) {
	weights, err := alpha.weights()
	if err != nil {
		return nil, err
	}
	// Each key's canonical text is rendered once for the sort.
	type orderedKey struct {
		key  FleetKey
		text string
	}
	keys := make([]orderedKey, 0, len(prior)+len(raw))
	for key, value := range prior {
		if value == nil || value.Sign() <= 0 {
			return nil, errInvalidPrior
		}
		keys = append(keys, orderedKey{key: key, text: key.String()})
	}
	for key, value := range raw {
		if value == nil || value.Sign() < 0 {
			return nil, errInvalidRaw
		}
		if _, ok := prior[key]; !ok {
			keys = append(keys, orderedKey{key: key, text: key.String()})
		}
	}
	slices.SortFunc(keys, func(a, b orderedKey) int { return strings.Compare(a.text, b.text) })
	records := make([]FoldRecord, 0, len(keys))
	for _, ordered := range keys {
		key := ordered.key
		record := FoldRecord{Key: key, Raw: new(big.Rat), Prior: new(big.Rat)}
		if value, ok := raw[key]; ok {
			record.HasRaw = true
			record.Raw.Set(value)
		}
		if value, ok := prior[key]; ok {
			record.HasPrior = true
			record.Prior.Set(value)
		}
		record.Next = weights.next(record.Raw, record.Prior, record.HasPrior)
		records = append(records, record)
	}
	return records, nil
}

// State is the EMA state a fold leaves: every identity with a positive next
// value, live or not. An identity that reaches zero is dropped.
func State(records []FoldRecord) map[FleetKey]*big.Rat {
	state := make(map[FleetKey]*big.Rat, len(records))
	for _, record := range records {
		if record.Next != nil && record.Next.Sign() > 0 {
			state[record.Key] = new(big.Rat).Set(record.Next)
		}
	}
	return state
}

// Scores is each UID's head score after a fold: the sum of the next values of
// its live identities. Only UIDs with a positive score appear; an identity
// that is not live this tempo keeps decaying in State but scores nothing.
// Scores reads only each record's Key, HasRaw and Next.
func Scores(records []FoldRecord) map[uint16]*big.Rat {
	scores := map[uint16]*big.Rat{}
	for _, record := range records {
		if !record.HasRaw || record.Next == nil || record.Next.Sign() == 0 {
			continue
		}
		if scores[record.Key.UID] == nil {
			scores[record.Key.UID] = new(big.Rat)
		}
		scores[record.Key.UID].Add(scores[record.Key.UID], record.Next)
	}
	return scores
}

// VerifyFold checks a fold transcript independently: strictly increasing
// canonical key order, a raw or prior value on every record, absent values
// equal to zero, positive priors, and every next value equal to the exact
// fold of its raw and prior values.
func VerifyFold(records []FoldRecord, alpha Ratio) error {
	weights, err := alpha.weights()
	if err != nil {
		return err
	}
	priorKey := ""
	for index, record := range records {
		key := record.Key.String()
		if priorKey != "" && key <= priorKey {
			return errFoldOrder
		}
		if !record.HasRaw && !record.HasPrior {
			return fmt.Errorf("head EMA record %d has neither raw nor prior state", index)
		}
		if record.Raw == nil || record.Raw.Sign() < 0 || (!record.HasRaw && record.Raw.Sign() != 0) {
			return fmt.Errorf("head EMA record %d raw value is invalid", index)
		}
		if record.Prior == nil || record.Prior.Sign() < 0 || (!record.HasPrior && record.Prior.Sign() != 0) || (record.HasPrior && record.Prior.Sign() <= 0) {
			return fmt.Errorf("head EMA record %d prior value is invalid", index)
		}
		if record.Next == nil || record.Next.Sign() < 0 {
			return fmt.Errorf("head EMA record %d next value is invalid", index)
		}
		if record.Next.Cmp(weights.next(record.Raw, record.Prior, record.HasPrior)) != 0 {
			return fmt.Errorf("head EMA record %d next value does not match the exact fold", index)
		}
		priorKey = key
	}
	return nil
}
