package ledger

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/protocol"
)

// ErrDeltas is returned for deltas that are not in strictly ascending
// coldkey order.
var ErrDeltas = errors.New("ledger: deltas are not in strictly ascending coldkey order")

// The account rules of §6.1 are shared by every v2 pool whose root commits
// cumulative per-coldkey accounts to vault2, the capture the flag pool
// (package ctf) among them: ApplyDeltas is the builder's half,
// CheckAccountRows and CheckAccountDeltas the auditor's (checks 8 and 7).

// ApplyDeltas returns the accounts of a root built on a finalized root whose
// accounts, in position order, are previous: each coldkey's positive delta is
// added to its account, and each coldkey without one gets a new account,
// appended in ascending coldkey order. Previous positions and coldkeys never
// move. deltas must be in strictly ascending coldkey order; zero deltas are
// skipped. It refuses previous accounts that repeat a coldkey (ErrPrevious)
// and deltas out of order (ErrDeltas).
func ApplyDeltas(previous []merkle.LedgerAccount, deltas []protocol.LedgerDelta) ([]merkle.LedgerAccount, error) {
	if err := checkDeltaOrder(deltas); err != nil {
		return nil, err
	}
	order, duplicate := coldkeyOrder(previous)
	if duplicate >= 0 {
		return nil, fmt.Errorf("%w: account %d repeats a coldkey", ErrPrevious, duplicate)
	}
	return applyDeltas(previous, order, deltas), nil
}

// CheckAccountRows runs §9.2 check 8 on a pending root's published accounts
// against the previous finalized root's: every previous position keeps its
// coldkey (ErrAccountMoved) without decreasing (ErrAccountLowered), no
// coldkey has two accounts (ErrAccountDuplicate), and the new accounts are in
// ascending coldkey order (ErrAccountUnsorted) with positive cumulatives
// (ErrAccountZeroDelta).
func CheckAccountRows(previous, accounts []merkle.LedgerAccount) error {
	_, err := checkAccountRows(previous, accounts, Faults(previous, accounts))
	return err
}

// CheckAccountDeltas runs §9.2 check 7's comparison on accounts that passed
// check 8: each account's increase over previous is its coldkey's delta
// (ErrAccountDelta), and each coldkey with a positive delta has an account
// (ErrAccountOmitted). deltas must be in strictly ascending coldkey order
// (ErrDeltas).
func CheckAccountDeltas(previous, accounts []merkle.LedgerAccount, deltas []protocol.LedgerDelta) error {
	if err := checkDeltaOrder(deltas); err != nil {
		return err
	}
	order, duplicate := coldkeyOrder(accounts)
	if duplicate >= 0 {
		return fmt.Errorf("%w: account %d", ErrAccountDuplicate, duplicate)
	}
	return checkAccountDeltas(previous, accounts, order, deltas)
}

func checkDeltaOrder(deltas []protocol.LedgerDelta) error {
	for i := 1; i < len(deltas); i++ {
		if bytes.Compare(deltas[i-1].Coldkey[:], deltas[i].Coldkey[:]) >= 0 {
			return fmt.Errorf("%w: delta %d", ErrDeltas, i)
		}
	}
	return nil
}

// checkAccountRows is check 8 with the faults of accounts against previous
// already listed. It returns the accounts' positions in ascending coldkey
// order.
func checkAccountRows(previous, accounts []merkle.LedgerAccount, faults []Fault) ([]uint32, error) {
	if len(faults) > 0 {
		fault := faults[0]
		if fault.Kind == merkle.LedgerFaultLowerValue {
			return nil, fmt.Errorf("%w: account %d", ErrAccountLowered, fault.Index)
		}
		return nil, fmt.Errorf("%w: account %d", ErrAccountMoved, fault.Index)
	}
	order, duplicate := coldkeyOrder(accounts)
	if duplicate >= 0 {
		return nil, fmt.Errorf("%w: account %d", ErrAccountDuplicate, duplicate)
	}
	for i := len(previous); i < len(accounts); i++ {
		if i > len(previous) && bytes.Compare(accounts[i-1].Coldkey[:], accounts[i].Coldkey[:]) > 0 {
			return nil, fmt.Errorf("%w: account %d", ErrAccountUnsorted, i)
		}
		if accounts[i].Cumulative.IsZero() {
			return nil, fmt.Errorf("%w: account %d", ErrAccountZeroDelta, i)
		}
	}
	return order, nil
}

// checkAccountDeltas is check 7's comparison over accounts that passed
// check 8, so increases are not negative and each coldkey has at most one
// account; order is the accounts' positions in ascending coldkey order and
// sum the deltas in ascending coldkey order.
func checkAccountDeltas(previous, accounts []merkle.LedgerAccount, order []uint32, sum []protocol.LedgerDelta) error {
	// expected[pos] is the delta of the account at pos.
	expected := make([]uint256.Int, len(accounts))
	k := 0
	for i := range sum {
		d := &sum[i]
		for k < len(order) && bytes.Compare(accounts[order[k]].Coldkey[:], d.Coldkey[:]) < 0 {
			k++
		}
		if k < len(order) && accounts[order[k]].Coldkey == d.Coldkey {
			expected[order[k]] = d.Delta
			continue
		}
		if !d.Delta.IsZero() {
			return fmt.Errorf("%w: coldkey %x", ErrAccountOmitted, d.Coldkey)
		}
	}
	for i := range accounts {
		increase := accounts[i].Cumulative
		if i < len(previous) {
			increase.Sub(&increase, &previous[i].Cumulative)
		}
		if !increase.Eq(&expected[i]) {
			return fmt.Errorf("%w: account %d increased %s, delta %s", ErrAccountDelta, i, increase.Dec(), expected[i].Dec())
		}
	}
	return nil
}
