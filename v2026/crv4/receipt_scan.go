// Receipt scan results retain the exact finalized coverage boundary. A missing
// receipt without completed coverage is unknown, never authenticated absence.
package crv4

import (
	"context"
	"net"
	"os"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Recovery owners persist one complete chunk, not one record per block and
// not an unbounded prefix that a late read timeout could discard.
const ReceiptScanChunkBlockLimit uint64 = 128

// The requested range does not attest earlier absence. A recovery owner must
// authenticate its stored original-attempt checkpoint before selecting First.
// PreviousHash, when present, binds this range to that exact preceding block.
type FinalizedExtrinsicScanRange struct {
	First         uint64
	PreviousHash  types.Hash
	MaximumBlocks uint64
}

// Only the complete-body scanner constructs this witness. Exported accessors
// copy values; callers cannot turn a newer head into old-prefix coverage.
// A bounded failed scan may retain completed earlier bodies alongside its
// error. Callers must still handle that error and never widen this prefix.
type FinalizedExtrinsicScan struct {
	from          uint64
	previousHash  types.Hash
	extrinsicHash types.Hash
	finalizedHash types.Hash
	finalizedAt   uint64
	throughHash   types.Hash
	through       uint64
	absent        bool
	receipt       *FinalizedExtrinsic
}

// Input identity is retained by the authenticated reader, so a cache writer
// cannot attach coverage for another transaction or an unscanned range gap.
func (self *FinalizedExtrinsicScan) Request() (types.Hash, uint64, types.Hash) {
	if self == nil {
		return types.Hash{}, 0, types.Hash{}
	}
	return self.extrinsicHash, self.from, self.previousHash
}

// Membership still needs exact-runtime dispatch and source-event validation.
func (self *FinalizedExtrinsicScan) Receipt() *FinalizedExtrinsic {
	if self == nil || self.receipt == nil {
		return nil
	}
	copy := *self.receipt
	return &copy
}

// The range is inclusive. False means that no negative conclusion exists,
// including a prepared block later than the currently finalized head.
func (self *FinalizedExtrinsicScan) AbsenceBoundary() (uint64, types.Hash, bool) {
	if self == nil || !self.absent || self.receipt != nil || self.from > self.through {
		return 0, types.Hash{}, false
	}
	return self.through, self.throughHash, true
}

// The captured head may be later than completed coverage or earlier than a
// retained checkpoint. It is an observed identity, never a proof of absence.
func (self *FinalizedExtrinsicScan) FinalizedBoundary() (uint64, types.Hash) {
	if self == nil {
		return 0, types.Hash{}
	}
	return self.finalizedAt, self.finalizedHash
}

// Only full coverage through the captured head permits a caller to compare a
// nonce there. Chunk callers can persist earlier completed absence and resume.
func (self *FinalizedExtrinsicScan) ReachedFinalizedBoundary() bool {
	return self != nil && self.absent && self.receipt == nil && self.through == self.finalizedAt && self.throughHash == self.finalizedHash
}

// A proved contradiction never exports partial negative evidence. Only pure
// unavailable/transport leaves retain completed earlier bodies; diagnostic text
// and unowned EOF are deliberately insufficient for this classification.
func receiptScanUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if HasSubstrateReadTransportCause(err) {
		return RetryableSubstrateReadTransportError(err)
	}
	if _, ok := err.(*ReceiptEvidenceUnavailableError); ok {
		return true
	}
	if err == context.DeadlineExceeded || err == context.Canceled {
		return true
	}
	switch cause := err.(type) {
	case *os.PathError:
		return false
	case net.Error:
		return cause.Timeout()
	case interface{ Unwrap() []error }:
		children := cause.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !receiptScanUnavailable(child) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return receiptScanUnavailable(cause.Unwrap())
	}
	return false
}
