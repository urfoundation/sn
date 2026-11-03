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
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/demand/pricesheet"
)

// launchFile is the file published at launch, byte for byte. Tests keep their own copy
// because the repository's price.yml moves on with each signed price.
const launchFile = `sn: 25
# the alpha must be deposited as demand deposits per block (7 days)
#
# Initial period: no operator demand deposits are collected, so every rate is
# 0 alpha per GiB and 0 alpha per user. The tiers are kept for the shape of the
# schedule (an operator pays the best tier whose staked-alpha threshold it meets;
# the 0 tier applies to everyone) and will be filled in when deposits start.
tiered_per_block_demand_deposits:
  0:
    alpha_per_gib: 0
    alpha_per_user: 0
  1000:
    alpha_per_gib: 0
    alpha_per_user: 0
`

// A file as the owner has it after editing the rates: comments, one of them on a
// content line, around everything attach must leave alone.
const pricedFile = `sn: 25
# the alpha must be deposited as demand deposits per block (7 days)
#
# an operator pays the best tier whose staked-alpha threshold it meets
tiered_per_block_demand_deposits:
  0:
    alpha_per_gib: 0.021605
    alpha_per_user: 0.005401
  1000: # staked operators
    alpha_per_gib: 0.0108025
    alpha_per_user: 0.0027005
# the end of the file
`

const castTimeout = 2 * time.Minute

// What a refused epoch is told, so a test can tell this refusal from any other.
const placeRefusal = "a new entry must come after every existing one"

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

func writeSheet(t testing.TB, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "price.yml")
	if err := os.WriteFile(path, []byte(text), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func readSheet(t testing.TB, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// message runs the command and returns the line it says to sign and its cast command.
func (h *harness) message(t testing.TB, path string, epoch uint64, capAlpha string) (line, command string) {
	t.Helper()
	out, err := h.run("message", "--in="+path, fmt.Sprintf("--epoch=%d", epoch), "--cap="+capAlpha)
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
func (h *harness) attach(t testing.TB, path string, epoch uint64, capAlpha string) string {
	t.Helper()
	line, _ := h.message(t, path, epoch, capAlpha)
	signature := h.sign(t, line)
	if _, err := h.run("attach", "--in="+path, fmt.Sprintf("--epoch=%d", epoch), "--cap="+capAlpha, "--signature="+signature); err != nil {
		t.Fatal(err)
	}
	return signature
}

// The launch file verifies, reports that nothing signed is in force, and is left alone.
func TestTheLaunchFileVerifies(t *testing.T) {
	path := writeSheet(t, launchFile)
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"verify", "--in=" + path}, &stdout, dependencies{signerFor: pricesheet.SignerFor}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "no signed price is in force") || strings.Contains(stdout.String(), "Warning") {
		t.Fatal(stdout.String())
	}
	if readSheet(t, path) != launchFile {
		t.Fatal("verify changed the file")
	}
}

// The repository's own price.yml, whatever it holds by now, verifies under the real
// pinned signer. It is the file the site publishes.
func TestTheRepositoryFileVerifies(t *testing.T) {
	const repositoryFile = "../../price.yml"
	var stdout bytes.Buffer
	if err := run(context.Background(), []string{"verify", "--in=" + repositoryFile}, &stdout, dependencies{signerFor: pricesheet.SignerFor}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "Warning") {
		t.Fatal(stdout.String())
	}
}

// The owner key's path stays out of this code, so without the flag the printed command
// has a blank that cast cannot read as a path, and the output says to fill it.
func TestMessageWithoutALedgerPathLeavesABlank(t *testing.T) {
	path := writeSheet(t, pricedFile)
	var stdout bytes.Buffer
	args := []string{"message", "--in=" + path, "--epoch=9", "--cap=5000"}
	if err := run(context.Background(), args, &stdout, dependencies{signerFor: pricesheet.SignerFor}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	if !strings.Contains(out, `--mnemonic-derivation-path "<ledger-path>" "`) || !strings.Contains(out, "Put the owner key's derivation path in place of <ledger-path>") {
		t.Fatal(out)
	}
}

// Whatever the flag holds is printed into a command to run, so only a path gets through.
func TestMessageRefusesWhatIsNotALedgerPath(t *testing.T) {
	path := writeSheet(t, pricedFile)
	for _, bad := range []string{`m/44'/60'/0'/0/0" --private-key "0x1`, "44'/60'/0'/0/0", "m/", "m", "m/44'/$(id)", "m/44''/0"} {
		var stdout bytes.Buffer
		args := []string{"message", "--in=" + path, "--epoch=9", "--cap=5000", "--ledger-path=" + bad}
		if err := run(context.Background(), args, &stdout, dependencies{signerFor: pricesheet.SignerFor}); err == nil {
			t.Fatalf("took %q:\n%s", bad, stdout.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("printed for %q:\n%s", bad, stdout.String())
		}
	}
}

func TestMessagePrintsTheLineAndTheCommand(t *testing.T) {
	path := writeSheet(t, pricedFile)
	var stdout bytes.Buffer
	args := []string{"message", "--in=" + path, "--epoch=9", "--cap=5000", "--ledger-path=m/44'/60'/7'/0/0"}
	if err := run(context.Background(), args, &stdout, dependencies{signerFor: pricesheet.SignerFor}); err != nil {
		t.Fatal(err)
	}
	const line = "UR price sheet v1: chain 964, sn 25, from epoch 9, cap 5000 alpha per epoch, tier 0 alpha 0.021605 per GiB 0.005401 per user, tier 1000 alpha 0.0108025 per GiB 0.0027005 per user"
	want := "The line to sign:\n\n" + line + "\n\n" +
		"Not checked against the chain: pass --rpc to refuse an epoch that is too soon.\n\n" +
		"Sign it with the Ledger's Ethereum app open. The device must show exactly this line,\n" +
		"and the key is 0x16C372dbBb24cd8473345ab13971E40814C8658F:\n\n" +
		`cast wallet sign --ledger --mnemonic-derivation-path "m/44'/60'/7'/0/0" "` + line + "\"\n\n" +
		"Then attach the signature it prints:\n\n" +
		"go run ./cli/pricesheet attach --in=" + path + " --epoch=9 --cap=5000 --signature=0x...\n"
	if stdout.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", stdout.String(), want)
	}
	if readSheet(t, path) != pricedFile {
		t.Fatal("message changed the file")
	}
}

// An exact amount stays exact however it is spelled, up to what a uint64 of rao holds.
func TestCapIsExact(t *testing.T) {
	for text, want := range map[string]uint64{
		"5000":                  5_000_000_000_000,
		"5000.000":              5_000_000_000_000,
		"0.000000001":           1,
		"0.000065":              65_000, // uint64(0.000065*1e9) is 64,999 through a float
		"18446744073.709551615": 1<<64 - 1,
	} {
		got, err := parseAlpha("--cap", text)
		if err != nil || got != want {
			t.Fatalf("%s: got %d, %v, want %d", text, got, err, want)
		}
	}
	for _, text := range []string{"", "0.0000000001", "18446744073.709551616", "1e3", "1_000", "0x10", "-1", "+1", ".5", "5.", "05", " 5", "NaN", "Inf"} {
		if got, err := parseAlpha("--cap", text); err == nil {
			t.Fatalf("%q read as %d", text, got)
		}
	}
}

func TestMessageRefuses(t *testing.T) {
	h := newHarness(t)
	replace := func(old, next string) string {
		if !strings.Contains(pricedFile, old) {
			t.Fatalf("no %q in the file", old)
		}
		return strings.Replace(pricedFile, old, next, 1)
	}
	for name, file := range map[string]string{
		"rising rate":      replace("alpha_per_gib: 0.0108025", "alpha_per_gib: 0.03"),
		"rising user rate": replace("alpha_per_user: 0.0027005", "alpha_per_user: 0.03"),
		"partly priced":    replace("alpha_per_gib: 0.0108025", "alpha_per_gib: 0"),
		"no zero tier":     replace("  0:\n", "  1:\n"),
		"tiers reversed":   "sn: 25\ntiered_per_block_demand_deposits:\n  1000:\n    alpha_per_gib: 0\n    alpha_per_user: 0\n  0:\n    alpha_per_gib: 0\n    alpha_per_user: 0\n",
		"repeated tier":    "sn: 25\ntiered_per_block_demand_deposits:\n  0:\n    alpha_per_gib: 0\n    alpha_per_user: 0\n  0:\n    alpha_per_gib: 0\n    alpha_per_user: 0\n",
		"negative rate":    replace("alpha_per_gib: 0.021605", "alpha_per_gib: -0.021605"),
		"misspelt rate":    replace("alpha_per_gib: 0.021605", "alpha_per_gb: 0.021605"),
		"missing rate":     replace("    alpha_per_user: 0.005401\n", ""),
		"no rates":         "sn: 25\n",
		"misspelt section": pricedFile + "signed_schedulee:\n  1: {}\n",
		"unknown field":    pricedFile + "notes: hello\n",
		"another subnet":   replace("sn: 25", "sn: 26"),
		"no sn":            replace("sn: 25\n", ""),
		"not a sheet":      "<html>",
		"nine tiers": "sn: 25\ntiered_per_block_demand_deposits:\n" + func() string {
			var tiers strings.Builder
			for i := range 9 {
				fmt.Fprintf(&tiers, "  %d:\n    alpha_per_gib: 0\n    alpha_per_user: 0\n", i)
			}
			return tiers.String()
		}(),
	} {
		path := writeSheet(t, file)
		if out, err := h.run("message", "--in="+path, "--epoch=9", "--cap=5000"); err == nil {
			t.Fatalf("%s: printed:\n%s", name, out)
		}
	}
	path := writeSheet(t, pricedFile)
	for name, args := range map[string][]string{
		"no in":        {"--epoch=9", "--cap=5000"},
		"missing file": {"--in=" + path + ".gone", "--epoch=9", "--cap=5000"},
		"no epoch":     {"--in=" + path, "--cap=5000"},
		"no cap":       {"--in=" + path, "--epoch=9"},
		"zero cap":     {"--in=" + path, "--epoch=9", "--cap=0"},
		"octal epoch":  {"--in=" + path, "--epoch=010", "--cap=5000"},
		"hex epoch":    {"--in=" + path, "--epoch=0x10", "--cap=5000"},
		"split epoch":  {"--in=" + path, "--epoch=1_000", "--cap=5000"},
		"huge epoch":   {"--in=" + path, "--epoch=18446744073709551616", "--cap=5000"},
		"testnet":      {"--in=" + path, "--epoch=9", "--cap=5000", "--chain=945"},
		"chain text":   {"--in=" + path, "--epoch=9", "--cap=5000", "--chain=main"},
		"positional":   {"--in=" + path, "--epoch=9", "--cap=5000", "extra"},
		"signature":    {"--in=" + path, "--epoch=9", "--cap=5000", "--signature=0x00"},
		"unknown flag": {"--in=" + path, "--epoch=9", "--cap=5000", "--force"},
		"unused url":   {"--in=" + path, "--epoch=9", "--cap=5000", "--url=http://example.invalid"},
	} {
		if out, err := h.run(append([]string{"message"}, args...)...); err == nil {
			t.Fatalf("%s: printed:\n%s", name, out)
		}
	}
	if _, err := h.run("sign", "--in="+path); err == nil {
		t.Fatal("ran an unknown command")
	}
	for _, help := range [][]string{nil, {"help"}, {"--help"}, {"-h"}, {"attach", "--help"}} {
		out, err := h.run(help...)
		if err != nil || !strings.Contains(out, "pricesheet verify") {
			t.Fatal(help, out, err)
		}
	}
}

// fakeChain answers as the coordinator's RPC does and checks what it is asked.
func fakeChain(t *testing.T, chainID string, currentEpoch *uint64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		var result string
		switch request.Method {
		case "eth_chainId":
			result = chainID
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
			if data := call.Input + call.Data; data != "0x76671808" { // currentEpoch()
				t.Errorf("called %s", data)
			}
			result = "0x"
			if currentEpoch != nil {
				result = "0x" + hex.EncodeToString(common.LeftPadBytes(new(big.Int).SetUint64(*currentEpoch).Bytes(), 32))
			}
		default:
			t.Errorf("unexpected method %s", request.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%q}`, request.ID, result)
	}))
	t.Cleanup(server.Close)
	return server
}

// With an RPC, an epoch must be at least the current one plus one.
func TestAnEpochMustBeAhead(t *testing.T) {
	h := newHarness(t)
	path := writeSheet(t, pricedFile)
	current := uint64(4)
	chain := fakeChain(t, "0x3c4", &current)
	message := func(epoch uint64) (string, error) {
		return h.run("message", "--in="+path, fmt.Sprintf("--epoch=%d", epoch), "--cap=5000", "--rpc="+chain.URL)
	}
	for _, epoch := range []uint64{0, 3, 4} {
		if out, err := message(epoch); err == nil || !strings.Contains(err.Error(), "the earliest is 5") {
			t.Fatalf("epoch %d: %v\n%s", epoch, err, out)
		}
	}
	for _, epoch := range []uint64{5, 500} {
		out, err := message(epoch)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, fmt.Sprintf("The current epoch is 4, so epoch %d is at least one epoch ahead.", epoch)) {
			t.Fatal(out)
		}
	}
	// A chain that moves on while the owner is signing refuses at attach too.
	line, _ := h.message(t, path, 5, "5000")
	current = 5
	attach := []string{"attach", "--in=" + path, "--epoch=5", "--cap=5000", "--signature=" + h.sign(t, line), "--rpc=" + chain.URL}
	if _, err := h.run(attach...); err == nil {
		t.Fatal("attached an entry that is too soon")
	}
	if readSheet(t, path) != pricedFile {
		t.Fatal("a refused attach changed the file")
	}
	// A chain that cannot answer is no answer: nothing is printed for it.
	wrongChain := fakeChain(t, "0x3b1", &current)
	noCoordinator := fakeChain(t, "0x3c4", nil)
	closed := fakeChain(t, "0x3c4", &current)
	closed.Close()
	for name, endpoint := range map[string]string{"another chain": wrongChain.URL, "no coordinator": noCoordinator.URL, "down": closed.URL, "not a url": "::"} {
		if out, err := h.run("message", "--in="+path, "--epoch=50", "--cap=5000", "--rpc="+endpoint); err == nil || out != "" {
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
	path := writeSheet(t, pricedFile)
	line, _ := h.message(t, path, 9, "5000.0")
	signature := h.sign(t, line)
	out, err := h.run("attach", "--in="+path, "--epoch=9", "--cap=5000.0", "--signature="+signature)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, line) {
		t.Fatal(out)
	}
	want := pricedFile + `signed_schedule:
  9:
    epoch_cap_alpha: 5000
    signature: "` + signature + `"
    tiers:
      0:
        alpha_per_gib: 0.021605
        alpha_per_user: 0.005401
      1000:
        alpha_per_gib: 0.0108025
        alpha_per_user: 0.0027005
`
	if got := readSheet(t, path); got != want {
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
	// The same entry again is at the newest epoch like any other, and is refused.
	if out, err := h.run("attach", "--in="+path, "--epoch=9", "--cap=5000", "--signature="+signature); err == nil || !strings.Contains(err.Error(), placeRefusal) {
		t.Fatal(out, err)
	}
	if readSheet(t, path) != want {
		t.Fatal("attaching the same entry changed the file")
	}
}

// Each later price is signed over the rates then in the file. Every earlier entry and
// every comment stays byte for byte, wherever in the file the section is.
func TestAttachKeepsEveryEarlierByte(t *testing.T) {
	h := newHarness(t)
	layouts := map[string]string{
		"at the end of the file": pricedFile,
		"no final newline":       strings.TrimSuffix(pricedFile, "\n"),
	}
	for name, start := range layouts {
		t.Run(name, func(t *testing.T) {
			path := writeSheet(t, start)
			h.attach(t, path, 9, "5000")
			first := readSheet(t, path)
			// The owner's own note beside the first entry.
			noted := strings.Replace(first, "signed_schedule:\n", "signed_schedule:\n  # prices from launch\n", 1)
			noted = strings.Replace(noted, "    epoch_cap_alpha: 5000\n", "    epoch_cap_alpha: 5000 # reviewed\n", 1)
			noted += "# after the schedule\n"
			edited := strings.Replace(noted, "alpha_per_gib: 0.021605", "alpha_per_gib: 0.04", 1)
			edited = strings.Replace(edited, "alpha_per_user: 0.0027005", "alpha_per_user: 0.002", 1)
			if err := os.WriteFile(path, []byte(edited), 0o640); err != nil {
				t.Fatal(err)
			}
			h.attach(t, path, 20, "6000")
			second := readSheet(t, path)
			block := strings.TrimPrefix(second, edited[:strings.Index(edited, "# after the schedule")])
			if !strings.HasSuffix(block, "# after the schedule\n") || !strings.HasPrefix(block, "  20:\n") {
				t.Fatalf("the entry is not right after the last one:\n%s", second)
			}
			if without := strings.Replace(second, strings.TrimSuffix(block, "# after the schedule\n"), "", 1); without != edited {
				t.Fatalf("attach changed other bytes:\n%s", second)
			}
			if out, err := h.run("verify", "--in="+path); err != nil || strings.Count(out, "UR price sheet v1") != 2 {
				t.Fatal(out, err)
			}
			sheet, err := pricesheet.Parse([]byte(second))
			if err != nil {
				t.Fatal(err)
			}
			if sheet.At(19).EffectiveEpoch != 9 || sheet.At(20).EffectiveEpoch != 20 || sheet.At(20).EpochCapRao != 6_000_000_000_000 {
				t.Fatalf("%+v", sheet)
			}
			last, err := pricesheet.Parse([]byte(edited))
			if err != nil {
				t.Fatal(err)
			}
			if !pricesheet.Extends(sheet, last) {
				t.Fatal("a reader would take this for a rollback")
			}
		})
	}
	// The section ahead of the rates: a new entry goes right after the last one, and an
	// epoch at or below the newest is refused, at message and at attach, however good its
	// signature.
	t.Run("in the middle of the file", func(t *testing.T) {
		path := writeSheet(t, pricedFile)
		h.attach(t, path, 20, "5000")
		text := readSheet(t, path)
		at := strings.Index(text, "signed_schedule:")
		at2 := strings.Index(text, "tiered_per_block_demand_deposits:")
		moved := text[:at2] + text[at:] + "# the rates\n" + text[at2:at]
		if err := os.WriteFile(path, []byte(moved), 0o640); err != nil {
			t.Fatal(err)
		}
		line, _ := h.message(t, path, 21, "5000")
		for _, epoch := range []uint64{0, 9, 19, 20} {
			flags := []string{"--in=" + path, fmt.Sprintf("--epoch=%d", epoch), "--cap=5000"}
			if out, err := h.run(append([]string{"message"}, flags...)...); err == nil || !strings.Contains(err.Error(), placeRefusal) {
				t.Fatalf("message at epoch %d: %v\n%s", epoch, err, out)
			}
			signature := h.sign(t, strings.Replace(line, "from epoch 21", fmt.Sprintf("from epoch %d", epoch), 1))
			if out, err := h.run(append([]string{"attach", "--signature=" + signature}, flags...)...); err == nil || !strings.Contains(err.Error(), placeRefusal) {
				t.Fatalf("attach at epoch %d: %v\n%s", epoch, err, out)
			}
			if readSheet(t, path) != moved {
				t.Fatalf("a refused attach at epoch %d changed the file", epoch)
			}
		}
		h.attach(t, path, 21, "5000")
		h.attach(t, path, 30, "5000")
		sheet, err := pricesheet.Parse([]byte(readSheet(t, path)))
		if err != nil {
			t.Fatal(err)
		}
		var epochs []uint64
		for _, entry := range sheet.Entries {
			epochs = append(epochs, entry.EffectiveEpoch)
		}
		if !slices.Equal(epochs, []uint64{20, 21, 30}) {
			t.Fatal(epochs)
		}
		last, err := pricesheet.Parse([]byte(moved))
		if err != nil {
			t.Fatal(err)
		}
		if !pricesheet.Extends(sheet, last) {
			t.Fatal("a reader would refuse this file")
		}
		result := readSheet(t, path)
		for _, removed := range []uint64{21, 30} {
			start := strings.Index(result, fmt.Sprintf("  %d:\n", removed))
			end := start + strings.Index(result[start:], "      1000:\n") + len("      1000:\n        alpha_per_gib: 0.0108025\n        alpha_per_user: 0.0027005\n")
			result = result[:start] + result[end:]
		}
		if result != moved {
			t.Fatalf("attach changed other bytes:\n%s", readSheet(t, path))
		}
	})
}

func TestAttachRefuses(t *testing.T) {
	h := newHarness(t)
	other := newHarness(t)
	path := writeSheet(t, pricedFile)
	signature := h.attach(t, path, 9, "5000")
	current := readSheet(t, path)
	line, _ := h.message(t, path, 20, "5000")
	good := h.sign(t, line)
	attach := func(epoch, capAlpha, signature string) []string {
		return []string{"attach", "--in=" + path, "--epoch=" + epoch, "--cap=" + capAlpha, "--signature=" + signature}
	}
	for name, args := range map[string][]string{
		"another signer":      attach("20", "5000", other.sign(t, line)),
		"another epoch":       attach("21", "5000", good),
		"another cap":         attach("20", "5001", good),
		"no signature":        {"attach", "--in=" + path, "--epoch=20", "--cap=5000"},
		"short signature":     attach("20", "5000", good[:len(good)-2]),
		"long signature":      attach("20", "5000", good+"00"),
		"no 0x":               attach("20", "5000", good[2:]),
		"not hex":             attach("20", "5000", "0x"+strings.Repeat("zz", 65)),
		"bad recovery byte":   attach("20", "5000", good[:len(good)-2]+"1d"),
		"text mode signature": attach("20", "5000", h.sign(t, "0x"+hex.EncodeToString([]byte(line)))),
		"bytes not text": attach("20", "5000", func() string {
			sig, _ := crypto.Sign(crypto.Keccak256([]byte(line)), h.key)
			sig[64] += 27
			return "0x" + hex.EncodeToString(sig)
		}()),
		"no pinned signer": append(attach("20", "5000", good), "--chain=945"),
	} {
		if out, err := h.run(args...); err == nil {
			t.Fatalf("%s: %s", name, out)
		}
		if readSheet(t, path) != current {
			t.Fatalf("%s changed the file", name)
		}
	}
	// An existing entry never changes, whoever signed the change.
	for name, args := range map[string][]string{
		"a changed cap":       attach("9", "5001", h.sign(t, strings.Replace(line, "from epoch 20, cap 5000 ", "from epoch 9, cap 5001 ", 1))),
		"another signature":   attach("9", "5000", other.sign(t, strings.Replace(line, "from epoch 20", "from epoch 9", 1))),
		"malleated signature": attach("9", "5000", malleate(t, signature)),
	} {
		if out, err := h.run(args...); err == nil || !strings.Contains(err.Error(), placeRefusal) {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		if readSheet(t, path) != current {
			t.Fatalf("%s changed the file", name)
		}
	}
	// A signature over rates the owner has since edited says nothing of the new ones.
	edited := strings.Replace(current, "alpha_per_gib: 0.021605", "alpha_per_gib: 0.04", 1)
	if err := os.WriteFile(path, []byte(edited), 0o640); err != nil {
		t.Fatal(err)
	}
	if out, err := h.run(attach("20", "5000", good)...); err == nil || !strings.Contains(err.Error(), "exactly this line") {
		t.Fatal(out, err)
	}
	if readSheet(t, path) != edited {
		t.Fatal("a refused attach changed the file")
	}
	// A good signature does not place an entry before the newest: nobody is offered the
	// chance to sign one, and attach takes none.
	earlier := strings.Replace(line, "from epoch 20", "from epoch 5", 1)
	earlier = strings.Replace(earlier, "alpha 0.021605", "alpha 0.04", 1)
	if out, err := h.run(attach("5", "5000", h.sign(t, earlier))...); err == nil || !strings.Contains(err.Error(), placeRefusal) {
		t.Fatal(out, err)
	}
	if out, err := h.run("message", "--in="+path, "--epoch=5", "--cap=5000"); err == nil || !strings.Contains(err.Error(), placeRefusal) {
		t.Fatal("offered to sign what attach would refuse:", out, err)
	}
	if readSheet(t, path) != edited {
		t.Fatal("a refused attach changed the file")
	}
	// A tampered earlier entry stops everything: nothing new joins a bad file.
	tampered := strings.Replace(current, "epoch_cap_alpha: 5000", "epoch_cap_alpha: 6000", 1)
	if err := os.WriteFile(path, []byte(tampered), 0o640); err != nil {
		t.Fatal(err)
	}
	if out, err := h.run(attach("20", "5000", good)...); err == nil {
		t.Fatal(out)
	}
	if out, err := h.run("message", "--in="+path, "--epoch=20", "--cap=5000"); err == nil {
		t.Fatal(out)
	}
	if readSheet(t, path) != tampered {
		t.Fatal("a refused attach changed the file")
	}
	// Layouts it cannot add lines to are refused rather than rewritten.
	for name, file := range map[string]string{
		"flow schedule": pricedFile + "signed_schedule: {}\n",
		"carriage":      strings.ReplaceAll(pricedFile, "\n", "\r\n"),
	} {
		path := writeSheet(t, file)
		if out, err := h.run("attach", "--in="+path, "--epoch=20", "--cap=5000", "--signature="+good); err == nil {
			t.Fatalf("%s: %s", name, out)
		}
		if readSheet(t, path) != file {
			t.Fatalf("%s changed the file", name)
		}
	}
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

// Layout and write failures never reach the file.
func TestAFailedWriteLeavesTheFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	h := newHarness(t)
	path := writeSheet(t, pricedFile)
	line, _ := h.message(t, path, 20, "5000")
	directory := filepath.Dir(path)
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(directory, 0o700)
	if out, err := h.run("attach", "--in="+path, "--epoch=20", "--cap=5000", "--signature="+h.sign(t, line)); err == nil {
		t.Fatal(out)
	}
	if readSheet(t, path) != pricedFile {
		t.Fatal("a failed write changed the file")
	}
}

// Rates a float would round survive signing, attach, and a reader's parse, exactly.
func TestExactRatesRoundTrip(t *testing.T) {
	h := newHarness(t)
	replaced := func(gib, user, gib2, user2 string) string {
		return strings.NewReplacer(
			"alpha_per_gib: 0.021605", "alpha_per_gib: "+gib,
			"alpha_per_user: 0.005401", "alpha_per_user: "+user,
			"alpha_per_gib: 0.0108025", "alpha_per_gib: "+gib2,
			"alpha_per_user: 0.0027005", "alpha_per_user: "+user2,
		).Replace(pricedFile)
	}
	for name, test := range map[string]struct {
		file    string
		inLine  []string
		inEntry []string
	}{
		"past a double": {
			file: replaced("0.1234567891234567891", "0.00000000000000000003", "0.1234567891234567890", "0.00000000000000000001"),
			inLine: []string{
				"tier 0 alpha 0.1234567891234567891 per GiB 0.00000000000000000003 per user",
				"tier 1000 alpha 0.123456789123456789 per GiB 0.00000000000000000001 per user",
			},
			inEntry: []string{
				"alpha_per_gib: 0.1234567891234567891\n",
				"alpha_per_gib: 0.123456789123456789\n", // the file's trailing zero states the same price
				"alpha_per_user: 0.00000000000000000003\n",
			},
		},
		"the finest rate a sheet can state": {
			file:    replaced("0.0000000000000000000000000003", "0.0000000000000000000000000002", "0.0000000000000000000000000002", "0.0000000000000000000000000001"),
			inLine:  []string{"tier 0 alpha 0.0000000000000000000000000003 per GiB 0.0000000000000000000000000002 per user"},
			inEntry: []string{"alpha_per_gib: 0.0000000000000000000000000003\n"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := writeSheet(t, test.file)
			const maxCap = "18446744073.709551615"
			line, _ := h.message(t, path, 9, maxCap)
			for _, want := range append([]string{"cap " + maxCap + " alpha"}, test.inLine...) {
				if !strings.Contains(line, want) {
					t.Fatalf("no %q in %s", want, line)
				}
			}
			h.attach(t, path, 9, maxCap)
			result := readSheet(t, path)
			for _, want := range append([]string{"epoch_cap_alpha: " + maxCap + "\n"}, test.inEntry...) {
				if !strings.Contains(result, want) {
					t.Fatalf("no %q in:\n%s", want, result)
				}
			}
			if out, err := h.run("verify", "--in="+path); err != nil || !strings.Contains(out, line) {
				t.Fatal(out, err)
			}
		})
	}
}

func signedFile(t *testing.T, h *harness) string {
	t.Helper()
	path := writeSheet(t, pricedFile)
	h.attach(t, path, 9, "5000")
	h.attach(t, path, 20, "5000")
	return readSheet(t, path)
}

func TestVerifyRefuses(t *testing.T) {
	h := newHarness(t)
	other := newHarness(t)
	good := signedFile(t, h)
	if out, err := h.run("verify", "--in="+writeSheet(t, good)); err != nil {
		t.Fatal(out, err)
	}
	// Under the owner's real pinned key, a generated key's signature is no signature.
	var stdout bytes.Buffer
	err := run(context.Background(), []string{"verify", "--in=" + writeSheet(t, good)}, &stdout, dependencies{signerFor: pricesheet.SignerFor})
	if err == nil || !strings.Contains(err.Error(), h.signer.Hex()) || !strings.Contains(err.Error(), pricesheet.MainnetSigner.Hex()) {
		t.Fatal(stdout.String(), err)
	}
	mirrorEnd := strings.Index(good, "signed_schedule:")
	mirror := good[:mirrorEnd]
	for name, file := range map[string]string{
		"mirror rate differs":          strings.Replace(mirror, "0.021605", "0.03", 1) + good[mirrorEnd:],
		"mirror differs past a double": strings.Replace(mirror, "0.021605", "0.0216050000000000001", 1) + good[mirrorEnd:],
		"mirror threshold differs":     strings.Replace(mirror, "1000:", "1001:", 1) + good[mirrorEnd:],
		"mirror tier missing":          mirror[:strings.Index(mirror, "  1000:")] + good[mirrorEnd:],
		"no mirror":                    "sn: 25\n" + good[mirrorEnd:],
		"bad mirror":                   strings.Replace(good, "0.0108025", "0.5", 1),
		"changed rate":                 strings.Replace(good, "signed_schedule:\n  9:\n    epoch_cap_alpha: 5000", "signed_schedule:\n  9:\n    epoch_cap_alpha: 5001", 1),
		"an entry by another key":      forge(t, other, good),
		"unknown top-level field":      good + "extra: 1\n",
		"misspelt schedule":            strings.Replace(good, "signed_schedule:", "signed_schedulee:", 1),
		"another subnet":               strings.Replace(good, "sn: 25", "sn: 26", 1),
		"an entry with no signature":   regexp.MustCompile(`(?m)^    signature: .*\n`).ReplaceAllString(good, ""),
		"not a sheet":                  "<html>",
		"empty":                        "",
		"two documents":                good + "---\nsn: 25\n",
	} {
		if out, err := h.run("verify", "--in="+writeSheet(t, file)); err == nil {
			t.Fatalf("%s: %s", name, out)
		}
	}
	// The mirror is compared by value, however it is written.
	respelled := strings.ReplaceAll(mirror, "0.021605", "0.02160500000000") + good[mirrorEnd:]
	if out, err := h.run("verify", "--in="+writeSheet(t, respelled)); err != nil {
		t.Fatal(out, err)
	}
	// The flags.
	path := writeSheet(t, good)
	for name, args := range map[string][]string{
		"neither":    {"verify"},
		"both":       {"verify", "--in=" + path, "--url=http://example.invalid"},
		"bad chain":  {"verify", "--in=" + path, "--chain=0x3c4"},
		"testnet":    {"verify", "--in=" + path, "--chain=945"},
		"positional": {"verify", path},
	} {
		if out, err := h.run(args...); err == nil {
			t.Fatalf("%s: %s", name, out)
		}
	}
}

// forge swaps the first entry's signature for a good signature of the same line by
// someone else.
func forge(t *testing.T, other *harness, file string) string {
	t.Helper()
	sheet, err := pricesheet.Parse([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	line, err := sheet.Entries[0].Message(964, 25)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Replace(file, hex.EncodeToString(sheet.Entries[0].Signature), strings.TrimPrefix(other.sign(t, line), "0x"), 1)
}

func TestVerifySaysWhatAppliesNow(t *testing.T) {
	h := newHarness(t)
	path := writeSheet(t, signedFile(t, h))
	out, err := h.run("verify", "--in="+path)
	if err != nil || !strings.Contains(out, "Pass --rpc to see which entry applies now.") {
		t.Fatal(out, err)
	}
	if strings.Count(out, "UR price sheet v1") != 2 || !strings.Contains(out, "from epoch 9,") || !strings.Contains(out, "from epoch 20,") {
		t.Fatal(out)
	}
	for current, want := range map[uint64]string{
		0:    "Current epoch 0: no signed price is in force yet. The entry from epoch 9 is next.",
		8:    "Current epoch 8: no signed price is in force yet. The entry from epoch 9 is next.",
		9:    "Current epoch 9: the entry from epoch 9 applies. The entry from epoch 20 is next.",
		19:   "Current epoch 19: the entry from epoch 9 applies. The entry from epoch 20 is next.",
		20:   "Current epoch 20: the entry from epoch 20 applies.\n",
		1000: "Current epoch 1000: the entry from epoch 20 applies.\n",
	} {
		chain := fakeChain(t, "0x3c4", &current)
		out, err := h.run("verify", "--in="+path, "--rpc="+chain.URL)
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("epoch %d: %v\n%s", current, err, out)
		}
	}
	// No entries at all: the same answer as the launch file gets.
	current := uint64(4)
	chain := fakeChain(t, "0x3c4", &current)
	out, err = h.run("verify", "--in="+writeSheet(t, launchFile), "--rpc="+chain.URL)
	if err != nil || !strings.Contains(out, "Current epoch 4: no signed price is in force.") {
		t.Fatal(out, err)
	}
	// Rates nobody signed are said to be just that, and are in force nowhere.
	out, err = h.run("verify", "--in="+writeSheet(t, pricedFile), "--rpc="+chain.URL)
	if err != nil || !strings.Contains(out, "Warning: the file's top-level rates are not zero and nobody signed them.") {
		t.Fatal(out, err)
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
	if err != nil || strings.Count(out, "UR price sheet v1") != 2 || !strings.Contains(out, server.URL) {
		t.Fatal(out, err)
	}
	for name, change := range map[string]func(){
		"an outage":  func() { status, body = http.StatusBadGateway, file },
		"a redirect": func() { status, body, location = http.StatusFound, "", server.URL },
		"a page":     func() { status, body, location = http.StatusOK, "<html>", "" },
		"too large":  func() { status, body = http.StatusOK, file+"#"+strings.Repeat(" ", maxSheetBytes)+"\n" },
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

func decodeSignature(t *testing.T, text string) []byte {
	t.Helper()
	if !signatureText.MatchString(text) {
		t.Fatalf("cast printed %q, not what --signature takes", text)
	}
	raw, err := hex.DecodeString(text[2:])
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The same cast that signs on the Ledger, with a generated key in the Ledger's place:
// it treats the line as text, hashes it as pricesheet does, and returns a recovery
// byte pricesheet takes. Shapes run from the shortest line to the longest.
func TestCastSignsWhatPricesheetVerifies(t *testing.T) {
	cast := castPath(t)
	h := newHarness(t)
	key := hexKey(h.key)
	if got := castOutput(t, cast, "wallet", "address", "--private-key", key); !strings.EqualFold(got, h.signer.Hex()) {
		t.Fatalf("cast derives %s, not %s", got, h.signer)
	}
	tiers := func(rates ...string) string {
		var file strings.Builder
		file.WriteString("sn: 25\ntiered_per_block_demand_deposits:\n")
		for i := 0; i < len(rates); i += 2 {
			fmt.Fprintf(&file, "  %d:\n    alpha_per_gib: %s\n    alpha_per_user: %s\n", i*500, rates[i], rates[i+1])
		}
		return file.String()
	}
	for name, file := range map[string]string{
		"priced":    pricedFile,
		"zero":      tiers("0", "0", "0", "0"),
		"gib only":  tiers("0.5", "0", "0.25", "0"),
		"user only": tiers("0", "0.5", "0", "0.25"),
		"one tier":  tiers("0.1", "0.2"),
		"exact":     tiers("0.1234567891234567891", "0.00000000000000000003", "0.1234567891234567890", "0.00000000000000000001"),
		"longest": func() string {
			var rates []string
			for i := range 8 {
				rate := fmt.Sprintf("0.0000000011234567891234567%03d", 999-i)
				rates = append(rates, rate, rate)
			}
			return tiers(rates...)
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			path := writeSheet(t, file)
			line, _ := h.message(t, path, 9, "5000")
			signature := castOutput(t, cast, "wallet", "sign", "--private-key", key, line)
			entry := pricesheet.Entry{EffectiveEpoch: 9, EpochCapRao: 5_000_000_000_000}
			sheet, err := pricesheet.Parse([]byte(file))
			if err != nil {
				t.Fatal(err)
			}
			entry.Tiers = sheet.Mirror
			entry.Signature = decodeSignature(t, signature)
			if err := entry.Verify(964, 25, h.signer); err != nil {
				t.Fatalf("%v\n%s", err, line)
			}
			if out, err := h.run("attach", "--in="+path, "--epoch=9", "--cap=5000", "--signature="+signature); err != nil {
				t.Fatal(out, err)
			}
			if out, err := h.run("verify", "--in="+path); err != nil || !strings.Contains(out, line) {
				t.Fatal(out, err)
			}
			// And the other way: what Go signs, cast verifies.
			castOutput(t, cast, "wallet", "verify", "--address", h.signer.Hex(), line, h.sign(t, line))
			// A line is text to cast: signing its bytes as hex comes to the same thing, and
			// only a line starting 0x would be read as hex. None does.
			if strings.HasPrefix(line, "0x") {
				t.Fatal(line)
			}
			asHex := castOutput(t, cast, "wallet", "sign", "--private-key", key, "0x"+hex.EncodeToString([]byte(line)))
			if asHex != signature {
				t.Fatalf("text and hex differ:\n%s\n%s", signature, asHex)
			}
		})
	}
}

// Both recovery bytes cast can return, 27 and 28, recover the signer.
func TestCastRecoveryBytes(t *testing.T) {
	cast := castPath(t)
	h := newHarness(t)
	path := writeSheet(t, pricedFile)
	seen := map[byte]int{}
	for epoch := uint64(1); epoch <= 64 && (seen[27] == 0 || seen[28] == 0); epoch++ {
		line, _ := h.message(t, path, epoch, "5000")
		text := castOutput(t, cast, "wallet", "sign", "--private-key", hexKey(h.key), line)
		signature := decodeSignature(t, text)
		seen[signature[64]]++
		sheet, err := pricesheet.Parse([]byte(pricedFile))
		if err != nil {
			t.Fatal(err)
		}
		entry := pricesheet.Entry{EffectiveEpoch: epoch, EpochCapRao: 5_000_000_000_000, Tiers: sheet.Mirror, Signature: signature}
		if err := entry.Verify(964, 25, h.signer); err != nil {
			t.Fatal(err)
		}
	}
	if seen[27] == 0 || seen[28] == 0 || len(seen) != 2 {
		t.Fatalf("recovery bytes seen: %v", seen)
	}
}

// The printed command, run as a shell runs it with only the wallet swapped, signs the
// exact line: the quoting leaves it whole in the shells the owner might use.
func TestThePrintedCommandSurvivesTheShell(t *testing.T) {
	cast := castPath(t)
	h := newHarness(t)
	path := writeSheet(t, pricedFile)
	line, command := h.message(t, path, 9, "5000")
	if !regexp.MustCompile(`^[A-Za-z0-9 .,:]+$`).MatchString(line) {
		t.Fatalf("a character a shell might read: %s", line)
	}
	const ledger = `--ledger --mnemonic-derivation-path "` + ledgerPathBlank + `"`
	if !strings.HasPrefix(command, "cast wallet sign "+ledger+` "`) {
		t.Fatal(command)
	}
	help := castOutput(t, cast, "wallet", "sign", "--help")
	for _, flag := range []string{"--ledger", "--mnemonic-derivation-path"} {
		if !strings.Contains(help, flag) {
			t.Fatalf("this cast has no %s", flag)
		}
	}
	command = strings.Replace(command, ledger, "--private-key "+hexKey(h.key), 1)
	ran := 0
	for _, shell := range []string{"sh", "bash", "zsh"} {
		if _, err := exec.LookPath(shell); err != nil {
			continue
		}
		ran++
		out, err := exec.Command(shell, "-c", command).Output()
		if err != nil {
			t.Fatal(shell, err)
		}
		signature := strings.TrimSpace(string(out))
		if signature != castOutput(t, cast, "wallet", "sign", "--private-key", hexKey(h.key), line) {
			t.Fatalf("%s signed something else", shell)
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
	path := writeSheet(t, pricedFile)
	line, _ := h.message(t, path, 9, "5000")
	signature := castOutput(t, cast, "wallet", "sign", "--private-key", hexKey(h.key), line)
	var stdout bytes.Buffer
	args := []string{"attach", "--in=" + path, "--epoch=9", "--cap=5000", "--signature=" + signature}
	err := run(context.Background(), args, &stdout, dependencies{signerFor: pricesheet.SignerFor})
	if err == nil || !strings.Contains(err.Error(), h.signer.Hex()) || !strings.Contains(err.Error(), "0x16C372dbBb24cd8473345ab13971E40814C8658F") {
		t.Fatal(stdout.String(), err)
	}
	if readSheet(t, path) != pricedFile {
		t.Fatal("a refused attach changed the file")
	}
}

// siteVerdict is what the site's gate and parser make of a file.
type siteVerdict struct {
	Problems []string `json:"problems"`
	Error    string   `json:"error"`
	Sheet    *struct {
		Sn    int `json:"sn"`
		Tiers []struct {
			MinStakeAlpha float64 `json:"minStakeAlpha"`
			Exact         struct {
				MinStake string `json:"minStake"`
				Gib      string `json:"gib"`
				User     string `json:"user"`
			} `json:"exact"`
		} `json:"tiers"`
		Signed []struct {
			EffectiveEpoch string `json:"effectiveEpoch"`
			CapAlpha       string `json:"capAlpha"`
		} `json:"signed"`
	} `json:"sheet"`
}

const siteScript = `
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
const site = process.argv[2];
const { parsePriceSheet } = await import(pathToFileURL(site + '/react/src/lib/price.js').href);
const { checkPriceSheet } = await import(pathToFileURL(site + '/scripts/price-gate.mjs').href);
const results = JSON.parse(readFileSync(0, 'utf8')).map((text) => {
    const verdict = { problems: checkPriceSheet(text) };
    try { verdict.sheet = parsePriceSheet(text); } catch (err) { verdict.error = String(err.message); }
    return verdict;
});
console.log(JSON.stringify(results));
`

func askTheSite(t *testing.T, site string, files []string) []siteVerdict {
	t.Helper()
	script := filepath.Join(t.TempDir(), "ask.mjs")
	if err := os.WriteFile(script, []byte(siteScript), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", script, site)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	var verdicts []siteVerdict
	if err := json.Unmarshal(output, &verdicts); err != nil {
		t.Fatalf("%v\n%s", err, output)
	}
	if len(verdicts) != len(files) {
		t.Fatal(len(verdicts), len(files))
	}
	return verdicts
}

// The file attach writes reads the same through the site's own parser, and its gate
// passes it. What this tool refuses, apart from a bad signature, the gate refuses too.
// The site lives in another repository, so this runs when told where:
//
//	PRICESHEET_SITE_DIR=/path/to/web/ur.xyz go test ./cli/pricesheet -run TheSite
func TestTheSiteReadsWhatAttachWrites(t *testing.T) {
	site := os.Getenv("PRICESHEET_SITE_DIR")
	if site == "" {
		t.Skip("set PRICESHEET_SITE_DIR to the site's ur.xyz directory")
	}
	h := newHarness(t)
	path := writeSheet(t, pricedFile)
	h.attach(t, path, 9, "5000")
	h.attach(t, path, 20, "6000.5")
	h.attach(t, path, 31, "7000")
	good := readSheet(t, path)
	verdict := askTheSite(t, site, []string{good})[0]
	if len(verdict.Problems) != 0 || verdict.Error != "" || verdict.Sheet == nil {
		t.Fatalf("%+v", verdict)
	}
	sheet := verdict.Sheet
	if sheet.Sn != 25 || len(sheet.Tiers) != 2 || sheet.Tiers[0].MinStakeAlpha != 0 || sheet.Tiers[1].MinStakeAlpha != 1000 ||
		sheet.Tiers[0].Exact.Gib != "0.021605" || sheet.Tiers[0].Exact.User != "0.005401" ||
		sheet.Tiers[1].Exact.Gib != "0.0108025" || sheet.Tiers[1].Exact.User != "0.0027005" {
		t.Fatalf("the site does not show the rates: %+v", sheet)
	}
	if len(sheet.Signed) != 3 || sheet.Signed[0].EffectiveEpoch != "9" || sheet.Signed[1].EffectiveEpoch != "20" || sheet.Signed[2].EffectiveEpoch != "31" || sheet.Signed[1].CapAlpha != "6000.5" {
		t.Fatalf("the site does not show the entries: %+v", sheet.Signed)
	}

	mirrorEnd := strings.Index(good, "signed_schedule:")
	mirror, schedule := good[:mirrorEnd], good[mirrorEnd:]
	with := func(old, next string) string {
		if !strings.Contains(good, old) {
			t.Fatalf("no %q in:\n%s", old, good)
		}
		return strings.Replace(good, old, next, 1)
	}
	inMirror := func(old, next string) string {
		if !strings.Contains(mirror, old) {
			t.Fatalf("no %q in the mirror", old)
		}
		return strings.Replace(mirror, old, next, 1) + schedule
	}
	inEntry := func(old, next string) string {
		if !strings.Contains(schedule, old) {
			t.Fatalf("no %q in the schedule", old)
		}
		return mirror + strings.Replace(schedule, old, next, 1)
	}
	refusedByBoth := map[string]string{
		"wrong sn":                    with("sn: 25", "sn: 26"),
		"no sn":                       with("sn: 25\n", ""),
		"sn text":                     with("sn: 25", "sn: abc"),
		"no zero tier":                inMirror("  0:\n", "  1:\n"),
		"rising rate":                 inMirror("alpha_per_gib: 0.0108025", "alpha_per_gib: 0.03"),
		"partly priced":               inMirror("alpha_per_gib: 0.0108025", "alpha_per_gib: 0"),
		"negative rate":               inMirror("alpha_per_gib: 0.021605", "alpha_per_gib: -0.021605"),
		"misspelt rate":               inMirror("alpha_per_gib: 0.021605", "alpha_per_gb: 0.021605"),
		"missing rate":                inMirror("    alpha_per_user: 0.005401\n", ""),
		"repeated rate":               inMirror("    alpha_per_user: 0.005401\n", "    alpha_per_user: 0.005401\n    alpha_per_user: 0.005401\n"),
		"quoted rate":                 inMirror("alpha_per_gib: 0.021605", `alpha_per_gib: "0.021605"`),
		"exponent rate":               inMirror("alpha_per_gib: 0.021605", "alpha_per_gib: 2.1605e-2"),
		"split rate":                  inMirror("alpha_per_gib: 0.021605", "alpha_per_gib: 0_1"),
		"hex rate":                    inMirror("alpha_per_gib: 0.021605", "alpha_per_gib: 0x1"),
		"null rate":                   inMirror("alpha_per_gib: 0.021605", "alpha_per_gib: null"),
		"bare leading dot":            inMirror("alpha_per_gib: 0.021605", "alpha_per_gib: .021605"),
		"overflowing rate":            inMirror("alpha_per_gib: 0.021605", "alpha_per_gib: 18446744074"),
		"twenty-nine places":          inMirror("alpha_per_gib: 0.021605", "alpha_per_gib: 0.02160500000000000000000000000"),
		"fractional rao threshold":    inMirror("  1000:", "  1000.0000000001:"),
		"tiers reversed":              mirror[:strings.Index(mirror, "  0:\n")] + "  1000:\n    alpha_per_gib: 0.0108025\n    alpha_per_user: 0.0027005\n  0:\n    alpha_per_gib: 0.021605\n    alpha_per_user: 0.005401\n" + schedule,
		"mirror differs":              inMirror("alpha_per_gib: 0.021605", "alpha_per_gib: 0.022"),
		"mirror differs past doubles": inMirror("alpha_per_gib: 0.021605", "alpha_per_gib: 0.0216050000000000001"),
		"mirror threshold differs":    inMirror("  1000:", "  1001:"),
		"no mirror":                   "sn: 25\n" + schedule,
		"misspelt mirror":             inMirror("tiered_per_block_demand_deposits:", "tiered_per_block_demand_deposit:"),
		"misspelt schedule":           with("signed_schedule:", "signed_schedulee:"),
		"unknown field":               good + "extra: 1\n",
		"no signature":                inEntry(`    signature: "`, `    sig: "`),
		"long signature":              inEntry(`signature: "0x`, `signature: "0x00`),
		"unknown entry field":         inEntry("    tiers:\n", "    surprise: 1\n    tiers:\n"),
		"zero cap":                    inEntry("epoch_cap_alpha: 5000", "epoch_cap_alpha: 0"),
		"no cap":                      inEntry("    epoch_cap_alpha: 5000\n", ""),
		"fractional rao cap":          inEntry("epoch_cap_alpha: 5000", "epoch_cap_alpha: 5000.0000000001"),
		"epochs out of order":         inEntry("  9:\n", "  99:\n"),
		"repeated epoch":              inEntry("  20:\n", "  9:\n"),
		"epoch text":                  inEntry("  9:\n", "  soon:\n"),
		"two documents":               good + "---\nsn: 25\n",
		"not a sheet":                 "<html>",
	}
	refusedByTheToolOnly := map[string]string{
		"changed entry rate": inEntry("alpha_per_user: 0.005401", "alpha_per_user: 0.0054"),
		"changed entry cap":  inEntry("epoch_cap_alpha: 5000", "epoch_cap_alpha: 5001"),
		"forged":             forge(t, newHarness(t), good),
	}
	refusedByTheSiteOnly := map[string]string{
		"rates nobody signed": pricedFile,
		"flow rates":          "sn: 25\ntiered_per_block_demand_deposits:\n  0: {alpha_per_gib: 0, alpha_per_user: 0}\n",
		"carriage returns":    strings.ReplaceAll(launchFile, "\n", "\r\n"),
		"a tab":               strings.Replace(launchFile, "sn: 25", "sn:\t25", 1),
	}
	var names, files []string
	for _, cases := range []map[string]string{refusedByBoth, refusedByTheToolOnly, refusedByTheSiteOnly} {
		for name, file := range cases {
			names = append(names, name)
			files = append(files, file)
		}
	}
	verdicts := askTheSite(t, site, files)
	for i, name := range names {
		_, toolErr := h.run("verify", "--in="+writeSheet(t, files[i]))
		_, both := refusedByBoth[name]
		_, toolOnly := refusedByTheToolOnly[name]
		_, siteOnly := refusedByTheSiteOnly[name]
		switch {
		case both && (toolErr == nil || len(verdicts[i].Problems) == 0):
			t.Errorf("%s: tool %v, gate %q", name, toolErr, verdicts[i].Problems)
		case toolOnly && (toolErr == nil || len(verdicts[i].Problems) != 0):
			t.Errorf("%s: tool %v, gate %q", name, toolErr, verdicts[i].Problems)
		case siteOnly && (toolErr != nil || len(verdicts[i].Problems) == 0):
			t.Errorf("%s: tool %v, gate %q", name, toolErr, verdicts[i].Problems)
		}
	}
}
