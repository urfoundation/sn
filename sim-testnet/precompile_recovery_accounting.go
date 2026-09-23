// Recovery keeps every original receipt and tracks the two probe positions
// through the separately authorized, finite sequence of value-bearing calls.
package main

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func precompileRecoveryAfterTransfer(step PrecompileRecoveryStep) bool {
	return step.Operation == "recover-sample" || step.Operation == "reseed-sample"
}

func precompileRecoveryReceipt(step PrecompileRecoveryStep) (string, uint64, string) {
	if step.Seed != nil {
		return step.Seed.TransactionHash, step.Seed.BlockNumber, step.Seed.BlockHash
	}
	return step.Move.TransactionHash, step.Move.BlockNumber, step.Move.BlockHash
}

// The seed event has no pre-state. Its delta is the observed position increase
// since the pinned quote, not an assertion about the conversion rate or yield.
func reconcilePrecompileRecoveryEvent(step PrecompileRecoveryStep, values map[string]any) (PrecompileRecoveryStep, error) {
	if step.Operation == "reseed-sample" {
		if step.Seed == nil {
			return PrecompileRecoveryStep{}, errors.New("probe reseed intent is absent")
		}
		amount, ok := conformanceEventUint64(values, "amountArg")
		value, valueOk := conformanceEventBigInt(values, "valueSent")
		after, afterOk := conformanceEventUint64(values, "stakeAfter")
		if !ok || !valueOk || !afterOk || amount != step.Seed.TAORao || value.String() != step.Seed.ValueWei || after <= step.QuoteSourceRao {
			return PrecompileRecoveryStep{}, errors.New("probe reseed event changed the fixed value or lost custody")
		}
		seed := *step.Seed
		seed.AfterRao, seed.DeltaRao = after, after-step.QuoteSourceRao
		step.Seed = &seed
		return step, nil
	}
	if step.Operation == "recover-move" || step.Operation == "recover-sample" {
		values = map[string]any{"amount": values["amount"], "fromBefore": values["sourceBefore"], "fromAfter": values["sourceAfter"], "toBefore": values["destinationBefore"], "toAfter": values["destinationAfter"]}
	}
	from, fromOk := conformanceEventUint64(values, "fromBefore")
	to, toOk := conformanceEventUint64(values, "toBefore")
	if !fromOk || !toOk || from < step.QuoteSourceRao || to < step.QuoteDestinationRao {
		return PrecompileRecoveryStep{}, errors.New("probe recovery receipt decreased its quoted custody")
	}
	intent := step.Move
	intent.FromBeforeRao, intent.ToBeforeRao = from, to
	observed, err := reconcilePrecompileMoveEvent(intent, values)
	if err != nil {
		return PrecompileRecoveryStep{}, err
	}
	step.Move = observed
	step.SourceCreditRao, step.DestinationCreditRao = from-step.QuoteSourceRao, to-step.QuoteDestinationRao
	return step, nil
}

// Checks the original sample transfer against its immutable quote and the
// preceding retained position. Its remaining balance is never rounded away.
func precompileRecoveryOriginalTransferAccounted(evidence *PrecompileConformanceEvidence, sample, move, previousBlock uint64) bool {
	r, t := evidence.Recovery, evidence.Transfer
	if r == nil || move != 0 || sample == 0 || r.SampleTransferQuoteHead.Number < previousBlock || !validCanonicalHashHex(r.SampleTransferQuoteHead.Hash) || r.SampleTransferSourceQuoteRao < sample || r.SampleTransferSourceQuoteRao <= evidence.Seed.BeforeRao || t.AmountRao != r.SampleTransferSourceQuoteRao-evidence.Seed.BeforeRao || t.ProbeBeforeRao < r.SampleTransferSourceQuoteRao || t.ProviderBeforeRao < r.SampleTransferDestinationQuoteRao || r.SampleTransferCreditRao != t.ProbeBeforeRao-sample || r.SampleTransferInclusionCreditRao != t.ProbeBeforeRao-r.SampleTransferSourceQuoteRao || r.SampleTransferDestinationCreditRao != t.ProviderBeforeRao-r.SampleTransferDestinationQuoteRao {
		return false
	}
	if !validConformanceTransaction(t.TransactionHash, t.BlockHash, t.BlockNumber) || t.BlockNumber <= r.SampleTransferQuoteHead.Number {
		return false
	}
	_, err := precompileMoveObservedAmount(precompileTransferMoveStep(t))
	return err == nil
}

// The pre-transfer mode supports the existing action's readiness/intent without
// recursively treating that action's not-yet-recorded receipt as complete.
func precompileRecoveryPositionsBeforeTransfer(evidence *PrecompileConformanceEvidence) (uint64, uint64, bool, error) {
	return precompileRecoveryPositionsThrough(evidence, false)
}
func precompileRecoveryPositions(evidence *PrecompileConformanceEvidence) (uint64, uint64, bool, error) {
	return precompileRecoveryPositionsThrough(evidence, true)
}
func precompileRecoveryPositionsThrough(evidence *PrecompileConformanceEvidence, includeTransfer bool) (uint64, uint64, bool, error) {
	if evidence == nil || evidence.Recovery == nil {
		return 0, 0, false, errors.New("probe recovery evidence is absent")
	}
	record := evidence.Recovery
	if err := validatePrecompileRecoveryAuthorization(evidence, &record.Authorization); err != nil {
		return 0, 0, false, err
	}
	if evidence.Dividend.BaselineRao != evidence.Snapshot.BaselineRao || evidence.Dividend.SinceBlock != evidence.Snapshot.SinceBlock || evidence.Dividend.DeltaRao == 0 || !exactIncrease(evidence.Dividend.BaselineRao, evidence.Dividend.CurrentRao, evidence.Dividend.DeltaRao) || evidence.Dividend.FinalizedHead.Number < evidence.Snapshot.BlockNumber || !validCanonicalHashHex(evidence.Dividend.FinalizedHead.Hash) {
		return 0, 0, false, errors.New("probe recovery cannot precede finalized dividend evidence")
	}
	if uint64(len(record.Steps)) > record.Authorization.Request.MaximumSteps {
		return 0, 0, false, errors.New("probe recovery exceeded its authorized operation count")
	}
	sample, move := evidence.Dividend.CurrentRao, evidence.Back.FromAfterRao
	previousBlock := evidence.Dividend.FinalizedHead.Number
	previousOperation := ""
	var moveProvider, sampleProvider, reseedCount uint64
	transferred := false
	applyTransfer := func() error {
		if !precompileRecoveryOriginalTransferAccounted(evidence, sample, move, previousBlock) {
			return errors.New("probe recovery lost the original sample transfer accounting")
		}
		sample, sampleProvider = evidence.Transfer.ProbeAfterRao, evidence.Transfer.ProviderAfterRao
		previousBlock = evidence.Transfer.BlockNumber
		transferred = true
		return nil
	}
	for index, step := range record.Steps {
		afterTransfer := precompileRecoveryAfterTransfer(step)
		if afterTransfer && !includeTransfer {
			return sample, move, true, nil
		}
		if afterTransfer && !transferred {
			if err := applyTransfer(); err != nil {
				return 0, 0, false, err
			}
		}
		action, _, err := precompileRecoveryAction(&record.Authorization, index, step)
		if err != nil || !precompileRecoveryActionEqual(step.Action, action) || step.QuoteHead.Number < previousBlock || !validCanonicalHashHex(step.QuoteHead.Hash) || transferred != afterTransfer || (previousOperation == "top-up" && step.Operation != "recover-move") || (previousOperation == "reseed-sample" && step.Operation != "recover-sample") {
			return 0, 0, false, fmt.Errorf("probe recovery step %d changed its authorized sequence", index+1)
		}
		switch step.Operation {
		case "top-up":
			if move == 0 || step.QuoteSourceRao < sample || step.QuoteDestinationRao < move {
				return 0, 0, false, errors.New("probe top-up lost previously observed custody")
			}
		case "recover-move":
			if move == 0 || step.QuoteSourceRao < move || step.QuoteDestinationRao < moveProvider {
				return 0, 0, false, errors.New("probe move sweep lost previously observed custody")
			}
		case "recover-sample":
			if sample == 0 || move != 0 || step.QuoteSourceRao < sample || step.QuoteDestinationRao < sampleProvider {
				return 0, 0, false, errors.New("probe sample sweep lost previously observed custody")
			}
		case "reseed-sample":
			reseedCount++
			if sample == 0 || move != 0 || step.QuoteSourceRao < sample || reseedCount > record.Authorization.Request.MaximumReseeds {
				return 0, 0, false, errors.New("probe reseed exceeded its outstanding liability or count")
			}
		}
		tx, block, hash := precompileRecoveryReceipt(step)
		if tx == "" {
			incomplete := index == len(record.Steps)-1 && block == 0 && hash == "" && step.SourceCreditRao == 0 && step.DestinationCreditRao == 0
			if step.Seed != nil {
				incomplete = incomplete && step.Seed.AfterRao == 0 && step.Seed.DeltaRao == 0
			} else {
				incomplete = incomplete && step.Move.FromAfterRao == 0 && step.Move.ToAfterRao == 0 && step.Move.FromBeforeRao == step.QuoteSourceRao && step.Move.ToBeforeRao == step.QuoteDestinationRao && step.Move.NativeShareResidueRao == 0 && step.Move.NativeShareCreditRoundingRao == 0
			}
			if !incomplete {
				return 0, 0, false, errors.New("probe recovery changed its incomplete operation")
			}
			return sample, move, false, nil
		}
		if !validConformanceTransaction(tx, hash, block) || block <= step.QuoteHead.Number {
			return 0, 0, false, errors.New("probe recovery receipt is invalid or precedes its quote")
		}
		if step.Seed != nil {
			if step.SourceCreditRao != 0 || step.DestinationCreditRao != 0 || step.Seed.DeltaRao == 0 || !exactIncrease(step.Seed.BeforeRao, step.Seed.AfterRao, step.Seed.DeltaRao) {
				return 0, 0, false, errors.New("probe reseed lost its observed balance increase")
			}
			sample = step.Seed.AfterRao
		} else {
			if step.Move.FromBeforeRao < step.QuoteSourceRao || step.Move.ToBeforeRao < step.QuoteDestinationRao || step.SourceCreditRao != step.Move.FromBeforeRao-step.QuoteSourceRao || step.DestinationCreditRao != step.Move.ToBeforeRao-step.QuoteDestinationRao {
				return 0, 0, false, errors.New("probe recovery changed its inter-block credits")
			}
			if _, err := precompileMoveObservedAmount(step.Move); err != nil {
				return 0, 0, false, err
			}
			switch step.Operation {
			case "top-up":
				sample, move = step.Move.FromAfterRao, step.Move.ToAfterRao
			case "recover-move":
				move, moveProvider = step.Move.FromAfterRao, step.Move.ToAfterRao
			case "recover-sample":
				sample, sampleProvider = step.Move.FromAfterRao, step.Move.ToAfterRao
			}
		}
		previousBlock, previousOperation = block, step.Operation
	}
	if includeTransfer && !transferred && evidence.Transfer.TransactionHash != "" {
		if err := applyTransfer(); err != nil {
			return 0, 0, false, err
		}
	}
	return sample, move, true, nil
}

func precompileRecoveryTransferAccounted(evidence *PrecompileConformanceEvidence) bool {
	sample, move, settled, err := precompileRecoveryPositions(evidence)
	return err == nil && settled && sample == 0 && move == 0 && evidence.Transfer.TransactionHash != ""
}

func precompileRecoveryEventValues(evidence *PrecompileConformanceEvidence, step PrecompileRecoveryStep, receipt *types.Receipt) (map[string]any, error) {
	if receipt == nil {
		return nil, errors.New("probe recovery receipt is absent")
	}
	parsed, err := (&Executor{}).precompileABI()
	if err != nil {
		return nil, err
	}
	sample, move, recovery := common.HexToHash(evidence.SampleHotkey), common.HexToHash(evidence.MoveHotkey), common.HexToHash(evidence.RecoveryColdkey)
	filtered := *receipt
	filtered.Logs = nil
	for _, log := range receipt.Logs {
		if log != nil && log.Address == common.HexToAddress(evidence.ProbeAddress) {
			filtered.Logs = append(filtered.Logs, log)
		}
	}
	switch step.Operation {
	case "top-up":
		return conformanceEventValues(parsed, "MoveRoundTrip", &filtered, sample, move)
	case "recover-move":
		return conformanceEventValues(parsed, "TransferredOut", &filtered, recovery, move)
	case "recover-sample":
		return conformanceEventValues(parsed, "TransferredOut", &filtered, recovery, sample)
	case "reseed-sample":
		return conformanceEventValues(parsed, "Seeded", &filtered, sample)
	}
	return nil, errors.New("probe recovery event has an unapproved operation")
}

func verifyPrecompileRecoveryEvent(evidence *PrecompileConformanceEvidence, step PrecompileRecoveryStep, receipt *types.Receipt, partial bool) error {
	values, err := precompileRecoveryEventValues(evidence, step, receipt)
	if err != nil {
		return err
	}
	observed, err := reconcilePrecompileRecoveryEvent(step, values)
	if err != nil {
		return err
	}
	if observed.Seed != nil {
		observed.Seed.TransactionHash, observed.Seed.BlockNumber, observed.Seed.BlockHash = receiptFields(receipt)
	} else {
		observed.Move.TransactionHash, observed.Move.BlockNumber, observed.Move.BlockHash = receiptFields(receipt)
	}
	if !partial && !reflect.DeepEqual(observed, step) {
		return errors.New("probe recovery receipt differs from its retained evidence")
	}
	return nil
}

// Original intent remains fixed while receipt outputs record credits accrued
// after its pinned quote. The caller persists the successful receipt even if
// another explicitly bounded cleanup is needed.
func reconcilePrecompileRecoveryOriginalTransfer(evidence *PrecompileConformanceEvidence, values map[string]any) (PrecompileTransferStep, error) {
	sample, move, settled, err := precompileRecoveryPositionsBeforeTransfer(evidence)
	if err != nil || !settled || move != 0 {
		return PrecompileTransferStep{}, errors.Join(errors.New("original transfer lost completed move recovery"), err)
	}
	record := evidence.Recovery
	step := PrecompileRecoveryStep{Operation: "recover-sample", QuoteSourceRao: record.SampleTransferSourceQuoteRao, QuoteDestinationRao: record.SampleTransferDestinationQuoteRao, Move: precompileTransferMoveStep(evidence.Transfer)}
	observed, err := reconcilePrecompileRecoveryEvent(step, values)
	if err != nil || observed.Move.FromBeforeRao < sample {
		return PrecompileTransferStep{}, errors.Join(errors.New("original transfer lost retained sample custody"), err)
	}
	transfer := evidence.Transfer
	transfer.ProbeBeforeRao, transfer.ProbeAfterRao = observed.Move.FromBeforeRao, observed.Move.FromAfterRao
	transfer.ProviderBeforeRao, transfer.ProviderAfterRao = observed.Move.ToBeforeRao, observed.Move.ToAfterRao
	transfer.NativeShareResidueRao, transfer.NativeShareCreditRoundingRao = observed.Move.NativeShareResidueRao, observed.Move.NativeShareCreditRoundingRao
	record.SampleTransferCreditRao = transfer.ProbeBeforeRao - sample
	record.SampleTransferInclusionCreditRao, record.SampleTransferDestinationCreditRao = observed.SourceCreditRao, observed.DestinationCreditRao
	return transfer, nil
}

// A completion may append missing phases, never rewrite an already retained
// financial receipt, dividend observation or native/probe identity.
func verifyPrecompileRecoveryOriginalEvidence(original, recovered *PrecompileConformanceEvidence) error {
	if original == nil || recovered == nil || recovered.Recovery == nil {
		return errors.New("probe recovery original or completed evidence is absent")
	}
	originalCopy := *original
	originalCopy.EvidenceHash = ""
	originalHash, err := canonicalHashHex(originalCopy)
	if err != nil || originalHash != original.EvidenceHash || recovered.Recovery.Authorization.Request.OriginalEvidenceHash != originalHash {
		return errors.New("probe recovery changed its original evidence hash")
	}
	before, after := *original, *recovered
	before.Recovery, after.Recovery = nil, nil
	before.Complete, after.Complete = false, false
	before.EvidenceHash, after.EvidenceHash = "", ""
	if before.Dividend == (PrecompileDividendStep{}) {
		after.Dividend = PrecompileDividendStep{}
	}
	if before.Transfer == (PrecompileTransferStep{}) {
		after.Transfer = PrecompileTransferStep{}
	}
	if !reflect.DeepEqual(before, after) {
		return errors.New("probe recovery rewrote retained conformance evidence")
	}
	return nil
}
