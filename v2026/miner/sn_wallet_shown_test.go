package miner

// The consent a person signs in their own wallet must be the bytes the CLI
// submits, and the command it prints must run as printed in every shell a
// provider uses, Windows PowerShell 5.1 and cmd.exe included. A refusal on
// this host names the check that failed (support inbox 8177).

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
)

// One typed line per Read, the way a terminal delivers them: every prompt
// reads through a buffered reader of its own.
type setupTypedLines struct {
	lines []string
}

func (self *setupTypedLines) Read(p []byte) (int, error) {
	if len(self.lines) == 0 {
		return 0, io.EOF
	}
	n := copy(p, self.lines[0])
	self.lines[0] = self.lines[0][n:]
	if self.lines[0] == "" {
		self.lines = self.lines[1:]
	}
	return n, nil
}

// Characters that POSIX shells, Windows PowerShell 5.1 and 7, and cmd.exe all
// pass to a program unchanged and unsplit, so an argument made of them needs
// no quoting. A backslash is a path separator only on Windows.
func walletShellNeutralTest(argument string) bool {
	neutral := `^[A-Za-z0-9._/:=+-]+$`
	if runtime.GOOS == "windows" {
		neutral = `^[A-Za-z0-9._/:=+\\-]+$`
	}
	return regexp.MustCompile(neutral).MatchString(argument)
}

// Setup shows the message to sign. A person signs what is shown, so it must
// be the submitted message byte for byte: no indentation on any line.
func TestSetupShowsExactlyTheMessageItSubmits(t *testing.T) {
	t.Setenv("URNETWORK_STATE_DIR", t.TempDir())
	keypair := testAliceKeypair(t)
	message := protocol.WalletMappingConsentPrefix + `{"schema":"synthetic","nonce":"AAAA","from_epoch":3}`
	signature, err := snSignWalletChallenge(keypair, message)
	if err != nil {
		t.Fatal(err)
	}
	var submitted []string
	previous := runSelf
	t.Cleanup(func() { runSelf = previous })
	runSelf = func(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
		switch {
		case 2 <= len(args) && args[0] == "wallet" && args[1] == "challenge":
			_, err := io.WriteString(stdout, "Sign this message with the coldkey "+testAliceAddress+".\n\n----- message -----\n"+message+"\n----- end -----\n")
			return err
		case 2 <= len(args) && args[0] == "wallet" && args[1] == "set":
			submitted = args
			return nil
		}
		t.Errorf("setup ran an unexpected step %v", args)
		return errors.New("unexpected step")
	}
	var out bytes.Buffer
	setupPayout(&setupRun{in: &setupTypedLines{lines: []string{testAliceAddress + "\n", signature + "\n"}}, out: &out, errOut: &out})
	shown := out.String()
	if !strings.Contains(shown, "\n"+message+"\n") {
		t.Fatalf("setup shows a message to sign that differs from the bytes it submits:\n%s", shown)
	}
	if !slices.Contains(submitted, "--message="+message) || !slices.Contains(submitted, "--signature="+signature) {
		t.Fatalf("setup submitted %q", submitted)
	}
}

// The printed submit command, with only its signature placeholder filled in,
// runs as printed: every argument is one no shell changes or splits, and the
// wallet is set with the consent the person signed.
func TestWalletChallengePrintsACommandEveryShellRunsAsPrinted(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	token, key, _ := financialWalletMappingTestProvider(t)
	f := newFinancialWalletMappingHttp(t, token, key.Address(), key.PublicKey())
	strategy := connect.NewClientStrategy(ctx, swarmWalletTestSettings())
	defer strategy.Close()
	var out bytes.Buffer
	if err := snPrintProviderWalletChallenge(ctx, strategy, f.endpoint.URL, key.Address(), &snWalletProof{}, &out); err != nil {
		t.Fatal(err)
	}
	printed := out.String()
	match := setupMessagePattern.FindStringSubmatch(printed)
	if match == nil {
		t.Fatalf("no message between the markers:\n%s", printed)
	}
	// a Polkadot extension's signRaw signs the printed message <Bytes>-wrapped
	signature, err := snSignWalletChallenge(key, match[1])
	if err != nil {
		t.Fatal(err)
	}
	command := ""
	for _, line := range strings.Split(printed, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "provider wallet set ") {
			command = strings.TrimSpace(line)
		}
	}
	if !strings.Contains(command, "0x<128 hex chars>") {
		t.Fatalf("no submit command with a signature placeholder:\n%s", printed)
	}
	words := strings.Fields(strings.Replace(command, "0x<128 hex chars>", signature, 1))
	for _, word := range words {
		if !walletShellNeutralTest(word) {
			t.Fatalf("the submit command has an argument that needs a shell's quoting, which Windows PowerShell 5.1 and cmd.exe don't apply as printed: %q in\n%s", word, command)
		}
	}
	opts := parseArgsForTest(t, words[1:])
	proof, err := snWalletProofFromOpts(opts)
	if err != nil {
		t.Fatal(err)
	}
	apiUrl, _ := opts.String("--api_url")
	coldkey, _ := opts.String("<coldkey_ss58>")
	if err := financialWalletMappingTestSet(t, ctx, apiUrl, coldkey, proof); err != nil {
		t.Fatal("the printed command didn't set the wallet:", err)
	}
	journal, err := financialWalletMappingTestJournal(ctx, filepath.Join(os.Getenv("URNETWORK_STATE_DIR"), ".provider.jwt"), f.endpoint.URL)
	if err != nil || len(journal.Records) != 1 || !journal.Records[0].Acknowledged || journal.Records[0].Original.Message != match[1] {
		t.Fatal("the printed command's consent isn't the acknowledged original", err)
	}
}

// The hotkey consent's printed command runs as printed too, and sets the chain
// with the statement the coldkey signed.
func TestHotkeyWalletChallengePrintsACommandEveryShellRunsAsPrinted(t *testing.T) {
	setup := newTestHotkeyWalletSetup(t, 1, nil)
	pending, printed := setup.challenge()
	message, err := pending.Message()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := setup.coldkey.Sign([]byte(message))
	if err != nil {
		t.Fatal(err)
	}
	command := ""
	for _, line := range strings.Split(printed, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "provider wallet hotkey set ") {
			command = strings.TrimSpace(line)
		}
	}
	if !strings.Contains(command, "0x<128 hex chars>") {
		t.Fatalf("no submit command with a signature placeholder:\n%s", printed)
	}
	words := strings.Fields(strings.Replace(command, "0x<128 hex chars>", "0x"+hex.EncodeToString(signature), 1))
	for _, word := range words {
		if !walletShellNeutralTest(word) {
			t.Fatalf("the submit command has an argument that needs a shell's quoting, which Windows PowerShell 5.1 and cmd.exe don't apply as printed: %q in\n%s", word, command)
		}
	}
	var out bytes.Buffer
	if err := hotkeyWalletSet(t.Context(), parseArgsForTest(t, words[1:]), &out); err != nil {
		t.Fatalf("the printed command didn't set the chain: %v\n%s", err, out.String())
	}
	if chain, _, _ := setup.chain(); len(chain) != 1 || chain[0].Message != message {
		t.Fatalf("chain = %+v", chain)
	}
}

// A consent refused on this host says which check refused it, so nobody
// takes it for a server-side mapping; nothing is retained or sent.
func TestWalletSetNamesTheLocalCheckThatRefusedTheConsent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	token, key, _ := financialWalletMappingTestProvider(t)
	f := newFinancialWalletMappingHttp(t, token, key.Address(), key.PublicKey())
	other, err := crv4.KeypairFromSeed([32]byte{77})
	if err != nil {
		t.Fatal(err)
	}
	message := financialWalletMappingTestMessage(t, token, key.PublicKey(), 3, 20, 9)
	otherMessage := financialWalletMappingTestMessage(t, token, other.PublicKey(), 3, 20, 10)
	for _, c := range []struct {
		name    string
		message string
		signer  *crv4.Keypair
		signed  string
		want    string
	}{
		{"signed as shown, indented", message, key, "    " + strings.ReplaceAll(message, "\n", "\n    "), "the signature doesn't verify"},
		{"quotes dropped on the way", strings.ReplaceAll(message, `"`, ""), key, message, "changed on the way"},
		{"another coldkey's challenge", otherMessage, other, otherMessage, "issued for coldkey " + other.Address()},
	} {
		signature, err := snSignWalletChallenge(c.signer, c.signed)
		if err != nil {
			t.Fatal(err)
		}
		err = financialWalletMappingTestSet(t, ctx, f.endpoint.URL, key.Address(), &snWalletProof{Message: c.message, Signature: signature})
		if err == nil || !errors.Is(err, protocol.ErrWalletMappingIntegrity) || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: refusal %q doesn't say %q", c.name, err, c.want)
		}
	}
	f.stateLock.Lock()
	challenges, posts := len(f.challengeRequests), len(f.walletBodies)
	f.stateLock.Unlock()
	if challenges != 0 || posts != 0 {
		t.Fatal("a refused consent reached the server", challenges, posts)
	}
	journal, err := financialWalletMappingTestJournal(ctx, filepath.Join(os.Getenv("URNETWORK_STATE_DIR"), ".provider.jwt"), f.endpoint.URL)
	if err != nil || len(journal.Records) != 0 {
		t.Fatal("a refused consent was retained", err)
	}
}

// --message-file passes the challenge without a shell: the file's text, with
// a CRLF or a final line break that an editor adds taken off.
func TestWalletSetReadsTheMessageFromAFile(t *testing.T) {
	dir := t.TempDir()
	message := protocol.WalletMappingConsentPrefix + `{"schema":"synthetic","nonce":"AAAA"}`
	signature := "0x" + strings.Repeat("ab", 64)
	for name, content := range map[string]string{
		"exact":               message,
		"final line break":    message + "\n",
		"crlf line endings":   strings.ReplaceAll(message, "\n", "\r\n") + "\r\n",
		"one-line \\n escape": snEscapeMessage(message),
	} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".txt")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		proof, err := snWalletProofFromOpts(parseArgsForTest(t, []string{"wallet", "set", testAliceAddress, "--message-file=" + path, "--signature=" + signature}))
		if err != nil || snUnescapeMessage(proof.Message) != message || proof.Signature != signature {
			t.Fatalf("%s: message %q, %v", name, proof.Message, err)
		}
	}
	if _, err := snWalletProofFromOpts(parseArgsForTest(t, []string{"wallet", "set", testAliceAddress, "--message-file=" + filepath.Join(dir, "absent.txt"), "--signature=" + signature})); err == nil {
		t.Fatal("a missing message file was accepted")
	}
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler}
	for _, arguments := range [][]string{
		{"wallet", "set", testAliceAddress, "--message-file=/synthetic/challenge.txt"},
		{"wallet", "set", testAliceAddress, "--message-file=/synthetic/challenge.txt", "--message=synthetic consent", "--signature=" + signature},
		{"wallet", "set", testAliceAddress, "--coldkey_seed_file=/synthetic/coldkey.seed", "--message-file=/synthetic/challenge.txt", "--signature=" + signature},
	} {
		if _, err := parser.ParseArgs(mainUsage(), arguments, "synthetic"); err == nil {
			t.Fatalf("parse %v: accepted", arguments)
		} else if _, ok := err.(*docopt.UserError); !ok {
			t.Fatalf("parse %v: want user syntax refusal, got %T: %v", arguments, err, err)
		}
	}
}

// A value no shell changes is printed bare; another is quoted for the shell
// of the platform the command runs on.
func TestWalletShellValueIsBareWhenNoShellChangesIt(t *testing.T) {
	for _, value := range []string{"https://api.bringyour.com", "/home/miner/.urnetwork/.provider.jwt", "C:/Users/miner/.urnetwork/.provider.jwt"} {
		if got := snWalletShellValue(value); got != value {
			t.Fatalf("%q printed as %q", value, got)
		}
	}
	spaced := "/home/a miner/.urnetwork/.provider.jwt"
	want := "'" + spaced + "'"
	if runtime.GOOS == "windows" {
		want = `"` + spaced + `"`
	}
	if got := snWalletShellValue(spaced); got != want {
		t.Fatalf("%q printed as %q, want %q", spaced, got, want)
	}
	if runtime.GOOS == "windows" {
		if path := `C:\Users\miner\.urnetwork\.provider.jwt`; snWalletShellValue(path) != path {
			t.Fatalf("%q printed as %q", path, snWalletShellValue(path))
		}
	}
}
