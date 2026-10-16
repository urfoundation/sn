package ledger

import (
	"context"
	"slices"
)

// PoolEpoch keys a pool's epoch.
type PoolEpoch struct {
	NoID  uint64
	Epoch uint64
}

// MemoryCustody is a Custody held in memory, for tests and one-shot tools
// such as snclaim. A validator's custody must be durable.
type MemoryCustody struct {
	Objects Objects
	// Rows holds the chunks of each epoch's valid rows.
	Rows map[PoolEpoch][]Ref
	// Nulls holds, per epoch, the seq of every null declaration recorded.
	Nulls map[PoolEpoch][]uint64
}

// NewMemoryCustody returns an empty MemoryCustody.
func NewMemoryCustody() *MemoryCustody {
	return &MemoryCustody{Objects: Objects{}, Rows: map[PoolEpoch][]Ref{}, Nulls: map[PoolEpoch][]uint64{}}
}

func (c *MemoryCustody) Object(ctx context.Context, ref Ref) ([]byte, bool, error) {
	data, ok := c.Objects[ref]
	return data, ok, nil
}

func (c *MemoryCustody) Keep(ctx context.Context, ref Ref, data []byte) error {
	c.Objects[ref] = data
	return nil
}

func (c *MemoryCustody) ValidRows(ctx context.Context, noID, epoch uint64) ([]Ref, bool, error) {
	chunks, ok := c.Rows[PoolEpoch{noID, epoch}]
	return chunks, ok, nil
}

func (c *MemoryCustody) KeepValidRows(ctx context.Context, noID, epoch uint64, chunks []Ref) error {
	c.Rows[PoolEpoch{noID, epoch}] = slices.Clone(chunks)
	return nil
}

func (c *MemoryCustody) KeepNull(ctx context.Context, noID, epoch, seq uint64) error {
	key := PoolEpoch{noID, epoch}
	c.Nulls[key] = append(c.Nulls[key], seq)
	return nil
}
