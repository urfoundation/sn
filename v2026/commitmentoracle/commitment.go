// Package commitmentoracle is the SN25 mainnet commitment oracle: a long-running
// service that mirrors finalized native fleet commitments into
// STCoordinator.mirrorCommitment, which bindFleetMember requires
// (evm/src/STCoordinator.sol mirrorCommitment, bindFleetMember).
//
// The only input is finalized chain state. Native commitments are read from
// Commitments.CommitmentOf(netuid, *) at the subtensor finalized head; only the
// release fleet format (exactly one Sha256 field, crv4.DecodeFleetCommitment*)
// is considered, and the format alone is not enough: a validator's source
// commitment has the same format, so role.go also requires a registered hotkey
// without a validator permit and a standalone fleet set_commitment in the
// commitment block. The arguments are the hotkey, the commitment hash, the
// registration's commitment block and that block's Substrate hash, which is
// what sim-testnet/fleet.go mirrorFleetCommitment sends on the closed testnet.
// Coordinator state (active oracle, mirrored records, policy) is read at the
// EVM finalized block, and a mirror counts as done only once its receipt is
// finalized.
package commitmentoracle

import (
	"errors"
	"fmt"

	"github.com/urfoundation/sn/v2026/crv4"
)

// The exact byte length of a one-field Sha256 registration: u64 deposit, u32
// block, compact vec length 1, the Sha256 variant and the 32-byte hash.
const fleetRegistrationBytes = 8 + 4 + 1 + 1 + 32

// Why a native commitment is not mirrored. The values are stable metric and
// log labels.
type skipReason string

const (
	skipNone           skipReason = ""
	skipNotFleetFormat skipReason = "not_fleet_format"
	skipExpired        skipReason = "expired"
	skipMirrored       skipReason = "mirrored"
	skipNewerMirrored  skipReason = "newer_mirrored"
	skipConflict       skipReason = "conflict"
	skipInconsistent   skipReason = "inconsistent"
	// The hotkey holds a validator permit (role.go).
	skipValidator skipReason = "validator"
	// The hotkey is not registered on the netuid.
	skipUnregistered skipReason = "unregistered"
	// The commitment block has no standalone fleet set_commitment from the
	// hotkey, e.g. a validator source commitment's batch.
	skipNoFleetEvidence skipReason = "no_fleet_evidence"
)

// One raw CommitmentOf(netuid, hotkey) storage value read at a finalized head.
type rawCommitment struct {
	hotkey [32]byte
	raw    []byte
}

// A native commitment in the release fleet format.
type fleetCommitment struct {
	hotkey          [32]byte
	commitmentHash  [32]byte
	commitmentBlock uint64
}

// The exact mirrorCommitment(hotkey, commitmentHash, finalizedBlock,
// finalizedBlockHash) arguments.
type mirrorArgs struct {
	hotkey             [32]byte
	commitmentHash     [32]byte
	finalizedBlock     uint64
	finalizedBlockHash [32]byte
}

// The coordinator's mirroredCommitments(hotkey) record.
type mirroredRecord struct {
	commitmentHash     [32]byte
	finalizedBlockHash [32]byte
	finalizedBlock     uint64
}

// Decodes one storage value. Anything other than the exact one-field Sha256
// registration (old multi-field entries, Raw data, a trailing Sha256 after other
// fields, a zero hash or block) is not fleet format and is never mirrored.
func parseFleetCommitment(entry rawCommitment) (fleetCommitment, error) {
	if entry.hotkey == ([32]byte{}) {
		return fleetCommitment{}, errors.New("commitment hotkey is zero")
	}
	if len(entry.raw) != fleetRegistrationBytes {
		return fleetCommitment{}, fmt.Errorf("registration has %d bytes, want %d", len(entry.raw), fleetRegistrationBytes)
	}
	commitmentHash, commitmentBlock, err := crv4.DecodeFleetCommitmentRegistrationWithBlockV454(entry.raw)
	if err != nil {
		return fleetCommitment{}, err
	}
	return fleetCommitment{hotkey: entry.hotkey, commitmentHash: commitmentHash, commitmentBlock: commitmentBlock}, nil
}

// Decides whether a fleet commitment still needs a mirror, given the native
// finalized head, the coordinator's commitmentMaxAgeBlocks and the record
// mirrored at the EVM finalized block. It mirrors the contract's own rules:
// an older block reverts StaleCommitment, the identical record is a no-op, and
// a binding against a commitment older than the max age reverts anyway.
func mirrorDecision(commitment fleetCommitment, nativeFinalized uint64, commitmentMaxAgeBlocks uint64, mirrored mirroredRecord) skipReason {
	if commitment.commitmentBlock > nativeFinalized {
		return skipInconsistent
	}
	if mirrored.finalizedBlock > commitment.commitmentBlock {
		return skipNewerMirrored
	}
	if mirrored.finalizedBlock == commitment.commitmentBlock && mirrored.finalizedBlock != 0 {
		if mirrored.commitmentHash == commitment.commitmentHash {
			return skipMirrored
		}
		return skipConflict
	}
	if nativeFinalized > commitment.commitmentBlock+commitmentMaxAgeBlocks {
		return skipExpired
	}
	return skipNone
}

// Derives the mirror arguments from finalized state only. lastCommitmentRaw is
// LastCommitment(netuid, hotkey) at the same finalized head and must name the
// registration's block; blockHash is the canonical Substrate hash at the
// commitment block and headerNumber the number in that hash's header.
func deriveMirrorArgs(commitment fleetCommitment, nativeFinalized uint64, lastCommitmentRaw []byte, blockHash [32]byte, headerNumber uint64) (mirrorArgs, error) {
	if commitment.commitmentBlock == 0 || commitment.commitmentBlock > nativeFinalized {
		return mirrorArgs{}, fmt.Errorf("commitment block %d is not at or below the finalized head %d", commitment.commitmentBlock, nativeFinalized)
	}
	lastBlock, err := crv4.DecodeFleetCommitmentLastBlock(lastCommitmentRaw)
	if err != nil {
		return mirrorArgs{}, err
	}
	if lastBlock != commitment.commitmentBlock {
		return mirrorArgs{}, fmt.Errorf("registration block %d differs from LastCommitment %d", commitment.commitmentBlock, lastBlock)
	}
	if blockHash == ([32]byte{}) {
		return mirrorArgs{}, fmt.Errorf("no canonical hash at block %d", commitment.commitmentBlock)
	}
	if headerNumber != commitment.commitmentBlock {
		return mirrorArgs{}, fmt.Errorf("header of hash 0x%x is block %d, want %d", blockHash, headerNumber, commitment.commitmentBlock)
	}
	return mirrorArgs{
		hotkey:             commitment.hotkey,
		commitmentHash:     commitment.commitmentHash,
		finalizedBlock:     commitment.commitmentBlock,
		finalizedBlockHash: blockHash,
	}, nil
}

// Every field the oracle attests, as sim-testnet fleetMirrorMatches.
func (self mirrorArgs) matches(record mirroredRecord) bool {
	return record.commitmentHash == self.commitmentHash &&
		record.finalizedBlock == self.finalizedBlock &&
		record.finalizedBlockHash == self.finalizedBlockHash
}

func (self mirrorArgs) String() string {
	return fmt.Sprintf("hotkey=0x%x commitment=0x%x block=%d block_hash=0x%x", self.hotkey, self.commitmentHash, self.finalizedBlock, self.finalizedBlockHash)
}
