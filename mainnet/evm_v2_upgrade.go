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
// pinned to one head block.
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
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/stabi"
)

const settlement2UpgradeReviewSchema = "urnetwork-mainnet-settlement-v2-upgrade-review-v1"

// The canonical deterministic deployment proxy, at its keyless address on
// every chain that has it, and its exact runtime code: called with a 32-byte
// salt followed by creation code, it CREATE2s that code with the salt and
// returns the new address.
var upgradeCreate2Factory = common.HexToAddress("0x4e59b44847b379578588920ca78fbf26c0b4956c")

const upgradeCreate2FactoryRuntime = "0x7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe03601600081602082378035828234f58015156039578182fd5b8082525050506014600cf3"

// The phase C implementation's functions that STSettlement2 lacks. An artifact
// without all three is not the reviewed upgrade.
var upgradePhaseCFunctions = []string{"assignSurplus(uint256,uint256,uint256)", "recordV1Sweep(uint256,uint256,bytes32)", "v1Sweep(uint256)"}

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
// offsets in the runtime code.
type upgradeArtifact struct {
	abi      abi.ABI
	creation []byte
	runtime  []byte
	self     []int
}

// loadUpgradeArtifact reads a forge artifact of STSettlement2PhaseC: it must
// declare the phase C functions and exactly one immutable, UUPSUpgradeable's
// __self, a 32-byte word.
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

// One upgrade review request.
type settlement2Upgrade struct {
	settlement common.Address
	safe       common.Address
	salt       common.Hash
	artifact   upgradeArtifact
}

// The review's result. Every amount is a decimal string; data is hex.
type settlement2UpgradeReview struct {
	Schema         string                      `json:"schema"`
	ChainId        uint64                      `json:"chain_id"`
	HeadBlock      uint64                      `json:"head_block"`
	Implementation settlement2UpgradeCreation  `json:"implementation"`
	Settlement2    settlement2UpgradeProxy     `json:"settlement2"`
	Safe           settlement2UpgradeSafe      `json:"safe"`
	SafeTx         settlement2UpgradeSafeTx    `json:"safe_transaction"`
	OwnerTx        settlement2UpgradeOwnerTx   `json:"owner_transaction"`
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
// exists.
func runUpgradeSettlement2Command(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "review" {
		fmt.Fprintln(stderr, "usage: upgrade-settlement2 review --rpc URL --settlement2 ADDRESS --safe ADDRESS --artifact FORGE-ARTIFACT.json --salt 0xSALT")
		return 2
	}
	flags := flag.NewFlagSet("upgrade-settlement2 review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var settlement, safe, artifactPath, salt string
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) EVM JSON-RPC URL")
	flags.StringVar(&settlement, "settlement2", "", "the STSettlement2 proxy")
	flags.StringVar(&safe, "safe", "", "the governance Safe, which owns STSettlement2")
	flags.StringVar(&artifactPath, "artifact", "", "the forge artifact of STSettlement2PhaseC (evm/out/STSettlement2PhaseC.sol/STSettlement2PhaseC.json)")
	flags.StringVar(&salt, "salt", "", "the CREATE2 salt: 0x and 32 bytes of hex")
	complete := flags.Parse(args[1:]) == nil && flags.NArg() == 0
	set := map[string]bool{}
	flags.Visit(func(given *flag.Flag) { set[given.Name] = true })
	flags.VisitAll(func(defined *flag.Flag) { complete = complete && set[defined.Name] })
	var upgrade settlement2Upgrade
	var settlementOk, safeOk bool
	upgrade.settlement, settlementOk = poolAddress(settlement)
	upgrade.safe, safeOk = poolAddress(safe)
	upgrade.salt = common.HexToHash(salt)
	if !complete || !settlementOk || !safeOk || !validHash(salt) {
		fmt.Fprintln(stderr, "upgrade-settlement2 review requires every flag: nonzero 0x addresses, an artifact path and a 0x 32-byte salt")
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
	case "eth_chainId", "eth_blockNumber", "eth_call", "eth_getCode", "eth_getStorageAt":
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

// call runs one eth_call at the head, from `from` when it is set. A call the
// chain ran and failed is a refusal, never a failed read.
func (self *upgradeChain) call(ctx context.Context, from *common.Address, to common.Address, data []byte) ([]byte, error) {
	message := map[string]any{"to": to.Hex(), "data": hexutil.Encode(data)}
	if from != nil {
		message["from"] = from.Hex()
	}
	var output hexutil.Bytes
	err := self.client.callUpgradeRead(ctx, "eth_call", []any{message, self.head}, &output)
	var failure *rpcCallError
	if errors.As(err, &failure) {
		var encoded string
		_ = json.Unmarshal(failure.data, &encoded)
		revert, decodeErr := hexutil.Decode(encoded)
		if failure.code == 3 || decodeErr == nil || strings.Contains(failure.message, "revert") || strings.Contains(failure.message, "evm error") {
			return nil, upgradeRefusal(fmt.Sprintf("call 0x%x to %s failed with %q and data 0x%x%s", data[:4], to.Hex(), failure.message, revert, upgradeRevertReason(revert)))
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
	output, err := chain.call(ctx, nil, to, data)
	if err != nil {
		return nil, fmt.Errorf("%s of %s: %w", method, to.Hex(), err)
	}
	values, err := contract.Unpack(method, output)
	if err != nil {
		return nil, upgradeRefusal(fmt.Sprintf("%s of %s answered 0x%x, not as its contract does", method, to.Hex(), output))
	}
	return values, nil
}

func reviewSettlement2Upgrade(ctx context.Context, client *rpcClient, upgrade settlement2Upgrade) (settlement2UpgradeReview, error) {
	var result settlement2UpgradeReview
	var chainId hexutil.Uint64
	if err := client.callUpgradeRead(ctx, "eth_chainId", []any{}, &chainId); err != nil {
		return result, err
	}
	if chainId != mainnetEvmChainId {
		return result, upgradeRefusal("the RPC is not EVM chain 964")
	}
	var head hexutil.Uint64
	if err := client.callUpgradeRead(ctx, "eth_blockNumber", []any{}, &head); err != nil {
		return result, err
	}
	chain := &upgradeChain{client: client, head: hexutil.EncodeUint64(uint64(head))}

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

	// The proxy: owned by the Safe, not yet on the implementation.
	settlementAbi := stabi.NewSTSettlement2()
	ownerOutput, err := chain.call(ctx, nil, upgrade.settlement, settlementAbi.PackOwner())
	if err != nil {
		return result, err
	}
	owner, err := settlementAbi.UnpackOwner(ownerOutput)
	if err != nil {
		return result, upgradeRefusal(fmt.Sprintf("owner() of %s answered 0x%x", upgrade.settlement.Hex(), ownerOutput))
	}
	if owner != upgrade.safe {
		return result, upgradeRefusal(fmt.Sprintf("STSettlement2's owner is %s, not the Safe", owner.Hex()))
	}
	slot, err := chain.word(ctx, upgrade.settlement, common.HexToHash(contractImplementationSlot))
	if err != nil {
		return result, err
	}
	current := common.BytesToAddress(slot.Bytes())
	if current == implementation {
		return result, upgradeRefusal("the proxy already runs the phase C implementation")
	}
	if current == (common.Address{}) {
		return result, upgradeRefusal("the proxy has no implementation: it is not the STSettlement2 proxy")
	}

	// The Safe: 1.4.1, one owner, threshold one, no modules and no guard.
	safe := settlement2UpgradeSafe{Address: upgrade.safe.Hex(), Modules: []string{}}
	values, err := upgradeView(ctx, chain, upgrade.safe, upgradeSafeAbi, "VERSION")
	if err != nil {
		return result, err
	}
	safe.Version = values[0].(string)
	values, err = upgradeView(ctx, chain, upgrade.safe, upgradeSafeAbi, "getThreshold")
	if err != nil {
		return result, err
	}
	threshold := values[0].(*big.Int)
	values, err = upgradeView(ctx, chain, upgrade.safe, upgradeSafeAbi, "getOwners")
	if err != nil {
		return result, err
	}
	owners := values[0].([]common.Address)
	values, err = upgradeView(ctx, chain, upgrade.safe, upgradeSafeAbi, "nonce")
	if err != nil {
		return result, err
	}
	nonce := values[0].(*big.Int)
	values, err = upgradeView(ctx, chain, upgrade.safe, upgradeSafeAbi, "getModulesPaginated", upgradeModulesSentinel, big.NewInt(10))
	if err != nil {
		return result, err
	}
	modules := values[0].([]common.Address)
	guard, err := chain.word(ctx, upgrade.safe, upgradeSafeGuardSlot)
	if err != nil {
		return result, err
	}
	handler, err := chain.word(ctx, upgrade.safe, upgradeSafeFallbackSlot)
	if err != nil {
		return result, err
	}
	switch {
	case safe.Version != upgradeSafeVersion:
		return result, upgradeRefusal(fmt.Sprintf("the Safe runs version %q, not %s", safe.Version, upgradeSafeVersion))
	case threshold.Cmp(common.Big1) != 0 || len(owners) != 1:
		return result, upgradeRefusal(fmt.Sprintf("the Safe has threshold %s of %d owners, not one of one", threshold, len(owners)))
	case len(modules) != 0:
		return result, upgradeRefusal(fmt.Sprintf("the Safe has %d modules", len(modules)))
	case guard != (common.Hash{}):
		return result, upgradeRefusal("the Safe has a transaction guard")
	}
	safe.Threshold, safe.Owners, safe.Nonce = threshold.Uint64(), []string{owners[0].Hex()}, nonce.String()
	safe.Guard, safe.FallbackHandler = common.Address{}.Hex(), common.BytesToAddress(handler.Bytes()).Hex()

	// The Safe transaction: a call of upgradeToAndCall(implementation, 0x) on
	// the proxy, at the Safe's nonce, with no refund. The Safe hashes it too.
	upgradeData := settlementAbi.PackUpgradeToAndCall(implementation, []byte{})
	tx := safeTransaction{To: upgrade.settlement, Value: new(big.Int), Data: upgradeData, SafeTxGas: new(big.Int), BaseGas: new(big.Int), GasPrice: new(big.Int), Nonce: nonce}
	domain, message, hash := safeTransactionHash(mainnetEvmChainId, upgrade.safe, tx)
	values, err = upgradeView(ctx, chain, upgrade.safe, upgradeSafeAbi, "getTransactionHash", tx.To, tx.Value, tx.Data, tx.Operation, tx.SafeTxGas, tx.BaseGas, tx.GasPrice, tx.GasToken, tx.RefundReceiver, tx.Nonce)
	if err != nil {
		return result, err
	}
	if values[0].([32]byte) != hash {
		return result, upgradeRefusal(fmt.Sprintf("the Safe hashes the transaction as %x, not %s", values[0], hash.Hex()))
	}
	signature := upgradeOwnerSignature(owners[0])
	execData, err := upgradeSafeAbi.Pack("execTransaction", tx.To, tx.Value, tx.Data, tx.Operation, tx.SafeTxGas, tx.BaseGas, tx.GasPrice, tx.GasToken, tx.RefundReceiver, signature)
	if err != nil {
		return result, err
	}

	// Once the implementation exists, the owner's execTransaction must succeed.
	preflight := settlement2UpgradePreflight{Result: "not run: the implementation does not exist yet; create it, then review again before signing"}
	if len(created) != 0 {
		output, err := chain.call(ctx, &owners[0], upgrade.safe, execData)
		if err != nil {
			return result, err
		}
		success, unpackErr := upgradeSafeAbi.Unpack("execTransaction", output)
		if unpackErr != nil || len(success) != 1 || success[0] != true {
			return result, upgradeRefusal(fmt.Sprintf("the owner's execTransaction answered 0x%x, not true", output))
		}
		preflight = settlement2UpgradePreflight{Ran: true, Result: "execTransaction from the owner returns true at the head"}
	}

	zero := common.Address{}.Hex()
	result = settlement2UpgradeReview{
		Schema:    settlement2UpgradeReviewSchema,
		ChainId:   mainnetEvmChainId,
		HeadBlock: uint64(head),
		Implementation: settlement2UpgradeCreation{
			Factory: upgradeCreate2Factory.Hex(), Salt: upgrade.salt.Hex(), Address: implementation.Hex(),
			CreationBytes: len(upgrade.artifact.creation), CreationKeccak: crypto.Keccak256Hash(upgrade.artifact.creation).Hex(),
			RuntimeBytes: len(expected), RuntimeCodeHash: crypto.Keccak256Hash(expected).Hex(), Created: len(created) != 0,
			CreateTo: upgradeCreate2Factory.Hex(), CreateValueWei: "0", CreateData: hexutil.Encode(createData), CreateDataKeccak: crypto.Keccak256Hash(createData).Hex(),
		},
		Settlement2: settlement2UpgradeProxy{Proxy: upgrade.settlement.Hex(), Owner: owner.Hex(), CurrentImplementation: current.Hex()},
		Safe:        safe,
		SafeTx: settlement2UpgradeSafeTx{
			To: tx.To.Hex(), ValueWei: "0", Data: hexutil.Encode(tx.Data), Operation: tx.Operation, SafeTxGas: "0", BaseGas: "0", GasPrice: "0",
			GasToken: zero, RefundReceiver: zero, Nonce: nonce.String(), Function: "upgradeToAndCall(address,bytes)",
			Args:            map[string]string{"newImplementation": implementation.Hex(), "data": "0x"},
			DomainSeparator: domain.Hex(), MessageHash: message.Hex(), SafeTxHash: hash.Hex(),
		},
		OwnerTx: settlement2UpgradeOwnerTx{
			From: owners[0].Hex(), To: upgrade.safe.Hex(), ValueWei: "0", Data: hexutil.Encode(execData), DataKeccak: crypto.Keccak256Hash(execData).Hex(),
			Function: "execTransaction(address,uint256,bytes,uint8,uint256,uint256,uint256,address,address,bytes)", Signature: hexutil.Encode(signature),
		},
		Preflight: preflight,
	}
	return result, ctx.Err()
}
