// Since 2026-11-06 an OpenZeppelin 5.6.1 TimelockController owns the
// STSettlement2 proxy: at block 9,448,608 the governance Safe executed
// transferOwnership(timelock). Every owner call waits at least 72 h, longer
// than vault2's 14,400-block challenge window plus the time to respond; the
// Safe alone proposes and cancels; anyone executes a ready call; only the
// timelock administers itself.
//
// With --timelock, upgrade-settlement2 review and owner-call-settlement2
// review check that timelock at the same head as the call: its exact reviewed
// code, its minimum delay and the Safe's roles. The reviewed owner call then
// becomes the Safe's call of schedule(settlement2, value, call, 0x0, salt,
// minDelay), which the Safe's owner executes exactly as the direct Safe call
// went, and the call itself is preflighted from the timelock, so a schedule
// whose execution would revert is refused up front. The value is zero but for
// the capture the flag upgrade's call, which the timelock pays from its own
// balance: that call's preflight needs the balance, and review refuses a
// timelock that holds less. With --execute, review covers the scheduled
// operation's execute step instead: the operation must be ready, and review
// prints the timelock's execute transaction, which any funded key sends while
// execution is open, or the Safe's execTransaction of it while only the Safe
// executes. Without --timelock both reviews keep the direct Safe call.
package main

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// A review in timelock mode adds the timelock section, and its execute step
// for an open executor has no Safe transaction, so its schemas are new ones.
const (
	settlement2UpgradeTimelockReviewSchema   = "urnetwork-mainnet-settlement-v2-upgrade-review-v2"
	settlement2OwnerCallTimelockReviewSchema = "urnetwork-mainnet-settlement-v2-owner-call-review-v2"
)

// settlement2TimelockCodeHash is the runtime code hash of the timelock that
// owns STSettlement2 on mainnet, 0x5529dfe070f73A96C14b5aFCf4fb3DdeA6122fAc,
// which the Ledger deployer created at block 9,448,589 (transaction
// 0xfa632257…9df0). Its code is OpenZeppelin 5.6.1's TimelockController, the
// pinned 5fd1781b, as sn/evm builds it at a03d119d: solc 0.8.24, via-IR,
// cancun, 200 optimizer runs. It has no immutables, so every deployment of
// that build has this code. Its last 53 bytes, the CBOR metadata, hash the
// absolute library paths of the directory it was built in; a build anywhere
// else has the same 5,226 executable bytes but another metadata hash, so not
// this code hash (evm/test/V2SettlementTimelock.t.sol pins both).
var settlement2TimelockCodeHash = common.HexToHash("0xe3a14af165a6b22bbe7e2c4259833eed0f02e64acb782992c0c4f50ab32eb880")

// settlement2TimelockMinDelay is the least minimum delay review accepts: the
// owner's 72 h of 2026-11-06, more than vault2's ~48 h challenge window plus
// the time to respond.
const settlement2TimelockMinDelay = 72 * 60 * 60

// The timelock's roles. DEFAULT_ADMIN_ROLE is zero.
var settlement2TimelockRoles = []struct {
	name string
	id   common.Hash
}{
	{"proposer", crypto.Keccak256Hash([]byte("PROPOSER_ROLE"))},
	{"canceller", crypto.Keccak256Hash([]byte("CANCELLER_ROLE"))},
	{"executor", crypto.Keccak256Hash([]byte("EXECUTOR_ROLE"))},
	{"admin", common.Hash{}},
}

// TimelockController.OperationState by its value.
var settlement2TimelockStates = []string{"unset", "waiting", "ready", "done"}

// The TimelockController functions review reads and the Safe or an executor
// calls.
const settlement2TimelockAbiJson = `[
 {"type":"function","name":"getMinDelay","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
 {"type":"function","name":"hasRole","stateMutability":"view","inputs":[{"name":"role","type":"bytes32"},{"name":"account","type":"address"}],"outputs":[{"name":"","type":"bool"}]},
 {"type":"function","name":"hashOperation","stateMutability":"pure","inputs":[{"name":"target","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"},{"name":"predecessor","type":"bytes32"},{"name":"salt","type":"bytes32"}],"outputs":[{"name":"","type":"bytes32"}]},
 {"type":"function","name":"getOperationState","stateMutability":"view","inputs":[{"name":"id","type":"bytes32"}],"outputs":[{"name":"","type":"uint8"}]},
 {"type":"function","name":"getTimestamp","stateMutability":"view","inputs":[{"name":"id","type":"bytes32"}],"outputs":[{"name":"","type":"uint256"}]},
 {"type":"function","name":"schedule","stateMutability":"nonpayable","inputs":[{"name":"target","type":"address"},{"name":"value","type":"uint256"},{"name":"data","type":"bytes"},{"name":"predecessor","type":"bytes32"},{"name":"salt","type":"bytes32"},{"name":"delay","type":"uint256"}],"outputs":[]},
 {"type":"function","name":"execute","stateMutability":"payable","inputs":[{"name":"target","type":"address"},{"name":"value","type":"uint256"},{"name":"payload","type":"bytes"},{"name":"predecessor","type":"bytes32"},{"name":"salt","type":"bytes32"}],"outputs":[]}
]`

var settlement2TimelockAbi = func() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(settlement2TimelockAbiJson))
	if err != nil {
		panic(err)
	}
	return parsed
}()

// settlement2Governance is who owns STSettlement2 in a review: the Safe
// directly or, with --timelock, the timelock the Safe proposes to; then also
// the operation's salt and whether review covers its execute step.
type settlement2Governance struct {
	safe     common.Address
	timelock common.Address
	salt     common.Hash
	execute  bool
}

func (self settlement2Governance) timelocked() bool {
	return self.timelock != (common.Address{})
}

// owner is the account STSettlement2's owner() must answer.
func (self settlement2Governance) owner() common.Address {
	if self.timelocked() {
		return self.timelock
	}
	return self.safe
}

// settlement2TimelockFlags defines a review's optional timelock flags. The
// function it returns completes the governance once the flags are parsed and
// reports false for a malformed address or salt, or for --timelock-salt or
// --execute without --timelock.
func settlement2TimelockFlags(flags *flag.FlagSet) func(set map[string]bool, governance *settlement2Governance) bool {
	var timelock, salt string
	var execute bool
	flags.StringVar(&timelock, "timelock", "", "STSettlement2's owner when it is the reviewed TimelockController the Safe proposes to: the call is scheduled through it")
	flags.StringVar(&salt, "timelock-salt", "", "with --timelock: the operation's salt, 0x and 32 bytes of hex; zero when omitted")
	flags.BoolVar(&execute, "execute", false, "with --timelock: review the scheduled operation's execute step instead of its schedule")
	return func(set map[string]bool, governance *settlement2Governance) bool {
		if !set["timelock"] {
			return !set["timelock-salt"] && !set["execute"]
		}
		address, ok := poolAddress(timelock)
		if !ok || address == governance.safe || set["timelock-salt"] && !validHash(salt) {
			return false
		}
		governance.timelock, governance.salt, governance.execute = address, common.HexToHash(salt), execute
		return true
	}
}

// The timelock section of a review in timelock mode. call is the owner call
// the timelock makes when the operation executes, with its value. roles
// names, for each role, which of the Safe, the zero address and the timelock
// itself hold it: TimelockController keeps no member list. earliest_execution
// is, for a schedule, the head's time plus the minimum delay (the schedule
// lands later, so the operation is ready later still) and, for a scheduled
// operation, its getTimestamp. execute_transaction is the timelock's execute
// of the call; it sends no value, since the timelock pays the call's value
// from its own balance.
type settlement2UpgradeTimelock struct {
	Address               string                       `json:"address"`
	CodeHash              string                       `json:"code_hash"`
	MinDelaySeconds       string                       `json:"min_delay_seconds"`
	Roles                 map[string][]string          `json:"roles"`
	Executor              string                       `json:"executor"`
	Step                  string                       `json:"step"`
	Call                  settlement2TimelockCall      `json:"call"`
	Predecessor           string                       `json:"predecessor"`
	Salt                  string                       `json:"salt"`
	OperationId           string                       `json:"operation_id"`
	OperationState        string                       `json:"operation_state"`
	EarliestExecution     string                       `json:"earliest_execution"`
	EarliestExecutionUnix string                       `json:"earliest_execution_unix"`
	ExecuteTx             settlement2TimelockExecuteTx `json:"execute_transaction"`
	CallPreflight         settlement2UpgradePreflight  `json:"call_preflight"`
}

type settlement2TimelockCall struct {
	Target   string            `json:"target"`
	ValueWei string            `json:"value_wei"`
	Data     string            `json:"data"`
	Function string            `json:"function"`
	Args     map[string]string `json:"args"`
}

type settlement2TimelockExecuteTx struct {
	To         string `json:"to"`
	ValueWei   string `json:"value_wei"`
	Data       string `json:"data"`
	DataKeccak string `json:"data_keccak256"`
	Function   string `json:"function"`
}

// readSettlement2Timelock requires the reviewed TimelockController's exact
// code at the governance's timelock, a minimum delay of at least 72 h, and
// the Safe as a proposer and a canceller, and requires the zero address (open
// execution) or the Safe to execute. It returns the section's timelock facts
// and the minimum delay.
func readSettlement2Timelock(ctx context.Context, chain *upgradeChain, governance settlement2Governance) (settlement2UpgradeTimelock, *big.Int, error) {
	timelock := settlement2UpgradeTimelock{Address: governance.timelock.Hex(), Roles: map[string][]string{}}
	code, err := chain.code(ctx, governance.timelock)
	if err != nil {
		return timelock, nil, err
	}
	if hash := crypto.Keccak256Hash(code); hash != settlement2TimelockCodeHash {
		return timelock, nil, upgradeRefusal(fmt.Sprintf("the timelock's runtime code hash is %s, not %s, the reviewed OpenZeppelin 5.6.1 TimelockController's", hash.Hex(), settlement2TimelockCodeHash.Hex()))
	}
	values, err := upgradeView(ctx, chain, governance.timelock, settlement2TimelockAbi, "getMinDelay")
	if err != nil {
		return timelock, nil, err
	}
	delay := values[0].(*big.Int)
	held := map[string]map[common.Address]bool{}
	for _, role := range settlement2TimelockRoles {
		held[role.name], timelock.Roles[role.name] = map[common.Address]bool{}, []string{}
		for _, account := range []common.Address{governance.safe, {}, governance.timelock} {
			values, err := upgradeView(ctx, chain, governance.timelock, settlement2TimelockAbi, "hasRole", role.id, account)
			if err != nil {
				return timelock, nil, err
			}
			if values[0].(bool) {
				held[role.name][account] = true
				timelock.Roles[role.name] = append(timelock.Roles[role.name], account.Hex())
			}
		}
	}
	switch {
	case delay.Cmp(big.NewInt(settlement2TimelockMinDelay)) < 0:
		return timelock, nil, upgradeRefusal(fmt.Sprintf("the timelock's minimum delay is %s s, below the reviewed %d s (72 h)", delay, settlement2TimelockMinDelay))
	case !held["proposer"][governance.safe]:
		return timelock, nil, upgradeRefusal("the Safe does not hold the timelock's PROPOSER_ROLE")
	case !held["canceller"][governance.safe]:
		return timelock, nil, upgradeRefusal("the Safe does not hold the timelock's CANCELLER_ROLE")
	case !held["executor"][common.Address{}] && !held["executor"][governance.safe]:
		return timelock, nil, upgradeRefusal("neither the zero address, which opens execution to anyone, nor the Safe holds the timelock's EXECUTOR_ROLE")
	}
	timelock.CodeHash, timelock.MinDelaySeconds, timelock.Executor = settlement2TimelockCodeHash.Hex(), delay.String(), "safe"
	if held["executor"][common.Address{}] {
		timelock.Executor = "open"
	}
	return timelock, delay, nil
}

// settlement2LooksLikeTimelock reports whether an owner other than the Safe
// answers getMinDelay(), as a TimelockController does, for the refusal's hint.
func settlement2LooksLikeTimelock(ctx context.Context, chain *upgradeChain, owner common.Address) bool {
	_, err := upgradeView(ctx, chain, owner, settlement2TimelockAbi, "getMinDelay")
	return err == nil
}

// settlement2TimelockOperation is TimelockController.hashOperation of a call
// of data with value on target, with no predecessor.
func settlement2TimelockOperation(target common.Address, value *big.Int, data []byte, salt common.Hash) common.Hash {
	encoded, err := settlement2TimelockAbi.Methods["hashOperation"].Inputs.Pack(target, value, data, [32]byte{}, [32]byte(salt))
	if err != nil {
		panic(err)
	}
	return crypto.Keccak256Hash(encoded)
}

// settlement2TimelockTime renders a unix time as UTC RFC 3339, or nothing for
// one past int64.
func settlement2TimelockTime(seconds *big.Int) string {
	if !seconds.IsInt64() {
		return ""
	}
	return time.Unix(seconds.Int64(), 0).UTC().Format(time.RFC3339)
}

// balance is an account's balance at the head, in wei.
func (self *upgradeChain) balance(ctx context.Context, address common.Address) (*big.Int, error) {
	var balance hexutil.Big
	if err := self.client.callUpgradeRead(ctx, "eth_getBalance", []any{address.Hex(), self.head}, &balance); err != nil {
		return nil, err
	}
	return balance.ToInt(), nil
}

// time is the head block's timestamp.
func (self *upgradeChain) time(ctx context.Context) (uint64, error) {
	var block struct {
		Number    hexutil.Uint64 `json:"number"`
		Timestamp hexutil.Uint64 `json:"timestamp"`
	}
	if err := self.client.callUpgradeRead(ctx, "eth_getBlockByNumber", []any{self.head, false}, &block); err != nil {
		return 0, err
	}
	if hexutil.EncodeUint64(uint64(block.Number)) != self.head {
		return 0, fmt.Errorf("eth_getBlockByNumber %s answered block %d", self.head, uint64(block.Number))
	}
	return uint64(block.Timestamp), nil
}

// settlement2TimelockReview is the governance part of a review in timelock
// mode: the timelock section, and the Safe and its transaction, which are nil
// for the execute step of an open executor, whose execute transaction anyone
// sends.
type settlement2TimelockReview struct {
	timelock  settlement2UpgradeTimelock
	safe      *settlement2UpgradeSafe
	safeTx    *settlement2UpgradeSafeTx
	ownerTx   *settlement2UpgradeOwnerTx
	preflight settlement2UpgradePreflight
}

// reviewSettlement2Timelock checks the timelock and the operation of call,
// with data and value, on settlement, then builds the transaction of the step
// under review: the Safe's schedule, or the execute step. At the schedule
// step, schedulable, when set, refuses an operation that could not succeed
// once it is ready; it gets the head's time and the earliest execution. With
// preflight it runs the call from the timelock and the step's transaction
// from its sender at the head, and either failing is a refusal; the timelock
// pays the call's value from its own balance, so review first refuses one
// that holds less. Without preflight neither runs.
func reviewSettlement2Timelock(ctx context.Context, chain *upgradeChain, governance settlement2Governance, settlement common.Address, call settlement2TimelockCall, data []byte, value *big.Int, preflight bool, schedulable func(now uint64, earliest *big.Int) error) (settlement2TimelockReview, error) {
	var result settlement2TimelockReview
	if value == nil {
		value = new(big.Int)
	}
	timelock, delay, err := readSettlement2Timelock(ctx, chain, governance)
	if err != nil {
		return result, err
	}
	id := settlement2TimelockOperation(settlement, value, data, governance.salt)
	values, err := upgradeView(ctx, chain, governance.timelock, settlement2TimelockAbi, "hashOperation", settlement, value, data, [32]byte{}, [32]byte(governance.salt))
	if err != nil {
		return result, err
	}
	if common.Hash(values[0].([32]byte)) != id {
		return result, upgradeRefusal(fmt.Sprintf("the timelock hashes the operation as %x, not %s", values[0], id.Hex()))
	}
	values, err = upgradeView(ctx, chain, governance.timelock, settlement2TimelockAbi, "getOperationState", id)
	if err != nil {
		return result, err
	}
	if int(values[0].(uint8)) >= len(settlement2TimelockStates) {
		return result, upgradeRefusal(fmt.Sprintf("the timelock answered operation state %d", values[0]))
	}
	state := settlement2TimelockStates[values[0].(uint8)]
	values, err = upgradeView(ctx, chain, governance.timelock, settlement2TimelockAbi, "getTimestamp", id)
	if err != nil {
		return result, err
	}
	ready := values[0].(*big.Int)
	now, err := chain.time(ctx)
	if err != nil {
		return result, err
	}
	executeData, err := settlement2TimelockAbi.Pack("execute", settlement, value, data, [32]byte{}, [32]byte(governance.salt))
	if err != nil {
		return result, err
	}
	zero := common.Hash{}.Hex()
	timelock.Call, timelock.Predecessor, timelock.Salt, timelock.OperationId, timelock.OperationState = call, zero, governance.salt.Hex(), id.Hex(), state
	timelock.ExecuteTx = settlement2TimelockExecuteTx{To: governance.timelock.Hex(), ValueWei: "0", Data: hexutil.Encode(executeData), DataKeccak: crypto.Keccak256Hash(executeData).Hex(),
		Function: "execute(address,uint256,bytes,bytes32,bytes32)"}
	timelock.CallPreflight = settlement2UpgradePreflight{Result: "not run"}
	result.preflight = settlement2UpgradePreflight{Result: "not run"}

	// The step must find the operation in its state: a schedule only an
	// operation that does not exist yet, an execute only a ready one.
	earliest := ready
	if governance.execute {
		switch state {
		case "unset":
			return result, upgradeRefusal(fmt.Sprintf("the operation %s is not scheduled: review its schedule, without --execute", id.Hex()))
		case "waiting":
			return result, upgradeRefusal(fmt.Sprintf("the operation %s is not ready until %s, %s s after the head", id.Hex(), settlement2TimelockTime(ready), new(big.Int).Sub(ready, new(big.Int).SetUint64(now))))
		case "done":
			return result, upgradeRefusal(fmt.Sprintf("the operation %s has already executed", id.Hex()))
		}
		timelock.Step = "execute"
	} else {
		switch state {
		case "waiting":
			return result, upgradeRefusal(fmt.Sprintf("the operation %s is already scheduled, ready at %s: then review its execute step with --execute", id.Hex(), settlement2TimelockTime(ready)))
		case "ready":
			return result, upgradeRefusal(fmt.Sprintf("the operation %s is already scheduled and ready: review its execute step with --execute", id.Hex()))
		case "done":
			return result, upgradeRefusal(fmt.Sprintf("the operation %s has executed; scheduling the same call again takes another --timelock-salt", id.Hex()))
		}
		timelock.Step, earliest = "schedule", new(big.Int).Add(new(big.Int).SetUint64(now), delay)
		if schedulable != nil {
			if err := schedulable(now, earliest); err != nil {
				return result, err
			}
		}
	}
	timelock.EarliestExecution, timelock.EarliestExecutionUnix = settlement2TimelockTime(earliest), earliest.String()

	// The call itself, from the timelock, as the execute step runs it: with
	// its value, which the timelock pays from its own balance.
	if preflight {
		ran := "the call from the timelock succeeds at the head"
		if value.Sign() != 0 {
			balance, err := chain.balance(ctx, governance.timelock)
			if err != nil {
				return result, err
			}
			if balance.Cmp(value) < 0 {
				return result, upgradeRefusal(fmt.Sprintf("the timelock %s holds %s wei, less than the call's value of %s wei, which it pays from its own balance at execution: send it at least %s wei first", governance.timelock.Hex(), balance, value, new(big.Int).Sub(value, balance)))
			}
			ran = "the call from the timelock, with its value, succeeds at the head"
		}
		if _, err := chain.call(ctx, &governance.timelock, value, settlement, data); err != nil {
			return result, fmt.Errorf("the call from the timelock: %w", err)
		}
		timelock.CallPreflight = settlement2UpgradePreflight{Ran: true, Result: ran}
	}

	// The step's transaction: the Safe's schedule; anyone's execute while
	// execution is open; otherwise the Safe's execute.
	var function string
	var args map[string]string
	switch {
	case !governance.execute:
		data, err = settlement2TimelockAbi.Pack("schedule", settlement, value, data, [32]byte{}, [32]byte(governance.salt), delay)
		if err != nil {
			return result, err
		}
		function, args = "schedule(address,uint256,bytes,bytes32,bytes32,uint256)", map[string]string{"target": settlement.Hex(), "value": value.String(), "data": call.Data, "predecessor": zero, "salt": governance.salt.Hex(), "delay": delay.String()}
	case timelock.Executor == "open":
		if preflight {
			if _, err := chain.call(ctx, nil, nil, governance.timelock, executeData); err != nil {
				return result, fmt.Errorf("the execute transaction: %w", err)
			}
			result.preflight = settlement2UpgradePreflight{Ran: true, Result: "execute from any sender succeeds at the head"}
		}
		result.timelock = timelock
		return result, nil
	default:
		data = executeData
		function, args = "execute(address,uint256,bytes,bytes32,bytes32)", map[string]string{"target": settlement.Hex(), "value": value.String(), "payload": call.Data, "predecessor": zero, "salt": governance.salt.Hex()}
	}
	safe, owner, nonce, err := readUpgradeSafe(ctx, chain, governance.safe)
	if err != nil {
		return result, err
	}
	safeTx, ownerTx, execData, err := buildOwnerSafeCall(ctx, chain, governance.safe, owner, nonce, governance.timelock, data, function, args)
	if err != nil {
		return result, err
	}
	if preflight {
		if err := preflightOwnerCall(ctx, chain, owner, governance.safe, execData); err != nil {
			return result, err
		}
		result.preflight = settlement2UpgradePreflight{Ran: true, Result: "execTransaction from the owner returns true at the head"}
	}
	result.timelock, result.safe, result.safeTx, result.ownerTx = timelock, &safe, &safeTx, &ownerTx
	return result, nil
}
