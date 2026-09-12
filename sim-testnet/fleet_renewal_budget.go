package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

type FleetRenewalNonce struct {
	Role      string         `json:"role"`
	Address   common.Address `json:"address"`
	Finalized uint64         `json:"finalized"`
	Latest    uint64         `json:"latest"`
	Pending   uint64         `json:"pending"`
}

type fleetRenewalExposure struct {
	Liability        DecimalUint
	SupersededCredit DecimalUint
	Transactions     map[common.Hash]*ethTypes.Transaction
	Nonces           map[common.Address]map[uint64]bool
}

func readFleetRenewalTransactionInput(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumCampaignEvidenceRawFileBytes {
		return nil, errors.New("renewal transaction evidence is not a bounded regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var transactions []string
	if err := json.Unmarshal(raw, &transactions); err != nil {
		return nil, err
	}
	return transactions, nil
}

// Daemon queues remain untouched. Signed attempts are liabilities regardless
// of their mutable status labels; a replaced transaction is still retained.
func readFleetRenewalQueueTransactions(cfg *ResolvedConfig, stateDir string) ([]string, error) {
	var transactions []string
	for miner := 1; miner <= cfg.Config.Topology.Miners; miner++ {
		relative := fmt.Sprintf("runtime/miner-%d/claims/claim-queue.json", miner)
		_, err := os.Lstat(filepath.Join(stateDir, filepath.FromSlash(relative)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		raw, err := readValidatorEvidenceHistoricalFile(stateDir, relative, maximumCampaignEvidenceRawFileBytes)
		if err != nil {
			return nil, err
		}
		var queue struct {
			Schema  string `json:"schema"`
			Entries map[string]struct {
				Hash string `json:"tx_hash"`
				Raw  string `json:"raw_tx_hex"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(raw, &queue); err != nil {
			return nil, err
		}
		if queue.Schema != "urnetwork-provider-claim-queue-v1" {
			return nil, fmt.Errorf("renewal claim queue %d has an unknown schema", miner)
		}
		for _, entry := range queue.Entries {
			if entry.Raw == "" && entry.Hash == "" {
				continue
			}
			decoded, err := hex.DecodeString(stringsTrim0x(entry.Raw))
			if err != nil {
				return nil, err
			}
			var tx ethTypes.Transaction
			if err := tx.UnmarshalBinary(decoded); err != nil {
				return nil, fmt.Errorf("renewal claim queue %d has incomplete signed bytes: %w", miner, err)
			}
			if tx.Hash().Hex() != entry.Hash {
				return nil, errors.New("renewal queue signed bytes differ from its transaction hash")
			}
			transactions = append(transactions, entry.Raw)
		}
	}
	return transactions, nil
}

func canonicalFleetRenewalTransactions(inputs []string) ([]string, error) {
	if len(inputs) > 20000 {
		return nil, errors.New("renewal external transaction count exceeds the deployment bound")
	}
	byHash := map[string]string{}
	var total int
	for _, input := range inputs {
		total += len(input)
		if total > maximumCampaignEvidenceRawFileBytes {
			return nil, errors.New("renewal external transaction bytes exceed the deployment bound")
		}
		raw, err := hex.DecodeString(stringsTrim0x(input))
		if err != nil {
			return nil, err
		}
		var tx ethTypes.Transaction
		if err := tx.UnmarshalBinary(raw); err != nil {
			return nil, err
		}
		canonical, err := tx.MarshalBinary()
		if err != nil {
			return nil, err
		}
		byHash[tx.Hash().Hex()] = "0x" + hex.EncodeToString(canonical)
	}
	keys := make([]string, 0, len(byHash))
	for hash := range byHash {
		keys = append(keys, hash)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, hash := range keys {
		result = append(result, byHash[hash])
	}
	return result, nil
}

// The original action ceilings cover one exact transaction per intent. All
// additional signed attempts, including cancellations and out-of-harness
// recovery, retain their complete gas/value ceiling in the campaign reserve.
func fleetRenewalCampaignExposure(stateDir string, base *SetupPlan, entries []JournalEntry, external []string) (fleetRenewalExposure, error) {
	result := fleetRenewalExposure{Transactions: map[common.Hash]*ethTypes.Transaction{}, Nonces: map[common.Address]map[uint64]bool{}}
	planned := map[string]string{}
	for _, a := range base.Actions {
		if a.Kind == "evm-transaction" {
			planned[a.ID+"\x00"+a.IntentHash] = a.ID
			for _, hash := range a.AcceptedPriorIntentHashes {
				planned[a.ID+"\x00"+hash] = a.ID
			}
		}
	}
	covered := map[string]bool{}
	total := new(big.Int)
	retired := new(big.Int)
	retiredIntents := map[string]bool{}
	history := map[string]*SetupPlan{}
	chain := new(big.Int).SetUint64(base.ChainID)
	add := func(raw []byte, entry *JournalEntry) error {
		var tx ethTypes.Transaction
		if err := tx.UnmarshalBinary(raw); err != nil {
			return err
		}
		if !tx.Protected() || tx.ChainId().Cmp(chain) != 0 {
			return errors.New("renewal liability transaction is unprotected or belongs to another chain")
		}
		sender, err := ethTypes.Sender(ethTypes.LatestSignerForChainID(chain), &tx)
		if err != nil {
			return err
		}
		if entry != nil && (tx.Hash().Hex() != entry.TransactionHash || sender != common.HexToAddress(entry.Signer) || strconv.FormatUint(tx.Nonce(), 10) != entry.Nonce) {
			return errors.New("renewal liability signed transaction differs from journal")
		}
		if _, seen := result.Transactions[tx.Hash()]; seen {
			return nil
		}
		result.Transactions[tx.Hash()] = &tx
		if result.Nonces[sender] == nil {
			result.Nonces[sender] = map[uint64]bool{}
		}
		result.Nonces[sender][tx.Nonce()] = true
		if entry != nil {
			key := planned[entry.ActionID+"\x00"+entry.IntentHash]
			if key != "" && !covered[key] && base.allowedPlanHashes()[entry.PlanHash] {
				covered[key] = true
				return nil
			}
		}
		maximum := new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), tx.GasFeeCap())
		maximum.Add(maximum, tx.Value())
		total.Add(total, maximum)
		if entry != nil && tx.Value().Sign() == 0 && entry.PlanHash != base.PlanHash && base.allowedPlanHashes()[entry.PlanHash] {
			key := entry.ActionID + "\x00" + entry.IntentHash
			if !retiredIntents[key] && fleetRenewalVerifiedTransaction(entries, *entry) {
				source := history[entry.PlanHash]
				if source == nil {
					var err error
					source, err = readValidatorEvidenceHistoricalPlan(stateDir, entry.PlanHash)
					if err != nil {
						return err
					}
					history[entry.PlanHash] = source
				}
				action, err := exactPlanActionByID(source, entry.ActionID)
				if err != nil {
					// Bounded runtime repairs may be journaled under the source
					// identity without being an original setup action. Their
					// complete envelope remains charged to campaign reserve.
					return nil
				}
				ceiling, ok := new(big.Int).SetString(string(action.Spend.EVMGasWei), 10)
				if action.Kind != "evm-transaction" || action.IntentHash != entry.IntentHash || !ok || maximum.Cmp(ceiling) > 0 {
					return errors.New("renewal retired gas differs from its exact ancestor action ceiling")
				}
				retired.Add(retired, maximum)
				retiredIntents[key] = true
			}
		}
		return nil
	}
	for _, entry := range entries {
		if entry.TransactionHash == "" || !common.IsHexAddress(entry.Signer) {
			continue
		}
		if !validCanonicalHashHex(entry.TransactionHash) {
			return result, errors.New("renewal liability transaction hash is malformed")
		}
		if _, seen := result.Transactions[common.HexToHash(entry.TransactionHash)]; seen {
			continue
		}
		raw, err := readValidatorEvidenceHistoricalFile(stateDir, "transactions/"+stringsTrim0x(entry.TransactionHash)+".rlp", maximumCampaignEvidenceRawFileBytes)
		if err != nil {
			return result, err
		}
		if err := add(raw, &entry); err != nil {
			return result, err
		}
	}
	for _, input := range external {
		raw, err := hex.DecodeString(stringsTrim0x(input))
		if err != nil {
			return result, err
		}
		if err := add(raw, nil); err != nil {
			return result, err
		}
	}
	reserved := new(big.Int)
	if !base.SupersededSpend.EVMGasWei.IsZero() {
		var ok bool
		reserved, ok = new(big.Int).SetString(string(base.SupersededSpend.EVMGasWei), 10)
		if !ok || reserved.Sign() < 0 {
			return result, errors.New("renewal superseded gas allowance is malformed")
		}
	}
	if retired.Cmp(reserved) > 0 {
		retired.Set(reserved)
	}
	total.Sub(total, retired)
	result.Liability = DecimalUint(total.String())
	result.SupersededCredit = DecimalUint(retired.String())
	return result, nil
}

// A retired ceiling covers the exact successfully finalized transaction whose
// postcondition was verified, never a failed or still pending sibling attempt.
func fleetRenewalVerifiedTransaction(entries []JournalEntry, transaction JournalEntry) bool {
	latestHash := ""
	for _, entry := range entries {
		if entry.PlanHash != transaction.PlanHash || entry.ActionID != transaction.ActionID || entry.IntentHash != transaction.IntentHash {
			continue
		}
		if entry.Stage == StageFinalized {
			latestHash = entry.TransactionHash
		}
		if entry.Stage == StageVerified && latestHash == transaction.TransactionHash {
			return true
		}
	}
	return false
}

func validateFleetRenewalNonceCoverage(roles *RoleSecrets, exposure fleetRenewalExposure, checkpoints []FleetRenewalNonce) error {
	if len(checkpoints) != len(roles.EVM) {
		return errors.New("renewal gas accounting omits an existing EVM role")
	}
	seen := map[string]bool{}
	addresses := map[common.Address]bool{}
	for _, point := range checkpoints {
		role, ok := roles.EVM[point.Role]
		if !ok || seen[point.Role] || point.Address != common.HexToAddress(role.Address) || addresses[point.Address] || point.Finalized > point.Latest || point.Latest > point.Pending || point.Pending > 20000 {
			return errors.New("renewal EVM nonce checkpoint changes custody or exceeds its bound")
		}
		seen[point.Role], addresses[point.Address] = true, true
		for nonce := uint64(0); nonce < point.Pending; nonce++ {
			if !exposure.Nonces[point.Address][nonce] {
				return fmt.Errorf("renewal gas accounting is incomplete: role %s nonce %d has no retained signed transaction", point.Role, nonce)
			}
		}
	}
	for address := range exposure.Nonces {
		if !addresses[address] {
			return fmt.Errorf("renewal transaction evidence contains non-deployment signer %s", address)
		}
	}
	return nil
}

func observeFleetRenewalNonces(ctx context.Context, manager *EvmTxManager, roles *RoleSecrets, block uint64) ([]FleetRenewalNonce, error) {
	labels := make([]string, 0, len(roles.EVM))
	for label := range roles.EVM {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	points := make([]FleetRenewalNonce, 0, len(labels))
	for _, label := range labels {
		point := FleetRenewalNonce{Role: label, Address: common.HexToAddress(roles.EVM[label].Address)}
		var err error
		point.Finalized, err = manager.client.NonceAt(ctx, point.Address, new(big.Int).SetUint64(block))
		if err != nil {
			return nil, err
		}
		point.Latest, err = manager.client.NonceAt(ctx, point.Address, nil)
		if err != nil {
			return nil, err
		}
		point.Pending, err = manager.client.PendingNonceAt(ctx, point.Address)
		if err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, nil
}

func equalFleetRenewalTransactions(left, right []string) bool {
	return strings.Join(left, "\n") == strings.Join(right, "\n")
}

func (e *Executor) verifyFleetRenewalLiveBudget(ctx context.Context, renewal FleetRenewal) error {
	queued, err := readFleetRenewalQueueTransactions(e.cfg, e.stateDir)
	if err != nil {
		return err
	}
	inputs := append(append([]string(nil), renewal.TransactionEvidence...), queued...)
	inputs, err = canonicalFleetRenewalTransactions(inputs)
	if err != nil {
		return err
	}
	exposure, err := fleetRenewalCampaignExposure(e.stateDir, e.plan, e.journal.Entries(), inputs)
	if err != nil {
		return err
	}
	if exposure.Liability != renewal.CampaignLiabilityWei || exposure.SupersededCredit != renewal.SupersededGasCoveredWei {
		return errors.New("renewal resume found additional signed campaign liabilities; a new reviewed plan is required")
	}
	manager := e.oracle
	if manager == nil {
		return errors.New("renewal accounting oracle reader is unavailable")
	}
	head, err := finalizedEVMHead(ctx, manager.client)
	if err != nil {
		return err
	}
	points, err := observeFleetRenewalNonces(ctx, manager, e.roles, head.Number)
	if err != nil {
		return err
	}
	return validateFleetRenewalNonceCoverage(e.roles, exposure, points)
}
