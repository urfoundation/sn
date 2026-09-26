package chain

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"

	"github.com/urfoundation/sn/crv4"
)

// SubmitRequest is one signed native write. Apply=false quotes and reports the
// exact signed bytes and never broadcasts.
type SubmitRequest struct {
	Command     string
	Netuid      uint16
	Hotkey      [32]byte
	Signer      *crv4.Keypair
	Call        types.Call
	FeeLimitRao uint64
	Journal     *Journal
	Apply       bool
	Output      io.Writer
}

// SubmitResult reports the signed bytes and, when applied, the canonical
// finalized receipt.
type SubmitResult struct {
	ExtrinsicHash  types.Hash
	Raw            []byte
	Nonce          uint32
	FeeEstimateRao uint64
	Receipt        *crv4.FinalizedExtrinsic
}

// SubmitCall signs the call under the bound (authenticated) chain view, quotes
// and approves its fee, journals the broadcast, waits for canonical finality
// with dispatch success, and journals the receipt. Every step prints to
// req.Output so an operator sees the extrinsic hash before the wait.
func SubmitCall(ctx context.Context, bound *crv4.Chain, req SubmitRequest) (SubmitResult, error) {
	var result SubmitResult
	if ctx == nil || bound == nil || bound.API == nil || bound.API.Client == nil || bound.Meta == nil || bound.Runtime == nil || req.Signer == nil || req.Output == nil {
		return result, errors.New("submit dependencies are unavailable")
	}
	if req.Apply && req.Journal == nil {
		return result, errors.New("an applied submit requires a journal")
	}
	nonce, err := bound.AccountNonceContext(ctx, req.Signer.Address())
	if err != nil {
		return result, err
	}
	raw, err := EncodeSignedCall(bound, req.Signer.Ring, req.Call, nonce)
	if err != nil {
		return result, err
	}
	result.Raw, result.Nonce, result.ExtrinsicHash = raw, nonce, ExtrinsicHash(raw)
	estimated, err := ApproveNativeTransactionFee(ctx, bound, raw, req.FeeLimitRao)
	result.FeeEstimateRao = estimated
	if err != nil {
		return result, err
	}
	fmt.Fprintf(req.Output, "extrinsic: %s (signer %s, nonce %d, %d bytes)\n", result.ExtrinsicHash.Hex(), req.Signer.Address(), nonce, len(raw))
	fmt.Fprintf(req.Output, "fee: estimated %s TAO (%d rao), limit %d rao\n", FormatRao(estimated), estimated, req.FeeLimitRao)
	if !req.Apply {
		fmt.Fprintf(req.Output, "dry run: nothing was broadcast; re-run with --apply to submit\n")
		return result, nil
	}
	if err := req.Journal.SaveRaw(result.ExtrinsicHash, raw); err != nil {
		return result, err
	}
	base := JournalEntry{Command: req.Command, Netuid: req.Netuid, Signer: req.Signer.Address(), Hotkey: Hex32(req.Hotkey), Nonce: nonce, ExtrinsicHash: result.ExtrinsicHash.Hex(), FeeEstimateRao: estimated, FeeLimitRao: req.FeeLimitRao}
	broadcast := base
	broadcast.Stage = JournalStageBroadcast
	if err := req.Journal.Append(broadcast); err != nil {
		return result, err
	}
	fmt.Fprintf(req.Output, "broadcast: journaled at %s; waiting for finality...\n", req.Journal.Path())
	receipt, err := bound.SubmitRawAndWatchFinalized(ctx, codec.HexEncodeToString(raw))
	if err != nil {
		failed := base
		failed.Stage, failed.Detail = JournalStageFailed, err.Error()
		return result, errors.Join(err, req.Journal.Append(failed))
	}
	finalized := base
	finalized.Stage, finalized.BlockNumber, finalized.BlockHash = JournalStageFinalized, receipt.BlockNumber, receipt.BlockHash.Hex()
	if err := req.Journal.Append(finalized); err != nil {
		return result, err
	}
	result.Receipt = receipt
	fmt.Fprintf(req.Output, "finalized: extrinsic %s in block %d (%s)\n", receipt.ExtrinsicHash.Hex(), receipt.BlockNumber, receipt.BlockHash.Hex())
	return result, nil
}
