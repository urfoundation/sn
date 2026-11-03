package demand

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/demand/bridge"
	"github.com/urfoundation/sn/demand/jupiter"
	"github.com/urfoundation/sn/demand/solana"
)

// Live checks of the USDC to alpha route against public systems. They read and
// simulate only. Nothing here holds a key, and the doubles below refuse every
// call that could spend, so no money can move. Each test skips unless its
// variables are set:
//
//	DEMAND_LIVE_SOLANA_RPC   a Solana JSON-RPC endpoint. Providers put keys in
//	                         these URLs, so it is scrubbed from all test output.
//	DEMAND_LIVE_EVM_RPC      a Bittensor EVM JSON-RPC endpoint; an archive for
//	                         the price history.
//	DEMAND_LIVE_USDC_HOLDER  a system-owned wallet holding USDC and SOL.
//	                         TestLiveFindHolders lists candidates.
//	DEMAND_LIVE_TAO_HOLDER   optional, a system-owned wallet holding TAO and SOL,
//	                         whose bridge send is simulated to price the bridge.
//	DEMAND_LIVE_SIZES        optional, the USD sizes to try, comma separated.
//	DEMAND_LIVE_MAX_PRICE_GAP and DEMAND_LIVE_MAX_SLIPPAGE_BPS
//	                         optional, guards to try in place of the defaults.
//	DEMAND_LIVE_HISTORY_SAMPLES  optional, how many ten-minute samples of the alpha
//	                         price to read back.
//
// A taker's USDC balance moves JupiterZ's price (about 0.6 bps per $1,000), so a
// holder's numbers are only a guide to a wallet of about the same balance.

const (
	liveNetuid = uint16(25)
	// What a bridge send costs when no TAO holder is named to simulate one with.
	liveBridgeLamports = 23_000_000
	// What eth_estimateGas answered for a wTAO withdraw and an addStakeLimit.
	liveUnwrapGas = 43_967
	liveStakeGas  = 107_835
	// 50 blocks is ten minutes on Bittensor, how long the route usually takes.
	liveHistorySpacing = 50
	liveHistorySamples = 432
)

// liveSizes are the deposit sizes the route must handle, in USD;
// DEMAND_LIVE_SIZES, comma separated, replaces them.
func liveSizes() []float64 {
	sizes := []float64{500, 1600, 3000, 4300}
	if list := os.Getenv("DEMAND_LIVE_SIZES"); list != "" {
		sizes = nil
		for _, field := range strings.Split(list, ",") {
			if size, err := strconv.ParseFloat(strings.TrimSpace(field), 64); err == nil && size > 0 {
				sizes = append(sizes, size)
			}
		}
	}
	return sizes
}

var errLiveRefused = errors.New("demand: live tests never spend")

var liveHTTP = &http.Client{Timeout: 30 * time.Second}

// liveLog scrubs the Solana endpoint from everything a test reports. The route's
// clients keep its path and query out of their errors but may still name its host.
type liveLog struct {
	*testing.T
	scrub *strings.Replacer
}

func (l liveLog) Logf(format string, args ...any) {
	l.T.Helper()
	l.T.Log(l.scrub.Replace(fmt.Sprintf(format, args...)))
}

func (l liveLog) Errorf(format string, args ...any) {
	l.T.Helper()
	l.T.Error(l.scrub.Replace(fmt.Sprintf(format, args...)))
}

func (l liveLog) Fatalf(format string, args ...any) {
	l.T.Helper()
	l.T.Fatal(l.scrub.Replace(fmt.Sprintf(format, args...)))
}

func liveScrubber(endpoint string) *strings.Replacer {
	secrets := []string{endpoint}
	if u, err := url.Parse(endpoint); err == nil {
		secrets = append(secrets, u.Host, u.Hostname(), u.RawQuery, strings.Trim(u.Path, "/"))
		for _, values := range u.Query() {
			secrets = append(secrets, values...)
		}
	}
	var pairs []string
	for _, secret := range secrets {
		if secret != "" {
			pairs = append(pairs, secret, "(rpc)")
		}
	}
	return strings.NewReplacer(pairs...)
}

// liveStart skips the test unless names are all set, and bounds its work.
func liveStart(t *testing.T, names ...string) (liveLog, context.Context) {
	t.Helper()
	for _, name := range names {
		if os.Getenv(name) == "" {
			t.Skipf("set %s", strings.Join(names, " and "))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	t.Cleanup(cancel)
	return liveLog{T: t, scrub: liveScrubber(os.Getenv("DEMAND_LIVE_SOLANA_RPC"))}, ctx
}

// liveRPC is one JSON-RPC call, for what the route's own clients do not read. Its
// errors carry the method and never the endpoint.
func liveRPC(ctx context.Context, endpoint, method string, params []any, out any) error {
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%s: bad endpoint", method)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := liveHTTP.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return fmt.Errorf("%s: %w", method, urlErr.Err)
		}
		return fmt.Errorf("%s: transport error", method)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("%s: read: %w", method, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", method, resp.StatusCode)
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("%s: decode: %w", method, err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("%s: rpc error %d: %s", method, envelope.Error.Code, envelope.Error.Message)
	}
	return json.Unmarshal(envelope.Result, out)
}

// liveEVM reads Bittensor EVM. It has no send, and any other EVM method is a nil
// call: these tests only read through Call.
type liveEVM struct {
	EVM
	endpoint string
	calls    int
}

func (e *liveEVM) callAt(ctx context.Context, to common.Address, data []byte, block string) ([]byte, error) {
	var out string
	call := map[string]any{"to": to.Hex(), "data": "0x" + hex.EncodeToString(data)}
	if err := liveRPC(ctx, e.endpoint, "eth_call", []any{call, block}, &out); err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimPrefix(out, "0x"))
}

func (e *liveEVM) Call(ctx context.Context, to common.Address, data []byte) ([]byte, error) {
	e.calls++
	return e.callAt(ctx, to, data, "latest")
}

func (e *liveEVM) head(ctx context.Context) (uint64, error) {
	var out string
	if err := liveRPC(ctx, e.endpoint, "eth_blockNumber", []any{}, &out); err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimPrefix(out, "0x"), 16, 64)
}

func (e *liveEVM) gasPrice(ctx context.Context) (*big.Int, error) {
	var out string
	if err := liveRPC(ctx, e.endpoint, "eth_gasPrice", []any{}, &out); err != nil {
		return nil, err
	}
	price, ok := new(big.Int).SetString(strings.TrimPrefix(out, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("gas price %q", out)
	}
	return price, nil
}

func (e *liveEVM) word(ctx context.Context, name string, data []byte) (*big.Int, error) {
	raw, err := e.Call(ctx, AlphaPrecompile, data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return unpackWord(name, raw)
}

func (e *liveEVM) simSwap(ctx context.Context, taoRao uint64) (*big.Int, error) {
	return e.word(ctx, "simSwapTaoForAlpha", packSimSwap(liveNetuid, taoRao))
}

// liveSolana is the real RPC with its broadcast refused and its last simulation kept.
type liveSolana struct {
	*solana.Client
	last *solana.Simulation
}

func (s *liveSolana) Simulate(ctx context.Context, tx []byte, accounts []solana.PublicKey) (*solana.Simulation, error) {
	sim, err := s.Client.Simulate(ctx, tx, accounts)
	s.last = sim
	return sim, err
}

func (s *liveSolana) SendTransaction(context.Context, []byte) (string, error) {
	return "", errLiveRefused
}

// liveJupiter is the real client with /execute refused and its orders kept. It
// waits out a rate limit, which production leaves to the caller's next run.
type liveJupiter struct {
	*jupiter.Client
	orders []*jupiter.Order
}

func newLiveJupiter() *liveJupiter { return &liveJupiter{Client: jupiter.NewClient("")} }

func (j *liveJupiter) Order(ctx context.Context, req jupiter.OrderRequest) (*jupiter.Order, error) {
	for attempt := 0; ; attempt++ {
		order, err := j.Client.Order(ctx, req)
		var api *jupiter.APIError
		if errors.As(err, &api) && api.Status == http.StatusTooManyRequests && attempt < 3 {
			if err := sleepContext(ctx, 10*time.Second); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		j.orders = append(j.orders, order)
		return order, nil
	}
}

func (j *liveJupiter) Execute(context.Context, []byte, string) (*jupiter.ExecuteResult, error) {
	return nil, errLiveRefused
}

// liveRefusal is the state and the alpha wallet's sender, neither of which a live
// run may reach.
type liveRefusal struct{}

func (liveRefusal) Load(context.Context) (*State, uint64, error) { return &State{}, 0, nil }

func (liveRefusal) Save(context.Context, *State, uint64) (uint64, error) { return 0, errLiveRefused }

func (liveRefusal) Record(context.Context, Event) error { return errLiveRefused }

func (liveRefusal) SendPrepared(context.Context, Prepare) (string, error) { return "", errLiveRefused }

// liveLimits are the conversion limits demand.yml gets when it names none, less
// any of the guards a run asks to try.
func liveLimits() Conversion {
	gap, slippage := defaultMaxPriceGap, defaultMaxSlippageBps
	if value, err := strconv.ParseFloat(os.Getenv("DEMAND_LIVE_MAX_PRICE_GAP"), 64); err == nil && value > 0 {
		gap = value
	}
	if value, err := strconv.Atoi(os.Getenv("DEMAND_LIVE_MAX_SLIPPAGE_BPS")); err == nil && value > 0 {
		slippage = value
	}
	return Conversion{
		Enabled:              true,
		MaxUSDCMicroPerEpoch: uint64(defaultMaxUSDCPerEpoch * microPerUSDC),
		MaxPriceGap:          gap,
		MaxSlippageBps:       slippage,
		MinSOLLamports:       uint64(defaultMinSOL * lamportsPerSOL),
		MaxBridgeFeeLamports: uint64(defaultMaxBridgeFeeSOL * lamportsPerSOL),
		AlphaBufferBps:       defaultAlphaBufferBps,
		GasReserveRao:        uint64(defaultGasReserveTAO * raoPerTAO),
		PoolPriceTolerance:   defaultPoolPriceTolerance,
	}
}

// liveFunder is a Funder for a wallet whose key it does not have: its signing key
// is nil, so Transaction.Sign refuses before anything could be sent.
func liveFunder(t *testing.T, wallet solana.PublicKey, sol SolanaAPI, jup JupiterAPI, evm EVM) *Funder {
	t.Helper()
	cfg := &Config{USDCAddress: wallet, Conversion: liveLimits()}
	f, err := NewFunder(cfg, liveNetuid, Deps{Store: liveRefusal{}, EVM: evm, Sender: liveRefusal{}, Solana: sol, Jupiter: jup, Market: NewCoinbaseMarket()})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

type liveLevel struct{ price, size float64 }

// liveBook reads Coinbase's public book in depth: BestAsk prices only the top of it.
func liveBook(ctx context.Context, product string) (bids, asks []liveLevel, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.coinbase.com/api/v3/brokerage/market/product_book?limit=200&product_id="+product, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := liveHTTP.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	var book struct {
		Pricebook struct {
			Bids []struct {
				Price string `json:"price"`
				Size  string `json:"size"`
			} `json:"bids"`
			Asks []struct {
				Price string `json:"price"`
				Size  string `json:"size"`
			} `json:"asks"`
		} `json:"pricebook"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&book); err != nil {
		return nil, nil, err
	}
	for _, b := range book.Pricebook.Bids {
		price, _ := strconv.ParseFloat(b.Price, 64)
		size, _ := strconv.ParseFloat(b.Size, 64)
		bids = append(bids, liveLevel{price, size})
	}
	for _, a := range book.Pricebook.Asks {
		price, _ := strconv.ParseFloat(a.Price, 64)
		size, _ := strconv.ParseFloat(a.Size, 64)
		asks = append(asks, liveLevel{price, size})
	}
	if len(bids) == 0 || len(asks) == 0 {
		return nil, nil, fmt.Errorf("the %s book is empty", product)
	}
	return bids, asks, nil
}

// liveVWAP is the price usd really fills at against these asks; 0 if the book is thinner.
func liveVWAP(asks []liveLevel, usd float64) float64 {
	remaining, bought := usd, 0.0
	for _, level := range asks {
		cost := level.price * level.size
		if cost >= remaining {
			return usd / (bought + remaining/level.price)
		}
		remaining -= cost
		bought += level.size
	}
	return 0
}

// liveOrderFields are the parts of /order the Order type does not keep.
type liveOrderFields struct {
	InAmount                  string  `json:"inAmount"`
	OutAmount                 string  `json:"outAmount"`
	OtherAmountThreshold      string  `json:"otherAmountThreshold"`
	SlippageBps               int     `json:"slippageBps"`
	FeeBps                    int     `json:"feeBps"`
	Router                    string  `json:"router"`
	SwapType                  string  `json:"swapType"`
	Mode                      string  `json:"mode"`
	Gasless                   bool    `json:"gasless"`
	Maker                     string  `json:"maker"`
	GuaranteedPrice           bool    `json:"guaranteedPrice"`
	SignatureFeeLamports      uint64  `json:"signatureFeeLamports"`
	SignatureFeePayer         string  `json:"signatureFeePayer"`
	PrioritizationFeeLamports uint64  `json:"prioritizationFeeLamports"`
	PrioritizationFeePayer    string  `json:"prioritizationFeePayer"`
	RentFeeLamports           uint64  `json:"rentFeeLamports"`
	RentFeePayer              string  `json:"rentFeePayer"`
	LastValidBlockHeight      string  `json:"lastValidBlockHeight"`
	ExpireAt                  string  `json:"expireAt"`
	TransactionVersion        *int    `json:"transactionVersion"`
	InUsdValue                float64 `json:"inUsdValue"`
	OutUsdValue               float64 `json:"outUsdValue"`
	RoutePlan                 []struct {
		Percent  float64 `json:"percent"`
		SwapInfo struct {
			Label      string `json:"label"`
			OutputMint string `json:"outputMint"`
		} `json:"swapInfo"`
	} `json:"routePlan"`
}

func liveFields(order *jupiter.Order) (*liveOrderFields, error) {
	fields := &liveOrderFields{}
	if err := json.Unmarshal(order.Raw, fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func (f *liveOrderFields) route() string {
	labels := make([]string, 0, len(f.RoutePlan))
	for _, leg := range f.RoutePlan {
		labels = append(labels, fmt.Sprintf("%s:%.0f%%", leg.SwapInfo.Label, leg.Percent))
	}
	return strings.Join(labels, "+")
}

// liveCheckParse holds Client.Order's parse against the raw response.
func liveCheckParse(l liveLog, order *jupiter.Order, amount uint64) *liveOrderFields {
	l.T.Helper()
	fields, err := liveFields(order)
	if err != nil {
		l.Fatalf("raw /order does not decode: %v", err)
	}
	same := func(name, raw string, parsed uint64) {
		l.T.Helper()
		if raw == "" {
			l.Errorf("raw /order has no %s", name)
		} else if raw != strconv.FormatUint(parsed, 10) {
			l.Errorf("%s: raw %q parsed %d", name, raw, parsed)
		}
	}
	same("inAmount", fields.InAmount, order.InAmount)
	same("outAmount", fields.OutAmount, order.OutAmount)
	same("otherAmountThreshold", fields.OtherAmountThreshold, order.OtherAmountThreshold)
	switch {
	case order.InAmount != amount:
		l.Errorf("in %d for an order of %d", order.InAmount, amount)
	case order.OtherAmountThreshold == 0 || order.OtherAmountThreshold > order.OutAmount:
		l.Errorf("threshold %d against out %d", order.OtherAmountThreshold, order.OutAmount)
	case order.FeeBps != fields.FeeBps || order.SlippageBps != fields.SlippageBps || order.Router != fields.Router || order.Gasless != fields.Gasless:
		l.Errorf("parsed %+v against raw %+v", order, fields)
	}
	return fields
}

func liveRawDump(order *jupiter.Order) string {
	var body map[string]any
	if err := json.Unmarshal(order.Raw, &body); err != nil {
		return err.Error()
	}
	if tx, ok := body["transaction"].(string); ok {
		body["transaction"] = fmt.Sprintf("(%d base64 chars)", len(tx))
	}
	dump, err := json.Marshal(body)
	if err != nil {
		return err.Error()
	}
	return string(dump)
}

// liveLargestOwners are the wallets holding mint's largest accounts.
func liveLargestOwners(ctx context.Context, endpoint string, client *solana.Client, mint solana.PublicKey) ([]solana.PublicKey, error) {
	var largest struct {
		Value []struct {
			Address string `json:"address"`
		} `json:"value"`
	}
	if err := liveRPC(ctx, endpoint, "getTokenLargestAccounts", []any{mint.String(), map[string]any{"commitment": "confirmed"}}, &largest); err != nil {
		return nil, err
	}
	var owners []solana.PublicKey
	for _, entry := range largest.Value {
		token, err := client.TokenAccount(ctx, solana.MustPublicKey(entry.Address))
		if err != nil {
			return nil, err
		}
		if token != nil {
			owners = append(owners, token.Owner)
		}
	}
	return owners, nil
}

// liveRecentOwners are the wallets holding at least leastMicro of USDC after recent
// USDC transactions. RPCs refuse getTokenLargestAccounts on a mint this large.
func liveRecentOwners(ctx context.Context, endpoint string, leastMicro uint64, transactions int) ([]solana.PublicKey, error) {
	var signatures []struct {
		Signature string `json:"signature"`
	}
	if err := liveRPC(ctx, endpoint, "getSignaturesForAddress", []any{USDCMint.String(), map[string]any{"limit": transactions, "commitment": "confirmed"}}, &signatures); err != nil {
		return nil, err
	}
	seen := map[solana.PublicKey]bool{}
	var owners []solana.PublicKey
	for _, entry := range signatures {
		var tx *struct {
			Meta struct {
				PostTokenBalances []struct {
					Owner         string `json:"owner"`
					Mint          string `json:"mint"`
					UITokenAmount struct {
						Amount string `json:"amount"`
					} `json:"uiTokenAmount"`
				} `json:"postTokenBalances"`
			} `json:"meta"`
		}
		// Providers limit this read hardest, and any recent transaction will do.
		if err := sleepContext(ctx, 500*time.Millisecond); err != nil {
			return nil, err
		}
		if err := liveRPC(ctx, endpoint, "getTransaction", []any{entry.Signature, map[string]any{"encoding": "jsonParsed", "maxSupportedTransactionVersion": 1, "commitment": "confirmed"}}, &tx); err != nil || tx == nil {
			continue
		}
		for _, balance := range tx.Meta.PostTokenBalances {
			amount, err := strconv.ParseUint(balance.UITokenAmount.Amount, 10, 64)
			if err != nil || balance.Mint != USDCMint.String() || amount < leastMicro || balance.Owner == "" {
				continue
			}
			owner, err := solana.ParsePublicKey(balance.Owner)
			if err == nil && !seen[owner] {
				seen[owner] = true
				owners = append(owners, owner)
			}
		}
	}
	return owners, nil
}

// TestLiveFindHolders lists system-owned wallets a taker order can be built for:
// the largest holders of TAO, and holders of USDC from recent transactions.
func TestLiveFindHolders(t *testing.T) {
	l, ctx := liveStart(t, "DEMAND_LIVE_SOLANA_RPC")
	endpoint := os.Getenv("DEMAND_LIVE_SOLANA_RPC")
	client := solana.NewClient(endpoint)
	report := func(source string, owners []solana.PublicKey) {
		l.T.Helper()
		for _, wallet := range owners {
			account, err := client.Account(ctx, wallet)
			if err != nil {
				l.Logf("%s: %v", wallet, err)
				continue
			}
			if account == nil || account.Owner != solana.SystemProgram {
				continue
			}
			usdcATA, err := solana.AssociatedTokenAddress(wallet, USDCMint)
			if err != nil {
				l.Fatalf("%v", err)
			}
			taoATA, err := solana.AssociatedTokenAddress(wallet, bridge.TaoMint)
			if err != nil {
				l.Fatalf("%v", err)
			}
			usdc, err := client.TokenAccount(ctx, usdcATA)
			if err != nil {
				l.Logf("%s: %v", wallet, err)
				continue
			}
			tao, err := client.TokenAccount(ctx, taoATA)
			if err != nil {
				l.Logf("%s: %v", wallet, err)
				continue
			}
			if usdc == nil || usdc.Amount < 500*microPerUSDC {
				continue
			}
			// A count under the limit is the wallet's whole history, so a new wallet
			// shows here. Its token account's own history shows how quiet its balance is.
			var history, usdcHistory []struct {
				BlockTime int64 `json:"blockTime"`
			}
			if err := sleepContext(ctx, 300*time.Millisecond); err != nil {
				l.Fatalf("%v", err)
			}
			if err := liveRPC(ctx, endpoint, "getSignaturesForAddress", []any{wallet.String(), map[string]any{"limit": 25, "commitment": "confirmed"}}, &history); err != nil {
				l.Logf("%s: %v", wallet, err)
				continue
			}
			if err := sleepContext(ctx, 300*time.Millisecond); err != nil {
				l.Fatalf("%v", err)
			}
			if err := liveRPC(ctx, endpoint, "getSignaturesForAddress", []any{usdcATA.String(), map[string]any{"limit": 3, "commitment": "confirmed"}}, &usdcHistory); err != nil {
				l.Logf("%s: %v", wallet, err)
				continue
			}
			usdcQuiet := "never"
			if len(usdcHistory) > 0 {
				usdcQuiet = time.Since(time.Unix(usdcHistory[0].BlockTime, 0)).Round(time.Second).String()
			}
			taoRao := uint64(0)
			if tao != nil {
				taoRao = tao.Amount
			}
			l.Logf("from=%s wallet=%s sol=%.3f usdc=%s taoATA=%v tao=%.3f signatures=%d usdcLastMoved=%s", source, wallet, float64(account.Lamports)/lamportsPerSOL, microString(usdc.Amount), tao != nil, float64(taoRao)/raoPerTAO, len(history), usdcQuiet)
		}
	}
	if owners, err := liveLargestOwners(ctx, endpoint, client, bridge.TaoMint); err != nil {
		l.Logf("the largest holders of TAO: %v", err)
	} else {
		report("tao", owners)
	}
	if owners, err := liveRecentOwners(ctx, endpoint, 2_000*microPerUSDC, 200); err != nil {
		l.Logf("recent holders of USDC: %v", err)
	} else {
		report("usdc", owners)
	}
}

// TestLiveQuotes prices the deposit sizes on Jupiter against Coinbase's ask, and
// holds Client.Order's parse against each raw response.
func TestLiveQuotes(t *testing.T) {
	l, ctx := liveStart(t, "DEMAND_LIVE_SOLANA_RPC")
	market, jup, limits := NewCoinbaseMarket(), newLiveJupiter(), liveLimits()
	client := solana.NewClient(os.Getenv("DEMAND_LIVE_SOLANA_RPC"))
	for i, usd := range liveSizes() {
		amount := uint64(usd * microPerUSDC)
		ask, err := market.BestAsk(ctx)
		if err != nil {
			l.Fatalf("%v", err)
		}
		_, asks, err := liveBook(ctx, "TAO-USD")
		if err != nil {
			l.Fatalf("%v", err)
		}
		quote, err := jup.Order(ctx, jupiter.OrderRequest{InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: amount, SlippageBps: limits.MaxSlippageBps})
		if err != nil {
			l.Errorf("$%.0f: %v", usd, err)
			continue
		}
		fields := liveCheckParse(l, quote, amount)
		if i == 0 {
			l.Logf("raw %s", liveRawDump(quote))
		}
		floor := swapFloor(amount, ask, limits.MaxPriceGap)
		price := usd / (float64(quote.OutAmount) / raoPerTAO)
		vwap := liveVWAP(asks, usd)
		inventory := "-"
		if maker, err := solana.ParsePublicKey(fields.Maker); err == nil {
			if account, err := solana.AssociatedTokenAddress(maker, bridge.TaoMint); err == nil {
				if token, err := client.TokenAccount(ctx, account); err == nil && token != nil {
					inventory = fmt.Sprintf("%.3f", float64(token.Amount)/raoPerTAO)
				}
			}
		}
		l.Logf("usd=%.0f ask=%.2f vwap=%.3f tao=%.6f price=%.3f gap=%+.3f%% gapVWAP=%+.3f%% floor=%.6f pass=%v margin=%+.1fbps router=%s type=%s feeBps=%d slipBps=%d threshold=%.6f gasless=%v makerTAO=%s route=%s",
			usd, ask, vwap, float64(quote.OutAmount)/raoPerTAO, price, (price/ask-1)*100, (price/vwap-1)*100, float64(floor)/raoPerTAO, quote.OutAmount >= floor,
			(float64(quote.OutAmount)/float64(floor)-1)*10_000, quote.Router, fields.SwapType, quote.FeeBps, quote.SlippageBps, float64(quote.OtherAmountThreshold)/raoPerTAO, quote.Gasless, inventory, fields.route())
	}
}

// liveTakerOrder orders amount for the wallet as startSwap does, runs the real
// checkSwap on it, and reports what the simulation showed.
func liveTakerOrder(ctx context.Context, l liveLog, run *conversion, sol *liveSolana, jup *liveJupiter, market MarketAPI, usd float64) {
	limits, wallet, amount := run.cfg.Conversion, run.cfg.USDCAddress, uint64(usd*microPerUSDC)
	slippage := func(out, floor uint64) int { return swapSlippage(out, floor, limits.MaxSlippageBps) }
	ask, err := market.BestAsk(ctx)
	if err != nil {
		l.Fatalf("%v", err)
	}
	floor := swapFloor(amount, ask, limits.MaxPriceGap)
	quote, err := jup.Order(ctx, jupiter.OrderRequest{InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: amount, SlippageBps: limits.MaxSlippageBps})
	if err != nil {
		l.Errorf("$%.0f quote: %v", usd, err)
		return
	}
	liveCheckParse(l, quote, amount)
	order, err := jup.Order(ctx, jupiter.OrderRequest{InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: amount, Taker: wallet.String(), SlippageBps: slippage(quote.OutAmount, floor)})
	if err != nil {
		var refused *jupiter.OrderError
		if errors.As(err, &refused) {
			l.Logf("usd=%.0f order refused: %v", usd, err)
			return
		}
		l.Errorf("$%.0f order: %v", usd, err)
		return
	}
	fields := liveCheckParse(l, order, amount)
	l.Logf("raw %s", liveRawDump(order))
	price := usd / (float64(order.OutAmount) / raoPerTAO)
	l.Logf("usd=%.0f ask=%.2f quoteRouter=%s quoteOut=%.6f orderRouter=%s orderType=%s out=%.6f orderOverQuote=%+.1fbps price=%.3f gap=%+.3f%% floor=%.6f outOverFloor=%v threshold=%.6f thresholdOverFloor=%v slipSent=%d slipBack=%d feeBps=%d gasless=%v route=%s",
		usd, ask, quote.Router, float64(quote.OutAmount)/raoPerTAO, order.Router, fields.SwapType, float64(order.OutAmount)/raoPerTAO, (float64(order.OutAmount)/float64(quote.OutAmount)-1)*10_000, price, (price/ask-1)*100, float64(floor)/raoPerTAO, order.OutAmount >= floor,
		float64(order.OtherAmountThreshold)/raoPerTAO, order.OtherAmountThreshold >= floor, slippage(quote.OutAmount, floor), order.SlippageBps, order.FeeBps, order.Gasless, fields.route())
	guard := "PASS"
	if order.OutAmount < floor || order.OtherAmountThreshold < floor {
		guard = "REFUSED"
		if order.OutAmount >= floor {
			// startSwap sizes slippage from the quote, which another router may have
			// answered. Sized from this order's own output, the minimum clears the floor.
			again, err := jup.Order(ctx, jupiter.OrderRequest{InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: amount, Taker: wallet.String(), SlippageBps: slippage(order.OutAmount, floor)})
			switch {
			case err != nil:
				l.Logf("  reorder: %v", err)
			default:
				l.Logf("  reorder: slipSent=%d router=%s out=%.6f threshold=%.6f floor=%.6f wouldPass=%v", slippage(order.OutAmount, floor), again.Router, float64(again.OutAmount)/raoPerTAO, float64(again.OtherAmountThreshold)/raoPerTAO, float64(floor)/raoPerTAO, again.OutAmount >= floor && again.OtherAmountThreshold >= floor)
			}
		}
	}
	l.Logf("orderGuard=%s usd=%.0f router=%s gap=%+.3f%%", guard, usd, order.Router, (price/ask-1)*100)
	if len(order.Transaction) == 0 {
		l.Errorf("$%.0f: no transaction", usd)
		return
	}
	first := order.Transaction[0]
	tx, err := solana.ParseTransaction(order.Transaction)
	if err != nil {
		l.Errorf("$%.0f ParseTransaction refused the order (first byte 0x%02x): %v", usd, first, err)
		return
	}
	programs, complete := tx.ProgramIDs()
	opensATA := false
	for _, program := range programs {
		opensATA = opensATA || program == solana.AssociatedTokenProgram
	}
	height, err := sol.BlockHeight(ctx)
	if err != nil {
		l.Errorf("%v", err)
	}
	expiresIn := ""
	if at, err := strconv.ParseInt(fields.ExpireAt, 10, 64); err == nil {
		expiresIn = time.Until(time.Unix(at, 0)).Round(time.Second).String()
	}
	signed := 0
	for _, signature := range tx.Signatures {
		if signature != ([64]byte{}) {
			signed++
		}
	}
	l.Logf("tx: firstByte=0x%02x version=%d bytes=%d signers=%d slotsFilled=%d walletPays=%v payer=%s signatureFeePayer=%q prioFee=%d/%q rent=%d/%q sigFee=%d maker=%q lastValid=%d heightNow=%d left=%d expireAt=%q expiresIn=%s programs=%d complete=%v opensATA=%v",
		first, tx.Version, len(order.Transaction), tx.NumRequiredSignatures, signed, tx.FeePayer() == wallet, tx.FeePayer(), fields.SignatureFeePayer, fields.PrioritizationFeeLamports, fields.PrioritizationFeePayer, fields.RentFeeLamports, fields.RentFeePayer, fields.SignatureFeeLamports, fields.Maker,
		order.LastValidBlockHeight, height, int64(order.LastValidBlockHeight)-int64(height), fields.ExpireAt, expiresIn, len(programs), complete, opensATA)
	usdc, err := run.tokenBalance(ctx, run.usdc)
	if err != nil {
		l.Fatalf("%v", err)
	}
	lamports, err := sol.Balance(ctx, wallet)
	if err != nil {
		l.Fatalf("%v", err)
	}
	taoToken, err := sol.TokenAccount(ctx, run.tao)
	if err != nil {
		l.Fatalf("%v", err)
	}
	taoBefore, checkErr := run.checkSwap(ctx, tx, order, usdc, lamports, amount, floor)
	verdict := "PASS"
	if checkErr != nil {
		verdict = "REFUSED: " + checkErr.Error()
	}
	sim := sol.last
	if sim == nil {
		l.Errorf("$%.0f checkSwap refused before simulating: %s", usd, verdict)
		return
	}
	if len(sim.Err) > 0 {
		l.Errorf("$%.0f simulation failed: %s %s", usd, sim.Err, lastLogs(sim.Logs, 8))
		return
	}
	if len(sim.Accounts) != 3 {
		l.Errorf("$%.0f: %d simulated accounts", usd, len(sim.Accounts))
		return
	}
	usdcAfter, taoAfter, walletAfter := sim.Accounts[0], sim.Accounts[1], sim.Accounts[2]
	var usdcMoved, taoMoved int64
	if usdcAfter.Token != nil {
		usdcMoved = int64(usdc) - int64(usdcAfter.Token.Amount)
	}
	taoAfterAmount, taoDecimals := uint64(0), uint8(0)
	if taoAfter.Token != nil {
		taoAfterAmount, taoDecimals = taoAfter.Token.Amount, taoAfter.Token.Decimals
		taoMoved = int64(taoAfterAmount) - int64(taoBefore)
	}
	spent := int64(lamports) - int64(walletAfter.Lamports)
	l.Logf("sim: taoATABefore=%v units=%d usdcMoved=%d exact=%v taoExists=%v taoParsed=%v taoOwnerOK=%v taoDecimals=%d taoATALamports=%d taoMoved=%d taoOverFloor=%v taoOverQuote=%+.2fbps solSpent=%d cap=%d withinCap=%v checkSwap=%s",
		taoToken != nil, sim.UnitsConsumed, usdcMoved, usdcMoved == int64(amount), taoAfter.Exists, taoAfter.Token != nil, taoAfter.Token != nil && taoAfter.Token.Owner == wallet, taoDecimals, taoAfter.Lamports, taoMoved, taoMoved >= int64(floor),
		(float64(taoMoved)/float64(order.OutAmount)-1)*10_000, spent, maxSwapLamports, spent <= maxSwapLamports, verdict)
	if checkErr != nil {
		l.Errorf("$%.0f %s", usd, verdict)
	}
}

// TestLiveTakerOrder builds real orders for the holder and runs each through the
// gate before signing. A holder without a TAO account is the funder's new wallet.
func TestLiveTakerOrder(t *testing.T) {
	l, ctx := liveStart(t, "DEMAND_LIVE_SOLANA_RPC", "DEMAND_LIVE_USDC_HOLDER")
	wallet := solana.MustPublicKey(os.Getenv("DEMAND_LIVE_USDC_HOLDER"))
	sol, jup := &liveSolana{Client: solana.NewClient(os.Getenv("DEMAND_LIVE_SOLANA_RPC"))}, newLiveJupiter()
	f := liveFunder(t, wallet, sol, jup, &liveEVM{})
	run := &conversion{Funder: f, state: &State{}, epoch: 1}
	account, err := sol.Account(ctx, wallet)
	if err != nil || account == nil || account.Owner != solana.SystemProgram {
		l.Fatalf("the holder must be a system-owned wallet: %+v %v", account, err)
	}
	usdc, err := run.tokenBalance(ctx, run.usdc)
	if err != nil {
		l.Fatalf("%v", err)
	}
	tao, err := sol.TokenAccount(ctx, run.tao)
	if err != nil {
		l.Fatalf("%v", err)
	}
	l.Logf("holder=%s sol=%.3f usdc=%s taoATA=%v", wallet, float64(account.Lamports)/lamportsPerSOL, microString(usdc), tao != nil)
	for _, usd := range liveSizes() {
		if usdc < uint64(usd*microPerUSDC) {
			l.Logf("usd=%.0f skipped: the holder has %s USDC", usd, microString(usdc))
			continue
		}
		liveTakerOrder(ctx, l, run, sol, jup, f.deps.Market, usd)
	}
}

// TestLiveStartSwapStopsAtSigning runs the real startSwap for the holder: sizing,
// every guard, the order and the simulation. With no key, Sign is where it must
// stop; were it ever to go on, the state, /execute and broadcast all refuse.
func TestLiveStartSwapStopsAtSigning(t *testing.T) {
	l, ctx := liveStart(t, "DEMAND_LIVE_SOLANA_RPC", "DEMAND_LIVE_USDC_HOLDER")
	wallet := solana.MustPublicKey(os.Getenv("DEMAND_LIVE_USDC_HOLDER"))
	sol, jup := &liveSolana{Client: solana.NewClient(os.Getenv("DEMAND_LIVE_SOLANA_RPC"))}, newLiveJupiter()
	f := liveFunder(t, wallet, sol, jup, &liveEVM{})
	ask, err := f.deps.Market.BestAsk(ctx)
	if err != nil {
		l.Fatalf("%v", err)
	}
	for _, usd := range liveSizes() {
		run := &conversion{Funder: f, state: &State{}, epoch: 1}
		needRao := uint64(usd / ask / swapPriceMargin * raoPerTAO)
		jup.orders, sol.last = nil, nil
		err := run.startSwap(ctx, needRao)
		switch {
		case err == nil:
			l.Fatalf("usd=%.0f startSwap finished without a key", usd)
		case strings.Contains(err.Error(), "not an ed25519 private key"):
			last := jup.orders[len(jup.orders)-1]
			l.Logf("usd=%.0f need=%.6f TAO: every guard passed, stopped at Sign. orders=%d amount=%s router=%s out=%.6f threshold=%.6f", usd, float64(needRao)/raoPerTAO, len(jup.orders), microString(last.InAmount), last.Router, float64(last.OutAmount)/raoPerTAO, float64(last.OtherAmountThreshold)/raoPerTAO)
		default:
			l.Errorf("usd=%.0f need=%.6f TAO: startSwap refused: %v", usd, float64(needRao)/raoPerTAO, err)
		}
	}
}

func liveFloat(value *big.Int) float64 {
	f, _ := new(big.Float).SetInt(value).Float64()
	return f
}

// liveExactTao is the least TAO the pool takes for alpha rao of alpha.
func liveExactTao(ctx context.Context, evm *liveEVM, alpha *big.Int, low, high uint64) (uint64, error) {
	for high-low > 1_000 {
		mid := low + (high-low)/2
		out, err := evm.simSwap(ctx, mid)
		if err != nil {
			return 0, err
		}
		if out.Cmp(alpha) >= 0 {
			high = mid
		} else {
			low = mid
		}
	}
	return high, nil
}

// TestLiveAlphaPool reads the pool the stake fills against: its impact at the
// deposit sizes, what the funder's sizing makes of it, and how far the price moves
// while a conversion is in flight.
func TestLiveAlphaPool(t *testing.T) {
	l, ctx := liveStart(t, "DEMAND_LIVE_EVM_RPC")
	evm, jup := &liveEVM{endpoint: os.Getenv("DEMAND_LIVE_EVM_RPC")}, newLiveJupiter()
	f := &Funder{cfg: &Config{Conversion: liveLimits()}, netuid: liveNetuid, deps: Deps{EVM: evm}}
	head, err := evm.head(ctx)
	if err != nil {
		l.Fatalf("%v", err)
	}
	price, err := f.alphaPrice(ctx)
	if err != nil {
		l.Fatalf("%v", err)
	}
	taoPool, err := evm.word(ctx, "getTaoInPool", packTaoInPool(liveNetuid))
	if err != nil {
		l.Fatalf("%v", err)
	}
	alphaPool, err := evm.word(ctx, "getAlphaInPool", packCall(selector("getAlphaInPool(uint16)"), wordUint(uint64(liveNetuid))))
	if err != nil {
		l.Fatalf("%v", err)
	}
	spot := liveFloat(price) / 1e18
	l.Logf("block=%d price=%.9f TAO/alpha (raw %s) taoInPool=%.4f alphaInPool=%.4f T/A=%.9f price/(T/A)=%.6f", head, spot, price, liveFloat(taoPool)/raoPerTAO, liveFloat(alphaPool)/raoPerTAO, liveFloat(taoPool)/liveFloat(alphaPool), spot/(liveFloat(taoPool)/liveFloat(alphaPool)))
	// The smallest swap shows the pool's fee alone.
	const dustRao = 10_000_000
	dust, err := evm.simSwap(ctx, dustRao)
	if err != nil {
		l.Fatalf("%v", err)
	}
	l.Logf("dust: %d rao buys %s alpha rao; pays %.4f%% under spot", uint64(dustRao), dust, (1-liveFloat(dust)*spot/dustRao)*100)
	market := NewCoinbaseMarket()
	ask, err := market.BestAsk(ctx)
	if err != nil {
		l.Fatalf("%v", err)
	}
	limits := f.cfg.Conversion
	for _, usd := range liveSizes() {
		quote, err := jup.Order(ctx, jupiter.OrderRequest{InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: uint64(usd * microPerUSDC), SlippageBps: limits.MaxSlippageBps})
		if err != nil {
			l.Errorf("$%.0f: %v", usd, err)
			continue
		}
		x := quote.OutAmount
		out, err := evm.simSwap(ctx, x)
		if err != nil {
			l.Fatalf("%v", err)
		}
		T, A := liveFloat(taoPool), liveFloat(alphaPool)
		atSpot := float64(x) / raoPerTAO / spot * raoPerTAO
		impact := 1 - liveFloat(out)/atSpot
		constantProduct := A * float64(x) / (T + float64(x))
		// The marginal price after the trade, from two nearby simulations.
		step := max(x/1_000, 1_000_000)
		next, err := evm.simSwap(ctx, x+step)
		if err != nil {
			l.Fatalf("%v", err)
		}
		marginal := float64(step) / (liveFloat(next) - liveFloat(out))
		// What the funder would pay to buy that alpha back.
		linear := new(big.Int).Quo(new(big.Int).Mul(out, price), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)).Uint64()
		before := evm.calls
		sized, err := f.taoForAlpha(ctx, out)
		if err != nil {
			l.Fatalf("%v", err)
		}
		sims := evm.calls - before - 1
		exact, err := liveExactTao(ctx, evm, out, linear, sized)
		if err != nil {
			l.Fatalf("%v", err)
		}
		buffered := sized + sized/10_000*limits.AlphaBufferBps + 1
		outBuffered, err := evm.simSwap(ctx, buffered)
		if err != nil {
			l.Fatalf("%v", err)
		}
		limit, err := f.stakeLimit(ctx, buffered)
		if err != nil {
			l.Fatalf("%v", err)
		}
		l.Logf("usd=%.0f tao=%.6f alphaOut=%.4f atSpot=%.4f impact=%.3f%% cpOut=%.4f simOverCP=%+.3f%% poolShare=%.3f%% postPrice=%.9f postOverSpot=%+.3f%% | linear=%.6f exact=%.6f linearShort=%.3f%% sized=%.6f sims=%d overshoot=%.3f%% | buffered=%.6f alphaBuffered=%.4f buysExtra=%.3f%% stakeLimit=%.9f limitOverSpot=%+.3f%% limitOverPost=%+.3f%%",
			usd, float64(x)/raoPerTAO, liveFloat(out)/raoPerTAO, atSpot/raoPerTAO, impact*100, constantProduct/raoPerTAO, (liveFloat(out)/constantProduct-1)*100, float64(x)/T*100,
			marginal, (marginal/spot-1)*100, float64(linear)/raoPerTAO, float64(exact)/raoPerTAO, (1-float64(linear)/float64(exact))*100, float64(sized)/raoPerTAO, sims, (float64(sized)/float64(exact)-1)*100,
			float64(buffered)/raoPerTAO, liveFloat(outBuffered)/raoPerTAO, (liveFloat(outBuffered)/liveFloat(out)-1)*100, float64(limit)/raoPerTAO, (float64(limit)/raoPerTAO/spot-1)*100, (float64(limit)/raoPerTAO/marginal-1)*100)
	}
	l.Logf("usdPerAlphaAtAsk=%.4f ask=%.2f", spot*ask, ask)
}

// TestLiveAlphaHistory reads the alpha price back through the archive, to size the
// buffer: a price that rises more than the buffer between the swap and the stake
// needs a second conversion. DEMAND_LIVE_HISTORY_SAMPLES replaces how far back.
func TestLiveAlphaHistory(t *testing.T) {
	l, ctx := liveStart(t, "DEMAND_LIVE_EVM_RPC")
	evm := &liveEVM{endpoint: os.Getenv("DEMAND_LIVE_EVM_RPC")}
	head, err := evm.head(ctx)
	if err != nil {
		l.Fatalf("%v", err)
	}
	samples := liveHistorySamples
	if value, err := strconv.Atoi(os.Getenv("DEMAND_LIVE_HISTORY_SAMPLES")); err == nil && value > 0 {
		samples = value
	}
	liveAlphaHistory(ctx, l, evm, head, samples)
}

// liveAlphaHistory reports how far the alpha price rises over the windows a
// conversion can take, from samples spaced ten minutes apart.
func liveAlphaHistory(ctx context.Context, l liveLog, evm *liveEVM, head uint64, samples int) {
	l.T.Helper()
	prices := make([]float64, 0, samples)
	for i := samples - 1; i >= 0; i-- {
		block := head - uint64(i)*liveHistorySpacing
		raw, err := evm.callAt(ctx, AlphaPrecompile, packAlphaPrice(liveNetuid), fmt.Sprintf("0x%x", block))
		if err != nil {
			l.Logf("history: block %d: %v", block, err)
			continue
		}
		price, err := unpackWord("getAlphaPrice", raw)
		if err != nil {
			l.Logf("history: block %d: %v", block, err)
			continue
		}
		if price.Sign() > 0 {
			prices = append(prices, liveFloat(price)/1e18)
		}
	}
	if len(prices) < 8 {
		l.Logf("history: only %d samples", len(prices))
		return
	}
	for _, windows := range []int{1, 3, 6} {
		var moves []float64
		for i := windows; i < len(prices); i++ {
			moves = append(moves, prices[i]/prices[i-windows]-1)
		}
		sort.Float64s(moves)
		over := 0
		for _, move := range moves {
			if move >= float64(defaultAlphaBufferBps)/10_000 {
				over++
			}
		}
		percentile := func(p float64) float64 { return moves[int(p*float64(len(moves)-1))] * 100 }
		l.Logf("history: %d samples, window=%dmin p05=%+.3f%% p50=%+.3f%% p95=%+.3f%% p99=%+.3f%% p99.9=%+.3f%% max=%+.3f%% min=%+.3f%% windowsOverBuffer=%d/%d", len(prices), windows*liveHistorySpacing*12/60, percentile(0.05), percentile(0.5), percentile(0.95), percentile(0.99), percentile(0.999), moves[len(moves)-1]*100, moves[0]*100, over, len(moves))
	}
	low, high := math.Inf(1), math.Inf(-1)
	for _, price := range prices {
		low, high = min(low, price), max(high, price)
	}
	l.Logf("history: %.1f days, price low=%.9f high=%.9f first=%.9f last=%.9f", float64(len(prices)*liveHistorySpacing*12)/86_400, low, high, prices[0], prices[len(prices)-1])
}

// liveBridgeSend simulates a bridge send for a TAO holder and returns its cost.
func liveBridgeSend(ctx context.Context, sol *liveSolana, holder solana.PublicKey) (uint64, error) {
	source, err := solana.AssociatedTokenAddress(holder, bridge.TaoMint)
	if err != nil {
		return 0, err
	}
	token, err := sol.TokenAccount(ctx, source)
	if err != nil || token == nil {
		return 0, fmt.Errorf("the holder's TAO account: %v", err)
	}
	lamports, err := sol.Balance(ctx, holder)
	if err != nil {
		return 0, err
	}
	tableAccount, err := sol.Account(ctx, bridge.LookupTable)
	if err != nil || tableAccount == nil {
		return 0, fmt.Errorf("the lookup table: %v", err)
	}
	table, err := solana.ParseLookupTable(bridge.LookupTable, tableAccount.Data)
	if err != nil {
		return 0, err
	}
	blockhash, err := sol.LatestBlockhash(ctx)
	if err != nil {
		return 0, err
	}
	send := bridge.Send{Sender: holder, To: bridge.WTAO, AmountRao: min(token.Amount/bridge.UnitRao*bridge.UnitRao, 1_000_000_000), MaxFeeLamports: liveLimits().MaxBridgeFeeLamports}
	tx, err := send.Transaction(blockhash.Hash, table)
	if err != nil {
		return 0, err
	}
	sim, err := sol.Simulate(ctx, tx.Serialize(), []solana.PublicKey{holder})
	if err != nil {
		return 0, err
	}
	if len(sim.Err) > 0 {
		return 0, fmt.Errorf("the send does not simulate: %s %s", sim.Err, lastLogs(sim.Logs, 3))
	}
	return lamports - sim.Accounts[0].Lamports, nil
}

// TestLiveEndToEnd prices the whole route for a deposit-sized conversion.
func TestLiveEndToEnd(t *testing.T) {
	l, ctx := liveStart(t, "DEMAND_LIVE_SOLANA_RPC", "DEMAND_LIVE_EVM_RPC", "DEMAND_LIVE_USDC_HOLDER")
	wallet := solana.MustPublicKey(os.Getenv("DEMAND_LIVE_USDC_HOLDER"))
	sol, jup := &liveSolana{Client: solana.NewClient(os.Getenv("DEMAND_LIVE_SOLANA_RPC"))}, newLiveJupiter()
	evm := &liveEVM{endpoint: os.Getenv("DEMAND_LIVE_EVM_RPC")}
	f := liveFunder(t, wallet, sol, jup, evm)
	run := &conversion{Funder: f, state: &State{}, epoch: 1}
	limits := f.cfg.Conversion

	ask, err := f.deps.Market.BestAsk(ctx)
	if err != nil {
		l.Fatalf("%v", err)
	}
	bids, _, err := liveBook(ctx, "TAO-USD")
	if err != nil {
		l.Fatalf("%v", err)
	}
	mid := (ask + bids[0].price) / 2
	solBids, solAsks, err := liveBook(ctx, "SOL-USD")
	if err != nil {
		l.Fatalf("%v", err)
	}
	solUSD := (solBids[0].price + solAsks[0].price) / 2

	bridgeLamports, bridgeSource := uint64(liveBridgeLamports), "assumed"
	if holder := os.Getenv("DEMAND_LIVE_TAO_HOLDER"); holder != "" {
		measured, err := liveBridgeSend(ctx, sol, solana.MustPublicKey(holder))
		if err != nil {
			l.Errorf("the bridge send: %v", err)
		} else {
			bridgeLamports, bridgeSource = measured, "simulated"
		}
	}
	gasPrice, err := evm.gasPrice(ctx)
	if err != nil {
		l.Fatalf("%v", err)
	}

	for _, usd := range liveSizes() {
		amount := uint64(usd * microPerUSDC)
		floor := swapFloor(amount, ask, limits.MaxPriceGap)
		quote, err := jup.Order(ctx, jupiter.OrderRequest{InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: amount, SlippageBps: limits.MaxSlippageBps})
		if err != nil {
			l.Errorf("$%.0f: %v", usd, err)
			continue
		}
		order, err := jup.Order(ctx, jupiter.OrderRequest{InputMint: USDCMint.String(), OutputMint: bridge.TaoMint.String(), Amount: amount, Taker: wallet.String(), SlippageBps: swapSlippage(quote.OutAmount, floor, limits.MaxSlippageBps)})
		if err != nil {
			l.Errorf("$%.0f: %v", usd, err)
			continue
		}
		tx, err := solana.ParseTransaction(order.Transaction)
		if err != nil {
			l.Errorf("$%.0f: %v", usd, err)
			continue
		}
		usdc, err := run.tokenBalance(ctx, run.usdc)
		if err != nil {
			l.Fatalf("%v", err)
		}
		lamports, err := sol.Balance(ctx, wallet)
		if err != nil {
			l.Fatalf("%v", err)
		}
		if _, err := run.checkSwap(ctx, tx, order, usdc, lamports, amount, floor); err != nil {
			l.Errorf("$%.0f checkSwap: %v", usd, err)
		}
		swapLamports := uint64(0)
		if sol.last != nil && len(sol.last.Err) == 0 && len(sol.last.Accounts) == 3 && sol.last.Accounts[2].Lamports < lamports {
			swapLamports = lamports - sol.last.Accounts[2].Lamports
		}
		price, err := f.alphaPrice(ctx)
		if err != nil {
			l.Fatalf("%v", err)
		}
		alphaOut, err := evm.simSwap(ctx, order.OutAmount)
		if err != nil {
			l.Fatalf("%v", err)
		}
		spot := liveFloat(price) / 1e18
		tao := float64(order.OutAmount) / raoPerTAO
		alpha := liveFloat(alphaOut) / raoPerTAO
		swapUSD := float64(swapLamports) / lamportsPerSOL * solUSD
		bridgeUSD := float64(bridgeLamports) / lamportsPerSOL * solUSD
		gasUSD := (liveUnwrapGas + liveStakeGas) * liveFloat(gasPrice) / 1e18 * ask
		// Everything paid over the alpha's worth at the pool's price and Coinbase's ask.
		spotUSD := spot * ask
		jupiterUSD := usd - tao*ask
		poolUSD := (tao - alpha*spot) * ask
		total := usd + swapUSD + bridgeUSD + gasUSD
		effective := total / alpha
		l.Logf("usd=%.0f router=%s gasless=%v ask=%.2f mid=%.2f solUSD=%.2f tao=%.6f alpha=%.4f spot=%.9f spotUSD=%.4f effectiveUSD=%.4f overAsk=%+.3f%% overMid=%+.3f%%",
			usd, order.Router, order.Gasless, ask, mid, solUSD, tao, alpha, spot, spotUSD, effective, (effective/spotUSD-1)*100, (effective/(spot*mid)-1)*100)
		l.Logf("  costs jupiter=$%.3f(%.3f%%) pool=$%.3f(%.3f%%) swapSOL=%d($%.3f) bridgeSOL=%d($%.3f %s) gas=$%.3f total=$%.3f over $%.2f of alpha at spot",
			jupiterUSD, jupiterUSD/usd*100, poolUSD, poolUSD/usd*100, swapLamports, swapUSD, bridgeLamports, bridgeUSD, bridgeSource, gasUSD, jupiterUSD+poolUSD+swapUSD+bridgeUSD+gasUSD, alpha*spotUSD)
	}
}
