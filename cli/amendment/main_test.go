package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/amendment"
	"github.com/urfoundation/sn/stabi"
)

// launchFile is the file before any entry is signed, as the repository first held it.
// Tests keep their own copy because the repository's file moves on with each entry.
const launchFile = `sn: 25
# Settlement amendments: changes to how SN25 settles from an epoch on, for one
# network operator, made without a new policy hash.
#
# Each entry is one line of plain text that names its chain, subnet, operator
# and first epoch and every term it binds, signed by the owner's key that signs
# price.yml. Only signed entries count, and a signed entry is permanent. An
# entry applies from its first epoch until a later entry of the same kind for
# the same operator takes effect. Version 1 is the extender pool (WHITEPAPER.md
# section 8.6), version 2 capture the flag (CAPTURETHEFLAG.md).
`

// testCarry stands in for the carry contract: EIP-55's own example address.
const testCarry = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"

const castTimeout = 2 * time.Minute

// extendersLine and ctfLine are written out here, not built by the package, so the
// tool is held to the lines the design gives.
func extendersLine(noID, epoch uint64) string {
	return fmt.Sprintf("UR settlement amendment v1: chain 964, sn 25, no %d, from epoch %d, intra-platform and intra-network extenders, units egress prefix, free and paid, zeta 1", noID, epoch)
}

func ctfLine(noID, epoch uint64) string {
	return fmt.Sprintf("UR settlement amendment v2: chain 964, sn 25, no %d, from epoch %d, capture the flag, ctf pool %d, carry %s, shares pool 4/10 ctf 1/10 head 5/10, head cedes to pool, slots 5 providers 5 extenders, countries iso 3166-1 249, presence half epoch, units egress prefix, draw drand quicknet 60s after census", noID, epoch, 1<<32+noID, testCarry)
}

type harness struct {
	key    *ecdsa.PrivateKey
	signer common.Address
	deps   dependencies
}

// newHarness pins a generated key in place of the owner's. Nothing here reaches the
// owner's key, and only a test can replace the pinned signer.
func newHarness(t testing.TB) *harness {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	signer := crypto.PubkeyToAddress(key.PublicKey)
	return &harness{key: key, signer: signer, deps: dependencies{signerFor: func(chainID uint64, netuid uint16) (common.Address, bool) {
		return signer, chainID == 964 && netuid == 25
	}}}
}

func (h *harness) run(args ...string) (string, error) {
	var stdout bytes.Buffer
	err := run(context.Background(), args, &stdout, h.deps)
	return stdout.String(), err
}

func (h *harness) sign(t testing.TB, line string) string {
	t.Helper()
	signature, err := crypto.Sign(accounts.TextHash([]byte(line)), h.key)
	if err != nil {
		t.Fatal(err)
	}
	signature[crypto.RecoveryIDOffset] += 27 // as a hardware wallet returns it
	return "0x" + hex.EncodeToString(signature)
}

func writeAmendments(t testing.TB, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "amendments.yml")
	if err := os.WriteFile(path, []byte(text), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func readAmendments(t testing.TB, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// message runs the command at a current epoch and returns the line it says to sign and
// its cast command.
func (h *harness) message(t testing.TB, path, line string, current uint64) (string, string) {
	t.Helper()
	out, err := h.run("message", "--in="+path, "--line="+line, fmt.Sprintf("--current-epoch=%d", current))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if lines[0] != "The line to sign:" || lines[1] != "" {
		t.Fatalf("unexpected output:\n%s", out)
	}
	for _, text := range lines {
		if strings.HasPrefix(text, "cast wallet sign ") {
			return lines[2], text
		}
	}
	t.Fatalf("no cast command in:\n%s", out)
	return "", ""
}

// attach signs what message prints and attaches it.
func (h *harness) attach(t testing.TB, path, line string, current uint64) string {
	t.Helper()
	printed, _ := h.message(t, path, line, current)
	if printed != line {
		t.Fatalf("message printed %q for %q", printed, line)
	}
	signature := h.sign(t, line)
	if out, err := h.run("attach", "--in="+path, "--line="+line, fmt.Sprintf("--current-epoch=%d", current), "--signature="+signature); err != nil {
		t.Fatal(out, err)
	}
	return signature
}

func entryText(line, signature string) string {
	return "  - line: \"" + line + "\"\n    signature: \"" + signature + "\"\n"
}

// The repository's own amendments.yml, whatever it holds by now, verifies under the
// real pinned signer. It is the file the site publishes.
func TestTheRepositoryFileVerifies(t *testing.T) {
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"verify", "--in=../../amendments.yml"}, &stdout, dependencies{signerFor: amendment.SignerFor}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "signer 0x16C372dbBb24cd8473345ab13971E40814C8658F") {
		t.Fatal(stdout.String())
	}
}

// The launch file verifies, says nothing is in force, and is left alone.
func TestTheLaunchFileVerifies(t *testing.T) {
	path := writeAmendments(t, launchFile)
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"verify", "--in=" + path, "--current-epoch=7"}, &stdout, dependencies{signerFor: amendment.SignerFor}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "No signed entries: no signed amendment is in force.") {
		t.Fatal(stdout.String())
	}
	if readAmendments(t, path) != launchFile {
		t.Fatal("verify changed the file")
	}
}

func TestMessagePrintsTheLineAndTheCommand(t *testing.T) {
	path := writeAmendments(t, launchFile)
	line := ctfLine(1, 8)
	var stdout bytes.Buffer
	args := []string{"message", "--in=" + path, "--line=" + line, "--current-epoch=7", "--ledger-path=m/44'/60'/7'/0/0"}
	if err := run(context.Background(), args, &stdout, dependencies{signerFor: amendment.SignerFor}); err != nil {
		t.Fatal(err)
	}
	want := "The line to sign:\n\n" + line + "\n\n" +
		"The current epoch, as given, is 7. Epoch 8 has not started.\n\n" +
		"Sign it with the Ledger's Ethereum app open. The device must show exactly this line,\n" +
		"and the key is 0x16C372dbBb24cd8473345ab13971E40814C8658F:\n\n" +
		`cast wallet sign --ledger --mnemonic-derivation-path "m/44'/60'/7'/0/0" "` + line + "\"\n\n" +
		"Then attach the signature it prints, giving the current epoch again (or --rpc), since\n" +
		"attach also refuses an amendment whose first epoch has started:\n\n" +
		"go run ./cli/amendment attach --in=" + path + " --current-epoch=N --signature=0x... --line=\"" + line + "\"\n"
	if stdout.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", stdout.String(), want)
	}
	if readAmendments(t, path) != launchFile {
		t.Fatal("message changed the file")
	}
}

// The owner key's path stays out of this code, so without the flag the printed command
// has a blank that cast cannot read as a path, and the output says to fill it.
func TestMessageWithoutALedgerPathLeavesABlank(t *testing.T) {
	path := writeAmendments(t, launchFile)
	var stdout bytes.Buffer
	args := []string{"message", "--in=" + path, "--line=" + extendersLine(1, 8), "--current-epoch=7"}
	if err := run(context.Background(), args, &stdout, dependencies{signerFor: amendment.SignerFor}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if !strings.Contains(out, `--mnemonic-derivation-path "<ledger-path>" "`) || !strings.Contains(out, "Put the owner key's derivation path in place of <ledger-path>") {
		t.Fatal(out)
	}
}

// Whatever the flag holds is printed into a command to run, so only a path gets through.
func TestMessageRefusesWhatIsNotALedgerPath(t *testing.T) {
	path := writeAmendments(t, launchFile)
	for _, bad := range []string{`m/44'/60'/0'/0/0" --private-key "0x1`, "44'/60'/0'/0/0", "m/", "m", "m/44'/$(id)", "m/44''/0"} {
		var stdout bytes.Buffer
		args := []string{"message", "--in=" + path, "--line=" + extendersLine(1, 8), "--current-epoch=7", "--ledger-path=" + bad}
		if err := run(context.Background(), args, &stdout, dependencies{signerFor: amendment.SignerFor}); err == nil {
			t.Fatalf("took %q:\n%s", bad, stdout.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("printed for %q:\n%s", bad, stdout.String())
		}
	}
}

// An amendment is signed before its first epoch starts: message, attach and sign all
// refuse a first epoch at or before the current one, and need to be told the current
// epoch to judge it.
func TestAStartedEpochIsRefused(t *testing.T) {
	h := newHarness(t)
	keyFile := writeKeyFile(t, h.key)
	path := writeAmendments(t, launchFile)
	line := ctfLine(1, 8)
	signature := h.sign(t, line)
	commands := map[string][]string{
		"message": {"message", "--in=" + path, "--line=" + line},
		"attach":  {"attach", "--in=" + path, "--line=" + line, "--signature=" + signature},
		"sign":    {"sign", "--in=" + path, "--line=" + line, "--key-file=" + keyFile},
	}
	for name, command := range commands {
		for _, current := range []string{"8", "9", "1000", "18446744073709551615"} {
			out, err := h.run(append(slices.Clone(command), "--current-epoch="+current)...)
			if err == nil || !strings.Contains(err.Error(), "epoch 8 has started") {
				t.Fatalf("%s at epoch %s: %v\n%s", name, current, err, out)
			}
			if out != "" {
				t.Fatalf("%s at epoch %s printed:\n%s", name, current, out)
			}
		}
		if _, err := h.run(append(slices.Clone(command), "--current-epoch=8")...); err == nil || !strings.Contains(err.Error(), "the earliest is 9") {
			t.Fatalf("%s: %v", name, err)
		}
		for what, args := range map[string][]string{
			"no current epoch": nil,
			"both":             {"--current-epoch=7", "--rpc=http://127.0.0.1:1"},
			"07":               {"--current-epoch=07"},
			"0x7":              {"--current-epoch=0x7"},
			"-1":               {"--current-epoch=-1"},
			"7.0":              {"--current-epoch=7.0"},
			"empty":            {"--current-epoch="},
			"past uint64":      {"--current-epoch=18446744073709551616"},
		} {
			if out, err := h.run(append(slices.Clone(command), args...)...); err == nil {
				t.Fatalf("%s with %s: %s", name, what, out)
			}
		}
		if readAmendments(t, path) != launchFile {
			t.Fatalf("a refused %s changed the file", name)
		}
	}
	// The epoch before is in time, and the first epoch of all is never.
	if _, err := h.run(append(slices.Clone(commands["message"]), "--current-epoch=7")...); err != nil {
		t.Fatal(err)
	}
	zero := extendersLine(1, 0)
	if _, err := h.run("message", "--in="+path, "--line="+zero, "--current-epoch=0"); err == nil {
		t.Fatal("offered to sign for epoch 0 during epoch 0")
	}
	if _, err := h.run(append(slices.Clone(commands["attach"]), "--current-epoch=7")...); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readAmendments(t, path), line) {
		t.Fatal("the entry in time was not attached")
	}
}

// mainnetEpochStart is the coordinator's first block of epoch e on mainnet: weekly
// 50,400-block epochs from epoch 1 at block 9,247,228.
func mainnetEpochStart(epoch uint64) uint64 {
	return 9_247_228 + 50_400*(epoch-1)
}

// fakeChain answers as the coordinator's RPC does and checks what it is asked. It is a
// local stand-in: no test reaches a chain. Its latest block is 1,000 blocks into the
// current epoch and headAge old, or 6 seconds old when none is given.
func fakeChain(t *testing.T, chainID string, currentEpoch *uint64, headAge ...time.Duration) *httptest.Server {
	t.Helper()
	age := 6 * time.Second
	if len(headAge) > 0 {
		age = headAge[0]
	}
	coordinator := stabi.NewSTCoordinator()
	word := func(value uint64) string {
		return "0x" + hex.EncodeToString(common.LeftPadBytes(new(big.Int).SetUint64(value).Bytes(), 32))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		var result any
		switch request.Method {
		case "eth_chainId":
			result = chainID
		case "eth_getBlockByNumber":
			if len(request.Params) == 0 || string(request.Params[0]) != `"latest"` {
				t.Errorf("asked for block %s", request.Params)
			}
			head := uint64(9_000_000)
			if currentEpoch != nil && *currentEpoch > 0 {
				head = mainnetEpochStart(*currentEpoch) + 1_000
			}
			result = map[string]string{"number": hexutil.EncodeUint64(head), "timestamp": hexutil.EncodeUint64(uint64(time.Now().Add(-age).Unix()))}
		case "eth_call":
			var call struct {
				To    string `json:"to"`
				Input string `json:"input"`
				Data  string `json:"data"`
			}
			if err := json.Unmarshal(request.Params[0], &call); err != nil {
				t.Error(err)
			}
			if !strings.EqualFold(call.To, "0xC18925925E2B7bb9059b7d696b8c92762AE86406") {
				t.Errorf("called %s", call.To)
			}
			data, err := hexutil.Decode(call.Input + call.Data)
			if err != nil {
				t.Error(err)
			}
			result = "0x"
			switch {
			case currentEpoch == nil:
			case bytes.Equal(data, coordinator.PackCurrentEpoch()):
				result = word(*currentEpoch)
			case len(data) == 36 && bytes.Equal(data[:4], coordinator.PackEpochStartBlock(big.NewInt(0))[:4]):
				result = word(mainnetEpochStart(new(big.Int).SetBytes(data[4:]).Uint64()))
			default:
				t.Errorf("called %x", data)
			}
		default:
			t.Errorf("unexpected method %s", request.Method)
		}
		encoded, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(encoded)
	}))
	t.Cleanup(server.Close)
	return server
}

// The current epoch can come from the coordinator instead, and a chain that moves on
// while the owner signs refuses at attach.
func TestTheCurrentEpochFromTheChain(t *testing.T) {
	h := newHarness(t)
	path := writeAmendments(t, launchFile)
	current := uint64(7)
	chain := fakeChain(t, "0x3c4", &current)
	line := ctfLine(1, 8)
	out, err := h.run("message", "--in="+path, "--line="+line, "--rpc="+chain.URL)
	want := "The current epoch, read from the chain at block 9550628, is 7. Epoch 8 has not started: it starts at block 9600028, 49400 blocks from now, about 6.9 days at 12 s a block. Publish the entry before then."
	if err != nil || !strings.Contains(out, want) {
		t.Fatal(out, err)
	}
	// sign reads it too.
	signPath := writeAmendments(t, launchFile)
	if out, err := h.run("sign", "--in="+signPath, "--line="+line, "--key-file="+writeKeyFile(t, h.key), "--rpc="+chain.URL); err != nil {
		t.Fatal(out, err)
	}
	current = 8
	attach := []string{"attach", "--in=" + path, "--line=" + line, "--signature=" + h.sign(t, line), "--rpc=" + chain.URL}
	if out, err := h.run(attach...); err == nil || !strings.Contains(err.Error(), "epoch 8 has started") {
		t.Fatal(out, err)
	}
	if out, err := h.run("sign", "--in="+path, "--line="+line, "--key-file="+writeKeyFile(t, h.key), "--rpc="+chain.URL); err == nil || !strings.Contains(err.Error(), "epoch 8 has started") {
		t.Fatal(out, err)
	}
	if readAmendments(t, path) != launchFile {
		t.Fatal("a refused attach changed the file")
	}
	// A node behind the chain could report an epoch that has ended, so its answer is
	// no answer.
	current = 7
	behind := fakeChain(t, "0x3c4", &current, time.Hour)
	for _, args := range [][]string{
		{"message", "--in=" + path, "--line=" + line, "--rpc=" + behind.URL},
		{"attach", "--in=" + path, "--line=" + line, "--signature=" + h.sign(t, line), "--rpc=" + behind.URL},
		{"sign", "--in=" + path, "--line=" + line, "--key-file=" + writeKeyFile(t, h.key), "--rpc=" + behind.URL},
		{"verify", "--in=" + path, "--rpc=" + behind.URL},
	} {
		if out, err := h.run(args...); err == nil || !strings.Contains(err.Error(), "is behind") || out != "" {
			t.Fatalf("%s: %v\n%s", args[0], err, out)
		}
	}
	if readAmendments(t, path) != launchFile {
		t.Fatal("a refused attach changed the file")
	}
	// The epoch is read only from the coordinator of the line's own deployment.
	elsewhere := &harness{key: h.key, signer: h.signer, deps: dependencies{signerFor: func(uint64, uint16) (common.Address, bool) { return h.signer, true }}}
	otherDeployment := strings.Replace(line, "chain 964", "chain 945", 1)
	if out, err := elsewhere.run("message", "--in="+path, "--line="+otherDeployment, "--rpc="+chain.URL); err == nil || !strings.Contains(err.Error(), "no coordinator is known") {
		t.Fatal(out, err)
	}
	// A chain that cannot answer is no answer: nothing is printed for it.
	wrongChain := fakeChain(t, "0x3b1", &current)
	noCoordinator := fakeChain(t, "0x3c4", nil)
	closed := fakeChain(t, "0x3c4", &current)
	closed.Close()
	for name, endpoint := range map[string]string{"another chain": wrongChain.URL, "no coordinator": noCoordinator.URL, "down": closed.URL, "not a url": "::"} {
		if out, err := h.run("message", "--in="+path, "--line="+ctfLine(1, 50), "--rpc="+endpoint); err == nil || out != "" {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		if out, err := h.run("verify", "--in="+path, "--rpc="+endpoint); err == nil || out != "" {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
	}
}

// The first entry adds the section at the end, and the rest of the file is untouched.
func TestAttachAddsTheFirstEntry(t *testing.T) {
	h := newHarness(t)
	path := writeAmendments(t, launchFile)
	line := extendersLine(1, 8)
	signature := h.sign(t, line)
	out, err := h.run("attach", "--in="+path, "--line="+line, "--current-epoch=7", "--signature="+signature)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, line) || !strings.Contains(out, "signature "+signature) {
		t.Fatal(out)
	}
	want := launchFile + "signed_amendments:\n" + entryText(line, signature)
	if got := readAmendments(t, path); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatal(info.Mode(), err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	if out, err := h.run("verify", "--in="+path); err != nil || !strings.Contains(out, line) {
		t.Fatal(out, err)
	}
	// The same entry again is at the newest epoch of its kind like any other, and is
	// refused before anyone is asked to sign it.
	if out, err := h.run("attach", "--in="+path, "--line="+line, "--current-epoch=7", "--signature="+signature); err == nil || !strings.Contains(err.Error(), placeRefusal) {
		t.Fatal(out, err)
	}
	if out, err := h.run("message", "--in="+path, "--line="+line, "--current-epoch=7"); err == nil || !strings.Contains(err.Error(), placeRefusal) {
		t.Fatal(out, err)
	}
	if readAmendments(t, path) != want {
		t.Fatal("attaching the same entry changed the file")
	}
}

// Each later entry goes right after the last one. Every earlier entry and every
// comment stays byte for byte, whatever the list's margin.
func TestAttachKeepsEveryEarlierByte(t *testing.T) {
	h := newHarness(t)
	for name, start := range map[string]string{
		"at the end of the file": launchFile,
		"no final newline":       strings.TrimSuffix(launchFile, "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			path := writeAmendments(t, start)
			h.attach(t, path, extendersLine(1, 8), 7)
			first := readAmendments(t, path)
			// The owner's own notes beside the first entry.
			noted := strings.Replace(first, "signed_amendments:\n", "signed_amendments:\n  # the extender pool, E_x\n", 1)
			noted += "# after the entries\n"
			if err := os.WriteFile(path, []byte(noted), 0o640); err != nil {
				t.Fatal(err)
			}
			// Another kind at the same epoch, then a later entry of that kind.
			for _, line := range []string{ctfLine(1, 8), ctfLine(1, 12)} {
				before := readAmendments(t, path)
				signature := h.attach(t, path, line, 7)
				after := readAmendments(t, path)
				block := entryText(line, signature)
				at := strings.Index(before, "# after the entries")
				if after != before[:at]+block+before[at:] {
					t.Fatalf("the entry is not right after the last one, or other bytes changed:\n%s", after)
				}
			}
			out, err := h.run("verify", "--in="+path, "--current-epoch=12")
			if err != nil || strings.Count(out, "UR settlement amendment") != 3 {
				t.Fatal(out, err)
			}
			for _, want := range []string{
				"  no 1, the extender pool: the entry from epoch 8 applies.\n",
				"  no 1, capture the flag: the entry from epoch 12 applies.\n",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("no %q in:\n%s", want, out)
				}
			}
		})
	}
	// A list at the margin gets its entries at the margin.
	t.Run("a list at the margin", func(t *testing.T) {
		path := writeAmendments(t, launchFile)
		h.attach(t, path, extendersLine(1, 8), 7)
		margin := strings.ReplaceAll(strings.ReplaceAll(readAmendments(t, path), "  - line", "- line"), "    signature", "  signature")
		if err := os.WriteFile(path, []byte(margin), 0o640); err != nil {
			t.Fatal(err)
		}
		line := ctfLine(1, 8)
		signature := h.attach(t, path, line, 7)
		if got, want := readAmendments(t, path), margin+"- line: \""+line+"\"\n  signature: \""+signature+"\"\n"; got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
}

// malleate returns the other signature of the same message by the same key.
func malleate(t *testing.T, signature string) string {
	t.Helper()
	raw, err := hex.DecodeString(strings.TrimPrefix(signature, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	order := crypto.S256().Params().N
	s := new(big.Int).Sub(order, new(big.Int).SetBytes(raw[32:64]))
	copy(raw[32:64], common.LeftPadBytes(s.Bytes(), 32))
	raw[64] = 27 + 28 - raw[64]
	return "0x" + hex.EncodeToString(raw)
}

// A signature whose s is in the upper half is the same signature: attach takes it and
// stores the form readers take.
func TestAttachStoresTheLowerS(t *testing.T) {
	h := newHarness(t)
	path := writeAmendments(t, launchFile)
	line := ctfLine(1, 8)
	signature := h.sign(t, line)
	if out, err := h.run("attach", "--in="+path, "--line="+line, "--current-epoch=7", "--signature="+malleate(t, signature)); err != nil {
		t.Fatal(out, err)
	}
	if got := readAmendments(t, path); got != launchFile+"signed_amendments:\n"+entryText(line, signature) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestAttachRefuses(t *testing.T) {
	h := newHarness(t)
	other := newHarness(t)
	path := writeAmendments(t, launchFile)
	first := extendersLine(1, 8)
	firstSignature := h.attach(t, path, first, 7)
	current := readAmendments(t, path)
	line := ctfLine(1, 8)
	good := h.sign(t, line)
	attach := func(line, signature string) []string {
		return []string{"attach", "--in=" + path, "--line=" + line, "--current-epoch=7", "--signature=" + signature}
	}
	for name, args := range map[string][]string{
		"another signer":       attach(line, other.sign(t, line)),
		"another line":         attach(ctfLine(1, 9), good),
		"another operator":     attach(ctfLine(2, 8), good),
		"no signature":         {"attach", "--in=" + path, "--line=" + line, "--current-epoch=7"},
		"no line":              {"attach", "--in=" + path, "--current-epoch=7", "--signature=" + good},
		"no file":              {"attach", "--line=" + line, "--current-epoch=7", "--signature=" + good},
		"a missing file":       {"attach", "--in=" + path + ".gone", "--line=" + line, "--current-epoch=7", "--signature=" + good},
		"short signature":      attach(line, good[:len(good)-2]),
		"long signature":       attach(line, good+"00"),
		"no 0x":                attach(line, good[2:]),
		"not hex":              attach(line, "0x"+strings.Repeat("zz", 65)),
		"bad recovery byte":    attach(line, good[:len(good)-2]+"1d"),
		"raw recovery byte":    attach(line, good[:len(good)-2]+"00"),
		"text mode signature":  attach(line, h.sign(t, "0x"+hex.EncodeToString([]byte(line)))),
		"a near miss":          attach(strings.Replace(line, "60s", "61s", 1), h.sign(t, strings.Replace(line, "60s", "61s", 1))),
		"the design's carry":   attach(strings.Replace(line, testCarry, "0x<carry contract>", 1), good),
		"a carry in lowercase": attach(strings.Replace(line, testCarry, strings.ToLower(testCarry), 1), h.sign(t, strings.Replace(line, testCarry, strings.ToLower(testCarry), 1))),
		"bytes not text": attach(line, func() string {
			sig, _ := crypto.Sign(crypto.Keccak256([]byte(line)), h.key)
			sig[64] += 27
			return "0x" + hex.EncodeToString(sig)
		}()),
		"no pinned signer": attach(strings.Replace(line, "chain 964", "chain 945", 1), h.sign(t, strings.Replace(line, "chain 964", "chain 945", 1))),
		"another subnet":   attach(strings.Replace(line, "sn 25", "sn 26", 1), h.sign(t, strings.Replace(line, "sn 25", "sn 26", 1))),
		"positional":       append(attach(line, good), "extra"),
		"unknown flag":     append(attach(line, good), "--force"),
		"verify's flag":    append(attach(line, good), "--url=http://example.invalid"),
	} {
		if out, err := h.run(args...); err == nil {
			t.Fatalf("%s: %s", name, out)
		}
		if readAmendments(t, path) != current {
			t.Fatalf("%s changed the file", name)
		}
	}
	// An existing entry never changes, and none comes before one of its kind and
	// operator, whoever signed it.
	for name, args := range map[string][]string{
		"the same entry":        attach(first, firstSignature),
		"the same, malleated":   attach(first, malleate(t, firstSignature)),
		"an earlier entry":      attach(extendersLine(1, 7), h.sign(t, extendersLine(1, 7))),
		"the same, by another":  attach(first, other.sign(t, first)),
		"an earlier, offered":   {"message", "--in=" + path, "--line=" + extendersLine(1, 6), "--current-epoch=5"},
		"the same, offered":     {"message", "--in=" + path, "--line=" + first, "--current-epoch=7"},
		"the same, by key file": {"sign", "--in=" + path, "--line=" + first, "--current-epoch=7", "--key-file=" + writeKeyFile(t, h.key)},
	} {
		if out, err := h.run(args...); err == nil || !strings.Contains(err.Error(), placeRefusal) {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		if readAmendments(t, path) != current {
			t.Fatalf("%s changed the file", name)
		}
	}
	// Nor does any entry come before the newest in the file, of whatever kind or
	// operator: a reader holding that one may have decided the epochs between.
	for name, args := range map[string][]string{
		"another operator, earlier":  {"message", "--in=" + path, "--line=" + extendersLine(2, 7), "--current-epoch=6"},
		"another kind, earlier":      attach(ctfLine(1, 7), h.sign(t, ctfLine(1, 7))),
		"another operator's, signed": attach(ctfLine(2, 5), h.sign(t, ctfLine(2, 5))),
	} {
		args[len(args)-1] = strings.Replace(args[len(args)-1], "--current-epoch=7", "--current-epoch=4", 1)
		if out, err := h.run(args...); err == nil || !strings.Contains(err.Error(), placeRefusal) {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
	}
	if readAmendments(t, path) != current {
		t.Fatal("a refused entry changed the file")
	}
	// Another operator's entry as late as the newest is its own series.
	if _, err := h.run("message", "--in="+path, "--line="+extendersLine(2, 8), "--current-epoch=7"); err != nil {
		t.Fatal(err)
	}
	// A tampered earlier entry stops everything: nothing new joins a bad file.
	tampered := strings.Replace(current, "from epoch 8", "from epoch 9", 1)
	if err := os.WriteFile(path, []byte(tampered), 0o640); err != nil {
		t.Fatal(err)
	}
	if out, err := h.run(attach(line, good)...); err == nil {
		t.Fatal(out)
	}
	if out, err := h.run("message", "--in="+path, "--line="+line, "--current-epoch=7"); err == nil {
		t.Fatal(out)
	}
	if readAmendments(t, path) != tampered {
		t.Fatal("a refused attach changed the file")
	}
	// Layouts it cannot add lines to, or files it cannot take, are refused rather than
	// rewritten.
	for name, file := range map[string]string{
		"flow list":         launchFile + "signed_amendments: []\n",
		"carriage returns":  strings.ReplaceAll(current, "\n", "\r\n"),
		"misspelt section":  strings.Replace(current, "signed_amendments:", "signed_amendment:", 1),
		"an unknown field":  current + "notes: hello\n",
		"another subnet":    strings.Replace(current, "sn: 25", "sn: 26", 1),
		"not a file at all": "<html>",
	} {
		path := writeAmendments(t, file)
		if out, err := h.run("attach", "--in="+path, "--line="+line, "--current-epoch=7", "--signature="+good); err == nil {
			t.Fatalf("%s: %s", name, out)
		}
		if readAmendments(t, path) != file {
			t.Fatalf("%s changed the file", name)
		}
	}
}

// Layout and write failures never reach the file.
func TestAFailedWriteLeavesTheFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	h := newHarness(t)
	path := writeAmendments(t, launchFile)
	line := ctfLine(1, 8)
	directory := filepath.Dir(path)
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(directory, 0o700)
	if out, err := h.run("attach", "--in="+path, "--line="+line, "--current-epoch=7", "--signature="+h.sign(t, line)); err == nil {
		t.Fatal(out)
	}
	if readAmendments(t, path) != launchFile {
		t.Fatal("a failed write changed the file")
	}
}

func writeKeyFile(t testing.TB, key *ecdsa.PrivateKey) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "signer.key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(crypto.FromECDSA(key))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// sign signs with a key held in a file, only the pinned signer's, and attaches the
// entry as attach would. The keys here are generated; none is the owner's.
func TestSignWithAKeyFile(t *testing.T) {
	h := newHarness(t)
	other := newHarness(t)
	path := writeAmendments(t, launchFile)
	line := ctfLine(1, 8)
	out, err := h.run("sign", "--in="+path, "--line="+line, "--current-epoch=7", "--key-file="+writeKeyFile(t, h.key))
	if err != nil {
		t.Fatal(out, err)
	}
	signature := h.sign(t, line) // deterministic, as the Ledger's is
	if !strings.HasPrefix(out, "Signed and attached to "+path) || !strings.Contains(out, "signature "+signature) {
		t.Fatal(out)
	}
	if got := readAmendments(t, path); got != launchFile+"signed_amendments:\n"+entryText(line, signature) {
		t.Fatalf("got:\n%s", got)
	}
	if out, err := h.run("verify", "--in="+path, "--current-epoch=8"); err != nil || !strings.Contains(out, "no 1, capture the flag: the entry from epoch 8 applies.") {
		t.Fatal(out, err)
	}
	signedFile := readAmendments(t, path)
	keyDirectory := t.TempDir()
	badKey := func(name, text string) string {
		path := filepath.Join(keyDirectory, name)
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	next := ctfLine(1, 9)
	for name, args := range map[string][]string{
		"another key":        {"--key-file=" + writeKeyFile(t, other.key)},
		"no key file":        nil,
		"a missing key file": {"--key-file=" + filepath.Join(keyDirectory, "gone")},
		"an empty key file":  {"--key-file=" + badKey("empty", "")},
		"a short key":        {"--key-file=" + badKey("short", strings.Repeat("ab", 31))},
		"not hex":            {"--key-file=" + badKey("text", strings.Repeat("zz", 32))},
		"the curve order":    {"--key-file=" + badKey("order", hex.EncodeToString(crypto.S256().Params().N.Bytes()))},
		"a started epoch":    {"--key-file=" + writeKeyFile(t, h.key), "--current-epoch=9"},
	} {
		args := append([]string{"sign", "--in=" + path, "--line=" + next}, args...)
		if !slices.ContainsFunc(args, func(arg string) bool { return strings.HasPrefix(arg, "--current-epoch=") }) {
			args = append(args, "--current-epoch=7")
		}
		if out, err := h.run(args...); err == nil {
			t.Fatalf("%s: %s", name, out)
		}
		if readAmendments(t, path) != signedFile {
			t.Fatalf("%s changed the file", name)
		}
	}
}

func signedFile(t *testing.T, h *harness) string {
	t.Helper()
	path := writeAmendments(t, launchFile)
	h.attach(t, path, extendersLine(1, 8), 7)
	h.attach(t, path, ctfLine(1, 8), 7)
	h.attach(t, path, ctfLine(1, 12), 7)
	return readAmendments(t, path)
}

// forge swaps the first entry's signature for a good signature of the same line by
// someone else.
func forge(t *testing.T, other *harness, file string) string {
	t.Helper()
	parsed, err := amendment.ParseFile([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	first := parsed.Entries()[0]
	line, err := first.Line()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Replace(file, hex.EncodeToString(first.Signature), strings.TrimPrefix(other.sign(t, line), "0x"), 1)
}

func TestVerifyRefuses(t *testing.T) {
	h := newHarness(t)
	other := newHarness(t)
	good := signedFile(t, h)
	if out, err := h.run("verify", "--in="+writeAmendments(t, good)); err != nil {
		t.Fatal(out, err)
	}
	// Under the owner's real pinned key, a generated key's signature is no signature.
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"verify", "--in=" + writeAmendments(t, good)}, &stdout, dependencies{signerFor: amendment.SignerFor})
	if err == nil || !strings.Contains(err.Error(), h.signer.Hex()) || !strings.Contains(err.Error(), "0x16C372dbBb24cd8473345ab13971E40814C8658F") {
		t.Fatal(stdout.String(), err)
	}
	for name, file := range map[string]string{
		"an entry by another key":    forge(t, other, good),
		"a changed line":             strings.Replace(good, "from epoch 12", "from epoch 11", 1),
		"unknown top-level field":    good + "extra: 1\n",
		"misspelt section":           strings.Replace(good, "signed_amendments:", "signed_amendmentss:", 1),
		"another subnet":             strings.Replace(good, "sn: 25", "sn: 26", 1),
		"an entry with no signature": regexp.MustCompile(`(?m)^    signature: .*\n`).ReplaceAllString(good, ""),
		"not a file":                 "<html>",
		"empty":                      "",
		"two documents":              good + "---\nsn: 25\n",
	} {
		if out, err := h.run("verify", "--in="+writeAmendments(t, file)); err == nil {
			t.Fatalf("%s: %s", name, out)
		}
	}
	path := writeAmendments(t, good)
	for name, args := range map[string][]string{
		"neither":    {"verify"},
		"both":       {"verify", "--in=" + path, "--url=http://example.invalid"},
		"bad chain":  {"verify", "--in=" + path, "--chain=0x3c4"},
		"testnet":    {"verify", "--in=" + path, "--chain=945"},
		"positional": {"verify", path},
		"a line":     {"verify", "--in=" + path, "--line=" + ctfLine(1, 8)},
		"bad epoch":  {"verify", "--in=" + path, "--current-epoch=x"},
	} {
		if out, err := h.run(args...); err == nil {
			t.Fatalf("%s: %s", name, out)
		}
	}
	if _, err := h.run("sign2", "--in="+path); err == nil {
		t.Fatal("ran an unknown command")
	}
	for _, help := range [][]string{nil, {"help"}, {"--help"}, {"-h"}, {"attach", "--help"}, {"sign", "-h"}} {
		out, err := h.run(help...)
		if err != nil || !strings.Contains(out, "amendment verify") {
			t.Fatal(help, out, err)
		}
	}
}

func TestVerifySaysWhatAppliesNow(t *testing.T) {
	h := newHarness(t)
	path := writeAmendments(t, signedFile(t, h))
	out, err := h.run("verify", "--in="+path)
	if err != nil || !strings.Contains(out, "Pass --current-epoch or --rpc to see which entries apply now.") || strings.Count(out, "UR settlement amendment") != 3 {
		t.Fatal(out, err)
	}
	for current, want := range map[uint64]string{
		0:  "Current epoch 0:\n  no 1, the extender pool: none applies yet. The entry from epoch 8 is next.\n  no 1, capture the flag: none applies yet. The entry from epoch 8 is next.\n",
		7:  "Current epoch 7:\n  no 1, the extender pool: none applies yet. The entry from epoch 8 is next.\n  no 1, capture the flag: none applies yet. The entry from epoch 8 is next.\n",
		8:  "Current epoch 8:\n  no 1, the extender pool: the entry from epoch 8 applies.\n  no 1, capture the flag: the entry from epoch 8 applies. The entry from epoch 12 is next.\n",
		11: "Current epoch 11:\n  no 1, the extender pool: the entry from epoch 8 applies.\n  no 1, capture the flag: the entry from epoch 8 applies. The entry from epoch 12 is next.\n",
		12: "Current epoch 12:\n  no 1, the extender pool: the entry from epoch 8 applies.\n  no 1, capture the flag: the entry from epoch 12 applies.\n",
	} {
		out, err := h.run("verify", "--in="+path, fmt.Sprintf("--current-epoch=%d", current))
		if err != nil || !strings.HasSuffix(out, want) {
			t.Fatalf("epoch %d: %v\n%s", current, err, out)
		}
		chain := fakeChain(t, "0x3c4", &current)
		if fromChain, err := h.run("verify", "--in="+path, "--rpc="+chain.URL); err != nil || fromChain != out {
			t.Fatalf("epoch %d from the chain: %v\n%s", current, err, fromChain)
		}
	}
}

func TestVerifyReadsTheAddress(t *testing.T) {
	h := newHarness(t)
	file := signedFile(t, h)
	status, body, location := http.StatusOK, file, ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cache-Control") != "no-cache" {
			t.Error("a cached copy could answer")
		}
		if location != "" {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	out, err := h.run("verify", "--url="+server.URL)
	if err != nil || strings.Count(out, "UR settlement amendment") != 3 || !strings.Contains(out, server.URL) {
		t.Fatal(out, err)
	}
	for name, change := range map[string]func(){
		"an outage":  func() { status, body = http.StatusBadGateway, file },
		"a redirect": func() { status, body, location = http.StatusFound, "", server.URL },
		"a page":     func() { status, body, location = http.StatusOK, "<html>", "" },
		"too large":  func() { status, body = http.StatusOK, file+"#"+strings.Repeat(" ", maxFileBytes)+"\n" },
	} {
		change()
		if out, err := h.run("verify", "--url="+server.URL); err == nil {
			t.Fatalf("%s: %s", name, out)
		}
	}
}

func castPath(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("cast")
	if err != nil {
		t.Skip("cast is not installed")
	}
	return path
}

func castOutput(t *testing.T, cast string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), castTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, cast, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("cast %s: %v\n%s", redact(args), err, out)
	}
	return strings.TrimSpace(string(out))
}

// redact keeps a generated key out of the test log all the same.
func redact(args []string) string {
	shown := slices.Clone(args)
	for i := 1; i < len(shown); i++ {
		if shown[i-1] == "--private-key" {
			shown[i] = "<key>"
		}
	}
	return strings.Join(shown, " ")
}

func hexKey(key *ecdsa.PrivateKey) string {
	return "0x" + hex.EncodeToString(crypto.FromECDSA(key))
}

// The same cast that signs on the Ledger, with a generated key in the Ledger's place:
// it treats the line as text, hashes it as the package does, and returns a signature
// readers take as it is, with either recovery byte.
func TestCastSignsWhatAmendmentVerifies(t *testing.T) {
	cast := castPath(t)
	h := newHarness(t)
	key := hexKey(h.key)
	if got := castOutput(t, cast, "wallet", "address", "--private-key", key); !strings.EqualFold(got, h.signer.Hex()) {
		t.Fatalf("cast derives %s, not %s", got, h.signer)
	}
	seen := map[byte]int{}
	for epoch := uint64(8); epoch < 72 && (seen[27] == 0 || seen[28] == 0 || epoch < 12); epoch++ {
		for _, line := range []string{extendersLine(1, epoch), ctfLine(1, epoch)} {
			text := castOutput(t, cast, "wallet", "sign", "--private-key", key, line)
			if !signatureText.MatchString(text) {
				t.Fatalf("cast printed %q, not what --signature takes", text)
			}
			entry, err := amendment.ParseLine(line)
			if err != nil {
				t.Fatal(err)
			}
			if entry.Signature, err = hex.DecodeString(text[2:]); err != nil {
				t.Fatal(err)
			}
			seen[entry.Signature[64]]++
			// As cast returns it, with no change: the reader's own form.
			if err := entry.Verify(964, 25, h.signer); err != nil {
				t.Fatalf("%v\n%s", err, line)
			}
			if text != h.sign(t, line) {
				t.Fatalf("cast and Go sign the line differently:\n%s\n%s", text, h.sign(t, line))
			}
		}
	}
	if seen[27] == 0 || seen[28] == 0 || len(seen) != 2 {
		t.Fatalf("recovery bytes seen: %v", seen)
	}
	// And through the tool: attach takes what cast prints, and cast verifies the file's.
	path := writeAmendments(t, launchFile)
	line := ctfLine(1, 8)
	signature := castOutput(t, cast, "wallet", "sign", "--private-key", key, line)
	if out, err := h.run("attach", "--in="+path, "--line="+line, "--current-epoch=7", "--signature="+signature); err != nil {
		t.Fatal(out, err)
	}
	castOutput(t, cast, "wallet", "verify", "--address", h.signer.Hex(), line, signature)
	// A line is text to cast: only one starting 0x would be read as hex, and none does.
	asHex := castOutput(t, cast, "wallet", "sign", "--private-key", key, "0x"+hex.EncodeToString([]byte(line)))
	if asHex != signature {
		t.Fatalf("text and hex differ:\n%s\n%s", signature, asHex)
	}
}

// The printed commands, run as a shell runs them with only the wallet swapped, sign the
// exact line and attach it: the quoting leaves the line whole in the shells the owner
// might use.
func TestThePrintedCommandsSurviveTheShell(t *testing.T) {
	castPath(t)
	h := newHarness(t)
	line := ctfLine(1, 8)
	if !regexp.MustCompile(`^[A-Za-z0-9 .,:/-]+$`).MatchString(line) {
		t.Fatalf("a character a shell might read: %s", line)
	}
	ran := 0
	for _, shell := range []string{"sh", "bash", "zsh"} {
		if _, err := exec.LookPath(shell); err != nil {
			continue
		}
		ran++
		path := writeAmendments(t, launchFile)
		_, command := h.message(t, path, line, 7)
		const ledger = `--ledger --mnemonic-derivation-path "` + ledgerPathBlank + `"`
		if !strings.HasPrefix(command, "cast wallet sign "+ledger+` "`) {
			t.Fatal(command)
		}
		command = strings.Replace(command, ledger, "--private-key "+hexKey(h.key), 1)
		out, err := exec.Command(shell, "-c", command).Output()
		if err != nil {
			t.Fatal(shell, err)
		}
		signature := strings.TrimSpace(string(out))
		if signature != h.sign(t, line) {
			t.Fatalf("%s signed something else", shell)
		}
		// The attach command message printed, as the shell reads its --line.
		var printed string
		message, _ := h.run("message", "--in="+path, "--line="+line, "--current-epoch=7")
		for _, text := range strings.Split(message, "\n") {
			if strings.HasPrefix(text, "go run ./cli/amendment attach ") {
				printed = text
			}
		}
		words, err := exec.Command(shell, "-c", "set -- "+strings.TrimPrefix(printed, "go run ./cli/amendment ")+`; for arg in "$@"; do printf '%s\n' "$arg"; done`).Output()
		if err != nil {
			t.Fatal(shell, err)
		}
		args := strings.Split(strings.TrimSuffix(string(words), "\n"), "\n")
		for i := range args {
			switch {
			case args[i] == "--current-epoch=N":
				args[i] = "--current-epoch=7"
			case args[i] == "--signature=0x...":
				args[i] = "--signature=" + signature
			}
		}
		if out, err := h.run(args...); err != nil {
			t.Fatalf("%s: %v\n%s\n%q", shell, err, out, args)
		}
		if !strings.Contains(readAmendments(t, path), `- line: "`+line+`"`) {
			t.Fatalf("%s: not attached", shell)
		}
	}
	if ran == 0 {
		t.Skip("no shell to run it in")
	}
}

// A signature from any key but the pinned one gets nowhere, through the same path the
// owner's will take. Nothing here signs with the owner's key.
func TestOnlyThePinnedSignerIsAccepted(t *testing.T) {
	cast := castPath(t)
	h := newHarness(t)
	path := writeAmendments(t, launchFile)
	line := ctfLine(1, 8)
	signature := castOutput(t, cast, "wallet", "sign", "--private-key", hexKey(h.key), line)
	var stdout bytes.Buffer
	args := []string{"attach", "--in=" + path, "--line=" + line, "--current-epoch=7", "--signature=" + signature}
	err := run(context.Background(), args, &stdout, dependencies{signerFor: amendment.SignerFor})
	if err == nil || !strings.Contains(err.Error(), h.signer.Hex()) || !strings.Contains(err.Error(), "0x16C372dbBb24cd8473345ab13971E40814C8658F") {
		t.Fatal(stdout.String(), err)
	}
	if readAmendments(t, path) != launchFile {
		t.Fatal("a refused attach changed the file")
	}
	stdout.Reset()
	args = []string{"sign", "--in=" + path, "--line=" + line, "--current-epoch=7", "--key-file=" + writeKeyFile(t, h.key)}
	if err := run(context.Background(), args, &stdout, dependencies{signerFor: amendment.SignerFor}); err == nil || !strings.Contains(err.Error(), "not the pinned signer 0x16C372dbBb24cd8473345ab13971E40814C8658F") {
		t.Fatal(stdout.String(), err)
	}
	if readAmendments(t, path) != launchFile {
		t.Fatal("a refused sign changed the file")
	}
}

// A key file others can read is refused before it is used.
func TestSignRefusesAKeyFileOthersCanRead(t *testing.T) {
	h := newHarness(t)
	path := writeAmendments(t, launchFile)
	keyFile := writeKeyFile(t, h.key)
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604} {
		if err := os.Chmod(keyFile, mode); err != nil {
			t.Fatal(err)
		}
		out, err := h.run("sign", "--in="+path, "--line="+ctfLine(1, 8), "--current-epoch=7", "--key-file="+keyFile)
		if err == nil || !strings.Contains(err.Error(), "can be read by others") {
			t.Fatalf("mode %04o: %v\n%s", mode, err, out)
		}
	}
	if readAmendments(t, path) != launchFile {
		t.Fatal("a refused sign changed the file")
	}
}

// The chain is read after the file, so an edit made meanwhile is not overwritten.
func TestAnEditWhileReadingTheChainIsKept(t *testing.T) {
	h := newHarness(t)
	path := writeAmendments(t, launchFile)
	edited := launchFile + "# edited meanwhile\n"
	current := uint64(7)
	chain := fakeChain(t, "0x3c4", &current)
	var once sync.Once
	editing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			if err := os.WriteFile(path, []byte(edited), 0o640); err != nil {
				t.Error(err)
			}
		})
		chain.Config.Handler.ServeHTTP(w, r)
	}))
	defer editing.Close()
	line := ctfLine(1, 8)
	out, err := h.run("attach", "--in="+path, "--line="+line, "--signature="+h.sign(t, line), "--rpc="+editing.URL)
	if err == nil || !strings.Contains(err.Error(), "changed while this ran") {
		t.Fatal(out, err)
	}
	if readAmendments(t, path) != edited {
		t.Fatal("the edit was overwritten")
	}
}
