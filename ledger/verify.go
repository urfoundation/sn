package ledger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/protocol"
)

// DefaultMaxFetchBytes is the default budget of object bytes new to custody
// that one audit may fetch (the validator's --ledger_max_bytes).
const DefaultMaxFetchBytes = 2 << 30

var (
	// ErrInput is returned for a nil context, and by Verify without a
	// Fetcher or a Custody.
	ErrInput = errors.New("ledger: a context is required, and to verify a fetcher and a custody")
	// ErrObjectNotFound is the error a Fetcher may return for an absent object.
	ErrObjectNotFound = errors.New("ledger: object not found")

	// ErrObjectUnavailable: an object could not be fetched.
	ErrObjectUnavailable = errors.New("ledger: object unavailable")
	// ErrByteBudget: the objects new to custody exceed the byte budget.
	ErrByteBudget = errors.New("ledger: objects exceed the byte budget")

	// Check 1, header (with the codec's header errors and ErrCoveredEpochs
	// and ErrTooManyEpochs).

	// ErrLedgerHash: the header's sha256 is not the committed ledgerHash.
	ErrLedgerHash = errors.New("ledger: header sha256 is not the committed ledger hash")
	// ErrHeaderNoID: the header's no_id is not the pool.
	ErrHeaderNoID = errors.New("ledger: header no_id is not the pool")
	// ErrHeaderEpoch: the header's epoch is not the pending root's.
	ErrHeaderEpoch = errors.New("ledger: header epoch is not the pending root's")

	// Check 2, previous root.

	// ErrPreviousNotTight: the previous root's accounts do not sum to its total.
	ErrPreviousNotTight = errors.New("ledger: previous root's accounts do not sum to its total")
	// ErrPreviousRebuild: the previous root's objects do not rebuild it.
	ErrPreviousRebuild = errors.New("ledger: previous root's objects do not rebuild it")

	// Check 3, fixed inputs (with ErrFixedInputs: the header lists other
	// chunks for an epoch whose valid rows custody holds).

	// ErrNullAgainstValidRows: providers null for an epoch whose valid rows
	// custody holds.
	ErrNullAgainstValidRows = errors.New("ledger: providers null for an epoch with valid rows")

	// Check 4, chunks (with ErrChunkLength, ErrChunkShort, ErrChunkEmpty and
	// ErrRowEncoding). Check 5, rows (with ErrRowZeroClientID,
	// ErrRowZeroUsage, ErrRowsUnsorted, ErrRowDuplicate and ErrUsageOverflow).

	// ErrChunkHash: a chunk's bytes do not match its reference.
	ErrChunkHash = errors.New("ledger: chunk bytes do not match their reference")

	// Check 6, head exclusion.

	// ErrHeadFlagSet: head_excluded is 1 without an active binding.
	ErrHeadFlagSet = errors.New("ledger: head_excluded set without an active binding")
	// ErrHeadFlagCleared: head_excluded is 0 with an active binding.
	ErrHeadFlagCleared = errors.New("ledger: head_excluded cleared with an active binding")

	// Check 7, allocation (with ErrEpochNotAllocated).

	// ErrAccountOmitted: a coldkey with a positive delta has no account.
	ErrAccountOmitted = errors.New("ledger: a coldkey with a positive delta has no account")
	// ErrAccountDelta: an account's increase is not its coldkey's delta.
	ErrAccountDelta = errors.New("ledger: an account's increase is not its coldkey's delta")

	// Check 8, accounts.

	// ErrAccountMoved: a previous position does not keep its coldkey, or is missing.
	ErrAccountMoved = errors.New("ledger: a previous position does not keep its coldkey")
	// ErrAccountLowered: a previous account's cumulative decreased.
	ErrAccountLowered = errors.New("ledger: a previous account's cumulative decreased")
	// ErrAccountDuplicate: a coldkey has two accounts.
	ErrAccountDuplicate = errors.New("ledger: a coldkey has two accounts")
	// ErrAccountUnsorted: new accounts are not in ascending coldkey order.
	ErrAccountUnsorted = errors.New("ledger: new accounts are not in ascending coldkey order")
	// ErrAccountZeroDelta: a new account has a zero cumulative.
	ErrAccountZeroDelta = errors.New("ledger: a new account has a zero delta")

	// Check 9, tree.

	// ErrTreeLeafCount: the account rows are not the committed leaf count.
	ErrTreeLeafCount = errors.New("ledger: account rows are not the committed leaf count")
	// ErrTreeTotal: the account rows do not sum to the committed total.
	ErrTreeTotal = errors.New("ledger: account rows do not sum to the committed total")
	// ErrTreeRoot: the account rows do not rebuild the committed root.
	ErrTreeRoot = errors.New("ledger: account rows do not rebuild the committed root")
)

// Check names a §9.2 check.
type Check int

const (
	// CheckNone: every check passed.
	CheckNone Check = iota
	CheckHeader
	CheckPrevious
	CheckFixedInputs
	CheckChunks
	CheckRows
	CheckHeadExclusion
	CheckAllocation
	CheckAccounts
	CheckTree
	// CheckFetch: an object was unavailable, or the byte budget ran out.
	CheckFetch
	// CheckRecovery: the header's recovery components are not the ones the
	// chain calls for (§16 step 6).
	CheckRecovery
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
	case CheckRows:
		return "provider rows"
	case CheckHeadExclusion:
		return "head exclusion"
	case CheckAllocation:
		return "allocation"
	case CheckAccounts:
		return "accounts"
	case CheckTree:
		return "tree"
	case CheckFetch:
		return "fetch"
	case CheckRecovery:
		return "recovery"
	default:
		return fmt.Sprintf("check(%d)", int(c))
	}
}

// Fetcher reads published objects, such as GET /sn/ledger/object.
type Fetcher interface {
	// Fetch opens the object named by ref. Any error means the object is
	// unavailable now.
	Fetch(ctx context.Context, ref Ref) (io.ReadCloser, error)
}

// Fetch serves the objects of o.
func (o Objects) Fetch(ctx context.Context, ref Ref) (io.ReadCloser, error) {
	data, ok := o[ref]
	if !ok {
		return nil, fmt.Errorf("%w: %v", ErrObjectNotFound, ref)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// Custody is the validator's durable store of audited objects and findings.
// Its errors are returned by Verify as they are.
type Custody interface {
	// Object returns a held object, with ok false when it is not held.
	Object(ctx context.Context, ref Ref) (data []byte, ok bool, err error)
	// Keep stores an object whose bytes match ref.
	Keep(ctx context.Context, ref Ref, data []byte) error
	// ValidRows returns the chunks of the epoch's rows that an audit found
	// valid, with ok false when there are none.
	ValidRows(ctx context.Context, noID, epoch uint64) (chunks []Ref, ok bool, err error)
	// KeepValidRows records the epoch's rows, listed by chunks, as valid.
	KeepValidRows(ctx context.Context, noID, epoch uint64, chunks []Ref) error
	// KeepNull records that the pending root seq declared the epoch's inputs
	// unrecoverable.
	KeepNull(ctx context.Context, noID, epoch, seq uint64) error
}

// HeadOracle reads fleet bindings for check 6. Its errors are returned by
// Verify as they are.
type HeadOracle interface {
	// HeadBindings returns the epoch's candidates, the client ids with a
	// FleetBound event whose validity covers the epoch, each mapped to
	// whether bindingAt reports an active binding at the epoch's start or end
	// boundary.
	HeadBindings(ctx context.Context, epoch uint64) (map[[16]byte]bool, error)
}

// VerifyInput is what a validator read from chain at a pinned block, and
// its sources.
type VerifyInput struct {
	NoID       uint64
	FirstEpoch uint64
	// Pending is the pending root, with Seq its commit sequence number.
	Pending Root
	Seq     uint64
	// Previous is the latest finalized root, nil before the pool's first.
	Previous *Root
	// FundedThrough holds fundedThrough[k] for every covered epoch k.
	FundedThrough map[uint64]uint256.Int
	Fetcher       Fetcher
	Custody       Custody
	// Heads decides check 6. Nil skips check 6, for tools without archive
	// state; a validator always sets it.
	Heads HeadOracle
	// MaxFetchBytes bounds the bytes fetched for objects not in custody.
	// The budget is per root: a retry passes what earlier attempts left.
	MaxFetchBytes uint64
	// Recoveries holds, per covered epoch, the recovery components the chain
	// calls for, from PlanRecovery over vault2's SurplusAssigned events, with
	// each amount checked against vault1 (RecoverySchedule.CheckSource). The
	// header must list exactly these. Nil calls for none.
	Recoveries map[uint64][]Recovery
}

// Fault is a previous account that the pending root's published account
// rows lower (LedgerFaultLowerValue) or give another coldkey, or omit
// (LedgerFaultWrongLeaf). When the rows reproduce the pending root,
// checkPath at Index finds Kind at the leaf.
type Fault struct {
	Index uint64
	Kind  merkle.LedgerFault
}

// Result is an audit's outcome.
type Result struct {
	// Check is the first check that failed, and Err its error; CheckNone and
	// nil when the root passed every check.
	Check Check
	Err   error
	// Missing lists objects that could not be obtained: unavailable, or
	// served with bytes that do not match their reference.
	Missing []Ref
	// Previous is the latest finalized root's tree and PreviousAccounts its
	// accounts, once check 2 passed.
	Previous         *merkle.LedgerSumTree
	PreviousAccounts []merkle.LedgerAccount
	// Accounts are the pending root's published accounts, once read, and Tree
	// their tree. Reproduced reports that Tree is the pending root: root,
	// total and leaf count.
	Accounts   []merkle.LedgerAccount
	Tree       *merkle.LedgerSumTree
	Reproduced bool
	// Faults lists, by index, every previous account whose published row is
	// below its value, names another coldkey, or is missing.
	Faults []Fault
	// FetchedBytes counts the bytes fetched for objects not in custody.
	FetchedBytes uint64
}

// Action is the §9.3 response to a result.
type Action int

const (
	// ActionRefuse: refuse approval and send nothing.
	ActionRefuse Action = iota
	// ActionApprove: approve the pending root.
	ActionApprove
	// ActionChallenge: send checkPath for each fault, with
	// PreviousAccounts[i], Previous.Proof(i) and Tree.Path(i).
	ActionChallenge
	// ActionDemand: refuse approval and, by commit + 7,200, demand every
	// fault's index and a value-weighted sample of the previous accounts,
	// each with PreviousAccounts[i] and Previous.Proof(i).
	ActionDemand
)

func (a Action) String() string {
	switch a {
	case ActionApprove:
		return "approve"
	case ActionChallenge:
		return "challenge"
	case ActionDemand:
		return "demand"
	default:
		return "refuse"
	}
}

// Action returns the response §9.3 prescribes: approve a root that passed;
// challenge a fault when the published rows reproduce the root, so that
// checkPath can prove it; demand when objects are missing or the rows do
// not reproduce the root and there is a previous root to protect; refuse
// otherwise.
func (r *Result) Action() Action {
	switch {
	case r.Err == nil:
		return ActionApprove
	case r.Reproduced && len(r.Faults) > 0:
		return ActionChallenge
	case r.Previous != nil && (len(r.Missing) > 0 || !r.Reproduced):
		return ActionDemand
	default:
		return ActionRefuse
	}
}

// failure is a check the pending root failed, as opposed to an error of
// the verifier's own inputs or context.
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
	headerRole         = role{check: CheckHeader, mismatch: ErrLedgerHash, oversize: ErrHeaderTooLarge, limit: MaxHeaderBytes}
	previousHeaderRole = role{check: CheckPrevious, within: ErrPreviousRebuild, mismatch: ErrLedgerHash, oversize: ErrHeaderTooLarge, limit: MaxHeaderBytes}
	previousChunkRole  = role{check: CheckPrevious, within: ErrPreviousRebuild, mismatch: ErrChunkHash, oversize: ErrChunkLength, limit: ChunkRows * AccountRowBytes}
	accountChunkRole   = role{check: CheckChunks, mismatch: ErrChunkHash, oversize: ErrChunkLength, limit: ChunkRows * AccountRowBytes}
	providerChunkRole  = role{check: CheckChunks, mismatch: ErrChunkHash, oversize: ErrChunkLength, limit: ChunkRows * ProviderRowBytes}
)

type verifier struct {
	ctx context.Context
	in  *VerifyInput
	res *Result
	// order is the pending accounts in ascending coldkey order.
	order []uint32
}

// Verify audits the pending root (§9.2). It returns an error only for its
// own inputs, its context, its custody or its head oracle; the audit's
// outcome is in the Result. The checks run in this order, stopping at the
// first that fails: 2 (the previous root, so that a demand has accounts to
// protect), 1, the pending root's account chunks (4) and the faults they
// show against the previous root, 3, 8, 9, then per covered epoch its
// provider chunks (4), rows (5) and head flags (6) with its allocation, and
// finally 7. Objects fetched are kept in custody, and an epoch's rows that
// pass checks 4 to 6 and allocate are recorded as valid.
func Verify(ctx context.Context, in VerifyInput) (*Result, error) {
	if ctx == nil || in.Fetcher == nil || in.Custody == nil {
		return nil, ErrInput
	}
	if err := ctx.Err(); err != nil {
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
	if err := v.recoveryDeclarations(header); err != nil {
		return err
	}
	held, heldRecovery, err := v.fixedInputs(header)
	if err != nil {
		return err
	}
	if err := v.checkAccounts(); err != nil {
		return err
	}
	if err := v.checkTree(); err != nil {
		return err
	}
	return v.allocation(header, p, held, heldRecovery)
}

// get returns an object from custody, or fetches, checks and keeps it.
func (v *verifier) get(ref Ref, r *role) ([]byte, error) {
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
		return nil, fail(CheckFetch, fmt.Errorf("%w: %v: %w", ErrObjectUnavailable, ref, err))
	}
	if uint64(len(data)) > limit {
		if budget {
			return nil, fail(CheckFetch, fmt.Errorf("%w: %d bytes at %v", ErrByteBudget, v.in.MaxFetchBytes, ref))
		}
		return nil, r.fail(fmt.Errorf("%w: %v", r.oversize, ref))
	}
	if RefOf(data) != ref {
		v.res.Missing = append(v.res.Missing, ref)
		return nil, r.fail(fmt.Errorf("%w: %v", r.mismatch, ref))
	}
	if err := v.in.Custody.Keep(v.ctx, ref, data); err != nil {
		return nil, err
	}
	return data, nil
}

// loadAccounts reads an account chunk list (check 4 for the pending root).
func (v *verifier) loadAccounts(refs []Ref, r *role) ([]merkle.LedgerAccount, error) {
	var accounts []merkle.LedgerAccount
	for j, ref := range refs {
		data, err := v.get(ref, r)
		if err != nil {
			return nil, err
		}
		if err := checkChunk(len(data), AccountRowBytes, j, len(refs)); err != nil {
			return nil, r.fail(err)
		}
		accounts = decodeAccountRows(accounts, data)
	}
	return accounts, nil
}

// previous runs check 2: the latest finalized root's accounts, from its
// header, sum to its total and rebuild it.
func (v *verifier) previous() error {
	previous := v.in.Previous
	if previous == nil {
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
	accounts, err := v.loadAccounts(header.Accounts, &previousChunkRole)
	if err != nil {
		return err
	}
	var sum uint256.Int
	for i := range accounts {
		sum.Add(&sum, &accounts[i].Cumulative)
	}
	if !sum.Eq(&previous.Total) {
		return fail(CheckPrevious, fmt.Errorf("%w: %s, total %s", ErrPreviousNotTight, sum.Dec(), previous.Total.Dec()))
	}
	if uint64(len(accounts)) != previous.LeafCount {
		return fail(CheckPrevious, fmt.Errorf("%w: %d accounts, leaf count %d", ErrPreviousRebuild, len(accounts), previous.LeafCount))
	}
	tree, err := merkle.BuildLedgerSumTree(v.ctx, accounts)
	if err != nil {
		return err
	}
	if root, _ := tree.Root(); root != previous.Root {
		return fail(CheckPrevious, fmt.Errorf("%w: root", ErrPreviousRebuild))
	}
	v.res.Previous, v.res.PreviousAccounts = tree, accounts
	return nil
}

// header runs check 1 on the pending root's header.
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
		return nil, fail(CheckHeader, fmt.Errorf("%w: %d, pool %d", ErrHeaderNoID, h.NoID, v.in.NoID))
	}
	if h.Epoch != pending.Epoch {
		return nil, fail(CheckHeader, fmt.Errorf("%w: %d, pending %d", ErrHeaderEpoch, h.Epoch, pending.Epoch))
	}
	if pending.Epoch <= p {
		return nil, fail(CheckHeader, fmt.Errorf("%w: epoch %d does not follow %d", ErrCoveredEpochs, pending.Epoch, p))
	}
	if pending.Epoch-p > MaxCoveredEpochs {
		return nil, fail(CheckHeader, fmt.Errorf("%w: epochs %d to %d", ErrTooManyEpochs, p+1, pending.Epoch))
	}
	if uint64(len(h.Epochs)) != pending.Epoch-p {
		return nil, fail(CheckHeader, fmt.Errorf("%w: %d epochs listed for %d to %d", ErrCoveredEpochs, len(h.Epochs), p+1, pending.Epoch))
	}
	for i := range h.Epochs {
		if h.Epochs[i].Epoch != p+1+uint64(i) {
			return nil, fail(CheckHeader, fmt.Errorf("%w: entry %d is epoch %d", ErrCoveredEpochs, i, h.Epochs[i].Epoch))
		}
	}
	return h, nil
}

// accounts reads the pending root's account chunks (check 4), lists the
// previous accounts they fault, and rebuilds their tree.
func (v *verifier) accounts(h *Header) error {
	accounts, err := v.loadAccounts(h.Accounts, &accountChunkRole)
	if err != nil {
		return err
	}
	v.res.Accounts = accounts
	v.res.Faults = Faults(v.res.PreviousAccounts, accounts)
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

// Faults lists, by index, every previous account whose row in accounts is
// below its value (merkle.LedgerFaultLowerValue), names another coldkey or
// is missing (merkle.LedgerFaultWrongLeaf): when accounts are a pending
// root's tree, what checkPath finds at the account's leaf. previous may be
// any finalized root's accounts, as positions and coldkeys never change and
// values never decrease.
func Faults(previous, accounts []merkle.LedgerAccount) []Fault {
	var faults []Fault
	for i := range previous {
		kind := merkle.LedgerFaultNone
		switch {
		case i >= len(accounts):
			kind = merkle.LedgerFaultWrongLeaf
		case accounts[i].Cumulative.Lt(&previous[i].Cumulative):
			kind = merkle.LedgerFaultLowerValue
		case accounts[i].Coldkey != previous[i].Coldkey:
			kind = merkle.LedgerFaultWrongLeaf
		}
		if kind != merkle.LedgerFaultNone {
			faults = append(faults, Fault{Index: uint64(i), Kind: kind})
		}
	}
	return faults
}

// recoveryDeclarations runs check 10: each covered epoch lists exactly the
// recovery components the chain calls for, with their scheduled amounts. It
// runs once the pending accounts are read, so a dispute over the recovery
// is a refusal with the root reproduced, never a demand.
func (v *verifier) recoveryDeclarations(h *Header) error {
	for i := range h.Epochs {
		epoch := &h.Epochs[i]
		want := v.in.Recoveries[epoch.Epoch]
		for j, listed := range epoch.Recovery {
			if j >= len(want) || listed.Epoch != want[j].Epoch {
				return fail(CheckRecovery, fmt.Errorf("%w: epoch %d recovers epoch %d", ErrRecoveryUnexpected, epoch.Epoch, listed.Epoch))
			}
			if listed.Amount != want[j].Amount {
				return fail(CheckRecovery, fmt.Errorf("%w: epoch %d recovers %d of epoch %d, not %d", ErrRecoveryAmount, epoch.Epoch, listed.Amount, listed.Epoch, want[j].Amount))
			}
		}
		if len(epoch.Recovery) < len(want) {
			return fail(CheckRecovery, fmt.Errorf("%w: epoch %d does not recover epoch %d", ErrRecoveryMissing, epoch.Epoch, want[len(epoch.Recovery)].Epoch))
		}
	}
	// PlanRecovery plans only covered epochs; anything else is the caller's.
	for k, want := range v.in.Recoveries {
		if len(want) > 0 && (k < h.Epochs[0].Epoch || k > h.Epoch) {
			return fmt.Errorf("%w: a recovery is expected at epoch %d, which the root does not cover", ErrInput, k)
		}
	}
	return nil
}

// fixedRows runs check 3 for one list of provider chunks of a pool epoch: a
// covered epoch's, or a recovery component's source epoch's. It records a
// null declaration it accepts, and reports whether custody holds the
// epoch's valid rows.
func (v *verifier) fixedRows(epoch uint64, null bool, providers []Ref, label string) (bool, error) {
	chunks, ok, err := v.in.Custody.ValidRows(v.ctx, v.in.NoID, epoch)
	if err != nil {
		return false, err
	}
	switch {
	case ok && null:
		return false, fail(CheckFixedInputs, fmt.Errorf("%w: %s", ErrNullAgainstValidRows, label))
	case ok && !slices.Equal(chunks, providers):
		return false, fail(CheckFixedInputs, fmt.Errorf("%w: %s", ErrFixedInputs, label))
	case null:
		if err := v.in.Custody.KeepNull(v.ctx, v.in.NoID, epoch, v.in.Seq); err != nil {
			return false, err
		}
	}
	return ok, nil
}

// fixedInputs runs check 3 and records every null declaration it accepts.
// It returns, per covered epoch, whether custody holds its valid rows, and
// per recovery component whether custody holds its source epoch's.
func (v *verifier) fixedInputs(h *Header) ([]bool, [][]bool, error) {
	held, heldRecovery := make([]bool, len(h.Epochs)), make([][]bool, len(h.Epochs))
	for i := range h.Epochs {
		epoch := &h.Epochs[i]
		var err error
		if held[i], err = v.fixedRows(epoch.Epoch, epoch.Null, epoch.Providers, fmt.Sprintf("epoch %d", epoch.Epoch)); err != nil {
			return nil, nil, err
		}
		heldRecovery[i] = make([]bool, len(epoch.Recovery))
		for j := range epoch.Recovery {
			component := &epoch.Recovery[j]
			if heldRecovery[i][j], err = v.fixedRows(component.Epoch, component.Null, component.Providers, fmt.Sprintf("epoch %d recovery %d", epoch.Epoch, component.Epoch)); err != nil {
				return nil, nil, err
			}
		}
	}
	return held, heldRecovery, nil
}

// checkAccounts runs check 8.
func (v *verifier) checkAccounts() error {
	if len(v.res.Faults) > 0 {
		fault := v.res.Faults[0]
		if fault.Kind == merkle.LedgerFaultLowerValue {
			return fail(CheckAccounts, fmt.Errorf("%w: account %d", ErrAccountLowered, fault.Index))
		}
		return fail(CheckAccounts, fmt.Errorf("%w: account %d", ErrAccountMoved, fault.Index))
	}
	accounts := v.res.Accounts
	order, duplicate := coldkeyOrder(accounts)
	if duplicate >= 0 {
		return fail(CheckAccounts, fmt.Errorf("%w: account %d", ErrAccountDuplicate, duplicate))
	}
	for i := len(v.res.PreviousAccounts); i < len(accounts); i++ {
		if i > len(v.res.PreviousAccounts) && bytes.Compare(accounts[i-1].Coldkey[:], accounts[i].Coldkey[:]) > 0 {
			return fail(CheckAccounts, fmt.Errorf("%w: account %d", ErrAccountUnsorted, i))
		}
		if accounts[i].Cumulative.IsZero() {
			return fail(CheckAccounts, fmt.Errorf("%w: account %d", ErrAccountZeroDelta, i))
		}
	}
	v.order = order
	return nil
}

// checkTree runs check 9.
func (v *verifier) checkTree() error {
	if v.res.Reproduced {
		return nil
	}
	pending := &v.in.Pending
	if v.res.Tree == nil || v.res.Tree.LeafCount() != pending.LeafCount {
		return fail(CheckTree, fmt.Errorf("%w: %d rows, leaf count %d", ErrTreeLeafCount, len(v.res.Accounts), pending.LeafCount))
	}
	if _, total := v.res.Tree.Root(); !total.Eq(&pending.Total) {
		return fail(CheckTree, fmt.Errorf("%w: %s, committed %s", ErrTreeTotal, total.Dec(), pending.Total.Dec()))
	}
	return fail(CheckTree, ErrTreeRoot)
}

// allocation runs checks 4 to 6 on each covered epoch's provider rows and
// on its recovery components' source rows, walks the epochs (§6.4), each
// recovery component allocated by its source rows first, and runs check 7.
func (v *verifier) allocation(h *Header, p uint64, held []bool, heldRecovery [][]bool) error {
	var base uint256.Int
	if v.in.Previous != nil {
		base = v.in.Previous.Total
	}
	funding, _, err := fundings(v.in.FundedThrough, p, v.in.Pending.Epoch, &base)
	if err != nil {
		return err
	}
	var w walk
	for i := range h.Epochs {
		epoch := &h.Epochs[i]
		ordinary, err := v.recovery(epoch, &w, &funding[i], heldRecovery[i])
		if err != nil {
			return err
		}
		var rows []protocol.LedgerUsageRow
		if !epoch.Null {
			if rows, err = v.providerRows(epoch.Epoch, epoch.Providers, fmt.Sprintf("epoch %d", epoch.Epoch)); err != nil {
				return err
			}
		}
		// Check 5 bounds the usage, so the allocation fails only with the
		// context.
		if _, _, err := w.step(v.ctx, &ordinary, rows, epoch.Null); err != nil {
			return err
		}
		if !epoch.Null && !held[i] {
			if err := v.in.Custody.KeepValidRows(v.ctx, v.in.NoID, epoch.Epoch, epoch.Providers); err != nil {
				return err
			}
		}
	}
	if !w.allocated {
		return fail(CheckAllocation, fmt.Errorf("%w: epoch %d", ErrEpochNotAllocated, v.in.Pending.Epoch))
	}
	return v.checkDeltas(w.sum)
}

// recovery checks and allocates a covered epoch's recovery components, each
// over its source epoch's rows (checks 4 to 6, with the source epoch's head
// bindings), records valid source rows, and returns the epoch's ordinary
// funding: f_k less the amounts allocated. Check 10 has matched the
// components to the chain's, so amounts beyond f_k are the caller's input
// error.
func (v *verifier) recovery(epoch *HeaderEpoch, w *walk, f *uint256.Int, held []bool) (uint256.Int, error) {
	ordinary := *f
	var total uint256.Int
	for _, component := range epoch.Recovery {
		total.AddUint64(&total, component.Amount)
	}
	if total.Gt(f) {
		return ordinary, fmt.Errorf("%w: epoch %d recovers %s of funding %s", ErrFunding, epoch.Epoch, total.Dec(), f.Dec())
	}
	for j := range epoch.Recovery {
		component := &epoch.Recovery[j]
		if component.Null {
			continue
		}
		label := fmt.Sprintf("epoch %d recovery %d", epoch.Epoch, component.Epoch)
		rows, err := v.providerRows(component.Epoch, component.Providers, label)
		if err != nil {
			return ordinary, err
		}
		amount := uint256.NewInt(component.Amount)
		allocated, _, err := w.recover(v.ctx, amount, rows)
		if err != nil {
			return ordinary, err
		}
		if allocated {
			ordinary.Sub(&ordinary, amount)
		}
		if !held[j] {
			if err := v.in.Custody.KeepValidRows(v.ctx, v.in.NoID, component.Epoch, component.Providers); err != nil {
				return ordinary, err
			}
		}
	}
	return ordinary, nil
}

// providerRows reads a pool epoch's provider chunks (check 4) and checks
// its rows (check 5) and its head flags against the epoch's bindings (check
// 6). label names them in errors.
func (v *verifier) providerRows(epoch uint64, providers []Ref, label string) ([]protocol.LedgerUsageRow, error) {
	var rows []protocol.LedgerUsageRow
	for j, ref := range providers {
		data, err := v.get(ref, &providerChunkRole)
		if err != nil {
			return nil, err
		}
		if err := checkChunk(len(data), ProviderRowBytes, j, len(providers)); err != nil {
			return nil, fail(CheckChunks, fmt.Errorf("%s: %w", label, err))
		}
		if rows, err = decodeProviderRows(rows, data); err != nil {
			return nil, fail(CheckChunks, fmt.Errorf("%s chunk %d: %w", label, j, err))
		}
	}
	if err := checkProviderRows(rows); err != nil {
		return nil, fail(CheckRows, fmt.Errorf("%s: %w", label, err))
	}
	if v.in.Heads == nil || len(rows) == 0 {
		return rows, nil
	}
	bound, err := v.in.Heads.HeadBindings(v.ctx, epoch)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		switch want := bound[rows[i].ClientID]; {
		case rows[i].HeadExcluded && !want:
			return nil, fail(CheckHeadExclusion, fmt.Errorf("%w: %s row %d", ErrHeadFlagSet, label, i))
		case !rows[i].HeadExcluded && want:
			return nil, fail(CheckHeadExclusion, fmt.Errorf("%w: %s row %d", ErrHeadFlagCleared, label, i))
		}
	}
	return rows, nil
}

// checkDeltas runs check 7's comparison: each account's increase over the
// previous root is its coldkey's summed delta, and each coldkey with a
// positive delta has an account. Check 8 has passed, so increases are not
// negative and each coldkey has at most one account.
func (v *verifier) checkDeltas(sum []protocol.LedgerDelta) error {
	accounts, previous, order := v.res.Accounts, v.res.PreviousAccounts, v.order
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
			return fail(CheckAllocation, fmt.Errorf("%w: coldkey %x", ErrAccountOmitted, d.Coldkey))
		}
	}
	for i := range accounts {
		increase := accounts[i].Cumulative
		if i < len(previous) {
			increase.Sub(&increase, &previous[i].Cumulative)
		}
		if !increase.Eq(&expected[i]) {
			return fail(CheckAllocation, fmt.Errorf("%w: account %d increased %s, delta %s", ErrAccountDelta, i, increase.Dec(), expected[i].Dec()))
		}
	}
	return nil
}
