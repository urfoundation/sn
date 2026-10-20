package onchain

// v2_check.go — `snclaim v2 check --all` (VERSION2-FINAL.md §9.3, §10.2,
// §4.2 G2): an independent watcher of a pool's pending root. It reads the
// pending root, the latest finalized root F and the covered epochs'
// fundedThrough from vault2 at the finalized block, fetches the published
// objects, and runs ledger.Verify (without head bindings, for which a
// watcher has no archive state).
//
// The watcher protects the accounts of the newest finalized root whose
// objects it fetched and verified: F, or when F's objects are missing or do
// not rebuild it, the newest older root whose objects do, which it says.
// Every account of that root is reported as having no fault, a provable
// fault, or as unverifiable when objects are missing or do not reproduce
// the pending root: never "no fault" then. With --challenge it sends
// checkPath for a provable fault, which deletes the root; with --demand it
// sends the §9.3 demand set (ledger.DemandSet) in its order, the
// value-weighted sample first, each demand paying demandFeeWei with the
// root's index, the account and its proof against that root, until the
// challenge window closes.

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"slices"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"

	docopt "github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/ledger"
	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/stabi"
)

// v2LogChunkBlocks is the widest eth_getLogs span asked for: the public
// endpoint's enforced maximum (validator/chain.go getLogsChunkBlocks).
const v2LogChunkBlocks = 1_000

// v2State is a pool's vault2 state at the finalized block.
type v2State struct {
	firstEpoch uint64
	pending    stabi.ISTSettlementVault2Pending
	// latest is the latest finalized root F, at index latestIndex; nil
	// before the pool's first root.
	latest      *stabi.ISTSettlementVault2Root
	latestIndex uint64
	funded      map[uint64]uint256.Int
}

// readV2State reads the pool, its pending root, F and fundedThrough for
// the covered epochs p+1 … e. A range Verify refuses by its length alone
// is not read.
func readV2State(ctx context.Context, r *v2Reader, noID uint64) (*v2State, error) {
	n := new(big.Int).SetUint64(noID)
	pool, err := v2View(ctx, r, vault2.PackPools(n), vault2.UnpackPools)
	if err != nil {
		return nil, fmt.Errorf("pools(%d): %w", noID, err)
	}
	if !pool.Registered {
		return nil, fmt.Errorf("pool %d is not registered in vault2 %s", noID, r.vault.Hex())
	}
	pending, err := v2View(ctx, r, vault2.PackPending(n), vault2.UnpackPending)
	if err != nil {
		return nil, fmt.Errorf("pending(%d): %w", noID, err)
	}
	state := &v2State{firstEpoch: pool.FirstEpoch, pending: pending, funded: map[uint64]uint256.Int{}}
	if pending.Seq == 0 {
		return state, nil
	}
	count, err := v2View(ctx, r, vault2.PackRootCount(n), vault2.UnpackRootCount)
	if err != nil {
		return nil, fmt.Errorf("rootCount(%d): %w", noID, err)
	}
	p := pool.FirstEpoch - 1
	if count.Sign() > 0 {
		index := count.Uint64() - 1
		root, err := v2View(ctx, r, vault2.PackRoots(n, new(big.Int).SetUint64(index)), vault2.UnpackRoots)
		if err != nil {
			return nil, fmt.Errorf("roots(%d,%d): %w", noID, index, err)
		}
		state.latest, state.latestIndex, p = &root, index, root.Epoch
	}
	if pending.Epoch > p && pending.Epoch-p <= ledger.MaxCoveredEpochs {
		for k := p + 1; k <= pending.Epoch; k++ {
			funded, err := v2View(ctx, r, vault2.PackFundedThrough(n, new(big.Int).SetUint64(k)), vault2.UnpackFundedThrough)
			if err != nil {
				return nil, fmt.Errorf("fundedThrough(%d,%d): %w", noID, k, err)
			}
			state.funded[k] = *uint256.MustFromBig(funded)
		}
	}
	return state, nil
}

// v2Objects fetches ledger objects from GET /sn/ledger/object on the API
// host, resolved against --api_url as the validator resolves an operator's
// api_url: a path in it is replaced, not extended.
type v2Objects struct{ api *url.URL }

func (o v2Objects) Fetch(ctx context.Context, ref ledger.Ref) (io.ReadCloser, error) {
	return v2Get(ctx, o.api.ResolveReference(&url.URL{Path: "/sn/ledger/object", RawQuery: url.Values{"hash": {ref.String()}}.Encode()}))
}

// v2ReadObject fetches an object of at most limit bytes and checks its
// bytes against its reference.
func v2ReadObject(ctx context.Context, objects ledger.Fetcher, ref ledger.Ref, limit int) ([]byte, error) {
	body, err := objects.Fetch(ctx, ref)
	var data []byte
	if err == nil {
		data, err = io.ReadAll(io.LimitReader(body, int64(limit)+1))
		body.Close()
		if err == nil && (len(data) > limit || ledger.RefOf(data) != ref) {
			err = errors.New("the bytes served do not match it")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("object %v: %w", ref, err)
	}
	return data, nil
}

// v2ReadAccounts reads a root's published accounts: its canonical header
// and account chunks, each checked against its reference, and their tree.
func v2ReadAccounts(ctx context.Context, objects ledger.Fetcher, ledgerHash [32]byte) ([]merkle.LedgerAccount, *merkle.LedgerSumTree, error) {
	data, err := v2ReadObject(ctx, objects, ledgerHash, ledger.MaxHeaderBytes)
	if err != nil {
		return nil, nil, err
	}
	header, err := ledger.DecodeHeader(data)
	if err != nil {
		return nil, nil, fmt.Errorf("header %v: %w", ledger.Ref(ledgerHash), err)
	}
	chunks := make([][]byte, len(header.Accounts))
	for j, ref := range header.Accounts {
		if chunks[j], err = v2ReadObject(ctx, objects, ref, ledger.ChunkRows*ledger.AccountRowBytes); err != nil {
			return nil, nil, err
		}
	}
	accounts, err := ledger.DecodeAccountChunks(chunks)
	if err != nil {
		return nil, nil, fmt.Errorf("account chunks of %v: %w", ledger.Ref(ledgerHash), err)
	}
	tree, err := merkle.BuildLedgerSumTree(ctx, accounts)
	if err != nil {
		return nil, nil, fmt.Errorf("accounts of %v: %w", ledger.Ref(ledgerHash), err)
	}
	return accounts, tree, nil
}

// v2Rebuilds reports that tree is the committed root.
func v2Rebuilds(tree *merkle.LedgerSumTree, root [32]byte, total *big.Int, leafCount uint64) bool {
	hash, sum := tree.Root()
	return hash == root && sum.ToBig().Cmp(total) == 0 && tree.LeafCount() == leafCount
}

// v2Protected is the finalized root whose accounts the watcher protects.
type v2Protected struct {
	index    uint64
	accounts []merkle.LedgerAccount
	tree     *merkle.LedgerSumTree
}

// v2Assessment is what the pending root's published accounts show against
// the protected root, and the §9.3 response.
type v2Assessment struct {
	protected v2Protected
	// tree is the published accounts' tree, nil when they were not read;
	// reproduced reports that it is the pending root.
	tree       *merkle.LedgerSumTree
	reproduced bool
	faults     []ledger.Fault
	action     ledger.Action
}

// cmdV2Check implements `snclaim v2 check --all`.
func cmdV2Check(ctx context.Context, opts docopt.Opts) error {
	vault, err := parseVault2(strOpt(opts, "--vault"))
	if err != nil {
		return err
	}
	noID, _, err := parseUint64Opt(opts, "--no_id")
	if err != nil {
		return err
	}
	api, err := parseAPIURL(strOpt(opts, "--api_url"))
	if err != nil {
		return err
	}
	wantChainID, gasLimit, err := parseChainAndGas(opts)
	if err != nil {
		return err
	}
	challenge, demand := boolOpt(opts, "--challenge"), boolOpt(opts, "--demand")
	var key *ecdsa.PrivateKey
	if challenge || demand {
		if !hasOpt(opts, "--key_file") {
			return errors.New("--challenge and --demand send transactions: --key_file is required")
		}
		if key, err = loadKeyFile(strOpt(opts, "--key_file")); err != nil {
			return err
		}
	}
	client, chainID, rpcURL, err := v2Connect(ctx, opts, wantChainID)
	if err != nil {
		return err
	}
	defer client.Close()

	head, err := ReadEVMBlockIdentity(ctx, client, big.NewInt(int64(rpc.FinalizedBlockNumber)))
	if err != nil {
		return fmt.Errorf("read the finalized EVM head: %w", err)
	}
	reader := &v2Reader{client: client, vault: vault, block: new(big.Int).SetUint64(head.Number)}
	state, err := readV2State(ctx, reader, noID)
	if err != nil {
		return err
	}
	fmt.Printf("settlement v2 ledger check — vault %s, pool %d (chain id %s, rpc %s)\n", vault.Hex(), noID, chainID, rpcURL)
	fmt.Printf("  finalized block:   %d (%s)\n", head.Number, head.Hash)
	pending := &state.pending
	if pending.Seq == 0 {
		fmt.Printf("  pending root:      none; nothing to check\n")
		return nil
	}
	fmt.Printf("  pending root:      seq %d, epoch %d, committed at block %d, %d open demands\n", pending.Seq, pending.Epoch, pending.CommittedBlock, pending.OpenDemands)
	fmt.Printf("                     root 0x%x, total %s, %d accounts, ledger %v\n", pending.Root, formatAlpha(pending.Total), pending.LeafCount, ledger.Ref(pending.LedgerHash))
	objects := v2Objects{api: api}
	in := ledger.VerifyInput{
		NoID: noID, FirstEpoch: state.firstEpoch, Seq: pending.Seq,
		Pending:       v2LedgerRoot(pending.Epoch, pending.Root, pending.Total, pending.LeafCount, pending.LedgerHash),
		FundedThrough: state.funded, Fetcher: objects, Custody: ledger.NewMemoryCustody(),
		MaxFetchBytes: ledger.DefaultMaxFetchBytes,
	}
	if latest := state.latest; latest != nil {
		fmt.Printf("  finalized root:    index %d, epoch %d, root 0x%x, total %s, %d accounts\n", state.latestIndex, latest.Epoch, latest.Root, formatAlpha(latest.Total), latest.LeafCount)
		root := v2LedgerRoot(latest.Epoch, latest.Root, latest.Total, latest.LeafCount, latest.LedgerHash)
		in.Previous = &root
	} else {
		fmt.Printf("  finalized root:    none; the pending root is the pool's first\n")
	}
	res, err := ledger.Verify(ctx, in)
	if err != nil {
		return fmt.Errorf("verify the pending root: %w", err)
	}
	reportV2Audit(res)
	if state.latest == nil {
		return nil
	}
	a := &v2Assessment{
		protected: v2Protected{index: state.latestIndex, accounts: res.PreviousAccounts, tree: res.Previous},
		tree:      res.Tree, reproduced: res.Reproduced, faults: res.Faults, action: res.Action(),
	}
	if res.Previous == nil {
		if a, err = v2Fallback(ctx, reader, objects, noID, state); err != nil {
			return err
		}
	}
	reportV2Accounts(a)

	sender := &v2Sender{client: client, chainID: chainID, rpcURL: rpcURL, vault: vault, key: key, gasLimit: gasLimit, dryRun: boolOpt(opts, "--dry-run")}
	if key != nil {
		sender.from = crypto.PubkeyToAddress(key.PublicKey)
	}
	switch a.action {
	case ledger.ActionChallenge:
		if !challenge {
			return fmt.Errorf("pending root seq %d faults %d accounts of finalized root %d; --challenge deletes it", pending.Seq, len(a.faults), a.protected.index)
		}
		return v2Challenge(ctx, sender, noID, state, a)
	case ledger.ActionDemand:
		if !demand {
			return fmt.Errorf("pending root seq %d leaves the accounts of finalized root %d unverifiable; --demand sends the demand set", pending.Seq, a.protected.index)
		}
		return v2Demand(ctx, sender, noID, state, a, head.Number)
	}
	if a.protected.index != state.latestIndex {
		return fmt.Errorf("finalized root %d's objects are missing or do not rebuild it: the accounts added after root %d cannot be checked", state.latestIndex, a.protected.index)
	}
	return nil
}

// v2LedgerRoot is a vault2 root as ledger.Verify takes it.
func v2LedgerRoot(epoch uint64, root [32]byte, total *big.Int, leafCount uint64, ledgerHash [32]byte) ledger.Root {
	return ledger.Root{Epoch: epoch, Root: root, Total: *uint256.MustFromBig(total), LeafCount: leafCount, LedgerHash: ledgerHash}
}

// v2Fallback protects, when the latest finalized root's objects are missing
// or do not rebuild it, the newest older root whose objects do, and reads
// the pending root's published accounts against it: a fault they show is
// challenged when they reproduce the pending root, and a pending root they
// do not reproduce is demanded.
func v2Fallback(ctx context.Context, r *v2Reader, objects ledger.Fetcher, noID uint64, state *v2State) (*v2Assessment, error) {
	a := &v2Assessment{action: ledger.ActionRefuse}
	found := false
	for k := state.latestIndex; k > 0 && !found; {
		k--
		root, err := v2View(ctx, r, vault2.PackRoots(new(big.Int).SetUint64(noID), new(big.Int).SetUint64(k)), vault2.UnpackRoots)
		if err != nil {
			return nil, fmt.Errorf("roots(%d,%d): %w", noID, k, err)
		}
		accounts, tree, err := v2ReadAccounts(ctx, objects, root.LedgerHash)
		if err == nil && !v2Rebuilds(tree, root.Root, root.Total, root.LeafCount) {
			err = errors.New("its published accounts do not rebuild it")
		}
		if err != nil {
			fmt.Printf("  fallback:          finalized root %d is unverifiable too: %v\n", k, err)
			continue
		}
		a.protected, found = v2Protected{index: k, accounts: accounts, tree: tree}, true
	}
	if !found {
		return nil, fmt.Errorf("no finalized root of pool %d has objects that rebuild it: its accounts cannot be checked, challenged or demanded", noID)
	}
	fmt.Printf("  fallback:          finalized root %d's objects are missing or do not rebuild it; protecting finalized root %d, the newest whose objects do\n", state.latestIndex, a.protected.index)
	accounts, tree, err := v2ReadAccounts(ctx, objects, state.pending.LedgerHash)
	if err != nil {
		fmt.Printf("  pending accounts:  unavailable: %v\n", err)
	} else {
		a.tree, a.faults = tree, ledger.Faults(a.protected.accounts, accounts)
		a.reproduced = v2Rebuilds(tree, state.pending.Root, state.pending.Total, state.pending.LeafCount)
	}
	switch {
	case !a.reproduced:
		a.action = ledger.ActionDemand
	case len(a.faults) > 0:
		a.action = ledger.ActionChallenge
	}
	return a, nil
}

// reportV2Audit prints the audit and the objects it could not obtain.
func reportV2Audit(res *ledger.Result) {
	if res.Err == nil {
		fmt.Printf("  audit:             %v\n", res.Action())
	} else {
		fmt.Printf("  audit:             %v (%v check: %v)\n", res.Action(), res.Check, res.Err)
	}
	for _, ref := range res.Missing {
		fmt.Printf("  object missing:    %v\n", ref)
	}
}

// reportV2Accounts prints, for the protected root's accounts, no fault,
// the provable faults, or unverifiable.
func reportV2Accounts(a *v2Assessment) {
	n, k := len(a.protected.accounts), a.protected.index
	switch a.action {
	case ledger.ActionDemand:
		fmt.Printf("  previous accounts: %d of root %d, unverifiable: the published objects are missing or do not reproduce the pending root\n", n, k)
		for _, fault := range a.faults {
			fmt.Printf("    account %d:      the published row shows %v\n", fault.Index, fault.Kind)
		}
	case ledger.ActionChallenge:
		fmt.Printf("  previous accounts: %d of root %d, %d with a provable fault, %d with no fault\n", n, k, len(a.faults), n-len(a.faults))
		for _, fault := range a.faults {
			fmt.Printf("    account %d:      %v\n", fault.Index, fault.Kind)
		}
	default:
		fmt.Printf("  previous accounts: %d of root %d, no fault\n", n, k)
	}
}

// v2PendingGone reads, at the latest block, whether pending root seq has
// left the pending slot: deleted, replaced or finalized.
func v2PendingGone(ctx context.Context, s *v2Sender, noID, seq uint64) (bool, error) {
	pending, err := v2View(ctx, &v2Reader{client: s.client, vault: s.vault}, vault2.PackPending(new(big.Int).SetUint64(noID)), vault2.UnpackPending)
	if err != nil {
		return false, fmt.Errorf("pending(%d): %w", noID, err)
	}
	return pending.Seq != seq, nil
}

// v2Challenge sends checkPath for the first provable fault: one deletes the
// root. A root that has left the pending slot, deleted by another
// challenger or replaced, is done (SPEC A6).
func v2Challenge(ctx context.Context, s *v2Sender, noID uint64, state *v2State, a *v2Assessment) error {
	fault := a.faults[0]
	path, err := a.tree.Path(fault.Index)
	if err != nil {
		return fmt.Errorf("the pending root has no account %d to open: %w", fault.Index, err)
	}
	// Faults index the protected root's accounts, so each has a proof.
	proof, _ := a.protected.tree.Proof(fault.Index)
	account := &a.protected.accounts[fault.Index]
	seq, k := state.pending.Seq, a.protected.index
	calldata := vault2.PackCheckPath(new(big.Int).SetUint64(noID), new(big.Int).SetUint64(k), v2LedgerAccount(fault.Index, account), v2ProofNodes(proof), v2PathNodes(path))
	receipt, err := s.send(ctx, "challenge", calldata, nil, 0, func() {
		fmt.Printf("  pending:     pool %d, seq %d, root 0x%x\n", noID, seq, state.pending.Root)
		fmt.Printf("  account:     %d of finalized root %d: coldkey 0x%x, cumulative %s\n", fault.Index, k, account.Coldkey, v2Alpha(&account.Cumulative))
		fmt.Printf("  fault:       %v\n", fault.Kind)
	})
	if err != nil {
		if !v2Reverted(err) {
			return err
		}
		gone, rerr := v2PendingGone(ctx, s, noID, seq)
		if rerr != nil {
			return errors.Join(err, rerr)
		}
		if !gone {
			return err
		}
		fmt.Printf("pending root seq %d is no longer pending (deleted or replaced); done\n", seq)
		return nil
	}
	if receipt == nil {
		return nil // dry run
	}
	for _, log := range receipt.Logs {
		if log.Address != s.vault {
			continue
		}
		event, err := vault2.UnpackDistributionDeletedEvent(log)
		if err != nil || event.Seq != seq {
			continue
		}
		fmt.Printf("DistributionDeleted: pool %s, seq %d\n", event.NoId, event.Seq)
		fmt.Printf("  account:     %s of finalized root %s\n", event.Index, event.RootIndex)
		fmt.Printf("  fault:       %v at level %s\n", merkle.LedgerFault(event.Fault), event.Level)
		fmt.Printf("  challenger:  %s\n", event.Challenger.Hex())
		return nil
	}
	return fmt.Errorf("tx %s succeeded but %s emitted no DistributionDeleted for seq %d", receipt.TxHash, s.vault.Hex(), seq)
}

// v2Key names a demand within a pending root: an account index of the
// finalized root it is proven against.
type v2Key struct{ root, index uint64 }

// v2History is what vault2's events since F's finalization say about the
// pending root and its predecessors over F.
type v2History struct {
	// doublings counts the earlier pending roots over F replaced or deleted
	// while a demand was open, and again holds their unanswered demands.
	doublings uint
	again     []v2Key
	// demanded holds the demands on the pending root, and ours those this
	// sender made.
	demanded map[v2Key]bool
	ours     map[v2Key]bool
}

// v2Event is a decoded vault2 event of the demand history.
type v2Event struct {
	name        string
	seq         uint64
	key         v2Key
	replacedSeq uint64
	sender      common.Address
}

// v2EventID returns the ID of a vault2 event. NewSTSettlementVault2 parsed
// the ABI when the package loaded, so parsing it again cannot fail.
func v2EventID(name string) common.Hash {
	parsed, _ := parsedVault2ABI()
	return parsed.Events[name].ID
}

// decodeV2Event decodes a log of the demand history. A DistributionFinalized
// keys its root index.
func decodeV2Event(log *types.Log) (v2Event, error) {
	var topic common.Hash
	if len(log.Topics) > 0 {
		topic = log.Topics[0]
	}
	var ev v2Event
	var err error
	switch topic {
	case v2EventID("DistributionCommitted"):
		var e *stabi.STSettlementVault2DistributionCommitted
		if e, err = vault2.UnpackDistributionCommittedEvent(log); err == nil {
			ev = v2Event{name: "DistributionCommitted", seq: e.Seq, replacedSeq: e.ReplacedSeq}
		}
	case v2EventID("DistributionDeleted"):
		var e *stabi.STSettlementVault2DistributionDeleted
		if e, err = vault2.UnpackDistributionDeletedEvent(log); err == nil {
			ev = v2Event{name: "DistributionDeleted", seq: e.Seq, key: v2Key{e.RootIndex.Uint64(), e.Index.Uint64()}}
		}
	case v2EventID("PathDemanded"):
		var e *stabi.STSettlementVault2PathDemanded
		if e, err = vault2.UnpackPathDemandedEvent(log); err == nil {
			ev = v2Event{name: "PathDemanded", seq: e.Seq, key: v2Key{e.RootIndex.Uint64(), e.Index.Uint64()}, sender: e.Demander}
		}
	case v2EventID("PathAnswered"):
		var e *stabi.STSettlementVault2PathAnswered
		if e, err = vault2.UnpackPathAnsweredEvent(log); err == nil {
			ev = v2Event{name: "PathAnswered", seq: e.Seq, key: v2Key{e.RootIndex.Uint64(), e.Index.Uint64()}}
		}
	case v2EventID("DistributionFinalized"):
		var e *stabi.STSettlementVault2DistributionFinalized
		if e, err = vault2.UnpackDistributionFinalizedEvent(log); err == nil {
			ev = v2Event{name: "DistributionFinalized", seq: e.Seq, key: v2Key{root: e.RootIndex.Uint64()}}
		}
	default:
		err = errors.New("not an event of the demand history")
	}
	if err != nil {
		return ev, fmt.Errorf("vault2 log %d of tx %s at block %d: %w", log.Index, log.TxHash, log.BlockNumber, err)
	}
	return ev, nil
}

// readV2History scans vault2's events of the pool backwards from the
// finalized head, in spans the public endpoint answers, to F's
// DistributionFinalized, and reads the history after it. Demands are keyed
// by seq, root index and account index.
func readV2History(ctx context.Context, s *v2Sender, noID uint64, state *v2State, head uint64) (*v2History, error) {
	query := ethereum.FilterQuery{
		Addresses: []common.Address{s.vault},
		Topics: [][]common.Hash{
			{v2EventID("DistributionCommitted"), v2EventID("DistributionDeleted"), v2EventID("PathDemanded"), v2EventID("PathAnswered"), v2EventID("DistributionFinalized")},
			{common.BigToHash(new(big.Int).SetUint64(noID))},
		},
	}
	var events []v2Event
	for to := head; ; {
		from := to - min(to, v2LogChunkBlocks-1)
		query.FromBlock, query.ToBlock = new(big.Int).SetUint64(from), new(big.Int).SetUint64(to)
		logs, err := retryOnchainRead(ctx, func(ctx context.Context) ([]types.Log, error) {
			cctx, cancel := context.WithTimeout(ctx, callTimeout)
			defer cancel()
			return s.client.FilterLogs(cctx, query)
		})
		if err != nil {
			return nil, fmt.Errorf("eth_getLogs [%d,%d]: %w", from, to, err)
		}
		chunk := make([]v2Event, len(logs))
		start := -1
		for i := range logs {
			if chunk[i], err = decodeV2Event(&logs[i]); err != nil {
				return nil, err
			}
			if chunk[i].name == "DistributionFinalized" && chunk[i].key.root == state.latestIndex {
				start = i
			}
		}
		if start >= 0 {
			events = append(chunk[start+1:], events...)
			break
		}
		events = append(chunk, events...)
		if from == 0 {
			return nil, fmt.Errorf("vault2 logs up to block %d hold no DistributionFinalized of root %d of pool %d", head, state.latestIndex, noID)
		}
		to = from - 1
	}

	history := &v2History{demanded: map[v2Key]bool{}, ours: map[v2Key]bool{}}
	gone := map[uint64]bool{}
	open := map[uint64]map[v2Key]bool{}
	for _, ev := range events {
		switch ev.name {
		case "DistributionCommitted":
			if ev.replacedSeq != 0 {
				gone[ev.replacedSeq] = true
			}
		case "DistributionDeleted":
			gone[ev.seq] = true
		case "PathDemanded":
			if open[ev.seq] == nil {
				open[ev.seq] = map[v2Key]bool{}
			}
			open[ev.seq][ev.key] = true
			if ev.seq == state.pending.Seq {
				history.demanded[ev.key] = true
				if ev.sender == s.from {
					history.ours[ev.key] = true
				}
			}
		case "PathAnswered":
			delete(open[ev.seq], ev.key)
		}
	}
	for seq := range gone {
		if len(open[seq]) > 0 {
			history.doublings++
			for key := range open[seq] {
				history.again = append(history.again, key)
			}
		}
	}
	return history, nil
}

// v2Demand sends the §9.3 demand set against the protected root in
// ledger.DemandSet's order: the value-weighted sample first, in the order
// drawn, then the earlier roots' unanswered indexes, then the faulted rows.
// An earlier root's unanswered demand is demanded again at its account index
// when the protected root holds that account. A sender draws its sample once
// per pending root and protected root: once it holds as many demands there
// as the sample size, later runs demand only the forced indexes, so a
// watcher run again does not pay for a fresh sample. A demand already made
// is done (SPEC A6); a pending root that leaves the slot ends the demands.
// A demand's preflight runs at the latest block and the demand in a later
// one, so demands are sent only while the next block is inside the
// challenge window (§5 Senders): when it closes the demands stop, and those
// sent are the sample's first draws.
func v2Demand(ctx context.Context, s *v2Sender, noID uint64, state *v2State, a *v2Assessment, head uint64) error {
	history, err := readV2History(ctx, s, noID, state, head)
	if err != nil {
		return err
	}
	k, held := a.protected.index, uint64(len(a.protected.accounts))
	var again []uint64
	beyond := 0
	for _, key := range history.again {
		if key.index < held {
			again = append(again, key.index)
		} else {
			beyond++
		}
	}
	// The forced indexes in DemandSet's order after its sample: the earlier
	// unanswered ones, then the other faults, each ascending.
	slices.Sort(again)
	again = slices.Compact(again)
	forced := slices.Clone(again)
	for _, fault := range a.faults {
		if _, found := slices.BinarySearch(again, fault.Index); !found {
			forced = append(forced, fault.Index)
		}
	}
	ours, already := 0, 0
	for key := range history.demanded {
		if key.root != k {
			continue
		}
		already++
		if history.ours[key] {
			ours++
		}
	}
	set := forced
	if history.doublings >= 32 || uint64(ours) < ledger.DemandSample<<history.doublings {
		if set, err = ledger.DemandSet(a.protected.accounts, a.faults, history.doublings, again, v2Random); err != nil {
			return err
		}
	}
	fee, err := v2View(ctx, &v2Reader{client: s.client, vault: s.vault}, vault2.PackDemandFeeWei(), vault2.UnpackDemandFeeWei)
	if err != nil {
		return fmt.Errorf("demandFeeWei(): %w", err)
	}
	window, err := v2View(ctx, &v2Reader{client: s.client, vault: s.vault}, vault2.PackCHALLENGEBLOCKS(), vault2.UnpackCHALLENGEBLOCKS)
	if err != nil {
		return fmt.Errorf("CHALLENGE_BLOCKS(): %w", err)
	}
	end := state.pending.CommittedBlock + window
	seq, n, root := state.pending.Seq, new(big.Int).SetUint64(noID), new(big.Int).SetUint64(k)
	fmt.Printf("demand set: %d accounts of root %d (%d doublings, %d forced), %d already demanded\n", len(set), k, history.doublings, len(forced), already)
	if beyond > 0 {
		fmt.Printf("  %d unanswered earlier demands name accounts that root %d does not hold\n", beyond, k)
	}
	for _, index := range set {
		if history.demanded[v2Key{k, index}] {
			continue
		}
		header, err := retryOnchainRead(ctx, func(ctx context.Context) (*types.Header, error) {
			cctx, cancel := context.WithTimeout(ctx, callTimeout)
			defer cancel()
			return s.client.HeaderByNumber(cctx, nil)
		})
		if err != nil {
			return fmt.Errorf("read the latest EVM block: %w", err)
		}
		if latest := header.Number.Uint64(); latest+1 >= end {
			fmt.Printf("challenge window closing: pending root seq %d takes demands through block %d, and the next block is %d; no more demands sent\n", seq, end-1, latest+1)
			return nil
		}
		// The set indexes the protected root's accounts, so each has a proof.
		proof, _ := a.protected.tree.Proof(index)
		account := &a.protected.accounts[index]
		receipt, err := s.send(ctx, "demand", vault2.PackDemandPath(n, root, v2LedgerAccount(index, account), v2ProofNodes(proof)), fee, 0, func() {
			fmt.Printf("  pending:     pool %d, seq %d\n", noID, seq)
			fmt.Printf("  account:     %d of finalized root %d: coldkey 0x%x, cumulative %s\n", index, k, account.Coldkey, v2Alpha(&account.Cumulative))
			fmt.Printf("  fee:         %s wei\n", fee)
		})
		if err != nil {
			if !v2Reverted(err) {
				return err
			}
			gone, rerr := v2PendingGone(ctx, s, noID, seq)
			if rerr != nil {
				return errors.Join(err, rerr)
			}
			if gone {
				fmt.Printf("pending root seq %d is no longer pending (deleted or replaced); no more demands\n", seq)
				return nil
			}
			demanded, rerr := v2View(ctx, &v2Reader{client: s.client, vault: s.vault}, vault2.PackDemanded(n, seq, root, new(big.Int).SetUint64(index)), vault2.UnpackDemanded)
			if rerr != nil {
				return errors.Join(err, fmt.Errorf("demanded(%d,%d,%d,%d): %w", noID, seq, k, index, rerr))
			}
			if demanded == 0 {
				return err
			}
			fmt.Printf("account %d of root %d: already demanded on seq %d; done\n", index, k, seq)
			continue
		}
		if receipt == nil {
			continue // dry run
		}
		if !v2Demanded(receipt, s.vault, seq, k, index) {
			return fmt.Errorf("tx %s succeeded but %s emitted no PathDemanded for seq %d account %d of root %d", receipt.TxHash, s.vault.Hex(), seq, index, k)
		}
		fmt.Printf("PathDemanded: pool %d, seq %d, account %d of root %d\n", noID, seq, index, k)
	}
	return nil
}

// v2Demanded reports a PathDemanded of seq, root and index in the receipt.
func v2Demanded(receipt *types.Receipt, vault common.Address, seq, root, index uint64) bool {
	for _, log := range receipt.Logs {
		if log.Address != vault {
			continue
		}
		event, err := vault2.UnpackPathDemandedEvent(log)
		if err == nil && event.Seq == seq && event.RootIndex.IsUint64() && event.RootIndex.Uint64() == root && event.Index.IsUint64() && event.Index.Uint64() == index {
			return true
		}
	}
	return false
}
