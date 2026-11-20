// Explicit production profiles select reviewed epoch timing while retained
// approvals keep the original prediction and signed transaction interpretation.
package crv4

import (
	"errors"
	"math"
	"time"
)

// Constants pinned against bittensor-drand v2.0.0 src/constants.rs
// (https://github.com/opentensor/bittensor-drand @ 0dff71eb) and drand
// quicknet chain info
// (https://api.drand.sh/52db9ba70e0cc0f6eaf7803dd07447a1f5477735fd3f661792ba94600c84e971/info).
const (
	// Explicit production approval selects the v470 tempo-drift semantics.
	// Empty preserves the original drand-v2 scheduler for retained callers.
	TempoDriftEpochScheduleProfile = "urnetwork-subtensor-tempo-drift-v1"

	// MaxTempo is subtensor's MAX_TEMPO (upper bound for owner-set tempo).
	MaxTempo uint64 = 50_400

	// DrandGenesisTime is the drand quicknet genesis timestamp (unix seconds).
	DrandGenesisTime uint64 = 1_692_803_367

	// DrandPeriod is the drand quicknet round period in seconds.
	DrandPeriod uint64 = 3

	// SecurityBlockOffset: blocks added to the predicted reveal block so the
	// targeted drand pulse has already been ingested on-chain at reveal time.
	SecurityBlockOffset uint64 = 3

	// CommitInclusionBlockOffset: the commit extrinsic lands in the block
	// AFTER the chain head that the state snapshot was read at.
	CommitInclusionBlockOffset uint64 = 1
)

// EpochScheduleState is a snapshot of subtensor's per-subnet epoch storage,
// all read at the same block. Mirrors bittensor-drand's EpochScheduleState
// and the SDK's get_epoch_schedule_state (storage items in pallet
// SubtensorModule: LastEpochBlock, PendingEpochAt, SubnetEpochIndex, Tempo,
// BlocksSinceLastStep; CurrentBlock is the block number of the snapshot).
type EpochScheduleState struct {
	EpochScheduleProfile string `json:"epoch_schedule_profile,omitempty"`
	LastEpochBlock       uint64
	PendingEpochAt       uint64
	SubnetEpochIndex     uint64
	Tempo                uint16
	BlocksSinceLastStep  uint64
	CurrentBlock         uint64
}

var (
	// ErrTempoZero is returned when the subnet tempo is 0 (no epochs run).
	ErrTempoZero = errors.New("crv4: tempo is zero; subnet does not run epochs")
	// ErrBoundExceeded is returned when the reveal-block simulation exceeds
	// its block budget.
	ErrBoundExceeded = errors.New("crv4: reveal block simulation exceeded budget")
)

// A runtime number or matching metadata does not select scheduling semantics.
// The exact production approval chooses the reviewed profile independently.
func ValidateEpochScheduleProfile(profile string) error {
	if profile != "" && profile != TempoDriftEpochScheduleProfile {
		return errors.New("crv4: epoch schedule profile is unsupported")
	}
	return nil
}

// maxSimulationBlocks bounds the reveal-block search, port of
// constants.rs::max_simulation_blocks.
func maxSimulationBlocks(revealPeriodEpochs uint64) uint64 {
	return satAdd(satMul(revealPeriodEpochs, MaxTempo), MaxTempo)
}

// The legacy empty profile preserves drand-v2/v424 interpretation. Explicit
// tempo-drift follows Subtensor 923fd1fa run_coinbase.rs:1208: the fallback uses
// this subnet's tempo, not MAX_TEMPO. Approval remains outside this pure model.
//
//	tempo == 0                        -> never
//	pending > 0 && block >= pending   -> fire (owner-triggered)
//	blocks_since_last_step > selected limit -> fire (safety net)
//	block - last_epoch_block >= tempo -> fire (normal cadence)
func shouldRunEpoch(s *EpochScheduleState, block uint64) bool {
	if s.Tempo == 0 {
		return false
	}
	if s.PendingEpochAt > 0 && block >= s.PendingEpochAt {
		return true
	}
	limit := MaxTempo
	if s.EpochScheduleProfile == TempoDriftEpochScheduleProfile {
		limit = uint64(s.Tempo)
	}
	if s.BlocksSinceLastStep > limit {
		return true
	}
	return satSub(block, s.LastEpochBlock) >= uint64(s.Tempo)
}

// currentEpochPreRunCoinbase ports subtensor
// weights.rs::current_epoch_with_lookahead (== bittensor-drand
// epoch_schedule.rs::current_epoch_pre_run_coinbase): the epoch index that a
// commit or reveal happening at `block` belongs to. reveal_crv3_commits runs
// before run_coinbase in block_step, so if the epoch slot fires this block,
// the counter is observed +1.
func currentEpochPreRunCoinbase(s *EpochScheduleState, block uint64) uint64 {
	base := s.SubnetEpochIndex
	if shouldRunEpoch(s, block) {
		return satAdd(base, 1)
	}
	return base
}

// simulateRunCoinbase advances the schedule state by one block, port of
// bittensor-drand epoch_schedule.rs::simulate_run_coinbase (subset of
// subtensor run_coinbase.rs; does not model MaxEpochsPerBlock deferral).
func simulateRunCoinbase(s *EpochScheduleState, block uint64) EpochScheduleState {
	next := *s
	next.BlocksSinceLastStep = satAdd(next.BlocksSinceLastStep, 1)
	if s.EpochScheduleProfile == TempoDriftEpochScheduleProfile {
		next.BlocksSinceLastStep = min(next.BlocksSinceLastStep, uint64(s.Tempo)+1)
	}
	next.CurrentBlock = block
	if shouldRunEpoch(&next, block) {
		next.LastEpochBlock = block
		next.PendingEpochAt = 0
		next.SubnetEpochIndex = satAdd(next.SubnetEpochIndex, 1)
		next.BlocksSinceLastStep = 0
	}
	return next
}

// advanceBlocks applies simulateRunCoinbase for each block in [start, end].
func advanceBlocks(from *EpochScheduleState, start, end uint64) EpochScheduleState {
	state := *from
	if start > end {
		return state
	}
	for b := start; b <= end; b++ {
		state = simulateRunCoinbase(&state, b)
	}
	return state
}

// PredictFirstRevealBlock predicts the first block at which the chain will
// reveal a commit submitted against head state s. Port of bittensor-drand
// v2.0.0 epoch_schedule.rs::predict_first_reveal_block:
//
//  1. The extrinsic is included at head+1 (CommitInclusionBlockOffset).
//  2. The chain keys TimelockedWeightCommits by current_epoch_with_lookahead.
//     The tempo-drift profile evaluates it after initialization, when extrinsics
//     execute; an empty profile retains the original drand-v2 pre-step model.
//  3. The reveal fires at the first block whose pre-run_coinbase epoch equals
//     commit_epoch + revealPeriodEpochs (exact equality; reveal_crv3_commits
//     takes entries for epoch cur_epoch - reveal_period).
func PredictFirstRevealBlock(s *EpochScheduleState, revealPeriodEpochs uint64) (uint64, error) {
	if s == nil {
		return 0, errors.New("crv4: epoch schedule is absent")
	}
	if err := ValidateEpochScheduleProfile(s.EpochScheduleProfile); err != nil {
		return 0, err
	}
	// Source 923fd1fa bounds reveal periods to 1..100 and native heights to u32.
	// Apply those limits only to the explicitly reviewed production model.
	if s.EpochScheduleProfile == TempoDriftEpochScheduleProfile && (revealPeriodEpochs == 0 || revealPeriodEpochs > 100 ||
		s.CurrentBlock >= math.MaxUint32 || s.LastEpochBlock > s.CurrentBlock || s.PendingEpochAt > math.MaxUint32 ||
		s.BlocksSinceLastStep > s.CurrentBlock || s.SubnetEpochIndex > math.MaxUint64-revealPeriodEpochs-1) {
		return 0, errors.New("crv4: tempo-drift schedule exceeds reviewed block, epoch or reveal-period bounds")
	}
	if s.Tempo == 0 {
		return 0, ErrTempoZero
	}

	headBlock := s.CurrentBlock
	extrinsicBlock := headBlock + CommitInclusionBlockOffset

	postBeforeExtrinsic := *s
	if extrinsicBlock != headBlock+1 {
		postBeforeExtrinsic = advanceBlocks(s, headBlock+1, extrinsicBlock-1)
	}

	commitEpoch := currentEpochPreRunCoinbase(&postBeforeExtrinsic, extrinsicBlock)
	if s.EpochScheduleProfile == TempoDriftEpochScheduleProfile {
		// Reveals run before coinbase; the extrinsic runs after initialization.
		// At B==tempo the increment alone consumes an epoch before the commit.
		postInitialization := simulateRunCoinbase(&postBeforeExtrinsic, extrinsicBlock)
		commitEpoch = currentEpochPreRunCoinbase(&postInitialization, extrinsicBlock)
	}
	targetEpoch := commitEpoch + revealPeriodEpochs

	maxSim := maxSimulationBlocks(revealPeriodEpochs)
	if s.EpochScheduleProfile == TempoDriftEpochScheduleProfile {
		// Root may select any u16 tempo. MAX_TEMPO bounds owner choices only.
		limit := max(MaxTempo, uint64(s.Tempo))
		maxSim = satAdd(satMul(revealPeriodEpochs, limit), limit)
		maxSim = min(maxSim, math.MaxUint32-extrinsicBlock)
	}

	postPrev := postBeforeExtrinsic
	for r := extrinsicBlock; r <= satAdd(extrinsicBlock, maxSim); r++ {
		if currentEpochPreRunCoinbase(&postPrev, r) == targetEpoch {
			return r, nil
		}
		postPrev = simulateRunCoinbase(&postPrev, r)
	}

	return 0, ErrBoundExceeded
}

// RevealRound computes the drand quicknet round to timelock-encrypt to, plus
// the predicted on-chain reveal block, for a commit submitted now against
// state s. Pure function; port of bittensor-drand v2.0.0
// drand.rs::generate_commit_v2 round computation:
//
//	first_reveal_block = PredictFirstRevealBlock(s, revealPeriodEpochs)
//	target_ingest      = first_reveal_block + SecurityBlockOffset
//	secs_until_ingest  = (target_ingest - s.CurrentBlock) * blockTimeSecs
//	reveal_round       = floor((now + secs_until_ingest - GENESIS_TIME) / PERIOD)
//	                     (minimum 1)
//
// blockTimeSecs is the chain block time in seconds (mainnet 12.0; public
// testnet with fast-blocks 0.25).
func RevealRound(now time.Time, s *EpochScheduleState, revealPeriodEpochs uint64, blockTimeSecs float64) (round uint64, revealBlock uint64, err error) {
	revealBlock, err = PredictFirstRevealBlock(s, revealPeriodEpochs)
	if err != nil {
		return 0, 0, err
	}
	targetIngest := satAdd(revealBlock, SecurityBlockOffset)
	blocksUntilIngest := satSub(targetIngest, s.CurrentBlock)
	secsUntilIngest := float64(blocksUntilIngest) * blockTimeSecs

	// Rust uses SystemTime::now() as_secs_f64 (fractional seconds).
	nowSecs := float64(now.UnixNano()) / 1e9
	targetSecs := nowSecs + secsUntilIngest

	r := math.Floor((targetSecs - float64(DrandGenesisTime)) / float64(DrandPeriod))
	if r < 1 {
		return 1, revealBlock, nil
	}
	return uint64(r), revealBlock, nil
}

// CurrentDrandRound returns the latest quicknet round expected to be
// published at time now (round 1 at genesis; a new round every DrandPeriod).
func CurrentDrandRound(now time.Time) uint64 {
	nowSecs := uint64(now.Unix())
	if nowSecs < DrandGenesisTime {
		return 0
	}
	return (nowSecs-DrandGenesisTime)/DrandPeriod + 1
}

func satAdd(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

func satSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

func satMul(a, b uint64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > math.MaxUint64/b {
		return math.MaxUint64
	}
	return a * b
}
