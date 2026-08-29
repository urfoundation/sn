package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
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

// Retains the provider's integer token so parsing never passes through float64.
type nativeTransactionFeeResponse struct {
	PartialFee json.RawMessage `json:"partialFee"`
}

// Parse the standard payment_queryInfo fee without accepting JSON floats,
// signs, overflow, or provider-specific loss of integer precision.
func parseNativeTransactionFee(raw json.RawMessage) (uint64, error) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return 0, fmt.Errorf("decode native transaction fee: %w", err)
		}
		value = strings.TrimSpace(decoded)
	}
	base := 10
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 16
		value = value[2:]
	}
	if value == "" {
		return 0, errors.New("native transaction fee is empty")
	}
	fee, err := strconv.ParseUint(value, base, 64)
	if err != nil {
		return 0, fmt.Errorf("native transaction fee %q is not an unsigned uint64: %w", value, err)
	}
	return fee, nil
}

// Enforce the approval-bound per-extrinsic ceiling.
func validateNativeTransactionFee(estimated, limit uint64) error {
	if limit == 0 {
		return errors.New("native transaction fee limit is zero")
	}
	if estimated > limit {
		return fmt.Errorf("estimated native transaction fee %d rao exceeds approved limit %d rao", estimated, limit)
	}
	return nil
}

// Quote the exact signed bytes immediately before broadcast. The funded role
// balance remains the hard loss boundary if the runtime multiplier changes
// between this read and inclusion.
func (self *SubstrateManager) approveNativeTransactionFee(ctx context.Context, raw []byte) (uint64, uint64, error) {
	if self == nil || self.chain == nil || self.chain.API == nil || self.chain.API.Client == nil || self.cfg == nil || self.cfg.Config == nil {
		return 0, 0, errors.New("native transaction fee quote dependencies are unavailable")
	}
	var response nativeTransactionFeeResponse
	if err := self.chain.API.Client.CallContext(ctx, &response, "payment_queryInfo", codec.HexEncodeToString(raw)); err != nil {
		return 0, 0, fmt.Errorf("quote native transaction fee: %w", err)
	}
	estimated, err := parseNativeTransactionFee(response.PartialFee)
	if err != nil {
		return 0, 0, err
	}
	limit := self.cfg.Config.Budgets.MaximumNativeTransactionFeeRao
	if err := validateNativeTransactionFee(estimated, limit); err != nil {
		return estimated, limit, err
	}
	return estimated, limit, nil
}

// subtensorAccountInfo matches runtime 451's System.Account value. Subtensor's
// Balance is u64 (rao), while the generic GSRPC AccountInfo assumes u128 and
// therefore cannot decode this runtime's AccountData.
type subtensorAccountInfo struct {
	Nonce       types.U32
	Consumers   types.U32
	Providers   types.U32
	Sufficients types.U32
	Data        struct {
		Free     types.U64
		Reserved types.U64
		Frozen   types.U64
		Flags    types.U128
	}
}

// SetupFacts are finalized, read-only inputs which make the setup plan exact.
// In particular, alpha is transferred from an existing wallet-owned position;
// the harness never guesses an AMM conversion or silently buys an unbounded
// amount of subnet alpha while applying an approved plan.
type SetupFacts struct {
	BurnRao               uint64            `json:"burn_rao"`
	MinBurnRao            uint64            `json:"min_burn_rao,omitempty"`
	MaxBurnRao            uint64            `json:"max_burn_rao,omitempty"`
	BurnHalfLifeBlocks    uint16            `json:"burn_half_life_blocks,omitempty"`
	BurnIncreaseMultQ64   string            `json:"burn_increase_mult_q64,omitempty"`
	AlphaSourceHotkey     string            `json:"alpha_source_hotkey"`
	AlphaAvailableRao     uint64            `json:"alpha_available_rao"`
	ExistingUIDCount      uint16            `json:"existing_uid_count"`
	SubnetOwnerHotkey     string            `json:"subnet_owner_hotkey"`
	UIDZeroHotkey         string            `json:"uid_zero_hotkey"`
	ExistingUIDs          []ExistingUIDFact `json:"existing_uids"`
	ExistentialDepositRao uint64            `json:"existential_deposit_rao"`
	NominatorMinimumRao   uint64            `json:"nominator_minimum_rao"`
	ProbeTAORao           uint64            `json:"probe_tao_rao"`
	WalletFreeTAORao      uint64            `json:"wallet_free_tao_rao"`
	FinalizedBlock        uint64            `json:"finalized_block"`
	FinalizedBlockHash    string            `json:"finalized_block_hash"`
}

type ExistingUIDFact struct {
	UID               uint16 `json:"uid"`
	Hotkey            string `json:"hotkey"`
	Coldkey           string `json:"coldkey"`
	RegistrationBlock uint64 `json:"registration_block"`
	SubnetOwner       bool   `json:"subnet_owner"`
}

// Captures the runtime auction state needed to prove a bounded bootstrap.
type registrationEconomics struct {
	BurnRao             uint64
	MinBurnRao          uint64
	MaxBurnRao          uint64
	BurnHalfLifeBlocks  uint16
	BurnIncreaseMultQ64 string
}

type SubnetTopologyFacts struct {
	UIDCount    uint16
	OwnerHotkey [32]byte
	UIDZero     [32]byte
}

type runtime451PruneNeuron struct {
	UID               uint16
	Hotkey            [32]byte
	EmissionRao       uint64
	RegistrationBlock uint64
	Immune            bool
	Immortal          bool
}

// Read every auction parameter from one finalized state root.
func readRegistrationEconomicsAt(chain *crv4.Chain, netuid uint16, finalized types.Hash) (registrationEconomics, error) {
	var result registrationEconomics
	if chain == nil || chain.API == nil || chain.API.RPC == nil || chain.API.RPC.State == nil || chain.Meta == nil {
		return result, errors.New("registration economics chain dependencies are unavailable")
	}
	readU64 := func(storage string) (uint64, error) {
		key, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, storage, netuidArg(netuid))
		if err != nil {
			return 0, err
		}
		var value types.U64
		if err := readRequiredStorageAt(chain, key, crv4.PalletName, storage, &value, finalized); err != nil {
			return 0, err
		}
		return uint64(value), nil
	}
	var err error
	if result.BurnRao, err = readU64("Burn"); err != nil {
		return result, fmt.Errorf("read Burn: %w", err)
	} else if result.BurnRao == 0 {
		return result, errors.New("Burn is zero")
	}
	if result.MinBurnRao, err = readU64("MinBurn"); err != nil {
		return result, fmt.Errorf("read MinBurn: %w", err)
	} else if result.MinBurnRao == 0 {
		return result, errors.New("MinBurn is zero")
	}
	if result.MaxBurnRao, err = readU64("MaxBurn"); err != nil {
		return result, fmt.Errorf("read MaxBurn: %w", err)
	} else if result.MaxBurnRao == 0 {
		return result, errors.New("MaxBurn is zero")
	}
	halfLifeKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "BurnHalfLife", netuidArg(netuid))
	if err != nil {
		return result, err
	}
	var halfLife types.U16
	if err := readRequiredStorageAt(chain, halfLifeKey, crv4.PalletName, "BurnHalfLife", &halfLife, finalized); err != nil {
		return result, fmt.Errorf("read BurnHalfLife: %w", err)
	} else if halfLife == 0 {
		return result, errors.New("BurnHalfLife is zero")
	}
	result.BurnHalfLifeBlocks = uint16(halfLife)
	multiplierKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "BurnIncreaseMult", netuidArg(netuid))
	if err != nil {
		return result, err
	}
	var multiplier types.U128
	if err := readRequiredStorageAt(chain, multiplierKey, crv4.PalletName, "BurnIncreaseMult", &multiplier, finalized); err != nil {
		return result, fmt.Errorf("read BurnIncreaseMult: %w", err)
	} else if multiplier.Int == nil || multiplier.Sign() <= 0 {
		return result, errors.New("BurnIncreaseMult is zero")
	}
	result.BurnIncreaseMultQ64 = multiplier.String()
	return result, nil
}

func runtime451PruneCandidate(neurons []runtime451PruneNeuron, minimumNonImmune uint16) (uint16, error) {
	var bestImmune *runtime451PruneNeuron
	var bestNonImmune *runtime451PruneNeuron
	var nonImmune uint16
	better := func(candidate runtime451PruneNeuron, current *runtime451PruneNeuron) bool {
		return current == nil || candidate.EmissionRao < current.EmissionRao ||
			(candidate.EmissionRao == current.EmissionRao && candidate.RegistrationBlock < current.RegistrationBlock) ||
			(candidate.EmissionRao == current.EmissionRao && candidate.RegistrationBlock == current.RegistrationBlock && candidate.UID < current.UID)
	}
	for i := range neurons {
		candidate := neurons[i]
		if candidate.Immortal {
			continue
		}
		if candidate.Immune {
			if better(candidate, bestImmune) {
				copy := candidate
				bestImmune = &copy
			}
			continue
		}
		nonImmune++
		if better(candidate, bestNonImmune) {
			copy := candidate
			bestNonImmune = &copy
		}
	}
	if nonImmune > minimumNonImmune && bestNonImmune != nil {
		return bestNonImmune.UID, nil
	}
	if bestImmune != nil {
		return bestImmune.UID, nil
	}
	return 0, errors.New("runtime-451 prune set has no eligible neuron")
}

// Runtime 451 defines Balance as a fixed-width u64 in rao. Decode the
// metadata constant exactly so a runtime type or unit change cannot silently
// alter EVM mirror-account funding.
func decodeRuntimeExistentialDepositRao(raw []byte) (uint64, error) {
	if len(raw) != 8 {
		return 0, fmt.Errorf("Balances.ExistentialDeposit SCALE length is %d, want 8", len(raw))
	}
	value := binary.LittleEndian.Uint64(raw)
	if value == 0 {
		return 0, errors.New("Balances.ExistentialDeposit is zero")
	}
	return value, nil
}

// Finalized activation facts distinguish the atomic-transfer toggle from the
// one-time Dynamic TAO token activation required before staking.
type SubnetActivationState struct {
	SubtokenEnabled          bool
	NetworkRegisteredAt      uint64
	StartCallDelay           uint64
	FirstEmissionBlockNumber uint64
	FinalizedBlock           uint64
}

const stakingPrecompileABI = `[{"type":"function","name":"getStake","inputs":[{"name":"hotkey","type":"bytes32"},{"name":"coldkey","type":"bytes32"},{"name":"netuid","type":"uint256"}],"outputs":[{"name":"","type":"uint256"}],"stateMutability":"view"},{"type":"function","name":"getNominatorMinRequiredStake","inputs":[],"outputs":[{"name":"","type":"uint256"}],"stateMutability":"view"}]`

var stakingPrecompileAddress = common.HexToAddress("0x0000000000000000000000000000000000000805")

// ReadSetupFacts selects the largest finalized alpha position controlled by
// the supplied testnet wallet. A single source position is intentional: each
// planned transfer is then atomic, bounded, and trivially resumable.
func ReadSetupFacts(ctx context.Context, cfg *ResolvedConfig) (*SetupFacts, error) {
	chain, err := crv4.DialChain(cfg.OperationalSubstrate)
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
	existentialDeposit, err := chain.Meta.FindConstantValue("Balances", "ExistentialDeposit")
	if err != nil {
		return nil, fmt.Errorf("read Balances.ExistentialDeposit metadata constant: %w", err)
	}
	facts.ExistentialDepositRao, err = decodeRuntimeExistentialDepositRao(existentialDeposit)
	if err != nil {
		return nil, err
	}
	registrationEconomics, err := readRegistrationEconomicsAt(chain, cfg.Netuid, finalizedHash)
	if err != nil {
		return nil, fmt.Errorf("read registration economics at finalized block %d: %w", facts.FinalizedBlock, err)
	}
	facts.BurnRao = registrationEconomics.BurnRao
	facts.MinBurnRao = registrationEconomics.MinBurnRao
	facts.MaxBurnRao = registrationEconomics.MaxBurnRao
	facts.BurnHalfLifeBlocks = registrationEconomics.BurnHalfLifeBlocks
	facts.BurnIncreaseMultQ64 = registrationEconomics.BurnIncreaseMultQ64
	topology, err := readSubnetTopologyAt(chain, cfg.Netuid, finalizedHash)
	if err != nil {
		return nil, err
	}
	facts.ExistingUIDCount = topology.UIDCount
	facts.SubnetOwnerHotkey = "0x" + fmt.Sprintf("%x", topology.OwnerHotkey)
	facts.UIDZeroHotkey = "0x" + fmt.Sprintf("%x", topology.UIDZero)
	facts.ExistingUIDs, err = readExistingUIDFactsAt(chain, cfg.Netuid, finalizedHash, topology)
	if err != nil {
		return nil, err
	}
	wallet, err := ss58.DecodeWithPrefix(cfg.WalletPublic, ss58.BittensorPrefix)
	if err != nil {
		return nil, fmt.Errorf("testnet wallet public key: %w", err)
	}
	accountKey, err := types.CreateStorageKey(chain.Meta, "System", "Account", wallet[:])
	if err != nil {
		return nil, err
	}
	var account subtensorAccountInfo
	if ok, readErr := chain.API.RPC.State.GetStorage(accountKey, &account, finalizedHash); readErr != nil {
		return nil, readErr
	} else if !ok {
		return nil, fmt.Errorf("testnet wallet free balance is unavailable")
	}
	facts.WalletFreeTAORao = uint64(account.Data.Free)
	hotkeysKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "StakingHotkeys", wallet[:])
	if err != nil {
		return nil, err
	}
	var hotkeys []types.AccountID
	if _, err := chain.API.RPC.State.GetStorage(hotkeysKey, &hotkeys, finalizedHash); err != nil {
		return nil, err
	}
	if len(hotkeys) == 0 {
		return facts, fmt.Errorf("testnet wallet has no staking hotkeys")
	}
	evm, err := dialConfiguredEVMClient(ctx, cfg, cfg.OperationalEVM)
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
		return facts, fmt.Errorf("testnet wallet has no alpha on netuid %d", cfg.Netuid)
	}
	return facts, nil
}

func DialSubstrateManager(cfg *ResolvedConfig, stateDir string, j *Journal) (*SubstrateManager, error) {
	chain, err := crv4.DialChain(cfg.OperationalSubstrate)
	if err != nil {
		return nil, err
	}
	if strings.ToLower(chain.GenesisHash.Hex()) != testnetGenesis || uint32(chain.Runtime.SpecVersion) != cfg.Public.Chain.ExpectedRuntimeSpec {
		chain.API.Client.Close()
		return nil, fmt.Errorf("operational RPC runtime identity mismatch")
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
	"burn_half_life":                {Storage: "BurnHalfLife", Call: "sudo_set_burn_half_life", Kind: "u16"},
	"tempo":                         {Storage: "Tempo", Call: "sudo_set_tempo", Kind: "u16"},
	"max_allowed_uids":              {Storage: "MaxAllowedUids", Call: "sudo_set_max_allowed_uids", Kind: "u16"},
	"mechanism_count":               {Storage: "MechanismCountCurrent", Call: "sudo_set_mechanism_count", Kind: "u8"},
	"commit_reveal_weights_enabled": {Storage: "CommitRevealWeightsEnabled", Call: "sudo_set_commit_reveal_weights_enabled", Kind: "bool"},
	"commit_reveal_period":          {Storage: "RevealPeriodEpochs", Call: "sudo_set_commit_reveal_weights_interval", Kind: "u64"},
	"liquid_alpha_enabled":          {Storage: "LiquidAlphaOn", Call: "sudo_set_liquid_alpha_enabled", Kind: "bool"},
	"immunity_period":               {Storage: "ImmunityPeriod", Call: "sudo_set_immunity_period", Kind: "u16"},
	"min_allowed_weights":           {Storage: "MinAllowedWeights", Call: "sudo_set_min_allowed_weights", Kind: "u16"},
	"weights_version_key":           {Storage: "WeightsVersionKey", Call: "sudo_set_weights_version_key", Kind: "u64"},
	"serving_rate_limit":            {Storage: "ServingRateLimit", Call: "sudo_set_serving_rate_limit", Kind: "u64"},
	"transfer_enabled":              {Storage: "TransferToggle", Call: "sudo_set_toggle_transfer", Kind: "bool"},
}

func netuidArg(n uint16) []byte { var b [2]byte; binary.LittleEndian.PutUint16(b[:], n); return b[:] }

// Decode a runtime-declared ValueQuery fallback when no raw key exists. A
// missing OptionalQuery remains absent; inventing a zero value there would
// hide missing identities and subnet state.
func decodeStorageFallback(entry types.StorageEntryMetadata, value any) (bool, error) {
	entryV14, ok := entry.(types.StorageEntryMetadataV14)
	if !ok {
		return false, fmt.Errorf("runtime storage entry is not metadata v14")
	}
	if !entryV14.Modifier.IsDefault {
		return false, nil
	}
	if err := codec.Decode(entryV14.Fallback, value); err != nil {
		return false, fmt.Errorf("decode runtime storage fallback: %w", err)
	}
	return true, nil
}

// Read one finalized value with the same absent-key semantics the runtime
// applies. GSRPC reports only raw key presence and otherwise leaves zeroes.
func readStorageAt(chain *crv4.Chain, key types.StorageKey, pallet, storage string, value any, blockHash types.Hash) (bool, error) {
	present, err := chain.API.RPC.State.GetStorage(key, value, blockHash)
	if err != nil || present {
		return present, err
	}
	entry, err := chain.Meta.FindStorageEntryMetadata(pallet, storage)
	if err != nil {
		return false, err
	}
	return decodeStorageFallback(entry, value)
}

// Require a concrete value or a runtime-declared ValueQuery fallback. Owner
// settings and activation prerequisites must never interpret absent optional
// storage as a real zero/false value.
func readRequiredStorageAt(chain *crv4.Chain, key types.StorageKey, pallet, storage string, value any, blockHash types.Hash) error {
	present, err := readStorageAt(chain, key, pallet, storage, value, blockHash)
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("%s.%s storage is absent", pallet, storage)
	}
	return nil
}

func readSubnetTopologyAt(chain *crv4.Chain, netuid uint16, finalized types.Hash) (SubnetTopologyFacts, error) {
	var result SubnetTopologyFacts
	countKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "SubnetworkN", netuidArg(netuid))
	if err != nil {
		return result, err
	}
	var count types.U16
	if err := readRequiredStorageAt(chain, countKey, crv4.PalletName, "SubnetworkN", &count, finalized); err != nil {
		return result, err
	}
	if count == 0 {
		return result, fmt.Errorf("SubnetworkN is zero for netuid %d", netuid)
	}
	ownerKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "SubnetOwnerHotkey", netuidArg(netuid))
	if err != nil {
		return result, err
	}
	var owner types.AccountID
	if err := readRequiredStorageAt(chain, ownerKey, crv4.PalletName, "SubnetOwnerHotkey", &owner, finalized); err != nil {
		return result, err
	}
	uidZeroKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "Keys", netuidArg(netuid), netuidArg(0))
	if err != nil {
		return result, err
	}
	var uidZero types.AccountID
	if err := readRequiredStorageAt(chain, uidZeroKey, crv4.PalletName, "Keys", &uidZero, finalized); err != nil {
		return result, err
	}
	result.UIDCount = uint16(count)
	copy(result.OwnerHotkey[:], owner[:])
	copy(result.UIDZero[:], uidZero[:])
	return result, nil
}

func readExistingUIDFactsAt(chain *crv4.Chain, netuid uint16, finalized types.Hash, topology SubnetTopologyFacts) ([]ExistingUIDFact, error) {
	result := make([]ExistingUIDFact, 0, topology.UIDCount)
	for uid := uint16(0); uid < topology.UIDCount; uid++ {
		uidBytes := netuidArg(uid)
		hotkeyKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "Keys", netuidArg(netuid), uidBytes)
		if err != nil {
			return nil, err
		}
		var hotkey types.AccountID
		if err := readRequiredStorageAt(chain, hotkeyKey, crv4.PalletName, "Keys", &hotkey, finalized); err != nil {
			return nil, err
		}
		ownerKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "Owner", hotkey[:])
		if err != nil {
			return nil, err
		}
		var coldkey types.AccountID
		if err := readRequiredStorageAt(chain, ownerKey, crv4.PalletName, "Owner", &coldkey, finalized); err != nil {
			return nil, err
		}
		registrationKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "BlockAtRegistration", netuidArg(netuid), uidBytes)
		if err != nil {
			return nil, err
		}
		var registrationBlock types.U64
		if err := readRequiredStorageAt(chain, registrationKey, crv4.PalletName, "BlockAtRegistration", &registrationBlock, finalized); err != nil {
			return nil, err
		}
		var key [32]byte
		copy(key[:], hotkey[:])
		result = append(result, ExistingUIDFact{
			UID: uid, Hotkey: "0x" + fmt.Sprintf("%x", hotkey), Coldkey: "0x" + fmt.Sprintf("%x", coldkey),
			RegistrationBlock: uint64(registrationBlock), SubnetOwner: key == topology.OwnerHotkey,
		})
	}
	return result, nil
}

func (m *SubstrateManager) SubnetTopology() (SubnetTopologyFacts, error) {
	finalized, _, err := m.finalizedHead()
	if err != nil {
		return SubnetTopologyFacts{}, err
	}
	return readSubnetTopologyAt(m.chain, m.cfg.Netuid, finalized)
}

func (m *SubstrateManager) ExistingUIDFacts() ([]ExistingUIDFact, error) {
	finalized, _, err := m.finalizedHead()
	if err != nil {
		return nil, err
	}
	topology, err := readSubnetTopologyAt(m.chain, m.cfg.Netuid, finalized)
	if err != nil {
		return nil, err
	}
	return readExistingUIDFactsAt(m.chain, m.cfg.Netuid, finalized, topology)
}

// Runtime451PruneCandidate mirrors the pinned runtime's emission,
// registration-age, immunity, and UID tie breakers at one finalized state.
// The fresh-plan invariant proves the only subnet-owner-owned live identity is
// SubnetOwnerHotkey, so it is the sole immortal entry in this release topology.
func (m *SubstrateManager) Runtime451PruneCandidate() (uint16, error) {
	finalized, block, err := m.finalizedHead()
	if err != nil {
		return 0, err
	}
	topology, err := readSubnetTopologyAt(m.chain, m.cfg.Netuid, finalized)
	if err != nil {
		return 0, err
	}
	readU16 := func(storage string) (uint16, error) {
		key, keyErr := types.CreateStorageKey(m.chain.Meta, crv4.PalletName, storage, netuidArg(m.cfg.Netuid))
		if keyErr != nil {
			return 0, keyErr
		}
		var value types.U16
		if readErr := readRequiredStorageAt(m.chain, key, crv4.PalletName, storage, &value, finalized); readErr != nil {
			return 0, readErr
		}
		return uint16(value), nil
	}
	immunityPeriod, err := readU16("ImmunityPeriod")
	if err != nil {
		return 0, err
	}
	minimumNonImmune, err := readU16("MinNonImmuneUids")
	if err != nil {
		return 0, err
	}
	emissionKey, err := types.CreateStorageKey(m.chain.Meta, crv4.PalletName, "Emission", netuidArg(m.cfg.Netuid))
	if err != nil {
		return 0, err
	}
	var emissions []types.U64
	if _, err := readStorageAt(m.chain, emissionKey, crv4.PalletName, "Emission", &emissions, finalized); err != nil {
		return 0, err
	}
	neurons := make([]runtime451PruneNeuron, 0, topology.UIDCount)
	for uid := uint16(0); uid < topology.UIDCount; uid++ {
		uidBytes := netuidArg(uid)
		hotkeyKey, keyErr := types.CreateStorageKey(m.chain.Meta, crv4.PalletName, "Keys", netuidArg(m.cfg.Netuid), uidBytes)
		if keyErr != nil {
			return 0, keyErr
		}
		var hotkey types.AccountID
		if readErr := readRequiredStorageAt(m.chain, hotkeyKey, crv4.PalletName, "Keys", &hotkey, finalized); readErr != nil {
			return 0, readErr
		}
		registrationKey, keyErr := types.CreateStorageKey(m.chain.Meta, crv4.PalletName, "BlockAtRegistration", netuidArg(m.cfg.Netuid), uidBytes)
		if keyErr != nil {
			return 0, keyErr
		}
		var registered types.U64
		if readErr := readRequiredStorageAt(m.chain, registrationKey, crv4.PalletName, "BlockAtRegistration", &registered, finalized); readErr != nil {
			return 0, readErr
		}
		var key [32]byte
		copy(key[:], hotkey[:])
		emission := uint64(0)
		if int(uid) < len(emissions) {
			emission = uint64(emissions[uid])
		}
		registrationBlock := uint64(registered)
		age := uint64(0)
		if block >= registrationBlock {
			age = block - registrationBlock
		}
		neurons = append(neurons, runtime451PruneNeuron{
			UID: uid, Hotkey: key, EmissionRao: emission, RegistrationBlock: registrationBlock,
			Immune: age < uint64(immunityPeriod), Immortal: key == topology.OwnerHotkey,
		})
	}
	return runtime451PruneCandidate(neurons, minimumNonImmune)
}

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

// waitForFinalizedCheckpoint closes a real RPC race: a subscription may emit
// a finalized extrinsic before a separately served chain_getFinalizedHead has
// advanced to that block. Native post-state reads must not run until the
// canonical finalized read surface can prove the transaction block.
func waitForFinalizedCheckpoint(ctx context.Context, target ChainHead, interval time.Duration, observe func() (ChainHead, string, error)) error {
	if target.Number == 0 || target.Hash == "" || interval <= 0 || observe == nil {
		return errors.New("finalized checkpoint wait configuration is incomplete")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastErr error
	for {
		head, canonical, err := observe()
		if err == nil {
			lastErr = nil
			ready, checkErr := checkpointVisibility(target, head, canonical)
			if checkErr != nil {
				return checkErr
			}
			if ready {
				return nil
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("finalized checkpoint %d was not readable: %v: %w", target.Number, lastErr, ctx.Err())
			}
			return fmt.Errorf("finalized checkpoint %d was not readable: %w", target.Number, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (m *SubstrateManager) waitForFinalizedReadCheckpoint(ctx context.Context, block uint64, hash types.Hash) error {
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	target := ChainHead{Number: block, Hash: hash.Hex()}
	return waitForFinalizedCheckpoint(waitCtx, target, 500*time.Millisecond, func() (ChainHead, string, error) {
		finalizedHash, finalizedBlock, err := m.finalizedHead()
		if err != nil {
			return ChainHead{}, "", err
		}
		head := ChainHead{Number: finalizedBlock, Hash: finalizedHash.Hex()}
		if finalizedBlock < block {
			return head, "", nil
		}
		canonical, err := m.chain.API.RPC.Chain.GetBlockHash(block)
		if err != nil {
			return head, "", err
		}
		return head, canonical.Hex(), nil
	})
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
		err = readRequiredStorageAt(m.chain, key, crv4.PalletName, s.Storage, &v, finalized)
		return uint8(v), err
	case "u16":
		var v types.U16
		err = readRequiredStorageAt(m.chain, key, crv4.PalletName, s.Storage, &v, finalized)
		return uint16(v), err
	case "u64":
		var v types.U64
		err = readRequiredStorageAt(m.chain, key, crv4.PalletName, s.Storage, &v, finalized)
		return uint64(v), err
	case "bool":
		var v types.Bool
		err = readRequiredStorageAt(m.chain, key, crv4.PalletName, s.Storage, &v, finalized)
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
	finalized, _, err := m.finalizedHead()
	if err != nil {
		return 0, err
	}
	return m.freeBalanceAtHash(account, finalized)
}

// FreeBalanceAtBlock reads the same native block used by an EVM balance
// query. This prevents independently fronted RPC endpoints from producing a
// mixed-height funding decision.
func (m *SubstrateManager) FreeBalanceAtBlock(account [32]byte, block uint64) (uint64, error) {
	_, finalizedBlock, err := m.finalizedHead()
	if err != nil {
		return 0, err
	}
	if block > finalizedBlock {
		return 0, fmt.Errorf("native finalized head %d is behind requested EVM block %d", finalizedBlock, block)
	}
	hash, err := m.chain.API.RPC.Chain.GetBlockHash(block)
	if err != nil {
		return 0, err
	}
	return m.freeBalanceAtHash(account, hash)
}

func (m *SubstrateManager) freeBalanceAtHash(account [32]byte, finalized types.Hash) (uint64, error) {
	key, err := types.CreateStorageKey(m.chain.Meta, "System", "Account", account[:])
	if err != nil {
		return 0, err
	}
	var info subtensorAccountInfo
	ok, err := m.chain.API.RPC.State.GetStorage(key, &info, finalized)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	return uint64(info.Data.Free), nil
}

func (m *SubstrateManager) Send(ctx context.Context, planHash string, a Action, call types.Call) (types.Hash, uint64, error) {
	return m.SendAs(ctx, planHash, a, call, m.signer)
}

func (m *SubstrateManager) SendAs(ctx context.Context, planHash string, a Action, call types.Call, signer signature.KeyringPair) (types.Hash, uint64, error) {
	if prior, ok := m.journal.LatestTransaction(planHash, a.ID, a.IntentHash); ok {
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
		return m.watchRaw(ctx, planHash, a, raw, hash, prior.RecoveryBlock, prior.RecoveryBlockHash, false)
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
	feeEstimate, feeLimit, err := m.approveNativeTransactionFee(ctx, raw)
	if err != nil {
		return types.Hash{}, 0, err
	}
	path := filepath.Join(m.stateDir, "transactions", stringsTrim0x(hash.Hex())+".scale")
	if err := atomicWrite(path, raw, 0o600); err != nil {
		return types.Hash{}, 0, err
	}
	recoveryHash, recoveryBlock, err := m.finalizedHead()
	if err != nil {
		return types.Hash{}, 0, err
	}
	if err := m.journal.Append(JournalEntry{DeploymentID: m.cfg.Config.Deployment.DeploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageBroadcast, Signer: signer.Address, Nonce: strconv.FormatUint(uint64(nonce), 10), TransactionHash: hash.Hex(), RecoveryBlock: recoveryBlock, RecoveryBlockHash: recoveryHash.Hex(), FeeEstimateRao: feeEstimate, FeeLimitRao: feeLimit}); err != nil {
		return types.Hash{}, 0, err
	}
	return m.watchRaw(ctx, planHash, a, raw, hash, recoveryBlock, recoveryHash.Hex(), true)
}

func (m *SubstrateManager) RoleSigner(roles *RoleSecrets, label string) (signature.KeyringPair, error) {
	v, ok := roles.Substrate[label]
	if !ok {
		return signature.KeyringPair{}, fmt.Errorf("missing substrate role %s", label)
	}
	return signature.KeyringPairFromSecret("0x"+v.SeedHex, 42)
}

// Build a runtime-enforced registration limit so a moving burn auction cannot
// charge more than the approved action ceiling between observation and block.
func (m *SubstrateManager) BurnRegisterLimitCall(hotkey [32]byte, limitPrice uint64) (types.Call, error) {
	account, err := types.NewAccountID(hotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	return types.NewCall(m.chain.Meta, crv4.PalletName+".register_limit", types.NewU16(m.cfg.Netuid), *account, types.NewU64(limitPrice))
}

// Build a fill-or-kill Dynamic TAO purchase with an explicit maximum price.
func (self *SubstrateManager) AddStakeLimitCall(hotkey [32]byte, amount, limitPrice uint64, allowPartial bool) (types.Call, error) {
	account, err := types.NewAccountID(hotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	return types.NewCall(
		self.chain.Meta,
		crv4.PalletName+".add_stake_limit",
		*account,
		types.NewU16(self.cfg.Netuid),
		types.NewU64(amount),
		types.NewU64(limitPrice),
		types.NewBool(allowPartial),
	)
}

// Read all activation prerequisites and results from one finalized head.
func (self *SubstrateManager) ActivationState() (SubnetActivationState, error) {
	finalized, block, err := self.finalizedHead()
	if err != nil {
		return SubnetActivationState{}, err
	}
	state := SubnetActivationState{FinalizedBlock: block}
	readNetuid := func(storage string, out any) (bool, error) {
		key, keyErr := types.CreateStorageKey(self.chain.Meta, crv4.PalletName, storage, netuidArg(self.cfg.Netuid))
		if keyErr != nil {
			return false, keyErr
		}
		return readStorageAt(self.chain, key, crv4.PalletName, storage, out, finalized)
	}
	var enabled types.Bool
	if present, err := readNetuid("SubtokenEnabled", &enabled); err != nil {
		return state, err
	} else if !present {
		return state, errors.New("SubtensorModule.SubtokenEnabled storage is absent")
	}
	state.SubtokenEnabled = bool(enabled)
	var registered types.U64
	if present, err := readNetuid("NetworkRegisteredAt", &registered); err != nil {
		return state, err
	} else if !present {
		return state, errors.New("SubtensorModule.NetworkRegisteredAt storage is absent")
	}
	state.NetworkRegisteredAt = uint64(registered)
	delayKey, err := types.CreateStorageKey(self.chain.Meta, crv4.PalletName, "StartCallDelay")
	if err != nil {
		return state, err
	}
	var delay types.U64
	if err := readRequiredStorageAt(self.chain, delayKey, crv4.PalletName, "StartCallDelay", &delay, finalized); err != nil {
		return state, err
	}
	state.StartCallDelay = uint64(delay)
	var first types.U64
	if present, err := readNetuid("FirstEmissionBlockNumber", &first); err != nil {
		return state, err
	} else if present {
		state.FirstEmissionBlockNumber = uint64(first)
	}
	return state, nil
}

// Build the one-time subnet-token activation call after its delay expires.
func (self *SubstrateManager) StartCallCall() (types.Call, error) {
	return types.NewCall(self.chain.Meta, crv4.PalletName+".start_call", types.NewU16(self.cfg.Netuid))
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
	if _, err := readStorageAt(m.chain, key, crv4.PalletName, "Delegates", &take, finalized); err != nil {
		return 0, err
	}
	return uint16(take), nil
}

func (m *SubstrateManager) DecreaseTakeCall(hotkey [32]byte, take uint16) (types.Call, error) {
	account, err := types.NewAccountID(hotkey[:])
	if err != nil {
		return types.Call{}, err
	}
	// Runtime v451 encodes sp_runtime::PerU16 exactly as its u16 parts.
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

func (m *SubstrateManager) UIDCount() (uint16, error) {
	key, err := types.CreateStorageKey(m.chain.Meta, crv4.PalletName, "SubnetworkN", netuidArg(m.cfg.Netuid))
	if err != nil {
		return 0, err
	}
	var count types.U16
	finalized, _, err := m.finalizedHead()
	if err != nil {
		return 0, err
	}
	ok, err := m.chain.API.RPC.State.GetStorage(key, &count, finalized)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("SubnetworkN is absent for netuid %d", m.cfg.Netuid)
	}
	return uint16(count), nil
}

// Read the controlling coldkey for a hotkey from the same finalized runtime
// storage used by registration. Owner is a ValueQuery; an unowned hotkey
// resolves to the runtime's zero-account fallback and will fail exact matching.
func (m *SubstrateManager) HotkeyOwner(hotkey [32]byte) ([32]byte, error) {
	var result [32]byte
	key, err := types.CreateStorageKey(m.chain.Meta, crv4.PalletName, "Owner", hotkey[:])
	if err != nil {
		return result, err
	}
	finalized, _, err := m.finalizedHead()
	if err != nil {
		return result, err
	}
	var owner types.AccountID
	if err := readRequiredStorageAt(m.chain, key, crv4.PalletName, "Owner", &owner, finalized); err != nil {
		return result, err
	}
	return [32]byte(owner), nil
}

func validateHotkeyOwner(label string, actual, expected [32]byte) error {
	if actual != expected {
		return fmt.Errorf("%s coldkey is 0x%x, want 0x%x", label, actual, expected)
	}
	return nil
}

func (m *SubstrateManager) appendRecoveredFinality(ctx context.Context, planHash string, a Action, hash types.Hash, receipt *crv4.FinalizedExtrinsic, recoveryBlock uint64, recoveryHash string) (types.Hash, uint64, error) {
	if err := m.journal.Append(JournalEntry{DeploymentID: m.cfg.Config.Deployment.DeploymentID, PlanHash: planHash, ActionID: a.ID, IntentHash: a.IntentHash, Stage: StageFinalized, TransactionHash: hash.Hex(), BlockNumber: receipt.BlockNumber, BlockHash: receipt.BlockHash.Hex(), RecoveryBlock: recoveryBlock, RecoveryBlockHash: recoveryHash}); err != nil {
		return hash, 0, err
	}
	if err := m.waitForFinalizedReadCheckpoint(ctx, receipt.BlockNumber, receipt.BlockHash); err != nil {
		return hash, 0, err
	}
	return hash, receipt.BlockNumber, nil
}

func (m *SubstrateManager) watchRaw(ctx context.Context, planHash string, a Action, raw []byte, hash types.Hash, recoveryBlock uint64, recoveryHash string, feeChecked bool) (types.Hash, uint64, error) {
	if receipt, found, err := m.chain.FindFinalizedExtrinsic(ctx, hash, recoveryBlock); err != nil {
		return hash, 0, err
	} else if found {
		return m.appendRecoveredFinality(ctx, planHash, a, hash, receipt, recoveryBlock, recoveryHash)
	}
	if !feeChecked {
		if _, _, err := m.approveNativeTransactionFee(ctx, raw); err != nil {
			return hash, 0, err
		}
	}
	statuses := make(chan types.ExtrinsicStatus)
	sub, err := m.chain.API.Client.Subscribe(ctx, "author", "submitAndWatchExtrinsic", "unwatchExtrinsic", "extrinsicUpdate", statuses, codec.HexEncodeToString(raw))
	if err != nil {
		if receipt, found, scanErr := m.chain.FindFinalizedExtrinsic(ctx, hash, recoveryBlock); scanErr == nil && found {
			return m.appendRecoveredFinality(ctx, planHash, a, hash, receipt, recoveryBlock, recoveryHash)
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
				if err := m.waitForFinalizedReadCheckpoint(ctx, n, status.AsFinalized); err != nil {
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
			present, err = readStorageAt(chain, key, crv4.PalletName, gate.Storage, &value, finalized)
			observed = []uint64{uint64(value)}
		case "u64":
			var value types.U64
			present, err = readStorageAt(chain, key, crv4.PalletName, gate.Storage, &value, finalized)
			observed = []uint64{uint64(value)}
		case "u16_pair":
			var value struct {
				Low  types.U16
				High types.U16
			}
			present, err = readStorageAt(chain, key, crv4.PalletName, gate.Storage, &value, finalized)
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
