package main

// This unit derives the complete release-emitter query from the reviewed
// plan lineage before the live capture starts. Current deployment addresses
// alone are insufficient: carried receipts can have emitted through a retired
// vault or reserve contract that is no longer part of the active graph.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// Separates the ordinary active emitter graph from the complete immutable
// lineage graph. The former is intentionally used for by-name projection;
// the latter is retained for exact transaction-based historical replay.
type finalReleaseContractCaptureCensus struct {
	fromBlock        uint64
	currentAddresses []string
	releaseAddresses []string
	queryAddresses   []common.Address
}

// Loads only predecessor plans explicitly named by the active approved plan,
// authenticates their canonical hashes, and then derives the capture range.
// Directory enumeration is deliberately forbidden: an unrelated local plan
// file must never broaden the trusted historical contract graph.
func finalCaptureReleaseContractCensusFromState(stateRoot string, current *SetupPlan, deployment *ContractDeployment, batcher common.Address) (finalReleaseContractCaptureCensus, error) {
	if stateRoot == "" || current == nil || deployment == nil {
		return finalReleaseContractCaptureCensus{}, errors.New("historical release capture state is incomplete")
	}
	if err := requireFinalHex32("current release plan hash", current.PlanHash); err != nil {
		return finalReleaseContractCaptureCensus{}, err
	}
	plans := map[string]*SetupPlan{strings.ToLower(current.PlanHash): current}
	for _, hash := range current.PriorPlanHashes {
		if err := requireFinalHex32("approved predecessor plan hash", hash); err != nil {
			return finalReleaseContractCaptureCensus{}, err
		}
		key := strings.ToLower(hash)
		if _, found := plans[key]; found {
			return finalReleaseContractCaptureCensus{}, fmt.Errorf("approved predecessor plan %s is duplicated", hash)
		}
		path := filepath.Join(stateRoot, "plans", stringsTrim0x(key)+".json")
		plan, err := readPersistedPlanFile(path)
		if err != nil || !strings.EqualFold(plan.PlanHash, key) || plan.DeploymentID != current.DeploymentID || plan.ChainID != current.ChainID || plan.Netuid != current.Netuid {
			return finalReleaseContractCaptureCensus{}, stateMismatchError(err, "load approved predecessor plan %s", hash)
		}
		plans[key] = plan
	}
	journalBytes, err := os.ReadFile(filepath.Join(stateRoot, "journal.jsonl"))
	if err != nil {
		return finalReleaseContractCaptureCensus{}, err
	}
	entries, err := decodeFinalSemanticJournalBytes(journalBytes)
	if err != nil {
		return finalReleaseContractCaptureCensus{}, err
	}
	return finalCaptureReleaseContractCensusForLineage(current, deployment, batcher, plans, entries)
}

// Computes a stable emitter/address range from an already authenticated plan
// lineage. Keeping this pure makes every omission, foreign address, and range
// boundary testable without a filesystem or a live chain dependency.
func finalCaptureReleaseContractCensusForLineage(current *SetupPlan, deployment *ContractDeployment, batcher common.Address, plans map[string]*SetupPlan, entries []JournalEntry) (finalReleaseContractCaptureCensus, error) {
	if current == nil || deployment == nil || batcher == (common.Address{}) || len(plans) == 0 {
		return finalReleaseContractCaptureCensus{}, errors.New("historical release capture lineage is incomplete")
	}
	if err := requireFinalHex32("current release plan hash", current.PlanHash); err != nil {
		return finalReleaseContractCaptureCensus{}, err
	}
	currentHash := strings.ToLower(current.PlanHash)
	if plans[currentHash] != current {
		return finalReleaseContractCaptureCensus{}, errors.New("historical release capture lacks the exact active plan")
	}
	allowed := make(map[string]bool, len(current.PriorPlanHashes)+1)
	allowed[currentHash] = true
	for _, hash := range current.PriorPlanHashes {
		if err := requireFinalHex32("approved predecessor plan hash", hash); err != nil {
			return finalReleaseContractCaptureCensus{}, err
		}
		key := strings.ToLower(hash)
		if allowed[key] || plans[key] == nil {
			return finalReleaseContractCaptureCensus{}, fmt.Errorf("historical release capture has missing or duplicate predecessor plan %s", hash)
		}
		allowed[key] = true
	}
	for hash, plan := range plans {
		if !allowed[hash] || plan == nil || !strings.EqualFold(hash, plan.PlanHash) || plan.DeploymentID != current.DeploymentID || plan.ChainID != current.ChainID || plan.Netuid != current.Netuid {
			return finalReleaseContractCaptureCensus{}, fmt.Errorf("historical release capture has foreign plan %s", hash)
		}
	}

	currentSet, err := finalReleaseContractAddressSet(deployment, batcher)
	if err != nil {
		return finalReleaseContractCaptureCensus{}, err
	}
	releaseSet := make(map[string]common.Address, len(currentSet)+3*len(plans))
	for key, address := range currentSet {
		releaseSet[key] = address
	}
	minimum := deployment.DeployBlock
	if minimum == 0 {
		return finalReleaseContractCaptureCensus{}, errors.New("current release deployment block is unavailable")
	}
	for _, plan := range plans {
		if err := finalAddReleaseDeploymentAddresses(releaseSet, plan.Deployment); err != nil {
			return finalReleaseContractCaptureCensus{}, err
		}
		if plan.Deployment.DeployBlock != 0 && plan.Deployment.DeployBlock < minimum {
			minimum = plan.Deployment.DeployBlock
		}
		for _, retired := range plan.SupersededDeployments {
			if err := finalAddReleaseDeploymentAddresses(releaseSet, retired); err != nil {
				return finalReleaseContractCaptureCensus{}, err
			}
			if retired.DeployBlock != 0 && retired.DeployBlock < minimum {
				minimum = retired.DeployBlock
			}
		}
		if historicalBatcher, found, batcherErr := finalHistoricalPlanFleetBatcher(plan); batcherErr != nil {
			return finalReleaseContractCaptureCensus{}, batcherErr
		} else if found {
			releaseSet[strings.ToLower(historicalBatcher.Hex())] = historicalBatcher
		}
	}
	for _, entry := range entries {
		if entry.Stage != StageFinalized || entry.BlockNumber == 0 || entry.DeploymentID != current.DeploymentID {
			continue
		}
		planHash := strings.ToLower(entry.PlanHash)
		plan := plans[planHash]
		if !allowed[planHash] || plan == nil {
			continue
		}
		action, err := exactPlanActionByID(plan, entry.ActionID)
		if err != nil || !actionAcceptsIntent(action, entry.IntentHash) {
			return finalReleaseContractCaptureCensus{}, stateMismatchError(err, "historical release journal action %s is not approved", entry.ActionID)
		}
		// The journal is deliberately cross-chain.  Only an EVM transaction
		// can widen the EVM log-capture floor; treating a finalized native
		// commitment or read as an EVM mutation makes a valid mixed-chain
		// release impossible to capture.
		if action.Kind != "evm-transaction" {
			continue
		}
		if entry.BlockNumber < minimum {
			minimum = entry.BlockNumber
		}
	}
	if minimum == 0 {
		return finalReleaseContractCaptureCensus{}, errors.New("historical release capture range is empty")
	}
	return finalCanonicalReleaseContractCaptureCensus(minimum, currentSet, releaseSet)
}

// Adds the event-emitting half of one deterministic deployment. A zero
// deployment is a legitimate pre-deployment plan placeholder, but a partial
// deployment is never a safe historical replay source.
func finalAddReleaseDeploymentAddresses(addresses map[string]common.Address, deployment ContractDeployment) error {
	values := []common.Address{deployment.CoordinatorProxy, deployment.SettlementVault, deployment.ReserveSink}
	nonzero := 0
	for _, value := range values {
		if value != (common.Address{}) {
			nonzero++
		}
	}
	if nonzero == 0 {
		return nil
	}
	if nonzero != len(values) {
		return errors.New("historical release deployment has a partial emitter graph")
	}
	for _, value := range values {
		addresses[strings.ToLower(value.Hex())] = value
	}
	return nil
}

// Extracts a predecessor batcher only when its exact reviewed action exists.
// Plans from before fleet refresh support are therefore valid without adding
// a guessed address to the live log query.
func finalHistoricalPlanFleetBatcher(plan *SetupPlan) (common.Address, bool, error) {
	if plan == nil {
		return common.Address{}, false, errors.New("historical release plan is unavailable")
	}
	count := 0
	for _, action := range plan.Actions {
		if action.ID == "fleet.refresh.deploy-batcher" {
			count++
		}
	}
	if count == 0 {
		return common.Address{}, false, nil
	}
	if count != 1 {
		return common.Address{}, false, errors.New("historical release plan duplicates fleet batcher action")
	}
	batcher, _, err := finalPlanFleetBatcher(plan)
	if err != nil {
		return common.Address{}, false, err
	}
	return batcher, true, nil
}

// Builds the active four-address set used by ordinary semantic projections.
// It is kept distinct from historical addresses so a recycled event signature
// cannot satisfy a current pool, epoch, or reserve assertion.
func finalReleaseContractAddressSet(deployment *ContractDeployment, batcher common.Address) (map[string]common.Address, error) {
	if deployment == nil || batcher == (common.Address{}) {
		return nil, errors.New("current release emitter graph is incomplete")
	}
	result := make(map[string]common.Address, 4)
	for _, address := range []common.Address{deployment.CoordinatorProxy, deployment.SettlementVault, deployment.ReserveSink, batcher} {
		if address == (common.Address{}) {
			return nil, errors.New("current release emitter graph has a zero address")
		}
		key := strings.ToLower(address.Hex())
		if result[key] != (common.Address{}) {
			return nil, errors.New("current release emitter graph has duplicate addresses")
		}
		result[key] = address
	}
	return result, nil
}

// Canonicalizes all query addresses and keeps their string and binary forms
// synchronized. RPC address ordering is deterministic, which makes the
// captured JSON projection stable across providers and retries.
func finalCanonicalReleaseContractCaptureCensus(fromBlock uint64, current map[string]common.Address, release map[string]common.Address) (finalReleaseContractCaptureCensus, error) {
	if fromBlock == 0 || len(current) != 4 || len(release) < len(current) {
		return finalReleaseContractCaptureCensus{}, errors.New("historical release capture census is incomplete")
	}
	currentStrings, err := finalCanonicalReleaseAddressStrings(current)
	if err != nil {
		return finalReleaseContractCaptureCensus{}, err
	}
	releaseStrings, err := finalCanonicalReleaseAddressStrings(release)
	if err != nil {
		return finalReleaseContractCaptureCensus{}, err
	}
	for _, address := range currentStrings {
		if _, found := release[address]; !found {
			return finalReleaseContractCaptureCensus{}, errors.New("historical release capture omits a current emitter")
		}
	}
	query := make([]common.Address, len(releaseStrings))
	for index, address := range releaseStrings {
		query[index] = release[address]
	}
	return finalReleaseContractCaptureCensus{
		fromBlock: fromBlock, currentAddresses: currentStrings, releaseAddresses: releaseStrings, queryAddresses: query,
	}, nil
}

// Rejects noncanonical map keys or values before their address is sent to an
// RPC provider. A caller cannot smuggle an unknown emitter by exploiting Go's
// case-insensitive address parser or an inconsistent map key.
func finalCanonicalReleaseAddressStrings(addresses map[string]common.Address) ([]string, error) {
	result := make([]string, 0, len(addresses))
	for key, address := range addresses {
		canonical, err := finalCanonicalAddress(address.Hex())
		if err != nil || key != canonical || address != common.HexToAddress(key) {
			return nil, stateMismatchError(err, "historical release capture address is not canonical")
		}
		result = append(result, canonical)
	}
	sort.Strings(result)
	for index := 1; index < len(result); index++ {
		if result[index] == result[index-1] {
			return nil, errors.New("historical release capture address is duplicated")
		}
	}
	return result, nil
}

// Validates the active graph exactly and the historical query graph
// canonically. Historical members are later joined one-for-one to archived
// plan actions, while this boundary prevents historical members from entering
// ordinary current-release event selection.
func finalVerifyCollectedReleaseContractCensus(value *FinalCollectedChainSnapshot, deployment *ContractDeployment) (map[string]bool, error) {
	if value == nil || deployment == nil {
		return nil, errors.New("final release capture snapshot is unavailable")
	}
	batcher, err := finalCanonicalAddress(value.FleetBatcher)
	if err != nil || batcher != value.FleetBatcher {
		return nil, stateMismatchError(err, "final release fleet batcher is not canonical")
	}
	current, err := finalReleaseContractAddressSet(deployment, common.HexToAddress(batcher))
	if err != nil {
		return nil, err
	}
	expected, err := finalCanonicalReleaseAddressStrings(current)
	if err != nil || !slices.Equal(value.CurrentReleaseAddresses, expected) {
		return nil, stateMismatchError(err, "final release current emitter census differs from the active deployment")
	}
	release, err := finalCanonicalCollectedReleaseAddresses(value.ReleaseContractAddresses)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(release))
	for _, address := range release {
		allowed[address] = true
	}
	for _, address := range expected {
		if !allowed[address] {
			return nil, errors.New("final release historical emitter census omits an active address")
		}
	}
	return allowed, nil
}

// Requires an ordered, duplicate-free query census so serialized snapshots
// cannot change their release-address projection by capitalization or order.
func finalCanonicalCollectedReleaseAddresses(values []string) ([]string, error) {
	if len(values) < 4 {
		return nil, errors.New("final release historical emitter census is incomplete")
	}
	result := make([]string, len(values))
	for index, value := range values {
		canonical, err := finalCanonicalAddress(value)
		if err != nil || canonical != value || index > 0 && value <= values[index-1] {
			return nil, stateMismatchError(err, "final release historical emitter census is not canonical")
		}
		result[index] = canonical
	}
	return result, nil
}
