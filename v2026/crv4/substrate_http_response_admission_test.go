// Keep physical byte admission separate from the native method retry owner.
package crv4

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Unknown methods and submissions cross the undecorated physical transport,
// but the shared HTTP owner must still refuse declared overlimit responses.
func TestSubstrateHttpUnmarkedResponseAdmission(t *testing.T) {
	for _, method := range []string{"synthetic_unknown_read", "author_submitExtrinsic", "author_submitAndWatchExtrinsic"} {
		calls, closes := 0, 0
		client := newSubstrateReadHttpDecoratedFixture(t, 64, substrateReadHttpTestRoundTripper(func(request *http.Request) (*http.Response, error) {
			calls++
			if marked, _ := request.Context().Value(substrateReadHttpContextKey{}).(bool); marked {
				t.Fatal("unmarked method gained replay authority")
			}
			return &http.Response{StatusCode: 200, ContentLength: substrateReadHttpResponseLimit + 1, Body: &substrateReadHttpTestBody{reader: strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"accepted"}`), closes: &closes}}, nil
		}))
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		client.readRetry = hooks
		result := "unchanged"
		err := client.CallContext(t.Context(), &result, method, "0x0102")
		if !errors.Is(err, gsrpcgeth.ErrHttpResponseLimit) || calls != 1 || closes != 1 || budget.attempts != 0 || budget.owner != nil || result != "unchanged" || RetryableSubstrateReadTransportError(err) {
			t.Fatalf("unmarked %s escaped admission or gained retry: calls=%d closes=%d attempts=%d result=%q err=%v", method, calls, closes, budget.attempts, result, err)
		}
	}
}

// The production metadata path was already marked and physically bounded.
// Its smaller configured ceiling must remain effective before the shared one.
func TestSubstrateHttpRuntimeMetadataRetainsMarkedAdmission(t *testing.T) {
	calls, closes := 0, 0
	block := types.Hash{71}
	client := newSubstrateReadHttpDecoratedFixture(t, 64, substrateReadHttpTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		var call chainContextRPCRequest
		if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		if marked, _ := request.Context().Value(substrateReadHttpContextKey{}).(bool); !marked || call.Method != "state_getMetadata" || len(call.Params) != 1 || string(call.Params[0]) != `"`+block.Hex()+`"` {
			t.Fatal("metadata lost marked exact-block read", call)
		}
		return &http.Response{StatusCode: 200, ContentLength: 128, Body: &substrateReadHttpTestBody{reader: strings.NewReader(`{}`), closes: &closes}}, nil
	}))
	budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
	client.readRetry = hooks
	metadata, hash, err := RuntimeMetadataAtContext(t.Context(), &Chain{API: &gsrpc.SubstrateAPI{Client: client}}, block)
	if err == nil || errors.Is(err, gsrpcgeth.ErrHttpResponseLimit) || !strings.Contains(err.Error(), "native HTTP response exceeds byte limit") || metadata != nil || hash != "" || calls != 1 || closes != 1 || budget.attempts != 1 || budget.elapsed != 0 {
		t.Fatalf("marked metadata transport changed: calls=%d closes=%d attempts=%d metadata=%p hash=%q err=%v", calls, closes, budget.attempts, metadata, hash, err)
	}
}

func TestSubstrateHttpUnmarkedCloseFailureNeverReplays(t *testing.T) {
	for _, status := range []int{200, 503} {
		calls, closes := 0, 0
		sentinel := errors.New("synthetic response release failure")
		client := newSubstrateReadHttpDecoratedFixture(t, 64, substrateReadHttpTestRoundTripper(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, ContentLength: -1, Body: &substrateReadHttpTestBody{reader: strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"accepted"}`), closeErr: sentinel, closes: &closes}}, nil
		}))
		budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
		client.readRetry = hooks
		result := "unchanged"
		err := client.CallContext(t.Context(), &result, "author_submitExtrinsic", "0x0102")
		if !errors.Is(err, sentinel) || calls != 1 || closes != 1 || budget.attempts != 0 || budget.owner != nil || result != "unchanged" || RetryableSubstrateReadTransportError(err) {
			t.Fatalf("ambiguous submission release changed ownership: status=%d calls=%d closes=%d attempts=%d result=%q err=%v", status, calls, closes, budget.attempts, result, err)
		}
	}
}

type substrateHttpRepeatedZero struct{ remaining int }

func (self *substrateHttpRepeatedZero) Read(value []byte) (int, error) {
	if self.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(value), self.remaining)
	for index := range value[:n] {
		value[index] = '0'
	}
	self.remaining -= n
	return n, nil
}

// Both response owners must preserve 16 MiB of native event bytes after hex
// expansion; the smaller websocket/server-request bound is not interchangeable.
func TestSubstrateHttpLargeEventWireAllowance(t *testing.T) {
	calls, closes := 0, 0
	client := newSubstrateReadHttpDecoratedFixture(t, substrateReadHttpResponseLimit, substrateReadHttpTestRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		if marked, _ := request.Context().Value(substrateReadHttpContextKey{}).(bool); !marked {
			t.Fatal("native storage read lost marker")
		}
		reader := io.MultiReader(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x`), &substrateHttpRepeatedZero{remaining: 2 * finalizedExtrinsicEventsBytes}, strings.NewReader(`"}`))
		return &http.Response{StatusCode: 200, ContentLength: -1, Header: make(http.Header), Body: &substrateReadHttpTestBody{reader: reader, closes: &closes}}, nil
	}))
	budget, hooks := newSubstrateReadTestBudget(t, 75*time.Second)
	client.readRetry = hooks
	var result string
	err := client.CallContext(t.Context(), &result, "state_getStorage", "0x0102", types.Hash{73}.Hex())
	if err != nil || len(result) != 2+2*finalizedExtrinsicEventsBytes || calls != 1 || closes != 1 || budget.attempts != 1 {
		t.Fatalf("valid native event wire ceiling narrowed: len=%d calls=%d closes=%d attempts=%d err=%v", len(result), calls, closes, budget.attempts, err)
	}
}
