package onchain

// v2_ctf.go — `snclaim v2 check --all` of a capture the flag pool
// (CAPTURETHEFLAG.md §8, §10). Network operator n's CTF pool is the vault2
// pool 2^32 + n. Its roots, accounts, proofs and claims are a vault2 pool's
// like any other, so status, claim and the check of the accounts are the
// provider pool's; only the audit differs. Beside vault2's state, and at the
// same finalized block, the check reads what ctf.Verify takes:
//
//   - vault2's coordinator (STSettlement2) and netuid, which with the chain id
//     seed the draw;
//   - the signed settlement amendments (--amendments_url, verified against the
//     price sheet's pinned key), for the capture the flag entry in force at the
//     pending root's epoch: E_c, its first epoch, and its carry contract. An
//     entry in force at another covered epoch must name the same carry
//     contract, since one root is never audited against two;
//   - for each covered epoch, the census commitment ctfCensus(n, k) and, for a
//     committed census, the EVM timestamp of its block B_k and the drand
//     quicknet pulse of ρ_k from --drand_url, which is not trusted: the pulse
//     must verify against quicknet's key. A committed census is drawn as
//     committed, as §8's check 1 requires, also for an epoch before E_c;
//   - the carry contract's coldkey and recycledFor(k). The contract must serve
//     vault2's pool 2^32 + n;
//   - for each epoch the audit draws, the head bindings its draw leaves out,
//     from the v1 coordinator's FleetBound events and archive bindingAt reads
//     at the epoch's boundaries, as the validator reads them
//     (v2_ctf_heads.go). A census that lists a head-bound client, which P5
//     keeps out of every census, then draws here as it does for the
//     validator: the client is left out, and the check names it.
//
// The audit fails closed. When an input cannot be read or the audit cannot
// run, the check says so and never approves; it still protects the accounts
// of the latest finalized root from the published objects alone, as it does
// when those objects are missing, then fails. A pulse that does not verify,
// unknown amendments, capture the flag not in force at the pending root's
// epoch, a carry contract that is not the pool's, or head bindings an RPC
// without archive state cannot serve all end that way. A
// refused root that leaves nothing to challenge or demand fails the check
// too, unlike a provider pool's: a wrong draw or wrong amounts never fault an
// account, so only the auditors' refusal stops such a root, and the failed
// check is the watcher's alert.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/amendment"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ctf"
	"github.com/urfoundation/sn/ledger"
)

// v2AmendmentSigner is the deployment's pinned amendment signer; tests
// replace it, as the price sheet's readers replace theirs.
var v2AmendmentSigner = amendment.SignerFor

// v2PulseReplyLimit bounds a drand relay's answer: one pulse.
const v2PulseReplyLimit = 4 << 10

// v2CtfViews is the ABI of the capture the flag views the check calls:
// STSettlement2Ctf's ctfCensus, and STCtfCarry's coldkey, vault, poolId and
// recycledFor (CAPTURETHEFLAG.md §3.1, §4.6).
var v2CtfViews = sync.OnceValues(func() (abi.ABI, error) {
	return abi.JSON(strings.NewReader(`[
{"type":"function","name":"ctfCensus","stateMutability":"view","inputs":[{"name":"noId","type":"uint256"},{"name":"epoch","type":"uint256"}],"outputs":[{"name":"censusHash","type":"bytes32"},{"name":"blockNumber","type":"uint64"}]},
{"type":"function","name":"coldkey","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"bytes32"}]},
{"type":"function","name":"vault","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"address"}]},
{"type":"function","name":"poolId","stateMutability":"view","inputs":[],"outputs":[{"name":"","type":"uint256"}]},
{"type":"function","name":"recycledFor","stateMutability":"view","inputs":[{"name":"epoch","type":"uint256"}],"outputs":[{"name":"amount","type":"uint256"}]}
]`))
})

// v2CtfView calls a capture the flag view of contract at the reader's block
// and returns its outputs.
func v2CtfView(ctx context.Context, r *v2Reader, contract common.Address, name string, args ...any) ([]any, error) {
	views, err := v2CtfViews()
	if err != nil {
		return nil, err
	}
	data, err := views.Pack(name, args...)
	if err != nil {
		return nil, err
	}
	ret, err := ethCallAt(ctx, r.client, contract, data, r.block)
	if err != nil {
		return nil, err
	}
	return views.Unpack(name, ret)
}

// v2CtfSources are where the check reads what the chain does not hold.
type v2CtfSources struct {
	// amendments is the signed settlement amendments file's URL, and drand
	// a drand relay serving quicknet's pulses.
	amendments string
	drand      *url.URL
}

// v2CtfAudit is what the check read for a capture the flag pool's audit.
type v2CtfAudit struct {
	in *ctf.VerifyInput
	// entry is the capture the flag entry in force at the pending root's
	// epoch, and carry its carry contract.
	entry *amendment.Entry
	carry common.Address
	// heads reads the head bindings of the epochs the audit draws.
	heads *v2CtfHeads
}

// readV2Ctf reads, at the reader's block, what ctf.Verify takes for
// operator n's capture the flag pool besides vault2's state.
func readV2Ctf(ctx context.Context, r *v2Reader, sources v2CtfSources, chainID *big.Int, n uint64, state *v2State) (*v2CtfAudit, error) {
	poolID, _ := amendment.CtfPoolID(n)
	if !chainID.IsUint64() {
		return nil, fmt.Errorf("chain id %s is not a 64-bit integer", chainID)
	}
	coordinator, err := v2View(ctx, r, vault2.PackCoordinator(), vault2.UnpackCoordinator)
	if err != nil {
		return nil, fmt.Errorf("coordinator(): %w", err)
	}
	netuid, err := v2View(ctx, r, vault2.PackNetuid(), vault2.UnpackNetuid)
	if err != nil {
		return nil, fmt.Errorf("netuid(): %w", err)
	}
	file, err := readV2Amendments(ctx, sources.amendments, chainID.Uint64(), netuid)
	if err != nil {
		return nil, err
	}
	pending := &state.pending
	inForce, err := file.At(n, pending.Epoch, nil)
	if err != nil {
		return nil, err
	}
	entry := inForce.CaptureTheFlag
	if entry == nil || entry.Ctf == nil {
		return nil, fmt.Errorf("no signed amendment puts capture the flag in force for operator %d at epoch %d, the pending root's, so its carry contract is unknown", n, pending.Epoch)
	}
	if entry.Ctf.CtfPool != poolID {
		return nil, fmt.Errorf("the signed capture the flag entry of operator %d names pool %d, not %d", n, entry.Ctf.CtfPool, poolID)
	}
	audit := &v2CtfAudit{entry: entry, carry: entry.Ctf.Carry}
	if err := checkV2Carry(ctx, r, audit.carry, poolID); err != nil {
		return nil, err
	}
	out, err := v2CtfView(ctx, r, audit.carry, "coldkey")
	if err != nil {
		return nil, fmt.Errorf("carry contract %s coldkey(): %w", audit.carry.Hex(), err)
	}
	in := &ctf.VerifyInput{
		NoID: n, ChainID: chainID.Uint64(), Netuid: netuid, FirstEpoch: state.firstEpoch, CarryColdkey: out[0].([32]byte),
		Pending:       v2LedgerRoot(pending.Epoch, pending.Root, pending.Total, pending.LeafCount, pending.LedgerHash),
		FundedThrough: state.funded, Epochs: map[uint64]ctf.EpochFacts{}, Custody: ledger.NewMemoryCustody(),
		MaxFetchBytes: ledger.DefaultMaxFetchBytes,
	}
	audit.in = in
	p := state.firstEpoch - 1
	if state.latest != nil {
		p = state.latest.Epoch
	}
	// A range Verify refuses by its length alone is not read, as for the
	// funding (readV2State).
	if pending.Epoch <= p || pending.Epoch-p > ledger.MaxCoveredEpochs {
		return audit, nil
	}
	for k := p + 1; k <= pending.Epoch; k++ {
		facts, err := readV2CtfEpoch(ctx, r, sources, file, audit, coordinator, n, k)
		if err != nil {
			return nil, err
		}
		in.Epochs[k] = *facts
	}
	// Each draw leaves out the head-bound candidates the validator's leaves
	// out (v2_ctf_heads.go).
	audit.heads = newV2CtfHeads(r, coordinator)
	in.Heads = audit.heads
	return audit, nil
}

// readV2Amendments reads the signed settlement amendments of the
// deployment: the published file, verified against the pinned signer, or
// with no pinned signer the unpublished file, which puts nothing in force.
// An unreadable or unverified file is an error: what is in force is then
// unknown, and configuration does not stand in for it.
func readV2Amendments(ctx context.Context, address string, chainID uint64, netuid uint16) (*amendment.File, error) {
	signer, pinned := v2AmendmentSigner(chainID, netuid)
	if !pinned {
		return amendment.Unpublished(chainID, netuid)
	}
	file, problem := amendment.NewSource(address, chainID, netuid, signer, nil).Refresh(ctx)
	if file == nil {
		return nil, fmt.Errorf("the signed amendments at %s: %v; %w", address, problem, amendment.ErrUnknown)
	}
	return file, nil
}

// checkV2Carry requires the carry contract to serve vault2's pool poolID.
func checkV2Carry(ctx context.Context, r *v2Reader, carry common.Address, poolID uint64) error {
	vault, err := v2CtfView(ctx, r, carry, "vault")
	if err != nil {
		return fmt.Errorf("carry contract %s vault(): %w", carry.Hex(), err)
	}
	pool, err := v2CtfView(ctx, r, carry, "poolId")
	if err != nil {
		return fmt.Errorf("carry contract %s poolId(): %w", carry.Hex(), err)
	}
	if vault[0].(common.Address) != r.vault || pool[0].(*big.Int).Cmp(new(big.Int).SetUint64(poolID)) != 0 {
		return fmt.Errorf("carry contract %s serves vault %s pool %s, not vault2 %s pool %d", carry.Hex(), vault[0].(common.Address).Hex(), pool[0].(*big.Int), r.vault.Hex(), poolID)
	}
	return nil
}

// readV2CtfEpoch reads covered epoch k's facts: recycledFor(k), its census
// commitment and, for a committed census, the timestamp of the commitment's
// block and the pulse of its draw round. A capture the flag entry in force
// at k must name the pending root's carry contract.
func readV2CtfEpoch(ctx context.Context, r *v2Reader, sources v2CtfSources, file *amendment.File, audit *v2CtfAudit, coordinator common.Address, n, k uint64) (*ctf.EpochFacts, error) {
	inForce, err := file.At(n, k, nil)
	if err != nil {
		return nil, err
	}
	if entry := inForce.CaptureTheFlag; entry != nil && (entry.Ctf == nil || entry.Ctf.Carry != audit.carry) {
		return nil, fmt.Errorf("the signed capture the flag entries of operator %d name another carry contract at epoch %d than at epoch %d: one root cannot be audited against two", n, k, audit.in.Pending.Epoch)
	}
	epoch := new(big.Int).SetUint64(k)
	out, err := v2CtfView(ctx, r, audit.carry, "recycledFor", epoch)
	if err != nil {
		return nil, fmt.Errorf("carry contract %s recycledFor(%d): %w", audit.carry.Hex(), k, err)
	}
	// ABI uint256 values always fit.
	facts := &ctf.EpochFacts{Recycled: *uint256.MustFromBig(out[0].(*big.Int))}
	out, err = v2CtfView(ctx, r, coordinator, "ctfCensus", new(big.Int).SetUint64(n), epoch)
	if err != nil {
		return nil, fmt.Errorf("coordinator %s ctfCensus(%d,%d): %w", coordinator.Hex(), n, k, err)
	}
	hash, block := out[0].([32]byte), out[1].(uint64)
	if hash == ([32]byte{}) {
		return facts, nil
	}
	time, err := v2BlockTime(ctx, r.client, block)
	if err != nil {
		return nil, fmt.Errorf("epoch %d census block %d: %w", k, block, err)
	}
	facts.Census = &ctf.CensusCommitment{Hash: hash, Block: block, Time: time}
	if facts.Pulse, err = v2DrandPulse(ctx, sources.drand, ctf.DrawRound(time)); err != nil {
		return nil, fmt.Errorf("epoch %d draw: %w", k, err)
	}
	return facts, nil
}

// v2BlockTime reads block number's EVM timestamp, in unix seconds.
func v2BlockTime(ctx context.Context, client *ethclient.Client, number uint64) (uint64, error) {
	type block struct {
		Number    *hexutil.Uint64 `json:"number"`
		Timestamp *hexutil.Uint64 `json:"timestamp"`
	}
	read, err := retryOnchainRead(ctx, func(ctx context.Context) (*block, error) {
		cctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		var read *block
		err := client.Client().CallContext(cctx, &read, "eth_getBlockByNumber", hexutil.EncodeUint64(number), false)
		return read, err
	})
	if err != nil {
		return 0, err
	}
	if read == nil || read.Number == nil || uint64(*read.Number) != number || read.Timestamp == nil {
		return 0, errors.New("the RPC answered no block with that number and a timestamp")
	}
	return uint64(*read.Timestamp), nil
}

// v2DrandPulse reads quicknet's pulse of round from the relay at relay,
// GET <relay>/<quicknet chain hash>/public/<round>, within the time of a
// chain call. The relay is not trusted: the pulse must be of that round and
// verify against quicknet's key.
func v2DrandPulse(ctx context.Context, relay *url.URL, round uint64) (*ctf.Pulse, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	u := relay.JoinPath(crv4.QuicknetChainHash, "public", strconv.FormatUint(round, 10))
	body, err := v2Get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var reply struct {
		Round     uint64 `json:"round"`
		Signature string `json:"signature"`
	}
	if err := json.NewDecoder(io.LimitReader(body, v2PulseReplyLimit)).Decode(&reply); err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	signature, err := hex.DecodeString(reply.Signature)
	if err != nil || reply.Round != round {
		return nil, fmt.Errorf("GET %s answered round %d with signature %q", u, reply.Round, reply.Signature)
	}
	pulse := &ctf.Pulse{Round: round, Signature: signature}
	if err := pulse.Verify(); err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	return pulse, nil
}

// v2CtfAccounts names the account chunks of a capture the flag root's
// header.
func v2CtfAccounts(data []byte) ([]ledger.Ref, error) {
	header, err := ctf.DecodeHeader(data)
	if err != nil {
		return nil, err
	}
	return header.Accounts, nil
}

// reportV2Ctf prints the terms of the audit: the entry in force and the
// carry contract.
func reportV2Ctf(audit *v2CtfAudit) {
	entry := audit.entry
	fmt.Printf("  capture the flag:  operator %d, from epoch %d by the signed amendment entry, carry contract %s\n", entry.NoID, entry.FromEpoch, audit.carry.Hex())
	fmt.Printf("                     carry coldkey 0x%x\n", audit.in.CarryColdkey)
}

// reportV2CtfHeads prints where an audit that drew read its head bindings.
func reportV2CtfHeads(h *v2CtfHeads) {
	if h != nil && h.scanned {
		fmt.Printf("  head bindings:     %d FleetBound events of v1 coordinator %s from block %d\n", len(h.bindings), h.coordinator.Hex(), h.from)
	}
}

// reportV2CtfAudit prints each epoch the audit recomputed, then the audit
// and the objects it could not obtain. Amounts are rao.
func reportV2CtfAudit(res *ctf.Result) {
	for _, e := range res.Epochs {
		label := fmt.Sprintf("  epoch %d:", e.Epoch)
		switch {
		case e.Census == nil:
			fmt.Printf("%-21sno census committed: no draw\n", label)
		case !e.Drawn:
			fmt.Printf("%-21scensus %v at block %d is not canonical: no draw (%v)\n", label, *e.Census, e.CensusBlock, e.CensusErr)
		default:
			fmt.Printf("%-21scensus %v at block %d, %d candidates; drand round %d, %d winners\n", label, *e.Census, e.CensusBlock, e.Candidates, e.Round, len(e.Awards))
		}
		// A census that lists a head-bound client breaks P5 (§4.2); the draw
		// left it out, as the validator's does (§8 check 3).
		if len(e.Bound) > 0 {
			fmt.Printf("                     left out %d head-bound candidates:", len(e.Bound))
			for _, c := range e.Bound {
				fmt.Printf(" 0x%x (%v, %v)", c.ClientID, c.Role, c.Country)
			}
			fmt.Println()
		}
		matured := "not matured"
		if e.Matured {
			matured = "matured"
		}
		fmt.Printf("                     funding %s, returned %s of %s owed (%s), fresh %s, slot %s rao\n", e.Funding.Dec(), e.Returned.Dec(), e.Owed.Dec(), matured, e.Fresh.Dec(), e.SlotBase.Dec())
		paid := e.Paid()
		fmt.Printf("                     paid %s, pending %s, carried %s rao\n", paid.Dec(), e.PendingTotal.Dec(), e.Carried.Dec())
	}
	if res.Err == nil {
		fmt.Printf("  audit:             %v\n", res.Action())
	} else {
		fmt.Printf("  audit:             %v (%v check: %v)\n", res.Action(), res.Check, res.Err)
	}
	for _, ref := range res.Missing {
		fmt.Printf("  object missing:    %v\n", ref)
	}
}

// v2CheckCtf audits the pending root of operator n's capture the flag pool,
// vault2 pool poolID, and returns what it shows of the accounts of the
// latest finalized root, nil for the pool's first root. failed is an audit
// that could not run, whose accounts are then assessed from the published
// objects alone, or one that refused the root with nothing to challenge or
// demand: the check acts on the accounts all the same, then fails.
func v2CheckCtf(ctx context.Context, r *v2Reader, objects ledger.Fetcher, sources v2CtfSources, chainID *big.Int, n, poolID uint64, state *v2State, previous *ledger.Root) (a *v2Assessment, failed error, err error) {
	audit, unavailable := readV2Ctf(ctx, r, sources, chainID, n, state)
	var res *ctf.Result
	if unavailable == nil {
		reportV2Ctf(audit)
		audit.in.Previous, audit.in.Fetcher = previous, objects
		res, unavailable = ctf.Verify(ctx, *audit.in)
		reportV2CtfHeads(audit.heads)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}
	if unavailable != nil {
		failed = fmt.Errorf("the capture the flag audit could not run: %w", unavailable)
		fmt.Printf("  audit:             unavailable: %v\n", failed)
		if state.latest == nil {
			return nil, failed, nil
		}
		a, err = v2AccountsOnly(ctx, r, objects, poolID, state)
		return a, failed, err
	}
	reportV2CtfAudit(res)
	// A refusal leaves nothing to act on when the previous root's accounts
	// were read and show no fault the watcher can prove or demand; without
	// them the fallback below decides.
	if res.Err != nil && res.Action() == ledger.ActionRefuse && (state.latest == nil || res.Previous != nil) {
		failed = fmt.Errorf("the capture the flag audit refuses pending root seq %d (%v check: %w)", state.pending.Seq, res.Check, res.Err)
	}
	if state.latest == nil {
		return nil, failed, nil
	}
	a = &v2Assessment{
		protected: v2Protected{index: state.latestIndex, accounts: res.PreviousAccounts, tree: res.Previous},
		tree:      res.Tree, reproduced: res.Reproduced, faults: res.Faults, action: res.Action(),
	}
	if res.Previous == nil {
		a, err = v2Fallback(ctx, r, objects, poolID, state, v2CtfAccounts)
	}
	return a, failed, err
}

// v2AccountsOnly assesses the pending root's published accounts against the
// latest finalized root, or when its objects are missing or do not rebuild
// it, the newest older root whose objects do (v2Fallback): for an audit that
// could not run, the accounts are still protected.
func v2AccountsOnly(ctx context.Context, r *v2Reader, objects ledger.Fetcher, poolID uint64, state *v2State) (*v2Assessment, error) {
	latest := state.latest
	accounts, tree, err := v2ReadAccounts(ctx, objects, latest.LedgerHash, v2CtfAccounts)
	if err == nil && !v2Rebuilds(tree, latest.Root, latest.Total, latest.LeafCount) {
		err = errors.New("its published accounts do not rebuild it")
	}
	if err != nil {
		fmt.Printf("  fallback:          finalized root %d is unverifiable: %v\n", state.latestIndex, err)
		return v2Fallback(ctx, r, objects, poolID, state, v2CtfAccounts)
	}
	return v2AssessPending(ctx, objects, state, v2CtfAccounts, &v2Assessment{protected: v2Protected{index: state.latestIndex, accounts: accounts, tree: tree}, action: ledger.ActionRefuse})
}
