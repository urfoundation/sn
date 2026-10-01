// Shared recovery readers authenticate raw headers and complete block bodies.
// Callers separately establish canonical finality and original action authority.
package crv4

import (
	"context"
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Return only the header facts needed for ancestry after its complete SCALE
// hash matches the requested block, including modern runtime-update digests.
func (self *Chain) ReceiptHeaderAtContext(ctx context.Context, blockHash types.Hash) (uint64, types.Hash, error) {
	if ctx == nil || self == nil || self.API == nil || self.API.Client == nil || blockHash == (types.Hash{}) {
		return 0, types.Hash{}, errors.New("crv4: receipt header reader is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return 0, types.Hash{}, err
	}
	header, number, err := self.receiptHeaderAt(ctx, blockHash)
	if err != nil {
		return 0, types.Hash{}, err
	}
	parent, _ := receiptHash(header.ParentHash)
	if err := ctx.Err(); err != nil {
		return 0, types.Hash{}, err
	}
	return number, parent, nil
}

// Missing, truncated or substituted bodies cannot return negative membership.
// Every extrinsic is decoded and committed before an early matching entry wins.
func (self *Chain) ReceiptBlockExtrinsicContext(ctx context.Context, blockHash, extrinsicHash types.Hash) (uint64, types.Hash, bool, error) {
	if ctx == nil || self == nil || self.API == nil || self.API.Client == nil || blockHash == (types.Hash{}) || extrinsicHash == (types.Hash{}) {
		return 0, types.Hash{}, false, errors.New("crv4: receipt block reader is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return 0, types.Hash{}, false, err
	}
	body, err := self.receiptBlockAt(ctx, blockHash)
	if err != nil {
		return 0, types.Hash{}, false, err
	}
	_, found, err := extrinsicIndex(body.extrinsics, extrinsicHash)
	if err = errors.Join(err, ctx.Err()); err != nil {
		return 0, types.Hash{}, false, err
	}
	parent, _ := receiptHash(body.header.ParentHash)
	return body.number, parent, found, nil
}
