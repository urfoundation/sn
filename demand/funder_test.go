package demand

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/demand/bridge"
	"github.com/urfoundation/sn/demand/jupiter"
	"github.com/urfoundation/sn/demand/solana"
)

const testNetuid = 25

// testWallets writes a demand.yml with fresh keys.
func testConfigYAML(t *testing.T, conversion string) []byte {
	t.Helper()
	evmKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub, secret, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Appendf(nil, `alpha_wallet:
  address: "%s"
  private_key: "%s"
  hotkey: "0x%s"
usdc_wallet:
  address: "%s"
  private_key: "%s"
conversion:
%s
`, crypto.PubkeyToAddress(evmKey.PublicKey).Hex(), hex.EncodeToString(crypto.FromECDSA(evmKey)), strings.Repeat("c0", 32),
		solana.Base58Encode(pub), solana.Base58Encode(secret), conversion)
}

// world fakes Bittensor, Solana, Jupiter, Coinbase, LayerZero Scan and the store.
type world struct {
	t   *testing.T
	cfg *Config

	clock time.Time

	// Bittensor
	alphaRao, nativeWei, wtaoWei *big.Int
	price                        *big.Int
	poolRao                      uint64
	peerStore                    [32]byte
	operations                   []string
	sendErr                      error
	hotkeyErr                    error

	// Solana
	usdcAccount, taoAccount solana.PublicKey
	usdc, tao, lamports     uint64
	height                  uint64
	statuses                map[string]*solana.SignatureStatus
	peerData                []byte
	table                   []byte
	simulateErr             json.RawMessage
	simulateTaoDelta        int64 // added to what a swap's simulation pays

	// Jupiter and Coinbase, in USD per TAO
	jupiterPrice float64
	ask          float64
	orders       map[string]*fakeOrder
	swaps        []*fakeOrder
	executeLost  bool // the swap lands but its reply is lost
	executeDrop  bool // the swap never lands and its reply is lost
	executeCode  int

	// Bridge
	sends        []uint64
	sendLost     bool // the send lands but its reply is lost
	sendDrop     int  // this many broadcasts never reach the chain
	deliverAfter int  // sleeps before a landed send's wTAO arrives
	pending      []*fakeDelivery

	// Store
	stateJSON []byte
	version   uint64
	saves     int
	staleNext bool
	events    []Event
}

type fakeOrder struct {
	amount, out uint64
	raw         []byte
}

type fakeDelivery struct {
	wei   *big.Int
	after int
}

func newWorld(t *testing.T, conversion string) *world {
	t.Helper()
	cfg, err := ParseConfig(testConfigYAML(t, conversion))
	if err != nil {
		t.Fatal(err)
	}
	// These worlds' sums were worked out at a reserve of 0.1 TAO. Tests of the default
	// parse it themselves, and any other reserve names itself in conversion.
	if !strings.Contains(conversion, "gas_reserve_tao") {
		cfg.Conversion.GasReserveRao = raoPerTAO / 10
	}
	usdcAccount, err := solana.AssociatedTokenAddress(cfg.USDCAddress, USDCMint)
	if err != nil {
		t.Fatal(err)
	}
	taoAccount, err := solana.AssociatedTokenAddress(cfg.USDCAddress, bridge.TaoMint)
	if err != nil {
		t.Fatal(err)
	}
	peer := make([]byte, 64)
	copy(peer[8+12:], bridge.WTAO[:])
	encodedTable, err := os.ReadFile("bridge/testdata/lookup_table.b64")
	if err != nil {
		t.Fatal(err)
	}
	table, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encodedTable)))
	if err != nil {
		t.Fatal(err)
	}
	return &world{
		t: t, cfg: cfg, clock: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC),
		alphaRao: new(big.Int), nativeWei: taoWei(0.2), wtaoWei: new(big.Int),
		price: big.NewInt(4_000_000_000_000_000), poolRao: 300_000 * raoPerTAO, peerStore: [32]byte(bridge.OFTStore),
		usdcAccount: usdcAccount, taoAccount: taoAccount,
		lamports: 250_000_000, height: 1_000, statuses: map[string]*solana.SignatureStatus{},
		peerData: peer, table: table,
		jupiterPrice: 240, ask: 240, orders: map[string]*fakeOrder{},
		deliverAfter: 2,
	}
}

func taoWei(tao float64) *big.Int {
	return new(big.Int).Mul(big.NewInt(int64(tao*raoPerTAO)), WeiPerRao)
}

func alpha(a float64) *big.Int { return big.NewInt(int64(a * raoPerTAO)) }

func (w *world) funder(t *testing.T) *Funder {
	t.Helper()
	f, err := NewFunder(w.cfg, testNetuid, Deps{
		Store: w, EVM: w, Sender: w, Solana: worldSolana{w}, Jupiter: w, Market: w, Scan: w,
		Now:   func() time.Time { return w.clock },
		Sleep: w.sleep,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (w *world) sleep(_ context.Context, d time.Duration) error {
	w.clock = w.clock.Add(d)
	w.height += 40
	remaining := w.pending[:0]
	for _, delivery := range w.pending {
		if delivery.after--; delivery.after <= 0 {
			w.wtaoWei = new(big.Int).Add(w.wtaoWei, delivery.wei)
		} else {
			remaining = append(remaining, delivery)
		}
	}
	w.pending = remaining
	return nil
}

// EVM

func (w *world) Call(_ context.Context, to common.Address, data []byte) ([]byte, error) {
	selectorIs := func(s []byte) bool { return bytes.HasPrefix(data, s) }
	switch {
	case to == StakingPrecompile && selectorIs(getStakeSelector):
		return word(w.alphaRao), nil
	case to == StakingPrecompile && selectorIs(addStakeLimitSelector):
		return nil, w.hotkeyErr
	case to == bridge.WTAO && selectorIs(balanceOfSelector):
		return word(w.wtaoWei), nil
	case to == bridge.WTAO && selectorIs(peersSelector):
		return w.peerStore[:], nil
	case to == AlphaPrecompile && selectorIs(alphaPriceSelector):
		return word(w.price), nil
	case to == AlphaPrecompile && selectorIs(taoInPoolSelector):
		return wordUint(w.poolRao), nil
	case to == AlphaPrecompile && selectorIs(simSwapSelector):
		tao := new(big.Int).SetBytes(data[36:68])
		return word(w.alphaFor(tao)), nil
	}
	return nil, fmt.Errorf("unexpected call to %s", to)
}

func (w *world) alphaFor(tao *big.Int) *big.Int {
	return new(big.Int).Quo(new(big.Int).Mul(tao, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)), w.price)
}

func (w *world) Balance(_ context.Context, account common.Address) (*big.Int, error) {
	if account != w.cfg.AlphaAddress {
		return nil, errors.New("unexpected account")
	}
	return new(big.Int).Set(w.nativeWei), nil
}

func (w *world) PendingNonce(context.Context, common.Address) (uint64, error) {
	return uint64(len(w.operations)), nil
}

func (w *world) SendPrepared(ctx context.Context, prepare Prepare) (string, error) {
	if w.sendErr != nil {
		return "", w.sendErr
	}
	operation, to, data, err := prepare(ctx)
	if err != nil || operation == "" {
		return "", err
	}
	if !AllowedAlphaWalletCall(to, data, bridge.WTAO, w.cfg.Hotkey, testNetuid) {
		w.t.Fatalf("the conversion sent a call a signer must refuse: %s", operation)
	}
	w.operations = append(w.operations, operation)
	switch {
	case bytes.HasPrefix(data, withdrawSelector):
		amount := new(big.Int).SetBytes(data[4:36])
		w.wtaoWei = new(big.Int).Sub(w.wtaoWei, amount)
		w.nativeWei = new(big.Int).Add(w.nativeWei, amount)
	case bytes.HasPrefix(data, addStakeLimitSelector):
		amount := new(big.Int).SetBytes(data[36:68])
		w.nativeWei = new(big.Int).Sub(w.nativeWei, new(big.Int).Mul(amount, WeiPerRao))
		w.alphaRao = new(big.Int).Add(w.alphaRao, w.alphaFor(amount))
	}
	return "0xhash" + operation, nil
}

// Solana

// worldSolana is the world as Solana sees it, where Balance means lamports.
type worldSolana struct{ *world }

func (s worldSolana) Balance(_ context.Context, account solana.PublicKey) (uint64, error) {
	if account != s.cfg.USDCAddress {
		return 0, errors.New("unexpected account")
	}
	return s.lamports, nil
}

func (w *world) BlockHeight(context.Context) (uint64, error) { return w.height, nil }

func (w *world) TokenAccount(_ context.Context, address solana.PublicKey) (*solana.TokenAccount, error) {
	switch address {
	case w.usdcAccount:
		return w.token(address, USDCMint, w.usdc), nil
	case w.taoAccount:
		return w.token(address, bridge.TaoMint, w.tao), nil
	}
	return nil, errors.New("unexpected token account")
}

func (w *world) token(address, mint solana.PublicKey, amount uint64) *solana.TokenAccount {
	return &solana.TokenAccount{Address: address, Mint: mint, Owner: w.cfg.USDCAddress, Amount: amount}
}

func (w *world) Account(_ context.Context, address solana.PublicKey) (*solana.Account, error) {
	switch address {
	case bridge.PeerConfig:
		return &solana.Account{Owner: bridge.OFTProgram, Data: w.peerData}, nil
	case bridge.LookupTable:
		return &solana.Account{Data: w.table}, nil
	}
	return nil, errors.New("unexpected account")
}

func sendAmount(t *testing.T, message []byte) (uint64, bool) {
	t.Helper()
	discriminator := sha256.Sum256([]byte("global:send"))
	at := bytes.Index(message, discriminator[:8])
	if at < 0 {
		return 0, false
	}
	return binary.LittleEndian.Uint64(message[at+44:]), true
}

func (w *world) Simulate(_ context.Context, raw []byte, accounts []solana.PublicKey) (*solana.Simulation, error) {
	tx, err := solana.ParseTransaction(raw)
	if err != nil {
		return nil, err
	}
	// Nothing landable exists before the gate has passed.
	if tx.SignedBy(w.cfg.USDCAddress) {
		return nil, errors.New("simulated a transaction the wallet had already signed")
	}
	sim := &solana.Simulation{Err: w.simulateErr}
	usdc, tao, lamports := w.usdc, w.tao, w.lamports
	if amount, ok := sendAmount(w.t, tx.Message); ok {
		tao -= amount
		lamports -= 23_000_000
	} else {
		order := w.orders[string(tx.Message)]
		if order == nil {
			return nil, errors.New("simulated an unknown transaction")
		}
		usdc -= order.amount
		tao = uint64(int64(tao+order.out) + w.simulateTaoDelta)
		lamports -= 1_000_000
	}
	for _, account := range accounts {
		switch account {
		case w.usdcAccount:
			sim.Accounts = append(sim.Accounts, solana.SimulatedAccount{Exists: true, Token: w.token(account, USDCMint, usdc)})
		case w.taoAccount:
			sim.Accounts = append(sim.Accounts, solana.SimulatedAccount{Exists: true, Token: w.token(account, bridge.TaoMint, tao)})
		case w.cfg.USDCAddress:
			sim.Accounts = append(sim.Accounts, solana.SimulatedAccount{Exists: true, Lamports: lamports})
		default:
			return nil, errors.New("simulated an unexpected account")
		}
	}
	return sim, nil
}

func (w *world) SignatureStatuses(_ context.Context, signatures []string) ([]*solana.SignatureStatus, error) {
	statuses := make([]*solana.SignatureStatus, len(signatures))
	for i, signature := range signatures {
		statuses[i] = w.statuses[signature]
	}
	return statuses, nil
}

func (w *world) LatestBlockhash(context.Context) (solana.Blockhash, error) {
	return solana.Blockhash{Hash: [32]byte{1}, LastValidBlockHeight: w.height + 150}, nil
}

func (w *world) SendTransaction(_ context.Context, raw []byte) (string, error) {
	tx, err := solana.ParseTransaction(raw)
	if err != nil {
		return "", err
	}
	if !tx.SignedBy(w.cfg.USDCAddress) {
		w.t.Fatal("the bridge send is not signed by the wallet")
	}
	if w.sendDrop > 0 {
		w.sendDrop--
		return "", errors.New("dropped")
	}
	signature := tx.ID()
	if w.statuses[signature] != nil {
		return signature, nil // the same bytes cannot land twice
	}
	amount, ok := sendAmount(w.t, tx.Message)
	if !ok {
		w.t.Fatal("broadcast something other than a bridge send")
	}
	w.sends = append(w.sends, amount)
	w.tao -= amount
	w.lamports -= 23_000_000
	w.statuses[signature] = &solana.SignatureStatus{ConfirmationStatus: "confirmed"}
	w.pending = append(w.pending, &fakeDelivery{wei: new(big.Int).Mul(new(big.Int).SetUint64(amount), WeiPerRao), after: w.deliverAfter})
	if w.sendLost {
		return "", errors.New("the reply was lost")
	}
	return signature, nil
}

// Jupiter, Coinbase and Scan

func (w *world) Order(_ context.Context, req jupiter.OrderRequest) (*jupiter.Order, error) {
	if req.InputMint != USDCMint.String() || req.OutputMint != bridge.TaoMint.String() {
		w.t.Fatal("an order for the wrong pair")
	}
	out := uint64(float64(req.Amount) / microPerUSDC / w.jupiterPrice * raoPerTAO)
	order := &jupiter.Order{RequestID: "request", InAmount: req.Amount, OutAmount: out, OtherAmountThreshold: out - out/1_000, Router: "fake", LastValidBlockHeight: w.height + 150}
	if req.Taker == "" {
		return order, nil
	}
	if req.Taker != w.cfg.USDCAddress.String() || req.Receiver != "" {
		w.t.Fatal("an order that does not pay the wallet itself")
	}
	tx, err := solana.CompileV0(w.cfg.USDCAddress, []solana.Instruction{{Program: solana.MustPublicKey("JUP6LkbZbjS1jKKwapdHNy74zcZ3tLUZoi5QNyVTaV4"), Data: binary.LittleEndian.AppendUint64(nil, uint64(len(w.orders)))}}, [32]byte{2}, nil)
	if err != nil {
		return nil, err
	}
	order.Transaction = tx.Serialize()
	w.orders[string(tx.Message)] = &fakeOrder{amount: req.Amount, out: out, raw: order.Transaction}
	return order, nil
}

func (w *world) Execute(_ context.Context, signed []byte, _ string) (*jupiter.ExecuteResult, error) {
	tx, err := solana.ParseTransaction(signed)
	if err != nil {
		return nil, err
	}
	if !tx.SignedBy(w.cfg.USDCAddress) {
		w.t.Fatal("the swap is not signed by the wallet")
	}
	if w.executeCode != 0 {
		return &jupiter.ExecuteResult{Status: "Failed", Code: w.executeCode}, nil
	}
	if w.executeDrop {
		return nil, errors.New("the swap never reached the chain")
	}
	order := w.orders[string(tx.Message)]
	if order == nil {
		w.t.Fatal("executed an unknown transaction")
	}
	w.swaps = append(w.swaps, order)
	w.usdc -= order.amount
	w.tao += order.out
	w.lamports -= 1_000_000
	w.statuses[tx.ID()] = &solana.SignatureStatus{ConfirmationStatus: "confirmed"}
	if w.executeLost {
		return nil, errors.New("the reply was lost")
	}
	return &jupiter.ExecuteResult{Status: "Success", Signature: tx.ID()}, nil
}

func (w *world) BestAsk(context.Context) (float64, error) { return w.ask, nil }

func (w *world) MessageStatus(context.Context, string) (string, string, error) {
	return "INFLIGHT", "", nil
}

// Store

func (w *world) Load(context.Context) (*State, uint64, error) {
	state, err := UnmarshalState(w.stateJSON)
	return state, w.version, err
}

func (w *world) Save(_ context.Context, state *State, version uint64) (uint64, error) {
	if w.staleNext || version != w.version {
		w.staleNext = false
		return 0, fmt.Errorf("%w: version %d", ErrStale, version)
	}
	raw, err := MarshalState(state)
	if err != nil {
		return 0, err
	}
	w.stateJSON, w.version = raw, w.version+1
	w.saves++
	return w.version, nil
}

func (w *world) Record(_ context.Context, event Event) error {
	w.events = append(w.events, event)
	return nil
}

func (w *world) kinds() string {
	kinds := make([]string, len(w.events))
	for i, event := range w.events {
		kinds[i] = event.Kind
	}
	return strings.Join(kinds, ",")
}

const enabled = "  enabled: true"

// The whole route: USDC becomes alpha, in order, and the wallet ends funded with
// its gas reserve intact.
func TestConvertsUSDCWhenTheWalletIsShort(t *testing.T) {
	w := newWorld(t, enabled)
	w.usdc = 5_000 * microPerUSDC
	need := alpha(1_656)
	result, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: need})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Funded || result.AlphaRao.Cmp(need) < 0 {
		t.Fatalf("not funded: %+v (alpha %s)", result, w.alphaRao)
	}
	if got := w.kinds(); got != "swapped,bridged,arrived,unwrapped,staked" {
		t.Fatal("steps", got)
	}
	if len(w.swaps) != 1 || len(w.sends) != 1 {
		t.Fatalf("%d swaps and %d sends", len(w.swaps), len(w.sends))
	}
	if w.nativeWei.Cmp(taoWei(0.1)) < 0 {
		t.Fatalf("the stake ate the gas reserve: %s wei left", w.nativeWei)
	}
	if w.wtaoWei.Sign() != 0 {
		t.Fatal("wTAO left behind", w.wtaoWei)
	}
	// 1,656 alpha is 6.624 TAO at this price. With its 1% buffer, less the 0.1 TAO
	// the wallet holds above its gas reserve, that is about $1,590.
	if spent := w.swaps[0].amount; spent < 1_585*microPerUSDC || spent > 1_620*microPerUSDC {
		t.Fatal("spent", microString(spent))
	}
	state, _, err := w.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Swap != nil || state.Bridge != nil || state.SpentUSDCMicro != w.swaps[0].amount {
		t.Fatalf("state after the conversion: %+v", state)
	}
}

func TestAFundedWalletTouchesNothing(t *testing.T) {
	w := newWorld(t, enabled)
	w.alphaRao = alpha(2_000)
	w.usdc = 5_000 * microPerUSDC
	result, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: alpha(1_656)})
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if w.saves != 0 || len(w.operations) != 0 || len(w.swaps) != 0 || len(w.sends) != 0 {
		t.Fatal("a funded wallet acted")
	}
}

// What is already on the way is used before more is bought, from the furthest
// along back to the start.
func TestUsesWhatTheWalletsAlreadyHold(t *testing.T) {
	need := alpha(1_000) // 4 TAO at this price
	for _, test := range []struct {
		name  string
		setup func(*world)
		steps string
	}{
		{"native TAO", func(w *world) { w.nativeWei = taoWei(5) }, "staked"},
		{"wTAO", func(w *world) { w.wtaoWei = taoWei(5) }, "unwrapped,staked"},
		{"TAO on Solana", func(w *world) { w.tao = 5 * raoPerTAO }, "bridged,arrived,unwrapped,staked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := newWorld(t, enabled)
			test.setup(w)
			result, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: need})
			if err != nil || !result.Funded {
				t.Fatal(result, err)
			}
			if got := w.kinds(); got != test.steps {
				t.Fatal("steps", got)
			}
			if len(w.swaps) != 0 {
				t.Fatal("bought USDC it did not need")
			}
		})
	}
}

// Refusals spend nothing and sign nothing: the fake's simulation refuses a signed
// transaction, and the only other places a signature could go, the journal and the
// two sends, stay empty here.
func TestRefusalsSpendNothing(t *testing.T) {
	need := alpha(1_656)
	for _, test := range []struct {
		name       string
		conversion string
		setup      func(*world)
		want       error
		wantText   string
	}{
		{"disabled", "  enabled: false", func(w *world) { w.usdc = 5_000 * microPerUSDC }, ErrDisabled, ""},
		{"too little USDC", enabled, func(w *world) { w.usdc = 1_000 * microPerUSDC }, ErrInsufficientUSDC, ""},
		{"no SOL", enabled, func(w *world) { w.usdc = 5_000 * microPerUSDC; w.lamports = 1_000_000 }, ErrInsufficientSOL, ""},
		{"over budget", enabled + "\n  max_usdc_per_epoch: 1000", func(w *world) { w.usdc = 5_000 * microPerUSDC }, ErrEpochBudget, ""},
		{"Jupiter above the guard", enabled, func(w *world) { w.usdc = 5_000 * microPerUSDC; w.jupiterPrice = 250 }, ErrPriceGuard, ""},
		{"simulation pays less", enabled, func(w *world) { w.usdc = 5_000 * microPerUSDC; w.simulateTaoDelta = -2 * raoPerTAO }, nil, "below the floor"},
		{"simulation fails", enabled, func(w *world) { w.usdc = 5_000 * microPerUSDC; w.simulateErr = json.RawMessage(`"BlockhashNotFound"`) }, nil, "simulation failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := newWorld(t, test.conversion)
			test.setup(w)
			_, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: need})
			if err == nil {
				t.Fatal("no refusal")
			}
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatal(err)
			}
			if !strings.Contains(err.Error(), test.wantText) {
				t.Fatal(err)
			}
			if len(w.swaps) != 0 || len(w.sends) != 0 || len(w.operations) != 0 {
				t.Fatal("a refusal acted")
			}
			state, _, _ := w.Load(context.Background())
			if state.Swap != nil || state.SpentUSDCMicro != 0 {
				t.Fatalf("a refusal left state: %+v", state)
			}
		})
	}
}

// A changed bridge keeps the TAO on Solana rather than sending it elsewhere.
func TestAChangedBridgeSendsNothing(t *testing.T) {
	for name, change := range map[string]func(*world){
		"solana peer": func(w *world) { w.peerData[8+12] ^= 1 },
		"wtao peer":   func(w *world) { w.peerStore[0] ^= 1 },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, enabled)
			w.tao = 7 * raoPerTAO
			change(w)
			_, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: alpha(1_000)})
			if !errors.Is(err, ErrBridgeChanged) {
				t.Fatal(err)
			}
			if len(w.sends) != 0 {
				t.Fatal("sent over a changed bridge")
			}
		})
	}
}

// A swap that lands with its reply lost is booked once, not bought again.
func TestALostSwapReplyDoesNotBuyTwice(t *testing.T) {
	w := newWorld(t, enabled)
	w.usdc = 5_000 * microPerUSDC
	w.executeLost = true
	result, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: alpha(1_656)})
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(w.swaps) != 1 {
		t.Fatalf("%d swaps", len(w.swaps))
	}
}

// A swap that never lands is forgotten only after it can no longer land, gives
// its budget back, and is bought again once.
func TestAnExpiredSwapIsBoughtOnce(t *testing.T) {
	w := newWorld(t, enabled+"\n  max_usdc_per_epoch: 2000")
	w.usdc = 5_000 * microPerUSDC
	w.executeDrop = true
	done := false
	sleep := w.sleep
	f, err := NewFunder(w.cfg, testNetuid, Deps{
		Store: w, EVM: w, Sender: w, Solana: worldSolana{w}, Jupiter: w, Market: w, Scan: w,
		Now: func() time.Time { return w.clock },
		Sleep: func(ctx context.Context, d time.Duration) error {
			state, _, _ := w.Load(ctx)
			if !done && state.Swap == nil {
				t.Fatal("the swap was forgotten while waiting")
			}
			w.executeDrop = false
			done = true
			return sleep(ctx, d)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.Ensure(context.Background(), Request{Epoch: 7, NeedRao: alpha(1_656)})
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(w.swaps) != 1 || !strings.Contains(w.kinds(), "swap_failed") {
		t.Fatal(len(w.swaps), w.kinds())
	}
	state, _, _ := w.Load(context.Background())
	if state.SpentUSDCMicro != w.swaps[0].amount {
		t.Fatal("the failed swap kept its budget", state.SpentUSDCMicro, w.swaps[0].amount)
	}
}

// Jupiter's expired report clears the swap at once: it can never land.
func TestAReportedExpiryRetries(t *testing.T) {
	w := newWorld(t, enabled)
	w.usdc = 5_000 * microPerUSDC
	w.executeCode = -1005
	if _, _, err := w.funder(t).step(context.Background(), Request{Epoch: 7, NeedRao: alpha(1_656)}); err != nil {
		t.Fatal(err)
	}
	state, _, _ := w.Load(context.Background())
	if state.Swap != nil || state.SpentUSDCMicro != 0 {
		t.Fatalf("expired swap kept: %+v", state)
	}
}

// A bridge send whose broadcast was lost goes out again as the same bytes.
func TestALostBridgeBroadcastIsRepeated(t *testing.T) {
	w := newWorld(t, enabled)
	w.tao = 7 * raoPerTAO
	w.sendDrop = 1
	result, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: alpha(1_000)})
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(w.sends) != 1 {
		t.Fatalf("%d sends", len(w.sends))
	}
}

// A send that lands with its reply lost is not sent again.
func TestALostBridgeReplyDoesNotSendTwice(t *testing.T) {
	w := newWorld(t, enabled)
	w.tao = 7 * raoPerTAO
	w.sendLost = true
	result, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: alpha(1_000)})
	if err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(w.sends) != 1 {
		t.Fatalf("%d sends", len(w.sends))
	}
}

// While a bridge is under way its TAO is in no balance, and that must not read
// as a reason to buy more: through a slow delivery and a restart nothing is
// bought or sent again.
func TestAWaitingBridgeBuysNothingMoreAcrossARestart(t *testing.T) {
	w := newWorld(t, enabled)
	w.usdc = 5_000 * microPerUSDC
	w.deliverAfter = 1_000_000
	need := alpha(1_656)
	result, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: need})
	if err != nil {
		t.Fatal(err)
	}
	if result.Funded || !strings.Contains(result.Waiting, "LayerZero delivery") {
		t.Fatalf("%+v", result)
	}
	if len(w.swaps) != 1 || len(w.sends) != 1 {
		t.Fatalf("%d swaps and %d sends", len(w.swaps), len(w.sends))
	}
	// A new process, a new run, still waiting.
	if result, err = w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: need}); err != nil || result.Funded {
		t.Fatal(result, err)
	}
	if len(w.swaps) != 1 || len(w.sends) != 1 {
		t.Fatalf("%d swaps and %d sends after the restart", len(w.swaps), len(w.sends))
	}
	w.pending[0].after = 1
	if result, err = w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: need}); err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(w.swaps) != 1 || len(w.sends) != 1 {
		t.Fatalf("%d swaps and %d sends at the end", len(w.swaps), len(w.sends))
	}
}

// A journal write that loses stops the step before anything is sent.
func TestALostStateWriteSendsNothing(t *testing.T) {
	w := newWorld(t, enabled)
	w.usdc = 5_000 * microPerUSDC
	w.staleNext = true
	_, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: alpha(1_656)})
	if !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	if len(w.swaps) != 0 {
		t.Fatal("swapped without its journal")
	}
}

// The USDC budget is per epoch: a new epoch starts it again.
func TestTheBudgetIsPerEpoch(t *testing.T) {
	w := newWorld(t, enabled+"\n  max_usdc_per_epoch: 1700")
	w.usdc = 5_000 * microPerUSDC
	if result, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: alpha(1_656)}); err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	// More is owed in the same epoch: over budget.
	if _, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: alpha(3_400)}); !errors.Is(err, ErrEpochBudget) {
		t.Fatal(err)
	}
	if result, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 8, NeedRao: alpha(3_300)}); err != nil || !result.Funded {
		t.Fatal(result, err)
	}
	if len(w.swaps) != 2 {
		t.Fatalf("%d swaps", len(w.swaps))
	}
}

// Steps that never fund the wallet stop rather than spending gas forever.
func TestStepsThatDoNotHelpStop(t *testing.T) {
	w := newWorld(t, enabled)
	w.nativeWei = taoWei(100)
	w.price = big.NewInt(4_000_000_000_000_000)
	stubborn := &stubbornSender{world: w}
	f, err := NewFunder(w.cfg, testNetuid, Deps{Store: w, EVM: w, Sender: stubborn, Solana: worldSolana{w}, Jupiter: w, Market: w, Scan: w, Now: func() time.Time { return w.clock }, Sleep: w.sleep})
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.Ensure(context.Background(), Request{Epoch: 7, NeedRao: alpha(1_000)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Funded || stubborn.sends != maxActionsPerEnsure {
		t.Fatalf("%+v after %d sends", result, stubborn.sends)
	}
}

// stubbornSender accepts every send and changes nothing.
type stubbornSender struct {
	world *world
	sends int
}

func (s *stubbornSender) SendPrepared(ctx context.Context, prepare Prepare) (string, error) {
	operation, _, _, err := prepare(ctx)
	if err != nil || operation == "" {
		return "", err
	}
	s.sends++
	return "0xhash", nil
}

func TestASendErrorIsReported(t *testing.T) {
	w := newWorld(t, enabled)
	w.nativeWei = taoWei(10)
	w.sendErr = errors.New("reverted")
	if _, err := w.funder(t).Ensure(context.Background(), Request{Epoch: 7, NeedRao: alpha(1_000)}); err == nil || !strings.Contains(err.Error(), "reverted") {
		t.Fatal(err)
	}
}

// The signer's gate admits exactly the two conversion calls.
func TestAllowedAlphaWalletCall(t *testing.T) {
	hotkey := [32]byte{0xc0}
	allowed := func(to common.Address, data []byte) bool {
		return AllowedAlphaWalletCall(to, data, bridge.WTAO, hotkey, testNetuid)
	}
	if !allowed(bridge.WTAO, packWithdraw(big.NewInt(1))) {
		t.Fatal("unwrap refused")
	}
	if !allowed(StakingPrecompile, packAddStakeLimit(hotkey, 1, 1, testNetuid)) {
		t.Fatal("stake refused")
	}
	partial := packAddStakeLimit(hotkey, 1, 1, testNetuid)
	partial[131] = 1
	otherOwner := common.HexToAddress("0x1")
	transfer := append(append([]byte{}, selector("transfer(address,uint256)")...), make([]byte, 64)...)
	transferStake := append(append([]byte{}, selector("transferStake(bytes32,bytes32,uint256,uint256,uint256)")...), make([]byte, 160)...)
	for name, refused := range map[string]bool{
		"withdraw elsewhere": allowed(otherOwner, packWithdraw(big.NewInt(1))),
		"token transfer":     allowed(bridge.WTAO, transfer),
		"stake transfer":     allowed(StakingPrecompile, transferStake),
		"another hotkey":     allowed(StakingPrecompile, packAddStakeLimit([32]byte{1}, 1, 1, testNetuid)),
		"another subnet":     allowed(StakingPrecompile, packAddStakeLimit(hotkey, 1, 1, 26)),
		"a partial fill":     allowed(StakingPrecompile, partial),
		"short data":         allowed(bridge.WTAO, withdrawSelector),
	} {
		if refused {
			t.Fatal("admitted", name)
		}
	}
}

func TestStatusReportsAHotkeyThatCannotTakeStake(t *testing.T) {
	w := newWorld(t, enabled)
	w.hotkeyErr = errors.New("dispatch execution failed: HotKeyAccountNotExists")
	w.usdc, w.lamports = 12*microPerUSDC, 34
	status, err := w.funder(t).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.HotkeyError, "HotKeyAccountNotExists") || status.USDCMicro != 12*microPerUSDC || status.Lamports != 34 || !status.ConversionEnabled {
		t.Fatalf("%+v", status)
	}
}

func TestParseConfig(t *testing.T) {
	raw := testConfigYAML(t, enabled)
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Conversion.Enabled || cfg.Conversion.MaxUSDCMicroPerEpoch != 6_000*microPerUSDC || cfg.Conversion.GasReserveRao != raoPerTAO/5 || cfg.Conversion.MaxBridgeFeeLamports != lamportsPerSOL/20 {
		t.Fatalf("%+v", cfg.Conversion)
	}
	if crypto.PubkeyToAddress(cfg.AlphaKey().PublicKey) != cfg.AlphaAddress {
		t.Fatal("alpha key")
	}
	secretHex := hex.EncodeToString(crypto.FromECDSA(cfg.AlphaKey()))
	secretSolana := solana.Base58Encode(cfg.usdcKey)
	for _, printed := range []string{fmt.Sprintf("%v", cfg), fmt.Sprintf("%s", cfg), fmt.Sprintf("%#v", cfg), fmt.Sprint(cfg)} {
		if strings.Contains(printed, secretHex) || strings.Contains(printed, secretSolana) {
			t.Fatal("a printed config holds a key")
		}
	}
	mutations := map[string]func(string) string{
		"unknown field": func(s string) string { return s + "\nsurprise: 1\n" },
		"wrong evm address": func(s string) string {
			return strings.Replace(s, cfg.AlphaAddress.Hex(), common.HexToAddress("0x1").Hex(), 1)
		},
		"wrong solana address": func(s string) string {
			return strings.Replace(s, cfg.USDCAddress.String(), bridge.TaoMint.String(), 1)
		},
		"short hotkey":  func(s string) string { return strings.Replace(s, strings.Repeat("c0", 32), "c0", 1) },
		"bad gap":       func(s string) string { return s + "  max_price_gap: 0.5\n" },
		"sol below fee": func(s string) string { return s + "  min_sol: 0.01\n" },
	}
	for name, mutate := range mutations {
		_, err := ParseConfig([]byte(mutate(string(raw))))
		if err == nil {
			t.Fatal("accepted", name)
		}
		if strings.Contains(err.Error(), secretHex) || strings.Contains(err.Error(), secretSolana) {
			t.Fatal("an error holds a key:", name)
		}
	}
}

// Raw journals survive the store's encoding, keeping what resuming needs.
func TestStateRoundTrip(t *testing.T) {
	state := &State{Epoch: 3, SpentUSDCMicro: 9, Swap: &SwapRecord{Raw: base64.StdEncoding.EncodeToString([]byte("tx")), Ask: 240.5, SubmittedAt: time.Unix(5, 0).UTC()}, Bridge: &BridgeRecord{Signature: "s", WTAOBeforeWei: "12"}}
	raw, err := MarshalState(state)
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Swap == nil || back.Bridge == nil || back.Swap.Ask != 240.5 || !back.Swap.SubmittedAt.Equal(time.Unix(5, 0)) || back.Bridge.WTAOBeforeWei != "12" {
		t.Fatalf("%+v", back)
	}
	empty, err := UnmarshalState(nil)
	if err != nil || empty.Swap != nil {
		t.Fatal(empty, err)
	}
}
