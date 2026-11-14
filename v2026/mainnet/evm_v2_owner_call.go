// After the phase C upgrade, the governance Safe makes the v1 sweep's two
// owner calls on the STSettlement2 proxy (VERSION2-FINAL.md §16):
//   - recordV1Sweep, once the single-leaf v1 sweep root is committed;
//   - assignSurplus, once absorbStake has made the swept stake vault2 surplus,
//     before the receiving epoch's close.
//
// owner-call review checks the call against the chain at one head (the record
// against the v1 commitment, the assignment against the pool's next unfunded
// epoch, vault2's surplus and the pool's scheduled recovery), builds the Safe
// transaction and the owner's execTransaction exactly as upgrade-settlement2
// review does, and preflights it from the owner. It neither signs nor sends.
// Since 2026-11-06 a timelock owns the proxy; with --timelock, review
// schedules the call through it, or reviews its execute step
// (evm_v2_timelock.go).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/stabi"
)

const settlement2OwnerCallReviewSchema = "urnetwork-mainnet-settlement-v2-owner-call-review-v1"

// The phase C owner calls and the sweep record view.
const settlement2PhaseCAbiJson = `[
 {"type":"function","name":"assignSurplus","stateMutability":"nonpayable","inputs":[{"name":"noId","type":"uint256"},{"name":"epoch","type":"uint256"},{"name":"amount","type":"uint256"}],"outputs":[]},
 {"type":"function","name":"recordV1Sweep","stateMutability":"nonpayable","inputs":[{"name":"noId","type":"uint256"},{"name":"epoch","type":"uint256"},{"name":"artifactHash","type":"bytes32"}],"outputs":[]},
 {"type":"function","name":"v1Sweep","stateMutability":"view","inputs":[{"name":"noId","type":"uint256"}],"outputs":[{"name":"epoch","type":"uint64"},{"name":"payoutRoot","type":"bytes32"},{"name":"artifactHash","type":"bytes32"}]}
]`

var settlement2PhaseCAbi = func() abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(settlement2PhaseCAbiJson))
	if err != nil {
		panic(err)
	}
	return parsed
}()

// One owner call request: assignSurplus takes an amount, recordV1Sweep an
// artifact hash.
type settlement2OwnerCall struct {
	settlement   common.Address
	governance   settlement2Governance
	function     string
	noId         uint64
	epoch        uint64
	amount       *big.Int
	artifactHash common.Hash
}

// The review's result. chain names the facts the call was checked against.
// Only a review in timelock mode has the timelock section, and the execute
// step of an open executor has no Safe transaction.
type settlement2OwnerCallReview struct {
	Schema      string                      `json:"schema"`
	ChainId     uint64                      `json:"chain_id"`
	HeadBlock   uint64                      `json:"head_block"`
	Settlement2 settlement2UpgradeProxy     `json:"settlement2"`
	Chain       map[string]string           `json:"chain"`
	Timelock    *settlement2UpgradeTimelock `json:"timelock,omitempty"`
	Safe        *settlement2UpgradeSafe     `json:"safe,omitempty"`
	SafeTx      *settlement2UpgradeSafeTx   `json:"safe_transaction,omitempty"`
	OwnerTx     *settlement2UpgradeOwnerTx  `json:"owner_transaction,omitempty"`
	Preflight   settlement2UpgradePreflight `json:"preflight"`
}

func runSettlement2OwnerCallCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	const usage = "usage: owner-call-settlement2 review --rpc URL --settlement2 ADDRESS --safe ADDRESS --no-id N --epoch E (--function assignSurplus --amount RAO | --function recordV1Sweep --artifact-hash 0xHASH) [--timelock ADDRESS [--timelock-salt 0xSALT] [--execute]]"
	if len(args) == 0 || args[0] != "review" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	flags := flag.NewFlagSet("owner-call-settlement2 review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var call settlement2OwnerCall
	var settlement, safe, amount, artifactHash string
	rpcUrl := flags.String("rpc", "", "explicit owned HTTP(S) EVM JSON-RPC URL")
	flags.StringVar(&settlement, "settlement2", "", "the STSettlement2 proxy")
	flags.StringVar(&safe, "safe", "", "the governance Safe, which owns STSettlement2 or proposes to the timelock that does")
	flags.StringVar(&call.function, "function", "", "assignSurplus or recordV1Sweep")
	flags.Uint64Var(&call.noId, "no-id", 0, "the pool's no_id")
	flags.Uint64Var(&call.epoch, "epoch", 0, "assignSurplus: the pool's next unfunded epoch; recordV1Sweep: the v1 sweep epoch")
	flags.StringVar(&amount, "amount", "", "assignSurplus: the surplus to assign, in rao")
	flags.StringVar(&artifactHash, "artifact-hash", "", "recordV1Sweep: the committed artifact hash, 0x and 32 bytes of hex")
	timelocked := settlement2TimelockFlags(flags)
	parsed := flags.Parse(args[1:]) == nil && flags.NArg() == 0
	set := map[string]bool{}
	flags.Visit(func(given *flag.Flag) { set[given.Name] = true })
	complete := parsed && set["rpc"] && set["settlement2"] && set["safe"] && set["function"] && set["no-id"] && set["epoch"]
	var settlementOk, safeOk bool
	call.settlement, settlementOk = poolAddress(settlement)
	call.governance.safe, safeOk = poolAddress(safe)
	complete = complete && timelocked(set, &call.governance)
	switch call.function {
	case "assignSurplus":
		var ok bool
		call.amount, ok = new(big.Int).SetString(amount, 10)
		complete = complete && set["amount"] && !set["artifact-hash"] && ok && call.amount.Sign() > 0
	case "recordV1Sweep":
		complete = complete && set["artifact-hash"] && !set["amount"] && validHash(artifactHash)
		call.artifactHash = common.HexToHash(artifactHash)
	default:
		complete = false
	}
	if !complete || !settlementOk || !safeOk || call.noId == 0 || call.epoch == 0 {
		fmt.Fprintln(stderr, "owner-call-settlement2 review requires nonzero 0x addresses, a no_id and epoch, and either --function assignSurplus with a positive decimal --amount or --function recordV1Sweep with a 0x 32-byte --artifact-hash; --timelock-salt (0x and 32 bytes) and --execute only with --timelock, another nonzero 0x address")
		fmt.Fprintln(stderr, usage)
		return 2
	}
	client, err := newRpcClient(*rpcUrl, 60*time.Second)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	review, err := reviewSettlement2OwnerCall(ctx, client, call)
	if err != nil {
		fmt.Fprintln(stderr, "owner-call-settlement2 review:", err)
		if errors.Is(err, errUpgradeRefused) {
			return 3
		}
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(review); err != nil {
		fmt.Fprintln(stderr, "owner-call-settlement2 review output:", err)
		return 1
	}
	return 0
}

// settlement2RecoverySchedule returns a pool's recovery schedule; tests
// replace it.
var settlement2RecoverySchedule = ledger.ScheduledRecovery

// reviewSettlement2Recovery guards a pool's scheduled recovery (§16 step 6).
// The recovery is the first part of the pool's assigned surplus through the
// schedule's last epoch (ledger.PlanRecovery). While part of it is unplaced,
// an assignment must place all of it in one call, to an epoch the schedule
// still covers: a short or split one would spread it across roots, and one
// after the schedule's last epoch would be ordinary funding. The pool's
// SurplusAssigned logs are read from the schedule's first block to the head.
// A pool without a schedule, or whose recovery is placed, is not limited.
func reviewSettlement2Recovery(ctx context.Context, chain *upgradeChain, head uint64, vault2 common.Address, call settlement2OwnerCall, firstEpoch uint64, facts map[string]string) error {
	schedule := settlement2RecoverySchedule(vault2, call.noId)
	if schedule == nil {
		return nil
	}
	surplus := map[uint64]uint256.Int{}
	topics := []any{[]common.Hash{settlement2SurplusAssignedTopic}, []common.Hash{common.BigToHash(new(big.Int).SetUint64(call.noId))}}
	vaultAbi := stabi.NewSTSettlementVault2()
	for from := schedule.FromBlock; from <= head; from += settlement2LogChunkBlocks {
		var logs []types.Log
		filter := map[string]any{"address": vault2.Hex(), "fromBlock": hexutil.EncodeUint64(from), "toBlock": hexutil.EncodeUint64(min(from+settlement2LogChunkBlocks-1, head)), "topics": topics}
		if err := chain.client.callUpgradeRead(ctx, "eth_getLogs", []any{filter}, &logs); err != nil {
			return fmt.Errorf("vault2 SurplusAssigned logs from block %d: %w", from, err)
		}
		for i := range logs {
			event, err := vaultAbi.UnpackSurplusAssignedEvent(&logs[i])
			if err != nil || logs[i].Address != vault2 || !event.NoId.IsUint64() || event.NoId.Uint64() != call.noId || !event.Epoch.IsUint64() {
				return upgradeRefusal(fmt.Sprintf("vault2 log %d of block %d is not a SurplusAssigned of pool %d", logs[i].Index, logs[i].BlockNumber, call.noId))
			}
			amount, overflow := uint256.FromBig(event.Amount)
			if overflow {
				return upgradeRefusal(fmt.Sprintf("vault2 log %d of block %d assigns 2^256 or more", logs[i].Index, logs[i].BlockNumber))
			}
			total := surplus[event.Epoch.Uint64()]
			surplus[event.Epoch.Uint64()] = *total.Add(&total, amount)
		}
	}
	unplaced := ledger.RecoveryRemaining(schedule, surplus, firstEpoch, call.epoch)
	facts["recovery_unplaced"], facts["recovery_through_epoch"] = unplaced.Dec(), fmt.Sprint(schedule.ThroughEpoch)
	if unplaced.IsZero() {
		return nil
	}
	if call.epoch > schedule.ThroughEpoch {
		return upgradeRefusal(fmt.Sprintf("pool %d's scheduled recovery is placed only through epoch %d and %s rao of it stay unplaced; an assignment to epoch %d is ordinary funding", call.noId, schedule.ThroughEpoch, unplaced.Dec(), call.epoch))
	}
	after := maps.Clone(surplus)
	amount, _ := uint256.FromBig(call.amount)
	total := after[call.epoch]
	after[call.epoch] = *total.Add(&total, amount)
	if left := ledger.RecoveryRemaining(schedule, after, firstEpoch, call.epoch); !left.IsZero() {
		return upgradeRefusal(fmt.Sprintf("assigning %s leaves %s rao of the scheduled recovery unplaced; assign at least %s in one call", call.amount, left.Dec(), unplaced.Dec()))
	}
	return nil
}

// settlement2LogChunkBlocks is the widest eth_getLogs span asked for, the
// public endpoint's maximum.
const settlement2LogChunkBlocks = 1_000

// settlement2SurplusAssignedTopic is vault2's SurplusAssigned event ID.
var settlement2SurplusAssignedTopic = func() common.Hash {
	parsed, err := stabi.STSettlementVault2MetaData.ParseABI()
	if err != nil {
		panic(err)
	}
	return parsed.Events["SurplusAssigned"].ID
}()

// ownerCallView is upgradeView for a contract binding's packed call.
func ownerCallView[T any](ctx context.Context, chain *upgradeChain, to common.Address, data []byte, unpack func([]byte) (T, error)) (T, error) {
	var value T
	output, err := chain.call(ctx, nil, nil, to, data)
	if err != nil {
		return value, fmt.Errorf("view 0x%x of %s: %w", data[:4], to.Hex(), err)
	}
	if value, err = unpack(output); err != nil {
		return value, upgradeRefusal(fmt.Sprintf("view 0x%x of %s answered 0x%x, not as its contract does", data[:4], to.Hex(), output))
	}
	return value, nil
}

func reviewSettlement2OwnerCall(ctx context.Context, client *rpcClient, call settlement2OwnerCall) (settlement2OwnerCallReview, error) {
	var result settlement2OwnerCallReview
	chain, head, err := openUpgradeChain(ctx, client)
	if err != nil {
		return result, err
	}
	current, err := readSettlement2Proxy(ctx, chain, call.settlement, call.governance)
	if err != nil {
		return result, err
	}
	noId, epoch := new(big.Int).SetUint64(call.noId), new(big.Int).SetUint64(call.epoch)

	// The proxy must run the phase C implementation: v1Sweep answers.
	recorded, err := upgradeView(ctx, chain, call.settlement, settlement2PhaseCAbi, "v1Sweep", noId)
	if err != nil {
		return result, upgradeRefusal(fmt.Sprintf("the proxy does not run the phase C implementation: %v", err))
	}
	settlementAbi, vaultAbi := stabi.NewSTSettlement2(), stabi.NewSTSettlementVault2()
	vault2, err := ownerCallView(ctx, chain, call.settlement, settlementAbi.PackVault(), settlementAbi.UnpackVault)
	if err != nil {
		return result, err
	}
	pool, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackPools(noId), vaultAbi.UnpackPools)
	if err != nil {
		return result, err
	}
	if !pool.Registered {
		return result, upgradeRefusal(fmt.Sprintf("vault2 has no pool %d", call.noId))
	}
	facts := map[string]string{"vault2": vault2.Hex(), "first_epoch": fmt.Sprint(pool.FirstEpoch)}
	var data []byte
	var args map[string]string
	var function string
	switch call.function {
	case "assignSurplus":
		last, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackLastFundedEpoch(noId), vaultAbi.UnpackLastFundedEpoch)
		if err != nil {
			return result, err
		}
		live, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackLiveEscrowStake(), vaultAbi.UnpackLiveEscrowStake)
		if err != nil {
			return result, err
		}
		accounted, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackEscrowAccounted(), vaultAbi.UnpackEscrowAccounted)
		if err != nil {
			return result, err
		}
		captured, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackCapturedTotal(noId), vaultAbi.UnpackCapturedTotal)
		if err != nil {
			return result, err
		}
		surplus := new(big.Int)
		if live.Cmp(accounted) > 0 {
			surplus.Sub(live, accounted)
		}
		next := new(big.Int).Add(last, common.Big1)
		facts["last_funded_epoch"], facts["live_escrow_stake"], facts["escrow_accounted"] = last.String(), live.String(), accounted.String()
		facts["surplus"], facts["captured_total"] = surplus.String(), captured.String()
		if next.Cmp(epoch) != 0 {
			return result, upgradeRefusal(fmt.Sprintf("the pool's next unfunded epoch is %s, not %d", next, call.epoch))
		}
		if call.amount.Cmp(surplus) > 0 {
			return result, upgradeRefusal(fmt.Sprintf("the amount %s exceeds vault2's surplus %s", call.amount, surplus))
		}
		if err := reviewSettlement2Recovery(ctx, chain, head, vault2, call, pool.FirstEpoch, facts); err != nil {
			return result, err
		}
		data, err = settlement2PhaseCAbi.Pack("assignSurplus", noId, epoch, call.amount)
		if err != nil {
			return result, err
		}
		function, args = "assignSurplus(uint256,uint256,uint256)", map[string]string{"noId": noId.String(), "epoch": epoch.String(), "amount": call.amount.String()}
	case "recordV1Sweep":
		if recorded[0].(uint64) != 0 {
			return result, upgradeRefusal(fmt.Sprintf("pool %d already records its v1 sweep at epoch %d", call.noId, recorded[0].(uint64)))
		}
		if call.epoch < pool.FirstEpoch {
			return result, upgradeRefusal(fmt.Sprintf("epoch %d is before the pool's first v2 epoch %d", call.epoch, pool.FirstEpoch))
		}
		coordinator, v1Vault := stabi.NewSTCoordinator(), stabi.NewSTSettlementVault()
		v1, err := ownerCallView(ctx, chain, call.settlement, settlementAbi.PackV1Coordinator(), settlementAbi.UnpackV1Coordinator)
		if err != nil {
			return result, err
		}
		vault1, err := ownerCallView(ctx, chain, v1, coordinator.PackSettlementVault(), coordinator.UnpackSettlementVault)
		if err != nil {
			return result, err
		}
		coldkey, err := ownerCallView(ctx, chain, vault2, vaultAbi.PackSelfColdkey(), vaultAbi.UnpackSelfColdkey)
		if err != nil {
			return result, err
		}
		leaf, err := ownerCallView(ctx, chain, vault1, v1Vault.PackPayoutLeaf(coldkey, big.NewInt(10_000)), v1Vault.UnpackPayoutLeaf)
		if err != nil {
			return result, err
		}
		commitment, err := ownerCallView(ctx, chain, v1, coordinator.PackRootCommitments(epoch, noId), coordinator.UnpackRootCommitments)
		if err != nil {
			return result, err
		}
		facts["v1_coordinator"], facts["v1_vault"], facts["sweep_root"] = v1.Hex(), vault1.Hex(), common.Hash(leaf).Hex()
		facts["committed_root"], facts["committed_artifact_hash"] = common.Hash(commitment.PayoutRoot).Hex(), common.Hash(commitment.ArtifactHash).Hex()
		facts["commit_block"] = fmt.Sprint(commitment.CommitBlock)
		if commitment.PayoutRoot != leaf {
			return result, upgradeRefusal(fmt.Sprintf("the v1 root committed for epoch %d is not the single leaf paying vault2's coldkey 10,000 bps", call.epoch))
		}
		if common.Hash(commitment.ArtifactHash) != call.artifactHash {
			return result, upgradeRefusal(fmt.Sprintf("the committed artifact hash is %s, not %s", common.Hash(commitment.ArtifactHash).Hex(), call.artifactHash.Hex()))
		}
		data, err = settlement2PhaseCAbi.Pack("recordV1Sweep", noId, epoch, call.artifactHash)
		if err != nil {
			return result, err
		}
		function, args = "recordV1Sweep(uint256,uint256,bytes32)", map[string]string{"noId": noId.String(), "epoch": epoch.String(), "artifactHash": call.artifactHash.Hex()}
	}

	review := settlement2OwnerCallReview{
		Schema: settlement2OwnerCallReviewSchema, ChainId: mainnetEvmChainId, HeadBlock: head,
		Settlement2: settlement2UpgradeProxy{Proxy: call.settlement.Hex(), Owner: call.governance.owner().Hex(), CurrentImplementation: current.Hex()},
		Chain:       facts,
	}

	// Through the timelock, the Safe schedules the call, or the operation's
	// execute step is reviewed.
	if call.governance.timelocked() {
		timelocked, err := reviewSettlement2Timelock(ctx, chain, call.governance, call.settlement,
			settlement2TimelockCall{Target: call.settlement.Hex(), ValueWei: "0", Data: hexutil.Encode(data), Function: function, Args: args}, data, new(big.Int), true, nil)
		if err != nil {
			return result, err
		}
		review.Schema, review.Timelock, review.Preflight = settlement2OwnerCallTimelockReviewSchema, &timelocked.timelock, timelocked.preflight
		review.Safe, review.SafeTx, review.OwnerTx = timelocked.safe, timelocked.safeTx, timelocked.ownerTx
		return review, ctx.Err()
	}

	safe, owner, nonce, err := readUpgradeSafe(ctx, chain, call.governance.safe)
	if err != nil {
		return result, err
	}
	safeTx, ownerTx, execData, err := buildOwnerSafeCall(ctx, chain, call.governance.safe, owner, nonce, call.settlement, data, function, args)
	if err != nil {
		return result, err
	}
	if err := preflightOwnerCall(ctx, chain, owner, call.governance.safe, execData); err != nil {
		return result, err
	}
	review.Safe, review.SafeTx, review.OwnerTx = &safe, &safeTx, &ownerTx
	review.Preflight = settlement2UpgradePreflight{Ran: true, Result: "execTransaction from the owner returns true at the head"}
	return review, ctx.Err()
}
