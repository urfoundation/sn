package validator

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/urnetwork/connect/v2026"

	"github.com/urfoundation/sn/v2026/protocol"
)

type ExactWeightInput struct {
	UID   uint16
	Score *big.Rat
}

// BuildWeightVectorExact combines head and pool channels with the canonical
// rational theta. Empty channels cede their allocation and every explicit mask
// is applied after aggregation, so duplicate UID inputs cannot bypass it.
func BuildWeightVectorExact(pools, head []ExactWeightInput, theta protocol.Rational, masked map[uint16]bool) ([]uint16, []*big.Rat, error) {
	if err := theta.Validate("theta"); err != nil || theta.Numerator > theta.Denominator {
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, errors.New("theta exceeds one")
	}
	poolByUID, poolSum, err := sumExactInputs(pools, masked)
	if err != nil {
		return nil, nil, fmt.Errorf("pool: %w", err)
	}
	headByUID, headSum, err := sumExactInputs(head, masked)
	if err != nil {
		return nil, nil, fmt.Errorf("head: %w", err)
	}
	if poolSum.Sign() == 0 && headSum.Sign() == 0 {
		return nil, nil, errors.New("no positive unmasked weights")
	}
	headShare := new(big.Rat).SetFrac(new(big.Int).SetUint64(theta.Numerator), new(big.Int).SetUint64(theta.Denominator))
	poolShare := new(big.Rat).Sub(big.NewRat(1, 1), headShare)
	if headSum.Sign() == 0 {
		headShare.SetInt64(0)
		poolShare.SetInt64(1)
	}
	if poolSum.Sign() == 0 {
		poolShare.SetInt64(0)
		headShare.SetInt64(1)
	}
	combined := map[uint16]*big.Rat{}
	addChannel := func(values map[uint16]*big.Rat, sum, share *big.Rat) {
		if sum.Sign() == 0 || share.Sign() == 0 {
			return
		}
		for uid, score := range values {
			part := new(big.Rat).Mul(share, new(big.Rat).Quo(score, sum))
			if combined[uid] == nil {
				combined[uid] = new(big.Rat)
			}
			combined[uid].Add(combined[uid], part)
		}
	}
	addChannel(poolByUID, poolSum, poolShare)
	addChannel(headByUID, headSum, headShare)
	uids := make([]uint16, 0, len(combined))
	for uid, score := range combined {
		if score.Sign() > 0 && !masked[uid] {
			uids = append(uids, uid)
		}
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	scores := make([]*big.Rat, len(uids))
	for i, uid := range uids {
		scores[i] = new(big.Rat).Set(combined[uid])
	}
	if len(uids) == 0 {
		return nil, nil, errors.New("all positive weights were masked")
	}
	return uids, scores, nil
}

func sumExactInputs(inputs []ExactWeightInput, masked map[uint16]bool) (map[uint16]*big.Rat, *big.Rat, error) {
	values := map[uint16]*big.Rat{}
	total := new(big.Rat)
	for i, input := range inputs {
		if input.Score == nil || input.Score.Sign() < 0 {
			return nil, nil, fmt.Errorf("input %d has nil or negative score", i)
		}
		if input.Score.Sign() == 0 || masked[input.UID] {
			continue
		}
		if values[input.UID] == nil {
			values[input.UID] = new(big.Rat)
		}
		values[input.UID].Add(values[input.UID], input.Score)
		total.Add(total, input.Score)
	}
	return values, total, nil
}

func depositRateAt(policy protocol.DepositPolicy, conviction *big.Int) (*big.Rat, error) {
	selected, err := protocol.DepositTierAt(policy, conviction)
	if err != nil {
		return nil, err
	}
	return new(big.Rat).SetFrac(
		new(big.Int).SetUint64(selected.RateNumeratorRaoPerGiB),
		new(big.Int).SetUint64(selected.RateDenominator),
	), nil
}

func impliedUsageQuality(deposit, conviction *big.Int, qualityPPM uint32, policy protocol.Policy) (*big.Rat, error) {
	if deposit == nil || deposit.Sign() < 0 {
		return nil, errors.New("invalid epoch deposit")
	}
	if deposit.Sign() == 0 || qualityPPM == 0 {
		return new(big.Rat), nil
	}
	rate, err := depositRateAt(policy.Deposit, conviction)
	if err != nil {
		return nil, err
	}
	q := qualityPPM
	if q < policy.Steering.QualityTransform.MinimumPPM {
		q = policy.Steering.QualityTransform.MinimumPPM
	}
	if q > policy.Steering.QualityTransform.MaximumPPM {
		q = policy.Steering.QualityTransform.MaximumPPM
	}
	usage := new(big.Rat).Quo(new(big.Rat).SetInt(deposit), rate)
	return usage.Mul(usage, new(big.Rat).SetFrac(new(big.Int).SetUint64(uint64(q)), big.NewInt(1_000_000))), nil
}

// PoolQualityPPM is the exposure-weighted quality of one isolated NO context.
// Bound head providers are omitted from both numerator and denominator.
func PoolQualityPPM(stats *StatsEngine, bound map[connect.Id]bool) uint32 {
	if stats == nil {
		return 0
	}
	quality := stats.QualityPPM()
	exposure := stats.Exposure()
	var numerator, denominator uint64
	for id, q := range quality {
		if bound[id] {
			continue
		}
		weight := exposure[id]
		if weight == 0 {
			weight = 1
		}
		// q <= 1e6 and an in-memory window cannot approach uint64 overflow;
		// use saturation defensively so malformed snapshots fail low.
		if q != 0 && weight > (^uint64(0)-numerator)/uint64(q) {
			return 0
		}
		numerator += uint64(q) * weight
		if ^uint64(0)-denominator < weight {
			return 0
		}
		denominator += weight
	}
	if denominator == 0 {
		return 0
	}
	return uint32(numerator / denominator)
}

func ExactHeadScores(fleets map[uint16]map[[32]byte]bool) map[uint16]*big.Rat {
	claims := map[[32]byte]uint64{}
	for _, hashes := range fleets {
		for hash := range hashes {
			claims[hash]++
		}
	}
	out := make(map[uint16]*big.Rat, len(fleets))
	for uid, hashes := range fleets {
		score := new(big.Rat)
		for hash := range hashes {
			if claims[hash] > 0 {
				score.Add(score, new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).SetUint64(claims[hash])))
			}
		}
		out[uid] = score
	}
	return out
}
