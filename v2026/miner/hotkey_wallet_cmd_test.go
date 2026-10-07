package miner

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/clientauth"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/hotkeywallet"
	"github.com/urfoundation/sn/v2026/operatorlist"
	"github.com/urfoundation/sn/v2026/protocol"
)

// A miner with a hotkey and a coldkey in seed files and its operators in a
// served list; every operator is authenticated unless a test says otherwise.
type testHotkeyWalletSetup struct {
	t               *testing.T
	base            string
	hotkey          *crv4.Keypair
	coldkey         *crv4.Keypair
	hotkeySeedPath  string
	coldkeySeedPath string
	operators       []*testHotkeyWalletOperator
	domains         []string
	listUrl         string
}

func newTestHotkeyWalletSetup(t *testing.T, count int, configure func(index int, operator *testHotkeyWalletOperator)) *testHotkeyWalletSetup {
	t.Helper()
	base := t.TempDir()
	t.Setenv("URNETWORK_STATE_DIR", base)
	self := &testHotkeyWalletSetup{
		t:               t,
		base:            base,
		hotkey:          testHotkey(t, 0x31),
		coldkey:         testHotkey(t, 0x41),
		hotkeySeedPath:  writeTestSeedFile(t, testSeedHex(0x31)),
		coldkeySeedPath: writeTestSeedFile(t, testSeedHex(0x41)),
	}
	names := []string{"one", "two", "three"}
	var listed []operatorlist.Operator
	for index := range count {
		operator := newTestHotkeyWalletOperator(t, byte(index+1), func(operator *testHotkeyWalletOperator) {
			if configure != nil {
				configure(index, operator)
			}
		})
		domain := names[index] + ".example"
		operator.authenticate(t, base, domain)
		self.operators = append(self.operators, operator)
		self.domains = append(self.domains, domain)
		listed = append(listed, operator.listed(domain))
	}
	self.listUrl = testServeOperatorList(t, listed...)
	return self
}

func (self *testHotkeyWalletSetup) coldkeySs58() string {
	return self.coldkey.Address()
}

// Parses the command against the real usage, following the served list.
func (self *testHotkeyWalletSetup) opts(arguments ...string) docopt.Opts {
	self.t.Helper()
	return parseArgsForTest(self.t, append(arguments, "--operators-url="+self.listUrl))
}

func (self *testHotkeyWalletSetup) challenge() (*protocol.HotkeyWalletMappingStatement, string) {
	self.t.Helper()
	var out bytes.Buffer
	if err := hotkeyWalletChallenge(self.t.Context(), self.opts("wallet", "hotkey", "challenge", self.coldkeySs58(), "--hotkey_seed_file="+self.hotkeySeedPath), &out); err != nil {
		self.t.Fatal(err)
	}
	pending, err := hotkeyWalletStore(self.base).Pending()
	if err != nil || pending == nil {
		self.t.Fatalf("no pending statement: %v", err)
	}
	return pending, out.String()
}

func (self *testHotkeyWalletSetup) setOffline(message string, signature []byte, extra ...string) (string, error) {
	self.t.Helper()
	var out bytes.Buffer
	arguments := append([]string{"wallet", "hotkey", "set", self.coldkeySs58(), "--hotkey_seed_file=" + self.hotkeySeedPath, "--message=" + snEscapeMessage(message), "--signature=0x" + hex.EncodeToString(signature)}, extra...)
	err := hotkeyWalletSet(self.t.Context(), self.opts(arguments...), &out)
	return out.String(), err
}

func (self *testHotkeyWalletSetup) setWithSeed() (string, error) {
	self.t.Helper()
	var out bytes.Buffer
	err := hotkeyWalletSet(self.t.Context(), self.opts("wallet", "hotkey", "set", self.coldkeySs58(), "--hotkey_seed_file="+self.hotkeySeedPath, "--coldkey_seed_file="+self.coldkeySeedPath), &out)
	return out.String(), err
}

func (self *testHotkeyWalletSetup) chain() ([]protocol.HotkeyWalletMappingConsent, *protocol.HotkeyWalletMappingStatement, [32]byte) {
	self.t.Helper()
	chain, err := hotkeyWalletStore(self.base).Chain()
	if err != nil || len(chain) == 0 {
		self.t.Fatalf("no hotkey wallet chain: %v", err)
	}
	head, headHash, err := protocol.VerifyHotkeyWalletMappingLineage(self.t.Context(), chain)
	if err != nil {
		self.t.Fatal(err)
	}
	return chain, head, headHash
}

// The operator delegates the network to this head, over these epochs.
func (self *testHotkeyWalletSetup) requireDelegation(operator *testHotkeyWalletOperator, headHash [32]byte, generation uint64, from uint64) {
	self.t.Helper()
	delegation := operator.delegationHead(self.t)
	if delegation == nil || delegation.Hotkey != self.hotkey.PublicKey() || delegation.ConsentHeadHash != headHash || delegation.ConsentGeneration != generation || delegation.FromEpoch != from || delegation.ThroughEpoch != from+65535 {
		self.t.Fatalf("delegation = %+v; want generation %d head %x from %d", delegation, generation, headHash, from)
	}
}

func TestMainUsageHotkeyWalletCommands(t *testing.T) {
	challenge := parseArgsForTest(t, []string{"wallet", "hotkey", "challenge", "synthetic-coldkey", "--hotkey_seed_file=/synthetic/hotkey.seed", "--wallet-from-epoch=3", "--wallet-through-epoch=9", "--operators-url=https://list.example/operators.yml", "-v"})
	for flag, want := range map[string]bool{"wallet": true, "hotkey": true, "challenge": true, "set": false, "status": false} {
		if value, _ := challenge.Bool(flag); value != want {
			t.Fatalf("challenge %s = %v", flag, value)
		}
	}
	for option, want := range map[string]string{"<coldkey_ss58>": "synthetic-coldkey", "--hotkey_seed_file": "/synthetic/hotkey.seed", "--wallet-from-epoch": "3", "--wallet-through-epoch": "9"} {
		if value, err := challenge.String(option); err != nil || value != want {
			t.Fatalf("challenge %s = %q, %v", option, value, err)
		}
	}
	for _, arguments := range [][]string{
		{"wallet", "hotkey", "set", "synthetic-coldkey", "--hotkey_seed_file=/synthetic/hotkey.seed", "--coldkey_seed_file=/synthetic/coldkey.seed"},
		{"wallet", "hotkey", "set", "synthetic-coldkey", "--hotkey_seed_file=/synthetic/hotkey.seed", "--message=synthetic", "--signature=00"},
		{"wallet", "hotkey", "status"},
		{"wallet", "hotkey", "status", "--hotkey_seed_file=/synthetic/hotkey.seed", "--operators-url=https://list.example/operators.yml"},
	} {
		if hotkey, _ := parseArgsForTest(t, arguments).Bool("hotkey"); !hotkey {
			t.Fatalf("parse %v: hotkey not selected", arguments)
		}
	}
	if hotkey, _ := parseArgsForTest(t, []string{"wallet", "set", "synthetic-coldkey"}).Bool("hotkey"); hotkey {
		t.Fatal("the provider wallet set selected the hotkey wallet")
	}
	parser := &docopt.Parser{HelpHandler: docopt.NoHelpHandler}
	for _, arguments := range [][]string{
		{"wallet", "hotkey", "challenge", "synthetic-coldkey"},
		{"wallet", "hotkey", "set", "synthetic-coldkey", "--hotkey_seed_file=/synthetic/hotkey.seed", "--coldkey_seed_file=/synthetic/coldkey.seed", "--message=synthetic", "--signature=00"},
		{"wallet", "hotkey", "set", "synthetic-coldkey", "--hotkey_seed_file=/synthetic/hotkey.seed", "--message=synthetic"},
		{"wallet", "hotkey", "status", "synthetic-coldkey"},
	} {
		if _, err := parser.ParseArgs(mainUsage(), arguments, "synthetic"); err == nil {
			t.Errorf("parse %v: accepted", arguments)
		}
	}
	for _, epochs := range [][]string{{"--wallet-from-epoch=3"}, {"--wallet-from-epoch=9", "--wallet-through-epoch=3"}, {"--wallet-from-epoch=0", "--wallet-through-epoch=65536"}} {
		opts := parseArgsForTest(t, append([]string{"wallet", "hotkey", "challenge", "synthetic-coldkey", "--hotkey_seed_file=/synthetic/hotkey.seed"}, epochs...))
		if _, err := hotkeyWalletEpochsFromOpts(opts); err == nil {
			t.Errorf("epochs %v accepted", epochs)
		}
	}
}

// The coldkey signs the printed statement elsewhere, raw rather than
// <Bytes>-wrapped, and one operator already holds a network consent the
// subnet read has to step past.
func TestHotkeyWalletChallengeThenSetWithAnOfflineColdkeySignature(t *testing.T) {
	setup := newTestHotkeyWalletSetup(t, 2, func(index int, operator *testHotkeyWalletOperator) {
		if index == 1 {
			operator.networkConsentFrom = 60
		}
	})
	pending, printed := setup.challenge()
	message, err := pending.Message()
	if err != nil {
		t.Fatal(err)
	}
	if pending.Generation != 1 || pending.FromEpoch != 0 || pending.ThroughEpoch != 65535 || pending.Subnet != testWalletDomain.HotkeySubnet() || pending.Hotkey != setup.hotkey.PublicKey() || pending.Coldkey != setup.coldkey.PublicKey() {
		t.Fatalf("pending statement = %+v", pending)
	}
	for _, line := range []string{
		"----- message -----\n" + message + "\n----- end -----\n",
		"provider wallet hotkey set " + setup.coldkeySs58() + " --hotkey_seed_file='" + setup.hotkeySeedPath + "' --message='" + snEscapeMessage(message) + "'",
	} {
		if !strings.Contains(printed, line) {
			t.Fatalf("challenge output lacks %q:\n%s", line, printed)
		}
	}

	signature, err := setup.coldkey.Sign([]byte(message))
	if err != nil {
		t.Fatal(err)
	}
	out, err := setup.setOffline(message, signature)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	chain, head, headHash := setup.chain()
	if len(chain) != 1 || chain[0].Message != message || head.Coldkey != setup.coldkey.PublicKey() {
		t.Fatalf("chain = %+v", chain)
	}
	for index, operator := range setup.operators {
		setup.requireDelegation(operator, headHash, 1, testWalletEpoch+2)
		if want := fmt.Sprintf("operator %s: chain stored; delegated to generation 1 from epoch 53 through 65588\n", setup.domains[index]); !strings.Contains(out, want) {
			t.Fatalf("set output lacks %q:\n%s", want, out)
		}
	}
	if pending, err := hotkeyWalletStore(setup.base).Pending(); err != nil || pending != nil {
		t.Fatalf("the signed statement is still pending: %v, %v", pending, err)
	}
}

// Without a challenge, set builds and signs the statement itself; the next
// generation starts after the operators' current epoch and the delegation
// moves past the previous delegation's start.
func TestHotkeyWalletSetSignsWithTheColdkeySeedFileAndExtendsTheChain(t *testing.T) {
	setup := newTestHotkeyWalletSetup(t, 1, nil)
	if out, err := setup.setWithSeed(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	_, _, firstHead := setup.chain()
	setup.requireDelegation(setup.operators[0], firstHead, 1, testWalletEpoch+2)

	out, err := setup.setWithSeed()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	chain, head, headHash := setup.chain()
	if len(chain) != 2 || head.Generation != 2 || head.FromEpoch != testWalletEpoch+1 || head.ThroughEpoch != testWalletEpoch+1+65535 {
		t.Fatalf("second generation = %+v", head)
	}
	setup.requireDelegation(setup.operators[0], headHash, 2, testWalletEpoch+3)
	if !strings.Contains(out, "operator one.example: chain stored; delegated to generation 2 from epoch 54 through 65589\n") {
		t.Fatalf("set output:\n%s", out)
	}
}

// A failing operator is reported and the others still adopt the head; setting
// the same signed statement again resends the stored head without a new
// generation.
func TestHotkeyWalletSetKeepsGoingPastAFailingOperatorAndResends(t *testing.T) {
	setup := newTestHotkeyWalletSetup(t, 3, func(index int, operator *testHotkeyWalletOperator) {
		operator.unavailable = index == 1
	})
	pending, _ := setup.challenge()
	message, _ := pending.Message()
	signature, err := hotkeywallet.Sign(setup.coldkey, message)
	if err != nil {
		t.Fatal(err)
	}
	out, err := setup.setOffline(message, signature[:])
	if err == nil || !strings.Contains(err.Error(), "1 of 3 authenticated operators do not adopt the head yet") {
		t.Fatalf("set with a failing operator: %v\n%s", err, out)
	}
	_, _, headHash := setup.chain()
	setup.requireDelegation(setup.operators[0], headHash, 1, testWalletEpoch+2)
	setup.requireDelegation(setup.operators[2], headHash, 1, testWalletEpoch+2)
	if !strings.Contains(out, "operator two.example: failed: storing the chain: ") {
		t.Fatalf("set output does not report the failing operator:\n%s", out)
	}

	setup.operators[1].setUnavailable(false)
	out, err = setup.setOffline(message, signature[:])
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if chain, _, _ := setup.chain(); len(chain) != 1 {
		t.Fatalf("resending appended generation %d", len(chain))
	}
	setup.requireDelegation(setup.operators[1], headHash, 1, testWalletEpoch+2)
	for _, line := range []string{
		"operator one.example: chain stored; the delegation already adopts generation 1\n",
		"operator two.example: chain stored; delegated to generation 1 from epoch 53 through 65588\n",
	} {
		if !strings.Contains(out, line) {
			t.Fatalf("resend output lacks %q:\n%s", line, out)
		}
	}
	if _, accepts := setup.operators[0].counts(); accepts != 1 {
		t.Fatalf("an adopted delegation was signed again: %d accepts", accepts)
	}
}

func TestHotkeyWalletStatusShowsTheChainAndEachOperator(t *testing.T) {
	setup := newTestHotkeyWalletSetup(t, 3, func(index int, operator *testHotkeyWalletOperator) {
		operator.unavailable = index == 1
	})
	// never authenticated
	if err := clientauth.RemoveToken(operatorJwtPath(setup.base, setup.domains[2])); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.setWithSeed(); err == nil {
		t.Fatal("set did not report the failing operator")
	}
	setup.operators[1].setUnavailable(false)
	chain, head, headHash := setup.chain()
	_, originalHash, err := protocol.VerifyHotkeyWalletMappingConsent(t.Context(), chain[0])
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := hotkeyWalletStatus(t.Context(), setup.opts("wallet", "hotkey", "status"), &out); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(""+
		"hotkey wallet chain in %s:\n"+
		"  generation 1: coldkey %s, epochs 0 through 65535, original 0x%x\n"+
		"head: generation 1, 0x%x, hotkey %s, chain 7 netuid 25\n"+
		"operator one.example: adopts the head, generation 1, from epoch 53 through 65588\n"+
		"operator two.example: no hotkey delegation\n"+
		"operator three.example: awaiting auth\n",
		filepath.Join(setup.base, "hotkey-wallet"), setup.coldkeySs58(), originalHash, headHash, setup.hotkey.Address())
	if head.Generation != 1 || out.String() != want {
		t.Fatalf("status:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestHotkeyWalletSetRefusesAMismatchedPendingStatement(t *testing.T) {
	setup := newTestHotkeyWalletSetup(t, 1, nil)
	pending, _ := setup.challenge()
	message, _ := pending.Message()
	signature, err := hotkeywallet.Sign(setup.coldkey, message)
	if err != nil {
		t.Fatal(err)
	}
	altered := strings.Replace(message, `"through_epoch":65535`, `"through_epoch":65534`, 1)
	otherColdkey := testHotkey(t, 0x42)
	otherSignature, err := hotkeywallet.Sign(otherColdkey, message)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		message   string
		signature []byte
		extra     []string
		coldkey   string
		want      string
	}{
		{message: altered, signature: signature[:], want: "--message is not the pending statement"},
		{message: message, signature: signature[:], coldkey: otherColdkey.Address(), want: "the pending statement names coldkey " + setup.coldkeySs58()},
		{message: message, signature: signature[:], extra: []string{"--wallet-from-epoch=5", "--wallet-through-epoch=6"}, want: "the pending statement earns epochs 0 through 65535, not 5 through 6"},
		{message: message, signature: otherSignature[:], want: "the coldkey's signature does not verify"},
	} {
		coldkey := setup.coldkeySs58()
		if c.coldkey != "" {
			coldkey = c.coldkey
		}
		arguments := append([]string{"wallet", "hotkey", "set", coldkey, "--hotkey_seed_file=" + setup.hotkeySeedPath, "--message=" + snEscapeMessage(c.message), "--signature=0x" + hex.EncodeToString(c.signature)}, c.extra...)
		var out bytes.Buffer
		if err := hotkeyWalletSet(t.Context(), setup.opts(arguments...), &out); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("set %q: %v; want %q", c.want, err, c.want)
		}
	}
	store := hotkeyWalletStore(setup.base)
	if chain, err := store.Chain(); err != nil || chain != nil {
		t.Fatalf("a refused statement was appended: %v, %v", chain, err)
	}
	if still, err := store.Pending(); err != nil || still == nil || *still != *pending {
		t.Fatalf("the pending statement changed: %+v, %v", still, err)
	}
	if submissions, _ := setup.operators[0].counts(); submissions != 0 {
		t.Fatalf("a refused statement reached the operator %d times", submissions)
	}
	if out, err := setup.setOffline(message, signature[:]); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestHotkeyWalletChallengeRefusesOperatorsThatDisagreeOnTheSubnet(t *testing.T) {
	setup := newTestHotkeyWalletSetup(t, 2, func(index int, operator *testHotkeyWalletOperator) {
		if index == 1 {
			operator.domain.Netuid = 26
		}
	})
	var out bytes.Buffer
	err := hotkeyWalletChallenge(t.Context(), setup.opts("wallet", "hotkey", "challenge", setup.coldkeySs58(), "--hotkey_seed_file="+setup.hotkeySeedPath), &out)
	if err == nil || !strings.Contains(err.Error(), "operators disagree on the subnet: one.example states chain 7") || !strings.Contains(err.Error(), "two.example states chain 7") {
		t.Fatalf("disagreeing operators: %v", err)
	}
	if pending, err := hotkeyWalletStore(setup.base).Pending(); err != nil || pending != nil {
		t.Fatalf("a statement for a disputed subnet is pending: %v, %v", pending, err)
	}
}

// Operators that state genesis_hash in GET /sn/epoch name the subnet there, so
// neither challenge nor set asks them for a network consent challenge.
func TestHotkeyWalletChallengeReadsTheSubnetFromTheEpoch(t *testing.T) {
	setup := newTestHotkeyWalletSetup(t, 2, func(index int, operator *testHotkeyWalletOperator) {
		operator.statesGenesisHash = true
	})
	pending, _ := setup.challenge()
	if pending.Generation != 1 || pending.Subnet != testWalletDomain.HotkeySubnet() {
		t.Fatalf("pending statement = %+v", pending)
	}
	if out, err := setup.setWithSeed(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	_, _, headHash := setup.chain()
	for index, operator := range setup.operators {
		setup.requireDelegation(operator, headHash, 1, testWalletEpoch+2)
		if challenges := operator.networkChallengeCount(); challenges != 0 {
			t.Fatalf("operator %s was asked for %d network consent challenges", setup.domains[index], challenges)
		}
	}
}

// Only the operator that predates genesis_hash in GET /sn/epoch is asked for a
// network consent challenge; the other states the subnet without one.
func TestHotkeyWalletChallengeFallsBackToAChallengeForAnOperatorWithoutGenesisHash(t *testing.T) {
	setup := newTestHotkeyWalletSetup(t, 2, func(index int, operator *testHotkeyWalletOperator) {
		operator.statesGenesisHash = index == 0
	})
	pending, _ := setup.challenge()
	if pending.Generation != 1 || pending.Subnet != testWalletDomain.HotkeySubnet() {
		t.Fatalf("pending statement = %+v", pending)
	}
	for index, want := range []int{0, 1} {
		if challenges := setup.operators[index].networkChallengeCount(); challenges != want {
			t.Fatalf("operator %s was asked for %d network consent challenges, want %d", setup.domains[index], challenges, want)
		}
	}
}

// An operator that states genesis_hash must agree with every other operator,
// whether that one states the subnet in GET /sn/epoch or in a challenge.
func TestHotkeyWalletChallengeRefusesOperatorsThatDisagreeOnTheEpochSubnet(t *testing.T) {
	cases := []struct {
		secondStatesGenesisHash bool
		move                    func(domain *protocol.ClientKeyHistoryDomain)
	}{
		{secondStatesGenesisHash: true, move: func(domain *protocol.ClientKeyHistoryDomain) { domain.GenesisHash = [32]byte{9} }},
		{secondStatesGenesisHash: true, move: func(domain *protocol.ClientKeyHistoryDomain) { domain.ChainID = 8 }},
		{secondStatesGenesisHash: true, move: func(domain *protocol.ClientKeyHistoryDomain) { domain.Netuid = 26 }},
		{secondStatesGenesisHash: false, move: func(domain *protocol.ClientKeyHistoryDomain) { domain.GenesisHash = [32]byte{9} }},
	}
	for _, c := range cases {
		moved := testWalletDomain
		c.move(&moved)
		want := fmt.Sprintf("operators disagree on the subnet: one.example states chain 7 genesis 0x%x netuid 25, two.example states chain %d genesis 0x%x netuid %d", testWalletDomain.GenesisHash, moved.ChainID, moved.GenesisHash, moved.Netuid)
		setup := newTestHotkeyWalletSetup(t, 2, func(index int, operator *testHotkeyWalletOperator) {
			operator.statesGenesisHash = index == 0 || c.secondStatesGenesisHash
			if index == 1 {
				c.move(&operator.domain)
			}
		})
		var out bytes.Buffer
		err := hotkeyWalletChallenge(t.Context(), setup.opts("wallet", "hotkey", "challenge", setup.coldkeySs58(), "--hotkey_seed_file="+setup.hotkeySeedPath), &out)
		if err == nil || err.Error() != want {
			t.Errorf("disagreeing operators: %v; want %s", err, want)
		}
		if pending, err := hotkeyWalletStore(setup.base).Pending(); err != nil || pending != nil {
			t.Errorf("a statement for a disputed subnet is pending: %v, %v", pending, err)
		}
		wantChallenges := []int{0, 0}
		if !c.secondStatesGenesisHash {
			wantChallenges[1] = 1
		}
		for index, operator := range setup.operators {
			if challenges := operator.networkChallengeCount(); challenges != wantChallenges[index] {
				t.Errorf("operator %s was asked for %d network consent challenges, want %d", setup.domains[index], challenges, wantChallenges[index])
			}
		}
	}
}

// A genesis_hash beside a chain id of 0, as an operator answers from its epoch
// row before the chain id is mirrored, is reported rather than read through a
// challenge.
func TestHotkeyWalletChallengeReportsAnIncompleteEpochSubnetWithoutAChallenge(t *testing.T) {
	setup := newTestHotkeyWalletSetup(t, 1, func(index int, operator *testHotkeyWalletOperator) {
		operator.statesGenesisHash = true
		operator.domain.ChainID = 0
	})
	var out bytes.Buffer
	err := hotkeyWalletChallenge(t.Context(), setup.opts("wallet", "hotkey", "challenge", setup.coldkeySs58(), "--hotkey_seed_file="+setup.hotkeySeedPath), &out)
	if err == nil || !strings.Contains(err.Error(), "no authenticated listed operator stated its subnet") {
		t.Fatalf("incomplete subnet: %v", err)
	}
	if want := "operator one.example: its subnet is unavailable: GET /sn/epoch states chain 0 genesis 0x03"; !strings.Contains(out.String(), want) {
		t.Fatalf("challenge output lacks %q:\n%s", want, out.String())
	}
	if challenges := setup.operators[0].networkChallengeCount(); challenges != 0 {
		t.Fatalf("the operator was asked for %d network consent challenges", challenges)
	}
}
