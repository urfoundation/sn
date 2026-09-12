package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
)

const fleetRenewalBatchSize uint64 = 10
const fleetRenewalMaximumInFlight uint64 = 10

// A batch cannot age while another batch waits for inclusion. All independent
// native signers in one batch advance together; each EVM phase has one ordered
// nonce owner. Every phase joins before its dependent phase starts.
func orderFleetRenewalActions(actions []Action, renewal FleetRenewal) ([]Action, error) {
	ordered := make([]Action, 0, len(actions))
	previous := []string{}
	oracleNonce, keeperNonce := renewal.OracleNonce, renewal.KeeperNonce
	for first := 1; first <= len(renewal.Fleets); first += int(renewal.BatchSize) {
		last := min(first+int(renewal.BatchSize)-1, len(renewal.Fleets))
		next := []string{}
		for _, operation := range []string{"commitment", "mirror", "revoke", "bind"} {
			for _, a := range actions {
				fleet, err := strconv.Atoi(a.Parameters["fleet"])
				if err != nil {
					return nil, err
				}
				if fleet < first || fleet > last || a.Parameters["operation"] != operation {
					continue
				}
				member, _ := strconv.Atoi(a.Parameters["member"])
				switch operation {
				case "commitment":
					a.DependsOn = append([]string{}, previous...)
				case "mirror":
					a.DependsOn = []string{fleetRenewalActionID(renewal.Round, fleet, "commitment", 0)}
					a.Parameters["renewal_expected_nonce"] = strconv.FormatUint(oracleNonce, 10)
					oracleNonce++
				case "revoke":
					a.DependsOn = []string{fleetRenewalActionID(renewal.Round, fleet, "mirror", 0)}
					a.Parameters["renewal_expected_nonce"] = strconv.FormatUint(keeperNonce, 10)
					keeperNonce++
				case "bind":
					a.DependsOn = []string{fleetRenewalActionID(renewal.Round, fleet, "mirror", 0)}
					if renewal.Fleets[fleet-1].Members[member-1].RevokeSignature != "" {
						a.DependsOn = append(a.DependsOn, fleetRenewalActionID(renewal.Round, fleet, "revoke", member))
					}
					a.Parameters["renewal_expected_nonce"] = strconv.FormatUint(keeperNonce, 10)
					keeperNonce++
					next = append(next, a.ID)
				}
				a.IntentHash, err = actionIntentHash(a)
				if err != nil {
					return nil, err
				}
				ordered = append(ordered, a)
			}
		}
		previous = next
	}
	if len(ordered) != len(actions) {
		return nil, errors.New("renewal pipeline omitted an action")
	}
	return ordered, nil
}

// The usual 2*baseFee+tip quote is headroom, not an obligation to exceed the
// approved ceiling. Renewal can use that ceiling when it still covers the
// current inclusion price; a price above approval is always refused.
func fleetRenewalQuotedFeeCap(action Action, baseFee, tip *big.Int) (*big.Int, error) {
	_, maximum, err := evmActionFeeEnvelope(action)
	if err != nil {
		return nil, err
	}
	if !isFleetRenewalAction(action) || tip == nil || tip.Sign() < 0 {
		return nil, errors.New("renewal fee quote is unavailable")
	}
	required := new(big.Int).Set(tip)
	if baseFee != nil {
		if baseFee.Sign() < 0 {
			return nil, errors.New("renewal base fee is negative")
		}
		required.Add(required, baseFee)
	}
	ceiling := new(big.Int).SetUint64(maximum)
	if required.Cmp(ceiling) > 0 {
		return nil, fmt.Errorf("%s current inclusion price %s exceeds approved fee-per-gas ceiling %d", action.ID, required, maximum)
	}
	return ceiling, nil
}

// Release the existing account turn only after durable preparation and an
// ordered submission of the exact bytes. Reconciliation cannot sign a new
// nonce and may rebroadcast only this already approved transaction.
func (m *EvmTxManager) submitFleetRenewal(ctx context.Context, planHash string, action Action, to *common.Address, value *big.Int, data []byte) (*ethTypes.Transaction, error) {
	if !isFleetRenewalAction(action) {
		return nil, errors.New("pipelined submission is restricted to exact fleet renewal actions")
	}
	release, err := m.acquireNonceTurn(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	signed, err := m.prepareOwnedEVMTransaction(ctx, planHash, action, to, value, data)
	if err != nil {
		return nil, err
	}
	if receipt, err := m.client.TransactionReceipt(ctx, signed.Hash()); err == nil {
		if err := validateEVMReceiptIdentity(receipt, signed.Hash()); err != nil {
			return nil, err
		}
		return signed, nil
	} else if !errors.Is(err, ethereum.NotFound) {
		return nil, err
	}
	head, err := finalizedEVMHead(ctx, m.client)
	if err != nil {
		return nil, err
	}
	sender, err := ethTypes.Sender(ethTypes.LatestSignerForChainID(m.chainID), signed)
	if err != nil {
		return nil, err
	}
	nonce, err := m.client.NonceAt(ctx, sender, new(big.Int).SetUint64(head.Number))
	if err != nil {
		return nil, err
	}
	if nonce > signed.Nonce() {
		return nil, fmt.Errorf("renewal nonce %d was consumed by another finalized transaction", signed.Nonce())
	}
	if err := m.client.SendTransaction(ctx, signed); !knownEVMTxError(err) {
		return nil, fmt.Errorf("submit exact renewal %s: %w", signed.Hash(), err)
	}
	return signed, nil
}

// Every started worker joins, including after failure/cancellation. The bound
// applies to native signers and to EVM transactions awaiting finality.
func runFleetRenewalJoined(ctx context.Context, jobs []func(context.Context) error) error {
	if len(jobs) > int(fleetRenewalMaximumInFlight) {
		return errors.New("renewal wave exceeds its approved in-flight bound")
	}
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	results := make(chan error, len(jobs))
	for _, job := range jobs {
		workers.Add(1)
		go func(run func(context.Context) error) {
			defer workers.Done()
			err := run(owned)
			if err != nil {
				cancel()
			}
			results <- err
		}(job)
	}
	workers.Wait()
	close(results)
	var result error
	for err := range results {
		result = errors.Join(result, err)
	}
	if result == nil {
		return ctx.Err()
	}
	return result
}

func (e *Executor) finishFleetRenewalPipelineAction(ctx context.Context, action Action, manager *EvmTxManager, signed *ethTypes.Transaction) error {
	receipt, err := manager.waitExactTransaction(ctx, e.plan.PlanHash, action, signed)
	if err == nil {
		err = e.persistFleetRenewalBindingReceipt(action, receipt)
	}
	if err != nil {
		return err
	}
	post, err := e.verifyActionPostcondition(ctx, action)
	if err != nil {
		return err
	}
	path, hash, err := e.persistActionPostcondition(post)
	if err != nil {
		return err
	}
	return e.journal.Append(JournalEntry{DeploymentID: e.plan.DeploymentID, PlanHash: e.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageVerified, PostconditionHash: hash, PostconditionPath: path})
}

func (e *Executor) executeFleetRenewalPipeline(ctx context.Context, actions []Action) error {
	for offset := 0; offset < len(actions); {
		operation := actions[offset].Parameters["operation"]
		end := offset
		for end < len(actions) && end-offset < int(fleetRenewalMaximumInFlight) && actions[end].Parameters["operation"] == operation {
			end++
		}
		wave := actions[offset:end]
		if operation == "commitment" {
			jobs := make([]func(context.Context) error, 0, len(wave))
			for _, action := range wave {
				a := action
				jobs = append(jobs, func(owned context.Context) error { return e.Execute(owned, a) })
			}
			if err := runFleetRenewalJoined(ctx, jobs); err != nil {
				return err
			}
		} else {
			owned, cancel := context.WithCancel(ctx)
			type pending struct {
				action  Action
				manager *EvmTxManager
				signed  *ethTypes.Transaction
			}
			pendingWrites := make([]pending, 0, len(wave))
			var submissionErr error
			for _, action := range wave {
				if err := owned.Err(); err != nil {
					submissionErr = err
					break
				}
				if _, verified := e.verifiedActionEntry(action); verified {
					if err := e.Execute(owned, action); err != nil {
						submissionErr = err
						break
					}
					continue
				}
				if err := e.verifyActionDependencies(action); err != nil {
					submissionErr = err
					break
				}
				if err := e.journal.Append(JournalEntry{DeploymentID: e.plan.DeploymentID, PlanHash: e.plan.PlanHash, ActionID: action.ID, IntentHash: action.IntentHash, Stage: StageIntent}); err != nil {
					submissionErr = err
					break
				}
				var prepared pending
				err := e.executeFleetRenewalActionWithSender(owned, action, func(ctx context.Context, manager *EvmTxManager, planHash string, a Action, to *common.Address, value *big.Int, data []byte) (*ethTypes.Receipt, error) {
					signed, err := manager.submitFleetRenewal(ctx, planHash, a, to, value, data)
					if err == nil {
						prepared = pending{a, manager, signed}
					}
					return nil, err
				})
				if err != nil {
					submissionErr = err
					break
				}
				pendingWrites = append(pendingWrites, prepared)
			}
			if submissionErr != nil {
				cancel()
			}
			jobs := make([]func(context.Context) error, 0, len(pendingWrites))
			for _, write := range pendingWrites {
				write := write
				jobs = append(jobs, func(ctx context.Context) error {
					return e.finishFleetRenewalPipelineAction(ctx, write.action, write.manager, write.signed)
				})
			}
			completionErr := runFleetRenewalJoined(owned, jobs)
			cancel()
			if err := errors.Join(submissionErr, completionErr); err != nil {
				return err
			}
		}
		offset = end
	}
	return nil
}
