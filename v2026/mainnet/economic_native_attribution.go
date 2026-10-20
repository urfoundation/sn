// Recipient projections preserve the original execution's amounts and identity
// for later custody reconciliation. They add no runtime or economic authority,
// and do not change the aggregate/completion hashes written by older owners.
package main

import (
	"errors"
	"math/big"
)

const nativeExecutionEffectsSchema = "urnetwork-native-recipient-effects-v1"
const nativeTreasuryEffectsSchema = "urnetwork-native-recipient-effects-v2"

// Gross is earned entitlement, including rewards locked into collateral. Only
// the original miner-credit branch has liquid/collateral values; owner recycle
// is neither a payment to the owner nor a new reserve or provider liability.
type nativeExecutionEffect struct {
	Treasury   *nativeTreasuryRecipientEffect `json:"approved_treasury,omitempty"`
	Ordinal    uint64                         `json:"observation_ordinal"`
	Recipient  nativeExecutionRecipient       `json:"recipient"`
	Branch     string                         `json:"branch"`
	Provider   bool                           `json:"approved_provider"`
	Gross      string                         `json:"gross_alpha"`
	Liquid     string                         `json:"liquid_alpha"`
	Collateral string                         `json:"collateral_alpha"`
	Recycled   string                         `json:"recycled_alpha"`
}

// The earning occurrence is the actual Initialization trace and event at this
// parent/child boundary. An EVM height, timestamp or later payout epoch cannot
// replace it. AggregateHash excludes only the subsequently attached producer
// authority/finality hashes; the immutable admission, job and trace stay bound.
type nativeExecutionEffectProjection struct {
	Schema          string                   `json:"schema"`
	Parent          economicEmissionBoundary `json:"parent"`
	Boundary        economicEmissionBoundary `json:"boundary"`
	AggregateHash   string                   `json:"aggregate_basis_hash"`
	AdmissionHash   string                   `json:"admission_hash"`
	JobHash         string                   `json:"job_hash"`
	TraceHash       string                   `json:"trace_hash"`
	EventIndex      *uint64                  `json:"event_index"`
	EmissionOrdinal *uint64                  `json:"emission_observation_ordinal"`
	Effects         []nativeExecutionEffect  `json:"effects"`
	ContentHash     string                   `json:"content_hash"`
}

func nativeExecutionEffectBasis(outcome nativeExecutionOutcome) string {
	outcome.ProducerAuthorityHash, outcome.FinalityProofHash = "", ""
	return outcome.hash()
}

func (self nativeExecutionEffectProjection) hash() string {
	self.ContentHash = ""
	return rootObjectHash(self)
}

func newNativeExecutionEffects(parent economicEmissionBoundary, outcome nativeExecutionOutcome, eventIndex, ordinal *uint64, effects []nativeExecutionEffect) *nativeExecutionEffectProjection {
	result := &nativeExecutionEffectProjection{Schema: nativeExecutionEffectsSchema, Parent: parent, Boundary: outcome.Boundary, AggregateHash: nativeExecutionEffectBasis(outcome), AdmissionHash: outcome.AdmissionHash, JobHash: outcome.JobHash, TraceHash: outcome.TraceHash, EventIndex: eventIndex, EmissionOrdinal: ordinal, Effects: append([]nativeExecutionEffect{}, effects...)}
	if outcome.Treasury != nil {
		result.Schema = nativeTreasuryEffectsSchema
	}
	result.ContentHash = result.hash()
	return result
}

// Hashes detect changes; callers still have to obtain the outcome from the
// admitted replay. This check never turns a self-sealed external JSON document
// into execution evidence. A missing old projection is unknown, not zero.
func (self *nativeExecutionEffectProjection) validate(outcome nativeExecutionOutcome) error {
	if self == nil {
		return errors.New("native recipient projection is unavailable; replay the original retained job")
	}
	expectedSchema := nativeExecutionEffectsSchema
	if outcome.Treasury != nil {
		expectedSchema = nativeTreasuryEffectsSchema
	}
	if err := outcome.Treasury.validate(); err != nil {
		return err
	}
	if self.Schema != expectedSchema || self.ContentHash != self.hash() || !outcome.AmountsAuthenticated || outcome.ContentHash != outcome.hash() || self.Boundary != outcome.Boundary || self.Parent.Number+1 != self.Boundary.Number || !rootCanonicalHash(self.Parent.Hash) || self.AggregateHash != nativeExecutionEffectBasis(outcome) || self.AdmissionHash != outcome.AdmissionHash || self.JobHash != outcome.JobHash || self.TraceHash != outcome.TraceHash || len(self.Effects) > rootCensusLimit || len(outcome.Recipients) > rootCensusLimit {
		return errors.New("native recipient projection differs from original execution identity")
	}
	if (self.EventIndex == nil) != (self.EmissionOrdinal == nil) || self.EmissionOrdinal != nil && *self.EmissionOrdinal == 0 || self.EventIndex == nil && (len(self.Effects) != 0 || len(outcome.Recipients) != 0 || outcome.MinerAllocation != "0") {
		return errors.New("native recipient projection omitted its original emission occurrence")
	}
	identities := make(map[uint16]nativeExecutionRecipient, len(outcome.Recipients))
	for _, recipient := range outcome.Recipients {
		if _, exists := identities[recipient.Uid]; exists {
			return errors.New("native projection original UID is repeated")
		}
		identities[recipient.Uid] = recipient
	}
	provider, owner, residual, collateral := new(big.Int), new(big.Int), new(big.Int), new(big.Int)
	treasury := cloneNativeTreasuryAmounts(outcome.Treasury)
	if treasury != nil {
		treasury.Gross, treasury.Liquid, treasury.Collateral = "0", "0", "0"
	}
	used := make(map[uint16]bool, len(self.Effects))
	var previous uint64
	if self.EmissionOrdinal != nil {
		previous = *self.EmissionOrdinal
	}
	for _, effect := range self.Effects {
		identity, exists := identities[effect.Recipient.Uid]
		if !exists || effect.Recipient != identity || !rootCanonicalHash(identity.Hotkey) || !rootCanonicalHash(identity.Coldkey) || identity.Registered > self.Boundary.Number || used[identity.Uid] || effect.Ordinal <= previous {
			return errors.New("native recipient amount changed original generation or occurrence")
		}
		used[identity.Uid], previous = true, effect.Ordinal
		gross, e1 := monitorEconomicInteger(effect.Gross)
		liquid, e2 := monitorEconomicInteger(effect.Liquid)
		locked, e3 := monitorEconomicInteger(effect.Collateral)
		recycled, e4 := monitorEconomicInteger(effect.Recycled)
		if err := errors.Join(e1, e2, e3, e4); err != nil {
			return err
		}
		if !gross.IsUint64() || !liquid.IsUint64() || !locked.IsUint64() || !recycled.IsUint64() {
			return errors.New("native recipient projection amount exceeds original u64")
		}
		if effect.Treasury != nil {
			if treasury == nil {
				return errors.New("legacy native projection cannot enroll treasury credit")
			}
			if err := effect.Treasury.validate(treasury.Policy, effect); err != nil {
				return err
			}
		} else if treasury != nil && nativeTreasuryRecipient(treasury.Policy, effect.Recipient) {
			return errors.New("native treasury role was omitted or relabelled")
		}
		switch effect.Branch {
		case "native-miner-credit":
			if recycled.Sign() != 0 || new(big.Int).Add(liquid, locked).Cmp(gross) != 0 {
				return errors.New("native recipient liquid and collateral do not conserve gross")
			}
			collateral.Add(collateral, locked)
			if effect.Provider {
				provider.Add(provider, gross)
			} else if effect.Treasury != nil {
				for _, item := range []struct {
					target *string
					value  string
				}{
					{target: &treasury.Gross, value: effect.Gross}, {target: &treasury.Liquid, value: effect.Liquid}, {target: &treasury.Collateral, value: effect.Collateral},
				} {
					if err := nativeExecutionAdd(item.target, item.value, false); err != nil {
						return err
					}
				}
			} else {
				residual.Add(residual, gross)
			}
		case "native-owner-recycle":
			if effect.Provider || effect.Treasury != nil || liquid.Sign() != 0 || locked.Sign() != 0 || recycled.Cmp(gross) != 0 {
				return errors.New("native recycle cannot create provider, collateral or reserve credit")
			}
			owner.Add(owner, recycled)
		default:
			return errors.New("native recipient projection branch is unsupported")
		}
	}
	if treasury != nil && (treasury.Gross != outcome.Treasury.Gross || treasury.Liquid != outcome.Treasury.Liquid || treasury.Collateral != outcome.Treasury.Collateral) {
		return errors.New("native treasury effects do not reproduce original aggregates")
	}
	if treasury != nil && self.EventIndex != nil {
		for _, recipient := range treasury.Policy.Recipients {
			identity, exists := identities[recipient.Uid]
			if !exists || !nativeTreasuryRecipient(treasury.Policy, identity) || !used[recipient.Uid] {
				return errors.New("native treasury projection omitted an approved execution generation")
			}
		}
	}
	if provider.String() != outcome.ProviderEntitlement || owner.String() != outcome.OwnerRecycled || residual.String() != outcome.ResidualEntitlement || collateral.String() != outcome.CollateralCapture {
		return errors.New("native recipient amounts do not reproduce original aggregates")
	}
	return nil
}
