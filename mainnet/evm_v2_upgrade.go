// The phase C upgrade of STSettlement2 (VERSION2-FINAL.md §16; meta review
// A5) goes out in two transactions:
//   - anyone creates the reviewed implementation, STSettlement2PhaseC, through
//     the canonical deterministic deployment proxy, so its address is fixed
//     before it exists;
//   - the governance Safe's owner then executes the Safe's direct call
//     upgradeToAndCall(implementation, 0x) on the STSettlement2 proxy, as the
//     launch's Safe calls went (mainnet/LAUNCH.md).
//
// review builds both from the forge artifact for review: the creation call,
// with the implementation's predicted address and exact runtime code; the
// Safe transaction, with its EIP-712 hash, which the Safe itself must compute
// identically; and the owner's execTransaction. Once the implementation
// exists, review requires its exact code and preflights the owner's
// execTransaction as an eth_call. It neither signs nor sends, and every read is
// pinned to one head block. Since 2026-11-06 a timelock owns the proxy; with
// --timelock, review schedules the same call through it (evm_v2_timelock.go).
//
// The capture the flag upgrade (CAPTURETHEFLAG.md §3.1, §11.2) replaces phase
// C with STSettlement2Ctf and registers an operator's CTF pool in the same
// timelock operation: its call is upgradeToAndCall(implementation,
// registerCtfPool(noId, maximumBurnRao, firstEpoch)) with maximumBurnRao ×
// 10^9 wei, which the timelock pays from its own balance and vault2's refund
// of the burn surplus returns to. With that artifact review takes --no-id,
// --maximum-burn-rao, --first-epoch and --timelock, checks the registration
// against the chain as registerCtfPool will at execution, and requires the
// operation to be ready before the first epoch starts and, once its call is
// preflighted, the timelock to hold the value.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/stabi"
)

const settlement2UpgradeReviewSchema = "urnetwork-mainnet-settlement-v2-upgrade-review-v1"

// The capture the flag upgrade's review adds the ctf section, and its
// timelock call carries a value, so its schema is a new one.
const settlement2CtfUpgradeReviewSchema = "urnetwork-mainnet-settlement-v2-ctf-upgrade-review-v1"

// The canonical deterministic deployment proxy, at its keyless address on
// every chain that has it, and its exact runtime code: called with a 32-byte
// salt followed by creation code, it CREATE2s that code with the salt and
// returns the new address.
var upgradeCreate2Factory = common.HexToAddress("0x4e59b44847b379578588920ca78fbf26c0b4956c")

const upgradeCreate2FactoryRuntime = "0x7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe03601600081602082378035828234f58015156039578182fd5b8082525050506014600cf3"

// The phase C implementation's functions that STSettlement2 lacks. An artifact
// without all three is not the reviewed upgrade.
var upgradePhaseCFunctions = []string{"assignSurplus(uint256,uint256,uint256)", "recordV1Sweep(uint256,uint256,bytes32)", "v1Sweep(uint256)"}

// The capture the flag implementation's functions that phase C lacks
// (CAPTURETHEFLAG.md §3.1). An artifact with phase C's functions and all five
// of these is STSettlement2Ctf; one with only some of them is neither.
var upgradeCtfFunctions = []string{"registerCtfPool(uint256,uint64,uint64)", "commitCtfCensus(uint256,uint256,bytes32)", "ctfCensus(uint256,uint256)", "ctfSettlementAt(uint256,uint256)", "ctfPoolId(uint256)"}

// Operator n's CTF pool is vault2 pool 2^32 + n.
var settlement2CtfPoolBase = new(big.Int).Lsh(common.Big1, 32)

// settlement2BlockSeconds is the measured block time review estimates an
// epoch's start with (CAPTURETHEFLAG.md §11.2).
const settlement2BlockSeconds = 12

// How registerCtfPool derives the CTF pool's hotkey, which no one knows
// before the block that executes the operation.
const settlement2CtfHotkeyDerivation = "keccak256(abi.encode(proxy, pool_id, block.number, blockhash(block.number - 1))) at execution"

// Reviewed coordinates of a Safe 1.4.1 (SafeL2): its version, the guard slot
// keccak256("guard_manager.guard.address"), the fallback handler slot
// keccak256("fallback_manager.handler.address") and its EIP-712 type hashes.
const upgradeSafeVersion = "1.4.1"

var (
	upgradeSafeGuardSlot    = common.HexToHash("0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8")
	upgradeSafeFallbackSlot = common.HexToHash("0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5")
	upgradeSafeDomainType   = crypto.Keccak256Hash([]byte("EIP712Domain(uint256 chainId,address verifyingContract)"))
	upgradeSafeTxType       = crypto.Keccak256Hash([]byte("SafeTx(address to,uint256 value,bytes data,uint8 operation,uint256 safeTxGas,uint256 baseGas,uint256 gasPrice,address gasToken,address refundReceiver,uint256 nonce)"))
	upgradeModulesSentinel  = common.HexToAddress("0x0000000000000000000000000000000000000001")
)

// The Safe functions review reads and the owner calls.
const upgradeSafeAbiJson = `[
 {"type":"function","name":"VERSION","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"string"}]},
 {"type":"function","name":"nonce","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
 {"type":"function","name":"getThreshold","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
 {"type":"function","name":"getOwners","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"address[]"}]},
 {"type":"function","name":"getModulesPaginated","stateMutability":"view","inputs":[{"name":"start","type":"address"},{"name":"pageSize","type":"uint256"}],"outputs":[{"name":"array","type":"address[]"},{"name":"next","type":"address"}]},
 {"type":"function","name":"getTransactionHash","stateMutability":"view","inputs":[{"name":"to","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"},{"name":"operation","type":"uint8"},{"name":"safeTxGas","type":"uint256"},{"name":"baseGas","type":"uint256"},{"name":"gasPrice","type":"uint256"},{"name":"gasToken","type":"address"},{"name":"refundReceiver","type":"address"},{"name":"_nonce","type":"uint256"}],"outputs":[{"name":"","type":"bytes32"}]},
 {"type":"function","name":"execTransaction","stateMutability":"payable","inputs":[{"name":"to","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"},{"name":"operation","type":"uint8"},{"name":"safeTxGas","type":"uint256"},{"name":"baseGas","type":"uint256"},{"name":"gasPrice","type":"uint256"},{"name":"gasToken","type":"address"},{"name":"refundReceiver","type":"address"},{"name":"signatures","type":"bytes"}],"outputs":[{"name":"success","type":"bool"}]}
]`

var upgradeSafeAbi = func() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(upgradeSafeAbiJson))
	if err != nil {
		panic(err)
	}
	return parsed
}()

// A refusal means the owner must not sign. It exits 3; exit 1 is only a read
// that got no answer, which decides nothing.
var errUpgradeRefused = errors.New("settlement2 upgrade refused")

func upgradeRefusal(reason string) error { return fmt.Errorf("%w: %s", errUpgradeRefused, reason) }

// upgradeArtifact is the implementation's forge build: its creation code, its
// runtime code with the UUPS __self immutable left zero, and that immutable's
// offsets in the runtime code. ctf marks STSettlement2Ctf's.
type upgradeArtifact struct {
	abi      abi.ABI
	creation []byte
	runtime  []byte
	self     []int
	ctf      bool
}

// kind names the implementation the artifact builds.
func (self upgradeArtifact) kind() string {
	if self.ctf {
		return "capture the flag"
	}
	return "phase C"
}

// loadUpgradeArtifact reads a forge artifact of STSettlement2PhaseC or of
// STSettlement2Ctf: it must declare the phase C functions, and the second
// also every capture the flag function, and exactly one immutable,
// UUPSUpgradeable's __self, a 32-byte word.
func loadUpgradeArtifact(raw []byte) (upgradeArtifact, error) {
	var result upgradeArtifact
	var parsed struct {
		Abi      json.RawMessage `json:"abi"`
		Bytecode struct {
			Object string `json:"object"`
		} `json:"bytecode"`
		DeployedBytecode struct {
			Object              string `json:"object"`
			ImmutableReferences map[string][]struct {
				Start  int `json:"start"`
				Length int `json:"length"`
			} `json:"immutableReferences"`
		} `json:"deployedBytecode"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return result, err
	}
	var err error
	if result.abi, err = abi.JSON(bytes.NewReader(parsed.Abi)); err != nil {
		return result, err
	}
	declared := map[string]bool{}
	for _, method := range result.abi.Methods {
		declared[method.Sig] = true
	}
	for _, signature := range upgradePhaseCFunctions {
		if !declared[signature] {
			return result, fmt.Errorf("the artifact lacks %s: it is not the phase C implementation", signature)
		}
	}
	var ctf []string
	for _, signature := range upgradeCtfFunctions {
		if declared[signature] {
			ctf = append(ctf, signature)
		}
	}
	switch len(ctf) {
	case 0:
	case len(upgradeCtfFunctions):
		result.ctf = true
	default:
		return result, fmt.Errorf("the artifact declares %s but not all of %s: it is neither the phase C nor the capture the flag implementation", strings.Join(ctf, ", "), strings.Join(upgradeCtfFunctions, ", "))
	}
	creation, creationErr := hexutil.Decode(parsed.Bytecode.Object)
	runtime, runtimeErr := hexutil.Decode(parsed.DeployedBytecode.Object)
	if err := errors.Join(creationErr, runtimeErr); err != nil || len(creation) == 0 || len(runtime) == 0 || len(runtime) >= 24_576 || len(creation) >= 49_152 {
		return result, errors.Join(errors.New("the artifact's creation or runtime code is missing, malformed or over the EIP-170/3860 limits"), err)
	}
	if len(parsed.DeployedBytecode.ImmutableReferences) != 1 {
		return result, fmt.Errorf("the artifact has %d immutables, want only UUPSUpgradeable's __self", len(parsed.DeployedBytecode.ImmutableReferences))
	}
	for _, references := range parsed.DeployedBytecode.ImmutableReferences {
		for _, reference := range references {
			if reference.Length != 32 || reference.Start < 0 || reference.Start+32 > len(runtime) || !bytes.Equal(runtime[reference.Start:reference.Start+32], make([]byte, 32)) {
				return result, errors.New("an immutable reference is not a zero 32-byte word of the runtime code")
			}
			result.self = append(result.self, reference.Start)
		}
	}
	if len(result.self) == 0 {
		return result, errors.New("the artifact's __self immutable has no reference")
	}
	result.creation, result.runtime = creation, runtime
	return result, nil
}

// address is the implementation's CREATE2 address through the factory.
func (self upgradeArtifact) address(salt common.Hash) common.Address {
	return crypto.CreateAddress2(upgradeCreate2Factory, salt, crypto.Keccak256(self.creation))
}

// runtimeAt is the exact runtime code created at `address`: __self holds it.
func (self upgradeArtifact) runtimeAt(address common.Address) []byte {
	code := bytes.Clone(self.runtime)
	for _, start := range self.self {
		copy(code[start:start+32], common.LeftPadBytes(address.Bytes(), 32))
	}
	return code
}

// safeTransaction is a Safe 1.4.1 transaction, operation 0 (a call) or 1.
type safeTransaction struct {
	To             common.Address
	Value          *big.Int
	Data           []byte
	Operation      uint8
	SafeTxGas      *big.Int
	BaseGas        *big.Int
	GasPrice       *big.Int
	GasToken       common.Address
	RefundReceiver common.Address
	Nonce          *big.Int
}

// safeTransactionHash returns the Safe's EIP-712 domain separator, the
// SafeTx struct hash and the transaction hash the owner approves.
func safeTransactionHash(chainId uint64, safe common.Address, tx safeTransaction) (domain, message, hash common.Hash) {
	word := func(value *big.Int) []byte { return common.LeftPadBytes(value.Bytes(), 32) }
	address := func(value common.Address) []byte { return common.LeftPadBytes(value.Bytes(), 32) }
	domain = crypto.Keccak256Hash(upgradeSafeDomainType.Bytes(), word(new(big.Int).SetUint64(chainId)), address(safe))
	message = crypto.Keccak256Hash(upgradeSafeTxType.Bytes(), address(tx.To), word(tx.Value), crypto.Keccak256(tx.Data),
		word(big.NewInt(int64(tx.Operation))), word(tx.SafeTxGas), word(tx.BaseGas), word(tx.GasPrice),
		address(tx.GasToken), address(tx.RefundReceiver), word(tx.Nonce))
	hash = crypto.Keccak256Hash([]byte{0x19, 0x01}, domain.Bytes(), message.Bytes())
	return domain, message, hash
}

// upgradeOwnerSignature is the Safe signature of an owner who sends
// execTransaction itself: r is the owner, s zero and v 1, which the Safe
// accepts when the sender is that owner.
func upgradeOwnerSignature(owner common.Address) []byte {
	return append(append(common.LeftPadBytes(owner.Bytes(), 32), make([]byte, 32)...), 1)
}

// One upgrade review request. ctf is the CTF pool registration that the
// capture the flag upgrade's call makes, and nil for phase C's.
type settlement2Upgrade struct {
	settlement common.Address
	governance settlement2Governance
	salt       common.Hash
	artifact   upgradeArtifact
	ctf        *settlement2CtfRegistration
}

// settlement2CtfRegistration is registerCtfPool's arguments.
type settlement2CtfRegistration struct {
	noId           uint64
	maximumBurnRao uint64
	firstEpoch     uint64
}

// poolId is the CTF pool's vault2 id, 2^32 + noId.
func (self settlement2CtfRegistration) poolId() *big.Int {
	return new(big.Int).Add(settlement2CtfPoolBase, new(big.Int).SetUint64(self.noId))
}

// value is the call's value, maximumBurnRao × 10^9 wei: vault2 burns at most
// maximumBurnRao from it and refunds the rest.
func (self settlement2CtfRegistration) value() *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(self.maximumBurnRao), big.NewInt(1_000_000_000))
}

// The review's result. Every amount is a decimal string; data is hex. Only a
// review in timelock mode has the timelock section, only the capture the flag
// upgrade's has the ctf section, and the execute step of an open executor has
// no Safe transaction.
type settlement2UpgradeReview struct {
	Schema         string                      `json:"schema"`
	ChainId        uint64                      `json:"chain_id"`
	HeadBlock      uint64                      `json:"head_block"`
	Implementation settlement2UpgradeCreation  `json:"implementation"`
	Settlement2    settlement2UpgradeProxy     `json:"settlement2"`
	Ctf            *settlement2CtfUpgrade      `json:"ctf,omitempty"`
	Timelock       *settlement2UpgradeTimelock `json:"timelock,omitempty"`
	Safe           *settlement2UpgradeSafe     `json:"safe,omitempty"`
	SafeTx         *settlement2UpgradeSafeTx   `json:"safe_transaction,omitempty"`
	OwnerTx        *settlement2UpgradeOwnerTx  `json:"owner_transaction,omitempty"`
	Preflight      settlement2UpgradePreflight `json:"preflight"`
}

type settlement2UpgradeCreation struct {
	Factory          string `json:"factory"`
	Salt             string `json:"salt"`
	Address          string `json:"address"`
	CreationBytes    int    `json:"creation_bytes"`
	CreationKeccak   string `json:"creation_keccak256"`
	RuntimeBytes     int    `json:"runtime_bytes"`
	RuntimeCodeHash  string `json:"runtime_code_hash"`
	Created          bool   `json:"created"`
	CreateTo         string `json:"create_to"`
	CreateValueWei   string `json:"create_value_wei"`
	CreateData       string `json:"create_data"`
	CreateDataKeccak string `json:"create_data_keccak256"`
}

type settlement2UpgradeProxy struct {
	Proxy                 string `json:"proxy"`
	Owner                 string `json:"owner"`
	CurrentImplementation string `json:"current_implementation"`
}

// The ctf section: registerCtfPool's arguments, the call's value and what
// review checked them against at the head, through the proxy's vault() and
// v1Coordinator(). The pool's hotkey is derived only at execution.
type settlement2CtfUpgrade struct {
	NoId                 uint64 `json:"no_id"`
	PoolId               string `json:"pool_id"`
	MaximumBurnRao       string `json:"maximum_burn_rao"`
	ValueWei             string `json:"value_wei"`
	FirstEpoch           uint64 `json:"first_epoch"`
	FirstEpochStartBlock uint64 `json:"first_epoch_start_block"`
	BlocksRemaining      uint64 `json:"blocks_remaining"`
	CurrentEpoch         uint64 `json:"current_epoch"`
	Vault2               string `json:"vault2"`
	V1Coordinator        string `json:"v1_coordinator"`
	ProviderPoolHotkey   string `json:"provider_pool_hotkey"`
	TimelockBalanceWei   string `json:"timelock_balance_wei"`
	HotkeyDerivation     string `json:"hotkey_derivation"`
}

type settlement2UpgradeSafe struct {
	Address         string   `json:"address"`
	Version         string   `json:"version"`
	Threshold       uint64   `json:"threshold"`
	Owners          []string `json:"owners"`
	Nonce           string   `json:"nonce"`
	Modules         []string `json:"modules"`
	Guard           string   `json:"guard"`
	FallbackHandler string   `json:"fallback_handler"`
}

type settlement2UpgradeSafeTx struct {
	To              string            `json:"to"`
	ValueWei        string            `json:"value_wei"`
	Data            string            `json:"data"`
	Operation       uint8             `json:"operation"`
	SafeTxGas       string            `json:"safe_tx_gas"`
	BaseGas         string            `json:"base_gas"`
	GasPrice        string            `json:"gas_price"`
	GasToken        string            `json:"gas_token"`
	RefundReceiver  string            `json:"refund_receiver"`
	Nonce           string            `json:"nonce"`
	Function        string            `json:"function"`
	Args            map[string]string `json:"args"`
	DomainSeparator string            `json:"domain_separator"`
	MessageHash     string            `json:"message_hash"`
	SafeTxHash      string            `json:"safe_tx_hash"`
}

type settlement2UpgradeOwnerTx struct {
	From       string `json:"from"`
	To         string `json:"to"`
	ValueWei   string `json:"value_wei"`
	Data       string `json:"data"`
	DataKeccak string `json:"data_keccak256"`
	Function   string `json:"function"`
	Signature  string `json:"signature"`
}

type settlement2UpgradePreflight struct {
	Ran    bool   `json:"ran"`
	Result string `json:"result"`
}

// review builds the creation call and the Safe transaction, refuses what is
// wrong, and preflights the owner's execTransaction once the implementation
// exists. The capture the flag artifact's upgrade takes the registration's
// three flags and goes only through the timelock.
func runUpgradeSettlement2Command(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	const usage = "usage: upgrade-settlement2 review --rpc URL --settlement2 ADDRESS --safe ADDRESS --artifact FORGE-ARTIFACT.json --salt 0xSALT [--timelock ADDRESS [--timelock-salt 0xSALT] [--execute] [--no-id N --maximum-burn-rao RAO --first-epoch E]]"
	if len(args) == 0 || args[0] != "review" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	flags := flag.NewFlagSet("upgrade-settlement2 review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var settlement, safe, artifactPath, salt string
	var registration settlement2CtfRegistration
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) EVM JSON-RPC URL")
	flags.StringVar(&settlement, "settlement2", "", "the STSettlement2 proxy")
	flags.StringVar(&safe, "safe", "", "the governance Safe, which owns STSettlement2 or proposes to the timelock that does")
	flags.StringVar(&artifactPath, "artifact", "", "the forge artifact of STSettlement2PhaseC (evm/out/STSettlement2PhaseC.sol/STSettlement2PhaseC.json) or of STSettlement2Ctf, whose upgrade also registers a CTF pool")
	flags.StringVar(&salt, "salt", "", "the CREATE2 salt: 0x and 32 bytes of hex")
	flags.Uint64Var(&registration.noId, "no-id", 0, "STSettlement2Ctf only: the v1 operator, 1 to 2^32 - 1, whose CTF pool 2^32 + N the upgrade's call registers")
	flags.Uint64Var(&registration.maximumBurnRao, "maximum-burn-rao", 0, "STSettlement2Ctf only: registerCtfPool's burn ceiling in rao; the call's value is this × 10^9 wei")
	flags.Uint64Var(&registration.firstEpoch, "first-epoch", 0, "STSettlement2Ctf only: the CTF pool's first epoch")
	timelocked := settlement2TimelockFlags(flags)
	complete := flags.Parse(args[1:]) == nil && flags.NArg() == 0
	set := map[string]bool{}
	flags.Visit(func(given *flag.Flag) { set[given.Name] = true })
	for _, required := range []string{"rpc", "settlement2", "safe", "artifact", "salt"} {
		complete = complete && set[required]
	}
	var upgrade settlement2Upgrade
	var settlementOk, safeOk bool
	upgrade.settlement, settlementOk = poolAddress(settlement)
	upgrade.governance.safe, safeOk = poolAddress(safe)
	upgrade.salt = common.HexToHash(salt)
	if !complete || !settlementOk || !safeOk || !validHash(salt) || !timelocked(set, &upgrade.governance) {
		fmt.Fprintln(stderr, "upgrade-settlement2 review requires --rpc, --settlement2, --safe, --artifact and --salt: nonzero 0x addresses, an artifact path and a 0x 32-byte salt; --timelock-salt (0x and 32 bytes) and --execute only with --timelock, another nonzero 0x address")
		fmt.Fprintln(stderr, usage)
		return 2
	}
	registers := set["no-id"] || set["maximum-burn-rao"] || set["first-epoch"]
	if set["no-id"] && (registration.noId == 0 || registration.noId >= 1<<32) || set["maximum-burn-rao"] && registration.maximumBurnRao == 0 || set["first-epoch"] && registration.firstEpoch == 0 {
		fmt.Fprintln(stderr, "upgrade-settlement2 review: --no-id is 1 to 2^32 - 1, and --maximum-burn-rao and --first-epoch are positive decimal integers")
		fmt.Fprintln(stderr, usage)
		return 2
	}
	raw, err := os.ReadFile(artifactPath)
	if err == nil {
		upgrade.artifact, err = loadUpgradeArtifact(raw)
	}
	if err != nil {
		fmt.Fprintln(stderr, "upgrade-settlement2 review: artifact:", err)
		return 2
	}
	switch {
	case upgrade.artifact.ctf && !(set["no-id"] && set["maximum-burn-rao"] && set["first-epoch"] && upgrade.governance.timelocked()):
		fmt.Fprintln(stderr, "upgrade-settlement2 review: the STSettlement2Ctf artifact's upgrade registers a CTF pool in the same timelock operation: it requires --no-id, --maximum-burn-rao, --first-epoch and --timelock")
		fmt.Fprintln(stderr, usage)
		return 2
	case !upgrade.artifact.ctf && registers:
		fmt.Fprintln(stderr, "upgrade-settlement2 review: --no-id, --maximum-burn-rao and --first-epoch go only with the STSettlement2Ctf artifact")
		fmt.Fprintln(stderr, usage)
		return 2
	case upgrade.artifact.ctf:
		upgrade.ctf = &registration
	}
	client, err := newRpcClient(*rpcUrl, 60*time.Second)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	review, err := reviewSettlement2Upgrade(ctx, client, upgrade)
	if err != nil {
		fmt.Fprintln(stderr, "upgrade-settlement2 review:", err)
		if errors.Is(err, errUpgradeRefused) {
			return 3
		}
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(review); err != nil {
		fmt.Fprintln(stderr, "upgrade-settlement2 review output:", err)
		return 1
	}
	return 0
}

// The upgrade review reads only these methods; it cannot send.
func (self *rpcClient) callUpgradeRead(ctx context.Context, method string, params []any, result any) error {
	switch method {
	case "eth_chainId", "eth_blockNumber", "eth_getBlockByNumber", "eth_call", "eth_getCode", "eth_getStorageAt", "eth_getLogs", "eth_getBalance":
		return self.callAdmittedRead(ctx, method, params, result, false, maxRpcReplyBytes)
	}
	return errors.New("method is outside the settlement2 upgrade read profile")
}

// upgradeChain pins every read to one head block.
type upgradeChain struct {
	client *rpcClient
	head   string
}

func (self *upgradeChain) code(ctx context.Context, address common.Address) ([]byte, error) {
	var code hexutil.Bytes
	err := self.client.callUpgradeRead(ctx, "eth_getCode", []any{address.Hex(), self.head}, &code)
	return code, err
}

func (self *upgradeChain) word(ctx context.Context, address common.Address, slot common.Hash) (common.Hash, error) {
	var value hexutil.Bytes
	if err := self.client.callUpgradeRead(ctx, "eth_getStorageAt", []any{address.Hex(), slot.Hex(), self.head}, &value); err != nil {
		return common.Hash{}, err
	}
	if len(value) != 32 {
		return common.Hash{}, fmt.Errorf("eth_getStorageAt of %s answered %d bytes", address.Hex(), len(value))
	}
	return common.BytesToHash(value), nil
}

// call runs one eth_call at the head, from `from` when it is set and with
// value when it is nonzero: the node then checks that the sender holds it. A
// call the chain ran and failed is a refusal, never a failed read.
func (self *upgradeChain) call(ctx context.Context, from *common.Address, value *big.Int, to common.Address, data []byte) ([]byte, error) {
	message := map[string]any{"to": to.Hex(), "data": hexutil.Encode(data)}
	if from != nil {
		message["from"] = from.Hex()
	}
	valued := ""
	if value != nil && value.Sign() != 0 {
		message["value"], valued = hexutil.EncodeBig(value), fmt.Sprintf(" with %s wei", value)
	}
	var output hexutil.Bytes
	err := self.client.callUpgradeRead(ctx, "eth_call", []any{message, self.head}, &output)
	var failure *rpcCallError
	if errors.As(err, &failure) {
		var encoded string
		_ = json.Unmarshal(failure.data, &encoded)
		revert, decodeErr := hexutil.Decode(encoded)
		if failure.code == 3 || decodeErr == nil || strings.Contains(failure.message, "revert") || strings.Contains(failure.message, "evm error") {
			return nil, upgradeRefusal(fmt.Sprintf("call 0x%x to %s%s failed with %q and data 0x%x%s", data[:4], to.Hex(), valued, failure.message, revert, upgradeRevertReason(revert)))
		}
	}
	return output, err
}

// upgradeRevertReason names an Error(string) revert, such as a Safe's GS013.
func upgradeRevertReason(revert []byte) string {
	if len(revert) < 4 || !bytes.Equal(revert[:4], []byte{0x08, 0xc3, 0x79, 0xa0}) {
		return ""
	}
	values, err := abi.Arguments{{Type: abi.Type{T: abi.StringTy}}}.Unpack(revert[4:])
	if err != nil || len(values) != 1 {
		return ""
	}
	return fmt.Sprintf(" (%v)", values[0])
}

// upgradeView decodes one view of `contract` at `to`. An answer its contract
// would not give, as an address without code gives, is a refusal.
func upgradeView(ctx context.Context, chain *upgradeChain, to common.Address, contract abi.ABI, method string, args ...any) ([]any, error) {
	data, err := contract.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	output, err := chain.call(ctx, nil, nil, to, data)
	if err != nil {
		return nil, fmt.Errorf("%s of %s: %w", method, to.Hex(), err)
	}
	values, err := contract.Unpack(method, output)
	if err != nil {
		return nil, upgradeRefusal(fmt.Sprintf("%s of %s answered 0x%x, not as its contract does", method, to.Hex(), output))
	}
	return values, nil
}

// openUpgradeChain requires EVM chain 964 and pins every later read to its head.
func openUpgradeChain(ctx context.Context, client *rpcClient) (*upgradeChain, uint64, error) {
	var chainId hexutil.Uint64
	if err := client.callUpgradeRead(ctx, "eth_chainId", []any{}, &chainId); err != nil {
		return nil, 0, err
	}
	if chainId != mainnetEvmChainId {
		return nil, 0, upgradeRefusal("the RPC is not EVM chain 964")
	}
	var head hexutil.Uint64
	if err := client.callUpgradeRead(ctx, "eth_blockNumber", []any{}, &head); err != nil {
		return nil, 0, err
	}
	return &upgradeChain{client: client, head: hexutil.EncodeUint64(uint64(head))}, uint64(head), nil
}

// readSettlement2Proxy requires the Safe, or with --timelock the timelock, to
// own the proxy and returns the proxy's current implementation, which must
// exist. Another owner that answers getMinDelay() is named as a timelock to
// review through.
func readSettlement2Proxy(ctx context.Context, chain *upgradeChain, settlement common.Address, governance settlement2Governance) (common.Address, error) {
	settlementAbi := stabi.NewSTSettlement2()
	ownerOutput, err := chain.call(ctx, nil, nil, settlement, settlementAbi.PackOwner())
	if err != nil {
		return common.Address{}, err
	}
	owner, err := settlementAbi.UnpackOwner(ownerOutput)
	if err != nil {
		return common.Address{}, upgradeRefusal(fmt.Sprintf("owner() of %s answered 0x%x", settlement.Hex(), ownerOutput))
	}
	if owner != governance.owner() {
		switch {
		case governance.timelocked():
			return common.Address{}, upgradeRefusal(fmt.Sprintf("STSettlement2's owner is %s, not the timelock %s", owner.Hex(), governance.timelock.Hex()))
		case settlement2LooksLikeTimelock(ctx, chain, owner):
			return common.Address{}, upgradeRefusal(fmt.Sprintf("STSettlement2's owner is %s, not the Safe; it answers getMinDelay() as a TimelockController does: review the call through it with --timelock %s", owner.Hex(), owner.Hex()))
		}
		return common.Address{}, upgradeRefusal(fmt.Sprintf("STSettlement2's owner is %s, not the Safe", owner.Hex()))
	}
	slot, err := chain.word(ctx, settlement, common.HexToHash(contractImplementationSlot))
	if err != nil {
		return common.Address{}, err
	}
	current := common.BytesToAddress(slot.Bytes())
	if current == (common.Address{}) {
		return common.Address{}, upgradeRefusal("the proxy has no implementation: it is not the STSettlement2 proxy")
	}
	return current, nil
}

// readUpgradeSafe requires a Safe 1.4.1 with one owner at threshold one, no
// modules and no guard, and returns it with its owner and nonce.
func readUpgradeSafe(ctx context.Context, chain *upgradeChain, address common.Address) (settlement2UpgradeSafe, common.Address, *big.Int, error) {
	safe := settlement2UpgradeSafe{Address: address.Hex(), Modules: []string{}}
	values, err := upgradeView(ctx, chain, address, upgradeSafeAbi, "VERSION")
	if err != nil {
		return safe, common.Address{}, nil, err
	}
	safe.Version = values[0].(string)
	values, err = upgradeView(ctx, chain, address, upgradeSafeAbi, "getThreshold")
	if err != nil {
		return safe, common.Address{}, nil, err
	}
	threshold := values[0].(*big.Int)
	values, err = upgradeView(ctx, chain, address, upgradeSafeAbi, "getOwners")
	if err != nil {
		return safe, common.Address{}, nil, err
	}
	owners := values[0].([]common.Address)
	values, err = upgradeView(ctx, chain, address, upgradeSafeAbi, "nonce")
	if err != nil {
		return safe, common.Address{}, nil, err
	}
	nonce := values[0].(*big.Int)
	values, err = upgradeView(ctx, chain, address, upgradeSafeAbi, "getModulesPaginated", upgradeModulesSentinel, big.NewInt(10))
	if err != nil {
		return safe, common.Address{}, nil, err
	}
	modules := values[0].([]common.Address)
	guard, err := chain.word(ctx, address, upgradeSafeGuardSlot)
	if err != nil {
		return safe, common.Address{}, nil, err
	}
	handler, err := chain.word(ctx, address, upgradeSafeFallbackSlot)
	if err != nil {
		return safe, common.Address{}, nil, err
	}
	switch {
	case safe.Version != upgradeSafeVersion:
		return safe, common.Address{}, nil, upgradeRefusal(fmt.Sprintf("the Safe runs version %q, not %s", safe.Version, upgradeSafeVersion))
	case threshold.Cmp(common.Big1) != 0 || len(owners) != 1:
		return safe, common.Address{}, nil, upgradeRefusal(fmt.Sprintf("the Safe has threshold %s of %d owners, not one of one", threshold, len(owners)))
	case len(modules) != 0:
		return safe, common.Address{}, nil, upgradeRefusal(fmt.Sprintf("the Safe has %d modules", len(modules)))
	case guard != (common.Hash{}):
		return safe, common.Address{}, nil, upgradeRefusal("the Safe has a transaction guard")
	}
	safe.Threshold, safe.Owners, safe.Nonce = threshold.Uint64(), []string{owners[0].Hex()}, nonce.String()
	safe.Guard, safe.FallbackHandler = common.Address{}.Hex(), common.BytesToAddress(handler.Bytes()).Hex()
	return safe, owners[0], nonce, nil
}

// buildOwnerSafeCall builds the Safe's call of data on `to`, at the Safe's
// nonce with no refund, which the Safe must hash identically, and the owner's
// execTransaction of it.
func buildOwnerSafeCall(ctx context.Context, chain *upgradeChain, safe, owner common.Address, nonce *big.Int, to common.Address, data []byte, function string, args map[string]string) (settlement2UpgradeSafeTx, settlement2UpgradeOwnerTx, []byte, error) {
	tx := safeTransaction{To: to, Value: new(big.Int), Data: data, SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int), Nonce: nonce}
	domain, message, hash := safeTransactionHash(mainnetEvmChainId, safe, tx)
	values, err := upgradeView(ctx, chain, safe, upgradeSafeAbi, "getTransactionHash", tx.To, tx.Value, tx.Data, tx.Operation, tx.SafeTxGas, tx.BaseGas, tx.GasPrice, tx.GasToken, tx.RefundReceiver, tx.Nonce)
	if err != nil {
		return settlement2UpgradeSafeTx{}, settlement2UpgradeOwnerTx{}, nil, err
	}
	if values[0].([32]byte) != hash {
		return settlement2UpgradeSafeTx{}, settlement2UpgradeOwnerTx{}, nil, upgradeRefusal(fmt.Sprintf("the Safe hashes the transaction as %x, not %s", values[0], hash.Hex()))
	}
	signature := upgradeOwnerSignature(owner)
	execData, err := upgradeSafeAbi.Pack("execTransaction", tx.To, tx.Value, tx.Data, tx.Operation, tx.SafeTxGas, tx.BaseGas, tx.GasPrice, tx.GasToken, tx.RefundReceiver, signature)
	if err != nil {
		return settlement2UpgradeSafeTx{}, settlement2UpgradeOwnerTx{}, nil, err
	}
	zero := common.Address{}.Hex()
	return settlement2UpgradeSafeTx{
		To: tx.To.Hex(), ValueWei: "0", Data: hexutil.Encode(tx.Data), Operation: tx.Operation, SafeTxGas: "0", BaseGas: "0", GasPrice: "0",
		GasToken: zero, RefundReceiver: zero, Nonce: nonce.String(), Function: function, Args: args,
		DomainSeparator: domain.Hex(), MessageHash: message.Hex(), SafeTxHash: hash.Hex(),
	}, settlement2UpgradeOwnerTx{
		From: owner.Hex(), To: safe.Hex(), ValueWei: "0", Data: hexutil.Encode(execData), DataKeccak: crypto.Keccak256Hash(execData).Hex(),
		Function: "execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)", Signature: hexutil.Encode(signature),
	}, execData, nil
}

// preflightOwnerCall runs the owner's execTransaction as an eth_call from the
// owner; it must return true.
func preflightOwnerCall(ctx context.Context, chain *upgradeChain, owner, safe common.Address, execData []byte) error {
	output, err := chain.call(ctx, &owner, nil, safe, execData)
	if err != nil {
		return err
	}
	success, unpackErr := upgradeSafeAbi.Unpack("execTransaction", output)
	if unpackErr != nil || len(success) != 1 || success[0] != true {
		return upgradeRefusal(fmt.Sprintf("the owner's execTransaction answered 0x%x, not true", output))
	}
	return nil
}

func reviewSettlement2Upgrade(ctx context.Context, client *rpcClient, upgrade settlement2Upgrade) (settlement2UpgradeReview, error) {
	var result settlement2UpgradeReview
	chain, head, err := openUpgradeChain(ctx, client)
	if err != nil {
		return result, err
	}

	// The factory and the implementation it creates.
	factory, err := chain.code(ctx, upgradeCreate2Factory)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(factory, hexutil.MustDecode(upgradeCreate2FactoryRuntime)) {
		return result, upgradeRefusal("the deterministic deployment proxy is missing or its code differs")
	}
	implementation := upgrade.artifact.address(upgrade.salt)
	expected := upgrade.artifact.runtimeAt(implementation)
	created, err := chain.code(ctx, implementation)
	if err != nil {
		return result, err
	}
	if len(created) != 0 && !bytes.Equal(created, expected) {
		return result, upgradeRefusal(fmt.Sprintf("the code at %s is not the reviewed implementation's (code hash %s, want %s)", implementation.Hex(), crypto.Keccak256Hash(created).Hex(), crypto.Keccak256Hash(expected).Hex()))
	}
	createData := append(upgrade.salt.Bytes(), upgrade.artifact.creation...)

	// The proxy: owned by the Safe or the timelock, not yet on the
	// implementation.
	current, err := readSettlement2Proxy(ctx, chain, upgrade.settlement, upgrade.governance)
	if err != nil {
		return result, err
	}
	if current == implementation {
		return result, upgradeRefusal(fmt.Sprintf("the proxy already runs the %s implementation", upgrade.artifact.kind()))
	}
	// The phase C artifact must not replace the capture the flag
	// implementation, which keeps every phase C call: its ctfPoolId answers.
	if !upgrade.artifact.ctf {
		settlementAbi := stabi.NewSTSettlement2()
		_, err := ownerCallView(ctx, chain, upgrade.settlement, settlementAbi.PackCtfPoolId(common.Big1), settlementAbi.UnpackCtfPoolId)
		switch {
		case err == nil:
			return result, upgradeRefusal(fmt.Sprintf("the proxy runs the capture the flag implementation %s: the phase C artifact's upgrade would replace it and drop its CTF calls", current.Hex()))
		case !errors.Is(err, errUpgradeRefused):
			return result, err
		}
	}
	review := settlement2UpgradeReview{
		Schema:    settlement2UpgradeReviewSchema,
		ChainId:   mainnetEvmChainId,
		HeadBlock: head,
		Implementation: settlement2UpgradeCreation{
			Factory: upgradeCreate2Factory.Hex(), Salt: upgrade.salt.Hex(), Address: implementation.Hex(),
			CreationBytes: len(upgrade.artifact.creation), CreationKeccak: crypto.Keccak256Hash(upgrade.artifact.creation).Hex(),
			RuntimeBytes: len(expected), RuntimeCodeHash: crypto.Keccak256Hash(expected).Hex(), Created: len(created) != 0,
			CreateTo: upgradeCreate2Factory.Hex(), CreateValueWei: "0", CreateData: hexutil.Encode(createData), CreateDataKeccak: crypto.Keccak256Hash(createData).Hex(),
		},
		Settlement2: settlement2UpgradeProxy{Proxy: upgrade.settlement.Hex(), Owner: upgrade.governance.owner().Hex(), CurrentImplementation: current.Hex()},
	}

	// The owner call: upgradeToAndCall(implementation, 0x) on the proxy. The
	// capture the flag upgrade's is upgradeToAndCall(implementation,
	// registerCtfPool(noId, maximumBurnRao, firstEpoch)) with its value, once
	// the registration passes its checks at the head.
	settlementAbi := stabi.NewSTSettlement2()
	callData, value := []byte{}, new(big.Int)
	notCreated := "not run: the implementation does not exist yet; create it, then review again before signing"
	var schedulable func(uint64, *big.Int) error
	if upgrade.ctf != nil {
		if !upgrade.governance.timelocked() {
			return result, errors.New("the capture the flag upgrade goes only through the timelock")
		}
		ctf, balance, ready, err := reviewSettlement2CtfRegistration(ctx, chain, head, upgrade)
		if err != nil {
			return result, err
		}
		callData = settlementAbi.PackRegisterCtfPool(new(big.Int).SetUint64(upgrade.ctf.noId), upgrade.ctf.maximumBurnRao, upgrade.ctf.firstEpoch)
		value, schedulable = upgrade.ctf.value(), ready
		review.Schema, review.Ctf = settlement2CtfUpgradeReviewSchema, &ctf
		if balance.Cmp(value) < 0 {
			notCreated = fmt.Sprintf("not run: the implementation does not exist yet and the timelock holds less than the call's value; create it, send the timelock at least %s wei, then review again before signing", new(big.Int).Sub(value, balance))
		}
	}
	upgradeData := settlementAbi.PackUpgradeToAndCall(implementation, callData)
	function, args := "upgradeToAndCall(address,bytes)", map[string]string{"newImplementation": implementation.Hex(), "data": hexutil.Encode(callData)}

	// Through the timelock, the Safe schedules the call. Its preflights wait
	// for the implementation, except the execute step's, which then refuse.
	if upgrade.governance.timelocked() {
		call := settlement2TimelockCall{Target: upgrade.settlement.Hex(), ValueWei: value.String(), Data: hexutil.Encode(upgradeData), Function: function, Args: args}
		timelocked, err := reviewSettlement2Timelock(ctx, chain, upgrade.governance, upgrade.settlement, call, upgradeData, value, len(created) != 0 || upgrade.governance.execute, schedulable)
		if err != nil {
			return result, err
		}
		if !timelocked.preflight.Ran {
			timelocked.preflight.Result, timelocked.timelock.CallPreflight.Result = notCreated, notCreated
		}
		if review.Ctf == nil {
			review.Schema = settlement2UpgradeTimelockReviewSchema
		}
		review.Timelock, review.Preflight = &timelocked.timelock, timelocked.preflight
		review.Safe, review.SafeTx, review.OwnerTx = timelocked.safe, timelocked.safeTx, timelocked.ownerTx
		return review, ctx.Err()
	}

	// The Safe transaction: the call on the proxy, at the Safe's nonce, with
	// no refund.
	safe, owner, nonce, err := readUpgradeSafe(ctx, chain, upgrade.governance.safe)
	if err != nil {
		return result, err
	}
	safeTx, ownerTx, execData, err := buildOwnerSafeCall(ctx, chain, upgrade.governance.safe, owner, nonce, upgrade.settlement, upgradeData, function, args)
	if err != nil {
		return result, err
	}

	// Once the implementation exists, the owner's execTransaction must succeed.
	review.Preflight = settlement2UpgradePreflight{Result: notCreated}
	if len(created) != 0 {
		if err := preflightOwnerCall(ctx, chain, owner, upgrade.governance.safe, execData); err != nil {
			return result, err
		}
		review.Preflight = settlement2UpgradePreflight{Ran: true, Result: "execTransaction from the owner returns true at the head"}
	}
	review.Safe, review.SafeTx, review.OwnerTx = &safe, &safeTx, &ownerTx
	return review, ctx.Err()
}

// reviewSettlement2CtfRegistration checks the capture the flag upgrade's
// registerCtfPool call against the chain at the head, through the proxy's
// vault() and v1Coordinator(), as registerCtfPool checks it at execution: the
// proxy runs phase C, on which the upgrade is built; vault2 holds the
// operator's provider pool and not yet its CTF pool; the v1 operator is active
// in the current epoch; the first epoch has not started, nor starts at the
// next block, the earliest any execution lands in; and the first epoch's
// policy leaves a root committed at the end of its window time to clear
// vault2's challenge window. It returns the ctf section, the timelock's
// balance, and the schedule step's check that the operation is ready before
// the first epoch's estimated start.
func reviewSettlement2CtfRegistration(ctx context.Context, chain *upgradeChain, head uint64, upgrade settlement2Upgrade) (settlement2CtfUpgrade, *big.Int, func(uint64, *big.Int) error, error) {
	registration := *upgrade.ctf
	noId, poolId, firstEpoch := new(big.Int).SetUint64(registration.noId), registration.poolId(), new(big.Int).SetUint64(registration.firstEpoch)
	section := settlement2CtfUpgrade{NoId: registration.noId, PoolId: poolId.String(), MaximumBurnRao: strconv.FormatUint(registration.maximumBurnRao, 10),
		ValueWei: registration.value().String(), FirstEpoch: registration.firstEpoch, HotkeyDerivation: settlement2CtfHotkeyDerivation}
	fail := func(err error) (settlement2CtfUpgrade, *big.Int, func(uint64, *big.Int) error, error) {
		return section, nil, nil, err
	}
	refuse := func(reason string) (settlement2CtfUpgrade, *big.Int, func(uint64, *big.Int) error, error) {
		return fail(upgradeRefusal(reason))
	}

	// The proxy runs phase C: v1Sweep answers.
	if _, err := upgradeView(ctx, chain, upgrade.settlement, settlement2PhaseCAbi, "v1Sweep", noId); err != nil {
		if !errors.Is(err, errUpgradeRefused) {
			return fail(err)
		}
		return refuse(fmt.Sprintf("the proxy does not run the phase C implementation, on which the capture the flag upgrade is built: %v", err))
	}
	settlementAbi, vaultAbi, coordinator := stabi.NewSTSettlement2(), stabi.NewSTSettlementVault2(), stabi.NewSTCoordinator()
	vault2, err := ownerCallView(ctx, chain, upgrade.settlement, settlementAbi.PackVault(), settlementAbi.UnpackVault)
	if err != nil {
		return fail(err)
	}
	v1, err := ownerCallView(ctx, chain, upgrade.settlement, settlementAbi.PackV1Coordinator(), settlementAbi.UnpackV1Coordinator)
	if err != nil {
		return fail(err)
	}
	section.Vault2, section.V1Coordinator = vault2.Hex(), v1.Hex()

	// vault2 holds the operator's provider pool, and no CTF pool of it yet.
	provider, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackPools(noId), vaultAbi.UnpackPools)
	if err != nil {
		return fail(err)
	}
	if !provider.Registered {
		return refuse(fmt.Sprintf("vault2 has no provider pool %d, which registerCtfPool requires (UnknownPool)", registration.noId))
	}
	section.ProviderPoolHotkey = common.Hash(provider.Hotkey).Hex()
	held, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackPools(poolId), vaultAbi.UnpackPools)
	if err != nil {
		return fail(err)
	}
	if held.Registered {
		return refuse(fmt.Sprintf("vault2 already holds the CTF pool %s, 2^32 + %d, from epoch %d under hotkey %s", poolId, registration.noId, held.FirstEpoch, common.Hash(held.Hotkey).Hex()))
	}

	// The v1 operator is active now, and the first epoch is still to come.
	current, err := ownerCallView(ctx, chain, v1, coordinator.PackCurrentEpoch(), coordinator.UnpackCurrentEpoch)
	if err != nil {
		return fail(err)
	}
	if !current.IsUint64() {
		return refuse(fmt.Sprintf("currentEpoch() of %s answered %s", v1.Hex(), current))
	}
	section.CurrentEpoch = current.Uint64()
	operator, err := ownerCallView(ctx, chain, v1, coordinator.PackOperatorAt(noId, current), coordinator.UnpackOperatorAt)
	if err != nil {
		return fail(err)
	}
	if !operator.Active {
		return refuse(fmt.Sprintf("the v1 operator %d is not active in the current epoch %d (InactiveOperator)", registration.noId, section.CurrentEpoch))
	}
	start, err := ownerCallView(ctx, chain, v1, coordinator.PackEpochStartBlock(firstEpoch), coordinator.UnpackEpochStartBlock)
	if err != nil {
		return fail(err)
	}
	if !start.IsUint64() {
		return refuse(fmt.Sprintf("epochStartBlock(%d) of %s answered %s", registration.firstEpoch, v1.Hex(), start))
	}
	section.FirstEpochStartBlock = start.Uint64()
	switch {
	case section.CurrentEpoch >= registration.firstEpoch || section.FirstEpochStartBlock <= head:
		return refuse(fmt.Sprintf("the first epoch %d has started: the current epoch is %d, and registerCtfPool requires a later first epoch (InvalidEpoch)", registration.firstEpoch, section.CurrentEpoch))
	case section.FirstEpochStartBlock == head+1:
		return refuse(fmt.Sprintf("the first epoch %d starts at block %d, the next block: no execution lands before it, and registerCtfPool would revert InvalidEpoch", registration.firstEpoch, section.FirstEpochStartBlock))
	}
	section.BlocksRemaining = section.FirstEpochStartBlock - head

	// The first epoch's cadence, which registerCtfPool checks as
	// registerOperatorPool does.
	policy, err := ownerCallView(ctx, chain, v1, coordinator.PackPolicyAt(firstEpoch), coordinator.UnpackPolicyAt)
	if err != nil {
		return fail(err)
	}
	challenge, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackCHALLENGEBLOCKS(), vaultAbi.UnpackCHALLENGEBLOCKS)
	if err != nil {
		return fail(err)
	}
	if new(big.Int).SetUint64(policy.EpochBlocks).Cmp(new(big.Int).Add(new(big.Int).SetUint64(policy.RootCommitWindowBlocks), new(big.Int).SetUint64(challenge))) < 0 {
		return refuse(fmt.Sprintf("the first epoch's v1 policy has %d-block epochs, shorter than its %d-block commit window plus vault2's %d-block challenge window (CadenceTooShort)", policy.EpochBlocks, policy.RootCommitWindowBlocks, challenge))
	}

	// The timelock pays the call's value; reviewSettlement2Timelock requires
	// it once the call's preflight runs.
	balance, err := chain.balance(ctx, upgrade.governance.timelock)
	if err != nil {
		return fail(err)
	}
	section.TimelockBalanceWei = balance.String()

	// A schedule must make the operation ready before the first epoch's
	// estimated start, the head's time plus its remaining blocks at the
	// measured block time: executed from then on, registerCtfPool reverts
	// InvalidEpoch.
	schedulable := func(now uint64, earliest *big.Int) error {
		starts := new(big.Int).Mul(new(big.Int).SetUint64(section.BlocksRemaining), big.NewInt(settlement2BlockSeconds))
		starts.Add(starts, new(big.Int).SetUint64(now))
		if earliest.Cmp(starts) < 0 {
			return nil
		}
		return upgradeRefusal(fmt.Sprintf("the operation is ready at %s at the earliest, at or after the first epoch %d's estimated start at %s (block %d, %d blocks after the head at %d s each), when registerCtfPool reverts InvalidEpoch: schedule it sooner or choose a later first epoch",
			settlement2TimelockTime(earliest), registration.firstEpoch, settlement2TimelockTime(starts), section.FirstEpochStartBlock, section.BlocksRemaining, settlement2BlockSeconds))
	}
	return section, balance, schedulable, nil
}
