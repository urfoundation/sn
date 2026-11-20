package onchain

import (
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// VA-4: a payable call carries its value through the preflight, the gas
// estimate and the signed transaction; a call without one sends none and its
// preflight and estimate omit the field, as before values existed.
func TestSubmitCarriesValue(t *testing.T) {
	key, err := crypto.HexToECDSA(strings.Repeat("2b", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []*big.Int{big.NewInt(2_000_000_000), nil} {
		want, wantCall := new(big.Int), "absent"
		if value != nil {
			want.Set(value)
			wantCall = value.String()
		}
		var lock sync.Mutex
		var calls []string
		var sent *types.Transaction
		block := common.HexToHash("0x10")
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			var call struct {
				ID     json.RawMessage   `json:"id"`
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
				t.Error(err)
				return
			}
			lock.Lock()
			defer lock.Unlock()
			var result any
			switch call.Method {
			case "eth_chainId":
				result = "0x3c4"
			case "eth_call", "eth_estimateGas":
				var arguments struct {
					Value *hexutil.Big `json:"value"`
				}
				if err := json.Unmarshal(call.Params[0], &arguments); err != nil {
					t.Errorf("%s arguments: %v", call.Method, err)
					return
				}
				calls = append(calls, "absent")
				if arguments.Value != nil {
					calls[len(calls)-1] = (*big.Int)(arguments.Value).String()
				}
				result = "0x"
				if call.Method == "eth_estimateGas" {
					result = "0x5208"
				}
			case "eth_getTransactionCount":
				result = "0x0"
			case "eth_gasPrice":
				result = "0x1"
			case "eth_sendRawTransaction":
				var raw hexutil.Bytes
				sent = new(types.Transaction)
				if err := json.Unmarshal(call.Params[0], &raw); err != nil || sent.UnmarshalBinary(raw) != nil {
					t.Errorf("undecodable transaction: %v", err)
					return
				}
				result = sent.Hash()
			case "eth_getTransactionReceipt":
				result = &types.Receipt{Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 21_000, Logs: []*types.Log{}, TxHash: sent.Hash(), GasUsed: 21_000, BlockHash: block, BlockNumber: big.NewInt(16)}
			case "eth_getBlockByNumber":
				result = map[string]any{"number": "0x10", "hash": block}
			default:
				t.Errorf("unexpected RPC %s", call.Method)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": result})
		}))
		receipt, err := Submit(t.Context(), SubmitParams{Contract: common.HexToAddress("0x7a17000000000000000000000000000000000001"), Rpcs: []string{server.URL}, Key: key, Calldata: []byte{1, 2, 3, 4}, ChainID: big.NewInt(964), Value: value})
		server.Close()
		if err != nil || receipt == nil {
			t.Fatalf("submit with value %v: %v", value, err)
		}
		if len(calls) != 2 || calls[0] != wantCall || calls[1] != wantCall || sent.Value().Cmp(want) != 0 {
			t.Fatalf("value %v: preflight and estimate %v, transaction %v", value, calls, sent.Value())
		}
	}
}
