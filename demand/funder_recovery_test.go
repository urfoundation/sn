package demand

// How a conversion holds up against what really happens to it: the orders Jupiter
// really sends, replies that go missing, reads behind the chain, and being killed
// anywhere. It builds only on the world in funder_test.go.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/demand/bridge"
	"github.com/urfoundation/sn/demand/jupiter"
	"github.com/urfoundation/sn/demand/solana"
)

// recovShape is what /order answers a taker with. The first two are the shapes of
// every order sampled live; the fake's bodies go through the real client's decoder,
// so that they cannot hold a field Jupiter does not send.
type recovShape int

const (
	// An AMM route: the wallet pays, there is a last valid height, and the minimum
	// follows from the slippage asked for.
	recovAMM recovShape = iota
	// JupiterZ's RFQ: gasless, with a time about 53 seconds ahead and no height. Its
	// minimum is its output and it reports no slippage. It has two signature slots, the
	// maker's first, which pays the fee, and is empty until /execute.
	recovRFQ
	// The RFQ shape without its time, which no sampled order was: the fallback's case.
	recovUntimed
)

const recovRFQLifetime = 53 * time.Second

var (
	recovProgram = solana.PublicKey{0xa2}
	recovMaker   = solana.PublicKey{0xa1}
)

// recovReply is what /execute does with a signed swap.
type recovReply int

const (
	recovLandsAndReports recovReply = iota // lands at once and reports success
	recovReplyLost                         // the reply is lost, and it lands at the next poll
	recovFailureReported                   // reports a failure, and lands at the next poll all the same
	recovDropped                           // the reply is lost, and it never lands
	recovExpiredReported                   // reports that it expired, and never lands
)

// recovery is the world with faults, every one of them off until a test sets it.
type recovery struct {
	*world
	t    *testing.T
	logs []string

	// Jupiter
	shape     recovShape
	reply     func(execute int) recovReply
	expireIn  time.Duration
	quoteOut  func(fair uint64) uint64
	orderOut  func(takerCall int, fair uint64) uint64
	editOrder func(takerCall int, order *jupiter.Order)
	onTaker   func(takerCall int)
	makerKey  ed25519.PrivateKey // a maker that signs before /execute
	badSlot   bool               // a maker slot filled with something that is no signature
	quotes    int
	takers    int
	asked     []int // the slippage of each taker's order
	executes  int
	inFlight  []recovFlight

	// Solana
	staleTao        uint64
	staleTaoReads   int
	blindStatuses   int
	editSim         func(sim *solana.Simulation)
	simulated       [][]byte
	broadcasted     [][]byte
	missingTable    bool
	staleAfterLands int // reads behind each swap's landing
	blockhashes     uint64

	// Bittensor
	trace     []string
	alphaRead int
	onAlpha   func(read int)
	chainMin  uint64 // above 0, the chain refuses a stake below this first
	onSend    func() // once, at the first send after alpha has been read
	onPrepare func() // once, between a sender's two runs of a prepare
	retained  []recovIntent
	durable   bool
	stakes    int
	unwraps   int

	// LayerZero Scan
	scan string

	// Store
	onRecord func(event Event)
	loseSave int
	saveCall int

	gate *recovGate
}

type recovFlight struct {
	order     *fakeOrder
	signature string
}

type recovIntent struct {
	operation string
	to        common.Address
	data      []byte
}

func newRecovery(t *testing.T, conversion string) *recovery {
	t.Helper()
	r := &recovery{world: newWorld(t, conversion), t: t, expireIn: recovRFQLifetime}
	r.usdc = 60_000 * microPerUSDC
	return r
}

func (r *recovery) deps() Deps {
	return Deps{
		Store: recovStore{r}, EVM: recovEVM{r}, Sender: recovSender{r}, Solana: recovSolana{worldSolana{r.world}, r}, Jupiter: r, Market: r.world, Scan: r,
		Logf:  func(format string, args ...any) { r.logs = append(r.logs, fmt.Sprintf(format, args...)) },
		Now:   func() time.Time { return r.clock },
		Sleep: r.sleep,
	}
}

func (r *recovery) funder() *Funder {
	r.t.Helper()
	f, err := NewFunder(r.cfg, testNetuid, r.deps())
	if err != nil {
		r.t.Fatal(err)
	}
	return f
}

// sleep is a poll passing: what is in flight lands, and the world moves on.
func (r *recovery) sleep(ctx context.Context, d time.Duration) error {
	r.land()
	return r.world.sleep(ctx, d)
}

func (r *recovery) land() {
	for _, flight := range r.inFlight {
		r.landOrder(flight.order, flight.signature)
	}
	r.inFlight = nil
}

func (r *recovery) landOrder(order *fakeOrder, signature string) {
	if r.staleAfterLands > 0 {
		r.staleTao, r.staleTaoReads = r.tao, r.staleAfterLands
	}
	r.swaps = append(r.swaps, order)
	r.usdc -= order.amount
	r.tao += order.out
	r.statuses[signature] = &solana.SignatureStatus{ConfirmationStatus: "confirmed"}
}

// passTo brings the clock to at, and the chain along at the world's 40 blocks in 15 s.
func (r *recovery) passTo(at time.Time) {
	if passed := at.Sub(r.clock); passed > 0 {
		r.clock = at
		r.height += uint64(passed * 40 / (15 * time.Second))
	}
}

func (r *recovery) state() *State {
	r.t.Helper()
	state, _, err := r.Load(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	return state
}

func (r *recovery) seedState(state *State) {
	r.t.Helper()
	raw, err := MarshalState(state)
	if err != nil {
		r.t.Fatal(err)
	}
	r.stateJSON, r.version = raw, r.version+1
}

func (r *recovery) count(kind string) int {
	n := 0
	for _, event := range r.events {
		if event.Kind == kind {
			n++
		}
	}
	return n
}

func (r *recovery) logged(text string) bool {
	return strings.Contains(strings.Join(r.logs, "\n"), text)
}

func (r *recovery) stepOnce(f *Funder, request Request) string {
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

func (r *recovery) spentOnSwaps() uint64 {
	total := uint64(0)
	for _, swap := range r.swaps {
		total += swap.amount
	}
	return total
}

func recovRequest(epoch uint64, alphaAmount float64) Request {
	return Request{Epoch: epoch, NeedRao: alpha(alphaAmount)}
}

func recovWei(rao uint64) *big.Int {
	return new(big.Int).Mul(new(big.Int).SetUint64(rao), WeiPerRao)
}

// Jupiter

type recovTransport []byte

func (body recovTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func (r *recovery) decoded(ctx context.Context, req jupiter.OrderRequest, body map[string]any) (*jupiter.Order, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	// A new client each time: the real one paces its orders by the wall clock.
	client := jupiter.NewClient("", jupiter.WithBaseURL("http://jupiter.invalid"), jupiter.WithHTTPClient(&http.Client{Transport: recovTransport(raw)}))
	return client.Order(ctx, req)
}

func (r *recovery) Order(ctx context.Context, req jupiter.OrderRequest) (*jupiter.Order, error) {
	if req.InputMint != USDCMint.String() || req.OutputMint != bridge.TaoMint.String() {
		r.t.Error("an order for the wrong pair")
	}
	fair := uint64(float64(req.Amount) / microPerUSDC / r.jupiterPrice * raoPerTAO)
	body := map[string]any{"requestId": "request", "inAmount": strconv.FormatUint(req.Amount, 10), "feeBps": 10}
	if req.Taker == "" {
		r.quotes++
		out := fair
		if r.quoteOut != nil {
			out = r.quoteOut(fair)
		}
		body["router"], body["outAmount"], body["otherAmountThreshold"] = "metis", strconv.FormatUint(out, 10), strconv.FormatUint(out-out*uint64(req.SlippageBps)/10_000, 10)
		return r.decoded(ctx, req, body)
	}
	r.takers++
	call := r.takers
	r.asked = append(r.asked, req.SlippageBps)
	if r.onTaker != nil {
		r.onTaker(call)
	}
	out := fair
	if r.orderOut != nil {
		out = r.orderOut(call, fair)
	}
	if r.shape == recovAMM {
		body["router"], body["swapType"], body["gasless"], body["slippageBps"] = "metis", "aggregator", false, req.SlippageBps
		body["otherAmountThreshold"] = strconv.FormatUint(out-out*uint64(req.SlippageBps)/10_000, 10)
		body["lastValidBlockHeight"] = strconv.FormatUint(r.height+150, 10)
		body["signatureFeePayer"] = req.Taker
	} else {
		body["router"], body["swapType"], body["gasless"], body["slippageBps"] = "jupiterz", "rfq", true, 0
		body["otherAmountThreshold"] = strconv.FormatUint(out, 10)
		body["signatureFeePayer"], body["rentFeePayer"] = recovMaker.String(), req.Taker
		if r.shape == recovRFQ {
			body["expireAt"] = strconv.FormatInt(r.clock.Add(r.expireIn).Unix(), 10)
		}
	}
	body["outAmount"] = strconv.FormatUint(out, 10)
	tx, err := r.takerTransaction()
	if err != nil {
		return nil, err
	}
	body["transaction"] = base64.StdEncoding.EncodeToString(tx.Serialize())
	order, err := r.decoded(ctx, req, body)
	if err != nil {
		return nil, err
	}
	if r.editOrder != nil {
		r.editOrder(call, order)
	}
	r.orders[string(tx.Message)] = &fakeOrder{amount: req.Amount, out: out, raw: order.Transaction}
	return order, nil
}

func (r *recovery) takerTransaction() (*solana.Transaction, error) {
	data := binary.LittleEndian.AppendUint64(nil, uint64(r.takers))
	if r.shape == recovAMM {
		return solana.CompileV0(r.cfg.USDCAddress, []solana.Instruction{{Program: recovProgram, Data: data}}, [32]byte{2}, nil)
	}
	maker := recovMaker
	if r.makerKey != nil {
		maker = solana.PublicKeyOf(r.makerKey)
	}
	tx, err := solana.CompileV0(maker, []solana.Instruction{{Program: recovProgram, Accounts: []solana.AccountMeta{{Key: r.cfg.USDCAddress, Signer: true, Writable: true}}, Data: data}}, [32]byte{2}, nil)
	if err != nil {
		return nil, err
	}
	if r.makerKey != nil {
		return tx, tx.Sign(r.makerKey)
	}
	if r.badSlot {
		tx.Signatures[0] = [64]byte{1, 2, 3}
	}
	return tx, nil
}

func (r *recovery) Execute(_ context.Context, signed []byte, _ string) (*jupiter.ExecuteResult, error) {
	tx, err := solana.ParseTransaction(signed)
	if err != nil {
		return nil, err
	}
	if !tx.SignedBy(r.cfg.USDCAddress) {
		r.t.Error("the swap is not signed by the wallet")
	}
	order := r.orders[string(tx.Message)]
	if order == nil {
		r.t.Error("executed an unknown transaction")
		return nil, errors.New("unknown transaction")
	}
	r.executes++
	// The maker signs here, so with its slot empty the ID exists only from now on.
	if tx.Signatures[0] == ([64]byte{}) || r.badSlot {
		tx.Signatures[0] = sha512.Sum512(tx.Message)
	}
	signature := tx.ID()
	reply := recovLandsAndReports
	if r.reply != nil {
		reply = r.reply(r.executes)
	}
	switch reply {
	case recovReplyLost:
		r.inFlight = append(r.inFlight, recovFlight{order, signature})
		return nil, errors.New("jupiter: /execute: context deadline exceeded")
	case recovFailureReported:
		r.inFlight = append(r.inFlight, recovFlight{order, signature})
		return &jupiter.ExecuteResult{Status: "Failed", Code: -1006, Error: "timed out"}, nil
	case recovDropped:
		return nil, errors.New("jupiter: /execute: connection reset")
	case recovExpiredReported:
		return &jupiter.ExecuteResult{Status: "Failed", Code: -1005, Error: "expired"}, nil
	}
	r.landOrder(order, signature)
	return &jupiter.ExecuteResult{Status: "Success", Signature: signature}, nil
}

func (r *recovery) MessageStatus(context.Context, string) (string, string, error) {
	if r.scan != "" {
		return r.scan, "", nil
	}
	return "INFLIGHT", "", nil
}

// Solana

type recovSolana struct {
	worldSolana
	r *recovery
}

func (s recovSolana) TokenAccount(ctx context.Context, address solana.PublicKey) (*solana.TokenAccount, error) {
	if address == s.r.taoAccount && s.r.staleTaoReads > 0 {
		s.r.staleTaoReads--
		return s.r.token(address, bridge.TaoMint, s.r.staleTao), nil
	}
	return s.r.world.TokenAccount(ctx, address)
}

func (s recovSolana) Account(ctx context.Context, address solana.PublicKey) (*solana.Account, error) {
	if address == bridge.LookupTable && s.r.missingTable {
		return nil, nil
	}
	return s.r.world.Account(ctx, address)
}

func (s recovSolana) Simulate(ctx context.Context, raw []byte, accounts []solana.PublicKey) (*solana.Simulation, error) {
	s.r.simulated = append(s.r.simulated, bytes.Clone(raw))
	sim, err := s.r.world.Simulate(ctx, raw, accounts)
	if err == nil && s.r.editSim != nil {
		s.r.editSim(sim)
	}
	return sim, err
}

func (s recovSolana) SignatureStatuses(ctx context.Context, signatures []string) ([]*solana.SignatureStatus, error) {
	if s.r.blindStatuses > 0 {
		s.r.blindStatuses--
		return make([]*solana.SignatureStatus, len(signatures)), nil
	}
	return s.r.world.SignatureStatuses(ctx, signatures)
}

// LatestBlockhash moves on with each call, as the chain's does between sends, so that
// a new bridge send of the same amount has its own bytes and signature.
func (s recovSolana) LatestBlockhash(context.Context) (solana.Blockhash, error) {
	s.r.blockhashes++
	var hash [32]byte
	binary.LittleEndian.PutUint64(hash[:], s.r.blockhashes)
	return solana.Blockhash{Hash: hash, LastValidBlockHeight: s.r.height + 150}, nil
}

func (s recovSolana) SendTransaction(ctx context.Context, raw []byte) (string, error) {
	s.r.broadcasted = append(s.r.broadcasted, bytes.Clone(raw))
	return s.r.world.SendTransaction(ctx, raw)
}

// Bittensor

type recovEVM struct{ r *recovery }

func (e recovEVM) Call(ctx context.Context, to common.Address, data []byte) ([]byte, error) {
	r := e.r
	if to == StakingPrecompile && bytes.HasPrefix(data, addStakeLimitSelector) && r.chainMin > 0 && new(big.Int).SetBytes(data[36:68]).Cmp(new(big.Int).SetUint64(r.chainMin)) < 0 {
		return nil, errors.New("dispatch execution failed: AmountTooLow")
	}
	out, err := r.world.Call(ctx, to, data)
	if to == StakingPrecompile && bytes.HasPrefix(data, getStakeSelector) {
		r.trace = append(r.trace, "alpha")
		r.alphaRead++
		if r.onAlpha != nil {
			r.onAlpha(r.alphaRead)
		}
	}
	return out, err
}

func (e recovEVM) Balance(ctx context.Context, account common.Address) (*big.Int, error) {
	return e.r.world.Balance(ctx, account)
}

func (e recovEVM) PendingNonce(ctx context.Context, account common.Address) (uint64, error) {
	return uint64(len(e.r.operations) + len(e.r.retained)), nil
}

// recovSender is the durable sender as far as this package can see it. With durable
// set, an operation is retained before it goes out, so a kill from then on leaves it
// to finish, and every later call finishes it before preparing anything.
type recovSender struct{ r *recovery }

func (s recovSender) SendPrepared(ctx context.Context, prepare Prepare) (string, error) {
	r := s.r
	r.trace = append(r.trace, "send")
	if err := r.finishRetained(ctx); err != nil {
		return "", err
	}
	if r.onSend != nil && r.alphaRead > 0 {
		r.onSend()
		r.onSend = nil
	}
	operation, to, data, err := prepare(ctx)
	if r.onPrepare != nil && err == nil && operation != "" {
		r.onPrepare()
		r.onPrepare = nil
		operation, to, data, err = prepare(ctx)
	}
	if err != nil || operation == "" {
		return "", err
	}
	r.trace = append(r.trace, operation)
	if !AllowedAlphaWalletCall(to, data, bridge.WTAO, r.cfg.Hotkey, testNetuid) {
		r.t.Errorf("the conversion sent a call a signer must refuse: %s", operation)
	}
	if r.durable {
		r.retained = append(r.retained, recovIntent{operation, to, data})
		r.passGate("the signed send")
		if err := r.finishRetained(ctx); err != nil {
			return "", err
		}
		return "0xhash" + operation, nil
	}
	r.applied(to)
	return r.world.SendPrepared(ctx, func(context.Context) (string, common.Address, []byte, error) { return operation, to, data, nil })
}

func (r *recovery) finishRetained(ctx context.Context) error {
	retained := r.retained
	r.retained = nil
	for _, intent := range retained {
		r.applied(intent.to)
		if _, err := r.world.SendPrepared(ctx, func(context.Context) (string, common.Address, []byte, error) {
			return intent.operation, intent.to, intent.data, nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *recovery) applied(to common.Address) {
	if to == StakingPrecompile {
		r.stakes++
	} else {
		r.unwraps++
	}
}

// Store

type recovStore struct{ r *recovery }

func (s recovStore) Load(ctx context.Context) (*State, uint64, error) { return s.r.world.Load(ctx) }

func (s recovStore) Save(ctx context.Context, state *State, version uint64) (uint64, error) {
	s.r.saveCall++
	if s.r.saveCall == s.r.loseSave {
		return 0, fmt.Errorf("%w: version %d", ErrStale, version)
	}
	return s.r.world.Save(ctx, state, version)
}

func (s recovStore) Record(ctx context.Context, event Event) error {
	if s.r.onRecord != nil {
		s.r.onRecord(event)
	}
	return s.r.world.Record(ctx, event)
}

// ---- 1, 18: a swap with no height waits on time, and only then do the balances decide

// The fake holds to the sampled orders, or nothing else here means much.
func TestRecoveryOrdersHaveTheShapesJupiterSends(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	req := jupiter.OrderRequest{InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: 1_000 * microPerUSDC, Taker: r.cfg.USDCAddress.String(), SlippageBps: 50}
	order, err := r.Order(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if order.LastValidBlockHeight != 0 || !order.Gasless || order.SlippageBps != 0 || order.OtherAmountThreshold != order.OutAmount {
		t.Fatalf("%+v", order)
	}
	if left := order.ExpireAt.Sub(r.clock); left < 52*time.Second || left > 54*time.Second {
		t.Fatal("expires in", left)
	}
	tx, err := solana.ParseTransaction(order.Transaction)
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Signatures) != 2 || tx.Signatures[0] != ([64]byte{}) || tx.FeePayer() != recovMaker || tx.RequiredSigners()[1] != r.cfg.USDCAddress {
		t.Fatalf("%d slots, payer %s, signers %v", len(tx.Signatures), tx.FeePayer(), tx.RequiredSigners())
	}
	r.shape = recovAMM
	if order, err = r.Order(context.Background(), req); err != nil || order.LastValidBlockHeight == 0 || !order.ExpireAt.IsZero() || order.Gasless || order.OtherAmountThreshold >= order.OutAmount {
		t.Fatal(order, err)
	}
}

// An RFQ swap whose reply is lost is in flight until its time is well past. Calling it
// expired sooner, as a height of 0 once did, buys it all a second time.
func TestRecoveryAnRFQSwapWaitsOutItsTime(t *testing.T) {
	for name, reply := range map[string]recovReply{"its reply is lost": recovDropped, "a failure is reported": recovFailureReported} {
		t.Run(name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			r.shape = recovRFQ
			r.reply = func(execute int) recovReply {
				if execute > 1 {
					return recovDropped
				}
				return reply
			}
			f := r.funder()
			request := recovRequest(7, 1_656)
			if wait := r.stepOnce(f, request); wait != "" {
				t.Fatal(wait)
			}
			first := r.state().Swap
			if first == nil || first.LastValidBlockHeight != 0 || first.Signature != "" || !first.ExpireAt.Equal(r.clock.Add(recovRFQLifetime)) {
				t.Fatalf("journal %+v", first)
			}
			r.inFlight = nil // this one never lands
			for _, at := range []time.Time{r.clock, first.ExpireAt, first.ExpireAt.Add(swapExpiryGrace)} {
				r.passTo(at)
				if wait := r.stepOnce(f, request); wait != "swap landing" {
					t.Fatalf("at %s it waits on %q", at, wait)
				}
				if kept := r.state().Swap; kept == nil || kept.Raw != first.Raw || r.executes != 1 || r.takers != 1 || r.count("swap_failed") != 0 {
					t.Fatalf("at %s: journal %+v, %d executes, %d orders, steps %s", at, kept, r.executes, r.takers, r.kinds())
				}
			}
			// Past it, nothing landed: the budget comes back and it buys again, once.
			r.passTo(first.ExpireAt.Add(swapExpiryGrace + time.Second))
			r.stepOnce(f, request)
			state := r.state()
			if r.count("swap_failed") != 1 || r.executes != 2 || state.Swap == nil || state.Swap.Raw == first.Raw || state.SpentUSDCMicro != state.Swap.AmountMicro {
				t.Fatalf("steps %s, %d executes, state %+v", r.kinds(), r.executes, state)
			}
		})
	}
}

// The same swap landing while it waits: booked once, by its TAO, with no second buy.
func TestRecoveryAnRFQSwapThatLandsUnseenIsBookedOnce(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.reply = func(int) recovReply { return recovReplyLost }
	result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_656))
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if r.executes != 1 || len(r.swaps) != 1 || len(r.sends) != 1 || r.count("swapped") != 1 || r.count("swap_failed") != 0 {
		t.Fatalf("%d executes, %d swaps, %d sends, steps %s", r.executes, len(r.swaps), len(r.sends), r.kinds())
	}
	if booked := r.events[0].Data["tao_rao"]; booked != r.swaps[0].out {
		t.Fatalf("booked %v of %d", booked, r.swaps[0].out)
	}
	if state := r.state(); state.SpentUSDCMicro != r.swaps[0].amount {
		t.Fatalf("state %+v", state)
	}
}

// With neither a height nor a time the swap is accepted, and counts from its signing.
func TestRecoveryAnOrderWithNeitherHeightNorTimeWaitsFromItsSigning(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovUntimed
	r.reply = func(int) recovReply { return recovDropped }
	f := r.funder()
	request := recovRequest(7, 1_656)
	r.stepOnce(f, request)
	first := r.state().Swap
	if first == nil || first.LastValidBlockHeight != 0 || !first.ExpireAt.IsZero() {
		t.Fatalf("journal %+v", first)
	}
	for _, at := range []time.Time{r.clock, first.SignedAt.Add(swapNoExpiryWait - time.Second), first.SignedAt.Add(swapNoExpiryWait)} {
		r.passTo(at)
		if wait := r.stepOnce(f, request); wait != "swap landing" || r.state().Swap == nil || r.executes != 1 {
			t.Fatalf("at %s it waits on %q, %d executes, steps %s", at, wait, r.executes, r.kinds())
		}
	}
	r.passTo(first.SignedAt.Add(swapNoExpiryWait + time.Second))
	r.stepOnce(f, request)
	if r.count("swap_failed") != 1 || r.executes != 2 {
		t.Fatalf("steps %s, %d executes", r.kinds(), r.executes)
	}
}

// A time nobody could mean, such as milliseconds read as seconds, must not hold the
// route for a lifetime. No blockhash lasts that long, so it counts from the signing.
func TestRecoveryAnUnbelievableTimeCountsFromTheSigning(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.expireIn = 24 * time.Hour
	r.reply = func(int) recovReply { return recovDropped }
	f := r.funder()
	request := recovRequest(7, 1_656)
	r.stepOnce(f, request)
	first := r.state().Swap
	if first == nil || !first.ExpireAt.IsZero() {
		t.Fatalf("journal %+v", first)
	}
	r.passTo(first.SignedAt.Add(swapNoExpiryWait))
	if wait := r.stepOnce(f, request); wait != "swap landing" || r.count("swap_failed") != 0 {
		t.Fatalf("it waits on %q, steps %s", wait, r.kinds())
	}
	r.passTo(first.SignedAt.Add(swapNoExpiryWait + time.Second))
	r.stepOnce(f, request)
	if r.count("swap_failed") != 1 {
		t.Fatal("steps", r.kinds())
	}
}

// Killed before SubmittedAt is saved, the swap never went out. No time rule may read
// its zero as long expired and buy again: the journaled bytes go out, once.
func TestRecoveryAKillBeforeSubmittingSendsTheJournal(t *testing.T) {
	for name, shape := range map[string]recovShape{"rfq": recovRFQ, "no height or time": recovUntimed, "amm": recovAMM} {
		t.Run(name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			r.shape = shape
			r.reply = func(int) recovReply { return recovDropped }
			r.loseSave = 2
			request := recovRequest(7, 1_656)
			if _, _, err := r.funder().step(context.Background(), request); !errors.Is(err, ErrStale) {
				t.Fatal(err)
			}
			first := r.state().Swap
			if first == nil || !first.SubmittedAt.IsZero() || r.executes != 0 {
				t.Fatalf("journal %+v, %d executes", first, r.executes)
			}
			wait := r.stepOnce(r.funder(), request)
			kept := r.state().Swap
			if wait == "" || kept == nil || kept.Raw != first.Raw || kept.SubmittedAt.IsZero() || r.executes != 1 || r.takers != 1 || r.count("swap_failed") != 0 {
				t.Fatalf("it waits on %q: journal %+v, %d executes, %d orders, steps %s", wait, kept, r.executes, r.takers, r.kinds())
			}
		})
	}
}

// And resumed too late to matter: it never went out, so it is cleared and ordered anew.
func TestRecoveryAKilledSwapResumedAfterItsTimeIsOrderedAnew(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.loseSave = 2
	request := recovRequest(7, 1_656)
	if _, _, err := r.funder().step(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	first := r.state().Swap
	r.passTo(first.ExpireAt.Add(swapExpiryGrace + time.Second))
	result, err := r.funder().Ensure(context.Background(), request)
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if r.executes != 1 || len(r.swaps) != 1 || r.count("swap_failed") != 1 || r.takers != 2 {
		t.Fatalf("%d executes, %d swaps, %d orders, steps %s", r.executes, len(r.swaps), r.takers, r.kinds())
	}
	if state := r.state(); state.SpentUSDCMicro != r.swaps[0].amount {
		t.Fatalf("state %+v", state)
	}
}

// A maker's slot already holding a good signature names the swap, so it can be followed
// by its status. One holding anything else names nothing, and the reply's ID stands.
func TestRecoveryAFilledMakerSlotIsTrustedOnlyIfItIsASignature(t *testing.T) {
	t.Run("a good signature", func(t *testing.T) {
		r := newRecovery(t, enabled)
		r.shape = recovRFQ
		_, secret, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		r.makerKey = secret
		r.reply = func(int) recovReply { return recovReplyLost }
		f := r.funder()
		request := recovRequest(7, 1_656)
		r.stepOnce(f, request)
		journal := r.state().Swap
		raw, _ := base64.StdEncoding.DecodeString(journal.Raw)
		tx, err := solana.ParseTransaction(raw)
		if err != nil {
			t.Fatal(err)
		}
		if journal.Signature == "" || journal.Signature != tx.ID() || !tx.SignedBy(solana.PublicKeyOf(secret)) || !tx.SignedBy(r.cfg.USDCAddress) {
			t.Fatalf("journal %+v", journal)
		}
		// Its status settles it well before its time is up.
		r.land()
		r.stepOnce(f, request)
		if r.state().Swap != nil || r.count("swapped") != 1 || r.events[0].Data["signature"] != journal.Signature {
			t.Fatalf("state %+v, events %+v", r.state(), r.events)
		}
	})
	t.Run("anything else", func(t *testing.T) {
		r := newRecovery(t, enabled)
		r.shape = recovRFQ
		r.badSlot = true
		r.reply = func(int) recovReply { return recovDropped }
		r.stepOnce(r.funder(), recovRequest(7, 1_656))
		if journal := r.state().Swap; journal == nil || journal.Signature != "" {
			t.Fatalf("journal %+v", journal)
		}
	})
	t.Run("an empty slot is left for the maker", func(t *testing.T) {
		r := newRecovery(t, enabled)
		r.shape = recovRFQ
		r.reply = func(int) recovReply { return recovDropped }
		r.stepOnce(r.funder(), recovRequest(7, 1_656))
		journal := r.state().Swap
		raw, _ := base64.StdEncoding.DecodeString(journal.Raw)
		tx, err := solana.ParseTransaction(raw)
		if err != nil {
			t.Fatal(err)
		}
		if journal.Signature != "" || tx.Signatures[0] != ([64]byte{}) || !tx.SignedBy(r.cfg.USDCAddress) {
			t.Fatalf("journal %+v", journal)
		}
	})
}

// ---- B: past its time a swap with no signature is decided by its TAO

// The operator topping up USDC mid-flight hides the swap's USDC leaving. Its TAO
// still shows it landed: booked, and the budget stays spent.
func TestRecoveryADepositMidFlightStillBooksTheSwap(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.reply = func(int) recovReply { return recovReplyLost }
	f := r.funder()
	request := recovRequest(7, 1_656)
	r.stepOnce(f, request)
	journal := r.state().Swap
	r.land()
	r.usdc += 2 * journal.AmountMicro
	r.passTo(journal.ExpireAt.Add(swapExpiryGrace + time.Second))
	r.stepOnce(f, request)
	if r.count("swapped") != 1 || r.count("swap_failed") != 0 || r.state().SpentUSDCMicro != journal.AmountMicro || r.executes != 1 {
		t.Fatalf("steps %s, %d executes, state %+v", r.kinds(), r.executes, r.state())
	}
}

// The operator taking USDC out looks just like a swap's USDC leaving with a read of its
// TAO lagging, and buying on that read would buy twice. So the journal stays, its charge
// stays, nothing more is bought and the log says so, for as long as a read could lag.
// After that it is closed for a person to look at: not booked, and with no refund.
func TestRecoveryUSDCGoneWithNoTAONeedsAPerson(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.reply = func(int) recovReply { return recovDropped }
	f := r.funder()
	request := recovRequest(7, 1_656)
	r.stepOnce(f, request)
	journal := r.state().Swap
	r.usdc -= journal.AmountMicro
	for _, at := range []time.Time{
		journal.ExpireAt.Add(swapExpiryGrace + time.Second),
		journal.SignedAt.Add(5 * time.Minute),
	} {
		r.passTo(at)
		r.logs = nil
		wait := r.stepOnce(f, request)
		state := r.state()
		if wait == "" || state.Swap == nil || state.Swap.Raw != journal.Raw || state.SpentUSDCMicro != journal.AmountMicro || r.takers != 1 || r.executes != 1 || len(r.events) != 0 {
			t.Fatalf("at %s it waits on %q: state %+v, %d orders, %d executes, steps %s", at, wait, state, r.takers, r.executes, r.kinds())
		}
		if !r.logged("ALERT") {
			t.Fatalf("at %s nothing was said: %v", at, r.logs)
		}
	}
	// A whole run inside the wait reports it, and buys nothing.
	result, err := f.Ensure(context.Background(), request)
	if err != nil || result.Funded || result.Waiting != "the swap's TAO to show" || r.takers != 1 {
		t.Fatal(result, err, r.takers)
	}
	r.passTo(journal.SignedAt.Add(swapUnseenWait - time.Second))
	if wait := r.stepOnce(f, request); wait == "" || r.state().Swap == nil || len(r.events) != 0 {
		t.Fatalf("a second before the end it waits on %q, steps %s", wait, r.kinds())
	}
	r.passTo(journal.SignedAt.Add(swapUnseenWait))
	r.stepOnce(f, request)
	state := r.state()
	if r.count("swap_needs_person") != 1 || r.count("swapped") != 0 || r.count("swap_failed") != 0 || !r.logged("needs a person") {
		t.Fatalf("steps %s, logs %v", r.kinds(), r.logs)
	}
	// The charge stayed: the next swap is charged on top of it.
	if state.Swap == nil || state.Swap.Raw == journal.Raw || state.SpentUSDCMicro != journal.AmountMicro+state.Swap.AmountMicro {
		t.Fatalf("state %+v", state)
	}
}

// A lost write as it closes loses nothing and books nothing twice.
func TestRecoveryALostWriteClosingForAPersonRepeatsOnce(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.reply = func(int) recovReply { return recovDropped }
	f := r.funder()
	request := recovRequest(7, 1_656)
	r.stepOnce(f, request)
	journal := r.state().Swap
	r.usdc -= journal.AmountMicro
	r.passTo(journal.SignedAt.Add(swapUnseenWait))
	r.loseSave = r.saveCall + 1
	if _, _, err := f.step(context.Background(), request); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if kept := r.state().Swap; kept == nil || kept.Raw != journal.Raw || len(r.events) != 0 {
		t.Fatalf("journal %+v, steps %s", kept, r.kinds())
	}
	r.stepOnce(f, request)
	if r.count("swap_needs_person") != 1 {
		t.Fatal("steps", r.kinds())
	}
}

// If the TAO does show while it waits, that was a read behind a swap, and it is booked.
func TestRecoveryTAOShowingDuringTheWaitBooksTheSwap(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.reply = func(int) recovReply { return recovReplyLost }
	f := r.funder()
	request := recovRequest(7, 1_656)
	r.stepOnce(f, request)
	journal := r.state().Swap
	r.staleAfterLands = 1 << 30
	r.land()
	r.passTo(journal.ExpireAt.Add(swapExpiryGrace + time.Second))
	if wait := r.stepOnce(f, request); wait != "the swap's TAO to show" {
		t.Fatal(wait)
	}
	r.passTo(journal.SignedAt.Add(swapUnseenWait - time.Minute))
	r.staleTaoReads = 0
	r.stepOnce(f, request)
	if r.count("swapped") != 1 || r.count("swap_needs_person") != 0 || r.count("swap_failed") != 0 || r.executes != 1 || r.state().SpentUSDCMicro != journal.AmountMicro {
		t.Fatalf("steps %s, %d executes, state %+v", r.kinds(), r.executes, r.state())
	}
}

// USDC that never moved is the plain case: past its time, cleared and refunded.
func TestRecoveryUSDCUnmovedStillClearsAndRefunds(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.reply = func(execute int) recovReply { return recovDropped }
	f := r.funder()
	request := recovRequest(7, 1_656)
	r.stepOnce(f, request)
	journal := r.state().Swap
	r.passTo(journal.ExpireAt.Add(swapExpiryGrace + time.Second))
	r.stepOnce(f, request)
	state := r.state()
	if r.count("swap_failed") != 1 || r.count("swap_needs_person") != 0 || state.Swap == nil || state.SpentUSDCMicro != state.Swap.AmountMicro {
		t.Fatalf("steps %s, state %+v", r.kinds(), state)
	}
}

// A kill can leave a swap in flight with nothing to name it, and its TAO read can still
// be behind when its time is up. Its USDC leaving is why that read is given time.
func TestRecoveryATaoReadBehindAtExpiryDoesNotBuyAgain(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.reply = func(int) recovReply { return recovReplyLost }
	f := r.funder()
	request := recovRequest(7, 1_656)
	r.stepOnce(f, request)
	journal := r.state().Swap
	r.staleAfterLands = 2
	r.land()
	r.passTo(journal.ExpireAt.Add(swapExpiryGrace + time.Second))
	result, err := f.Ensure(context.Background(), request)
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if r.executes != 1 || len(r.swaps) != 1 || r.count("swap_failed") != 0 || r.count("swapped") != 1 {
		t.Fatalf("%d executes, %d swaps, steps %s", r.executes, len(r.swaps), r.kinds())
	}
}

// USDC gone and some TAO, short of the floor, is a poor fill and not a withdrawal. It
// looks like a read behind the swap too, so it gets a read's whole wait, and is then
// booked as it was.
func TestRecoveryAShortFillWithNoSignatureIsBookedAsItWas(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.reply = func(int) recovReply { return recovReplyLost }
	f := r.funder()
	request := recovRequest(7, 1_656)
	r.stepOnce(f, request)
	journal := r.state().Swap
	r.land()
	r.tao /= 2
	r.passTo(journal.ExpireAt.Add(swapExpiryGrace + time.Second))
	for _, at := range []time.Time{r.clock, journal.SignedAt.Add(swapUnseenWait - time.Second)} {
		r.passTo(at)
		if wait := r.stepOnce(f, request); wait != "the swap's TAO to show" || r.state().Swap == nil || len(r.events) != 0 || r.takers != 1 {
			t.Fatalf("at %s it waits on %q, steps %s, %d orders", at, wait, r.kinds(), r.takers)
		}
	}
	r.passTo(journal.SignedAt.Add(swapUnseenWait))
	r.stepOnce(f, request)
	if r.count("swapped") != 1 || r.count("swap_failed") != 0 || r.count("swap_needs_person") != 0 || !r.logged("below the floor") || r.events[0].Data["tao_rao"] != r.swaps[0].out/2 || r.state().SpentUSDCMicro < journal.AmountMicro {
		t.Fatalf("steps %s, events %+v, state %+v", r.kinds(), r.events, r.state())
	}
}

// ---- 2: a landed swap keeps its journal until its TAO shows

// A read behind a swap Jupiter reported, by any number of reads within the wait, buys
// nothing more, and the swap is booked at what it paid.
func TestRecoveryReadsBehindALandedSwapBuyNothing(t *testing.T) {
	for _, stale := range []int{1, 2, 3, 8} {
		for name, shape := range map[string]recovShape{"amm": recovAMM, "rfq": recovRFQ} {
			t.Run(fmt.Sprintf("%s %d reads", name, stale), func(t *testing.T) {
				r := newRecovery(t, enabled)
				r.shape = shape
				r.staleAfterLands = stale
				result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_656))
				if err != nil || !result.Funded {
					t.Fatal(result, err)
				}
				if r.executes != 1 || len(r.swaps) != 1 || r.count("swapped") != 1 {
					t.Fatalf("%d executes, %d swaps, steps %s", r.executes, len(r.swaps), r.kinds())
				}
				if booked := r.events[0].Data["tao_rao"]; booked != r.swaps[0].out {
					t.Fatalf("booked %v of %d", booked, r.swaps[0].out)
				}
				if state := r.state(); state.SpentUSDCMicro != r.swaps[0].amount {
					t.Fatalf("state %+v", state)
				}
			})
		}
	}
}

// A journal closes on the first read that shows its TAO. A read after that from a node
// further behind must not buy it all again: the TAO the gate reads seconds later, which
// the step did not know of, stops the swap before it is signed.
func TestRecoveryAReadBehindAClosedJournalBuysNothing(t *testing.T) {
	r := newRecovery(t, enabled)
	r.tao = 7 * raoPerTAO
	r.staleTao, r.staleTaoReads = 0, 1
	f := r.funder()
	request := recovRequest(7, 1_656)
	if wait := r.stepOnce(f, request); wait != "" {
		t.Fatal(wait)
	}
	if r.executes != 0 || r.saves != 0 || !r.logged("deciding again") {
		t.Fatalf("%d executes, %d saves, logs %v", r.executes, r.saves, r.logs)
	}
	// Reads that have caught up bridge what is there, and buy nothing.
	result, err := f.Ensure(context.Background(), request)
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(r.swaps) != 0 || len(r.sends) != 1 {
		t.Fatalf("%d swaps, %d sends", len(r.swaps), len(r.sends))
	}
}

// A swap can be known to have landed in two ways: Jupiter reports it, or its signature's
// status shows it.
func recovKnownLanded() map[string]func(*recovery) {
	return map[string]func(*recovery){
		"Jupiter reports it":  func(*recovery) {},
		"its status shows it": func(r *recovery) { r.reply = func(int) recovReply { return recovReplyLost } },
	}
}

// A swap known to have landed whose TAO read lags is waited for as long as a read could
// plausibly lag, because closing the journal on such a read buys it all again. Through
// all of that the charge stays and nothing more is bought, and past the first minute the
// log says so. At the end it goes to a person: no TAO to book, so no booking, and no
// refund. Only then can it buy again.
func TestRecoveryTheWaitForATaoReadEnds(t *testing.T) {
	for name, landing := range recovKnownLanded() {
		t.Run(name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			r.staleAfterLands = 1 << 30
			landing(r)
			f := r.funder()
			request := recovRequest(7, 1_656)
			r.stepOnce(f, request)
			r.land()
			journal := r.state().Swap
			if journal == nil || journal.Signature == "" {
				t.Fatalf("journal %+v", journal)
			}
			// A read behind by seconds is nothing to shout about.
			r.logs = nil
			if wait := r.stepOnce(f, request); wait != "the swap's TAO to show" || r.logged("ALERT") {
				t.Fatalf("it waits on %q, logs %v", wait, r.logs)
			}
			for _, at := range []time.Time{
				journal.SignedAt.Add(time.Minute),
				journal.SignedAt.Add(5*time.Minute + time.Second), // where it once gave up
				journal.SignedAt.Add(swapUnseenWait - time.Second),
			} {
				r.passTo(at)
				r.logs = nil
				wait := r.stepOnce(f, request)
				state := r.state()
				if wait != "the swap's TAO to show" || state.Swap == nil || state.Swap.Raw != journal.Raw || state.SpentUSDCMicro != journal.AmountMicro || r.takers != 1 || r.executes != 1 || len(r.events) != 0 {
					t.Fatalf("at %s it waits on %q: state %+v, %d orders, %d executes, steps %s", at, wait, state, r.takers, r.executes, r.kinds())
				}
				if !r.logged("ALERT") {
					t.Fatalf("at %s nothing was said: %v", at, r.logs)
				}
			}
			r.passTo(journal.SignedAt.Add(swapUnseenWait))
			r.reply = func(int) recovReply { return recovDropped }
			r.stepOnce(f, request)
			state := r.state()
			if r.count("swap_needs_person") != 1 || r.count("swapped") != 0 || r.count("swap_failed") != 0 || !r.logged("needs a person") {
				t.Fatalf("steps %s, logs %v", r.kinds(), r.logs)
			}
			if state.Swap == nil || state.Swap.Raw == journal.Raw || state.SpentUSDCMicro != journal.AmountMicro+state.Swap.AmountMicro {
				t.Fatalf("the charge did not stay: %+v", state)
			}
		})
	}
}

// TAO showing at any point in that wait books the swap as usual, at what it paid, and
// nothing is bought.
func TestRecoveryAReadCatchingUpLateBooksTheSwap(t *testing.T) {
	for name, landing := range recovKnownLanded() {
		t.Run(name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			r.staleAfterLands = 1 << 30
			landing(r)
			f := r.funder()
			request := recovRequest(7, 1_656)
			r.stepOnce(f, request)
			r.land()
			journal := r.state().Swap
			r.passTo(journal.SignedAt.Add(swapUnseenWait - time.Minute))
			if wait := r.stepOnce(f, request); wait != "the swap's TAO to show" {
				t.Fatal(wait)
			}
			r.staleTaoReads = 0
			r.stepOnce(f, request)
			if r.count("swapped") != 1 || r.count("swap_needs_person") != 0 || r.executes != 1 || r.takers != 1 || r.state().SpentUSDCMicro != journal.AmountMicro {
				t.Fatalf("steps %s, %d executes, %d orders, state %+v", r.kinds(), r.executes, r.takers, r.state())
			}
			if booked := r.events[0].Data["tao_rao"]; booked != r.swaps[0].out {
				t.Fatalf("booked %v of %d", booked, r.swaps[0].out)
			}
		})
	}
}

// ---- 3: nothing overwrites a journal

// A resumed swap goes out and is in flight, whatever /execute says, so the step stops.
func TestRecoveryAResumedSwapInFlightKeepsItsJournal(t *testing.T) {
	for name, reply := range map[string]recovReply{"reply lost": recovDropped, "failure reported": recovFailureReported} {
		t.Run(name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			r.reply = func(int) recovReply { return reply }
			r.loseSave = 2
			request := recovRequest(7, 1_656)
			if _, _, err := r.funder().step(context.Background(), request); !errors.Is(err, ErrStale) {
				t.Fatal(err)
			}
			first := r.state().Swap
			wait := r.stepOnce(r.funder(), request)
			state := r.state()
			if wait == "" || r.executes != 1 || state.Swap == nil || state.Swap.Raw != first.Raw || state.SpentUSDCMicro != first.AmountMicro || r.takers != 1 {
				t.Fatalf("it waits on %q after %d executes and %d orders, state %+v", wait, r.executes, r.takers, state)
			}
		})
	}
}

// startSwap itself refuses to replace a journal, whoever calls it.
func TestRecoveryStartingASwapOverAJournalIsRefused(t *testing.T) {
	r := newRecovery(t, enabled)
	journal := &SwapRecord{RequestID: "request", Raw: "kept"}
	run := &conversion{Funder: r.funder(), state: &State{Epoch: 7, Swap: journal}, epoch: 7}
	if err := run.startSwap(context.Background(), raoPerTAO); err == nil || run.state.Swap != journal || r.takers != 0 {
		t.Fatalf("%v, journal %+v, %d orders", err, run.state.Swap, r.takers)
	}
}

// ---- 4: nothing buys or stakes from under a transaction still finishing

// A stake the sender settles just before prepare, with the step's reads already taken,
// has already funded the wallet. Staking on took a need of 1,000 to 2,020.
func TestRecoveryAStakeSettledByTheSenderIsNotRepeated(t *testing.T) {
	r := newRecovery(t, enabled)
	r.nativeWei = taoWei(20)
	const retained = 4_040_000_000
	r.onSend = func() {
		r.nativeWei = new(big.Int).Sub(r.nativeWei, recovWei(retained))
		r.alphaRao = new(big.Int).Add(r.alphaRao, r.alphaFor(new(big.Int).SetUint64(retained)))
	}
	result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_000))
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(r.operations) != 0 || r.alphaRao.Cmp(alpha(1_020)) > 0 {
		t.Fatalf("operations %v, alpha %s", r.operations, r.alphaRao)
	}
}

// One that covers part of it leaves the stake only what is left to buy.
func TestRecoveryAStakeSettledInPartResizesTheNext(t *testing.T) {
	r := newRecovery(t, enabled)
	r.nativeWei = taoWei(20)
	const retained = 2_000_000_000 // half the need
	r.onSend = func() {
		r.nativeWei = new(big.Int).Sub(r.nativeWei, recovWei(retained))
		r.alphaRao = new(big.Int).Add(r.alphaRao, r.alphaFor(new(big.Int).SetUint64(retained)))
	}
	result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_000))
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(r.operations) != 1 || r.alphaRao.Cmp(alpha(1_000)) < 0 || r.alphaRao.Cmp(alpha(1_010)) > 0 {
		t.Fatalf("operations %v, alpha %s", r.operations, r.alphaRao)
	}
}

// The same for an unwrap: a wallet the settled stake funded spends no gas on it.
func TestRecoveryAStakeSettledBeforeAnUnwrapSkipsIt(t *testing.T) {
	r := newRecovery(t, enabled)
	r.nativeWei, r.wtaoWei = taoWei(5), taoWei(1)
	const retained = 4_040_000_000
	r.onSend = func() {
		r.nativeWei = new(big.Int).Sub(r.nativeWei, recovWei(retained))
		r.alphaRao = new(big.Int).Add(r.alphaRao, r.alphaFor(new(big.Int).SetUint64(retained)))
	}
	result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_000))
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(r.operations) != 0 || r.wtaoWei.Cmp(taoWei(1)) != 0 {
		t.Fatalf("operations %v, wTAO %s", r.operations, r.wtaoWei)
	}
}

// A settled send that leaves less above the reserve than the chain takes sends nothing.
func TestRecoveryASettledSendShrinksNoStakeBelowTheMinimum(t *testing.T) {
	r := newRecovery(t, enabled)
	r.nativeWei = taoWei(5)
	r.onSend = func() { r.nativeWei = recovWei(r.cfg.Conversion.GasReserveRao + 1_000) }
	if _, _, err := r.funder().step(context.Background(), recovRequest(7, 1_000)); err != nil {
		t.Fatal(err)
	}
	if len(r.operations) != 0 {
		t.Fatal(r.operations)
	}
}

// Before a step reads any balance, the sender has finished what the wallet sent.
func TestRecoveryEachStepSettlesBeforeReading(t *testing.T) {
	r := newRecovery(t, enabled)
	f := r.funder()
	r.tao = 7 * raoPerTAO
	for range 3 {
		r.trace = nil
		if _, _, err := f.step(context.Background(), recovRequest(7, 1_000)); err != nil {
			t.Fatal(err)
		}
		if len(r.trace) < 2 || r.trace[0] != "send" || r.trace[1] != "alpha" {
			t.Fatalf("the step began %v", r.trace)
		}
	}
	// A disabled funder is asked to do nothing, and the sender is not called.
	off := newRecovery(t, "  enabled: false")
	if _, _, err := off.funder().step(context.Background(), recovRequest(7, 1_000)); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(off.trace, ","), "send") {
		t.Fatal(off.trace)
	}
}

// A killed run's stake the sender still holds is finished first, and funds the wallet.
func TestRecoveryARetainedStakeFundsTheWalletBeforeAnyRead(t *testing.T) {
	r := newRecovery(t, enabled)
	r.durable = true
	r.nativeWei = taoWei(6.8)
	const retained = 6_690_000_000
	r.retained = []recovIntent{{"demand:stake:0:retained", StakingPrecompile, packAddStakeLimit(r.cfg.Hotkey, retained, 1<<40, testNetuid)}}
	result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_656))
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(r.swaps) != 0 || r.takers != 0 || r.stakes != 1 {
		t.Fatalf("%d swaps, %d orders, %d stakes", len(r.swaps), r.takers, r.stakes)
	}
}

// A stake mining between the step's reads of alpha and TAO reads as a wallet short of
// both. Alpha read again before spending shows it, so nothing is bought or bridged.
func TestRecoveryAStakeMiningBetweenReadsSpendsNothing(t *testing.T) {
	for name, setup := range map[string]func(*recovery){
		"before a swap":        func(*recovery) {},
		"before a bridge send": func(r *recovery) { r.tao = 7 * raoPerTAO },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			setup(r)
			r.nativeWei = taoWei(6.8)
			const mining = 6_690_000_000
			r.onAlpha = func(read int) {
				if read == 1 {
					r.nativeWei = new(big.Int).Sub(r.nativeWei, recovWei(mining))
					r.alphaRao = new(big.Int).Add(r.alphaRao, r.alphaFor(new(big.Int).SetUint64(mining)))
				}
			}
			result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_656))
			if err != nil || !result.Funded {
				t.Fatal(result, err)
			}
			if r.takers != 0 || len(r.swaps) != 0 || len(r.broadcasted) != 0 || len(r.operations) != 0 {
				t.Fatalf("%d orders, %d swaps, %d broadcasts, operations %v", r.takers, len(r.swaps), len(r.broadcasted), r.operations)
			}
		})
	}
}

// ---- 5: a landed bridge send is not forgotten on one missing status

// Past its time with no status, its TAO having left says it landed: it is followed, and
// nothing is sent or bought again.
func TestRecoveryASendWithNoStatusIsDecidedByItsTAO(t *testing.T) {
	r := newRecovery(t, enabled)
	r.tao = 7 * raoPerTAO
	r.deliverAfter = 1_000_000
	f := r.funder()
	request := recovRequest(7, 1_656)
	if wait := r.stepOnce(f, request); wait != "bridge send" {
		t.Fatal(wait)
	}
	journal := r.state().Bridge
	if journal == nil || journal.TaoBefore != 7*raoPerTAO || len(r.sends) != 1 {
		t.Fatalf("journal %+v, %d sends", journal, len(r.sends))
	}
	r.height = journal.LastValidBlockHeight + expiryGraceBlocks + 1
	r.blindStatuses = 1
	if wait := r.stepOnce(f, request); !strings.Contains(wait, "LayerZero delivery") {
		t.Fatal(wait)
	}
	kept := r.state().Bridge
	if kept == nil || kept.LandedAt.IsZero() || r.count("bridged") != 1 || r.count("bridge_expired") != 0 || r.takers != 0 || len(r.broadcasted) != 1 {
		t.Fatalf("journal %+v, steps %s, %d orders, %d broadcasts", kept, r.kinds(), r.takers, len(r.broadcasted))
	}
	r.pending[0].after = 1
	result, err := f.Ensure(context.Background(), request)
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(r.sends) != 1 || r.takers != 0 {
		t.Fatalf("%d sends, %d orders", len(r.sends), r.takers)
	}
}

// With its TAO still there it can no longer land and did not: cleared, and sent anew.
// A journal from before TaoBefore existed reads the same way.
func TestRecoveryASendWithNoStatusAndItsTAOStillThereIsCleared(t *testing.T) {
	for name, keepTaoBefore := range map[string]bool{"with TaoBefore": true, "an older journal": false} {
		t.Run(name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			r.tao = 7 * raoPerTAO
			r.sendDrop = 1
			f := r.funder()
			request := recovRequest(7, 1_000)
			r.stepOnce(f, request)
			state := r.state()
			if !keepTaoBefore {
				state.Bridge.TaoBefore = 0
				r.seedState(state)
			}
			r.height = state.Bridge.LastValidBlockHeight + expiryGraceBlocks + 1
			if wait := r.stepOnce(f, request); wait != "bridge send" {
				t.Fatal(wait)
			}
			if r.count("bridge_expired") != 1 || r.state().Bridge == nil || len(r.sends) != 1 {
				t.Fatalf("steps %s, %d sends, state %+v", r.kinds(), len(r.sends), r.state())
			}
		})
	}
}

// ---- 6: the later legs are checked before any USDC moves

func TestRecoveryAWalletThatCannotFinishTheRouteSpendsNoUSDC(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*recovery)
		want  error
		text  string
	}{
		{"no TAO for the unwrap's gas", func(r *recovery) { r.nativeWei = new(big.Int) }, ErrInsufficientGas, "rao"},
		{"no gas to unwrap what is on Solana either", func(r *recovery) {
			r.nativeWei, r.tao = new(big.Int), 1
		}, ErrInsufficientGas, "rao"},
		{"a little too little TAO", func(r *recovery) { r.nativeWei = recovWei(minRouteNativeRao - 1) }, ErrInsufficientGas, "rao"},
		{"a changed Solana peer", func(r *recovery) { r.peerData[8+12] ^= 1 }, ErrBridgeChanged, "wTAO"},
		{"a changed wTAO peer", func(r *recovery) { r.peerStore[0] ^= 1 }, ErrBridgeChanged, "wTAO's Solana peer"},
		{"no lookup table", func(r *recovery) { r.missingTable = true }, ErrBridgeChanged, "lookup table"},
		{"SOL below min_sol", func(r *recovery) { r.lamports = r.cfg.Conversion.MinSOLLamports - 1 }, ErrInsufficientSOL, "lamports"},
		{"SOL below the route's fees whatever min_sol says", func(r *recovery) {
			r.cfg.Conversion.MinSOLLamports = 1
			r.lamports = r.cfg.Conversion.MaxBridgeFeeLamports + bridgeTxLamports + maxSwapLamports - 1
		}, ErrInsufficientSOL, "lamports"},
		{"a hotkey that does not exist", func(r *recovery) {
			r.hotkeyErr = errors.New("dispatch execution failed: HotKeyAccountNotExists")
		}, ErrHotkeyNotReady, "HotKeyAccountNotExists"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			test.setup(r)
			_, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_656))
			if !errors.Is(err, test.want) || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("refused with %v", err)
			}
			if r.takers != 0 || r.quotes != 0 || r.saves != 0 || r.executes != 0 || len(r.swaps) != 0 || r.usdc != 60_000*microPerUSDC {
				t.Fatalf("%d orders, %d quotes, %d saves, %d executes, USDC %d", r.takers, r.quotes, r.saves, r.executes, r.usdc)
			}
		})
	}
}

// TAO already on Solana needs the same gas before it sets out: a send that lands as
// wTAO the wallet cannot pay to unwrap has only moved the problem. It waits on Solana.
func TestRecoveryABridgeSendNeedsGasToo(t *testing.T) {
	for name, native := range map[string]uint64{"none": 0, "a little too little": minRouteNativeRao - 1} {
		t.Run(name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			r.tao = 7 * raoPerTAO
			r.nativeWei = recovWei(native)
			_, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_000))
			if !errors.Is(err, ErrInsufficientGas) {
				t.Fatal(err)
			}
			if r.state().Bridge != nil || r.saves != 0 || len(r.broadcasted) != 0 || len(r.sends) != 0 || r.tao != 7*raoPerTAO {
				t.Fatalf("state %+v, %d saves, %d broadcasts, TAO %d", r.state(), r.saves, len(r.broadcasted), r.tao)
			}
		})
	}
	r := newRecovery(t, enabled)
	r.tao = 7 * raoPerTAO
	r.nativeWei = recovWei(minRouteNativeRao)
	if wait := r.stepOnce(r.funder(), recovRequest(7, 1_000)); wait != "bridge send" || len(r.sends) != 1 {
		t.Fatalf("it waits on %q, %d sends", wait, len(r.sends))
	}
}

// Each limit admits exactly what it says.
func TestRecoveryTheRouteChecksAdmitTheirLimits(t *testing.T) {
	r := newRecovery(t, enabled)
	r.nativeWei = recovWei(minRouteNativeRao)
	r.lamports = r.cfg.Conversion.MinSOLLamports
	r.stepOnce(r.funder(), recovRequest(7, 1_656))
	if len(r.swaps) != 1 {
		t.Fatalf("%d swaps, steps %s", len(r.swaps), r.kinds())
	}
}

// A dry run that dies with the caller is not a verdict on the hotkey.
func TestRecoveryACancelledDryRunIsNotABadHotkey(t *testing.T) {
	r := newRecovery(t, enabled)
	ctx, cancel := context.WithCancel(context.Background())
	r.hotkeyErr = context.Canceled
	cancel()
	f := r.funder()
	run := &conversion{Funder: f, state: &State{}, epoch: 7}
	if err := run.checkHotkey(ctx); !errors.Is(err, context.Canceled) || errors.Is(err, ErrHotkeyNotReady) {
		t.Fatal(err)
	}
}

// ---- 7: only a newer epoch starts the budget again

func TestRecoveryAnOlderEpochSpendsNothing(t *testing.T) {
	r := newRecovery(t, enabled+"\n  max_usdc_per_epoch: 1700")
	f := r.funder()
	need := alpha(1_656)
	if result, err := f.Ensure(context.Background(), Request{Epoch: 8, NeedRao: need}); err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	spent, saved, version := r.spentOnSwaps(), bytes.Clone(r.stateJSON), r.version
	// A funded wallet is funded in any epoch.
	if result, err := f.Ensure(context.Background(), Request{Epoch: 7, NeedRao: need}); err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	// Short of alpha, an older epoch is told no, spends nothing and changes nothing.
	r.alphaRao = new(big.Int)
	r.nativeWei = taoWei(100)
	operations := len(r.operations)
	for _, epoch := range []uint64{7, 1, 0} {
		if _, err := f.Ensure(context.Background(), Request{Epoch: epoch, NeedRao: need}); !errors.Is(err, ErrEpochBudget) {
			t.Fatal(epoch, err)
		}
	}
	if r.spentOnSwaps() != spent || !bytes.Equal(r.stateJSON, saved) || r.version != version || len(r.operations) != operations {
		t.Fatalf("spent %d of %d, state %s, operations %v", r.spentOnSwaps(), spent, r.stateJSON, r.operations)
	}
	// The newer epoch goes on as it was, and the next starts afresh.
	r.nativeWei = taoWei(0.2)
	if _, err := f.Ensure(context.Background(), Request{Epoch: 8, NeedRao: need}); !errors.Is(err, ErrEpochBudget) {
		t.Fatal(err)
	}
	if result, err := f.Ensure(context.Background(), Request{Epoch: 9, NeedRao: need}); err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if state := r.state(); state.Epoch != 9 || state.SpentUSDCMicro != r.swaps[len(r.swaps)-1].amount || len(r.swaps) != 2 {
		t.Fatalf("%d swaps, state %+v", len(r.swaps), state)
	}
}

// ---- 8: the order for this wallet is the price, and the floor never moves

// What live AMM orders do: the quote comes from a better route, the order pays above the
// floor, and slippage sized from the quote takes its minimum below the floor.
func TestRecoveryAnOrderBelowItsQuoteIsPlacedAgain(t *testing.T) {
	r := newRecovery(t, enabled+"\n  max_price_gap: 0.005")
	r.quoteOut = func(fair uint64) uint64 { return fair + fair/2_000 } // 5 bps above
	r.stepOnce(r.funder(), recovRequest(7, 1_656))
	if r.takers != 2 || len(r.swaps) != 1 {
		t.Fatalf("%d orders, %d swaps", r.takers, len(r.swaps))
	}
	floor, out := r.events[0].Data["min_out_rao"].(uint64), r.swaps[0].out
	if want := int((out-floor)*10_000/out) - reorderMarginBps; r.asked[1] != want || want <= 0 {
		t.Fatalf("asked %d bps, want %d", r.asked[1], want)
	}
	if worst := out - out*uint64(r.asked[1])/10_000; worst < floor {
		t.Fatalf("the order's minimum of %d is below the floor of %d", worst, floor)
	}
}

// A second order's price can move: within its margin it passes, and past it, nothing is
// signed. There is no third order.
func TestRecoveryTheSecondOrderIsCheckedAndIsTheLast(t *testing.T) {
	for _, test := range []struct {
		name  string
		moved uint64 // bps worse than the first order
		swaps int
	}{
		{"unmoved", 0, 1},
		{"moved within the margin", 5, 1},
		{"moved past the margin", 15, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRecovery(t, enabled+"\n  max_price_gap: 0.005")
			r.quoteOut = func(fair uint64) uint64 { return fair + fair/500 }
			r.orderOut = func(call int, fair uint64) uint64 {
				if call == 1 {
					return fair
				}
				return fair - fair*test.moved/10_000
			}
			_, _, err := r.funder().step(context.Background(), recovRequest(7, 1_656))
			if test.swaps == 0 && !errors.Is(err, ErrPriceGuard) || test.swaps != 0 && err != nil {
				t.Fatal(err)
			}
			if r.takers != 2 || len(r.swaps) != test.swaps || r.executes != test.swaps {
				t.Fatalf("%d orders, %d swaps, %d executes", r.takers, len(r.swaps), r.executes)
			}
			if test.swaps == 0 {
				if state := r.state(); state.Swap != nil || state.SpentUSDCMicro != 0 || r.saves != 0 {
					t.Fatalf("state %+v, %d saves", state, r.saves)
				}
			}
		})
	}
}

func TestRecoveryAnOrderThePriceGuardRefuses(t *testing.T) {
	better := func(fair uint64) uint64 { return fair + fair/500 }
	for _, test := range []struct {
		name   string
		setup  func(*recovery)
		orders int
	}{
		{"its output below the floor", func(r *recovery) { r.jupiterPrice = 245 }, 1},
		{"no room above the floor for the margin", func(r *recovery) {
			r.quoteOut = better
			// The floor is 50 bps below what is fair; this pays 9 bps above the floor.
			r.orderOut = func(_ int, fair uint64) uint64 { return fair - fair*41/10_000 }
		}, 1},
		{"a second order paying below the floor", func(r *recovery) {
			r.quoteOut = better
			r.orderOut = func(call int, fair uint64) uint64 {
				if call == 1 {
					return fair
				}
				return fair * 98 / 100
			}
		}, 2},
		{"a second order with its minimum below the floor", func(r *recovery) {
			r.quoteOut = better
			r.editOrder = func(call int, order *jupiter.Order) { order.OtherAmountThreshold = 1 }
		}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRecovery(t, enabled+"\n  max_price_gap: 0.005")
			test.setup(r)
			_, _, err := r.funder().step(context.Background(), recovRequest(7, 1_656))
			if !errors.Is(err, ErrPriceGuard) {
				t.Fatal(err)
			}
			if r.takers != test.orders || len(r.simulated) != 0 || r.saves != 0 || r.executes != 0 {
				t.Fatalf("%d orders, %d simulated, %d saves, %d executes", r.takers, len(r.simulated), r.saves, r.executes)
			}
		})
	}
}

// Asking for more than max_slippage_bps is never the way to the floor.
func TestRecoveryASecondOrdersSlippageIsCapped(t *testing.T) {
	r := newRecovery(t, enabled+"\n  max_slippage_bps: 20")
	r.editOrder = func(call int, order *jupiter.Order) {
		if call == 1 {
			order.OtherAmountThreshold = 1
		}
	}
	r.stepOnce(r.funder(), recovRequest(7, 1_656))
	if r.takers != 2 || r.asked[1] != 20 {
		t.Fatalf("%d orders, asked %v", r.takers, r.asked)
	}
}

// A quote is not the price: a quote below the floor, as a quote from a worse route can
// be, does not stop an order for this wallet that clears it.
func TestRecoveryAPoorQuoteDoesNotStopAGoodOrder(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.quoteOut = func(fair uint64) uint64 { return fair * 95 / 100 }
	r.stepOnce(r.funder(), recovRequest(7, 1_656))
	if r.takers != 1 || len(r.swaps) != 1 {
		t.Fatalf("%d orders, %d swaps", r.takers, len(r.swaps))
	}
}

// An RFQ order fills at its output, and needs no second order.
func TestRecoveryAnRFQOrderIsNotPlacedAgain(t *testing.T) {
	r := newRecovery(t, enabled)
	r.shape = recovRFQ
	r.quoteOut = func(fair uint64) uint64 { return fair * 11 / 10 }
	r.stepOnce(r.funder(), recovRequest(7, 1_656))
	if r.takers != 1 || len(r.swaps) != 1 {
		t.Fatalf("%d orders, %d swaps", r.takers, len(r.swaps))
	}
}

// USDC arriving between the first read and the simulation is no reason to refuse.
func TestRecoveryUSDCArrivingBeforeTheSimulationIsNoRefusal(t *testing.T) {
	r := newRecovery(t, enabled)
	r.onTaker = func(int) { r.usdc += 100 * microPerUSDC }
	r.stepOnce(r.funder(), recovRequest(7, 1_656))
	if len(r.swaps) != 1 {
		t.Fatalf("%d swaps", len(r.swaps))
	}
	// And USDC that left in that window is the shortage it is.
	r = newRecovery(t, enabled)
	r.onTaker = func(int) { r.usdc = 1 }
	if _, _, err := r.funder().step(context.Background(), recovRequest(7, 1_656)); !errors.Is(err, ErrInsufficientUSDC) {
		t.Fatal(err)
	}
}

// An ask that is no price leaves no floor, so it is refused before it sizes anything.
func TestRecoveryAnAskThatIsNoPriceIsRefused(t *testing.T) {
	for _, ask := range []string{"NaN", "+Inf", "0", "-1"} {
		r := newRecovery(t, enabled)
		value, err := strconv.ParseFloat(ask, 64)
		if err != nil {
			t.Fatal(err)
		}
		r.ask = value
		if _, _, err := r.funder().step(context.Background(), recovRequest(7, 1_656)); err == nil || !strings.Contains(err.Error(), "not a price") {
			t.Fatal(ask, err)
		}
		if r.quotes != 0 || r.takers != 0 {
			t.Fatal(ask, r.quotes, r.takers)
		}
	}
}

// ---- 9: the smallest stake reaches the hotkey check

// The chain refuses a small amount before it looks at the hotkey. The dry run has to
// clear that, or no missing hotkey is ever named.
func TestRecoveryTheDryRunReachesTheHotkeyCheck(t *testing.T) {
	r := newRecovery(t, enabled)
	r.chainMin = 2_001_007
	f := r.funder()
	status, err := f.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.HotkeyError != "" {
		t.Fatal("a good hotkey reads as", status.HotkeyError)
	}
	r.hotkeyErr = errors.New("dispatch execution failed: HotKeyAccountNotExists")
	if status, err = f.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.HotkeyError, "HotKeyAccountNotExists") {
		t.Fatal("a missing hotkey reads as", status.HotkeyError)
	}
	if _, err := f.Ensure(context.Background(), recovRequest(7, 1_656)); !errors.Is(err, ErrHotkeyNotReady) || !strings.Contains(err.Error(), "HotKeyAccountNotExists") {
		t.Fatal(err)
	}
}

// ---- 10: a delivery that does not show cannot wedge the route

// Scan says delivered and the wTAO balance is short of it. That may be a read behind
// the delivery, so at first nothing is decided, and nothing is bought or sent. If it
// stays that way the wTAO has moved on, and the route goes on from the balances.
func TestRecoveryADeliveryTheBalanceDoesNotShowCountsAsArrivedInTime(t *testing.T) {
	r := newRecovery(t, enabled)
	r.nativeWei, r.wtaoWei = taoWei(0.2), new(big.Int)
	r.seedState(&State{Epoch: 7, Bridge: &BridgeRecord{Signature: "sig", AmountRao: 3 * raoPerTAO, WTAOBeforeWei: taoWei(5).String(), LandedAt: r.clock}})
	r.scan = bridge.StatusDelivered
	f := r.funder()
	request := recovRequest(7, 1_000)
	if wait := r.stepOnce(f, request); !strings.Contains(wait, "LayerZero delivery") {
		t.Fatal(wait)
	}
	journal := r.state().Bridge
	if journal == nil || journal.DeliveredAt.IsZero() || r.count("arrived") != 0 {
		t.Fatalf("journal %+v, steps %s", journal, r.kinds())
	}
	r.passTo(journal.DeliveredAt.Add(deliveredSettleWait - time.Second))
	if wait := r.stepOnce(f, request); !strings.Contains(wait, "LayerZero delivery") || r.state().Bridge == nil {
		t.Fatal(wait)
	}
	if r.takers != 0 || len(r.broadcasted) != 0 {
		t.Fatalf("%d orders, %d broadcasts", r.takers, len(r.broadcasted))
	}
	r.passTo(journal.DeliveredAt.Add(deliveredSettleWait))
	r.stepOnce(f, request)
	state := r.state()
	if r.count("arrived") != 1 || !r.logged("it has moved on") || len(r.broadcasted) != 0 || state.Bridge != nil {
		t.Fatalf("steps %s, %d broadcasts, state %+v", r.kinds(), len(r.broadcasted), state)
	}
}

// The ordinary case of that: the read catches up, and it arrives by its balance.
func TestRecoveryAReadBehindADeliveryCatchesUp(t *testing.T) {
	r := newRecovery(t, enabled)
	r.tao = 7 * raoPerTAO
	r.scan = bridge.StatusDelivered
	r.deliverAfter = 3
	result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_000))
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(r.sends) != 1 || r.takers != 0 || r.count("arrived") != 1 || r.logged("it has moved on") {
		t.Fatalf("%d sends, %d orders, steps %s, logs %v", len(r.sends), r.takers, r.kinds(), r.logs)
	}
}

// Only DELIVERED counts: FAILED and BLOCKED wait on, say so, and never send again.
func TestRecoveryAFailedOrBlockedDeliveryWaitsAndSaysSo(t *testing.T) {
	for _, status := range []string{bridge.StatusFailed, bridge.StatusBlocked} {
		t.Run(status, func(t *testing.T) {
			r := newRecovery(t, enabled)
			r.tao = 7 * raoPerTAO
			r.deliverAfter = 1_000_000
			r.scan = status
			result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_000))
			if err != nil {
				t.Fatal(err)
			}
			if result.Funded || !strings.Contains(result.Waiting, status) || !r.logged(status) {
				t.Fatalf("%+v, logs %v", result, r.logs)
			}
			if len(r.sends) != 1 || len(r.broadcasted) != 1 || r.takers != 0 || r.state().Bridge == nil || r.count("arrived") != 0 {
				t.Fatalf("%d sends, %d broadcasts, %d orders, steps %s", len(r.sends), len(r.broadcasted), r.takers, r.kinds())
			}
		})
	}
}

// A delivery under way is no reason to leave TAO already held unstaked, and the journal
// of it outlives the stake.
func TestRecoveryAWaitingBridgeDoesNotBlockStakingHeldTAO(t *testing.T) {
	r := newRecovery(t, enabled)
	r.tao = 7 * raoPerTAO
	r.deliverAfter = 1_000_000
	f := r.funder()
	request := recovRequest(7, 1_000)
	if wait := r.stepOnce(f, request); wait != "bridge send" {
		t.Fatal(wait)
	}
	r.nativeWei = taoWei(5) // the operator tops up while it travels
	result, err := f.Ensure(context.Background(), request)
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if got := r.kinds(); got != "bridged,staked" {
		t.Fatal("steps", got)
	}
	if r.state().Bridge == nil || len(r.sends) != 1 || r.takers != 0 {
		t.Fatalf("state %+v, %d sends, %d orders", r.state(), len(r.sends), r.takers)
	}
	// With too little to cover the shortfall it stakes nothing and just waits.
	r.alphaRao, r.nativeWei = new(big.Int), taoWei(1)
	if wait := r.stepOnce(f, request); !strings.Contains(wait, "LayerZero delivery") || r.count("staked") != 1 {
		t.Fatalf("it waits on %q, steps %s", wait, r.kinds())
	}
}

// ---- 11: nothing landable exists before the gate has passed

func TestRecoveryABridgeSendIsSimulatedUnsigned(t *testing.T) {
	r := newRecovery(t, enabled)
	r.tao = 7 * raoPerTAO
	r.stepOnce(r.funder(), recovRequest(7, 1_000))
	if len(r.simulated) != 1 || len(r.broadcasted) != 1 {
		t.Fatalf("%d simulated, %d broadcast", len(r.simulated), len(r.broadcasted))
	}
	simulated, err := solana.ParseTransaction(r.simulated[0])
	if err != nil {
		t.Fatal(err)
	}
	sent, err := solana.ParseTransaction(r.broadcasted[0])
	if err != nil {
		t.Fatal(err)
	}
	if simulated.Signatures[0] != ([64]byte{}) || !bytes.Equal(simulated.Message, sent.Message) || !sent.SignedBy(r.cfg.USDCAddress) {
		t.Fatal("not the same send, unsigned and then signed")
	}
}

// A refused send was never signed, so there is nothing a later mistake could broadcast.
func TestRecoveryARefusedBridgeSendWasNeverSigned(t *testing.T) {
	r := newRecovery(t, enabled)
	r.tao = 7 * raoPerTAO
	r.simulateErr = json.RawMessage(`"BlockhashNotFound"`)
	if _, _, err := r.funder().step(context.Background(), recovRequest(7, 1_000)); err == nil || !strings.Contains(err.Error(), "does not simulate") {
		t.Fatal(err)
	}
	if r.saves != 0 || len(r.broadcasted) != 0 {
		t.Fatalf("%d saves, %d broadcasts", r.saves, len(r.broadcasted))
	}
	for _, raw := range r.simulated {
		tx, err := solana.ParseTransaction(raw)
		if err != nil {
			t.Fatal(err)
		}
		if tx.Signatures[0] != ([64]byte{}) {
			t.Fatal("simulated a signed send")
		}
	}
}

// ---- 12: below confirmed, a status decides nothing

func TestRecoveryAFailureBelowConfirmedDecidesNothing(t *testing.T) {
	failure := json.RawMessage(`{"InstructionError":[2,{"Custom":6001}]}`)
	t.Run("a swap", func(t *testing.T) {
		r := newRecovery(t, enabled)
		r.reply = func(int) recovReply { return recovDropped }
		f := r.funder()
		request := recovRequest(7, 1_656)
		r.stepOnce(f, request)
		journal := r.state().Swap
		r.statuses[journal.Signature] = &solana.SignatureStatus{Err: failure, ConfirmationStatus: "processed"}
		if wait := r.stepOnce(f, request); wait != "swap confirmation" || r.state().Swap == nil || r.count("swap_failed") != 0 || r.executes != 1 {
			t.Fatalf("it waits on %q, steps %s, %d executes", wait, r.kinds(), r.executes)
		}
		for _, level := range []string{"confirmed", "finalized"} {
			r.seedState(&State{Epoch: 7, SpentUSDCMicro: journal.AmountMicro, Swap: journal})
			r.events = nil
			r.statuses[journal.Signature] = &solana.SignatureStatus{Err: failure, ConfirmationStatus: level}
			r.reply = func(int) recovReply { return recovDropped }
			r.stepOnce(f, request)
			if r.count("swap_failed") != 1 {
				t.Fatal(level, r.kinds())
			}
		}
	})
	t.Run("a bridge send", func(t *testing.T) {
		r := newRecovery(t, enabled)
		r.tao = 7 * raoPerTAO
		r.sendDrop = 1
		f := r.funder()
		request := recovRequest(7, 1_000)
		r.stepOnce(f, request)
		journal := r.state().Bridge
		r.statuses[journal.Signature] = &solana.SignatureStatus{Err: failure, ConfirmationStatus: "processed"}
		if wait := r.stepOnce(f, request); wait != "bridge send confirmation" || r.state().Bridge == nil || r.count("bridge_failed") != 0 || len(r.broadcasted) != 1 {
			t.Fatalf("it waits on %q, steps %s, %d broadcasts", wait, r.kinds(), len(r.broadcasted))
		}
		r.statuses[journal.Signature] = &solana.SignatureStatus{Err: failure, ConfirmationStatus: "finalized"}
		if wait := r.stepOnce(f, request); wait != "bridge send" || r.count("bridge_failed") != 1 {
			t.Fatalf("it waits on %q, steps %s", wait, r.kinds())
		}
	})
}

// ---- 13, 14: demand.yml

func TestRecoveryTheParserAcceptsOnlyWhatIsInRange(t *testing.T) {
	for _, field := range []string{
		"max_usdc_per_epoch: .nan", "max_usdc_per_epoch: .inf", "max_usdc_per_epoch: -.inf", "max_usdc_per_epoch: 0", "max_usdc_per_epoch: -1", "max_usdc_per_epoch: 1e300", "max_usdc_per_epoch: 0.0000001",
		"max_price_gap: .nan", "max_price_gap: .inf", "max_price_gap: 0", "max_price_gap: 0.1", "max_price_gap: -0.01",
		"max_slippage_bps: 0", "max_slippage_bps: 1001", "max_slippage_bps: .nan",
		"min_sol: .nan", "min_sol: .inf", "min_sol: 0", "min_sol: 0.05", "min_sol: 0.056", "min_sol: 1e300",
		"max_bridge_fee_sol: .nan", "max_bridge_fee_sol: .inf", "max_bridge_fee_sol: 0", "max_bridge_fee_sol: 0.0000000001",
		"alpha_buffer_bps: 1001",
		"gas_reserve_tao: .nan", "gas_reserve_tao: .inf", "gas_reserve_tao: 0", "gas_reserve_tao: 0.019", "gas_reserve_tao: -1", "gas_reserve_tao: 1e300",
		"pool_price_tolerance: .nan", "pool_price_tolerance: .inf", "pool_price_tolerance: 0", "pool_price_tolerance: 0.1",
	} {
		if config, err := ParseConfig(testConfigYAML(t, enabled+"\n  "+field)); err == nil {
			t.Errorf("%s: accepted %v", field, config.Conversion)
		} else if !strings.Contains(err.Error(), "conversion.") && !strings.Contains(err.Error(), "does not parse") {
			t.Errorf("%s: %v", field, err)
		}
	}
	for field, check := range map[string]func(Conversion) bool{
		"min_sol: 0.057":          func(c Conversion) bool { return c.MinSOLLamports == 57_000_000 },
		"gas_reserve_tao: 0.02":   func(c Conversion) bool { return c.GasReserveRao == 20_000_000 },
		"max_price_gap: 0.099":    func(c Conversion) bool { return c.MaxPriceGap == 0.099 },
		"max_usdc_per_epoch: 0.5": func(c Conversion) bool { return c.MaxUSDCMicroPerEpoch == 500_000 },
	} {
		config, err := ParseConfig(testConfigYAML(t, enabled+"\n  "+field))
		if err != nil || !check(config.Conversion) {
			t.Errorf("%s: %v %v", field, config, err)
		}
	}
	defaults, err := ParseConfig(testConfigYAML(t, enabled))
	if err != nil || defaults.Conversion.GasReserveRao != 200_000_000 || defaults.Conversion.MinSOLLamports != 60_000_000 {
		t.Fatal(defaults, err)
	}
}

// yaml's errors quote what they read, so an unknown field or a bad value gets a plain
// sentence. No error, whatever its cause, holds a value from the file.
func TestRecoveryParserErrorsQuoteNothing(t *testing.T) {
	secret := "not-a-value-to-print"
	raw := string(testConfigYAML(t, enabled))
	for name, broken := range map[string]string{
		"an unknown field":     raw + "\n" + secret + ": 1\n",
		"a value of bad type":  raw + "  max_slippage_bps: " + secret + "\n",
		"a bad private key":    strings.Replace(raw, `private_key: "`, `private_key: "`+secret, 1),
		"a document in pieces": secret + ": [\n",
	} {
		_, err := ParseConfig([]byte(broken))
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRecoveryAConfigPrintsNoKeysHoweverItIsPrinted(t *testing.T) {
	const jupiterKey = "JUPITER-KEY-THAT-MUST-NOT-PRINT"
	cfg, err := ParseConfig(testConfigYAML(t, enabled+"\n  jupiter_api_key: "+jupiterKey))
	if err != nil {
		t.Fatal(err)
	}
	seed := []byte(cfg.usdcKey)[:ed25519.SeedSize]
	alphaKey := crypto.FromECDSA(cfg.AlphaKey())
	forbidden := []string{
		jupiterKey,
		strings.Trim(fmt.Sprint(seed), "[]"), // the decimal bytes, as %v prints a slice
		fmt.Sprintf("%x", seed),
		fmt.Sprintf("%X", seed),
		strings.Trim(fmt.Sprint(alphaKey), "[]"),
		hex.EncodeToString(alphaKey),
		solana.Base58Encode(cfg.usdcKey),
		solana.Base58Encode(alphaKey),
		fmt.Sprintf("%#x", cfg.AlphaKey().D),
		cfg.AlphaKey().D.String(),
	}
	values := map[string]any{
		"a value":                *cfg,
		"a pointer":              cfg,
		"a conversion":           cfg.Conversion,
		"a conversion's pointer": &cfg.Conversion,
		"a field of a struct":    struct{ Config Config }{*cfg},
		"a pointer in a struct":  struct{ Config *Config }{cfg},
		"a conversion in one":    struct{ Limits Conversion }{cfg.Conversion},
		"a slice":                []Config{*cfg},
		"a map":                  map[string]Config{"a": *cfg},
		"an interface":           []any{*cfg, cfg, cfg.Conversion},
		"a pointer to a pointer": &cfg,
	}
	for name, value := range values {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%d", "%x", "%X", "%o", "%t", "%e", "%g", "%U", "%c", "%b"} {
			if verb == "%v" {
				// Sprint and Sprintln take the same way, and are a separate call to make.
				for _, printed := range []string{fmt.Sprint(value), fmt.Sprintln(value)} {
					for _, secret := range forbidden {
						if strings.Contains(printed, secret) {
							t.Errorf("Sprint of %s prints a key", name)
						}
					}
				}
			}
			printed := fmt.Sprintf(verb, value)
			for _, secret := range forbidden {
				if strings.Contains(printed, secret) {
					t.Errorf("%s of %s prints a key", verb, name)
				}
			}
		}
	}
	if printed := fmt.Sprintf("%v %+v %s", *cfg, *cfg, *cfg); !strings.Contains(printed, cfg.AlphaAddress.Hex()) {
		t.Fatal(printed)
	}
	if printed := fmt.Sprintf("%+v", cfg.Conversion); !strings.Contains(printed, "max_usdc_micro_per_epoch 6000000000") {
		t.Fatal(printed)
	}
}

// ---- 15: wTAO dust spends no gas

func TestRecoveryWTAOBelowABridgeUnitIsLeftAlone(t *testing.T) {
	for _, test := range []struct {
		name  string
		wei   *big.Int
		steps string
	}{
		{"a bridge unit less a wei", new(big.Int).Sub(minUnwrapWei, big.NewInt(1)), "staked"},
		{"exactly a bridge unit", new(big.Int).Set(minUnwrapWei), "unwrapped,staked"},
		{"a bridge unit and a wei", new(big.Int).Add(minUnwrapWei, big.NewInt(1)), "unwrapped,staked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			r.nativeWei = taoWei(5)
			r.wtaoWei = new(big.Int).Set(test.wei)
			result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_000))
			if err != nil || !result.Funded {
				t.Fatal(result, err)
			}
			if got := r.kinds(); got != test.steps {
				t.Fatal("steps", got)
			}
		})
	}
	if unit := new(big.Int).Mul(big.NewInt(bridge.UnitRao), WeiPerRao); minUnwrapWei.Cmp(unit) != 0 {
		t.Fatal(minUnwrapWei)
	}
}

// An unwrap settling down to dust sends nothing for what is left.
func TestRecoveryAnUnwrapFindingOnlyDustSendsNothing(t *testing.T) {
	r := newRecovery(t, enabled)
	r.nativeWei, r.wtaoWei = taoWei(5), taoWei(5)
	r.onSend = func() { r.nativeWei, r.wtaoWei = taoWei(10), big.NewInt(999) }
	result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_000))
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if r.count("unwrapped") != 0 {
		t.Fatal("steps", r.kinds())
	}
}

// A sender may run a prepare more than once. What is booked is what a send returned,
// not what some run of it named.
func TestRecoveryOnlyWhatASendReturnedIsBooked(t *testing.T) {
	t.Run("an unwrap that came to nothing", func(t *testing.T) {
		r := newRecovery(t, enabled)
		r.nativeWei, r.wtaoWei = taoWei(5), taoWei(5)
		r.onPrepare = func() { r.nativeWei, r.wtaoWei = taoWei(10), new(big.Int) }
		if _, _, err := r.funder().step(context.Background(), recovRequest(7, 1_000)); err != nil {
			t.Fatal(err)
		}
		if r.count("unwrapped") != 0 || r.unwraps != 0 {
			t.Fatalf("steps %s, %d unwraps", r.kinds(), r.unwraps)
		}
	})
	t.Run("a stake that came to nothing", func(t *testing.T) {
		r := newRecovery(t, enabled)
		r.nativeWei = taoWei(20)
		r.onPrepare = func() { r.alphaRao = alpha(1_010) }
		if _, _, err := r.funder().step(context.Background(), recovRequest(7, 1_000)); err != nil {
			t.Fatal(err)
		}
		if r.count("staked") != 0 || r.stakes != 0 {
			t.Fatalf("steps %s, %d stakes", r.kinds(), r.stakes)
		}
	})
	t.Run("what the last run named", func(t *testing.T) {
		r := newRecovery(t, enabled)
		r.nativeWei, r.wtaoWei = taoWei(5), taoWei(5)
		r.onPrepare = func() { r.wtaoWei = taoWei(3) }
		if _, _, err := r.funder().step(context.Background(), recovRequest(7, 1_000)); err != nil {
			t.Fatal(err)
		}
		if booked := r.events[0].Data["wei"]; r.count("unwrapped") != 1 || booked != taoWei(3).String() {
			t.Fatalf("booked %v, steps %s", booked, r.kinds())
		}
	})
}

// ---- 16: accounting follows the money and cannot stop it

// A panic from Record, as a store's database layer raises, stops nothing.
func TestRecoveryARecordThatPanicsStopsNothing(t *testing.T) {
	defer func() {
		if failure := recover(); failure != nil {
			t.Fatalf("a panic from Record reached the caller: %v", failure)
		}
	}()
	r := newRecovery(t, enabled)
	r.onRecord = func(Event) { panic("the database is gone") }
	result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_656))
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(r.swaps) != 1 || len(r.sends) != 1 || r.stakes != 1 || !r.logged("could not record swapped: the database is gone") {
		t.Fatalf("%d swaps, %d sends, %d stakes, logs %v", len(r.swaps), len(r.sends), r.stakes, r.logs)
	}
}

// Each event is booked only once its state is saved, so a kill between them loses the
// event rather than booking it again on the next run.
func TestRecoveryEventsFollowTheSave(t *testing.T) {
	check := func(r *recovery, kinds ...string) {
		r.t.Helper()
		r.onRecord = func(event Event) {
			for _, kind := range kinds {
				if event.Kind != kind {
					continue
				}
				state := r.state()
				if state.Swap != nil || state.Bridge != nil {
					r.t.Errorf("%s booked with the journal still saved: %+v", kind, state)
				}
			}
		}
	}
	t.Run("swapped and arrived", func(t *testing.T) {
		r := newRecovery(t, enabled)
		check(r, "swapped", "arrived")
		if _, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_656)); err != nil {
			t.Fatal(err)
		}
		if r.count("swapped") != 1 || r.count("arrived") != 1 {
			t.Fatal(r.kinds())
		}
	})
	t.Run("swap_failed", func(t *testing.T) {
		r := newRecovery(t, enabled)
		r.reply = func(execute int) recovReply {
			if execute == 1 {
				return recovExpiredReported
			}
			return recovDropped
		}
		check(r, "swap_failed")
		r.stepOnce(r.funder(), recovRequest(7, 1_656))
		if r.count("swap_failed") != 1 {
			t.Fatal(r.kinds())
		}
	})
	t.Run("bridge_failed", func(t *testing.T) {
		r := newRecovery(t, enabled)
		r.tao = 7 * raoPerTAO
		r.sendDrop = 1
		f := r.funder()
		request := recovRequest(7, 1_000)
		r.stepOnce(f, request)
		r.statuses[r.state().Bridge.Signature] = &solana.SignatureStatus{Err: json.RawMessage(`"failed"`), ConfirmationStatus: "confirmed"}
		r.sendDrop = 1
		check(r, "bridge_failed")
		r.stepOnce(f, request)
		if r.count("bridge_failed") != 1 {
			t.Fatal(r.kinds())
		}
	})
	t.Run("bridge_expired", func(t *testing.T) {
		r := newRecovery(t, enabled)
		r.tao = 7 * raoPerTAO
		r.sendDrop = 2
		f := r.funder()
		request := recovRequest(7, 1_000)
		r.stepOnce(f, request)
		r.height = r.state().Bridge.LastValidBlockHeight + expiryGraceBlocks + 1
		check(r, "bridge_expired")
		r.stepOnce(f, request)
		if r.count("bridge_expired") != 1 {
			t.Fatal(r.kinds())
		}
	})
}

// ---- A: the gate keeps the wallet a wallet

func TestRecoveryTheGatesKeepTheWalletAWallet(t *testing.T) {
	someone := solana.PublicKey{9, 9, 9}
	for _, test := range []struct {
		name string
		edit func(*solana.SimulatedAccount)
		want string
	}{
		{"handed to another program", func(a *solana.SimulatedAccount) { a.Owner = someone }, "hands the wallet to"},
		{"given data to hold", func(a *solana.SimulatedAccount) { a.DataLen = 80 }, "bytes of data"},
		{"closed", func(a *solana.SimulatedAccount) { a.Exists = false }, "closes the wallet"},
	} {
		t.Run("the swap: "+test.name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			r.editSim = func(sim *solana.Simulation) { test.edit(&sim.Accounts[2]) }
			_, _, err := r.funder().step(context.Background(), recovRequest(7, 1_656))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatal(err)
			}
			if r.executes != 0 || r.saves != 0 {
				t.Fatalf("%d executes, %d saves", r.executes, r.saves)
			}
		})
		t.Run("the bridge send: "+test.name, func(t *testing.T) {
			r := newRecovery(t, enabled)
			r.tao = 7 * raoPerTAO
			r.editSim = func(sim *solana.Simulation) { test.edit(&sim.Accounts[1]) }
			_, _, err := r.funder().step(context.Background(), recovRequest(7, 1_000))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatal(err)
			}
			if len(r.broadcasted) != 0 || r.saves != 0 {
				t.Fatalf("%d broadcasts, %d saves", len(r.broadcasted), r.saves)
			}
		})
	}
}

// ---- E: a funded wallet buys no gas, and Status shows where it stands

func TestRecoveryStatusShowsNativeAgainstTheReserve(t *testing.T) {
	r := newRecovery(t, "  enabled: true\n  gas_reserve_tao: 0.3")
	r.nativeWei = taoWei(0.25)
	status, err := r.funder().Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.NativeRao != 250_000_000 || status.GasReserveRao != 300_000_000 {
		t.Fatalf("%+v", status)
	}
	raw, err := json.Marshal(status)
	if err != nil || !strings.Contains(string(raw), `"gas_reserve_rao":300000000`) || !strings.Contains(string(raw), `"native_rao":250000000`) {
		t.Fatal(string(raw), err)
	}
	// Funded, it reports that and buys nothing, below the reserve or not.
	r.alphaRao, r.nativeWei = alpha(2_000), new(big.Int)
	result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_000))
	if err != nil || !result.Funded || r.takers != 0 || r.saves != 0 {
		t.Fatal(result, err, r.takers, r.saves)
	}
}

// ---- State stays readable across this change

func TestRecoveryJournalsAreReadableEitherWay(t *testing.T) {
	older := `{"epoch":3,"spent_usdc_micro":9,"swap":{"request_id":"r","raw":"eA==","amount_micro":1,"quote_out_rao":2,"min_out_rao":3,"ask":240.5,"last_valid_block_height":7,"usdc_before":4,"tao_before":5,"signed_at":"2026-01-02T03:04:05Z","submitted_at":"0001-01-01T00:00:00Z"},"bridge":{"signature":"s","raw":"eA==","amount_rao":1,"last_valid_block_height":7,"wtao_before_wei":"12","lamports_before":6,"signed_at":"2026-01-02T03:04:05Z","landed_at":"0001-01-01T00:00:00Z"}}`
	state, err := UnmarshalState([]byte(older))
	if err != nil {
		t.Fatal(err)
	}
	if !state.Swap.ExpireAt.IsZero() || state.Swap.Succeeded || state.Bridge.TaoBefore != 0 || !state.Bridge.DeliveredAt.IsZero() || state.Swap.LastValidBlockHeight != 7 || state.Swap.TaoBefore != 5 {
		t.Fatalf("%+v %+v", state.Swap, state.Bridge)
	}
	raw, err := MarshalState(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"expire_at", "succeeded", "delivered_at"} {
		if strings.Contains(string(raw), absent) {
			t.Fatalf("%s written with no value: %s", absent, raw)
		}
	}
	state.Swap.ExpireAt = time.Unix(1_793_663_594, 0).UTC()
	state.Swap.Succeeded = true
	state.Bridge.TaoBefore = 8
	state.Bridge.DeliveredAt = time.Unix(1_793_663_600, 0).UTC()
	if raw, err = MarshalState(state); err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalState(raw)
	if err != nil || !back.Swap.ExpireAt.Equal(state.Swap.ExpireAt) || !back.Swap.Succeeded || back.Bridge.TaoBefore != 8 || !back.Bridge.DeliveredAt.Equal(state.Bridge.DeliveredAt) {
		t.Fatal(back, err)
	}
}

// ---- The default reserve, as a real demand.yml gets it

func TestRecoveryTheRouteRunsAtTheDefaultReserve(t *testing.T) {
	r := newRecovery(t, enabled)
	r.cfg.Conversion.GasReserveRao = uint64(defaultGasReserveTAO * raoPerTAO)
	result, err := r.funder().Ensure(context.Background(), recovRequest(7, 1_656))
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if got := r.kinds(); got != "swapped,bridged,arrived,unwrapped,staked" {
		t.Fatal("steps", got)
	}
	if r.nativeWei.Cmp(recovWei(r.cfg.Conversion.GasReserveRao)) < 0 {
		t.Fatalf("the stake ate the reserve: %s wei left", r.nativeWei)
	}
}

// ---- Being killed anywhere

// recovGate kills the run at one call, before or after it happens, as a process dying
// there would: runtime.Goexit, which nothing can recover and turn into an error.
type recovGate struct {
	point int
	after bool
	calls int
	name  string
}

func (g *recovGate) pass(name string, call func()) {
	g.calls++
	index := g.calls
	if index == g.point && !g.after {
		g.name = name
		runtime.Goexit()
	}
	call()
	if index == g.point && g.after {
		g.name = name
		runtime.Goexit()
	}
}

func (r *recovery) passGate(name string) {
	if r.gate != nil {
		r.gate.pass(name, func() {})
	}
}

type gatedStore struct {
	Store
	g *recovGate
}

func (s gatedStore) Load(ctx context.Context) (state *State, version uint64, err error) {
	s.g.pass("Store.Load", func() { state, version, err = s.Store.Load(ctx) })
	return
}

func (s gatedStore) Save(ctx context.Context, state *State, version uint64) (next uint64, err error) {
	s.g.pass("Store.Save", func() { next, err = s.Store.Save(ctx, state, version) })
	return
}

func (s gatedStore) Record(ctx context.Context, event Event) (err error) {
	s.g.pass("Store.Record", func() { err = s.Store.Record(ctx, event) })
	return
}

type gatedEVM struct {
	EVM
	g *recovGate
}

func (e gatedEVM) Call(ctx context.Context, to common.Address, data []byte) (out []byte, err error) {
	e.g.pass("EVM.Call", func() { out, err = e.EVM.Call(ctx, to, data) })
	return
}

func (e gatedEVM) Balance(ctx context.Context, account common.Address) (out *big.Int, err error) {
	e.g.pass("EVM.Balance", func() { out, err = e.EVM.Balance(ctx, account) })
	return
}

func (e gatedEVM) PendingNonce(ctx context.Context, account common.Address) (out uint64, err error) {
	e.g.pass("EVM.PendingNonce", func() { out, err = e.EVM.PendingNonce(ctx, account) })
	return
}

type gatedSender struct {
	Sender
	g *recovGate
}

func (s gatedSender) SendPrepared(ctx context.Context, prepare Prepare) (out string, err error) {
	s.g.pass("Sender.SendPrepared", func() { out, err = s.Sender.SendPrepared(ctx, prepare) })
	return
}

type gatedSolana struct {
	SolanaAPI
	g *recovGate
}

func (s gatedSolana) Balance(ctx context.Context, account solana.PublicKey) (out uint64, err error) {
	s.g.pass("Solana.Balance", func() { out, err = s.SolanaAPI.Balance(ctx, account) })
	return
}

func (s gatedSolana) BlockHeight(ctx context.Context) (out uint64, err error) {
	s.g.pass("Solana.BlockHeight", func() { out, err = s.SolanaAPI.BlockHeight(ctx) })
	return
}

func (s gatedSolana) TokenAccount(ctx context.Context, address solana.PublicKey) (out *solana.TokenAccount, err error) {
	s.g.pass("Solana.TokenAccount", func() { out, err = s.SolanaAPI.TokenAccount(ctx, address) })
	return
}

func (s gatedSolana) Account(ctx context.Context, address solana.PublicKey) (out *solana.Account, err error) {
	s.g.pass("Solana.Account", func() { out, err = s.SolanaAPI.Account(ctx, address) })
	return
}

func (s gatedSolana) Simulate(ctx context.Context, raw []byte, accounts []solana.PublicKey) (out *solana.Simulation, err error) {
	s.g.pass("Solana.Simulate", func() { out, err = s.SolanaAPI.Simulate(ctx, raw, accounts) })
	return
}

func (s gatedSolana) SignatureStatuses(ctx context.Context, signatures []string) (out []*solana.SignatureStatus, err error) {
	s.g.pass("Solana.SignatureStatuses", func() { out, err = s.SolanaAPI.SignatureStatuses(ctx, signatures) })
	return
}

func (s gatedSolana) LatestBlockhash(ctx context.Context) (out solana.Blockhash, err error) {
	s.g.pass("Solana.LatestBlockhash", func() { out, err = s.SolanaAPI.LatestBlockhash(ctx) })
	return
}

func (s gatedSolana) SendTransaction(ctx context.Context, raw []byte) (out string, err error) {
	s.g.pass("Solana.SendTransaction", func() { out, err = s.SolanaAPI.SendTransaction(ctx, raw) })
	return
}

type gatedJupiter struct {
	JupiterAPI
	g *recovGate
}

func (j gatedJupiter) Order(ctx context.Context, req jupiter.OrderRequest) (out *jupiter.Order, err error) {
	j.g.pass("Jupiter.Order", func() { out, err = j.JupiterAPI.Order(ctx, req) })
	return
}

func (j gatedJupiter) Execute(ctx context.Context, signed []byte, requestID string) (out *jupiter.ExecuteResult, err error) {
	j.g.pass("Jupiter.Execute", func() { out, err = j.JupiterAPI.Execute(ctx, signed, requestID) })
	return
}

type gatedMarket struct {
	MarketAPI
	g *recovGate
}

func (m gatedMarket) BestAsk(ctx context.Context) (out float64, err error) {
	m.g.pass("Market.BestAsk", func() { out, err = m.MarketAPI.BestAsk(ctx) })
	return
}

type gatedScan struct {
	ScanAPI
	g *recovGate
}

func (s gatedScan) MessageStatus(ctx context.Context, tx string) (status, destination string, err error) {
	s.g.pass("Scan.MessageStatus", func() { status, destination, err = s.ScanAPI.MessageStatus(ctx, tx) })
	return
}

func (r *recovery) gatedFunder(g *recovGate) *Funder {
	r.t.Helper()
	deps := r.deps()
	deps.Store, deps.EVM, deps.Sender = gatedStore{deps.Store, g}, gatedEVM{deps.EVM, g}, gatedSender{deps.Sender, g}
	deps.Solana, deps.Jupiter, deps.Market, deps.Scan = gatedSolana{deps.Solana, g}, gatedJupiter{deps.Jupiter, g}, gatedMarket{deps.Market, g}, gatedScan{deps.Scan, g}
	f, err := NewFunder(r.cfg, testNetuid, deps)
	if err != nil {
		r.t.Fatal(err)
	}
	return f
}

// recovScenario is a conversion and how the world treats it.
type recovScenario struct {
	name       string
	need       float64
	setup      func(*recovery)
	swaps      int // that land
	maxExecute int
	sends      int
	unwraps    int
}

func recovScenarios() []recovScenario {
	replying := func(reply recovReply) func(int) recovReply {
		return func(int) recovReply { return reply }
	}
	buying := func(shape recovShape, reply func(int) recovReply) func(*recovery) {
		return func(r *recovery) { r.shape, r.reply = shape, reply }
	}
	droppedOnce := func(execute int) recovReply {
		if execute == 1 {
			return recovDropped
		}
		return recovReplyLost
	}
	return []recovScenario{
		{name: "an AMM swap that lands and reports", need: 1_656, setup: buying(recovAMM, nil), swaps: 1, maxExecute: 1, sends: 1, unwraps: 1},
		{name: "an AMM swap, reply lost, landing at the next poll", need: 1_656, setup: buying(recovAMM, replying(recovReplyLost)), swaps: 1, maxExecute: 1, sends: 1, unwraps: 1},
		{name: "an AMM swap reported failed that lands", need: 1_656, setup: buying(recovAMM, replying(recovFailureReported)), swaps: 1, maxExecute: 1, sends: 1, unwraps: 1},
		{name: "an RFQ swap that lands and reports", need: 1_656, setup: buying(recovRFQ, nil), swaps: 1, maxExecute: 1, sends: 1, unwraps: 1},
		{name: "an RFQ swap, reply lost, landing at the next poll", need: 1_656, setup: buying(recovRFQ, replying(recovReplyLost)), swaps: 1, maxExecute: 1, sends: 1, unwraps: 1},
		{name: "an RFQ swap reported failed that lands", need: 1_656, setup: buying(recovRFQ, replying(recovFailureReported)), swaps: 1, maxExecute: 1, sends: 1, unwraps: 1},
		{name: "an RFQ swap lost once, then landing", need: 1_656, setup: buying(recovRFQ, droppedOnce), swaps: 1, maxExecute: 2, sends: 1, unwraps: 1},
		{name: "a swap with neither height nor time, reply lost", need: 1_656, setup: buying(recovUntimed, replying(recovReplyLost)), swaps: 1, maxExecute: 1, sends: 1, unwraps: 1},
		{name: "an RFQ swap with reads behind its landing", need: 1_656, setup: func(r *recovery) {
			r.shape, r.staleAfterLands = recovRFQ, 3
		}, swaps: 1, maxExecute: 1, sends: 1, unwraps: 1},
		{name: "an AMM swap with reads behind its landing and its reply lost", need: 1_656, setup: func(r *recovery) {
			r.reply, r.staleAfterLands = replying(recovReplyLost), 3
		}, swaps: 1, maxExecute: 1, sends: 1, unwraps: 1},
		{name: "an AMM swap with reads behind its landing past five minutes", need: 1_656, setup: func(r *recovery) {
			r.staleAfterLands = 24
		}, swaps: 1, maxExecute: 1, sends: 1, unwraps: 1},
		{name: "an RFQ swap, reply lost, with reads behind its landing past five minutes", need: 1_656, setup: func(r *recovery) {
			r.shape, r.reply, r.staleAfterLands = recovRFQ, replying(recovReplyLost), 24
		}, swaps: 1, maxExecute: 1, sends: 1, unwraps: 1},
		{name: "TAO on Solana, the send's reply lost", need: 1_000, setup: func(r *recovery) {
			r.tao, r.sendLost = 7*raoPerTAO, true
		}, sends: 1, unwraps: 1},
		{name: "TAO on Solana, the send dropped once", need: 1_000, setup: func(r *recovery) {
			r.tao, r.sendDrop = 7*raoPerTAO, 1
		}, sends: 1, unwraps: 1},
		{name: "wTAO to unwrap", need: 1_000, setup: func(r *recovery) { r.wtaoWei = taoWei(6) }, unwraps: 1},
		{name: "a surplus of TAO to stake", need: 1_000, setup: func(r *recovery) { r.nativeWei = taoWei(20) }},
	}
}

// Kill the conversion at every store call and external call, before it and after it,
// and resume from what is left. Whatever point it is, no more is bought or staked than
// one shortfall, and the journal and the budget end up as if nothing had happened.
func TestRecoveryKillAtEveryPointBuysAndStakesOnce(t *testing.T) {
	for _, scenario := range recovScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			need := alpha(scenario.need)
			request := Request{Epoch: 7, NeedRao: need}
			build := func() *recovery {
				r := newRecovery(t, enabled)
				r.durable = true
				scenario.setup(r)
				return r
			}
			// Each run's goroutine is its process, which a kill ends.
			run := func(r *recovery, g *recovGate) {
				f := r.gatedFunder(g)
				done := make(chan struct{})
				go func() {
					defer close(done)
					_, _ = f.Ensure(context.Background(), request)
				}()
				<-done
			}
			resume := func(r *recovery) (bool, error) {
				var lastErr error
				for range 8 {
					result, err := r.funder().Ensure(context.Background(), request)
					lastErr = err
					if err == nil && result.Funded {
						return true, nil
					}
					if err := r.sleep(context.Background(), pollInterval); err != nil {
						return false, err
					}
				}
				return false, lastErr
			}
			probe := build()
			count := &recovGate{}
			probe.gate = count
			run(probe, count)
			probe.gate = nil
			if funded, err := resume(probe); !funded {
				t.Fatal("undisturbed, it does not finish:", err, probe.kinds())
			}
			limit := new(big.Int).Div(new(big.Int).Mul(need, big.NewInt(102)), big.NewInt(100))
			points, bad := count.calls+2, 0
			for point := 1; point <= points; point++ {
				for _, after := range []bool{false, true} {
					r := build()
					g := &recovGate{point: point, after: after}
					r.gate = g
					run(r, g)
					r.gate = nil
					funded, err := resume(r)
					state := r.state()
					var wrong []string
					if !funded {
						wrong = append(wrong, fmt.Sprintf("not funded: %v", err))
					}
					if len(r.swaps) != scenario.swaps {
						wrong = append(wrong, fmt.Sprintf("%d swaps landed", len(r.swaps)))
					}
					if r.executes > scenario.maxExecute {
						wrong = append(wrong, fmt.Sprintf("%d swaps sent", r.executes))
					}
					if len(r.sends) != scenario.sends {
						wrong = append(wrong, fmt.Sprintf("%d bridge sends", len(r.sends)))
					}
					if r.stakes != 1 {
						wrong = append(wrong, fmt.Sprintf("%d stakes", r.stakes))
					}
					if r.unwraps != scenario.unwraps {
						wrong = append(wrong, fmt.Sprintf("%d unwraps", r.unwraps))
					}
					if r.alphaRao.Cmp(limit) > 0 {
						wrong = append(wrong, fmt.Sprintf("alpha %s against a need of %s", r.alphaRao, need))
					}
					if state.Swap != nil || state.Bridge != nil {
						wrong = append(wrong, fmt.Sprintf("a journal left open: %+v", state))
					}
					if state.SpentUSDCMicro != r.spentOnSwaps() {
						wrong = append(wrong, fmt.Sprintf("the budget shows %s spent of %s landed", microString(state.SpentUSDCMicro), microString(r.spentOnSwaps())))
					}
					for _, kind := range []string{"swapped", "bridged", "arrived", "unwrapped", "staked"} {
						if r.count(kind) > 1 {
							wrong = append(wrong, fmt.Sprintf("%d %s events", r.count(kind), kind))
						}
					}
					if len(wrong) > 0 {
						bad++
						t.Errorf("killed at call %d (%s, after: %v): %s; steps %s", point, g.name, after, strings.Join(wrong, "; "), r.kinds())
					}
				}
			}
			t.Logf("%d kill points, %d misbehaved", 2*points, bad)
		})
	}
}
