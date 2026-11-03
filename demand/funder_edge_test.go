package demand

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/demand/bridge"
	"github.com/urfoundation/sn/demand/jupiter"
	"github.com/urfoundation/sn/demand/solana"
	"github.com/urfoundation/sn/ss58"
)

// rig is the world with the faults it does not have. Every hook is off until a
// test sets it, and the world's own fields still drive everything else.
type rig struct {
	*world

	// Bittensor
	answer     func(to common.Address, data []byte) (raw []byte, handled bool, err error)
	failNative error
	failNonce  error
	settle     func() // earlier sends finishing, at the first read a prepare makes
	preparing  bool
	chain      func(operation string, to common.Address, data []byte) error

	// Solana
	lag           map[solana.PublicKey]*laggedRead
	lagSimulation bool
	failToken     func(address solana.PublicKey) error
	noToken       func(address solana.PublicKey) bool
	failStatuses  error
	failHeight    error
	editSim       func(sim *solana.Simulation)
	editAccount   func(address solana.PublicKey, account *solana.Account) *solana.Account
	simulations   int
	broadcasts    []string
	hashes        uint64

	// Jupiter and Coinbase
	payer         *otherPayer
	failOrder     func(call int) error
	editOrder     func(order *jupiter.Order, req jupiter.OrderRequest)
	beforeExecute func()
	afterExecute  func()
	expireAt      time.Time // of the last RFQ order
	orderCalls    int
	executes      int
	executed      []string
	failAsk       error

	// LayerZero Scan
	scanStatus string
	scanErr    error

	// Store
	saveCalls   int
	loseSaveAt  int
	loseWhen    func(*State) bool
	failRecord  error
	panicRecord bool

	// Clock and log
	sleeps       int
	frozenHeight bool
	realSleep    bool
	logs         []string
}

// laggedRead is a token account as a node behind the chain still shows it.
type laggedRead struct {
	amount uint64
	reads  int // how many reads show it; untilTimePasses keeps it until a sleep
}

const untilTimePasses = -1

// otherPayer builds a taker's order in the shape of every order sampled from
// Jupiter's RFQ route. A maker pays and signs first, inside /execute, so its slot is
// empty until then and the wallet signs second. The order has no last valid height,
// only expireAt, unix seconds as a string, 52 to 54 seconds ahead. And it fills at its
// quote: no slippage, and a threshold equal to its output. The real client decodes
// it, so that the fake cannot hold a field Jupiter does not send. Metis and OKX orders
// do carry a height, and those are the world's own.
type otherPayer struct {
	gasless         bool
	walletNotSigner bool
}

var rfqMaker = solana.PublicKey{0x9a, 0x9a, 0x9a}

const (
	rfqLifetime = 53 * time.Second
	// How long after expireAt a swap has surely stopped landing, by any grace.
	rfqSettled = 10 * time.Minute
)

// chainMinStakeRao is the chain's own minimum, as mainnet answers it: below this a
// stake, sent or dry run, reverts AmountTooLow before the hotkey is looked at. It is
// the fake's number and not minStakeRao, which is the funder's choice of what to send.
const chainMinStakeRao = 2_001_007

var errAmountTooLow = errors.New("dispatch execution failed: AmountTooLow")

func belowChainMinimum(data []byte) bool {
	return new(big.Int).SetBytes(data[36:68]).Cmp(big.NewInt(chainMinStakeRao)) < 0
}

func newRig(t *testing.T, conversion string) *rig {
	t.Helper()
	return &rig{world: newWorld(t, conversion), lag: map[solana.PublicKey]*laggedRead{}}
}

func (r *rig) funder(t *testing.T) *Funder {
	t.Helper()
	deps := Deps{
		Store: r, EVM: r, Sender: r, Solana: rigSolana{worldSolana{r.world}, r}, Jupiter: r, Market: r, Scan: r,
		Logf: r.logf,
		Now:  func() time.Time { return r.clock },
	}
	if !r.realSleep {
		deps.Sleep = r.sleep
	}
	f, err := NewFunder(r.cfg, testNetuid, deps)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (r *rig) logf(format string, args ...any) {
	r.logs = append(r.logs, fmt.Sprintf(format, args...))
}

func (r *rig) logged(text string) bool {
	return strings.Contains(strings.Join(r.logs, "\n"), text)
}

func (r *rig) sleep(ctx context.Context, d time.Duration) error {
	r.sleeps++
	for address, lagged := range r.lag {
		if lagged.reads == untilTimePasses {
			delete(r.lag, address)
		}
	}
	height := r.height
	err := r.world.sleep(ctx, d)
	if r.frozenHeight {
		r.height = height
	}
	return err
}

// Bittensor

// settled runs settle ahead of what a prepare reads. A call that only waits for
// earlier sends reads nothing and names nothing, and is no send of the wallet's.
func (r *rig) settled() {
	if r.preparing {
		r.preparing = false
		if r.settle != nil {
			r.settle()
		}
	}
}

func (r *rig) Call(ctx context.Context, to common.Address, data []byte) ([]byte, error) {
	r.settled()
	if r.answer != nil {
		if raw, handled, err := r.answer(to, data); handled {
			return raw, err
		}
	}
	if to == StakingPrecompile && bytes.HasPrefix(data, addStakeLimitSelector) && belowChainMinimum(data) {
		return nil, errAmountTooLow
	}
	return r.world.Call(ctx, to, data)
}

func (r *rig) failRead(callSelector []byte, err error) {
	r.answer = func(_ common.Address, data []byte) ([]byte, bool, error) {
		if bytes.HasPrefix(data, callSelector) {
			return nil, true, err
		}
		return nil, false, nil
	}
}

func (r *rig) Balance(ctx context.Context, account common.Address) (*big.Int, error) {
	r.settled()
	if r.failNative != nil {
		return nil, r.failNative
	}
	return r.world.Balance(ctx, account)
}

func (r *rig) PendingNonce(ctx context.Context, account common.Address) (uint64, error) {
	r.settled()
	if r.failNonce != nil {
		return 0, r.failNonce
	}
	return r.world.PendingNonce(ctx, account)
}

func (r *rig) SendPrepared(ctx context.Context, prepare Prepare) (string, error) {
	return r.world.SendPrepared(ctx, func(ctx context.Context) (string, common.Address, []byte, error) {
		r.preparing = true
		operation, to, data, err := prepare(ctx)
		r.preparing = false
		if err == nil && operation != "" && r.chain != nil {
			err = r.chain(operation, to, data)
		}
		if err == nil && operation != "" && to == StakingPrecompile && belowChainMinimum(data) {
			err = errAmountTooLow
		}
		return operation, to, data, err
	})
}

// Solana

// rigSolana is the rig as Solana sees it.
type rigSolana struct {
	worldSolana
	r *rig
}

func (s rigSolana) TokenAccount(ctx context.Context, address solana.PublicKey) (*solana.TokenAccount, error) {
	r := s.r
	if r.failToken != nil {
		if err := r.failToken(address); err != nil {
			return nil, err
		}
	}
	if r.noToken != nil && r.noToken(address) {
		return nil, nil
	}
	account, err := r.world.TokenAccount(ctx, address)
	if err != nil {
		return nil, err
	}
	if lagged := r.lag[address]; lagged != nil {
		account.Amount = lagged.amount
		if lagged.reads > 0 {
			if lagged.reads--; lagged.reads == 0 {
				delete(r.lag, address)
			}
		}
	}
	return account, nil
}

func (s rigSolana) Account(ctx context.Context, address solana.PublicKey) (*solana.Account, error) {
	account, err := s.r.world.Account(ctx, address)
	if err == nil && s.r.editAccount != nil {
		account = s.r.editAccount(address, account)
	}
	return account, err
}

func (s rigSolana) Simulate(ctx context.Context, raw []byte, accounts []solana.PublicKey) (*solana.Simulation, error) {
	r := s.r
	r.simulations++
	if r.lagSimulation {
		usdc, tao := r.usdc, r.tao
		if lagged := r.lag[r.usdcAccount]; lagged != nil {
			r.usdc = lagged.amount
		}
		if lagged := r.lag[r.taoAccount]; lagged != nil {
			r.tao = lagged.amount
		}
		defer func() { r.usdc, r.tao = usdc, tao }()
	}
	sim, err := r.world.Simulate(ctx, raw, accounts)
	if err == nil && r.editSim != nil {
		r.editSim(sim)
	}
	return sim, err
}

func (s rigSolana) SignatureStatuses(ctx context.Context, signatures []string) ([]*solana.SignatureStatus, error) {
	if s.r.failStatuses != nil {
		return nil, s.r.failStatuses
	}
	return s.r.world.SignatureStatuses(ctx, signatures)
}

func (s rigSolana) BlockHeight(ctx context.Context) (uint64, error) {
	if s.r.failHeight != nil {
		return 0, s.r.failHeight
	}
	return s.r.world.BlockHeight(ctx)
}

// LatestBlockhash moves on with each call, as the chain's does between sends, so
// that a new bridge send has its own bytes and signature.
func (s rigSolana) LatestBlockhash(context.Context) (solana.Blockhash, error) {
	s.r.hashes++
	var hash [32]byte
	binary.LittleEndian.PutUint64(hash[:], s.r.hashes)
	return solana.Blockhash{Hash: hash, LastValidBlockHeight: s.r.height + 150}, nil
}

func (s rigSolana) SendTransaction(ctx context.Context, raw []byte) (string, error) {
	s.r.broadcasts = append(s.r.broadcasts, base64.StdEncoding.EncodeToString(raw))
	return s.r.world.SendTransaction(ctx, raw)
}

// Jupiter, Coinbase and Scan

func (r *rig) Order(ctx context.Context, req jupiter.OrderRequest) (*jupiter.Order, error) {
	r.orderCalls++
	if r.failOrder != nil {
		if err := r.failOrder(r.orderCalls); err != nil {
			return nil, err
		}
	}
	order, err := r.world.Order(ctx, req)
	if err != nil {
		return nil, err
	}
	if r.payer != nil && req.Taker != "" {
		if order, err = r.rfqOrder(ctx, req, order); err != nil {
			return nil, err
		}
	}
	if r.editOrder != nil {
		r.editOrder(order, req)
	}
	return order, nil
}

// answerFromMemory is an HTTP transport that touches no network: it answers with body.
type answerFromMemory []byte

func (body answerFromMemory) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}, nil
}

// rfqOrder is /order's answer on the RFQ route, for the amounts of quote.
func (r *rig) rfqOrder(ctx context.Context, req jupiter.OrderRequest, quote *jupiter.Order) (*jupiter.Order, error) {
	instruction := solana.Instruction{
		Program:  solana.MustPublicKey("JUP6LkbZbjS1jKKwapdHNy74zcZ3tLUZoi5QNyVTaV4"),
		Accounts: []solana.AccountMeta{{Key: r.cfg.USDCAddress, Signer: !r.payer.walletNotSigner, Writable: true}},
		Data:     binary.LittleEndian.AppendUint64(nil, uint64(len(r.orders))),
	}
	tx, err := solana.CompileV0(rfqMaker, []solana.Instruction{instruction}, [32]byte{2}, nil)
	if err != nil {
		return nil, err
	}
	r.expireAt = r.clock.Add(rfqLifetime)
	body, err := json.Marshal(map[string]any{
		"requestId":            "rfq-request",
		"swapType":             "rfq",
		"router":               "jupiterz",
		"inAmount":             strconv.FormatUint(quote.InAmount, 10),
		"outAmount":            strconv.FormatUint(quote.OutAmount, 10),
		"otherAmountThreshold": strconv.FormatUint(quote.OutAmount, 10),
		"slippageBps":          0,
		"gasless":              r.payer.gasless,
		"expireAt":             strconv.FormatInt(r.expireAt.Unix(), 10),
		"transaction":          base64.StdEncoding.EncodeToString(tx.Serialize()),
	})
	if err != nil {
		return nil, err
	}
	client := jupiter.NewClient("", jupiter.WithBaseURL("http://rfq.invalid"), jupiter.WithHTTPClient(&http.Client{Transport: answerFromMemory(body)}))
	order, err := client.Order(ctx, req)
	if err != nil {
		return nil, err
	}
	r.orders[string(tx.Message)] = &fakeOrder{amount: order.InAmount, out: order.OutAmount, raw: order.Transaction}
	return order, nil
}

// signAsMaker fills the maker's slot, which only /execute can. The fake's signature
// need only be unique, so no key is involved.
func signAsMaker(tx *solana.Transaction) {
	if tx.Signatures[0] == ([64]byte{}) {
		tx.Signatures[0] = sha512.Sum512(tx.Message)
	}
}

func (r *rig) Execute(ctx context.Context, signed []byte, requestID string) (*jupiter.ExecuteResult, error) {
	r.executes++
	r.executed = append(r.executed, base64.StdEncoding.EncodeToString(signed))
	if r.beforeExecute != nil {
		r.beforeExecute()
	}
	var result *jupiter.ExecuteResult
	var err error
	if r.payer != nil {
		result, err = r.executeOtherPayer(signed)
	} else {
		result, err = r.world.Execute(ctx, signed, requestID)
	}
	if r.afterExecute != nil {
		r.afterExecute()
	}
	return result, err
}

// executeOtherPayer is /execute for a transaction another account pays for: the
// payer's signature, and so the transaction's ID, exists only from here on.
func (r *rig) executeOtherPayer(signed []byte) (*jupiter.ExecuteResult, error) {
	tx, err := solana.ParseTransaction(signed)
	if err != nil {
		return nil, err
	}
	if !tx.SignedBy(r.cfg.USDCAddress) {
		r.t.Fatal("the swap is not signed by the wallet")
	}
	if tx.Signatures[0] != ([64]byte{}) {
		r.t.Fatal("the payer's slot is already filled")
	}
	signAsMaker(tx)
	signature := tx.ID()
	if r.executeCode != 0 {
		return &jupiter.ExecuteResult{Status: "Failed", Code: r.executeCode, Signature: signature}, nil
	}
	if r.executeDrop {
		return nil, errors.New("the swap never reached the chain")
	}
	order := r.orders[string(tx.Message)]
	if order == nil {
		r.t.Fatal("executed an unknown transaction")
	}
	r.swaps = append(r.swaps, order)
	r.usdc -= order.amount
	r.tao += order.out
	r.statuses[signature] = &solana.SignatureStatus{ConfirmationStatus: "confirmed"}
	if r.executeLost {
		return nil, errors.New("the reply was lost")
	}
	return &jupiter.ExecuteResult{Status: "Success", Signature: signature}, nil
}

func (r *rig) BestAsk(ctx context.Context) (float64, error) {
	if r.failAsk != nil {
		return 0, r.failAsk
	}
	return r.world.BestAsk(ctx)
}

func (r *rig) MessageStatus(ctx context.Context, sourceTx string) (string, string, error) {
	if r.scanErr != nil {
		return "", "", r.scanErr
	}
	if r.scanStatus != "" {
		return r.scanStatus, "", nil
	}
	return r.world.MessageStatus(ctx, sourceTx)
}

// Store

func (r *rig) Save(ctx context.Context, state *State, version uint64) (uint64, error) {
	r.saveCalls++
	if r.saveCalls == r.loseSaveAt || (r.loseWhen != nil && r.loseWhen(state)) {
		r.loseWhen = nil
		return 0, fmt.Errorf("%w: version %d", ErrStale, version)
	}
	return r.world.Save(ctx, state, version)
}

// loseSave makes the nth save from now lose to another writer.
func (r *rig) loseSave(nth int) { r.loseSaveAt = r.saveCalls + nth }

// loseSaveClearingSwap makes the save that drops a swap's journal lose.
func (r *rig) loseSaveClearingSwap() {
	r.loseWhen = func(state *State) bool { return state.Swap == nil }
}

// loseSaveClearingBridge makes the save that drops a bridge send's journal lose.
func (r *rig) loseSaveClearingBridge() {
	r.loseWhen = func(state *State) bool { return state.Bridge == nil }
}

// loseSaveAfterExecute makes the first save after /execute answers lose, which holds
// the only copy of what it answered.
func (r *rig) loseSaveAfterExecute() {
	armed := false
	r.afterExecute = func() {
		if !armed {
			armed = true
			r.loseSave(1)
		}
	}
}

func (r *rig) Record(ctx context.Context, event Event) error {
	if r.panicRecord {
		panic("the ledger's connection died")
	}
	if r.failRecord != nil {
		return r.failRecord
	}
	return r.world.Record(ctx, event)
}

// What a test reads and arranges

func (r *rig) current() *State {
	r.t.Helper()
	state, _, err := r.Load(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	return state
}

func (r *rig) seed(state *State) {
	r.t.Helper()
	raw, err := MarshalState(state)
	if err != nil {
		r.t.Fatal(err)
	}
	r.stateJSON, r.version = raw, r.version+1
}

func (r *rig) count(kind string) int {
	n := 0
	for _, event := range r.events {
		if event.Kind == kind {
			n++
		}
	}
	return n
}

func (r *rig) last(kind string) *Event {
	r.t.Helper()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].Kind == kind {
			return &r.events[i]
		}
	}
	r.t.Fatalf("no %s event in %s", kind, r.kinds())
	return nil
}

func (r *rig) step(f *Funder, request Request) string {
	r.t.Helper()
	result, wait, err := f.step(context.Background(), request)
	if err != nil {
		r.t.Fatal(err)
	}
	if result != nil {
		r.t.Fatalf("the step finished: %+v", result)
	}
	return wait
}

func (r *rig) ensureFunded(f *Funder, request Request) *Result {
	r.t.Helper()
	result, err := f.Ensure(context.Background(), request)
	if err != nil {
		r.t.Fatal(err)
	}
	if !result.Funded || result.AlphaRao.Cmp(request.NeedRao) < 0 {
		r.t.Fatalf("not funded: %+v (steps %s)", result, r.kinds())
	}
	return result
}

// lagAt makes reads of the accounts show what they hold now.
func (r *rig) lagAt(reads int, accounts ...solana.PublicKey) {
	for _, account := range accounts {
		amount := r.usdc
		if account == r.taoAccount {
			amount = r.tao
		}
		r.lag[account] = &laggedRead{amount: amount, reads: reads}
	}
}

// lagFromFirstExecute keeps the accounts' reads behind the first swap's landing.
func (r *rig) lagFromFirstExecute(reads int, accounts ...solana.PublicKey) {
	armed := false
	r.beforeExecute = func() {
		if !armed {
			armed = true
			r.lagAt(reads, accounts...)
		}
	}
}

// landSwap is a journaled swap reaching the chain late, whatever the funder thinks
// of it: its USDC leaves and its TAO arrives.
func (r *rig) landSwap(rec *SwapRecord) {
	r.t.Helper()
	raw, err := base64.StdEncoding.DecodeString(rec.Raw)
	if err != nil {
		r.t.Fatal(err)
	}
	tx, err := solana.ParseTransaction(raw)
	if err != nil {
		r.t.Fatal(err)
	}
	order := r.orders[string(tx.Message)]
	if order == nil {
		r.t.Fatal("landed an unknown swap")
	}
	r.swaps = append(r.swaps, order)
	r.usdc -= order.amount
	r.tao += order.out
	if tx.FeePayer() == r.cfg.USDCAddress {
		r.lamports -= 1_000_000
	}
	signAsMaker(tx)
	r.statuses[tx.ID()] = &solana.SignatureStatus{ConfirmationStatus: "confirmed"}
}

// moveTo brings the clock to at, and the chain along at the world's 40 blocks in 15 seconds.
func (r *rig) moveTo(at time.Time) {
	if passed := at.Sub(r.clock); passed > 0 {
		r.clock = at
		r.height += uint64(passed * 40 / (15 * time.Second))
	}
}

// expireRFQ moves on until the last RFQ order's swap has surely stopped landing.
func (r *rig) expireRFQ() { r.moveTo(r.expireAt.Add(rfqSettled)) }

func deposit(epoch uint64, a float64) Request { return Request{Epoch: epoch, NeedRao: alpha(a)} }

func raoWei(rao uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(rao), WeiPerRao)
}

func raisePrice(r *rig, percent int64) {
	r.price = new(big.Int).Quo(new(big.Int).Mul(r.price, big.NewInt(100+percent)), big.NewInt(100))
}

func failedOnChain() *solana.SignatureStatus {
	return &solana.SignatureStatus{Err: json.RawMessage(`{"InstructionError":[2,{"Custom":6001}]}`), ConfirmationStatus: "confirmed"}
}

func usdcOf(swaps []*fakeOrder) uint64 {
	total := uint64(0)
	for _, swap := range swaps {
		total += swap.amount
	}
	return total
}

func (r *rig) requireNoSwapJournal() {
	r.t.Helper()
	state := r.current()
	if r.executes != 0 || r.saves != 0 || len(r.swaps) != 0 || state.Swap != nil || state.SpentUSDCMicro != 0 {
		r.t.Fatalf("a refusal acted: %d executes, %d saves, %d swaps, state %+v", r.executes, r.saves, len(r.swaps), state)
	}
}

func (r *rig) requireNoBridgeJournal() {
	r.t.Helper()
	if len(r.broadcasts) != 0 || r.saves != 0 || len(r.sends) != 0 || r.current().Bridge != nil {
		r.t.Fatalf("a refusal acted: %d broadcasts, %d saves, %d sends, state %+v", len(r.broadcasts), r.saves, len(r.sends), r.current())
	}
}

// A swap another account pays for has no signature the wallet can compute. With
// /execute's reply lost, the balances decide once its order has expired: booked once,
// never bought again.
func TestAGaslessSwapWithALostReplyIsSettledByItsUSDC(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.payer = &otherPayer{gasless: true}
	r.executeLost = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	if wait := r.step(f, request); wait != "" {
		t.Fatal("the buying step waits on", wait)
	}
	journal := r.current().Swap
	if journal == nil || journal.Signature != "" || journal.SubmittedAt.IsZero() || len(r.swaps) != 1 {
		t.Fatalf("journal %+v after %d swaps", journal, len(r.swaps))
	}
	raw, err := base64.StdEncoding.DecodeString(journal.Raw)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := solana.ParseTransaction(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tx.FeePayer() != rfqMaker || tx.Signatures[0] != ([64]byte{}) || !tx.SignedBy(r.cfg.USDCAddress) {
		t.Fatal("the journal does not hold the wallet's signature over the maker's transaction")
	}
	r.expireRFQ()
	r.step(f, request)
	state := r.current()
	if state.Swap != nil || state.SpentUSDCMicro != journal.AmountMicro {
		t.Fatalf("state after the swap was settled: %+v", state)
	}
	if r.count("swapped") != 1 || r.count("swap_failed") != 0 {
		t.Fatal("steps", r.kinds())
	}
	if received := r.last("swapped").Data["tao_rao"]; received != r.swaps[0].out {
		t.Fatalf("booked %v rao of the %d received", received, r.swaps[0].out)
	}
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.executes != 1 || r.orderCalls != 3 || len(r.sends) != 1 {
		t.Fatalf("%d swaps, %d executes, %d orders, %d sends", len(r.swaps), r.executes, r.orderCalls, len(r.sends))
	}
}

// The same route with the swap never landing: USDC still there once its order has
// expired clears it, gives the budget back and buys once more.
func TestAGaslessSwapThatNeverLandedRetriesOnceAfterExpiry(t *testing.T) {
	r := newRig(t, enabled+"\n  max_usdc_per_epoch: 2000")
	r.usdc = 5_000 * microPerUSDC
	r.payer = &otherPayer{gasless: true}
	r.executeDrop = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	journal := r.current().Swap
	if journal == nil || journal.Signature != "" || len(r.swaps) != 0 {
		t.Fatalf("journal %+v after %d swaps", journal, len(r.swaps))
	}
	r.executeDrop = false
	r.expireRFQ()
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.executes != 2 || r.count("swap_failed") != 1 || r.count("swapped") != 1 {
		t.Fatalf("%d swaps, %d executes, steps %s", len(r.swaps), r.executes, r.kinds())
	}
	if state := r.current(); state.SpentUSDCMicro != r.swaps[0].amount {
		t.Fatalf("spent %d for a swap of %d", state.SpentUSDCMicro, r.swaps[0].amount)
	}
}

// Where /execute does report, its signature names the swap from then on.
func TestAGaslessSwapTakesItsSignatureFromTheReport(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.payer = &otherPayer{gasless: true}
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	signature, _ := r.last("swapped").Data["signature"].(string)
	if signature == "" || r.statuses[signature] == nil {
		t.Fatalf("booked under signature %q", signature)
	}
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 {
		t.Fatalf("%d swaps", len(r.swaps))
	}
}

// The signature a report of failure carries is kept, and decides sooner than the
// balance would.
func TestAGaslessReportOfFailureIsFollowedByItsSignature(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.payer = &otherPayer{gasless: true}
	r.executeCode = -1000
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	journal := r.current().Swap
	if journal == nil || journal.Signature == "" || r.count("swap_failed") != 0 {
		t.Fatalf("journal %+v, steps %s", journal, r.kinds())
	}
	r.statuses[journal.Signature] = failedOnChain()
	r.executeCode = 0
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.count("swap_failed") != 1 || r.current().SpentUSDCMicro != r.swaps[0].amount {
		t.Fatalf("%d swaps, steps %s, state %+v", len(r.swaps), r.kinds(), r.current())
	}
}

// Every RFQ order sampled has no last valid height, so its expireAt alone says when
// its swap has stopped landing. Until then it is in flight, whatever /execute said,
// and clearing it buys a second time while the first can still land.
func TestAGaslessSwapInFlightIsNotBoughtAgainBeforeItExpires(t *testing.T) {
	for _, test := range []struct {
		name  string
		fault func(*rig)
	}{
		{"its reply is lost", func(r *rig) { r.executeDrop = true }},
		{"a failure is reported", func(r *rig) { r.executeCode = -1000 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			r.payer = &otherPayer{gasless: true}
			test.fault(r)
			f := r.funder(t)
			request := deposit(7, 1_656)
			r.step(f, request)
			first := r.current().Swap
			if first == nil || first.LastValidBlockHeight != 0 {
				t.Fatalf("journal %+v", first)
			}
			r.executeDrop, r.executeCode = false, 0
			for _, at := range []time.Time{r.clock, r.clock.Add(rfqLifetime / 2), r.expireAt, r.expireAt.Add(time.Second)} {
				r.moveTo(at)
				wait := r.step(f, request)
				if kept := r.current().Swap; kept == nil || kept.Raw != first.Raw || r.orderCalls != 3 || wait == "" {
					r.landSwap(first) // it was still valid, and lands now
					t.Fatalf("with its order good for %s more, it waits on %q after %d orders, and with the first landing %d swaps have spent %s USDC: steps %s",
						r.expireAt.Sub(at), wait, r.orderCalls, len(r.swaps), microString(usdcOf(r.swaps)), r.kinds())
				}
			}
			r.landSwap(first)
			r.ensureFunded(f, request)
			if len(r.swaps) != 1 || r.count("swap_failed") != 0 || r.orderCalls != 3 {
				t.Fatalf("%d swaps, %d orders, steps %s", len(r.swaps), r.orderCalls, r.kinds())
			}
		})
	}
}

// Its expireAt, kept in the journal from run to run, is what decides: through its
// grace it waits, and just past that, sooner than an order naming no time would be
// decided, a swap that never landed is cleared and bought again.
func TestAGaslessSwapIsDecidedByTheExpireAtInItsJournal(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.payer = &otherPayer{gasless: true}
	r.executeDrop = true
	request := deposit(7, 1_656)
	r.step(r.funder(t), request)
	r.executeDrop = false
	expireAt := r.expireAt
	r.moveTo(expireAt.Add(swapExpiryGrace))
	if wait := r.step(r.funder(t), request); wait != "swap landing" {
		t.Fatal(wait)
	}
	if r.current().Swap == nil || r.orderCalls != 3 {
		t.Fatalf("state %+v, %d orders", r.current(), r.orderCalls)
	}
	r.moveTo(expireAt.Add(swapExpiryGrace + time.Second))
	r.step(r.funder(t), request)
	if r.count("swap_failed") != 1 || len(r.swaps) != 1 {
		t.Fatalf("steps %s, %d swaps", r.kinds(), len(r.swaps))
	}
}

// An order naming neither a height nor a time is measured from when it went out, for
// longer than any order that names one, since nothing says when it stops landing.
func TestAnOrderWithNeitherHeightNorExpireAtWaitsLonger(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.editOrder = func(order *jupiter.Order, req jupiter.OrderRequest) {
		if req.Taker != "" {
			order.LastValidBlockHeight = 0
		}
	}
	r.executeDrop = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	journal := r.current().Swap
	r.executeDrop = false
	start := r.clock
	r.moveTo(start.Add(swapNoExpiryWait))
	if wait := r.step(f, request); wait != "swap landing" {
		t.Fatal(wait)
	}
	if kept := r.current().Swap; kept == nil || kept.Raw != journal.Raw || r.orderCalls != 3 {
		t.Fatalf("journal %+v, %d orders", kept, r.orderCalls)
	}
	r.moveTo(start.Add(swapNoExpiryWait + time.Second))
	r.ensureFunded(f, request)
	if r.count("swap_failed") != 1 || len(r.swaps) != 1 {
		t.Fatalf("steps %s, %d swaps", r.kinds(), len(r.swaps))
	}
}

// An order that does carry a height, as Metis and OKX orders do, is decided by it: not
// through its grace, and by the balance once past.
func TestASwapWithAHeightIsDecidedOnlyPastItsGrace(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.executeDrop = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	journal := r.current().Swap
	if journal == nil || journal.LastValidBlockHeight == 0 {
		t.Fatalf("journal %+v", journal)
	}
	r.executeDrop = false
	for _, height := range []uint64{journal.LastValidBlockHeight, journal.LastValidBlockHeight + expiryGraceBlocks} {
		r.height = height
		if wait := r.step(f, request); wait != "swap landing" {
			t.Fatalf("at height %d it waits on %q", height, wait)
		}
		if kept := r.current().Swap; kept == nil || kept.Signature != journal.Signature || r.orderCalls != 3 {
			t.Fatalf("at height %d the swap was decided: journal %+v, %d orders", height, kept, r.orderCalls)
		}
	}
	r.height = journal.LastValidBlockHeight + expiryGraceBlocks + 1
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.count("swap_failed") != 1 {
		t.Fatalf("%d swaps, steps %s", len(r.swaps), r.kinds())
	}
}

// The fake is only worth what its shape is. This holds it to the sampled orders, and
// the world's own orders to carrying a height.
func TestTheRFQFakeHasTheShapeJupiterSends(t *testing.T) {
	r := newRig(t, enabled)
	r.payer = &otherPayer{gasless: true}
	req := jupiter.OrderRequest{InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: 1_000 * microPerUSDC, Taker: r.cfg.USDCAddress.String(), SlippageBps: 50}
	order, err := r.Order(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if order.LastValidBlockHeight != 0 || !order.Gasless || order.SlippageBps != 0 || order.OutAmount == 0 || order.OtherAmountThreshold != order.OutAmount {
		t.Fatalf("%+v", order)
	}
	if left := r.expireAt.Sub(r.clock); left < 52*time.Second || left > 54*time.Second {
		t.Fatal("expires in", left)
	}
	tx, err := solana.ParseTransaction(order.Transaction)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Signatures) != 2 || tx.Signatures[0] != ([64]byte{}) || tx.FeePayer() != rfqMaker || tx.RequiredSigners()[1] != r.cfg.USDCAddress {
		t.Fatalf("%d slots, fee payer %s, signers %v", len(tx.Signatures), tx.FeePayer(), tx.RequiredSigners())
	}
	r.payer = nil
	if order, err = r.Order(context.Background(), req); err != nil || order.LastValidBlockHeight == 0 {
		t.Fatal(order, err)
	}
}

// Another fee payer on an order that does not call itself gasless is refused
// before the wallet signs or anything is written.
func TestAnotherPayerWithoutGaslessIsRefusedBeforeSigning(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.payer = &otherPayer{gasless: false}
	_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656))
	if err == nil || !strings.Contains(err.Error(), "neither the wallet nor a gasless payer") {
		t.Fatal(err)
	}
	if r.simulations != 0 {
		t.Fatal("simulated before refusing the payer")
	}
	r.requireNoSwapJournal()
}

func TestAWalletThatIsNotASignerIsRefused(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.payer = &otherPayer{gasless: true, walletNotSigner: true}
	_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656))
	if err == nil || !strings.Contains(err.Error(), "is not a required signer") {
		t.Fatal(err)
	}
	if r.simulations != 0 {
		t.Fatal("simulated before refusing the signers")
	}
	r.requireNoSwapJournal()
}

// The balances decide a swap with no signature, and the operator topping up the
// wallet's USDC while one is unresolved must not make it read as never landed.
func TestAGaslessSwapThatLandedAcrossAUSDCDepositIsBooked(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.payer = &otherPayer{gasless: true}
	r.executeLost = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	journal := r.current().Swap
	r.usdc += 2 * journal.AmountMicro
	r.expireRFQ()
	r.step(f, request)
	state := r.current()
	if r.count("swapped") != 1 || r.count("swap_failed") != 0 || state.SpentUSDCMicro != journal.AmountMicro {
		t.Fatalf("%s USDC left and %d rao of TAO arrived, yet the steps are %s and the budget shows %s USDC spent",
			microString(journal.AmountMicro), r.swaps[0].out, r.kinds(), microString(state.SpentUSDCMicro))
	}
}

// And the operator taking USDC out must not make a swap that never landed read as
// booked. It cannot read as failed either, since USDC gone with no TAO to show for it
// is also what a lagging read looks like, and buying on that buys twice. So within the
// wait nothing is booked, the charge stays, and nothing more is bought. After it the
// swap goes to a person, still with no booking and no refund.
func TestAGaslessSwapThatNeverLandedAcrossAUSDCWithdrawalIsNotBooked(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.payer = &otherPayer{gasless: true}
	r.executeDrop = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	journal := r.current().Swap
	r.usdc -= journal.AmountMicro
	r.expireRFQ()
	if wait := r.step(f, request); wait == "" {
		t.Fatal("it did not wait")
	}
	state := r.current()
	if r.count("swapped") != 0 || r.count("swap_failed") != 0 || state.Swap == nil || state.SpentUSDCMicro != journal.AmountMicro || r.orderCalls != 3 || r.executes != 1 {
		t.Fatalf("within the wait the steps are %q, the budget shows %s USDC spent of the %s USDC charged, and it made %d orders and %d executes",
			r.kinds(), microString(state.SpentUSDCMicro), microString(journal.AmountMicro), r.orderCalls, r.executes)
	}
	r.moveTo(journal.SignedAt.Add(swapUnseenWait))
	r.step(f, request)
	state = r.current()
	if r.count("swapped") != 0 || r.count("swap_failed") != 0 || r.count("swap_needs_person") != 1 || state.Swap == nil || state.SpentUSDCMicro != journal.AmountMicro+state.Swap.AmountMicro {
		t.Fatalf("after the wait the steps are %q and the budget shows %s USDC spent", r.kinds(), microString(state.SpentUSDCMicro))
	}
}

// Expiry reads the order's last valid height, which nothing checks. Without one a
// swap in flight looks expired on the very next step and is bought again while it
// can still land.
func TestAnOrderWithoutALastValidHeightDoesNotBuyTwice(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.editOrder = func(order *jupiter.Order, req jupiter.OrderRequest) {
		if req.Taker != "" {
			order.LastValidBlockHeight = 0
		}
	}
	r.executeDrop = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	if _, _, err := f.step(context.Background(), request); err != nil {
		// Refusing such an order before sending it is a fine answer.
		if r.executes != 0 {
			t.Fatal("refused after sending:", err)
		}
		return
	}
	first := r.current().Swap
	r.executeDrop = false
	r.step(f, request)
	r.landSwap(first) // the first was still valid, and lands now
	if len(r.swaps) != 1 {
		t.Fatalf("%d swaps landed for %s USDC where one was due: steps %s", len(r.swaps), microString(usdcOf(r.swaps)), r.kinds())
	}
}

// A swap's on-chain failure gives its budget back, leaves its USDC, and the next
// step buys once.
func TestASwapThatFailsOnChainGivesItsBudgetBackAndRetriesOnce(t *testing.T) {
	r := newRig(t, enabled+"\n  max_usdc_per_epoch: 2000")
	r.usdc = 5_000 * microPerUSDC
	r.executeDrop = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	journal := r.current().Swap
	if journal == nil || journal.Signature == "" || r.usdc != 5_000*microPerUSDC {
		t.Fatalf("journal %+v, USDC %d", journal, r.usdc)
	}
	r.statuses[journal.Signature] = failedOnChain()
	r.executeDrop = false
	r.step(f, request)
	if !strings.Contains(fmt.Sprint(r.last("swap_failed").Data["reason"]), "failed on chain") {
		t.Fatal(r.last("swap_failed"))
	}
	state := r.current()
	if len(r.swaps) != 1 || r.executes != 2 || state.Swap != nil || state.SpentUSDCMicro != r.swaps[0].amount {
		t.Fatalf("%d swaps, %d executes, state %+v", len(r.swaps), r.executes, state)
	}
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.count("swap_failed") != 1 || r.usdc != 5_000*microPerUSDC-r.swaps[0].amount {
		t.Fatalf("%d swaps, steps %s, USDC %d", len(r.swaps), r.kinds(), r.usdc)
	}
}

// A bridge send's on-chain failure leaves its TAO on Solana, and the next step
// sends once.
func TestABridgeSendThatFailsOnChainKeepsItsTaoAndRetriesOnce(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.sendDrop = 1
	f := r.funder(t)
	request := deposit(7, 1_000)
	if wait := r.step(f, request); wait != "bridge send" {
		t.Fatal(wait)
	}
	journal := r.current().Bridge
	if journal == nil || len(r.sends) != 0 || r.tao != 7*raoPerTAO {
		t.Fatalf("journal %+v, %d sends, TAO %d", journal, len(r.sends), r.tao)
	}
	r.statuses[journal.Signature] = failedOnChain()
	if wait := r.step(f, request); wait != "bridge send" {
		t.Fatal(wait)
	}
	retry := r.current().Bridge
	if r.count("bridge_failed") != 1 || retry == nil || retry.Signature == journal.Signature || len(r.sends) != 1 || r.sends[0] != 7*raoPerTAO {
		t.Fatalf("steps %s, journal %+v, sends %v", r.kinds(), retry, r.sends)
	}
	r.ensureFunded(f, request)
	if len(r.sends) != 1 || len(r.broadcasts) != 2 || len(r.swaps) != 0 {
		t.Fatalf("%d sends, %d broadcasts, %d swaps", len(r.sends), len(r.broadcasts), len(r.swaps))
	}
}

// A swap only processed is on its way: neither booked nor cleared, however long the
// node takes to confirm it.
func TestASwapOnlyProcessedWaitsForConfirmation(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.executeDrop = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	journal := r.current().Swap
	r.statuses[journal.Signature] = &solana.SignatureStatus{ConfirmationStatus: "processed"}
	r.executeDrop = false
	for _, height := range []uint64{r.height, journal.LastValidBlockHeight + expiryGraceBlocks + 1} {
		r.height = height
		if wait := r.step(f, request); wait != "swap confirmation" {
			t.Fatalf("at height %d it waits on %q", height, wait)
		}
		if r.current().Swap == nil || len(r.events) != 0 || r.orderCalls != 3 {
			t.Fatalf("at height %d: state %+v, steps %s, %d orders", height, r.current(), r.kinds(), r.orderCalls)
		}
	}
	r.landSwap(journal)
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.count("swapped") != 1 {
		t.Fatalf("%d swaps, steps %s", len(r.swaps), r.kinds())
	}
}

// A send only processed is on its way too: not landed yet, and not sent again.
func TestABridgeSendOnlyProcessedWaitsForConfirmation(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.sendDrop = 1
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.step(f, request)
	journal := r.current().Bridge
	r.statuses[journal.Signature] = &solana.SignatureStatus{ConfirmationStatus: "processed"}
	if wait := r.step(f, request); wait != "bridge send confirmation" {
		t.Fatal(wait)
	}
	if kept := r.current().Bridge; kept == nil || !kept.LandedAt.IsZero() || r.count("bridged") != 0 || len(r.broadcasts) != 1 {
		t.Fatalf("journal %+v, steps %s, %d broadcasts", kept, r.kinds(), len(r.broadcasts))
	}
	r.statuses[journal.Signature] = &solana.SignatureStatus{ConfirmationStatus: "confirmed"}
	r.tao -= journal.AmountRao
	r.pending = append(r.pending, &fakeDelivery{wei: raoWei(journal.AmountRao), after: 1})
	r.ensureFunded(f, request)
	if r.count("bridged") != 1 || len(r.broadcasts) != 1 {
		t.Fatalf("steps %s, %d broadcasts", r.kinds(), len(r.broadcasts))
	}
}

// An error at processed is one node's most recent block, which a fork can abandon,
// and the same signed swap can still land on the chain that survives. Only a confirmed
// failure is final: clearing sooner buys again while the first can land.
func TestASwapFailureSeenOnlyAtProcessedIsNotFinal(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.executeDrop = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	first := r.current().Swap
	r.statuses[first.Signature] = &solana.SignatureStatus{Err: failedOnChain().Err, ConfirmationStatus: "processed"}
	r.executeDrop = false
	wait := r.step(f, request)
	if kept := r.current().Swap; kept == nil || kept.Signature != first.Signature || r.orderCalls != 3 {
		r.landSwap(first)
		t.Fatalf("cleared on an error at processed: it waits on %q after %d orders, steps %s, and with the first landing on the surviving fork %d swaps have spent %s USDC",
			wait, r.orderCalls, r.kinds(), len(r.swaps), microString(usdcOf(r.swaps)))
	}
	r.landSwap(first)
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.count("swap_failed") != 0 {
		t.Fatalf("%d swaps, steps %s", len(r.swaps), r.kinds())
	}
}

// The same for a bridge send: clearing it while it can still land leaves its TAO
// unaccounted for if it does.
func TestABridgeFailureSeenOnlyAtProcessedIsNotFinal(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.sendDrop = 1
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.step(f, request)
	first := r.current().Bridge
	r.statuses[first.Signature] = &solana.SignatureStatus{Err: failedOnChain().Err, ConfirmationStatus: "processed"}
	wait := r.step(f, request)
	if kept := r.current().Bridge; kept == nil || kept.Signature != first.Signature || len(r.broadcasts) != 1 {
		t.Fatalf("cleared on an error at processed: it waits on %q after %d broadcasts, steps %s", wait, len(r.broadcasts), r.kinds())
	}
}

// One TAO read behind the swap leaves the next step to read the truth, and it buys
// nothing more.
func TestOneStaleTaoReadAfterASwapDoesNotBuyAgain(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.lagFromFirstExecute(1, r.taoAccount)
	r.ensureFunded(r.funder(t), deposit(7, 1_656))
	if len(r.swaps) != 1 || len(r.sends) != 1 {
		t.Fatalf("%d swaps and %d sends", len(r.swaps), len(r.sends))
	}
}

// What a landed swap is booked at should be what it paid.
func TestAStaleTaoReadDoesNotBookAnEmptySwap(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.lagFromFirstExecute(1, r.taoAccount)
	r.ensureFunded(r.funder(t), deposit(7, 1_656))
	if booked := r.last("swapped").Data["tao_rao"]; booked != r.swaps[0].out {
		t.Fatalf("booked %v rao of TAO for a swap that paid %d", booked, r.swaps[0].out)
	}
}

// A landed swap drops its journal whatever the TAO read shows, and the next read
// comes with no time for the node to catch up. While it is still behind, the wallet
// looks like it holds no TAO and buys the whole amount again.
func TestATaoBalanceStillStaleAfterASwapDoesNotBuyAgain(t *testing.T) {
	for _, test := range []struct {
		name          string
		lost, nodeLag bool
	}{
		{name: "the reply reports success"},
		{name: "the signature reports success", lost: true},
		{name: "the whole node lags, simulation included", nodeLag: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 20_000 * microPerUSDC
			r.executeLost = test.lost
			accounts := []solana.PublicKey{r.taoAccount}
			if test.nodeLag {
				accounts = append(accounts, r.usdcAccount)
				r.lagSimulation = true
			}
			r.lagFromFirstExecute(untilTimePasses, accounts...)
			f := r.funder(t)
			request := deposit(7, 1_656)
			result, err := f.Ensure(context.Background(), request)
			if len(r.swaps) != 1 {
				clear(r.lag)
				bought, swaps, sleeps := usdcOf(r.swaps), len(r.swaps), r.sleeps
				next, nextErr := f.Ensure(context.Background(), request)
				surplus := new(big.Int).Sub(r.nativeWei, raoWei(r.cfg.Conversion.GasReserveRao))
				t.Fatalf("%d swaps for %s USDC after %d sleeps, where one swap of %s USDC covers the shortfall; Ensure said %+v, %v; "+
					"with the node caught up the next run said %+v, %v and left %s wei of TAO above the reserve, unstaked",
					swaps, microString(bought), sleeps, microString(r.swaps[0].amount), result, err, next, nextErr, surplus)
			}
			if err != nil || !result.Funded {
				t.Fatal(result, err)
			}
		})
	}
}

// When a node's reads lag a landed swap and its simulation does not, the gate catches
// a buy made on those reads: the USDC it would move no longer matches.
func TestAFresherSimulationRefusesABuyOnStaleBalances(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 20_000 * microPerUSDC
	r.lagAt(untilTimePasses, r.usdcAccount, r.taoAccount)
	r.usdc -= 1_600 * microPerUSDC
	r.tao += 6_600_000_000
	f := r.funder(t)
	request := deposit(7, 1_656)
	_, err := f.Ensure(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "simulation moves") {
		t.Fatal(err)
	}
	r.requireNoSwapJournal()
	clear(r.lag)
	r.ensureFunded(f, request)
	if len(r.swaps) != 0 || len(r.sends) != 1 {
		t.Fatalf("%d swaps and %d sends", len(r.swaps), len(r.sends))
	}
}

// A new epoch waits on the old swap and writes nothing meanwhile. Once it lands the
// new epoch's budget is whole: the old swap was charged to its own.
func TestAnOldSwapThatLandsInANewEpochLeavesTheNewBudgetAlone(t *testing.T) {
	r := newRig(t, enabled+"\n  max_usdc_per_epoch: 1700")
	r.usdc = 5_000 * microPerUSDC
	r.executeDrop = true
	f := r.funder(t)
	r.step(f, deposit(7, 1_656))
	old := r.current()
	if old.Epoch != 7 || old.Swap == nil || old.SpentUSDCMicro != old.Swap.AmountMicro {
		t.Fatalf("state in the old epoch: %+v", old)
	}
	r.executeDrop = false
	saved := bytes.Clone(r.stateJSON)
	if wait := r.step(f, deposit(8, 1_656)); wait != "swap landing" {
		t.Fatal(wait)
	}
	if !bytes.Equal(r.stateJSON, saved) || r.orderCalls != 3 {
		t.Fatalf("waiting wrote %s, after %d orders", r.stateJSON, r.orderCalls)
	}
	r.landSwap(old.Swap)
	r.step(f, deposit(8, 1_656))
	state := r.current()
	if state.Epoch != 8 || state.Swap != nil || state.SpentUSDCMicro != 0 {
		t.Fatalf("state after the old swap landed: %+v", state)
	}
	r.ensureFunded(f, deposit(8, 1_656))
	if len(r.swaps) != 1 {
		t.Fatalf("%d swaps", len(r.swaps))
	}
	// All 1,700 USDC of the new budget is still there: this swap of about 1,600 passes.
	r.ensureFunded(f, deposit(8, 3_250))
	if state := r.current(); len(r.swaps) != 2 || state.SpentUSDCMicro != r.swaps[1].amount {
		t.Fatalf("%d swaps, state %+v", len(r.swaps), state)
	}
}

// An old swap that dies in a new epoch was never charged to it, so nothing comes
// off the new budget. In its own epoch its charge comes back.
func TestAnOldSwapThatDiesInANewEpochLeavesTheNewBudgetAlone(t *testing.T) {
	earlier := uint64(3_000 * microPerUSDC)
	for name, kill := range map[string]func(r *rig, rec *SwapRecord){
		"expired": func(r *rig, rec *SwapRecord) { r.height = rec.LastValidBlockHeight + expiryGraceBlocks + 1 },
		"failed":  func(r *rig, rec *SwapRecord) { r.statuses[rec.Signature] = failedOnChain() },
	} {
		for _, test := range []struct {
			name         string
			epoch        uint64
			spentAtStart uint64
		}{
			{"in a new epoch", 8, 0},
			{"in its own epoch", 7, earlier},
		} {
			t.Run(name+" "+test.name, func(t *testing.T) {
				r := newRig(t, enabled)
				r.usdc = 5_000 * microPerUSDC
				r.seed(&State{Epoch: 7, SpentUSDCMicro: earlier})
				r.executeDrop = true
				f := r.funder(t)
				r.step(f, deposit(7, 1_656))
				old := r.current().Swap
				kill(r, old)
				r.step(f, deposit(test.epoch, 1_656)) // the replacement stays in flight
				state := r.current()
				if r.count("swap_failed") != 1 || state.Epoch != test.epoch || state.Swap == nil || state.Swap.Signature == old.Signature {
					t.Fatalf("steps %s, state %+v", r.kinds(), state)
				}
				if want := test.spentAtStart + state.Swap.AmountMicro; state.SpentUSDCMicro != want {
					t.Fatalf("the budget shows %s USDC spent, want %s", microString(state.SpentUSDCMicro), microString(want))
				}
			})
		}
	}
}

// A swap journaled but never submitted goes out once, in whatever epoch resumes it.
func TestAnOldSwapNeverSubmittedGoesOutOnceInANewEpoch(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	f := r.funder(t)
	r.loseSave(2)
	if _, _, err := f.step(context.Background(), deposit(7, 1_656)); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	journal := r.current().Swap
	if journal == nil || !journal.SubmittedAt.IsZero() || r.executes != 0 {
		t.Fatalf("journal %+v after %d executes", journal, r.executes)
	}
	r.ensureFunded(f, deposit(8, 1_656))
	if state := r.current(); len(r.swaps) != 1 || r.executes != 1 || r.orderCalls != 3 || state.SpentUSDCMicro != 0 {
		t.Fatalf("%d swaps, %d executes, %d orders, state %+v", len(r.swaps), r.executes, r.orderCalls, state)
	}
}

// A resumed swap is in flight like any other once it has gone out, whatever /execute
// answered, so the step must stop there. Going on, it reads a wallet that still looks
// short, buys a second time, and overwrites the only journal of the first.
func TestAResumedSwapInFlightDoesNotBuyAgain(t *testing.T) {
	for _, test := range []struct {
		name  string
		fault func(*rig)
	}{
		{"its reply is lost", func(r *rig) { r.executeDrop = true }},
		{"a failure is reported", func(r *rig) { r.executeCode = -1000 }},
		{"it lands with its reply lost", func(r *rig) { r.executeLost = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			f := r.funder(t)
			request := deposit(7, 1_656)
			r.loseSave(2)
			if _, _, err := f.step(context.Background(), request); !errors.Is(err, ErrStale) {
				t.Fatal(err)
			}
			first := r.current().Swap
			test.fault(r)
			r.step(f, request)
			state := r.current()
			if r.executes != 1 || r.orderCalls != 3 || state.Swap == nil || state.Swap.Raw != first.Raw || state.SpentUSDCMicro != first.AmountMicro || state.Bridge != nil {
				t.Fatalf("with %s USDC in flight the same step sent %d swaps from %d orders, kept the first's journal: %v, journaled a bridge send: %v, and shows %s USDC spent",
					microString(first.AmountMicro), r.executes, r.orderCalls, state.Swap != nil && state.Swap.Raw == first.Raw, state.Bridge != nil, microString(state.SpentUSDCMicro))
			}
		})
	}
}

// The cap is per epoch, so a late retry naming an earlier epoch must not win a
// fresh budget: alternating epochs would then have no cap at all.
func TestARetriedOldEpochDoesNotRestartTheBudget(t *testing.T) {
	r := newRig(t, enabled+"\n  max_usdc_per_epoch: 1700")
	r.usdc = 20_000 * microPerUSDC
	f := r.funder(t)
	r.ensureFunded(f, deposit(8, 1_656))
	_, err := f.Ensure(context.Background(), deposit(7, 3_250))
	if !errors.Is(err, ErrEpochBudget) || len(r.swaps) != 1 {
		t.Fatalf("after epoch 8 spent %s USDC of its 1700, a request for epoch 7 said %v and %d swaps have spent %s USDC",
			microString(r.swaps[0].amount), err, len(r.swaps), microString(usdcOf(r.swaps)))
	}
}

// With the budget gone, what was already bought still converts, and costs no USDC.
func TestAnExhaustedBudgetStillConvertsWhatIsAlreadyBought(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*rig)
		steps string
	}{
		{"native TAO", func(r *rig) { r.nativeWei = taoWei(5) }, "staked"},
		{"wTAO", func(r *rig) { r.wtaoWei = taoWei(5) }, "unwrapped,staked"},
		{"TAO on Solana", func(r *rig) { r.tao = 5 * raoPerTAO }, "bridged,arrived,unwrapped,staked"},
		{"wTAO and TAO on Solana together", func(r *rig) { r.wtaoWei, r.tao = taoWei(2), 3*raoPerTAO }, "unwrapped,bridged,arrived,unwrapped,staked"},
		{"native TAO and TAO on Solana together", func(r *rig) { r.nativeWei, r.tao = taoWei(2), 3*raoPerTAO }, "bridged,arrived,unwrapped,staked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			budget := r.cfg.Conversion.MaxUSDCMicroPerEpoch
			r.seed(&State{Epoch: 7, SpentUSDCMicro: budget})
			test.setup(r)
			r.ensureFunded(r.funder(t), deposit(7, 1_000))
			if got := r.kinds(); got != test.steps {
				t.Fatal("steps", got)
			}
			if r.orderCalls != 0 || len(r.swaps) != 0 || r.usdc != 5_000*microPerUSDC || r.current().SpentUSDCMicro != budget {
				t.Fatalf("%d orders, %d swaps, USDC %d, state %+v", r.orderCalls, len(r.swaps), r.usdc, r.current())
			}
		})
	}
}

// Too little bought under an exhausted budget is held where it is: wTAO unwraps,
// nothing else moves, and the error says why.
func TestAnExhaustedBudgetWithTooLittleBoughtHoldsIt(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	budget := r.cfg.Conversion.MaxUSDCMicroPerEpoch
	r.seed(&State{Epoch: 7, SpentUSDCMicro: budget})
	r.wtaoWei, r.tao = taoWei(1), 1*raoPerTAO
	_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000))
	if !errors.Is(err, ErrEpochBudget) {
		t.Fatal(err)
	}
	if got := r.kinds(); got != "unwrapped" {
		t.Fatal("steps", got)
	}
	if len(r.swaps) != 0 || len(r.sends) != 0 || r.tao != 1*raoPerTAO || r.alphaRao.Sign() != 0 || r.current().SpentUSDCMicro != budget {
		t.Fatalf("%d swaps, %d sends, TAO %d, alpha %s", len(r.swaps), len(r.sends), r.tao, r.alphaRao)
	}
}

// Flooring to bridge units can leave a wallet a fraction of a unit short, and with
// the budget gone it cannot buy that unit. It says so and spends nothing.
func TestAnExhaustedBudgetCannotBuyTheLastBridgeUnit(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	budget := r.cfg.Conversion.MaxUSDCMicroPerEpoch
	r.seed(&State{Epoch: 7, SpentUSDCMicro: budget})
	r.nativeWei = raoWei(199_999_999) // 3,900,000,001 rao missing for 1,000 alpha
	r.tao = 3_900_000_500
	_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000))
	if !errors.Is(err, ErrEpochBudget) {
		t.Fatal(err)
	}
	if len(r.swaps) != 0 || len(r.sends) != 0 || r.tao != 3_900_000_500 || r.current().SpentUSDCMicro != budget {
		t.Fatalf("%d swaps, %d sends, TAO %d", len(r.swaps), len(r.sends), r.tao)
	}
}

// The cap admits a swap of exactly what is left, and not one micro-USDC more.
func TestABudgetOfExactlyTheSwapIsEnough(t *testing.T) {
	probe := newRig(t, enabled)
	probe.usdc = 5_000 * microPerUSDC
	probe.ensureFunded(probe.funder(t), deposit(7, 1_656))
	amount := probe.swaps[0].amount
	allowed := func(budget uint64) *rig {
		r := newRig(t, enabled+fmt.Sprintf("\n  max_usdc_per_epoch: %s", microString(budget)))
		r.usdc = 5_000 * microPerUSDC
		return r
	}
	r := allowed(amount)
	r.ensureFunded(r.funder(t), deposit(7, 1_656))
	if r.current().SpentUSDCMicro != r.cfg.Conversion.MaxUSDCMicroPerEpoch {
		t.Fatalf("state %+v", r.current())
	}
	r = allowed(amount - 1)
	if _, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656)); !errors.Is(err, ErrEpochBudget) {
		t.Fatal(err)
	}
	r.requireNoSwapJournal()
}

// A price rise past the buffer between swap and stake is a second, smaller
// conversion within the cap, and the wallet ends funded.
func TestAPriceRiseBeyondTheBufferRunsASecondConversion(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	sends := 0
	r.settle = func() {
		if sends++; sends == 1 {
			raisePrice(r, 3)
		}
	}
	r.ensureFunded(r.funder(t), deposit(7, 1_656))
	if got := r.kinds(); got != "swapped,bridged,arrived,unwrapped,swapped,bridged,arrived,unwrapped,staked" {
		t.Fatal("steps", got)
	}
	if len(r.swaps) != 2 || r.swaps[1].amount >= r.swaps[0].amount/10 {
		t.Fatalf("%d swaps: %s then %s USDC", len(r.swaps), microString(r.swaps[0].amount), microString(r.swaps[1].amount))
	}
	state := r.current()
	if state.SpentUSDCMicro != usdcOf(r.swaps) || state.SpentUSDCMicro > r.cfg.Conversion.MaxUSDCMicroPerEpoch {
		t.Fatalf("state %+v", state)
	}
	if r.nativeWei.Cmp(raoWei(r.cfg.Conversion.GasReserveRao)) < 0 || r.wtaoWei.Sign() != 0 {
		t.Fatalf("native %s, wTAO %s", r.nativeWei, r.wtaoWei)
	}
}

// A rise before the stake that the cap blocks stakes nothing: what was bought stays
// in the wallet as TAO, whole-or-not-at-all, until a later epoch's budget finishes it.
func TestAPriceRiseTheCapBlocksKeepsTheTaoUnstakedAndSaysSo(t *testing.T) {
	r := newRig(t, enabled+"\n  max_usdc_per_epoch: 1600")
	r.usdc = 5_000 * microPerUSDC
	sends := 0
	r.settle = func() {
		if sends++; sends == 1 {
			raisePrice(r, 3)
		}
	}
	f := r.funder(t)
	_, err := f.Ensure(context.Background(), deposit(7, 1_656))
	if !errors.Is(err, ErrEpochBudget) || !strings.Contains(err.Error(), "USDC already spent") {
		t.Fatal(err)
	}
	if got := r.kinds(); got != "swapped,bridged,arrived,unwrapped" {
		t.Fatal("steps", got)
	}
	state := r.current()
	bought := new(big.Int).Sub(r.nativeWei, taoWei(0.2))
	if len(r.swaps) != 1 || state.SpentUSDCMicro != r.swaps[0].amount || r.alphaRao.Sign() != 0 || bought.Cmp(taoWei(6.6)) < 0 {
		t.Fatalf("%d swaps, state %+v, alpha %s, %s wei bought", len(r.swaps), state, r.alphaRao, bought)
	}
	r.ensureFunded(f, deposit(8, 1_656))
	if len(r.swaps) != 2 {
		t.Fatalf("%d swaps", len(r.swaps))
	}
}

// A rise as the stake fills leaves that stake standing, and the cap refuses only the
// rest: funds staked, a clear error, nothing more spent.
func TestAPriceRiseDuringTheStakeLeavesItStakedWhenTheCapBlocksTheRest(t *testing.T) {
	r := newRig(t, enabled+"\n  max_usdc_per_epoch: 1600")
	r.usdc = 5_000 * microPerUSDC
	sends := 0
	r.settle = func() {
		if sends++; sends == 2 {
			raisePrice(r, 3)
		}
	}
	need := alpha(1_656)
	_, err := r.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: need})
	if !errors.Is(err, ErrEpochBudget) || !strings.Contains(err.Error(), "USDC already spent") {
		t.Fatal(err)
	}
	if got := r.kinds(); got != "swapped,bridged,arrived,unwrapped,staked" {
		t.Fatal("steps", got)
	}
	state := r.current()
	if r.alphaRao.Cmp(alpha(1_600)) < 0 || r.alphaRao.Cmp(need) >= 0 || len(r.swaps) != 1 || state.SpentUSDCMicro != r.swaps[0].amount {
		t.Fatalf("alpha %s, %d swaps, state %+v", r.alphaRao, len(r.swaps), state)
	}
	if r.nativeWei.Cmp(raoWei(r.cfg.Conversion.GasReserveRao)) < 0 {
		t.Fatalf("native %s", r.nativeWei)
	}
}

// A price past the limit reverts the stake whole: the TAO stays and nothing is
// booked. A later run stakes at the new price.
func TestAStakeThePoolRefusesStaysInTheWallet(t *testing.T) {
	r := newRig(t, enabled)
	r.nativeWei = taoWei(10)
	r.chain = func(_ string, to common.Address, data []byte) error {
		if to != StakingPrecompile {
			return nil
		}
		limit := new(big.Int).SetBytes(data[68:100])
		spot := new(big.Int).Quo(new(big.Int).Mul(r.price, big.NewInt(105)), big.NewInt(100))
		if new(big.Int).Quo(spot, WeiPerRao).Cmp(limit) > 0 {
			return errors.New("SlippageTooHigh")
		}
		return nil
	}
	f := r.funder(t)
	_, err := f.Ensure(context.Background(), deposit(7, 1_000))
	if err == nil || !strings.Contains(err.Error(), "stake TAO") || !strings.Contains(err.Error(), "SlippageTooHigh") {
		t.Fatal(err)
	}
	if r.alphaRao.Sign() != 0 || r.nativeWei.Cmp(taoWei(10)) != 0 || r.count("staked") != 0 || len(r.operations) != 0 {
		t.Fatalf("alpha %s, native %s, steps %s", r.alphaRao, r.nativeWei, r.kinds())
	}
	r.chain = nil
	r.ensureFunded(f, deposit(7, 1_000))
}

// At 1,000 alpha the world's price asks exactly 4 TAO, and the reserve is 0.1. wTAO
// rebuilds a native balance below the reserve, and the stake leaves all of it.
func TestANativeBalanceBelowTheReserveIsRebuiltFromWTAO(t *testing.T) {
	r := newRig(t, enabled)
	reserve := r.cfg.Conversion.GasReserveRao
	r.nativeWei, r.wtaoWei = raoWei(30_000_000), raoWei(4_070_000_000)
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	if got := r.kinds(); got != "unwrapped,staked" {
		t.Fatal("steps", got)
	}
	if r.nativeWei.Cmp(raoWei(reserve)) != 0 || r.wtaoWei.Sign() != 0 {
		t.Fatalf("native %s, wTAO %s", r.nativeWei, r.wtaoWei)
	}
	if staked := r.last("staked").Data["tao_rao"]; staked != uint64(4_000_000_000) {
		t.Fatal("staked", staked)
	}
}

// One rao too little wTAO to rebuild the reserve buys the rest instead of eating it.
func TestAShortWTAOBuysRatherThanEatTheReserve(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.nativeWei, r.wtaoWei = raoWei(30_000_000), raoWei(4_069_999_999)
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	if got := r.kinds(); got != "unwrapped,swapped,bridged,arrived,unwrapped,staked" {
		t.Fatal("steps", got)
	}
	if r.nativeWei.Cmp(raoWei(r.cfg.Conversion.GasReserveRao)) < 0 {
		t.Fatalf("the stake ate the reserve: %s wei left", r.nativeWei)
	}
}

// Native at exactly the reserve, or at the stake and the reserve to the rao.
func TestNativeAtTheBoundariesOfTheReserve(t *testing.T) {
	const reserve, stake = 100_000_000, 4_000_000_000
	for _, test := range []struct {
		name   string
		native uint64
		steps  string
	}{
		{"exactly the reserve", reserve, "swapped,bridged,arrived,unwrapped,staked"},
		{"one rao below the stake and the reserve", stake + reserve - 1, "swapped,bridged,arrived,unwrapped,staked"},
		{"exactly the stake and the reserve", stake + reserve, "staked"},
		{"one rao above the stake and the reserve", stake + reserve + 1, "staked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			r.nativeWei = raoWei(test.native)
			r.ensureFunded(r.funder(t), deposit(7, 1_000))
			if got := r.kinds(); got != test.steps {
				t.Fatal("steps", got)
			}
			if r.nativeWei.Cmp(raoWei(reserve)) < 0 {
				t.Fatalf("the stake ate the reserve: %s wei left", r.nativeWei)
			}
		})
	}
}

// A shortfall of a few rao still stakes what the precompile accepts.
func TestAFewRaoShortStakesTheMinimum(t *testing.T) {
	r := newRig(t, enabled)
	r.alphaRao = new(big.Int).Sub(alpha(1_000), big.NewInt(3))
	r.nativeWei = taoWei(1)
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	if got := r.kinds(); got != "staked" {
		t.Fatal("steps", got)
	}
	// The funder's minimum with its 1% buffer and the extra rao, which the chain accepts.
	want := uint64(minStakeRao + minStakeRao/10_000*r.cfg.Conversion.AlphaBufferBps + 1)
	if staked := r.last("staked").Data["tao_rao"]; staked != want || want < chainMinStakeRao {
		t.Fatalf("staked %v, want %d", staked, want)
	}
}

// Less above the reserve than the minimum stake buys, at Jupiter's minimum, rather
// than staking too little.
func TestAFewRaoShortBelowTheMinimumBuysRatherThanUnderstakes(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.alphaRao = new(big.Int).Sub(alpha(1_000), big.NewInt(3))
	r.nativeWei = raoWei(r.cfg.Conversion.GasReserveRao + minStakeRao - 1)
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	if len(r.swaps) != 1 || r.swaps[0].amount != minSwapMicro {
		t.Fatalf("%d swaps, %s USDC", len(r.swaps), microString(usdcOf(r.swaps)))
	}
	if staked := r.last("staked").Data["tao_rao"].(uint64); staked < chainMinStakeRao {
		t.Fatal("staked", staked)
	}
}

// Exactly the funder's minimum above the reserve is the least it ever stakes in the
// ordinary way, and the chain has to take it.
func TestTheSmallestStakeClearsTheChainsMinimum(t *testing.T) {
	r := newRig(t, enabled)
	r.alphaRao = new(big.Int).Sub(alpha(1_000), big.NewInt(3))
	r.nativeWei = raoWei(r.cfg.Conversion.GasReserveRao + minStakeRao)
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	if staked := r.last("staked").Data["tao_rao"]; staked != uint64(minStakeRao) {
		t.Fatal("staked", staked)
	}
}

// An earlier send settling can take everything above the reserve. The stake then
// sends nothing, and the next step sees what is really there.
func TestASettledSendThatTakesTheSurplusStakesNothing(t *testing.T) {
	r := newRig(t, enabled)
	r.nativeWei = taoWei(5)
	settled := false
	r.settle = func() {
		if !settled {
			settled = true
			r.nativeWei = raoWei(r.cfg.Conversion.GasReserveRao)
		}
	}
	_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000))
	if !errors.Is(err, ErrInsufficientUSDC) {
		t.Fatal(err)
	}
	if len(r.operations) != 0 || r.count("staked") != 0 {
		t.Fatalf("operations %v, steps %s", r.operations, r.kinds())
	}
}

// When it leaves less than the stake asked for, the stake shrinks to what is above
// the reserve and leaves all of the reserve.
func TestASettledSendShrinksAStakeRatherThanEatTheReserve(t *testing.T) {
	r := newRig(t, enabled)
	r.nativeWei = taoWei(5)
	reserve := r.cfg.Conversion.GasReserveRao
	settled := false
	r.settle = func() {
		if !settled {
			settled = true
			r.nativeWei = raoWei(reserve + 3*raoPerTAO)
		}
	}
	_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000))
	if !errors.Is(err, ErrInsufficientUSDC) {
		t.Fatal(err)
	}
	if staked := r.last("staked").Data["tao_rao"]; staked != uint64(3*raoPerTAO) || r.nativeWei.Cmp(raoWei(reserve)) != 0 {
		t.Fatalf("staked %v, native %s", staked, r.nativeWei)
	}
}

// When it leaves a little above the reserve, less than the precompile accepts, the
// stake must not go out to revert and burn gas.
func TestASettledSendDoesNotShrinkAStakeBelowTheMinimum(t *testing.T) {
	r := newRig(t, enabled)
	r.nativeWei = taoWei(5)
	settled := false
	r.settle = func() {
		if !settled {
			settled = true
			r.nativeWei = raoWei(r.cfg.Conversion.GasReserveRao + 1_000)
		}
	}
	var below []uint64
	r.chain = func(_ string, to common.Address, data []byte) error {
		if to == StakingPrecompile {
			if amount := new(big.Int).SetBytes(data[36:68]).Uint64(); amount < chainMinStakeRao {
				below = append(below, amount)
				return errors.New("AmountTooLow")
			}
		}
		return nil
	}
	_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000))
	if len(below) != 0 {
		t.Fatalf("sent a stake of %v rao below the chain's %d minimum, and Ensure said %v", below, chainMinStakeRao, err)
	}
}

// The sender settles the wallet's earlier sends before prepare runs. An earlier
// run's stake finalizing there can already fund the wallet, and the stake must not
// then buy the same alpha again from a surplus.
func TestAStakeSettledByTheSenderIsNotStakedAgain(t *testing.T) {
	r := newRig(t, enabled)
	r.nativeWei = taoWei(100)
	settled := false
	r.settle = func() {
		if !settled {
			settled = true
			const amount = 4_040_000_001
			r.nativeWei = new(big.Int).Sub(r.nativeWei, raoWei(amount))
			r.alphaRao = r.alphaFor(big.NewInt(amount))
		}
	}
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	if len(r.operations) != 0 {
		t.Fatalf("the wallet was already funded, and %v staked %s rao of alpha for 1,000 alpha's worth of need", r.operations, r.alphaRao)
	}
}

// The wallet pays for its sends from its native TAO, which the world leaves out. At a
// realistic price that changes nothing: one swap is still enough, and only the last
// send's gas can come out of the reserve, which is what the reserve is for.
func TestGasPaidByTheWalletStillConverges(t *testing.T) {
	const gas = 500_000 // rao per send
	for name, conversion := range map[string]string{
		"with the default buffer": enabled,
		"with no buffer":          enabled + "\n  alpha_buffer_bps: 0",
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, conversion)
			r.usdc = 5_000 * microPerUSDC
			r.chain = func(string, common.Address, []byte) error {
				r.nativeWei = new(big.Int).Sub(r.nativeWei, raoWei(gas))
				return nil
			}
			r.ensureFunded(r.funder(t), deposit(7, 1_656))
			if len(r.swaps) != 1 || len(r.sends) != 1 {
				t.Fatalf("%d swaps and %d sends", len(r.swaps), len(r.sends))
			}
			if r.nativeWei.Cmp(raoWei(r.cfg.Conversion.GasReserveRao-gas)) < 0 {
				t.Fatalf("native %s", r.nativeWei)
			}
		})
	}
}

// A stake mining between the first read of alpha and the read of TAO looks like a
// wallet short of both, so spending starts only from an alpha read after them.
func TestAStakeMiningBetweenReadsBuysNothing(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	need := alpha(1_000)
	mined := false
	r.failToken = func(address solana.PublicKey) error { // never fails: it marks the read
		if address == r.taoAccount && !mined {
			mined = true
			r.alphaRao = new(big.Int).Set(need)
		}
		return nil
	}
	r.ensureFunded(r.funder(t), Request{Epoch: 7, NeedRao: need})
	if !mined || r.orderCalls != 0 || len(r.sends) != 0 || len(r.operations) != 0 {
		t.Fatalf("%d orders, %d sends, operations %v", r.orderCalls, len(r.sends), r.operations)
	}
}

// An earlier stake settling can cover only part of the shortfall, and the stake then
// buys the rest with its own buffer, not the whole amount again.
func TestAnEarlierStakeShrinksTheNextToWhatIsStillShort(t *testing.T) {
	r := newRig(t, enabled)
	r.nativeWei = taoWei(100)
	settled := false
	r.settle = func() {
		if !settled {
			settled = true
			r.alphaRao = alpha(600)
			r.nativeWei = new(big.Int).Sub(r.nativeWei, taoWei(2.4))
		}
	}
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	// The 400 alpha still short is 1.6 TAO, with its 1% buffer and the extra rao.
	if staked := r.last("staked").Data["tao_rao"]; staked != uint64(1_616_000_001) || r.count("staked") != 1 {
		t.Fatalf("staked %v, steps %s", staked, r.kinds())
	}
}

// An earlier stake settling can also make an unwrap pointless, and the wTAO waits.
func TestAnUnwrapOfAFundedWalletSendsNothing(t *testing.T) {
	r := newRig(t, enabled)
	r.wtaoWei = taoWei(5)
	settled := false
	r.settle = func() {
		if !settled {
			settled = true
			r.alphaRao = alpha(1_000)
		}
	}
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	if len(r.operations) != 0 || r.wtaoWei.Cmp(taoWei(5)) != 0 {
		t.Fatalf("operations %v, wTAO %s", r.operations, r.wtaoWei)
	}
}

// With no gas the unwrap cannot go out. Nothing else is tried, and the error says so.
func TestNoGasForTheUnwrapIsReported(t *testing.T) {
	r := newRig(t, enabled)
	r.nativeWei, r.wtaoWei = new(big.Int), taoWei(5)
	r.chain = func(string, common.Address, []byte) error {
		if r.nativeWei.Sign() == 0 {
			return errors.New("insufficient funds for gas")
		}
		return nil
	}
	_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000))
	if err == nil || !strings.Contains(err.Error(), "unwrap wTAO") || !strings.Contains(err.Error(), "insufficient funds for gas") {
		t.Fatal(err)
	}
	if r.wtaoWei.Cmp(taoWei(5)) != 0 || len(r.events) != 0 || r.saves != 0 {
		t.Fatalf("wTAO %s, steps %s", r.wtaoWei, r.kinds())
	}
}

// The bridge moves whole units and leaves the dust behind.
func TestABridgeSendIsWholeUnitsAndLeavesTheDust(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7*raoPerTAO + 123
	f := r.funder(t)
	r.ensureFunded(f, deposit(7, 1_000))
	if len(r.sends) != 1 || r.sends[0] != 7*raoPerTAO || r.tao != 123 || len(r.swaps) != 0 {
		t.Fatalf("sends %v, TAO %d, %d swaps", r.sends, r.tao, len(r.swaps))
	}
	r.ensureFunded(f, deposit(7, 1_000))
	if len(r.sends) != 1 || len(r.swaps) != 0 {
		t.Fatalf("%d sends and %d swaps once funded", len(r.sends), len(r.swaps))
	}
}

// Nearly enough on Solana floors to just under what is missing. One swap tops it up.
// With a buffer it buys to the buffered amount; with none, Jupiter's minimum.
func TestAFlooredBalanceJustUnderTheNeedBuysOneSwap(t *testing.T) {
	for _, test := range []struct {
		name       string
		conversion string
		atMost     uint64
	}{
		{"with the default buffer", enabled, 10 * microPerUSDC},
		{"with no buffer", enabled + "\n  alpha_buffer_bps: 0", minSwapMicro},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, test.conversion)
			r.usdc = 5_000 * microPerUSDC
			r.nativeWei = raoWei(199_999_999) // 3,900,000,001 rao missing for 1,000 alpha
			r.tao = 3_900_000_500
			r.ensureFunded(r.funder(t), deposit(7, 1_000))
			if len(r.swaps) != 1 || r.swaps[0].amount > test.atMost || r.current().SpentUSDCMicro != r.swaps[0].amount {
				t.Fatalf("%d swaps for %s USDC", len(r.swaps), microString(usdcOf(r.swaps)))
			}
			if len(r.sends) != 1 || r.sends[0]%bridge.UnitRao != 0 || r.tao >= bridge.UnitRao {
				t.Fatalf("sends %v, TAO %d", r.sends, r.tao)
			}
			if booked := r.last("swapped").Data["tao_rao"]; booked != r.swaps[0].out {
				t.Fatalf("booked %v rao for a swap that paid %d, beside the TAO already held", booked, r.swaps[0].out)
			}
		})
	}
}

// Dust that cannot be bridged neither stops the route nor buys over and over.
func TestDustOnSolanaDoesNotBuyInALoop(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.tao = bridge.UnitRao - 1
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || len(r.sends) != 1 || r.tao >= bridge.UnitRao || r.tao == 0 {
		t.Fatalf("%d swaps, %d sends, TAO %d", len(r.swaps), len(r.sends), r.tao)
	}
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.orderCalls != 3 {
		t.Fatalf("%d swaps and %d orders once funded", len(r.swaps), r.orderCalls)
	}
}

// wTAO of less than a bridge unit, the least a delivery brings, is no reason to spend
// gas on an unwrap: anyone can send the wallet some.
func TestWTAODustIsLeftAlone(t *testing.T) {
	for name, wei := range map[string]*big.Int{
		"below one rao":     new(big.Int).Sub(WeiPerRao, big.NewInt(1)),
		"one rao":           new(big.Int).Set(WeiPerRao),
		"just under a unit": new(big.Int).Sub(raoWei(bridge.UnitRao), big.NewInt(1)),
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.nativeWei = taoWei(5)
			r.wtaoWei = new(big.Int).Set(wei)
			r.ensureFunded(r.funder(t), deposit(7, 1_000))
			if got := r.kinds(); got != "staked" {
				t.Fatal("steps", got)
			}
			if r.wtaoWei.Cmp(wei) != 0 {
				t.Fatal("wTAO", r.wtaoWei)
			}
		})
	}
}

// From one bridge unit up the unwrap takes all of it, the dust above the last rao too.
func TestWTAOFromOneBridgeUnitIsWithdrawnWhole(t *testing.T) {
	for name, wei := range map[string]*big.Int{
		"exactly one unit":  raoWei(bridge.UnitRao),
		"one unit and dust": new(big.Int).Add(raoWei(bridge.UnitRao), big.NewInt(7)),
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.nativeWei = taoWei(5)
			r.wtaoWei = new(big.Int).Set(wei)
			r.ensureFunded(r.funder(t), deposit(7, 1_000))
			if got := r.kinds(); got != "unwrapped,staked" {
				t.Fatal("steps", got)
			}
			if unwrapped := r.last("unwrapped").Data["wei"]; unwrapped != wei.String() || r.wtaoWei.Sign() != 0 {
				t.Fatalf("unwrapped %v, %s wTAO left", unwrapped, r.wtaoWei)
			}
		})
	}
}

// An earlier unwrap settling first can take it all, leaving nothing to send.
func TestAnUnwrapWithNothingLeftSendsNothing(t *testing.T) {
	r := newRig(t, enabled)
	r.nativeWei, r.wtaoWei = taoWei(5), taoWei(5)
	settled := false
	r.settle = func() {
		if !settled {
			settled = true
			r.nativeWei, r.wtaoWei = taoWei(10), new(big.Int)
		}
	}
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	if got := r.kinds(); got != "staked" {
		t.Fatal("steps", got)
	}
}

// wTAO in the wallet when the send goes out does not read as its delivery.
func TestADeliveryIsMeasuredFromTheBalanceBeforeTheSend(t *testing.T) {
	r := newRig(t, enabled)
	r.wtaoWei = taoWei(5)
	r.seed(&State{Epoch: 7, Bridge: &BridgeRecord{Signature: "sig", AmountRao: 3 * raoPerTAO, WTAOBeforeWei: taoWei(5).String(), LandedAt: r.clock}})
	f := r.funder(t)
	request := deposit(7, 1_000)
	if wait := r.step(f, request); !strings.Contains(wait, "LayerZero delivery") {
		t.Fatal(wait)
	}
	if r.current().Bridge == nil || r.count("arrived") != 0 {
		t.Fatalf("state %+v, steps %s", r.current(), r.kinds())
	}
	r.wtaoWei = taoWei(8)
	r.ensureFunded(f, request)
	if got := r.kinds(); got != "arrived,unwrapped,staked" {
		t.Fatal("steps", got)
	}
}

// Dust already in the wallet is part of the balance a delivery is measured from.
func TestWTAODustBeforeABridgeDoesNotHideItsArrival(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.wtaoWei = big.NewInt(999)
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	if got := r.kinds(); got != "bridged,arrived,unwrapped,staked" {
		t.Fatal("steps", got)
	}
	if unwrapped := r.last("unwrapped").Data["wei"]; unwrapped != new(big.Int).Add(taoWei(7), big.NewInt(999)).String() || r.wtaoWei.Sign() != 0 {
		t.Fatalf("unwrapped %v, %s wTAO left", unwrapped, r.wtaoWei)
	}
}

// The step needs a whole rao to unwrap, and prepare should too: an earlier unwrap
// settling first can leave only dust, not worth the gas of withdrawing it.
func TestAnUnwrapThatFindsOnlyDustSendsNothing(t *testing.T) {
	r := newRig(t, enabled)
	r.nativeWei, r.wtaoWei = taoWei(5), taoWei(5)
	settled := false
	r.settle = func() {
		if !settled {
			settled = true
			r.nativeWei = taoWei(10)
			r.wtaoWei = big.NewInt(999)
		}
	}
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	if r.count("unwrapped") != 0 {
		t.Fatalf("spent gas to unwrap %v wei", r.last("unwrapped").Data["wei"])
	}
}

// A write that loses at submission stops before /execute, and the next run sends
// the journaled bytes rather than ordering again.
func TestALostWriteAtSubmissionSendsNothingAndTheNextRunSendsIt(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.loseSave(2)
	if _, err := f.Ensure(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	journal := r.current().Swap
	if r.executes != 0 || journal == nil || !journal.SubmittedAt.IsZero() {
		t.Fatalf("%d executes, journal %+v", r.executes, journal)
	}
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.executes != 1 || r.orderCalls != 3 || r.executed[0] != journal.Raw {
		t.Fatalf("%d swaps, %d executes, %d orders", len(r.swaps), r.executes, r.orderCalls)
	}
	if state := r.current(); state.SpentUSDCMicro != r.swaps[0].amount {
		t.Fatalf("state %+v", state)
	}
}

// A write that loses as a landed swap finishes keeps the journal, which the next
// run resolves by signature, with no second purchase.
func TestALostWriteAfterASwapFinishedDoesNotBuyAgain(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.loseSaveClearingSwap()
	if _, err := f.Ensure(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if len(r.swaps) != 1 || r.current().Swap == nil {
		t.Fatalf("%d swaps, state %+v", len(r.swaps), r.current())
	}
	r.ensureFunded(f, request)
	if state := r.current(); len(r.swaps) != 1 || r.executes != 1 || len(r.sends) != 1 || state.SpentUSDCMicro != r.swaps[0].amount {
		t.Fatalf("%d swaps, %d executes, %d sends, state %+v", len(r.swaps), r.executes, len(r.sends), state)
	}
}

// With another payer the write after /execute also holds the only copy of the
// signature. The balances decide instead, once the swap can no longer land.
func TestALostWriteAfterAGaslessSwapFinishedDoesNotBuyAgain(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.payer = &otherPayer{gasless: true}
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.loseSaveAfterExecute()
	if _, err := f.Ensure(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if journal := r.current().Swap; journal == nil || journal.Signature != "" {
		t.Fatalf("journal %+v", journal)
	}
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.executes != 1 || r.orderCalls != 3 {
		t.Fatalf("%d swaps, %d executes, %d orders", len(r.swaps), r.executes, r.orderCalls)
	}
}

// Losing the write of a reported signature falls back the same way.
func TestALostWriteOfAReportedSignatureFallsBackToTheBalance(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.payer = &otherPayer{gasless: true}
	r.executeCode = -1000
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.loseSaveAfterExecute()
	if _, err := f.Ensure(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if journal := r.current().Swap; journal == nil || journal.Signature != "" {
		t.Fatalf("journal %+v", journal)
	}
	r.executeCode = 0
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.executes != 2 || r.current().SpentUSDCMicro != r.swaps[0].amount {
		t.Fatalf("%d swaps, %d executes, state %+v", len(r.swaps), r.executes, r.current())
	}
}

// A write that loses while clearing a swap keeps the journal and its budget. The
// next run clears it, returns the budget once and buys once.
func TestALostWriteClearingAnExpiredSwapRetriesOnce(t *testing.T) {
	r := newRig(t, enabled+"\n  max_usdc_per_epoch: 2000")
	r.usdc = 5_000 * microPerUSDC
	r.executeCode = -1005
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.loseSaveClearingSwap()
	if _, err := f.Ensure(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	journal := r.current().Swap
	if journal == nil || r.current().SpentUSDCMicro != journal.AmountMicro {
		t.Fatalf("state %+v", r.current())
	}
	r.executeCode = 0
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.current().SpentUSDCMicro != r.swaps[0].amount {
		t.Fatalf("%d swaps, state %+v", len(r.swaps), r.current())
	}
}

// A journal write that loses stops the bridge send before its broadcast.
func TestALostWriteJournalingABridgeBroadcastsNothing(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.loseSave(1)
	if _, err := f.Ensure(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if len(r.broadcasts) != 0 || r.current().Bridge != nil || r.tao != 7*raoPerTAO {
		t.Fatalf("%d broadcasts, state %+v", len(r.broadcasts), r.current())
	}
	r.ensureFunded(f, request)
	if len(r.sends) != 1 || len(r.broadcasts) != 1 {
		t.Fatalf("%d sends, %d broadcasts", len(r.sends), len(r.broadcasts))
	}
}

// A write that loses as a landed send is noted leaves nothing to redo: the next run
// reads the same landing and goes on waiting for delivery.
func TestALostWriteAfterABridgeLandedDoesNotSendAgain(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.step(f, request)
	r.loseSave(1)
	if _, _, err := f.step(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if journal := r.current().Bridge; journal == nil || !journal.LandedAt.IsZero() || r.count("bridged") != 0 {
		t.Fatalf("journal %+v, steps %s", journal, r.kinds())
	}
	r.ensureFunded(f, request)
	if len(r.sends) != 1 || len(r.broadcasts) != 1 || r.count("bridged") != 1 {
		t.Fatalf("%d sends, %d broadcasts, steps %s", len(r.sends), len(r.broadcasts), r.kinds())
	}
}

// A write that loses as the wTAO arrives keeps the journal, and the wTAO, which
// unwraps only after that write. The next run picks up from there.
func TestALostWriteWhenWTAOArrivedDoesNotSendAgain(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.loseSaveClearingBridge()
	if _, err := f.Ensure(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if journal := r.current().Bridge; journal == nil || journal.LandedAt.IsZero() || r.wtaoWei.Cmp(taoWei(7)) != 0 {
		t.Fatalf("journal %+v, wTAO %s", journal, r.wtaoWei)
	}
	r.ensureFunded(f, request)
	if len(r.sends) != 1 || len(r.broadcasts) != 1 || r.count("unwrapped") != 1 || len(r.swaps) != 0 {
		t.Fatalf("%d sends, %d broadcasts, steps %s, %d swaps", len(r.sends), len(r.broadcasts), r.kinds(), len(r.swaps))
	}
}

// A write that loses while clearing a failed send: the next run reads the same
// failure, clears it, and sends once.
func TestALostWriteClearingAFailedBridgeSendsOnce(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.sendDrop = 1
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.step(f, request)
	journal := r.current().Bridge
	r.statuses[journal.Signature] = failedOnChain()
	r.loseSave(1)
	if _, _, err := f.step(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if kept := r.current().Bridge; kept == nil || kept.Signature != journal.Signature || len(r.sends) != 0 {
		t.Fatalf("journal %+v, %d sends", kept, len(r.sends))
	}
	r.ensureFunded(f, request)
	if len(r.sends) != 1 || r.tao != 0 {
		t.Fatalf("%d sends, TAO %d", len(r.sends), r.tao)
	}
}

// And the same while clearing an expired send.
func TestALostWriteClearingAnExpiredBridgeSendsOnce(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.sendDrop = 1_000_000
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.step(f, request)
	journal := r.current().Bridge
	r.height = journal.LastValidBlockHeight + expiryGraceBlocks + 1
	r.loseSave(1)
	if _, _, err := f.step(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if kept := r.current().Bridge; kept == nil || kept.Signature != journal.Signature {
		t.Fatalf("journal %+v", kept)
	}
	r.sendDrop = 0
	r.ensureFunded(f, request)
	if len(r.sends) != 1 || r.tao != 0 {
		t.Fatalf("%d sends, TAO %d", len(r.sends), r.tao)
	}
}

// Events are kept for accounting, so one step is one event, even when the write
// that followed it lost and the next run did the step's bookkeeping again.
func TestALostWriteDoesNotBookAnEventTwice(t *testing.T) {
	request := deposit(7, 1_000)
	for _, test := range []struct {
		kind  string
		setup func(r *rig, f *Funder)
	}{
		{"swapped", func(r *rig, f *Funder) {
			r.usdc = 5_000 * microPerUSDC
			r.loseSaveClearingSwap()
			if _, err := f.Ensure(context.Background(), request); !errors.Is(err, ErrStale) {
				r.t.Fatal(err)
			}
		}},
		{"swap_failed", func(r *rig, f *Funder) {
			r.usdc = 5_000 * microPerUSDC
			r.executeCode = -1005
			r.loseSaveClearingSwap()
			if _, err := f.Ensure(context.Background(), request); !errors.Is(err, ErrStale) {
				r.t.Fatal(err)
			}
			r.executeCode = 0
		}},
		{"arrived", func(r *rig, f *Funder) {
			r.tao = 7 * raoPerTAO
			r.loseSaveClearingBridge()
			if _, err := f.Ensure(context.Background(), request); !errors.Is(err, ErrStale) {
				r.t.Fatal(err)
			}
		}},
		{"bridge_failed", func(r *rig, f *Funder) {
			r.tao = 7 * raoPerTAO
			r.sendDrop = 1
			r.step(f, request)
			r.statuses[r.current().Bridge.Signature] = failedOnChain()
			r.loseSave(1)
			if _, _, err := f.step(context.Background(), request); !errors.Is(err, ErrStale) {
				r.t.Fatal(err)
			}
		}},
		{"bridge_expired", func(r *rig, f *Funder) {
			r.tao = 7 * raoPerTAO
			r.sendDrop = 1_000_000
			r.step(f, request)
			r.height = r.current().Bridge.LastValidBlockHeight + expiryGraceBlocks + 1
			r.loseSave(1)
			if _, _, err := f.step(context.Background(), request); !errors.Is(err, ErrStale) {
				r.t.Fatal(err)
			}
			r.sendDrop = 0
		}},
	} {
		t.Run(test.kind, func(t *testing.T) {
			r := newRig(t, enabled)
			f := r.funder(t)
			test.setup(r, f)
			r.ensureFunded(f, request)
			if got := r.count(test.kind); got != 1 {
				t.Fatalf("%d %s events for one step: %s", got, test.kind, r.kinds())
			}
		})
	}
}

// LayerZero calling a message failed or blocked is reported and waited out. The TAO
// has left Solana, so sending or buying again could only pay twice.
func TestABridgeLayerZeroCallsFailedOrBlockedKeepsWaiting(t *testing.T) {
	for _, status := range []string{bridge.StatusFailed, bridge.StatusBlocked} {
		t.Run(status, func(t *testing.T) {
			r := newRig(t, enabled)
			r.tao = 7 * raoPerTAO
			r.usdc = 5_000 * microPerUSDC // it could buy, and must not
			r.deliverAfter = 1_000_000
			r.scanStatus = status
			f := r.funder(t)
			request := deposit(7, 1_000)
			for run := range 2 {
				result, err := f.Ensure(context.Background(), request)
				if err != nil {
					t.Fatal(err)
				}
				if result.Funded || !strings.Contains(result.Waiting, "LayerZero delivery") || !strings.Contains(result.Waiting, status) {
					t.Fatalf("run %d: %+v", run, result)
				}
				if !r.logged(fmt.Sprintf("(LayerZero status %q)", status)) {
					t.Fatalf("run %d: not reported in the log", run)
				}
				journal := r.current().Bridge
				if journal == nil || journal.LandedAt.IsZero() || len(r.sends) != 1 || len(r.broadcasts) != 1 || r.orderCalls != 0 {
					t.Fatalf("run %d: journal %+v, %d sends, %d broadcasts, %d orders", run, journal, len(r.sends), len(r.broadcasts), r.orderCalls)
				}
			}
			r.pending[0].after = 1 // LayerZero's retry goes through
			r.ensureFunded(f, request)
			if len(r.sends) != 1 || len(r.swaps) != 0 {
				t.Fatalf("%d sends and %d swaps", len(r.sends), len(r.swaps))
			}
		})
	}
}

// Scan is only ever a report: when it fails, the wait goes on and delivery still
// finishes the bridge. A delivery past its usual time is reported either way.
func TestScanErrorsDoNotBreakWaiting(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.deliverAfter = 1_000_000
	r.scanErr = errors.New("scan is down")
	f := r.funder(t)
	request := deposit(7, 1_000)
	result, err := f.Ensure(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Funded || !strings.Contains(result.Waiting, "LayerZero delivery") || !r.logged("scan is down") {
		t.Fatalf("%+v, logs %v", result, r.logs)
	}
	if r.logged("has waited") {
		t.Fatal("reported a delivery as late within its usual time")
	}
	if result, err = f.Ensure(context.Background(), request); err != nil || result.Funded {
		t.Fatal(result, err)
	}
	if !r.logged("has waited") {
		t.Fatal("a delivery past its usual time was not reported")
	}
	r.pending[0].after = 1
	r.ensureFunded(f, request)
	if len(r.sends) != 1 {
		t.Fatalf("%d sends", len(r.sends))
	}
}

// Scan calling a send delivered while the balance does not show it may be a read that
// has not caught up, so it waits, and past that the wTAO has moved on. It is not sent
// again either way.
func TestADeliveredSendTheBalanceDoesNotShow(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.deliverAfter = 1_000_000
	r.scanStatus = bridge.StatusDelivered
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.step(f, request)
	for range 2 {
		if wait := r.step(f, request); !strings.Contains(wait, "LayerZero delivery") {
			t.Fatal(wait)
		}
		if r.current().Bridge == nil || r.count("arrived") != 0 {
			t.Fatalf("state %+v, steps %s", r.current(), r.kinds())
		}
		r.moveTo(r.clock.Add(time.Minute))
	}
	r.moveTo(r.clock.Add(rfqSettled))
	_, _, err := f.step(context.Background(), request)
	if !errors.Is(err, ErrInsufficientUSDC) {
		t.Fatal(err)
	}
	if r.count("arrived") != 1 || r.current().Bridge != nil || !r.logged("it has moved on") {
		t.Fatalf("state %+v, steps %s", r.current(), r.kinds())
	}
	if len(r.sends) != 1 || len(r.broadcasts) != 1 {
		t.Fatalf("%d sends, %d broadcasts", len(r.sends), len(r.broadcasts))
	}
}

// When the read does catch up it is an arrival like any other.
func TestADeliveredSendTheBalanceShowsLateArrives(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.deliverAfter = 1_000_000
	r.scanStatus = bridge.StatusDelivered
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.step(f, request)
	r.step(f, request)
	r.pending[0].after = 1
	r.ensureFunded(f, request)
	if got := r.kinds(); got != "bridged,arrived,unwrapped,staked" || r.logged("it has moved on") {
		t.Fatalf("steps %s, logs %v", got, r.logs)
	}
}

// TAO that reaches the wallet while a delivery is on its way covers the shortfall on
// its own, and the send stays journaled for the wTAO it is still owed.
func TestHeldTaoIsStakedWhileADeliveryIsAwaited(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.deliverAfter = 1_000_000
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.step(f, request)
	if wait := r.step(f, request); !strings.Contains(wait, "LayerZero delivery") {
		t.Fatal(wait)
	}
	r.nativeWei = taoWei(10)
	r.ensureFunded(f, request)
	if got := r.kinds(); got != "bridged,staked" {
		t.Fatal("steps", got)
	}
	if kept := r.current().Bridge; kept == nil || kept.LandedAt.IsZero() || len(r.sends) != 1 || len(r.swaps) != 0 {
		t.Fatalf("journal %+v, %d sends, %d swaps", kept, len(r.sends), len(r.swaps))
	}
}

// A send the chain never saw goes out again as the same bytes until its last valid
// height; waits, doing neither, through the grace; and past it is cleared with its
// TAO still there, for one new send.
func TestANeverSeenBridgeSendIsClearedOnlyPastTheGrace(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.sendDrop = 1_000_000
	f := r.funder(t)
	request := deposit(7, 1_000)
	if wait := r.step(f, request); wait != "bridge send" {
		t.Fatal(wait)
	}
	journal := r.current().Bridge
	if journal == nil || len(r.broadcasts) != 1 {
		t.Fatalf("journal %+v, %d broadcasts", journal, len(r.broadcasts))
	}
	r.height = journal.LastValidBlockHeight
	if wait := r.step(f, request); wait != "bridge send landing" {
		t.Fatal(wait)
	}
	if len(r.broadcasts) != 2 || r.broadcasts[1] != r.broadcasts[0] {
		t.Fatalf("%d broadcasts", len(r.broadcasts))
	}
	for _, height := range []uint64{journal.LastValidBlockHeight + 1, journal.LastValidBlockHeight + expiryGraceBlocks} {
		r.height = height
		if wait := r.step(f, request); wait != "bridge send landing" {
			t.Fatal(wait)
		}
		kept := r.current().Bridge
		if len(r.broadcasts) != 2 || kept == nil || kept.Signature != journal.Signature || r.count("bridge_expired") != 0 {
			t.Fatalf("at height %d: %d broadcasts, journal %+v, steps %s", height, len(r.broadcasts), kept, r.kinds())
		}
	}
	r.height = journal.LastValidBlockHeight + expiryGraceBlocks + 1
	r.sendDrop = 0
	if wait := r.step(f, request); wait != "bridge send" {
		t.Fatal(wait)
	}
	fresh := r.current().Bridge
	if r.count("bridge_expired") != 1 || len(r.broadcasts) != 3 || fresh == nil || fresh.Signature == journal.Signature {
		t.Fatalf("steps %s, %d broadcasts, journal %+v", r.kinds(), len(r.broadcasts), fresh)
	}
	r.ensureFunded(f, request)
	if len(r.sends) != 1 || r.sends[0] != 7*raoPerTAO || r.tao != 0 || len(r.broadcasts) != 3 {
		t.Fatalf("sends %v, TAO %d, %d broadcasts", r.sends, r.tao, len(r.broadcasts))
	}
}

// A send that turns up during the grace is followed like any other.
func TestASendThatLandsInTheGraceIsFollowed(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.sendDrop = 1
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.step(f, request)
	journal := r.current().Bridge
	r.height = journal.LastValidBlockHeight + expiryGraceBlocks/2
	r.statuses[journal.Signature] = &solana.SignatureStatus{ConfirmationStatus: "confirmed"}
	r.tao -= journal.AmountRao
	r.pending = append(r.pending, &fakeDelivery{wei: raoWei(journal.AmountRao), after: 1})
	if wait := r.step(f, request); !strings.Contains(wait, "LayerZero delivery") {
		t.Fatal(wait)
	}
	if kept := r.current().Bridge; kept == nil || kept.LandedAt.IsZero() || r.count("bridged") != 1 {
		t.Fatalf("journal %+v, steps %s", kept, r.kinds())
	}
	r.ensureFunded(f, request)
	if len(r.broadcasts) != 1 || r.count("bridge_expired") != 0 {
		t.Fatalf("%d broadcasts, steps %s", len(r.broadcasts), r.kinds())
	}
}

// A landed swap the node no longer knows is still found, by its balances.
func TestALandedSwapWhoseStatusIsGoneIsBookedByItsUSDC(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.executeLost = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	journal := r.current().Swap
	delete(r.statuses, journal.Signature)
	r.height = journal.LastValidBlockHeight + expiryGraceBlocks + 1
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.count("swapped") != 1 || r.count("swap_failed") != 0 {
		t.Fatalf("%d swaps, steps %s", len(r.swaps), r.kinds())
	}
}

// A landed send the node no longer knows reads as never landed, though its TAO has
// left. With delivery still under way the wallet looks short, and must not buy.
func TestALandedSendWhoseStatusIsGoneIsNotBoughtAgain(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.usdc = 5_000 * microPerUSDC
	r.deliverAfter = 1_000_000
	f := r.funder(t)
	request := deposit(7, 1_656)
	if wait := r.step(f, request); wait != "bridge send" {
		t.Fatal(wait)
	}
	journal := r.current().Bridge
	if r.tao != 0 || len(r.sends) != 1 {
		t.Fatalf("TAO %d, %d sends", r.tao, len(r.sends))
	}
	delete(r.statuses, journal.Signature)
	r.height = journal.LastValidBlockHeight + expiryGraceBlocks + 1
	_, wait, err := f.step(context.Background(), request)
	if len(r.swaps) != 0 || r.current().Bridge == nil {
		t.Fatalf("7 TAO on the way and still %d swaps for %s USDC: steps %s, step waits on %q, err %v",
			len(r.swaps), microString(usdcOf(r.swaps)), r.kinds(), wait, err)
	}
}

// Ensure gives up after its budget of time with a report, and leaves what it waits
// for in place.
func TestEnsureReportsWaitingAfterItsBudget(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.deliverAfter = 1_000_000
	start := r.clock
	result, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000))
	if err != nil {
		t.Fatal(err)
	}
	if result.Funded || !strings.Contains(result.Waiting, "LayerZero delivery") || result.AlphaRao == nil || result.AlphaRao.Sign() != 0 {
		t.Fatalf("%+v", result)
	}
	if r.sleeps != int(ensureBudget/pollInterval) || r.clock.Sub(start) != ensureBudget {
		t.Fatalf("%d sleeps over %s", r.sleeps, r.clock.Sub(start))
	}
	if r.current().Bridge == nil || len(r.sends) != 1 {
		t.Fatalf("state %+v, %d sends", r.current(), len(r.sends))
	}
}

// So does a swap that a stuck node never lets resolve.
func TestEnsureReportsASwapThatNeverResolves(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.executeDrop = true
	r.frozenHeight = true
	result, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656))
	if err != nil {
		t.Fatal(err)
	}
	if result.Funded || result.Waiting != "swap landing" || r.sleeps != int(ensureBudget/pollInterval) {
		t.Fatalf("%+v after %d sleeps", result, r.sleeps)
	}
	if r.current().Swap == nil || r.executes != 1 || r.orderCalls != 3 {
		t.Fatalf("state %+v, %d executes, %d orders", r.current(), r.executes, r.orderCalls)
	}
}

// Cancelling during the real sleep returns at once, with what is under way
// journaled for the next run.
func TestCancellingDuringASleepReturnsPromptly(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.realSleep = true
	f := r.funder(t)
	request := deposit(7, 1_000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(20*time.Millisecond, cancel)
	started := time.Now()
	result, err := f.Ensure(ctx, request)
	if !errors.Is(err, context.Canceled) || result != nil {
		t.Fatal(result, err)
	}
	if elapsed := time.Since(started); elapsed >= pollInterval/3 {
		t.Fatal("returned after", elapsed)
	}
	if journal := r.current().Bridge; journal == nil || len(r.sends) != 1 {
		t.Fatalf("journal %+v, %d sends", journal, len(r.sends))
	}
	r.realSleep = false
	r.ensureFunded(r.funder(t), request)
	if len(r.sends) != 1 {
		t.Fatalf("%d sends", len(r.sends))
	}
}

// A read that fails anywhere in a step stops it with the state untouched, and the
// next run goes on as if nothing had happened.
func TestAReadErrorMidStepLeavesTheStateIntact(t *testing.T) {
	down := errors.New("the node is down")
	holdNative := func(r *rig) { r.nativeWei = taoWei(5) }
	holdWTAO := func(r *rig) { r.wtaoWei = taoWei(5) }
	holdSolana := func(r *rig) { r.tao = 5 * raoPerTAO }
	failing := func(callSelector []byte) func(*rig) {
		return func(r *rig) { r.failRead(callSelector, down) }
	}
	for _, test := range []struct {
		name  string
		setup func(*rig)
		fault func(*rig)
		want  string
	}{
		{"the alpha position", holdNative, failing(getStakeSelector), "read the alpha position"},
		{"the wTAO balance", holdNative, failing(balanceOfSelector), "read the wTAO balance"},
		{"the alpha price", holdNative, failing(alphaPriceSelector), "read the alpha price"},
		{"the pool's swap", holdNative, failing(simSwapSelector), "simulate the pool swap"},
		{"the native balance", holdNative, func(r *rig) { r.failNative = down }, "read the TAO balance"},
		{"the pool preparing a stake", holdNative, failing(taoInPoolSelector), "stake TAO"},
		{"the nonce preparing a stake", holdNative, func(r *rig) { r.failNonce = down }, "stake TAO"},
		{"the nonce preparing an unwrap", holdWTAO, func(r *rig) { r.failNonce = down }, "unwrap wTAO"},
		{"wTAO's peer starting a bridge", holdSolana, failing(peersSelector), "read wTAO's peer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.seed(&State{Epoch: 6, SpentUSDCMicro: 5 * microPerUSDC})
			test.setup(r)
			test.fault(r)
			f := r.funder(t)
			saved, version := bytes.Clone(r.stateJSON), r.version
			_, err := f.Ensure(context.Background(), deposit(7, 1_000))
			if !errors.Is(err, down) || !strings.Contains(err.Error(), test.want) {
				t.Fatal(err)
			}
			if !bytes.Equal(r.stateJSON, saved) || r.version != version || len(r.operations) != 0 || len(r.broadcasts) != 0 || len(r.events) != 0 {
				t.Fatalf("state %s, operations %v, %d broadcasts, steps %s", r.stateJSON, r.operations, len(r.broadcasts), r.kinds())
			}
			r.answer, r.failNative, r.failNonce = nil, nil, nil
			r.ensureFunded(f, deposit(7, 1_000))
		})
	}
}

// With a send on its way a read error keeps its journal, and does not end the wait.
func TestAnEVMReadErrorWhileABridgeIsInFlightKeepsItsJournal(t *testing.T) {
	down := errors.New("the node is down")
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	f := r.funder(t)
	request := deposit(7, 1_000)
	r.step(f, request)
	r.step(f, request) // its landing is noted
	saved := bytes.Clone(r.stateJSON)
	r.failRead(balanceOfSelector, down)
	if _, err := f.Ensure(context.Background(), request); !errors.Is(err, down) {
		t.Fatal(err)
	}
	if !bytes.Equal(r.stateJSON, saved) || r.current().Bridge == nil {
		t.Fatalf("state %s", r.stateJSON)
	}
	r.answer = nil
	r.ensureFunded(f, request)
	if len(r.sends) != 1 || len(r.broadcasts) != 1 {
		t.Fatalf("%d sends, %d broadcasts", len(r.sends), len(r.broadcasts))
	}
}

// Solana reads failing around a swap in flight keep its journal, with nothing bought
// again once they recover.
func TestASolanaReadErrorWhileASwapIsInFlightKeepsItsJournal(t *testing.T) {
	down := errors.New("the node is down")
	failTokenAccount := func(r *rig, account solana.PublicKey) {
		r.failToken = func(address solana.PublicKey) error {
			if address == account {
				return down
			}
			return nil
		}
	}
	for _, test := range []struct {
		name   string
		landed bool
		fault  func(r *rig, journal *SwapRecord)
	}{
		{"its signature status", false, func(r *rig, _ *SwapRecord) { r.failStatuses = down }},
		{"the block height", false, func(r *rig, _ *SwapRecord) { r.failHeight = down }},
		{"the USDC at expiry", false, func(r *rig, journal *SwapRecord) {
			r.height = journal.LastValidBlockHeight + expiryGraceBlocks + 1
			failTokenAccount(r, r.usdcAccount)
		}},
		{"the TAO as it finishes", true, func(r *rig, _ *SwapRecord) { failTokenAccount(r, r.taoAccount) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			r.executeLost = test.landed
			r.executeDrop = !test.landed
			f := r.funder(t)
			request := deposit(7, 1_656)
			r.step(f, request)
			saved := bytes.Clone(r.stateJSON)
			test.fault(r, r.current().Swap)
			if _, err := f.Ensure(context.Background(), request); !errors.Is(err, down) {
				t.Fatal(err)
			}
			if !bytes.Equal(r.stateJSON, saved) {
				t.Fatalf("state %s", r.stateJSON)
			}
			r.failToken, r.failStatuses, r.failHeight = nil, nil, nil
			r.executeDrop = false
			r.ensureFunded(f, request)
			if len(r.swaps) != 1 || r.current().SpentUSDCMicro != r.swaps[0].amount {
				t.Fatalf("%d swaps, state %+v", len(r.swaps), r.current())
			}
		})
	}
}

// A TAO read failing just after /execute reported success keeps the journal, and the
// next run settles it: by signature, or with another payer, by the balance.
func TestATaoReadErrorAfterASuccessfulSwapKeepsItsJournal(t *testing.T) {
	for name, payer := range map[string]*otherPayer{"the wallet pays": nil, "another account pays": {gasless: true}} {
		t.Run(name, func(t *testing.T) {
			down := errors.New("the node is down")
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			r.payer = payer
			r.afterExecute = func() {
				r.failToken = func(address solana.PublicKey) error {
					if address == r.taoAccount {
						return down
					}
					return nil
				}
			}
			f := r.funder(t)
			request := deposit(7, 1_656)
			if _, err := f.Ensure(context.Background(), request); !errors.Is(err, down) {
				t.Fatal(err)
			}
			state := r.current()
			if len(r.swaps) != 1 || state.Swap == nil || r.count("swapped") != 0 {
				t.Fatalf("%d swaps, state %+v, steps %s", len(r.swaps), state, r.kinds())
			}
			r.failToken, r.afterExecute = nil, nil
			r.ensureFunded(f, request)
			if len(r.swaps) != 1 || r.executes != 1 || r.count("swapped") != 1 {
				t.Fatalf("%d swaps, %d executes, steps %s", len(r.swaps), r.executes, r.kinds())
			}
		})
	}
}

// Every swap reported expired costs nothing and returns its budget, but they do not
// go on for ever.
func TestExpiringSwapsStopAtTheActionLimit(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.executeCode = -1005
	result, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656))
	if err != nil {
		t.Fatal(err)
	}
	if result.Funded || !strings.Contains(result.Waiting, "conversion steps ran") {
		t.Fatalf("%+v", result)
	}
	state := r.current()
	if r.executes != maxActionsPerEnsure || r.count("swap_failed") != maxActionsPerEnsure || state.Swap != nil || state.SpentUSDCMicro != 0 || len(r.swaps) != 0 {
		t.Fatalf("%d executes, steps %s, state %+v", r.executes, r.kinds(), state)
	}
}

// The gate before signing: any change to the wallet beyond exactly the swap is
// refused, with nothing journaled and nothing sent.
func TestTheSwapSimulationGateRefuses(t *testing.T) {
	someone := solana.PublicKey{9, 9, 9}
	for _, test := range []struct {
		name string
		edit func(r *rig, sim *solana.Simulation)
		want string
	}{
		{"a delegate on the USDC account", func(_ *rig, sim *solana.Simulation) { sim.Accounts[0].Token.Delegate = &someone }, "owner, delegate or close authority"},
		{"a close authority on the USDC account", func(_ *rig, sim *solana.Simulation) { sim.Accounts[0].Token.CloseAuthority = &someone }, "owner, delegate or close authority"},
		{"another owner on the USDC account", func(_ *rig, sim *solana.Simulation) { sim.Accounts[0].Token.Owner = someone }, "owner, delegate or close authority"},
		{"another mint on the USDC account", func(_ *rig, sim *solana.Simulation) { sim.Accounts[0].Token.Mint = someone }, "owner, delegate or close authority"},
		{"the USDC account closed", func(_ *rig, sim *solana.Simulation) { sim.Accounts[0] = solana.SimulatedAccount{} }, "closes the wallet's USDC account"},
		{"more USDC moved", func(_ *rig, sim *solana.Simulation) { sim.Accounts[0].Token.Amount-- }, "simulation moves"},
		{"less USDC moved", func(_ *rig, sim *solana.Simulation) { sim.Accounts[0].Token.Amount++ }, "simulation moves"},
		{"another owner on the TAO account", func(_ *rig, sim *solana.Simulation) { sim.Accounts[1].Token.Owner = someone }, "does not pay the wallet's own TAO account"},
		{"another mint on the TAO account", func(_ *rig, sim *solana.Simulation) { sim.Accounts[1].Token.Mint = someone }, "does not pay the wallet's own TAO account"},
		{"a delegate on the TAO account", func(_ *rig, sim *solana.Simulation) { sim.Accounts[1].Token.Delegate = &someone }, "does not pay the wallet's own TAO account"},
		{"a close authority on the TAO account", func(_ *rig, sim *solana.Simulation) { sim.Accounts[1].Token.CloseAuthority = &someone }, "does not pay the wallet's own TAO account"},
		{"no TAO account", func(_ *rig, sim *solana.Simulation) { sim.Accounts[1] = solana.SimulatedAccount{} }, "does not pay the wallet's own TAO account"},
		{"TAO below the floor", func(_ *rig, sim *solana.Simulation) { sim.Accounts[1].Token.Amount = 1 }, "below the floor"},
		{"more SOL spent than a swap may", func(r *rig, sim *solana.Simulation) { sim.Accounts[2].Lamports = r.lamports - maxSwapLamports - 1 }, "lamports of the wallet's SOL"},
		{"the wallet closed", func(_ *rig, sim *solana.Simulation) { sim.Accounts[2].Exists = false }, "closes the wallet"},
		{"the wallet handed to another program", func(_ *rig, sim *solana.Simulation) { sim.Accounts[2].Owner = someone }, "hands the wallet"},
		{"data left in the wallet", func(_ *rig, sim *solana.Simulation) { sim.Accounts[2].DataLen = 1 }, "bytes of data"},
		{"too few accounts returned", func(_ *rig, sim *solana.Simulation) { sim.Accounts = sim.Accounts[:2] }, "returned 2 accounts"},
		{"a simulated failure", func(_ *rig, sim *solana.Simulation) { sim.Err = json.RawMessage(`"BlockhashNotFound"`) }, "simulation failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			r.editSim = func(sim *solana.Simulation) { test.edit(r, sim) }
			_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656))
			if err == nil || !strings.Contains(err.Error(), "refused Jupiter's transaction") || !strings.Contains(err.Error(), test.want) {
				t.Fatal(err)
			}
			r.requireNoSwapJournal()
		})
	}
}

// Its limit on SOL admits exactly that much.
func TestTheSwapSimulationGateAdmitsItsLimit(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.editSim = func(sim *solana.Simulation) {
		if len(sim.Accounts) == 3 {
			sim.Accounts[2].Lamports = r.lamports - maxSwapLamports
		}
	}
	r.step(r.funder(t), deposit(7, 1_656))
	if len(r.swaps) != 1 {
		t.Fatalf("%d swaps", len(r.swaps))
	}
}

// The gate promises nothing else of the wallet's changes, and its owner is the most
// that a transaction it signs could change: another program would then hold its SOL.
func TestTheSwapGateRefusesAWalletAssignedToAnotherProgram(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.editSim = func(sim *solana.Simulation) {
		if len(sim.Accounts) == 3 {
			sim.Accounts[2].Owner = solana.PublicKey{9, 9, 9}
		}
	}
	_, _, err := r.funder(t).step(context.Background(), deposit(7, 1_656))
	if err == nil || r.executes != 0 {
		t.Fatalf("signed and sent a swap that hands the wallet to another program: %d executes, err %v", r.executes, err)
	}
	r.requireNoSwapJournal()
}

// The bridge send's gate: other than exactly its TAO, or past its cap on SOL, is
// refused before anything is journaled or broadcast.
func TestTheBridgeSimulationGateRefuses(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(r *rig, sim *solana.Simulation)
		want string
	}{
		{"one rao more TAO moved", func(_ *rig, sim *solana.Simulation) { sim.Accounts[0].Token.Amount-- }, "does not move exactly its TAO"},
		{"one rao less TAO moved", func(_ *rig, sim *solana.Simulation) { sim.Accounts[0].Token.Amount++ }, "does not move exactly its TAO"},
		{"no TAO account", func(_ *rig, sim *solana.Simulation) { sim.Accounts[0].Token = nil }, "does not move exactly its TAO"},
		{"too few accounts returned", func(_ *rig, sim *solana.Simulation) { sim.Accounts = sim.Accounts[:1] }, "does not move exactly its TAO"},
		{"more SOL spent than the cap", func(r *rig, sim *solana.Simulation) {
			sim.Accounts[1].Lamports = r.lamports - r.cfg.Conversion.MaxBridgeFeeLamports - bridgeTxLamports - 1
		}, "spends"},
		{"a simulated failure", func(_ *rig, sim *solana.Simulation) { sim.Err = json.RawMessage(`"BlockhashNotFound"`) }, "does not simulate"},
		{"the wallet closed", func(_ *rig, sim *solana.Simulation) { sim.Accounts[1].Exists = false }, "closes the wallet"},
		{"the wallet handed to another program", func(_ *rig, sim *solana.Simulation) { sim.Accounts[1].Owner = solana.PublicKey{9, 9, 9} }, "hands the wallet"},
		{"data left in the wallet", func(_ *rig, sim *solana.Simulation) { sim.Accounts[1].DataLen = 1 }, "bytes of data"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.tao = 7*raoPerTAO + 5
			r.editSim = func(sim *solana.Simulation) { test.edit(r, sim) }
			_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatal(err)
			}
			r.requireNoBridgeJournal()
			if r.tao != 7*raoPerTAO+5 {
				t.Fatal("TAO", r.tao)
			}
		})
	}
}

// Its cap admits exactly that much.
func TestTheBridgeSimulationGateAdmitsItsCap(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.editSim = func(sim *solana.Simulation) {
		sim.Accounts[1].Lamports = r.lamports - r.cfg.Conversion.MaxBridgeFeeLamports - bridgeTxLamports
	}
	r.step(r.funder(t), deposit(7, 1_000))
	if len(r.sends) != 1 {
		t.Fatalf("%d sends", len(r.sends))
	}
}

func TestABridgeWithoutSOLSendsNothing(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.lamports = r.cfg.Conversion.MaxBridgeFeeLamports + bridgeTxLamports - 1
	if _, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000)); !errors.Is(err, ErrInsufficientSOL) {
		t.Fatal(err)
	}
	r.requireNoBridgeJournal()
}

// The order the wallet would sign has to clear the floor too, not just its quote.
func TestAnOrderBelowTheFloorIsRefused(t *testing.T) {
	for name, edit := range map[string]func(*jupiter.Order){
		"its output":  func(order *jupiter.Order) { order.OutAmount /= 2 },
		"its minimum": func(order *jupiter.Order) { order.OtherAmountThreshold /= 2 },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			r.editOrder = func(order *jupiter.Order, req jupiter.OrderRequest) {
				if req.Taker != "" {
					edit(order)
				}
			}
			if _, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656)); !errors.Is(err, ErrPriceGuard) {
				t.Fatal(err)
			}
			if r.simulations != 0 {
				t.Fatal("simulated an order below the floor")
			}
			r.requireNoSwapJournal()
		})
	}
}

// An ask that is no price at all must not disable the guard it sizes.
func TestANonsenseAskSpendsNothing(t *testing.T) {
	for name, ask := range map[string]float64{"not a number": math.NaN(), "infinite": math.Inf(1), "zero": 0} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			r.ask = ask
			r.jupiterPrice = 2_400 // ten times too dear
			if _, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656)); err == nil {
				t.Fatal("no refusal")
			}
			r.requireNoSwapJournal()
		})
	}
}

// TAO the wallet already holds is no part of what a swap pays.
func TestTheSwapGateCountsOnlyWhatArrives(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.tao = 6 * raoPerTAO
	r.editSim = func(sim *solana.Simulation) { sim.Accounts[1].Token.Amount = r.tao + 1 }
	_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656))
	if err == nil || !strings.Contains(err.Error(), "below the floor") {
		t.Fatal(err)
	}
	r.requireNoSwapJournal()
}

// What the wallet signs must not accept less than the floor, so its slippage is cut
// to the room between the quote and the floor.
func TestTheOrderSlippageKeepsTheFloor(t *testing.T) {
	r := newRig(t, enabled+"\n  max_price_gap: 0.005")
	r.usdc = 5_000 * microPerUSDC
	var bps int
	var out uint64
	r.editOrder = func(order *jupiter.Order, req jupiter.OrderRequest) {
		if req.Taker != "" {
			bps, out = req.SlippageBps, order.OutAmount
		}
	}
	r.step(r.funder(t), deposit(7, 1_656))
	floor := r.last("swapped").Data["min_out_rao"].(uint64)
	if worst := out - out*uint64(bps)/10_000; bps <= 0 || worst < floor {
		t.Fatalf("%d bps of %d rao accepts %d, below the floor of %d", bps, out, worst, floor)
	}
}

// A quote short of what is needed is scaled up, so that one swap is enough even with
// no buffer to absorb the difference.
func TestAQuoteShortOfTheNeedIsScaledUp(t *testing.T) {
	r := newRig(t, enabled+"\n  alpha_buffer_bps: 0")
	r.usdc = 5_000 * microPerUSDC
	r.jupiterPrice = 241.5 // past the sizing margin over the ask, within the guard
	r.ensureFunded(r.funder(t), deposit(7, 1_656))
	if len(r.swaps) != 1 {
		t.Fatalf("%d swaps", len(r.swaps))
	}
}

// Before USDC moves the route must be able to finish: gas for the unwrap, and a hotkey
// that takes stake. Otherwise the TAO would only be parked partway along it.
func TestARouteThatCannotFinishBuysNothing(t *testing.T) {
	for _, test := range []struct {
		name  string
		fault func(*rig)
		want  error
	}{
		{"no gas", func(r *rig) { r.nativeWei = new(big.Int) }, ErrInsufficientGas},
		{"a rao of gas", func(r *rig) { r.nativeWei = raoWei(1) }, ErrInsufficientGas},
		{"a hotkey that cannot take stake", func(r *rig) {
			r.hotkeyErr = errors.New("dispatch execution failed: HotKeyAccountNotExists")
		}, ErrHotkeyNotReady},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			test.fault(r)
			if _, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656)); !errors.Is(err, test.want) {
				t.Fatal(err)
			}
			if r.orderCalls != 0 {
				t.Fatalf("%d orders", r.orderCalls)
			}
			r.requireNoSwapJournal()
		})
	}
}

// A check cut short by cancelling says nothing about the hotkey.
func TestACancelledHotkeyCheckIsNotAHotkeyFault(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.answer = func(_ common.Address, data []byte) ([]byte, bool, error) {
		if !bytes.HasPrefix(data, addStakeLimitSelector) {
			return nil, false, nil
		}
		cancel()
		return nil, true, errors.New("the call was cut short")
	}
	_, err := r.funder(t).Ensure(ctx, deposit(7, 1_656))
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrHotkeyNotReady) {
		t.Fatal(err)
	}
	r.requireNoSwapJournal()
}

// The gate compares the simulation with USDC read after the order, so USDC arriving
// meanwhile is no reason to refuse, and USDC leaving is refused as too little.
func TestUSDCMovingDuringTheOrder(t *testing.T) {
	moved := func(r *rig, to uint64) {
		r.editOrder = func(_ *jupiter.Order, req jupiter.OrderRequest) {
			if req.Taker != "" {
				r.usdc = to
			}
		}
	}
	t.Run("in", func(t *testing.T) {
		r := newRig(t, enabled)
		r.usdc = 5_000 * microPerUSDC
		moved(r, 6_000*microPerUSDC)
		r.step(r.funder(t), deposit(7, 1_656))
		if len(r.swaps) != 1 || r.usdc != 6_000*microPerUSDC-r.swaps[0].amount {
			t.Fatalf("%d swaps, USDC %d", len(r.swaps), r.usdc)
		}
	})
	t.Run("out", func(t *testing.T) {
		r := newRig(t, enabled)
		r.usdc = 5_000 * microPerUSDC
		moved(r, 1*microPerUSDC)
		if _, _, err := r.funder(t).step(context.Background(), deposit(7, 1_656)); !errors.Is(err, ErrInsufficientUSDC) {
			t.Fatal(err)
		}
		r.requireNoSwapJournal()
	})
}

// An order paying above the floor, with a minimum below it, is slippage sized from a
// better quote, and gets one more order sized from itself.
func TestAnOrderWithItsMinimumBelowTheFloorIsPlacedOnceMore(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	takers := 0
	r.editOrder = func(order *jupiter.Order, req jupiter.OrderRequest) {
		if req.Taker != "" {
			if takers++; takers == 1 {
				order.OtherAmountThreshold /= 2
			}
		}
	}
	r.ensureFunded(r.funder(t), deposit(7, 1_656))
	if len(r.swaps) != 1 || takers != 2 || r.orderCalls != 4 {
		t.Fatalf("%d swaps, %d taker orders, %d orders", len(r.swaps), takers, r.orderCalls)
	}
}

// With no room between what it pays and the floor, no slippage could hold, so it is
// refused without a second order.
func TestAnOrderPayingExactlyTheFloorWithALowerMinimumIsRefused(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.editOrder = func(order *jupiter.Order, req jupiter.OrderRequest) {
		if req.Taker != "" {
			order.OutAmount = swapFloor(req.Amount, r.ask, r.cfg.Conversion.MaxPriceGap)
			order.OtherAmountThreshold = order.OutAmount - 1
		}
	}
	if _, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656)); !errors.Is(err, ErrPriceGuard) {
		t.Fatal(err)
	}
	if r.orderCalls != 3 {
		t.Fatalf("%d orders", r.orderCalls)
	}
	r.requireNoSwapJournal()
}

// A swap journaled but never sent is not sent once it can no longer land, by whichever
// clock its order named: a new order is better than a stale one.
func TestASwapThatNeverWentOutIsNotSentOnceExpired(t *testing.T) {
	for _, test := range []struct {
		name   string
		payer  *otherPayer
		expire func(r *rig, journal *SwapRecord)
	}{
		{"past its height", nil, func(r *rig, journal *SwapRecord) { r.height = journal.LastValidBlockHeight + expiryGraceBlocks + 1 }},
		{"past its expireAt", &otherPayer{gasless: true}, func(r *rig, _ *SwapRecord) { r.expireRFQ() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			r.payer = test.payer
			f := r.funder(t)
			request := deposit(7, 1_656)
			r.loseSave(2)
			if _, _, err := f.step(context.Background(), request); !errors.Is(err, ErrStale) {
				t.Fatal(err)
			}
			journal := r.current().Swap
			if journal == nil || !journal.SubmittedAt.IsZero() || r.executes != 0 {
				t.Fatalf("journal %+v after %d executes", journal, r.executes)
			}
			test.expire(r, journal)
			r.ensureFunded(f, request)
			if r.count("swap_failed") != 1 || len(r.swaps) != 1 || r.executes != 1 || r.executed[0] == journal.Raw {
				t.Fatalf("steps %s, %d swaps, %d executes", r.kinds(), len(r.swaps), r.executes)
			}
			if spent := r.current().SpentUSDCMicro; spent != r.swaps[0].amount {
				t.Fatalf("spent %d for a swap of %d", spent, r.swaps[0].amount)
			}
		})
	}
}

// A fault at any point up to the signature spends nothing.
func TestQuoteAndOrderFaultsSpendNothing(t *testing.T) {
	down := errors.New("jupiter is down")
	failOrderCall := func(nth int) func(*rig) {
		return func(r *rig) {
			r.failOrder = func(call int) error {
				if call == nth {
					return down
				}
				return nil
			}
		}
	}
	for _, test := range []struct {
		name  string
		fault func(*rig)
		want  string
	}{
		{"the sizing quote", failOrderCall(1), "quote the swap"},
		{"the guarded quote", failOrderCall(2), "quote the swap"},
		{"the order", failOrderCall(3), "order the swap"},
		{"the market", func(r *rig) { r.failAsk = errors.New("the market is down") }, "the market is down"},
		{"a transaction that does not parse", func(r *rig) {
			r.editOrder = func(order *jupiter.Order, req jupiter.OrderRequest) {
				if req.Taker != "" {
					order.Transaction = []byte{0x80}
				}
			}
		}, "Jupiter's transaction"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			test.fault(r)
			_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatal(err)
			}
			r.requireNoSwapJournal()
		})
	}
}

// A bridge whose accounts are missing or changed is refused, not guessed at.
func TestAChangedBridgeAccountSendsNothing(t *testing.T) {
	missing := func(which solana.PublicKey) func(*rig) {
		return func(r *rig) {
			r.editAccount = func(address solana.PublicKey, account *solana.Account) *solana.Account {
				if address == which {
					return nil
				}
				return account
			}
		}
	}
	for _, test := range []struct {
		name    string
		fault   func(*rig)
		changed bool
	}{
		{"the peer config missing", missing(bridge.PeerConfig), true},
		{"the lookup table missing", missing(bridge.LookupTable), true},
		{"the peer config under another program", func(r *rig) {
			r.editAccount = func(address solana.PublicKey, account *solana.Account) *solana.Account {
				if address == bridge.PeerConfig {
					account.Owner = solana.PublicKey{9, 9, 9}
				}
				return account
			}
		}, true},
		{"the peer config cut short", func(r *rig) { r.peerData = r.peerData[:10] }, true},
		{"wTAO's peer cut short", func(r *rig) {
			r.answer = func(_ common.Address, data []byte) ([]byte, bool, error) {
				return r.peerStore[:31], bytes.HasPrefix(data, peersSelector), nil
			}
		}, true},
		{"a lookup table that does not parse", func(r *rig) { r.table = []byte{1, 2, 3} }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.tao = 7 * raoPerTAO
			test.fault(r)
			_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000))
			if err == nil || errors.Is(err, ErrBridgeChanged) != test.changed {
				t.Fatal(err)
			}
			r.requireNoBridgeJournal()
		})
	}
}

// The fake answers whatever it is asked, so this reads what it was asked: every read
// names the wallet, its hotkey and the subnet.
func TestTheChainIsAskedAboutTheRightAccounts(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	asked := map[string][]byte{}
	r.answer = func(_ common.Address, data []byte) ([]byte, bool, error) {
		asked[string(data[:4])] = bytes.Clone(data)
		return nil, false, nil
	}
	r.ensureFunded(r.funder(t), deposit(7, 1_000))
	for name, want := range map[string][]byte{
		"getStake":      packGetStake(r.cfg.Hotkey, ss58.EvmMirrorPubkey(r.cfg.AlphaAddress), testNetuid),
		"balanceOf":     packBalanceOf(r.cfg.AlphaAddress),
		"peers":         packPeers(bridge.SolanaEID),
		"getAlphaPrice": packAlphaPrice(testNetuid),
		"getTaoInPool":  packTaoInPool(testNetuid),
		"simSwap":       packSimSwap(testNetuid, new(big.Int).SetBytes(asked[string(simSwapSelector)][36:68]).Uint64()),
	} {
		if got := asked[string(want[:4])]; !bytes.Equal(got, want) {
			t.Fatalf("%s asked %x, want %x", name, got, want)
		}
	}
}

// The send names the alpha wallet, on Bittensor, for exactly its amount, under the
// configured cap, and is the wallet's own to sign and pay for.
func TestABridgeSendPaysTheAlphaWallet(t *testing.T) {
	r := newRig(t, enabled)
	r.tao = 7 * raoPerTAO
	r.step(r.funder(t), deposit(7, 1_000))
	raw, err := base64.StdEncoding.DecodeString(r.broadcasts[0])
	if err != nil {
		t.Fatal(err)
	}
	tx, err := solana.ParseTransaction(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := bridge.Send{Sender: r.cfg.USDCAddress, To: r.cfg.AlphaAddress, AmountRao: 7 * raoPerTAO, MaxFeeLamports: r.cfg.Conversion.MaxBridgeFeeLamports}.Data()
	if !bytes.Contains(tx.Message, want) {
		t.Fatalf("the send does not carry %x", want)
	}
	if tx.FeePayer() != r.cfg.USDCAddress || !tx.SignedBy(r.cfg.USDCAddress) {
		t.Fatal("not the wallet's own transaction")
	}
}

// At the realistic numbers below, the limit add_stake_limit gets is in its own unit,
// rao per whole alpha, where 1 TAO per alpha is 1,000,000,000.
func TestStakeLimitOnRealisticNumbers(t *testing.T) {
	const (
		priceWei = 4_153_920_000_000_000 // 0.00415392 TAO per alpha, at 18 decimals
		poolRao  = 300_000 * raoPerTAO
		stakeRao = 6_700_000_000
	)
	w := newWorld(t, enabled)
	w.price, w.poolRao = big.NewInt(priceWei), poolRao
	limit, err := w.funder(t).stakeLimit(context.Background(), stakeRao)
	if err != nil {
		t.Fatal(err)
	}
	// By hand: 4,153,920 rao per alpha, times 1.01 is 4,195,459.2, and the purchase
	// adds a factor of (1 + 6.7/300,000)² = 1.0000447, or 187 rao.
	if limit < 4_195_459 || limit > 4_195_700 {
		t.Fatalf("limit %d rao per alpha", limit)
	}
	spot := big.NewRat(priceWei, 1_000_000_000)
	growth := big.NewRat(poolRao+stakeRao, poolRao)
	exact := new(big.Rat).Mul(new(big.Rat).Mul(spot, new(big.Rat).Mul(growth, growth)), big.NewRat(101, 100))
	if want, _ := exact.Float64(); math.Abs(float64(limit)-want) > 1 {
		t.Fatalf("limit %d, independently %.3f", limit, want)
	}
	if ratio := float64(limit) * 1e9 / priceWei; ratio < 1.01 || ratio > 1.0101 {
		t.Fatalf("the limit is %f of the price: off by a unit", ratio)
	}
	// A constant-product pool of that price, bought from by the stake, ends below the
	// limit by the tolerance, so the fill goes through and no worse one would.
	taoIn := big.NewRat(poolRao, 1)
	alphaIn := new(big.Rat).Quo(taoIn, spot)
	taoAfter := new(big.Rat).Add(taoIn, big.NewRat(stakeRao, 1))
	alphaAfter := new(big.Rat).Quo(new(big.Rat).Mul(taoIn, alphaIn), taoAfter)
	after, _ := new(big.Rat).Quo(taoAfter, alphaAfter).Float64()
	if margin := float64(limit) / after; margin < 1.00999 || margin > 1.01001 {
		t.Fatalf("the limit %d is %f of the pool's price after the fill, %f", limit, margin, after)
	}
}

// The stake sends the limit computed for the amount it sends.
func TestAStakeCarriesTheLimitItComputed(t *testing.T) {
	r := newRig(t, enabled)
	r.price, r.poolRao = big.NewInt(4_153_920_000_000_000), 300_000*raoPerTAO
	r.nativeWei = taoWei(10)
	f := r.funder(t)
	var sentAmount, sentLimit uint64
	r.chain = func(_ string, to common.Address, data []byte) error {
		if to == StakingPrecompile {
			sentAmount = new(big.Int).SetBytes(data[36:68]).Uint64()
			sentLimit = new(big.Int).SetBytes(data[68:100]).Uint64()
		}
		return nil
	}
	r.ensureFunded(f, deposit(7, 1_600))
	// 1,600 alpha at this price is about 6.65 TAO, and 6.71 with its buffer.
	if sentAmount < 6_700_000_000 || sentAmount > 6_720_000_000 {
		t.Fatal("staked", sentAmount)
	}
	want, err := f.stakeLimit(context.Background(), sentAmount) // the fake's pool does not move
	if err != nil {
		t.Fatal(err)
	}
	if sentLimit != want || sentLimit < 4_195_459 || sentLimit > 4_195_700 {
		t.Fatalf("sent limit %d, want %d", sentLimit, want)
	}
	event := r.last("staked")
	if event.Data["tao_rao"] != sentAmount || event.Data["limit_rao_per_alpha"] != sentLimit {
		t.Fatalf("booked %+v", event.Data)
	}
}

// funderABI declares the calls the conversion makes as their contracts publish
// them, for go-ethereum to encode independently of evm.go.
const funderABI = `[
	{"type":"function","name":"withdraw","stateMutability":"nonpayable","outputs":[],
	 "inputs":[{"name":"wad","type":"uint256"}]},
	{"type":"function","name":"balanceOf","stateMutability":"view","outputs":[{"name":"","type":"uint256"}],
	 "inputs":[{"name":"owner","type":"address"}]},
	{"type":"function","name":"peers","stateMutability":"view","outputs":[{"name":"","type":"bytes32"}],
	 "inputs":[{"name":"eid","type":"uint32"}]},
	{"type":"function","name":"addStakeLimit","stateMutability":"payable","outputs":[],
	 "inputs":[{"name":"hotkey","type":"bytes32"},{"name":"amountRao","type":"uint256"},{"name":"limitPriceRao","type":"uint256"},{"name":"allowPartial","type":"bool"},{"name":"netuid","type":"uint256"}]},
	{"type":"function","name":"getStake","stateMutability":"view","outputs":[{"name":"","type":"uint256"}],
	 "inputs":[{"name":"hotkey","type":"bytes32"},{"name":"coldkey","type":"bytes32"},{"name":"netuid","type":"uint256"}]},
	{"type":"function","name":"getAlphaPrice","stateMutability":"view","outputs":[{"name":"","type":"uint256"}],
	 "inputs":[{"name":"netuid","type":"uint16"}]},
	{"type":"function","name":"getTaoInPool","stateMutability":"view","outputs":[{"name":"","type":"uint64"}],
	 "inputs":[{"name":"netuid","type":"uint16"}]},
	{"type":"function","name":"simSwapTaoForAlpha","stateMutability":"view","outputs":[{"name":"","type":"uint256"}],
	 "inputs":[{"name":"netuid","type":"uint16"},{"name":"tao","type":"uint64"}]}
]`

func TestPackedCallsMatchTheABI(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(funderABI))
	if err != nil {
		t.Fatal(err)
	}
	uint256 := func(n uint64) *big.Int { return new(big.Int).SetUint64(n) }
	largestWord := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	hotkey := [32]byte(bytes.Repeat([]byte{0xa1}, 32))
	coldkey := [32]byte(bytes.Repeat([]byte{0xb2}, 32))
	owner := common.HexToAddress("0x00112233445566778899aabbccddeeff00112233")
	for _, test := range []struct {
		name   string
		got    []byte
		method string
		args   []any
	}{
		{"withdraw", packWithdraw(taoWei(6.7)), "withdraw", []any{taoWei(6.7)}},
		{"withdraw of nothing", packWithdraw(new(big.Int)), "withdraw", []any{new(big.Int)}},
		{"withdraw of the largest word", packWithdraw(largestWord), "withdraw", []any{largestWord}},
		{"addStakeLimit", packAddStakeLimit(hotkey, 6_700_000_000, 4_195_646, 25), "addStakeLimit",
			[]any{hotkey, uint256(6_700_000_000), uint256(4_195_646), false, uint256(25)}},
		{"addStakeLimit at its limits", packAddStakeLimit(hotkey, math.MaxUint64, math.MaxUint64, math.MaxUint16), "addStakeLimit",
			[]any{hotkey, uint256(math.MaxUint64), uint256(math.MaxUint64), false, uint256(math.MaxUint16)}},
		{"getStake", packGetStake(hotkey, coldkey, 25), "getStake", []any{hotkey, coldkey, uint256(25)}},
		{"simSwapTaoForAlpha", packSimSwap(25, 6_700_000_000), "simSwapTaoForAlpha", []any{uint16(25), uint64(6_700_000_000)}},
		{"simSwapTaoForAlpha at its limits", packSimSwap(math.MaxUint16, math.MaxUint64), "simSwapTaoForAlpha", []any{uint16(math.MaxUint16), uint64(math.MaxUint64)}},
		{"balanceOf", packBalanceOf(owner), "balanceOf", []any{owner}},
		{"peers", packPeers(bridge.SolanaEID), "peers", []any{bridge.SolanaEID}},
		{"getAlphaPrice", packAlphaPrice(25), "getAlphaPrice", []any{uint16(25)}},
		{"getTaoInPool", packTaoInPool(25), "getTaoInPool", []any{uint16(25)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			want, err := parsed.Pack(test.method, test.args...)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(test.got, want) {
				t.Fatalf("packed %x\nthe ABI says %x", test.got, want)
			}
		})
	}
}

// The signer's gate reads the same bytes: it admits the ABI's encoding of a whole
// stake and refuses the ABI's encoding of a partial one.
func TestTheSignerGateAdmitsTheABIsEncoding(t *testing.T) {
	parsed, err := abi.JSON(strings.NewReader(funderABI))
	if err != nil {
		t.Fatal(err)
	}
	hotkey := [32]byte{0xc0}
	for _, test := range []struct {
		name    string
		partial bool
		want    bool
	}{
		{"whole", false, true},
		{"partial", true, false},
	} {
		data, err := parsed.Pack("addStakeLimit", hotkey, big.NewInt(6_700_000_000), big.NewInt(4_195_646), test.partial, big.NewInt(testNetuid))
		if err != nil {
			t.Fatal(err)
		}
		if got := AllowedAlphaWalletCall(StakingPrecompile, data, bridge.WTAO, hotkey, testNetuid); got != test.want {
			t.Fatalf("%s: admitted %v", test.name, got)
		}
	}
	data, err := parsed.Pack("withdraw", taoWei(1))
	if err != nil {
		t.Fatal(err)
	}
	if !AllowedAlphaWalletCall(bridge.WTAO, data, bridge.WTAO, hotkey, testNetuid) {
		t.Fatal("refused the ABI's withdraw")
	}
}

// The fake chain's rule is its own number, at the boundary mainnet showed, and comes
// ahead of the hotkey check as it does there.
func TestTheFakeChainRefusesTheAmountBeforeTheHotkey(t *testing.T) {
	r := newRig(t, enabled)
	r.hotkeyErr = errors.New("dispatch execution failed: HotKeyAccountNotExists")
	for amount, want := range map[uint64]error{2_001_006: errAmountTooLow, 2_001_007: r.hotkeyErr} {
		_, err := r.Call(context.Background(), StakingPrecompile, packAddStakeLimit(r.cfg.Hotkey, amount, 1, testNetuid))
		if !errors.Is(err, want) {
			t.Fatalf("a dry run of %d rao said %v", amount, err)
		}
	}
}

// Status's dry run of the smallest stake has to clear the chain's minimum. Otherwise
// the chain answers AmountTooLow before it looks at the hotkey: a good hotkey reads as
// broken, and a broken one is never named.
func TestTheStatusDryRunClearsTheChainsMinimum(t *testing.T) {
	r := newRig(t, enabled)
	f := r.funder(t)
	status, err := f.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.HotkeyError != "" {
		t.Fatalf("a good hotkey reads as: %s", status.HotkeyError)
	}
	r.hotkeyErr = errors.New("dispatch execution failed: HotKeyAccountNotExists")
	if status, err = f.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.HotkeyError, "HotKeyAccountNotExists") {
		t.Fatalf("a broken hotkey reads as: %s", status.HotkeyError)
	}
}

// Status reports a read it could not make rather than a partial picture, and changes
// nothing either way.
func TestStatusReportsAFailedRead(t *testing.T) {
	down := errors.New("the node is down")
	failing := func(callSelector []byte) func(*rig) {
		return func(r *rig) { r.failRead(callSelector, down) }
	}
	for name, fault := range map[string]func(*rig){
		"the alpha position": failing(getStakeSelector),
		"the wTAO balance":   failing(balanceOfSelector),
		"the alpha price":    failing(alphaPriceSelector),
		"the pool":           failing(taoInPoolSelector),
		"the native balance": func(r *rig) { r.failNative = down },
		"a token account":    func(r *rig) { r.failToken = func(solana.PublicKey) error { return down } },
		"the state":          func(r *rig) { r.stateJSON = []byte("{") },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, enabled)
			fault(r)
			status, err := r.funder(t).Status(context.Background())
			if err == nil || status != nil {
				t.Fatal(status, err)
			}
			if r.saves != 0 || len(r.operations) != 0 || r.orderCalls != 0 || len(r.broadcasts) != 0 {
				t.Fatalf("%d saves, operations %v, %d orders, %d broadcasts", r.saves, r.operations, r.orderCalls, len(r.broadcasts))
			}
		})
	}
}

func TestAnInvalidRequestIsRefused(t *testing.T) {
	r := newRig(t, enabled)
	f := r.funder(t)
	for name, need := range map[string]*big.Int{"no amount": nil, "a negative amount": big.NewInt(-1)} {
		if _, err := f.Ensure(context.Background(), Request{Epoch: 7, NeedRao: need}); err == nil || !strings.Contains(err.Error(), "invalid alpha requirement") {
			t.Fatal(name, err)
		}
	}
}

func TestANewFunderNeedsItsConnections(t *testing.T) {
	r := newRig(t, enabled)
	complete := func() Deps { return Deps{Store: r, EVM: r, Sender: r, Solana: rigSolana{worldSolana{r.world}, r}} }
	for name, build := range map[string]func() (*Funder, error){
		"a config": func() (*Funder, error) { return NewFunder(nil, testNetuid, complete()) },
		"a netuid": func() (*Funder, error) { return NewFunder(r.cfg, 0, complete()) },
		"a store": func() (*Funder, error) {
			deps := complete()
			deps.Store = nil
			return NewFunder(r.cfg, testNetuid, deps)
		},
		"an EVM": func() (*Funder, error) {
			deps := complete()
			deps.EVM = nil
			return NewFunder(r.cfg, testNetuid, deps)
		},
		"a sender": func() (*Funder, error) {
			deps := complete()
			deps.Sender = nil
			return NewFunder(r.cfg, testNetuid, deps)
		},
		"a Solana": func() (*Funder, error) {
			deps := complete()
			deps.Solana = nil
			return NewFunder(r.cfg, testNetuid, deps)
		},
	} {
		if _, err := build(); err == nil {
			t.Fatal("built without", name)
		}
	}
}

// A pool or balance that makes no sense stops the step before anything moves.
func TestPoolFaultsActOnNothing(t *testing.T) {
	replying := func(callSelector, raw []byte) func(*rig) {
		return func(r *rig) {
			r.answer = func(_ common.Address, data []byte) ([]byte, bool, error) {
				return raw, bytes.HasPrefix(data, callSelector), nil
			}
		}
	}
	for _, test := range []struct {
		name  string
		fault func(*rig)
		need  *big.Int
		want  string
	}{
		{"a zero price", func(r *rig) { r.price = new(big.Int) }, alpha(1_000), "alpha price is zero"},
		{"a pool that quotes no alpha", replying(simSwapSelector, wordUint(0)), alpha(1_000), "quotes no alpha"},
		{"a pool that never quotes enough", replying(simSwapSelector, word(new(big.Int).Sub(alpha(1_000), big.NewInt(1)))), alpha(1_000), "does not converge"},
		{"a reply of the wrong length", replying(alphaPriceSelector, []byte{1}), alpha(1_000), "returned 1 bytes"},
		{"a pool with no TAO", func(r *rig) { r.poolRao = 0 }, alpha(1_000), "the pool holds no TAO"},
		{"a shortfall past 64 bits", func(*rig) {}, new(big.Int).Lsh(big.NewInt(1), 64), "shortfall is out of range"},
		{"a TAO balance past 64 bits of rao", func(r *rig) { r.nativeWei = new(big.Int).Lsh(big.NewInt(1), 100) }, alpha(1_000), "out of range"},
		{"a price past 64 bits of TAO", func(r *rig) { r.price = new(big.Int).Lsh(big.NewInt(1), 100) }, alpha(1_000), "TAO estimate is out of range"},
		{"a pool that quotes almost nothing", replying(simSwapSelector, wordUint(1)), alpha(1_000), "TAO estimate is out of range"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.nativeWei = taoWei(10)
			test.fault(r)
			_, err := r.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: test.need})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatal(err)
			}
			if len(r.operations) != 0 || r.orderCalls != 0 || r.saves != 0 {
				t.Fatalf("operations %v, %d orders, %d saves", r.operations, r.orderCalls, r.saves)
			}
		})
	}
}

// A journal the funder cannot read stops it there: it does not act on a guess, or
// clear what it does not understand.
func TestACorruptJournalStopsWithoutActing(t *testing.T) {
	for _, test := range []struct {
		name  string
		state func(r *rig) *State
		want  string
	}{
		{"an unsubmitted swap", func(r *rig) *State {
			return &State{Epoch: 7, Swap: &SwapRecord{RequestID: "request", Raw: "%%%", AmountMicro: 1, LastValidBlockHeight: r.height + 10}}
		}, "illegal base64"},
		{"a send to rebroadcast", func(r *rig) *State {
			return &State{Epoch: 7, Bridge: &BridgeRecord{Signature: "sig", Raw: "%%%", LastValidBlockHeight: r.height + 10, WTAOBeforeWei: "0"}}
		}, "illegal base64"},
		{"a landed send's balance", func(r *rig) *State {
			return &State{Epoch: 7, Bridge: &BridgeRecord{Signature: "sig", LandedAt: r.clock, WTAOBeforeWei: "soup"}}
		}, "does not parse"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			r.seed(test.state(r))
			saved := bytes.Clone(r.stateJSON)
			_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatal(err)
			}
			if r.saves != 0 || !bytes.Equal(r.stateJSON, saved) || r.executes != 0 || len(r.broadcasts) != 0 || r.orderCalls != 0 {
				t.Fatalf("%d saves, %d executes, %d broadcasts, %d orders", r.saves, r.executes, len(r.broadcasts), r.orderCalls)
			}
		})
	}
}

func TestAnUnreadableStateActsOnNothing(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.stateJSON = []byte("{")
	_, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_000))
	if err == nil || !strings.Contains(err.Error(), "decode the state") {
		t.Fatal(err)
	}
	if r.orderCalls != 0 || r.saves != 0 {
		t.Fatalf("%d orders, %d saves", r.orderCalls, r.saves)
	}
}

// Accounting that cannot be written is logged, and the conversion goes on, however
// the store reports the failure.
func TestARecordErrorDoesNotStopTheConversion(t *testing.T) {
	for name, fault := range map[string]func(*rig){
		"an error": func(r *rig) { r.failRecord = errors.New("the ledger is full") },
		"a panic":  func(r *rig) { r.panicRecord = true },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, enabled)
			r.usdc = 5_000 * microPerUSDC
			fault(r)
			r.ensureFunded(r.funder(t), deposit(7, 1_656))
			if !r.logged("could not record swapped") || len(r.swaps) != 1 {
				t.Fatalf("%d swaps, logs %v", len(r.swaps), r.logs)
			}
		})
	}
}

// A report of failure is not proof a swap cannot land, so it waits for expiry
// rather than buying again at once.
func TestAFailedReportWaitsForItsSignature(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.executeCode = -1000
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	if !r.logged("jupiter execute reported") {
		t.Fatal("the report was not logged")
	}
	journal := r.current().Swap
	if journal == nil || r.count("swap_failed") != 0 {
		t.Fatalf("journal %+v, steps %s", journal, r.kinds())
	}
	if wait := r.step(f, request); wait != "swap landing" || r.orderCalls != 3 {
		t.Fatalf("it waits on %q after %d orders", wait, r.orderCalls)
	}
	r.executeCode = 0
	r.height = journal.LastValidBlockHeight + expiryGraceBlocks + 1
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 || r.count("swap_failed") != 1 {
		t.Fatalf("%d swaps, steps %s", len(r.swaps), r.kinds())
	}
}

// A fill under the floor gets past the gate only if the chain differs from the
// simulation, and it looks like a read behind the swap. So it gets a read's whole wait
// first. Then it is reported, booked as it was, and the rest is bought.
func TestAFillBelowTheFloorIsReportedAndToppedUp(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	short := false
	var delivered uint64
	r.afterExecute = func() {
		if !short {
			short = true
			r.tao = r.tao / 10 * 9
			delivered = r.tao
		}
	}
	f := r.funder(t)
	request := deposit(7, 1_656)
	result, err := f.Ensure(context.Background(), request)
	if err != nil || result.Funded || len(r.swaps) != 1 || r.count("swapped") != 0 {
		t.Fatalf("within the wait: %+v, %v, %d swaps, steps %s", result, err, len(r.swaps), r.kinds())
	}
	r.moveTo(r.current().Swap.SignedAt.Add(swapUnseenWait))
	r.ensureFunded(f, request)
	if !r.logged("below the floor") || len(r.swaps) != 2 {
		t.Fatalf("%d swaps, logs %v", len(r.swaps), r.logs)
	}
	first := r.events[0]
	if first.Kind != "swapped" || first.Data["tao_rao"] != delivered {
		t.Fatalf("booked %+v, delivered %d", first, delivered)
	}
	if state := r.current(); state.SpentUSDCMicro != usdcOf(r.swaps) {
		t.Fatalf("state %+v", state)
	}
}

// A funded wallet does not look at its journal, and the next conversion finds it
// as it was left.
func TestAFundedWalletLeavesItsJournalAlone(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.executeDrop = true
	f := r.funder(t)
	r.step(f, deposit(7, 1_656))
	saved := bytes.Clone(r.stateJSON)
	r.alphaRao = alpha(2_000)
	r.ensureFunded(f, deposit(7, 1_656))
	if !bytes.Equal(r.stateJSON, saved) {
		t.Fatalf("state %s", r.stateJSON)
	}
	r.executeDrop = false
	r.height += 1_000
	r.ensureFunded(f, deposit(7, 3_000))
	if r.count("swap_failed") != 1 || len(r.swaps) != 1 {
		t.Fatalf("steps %s, %d swaps", r.kinds(), len(r.swaps))
	}
}

// Turning conversion off stops everything but forgets nothing.
func TestDisablingLeavesAJournalForLater(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.executeLost = true
	f := r.funder(t)
	request := deposit(7, 1_656)
	r.step(f, request)
	saved := bytes.Clone(r.stateJSON)
	r.cfg.Conversion.Enabled = false
	if _, err := f.Ensure(context.Background(), request); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	if !bytes.Equal(r.stateJSON, saved) {
		t.Fatalf("state %s", r.stateJSON)
	}
	r.cfg.Conversion.Enabled = true
	r.ensureFunded(f, request)
	if len(r.swaps) != 1 {
		t.Fatalf("%d swaps", len(r.swaps))
	}
}

// A wallet may have no TAO account before its first swap, and no USDC account at all.
func TestTokenAccountsThatDoNotExistYet(t *testing.T) {
	t.Run("no TAO account before the first swap", func(t *testing.T) {
		r := newRig(t, enabled)
		r.usdc = 5_000 * microPerUSDC
		r.noToken = func(address solana.PublicKey) bool { return address == r.taoAccount && r.tao == 0 }
		r.ensureFunded(r.funder(t), deposit(7, 1_656))
		if len(r.swaps) != 1 {
			t.Fatalf("%d swaps", len(r.swaps))
		}
	})
	t.Run("no USDC account", func(t *testing.T) {
		r := newRig(t, enabled)
		r.noToken = func(address solana.PublicKey) bool { return address == r.usdcAccount }
		if _, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656)); !errors.Is(err, ErrInsufficientUSDC) {
			t.Fatal(err)
		}
		r.requireNoSwapJournal()
	})
}

func TestSwapFloorAndSlippageNumbers(t *testing.T) {
	// 1,600 USDC at 240 USD per TAO is 6.666… TAO, of which 99% is 6.6.
	if floor := swapFloor(1_600*microPerUSDC, 240, 0.01); floor < 6_599_999_999 || floor > 6_600_000_000 {
		t.Fatal("floor", floor)
	}
	for _, test := range []struct {
		name   string
		quote  uint64
		floor  uint64
		maxBps int
		want   int
	}{
		{"a quote at the floor", 1_000, 1_000, 100, 1},
		{"a quote below the floor", 900, 1_000, 100, 1},
		{"a quote just above the floor", 1_000_000, 999_999, 100, 1},
		{"a gap within the cap", 1_000_000, 996_300, 100, 37},
		{"a gap beyond the cap", 1_000_000, 500_000, 100, 100},
	} {
		if got := swapSlippage(test.quote, test.floor, test.maxBps); got != test.want {
			t.Fatalf("%s: %d bps, want %d", test.name, got, test.want)
		}
	}
}

// The default SOL minimum covers the swap's worst fee and then the bridge's cap.
func TestTheDefaultSOLMinimumCoversTheRoute(t *testing.T) {
	r := newRig(t, enabled)
	r.usdc = 5_000 * microPerUSDC
	r.lamports = r.cfg.Conversion.MinSOLLamports
	charged := false
	r.afterExecute = func() {
		if !charged {
			charged = true
			r.lamports -= maxSwapLamports - 1_000_000
		}
	}
	r.ensureFunded(r.funder(t), deposit(7, 1_656))
	if len(r.swaps) != 1 || len(r.sends) != 1 {
		t.Fatalf("%d swaps and %d sends", len(r.swaps), len(r.sends))
	}
}

// A configured minimum may be smaller, and the swap should not spend USDC on a route
// the wallet then cannot finish, which strands the TAO on Solana for want of SOL.
func TestAWalletAtTheSOLMinimumCanFinishTheRoute(t *testing.T) {
	conversion := enabled + "\n  min_sol: 0.05"
	if _, err := ParseConfig(testConfigYAML(t, conversion)); err != nil {
		return // refusing the configuration is a fine answer
	}
	r := newRig(t, conversion)
	r.usdc = 5_000 * microPerUSDC
	r.lamports = r.cfg.Conversion.MinSOLLamports
	result, err := r.funder(t).Ensure(context.Background(), deposit(7, 1_656))
	if err != nil && len(r.swaps) != 0 {
		t.Fatalf("spent %s USDC on a swap and then could not bridge it: %v (TAO on Solana %d)", microString(usdcOf(r.swaps)), err, r.tao)
	}
	if err == nil && !result.Funded {
		t.Fatalf("%+v", result)
	}
}
