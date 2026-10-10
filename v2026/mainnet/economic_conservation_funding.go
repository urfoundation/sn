// Original funding is a flow of fungible stake, not fresh income at each
// finalization or payment. Exact execution causes bound the income component;
// withdrawals and partial claims do not authorize an invented FIFO rule.
package main

import (
	"context"
	"errors"
	"math/big"
)

// Bounds describe the same amount. NonIncome is its complementary component,
// including original stock, deposits and refunds. Complete means all original
// source causes are known; it does not mean ambiguous fungible units are tagged.
type economicFundingRange struct {
	Amount           string `json:"amount_alpha"`
	MinimumIncome    string `json:"minimum_native_income_alpha"`
	MaximumIncome    string `json:"maximum_native_income_alpha"`
	MinimumNonIncome string `json:"minimum_non_income_alpha"`
	MaximumNonIncome string `json:"maximum_non_income_alpha"`
	Complete         bool   `json:"original_sources_complete"`
}

// Compact sources keep the receipt and execution identity after cold retirement.
type economicFundingCapture struct {
	Id          string               `json:"original_capture_id"`
	Pool        string               `json:"original_pool"`
	Epoch       string               `json:"original_epoch"`
	Receipt     string               `json:"original_receipt_hash"`
	EffectsHash string               `json:"original_effects_hash,omitempty"`
	Funding     economicFundingRange `json:"funding"`
	Opening     *string              `json:"original_opening_stock_alpha"`
	Deposits    *string              `json:"original_deposits_alpha"`
	Withdrawals *string              `json:"original_withdrawals_alpha"`
	Refunds     *string              `json:"original_refunds_alpha"`
}

// A carried obligation retains its original epoch, pool and complete source DAG.
type economicFundingEntitlement struct {
	Id          string               `json:"original_entitlement_id"`
	Pool        string               `json:"original_pool"`
	Epoch       string               `json:"original_epoch"`
	FundingHash string               `json:"original_funding_hash"`
	Funding     economicFundingRange `json:"funding"`
}

// This summary counts each original capture, accepted leaf and paid transfer
// once. An expired or missed root transports prior funds and cannot add income.
type economicConservationFundingSummary struct {
	Captured                  economicFundingRange         `json:"original_captured_sources"`
	Accepted                  economicFundingRange         `json:"original_accepted_claims"`
	Paid                      economicFundingRange         `json:"original_paid_transfers"`
	OriginalCaptures          uint64                       `json:"original_captures"`
	OriginalClaims            uint64                       `json:"original_claims"`
	OriginalPayments          uint64                       `json:"original_payments"`
	ActiveEntitlements        []economicFundingEntitlement `json:"active_original_entitlements"`
	CapitalSubsidyAuthorized  bool                         `json:"capital_subsidy_authorized"`
	NoNonIncomeProviderCredit *bool                        `json:"provider_credit_excludes_non_income"`
	Authority                 string                       `json:"authority"`
}

// Every constructor checks both canonical integers and complementary bounds.
func newEconomicFundingRange(amount, minimum, maximum string, complete bool) (economicFundingRange, error) {
	var result economicFundingRange
	total, err := monitorEconomicInteger(amount)
	if err != nil {
		return result, err
	}
	low, err := monitorEconomicInteger(minimum)
	if err != nil {
		return result, err
	}
	high, err := monitorEconomicInteger(maximum)
	if err != nil {
		return result, err
	}
	if low.Cmp(high) > 0 || high.Cmp(total) > 0 {
		return result, errors.New("economic funding income bounds exceed original amount")
	}
	return economicFundingRange{Amount: amount, MinimumIncome: minimum, MaximumIncome: maximum, MinimumNonIncome: new(big.Int).Sub(total, high).String(), MaximumNonIncome: new(big.Int).Sub(total, low).String(), Complete: complete || total.Sign() == 0}, nil
}

// Zero is explicit and complete even when no nonzero source has been observed.
func zeroEconomicFunding() economicFundingRange {
	return economicFundingRange{Amount: "0", MinimumIncome: "0", MaximumIncome: "0", MinimumNonIncome: "0", MaximumNonIncome: "0", Complete: true}
}

// The complementary bounds cannot be independently widened or resealed.
func (self economicFundingRange) validate() error {
	expected, err := newEconomicFundingRange(self.Amount, self.MinimumIncome, self.MaximumIncome, self.Complete)
	if err != nil {
		return err
	}
	if expected != self {
		return errors.New("economic funding lost its exact complementary source bounds")
	}
	return nil
}

// Addition combines distinct original sources, never repeated finalizations.
func (self economicFundingRange) add(other economicFundingRange) (economicFundingRange, error) {
	if err := errors.Join(self.validate(), other.validate()); err != nil {
		return economicFundingRange{}, err
	}
	total, err := economicConservationSum(self.Amount, other.Amount)
	if err != nil {
		return economicFundingRange{}, err
	}
	low, err := economicConservationSum(self.MinimumIncome, other.MinimumIncome)
	if err != nil {
		return economicFundingRange{}, err
	}
	high, err := economicConservationSum(self.MaximumIncome, other.MaximumIncome)
	if err != nil {
		return economicFundingRange{}, err
	}
	return newEconomicFundingRange(total, low, high, self.Complete && other.Complete)
}

// For any subset of a fungible balance, at most the excluded amount may have
// consumed income. No order or proportional coloring follows from an amount.
func (self economicFundingRange) subset(amount string) (economicFundingRange, error) {
	if err := self.validate(); err != nil {
		return economicFundingRange{}, err
	}
	total, err := monitorEconomicInteger(self.Amount)
	if err != nil {
		return economicFundingRange{}, err
	}
	selected, err := monitorEconomicInteger(amount)
	if err != nil {
		return economicFundingRange{}, err
	}
	if selected.Cmp(total) > 0 {
		return economicFundingRange{}, errors.New("economic funding subset exceeds its original obligation")
	}
	low, err := monitorEconomicInteger(self.MinimumIncome)
	if err != nil {
		return economicFundingRange{}, err
	}
	high, err := monitorEconomicInteger(self.MaximumIncome)
	if err != nil {
		return economicFundingRange{}, err
	}
	low.Sub(low, new(big.Int).Sub(total, selected))
	if low.Sign() < 0 {
		low.SetInt64(0)
	}
	if high.Cmp(selected) > 0 {
		high.Set(selected)
	}
	return newEconomicFundingRange(amount, low.String(), high.String(), self.Complete)
}

// Called only with a capture already reconciled against retained execution.
// The four source categories stay separate even when the total equals income.
func economicFundingForCapture(capture economicConservationCapture) (economicFundingCapture, error) {
	result := economicFundingCapture{Id: capture.Id, Pool: capture.Event.Values["noId"], Epoch: capture.Event.Values["epoch"], Receipt: capture.Event.ReceiptHash}
	amount := capture.Event.Values["amount"]
	unknown, err := newEconomicFundingRange(amount, "0", amount, false)
	if err != nil {
		return result, err
	}
	result.Funding = unknown
	if !capture.causalComplete() {
		return result, nil
	}
	effects := capture.PrincipalEffects
	if effects.Captured != amount || effects.ReceiptHash != result.Receipt || effects.After != "0" {
		return result, errors.New("economic capture funding changed its original whole-stock receipt")
	}
	nonIncome, err := economicConservationSum(effects.OpeningStock, effects.Deposits)
	if err != nil {
		return result, err
	}
	nonIncome, err = economicConservationSum(nonIncome, effects.Refunds)
	if err != nil {
		return result, err
	}
	beforeWithdrawal, err := economicConservationSum(nonIncome, effects.LiquidEarnings)
	if err != nil {
		return result, err
	}
	before, err := monitorEconomicInteger(beforeWithdrawal)
	if err != nil {
		return result, err
	}
	withdrawn, err := monitorEconomicInteger(effects.Withdrawals)
	if err != nil {
		return result, err
	}
	if before.Sub(before, withdrawn).Sign() < 0 || before.String() != amount {
		return result, errors.New("economic original capture source equation differs")
	}
	original, err := newEconomicFundingRange(beforeWithdrawal, effects.LiquidEarnings, effects.LiquidEarnings, true)
	if err != nil {
		return result, err
	}
	result.Funding, err = original.subset(amount)
	if err != nil {
		return result, err
	}
	result.EffectsHash = rootObjectHash(effects)
	result.Opening, result.Deposits, result.Withdrawals, result.Refunds = &effects.OpeningStock, &effects.Deposits, &effects.Withdrawals, &effects.Refunds
	return result, nil
}

// Active facts are indexed once. Admitted cold facts are direct lookups, so a
// carry spanning many archived epochs does not rewalk historical payloads.
type economicFundingResolver struct {
	ctx      context.Context
	state    *economicConservationState
	active   map[string]economicConservationEntitlement
	captures map[string]economicConservationCapture
	claims   map[string]economicConservationClaim
	resolved map[string]economicFundingEntitlement
	visiting map[string]bool
}

// Build a bounded active index once; duplicate identities cannot select a winner.
func newEconomicFundingResolver(ctx context.Context, state *economicConservationState) (*economicFundingResolver, error) {
	if ctx == nil || state == nil {
		return nil, errors.New("economic funding requires its original lifecycle owner")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &economicFundingResolver{ctx: ctx, state: state, active: map[string]economicConservationEntitlement{}, captures: map[string]economicConservationCapture{}, claims: map[string]economicConservationClaim{}, resolved: map[string]economicFundingEntitlement{}, visiting: map[string]bool{}}
	for _, value := range state.Entitlements {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, exists := result.active[value.Id]; exists {
			return nil, errors.New("economic funding repeated an original entitlement identity")
		}
		result.active[value.Id] = value
	}
	for _, value := range state.Captures {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, exists := result.captures[value.Id]; exists {
			return nil, errors.New("economic funding repeated an original capture identity")
		}
		result.captures[value.Id] = value
	}
	for _, value := range state.Claims {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, exists := result.claims[value.Id]; exists {
			return nil, errors.New("economic funding repeated an original claim identity")
		}
		result.claims[value.Id] = value
	}
	return result, nil
}

// Cold records are admitted original evidence, not caller-supplied projections.
func (self *economicFundingResolver) capture(id string) (economicFundingCapture, error) {
	if err := self.ctx.Err(); err != nil {
		return economicFundingCapture{}, err
	}
	if value, ok := self.captures[id]; ok {
		return economicFundingForCapture(value)
	}
	if view := self.state.archiveView; view != nil && view.funding != nil {
		if value, ok := view.funding.captures[id]; ok {
			return value, nil
		}
	}
	return economicFundingCapture{}, errors.New("economic funding lost its original capture composition")
}

// Follow original source epochs and pool identity with cycle refusal and memoized
// cold predecessors. A delayed old root can consume a newer epoch's carry;
// original receipt admission establishes that order, never the epoch number.
func (self *economicFundingResolver) entitlement(id string) (economicFundingEntitlement, error) {
	if err := self.ctx.Err(); err != nil {
		return economicFundingEntitlement{}, err
	}
	if value, ok := self.resolved[id]; ok {
		return value, nil
	}
	if self.visiting[id] {
		return economicFundingEntitlement{}, errors.New("economic funding carry repeats an original obligation")
	}
	record, ok := self.active[id]
	if !ok && self.state.archiveView != nil && self.state.archiveView.funding != nil {
		value, exists := self.state.archiveView.funding.entitlements[id]
		if exists {
			return value, nil
		}
	}
	if !ok {
		return economicFundingEntitlement{}, errors.New("economic funding lost an original entitlement")
	}
	self.visiting[id] = true
	defer delete(self.visiting, id)
	value := economicFundingEntitlement{Id: id, Pool: record.PoolId, Epoch: record.Epoch, FundingHash: economicEntitlementFundingHash(record), Funding: zeroEconomicFunding()}
	for _, source := range record.Sources {
		if err := self.ctx.Err(); err != nil {
			return value, err
		}
		var part economicFundingRange
		var err error
		switch source.Kind {
		case "capture":
			capture, readErr := self.capture(source.Id)
			if readErr != nil {
				return value, readErr
			}
			if capture.Pool != record.PoolId || capture.Epoch != record.Epoch || capture.Funding.Amount != source.Amount {
				return value, errors.New("economic capture funding moved original pool or epoch")
			}
			part = capture.Funding
		case "root-missed", "expired-entitlement":
			prior, readErr := self.entitlement(source.Id)
			if readErr != nil {
				return value, readErr
			}
			from, fromErr := monitorEconomicInteger(prior.Epoch)
			to, toErr := monitorEconomicInteger(record.Epoch)
			if fromErr != nil || toErr != nil || prior.Pool != record.PoolId || from.Cmp(to) == 0 {
				return value, errors.New("economic funding composition moved original source epoch or pool")
			}
			part, err = prior.Funding.subset(source.Amount)
		case "opening-funding-unattributed", "opening-carry-unattributed":
			part, err = newEconomicFundingRange(source.Amount, "0", source.Amount, false)
		default:
			return value, errors.New("economic funding source kind is unrecognized")
		}
		if err != nil {
			return value, err
		}
		value.Funding, err = value.Funding.add(part)
		if err != nil {
			return value, err
		}
	}
	expected := record.Funded
	if record.Total != nil {
		expected = *record.Total
	}
	// Legacy opening liabilities have no original source receipt. Preserve an
	// unknown bound rather than infer a zero source from an empty source list.
	if len(record.Sources) == 0 && expected != "0" {
		var err error
		value.Funding, err = newEconomicFundingRange(expected, "0", expected, false)
		if err != nil {
			return value, err
		}
	}
	if value.Funding.Amount != expected {
		return value, errors.New("economic funding composition differs from original obligation")
	}
	self.resolved[id] = value
	return value, nil
}

// An accepted leaf consumes a subset of its original finalized obligation.
func (self *economicFundingResolver) claim(value economicConservationClaim) (economicFundingRange, error) {
	if err := self.ctx.Err(); err != nil {
		return economicFundingRange{}, err
	}
	amount := value.Event.Values["amount"]
	if value.Status != "original-entitlement-observed" {
		return newEconomicFundingRange(amount, "0", amount, false)
	}
	prior, err := self.entitlement(value.Entitlement)
	if err != nil {
		return economicFundingRange{}, err
	}
	return prior.Funding.subset(amount)
}

// A transfer consumes the whole retained credit set. Missing opening evidence
// remains unknown and cannot be parsed as an explicit zero balance.
func (self *economicFundingResolver) payment(value economicConservationPayment) (economicFundingRange, error) {
	selected := newEconomicFundingSelection(nil)
	if err := selected.payment(self, value); err != nil {
		return economicFundingRange{}, err
	}
	return selected.funding()
}

// Summary callers already hold the admitted archive. Cold counters are built
// once from removed original facts; active facts are visited once per sample.
func (self *economicConservationState) fundingSummary(ctx context.Context) (*economicConservationFundingSummary, error) {
	if err := self.fundingOwner(ctx); err != nil {
		return nil, err
	}
	resolver, err := newEconomicFundingResolver(ctx, self)
	if err != nil {
		return nil, err
	}
	result := &economicConservationFundingSummary{Captured: zeroEconomicFunding(), Accepted: zeroEconomicFunding(), Paid: zeroEconomicFunding(), ActiveEntitlements: []economicFundingEntitlement{}, Authority: "original-native-capture-and-receipt-funding; fungible-source-bounds; no-capital-subsidy-authority"}
	var acceptedIndex, paidIndex *economicFundingSelectionIndex
	if self.archiveView != nil && self.archiveView.funding != nil {
		cold := self.archiveView.funding
		result.Captured = cold.captured
		acceptedIndex, paidIndex = cold.acceptedSelection, cold.paidSelection
		result.OriginalCaptures, result.OriginalClaims, result.OriginalPayments = cold.captureCount, cold.claimCount, cold.paymentCount
	}
	accepted, paid := newEconomicFundingSelection(acceptedIndex), newEconomicFundingSelection(paidIndex)
	for _, capture := range self.Captures {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, err := economicFundingForCapture(capture)
		if err != nil {
			return nil, err
		}
		result.Captured, err = result.Captured.add(value.Funding)
		if err != nil {
			return nil, err
		}
		result.OriginalCaptures++
	}
	for _, record := range self.Entitlements {
		value, err := resolver.entitlement(record.Id)
		if err != nil {
			return nil, err
		}
		result.ActiveEntitlements = append(result.ActiveEntitlements, value)
	}
	for _, claim := range self.Claims {
		if err := accepted.claim(resolver, claim); err != nil {
			return nil, err
		}
		result.OriginalClaims++
	}
	for _, payment := range self.Payments {
		if err := paid.payment(resolver, payment); err != nil {
			return nil, err
		}
		result.OriginalPayments++
	}
	result.Accepted, err = accepted.funding()
	if err != nil {
		return nil, err
	}
	result.Paid, err = paid.funding()
	if err != nil {
		return nil, err
	}
	if result.Accepted.MinimumNonIncome != "0" || result.Paid.MinimumNonIncome != "0" {
		value := false
		result.NoNonIncomeProviderCredit = &value
	} else if result.OriginalClaims != 0 && result.Accepted.Complete && result.Accepted.MaximumNonIncome == "0" && result.Paid.Complete && result.Paid.MaximumNonIncome == "0" {
		value := true
		result.NoNonIncomeProviderCredit = &value
	}
	if self.archiveView != nil && self.archiveView.fundingWork != nil {
		self.archiveView.fundingWork(ctx)
	}
	if err := self.fundingOwner(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// Public projection checks the original held owners before and after all
// arithmetic, not on every map lookup. Cold admission uses its private owner
// context here and still checks the complete held set before it is published.
func (self *economicConservationState) fundingOwner(ctx context.Context) error {
	if ctx == nil || self == nil {
		return errors.New("economic funding lacks original owner context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if self.archiveView != nil {
		if err := self.archiveView.checkAdmission(); err != nil {
			return err
		}
	}
	return ctx.Err()
}
