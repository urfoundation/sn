// The pool answers that leave one exact extrinsic's fate to finality are typed;
// every other refusal keeps its original error. Each answer is the node's own
// JSON-RPC wire shape, decoded by the real gsrpc client over a websocket.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpcgeth "github.com/centrifuge/go-substrate-rpc-client/v4/gethrpc"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/blake2b"
)

// One websocket peer answers author_submitAndWatchExtrinsic either with a
// JSON-RPC error object, or with a subscription and then one extrinsicUpdate.
func submitRawTestAnswer(t *testing.T, refusal map[string]any, update string) error {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		for {
			var call chainContextRPCRequest
			if err := connection.ReadJSON(&call); err != nil {
				return
			}
			if call.Method != "author_submitAndWatchExtrinsic" {
				if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": true}); err != nil {
					return
				}
				continue
			}
			if refusal != nil {
				if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": call.ID, "error": refusal}); err != nil {
					return
				}
				continue
			}
			if err := connection.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": "watch-1"}); err != nil {
				return
			}
			notification := map[string]any{"jsonrpc": "2.0", "method": "author_extrinsicUpdate", "params": map[string]any{"subscription": "watch-1", "result": json.RawMessage(update)}}
			if err := connection.WriteJSON(notification); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	transport, err := gsrpcgeth.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	chain := &Chain{API: &gsrpc.SubstrateAPI{Client: &contextSubstrateClient{Client: transport, url: "ws://rpc.example"}}}
	receipt, err := chain.SubmitRawAndWatchFinalized(ctx, "0x0102")
	if receipt != nil {
		t.Fatal("a pool answer fabricated a finalized receipt")
	}
	return err
}

// Substrate's author RPC refusals for a nonce that is taken or ahead, and for
// bytes the pool already holds or recently removed, are left to finality with
// the node's exact code, message and data. Before this type they were opaque
// wrapped errors, and ten of them in a row ended the validator (2026-11-08).
func TestSubmitRawTypesNonceRefusalsAsUnresolved(t *testing.T) {
	raw, err := codec.HexDecodeString("0x0102")
	if err != nil {
		t.Fatal(err)
	}
	hash := blake2b.Sum256(raw)
	for _, test := range []struct {
		name    string
		refusal map[string]any
		outcome SubmissionOutcome
	}{
		{name: "outdated", refusal: map[string]any{"code": 1010, "message": "Invalid Transaction", "data": "Transaction is outdated"}, outcome: SubmissionNonceTaken},
		{name: "outranked", refusal: map[string]any{"code": 1014, "message": "Priority is too low: (18446744073709551615 vs 18446744073709551615)", "data": "The transaction has too low priority to replace another transaction already in the pool."}, outcome: SubmissionNonceTaken},
		{name: "future", refusal: map[string]any{"code": 1010, "message": "Invalid Transaction", "data": "Transaction will be valid in the future"}, outcome: SubmissionNonceAhead},
		{name: "future refused", refusal: map[string]any{"code": 1021, "message": "The pool is not accepting future transactions"}, outcome: SubmissionNonceAhead},
		{name: "banned", refusal: map[string]any{"code": 1012, "message": "Transaction is temporarily banned"}, outcome: SubmissionBytesKnown},
		{name: "already imported", refusal: map[string]any{"code": 1013, "message": "Transaction Already Imported", "data": "0x" + strings.Repeat("ab", 32)}, outcome: SubmissionBytesKnown},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := submitRawTestAnswer(t, test.refusal, "")
			var unresolved *SubmissionUnresolvedError
			if !errors.As(err, &unresolved) || unresolved.Outcome != test.outcome || unresolved.ExtrinsicHash != hash || unresolved.Status != "" {
				t.Fatalf("pool refusal was not left to finality as %d: %T %v", test.outcome, err, err)
			}
			data, _ := test.refusal["data"].(string)
			if unresolved.Code != test.refusal["code"] || unresolved.Message != test.refusal["message"] || unresolved.Data != data || !strings.Contains(err.Error(), test.refusal["message"].(string)) {
				t.Fatalf("pool refusal lost the node's answer: %+v", unresolved)
			}
		})
	}
}

// A watch that ends without finality is left to it too; a usurping
// transaction took the nonce.
func TestSubmitRawTypesEndedWatchAsUnresolved(t *testing.T) {
	usurper := "0x" + strings.Repeat("cd", 32)
	for _, test := range []struct {
		update  string
		outcome SubmissionOutcome
		status  string
	}{
		{update: `{"usurped":"` + usurper + `"}`, outcome: SubmissionNonceTaken, status: "usurped by " + usurper},
		{update: `"invalid"`, outcome: SubmissionWatchEnded, status: "invalid"},
		{update: `"dropped"`, outcome: SubmissionWatchEnded, status: "dropped"},
		{update: `{"retracted":"` + usurper + `"}`, outcome: SubmissionWatchEnded, status: "retracted from " + usurper},
		{update: `{"finalityTimeout":"` + usurper + `"}`, outcome: SubmissionWatchEnded, status: "finality timeout at " + usurper},
	} {
		t.Run(test.status, func(t *testing.T) {
			err := submitRawTestAnswer(t, nil, test.update)
			var unresolved *SubmissionUnresolvedError
			if !errors.As(err, &unresolved) || unresolved.Outcome != test.outcome || unresolved.Status != test.status || unresolved.Code != 0 {
				t.Fatalf("ended watch was not left to finality: %T %v", err, err)
			}
		})
	}
}

// A refusal that no nonce order explains stays the original wrapped error, so
// its owner still counts it as hard: a bad signature, fees, a runtime's custom
// refusal, a full pool, an unknown code, or a 1010 without its exact data.
func TestSubmitRawKeepsOtherRefusalsOpaque(t *testing.T) {
	for _, refusal := range []map[string]any{
		{"code": 1010, "message": "Invalid Transaction", "data": "Transaction has a bad signature"},
		{"code": 1010, "message": "Invalid Transaction", "data": "Inability to pay some fees (e.g. account balance too low)"},
		{"code": 1010, "message": "Invalid Transaction", "data": "Custom error: 1"},
		{"code": 1010, "message": "Invalid Transaction"},
		{"code": 1010, "message": "Invalid Transaction", "data": map[string]any{"stale": true}},
		{"code": 1016, "message": "Immediately Dropped", "data": "The transaction couldn't enter the pool because of the limit"},
		{"code": 1002, "message": "Verification Error: Runtime error", "data": "RuntimeApi(\"Execution failed\")"},
		{"code": -32000, "message": "Transaction is outdated"},
	} {
		err := submitRawTestAnswer(t, refusal, "")
		var unresolved *SubmissionUnresolvedError
		if err == nil || errors.As(err, &unresolved) || !strings.HasPrefix(err.Error(), "crv4: submit/watch ") {
			t.Fatalf("refusal %v was left to finality or lost its wrapping: %T %v", refusal, err, err)
		}
	}
}

// The client's private type name, fields and text, declared in another package.
type jsonError struct {
	Code    int
	Message string
	Data    any
}

func (self *jsonError) Error() string { return self.Message }

// Recognition reads the gsrpc error by exact package path and type name only;
// a value of any other type with the same name, fields and text is not a
// refusal, and neither is a nil or a plain error.
func TestSubmissionRefusalRecognizesOnlyTheClientErrorType(t *testing.T) {
	lookalike := &jsonError{Code: 1010, Message: "Invalid Transaction", Data: "Transaction is outdated"}
	for _, err := range []error{lookalike, (*jsonError)(nil), nil, errors.New("Transaction is outdated")} {
		if submissionRefusal(types.Hash{1}, err) != nil {
			t.Fatalf("%T was read as a pool refusal", err)
		}
	}
}
