package crv4

// Runtime-454+ commitments pallet support. Runtime 454 commit
// 14cde6410fe8ec81a940e290c56f94a632a0988d retains the v452 storage
// encoding; runtime 478 metadata still has the same Data enum:
//
//   Commitments.set_commitment(netuid: u16, info: CommitmentInfo)
//   CommitmentInfo { fields: BoundedVec<Data> }
//   Data::Raw40([u8; 40])   = 0x29 || 40 bytes (Raw<n> is variant n+1)
//   Data::Sha256([u8; 32])  = 0x83 || hash
//
// TestCommitmentDataVariantsRuntime478 pins both indices against the
// runtime-478 metadata; pallet_commitments types.rs is unchanged in runtime
// 479 (release-479, e66c7eed).
//
// Two writers share one CommitmentOf(netuid, hotkey) slot format family, and
// they must never be confused (SN25 validator mirror incident, 2026-10-15):
//
//   - A fleet commitment is exactly one Raw40 field holding the ASCII tag
//     "urfleet1" followed by the 32-byte fleet manifest commitment hash. Only
//     this format is a fleet commitment; the commitment oracle mirrors only
//     it and extracts the hash.
//   - A validator source commitment is exactly one Sha256 field (unchanged),
//     written inside the validator's Utility.batch_all with its CRv4 commit.
//
// A one-field Sha256 commitment was the fleet format before the tag. It is
// now a legacy fleet commitment: fleet readers refuse it with
// ErrLegacyFleetCommitment so the miner republishes in the tagged format.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

const (
	CommitmentsPalletName = "Commitments"
	CallSetCommitment     = "set_commitment"
	dataSHA256Variant     = byte(131)
	// Data::Raw40, the Raw variant of exactly len(FleetCommitmentTag)+32 bytes.
	dataRaw40Variant = byte(41)
	// Runtime 454 retains Registration<TaoBalance, MaxFields, BlockNumber>
	// as a transparent u64 deposit, u32 block, then CommitmentInfo. The
	// release lock pins all three types; accepting only the exact byte length
	// prevents a multi-field registration that merely ends in a valid field
	// from masquerading as a one-field commitment.
	registrationFixedU32PrefixBytes = 8 + 4
)

// FleetCommitmentTag is the domain-separation tag of a fleet commitment, the
// first 8 bytes of its single Raw40 field. The validator source commitment
// is a Sha256 field and can never carry it.
const FleetCommitmentTag = "urfleet1"

// FleetCommitmentFieldBytes is the Raw40 payload: the tag then the hash.
const FleetCommitmentFieldBytes = len(FleetCommitmentTag) + 32

// ErrLegacyFleetCommitment marks a one-field Sha256 commitment read where a
// tagged fleet commitment is required: either a fleet published before the
// tag, which must republish, or a validator source commitment.
var ErrLegacyFleetCommitment = errors.New("crv4: commitment is a one-field Sha256 (legacy fleet or validator source), not a tagged fleet commitment; republish with provider fleet publish")

// FleetCommitmentData is the only commitment Data the fleet protocol writes:
// Data::Raw40(FleetCommitmentTag || Hash). Restricting the Go type makes an
// accidental Sha256, other Raw or multi-field fleet commitment impossible.
type FleetCommitmentData struct {
	Hash [32]byte
}

func (d FleetCommitmentData) Encode(encoder scale.Encoder) error {
	if err := encoder.PushByte(dataRaw40Variant); err != nil {
		return err
	}
	if err := encoder.Write([]byte(FleetCommitmentTag)); err != nil {
		return err
	}
	return encoder.Write(d.Hash[:])
}

// FleetCommitmentInfo SCALE-encodes as a compact-length vector followed by its
// fields. It always contains exactly one tagged Raw40 field.
type FleetCommitmentInfo struct {
	Fields []FleetCommitmentData
}

func NewFleetCommitmentInfo(hash [32]byte) (FleetCommitmentInfo, error) {
	if hash == ([32]byte{}) {
		return FleetCommitmentInfo{}, fmt.Errorf("crv4: fleet commitment hash is zero")
	}
	return FleetCommitmentInfo{Fields: []FleetCommitmentData{{Hash: hash}}}, nil
}

// EncodeFleetCommitmentInfo is exposed for cross-language fixtures and plan
// evidence. It is 0x04 || 0x29 || "urfleet1" || hash, 42 bytes.
func EncodeFleetCommitmentInfo(hash [32]byte) ([]byte, error) {
	info, err := NewFleetCommitmentInfo(hash)
	if err != nil {
		return nil, err
	}
	return codec.Encode(info)
}

// DecodeFleetCommitmentInfo accepts exactly EncodeFleetCommitmentInfo's
// bytes and returns the hash. A one-field Sha256 info is
// ErrLegacyFleetCommitment; anything else is an error.
func DecodeFleetCommitmentInfo(info []byte) ([32]byte, error) {
	var hash [32]byte
	if len(info) == 2+32 && info[0] == 0x04 && info[1] == dataSHA256Variant {
		return hash, ErrLegacyFleetCommitment
	}
	if len(info) != 2+FleetCommitmentFieldBytes {
		return hash, fmt.Errorf("crv4: fleet commitment info has %d bytes, want %d", len(info), 2+FleetCommitmentFieldBytes)
	}
	if info[0] != 0x04 || info[1] != dataRaw40Variant {
		return hash, fmt.Errorf("crv4: commitment is not exactly one Raw40 field")
	}
	if string(info[2:2+len(FleetCommitmentTag)]) != FleetCommitmentTag {
		return hash, fmt.Errorf("crv4: Raw40 commitment does not carry the %q fleet tag", FleetCommitmentTag)
	}
	copy(hash[:], info[2+len(FleetCommitmentTag):])
	if hash == ([32]byte{}) {
		return hash, fmt.Errorf("crv4: finalized commitment hash is zero")
	}
	encoded, err := EncodeFleetCommitmentInfo(hash)
	if err != nil || !bytes.Equal(info, encoded) {
		return [32]byte{}, fmt.Errorf("crv4: non-canonical commitment encoding")
	}
	return hash, nil
}

// SourceCommitmentData is the validator source commitment's field,
// Data::Sha256(Hash). Its bytes are unchanged by the fleet tag.
type SourceCommitmentData struct {
	Hash [32]byte
}

func (d SourceCommitmentData) Encode(encoder scale.Encoder) error {
	if err := encoder.PushByte(dataSHA256Variant); err != nil {
		return err
	}
	return encoder.Write(d.Hash[:])
}

// SourceCommitmentInfo always contains exactly one Sha256 field.
type SourceCommitmentInfo struct {
	Fields []SourceCommitmentData
}

func NewSourceCommitmentInfo(hash [32]byte) (SourceCommitmentInfo, error) {
	if hash == ([32]byte{}) {
		return SourceCommitmentInfo{}, fmt.Errorf("crv4: source commitment hash is zero")
	}
	return SourceCommitmentInfo{Fields: []SourceCommitmentData{{Hash: hash}}}, nil
}

// EncodeSourceCommitmentInfo is 0x04 || 0x83 || hash, 34 bytes. It refuses to
// return bytes that decode as a fleet commitment.
func EncodeSourceCommitmentInfo(hash [32]byte) ([]byte, error) {
	info, err := NewSourceCommitmentInfo(hash)
	if err != nil {
		return nil, err
	}
	encoded, err := codec.Encode(info)
	if err != nil {
		return nil, err
	}
	if err := checkNotFleetCommitment(encoded); err != nil {
		return nil, err
	}
	return encoded, nil
}

// Guards every source writer: source commitment bytes must never be a fleet
// commitment, or the commitment oracle would mirror a validator's source.
func checkNotFleetCommitment(info []byte) error {
	if _, err := DecodeFleetCommitmentInfo(info); err == nil {
		return errors.New("crv4: source commitment encodes as a tagged fleet commitment")
	}
	if len(info) < 2 || info[1] == dataRaw40Variant {
		return errors.New("crv4: source commitment uses the fleet Data variant")
	}
	return nil
}

// Decodes the source commitment's one-field Sha256 info exactly.
func decodeSourceCommitmentInfo(info []byte) ([32]byte, error) {
	var hash [32]byte
	if len(info) != 2+len(hash) {
		return hash, fmt.Errorf("crv4: source commitment info has %d bytes, want %d", len(info), 2+len(hash))
	}
	if info[0] != 0x04 || info[1] != dataSHA256Variant {
		return hash, fmt.Errorf("crv4: commitment is not exactly one Sha256 field")
	}
	copy(hash[:], info[2:])
	if hash == ([32]byte{}) {
		return hash, fmt.Errorf("crv4: finalized commitment hash is zero")
	}
	encoded, err := codec.Encode(SourceCommitmentInfo{Fields: []SourceCommitmentData{{Hash: hash}}})
	if err != nil || !bytes.Equal(info, encoded) {
		return [32]byte{}, fmt.Errorf("crv4: non-canonical commitment encoding")
	}
	return hash, nil
}

// Splits the exact runtime registration into its fixed-width storage block
// and info, then decodes the info. Header block numbers use compact SCALE in
// GSRPC, while this field is a plain u32; conflating them silently changes
// values whose low bits select a compact mode.
func decodeCommitmentRegistrationFixedU32(raw []byte, decodeInfo func([]byte) ([32]byte, error)) ([32]byte, uint64, error) {
	if len(raw) <= registrationFixedU32PrefixBytes {
		return [32]byte{}, 0, fmt.Errorf("crv4: commitment registration has %d bytes", len(raw))
	}
	// The info first, so a one-field Sha256 is ErrLegacyFleetCommitment
	// whatever its block.
	commitmentHash, err := decodeInfo(raw[registrationFixedU32PrefixBytes:])
	if err != nil {
		return [32]byte{}, 0, err
	}
	commitmentBlock := uint64(binary.LittleEndian.Uint32(raw[8:registrationFixedU32PrefixBytes]))
	if commitmentBlock == 0 {
		return [32]byte{}, 0, fmt.Errorf("crv4: commitment registration block is zero")
	}
	return commitmentHash, commitmentBlock, nil
}

// Decodes a tagged fleet registration exactly.
func decodeFleetCommitmentRegistrationFixedU32(raw []byte) ([32]byte, uint64, error) {
	return decodeCommitmentRegistrationFixedU32(raw, DecodeFleetCommitmentInfo)
}

// Decodes a validator source (one-field Sha256) registration exactly.
func decodeSourceCommitmentRegistrationFixedU32(raw []byte) ([32]byte, uint64, error) {
	return decodeCommitmentRegistrationFixedU32(raw, decodeSourceCommitmentInfo)
}

// DecodeFleetCommitmentRegistrationV454 accepts only the tagged one-field
// fleet registration and exposes its commitment hash.
func DecodeFleetCommitmentRegistrationV454(raw []byte) ([32]byte, error) {
	commitmentHash, _, err := decodeFleetCommitmentRegistrationFixedU32(raw)
	return commitmentHash, err
}

// DecodeFleetCommitmentRegistrationWithBlockV454 is
// DecodeFleetCommitmentRegistrationV454 that also returns the registration's
// fixed-width u32 commitment block. The commitment oracle mirrors that block.
func DecodeFleetCommitmentRegistrationWithBlockV454(raw []byte) ([32]byte, uint64, error) {
	return decodeFleetCommitmentRegistrationFixedU32(raw)
}

// DecodeSourceCommitmentRegistrationWithBlock decodes a validator source
// commitment's one-field Sha256 registration and its block.
func DecodeSourceCommitmentRegistrationWithBlock(raw []byte) ([32]byte, uint64, error) {
	return decodeSourceCommitmentRegistrationFixedU32(raw)
}

// DecodeFleetCommitmentLastBlock decodes LastCommitment's exact fixed-width u32
// storage value, which must equal the registration's block.
func DecodeFleetCommitmentLastBlock(raw []byte) (uint64, error) {
	return decodeFleetCommitmentBlockFixedU32(raw)
}

// DecodeFleetCommitmentRegistrationV453 preserves the preceding release API
// name; it now requires the tagged fleet format.
func DecodeFleetCommitmentRegistrationV453(raw []byte) ([32]byte, error) {
	return DecodeFleetCommitmentRegistrationV454(raw)
}

// DecodeFleetCommitmentRegistrationV452 preserves the v452 API name; it now
// requires the tagged fleet format.
func DecodeFleetCommitmentRegistrationV452(raw []byte) ([32]byte, error) {
	return DecodeFleetCommitmentRegistrationV454(raw)
}

// DecodeFleetCommitmentRegistrationV451 preserves the v451 API name; it now
// requires the tagged fleet format.
func DecodeFleetCommitmentRegistrationV451(raw []byte) ([32]byte, error) {
	return DecodeFleetCommitmentRegistrationV454(raw)
}

// DecodeFleetCommitmentRegistrationV447 preserves the first release-pinned
// API name; it now requires the tagged fleet format.
func DecodeFleetCommitmentRegistrationV447(raw []byte) ([32]byte, error) {
	return DecodeFleetCommitmentRegistrationV454(raw)
}

// Decode the pallet's fixed-width u32 storage value exactly. In particular,
// do not use types.BlockNumber: its SCALE decoder is intentionally compact for
// header fields and turns 0x9d7c7800 (7,896,221) into 7,975.
func decodeFleetCommitmentBlockFixedU32(raw []byte) (uint64, error) {
	if len(raw) != 4 {
		return 0, fmt.Errorf("crv4: LastCommitment has %d bytes, want 4", len(raw))
	}
	commitmentBlock := uint64(binary.LittleEndian.Uint32(raw))
	if commitmentBlock == 0 {
		return 0, fmt.Errorf("crv4: LastCommitment is zero")
	}
	return commitmentBlock, nil
}

// NewSetFleetCommitmentCall builds the runtime metadata-bound call.
func (c *Chain) NewSetFleetCommitmentCall(netuid uint16, hash [32]byte) (types.Call, error) {
	if netuid == 0 {
		return types.Call{}, fmt.Errorf("crv4: commitment netuid is zero")
	}
	info, err := NewFleetCommitmentInfo(hash)
	if err != nil {
		return types.Call{}, err
	}
	call, err := types.NewCall(c.Meta, CommitmentsPalletName+"."+CallSetCommitment, types.NewU16(netuid), info)
	if err != nil {
		return types.Call{}, fmt.Errorf("crv4: build commitments call: %w", err)
	}
	return call, nil
}

// NewSetSourceCommitmentCall builds the validator source commitment's
// Commitments.set_commitment(netuid, [Sha256(hash)]) call. Its bytes are the
// pre-tag format, unchanged; it never builds a fleet commitment.
func (c *Chain) NewSetSourceCommitmentCall(netuid uint16, hash [32]byte) (types.Call, error) {
	if netuid == 0 {
		return types.Call{}, fmt.Errorf("crv4: commitment netuid is zero")
	}
	if _, err := EncodeSourceCommitmentInfo(hash); err != nil {
		return types.Call{}, err
	}
	info, err := NewSourceCommitmentInfo(hash)
	if err != nil {
		return types.Call{}, err
	}
	call, err := types.NewCall(c.Meta, CommitmentsPalletName+"."+CallSetCommitment, types.NewU16(netuid), info)
	if err != nil {
		return types.Call{}, fmt.Errorf("crv4: build commitments call: %w", err)
	}
	return call, nil
}

// FinalizedCommitment identifies the canonical native record mirrored into the
// coordinator. CommitmentBlock is the block stored by the pallet; FinalizedAt
// is the finalized head at which the exact storage bytes were verified.
type FinalizedCommitment struct {
	Hash            [32]byte
	CommitmentBlock uint64
	FinalizedAt     uint64
	FinalizedHash   types.Hash
	ExtrinsicHash   types.Hash
}

// Require the finalized storage registration to come from the exact block
// containing the approved write. Hash equality alone is insufficient when a
// prior identical commitment has aged past the coordinator's freshness bound.
func ValidateFleetCommitmentWrite(expected [32]byte, finalizedBlock uint64, observed *FinalizedCommitment) error {
	if expected == ([32]byte{}) || finalizedBlock == 0 || observed == nil {
		return fmt.Errorf("crv4: finalized commitment write proof is incomplete")
	}
	if observed.Hash != expected {
		return fmt.Errorf("crv4: finalized commitment postcondition mismatch: got 0x%x want 0x%x", observed.Hash, expected)
	}
	if observed.CommitmentBlock != finalizedBlock || observed.FinalizedAt != finalizedBlock || observed.FinalizedHash == (types.Hash{}) {
		return fmt.Errorf("crv4: finalized commitment block proof is %d at %d, want exact write block %d", observed.CommitmentBlock, observed.FinalizedAt, finalizedBlock)
	}
	return nil
}

// SetFleetCommitment submits the hotkey-signed commitment, waits for finality,
// and proves the exact hash in finalized storage. This postcondition also
// catches an included DispatchError event without requiring runtime-specific
// event decoding.
func (c *Chain) SetFleetCommitment(ctx context.Context, kp *Keypair, netuid uint16, hash [32]byte) (*FinalizedCommitment, error) {
	call, err := c.NewSetFleetCommitmentCall(netuid, hash)
	if err != nil {
		return nil, err
	}
	nonce, err := c.AccountNonceContext(ctx, kp.Address())
	if err != nil {
		return nil, err
	}
	ext, err := c.NewSignedExtrinsic(kp, call, nonce)
	if err != nil {
		return nil, err
	}
	receipt, err := c.SubmitAndWatchFinalized(ctx, ext)
	if err != nil {
		return nil, err
	}
	observed, err := c.FleetCommitmentAtContext(ctx, netuid, kp.PublicKey(), receipt.BlockHash)
	if err != nil {
		return nil, err
	}
	if err := ValidateFleetCommitmentWrite(hash, receipt.BlockNumber, observed); err != nil {
		return nil, err
	}
	observed.ExtrinsicHash = receipt.ExtrinsicHash
	return observed, nil
}

// FleetCommitmentFinalized reads against the current finalized head.
func (c *Chain) FleetCommitmentFinalized(netuid uint16, hotkey [32]byte) (*FinalizedCommitment, error) {
	return c.FleetCommitmentFinalizedContext(context.Background(), netuid, hotkey)
}

// FleetCommitmentFinalizedContext selects and reads one finalized commitment
// without allowing a status caller's cancellation boundary to be discarded.
func (c *Chain) FleetCommitmentFinalizedContext(ctx context.Context, netuid uint16, hotkey [32]byte) (*FinalizedCommitment, error) {
	hash, err := FinalizedHeadContext(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("crv4: finalized head: %w", err)
	}
	return c.FleetCommitmentAtContext(ctx, netuid, hotkey, hash)
}

// FleetCommitmentAtContext verifies the exact tagged one-field fleet
// registration through exact-block state and header RPCs which honor ctx. A
// one-field Sha256 slot is ErrLegacyFleetCommitment.
func (c *Chain) FleetCommitmentAtContext(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash types.Hash) (*FinalizedCommitment, error) {
	return c.commitmentAtContext(ctx, netuid, hotkey, blockHash, 0, decodeFleetCommitmentRegistrationFixedU32)
}

// SourceCommitmentAtContext verifies a validator source commitment's exact
// one-field Sha256 registration, as FleetCommitmentAtContext does a fleet's.
func (c *Chain) SourceCommitmentAtContext(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash types.Hash) (*FinalizedCommitment, error) {
	return c.commitmentAtContext(ctx, netuid, hotkey, blockHash, 0, decodeSourceCommitmentRegistrationFixedU32)
}

// LegacyFleetCommitmentAtContext reads a pre-tag fleet commitment (one
// Sha256 field). It exists only so a miner can close an old publication
// record; nothing may mirror or bind what it returns.
func (c *Chain) LegacyFleetCommitmentAtContext(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash types.Hash) (*FinalizedCommitment, error) {
	return c.commitmentAtContext(ctx, netuid, hotkey, blockHash, 0, decodeSourceCommitmentRegistrationFixedU32)
}

// Source receipts already authenticated their complete header and body. Their
// nonzero height avoids a duplicate full-header read; ordinary commitment
// callers authenticate their header below. Both close the canonical witness.
func (self *Chain) commitmentAtContext(ctx context.Context, netuid uint16, hotkey [32]byte, blockHash types.Hash, receiptNumber uint64, decode func([]byte) ([32]byte, uint64, error)) (*FinalizedCommitment, error) {
	if ctx == nil || self == nil || self.API == nil || self.API.Client == nil || self.Meta == nil ||
		netuid == 0 || hotkey == ([32]byte{}) || blockHash == (types.Hash{}) {
		return nil, fmt.Errorf("crv4: invalid commitment lookup identity/block")
	}
	key, err := types.CreateStorageKey(self.Meta, CommitmentsPalletName, "CommitmentOf", encodeNetuid(netuid), hotkey[:])
	if err != nil {
		return nil, fmt.Errorf("crv4: commitment storage key: %w", err)
	}
	raw, err := self.storageRawAtContext(ctx, key, blockHash)
	if err != nil {
		return nil, fmt.Errorf("crv4: commitment storage: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("crv4: no finalized commitment for hotkey 0x%x on netuid %d", hotkey, netuid)
	}
	commitmentHash, registrationBlock, err := decode([]byte(*raw))
	if err != nil {
		return nil, err
	}

	lastKey, err := types.CreateStorageKey(self.Meta, CommitmentsPalletName, "LastCommitment", encodeNetuid(netuid), hotkey[:])
	if err != nil {
		return nil, fmt.Errorf("crv4: last commitment storage key: %w", err)
	}
	lastRaw, err := self.storageRawAtContext(ctx, lastKey, blockHash)
	if err != nil {
		return nil, fmt.Errorf("crv4: last commitment storage: %w", err)
	}
	if lastRaw == nil {
		return nil, fmt.Errorf("crv4: commitment exists without LastCommitment")
	}
	commitmentBlock, err := decodeFleetCommitmentBlockFixedU32([]byte(*lastRaw))
	if err != nil {
		return nil, err
	}
	if registrationBlock != commitmentBlock {
		return nil, fmt.Errorf("crv4: commitment registration block %d differs from LastCommitment %d", registrationBlock, commitmentBlock)
	}
	if receiptNumber == 0 {
		number, _, err := self.ReceiptHeaderAtContext(ctx, blockHash)
		if err != nil {
			return nil, fmt.Errorf("crv4: finalized commitment header: %w", err)
		}
		if number == 0 {
			return nil, fmt.Errorf("crv4: finalized commitment header has zero block number")
		}
		receiptNumber = number
	}
	if err := self.CheckCanonicalBlockAtContext(ctx, blockHash, receiptNumber); err != nil {
		return nil, err
	}
	return &FinalizedCommitment{Hash: commitmentHash, CommitmentBlock: uint64(commitmentBlock), FinalizedAt: receiptNumber, FinalizedHash: blockHash}, nil
}

// FleetCommitmentAt preserves the contextless compatibility surface for
// historical readers. Release fleet paths use FleetCommitmentAtContext.
func (c *Chain) FleetCommitmentAt(netuid uint16, hotkey [32]byte, blockHash types.Hash) (*FinalizedCommitment, error) {
	return c.FleetCommitmentAtContext(context.Background(), netuid, hotkey, blockHash)
}
