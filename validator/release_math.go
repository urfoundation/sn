package validator

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/urnetwork/connect"

	"github.com/urfoundation/sn/amendment"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/protocol/headscore"
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
	return combineExactChannels(masked, exactChannel{poolByUID, poolSum, poolShare}, exactChannel{headByUID, headSum, headShare})
}

// BuildWeightVectorCtf combines the pool, CTF and head channels under capture
// the flag's shares, which replace theta from E_c (CAPTURETHEFLAG.md §2.1). The
// cede rules apply in order:
//
//  1. A non-empty CTF channel takes exactly its share.
//  2. Pool and head share the rest in the ratio of their shares, or all of the
//     row when the CTF channel is empty. An empty one cedes to the other, so
//     with no head fleet the pools take the rest and the CTF pools keep theirs.
//  3. The CTF channel is empty with no positive unmasked input, and always when
//     the pool channel is: a CTF pool takes its operator's pool weight (§2.2),
//     so with no pool weight there is nothing to divide it by. Its share then
//     joins rule 2.
//  4. With no pool or head weight there is no provider row
//     (errNoPositiveUnmaskedWeights), which the treasury answers reserve-only.
//
// Masks apply after aggregation, as in BuildWeightVectorExact.
func BuildWeightVectorCtf(pools, ctf, head []ExactWeightInput, shares amendment.Shares, masked map[uint16]bool) ([]uint16, []*big.Rat, error) {
	poolShare, ctfShare, headShare, err := ctfShareRats(shares)
	if err != nil {
		return nil, nil, err
	}
	poolByUID, poolSum, err := sumExactInputs(pools, masked)
	if err != nil {
		return nil, nil, fmt.Errorf("pool: %w", err)
	}
	ctfByUID, ctfSum, err := sumExactInputs(ctf, masked)
	if err != nil {
		return nil, nil, fmt.Errorf("ctf: %w", err)
	}
	headByUID, headSum, err := sumExactInputs(head, masked)
	if err != nil {
		return nil, nil, fmt.Errorf("head: %w", err)
	}
	if poolSum.Sign() == 0 && headSum.Sign() == 0 {
		return nil, nil, errNoPositiveUnmaskedWeights
	}
	if poolSum.Sign() == 0 || ctfSum.Sign() == 0 {
		ctfShare.SetInt64(0)
	}
	rest := new(big.Rat).Sub(big.NewRat(1, 1), ctfShare)
	switch {
	case headSum.Sign() == 0:
		poolShare.Set(rest)
		headShare.SetInt64(0)
	case poolSum.Sign() == 0:
		poolShare.SetInt64(0)
		headShare.Set(rest)
	default:
		both := new(big.Rat).Add(poolShare, headShare)
		poolShare.Mul(rest, poolShare.Quo(poolShare, both))
		headShare.Sub(rest, poolShare)
	}
	return combineExactChannels(masked, exactChannel{poolByUID, poolSum, poolShare}, exactChannel{ctfByUID, ctfSum, ctfShare}, exactChannel{headByUID, headSum, headShare})
}

// ctfShareRats is shares as exact rationals: each between zero and one, and all
// three summing to one.
func ctfShareRats(shares amendment.Shares) (pool, ctf, head *big.Rat, err error) {
	rat := func(name string, ratio amendment.Ratio) (*big.Rat, error) {
		if ratio.Num == 0 || ratio.Den == 0 || ratio.Num >= ratio.Den {
			return nil, fmt.Errorf("capture the flag %s share %d/%d is not between zero and one", name, ratio.Num, ratio.Den)
		}
		return new(big.Rat).SetFrac(new(big.Int).SetUint64(ratio.Num), new(big.Int).SetUint64(ratio.Den)), nil
	}
	if pool, err = rat("pool", shares.Pool); err != nil {
		return nil, nil, nil, err
	}
	if ctf, err = rat("ctf", shares.Ctf); err != nil {
		return nil, nil, nil, err
	}
	if head, err = rat("head", shares.Head); err != nil {
		return nil, nil, nil, err
	}
	if total := new(big.Rat).Add(pool, ctf); total.Add(total, head).Cmp(big.NewRat(1, 1)) != 0 {
		return nil, nil, nil, errors.New("capture the flag shares do not sum to one")
	}
	return pool, ctf, head, nil
}

// exactChannel is one channel's aggregated inputs and the share of the row it
// takes.
type exactChannel struct {
	values     map[uint16]*big.Rat
	sum, share *big.Rat
}

// combineExactChannels gives each channel's UIDs its share in proportion to
// their scores, then leaves out masked UIDs and orders the row by UID.
func combineExactChannels(masked map[uint16]bool, channels ...exactChannel) ([]uint16, []*big.Rat, error) {
	combined := map[uint16]*big.Rat{}
	for _, channel := range channels {
		if channel.sum.Sign() == 0 || channel.share.Sign() == 0 {
			continue
		}
		for uid, score := range channel.values {
			part := new(big.Rat).Mul(channel.share, new(big.Rat).Quo(score, channel.sum))
			if combined[uid] == nil {
				combined[uid] = new(big.Rat)
			}
			combined[uid].Add(combined[uid], part)
		}
	}
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

// ExactHeadScores is the shared-prefix raw score of a census keyed by UID.
func ExactHeadScores(fleets map[uint16]map[[32]byte]bool) map[uint16]*big.Rat {
	return headscore.RawScores(fleets)
}
