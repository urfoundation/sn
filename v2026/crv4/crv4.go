// Package crv4 implements Go-native Bittensor commit-reveal v4 (CRv4)
// weight submission for the UR subnet validator (SP-2, decision D-1).
//
// CRv4 commits are drand-timelock-encrypted CLIENT-SIDE: the validator
// builds the SCALE weights payload, encrypts it to a future drand quicknet
// round with tle-compatible timelock encryption (NOT the age-based
// github.com/drand/tlock format), and submits
// SubtensorModule.commit_timelocked_weights signed by its sr25519 hotkey.
// The chain ingests drand pulses via pallet_drand and decrypts+applies the
// weights at the reveal epoch; the client never sends a reveal extrinsic.
//
// See crv4/README.md for every pinned upstream source (subtensor
// v3.4.9-424, bittensor-drand v2.0.0, ideal-lab5/timelock @ 5416406,
// bittensor SDK @ c4dca6b) and the conformance status.
package crv4

import (
	"context"
	"fmt"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// SubmitOptions tune SubmitWeightsCRv4. The zero value is a working default
// for a plain (single-mechanism) subnet on a 12s-block chain.
type SubmitOptions struct {
	// Mecid selects commit_timelocked_mechanism_weights for a sub-mechanism.
	// nil uses commit_timelocked_weights (MechId::MAIN == 0).
	Mecid *uint8

	// VersionKey is the payload weights version key; must be >= the
	// subnet's WeightsVersionKey hyperparameter for the reveal to apply.
	VersionKey uint64

	// BlockTimeSecs is the chain block time in seconds used to convert the
	// predicted reveal block into wall-clock time (mainnet 12.0, public
	// testnet fast-blocks 0.25). Default 12.0.
	BlockTimeSecs float64

	// RevealPeriodEpochs overrides the on-chain RevealPeriodEpochs
	// hyperparameter when non-nil (useful to avoid one storage read).
	RevealPeriodEpochs *uint64

	// MaxWeightLimit overrides the on-chain MaxWeightsLimit hyperparameter
	// when non-nil.
	MaxWeightLimit *uint16

	// CommitRevealVersion overrides the on-chain
	// CommitRevealWeightsVersion when non-nil (the chain rejects mismatches).
	CommitRevealVersion *uint16

	// Now overrides the wall clock (tests).
	Now func() time.Time
}

// SubmitResult reports what a SubmitWeightsCRv4 call committed.
type SubmitResult struct {
	TxHash        types.Hash
	RevealRound   uint64
	RevealBlock   uint64
	Uids          []uint16
	Values        []uint16
	CiphertextLen int
}

// SubmitWeightsCRv4 is the one-call CRv4 path used by the validator steering
// loop: normalize float scores to u16 (max -> 65535, with the subnet's
// max_weight_limit cap), build the WeightsTlockPayload bound to the hotkey,
// compute the reveal round from the live epoch schedule, timelock-encrypt,
// and submit the hotkey-signed commit extrinsic.
func SubmitWeightsCRv4(ctx context.Context, chain *Chain, kp *Keypair, netuid uint16, uids []uint16, scores []float64, opts SubmitOptions) (*SubmitResult, error) {
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	blockTime := opts.BlockTimeSecs
	if blockTime == 0 {
		blockTime = 12.0
	}

	enabled, err := chain.CommitRevealEnabled(netuid)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, fmt.Errorf("crv4: commit-reveal is disabled on netuid %d (use set_weights instead)", netuid)
	}

	version := CommitRevealVersion4
	if opts.CommitRevealVersion != nil {
		version = *opts.CommitRevealVersion
	} else if v, err := chain.CommitRevealVersion(); err == nil {
		version = v
	} else {
		return nil, err
	}

	// --- normalize scores -> u16 weights ---
	maxWeightLimit := uint16(U16Max)
	if opts.MaxWeightLimit != nil {
		maxWeightLimit = *opts.MaxWeightLimit
	} else if mwl, err := chain.MaxWeightsLimit(netuid); err == nil {
		maxWeightLimit = mwl
	} else {
		return nil, err
	}
	capped, err := ApplyMaxWeightLimit(scores, maxWeightLimit)
	if err != nil {
		return nil, err
	}
	u16uids, u16vals, err := NormalizeToU16(uids, capped)
	if err != nil {
		return nil, err
	}
	if len(u16uids) == 0 {
		return nil, fmt.Errorf("crv4: all weights are zero; nothing to commit")
	}

	// --- reveal round from the live epoch schedule ---
	revealPeriodEpochs := uint64(1)
	if opts.RevealPeriodEpochs != nil {
		revealPeriodEpochs = *opts.RevealPeriodEpochs
	} else if rpe, err := chain.RevealPeriodEpochs(netuid); err == nil {
		revealPeriodEpochs = rpe
	} else {
		return nil, err
	}
	state, err := chain.EpochScheduleState(netuid)
	if err != nil {
		return nil, err
	}
	round, revealBlock, err := RevealRound(now(), state, revealPeriodEpochs, blockTime)
	if err != nil {
		return nil, err
	}

	// --- payload -> timelock ciphertext ---
	payload := &Payload{
		Hotkey:     kp.PublicKey(),
		Uids:       u16uids,
		Values:     u16vals,
		VersionKey: opts.VersionKey,
	}
	encoded, err := payload.Encode()
	if err != nil {
		return nil, err
	}
	ciphertext, err := Encrypt(encoded, round)
	if err != nil {
		return nil, err
	}

	// --- commit extrinsic ---
	txHash, err := chain.Commit(ctx, kp, netuid, opts.Mecid, ciphertext, round, version)
	if err != nil {
		return nil, err
	}
	return &SubmitResult{
		TxHash:        txHash,
		RevealRound:   round,
		RevealBlock:   revealBlock,
		Uids:          u16uids,
		Values:        u16vals,
		CiphertextLen: len(ciphertext),
	}, nil
}
