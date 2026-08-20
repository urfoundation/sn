package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/signature"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/extrinsic/extensions"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/crypto/blake2b"

	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ss58"
)

type SubstrateManager struct {
	chain    *crv4.Chain
	signer   signature.KeyringPair
	stateDir string
	journal  *Journal
	cfg      *ResolvedConfig
}

// SetupFacts are finalized, read-only inputs which make the setup plan exact.
// In particular, alpha is transferred from an existing wallet-owned position;
// the harness never guesses an AMM conversion or silently buys an unbounded
// amount of subnet alpha while applying an approved plan.
type SetupFacts struct {
	BurnRao             uint64 `json:"burn_rao"`
	AlphaSourceHotkey   string `json:"alpha_source_hotkey"`
	AlphaAvailableRao   uint64 `json:"alpha_available_rao"`
	NominatorMinimumRao uint64 `json:"nominator_minimum_rao"`
	ProbeTAORao         uint64 `json:"probe_tao_rao"`
	WalletFreeTAORao    uint64 `json:"wallet_free_tao_rao"`
	FinalizedBlock      uint64 `json:"finalized_block"`
	FinalizedBlockHash  string `json:"finalized_block_hash"`
}

const stakingPrecompileABI = `[{"type":"function","name":"getStake","inputs":[{"name":"hotkey","type":"bytes32"},{"name":"coldkey","type":"bytes32"},{"name":"netuid","type":"uint256"}],"outputs":[{"name":"","type":"uint256"}],"stateMutability":"view"},{"type":"function","name":"getNominatorMinRequiredStake","inputs":[],"outputs":[{"name":"","type":"uint256"}],"stateMutability":"view"}]`

var stakingPrecompileAddress = common.HexToAddress("0x0000000000000000000000000000000000000805")

// ReadSetupFacts selects the largest finalized alpha position controlled by
// the supplied testnet wallet. A single source position is intentional: each
// planned transfer is then atomic, bounded, and trivially resumable.
func ReadSetupFacts(ctx context.Context, cfg *ResolvedConfig) (*SetupFacts, error) {
	ws, httpURL, err := authorityURLs(cfg.Authority)
	if err != nil {
		return nil, err
	}
	chain, err := crv4.DialChain(ws)
	if err != nil {
		return nil, err
	}
	defer chain.API.Client.Close()
	if strings.ToLower(chain.GenesisHash.Hex()) != testnetGenesis || uint32(chain.Runtime.SpecVersion) != cfg.Public.Chain.ExpectedRuntimeSpec {
		return nil, fmt.Errorf("setup facts RPC runtime identity mismatch")
	}
	finalizedHash, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return nil, err
	}
	header, err := chain.API.RPC.Chain.GetHeader(finalizedHash)
	if err != nil {
		return nil, err
	}
	facts := &SetupFacts{FinalizedBlock: uint64(header.Number), FinalizedBlockHash: finalizedHash.Hex()}
	burnKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "Burn", netuidArg(cfg.Netuid))
	if err != nil {
		return nil, err
	}
	var burn types.U64
	if ok, err := chain.API.RPC.State.GetStorage(burnKey, &burn, finalizedHash); err != nil {
		return nil, err
	} else if !ok || burn == 0 {
		return nil, fmt.Errorf("subnet burn is unavailable or zero at finalized block %d", facts.FinalizedBlock)
	}
	facts.BurnRao = uint64(burn)
	wallet, err := ss58.DecodeWithPrefix(cfg.WalletPublic, ss58.BittensorPrefix)
	if err != nil {
		return nil, fmt.Errorf("testnet wallet public key: %w", err)
	}
	accountKey, err := types.CreateStorageKey(chain.Meta, "System", "Account", wallet[:])
	if err != nil {
		return nil, err
	}
	var account types.AccountInfo
	if ok, readErr := chain.API.RPC.State.GetStorage(accountKey, &account, finalizedHash); readErr != nil {
		return nil, readErr
	} else if !ok || account.Data.Free.Int == nil || !account.Data.Free.IsUint64() {
		return nil, fmt.Errorf("testnet wallet free balance is unavailable or exceeds uint64")
	}
	facts.WalletFreeTAORao = account.Data.Free.Uint64()
	hotkeysKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "StakingHotkeys", wallet[:])
	if err != nil {
		return nil, err
	}
	var hotkeys []types.AccountID
	if _, err := chain.API.RPC.State.GetStorage(hotkeysKey, &hotkeys, finalizedHash); err != nil {
		return nil, err
	}
	if len(hotkeys) == 0 {
		return nil, fmt.Errorf("testnet wallet has no staking hotkeys")
	}
	evm, err := ethclient.DialContext(ctx, httpURL)
	if err != nil {
		return nil, err
	}
	defer evm.Close()
	chainID, err := evm.ChainID(ctx)
	if err != nil || chainID.Uint64() != testnetChainID {
		return nil, fmt.Errorf("setup facts EVM identity mismatch: chain_id=%v err=%v", chainID, err)
	}
	parsed, err := abi.JSON(strings.NewReader(stakingPrecompileABI))
	if err != nil {
		return nil, err
	}
	minimumData, err := parsed.Pack("getNominatorMinRequiredStake")
	if err != nil {
		return nil, err
	}
	minimumRaw, err := evm.CallContract(ctx, ethereum.CallMsg{To: &stakingPrecompileAddress, Data: minimumData}, new(big.Int).SetUint64(facts.FinalizedBlock))
	if err != nil {
		return nil, fmt.Errorf("read nominator minimum: %w", err)
	}
	minimumValues, err := parsed.Unpack("getNominatorMinRequiredStake", minimumRaw)
	if err != nil || len(minimumValues) != 1 {
		return nil, fmt.Errorf("decode nominator minimum: %w", err)
	}
	minimum, ok := minimumValues[0].(*big.Int)
	if !ok || !minimum.IsUint64() {
		return nil, fmt.Errorf("nominator minimum exceeds uint64")
	}
	facts.NominatorMinimumRao = minimum.Uint64()
	facts.ProbeTAORao = max64(facts.NominatorMinimumRao, 1_000)
	for _, account := range hotkeys {
		var hotkey [32]byte
		copy(hotkey[:], account[:])
		data, packErr := parsed.Pack("getStake", hotkey, wallet, new(big.Int).SetUint64(uint64(cfg.Netuid)))
		if packErr != nil {
			return nil, packErr
		}
		out, callErr := evm.CallContract(ctx, ethereum.CallMsg{To: &stakingPrecompileAddress, Data: data}, new(big.Int).SetUint64(facts.FinalizedBlock))
		if callErr != nil {
			return nil, fmt.Errorf("read alpha position 0x%x: %w", hotkey, callErr)
		}
		values, unpackErr := parsed.Unpack("getStake", out)
		if unpackErr != nil || len(values) != 1 {
			return nil, fmt.Errorf("decode alpha position 0x%x: %w", hotkey, unpackErr)
		}
		stake, ok := values[0].(*big.Int)
		if !ok || !stake.IsUint64() {
			return nil, fmt.Errorf("alpha position 0x%x exceeds uint64", hotkey)
		}
		if stake.Uint64() > facts.AlphaAvailableRao {
			facts.AlphaAvailableRao = stake.Uint64()
			facts.AlphaSourceHotkey = "0x" + fmt.Sprintf("%x", hotkey)
		}
	}
	if facts.AlphaSourceHotkey == "" || facts.AlphaAvailableRao == 0 {
		return nil, fmt.Errorf("testnet wallet has no alpha on netuid %d", cfg.Netuid)
	}
	return facts, nil
}

func DialSubstrateManager(cfg *ResolvedConfig, stateDir string, j *Journal) (*SubstrateManager, error) {
	ws, _, err := authorityURLs(cfg.Authority)
	if err != nil {
		return nil, err
	}
	chain, err := crv4.DialChain(ws)
	if err != nil {
		return nil, err
	}
	if strings.ToLower(chain.GenesisHash.Hex()) != testnetGenesis || uint32(chain.Runtime.SpecVersion) != cfg.Public.Chain.ExpectedRuntimeSpec {
		chain.API.Client.Close()
		return nil, fmt.Errorf("private RPC runtime identity mismatch")
	}
	signer, err := signature.KeyringPairFromSecret(cfg.WalletMaterial, 42)
	if err != nil {
		chain.API.Client.Close()
		return nil, err
	}
	if err, _ := verifySubnetOwner(chain, cfg.Netuid, signer.Address); err != nil {
		chain.API.Client.Close()
		return nil, err
	}
	return &SubstrateManager{chain: chain, signer: signer, stateDir: stateDir, journal: j, cfg: cfg}, nil
}

func DialIndependentSubstrateManager(cfg *ResolvedConfig) (*SubstrateManager, error) {
	chain, err := crv4.DialChain(cfg.Public.Chain.SubstratePublicReadEndpoint)
	if err != nil {
		return nil, err
	}
	if strings.ToLower(chain.GenesisHash.Hex()) != testnetGenesis || uint32(chain.Runtime.SpecVersion) != cfg.Public.Chain.ExpectedRuntimeSpec || uint32(chain.Runtime.TransactionVersion) != cfg.Public.Chain.ExpectedTransactionVersion {
		chain.API.Client.Close()
		return nil, fmt.Errorf("independent RPC runtime identity mismatch")
	}
	return &SubstrateManager{chain: chain, cfg: cfg}, nil
}
func (m *SubstrateManager) Close() { m.chain.API.Client.Close() }

type hyperShape struct{ Storage, Call, Kind string }

var hyperShapes = map[string]hyperShape{
	"tempo": {"Tempo", "sudo_set_tempo", "u16"}, "max_allowed_uids": {"MaxAllowedUids", "sudo_set_max_allowed_uids", "u16"}, "mechanism_count": {"MechanismCountCurrent", "sudo_set_mechanism_count", "u8"}, "commit_reveal_weights_enabled": {"CommitRevealWeightsEnabled", "sudo_set_commit_reveal_weights_enabled", "bool"}, "commit_reveal_period": {"RevealPeriodEpochs", "sudo_set_commit_reveal_weights_interval", "u64"}, "liquid_alpha_enabled": {"LiquidAlphaOn", "sudo_set_liquid_alpha_enabled", "bool"}, "immunity_period": {"ImmunityPeriod", "sudo_set_immunity_period", "u16"}, "min_allowed_weights": {"MinAllowedWeights", "sudo_set_min_allowed_weights", "u16"}, "weights_version_key": {"WeightsVersionKey", "sudo_set_weights_version_key", "u64"}, "serving_rate_limit": {"ServingRateLimit", "sudo_set_serving_rate_limit", "u64"}, "transfer_enabled": {"SubtokenEnabled", "sudo_set_toggle_transfer", "bool"},
}

func netuidArg(n uint16) []byte { var b [2]byte; binary.LittleEndian.PutUint16(b[:], n); return b[:] }

func (m *SubstrateManager) finalizedHead() (types.Hash, uint64, error) {
	hash, err := m.chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return types.Hash{}, 0, err
	}
	header, err := m.chain.API.RPC.Chain.GetHeader(hash)
	if err != nil {
		return types.Hash{}, 0, err
	}
	return hash, uint64(header.Number), nil
}

func (m *SubstrateManager) ReadHyper(name string) (any, error) {
	s, ok := hyperShapes[name]
	if !ok {
		return nil, fmt.Errorf("unsupported owner hyperparameter %q", name)
	}
	key, err := types.CreateStorageKey(m.chain.Meta, crv4.PalletName, s.Storage, netuidArg(m.cfg.Netuid))
	if err != nil {
		return nil, err
	}
	finalized, _, err := m.finalizedHead()
	if err != nil {
		return nil, err
	}
	switch s.Kind {
	case "u8":
		var v types.U8
		_, err = m.chain.API.RPC.State.GetStorage(key, &v, finalized)
		return uint8(v), err
	case "u16":
		var v types.U16
		_, err = m.chain.API.RPC.State.GetStorage(key, &v, finalized)
		return uint16(v), err
	case "u64":
		var v types.U64
		_, err = m.chain.API.RPC.State.GetStorage(key, &v, finalized)
		return uint64(v), err
	case "bool":
		var v types.Bool
		_, err = m.chain.API.RPC.State.GetStorage(key, &v, finalized)
		return bool(v), err
	default:
		return nil, fmt.Errorf("invalid hyperparameter shape")
	}
}

func normalizeYAMLValue(v any, kind string) (any, error) {
	switch kind {
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("want bool, got %T", v)
		}
		return b, nil
	case "u8", "u16", "u64":
		var value uint64
		switch x := v.(type) {
		case int:
			if x < 0 {
				return nil, fmt.Errorf("want unsigned integer, got %d", x)
			}
			value = uint64(x)
		case uint64:
			value = x
		default:
			return nil, fmt.Errorf("want unsigned integer, got %T", v)
		}
		if kind == "u8" && value > math.MaxUint8 {
			return nil, fmt.Errorf("value %d exceeds u8", value)
		}
		if kind == "u16" && value > math.MaxUint16 {
			return nil, fmt.Errorf("value %d exceeds u16", value)
		}
		return value, nil
	}
	return nil, fmt.Errorf("unsupported kind")
}
func hyperEqual(got, want any, kind string) bool {
	n, err := normalizeYAMLValue(want, kind)
	if err != nil {
		return false
	}
	switch v := got.(type) {
	case uint8:
		return uint64(v) == n.(uint64)
	case uint16:
		return uint64(v) == n.(uint64)
	case uint64:
		return v == n.(uint64)
	case bool:
		return v == n.(bool)
	}
	return false
}

func (m *SubstrateManager) HyperCall(name string, want any) (types.Call, error) {
	s, ok := hyperShapes[name]
	if !ok {
		return types.Call{}, fmt.Errorf("unsupported owner hyperparameter %q", name)
	}
	v, err := normalizeYAMLValue(want, s.Kind)
	if err != nil {
		return types.Call{}, err
	}
	args := []any{types.NewU16(m.cfg.Netuid)}
	switch s.Kind {
	case "u8":
		args = append(args, types.NewU8(uint8(v.(uint64))))
	case "u16":
		args = append(args, types.NewU16(uint16(v.(uint64))))
	case "u64":
		args = append(args, types.NewU64(v.(uint64)))
	case "bool":
		args = append(args, types.NewBool(v.(bool)))
	}
	return types.NewCall(m.chain.Meta, "AdminUtils."+s.Call, args...)
}

func (m *SubstrateManager) FundCall(destination [32]byte, rao uint64) (types.Call, error) {
	addr, err := types.NewMultiAddressFromAccountID(destination[:])
	if err != nil {
		return types.Call{}, err
	}
	return types.NewCall(m.chain.Meta, "Balances.transfer_keep_alive", addr, types.NewUCompactFromUInt(rao))
}

func (m *SubstrateManager) FreeBalance(account [32]byte) (uint64, error) {
	key, err := types.CreateStorageKey(m.chain.Meta, "System", "Account", account[:])
	if err != nil {
		return 0, err
	}
	var info types.AccountInfo
	finalized, _, err := m.finalizedHead()
	if err != nil {
		return 0, err
	}
	ok, err := m.chain.API.RPC.State.GetStorage(key, &info, finalized)
	if err != nil {
		return 0, err
	}
	if !ok || info.Data.Free.Int == nil {
		return 0, nil
	}
	if !info.Data.Free.IsUint64() {
		return 0, fmt.Errorf("free balance for 0x%x exceeds uint64", account)
	}
	return info.Data.Free.Uint64(), nil
}

func (m *SubstrateManager) Send(ctx context.Context, planHash string, a Action, call types.Call) (types.Hash, uint64, error) {
	return m.SendAs(ctx, planHash, a, call, m.signer)
}

func (m *SubstrateManager) SendAs(ctx context.Context, planHash string, a Action, call types.Call, signer signature.KeyringPair) (types.Hash, uint64, error) {
	if prior, ok := m.journal.LatestTransaction(a.ID, a.IntentHash); ok {
		rawPath := filepath.Join(m.stateDir, "transactions", stringsTrim0x(prior.TransactionHash)+".scale")
		raw, err := os.ReadFile(rawPath)
		if err != nil {
			return types.Hash{}, 0, fmt.Errorf("resume exact substrate transaction %s: %w", prior.TransactionHash, err)
		}
		digest := blake2b.Sum256(raw)
		hash := types.Hash(digest)
		if !strings.EqualFold(hash.Hex(), prior.TransactionHash) {
			return types.Hash{}, 0, fmt.Errorf("persisted substrate transaction hash mismatch: got %s want %s", hash.Hex(), prior.TransactionHash)
		}
		return m.watchRaw(ctx, planHash, a, raw, hash, prior.RecoveryBlock, prior.RecoveryBlockHash)
	}
	var nonce uint32
	if err := m.chain.API.Client.Call(&nonce, "system_accountNextIndex", signer.Address); err != nil {
		return types.Hash{}, 0, err
	}
	ext := extrinsic.NewExtrinsic(call)
	err := ext.Sign(signer, m.chain.Meta, extrinsic.WithEra(types.ExtrinsicEra{IsImmortalEra: true}, m.chain.GenesisHash), extrinsic.WithNonce(types.NewUCompactFromUInt(uint64(nonce))), extrinsic.WithTip(types.NewUCompactFromUInt(0)), extrinsic.WithSpecVersion(m.chain.Runtime.SpecVersion), extrinsic.WithTransactionVersion(m.chain.Runtime.TransactionVersion), extrinsic.WithGenesisHash(m.chain.GenesisHash), extrinsic.WithMetadataMode(extensions.CheckMetadataModeDisabled, extensions.CheckMetadataHash{Hash: types.NewEmptyOption[types.H256]()}))
	if err != nil {
		return types.Hash{}, 0, err
	}
	raw, err := codec.Encode(ext)
	if err != nil {
		return types.Hash{}, 0, err
	}
	h := blake2b.Sum256(raw)
	hash := types.Hash(h)
	path := filepath.Join(m.stateDir, "transactions", stringsTrim0x(hash.Hex())+".scale")
	if err := atomicWrite(path, raw, 0o600); err != nil {
		return types.Hash{}, 0, err
	}
	recoveryHash, recoveryBlock, err := m.finalizedHead()
	if err != nil {
		return types.Hash{}, 0, err
	}
	if err := m.journal.Append(JournalEntry{DeploymentID: m.cfg.Config.Deployment.DeploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageBroadcast, Signer: signer.Address, Nonce: strconv.FormatUint(uint64(nonce), 10), TransactionHash: hash.Hex(), RecoveryBlock: recoveryBlock, RecoveryBlockHash: recoveryHash.Hex()}); err != nil {
		return types.Hash{}, 0, err
	}
	return m.watchRaw(ctx, planHash, a, raw, hash, recoveryBlock, recoveryHash.Hex())
}

func (m *SubstrateManager) RoleSigner(roles *RoleSecrets, label string) (signature.KeyringPair, error) {
	v, ok := roles.Substrate[label]
	if !ok {
		return signature.KeyringPair{}, fmt.Errorf("missing substrate role %s", label)
	}
	return signature.KeyringPairFromSecret("0x"+v.SeedHex, 42)
}
func (m *SubstrateManager) BurnRegisterCall(hotkey [32]byte) (types.Call, error) {
	account, err := types.NewAccountID(hotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	return types.NewCall(m.chain.Meta, crv4.PalletName+".burned_register", types.NewU16(m.cfg.Netuid), *account)
}
func (m *SubstrateManager) AddStakeCall(hotkey [32]byte, amount uint64) (types.Call, error) {
	account, err := types.NewAccountID(hotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	return types.NewCall(m.chain.Meta, crv4.PalletName+".add_stake", *account, types.NewU16(m.cfg.Netuid), types.NewUCompactFromUInt(amount))
}

func (m *SubstrateManager) DelegateTake(hotkey [32]byte) (uint16, error) {
	key, err := types.CreateStorageKey(m.chain.Meta, crv4.PalletName, "Delegates", hotkey[:])
	if err != nil {
		return 0, err
	}
	var take types.U16
	finalized, _, err := m.finalizedHead()
	if err != nil {
		return 0, err
	}
	if _, err := m.chain.API.RPC.State.GetStorage(key, &take, finalized); err != nil {
		return 0, err
	}
	return uint16(take), nil
}

func (m *SubstrateManager) DecreaseTakeCall(hotkey [32]byte, take uint16) (types.Call, error) {
	account, err := types.NewAccountID(hotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	// Runtime v447 encodes sp_runtime::PerU16 exactly as its u16 parts.
	return types.NewCall(m.chain.Meta, crv4.PalletName+".decrease_take", *account, types.NewU16(take))
}

func (m *SubstrateManager) TransferStakeAndHotkeyCall(destinationColdkey, originHotkey, destinationHotkey [32]byte, amount uint64) (types.Call, error) {
	destination, err := types.NewAccountID(destinationColdkey[:])
	if err != nil {
		return types.Call{}, err
	}
	origin, err := types.NewAccountID(originHotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	destinationHot, err := types.NewAccountID(destinationHotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	return types.NewCall(
		m.chain.Meta,
		crv4.PalletName+".transfer_stake_and_hotkey",
		*destination,
		*origin,
		*destinationHot,
		types.NewU16(m.cfg.Netuid),
		types.NewU16(m.cfg.Netuid),
		types.NewU64(amount),
	)
}
func (m *SubstrateManager) UID(hotkey [32]byte) (uint16, bool, error) {
	key, err := types.CreateStorageKey(m.chain.Meta, crv4.PalletName, "Uids", netuidArg(m.cfg.Netuid), hotkey[:])
	if err != nil {
		return 0, false, err
	}
	var uid types.U16
	finalized, _, err := m.finalizedHead()
	if err != nil {
		return 0, false, err
	}
	ok, err := m.chain.API.RPC.State.GetStorage(key, &uid, finalized)
	return uint16(uid), ok, err
}

func (m *SubstrateManager) appendRecoveredFinality(planHash string, a Action, hash types.Hash, receipt *crv4.FinalizedExtrinsic, recoveryBlock uint64, recoveryHash string) (types.Hash, uint64, error) {
	if err := m.journal.Append(JournalEntry{DeploymentID: m.cfg.Config.Deployment.DeploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageFinalized, TransactionHash: hash.Hex(), BlockNumber: receipt.BlockNumber, BlockHash: receipt.BlockHash.Hex(), RecoveryBlock: recoveryBlock, RecoveryBlockHash: recoveryHash}); err != nil {
		return hash, 0, err
	}
	return hash, receipt.BlockNumber, nil
}

func (m *SubstrateManager) watchRaw(ctx context.Context, planHash string, a Action, raw []byte, hash types.Hash, recoveryBlock uint64, recoveryHash string) (types.Hash, uint64, error) {
	if receipt, found, err := m.chain.FindFinalizedExtrinsic(ctx, hash, recoveryBlock); err != nil {
		return hash, 0, err
	} else if found {
		return m.appendRecoveredFinality(planHash, a, hash, receipt, recoveryBlock, recoveryHash)
	}
	statuses := make(chan types.ExtrinsicStatus)
	sub, err := m.chain.API.Client.Subscribe(ctx, "author", "submitAndWatchExtrinsic", "unwatchExtrinsic", "extrinsicUpdate", statuses, codec.HexEncodeToString(raw))
	if err != nil {
		if receipt, found, scanErr := m.chain.FindFinalizedExtrinsic(ctx, hash, recoveryBlock); scanErr == nil && found {
			return m.appendRecoveredFinality(planHash, a, hash, receipt, recoveryBlock, recoveryHash)
		}
		return types.Hash{}, 0, err
	}
	defer sub.Unsubscribe()
	var included types.Hash
	for {
		select {
		case <-ctx.Done():
			return hash, 0, ctx.Err()
		case err := <-sub.Err():
			if err != nil {
				return hash, 0, err
			}
		case status, ok := <-statuses:
			if !ok {
				return hash, 0, fmt.Errorf("extrinsic status closed")
			}
			if status.IsInBlock {
				included = status.AsInBlock
				header, e := m.chain.API.RPC.Chain.GetHeader(included)
				if e == nil {
					if err := m.journal.Append(JournalEntry{DeploymentID: m.cfg.Config.Deployment.DeploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageIncluded, TransactionHash: hash.Hex(), BlockNumber: uint64(header.Number), BlockHash: included.Hex(), RecoveryBlock: recoveryBlock, RecoveryBlockHash: recoveryHash}); err != nil {
						return hash, 0, err
					}
				}
			}
			if status.IsFinalized {
				if e := m.chain.VerifyFinalizedExtrinsic(status.AsFinalized, hash); e != nil {
					return hash, 0, e
				}
				header, e := m.chain.API.RPC.Chain.GetHeader(status.AsFinalized)
				if e != nil {
					return hash, 0, e
				}
				n := uint64(header.Number)
				if err := m.journal.Append(JournalEntry{DeploymentID: m.cfg.Config.Deployment.DeploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageFinalized, TransactionHash: hash.Hex(), BlockNumber: n, BlockHash: status.AsFinalized.Hex(), RecoveryBlock: recoveryBlock, RecoveryBlockHash: recoveryHash}); err != nil {
					return hash, 0, err
				}
				return hash, n, nil
			}
			if status.IsDropped || status.IsInvalid || status.IsUsurped || status.IsFinalityTimeout || status.IsRetracted {
				return hash, 0, fmt.Errorf("extrinsic %s did not finalize: %+v (included %s)", hash, status, included)
			}
		}
	}
}

func (m *SubstrateManager) VerifyObservedGates() (map[string]any, error) {
	return verifyCompatibilityGates(m.chain, m.cfg)
}

func evaluateCompatibilityGate(gate CompatibilityGate, observed []uint64) error {
	if gate.Rule == "nonzero" {
		if len(observed) != 1 || observed[0] == 0 {
			return fmt.Errorf("observed %v, require nonzero", observed)
		}
		return nil
	}
	if gate.Rule != "exact" || len(observed) != len(gate.Expected) {
		return fmt.Errorf("unsupported or malformed compatibility rule")
	}
	for i := range observed {
		if observed[i] != gate.Expected[i] {
			return fmt.Errorf("observed %v, require exact %v", observed, gate.Expected)
		}
	}
	return nil
}

func verifyCompatibilityGates(chain *crv4.Chain, cfg *ResolvedConfig) (map[string]any, error) {
	finalized, err := chain.API.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	names := make([]string, 0, len(cfg.Hyperparameters.ObservedCompatibilityGates))
	for name := range cfg.Hyperparameters.ObservedCompatibilityGates {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		gate := cfg.Hyperparameters.ObservedCompatibilityGates[name]
		var args [][]byte
		if gate.Scope == "netuid" {
			args = append(args, netuidArg(cfg.Netuid))
		}
		key, keyErr := types.CreateStorageKey(chain.Meta, crv4.PalletName, gate.Storage, args...)
		if keyErr != nil {
			return nil, fmt.Errorf("compatibility gate %s storage: %w", name, keyErr)
		}
		var observed []uint64
		var present bool
		switch gate.Kind {
		case "u16":
			var value types.U16
			present, err = chain.API.RPC.State.GetStorage(key, &value, finalized)
			observed = []uint64{uint64(value)}
		case "u64":
			var value types.U64
			present, err = chain.API.RPC.State.GetStorage(key, &value, finalized)
			observed = []uint64{uint64(value)}
		case "u16_pair":
			var value struct {
				Low  types.U16
				High types.U16
			}
			present, err = chain.API.RPC.State.GetStorage(key, &value, finalized)
			observed = []uint64{uint64(value.Low), uint64(value.High)}
		default:
			return nil, fmt.Errorf("compatibility gate %s has unsupported kind %s", name, gate.Kind)
		}
		if err != nil {
			return nil, fmt.Errorf("compatibility gate %s read: %w", name, err)
		}
		if !present {
			return nil, fmt.Errorf("compatibility gate %s storage is absent", name)
		}
		if err := evaluateCompatibilityGate(gate, observed); err != nil {
			return nil, fmt.Errorf("compatibility gate %s: %w (%s)", name, err, gate.Decision)
		}
		out[name] = map[string]any{"storage": gate.Storage, "scope": gate.Scope, "observed": observed, "rule": gate.Rule, "expected": gate.Expected, "decision": gate.Decision, "finalized_block_hash": finalized.Hex()}
	}
	return out, nil
}

func evmMirrorForAddress(addr [20]byte) ([32]byte, string, error) {
	pk := ss58.EvmMirrorPubkey(addr)
	s, err := ss58.Encode(pk, 42)
	return pk, s, err
}

var _ = time.Second
