package usage

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/v2026/protocol"
)

func sample() *Statement {
	return &Statement{
		DeploymentID: "sn25-mainnet-test", ChainID: 964, GenesisHash: "0x01", Netuid: 25,
		Coordinator: common.HexToAddress("0x1"), VaultV2: common.HexToAddress("0x2"), PolicyHash: "0x03",
		NoID: 1, Epoch: 4, Start: Boundary{Number: 100, Hash: "0xa"}, End: Boundary{Number: 200, Hash: "0xb"},
		ProviderChunks: []string{"sha256:aa"}, ProviderRows: 2, TotalUsageBytes: 30, TotalUsers: 7,
	}
}

func TestSealVerifyAndRoundTrip(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	signer := crypto.PubkeyToAddress(key.PublicKey)
	statement := sample()
	if err := statement.Seal(key); err != nil {
		t.Fatal(err)
	}
	if err := statement.Verify(signer); err != nil {
		t.Fatal(err)
	}
	raw, err := statement.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := back.Verify(signer); err != nil {
		t.Fatal(err)
	}
	if back.TotalUsers != 7 || back.TotalUsageBytes != 30 || back.Epoch != 4 {
		t.Fatalf("%+v", back)
	}
	// The same facts seal to the same bytes, so a retry publishes nothing new.
	again := sample()
	if err := again.Seal(key); err != nil {
		t.Fatal(err)
	}
	if encoded, _ := again.Bytes(); string(encoded) != string(raw) {
		t.Fatal("sealing the same statement twice differs")
	}
	for name, change := range map[string]func(*Statement){
		"users":  func(s *Statement) { s.TotalUsers++ },
		"bytes":  func(s *Statement) { s.TotalUsageBytes++ },
		"epoch":  func(s *Statement) { s.Epoch++ },
		"chunks": func(s *Statement) { s.ProviderChunks = []string{"sha256:bb"} },
		"policy": func(s *Statement) { s.PolicyHash = "0x04" },
	} {
		forged := *back
		change(&forged)
		if err := forged.Verify(signer); err == nil {
			t.Fatal("a changed statement verified:", name)
		}
	}
	other, _ := crypto.GenerateKey()
	if err := back.Verify(crypto.PubkeyToAddress(other.PublicKey)); err == nil {
		t.Fatal("verified under another signer")
	}
	stolen := sample()
	if err := stolen.Seal(other); err != nil {
		t.Fatal(err)
	}
	if err := stolen.Verify(signer); err == nil {
		t.Fatal("a statement from another key verified")
	}
}

func TestDecodeRefusals(t *testing.T) {
	key, _ := crypto.GenerateKey()
	statement := sample()
	if err := statement.Seal(key); err != nil {
		t.Fatal(err)
	}
	raw, _ := statement.Bytes()
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["surprise"] = 1
	extra, _ := json.Marshal(fields)
	for name, data := range map[string][]byte{
		"not json":      []byte("{"),
		"unknown field": extra,
		"trailing":      append(append([]byte{}, raw...), raw...),
		"wrong schema":  []byte(strings.Replace(string(raw), Schema, "other", 1)),
		"oversized":     make([]byte, MaxBytes+1),
	} {
		if _, err := Decode(data); err == nil {
			t.Fatal("decoded", name)
		}
	}
}

func TestRows(t *testing.T) {
	rows := []protocol.LedgerUsageRow{{UsageBytes: 10}, {UsageBytes: 20}}
	statement := sample()
	if err := statement.CheckRows(rows); err != nil {
		t.Fatal(err)
	}
	statement.TotalUsageBytes = 31
	if err := statement.CheckRows(rows); err == nil {
		t.Fatal("a wrong total passed")
	}
	statement.TotalUsageBytes, statement.ProviderRows = 30, 3
	if err := statement.CheckRows(rows); err == nil {
		t.Fatal("a wrong row count passed")
	}
	if _, _, err := SumRows([]protocol.LedgerUsageRow{{UsageBytes: math.MaxUint64}, {UsageBytes: 1}}); err == nil {
		t.Fatal("an overflow passed")
	}
}
