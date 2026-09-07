// This unit replays the historical EVM state behind validator deposit choices
// and payout transitions. Transcript appends remain in the caller's one
// ordered sequence while reader implementations use pinned archive selectors.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Proves every historical coordinator checkpoint dispatched to the reviewed
// implementation, catching an upgrade that was restored before the report.
func verifyFinalSemanticCoordinatorRuntimes(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader, heads []ChainHead, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if evidence == nil || reader == nil || appendExchanges == nil || len(heads) == 0 {
		return errors.New("historical coordinator runtime audit is incomplete")
	}
	want := FinalCoordinatorRuntimeChainState{
		CoordinatorProxy: evidence.Deployment.CoordinatorProxy, CoordinatorImplementation: evidence.Deployment.CoordinatorImplementation,
		ObservedImplementationSlot: evidence.Deployment.ObservedImplementationSlot, ProxyCodeHash: evidence.Deployment.CoordinatorProxyCodeHash,
		ImplementationCodeHash: evidence.Deployment.ImplementationCodeHash, RuntimeRoots: append([]FinalReleaseRuntimeRoot(nil), evidence.Deployment.RuntimeRoots...),
	}
	for _, head := range heads {
		state, exchanges, err := reader.CoordinatorRuntime(ctx, head)
		if err != nil {
			return fmt.Errorf("at %d/%s: %w", head.Number, head.Hash, err)
		}
		if err := appendExchanges("evm", head, exchanges); err != nil {
			return err
		}
		want.Block = head
		if !finalJSONEqual(state, want) {
			return fmt.Errorf("historical coordinator implementation/runtime differs at %d/%s", head.Number, head.Hash)
		}
	}
	return nil
}

// Replays the exact release-steering relation at an immutable validator
// snapshot:
//
//	cumulativeConviction == reserve.operatorPrincipal
//	cumulativeConviction == convictionBefore + epochDeposits + epochConvictionAdded
//
// The signed ConvictionBeforeRao deliberately predates the settlement epoch's
// deposits and conviction additions. Comparing cumulative conviction directly
// to it would reject an honest nonzero epoch and, worse, fail to bind the two
// omitted ledger increments.
func verifyFinalSemanticCycleConvictionPrincipals(ctx context.Context, reader FinalSemanticChainReader, cycle FinalCRv4Cycle, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if reader == nil || appendExchanges == nil {
		return errors.New("validator conviction audit is unavailable")
	}
	for _, pool := range cycle.Pools {
		if pool.NoID == 0 {
			return errors.New("validator conviction audit has zero operator")
		}
		conviction, err := finalNonnegativeInteger("signed conviction before", pool.ConvictionBeforeRao)
		if err != nil {
			return err
		}
		deposit, exchanges, err := reader.EpochDeposit(ctx, cycle.SettlementEpoch, pool.NoID, cycle.EVMSnapshot)
		if err != nil {
			return fmt.Errorf("epoch deposit no=%d epoch=%d: %w", pool.NoID, cycle.SettlementEpoch, err)
		}
		if err := appendExchanges("evm", cycle.EVMSnapshot, exchanges); err != nil {
			return err
		}
		if deposit.Epoch != cycle.SettlementEpoch || deposit.NoID != pool.NoID || deposit.Block != cycle.EVMSnapshot || deposit.AmountRao != pool.ObservedDepositRao {
			return fmt.Errorf("epoch deposit no=%d differs from signed validator audit", pool.NoID)
		}
		depositAmount, err := finalNonnegativeInteger("epoch deposit", deposit.AmountRao)
		if err != nil {
			return err
		}
		added, exchanges, err := reader.EpochConvictionAdded(ctx, cycle.SettlementEpoch, pool.NoID, cycle.EVMSnapshot)
		if err != nil {
			return fmt.Errorf("epoch conviction increment no=%d epoch=%d: %w", pool.NoID, cycle.SettlementEpoch, err)
		}
		if err := appendExchanges("evm", cycle.EVMSnapshot, exchanges); err != nil {
			return err
		}
		if added.Epoch != cycle.SettlementEpoch || added.NoID != pool.NoID || added.Block != cycle.EVMSnapshot {
			return fmt.Errorf("epoch conviction increment no=%d has mismatched snapshot", pool.NoID)
		}
		addedAmount, err := finalNonnegativeInteger("epoch conviction increment", added.AmountRao)
		if err != nil {
			return err
		}
		coordinator, exchanges, err := reader.CoordinatorConviction(ctx, pool.NoID, cycle.EVMSnapshot)
		if err != nil {
			return fmt.Errorf("coordinator no=%d: %w", pool.NoID, err)
		}
		if err := appendExchanges("evm", cycle.EVMSnapshot, exchanges); err != nil {
			return err
		}
		if coordinator.NoID != pool.NoID || coordinator.Block != cycle.EVMSnapshot {
			return fmt.Errorf("coordinator conviction no=%d has mismatched snapshot", pool.NoID)
		}
		cumulative, err := finalNonnegativeInteger("coordinator cumulative conviction", coordinator.ConvictionRao)
		if err != nil {
			return err
		}
		principal, exchanges, err := reader.ReserveOperatorPrincipal(ctx, pool.NoID, cycle.EVMSnapshot)
		if err != nil {
			return fmt.Errorf("reserve no=%d: %w", pool.NoID, err)
		}
		if err := appendExchanges("evm", cycle.EVMSnapshot, exchanges); err != nil {
			return err
		}
		if principal.NoID != pool.NoID || principal.Block != cycle.EVMSnapshot {
			return fmt.Errorf("reserve operator principal no=%d has mismatched snapshot", pool.NoID)
		}
		principalAmount, err := finalNonnegativeInteger("reserve operator principal", principal.PrincipalRao)
		if err != nil {
			return err
		}
		if principalAmount.Cmp(cumulative) != 0 {
			return fmt.Errorf("reserve operator principal no=%d differs from coordinator cumulative conviction", pool.NoID)
		}
		increments := new(big.Int).Add(depositAmount, addedAmount)
		if cumulative.Cmp(increments) < 0 {
			return fmt.Errorf("coordinator conviction no=%d underflows signed pre-epoch conviction", pool.NoID)
		}
		before := new(big.Int).Sub(cumulative, increments)
		if before.Cmp(conviction) != 0 {
			return fmt.Errorf("coordinator conviction no=%d differs from signed validator audit", pool.NoID)
		}
	}
	return nil
}

// Checks each pool's baseline, finalized transitions, and terminal carry so a
// valid carry from a different pool cannot prove the selected operator.
func verifyFinalSemanticVaultCarries(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if evidence == nil || reader == nil || appendExchanges == nil {
		return errors.New("vault carry audit is unavailable")
	}
	rowsByNO := make(map[uint64][]FinalEpochOperatorEvidence, len(evidence.Pools))
	for _, row := range evidence.Epochs {
		rowsByNO[row.NoID] = append(rowsByNO[row.NoID], row)
	}
	for _, pool := range evidence.Pools {
		rows := rowsByNO[pool.NoID]
		if len(rows) == 0 {
			return fmt.Errorf("vault carry no=%d has no epoch transitions", pool.NoID)
		}
		points := make([]struct {
			head  ChainHead
			carry string
			label string
		}, 0, len(rows)+2)
		points = append(points, struct {
			head  ChainHead
			carry string
			label string
		}{head: evidence.Window.BaselineHead, carry: rows[0].CarryInRao, label: "baseline"})
		for _, row := range rows {
			points = append(points, struct {
				head  ChainHead
				carry string
				label string
			}{head: row.Finalize.Block, carry: row.CarryOutRao, label: fmt.Sprintf("epoch-%d-finalize", row.Epoch)})
		}
		points = append(points, struct {
			head  ChainHead
			carry string
			label string
		}{head: evidence.EVMTerminalHead, carry: pool.FinalCarryRao, label: "terminal"})
		for _, point := range points {
			if _, err := finalNonnegativeInteger("expected vault carry", point.carry); err != nil {
				return err
			}
			state, exchanges, err := reader.VaultCarry(ctx, pool.NoID, point.head)
			if err != nil {
				return fmt.Errorf("%s no=%d: %w", point.label, pool.NoID, err)
			}
			if err := appendExchanges("evm", point.head, exchanges); err != nil {
				return err
			}
			if state.NoID != pool.NoID || state.Block != point.head || state.CarryRao != point.carry {
				return fmt.Errorf("vault carry %s no=%d differs from semantic transition", point.label, pool.NoID)
			}
		}
	}
	return nil
}

// Reproduces the settlement vault's claim-once mapping key. It deliberately
// differs from the Merkle payout leaf, whose preimage includes shareBPS.
func finalSemanticVaultClaimKey(noID uint64, coldkey string) (string, error) {
	if noID == 0 {
		return "", errors.New("claim key has zero operator")
	}
	if err := requireFinalHex32("claim coldkey", coldkey); err != nil {
		return "", err
	}
	var noIDWord [common.HashLength]byte
	new(big.Int).SetUint64(noID).FillBytes(noIDWord[:])
	coldkeyHash := common.HexToHash(coldkey)
	return strings.ToLower(crypto.Keccak256Hash(append(noIDWord[:], coldkeyHash[:]...)).Hex()), nil
}

// Reproduces the contract's double-hashed payout leaf for one coldkey/share.
func finalSemanticVaultPayoutLeaf(coldkey string, shareBPS uint64) (string, error) {
	if err := requireFinalHex32("payout leaf coldkey", coldkey); err != nil {
		return "", err
	}
	if shareBPS == 0 || shareBPS > 10_000 {
		return "", errors.New("payout leaf share is invalid")
	}
	coldkeyHash := common.HexToHash(coldkey)
	var shareWord [common.HashLength]byte
	new(big.Int).SetUint64(shareBPS).FillBytes(shareWord[:])
	inner := crypto.Keccak256Hash(append(coldkeyHash[:], shareWord[:]...))
	return strings.ToLower(crypto.Keccak256Hash(inner[:]).Hex()), nil
}

// Proves every represented claim remains marked on chain, then reconciles each
// coldkey's cumulative credit over the whole acceptance interval:
//
//	terminalCredit = baselineCredit + sum(Claimed.amount) - sum(ClaimPaid.amount)
//
// ClaimPaid may settle prior credit and can originate in a withdrawal-only
// transaction, so neither side of this equation may be inferred per leaf.
func verifyFinalSemanticVaultClaims(ctx context.Context, evidence *FinalSemanticEvidence, reader FinalSemanticChainReader, appendExchanges func(string, ChainHead, []FinalRPCExchange) error) error {
	if evidence == nil || reader == nil || appendExchanges == nil {
		return errors.New("vault claim audit is unavailable")
	}
	claimedByColdkey := map[string]*big.Int{}
	paidByColdkey := map[string]*big.Int{}
	coldkeys := map[string]struct{}{}
	for _, row := range evidence.Epochs {
		for _, claim := range row.Claims {
			claimed, err := finalPositiveInteger("claimed payout leaf", claim.ClaimedRao)
			if err != nil {
				return err
			}
			coldkey := strings.ToLower(claim.Payee)
			if _, exists := claimedByColdkey[coldkey]; !exists {
				claimedByColdkey[coldkey] = new(big.Int)
			}
			claimedByColdkey[coldkey].Add(claimedByColdkey[coldkey], claimed)
			coldkeys[coldkey] = struct{}{}
		}
	}
	for _, payment := range evidence.ClaimPayments {
		coldkey := strings.ToLower(payment.Coldkey)
		amount, err := finalPositiveInteger("ClaimPaid amount", payment.AmountRao)
		if err != nil {
			return err
		}
		if _, exists := paidByColdkey[coldkey]; !exists {
			paidByColdkey[coldkey] = new(big.Int)
		}
		paidByColdkey[coldkey].Add(paidByColdkey[coldkey], amount)
		coldkeys[coldkey] = struct{}{}
	}
	for _, row := range evidence.Epochs {
		for _, claim := range row.Claims {
			coldkey := strings.ToLower(claim.Payee)
			state, exchanges, err := reader.VaultClaim(ctx, row.Epoch, row.NoID, coldkey, claim.ShareBPS, evidence.EVMTerminalHead)
			if err != nil {
				return fmt.Errorf("epoch=%d no=%d leaf=%d: %w", row.Epoch, row.NoID, claim.LeafIndex, err)
			}
			if err := appendExchanges("evm", evidence.EVMTerminalHead, exchanges); err != nil {
				return err
			}
			wantPayoutLeaf, err := finalSemanticVaultPayoutLeaf(coldkey, claim.ShareBPS)
			if err != nil {
				return err
			}
			wantClaimKey, err := finalSemanticVaultClaimKey(row.NoID, coldkey)
			if err != nil {
				return err
			}
			if state.Epoch != row.Epoch || state.NoID != row.NoID || state.Block != evidence.EVMTerminalHead || state.Coldkey != coldkey || state.ShareBPS != claim.ShareBPS || !state.LeafClaimed || state.PayoutLeaf != wantPayoutLeaf || state.ClaimKey != wantClaimKey {
				return fmt.Errorf("terminal vault claim state epoch=%d no=%d leaf=%d differs from semantic evidence", row.Epoch, row.NoID, claim.LeafIndex)
			}
		}
	}
	orderedColdkeys := make([]string, 0, len(coldkeys))
	for coldkey := range coldkeys {
		orderedColdkeys = append(orderedColdkeys, coldkey)
	}
	sort.Strings(orderedColdkeys)
	for _, coldkey := range orderedColdkeys {
		baseline, exchanges, err := reader.VaultClaimCredit(ctx, coldkey, evidence.Window.BaselineHead)
		if err != nil {
			return fmt.Errorf("baseline claim credit coldkey=%s: %w", coldkey, err)
		}
		if err := appendExchanges("evm", evidence.Window.BaselineHead, exchanges); err != nil {
			return err
		}
		terminal, exchanges, err := reader.VaultClaimCredit(ctx, coldkey, evidence.EVMTerminalHead)
		if err != nil {
			return fmt.Errorf("terminal claim credit coldkey=%s: %w", coldkey, err)
		}
		if err := appendExchanges("evm", evidence.EVMTerminalHead, exchanges); err != nil {
			return err
		}
		if baseline.Coldkey != coldkey || baseline.Block != evidence.Window.BaselineHead || terminal.Coldkey != coldkey || terminal.Block != evidence.EVMTerminalHead {
			return fmt.Errorf("claim credit coldkey=%s has mismatched pinned state", coldkey)
		}
		baselineAmount, err := finalNonnegativeInteger("baseline claim credit", baseline.CreditRao)
		if err != nil {
			return err
		}
		terminalAmount, err := finalNonnegativeInteger("terminal claim credit", terminal.CreditRao)
		if err != nil {
			return err
		}
		available := new(big.Int).Set(baselineAmount)
		if claimed := claimedByColdkey[coldkey]; claimed != nil {
			available.Add(available, claimed)
		}
		paid := paidByColdkey[coldkey]
		if paid == nil {
			paid = new(big.Int)
		}
		if available.Cmp(paid) < 0 {
			return fmt.Errorf("claim credit coldkey=%s underflows baseline plus represented claims", coldkey)
		}
		wantTerminal := new(big.Int).Sub(available, paid)
		if terminalAmount.Cmp(wantTerminal) != 0 {
			return fmt.Errorf("claim credit coldkey=%s terminal balance differs from accepted claim/payment ledger", coldkey)
		}
	}
	return nil
}
