// Claims select disjoint sinks in the original funding forest. An obligation
// can carry its remainder to one successor, but cannot earn that remainder
// again. Keep the shared capacity when several selected leaves exhaust it.
package main

import (
	"context"
	"encoding/json"
	"errors"
)

// Income and NonIncome are the maximum units still available at this node
// after selections in its predecessors. They describe separate extremal flows,
// not a simultaneous coloring of fungible stake.
type economicFundingFlowNode struct {
	FundingHash string
	Amount      string
	Selected    string
	Income      string
	NonIncome   string
	Parent      string
	Carry       string
}

// This private index is derived only during original archive admission. It is
// never serialized into a checkpoint or accepted in place of original receipts.
type economicFundingSelectionIndex struct {
	nodes            map[string]economicFundingFlowNode
	claims           map[string]bool
	amount           string
	maximumIncome    string
	maximumNonIncome string
	complete         bool
}

// A sample overlays only touched nodes. In particular, a new partial payment
// neither copies nor scans all previously retired claims and source payloads.
type economicFundingSelection struct {
	base *economicFundingSelectionIndex
	next economicFundingSelectionIndex
}

func newEconomicFundingSelection(base *economicFundingSelectionIndex) *economicFundingSelection {
	next := economicFundingSelectionIndex{nodes: map[string]economicFundingFlowNode{}, claims: map[string]bool{}, amount: "0", maximumIncome: "0", maximumNonIncome: "0", complete: true}
	if base != nil {
		next.amount, next.maximumIncome, next.maximumNonIncome, next.complete = base.amount, base.maximumIncome, base.maximumNonIncome, base.complete
	}
	return &economicFundingSelection{base: base, next: next}
}

func (self *economicFundingSelection) node(id string) (economicFundingFlowNode, bool) {
	if value, ok := self.next.nodes[id]; ok {
		return value, true
	}
	if self.base != nil {
		value, ok := self.base.nodes[id]
		return value, ok
	}
	return economicFundingFlowNode{}, false
}

// Replace one exact contribution without underflow or a signed amount on the
// wire. The selected maxima are accumulated across distinct sinks only.
func economicFundingReplace(total, before, after string) (string, error) {
	a, e1 := monitorEconomicInteger(total)
	b, e2 := monitorEconomicInteger(before)
	c, e3 := monitorEconomicInteger(after)
	if err := errors.Join(e1, e2, e3); err != nil {
		return "", err
	}
	if a.Sub(a, b).Sign() < 0 {
		return "", errors.New("economic funding flow removed unavailable original units")
	}
	a.Add(a, c)
	if a.BitLen() > 256 {
		return "", errors.New("economic funding flow exceeds uint256")
	}
	return a.String(), nil
}

// For one color, taking a selected unit now cannot reduce the maximum number
// selected over the entire forest: forwarding that same unit can select it at
// most once later. Of equally good flows, forward the largest remainder. This
// is a maximum flow on the single-successor carry forest, not a claim that the
// real transfer used income first. Run it independently for both colors.
func (self economicFundingFlowNode) color(available string) (used, carried string, err error) {
	input, e1 := monitorEconomicInteger(available)
	selected, e2 := monitorEconomicInteger(self.Selected)
	carry, e3 := monitorEconomicInteger(self.Carry)
	if err := errors.Join(e1, e2, e3); err != nil {
		return "", "", err
	}
	if selected.Cmp(input) > 0 {
		selected.Set(input)
	}
	input.Sub(input, selected)
	if input.Cmp(carry) > 0 {
		input.Set(carry)
	}
	return selected.String(), input.String(), nil
}

// Changes propagate only along the affected carry successors. A cold claim
// paid after its successor's claims still competes for the same original units.
func (self *economicFundingSelection) update(ctx context.Context, id string, value economicFundingFlowNode) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		before, ok := self.node(id)
		if !ok {
			return errors.New("economic funding flow lost its original obligation")
		}
		selected, err := economicConservationSum(value.Selected, value.Carry)
		if err != nil {
			return err
		}
		used, err := monitorEconomicInteger(selected)
		total, totalErr := monitorEconomicInteger(value.Amount)
		if err != nil || totalErr != nil || used.Cmp(total) > 0 {
			return errors.Join(err, totalErr, errors.New("economic funding selections and carry exceed original obligation"))
		}
		oldIncome, oldIncomeCarry, err := before.color(before.Income)
		if err != nil {
			return err
		}
		newIncome, newIncomeCarry, err := value.color(value.Income)
		if err != nil {
			return err
		}
		oldNonIncome, oldNonIncomeCarry, err := before.color(before.NonIncome)
		if err != nil {
			return err
		}
		newNonIncome, newNonIncomeCarry, err := value.color(value.NonIncome)
		if err != nil {
			return err
		}
		self.next.maximumIncome, err = economicFundingReplace(self.next.maximumIncome, oldIncome, newIncome)
		if err != nil {
			return err
		}
		self.next.maximumNonIncome, err = economicFundingReplace(self.next.maximumNonIncome, oldNonIncome, newNonIncome)
		if err != nil {
			return err
		}
		self.next.nodes[id] = value
		if value.Parent == "" || oldIncomeCarry == newIncomeCarry && oldNonIncomeCarry == newNonIncomeCarry {
			return nil
		}
		id = value.Parent
		value, ok = self.node(id)
		if !ok {
			return errors.New("economic funding flow lost its original carry successor")
		}
		value.Income, err = economicFundingReplace(value.Income, oldIncomeCarry, newIncomeCarry)
		if err != nil {
			return err
		}
		value.NonIncome, err = economicFundingReplace(value.NonIncome, oldNonIncomeCarry, newNonIncomeCarry)
		if err != nil {
			return err
		}
	}
}

// Original entitlement resolution already checks source pool, epoch, amount
// and cycles. Build each node once; cold nodes preserve their exact funding hash.
func (self *economicFundingSelection) obligation(resolver *economicFundingResolver, id string) error {
	prior, err := resolver.entitlement(id)
	if err != nil {
		return err
	}
	if value, ok := self.node(id); ok {
		if value.FundingHash != prior.FundingHash || value.Amount != prior.Funding.Amount {
			return errors.New("economic funding flow changed its original source binding")
		}
		return nil
	}
	record, ok := resolver.active[id]
	if !ok && resolver.state.archiveView != nil {
		record, ok = resolver.state.archiveView.entitlements[id]
	}
	if !ok {
		return errors.New("economic funding flow lost its original source edges")
	}
	value := economicFundingFlowNode{FundingHash: prior.FundingHash, Amount: prior.Funding.Amount, Selected: "0", Income: "0", NonIncome: "0", Carry: "0"}
	for _, source := range record.Sources {
		if err := resolver.ctx.Err(); err != nil {
			return err
		}
		var income, nonIncome string
		switch source.Kind {
		case "root-missed", "expired-entitlement":
			if err := self.obligation(resolver, source.Id); err != nil {
				return err
			}
			predecessor, _ := self.node(source.Id)
			if predecessor.Parent != "" {
				return errors.New("economic funding flow spends an original carry twice")
			}
			predecessor.Parent, predecessor.Carry = id, source.Amount
			// Selected units and carried units must be disjoint. Compare their
			// sum directly rather than assigning either a source color.
			selected, err := economicConservationSum(predecessor.Selected, source.Amount)
			if err != nil {
				return err
			}
			bound, e1 := monitorEconomicInteger(selected)
			total, e2 := monitorEconomicInteger(predecessor.Amount)
			if e1 != nil || e2 != nil || bound.Cmp(total) > 0 {
				return errors.Join(e1, e2, errors.New("economic funding carry overlaps original selected claims"))
			}
			_, income, err = predecessor.color(predecessor.Income)
			if err != nil {
				return err
			}
			_, nonIncome, err = predecessor.color(predecessor.NonIncome)
			if err != nil {
				return err
			}
			self.next.nodes[source.Id] = predecessor
		case "capture":
			capture, err := resolver.capture(source.Id)
			if err != nil {
				return err
			}
			income, nonIncome = capture.Funding.MaximumIncome, capture.Funding.MaximumNonIncome
		case "opening-funding-unattributed", "opening-carry-unattributed":
			income, nonIncome = source.Amount, source.Amount
		default:
			return errors.New("economic funding flow has an unrecognized original source")
		}
		value.Income, err = economicConservationSum(value.Income, income)
		if err != nil {
			return err
		}
		value.NonIncome, err = economicConservationSum(value.NonIncome, nonIncome)
		if err != nil {
			return err
		}
	}
	if len(record.Sources) == 0 {
		value.Income, value.NonIncome = prior.Funding.MaximumIncome, prior.Funding.MaximumNonIncome
	}
	self.next.nodes[id] = value
	return nil
}

func (self *economicFundingSelection) unknown(amount string) error {
	var err error
	self.next.amount, err = economicConservationSum(self.next.amount, amount)
	if err != nil {
		return err
	}
	self.next.maximumIncome, err = economicConservationSum(self.next.maximumIncome, amount)
	if err != nil {
		return err
	}
	self.next.maximumNonIncome, err = economicConservationSum(self.next.maximumNonIncome, amount)
	self.next.complete = self.next.complete && amount == "0"
	return err
}

func (self *economicFundingSelection) claim(resolver *economicFundingResolver, claim economicConservationClaim) error {
	if err := resolver.ctx.Err(); err != nil {
		return err
	}
	if self.next.claims[claim.Id] || self.base != nil && self.base.claims[claim.Id] {
		return errors.New("economic funding selection repeats an original accepted claim")
	}
	self.next.claims[claim.Id] = true
	if claim.Status != "original-entitlement-observed" {
		return self.unknown(claim.Event.Values["amount"])
	}
	prior, err := resolver.entitlement(claim.Entitlement)
	if err != nil {
		return err
	}
	if err := self.obligation(resolver, claim.Entitlement); err != nil {
		return err
	}
	value, _ := self.node(claim.Entitlement)
	value.Selected, err = economicConservationSum(value.Selected, claim.Event.Values["amount"])
	if err != nil {
		return err
	}
	self.next.amount, err = economicConservationSum(self.next.amount, claim.Event.Values["amount"])
	if err != nil {
		return err
	}
	self.next.complete = self.next.complete && (prior.Funding.Complete || claim.Event.Values["amount"] == "0")
	return self.update(resolver.ctx, claim.Entitlement, value)
}

func (self *economicFundingSelection) payment(resolver *economicFundingResolver, payment economicConservationPayment) error {
	if err := resolver.ctx.Err(); err != nil {
		return err
	}
	if payment.Status != "aggregate-credit-observed" {
		return self.unknown(payment.Event.Values["amount"])
	}
	before := self.next.amount
	if err := self.unknown(payment.Credit.Opening); err != nil {
		return err
	}
	for _, id := range payment.Credit.Claims {
		claim, known := resolver.claims[id]
		if !known && resolver.state.archiveView != nil {
			claim, known = resolver.state.archiveView.claims[id]
		}
		if !known {
			return errors.New("economic paid funding lost its original accepted leaf")
		}
		if err := self.claim(resolver, claim); err != nil {
			return err
		}
	}
	amount, err := economicFundingReplace(self.next.amount, before, "0")
	if err != nil || amount != payment.Event.Values["amount"] {
		return errors.Join(err, errors.New("economic paid funding differs from original credit transfer"))
	}
	return nil
}

func (self *economicFundingSelection) funding() (economicFundingRange, error) {
	minimum, err := economicFundingReplace(self.next.amount, self.next.maximumNonIncome, "0")
	if err != nil {
		return economicFundingRange{}, err
	}
	return newEconomicFundingRange(self.next.amount, minimum, self.next.maximumIncome, self.next.complete)
}

// Charge changed private facts against the same admitted index budget. Existing
// nodes replace their old byte charge; each newly remembered claim is bounded.
func (self *economicConservationArchiveView) retainFundingSelection(ctx context.Context, selection *economicFundingSelection) (*economicFundingSelectionIndex, error) {
	index := selection.base
	if index == nil {
		index = &economicFundingSelectionIndex{nodes: map[string]economicFundingFlowNode{}, claims: map[string]bool{}}
	}
	entryLimit, byteLimit := self.resources.IndexEntries/2, self.resources.IndexBytes/2
	if self.claimBasisEntries > entryLimit || self.entries > entryLimit-self.claimBasisEntries || self.claimBasisBytes > byteLimit {
		return nil, errMonitorEconomicCapacity
	}
	entries, bytes := self.entries, self.bytes
	// Remove all replaced payload charges before adding their new values.
	// A shrinking node must not depend on map iteration order to make room
	// for a growing sibling during replay of the same original segment.
	for id := range selection.next.nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if previous, ok := index.nodes[id]; ok {
			raw, err := json.Marshal(previous)
			if err != nil {
				return nil, err
			}
			if uint64(len(raw)) > bytes {
				return nil, errors.New("economic funding flow lost its original index charge")
			}
			bytes -= uint64(len(raw))
		}
	}
	reserve := func(value any, fresh bool) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		amount := uint64(len(raw))
		if fresh {
			if entries >= entryLimit-self.claimBasisEntries {
				return errMonitorEconomicCapacity
			}
			entries++
			amount += 256
		}
		if bytes > byteLimit-self.claimBasisBytes || amount > byteLimit-self.claimBasisBytes-bytes {
			return errMonitorEconomicCapacity
		}
		bytes += amount
		return nil
	}
	for id, value := range selection.next.nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, exists := index.nodes[id]; exists {
			if err := reserve(value, false); err != nil {
				return nil, err
			}
		} else if err := reserve(struct {
			Id   string
			Node economicFundingFlowNode
		}{Id: id, Node: value}, true); err != nil {
			return nil, err
		}
	}
	for id := range selection.next.claims {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := reserve(id, true); err != nil {
			return nil, err
		}
	}
	for id, value := range selection.next.nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index.nodes[id] = value
	}
	for id := range selection.next.claims {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index.claims[id] = true
	}
	self.entries, self.bytes = entries, bytes
	index.amount, index.maximumIncome, index.maximumNonIncome, index.complete = selection.next.amount, selection.next.maximumIncome, selection.next.maximumNonIncome, selection.next.complete
	return index, ctx.Err()
}
