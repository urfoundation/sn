// Carrying an immutable journal requires the original approved source graph,
// signed transactions and finalized receipts; current getters alone are not
// authority to reuse old code or to allocate a replacement CREATE nonce.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfoundation/sn/stabi"
)

const (
	validatorEvidenceArtifactParameter  = "validator_evidence_source_artifact_hash"
	validatorEvidenceSourceMaximumBytes = 512 * 1024
)

// The already approved plan archives the exact original lock and generated
// artifact before the executor can send CREATE. Subsequent locks may differ.
type ValidatorEvidenceSource struct {
	Schema      string           `json:"schema"`
	ReleaseLock *ReleaseLock     `json:"release_lock"`
	Artifact    ContractArtifact `json:"artifact"`
}

// Each original transaction keeps its own approved plan, action intent and
// persisted verification. A later plan may not substitute a same-shaped row.
type ValidatorEvidenceCarryReceipt struct {
	PlanHash          string `json:"plan_hash"`
	IntentHash        string `json:"intent_hash"`
	TransactionHash   string `json:"transaction_hash"`
	BlockNumber       uint64 `json:"block_number"`
	BlockHash         string `json:"block_hash"`
	PostconditionHash string `json:"postcondition_hash"`
}

// SourcePlanHash identifies the original CREATE approval, never the newly
// built candidate. Original actions and spend remain present exactly once.
type ValidatorEvidenceCarry struct {
	Schema                string                        `json:"schema"`
	SourcePlanHash        string                        `json:"source_plan_hash"`
	SourceReleaseLockHash string                        `json:"source_release_lock_hash"`
	Creation              ValidatorEvidenceCarryReceipt `json:"creation"`
	Anchor                ValidatorEvidenceCarryReceipt `json:"anchor"`
}

// This nonserialized observation can be constructed only by the read-only
// source/journal/finalized-chain verifier, then consumed by revision rendering.
type validatorEvidenceCarryObservation struct {
	reference  ValidatorEvidenceCarry
	sourcePlan *SetupPlan
	payloads   *validatorEvidenceDeploymentPayloads
}

// Historical files share the already-qualified no-follow, nonblocking
// descriptor walk. A FIFO must never wait for a writer before type admission.
func readValidatorEvidenceHistoricalFile(stateDir, name string, maximum int64) ([]byte, error) {
	return readValidatorEvidenceHistoricalFileObserved(stateDir, name, maximum, nil)
}

// The private observation boundary exposes the actual owned descriptor for
// deterministic custody tests; it cannot replace the production opener.
func readValidatorEvidenceHistoricalFileObserved(stateDir, name string, maximum int64, opened func(*os.File) error) (result []byte, resultErr error) {
	if err := validateCampaignEvidencePath(name); err != nil {
		return nil, err
	}
	if maximum <= 0 || maximum > maximumCampaignEvidenceRawFileBytes {
		return nil, errors.New("validator evidence history file bound is invalid")
	}
	file, err := openFinalCollectedFile(stateDir, filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
		if resultErr != nil {
			result = nil
		}
	}()
	if opened != nil {
		if err := opened(file); err != nil {
			return nil, err
		}
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum {
		return nil, errors.New("validator evidence historical file is not regular or exceeds its bound")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != info.Size() || int64(len(raw)) > maximum {
		return nil, errors.New("validator evidence historical file changed size or exceeds its bound")
	}
	return raw, nil
}

// Historical decoding is a separate read-only path. It can establish source
// provenance, never permission to execute an old initializer as a fresh plan.
func readValidatorEvidenceHistoricalPlan(stateDir, hash string) (*SetupPlan, error) {
	if value, err := decodeHex32("validator evidence historical plan", hash); err != nil || value == ([32]byte{}) {
		return nil, errors.New("validator evidence historical plan hash is invalid")
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, "plans/"+stringsTrim0x(hash)+".json", maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, err
	}
	plan, err := decodePersistedPlanBytesForHistory(raw, true)
	if err != nil {
		return nil, err
	}
	if plan.PlanHash != hash {
		return nil, errors.New("validator evidence historical plan path differs from its approval")
	}
	return plan, nil
}

// Do not infer a previous installation from a fresh v12 pointer. Only actual
// journal progress invokes carry authentication; a partial history refuses.
func validatorEvidenceHistoryRequired(plan *SetupPlan, entries []JournalEntry) bool {
	if plan == nil {
		return false
	}
	if plan.ValidatorEvidenceCarry != nil {
		return true
	}
	allowed := plan.allowedPlanHashes()
	for _, entry := range entries {
		if allowed[entry.PlanHash] && (entry.ActionID == validatorEvidenceDeployActionID || entry.ActionID == validatorEvidenceAnchorActionID) && (entry.Stage == StageBroadcast || entry.Stage == StageIncluded || entry.Stage == StageFinalized || entry.Stage == StageVerified) {
			return true
		}
	}
	return false
}

// The receipt owner is the original transaction's approved plan. Repeated
// verification in later plans cannot replace missing source finalization.
func validatorEvidenceHistoryReceipt(plan *SetupPlan, entries []JournalEntry, action Action) (ValidatorEvidenceCarryReceipt, JournalEntry, JournalEntry, error) {
	zero := ValidatorEvidenceCarryReceipt{}
	var finalized, verified, broadcast *JournalEntry
	allowed := plan.allowedPlanHashes()
	for _, entry := range entries {
		if !allowed[entry.PlanHash] || entry.ActionID != action.ID {
			continue
		}
		if entry.DeploymentID != plan.DeploymentID || entry.IntentHash != action.IntentHash {
			return zero, JournalEntry{}, JournalEntry{}, errors.New("validator evidence journal contains competing approved action history")
		}
		if entry.Stage == StageFinalized {
			if finalized != nil {
				return zero, JournalEntry{}, JournalEntry{}, errors.New("validator evidence history has duplicate finalized transactions")
			}
			copy := entry
			finalized = &copy
		}
	}
	if finalized == nil {
		return zero, JournalEntry{}, JournalEntry{}, errors.New("validator evidence history lacks its original finalized transaction")
	}
	for _, entry := range entries {
		if entry.PlanHash != finalized.PlanHash || entry.ActionID != action.ID || entry.IntentHash != action.IntentHash {
			continue
		}
		switch entry.Stage {
		case StageBroadcast:
			if broadcast != nil || entry.TransactionHash != finalized.TransactionHash {
				return zero, JournalEntry{}, JournalEntry{}, errors.New("validator evidence original broadcast is ambiguous")
			}
			copy := entry
			broadcast = &copy
		case StageVerified:
			if verified != nil {
				return zero, JournalEntry{}, JournalEntry{}, errors.New("validator evidence original verification is ambiguous")
			}
			copy := entry
			verified = &copy
		}
	}
	if broadcast == nil || verified == nil || broadcast.Sequence >= finalized.Sequence || verified.Sequence <= finalized.Sequence {
		return zero, JournalEntry{}, JournalEntry{}, errors.New("validator evidence broadcast, finalization and verification are not an ordered original history")
	}
	return ValidatorEvidenceCarryReceipt{PlanHash: finalized.PlanHash, IntentHash: action.IntentHash, TransactionHash: finalized.TransactionHash, BlockNumber: finalized.BlockNumber, BlockHash: finalized.BlockHash, PostconditionHash: verified.PostconditionHash}, *broadcast, *verified, nil
}

// Archived source identities must match the independently approved deployment
// and custody graph. Only the coordinator implementation may be newer.
func validatorEvidenceSourcePlanMatches(current, source *SetupPlan) error {
	if current == nil || source == nil || !current.allowedPlanHashes()[source.PlanHash] || current.ValidatorEvidence == nil || source.ValidatorEvidence == nil || *current.ValidatorEvidence != *source.ValidatorEvidence ||
		current.DeploymentID != source.DeploymentID || current.ChainID != source.ChainID || current.GenesisHash != source.GenesisHash || current.Netuid != source.Netuid || current.Owner != source.Owner || !reflect.DeepEqual(current.Roles, source.Roles) ||
		!contractDeploymentAddressesEqual(current.Deployment, source.Deployment) || !contractDeploymentRuntimeHashesCompatible(current.Deployment, source.Deployment) {
		return errors.New("validator evidence source approval differs from retained immutable custody")
	}
	currentSource, err := canonicalHashHex(current.ValidatorEvidenceSource)
	if err != nil {
		return err
	}
	originalSource, err := canonicalHashHex(source.ValidatorEvidenceSource)
	if err != nil {
		return err
	}
	if currentSource != originalSource {
		return errors.New("validator evidence source archive differs across approved lineage")
	}
	for _, id := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID} {
		want, err := exactPlanActionByID(current, id)
		if err != nil {
			return err
		}
		got, err := exactPlanActionByID(source, id)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(want, got) {
			return fmt.Errorf("validator evidence source action %s changed across approved lineage", id)
		}
	}
	return nil
}

// Retain exact signed bytes, not just an RPC's self-reported transaction hash.
// This reader does not send, estimate, recover, or choose a new account nonce.
func readValidatorEvidenceSourceTransaction(stateDir string, plan *SetupPlan, action Action, reference ValidatorEvidenceCarryReceipt, broadcast JournalEntry) (*types.Transaction, error) {
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, "transactions/"+stringsTrim0x(reference.TransactionHash)+".rlp", 64*1024)
	if err != nil {
		return nil, err
	}
	var transaction types.Transaction
	if err := transaction.UnmarshalBinary(raw); err != nil {
		return nil, err
	}
	canonical, err := transaction.MarshalBinary()
	if err != nil || !bytes.Equal(raw, canonical) {
		return nil, errors.New("validator evidence source transaction is not canonical RLP")
	}
	chainID := new(big.Int).SetUint64(plan.ChainID)
	if transaction.Hash().Hex() != reference.TransactionHash || !transaction.Protected() || transaction.ChainId().Cmp(chainID) != 0 {
		return nil, errors.New("validator evidence source transaction hash or protected chain differs")
	}
	signer, err := types.Sender(types.LatestSignerForChainID(chainID), &transaction)
	if err != nil {
		return nil, err
	}
	if broadcast.Signer != signer.Hex() || broadcast.Nonce != strconv.FormatUint(transaction.Nonce(), 10) {
		return nil, errors.New("validator evidence source transaction differs from its original signer or nonce")
	}
	if err := validateApprovedEVMTransactionFields(action, signer, transaction.Nonce(), transaction.To(), transaction.Value(), transaction.Data()); err != nil {
		return nil, err
	}
	// The original manager's legacy/dynamic-fee calls contain no auxiliary
	// account authorizations or blob effects beyond the approved operation.
	if transaction.Type() != types.LegacyTxType && transaction.Type() != types.DynamicFeeTxType {
		return nil, errors.New("validator evidence source transaction has an unapproved envelope type")
	}
	maximumGas, maximumFee, err := evmActionFeeEnvelope(action)
	if err != nil {
		return nil, err
	}
	fee, tip := transaction.GasFeeCap(), transaction.GasTipCap()
	if transaction.Gas() == 0 || transaction.Gas() > maximumGas || fee == nil || fee.Sign() < 0 || !fee.IsUint64() || fee.Uint64() > maximumFee || tip == nil || tip.Sign() < 0 || tip.Cmp(fee) > 0 {
		return nil, errors.New("validator evidence source transaction exceeds its approved gas or fee ceiling")
	}
	maximumCost := new(big.Int).Mul(new(big.Int).SetUint64(transaction.Gas()), fee)
	approvedCost, err := action.Spend.EVMGasWei.Big()
	if err != nil || maximumCost.Cmp(approvedCost) > 0 {
		return nil, errors.New("validator evidence source transaction exceeds its original approved spend")
	}
	return &transaction, nil
}

// The carry path admits receipt bytes before decoding. Evidence installation
// was introduced with v4 receipts, so no legacy receipt/hash fallback applies.
func readValidatorEvidenceSourcePostcondition(stateDir string, cfg *ResolvedConfig, plan *SetupPlan, entry JournalEntry) (*ActionPostcondition, error) {
	expected, err := postconditionRelativePath(entry.PlanHash, entry.ActionID)
	if err != nil || entry.PostconditionPath != expected {
		return nil, errors.New("validator evidence source postcondition path is not canonical")
	}
	raw, err := readValidatorEvidenceHistoricalFile(stateDir, expected, maximumCampaignEvidenceRawFileBytes)
	if err != nil {
		return nil, err
	}
	record, err := decodeActionPostcondition(raw)
	if err != nil {
		return nil, err
	}
	if err := validateActionPostconditionV4(record); err != nil {
		return nil, err
	}
	if cfg == nil || plan == nil || record.DeploymentID != plan.DeploymentID || record.DeploymentID != cfg.Config.Deployment.DeploymentID || record.PlanHash != entry.PlanHash || !plan.allowedPlanHashes()[record.PlanHash] || record.ActionID != entry.ActionID || record.IntentHash != entry.IntentHash {
		return nil, errors.New("validator evidence source postcondition differs from original approved identity")
	}
	if err := historicalPostconditionRPCIdentity(stateDir, cfg, plan, record); err != nil {
		return nil, err
	}
	hash, err := canonicalHashHex(record)
	if err != nil {
		return nil, err
	}
	if hash != entry.PostconditionHash {
		return nil, errors.New("validator evidence source postcondition differs from its journal hash")
	}
	return record, nil
}

// Inclusion is independently checked at each provider's finalized EVM hash;
// an Ethereum Header hash must not substitute for the runtime RPC hash.
func verifyValidatorEvidenceSourceReceipt(ctx context.Context, client *ethclient.Client, head ChainHead, action Action, reference ValidatorEvidenceCarryReceipt, transaction *types.Transaction, payloads *validatorEvidenceDeploymentPayloads) error {
	reader := ethEVMReceiptFinalityReader{client: client}
	if err := verifyEVMCheckpointFromReader(ctx, reader, head, ChainHead{Number: reference.BlockNumber, Hash: reference.BlockHash}); err != nil {
		return err
	}
	receipt, err := client.TransactionReceipt(ctx, transaction.Hash())
	if err != nil {
		return err
	}
	if !receiptMatchesEvidence(head, receipt, reference.TransactionHash, reference.BlockNumber, reference.BlockHash) {
		return errors.New("validator evidence source receipt differs from finalized approval")
	}
	if action.ID == validatorEvidenceDeployActionID && receipt.ContractAddress != payloads.Manifest.Address || action.ID == validatorEvidenceAnchorActionID && receipt.ContractAddress != (common.Address{}) {
		return errors.New("validator evidence source receipt has another CREATE or call identity")
	}
	actual, pending, err := client.TransactionByHash(ctx, transaction.Hash())
	if err != nil {
		return err
	}
	if actual == nil || pending {
		return errors.New("validator evidence source transaction is missing or pending")
	}
	want, err := transaction.MarshalBinary()
	if err != nil {
		return err
	}
	got, err := actual.MarshalBinary()
	if err != nil {
		return err
	}
	if !bytes.Equal(want, got) {
		return errors.New("validator evidence included transaction differs from original signed bytes")
	}
	_, err = readValidatorEvidenceDeploymentAtHead(ctx, client, payloads, ChainHead{Number: reference.BlockNumber, Hash: reference.BlockHash}, action.ID == validatorEvidenceAnchorActionID)
	return errors.Join(err, ctx.Err())
}

// One read-only authority check serves revision planning and execution. Every
// caller supplies the exact journal, original plan archive and live clients.
func authenticateValidatorEvidenceCarry(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry, operational, independent *ethclient.Client) (result *validatorEvidenceCarryObservation, resultErr error) {
	if ctx == nil {
		return nil, errors.New("validator evidence carry context is absent")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg == nil || cfg.Config == nil || cfg.Public == nil || plan == nil || operational == nil || independentRPCRequired(cfg) && independent == nil {
		return nil, errors.New("validator evidence carry reader ownership is incomplete")
	}
	if plan.DeploymentID != cfg.Config.Deployment.DeploymentID || plan.ChainID != cfg.ChainID || plan.GenesisHash != cfg.Public.Chain.GenesisHash || plan.Netuid != cfg.Netuid || plan.Owner != cfg.WalletPublic {
		return nil, errors.New("validator evidence carry differs from independently configured immutable domain")
	}
	if err := validateValidatorEvidenceSource(plan, true); err != nil {
		return nil, err
	}
	if err := validateValidatorEvidencePlan(plan); err != nil {
		return nil, err
	}
	creationAction, err := exactPlanActionByID(plan, validatorEvidenceDeployActionID)
	if err != nil {
		return nil, err
	}
	anchorAction, err := exactPlanActionByID(plan, validatorEvidenceAnchorActionID)
	if err != nil {
		return nil, err
	}
	creation, creationBroadcast, creationVerified, err := validatorEvidenceHistoryReceipt(plan, entries, creationAction)
	if err != nil {
		return nil, err
	}
	anchor, anchorBroadcast, anchorVerified, err := validatorEvidenceHistoryReceipt(plan, entries, anchorAction)
	if err != nil {
		return nil, err
	}
	source, err := readValidatorEvidenceHistoricalPlan(stateDir, creation.PlanHash)
	if err != nil {
		return nil, err
	}
	if source.ValidatorEvidenceCarry != nil {
		return nil, errors.New("validator evidence original CREATE approval is itself a carry")
	}
	if err := validatorEvidenceSourcePlanMatches(plan, source); err != nil {
		return nil, err
	}
	anchorPlan := source
	if anchor.PlanHash != source.PlanHash {
		anchorPlan, err = readValidatorEvidenceHistoricalPlan(stateDir, anchor.PlanHash)
		if err != nil {
			return nil, err
		}
		if !anchorPlan.allowedPlanHashes()[source.PlanHash] {
			return nil, errors.New("validator evidence anchor does not descend from original CREATE approval")
		}
		if err := validatorEvidenceSourcePlanMatches(plan, anchorPlan); err != nil {
			return nil, err
		}
	}
	if anchor.BlockNumber < creation.BlockNumber || anchor.TransactionHash == creation.TransactionHash || creationVerified.Sequence >= anchorBroadcast.Sequence {
		return nil, errors.New("validator evidence anchor precedes its original verified CREATE")
	}
	reference := ValidatorEvidenceCarry{Schema: "urnetwork-validator-evidence-carry-v1", SourcePlanHash: source.PlanHash, SourceReleaseLockHash: source.ReleaseLockHash, Creation: creation, Anchor: anchor}
	if plan.ValidatorEvidenceCarry != nil && *plan.ValidatorEvidenceCarry != reference {
		return nil, errors.New("validator evidence carry reference differs from authenticated source history")
	}
	payloads, err := validatorEvidencePayloadsForPlan(source)
	if err != nil {
		return nil, err
	}
	creationTransaction, err := readValidatorEvidenceSourceTransaction(stateDir, source, creationAction, creation, creationBroadcast)
	if err != nil {
		return nil, err
	}
	anchorTransaction, err := readValidatorEvidenceSourceTransaction(stateDir, anchorPlan, anchorAction, anchor, anchorBroadcast)
	if err != nil {
		return nil, err
	}
	observer := &Executor{cfg: cfg, stateDir: stateDir, plan: plan, payloads: &DeploymentPayloads{Manifest: plan.Deployment, ValidatorEvidence: payloads}, deployer: &EvmTxManager{client: operational}, independentEVM: independent}
	for _, item := range []struct {
		action      Action
		reference   ValidatorEvidenceCarryReceipt
		transaction *types.Transaction
		verified    JournalEntry
	}{
		{action: creationAction, reference: creation, transaction: creationTransaction, verified: creationVerified},
		{action: anchorAction, reference: anchor, transaction: anchorTransaction, verified: anchorVerified},
	} {
		record, err := readValidatorEvidenceSourcePostcondition(stateDir, cfg, plan, item.verified)
		if err != nil {
			return nil, err
		}
		if record.EVMFinalized.Number < item.reference.BlockNumber || record.IndependentEVMFinalized.Number < item.reference.BlockNumber {
			return nil, errors.New("validator evidence source verification predates transaction inclusion")
		}
		if err := observer.verifyHistoricalEVMPostcondition(ctx, item.action, record); err != nil {
			return nil, err
		}
		for _, client := range []*ethclient.Client{operational, independent} {
			if client == nil {
				continue
			}
			chainID, err := client.ChainID(ctx)
			if err != nil {
				return nil, err
			}
			if chainID == nil || chainID.Cmp(new(big.Int).SetUint64(plan.ChainID)) != 0 {
				return nil, errors.New("validator evidence carry provider has another EVM chain")
			}
			head, err := finalizedEVMHead(ctx, client)
			if err != nil {
				return nil, err
			}
			if err := verifyValidatorEvidenceSourceReceipt(ctx, client, head, item.action, item.reference, item.transaction, payloads); err != nil {
				return nil, err
			}
			if _, err := readValidatorEvidenceDeploymentAtHead(ctx, client, payloads, head, true); err != nil {
				return nil, err
			}
		}
	}
	return &validatorEvidenceCarryObservation{reference: reference, sourcePlan: source, payloads: payloads}, nil
}

// The planner opens only read clients and closes them before returning an
// observation. No mutation manager or transaction submission exists here.
func observeValidatorEvidenceCarry(ctx context.Context, cfg *ResolvedConfig, stateDir string, plan *SetupPlan, entries []JournalEntry) (*validatorEvidenceCarryObservation, error) {
	if !validatorEvidenceHistoryRequired(plan, entries) {
		return nil, nil
	}
	if ctx == nil || cfg == nil || cfg.Config == nil || cfg.Public == nil {
		return nil, errors.New("validator evidence carry planning owner is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	var independent *ethclient.Client
	if independentRPCRequired(cfg) {
		independent, err = dialConfiguredEVMClient(ctx, cfg, cfg.Public.Chain.EVMPublicReadEndpoint)
		if err != nil {
			return nil, err
		}
		defer independent.Close()
	}
	return authenticateValidatorEvidenceCarry(ctx, cfg, stateDir, plan, entries, client, independent)
}

// Every execution/resume reopens original authority, even when payload bytes
// are cached or no verified action survived in the current journal snapshot.
func (self *Executor) authenticateValidatorEvidenceCarry(ctx context.Context) (*validatorEvidenceCarryObservation, error) {
	if self == nil || self.plan == nil || self.plan.ValidatorEvidenceCarry == nil || self.journal == nil || self.deployer == nil || self.deployer.client == nil {
		return nil, errors.New("validator evidence execution carry owner is incomplete")
	}
	return authenticateValidatorEvidenceCarry(ctx, self.cfg, self.stateDir, self.plan, self.journal.Entries(), self.deployer.client, self.independentEVM)
}

// A carried CREATE/link action is a verify-only operation. Its original exact
// envelope is retained, never interpreted as permission to spend another nonce.
func (self *Executor) verifyValidatorEvidenceCarryAction(ctx context.Context, action Action) error {
	if self == nil || self.plan == nil || (action.ID != validatorEvidenceDeployActionID && action.ID != validatorEvidenceAnchorActionID) {
		return errors.New("validator evidence carried action is unavailable")
	}
	expected, err := exactPlanActionByID(self.plan, action.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(action, expected) {
		return errors.New("validator evidence carried action differs from its exact approved envelope")
	}
	_, err = self.authenticateValidatorEvidenceCarry(ctx)
	return err
}

// Rebuilding only immutable words from authenticated original source also
// checks collision with every newly selected upgrade, batcher and probe.
func bindValidatorEvidenceCarryPayloads(payloads *DeploymentPayloads, observed *validatorEvidenceCarryObservation) error {
	if payloads == nil || observed == nil || observed.sourcePlan == nil || observed.payloads == nil {
		return errors.New("validator evidence retained payload authority is unavailable")
	}
	original := observed.payloads
	next, err := buildValidatorEvidenceDeploymentFromArtifact(payloads, original.Manifest.ChainID, original.Manifest.GenesisHash, original.Manifest.Netuid, original.Manifest.DeployerNonce, original.Artifact)
	if err != nil {
		return err
	}
	if next.Manifest != original.Manifest || !bytes.Equal(next.Creation, original.Creation) || !bytes.Equal(next.Runtime, original.Runtime) || !bytes.Equal(next.Anchor, original.Anchor) {
		return errors.New("validator evidence retained payload changed during revision")
	}
	payloads.ValidatorEvidence, payloads.validatorEvidenceCarry = next, observed
	return nil
}

// Preserve the exact two approved actions once. The ordinary spend reducer
// recognizes their original verified intents instead of charging them again.
func carryValidatorEvidencePlan(revised, prior *SetupPlan, observed *validatorEvidenceCarryObservation) error {
	if revised == nil || revised.ValidatorEvidence == nil || prior == nil || observed == nil || observed.sourcePlan == nil || observed.payloads == nil {
		return errors.New("validator evidence carry plan authority is unavailable")
	}
	if err := validatorEvidenceSourcePlanMatches(prior, observed.sourcePlan); err != nil {
		return err
	}
	if !slices.Contains(revised.PriorPlanHashes, observed.reference.SourcePlanHash) {
		return errors.New("validator evidence carry lost original approval lineage")
	}
	source, err := newValidatorEvidenceSource(observed.sourcePlan.ValidatorEvidenceSource.ReleaseLock, observed.payloads.Artifact)
	if err != nil {
		return err
	}
	generated := *revised.ValidatorEvidence
	manifest, reference := observed.payloads.Manifest, observed.reference
	revised.ValidatorEvidence, revised.ValidatorEvidenceSource, revised.ValidatorEvidenceCarry = &manifest, source, &reference
	for _, id := range []string{validatorEvidenceDeployActionID, validatorEvidenceAnchorActionID} {
		original, err := exactPlanActionByID(prior, id)
		if err != nil {
			return err
		}
		original.Parameters = maps.Clone(original.Parameters)
		original.DependsOn = slices.Clone(original.DependsOn)
		original.AcceptedPriorIntentHashes = slices.Clone(original.AcceptedPriorIntentHashes)
		count := 0
		for index := range revised.Actions {
			if revised.Actions[index].ID == id {
				revised.Actions[index] = original
				count++
			}
		}
		if count != 1 {
			return errors.New("validator evidence carry action census is not exact")
		}
	}
	// Fresh activation and relay quotas were generated with a predicted CREATE
	// address. Bind those new approvals to the authenticated retained companion;
	// the original deploy/anchor actions above remain exactly unchanged.
	for index := range revised.Actions {
		action := &revised.Actions[index]
		activation := strings.HasPrefix(action.ID, "evidence.activate.")
		boundary, relay := action.ID == runtimeEvidenceActivationBoundaryActionId, action.ID == evidenceRelayReserveId
		if !activation && !boundary && !relay {
			continue
		}
		if action.Target != generated.Address.Hex() || ((activation || boundary) && action.Parameters["mode"] != "fresh-v2") || (relay && action.Parameters["runtime_code_hash"] != generated.RuntimeCodeHash.Hex()) {
			return errors.New("validator evidence dependent quota differs from its generated source")
		}
		intent, err := actionIntentHash(*action)
		if err != nil || intent != action.IntentHash {
			return errors.Join(errors.New("validator evidence dependent quota has an invalid intent"), err)
		}
		action.Target = manifest.Address.Hex()
		if relay {
			action.Parameters = maps.Clone(action.Parameters)
			action.Parameters["runtime_code_hash"] = manifest.RuntimeCodeHash.Hex()
		}
		action.IntentHash, err = actionIntentHash(*action)
		if err != nil {
			return err
		}
	}
	revised.MaximumSpend, err = maximumActionSpend(revised.Actions)
	if err != nil {
		return err
	}
	revised.PlanHash, err = revised.hash()
	return err
}

// The companion consumes the suffix only in its original CREATE generation.
// Later generations have a lower retained nonce and must not count it again.
func validatorEvidenceConsumedNonceBoundary(prior *SetupPlan, next uint64) (uint64, error) {
	if prior == nil || prior.ValidatorEvidence == nil {
		return next, nil
	}
	observed := prior.validatorEvidenceObserved
	if observed == nil {
		return next, nil
	}
	if observed.payloads == nil || observed.payloads.Manifest != *prior.ValidatorEvidence {
		return 0, errors.New("validator evidence nonce boundary lost authenticated carry identity")
	}
	nonce := observed.payloads.Manifest.DeployerNonce
	if nonce < next {
		return next, nil
	}
	if nonce != next || next == ^uint64(0) {
		return 0, errors.New("validator evidence original CREATE is not the exact consumed nonce suffix")
	}
	return next + 1, nil
}

// Generated immutable offsets are copied along with their containing map.
func cloneValidatorEvidenceArtifact(artifact ContractArtifact) ContractArtifact {
	copy := artifact
	copy.ImmutableReferences = maps.Clone(artifact.ImmutableReferences)
	for name, offsets := range copy.ImmutableReferences {
		copy.ImmutableReferences[name] = slices.Clone(offsets)
	}
	return copy
}

// Validate historical bytes before ABI or immutable patching can allocate or
// index from an untrusted declaration. Limits are transport/source ceilings,
// not defaults for validator proof capacity.
func validateValidatorEvidenceSourceArtifact(artifact ContractArtifact) error {
	if artifact.Name != "ValidatorEvidence" || len(artifact.ABI) == 0 || len(artifact.ABI) > 128*1024 ||
		len(artifact.CreationBytecode) == 0 || len(artifact.CreationBytecode) > 2*48*1024+2 ||
		len(artifact.RuntimeBytecode) == 0 || len(artifact.RuntimeBytecode) > 2*24*1024+2 {
		return errors.New("validator evidence source artifact exceeds its format or byte bound")
	}
	creation, err := hex.DecodeString(stringsTrim0x(artifact.CreationBytecode))
	if err != nil || len(creation) == 0 || len(creation) > 48*1024-96 {
		return errors.New("validator evidence source initializer is malformed or exceeds its deployment bound")
	}
	runtime, err := hex.DecodeString(stringsTrim0x(artifact.RuntimeBytecode))
	if err != nil || len(runtime) < 32 || len(runtime) > 24*1024 || crypto.Keccak256Hash(runtime).Hex() != artifact.RuntimeBytecodeHash {
		return errors.New("validator evidence source runtime hash or byte bound is invalid")
	}
	if hash, err := decodeHex32("validator evidence source Foundry artifact", artifact.FoundryArtifactHash); err != nil || hash == ([32]byte{}) || !releaseSHA256.MatchString(artifact.StorageLayoutHash) {
		return errors.New("validator evidence source compiler artifact identity is incomplete")
	}
	if len(artifact.ImmutableReferences) != 6 {
		return errors.New("validator evidence source immutable census is incomplete")
	}
	seenOffsets := map[int]bool{}
	for _, name := range []string{"coordinator", "settlementVault", "chainId", "netuid", "genesisHash", "deploymentIdHash"} {
		offsets := artifact.ImmutableReferences[name]
		if len(offsets) == 0 || len(offsets) > len(runtime)/32 {
			return fmt.Errorf("validator evidence source immutable %s is missing or exceeds its bound", name)
		}
		for _, offset := range offsets {
			if offset < 0 || offset > len(runtime)-32 || seenOffsets[offset] {
				return errors.New("validator evidence source immutable offsets are invalid or duplicated")
			}
			for previous := range seenOffsets {
				if offset < previous+32 && previous < offset+32 {
					return errors.New("validator evidence source immutable fields overlap")
				}
			}
			seenOffsets[offset] = true
		}
	}
	var originalABI, readerABI any
	if err := rejectDuplicatePostconditionJSONFields([]byte(artifact.ABI)); err != nil {
		return fmt.Errorf("validator evidence source ABI fields: %w", err)
	}
	if err := json.Unmarshal([]byte(artifact.ABI), &originalABI); err != nil {
		return fmt.Errorf("validator evidence source ABI: %w", err)
	}
	if err := json.Unmarshal([]byte(stabi.STValidatorEvidenceMetaData.ABI), &readerABI); err != nil {
		return fmt.Errorf("validator evidence reader ABI: %w", err)
	}
	// Abigen removes spaces in internalType annotations. Normalize only that
	// cosmetic spelling, retaining every name, component, type and array order.
	var normalizeInternalTypes func(any, int) error
	normalizeInternalTypes = func(value any, depth int) error {
		if depth > 64 {
			return errors.New("validator evidence ABI nesting exceeds the known interface bound")
		}
		switch typed := value.(type) {
		case map[string]any:
			if raw, exists := typed["internalType"]; exists {
				name, ok := raw.(string)
				if !ok {
					return errors.New("validator evidence ABI internal type is not a string")
				}
				typed["internalType"] = strings.ReplaceAll(name, " ", "")
			}
			for _, child := range typed {
				if err := normalizeInternalTypes(child, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range typed {
				if err := normalizeInternalTypes(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := normalizeInternalTypes(originalABI, 0); err != nil {
		return err
	}
	if err := normalizeInternalTypes(readerABI, 0); err != nil {
		return err
	}
	if !reflect.DeepEqual(originalABI, readerABI) {
		return errors.New("validator evidence historical interface is not the reviewed reader ABI")
	}
	return nil
}

// Snapshot the lock without borrowing its maps; the existing plan archival
// stores this bounded source along with the approval before any chain write.
func newValidatorEvidenceSource(lock *ReleaseLock, artifact ContractArtifact) (*ValidatorEvidenceSource, error) {
	if lock == nil {
		return nil, errors.New("validator evidence source release lock is unavailable")
	}
	if err := validateValidatorEvidenceSourceArtifact(artifact); err != nil {
		return nil, err
	}
	source := &ValidatorEvidenceSource{Schema: "urnetwork-validator-evidence-source-v1", ReleaseLock: lock, Artifact: cloneValidatorEvidenceArtifact(artifact)}
	raw, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	if len(raw) > validatorEvidenceSourceMaximumBytes {
		return nil, errors.New("validator evidence source archive exceeds its byte bound")
	}
	var owned ValidatorEvidenceSource
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&owned); err != nil {
		return nil, err
	}
	return &owned, nil
}

// The original approval binds both artifact contents and release provenance;
// a changed current compiler is relevant only to fresh installs.
func validateValidatorEvidenceSource(plan *SetupPlan, historical bool) error {
	if plan == nil || plan.ValidatorEvidenceSource == nil || plan.ValidatorEvidenceSource.Schema != "urnetwork-validator-evidence-source-v1" || plan.ValidatorEvidenceSource.ReleaseLock == nil {
		return errors.New("validator evidence approved source archive is missing")
	}
	source := plan.ValidatorEvidenceSource
	raw, err := json.Marshal(source)
	if err != nil || len(raw) > validatorEvidenceSourceMaximumBytes {
		return errors.New("validator evidence approved source archive exceeds its byte bound")
	}
	locked, err := canonicalHashHex(source.ReleaseLock)
	if err != nil {
		return err
	}
	expectedLock := plan.ReleaseLockHash
	if plan.ValidatorEvidenceCarry != nil {
		expectedLock = plan.ValidatorEvidenceCarry.SourceReleaseLockHash
	}
	if locked != expectedLock {
		return errors.New("validator evidence source release lock differs from its approval")
	}
	if err := validateValidatorEvidenceSourceArtifact(source.Artifact); err != nil {
		return err
	}
	if !historical {
		artifact, err := currentValidatorEvidenceArtifact()
		if err != nil {
			return err
		}
		current, err := canonicalHashHex(artifact)
		if err != nil {
			return err
		}
		actual, err := canonicalHashHex(source.Artifact)
		if err != nil {
			return err
		}
		if actual != current {
			return errors.New("fresh validator evidence source differs from the current generated artifact")
		}
	} else {
		if err := validateReleaseLockStatic(source.ReleaseLock); err != nil {
			return fmt.Errorf("validator evidence original release lock: %w", err)
		}
		for key, expected := range map[string]string{
			"validator_evidence_artifact_hash":       source.Artifact.FoundryArtifactHash,
			"validator_evidence_runtime_hash":        source.Artifact.RuntimeBytecodeHash,
			"validator_evidence_storage_layout_hash": source.Artifact.StorageLayoutHash,
		} {
			actual, err := lockString(source.ReleaseLock.EVMBuild, key)
			if err != nil || actual != expected {
				return fmt.Errorf("validator evidence original release does not authenticate %s", key)
			}
		}
	}
	return nil
}

// A persisted carry is a request for verification, not evidence by itself.
func validateValidatorEvidenceCarryShape(plan *SetupPlan) error {
	if plan == nil || plan.ValidatorEvidenceCarry == nil {
		return errors.New("validator evidence carry reference is unavailable")
	}
	carry := plan.ValidatorEvidenceCarry
	if carry.Schema != "urnetwork-validator-evidence-carry-v1" || carry.SourcePlanHash == plan.PlanHash || !slices.Contains(plan.PriorPlanHashes, carry.SourcePlanHash) || carry.Creation.PlanHash != carry.SourcePlanHash {
		return errors.New("validator evidence carry has no original approved source lineage")
	}
	for _, value := range []string{carry.SourcePlanHash, carry.SourceReleaseLockHash} {
		if hash, err := decodeHex32("validator evidence carry source", value); err != nil || hash == ([32]byte{}) {
			return errors.New("validator evidence carry source hash is invalid")
		}
	}
	for _, receipt := range []ValidatorEvidenceCarryReceipt{carry.Creation, carry.Anchor} {
		if !slices.Contains(plan.PriorPlanHashes, receipt.PlanHash) || receipt.BlockNumber == 0 {
			return errors.New("validator evidence carry receipt is outside approved history")
		}
		for _, value := range []string{receipt.PlanHash, receipt.IntentHash, receipt.TransactionHash, receipt.BlockHash, receipt.PostconditionHash} {
			if hash, err := decodeHex32("validator evidence carry receipt", value); err != nil || hash == ([32]byte{}) {
				return errors.New("validator evidence carry receipt identity is incomplete")
			}
		}
	}
	if carry.Creation.TransactionHash == carry.Anchor.TransactionHash || carry.Anchor.BlockNumber < carry.Creation.BlockNumber {
		return errors.New("validator evidence carry transactions have inconsistent chronology")
	}
	return nil
}
