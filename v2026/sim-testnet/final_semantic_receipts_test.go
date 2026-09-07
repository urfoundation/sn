package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestCanonicalRPCReceiptLogsHashIgnoresFieldOrderAndForeignLogs(t *testing.T) {
	address := "0x" + strings.Repeat("11", 20)
	foreign := "0x" + strings.Repeat("22", 20)
	tx := "0x" + strings.Repeat("33", 32)
	block := "0x" + strings.Repeat("44", 32)
	topic := "0x" + strings.Repeat("55", 32)
	first := json.RawMessage(`[
{"address":"` + foreign + `","topics":["` + topic + `"],"data":"0x99","blockNumber":"0xa","blockHash":"` + block + `","transactionHash":"` + tx + `","transactionIndex":"0x1","logIndex":"0x0","removed":false},
{"address":"` + address + `","topics":["` + topic + `"],"data":"0x01","blockNumber":"0xa","blockHash":"` + block + `","transactionHash":"` + tx + `","transactionIndex":"0x1","logIndex":"0x1","removed":false}
]`)
	second := json.RawMessage(`[
{"removed":false,"logIndex":"0x1","transactionIndex":"0x1","transactionHash":"` + tx + `","blockHash":"` + block + `","blockNumber":"0xa","data":"0x01","topics":["` + topic + `"],"address":"` + address + `"}
]`)
	allowed := map[common.Address]bool{common.HexToAddress(address): true}
	left, err := finalCanonicalRPCReceiptLogsHash(first, allowed)
	if err != nil {
		t.Fatal(err)
	}
	right, err := finalCanonicalRPCReceiptLogsHash(second, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if left != right || !strings.HasPrefix(left, "0x") || len(left) != 66 {
		t.Fatalf("canonical hashes differ: %q != %q", left, right)
	}
}

func TestCanonicalRPCReceiptLogsRejectsRemovedDuplicateAndMixedTransactions(t *testing.T) {
	address := "0x" + strings.Repeat("11", 20)
	tx := "0x" + strings.Repeat("33", 32)
	otherTx := "0x" + strings.Repeat("34", 32)
	block := "0x" + strings.Repeat("44", 32)
	topic := "0x" + strings.Repeat("55", 32)
	base := `{"address":"` + address + `","topics":["` + topic + `"],"data":"0x01","blockNumber":"0xa","blockHash":"` + block + `","transactionHash":"` + tx + `","transactionIndex":"0x1","logIndex":"0x1","removed":false}`
	allowed := map[common.Address]bool{common.HexToAddress(address): true}
	for name, raw := range map[string]string{
		"removed":           strings.Replace(base, `"removed":false`, `"removed":true`, 1),
		"duplicate":         base + "," + base,
		"mixed transaction": base + "," + strings.Replace(base, tx, otherTx, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := finalCanonicalRPCReceiptLogsHash(json.RawMessage("["+raw+"]"), allowed); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}
