package ledger

// recovery.go — the phase C recovery component (VERSION2-FINAL.md §16 step
// 6). The v1 sweep moves v1 carry into vault2, where it is assigned as
// surplus to a v2 epoch's funding (vault2 assignSurplus, which emits
// SurplusAssigned(noId, lastFundedEpoch + 1, amount)). A recovered source
// epoch's missed-root funding is then allocated by that source epoch's own
// provider rows; the rest of the epoch's funding, f_k − R_k, is ordinary.
//
// Which source epochs a pool recovers is the owner's decision, fixed here in
// code so that the validator's configuration, and with it its config_hash,
// does not change. Where the recovery lands is a fact of the chain: the
// first epoch whose own assigned surplus reaches the recovered total. Both
// the operator's builder and the validator compute it with PlanRecovery from
// vault2's SurplusAssigned events, and both check each source's amount
// against vault1's terminal RootMissed entitlement (CheckSource).

import (
	"errors"
	"fmt"
	"slices"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/protocol"
)

var (
	// ErrRecoveryInput is returned by Build for recovery components that are
	// not in ascending order of distinct source epochs before their covered
	// epoch, each with a positive amount.
	ErrRecoveryInput = errors.New("ledger: recovery components must be ascending distinct source epochs before their epoch, with positive amounts")
	// ErrRecoveryFunding is returned by Build when an epoch's recovery
	// amounts exceed its funding f_k.
	ErrRecoveryFunding = errors.New("ledger: recovery amounts exceed the epoch's funding")
	// ErrRecoverySource is returned by CheckSource when vault1's entitlement
	// of a source epoch is not the scheduled one.
	ErrRecoverySource = errors.New("ledger: the source epoch's v1 entitlement is not the scheduled recovery")

	// Check 10, recovery.

	// ErrRecoveryUnexpected: the header lists a recovery component that the
	// chain does not call for at that epoch.
	ErrRecoveryUnexpected = errors.New("ledger: header lists a recovery component the chain does not call for")
	// ErrRecoveryMissing: the header omits a recovery component that the
	// chain calls for.
	ErrRecoveryMissing = errors.New("ledger: header omits a recovery component the chain calls for")
	// ErrRecoveryAmount: a recovery component's amount is not the scheduled one.
	ErrRecoveryAmount = errors.New("ledger: a recovery component's amount is not the scheduled amount")
)

// V1EpochRootMissed is vault1's EpochStatus.RootMissed: the terminal status
// of a v1 epoch whose funding went to carry because no root was committed.
const V1EpochRootMissed uint8 = 3

// Recovery is the recovered funding of one source epoch.
type Recovery struct {
	// Epoch is the source epoch, a v1 epoch before the pool's first v2 epoch.
	Epoch uint64
	// Amount is vault1's funding of the source epoch, in rao.
	Amount uint64
}

// RecoveryInput is one recovery component of a covered epoch's input to
// Build: the source epoch's amount and provider rows (§6.2), built with the
// v2 rules over the source epoch's own earning window, wallets and head
// bindings.
type RecoveryInput struct {
	Recovery
	// Null declares the source epoch's inputs unrecoverable. Set it only on
	// an explicit operator command naming the source epoch.
	Null bool
	// Rows are the source epoch's provider rows, sorted by client id.
	Rows []protocol.LedgerUsageRow
	// Chunks, when not nil, are the references the source epoch's rows were
	// first recorded with. Rows must encode to exactly these chunks.
	Chunks []Ref
}

// RecoveryAllocation is what one recovery component contributed to a root.
type RecoveryAllocation struct {
	Recovery
	Null      bool
	Providers []Ref
	// Allocated reports that the source epoch's rows had eligible usage, so
	// Deltas, which sum to Amount, split it. Otherwise Amount joined the
	// covered epoch's ordinary amount.
	Allocated bool
	Deltas    []protocol.LedgerDelta
}

// RecoverySchedule is a pool's recovery of v1 funding by source epoch rows.
type RecoverySchedule struct {
	// Vault is the pool's vault2 address.
	Vault [20]byte
	NoID  uint64
	// Sources are the recovered source epochs in ascending order, each with
	// vault1's funding of it.
	Sources []Recovery
	// FromBlock is a block no SurplusAssigned event of the pool precedes:
	// vault2 assigns surplus only to a registered pool.
	FromBlock uint64
}

// recoverySchedules are the owner's recovery decisions.
var recoverySchedules = []RecoverySchedule{
	{
		// Mainnet (EVM chain 964), operator 1 (owner decision 2026-10-31).
		// Epoch 3's 4,132.715621039 α, vault1 entitlement(3,1), RootMissed
		// once its finalize ran after its root window closed without a root,
		// is allocated by epoch 3's own rows. Epoch 1's 3,482.257057872 α,
		// vault1 entitlement(1,1), had essentially no consented rows and is
		// ordinary funding of the receiving epoch, as is the rest of the
		// swept surplus (the reveal-lag capture, transfer rounding). The pool
		// was registered at block 9,386,659.
		Vault:     mainnetVault2,
		NoID:      1,
		Sources:   []Recovery{{Epoch: 3, Amount: 4_132_715_621_039}},
		FromBlock: 9_386_659,
	},
}

// ScheduledRecovery returns a copy of the recovery schedule of a vault2
// pool, or nil when it has none.
func ScheduledRecovery(vault [20]byte, noID uint64) *RecoverySchedule {
	for _, schedule := range recoverySchedules {
		if schedule.Vault == vault && schedule.NoID == noID {
			schedule.Sources = slices.Clone(schedule.Sources)
			return &schedule
		}
	}
	return nil
}

// total is the sum of the scheduled amounts.
func (s *RecoverySchedule) total() uint256.Int {
	var total uint256.Int
	for _, source := range s.Sources {
		total.AddUint64(&total, source.Amount)
	}
	return total
}

// CheckSource requires vault1's entitlement of a scheduled source epoch to be
// RootMissed with exactly the scheduled funding. RootMissed is terminal, so
// the answer never changes once it holds.
func (s *RecoverySchedule) CheckSource(epoch uint64, status uint8, funded *uint256.Int) error {
	i := slices.IndexFunc(s.Sources, func(source Recovery) bool { return source.Epoch == epoch })
	switch {
	case i < 0:
		return fmt.Errorf("%w: epoch %d is not a scheduled source", ErrRecoverySource, epoch)
	case status != V1EpochRootMissed:
		return fmt.Errorf("%w: epoch %d has status %d, not RootMissed", ErrRecoverySource, epoch, status)
	case !funded.Eq(uint256.NewInt(s.Sources[i].Amount)):
		return fmt.Errorf("%w: epoch %d was funded %s, not %d", ErrRecoverySource, epoch, funded.Dec(), s.Sources[i].Amount)
	}
	return nil
}

// PlanRecovery returns the recovery components that the covered epochs p+1 …
// e of a root must carry. surplus holds, per epoch, the total of vault2's
// SurplusAssigned amounts for the pool. The recovery lands in the first epoch
// k at or after firstEpoch whose own surplus reaches the scheduled total,
// since vault2 adds each assignment to epoch k's funding, f_k. A root
// carries it only when it covers k; an earlier root that covered k carried it
// already. A nil schedule, or no such epoch, plans nothing.
func PlanRecovery(schedule *RecoverySchedule, surplus map[uint64]uint256.Int, firstEpoch, p, e uint64) map[uint64][]Recovery {
	if schedule == nil || len(schedule.Sources) == 0 {
		return nil
	}
	total := schedule.total()
	epochs := make([]uint64, 0, len(surplus))
	for k := range surplus {
		epochs = append(epochs, k)
	}
	slices.Sort(epochs)
	for _, k := range epochs {
		if k < firstEpoch || k > e {
			continue
		}
		if assigned := surplus[k]; assigned.Lt(&total) {
			continue
		}
		if k <= p {
			return nil
		}
		return map[uint64][]Recovery{k: slices.Clone(schedule.Sources)}
	}
	return nil
}

// checkRecoveryInputs checks a covered epoch's recovery components for Build
// and returns their total.
func checkRecoveryInputs(epoch uint64, components []RecoveryInput) (uint256.Int, error) {
	var total uint256.Int
	for i := range components {
		c := &components[i]
		if c.Amount == 0 || c.Epoch >= epoch || (i > 0 && c.Epoch <= components[i-1].Epoch) {
			return total, fmt.Errorf("%w: epoch %d component %d", ErrRecoveryInput, epoch, i)
		}
		total.AddUint64(&total, c.Amount)
	}
	return total, nil
}
