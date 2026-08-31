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

// subtensorAccountInfo matches runtime 452's System.Account value. Subtensor's
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
	BurnRao                      uint64            `json:"burn_rao"`
	MinBurnRao                   uint64            `json:"min_burn_rao,omitempty"`
	MaxBurnRao                   uint64            `json:"max_burn_rao,omitempty"`
	BurnHalfLifeBlocks           uint16            `json:"burn_half_life_blocks,omitempty"`
	BurnIncreaseMultQ64          string            `json:"burn_increase_mult_q64,omitempty"`
	AlphaSourceHotkey            string            `json:"alpha_source_hotkey"`
	AlphaAvailableRao            uint64            `json:"alpha_available_rao"`
	AlphaTransferableRao         uint64            `json:"alpha_transferable_rao"`
	AlphaSourceStoredLockRao     uint64            `json:"alpha_source_stored_lock_rao"`
	AlphaSourceCollateralRao     uint64            `json:"alpha_source_collateral_rao"`
	WalletNetuidAlphaRao         uint64            `json:"wallet_netuid_alpha_rao"`
	WalletNetuidCollateralRao    uint64            `json:"wallet_netuid_collateral_rao"`
	ExistingUIDCount             uint16            `json:"existing_uid_count"`
	SubnetOwnerHotkey            string            `json:"subnet_owner_hotkey"`
	UIDZeroHotkey                string            `json:"uid_zero_hotkey"`
	ExistingUIDs                 []ExistingUIDFact `json:"existing_uids"`
	ExistentialDepositRao        uint64            `json:"existential_deposit_rao"`
	InitialMinStakeRao           uint64            `json:"initial_min_stake_rao"`
	DefaultMinTransferRao        uint64            `json:"default_min_transfer_rao"`
	AlphaPriceQ9                 uint64            `json:"alpha_price_tao_per_alpha_q9"`
	RegisteredAlphaRao           uint64            `json:"registered_alpha_rao"`
	ReserveValidatorAlphaRao     uint64            `json:"reserve_validator_alpha_rao"`
	IndependentValidatorAlphaRao uint64            `json:"independent_validator_alpha_rao"`
	AlphaSourceRegistered        bool              `json:"alpha_source_registered"`
	NominatorMinimumRao          uint64            `json:"nominator_minimum_rao"`
	ProbeTAORao                  uint64            `json:"probe_tao_rao"`
	WalletFreeTAORao             uint64            `json:"wallet_free_tao_rao"`
	DeployerNonce                uint64            `json:"deployer_nonce"`
	FinalizedBlock               uint64            `json:"finalized_block"`
	FinalizedBlockHash           string            `json:"finalized_block_hash"`
	EVMFinalizedBlock            uint64            `json:"evm_finalized_block"`
	EVMFinalizedBlockHash        string            `json:"evm_finalized_block_hash"`
}

type ExistingUIDFact struct {
	UID                 uint16 `json:"uid"`
	Hotkey              string `json:"hotkey"`
	Coldkey             string `json:"coldkey"`
	RegistrationBlock   uint64 `json:"registration_block"`
	SubnetOwner         bool   `json:"subnet_owner"`
	TotalHotkeyAlphaRao uint64 `json:"total_hotkey_alpha_rao,omitempty"`
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

type RegisteredAlphaSnapshot struct {
	FinalizedBlock uint64
	FinalizedHash  string
	TotalAlphaRao  uint64
	ByHotkey       map[[32]byte]uint64
}

// Captures every runtime restriction which can prevent a same-subnet,
// ownership-changing transfer from the approved source position. Stored lock
// mass is intentionally not rolled forward: treating any decay since the
// checkpoint as still locked is conservative and prevents the harness from
// moving conviction or requiring the destination to accept locked alpha.
type AlphaTransferSourceRestrictions struct {
	FinalizedBlock        uint64
	FinalizedHash         string
	StakingHotkeys        [][32]byte
	StoredLockRao         uint64
	PositionCollateralRao uint64
	ColdkeyCollateralRao  uint64
}

type runtime452PruneNeuron struct {
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

func runtime452PruneCandidate(neurons []runtime452PruneNeuron, minimumNonImmune uint16) (uint16, error) {
	var bestImmune *runtime452PruneNeuron
	var bestNonImmune *runtime452PruneNeuron
	var nonImmune uint16
	better := func(candidate runtime452PruneNeuron, current *runtime452PruneNeuron) bool {
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
	return 0, errors.New("runtime-452 prune set has no eligible neuron")
}

// Runtime 452 defines Balance as a fixed-width u64 in rao. Decode the
// metadata constant exactly so a runtime type or unit change cannot silently
// alter EVM mirror-account funding.
func decodeRuntimeExistentialDepositRao(raw []byte) (uint64, error) {
	return decodeRuntimeRaoConstant("Balances.ExistentialDeposit", raw)
}

func decodeRuntimeInitialMinStakeRao(raw []byte) (uint64, error) {
	return decodeRuntimeRaoConstant("SubtensorModule.InitialMinStake", raw)
}

func decodeRuntimeInitialMinTransferRao(raw []byte) (uint64, error) {
	return decodeRuntimeRaoConstant("SubtensorModule.InitialMinTransfer", raw)
}

type metadataConstantReader interface {
	FindConstantValue(module, constant string) ([]byte, error)
}

// DefaultMinTransfer is a pallet type-value function, not an on-chain storage
// item. Runtime 452 defines it from the InitialMinTransfer Config constant, so
// the authoritative public value is the exact finalized block's metadata
// constant. Do not fall back to similarly named storage or a local default.
func runtimeDefaultMinTransferMetadataValue(metadata metadataConstantReader) ([]byte, error) {
	value, err := metadata.FindConstantValue(crv4.PalletName, "InitialMinTransfer")
	if err != nil {
		return nil, fmt.Errorf("read %s.InitialMinTransfer metadata constant: %w", crv4.PalletName, err)
	}
	return value, nil
}

func validateRuntimeDefaultMinTransferBinding(raw []byte, observedCodeHash, expectedCodeHash string, expectedRao uint64) (uint64, error) {
	if err := validateRuntimeCodeHash(observedCodeHash, expectedCodeHash); err != nil {
		return 0, fmt.Errorf("bind DefaultMinTransfer to finalized runtime: %w", err)
	}
	value, err := decodeRuntimeInitialMinTransferRao(raw)
	if err != nil {
		return 0, err
	}
	if expectedRao == 0 {
		return 0, errors.New("reviewed DefaultMinTransfer is zero")
	}
	if value != expectedRao {
		return 0, fmt.Errorf(
			"finalized runtime DefaultMinTransfer is %d TAO rao, reviewed manifest requires %d",
			value,
			expectedRao,
		)
	}
	return value, nil
}

func runtimeCodeHashAt(chain *crv4.Chain, finalized types.Hash) (string, error) {
	var codeHash string
	if err := chain.API.Client.Call(&codeHash, "state_getStorageHash", "0x3a636f6465", finalized.Hex()); err != nil {
		return "", err
	}
	if err := validateRuntimeCodeHash(codeHash, codeHash); err != nil {
		return "", err
	}
	return strings.ToLower(codeHash), nil
}

type authenticatedRuntimeMetadata struct {
	Metadata *types.Metadata
	CodeHash string
}

func readAuthenticatedRuntimeMetadataAt(chain *crv4.Chain, cfg *ResolvedConfig, finalized types.Hash) (authenticatedRuntimeMetadata, error) {
	if cfg == nil || cfg.Release == nil {
		return authenticatedRuntimeMetadata{}, errors.New("release lock is unavailable")
	}
	codeHash, err := runtimeCodeHashAt(chain, finalized)
	if err != nil {
		return authenticatedRuntimeMetadata{}, fmt.Errorf("read finalized runtime code hash: %w", err)
	}
	if err := validateRuntimeCodeHash(codeHash, cfg.Release.Runtime.CodeHash); err != nil {
		return authenticatedRuntimeMetadata{}, err
	}
	metadata, err := chain.API.RPC.State.GetMetadata(finalized)
	if err != nil {
		return authenticatedRuntimeMetadata{}, fmt.Errorf("read finalized runtime metadata: %w", err)
	}
	return authenticatedRuntimeMetadata{Metadata: metadata, CodeHash: codeHash}, nil
}

func readRuntimeDefaultMinTransferAt(chain *crv4.Chain, cfg *ResolvedConfig, finalized types.Hash) (uint64, error) {
	authenticated, err := readAuthenticatedRuntimeMetadataAt(chain, cfg, finalized)
	if err != nil {
		return 0, err
	}
	raw, err := runtimeDefaultMinTransferMetadataValue(authenticated.Metadata)
	if err != nil {
		return 0, err
	}
	return validateRuntimeDefaultMinTransferBinding(
		raw,
		authenticated.CodeHash,
		cfg.Release.Runtime.CodeHash,
		cfg.Public.Chain.ExpectedDefaultMinTransferRao,
	)
}

func decodeRuntimeRaoConstant(name string, raw []byte) (uint64, error) {
	if len(raw) != 8 {
		return 0, fmt.Errorf("%s SCALE length is %d, want 8", name, len(raw))
	}
	value := binary.LittleEndian.Uint64(raw)
	if value == 0 {
		return 0, fmt.Errorf("%s is zero", name)
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
const alphaPricePrecompileABI = `[{"type":"function","name":"getAlphaPrice","inputs":[{"name":"netuid","type":"uint16"}],"outputs":[{"name":"","type":"uint256"}],"stateMutability":"view"}]`

var stakingPrecompileAddress = common.HexToAddress("0x0000000000000000000000000000000000000805")
var alphaPricePrecompileAddress = common.HexToAddress("0x0000000000000000000000000000000000000808")

// Runtime 452's alpha precompile returns the Q9 price after the chain's
// rao-to-EVM-wei converter applies a second 1e9 factor. Reject any unexpected
// precision or shape instead of silently guessing units.
func decodeAlphaPriceQ9(value *big.Int) (uint64, error) {
	if value == nil || value.Sign() <= 0 {
		return 0, errors.New("alpha precompile price is zero or unavailable")
	}
	q9, remainder := new(big.Int), new(big.Int)
	q9.QuoRem(value, new(big.Int).SetUint64(alphaPriceQ9Scale), remainder)
	if remainder.Sign() != 0 {
		return 0, fmt.Errorf("alpha precompile price %s is not an exact Q9 rao value", value)
	}
	if !q9.IsUint64() || q9.Sign() <= 0 {
		return 0, errors.New("alpha precompile Q9 price exceeds uint64")
	}
	return q9.Uint64(), nil
}

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
	authenticated, err := readAuthenticatedRuntimeMetadataAt(chain, cfg, finalizedHash)
	if err != nil {
		return nil, fmt.Errorf("authenticate finalized runtime metadata: %w", err)
	}
	existentialDeposit, err := authenticated.Metadata.FindConstantValue("Balances", "ExistentialDeposit")
	if err != nil {
		return nil, fmt.Errorf("read Balances.ExistentialDeposit metadata constant: %w", err)
	}
	facts.ExistentialDepositRao, err = decodeRuntimeExistentialDepositRao(existentialDeposit)
	if err != nil {
		return nil, err
	}
	initialMinStake, err := authenticated.Metadata.FindConstantValue(crv4.PalletName, "InitialMinStake")
	if err != nil {
		return nil, fmt.Errorf("read %s.InitialMinStake metadata constant: %w", crv4.PalletName, err)
	}
	facts.InitialMinStakeRao, err = decodeRuntimeInitialMinStakeRao(initialMinStake)
	if err != nil {
		return nil, err
	}
	initialMinTransfer, err := runtimeDefaultMinTransferMetadataValue(authenticated.Metadata)
	if err != nil {
		return nil, err
	}
	facts.DefaultMinTransferRao, err = validateRuntimeDefaultMinTransferBinding(
		initialMinTransfer,
		authenticated.CodeHash,
		cfg.Release.Runtime.CodeHash,
		cfg.Public.Chain.ExpectedDefaultMinTransferRao,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve finalized %s.DefaultMinTransfer: %w", crv4.PalletName, err)
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
	for _, existing := range facts.ExistingUIDs {
		registeredAlpha, addOK := checkedAdd(facts.RegisteredAlphaRao, existing.TotalHotkeyAlphaRao)
		if !addOK {
			return nil, errors.New("registered subnet alpha exceeds uint64")
		}
		facts.RegisteredAlphaRao = registeredAlpha
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
	evmHead, err := finalizedEVMHead(ctx, evm)
	if err != nil {
		return nil, fmt.Errorf("read finalized EVM setup head: %w", err)
	}
	facts.EVMFinalizedBlock = evmHead.Number
	facts.EVMFinalizedBlockHash = evmHead.Hash
	roles, err := BuildRoleSecrets(cfg)
	if err != nil {
		return nil, fmt.Errorf("derive setup deployer: %w", err)
	}
	deployer, err := roles.EVMAddress("deployer")
	if err != nil {
		return nil, err
	}
	facts.DeployerNonce, err = evm.NonceAt(ctx, deployer, new(big.Int).SetUint64(evmHead.Number))
	if err != nil {
		return nil, fmt.Errorf("read finalized deployer nonce: %w", err)
	}
	pendingNonce, err := evm.PendingNonceAt(ctx, deployer)
	if err != nil {
		return nil, fmt.Errorf("read pending deployer nonce: %w", err)
	}
	if pendingNonce != facts.DeployerNonce {
		return nil, fmt.Errorf("deployer has pending transactions: finalized nonce=%d pending nonce=%d", facts.DeployerNonce, pendingNonce)
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
		return nil, stateMismatchError(err, "decode nominator minimum returned %d values", len(minimumValues))
	}
	minimum, ok := minimumValues[0].(*big.Int)
	if !ok || !minimum.IsUint64() {
		return nil, fmt.Errorf("nominator minimum exceeds uint64")
	}
	facts.NominatorMinimumRao = minimum.Uint64()
	facts.ProbeTAORao = max64(facts.NominatorMinimumRao, 1_000)
	alphaPriceABI, err := abi.JSON(strings.NewReader(alphaPricePrecompileABI))
	if err != nil {
		return nil, err
	}
	priceData, err := alphaPriceABI.Pack("getAlphaPrice", cfg.Netuid)
	if err != nil {
		return nil, err
	}
	priceRaw, err := evm.CallContract(ctx, ethereum.CallMsg{To: &alphaPricePrecompileAddress, Data: priceData}, new(big.Int).SetUint64(facts.FinalizedBlock))
	if err != nil {
		return nil, fmt.Errorf("read finalized netuid %d alpha price: %w", cfg.Netuid, err)
	}
	priceValues, err := alphaPriceABI.Unpack("getAlphaPrice", priceRaw)
	if err != nil || len(priceValues) != 1 {
		return nil, stateMismatchError(err, "decode finalized alpha price returned %d values", len(priceValues))
	}
	priceWei, ok := priceValues[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("alpha price returned %T", priceValues[0])
	}
	facts.AlphaPriceQ9, err = decodeAlphaPriceQ9(priceWei)
	if err != nil {
		return nil, err
	}
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
			return nil, stateMismatchError(unpackErr, "decode alpha position 0x%x returned %d values", hotkey, len(values))
		}
		stake, ok := values[0].(*big.Int)
		if !ok || !stake.IsUint64() {
			return nil, fmt.Errorf("alpha position 0x%x exceeds uint64", hotkey)
		}
		walletAlpha, addOK := checkedAdd(facts.WalletNetuidAlphaRao, stake.Uint64())
		if !addOK {
			return nil, fmt.Errorf("wallet alpha on netuid %d exceeds uint64", cfg.Netuid)
		}
		facts.WalletNetuidAlphaRao = walletAlpha
		if stake.Uint64() > facts.AlphaAvailableRao {
			facts.AlphaAvailableRao = stake.Uint64()
			facts.AlphaSourceHotkey = "0x" + fmt.Sprintf("%x", hotkey)
		}
	}
	if facts.AlphaSourceHotkey == "" || facts.AlphaAvailableRao == 0 {
		return facts, fmt.Errorf("testnet wallet has no alpha on netuid %d", cfg.Netuid)
	}
	alphaSource, err := decodeHex32("alpha source hotkey", facts.AlphaSourceHotkey)
	if err != nil {
		return nil, err
	}
	var walletColdkey [32]byte
	copy(walletColdkey[:], wallet[:])
	restrictions, err := readAlphaTransferSourceRestrictionsAt(chain, cfg.Netuid, walletColdkey, alphaSource, finalizedHash, facts.FinalizedBlock)
	if err != nil {
		return nil, fmt.Errorf("read alpha source transfer restrictions: %w", err)
	}
	facts.AlphaSourceStoredLockRao = restrictions.StoredLockRao
	facts.AlphaSourceCollateralRao = restrictions.PositionCollateralRao
	facts.WalletNetuidCollateralRao = restrictions.ColdkeyCollateralRao
	facts.AlphaTransferableRao, err = alphaTransferCapacity(
		facts.AlphaAvailableRao,
		facts.WalletNetuidAlphaRao,
		facts.AlphaSourceStoredLockRao,
		facts.AlphaSourceCollateralRao,
		facts.WalletNetuidCollateralRao,
	)
	if err != nil {
		return nil, fmt.Errorf("derive alpha source transfer capacity: %w", err)
	}
	if facts.AlphaTransferableRao == 0 {
		return nil, errors.New("testnet wallet alpha source has no transferable alpha")
	}
	reserveHotkey, err := roleBytes32(roles, validatorHotkeyLabel(1))
	if err != nil {
		return nil, err
	}
	independentHotkey, err := roleBytes32(roles, validatorHotkeyLabel(2))
	if err != nil {
		return nil, err
	}
	for _, existing := range facts.ExistingUIDs {
		if strings.EqualFold(existing.Hotkey, facts.AlphaSourceHotkey) {
			facts.AlphaSourceRegistered = true
		}
		if strings.EqualFold(existing.Hotkey, "0x"+fmt.Sprintf("%x", reserveHotkey)) {
			facts.ReserveValidatorAlphaRao = existing.TotalHotkeyAlphaRao
		}
		if strings.EqualFold(existing.Hotkey, "0x"+fmt.Sprintf("%x", independentHotkey)) {
			facts.IndependentValidatorAlphaRao = existing.TotalHotkeyAlphaRao
		}
	}
	if !facts.AlphaSourceRegistered {
		return nil, errors.New("largest wallet alpha source is not a registered netuid hotkey")
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

const storageQueryChunkSize = 128

// Read a large finalized storage surface in bounded state_queryStorageAt
// batches. Sequential state_getStorage calls took long enough for the paired
// EVM checkpoint to expire on the public testnet RPC; the batch response is
// therefore validated for exact keys, uniqueness, and block identity.
func queryStorageAtExact(chain *crv4.Chain, keys []types.StorageKey, finalized types.Hash) (map[string]*types.StorageDataRaw, error) {
	if chain == nil || chain.API == nil || len(keys) == 0 {
		return nil, errors.New("finalized storage query requires a chain and keys")
	}
	result := make(map[string]*types.StorageDataRaw, len(keys))
	requested := make(map[string]bool, len(keys))
	for _, key := range keys {
		hexKey := key.Hex()
		if requested[hexKey] {
			return nil, fmt.Errorf("finalized storage query contains duplicate key %s", hexKey)
		}
		requested[hexKey] = true
	}
	for start := 0; start < len(keys); start += storageQueryChunkSize {
		end := min(start+storageQueryChunkSize, len(keys))
		sets, err := chain.API.RPC.State.QueryStorageAt(keys[start:end], finalized)
		if err != nil {
			return nil, err
		}
		chunk, err := validateStorageQueryChanges(keys[start:end], finalized, sets)
		if err != nil {
			return nil, err
		}
		for key, value := range chunk {
			result[key] = value
		}
	}
	if len(result) != len(keys) {
		return nil, fmt.Errorf("finalized storage query returned %d keys, want %d", len(result), len(keys))
	}
	return result, nil
}

func validateStorageQueryChanges(keys []types.StorageKey, finalized types.Hash, sets []types.StorageChangeSet) (map[string]*types.StorageDataRaw, error) {
	if len(sets) != 1 || sets[0].Block != finalized {
		return nil, fmt.Errorf("finalized storage query returned %d change sets at an unexpected block", len(sets))
	}
	requested := make(map[string]bool, len(keys))
	for _, key := range keys {
		requested[key.Hex()] = true
	}
	result := make(map[string]*types.StorageDataRaw, len(keys))
	for _, change := range sets[0].Changes {
		hexKey := change.StorageKey.Hex()
		if !requested[hexKey] {
			return nil, fmt.Errorf("finalized storage query returned unrequested key %s", hexKey)
		}
		if _, duplicate := result[hexKey]; duplicate {
			return nil, fmt.Errorf("finalized storage query returned duplicate key %s", hexKey)
		}
		if change.HasStorageData {
			data := types.StorageDataRaw(append([]byte(nil), change.StorageData...))
			result[hexKey] = &data
		} else {
			result[hexKey] = nil
		}
	}
	if len(result) != len(keys) {
		return nil, fmt.Errorf("finalized storage query returned %d changes, want %d", len(result), len(keys))
	}
	return result, nil
}

func decodeRequiredStorageQueryValue(values map[string]*types.StorageDataRaw, key types.StorageKey, label string, out any) error {
	raw, ok := values[key.Hex()]
	if !ok || raw == nil || len(*raw) == 0 {
		return fmt.Errorf("%s storage is absent from finalized query", label)
	}
	if err := codec.Decode(*raw, out); err != nil {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	return nil
}

func decodeValueQueryU64(values map[string]*types.StorageDataRaw, key types.StorageKey, label string) (uint64, error) {
	raw, ok := values[key.Hex()]
	if !ok {
		return 0, fmt.Errorf("%s key is absent from finalized query", label)
	}
	// Some Substrate RPC implementations serialize an absent ValueQuery item
	// as [key,"0x"] instead of [key]. Both represent the runtime fallback.
	if raw == nil || len(*raw) == 0 {
		return 0, nil
	}
	if len(*raw) != 8 {
		return 0, fmt.Errorf("%s SCALE length is %d, want 8", label, len(*raw))
	}
	return binary.LittleEndian.Uint64(*raw), nil
}

// Decode the leading AlphaBalance from a pinned runtime struct while also
// checking its complete SCALE width. Absent OptionQuery rows represent zero;
// a changed width is a runtime compatibility failure, not a value to guess.
func decodeOptionalLeadingU64(values map[string]*types.StorageDataRaw, key types.StorageKey, label string, encodedLength int) (uint64, error) {
	raw, ok := values[key.Hex()]
	if !ok {
		return 0, fmt.Errorf("%s key is absent from finalized query", label)
	}
	if raw == nil || len(*raw) == 0 {
		return 0, nil
	}
	if len(*raw) != encodedLength {
		return 0, fmt.Errorf("%s SCALE length is %d, want %d", label, len(*raw), encodedLength)
	}
	return binary.LittleEndian.Uint64((*raw)[:8]), nil
}

// Read lock and miner-collateral rows at one finalized checkpoint. The
// runtime permits only one individual conviction lock per coldkey/netuid; a
// second non-zero row is rejected because iterating an arbitrary first row
// would make transfer behavior ambiguous to the harness.
func readAlphaTransferSourceRestrictionsAt(chain *crv4.Chain, netuid uint16, coldkey, sourceHotkey [32]byte, finalized types.Hash, block uint64) (AlphaTransferSourceRestrictions, error) {
	result := AlphaTransferSourceRestrictions{FinalizedBlock: block, FinalizedHash: finalized.Hex()}
	hotkeysKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "StakingHotkeys", coldkey[:])
	if err != nil {
		return result, err
	}
	var stakingHotkeys []types.AccountID
	if err := readRequiredStorageAt(chain, hotkeysKey, crv4.PalletName, "StakingHotkeys", &stakingHotkeys, finalized); err != nil {
		return result, err
	}
	if len(stakingHotkeys) == 0 {
		return result, errors.New("alpha source coldkey has no staking hotkeys")
	}
	keys := make([]types.StorageKey, 0, len(stakingHotkeys)+2)
	lockKeys := make([]types.StorageKey, len(stakingHotkeys))
	for i, account := range stakingHotkeys {
		var hotkey [32]byte
		copy(hotkey[:], account[:])
		result.StakingHotkeys = append(result.StakingHotkeys, hotkey)
		lockKey, keyErr := types.CreateStorageKey(chain.Meta, crv4.PalletName, "Lock", coldkey[:], netuidArg(netuid), hotkey[:])
		if keyErr != nil {
			return result, keyErr
		}
		lockKeys[i] = lockKey
		keys = append(keys, lockKey)
	}
	positionCollateralKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "MinerCollateral", netuidArg(netuid), sourceHotkey[:], coldkey[:])
	if err != nil {
		return result, err
	}
	coldkeyCollateralKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "ColdkeyMinerCollateral", netuidArg(netuid), coldkey[:])
	if err != nil {
		return result, err
	}
	keys = append(keys, positionCollateralKey, coldkeyCollateralKey)
	values, err := queryStorageAtExact(chain, keys, finalized)
	if err != nil {
		return result, err
	}
	lockRows := 0
	for _, lockKey := range lockKeys {
		locked, decodeErr := decodeOptionalLeadingU64(values, lockKey, crv4.PalletName+".Lock", 32)
		if decodeErr != nil {
			return result, decodeErr
		}
		if locked == 0 {
			continue
		}
		lockRows++
		var addOK bool
		result.StoredLockRao, addOK = checkedAdd(result.StoredLockRao, locked)
		if !addOK {
			return result, errors.New("alpha source stored lock exceeds uint64")
		}
	}
	if lockRows > 1 {
		return result, fmt.Errorf("alpha source has %d non-zero conviction lock rows", lockRows)
	}
	result.PositionCollateralRao, err = decodeOptionalLeadingU64(values, positionCollateralKey, crv4.PalletName+".MinerCollateral", 40)
	if err != nil {
		return result, err
	}
	result.ColdkeyCollateralRao, err = decodeValueQueryU64(values, coldkeyCollateralKey, crv4.PalletName+".ColdkeyMinerCollateral")
	if err != nil {
		return result, err
	}
	if result.PositionCollateralRao > result.ColdkeyCollateralRao {
		return result, fmt.Errorf("source position collateral %d exceeds coldkey collateral %d", result.PositionCollateralRao, result.ColdkeyCollateralRao)
	}
	return result, nil
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

func readRegisteredAlphaSnapshotAt(chain *crv4.Chain, netuid uint16, finalized types.Hash, block uint64, topology SubnetTopologyFacts) (RegisteredAlphaSnapshot, error) {
	result := RegisteredAlphaSnapshot{FinalizedBlock: block, FinalizedHash: finalized.Hex(), ByHotkey: make(map[[32]byte]uint64, topology.UIDCount)}
	hotkeyKeys := make([]types.StorageKey, topology.UIDCount)
	for uid := uint16(0); uid < topology.UIDCount; uid++ {
		hotkeyKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "Keys", netuidArg(netuid), netuidArg(uid))
		if err != nil {
			return RegisteredAlphaSnapshot{}, err
		}
		hotkeyKeys[uid] = hotkeyKey
	}
	hotkeyValues, err := queryStorageAtExact(chain, hotkeyKeys, finalized)
	if err != nil {
		return RegisteredAlphaSnapshot{}, err
	}
	hotkeys := make([][32]byte, topology.UIDCount)
	totalKeys := make([]types.StorageKey, topology.UIDCount)
	for uid := uint16(0); uid < topology.UIDCount; uid++ {
		var account types.AccountID
		if err := decodeRequiredStorageQueryValue(hotkeyValues, hotkeyKeys[uid], crv4.PalletName+".Keys", &account); err != nil {
			return RegisteredAlphaSnapshot{}, err
		}
		var hotkey [32]byte
		copy(hotkey[:], account[:])
		hotkeys[uid] = hotkey
		totalKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "TotalHotkeyAlpha", hotkey[:], netuidArg(netuid))
		if err != nil {
			return RegisteredAlphaSnapshot{}, err
		}
		totalKeys[uid] = totalKey
	}
	totalValues, err := queryStorageAtExact(chain, totalKeys, finalized)
	if err != nil {
		return RegisteredAlphaSnapshot{}, err
	}
	for uid := uint16(0); uid < topology.UIDCount; uid++ {
		hotkey := hotkeys[uid]
		total, err := decodeValueQueryU64(totalValues, totalKeys[uid], crv4.PalletName+".TotalHotkeyAlpha")
		if err != nil {
			return RegisteredAlphaSnapshot{}, err
		}
		if _, duplicate := result.ByHotkey[hotkey]; duplicate {
			return RegisteredAlphaSnapshot{}, fmt.Errorf("netuid %d has duplicate registered hotkey 0x%x", netuid, hotkey)
		}
		result.ByHotkey[hotkey] = total
		next, ok := checkedAdd(result.TotalAlphaRao, total)
		if !ok {
			return RegisteredAlphaSnapshot{}, errors.New("registered subnet alpha exceeds uint64")
		}
		result.TotalAlphaRao = next
	}
	return result, nil
}

func (m *SubstrateManager) RegisteredAlphaSnapshot() (RegisteredAlphaSnapshot, error) {
	finalized, block, err := m.finalizedHead()
	if err != nil {
		return RegisteredAlphaSnapshot{}, err
	}
	topology, err := readSubnetTopologyAt(m.chain, m.cfg.Netuid, finalized)
	if err != nil {
		return RegisteredAlphaSnapshot{}, err
	}
	return readRegisteredAlphaSnapshotAt(m.chain, m.cfg.Netuid, finalized, block, topology)
}

// Read the complete registered-alpha composition at one canonical finalized
// block. Transfer postconditions use the inclusion block rather than a later
// emission snapshot, so a correct point-in-time reserve repair remains
// replayable after subsequent dilution.
func (m *SubstrateManager) RegisteredAlphaSnapshotAtBlock(block uint64) (RegisteredAlphaSnapshot, error) {
	if block == 0 {
		return RegisteredAlphaSnapshot{}, errors.New("registered-alpha snapshot block is zero")
	}
	_, finalizedBlock, err := m.finalizedHead()
	if err != nil {
		return RegisteredAlphaSnapshot{}, err
	}
	if block > finalizedBlock {
		return RegisteredAlphaSnapshot{}, fmt.Errorf("registered-alpha snapshot block %d is ahead of finalized head %d", block, finalizedBlock)
	}
	hash, err := m.chain.API.RPC.Chain.GetBlockHash(block)
	if err != nil {
		return RegisteredAlphaSnapshot{}, err
	}
	topology, err := readSubnetTopologyAt(m.chain, m.cfg.Netuid, hash)
	if err != nil {
		return RegisteredAlphaSnapshot{}, err
	}
	return readRegisteredAlphaSnapshotAt(m.chain, m.cfg.Netuid, hash, block, topology)
}

// Bind source transfer restrictions to the same finalized block as the
// registered-alpha snapshot used by the prebroadcast economic checks.
func (self *SubstrateManager) AlphaTransferSourceRestrictions(snapshot RegisteredAlphaSnapshot, coldkey, sourceHotkey [32]byte) (AlphaTransferSourceRestrictions, error) {
	if snapshot.FinalizedBlock == 0 || snapshot.FinalizedHash == "" {
		return AlphaTransferSourceRestrictions{}, errors.New("alpha source restrictions require a finalized snapshot")
	}
	finalized, err := types.NewHashFromHexString(snapshot.FinalizedHash)
	if err != nil {
		return AlphaTransferSourceRestrictions{}, fmt.Errorf("decode finalized alpha snapshot hash: %w", err)
	}
	return readAlphaTransferSourceRestrictionsAt(self.chain, self.cfg.Netuid, coldkey, sourceHotkey, finalized, snapshot.FinalizedBlock)
}

func readExistingUIDFactsAt(chain *crv4.Chain, netuid uint16, finalized types.Hash, topology SubnetTopologyFacts) ([]ExistingUIDFact, error) {
	hotkeyKeys := make([]types.StorageKey, topology.UIDCount)
	for uid := uint16(0); uid < topology.UIDCount; uid++ {
		hotkeyKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "Keys", netuidArg(netuid), netuidArg(uid))
		if err != nil {
			return nil, err
		}
		hotkeyKeys[uid] = hotkeyKey
	}
	hotkeyValues, err := queryStorageAtExact(chain, hotkeyKeys, finalized)
	if err != nil {
		return nil, fmt.Errorf("query registered hotkeys: %w", err)
	}
	hotkeys := make([]types.AccountID, topology.UIDCount)
	secondaryKeys := make([]types.StorageKey, 0, 3*int(topology.UIDCount))
	ownerKeys := make([]types.StorageKey, topology.UIDCount)
	registrationKeys := make([]types.StorageKey, topology.UIDCount)
	totalAlphaKeys := make([]types.StorageKey, topology.UIDCount)
	for uid := uint16(0); uid < topology.UIDCount; uid++ {
		var hotkey types.AccountID
		if err := decodeRequiredStorageQueryValue(hotkeyValues, hotkeyKeys[uid], crv4.PalletName+".Keys", &hotkey); err != nil {
			return nil, err
		}
		hotkeys[uid] = hotkey
		ownerKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "Owner", hotkey[:])
		if err != nil {
			return nil, err
		}
		registrationKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "BlockAtRegistration", netuidArg(netuid), netuidArg(uid))
		if err != nil {
			return nil, err
		}
		totalAlphaKey, err := types.CreateStorageKey(chain.Meta, crv4.PalletName, "TotalHotkeyAlpha", hotkey[:], netuidArg(netuid))
		if err != nil {
			return nil, err
		}
		ownerKeys[uid], registrationKeys[uid], totalAlphaKeys[uid] = ownerKey, registrationKey, totalAlphaKey
		secondaryKeys = append(secondaryKeys, ownerKey, registrationKey, totalAlphaKey)
	}
	secondaryValues, err := queryStorageAtExact(chain, secondaryKeys, finalized)
	if err != nil {
		return nil, fmt.Errorf("query registered UID facts: %w", err)
	}
	result := make([]ExistingUIDFact, 0, topology.UIDCount)
	for uid := uint16(0); uid < topology.UIDCount; uid++ {
		hotkey := hotkeys[uid]
		var coldkey types.AccountID
		if err := decodeRequiredStorageQueryValue(secondaryValues, ownerKeys[uid], crv4.PalletName+".Owner", &coldkey); err != nil {
			return nil, err
		}
		var registrationBlock types.U64
		if err := decodeRequiredStorageQueryValue(secondaryValues, registrationKeys[uid], crv4.PalletName+".BlockAtRegistration", &registrationBlock); err != nil {
			return nil, err
		}
		totalAlpha, err := decodeValueQueryU64(secondaryValues, totalAlphaKeys[uid], crv4.PalletName+".TotalHotkeyAlpha")
		if err != nil {
			return nil, err
		}
		var key [32]byte
		copy(key[:], hotkey[:])
		result = append(result, ExistingUIDFact{
			UID: uid, Hotkey: "0x" + fmt.Sprintf("%x", hotkey), Coldkey: "0x" + fmt.Sprintf("%x", coldkey),
			RegistrationBlock: uint64(registrationBlock), SubnetOwner: key == topology.OwnerHotkey, TotalHotkeyAlphaRao: totalAlpha,
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

// Runtime452PruneCandidate mirrors the pinned runtime's emission,
// registration-age, immunity, and UID tie breakers at one finalized state.
// The fresh-plan invariant proves the only subnet-owner-owned live identity is
// SubnetOwnerHotkey, so it is the sole immortal entry in this release topology.
func (m *SubstrateManager) Runtime452PruneCandidate() (uint16, error) {
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
	neurons := make([]runtime452PruneNeuron, 0, topology.UIDCount)
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
		neurons = append(neurons, runtime452PruneNeuron{
			UID: uid, Hotkey: key, EmissionRao: emission, RegistrationBlock: registrationBlock,
			Immune: age < uint64(immunityPeriod), Immortal: key == topology.OwnerHotkey,
		})
	}
	return runtime452PruneCandidate(neurons, minimumNonImmune)
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
	return readFreeBalanceAtHash(m.chain, account, finalized)
}

// Read runtime-452's u64 System.Account balance at one explicit native hash.
func readFreeBalanceAtHash(chain *crv4.Chain, account [32]byte, blockHash types.Hash) (uint64, error) {
	if chain == nil || chain.API == nil {
		return 0, errors.New("native balance chain is unavailable")
	}
	key, err := types.CreateStorageKey(chain.Meta, "System", "Account", account[:])
	if err != nil {
		return 0, err
	}
	var info subtensorAccountInfo
	ok, err := chain.API.RPC.State.GetStorage(key, &info, blockHash)
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

// Encode the exact metadata-driven signed transaction used by both first
// broadcast and interrupted-funding recovery. Keeping one encoder prevents a
// recovery verifier from drifting from the executor's wire representation.
func encodeSignedSubstrateCall(chain *crv4.Chain, signer signature.KeyringPair, call types.Call, nonce uint32) ([]byte, error) {
	if chain == nil || chain.Meta == nil || chain.Runtime == nil {
		return nil, errors.New("substrate signing context is unavailable")
	}
	ext := extrinsic.NewExtrinsic(call)
	err := ext.Sign(signer, chain.Meta, extrinsic.WithEra(types.ExtrinsicEra{IsImmortalEra: true}, chain.GenesisHash), extrinsic.WithNonce(types.NewUCompactFromUInt(uint64(nonce))), extrinsic.WithTip(types.NewUCompactFromUInt(0)), extrinsic.WithSpecVersion(chain.Runtime.SpecVersion), extrinsic.WithTransactionVersion(chain.Runtime.TransactionVersion), extrinsic.WithGenesisHash(chain.GenesisHash), extrinsic.WithMetadataMode(extensions.CheckMetadataModeDisabled, extensions.CheckMetadataHash{Hash: types.NewEmptyOption[types.H256]()}))
	if err != nil {
		return nil, err
	}
	return codec.Encode(ext)
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
	raw, err := encodeSignedSubstrateCall(m.chain, signer, call, nonce)
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
	// Runtime v452 encodes sp_runtime::PerU16 exactly as its u16 parts.
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
