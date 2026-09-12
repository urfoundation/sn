package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"

	nativeTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/protocol"
	"github.com/urfoundation/sn/stabi"
)

func validateFleetRenewalOptions(command string, o cliOptions) error {
	used := o.RenewalPlan != "" || o.RenewalTransactionEvidence != "" || o.RenewalValidFrom != 0 || o.RenewalValidTo != 0 || o.RenewalFeePerGas != 0
	if command != "fleet-renew" {
		if used {
			return errors.New("renewal options require fleet-renew")
		}
		return nil
	}
	if o.ProvisionalResume || o.Detach || o.Name != "" {
		return errors.New("fleet-renew requires the ordinary locked release and cannot launch a topology")
	}
	if o.Apply && (o.RenewalPlan == "" || !validCanonicalHashHex(o.PlanHash)) {
		return errors.New("fleet-renew apply requires --renewal-plan and its exact --plan-hash")
	}
	if o.RenewalPlan != "" {
		if !filepath.IsAbs(o.RenewalPlan) || filepath.Clean(o.RenewalPlan) != o.RenewalPlan || o.RenewalTransactionEvidence != "" || o.RenewalValidFrom != 0 || o.RenewalValidTo != 0 || o.RenewalFeePerGas != 0 {
			return errors.New("imported renewal requires one canonical absolute plan path and no replacement terms")
		}
	} else if o.RenewalValidFrom == 0 {
		return errors.New("fleet-renew planning requires an explicit future --renewal-valid-from-epoch")
	}
	if o.RenewalTransactionEvidence != "" && (!filepath.IsAbs(o.RenewalTransactionEvidence) || filepath.Clean(o.RenewalTransactionEvidence) != o.RenewalTransactionEvidence) {
		return errors.New("renewal transaction evidence requires a canonical absolute file path")
	}
	return nil
}

func readFleetRenewalPlan(path string) (*SetupPlan, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumCampaignEvidenceRawFileBytes {
		return nil, errors.New("renewal plan is not a bounded regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	plan, err := decodePersistedPlanBytes(raw)
	if err != nil {
		return nil, err
	}
	if len(plan.FleetRenewals) == 0 {
		return nil, errors.New("approved plan contains no fleet renewal")
	}
	return plan, nil
}

func validateFleetRenewalSource(base, approved *SetupPlan, entries []JournalEntry) error {
	if approved == nil || len(approved.FleetRenewals) == 0 || base == nil {
		return errors.New("renewal source is unavailable")
	}
	renewal := approved.FleetRenewals[len(approved.FleetRenewals)-1]
	want, err := appendFleetRenewalPlan(base, renewal)
	if err != nil {
		return err
	}
	if want.PlanHash != approved.PlanHash {
		return errors.New("renewal alters source actions, identities, or economic limits outside its exact append")
	}
	checkpoint := -1
	for index, entry := range entries {
		if entry.EntryHash == renewal.JournalHash {
			checkpoint = index
			break
		}
	}
	if checkpoint < 0 {
		return errors.New("renewal source journal checkpoint is absent")
	}
	if err := validateFleetLifecycleRenewalPending("", base, entries[:checkpoint+1]); err != nil {
		return err
	}
	planned := map[string]string{}
	for _, a := range approved.Actions {
		if isFleetRenewalAction(a) {
			planned[a.ID] = a.IntentHash
		}
	}
	for _, entry := range entries[checkpoint+1:] {
		if entry.PlanHash != approved.PlanHash || planned[entry.ActionID] != entry.IntentHash {
			return errors.New("deployment journal advanced outside the exact renewal; render a new reviewed plan")
		}
	}
	return nil
}

func runFleetRenewal(ctx context.Context, cfg *ResolvedConfig, stateDir string, o cliOptions) error {
	var plan *SetupPlan
	var err error
	if o.RenewalPlan != "" {
		plan, err = readFleetRenewalPlan(o.RenewalPlan)
	} else {
		plan, err = buildFleetRenewalPlan(ctx, cfg, stateDir, o)
	}
	if err != nil {
		var budget *fleetRenewalBudgetError
		if !o.Apply && errors.As(err, &budget) {
			return printResult(o.Format, budget, err)
		}
		return err
	}
	if !o.Apply {
		return printResult(o.Format, plan, nil)
	}
	if err := requireApproved(true, o.PlanHash, plan.PlanHash); err != nil {
		return err
	}
	current, err := loadPersistedPlan(cfg, stateDir)
	if err != nil {
		return err
	}
	base := current
	if current.PlanHash == plan.PlanHash {
		base, err = readValidatorEvidenceHistoricalPlan(stateDir, plan.FleetRenewals[len(plan.FleetRenewals)-1].SourcePlanHash)
		if err != nil {
			return err
		}
	}
	roles, err := loadExistingProvisionalRoles(cfg, stateDir)
	if err != nil {
		return err
	}
	renewal := plan.FleetRenewals[len(plan.FleetRenewals)-1]
	if err := validateFleetRenewalCustody(cfg, stateDir, roles, renewal); err != nil {
		return err
	}
	journal, err := OpenJournal(stateDir)
	if err != nil {
		return err
	}
	defer journal.Close()
	if err := validateFleetRenewalSource(base, plan, journal.Entries()); err != nil {
		return err
	}
	if err := validateFleetLifecycleRenewalPending(stateDir, base, journal.Entries()); err != nil {
		return err
	}
	// New renewal transactions are included in the fresh liability sum but
	// already have their own fixed action ceilings; compare the pre-approval
	// prefix to avoid charging the same transaction twice during resume.
	prefix := []JournalEntry{}
	for _, entry := range journal.Entries() {
		prefix = append(prefix, entry)
		if entry.EntryHash == renewal.JournalHash {
			break
		}
	}
	exposure, err := fleetRenewalCampaignExposure(stateDir, base, prefix, renewal.TransactionEvidence)
	if err != nil {
		return err
	}
	if exposure.Liability != renewal.CampaignLiabilityWei || exposure.SupersededCredit != renewal.SupersededGasCoveredWei {
		return errors.New("renewal campaign liability differs from the reviewed signed transaction set")
	}
	if err := validateFleetRenewalNonceCoverage(roles, exposure, renewal.EVMNonces); err != nil {
		return err
	}
	actions, err := fleetRenewalActions(plan, renewal)
	if err != nil {
		return err
	}
	remaining := Spend{}
	for _, a := range actions {
		if !exactVerifiedPlanAction(plan, journal.Entries(), a.ID) {
			remaining, err = addSpends(remaining, a.Spend)
			if err != nil {
				return err
			}
		}
	}
	if report := runDoctor(ctx, cfg, &doctorPlanBudget{Plan: plan, Remaining: remaining, StateDir: stateDir}); report.Error() != nil {
		return fmt.Errorf("renewal doctor must pass before apply: %w", report.Error())
	}
	executor, err := NewExecutor(ctx, cfg, stateDir, plan, journal, roles)
	if err != nil {
		return err
	}
	defer executor.Close()
	if err := executor.verifyFleetRenewalLiveBudget(ctx, renewal); err != nil {
		return err
	}
	if err := executor.verifyFleetRenewalPredecessors(ctx, renewal, base); err != nil {
		return err
	}
	if current.PlanHash != plan.PlanHash {
		fresh, err := observeFleetRenewal(ctx, cfg, stateDir, base, roles, journal.Entries(), cliOptions{RenewalValidFrom: renewal.ValidFromEpoch, RenewalValidTo: renewal.ValidToEpoch, RenewalFeePerGas: renewal.MaximumFeePerGasWei, RenewalTransactions: renewal.TransactionEvidence})
		if err != nil {
			return err
		}
		if err := validateFleetRenewalFreshPrestate(renewal, fresh); err != nil {
			return err
		}
		if err := executor.verifyFleetRenewalSignerBalances(ctx, actions); err != nil {
			return err
		}
		if err := writeRunInputs(cfg, stateDir, plan, roles); err != nil {
			return err
		}
	}
	if err := executor.executeFleetRenewalPipeline(ctx, actions); err != nil {
		return err
	}
	return printResult(o.Format, map[string]any{"command": "fleet-renew", "plan_hash": plan.PlanHash, "renewal_round": renewal.Round, "fleets": len(renewal.Fleets), "valid_from_epoch": renewal.ValidFromEpoch, "valid_to_epoch": renewal.ValidToEpoch, "status": "postcondition_verified"}, nil)
}

// All fleets must still match the approved snapshot before the first write.
// Resume reuses the exact persisted transaction envelopes for each action.
func validateFleetRenewalFreshPrestate(renewal FleetRenewal, fresh fleetRenewalObservation) error {
	if renewal.CampaignLiabilityWei != fresh.Renewal.CampaignLiabilityWei || renewal.SupersededGasCoveredWei != fresh.Renewal.SupersededGasCoveredWei || !equalFleetRenewalTransactions(renewal.TransactionEvidence, fresh.Renewal.TransactionEvidence) {
		return errors.New("renewal signed transaction liabilities changed since approval")
	}
	if left, _ := canonicalHashHex(renewal.EVMNonces); left != "" {
		right, _ := canonicalHashHex(fresh.Renewal.EVMNonces)
		if left != right {
			return errors.New("renewal deployment EVM nonce activity changed since approval")
		}
	}
	if fresh.Renewal.ObservedEpoch >= renewal.ValidFromEpoch || fresh.Renewal.Oracle != renewal.Oracle || fresh.Renewal.Keeper != renewal.Keeper || fresh.Renewal.OracleNonce != renewal.OracleNonce || fresh.Renewal.KeeperNonce != renewal.KeeperNonce {
		return errors.New("renewal signer nonce, oracle, or inclusion window changed since approval")
	}
	for _, fleet := range renewal.Fleets {
		manifest, err := protocol.ParseFleetManifest(fleet.Manifest)
		if err != nil {
			return err
		}
		native, ok := fresh.Native[manifest.Hotkey]
		account, accountOK := fresh.Accounts[manifest.Hotkey]
		if !ok || !accountOK || native.UID != fleet.UID || native.Coldkey != fleet.Coldkey || uint32(account.Nonce) != fleet.NativeNonce {
			return fmt.Errorf("renewal fleet %d custody or nonce changed since approval", fleet.Fleet)
		}
		for index, member := range fleet.Members {
			priorManifest := *manifest
			priorManifest.Generation = member.Prior.Generation
			binding, err := fleetRenewalBinding(priorManifest, manifest.Members[index], member.Prior)
			if err != nil {
				return err
			}
			read, ok := fresh.Records[binding.ClientID]
			if !ok || read.Count == nil || !read.Count.IsUint64() || read.Count.Uint64() != member.VersionCount || !fleetBindingRecordMatches(read.Record, binding, member.Prior.ValidToEpoch, fleet.UID) {
				return fmt.Errorf("renewal fleet %d predecessor changed since approval", fleet.Fleet)
			}
		}
	}
	return nil
}

func (e *Executor) verifyFleetRenewalSignerBalances(ctx context.Context, actions []Action) error {
	totals := map[common.Address]*big.Int{}
	for _, action := range actions {
		if action.Kind != "evm-transaction" {
			continue
		}
		address := common.HexToAddress(action.Parameters["renewal_expected_signer"])
		amount, ok := new(big.Int).SetString(string(action.Spend.EVMGasWei), 10)
		if !ok {
			return errors.New("renewal gas amount is malformed")
		}
		if totals[address] == nil {
			totals[address] = new(big.Int)
		}
		totals[address].Add(totals[address], amount)
	}
	for address, maximum := range totals {
		balance, err := e.keeper.client.BalanceAt(ctx, address, nil)
		if err != nil {
			return err
		}
		if balance.Cmp(maximum) < 0 {
			return fmt.Errorf("renewal signer %s existing balance %s is below exact maximum %s; no funding action is authorized", address, balance, maximum)
		}
	}
	return nil
}

func (e *Executor) fleetRenewalCoordinates(action Action) (*FleetRenewal, *FleetRenewalFleet, int, error) {
	if !isFleetRenewalAction(action) || e.plan == nil {
		return nil, nil, 0, errors.New("renewal action is unavailable")
	}
	round, err := strconv.ParseUint(action.Parameters["round"], 10, 64)
	if err != nil || round == 0 || round > uint64(len(e.plan.FleetRenewals)) {
		return nil, nil, 0, errors.New("renewal round is invalid")
	}
	renewal := &e.plan.FleetRenewals[round-1]
	fleet, err := strconv.Atoi(action.Parameters["fleet"])
	if err != nil || fleet < 1 || fleet > len(renewal.Fleets) {
		return nil, nil, 0, errors.New("renewal fleet is invalid")
	}
	member, err := strconv.Atoi(action.Parameters["member"])
	if err != nil || member < 0 || member > len(renewal.Fleets[fleet-1].Members) {
		return nil, nil, 0, errors.New("renewal member is invalid")
	}
	if action.ID != fleetRenewalActionID(round, fleet, action.Parameters["operation"], member) {
		return nil, nil, 0, errors.New("renewal action coordinates differ")
	}
	return renewal, &renewal.Fleets[fleet-1], member, nil
}

func (e *Executor) fleetRenewalFinalized(action Action) (JournalEntry, error) {
	var result JournalEntry
	for _, entry := range e.journal.Entries() {
		if entry.Stage != StageFinalized || entry.ActionID != action.ID || entry.IntentHash != action.IntentHash || !e.plan.allowedPlanHashes()[entry.PlanHash] {
			continue
		}
		if result.TransactionHash != "" && result.TransactionHash != entry.TransactionHash {
			return result, errors.New("renewal action has multiple finalized transactions")
		}
		result = entry
	}
	if result.TransactionHash == "" || result.BlockNumber == 0 || !validCanonicalHashHex(result.BlockHash) {
		return result, errors.New("renewal action has no exact finalized transaction")
	}
	return result, nil
}

// Re-authenticate every old receipt and signed binding at its historical block.
// Revocation and later renewal may change current state but never this proof.
func (e *Executor) verifyFleetRenewalPredecessors(ctx context.Context, renewal FleetRenewal, base *SetupPlan) error {
	head, err := finalizedEVMHead(ctx, e.keeper.client)
	if err != nil {
		return err
	}
	coordinator := stabi.NewSTCoordinator()
	receipts := map[string]*ethTypes.Receipt{}
	entries := e.journal.Entries()
	for _, fleet := range renewal.Fleets {
		for _, member := range fleet.Members {
			prior := member.Prior
			found := false
			for _, entry := range entries {
				if base.allowedPlanHashes()[entry.PlanHash] && entry.Stage == StageFinalized && entry.TransactionHash == prior.TransactionHash && entry.BlockNumber == prior.BlockNumber && entry.BlockHash == prior.BlockHash {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("fleet %d predecessor has no finalized approved journal lineage", fleet.Fleet)
			}
			receipt := receipts[prior.TransactionHash]
			if receipt == nil {
				receipt, err = verifyFinalizedEVMReceipt(ctx, e.keeper.client, head, prior.TransactionHash, prior.BlockNumber, prior.BlockHash)
				if err != nil {
					return err
				}
				receipts[prior.TransactionHash] = receipt
			}
			matched := 0
			for _, log := range receipt.Logs {
				if log == nil || log.Address != e.plan.Deployment.CoordinatorProxy {
					continue
				}
				event, err := coordinator.UnpackFleetBoundEvent(log)
				if err == nil && fleetLifecycleHex16(event.ClientId) == prior.ClientID && fleetLifecycleHex(event.FleetId) == prior.FleetID && fleetLifecycleHex(event.Hotkey) == prior.Hotkey && event.Uid == prior.UID && event.Generation == prior.Generation && event.ValidFromEpoch == prior.ValidFromEpoch && event.ValidToEpoch == prior.ValidToEpoch {
					matched++
				}
			}
			if matched != 1 {
				return errors.New("renewal predecessor receipt lacks exactly one matching FleetBound event")
			}
		}
	}
	return nil
}

func writeFleetRenewalImmutable(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeFleetRenewalImmutableBytes(path, raw)
}
func writeFleetRenewalImmutableBytes(path string, raw []byte) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("renewal immutable evidence is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	prior, err := os.ReadFile(path)
	if err == nil {
		if !bytes.Equal(prior, raw) {
			return fmt.Errorf("renewal refuses to overwrite immutable evidence %s", path)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return atomicWrite(path, raw, 0o644)
}

func (e *Executor) fleetRenewalCommitment(ctx context.Context, renewal *FleetRenewal, fleet *FleetRenewalFleet) (*FleetCommitmentEvidence, error) {
	manifest, err := protocol.ParseFleetManifest(fleet.Manifest)
	if err != nil {
		return nil, err
	}
	commitment, err := manifest.CommitmentHash()
	if err != nil {
		return nil, err
	}
	action, err := e.planAction(fleetRenewalActionID(renewal.Round, fleet.Fleet, "commitment", 0))
	if err != nil {
		return nil, err
	}
	transaction, err := e.fleetRenewalFinalized(action)
	if err != nil {
		return nil, err
	}
	var evidence FleetCommitmentEvidence
	stem := fleetRenewalStem(renewal.Round, fleet.Fleet)
	if err := readJSONFile(filepath.Join(e.stateDir, "public", stem+".commitment.json"), &evidence); err != nil {
		return nil, err
	}
	if evidence.Schema != fleetCommitmentEvidenceSchemaV2 || evidence.DeploymentID != e.plan.DeploymentID || evidence.PlanHash != transaction.PlanHash || evidence.ActionID != action.ID || evidence.IntentHash != action.IntentHash || evidence.ManifestURI != stem+".json" || evidence.CommitmentHash != fleetLifecycleHex(commitment) || evidence.Hotkey != fleetLifecycleHex(manifest.Hotkey) || evidence.ExtrinsicHash != transaction.TransactionHash || evidence.FinalizedBlock != transaction.BlockNumber || evidence.CommitmentBlock != transaction.BlockNumber || evidence.FinalizedBlockHash != transaction.BlockHash {
		return nil, errors.New("renewal commitment evidence differs from exact finalized action")
	}
	blockHash, err := nativeTypes.NewHashFromHexString(transaction.BlockHash)
	if err != nil {
		return nil, err
	}
	canonical, err := e.substrate.chain.API.RPC.Chain.GetBlockHash(transaction.BlockNumber)
	if err != nil || canonical != blockHash {
		return nil, stateMismatchError(err, "renewal native block is not canonical")
	}
	txHash, err := nativeTypes.NewHashFromHexString(transaction.TransactionHash)
	if err != nil {
		return nil, err
	}
	if err := verifyReleaseHistoryFinalizedExtrinsicContext(ctx, e.substrate.chain, e.cfg, blockHash, txHash); err != nil {
		return nil, err
	}
	observed, err := e.substrate.fleetCommitmentAt(manifest.Hotkey, blockHash)
	if err != nil {
		return nil, err
	}
	if err := crv4.ValidateFleetCommitmentWrite(commitment, transaction.BlockNumber, observed); err != nil {
		return nil, err
	}
	return &evidence, nil
}

func (e *Executor) verifyFleetRenewalLiveIdentity(ctx context.Context, renewal *FleetRenewal, fleet *FleetRenewalFleet, head ChainHead) error {
	manifest, err := protocol.ParseFleetManifest(fleet.Manifest)
	if err != nil {
		return err
	}
	uid, found, err := e.substrate.UID(manifest.Hotkey)
	if err != nil || !found || uid != fleet.UID {
		return stateMismatchError(err, "renewal fleet %d live UID changed", fleet.Fleet)
	}
	owner, err := e.substrate.HotkeyOwner(manifest.Hotkey)
	if err != nil || fleetLifecycleHex(owner) != fleet.Coldkey {
		return stateMismatchError(err, "renewal fleet %d coldkey custody changed", fleet.Fleet)
	}
	state, err := readFleetRefreshOracleStateAt(ctx, e.oracle, e.plan.Deployment.CoordinatorProxy, stabi.NewSTCoordinator(), head.Number)
	if err != nil {
		return err
	}
	if state.Active != renewal.Oracle || state.Immutable != renewal.Oracle || state.Pending != (common.Address{}) || state.PendingEpoch != 0 {
		return errors.New("renewal oracle routing changed")
	}
	if state.CurrentEpoch >= renewal.ValidFromEpoch {
		return fmt.Errorf("renewal inclusion window closed at epoch %d; exact pending transactions remain recoverable", renewal.ValidFromEpoch)
	}
	return nil
}

func (e *Executor) executeFleetRenewalAction(ctx context.Context, action Action) error {
	return e.executeFleetRenewalActionWithSender(ctx, action, nil)
}

type fleetRenewalSender func(context.Context, *EvmTxManager, string, Action, *common.Address, *big.Int, []byte) (*ethTypes.Receipt, error)

func (e *Executor) executeFleetRenewalActionWithSender(ctx context.Context, action Action, send fleetRenewalSender) error {
	renewal, fleet, member, err := e.fleetRenewalCoordinates(action)
	if err != nil {
		return err
	}
	manifest, err := protocol.ParseFleetManifest(fleet.Manifest)
	if err != nil {
		return err
	}
	head, err := finalizedEVMHead(ctx, e.keeper.client)
	if err != nil {
		return err
	}
	_, persisted := e.journal.LatestTransaction(e.plan.PlanHash, action.ID, action.IntentHash)
	if !persisted {
		if err := e.verifyFleetRenewalLiveIdentity(ctx, renewal, fleet, head); err != nil {
			return err
		}
	}
	stem := fleetRenewalStem(renewal.Round, fleet.Fleet)
	if action.Parameters["operation"] == "commitment" {
		canonical, err := manifest.Canonical()
		if err != nil {
			return err
		}
		if err := writeFleetRenewalImmutableBytes(filepath.Join(e.stateDir, "public", stem+".json"), append(canonical, '\n')); err != nil {
			return err
		}
		commitment, err := manifest.CommitmentHash()
		if err != nil {
			return err
		}
		call, err := e.substrate.chain.NewSetFleetCommitmentCall(e.cfg.Netuid, commitment)
		if err != nil {
			return err
		}
		signer, err := e.substrate.RoleSigner(e.roles, fleet.HotkeyRole)
		if err != nil {
			return err
		}
		tx, block, err := e.substrate.SendAsWithRecoveryPrecondition(ctx, e.plan.PlanHash, action, call, signer, func(hash nativeTypes.Hash, number uint64) error {
			balance, err := e.substrate.freeBalanceAtHash(manifest.Hotkey, hash)
			if err != nil {
				return err
			}
			minimum, ok := checkedAdd(e.plan.NativeTransactionFeeLimitRao, e.plan.LiveFacts.ExistentialDepositRao)
			if !ok || balance < minimum {
				return errors.New("renewal hotkey lacks its existing fee and keep-alive funds")
			}
			return nil
		})
		if err != nil {
			return err
		}
		blockHash, err := e.substrate.chain.API.RPC.Chain.GetBlockHash(block)
		if err != nil {
			return err
		}
		evidence := FleetCommitmentEvidence{Schema: fleetCommitmentEvidenceSchemaV2, DeploymentID: e.plan.DeploymentID, PlanHash: e.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, ManifestURI: stem + ".json", CommitmentHash: fleetLifecycleHex(commitment), Hotkey: fleetLifecycleHex(manifest.Hotkey), ExtrinsicHash: tx.Hex(), CommitmentBlock: block, FinalizedBlock: block, FinalizedBlockHash: blockHash.Hex()}
		return writeFleetRenewalImmutable(filepath.Join(e.stateDir, "public", stem+".commitment.json"), evidence)
	}
	commitment, err := e.fleetRenewalCommitment(ctx, renewal, fleet)
	if err != nil {
		return err
	}
	coordinator := stabi.NewSTCoordinator()
	address := e.plan.Deployment.CoordinatorProxy
	manager := e.keeper
	var data []byte
	if action.Parameters["operation"] == "mirror" {
		manager = e.oracle
		hash, _ := decodeHex32("commitment", commitment.CommitmentHash)
		blockHash, _ := decodeHex32("native finality", commitment.FinalizedBlockHash)
		data, err = coordinator.TryPackMirrorCommitment(manifest.Hotkey, hash, commitment.FinalizedBlock, blockHash)
	} else {
		data, err = hex.DecodeString(stringsTrim0x(action.Parameters["renewal_calldata"]))
	}
	if err != nil {
		return err
	}
	if !persisted {
		latest, err := manager.client.BlockNumber(ctx)
		if err != nil {
			return err
		}
		if err := validateFleetCommitmentInclusionLifetime(e.cfg, action, commitment, latest); err != nil {
			return err
		}
		if member != 0 {
			prior := fleet.Members[member-1]
			client := manifest.Members[member-1].ClientID
			reads, err := readFleetRenewalLatestAt(ctx, e.keeper, address, [][16]byte{client}, head.Number)
			if err != nil {
				return err
			}
			read := reads[client]
			priorManifest := *manifest
			priorManifest.Generation = prior.Prior.Generation
			binding, err := fleetRenewalBinding(priorManifest, manifest.Members[member-1], prior.Prior)
			if err != nil {
				return err
			}
			validTo := prior.Prior.ValidToEpoch
			if action.Parameters["operation"] == "bind" && prior.RevokeSignature != "" {
				revokeAction, err := e.planAction(fleetRenewalActionID(renewal.Round, fleet.Fleet, "revoke", member))
				if err != nil {
					return err
				}
				if _, err := e.fleetRenewalFinalized(revokeAction); err != nil {
					return err
				}
				validTo = renewal.ValidFromEpoch - 1
			}
			if read.Count.Uint64() != prior.VersionCount || !fleetBindingRecordMatches(read.Record, binding, validTo, fleet.UID) {
				return errors.New("renewal predecessor changed before the exact mutation")
			}
		}
	}
	var receipt *ethTypes.Receipt
	if send == nil {
		receipt, err = manager.Send(ctx, e.plan.PlanHash, action, &address, new(big.Int), data)
	} else {
		receipt, err = send(ctx, manager, e.plan.PlanHash, action, &address, new(big.Int), data)
	}
	if err != nil {
		return err
	}
	if receipt == nil {
		return nil
	}
	return e.persistFleetRenewalBindingReceipt(action, receipt)
}

func (e *Executor) persistFleetRenewalBindingReceipt(action Action, receipt *ethTypes.Receipt) error {
	renewal, fleet, member, err := e.fleetRenewalCoordinates(action)
	if err != nil {
		return err
	}
	if action.Parameters["operation"] == "bind" {
		if receipt == nil || receipt.BlockNumber == nil {
			return errors.New("renewal binding receipt is unavailable")
		}
		stem := fleetRenewalStem(renewal.Round, fleet.Fleet)
		evidence := fleet.Members[member-1].Binding
		evidence.DeploymentID = e.plan.DeploymentID
		evidence.PlanHash = e.plan.PlanHash
		evidence.ActionID = action.ID
		evidence.IntentHash = action.IntentHash
		evidence.TransactionHash = receipt.TxHash.Hex()
		evidence.BlockNumber = receipt.BlockNumber.Uint64()
		evidence.BlockHash = receipt.BlockHash.Hex()
		return writeFleetRenewalImmutable(filepath.Join(e.stateDir, "public", fmt.Sprintf("%s-member-%d.binding.json", stem, member)), evidence)
	}
	return nil
}

func (e *Executor) verifyFleetRenewalPostState(ctx context.Context, action Action, head ChainHead, state map[string]any) (map[string]any, error) {
	renewal, fleet, member, err := e.fleetRenewalCoordinates(action)
	if err != nil {
		return nil, err
	}
	commitment, err := e.fleetRenewalCommitment(ctx, renewal, fleet)
	if err != nil {
		return nil, err
	}
	transaction, err := e.fleetRenewalFinalized(action)
	if err != nil {
		return nil, err
	}
	state["transaction_hash"], state["finalized_block"], state["finalized_block_hash"] = transaction.TransactionHash, transaction.BlockNumber, transaction.BlockHash
	state["renewal_round"], state["fleet"], state["generation"] = renewal.Round, fleet.Fleet, action.Parameters["generation"]
	if action.Parameters["operation"] == "commitment" {
		state["commitment_hash"] = commitment.CommitmentHash
		return state, nil
	}
	manager := e.keeper
	if action.Parameters["operation"] == "mirror" {
		manager = e.oracle
	}
	receipt, err := verifyFinalizedEVMReceipt(ctx, manager.client, head, transaction.TransactionHash, transaction.BlockNumber, transaction.BlockHash)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(e.stateDir, "transactions", stringsTrim0x(transaction.TransactionHash)+".rlp"))
	if err != nil {
		return nil, err
	}
	var tx ethTypes.Transaction
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, err
	}
	signer, err := ethTypes.Sender(ethTypes.LatestSignerForChainID(new(big.Int).SetUint64(e.plan.ChainID)), &tx)
	if err != nil {
		return nil, err
	}
	if !tx.Protected() || tx.ChainId().Uint64() != e.plan.ChainID || tx.Hash().Hex() != transaction.TransactionHash {
		return nil, errors.New("renewal finalized raw transaction differs from journal")
	}
	if err := validateFleetRenewalEVMFields(action, signer, tx.Nonce(), tx.To(), tx.Value(), tx.Data()); err != nil {
		return nil, err
	}
	if err := validateFleetRenewalSignedTransaction(action, &tx, new(big.Int).SetUint64(e.plan.ChainID)); err != nil {
		return nil, err
	}
	manifest, err := protocol.ParseFleetManifest(fleet.Manifest)
	if err != nil {
		return nil, err
	}
	coordinator := stabi.NewSTCoordinator()
	address := e.plan.Deployment.CoordinatorProxy
	if action.Parameters["operation"] == "mirror" {
		hash, _ := decodeHex32("commitment", commitment.CommitmentHash)
		blockHash, _ := decodeHex32("finalized hash", commitment.FinalizedBlockHash)
		want, err := coordinator.TryPackMirrorCommitment(manifest.Hotkey, hash, commitment.FinalizedBlock, blockHash)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(tx.Data(), want) {
			return nil, errors.New("renewal mirror raw transaction changed exact native attestation")
		}
		mirrored, err := rawCoordinatorCallAt(ctx, manager, address, coordinator.PackMirroredCommitments(manifest.Hotkey), coordinator.UnpackMirroredCommitments, transaction.BlockNumber)
		if err != nil || !fleetMirrorMatches(mirrored, hash, commitment.FinalizedBlock, blockHash) {
			return nil, stateMismatchError(err, "renewal mirror postcondition differs")
		}
		state["commitment_hash"] = commitment.CommitmentHash
	} else {
		planned := fleet.Members[member-1]
		manifestMember := manifest.Members[member-1]
		version := planned.VersionCount
		binding, err := fleetRenewalBinding(*manifest, manifestMember, planned.Binding)
		if err != nil {
			return nil, err
		}
		validTo := renewal.ValidToEpoch
		if action.Parameters["operation"] == "revoke" {
			version--
			oldManifest := *manifest
			oldManifest.Generation = planned.Prior.Generation
			binding, err = fleetRenewalBinding(oldManifest, manifestMember, planned.Prior)
			if err != nil {
				return nil, err
			}
			validTo = renewal.ValidFromEpoch - 1
		}
		count, record, err := readFleetBindingVersionAt(ctx, manager, address, coordinator, manifestMember.ClientID, version, transaction.BlockNumber)
		if err != nil || count == nil || !count.IsUint64() || count.Uint64() != version+1 || !fleetBindingRecordMatches(record, binding, validTo, fleet.UID) {
			return nil, stateMismatchError(err, "renewal member exact inclusion postcondition differs")
		}
		if action.Parameters["operation"] == "bind" {
			matched := 0
			for _, log := range receipt.Logs {
				if log == nil || log.Address != address {
					continue
				}
				event, err := coordinator.UnpackFleetBoundEvent(log)
				if err == nil && event.ClientId == binding.ClientID && event.FleetId == binding.FleetID && event.Hotkey == binding.Hotkey && event.Uid == fleet.UID && event.Generation == binding.Generation && event.ValidFromEpoch == binding.ValidFromEpoch && event.ValidToEpoch == binding.ValidToEpoch {
					matched++
				}
			}
			if matched != 1 {
				return nil, errors.New("renewal bind receipt lacks its exact FleetBound event")
			}
		}
		state["client_id"], state["version_count"], state["uid"] = fleetLifecycleHex16(manifestMember.ClientID), count.String(), record.Uid
		state["valid_from_epoch"], state["valid_to_epoch"] = record.ValidFromEpoch, record.ValidToEpoch
	}
	state["calldata_hash"] = fleetRenewalCalldataHash(tx.Data())
	return state, nil
}
