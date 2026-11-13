package onchain

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type finalityClosureTestAPI struct {
	stateLock sync.Mutex
	calls     map[string]int
	reply     func(context.Context, string, int) (any, error)
}

func (self *finalityClosureTestAPI) GetBlockByNumber(ctx context.Context, selector string, full bool) (any, error) {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.calls[selector]++
	if full {
		return nil, errors.New("synthetic finality does not serve transaction bodies")
	}
	return self.reply(ctx, selector, self.calls[selector])
}

func (self *finalityClosureTestAPI) count(selector string) int {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return self.calls[selector]
}

func finalityClosureTestClient(t *testing.T, reply func(context.Context, string, int) (any, error)) (*ethclient.Client, *finalityClosureTestAPI) {
	t.Helper()
	api := &finalityClosureTestAPI{calls: map[string]int{}, reply: reply}
	server := rpc.NewServer()
	if err := server.RegisterName("eth", api); err != nil {
		t.Fatal(err)
	}
	client := ethclient.NewClient(rpc.DialInProc(server))
	t.Cleanup(client.Close)
	t.Cleanup(server.Stop)
	return client, api
}

func finalityClosureTestBlock(selector string, finalized uint64) any {
	number := finalized
	if selector != "finalized" {
		number, _ = hexutil.DecodeUint64(selector)
	}
	return map[string]any{"number": hexutil.EncodeUint64(number), "hash": common.BigToHash(new(big.Int).SetUint64(number)).Hex()}
}

func finalityClosureTestReceipt() *types.Receipt {
	return &types.Receipt{BlockNumber: big.NewInt(90), BlockHash: common.BigToHash(big.NewInt(90)), TxHash: common.Hash{1}}
}

func TestWaitFinalizedClosingTagRejectsRegressedFrontier(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client, api := finalityClosureTestClient(t, func(_ context.Context, selector string, ordinal int) (any, error) {
		finalized := uint64(95)
		if selector == "finalized" && ordinal > 1 {
			finalized = 70
			if ordinal == 3 {
				cancel() // a third tag read proves the regressed attempt retried
			}
		}
		return finalityClosureTestBlock(selector, finalized), nil
	})
	err := waitFinalized(ctx, client, finalityClosureTestReceipt())
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "do not retry") || strings.Contains(err.Error(), "reorged") || api.count("finalized") != 3 {
		t.Fatalf("canonical receipt90 retained regressed95→70 finality: %v tags=%d", err, api.count("finalized"))
	}
}

func TestWaitFinalizedRegressedFrontierCanRecover(t *testing.T) {
	client, api := finalityClosureTestClient(t, func(_ context.Context, selector string, ordinal int) (any, error) {
		finalized := uint64(95)
		if selector == "finalized" && ordinal == 2 {
			finalized = 70
		}
		return finalityClosureTestBlock(selector, finalized), nil
	})
	if err := waitFinalized(t.Context(), client, finalityClosureTestReceipt()); err != nil || api.count("finalized") != 4 || api.count("0x5a") != 3 {
		t.Fatalf("recovered finality did not close original receipt: %v tags=%d inclusions=%d", err, api.count("finalized"), api.count("0x5a"))
	}
}

func TestWaitFinalizedClosingTagAllowsOrdinaryAdvance(t *testing.T) {
	client, api := finalityClosureTestClient(t, func(_ context.Context, selector string, ordinal int) (any, error) {
		finalized := uint64(95)
		if selector == "finalized" && ordinal > 1 {
			finalized = 110
		}
		return finalityClosureTestBlock(selector, finalized), nil
	})
	if err := waitFinalized(t.Context(), client, finalityClosureTestReceipt()); err != nil {
		t.Fatalf("ordinary95→110 finality advance lost receipt90: %v", err)
	}
	if api.count("finalized") != 2 || api.count("0x5f") != 1 || api.count("0x6e") != 1 || api.count("0x5a") != 2 {
		t.Fatal("advance did not close both finalized identities and original receipt")
	}
}

func TestWaitFinalizedRetriesTransientRPCWithinOriginalContext(t *testing.T) {
	for _, phase := range []string{"opening", "inclusion", "closing"} {
		client, api := finalityClosureTestClient(t, func(_ context.Context, selector string, ordinal int) (any, error) {
			if phase == "opening" && selector == "finalized" && ordinal == 1 || phase == "inclusion" && selector == "0x5a" && ordinal == 1 || phase == "closing" && selector == "finalized" && ordinal == 2 {
				return nil, errors.New("synthetic transient finality Rpc outage")
			}
			return finalityClosureTestBlock(selector, 95), nil
		})
		if err := waitFinalized(t.Context(), client, finalityClosureTestReceipt()); err != nil {
			t.Fatalf("transient %s read became terminal evidence: %v", phase, err)
		}
		if api.count("finalized") < 3 || api.count("0x5a") < 2 {
			t.Fatalf("transient %s read bypassed full closing retry", phase)
		}
	}
}

func TestWaitFinalizedRetriesHTTPFailureWithoutReorg(t *testing.T) {
	var inclusionReads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var call struct {
			Id     json.RawMessage   `json:"id"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Error(err)
			return
		}
		var selector string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
			t.Errorf("malformed synthetic selector: %s", call.Params)
			return
		}
		if selector == "0x5a" && inclusionReads.Add(1) == 1 {
			http.Error(writer, "synthetic temporary archive outage", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.Id, "result": finalityClosureTestBlock(selector, 95)})
	}))
	t.Cleanup(server.Close)
	client, err := ethclient.Dial(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	if err := waitFinalized(t.Context(), client, finalityClosureTestReceipt()); err != nil || inclusionReads.Load() != 3 {
		t.Fatalf("HTTP outage was not retried to exact receipt closure: reads=%d error=%v", inclusionReads.Load(), err)
	}
}

func TestWaitFinalizedClosingIdentitiesRejectReplacement(t *testing.T) {
	for _, replaced := range []string{"receipt", "finalized"} {
		client, _ := finalityClosureTestClient(t, func(_ context.Context, selector string, ordinal int) (any, error) {
			block := finalityClosureTestBlock(selector, 95).(map[string]any)
			if replaced == "receipt" && selector == "0x5a" && ordinal == 2 || replaced == "finalized" && selector == "finalized" && ordinal == 2 {
				block["hash"] = common.Hash{0xee}.Hex()
			}
			return block, nil
		})
		err := waitFinalized(t.Context(), client, finalityClosureTestReceipt())
		if err == nil || !strings.Contains(err.Error(), "reorged") && !strings.Contains(err.Error(), "not canonical") {
			t.Fatalf("closing %s replacement retained finality: %v", replaced, err)
		}
	}
}

func TestWaitFinalizedClosingCancellationPublishesNoSuccess(t *testing.T) {
	for _, phase := range []string{"tag error", "final receipt"} {
		ctx, cancel := context.WithCancel(t.Context())
		client, _ := finalityClosureTestClient(t, func(_ context.Context, selector string, ordinal int) (any, error) {
			if phase == "tag error" && selector == "finalized" && ordinal == 2 {
				cancel()
				return nil, errors.New("synthetic closing read canceled")
			}
			if phase == "final receipt" && selector == "0x5a" && ordinal == 2 {
				cancel()
			}
			return finalityClosureTestBlock(selector, 95), nil
		})
		err := waitFinalized(ctx, client, finalityClosureTestReceipt())
		cancel()
		if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "reorged") {
			t.Fatalf("canceled %s published finality/reorg evidence: %v", phase, err)
		}
	}
}

func TestWaitFinalizedMalformedClosingTagRefusesImmediately(t *testing.T) {
	for _, malformed := range []any{map[string]any{}, map[string]any{"number": "0x5f", "hash": "0x01"}} {
		client, api := finalityClosureTestClient(t, func(_ context.Context, selector string, ordinal int) (any, error) {
			if selector == "finalized" && ordinal == 2 {
				return malformed, nil
			}
			return finalityClosureTestBlock(selector, 95), nil
		})
		err := waitFinalized(t.Context(), client, finalityClosureTestReceipt())
		if err == nil || strings.Contains(err.Error(), "reorged") || api.count("finalized") != 2 {
			t.Fatalf("malformed closing tag became success or reorg: reply=%v error=%v", malformed, err)
		}
	}
}

func TestWaitFinalizedMissingWitnessCanRecover(t *testing.T) {
	for _, phase := range []string{"opening", "inclusion", "closing"} {
		client, api := finalityClosureTestClient(t, func(_ context.Context, selector string, ordinal int) (any, error) {
			if phase == "opening" && selector == "finalized" && ordinal == 1 || phase == "inclusion" && selector == "0x5a" && ordinal == 1 || phase == "closing" && selector == "finalized" && ordinal == 2 {
				return nil, nil
			}
			return finalityClosureTestBlock(selector, 95), nil
		})
		if err := waitFinalized(t.Context(), client, finalityClosureTestReceipt()); err != nil || api.count("finalized") < 3 {
			t.Fatalf("temporarily missing %s witness did not recover through closing checks: %v", phase, err)
		}
	}
}

func TestWaitFinalizedMissingWitnessStaysPendingUntilCancellation(t *testing.T) {
	for _, selector := range []string{"finalized", "0x5a"} {
		ctx, cancel := context.WithCancel(t.Context())
		client, api := finalityClosureTestClient(t, func(_ context.Context, current string, ordinal int) (any, error) {
			if current == selector {
				if ordinal == 2 {
					cancel() // two absent reads prove a pending attempt retried
				}
				return nil, nil
			}
			return finalityClosureTestBlock(current, 95), nil
		})
		err := waitFinalized(ctx, client, finalityClosureTestReceipt())
		cancel()
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "do not retry") || strings.Contains(err.Error(), "reorged") || api.count(selector) != 2 {
			t.Fatalf("missing %s witness fabricated finality/reorg: %v", selector, err)
		}
	}
}

func TestWaitFinalizedUncoveredReceiptCancellationStaysAmbiguous(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	client, api := finalityClosureTestClient(t, func(_ context.Context, selector string, _ int) (any, error) {
		cancel()
		return finalityClosureTestBlock(selector, 70), nil
	})
	err := waitFinalized(ctx, client, finalityClosureTestReceipt())
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "do not retry") || api.count("0x5a") != 0 {
		t.Fatalf("uncovered receipt cancellation fabricated success/replay: %v", err)
	}
}
