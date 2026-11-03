package bridge

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/demand/solana"
)

type mainnetSend struct {
	Signature   string `json:"signature"`
	DataHex     string `json:"data_hex"`
	Sender      string `json:"sender"`
	TokenSource string `json:"token_source"`
	LookupTable string `json:"lookup_table"`
	Accounts    []struct {
		Key      string `json:"key"`
		Signer   bool   `json:"signer"`
		Writable bool   `json:"writable"`
	} `json:"accounts"`
}

func loadMainnetSend(t *testing.T) mainnetSend {
	t.Helper()
	raw, err := os.ReadFile("testdata/send_mainnet.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture mainnetSend
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// A send's accounts and data are exactly those of a send that landed on mainnet.
func TestSendMatchesMainnet(t *testing.T) {
	fixture := loadMainnetSend(t)
	data, err := hex.DecodeString(fixture.DataHex)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture sends 0.1 TAO to its H160, with empty options, capped at its
	// quoted fee.
	send := Send{
		Sender:         solana.MustPublicKey(fixture.Sender),
		To:             common.BytesToAddress(data[24:44]),
		AmountRao:      100_000_000,
		MaxFeeLamports: 21_851_909,
	}
	if got := hex.EncodeToString(send.Data()); got != fixture.DataHex {
		t.Fatalf("data\n got %s\nwant %s", got, fixture.DataHex)
	}
	instruction, err := send.Instruction()
	if err != nil {
		t.Fatal(err)
	}
	if instruction.Program != OFTProgram {
		t.Fatal("program", instruction.Program)
	}
	if len(instruction.Accounts) != len(fixture.Accounts) {
		t.Fatal(len(instruction.Accounts), len(fixture.Accounts))
	}
	for i, account := range instruction.Accounts {
		want := fixture.Accounts[i]
		if account.Key.String() != want.Key {
			t.Fatalf("account %d: got %s want %s", i, account.Key, want.Key)
		}
		if account.Signer != want.Signer {
			t.Fatalf("account %d (%s): signer %t, want %t", i, want.Key, account.Signer, want.Signer)
		}
		// The real flags are per key across the message, so a key listed twice
		// reads as writable in both positions: 2 and 10 are the store.
		if account.Writable != want.Writable {
			t.Fatalf("account %d (%s): writable %t, want %t", i, want.Key, account.Writable, want.Writable)
		}
	}
	if instruction.Accounts[3].Key.String() != fixture.TokenSource {
		t.Fatal("the token source is not the sender's associated account")
	}
	if fixture.LookupTable != LookupTable.String() {
		t.Fatal("lookup table", fixture.LookupTable)
	}
}

func TestSendRefusesDustAndMissingFields(t *testing.T) {
	sender := solana.PublicKey{0x5e}
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	cases := []Send{
		{To: to, AmountRao: 1_000, MaxFeeLamports: 1},
		{Sender: sender, AmountRao: 1_000, MaxFeeLamports: 1},
		{Sender: sender, To: to, MaxFeeLamports: 1},
		{Sender: sender, To: to, AmountRao: 1_500, MaxFeeLamports: 1},
		{Sender: sender, To: to, AmountRao: 1_000},
	}
	for i, send := range cases {
		if err := send.Validate(); err == nil {
			t.Fatalf("case %d passed validation", i)
		}
	}
}

func TestCheckPeerConfig(t *testing.T) {
	data := make([]byte, 64)
	copy(data[peerAddressOffset+12:], WTAO[:])
	if err := CheckPeerConfig(data); err != nil {
		t.Fatal(err)
	}
	data[peerAddressOffset+12] ^= 1
	if err := CheckPeerConfig(data); err == nil {
		t.Fatal("a different peer passed")
	}
	if err := CheckPeerConfig(data[:20]); err == nil {
		t.Fatal("a short account passed")
	}
}

// A compiled send fits in a packet, and read back through the lookup table, its
// accounts are the mainnet send's in order, with the same flags.
func TestCompiledSendResolvesToMainnetAccounts(t *testing.T) {
	fixture := loadMainnetSend(t)
	encoded, err := os.ReadFile("testdata/lookup_table.b64")
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	table, err := solana.ParseLookupTable(LookupTable, data)
	if err != nil {
		t.Fatal(err)
	}
	fixtureData, err := hex.DecodeString(fixture.DataHex)
	if err != nil {
		t.Fatal(err)
	}
	send := Send{Sender: solana.MustPublicKey(fixture.Sender), To: common.BytesToAddress(fixtureData[24:44]), AmountRao: 100_000_000, MaxFeeLamports: 21_851_909}
	tx, err := send.Transaction([32]byte{9}, table)
	if err != nil {
		t.Fatal(err)
	}
	raw := tx.Serialize()
	if len(raw) > solana.MaxTransactionBytes {
		t.Fatal(len(raw))
	}
	parsed, err := solana.ParseTransaction(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.FeePayer() != send.Sender || len(parsed.RequiredSigners()) != 1 {
		t.Fatal("the sender must be the only signer")
	}
	resolved, err := parsed.ResolveInstructions([]*solana.LookupTable{table})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 3 || resolved[0].Program != solana.ComputeBudgetProgram || resolved[1].Program != solana.ComputeBudgetProgram || resolved[2].Program != OFTProgram {
		t.Fatalf("instructions %+v", resolved)
	}
	if hex.EncodeToString(resolved[2].Data) != fixture.DataHex {
		t.Fatal("the instruction data changed in compilation")
	}
	if len(resolved[2].Accounts) != len(fixture.Accounts) {
		t.Fatal(len(resolved[2].Accounts))
	}
	for i, account := range resolved[2].Accounts {
		want := fixture.Accounts[i]
		if account.Key.String() != want.Key || account.Signer != want.Signer || account.Writable != want.Writable {
			t.Fatalf("account %d: got %+v want %+v", i, account, want)
		}
	}
}
