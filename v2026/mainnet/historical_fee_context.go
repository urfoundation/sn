// One offline owner joins actual receipt/native proofs to a complete replay.
// The join preserves candidate amounts and missing observations; independent
// checkpoint/runtime/source admission remains a separate authority boundary.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/urnetwork/server/v2026/strecovery"
)

const historicalFeeContextSchema = "urnetwork-historical-fee-context-request-v1"
const historicalFeeContextReportSchema = "urnetwork-historical-fee-context-report-v1"

// The independently supplied network and exact input pins cannot be selected
// by a replay subprocess or replaced by booleans in an imported report.
type historicalFeeContextRequest struct {
	Schema        string            `json:"schema"`
	Genesis       string            `json:"genesis_hash"`
	EvmChainId    uint64            `json:"evm_chain_id"`
	Archive       planFileReference `json:"archive"`
	Collection    planFileReference `json:"receipt_collection"`
	Checkpoint    planFileReference `json:"native_checkpoint"`
	FinalityProof planFileReference `json:"native_finality_proof"`
	Engine        planFileReference `json:"replay_engine"`
	Job           planFileReference `json:"replay_job"`
}

func (self historicalFeeContextRequest) validate() error {
	if self.Schema != historicalFeeContextSchema || !rootCanonicalHash(self.Genesis) || self.EvmChainId == 0 {
		return errors.New("historical fee context requires its exact schema and independent network")
	}
	paths := map[string]bool{}
	for _, reference := range []planFileReference{self.Archive, self.Collection, self.Checkpoint, self.FinalityProof, self.Engine, self.Job} {
		if !bootstrapRootAbsolutePath(reference.Path) || !planSha256(reference.Sha256) || paths[reference.Path] {
			return errors.New("historical fee context requires distinct bounded private input pins")
		}
		paths[reference.Path] = true
	}
	return nil
}

// Every original archived signature keeps its receipt and origins, including
// unavailable alternatives. Candidate amounts are not authenticated debits.
type historicalFeeContextTransaction struct {
	TransactionHash     string                            `json:"transaction_hash"`
	Role                string                            `json:"role"`
	Sender              string                            `json:"sender"`
	Nonce               uint64                            `json:"nonce"`
	Origins             []strecovery.Origin               `json:"origins"`
	Receipt             *strecovery.CommittedReceipt      `json:"receipt"`
	State               string                            `json:"state"`
	NativeBlock         *strecovery.ObservedBlockIdentity `json:"native_block"`
	Candidate           *historicalReplayFeeCandidate     `json:"candidate"`
	ActualWithdrawalRao *string                           `json:"actual_withdrawal_rao"`
	ActualRefundRao     *string                           `json:"actual_refund_rao"`
	ActualDebitRao      *string                           `json:"actual_debit_rao"`
	FeeAuthenticated    bool                              `json:"fee_authenticated"`
}

type historicalFeeContextReport struct {
	Schema                           string                             `json:"schema"`
	RequestHash                      string                             `json:"request_hash"`
	Admission                        string                             `json:"admission"`
	Genesis                          string                             `json:"genesis_hash"`
	EvmChainId                       uint64                             `json:"evm_chain_id"`
	Native                           strecovery.ReceiptFeeNativeContext `json:"native_context"`
	Transactions                     []historicalFeeContextTransaction  `json:"transactions"`
	JoinedCandidates                 uint64                             `json:"joined_candidates"`
	UnselectedCandidates             uint64                             `json:"unselected_candidates"`
	ContextProof                     *strecovery.ReceiptFeeContexts     `json:"context_proof"`
	Replay                           *historicalReplayReport            `json:"replay"`
	AuthorityCheckpointAuthenticated bool                               `json:"authority_checkpoint_authenticated"`
	RuntimeSourceAuthenticated       bool                               `json:"runtime_source_authenticated"`
	FinalityAuthenticated            bool                               `json:"finality_authenticated"`
	NativeFeesAuthenticated          bool                               `json:"native_fees_authenticated"`
	SpendingAuthorized               bool                               `json:"spending_authorized"`
	MissingAuthorities               []string                           `json:"missing_authorities"`
}

// These reads share one deadline and have individual protocol byte bounds.
// No input is reread by filename after decoding, and no caller-written derived
// fee-context report can replace the actual Server cryptographic verifier.
func runHistoricalFeeContext(ctx context.Context, request historicalFeeContextRequest, budget time.Duration, hooks historicalReplayHooks) (result *historicalFeeContextReport, resultErr error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	if budget < time.Minute || budget > 15*time.Minute {
		return nil, errors.New("historical fee context requires one 60–900 second owner budget")
	}
	owner, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, owner.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	read := func(reference planFileReference, limit int, value any) error {
		raw, digest, err := readBootstrapRootFile(owner, reference.Path, limit)
		if err != nil {
			return fmt.Errorf("read historical fee input: %w", err)
		}
		if digest != reference.Sha256 {
			return errors.Join(errEconomicNativeFeeIntegrity, errors.New("historical fee input differs from its exact pin"))
		}
		if err := decodePlanJson(raw, value); err != nil {
			return errors.Join(errEconomicNativeFeeIntegrity, err)
		}
		return nil
	}
	var archive strecovery.Archive
	var collection strecovery.ReceiptCollection
	var checkpoint strecovery.NativeFinalityCheckpoint
	var proof strecovery.ReceiptFinalityProof
	for _, input := range []struct {
		reference planFileReference
		limit     int
		value     any
	}{
		{reference: request.Archive, limit: strecovery.MaximumArchiveBytes, value: &archive},
		{reference: request.Collection, limit: strecovery.MaximumReceiptCollectionBytes, value: &collection},
		{reference: request.Checkpoint, limit: strecovery.MaximumNativeCheckpointBytes, value: &checkpoint},
		{reference: request.FinalityProof, limit: strecovery.MaximumReceiptFinalityBytes, value: &proof},
	} {
		if err := read(input.reference, input.limit, input.value); err != nil {
			return nil, err
		}
	}
	if archive.Selection.Genesis != request.Genesis || archive.Selection.ChainId != request.EvmChainId || checkpoint.Genesis != request.Genesis {
		return nil, errors.Join(errEconomicNativeFeeIntegrity, errors.New("historical fee evidence differs from the independent network"))
	}
	contexts, err := strecovery.VerifyReceiptFeeContexts(owner, &archive, &collection, &checkpoint, &proof)
	if err != nil {
		if owner.Err() != nil && monitorOnlyCancellationCauses(err, 0) {
			return nil, err
		}
		return nil, errors.Join(errEconomicNativeFeeIntegrity, fmt.Errorf("verify historical receipt and native contexts: %w", err))
	}
	replay, err := runHistoricalReplay(owner, historicalReplayRequest{Engine: request.Engine, Job: request.Job, Budget: budget}, hooks)
	if err != nil {
		return nil, err
	}
	result, err = joinHistoricalFeeContext(owner, request, contexts, replay)
	if err != nil && (owner.Err() == nil || !monitorOnlyCancellationCauses(err, 0)) {
		err = errors.Join(errEconomicNativeFeeIntegrity, err)
	}
	return result, err
}

// Only the immediately returned verifier/replay objects enter this join. It
// has no decoder accepting an imported report's claimed verification flags.
func joinHistoricalFeeContext(ctx context.Context, request historicalFeeContextRequest, contexts *strecovery.ReceiptFeeContexts, replay *historicalReplayReport) (*historicalFeeContextReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if contexts == nil || replay == nil || contexts.Finality == nil || !contexts.Finality.GrandpaCertificatesVerified || !contexts.Finality.NativeHeaderAncestryVerified || !contexts.Finality.NativeEvmCommitmentVerified || !replay.PostStateReproduced {
		return nil, errors.New("historical fee join lacks completed relative proof verifiers")
	}
	hexDigest := func(value historicalReplayDigest) string { return "0x" + hex.EncodeToString(value[:]) }
	var selected *strecovery.ReceiptFeeBlockContext
	for index := range contexts.Blocks {
		block := &contexts.Blocks[index]
		for _, native := range block.NativeContexts {
			if native.NativeBlock.Hash != hexDigest(replay.ChildHash) {
				continue
			}
			if selected != nil || block.State != "native_context_complete" || len(block.NativeContexts) != 1 || native.NativeParent == nil || native.NativeParentStateRoot == nil ||
				native.NativeParentHash != hexDigest(replay.ParentHash) || native.NativeParent.Hash != hexDigest(replay.ParentHash) ||
				native.NativeStateRoot != hexDigest(replay.ChildStateRoot) || *native.NativeParentStateRoot != hexDigest(replay.ParentStateRoot) || native.NativeParent.Number+1 != native.NativeBlock.Number {
				return nil, errors.New("historical fee replay contradicts its unique native parent and child proof")
			}
			selected = block
		}
	}
	if selected == nil {
		return nil, errors.New("historical fee replay has no complete unique receipt-native context")
	}
	result := &historicalFeeContextReport{Schema: historicalFeeContextReportSchema, RequestHash: rootObjectHash(request), Admission: "unapproved-replay-receipt-context-join", Genesis: request.Genesis, EvmChainId: request.EvmChainId,
		Native: selected.NativeContexts[0], Transactions: []historicalFeeContextTransaction{}, ContextProof: contexts, Replay: replay,
		MissingAuthorities: []string{
			"independently admitted genesis and GRANDPA authority checkpoint; certificates here are relative to the supplied checkpoint",
			"source-to-deployed parent runtime correspondence and independently admitted original callsite, metadata, payer and refund semantics",
			"explicit native withdrawal/refund evidence for each transaction; an absent event never proves zero",
			"operational selection, durable monitor admission and any spending authority are separate",
		}}
	candidates := map[string]historicalReplayFeeCandidate{}
	if replay.HookObservations != nil && replay.HookObservations.FeeEvents != nil {
		for _, candidate := range replay.HookObservations.FeeEvents.Candidates {
			hash := hexDigest(candidate.TransactionHash)
			if _, present := candidates[hash]; present {
				return nil, errors.New("historical fee replay repeats a transaction candidate")
			}
			candidates[hash] = candidate
		}
	}
	for _, transaction := range contexts.Transactions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := historicalFeeContextTransaction{TransactionHash: transaction.Hash, Role: transaction.Role, Sender: transaction.Sender, Nonce: transaction.Nonce, Origins: slices.Clone(transaction.Origins), Receipt: transaction.Receipt, State: transaction.NativeContextState}
		if transaction.Receipt != nil && transaction.Receipt.BlockHash == selected.EvmBlock.Hash {
			entry.NativeBlock = &result.Native.NativeBlock
			entry.State = "fee-observation-unavailable"
			if candidate, present := candidates[transaction.Hash]; present {
				if hexDigest(candidate.Payer) != transaction.ProfileMappedAccount {
					return nil, errors.New("historical fee candidate payer contradicts the signed receipt source profile")
				}
				candidate.EventOrdinals = slices.Clone(candidate.EventOrdinals)
				entry.Candidate = &candidate
				entry.State = candidate.Status
				result.JoinedCandidates++
				delete(candidates, transaction.Hash)
			}
		} else if transaction.Receipt != nil {
			entry.State = "historical-execution-not-supplied"
		}
		result.Transactions = append(result.Transactions, entry)
	}
	// The complete block may contain transactions outside the independently
	// selected archive. Keep their count and original replay evidence intact.
	result.UnselectedCandidates = uint64(len(candidates))
	return result, ctx.Err()
}
