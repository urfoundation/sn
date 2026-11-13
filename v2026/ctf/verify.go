package ctf

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
)

// maxPendingBytes bounds a pending object: 2^22 pending awards, about 1,700
// epochs of every slot won while the carry is out.
const maxPendingBytes = 1 << 22 * PendingRowBytes

var (
	// ErrObjectHash is returned for a census, award, pot or pending object
	// whose bytes do not match its reference.
	ErrObjectHash = errors.New("ctf: object bytes do not match their reference")
	// ErrHeaderCarryColdkey is returned for a header whose carry coldkey is
	// not the carry contract's.
	ErrHeaderCarryColdkey = errors.New("ctf: header carry_coldkey is not the carry contract's")
	// ErrCensusReference is returned for an epoch whose header census is not
	// the census committed on chain: null while one was committed, or a
	// reference while none was or another hash.
	ErrCensusReference = errors.New("ctf: header census is not the committed census")
	// ErrRound is returned for an epoch whose header round is not ρ_k with a
	// draw, or not 0 without one.
	ErrRound = errors.New("ctf: header round is not the draw's")
	// ErrAwardWinner is returned for award rows that are not the draw's
	// winners: another count, role, country, rank, unit or member.
	ErrAwardWinner = errors.New("ctf: award rows are not the draw's winners")
	// ErrAwardColdkey is returned for an award row with a zero coldkey.
	ErrAwardColdkey = errors.New("ctf: an award row has a zero coldkey")
	// ErrAwardAmount is returned for an award row whose slot amount, pot share
	// or deferred flag is not the walk's.
	ErrAwardAmount = errors.New("ctf: an award's amounts are not the walk's")
	// ErrPots is returned for pot rows that are not the walk's.
	ErrPots = errors.New("ctf: pot rows are not the walk's")
	// ErrPending is returned for pending rows that are not the walk's.
	ErrPending = errors.New("ctf: pending awards are not the walk's")
)

// Check names a check of the CTF audit (§8).
type Check int

const (
	// CheckNone: every check passed.
	CheckNone Check = iota
	// CheckHeader: the header (its hash, form, pool, epoch, countries, carry
	// coldkey and covered epochs).
	CheckHeader
	// CheckPrevious: the latest finalized root's objects rebuild it and give
	// its state.
	CheckPrevious
	// CheckFixedInputs: an epoch's awards are not the ones custody holds as
	// valid.
	CheckFixedInputs
	// CheckChunks: the pending root's account chunks.
	CheckChunks
	// CheckCensus: §8 check 1, the census reference against the chain and
	// the census objects.
	CheckCensus
	// CheckSeed: §8 check 2, the round of the draw.
	CheckSeed
	// CheckDraw: §8 check 3, the winning units, ranks and members.
	CheckDraw
	// CheckAmounts: §8 check 4, slot amounts, pot shares, pots and pending
	// awards.
	CheckAmounts
	// CheckAccounts: §8 check 5, the positions and coldkeys of the accounts
	// (the ledger's check 8).
	CheckAccounts
	// CheckAllocation: §8 check 5, each account's increase is its coldkey's
	// delta, the carry account's included (the ledger's check 7).
	CheckAllocation
	// CheckTree: the account rows rebuild the committed root, total and leaf
	// count.
	CheckTree
	// CheckFetch: an object was unavailable, or the byte budget ran out.
	CheckFetch
)

func (c Check) String() string {
	switch c {
	case CheckNone:
		return "none"
	case CheckHeader:
		return "header"
	case CheckPrevious:
		return "previous root"
	case CheckFixedInputs:
		return "fixed inputs"
	case CheckChunks:
		return "chunks"
	case CheckCensus:
		return "census"
	case CheckSeed:
		return "seed"
	case CheckDraw:
		return "draw"
	case CheckAmounts:
		return "amounts"
	case CheckAccounts:
		return "accounts"
	case CheckAllocation:
		return "allocation"
	case CheckTree:
		return "tree"
	case CheckFetch:
		return "fetch"
	default:
		return fmt.Sprintf("check(%d)", int(c))
	}
}

// CensusCommitment is the chain's record of an epoch's census:
// ctfCensus(n, k) and the timestamp of its block.
type CensusCommitment struct {
	Hash  ledger.Ref
	Block uint64
	// Time is block B_k's EVM timestamp, in unix seconds.
	Time uint64
}

// EpochFacts are what the chain says of a covered epoch.
type EpochFacts struct {
	// Census is the committed census, nil when ctfCensus(n, k) is zero.
	Census *CensusCommitment
	// Pulse is the quicknet pulse of DrawRound(Census.Time), from the chain's
	// Drand pallet or any relay. It is needed when the committed census is
	// canonical.
	Pulse *Pulse
	// Recycled is the carry contract's recycledFor[k].
	Recycled uint256.Int
}

// VerifyInput is what an auditor read from chain at one pinned block, and
// its sources.
type VerifyInput struct {
	// NoID is the operator n, below 2^32; the pool is PoolID(NoID).
	NoID uint64
	// ChainID and Netuid seed the draw: 964 and 25 on mainnet.
	ChainID uint64
	Netuid  uint16
	// FirstEpoch is the CTF pool's first epoch, from ctfSettlementAt.
	FirstEpoch uint64
	// CarryColdkey is the carry contract's coldkey.
	CarryColdkey [32]byte
	// Pending is the pool's pending root, and Previous its latest finalized
	// root, nil before the first.
	Pending  ledger.Root
	Previous *ledger.Root
	// FundedThrough holds fundedThrough[k] for every covered epoch k.
	FundedThrough map[uint64]uint256.Int
	// Epochs holds the chain's facts of every covered epoch.
	Epochs map[uint64]EpochFacts
	// Fetcher reads published objects, and Custody holds what audits kept:
	// objects, and per epoch of the pool PoolID(NoID) the awards object found
	// valid (ValidRows), which later roots must list again (§5 fixed inputs).
	Fetcher ledger.Fetcher
	Custody ledger.Custody
	// Heads gives each epoch's fleet bindings at either boundary. The draw
	// leaves out bound candidates; nil leaves out none, for tools without
	// archive state, and then a census that lists a bound client does not
	// reproduce.
	Heads ledger.HeadOracle
	// MaxFetchBytes bounds the bytes fetched for objects not in custody.
	MaxFetchBytes uint64
}

// Result is an audit's outcome, as ledger.Result is for a provider pool.
type Result struct {
	// Check is the first check that failed and Err its error; CheckNone and
	// nil when the root passed every check.
	Check Check
	Err   error
	// Missing lists objects that could not be obtained: unavailable, or
	// served with bytes that do not match their reference.
	Missing []ledger.Ref
	// Previous is the latest finalized root's tree, PreviousAccounts its
	// accounts and PreviousState its state, once check 2 passed.
	Previous         *merkle.LedgerSumTree
	PreviousAccounts []merkle.LedgerAccount
	PreviousState    *State
	// Accounts are the pending root's published accounts, once read, and Tree
	// their tree. Reproduced reports that Tree is the pending root: root,
	// total and leaf count.
	Accounts   []merkle.LedgerAccount
	Tree       *merkle.LedgerSumTree
	Reproduced bool
	// Faults lists, by index, every previous account whose published row is
	// below its value, names another coldkey, or is missing.
	Faults []ledger.Fault
	// FetchedBytes counts the bytes fetched for objects not in custody.
	FetchedBytes uint64
	// Epochs reports each covered epoch the audit recomputed, in order.
	Epochs []*EpochResult
}

// Action returns the response VERSION2-FINAL.md §9.3 prescribes, as for a
// provider pool's root: approve a root that passed; challenge a fault when
// the published rows reproduce the root; demand when objects are missing or
// the rows do not reproduce the root and there is a previous root to
// protect; refuse otherwise.
func (r *Result) Action() ledger.Action {
	switch {
	case r.Err == nil:
		return ledger.ActionApprove
	case r.Reproduced && len(r.Faults) > 0:
		return ledger.ActionChallenge
	case r.Previous != nil && (len(r.Missing) > 0 || !r.Reproduced):
		return ledger.ActionDemand
	default:
		return ledger.ActionRefuse
	}
}

// failure is a check the pending root failed, as opposed to an error of the
// verifier's own inputs or context.
type failure struct {
	check Check
	err   error
}

func (f *failure) Error() string { return f.err.Error() }

func fail(check Check, err error) error { return &failure{check: check, err: err} }

// role says how an object's faults are reported.
type role struct {
	check Check
	// within, when set, wraps every fault of the role.
	within   error
	mismatch error
	oversize error
	limit    int
}

func (r *role) fail(err error) error {
	if r.within != nil {
		err = fmt.Errorf("%w: %w", r.within, err)
	}
	return fail(r.check, err)
}

var (
	headerRole         = role{check: CheckHeader, mismatch: ledger.ErrLedgerHash, oversize: ledger.ErrHeaderTooLarge, limit: ledger.MaxHeaderBytes}
	previousHeaderRole = role{check: CheckPrevious, within: ledger.ErrPreviousRebuild, mismatch: ledger.ErrLedgerHash, oversize: ledger.ErrHeaderTooLarge, limit: ledger.MaxHeaderBytes}
	previousChunkRole  = role{check: CheckPrevious, within: ledger.ErrPreviousRebuild, mismatch: ledger.ErrChunkHash, oversize: ledger.ErrChunkLength, limit: ledger.ChunkRows * ledger.AccountRowBytes}
	previousPotsRole   = role{check: CheckPrevious, within: ledger.ErrPreviousRebuild, mismatch: ErrObjectHash, oversize: ErrPotRows, limit: NumPots * PotRowBytes}
	previousPendRole   = role{check: CheckPrevious, within: ledger.ErrPreviousRebuild, mismatch: ErrObjectHash, oversize: ErrPendingRows, limit: maxPendingBytes}
	accountChunkRole   = role{check: CheckChunks, mismatch: ledger.ErrChunkHash, oversize: ledger.ErrChunkLength, limit: ledger.ChunkRows * ledger.AccountRowBytes}
	awardsRole         = role{check: CheckDraw, mismatch: ErrObjectHash, oversize: ErrAwardRows, limit: NumSlots * AwardRowBytes}
	potsRole           = role{check: CheckAmounts, mismatch: ErrObjectHash, oversize: ErrPotRows, limit: NumPots * PotRowBytes}
	pendingRole        = role{check: CheckAmounts, mismatch: ErrObjectHash, oversize: ErrPendingRows, limit: maxPendingBytes}
)

type verifier struct {
	ctx context.Context
	in  *VerifyInput
	res *Result
	// previousLast is the latest finalized root's last covered epoch, whose
	// pot and pending rows are the pool's state.
	previousLast *HeaderEpoch
}

// Verify audits the pending CTF root (§8). It returns an error only for its
// own inputs (ErrInput, ledger.ErrFunding, ErrPulse for a pulse that does not
// verify), its context, its custody or its head oracle; the audit's outcome
// is in the Result. The checks run in this order, stopping at the first that
// fails: the previous root's accounts, the header, the pending root's account
// chunks and the faults they show, fixed inputs, accounts, tree, the previous
// root's state; then per covered epoch its census, seed, draw and amounts;
// and finally each account's delta. The previous root comes first and the
// pending accounts early so that a failed root still has accounts to protect
// and its faults known, whatever else is withheld. Every object the header
// names is fetched and kept in custody, and an epoch's awards that pass are
// recorded as valid.
func Verify(ctx context.Context, in VerifyInput) (*Result, error) {
	if ctx == nil || in.Fetcher == nil || in.Custody == nil {
		return nil, ErrInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := checkPool(in.NoID, in.CarryColdkey, in.FirstEpoch, in.Previous != nil); err != nil {
		return nil, err
	}
	v := &verifier{ctx: ctx, in: &in, res: &Result{}}
	err := v.run()
	if f, ok := err.(*failure); ok {
		v.res.Check, v.res.Err = f.check, f.err
		return v.res, nil
	}
	if err != nil {
		return nil, err
	}
	return v.res, nil
}

func (v *verifier) run() error {
	p := v.in.FirstEpoch - 1
	if v.in.Previous != nil {
		p = v.in.Previous.Epoch
	}
	if err := v.previous(); err != nil {
		return err
	}
	header, err := v.header(p)
	if err != nil {
		return err
	}
	if err := v.accounts(header); err != nil {
		return err
	}
	if err := v.fixedInputs(header); err != nil {
		return err
	}
	if err := ledger.CheckAccountRows(v.res.PreviousAccounts, v.res.Accounts); err != nil {
		return fail(CheckAccounts, err)
	}
	if err := v.checkTree(); err != nil {
		return err
	}
	if err := v.previousState(); err != nil {
		return err
	}
	deltas, err := v.epochs(header, p)
	if err != nil {
		return err
	}
	if err := ledger.CheckAccountDeltas(v.res.PreviousAccounts, v.res.Accounts, deltas); err != nil {
		return fail(CheckAllocation, err)
	}
	return nil
}

// get returns an object from custody, or fetches, checks and keeps it. The
// empty object needs no fetch.
func (v *verifier) get(ref ledger.Ref, r *role) ([]byte, error) {
	if ref == emptyRef {
		return []byte{}, nil
	}
	data, ok, err := v.in.Custody.Object(v.ctx, ref)
	if err != nil {
		return nil, err
	}
	if ok {
		return data, nil
	}
	limit, budget := uint64(r.limit), false
	if remaining := v.in.MaxFetchBytes - min(v.res.FetchedBytes, v.in.MaxFetchBytes); remaining < limit {
		limit, budget = remaining, true
	}
	body, err := v.in.Fetcher.Fetch(v.ctx, ref)
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(body, int64(limit)+1))
		body.Close()
		v.res.FetchedBytes += uint64(len(data))
	}
	if err != nil {
		if ctxErr := v.ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		v.res.Missing = append(v.res.Missing, ref)
		return nil, fail(CheckFetch, fmt.Errorf("%w: %v: %w", ledger.ErrObjectUnavailable, ref, err))
	}
	if uint64(len(data)) > limit {
		if budget {
			return nil, fail(CheckFetch, fmt.Errorf("%w: %d bytes at %v", ledger.ErrByteBudget, v.in.MaxFetchBytes, ref))
		}
		return nil, r.fail(fmt.Errorf("%w: %v is too long", r.oversize, ref))
	}
	if ledger.RefOf(data) != ref {
		v.res.Missing = append(v.res.Missing, ref)
		return nil, r.fail(fmt.Errorf("%w: %v", r.mismatch, ref))
	}
	if err := v.in.Custody.Keep(v.ctx, ref, data); err != nil {
		return nil, err
	}
	return data, nil
}

// loadAccounts reads an account chunk list.
func (v *verifier) loadAccounts(refs []ledger.Ref, r *role) ([]merkle.LedgerAccount, error) {
	chunks := make([][]byte, len(refs))
	for j, ref := range refs {
		var err error
		if chunks[j], err = v.get(ref, r); err != nil {
			return nil, err
		}
	}
	accounts, err := ledger.DecodeAccountChunks(chunks)
	if err != nil {
		return nil, r.fail(err)
	}
	return accounts, nil
}

// previous checks that the latest finalized root's objects rebuild it. Its
// state is read later, by previousState.
func (v *verifier) previous() error {
	previous := v.in.Previous
	if previous == nil {
		v.res.PreviousState = &State{}
		return nil
	}
	data, err := v.get(previous.LedgerHash, &previousHeaderRole)
	if err != nil {
		return err
	}
	header, err := DecodeHeader(data)
	if err != nil {
		return previousHeaderRole.fail(err)
	}
	if header.NoID != v.in.NoID {
		return previousHeaderRole.fail(fmt.Errorf("%w: %d, pool of operator %d", ledger.ErrHeaderNoID, header.NoID, v.in.NoID))
	}
	if header.Epoch != previous.Epoch || len(header.Epochs) == 0 {
		return previousHeaderRole.fail(fmt.Errorf("%w: %d, finalized %d", ledger.ErrHeaderEpoch, header.Epoch, previous.Epoch))
	}
	accounts, err := v.loadAccounts(header.Accounts, &previousChunkRole)
	if err != nil {
		return err
	}
	var sum uint256.Int
	for i := range accounts {
		sum.Add(&sum, &accounts[i].Cumulative)
	}
	if !sum.Eq(&previous.Total) {
		return fail(CheckPrevious, fmt.Errorf("%w: %s, total %s", ledger.ErrPreviousNotTight, sum.Dec(), previous.Total.Dec()))
	}
	if uint64(len(accounts)) != previous.LeafCount {
		return fail(CheckPrevious, fmt.Errorf("%w: %d accounts, leaf count %d", ledger.ErrPreviousRebuild, len(accounts), previous.LeafCount))
	}
	tree, err := merkle.BuildLedgerSumTree(v.ctx, accounts)
	if err != nil {
		return err
	}
	if root, _ := tree.Root(); root != previous.Root {
		return fail(CheckPrevious, fmt.Errorf("%w: root", ledger.ErrPreviousRebuild))
	}
	v.res.Previous, v.res.PreviousAccounts = tree, accounts
	v.previousLast = &header.Epochs[len(header.Epochs)-1]
	return nil
}

// previousState reads the latest finalized root's state from its last
// epoch's pot and pending rows. It runs once the pending root's accounts are
// known, so that a withheld state object, like any withheld object, leads to
// a demand that protects the previous accounts.
func (v *verifier) previousState() error {
	if v.res.PreviousState != nil {
		return nil
	}
	pots, err := v.get(v.previousLast.Pots, &previousPotsRole)
	if err != nil {
		return err
	}
	pending, err := v.get(v.previousLast.Pending, &previousPendRole)
	if err != nil {
		return err
	}
	state, err := decodeState(pots, pending)
	if err != nil {
		return previousPotsRole.fail(err)
	}
	v.res.PreviousState = state
	return nil
}

// censusObject reads a census object as get does, except for an object
// longer than limit: no census Build would draw holds one, so it is read on
// through its hash, within the byte budget, and reported oversize when it
// matches its reference, making the census not canonical for the auditor
// exactly as for Build. An object served with other bytes is missing, as
// with get, so serving other bytes never cancels a draw.
func (v *verifier) censusObject(ref ledger.Ref, limit int) ([]byte, bool, error) {
	if ref == emptyRef {
		return []byte{}, false, nil
	}
	data, ok, err := v.in.Custody.Object(v.ctx, ref)
	if err != nil || ok {
		return data, false, err
	}
	remaining := v.in.MaxFetchBytes - min(v.res.FetchedBytes, v.in.MaxFetchBytes)
	keep := uint64(limit) + 1
	if remaining < uint64(limit) {
		keep = remaining + 1
	}
	hash := sha256.New()
	var n uint64
	body, err := v.in.Fetcher.Fetch(v.ctx, ref)
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(body, int64(keep)))
		n = uint64(len(data))
		hash.Write(data)
		if err == nil && n > uint64(limit) && n <= remaining {
			// Hash the rest, up to one byte past the budget.
			var rest int64
			rest, err = io.Copy(hash, io.LimitReader(body, int64(min(remaining-n, math.MaxInt64-1))+1))
			n += uint64(rest)
		}
		body.Close()
		v.res.FetchedBytes += n
	}
	if err != nil {
		if ctxErr := v.ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		v.res.Missing = append(v.res.Missing, ref)
		return nil, false, fail(CheckFetch, fmt.Errorf("%w: %v: %w", ledger.ErrObjectUnavailable, ref, err))
	}
	if n > remaining {
		return nil, false, fail(CheckFetch, fmt.Errorf("%w: %d bytes at %v", ledger.ErrByteBudget, v.in.MaxFetchBytes, ref))
	}
	if ledger.Ref(hash.Sum(nil)) != ref {
		v.res.Missing = append(v.res.Missing, ref)
		return nil, false, fail(CheckCensus, fmt.Errorf("%w: %v", ErrObjectHash, ref))
	}
	if n > uint64(limit) {
		return nil, true, nil
	}
	if err := v.in.Custody.Keep(v.ctx, ref, data); err != nil {
		return nil, false, err
	}
	return data, false, nil
}

// header checks the pending root's header.
func (v *verifier) header(p uint64) (*Header, error) {
	pending := &v.in.Pending
	data, err := v.get(pending.LedgerHash, &headerRole)
	if err != nil {
		return nil, err
	}
	h, err := DecodeHeader(data)
	if err != nil {
		return nil, fail(CheckHeader, err)
	}
	if h.NoID != v.in.NoID {
		return nil, fail(CheckHeader, fmt.Errorf("%w: %d, pool of operator %d", ledger.ErrHeaderNoID, h.NoID, v.in.NoID))
	}
	if h.Epoch != pending.Epoch {
		return nil, fail(CheckHeader, fmt.Errorf("%w: %d, pending %d", ledger.ErrHeaderEpoch, h.Epoch, pending.Epoch))
	}
	if h.CarryColdkey != v.in.CarryColdkey {
		return nil, fail(CheckHeader, fmt.Errorf("%w: %x", ErrHeaderCarryColdkey, h.CarryColdkey))
	}
	if err := checkCovered(p, pending.Epoch); err != nil {
		return nil, fail(CheckHeader, err)
	}
	if uint64(len(h.Epochs)) != pending.Epoch-p {
		return nil, fail(CheckHeader, fmt.Errorf("%w: %d epochs listed for %d to %d", ledger.ErrCoveredEpochs, len(h.Epochs), p+1, pending.Epoch))
	}
	for i := range h.Epochs {
		if h.Epochs[i].Epoch != p+1+uint64(i) {
			return nil, fail(CheckHeader, fmt.Errorf("%w: entry %d is epoch %d", ledger.ErrCoveredEpochs, i, h.Epochs[i].Epoch))
		}
	}
	return h, nil
}

// accounts reads the pending root's account chunks, lists the previous
// accounts they fault, and rebuilds their tree.
func (v *verifier) accounts(h *Header) error {
	accounts, err := v.loadAccounts(h.Accounts, &accountChunkRole)
	if err != nil {
		return err
	}
	v.res.Accounts = accounts
	v.res.Faults = ledger.Faults(v.res.PreviousAccounts, accounts)
	if n := uint64(len(accounts)); n == 0 || n > merkle.MaxLedgerLeafCount {
		return nil
	}
	tree, err := merkle.BuildLedgerSumTree(v.ctx, accounts)
	if err != nil {
		return err
	}
	root, total := tree.Root()
	pending := &v.in.Pending
	v.res.Tree = tree
	v.res.Reproduced = root == pending.Root && total.Eq(&pending.Total) && tree.LeafCount() == pending.LeafCount
	return nil
}

// fixedInputs checks that every covered epoch whose awards custody holds as
// valid lists them again.
func (v *verifier) fixedInputs(h *Header) error {
	poolID := PoolID(v.in.NoID)
	for i := range h.Epochs {
		epoch := &h.Epochs[i]
		held, ok, err := v.in.Custody.ValidRows(v.ctx, poolID, epoch.Epoch)
		if err != nil {
			return err
		}
		if ok && (len(held) != 1 || held[0] != epoch.Awards) {
			return fail(CheckFixedInputs, fmt.Errorf("%w: epoch %d awards", ledger.ErrFixedInputs, epoch.Epoch))
		}
	}
	return nil
}

// checkTree checks that the account rows rebuild the committed root.
func (v *verifier) checkTree() error {
	if v.res.Reproduced {
		return nil
	}
	pending := &v.in.Pending
	if v.res.Tree == nil || v.res.Tree.LeafCount() != pending.LeafCount {
		return fail(CheckTree, fmt.Errorf("%w: %d rows, leaf count %d", ledger.ErrTreeLeafCount, len(v.res.Accounts), pending.LeafCount))
	}
	if _, total := v.res.Tree.Root(); !total.Eq(&pending.Total) {
		return fail(CheckTree, fmt.Errorf("%w: %s, committed %s", ledger.ErrTreeTotal, total.Dec(), pending.Total.Dec()))
	}
	return fail(CheckTree, ledger.ErrTreeRoot)
}

// epochs recomputes every covered epoch from its census, the chain's facts
// and its award rows' coldkeys, compares the header's objects, and returns
// every coldkey's delta.
func (v *verifier) epochs(h *Header, p uint64) ([]protocol.LedgerDelta, error) {
	var base uint256.Int
	if v.in.Previous != nil {
		base = v.in.Previous.Total
	}
	funding, _, err := fundings(v.in.FundedThrough, p, v.in.Pending.Epoch, &base)
	if err != nil {
		return nil, err
	}
	w := newWalk(v.res.PreviousState)
	for i := range h.Epochs {
		if err := v.ctx.Err(); err != nil {
			return nil, err
		}
		entry := &h.Epochs[i]
		facts, ok := v.in.Epochs[entry.Epoch]
		if !ok {
			return nil, fmt.Errorf("%w: no chain facts for epoch %d", ErrInput, entry.Epoch)
		}
		drawn, err := v.draw(entry, &facts)
		if err != nil {
			return nil, err
		}
		awards, err := v.awards(entry, drawn.draw)
		if err != nil {
			return nil, err
		}
		// The award rows are the winners (check 3), so each winner's coldkey
		// is its slot's row's.
		var coldkeys [NumSlots][32]byte
		for j := range awards {
			a := &awards[j]
			coldkeys[SlotIndex(a.Country.Index(), a.Role, a.Rank)] = a.Coldkey
		}
		e, err := w.step(entry.Epoch, &funding[i], &facts.Recycled, drawn.draw, func(winner *Winner) [32]byte {
			return coldkeys[SlotIndex(winner.Country.Index(), winner.Role, winner.Rank)]
		})
		if err != nil {
			return nil, err
		}
		drawn.report(e)
		v.res.Epochs = append(v.res.Epochs, e)
		if err := v.amounts(entry, e, awards); err != nil {
			return nil, err
		}
	}
	deltas, err := w.finish(v.in.CarryColdkey)
	if errors.Is(err, ErrReturnRolls) {
		return nil, fail(CheckAmounts, err)
	}
	if err != nil {
		return nil, err
	}
	return deltas, nil
}

// draw runs §8 checks 1 and 2 for a covered epoch and, for a canonical
// committed census, its draw.
func (v *verifier) draw(entry *HeaderEpoch, facts *EpochFacts) (*epochDraw, error) {
	d := &epochDraw{}
	c := facts.Census
	switch {
	case c == nil && entry.Census != nil:
		return nil, fail(CheckCensus, fmt.Errorf("%w: epoch %d lists %v, and none was committed", ErrCensusReference, entry.Epoch, *entry.Census))
	case c == nil:
		if entry.Round != 0 {
			return nil, fail(CheckSeed, fmt.Errorf("%w: epoch %d without a draw lists round %d", ErrRound, entry.Epoch, entry.Round))
		}
		return d, nil
	case entry.Census == nil || *entry.Census != c.Hash:
		return nil, fail(CheckCensus, fmt.Errorf("%w: epoch %d, committed %v", ErrCensusReference, entry.Epoch, c.Hash))
	}
	hash := c.Hash
	d.census, d.censusBlock = &hash, c.Block
	// The census decides the draw only from bytes that match their
	// references, read as Build reads them.
	census, invalid, err := readCensus(c.Hash, v.in.NoID, entry.Epoch, v.censusObject)
	if err != nil {
		return nil, err
	}
	if invalid != nil {
		d.censusErr = invalid
		if entry.Round != 0 {
			return nil, fail(CheckSeed, fmt.Errorf("%w: epoch %d lists round %d, and its census is not canonical: %v", ErrRound, entry.Epoch, entry.Round, invalid))
		}
		return d, nil
	}
	d.decoded = census
	d.round = DrawRound(c.Time)
	if entry.Round != d.round {
		return nil, fail(CheckSeed, fmt.Errorf("%w: epoch %d lists round %d, not %d", ErrRound, entry.Epoch, entry.Round, d.round))
	}
	if facts.Pulse == nil || facts.Pulse.Round != d.round {
		return nil, fmt.Errorf("%w: epoch %d needs the pulse of round %d", ErrInput, entry.Epoch, d.round)
	}
	if err := facts.Pulse.Verify(); err != nil {
		return nil, err
	}
	d.randomness = facts.Pulse.Randomness()
	var bound map[[16]byte]bool
	if v.in.Heads != nil {
		if bound, err = v.in.Heads.HeadBindings(v.ctx, entry.Epoch); err != nil {
			return nil, err
		}
	}
	d.draw = DrawCensus(d.decoded.Candidates, Seed(v.in.ChainID, v.in.Netuid, PoolID(v.in.NoID), entry.Epoch, c.Hash, d.randomness), bound)
	return d, nil
}

// awards runs §8 check 3: the epoch's award rows are its draw's winners, in
// role, country and rank order, each with a coldkey.
func (v *verifier) awards(entry *HeaderEpoch, draw *Draw) ([]Award, error) {
	data, err := v.get(entry.Awards, &awardsRole)
	if err != nil {
		return nil, err
	}
	awards, err := DecodeAwards(data)
	if err != nil {
		return nil, fail(CheckDraw, fmt.Errorf("epoch %d: %w", entry.Epoch, err))
	}
	i := 0
	for role := RoleProvider; role <= RoleExtender; role++ {
		for c := range NumCountries {
			if draw == nil {
				break
			}
			for _, winner := range draw.Winners[PotIndex(c, role)] {
				if i >= len(awards) {
					return nil, fail(CheckDraw, fmt.Errorf("%w: epoch %d has %d award rows, fewer than its winners", ErrAwardWinner, entry.Epoch, len(awards)))
				}
				a := &awards[i]
				if a.Role != winner.Role || a.Country != winner.Country || a.Rank != winner.Rank || a.Unit != winner.Unit || a.ClientID != winner.ClientID {
					return nil, fail(CheckDraw, fmt.Errorf("%w: epoch %d row %d", ErrAwardWinner, entry.Epoch, i))
				}
				if a.Coldkey == ([32]byte{}) {
					return nil, fail(CheckDraw, fmt.Errorf("%w: epoch %d row %d", ErrAwardColdkey, entry.Epoch, i))
				}
				i++
			}
		}
	}
	if i != len(awards) {
		return nil, fail(CheckDraw, fmt.Errorf("%w: epoch %d has %d award rows for %d winners", ErrAwardWinner, entry.Epoch, len(awards), i))
	}
	return awards, nil
}

// amounts runs §8 check 4: the award rows' amounts are the walk's, and the
// header names the walk's pot and pending rows, which are published. An
// epoch that passes records its awards as valid.
func (v *verifier) amounts(entry *HeaderEpoch, e *EpochResult, published []Award) error {
	for i := range e.Awards {
		a, b := &e.Awards[i], &published[i]
		if !a.Slot.Eq(&b.Slot) || !a.Pot.Eq(&b.Pot) || a.Deferred != b.Deferred {
			return fail(CheckAmounts, fmt.Errorf("%w: epoch %d row %d has slot %s pot %s deferred %t, the walk slot %s pot %s deferred %t",
				ErrAwardAmount, entry.Epoch, i, b.Slot.Dec(), b.Pot.Dec(), b.Deferred, a.Slot.Dec(), a.Pot.Dec(), a.Deferred))
		}
	}
	objects := ledger.Objects{}
	if err := encodeEpoch(e, objects); err != nil {
		return err
	}
	if e.AwardsRef != entry.Awards {
		// The rows match field by field, so this needs another encoding.
		return fail(CheckAmounts, fmt.Errorf("%w: epoch %d awards %v, recomputed %v", ErrAwardAmount, entry.Epoch, entry.Awards, e.AwardsRef))
	}
	if e.PotsRef != entry.Pots {
		return fail(CheckAmounts, fmt.Errorf("%w: epoch %d lists %v, recomputed %v", ErrPots, entry.Epoch, entry.Pots, e.PotsRef))
	}
	if e.PendingRef != entry.Pending {
		return fail(CheckAmounts, fmt.Errorf("%w: epoch %d lists %v, recomputed %v", ErrPending, entry.Epoch, entry.Pending, e.PendingRef))
	}
	// The next root's audit starts from these rows, so they must be
	// published: matching the recomputed references, they are its bytes.
	if _, err := v.get(entry.Pots, &potsRole); err != nil {
		return err
	}
	if _, err := v.get(entry.Pending, &pendingRole); err != nil {
		return err
	}
	poolID := PoolID(v.in.NoID)
	if _, ok, err := v.in.Custody.ValidRows(v.ctx, poolID, entry.Epoch); err != nil {
		return err
	} else if !ok {
		if err := v.in.Custody.KeepValidRows(v.ctx, poolID, entry.Epoch, []ledger.Ref{e.AwardsRef}); err != nil {
			return err
		}
	}
	return nil
}
