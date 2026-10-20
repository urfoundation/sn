// The independent verifier resolves hotkey mode, the last of the three earning
// modes, from retained delegation and global consent originals, with the same
// precedence and resolver as settlement. Keys and identities are synthetic.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	schnorrkel "github.com/ChainSafe/go-schnorrkel"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfoundation/sn/v2026/payoutartifact"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/ss58"
	"github.com/urfoundation/sn/v2026/validator"
	"github.com/urnetwork/connect/v2026"
)

// The hotkey that signs every delegation and global consent, and the coldkey
// its global consent names.
const economicHotkeyTestHotkeySeed = 51
const economicHotkeyTestColdkeySeed = 52

// The operator root the synthetic consents and delegations carry; its address
// is economicNetworkWalletTestSigner.
func economicHotkeyTestRoot(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("37", 32))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// Another operator's root, which the verifier never admits.
func economicHotkeyTestForeignRoot(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("38", 32))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// The public key and the substrate-context signature over message of a
// synthetic sr25519 seed.
func economicHotkeyTestKey(t *testing.T, seed byte, message string) ([32]byte, [64]byte) {
	t.Helper()
	key, err := schnorrkel.NewMiniSecretKeyFromRaw([32]byte{seed})
	if err != nil {
		t.Fatal(err)
	}
	if message == "" {
		return key.Public().Encode(), [64]byte{}
	}
	signature, err := key.ExpandEd25519().Sign(schnorrkel.NewSigningContext([]byte("substrate"), []byte(message)))
	if err != nil {
		t.Fatal(err)
	}
	return key.Public().Encode(), signature.Encode()
}

// A global consent chain of hotkeySeed naming the fixture coldkey. Generation
// g is effective from fromEpoch+g-1 through fromEpoch+100. Returns the
// originals and their hashes.
func economicHotkeyTestConsents(t *testing.T, hotkeySeed byte, fromEpoch uint64, generations int) ([]protocol.HotkeyWalletMappingConsent, [][32]byte) {
	t.Helper()
	hotkey, _ := economicHotkeyTestKey(t, hotkeySeed, "")
	coldkey, _ := economicHotkeyTestKey(t, economicHotkeyTestColdkeySeed, "")
	var originals []protocol.HotkeyWalletMappingConsent
	var hashes [][32]byte
	var previous [32]byte
	for index := 0; index < generations; index++ {
		statement := protocol.HotkeyWalletMappingStatement{Schema: protocol.HotkeyWalletMappingConsentSchema, Scope: protocol.HotkeyWalletMappingScope, Subnet: economicNetworkWalletTestDomain.HotkeySubnet(), Hotkey: hotkey, Coldkey: coldkey, Generation: uint64(index + 1), PreviousHash: previous, Nonce: [32]byte{hotkeySeed, byte(index + 1)}, IssuedAt: 3000, FromEpoch: fromEpoch + uint64(index), ThroughEpoch: fromEpoch + 100}
		message, err := statement.Message()
		if err != nil {
			t.Fatal(err)
		}
		_, coldkeySignature := economicHotkeyTestKey(t, economicHotkeyTestColdkeySeed, message)
		_, hotkeySignature := economicHotkeyTestKey(t, hotkeySeed, message)
		original := protocol.HotkeyWalletMappingConsent{Message: message, ColdkeySignature: coldkeySignature, HotkeySignature: hotkeySignature}
		_, hash, err := protocol.VerifyHotkeyWalletMappingConsent(t.Context(), original)
		if err != nil {
			t.Fatal(err)
		}
		originals, hashes, previous = append(originals, original), append(hashes, hash), hash
	}
	return originals, hashes
}

// A one-generation delegation of networkId by hotkeySeed to a global consent
// head, effective from the test epoch, issued before the window under root.
// mutate may change the statement before the operator signs it. Returns the
// original and its hash as a roster head.
func economicHotkeyTestDelegation(t *testing.T, networkId [16]byte, hotkeySeed byte, consentHead [32]byte, consentGeneration uint64, root *ecdsa.PrivateKey, mutate func(*protocol.HotkeyNetworkDelegationStatement)) (protocol.WalletMappingConsent, string) {
	t.Helper()
	hotkey, _ := economicHotkeyTestKey(t, hotkeySeed, "")
	statement := protocol.HotkeyNetworkDelegationStatement{Domain: economicNetworkWalletTestDomain, UserId: [16]byte{7}, NetworkId: networkId, Hotkey: hotkey, ConsentHeadHash: consentHead, ConsentGeneration: consentGeneration, Generation: 1, Nonce: [32]byte{networkId[0], hotkeySeed}, IssuedAt: 4000, ExpiresAt: 4300, FromEpoch: economicNetworkWalletTestEpoch, ThroughEpoch: economicNetworkWalletTestEpoch + 100}
	if mutate != nil {
		mutate(&statement)
	}
	if err := protocol.SignProspectiveHotkeyNetworkDelegation(&statement, economicNetworkWalletTestBoundary, root); err != nil {
		t.Fatal(err)
	}
	message, err := statement.Message()
	if err != nil {
		t.Fatal(err)
	}
	_, signature := economicHotkeyTestKey(t, hotkeySeed, message)
	original := protocol.WalletMappingConsent{Message: message, Signature: signature}
	_, hash, err := protocol.VerifyHotkeyNetworkDelegation(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, hex.EncodeToString(hash[:])
}

// A network consent for networkId whose acceptance may cross the window start,
// so its effective consent stays unknown.
func economicHotkeyTestUnknownNetwork(t *testing.T, networkId [16]byte, seed byte) (protocol.WalletMappingConsent, string) {
	t.Helper()
	original, _ := economicNetworkWalletTestSign(t, seed, func(coldkey [32]byte) string {
		statement := protocol.NetworkWalletMappingStatement{Domain: economicNetworkWalletTestDomain, UserId: [16]byte{7}, NetworkId: networkId, Coldkey: coldkey, Generation: 1, Nonce: [32]byte{seed}, IssuedAt: economicNetworkWalletTestStartUnix - 60, ExpiresAt: economicNetworkWalletTestStartUnix + 240, FromEpoch: economicNetworkWalletTestEpoch, ThroughEpoch: economicNetworkWalletTestEpoch + 100}
		if err := protocol.SignProspectiveNetworkWalletMapping(&statement, economicNetworkWalletTestBoundary, economicHotkeyTestRoot(t)); err != nil {
			t.Fatal(err)
		}
		message, err := statement.Message()
		if err != nil {
			t.Fatal(err)
		}
		return message
	})
	_, hash, err := protocol.VerifyNetworkWalletMappingConsent(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	return original, hex.EncodeToString(hash[:])
}

// Six providers in three networks, one per resolution path:
//   - network 9 pins a network consent effective at the epoch: client 1 has
//     its own effective consent, client 2 no own chain and client 3 an own
//     consent from a later epoch, so 2 and 3 earn to the network consent;
//   - network 10 pins no network consent and a delegation: client 4 earns to
//     the hotkey's global consent;
//   - network 11 pins a network consent from a later epoch and a delegation:
//     clients 5 (no own chain) and 6 (own consent from a later epoch) earn to
//     the hotkey's global consent.
type economicHotkeyTestRoster struct {
	authority          *payoutartifact.WholeWorkAuthority
	originals          *economicProviderOriginals
	providerColdkey    [32]byte
	networkColdkey     [32]byte
	hotkey             [32]byte
	hotkeyColdkey      [32]byte
	consentHash        [32]byte
	delegationHashes   map[[16]byte]string
	providerOriginals  map[[16]byte][]protocol.WalletMappingConsent
	networkOriginals   map[[16]byte][]protocol.WalletMappingConsent
	delegationChains   map[[16]byte][]protocol.WalletMappingConsent
	hotkeyConsentChain []protocol.HotkeyWalletMappingConsent
}

// The roster and exactly the evidence its resolution consults.
func newEconomicHotkeyTestRoster(t *testing.T) *economicHotkeyTestRoster {
	t.Helper()
	root := economicHotkeyTestRoot(t)
	roster := &economicHotkeyTestRoster{delegationHashes: map[[16]byte]string{}, providerOriginals: map[[16]byte][]protocol.WalletMappingConsent{}, networkOriginals: map[[16]byte][]protocol.WalletMappingConsent{}, delegationChains: map[[16]byte][]protocol.WalletMappingConsent{}}
	owned, ownedColdkey, ownedHead := economicNetworkWalletTestProvider(t, [16]byte{1}, [16]byte{9}, 21, economicNetworkWalletTestEpoch)
	later, _, laterHead := economicNetworkWalletTestProvider(t, [16]byte{3}, [16]byte{9}, 23, economicNetworkWalletTestEpoch+1)
	laterHotkey, _, laterHotkeyHead := economicNetworkWalletTestProvider(t, [16]byte{6}, [16]byte{11}, 26, economicNetworkWalletTestEpoch+1)
	network, networkColdkey, networkHead := economicNetworkWalletTestNetwork(t, [16]byte{9}, 29, economicNetworkWalletTestEpoch)
	laterNetwork, _, laterNetworkHead := economicNetworkWalletTestNetwork(t, [16]byte{11}, 31, economicNetworkWalletTestEpoch+1)
	consents, consentHashes := economicHotkeyTestConsents(t, economicHotkeyTestHotkeySeed, economicNetworkWalletTestEpoch, 1)
	for _, networkId := range [][16]byte{{10}, {11}} {
		delegation, head := economicHotkeyTestDelegation(t, networkId, economicHotkeyTestHotkeySeed, consentHashes[0], 1, root, nil)
		roster.delegationChains[networkId] = []protocol.WalletMappingConsent{delegation}
		roster.delegationHashes[networkId] = head
	}
	roster.providerColdkey, roster.networkColdkey = ownedColdkey, networkColdkey
	roster.hotkey, _ = economicHotkeyTestKey(t, economicHotkeyTestHotkeySeed, "")
	roster.hotkeyColdkey, _ = economicHotkeyTestKey(t, economicHotkeyTestColdkeySeed, "")
	roster.consentHash = consentHashes[0]
	roster.hotkeyConsentChain = consents
	roster.providerOriginals[[16]byte{1}] = []protocol.WalletMappingConsent{owned}
	roster.providerOriginals[[16]byte{3}] = []protocol.WalletMappingConsent{later}
	roster.providerOriginals[[16]byte{6}] = []protocol.WalletMappingConsent{laterHotkey}
	roster.networkOriginals[[16]byte{9}] = []protocol.WalletMappingConsent{network}
	roster.networkOriginals[[16]byte{11}] = []protocol.WalletMappingConsent{laterNetwork}
	roster.authority = &payoutartifact.WholeWorkAuthority{
		Domain: economicNetworkWalletTestDomain,
		Epoch:  economicNetworkWalletTestEpoch,
		ExpectedProviders: []payoutartifact.WholeWorkExpectedProvider{
			{ClientId: [16]byte{1}, NetworkId: [16]byte{9}, WalletHeadHash: ownedHead, WalletGeneration: 1},
			{ClientId: [16]byte{2}, NetworkId: [16]byte{9}},
			{ClientId: [16]byte{3}, NetworkId: [16]byte{9}, WalletHeadHash: laterHead, WalletGeneration: 1},
			{ClientId: [16]byte{4}, NetworkId: [16]byte{10}},
			{ClientId: [16]byte{5}, NetworkId: [16]byte{11}},
			{ClientId: [16]byte{6}, NetworkId: [16]byte{11}, WalletHeadHash: laterHotkeyHead, WalletGeneration: 1},
		},
		NetworkWallets: []payoutartifact.WholeWorkNetworkWallet{
			{NetworkId: [16]byte{9}, WalletHeadHash: networkHead, WalletGeneration: 1},
			{NetworkId: [16]byte{11}, WalletHeadHash: laterNetworkHead, WalletGeneration: 1},
		},
		HotkeyDelegations: []payoutartifact.WholeWorkHotkeyDelegation{
			{NetworkId: [16]byte{10}, DelegationHeadHash: roster.delegationHashes[[16]byte{10}], DelegationGeneration: 1},
			{NetworkId: [16]byte{11}, DelegationHeadHash: roster.delegationHashes[[16]byte{11}], DelegationGeneration: 1},
		},
	}
	roster.originals = &economicProviderOriginals{
		Wallets: []economicProviderWalletOriginal{
			{ClientId: [16]byte{1}, Originals: roster.providerOriginals[[16]byte{1}]},
			{ClientId: [16]byte{3}, Originals: roster.providerOriginals[[16]byte{3}]},
			{ClientId: [16]byte{6}, Originals: roster.providerOriginals[[16]byte{6}]},
		},
		NetworkWallets: []economicNetworkWalletOriginal{
			{NetworkId: [16]byte{9}, Originals: roster.networkOriginals[[16]byte{9}]},
			{NetworkId: [16]byte{11}, Originals: roster.networkOriginals[[16]byte{11}]},
		},
		HotkeyDelegations: []economicHotkeyDelegationOriginal{
			{NetworkId: [16]byte{10}, Originals: roster.delegationChains[[16]byte{10}]},
			{NetworkId: [16]byte{11}, Originals: roster.delegationChains[[16]byte{11}]},
		},
		HotkeyConsents: []economicHotkeyConsentOriginal{
			{NetworkId: [16]byte{10}, Originals: consents},
			{NetworkId: [16]byte{11}, Originals: consents},
		},
	}
	return roster
}

// The expected mode and coldkey of each fixture client.
func (self *economicHotkeyTestRoster) assertWallets(t *testing.T, wallets map[[16]byte]*protocol.EarningWallet) {
	t.Helper()
	for clientId, want := range map[[16]byte]struct {
		mode      string
		networkId [16]byte
		coldkey   [32]byte
	}{
		{1}: {mode: protocol.EarningWalletModeProvider, networkId: [16]byte{9}, coldkey: self.providerColdkey},
		{2}: {mode: protocol.EarningWalletModeNetwork, networkId: [16]byte{9}, coldkey: self.networkColdkey},
		{3}: {mode: protocol.EarningWalletModeNetwork, networkId: [16]byte{9}, coldkey: self.networkColdkey},
		{4}: {mode: protocol.EarningWalletModeHotkey, networkId: [16]byte{10}, coldkey: self.hotkeyColdkey},
		{5}: {mode: protocol.EarningWalletModeHotkey, networkId: [16]byte{11}, coldkey: self.hotkeyColdkey},
		{6}: {mode: protocol.EarningWalletModeHotkey, networkId: [16]byte{11}, coldkey: self.hotkeyColdkey},
	} {
		wallet := wallets[clientId]
		if wallet == nil || wallet.Mode != want.mode || wallet.Coldkey != want.coldkey || wallet.ClientId != clientId || wallet.NetworkId != want.networkId {
			t.Errorf("client %x earns to %+v, want %s", clientId[:1], wallet, want.mode)
			continue
		}
		if want.mode != protocol.EarningWalletModeHotkey {
			if wallet.Hotkey != ([32]byte{}) || wallet.ConsentHeadHash != ([32]byte{}) {
				t.Errorf("client %x gained hotkey fields in %s mode", clientId[:1], want.mode)
			}
			continue
		}
		delegationHead, _ := hex.DecodeString(self.delegationHashes[want.networkId])
		if wallet.Hotkey != self.hotkey || wallet.HeadHash != [32]byte(delegationHead) || wallet.OriginalHash != [32]byte(delegationHead) || wallet.Generation != 1 || wallet.HeadGeneration != 1 || wallet.ConsentOriginalHash != self.consentHash || wallet.ConsentHeadHash != self.consentHash || wallet.ConsentGeneration != 1 || wallet.ConsentHeadGeneration != 1 {
			t.Errorf("client %x hotkey wallet lost its delegation or global consent: %+v", clientId[:1], wallet)
		}
	}
	if len(wallets) != 6 || wallets[[16]byte{5}] == wallets[[16]byte{6}] {
		t.Error("a shared hotkey resolution was not owned per provider")
	}
}

// Each provider of the fixture roster earns through its own resolution path.
func TestEconomicProviderEarningWalletsApplyHotkeyPrecedence(t *testing.T) {
	roster := newEconomicHotkeyTestRoster(t)
	wallets, err := economicProviderEarningWallets(t.Context(), roster.authority, roster.originals, economicNetworkWalletTestSigner(t), economicNetworkWalletTestStartBlock, economicNetworkWalletTestStartUnix)
	if err != nil {
		t.Fatal(err)
	}
	roster.assertWallets(t, wallets)
}

// The hotkey resolution is evaluated only after the network chain falls back.
// Unknown, missing or contradictory evidence at any step is final, and only
// the evidence the resolution consults may be retained.
func TestEconomicProviderEarningWalletsRefuseUnsafeHotkeyFallbacks(t *testing.T) {
	root := economicHotkeyTestRoot(t)
	signer := economicNetworkWalletTestSigner(t)
	consents, consentHashes := economicHotkeyTestConsents(t, economicHotkeyTestHotkeySeed, economicNetworkWalletTestEpoch, 2)
	// one provider without its own chain in network 10, delegated to the hotkey
	single := func(networkWallets []payoutartifact.WholeWorkNetworkWallet, delegationHead string) *payoutartifact.WholeWorkAuthority {
		authority := &payoutartifact.WholeWorkAuthority{Domain: economicNetworkWalletTestDomain, Epoch: economicNetworkWalletTestEpoch, ExpectedProviders: []payoutartifact.WholeWorkExpectedProvider{{ClientId: [16]byte{4}, NetworkId: [16]byte{10}}}, NetworkWallets: networkWallets}
		if delegationHead != "" {
			authority.HotkeyDelegations = []payoutartifact.WholeWorkHotkeyDelegation{{NetworkId: [16]byte{10}, DelegationHeadHash: delegationHead, DelegationGeneration: 1}}
		}
		return authority
	}
	delegation, delegationHead := economicHotkeyTestDelegation(t, [16]byte{10}, economicHotkeyTestHotkeySeed, consentHashes[0], 1, root, nil)
	hotkeyEvidence := func(delegations []protocol.WalletMappingConsent, consentOriginals []protocol.HotkeyWalletMappingConsent) *economicProviderOriginals {
		return &economicProviderOriginals{HotkeyDelegations: []economicHotkeyDelegationOriginal{{NetworkId: [16]byte{10}, Originals: delegations}}, HotkeyConsents: []economicHotkeyConsentOriginal{{NetworkId: [16]byte{10}, Originals: consentOriginals}}}
	}
	// the evidence of the valid single-provider roster resolves
	if wallets, err := economicProviderEarningWallets(t.Context(), single(nil, delegationHead), hotkeyEvidence([]protocol.WalletMappingConsent{delegation}, consents[:1]), signer, economicNetworkWalletTestStartBlock, economicNetworkWalletTestStartUnix); err != nil || wallets[[16]byte{4}] == nil || wallets[[16]byte{4}].Mode != protocol.EarningWalletModeHotkey {
		t.Fatal("the valid hotkey fixture did not resolve", err)
	}
	unknownNetwork, unknownNetworkHead := economicHotkeyTestUnknownNetwork(t, [16]byte{10}, 33)
	unknownNetworkEvidence := hotkeyEvidence([]protocol.WalletMappingConsent{delegation}, consents[:1])
	unknownNetworkEvidence.NetworkWallets = []economicNetworkWalletOriginal{{NetworkId: [16]byte{10}, Originals: []protocol.WalletMappingConsent{unknownNetwork}}}
	laterDelegation, laterDelegationHead := economicHotkeyTestDelegation(t, [16]byte{10}, economicHotkeyTestHotkeySeed, consentHashes[0], 1, root, func(statement *protocol.HotkeyNetworkDelegationStatement) {
		statement.FromEpoch = economicNetworkWalletTestEpoch + 1
	})
	unknownDelegation, unknownDelegationHead := economicHotkeyTestDelegation(t, [16]byte{10}, economicHotkeyTestHotkeySeed, consentHashes[0], 1, root, func(statement *protocol.HotkeyNetworkDelegationStatement) {
		statement.IssuedAt, statement.ExpiresAt = economicNetworkWalletTestStartUnix-60, economicNetworkWalletTestStartUnix+240
	})
	foreignDelegation, foreignDelegationHead := economicHotkeyTestDelegation(t, [16]byte{10}, economicHotkeyTestHotkeySeed, consentHashes[0], 1, economicHotkeyTestForeignRoot(t), nil)
	otherHotkeyConsents, otherHotkeyHashes := economicHotkeyTestConsents(t, 53, economicNetworkWalletTestEpoch, 1)
	otherHotkeyDelegation, otherHotkeyDelegationHead := economicHotkeyTestDelegation(t, [16]byte{10}, economicHotkeyTestHotkeySeed, otherHotkeyHashes[0], 1, root, nil)
	laterConsents, laterConsentHashes := economicHotkeyTestConsents(t, economicHotkeyTestHotkeySeed, economicNetworkWalletTestEpoch+1, 1)
	laterConsentDelegation, laterConsentDelegationHead := economicHotkeyTestDelegation(t, [16]byte{10}, economicHotkeyTestHotkeySeed, laterConsentHashes[0], 1, root, nil)
	otherNetworkDelegation, _ := economicHotkeyTestDelegation(t, [16]byte{11}, economicHotkeyTestHotkeySeed, consentHashes[0], 1, root, nil)
	cases := []struct {
		name      string
		authority *payoutartifact.WholeWorkAuthority
		originals *economicProviderOriginals
		want      error
	}{
		{
			name:      "network consent unknown at the window start",
			authority: single([]payoutartifact.WholeWorkNetworkWallet{{NetworkId: [16]byte{10}, WalletHeadHash: unknownNetworkHead, WalletGeneration: 1}}, delegationHead),
			originals: unknownNetworkEvidence,
			want:      protocol.ErrWalletMappingUnavailable,
		},
		{
			name:      "delegation not effective at the epoch",
			authority: single(nil, laterDelegationHead),
			originals: hotkeyEvidence([]protocol.WalletMappingConsent{laterDelegation}, nil),
			want:      protocol.ErrWalletMappingNotEffective,
		},
		{
			name:      "delegation acceptance unknown at the window start",
			authority: single(nil, unknownDelegationHead),
			originals: hotkeyEvidence([]protocol.WalletMappingConsent{unknownDelegation}, consents[:1]),
			want:      protocol.ErrWalletMappingUnavailable,
		},
		{
			name:      "delegation issued under another operator root",
			authority: single(nil, foreignDelegationHead),
			originals: hotkeyEvidence([]protocol.WalletMappingConsent{foreignDelegation}, consents[:1]),
			want:      protocol.ErrWalletMappingIntegrity,
		},
		{
			name:      "missing delegation originals",
			authority: single(nil, delegationHead),
			originals: &economicProviderOriginals{HotkeyConsents: []economicHotkeyConsentOriginal{{NetworkId: [16]byte{10}, Originals: consents[:1]}}},
			want:      protocol.ErrWalletMappingUnavailable,
		},
		{
			name:      "missing global consent originals",
			authority: single(nil, delegationHead),
			originals: &economicProviderOriginals{HotkeyDelegations: []economicHotkeyDelegationOriginal{{NetworkId: [16]byte{10}, Originals: []protocol.WalletMappingConsent{delegation}}}},
			want:      protocol.ErrWalletMappingUnavailable,
		},
		{
			name:      "global consent beyond the named head",
			authority: single(nil, delegationHead),
			originals: hotkeyEvidence([]protocol.WalletMappingConsent{delegation}, consents),
			want:      protocol.ErrWalletMappingIntegrity,
		},
		{
			name:      "global consent of another hotkey",
			authority: single(nil, otherHotkeyDelegationHead),
			originals: hotkeyEvidence([]protocol.WalletMappingConsent{otherHotkeyDelegation}, otherHotkeyConsents),
			want:      protocol.ErrWalletMappingIntegrity,
		},
		{
			name:      "global consent not effective at the epoch",
			authority: single(nil, laterConsentDelegationHead),
			originals: hotkeyEvidence([]protocol.WalletMappingConsent{laterConsentDelegation}, laterConsents),
			want:      protocol.ErrWalletMappingNotEffective,
		},
		{
			name:      "delegation of another network",
			authority: single(nil, delegationHead),
			originals: hotkeyEvidence([]protocol.WalletMappingConsent{otherNetworkDelegation}, consents[:1]),
			want:      protocol.ErrWalletMappingIntegrity,
		},
		{
			name:      "hotkey originals for a network without a pinned delegation",
			authority: single(nil, ""),
			originals: hotkeyEvidence([]protocol.WalletMappingConsent{delegation}, consents[:1]),
			want:      protocol.ErrWalletMappingIntegrity,
		},
		{
			name:      "no network chain and no delegation",
			authority: single(nil, ""),
			originals: &economicProviderOriginals{},
			want:      protocol.ErrWalletMappingAbsent,
		},
	}
	for _, c := range cases {
		wallets, err := economicProviderEarningWallets(t.Context(), c.authority, c.originals, signer, economicNetworkWalletTestStartBlock, economicNetworkWalletTestStartUnix)
		if wallets != nil || !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, %v", c.name, wallets, err)
		}
	}
}

// Hotkey evidence that no resolution consults, or that is out of order, is
// refused even when every provider resolves without it.
func TestEconomicProviderEarningWalletsRefuseUnconsultedHotkeyEvidence(t *testing.T) {
	signer := economicNetworkWalletTestSigner(t)
	for index, change := range []func(*economicHotkeyTestRoster){
		// network 9 resolves in network mode, so its delegation is unconsulted
		func(roster *economicHotkeyTestRoster) {
			delegation, head := economicHotkeyTestDelegation(t, [16]byte{9}, economicHotkeyTestHotkeySeed, roster.consentHash, 1, economicHotkeyTestRoot(t), nil)
			roster.authority.HotkeyDelegations = append([]payoutartifact.WholeWorkHotkeyDelegation{{NetworkId: [16]byte{9}, DelegationHeadHash: head, DelegationGeneration: 1}}, roster.authority.HotkeyDelegations...)
			roster.originals.HotkeyDelegations = append([]economicHotkeyDelegationOriginal{{NetworkId: [16]byte{9}, Originals: []protocol.WalletMappingConsent{delegation}}}, roster.originals.HotkeyDelegations...)
			roster.originals.HotkeyConsents = append([]economicHotkeyConsentOriginal{{NetworkId: [16]byte{9}, Originals: roster.hotkeyConsentChain}}, roster.originals.HotkeyConsents...)
		},
		// a global consent chain alone, for a network that resolves otherwise
		func(roster *economicHotkeyTestRoster) {
			roster.originals.HotkeyConsents = append([]economicHotkeyConsentOriginal{{NetworkId: [16]byte{9}, Originals: roster.hotkeyConsentChain}}, roster.originals.HotkeyConsents...)
		},
		// out of order
		func(roster *economicHotkeyTestRoster) {
			slices.Reverse(roster.originals.HotkeyDelegations)
		},
		func(roster *economicHotkeyTestRoster) {
			slices.Reverse(roster.originals.HotkeyConsents)
		},
		// duplicated
		func(roster *economicHotkeyTestRoster) {
			roster.originals.HotkeyConsents = append(roster.originals.HotkeyConsents, roster.originals.HotkeyConsents[1])
		},
	} {
		roster := newEconomicHotkeyTestRoster(t)
		change(roster)
		if wallets, err := economicProviderEarningWallets(t.Context(), roster.authority, roster.originals, signer, economicNetworkWalletTestStartBlock, economicNetworkWalletTestStartUnix); wallets != nil || !errors.Is(err, protocol.ErrWalletMappingIntegrity) {
			t.Errorf("unconsulted or misordered hotkey evidence %d was admitted: %v", index, err)
		}
	}
}

// Without hotkey evidence the retained originals and the measurement encode
// exactly as before hotkey mode, so existing evidence hashes are unchanged.
func TestEconomicProviderEvidenceWithoutHotkeyKeepsBytes(t *testing.T) {
	roster := newEconomicHotkeyTestRoster(t)
	originals := *roster.originals
	originals.HotkeyDelegations, originals.HotkeyConsents = nil, nil
	originals.Attempts = json.RawMessage(`{"synthetic":true}`)
	// the shape before hotkey mode
	previous := struct {
		Work           *payoutartifact.WholeWorkInventory        `json:"work"`
		Wallets        []economicProviderWalletOriginal          `json:"wallets"`
		NetworkWallets []economicNetworkWalletOriginal           `json:"network_wallets,omitempty"`
		Bindings       *validator.ProviderAttemptBindingOriginal `json:"bindings"`
		Attempts       json.RawMessage                           `json:"attempts"`
	}{Work: originals.Work, Wallets: originals.Wallets, NetworkWallets: originals.NetworkWallets, Bindings: originals.Bindings, Attempts: originals.Attempts}
	if rootObjectHash(originals) != rootObjectHash(previous) {
		t.Fatal("provider originals without hotkey evidence changed their bytes")
	}
	measurement := economicConservationProviderMeasurement{WorkInventoryHash: "sha256:01", WalletOriginalsHash: "sha256:02", NetworkWalletOriginalsHash: "sha256:03", BindingOriginalsHash: "sha256:04", TrialAuthorityHash: "sha256:05", TrialOriginalsHash: "06", ArtifactHash: "sha256:07", WorkAuthorityHash: "sha256:08", WorkWindowHash: "sha256:09", TrialRegistryHash: "sha256:10", TrialWindowHash: "sha256:11", ProviderHash: "sha256:12", Providers: 13, CompletedBytes: 14, Assignments: 15, Confirmations: 16}
	previousMeasurement := struct {
		WorkInventoryHash          string `json:"work_inventory_hash"`
		WalletOriginalsHash        string `json:"wallet_originals_hash"`
		NetworkWalletOriginalsHash string `json:"network_wallet_originals_hash,omitempty"`
		BindingOriginalsHash       string `json:"binding_originals_hash"`
		TrialAuthorityHash         string `json:"trial_authority_hash"`
		TrialOriginalsHash         string `json:"trial_originals_hash"`
		ArtifactHash               string `json:"artifact_hash"`
		WorkAuthorityHash          string `json:"work_authority_hash"`
		WorkWindowHash             string `json:"work_window_hash"`
		TrialRegistryHash          string `json:"trial_registry_hash"`
		TrialWindowHash            string `json:"trial_window_hash"`
		ProviderHash               string `json:"provider_hash"`
		Providers                  uint64 `json:"providers"`
		CompletedBytes             uint64 `json:"completed_bytes"`
		Assignments                uint64 `json:"assignments"`
		Confirmations              uint64 `json:"confirmations"`
	}{WorkInventoryHash: "sha256:01", WalletOriginalsHash: "sha256:02", NetworkWalletOriginalsHash: "sha256:03", BindingOriginalsHash: "sha256:04", TrialAuthorityHash: "sha256:05", TrialOriginalsHash: "06", ArtifactHash: "sha256:07", WorkAuthorityHash: "sha256:08", WorkWindowHash: "sha256:09", TrialRegistryHash: "sha256:10", TrialWindowHash: "sha256:11", ProviderHash: "sha256:12", Providers: 13, CompletedBytes: 14, Assignments: 15, Confirmations: 16}
	if rootObjectHash(measurement) != rootObjectHash(previousMeasurement) {
		t.Fatal("a provider measurement without hotkey evidence changed its bytes")
	}
	// with hotkey evidence both encodings name it
	raw, err := json.Marshal(roster.originals)
	if err != nil || !bytes.Contains(raw, []byte(`"hotkey_delegations":`)) || !bytes.Contains(raw, []byte(`"hotkey_consents":`)) {
		t.Fatal("hotkey evidence was not retained", err)
	}
}

// An operator API serving the four history routes from fixed chains. It
// records each route's requested selectors.
type economicHotkeyTestApi struct {
	stateLock sync.Mutex
	requested map[string][]string
	roster    *economicHotkeyTestRoster
}

// Serves the roster's chains; the global consent chain through the requested
// generation.
func (self *economicHotkeyTestApi) serve(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		var request struct {
			ClientId   string   `json:"client_id"`
			NetworkId  string   `json:"network_id"`
			HotkeySs58 string   `json:"hotkey_ss58"`
			HeadHash   [32]byte `json:"head_hash"`
			Generation uint64   `json:"generation"`
		}
		if err != nil || json.Unmarshal(raw, &request) != nil {
			http.Error(w, "synthetic request mismatch", http.StatusBadRequest)
			return
		}
		selector := request.ClientId + request.NetworkId + request.HotkeySs58
		func() {
			self.stateLock.Lock()
			defer self.stateLock.Unlock()
			self.requested[r.URL.Path] = append(self.requested[r.URL.Path], selector)
		}()
		var originals any
		switch r.URL.Path {
		case "/sn/wallet/consent/history":
			originals = self.roster.providerOriginals[economicHotkeyTestId(t, request.ClientId)]
		case "/sn/wallet/network-consent/history":
			originals = self.roster.networkOriginals[economicHotkeyTestId(t, request.NetworkId)]
		case "/sn/wallet/hotkey-delegation/history":
			originals = self.roster.delegationChains[economicHotkeyTestId(t, request.NetworkId)]
		case "/sn/wallet/hotkey-consent/history":
			originals = self.roster.hotkeyConsentChain[:request.Generation]
		}
		_ = json.NewEncoder(w).Encode(struct {
			Originals any `json:"originals"`
		}{Originals: originals})
	}))
}

// The 16 bytes of a server UUID string.
func economicHotkeyTestId(t *testing.T, value string) [16]byte {
	t.Helper()
	id, err := connect.ParseId(value)
	if err != nil {
		return [16]byte{}
	}
	return [16]byte(id)
}

// The live reader acquires exactly the evidence the admission consults: no
// hotkey chain for a network whose consent is effective, and the delegation
// and global consent for each network that falls back past its consent.
func TestReadEconomicProviderWalletOriginalsFollowsHotkeyPrecedence(t *testing.T) {
	roster := newEconomicHotkeyTestRoster(t)
	api := &economicHotkeyTestApi{requested: map[string][]string{}, roster: roster}
	endpoint := api.serve(t)
	defer endpoint.Close()
	reader, err := validator.NewHttpWalletMappingReader(endpoint.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.CloseIdleConnections()
	remaining := uint64(16 * 1024 * 1024)
	charges := 0
	charge := func(value any) error {
		raw, err := json.Marshal(value)
		if err != nil || uint64(len(raw)) > remaining {
			return errors.Join(errMonitorEconomicCapacity, err)
		}
		remaining -= uint64(len(raw))
		charges++
		return nil
	}
	result := &economicProviderOriginals{}
	if err := readEconomicProviderWalletOriginals(t.Context(), reader, roster.authority, result, charge, func() uint64 { return remaining }); err != nil {
		t.Fatal(err)
	}
	if charges != 3+2+2+2 || len(result.Wallets) != 3 || len(result.NetworkWallets) != 2 || len(result.HotkeyDelegations) != 2 || len(result.HotkeyConsents) != 2 {
		t.Fatal("the reader acquired other evidence than the precedence consults", charges, result)
	}
	hotkeySs58, err := ss58.Encode(roster.hotkey, ss58.BittensorPrefix)
	if err != nil {
		t.Fatal(err)
	}
	wantRequests := map[string][]string{
		"/sn/wallet/consent/history":           {connect.Id([16]byte{1}).String(), connect.Id([16]byte{3}).String(), connect.Id([16]byte{6}).String()},
		"/sn/wallet/network-consent/history":   {connect.Id([16]byte{9}).String(), connect.Id([16]byte{11}).String()},
		"/sn/wallet/hotkey-delegation/history": {connect.Id([16]byte{10}).String(), connect.Id([16]byte{11}).String()},
		"/sn/wallet/hotkey-consent/history":    {hotkeySs58, hotkeySs58},
	}
	func() {
		api.stateLock.Lock()
		defer api.stateLock.Unlock()
		for path, want := range wantRequests {
			if !slices.Equal(api.requested[path], want) {
				t.Errorf("%s was requested for %v, want %v", path, api.requested[path], want)
			}
		}
	}()
	wallets, err := economicProviderEarningWallets(t.Context(), roster.authority, result, economicNetworkWalletTestSigner(t), economicNetworkWalletTestStartBlock, economicNetworkWalletTestStartUnix)
	if err != nil {
		t.Fatal("the admission refused the evidence its reader acquired", err)
	}
	roster.assertWallets(t, wallets)
}

// The last mode reached has nothing to fall back to: a network chain that is
// not effective without a pinned delegation, or a delegation that is not
// effective, fails the acquisition.
func TestReadEconomicProviderWalletOriginalsRefusesFinalFallbacks(t *testing.T) {
	for index, change := range []func(*economicHotkeyTestRoster){
		func(roster *economicHotkeyTestRoster) {
			roster.authority.HotkeyDelegations = roster.authority.HotkeyDelegations[:1]
		},
		func(roster *economicHotkeyTestRoster) {
			delegation, head := economicHotkeyTestDelegation(t, [16]byte{10}, economicHotkeyTestHotkeySeed, roster.consentHash, 1, economicHotkeyTestRoot(t), func(statement *protocol.HotkeyNetworkDelegationStatement) {
				statement.FromEpoch = economicNetworkWalletTestEpoch + 1
			})
			roster.delegationChains[[16]byte{10}] = []protocol.WalletMappingConsent{delegation}
			roster.authority.HotkeyDelegations[0].DelegationHeadHash = head
		},
	} {
		roster := newEconomicHotkeyTestRoster(t)
		change(roster)
		api := &economicHotkeyTestApi{requested: map[string][]string{}, roster: roster}
		endpoint := api.serve(t)
		reader, err := validator.NewHttpWalletMappingReader(endpoint.URL)
		if err != nil {
			endpoint.Close()
			t.Fatal(err)
		}
		remaining := uint64(16 * 1024 * 1024)
		err = readEconomicProviderWalletOriginals(t.Context(), reader, roster.authority, &economicProviderOriginals{}, func(any) error { return nil }, func() uint64 { return remaining })
		reader.CloseIdleConnections()
		endpoint.Close()
		if !errors.Is(err, protocol.ErrWalletMappingUnavailable) {
			t.Errorf("a final fallback %d was acquired: %v", index, err)
		}
	}
}
