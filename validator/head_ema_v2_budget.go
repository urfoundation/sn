package validator

// A single operation allowance covers retained/input copies, generated
// transcript/index owners and conservative exact-rational work before math.
// It bounds logical payload, not allocator overhead or process resident size.

import (
	"context"
	"errors"
	"math/big"
	"math/bits"
	"reflect"

	"github.com/urfoundation/sn/protocol"
)

// Charges remain monotonic across phases, including the eventual encoded file.
type headEMAStoreV2Budget struct { limit, used uint64 }

// Check division before multiplication, including adversarial count overflow.
func (self *headEMAStoreV2Budget) charge(count, width uint64) error {
	if self.used > self.limit || width != 0 && count > (self.limit-self.used)/width {
		return errors.New("bounded head EMA operation control allowance exceeded")
	}
	self.used += count*width
	return nil
}

// Arithmetic is planned with integer bit bounds, never parsed candidate values.
type headEMAStoreV2Fraction struct { numerator, denominator uint64 }

// Any lexical prefix of the real UID aggregation is bounded by the product of
// all operand denominators and terms times the largest numerator.
type headEMAStoreV2UIDBound struct { numerator, denominator, terms uint64 }

// Every sum used to size a later allocation is overflow-checked independently.
func headEMAStoreV2BitSum(values ...uint64) (uint64, error) {
	var total uint64
	for _, value := range values {
		if value > ^uint64(0)-total { return 0, errors.New("bounded head EMA rational growth overflow") }
		total += value
	}
	return total, nil
}

// Admission already charged the byte length; this scan constructs no integer.
func headEMAStoreV2DecimalBits(ctx context.Context, value string) (uint64, error) {
	if len(value) == 0 { return 0, errors.New("bounded head EMA rational text is empty") }
	for index := range len(value) {
		if index%4096 == 0 { if err := ctx.Err(); err != nil { return 0, err } }
		if value[index] < '0' || value[index] > '9' { return 0, errors.New("bounded head EMA rational text is not unsigned decimal") }
	}
	if value == "0" { return 0, nil }
	if uint64(len(value)) > (^uint64(0)-2)/10 { return 0, errors.New("bounded head EMA decimal growth overflow") }
	// 10^3 < 2^10, so no float rounding can understate this reserve.
	return (uint64(len(value))*10+2)/3, nil
}

// The exact canonical decoder still validates reduction and denominator sign.
func headEMAStoreV2TextFraction(ctx context.Context, value RationalJSON) (headEMAStoreV2Fraction, error) {
	numerator, err := headEMAStoreV2DecimalBits(ctx, value.Numerator)
	if err != nil { return headEMAStoreV2Fraction{}, err }
	denominator, err := headEMAStoreV2DecimalBits(ctx, value.Denominator)
	if err != nil || denominator == 0 { return headEMAStoreV2Fraction{}, errors.Join(errors.New("bounded head EMA denominator is absent or zero"), err) }
	return headEMAStoreV2Fraction{numerator: numerator, denominator: denominator}, nil
}

// Borrowed operands are inspected before a Set/copy, normalization or format.
func headEMAStoreV2RatFraction(value *big.Rat) (headEMAStoreV2Fraction, error) {
	if value == nil || value.Sign() < 0 { return headEMAStoreV2Fraction{}, errors.New("head EMA raw score is nil or negative") }
	return headEMAStoreV2Fraction{numerator: uint64(value.Num().BitLen()), denominator: uint64(value.Denom().BitLen())}, nil
}

// Word payload includes every real SetString/SetFrac, multiplication, cross
// product, independent verification, canonical formatting and output parse.
// Sixteen owners are a conservative work reserve, not sixteen fake replays.
func (self headEMAStoreV2Fraction) reserve(budget *headEMAStoreV2Budget, owners uint64) error {
	wordBytes := uint64(reflect.TypeFor[big.Word]().Size())
	wordBits := wordBytes*8
	words := self.numerator/wordBits+self.denominator/wordBits
	if self.numerator%wordBits != 0 { words++ }
	if self.denominator%wordBits != 0 { words++ }
	if err := budget.charge(words, wordBytes*owners); err != nil { return err }
	return nil
}

// Reserve generated decimal owners before any big.Int.String allocation.
func (self headEMAStoreV2Fraction) reserveText(budget *headEMAStoreV2Budget) error {
	for _, length := range [2]uint64{self.numerator, self.denominator} {
		digits := length/3
		if length%3 != 0 { digits++ }
		if digits == 0 { digits = 1 }
		// Generated record, durable entry, returned transcript and independent
		// canonical verifier each own their logical string payload.
		if err := budget.charge(digits, 4); err != nil { return err }
	}
	return nil
}

// Multiplication has fixed-width policy operands and bounded integer products.
func (self headEMAStoreV2Fraction) multiply(numerator, denominator uint64) (headEMAStoreV2Fraction, error) {
	if numerator == 0 || self.numerator == 0 { return headEMAStoreV2Fraction{denominator: 1}, nil }
	n, err := headEMAStoreV2BitSum(self.numerator, uint64(bits.Len64(numerator)))
	if err != nil { return headEMAStoreV2Fraction{}, err }
	d, err := headEMAStoreV2BitSum(self.denominator, uint64(bits.Len64(denominator)))
	return headEMAStoreV2Fraction{numerator: n, denominator: d}, err
}

// Both cross products and their addition precede normalization; reserve them.
func (self headEMAStoreV2Fraction) add(other headEMAStoreV2Fraction) (headEMAStoreV2Fraction, error) {
	if self.numerator == 0 { return other, nil }
	if other.numerator == 0 { return self, nil }
	left, err := headEMAStoreV2BitSum(self.numerator, other.denominator)
	if err != nil { return headEMAStoreV2Fraction{}, err }
	right, err := headEMAStoreV2BitSum(other.numerator, self.denominator)
	if err != nil { return headEMAStoreV2Fraction{}, err }
	n, err := headEMAStoreV2BitSum(max(left, right), 1)
	if err != nil { return headEMAStoreV2Fraction{}, err }
	d, err := headEMAStoreV2BitSum(self.denominator, other.denominator)
	return headEMAStoreV2Fraction{numerator: n, denominator: d}, err
}

// Known invalid policy, epoch and census refuse before callbacks or copying.
func (self *HeadEMAStore) admitHeadEMAStoreV2KnownWithLock(ctx context.Context, operation headEMAStoreV2Operation, epoch uint64, alpha protocol.Rational, limits HeadEMAStoreV2Limits) error {
	if err := ctx.Err(); err != nil { return err }
	if err := limits.validate(); err != nil { return err }
	if operation > headEMAStoreV2Fold { return errors.New("bounded head EMA operation is unknown") }
	if err := alpha.Validate("head_score_ema"); err != nil || alpha.Numerator > alpha.Denominator { return errors.New("invalid head EMA policy") }
	if uint64(len(self.values)) > limits.MaxEntries || uint64(len(self.lastFold)) > limits.MaxEntries {
		return errors.New("bounded head EMA retained census exceeds its allowance")
	}
	if operation != headEMAStoreV2Fold && self.lastSubnetEpoch != nil {
		if epoch < *self.lastSubnetEpoch { return errors.New("head EMA epoch regressed") }
		if epoch == *self.lastSubnetEpoch {
			if self.lastAlpha == nil || *self.lastAlpha != alpha { return errors.New("same-epoch head EMA policy changed") }
		} else if operation != headEMAStoreV2FoldEpoch && (*self.lastSubnetEpoch == ^uint64(0) || epoch != *self.lastSubnetEpoch+1) {
			return errors.New("head EMA epoch jumped")
		}
	}
	return nil
}

// Each retained or returned record owns its three canonical rational strings.
func chargeHeadEMAStoreV2Records(ctx context.Context, budget *headEMAStoreV2Budget, records []HeadEMAMeasurement, copies uint64) error {
	if err := budget.charge(uint64(len(records)), uint64(reflect.TypeFor[HeadEMAMeasurement]().Size())*copies); err != nil { return err }
	for _, record := range records {
		if err := ctx.Err(); err != nil { return err }
		for _, value := range [3]RationalJSON{record.Raw, record.Prior, record.Next} {
			if err := budget.charge(uint64(len(value.Numerator)), copies); err != nil { return err }
			if err := budget.charge(uint64(len(value.Denominator)), copies); err != nil { return err }
		}
	}
	return nil
}

// One full plan completes before proportional copies or rational work. Fixed
// lookup storage is charged before insertion; the exact current/prior union
// is refused before any transcript is parsed, cloned or folded.
func (self *HeadEMAStore) admitHeadEMAStoreV2WithLock(ctx context.Context, operation headEMAStoreV2Operation, epoch uint64, raw map[FleetScoreKey]*big.Rat, records []HeadEMAMeasurement, alpha protocol.Rational, limits HeadEMAStoreV2Limits) (headEMAStoreV2Budget, error) {
	budget := headEMAStoreV2Budget{limit: limits.MaxControlBytes}
	if err := self.admitHeadEMAStoreV2KnownWithLock(ctx, operation, epoch, alpha, limits); err != nil { return budget, err }
	if uint64(len(raw)) > limits.MaxEntries || uint64(len(records)) > limits.MaxEntries { return budget, errors.New("bounded head EMA input census exceeds its allowance") }
	fixed := headEMAStoreV2FixedControlBytes()+uint64(reflect.TypeFor[HeadEMAStore]().Size())+
		uint64(reflect.TypeFor[headEMAStoreV2Budget]().Size())+
		2*uint64(reflect.TypeFor[headEMAStoreV2JSONWriter]().Size())+
		uint64(len(headEMASchemaV2))+30+uint64(reflect.TypeFor[uint64]().Size())+uint64(reflect.TypeFor[protocol.Rational]().Size())
	if err := budget.charge(1, fixed); err != nil { return budget, err }
	// Retained/descriptor name strings and transient component-slice headers
	// from two native namespace checks share this operation allowance. The
	// bound is deliberately per byte, so multibyte names cannot undercharge.
	if err := budget.charge(uint64(len(self.path)), 8+2*uint64(reflect.TypeFor[string]().Size())); err != nil { return budget, err }
	if operation != headEMAStoreV2Preview {
		// Native hash rechecks use one fixed 4096-byte buffer and four
		// descriptor/metadata owners; this is independent of file size.
		if err := budget.charge(1, 4096+4*uint64(reflect.TypeFor[attemptPrivateFileState]().Size())+1024); err != nil { return budget, err }
		// File names held by the native descriptors share the physical prefix
		// in meaning, but each resulting string is a separate logical owner.
		if err := budget.charge(uint64(len(self.path))+24, 6); err != nil { return budget, err }
	}
	entryWidth := uint64(reflect.TypeFor[headEMAEntry]().Size())+uint64(reflect.TypeFor[string]().Size())
	if err := budget.charge(uint64(len(self.values)), 2*entryWidth); err != nil { return budget, err }
	for key, value := range self.values {
		if err := ctx.Err(); err != nil { return budget, err }
		for _, text := range [3]string{key, value.Numerator, value.Denominator} {
			if err := budget.charge(uint64(len(text)), 2); err != nil { return budget, err }
		}
	}
	if err := chargeHeadEMAStoreV2Records(ctx, &budget, self.lastFold, 2); err != nil { return budget, err }
	if err := chargeHeadEMAStoreV2Records(ctx, &budget, records, 2); err != nil { return budget, err }
	inputCount := uint64(len(raw))
	if operation == headEMAStoreV2Commit { inputCount = uint64(len(records)) }
	planWidth := uint64(reflect.TypeFor[FleetScoreKey]().Size())+uint64(reflect.TypeFor[headEMAStoreV2Fraction]().Size())
	if err := budget.charge(inputCount, planWidth+uint64(reflect.TypeFor[*big.Rat]().Size())+uint64(reflect.TypeFor[big.Rat]().Size())); err != nil { return budget, err }
	if err := budget.charge(inputCount+uint64(len(self.values)), 156+planWidth+uint64(reflect.TypeFor[headEMAStoreV2UIDBound]().Size())+2); err != nil { return budget, err }
	current := make(map[FleetScoreKey]headEMAStoreV2Fraction, int(inputCount))
	if operation == headEMAStoreV2Commit {
		for _, record := range records {
			if err := ctx.Err(); err != nil { return budget, err }
			// Bound all supplied fields, including absent raw/prior text,
			// before rawHeadEMAInputs or the exact transcript comparison.
			for _, value := range [3]RationalJSON{record.Raw, record.Prior, record.Next} {
				bound, err := headEMAStoreV2TextFraction(ctx, value)
				if err != nil { return budget, err }
				if err := bound.reserve(&budget, 16); err != nil { return budget, err }
			}
			if record.HasRaw {
				if _, found := current[record.Key]; found { return budget, errors.New("head EMA transcript duplicates a raw identity") }
				bound, err := headEMAStoreV2TextFraction(ctx, record.Raw)
				if err != nil { return budget, err }
				current[record.Key] = bound
			}
		}
	} else {
		for key, value := range raw {
			if err := ctx.Err(); err != nil { return budget, err }
			bound, err := headEMAStoreV2RatFraction(value)
			if err != nil { return budget, err }
			// Same-epoch comparison can cross-multiply an independently large
			// caller operand even when the retained transcript is tiny.
			if err := bound.reserve(&budget, 16); err != nil { return budget, err }
			current[key] = bound
		}
	}
	union := uint64(len(self.values))
	for key := range current {
		if err := ctx.Err(); err != nil { return budget, err }
		if _, found := self.values[key.String()]; !found {
			if union >= limits.MaxEntries { return budget, errors.New("bounded head EMA current/prior union exceeds its allowance") }
			union++
		}
	}
	sameEpoch := operation != headEMAStoreV2Fold && self.lastSubnetEpoch != nil && epoch == *self.lastSubnetEpoch
	if sameEpoch { union = uint64(len(self.lastFold)) }
	// Includes scratch entries, all-key map and sorting, three transcript
	// owners, output maps, and fixed rational/verification temporaries.
	if err := budget.charge(union, 4*156+3*uint64(reflect.TypeFor[HeadEMAMeasurement]().Size())+2*entryWidth+2048); err != nil { return budget, err }
	uidBounds := make(map[uint16]headEMAStoreV2UIDBound, len(current))
	addUID := func(uid uint16, next headEMAStoreV2Fraction) error {
		if next.numerator == 0 { return nil }
		prior := uidBounds[uid]
		denominator, err := headEMAStoreV2BitSum(prior.denominator, next.denominator)
		if err != nil { return err }
		terms, err := headEMAStoreV2BitSum(prior.terms, 1)
		if err != nil { return err }
		uidBounds[uid] = headEMAStoreV2UIDBound{numerator: max(prior.numerator, next.numerator), denominator: denominator, terms: terms}
		return nil
	}
	plan := func(key FleetScoreKey, current, prior headEMAStoreV2Fraction, hasRaw, hasPrior bool) error {
		next := current
		if hasPrior {
			left, err := current.multiply(alpha.Numerator, alpha.Denominator)
			if err != nil { return err }
			right, err := prior.multiply(alpha.Denominator-alpha.Numerator, alpha.Denominator)
			if err != nil { return err }
			for _, value := range [2]headEMAStoreV2Fraction{left, right} { if err := value.reserve(&budget, 16); err != nil { return err } }
			next, err = left.add(right)
			if err != nil { return err }
		}
		for _, value := range [3]headEMAStoreV2Fraction{current, prior, next} {
			if err := value.reserve(&budget, 16); err != nil { return err }
			if err := value.reserveText(&budget); err != nil { return err }
		}
		if hasRaw { return addUID(key.UID, next) }
		return nil
	}
	if sameEpoch {
		for _, record := range self.lastFold {
			if err := ctx.Err(); err != nil { return budget, err }
			for _, value := range [3]RationalJSON{record.Raw, record.Prior, record.Next} {
				bound, err := headEMAStoreV2TextFraction(ctx, value)
				if err != nil { return budget, err }
				if err := bound.reserve(&budget, 16); err != nil { return budget, err }
			}
			if record.HasRaw {
				bound, err := headEMAStoreV2TextFraction(ctx, record.Next)
				if err != nil { return budget, err }
				if err := addUID(record.Key.UID, bound); err != nil { return budget, err }
			}
		}
	} else {
		for _, entry := range self.values {
			if err := ctx.Err(); err != nil { return budget, err }
			prior, err := headEMAStoreV2TextFraction(ctx, RationalJSON{Numerator: entry.Numerator, Denominator: entry.Denominator})
			if err != nil { return budget, err }
			value, found := current[entry.Key]
			if !found { value = headEMAStoreV2Fraction{denominator: 1} }
			if err := plan(entry.Key, value, prior, found, true); err != nil { return budget, err }
		}
		for key, value := range current {
			if err := ctx.Err(); err != nil { return budget, err }
			if _, found := self.values[key.String()]; !found {
				if err := plan(key, value, headEMAStoreV2Fraction{denominator: 1}, true, false); err != nil { return budget, err }
			}
		}
	}
	for _, value := range uidBounds {
		numerator, err := headEMAStoreV2BitSum(value.numerator, value.denominator, uint64(bits.Len64(value.terms)))
		if err != nil { return budget, err }
		bound := headEMAStoreV2Fraction{numerator: numerator, denominator: value.denominator}
		if err := bound.reserve(&budget, 16); err != nil { return budget, err }
	}
	return budget, ctx.Err()
}

// The completed owned state and every returned logical owner fit the earlier
// shared reservation. This cannot substitute for pre-allocation growth checks.
func checkHeadEMAStoreV2Completed(ctx context.Context, store *HeadEMAStore, output map[uint16]*big.Rat, records []HeadEMAMeasurement, reservation headEMAStoreV2Budget) error {
	if reservation.used > reservation.limit { return errors.New("bounded head EMA completed plan exceeds its allowance") }
	actual := headEMAStoreV2Budget{limit: reservation.used}
	if err := actual.charge(1, headEMAStoreV2FixedControlBytes()); err != nil { return err }
	if err := actual.charge(uint64(len(store.path)), 1); err != nil { return err }
	if err := actual.charge(uint64(len(store.values)), uint64(reflect.TypeFor[headEMAEntry]().Size())+uint64(reflect.TypeFor[string]().Size())); err != nil { return err }
	for key, value := range store.values {
		if err := ctx.Err(); err != nil { return err }
		for _, text := range [3]string{key, value.Numerator, value.Denominator} { if err := actual.charge(uint64(len(text)), 1); err != nil { return err } }
	}
	if err := chargeHeadEMAStoreV2Records(ctx, &actual, store.lastFold, 1); err != nil { return err }
	if err := chargeHeadEMAStoreV2Records(ctx, &actual, records, 1); err != nil { return err }
	if err := actual.charge(uint64(len(output)), 2+uint64(reflect.TypeFor[*big.Rat]().Size())+uint64(reflect.TypeFor[big.Rat]().Size())); err != nil { return err }
	for _, value := range output {
		if err := ctx.Err(); err != nil { return err }
		bound, err := headEMAStoreV2RatFraction(value)
		if err != nil { return err }
		if err := bound.reserve(&actual, 1); err != nil { return err }
	}
	return ctx.Err()
}
