package main

// The final live-chain capture is the only network-reading boundary used by
// offline semantic reconstruction. It pins contract logs and native UID state
// to exact finalized heads before supervised services are stopped.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ss58"
)

// v7 retains the exact transaction envelope and authenticated predecessor
// contract census for every captured release log. A receipt only proves
// emitted events; carried-plan replay also needs its caller, calldata, value,
// and the historical deployment graph that emitted every required side effect.
// It separately pins the active deployment boundary so historical logs from a
// reused address cannot enter ordinary current-generation event selection.
const finalCollectedChainSnapshotSchema = "urnetwork-final-chain-snapshot-v7"

// Caps every public EVM log request to the official endpoint's inclusive
// range limit. Collector and public replay share this one deterministic
// splitter so an evidence capture cannot succeed with a request the reviewer
// later cannot reproduce.
const finalEVMLogQueryMaximumBlocks = uint64(1000)

// Represents one inclusive EVM log request interval after a caller has
// authenticated the surrounding campaign heads.
type finalEVMLogQueryRange struct {
	From uint64
	To   uint64
}

// Splits an inclusive block interval into complete, adjacent provider-safe
// requests. Overflow, inverted bounds, gaps, and overlaps are impossible in
// its output and callers can reuse it for capture and transcript replay.
func finalEVMLogQueryRanges(from, to uint64) ([]finalEVMLogQueryRange, error) {
	if from == 0 || to < from || finalEVMLogQueryMaximumBlocks == 0 {
		return nil, errors.New("EVM log query range is invalid")
	}
	result := make([]finalEVMLogQueryRange, 0, 1+(to-from)/finalEVMLogQueryMaximumBlocks)
	for first := from; ; {
		last := first + finalEVMLogQueryMaximumBlocks - 1
		if last < first || last > to {
			last = to
		}
		result = append(result, finalEVMLogQueryRange{From: first, To: last})
		if last == to {
			return result, nil
		}
		first = last + 1
	}
}

type FinalCollectedNativeUIDState struct {
	UID               uint16 `json:"uid"`
	HotkeyPublicKey   string `json:"hotkey_public_key"`
	ColdkeyPublicKey  string `json:"coldkey_public_key"`
	RegistrationBlock uint64 `json:"registration_block"`
	StakeRao          string `json:"stake_rao"`
	ValidatorPermit   bool   `json:"validator_permit"`
	ValidatorTrustU16 uint16 `json:"validator_trust_u16"`
}

// FinalCollectedRewardStakePosition is one exact staking-precompile position.
// Identity is a stable public role label; the cryptographic hotkey/coldkey pair
// is repeated so source reconstruction never trusts a label as authority.
type FinalCollectedRewardStakePosition struct {
	Identity         string `json:"identity"`
	HotkeyPublicKey  string `json:"hotkey_public_key"`
	ColdkeyPublicKey string `json:"coldkey_public_key"`
	StakeRao         string `json:"stake_rao"`
}

// FinalCollectedRewardStakeSnapshot binds a native reward vector checkpoint
// to the same-height finalized EVM block used for getStake. The two hashes are
// intentionally separate because public native and EVM RPC namespaces may
// expose different canonical block identities.
type FinalCollectedRewardStakeSnapshot struct {
	NativeHead ChainHead                           `json:"native_head"`
	EVMHead    ChainHead                           `json:"evm_head"`
	Positions  []FinalCollectedRewardStakePosition `json:"positions"`
}

// Retains the signed envelope for one transaction which emitted a captured
// release-contract log. The log group supplies immutable block coordinates;
// this record supplies the action payload that an archive replay cannot
// reconstruct safely from mutable local state.
type FinalCollectedEVMTransaction struct {
	TransactionHash string    `json:"transaction_hash"`
	Block           ChainHead `json:"block"`
	From            string    `json:"from"`
	To              string    `json:"to"`
	Input           string    `json:"input"`
	ValueWei        string    `json:"value_wei"`
}

// Captures a proxy's canonical post-construction checkpoint before later
// UUPS transitions. Source projection uses it as a real observed baseline
// rather than treating a mutable plan deployment field as historical state.
type FinalCollectedCoordinatorBaseline struct {
	Proxy                     string    `json:"proxy"`
	Head                      ChainHead `json:"head"`
	Implementation            string    `json:"implementation"`
	ImplementationRuntimeHash string    `json:"implementation_runtime_hash"`
	ProxyRuntimeHash          string    `json:"proxy_runtime_hash"`
}

type FinalCollectedChainSnapshot struct {
	Schema                   string                              `json:"schema"`
	Phase                    string                              `json:"phase"`
	RunID                    string                              `json:"run_id"`
	DeploymentID             string                              `json:"deployment_id"`
	FleetBatcher             string                              `json:"fleet_batcher"`
	EVMFromBlock             uint64                              `json:"evm_from_block"`
	CurrentReleaseFromBlock  uint64                              `json:"current_release_from_block"`
	EVMHead                  ChainHead                           `json:"evm_head"`
	CurrentReleaseAddresses  []string                            `json:"current_release_addresses"`
	ReleaseContractAddresses []string                            `json:"release_contract_addresses"`
	EVMLogs                  []finalCanonicalEVMLog              `json:"evm_logs"`
	EVMTransactions          []FinalCollectedEVMTransaction      `json:"evm_transactions"`
	CoordinatorBaselines     []FinalCollectedCoordinatorBaseline `json:"coordinator_baselines"`
	NativeHead               ChainHead                           `json:"native_head"`
	NativeHeads              []ChainHead                         `json:"native_referenced_heads"`
	NativeUIDs               []FinalCollectedNativeUIDState      `json:"native_uids"`
	PublicIdentitiesHash     string                              `json:"public_identities_hash"`
	RewardStakeSnapshots     []FinalCollectedRewardStakeSnapshot `json:"reward_stake_snapshots"`
}

func captureFinalSemanticLiveChain(ctx context.Context, cfg *ResolvedConfig, stateRoot, runRoot string, result *ScenarioResult, terminal *ScenarioObservation, history []*ScenarioObservation) ([]FinalArtifactLocator, error) {
	if ctx == nil || cfg == nil || cfg.Config == nil || result == nil || terminal == nil || terminal.Status == nil || terminal.Status.Contracts == nil || terminal.NativeRewards == nil {
		return nil, errors.New("final live-chain capture context is incomplete")
	}
	if terminal.NativeRewardsError != "" {
		return nil, fmt.Errorf("terminal native reward checkpoint is unavailable: %s", terminal.NativeRewardsError)
	}
	deployment, err := loadContractDeployment(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("load final live-chain deployment: %w", err)
	}
	evmHead := terminal.Status.Contracts.FinalizedHead
	if evmHead != result.EndHead || evmHead.Number < deployment.DeployBlock || deployment.DeployBlock == 0 {
		return nil, errors.New("final EVM capture range is not bound to deployment and scenario terminal head")
	}
	plan, err := readPersistedPlan(stateRoot)
	if err != nil || plan.DeploymentID != deployment.DeploymentID || plan.Deployment.CoordinatorProxy != deployment.CoordinatorProxy {
		return nil, stateMismatchError(err, "load final live-chain plan for fleet batcher")
	}
	batcher, _, err := finalPlanFleetBatcher(plan)
	if err != nil {
		return nil, fmt.Errorf("load final live-chain fleet batcher: %w", err)
	}
	census, err := finalCaptureReleaseContractCensusFromState(stateRoot, plan, deployment, batcher)
	if err != nil {
		return nil, fmt.Errorf("derive final historical release-contract census: %w", err)
	}
	logs, transactions, err := captureFinalEVMLogs(ctx, cfg, census.fromBlock, census.queryAddresses, evmHead)
	if err != nil {
		return nil, err
	}
	baselines, err := captureFinalHistoricalCoordinatorBaselines(ctx, cfg, stateRoot, plan, result.CampaignStartHead, logs)
	if err != nil {
		return nil, err
	}
	nativeHead := terminal.NativeRewards.FinalizedHead
	rewardHeads, err := finalCollectedRewardHeads(history, nativeHead)
	if err != nil {
		return nil, err
	}
	nativeUIDs, nativeHeads, err := captureFinalNativeState(ctx, cfg, stateRoot, nativeHead, rewardHeads)
	if err != nil {
		return nil, err
	}
	identitiesHash, rewardStakes, err := captureFinalRewardStakeSnapshots(ctx, cfg, stateRoot, deployment, evmHead, rewardHeads)
	if err != nil {
		return nil, err
	}
	snapshot := FinalCollectedChainSnapshot{
		Schema: finalCollectedChainSnapshotSchema, Phase: result.Name, RunID: result.RunID, DeploymentID: result.DeploymentID,
		EVMFromBlock:             census.fromBlock,
		CurrentReleaseFromBlock:  deployment.DeployBlock,
		EVMHead:                  evmHead,
		CurrentReleaseAddresses:  census.currentAddresses,
		ReleaseContractAddresses: census.releaseAddresses,
		EVMLogs:                  logs,
		EVMTransactions:          transactions,
		CoordinatorBaselines:     baselines,
		NativeHead:               nativeHead,
		NativeHeads:              nativeHeads,
		NativeUIDs:               nativeUIDs,
		FleetBatcher:             strings.ToLower(batcher.Hex()),
		PublicIdentitiesHash:     identitiesHash, RewardStakeSnapshots: rewardStakes,
	}
	if err := verifyFinalCollectedChainSnapshot(&snapshot, deployment); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(&snapshot)
	if err != nil {
		return nil, err
	}
	entry := FinalCollectedFileBundleEntry{
		Path: "live-chain/final-chain-snapshot.json", ContentHash: bytesSHA256(encoded), SizeBytes: uint64(len(encoded)), Data: encoded,
	}
	return persistFinalCollectedBundleChunks(runRoot, "live-chain", []FinalCollectedFileBundleEntry{entry})
}

func finalCollectedRewardHeads(history []*ScenarioObservation, terminal ChainHead) ([]ChainHead, error) {
	byNumber := make(map[uint64]ChainHead)
	for _, observation := range history {
		if observation == nil || observation.NativeRewards == nil || observation.NativeRewardsError != "" {
			continue
		}
		head := observation.NativeRewards.FinalizedHead
		if head.Number == 0 || head.Number > terminal.Number || requireFinalHex32("native reward checkpoint", strings.ToLower(head.Hash)) != nil {
			return nil, errors.New("closed history contains an invalid native reward checkpoint")
		}
		head.Hash = strings.ToLower(head.Hash)
		if prior, ok := byNumber[head.Number]; ok && prior != head {
			return nil, fmt.Errorf("closed history has conflicting native reward checkpoint at %d", head.Number)
		}
		byNumber[head.Number] = head
	}
	if terminal.Number == 0 || requireFinalHex32("terminal native reward checkpoint", strings.ToLower(terminal.Hash)) != nil {
		return nil, errors.New("terminal native reward checkpoint is invalid")
	}
	terminal.Hash = strings.ToLower(terminal.Hash)
	if prior, ok := byNumber[terminal.Number]; ok && prior != terminal {
		return nil, errors.New("terminal native reward checkpoint conflicts with closed history")
	}
	byNumber[terminal.Number] = terminal
	result := make([]ChainHead, 0, len(byNumber))
	for _, head := range byNumber {
		result = append(result, head)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Number < result[j].Number })
	if len(result) < 2 {
		return nil, errors.New("closed history has fewer than two native reward checkpoints")
	}
	return result, nil
}

type finalRewardStakePair struct {
	identity string
	hotkey   [32]byte
	coldkey  [32]byte
}

func finalRewardStakePairs(cfg *ResolvedConfig, deployment *ContractDeployment, identities *finalPublicIdentities) ([]finalRewardStakePair, error) {
	if cfg == nil || cfg.Config == nil || deployment == nil || identities == nil || identities.DeploymentID != deployment.DeploymentID {
		return nil, errors.New("reward stake identity graph is incomplete")
	}
	result := make([]finalRewardStakePair, 0, 2*cfg.Config.Topology.fleetCandidates()+cfg.Config.Topology.ChurnFloorUIDs+2*cfg.Config.Topology.Operators+2*cfg.Config.Topology.Validators)
	seen := map[string]bool{}
	add := func(identity, hotkeyEncoded, coldkeyEncoded string) error {
		hotkey, err := decodeHex32(identity+" hotkey", strings.ToLower(hotkeyEncoded))
		if err != nil {
			return err
		}
		coldkey, err := decodeHex32(identity+" coldkey", strings.ToLower(coldkeyEncoded))
		if err != nil {
			return err
		}
		if identity == "" || strings.ContainsAny(identity, "/\\\r\n\x00") || seen[identity] {
			return fmt.Errorf("reward stake identity %q is invalid or duplicated", identity)
		}
		seen[identity] = true
		result = append(result, finalRewardStakePair{identity: identity, hotkey: hotkey, coldkey: coldkey})
		return nil
	}
	publicKey := func(label string) string { return identities.Substrate[label].PublicKey }
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		if err := add(fmt.Sprintf("fleet-%d", fleet), publicKey(fleetHotkeyLabel(fleet)), publicKey(fleetColdkeyLabel(fleet))); err != nil {
			return nil, err
		}
	}
	for churn := 1; churn <= cfg.Config.Topology.ChurnFloorUIDs; churn++ {
		if err := add(fmt.Sprintf("churn-%d", churn), publicKey(churnHotkeyLabel(churn)), publicKey(churnColdkeyLabel(churn))); err != nil {
			return nil, err
		}
	}
	vaultColdkey := ss58.EvmMirrorPubkey(deployment.SettlementVault)
	for noID := 1; noID <= cfg.Config.Topology.Operators; noID++ {
		if err := add(fmt.Sprintf("pool-%d", noID), publicKey(fmt.Sprintf("operator-%d-pool-hotkey", noID)), fmt.Sprintf("0x%x", vaultColdkey[:])); err != nil {
			return nil, err
		}
	}
	for validator := 1; validator <= cfg.Config.Topology.Validators; validator++ {
		if err := add(fmt.Sprintf("validator-%d", validator), publicKey(validatorHotkeyLabel(validator)), publicKey(fmt.Sprintf("validator-%d-coldkey", validator))); err != nil {
			return nil, err
		}
	}
	reserveColdkey := ss58.EvmMirrorPubkey(deployment.ReserveSink)
	if err := add("reserve-validator-sink", publicKey(validatorHotkeyLabel(1)), fmt.Sprintf("0x%x", reserveColdkey[:])); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].identity < result[j].identity })
	return result, nil
}

func captureFinalRewardStakeSnapshots(ctx context.Context, cfg *ResolvedConfig, stateRoot string, deployment *ContractDeployment, terminalEVM ChainHead, heads []ChainHead) (string, []FinalCollectedRewardStakeSnapshot, error) {
	if len(heads) == 0 {
		return "", nil, errors.New("reward-stake capture has no native checkpoints")
	}
	identityPath := filepath.Join(stateRoot, "public", "identities.json")
	if !pathWithinRoot(stateRoot, identityPath) {
		return "", nil, errors.New("public identities path escapes state root")
	}
	if err := rejectFinalArtifactSymlinkComponents(stateRoot, identityPath); err != nil {
		return "", nil, err
	}
	info, err := os.Lstat(identityPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("public identities are not a regular file")
	}
	identityBytes, err := os.ReadFile(identityPath)
	if err != nil {
		return "", nil, err
	}
	var identities finalPublicIdentities
	if err := decodeStrictJSONBytes(identityBytes, &identities); err != nil || identities.Schema != "urnetwork-sim-public-identities-v1" || identities.DeploymentID != deployment.DeploymentID {
		return "", nil, stateMismatchError(err, "public reward-stake identities are invalid")
	}
	pairs, err := finalRewardStakePairs(cfg, deployment, &identities)
	if err != nil {
		return "", nil, err
	}
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return "", nil, fmt.Errorf("dial reward-stake EVM capture: %w", err)
	}
	defer client.Close()
	finalized, err := finalizedEVMHead(ctx, client)
	if err != nil || finalized.Number < terminalEVM.Number || finalized.Number < heads[len(heads)-1].Number {
		return "", nil, stateMismatchError(err, "public EVM finalized head does not cover reward-stake capture")
	}
	parsed, err := abi.JSON(strings.NewReader(stakingPrecompileABI))
	if err != nil {
		return "", nil, err
	}
	result := make([]FinalCollectedRewardStakeSnapshot, 0, len(heads))
	for _, nativeHead := range heads {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		evmHead, err := (ethEVMBlockReader{client: client}).EVMBlockByNumber(ctx, new(big.Int).SetUint64(nativeHead.Number))
		if err != nil {
			return "", nil, stateMismatchError(err, "resolve same-height finalized EVM reward-stake block")
		}
		snapshot := FinalCollectedRewardStakeSnapshot{NativeHead: nativeHead, EVMHead: evmHead, Positions: make([]FinalCollectedRewardStakePosition, 0, len(pairs))}
		for _, pair := range pairs {
			values, err := contractCallAt(ctx, client, stakingPrecompileAddress, parsed, "getStake", evmHead.Number, pair.hotkey, pair.coldkey, new(big.Int).SetUint64(uint64(cfg.Netuid)))
			if err != nil || len(values) != 1 {
				return "", nil, stateMismatchError(err, "capture %s getStake returned %d values", pair.identity, len(values))
			}
			stake, ok := values[0].(*big.Int)
			if !ok || stake.Sign() < 0 {
				return "", nil, fmt.Errorf("capture %s getStake returned %T or a negative value", pair.identity, values[0])
			}
			snapshot.Positions = append(snapshot.Positions, FinalCollectedRewardStakePosition{
				Identity: pair.identity, HotkeyPublicKey: strings.ToLower(fmt.Sprintf("0x%x", pair.hotkey[:])),
				ColdkeyPublicKey: strings.ToLower(fmt.Sprintf("0x%x", pair.coldkey[:])), StakeRao: stake.String(),
			})
		}
		recheck, err := (ethEVMBlockReader{client: client}).EVMBlockByNumber(ctx, new(big.Int).SetUint64(evmHead.Number))
		if err != nil || recheck != evmHead {
			return "", nil, stateMismatchError(err, "same-height EVM reward-stake block changed during capture")
		}
		result = append(result, snapshot)
	}
	return bytesSHA256(identityBytes), result, nil
}

func captureFinalEVMLogs(ctx context.Context, cfg *ResolvedConfig, fromBlock uint64, addresses []common.Address, terminal ChainHead) ([]finalCanonicalEVMLog, []FinalCollectedEVMTransaction, error) {
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return nil, nil, fmt.Errorf("dial final EVM capture: %w", err)
	}
	defer client.Close()
	head, err := (ethEVMBlockReader{client: client}).EVMBlockByNumber(ctx, new(big.Int).SetUint64(terminal.Number))
	if err != nil || !strings.EqualFold(head.Hash, terminal.Hash) {
		return nil, nil, errors.New("final EVM capture terminal block is not canonical")
	}
	if fromBlock == 0 || len(addresses) == 0 {
		return nil, nil, errors.New("final EVM capture release address range is unavailable")
	}
	const maximumAddresses = 32
	values := make([]finalCanonicalEVMLog, 0)
	ranges, err := finalEVMLogQueryRanges(fromBlock, terminal.Number)
	if err != nil {
		return nil, nil, err
	}
	for _, blockRange := range ranges {
		for addressStart := 0; addressStart < len(addresses); addressStart += maximumAddresses {
			addressEnd := addressStart + maximumAddresses
			if addressEnd > len(addresses) {
				addressEnd = len(addresses)
			}
			logs, queryErr := client.FilterLogs(ctx, ethereum.FilterQuery{
				FromBlock: new(big.Int).SetUint64(blockRange.From), ToBlock: new(big.Int).SetUint64(blockRange.To), Addresses: addresses[addressStart:addressEnd],
			})
			if queryErr != nil {
				return nil, nil, fmt.Errorf("capture release-contract logs [%d,%d] addresses[%d,%d): %w", blockRange.From, blockRange.To, addressStart, addressEnd, queryErr)
			}
			for _, value := range logs {
				canonical, canonicalErr := finalCanonicalLogFromGeth(value)
				if canonicalErr != nil {
					return nil, nil, canonicalErr
				}
				values = append(values, canonical)
			}
		}
	}
	logs, err := finalCanonicalizeLogs(values)
	if err != nil {
		return nil, nil, err
	}
	transactions, err := captureFinalEVMTransactions(ctx, cfg, client, logs)
	if err != nil {
		return nil, nil, err
	}
	recheck, err := (ethEVMBlockReader{client: client}).EVMBlockByNumber(ctx, new(big.Int).SetUint64(terminal.Number))
	if err != nil || recheck != head {
		return nil, nil, stateMismatchError(err, "final EVM capture terminal block changed during capture")
	}
	return logs, transactions, nil
}

// Resolves each transaction exactly once after log collection. The stable
// block/hash ordering makes the captured envelope deterministic even when an
// RPC provider returns transaction lookups in a different order.
func captureFinalEVMTransactions(ctx context.Context, cfg *ResolvedConfig, client *ethclient.Client, logs []finalCanonicalEVMLog) ([]FinalCollectedEVMTransaction, error) {
	if cfg == nil || client == nil || len(logs) == 0 {
		return nil, errors.New("final EVM transaction capture inputs are incomplete")
	}
	byHash := make(map[string]ChainHead, len(logs))
	for _, log := range logs {
		head := ChainHead{Number: log.BlockNumber, Hash: log.BlockHash}
		if prior, found := byHash[log.TransactionHash]; found && prior != head {
			return nil, fmt.Errorf("captured transaction %s spans conflicting blocks", log.TransactionHash)
		}
		byHash[log.TransactionHash] = head
	}
	hashes := make([]string, 0, len(byHash))
	for hash := range byHash {
		hashes = append(hashes, hash)
	}
	sort.Slice(hashes, func(i, j int) bool {
		left, right := byHash[hashes[i]], byHash[hashes[j]]
		if left.Number != right.Number {
			return left.Number < right.Number
		}
		return hashes[i] < hashes[j]
	})
	result := make([]FinalCollectedEVMTransaction, 0, len(hashes))
	for _, hash := range hashes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		transaction, pending, err := client.TransactionByHash(ctx, common.HexToHash(hash))
		if err != nil || transaction == nil || pending || !strings.EqualFold(transaction.Hash().Hex(), hash) || transaction.To() == nil || transaction.Value().Sign() < 0 {
			return nil, stateMismatchError(err, "capture release transaction %s", hash)
		}
		chainID := transaction.ChainId()
		if chainID == nil || !chainID.IsUint64() || chainID.Uint64() != cfg.ChainID {
			return nil, fmt.Errorf("capture release transaction %s has unexpected chain ID", hash)
		}
		from, err := ethTypes.Sender(ethTypes.LatestSignerForChainID(chainID), transaction)
		if err != nil || from == (common.Address{}) {
			return nil, stateMismatchError(err, "recover release transaction %s sender", hash)
		}
		result = append(result, FinalCollectedEVMTransaction{
			TransactionHash: strings.ToLower(transaction.Hash().Hex()), Block: byHash[hash], From: strings.ToLower(from.Hex()),
			To: strings.ToLower(transaction.To().Hex()), Input: hexutil.Encode(transaction.Data()), ValueWei: transaction.Value().String(),
		})
	}
	return result, nil
}

// Captures every proxy's post-construction slot and code at the exact
// initializer Upgraded event. The generic deployment manifest block may refer
// to a later helper deployment, so only the finalized coordinator-proxy
// action and its raw event are accepted as a chronology baseline.
func captureFinalHistoricalCoordinatorBaselines(ctx context.Context, cfg *ResolvedConfig, stateRoot string, current *SetupPlan, campaignStart ChainHead, logs []finalCanonicalEVMLog) ([]FinalCollectedCoordinatorBaseline, error) {
	if ctx == nil || cfg == nil || current == nil || stateRoot == "" || campaignStart.Number < 2 || len(logs) == 0 {
		return nil, errors.New("historical coordinator baseline capture inputs are incomplete")
	}
	plans := map[string]*SetupPlan{current.PlanHash: current}
	for _, hash := range current.PriorPlanHashes {
		path := filepath.Join(stateRoot, "plans", stringsTrim0x(hash)+".json")
		plan, err := readPersistedPlanFile(path)
		if err != nil || plan == nil || !strings.EqualFold(plan.PlanHash, hash) || plan.DeploymentID != current.DeploymentID || plan.ChainID != current.ChainID || plan.Netuid != current.Netuid {
			return nil, stateMismatchError(err, "load historical coordinator baseline plan %s", hash)
		}
		if _, duplicate := plans[plan.PlanHash]; duplicate {
			return nil, fmt.Errorf("historical coordinator baseline plan %s is duplicated", plan.PlanHash)
		}
		plans[plan.PlanHash] = plan
	}
	journalBytes, err := os.ReadFile(filepath.Join(stateRoot, "journal.jsonl"))
	if err != nil {
		return nil, err
	}
	entries, err := decodeFinalSemanticJournalBytes(journalBytes)
	if err != nil {
		return nil, err
	}
	logsByTransaction := make(map[string][]finalCanonicalEVMLog)
	for _, log := range logs {
		logsByTransaction[log.TransactionHash] = append(logsByTransaction[log.TransactionHash], log)
	}
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return nil, fmt.Errorf("dial historical coordinator baseline EVM capture: %w", err)
	}
	defer client.Close()
	result := make([]FinalCollectedCoordinatorBaseline, 0, len(plans))
	seen := make(map[string]bool, len(plans))
	for _, entry := range entries {
		if entry.Stage != StageFinalized || entry.BlockNumber == 0 || entry.BlockNumber >= campaignStart.Number || entry.DeploymentID != current.DeploymentID {
			continue
		}
		plan := plans[entry.PlanHash]
		if plan == nil || !current.allowedPlanHashes()[entry.PlanHash] {
			continue
		}
		action, actionErr := exactPlanActionByID(plan, entry.ActionID)
		if actionErr != nil || action.ID != "evm.coordinator-proxy" || action.Kind != "evm-transaction" || !actionAcceptsIntent(action, entry.IntentHash) {
			continue
		}
		proxy := strings.ToLower(plan.Deployment.CoordinatorProxy.Hex())
		if seen[proxy] {
			return nil, fmt.Errorf("historical coordinator baseline proxy %s has multiple deployment actions", proxy)
		}
		group := logsByTransaction[entry.TransactionHash]
		var upgraded *finalCanonicalEVMLog
		for index := range group {
			implementation, found, logErr := finalHistoricalCoordinatorUpgradedLog(group[index])
			if logErr != nil {
				return nil, logErr
			}
			if !found || group[index].Address != proxy {
				continue
			}
			if upgraded != nil {
				return nil, fmt.Errorf("historical coordinator baseline proxy %s has multiple initializer events", proxy)
			}
			copy := group[index]
			copy.Topics = append([]string(nil), group[index].Topics...)
			upgraded = &copy
			_ = implementation
		}
		if upgraded == nil || upgraded.BlockNumber != entry.BlockNumber || !strings.EqualFold(upgraded.BlockHash, entry.BlockHash) {
			return nil, fmt.Errorf("historical coordinator baseline proxy %s has no exact initializer event", proxy)
		}
		implementation, _, implementationErr := finalHistoricalCoordinatorUpgradedLog(*upgraded)
		if implementationErr != nil || !strings.EqualFold(implementation, plan.Deployment.CoordinatorImplementation.Hex()) {
			return nil, stateMismatchError(implementationErr, "historical coordinator baseline proxy %s initializer differs from its plan", proxy)
		}
		head := ChainHead{Number: entry.BlockNumber, Hash: strings.ToLower(entry.BlockHash)}
		canonical, blockErr := (ethEVMBlockReader{client: client}).EVMBlockByNumber(ctx, new(big.Int).SetUint64(head.Number))
		if blockErr != nil || canonical != head {
			return nil, stateMismatchError(blockErr, "historical coordinator baseline proxy %s header is not canonical", proxy)
		}
		selector := finalEVMBlockSelector{BlockHash: head.Hash, RequireCanonical: true}
		var slot hexutil.Bytes
		if err := client.Client().CallContext(ctx, &slot, "eth_getStorageAt", proxy, erc1967ImplementationSlot, selector); err != nil || len(slot) != common.HashLength || !strings.EqualFold(common.BytesToAddress(slot[common.HashLength-common.AddressLength:]).Hex(), implementation) {
			return nil, stateMismatchError(err, "historical coordinator baseline proxy %s implementation slot differs", proxy)
		}
		var proxyCode hexutil.Bytes
		if err := client.Client().CallContext(ctx, &proxyCode, "eth_getCode", proxy, selector); err != nil || len(proxyCode) == 0 {
			return nil, stateMismatchError(err, "historical coordinator baseline proxy %s code is unavailable", proxy)
		}
		var implementationCode hexutil.Bytes
		if err := client.Client().CallContext(ctx, &implementationCode, "eth_getCode", implementation, selector); err != nil || len(implementationCode) == 0 {
			return nil, stateMismatchError(err, "historical coordinator baseline implementation %s code is unavailable", implementation)
		}
		implementationHash := strings.ToLower(crypto.Keccak256Hash(implementationCode).Hex())
		plannedHash := strings.ToLower(plan.Deployment.RuntimeHashes[plan.Deployment.CoordinatorImplementation.Hex()])
		if requireFinalHex32("historical coordinator baseline planned runtime", plannedHash) != nil || !strings.EqualFold(implementationHash, plannedHash) {
			return nil, errors.New("historical coordinator baseline implementation runtime differs from its plan")
		}
		recheck, blockErr := (ethEVMBlockReader{client: client}).EVMBlockByNumber(ctx, new(big.Int).SetUint64(head.Number))
		if blockErr != nil || recheck != head {
			return nil, stateMismatchError(blockErr, "historical coordinator baseline proxy %s block changed during capture", proxy)
		}
		result = append(result, FinalCollectedCoordinatorBaseline{Proxy: proxy, Head: head, Implementation: implementation, ImplementationRuntimeHash: implementationHash, ProxyRuntimeHash: strings.ToLower(crypto.Keccak256Hash(proxyCode).Hex())})
		seen[proxy] = true
	}
	if len(result) == 0 {
		return nil, errors.New("historical coordinator baseline capture found no proxy deployment action")
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Proxy < result[right].Proxy })
	for index := range result {
		if err := finalVerifyHistoricalCoordinatorBaseline(result[index]); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func captureFinalNativeState(ctx context.Context, cfg *ResolvedConfig, stateRoot string, head ChainHead, rewardHeads []ChainHead) ([]FinalCollectedNativeUIDState, []ChainHead, error) {
	if head.Number == 0 || head.Hash == "" {
		return nil, nil, errors.New("final native capture head is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	chain, err := crv4.DialChain(cfg.OperationalSubstrate)
	if err != nil {
		return nil, nil, fmt.Errorf("dial final native capture: %w", err)
	}
	defer chain.API.Client.Close()
	hash, err := types.NewHashFromHexString(head.Hash)
	if err != nil {
		return nil, nil, fmt.Errorf("decode final native head: %w", err)
	}
	if !strings.EqualFold(chain.GenesisHash.Hex(), cfg.Public.Chain.GenesisHash) {
		return nil, nil, fmt.Errorf("final native capture genesis %s, want %s", chain.GenesisHash.Hex(), cfg.Public.Chain.GenesisHash)
	}
	authenticated, err := readAuthenticatedRuntimeMetadataAt(chain, cfg, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("authenticate final native runtime at %s: %w", hash.Hex(), err)
	}
	bindAuthenticatedRuntime(chain, authenticated)
	header, err := chain.API.RPC.Chain.GetHeader(hash)
	if err != nil || header == nil || uint64(header.Number) != head.Number {
		return nil, nil, errors.New("final native capture head number and hash differ")
	}
	topology, err := readSubnetTopologyAt(chain, cfg.Netuid, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("read final native topology: %w", err)
	}
	facts, err := readExistingUIDFactsAt(chain, cfg.Netuid, hash, topology)
	if err != nil {
		return nil, nil, fmt.Errorf("read final native UID identities: %w", err)
	}
	readVector := func(storage string, out any) error {
		key, keyErr := types.CreateStorageKey(chain.Meta, crv4.PalletName, storage, netuidArg(cfg.Netuid))
		if keyErr != nil {
			return keyErr
		}
		return readRequiredStorageAt(chain, key, crv4.PalletName, storage, out, hash)
	}
	var permits []bool
	if err := readVector("ValidatorPermit", &permits); err != nil {
		return nil, nil, fmt.Errorf("read final validator permits: %w", err)
	}
	var trusts []types.U16
	if err := readVector("ValidatorTrust", &trusts); err != nil {
		return nil, nil, fmt.Errorf("read final validator trust: %w", err)
	}
	if len(facts) != int(topology.UIDCount) || len(permits) != len(facts) || len(trusts) != len(facts) {
		return nil, nil, fmt.Errorf("final native vectors identities/permits/trust=%d/%d/%d, want %d", len(facts), len(permits), len(trusts), topology.UIDCount)
	}
	result := make([]FinalCollectedNativeUIDState, len(facts))
	for index, fact := range facts {
		result[index] = FinalCollectedNativeUIDState{
			UID: fact.UID, HotkeyPublicKey: strings.ToLower(fact.Hotkey), ColdkeyPublicKey: strings.ToLower(fact.Coldkey),
			RegistrationBlock: fact.RegistrationBlock, StakeRao: strconv.FormatUint(fact.TotalHotkeyAlphaRao, 10),
			ValidatorPermit: permits[index], ValidatorTrustU16: uint16(trusts[index]),
		}
	}
	blockNumbers := map[uint64]bool{head.Number: true}
	rewardByNumber := make(map[uint64]ChainHead, len(rewardHeads))
	for _, rewardHead := range rewardHeads {
		if rewardHead.Number == 0 || rewardHead.Number > head.Number || rewardHead.Hash == "" {
			return nil, nil, errors.New("referenced reward head is outside the terminal native checkpoint")
		}
		blockNumbers[rewardHead.Number] = true
		rewardByNumber[rewardHead.Number] = rewardHead
	}
	for validatorID := 1; validatorID <= cfg.Config.Topology.Validators; validatorID++ {
		intents, readErr := readValidatorIntentFile(stateRoot, validatorID)
		if readErr != nil {
			return nil, nil, fmt.Errorf("read validator %d native receipt references: %w", validatorID, readErr)
		}
		for _, intent := range intents {
			for _, number := range []uint64{intent.FinalizedBlock, intent.RevealBlock, intent.ApplicationBlock} {
				if number != 0 {
					if number > head.Number {
						return nil, nil, fmt.Errorf("validator %d references future native block %d beyond terminal %d", validatorID, number, head.Number)
					}
					blockNumbers[number] = true
				}
			}
		}
	}
	numbers := make([]uint64, 0, len(blockNumbers))
	for number := range blockNumbers {
		numbers = append(numbers, number)
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	heads := make([]ChainHead, len(numbers))
	for index, number := range numbers {
		blockHash, hashErr := chain.API.RPC.Chain.GetBlockHash(number)
		if hashErr != nil {
			return nil, nil, fmt.Errorf("resolve referenced native block %d: %w", number, hashErr)
		}
		if blockHash == (types.Hash{}) {
			return nil, nil, fmt.Errorf("resolve referenced native block %d: empty block hash", number)
		}
		heads[index] = ChainHead{Number: number, Hash: strings.ToLower(blockHash.Hex())}
		if rewardHead, ok := rewardByNumber[number]; ok && heads[index] != rewardHead {
			return nil, nil, fmt.Errorf("native reward checkpoint %d is not canonical", number)
		}
	}
	if heads[len(heads)-1] != head {
		return nil, nil, errors.New("referenced native heads do not end at the terminal checkpoint")
	}
	return result, heads, nil
}

func verifyFinalCollectedChainSnapshot(value *FinalCollectedChainSnapshot, deployment *ContractDeployment) error {
	if value == nil || deployment == nil || value.Schema != finalCollectedChainSnapshotSchema || value.Phase == "" || value.RunID == "" || value.DeploymentID != deployment.DeploymentID || value.EVMFromBlock == 0 || value.CurrentReleaseFromBlock != deployment.DeployBlock || value.EVMFromBlock > value.CurrentReleaseFromBlock || value.EVMHead.Number < value.EVMFromBlock || value.NativeHead.Number == 0 || len(value.EVMLogs) == 0 || len(value.EVMTransactions) == 0 || len(value.NativeHeads) == 0 || len(value.NativeUIDs) == 0 || !validSHA256ContentHash(value.PublicIdentitiesHash) || len(value.RewardStakeSnapshots) < 2 || !common.IsHexAddress(value.FleetBatcher) || common.HexToAddress(value.FleetBatcher) == (common.Address{}) {
		return errors.New("final chain snapshot identity or contents are incomplete")
	}
	if _, err := decodeHex32("final EVM snapshot head", value.EVMHead.Hash); err != nil {
		return err
	}
	if _, err := decodeHex32("final native snapshot head", value.NativeHead.Hash); err != nil {
		return err
	}
	for index, head := range value.NativeHeads {
		if head.Number == 0 || head.Number > value.NativeHead.Number || (index > 0 && head.Number <= value.NativeHeads[index-1].Number) {
			return errors.New("referenced native heads are incomplete or non-canonical")
		}
		if _, err := decodeHex32("referenced native head", head.Hash); err != nil {
			return err
		}
	}
	if value.NativeHeads[len(value.NativeHeads)-1] != value.NativeHead {
		return errors.New("referenced native heads do not include the terminal checkpoint")
	}
	allowed, err := finalVerifyCollectedReleaseContractCensus(value, deployment)
	if err != nil {
		return err
	}
	logs, err := finalCanonicalizeLogs(value.EVMLogs)
	if err != nil {
		return err
	}
	for index, log := range logs {
		if !finalJSONEqual(log, value.EVMLogs[index]) || !allowed[log.Address] || log.BlockNumber < value.EVMFromBlock || log.BlockNumber > value.EVMHead.Number {
			return errors.New("final EVM log capture is non-canonical or outside the pinned range")
		}
	}
	if err := verifyFinalCollectedEVMTransactions(value.EVMTransactions, logs, allowed); err != nil {
		return err
	}
	if err := verifyFinalCollectedCoordinatorBaselines(value.CoordinatorBaselines, value.EVMLogs); err != nil {
		return err
	}
	for index, uid := range value.NativeUIDs {
		if int(uid.UID) != index || uid.RegistrationBlock == 0 || len(uid.HotkeyPublicKey) != 66 || len(uid.ColdkeyPublicKey) != 66 || !strings.HasPrefix(uid.HotkeyPublicKey, "0x") || !strings.HasPrefix(uid.ColdkeyPublicKey, "0x") {
			return errors.New("final native UID snapshot is incomplete or non-canonical")
		}
		stake, ok := new(big.Int).SetString(uid.StakeRao, 10)
		if !ok || stake.Sign() < 0 || stake.String() != uid.StakeRao {
			return errors.New("final native UID stake is not a decimal integer")
		}
	}
	nativeHeads := make(map[ChainHead]bool, len(value.NativeHeads))
	for _, head := range value.NativeHeads {
		nativeHeads[head] = true
	}
	var priorIdentities []string
	for snapshotIndex, snapshot := range value.RewardStakeSnapshots {
		if !nativeHeads[snapshot.NativeHead] || snapshot.EVMHead.Number != snapshot.NativeHead.Number || snapshot.EVMHead.Number > value.EVMHead.Number || len(snapshot.Positions) == 0 || snapshotIndex > 0 && snapshot.NativeHead.Number <= value.RewardStakeSnapshots[snapshotIndex-1].NativeHead.Number {
			return errors.New("reward stake snapshot is unpinned, incomplete, or non-canonical")
		}
		if err := verifyFinalHead("reward stake EVM block", snapshot.EVMHead); err != nil {
			return err
		}
		identities := make([]string, len(snapshot.Positions))
		seenPairs := make(map[string]bool, len(snapshot.Positions))
		for index, position := range snapshot.Positions {
			if position.Identity == "" || strings.ContainsAny(position.Identity, "/\\\r\n\x00") || index > 0 && position.Identity <= snapshot.Positions[index-1].Identity {
				return errors.New("reward stake positions are not canonically ordered")
			}
			if _, err := decodeHex32("reward stake hotkey", position.HotkeyPublicKey); err != nil {
				return err
			}
			if _, err := decodeHex32("reward stake coldkey", position.ColdkeyPublicKey); err != nil {
				return err
			}
			stake, ok := new(big.Int).SetString(position.StakeRao, 10)
			if !ok || stake.Sign() < 0 || stake.String() != position.StakeRao {
				return errors.New("reward owner-pair stake is not a canonical nonnegative integer")
			}
			pair := position.HotkeyPublicKey + "|" + position.ColdkeyPublicKey
			if seenPairs[pair] {
				return errors.New("reward stake snapshot duplicates a cryptographic owner pair")
			}
			seenPairs[pair] = true
			identities[index] = position.Identity + "|" + position.HotkeyPublicKey + "|" + position.ColdkeyPublicKey
		}
		if snapshotIndex > 0 && !slices.Equal(identities, priorIdentities) {
			return errors.New("reward stake snapshot identity census changed between checkpoints")
		}
		priorIdentities = identities
	}
	return nil
}

// Verifies canonical baseline observations before source reconstruction joins
// them to archived coordinator-proxy actions. The snapshot validator cannot
// infer plan lineage, but it can reject duplicated, malformed, or unanchored
// post-construction state before that later exact join.
func verifyFinalCollectedCoordinatorBaselines(values []FinalCollectedCoordinatorBaseline, logs []finalCanonicalEVMLog) error {
	if len(values) == 0 {
		return nil
	}
	upgradedTopic := strings.ToLower(crypto.Keccak256Hash([]byte("Upgraded(address)")).Hex())
	for index := range values {
		baseline := values[index]
		if err := finalVerifyHistoricalCoordinatorBaseline(baseline); err != nil {
			return err
		}
		if index > 0 && baseline.Proxy <= values[index-1].Proxy {
			return errors.New("historical coordinator baselines are not canonically ordered")
		}
		found := false
		for _, log := range logs {
			implementation, isUpgrade, err := finalHistoricalCoordinatorUpgradedLog(log)
			if err != nil {
				return err
			}
			if !isUpgrade || log.Address != baseline.Proxy || log.BlockNumber != baseline.Head.Number || !strings.EqualFold(log.BlockHash, baseline.Head.Hash) || !strings.EqualFold(implementation, baseline.Implementation) || !strings.EqualFold(log.Topics[0], upgradedTopic) {
				continue
			}
			if found {
				return errors.New("historical coordinator baseline has multiple matching initializer logs")
			}
			found = true
		}
		if !found {
			return errors.New("historical coordinator baseline has no matching initializer log")
		}
	}
	return nil
}

// Checks that every retained release log has exactly one canonical transaction
// envelope and that the snapshot cannot introduce a payload without a log
// anchored at its claimed block. This closes both directions of the source
// graph before archived-plan action replay begins.
func verifyFinalCollectedEVMTransactions(values []FinalCollectedEVMTransaction, logs []finalCanonicalEVMLog, allowed map[string]bool) error {
	required := make(map[string]ChainHead, len(logs))
	for _, log := range logs {
		head := ChainHead{Number: log.BlockNumber, Hash: log.BlockHash}
		if prior, found := required[log.TransactionHash]; found && prior != head {
			return errors.New("final EVM logs bind one transaction to conflicting blocks")
		}
		required[log.TransactionHash] = head
	}
	for index, value := range values {
		if err := requireFinalHex32("final EVM transaction", value.TransactionHash); err != nil || value.TransactionHash != strings.ToLower(value.TransactionHash) {
			return stateMismatchError(err, "final EVM transaction hash is not canonical")
		}
		head, found := required[value.TransactionHash]
		if !found || value.Block != head {
			return errors.New("final EVM transaction has no exact captured receipt-log group")
		}
		from, fromErr := finalCanonicalAddress(value.From)
		to, toErr := finalCanonicalAddress(value.To)
		if fromErr != nil || toErr != nil || from != value.From || to != value.To || !allowed[value.To] {
			return stateMismatchError(errors.Join(fromErr, toErr), "final EVM transaction sender or target is not canonical release state")
		}
		input, inputErr := hexutil.Decode(value.Input)
		if inputErr != nil || hexutil.Encode(input) != value.Input {
			return stateMismatchError(inputErr, "final EVM transaction calldata is not canonical")
		}
		amount, amountOK := new(big.Int).SetString(value.ValueWei, 10)
		if !amountOK || amount.Sign() < 0 || amount.String() != value.ValueWei {
			return errors.New("final EVM transaction value is not a canonical nonnegative decimal")
		}
		if index > 0 {
			previous := values[index-1]
			if value.Block.Number < previous.Block.Number || value.Block.Number == previous.Block.Number && value.TransactionHash <= previous.TransactionHash {
				return errors.New("final EVM transactions are not canonically ordered")
			}
		}
		delete(required, value.TransactionHash)
	}
	if len(required) != 0 {
		return errors.New("final EVM transaction capture omits a receipt-log group")
	}
	return nil
}
