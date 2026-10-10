package scale

import (
	"errors"
	"fmt"
	"io"
)

// ErrDecodeResourceLimit identifies exhausted local decoding capacity, not a
// claim that otherwise well-formed bytes are independently trusted.
var ErrDecodeResourceLimit = errors.New("scale decoding resource limit exceeded")

// DecoderLimits bounds reflected storage, collection sizes and recursive work.
// Custom decoders must reserve their own derived allocations as well. These
// count requested storage; allocator overhead is not a process memory limit.
type DecoderLimits struct {
	MaxCollectionElements uint64
	MaxAllocationBytes    uint64
	MaxDecodedValues      uint64
	MaxDepth              uint32
}

// One decode owns this state. Decoder's value copies share it; independent
// decoders never share counters. A failed budget cannot be reused or refunded.
type decodeBudget struct {
	limits                   DecoderLimits
	allocationBytesRemaining uint64
	decodedValuesRemaining   uint64
	depth                    uint32
	err                      error
}

// NewDecoderWithLimits opts one reader into finite, strictly positive limits.
// Existing NewDecoder callers retain their compatibility behavior.
func NewDecoderWithLimits(reader io.Reader, limits DecoderLimits) (*Decoder, error) {
	if reader == nil || limits.MaxCollectionElements == 0 || limits.MaxAllocationBytes == 0 || limits.MaxDecodedValues == 0 || limits.MaxDepth == 0 {
		return nil, errors.New("scale bounded decoder requires a reader and positive finite limits")
	}
	return &Decoder{reader: reader, budget: &decodeBudget{
		limits: limits, allocationBytesRemaining: limits.MaxAllocationBytes,
		decodedValuesRemaining: limits.MaxDecodedValues,
	}}, nil
}

func (self *decodeBudget) refuse(reason string) error {
	if self.err == nil {
		self.err = fmt.Errorf("%w: %s", ErrDecodeResourceLimit, reason)
	}
	return self.err
}

func (self *decodeBudget) enter() error {
	if self.err != nil {
		return self.err
	}
	if self.depth >= self.limits.MaxDepth {
		return self.refuse("depth")
	}
	if self.decodedValuesRemaining == 0 {
		return self.refuse("decoded values")
	}
	self.decodedValuesRemaining--
	self.depth++
	return nil
}

// ReserveAllocation admits requested backing storage before allocation. Custom
// decoders use it for storage derived from already admitted collections. It is
// a no-op for an unbounded decoder; multiplication never overflows the budget.
func (self Decoder) ReserveAllocation(count, elementBytes uint64) error {
	if self.budget == nil {
		return nil
	}
	if self.budget.err != nil {
		return self.budget.err
	}
	if elementBytes != 0 && count > self.budget.allocationBytesRemaining/elementBytes {
		return self.budget.refuse("allocation bytes")
	}
	self.budget.allocationBytesRemaining -= count * elementBytes
	return nil
}

func (self Decoder) admitCollection(count uint64) error {
	if self.budget == nil {
		return nil
	}
	if self.budget.err != nil {
		return self.budget.err
	}
	if count > self.budget.limits.MaxCollectionElements {
		return self.budget.refuse("collection elements")
	}
	return nil
}
