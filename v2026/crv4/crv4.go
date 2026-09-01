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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"golang.org/x/crypto/blake2b"
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
	TxHash             types.Hash
	RevealRound        uint64
	RevealBlock        uint64
	Uids               []uint16
	Values             []uint16
	CiphertextLen      int
	FinalizedBlock     uint64
	FinalizedBlockHash types.Hash
}

const PreparedSubmissionSchema = "urnetwork-crv4-prepared-submission-v1"

// PreparedSubmission is the complete, exact CRv4 write-ahead record. It is
// deliberately composed only of JSON-safe primitives so a validator can
// fsync it before the first broadcast and replay the byte-identical signed
// extrinsic after a crash. The plaintext payload is private validator state;
// callers must persist this object with mode 0600 and must not publish it
// before the reveal round.
type PreparedSubmission struct {
	Schema              string   `json:"schema"`
	Netuid              uint16   `json:"netuid"`
	Mecid               *uint8   `json:"mecid,omitempty"`
	HotkeyHex           string   `json:"hotkey_hex"`
	VersionKey          uint64   `json:"version_key"`
	CommitRevealVersion uint16   `json:"commit_reveal_version"`
	AccountNonce        uint32   `json:"account_nonce"`
	PreparedAtBlock     uint64   `json:"prepared_at_block"`
	PreparedAtBlockHash string   `json:"prepared_at_block_hash"`
	SubnetEpoch         uint64   `json:"subnet_epoch"`
	RevealRound         uint64   `json:"reveal_round"`
	RevealBlock         uint64   `json:"reveal_block"`
	UIDs                []uint16 `json:"uids"`
	Values              []uint16 `json:"values"`
	PayloadHex          string   `json:"payload_hex"`
	CiphertextHex       string   `json:"ciphertext_hex"`
	CiphertextSHA256    string   `json:"ciphertext_sha256"`
	ExtrinsicHex        string   `json:"extrinsic_hex"`
	ExtrinsicHash       string   `json:"extrinsic_hash"`
}

// Validate validates every durable field which can be independently
// reconstructed and returns the exact signed SCALE bytes for broadcast.
func (p *PreparedSubmission) Validate() ([]byte, error) {
	if p == nil || p.Schema != PreparedSubmissionSchema {
		return nil, fmt.Errorf("crv4: unsupported prepared submission schema")
	}
	if len(p.UIDs) == 0 || len(p.UIDs) != len(p.Values) {
		return nil, fmt.Errorf("crv4: malformed prepared weights")
	}
	hotkeyRaw, err := hex.DecodeString(strings.TrimPrefix(p.HotkeyHex, "0x"))
	if err != nil || len(hotkeyRaw) != 32 {
		return nil, fmt.Errorf("crv4: malformed prepared hotkey")
	}
	var hotkey [32]byte
	copy(hotkey[:], hotkeyRaw)
	wantPayload, err := (&Payload{Hotkey: hotkey, Uids: p.UIDs, Values: p.Values, VersionKey: p.VersionKey}).Encode()
	if err != nil {
		return nil, err
	}
	payload, err := codec.HexDecodeString(p.PayloadHex)
	if err != nil || !bytes.Equal(payload, wantPayload) {
		return nil, fmt.Errorf("crv4: prepared payload does not match weights and hotkey")
	}
	ciphertext, err := codec.HexDecodeString(p.CiphertextHex)
	if err != nil || len(ciphertext) == 0 || len(ciphertext) > MaxCommitSizeBytes {
		return nil, fmt.Errorf("crv4: malformed prepared ciphertext")
	}
	cipherHash := sha256.Sum256(ciphertext)
	if p.CiphertextSHA256 != "0x"+hex.EncodeToString(cipherHash[:]) {
		return nil, fmt.Errorf("crv4: prepared ciphertext hash mismatch")
	}
	raw, err := codec.HexDecodeString(p.ExtrinsicHex)
	if err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("crv4: malformed prepared extrinsic")
	}
	if err := validateExtrinsicEnvelope(raw); err != nil {
		return nil, err
	}
	if !bytes.Contains(raw, ciphertext) {
		return nil, fmt.Errorf("crv4: prepared extrinsic does not contain ciphertext")
	}
	digest := blake2b.Sum256(raw)
	if p.ExtrinsicHash != types.Hash(digest).Hex() {
		return nil, fmt.Errorf("crv4: prepared extrinsic hash mismatch")
	}
	return raw, nil
}

func validateExtrinsicEnvelope(raw []byte) error {
	if len(raw) < 2 {
		return fmt.Errorf("crv4: truncated prepared extrinsic")
	}
	var declared uint64
	var prefix int
	switch raw[0] & 3 {
	case 0:
		declared, prefix = uint64(raw[0]>>2), 1
	case 1:
		if len(raw) < 2 {
			return fmt.Errorf("crv4: truncated prepared extrinsic length")
		}
		declared, prefix = uint64(uint16(raw[0])|uint16(raw[1])<<8)>>2, 2
	case 2:
		if len(raw) < 4 {
			return fmt.Errorf("crv4: truncated prepared extrinsic length")
		}
		declared = uint64(uint32(raw[0])|uint32(raw[1])<<8|uint32(raw[2])<<16|uint32(raw[3])<<24) >> 2
		prefix = 4
	case 3:
		width := int(raw[0]>>2) + 4
		if width > 8 || len(raw) < 1+width {
			return fmt.Errorf("crv4: unsupported prepared extrinsic length")
		}
		prefix = 1 + width
		for index := 0; index < width; index++ {
			declared |= uint64(raw[1+index]) << (8 * index)
		}
	}
	if declared != uint64(len(raw)-prefix) {
		return fmt.Errorf("crv4: prepared extrinsic SCALE length mismatch")
	}
	// Signed version-4 is 0x84. Every production CRv4 submission must be
	// signed; accepting an unsigned envelope would make replay meaningless.
	if raw[prefix] != 0x84 {
		return fmt.Errorf("crv4: prepared extrinsic is not signed version 4")
	}
	return nil
}

// SubmitWeightsCRv4 is the one-call CRv4 path used by the validator steering
// loop: normalize float scores to u16 (max -> 65535, with the subnet's
// max_weight_limit cap), build the WeightsTlockPayload bound to the hotkey,
// compute the reveal round from the live epoch schedule, timelock-encrypt,
// and submit the hotkey-signed commit extrinsic.
func SubmitWeightsCRv4(ctx context.Context, chain *Chain, kp *Keypair, netuid uint16, uids []uint16, scores []float64, opts SubmitOptions) (*SubmitResult, error) {
	prepared, err := PrepareWeightsCRv4(ctx, chain, kp, netuid, uids, scores, opts)
	if err != nil {
		return nil, err
	}
	return SubmitPrepared(ctx, chain, prepared)
}

// PrepareWeightsCRv4 constructs but does not broadcast a signed submission.
func PrepareWeightsCRv4(ctx context.Context, chain *Chain, kp *Keypair, netuid uint16, uids []uint16, scores []float64, opts SubmitOptions) (*PreparedSubmission, error) {
	version, maxWeightLimit, err := resolveSubmitParameters(chain, netuid, opts)
	if err != nil {
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
	if err := repairMaxWeightLimitU16(u16uids, u16vals, maxWeightLimit); err != nil {
		return nil, err
	}
	if len(u16uids) == 0 {
		return nil, fmt.Errorf("crv4: all weights are zero; nothing to commit")
	}
	return prepareWeightsU16(ctx, chain, kp, netuid, u16uids, u16vals, version, opts)
}

// SubmitWeightsCRv4Exact is the release-1.0 production entry point. It applies
// the max-weight cap and u16 normalization using only big.Int/big.Rat math,
// then commits the same CRv4 payload as SubmitWeightsCRv4.
func SubmitWeightsCRv4Exact(ctx context.Context, chain *Chain, kp *Keypair, netuid uint16, uids []uint16, scores []*big.Rat, opts SubmitOptions) (*SubmitResult, error) {
	prepared, err := PrepareWeightsCRv4Exact(ctx, chain, kp, netuid, uids, scores, opts)
	if err != nil {
		return nil, err
	}
	return SubmitPrepared(ctx, chain, prepared)
}

// PrepareWeightsCRv4Exact is the write-ahead half of the release path. The
// caller must durably persist its result before calling SubmitPrepared.
func PrepareWeightsCRv4Exact(ctx context.Context, chain *Chain, kp *Keypair, netuid uint16, uids []uint16, scores []*big.Rat, opts SubmitOptions) (*PreparedSubmission, error) {
	version, maxWeightLimit, err := resolveSubmitParameters(chain, netuid, opts)
	if err != nil {
		return nil, err
	}
	capped, err := ApplyMaxWeightLimitRational(scores, maxWeightLimit)
	if err != nil {
		return nil, err
	}
	u16uids, u16vals, err := NormalizeRationalToU16(uids, capped)
	if err != nil {
		return nil, err
	}
	if err := repairMaxWeightLimitU16(u16uids, u16vals, maxWeightLimit); err != nil {
		return nil, err
	}
	if len(u16uids) == 0 {
		return nil, fmt.Errorf("crv4: all weights are zero; nothing to commit")
	}
	return prepareWeightsU16(ctx, chain, kp, netuid, u16uids, u16vals, version, opts)
}

func resolveSubmitParameters(chain *Chain, netuid uint16, opts SubmitOptions) (uint16, uint16, error) {
	enabled, err := chain.CommitRevealEnabled(netuid)
	if err != nil {
		return 0, 0, err
	}
	if !enabled {
		return 0, 0, fmt.Errorf("crv4: commit-reveal is disabled on netuid %d (use set_weights instead)", netuid)
	}
	version := CommitRevealVersion4
	if opts.CommitRevealVersion != nil {
		version = *opts.CommitRevealVersion
	} else if version, err = chain.CommitRevealVersion(); err != nil {
		return 0, 0, err
	}
	maxWeightLimit := uint16(U16Max)
	if opts.MaxWeightLimit != nil {
		maxWeightLimit = *opts.MaxWeightLimit
	} else if maxWeightLimit, err = chain.MaxWeightsLimit(netuid); err != nil {
		return 0, 0, err
	}
	return version, maxWeightLimit, nil
}

func prepareWeightsU16(ctx context.Context, chain *Chain, kp *Keypair, netuid uint16, u16uids, u16vals []uint16, version uint16, opts SubmitOptions) (*PreparedSubmission, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	blockTime := opts.BlockTimeSecs
	if blockTime == 0 {
		blockTime = 12.0
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
	state, preparedHash, err := chain.EpochScheduleStateFinalized(netuid)
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

	// --- exact signed commit extrinsic ---
	nonce, err := chain.AccountNonce(kp.Address())
	if err != nil {
		return nil, err
	}
	ext, err := chain.NewCommitExtrinsic(kp, netuid, opts.Mecid, ciphertext, round, version, nonce)
	if err != nil {
		return nil, err
	}
	raw, err := codec.Encode(*ext)
	if err != nil {
		return nil, err
	}
	txHash := blake2b.Sum256(raw)
	cipherHash := sha256.Sum256(ciphertext)
	hotkey := kp.PublicKey()
	prepared := &PreparedSubmission{
		Schema: PreparedSubmissionSchema, Netuid: netuid, Mecid: opts.Mecid,
		HotkeyHex: "0x" + hex.EncodeToString(hotkey[:]), VersionKey: opts.VersionKey,
		CommitRevealVersion: version, AccountNonce: nonce,
		PreparedAtBlock: state.CurrentBlock, PreparedAtBlockHash: preparedHash.Hex(), SubnetEpoch: state.SubnetEpochIndex,
		RevealRound: round, RevealBlock: revealBlock,
		UIDs: append([]uint16(nil), u16uids...), Values: append([]uint16(nil), u16vals...),
		PayloadHex: codec.HexEncodeToString(encoded), CiphertextHex: codec.HexEncodeToString(ciphertext),
		CiphertextSHA256: "0x" + hex.EncodeToString(cipherHash[:]),
		ExtrinsicHex:     codec.HexEncodeToString(raw), ExtrinsicHash: types.Hash(txHash).Hex(),
	}
	if _, err := prepared.Validate(); err != nil {
		return nil, err
	}
	return prepared, nil
}

// SubmitPrepared broadcasts one already-signed CRv4 extrinsic and returns a
// dispatch-successful canonical finalized receipt. It never allocates a new
// nonce or regenerates randomized ciphertext.
func SubmitPrepared(ctx context.Context, chain *Chain, prepared *PreparedSubmission) (*SubmitResult, error) {
	_, err := prepared.Validate()
	if err != nil {
		return nil, err
	}
	receipt, err := chain.SubmitRawAndWatchFinalized(ctx, prepared.ExtrinsicHex)
	if err != nil {
		return nil, err
	}
	return &SubmitResult{
		TxHash: receipt.ExtrinsicHash, RevealRound: prepared.RevealRound,
		RevealBlock: prepared.RevealBlock, Uids: append([]uint16(nil), prepared.UIDs...),
		Values:         append([]uint16(nil), prepared.Values...),
		CiphertextLen:  len(mustDecodeHex(prepared.CiphertextHex)),
		FinalizedBlock: receipt.BlockNumber, FinalizedBlockHash: receipt.BlockHash,
	}, nil
}

func mustDecodeHex(value string) []byte {
	raw, _ := codec.HexDecodeString(value)
	return raw
}
