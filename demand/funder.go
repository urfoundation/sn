package demand

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/demand/bridge"
	"github.com/urfoundation/sn/demand/jupiter"
	"github.com/urfoundation/sn/demand/solana"
	"github.com/urfoundation/sn/ss58"
)

// USDCMint is USDC on Solana.
var USDCMint = solana.MustPublicKey("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")

var (
	// ErrDisabled is a short alpha wallet while conversion.enabled is off.
	ErrDisabled = errors.New("demand: the alpha wallet is short and USDC conversion is disabled")
	// ErrInsufficientUSDC is a USDC wallet too small for the whole shortfall.
	// Nothing converts: part of a deposit does not pay it.
	ErrInsufficientUSDC = errors.New("demand: the USDC wallet cannot cover the shortfall")
	// ErrInsufficientSOL is a USDC wallet without the SOL for the route's fees.
	ErrInsufficientSOL = errors.New("demand: the USDC wallet needs SOL for fees")
	// ErrInsufficientGas is an alpha wallet without the TAO to finish the route.
	ErrInsufficientGas = errors.New("demand: the alpha wallet needs TAO for gas")
	// ErrHotkeyNotReady is a deposit hotkey a dry run of the smallest stake says
	// cannot take stake, for example one that is not registered yet.
	ErrHotkeyNotReady = errors.New("demand: the deposit hotkey cannot take stake")
	// ErrEpochBudget is a conversion beyond conversion.max_usdc_per_epoch, or for
	// an epoch older than the one the budget has moved on to.
	ErrEpochBudget = errors.New("demand: the conversion exceeds the epoch's USDC budget")
	// ErrPriceGuard is a Jupiter price too far above Coinbase's public ask.
	ErrPriceGuard = errors.New("demand: Jupiter's price is beyond the guard")
	// ErrBridgeChanged is a bridge whose peers are no longer the expected ones.
	ErrBridgeChanged = errors.New("demand: the bridge configuration changed")
	// ErrStale is a state write that lost to another writer.
	ErrStale = errors.New("demand: the conversion state changed underneath this writer")
)

const (
	// Above the least the staking precompile accepts, which is the chain's minimum
	// plus the pool's fee: a dry run on SN25 refused 2,001,006 rao with AmountTooLow
	// and passed 2,001,007 (2026-11-02).
	minStakeRao = 2_100_000
	// What the alpha wallet must hold before TAO sets out for it. An unwrap costs
	// about 42,000 gas, or 1,300,000 rao at the 30 gwei fee cap, and the dry run of
	// the smallest stake needs that stake in the balance to speak to the hotkey at all.
	minRouteNativeRao = minStakeRao
	// Jupiter's orders below about a dollar are all fees.
	minSwapMicro = 1_000_000
	// A swap may spend this much of the wallet's SOL: fees, its TAO account the
	// first time, and an intermediate account Jupiter's route may open.
	maxSwapLamports = 6_000_000
	// What a bridge send may spend beyond LayerZero's fee.
	bridgeTxLamports = 1_000_000
	// Blocks past a transaction's last valid height before it counts as expired,
	// so a lagging node cannot make a landed transaction look lost.
	expiryGraceBlocks = 30
	// JupiterZ's orders have no last valid height, only a time about 53 seconds
	// ahead. The chain's clock can run behind this one, and an order's blockhash
	// outlives that time, so such a swap counts as expired this long after it.
	swapExpiryGrace = 2 * time.Minute
	// A swap with neither a height nor a time counts as expired this long after it
	// was signed, which outlasts any blockhash it can carry. A transaction's life
	// runs from when it was built, not from when it went out, which a kill can leave
	// unrecorded.
	swapNoExpiryWait = 3 * time.Minute
	// An order's time further ahead than this is not believed: the journal would
	// hold the route for all of it, and no blockhash lasts that long.
	maxOrderExpiry = 10 * time.Minute
	// A swap that landed, or whose USDC left, keeps its journal until its TAO shows,
	// since a read of the wallet can lag the swap it follows, and closing the journal
	// on one would buy twice. Balances cannot tell that from USDC withdrawn or TAO
	// that never came, so the journal stays this long after signing, and then needs
	// a person.
	swapUnseenWait = 30 * time.Minute
	// A read behind a swap by seconds is ordinary, so nothing is said this soon.
	swapAlertAfter = time.Minute
	// A second order's price can move from the first's, so its slippage leaves this
	// much of the room to the floor unused.
	reorderMarginBps = 10
	// LayerZero Scan can report a delivery before a read of wTAO shows it, so a
	// delivery the balance does not show counts as arrived only this long after.
	deliveredSettleWait = 5 * time.Minute
	// The sizing margin over Coinbase's ask, and over Jupiter's first quote.
	swapPriceMargin = 1.005
	swapQuoteMargin = 1.002
	// Ensure works this long before reporting that it is still waiting. The
	// route usually takes under ten minutes.
	ensureBudget     = 12 * time.Minute
	pollInterval     = 15 * time.Second
	deliveryWarnWait = 15 * time.Minute
	// Swap, its resolution, unwrap, stake, each a few times over.
	maxActionsPerEnsure = 12
)

// minUnwrapWei is the least wTAO worth an unwrap's gas: one bridge unit, which is
// also the least a delivery brings. Anyone can send the wallet wTAO, and less than
// this must not be able to make it spend gas.
var minUnwrapWei = new(big.Int).Mul(big.NewInt(bridge.UnitRao), WeiPerRao)

// State is what the Funder must remember between runs. Everything else it reads
// from the chains, so a crash at any point resumes from what really happened.
type State struct {
	Epoch          uint64 `json:"epoch"`
	SpentUSDCMicro uint64 `json:"spent_usdc_micro"`
	// Swap and Bridge are journaled before they are sent: while one is set, the
	// USDC or TAO it spent may already be gone from the balances.
	Swap   *SwapRecord   `json:"swap,omitempty"`
	Bridge *BridgeRecord `json:"bridge,omitempty"`
}

// SwapRecord is a signed Jupiter swap of USDC for TAO.
type SwapRecord struct {
	RequestID            string  `json:"request_id"`
	Signature            string  `json:"signature,omitempty"`
	Raw                  string  `json:"raw"`
	AmountMicro          uint64  `json:"amount_micro"`
	QuoteOutRao          uint64  `json:"quote_out_rao"`
	MinOutRao            uint64  `json:"min_out_rao"`
	Ask                  float64 `json:"ask"`
	Router               string  `json:"router,omitempty"`
	LastValidBlockHeight uint64  `json:"last_valid_block_height"`
	// ExpireAt is when the order lapses, for one without a last valid height. A
	// record with neither counts from SignedAt.
	ExpireAt    time.Time `json:"expire_at,omitzero"`
	USDCBefore  uint64    `json:"usdc_before"`
	TaoBefore   uint64    `json:"tao_before"`
	SignedAt    time.Time `json:"signed_at"`
	SubmittedAt time.Time `json:"submitted_at"`
	// Succeeded is Jupiter's report that the swap landed, which holds even while a
	// node has not caught up with it.
	Succeeded bool `json:"succeeded,omitempty"`
}

// BridgeRecord is a signed bridge send, and once landed, the delivery awaited.
type BridgeRecord struct {
	Signature            string `json:"signature"`
	Raw                  string `json:"raw"`
	AmountRao            uint64 `json:"amount_rao"`
	LastValidBlockHeight uint64 `json:"last_valid_block_height"`
	WTAOBeforeWei        string `json:"wtao_before_wei"`
	LamportsBefore       uint64 `json:"lamports_before"`
	// TaoBefore is the wallet's TAO on Solana before the send, whose leaving shows
	// a landing the send's status no longer does.
	TaoBefore uint64    `json:"tao_before,omitempty"`
	SignedAt  time.Time `json:"signed_at"`
	LandedAt  time.Time `json:"landed_at"`
	// DeliveredAt is when LayerZero Scan first reported a delivery that a read of
	// the wTAO balance does not show.
	DeliveredAt time.Time `json:"delivered_at,omitzero"`
}

// Event is one step of a conversion, kept for accounting.
type Event struct {
	Time  time.Time      `json:"time"`
	Epoch uint64         `json:"epoch"`
	Kind  string         `json:"kind"`
	Data  map[string]any `json:"data,omitempty"`
}

// Store keeps the Funder's state durably.
type Store interface {
	// Load is the state and its version; a zero state at version 0 at first.
	Load(ctx context.Context) (*State, uint64, error)
	// Save replaces the state if it is still at version and returns the new
	// version. A write that lost wraps ErrStale.
	Save(ctx context.Context, state *State, version uint64) (uint64, error)
	// Record appends an event.
	Record(ctx context.Context, event Event) error
}

// EVM reads Bittensor EVM as the alpha wallet.
type EVM interface {
	Call(ctx context.Context, to common.Address, data []byte) ([]byte, error)
	Balance(ctx context.Context, account common.Address) (*big.Int, error)
	PendingNonce(ctx context.Context, account common.Address) (uint64, error)
}

// Prepare names one alpha wallet transaction, or "" when none is due. It runs
// after the sender has settled the wallet's earlier transactions, so it sizes
// from balances those have already changed.
type Prepare func(ctx context.Context) (operation string, to common.Address, data []byte, err error)

// Sender sends alpha wallet transactions durably: an operation is sent once, and
// SendPrepared returns only after its finalized inclusion. A Prepare that names
// no operation sends nothing, and still waits for the earlier transactions.
type Sender interface {
	SendPrepared(ctx context.Context, prepare Prepare) (string, error)
}

// SolanaAPI is the Solana RPC the route uses.
type SolanaAPI interface {
	Balance(ctx context.Context, account solana.PublicKey) (uint64, error)
	BlockHeight(ctx context.Context) (uint64, error)
	TokenAccount(ctx context.Context, address solana.PublicKey) (*solana.TokenAccount, error)
	Account(ctx context.Context, address solana.PublicKey) (*solana.Account, error)
	Simulate(ctx context.Context, tx []byte, accounts []solana.PublicKey) (*solana.Simulation, error)
	SignatureStatuses(ctx context.Context, signatures []string) ([]*solana.SignatureStatus, error)
	LatestBlockhash(ctx context.Context) (solana.Blockhash, error)
	SendTransaction(ctx context.Context, tx []byte) (string, error)
}

// JupiterAPI is the swap API the route uses.
type JupiterAPI interface {
	Order(ctx context.Context, req jupiter.OrderRequest) (*jupiter.Order, error)
	Execute(ctx context.Context, signedTx []byte, requestID string) (*jupiter.ExecuteResult, error)
}

// MarketAPI is the price reference.
type MarketAPI interface {
	BestAsk(ctx context.Context) (float64, error)
}

// ScanAPI reports a bridge message's status.
type ScanAPI interface {
	MessageStatus(ctx context.Context, sourceTx string) (status, destinationTx string, err error)
}

// Deps are the Funder's connections. Store, EVM and Sender come from the
// server; a nil Jupiter, Market or Scan takes its public default.
type Deps struct {
	Store   Store
	EVM     EVM
	Sender  Sender
	Solana  SolanaAPI
	Jupiter JupiterAPI
	Market  MarketAPI
	Scan    ScanAPI
	Logf    func(format string, args ...any)
	Now     func() time.Time
	Sleep   func(ctx context.Context, d time.Duration) error
}

// Funder keeps the alpha wallet able to pay demand deposits.
type Funder struct {
	cfg    *Config
	netuid uint16
	deps   Deps
	source [32]byte
	usdc   solana.PublicKey
	tao    solana.PublicKey
}

// NewFunder builds a Funder for one subnet.
func NewFunder(cfg *Config, netuid uint16, deps Deps) (*Funder, error) {
	if cfg == nil || netuid == 0 || deps.Store == nil || deps.EVM == nil || deps.Sender == nil || deps.Solana == nil {
		return nil, errors.New("demand: funder configuration is incomplete")
	}
	if deps.Jupiter == nil {
		deps.Jupiter = jupiter.NewClient(cfg.Conversion.JupiterAPIKey)
	}
	if deps.Market == nil {
		deps.Market = NewCoinbaseMarket()
	}
	if deps.Scan == nil {
		deps.Scan = bridge.NewScan("")
	}
	if deps.Logf == nil {
		deps.Logf = func(string, ...any) {}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Sleep == nil {
		deps.Sleep = sleepContext
	}
	usdc, err := solana.AssociatedTokenAddress(cfg.USDCAddress, USDCMint)
	if err != nil {
		return nil, err
	}
	tao, err := solana.AssociatedTokenAddress(cfg.USDCAddress, bridge.TaoMint)
	if err != nil {
		return nil, err
	}
	return &Funder{cfg: cfg, netuid: netuid, deps: deps, source: ss58.EvmMirrorPubkey(cfg.AlphaAddress), usdc: usdc, tao: tao}, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Request asks that the alpha wallet hold NeedRao of alpha on the deposit hotkey.
type Request struct {
	Epoch   uint64
	NeedRao *big.Int
}

// Result reports a run. A result that is not Funded says why in Waiting: either
// something is under way, or the run took as many steps as it may, which can leave
// nothing under way at all. Either way the caller runs Ensure again later.
type Result struct {
	Funded   bool
	AlphaRao *big.Int
	Waiting  string
}

// Ensure makes the alpha wallet able to pay, converting USDC if it must. It
// works until the wallet is funded, a step refuses (an error), its budget of time
// runs out on a step still under way, or it has taken as many steps as a run may
// (each of those a Result with Waiting).
func (f *Funder) Ensure(ctx context.Context, request Request) (*Result, error) {
	if request.NeedRao == nil || request.NeedRao.Sign() < 0 {
		return nil, errors.New("demand: invalid alpha requirement")
	}
	deadline := f.deps.Now().Add(ensureBudget)
	stalled := func(wait string) (*Result, error) {
		alpha, err := f.AlphaRao(ctx)
		if err != nil {
			return nil, err
		}
		return &Result{AlphaRao: alpha, Waiting: wait}, nil
	}
	// A step that is not waiting has just done something. A healthy conversion
	// does a handful of them, so this many means the steps are not getting the
	// wallet any closer, and each may have cost gas.
	acted := 0
	for {
		result, wait, err := f.step(ctx, request)
		if err != nil || result != nil {
			return result, err
		}
		if wait == "" {
			if acted++; acted >= maxActionsPerEnsure {
				return stalled(fmt.Sprintf("%d conversion steps ran without funding the wallet", acted))
			}
			continue
		}
		if !f.deps.Now().Before(deadline) {
			return stalled(wait)
		}
		if err := f.deps.Sleep(ctx, pollInterval); err != nil {
			return nil, err
		}
	}
}

// step does one thing. It returns a result when finished, what it is waiting
// for when something is under way, and neither when the next step is due now.
func (f *Funder) step(ctx context.Context, request Request) (*Result, string, error) {
	// The sender finishes the wallet's earlier transactions first, or a stake still
	// mining would read as a shortfall to buy. With conversion off nothing here can
	// buy, and nothing touches the sender.
	if f.cfg.Conversion.Enabled {
		if err := f.settle(ctx); err != nil {
			return nil, "", err
		}
	}
	alpha, err := f.AlphaRao(ctx)
	if err != nil {
		return nil, "", err
	}
	if alpha.Cmp(request.NeedRao) >= 0 {
		return &Result{Funded: true, AlphaRao: alpha}, "", nil
	}
	if !f.cfg.Conversion.Enabled {
		return nil, "", fmt.Errorf("%w: it holds %s rao of the %s rao needed", ErrDisabled, alpha, request.NeedRao)
	}
	state, version, err := f.deps.Store.Load(ctx)
	if err != nil {
		return nil, "", err
	}
	// Only a newer epoch starts the budget again: were an older one to, alternating
	// the two would have no cap at all.
	if request.Epoch < state.Epoch {
		return nil, "", fmt.Errorf("%w: epoch %d is older than epoch %d, and spends nothing", ErrEpochBudget, request.Epoch, state.Epoch)
	}
	run := &conversion{Funder: f, state: state, version: version, epoch: request.Epoch}
	if request.Epoch > state.Epoch {
		state.Epoch, state.SpentUSDCMicro = request.Epoch, 0
	}
	reserve := f.cfg.Conversion.GasReserveRao
	if state.Bridge != nil {
		wait, err := run.resolveBridge(ctx)
		if err != nil {
			return nil, "", err
		}
		if wait != "" {
			// A delivery the route is waiting for is no reason to leave TAO the
			// wallet already holds unstaked, if that covers the shortfall.
			taoExact, taoBuffered, native, err := f.stakeNeeds(ctx, request.NeedRao, alpha)
			if err != nil {
				return nil, "", err
			}
			if native >= taoExact+reserve {
				if sent, err := f.stake(ctx, request, alpha, min(native-reserve, taoBuffered)); err != nil || sent {
					return nil, "", err
				}
			}
			return nil, wait, nil
		}
	}
	if state.Swap != nil {
		wait, err := run.resolveSwap(ctx)
		if err != nil || wait != "" {
			return nil, wait, err
		}
		// What resolving it did may still be in flight, whatever Jupiter answered,
		// and buying on now would overwrite the journal of it.
		if state.Swap != nil {
			return nil, "swap landing", nil
		}
	}
	wtao, err := f.wtaoWei(ctx)
	if err != nil {
		return nil, "", err
	}
	if wtao.Cmp(minUnwrapWei) >= 0 {
		return nil, "", f.unwrap(ctx, request)
	}
	taoExact, taoBuffered, native, err := f.stakeNeeds(ctx, request.NeedRao, alpha)
	if err != nil {
		return nil, "", err
	}
	if native >= taoExact+reserve {
		_, err := f.stake(ctx, request, alpha, min(native-reserve, taoBuffered))
		return nil, "", err
	}
	missingExact := taoExact + reserve - native
	onSolana, err := f.tokenBalance(ctx, f.tao)
	if err != nil {
		return nil, "", err
	}
	// A bridge send and a swap both end in wTAO only the wallet's own gas can unwrap,
	// so without that gas either would only park it there.
	if native < minRouteNativeRao {
		return nil, "", fmt.Errorf("%w: it holds %d rao, below the %d rao required before TAO sets out for it", ErrInsufficientGas, native, uint64(minRouteNativeRao))
	}
	bridgeable := onSolana / bridge.UnitRao * bridge.UnitRao
	bridging := bridgeable >= missingExact
	if !bridging {
		if err := run.checkRoute(ctx); err != nil {
			return nil, "", err
		}
	}
	// A stake mining between the first read of alpha and the read of TAO looks like
	// a wallet short of both. Spending starts only from an alpha read after them.
	latest, err := f.AlphaRao(ctx)
	if err != nil {
		return nil, "", err
	}
	if latest.Cmp(alpha) != 0 {
		return nil, "", nil
	}
	if bridging {
		return nil, "bridge send", run.startBridge(ctx, bridgeable)
	}
	// Rounding to bridge units can leave a wallet with nearly enough, so what is
	// bought is at least one unit.
	buy := uint64(bridge.UnitRao)
	if need := taoBuffered + reserve - native; need > onSolana && need-onSolana > buy {
		buy = need - onSolana
	}
	run.boughtOn = &onSolana
	return nil, "", run.startSwap(ctx, buy)
}

// settle waits for the wallet's earlier transactions and sends nothing.
func (f *Funder) settle(ctx context.Context) error {
	_, err := f.deps.Sender.SendPrepared(ctx, func(context.Context) (string, common.Address, []byte, error) {
		return "", common.Address{}, nil, nil
	})
	if err != nil {
		return fmt.Errorf("demand: settle the wallet's earlier transactions: %w", err)
	}
	return nil
}

// stakeNeeds is what the shortfall from alpha to need costs, exactly and with the
// buffer, and the TAO the wallet holds.
func (f *Funder) stakeNeeds(ctx context.Context, need, alpha *big.Int) (taoExact, taoBuffered, native uint64, err error) {
	if taoExact, err = f.taoForAlpha(ctx, new(big.Int).Sub(need, alpha)); err != nil {
		return 0, 0, 0, err
	}
	if native, err = f.nativeRao(ctx); err != nil {
		return 0, 0, 0, err
	}
	return taoExact, f.buffered(taoExact), native, nil
}

func (f *Funder) buffered(taoRao uint64) uint64 {
	return taoRao + taoRao/10_000*f.cfg.Conversion.AlphaBufferBps + 1
}

// AlphaRao is the alpha the wallet holds on the deposit hotkey.
func (f *Funder) AlphaRao(ctx context.Context) (*big.Int, error) {
	raw, err := f.deps.EVM.Call(ctx, StakingPrecompile, packGetStake(f.cfg.Hotkey, f.source, f.netuid))
	if err != nil {
		return nil, fmt.Errorf("demand: read the alpha position: %w", err)
	}
	return unpackWord("getStake", raw)
}

func (f *Funder) wtaoWei(ctx context.Context) (*big.Int, error) {
	raw, err := f.deps.EVM.Call(ctx, bridge.WTAO, packBalanceOf(f.cfg.AlphaAddress))
	if err != nil {
		return nil, fmt.Errorf("demand: read the wTAO balance: %w", err)
	}
	return unpackWord("balanceOf", raw)
}

func (f *Funder) nativeRao(ctx context.Context) (uint64, error) {
	balance, err := f.deps.EVM.Balance(ctx, f.cfg.AlphaAddress)
	if err != nil {
		return 0, fmt.Errorf("demand: read the TAO balance: %w", err)
	}
	rao := new(big.Int).Quo(balance, WeiPerRao)
	if !rao.IsUint64() {
		return 0, errors.New("demand: the TAO balance is out of range")
	}
	return rao.Uint64(), nil
}

// tokenBalance is a token account's amount, 0 when it does not exist yet.
func (f *Funder) tokenBalance(ctx context.Context, account solana.PublicKey) (uint64, error) {
	token, err := f.deps.Solana.TokenAccount(ctx, account)
	if err != nil {
		return 0, err
	}
	if token == nil {
		return 0, nil
	}
	return token.Amount, nil
}

// taoForAlpha is the TAO the pool wants for alpha rao of alpha, read from the
// pool's own swap simulation, and at least what the staking precompile accepts.
func (f *Funder) taoForAlpha(ctx context.Context, alpha *big.Int) (uint64, error) {
	if !alpha.IsUint64() {
		return 0, errors.New("demand: the alpha shortfall is out of range")
	}
	price, err := f.alphaPrice(ctx)
	if err != nil {
		return 0, err
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	estimate := new(big.Int).Quo(new(big.Int).Mul(alpha, price), scale)
	if !estimate.IsUint64() {
		return 0, errors.New("demand: the TAO estimate is out of range")
	}
	tao := max(estimate.Uint64(), minStakeRao)
	for range 6 {
		raw, err := f.deps.EVM.Call(ctx, AlphaPrecompile, packSimSwap(f.netuid, tao))
		if err != nil {
			return 0, fmt.Errorf("demand: simulate the pool swap: %w", err)
		}
		out, err := unpackWord("simSwapTaoForAlpha", raw)
		if err != nil {
			return 0, err
		}
		if out.Sign() <= 0 {
			return 0, errors.New("demand: the pool quotes no alpha")
		}
		if out.Cmp(alpha) >= 0 {
			return tao, nil
		}
		grown := new(big.Int).Mul(new(big.Int).SetUint64(tao), alpha)
		grown.Quo(grown, out)
		if !grown.IsUint64() {
			return 0, errors.New("demand: the TAO estimate is out of range")
		}
		tao = grown.Uint64() + grown.Uint64()/1_000 + 1
	}
	return 0, errors.New("demand: the pool swap simulation does not converge")
}

func (f *Funder) alphaPrice(ctx context.Context) (*big.Int, error) {
	raw, err := f.deps.EVM.Call(ctx, AlphaPrecompile, packAlphaPrice(f.netuid))
	if err != nil {
		return nil, fmt.Errorf("demand: read the alpha price: %w", err)
	}
	price, err := unpackWord("getAlphaPrice", raw)
	if err != nil {
		return nil, err
	}
	if price.Sign() <= 0 {
		return nil, errors.New("demand: the alpha price is zero")
	}
	return price, nil
}

// stakeLimit is the most rao per alpha the stake may fill at: the pool's price
// after buying with taoRao, which a constant-product pool raises by (1 + t/T)²,
// plus the tolerance.
func (f *Funder) stakeLimit(ctx context.Context, taoRao uint64) (uint64, error) {
	price, err := f.alphaPrice(ctx)
	if err != nil {
		return 0, err
	}
	raw, err := f.deps.EVM.Call(ctx, AlphaPrecompile, packTaoInPool(f.netuid))
	if err != nil {
		return 0, fmt.Errorf("demand: read the pool: %w", err)
	}
	pool, err := unpackWord("getTaoInPool", raw)
	if err != nil {
		return 0, err
	}
	if pool.Sign() <= 0 {
		return 0, errors.New("demand: the pool holds no TAO")
	}
	priceRao, _ := new(big.Float).Quo(new(big.Float).SetInt(price), new(big.Float).SetInt(WeiPerRao)).Float64()
	poolRao, _ := new(big.Float).SetInt(pool).Float64()
	growth := 1 + float64(taoRao)/poolRao
	return uint64(priceRao * growth * growth * (1 + f.cfg.Conversion.PoolPriceTolerance)), nil
}

// record books an event, after the state that made it final is saved. A kill
// between the two loses the event, which beats booking it twice. Accounting never
// stops the money, whichever way a store's database reports its failure.
func (f *Funder) record(ctx context.Context, epoch uint64, kind string, data map[string]any) {
	f.deps.Logf("[demand]epoch %d %s %v", epoch, kind, data)
	defer func() {
		if failure := recover(); failure != nil {
			f.deps.Logf("[demand]epoch %d could not record %s: %v", epoch, kind, failure)
		}
	}()
	if err := f.deps.Store.Record(ctx, Event{Time: f.deps.Now().UTC(), Epoch: epoch, Kind: kind, Data: data}); err != nil {
		f.deps.Logf("[demand]epoch %d could not record %s: %v", epoch, kind, err)
	}
}

// unwrap turns all the wallet's wTAO into TAO, unless what the sender settles first
// leaves nothing to do. The account's nonce keeps each send's operation distinct
// from every earlier one of the same amount.
func (f *Funder) unwrap(ctx context.Context, request Request) error {
	var sent *big.Int
	hash, err := f.deps.Sender.SendPrepared(ctx, func(ctx context.Context) (string, common.Address, []byte, error) {
		alpha, err := f.AlphaRao(ctx)
		if err != nil {
			return "", common.Address{}, nil, err
		}
		if alpha.Cmp(request.NeedRao) >= 0 {
			return "", common.Address{}, nil, nil
		}
		wei, err := f.wtaoWei(ctx)
		if err != nil {
			return "", common.Address{}, nil, err
		}
		if wei.Cmp(minUnwrapWei) < 0 {
			return "", common.Address{}, nil, nil
		}
		nonce, err := f.deps.EVM.PendingNonce(ctx, f.cfg.AlphaAddress)
		if err != nil {
			return "", common.Address{}, nil, err
		}
		sent = wei
		return fmt.Sprintf("demand:unwrap:%d:%s", nonce, wei), bridge.WTAO, packWithdraw(wei), nil
	})
	if err != nil {
		return fmt.Errorf("demand: unwrap wTAO: %w", err)
	}
	// Only a send that returned is booked: a sender may run prepare again, and that
	// a run named an operation does not mean it went out.
	if hash != "" {
		f.record(ctx, request.Epoch, "unwrapped", map[string]any{"wei": sent.String(), "tx": hash})
	}
	return nil
}

// stake buys alpha on the deposit hotkey with TAO, whole or not at all, and reports
// whether it sent. The amount is taken after earlier sends settle, which can cover
// some or all of the shortfall alphaBefore left, never eats the gas reserve, and
// is never less than the chain accepts.
func (f *Funder) stake(ctx context.Context, request Request, alphaBefore *big.Int, taoRao uint64) (bool, error) {
	var sent, sentLimit uint64
	hash, err := f.deps.Sender.SendPrepared(ctx, func(ctx context.Context) (string, common.Address, []byte, error) {
		alpha, err := f.AlphaRao(ctx)
		if err != nil {
			return "", common.Address{}, nil, err
		}
		if alpha.Cmp(request.NeedRao) >= 0 {
			return "", common.Address{}, nil, nil
		}
		want := taoRao
		if alpha.Cmp(alphaBefore) != 0 {
			exact, err := f.taoForAlpha(ctx, new(big.Int).Sub(request.NeedRao, alpha))
			if err != nil {
				return "", common.Address{}, nil, err
			}
			want = min(want, f.buffered(exact))
		}
		native, err := f.nativeRao(ctx)
		if err != nil {
			return "", common.Address{}, nil, err
		}
		reserve := f.cfg.Conversion.GasReserveRao
		if native <= reserve {
			return "", common.Address{}, nil, nil
		}
		amount := min(want, native-reserve)
		if amount < minStakeRao {
			return "", common.Address{}, nil, nil
		}
		limit, err := f.stakeLimit(ctx, amount)
		if err != nil {
			return "", common.Address{}, nil, err
		}
		nonce, err := f.deps.EVM.PendingNonce(ctx, f.cfg.AlphaAddress)
		if err != nil {
			return "", common.Address{}, nil, err
		}
		sent, sentLimit = amount, limit
		return fmt.Sprintf("demand:stake:%d:%d", nonce, amount), StakingPrecompile, packAddStakeLimit(f.cfg.Hotkey, amount, limit, f.netuid), nil
	})
	if err != nil {
		return false, fmt.Errorf("demand: stake TAO: %w", err)
	}
	// Only a send that returned is booked, as for an unwrap.
	if hash == "" {
		return false, nil
	}
	f.record(ctx, request.Epoch, "staked", map[string]any{"tao_rao": sent, "limit_rao_per_alpha": sentLimit, "tx": hash})
	return true, nil
}

// checkHotkey dry-runs the smallest stake, since the chain refuses a hotkey it does
// not know only after an amount it accepts.
func (f *Funder) checkHotkey(ctx context.Context) error {
	limit, err := f.stakeLimit(ctx, minStakeRao)
	if err != nil {
		return err
	}
	if err := f.stakeDryRun(ctx, limit); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %w", ErrHotkeyNotReady, err)
	}
	return nil
}

func (f *Funder) stakeDryRun(ctx context.Context, limit uint64) error {
	_, err := f.deps.EVM.Call(ctx, StakingPrecompile, packAddStakeLimit(f.cfg.Hotkey, minStakeRao, limit, f.netuid))
	return err
}

// conversion is one run's view of the durable state.
type conversion struct {
	*Funder
	state   *State
	version uint64
	epoch   uint64
	// boughtOn is the TAO balance on Solana the step decided to buy from, if a step
	// did.
	boughtOn *uint64
}

func (c *conversion) save(ctx context.Context) error {
	version, err := c.deps.Store.Save(ctx, c.state, c.version)
	if err != nil {
		return err
	}
	c.version = version
	return nil
}

// swapFloor is the least TAO (rao) amountMicro USDC may buy: its worth at
// Coinbase's ask, less the gap.
func swapFloor(amountMicro uint64, ask, gap float64) uint64 {
	return uint64(math.Floor(float64(amountMicro) / microPerUSDC / ask * (1 - gap) * raoPerTAO))
}

// swapSlippage keeps a fill above the floor and caps slippage regardless.
func swapSlippage(quoteOut, floor uint64, maxBps int) int {
	if quoteOut <= floor {
		return 1
	}
	return max(min(maxBps, int((quoteOut-floor)*10_000/quoteOut)), 1)
}

// sizeSwap is the USDC that buys needRao of TAO on Jupiter by its quote, and the
// ask the swap is to be guarded by. It places nothing, and guards nothing yet.
func (c *conversion) sizeSwap(ctx context.Context, needRao uint64) (amountMicro uint64, ask float64, err error) {
	if ask, err = c.deps.Market.BestAsk(ctx); err != nil {
		return 0, 0, err
	}
	// The floor is computed from this, and NaN or infinity would leave no floor.
	if !(ask > 0 && ask < math.Inf(1)) {
		return 0, 0, fmt.Errorf("demand: Coinbase's ask of %v is not a price", ask)
	}
	amountMicro = max(uint64(math.Ceil(float64(needRao)/raoPerTAO*ask*swapPriceMargin*microPerUSDC)), minSwapMicro)
	quote, err := c.deps.Jupiter.Order(ctx, jupiter.OrderRequest{InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: amountMicro, SlippageBps: c.cfg.Conversion.MaxSlippageBps})
	if err != nil {
		return 0, 0, fmt.Errorf("demand: quote the swap: %w", err)
	}
	if quote.OutAmount < needRao {
		amountMicro = uint64(math.Ceil(float64(amountMicro) * float64(needRao) / float64(quote.OutAmount) * swapQuoteMargin))
	}
	return amountMicro, ask, nil
}

// checkRoute refuses a swap whose TAO could not finish the route, so that USDC is
// not spent only to park TAO partway along it: the bridge is the expected one, the
// USDC wallet's SOL pays for this swap and then the bridge send, and the hotkey takes
// stake. The step has already seen that the alpha wallet can pay for its unwrap.
func (c *conversion) checkRoute(ctx context.Context) error {
	if err := c.checkBridge(ctx); err != nil {
		return err
	}
	if _, err := c.bridgeTable(ctx); err != nil {
		return err
	}
	lamports, err := c.deps.Solana.Balance(ctx, c.cfg.USDCAddress)
	if err != nil {
		return err
	}
	limits := c.cfg.Conversion
	required := max(limits.MinSOLLamports, limits.MaxBridgeFeeLamports+bridgeTxLamports+maxSwapLamports)
	if lamports < required {
		return fmt.Errorf("%w: it holds %d lamports, below the %d required", ErrInsufficientSOL, lamports, required)
	}
	return c.checkHotkey(ctx)
}

// orderExpiry is the time to journal for an order, if it has one to believe.
func (c *conversion) orderExpiry(order *jupiter.Order) time.Time {
	if order.ExpireAt.IsZero() || order.ExpireAt.After(c.deps.Now().Add(maxOrderExpiry)) {
		return time.Time{}
	}
	return order.ExpireAt.UTC()
}

// reorderSlippage is the slippage of a second order that keeps the minimum of an
// order paying out above floor. Not positive means there is no room for one.
func reorderSlippage(out, floor uint64, maxBps int) int {
	return min(maxBps, int((out-floor)*10_000/out)-reorderMarginBps)
}

// startSwap buys needRao of TAO with USDC: guarded, simulated, journaled, then
// sent. Anything refused here spends nothing. Its caller has already run checkRoute.
// A journal closes as soon as its TAO shows once, and another node's read can still
// be behind that, so TAO the step did not know of stops the swap before signing.
func (c *conversion) startSwap(ctx context.Context, needRao uint64) error {
	if c.state.Swap != nil {
		return errors.New("demand: a swap is already journaled")
	}
	limits := c.cfg.Conversion
	amount, ask, err := c.sizeSwap(ctx, needRao)
	if err != nil {
		return err
	}
	usdc, err := c.tokenBalance(ctx, c.usdc)
	if err != nil {
		return err
	}
	if usdc < amount {
		return fmt.Errorf("%w: it holds %s USDC of the %s USDC needed", ErrInsufficientUSDC, microString(usdc), microString(amount))
	}
	if c.state.SpentUSDCMicro+amount > limits.MaxUSDCMicroPerEpoch {
		return fmt.Errorf("%w: %s USDC already spent, %s USDC more, %s USDC allowed", ErrEpochBudget, microString(c.state.SpentUSDCMicro), microString(amount), microString(limits.MaxUSDCMicroPerEpoch))
	}
	floor := swapFloor(amount, ask, limits.MaxPriceGap)
	quote, err := c.deps.Jupiter.Order(ctx, jupiter.OrderRequest{InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: amount, SlippageBps: limits.MaxSlippageBps})
	if err != nil {
		return fmt.Errorf("demand: quote the swap: %w", err)
	}
	place := func(slippageBps int) (*jupiter.Order, error) {
		order, err := c.deps.Jupiter.Order(ctx, jupiter.OrderRequest{
			InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: amount,
			Taker: c.cfg.USDCAddress.String(), SlippageBps: slippageBps,
		})
		if err != nil {
			return nil, fmt.Errorf("demand: order the swap: %w", err)
		}
		return order, nil
	}
	refuse := func(order *jupiter.Order) error {
		return fmt.Errorf("%w: the order pays %d rao with a minimum of %d, below the floor of %d at Coinbase's ask of %.4f", ErrPriceGuard, order.OutAmount, order.OtherAmountThreshold, floor, ask)
	}
	// The order for this taker is the real route and price: a quote may come from
	// another router altogether, which is why it only sizes the first slippage.
	order, err := place(swapSlippage(quote.OutAmount, floor, limits.MaxSlippageBps))
	if err != nil {
		return err
	}
	if order.OutAmount < floor {
		return refuse(order)
	}
	// Paying above the floor, with a minimum below it, is slippage sized from a
	// better quote. Once, order again with slippage sized from this order.
	if order.OtherAmountThreshold < floor {
		slippage := reorderSlippage(order.OutAmount, floor, limits.MaxSlippageBps)
		if slippage <= 0 {
			return refuse(order)
		}
		if order, err = place(slippage); err != nil {
			return err
		}
		if order.OutAmount < floor || order.OtherAmountThreshold < floor {
			return refuse(order)
		}
	}
	tx, err := solana.ParseTransaction(order.Transaction)
	if err != nil {
		return fmt.Errorf("demand: Jupiter's transaction: %w", err)
	}
	// The gate compares the simulation with these to the unit, so they are read last:
	// an inflow since the first read is no reason to refuse.
	if usdc, err = c.tokenBalance(ctx, c.usdc); err != nil {
		return err
	}
	if usdc < amount {
		return fmt.Errorf("%w: it holds %s USDC of the %s USDC needed", ErrInsufficientUSDC, microString(usdc), microString(amount))
	}
	lamports, err := c.deps.Solana.Balance(ctx, c.cfg.USDCAddress)
	if err != nil {
		return err
	}
	taoBefore, err := c.checkSwap(ctx, tx, order, usdc, lamports, amount, floor)
	if err != nil {
		return fmt.Errorf("demand: refused Jupiter's transaction: %w", err)
	}
	if c.boughtOn != nil && taoBefore > *c.boughtOn {
		c.deps.Logf("[demand]epoch %d the wallet holds %d rao of TAO, not the %d rao the swap was decided on: deciding again", c.epoch, taoBefore, *c.boughtOn)
		return nil
	}
	if err := tx.Sign(c.cfg.usdcKey); err != nil {
		return err
	}
	// The ID is the fee payer's signature. Another payer, such as a JupiterZ maker,
	// usually signs inside /execute, so its ID comes from that report, or the
	// balance decides (resolveSwap). A slot it has already filled is only its ID if
	// the signature is good.
	signature := ""
	if payer := tx.FeePayer(); payer == c.cfg.USDCAddress || tx.SignedBy(payer) {
		signature = tx.ID()
	}
	c.state.Swap = &SwapRecord{
		RequestID: order.RequestID, Signature: signature, Raw: base64.StdEncoding.EncodeToString(tx.Serialize()),
		AmountMicro: amount, QuoteOutRao: order.OutAmount, MinOutRao: floor, Ask: ask, Router: order.Router,
		LastValidBlockHeight: order.LastValidBlockHeight, ExpireAt: c.orderExpiry(order), USDCBefore: usdc, TaoBefore: taoBefore, SignedAt: c.deps.Now().UTC(),
	}
	c.state.SpentUSDCMicro += amount
	if err := c.save(ctx); err != nil {
		return err
	}
	return c.executeSwap(ctx)
}

// walletIntact refuses a simulation that leaves the wallet anything but a plain
// wallet: closed, handed to another program, or holding data. Its owner is the most
// that a transaction it signs could change, since that program would then hold its
// SOL and sign for it.
func walletIntact(after solana.SimulatedAccount) error {
	switch {
	case !after.Exists:
		return errors.New("simulation closes the wallet")
	case after.Owner != solana.SystemProgram:
		return fmt.Errorf("simulation hands the wallet to %s", after.Owner)
	case after.DataLen != 0:
		return fmt.Errorf("simulation leaves %d bytes of data in the wallet", after.DataLen)
	}
	return nil
}

// checkSwap is the gate before signing: the wallet must be a required signer, the
// fee payer it or Jupiter's gasless payer, and a simulation of the three accounts
// it asks about must show exactly amount of USDC leaving, at least floor of TAO
// arriving, no delegate or close authority on either token account, the wallet
// still a plain wallet, and no more than maxSwapLamports of its SOL spent. Nothing
// else is checked: the wallet's other token accounts, for one, are not read. It
// returns the TAO balance before.
func (c *conversion) checkSwap(ctx context.Context, tx *solana.Transaction, order *jupiter.Order, usdc, lamports, amount, floor uint64) (uint64, error) {
	wallet := c.cfg.USDCAddress
	signer := false
	for _, key := range tx.RequiredSigners() {
		signer = signer || key == wallet
	}
	if !signer {
		return 0, fmt.Errorf("the wallet %s is not a required signer", wallet)
	}
	if payer := tx.FeePayer(); payer != wallet && !order.Gasless {
		return 0, fmt.Errorf("fee payer %s is neither the wallet nor a gasless payer", payer)
	}
	taoBefore, err := c.tokenBalance(ctx, c.tao)
	if err != nil {
		return 0, err
	}
	sim, err := c.deps.Solana.Simulate(ctx, order.Transaction, []solana.PublicKey{c.usdc, c.tao, wallet})
	if err != nil {
		return 0, err
	}
	if len(sim.Err) > 0 {
		return 0, fmt.Errorf("simulation failed: %s %s", sim.Err, lastLogs(sim.Logs, 3))
	}
	if len(sim.Accounts) != 3 {
		return 0, fmt.Errorf("simulation returned %d accounts, want 3", len(sim.Accounts))
	}
	usdcAfter, taoAfter, walletAfter := sim.Accounts[0].Token, sim.Accounts[1].Token, sim.Accounts[2]
	switch {
	case usdcAfter == nil || !sim.Accounts[0].Exists:
		return 0, errors.New("simulation closes the wallet's USDC account")
	case usdcAfter.Owner != wallet || usdcAfter.Mint != USDCMint || usdcAfter.Delegate != nil || usdcAfter.CloseAuthority != nil:
		return 0, errors.New("simulation changes the wallet's USDC account owner, delegate or close authority")
	case usdc < amount || usdcAfter.Amount != usdc-amount:
		return 0, fmt.Errorf("simulation moves %d of USDC, not the swap's %d", int64(usdc)-int64(usdcAfter.Amount), amount)
	case taoAfter == nil || taoAfter.Owner != wallet || taoAfter.Mint != bridge.TaoMint || taoAfter.Delegate != nil || taoAfter.CloseAuthority != nil:
		return 0, errors.New("simulation does not pay the wallet's own TAO account")
	case taoAfter.Amount < taoBefore || taoAfter.Amount-taoBefore < floor:
		return 0, fmt.Errorf("simulation pays %d rao of TAO, below the floor %d", int64(taoAfter.Amount)-int64(taoBefore), floor)
	case walletAfter.Lamports+maxSwapLamports < lamports:
		return 0, fmt.Errorf("simulation spends %d lamports of the wallet's SOL", lamports-walletAfter.Lamports)
	}
	if err := walletIntact(walletAfter); err != nil {
		return 0, err
	}
	return taoBefore, nil
}

func lastLogs(logs []string, n int) string {
	if len(logs) > n {
		logs = logs[len(logs)-n:]
	}
	return strings.Join(logs, " | ")
}

// executeSwap hands the journaled transaction to Jupiter. Whatever it reports, the
// signature or the balance decides later if it did not plainly land, and even one
// that did keeps its journal until its TAO shows.
func (c *conversion) executeSwap(ctx context.Context) error {
	rec := c.state.Swap
	raw, err := base64.StdEncoding.DecodeString(rec.Raw)
	if err != nil {
		return err
	}
	rec.SubmittedAt = c.deps.Now().UTC()
	if err := c.save(ctx); err != nil {
		return err
	}
	result, err := c.deps.Jupiter.Execute(ctx, raw, rec.RequestID)
	if err != nil {
		c.deps.Logf("[demand]epoch %d jupiter execute: %v; the signature decides", c.epoch, err)
		return nil
	}
	if rec.Signature == "" {
		rec.Signature = result.Signature
	}
	switch {
	case result.Succeeded():
		rec.Succeeded = true
		if err := c.save(ctx); err != nil {
			return err
		}
		_, err := c.resolveSwap(ctx)
		return err
	case result.Expired():
		return c.clearSwap(ctx, "expired before landing")
	}
	c.deps.Logf("[demand]epoch %d jupiter execute reported %s (code %d): %s", c.epoch, result.Status, result.Code, result.Error)
	return c.save(ctx)
}

// swapExpired reports whether a journaled swap can no longer land: past its last
// valid height, or with none, past its time, or with neither either, long enough
// after it was signed. Each of these allows for reads and clocks running behind.
func (c *conversion) swapExpired(ctx context.Context, rec *SwapRecord) (bool, error) {
	if rec.LastValidBlockHeight > 0 {
		height, err := c.deps.Solana.BlockHeight(ctx)
		if err != nil {
			return false, err
		}
		return height > rec.LastValidBlockHeight+expiryGraceBlocks, nil
	}
	now := c.deps.Now()
	if !rec.ExpireAt.IsZero() {
		return now.After(rec.ExpireAt.Add(swapExpiryGrace)), nil
	}
	return now.After(rec.SignedAt.Add(swapNoExpiryWait)), nil
}

// resolveSwap settles a journaled swap. It returns what it is waiting for, and
// nothing once it has closed the journal or has only just sent the swap. The journal
// says which.
func (c *conversion) resolveSwap(ctx context.Context) (string, error) {
	rec := c.state.Swap
	if rec.SubmittedAt.IsZero() {
		// Its submission is saved before it goes out, so this one never went. Past
		// its time there is nothing to send: a new order is better than a stale one.
		expired, err := c.swapExpired(ctx, rec)
		if err != nil {
			return "", err
		}
		if expired {
			return "", c.clearSwap(ctx, "expired before it went out")
		}
		return "", c.executeSwap(ctx)
	}
	if rec.Succeeded {
		return c.finishSwap(ctx)
	}
	if rec.Signature != "" {
		statuses, err := c.deps.Solana.SignatureStatuses(ctx, []string{rec.Signature})
		if err != nil {
			return "", err
		}
		if len(statuses) == 1 && statuses[0] != nil {
			status := statuses[0]
			// Below confirmed, an error or a success is one node's view of a block
			// that a fork can abandon, and the signed swap can still land.
			if status.ConfirmationStatus != "confirmed" && status.ConfirmationStatus != "finalized" {
				return "swap confirmation", nil
			}
			if len(status.Err) > 0 {
				return "", c.clearSwap(ctx, "failed on chain: "+string(status.Err))
			}
			return c.finishSwap(ctx)
		}
	}
	expired, err := c.swapExpired(ctx, rec)
	if err != nil {
		return "", err
	}
	if !expired {
		return "swap landing", nil
	}
	// It can no longer land, and it has had time to show if it did. A swap whose
	// signature never came back has landed if its TAO arrived. USDC only corroborates,
	// since the operator can move it at any time: a top-up hides a swap's leaving, and
	// a withdrawal looks like one. USDC where it was, with no TAO, is no swap at all.
	usdc, err := c.tokenBalance(ctx, c.usdc)
	if err != nil {
		return "", err
	}
	tao, err := c.tokenBalance(ctx, c.tao)
	if err != nil {
		return "", err
	}
	if usdc+rec.AmountMicro > rec.USDCBefore && tao < rec.TaoBefore+rec.MinOutRao {
		return "", c.clearSwap(ctx, "expired before landing")
	}
	return c.finishSwap(ctx)
}

// finishSwap books a swap that has landed, or whose USDC left. Until its TAO reaches
// the floor the journal stays, and nothing more is bought: a read behind the swap
// would otherwise read as a wallet with no TAO, and buy it all again. That lasts as
// long as a read could plausibly lag. Then TAO short of the floor is booked as what
// it is, and no TAO at all goes to a person. Its USDC stays spent against the budget
// whichever it is.
func (c *conversion) finishSwap(ctx context.Context) (string, error) {
	rec := c.state.Swap
	tao, err := c.tokenBalance(ctx, c.tao)
	if err != nil {
		return "", err
	}
	received := uint64(0)
	if tao > rec.TaoBefore {
		received = tao - rec.TaoBefore
	}
	if received < rec.MinOutRao {
		now := c.deps.Now()
		if until := rec.SignedAt.Add(swapUnseenWait); now.Before(until) {
			if !now.Before(rec.SignedAt.Add(swapAlertAfter)) {
				c.deps.Logf("[demand]epoch %d ALERT swap %s of %s USDC: %d rao of TAO shows of its floor of %d. A read behind the swap and USDC withdrawn from the wallet look the same, so nothing more is bought until %s. If this lasts, check the wallet by hand", c.epoch, rec.Signature, microString(rec.AmountMicro), received, rec.MinOutRao, until.Format(time.RFC3339))
			}
			return "the swap's TAO to show", nil
		}
		if received == 0 {
			return "", c.abandonSwap(ctx)
		}
		c.deps.Logf("[demand]epoch %d ALERT swap %s paid %d rao of TAO, below the floor %d", c.epoch, rec.Signature, received, rec.MinOutRao)
	}
	c.state.Swap = nil
	if err := c.save(ctx); err != nil {
		return "", err
	}
	c.record(ctx, c.epoch, "swapped", map[string]any{
		"usdc_micro": rec.AmountMicro, "tao_rao": received, "min_out_rao": rec.MinOutRao, "ask": rec.Ask, "router": rec.Router, "signature": rec.Signature,
	})
	return "", nil
}

// abandonSwap closes a swap nothing more can be learned about: it landed or its USDC
// left, and no TAO came. With no TAO to book it is not booked. Its USDC may well be
// spent, so its budget does not come back, and the event says it needs a person.
func (c *conversion) abandonSwap(ctx context.Context) error {
	rec := c.state.Swap
	c.state.Swap = nil
	if err := c.save(ctx); err != nil {
		return err
	}
	c.deps.Logf("[demand]epoch %d ALERT swap %s of %s USDC: no TAO has come since it was signed at %s. This needs a person: its budget stays spent, and the conversion goes on without it", c.epoch, rec.Signature, microString(rec.AmountMicro), rec.SignedAt.Format(time.RFC3339))
	c.record(ctx, c.epoch, "swap_needs_person", map[string]any{"usdc_micro": rec.AmountMicro, "min_out_rao": rec.MinOutRao, "signature": rec.Signature, "request_id": rec.RequestID, "signed_at": rec.SignedAt.Format(time.RFC3339)})
	return nil
}

func (c *conversion) clearSwap(ctx context.Context, reason string) error {
	rec := c.state.Swap
	if c.state.SpentUSDCMicro >= rec.AmountMicro {
		c.state.SpentUSDCMicro -= rec.AmountMicro
	}
	c.state.Swap = nil
	if err := c.save(ctx); err != nil {
		return err
	}
	c.record(ctx, c.epoch, "swap_failed", map[string]any{"usdc_micro": rec.AmountMicro, "reason": reason, "signature": rec.Signature})
	return nil
}

// checkBridge refuses to send TAO unless both ends still name each other.
func (c *conversion) checkBridge(ctx context.Context) error {
	peer, err := c.deps.Solana.Account(ctx, bridge.PeerConfig)
	if err != nil {
		return err
	}
	if peer == nil || peer.Owner != bridge.OFTProgram {
		return fmt.Errorf("%w: the Solana peer config is missing", ErrBridgeChanged)
	}
	if err := bridge.CheckPeerConfig(peer.Data); err != nil {
		return fmt.Errorf("%w: %w", ErrBridgeChanged, err)
	}
	raw, err := c.deps.EVM.Call(ctx, bridge.WTAO, packPeers(bridge.SolanaEID))
	if err != nil {
		return fmt.Errorf("demand: read wTAO's peer: %w", err)
	}
	if len(raw) != 32 || [32]byte(raw) != [32]byte(bridge.OFTStore) {
		return fmt.Errorf("%w: wTAO's Solana peer is %x", ErrBridgeChanged, raw)
	}
	return nil
}

// bridgeTable is LayerZero's lookup table, without which a send does not fit.
func (c *conversion) bridgeTable(ctx context.Context) (*solana.LookupTable, error) {
	account, err := c.deps.Solana.Account(ctx, bridge.LookupTable)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, fmt.Errorf("%w: LayerZero's lookup table is missing", ErrBridgeChanged)
	}
	return solana.ParseLookupTable(bridge.LookupTable, account.Data)
}

// startBridge sends amountRao of TAO to the alpha wallet over LayerZero: simulated
// unsigned, signed only once that passes, journaled, then broadcast.
func (c *conversion) startBridge(ctx context.Context, amountRao uint64) error {
	if c.state.Bridge != nil {
		return errors.New("demand: a bridge send is already journaled")
	}
	if err := c.checkBridge(ctx); err != nil {
		return err
	}
	lamports, err := c.deps.Solana.Balance(ctx, c.cfg.USDCAddress)
	if err != nil {
		return err
	}
	limits := c.cfg.Conversion
	if lamports < limits.MaxBridgeFeeLamports+bridgeTxLamports {
		return fmt.Errorf("%w: it holds %d lamports, below the bridge's %d", ErrInsufficientSOL, lamports, limits.MaxBridgeFeeLamports+bridgeTxLamports)
	}
	table, err := c.bridgeTable(ctx)
	if err != nil {
		return err
	}
	wtao, err := c.wtaoWei(ctx)
	if err != nil {
		return err
	}
	blockhash, err := c.deps.Solana.LatestBlockhash(ctx)
	if err != nil {
		return err
	}
	send := bridge.Send{Sender: c.cfg.USDCAddress, To: c.cfg.AlphaAddress, AmountRao: amountRao, MaxFeeLamports: limits.MaxBridgeFeeLamports}
	tx, err := send.Transaction(blockhash.Hash, table)
	if err != nil {
		return err
	}
	taoBefore, err := c.tokenBalance(ctx, c.tao)
	if err != nil {
		return err
	}
	sim, err := c.deps.Solana.Simulate(ctx, tx.Serialize(), []solana.PublicKey{c.tao, c.cfg.USDCAddress})
	if err != nil {
		return err
	}
	if len(sim.Err) > 0 {
		return fmt.Errorf("demand: the bridge send does not simulate: %s %s", sim.Err, lastLogs(sim.Logs, 3))
	}
	if len(sim.Accounts) != 2 || sim.Accounts[0].Token == nil || sim.Accounts[0].Token.Amount != taoBefore-amountRao {
		return errors.New("demand: the bridge send's simulation does not move exactly its TAO")
	}
	if sim.Accounts[1].Lamports+limits.MaxBridgeFeeLamports+bridgeTxLamports < lamports {
		return fmt.Errorf("demand: the bridge send's simulation spends %d lamports", lamports-sim.Accounts[1].Lamports)
	}
	if err := walletIntact(sim.Accounts[1]); err != nil {
		return fmt.Errorf("demand: the bridge send: %w", err)
	}
	if err := tx.Sign(c.cfg.usdcKey); err != nil {
		return err
	}
	raw := tx.Serialize()
	c.state.Bridge = &BridgeRecord{
		Signature: tx.ID(), Raw: base64.StdEncoding.EncodeToString(raw), AmountRao: amountRao,
		LastValidBlockHeight: blockhash.LastValidBlockHeight, WTAOBeforeWei: wtao.String(), LamportsBefore: lamports, TaoBefore: taoBefore, SignedAt: c.deps.Now().UTC(),
	}
	if err := c.save(ctx); err != nil {
		return err
	}
	if _, err := c.deps.Solana.SendTransaction(ctx, raw); err != nil {
		c.deps.Logf("[demand]epoch %d broadcast the bridge send: %v; its signature decides", c.epoch, err)
	}
	return nil
}

// resolveBridge follows a journaled send to its landing on Solana, then to the
// delivery of wTAO on Bittensor. While it waits nothing more is bought: the TAO
// it carries is in no balance. It returns what it is waiting for, and nothing
// once the wTAO has arrived or the send can no longer land.
func (c *conversion) resolveBridge(ctx context.Context) (string, error) {
	rec := c.state.Bridge
	now := c.deps.Now().UTC()
	if rec.LandedAt.IsZero() {
		statuses, err := c.deps.Solana.SignatureStatuses(ctx, []string{rec.Signature})
		if err != nil {
			return "", err
		}
		if len(statuses) != 1 || statuses[0] == nil {
			wait, err := c.rebroadcastBridge(ctx)
			if err != nil || wait != "" || c.state.Bridge == nil {
				return wait, err
			}
		} else {
			status := statuses[0]
			// Below confirmed, an error is one node's view of a block that a fork
			// can abandon, and the signed send can still land.
			if status.ConfirmationStatus != "confirmed" && status.ConfirmationStatus != "finalized" {
				return "bridge send confirmation", nil
			}
			if len(status.Err) > 0 {
				c.state.Bridge = nil
				if err := c.save(ctx); err != nil {
					return "", err
				}
				c.record(ctx, c.epoch, "bridge_failed", map[string]any{"signature": rec.Signature, "error": string(status.Err)})
				return "", nil
			}
			if err := c.noteBridgeLanded(ctx); err != nil {
				return "", err
			}
		}
	}
	wtao, err := c.wtaoWei(ctx)
	if err != nil {
		return "", err
	}
	before, ok := new(big.Int).SetString(rec.WTAOBeforeWei, 10)
	if !ok {
		return "", errors.New("demand: the journaled wTAO balance does not parse")
	}
	want := new(big.Int).Add(before, new(big.Int).Mul(new(big.Int).SetUint64(rec.AmountRao), WeiPerRao))
	if wtao.Cmp(want) >= 0 {
		return "", c.bridgeArrived(ctx)
	}
	waited := now.Sub(rec.LandedAt).Round(time.Second)
	status, _, err := c.deps.Scan.MessageStatus(ctx, rec.Signature)
	if err != nil {
		c.deps.Logf("[demand]epoch %d LayerZero Scan: %v", c.epoch, err)
	}
	if status == bridge.StatusDelivered {
		// Delivered, yet the balance is short of it: either a read that has not
		// caught up, so wait, or the wTAO has moved on since, which waiting cannot
		// change. Nothing is sent again whichever it is.
		if rec.DeliveredAt.IsZero() {
			rec.DeliveredAt = now
			if err := c.save(ctx); err != nil {
				return "", err
			}
		}
		if now.Sub(rec.DeliveredAt) >= deliveredSettleWait {
			c.deps.Logf("[demand]epoch %d bridge send %s was delivered, and the wTAO balance of %s wei is short of the %s wei that shows it: it has moved on", c.epoch, rec.Signature, wtao, want)
			return "", c.bridgeArrived(ctx)
		}
	}
	if status == bridge.StatusFailed || status == bridge.StatusBlocked || waited > deliveryWarnWait {
		c.deps.Logf("[demand]epoch %d bridge send %s has waited %s for delivery (LayerZero status %q)", c.epoch, rec.Signature, waited, status)
	}
	return fmt.Sprintf("LayerZero delivery of %d rao of TAO, %s after send %s (status %q)", rec.AmountRao, waited, rec.Signature, status), nil
}

func (c *conversion) bridgeArrived(ctx context.Context) error {
	rec := c.state.Bridge
	c.state.Bridge = nil
	if err := c.save(ctx); err != nil {
		return err
	}
	c.record(ctx, c.epoch, "arrived", map[string]any{"tao_rao": rec.AmountRao, "signature": rec.Signature, "seconds": c.deps.Now().UTC().Sub(rec.LandedAt).Seconds()})
	return nil
}

func (c *conversion) noteBridgeLanded(ctx context.Context) error {
	rec := c.state.Bridge
	rec.LandedAt = c.deps.Now().UTC()
	if err := c.save(ctx); err != nil {
		return err
	}
	spent := uint64(0)
	if lamports, err := c.deps.Solana.Balance(ctx, c.cfg.USDCAddress); err == nil && lamports < rec.LamportsBefore {
		spent = rec.LamportsBefore - lamports
	}
	c.record(ctx, c.epoch, "bridged", map[string]any{"tao_rao": rec.AmountRao, "signature": rec.Signature, "lamports": spent})
	return nil
}

// rebroadcastBridge deals with a send the chain shows no status for. It sends the
// same bytes again while they can still land, which cannot land twice. Once they
// cannot, a node's missing status proves nothing but the TAO does: gone, the send
// landed and is followed like any other, and still there, the send is cleared. It
// returns what it is waiting for, and nothing when the journal says what happened.
func (c *conversion) rebroadcastBridge(ctx context.Context) (string, error) {
	rec := c.state.Bridge
	height, err := c.deps.Solana.BlockHeight(ctx)
	if err != nil {
		return "", err
	}
	if height > rec.LastValidBlockHeight+expiryGraceBlocks {
		tao, err := c.tokenBalance(ctx, c.tao)
		if err != nil {
			return "", err
		}
		if tao+rec.AmountRao <= rec.TaoBefore {
			return "", c.noteBridgeLanded(ctx)
		}
		c.state.Bridge = nil
		if err := c.save(ctx); err != nil {
			return "", err
		}
		c.record(ctx, c.epoch, "bridge_expired", map[string]any{"signature": rec.Signature})
		return "", nil
	}
	if height <= rec.LastValidBlockHeight {
		raw, err := base64.StdEncoding.DecodeString(rec.Raw)
		if err != nil {
			return "", err
		}
		if _, err := c.deps.Solana.SendTransaction(ctx, raw); err != nil {
			c.deps.Logf("[demand]epoch %d rebroadcast the bridge send: %v", c.epoch, err)
		}
	}
	return "bridge send landing", nil
}

func microString(micro uint64) string {
	return fmt.Sprintf("%d.%06d", micro/microPerUSDC, micro%microPerUSDC)
}

// WalletStatus is what the two wallets hold and what is under way.
type WalletStatus struct {
	AlphaAddress string `json:"alpha_address"`
	Hotkey       string `json:"hotkey"`
	AlphaRao     string `json:"alpha_rao"`
	NativeRao    uint64 `json:"native_rao"`
	// GasReserveRao is what NativeRao should not fall below: a funded wallet buys
	// no TAO for gas, and the next conversion refills it.
	GasReserveRao uint64 `json:"gas_reserve_rao"`
	WTAOWei       string `json:"wtao_wei"`
	// HotkeyError is why the wallet cannot stake on its hotkey today, if so.
	HotkeyError string `json:"hotkey_error,omitempty"`

	USDCAddress string `json:"usdc_address"`
	USDCMicro   uint64 `json:"usdc_micro"`
	TaoRao      uint64 `json:"tao_rao"`
	Lamports    uint64 `json:"lamports"`

	ConversionEnabled bool   `json:"conversion_enabled"`
	State             *State `json:"state"`
}

// Status reads both wallets. It signs nothing.
func (f *Funder) Status(ctx context.Context) (*WalletStatus, error) {
	status := &WalletStatus{AlphaAddress: f.cfg.AlphaAddress.Hex(), Hotkey: common.Bytes2Hex(f.cfg.Hotkey[:]), GasReserveRao: f.cfg.Conversion.GasReserveRao, USDCAddress: f.cfg.USDCAddress.String(), ConversionEnabled: f.cfg.Conversion.Enabled}
	alpha, err := f.AlphaRao(ctx)
	if err != nil {
		return nil, err
	}
	status.AlphaRao = alpha.String()
	if status.NativeRao, err = f.nativeRao(ctx); err != nil {
		return nil, err
	}
	wtao, err := f.wtaoWei(ctx)
	if err != nil {
		return nil, err
	}
	status.WTAOWei = wtao.String()
	// A dry run of the smallest stake says whether the hotkey can take stake.
	limit, err := f.stakeLimit(ctx, minStakeRao)
	if err != nil {
		return nil, err
	}
	if err := f.stakeDryRun(ctx, limit); err != nil {
		status.HotkeyError = err.Error()
	}
	if status.USDCMicro, err = f.tokenBalance(ctx, f.usdc); err != nil {
		return nil, err
	}
	if status.TaoRao, err = f.tokenBalance(ctx, f.tao); err != nil {
		return nil, err
	}
	if status.Lamports, err = f.deps.Solana.Balance(ctx, f.cfg.USDCAddress); err != nil {
		return nil, err
	}
	if status.State, _, err = f.deps.Store.Load(ctx); err != nil {
		return nil, err
	}
	return status, nil
}

// MarshalState and UnmarshalState are the Store's encoding.
func MarshalState(state *State) ([]byte, error) { return json.Marshal(state) }

// UnmarshalState decodes a state; empty data is a zero state.
func UnmarshalState(data []byte) (*State, error) {
	state := &State{}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("demand: decode the state: %w", err)
	}
	return state, nil
}
