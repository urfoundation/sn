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
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ss58"
)

const finalCollectedChainSnapshotSchema = "urnetwork-final-chain-snapshot-v2"

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

type FinalCollectedChainSnapshot struct {
	Schema               string                              `json:"schema"`
	Phase                string                              `json:"phase"`
	RunID                string                              `json:"run_id"`
	DeploymentID         string                              `json:"deployment_id"`
	EVMFromBlock         uint64                              `json:"evm_from_block"`
	EVMHead              ChainHead                           `json:"evm_head"`
	EVMLogs              []finalCanonicalEVMLog              `json:"evm_logs"`
	NativeHead           ChainHead                           `json:"native_head"`
	NativeHeads          []ChainHead                         `json:"native_referenced_heads"`
	NativeUIDs           []FinalCollectedNativeUIDState      `json:"native_uids"`
	PublicIdentitiesHash string                              `json:"public_identities_hash"`
	RewardStakeSnapshots []FinalCollectedRewardStakeSnapshot `json:"reward_stake_snapshots"`
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
	logs, err := captureFinalEVMLogs(ctx, cfg, deployment, evmHead)
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
		EVMFromBlock: deployment.DeployBlock, EVMHead: evmHead, EVMLogs: logs, NativeHead: nativeHead, NativeHeads: nativeHeads, NativeUIDs: nativeUIDs,
		PublicIdentitiesHash: identitiesHash, RewardStakeSnapshots: rewardStakes,
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
		header, err := client.HeaderByNumber(ctx, new(big.Int).SetUint64(nativeHead.Number))
		if err != nil || header == nil || header.Number == nil || !header.Number.IsUint64() || header.Number.Uint64() != nativeHead.Number {
			return "", nil, stateMismatchError(err, "resolve same-height finalized EVM reward-stake block")
		}
		evmHead := ChainHead{Number: nativeHead.Number, Hash: strings.ToLower(header.Hash().Hex())}
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
		recheck, err := client.HeaderByNumber(ctx, new(big.Int).SetUint64(evmHead.Number))
		if err != nil || recheck == nil || !strings.EqualFold(recheck.Hash().Hex(), evmHead.Hash) {
			return "", nil, stateMismatchError(err, "same-height EVM reward-stake block changed during capture")
		}
		result = append(result, snapshot)
	}
	return bytesSHA256(identityBytes), result, nil
}

func captureFinalEVMLogs(ctx context.Context, cfg *ResolvedConfig, deployment *ContractDeployment, terminal ChainHead) ([]finalCanonicalEVMLog, error) {
	client, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
	if err != nil {
		return nil, fmt.Errorf("dial final EVM capture: %w", err)
	}
	defer client.Close()
	header, err := client.HeaderByNumber(ctx, new(big.Int).SetUint64(terminal.Number))
	if err != nil || header == nil || !strings.EqualFold(header.Hash().Hex(), terminal.Hash) {
		return nil, errors.New("final EVM capture terminal block is not canonical")
	}
	addresses := []common.Address{deployment.CoordinatorProxy, deployment.SettlementVault, deployment.ReserveSink}
	const maximumRange = uint64(2_000)
	values := make([]finalCanonicalEVMLog, 0)
	for first := deployment.DeployBlock; first <= terminal.Number; {
		last := first + maximumRange - 1
		if last < first || last > terminal.Number {
			last = terminal.Number
		}
		logs, queryErr := client.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(first), ToBlock: new(big.Int).SetUint64(last), Addresses: addresses,
		})
		if queryErr != nil {
			return nil, fmt.Errorf("capture release-contract logs [%d,%d]: %w", first, last, queryErr)
		}
		for _, value := range logs {
			canonical, canonicalErr := finalCanonicalLogFromGeth(value)
			if canonicalErr != nil {
				return nil, canonicalErr
			}
			values = append(values, canonical)
		}
		if last == terminal.Number {
			break
		}
		first = last + 1
	}
	return finalCanonicalizeLogs(values)
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
		if hashErr != nil || blockHash == (types.Hash{}) {
			return nil, nil, fmt.Errorf("resolve referenced native block %d: %w", number, hashErr)
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
	if value == nil || deployment == nil || value.Schema != finalCollectedChainSnapshotSchema || value.Phase == "" || value.RunID == "" || value.DeploymentID != deployment.DeploymentID || value.EVMFromBlock != deployment.DeployBlock || value.EVMFromBlock == 0 || value.EVMHead.Number < value.EVMFromBlock || value.NativeHead.Number == 0 || len(value.EVMLogs) == 0 || len(value.NativeHeads) == 0 || len(value.NativeUIDs) == 0 || !validSHA256ContentHash(value.PublicIdentitiesHash) || len(value.RewardStakeSnapshots) < 2 {
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
	allowed := map[string]bool{
		strings.ToLower(deployment.CoordinatorProxy.Hex()): true,
		strings.ToLower(deployment.SettlementVault.Hex()):  true,
		strings.ToLower(deployment.ReserveSink.Hex()):      true,
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
