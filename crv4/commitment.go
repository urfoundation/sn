package crv4

// Runtime-452 commitments pallet support used by fleet promotion. The exact
// encoding is pinned to RaoFoundation/subtensor testnet commit
// da06f033663896ef2fdbbfc3ecc68ca908fba0f5:
//
//   Commitments.set_commitment(netuid: u16, info: CommitmentInfo)
//   CommitmentInfo { fields: BoundedVec<Data> }
//   Data::Sha256([u8; 32]) = 0x83 || hash
//
// A fleet publishes exactly one Sha256 field. The coordinator's narrowly
// scoped oracle mirrors that finalized native commitment before any fleet
// member binding can become active.

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

const (
	CommitmentsPalletName = "Commitments"
	CallSetCommitment     = "set_commitment"
	dataSHA256Variant     = byte(131)
	// Runtime v452 stores Registration<TaoBalance, MaxFields, BlockNumber>
	// as a transparent u64 deposit, u32 block, then CommitmentInfo. The
	// release lock pins all three types; accepting only the exact byte length
	// prevents a multi-field registration that merely ends in Sha256 from
	// masquerading as the one-field fleet commitment protocol.
	registrationV452PrefixBytes = 8 + 4
)

// SHA256CommitmentData is the only commitment Data variant accepted by the
// release fleet protocol. Restricting the Go type makes an accidental Raw or
// mutable multi-field commitment impossible in production code.
type SHA256CommitmentData struct {
	Hash [32]byte
}

func (d SHA256CommitmentData) Encode(encoder scale.Encoder) error {
	if err := encoder.PushByte(dataSHA256Variant); err != nil {
		return err
	}
	return encoder.Write(d.Hash[:])
}

// FleetCommitmentInfo SCALE-encodes as a compact-length vector followed by its
// fields. FleetCommitmentInfo always contains exactly one Sha256 field.
type FleetCommitmentInfo struct {
	Fields []SHA256CommitmentData
}

func NewFleetCommitmentInfo(hash [32]byte) (FleetCommitmentInfo, error) {
	if hash == ([32]byte{}) {
		return FleetCommitmentInfo{}, fmt.Errorf("crv4: fleet commitment hash is zero")
	}
	return FleetCommitmentInfo{Fields: []SHA256CommitmentData{{Hash: hash}}}, nil
}

// EncodeFleetCommitmentInfo is exposed for cross-language fixtures and plan
// evidence. For one field it is 0x04 || 0x83 || hash.
func EncodeFleetCommitmentInfo(hash [32]byte) ([]byte, error) {
	info, err := NewFleetCommitmentInfo(hash)
	if err != nil {
		return nil, err
	}
	return codec.Encode(info)
}

// Decode the exact runtime registration and retain its fixed-width storage
// block. Header block numbers use compact SCALE in GSRPC, while this field is
// a plain u32; conflating them silently changes values whose low bits select a
// compact mode.
func decodeFleetCommitmentRegistrationV452(raw []byte) ([32]byte, uint64, error) {
	var commitmentHash [32]byte
	wantInfoLen := 1 + 1 + len(commitmentHash) // Vec length(1), Sha256 variant, hash
	if len(raw) != registrationV452PrefixBytes+wantInfoLen {
		return commitmentHash, 0, fmt.Errorf("crv4: fleet commitment registration has %d bytes, want %d", len(raw), registrationV452PrefixBytes+wantInfoLen)
	}
	commitmentBlock := uint64(binary.LittleEndian.Uint32(raw[8:registrationV452PrefixBytes]))
	if commitmentBlock == 0 {
		return commitmentHash, 0, fmt.Errorf("crv4: fleet commitment registration block is zero")
	}
	info := raw[registrationV452PrefixBytes:]
	if info[0] != 0x04 || info[1] != dataSHA256Variant {
		return commitmentHash, 0, fmt.Errorf("crv4: commitment is not exactly one Sha256 field")
	}
	copy(commitmentHash[:], info[2:])
	if commitmentHash == ([32]byte{}) {
		return commitmentHash, 0, fmt.Errorf("crv4: finalized commitment hash is zero")
	}
	encoded, err := EncodeFleetCommitmentInfo(commitmentHash)
	if err != nil || !bytes.Equal(info, encoded) {
		return [32]byte{}, 0, fmt.Errorf("crv4: non-canonical commitment encoding")
	}
	return commitmentHash, commitmentBlock, nil
}

// DecodeFleetCommitmentRegistrationV452 accepts only the release protocol's
// one-field registration and exposes its canonical commitment hash.
func DecodeFleetCommitmentRegistrationV452(raw []byte) ([32]byte, error) {
	commitmentHash, _, err := decodeFleetCommitmentRegistrationV452(raw)
	return commitmentHash, err
}

// DecodeFleetCommitmentRegistrationV451 preserves the API introduced under
// the preceding runtime after the v452 audit proved the shape unchanged.
func DecodeFleetCommitmentRegistrationV451(raw []byte) ([32]byte, error) {
	return DecodeFleetCommitmentRegistrationV452(raw)
}

// DecodeFleetCommitmentRegistrationV447 preserves the first release-pinned
// API after the v452 audit proved the shape unchanged.
func DecodeFleetCommitmentRegistrationV447(raw []byte) ([32]byte, error) {
	return DecodeFleetCommitmentRegistrationV452(raw)
}

// Decode the pallet's fixed-width u32 storage value exactly. In particular,
// do not use types.BlockNumber: its SCALE decoder is intentionally compact for
// header fields and turns 0x9d7c7800 (7,896,221) into 7,975.
func decodeFleetCommitmentBlockV452(raw []byte) (uint64, error) {
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
	nonce, err := c.AccountNonce(kp.Address())
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
	observed, err := c.FleetCommitmentAt(netuid, kp.PublicKey(), receipt.BlockHash)
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
	hash, err := c.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return nil, fmt.Errorf("crv4: finalized head: %w", err)
	}
	return c.FleetCommitmentAt(netuid, hotkey, hash)
}

// FleetCommitmentAt verifies the release's exact runtime-452 one-field Sha256
// registration directly from CommitmentOf storage.
func (c *Chain) FleetCommitmentAt(netuid uint16, hotkey [32]byte, blockHash types.Hash) (*FinalizedCommitment, error) {
	if netuid == 0 || hotkey == ([32]byte{}) || blockHash == (types.Hash{}) {
		return nil, fmt.Errorf("crv4: invalid commitment lookup identity/block")
	}
	key, err := types.CreateStorageKey(c.Meta, CommitmentsPalletName, "CommitmentOf", encodeNetuid(netuid), hotkey[:])
	if err != nil {
		return nil, fmt.Errorf("crv4: commitment storage key: %w", err)
	}
	raw, err := c.API.RPC.State.GetStorageRaw(key, blockHash)
	if err != nil {
		return nil, fmt.Errorf("crv4: commitment storage: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("crv4: no finalized commitment for hotkey 0x%x on netuid %d", hotkey, netuid)
	}
	commitmentHash, registrationBlock, err := decodeFleetCommitmentRegistrationV452([]byte(*raw))
	if err != nil {
		return nil, err
	}

	lastKey, err := types.CreateStorageKey(c.Meta, CommitmentsPalletName, "LastCommitment", encodeNetuid(netuid), hotkey[:])
	if err != nil {
		return nil, fmt.Errorf("crv4: last commitment storage key: %w", err)
	}
	lastRaw, err := c.API.RPC.State.GetStorageRaw(lastKey, blockHash)
	if err != nil {
		return nil, fmt.Errorf("crv4: last commitment storage: %w", err)
	}
	if lastRaw == nil {
		return nil, fmt.Errorf("crv4: commitment exists without LastCommitment")
	}
	commitmentBlock, err := decodeFleetCommitmentBlockV452([]byte(*lastRaw))
	if err != nil {
		return nil, err
	}
	if registrationBlock != commitmentBlock {
		return nil, fmt.Errorf("crv4: commitment registration block %d differs from LastCommitment %d", registrationBlock, commitmentBlock)
	}
	header, err := c.API.RPC.Chain.GetHeader(blockHash)
	if err != nil {
		return nil, fmt.Errorf("crv4: finalized commitment header: %w", err)
	}
	return &FinalizedCommitment{Hash: commitmentHash, CommitmentBlock: uint64(commitmentBlock), FinalizedAt: uint64(header.Number), FinalizedHash: blockHash}, nil
}
