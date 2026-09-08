//go:build linux || darwin

package validator

// One control-payload allowance follows compact head collection through its
// retained state, current scores, arithmetic plans and generated transcript.
// This bounds owned values, not Go map buckets or allocator/RSS overhead.

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"math/bits"
	"reflect"
	"sort"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// Budget copies describe one phase's admission, never cached verification.
// Every returned plan includes the caller's already-owned collection payload.
type releaseHeadV2Budget struct {
	limit      uint64
	used       uint64
	output     uint64
	collection bool
}

// Multiplication is checked against the remaining original caller allowance.
func (self *releaseHeadV2Budget) charge(count, width uint64) error {
	if self.used > self.limit || width != 0 && count > (self.limit-self.used)/width {
		return errors.New("compact head EMA control storage exceeds its bound")
	}
	self.used += count * width
	return nil
}

// Generated output is reserved before construction and measured again at EOF.
func (self *releaseHeadV2Budget) chargeOutput(count, width uint64) error {
	if err := self.charge(count, width); err != nil {
		return err
	}
	self.output += count * width
	return nil
}

// Fixed-width decimal/hex key strings cannot exceed these existing field widths.
const releaseHeadV2KeyBytes = 64 + 1 + 64 + 1 + 20 + 1 + 5

// Bit lengths bound all exact rational construction without parsing strings.
// Zero is represented by a zero numerator and a one-bit denominator.
type releaseHeadV2FractionBound struct {
	numerator   uint64
	denominator uint64
}

// A UID sum must bound every prefix of the real transcript's lexical order,
// not just the planner's iteration order. Its denominator divides the product
// of all operand denominators; its numerator is below terms*2^maxN*product.
type releaseHeadV2UIDBound struct{ numerator, denominator, terms uint64 }

// Reserve the native binding result and every derived provider/fleet lookup
// before RPC or proportional construction. Transport and full proof replay
// retain their existing independent explicit bounds; copied head prefixes are
// charged to this allowance individually before insertion after actual replay.
func reserveReleaseHeadV2CollectionDerived(ctx context.Context, draft *ReleaseMeasurementArtifact, budget releaseHeadV2Budget) (releaseHeadV2Budget, error) {
	keyBytes := uint64(reflect.TypeFor[FleetScoreKey]().Size())
	stringBytes := uint64(reflect.TypeFor[string]().Size())
	mapBytes := uint64(reflect.TypeFor[map[connect.Id]bool]().Size())
	if err := budget.charge(1, uint64(reflect.TypeFor[releaseHeadResult]().Size())); err != nil {
		return budget, err
	}
	if err := budget.charge(uint64(len(draft.Inputs)), 3*(8+mapBytes)); err != nil {
		return budget, err
	}
	if err := budget.charge(uint64(len(draft.ControlledNOIDs)), 8+1); err != nil {
		return budget, err
	}
	for _, input := range draft.Inputs {
		if err := ctx.Err(); err != nil {
			return budget, err
		}
		// Sorted ids, native binding, provider-owner/seen/current lookups,
		// fleet map, bound membership, UID member rows, stale rows and replay
		// current-binding map. Strings are counted even where owners share.
		width := 2*16 + uint64(reflect.TypeFor[stabi.BindingAtOutput]().Size()) +
			3*(57+stringBytes) + 36 + 8 + 1 + keyBytes +
			keyBytes + mapBytes + 16 + 1 + 2 + uint64(reflect.TypeFor[[]releaseHeadMember]().Size()) + uint64(reflect.TypeFor[releaseHeadMember]().Size()) +
			uint64(reflect.TypeFor[StaleHeadBinding]().Size()) + 36 + 16 + keyBytes
		if err := budget.charge(uint64(len(input.Stats.Providers)), width); err != nil {
			return budget, err
		}
	}
	return budget, nil
}

// Planning order is deterministic even at an exact budget boundary. The
// fixed key slice is admitted by the caller before this allocation.
func orderedReleaseHeadV2Keys[T any](values map[FleetScoreKey]T) []FleetScoreKey {
	keys := make([]FleetScoreKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := keys[i], keys[j]
		if order := bytes.Compare(left.FleetID[:], right.FleetID[:]); order != 0 {
			return order < 0
		}
		if order := bytes.Compare(left.Hotkey[:], right.Hotkey[:]); order != 0 {
			return order < 0
		}
		if left.Generation != right.Generation {
			return left.Generation < right.Generation
		}
		return left.UID < right.UID
	})
	return keys
}

// Addition itself must not wrap while calculating an allocation reservation.
func releaseHeadV2BitSum(parts ...uint64) (uint64, error) {
	var total uint64
	for _, part := range parts {
		if part > ^uint64(0)-total {
			return 0, errors.New("compact head EMA arithmetic growth overflows")
		}
		total += part
	}
	return total, nil
}

// 10^3 < 2^10 gives an integer-only upper bound before a decimal parse.
// Canonicality and exact fold arithmetic remain the existing verifier's job.
func releaseHeadV2DecimalBits(value string) (uint64, error) {
	if len(value) == 0 {
		return 0, errors.New("compact head EMA rational text is absent")
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, errors.New("compact head EMA rational text is not unsigned decimal")
		}
	}
	if value == "0" {
		return 0, nil
	}
	length := uint64(len(value))
	if length > (^uint64(0)-2)/10 {
		return 0, errors.New("compact head EMA rational text size overflows")
	}
	return (length*10 + 2) / 3, nil
}

// Existing rational text is admitted by byte length before this bounded scan.
func releaseHeadV2TextBound(value RationalJSON) (releaseHeadV2FractionBound, error) {
	numerator, err := releaseHeadV2DecimalBits(value.Numerator)
	if err != nil {
		return releaseHeadV2FractionBound{}, err
	}
	denominator, err := releaseHeadV2DecimalBits(value.Denominator)
	if err != nil || denominator == 0 {
		return releaseHeadV2FractionBound{}, errors.Join(errors.New("compact head EMA denominator is absent or zero"), err)
	}
	return releaseHeadV2FractionBound{numerator: numerator, denominator: denominator}, nil
}

// Word payload is reserved before big.Int parsing or result construction.
func (self releaseHeadV2FractionBound) reserve(budget *releaseHeadV2Budget, output bool) error {
	wordBytes := uint64(reflect.TypeFor[big.Word]().Size())
	wordBits := wordBytes * 8
	words := self.numerator/wordBits + self.denominator/wordBits
	if self.numerator%wordBits != 0 {
		words++
	}
	if self.denominator%wordBits != 0 {
		words++
	}
	if output {
		return budget.chargeOutput(words, wordBytes)
	}
	return budget.charge(words, wordBytes)
}

// The unchanged fold parses/copies operands, verifies them independently, and
// parses the completed transcript again for UID aggregation. Eight word-value
// owners per bound conservatively cover those phases and their cross products;
// this remains explicit integer payload, not math/big allocator/RSS accounting.
func (self releaseHeadV2FractionBound) reserveArithmetic(budget *releaseHeadV2Budget) error {
	for range 8 {
		if err := self.reserve(budget, false); err != nil {
			return err
		}
	}
	return nil
}

// 2^3 < 10 bounds decimal output length without formatting a large integer.
func (self releaseHeadV2FractionBound) reserveText(budget *releaseHeadV2Budget) error {
	for _, length := range []uint64{self.numerator, self.denominator} {
		digits := length / 3
		if length%3 != 0 {
			digits++
		}
		if digits == 0 {
			digits = 1
		}
		if err := budget.chargeOutput(digits, 1); err != nil {
			return err
		}
		// Canonical verification formats the value again; the preview's entry
		// map also owns a separately formatted next value until preview ends.
		if err := budget.charge(digits, 2); err != nil {
			return err
		}
	}
	return nil
}

// Cross products and their addition are admitted before exact rational Add.
func (self releaseHeadV2FractionBound) add(other releaseHeadV2FractionBound) (releaseHeadV2FractionBound, error) {
	if self.numerator == 0 {
		return other, nil
	}
	if other.numerator == 0 {
		return self, nil
	}
	left, err := releaseHeadV2BitSum(self.numerator, other.denominator)
	if err != nil {
		return releaseHeadV2FractionBound{}, err
	}
	right, err := releaseHeadV2BitSum(other.numerator, self.denominator)
	if err != nil {
		return releaseHeadV2FractionBound{}, err
	}
	numerator, err := releaseHeadV2BitSum(max(left, right), 1)
	if err != nil {
		return releaseHeadV2FractionBound{}, err
	}
	denominator, err := releaseHeadV2BitSum(self.denominator, other.denominator)
	return releaseHeadV2FractionBound{numerator: numerator, denominator: denominator}, err
}

// Multiplication by the unchanged policy rational has fixed uint64 operands.
func (self releaseHeadV2FractionBound) multiply(numerator, denominator uint64) (releaseHeadV2FractionBound, error) {
	if numerator == 0 || self.numerator == 0 {
		return releaseHeadV2FractionBound{denominator: 1}, nil
	}
	n, err := releaseHeadV2BitSum(self.numerator, uint64(bits.Len64(numerator)))
	if err != nil {
		return releaseHeadV2FractionBound{}, err
	}
	d, err := releaseHeadV2BitSum(self.denominator, uint64(bits.Len64(denominator)))
	return releaseHeadV2FractionBound{numerator: n, denominator: d}, err
}

// Known state/epoch/policy refusal precedes callbacks and is repeated under the
// final preview lock. No parsed rational or accepted-result cache is retained.
func (self *HeadEMAStore) admitReleaseHeadV2KnownWithLock(ctx context.Context, epoch uint64, alpha protocol.Rational, maxEntries uint64, budget releaseHeadV2Budget) (releaseHeadV2Budget, error) {
	if err := ctx.Err(); err != nil {
		return budget, err
	}
	maxEntries, budget, err := self.releaseHeadV2Limits(maxEntries, budget)
	if err != nil {
		return budget, err
	}
	budget, err = self.reserveReleaseHeadV2OwnerWithLock(budget)
	if err != nil {
		return budget, err
	}
	if err := alpha.Validate("head_score_ema"); err != nil || alpha.Numerator > alpha.Denominator {
		return budget, errors.New("invalid head EMA policy")
	}
	if uint64(len(self.values)) > maxEntries || uint64(len(self.lastFold)) > maxEntries {
		return budget, errors.New("compact head EMA census exceeds its bound")
	}
	if self.lastSubnetEpoch != nil {
		if epoch < *self.lastSubnetEpoch {
			return budget, errors.New("compact head EMA epoch regressed")
		}
		if epoch == *self.lastSubnetEpoch {
			if self.lastAlpha == nil || *self.lastAlpha != alpha {
				return budget, errors.New("same-epoch head EMA policy changed")
			}
		} else if *self.lastSubnetEpoch == ^uint64(0) || epoch != *self.lastSubnetEpoch+1 {
			return budget, errors.New("compact head EMA epoch jumped")
		}
	}
	remaining := budget.limit - budget.used
	for key, value := range self.values {
		if uint64(len(key)) > remaining {
			return budget, errors.New("compact head EMA key exceeds its control bound")
		}
		remaining -= uint64(len(key))
		if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(&value), &remaining); err != nil {
			return budget, err
		}
	}
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(self.lastFold), &remaining); err != nil {
		return budget, err
	}
	budget.used = budget.limit - remaining
	return budget, ctx.Err()
}

// This preflight holds no state lock across a chain, client-key or proof call.
func (self *HeadEMAStore) admitReleaseHeadV2Known(ctx context.Context, epoch uint64, alpha protocol.Rational, maxEntries uint64, budget releaseHeadV2Budget) error {
	if self == nil || ctx == nil || maxEntries == 0 || budget.limit == 0 || budget.limit > maxReleaseMeasurementArtifactBytes || budget.used > budget.limit {
		return errors.New("compact head EMA owner or bounds are invalid")
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	_, err := self.admitReleaseHeadV2KnownWithLock(ctx, epoch, alpha, maxEntries, budget)
	return err
}

// Reserve fixed current/lookup/transcript/UID payload before formatting keys
// or constructing any proportional planner, big.Rat or returned slice.
func admitReleaseHeadV2CurrentWithLock[T any](ctx context.Context, store *HeadEMAStore, epoch uint64, current map[FleetScoreKey]T, maxEntries uint64, budget releaseHeadV2Budget) (releaseHeadV2Budget, error) {
	maxEntries, budget, err := store.releaseHeadV2Limits(maxEntries, budget)
	if err != nil {
		return budget, err
	}
	count := uint64(len(current))
	if count > maxEntries {
		return budget, errors.New("compact head EMA census exceeds its bound")
	}
	if err := budget.charge(count, releaseHeadV2KeyBytes); err != nil {
		return budget, err
	}
	entries := uint64(len(store.values))
	for key := range current {
		if err := ctx.Err(); err != nil {
			return budget, err
		}
		if _, found := store.values[key.String()]; !found {
			if entries == maxEntries {
				return budget, errors.New("compact head EMA current/history union exceeds its bound")
			}
			entries++
		}
	}
	if store.lastSubnetEpoch != nil && epoch == *store.lastSubnetEpoch {
		var expected uint64
		for _, record := range store.lastFold {
			if record.HasRaw {
				expected++
				if _, found := current[record.Key]; !found {
					return budget, errors.New("same-epoch head EMA raw identities changed")
				}
			}
		}
		if expected != count {
			return budget, errors.New("same-epoch head EMA raw input cardinality changed")
		}
		entries = uint64(len(store.lastFold))
	}
	keyBytes := uint64(reflect.TypeFor[FleetScoreKey]().Size())
	ratBytes := uint64(reflect.TypeFor[big.Rat]().Size())
	boundBytes := uint64(reflect.TypeFor[releaseHeadV2FractionBound]().Size())
	pointerBytes := uint64(reflect.TypeFor[*big.Rat]().Size())
	stringBytes := uint64(reflect.TypeFor[string]().Size())
	// Current and same-epoch candidate maps, the fraction plan and UID plan.
	if err := budget.charge(count, 2*(keyBytes+pointerBytes+ratBytes)+keyBytes+boundBytes+2+uint64(reflect.TypeFor[releaseHeadV2UIDBound]().Size())); err != nil {
		return budget, err
	}
	// The exact successor clones entries, constructs its all-key map/sort
	// slice, and owns raw/prior/next/verification rational objects per row.
	if err := budget.charge(entries, 2*releaseHeadV2KeyBytes+3*stringBytes+keyBytes+uint64(reflect.TypeFor[headEMAEntry]().Size())+10*ratBytes); err != nil {
		return budget, err
	}
	for _, entry := range store.values {
		if err := budget.charge(uint64(len(entry.Numerator))+uint64(len(entry.Denominator)), 1); err != nil {
			return budget, err
		}
	}
	if err := budget.chargeOutput(entries, uint64(reflect.TypeFor[HeadEMAMeasurement]().Size())); err != nil {
		return budget, err
	}
	if err := budget.chargeOutput(count, 2+ratBytes); err != nil {
		return budget, err
	}
	if budget.collection {
		// Shared assembly owns the exact ranking copy, eligible/ranked/return
		// rows, UID lists and membership set. No top-200 truncation is used to
		// excuse the complete eligible census or its comparison operands.
		if err := budget.charge(count, 5*uint64(reflect.TypeFor[ExactWeightInput]().Size())+3*ratBytes+8*2+3); err != nil {
			return budget, err
		}
	}
	return budget, nil
}

// The actual current/history union is known after binding census, before any
// operator's record/proof replay. Final preview repeats admission, not authority.
func (self *HeadEMAStore) admitReleaseHeadV2Current(ctx context.Context, epoch uint64, current map[FleetScoreKey]map[[32]byte]bool, alpha protocol.Rational, maxEntries uint64, budget releaseHeadV2Budget) error {
	if self == nil || ctx == nil || maxEntries == 0 || budget.limit == 0 || budget.limit > maxReleaseMeasurementArtifactBytes || budget.used > budget.limit {
		return errors.New("compact head EMA owner or bounds are invalid")
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	budget, err := self.admitReleaseHeadV2KnownWithLock(ctx, epoch, alpha, maxEntries, budget)
	if err != nil {
		return err
	}
	_, err = admitReleaseHeadV2CurrentWithLock(ctx, self, epoch, current, maxEntries, budget)
	return err
}

// Actual shared-prefix multiplicities determine the arithmetic bound. Each
// new census entry is charged before insertion; no big.Rat exists yet.
func planReleaseHeadV2Raw(ctx context.Context, fleets map[FleetScoreKey]map[[32]byte]bool, budget releaseHeadV2Budget) (map[[32]byte]uint64, map[FleetScoreKey]releaseHeadV2FractionBound, releaseHeadV2Budget, error) {
	if err := budget.charge(uint64(len(fleets)), uint64(reflect.TypeFor[FleetScoreKey]().Size())); err != nil {
		return nil, nil, budget, err
	}
	claims := map[[32]byte]uint64{}
	for _, prefixes := range fleets {
		for prefix := range prefixes {
			if err := ctx.Err(); err != nil {
				return nil, nil, budget, err
			}
			if claims[prefix] == 0 {
				if err := budget.charge(1, 32+8); err != nil {
					return nil, nil, budget, err
				}
			}
			claims[prefix]++
		}
	}
	raw := make(map[FleetScoreKey]releaseHeadV2FractionBound, len(fleets))
	for _, key := range orderedReleaseHeadV2Keys(fleets) {
		prefixes := fleets[key]
		// A reduced denominator divides the product of DISTINCT observed
		// multiplicities; repeated shared denominators do not multiply growth.
		seen := map[uint64]bool{}
		temporary, denominator, largest := uint64(0), uint64(1), uint64(1)
		for prefix := range prefixes {
			if err := ctx.Err(); err != nil {
				return nil, nil, budget, err
			}
			count := claims[prefix]
			largest = max(largest, uint64(bits.Len64(count)))
			if count > 1 && !seen[count] {
				if err := budget.charge(1, 8+1); err != nil {
					return nil, nil, budget, err
				}
				temporary += 8 + 1
				seen[count] = true
				var err error
				denominator, err = releaseHeadV2BitSum(denominator, uint64(bits.Len64(count-1)))
				if err != nil {
					return nil, nil, budget, err
				}
			}
		}
		// The temporary multiplicity set has no owner after this iteration.
		budget.used -= temporary
		numerator := uint64(bits.Len64(uint64(len(prefixes))))
		if numerator != 0 && denominator != 1 {
			var err error
			numerator, err = releaseHeadV2BitSum(numerator, denominator)
			if err != nil {
				return nil, nil, budget, err
			}
		}
		raw[key] = releaseHeadV2FractionBound{numerator: numerator, denominator: denominator}
		if len(prefixes) != 0 {
			work, err := raw[key].add(releaseHeadV2FractionBound{numerator: 1, denominator: largest})
			if err != nil {
				return nil, nil, budget, err
			}
			if err := work.reserveArithmetic(&budget); err != nil {
				return nil, nil, budget, err
			}
		}
	}
	return claims, raw, budget, nil
}

// All raw/prior/intermediate/next and per-UID aggregate growth is reserved
// before the unchanged exact preview parses or folds a single rational.
func (self *HeadEMAStore) planReleaseHeadV2PreviewWithLock(ctx context.Context, epoch uint64, raw map[FleetScoreKey]releaseHeadV2FractionBound, alpha protocol.Rational, budget releaseHeadV2Budget) (releaseHeadV2Budget, error) {
	if err := budget.charge(2, uint64(reflect.TypeFor[big.Rat]().Size())+2*uint64(reflect.TypeFor[big.Word]().Size())); err != nil {
		return budget, err
	}
	if err := budget.charge(uint64(len(raw)), uint64(reflect.TypeFor[FleetScoreKey]().Size())); err != nil {
		return budget, err
	}
	if err := budget.charge(uint64(len(self.values)), uint64(reflect.TypeFor[string]().Size())); err != nil {
		return budget, err
	}
	for _, value := range raw {
		if err := value.reserve(&budget, false); err != nil {
			return budget, errors.Join(errors.New("compact head EMA raw rational exceeds its control bound"), err)
		}
	}
	uidBounds := make(map[uint16]releaseHeadV2UIDBound, len(raw))
	addUID := func(uid uint16, next releaseHeadV2FractionBound) error {
		if next.numerator == 0 {
			return nil
		}
		prior := uidBounds[uid]
		denominator, err := releaseHeadV2BitSum(prior.denominator, next.denominator)
		if err != nil {
			return err
		}
		terms, err := releaseHeadV2BitSum(prior.terms, 1)
		if err != nil {
			return err
		}
		uidBounds[uid] = releaseHeadV2UIDBound{numerator: max(prior.numerator, next.numerator), denominator: denominator, terms: terms}
		return nil
	}
	if self.lastSubnetEpoch != nil && epoch == *self.lastSubnetEpoch {
		for _, record := range self.lastFold {
			if err := ctx.Err(); err != nil {
				return budget, err
			}
			for _, value := range []RationalJSON{record.Raw, record.Prior, record.Next} {
				if err := budget.chargeOutput(uint64(len(value.Numerator))+uint64(len(value.Denominator)), 1); err != nil {
					return budget, err
				}
			}
			if record.HasRaw {
				current, err := releaseHeadV2TextBound(record.Raw)
				if err != nil {
					return budget, err
				}
				if err := current.reserveArithmetic(&budget); err != nil {
					return budget, err
				}
				next, err := releaseHeadV2TextBound(record.Next)
				if err != nil {
					return budget, err
				}
				if err := next.reserveArithmetic(&budget); err != nil {
					return budget, err
				}
				if err := addUID(record.Key.UID, next); err != nil {
					return budget, err
				}
			}
		}
	} else {
		planRecord := func(key FleetScoreKey, current, prior releaseHeadV2FractionBound, hasRaw, hasPrior bool) error {
			next := current
			if hasPrior {
				if err := prior.reserveArithmetic(&budget); err != nil {
					return err
				}
				left, err := current.multiply(alpha.Numerator, alpha.Denominator)
				if err != nil {
					return err
				}
				right, err := prior.multiply(alpha.Denominator-alpha.Numerator, alpha.Denominator)
				if err != nil {
					return err
				}
				if err := left.reserveArithmetic(&budget); err != nil {
					return err
				}
				if err := right.reserveArithmetic(&budget); err != nil {
					return err
				}
				next, err = left.add(right)
				if err != nil {
					return err
				}
			}
			if err := current.reserveArithmetic(&budget); err != nil {
				return err
			}
			if err := next.reserveArithmetic(&budget); err != nil {
				return err
			}
			for _, value := range []releaseHeadV2FractionBound{current, prior, next} {
				if err := value.reserveText(&budget); err != nil {
					return err
				}
			}
			if hasRaw {
				if err := next.reserve(&budget, false); err != nil {
					return err
				}
				if err := addUID(key.UID, next); err != nil {
					return err
				}
			}
			return nil
		}
		priorKeys := make([]string, 0, len(self.values))
		for key := range self.values {
			priorKeys = append(priorKeys, key)
		}
		sort.Strings(priorKeys)
		for _, id := range priorKeys {
			entry := self.values[id]
			if err := ctx.Err(); err != nil {
				return budget, err
			}
			prior, err := releaseHeadV2TextBound(RationalJSON{Numerator: entry.Numerator, Denominator: entry.Denominator})
			if err != nil {
				return budget, err
			}
			current, found := raw[entry.Key]
			if !found {
				current = releaseHeadV2FractionBound{denominator: 1}
			}
			if err := planRecord(entry.Key, current, prior, found, true); err != nil {
				return budget, err
			}
		}
		for _, key := range orderedReleaseHeadV2Keys(raw) {
			current := raw[key]
			if err := ctx.Err(); err != nil {
				return budget, err
			}
			if _, found := self.values[key.String()]; !found {
				if err := planRecord(key, current, releaseHeadV2FractionBound{denominator: 1}, true, false); err != nil {
					return budget, err
				}
			}
		}
	}
	var largest releaseHeadV2FractionBound
	for _, value := range uidBounds {
		numerator, err := releaseHeadV2BitSum(value.numerator, value.denominator, uint64(bits.Len64(value.terms)))
		if err != nil {
			return budget, err
		}
		bound := releaseHeadV2FractionBound{numerator: numerator, denominator: value.denominator}
		if err := bound.reserveArithmetic(&budget); err != nil {
			return budget, err
		}
		if err := bound.reserve(&budget, true); err != nil {
			return budget, err
		}
		if budget.collection {
			// Ranking copies real scores; completed Weights and Eligible each
			// account their pointed-to value even when those pointers alias.
			for range 3 {
				if err := bound.reserve(&budget, false); err != nil {
					return budget, err
				}
			}
		}
		largest.numerator = max(largest.numerator, bound.numerator)
		largest.denominator = max(largest.denominator, bound.denominator)
	}
	if budget.collection {
		comparison, err := largest.add(largest)
		if err != nil {
			return budget, err
		}
		if err := comparison.reserveArithmetic(&budget); err != nil {
			return budget, err
		}
	}
	return budget, ctx.Err()
}

// The exact shared-prefix formula is unchanged; every arithmetic operand is
// covered by the completed plan and each finite loop observes cancellation.
func buildReleaseHeadV2Raw(ctx context.Context, fleets map[FleetScoreKey]map[[32]byte]bool, claims map[[32]byte]uint64) (map[FleetScoreKey]*big.Rat, error) {
	raw := make(map[FleetScoreKey]*big.Rat, len(fleets))
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
				return nil, errors.New("compact head EMA prefix census is incomplete")
			}
			score.Add(score, new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).SetUint64(claims[prefix])))
		}
		raw[key] = score
	}
	return raw, nil
}

// Exact completed output must fit both the shared allowance and its reserved
// portion. This check cannot substitute for the earlier growth admission.
func checkReleaseHeadV2Output(ctx context.Context, ema map[uint16]*big.Rat, head []HeadEMAMeasurement, budget releaseHeadV2Budget) error {
	remaining := budget.output
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(head), &remaining); err != nil {
		return err
	}
	output := releaseHeadV2Budget{limit: remaining}
	for _, value := range ema {
		if err := ctx.Err(); err != nil {
			return err
		}
		if value == nil {
			return errors.New("compact head EMA output rational is nil")
		}
		if err := output.charge(1, 2+uint64(reflect.TypeFor[big.Rat]().Size())); err != nil {
			return err
		}
		bound := releaseHeadV2FractionBound{numerator: uint64(value.Num().BitLen()), denominator: uint64(value.Denom().BitLen())}
		if err := bound.reserve(&output, false); err != nil {
			return err
		}
	}
	if budget.used > budget.limit || budget.output > budget.used {
		return errors.New("compact head EMA completed output exceeds combined control bound")
	}
	return ctx.Err()
}

// Measure the complete returned head after unchanged selection/exclusion.
// Shared pointers/strings are charged per returned owner, just as in the
// common control convention; a positive result never bypasses this EOF check.
func checkReleaseHeadV2Result(ctx context.Context, result releaseHeadResult, budget releaseHeadV2Budget) error {
	if budget.used > budget.limit {
		return errors.New("compact head completed result exceeds combined control bound")
	}
	remaining := budget.used
	fixed := uint64(reflect.TypeFor[releaseHeadResult]().Size())
	if fixed > remaining {
		return errors.New("compact head completed result fixed storage exceeds its bound")
	}
	remaining -= fixed
	for _, value := range []any{result.Inputs, result.Bindings, result.HeadEMA, result.StaleBindings, result.EligibleUIDs, result.SelectedUIDs, result.RejectedUIDs} {
		if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(value), &remaining); err != nil {
			return err
		}
	}
	account := releaseHeadV2Budget{limit: remaining}
	for _, rows := range [][]ExactWeightInput{result.Weights, result.Eligible} {
		if err := account.charge(uint64(len(rows)), uint64(reflect.TypeFor[ExactWeightInput]().Size())); err != nil {
			return err
		}
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			if row.Score == nil || row.Score.Sign() < 0 {
				return errors.New("compact head completed result score is nil or negative")
			}
			if err := account.charge(1, uint64(reflect.TypeFor[big.Rat]().Size())); err != nil {
				return err
			}
			value := releaseHeadV2FractionBound{numerator: uint64(row.Score.Num().BitLen()), denominator: uint64(row.Score.Denom().BitLen())}
			if err := value.reserve(&account, false); err != nil {
				return err
			}
		}
	}
	if err := account.charge(uint64(len(result.Bound)), 8+uint64(reflect.TypeFor[map[connect.Id]bool]().Size())); err != nil {
		return err
	}
	for _, members := range result.Bound {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := account.charge(uint64(len(members)), 16+1); err != nil {
			return err
		}
	}
	if err := account.charge(uint64(len(result.Controlled)), 2+1); err != nil {
		return err
	}
	return ctx.Err()
}

// Raw scores already supplied by a caller still require complete fixed,
// history, growth and completed-output admission before the exact preview.
func (self *HeadEMAStore) previewForEpochV2WithBudget(ctx context.Context, epoch uint64, raw map[FleetScoreKey]*big.Rat, alpha protocol.Rational, maxEntries uint64, budget releaseHeadV2Budget) (ema map[uint16]*big.Rat, head []HeadEMAMeasurement, resultErr error) {
	if self == nil || ctx == nil || maxEntries == 0 || budget.limit == 0 || budget.limit > maxReleaseMeasurementArtifactBytes || budget.used > budget.limit {
		return nil, nil, errors.New("compact head EMA owner or bounds are invalid")
	}
	owner, maxEntries, budget, err := self.ownReleaseHeadEMAV2(ctx, maxEntries, budget)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		resultErr = owner.finish(ctx, resultErr)
		if resultErr != nil {
			ema, head = nil, nil
		}
	}()
	self.mu.Lock()
	defer self.mu.Unlock()
	budget, err = self.admitReleaseHeadV2KnownWithLock(ctx, epoch, alpha, maxEntries, budget)
	if err != nil {
		return nil, nil, err
	}
	if uint64(len(raw)) > maxEntries {
		return nil, nil, errors.New("compact head EMA census exceeds its bound")
	}
	// Inspect existing operands before fixed-planner allocation. This retains
	// the old raw-word refusal while the full shared reservation still follows.
	wordCheck := budget
	for _, value := range raw {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if value == nil || value.Sign() < 0 {
			return nil, nil, errors.New("compact head EMA raw score is nil or negative")
		}
		bound := releaseHeadV2FractionBound{numerator: uint64(value.Num().BitLen()), denominator: uint64(value.Denom().BitLen())}
		if err := bound.reserve(&wordCheck, false); err != nil {
			return nil, nil, errors.Join(errors.New("compact head EMA raw rational exceeds its control bound"), err)
		}
	}
	budget, err = admitReleaseHeadV2CurrentWithLock(ctx, self, epoch, raw, maxEntries, budget)
	if err != nil {
		return nil, nil, err
	}
	bounds := make(map[FleetScoreKey]releaseHeadV2FractionBound, len(raw))
	for key, value := range raw {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if value == nil || value.Sign() < 0 {
			return nil, nil, errors.New("compact head EMA raw score is nil or negative")
		}
		bounds[key] = releaseHeadV2FractionBound{numerator: uint64(value.Num().BitLen()), denominator: uint64(value.Denom().BitLen())}
	}
	budget, err = self.planReleaseHeadV2PreviewWithLock(ctx, epoch, bounds, alpha, budget)
	if err != nil {
		return nil, nil, err
	}
	ema, head, err = self.previewForEpochWithLock(epoch, raw, alpha)
	if err = errors.Join(err, ctx.Err()); err != nil {
		return nil, nil, err
	}
	if err := checkReleaseHeadV2Output(ctx, ema, head, budget); err != nil {
		return nil, nil, err
	}
	return ema, head, nil
}

// Direct callers acquire the same durable owner as collection. The collector
// already retains that owner across its complete callback tree.
func (self *HeadEMAStore) previewReleaseHeadV2Fleets(ctx context.Context, epoch uint64, fleets map[FleetScoreKey]map[[32]byte]bool, alpha protocol.Rational, maxEntries uint64, budget releaseHeadV2Budget) (ema map[uint16]*big.Rat, head []HeadEMAMeasurement, planned releaseHeadV2Budget, resultErr error) {
	if self == nil || ctx == nil || maxEntries == 0 || budget.limit == 0 || budget.limit > maxReleaseMeasurementArtifactBytes || budget.used > budget.limit {
		return nil, nil, budget, errors.New("compact head EMA owner or bounds are invalid")
	}
	owner, maxEntries, budget, err := self.ownReleaseHeadEMAV2(ctx, maxEntries, budget)
	if err != nil {
		return nil, nil, budget, err
	}
	defer func() {
		resultErr = owner.finish(ctx, resultErr)
		if resultErr != nil {
			ema, head = nil, nil
		}
	}()
	return owner.previewFleets(ctx, epoch, fleets, alpha, maxEntries, budget)
}

// The operation token remains held, but the state mutex covers only bounded
// arithmetic. Native final witnesses and all external callbacks run outside it.
func (self *releaseHeadEMAOwnerV2) previewFleets(ctx context.Context, epoch uint64, fleets map[FleetScoreKey]map[[32]byte]bool, alpha protocol.Rational, maxEntries uint64, budget releaseHeadV2Budget) (map[uint16]*big.Rat, []HeadEMAMeasurement, releaseHeadV2Budget, error) {
	store := self.store
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.previewReleaseHeadV2FleetsWithLock(ctx, epoch, fleets, alpha, maxEntries, budget)
}

// Known/current/growth reservations precede all real score construction;
// neither a fresh owner budget nor a standalone runtime plan replaces them.
func (self *HeadEMAStore) previewReleaseHeadV2FleetsWithLock(ctx context.Context, epoch uint64, fleets map[FleetScoreKey]map[[32]byte]bool, alpha protocol.Rational, maxEntries uint64, budget releaseHeadV2Budget) (map[uint16]*big.Rat, []HeadEMAMeasurement, releaseHeadV2Budget, error) {
	budget, err := self.admitReleaseHeadV2KnownWithLock(ctx, epoch, alpha, maxEntries, budget)
	if err != nil {
		return nil, nil, budget, err
	}
	budget, err = admitReleaseHeadV2CurrentWithLock(ctx, self, epoch, fleets, maxEntries, budget)
	if err != nil {
		return nil, nil, budget, err
	}
	claims, bounds, budget, err := planReleaseHeadV2Raw(ctx, fleets, budget)
	if err != nil {
		return nil, nil, budget, err
	}
	budget, err = self.planReleaseHeadV2PreviewWithLock(ctx, epoch, bounds, alpha, budget)
	if err != nil {
		return nil, nil, budget, err
	}
	raw, err := buildReleaseHeadV2Raw(ctx, fleets, claims)
	if err != nil {
		return nil, nil, budget, err
	}
	ema, head, err := self.previewForEpochWithLock(epoch, raw, alpha)
	if err = errors.Join(err, ctx.Err()); err != nil {
		return nil, nil, budget, err
	}
	if err := checkReleaseHeadV2Output(ctx, ema, head, budget); err != nil {
		return nil, nil, budget, err
	}
	return ema, head, budget, nil
}
