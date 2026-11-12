//go:build linux || darwin

// The capture the flag audit (CAPTURETHEFLAG.md §8). Operator n's CTF pool is
// vault2 pool 2^32 + n, which settles like the provider pool: vault2 finalizes
// its root only with auditor approval. The settlement v2 audit therefore audits
// it beside the provider pool, with the same custody, objects, head bindings,
// decisions and auditor transactions, and runs ctf.Verify instead of
// ledger.Verify. Verify recomputes each covered epoch's census, draw, slot
// amounts, pots, pending awards and accounts from the published objects and
// these chain facts, read at the audit's pinned finalized block:
//
//   - the capture the flag entry in force for the operator at the pending root's
//     epoch (the signed settlement amendments, with the configured fallback),
//     which names the carry contract; every entry in force at a covered epoch
//     must name the same one, as one root is never audited against two;
//   - the carry contract, which must serve vault2's pool 2^32 + n: its coldkey
//     and recycledFor(k);
//   - for every covered epoch, ctfCensus(n, k) and, for a committed census, the
//     EVM timestamp of its block B_k and the drand quicknet pulse of ρ_k from a
//     relay, which is not trusted: the pulse must verify against quicknet's
//     key. Every committed census is drawn as committed (§5, §8 check 1), an
//     epoch before E_c's too.
//
// It approves only a root that reproduces exactly, and challenges, demands or
// refuses as for the provider pool (VERSION2-FINAL.md §9.3). An input it cannot
// read or verify (no entry in force, unknown amendments, a pulse no relay
// serves) decides nothing and is tried again, so nothing is approved that the
// validator cannot rebuild. Through an outage of the published amendments the
// last verified file stands, as it does for steering: a later entry it misses
// can only name another carry contract, which the root's header then fails. A
// validator whose ctf setting is off audits no CTF pool. These are the facts
// `snclaim v2 check` reads.
//
// The draw leaves out the census's head-bound candidates, as the operator's
// must (ctf.Verify, ctf/doc.go), so no winner holds a fleet binding (§8 check
// 3). A census that lists one breaks P5 (§4.2), and the audit reports it
// (ledger_audit_ctf_census_bound), but judges the root as built: a committed
// census is write-once, so refusing for it would refuse every later root of the
// pool, and anyone who can make a client look bound could stall the pool. The
// watcher, which reads no bindings, refuses such a root wherever the bound
// client changes its draw.
package validator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/amendment"
	"github.com/urfoundation/sn/crv4"
	"github.com/urfoundation/sn/ctf"
	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/stabi"
)

// DefaultDrandURL is the drand relay `validator run` reads quicknet's pulses
// from without --drand_url.
const DefaultDrandURL = "https://api.drand.sh"

const (
	// settlementV2DrandTimeout bounds one relay's answer, and
	// settlementV2DrandMaxBytes its body: a pulse is about 250 bytes.
	settlementV2DrandTimeout  = 10 * time.Second
	settlementV2DrandMaxBytes = 4096
)

// settlementV2CtfPool is a capture the flag pool's operator.
type settlementV2CtfPool struct {
	noID uint64
}

// settlementV2CtfHeaderAccounts names the account chunks of a capture the
// flag root's header.
func settlementV2CtfHeaderAccounts(data []byte) ([]ledger.Ref, error) {
	header, err := ctf.DecodeHeader(data)
	if err != nil {
		return nil, err
	}
	return header.Accounts, nil
}

// errSettlementV2CtfNotInForce: no capture the flag entry is in force for the
// operator at the pending root's epoch, or the amendments are unknown, so the
// audit cannot rebuild the root and decides nothing.
var errSettlementV2CtfNotInForce = errors.New("capture the flag is not in force at the pending root's epoch, so its carry contract is unknown")

// settlementV2CtfReader reads what ctf.Verify takes beside vault2's state.
type settlementV2CtfReader interface {
	// ctfInput reads, at state's pinned block, operator noID's capture the flag
	// terms and the chain's facts of every covered epoch. Its Pending, Previous,
	// FundedThrough, sources and budget are the audit's to fill.
	ctfInput(ctx context.Context, state *settlementV2PoolState, noID uint64) (*ctf.VerifyInput, error)
}

// verifyCtf runs ctf.Verify on a capture the flag pool's pending root, with
// what is left of the root's byte budget, and records what it fetched.
func (self *settlementV2Audit) verifyCtf(ctx context.Context, pool settlementV2AuditPool, state *settlementV2PoolState, record *settlementV2AuditRecord) (*settlementV2Finding, error) {
	pending := &state.pending
	if self.ctf == nil {
		return nil, fmt.Errorf("capture the flag audit of pool %d seq %d: %w", state.noID, pending.Seq, errSettlementV2CtfNotInForce)
	}
	in, err := self.ctf.ctfInput(ctx, state, pool.ctf.noID)
	if err != nil {
		return nil, fmt.Errorf("capture the flag audit of pool %d seq %d: %w", state.noID, pending.Seq, err)
	}
	in.Pending, in.FundedThrough = settlementV2PendingRoot(pending), state.funded
	if previous := state.previous; previous != nil {
		root := settlementV2LedgerRoot(previous.Epoch, previous.Root, previous.Total, previous.LeafCount, previous.LedgerHash)
		in.Previous = &root
	}
	in.Fetcher, in.Custody, in.Heads, in.MaxFetchBytes = pool.objects, self.custody, self.reader.headOracle(state), self.remainingBytes(record)
	res, err := ctf.Verify(ctx, *in)
	if err != nil {
		return nil, fmt.Errorf("capture the flag audit of pool %d seq %d: %w", state.noID, pending.Seq, err)
	}
	record.FetchedBytes += res.FetchedBytes
	// A census that lists head-bound clients breaks P5. The draw left them out,
	// so the root is judged as built, and the breach is reported.
	for _, epoch := range res.Epochs {
		if len(epoch.Bound) > 0 {
			releaseDiagnostic(ctx, "operator", "ledger_audit_ctf_census_bound", 0, false, 0, releaseDiagnosticFacts{operatorId: state.noID, operatorKnown: true})
		}
	}
	return &settlementV2Finding{check: res.Check.String(), err: res.Err, missing: res.Missing, previous: res.Previous, previousAccounts: res.PreviousAccounts, faults: res.Faults, action: res.Action()}, nil
}

// settlementV2CtfChain is the production settlementV2CtfReader.
type settlementV2CtfChain struct {
	chain      *ChainClient
	settlement common.Address
	vault      common.Address
	chainID    uint64
	netuid     uint16
	amendments *releaseAmendments
	// drand are the relays quicknet's pulses are read from, in order.
	drand  []string
	client *http.Client
	// pulses holds verified pulses by round; a pulse never changes.
	pulses sync.Map
}

func newSettlementV2CtfChain(cfg *ReleaseConfig, chain *ChainClient, addresses SettlementV2Addresses, drand string) (*settlementV2CtfChain, error) {
	amendments, err := newReleaseAmendments(cfg)
	if err != nil {
		return nil, err
	}
	if drand == "" {
		drand = DefaultDrandURL
	}
	return &settlementV2CtfChain{chain: chain, settlement: addresses.Settlement, vault: addresses.Vault, chainID: cfg.ChainID, netuid: cfg.Netuid, amendments: amendments, drand: []string{drand},
		client: &http.Client{Timeout: settlementV2DrandTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (self *settlementV2CtfChain) ctfInput(ctx context.Context, state *settlementV2PoolState, noID uint64) (*ctf.VerifyInput, error) {
	self.amendments.refresh(ctx)
	poolID, ok := amendment.CtfPoolID(noID)
	if !ok || poolID != state.noID {
		return nil, fmt.Errorf("pool %d is not operator %d's capture the flag pool", state.noID, noID)
	}
	at := func(epoch uint64) (*amendment.Entry, error) {
		entries, err := self.amendments.captureTheFlagAt([]uint64{noID}, epoch)
		if err != nil || len(entries) == 0 {
			return nil, err
		}
		return entries[0], nil
	}
	pending := &state.pending
	entry, err := at(pending.Epoch)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, errSettlementV2CtfNotInForce
	}
	if entry.Ctf.CtfPool != poolID {
		return nil, fmt.Errorf("the capture the flag entry of operator %d names pool %d, not %d", noID, entry.Ctf.CtfPool, poolID)
	}
	carry := entry.Ctf.Carry
	view := func(to common.Address, metadata *bind.MetaData, method string, arguments ...any) ([]any, error) {
		values, err := self.chain.settlementV2ViewAtHashContext(ctx, to, metadata, method, arguments, state.block, state.blockHash)
		if err != nil {
			return nil, fmt.Errorf("%s %s at block %d: %w", to.Hex(), method, state.block, err)
		}
		return values, nil
	}
	vault, err := view(carry, &stabi.STCtfCarryMetaData, "vault")
	if err != nil {
		return nil, err
	}
	pool, err := view(carry, &stabi.STCtfCarryMetaData, "poolId")
	if err != nil {
		return nil, err
	}
	if vault[0].(common.Address) != self.vault || pool[0].(*big.Int).Cmp(new(big.Int).SetUint64(poolID)) != 0 {
		return nil, fmt.Errorf("carry contract %s serves vault %s pool %s, not vault2 %s pool %d", carry.Hex(), vault[0].(common.Address).Hex(), pool[0].(*big.Int), self.vault.Hex(), poolID)
	}
	coldkey, err := view(carry, &stabi.STCtfCarryMetaData, "coldkey")
	if err != nil {
		return nil, err
	}
	in := &ctf.VerifyInput{NoID: noID, ChainID: self.chainID, Netuid: self.netuid, FirstEpoch: state.firstEpoch, CarryColdkey: coldkey[0].([32]byte), Epochs: map[uint64]ctf.EpochFacts{}}
	p := state.firstEpoch - 1
	if state.previous != nil {
		p = state.previous.Epoch
	}
	// A range Verify refuses by its length alone is not read, as for the
	// funding (settlementV2AuditChain.poolState).
	if pending.Epoch <= p || pending.Epoch-p > ledger.MaxCoveredEpochs {
		return in, nil
	}
	for k := p + 1; k <= pending.Epoch; k++ {
		epoch := new(big.Int).SetUint64(k)
		recycled, err := view(carry, &stabi.STCtfCarryMetaData, "recycledFor", epoch)
		if err != nil {
			return nil, err
		}
		amount, overflow := uint256.FromBig(recycled[0].(*big.Int))
		if overflow {
			return nil, fmt.Errorf("carry contract %s recycledFor(%d) overflows", carry.Hex(), k)
		}
		facts := ctf.EpochFacts{Recycled: *amount}
		entry, err := at(k)
		if err != nil {
			return nil, err
		}
		if entry != nil && entry.Ctf.Carry != carry {
			return nil, fmt.Errorf("the capture the flag entries of operator %d name another carry contract at epoch %d than at epoch %d: one root cannot be audited against two", noID, k, pending.Epoch)
		}
		// Every committed census is drawn as committed, before E_c too.
		census, err := view(self.settlement, &stabi.STSettlement2MetaData, "ctfCensus", new(big.Int).SetUint64(noID), epoch)
		if err != nil {
			return nil, err
		}
		if hash, block := census[0].([32]byte), census[1].(uint64); hash != ([32]byte{}) {
			time, err := self.blockTime(ctx, block)
			if err != nil {
				return nil, fmt.Errorf("epoch %d census block %d: %w", k, block, err)
			}
			facts.Census = &ctf.CensusCommitment{Hash: hash, Block: block, Time: time}
			// The pulse is read for every committed census, as the watcher reads
			// it: one no relay serves decides nothing, and the audit tries again.
			if facts.Pulse, err = self.pulse(ctx, ctf.DrawRound(time)); err != nil {
				return nil, fmt.Errorf("epoch %d draw: %w", k, err)
			}
		}
		in.Epochs[k] = facts
	}
	return in, nil
}

// blockTime is block number's EVM timestamp, in unix seconds.
func (self *settlementV2CtfChain) blockTime(ctx context.Context, number uint64) (uint64, error) {
	var header *struct {
		Number    *hexutil.Uint64 `json:"number"`
		Timestamp *hexutil.Uint64 `json:"timestamp"`
	}
	err := self.chain.retryChainRead(ctx, func(callCtx context.Context) error {
		header = nil
		return self.chain.client.Client().CallContext(callCtx, &header, "eth_getBlockByNumber", hexutil.EncodeUint64(number), false)
	})
	if err != nil {
		return 0, err
	}
	if header == nil || header.Number == nil || uint64(*header.Number) != number || header.Timestamp == nil {
		return 0, errors.New("the RPC answered no block with that number and a timestamp")
	}
	return uint64(*header.Timestamp), nil
}

// pulse is quicknet's verified pulse of round, from the first relay that
// serves one that verifies.
func (self *settlementV2CtfChain) pulse(ctx context.Context, round uint64) (*ctf.Pulse, error) {
	if value, ok := self.pulses.Load(round); ok {
		return value.(*ctf.Pulse), nil
	}
	var problems []string
	for _, relay := range self.drand {
		pulse, err := settlementV2DrandPulse(ctx, self.client, relay, round)
		if err == nil {
			self.pulses.Store(round, pulse)
			return pulse, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		problems = append(problems, fmt.Sprintf("%s: %v", relay, err))
	}
	return nil, fmt.Errorf("no drand relay served a verified pulse of round %d: %s", round, strings.Join(problems, "; "))
}

// settlementV2DrandPulse reads quicknet's pulse of round from relay, GET
// <relay>/<quicknet chain hash>/public/<round>. The relay is trusted for
// nothing: the pulse must be of that round and verify against quicknet's key,
// and a randomness it states must be the signature's sha256.
func settlementV2DrandPulse(ctx context.Context, client *http.Client, relay string, round uint64) (*ctf.Pulse, error) {
	ctx, cancel := context.WithTimeout(ctx, settlementV2DrandTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(relay, "/")+"/"+crv4.QuicknetChainHash+"/public/"+strconv.FormatUint(round, 10), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, settlementV2DrandMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > settlementV2DrandMaxBytes {
		return nil, fmt.Errorf("more than %d bytes", settlementV2DrandMaxBytes)
	}
	var answer struct {
		Round      uint64 `json:"round"`
		Signature  string `json:"signature"`
		Randomness string `json:"randomness"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, err
	}
	signature, err := hex.DecodeString(answer.Signature)
	if err != nil || answer.Round != round {
		return nil, fmt.Errorf("%w: round %d answered as round %d with signature %q", ctf.ErrPulse, round, answer.Round, answer.Signature)
	}
	pulse := &ctf.Pulse{Round: round, Signature: signature}
	if err := pulse.Verify(); err != nil {
		return nil, err
	}
	if randomness := sha256.Sum256(signature); answer.Randomness != "" && answer.Randomness != hex.EncodeToString(randomness[:]) {
		return nil, fmt.Errorf("%w: round %d: randomness is not sha256 of the signature", ctf.ErrPulse, round)
	}
	return pulse, nil
}
