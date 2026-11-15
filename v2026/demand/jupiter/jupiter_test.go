package jupiter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	taoMint  = "taoC6xyv2v8tDLcev4uaGUgV4vdQsWJrGft2kcBRrBY"
	usdcMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	taker    = "FkaLnX17cXZGyeu3kZGdHCNdFMJJzBrPPYVvd18B3MZp"
	receiver = "46yocBWGMD1vB9auwVhN2kvu3cm6qcX6DLZHr4r78PTG"
)

type request struct {
	Method string
	Path   string
	Query  map[string]string
	Body   map[string]string
	APIKey string
}

type fake struct {
	t        *testing.T
	mu       sync.Mutex
	requests []request
	reply    func(r request) (int, string)
}

func newFake(t *testing.T, apiKey string, reply func(r request) (int, string)) (*fake, *Client, *[]time.Duration) {
	f := &fake{t: t, reply: reply}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	var waits []time.Duration
	c := NewClient(apiKey, WithBaseURL(server.URL+"/swap/v2/"))
	clock := time.Date(2026, 10, 22, 4, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }
	c.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		clock = clock.Add(d)
		return nil
	}
	return f, c, &waits
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := request{Method: r.Method, Path: r.URL.Path, Query: map[string]string{}, APIKey: r.Header.Get("x-api-key")}
	for k, v := range r.URL.Query() {
		req.Query[k] = v[0]
	}
	if r.Method == http.MethodPost {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &req.Body); err != nil || r.Header.Get("Content-Type") != "application/json" {
			f.t.Errorf("bad POST body %s", raw)
		}
	}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	status, body := f.reply(req)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	io.WriteString(w, body)
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestOrderQuote(t *testing.T) {
	quote := fixture(t, "order_notaker.json")
	f, c, waits := newFake(t, "", func(request) (int, string) { return 200, quote })
	order, err := c.Order(context.Background(), OrderRequest{InputMint: taoMint, OutputMint: usdcMint, Amount: 2_000_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if order.InAmount != 2_000_000_000 || order.OutAmount != 528_134_013 || order.OtherAmountThreshold != 528_134_013 || order.Transaction != nil ||
		order.FeeBps != 10 || order.Router != "metis" || order.RequestID != "01a1621c-9ec5-725b-8845-a9792380b05d" || order.PriceImpactPct > 0 || len(order.Raw) == 0 {
		t.Fatalf("%+v", order)
	}
	r := f.requests[0]
	if r.Method != "GET" || r.Path != "/swap/v2/order" || r.Query["amount"] != "2000000000" || r.Query["inputMint"] != taoMint || r.Query["outputMint"] != usdcMint || r.APIKey != "" {
		t.Fatalf("%+v", r)
	}
	if _, ok := r.Query["taker"]; ok {
		t.Fatal("no taker without one")
	}
	if _, ok := r.Query["slippageBps"]; ok {
		t.Fatal("slippage 0 leaves it to Jupiter")
	}
	// Keyless orders are paced one per 2 s.
	c.Order(context.Background(), OrderRequest{InputMint: taoMint, OutputMint: usdcMint, Amount: 2_000_000_000})
	if len(*waits) != 2 || (*waits)[0] != 0 || (*waits)[1] != 2*time.Second {
		t.Fatalf("waits %v", *waits)
	}
}

func TestOrderWithTaker(t *testing.T) {
	built := fixture(t, "order_taker.json")
	f, c, waits := newFake(t, "key-1", func(request) (int, string) { return 200, built })
	order, err := c.Order(context.Background(), OrderRequest{InputMint: taoMint, OutputMint: usdcMint, Amount: 2_000_000_000, Taker: taker, Receiver: receiver, SlippageBps: 80})
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Transaction string `json:"transaction"`
	}
	json.Unmarshal([]byte(built), &want)
	tx, _ := base64.StdEncoding.DecodeString(want.Transaction)
	if string(order.Transaction) != string(tx) || len(tx) == 0 || order.LastValidBlockHeight != 437_953_820 || order.SlippageBps != 80 ||
		order.OtherAmountThreshold != 524_634_446 || order.SignatureFeePayer != taker || order.Gasless {
		t.Fatalf("%+v", order)
	}
	r := f.requests[0]
	if r.Query["taker"] != taker || r.Query["receiver"] != receiver || r.Query["slippageBps"] != "80" || r.APIKey != "key-1" {
		t.Fatalf("%+v", r)
	}
	c.Order(context.Background(), OrderRequest{InputMint: taoMint, OutputMint: usdcMint, Amount: 2_000_000_000})
	if (*waits)[1] != time.Second {
		t.Fatalf("a keyed client is paced one per second: %v", *waits)
	}
}

// A JupiterZ order as sampled live: gasless, with an expireAt of unix seconds in a
// string, and no lastValidBlockHeight.
func TestOrderRFQ(t *testing.T) {
	body := `{"swapType":"rfq","router":"jupiterz","gasless":true,"inAmount":"1500000000","outAmount":"6716236854","otherAmountThreshold":"6716236854","slippageBps":0,"feeBps":10,"expireAt":"1793663594","signatureFeePayer":"` + taker + `","transaction":"AQ==","requestId":"r"}`
	_, c, _ := newFake(t, "", func(request) (int, string) { return 200, body })
	order, err := c.Order(context.Background(), OrderRequest{InputMint: usdcMint, OutputMint: taoMint, Amount: 1_500_000_000, Taker: taker})
	if err != nil {
		t.Fatal(err)
	}
	if order.LastValidBlockHeight != 0 || !order.ExpireAt.Equal(time.Unix(1_793_663_594, 0)) || !order.Gasless || order.OtherAmountThreshold != order.OutAmount || order.SlippageBps != 0 {
		t.Fatalf("%+v", order)
	}
	// An AMM order has a height and no such time, and a number parses like a string.
	for name, test := range map[string]struct {
		field string
		want  time.Time
	}{
		"no time":  {``, time.Time{}},
		"null":     {`"expireAt":null,`, time.Time{}},
		"a number": {`"expireAt":1793663594,`, time.Unix(1_793_663_594, 0)},
	} {
		reply := `{` + test.field + `"router":"metis","inAmount":"1","outAmount":"1","requestId":"r"}`
		_, c, _ := newFake(t, "", func(request) (int, string) { return 200, reply })
		order, err := c.Order(context.Background(), OrderRequest{InputMint: usdcMint, OutputMint: taoMint, Amount: 1})
		if err != nil || !order.ExpireAt.Equal(test.want) {
			t.Fatalf("%s: %+v %v", name, order, err)
		}
	}
	// What cannot be a time is no order to trust.
	_, c, _ = newFake(t, "", func(request) (int, string) {
		return 200, `{"expireAt":"soon","router":"metis","inAmount":"1","outAmount":"1","requestId":"r"}`
	})
	if _, err := c.Order(context.Background(), OrderRequest{InputMint: usdcMint, OutputMint: taoMint, Amount: 1}); err == nil {
		t.Fatal("an expireAt that is no number was accepted")
	}
}

func TestOrderRefusals(t *testing.T) {
	ctx := context.Background()
	f, c, _ := newFake(t, "", func(request) (int, string) { return 200, "{}" })
	for name, bad := range map[string]OrderRequest{
		"no mint":           {OutputMint: usdcMint, Amount: 1},
		"no amount":         {InputMint: taoMint, OutputMint: usdcMint},
		"receiver no taker": {InputMint: taoMint, OutputMint: usdcMint, Amount: 1, Receiver: receiver},
		"receiver is taker": {InputMint: taoMint, OutputMint: usdcMint, Amount: 1, Taker: taker, Receiver: taker},
		"slippage":          {InputMint: taoMint, OutputMint: usdcMint, Amount: 1, SlippageBps: 10_001},
	} {
		if _, err := c.Order(ctx, bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(f.requests) != 0 {
		t.Fatal("a refused order reached the server")
	}
	ok := OrderRequest{InputMint: taoMint, OutputMint: usdcMint, Amount: 2_000_000_000, Taker: taker}
	for name, c2 := range map[string]struct {
		status int
		body   string
		check  func(error) bool
	}{
		"router error": {200, `{"transaction":"","requestId":"r","router":"metis","inAmount":"2000000000","outAmount":"5","errorCode":1,"errorMessage":"Insufficient funds"}`,
			func(err error) bool {
				var oe *OrderError
				return errors.As(err, &oe) && oe.Code == 1 && oe.Router == "metis"
			}},
		"empty transaction": {200, `{"transaction":"","requestId":"r","router":"metis","inAmount":"2000000000","outAmount":"5"}`,
			func(err error) bool { var oe *OrderError; return errors.As(err, &oe) }},
		"other amount": {200, `{"transaction":null,"requestId":"r","router":"metis","inAmount":"1","outAmount":"5"}`,
			func(err error) bool { return err != nil && strings.Contains(err.Error(), "for 2000000000 in") }},
		"http 400": {400, `{"error":"Invalid inputMint"}`,
			func(err error) bool {
				var ae *APIError
				return errors.As(err, &ae) && ae.Status == 400 && ae.Path == "/order"
			}},
		"top-level error": {200, `{"error":"Route not found"}`,
			func(err error) bool {
				var oe *OrderError
				return errors.As(err, &oe) && oe.Message == "Route not found"
			}},
	} {
		_, client, _ := newFake(t, "", func(request) (int, string) { return c2.status, c2.body })
		if _, err := client.Order(ctx, ok); !c2.check(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestExecute(t *testing.T) {
	ctx := context.Background()
	success := `{"status":"Success","signature":"5sig","slot":"459920001","code":0,"totalInputAmount":"2000000000","inputAmountResult":"2000000000","outputAmountResult":"529000000","totalOutputAmount":"528471000"}`
	f, c, waits := newFake(t, "", func(request) (int, string) { return 200, success })
	result, err := c.Execute(ctx, []byte{1, 2, 3}, "req-1")
	if err != nil || !result.Succeeded() || result.Expired() || result.Signature != "5sig" || result.Slot != 459_920_001 || result.InputAmount != 2_000_000_000 || result.OutputAmount != 528_471_000 || len(result.Raw) == 0 {
		t.Fatalf("%+v %v", result, err)
	}
	r := f.requests[0]
	if r.Method != "POST" || r.Path != "/swap/v2/execute" || r.Body["signedTransaction"] != base64.StdEncoding.EncodeToString([]byte{1, 2, 3}) || r.Body["requestId"] != "req-1" {
		t.Fatalf("%+v", r)
	}
	if len(*waits) != 0 {
		t.Fatal("/execute is not paced")
	}
	expired := `{"status":"Failed","signature":"5sig","code":-1005,"error":"Transaction expired"}`
	_, c, _ = newFake(t, "", func(request) (int, string) { return 200, expired })
	result, err = c.Execute(ctx, []byte{1}, "req-2")
	if err != nil || result.Succeeded() || !result.Expired() || result.Error != "Transaction expired" {
		t.Fatalf("a failed swap is a result: %+v %v", result, err)
	}
	failedOutput := `{"status":"Failed","code":-1000,"inputAmountResult":"7","outputAmountResult":"9"}`
	_, c, _ = newFake(t, "", func(request) (int, string) { return 200, failedOutput })
	if result, err = c.Execute(ctx, []byte{1}, "req-3"); err != nil || result.InputAmount != 7 || result.OutputAmount != 9 {
		t.Fatalf("route amounts back the totals: %+v %v", result, err)
	}
	calls := 0
	_, c, _ = newFake(t, "", func(request) (int, string) { calls++; return 503, "busy" })
	var ae *APIError
	if _, err := c.Execute(ctx, []byte{1}, "req-4"); !errors.As(err, &ae) || ae.Status != 503 || calls != 1 {
		t.Fatalf("/execute is never retried: %v after %d calls", err, calls)
	}
	_, c, _ = newFake(t, "", func(request) (int, string) { return 200, `{}` })
	if _, err := c.Execute(ctx, []byte{1}, "req-5"); err == nil {
		t.Fatal("a reply without a status must fail")
	}
	if _, err := c.Execute(ctx, nil, "req-6"); err == nil {
		t.Fatal("an empty transaction must fail")
	}
}
