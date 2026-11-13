// The keys, operator domain and identities here are visibly synthetic. The
// coldkey and hotkey sign the same substrate transcripts a wallet does.
package hotkeywallet

import (
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
)

var testDomain = protocol.ClientKeyHistoryDomain{
	ChainID:          7,
	GenesisHash:      [32]byte{3},
	Netuid:           25,
	Coordinator:      common.Address{4},
	SettlementVault:  common.Address{5},
	DeploymentIDHash: [32]byte{6},
	PolicyHash:       [32]byte{7},
	NoID:             8,
}

// When every synthetic statement is issued.
var testNow = time.Unix(1791244800, 0)

func testKey(t testing.TB, fill byte) *crv4.Keypair {
	t.Helper()
	var seed [32]byte
	for i := range seed {
		seed[i] = fill
	}
	key, err := crv4.KeypairFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// The next generation of the chain, signed by the coldkey and the hotkey.
func testOriginal(t testing.TB, chain []protocol.HotkeyWalletMappingConsent, coldkey *crv4.Keypair, hotkey *crv4.Keypair, fromEpoch uint64, throughEpoch uint64) protocol.HotkeyWalletMappingConsent {
	t.Helper()
	statement, err := NextStatement(chain, testDomain.HotkeySubnet(), hotkey.PublicKey(), coldkey.PublicKey(), fromEpoch, throughEpoch, testNow)
	if err != nil {
		t.Fatal(err)
	}
	return testSigned(t, *statement, coldkey, hotkey)
}

func testSigned(t testing.TB, statement protocol.HotkeyWalletMappingStatement, coldkey *crv4.Keypair, hotkey *crv4.Keypair) protocol.HotkeyWalletMappingConsent {
	t.Helper()
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	coldkeySignature, err := Sign(coldkey, message)
	if err != nil {
		t.Fatal(err)
	}
	hotkeySignature, err := Sign(hotkey, message)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.HotkeyWalletMappingConsent{Message: message, ColdkeySignature: coldkeySignature, HotkeySignature: hotkeySignature}
}

// Generation n earns from epoch 100n through 100n+1000.
func testChain(t testing.TB, coldkey *crv4.Keypair, hotkey *crv4.Keypair, generations int) []protocol.HotkeyWalletMappingConsent {
	t.Helper()
	var chain []protocol.HotkeyWalletMappingConsent
	for generation := uint64(1); generation <= uint64(generations); generation++ {
		chain = append(chain, testOriginal(t, chain, coldkey, hotkey, 100*generation, 100*generation+1000))
	}
	return chain
}

func testHash(t testing.TB, original protocol.HotkeyWalletMappingConsent) [32]byte {
	t.Helper()
	_, hash, err := protocol.VerifyHotkeyWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestNextStatementStartsAndExtendsTheChain(t *testing.T) {
	coldkey, hotkey, replacement := testKey(t, 1), testKey(t, 2), testKey(t, 3)
	first, err := NextStatement(nil, testDomain.HotkeySubnet(), hotkey.PublicKey(), coldkey.PublicKey(), 100, 1100, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if first.Schema != protocol.HotkeyWalletMappingConsentSchema || first.Scope != protocol.HotkeyWalletMappingScope || first.Generation != 1 || first.PreviousHash != ([32]byte{}) || first.Nonce == ([32]byte{}) || first.IssuedAt != testNow.Unix() || first.FromEpoch != 100 || first.ThroughEpoch != 1100 {
		t.Fatalf("first generation is not the chain's start: %+v", first)
	}
	chain := []protocol.HotkeyWalletMappingConsent{testSigned(t, *first, coldkey, hotkey)}
	// a later generation is how the coldkey is replaced
	second, err := NextStatement(chain, testDomain.HotkeySubnet(), hotkey.PublicKey(), replacement.PublicKey(), 200, 1200, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if second.Generation != 2 || second.PreviousHash != testHash(t, chain[0]) || second.Coldkey != replacement.PublicKey() || second.Nonce == first.Nonce || second.IssuedAt != testNow.Add(time.Hour).Unix() {
		t.Fatalf("second generation does not extend the chain: %+v", second)
	}
	chain = append(chain, testSigned(t, *second, replacement, hotkey))
	if head, headHash, err := protocol.VerifyHotkeyWalletMappingLineage(t.Context(), chain); err != nil || head.Generation != 2 || headHash != testHash(t, chain[1]) {
		t.Fatal("extended chain does not verify", head, err)
	}
}

func TestNextStatementRefusesAFromEpochNotAfterThePreviousGeneration(t *testing.T) {
	coldkey, hotkey := testKey(t, 1), testKey(t, 2)
	chain := testChain(t, coldkey, hotkey, 1)
	for _, fromEpoch := range []uint64{100, 99, 0} {
		if statement, err := NextStatement(chain, testDomain.HotkeySubnet(), hotkey.PublicKey(), coldkey.PublicKey(), fromEpoch, 1500, testNow); err == nil {
			t.Errorf("from epoch %d after the previous generation's 100 was admitted: %+v", fromEpoch, statement)
		}
	}
}

func TestNextStatementRefusesAnotherChainOrInvalidFields(t *testing.T) {
	coldkey, hotkey := testKey(t, 1), testKey(t, 2)
	chain := testChain(t, coldkey, hotkey, 1)
	otherSubnet := testDomain.HotkeySubnet()
	otherSubnet.Netuid++
	for _, c := range []struct {
		name                    string
		chain                   []protocol.HotkeyWalletMappingConsent
		subnet                  protocol.HotkeyWalletMappingSubnet
		hotkey, coldkey         [32]byte
		fromEpoch, throughEpoch uint64
		now                     time.Time
	}{
		{name: "another hotkey", chain: chain, subnet: testDomain.HotkeySubnet(), hotkey: testKey(t, 4).PublicKey(), coldkey: coldkey.PublicKey(), fromEpoch: 200, throughEpoch: 300, now: testNow},
		{name: "another subnet", chain: chain, subnet: otherSubnet, hotkey: hotkey.PublicKey(), coldkey: coldkey.PublicKey(), fromEpoch: 200, throughEpoch: 300, now: testNow},
		{name: "an incomplete subnet", subnet: protocol.HotkeyWalletMappingSubnet{ChainID: 7, Netuid: 25}, hotkey: hotkey.PublicKey(), coldkey: coldkey.PublicKey(), fromEpoch: 200, throughEpoch: 300, now: testNow},
		{name: "no coldkey", subnet: testDomain.HotkeySubnet(), hotkey: hotkey.PublicKey(), fromEpoch: 200, throughEpoch: 300, now: testNow},
		{name: "reversed epochs", subnet: testDomain.HotkeySubnet(), hotkey: hotkey.PublicKey(), coldkey: coldkey.PublicKey(), fromEpoch: 300, throughEpoch: 200, now: testNow},
		{name: "too many epochs", subnet: testDomain.HotkeySubnet(), hotkey: hotkey.PublicKey(), coldkey: coldkey.PublicKey(), fromEpoch: 200, throughEpoch: 200 + 65536, now: testNow},
		{name: "no issue time", subnet: testDomain.HotkeySubnet(), hotkey: hotkey.PublicKey(), coldkey: coldkey.PublicKey(), fromEpoch: 200, throughEpoch: 300, now: time.Unix(0, 0)},
		{name: "an unverifiable chain", chain: []protocol.HotkeyWalletMappingConsent{{Message: chain[0].Message, ColdkeySignature: chain[0].HotkeySignature, HotkeySignature: chain[0].HotkeySignature}}, subnet: testDomain.HotkeySubnet(), hotkey: hotkey.PublicKey(), coldkey: coldkey.PublicKey(), fromEpoch: 200, throughEpoch: 300, now: testNow},
	} {
		if statement, err := NextStatement(c.chain, c.subnet, c.hotkey, c.coldkey, c.fromEpoch, c.throughEpoch, c.now); err == nil {
			t.Errorf("next statement with %s was admitted: %+v", c.name, statement)
		}
	}
}

func TestSignWrapsTheMessageInBytes(t *testing.T) {
	hotkey := testKey(t, 2)
	statement, err := NextStatement(nil, testDomain.HotkeySubnet(), hotkey.PublicKey(), testKey(t, 1).PublicKey(), 100, 1100, testNow)
	if err != nil {
		t.Fatal(err)
	}
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := Sign(hotkey, message)
	if err != nil {
		t.Fatal(err)
	}
	if !hotkey.Verify([]byte("<Bytes>"+message+"</Bytes>"), signature[:]) || hotkey.Verify([]byte(message), signature[:]) {
		t.Fatal("the signature is not over the <Bytes>-wrapped message")
	}
}

func TestSignRefusesEveryOtherMessage(t *testing.T) {
	hotkey := testKey(t, 2)
	operator, _ := testOperatorKey(t)
	provider := protocol.WalletMappingStatement{Schema: protocol.WalletMappingConsentSchema, Domain: testDomain, UserId: [16]byte{7}, ClientId: [16]byte{8}, NetworkId: [16]byte{9}, Coldkey: hotkey.PublicKey(), Generation: 1, Nonce: [32]byte{10}, IssuedAt: 1000, ExpiresAt: 1300, FromEpoch: 51, ThroughEpoch: 151}
	providerMessage, err := provider.Message()
	if err != nil {
		t.Fatal(err)
	}
	network := protocol.NetworkWalletMappingStatement{Domain: testDomain, UserId: [16]byte{7}, NetworkId: [16]byte{9}, Coldkey: hotkey.PublicKey(), Generation: 1, Nonce: [32]byte{10}, IssuedAt: 1000, ExpiresAt: 1300, FromEpoch: 51, ThroughEpoch: 151}
	if err := protocol.SignProspectiveNetworkWalletMapping(&network, testBoundary, operator); err != nil {
		t.Fatal(err)
	}
	networkMessage, err := network.Message()
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{
		providerMessage,
		networkMessage,
		"Sign in to URnetwork\nChallenge: AAAAAAAAAAAAAAAAAAAAAAAA\nTimestamp: 1791244800",
		protocol.HotkeyWalletMappingConsentPrefix + "{}",
		protocol.HotkeyNetworkDelegationPrefix + "{}",
		"",
	} {
		if _, err := Sign(hotkey, message); !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
			t.Errorf("message %q was signed: %v", message, err)
		}
	}
}
