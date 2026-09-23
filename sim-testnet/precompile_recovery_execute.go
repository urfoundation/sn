// The release's existing writer advances one recovery transaction per turn.
// Every signed operation uses the ordinary durable sender and receipt journal.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// A missing approval is a normal pending liability. A malformed or foreign
// approval is an integrity error and cannot gain permission by waiting.
func (self *Executor) loadPrecompileRecoveryAuthorization(evidence *PrecompileConformanceEvidence) error {
	if evidence.Recovery != nil {
		if err := validatePrecompileRecoveryPlan(self.plan, evidence, &evidence.Recovery.Authorization); err != nil {
			return err
		}
		return self.adoptPrecompileRecoveryGasRevision(evidence)
	}
	var authorization PrecompileRecoveryAuthorization
	if err := readJSONFile(filepath.Join(self.stateDir, precompileRecoveryAuthorizationFilename), &authorization); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errPrecompileRecoveryPending
		}
		return err
	}
	if err := validatePrecompileRecoveryPlan(self.plan, evidence, &authorization); err != nil {
		return err
	}
	found := false
	for _, entry := range self.journal.Entries() {
		if entry.EntryHash == authorization.Request.Budget.JournalHash && entry.PlanHash == authorization.Request.PlanHash {
			found = true
			break
		}
	}
	if !found {
		return errors.New("probe recovery budget has no authenticated journal anchor")
	}
	evidence.Recovery = &PrecompileRecoveryEvidence{Authorization: authorization}
	return writePrecompileEvidence(self.stateDir, evidence)
}

// Source quotes and preflight share one finalized head. Only an actual revert
// selects the preauthorized top-up/reseed; other failures retain their cause.
func (self *Executor) preparePrecompileRecoveryStep(ctx context.Context, evidence *PrecompileConformanceEvidence) (PrecompileRecoveryStep, error) {
	sampleMinimum, moveMinimum, settled, err := precompileRecoveryPositions(evidence)
	if err != nil || !settled {
		return PrecompileRecoveryStep{}, errors.Join(errors.New("probe recovery cannot quote an unsettled sequence"), err)
	}
	afterTransfer := evidence.Transfer.TransactionHash != ""
	if (!afterTransfer && moveMinimum == 0) || (afterTransfer && sampleMinimum == 0) {
		return PrecompileRecoveryStep{}, errors.New("probe recovery has no outstanding position")
	}
	if len(evidence.Recovery.Steps) >= precompileRecoveryMaximumSteps {
		return PrecompileRecoveryStep{}, fmt.Errorf("%w: approved operation count exhausted", errPrecompileRecoveryPending)
	}
	head, err := finalizedEVMHead(ctx, self.deployer.client)
	if err != nil {
		return PrecompileRecoveryStep{}, err
	}
	probe := common.HexToAddress(evidence.ProbeAddress)
	code, err := self.deployer.client.CodeAt(ctx, probe, new(big.Int).SetUint64(head.Number))
	if err != nil || crypto.Keccak256Hash(code).Hex() != evidence.Recovery.Authorization.Request.ProbeRuntimeHash {
		return PrecompileRecoveryStep{}, stateMismatchError(err, "probe recovery runtime differs from approval")
	}
	sample, err := decodeHex32("probe sample", evidence.SampleHotkey)
	if err != nil {
		return PrecompileRecoveryStep{}, err
	}
	move, err := decodeHex32("probe move", evidence.MoveHotkey)
	if err != nil {
		return PrecompileRecoveryStep{}, err
	}
	recovery, err := decodeHex32("probe recipient", evidence.RecoveryColdkey)
	if err != nil {
		return PrecompileRecoveryStep{}, err
	}
	coldkey := ss58Mirror(probe)
	hotkey, minimum, operation := move, moveMinimum, "recover-move"
	if afterTransfer {
		hotkey, minimum, operation = sample, sampleMinimum, "recover-sample"
	}
	balance, err := self.readStakeAt(ctx, head.Number, hotkey, coldkey)
	if err != nil || balance < minimum {
		return PrecompileRecoveryStep{}, stateMismatchError(err, "probe recovery custody decreased from %d to %d", minimum, balance)
	}
	provider, err := self.readStakeAt(ctx, head.Number, hotkey, recovery)
	if err != nil {
		return PrecompileRecoveryStep{}, err
	}
	step := PrecompileRecoveryStep{Operation: operation, QuoteHead: head, QuoteSourceRao: balance, QuoteDestinationRao: provider, Move: PrecompileMoveStep{AmountRao: balance, FromBeforeRao: balance, ToBeforeRao: provider}}
	preflight := func(candidate PrecompileRecoveryStep) (Action, error) {
		action, data, err := precompileRecoveryAction(&evidence.Recovery.Authorization, len(evidence.Recovery.Steps), candidate)
		if err != nil {
			return Action{}, err
		}
		value, ok := new(big.Int).SetString(action.Parameters["recovery_value_wei"], 10)
		if !ok {
			return Action{}, errors.New("probe recovery value is invalid")
		}
		_, err = self.deployer.client.CallContract(ctx, ethereum.CallMsg{From: evidence.Recovery.Authorization.Request.Deployer, To: &probe, Value: value, Data: data}, new(big.Int).SetUint64(head.Number))
		return action, errors.Join(err, ctx.Err())
	}
	action, err := preflight(step)
	if err != nil {
		if !precompileRecoveryCallReverted(err) {
			return PrecompileRecoveryStep{}, err
		}
		count := len(evidence.Recovery.Steps)
		if count > 0 && (evidence.Recovery.Steps[count-1].Operation == "top-up" || evidence.Recovery.Steps[count-1].Operation == "reseed-sample") {
			return PrecompileRecoveryStep{}, fmt.Errorf("%w: funded sweep preflight: %v", errPrecompileRecoveryPending, err)
		}
		if afterTransfer {
			var reseeds uint64
			for _, prior := range evidence.Recovery.Steps {
				if prior.Operation == "reseed-sample" {
					reseeds++
				}
			}
			if reseeds >= evidence.Recovery.Authorization.Request.MaximumReseeds {
				return PrecompileRecoveryStep{}, fmt.Errorf("%w: reseed bound exhausted", errPrecompileRecoveryPending)
			}
			amount := evidence.Recovery.Authorization.Request.ReseedTaoRao
			value := new(big.Int).Mul(new(big.Int).SetUint64(amount), big.NewInt(1_000_000_000))
			step = PrecompileRecoveryStep{Operation: "reseed-sample", QuoteHead: head, QuoteSourceRao: balance, Seed: &PrecompileValueStep{TAORao: amount, ValueWei: value.String(), BeforeRao: balance}}
		} else {
			sampleBalance, readErr := self.readStakeAt(ctx, head.Number, sample, coldkey)
			if readErr != nil || sampleBalance < sampleMinimum {
				return PrecompileRecoveryStep{}, stateMismatchError(readErr, "probe recovery sample custody decreased")
			}
			step = PrecompileRecoveryStep{Operation: "top-up", QuoteHead: head, QuoteSourceRao: sampleBalance, QuoteDestinationRao: balance, Move: PrecompileMoveStep{AmountRao: evidence.Recovery.Authorization.Request.TopUpRao, FromBeforeRao: sampleBalance, ToBeforeRao: balance}}
		}
		action, err = preflight(step)
		if err != nil {
			if !precompileRecoveryCallReverted(err) {
				return PrecompileRecoveryStep{}, err
			}
			return PrecompileRecoveryStep{}, fmt.Errorf("%w: bounded funding preflight: %v", errPrecompileRecoveryPending, err)
		}
	}
	step.Action = action
	return step, nil
}

// A durable receipt is followed by its durable postcondition before another
// action is prepared. Crashes at either boundary retain the same signed bytes.
func (self *Executor) advancePrecompileRecovery(ctx context.Context, evidence *PrecompileConformanceEvidence) error {
	if err := self.loadPrecompileRecoveryAuthorization(evidence); err != nil {
		return err
	}
	if count := len(evidence.Recovery.Steps); count > 0 {
		last := evidence.Recovery.Steps[count-1]
		if _, verified := self.verifiedActionEntry(last.Action); !verified {
			if err := self.Execute(ctx, last.Action); err != nil {
				return err
			}
			var err error
			evidence, err = loadPrecompileEvidence(self.stateDir)
			if err != nil {
				return err
			}
		}
	}
	sample, move, settled, err := precompileRecoveryPositions(evidence)
	if err != nil {
		return err
	}
	afterTransfer := evidence.Transfer.TransactionHash != ""
	if settled && move == 0 && (!afterTransfer || sample == 0) {
		return nil
	}
	if !settled {
		return errors.New("probe recovery has an unverified incomplete operation")
	}
	step, err := self.preparePrecompileRecoveryStep(ctx, evidence)
	if err != nil {
		return err
	}
	evidence.Recovery.Steps = append(evidence.Recovery.Steps, step)
	if err := writePrecompileEvidence(self.stateDir, evidence); err != nil {
		return err
	}
	if err := self.Execute(ctx, step.Action); err != nil {
		return err
	}
	updated, err := loadPrecompileEvidence(self.stateDir)
	if err != nil {
		return err
	}
	sample, move, settled, err = precompileRecoveryPositions(updated)
	if err != nil {
		return err
	}
	if !settled || move != 0 || (afterTransfer && sample != 0) {
		return errPrecompileRecoveryPending
	}
	return nil
}

// Looks up an extra action only through its signed supplemental authority; its
// arbitrary journal prefix never supplies permission by itself.
func (self *Executor) precompileRecoveryStepForAction(action Action, evidence *PrecompileConformanceEvidence) (int, error) {
	if evidence == nil || evidence.Recovery == nil {
		return 0, errors.New("probe recovery action has no supplemental authority")
	}
	if err := validatePrecompileRecoveryPlan(self.plan, evidence, &evidence.Recovery.Authorization); err != nil {
		return 0, err
	}
	if _, _, _, err := precompileRecoveryPositions(evidence); err != nil {
		return 0, err
	}
	for index, step := range evidence.Recovery.Steps {
		if step.Action.ID == action.ID {
			if !precompileRecoveryActionEqual(step.Action, action) {
				return 0, errors.New("probe recovery action changed from its signed scope")
			}
			return index, nil
		}
	}
	return 0, errors.New("probe recovery action is absent from its durable sequence")
}

// Receipt reconciliation persists before postcondition verification, allowing
// a crash at either boundary to retain the same call, nonce and transferred value.
func (self *Executor) executePrecompileRecoveryStep(ctx context.Context, action Action) error {
	evidence, err := loadPrecompileEvidence(self.stateDir)
	if err != nil {
		return err
	}
	index, err := self.precompileRecoveryStepForAction(action, evidence)
	if err != nil {
		return err
	}
	step := evidence.Recovery.Steps[index]
	_, data, err := precompileRecoveryAction(&evidence.Recovery.Authorization, index, step)
	if err != nil {
		return err
	}
	probe := common.HexToAddress(evidence.ProbeAddress)
	value, ok := new(big.Int).SetString(action.Parameters["recovery_value_wei"], 10)
	if !ok {
		return errors.New("probe recovery value is invalid")
	}
	receipt, err := self.deployer.Send(ctx, self.plan.PlanHash, action, &probe, value, data)
	if err != nil {
		return err
	}
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
	evidence.Recovery.Steps[index] = observed
	if _, _, _, err := precompileRecoveryPositions(evidence); err != nil {
		return err
	}
	return writePrecompileEvidence(self.stateDir, evidence)
}

// Both ordinary postcondition readers replay the exact receipt and semantic
// envelope. Partial recovery is successful work, but is not final conformance.
func (self *Executor) verifyPrecompileRecoveryPostState(ctx context.Context, action Action, head ChainHead, state map[string]any) (map[string]any, error) {
	evidence, err := loadPrecompileEvidence(self.stateDir)
	if err != nil {
		return nil, err
	}
	index, err := self.precompileRecoveryStepForAction(action, evidence)
	if err != nil {
		return nil, err
	}
	step := evidence.Recovery.Steps[index]
	tx, block, hash := precompileRecoveryReceipt(step)
	entry := JournalEntry{ActionID: action.ID, IntentHash: action.IntentHash, TransactionHash: tx, BlockNumber: block, BlockHash: hash}
	if err := verifyPrecompileRecoveryCall(ctx, ethEVMReceiptFinalityReader{client: self.deployer.client}, head, self.plan, evidence, entry, 0, false); err != nil {
		return nil, err
	}
	state["authorization_hash"], state["step_index"], state["operation"] = evidence.Recovery.Authorization.Hash, index+1, step.Operation
	sample, move, _, err := precompileRecoveryPositions(evidence)
	if err != nil {
		return nil, err
	}
	state["evidence_hash"], state["sample_rao"], state["move_rao"] = evidence.EvidenceHash, sample, move
	state["canonical_chain_evidence"] = true
	return state, nil
}
