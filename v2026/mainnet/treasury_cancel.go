// Cancellation spends no treasury principal and never dispatches the inner call.
// Its original depositor may recover the reserved deposit after recipient drift.
package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// The exact original native timepoint, call hash and depositor are mandatory;
// current earning policy, registration generations and stake are irrelevant.
func treasuryCancellationFacts(ctx context.Context, a treasuryAction, metadata *types.Metadata, number uint64, read func(context.Context, string, []byte) ([]byte, bool, error)) (treasuryFacts, error) {
	var f treasuryFacts
	if err := rootAccountProfile(metadata); err != nil {
		return f, err
	}
	account, _ := hex.DecodeString(a.Owner[2:])
	key, err := types.CreateStorageKey(metadata, "System", "Account", account)
	if err != nil {
		return f, err
	}
	data, exists, err := read(ctx, key.Hex(), make([]byte, 56))
	if err != nil {
		return f, err
	}
	if !exists || len(data) != 56 {
		return f, errors.New("treasury cancellation signatory account unavailable")
	}
	f.Nonce = binary.LittleEndian.Uint32(data[:4])
	free, frozen := binary.LittleEndian.Uint64(data[16:24]), binary.LittleEndian.Uint64(data[32:40])
	if free > frozen {
		f.SignatorySpendable = free - frozen
	}
	inner, err := a.innerCall()
	if err != nil {
		return f, err
	}
	multisig, _ := hex.DecodeString(a.Descriptor.Multisig.AccountId[2:])
	hash, _ := hex.DecodeString(rootExtrinsicHash(inner)[2:])
	key, err = types.CreateStorageKey(metadata, "Multisig", "Multisigs", multisig, hash)
	if err != nil {
		return f, err
	}
	raw, present, err := read(ctx, key.Hex(), nil)
	if err != nil {
		return f, err
	}
	if present {
		f.Pending, err = decodeTreasuryPending(raw, a)
		if err != nil {
			return f, err
		}
		if uint64(f.Pending.Timepoint.Height) > number {
			return f, errors.New("treasury cancellation pending operation is in the future")
		}
	}
	return f, nil
}
