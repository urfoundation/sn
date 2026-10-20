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

var errNoPositiveUnmaskedWeights = errors.New("no positive unmasked weights")

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
		return nil, nil, errNoPositiveUnmaskedWeights
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

// impliedDemand is one pool's exact demand signal (§8.1): the audited usage
// (bytes and distinct users from the operator's signed payout artifact) priced
// at the conviction-zero tier, so a conviction discount changes what the
// operator pays and not its weight. When the epoch cap truncated the deposit
// the operator actually owed at its own tier, the demand is truncated by the
// same factor: usage the operator did not pay for buys no weight, exactly as
// deposit / rate(tier) was bounded by cap / rate(tier) before. Under a
// zero-price policy every pool's implied demand is exactly 1, so measured
// quality alone steers the pool channel and neither usage, a voluntary deposit
// nor conviction changes the score.
func impliedDemand(usageBytes, users uint64, conviction *big.Int, policy protocol.DepositPolicy) (*big.Rat, error) {
	tier, err := protocol.DepositTierAt(policy, conviction)
	if err != nil {
		return nil, err
	}
	if policy.IsZeroPrice() {
		return big.NewRat(1, 1), nil
	}
	if policy.EpochCapRaoPerOperator == 0 {
		return nil, errors.New("deposit epoch cap is zero")
	}
	baseline := protocol.DepositDemandRao(usageBytes, users, policy.Tiers[0])
	owed := protocol.DepositDemandRao(usageBytes, users, tier)
	if baseline == nil || owed == nil {
		return nil, errors.New("deposit rate divisor is zero")
	}
	if owed.Sign() == 0 {
		return new(big.Rat), nil
	}
	capRao := new(big.Rat).SetInt(new(big.Int).SetUint64(policy.EpochCapRaoPerOperator))
	if owed.Cmp(capRao) > 0 {
		baseline.Mul(baseline, capRao)
		baseline.Quo(baseline, owed)
	}
	return baseline, nil
}

// impliedUsageQuality is the pool score implied_demand × clamped quality. A
// pool with zero measured quality scores zero even at zero price.
func impliedUsageQuality(usageBytes, users uint64, conviction *big.Int, qualityPPM uint32, policy protocol.Policy) (*big.Rat, error) {
	demand, err := impliedDemand(usageBytes, users, conviction, policy.Deposit)
	if err != nil {
		return nil, err
	}
	if qualityPPM == 0 || demand.Sign() == 0 {
		return new(big.Rat), nil
	}
	q := qualityPPM
	if q < policy.Steering.QualityTransform.MinimumPPM {
		q = policy.Steering.QualityTransform.MinimumPPM
	}
	if q > policy.Steering.QualityTransform.MaximumPPM {
		q = policy.Steering.QualityTransform.MaximumPPM
	}
	return demand.Mul(demand, new(big.Rat).SetFrac(new(big.Int).SetUint64(uint64(q)), big.NewInt(1_000_000))), nil
}

// PoolQualityPPM is the exposure-weighted quality of one isolated NO context.
// Bound head providers are omitted from both numerator and denominator.
func PoolQualityPPM(stats *StatsEngine, bound map[connect.Id]bool) uint32 {
	if stats == nil {
		return 0
	}
	verified, err := VerifyReleaseStatsMeasurement(stats.currentReleaseStatsMeasurement())
	if err != nil {
		return 0
	}
	return PoolQualityFromReleaseStats(verified, bound)
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
