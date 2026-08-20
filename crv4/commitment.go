package crv4

// Runtime-447 commitments pallet support used by fleet promotion. The exact
// encoding is pinned to RaoFoundation/subtensor v447:
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
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

const (
	CommitmentsPalletName = "Commitments"
	CallSetCommitment     = "set_commitment"
	dataSHA256Variant     = byte(131)
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
	if observed.Hash != hash {
		return nil, fmt.Errorf("crv4: finalized commitment postcondition mismatch: got 0x%x want 0x%x", observed.Hash, hash)
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

// FleetCommitmentAt verifies the release's one-field Sha256 encoding directly
// from CommitmentOf storage. Registration ends with CommitmentInfo, so the
// suffix is unambiguous even if runtime Balance/BlockNumber widths evolve.
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
	wantLen := 1 + 1 + 32 // Vec length(1), Sha256 variant, hash
	if len(*raw) < wantLen {
		return nil, fmt.Errorf("crv4: malformed commitment registration (%d bytes)", len(*raw))
	}
	tail := []byte(*raw)[len(*raw)-wantLen:]
	if tail[0] != 0x04 || tail[1] != dataSHA256Variant {
		return nil, fmt.Errorf("crv4: commitment is not exactly one Sha256 field")
	}
	var commitmentHash [32]byte
	copy(commitmentHash[:], tail[2:])
	if commitmentHash == ([32]byte{}) {
		return nil, fmt.Errorf("crv4: finalized commitment hash is zero")
	}
	encoded, _ := EncodeFleetCommitmentInfo(commitmentHash)
	if !bytes.Equal(tail, encoded) {
		return nil, fmt.Errorf("crv4: non-canonical commitment encoding")
	}

	lastKey, err := types.CreateStorageKey(c.Meta, CommitmentsPalletName, "LastCommitment", encodeNetuid(netuid), hotkey[:])
	if err != nil {
		return nil, fmt.Errorf("crv4: last commitment storage key: %w", err)
	}
	var commitmentBlock types.BlockNumber
	ok, err := c.API.RPC.State.GetStorage(lastKey, &commitmentBlock, blockHash)
	if err != nil {
		return nil, fmt.Errorf("crv4: last commitment storage: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("crv4: commitment exists without LastCommitment")
	}
	header, err := c.API.RPC.Chain.GetHeader(blockHash)
	if err != nil {
		return nil, fmt.Errorf("crv4: finalized commitment header: %w", err)
	}
	return &FinalizedCommitment{Hash: commitmentHash, CommitmentBlock: uint64(commitmentBlock), FinalizedAt: uint64(header.Number), FinalizedHash: blockHash}, nil
}
