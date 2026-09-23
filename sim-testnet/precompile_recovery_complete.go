// Supplemental closure is independently signed after strict on-chain replay.
// It cannot rewrite the campaign result whose deferred scope it closes.
package main

import (
	"context"
	"errors"
	"math/big"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const precompileRecoveryCompletionFilename = "public/precompile-recovery-complete.json"

type PrecompileRecoveryCompletionRecord struct {
	Schema               string         `json:"schema"`
	PlanHash             string         `json:"plan_hash"`
	ConfigHash           string         `json:"config_hash"`
	DeploymentId         string         `json:"deployment_id"`
	AuthorizationHash    string         `json:"authorization_hash"`
	OriginalEvidenceHash string         `json:"original_evidence_hash"`
	EvidenceHash         string         `json:"evidence_hash"`
	JournalHash          string         `json:"journal_hash"`
	FinalizedHead        ChainHead      `json:"finalized_head"`
	SampleRao            uint64         `json:"sample_rao"`
	MoveRao              uint64         `json:"move_rao"`
	Owner                common.Address `json:"owner"`
	Deployer             common.Address `json:"deployer"`
}
type PrecompileRecoveryCompletion struct {
	Record            PrecompileRecoveryCompletionRecord `json:"record"`
	Hash              string                             `json:"hash"`
	OwnerSignature    string                             `json:"owner_signature"`
	DeployerSignature string                             `json:"deployer_signature"`
}

func verifyPrecompileRecoveryCompletion(plan *SetupPlan, evidence *PrecompileConformanceEvidence, completion *PrecompileRecoveryCompletion) error {
	if evidence == nil || evidence.Recovery == nil || completion == nil || !precompileEvidenceComplete(evidence) {
		return errors.New("probe recovery completion has no fully recovered conformance evidence")
	}
	authorization := &evidence.Recovery.Authorization
	if err := validatePrecompileRecoveryPlan(plan, evidence, authorization); err != nil {
		return err
	}
	copy := *evidence
	copy.EvidenceHash = ""
	hash, err := canonicalHashHex(copy)
	record := completion.Record
	if err != nil || hash != evidence.EvidenceHash || record.Schema != "urnetwork-precompile-recovery-complete-v1" || record.PlanHash != plan.PlanHash || record.ConfigHash != evidence.ConfigHash || record.DeploymentId != plan.DeploymentID || record.AuthorizationHash != authorization.Hash || record.OriginalEvidenceHash != authorization.Request.OriginalEvidenceHash || record.EvidenceHash != hash || !validCanonicalHashHex(record.JournalHash) || !validCanonicalHashHex(record.FinalizedHead.Hash) || record.FinalizedHead.Number < evidence.Transfer.BlockNumber || record.SampleRao != 0 || record.MoveRao != 0 || record.Owner != common.HexToAddress(plan.Roles.Owner) || record.Deployer != common.HexToAddress(plan.Roles.Deployer) {
		return errors.New("probe recovery completion changed its signed evidence, custody or identity")
	}
	for _, step := range evidence.Recovery.Steps {
		_, block, _ := precompileRecoveryReceipt(step)
		if block > record.FinalizedHead.Number {
			return errors.New("probe recovery completion precedes a retained repair")
		}
	}
	if err := verifyCoordinatorRepairSignature(record, completion.Hash, completion.OwnerSignature, record.Owner); err != nil {
		return err
	}
	return verifyCoordinatorRepairSignature(record, completion.Hash, completion.DeployerSignature, record.Deployer)
}

// Every receipt, original call, quote and the mature dividend are pinned. Zero
// final balances are read for both hotkeys from the same canonical final head.
func (self *Executor) verifyPrecompileRecoveryFinalEvidence(ctx context.Context, head ChainHead, evidence *PrecompileConformanceEvidence) error {
	if evidence == nil || evidence.Recovery == nil {
		return errors.New("probe recovery final evidence is absent")
	}
	if err := verifyEVMCheckpoint(ctx, self.deployer.client, head, head); err != nil {
		return err
	}
	if err := validatePrecompileRecoveryPlan(self.plan, evidence, &evidence.Recovery.Authorization); err != nil {
		return err
	}
	if !precompileEvidenceComplete(evidence) {
		return errors.New("probe recovery still has incomplete accounting")
	}
	nonce, err := self.deployer.client.NonceAt(ctx, common.HexToAddress(self.plan.Roles.Deployer), new(big.Int).SetUint64(head.Number))
	if err != nil {
		return err
	}
	if err := verifyPrecompileProbeSuccessorCalls(ctx, self.cfg, self.stateDir, self.plan, self.journal.Entries(), ethEVMReceiptFinalityReader{client: self.deployer.client}, head, nonce); err != nil {
		return err
	}
	sample, err := decodeHex32("recovery sample", evidence.SampleHotkey)
	if err != nil {
		return err
	}
	move, err := decodeHex32("recovery move", evidence.MoveHotkey)
	if err != nil {
		return err
	}
	provider, err := decodeHex32("recovery recipient", evidence.RecoveryColdkey)
	if err != nil {
		return err
	}
	probe := common.HexToAddress(evidence.ProbeAddress)
	cold := ss58Mirror(probe)
	for _, step := range evidence.Recovery.Steps {
		if err := verifyEVMCheckpoint(ctx, self.deployer.client, head, step.QuoteHead); err != nil {
			return err
		}
		source, destinationCold, destinationHotkey := move, provider, move
		if step.Operation == "top-up" {
			source, destinationCold, destinationHotkey = sample, cold, move
		} else if precompileRecoveryAfterTransfer(step) {
			source, destinationCold, destinationHotkey = sample, provider, sample
		}
		balance, err := self.readStakeAt(ctx, step.QuoteHead.Number, source, cold)
		if err != nil || balance != step.QuoteSourceRao {
			return stateMismatchError(err, "probe recovery quote changed its source balance")
		}
		if step.Operation != "reseed-sample" {
			balance, err = self.readStakeAt(ctx, step.QuoteHead.Number, destinationHotkey, destinationCold)
			if err != nil || balance != step.QuoteDestinationRao {
				return stateMismatchError(err, "probe recovery quote changed its destination balance")
			}
		}
	}
	record := evidence.Recovery
	if err := verifyEVMCheckpoint(ctx, self.deployer.client, head, record.SampleTransferQuoteHead); err != nil {
		return err
	}
	for _, quote := range []struct {
		cold [32]byte
		want uint64
	}{{cold: cold, want: record.SampleTransferSourceQuoteRao}, {cold: provider, want: record.SampleTransferDestinationQuoteRao}} {
		got, err := self.readStakeAt(ctx, record.SampleTransferQuoteHead.Number, sample, quote.cold)
		if err != nil || got != quote.want {
			return stateMismatchError(err, "original sample transfer quote changed")
		}
	}
	if err := verifyEVMCheckpoint(ctx, self.deployer.client, head, evidence.Dividend.FinalizedHead); err != nil {
		return err
	}
	parsed, err := self.precompileABI()
	if err != nil {
		return err
	}
	baseline, current, since, err := readDividendAt(ctx, self.deployer.client, probe, parsed, sample, evidence.Dividend.FinalizedHead.Number)
	if err != nil || baseline != evidence.Dividend.BaselineRao || current < evidence.Dividend.CurrentRao || since != evidence.Dividend.SinceBlock {
		return stateMismatchError(err, "retained dividend is not proved at its historical checkpoint")
	}
	code, err := self.deployer.client.CodeAt(ctx, probe, new(big.Int).SetUint64(head.Number))
	if err != nil || crypto.Keccak256Hash(code).Hex() != record.Authorization.Request.ProbeRuntimeHash {
		return stateMismatchError(err, "recovery probe runtime changed")
	}
	for _, hotkey := range [][32]byte{sample, move} {
		balance, err := self.readStakeAt(ctx, head.Number, hotkey, cold)
		if err != nil || balance != 0 {
			return stateMismatchError(err, "probe recovery final custody is not zero: %d", balance)
		}
	}
	return nil
}

func (self *Executor) completePrecompileRecovery(ctx context.Context) (*PrecompileRecoveryCompletion, error) {
	evidence, err := loadPrecompileEvidence(self.stateDir)
	if err != nil {
		return nil, err
	}
	head, err := finalizedEVMHead(ctx, self.deployer.client)
	if err != nil {
		return nil, err
	}
	if err := self.verifyPrecompileRecoveryFinalEvidence(ctx, head, evidence); err != nil {
		return nil, err
	}
	entries := self.journal.Entries()
	if len(entries) == 0 {
		return nil, errors.New("probe recovery has no final journal checkpoint")
	}
	record := PrecompileRecoveryCompletionRecord{Schema: "urnetwork-precompile-recovery-complete-v1", PlanHash: self.plan.PlanHash, ConfigHash: evidence.ConfigHash, DeploymentId: self.plan.DeploymentID, AuthorizationHash: evidence.Recovery.Authorization.Hash, OriginalEvidenceHash: evidence.Recovery.Authorization.Request.OriginalEvidenceHash, EvidenceHash: evidence.EvidenceHash, JournalHash: entries[len(entries)-1].EntryHash, FinalizedHead: head, Owner: common.HexToAddress(self.plan.Roles.Owner), Deployer: common.HexToAddress(self.plan.Roles.Deployer)}
	completion := &PrecompileRecoveryCompletion{Record: record}
	for _, name := range []string{coordinatorRepairOwnerRole, coordinatorRepairDeployerRole} {
		role, err := self.roles.EVMKey(name)
		if err != nil {
			return nil, err
		}
		key, err := crypto.HexToECDSA(role.PrivateKeyHex)
		if err != nil {
			return nil, err
		}
		hash, signature, err := coordinatorRepairSignature(record, key)
		if err != nil {
			return nil, err
		}
		completion.Hash = hash
		if name == coordinatorRepairOwnerRole {
			completion.OwnerSignature = signature
		} else {
			completion.DeployerSignature = signature
		}
	}
	if err := verifyPrecompileRecoveryCompletion(self.plan, evidence, completion); err != nil {
		return nil, err
	}
	path := filepath.Join(self.stateDir, precompileRecoveryCompletionFilename)
	if err := validatePrecompileRecoveryArtifactPath(self.stateDir, path); err != nil {
		return nil, err
	}
	if err := writeCoordinatorRepairFile(path, completion); err != nil {
		return nil, err
	}
	return completion, nil
}
