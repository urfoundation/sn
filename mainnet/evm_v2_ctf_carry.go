// The capture the flag carry contract (CAPTURETHEFLAG.md §4.6, §11.2) is
// created once per CTF pool, after the timelock operation that registers the
// pool, through the canonical deterministic deployment proxy, with its seed as
// the call's value. Its address is CREATE2(factory, salt, keccak256(STCtfCarry
// creation code ‖ abi.encode(vault2, 2^32 + noId))): it depends on the exact
// build, which the amendment entry v2 then names. Its constructor stakes the
// seed on vault2's escrow hotkey as its residual, makes the pool hotkey its
// auto-stake destination, and sets the runtime's opt-outs of third-party
// staking-hotkey growth and of locked alpha, so no third party can block
// vault2's claims to it or its own moves.
//
// review pins the artifact and derives the address from it, the salt, and
// vault2 and the CTF pool, which it reads through STSettlement2 at the head.
// Before the carry exists it requires the carry's coldkey to be untouched (no
// staking hotkeys, owned hotkeys or lock: a coldkey swap prepared into it, or
// stake sent to it, means its address is known) and already a hotkey account
// of the guardian named by --guardian, which refuses every coldkey swap into
// it and every reservation of it, as must vault2's coldkey be; a pool hotkey
// that holds a UID, which the auto-stake destination needs; the
// deterministic deployment proxy's account holding TAO above its existential
// deposit, without which the creation's value cannot pass through it; a seed
// of at least the contract's seedMinimum() at the head; and, with --from, the
// creation to succeed as an eth_call from that sender with its value and to
// answer the address. Once the carry exists it requires the artifact's code
// outside the immutables, immutables that name vault2, the pool and the
// carry's own coldkey, a residual held on the escrow hotkey, the pool hotkey
// as its auto-stake destination, both on its staking-hotkey list, both
// opt-outs, both coldkeys still the guardian's hotkey accounts, and no hotkey
// of its own. It neither signs nor sends, and every read is pinned to one
// head block.
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
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

const ctfCarryReviewSchema = "urnetwork-mainnet-ctf-carry-review-v1"

// The carry's seed floor and its multiple of the nominator minimum, as
// STCtfCarry declares them; review checks the created contract's against them.
const (
	ctfCarryMinimumSeedRao     = 100_000_000
	ctfCarrySeedMultiple       = 10
	ctfCarryExistentialDeposit = 500
	ctfCarryStakingHotkeyPage  = 64
)

// The functions an STCtfCarry artifact must declare, and its immutables, one
// per view: an artifact without them is not the reviewed carry.
var ctfCarryFunctions = []string{
	"recycle(uint256,(uint256,bytes32,uint256),(bytes32,uint256)[])", "recycledFor(uint256)", "seed()",
	"residual()", "seedMinimum()", "MINIMUM_SEED_RAO()", "SEED_NOMINATOR_MULTIPLE()", "coldkey()",
	"vault()", "poolId()", "netuid()", "escrowHotkey()", "poolHotkey()", "vaultColdkey()",
}

const ctfCarryImmutables = 7

// The runtime views review reads: the staking precompile's (runtime 486,
// precompiles/src/staking.rs) and the alpha precompile's price.
const ctfCarryRuntimeAbiJson = `[
 {"type":"function","name":"getStake","stateMutability":"view","inputs":[{"name":"hotkey","type":"bytes32"},{"name":"coldkey","type":"bytes32"},{"name":"netuid","type":"uint256"}],"outputs":[{"name":"","type":"uint256"}]},
 {"type":"function","name":"getNominatorMinRequiredStake","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
 {"type":"function","name":"getStakingHotkeys","stateMutability":"view","inputs":[{"name":"coldkey","type":"bytes32"},{"name":"offset","type":"uint64"},{"name":"limit","type":"uint16"}],"outputs":[{"name":"hotkeys","type":"bytes32[]"},{"name":"total","type":"uint64"}]},
 {"type":"function","name":"getOwnedHotkeys","stateMutability":"view","inputs":[{"name":"coldkey","type":"bytes32"}],"outputs":[{"name":"","type":"bytes32[]"}]},
 {"type":"function","name":"getColdkeyLock","stateMutability":"view","inputs":[{"name":"coldkey","type":"bytes32"},{"name":"netuid","type":"uint256"}],"outputs":[{"name":"lockInfo","type":"tuple","components":[{"name":"exists","type":"bool"},{"name":"hotkey","type":"bytes32"},{"name":"locked_mass","type":"uint256"},{"name":"conviction","type":"uint128"},{"name":"is_perpetual","type":"bool"}]}]},
 {"type":"function","name":"getRejectStakingHotkeyGrowth","stateMutability":"view","inputs":[{"name":"coldkey","type":"bytes32"}],"outputs":[{"name":"","type":"bool"}]},
 {"type":"function","name":"getRejectLockedAlpha","stateMutability":"view","inputs":[{"name":"coldkey","type":"bytes32"}],"outputs":[{"name":"","type":"bool"}]},
 {"type":"function","name":"getHotkeyOwner","stateMutability":"view","inputs":[{"name":"hotkey","type":"bytes32"}],"outputs":[{"name":"exists","type":"bool"},{"name":"owner","type":"bytes32"}]},
 {"type":"function","name":"getAutoStakeDestination","stateMutability":"view","inputs":[{"name":"coldkey","type":"bytes32"},{"name":"netuid","type":"uint16"}],"outputs":[{"name":"exists","type":"bool"},{"name":"hotkey","type":"bytes32"}]},
 {"type":"function","name":"getUid","stateMutability":"view","inputs":[{"name":"netuid","type":"uint16"},{"name":"hotkey","type":"bytes32"}],"outputs":[{"name":"exists","type":"bool"},{"name":"uid","type":"uint16"}]},
 {"type":"function","name":"getAlphaPrice","stateMutability":"view","inputs":[{"name":"netuid","type":"uint16"}],"outputs":[{"name":"","type":"uint256"}]}
]`

var ctfCarryRuntimeAbi = func() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(ctfCarryRuntimeAbiJson))
	if err != nil {
		panic(err)
	}
	return parsed
}()

var (
	ctfCarryNeuron  = common.HexToAddress("0x0000000000000000000000000000000000000804")
	ctfCarryStaking = common.HexToAddress("0x0000000000000000000000000000000000000805")
	ctfCarryAlpha   = common.HexToAddress("0x0000000000000000000000000000000000000808")
)

// ctfCarryArtifact is STCtfCarry's forge build: its ABI, creation code, and
// runtime code with the immutables' ranges, which hold zeros.
type ctfCarryArtifact struct {
	abi        abi.ABI
	creation   []byte
	runtime    []byte
	immutables [][2]int
}

// loadCtfCarryArtifact reads a forge artifact of STCtfCarry: it must declare
// every carry function and exactly its seven immutables, each a zero 32-byte
// word of the runtime code.
func loadCtfCarryArtifact(raw []byte) (ctfCarryArtifact, error) {
	var result ctfCarryArtifact
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
	for _, signature := range ctfCarryFunctions {
		if !declared[signature] {
			return result, fmt.Errorf("the artifact lacks %s: it is not the reviewed STCtfCarry", signature)
		}
	}
	if constructor := result.abi.Constructor; len(constructor.Inputs) != 2 || constructor.Inputs[0].Type.T != abi.AddressTy ||
		constructor.Inputs[1].Type.String() != "uint256" || !constructor.IsPayable() {
		return result, errors.New("the artifact's constructor is not the payable (address vault, uint256 poolId)")
	}
	creation, creationErr := hexutil.Decode(parsed.Bytecode.Object)
	runtime, runtimeErr := hexutil.Decode(parsed.DeployedBytecode.Object)
	if err := errors.Join(creationErr, runtimeErr); err != nil || len(creation) == 0 || len(runtime) == 0 || len(runtime) >= 24_576 || len(creation) >= 49_152 {
		return result, errors.Join(errors.New("the artifact's creation or runtime code is missing, malformed or over the EIP-170/3860 limits"), err)
	}
	if len(parsed.DeployedBytecode.ImmutableReferences) != ctfCarryImmutables {
		return result, fmt.Errorf("the artifact has %d immutables, want STCtfCarry's %d", len(parsed.DeployedBytecode.ImmutableReferences), ctfCarryImmutables)
	}
	for _, references := range parsed.DeployedBytecode.ImmutableReferences {
		for _, reference := range references {
			if reference.Length != 32 || reference.Start < 0 || reference.Start+32 > len(runtime) || !bytes.Equal(runtime[reference.Start:reference.Start+32], make([]byte, 32)) {
				return result, errors.New("an immutable reference is not a zero 32-byte word of the runtime code")
			}
			result.immutables = append(result.immutables, [2]int{reference.Start, reference.Start + 32})
		}
	}
	result.creation, result.runtime = creation, runtime
	return result, nil
}

// initcode is the creation code with the constructor's arguments.
func (self ctfCarryArtifact) initcode(vault2 common.Address, poolId *big.Int) ([]byte, []byte, error) {
	args, err := self.abi.Pack("", vault2, poolId)
	if err != nil {
		return nil, nil, err
	}
	return append(bytes.Clone(self.creation), args...), args, nil
}

// matches reports whether code is the artifact's runtime code outside the
// immutables' words, which the constructor fills.
func (self ctfCarryArtifact) matches(code []byte) bool {
	if len(code) != len(self.runtime) {
		return false
	}
	masked := bytes.Clone(code)
	for _, word := range self.immutables {
		copy(masked[word[0]:word[1]], make([]byte, 32))
	}
	return bytes.Equal(masked, self.runtime)
}

// One carry review request.
type ctfCarryRequest struct {
	settlement common.Address
	noId       uint64
	salt       common.Hash
	seedRao    uint64
	guardian   common.Hash
	from       *common.Address
	artifact   ctfCarryArtifact
}

// The review's result. Amounts are decimal strings; data is hex. Only a
// review of a created carry has the created section.
type ctfCarryReview struct {
	Schema    string                      `json:"schema"`
	ChainId   uint64                      `json:"chain_id"`
	HeadBlock uint64                      `json:"head_block"`
	Carry     ctfCarryCreation            `json:"carry"`
	Pool      ctfCarryPool                `json:"pool"`
	Runtime   ctfCarryRuntime             `json:"runtime"`
	Created   *ctfCarryCreated            `json:"created,omitempty"`
	Preflight settlement2UpgradePreflight `json:"preflight"`
}

type ctfCarryCreation struct {
	Factory               string `json:"factory"`
	Salt                  string `json:"salt"`
	Address               string `json:"address"`
	Coldkey               string `json:"coldkey"`
	CreationBytes         int    `json:"creation_bytes"`
	CreationKeccak        string `json:"creation_keccak256"`
	ConstructorArgs       string `json:"constructor_args"`
	InitcodeKeccak        string `json:"initcode_keccak256"`
	RuntimeBytes          int    `json:"runtime_bytes"`
	Exists                bool   `json:"exists"`
	SeedRao               string `json:"seed_rao"`
	ExistentialDepositRao uint64 `json:"existential_deposit_rao"`
	CreateTo              string `json:"create_to"`
	CreateValueWei        string `json:"create_value_wei"`
	CreateData            string `json:"create_data"`
	CreateDataKeccak      string `json:"create_data_keccak256"`
}

type ctfCarryPool struct {
	Settlement2  string `json:"settlement2"`
	Vault2       string `json:"vault2"`
	NoId         uint64 `json:"no_id"`
	PoolId       string `json:"pool_id"`
	PoolHotkey   string `json:"pool_hotkey"`
	FirstEpoch   uint64 `json:"first_epoch"`
	Netuid       uint16 `json:"netuid"`
	EscrowHotkey string `json:"escrow_hotkey"`
	VaultColdkey string `json:"vault_coldkey"`
}

// What the runtime says of the carry's coldkey and the seed at the head.
type ctfCarryRuntime struct {
	NominatorMinimumRao string `json:"nominator_minimum_rao"`
	SeedMinimumRao      string `json:"seed_minimum_rao"`
	AlphaPriceWei       string `json:"alpha_price_wei"`
	AddressBalanceWei   string `json:"address_balance_wei"`
	StakingHotkeys      uint64 `json:"staking_hotkeys"`
	OwnedHotkeys        int    `json:"owned_hotkeys"`
	Lock                bool   `json:"lock"`
	PoolHotkeyUid       *int   `json:"pool_hotkey_uid"`
	// The guardian, and the coldkeys of the hotkey accounts the carry's
	// coldkey and vault2's are, or null.
	Guardian                string  `json:"guardian"`
	ColdkeyHotkeyOwner      *string `json:"coldkey_hotkey_owner"`
	VaultColdkeyHotkeyOwner *string `json:"vault_coldkey_hotkey_owner"`
	// The proxy's mapped account, and its balance above the existential deposit.
	ProxyAccount    string `json:"proxy_account"`
	ProxyBalanceWei string `json:"proxy_balance_wei"`
}

type ctfCarryCreated struct {
	RuntimeCodeHash     string `json:"runtime_code_hash"`
	ResidualRao         string `json:"residual_rao"`
	EscrowPositionRao   string `json:"escrow_position_rao"`
	ResidualTaoRao      string `json:"residual_tao_rao"`
	EscrowListed        bool   `json:"escrow_listed"`
	PoolHotkeyPinned    bool   `json:"pool_hotkey_pinned"`
	PoolListed          bool   `json:"pool_listed"`
	RejectsGrowth       bool   `json:"rejects_staking_hotkey_growth"`
	RejectsLockedAlpha  bool   `json:"rejects_locked_alpha"`
	AmendmentCarryEntry string `json:"amendment_carry"`
}

func runCtfCarryCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	const usage = "usage: ctf-carry review --rpc URL --settlement2 ADDRESS --no-id N --artifact evm/out/STCtfCarry.sol/STCtfCarry.json --salt 0xSALT --seed-rao RAO --guardian 0xCOLDKEY [--from ADDRESS]"
	if len(args) == 0 || args[0] != "review" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	flags := flag.NewFlagSet("ctf-carry review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var settlement, artifactPath, salt, guardian, from string
	var request ctfCarryRequest
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) EVM JSON-RPC URL")
	flags.StringVar(&settlement, "settlement2", "", "the STSettlement2 proxy, running STSettlement2Ctf")
	flags.Uint64Var(&request.noId, "no-id", 0, "the v1 operator, 1 to 2^32 - 1, whose CTF pool 2^32 + N the carry serves")
	flags.StringVar(&artifactPath, "artifact", "", "the forge artifact of STCtfCarry (evm/out/STCtfCarry.sol/STCtfCarry.json)")
	flags.StringVar(&salt, "salt", "", "the CREATE2 salt: 0x and 32 bytes of hex, random and private until the creation")
	flags.Uint64Var(&request.seedRao, "seed-rao", 0, "the TAO in rao the constructor stakes as the residual; the creation's value adds the existential deposit")
	flags.StringVar(&guardian, "guardian", "", "the guardian coldkey's public key, 0x and 32 bytes of hex, whose hotkey accounts the carry's coldkey and vault2's must be")
	flags.StringVar(&from, "from", "", "the creator: the preflight runs the creation as an eth_call from it")
	complete := flags.Parse(args[1:]) == nil && flags.NArg() == 0
	set := map[string]bool{}
	flags.Visit(func(given *flag.Flag) { set[given.Name] = true })
	for _, required := range []string{"rpc", "settlement2", "no-id", "artifact", "salt", "seed-rao", "guardian"} {
		complete = complete && set[required]
	}
	var settlementOk, fromOk bool
	request.settlement, settlementOk = poolAddress(settlement)
	request.salt = common.HexToHash(salt)
	request.guardian = common.HexToHash(guardian)
	fromOk = !set["from"]
	if set["from"] {
		var creator common.Address
		creator, fromOk = poolAddress(from)
		request.from = &creator
	}
	if !complete || !settlementOk || !fromOk || !validHash(salt) || !validHash(guardian) || request.guardian == (common.Hash{}) ||
		request.noId == 0 || request.noId >= 1<<32 || request.seedRao == 0 {
		fmt.Fprintln(stderr, "ctf-carry review requires --rpc, --settlement2, --no-id, --artifact, --salt, --seed-rao and --guardian: a nonzero 0x address, an operator of 1 to 2^32 - 1, an artifact path, a 0x 32-byte salt, a positive seed in rao and a nonzero 0x 32-byte coldkey; --from, when given, is another nonzero 0x address")
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if request.salt == (common.Hash{}) {
		fmt.Fprintln(stderr, "ctf-carry review: the zero salt makes the carry's address known before it exists; choose a random salt and keep it private until the creation")
		return 2
	}
	raw, err := os.ReadFile(artifactPath)
	if err == nil {
		request.artifact, err = loadCtfCarryArtifact(raw)
	}
	if err != nil {
		fmt.Fprintln(stderr, "ctf-carry review: artifact:", err)
		return 2
	}
	client, err := newRpcClient(*rpcUrl, 60*time.Second)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	review, err := reviewCtfCarry(ctx, client, request)
	if err != nil {
		fmt.Fprintln(stderr, "ctf-carry review:", err)
		if errors.Is(err, errUpgradeRefused) {
			return 3
		}
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(review); err != nil {
		fmt.Fprintln(stderr, "ctf-carry review output:", err)
		return 1
	}
	return 0
}

// ctfCarryRefusal is a refusal of the carry review, which exits 3.
func ctfCarryRefusal(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errUpgradeRefused, fmt.Sprintf(format, args...))
}

// ctfCarryRead is one view of the runtime's precompiles at the head.
func ctfCarryRead(ctx context.Context, chain *upgradeChain, to common.Address, method string, args ...any) ([]any, error) {
	return upgradeView(ctx, chain, to, ctfCarryRuntimeAbi, method, args...)
}

func reviewCtfCarry(ctx context.Context, client *rpcClient, request ctfCarryRequest) (ctfCarryReview, error) {
	var result ctfCarryReview
	chain, head, err := openUpgradeChain(ctx, client)
	if err != nil {
		return result, err
	}
	factory, err := chain.code(ctx, upgradeCreate2Factory)
	if err != nil {
		return result, err
	}
	if !bytes.Equal(factory, hexutil.MustDecode(upgradeCreate2FactoryRuntime)) {
		return result, ctfCarryRefusal("the deterministic deployment proxy is missing or its code differs")
	}

	// The CTF pool, through STSettlement2Ctf, and what vault2 says of it.
	pool, err := reviewCtfCarryPool(ctx, chain, request)
	if err != nil {
		return result, err
	}
	vault2 := common.HexToAddress(pool.Vault2)
	poolId := new(big.Int).Add(settlement2CtfPoolBase, new(big.Int).SetUint64(request.noId))
	escrow, poolHotkey, vaultColdkey := common.HexToHash(pool.EscrowHotkey), common.HexToHash(pool.PoolHotkey), common.HexToHash(pool.VaultColdkey)

	// The address the artifact, the salt and the arguments give.
	initcode, args, err := request.artifact.initcode(vault2, poolId)
	if err != nil {
		return result, err
	}
	address := crypto.CreateAddress2(upgradeCreate2Factory, request.salt, crypto.Keccak256(initcode))
	coldkey := common.Hash(ss58.EvmMirrorPubkey([20]byte(address)))
	createData := append(request.salt.Bytes(), initcode...)
	value := new(big.Int).Mul(new(big.Int).SetUint64(request.seedRao+ctfCarryExistentialDeposit), big.NewInt(1_000_000_000))
	code, err := chain.code(ctx, address)
	if err != nil {
		return result, err
	}
	result = ctfCarryReview{
		Schema: ctfCarryReviewSchema, ChainId: mainnetEvmChainId, HeadBlock: head, Pool: pool,
		Carry: ctfCarryCreation{
			Factory: upgradeCreate2Factory.Hex(), Salt: request.salt.Hex(), Address: address.Hex(), Coldkey: coldkey.Hex(),
			CreationBytes: len(request.artifact.creation), CreationKeccak: crypto.Keccak256Hash(request.artifact.creation).Hex(),
			ConstructorArgs: hexutil.Encode(args), InitcodeKeccak: crypto.Keccak256Hash(initcode).Hex(), RuntimeBytes: len(request.artifact.runtime),
			Exists: len(code) != 0, SeedRao: strconv.FormatUint(request.seedRao, 10), ExistentialDepositRao: ctfCarryExistentialDeposit,
			CreateTo: upgradeCreate2Factory.Hex(), CreateValueWei: value.String(), CreateData: hexutil.Encode(createData),
			CreateDataKeccak: crypto.Keccak256Hash(createData).Hex(),
		},
	}

	// The runtime at the head: the seed's least, and the carry coldkey.
	runtime, nominator, price, err := reviewCtfCarryRuntime(ctx, chain, address, coldkey, vaultColdkey, pool.Netuid, poolHotkey)
	if err != nil {
		return result, err
	}
	runtime.Guardian = request.guardian.Hex()
	result.Runtime = runtime
	// vault2's coldkey, before the creation and after: a hotkey account of the
	// guardian's, which nobody else can release together with a coldkey swap
	// into vault2.
	switch {
	case runtime.VaultColdkeyHotkeyOwner == nil:
		return result, ctfCarryRefusal("vault2's coldkey %s is not a hotkey account, so a coldkey swap could merge into it or reserve it: the guardian %s calls try_associate_hotkey(%s), through MevShield.submit_encrypted so that nobody takes it first", vaultColdkey.Hex(), request.guardian.Hex(), vaultColdkey.Hex())
	case *runtime.VaultColdkeyHotkeyOwner != request.guardian.Hex():
		return result, ctfCarryRefusal("vault2's coldkey %s is a hotkey account of %s, not of the guardian %s: its holder can release it together with a coldkey swap into vault2", vaultColdkey.Hex(), *runtime.VaultColdkeyHotkeyOwner, request.guardian.Hex())
	}
	if len(code) != 0 {
		created, err := reviewCreatedCtfCarry(ctx, chain, request, address, code, pool, coldkey, escrow, poolHotkey, vaultColdkey, nominator, price)
		if err != nil {
			return result, err
		}
		switch {
		case runtime.ColdkeyHotkeyOwner == nil:
			return result, ctfCarryRefusal("the carry's coldkey %s is not a hotkey account, so a coldkey swap could merge into it or reserve it: the guardian %s calls try_associate_hotkey(%s)", coldkey.Hex(), request.guardian.Hex(), coldkey.Hex())
		case *runtime.ColdkeyHotkeyOwner != request.guardian.Hex():
			return result, ctfCarryRefusal("the carry's coldkey %s is a hotkey account of %s, not of the guardian %s: its holder can release it together with a coldkey swap into the carry", coldkey.Hex(), *runtime.ColdkeyHotkeyOwner, request.guardian.Hex())
		case runtime.OwnedHotkeys != 0:
			return result, ctfCarryRefusal("the carry's coldkey owns %d hotkeys, which only a coldkey swap into it gives", runtime.OwnedHotkeys)
		}
		result.Created = created
		result.Preflight = settlement2UpgradePreflight{Result: "not run: the carry exists; its code, residual and opt-outs are checked above"}
		return result, ctx.Err()
	}

	// Before the creation: an untouched coldkey, a seed of at least the
	// least, and the creation itself as an eth_call.
	switch {
	case runtime.StakingHotkeys != 0:
		return result, ctfCarryRefusal("the carry's coldkey %s already holds %d staking hotkeys: someone knows its address; create it under another salt", coldkey.Hex(), runtime.StakingHotkeys)
	case runtime.OwnedHotkeys != 0:
		return result, ctfCarryRefusal("the carry's coldkey %s already owns %d hotkeys, as a coldkey swap into it leaves: create it under another salt", coldkey.Hex(), runtime.OwnedHotkeys)
	case runtime.Lock:
		return result, ctfCarryRefusal("the carry's coldkey %s holds a conviction lock on netuid %d, as a coldkey swap into it leaves: create it under another salt", coldkey.Hex(), pool.Netuid)
	case runtime.PoolHotkeyUid == nil:
		return result, ctfCarryRefusal("the CTF pool hotkey %s holds no UID on netuid %d, which the carry's auto-stake destination needs: re-register it with vault2's registerPoolUid first", poolHotkey.Hex(), pool.Netuid)
	case runtime.ColdkeyHotkeyOwner == nil:
		return result, ctfCarryRefusal("the carry's coldkey %s is not a hotkey account yet: the guardian %s calls try_associate_hotkey(%s) first, through MevShield.submit_encrypted, so that no coldkey swap can merge into it or reserve it", coldkey.Hex(), request.guardian.Hex(), coldkey.Hex())
	case *runtime.ColdkeyHotkeyOwner != request.guardian.Hex():
		return result, ctfCarryRefusal("the carry's coldkey %s is a hotkey account of %s, not of the guardian %s: someone took it first and can release it together with a coldkey swap into it; create the carry under another salt", coldkey.Hex(), *runtime.ColdkeyHotkeyOwner, request.guardian.Hex())
	case runtime.ProxyBalanceWei == "0":
		return result, ctfCarryRefusal("the deterministic deployment proxy's account holds no TAO above its existential deposit, so the creation's value cannot pass through it (OutOfFund): send its mapped account %s at least 501 rao first", runtime.ProxyAccount)
	}
	minimum, _ := new(big.Int).SetString(runtime.SeedMinimumRao, 10)
	if new(big.Int).SetUint64(request.seedRao).Cmp(minimum) < 0 {
		return result, ctfCarryRefusal("a seed of %d rao is below the carry's least seed at the head, %s rao: ten times the nominator minimum of %s rao, and at least %d", request.seedRao, minimum, runtime.NominatorMinimumRao, ctfCarryMinimumSeedRao)
	}
	if request.from == nil {
		result.Preflight = settlement2UpgradePreflight{Result: "not run: name the creator with --from to run the creation as an eth_call"}
		return result, ctx.Err()
	}
	output, err := chain.call(ctx, request.from, value, upgradeCreate2Factory, createData)
	if err != nil {
		return result, err
	}
	if common.BytesToAddress(output) != address || len(output) != common.AddressLength {
		return result, ctfCarryRefusal("the creation from %s answered 0x%x, not the address %s", request.from.Hex(), output, address.Hex())
	}
	result.Preflight = settlement2UpgradePreflight{Ran: true, Result: fmt.Sprintf("the creation from %s with %s wei creates %s at the head", request.from.Hex(), value, address.Hex())}
	return result, ctx.Err()
}

// reviewCtfCarryPool reads the CTF pool through the STSettlement2 proxy, which
// must run STSettlement2Ctf, and vault2's record of it.
func reviewCtfCarryPool(ctx context.Context, chain *upgradeChain, request ctfCarryRequest) (ctfCarryPool, error) {
	pool := ctfCarryPool{Settlement2: request.settlement.Hex(), NoId: request.noId}
	settlementAbi, vaultAbi := stabi.NewSTSettlement2(), stabi.NewSTSettlementVault2()
	noId := new(big.Int).SetUint64(request.noId)
	poolId, err := ownerCallView(ctx, chain, request.settlement, settlementAbi.PackCtfPoolId(noId), settlementAbi.UnpackCtfPoolId)
	if err != nil {
		if errors.Is(err, errUpgradeRefused) {
			return pool, ctfCarryRefusal("STSettlement2 at %s does not answer ctfPoolId: it does not run the capture the flag implementation (%v)", request.settlement.Hex(), err)
		}
		return pool, err
	}
	if want := new(big.Int).Add(settlement2CtfPoolBase, noId); poolId.Cmp(want) != 0 {
		return pool, ctfCarryRefusal("ctfPoolId(%d) answered %s, not 2^32 + %d", request.noId, poolId, request.noId)
	}
	pool.PoolId = poolId.String()
	vault2, err := ownerCallView(ctx, chain, request.settlement, settlementAbi.PackVault(), settlementAbi.UnpackVault)
	if err != nil {
		return pool, err
	}
	pool.Vault2 = vault2.Hex()
	held, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackPools(poolId), vaultAbi.UnpackPools)
	if err != nil {
		return pool, err
	}
	if !held.Registered {
		return pool, ctfCarryRefusal("vault2 %s holds no CTF pool %s: run the timelock operation that registers it first, since the constructor reads its hotkey", vault2.Hex(), poolId)
	}
	resolved, err := ownerCallView(ctx, chain, request.settlement, settlementAbi.PackCtfSettlementAt(noId, new(big.Int).SetUint64(held.FirstEpoch)), settlementAbi.UnpackCtfSettlementAt)
	if err != nil {
		return pool, err
	}
	if resolved.Vault != vault2 || resolved.Hotkey != held.Hotkey || resolved.FirstEpoch != held.FirstEpoch {
		return pool, ctfCarryRefusal("ctfSettlementAt(%d, %d) answers %s, %x from epoch %d; vault2 holds the pool under %x from epoch %d",
			request.noId, held.FirstEpoch, resolved.Vault.Hex(), resolved.Hotkey, resolved.FirstEpoch, held.Hotkey, held.FirstEpoch)
	}
	pool.PoolHotkey, pool.FirstEpoch = common.Hash(held.Hotkey).Hex(), held.FirstEpoch
	if pool.Netuid, err = ownerCallView(ctx, chain, vault2, vaultAbi.PackNetuid(), vaultAbi.UnpackNetuid); err != nil {
		return pool, err
	}
	escrow, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackEscrowHotkey(), vaultAbi.UnpackEscrowHotkey)
	if err != nil {
		return pool, err
	}
	self, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackSelfColdkey(), vaultAbi.UnpackSelfColdkey)
	if err != nil {
		return pool, err
	}
	pool.EscrowHotkey, pool.VaultColdkey = common.Hash(escrow).Hex(), common.Hash(self).Hex()
	return pool, nil
}

// reviewCtfCarryRuntime reads the seed's least at the head, the alpha price,
// and what the runtime holds for the carry's coldkey: its staking hotkeys,
// owned hotkeys and lock, the address's balance, which a seed stakes too, and
// the owners of the hotkey accounts the carry's coldkey and vault2's are.
func reviewCtfCarryRuntime(ctx context.Context, chain *upgradeChain, address common.Address, coldkey, vaultColdkey common.Hash, netuid uint16, poolHotkey common.Hash) (ctfCarryRuntime, *big.Int, *big.Int, error) {
	var runtime ctfCarryRuntime
	values, err := ctfCarryRead(ctx, chain, ctfCarryStaking, "getNominatorMinRequiredStake")
	if err != nil {
		return runtime, nil, nil, err
	}
	nominator := values[0].(*big.Int)
	minimum := new(big.Int).Mul(nominator, big.NewInt(ctfCarrySeedMultiple))
	if minimum.Cmp(big.NewInt(ctfCarryMinimumSeedRao)) < 0 {
		minimum = big.NewInt(ctfCarryMinimumSeedRao)
	}
	runtime.NominatorMinimumRao, runtime.SeedMinimumRao = nominator.String(), minimum.String()
	if values, err = ctfCarryRead(ctx, chain, ctfCarryAlpha, "getAlphaPrice", netuid); err != nil {
		return runtime, nil, nil, err
	}
	price := values[0].(*big.Int)
	runtime.AlphaPriceWei = price.String()
	balance, err := chain.balance(ctx, address)
	if err != nil {
		return runtime, nil, nil, err
	}
	runtime.AddressBalanceWei = balance.String()
	if values, err = ctfCarryRead(ctx, chain, ctfCarryStaking, "getStakingHotkeys", [32]byte(coldkey), uint64(0), uint16(1)); err != nil {
		return runtime, nil, nil, err
	}
	runtime.StakingHotkeys = values[1].(uint64)
	if values, err = ctfCarryRead(ctx, chain, ctfCarryStaking, "getOwnedHotkeys", [32]byte(coldkey)); err != nil {
		return runtime, nil, nil, err
	}
	runtime.OwnedHotkeys = len(values[0].([][32]byte))
	if values, err = ctfCarryRead(ctx, chain, ctfCarryStaking, "getColdkeyLock", [32]byte(coldkey), new(big.Int).SetUint64(uint64(netuid))); err != nil {
		return runtime, nil, nil, err
	}
	// The tuple decodes to a struct the ABI package builds; only exists matters.
	exists := reflect.ValueOf(values[0]).FieldByName("Exists")
	if !exists.IsValid() || exists.Kind() != reflect.Bool {
		return runtime, nil, nil, ctfCarryRefusal("getColdkeyLock answered %v, not a lock record", values[0])
	}
	runtime.Lock = exists.Bool()
	for _, account := range []struct {
		key   common.Hash
		owner **string
	}{{coldkey, &runtime.ColdkeyHotkeyOwner}, {vaultColdkey, &runtime.VaultColdkeyHotkeyOwner}} {
		if values, err = ctfCarryRead(ctx, chain, ctfCarryStaking, "getHotkeyOwner", [32]byte(account.key)); err != nil {
			return runtime, nil, nil, err
		}
		if values[0].(bool) {
			owner := common.Hash(values[1].([32]byte)).Hex()
			*account.owner = &owner
		}
	}
	proxyBalance, err := chain.balance(ctx, upgradeCreate2Factory)
	if err != nil {
		return runtime, nil, nil, err
	}
	runtime.ProxyBalanceWei = proxyBalance.String()
	runtime.ProxyAccount = common.Hash(ss58.EvmMirrorPubkey([20]byte(upgradeCreate2Factory))).Hex()
	if values, err = ctfCarryRead(ctx, chain, ctfCarryNeuron, "getUid", netuid, [32]byte(poolHotkey)); err != nil {
		return runtime, nil, nil, err
	}
	if values[0].(bool) {
		uid := int(values[1].(uint16))
		runtime.PoolHotkeyUid = &uid
	}
	return runtime, nominator, price, nil
}

// reviewCreatedCtfCarry checks a created carry: the artifact's code, its
// immutables, its constants, its residual on the escrow hotkey, which its
// list keeps, and both opt-outs.
func reviewCreatedCtfCarry(ctx context.Context, chain *upgradeChain, request ctfCarryRequest, address common.Address, code []byte, pool ctfCarryPool,
	coldkey, escrow, poolHotkey, vaultColdkey common.Hash, nominator, price *big.Int) (*ctfCarryCreated, error) {
	if !request.artifact.matches(code) {
		return nil, ctfCarryRefusal("the code at %s is not the reviewed STCtfCarry's outside its immutables (code hash %s)", address.Hex(), crypto.Keccak256Hash(code).Hex())
	}
	carry := request.artifact.abi
	view := func(method string) (any, error) {
		values, err := upgradeView(ctx, chain, address, carry, method)
		if err != nil {
			return nil, err
		}
		return values[0], nil
	}
	want := map[string]any{
		"vault": common.HexToAddress(pool.Vault2), "poolId": new(big.Int).Add(settlement2CtfPoolBase, new(big.Int).SetUint64(request.noId)),
		"netuid": pool.Netuid, "escrowHotkey": [32]byte(escrow), "poolHotkey": [32]byte(poolHotkey), "vaultColdkey": [32]byte(vaultColdkey),
		"coldkey": [32]byte(coldkey), "MINIMUM_SEED_RAO": big.NewInt(ctfCarryMinimumSeedRao), "SEED_NOMINATOR_MULTIPLE": big.NewInt(ctfCarrySeedMultiple),
	}
	for _, method := range []string{"vault", "poolId", "netuid", "escrowHotkey", "poolHotkey", "vaultColdkey", "coldkey", "MINIMUM_SEED_RAO", "SEED_NOMINATOR_MULTIPLE"} {
		got, err := view(method)
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(got) != fmt.Sprint(want[method]) {
			return nil, ctfCarryRefusal("%s() of %s answers %v, want %v", method, address.Hex(), got, want[method])
		}
	}
	residualValue, err := view("residual")
	if err != nil {
		return nil, err
	}
	residual := residualValue.(*big.Int)
	values, err := ctfCarryRead(ctx, chain, ctfCarryStaking, "getStake", [32]byte(escrow), [32]byte(coldkey), new(big.Int).SetUint64(uint64(pool.Netuid)))
	if err != nil {
		return nil, err
	}
	position := values[0].(*big.Int)
	listed, err := ctfCarryListed(ctx, chain, coldkey, escrow)
	if err != nil {
		return nil, err
	}
	poolListed, err := ctfCarryListed(ctx, chain, coldkey, poolHotkey)
	if err != nil {
		return nil, err
	}
	if values, err = ctfCarryRead(ctx, chain, ctfCarryStaking, "getAutoStakeDestination", [32]byte(coldkey), pool.Netuid); err != nil {
		return nil, err
	}
	pinned := values[0].(bool) && values[1].([32]byte) == [32]byte(poolHotkey)
	if values, err = ctfCarryRead(ctx, chain, ctfCarryStaking, "getRejectStakingHotkeyGrowth", [32]byte(coldkey)); err != nil {
		return nil, err
	}
	rejectsGrowth := values[0].(bool)
	if values, err = ctfCarryRead(ctx, chain, ctfCarryStaking, "getRejectLockedAlpha", [32]byte(coldkey)); err != nil {
		return nil, err
	}
	rejectsLocked := values[0].(bool)
	worth := new(big.Int).Div(new(big.Int).Mul(residual, price), big.NewInt(1_000_000_000_000_000_000))
	created := &ctfCarryCreated{
		RuntimeCodeHash: crypto.Keccak256Hash(code).Hex(), ResidualRao: residual.String(), EscrowPositionRao: position.String(),
		ResidualTaoRao: worth.String(), EscrowListed: listed, PoolHotkeyPinned: pinned, PoolListed: poolListed,
		RejectsGrowth: rejectsGrowth, RejectsLockedAlpha: rejectsLocked,
		AmendmentCarryEntry: address.Hex(),
	}
	switch {
	case residual.Sign() == 0:
		return created, ctfCarryRefusal("the carry at %s keeps no residual: root's sweep cleared it, or it was never seeded; restore it with seed()", address.Hex())
	case position.Cmp(residual) < 0:
		return created, ctfCarryRefusal("the carry holds %s rao on the escrow hotkey, below its residual of %s: the next recycle lowers the residual", position, residual)
	case !listed:
		return created, ctfCarryRefusal("the carry coldkey's staking-hotkey list does not hold the escrow hotkey %s", escrow.Hex())
	case !pinned || !poolListed:
		return created, ctfCarryRefusal("the carry coldkey's auto-stake destination on netuid %d is not the CTF pool hotkey %s (pinned %v, listed %v)", pool.Netuid, poolHotkey.Hex(), pinned, poolListed)
	case !rejectsGrowth || !rejectsLocked:
		return created, ctfCarryRefusal("the carry coldkey's opt-outs are not set: staking-hotkey growth %v, locked alpha %v", rejectsGrowth, rejectsLocked)
	case worth.Cmp(nominator) < 0:
		return created, ctfCarryRefusal("the residual of %s rao of alpha is worth about %s rao of TAO, below the nominator minimum of %s: root's next sweep would clear it; add to it with seed()", residual, worth, nominator)
	}
	return created, nil
}

// ctfCarryListed reports whether coldkey's staking-hotkey list holds hotkey,
// reading it a page at a time.
func ctfCarryListed(ctx context.Context, chain *upgradeChain, coldkey, hotkey common.Hash) (bool, error) {
	for offset := uint64(0); ; offset += ctfCarryStakingHotkeyPage {
		values, err := ctfCarryRead(ctx, chain, ctfCarryStaking, "getStakingHotkeys", [32]byte(coldkey), offset, uint16(ctfCarryStakingHotkeyPage))
		if err != nil {
			return false, err
		}
		page, total := values[0].([][32]byte), values[1].(uint64)
		for _, listed := range page {
			if listed == [32]byte(hotkey) {
				return true, nil
			}
		}
		if len(page) == 0 || offset+uint64(len(page)) >= total {
			return false, nil
		}
	}
}
