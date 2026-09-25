// A pause retains its custody baseline and exact signed transaction before
// broadcast. Interrupted post-state reads resume that same owner and nonce.
package main

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// This checkpoint supplements the unchanged plan and durable broadcast row;
// it cannot authorize a replacement transaction or another plan's baseline.
type governancePauseIntent struct {
	ActionId        string `json:"action_id"`
	IntentHash      string `json:"intent_hash"`
	TransactionHash string `json:"transaction_hash"`
	Signer          string `json:"signer"`
	Nonce           uint64 `json:"nonce"`
}

// The account turn covers preparation, checkpoint persistence and finality.
func (self *Executor) governancePause(ctx context.Context, action Action) error {
	return self.governancePauseWithWriter(ctx, action, self.writeGovernanceEvidence)
}

// The writer seam permits deterministic interruption after chain finality;
// production always uses the same atomic public-evidence writer at both stages.
func (self *Executor) governancePauseWithWriter(ctx context.Context, action Action, write func(*GovernanceDrillEvidence) error) error {
	if ctx == nil || self == nil || self.cfg == nil || self.cfg.Config == nil || self.plan == nil || self.payloads == nil || self.guardian == nil || self.guardian.key == nil || self.owner == nil || self.owner.key == nil || self.journal == nil || write == nil {
		return errors.New("governance pause owner is incomplete")
	}
	if self.cfg.readOnlyAudit || self.guardian.readOnly {
		return errors.New("read-only observation cannot prepare a governance pause")
	}
	approved, err := exactPlanActionByID(self.plan, "governance.guardian-pause")
	if err != nil {
		return err
	}
	intentHash, err := actionIntentHash(action)
	if err != nil || action.ID != approved.ID || action.IntentHash != approved.IntentHash || intentHash != approved.IntentHash {
		return errors.New("governance pause differs from its current approved action")
	}
	evidence, err := self.loadGovernanceEvidence()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if evidence != nil && governanceStageRank(evidence.Stage) >= 1 {
		return nil
	}
	if evidence != nil && (evidence.Stage != "pause-prepared" || evidence.PauseIntent == nil) {
		return errors.New("governance pause has an unknown or incomplete preparation")
	}
	release, err := self.guardian.acquireNonceTurn(ctx)
	if err != nil {
		return err
	}
	defer release()
	parsed, err := abi.JSON(strings.NewReader(CoordinatorABI))
	if err != nil {
		return err
	}
	data, err := parsed.Pack("setPaused", true)
	if err != nil {
		return err
	}
	if evidence == nil {
		epoch, noId, _, err := self.findFinalizedEntitlement(ctx)
		if err != nil {
			return err
		}
		before, err := self.governanceSnapshot(ctx, epoch, noId)
		if err != nil {
			return err
		}
		if err := self.validateGovernancePauseBaseline(before); err != nil {
			return err
		}
		evidence = &GovernanceDrillEvidence{Schema: "urnetwork-governance-drill-evidence-v1", DeploymentID: self.cfg.Config.Deployment.DeploymentID, PlanHash: self.plan.PlanHash, Stage: "pause-prepared", Before: before}
	} else {
		// Refuse a missing journal owner before transaction preparation can
		// select another nonce. The baseline is re-read at its original hash.
		if err := self.validateGovernancePauseJournal(action, evidence.PauseIntent); err != nil {
			return err
		}
		if err := self.validateGovernancePauseBaseline(evidence.Before); err != nil {
			return err
		}
		pinned, err := withFinalizedEVMHead(ctx, evidence.Before.FinalizedHead)
		if err != nil {
			return err
		}
		observed, err := self.governanceSnapshot(pinned, evidence.Before.Entitlement.Epoch, evidence.Before.Entitlement.NoID)
		if err != nil {
			return err
		}
		if observed != evidence.Before {
			return errors.New("governance pause retained custody baseline changed")
		}
	}
	proxy := self.payloads.Manifest.CoordinatorProxy
	signed, err := self.guardian.prepareOwnedEVMTransaction(ctx, self.plan.PlanHash, action, &proxy, new(big.Int), data, 0)
	if err != nil {
		return err
	}
	if evidence.PauseIntent == nil {
		evidence.PauseIntent = &governancePauseIntent{ActionId: action.ID, IntentHash: action.IntentHash, TransactionHash: signed.Hash().Hex(), Signer: crypto.PubkeyToAddress(self.guardian.key.PublicKey).Hex(), Nonce: signed.Nonce()}
	}
	if err := self.validateGovernancePauseTransaction(action, evidence.PauseIntent, signed, data); err != nil {
		return err
	}
	if err := write(evidence); err != nil {
		return err
	}
	receipt, err := self.guardian.waitExactTransaction(ctx, self.plan.PlanHash, action, signed)
	if err != nil {
		return err
	}
	if receipt.BlockNumber.Uint64() <= evidence.Before.FinalizedHead.Number {
		return errors.New("governance pause transaction does not follow its custody baseline")
	}
	after, err := self.governanceSnapshot(ctx, evidence.Before.Entitlement.Epoch, evidence.Before.Entitlement.NoID)
	if err != nil || !after.Paused || after.FinalizedHead.Number < receipt.BlockNumber.Uint64() || after.Implementation != evidence.Before.Implementation || after.Owner != evidence.Before.Owner || after.Guardian != evidence.Before.Guardian {
		return stateMismatchError(err, "guardian pause postcondition is false")
	}
	evidence.Stage = "paused"
	if evidence.Transactions == nil {
		evidence.Transactions = map[string]string{}
	}
	evidence.Transactions[action.ID] = receipt.TxHash.Hex()
	return write(evidence)
}

// The pre-mutation snapshot must name the exact configured custody owners.
func (self *Executor) validateGovernancePauseBaseline(before GovernanceCustodySnapshot) error {
	if before.Paused || !strings.EqualFold(before.Implementation, self.payloads.CoordinatorUpgrade.Implementation.Hex()) || !strings.EqualFold(before.Owner, crypto.PubkeyToAddress(self.owner.key.PublicKey).Hex()) || !strings.EqualFold(before.Guardian, crypto.PubkeyToAddress(self.guardian.key.PublicKey).Hex()) || before.FinalizedHead.Number == 0 {
		return errors.New("governance drill must start unpaused on the reviewed implementation and custody owners")
	}
	_, err := decodeHex32("governance pause baseline", before.FinalizedHead.Hash)
	return err
}

// An exact original broadcast row fixes nonce and signer before any replay.
func (self *Executor) validateGovernancePauseJournal(action Action, intent *governancePauseIntent) error {
	if intent == nil || intent.ActionId != action.ID || intent.IntentHash != action.IntentHash || !validCanonicalHashHex(intent.TransactionHash) || !strings.EqualFold(intent.Signer, crypto.PubkeyToAddress(self.guardian.key.PublicKey).Hex()) {
		return errors.New("governance pause transaction owner differs from its approved checkpoint")
	}
	found := false
	for _, entry := range self.journal.Entries() {
		if entry.PlanHash != self.plan.PlanHash || entry.ActionID != action.ID || entry.Stage != StageBroadcast {
			continue
		}
		if entry.DeploymentID != self.cfg.Config.Deployment.DeploymentID || entry.IntentHash != action.IntentHash || entry.TransactionHash != intent.TransactionHash || !strings.EqualFold(entry.Signer, intent.Signer) || entry.Nonce != strconv.FormatUint(intent.Nonce, 10) {
			return errors.New("governance pause checkpoint differs from its original signed nonce")
		}
		found = true
	}
	if !found {
		return errors.New("governance pause checkpoint has no original broadcast owner")
	}
	return nil
}

// Replay authenticates the retained RLP against the pause payload as well as
// its plan, fee envelope, guardian, nonce and durable transaction identity.
func (self *Executor) validateGovernancePauseTransaction(action Action, intent *governancePauseIntent, signed *types.Transaction, data []byte) error {
	if err := self.validateGovernancePauseJournal(action, intent); err != nil {
		return err
	}
	if signed == nil || !signed.Protected() || self.guardian.chainID == nil || signed.ChainId().Cmp(self.guardian.chainID) != 0 || signed.Hash().Hex() != intent.TransactionHash || signed.Nonce() != intent.Nonce || signed.To() == nil || *signed.To() != self.payloads.Manifest.CoordinatorProxy || signed.Value().Sign() != 0 || !bytes.Equal(signed.Data(), data) {
		return errors.New("governance pause retained transaction changed its exact approval")
	}
	signer, err := types.Sender(types.LatestSignerForChainID(self.guardian.chainID), signed)
	if err != nil || !strings.EqualFold(signer.Hex(), intent.Signer) {
		return errors.Join(errors.New("governance pause retained transaction has another guardian"), err)
	}
	gas, fee, err := evmActionFeeEnvelope(action)
	ceiling, ceilingErr := action.Spend.EVMGasWei.Big()
	if err != nil || ceilingErr != nil || signed.Gas() > gas || signed.GasFeeCap().Sign() < 0 || signed.GasFeeCap().Cmp(new(big.Int).SetUint64(fee)) > 0 || new(big.Int).Mul(new(big.Int).SetUint64(signed.Gas()), signed.GasFeeCap()).Cmp(ceiling) > 0 {
		return errors.Join(errors.New("governance pause retained transaction exceeds its approved fee envelope"), err, ceilingErr)
	}
	return nil
}
