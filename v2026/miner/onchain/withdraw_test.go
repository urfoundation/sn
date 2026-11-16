// CL-1: `snclaim withdraw --vault v1` runs through the real usage and command
// against a JSON-RPC fake of the chain and v1 vault, as the submit tests do.
// Selectors, error IDs and the ClaimPaid log are built with keccak256x.
package onchain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/urfoundation/sn/v2026/ss58"
)

const (
	withdrawTestVault  = "0x0102030405060708090a0b0c0d0e0f1011121314"
	withdrawTestChain  = 945
	withdrawTestCredit = 1_234_567_891 // rao read from claimCredit
	withdrawTestPaid   = 2_000_000_007 // rao in ClaimPaid: a later deferred claim grew the credit

	// The methods the fixture answers for a refusal after the credit read,
	// one after the preflight, and one finalized withdrawal.
	withdrawTestRead      = "eth_call=1 eth_chainId=1"
	withdrawTestPreflight = "eth_call=2 eth_chainId=1"
	withdrawTestSent      = "eth_call=2 eth_chainId=1 eth_estimateGas=1 eth_gasPrice=1 eth_getBlockByNumber=5 eth_getTransactionCount=1 eth_getTransactionReceipt=1 eth_sendRawTransaction=1"
)

// withdrawTestServer answers as the chain and the v1 vault: alice's credit
// read and the relayer's withdrawClaimCredit preflight, then one send, its
// receipt and finality, with the block identities of the submit fixture.
type withdrawTestServer struct {
	t               *testing.T
	server          *httptest.Server
	keyFile         string
	credit          []byte       // claimCredit(alice) return data; nil answers an RPC error
	revert          []byte       // withdrawClaimCredit preflight revert data; nil succeeds
	estimateRefused bool         // eth_estimateGas answers an RPC error
	logs            []*types.Log // the receipt's logs
	stateLock       sync.Mutex
	calls           map[string]int
	sent            []*types.Transaction
}

// Starts the fixture once configure has adjusted its defaults: alice's credit
// and a receipt carrying her ClaimPaid.
func newWithdrawTestServer(t *testing.T, configure func(*withdrawTestServer)) *withdrawTestServer {
	t.Helper()
	self := &withdrawTestServer{
		t:       t,
		keyFile: filepath.Join(t.TempDir(), "relayer.key"),
		credit:  word(withdrawTestCredit),
		logs:    []*types.Log{withdrawTestClaimPaid(common.HexToAddress(withdrawTestVault), aliceHex, withdrawTestPaid)},
		calls:   map[string]int{},
	}
	if err := os.WriteFile(self.keyFile, []byte(testKeyHex), 0o600); err != nil {
		t.Fatal(err)
	}
	if configure != nil {
		configure(self)
	}
	self.server = httptest.NewServer(http.HandlerFunc(self.serve))
	t.Cleanup(self.server.Close)
	return self
}

func (self *withdrawTestServer) serve(writer http.ResponseWriter, request *http.Request) {
	var call struct {
		Id     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		self.t.Error(err)
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.calls[call.Method]++
	relayer := common.HexToAddress(testKeyAddr)
	reply := map[string]any{"jsonrpc": "2.0", "id": call.Id}
	switch call.Method {
	case "eth_chainId":
		reply["result"] = hexutil.EncodeUint64(withdrawTestChain)
	case "eth_call", "eth_estimateGas":
		var message struct {
			From  common.Address `json:"from"`
			To    common.Address `json:"to"`
			Input hexutil.Bytes  `json:"input"`
		}
		if len(call.Params) == 0 || json.Unmarshal(call.Params[0], &message) != nil || message.To != common.HexToAddress(withdrawTestVault) {
			self.t.Errorf("%s does not address the vault: %s", call.Method, call.Params)
		}
		if call.Method == "eth_call" && (len(call.Params) != 2 || string(call.Params[1]) != `"latest"`) {
			self.t.Errorf("eth_call is not at the latest block: %s", call.Params)
		}
		switch {
		case call.Method == "eth_call" && bytes.Equal(message.Input, withdrawTestCalldata("claimCredit(bytes32)")):
			if self.credit == nil {
				reply["error"] = map[string]any{"code": -32000, "message": "synthetic credit read refusal"}
			} else {
				reply["result"] = hexutil.Bytes(self.credit)
			}
		case message.From != relayer || !bytes.Equal(message.Input, withdrawTestCalldata("withdrawClaimCredit(bytes32)")):
			self.t.Errorf("unexpected %s 0x%x from %s", call.Method, []byte(message.Input), message.From)
		case call.Method == "eth_estimateGas" && self.estimateRefused:
			reply["error"] = map[string]any{"code": -32000, "message": "synthetic estimate refusal"}
		case call.Method == "eth_estimateGas":
			reply["result"] = "0xc350"
		case self.revert != nil:
			reply["error"] = map[string]any{"code": 3, "message": "execution reverted", "data": hexutil.Bytes(self.revert)}
		default:
			reply["result"] = hexutil.Bytes(word(withdrawTestPaid))
		}
	case "eth_getTransactionCount":
		var account common.Address
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &account) != nil || account != relayer || string(call.Params[1]) != `"pending"` {
			self.t.Errorf("nonce is not the relayer's pending nonce: %s", call.Params)
		}
		reply["result"] = "0x7"
	case "eth_gasPrice":
		reply["result"] = "0x2"
	case "eth_sendRawTransaction":
		var raw hexutil.Bytes
		tx := new(types.Transaction)
		if len(call.Params) != 1 || json.Unmarshal(call.Params[0], &raw) != nil || tx.UnmarshalBinary(raw) != nil {
			self.t.Errorf("send lacks one signed transaction: %s", call.Params)
			return
		}
		self.sent = append(self.sent, tx)
		reply["result"] = tx.Hash()
	case "eth_getTransactionReceipt":
		if len(self.sent) != 1 {
			self.t.Errorf("receipt requested after %d sends", len(self.sent))
			return
		}
		hash := self.sent[0].Hash()
		logs := make([]*types.Log, len(self.logs))
		for index, original := range self.logs {
			entry := *original
			entry.TxHash, entry.BlockHash, entry.BlockNumber, entry.Index = hash, common.Hash{0x44}, 4, uint(index)
			logs[index] = &entry
		}
		reply["result"] = &types.Receipt{Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 50000, Logs: logs, TxHash: hash, GasUsed: 50000, EffectiveGasPrice: big.NewInt(2), BlockHash: common.Hash{0x44}, BlockNumber: big.NewInt(4)}
	case "eth_getBlockByNumber":
		var selector string
		if len(call.Params) != 2 || json.Unmarshal(call.Params[0], &selector) != nil {
			self.t.Errorf("block request has no selector: %s", call.Params)
		} else if selector == "0x4" {
			reply["result"] = syntheticBlockIdentityFixture(self.t, 4, common.Hash{0x44})
		} else if selector == "finalized" || selector == "0x6" {
			reply["result"] = syntheticBlockIdentityFixture(self.t, 6, common.Hash{0x66})
		} else {
			self.t.Errorf("unexpected block selector %q", selector)
		}
	default:
		self.t.Errorf("unexpected method %s", call.Method)
	}
	_ = json.NewEncoder(writer).Encode(reply)
}

// Every method answered so far with its count, in name order.
func (self *withdrawTestServer) observed() string {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	methods := make([]string, 0, len(self.calls))
	for method, count := range self.calls {
		methods = append(methods, fmt.Sprintf("%s=%d", method, count))
	}
	sort.Strings(methods)
	return strings.Join(methods, " ")
}

func (self *withdrawTestServer) transactions() []*types.Transaction {
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return slices.Clone(self.sent)
}

// A complete withdraw command for this fixture with alice's ss58 coldkey; each
// flag given replaces the flag of the same name or is appended.
func (self *withdrawTestServer) argv(flags ...string) []string {
	argv := []string{"withdraw", "--vault=v1", "--coldkey=" + aliceSS58, "--contract=" + withdrawTestVault, "--rpc=" + self.server.URL, "--key_file=" + self.keyFile}
	for _, flag := range flags {
		name, _, _ := strings.Cut(flag, "=")
		if index := slices.IndexFunc(argv, func(arg string) bool { return strings.HasPrefix(arg, name+"=") }); index >= 0 {
			argv[index] = flag
		} else {
			argv = append(argv, flag)
		}
	}
	return argv
}

// selector(signature) ‖ alice, built without stabi.
func withdrawTestCalldata(signature string) []byte {
	return append(slices.Clone(keccak256x([]byte(signature))[:4]), common.FromHex(aliceHex)...)
}

// ClaimPaid(coldkey, amount, relayer) from address, encoded by hand: the
// event ID, coldkey and relayer topics, then the amount word.
func withdrawTestClaimPaid(address common.Address, coldkey string, amount uint64) *types.Log {
	return &types.Log{Address: address, Topics: []common.Hash{
		common.BytesToHash(keccak256x([]byte("ClaimPaid(bytes32,uint256,address)"))),
		common.HexToHash(coldkey),
		common.BytesToHash(common.HexToAddress(testKeyAddr).Bytes()),
	}, Data: word(amount)}
}

// Parses argv with the real usage and runs the command. A read retry fails at
// once instead of waiting, so every refusal is the first answer's.
func withdrawTestRun(t *testing.T, argv []string) error {
	t.Helper()
	opts, err := parseArgv(t, argv)
	if err != nil {
		t.Fatalf("usage refused %q: %v", argv, err)
	}
	parent, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ctx, _ := onchainReadTestContext(t, parent, func(context.Context, time.Duration) error {
		return errors.New("synthetic withdraw fixture forbids a retry wait")
	})
	return cmdWithdraw(ctx, opts)
}

// CL-1: both option forms parse with a repeatable --rpc; --vault, --coldkey
// and the relayer --key_file are required.
func TestWithdrawUsage(t *testing.T) {
	spaced := []string{"withdraw", "--vault", "v1", "--coldkey", aliceSS58, "--contract", withdrawTestVault, "--rpc", "https://a.example", "--rpc", "https://b.example", "--key_file", "/tmp/k"}
	joined := []string{"withdraw", "--vault=v1", "--coldkey=0x" + aliceHex, "--contract=" + withdrawTestVault, "--rpc=https://a.example", "--rpc=https://b.example", "--key_file=/tmp/k", "--chain_id=945", "--gas_limit=90000", "--dry-run"}
	for _, tc := range []struct {
		argv    []string
		coldkey string
	}{{spaced, aliceSS58}, {joined, "0x" + aliceHex}} {
		opts, err := parseArgv(t, tc.argv)
		if err != nil || !boolOpt(opts, "withdraw") || boolOpt(opts, "submit") || boolOpt(opts, "status") || strOpt(opts, "--vault") != "v1" || strOpt(opts, "--coldkey") != tc.coldkey || len(strsOpt(opts, "--rpc")) != 2 {
			t.Fatalf("withdraw usage %q = %v, %v", tc.argv, opts, err)
		}
	}
	for _, required := range []string{"--vault", "--coldkey", "--key_file"} {
		argv := slices.DeleteFunc(slices.Clone(joined), func(arg string) bool { return strings.HasPrefix(arg, required+"=") })
		if _, err := parseArgv(t, argv); err == nil {
			t.Errorf("withdraw without %s parsed", required)
		}
	}
}

// CL-1: a zero credit at the latest block is refused before any estimate,
// nonce, fee, signature or send.
func TestWithdrawRefusesZeroCredit(t *testing.T) {
	fixture := newWithdrawTestServer(t, func(self *withdrawTestServer) { self.credit = word(0) })
	err := withdrawTestRun(t, fixture.argv())
	if err == nil || err.Error() != "coldkey "+aliceSS58+" (0x"+aliceHex+") has no v1 claim credit at the latest block; nothing sent" || fixture.observed() != withdrawTestRead {
		t.Fatalf("zero credit was not refused before signing: %v; observed %s", err, fixture.observed())
	}
}

// CL-1: a credit is withdrawn through the claims' lifecycle: the relayer signs
// one legacy withdrawClaimCredit(coldkey) to the vault at its pending nonce and
// the suggested fee, and the command returns after finality. ss58 and hex
// coldkeys are accepted; an explicit gas limit replaces the estimate's 20%
// headroom, and an unavailable estimate.
func TestWithdrawSendsOneFinalizedTransaction(t *testing.T) {
	for _, tc := range []struct {
		flags           []string
		estimateRefused bool
		gas             uint64
	}{
		{nil, false, 60_000}, // estimate 50,000 + 20%
		{[]string{"--coldkey=0x" + aliceHex, "--chain_id=945", "--gas_limit=90000"}, false, 90_000},
		{[]string{"--gas_limit=90000"}, true, 90_000},
	} {
		fixture := newWithdrawTestServer(t, func(self *withdrawTestServer) { self.estimateRefused = tc.estimateRefused })
		if err := withdrawTestRun(t, fixture.argv(tc.flags...)); err != nil || fixture.observed() != withdrawTestSent {
			t.Fatalf("withdrawal %q failed: %v; observed %s", tc.flags, err, fixture.observed())
		}
		sent := fixture.transactions()
		if len(sent) != 1 {
			t.Fatalf("withdrawal %q sent %d transactions", tc.flags, len(sent))
		}
		tx := sent[0]
		sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(withdrawTestChain)), tx)
		if err != nil || sender != common.HexToAddress(testKeyAddr) || tx.Type() != types.LegacyTxType || tx.ChainId().Cmp(big.NewInt(withdrawTestChain)) != 0 ||
			tx.To() == nil || *tx.To() != common.HexToAddress(withdrawTestVault) || !bytes.Equal(tx.Data(), withdrawTestCalldata("withdrawClaimCredit(bytes32)")) ||
			tx.Value().Sign() != 0 || tx.Nonce() != 7 || tx.GasPrice().Cmp(big.NewInt(2)) != 0 || tx.Gas() != tc.gas {
			t.Fatalf("withdrawal %q signed the wrong transaction: sender %s (%v) type %d chain %s to %v data 0x%x value %s nonce %d gasPrice %s gas %d",
				tc.flags, sender, err, tx.Type(), tx.ChainId(), tx.To(), tx.Data(), tx.Value(), tx.Nonce(), tx.GasPrice(), tx.Gas())
		}
	}
}

// CL-1: each reason the vault reverts a withdrawal is decoded by name from the
// preflight, and nothing is estimated, signed or sent.
func TestWithdrawDecodesRevert(t *testing.T) {
	for _, reason := range []string{"TransferBelowMinimum", "RuntimePriceUnavailable", "RuntimeTransferFailed", "RuntimeAccountingMismatch", "NothingToWithdraw"} {
		fixture := newWithdrawTestServer(t, func(self *withdrawTestServer) { self.revert = keccak256x([]byte(reason + "()"))[:4] })
		err := withdrawTestRun(t, fixture.argv())
		if err == nil || !strings.HasPrefix(err.Error(), "preflight eth_call failed: execution reverted: ") || !strings.Contains(err.Error(), "revert "+reason) || fixture.observed() != withdrawTestPreflight {
			t.Fatalf("%s was not decoded before signing: %v; observed %s", reason, err, fixture.observed())
		}
	}
}

// CL-1: a malformed coldkey is refused before any RPC.
func TestWithdrawRefusesMalformedColdkey(t *testing.T) {
	polkadotAlice, err := ss58.Encode([32]byte(common.FromHex(aliceHex)), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, coldkey := range []string{"", "not-a-key", "0x1234", "0x" + aliceHex + "ff", polkadotAlice, "0x" + strings.Repeat("00", 32)} {
		fixture := newWithdrawTestServer(t, nil)
		err := withdrawTestRun(t, fixture.argv("--coldkey="+coldkey))
		if err == nil || !strings.HasPrefix(err.Error(), "coldkey: ") || fixture.observed() != "" {
			t.Fatalf("coldkey %q was not refused before any RPC: %v; observed %s", coldkey, err, fixture.observed())
		}
	}
}

// CL-1: --vault accepts only v1; anything else is refused before any RPC.
func TestWithdrawRefusesUnknownVault(t *testing.T) {
	for _, vault := range []string{"v2", "v3", "V1", ""} {
		fixture := newWithdrawTestServer(t, nil)
		err := withdrawTestRun(t, fixture.argv("--vault="+vault))
		if err == nil || err.Error() != fmt.Sprintf("--vault: %q holds no claim credit (want v1)", vault) || fixture.observed() != "" {
			t.Fatalf("--vault=%s was not refused before any RPC: %v; observed %s", vault, err, fixture.observed())
		}
	}
}

// CL-1: a malformed vault address, chain id or gas limit, or a missing relayer
// key file, is refused before any RPC.
func TestWithdrawRefusesMalformedFlags(t *testing.T) {
	for _, tc := range []struct{ flag, refusal string }{
		{"--contract=0x1234", `--contract: "0x1234" is not a 20-byte hex address`},
		{"--chain_id=forty", `--chain_id: "forty" is not a non-negative integer`},
		{"--gas_limit=lots", "--gas_limit: "},
		{"--key_file=" + filepath.Join(t.TempDir(), "missing.key"), "key_file: "},
	} {
		fixture := newWithdrawTestServer(t, nil)
		err := withdrawTestRun(t, fixture.argv(tc.flag))
		if err == nil || !strings.HasPrefix(err.Error(), tc.refusal) || fixture.observed() != "" {
			t.Fatalf("%s was not refused before any RPC: %v; observed %s", tc.flag, err, fixture.observed())
		}
	}
}

// CL-1: an endpoint on another chain than --chain_id is refused before the
// credit read.
func TestWithdrawRefusesChainMismatch(t *testing.T) {
	fixture := newWithdrawTestServer(t, nil)
	err := withdrawTestRun(t, fixture.argv("--chain_id=964"))
	if err == nil || err.Error() != "chain id mismatch: --chain_id=964 but "+fixture.server.URL+" reports 945" || fixture.observed() != "eth_chainId=1" {
		t.Fatalf("chain mismatch was not refused before the read: %v; observed %s", err, fixture.observed())
	}
}

// CL-1: an unreachable endpoint, a refused credit read and a credit read that
// does not decode are each refused before anything is signed.
func TestWithdrawRefusesFailedCreditRead(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "synthetic endpoint refusal", http.StatusBadRequest)
	}))
	defer refusing.Close()
	fixture := newWithdrawTestServer(t, nil)
	if err := withdrawTestRun(t, fixture.argv("--rpc="+refusing.URL)); err == nil || !strings.HasPrefix(err.Error(), "no rpc endpoint reachable: ") || fixture.observed() != "" {
		t.Fatalf("unreachable endpoint was not refused: %v; observed %s", err, fixture.observed())
	}
	for _, credit := range [][]byte{nil, {}} {
		fixture := newWithdrawTestServer(t, func(self *withdrawTestServer) { self.credit = credit })
		err := withdrawTestRun(t, fixture.argv())
		if err == nil || !strings.HasPrefix(err.Error(), "claimCredit(0x"+aliceHex+"): ") || fixture.observed() != withdrawTestRead {
			t.Fatalf("credit read %x was not refused before signing: %v; observed %s", credit, err, fixture.observed())
		}
	}
}

// CL-1: --dry-run reads the credit, preflights and estimates, then stops
// before the nonce, fee, signature and send.
func TestWithdrawDryRunSendsNothing(t *testing.T) {
	fixture := newWithdrawTestServer(t, nil)
	if err := withdrawTestRun(t, fixture.argv("--dry-run")); err != nil || fixture.observed() != "eth_call=2 eth_chainId=1 eth_estimateGas=1" {
		t.Fatalf("dry run did not stop after the preflight: %v; observed %s", err, fixture.observed())
	}
}

// CL-1: a finalized withdrawal whose receipt has no ClaimPaid from the vault
// for this coldkey reports no payment and fails after its one send.
func TestWithdrawRequiresClaimPaid(t *testing.T) {
	vault := common.HexToAddress(withdrawTestVault)
	deferred := withdrawTestClaimPaid(vault, aliceHex, withdrawTestPaid)
	deferred.Topics = deferred.Topics[:2]
	deferred.Topics[0] = common.BytesToHash(keccak256x([]byte("ClaimPaymentDeferred(bytes32,uint256,uint256,uint64,uint8)")))
	for _, logs := range [][]*types.Log{
		nil,
		{withdrawTestClaimPaid(common.Address{0x99}, aliceHex, withdrawTestPaid)},
		{deferred},
		{withdrawTestClaimPaid(vault, strings.Repeat("11", 32), withdrawTestPaid)},
	} {
		fixture := newWithdrawTestServer(t, func(self *withdrawTestServer) { self.logs = logs })
		err := withdrawTestRun(t, fixture.argv())
		if err == nil || !strings.HasSuffix(err.Error(), " succeeded but "+vault.Hex()+" emitted no ClaimPaid for coldkey "+aliceSS58+" (0x"+aliceHex+")") || fixture.observed() != withdrawTestSent {
			t.Fatalf("receipt without alice's ClaimPaid reported a payment: %v; observed %s", err, fixture.observed())
		}
	}
}

// The child half of TestWithdrawRunReports: the snclaim entry point itself.
func TestWithdrawRunChild(t *testing.T) {
	if argv := os.Getenv("SNCLAIM_WITHDRAW_TEST_ARGV"); argv != "" {
		Run(strings.Split(argv, "\n"))
	}
}

// CL-1: the snclaim entry point dispatches withdraw: a paid credit prints the
// intent and the ClaimPaid coldkey, amount and relayer and exits 0; a zero
// credit prints its refusal and exits 1.
func TestWithdrawRunReports(t *testing.T) {
	coldkey := aliceSS58 + " (0x" + aliceHex + ")"
	for _, tc := range []struct {
		credit uint64
		exit   int
		sent   int
		want   []string
	}{
		{withdrawTestCredit, 0, 1, []string{
			"settlement-vault v1 credit withdrawal intent\n  contract:   " + common.HexToAddress(withdrawTestVault).Hex() + " (chain id 945, rpc ",
			"\n  from:       " + testKeyAddr + "\n  coldkey:    " + coldkey + "\n  credit:     1.234567891 α (1234567891 rao)\n  calldata:   36 bytes, selector 0xac399a93\n  gas (est):  50000\n",
			"\nClaimPaid: coldkey " + coldkey + "\n  paid:     2.000000007 α (2000000007 rao)\n  relayer:  " + testKeyAddr + "\n",
		}},
		{0, 1, 0, []string{"snclaim: coldkey " + coldkey + " has no v1 claim credit at the latest block; nothing sent\n"}},
	} {
		fixture := newWithdrawTestServer(t, func(self *withdrawTestServer) { self.credit = word(tc.credit) })
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWithdrawRunChild$", "-test.timeout=50s")
		cmd.Env = append(os.Environ(), "SNCLAIM_WITHDRAW_TEST_ARGV="+strings.Join(fixture.argv(), "\n"))
		output, err := cmd.CombinedOutput()
		cancel()
		exit := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exit = exitErr.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if exit != tc.exit || len(fixture.transactions()) != tc.sent {
			t.Fatalf("credit %d: exit %d with %d sends, want exit %d with %d:\n%s", tc.credit, exit, len(fixture.transactions()), tc.exit, tc.sent, output)
		}
		for _, want := range tc.want {
			if !strings.Contains(string(output), want) {
				t.Fatalf("credit %d: output lacks %q:\n%s", tc.credit, want, output)
			}
		}
	}
}
