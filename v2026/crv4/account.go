package crv4

// Shares the reviewed Subtensor account layout with native balance readers.
// Replay reads require a complete row at an independently authenticated block.

import (
	"context"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
)

// Uses u64 rao balances; the generic Substrate account assumes u128 balances
// and cannot decode the reviewed Subtensor System.Account value.
type SubtensorAccountInfo struct {
	Nonce       types.U32
	Consumers   types.U32
	Providers   types.U32
	Sufficients types.U32
	Data        struct {
		Free     types.U64
		Reserved types.U64
		Frozen   types.U64
		Flags    types.U128
	}
}

// Reads the canonical nonce at the caller's authenticated block. JSON null
// means absence; present rows must have the entire exact reviewed layout.
func (self *Chain) AccountNonceAtContext(ctx context.Context, publicKey [32]byte, blockHash types.Hash) (uint32, error) {
	if blockHash == (types.Hash{}) {
		return 0, errors.New("crv4: account nonce block hash is zero")
	}
	if self == nil || self.Meta == nil {
		return 0, errors.New("crv4: storage metadata is unavailable")
	}
	key, err := types.CreateStorageKey(self.Meta, "System", "Account", publicKey[:])
	if err != nil {
		return 0, fmt.Errorf("crv4: storage key System.Account: %w", err)
	}
	raw, err := self.storageRawAtContext(ctx, key, blockHash)
	if err != nil {
		return 0, fmt.Errorf("crv4: read System.Account: %w", err)
	}
	if raw == nil {
		return 0, nil
	}
	const accountBytes = 4*4 + 3*8 + 16
	if len(*raw) != accountBytes {
		return 0, fmt.Errorf("crv4: System.Account has %d bytes, want reviewed Subtensor layout of %d", len(*raw), accountBytes)
	}
	var account SubtensorAccountInfo
	if err := codec.Decode([]byte(*raw), &account); err != nil {
		return 0, fmt.Errorf("crv4: decode System.Account: %w", err)
	}
	return uint32(account.Nonce), nil
}
