// The carry review (CAPTURETHEFLAG.md §4.6, §11.2) against a JSON-RPC chain
// that answers what review reads: STSettlement2Ctf's views at the proxy,
// vault2's, the staking and alpha precompiles', the deterministic deployment
// proxy's code and its CREATE2 as an eth_call, and, once created, the carry's
// code and views. The artifact is the forge build of STCtfCarry. CR-1 is the
// review before the creation, CR-2 its refusals, CR-3 the usage errors, CR-4
// the review of a created carry and CR-5 its refusals.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/ss58"
	"github.com/urfoundation/sn/stabi"
)

const (
	ctfCarryTestNoId       = 1
	ctfCarryTestNetuid     = 25
	ctfCarryTestFirstEpoch = 8
	ctfCarryTestSeed       = 1_000_000_000
	ctfCarryTestResidual   = 987_654_321
)

var (
	ctfCarryTestSettlement = common.HexToAddress("0x5e771e5e771e5e771e5e771e5e771e5e771e0002")
	ctfCarryTestVault2     = common.HexToAddress("0x7a017a017a017a017a017a017a017a017a010002")
	ctfCarryTestCreator    = common.HexToAddress("0xc4ea70c4ea70c4ea70c4ea70c4ea70c4ea700001")
	ctfCarryTestSalt       = crypto.Keccak256Hash([]byte("ctf carry test salt"))
	ctfCarryTestEscrow     = crypto.Keccak256Hash([]byte("escrow hotkey"))
	ctfCarryTestPoolHotkey = crypto.Keccak256Hash([]byte("ctf pool hotkey"))
	ctfCarryTestSelf       = crypto.Keccak256Hash([]byte("vault2 coldkey"))
	ctfCarryTestGuardian   = crypto.Keccak256Hash([]byte("guardian coldkey"))
	ctfCarryTestPoolId     = new(big.Int).Add(new(big.Int).Lsh(common.Big1, 32), big.NewInt(ctfCarryTestNoId))
)

// ctfCarryFake is the chain review reads, which each case changes.
type ctfCarryFake struct {
	t        *testing.T
	artifact ctfCarryArtifact
	// The carry's address and coldkey, from the artifact and the test salt.
	address common.Address
	coldkey [32]byte
	// The pool and the proxy.
	notCtf     bool
	registered bool
	resolved   common.Address
	// The runtime.
	nominator     *big.Int
	price         *big.Int
	stakingLists  map[[32]byte][][32]byte
	owned         map[[32]byte][][32]byte
	locked        map[[32]byte]bool
	rejectsGrowth map[[32]byte]bool
	rejectsLocked map[[32]byte]bool
	stakes        map[[32]byte]*big.Int
	autoStake     map[[32]byte][32]byte
	// noPoolUid takes the CTF pool hotkey's UID away; hotkeyOwners names the
	// owner of each hotkey account; proxyBalance is the proxy's EVM balance.
	noPoolUid    bool
	hotkeyOwners map[[32]byte][32]byte
	proxyBalance *big.Int
	// The factory and its CREATE2: wrongFactory changes its code,
	// createAnswer replaces the created address, createFails reverts it.
	wrongFactory bool
	createAnswer *common.Address
	createFails  bool
	// The created carry: its code, residual and views.
	code     []byte
	residual *big.Int
	views    map[string]any
}

func newCtfCarryFake(t *testing.T) *ctfCarryFake {
	t.Helper()
	artifact, err := loadCtfCarryArtifact(e2eForgeArtifact(t, "STCtfCarry").raw)
	if err != nil {
		t.Fatal(err)
	}
	f := &ctfCarryFake{t: t, artifact: artifact, registered: true, resolved: ctfCarryTestVault2,
		nominator: big.NewInt(20_000_000), price: big.NewInt(1e18),
		stakingLists: map[[32]byte][][32]byte{}, owned: map[[32]byte][][32]byte{}, locked: map[[32]byte]bool{},
		rejectsGrowth: map[[32]byte]bool{}, rejectsLocked: map[[32]byte]bool{}, stakes: map[[32]byte]*big.Int{}, autoStake: map[[32]byte][32]byte{},
		hotkeyOwners: map[[32]byte][32]byte{}, proxyBalance: big.NewInt(1_000_000_000_000)}
	initcode, _, err := artifact.initcode(ctfCarryTestVault2, ctfCarryTestPoolId)
	if err != nil {
		t.Fatal(err)
	}
	f.address = crypto.CreateAddress2(upgradeCreate2Factory, ctfCarryTestSalt, crypto.Keccak256(initcode))
	f.coldkey = ss58.EvmMirrorPubkey([20]byte(f.address))
	// The guardian associated the carry's coldkey and vault2's as hotkeys of
	// its own.
	f.hotkeyOwners[f.coldkey] = [32]byte(ctfCarryTestGuardian)
	f.hotkeyOwners[ctfCarryTestSelf] = [32]byte(ctfCarryTestGuardian)
	return f
}

// create makes the carry exist, as its constructor leaves it: its code with
// the immutables filled, its views, the residual on the escrow hotkey, the
// pool hotkey as its auto-stake destination, both on its list, and both
// opt-outs.
func (self *ctfCarryFake) create() {
	self.code = bytes.Clone(self.artifact.runtime)
	for i, word := range self.artifact.immutables {
		copy(self.code[word[0]:word[1]], crypto.Keccak256([]byte{byte(i)}))
	}
	self.residual = big.NewInt(ctfCarryTestResidual)
	self.views = map[string]any{
		"vault": ctfCarryTestVault2, "poolId": ctfCarryTestPoolId, "netuid": uint16(ctfCarryTestNetuid),
		"escrowHotkey": [32]byte(ctfCarryTestEscrow), "poolHotkey": [32]byte(ctfCarryTestPoolHotkey),
		"vaultColdkey": [32]byte(ctfCarryTestSelf), "coldkey": self.coldkey,
		"MINIMUM_SEED_RAO": big.NewInt(ctfCarryMinimumSeedRao), "SEED_NOMINATOR_MULTIPLE": big.NewInt(ctfCarrySeedMultiple),
	}
	self.stakes[self.coldkey] = big.NewInt(ctfCarryTestResidual)
	self.stakingLists[self.coldkey] = [][32]byte{crypto.Keccak256Hash([]byte("dust")), [32]byte(ctfCarryTestEscrow), [32]byte(ctfCarryTestPoolHotkey)}
	self.autoStake[self.coldkey] = [32]byte(ctfCarryTestPoolHotkey)
	self.rejectsGrowth[self.coldkey], self.rejectsLocked[self.coldkey] = true, true
}

func (self *ctfCarryFake) pack(contract abi.ABI, method string, values ...any) []byte {
	self.t.Helper()
	data, err := contract.Methods[method].Outputs.Pack(values...)
	if err != nil {
		self.t.Fatalf("pack %s: %v", method, err)
	}
	return data
}

// call answers one eth_call, or reverts with ok false.
func (self *ctfCarryFake) call(from common.Address, to common.Address, value *big.Int, data []byte) ([]byte, bool) {
	if len(data) < 4 {
		return nil, false
	}
	runtime := ctfCarryRuntimeAbi
	settlementAbi, _ := stabi.STSettlement2MetaData.ParseABI()
	vaultAbi, _ := stabi.STSettlementVault2MetaData.ParseABI()
	method := func(contract *abi.ABI) (*abi.Method, []any) {
		found, err := contract.MethodById(data[:4])
		if err != nil {
			return nil, nil
		}
		args, err := found.Inputs.Unpack(data[4:])
		if err != nil {
			return nil, nil
		}
		return found, args
	}
	switch to {
	case upgradeCreate2Factory:
		if self.createFails || len(data) < 32 {
			return nil, false
		}
		created := crypto.CreateAddress2(upgradeCreate2Factory, common.BytesToHash(data[:32]), crypto.Keccak256(data[32:]))
		if self.createAnswer != nil {
			created = *self.createAnswer
		}
		if from != ctfCarryTestCreator || value == nil || value.Sign() == 0 {
			return nil, false
		}
		return created.Bytes(), true
	case ctfCarryTestSettlement:
		found, args := method(settlementAbi)
		switch {
		case found == nil || self.notCtf && strings.HasPrefix(found.Name, "ctf"):
			return nil, false
		case found.Name == "ctfPoolId":
			return self.pack(*settlementAbi, found.Name, new(big.Int).Add(new(big.Int).Lsh(common.Big1, 32), args[0].(*big.Int))), true
		case found.Name == "vault":
			return self.pack(*settlementAbi, found.Name, ctfCarryTestVault2), true
		case found.Name == "ctfSettlementAt":
			return self.pack(*settlementAbi, found.Name, self.resolved, [32]byte(ctfCarryTestPoolHotkey), uint64(ctfCarryTestFirstEpoch)), true
		}
	case ctfCarryTestVault2:
		found, args := method(vaultAbi)
		switch {
		case found == nil:
			return nil, false
		case found.Name == "pools":
			if !self.registered || args[0].(*big.Int).Cmp(ctfCarryTestPoolId) != 0 {
				return self.pack(*vaultAbi, found.Name, stabi.ISTSettlementVault2Pool{}), true
			}
			return self.pack(*vaultAbi, found.Name, stabi.ISTSettlementVault2Pool{Hotkey: ctfCarryTestPoolHotkey, FirstEpoch: ctfCarryTestFirstEpoch, Registered: true}), true
		case found.Name == "netuid":
			return self.pack(*vaultAbi, found.Name, uint16(ctfCarryTestNetuid)), true
		case found.Name == "escrowHotkey":
			return self.pack(*vaultAbi, found.Name, [32]byte(ctfCarryTestEscrow)), true
		case found.Name == "selfColdkey":
			return self.pack(*vaultAbi, found.Name, [32]byte(ctfCarryTestSelf)), true
		}
	case ctfCarryStaking:
		found, args := method(&runtime)
		if found == nil {
			return nil, false
		}
		switch found.Name {
		case "getNominatorMinRequiredStake":
			return self.pack(runtime, found.Name, self.nominator), true
		case "getStakingHotkeys":
			list := self.stakingLists[args[0].([32]byte)]
			offset, limit := int(args[1].(uint64)), int(args[2].(uint16))
			page := [][32]byte{}
			for i := offset; i < len(list) && i < offset+limit; i++ {
				page = append(page, list[i])
			}
			return self.pack(runtime, found.Name, page, uint64(len(list))), true
		case "getOwnedHotkeys":
			owned := self.owned[args[0].([32]byte)]
			if owned == nil {
				owned = [][32]byte{}
			}
			return self.pack(runtime, found.Name, owned), true
		case "getColdkeyLock":
			lock := struct {
				Exists      bool
				Hotkey      [32]byte
				LockedMass  *big.Int
				Conviction  *big.Int
				IsPerpetual bool
			}{Exists: self.locked[args[0].([32]byte)], LockedMass: new(big.Int), Conviction: new(big.Int)}
			return self.pack(runtime, found.Name, lock), true
		case "getRejectStakingHotkeyGrowth":
			return self.pack(runtime, found.Name, self.rejectsGrowth[args[0].([32]byte)]), true
		case "getRejectLockedAlpha":
			return self.pack(runtime, found.Name, self.rejectsLocked[args[0].([32]byte)]), true
		case "getHotkeyOwner":
			owner, ok := self.hotkeyOwners[args[0].([32]byte)]
			return self.pack(runtime, found.Name, ok, owner), true
		case "getAutoStakeDestination":
			destination, ok := self.autoStake[args[0].([32]byte)]
			return self.pack(runtime, found.Name, ok, destination), true
		case "getStake":
			staked := self.stakes[args[1].([32]byte)]
			if staked == nil || args[0].([32]byte) != [32]byte(ctfCarryTestEscrow) {
				staked = new(big.Int)
			}
			return self.pack(runtime, found.Name, staked), true
		}
	case ctfCarryNeuron:
		found, args := method(&runtime)
		if found == nil || found.Name != "getUid" {
			return nil, false
		}
		registered := !self.noPoolUid && args[1].([32]byte) == [32]byte(ctfCarryTestPoolHotkey)
		return self.pack(runtime, found.Name, registered, uint16(31)), true
	case ctfCarryAlpha:
		return self.pack(runtime, "getAlphaPrice", self.price), true
	case self.address:
		if len(self.code) == 0 {
			return nil, true
		}
		found, _ := method(&self.artifact.abi)
		if found == nil {
			return nil, false
		}
		if found.Name == "residual" {
			return self.pack(self.artifact.abi, found.Name, self.residual), true
		}
		if view, ok := self.views[found.Name]; ok {
			return self.pack(self.artifact.abi, found.Name, view), true
		}
	}
	return nil, false
}

// serve answers review's JSON-RPC reads from the fake.
func (self *ctfCarryFake) serve() string {
	self.t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		var rpc struct {
			Id     int               `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &rpc); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": rpc.Id}
		address := func() common.Address {
			var value common.Address
			_ = json.Unmarshal(rpc.Params[0], &value)
			return value
		}
		switch rpc.Method {
		case "eth_chainId":
			reply["result"] = hexutil.EncodeUint64(mainnetEvmChainId)
		case "eth_blockNumber":
			reply["result"] = hexutil.EncodeUint64(9_580_000)
		case "eth_getCode":
			switch at := address(); {
			case at == upgradeCreate2Factory && !self.wrongFactory:
				reply["result"] = upgradeCreate2FactoryRuntime
			case at == self.address:
				reply["result"] = hexutil.Encode(self.code)
			default:
				reply["result"] = "0x"
			}
		case "eth_getBalance":
			balance := new(big.Int)
			if address() == upgradeCreate2Factory {
				balance = self.proxyBalance
			}
			reply["result"] = hexutil.EncodeBig(balance)
		case "eth_call":
			var message struct {
				From  common.Address `json:"from"`
				To    common.Address `json:"to"`
				Data  hexutil.Bytes  `json:"data"`
				Value *hexutil.Big   `json:"value"`
			}
			_ = json.Unmarshal(rpc.Params[0], &message)
			output, ok := self.call(message.From, message.To, (*big.Int)(message.Value), message.Data)
			if ok {
				reply["result"] = hexutil.Encode(output)
			} else {
				reply["error"] = map[string]any{"code": 3, "message": "execution reverted", "data": "0x"}
			}
		default:
			reply["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(reply)
	}))
	self.t.Cleanup(server.Close)
	return server.URL
}

// review runs the command with the default flags, overridden by extra.
func (self *ctfCarryFake) review(t *testing.T, extra ...string) (int, ctfCarryReview, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "STCtfCarry.json")
	if err := os.WriteFile(path, e2eForgeArtifact(t, "STCtfCarry").raw, 0o600); err != nil {
		t.Fatal(err)
	}
	flags := map[string]string{
		"--rpc": self.serve(), "--settlement2": ctfCarryTestSettlement.Hex(), "--no-id": "1", "--artifact": path,
		"--salt": ctfCarryTestSalt.Hex(), "--seed-rao": "1000000000", "--guardian": ctfCarryTestGuardian.Hex(),
	}
	var args []string
	for i := 0; i < len(extra); i += 2 {
		if extra[i+1] == "" {
			delete(flags, extra[i])
			continue
		}
		flags[extra[i]] = extra[i+1]
	}
	args = append(args, "review")
	for name, value := range flags {
		args = append(args, name, value)
	}
	var stdout, stderr bytes.Buffer
	code := runCtfCarryCommand(context.Background(), args, &stdout, &stderr)
	var review ctfCarryReview
	if code == 0 {
		if err := json.Unmarshal(stdout.Bytes(), &review); err != nil {
			t.Fatalf("output %q: %v", stdout.String(), err)
		}
	}
	return code, review, stderr.String()
}

// CR-1: before the creation, review derives the carry's address from the
// artifact, the salt, vault2 and the pool, and its coldkey from the address;
// builds the creation call with the seed and the existential deposit as its
// value; and, with --from, runs it as an eth_call that answers the address. A
// list third parties filled through the public coldkey is reported.
func TestCtfCarryReviewBeforeCreation(t *testing.T) {
	f := newCtfCarryFake(t)
	code, review, stderr := f.review(t)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	args, err := f.artifact.abi.Pack("", ctfCarryTestVault2, ctfCarryTestPoolId)
	if err != nil {
		t.Fatal(err)
	}
	initcode := append(bytes.Clone(f.artifact.creation), args...)
	want := crypto.CreateAddress2(common.HexToAddress("0x4e59b44847b379578588920ca78fbf26c0b4956c"), ctfCarryTestSalt, crypto.Keccak256(initcode))
	carry := review.Carry
	switch {
	case review.Schema != ctfCarryReviewSchema || review.ChainId != 964 || review.Created != nil:
		t.Fatalf("review %+v", review)
	case carry.Address != want.Hex() || carry.Coldkey != common.Hash(ss58.EvmMirrorPubkey([20]byte(want))).Hex() || carry.Exists:
		t.Fatalf("carry %+v, want the address %s", carry, want.Hex())
	case carry.ConstructorArgs != hexutil.Encode(args) || carry.InitcodeKeccak != crypto.Keccak256Hash(initcode).Hex():
		t.Fatalf("constructor args %s, initcode keccak %s", carry.ConstructorArgs, carry.InitcodeKeccak)
	case carry.CreateTo != upgradeCreate2Factory.Hex() || carry.CreateData != hexutil.Encode(append(ctfCarryTestSalt.Bytes(), initcode...)):
		t.Fatalf("creation to %s with data %s", carry.CreateTo, carry.CreateData)
	case carry.CreateValueWei != "1000000500000000000" || carry.SeedRao != "1000000000" || carry.ExistentialDepositRao != 500:
		t.Fatalf("creation value %s for a seed of %s", carry.CreateValueWei, carry.SeedRao)
	case review.Pool.PoolId != "4294967297" || review.Pool.Vault2 != ctfCarryTestVault2.Hex() || review.Pool.PoolHotkey != ctfCarryTestPoolHotkey.Hex() ||
		review.Pool.Netuid != ctfCarryTestNetuid || review.Pool.EscrowHotkey != ctfCarryTestEscrow.Hex() || review.Pool.FirstEpoch != ctfCarryTestFirstEpoch:
		t.Fatalf("pool %+v", review.Pool)
	case review.Runtime.NominatorMinimumRao != "20000000" || review.Runtime.SeedMinimumRao != "200000000" || review.Runtime.StakingHotkeys != 0 ||
		review.Runtime.PoolHotkeyUid == nil || *review.Runtime.PoolHotkeyUid != 31 || review.Runtime.ColdkeyHotkeyOwner == nil ||
		*review.Runtime.ColdkeyHotkeyOwner != ctfCarryTestGuardian.Hex() || review.Runtime.Guardian != ctfCarryTestGuardian.Hex() ||
		review.Runtime.VaultColdkeyHotkeyOwner == nil || *review.Runtime.VaultColdkeyHotkeyOwner != ctfCarryTestGuardian.Hex() ||
		review.Runtime.ProxyBalanceWei != "1000000000000" ||
		review.Runtime.ProxyAccount != common.Hash(ss58.EvmMirrorPubkey([20]byte(upgradeCreate2Factory))).Hex():
		t.Fatalf("runtime %+v", review.Runtime)
	case review.Preflight.Ran || !strings.Contains(review.Preflight.Result, "--from"):
		t.Fatalf("preflight %+v without --from", review.Preflight)
	}

	code, review, stderr = f.review(t, "--from", ctfCarryTestCreator.Hex())
	if code != 0 || !review.Preflight.Ran || !strings.Contains(review.Preflight.Result, want.Hex()) {
		t.Fatalf("exit %d with --from: preflight %+v; %s", code, review.Preflight, stderr)
	}

	// A seed of exactly the least passes; the floor rules when the nominator
	// minimum is small.
	f.nominator = big.NewInt(1)
	if code, review, stderr = f.review(t, "--seed-rao", "100000000"); code != 0 || review.Runtime.SeedMinimumRao != "100000000" {
		t.Fatalf("exit %d for the floor's seed: %+v; %s", code, review.Runtime, stderr)
	}

	// The guardian's association makes the coldkey public: stake third
	// parties send it before the creation, up to the 128 entries they can
	// add, is reported and not refused.
	f.stakingLists[f.coldkey] = make([][32]byte, 128)
	if code, review, stderr = f.review(t); code != 0 || review.Runtime.StakingHotkeys != 128 {
		t.Fatalf("exit %d with a list third parties filled: %+v; %s", code, review.Runtime, stderr)
	}
}

// CR-2: before the creation review refuses, exit 3, what the creation would
// make unsafe or fail: the proxy's code, a proxy without the capture the flag
// implementation, a pool vault2 does not hold or the proxy resolves
// elsewhere, a carry coldkey that owns hotkeys, holds a lock or holds more
// staking hotkeys than third parties can add, either coldkey not the
// guardian's hotkey account, a proxy without TAO above its existential
// deposit, a seed below the least, and a creation that fails or answers
// another address.
func TestCtfCarryReviewRefusesBeforeCreation(t *testing.T) {
	other := common.HexToAddress("0x0123456789012345678901234567890123456789")
	for _, c := range []struct {
		name   string
		change func(*ctfCarryFake)
		extra  []string
		want   string
	}{
		{"factory code", func(f *ctfCarryFake) { f.wrongFactory = true }, nil, "deterministic deployment proxy"},
		{"phase C proxy", func(f *ctfCarryFake) { f.notCtf = true }, nil, "capture the flag implementation"},
		{"no CTF pool", func(f *ctfCarryFake) { f.registered = false }, nil, "holds no CTF pool"},
		{"pool resolved elsewhere", func(f *ctfCarryFake) { f.resolved = other }, nil, "ctfSettlementAt"},
		{"more staking hotkeys than third parties can add", func(f *ctfCarryFake) { f.stakingLists[f.coldkey] = make([][32]byte, 129) }, nil, "already holds 129 staking hotkeys"},
		{"owned hotkeys", func(f *ctfCarryFake) { f.owned[f.coldkey] = [][32]byte{{2}} }, nil, "already owns 1 hotkeys"},
		{"a lock", func(f *ctfCarryFake) { f.locked[f.coldkey] = true }, nil, "conviction lock"},
		{"a pool hotkey without a UID", func(f *ctfCarryFake) { f.noPoolUid = true }, nil, "holds no UID"},
		{"a coldkey that is no hotkey account", func(f *ctfCarryFake) { delete(f.hotkeyOwners, f.coldkey) }, nil, "not a hotkey account yet"},
		{"a coldkey someone else associated", func(f *ctfCarryFake) { f.hotkeyOwners[f.coldkey] = [32]byte{7} }, nil, "someone took it first"},
		{"vault2's coldkey no hotkey account", func(f *ctfCarryFake) { delete(f.hotkeyOwners, [32]byte(ctfCarryTestSelf)) }, nil, "vault2's coldkey"},
		{"vault2's coldkey someone else's hotkey account", func(f *ctfCarryFake) { f.hotkeyOwners[ctfCarryTestSelf] = [32]byte{7} }, nil, "not of the guardian"},
		{"a proxy with nothing above its existential deposit", func(f *ctfCarryFake) { f.proxyBalance = new(big.Int) }, nil, "OutOfFund"},
		{"seed below ten times the nominator minimum", func(f *ctfCarryFake) { f.nominator = big.NewInt(100_000_001) }, nil, "below the carry's least seed"},
		{"seed below the floor", func(*ctfCarryFake) {}, []string{"--seed-rao", "99999999"}, "below the carry's least seed"},
		{"creation fails", func(f *ctfCarryFake) { f.createFails = true }, []string{"--from", ctfCarryTestCreator.Hex()}, "failed"},
		{"creation answers another address", func(f *ctfCarryFake) { f.createAnswer = &other }, []string{"--from", ctfCarryTestCreator.Hex()}, "not the address"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newCtfCarryFake(t)
			c.change(f)
			code, _, stderr := f.review(t, c.extra...)
			if code != 3 || !strings.Contains(stderr, c.want) {
				t.Fatalf("exit %d, want 3 with %q: %s", code, c.want, stderr)
			}
		})
	}
}

// CR-3: usage errors exit 2 before any read.
func TestCtfCarryReviewUsage(t *testing.T) {
	phaseC := filepath.Join(t.TempDir(), "STSettlement2PhaseC.json")
	if err := os.WriteFile(phaseC, e2eForgeArtifact(t, "STSettlement2PhaseC").raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		extra []string
		want  string
	}{
		{"no rpc", []string{"--rpc", ""}, "requires"},
		{"no salt", []string{"--salt", ""}, "requires"},
		{"no seed", []string{"--seed-rao", ""}, "requires"},
		{"zero seed", []string{"--seed-rao", "0"}, "requires"},
		{"operator 0", []string{"--no-id", "0"}, "requires"},
		{"operator 2^32", []string{"--no-id", "4294967296"}, "requires"},
		{"bad settlement", []string{"--settlement2", "0x00"}, "requires"},
		{"bad creator", []string{"--from", "creator"}, "requires"},
		{"no guardian", []string{"--guardian", ""}, "requires"},
		{"short guardian", []string{"--guardian", "0x01"}, "requires"},
		{"zero guardian", []string{"--guardian", "0x0000000000000000000000000000000000000000000000000000000000000000"}, "requires"},
		{"zero salt", []string{"--salt", "0x0000000000000000000000000000000000000000000000000000000000000000"}, "zero salt"},
		{"phase C artifact", []string{"--artifact", phaseC}, "not the reviewed STCtfCarry"},
		{"missing artifact", []string{"--artifact", filepath.Join(t.TempDir(), "none.json")}, "artifact"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newCtfCarryFake(t)
			code, _, stderr := f.review(t, c.extra...)
			if code != 2 || !strings.Contains(stderr, c.want) {
				t.Fatalf("exit %d, want 2 with %q: %s", code, c.want, stderr)
			}
		})
	}
	var stderr bytes.Buffer
	if code := runCtfCarryCommand(context.Background(), []string{"create"}, io.Discard, &stderr); code != 2 {
		t.Fatalf("a subcommand other than review exits %d", code)
	}
}

// CR-4: once the carry exists, review checks its code against the artifact
// outside the immutables, the immutables and constants through its views, its
// residual on the escrow hotkey, which its list holds, and both opt-outs, and
// names the address for the amendment entry.
func TestCtfCarryReviewAfterCreation(t *testing.T) {
	f := newCtfCarryFake(t)
	f.create()
	code, review, stderr := f.review(t)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	created := review.Created
	if created == nil || !review.Carry.Exists || created.ResidualRao != "987654321" || created.EscrowPositionRao != "987654321" ||
		created.ResidualTaoRao != "987654321" || !created.EscrowListed || !created.PoolHotkeyPinned || !created.PoolListed ||
		!created.RejectsGrowth || !created.RejectsLockedAlpha ||
		created.AmendmentCarryEntry != f.address.Hex() || created.RuntimeCodeHash != crypto.Keccak256Hash(f.code).Hex() || review.Preflight.Ran {
		t.Fatalf("created %+v, carry %+v, preflight %+v", created, review.Carry, review.Preflight)
	}
	// A created carry's review does not refuse the touched coldkey or the seed.
	f.nominator = big.NewInt(1)
	if code, _, stderr = f.review(t, "--seed-rao", "1"); code != 0 {
		t.Fatalf("exit %d for a created carry with any seed: %s", code, stderr)
	}
}

// CR-5: a created carry that is not as reviewed is refused, exit 3.
func TestCtfCarryReviewRefusesACreatedCarry(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(*ctfCarryFake)
		want   string
	}{
		{"other code", func(f *ctfCarryFake) { f.code[0] ^= 1 }, "not the reviewed STCtfCarry"},
		{"short code", func(f *ctfCarryFake) { f.code = f.code[:len(f.code)-1] }, "not the reviewed STCtfCarry"},
		{"another pool hotkey", func(f *ctfCarryFake) { f.views["poolHotkey"] = [32]byte{9} }, "poolHotkey()"},
		{"another coldkey", func(f *ctfCarryFake) { f.views["coldkey"] = [32]byte{9} }, "coldkey()"},
		{"another floor", func(f *ctfCarryFake) { f.views["MINIMUM_SEED_RAO"] = big.NewInt(1) }, "MINIMUM_SEED_RAO()"},
		{"no residual", func(f *ctfCarryFake) { f.residual = new(big.Int) }, "keeps no residual"},
		{"a position below the residual", func(f *ctfCarryFake) { f.stakes[f.coldkey] = big.NewInt(ctfCarryTestResidual - 1) }, "below its residual"},
		{"escrow not listed", func(f *ctfCarryFake) { f.stakingLists[f.coldkey] = [][32]byte{{3}} }, "does not hold the escrow hotkey"},
		{"another auto-stake destination", func(f *ctfCarryFake) { f.autoStake[f.coldkey] = [32]byte{4} }, "auto-stake destination"},
		{"no auto-stake destination", func(f *ctfCarryFake) { delete(f.autoStake, f.coldkey) }, "auto-stake destination"},
		{"pool hotkey not listed", func(f *ctfCarryFake) { f.stakingLists[f.coldkey] = [][32]byte{[32]byte(ctfCarryTestEscrow)} }, "auto-stake destination"},
		{"growth accepted", func(f *ctfCarryFake) { f.rejectsGrowth[f.coldkey] = false }, "opt-outs"},
		{"locked alpha accepted", func(f *ctfCarryFake) { f.rejectsLocked[f.coldkey] = false }, "opt-outs"},
		{"a residual worth less than the nominator minimum", func(f *ctfCarryFake) { f.price = big.NewInt(1e16) }, "below the nominator minimum"},
		{"a coldkey that is no hotkey account", func(f *ctfCarryFake) { delete(f.hotkeyOwners, f.coldkey) }, "not a hotkey account"},
		{"a coldkey someone else holds", func(f *ctfCarryFake) { f.hotkeyOwners[f.coldkey] = [32]byte{7} }, "not of the guardian"},
		{"vault2's coldkey no hotkey account", func(f *ctfCarryFake) { delete(f.hotkeyOwners, [32]byte(ctfCarryTestSelf)) }, "vault2's coldkey"},
		{"vault2's coldkey someone else's", func(f *ctfCarryFake) { f.hotkeyOwners[ctfCarryTestSelf] = [32]byte{7} }, "not of the guardian"},
		{"owned hotkeys", func(f *ctfCarryFake) { f.owned[f.coldkey] = [][32]byte{{5}} }, "owns 1 hotkeys"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newCtfCarryFake(t)
			f.create()
			c.change(f)
			code, _, stderr := f.review(t)
			if code != 3 || !strings.Contains(stderr, c.want) {
				t.Fatalf("exit %d, want 3 with %q: %s", code, c.want, stderr)
			}
		})
	}
}
