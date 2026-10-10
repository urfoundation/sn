// A vault capture joins one original native stake reduction to one successful
// contract receipt. Complete interval causes explain stock without earning it
// again; missing causes remain unknown even when observed amounts agree.
package main

import (
	"context"
	"errors"
	"math/big"
	"reflect"
)

const economicCaptureEffectsSchema = "urnetwork-original-native-vault-capture-v1"

// The cursor includes the original mutation ordinal. Two captures in one
// native block cannot borrow each other's effects or spend initialization twice.
type economicCaptureEffectCursor struct {
	Boundary economicEmissionBoundary `json:"original_native_block"`
	Ordinal  uint64                   `json:"original_mutation_ordinal"`
}

type economicCaptureEffectSource struct {
	Boundary   economicEmissionBoundary `json:"original_native_block"`
	Projection string                   `json:"principal_projection_hash"`
	Outcome    string                   `json:"original_outcome_hash"`
	Job        string                   `json:"original_job_hash"`
}

// OpeningStock is the interval's original stock, not all principal ultimately
// paid to a provider. Withdrawals/refunds remain separate signed flow terms;
// this certificate makes no FIFO allocation among interchangeable stake units.
type economicConservationCaptureEffects struct {
	Schema          string                        `json:"schema"`
	CaptureId       string                        `json:"original_capture_id"`
	Query           historicalPrincipalQuery      `json:"original_pool_identity"`
	From            economicCaptureEffectCursor   `json:"after_original_cursor"`
	Through         economicCaptureEffectCursor   `json:"through_original_cursor"`
	PreviousCapture string                        `json:"previous_original_capture_id,omitempty"`
	Sources         []economicCaptureEffectSource `json:"original_executions"`
	ExtrinsicIndex  uint32                        `json:"original_extrinsic_index"`
	ExtrinsicHash   string                        `json:"original_extrinsic_blake2b_256"`
	TransactionHash string                        `json:"original_evm_transaction_hash"`
	ReceiptHash     string                        `json:"original_receipt_hash"`
	OpeningStock    string                        `json:"opening_stock_alpha"`
	Deposits        string                        `json:"deposits_alpha"`
	Withdrawals     string                        `json:"withdrawals_alpha"`
	Refunds         string                        `json:"refunds_alpha"`
	LiquidEarnings  string                        `json:"native_liquid_earnings_alpha"`
	Captured        string                        `json:"captured_stock_alpha"`
	After           string                        `json:"stock_after_capture_alpha"`
}

// No elapsed-height estimate fills a missing block or an absent API result.
// A nil result is incomplete evidence, not a zero-amount certificate.
func deriveEconomicCaptureEffects(ctx context.Context, policy economicConservationPolicy, capture economicConservationCapture, prior *economicConservationCaptureEffects, query historicalPrincipalQuery, values map[uint64]economicConservationPrincipalExecution) (*economicConservationCaptureEffects, error) {
	return deriveEconomicCaptureEffectsLookup(ctx, policy, capture, prior, query, func(number uint64) (economicConservationPrincipalExecution, bool, error) {
		value, found := values[number]
		return value, found, nil
	})
}

// Production walks one authenticated cold segment at a time. The map adapter
// above retains the original arithmetic fixture API without loading an archive.
func deriveEconomicCaptureEffectsLookup(ctx context.Context, policy economicConservationPolicy, capture economicConservationCapture, prior *economicConservationCaptureEffects, query historicalPrincipalQuery, lookup func(uint64) (economicConservationPrincipalExecution, bool, error)) (*economicConservationCaptureEffects, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	authority := policy.Native.Observation.Execution.Principal
	if authority == nil || authority.Effects == nil || capture.Native == nil || capture.KnownLiquidAlpha == nil || capture.Event.CaptureIdentity == nil {
		return nil, nil
	}
	identity := capture.Event.CaptureIdentity
	if err := identity.validate(policy.Vault, capture.Event); err != nil {
		return nil, err
	}
	if identity.PoolHotkey != economicNativeFeeHash(query.Hotkey) || identity.Coldkey != economicNativeFeeHash(query.Coldkey) || identity.Netuid != query.Netuid {
		return nil, nil
	}
	from := economicCaptureEffectCursor{Boundary: authority.Parent}
	opening, previousCapture := "", ""
	if prior != nil {
		from, opening, previousCapture = prior.Through, prior.After, prior.CaptureId
	}
	if from.Boundary.Number > capture.Native.Number || from.Boundary.Number == capture.Native.Number && from.Boundary.Hash != capture.Native.Hash {
		return nil, errors.New("economic capture causal interval moved behind original progress")
	}
	result := &economicConservationCaptureEffects{Schema: economicCaptureEffectsSchema, CaptureId: capture.Id, Query: query, From: from, PreviousCapture: previousCapture, Sources: []economicCaptureEffectSource{}, Deposits: "0", Withdrawals: "0", Refunds: "0", LiquidEarnings: "0", Captured: capture.Event.Values["amount"], ReceiptHash: capture.Event.ReceiptHash}
	first := from.Boundary.Number + 1
	if from.Ordinal != 0 {
		first = from.Boundary.Number
	}
	previous := from.Boundary
	current := opening
	matched := false
	for number := first; number <= capture.Native.Number; number++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, exists, err := lookup(number)
		if err != nil {
			return nil, err
		}
		if !exists || number != first && value.Projection.Parent != previous || number == first && from.Ordinal == 0 && value.Projection.Parent != from.Boundary || number == first && from.Ordinal != 0 && value.Projection.Boundary != from.Boundary {
			return nil, nil
		}
		if number == capture.Native.Number && value.Projection.Boundary != *capture.Native {
			return nil, errors.New("economic capture changed original executed native block")
		}
		reconciled, err := value.Projection.reconcile(value.Outcome)
		if err != nil {
			return nil, err
		}
		var pool *nativePrincipalPoolEffects
		for index := range reconciled.Pools {
			if reconciled.Pools[index].Query == query {
				pool = &reconciled.Pools[index]
				break
			}
		}
		if pool == nil || !pool.Complete || pool.Before == nil || pool.After == nil {
			return nil, nil
		}
		if number == first && from.Ordinal == 0 {
			current = *pool.Before
			opening = current
		} else if number != first && current != *pool.Before {
			return nil, errors.New("economic capture lost original inter-block stock continuity")
		}
		result.Sources = append(result.Sources, economicCaptureEffectSource{Boundary: value.Projection.Boundary, Projection: value.Projection.ContentHash, Outcome: value.Outcome.ContentHash, Job: value.Projection.JobHash})
		for _, effect := range value.Projection.Effects {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if effect.Query != query || number == from.Boundary.Number && effect.Original.Ordinal <= from.Ordinal {
				continue
			}
			if effect.Before != current {
				return nil, nil
			}
			current = effect.After
			if effect.Kind == "vault-capture" {
				// Any intervening unobserved movement is a missing receipt, even
				// when it netted to zero or shares a later transaction's amount.
				if matched || number != capture.Native.Number || effect.EvmTransactionHash != capture.Event.TransactionHash || effect.ExtrinsicIndex == nil || effect.Amount != result.Captured || effect.Before != result.Captured || effect.After != "0" {
					return nil, nil
				}
				result.Through = economicCaptureEffectCursor{Boundary: value.Projection.Boundary, Ordinal: effect.Original.Ordinal}
				result.ExtrinsicIndex, result.ExtrinsicHash = *effect.ExtrinsicIndex, effect.ExtrinsicHash
				result.TransactionHash, result.After = effect.EvmTransactionHash, effect.After
				matched = true
				break
			}
			var target *string
			switch effect.Kind {
			case "deposit":
				target = &result.Deposits
			case "withdrawal":
				target = &result.Withdrawals
			case "refund":
				target = &result.Refunds
			case "earning":
				target = &result.LiquidEarnings
			case "support":
				continue
			default:
				return nil, nil
			}
			*target, err = economicConservationSum(*target, effect.Amount)
			if err != nil {
				return nil, err
			}
		}
		if !matched && current != *pool.After {
			return nil, nil
		}
		previous = value.Projection.Boundary
	}
	if !matched || result.LiquidEarnings != *capture.KnownLiquidAlpha {
		return nil, nil
	}
	result.OpeningStock = opening
	stock, err := monitorEconomicInteger(opening)
	if err != nil {
		return nil, err
	}
	for _, term := range []struct {
		value string
		sign  int64
	}{{value: result.Deposits, sign: 1}, {value: result.Refunds, sign: 1}, {value: result.Withdrawals, sign: -1}, {value: result.LiquidEarnings, sign: 1}} {
		amount, err := monitorEconomicInteger(term.value)
		if err != nil {
			return nil, err
		}
		stock.Add(stock, new(big.Int).Mul(amount, big.NewInt(term.sign)))
	}
	if stock.String() != result.Captured {
		return nil, nil
	}
	return result, ctx.Err()
}

// The original opening parent must commit the vault's exact starting block.
// A later mapped event cannot repair an unknown opening boundary.
func (self *economicConservationState) reconcileCaptureEffects(ctx context.Context, policy economicConservationPolicy, check bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lookup, err := self.captureExecutionLookup(ctx)
	if err != nil {
		return err
	}
	opening, err := self.principalSummary(policy)
	if err != nil {
		return err
	}
	joinedOpening := opening != nil && opening.EvmHash == policy.Vault.From.Hash && opening.OpeningAlpha != nil
	previous := map[string]*economicConservationCaptureEffects{}
	blocked := map[string]bool{}
	if self.archiveView != nil {
		for pool, value := range self.archiveView.captureEffectHeads {
			copy := value
			previous[pool] = &copy
		}
	}
	if self.Archive != nil {
		for pool := range self.Archive.PoolBoundaries {
			blocked[pool] = previous[pool] == nil
		}
	}
	for index := range self.Captures {
		if err := ctx.Err(); err != nil {
			return err
		}
		capture := &self.Captures[index]
		pool := capture.Event.Values["noId"]
		var expected *economicConservationCaptureEffects
		if joinedOpening && !blocked[pool] {
			for _, route := range policy.Routes {
				if route.Kind != "tail-pool" || route.PoolId != pool || route.Hotkey != capture.Event.Values["poolHotkey"] {
					continue
				}
				query, err := economicConservationPrincipalQuery(route, policy.Native.Observation.Netuid)
				if err != nil {
					return err
				}
				expected, err = deriveEconomicCaptureEffectsLookup(ctx, policy, *capture, previous[pool], query, lookup)
				if err != nil {
					return err
				}
			}
		}
		var stock *string
		if expected != nil {
			stock = &expected.OpeningStock
			previous[pool] = expected
		} else {
			blocked[pool] = true
		}
		if check {
			// Legacy unknown captures may acquire this optional companion at
			// the next sample. An existing claim must reproduce exactly.
			if capture.PrincipalEffects == nil && capture.OpeningPrincipalAlpha == nil {
				continue
			}
			if !reflect.DeepEqual(capture.PrincipalEffects, expected) || !reflect.DeepEqual(capture.OpeningPrincipalAlpha, stock) {
				return errors.New("economic capture causal join differs from original native execution and receipt")
			}
		} else {
			capture.PrincipalEffects, capture.OpeningPrincipalAlpha = expected, stock
			if expected != nil {
				capture.Status = "original-native-stake-and-vault-receipt-reconciled"
			}
		}
	}
	return nil
}

func (self economicConservationCapture) causalComplete() bool {
	return self.PrincipalEffects != nil && self.PrincipalEffects.CaptureId == self.Id && self.PrincipalEffects.Schema == economicCaptureEffectsSchema
}
