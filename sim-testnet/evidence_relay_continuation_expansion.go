//go:build linux || darwin

package main

// V5 is an explicit, finite funding revision. It doubles the aggregate relay
// reserve once; refreshes retain that same reserve and all historical debits.

import "errors"

// The version fixes both aggregate capacity and money. Legacy approvals retain
// their original 25.6 TAO reserve; v5 approves exactly 51.2 TAO, never a multiplier
// applied to a predecessor that could grow again on every recovery.
func (self *EvidenceRelayContinuation) reserveSpend() (Spend, error) {
	if self == nil {
		return Spend{}, errors.New("relay continuation reserve owner is absent")
	}
	fee, slots, err := self.feeTerms()
	if err != nil {
		return Spend{}, err
	}
	spend := self.OriginalReserve.Spend
	if evidenceRelayExpandedFunding(self.Schema) {
		spend.EVMGasWei = multiplyUint64Decimal(evidenceRelayContinuationGas*fee, slots)
	}
	return spend, nil
}

// Only an explicit capture option can introduce v5. Ordinary captures retain
// the already approved wire version's capacity and cannot implicitly expand it.
func evidenceRelayContinuationCaptureSchema(base *SetupPlan, requestedSlots uint64, pin *EvidenceRelayContinuation) (string, error) {
	if base == nil {
		return "", errors.New("relay continuation source approval is absent")
	}
	schema := evidenceRelayContinuationSchema
	if prior := base.EvidenceRelayContinuation; prior != nil {
		switch prior.Schema {
		case evidenceRelayContinuationSchema, evidenceRelayContinuationRefreshSchema:
			schema = evidenceRelayContinuationRefreshSchema
		case evidenceRelayContinuationExpansionSchema, evidenceRelayContinuationSourceExpansionSchema:
			schema = prior.Schema
		default:
			return "", errors.New("relay refresh cannot change an older approved fee version")
		}
	}
	if requestedSlots != 0 {
		if requestedSlots != evidenceRelayContinuationExpandedSlots || base.EvidenceRelayContinuation == nil {
			return "", errors.New("relay funding revision requires an existing continuation and exactly 2048 aggregate slots")
		}
		if schema != evidenceRelayContinuationSourceExpansionSchema {
			schema = evidenceRelayContinuationExpansionSchema
		}
	}
	if pin != nil {
		if requestedSlots != 0 {
			return "", errors.New("relay import cannot replace its approved slot capacity")
		}
		if evidenceRelayExpandedFunding(pin.Schema) && base.EvidenceRelayContinuation != nil {
			schema = pin.Schema
		}
		if pin.Schema == "urnetwork-sim-evidence-relay-continuation-v2" && base.EvidenceRelayContinuation == nil {
			schema = pin.Schema
		}
		if pin.Schema != schema {
			return "", errors.New("relay import changed its approved continuation version")
		}
	}
	return schema, nil
}
