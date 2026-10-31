package ledger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/protocol"
)

var (
	// ErrPrevious is returned when the previous accounts do not rebuild the
	// previous finalized root, or repeat a coldkey.
	ErrPrevious = errors.New("ledger: previous accounts do not rebuild the previous root")
	// ErrEpochInput is returned for an unrecoverable epoch given rows or chunks.
	ErrEpochInput = errors.New("ledger: an unrecoverable epoch has rows")
	// ErrFixedInputs is returned when an epoch's rows differ from the chunks
	// it was first built with (§6.4 fixed inputs).
	ErrFixedInputs = errors.New("ledger: rows differ from the epoch's fixed chunks")
	// ErrTotalNotIncreased is returned when the root's total would not exceed
	// the previous root's total. vault2 refuses such a commit; the operator
	// commits nothing for the epoch.
	ErrTotalNotIncreased = errors.New("ledger: total does not exceed the previous total")
	// ErrEpochNotAllocated is returned when the root's own epoch has no
	// eligible usage, or is unrecoverable: a root's last epoch must allocate.
	ErrEpochNotAllocated = errors.New("ledger: the root's epoch does not allocate")
)

// Root is a root as vault2 records it, finalized or pending.
type Root struct {
	Epoch      uint64
	Root       [32]byte
	Total      uint256.Int
	LeafCount  uint64
	LedgerHash [32]byte
}

// Previous is the latest finalized root with its accounts in position order.
type Previous struct {
	Root     Root
	Accounts []merkle.LedgerAccount
}

// EpochInput is one covered epoch's input to Build.
type EpochInput struct {
	Epoch uint64
	// Null declares the epoch's inputs unrecoverable ("providers": null). Set
	// it only on an explicit operator command naming the epoch.
	Null bool
	// Rows are the epoch's provider rows (§6.2), sorted by client id.
	Rows []protocol.LedgerUsageRow
	// Chunks, when not nil, are the chunk references the epoch's rows were
	// first built with. Rows must encode to exactly these chunks. Leave it
	// nil the first time, or to rebuild on an operator command.
	Chunks []Ref
	// Recovery lists the epoch's recovery components (PlanRecovery), in
	// ascending source epoch order. Each is allocated by its source epoch's
	// rows, and the epoch's own rows allocate f_k less their amounts.
	Recovery []RecoveryInput
}

// BuildInput is a root's input.
type BuildInput struct {
	NoID uint64
	// Epoch is the root's epoch e.
	Epoch      uint64
	FirstEpoch uint64
	// Previous is the latest finalized root, nil for the pool's first root.
	Previous *Previous
	// Epochs are the covered epochs p+1 … e, in order.
	Epochs []EpochInput
	// FundedThrough holds vault2's fundedThrough[k] for every covered epoch k.
	FundedThrough map[uint64]uint256.Int
}

// EpochAllocation is what one covered epoch contributed to a root.
type EpochAllocation struct {
	Epoch     uint64
	Null      bool
	Providers []Ref
	// Funding is f_k.
	Funding uint256.Int
	// Allocated reports that the epoch's rows had eligible usage. Amount, f_k
	// less the epoch's allocated recovery plus the carry from earlier
	// epochs, was then split into Deltas.
	Allocated bool
	Amount    uint256.Int
	// Deltas lists every coldkey with eligible usage in ascending order, zero
	// deltas included. They sum to Amount.
	Deltas []protocol.LedgerDelta
	// Recovery lists the epoch's recovery components and what each allocated.
	Recovery []RecoveryAllocation
}

// Objects holds ledger objects by reference.
type Objects map[Ref][]byte

// Ledger is a built root.
type Ledger struct {
	// Commit is the root to commit; Ref(Commit.LedgerHash) names Header.
	Commit Root
	Header []byte
	// Accounts are the root's accounts in position order, and Tree their tree.
	Accounts []merkle.LedgerAccount
	Tree     *merkle.LedgerSumTree
	// Objects holds the header and every chunk the header lists. Publish the
	// chunks before the header.
	Objects Objects
	Epochs  []EpochAllocation
}

// Build builds the root for epoch e (§6.4, §6.5, §8.2). Each covered epoch
// with eligible usage is allocated from its own rows; the others carry their
// funding forward. A covered epoch's recovery components (§16 step 6) are
// each allocated from their source epoch's rows first, and the epoch's own
// walk step takes f_k less the amounts they allocated. Each coldkey's deltas
// are added to its account, and coldkeys without an account get new
// accounts, appended in ascending order. The result depends only on the
// input.
func Build(ctx context.Context, in BuildInput) (*Ledger, error) {
	if ctx == nil {
		return nil, ErrInput
	}
	p := in.FirstEpoch - 1
	if in.Previous != nil {
		p = in.Previous.Root.Epoch
	}
	if in.Epoch <= p {
		return nil, fmt.Errorf("%w: epoch %d does not follow %d", ErrCoveredEpochs, in.Epoch, p)
	}
	if in.Epoch-p > MaxCoveredEpochs {
		return nil, fmt.Errorf("%w: epochs %d to %d", ErrTooManyEpochs, p+1, in.Epoch)
	}
	if uint64(len(in.Epochs)) != in.Epoch-p {
		return nil, fmt.Errorf("%w: %d inputs for epochs %d to %d", ErrCoveredEpochs, len(in.Epochs), p+1, in.Epoch)
	}
	for i := range in.Epochs {
		if in.Epochs[i].Epoch != p+1+uint64(i) {
			return nil, fmt.Errorf("%w: input %d is epoch %d", ErrCoveredEpochs, i, in.Epochs[i].Epoch)
		}
	}

	var previous []merkle.LedgerAccount
	var base uint256.Int
	if in.Previous != nil {
		tree, err := merkle.BuildLedgerSumTree(ctx, in.Previous.Accounts)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrPrevious, err)
		}
		root, total := tree.Root()
		if root != in.Previous.Root.Root || !total.Eq(&in.Previous.Root.Total) || tree.LeafCount() != in.Previous.Root.LeafCount {
			return nil, ErrPrevious
		}
		previous, base = in.Previous.Accounts, total
	}
	order, duplicate := coldkeyOrder(previous)
	if duplicate >= 0 {
		return nil, fmt.Errorf("%w: account %d repeats a coldkey", ErrPrevious, duplicate)
	}
	funding, last, err := fundings(in.FundedThrough, p, in.Epoch, &base)
	if err != nil {
		return nil, err
	}
	if last.Eq(&base) {
		return nil, ErrTotalNotIncreased
	}

	objects := Objects{}
	header := &Header{NoID: in.NoID, Epoch: in.Epoch, Epochs: make([]HeaderEpoch, len(in.Epochs))}
	epochs := make([]EpochAllocation, len(in.Epochs))
	var w walk
	for i := range in.Epochs {
		input, out := &in.Epochs[i], &epochs[i]
		out.Epoch, out.Null, out.Funding = input.Epoch, input.Null, funding[i]
		if input.Null {
			if len(input.Rows) != 0 || input.Chunks != nil {
				return nil, fmt.Errorf("%w: epoch %d", ErrEpochInput, input.Epoch)
			}
		} else {
			if err := checkProviderRows(input.Rows); err != nil {
				return nil, fmt.Errorf("epoch %d: %w", input.Epoch, err)
			}
			chunks := EncodeProviderChunks(input.Rows)
			out.Providers = make([]Ref, len(chunks))
			for j, chunk := range chunks {
				out.Providers[j] = RefOf(chunk)
				objects[out.Providers[j]] = chunk
			}
			if input.Chunks != nil && !slices.Equal(input.Chunks, out.Providers) {
				return nil, fmt.Errorf("%w: epoch %d", ErrFixedInputs, input.Epoch)
			}
		}
		ordinary, err := buildRecovery(ctx, &w, input, out, &funding[i], objects)
		if err != nil {
			return nil, err
		}
		if out.Amount, out.Deltas, err = w.step(ctx, &ordinary, input.Rows, input.Null); err != nil {
			return nil, fmt.Errorf("epoch %d: %w", input.Epoch, err)
		}
		out.Allocated = w.allocated
		header.Epochs[i] = HeaderEpoch{Epoch: out.Epoch, Null: out.Null, Providers: out.Providers}
		for _, component := range out.Recovery {
			header.Epochs[i].Recovery = append(header.Epochs[i].Recovery, HeaderRecovery{Epoch: component.Epoch, Amount: component.Amount, Null: component.Null, Providers: component.Providers})
		}
	}
	if !w.allocated {
		return nil, fmt.Errorf("%w: epoch %d", ErrEpochNotAllocated, in.Epoch)
	}

	accounts := applyDeltas(previous, order, w.sum)
	tree, err := merkle.BuildLedgerSumTree(ctx, accounts)
	if err != nil {
		return nil, err
	}
	for _, chunk := range encodeAccountChunks(accounts) {
		ref := RefOf(chunk)
		header.Accounts = append(header.Accounts, ref)
		objects[ref] = chunk
	}
	data, err := EncodeHeader(header)
	if err != nil {
		return nil, err
	}
	ledgerHash := sha256.Sum256(data)
	objects[ledgerHash] = data
	root, total := tree.Root()
	return &Ledger{
		Commit:   Root{Epoch: in.Epoch, Root: root, Total: total, LeafCount: tree.LeafCount(), LedgerHash: ledgerHash},
		Header:   data,
		Accounts: accounts,
		Tree:     tree,
		Objects:  objects,
		Epochs:   epochs,
	}, nil
}

// buildRecovery checks and allocates a covered epoch's recovery components
// into out and the walk, encodes their source rows into objects, and returns
// the epoch's ordinary funding: f_k less the amounts allocated. A component
// whose source epoch has no rows, no eligible usage, or is unrecoverable
// leaves its amount in the ordinary funding.
func buildRecovery(ctx context.Context, w *walk, input *EpochInput, out *EpochAllocation, f *uint256.Int, objects Objects) (uint256.Int, error) {
	ordinary := *f
	if len(input.Recovery) == 0 {
		return ordinary, nil
	}
	total, err := checkRecoveryInputs(input.Epoch, input.Recovery)
	if err != nil {
		return ordinary, err
	}
	if total.Gt(f) {
		return ordinary, fmt.Errorf("%w: epoch %d recovers %s of %s", ErrRecoveryFunding, input.Epoch, total.Dec(), f.Dec())
	}
	out.Recovery = make([]RecoveryAllocation, len(input.Recovery))
	for j := range input.Recovery {
		component, alloc := &input.Recovery[j], &out.Recovery[j]
		alloc.Recovery, alloc.Null = component.Recovery, component.Null
		if component.Null {
			if len(component.Rows) != 0 || component.Chunks != nil {
				return ordinary, fmt.Errorf("%w: epoch %d recovery %d", ErrEpochInput, input.Epoch, component.Epoch)
			}
			continue
		}
		if err := checkProviderRows(component.Rows); err != nil {
			return ordinary, fmt.Errorf("epoch %d recovery %d: %w", input.Epoch, component.Epoch, err)
		}
		chunks := EncodeProviderChunks(component.Rows)
		alloc.Providers = make([]Ref, len(chunks))
		for k, chunk := range chunks {
			alloc.Providers[k] = RefOf(chunk)
			objects[alloc.Providers[k]] = chunk
		}
		if component.Chunks != nil && !slices.Equal(component.Chunks, alloc.Providers) {
			return ordinary, fmt.Errorf("%w: epoch %d recovery %d", ErrFixedInputs, input.Epoch, component.Epoch)
		}
		amount := uint256.NewInt(component.Amount)
		if alloc.Allocated, alloc.Deltas, err = w.recover(ctx, amount, component.Rows); err != nil {
			return ordinary, fmt.Errorf("epoch %d recovery %d: %w", input.Epoch, component.Epoch, err)
		}
		if alloc.Allocated {
			ordinary.Sub(&ordinary, amount)
		}
	}
	return ordinary, nil
}

// applyDeltas adds each positive delta to its coldkey's account, keeping
// every previous position, and appends an account for each coldkey without
// one. order is previous in ascending coldkey order, and deltas are
// ascending too, so the new accounts are appended in ascending order.
func applyDeltas(previous []merkle.LedgerAccount, order []uint32, deltas []protocol.LedgerDelta) []merkle.LedgerAccount {
	accounts := slices.Clone(previous)
	k := 0
	for i := range deltas {
		d := &deltas[i]
		if d.Delta.IsZero() {
			continue
		}
		for k < len(order) && bytes.Compare(previous[order[k]].Coldkey[:], d.Coldkey[:]) < 0 {
			k++
		}
		if k < len(order) && previous[order[k]].Coldkey == d.Coldkey {
			account := &accounts[order[k]]
			account.Cumulative.Add(&account.Cumulative, &d.Delta)
			continue
		}
		accounts = append(accounts, merkle.LedgerAccount{Coldkey: d.Coldkey, Cumulative: d.Delta})
	}
	return accounts
}
