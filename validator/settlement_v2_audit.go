//go:build linux || darwin

// The settlement v2 ledger audit (VERSION2-FINAL.md §9.2, §9.3). For every
// configured operator whose pool vault2 registered, the validator reads the
// pending root, the latest finalized root F, fundedThrough for the covered
// epochs and vault2's challenge window at a finalized, hash-pinned block. It
// fetches the ledger objects it does not hold from the operator's object
// endpoint into durable custody, within --ledger_max_bytes per root over all
// attempts, runs ledger.Verify with head bindings read from the archive, and
// decides once per root:
//
//   - approve a root that passes every check;
//   - challenge the first fault that the published accounts, when they
//     reproduce the root, prove against the protected root;
//   - refuse a root whose objects could not be obtained (missing, or beyond
//     the byte budget) by commit + CHALLENGE_BLOCKS/2 (7,200 blocks), or that
//     failed any other check; when the published accounts are missing or do
//     not reproduce the root, also demand the §9.3 set of the protected
//     root's accounts.
//
// The protected root is the latest finalized root whose accounts custody
// holds and verified: F, which check 2 verifies, or when F's objects cannot
// be obtained or do not rebuild it, the latest older root whose objects
// custody holds and rebuild it. Verify stops at check 2 then, so the audit
// reads the pending root's published accounts itself to compare them with
// that root. Demands and challenges name the protected root's index and
// prove its accounts against it. An audit that could not obtain the objects
// Verify reads is retried until that block.
// A pool with a phase C recovery schedule (ledger.ScheduledRecovery) also
// has the recovery components its covered epochs must carry read from
// vault2's SurplusAssigned events, each source amount checked against
// vault1's terminal RootMissed entitlement, and Verify requires exactly
// those (VERSION2-FINAL.md §16 step 6).
// The decision and the demand set, drawn once after the commit was seen, are
// recorded before anything is sent, and sends take their trees from
// custody; the auditor sender is idempotent, so a restart sends exactly the
// same transactions. An approval is kept counting at the current auditor set
// while the root is pending. Without an auditor key the audit records its
// decisions and sends nothing. It never changes a pool's weight (VA-5), and
// its failures are reported and retried, never fatal.
package validator

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/stabi"
)

// settlementV2AuditInterval spaces the audit's passes. A pass that holds its
// decision reads the pending roots and sends from custody; one that waits
// for objects verifies again.
const settlementV2AuditInterval = time.Minute

// The audit's decisions, as recorded.
const (
	settlementV2DecisionApprove   = "approve"
	settlementV2DecisionChallenge = "challenge"
	settlementV2DecisionDemand    = "demand"
	settlementV2DecisionRefuse    = "refuse"
)

// settlementV2PoolState is what the audit read about one pool at a pinned
// finalized block.
type settlementV2PoolState struct {
	noID       uint64
	block      uint64
	blockHash  [32]byte
	registered bool
	firstEpoch uint64
	pending    stabi.ISTSettlementVault2Pending
	// previous is the latest finalized root F, at previousIndex; nil before
	// the pool's first root.
	previous      *stabi.ISTSettlementVault2Root
	previousIndex uint64
	// funded holds fundedThrough for the covered epochs, read only for a
	// range Verify can audit at all.
	funded map[uint64]uint256.Int
	// challengeBlocks is vault2's CHALLENGE_BLOCKS.
	challengeBlocks uint64
}

// settlementV2AuditReader is what the audit reads from chain.
type settlementV2AuditReader interface {
	// poolState reads the pool at the finalized head; pending and the rest
	// only when it is registered.
	poolState(ctx context.Context, noID uint64) (*settlementV2PoolState, error)
	// finalizedRoot reads the pool's finalized root rootIndex at state's
	// pinned block.
	finalizedRoot(ctx context.Context, state *settlementV2PoolState, rootIndex uint64) (stabi.ISTSettlementVault2Root, error)
	// headOracle decides check 6 at state's pinned block.
	headOracle(state *settlementV2PoolState) ledger.HeadOracle
	// demandHistory reads, from vault2's events between F's finalization and
	// the pending commit, the earlier pending roots over F that were replaced
	// or deleted while a demand was open, and the indexes of those demands.
	demandHistory(ctx context.Context, state *settlementV2PoolState) (doublings uint, again []uint64, err error)
	// recoveries returns the phase C recovery components the pending root's
	// covered epochs must carry (ledger.PlanRecovery), each source amount
	// checked against vault1 at state's pinned block, or nil for a pool
	// without a recovery schedule.
	recoveries(ctx context.Context, state *settlementV2PoolState) (map[uint64][]ledger.Recovery, error)
}

// settlementV2AuditSender sends the auditor's transactions;
// *SettlementV2Sender implements it.
type settlementV2AuditSender interface {
	Approve(ctx context.Context, noID uint64, pending stabi.ISTSettlementVault2Pending) error
	Challenge(ctx context.Context, noID uint64, pending stabi.ISTSettlementVault2Pending, rootIndex uint64, account stabi.LedgerTreeAccount, oldProof []stabi.LedgerTreeProofNode, newPath []stabi.LedgerTreePathNode) error
	Demand(ctx context.Context, noID uint64, pending stabi.ISTSettlementVault2Pending, rootIndex uint64, account stabi.LedgerTreeAccount, oldProof []stabi.LedgerTreeProofNode) error
}

// settlementV2Anchor is the protected root: the finalized root rootIndex
// whose accounts the audit demands or challenges, with their tree.
type settlementV2Anchor struct {
	index    uint64
	accounts []merkle.LedgerAccount
	tree     *merkle.LedgerSumTree
}

// settlementV2AuditPool is one audited pool and its operator's objects.
type settlementV2AuditPool struct {
	noID    uint64
	objects ledger.Fetcher
}

// settlementV2Audit audits the pending roots of its pools.
type settlementV2Audit struct {
	reader  settlementV2AuditReader
	custody *settlementV2Custody
	// sender is nil without --ledger_auditor_key_file.
	sender   settlementV2AuditSender
	pools    []settlementV2AuditPool
	maxBytes uint64
	// random is crypto/rand.Reader; tests fix it.
	random io.Reader
	// confirm reports whether the chain has confirmed the configured
	// contracts (§13); the audit runs no pass before it succeeds once. Nil
	// once confirmed.
	confirm func(context.Context) error
	// planned holds each pending root's recovery components, read once: they
	// are fixed once its epochs are funded.
	planned map[settlementV2PlanKey]map[uint64][]ledger.Recovery
}

// settlementV2PlanKey names a pool's pending root.
type settlementV2PlanKey struct {
	noID, seq uint64
}

func newSettlementV2Audit(reader settlementV2AuditReader, custody *settlementV2Custody, sender settlementV2AuditSender, pools []settlementV2AuditPool, maxBytes uint64) *settlementV2Audit {
	return &settlementV2Audit{reader: reader, custody: custody, sender: sender, pools: pools, maxBytes: maxBytes, random: rand.Reader, planned: map[settlementV2PlanKey]map[uint64][]ledger.Recovery{}}
}

// errSettlementV2Unconfirmed: the release runtime has not yet confirmed the
// settlement_v2 contracts at a finalized snapshot.
var errSettlementV2Unconfirmed = errors.New("settlement v2 contracts are not confirmed at a finalized snapshot yet")

// settlementV2Confirmed returns nil once the runtime's identity check,
// verifyReleaseSettlementV2Context in its first advance, has passed. It is
// the release audit's confirmation, so the audit starts only after it.
func (self *releaseRuntimeV2) settlementV2Confirmed(ctx context.Context) error {
	release, err := self.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	if !self.settlementV2Verified {
		return errSettlementV2Unconfirmed
	}
	return nil
}

// newReleaseSettlementV2Audit audits the configured operators' pools when the
// configuration names settlement v2, and returns nil otherwise. Custody and
// the sender's records live under state_dir; the flags come from `validator
// run` (SettlementV2RuntimeOptionsFrom). The audit runs no pass before
// confirm succeeds.
func newReleaseSettlementV2Audit(ctx context.Context, cfg *ReleaseConfig, chain *ChainClient, confirm func(context.Context) error) (*settlementV2Audit, error) {
	addresses := releaseSettlementV2Addresses(cfg)
	if !addresses.configured() {
		return nil, nil
	}
	custody, err := openSettlementV2Custody(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	options := SettlementV2RuntimeOptionsFrom(ctx)
	var sender settlementV2AuditSender
	if options.AuditorKeyFile != "" {
		if sender, err = NewSettlementV2Sender(chain, cfg.RPC, cfg.ChainID, addresses, options.AuditorKeyFile, cfg.StateDir); err != nil {
			return nil, err
		}
	}
	pools := make([]settlementV2AuditPool, len(cfg.Operators))
	for i, operator := range cfg.Operators {
		objects, err := newSettlementV2Objects(operator.APIURL)
		if err != nil {
			return nil, err
		}
		pools[i] = settlementV2AuditPool{noID: operator.NoID, objects: objects}
	}
	reader := &settlementV2AuditChain{chain: chain, vault: addresses.Vault, settlementVault: common.HexToAddress(cfg.SettlementVault), deployBlock: cfg.DeployBlock}
	audit := newSettlementV2Audit(reader, custody, sender, pools, options.LedgerMaxBytes)
	audit.confirm = confirm
	return audit, nil
}

// worker is the audit's run loop, or nil without an audit.
func (self *settlementV2Audit) worker() func(context.Context) error {
	if self == nil {
		return nil
	}
	return self.run
}

// run audits every pool once the configured contracts are confirmed, then
// waits an interval, until ctx ends. A failed confirmation or a pool's
// failure is reported and retried at the next pass.
func (self *settlementV2Audit) run(ctx context.Context) error {
	for {
		if err := self.confirmed(ctx); err != nil {
			if ctx.Err() == nil {
				releaseDiagnostic(ctx, "runtime", "ledger_audit_retrying", 0, false, 0, releaseDiagnosticFacts{cause: releaseDiagnosticReadCause(err)})
			}
		} else {
			for _, pool := range self.pools {
				if err := self.audit(ctx, pool); err != nil && ctx.Err() == nil {
					releaseDiagnostic(ctx, "operator", "ledger_audit_retrying", 0, false, 0, releaseDiagnosticFacts{cause: releaseDiagnosticReadCause(err), operatorId: pool.noID, operatorKnown: true})
				}
			}
		}
		if err := waitReleaseSnapshotRetry(ctx, settlementV2AuditInterval); err != nil {
			return err
		}
	}
}

// confirmed runs confirm until it succeeds once, so the audit never reads or
// sends to a contract the chain has not confirmed.
func (self *settlementV2Audit) confirmed(ctx context.Context) error {
	if self.confirm == nil {
		return nil
	}
	if err := self.confirm(ctx); err != nil {
		return err
	}
	self.confirm = nil
	return nil
}

// audit takes one step on the pool's pending root: it audits the root until
// the audit decides, then sends what the decision calls for until nothing is
// left to send. An approval stays open: it counts only at the auditor set
// version it was sent at, so every pass approves again while the root is
// pending, sending only after the set changed; a change after end(e+1) ends
// the decision.
func (self *settlementV2Audit) audit(ctx context.Context, pool settlementV2AuditPool) error {
	state, err := self.reader.poolState(ctx, pool.noID)
	if err != nil {
		return err
	}
	if !state.registered || state.pending.Seq == 0 {
		return nil
	}
	record, err := self.custody.auditRecord(state.noID, state.pending)
	if err != nil || record.Done {
		return err
	}
	if record.Decision == "" {
		res, err := self.verify(ctx, pool, state, record)
		if err != nil {
			return err
		}
		if err := self.decide(ctx, pool, state, record, res); err != nil || record.Decision == "" {
			return err
		}
	}
	// Without an auditor key a decision is final as recorded.
	if self.sender == nil {
		return nil
	}
	outcome, err := self.send(ctx, state, record)
	if err != nil || outcome == "" {
		return err
	}
	record.Done, record.Outcome = true, outcome
	return self.custody.keepAuditRecord(record)
}

// verify runs ledger.Verify with what is left of the root's byte budget and
// records what it fetched. The recovery components the chain calls for are
// read once per pending root.
func (self *settlementV2Audit) verify(ctx context.Context, pool settlementV2AuditPool, state *settlementV2PoolState, record *settlementV2AuditRecord) (*ledger.Result, error) {
	pending := &state.pending
	key := settlementV2PlanKey{noID: state.noID, seq: pending.Seq}
	recoveries, planned := self.planned[key]
	if !planned {
		var err error
		if recoveries, err = self.reader.recoveries(ctx, state); err != nil {
			return nil, fmt.Errorf("settlement v2 recovery plan of pool %d seq %d: %w", state.noID, pending.Seq, err)
		}
		self.planned[key] = recoveries
	}
	in := ledger.VerifyInput{
		NoID: state.noID, FirstEpoch: state.firstEpoch, Seq: pending.Seq,
		Pending:       settlementV2PendingRoot(pending),
		FundedThrough: state.funded, Fetcher: pool.objects, Custody: self.custody, Heads: self.reader.headOracle(state),
		MaxFetchBytes: self.remainingBytes(record), Recoveries: recoveries,
	}
	if previous := state.previous; previous != nil {
		root := settlementV2LedgerRoot(previous.Epoch, previous.Root, previous.Total, previous.LeafCount, previous.LedgerHash)
		in.Previous = &root
	}
	res, err := ledger.Verify(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("settlement v2 audit of pool %d seq %d: %w", state.noID, pending.Seq, err)
	}
	record.FetchedBytes += res.FetchedBytes
	return res, nil
}

// remainingBytes is what is left of the root's byte budget.
func (self *settlementV2Audit) remainingBytes(record *settlementV2AuditRecord) uint64 {
	return self.maxBytes - min(record.FetchedBytes, self.maxBytes)
}

// decide records the audit's finding and, unless the audit waits for its
// objects, its decision. A demand draws its set now with ledger.DemandSet: a
// value-weighted sample over all of the protected root's accounts, in the
// order drawn and sent first, then the earlier roots' unanswered indexes,
// then every other account the published rows fault. Neither rows the
// operator publishes, even faulting every account, nor demands left
// unanswered on an earlier root can shrink the sample or move it back, so
// the demands sent before the challenge window closes are a sample even
// when the whole set cannot be sent.
func (self *settlementV2Audit) decide(ctx context.Context, pool settlementV2AuditPool, state *settlementV2PoolState, record *settlementV2AuditRecord, res *ledger.Result) error {
	record.observe(state.block, res)
	action := res.Action()
	unobtained := len(res.Missing) > 0 || errors.Is(res.Err, ledger.ErrByteBudget)
	if unobtained && (action == ledger.ActionDemand || action == ledger.ActionRefuse) && state.block < state.pending.CommittedBlock+state.challengeBlocks/2 {
		return self.custody.keepAuditRecord(record)
	}
	var anchor *settlementV2Anchor
	faults := res.Faults
	switch {
	case res.Previous != nil:
		anchor = &settlementV2Anchor{index: state.previousIndex, accounts: res.PreviousAccounts, tree: res.Previous}
	case state.previous != nil:
		var err error
		if anchor, action, faults, err = self.fallback(ctx, pool, state, record); err != nil {
			return err
		}
		record.Faults = settlementV2AuditFaults(faults)
	}
	record.Decision = action.String()
	switch action {
	case ledger.ActionChallenge:
		record.RootIndex = anchor.index
	case ledger.ActionDemand:
		doublings, again, err := self.reader.demandHistory(ctx, state)
		if err != nil {
			return err
		}
		// An older protected root holds fewer accounts than F.
		again = slices.DeleteFunc(again, func(index uint64) bool { return index >= uint64(len(anchor.accounts)) })
		demands, err := ledger.DemandSet(anchor.accounts, faults, doublings, again, self.random)
		if err != nil {
			return fmt.Errorf("settlement v2 demand set of pool %d seq %d: %w", state.noID, state.pending.Seq, err)
		}
		record.RootIndex, record.Doublings, record.Demands = anchor.index, doublings, demands
	}
	if err := self.custody.keepAuditRecord(record); err != nil {
		return err
	}
	releaseDiagnostic(ctx, "operator", "ledger_audit_"+record.Decision, 0, false, 0, releaseDiagnosticFacts{operatorId: state.noID, operatorKnown: true})
	return nil
}

// fallback protects, when F's objects cannot be obtained or do not rebuild
// it, the latest older finalized root whose objects custody holds and
// rebuild it, and compares the pending root's published accounts with it. A
// fault they prove is challenged when they reproduce the pending root, and a
// pending root they do not reproduce is demanded; otherwise, or without such
// an older root, the pending root is refused.
func (self *settlementV2Audit) fallback(ctx context.Context, pool settlementV2AuditPool, state *settlementV2PoolState, record *settlementV2AuditRecord) (*settlementV2Anchor, ledger.Action, []ledger.Fault, error) {
	var anchor *settlementV2Anchor
	for rootIndex := state.previousIndex; anchor == nil && rootIndex > 0; {
		rootIndex--
		var err error
		if anchor, err = self.anchor(ctx, state, rootIndex); err != nil {
			return nil, ledger.ActionRefuse, nil, err
		}
	}
	if anchor == nil {
		return nil, ledger.ActionRefuse, nil, nil
	}
	accounts, ok, err := self.pendingAccounts(ctx, pool, state, record)
	if err != nil || !ok {
		return anchor, ledger.ActionDemand, nil, err
	}
	faults := ledger.Faults(anchor.accounts, accounts)
	// A tree not built because ctx ended leads to a demand, whose history
	// read then fails with ctx.
	switch {
	case settlementV2Rebuild(ctx, accounts, settlementV2PendingRoot(&state.pending)) == nil:
		return anchor, ledger.ActionDemand, faults, nil
	case len(faults) > 0:
		return anchor, ledger.ActionChallenge, faults, nil
	}
	return anchor, ledger.ActionRefuse, nil, nil
}

// anchor returns the finalized root rootIndex's accounts and tree from
// custody alone, or nil when custody does not hold objects that rebuild that
// root.
func (self *settlementV2Audit) anchor(ctx context.Context, state *settlementV2PoolState, rootIndex uint64) (*settlementV2Anchor, error) {
	root := state.previous
	if rootIndex != state.previousIndex {
		read, err := self.reader.finalizedRoot(ctx, state, rootIndex)
		if err != nil {
			return nil, err
		}
		root = &read
	}
	accounts, tree, err := self.custody.accounts(ctx, settlementV2LedgerRoot(root.Epoch, root.Root, root.Total, root.LeafCount, root.LedgerHash))
	if err != nil || tree == nil {
		return nil, err
	}
	return &settlementV2Anchor{index: rootIndex, accounts: accounts, tree: tree}, nil
}

// pendingAccounts reads the pending root's published accounts, which Verify
// did not reach: its header and account chunks, each from custody or fetched
// within what is left of the root's byte budget and kept. ok is false when an
// object cannot be obtained, recorded as missing, or they do not decode.
func (self *settlementV2Audit) pendingAccounts(ctx context.Context, pool settlementV2AuditPool, state *settlementV2PoolState, record *settlementV2AuditRecord) ([]merkle.LedgerAccount, bool, error) {
	data, err := self.object(ctx, pool, record, state.pending.LedgerHash, ledger.MaxHeaderBytes)
	if data == nil {
		return nil, false, err
	}
	header, err := ledger.DecodeHeader(data)
	if err != nil {
		return nil, false, nil
	}
	chunks := make([][]byte, len(header.Accounts))
	for j, ref := range header.Accounts {
		if chunks[j], err = self.object(ctx, pool, record, ref, ledger.ChunkRows*ledger.AccountRowBytes); chunks[j] == nil {
			return nil, false, err
		}
	}
	accounts, err := ledger.DecodeAccountChunks(chunks)
	return accounts, err == nil, nil
}

// object returns an object from custody, or fetches at most limit bytes of it
// within what is left of the root's byte budget, checks them against ref
// and keeps them. It returns nil, and records ref as missing, when the object
// cannot be obtained.
func (self *settlementV2Audit) object(ctx context.Context, pool settlementV2AuditPool, record *settlementV2AuditRecord, ref ledger.Ref, limit uint64) ([]byte, error) {
	data, ok, err := self.custody.Object(ctx, ref)
	if err != nil || ok {
		return data, err
	}
	limit = min(limit, self.remainingBytes(record))
	body, err := pool.objects.Fetch(ctx, ref)
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(body, int64(limit)+1))
		body.Close()
		record.FetchedBytes += uint64(len(data))
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil || uint64(len(data)) > limit || ledger.RefOf(data) != ref {
		record.Missing = append(record.Missing, ref.String())
		return nil, nil
	}
	return data, self.custody.Keep(ctx, ref, data)
}

// send carries out the decision. It returns the outcome once nothing is left
// to send, and no outcome for an approval that counts; an error is retried
// at the next pass. A root that left the pending slot, an approval past
// end(e+1), a demand past the challenge window, a finalized root and an
// answered opening each end the decision.
func (self *settlementV2Audit) send(ctx context.Context, state *settlementV2PoolState, record *settlementV2AuditRecord) (string, error) {
	noID, pending := state.noID, state.pending
	var anchor *settlementV2Anchor
	var tree *merkle.LedgerSumTree
	var err error
	if record.Decision == settlementV2DecisionChallenge || record.Decision == settlementV2DecisionDemand {
		if anchor, err = self.anchor(ctx, state, record.RootIndex); err != nil {
			return "", err
		}
		if anchor != nil && record.Decision == settlementV2DecisionChallenge {
			if _, tree, err = self.custody.accounts(ctx, settlementV2PendingRoot(&pending)); err != nil {
				return "", err
			}
		}
		if anchor == nil || record.Decision == settlementV2DecisionChallenge && tree == nil {
			return "", fmt.Errorf("settlement v2 audit of pool %d seq %d no longer rebuilds the trees of its %s against root %d", noID, pending.Seq, record.Decision, record.RootIndex)
		}
	}
	switch record.Decision {
	case settlementV2DecisionApprove:
		err = self.sender.Approve(ctx, noID, pending)
	case settlementV2DecisionChallenge:
		// A fault indexes an account of the protected root, and a tree that
		// reproduces the pending root has at least its leaves, so the opening
		// exists.
		index := record.Faults[0].Index
		proof, _ := anchor.tree.Proof(index)
		path, _ := tree.Path(index)
		err = self.sender.Challenge(ctx, noID, pending, anchor.index, settlementV2Account(index, &anchor.accounts[index]), settlementV2ProofNodes(proof), settlementV2PathNodes(path))
	case settlementV2DecisionDemand:
		// Every index is an account of the protected root: drawn from its
		// accounts or faulted against them.
		for _, index := range record.Demands {
			proof, _ := anchor.tree.Proof(index)
			if err = self.sender.Demand(ctx, noID, pending, anchor.index, settlementV2Account(index, &anchor.accounts[index]), settlementV2ProofNodes(proof)); err != nil {
				break
			}
		}
	default:
		return "refused", nil
	}
	switch {
	case err == nil && record.Decision == settlementV2DecisionApprove:
		return "", nil
	case err == nil:
		return "sent", nil
	case errors.Is(err, ErrSettlementV2RootNotPending), errors.Is(err, ErrSettlementV2ApprovalClosed), errors.Is(err, ErrSettlementV2ChallengeWindowClosed),
		errors.Is(err, ErrSettlementV2RootFinalized), errors.Is(err, ErrSettlementV2ChallengeAnswered):
		return err.Error(), nil
	}
	return "", err
}

// settlementV2LedgerRoot is a vault2 root as ledger.Verify takes it.
func settlementV2LedgerRoot(epoch uint64, root [32]byte, total *big.Int, leafCount uint64, ledgerHash [32]byte) ledger.Root {
	return ledger.Root{Epoch: epoch, Root: root, Total: *uint256.MustFromBig(total), LeafCount: leafCount, LedgerHash: ledgerHash}
}

func settlementV2PendingRoot(pending *stabi.ISTSettlementVault2Pending) ledger.Root {
	return settlementV2LedgerRoot(pending.Epoch, pending.Root, pending.Total, pending.LeafCount, pending.LedgerHash)
}

func settlementV2Account(index uint64, account *merkle.LedgerAccount) stabi.LedgerTreeAccount {
	return stabi.LedgerTreeAccount{Index: new(big.Int).SetUint64(index), Coldkey: account.Coldkey, Cumulative: account.Cumulative.ToBig()}
}

func settlementV2ProofNodes(nodes []merkle.LedgerNode) []stabi.LedgerTreeProofNode {
	out := make([]stabi.LedgerTreeProofNode, len(nodes))
	for k := range nodes {
		out[k] = stabi.LedgerTreeProofNode{Hash: nodes[k].Hash, Sum: nodes[k].Sum.ToBig()}
	}
	return out
}

func settlementV2PathNodes(nodes []merkle.LedgerPathNode) []stabi.LedgerTreePathNode {
	out := make([]stabi.LedgerTreePathNode, len(nodes))
	for k := range nodes {
		out[k] = stabi.LedgerTreePathNode{LeftHash: nodes[k].LeftHash, LeftSum: nodes[k].LeftSum.ToBig(), RightHash: nodes[k].RightHash, RightSum: nodes[k].RightSum.ToBig()}
	}
	return out
}
