// This narrow wire command is consumed only through nativefee.Invoke. It runs
// the original verifier and emits one exact transaction's complete native fee;
// request-supplied policy is never its own independent approval authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/urfoundation/sn/v2026/nativefee"
	"github.com/urnetwork/server/v2026/strecovery"
)

func runEconomicNativeFeeOutcomeCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("verify-native-fee-outcome", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("request", "", "absolute private original request")
	digest := flags.String("request-sha256", "", "exact original request SHA256")
	transaction := flags.String("transaction", "", "exact retained signed EVM transaction hash")
	policyJSON := flags.String("policy-json", "", "independently selected native policy")
	budget := flags.Duration("budget", 300*time.Second, "complete verifier budget")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !rootCanonicalHash(*transaction) || len(*policyJSON) == 0 || len(*policyJSON) > 16*1024 || *budget < time.Minute || *budget > 15*time.Minute {
		fmt.Fprintln(stderr, "native fee outcome requires exact request, transaction and independent policy with a 60s–15m budget")
		return 2
	}
	reference := nativefee.Reference{Path: *path, Sha256: *digest}
	var policy nativefee.NativePolicy
	if err := errors.Join(reference.Validate(), decodePlanJson([]byte(*policyJSON), &policy)); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := policy.Validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	owner, cancel := context.WithTimeout(ctx, *budget)
	defer cancel()
	statement, err := produceEconomicNativeFeeOutcome(owner, reference, policy, *transaction, *budget)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := owner.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(statement); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func produceEconomicNativeFeeOutcome(ctx context.Context, reference nativefee.Reference, policy nativefee.NativePolicy, transactionHash string, budget time.Duration) (*nativefee.Statement, error) {
	raw, digest, err := readBootstrapRootFile(ctx, reference.Path, 64*1024)
	if err != nil {
		return nil, err
	}
	if digest != reference.Sha256 {
		return nil, errors.New("native fee outcome request differs from its original pin")
	}
	var request economicNativeFeeRequest
	if err := decodePlanJson(raw, &request); err != nil {
		return nil, err
	}
	if rootObjectHash(request.Policy) != policy.Hash() {
		return nil, errors.New("native fee request cannot select its own approval authority")
	}
	// This call verifies approval, signed receipts, GRANDPA ancestry and the
	// actual owned native runtime replay. It has no imported-report shortcut.
	evidence, err := runEconomicNativeFeeEvidence(ctx, request, budget, historicalReplayHooks{})
	if err != nil {
		return nil, err
	}
	if evidence == nil || evidence.Context == nil || evidence.Context.Replay == nil || evidence.Context.ContextProof == nil || evidence.Context.ContextProof.Finality == nil {
		return nil, errors.New("original native fee verifier returned no completed context")
	}
	joined := evidence.Context
	var selected *historicalFeeContextTransaction
	for index := range joined.Transactions {
		item := &joined.Transactions[index]
		if item.TransactionHash == transactionHash {
			if selected != nil {
				return nil, errors.New("original native fee context repeats target transaction")
			}
			selected = item
		}
	}
	if selected == nil || !selected.FeeAuthenticated || selected.Receipt == nil || selected.NativeBlock == nil || selected.Candidate == nil || selected.Candidate.ExtrinsicIndex == nil || selected.ActualWithdrawalRao == nil || selected.ActualRefundRao == nil || selected.ActualDebitRao == nil || joined.Native.NativeParent == nil || joined.Native.NativeParentStateRoot == nil {
		return nil, errors.New("original native fee withdrawal, refund or finalized transaction remains unknown")
	}
	// The exact hash-pinned archive was verified above. Rereading it only
	// retrieves the original signature; changed bytes cannot join this result.
	archiveRaw, archiveDigest, err := readBootstrapRootFile(ctx, request.Context.Archive.Path, strecovery.MaximumArchiveBytes)
	if err != nil {
		return nil, err
	}
	if archiveDigest != request.Context.Archive.Sha256 {
		return nil, errors.New("original signature archive changed after verification")
	}
	var archive strecovery.Archive
	if err := decodePlanJson(archiveRaw, &archive); err != nil {
		return nil, err
	}
	var signature []byte
	for _, transaction := range archive.Transactions {
		if transaction.Hash == transactionHash {
			if signature != nil {
				return nil, errors.New("original signature archive repeats transaction")
			}
			signature = slices.Clone(transaction.Raw)
		}
	}
	finality := joined.ContextProof.Finality
	statement := &nativefee.Statement{
		Schema: nativefee.StatementSchema, RequestSha256: reference.Sha256, RequestHash: evidence.RequestHash, PolicyHash: policy.Hash(), ApprovalHash: evidence.ApprovalHash, ProofHash: evidence.ContentHash,
		Genesis: joined.Genesis, EvmChainId: joined.EvmChainId, RuntimeCodeSha256: economicNativeFeeSha(joined.Replay.RuntimeCodeSha256), EngineSha256: request.Context.Engine.Sha256, ProfileSha256: policy.ProfileSha256,
		PayerProfile: joined.ContextProof.Profile, PayerRuntimeSource: joined.ContextProof.ProfileRuntimeSource,
		NativeBlockNumber: selected.NativeBlock.Number, NativeBlockHash: selected.NativeBlock.Hash, NativeParentNumber: joined.Native.NativeParent.Number, NativeParentHash: joined.Native.NativeParent.Hash,
		NativeStateRoot: joined.Native.NativeStateRoot, NativeParentStateRoot: *joined.Native.NativeParentStateRoot, NativeFinalizedNumber: finality.NativeFinalized.Number, NativeFinalizedHash: finality.NativeFinalized.Hash,
		TransactionHash: selected.TransactionHash, Sender: selected.Sender, Nonce: selected.Nonce, RawTransaction: signature, ReceiptStatus: selected.Receipt.Status, ReceiptBytesHash: selected.Receipt.ReceiptBytesHash,
		EvmBlockNumber: selected.Receipt.BlockNumber, EvmBlockHash: selected.Receipt.BlockHash, TransactionIndex: selected.Receipt.TransactionIndex, ExtrinsicIndex: *selected.Candidate.ExtrinsicIndex,
		Payer: economicNativeFeeHash(selected.Candidate.Payer), WithdrawalRao: *selected.ActualWithdrawalRao, RefundRao: *selected.ActualRefundRao, DebitRao: *selected.ActualDebitRao,
		Originals: []nativefee.Original{
			{Kind: "request", Reference: reference},
			{Kind: "approval", Reference: nativefee.Reference(request.Approval)},
			{Kind: "archive", Reference: nativefee.Reference(request.Context.Archive)},
			{Kind: "receipt_collection", Reference: nativefee.Reference(request.Context.Collection)},
			{Kind: "checkpoint", Reference: nativefee.Reference(request.Context.Checkpoint)},
			{Kind: "finality_proof", Reference: nativefee.Reference(request.Context.FinalityProof)},
			{Kind: "replay_job", Reference: nativefee.Reference(request.Context.Job)},
		},
	}
	if err := statement.Validate(policy, reference, transactionHash); err != nil {
		return nil, err
	}
	return statement, ctx.Err()
}
