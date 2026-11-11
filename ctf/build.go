package ctf

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/protocol"
)

// ErrColdkey is returned by Build for a winner without a nonzero payout
// coldkey in its epoch's Coldkeys.
var ErrColdkey = errors.New("ctf: a winner has no payout coldkey")

// Previous is the latest finalized CTF root: its accounts in position order,
// and its State, the pot out and pending rows of its last covered epoch.
type Previous struct {
	Root     ledger.Root
	Accounts []merkle.LedgerAccount
	State    State
}

// CensusInput is an epoch's committed census: the chain's record of
// commitCtfCensus and the census objects.
type CensusInput struct {
	// Hash is ctfCensus(n, k)'s census hash, Block its block B_k and Time
	// that block's EVM timestamp, in unix seconds.
	Hash  ledger.Ref
	Block uint64
	Time  uint64
	// Objects holds the census header (Hash), its row chunks and its
	// exclusion object.
	Objects ledger.Objects
}

// EpochInput is one covered epoch's input to Build.
type EpochInput struct {
	Epoch uint64
	// Census is the epoch's committed census, nil when none was committed in
	// [end(k), end(k) + 600].
	Census *CensusInput
	// Pulse is the quicknet pulse of DrawRound(Census.Time). It is needed
	// when the census is canonical, and is otherwise ignored.
	Pulse *Pulse
	// Recycled is the carry contract's recycledFor[k].
	Recycled uint256.Int
	// Bound maps client ids to an active fleet binding at either boundary of
	// the epoch, as ledger.HeadOracle.HeadBindings returns them. The draw
	// leaves out every candidate mapped to true.
	Bound map[[16]byte]bool
	// Coldkeys maps client ids to their payout coldkeys under the provider
	// pool's wallet selection (§3.2); it must hold every winner's.
	Coldkeys map[[16]byte][32]byte
	// Awards, when not nil, is the awards object the epoch was first built
	// with; the awards must encode to exactly it (fixed inputs, §5). Leave it
	// nil the first time, or to rebuild on an operator command naming the
	// epoch.
	Awards *ledger.Ref
}

// BuildInput is a CTF root's input.
type BuildInput struct {
	// NoID is the operator n, below 2^32; the pool is PoolID(NoID).
	NoID uint64
	// ChainID and Netuid seed the draw: 964 and 25 on mainnet.
	ChainID uint64
	Netuid  uint16
	// Epoch is the root's epoch e, and FirstEpoch the pool's first epoch.
	Epoch      uint64
	FirstEpoch uint64
	// CarryColdkey is the carry contract's coldkey (§4.6).
	CarryColdkey [32]byte
	// Previous is the latest finalized root, nil for the pool's first root.
	Previous *Previous
	// Epochs are the covered epochs p+1 … e, in order.
	Epochs []EpochInput
	// FundedThrough holds vault2's fundedThrough[k] for every covered epoch.
	FundedThrough map[uint64]uint256.Int
}

// Ledger is a built CTF root.
type Ledger struct {
	// Commit is the root to commit with commitDistribution for the pool
	// PoolID(NoID); ledger.Ref(Commit.LedgerHash) names Header.
	Commit ledger.Root
	Header []byte
	// Accounts are the root's accounts in position order, and Tree their tree.
	Accounts []merkle.LedgerAccount
	Tree     *merkle.LedgerSumTree
	// Objects holds the header and every object it names, the census objects
	// and the countries object included. Publish the others before the header.
	Objects ledger.Objects
	// Epochs reports each covered epoch.
	Epochs []*EpochResult
	// Deltas are every coldkey's delta over the covered epochs, ascending,
	// the carry coldkey's included.
	Deltas []protocol.LedgerDelta
	// State is the pool's state after the root's last epoch.
	State State
}

// Build builds the CTF root for epoch e (§4.5, §5). Each covered epoch's
// committed census, when canonical, is drawn with the pulse of ρ_k; then the
// walk of §4.5 pays slot amounts, pot shares and pending awards, and the carry
// coldkey receives the rest, so the total is fundedThrough[e]. Each coldkey's
// delta is added to its account, and coldkeys without one get new accounts,
// appended in ascending order. The result depends only on the input.
//
// Build refuses ErrInput for a missing context or input, ledger.ErrPrevious
// for previous accounts or state that do not rebuild the previous root,
// ledger.ErrTotalNotIncreased when fundedThrough[e] does not exceed the
// previous total, ErrCensusUnavailable for a census object it was not given,
// ErrPulse for a pulse that does not verify, ErrColdkey for a winner without a
// coldkey, ledger.ErrFixedInputs for awards that differ from the fixed ones
// and ErrReturnRolls for returned carry recorded for a last epoch that funded
// nothing; and the covered-epoch, funding and size errors of the ledger
// package. A committed census that is not canonical gives no draw, as none
// would: it is reported in EpochResult.CensusErr, not refused. The census is
// read in a fixed order, the header, the row chunks, the exclusion object, and
// an object longer than a canonical census holds ends the reading as not
// canonical, exactly as Verify reads it.
func Build(ctx context.Context, in BuildInput) (*Ledger, error) {
	if ctx == nil {
		return nil, ErrInput
	}
	p, err := checkPool(in.NoID, in.CarryColdkey, in.FirstEpoch, in.Previous != nil)
	if err != nil {
		return nil, err
	}
	if in.Previous != nil {
		p = in.Previous.Root.Epoch
	}
	if err := checkCovered(p, in.Epoch); err != nil {
		return nil, err
	}
	if uint64(len(in.Epochs)) != in.Epoch-p {
		return nil, fmt.Errorf("%w: %d inputs for epochs %d to %d", ledger.ErrCoveredEpochs, len(in.Epochs), p+1, in.Epoch)
	}
	for i := range in.Epochs {
		if in.Epochs[i].Epoch != p+1+uint64(i) {
			return nil, fmt.Errorf("%w: input %d is epoch %d", ledger.ErrCoveredEpochs, i, in.Epochs[i].Epoch)
		}
	}

	var previous []merkle.LedgerAccount
	var base uint256.Int
	state := &State{}
	if in.Previous != nil {
		tree, err := merkle.BuildLedgerSumTree(ctx, in.Previous.Accounts)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ledger.ErrPrevious, err)
		}
		root, total := tree.Root()
		if root != in.Previous.Root.Root || !total.Eq(&in.Previous.Root.Total) || tree.LeafCount() != in.Previous.Root.LeafCount {
			return nil, ledger.ErrPrevious
		}
		if err := checkState(&in.Previous.State); err != nil {
			return nil, fmt.Errorf("%w: %w", ledger.ErrPrevious, err)
		}
		previous, base, state = in.Previous.Accounts, total, &in.Previous.State
	}
	funding, last, err := fundings(in.FundedThrough, p, in.Epoch, &base)
	if err != nil {
		return nil, err
	}
	if last.Eq(&base) {
		return nil, ledger.ErrTotalNotIncreased
	}

	poolID := PoolID(in.NoID)
	objects := ledger.Objects{CountriesRef: CountriesObject()}
	header := &Header{NoID: in.NoID, Epoch: in.Epoch, CarryColdkey: in.CarryColdkey, Epochs: make([]HeaderEpoch, len(in.Epochs))}
	built := &Ledger{Epochs: make([]*EpochResult, len(in.Epochs))}
	w := newWalk(state)
	for i := range in.Epochs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		input := &in.Epochs[i]
		drawn, err := buildDraw(&in, input, poolID, objects)
		if err != nil {
			return nil, err
		}
		if drawn.draw != nil {
			for j := range drawn.draw.Winners {
				for _, winner := range drawn.draw.Winners[j] {
					if coldkey := input.Coldkeys[winner.ClientID]; coldkey == ([32]byte{}) {
						return nil, fmt.Errorf("%w: epoch %d client %x", ErrColdkey, input.Epoch, winner.ClientID)
					}
				}
			}
		}
		e, err := w.step(input.Epoch, &funding[i], &input.Recycled, drawn.draw, func(winner *Winner) [32]byte { return input.Coldkeys[winner.ClientID] })
		if err != nil {
			return nil, err
		}
		drawn.report(e)
		if err := encodeEpoch(e, objects); err != nil {
			return nil, err
		}
		if input.Awards != nil && *input.Awards != e.AwardsRef {
			return nil, fmt.Errorf("%w: epoch %d awards", ledger.ErrFixedInputs, input.Epoch)
		}
		header.Epochs[i] = HeaderEpoch{Epoch: e.Epoch, Census: e.Census, Round: e.Round, Awards: e.AwardsRef, Pots: e.PotsRef, Pending: e.PendingRef}
		built.Epochs[i] = e
	}
	if built.Deltas, err = w.finish(in.CarryColdkey); err != nil {
		return nil, err
	}
	built.State = w.state
	if built.Accounts, err = ledger.ApplyDeltas(previous, built.Deltas); err != nil {
		return nil, err
	}
	if built.Tree, err = merkle.BuildLedgerSumTree(ctx, built.Accounts); err != nil {
		return nil, err
	}
	chunks, err := ledger.EncodeAccountChunks(built.Accounts)
	if err != nil {
		return nil, err
	}
	for _, chunk := range chunks {
		ref := ledger.RefOf(chunk)
		header.Accounts = append(header.Accounts, ref)
		objects[ref] = chunk
	}
	if built.Header, err = EncodeHeader(header); err != nil {
		return nil, err
	}
	ledgerHash := sha256.Sum256(built.Header)
	objects[ledgerHash] = built.Header
	built.Objects = objects
	root, total := built.Tree.Root()
	built.Commit = ledger.Root{Epoch: in.Epoch, Root: root, Total: total, LeafCount: built.Tree.LeafCount(), LedgerHash: ledgerHash}
	return built, nil
}

// emptyRef names the empty object: the awards of an epoch without winners,
// pending rows when none are outstanding, an exclusion object without counts.
var emptyRef = ledger.RefOf(nil)

// checkPool checks a pool's fixed inputs and returns p for its first root,
// firstEpoch − 1.
func checkPool(noID uint64, carry [32]byte, firstEpoch uint64, hasPrevious bool) (uint64, error) {
	if noID >= PoolIDBase {
		return 0, fmt.Errorf("%w: operator %d is not below 2^32", ErrInput, noID)
	}
	if carry == ([32]byte{}) {
		return 0, fmt.Errorf("%w: the carry coldkey is zero", ErrInput)
	}
	if !hasPrevious && firstEpoch == 0 {
		return 0, fmt.Errorf("%w: the first epoch is 0", ErrInput)
	}
	return firstEpoch - 1, nil
}

func checkCovered(p, e uint64) error {
	if e <= p {
		return fmt.Errorf("%w: epoch %d does not follow %d", ledger.ErrCoveredEpochs, e, p)
	}
	if e-p > ledger.MaxCoveredEpochs {
		return fmt.Errorf("%w: epochs %d to %d", ledger.ErrTooManyEpochs, p+1, e)
	}
	return nil
}

// checkState checks a root's state as its pot and pending rows would decode.
func checkState(s *State) error {
	for j := range s.Pots {
		if err := checkAmount(&s.Pots[j]); err != nil {
			return err
		}
	}
	return checkPending(s.Pending)
}

// fundings returns f_k = fundedThrough[k] − fundedThrough[k−1] for k = p+1 …
// e, with fundedThrough[p] = base, the previous root's total, and
// fundedThrough[e]. The caller has checked 0 < e − p <= 96.
func fundings(fundedThrough map[uint64]uint256.Int, p, e uint64, base *uint256.Int) ([]uint256.Int, uint256.Int, error) {
	f := make([]uint256.Int, e-p)
	last := *base
	for i := range f {
		k := p + 1 + uint64(i)
		next, ok := fundedThrough[k]
		if !ok {
			return nil, last, fmt.Errorf("%w: epoch %d has no snapshot", ledger.ErrFunding, k)
		}
		if next.Lt(&last) {
			return nil, last, fmt.Errorf("%w: epoch %d decreases", ledger.ErrFunding, k)
		}
		f[i].Sub(&next, &last)
		last = next
	}
	if !last.Lt(amountLimit) {
		return nil, last, fmt.Errorf("%w: epoch %d reaches 2^128", ledger.ErrFunding, e)
	}
	return f, last, nil
}

// epochDraw is an epoch's census and draw, before the walk.
type epochDraw struct {
	census      *ledger.Ref
	censusBlock uint64
	decoded     *Census
	censusErr   error
	round       uint64
	randomness  [32]byte
	draw        *Draw
}

func (d *epochDraw) report(e *EpochResult) {
	e.Census, e.CensusBlock, e.CensusErr = d.census, d.censusBlock, d.censusErr
	if d.decoded != nil {
		e.Candidates, e.Excluded, e.ExcusedSamples = len(d.decoded.Candidates), d.decoded.Excluded, d.decoded.ExcusedSamples
	}
	if d.draw != nil {
		e.Drawn, e.Round, e.Randomness, e.Seed, e.Bound = true, d.round, d.randomness, d.draw.Seed, d.draw.Bound
	}
}

// buildDraw decodes an epoch's committed census from its input and, when it
// is canonical, draws it.
func buildDraw(in *BuildInput, input *EpochInput, poolID uint64, objects ledger.Objects) (*epochDraw, error) {
	d := &epochDraw{}
	c := input.Census
	if c == nil {
		return d, nil
	}
	hash := c.Hash
	d.census, d.censusBlock = &hash, c.Block
	census, invalid, err := readCensus(c.Hash, in.NoID, input.Epoch, func(ref ledger.Ref, limit int) ([]byte, bool, error) {
		data, ok := c.Objects[ref]
		switch {
		case !ok && ref == emptyRef:
			return []byte{}, false, nil
		case !ok || ledger.RefOf(data) != ref:
			return nil, false, fmt.Errorf("%w: epoch %d %v", ErrCensusUnavailable, input.Epoch, ref)
		case len(data) > limit:
			return nil, true, nil
		}
		objects[ref] = data
		return data, false, nil
	})
	if err != nil {
		return nil, err
	}
	if invalid != nil {
		d.censusErr = invalid
		return d, nil
	}
	d.decoded = census
	d.round = DrawRound(c.Time)
	if input.Pulse == nil || input.Pulse.Round != d.round {
		return nil, fmt.Errorf("%w: epoch %d needs the pulse of round %d", ErrInput, input.Epoch, d.round)
	}
	if err := input.Pulse.Verify(); err != nil {
		return nil, err
	}
	d.randomness = input.Pulse.Randomness()
	d.draw = DrawCensus(d.decoded.Candidates, Seed(in.ChainID, in.Netuid, poolID, input.Epoch, c.Hash, d.randomness), input.Bound)
	return d, nil
}

// encodeEpoch encodes an epoch's awards, pots and pending rows into objects
// and records their references.
func encodeEpoch(e *EpochResult, objects ledger.Objects) error {
	awards, err := EncodeAwards(e.Awards)
	if err != nil {
		return err
	}
	pots, err := EncodePots(e.Pots)
	if err != nil {
		return err
	}
	pending, err := EncodePending(e.Pending)
	if err != nil {
		return err
	}
	e.AwardsRef, e.PotsRef, e.PendingRef = ledger.RefOf(awards), ledger.RefOf(pots), ledger.RefOf(pending)
	objects[e.AwardsRef], objects[e.PotsRef], objects[e.PendingRef] = awards, pots, pending
	return nil
}

// ReadPrevious reads a finalized CTF root's accounts and state from its
// published objects, as Build needs them: the header named by
// root.LedgerHash, its account chunks, and its last epoch's pot and pending
// rows. Every object must match its reference, and the accounts must rebuild
// root; otherwise it refuses with ledger.ErrPrevious.
func ReadPrevious(ctx context.Context, root ledger.Root, fetcher ledger.Fetcher) (*Previous, error) {
	if ctx == nil || fetcher == nil {
		return nil, ErrInput
	}
	read := func(ref ledger.Ref) ([]byte, error) {
		if ref == emptyRef {
			return nil, nil
		}
		body, err := fetcher.Fetch(ctx, ref)
		if err != nil {
			return nil, err
		}
		defer body.Close()
		data, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		if ledger.RefOf(data) != ref {
			return nil, fmt.Errorf("%w: %v does not match its reference", ledger.ErrPrevious, ref)
		}
		return data, nil
	}
	data, err := read(root.LedgerHash)
	if err != nil {
		return nil, err
	}
	header, err := DecodeHeader(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ledger.ErrPrevious, err)
	}
	if len(header.Epochs) == 0 || header.Epoch != root.Epoch {
		return nil, fmt.Errorf("%w: header epoch %d", ledger.ErrPrevious, header.Epoch)
	}
	chunks := make([][]byte, len(header.Accounts))
	for j, ref := range header.Accounts {
		if chunks[j], err = read(ref); err != nil {
			return nil, err
		}
	}
	accounts, err := ledger.DecodeAccountChunks(chunks)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ledger.ErrPrevious, err)
	}
	tree, err := merkle.BuildLedgerSumTree(ctx, accounts)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ledger.ErrPrevious, err)
	}
	if r, total := tree.Root(); r != root.Root || !total.Eq(&root.Total) || tree.LeafCount() != root.LeafCount {
		return nil, ledger.ErrPrevious
	}
	last := &header.Epochs[len(header.Epochs)-1]
	pots, err := read(last.Pots)
	if err != nil {
		return nil, err
	}
	pending, err := read(last.Pending)
	if err != nil {
		return nil, err
	}
	state, err := decodeState(pots, pending)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ledger.ErrPrevious, err)
	}
	return &Previous{Root: root, Accounts: accounts, State: *state}, nil
}

// decodeState decodes a root's state from its last epoch's pot and pending
// rows.
func decodeState(potRows, pendingRows []byte) (*State, error) {
	pots, err := DecodePots(potRows)
	if err != nil {
		return nil, err
	}
	pending, err := DecodePending(pendingRows)
	if err != nil {
		return nil, err
	}
	s := &State{Pending: pending}
	for j := range pots {
		s.Pots[j] = pots[j].Out
	}
	return s, nil
}
